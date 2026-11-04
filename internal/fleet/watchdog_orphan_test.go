package fleet

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/bot"
	"rsb0t-go/internal/client"
	"rsb0t-go/internal/stallwatch"
)

// The 18 October 2026 incident, replayed: a watchdog restart stopped a ScarabSwarm bot that took
// minutes to wind down, gave up after 15 seconds, and left the account in game with no bot for
// eight hours. These drive the real watchdog code over real bot.Runners (a script that holds in
// OnStop stands in for the slow bot) and a start function that records what would have been started.

// holdBot is a script that runs until stopped, then holds in OnStop until release is closed: the slow
// ScarabSwarm prayer ladder. With err set, its first Loop fails instead (a script's own error).
type holdBot struct {
	release chan struct{}
	err     error
}

func (b *holdBot) OnStart(*bot.Context) error { return nil }
func (b *holdBot) Loop(*bot.Context) error    { return b.err }
func (b *holdBot) OnStop(*bot.Context) {
	if b.release != nil {
		<-b.release
	}
}

func newRunner(script string, b bot.Bot, settings map[string]interface{}) *bot.Runner {
	ctx := bot.NewContext(nil, "x", nil, nil, nil, nil, nil, nil, nil, settings, nil)
	r := bot.NewRunner(script, b, ctx)
	r.Start()
	return r
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// logSink collects every line the Manager prints, safely across the watchdog's goroutines.
type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (s *logSink) add(l string) { s.mu.Lock(); s.lines = append(s.lines, l); s.mu.Unlock() }
func (s *logSink) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lines...)
}
func (s *logSink) has(sub string) bool {
	for _, l := range s.all() {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}
func (s *logSink) count(sub string) int {
	n := 0
	for _, l := range s.all() {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

type startCall struct {
	user, script string
	settings     map[string]interface{}
	oldFinished  bool
}

// startRec is the watchdog's start function: it records the call and reports whether the old bot
// had finished by then, which is what "never a second copy beside a live one" means.
type startRec struct {
	mu    sync.Mutex
	calls []startCall
	old   func(user string) *bot.Runner
	err   error
}

func (r *startRec) fn(user, script string, settings map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := startCall{user: user, script: script, settings: settings, oldFinished: true}
	if r.old != nil {
		if old := r.old(user); old != nil {
			c.oldFinished = old.Finished()
		}
	}
	r.calls = append(r.calls, c)
	return r.err
}

func (r *startRec) n() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.calls) }
func (r *startRec) get(i int) startCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[i]
}

// orphanFixture is a manager with one account in game whose bot is the slow one, and a watchdog
// wired to a recording start function.
type orphanFixture struct {
	m        *Manager
	w        *watchdog
	a        *account
	r        *bot.Runner
	bot      *holdBot
	log      *logSink
	starts   *startRec
	settings map[string]interface{}
}

func newOrphanFixture(t *testing.T, name string) *orphanFixture {
	t.Helper()
	m := New(Config{})
	f := &orphanFixture{m: m, log: &logSink{}, starts: &startRec{}, settings: map[string]interface{}{"homeWorld": 2, "mule": "m1"}}
	m.Log = f.log.add
	f.a = watchAccount(m, name)
	f.a.script = "ScarabSwarm"
	f.a.session = &client.Session{} // a placeholder: present, never used by a stopped account
	f.bot = &holdBot{release: make(chan struct{})}
	f.r = newRunner("ScarabSwarm", f.bot, f.settings)
	f.a.runner = f.r
	waitUntil(t, "the bot to start", func() bool { return f.r.Status().State == bot.StateRunning })
	f.w = testWatchdog(m)
	f.w.cfg.RestartWait, f.w.cfg.RestartFinishWait = 20*time.Millisecond, 5*time.Second
	f.starts.old = func(string) *bot.Runner { return f.r }
	f.w.startFn = f.starts.fn
	t.Cleanup(func() { f.releaseBot() })
	return f
}

func (f *orphanFixture) releaseBot() {
	defer func() { _ = recover() }() // already released
	close(f.bot.release)
}

// TestRestartFinishesTheJobWhenTheOldBotIsSlowToStop is the incident: the old bot outlives the
// first RestartWait, so the restart says it is NOT starting a second copy beside it -- and then
// keeps waiting, and starts the same script with the same settings once the old bot has ended.
func TestRestartFinishesTheJobWhenTheOldBotIsSlowToStop(t *testing.T) {
	f := newOrphanFixture(t, "slowstop")
	f.w.begin(f.a.username)
	done := make(chan struct{})
	go func() { f.w.restart(f.a); close(done) }()

	waitUntil(t, "the restart to give up its quiet wait", func() bool { return f.log.has("NOT starting a second copy beside it; waiting up to") })
	time.Sleep(60 * time.Millisecond) // several RestartWaits: it must not start beside the live one
	if n := f.starts.n(); n != 0 {
		t.Fatalf("started %d times while the old bot was still winding down", n)
	}
	f.a.mu.RLock()
	by := f.a.stopBy
	f.a.mu.RUnlock()
	if by != stopWatchdog {
		t.Fatalf("the restart's stop is marked %v, want the watchdog's own", by)
	}

	f.releaseBot() // the old bot finally ends, two minutes late in the live case
	<-done
	if f.starts.n() != 1 {
		t.Fatalf("starts = %d, want exactly one once the old bot ended", f.starts.n())
	}
	c := f.starts.get(0)
	if c.user != "slowstop" || c.script != "ScarabSwarm" || c.settings["homeWorld"] != 2 || c.settings["mule"] != "m1" {
		t.Fatalf("started %+v, want the same script with the same full settings", c)
	}
	if !c.oldFinished {
		t.Fatal("the new bot was started while the old runner was still alive: a second copy beside a live one")
	}
	if !f.log.has("restart: ScarabSwarm is running again") {
		t.Fatalf("no success line: %q", f.log.all())
	}
	if f.w.inFlight["slowstop"] {
		t.Fatal("the restart must hand the account back to the sweep when it is done")
	}
}

// TestRestartThatTheOldBotOutlivesIsNotStartedBesideIt: a bot still alive after both waits is left
// alone for the ladder's relogin -- never a second copy.
func TestRestartThatTheOldBotOutlivesIsNotStartedBesideIt(t *testing.T) {
	f := newOrphanFixture(t, "stuck")
	f.w.cfg.RestartFinishWait = 40 * time.Millisecond
	f.w.begin(f.a.username)
	f.w.restart(f.a)
	if f.starts.n() != 0 {
		t.Fatalf("started %d times beside a live bot", f.starts.n())
	}
	if !f.log.has("has still not ended after") {
		t.Fatalf("no line saying it gave up waiting: %q", f.log.all())
	}
}

// TestRestartLeavesAnOperatorsStopAlone: an operator who presses Stop while the restart waits for
// the old bot has the last word. Their stop outranks the watchdog's.
func TestRestartLeavesAnOperatorsStopAlone(t *testing.T) {
	f := newOrphanFixture(t, "opstop")
	f.w.begin(f.a.username)
	done := make(chan struct{})
	go func() { f.w.restart(f.a); close(done) }()
	waitUntil(t, "the restart to be waiting", func() bool { return f.log.has("waiting up to") })
	if err := f.m.StopBot("opstop"); err != nil { // the panel's POST /api/bot/stop
		t.Fatal(err)
	}
	f.releaseBot()
	<-done
	if f.starts.n() != 0 {
		t.Fatal("the restart started a bot an operator had just stopped")
	}
	if !f.log.has("an operator stopped the bot meanwhile") {
		t.Fatalf("no line: %q", f.log.all())
	}
}

// TestRestartNeverStartsASecondCopyWhenSomethingElseStartedOne: if another bot was started on the
// account while the old one wound down (an operator's start, a reconnect resume), the restart
// leaves it.
func TestRestartNeverStartsASecondCopyWhenSomethingElseStartedOne(t *testing.T) {
	f := newOrphanFixture(t, "raced")
	f.w.begin(f.a.username)
	done := make(chan struct{})
	go func() { f.w.restart(f.a); close(done) }()
	waitUntil(t, "the restart to be waiting", func() bool { return f.log.has("waiting up to") })
	other := &holdBot{release: make(chan struct{})}
	defer close(other.release)
	replacement := newRunner("ScarabSwarm", other, f.settings)
	f.a.mu.Lock()
	f.a.runner = replacement // what a StartBot does
	f.a.mu.Unlock()
	f.releaseBot()
	<-done
	if f.starts.n() != 0 {
		t.Fatal("started a second bot beside the one that took the account")
	}
	if !f.log.has("another bot (running) was started on the account meanwhile") {
		t.Fatalf("no line: %q", f.log.all())
	}
	replacement.Stop()
}

// TestRestartArmsTheReconnectWhenTheConnectionDroppedMeanwhile: the account dropped while the old
// bot was stopping (10:46 live) and is mid-reconnect when the old bot ends. Nothing can start yet;
// the restart arms the reconnect's resume so the script starts when the login lands.
func TestRestartArmsTheReconnectWhenTheConnectionDroppedMeanwhile(t *testing.T) {
	f := newOrphanFixture(t, "dropped")
	f.w.begin(f.a.username)
	done := make(chan struct{})
	go func() { f.w.restart(f.a); close(done) }()
	waitUntil(t, "the restart to be waiting", func() bool { return f.log.has("waiting up to") })
	f.a.mu.Lock()
	f.a.session, f.a.reconnecting = nil, true
	f.a.mu.Unlock()
	f.releaseBot()
	<-done
	f.a.mu.RLock()
	armed := f.a.resumeScript
	f.a.mu.RUnlock()
	if !armed || f.starts.n() != 0 {
		t.Fatalf("resumeScript=%v starts=%d, want the resume armed and no start without a session", armed, f.starts.n())
	}
}

// TestStopSourcesAreRecordedAndAnOperatorsOutranksTheRest: every outside stop is marked with who
// asked, a new run clears nothing here (StartBot does), and the watchdog's stop never downgrades an
// operator's.
func TestStopSourcesAreRecordedAndAnOperatorsOutranksTheRest(t *testing.T) {
	m := New(Config{})
	m.Log = func(string) {}
	a := watchAccount(m, "marks")
	a.script = "ScarabSwarm"
	a.runner = newRunner("ScarabSwarm", &holdBot{}, nil)
	defer a.runner.Stop()

	if err := m.stopBot("marks", stopWatchdog); err != nil {
		t.Fatal(err)
	}
	if a.stopBy != stopWatchdog {
		t.Fatalf("after the watchdog's stop: %v", a.stopBy)
	}
	if err := m.StopBot("marks"); err != nil { // the public entry point is the operator's
		t.Fatal(err)
	}
	if a.stopBy != stopOperator {
		t.Fatalf("after an operator's stop: %v", a.stopBy)
	}
	if err := m.stopBot("marks", stopWatchdog); err != nil {
		t.Fatal(err)
	}
	if a.stopBy != stopOperator {
		t.Fatalf("the watchdog's stop downgraded an operator's: %v", a.stopBy)
	}
	if !a.runner.StoppedFromOutside() {
		t.Fatal("a stop through the fleet must show on the runner as called from outside")
	}
	if got := stopOperator.kind(); got != stallwatch.StopOperator {
		t.Fatalf("kind = %v", got)
	}
}

// The safety net, driven through whole sweeps over real runners.

// orphan makes the fixture's bot end by the watchdog's own stop, and waits for it to be finished.
func (f *orphanFixture) orphan(t *testing.T, by stopSource) {
	t.Helper()
	if err := f.m.stopBot(f.a.username, by); err != nil {
		t.Fatal(err)
	}
	f.releaseBot()
	select {
	case <-f.r.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the bot did not end")
	}
}

func (f *orphanFixture) act() *orphanFixture {
	f.w.mu.Lock()
	f.w.mode = stallwatch.ModeAct
	f.w.mu.Unlock()
	return f
}

// settle waits for every action goroutine of the watchdog to finish.
func (f *orphanFixture) settle(t *testing.T) {
	t.Helper()
	waitUntil(t, "the watchdog's actions to finish", func() bool {
		f.w.mu.Lock()
		defer f.w.mu.Unlock()
		return len(f.w.inFlight) == 0
	})
}

// TestSafetyNetStartsTheBotTheWatchdogLeftStopped is the whole incident: the account is in game,
// the bot ended after the watchdog's own stop, nothing restarted it. Eight hours later one sweep
// starts the same script with the same settings and says why.
func TestSafetyNetStartsTheBotTheWatchdogLeftStopped(t *testing.T) {
	f := newOrphanFixture(t, "incident").act()
	f.orphan(t, stopWatchdog)
	now := time.Now()

	// Just after it ended: not an orphan yet.
	f.w.sweep(now.Add(time.Minute))
	f.settle(t)
	if f.starts.n() != 0 {
		t.Fatal("started before OrphanAfter")
	}

	f.w.sweep(now.Add(8 * time.Hour))
	f.settle(t)
	if f.starts.n() != 1 {
		t.Fatalf("starts = %d, want exactly one", f.starts.n())
	}
	c := f.starts.get(0)
	if c.script != "ScarabSwarm" || c.settings["homeWorld"] != 2 || !c.oldFinished {
		t.Fatalf("started %+v, want the assigned script with its full settings, after the old runner ended", c)
	}
	if !f.log.has("safety net: ScarabSwarm has been stopped for") || !f.log.has("the watchdog's own restart or relogin stopped it and nothing started it again") {
		t.Fatalf("the start must say why: %q", f.log.all())
	}
	if !f.log.has("safety net: ScarabSwarm is running again") {
		t.Fatalf("no success line: %q", f.log.all())
	}
	f.w.mu.Lock()
	n := f.w.counts.Orphans
	f.w.mu.Unlock()
	if n != 1 {
		t.Fatalf("counts.Orphans = %d", n)
	}
}

// TestSafetyNetLeavesAnOperatorsStopAlone: the panel's stop, however long ago.
func TestSafetyNetLeavesAnOperatorsStopAlone(t *testing.T) {
	f := newOrphanFixture(t, "opstopped").act()
	f.orphan(t, stopOperator)
	for _, h := range []time.Duration{time.Hour, 8 * time.Hour, 48 * time.Hour} {
		f.w.sweep(time.Now().Add(h))
	}
	f.settle(t)
	if f.starts.n() != 0 || f.log.has("safety net") {
		t.Fatalf("touched an operator's stop: starts=%d log=%q", f.starts.n(), f.log.all())
	}
}

// midReconnectStop is the state a drop leaves behind: the read loop's cleanup stopped the bot with a
// plain runner.Stop() (no source recorded through the fleet), the connection is gone and the
// reconnect loop is retrying. It waits for the bot to be finished.
func (f *orphanFixture) midReconnectStop(t *testing.T) {
	t.Helper()
	f.r.Stop()
	f.releaseBot()
	select {
	case <-f.r.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the bot did not end")
	}
	f.a.mu.Lock()
	f.a.session, f.a.reconnecting = nil, true
	f.a.mu.Unlock()
}

// loggedInByHand is the account back in game after somebody logged it in (ConnectExisting): a session,
// no reconnect in flight, the disconnect flag cleared, a fresh login time.
func (f *orphanFixture) loggedInByHand() {
	f.a.mu.Lock()
	f.a.session, f.a.reconnecting, f.a.deliberateDisconnect = &client.Session{}, false, false
	f.a.connectedAt = time.Now()
	f.a.mu.Unlock()
}

// TestOperatorDisconnectMidReconnectIsLeftAlone: an operator logs out an account whose connection had
// dropped and whose reconnect was still retrying. There is no session to log out, so Disconnect
// returns early, and the stale bot was stopped by the read loop with no source of its own. The
// operator's disconnect must still be recorded, or the safety net starts the old script five minutes
// after they log the account back in by hand. The control case is the same account without the
// Disconnect, which the net does start.
func TestOperatorDisconnectMidReconnectIsLeftAlone(t *testing.T) {
	f := newOrphanFixture(t, "midreconnect").act()
	f.midReconnectStop(t)
	if err := f.m.Disconnect(f.a.username); err != nil {
		t.Fatalf("Disconnect mid-reconnect: %v", err)
	}
	f.a.mu.RLock()
	by, deliberate, resume := f.a.stopBy, f.a.deliberateDisconnect, f.a.resumeScript
	f.a.mu.RUnlock()
	if by != stopOperator || !deliberate || resume {
		t.Fatalf("after an operator's Disconnect mid-reconnect: stopBy=%v deliberateDisconnect=%v resumeScript=%v, want an operator's stop recorded", by, deliberate, resume)
	}
	f.loggedInByHand()
	f.w.sweep(time.Now().Add(8 * time.Hour))
	f.settle(t)
	if f.starts.n() != 0 || f.log.has("safety net") {
		t.Fatalf("started a script an operator disconnected: starts=%d log=%q", f.starts.n(), f.log.all())
	}

	control := newOrphanFixture(t, "midreconnectcontrol").act()
	control.midReconnectStop(t)
	control.loggedInByHand()
	control.w.sweep(time.Now().Add(8 * time.Hour))
	control.settle(t)
	if control.starts.n() != 1 {
		t.Fatalf("control: starts=%d, want the net to start a bot nobody stopped on purpose", control.starts.n())
	}
}

// TestFailedDisconnectMarksNothing: Disconnect on an account that is neither connected nor
// reconnecting is an error and changes nothing, so it is not an operator's stop either.
func TestFailedDisconnectMarksNothing(t *testing.T) {
	f := newOrphanFixture(t, "notconnected")
	f.a.mu.Lock()
	f.a.session = nil
	f.a.mu.Unlock()
	if err := f.m.Disconnect(f.a.username); err == nil {
		t.Fatal("Disconnect of an account that is not connected must fail")
	}
	f.a.mu.RLock()
	by, deliberate := f.a.stopBy, f.a.deliberateDisconnect
	f.a.mu.RUnlock()
	if by != stopNone || deliberate {
		t.Fatalf("a failed Disconnect left stopBy=%v deliberateDisconnect=%v", by, deliberate)
	}
}

// TestSafetyNetLeavesAScriptsOwnErrorAlone: a script that failed during this login session ended
// its own run. The retry that follows a NEW session (an error from an earlier one) is the pure
// plan's business (TestOrphanStopsThatAreNotAnOperatorsAreStarted).
func TestSafetyNetLeavesAScriptsOwnErrorAlone(t *testing.T) {
	m := New(Config{})
	sink := &logSink{}
	m.Log = sink.add
	a := watchAccount(m, "failed")
	a.script = "ScarabSwarm"
	a.session = &client.Session{}
	a.connectedAt = time.Now().Add(-time.Hour) // logged in before the script failed
	a.runner = newRunner("ScarabSwarm", &holdBot{err: errors.New("bank not found")}, nil)
	select {
	case <-a.runner.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the script did not fail")
	}
	if a.runner.Status().State != bot.StateError || a.runner.StoppedFromOutside() {
		t.Fatalf("setup: state %s, stopped from outside %v", a.runner.Status().State, a.runner.StoppedFromOutside())
	}
	rec := &startRec{}
	w := testWatchdog(m)
	w.startFn = rec.fn
	w.mode = stallwatch.ModeAct
	w.sweep(time.Now().Add(8 * time.Hour))
	if rec.n() != 0 || sink.has("safety net") {
		t.Fatalf("restarted a script that ended with its own error: starts=%d log=%q", rec.n(), sink.all())
	}
}

// TestSafetyNetWaitsForAccountsThatAreNotInGame: logging in, reconnecting, hopping, logged out --
// whoever owns that transition restarts the script.
func TestSafetyNetWaitsForAccountsThatAreNotInGame(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(a *account)
	}{
		{"hopping", func(a *account) { a.hopping = true }},
		{"reconnecting", func(a *account) { a.reconnecting = true }},
		{"connecting", func(a *account) { a.connecting = true }},
		{"deliberately disconnected", func(a *account) { a.deliberateDisconnect = true }},
		{"no session", func(a *account) { a.session = nil }},
	} {
		f := newOrphanFixture(t, "away").act()
		f.orphan(t, stopWatchdog)
		f.a.mu.Lock()
		c.set(f.a)
		f.a.mu.Unlock()
		f.w.sweep(time.Now().Add(8 * time.Hour))
		f.settle(t)
		if f.starts.n() != 0 {
			t.Errorf("%s: started a bot on an account that is not in game", c.name)
		}
	}
}

// TestSafetyNetOnlyReportsOutsideActMode: observe and nudge say a start is due, once, and take none.
func TestSafetyNetOnlyReportsOutsideActMode(t *testing.T) {
	f := newOrphanFixture(t, "observed") // observe
	f.orphan(t, stopWatchdog)
	now := time.Now().Add(8 * time.Hour)
	for i := 0; i < 5; i++ {
		f.w.sweep(now.Add(time.Duration(i) * 30 * time.Second))
	}
	f.settle(t)
	if f.starts.n() != 0 {
		t.Fatal("observe mode started a bot")
	}
	if n := f.log.count("it would be started again, but the watchdog is not in act mode"); n != 1 {
		t.Fatalf("the held start was reported %d times in five sweeps, want once: %q", n, f.log.all())
	}
}

// TestSafetyNetCapsRepeatedStartsForOneAccount: a bot that keeps ending is started OrphanMaxStarts
// times and then reported for an operator, not started forever.
func TestSafetyNetCapsRepeatedStartsForOneAccount(t *testing.T) {
	f := newOrphanFixture(t, "keepsending").act()
	f.orphan(t, stopWatchdog)
	now := time.Now().Add(8 * time.Hour)
	for i := 0; i < 6; i++ { // the fake start never replaces the runner, so every sweep sees an orphan
		f.w.sweep(now.Add(time.Duration(i) * 30 * time.Second))
		f.settle(t)
	}
	limit := stallwatch.DefaultConfig().OrphanMaxStarts
	if f.starts.n() != limit {
		t.Fatalf("starts = %d, want the cap of %d", f.starts.n(), limit)
	}
	if n := f.log.count("NEEDS ATTENTION: ScarabSwarm has been stopped in game"); n != 1 {
		t.Fatalf("the cap was reported %d times, want once: %q", n, f.log.all())
	}
	f.w.mu.Lock()
	st := f.w.status["keepsending"]
	f.w.mu.Unlock()
	if st.State != "needs_attention" {
		t.Fatalf("status = %+v, want needs_attention so the panel shows it", st)
	}
}

// TestSafetyNetStartsFewPerSweep: a fleet-wide event that strands many accounts at once is not
// answered with a start stampede.
func TestSafetyNetStartsFewPerSweep(t *testing.T) {
	m := New(Config{})
	m.Log = func(string) {}
	rec := &startRec{}
	w := testWatchdog(m)
	// A start that takes the account out of the net's reach the way a real StartBot does (a new run
	// nobody has stopped), without needing a live session to run one on.
	w.startFn = func(user, script string, settings map[string]interface{}) error {
		a := m.find(user)
		a.mu.Lock()
		a.stopBy = stopOperator
		a.mu.Unlock()
		return rec.fn(user, script, settings)
	}
	w.mode = stallwatch.ModeAct
	const accounts = 7
	for i := 0; i < accounts; i++ {
		a := watchAccount(m, fmt.Sprintf("many%d", i))
		a.script = "ScarabSwarm"
		a.session = &client.Session{}
		a.runner = newRunner("ScarabSwarm", &holdBot{}, nil)
		waitUntil(t, "start", func() bool { return a.runner.Status().State == bot.StateRunning })
		if err := m.stopBot(a.username, stopWatchdog); err != nil {
			t.Fatal(err)
		}
		<-a.runner.Done()
	}
	perSweep := stallwatch.DefaultConfig().OrphanPerSweep
	distinct := func() int {
		seen := map[string]bool{}
		for i := 0; i < rec.n(); i++ {
			seen[rec.get(i).user] = true
		}
		return len(seen)
	}
	now := time.Now().Add(8 * time.Hour)
	for sweepNo, want := 1, perSweep; sweepNo <= 3; sweepNo++ {
		w.sweep(now.Add(time.Duration(sweepNo) * 30 * time.Second))
		if want > accounts {
			want = accounts
		}
		waitUntil(t, fmt.Sprintf("sweep %d's starts", sweepNo), func() bool { return rec.n() >= want })
		waitUntil(t, "the actions to finish", func() bool {
			w.mu.Lock()
			defer w.mu.Unlock()
			return len(w.inFlight) == 0
		})
		if rec.n() != want || distinct() != want {
			t.Fatalf("after sweep %d: %d starts over %d accounts, want exactly %d (at most %d per sweep, none twice)", sweepNo, rec.n(), distinct(), want, perSweep)
		}
		want += perSweep
	}
}

// TestSweepNeverBlamesAnOperatorForItsOwnStop is the 11:02 line: a ladder dropped because the bot
// the watchdog itself stopped was never running again. It must not say "an operator or a hop took
// over". A ladder dropped for an operator's stop still names the operator.
func TestSweepNeverBlamesAnOperatorForItsOwnStop(t *testing.T) {
	for _, by := range []stopSource{stopWatchdog, stopOperator} {
		f := newOrphanFixture(t, "ladder") // observe: no start, so only the ladder's own line is under test
		f.orphan(t, by)
		f.w.hist["ladder"] = &stallwatch.History{Script: "ScarabSwarm", Detected: true, Step: stallwatch.StepRestarted, StepAt: time.Now()}
		now := time.Now()
		f.w.sweep(now)
		f.w.sweep(now.Add(f.w.cfg.IdleAbort + time.Minute))
		if f.log.has("operator or a hop took over") {
			t.Fatalf("by %v: the old claim is back: %q", by, f.log.all())
		}
		switch by {
		case stopWatchdog:
			if !f.log.has("the bot this watchdog itself stopped") {
				t.Fatalf("own stop: no honest line: %q", f.log.all())
			}
		case stopOperator:
			if !f.log.has("ladder dropped: the account left the game or its bot stopped for more than 15m0s (an operator stopped it)") {
				t.Fatalf("operator stop: %q", f.log.all())
			}
		}
	}
}

// TestOrphanObsReadsTheRunner pins what the safety net reads off a real runner at each stage of a
// stop: winding down while OnStop holds, then finished with who stopped it.
func TestOrphanObsReadsTheRunner(t *testing.T) {
	f := newOrphanFixture(t, "reader")
	read := func() stallwatch.OrphanObs { return f.w.observe(f.a).orphan }
	// While it runs, observe would read the (placeholder) session's game state, so look without one.
	f.a.mu.Lock()
	f.a.session = nil
	f.a.mu.Unlock()
	if o := read(); !o.HasBot || !o.Winding || o.InGame {
		t.Fatalf("running, no session: %+v", o)
	}
	f.a.mu.Lock()
	f.a.session = &client.Session{}
	f.a.mu.Unlock()
	if err := f.m.stopBot("reader", stopHop); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the loop to leave Running", func() bool { return f.r.Status().State == bot.StateStopped })
	if o := read(); !o.Winding {
		t.Fatalf("OnStop is still holding: %+v, want the bot still winding down", o)
	}
	f.releaseBot()
	<-f.r.Done()
	o := read()
	if o.Winding || !o.StopCalled || o.Stop != stallwatch.StopHop || o.StoppedAt.IsZero() || o.Errored || o.Finished {
		t.Fatalf("finished: %+v", o)
	}
	f.a.script = ""
	if o := read(); o.HasBot {
		t.Fatalf("an account with no script is not an orphan candidate: %+v", o)
	}
}
