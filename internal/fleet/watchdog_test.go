package fleet

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/client"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/stallwatch"
)

// The decision itself is table-tested in internal/stallwatch; these cover the glue that must hold
// for it to mean anything: the watchdog's own lines never count as the account "saying so", an
// account with nothing to watch is left alone, and the dump is rate-limited.

func watchAccount(m *Manager, name string) *account {
	a := &account{username: name, script: "ProgressiveWCFletch", chat: chatlog.New(), activity: chatlog.New()}
	m.mu.Lock()
	m.accounts = append(m.accounts, a)
	m.mu.Unlock()
	return a
}

func TestAlogfStampsTheLastLineAndWlogfDoesNot(t *testing.T) {
	m := New(Config{})
	var printed []string
	m.Log = func(s string) { printed = append(printed, s) }
	a := watchAccount(m, "Acct188")
	if a.lastLine.Load() != 0 {
		t.Fatal("a fresh account has written nothing")
	}
	m.alogf(a, "[%s/%s] [loot] random event Mime: completed, no item reward in the pack", "Acct188", "ProgressiveWCFletch")
	stamped := a.lastLine.Load()
	if stamped == 0 {
		t.Fatal("alogf must stamp the account's last-line time")
	}
	time.Sleep(2 * time.Millisecond)
	m.wlogf(a, "STALLED: running but silent")
	if a.lastLine.Load() != stamped {
		t.Fatal("the watchdog's own line must not count as the account saying something")
	}
	if got := a.activity.Recent(5); len(got) != 2 || !strings.HasPrefix(got[1], "[Acct188/ProgressiveWCFletch] [watchdog] STALLED") {
		t.Fatalf("activity = %q; the watchdog line must still reach the account's log, tagged", got)
	}
	if len(printed) != 2 {
		t.Fatalf("printed %d lines, want both", len(printed))
	}
	if got := lastBotLine(a); !strings.Contains(got, "[loot] random event Mime") {
		t.Fatalf("lastBotLine = %q, want the loot line, not the watchdog's", got)
	}
}

func TestWatchdogOffStartsNothing(t *testing.T) {
	m := New(Config{})
	cfg := DefaultWatchdogConfig()
	cfg.Mode = stallwatch.ModeOff
	m.StartWatchdog(cfg)
	if m.watch != nil {
		t.Fatal("-watchdog off must not start a watchdog")
	}
	if m.watchStatus("anyone") != nil {
		t.Fatal("no watchdog, no status")
	}
}

// TestSweepLeavesAccountsWithNothingToWatchAlone: not connected, connected with no bot, a mule --
// a sweep reads them without a session and neither acts nor reports.
func TestSweepLeavesAccountsWithNothingToWatchAlone(t *testing.T) {
	m := New(Config{})
	var lines []string
	m.Log = func(s string) { lines = append(lines, s) }
	watchAccount(m, "offline1")
	mule := watchAccount(m, "mule")
	mule.script = "MuleReceiver"
	cfg := DefaultWatchdogConfig()
	cfg.DumpDir = ""
	m.StartWatchdog(cfg)
	defer m.CloseAll()
	w := m.watch
	start := time.Now()
	for i := 0; i < 200; i++ { // 100 simulated minutes of sweeps
		w.sweep(start.Add(time.Duration(i) * 30 * time.Second))
	}
	for _, l := range lines {
		if strings.Contains(l, "] [watchdog] ") {
			t.Fatalf("a per-account watchdog line for an account with nothing to watch: %q", l)
		}
	}
	if st := m.watchStatus("offline1"); st != nil {
		t.Fatalf("status %+v for an idle account", st)
	}
	for _, s := range m.Snapshot() {
		if s.Watchdog != nil {
			t.Fatalf("%s: Snapshot carries a watchdog status %+v", s.Username, s.Watchdog)
		}
	}
}

func TestDumpIsRateLimited(t *testing.T) {
	m := New(Config{})
	dir := t.TempDir()
	w := &watchdog{m: m, cfg: WatchdogConfig{DumpDir: dir, DumpEvery: 10 * time.Minute, MaxDumps: 2}}
	now := time.Date(2026, 9, 23, 3, 0, 0, 0, time.UTC)
	if got := w.dump(now, "Acct188"); !strings.Contains(got, "goroutine dump") || !strings.Contains(got, "Acct188") {
		t.Fatalf("first dump: %q", got)
	}
	if got := w.dump(now.Add(time.Minute), "Acct206"); got != "" {
		t.Fatalf("a second dump inside DumpEvery: %q", got)
	}
	if got := w.dump(now.Add(11*time.Minute), "Acct206"); got == "" {
		t.Fatal("a dump after DumpEvery is allowed")
	}
	if got := w.dump(now.Add(30*time.Minute), "Acct207"); got != "" {
		t.Fatalf("past MaxDumps: %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("dump files = %d (%v), want 2", len(entries), err)
	}
	body, _ := os.ReadFile(dir + string(os.PathSeparator) + entries[0].Name())
	if !strings.Contains(string(body), "goroutine ") {
		t.Fatal("the dump holds goroutine stacks")
	}
}

func TestDescribeCountsAndFileNames(t *testing.T) {
	if got := describeCounts(map[string]int{"ScarabSwarm": 10, "ProgressiveWCFletch": 2}); got != "ProgressiveWCFletch 2, ScarabSwarm 10" {
		t.Fatalf("describeCounts = %q", got)
	}
	if got := describeCounts(nil); got != "none" {
		t.Fatalf("describeCounts(nil) = %q", got)
	}
	if got := sanitizeFileName("acct 1/..\\x"); got != "acct_1____x" {
		t.Fatalf("sanitizeFileName = %q", got)
	}
}

// TestBlindOutsideEvent: blindness only counts outside an event and with the tile known, so an
// account the mime or maze is still holding, or one the Guardian is working on, is never sent
// straight to a relogin for it.
func TestBlindOutsideEvent(t *testing.T) {
	for _, c := range []struct {
		readable, tileOK, inTrap, busy, want bool
	}{
		{false, true, false, false, true},   // Acct188 after the mime: blind
		{true, true, false, false, false},   // readable
		{false, true, true, false, false},   // still in the mime/maze square
		{false, true, false, true, false},   // the Guardian is handling an event
		{false, false, false, false, false}, // no tile yet (a login still arriving)
	} {
		if got := blindOutsideEvent(c.readable, c.tileOK, c.inTrap, c.busy); got != c.want {
			t.Errorf("blindOutsideEvent(readable=%v tileOK=%v inTrap=%v busy=%v) = %v, want %v", c.readable, c.tileOK, c.inTrap, c.busy, got, c.want)
		}
	}
}

// watchSession records every packet an action would send, and sends none.
type watchSession struct {
	state  *gamestate.State
	cheats anticheat.Counters
	sent   int
}

func (s *watchSession) Send(build func(w *packet.Packet)) error {
	build(packet.NewWriter(64))
	s.sent++
	return nil
}
func (s *watchSession) State() *gamestate.State     { return s.state }
func (s *watchSession) Cheats() *anticheat.Counters { return &s.cheats }

func testWatchdog(m *Manager) *watchdog {
	cfg := DefaultWatchdogConfig()
	cfg.DumpDir = ""
	return &watchdog{m: m, cfg: cfg, mode: cfg.Mode, started: time.Now(), hist: map[string]*stallwatch.History{},
		status: map[string]WatchdogStatus{}, inFlight: map[string]bool{}, scriptBreakerAt: map[string]time.Time{},
		counts: watchdogCounts{Recovered: map[string]int{}}}
}

// TestNudgeSendsNothingWhileTheGuardianIsBusy: the nudge must not press or close anything while
// a random event is being handled, and with the Guardian idle it closes the open modal. (A
// surviving mutant in review2: dropping the Guardian-busy guard failed no test.)
func TestNudgeSendsNothingWhileTheGuardianIsBusy(t *testing.T) {
	m := New(Config{})
	var lines []string
	m.Log = func(s string) { lines = append(lines, s) }
	a := watchAccount(m, "Acct188")
	w := testWatchdog(m)
	busy := true
	w.busyFn = func(actions.Session) bool { return busy }
	s := &watchSession{state: gamestate.New()}
	s.state.MainModalID = 1234

	w.nudgeOn(a, s)
	if s.sent != 0 {
		t.Fatalf("the nudge sent %d packets while the Guardian was busy", s.sent)
	}
	if main, _, _ := s.state.Modals(); main != 1234 {
		t.Fatalf("the modal was closed while the Guardian was busy (main=%d)", main)
	}
	if len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "the Guardian is handling a random event; nothing sent") {
		t.Fatalf("lines = %q, want the Guardian-busy line", lines)
	}

	busy = false
	w.nudgeOn(a, s)
	if s.sent != 1 {
		t.Fatalf("with the Guardian idle the nudge sent %d packets, want the one CLOSE_MODAL", s.sent)
	}
	if !strings.Contains(lines[len(lines)-1], "nudge: closed the open modal") {
		t.Fatalf("last line = %q", lines[len(lines)-1])
	}
}

// TestObserveTreatsAHoppingAccountAsNotInGame: a world hop logs the account out and back in on
// purpose, so it is never watched mid-hop -- and neither is an account connecting, reconnecting
// or deliberately disconnected. (A surviving mutant in review2: dropping the hopping flag failed
// no test.)
func TestObserveTreatsAHoppingAccountAsNotInGame(t *testing.T) {
	m := New(Config{})
	a := watchAccount(m, "hopper")
	a.session = &client.Session{} // present, never used: no runner, so observe reads no state
	w := testWatchdog(m)
	if !w.observe(a).obs.InGame {
		t.Fatal("a plain live session must be in game")
	}
	for _, c := range []struct {
		name string
		set  func(bool)
	}{
		{"hopping", func(v bool) { a.hopping = v }},
		{"connecting", func(v bool) { a.connecting = v }},
		{"reconnecting", func(v bool) { a.reconnecting = v }},
		{"deliberately disconnected", func(v bool) { a.deliberateDisconnect = v }},
	} {
		c.set(true)
		if w.observe(a).obs.InGame {
			t.Errorf("%s: observed as in game", c.name)
		}
		c.set(false)
	}
	a.session = nil
	if w.observe(a).obs.InGame {
		t.Fatal("no session, not in game")
	}
	if !inGame(true, false, false, false, false) || inGame(true, false, false, true, false) {
		t.Fatal("inGame must refuse a hopping account and accept a plain one")
	}
}

// TestReloginRetriesARefusedLogin: straight after the logout the reconnect loop is often still
// winding down and ConnectExisting refuses ("finishing disconnect"); the relogin retries.
func TestReloginRetriesARefusedLogin(t *testing.T) {
	m := New(Config{})
	a := watchAccount(m, "Acct206")
	w := testWatchdog(m)
	w.connectRetry = time.Millisecond
	calls := 0
	w.connectFn = func(string) error {
		calls++
		if calls < 3 {
			return errors.New("fleet: Acct206 is finishing disconnect; retry shortly")
		}
		return nil
	}
	if !w.requeueLogin(a, "ProgressiveWCFletch") {
		t.Fatal("the third attempt queued the login")
	}
	if calls != 3 || !a.resumeScript {
		t.Fatalf("calls=%d resumeScript=%v", calls, a.resumeScript)
	}
}

// TestReloginThatCannotLogInDoesNotStrandTheAccount: a relogin whose ConnectExisting keeps
// failing used to return with the account logged out and deliberateDisconnect still set --
// offline, never retried by anything, and invisible to the watchdog as "not in game". Now the
// flag is cleared, the error is the account's last error, and it is marked needs attention.
func TestReloginThatCannotLogInDoesNotStrandTheAccount(t *testing.T) {
	m := New(Config{})
	var lines []string
	m.Log = func(s string) { lines = append(lines, s) }
	a := watchAccount(m, "Acct216")
	w := testWatchdog(m)
	w.settle, w.connectRetry, w.connectAttempts = time.Millisecond, time.Millisecond, 3
	w.hist[a.username] = &stallwatch.History{Script: a.script, Detected: true, Step: stallwatch.StepRelogged, StepAt: time.Now()}
	w.relogins = 1 // as sweep counts it before starting the goroutine
	calls := 0
	w.connectFn = func(string) error {
		calls++
		a.mu.Lock()
		a.deliberateDisconnect = true // what Disconnect leaves behind while the old loop winds down
		a.mu.Unlock()
		return errors.New("fleet: Acct216 is finishing disconnect; retry shortly")
	}
	w.relogin(a, "stalled")
	if calls != 3 {
		t.Fatalf("connect attempts = %d, want 3", calls)
	}
	a.mu.RLock()
	deliberate, lastErr := a.deliberateDisconnect, a.lastErr
	a.mu.RUnlock()
	if deliberate {
		t.Fatal("the account was left logged out with deliberateDisconnect set: nothing would ever log it back in")
	}
	if !strings.Contains(lastErr, "watchdog relogin could not log it back in") {
		t.Fatalf("lastErr = %q", lastErr)
	}
	st, ok := w.status[a.username]
	if !ok || st.State != "needs_attention" || !strings.Contains(st.Detail, "could not queue the login") {
		t.Fatalf("status = %+v, want needs_attention saying why", st)
	}
	if h := w.hist[a.username]; h.Step != stallwatch.StepGaveUp || h.GaveUpAt.IsZero() {
		t.Fatalf("history step %s, want the ladder closed as given up", h.Step)
	}
	if w.relogins != 0 || w.inFlight[a.username] {
		t.Fatalf("relogins=%d inFlight=%v: the relogin must still close itself", w.relogins, w.inFlight[a.username])
	}
	found := false
	for _, l := range lines {
		found = found || strings.Contains(l, "[watchdog] NEEDS ATTENTION: the relogin logged it out")
	}
	if !found {
		t.Fatalf("no NEEDS ATTENTION line: %q", lines)
	}
}

// TestDefaultWatchdogIsObserve: the fleet default, which the -watchdog flag's default is, acts on
// nothing.
func TestDefaultWatchdogIsObserve(t *testing.T) {
	if got := DefaultWatchdogConfig().Mode; got != stallwatch.ModeObserve {
		t.Fatalf("DefaultWatchdogConfig().Mode = %q, want observe", got)
	}
	m := New(Config{})
	cfg := DefaultWatchdogConfig()
	cfg.DumpDir = ""
	m.StartWatchdog(cfg)
	defer m.CloseAll()
	if info := m.WatchdogInfo(); info.Mode != "observe" || !info.Running || !info.Configured {
		t.Fatalf("info = %+v", info)
	}
}

// TestWatchdogModeSwitchesAtRuntime: the mode changes without a restart, a bad mode is refused,
// off stops the sweeps and clears the statuses, and a watchdog started off can be switched on.
func TestWatchdogModeSwitchesAtRuntime(t *testing.T) {
	m := New(Config{})
	if _, err := m.SetWatchdogMode("act"); err == nil {
		t.Fatal("a Manager with no watchdog configured must refuse a switch")
	}
	if info := m.WatchdogInfo(); info.Configured || info.Mode != "off" {
		t.Fatalf("unconfigured info = %+v", info)
	}
	cfg := DefaultWatchdogConfig()
	cfg.DumpDir = ""
	m.StartWatchdog(cfg)
	defer m.CloseAll()
	prev, err := m.SetWatchdogMode("ACT")
	if err != nil || prev != "observe" {
		t.Fatalf("switch to act: prev %q err %v", prev, err)
	}
	if info := m.WatchdogInfo(); info.Mode != "act" {
		t.Fatalf("mode after the switch = %q", info.Mode)
	}
	m.watch.mu.Lock()
	live := m.watch.mode
	m.watch.mu.Unlock()
	if live != stallwatch.ModeAct {
		t.Fatalf("the sweep reads %q", live)
	}
	if _, err := m.SetWatchdogMode("restart"); err == nil {
		t.Fatal("an unknown mode must be refused")
	}
	m.watch.mu.Lock()
	m.watch.status["someone"] = WatchdogStatus{State: "stalled"}
	m.watch.mu.Unlock()
	if prev, err := m.SetWatchdogMode("off"); err != nil || prev != "act" {
		t.Fatalf("switch to off: prev %q err %v", prev, err)
	}
	if m.watchStatus("someone") != nil {
		t.Fatal("off must clear the per-account statuses")
	}
	m.watch.sweep(time.Now()) // must do nothing, and not panic
	if info := m.WatchdogInfo(); info.Running || info.Mode != "off" {
		t.Fatalf("info after off = %+v", info)
	}

	// Started off: nothing runs until a switch starts it.
	o := New(Config{})
	offCfg := DefaultWatchdogConfig()
	offCfg.Mode, offCfg.DumpDir = stallwatch.ModeOff, ""
	o.StartWatchdog(offCfg)
	defer o.CloseAll()
	if o.watch != nil {
		t.Fatal("mode off started a watchdog")
	}
	if prev, err := o.SetWatchdogMode("observe"); err != nil || prev != "off" {
		t.Fatalf("switch on: prev %q err %v", prev, err)
	}
	if o.watch == nil {
		t.Fatal("switching an off watchdog on must start it")
	}
	if info := o.WatchdogInfo(); !info.Running || info.Mode != "observe" {
		t.Fatalf("info = %+v", info)
	}
}
