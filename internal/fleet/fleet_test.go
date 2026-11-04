package fleet

import (
	"math/big"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/client"
)

// This file is a white-box test (package fleet, not fleet_test) specifically so it can
// build *account values directly and append them to a Manager's own accounts slice —
// the only way to exercise Snapshot()'s status-mapping logic without a live
// *client.Session, which Connect() can only produce via a real network dial/login.
// Per COLLAB.md's task description, that live half (Connect, and Snapshot's
// session!=nil branch) is not unit-tested here: internal/client.Session has no
// exported constructor and its `state`/other fields are unexported, so there is no way
// to build a working fake one from outside internal/client, and internal/client itself
// is off-limits for this pass (another collaborator, AJ "controlpanel", is actively
// changing session.go's keepalive/reconnect behavior). The session==nil branch below
// (a failed or not-yet-connected account) is genuinely testable and is real production
// behavior — the dashboard shows exactly these accounts while a fleet is still logging
// in or after a login failure.

func TestNewStoresConfig(t *testing.T) {
	mod := big.NewInt(12345)
	exp := big.NewInt(65537)
	m := New(Config{WSURL: "wss://example/ws", HTTPBase: "https://example/", Modulus: mod, Exponent: exp})
	if m.cfg.WSURL != "wss://example/ws" || m.cfg.HTTPBase != "https://example/" {
		t.Fatalf("New() did not retain its Config: %+v", m.cfg)
	}
	if m.cfg.Modulus != mod || m.cfg.Exponent != exp {
		t.Fatalf("New() did not retain Modulus/Exponent pointers unchanged")
	}
}

func TestSnapshotEmptyManager(t *testing.T) {
	m := New(Config{})
	got := m.Snapshot()
	if len(got) != 0 {
		t.Fatalf("Snapshot() on a fresh Manager = %+v, want empty", got)
	}
}

func TestSnapshotReportsUnconnectedAndFailedAccountsWithoutASession(t *testing.T) {
	m := New(Config{})
	m.accounts = append(m.accounts,
		&account{username: "queued"},
		&account{username: "failed_login", lastErr: "bad password"},
		&account{username: "dropped", disconnected: true, disconnectMsg: "websocket: close 1005", packets: 42},
	)

	got := m.Snapshot()
	if len(got) != 3 {
		t.Fatalf("Snapshot() returned %d entries, want 3", len(got))
	}

	// Order must match Connect()'s append order (accounts is never re-sorted), which
	// is what lets the dashboard show a stable row per account across refreshes.
	queued, failed, dropped := got[0], got[1], got[2]

	for name, st := range map[string]AccountStatus{"queued": queued, "failed_login": failed, "dropped": dropped} {
		if st.Connected {
			t.Errorf("%s: Connected = true, want false (no session)", name)
		}
		if st.MainModal != -1 || st.SideModal != -1 {
			t.Errorf("%s: MainModal/SideModal = %d/%d, want both -1 (gamestate.NoModal)", name, st.MainModal, st.SideModal)
		}
		if st.SelfSlot != -1 {
			t.Errorf("%s: SelfSlot = %d, want -1", name, st.SelfSlot)
		}
		if st.Inventories != nil {
			t.Errorf("%s: Inventories = %+v, want nil (no session to read from)", name, st.Inventories)
		}
		if st.ConnectedSecs != 0 {
			t.Errorf("%s: ConnectedSecs = %v, want 0 (never connected)", name, st.ConnectedSecs)
		}
	}

	if queued.Username != "queued" || queued.Error != "" || queued.Disconnected {
		t.Errorf("queued account status = %+v", queued)
	}
	if failed.Error != "bad password" {
		t.Errorf("failed_login: Error = %q, want %q", failed.Error, "bad password")
	}
	if !dropped.Disconnected || dropped.DisconnectMsg != "websocket: close 1005" || dropped.Packets != 42 {
		t.Errorf("dropped account status = %+v", dropped)
	}
}

// TestSnapshotReportsRecentChatAndBankUpdatedAtWithoutASession covers the two new
// AccountStatus panel-facing fields (see the chat feed / bank viewer panels) that don't
// need a live session to test: RecentChat/ChatCount come straight from account.chat
// (chatlog.Log), and BankUpdatedAt is a plain time.Time formatted as RFC3339.
// BankItems/HP/MaxHP/Dead all require a real *client.Session (see this file's own
// package doc comment for why that's out of scope for a white-box unit test here) and
// are covered instead by manual live verification (see COLLAB.md).
func TestSnapshotReportsRecentChatAndBankUpdatedAtWithoutASession(t *testing.T) {
	m := New(Config{})
	chat := chatlog.New()
	chat.Record("Would you like to trade with Bob?")
	chat.Record("Nice bot")
	bankAt := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	m.accounts = append(m.accounts, &account{username: "chatty", chat: chat, activity: chatlog.New(), bankUpdatedAt: bankAt})

	got := m.Snapshot()
	if len(got) != 1 {
		t.Fatalf("Snapshot() = %+v", got)
	}
	s := got[0]
	if len(s.RecentChat) != 2 || s.RecentChat[0] != "Would you like to trade with Bob?" || s.RecentChat[1] != "Nice bot" {
		t.Errorf("RecentChat = %v", s.RecentChat)
	}
	if s.ChatCount != 2 {
		t.Errorf("ChatCount = %d, want 2", s.ChatCount)
	}
	if s.BankUpdatedAt != bankAt.Format(time.RFC3339) {
		t.Errorf("BankUpdatedAt = %q, want %q", s.BankUpdatedAt, bankAt.Format(time.RFC3339))
	}
}

// TestSnapshotOmitsBankUpdatedAtWhenTheBankWasNeverOpened checks the zero-value path:
// an account whose bank has never been open this session should report no
// BankUpdatedAt at all, not a bogus formatted zero time.
func TestSnapshotOmitsBankUpdatedAtWhenTheBankWasNeverOpened(t *testing.T) {
	m := New(Config{})
	m.accounts = append(m.accounts, &account{username: "neverbanked", chat: chatlog.New(), activity: chatlog.New()})
	got := m.Snapshot()
	if len(got) != 1 || got[0].BankUpdatedAt != "" {
		t.Fatalf("BankUpdatedAt = %+v, want empty", got)
	}
}

func TestCloseAllOnAManagerWithNoSessionsDoesNotPanic(t *testing.T) {
	m := New(Config{})
	m.accounts = append(m.accounts, &account{username: "never_connected"})
	// CloseAll must tolerate accounts whose session was never set (Connect failed
	// before assigning one) — it should just skip them, not dereference a nil session.
	m.CloseAll()
}

func TestSnapshotReportsAReconnectingAccount(t *testing.T) {
	m := New(Config{})
	m.accounts = append(m.accounts, &account{
		username: "flaky", disconnected: true, disconnectMsg: "websocket: close 1006",
		reconnecting: true, reconnectAttempt: 3,
	})

	got := m.Snapshot()
	if len(got) != 1 {
		t.Fatalf("Snapshot() returned %d entries, want 1", len(got))
	}
	st := got[0]
	if st.Connected {
		t.Error("Connected = true, want false while mid-reconnect (session is nil)")
	}
	if !st.Reconnecting {
		t.Error("Reconnecting = false, want true")
	}
	if st.ReconnectAttempt != 3 {
		t.Errorf("ReconnectAttempt = %d, want 3", st.ReconnectAttempt)
	}
}

// This block tests the fleetmgmt additions: Disconnect/ConnectExisting/AddAccount and the
// deliberateDisconnect interaction with reconnectLoop. As with the rest of this file, the
// live half of each (an actual client.Connect/Session.Logout network round-trip) is not
// exercised here — only the pure account-bookkeeping logic that decides whether to call
// into that live half at all. A zero-value &client.Session{} stands in for "this account
// has a session" wherever a test only needs Disconnect/ConnectExisting to see
// session != nil and take the right branch without ever calling a method on it (calling one
// on a genuinely zero-value Session, e.g. Logout, would panic on its nil stream — none of
// these tests do that).

func TestDisconnectUnknownAccount(t *testing.T) {
	m := New(Config{})
	if err := m.Disconnect("ghost"); err == nil {
		t.Error("Disconnect on an unknown account: want an error, got nil")
	}
}

func TestDisconnectAlreadyDisconnectedLeavesNoStuckFlag(t *testing.T) {
	m := New(Config{})
	a := &account{username: "offline"}
	m.accounts = append(m.accounts, a)

	if err := m.Disconnect("offline"); err == nil {
		t.Error("Disconnect on an account with no session and not reconnecting: want an error, got nil")
	}
	a.mu.RLock()
	deliberate := a.deliberateDisconnect
	a.mu.RUnlock()
	if deliberate {
		t.Error("deliberateDisconnect left true after a no-op Disconnect — would block a later ConnectExisting/reconnect for no reason")
	}
}

func TestDisconnectMidReconnectCancelsWithoutError(t *testing.T) {
	m := New(Config{})
	a := &account{username: "flaky", reconnecting: true}
	m.accounts = append(m.accounts, a)

	if err := m.Disconnect("flaky"); err != nil {
		t.Errorf("Disconnect mid-reconnect: got error %v, want nil (there is nothing to log out yet, just cancel)", err)
	}
	a.mu.RLock()
	deliberate := a.deliberateDisconnect
	a.mu.RUnlock()
	if !deliberate {
		t.Error("deliberateDisconnect not set — reconnectLoop would keep retrying")
	}
}

func TestConnectExistingUnknownAccount(t *testing.T) {
	m := New(Config{})
	if err := m.ConnectExisting("ghost"); err == nil {
		t.Error("ConnectExisting on an unknown account: want an error, got nil")
	}
}

func TestConnectExistingRefusesAnAlreadyConnectedAccount(t *testing.T) {
	m := New(Config{})
	a := &account{username: "online", session: &client.Session{}}
	m.accounts = append(m.accounts, a)
	if err := m.ConnectExisting("online"); err == nil {
		t.Error("ConnectExisting on an already-connected account: want an error, got nil")
	}
}

func TestConnectExistingWakesAnAccountAlreadyReconnecting(t *testing.T) {
	m := New(Config{})
	a := &account{username: "flaky", reconnecting: true}
	m.accounts = append(m.accounts, a)
	if err := m.ConnectExisting("flaky"); err != nil {
		t.Fatal(err)
	}
	if len(a.retryWake) != 1 {
		t.Fatal("retry owner was not woken")
	}
	if err := m.ConnectExisting("flaky"); err != nil {
		t.Fatal(err)
	}
	if len(a.retryWake) != 1 {
		t.Fatal("duplicate wake was not coalesced")
	}
}

func TestAddAccountRefusesADuplicateUsername(t *testing.T) {
	m := New(Config{})
	m.accounts = append(m.accounts, &account{username: "dupe"})
	if err := m.AddAccount("dupe", "pw", "", nil); err == nil {
		t.Error("AddAccount with a username already in the fleet: want an error, got nil")
	}
	// Must not have appended a second entry for the same name before the network call
	// it would otherwise attempt.
	if len(m.accounts) != 1 {
		t.Errorf("accounts = %d, want 1 (no duplicate appended)", len(m.accounts))
	}
}

func TestSnapshotReportsManuallyDisconnected(t *testing.T) {
	m := New(Config{})
	m.accounts = append(m.accounts, &account{username: "loggedout", disconnected: true, deliberateDisconnect: true})
	got := m.Snapshot()
	if len(got) != 1 || !got[0].ManuallyDisconnected {
		t.Fatalf("Snapshot()[0].ManuallyDisconnected = %v, want true: %+v", got[0].ManuallyDisconnected, got)
	}
}

// TestReconnectLoopCancelsWhenDeliberatelyDisconnected is the direct test of the race this
// task called out explicitly: an account whose deliberateDisconnect flag is already set
// when reconnectLoop is (re)entered must give up immediately rather than retry — this is
// the exact scenario of an operator disconnecting an account that was already mid-
// automatic-reconnect from an earlier unexpected drop.
func TestReconnectLoopCancelsWhenDeliberatelyDisconnected(t *testing.T) {
	m := New(Config{})
	a := &account{username: "manual", deliberateDisconnect: true}
	m.accounts = append(m.accounts, a)

	done := make(chan struct{})
	go func() {
		m.reconnectLoop(a)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnectLoop did not return promptly when deliberateDisconnect was already set")
	}

	a.mu.RLock()
	reconnecting, attempt := a.reconnecting, a.reconnectAttempt
	a.mu.RUnlock()
	if reconnecting {
		t.Error("reconnecting left true after reconnectLoop canceled")
	}
	if attempt != 0 {
		t.Errorf("reconnectAttempt = %d, want 0 (no attempt should have been made)", attempt)
	}
}

func TestReconnectBackoffDoublesThenCaps(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 2 * time.Second}, // clamped to attempt 1, same as below
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 32 * time.Second},
		{6, 60 * time.Second}, // 64s would exceed the cap
		{7, 60 * time.Second}, // stays capped indefinitely
		{100, 60 * time.Second},
	}
	for _, c := range cases {
		if got := reconnectBackoff(c.attempt); got != c.want {
			t.Errorf("reconnectBackoff(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}
