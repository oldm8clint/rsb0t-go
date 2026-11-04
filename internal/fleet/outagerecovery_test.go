package fleet

import (
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"rsb0t-go/internal/client"
)

// The tests in this file reproduce the 2026-09-20 outage, in which 31 of 269 live
// accounts were retired permanently into the dashboard's "error" state — 19 on
// `client: dial: websocket: bad handshake` and 12 on `client: checksums: expected >= 36
// bytes from /crc0, got 0` — and had to be brought back one POST at a time.
//
// They deliberately drive the REAL client.Connect against a real HTTP/WebSocket server
// that is genuinely refusing, rather than handing the manager a hand-written error
// value. Hand-written errors are what let this bug exist: retryableLoginError classifies
// by type, and the two errors that stranded the fleet were untyped strings produced deep
// inside gorilla and net/http, which no test that invents its own error would ever have
// produced. Each test therefore asserts on the account reaching the connected state
// again by itself, with the failure observed on the way — not on a log line.

// outageServer serves both the cache endpoint and the game WebSocket, and can be put
// into an outage in either or both of the two ways the live server failed.
type outageServer struct {
	*httptest.Server
	mu           sync.Mutex
	refuseUpgade bool // answer the WebSocket upgrade with 502 rather than 101
	emptyCrc     bool // answer /crc0 with a zero-length body
	dials        atomic.Int32
	crcFetches   atomic.Int32
}

func newOutageServer(t *testing.T) *outageServer {
	t.Helper()
	s := &outageServer{}
	upgrader := websocket.Upgrader{Subprotocols: []string{"binary"}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		refuse, empty := s.refuseUpgade, s.emptyCrc
		s.mu.Unlock()

		if strings.HasSuffix(r.URL.Path, "crc0") {
			s.crcFetches.Add(1)
			if empty {
				// Exactly the live failure: 200 OK, zero bytes. A server whose
				// cache has not finished loading answers this way.
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(make([]byte, 40))
			return
		}

		s.dials.Add(1)
		if refuse {
			// A reverse proxy that is up while the game server behind it is not.
			// gorilla turns any non-101 answer into websocket.ErrBadHandshake.
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		// The same minimal accept-everything handshake localLoginManager uses.
		if _, _, err = ws.ReadMessage(); err != nil {
			return
		}
		if err = ws.WriteMessage(websocket.BinaryMessage, make([]byte, 17)); err != nil {
			return
		}
		if _, _, err = ws.ReadMessage(); err != nil {
			return
		}
		if err = ws.WriteMessage(websocket.BinaryMessage, []byte{2, 0, 0}); err != nil {
			return
		}
		for {
			if _, _, err = ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *outageServer) set(refuseUpgrade, emptyCrc bool) {
	s.mu.Lock()
	s.refuseUpgade, s.emptyCrc = refuseUpgrade, emptyCrc
	s.mu.Unlock()
}

func (s *outageServer) wsURL() string { return "ws" + strings.TrimPrefix(s.URL, "http") }

func (s *outageServer) httpBase() string { return s.URL + "/" }

// backoffRecorder replaces the manager's wait so a test runs at full speed while still
// seeing every delay the real schedule asked for.
type backoffRecorder struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (b *backoffRecorder) wait(<-chan struct{}, time.Duration) bool { return true }

func (b *backoffRecorder) record(d time.Duration) {
	b.mu.Lock()
	b.waits = append(b.waits, d)
	b.mu.Unlock()
}

func (b *backoffRecorder) all() []time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]time.Duration(nil), b.waits...)
}

func (b *backoffRecorder) install(m *Manager) {
	m.retryWait = func(done <-chan struct{}, d time.Duration) bool {
		b.record(d)
		return true
	}
}

// assertJitteredSchedule checks the recorded delays are the ordinary reconnect schedule
// with its ±30% jitter, which is what "retried with the existing backoff" has to mean if
// it is to mean anything: a retry loop with no wait is the thundering herd jitterFraction
// exists to prevent.
func assertJitteredSchedule(t *testing.T, waits []time.Duration) {
	t.Helper()
	if len(waits) == 0 {
		t.Fatal("no retry was ever scheduled — the account was retired instead of retried")
	}
	for i, got := range waits {
		want := reconnectBackoff(i + 1)
		lo, hi := time.Duration(float64(want)*0.7), time.Duration(float64(want)*1.3)
		if got < lo || got > hi {
			t.Fatalf("retry %d waited %v, outside the jittered %v schedule [%v,%v]", i+1, got, want, lo, hi)
		}
	}
}

// TestConnectStageErrorsAreRetryable states the classification directly, alongside the
// end-to-end tests below that prove the real errors carry this type.
func TestConnectStageErrorsAreRetryable(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"bad handshake", &client.ConnectError{Stage: client.StageDial, Err: websocket.ErrBadHandshake}, true},
		{"empty crc0", &client.ConnectError{Stage: client.StageChecksums, Err: errShortCrc}, true},
		{"crc0 http error", &client.ConnectError{Stage: client.StageChecksums, Err: &client.HTTPStatusError{URL: "http://x/crc0", Status: "502 Bad Gateway"}}, true},
		{"bad credentials", &client.LoginRejectedError{Response: 3}, false},
		{"out of date", &client.LoginRejectedError{Response: 6}, false},
	} {
		if got := retryableLoginError(tc.err); got != tc.want {
			t.Errorf("%s (%v): retryable=%v want=%v", tc.name, tc.err, got, tc.want)
		}
	}
}

// TestConnectErrorKeepsItsLiveWording guards the operator-facing side: the dashboard's
// lastError column, the archived fleet logs and every triage note written against them
// must keep reading exactly as they did before these errors gained a type.
func TestConnectErrorKeepsItsLiveWording(t *testing.T) {
	for _, tc := range []struct{ err, want string }{
		{(&client.ConnectError{Stage: client.StageDial, Err: websocket.ErrBadHandshake}).Error(), "client: dial: websocket: bad handshake"},
		{(&client.ConnectError{Stage: client.StageChecksums, Err: errShortCrc}).Error(), "client: checksums: expected >= 36 bytes from /crc0, got 0"},
	} {
		if tc.err != tc.want {
			t.Errorf("error text %q, want %q", tc.err, tc.want)
		}
	}
}

var errShortCrc = errShortCrcValue{}

type errShortCrcValue struct{}

func (errShortCrcValue) Error() string { return "expected >= 36 bytes from /crc0, got 0" }

// TestFleetHealsFromADialHandshakeFailure is the 19-account half of the live incident.
func TestFleetHealsFromADialHandshakeFailure(t *testing.T) {
	srv := newOutageServer(t)
	srv.set(true, false)

	// Checksums supplied, so this test isolates the dial: the only thing that can fail
	// is the WebSocket upgrade.
	m := New(Config{WSURL: srv.wsURL(), HTTPBase: srv.httpBase(), Checksums: &[9]int32{},
		Modulus: big.NewInt(9999991), Exponent: big.NewInt(65537), NoAntiCheat: true})
	t.Cleanup(m.CloseAll)

	var b backoffRecorder
	b.install(m)

	m.Connect("test_handshake", "fixture_password")

	// The failure has to be the real one, seen by the manager, before anything is
	// claimed about recovery.
	st := m.Snapshot()[0]
	if !strings.Contains(st.Error, "bad handshake") {
		t.Fatalf("first login did not fail the way the live fleet did: %q", st.Error)
	}
	if !st.Reconnecting {
		t.Fatal("a dial handshake failure was treated as terminal: no retry scheduled")
	}

	srv.set(false, false)
	got := waitFleetStatus(t, m, func(st AccountStatus) bool { return st.Connected && !st.Reconnecting })
	if got.Error != "" {
		t.Fatalf("recovered account still carries an error: %q", got.Error)
	}
	if srv.dials.Load() < 2 {
		t.Fatalf("server saw %d dials; the account cannot have retried", srv.dials.Load())
	}
	assertJitteredSchedule(t, b.all())
}

// TestFleetHealsFromAnEmptyCrc0 is the 12-account half of the live incident.
func TestFleetHealsFromAnEmptyCrc0(t *testing.T) {
	srv := newOutageServer(t)
	srv.set(false, true)

	// No Checksums configured, so every login fetches /crc0 for itself — the
	// production shape, since cmd/controlpanel sets neither.
	m := New(Config{WSURL: srv.wsURL(), HTTPBase: srv.httpBase(),
		Modulus: big.NewInt(9999991), Exponent: big.NewInt(65537), NoAntiCheat: true})
	t.Cleanup(m.CloseAll)
	client.InvalidateChecksums()
	t.Cleanup(client.InvalidateChecksums)

	var b backoffRecorder
	b.install(m)

	m.Connect("test_crc", "fixture_password")

	st := m.Snapshot()[0]
	if !strings.Contains(st.Error, "/crc0") {
		t.Fatalf("first login did not fail the way the live fleet did: %q", st.Error)
	}
	if !st.Reconnecting {
		t.Fatal("an empty /crc0 was treated as terminal: no retry scheduled")
	}

	srv.set(false, false)
	got := waitFleetStatus(t, m, func(st AccountStatus) bool { return st.Connected && !st.Reconnecting })
	if got.Error != "" {
		t.Fatalf("recovered account still carries an error: %q", got.Error)
	}
	if srv.crcFetches.Load() < 2 {
		t.Fatalf("server saw %d /crc0 fetches; the account cannot have retried", srv.crcFetches.Load())
	}
	assertJitteredSchedule(t, b.all())
}

// TestFleetHealsWhenBothTheWebAndGameSidesAreDown is the outage as it actually happened:
// the whole server is unreachable for a while and the fleet is expected to come back
// with nobody touching it.
func TestFleetHealsFromAWholeServerOutage(t *testing.T) {
	srv := newOutageServer(t)
	srv.set(true, true)

	m := New(Config{WSURL: srv.wsURL(), HTTPBase: srv.httpBase(),
		Modulus: big.NewInt(9999991), Exponent: big.NewInt(65537), NoAntiCheat: true})
	t.Cleanup(m.CloseAll)
	client.InvalidateChecksums()
	t.Cleanup(client.InvalidateChecksums)

	var b backoffRecorder
	b.install(m)

	for _, name := range []string{"outage_a", "outage_b", "outage_c"} {
		m.Connect(name, "fixture_password")
	}
	for _, st := range m.Snapshot() {
		if !st.Reconnecting {
			t.Fatalf("%s was retired during the outage (error %q) instead of waiting it out", st.Username, st.Error)
		}
	}

	srv.set(false, false)
	deadline := time.Now().Add(10 * time.Second)
	for {
		snap := m.Snapshot()
		back := 0
		for _, st := range snap {
			if st.Connected && !st.Reconnecting && st.Error == "" {
				back++
			}
		}
		if back == len(snap) && len(snap) == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of 3 accounts came back on their own: %+v", back, snap)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestLoginRejectionsThatMustStayTerminal pins the other half of the requirement: the
// fix must not turn a permanent rejection into an endless retry.
func TestLoginRejectionsThatMustStayTerminal(t *testing.T) {
	for _, response := range []int{3, 4, 6, 11, 12, 13} {
		m := localLoginManager(t)
		var calls atomic.Int32
		m.connectLogin = func(client.Config) (*client.Session, error) {
			calls.Add(1)
			return nil, &client.LoginRejectedError{Response: response}
		}
		m.retryWait = func(<-chan struct{}, time.Duration) bool { return true }
		m.Connect("test_terminal", "fixture_password")
		st := waitFleetStatus(t, m, func(st AccountStatus) bool { return !st.Reconnecting })
		if st.Connected || st.Error == "" {
			t.Fatalf("response %d: status=%+v", response, st)
		}
		// One attempt, then stopped: an account whose credentials or client
		// revision are wrong must not keep hammering the login server.
		time.Sleep(20 * time.Millisecond)
		if n := calls.Load(); n != 1 {
			t.Fatalf("response %d: %d login attempts, want exactly 1", response, n)
		}
		m.CloseAll()
	}
}

// TestResponse8IsRetried pins the third error class seen in the live outage. Those
// accounts did recover, and must keep recovering.
func TestResponse8IsRetried(t *testing.T) {
	m := localLoginManager(t)
	var calls atomic.Int32
	m.connectLogin = func(cfg client.Config) (*client.Session, error) {
		if calls.Add(1) == 1 {
			return nil, &client.LoginRejectedError{Response: 8}
		}
		return client.Connect(cfg)
	}
	var b backoffRecorder
	b.install(m)
	m.Connect("test_response8", "fixture_password")
	waitFleetStatus(t, m, func(st AccountStatus) bool { return st.Connected && !st.Reconnecting })
	assertJitteredSchedule(t, b.all())
}

// TestRotatedLoginKeyIsRefetchedAndRetried covers the hazard CLAUDE.md names and the
// live fleet has never had a defence against: The live server rotates its RSA keypair on every
// restart, cmd/controlpanel scrapes it once, and the engine answers a block it cannot
// decrypt with response 6 — which is terminal, and rightly so for its other meanings.
func TestRotatedLoginKeyIsRefetchedAndRetried(t *testing.T) {
	oldKey, newKey := big.NewInt(1111111), big.NewInt(2222222)
	var fetches atomic.Int32

	m := localLoginManager(t)
	m.cfg.Modulus = oldKey
	m.cfg.ModulusSource = func() (*big.Int, error) {
		fetches.Add(1)
		return newKey, nil
	}
	var signedWith []*big.Int
	var mu sync.Mutex
	m.connectLogin = func(cfg client.Config) (*client.Session, error) {
		mu.Lock()
		signedWith = append(signedWith, cfg.Modulus)
		mu.Unlock()
		// The engine's own behaviour: a block it cannot decrypt is answered
		// "out of date", exactly as a stale cache or an old client would be.
		if cfg.Modulus.Cmp(newKey) != 0 {
			return nil, &client.LoginRejectedError{Response: 6}
		}
		return client.Connect(cfg)
	}
	var b backoffRecorder
	b.install(m)

	m.Connect("test_rotated", "fixture_password")
	waitFleetStatus(t, m, func(st AccountStatus) bool { return st.Connected && !st.Reconnecting })

	mu.Lock()
	defer mu.Unlock()
	if len(signedWith) < 2 {
		t.Fatalf("only %d login attempts — the rejection was not retried", len(signedWith))
	}
	if signedWith[0].Cmp(oldKey) != 0 {
		t.Fatalf("first attempt signed with %v, want the startup key %v", signedWith[0], oldKey)
	}
	if last := signedWith[len(signedWith)-1]; last.Cmp(newKey) != 0 {
		t.Fatalf("recovery signed with %v, want the rotated key %v", last, newKey)
	}
	if fetches.Load() != 1 {
		t.Fatalf("re-read the key %d times, want exactly 1", fetches.Load())
	}
	assertJitteredSchedule(t, b.all())
}

// TestOneRotationCostsOneFetchForTheWholeFleet is why staleLoginKey tracks generations
// rather than just re-fetching on every response 6: at the real roster a rotation
// rejects hundreds of accounts within seconds, and each of them must not go and scrape
// the client bundle for itself.
func TestOneRotationCostsOneFetchForTheWholeFleet(t *testing.T) {
	oldKey, newKey := big.NewInt(1111111), big.NewInt(2222222)
	var fetches atomic.Int32

	m := localLoginManager(t)
	m.cfg.Modulus = oldKey
	m.cfg.ModulusSource = func() (*big.Int, error) {
		fetches.Add(1)
		return newKey, nil
	}
	m.connectLogin = func(cfg client.Config) (*client.Session, error) {
		if cfg.Modulus.Cmp(newKey) != 0 {
			return nil, &client.LoginRejectedError{Response: 6}
		}
		return client.Connect(cfg)
	}
	m.retryWait = func(<-chan struct{}, time.Duration) bool { return true }

	names := []string{"rot_a", "rot_b", "rot_c", "rot_d", "rot_e", "rot_f"}
	for _, n := range names {
		m.Connect(n, "fixture_password")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		back := 0
		snap := m.Snapshot()
		for _, st := range snap {
			if st.Connected && !st.Reconnecting {
				back++
			}
		}
		if back == len(names) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d accounts recovered from the rotation: %+v", back, len(names), snap)
		}
		time.Sleep(time.Millisecond)
	}
	if n := fetches.Load(); n != 1 {
		t.Fatalf("%d accounts caused %d key re-reads, want 1", len(names), n)
	}
}

// TestResponse6StaysTerminalWhenTheKeyDidNotChange is the guard that keeps the rotation
// recovery honest. Response 6 also means "your client revision is wrong", which nothing
// here can fix — so forgiving it requires observing that the key really moved.
func TestResponse6StaysTerminalWhenTheKeyDidNotChange(t *testing.T) {
	sameKey := big.NewInt(1111111)
	var fetches atomic.Int32

	m := localLoginManager(t)
	m.cfg.Modulus = sameKey
	m.cfg.ModulusSource = func() (*big.Int, error) {
		fetches.Add(1)
		return new(big.Int).Set(sameKey), nil
	}
	var calls atomic.Int32
	m.connectLogin = func(client.Config) (*client.Session, error) {
		calls.Add(1)
		return nil, &client.LoginRejectedError{Response: 6}
	}
	m.retryWait = func(<-chan struct{}, time.Duration) bool { return true }

	m.Connect("test_outofdate", "fixture_password")
	st := waitFleetStatus(t, m, func(st AccountStatus) bool { return !st.Reconnecting })
	if st.Connected || !strings.Contains(st.Error, "response 6") {
		t.Fatalf("status=%+v", st)
	}
	time.Sleep(20 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Fatalf("%d login attempts, want exactly 1 — an out-of-date client must stop", n)
	}
	if fetches.Load() != 1 {
		t.Fatalf("re-read the key %d times, want exactly 1 (checked once, unchanged, gave up)", fetches.Load())
	}
}

// TestUnreadableLoginKeyKeepsAccountsInRetry covers the worst version of the rotation
// case: the server restarted, the key moved AND its client bundle is not being served
// yet, so the fleet cannot tell whether the key changed. That is the moment an account
// most needs not to be given up on, so an unreadable key keeps it retrying.
func TestUnreadableLoginKeyKeepsAccountsInRetry(t *testing.T) {
	oldKey, newKey := big.NewInt(1111111), big.NewInt(2222222)
	var readable atomic.Bool

	m := localLoginManager(t)
	m.cfg.Modulus = oldKey
	m.cfg.ModulusSource = func() (*big.Int, error) {
		if !readable.Load() {
			return nil, errors.New("fleet: fetching client bundle: connection refused")
		}
		return newKey, nil
	}
	m.connectLogin = func(cfg client.Config) (*client.Session, error) {
		if cfg.Modulus.Cmp(newKey) != 0 {
			return nil, &client.LoginRejectedError{Response: 6}
		}
		return client.Connect(cfg)
	}
	m.retryWait = func(<-chan struct{}, time.Duration) bool { return true }

	m.Connect("test_unreadable", "fixture_password")
	waitFleetStatus(t, m, func(st AccountStatus) bool { return st.Reconnecting })

	// Nothing may retire the account while the key cannot be read.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if st := m.Snapshot()[0]; !st.Reconnecting && !st.Connected {
			t.Fatalf("account retired while the key was unreadable: %+v", st)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// modulusRecheckInterval would otherwise hold the stale "could not read it"
	// verdict; the rotation is only observable once the bundle is served again.
	m.modMu.Lock()
	m.modCheckedAt = time.Time{}
	m.modMu.Unlock()
	readable.Store(true)
	got := waitFleetStatus(t, m, func(st AccountStatus) bool { return st.Connected && !st.Reconnecting })
	if got.Error != "" {
		t.Fatalf("recovered account still carries an error: %q", got.Error)
	}
}

// TestResponse6WithNoModulusSourceIsUnchanged pins that every local-engine tool and test,
// which configures no ModulusSource, behaves exactly as it did before this change.
func TestResponse6WithNoModulusSourceIsUnchanged(t *testing.T) {
	m := localLoginManager(t)
	var calls atomic.Int32
	m.connectLogin = func(client.Config) (*client.Session, error) {
		calls.Add(1)
		return nil, &client.LoginRejectedError{Response: 6}
	}
	m.retryWait = func(<-chan struct{}, time.Duration) bool { return true }
	m.Connect("test_local_outofdate", "fixture_password")
	st := waitFleetStatus(t, m, func(st AccountStatus) bool { return !st.Reconnecting })
	if st.Connected {
		t.Fatalf("status=%+v", st)
	}
	time.Sleep(20 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Fatalf("%d login attempts, want exactly 1", n)
	}
}
