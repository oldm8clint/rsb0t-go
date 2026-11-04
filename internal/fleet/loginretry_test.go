package fleet

import (
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"rsb0t-go/internal/bot"
	"rsb0t-go/internal/client"
	"rsb0t-go/internal/transport"
)

func TestRetryableLoginErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("login seed: %w", transport.ErrReadTimeout), true},
		{fmt.Errorf("socket: %w", io.EOF), true},
		{&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, true},
		{&client.LoginRejectedError{Response: 16}, true},
		{&client.LoginRejectedError{Response: 5}, true},
		{&client.LoginRejectedError{Response: 3}, false},
		{&client.LoginRejectedError{Response: 4}, false},
		{&client.LoginRejectedError{Response: 6}, false},
		{errors.New("invalid websocket URL"), false},
	} {
		if got := retryableLoginError(tc.err); got != tc.want {
			t.Fatalf("%v retry=%v want=%v", tc.err, got, tc.want)
		}
	}
}

var startupFixtureOnce sync.Once

type loginStartupBot struct{}

func (*loginStartupBot) OnStart(*bot.Context) error  { return nil }
func (*loginStartupBot) Loop(ctx *bot.Context) error { ctx.Delay(time.Second); return nil }
func (*loginStartupBot) OnStop(*bot.Context)         {}

func TestConfiguredStartupBotStartsAfterLoginTimeoutRecovery(t *testing.T) {
	startupFixtureOnce.Do(func() { bot.Register("LoginRetryStartupFixture", func() bot.Bot { return &loginStartupBot{} }) })
	m := localLoginManager(t)
	var calls atomic.Int32
	m.connectLogin = func(cfg client.Config) (*client.Session, error) {
		if calls.Add(1) == 1 {
			return nil, transport.ErrReadTimeout
		}
		return client.Connect(cfg)
	}
	allow := make(chan struct{})
	m.retryWait = func(done <-chan struct{}, duration time.Duration) bool {
		select {
		case <-allow:
			return true
		case <-done:
			return false
		}
	}
	m.Connect("test_startup", "fixture_password")
	started := make(chan error, 1)
	go func() {
		if !m.WaitConnected("test_startup") {
			started <- errors.New("startup wait aborted")
			return
		}
		started <- m.StartBot("test_startup", "LoginRetryStartupFixture", map[string]interface{}{"setting": "preserved"})
	}()
	select {
	case err := <-started:
		t.Fatalf("startup acted before recovery: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(allow)
	select {
	case err := <-started:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("startup bot never started")
	}
	st := m.Snapshot()[0]
	if st.Script != "LoginRetryStartupFixture" || st.BotState != "running" || st.Settings["setting"] != "preserved" {
		t.Fatalf("startup state=%+v", st)
	}
	if err := m.StopBot("test_startup"); err != nil {
		t.Fatal(err)
	}
}

func TestStartupWaitEndsOnTerminalFailureOrDisconnect(t *testing.T) {
	m := localLoginManager(t)
	m.accounts = append(m.accounts, &account{username: "test_terminal", lastErr: "bad credentials"})
	if m.WaitConnected("test_terminal") {
		t.Fatal("terminal login treated as connected")
	}
	if m.WaitConnected("unknown") {
		t.Fatal("unknown account treated as connected")
	}
	a := m.accounts[0]
	a.lastErr = ""
	a.deliberateDisconnect = true
	if m.WaitConnected("test_terminal") {
		t.Fatal("deliberate disconnect ignored")
	}
}

// Complete a real login handshake on loopback. No credentials leave this test
// server; it accepts the packet without decoding the RSA block and stays open.
func localLoginManager(t *testing.T) *Manager {
	t.Helper()
	upgrader := websocket.Upgrader{Subprotocols: []string{"binary"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
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
	m := New(Config{WSURL: "ws" + strings.TrimPrefix(server.URL, "http"), Checksums: &[9]int32{}, Modulus: big.NewInt(9999991), Exponent: big.NewInt(65537), NoAntiCheat: true})
	t.Cleanup(func() { m.CloseAll(); server.Close() })
	return m
}

func waitFleetStatus(t *testing.T, m *Manager, ready func(AccountStatus) bool) AccountStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		states := m.Snapshot()
		if len(states) == 1 && ready(states[0]) {
			return states[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("fleet did not reach expected status: %+v", m.Snapshot())
	return AccountStatus{}
}

func TestFirstLoginTimeoutRecoversAutomatically(t *testing.T) {
	m := localLoginManager(t)
	var calls atomic.Int32
	var waits atomic.Int32
	m.connectLogin = func(cfg client.Config) (*client.Session, error) {
		if calls.Add(1) == 1 {
			return nil, fmt.Errorf("login reading seed: %w", transport.ErrReadTimeout)
		}
		return client.Connect(cfg)
	}
	m.retryWait = func(done <-chan struct{}, duration time.Duration) bool {
		waits.Add(1)
		if duration < 1400*time.Millisecond || duration > 2600*time.Millisecond {
			t.Errorf("first backoff %v outside jittered2s", duration)
		}
		return true
	}
	m.Connect("test_timeout", "fixture_password")
	st := waitFleetStatus(t, m, func(st AccountStatus) bool { return st.Connected && !st.Reconnecting })
	if calls.Load() != 2 || waits.Load() != 1 || st.Error != "" || st.Disconnected || st.Connecting || st.ReconnectAttempt != 0 {
		t.Fatalf("calls=%d waits=%d status=%+v", calls.Load(), waits.Load(), st)
	}
}

func TestManualReconnectFromCompletedErrorRetriesTimeoutAndClearsState(t *testing.T) {
	m := localLoginManager(t)
	var calls atomic.Int32
	m.connectLogin = func(cfg client.Config) (*client.Session, error) {
		if calls.Add(1) == 1 {
			return nil, &client.LoginRejectedError{Response: 3}
		}
		if calls.Load() == 2 {
			return nil, transport.ErrReadTimeout
		}
		return client.Connect(cfg)
	}
	m.Connect("test_manual", "fixture_password")
	a := m.find("test_manual")
	a.mu.Lock()
	a.lastErr = transport.ErrReadTimeout.Error()
	a.disconnected = true
	a.disconnectMsg = "old closed socket"
	a.deliberateDisconnect = true
	a.mu.Unlock()
	allow := make(chan struct{})
	m.retryWait = func(done <-chan struct{}, duration time.Duration) bool {
		select {
		case <-allow:
			return true
		case <-done:
			return false
		}
	}
	start := time.Now()
	if err := m.ConnectExisting("test_manual"); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("API acceptance waited for a login attempt")
	}
	if err := m.ConnectExisting("test_manual"); err != nil {
		t.Fatalf("existing retry owner should accept a wake: %v", err)
	}
	if !m.Snapshot()[0].Reconnecting {
		t.Fatal("accepted reconnect not observable immediately")
	}
	close(allow)
	st := waitFleetStatus(t, m, func(st AccountStatus) bool { return st.Connected && !st.Reconnecting })
	if calls.Load() != 3 || st.Error != "" || st.DisconnectMsg != "" || st.ManuallyDisconnected || st.Connecting || st.Disconnected {
		t.Fatalf("calls=%d status=%+v", calls.Load(), st)
	}
}

func TestReconnectStopsOnPermanentCredentialFailure(t *testing.T) {
	m := localLoginManager(t)
	var calls atomic.Int32
	m.connectLogin = func(client.Config) (*client.Session, error) {
		if calls.Add(1) == 1 {
			return nil, transport.ErrReadTimeout
		}
		return nil, &client.LoginRejectedError{Response: 3}
	}
	m.retryWait = func(<-chan struct{}, time.Duration) bool { return true }
	m.Connect("test_bad_credentials", "fixture_password")
	st := waitFleetStatus(t, m, func(st AccountStatus) bool { return !st.Reconnecting && strings.Contains(st.Error, "response 3") })
	if calls.Load() != 2 || st.Connected {
		t.Fatalf("calls=%d status=%+v", calls.Load(), st)
	}
}

func TestManualReconnectHonoursGlobalPacingAndDisconnectCancellation(t *testing.T) {
	m := localLoginManager(t)
	// An empty gate simulates a fleet-wide backlog; accepting the API request
	// must not bypass the gate or prevent an operator from cancelling it.
	m.loginRate.Close()
	m.loginRate = &loginRateGate{tokens: make(chan struct{}), stop: make(chan struct{})}
	var calls atomic.Int32
	m.connectLogin = func(client.Config) (*client.Session, error) { calls.Add(1); return nil, transport.ErrReadTimeout }
	m.retryWait = func(<-chan struct{}, time.Duration) bool { return true }
	m.accounts = append(m.accounts, &account{username: "test_queued", lastErr: "transport timed out"})
	if err := m.ConnectExisting("test_queued"); err != nil {
		t.Fatal(err)
	}
	waitFleetStatus(t, m, func(st AccountStatus) bool { return st.Connecting })
	if calls.Load() != 0 {
		t.Fatal("reconnect bypassed global gate")
	}
	if err := m.Disconnect("test_queued"); err != nil {
		t.Fatal(err)
	}
	m.loginRate.tokens <- struct{}{}
	waitFleetStatus(t, m, func(st AccountStatus) bool { return !st.Connecting && !st.Reconnecting && st.ManuallyDisconnected })
	if calls.Load() != 0 {
		t.Fatal("cancelled queued attempt invoked login")
	}
}
