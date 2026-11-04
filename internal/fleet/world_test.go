package fleet

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/client"
	"rsb0t-go/internal/scripts/worldhopplan"
	"rsb0t-go/internal/worlds"
)

// These are white-box tests (package fleet) for the same reason fleet_test.go is: the thing
// under test is which URL a login DIALS, and the only way to see that without a real server
// is to intercept m.connectLogin, which is unexported.
//
// What they assert, in one line each, because "per-account world" is easy to half-implement:
//   - an account that names a world dials that world's URL;
//   - an account that names none dials the fleet's, byte for byte;
//   - a RECONNECT dials the account's own world, not the fleet's — the one that would
//     otherwise silently drag a world-2 account back to world 1 after every drop;
//   - a hop is refused on a deployment with one world;
//   - a hop that cannot log in anywhere lands the account rather than flapping.

// dialRecorder captures every WSURL a Manager's login path actually used.
type dialRecorder struct {
	mu   sync.Mutex
	urls []string
	err  error
}

func (d *dialRecorder) install(m *Manager) {
	m.connectLogin = func(cfg client.Config) (*client.Session, error) {
		d.mu.Lock()
		d.urls = append(d.urls, cfg.WSURL)
		err := d.err
		d.mu.Unlock()
		if err == nil {
			// A nil session with a nil error would make the caller start a read loop on
			// nothing, so every test here supplies an error; this branch exists only so a
			// future test can opt out deliberately.
			return nil, fmt.Errorf("fleet test: no session fixture")
		}
		return nil, err
	}
}

func (d *dialRecorder) seen() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.urls...)
}

func (d *dialRecorder) setErr(err error) {
	d.mu.Lock()
	d.err = err
	d.mu.Unlock()
}

func twoWorldManager(t *testing.T) (*Manager, *dialRecorder) {
	t.Helper()
	wm, err := worlds.New("wss://w1.example.test/", "https://w1.example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Config{WSURL: "wss://w1.example.test/", HTTPBase: "https://w1.example.test/", Worlds: wm})
	t.Cleanup(m.CloseAll)
	var d dialRecorder
	d.setErr(fmt.Errorf("fleet test: login refused"))
	d.install(m)
	return m, &d
}

func TestAnAccountThatNamesNoWorldDialsTheFleetURLUnchanged(t *testing.T) {
	m, d := twoWorldManager(t)
	a := &account{username: "plain", chat: chatlog.New(), activity: chatlog.New()}
	m.accounts = append(m.accounts, a)
	_ = m.loginOnce(a)

	got := d.seen()
	if len(got) != 1 || got[0] != "wss://w1.example.test/" {
		t.Fatalf("dialled %v, want the fleet's own wsURL exactly once", got)
	}
}

func TestAnAccountThatNamesAWorldDialsThatWorld(t *testing.T) {
	m, d := twoWorldManager(t)
	a := &account{username: "w2acct", chat: chatlog.New(), activity: chatlog.New()}
	a.world = 2
	m.accounts = append(m.accounts, a)
	_ = m.loginOnce(a)

	got := d.seen()
	if len(got) != 1 || got[0] != "wss://w2.example.test/" {
		t.Fatalf("dialled %v, want wss://w2.example.test/", got)
	}
}

// THE RECONNECT CASE. loginOnce is the single site that builds a session, and every path —
// the first connect, an operator's ConnectExisting, reconnectLoop's automatic retry after a
// drop, and a world hop — goes through it. This asserts that property directly by calling it
// repeatedly, which is what a retry loop does: the account must come back on ITS world every
// time, not drift to the fleet default.
func TestEveryRetryDialsTheAccountsOwnWorld(t *testing.T) {
	m, d := twoWorldManager(t)
	a := &account{username: "w2acct", chat: chatlog.New(), activity: chatlog.New()}
	a.world = 2
	m.accounts = append(m.accounts, a)
	for i := 0; i < 4; i++ {
		_ = m.loginOnce(a)
	}
	for i, u := range d.seen() {
		if u != "wss://w2.example.test/" {
			t.Fatalf("attempt %d dialled %q, want wss://w2.example.test/ — a retry drifted to the fleet default", i+1, u)
		}
	}
}

// ConnectWorld is the roster path, and the world it records has to survive into the dial.
func TestConnectWorldRecordsAndDialsTheWorld(t *testing.T) {
	m, d := twoWorldManager(t)
	m.ConnectWorld("rosteracct", "pw", 2)

	if got := m.WorldOf("rosteracct"); got != 2 {
		t.Fatalf("WorldOf = %d, want 2", got)
	}
	if got := m.HomeWorldOf("rosteracct"); got != 2 {
		t.Fatalf("HomeWorldOf = %d, want 2 — the roster's world is the account's home", got)
	}
	seen := d.seen()
	if len(seen) == 0 || seen[0] != "wss://w2.example.test/" {
		t.Fatalf("dialled %v, want wss://w2.example.test/", seen)
	}
	// And it is visible to the panel.
	st := m.Snapshot()[0]
	if st.World != 2 || st.HomeWorld != 2 {
		t.Fatalf("Snapshot world = %d home = %d, want 2/2", st.World, st.HomeWorld)
	}
}

// An account naming no world still reports a real world to the panel, never 0, so a map can
// group by world without special-casing.
func TestSnapshotNeverReportsWorldZero(t *testing.T) {
	m, _ := twoWorldManager(t)
	m.accounts = append(m.accounts, &account{username: "plain", chat: chatlog.New(), activity: chatlog.New()})
	st := m.Snapshot()[0]
	if st.World != 1 || st.HomeWorld != 1 {
		t.Fatalf("world = %d home = %d, want the fleet's own world 1 for an account that named none", st.World, st.HomeWorld)
	}
}

func TestConnectWorldRejectsAnImpossibleWorld(t *testing.T) {
	m, d := twoWorldManager(t)
	m.ConnectWorld("bad", "pw", worlds.Max+1)
	if len(d.seen()) != 0 {
		t.Fatal("an out-of-range world must not be dialled at all, let alone on the fleet default")
	}
	st := m.Snapshot()[0]
	if st.Error == "" {
		t.Fatal("an out-of-range world must be visible as an error, not silently ignored")
	}
}

func TestWorldCounts(t *testing.T) {
	m, _ := twoWorldManager(t)
	m.accounts = append(m.accounts,
		&account{username: "a", chat: chatlog.New(), activity: chatlog.New()},
		&account{username: "b", chat: chatlog.New(), activity: chatlog.New(), hopState: hopState{world: 2}},
		&account{username: "c", chat: chatlog.New(), activity: chatlog.New(), hopState: hopState{world: 2}},
	)
	got := m.WorldCounts()
	if got[1] != 1 || got[2] != 2 {
		t.Fatalf("WorldCounts = %v, want 1 on world 1 and 2 on world 2", got)
	}
}

// A local engine has one world, and a hop there would be a logout/login on the identical
// URL. Refused, so a single-world cycle can never be reported as a two-world hop.
func TestHopIsRefusedOnASingleWorldDeployment(t *testing.T) {
	m := New(Config{WSURL: "ws://localhost/", HTTPBase: "http://localhost/"})
	t.Cleanup(m.CloseAll)
	m.accounts = append(m.accounts, &account{username: "local", chat: chatlog.New(), activity: chatlog.New()})
	if m.WorldsAreDistinct() {
		t.Fatal("ws://localhost/ must not be reported as a multi-world deployment")
	}
	if err := m.HopWorld("local", 2, "test"); err != ErrOneWorld {
		t.Fatalf("HopWorld = %v, want ErrOneWorld", err)
	}
}

// Asking to hop to the world the account is already on is a no-op, not an error, so a script
// can ask unconditionally.
func TestHopToTheCurrentWorldIsANoOp(t *testing.T) {
	m, d := twoWorldManager(t)
	a := &account{username: "w2acct", chat: chatlog.New(), activity: chatlog.New()}
	a.world = 2
	m.accounts = append(m.accounts, a)
	if err := m.HopWorld("w2acct", 2, "test"); err != nil {
		t.Fatalf("HopWorld to the current world = %v, want nil", err)
	}
	// Nothing was dialled, so no login budget was spent.
	if len(d.seen()) != 0 {
		t.Fatalf("a no-op hop dialled %v", d.seen())
	}
}

// THE LIVELOCK TEST. Every login fails, so the hop can reach neither world. It must stop
// trying — bounded by the plan's attempt/hop limits — and leave the account for the ordinary
// reconnect path rather than logging out and in forever.
func TestAHopThatCanReachNoWorldIsBoundedAndLandsTheAccount(t *testing.T) {
	m, d := twoWorldManager(t)
	d.setErr(fmt.Errorf("fleet test: every login refused"))

	a := &account{username: "stuck", chat: chatlog.New(), activity: chatlog.New()}
	a.world, a.hop.Home, a.hop.Current = 1, 1, 1
	m.accounts = append(m.accounts, a)

	if err := m.HopWorld("stuck", 2, "test"); err != nil {
		t.Fatalf("HopWorld = %v", err)
	}

	// The hop must finish on its own. The plan's bounds are MaxAttempts per leg with
	// RetryWait between, so give it generous room and then assert it is DONE, not that it
	// happened to be idle at one instant.
	deadline := time.Now().Add(20 * time.Second)
	for {
		a.mu.RLock()
		hopping := a.hopping
		a.mu.RUnlock()
		if !hopping {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the hop never finished: it is still running after 20s, which is the flapping this is built to stop")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Bounded: a handful of attempts, not an unbounded stream. MaxAttempts per leg over at
	// most two legs, plus the initial connect attempt this test never made.
	if n := len(d.seen()); n == 0 || n > 2*worldhopplan.DefaultLimits.MaxAttempts+2 {
		t.Fatalf("the hop made %d login attempts; want between 1 and %d", n, 2*worldhopplan.DefaultLimits.MaxAttempts+2)
	}
	if m.HopLog("stuck") == "" {
		t.Fatal("a finished hop must say what it decided")
	}
	// And the account is not left parked: the deliberate-disconnect flag that stops
	// reconnectLoop must be clear, so ordinary recovery owns it again.
	a.mu.RLock()
	parked := a.deliberateDisconnect
	a.mu.RUnlock()
	if parked {
		t.Fatal("the account was left flagged as deliberately disconnected, so nothing will ever reconnect it")
	}
}

// Response 5 is the one outcome that must NOT be retried into. classifyHopError is the
// boundary between the live layer and the plan, so it is worth pinning directly.
func TestClassifyHopError(t *testing.T) {
	cases := []struct {
		err  error
		want worldhopplan.Outcome
	}{
		{&client.LoginRejectedError{Response: client.AlreadyLoggedInResponse}, worldhopplan.OutcomeAlreadyLoggedIn},
		{&client.LoginRejectedError{Response: connectionCeilingResponse}, worldhopplan.OutcomeCeiling},
		{&client.LoginRejectedError{Response: 3}, worldhopplan.OutcomeTerminal}, // bad password
		{fmt.Errorf("fleet test: something odd"), worldhopplan.OutcomeTerminal},
	}
	for _, c := range cases {
		if got, _ := classifyHopError(c.err); got != c.want {
			t.Errorf("classifyHopError(%v) = %s, want %s", c.err, got, c.want)
		}
	}
}

// The two responses multi-world operation adds, and the one that carries a number.
func TestClassifyHopErrorHandlesAFullWorldAndTheHopTimer(t *testing.T) {
	if got, _ := classifyHopError(&client.LoginRejectedError{Response: client.WorldFullResponse}); got != worldhopplan.OutcomeWorldFull {
		t.Errorf("a full world classified as %s, want %s", got, worldhopplan.OutcomeWorldFull)
	}
	got, wait := classifyHopError(&client.LoginRejectedError{Response: client.HopTimerResponse, RetrySeconds: 31})
	if got != worldhopplan.OutcomeHopTimer {
		t.Errorf("the hop timer classified as %s, want %s", got, worldhopplan.OutcomeHopTimer)
	}
	if wait != 31*time.Second {
		t.Errorf("wait = %s, want the 31 seconds the SERVER stated, not a default", wait)
	}
	// Without the payload byte there is no number, and the plan falls back to its own.
	if _, wait := classifyHopError(&client.LoginRejectedError{Response: client.HopTimerResponse}); wait != 0 {
		t.Errorf("wait = %s, want 0 so the plan uses DefaultHopTimer", wait)
	}
}

// The hop timer is the NORMAL answer to a hop inside the server's window. Before
// 2026-09-22 it fell through retryableLoginError's default and was terminal, which would
// have retired an account permanently for hopping a few seconds early.
func TestTheHopTimerIsRetryableNotTerminal(t *testing.T) {
	if !retryableLoginError(&client.LoginRejectedError{Response: client.HopTimerResponse}) {
		t.Fatal("response 21 must be retryable: it is what the server says to a normal world hop")
	}
}

// ---------------------------------------------------------------------------
// Full-world failover, end to end through the Manager
// ---------------------------------------------------------------------------

// threeWorldManager is a fleet that knows about w1/w2/w3, the shape the live roster needs.
func threeWorldManager(t *testing.T) (*Manager, *dialRecorder) {
	t.Helper()
	wm, err := worlds.New("wss://w1.example.test/", "https://w1.example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := wm.List([]int{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	m := New(Config{WSURL: "wss://w1.example.test/", HTTPBase: "https://w1.example.test/", Worlds: wm})
	t.Cleanup(m.CloseAll)
	var d dialRecorder
	d.install(m)
	return m, &d
}

// THE LIVE INCIDENT. World 1 answers "full" (response 7); the account must end up dialling a
// different world, not knocking on world 1 again.
func TestAFullWorldMovesTheAccountToAnotherWorld(t *testing.T) {
	m, d := threeWorldManager(t)
	d.setErr(&client.LoginRejectedError{Response: client.WorldFullResponse})

	a := &account{username: "shutout", chat: chatlog.New(), activity: chatlog.New()}
	a.world, a.hop.Home, a.hop.Current = 1, 1, 1
	m.accounts = append(m.accounts, a)

	_ = m.loginOnce(a) // the refusal that records world 1 as full
	if full := m.FullWorlds(); full[1].IsZero() {
		t.Fatal("a response 7 must record that world as full, so the other accounts skip it")
	}
	if !m.failoverToOpenWorld(a) {
		t.Fatal("two other worlds are configured; the account must move")
	}
	if got := m.WorldOf("shutout"); got == 1 {
		t.Fatal("the account is still on the world that refused it")
	}
	// RE-HOMED, which is the part that is easy to miss: without it donorrunner's hop-home
	// would push the account straight back into the full world after every hand-off.
	if m.HomeWorldOf("shutout") != m.WorldOf("shutout") {
		t.Fatalf("home %d != current %d — the account will be sent back to the full world",
			m.HomeWorldOf("shutout"), m.WorldOf("shutout"))
	}
	// And the next login actually dials the new world.
	_ = m.loginOnce(a)
	seen := d.seen()
	last := seen[len(seen)-1]
	if last == "wss://w1.example.test/" {
		t.Fatalf("after failover the account dialled %q again", last)
	}
}

// A successful login is the most direct evidence a world has room, and must clear the mark —
// otherwise one busy minute would take a world out of service for the whole TTL.
func TestASuccessfulLoginClearsTheFullMark(t *testing.T) {
	m, _ := threeWorldManager(t)
	a := &account{username: "acct", chat: chatlog.New(), activity: chatlog.New()}
	a.world = 2
	m.accounts = append(m.accounts, a)

	m.noteWorldOutcome(a, &client.LoginRejectedError{Response: client.WorldFullResponse})
	if m.FullWorlds()[2].IsZero() {
		t.Fatal("world 2 should be marked full")
	}
	m.noteWorldOutcome(a, nil)
	if _, still := m.FullWorlds()[2]; still {
		t.Fatal("a successful login on world 2 must clear its full mark")
	}
}

// With one configured world there is nowhere to go, and nothing must change: this is every
// roster that has not been told about w2/w3, and it must behave exactly as it always did.
func TestFailoverIsANoOpWithOneConfiguredWorld(t *testing.T) {
	m, _ := twoWorldManager(t) // derivable host, but no worldList: Available() is just {1}
	a := &account{username: "solo", chat: chatlog.New(), activity: chatlog.New()}
	a.world, a.hop.Home = 1, 1
	m.accounts = append(m.accounts, a)
	if m.failoverToOpenWorld(a) {
		t.Fatal("there is only one configured world; nothing should have moved")
	}
	if m.WorldOf("solo") != 1 {
		t.Fatalf("the account moved to world %d out of thin air", m.WorldOf("solo"))
	}
}

// Every world full: wait, do not invent a destination.
func TestFailoverWaitsWhenEveryWorldIsFull(t *testing.T) {
	m, _ := threeWorldManager(t)
	a := &account{username: "acct", chat: chatlog.New(), activity: chatlog.New()}
	a.world, a.hop.Home = 1, 1
	m.accounts = append(m.accounts, a)
	now := time.Now()
	for _, w := range []int{1, 2, 3} {
		m.fullWorlds().note(w, now)
	}
	if m.failoverToOpenWorld(a) {
		t.Fatal("every world is full; the account must wait rather than move somewhere full")
	}
	if m.WorldOf("acct") != 1 {
		t.Fatalf("the account moved to world %d with nowhere open", m.WorldOf("acct"))
	}
}

// Response 7 must never be read as a rate verdict: doing so would halt every login
// fleet-wide because one world is busy. Measured live: four routes stayed healthy through
// 768 of these.
func TestAFullWorldIsNotARateRejection(t *testing.T) {
	err := &client.LoginRejectedError{Response: client.WorldFullResponse}
	if isLoginRateRejection(err) {
		t.Fatal("response 7 must not trip the fleet-wide quiet window")
	}
	if isConnectionCeilingRejection(err) {
		t.Fatal("response 7 is not the per-address connection ceiling")
	}
	if !isWorldFullRejection(err) {
		t.Fatal("response 7 must be recognised as a full world")
	}
	if !retryableLoginError(err) {
		t.Fatal("response 7 must stay retryable: the account is fine, the world is busy")
	}
}

// The whole fleet must not stampede onto one replacement world.
func TestAccountsSpreadAcrossTheReplacementWorlds(t *testing.T) {
	m, _ := threeWorldManager(t)
	landed := map[int]int{}
	for i := 0; i < 120; i++ {
		a := &account{username: fmt.Sprintf("acct_%d", i), chat: chatlog.New(), activity: chatlog.New()}
		a.world, a.hop.Home = 1, 1
		m.accounts = append(m.accounts, a)
		if !m.failoverToOpenWorld(a) {
			t.Fatal("worlds 2 and 3 have room")
		}
		landed[m.worldOf(a)]++
	}
	if len(landed) != 2 {
		t.Fatalf("120 shut-out accounts landed on %v, want both worlds 2 and 3", landed)
	}
	for w, n := range landed {
		if n < 30 {
			t.Errorf("world %d took only %d of 120 (%v) — that is a stampede, not a spread", w, n, landed)
		}
	}
}
