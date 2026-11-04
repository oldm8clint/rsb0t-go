package fleet

// Tests added for review revW (2026-09-23) of the ScarabSwarm world-2 fleet half: the two
// blocking give-up defects in worldhop.go, and the wiring points the review's mutation run showed
// no test would notice being removed.

import (
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/bot"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/client"
	"rsb0t-go/internal/scripts/worldhopplan"
)

// B1. A hop that gives up with the account logged out must get it back in game WITH its script:
// the leg's Disconnect cleared resumeScript, and ensureWorking only reconnected. The reviewer's
// probe ended at state=stopped.
func TestAHopThatGivesUpLoggedOutRestartsTheScript(t *testing.T) {
	startupFixtureOnce.Do(func() { bot.Register("LoginRetryStartupFixture", func() bot.Bot { return &loginStartupBot{} }) })
	m := localLoginManager(t)
	m.Connect("giveup", "fixture_password")
	if !m.WaitConnected("giveup") {
		t.Fatal("never connected")
	}
	if err := m.StartBot("giveup", "LoginRetryStartupFixture", map[string]interface{}{"homeWorld": 2.0}); err != nil {
		t.Fatal(err)
	}
	a := m.find("giveup")
	// hopLeg's logout, then a failed login on the other world, then runHop's give-up.
	if err := m.Disconnect("giveup"); err != nil {
		t.Fatal(err)
	}
	if !m.waitSessionGone(a, 10*time.Second) {
		t.Fatal("session never went")
	}
	m.ensureWorking(a)
	if !m.WaitConnected("giveup") {
		t.Fatal("ensureWorking never logged it back in")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := m.Snapshot()[0]; st.BotState == "running" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	st := m.Snapshot()[0]
	t.Fatalf("after a hop gave up the account is back in game but its script is not running: script=%s state=%s", st.Script, st.BotState)
}

// B2. A give-up ends that request only. runHop returned from a give-up with a.hop's leg, attempt
// count and outcome untouched, so every later request (an errand's next hop, an hour on) was
// answered with the same give-up and never attempted a login.
func TestAGiveUpDoesNotStickToTheAccountsNextHop(t *testing.T) {
	m, d := twoWorldManager(t)
	d.setErr(&client.LoginRejectedError{Response: client.WorldFullResponse})
	// ensureWorking hands the account to retryAccount; park that retry so it does not dial on.
	m.retryWait = func(done <-chan struct{}, _ time.Duration) bool { <-done; return false }
	a := &account{username: "sticky", chat: chatlog.New(), activity: chatlog.New()}
	a.world, a.hop.Home, a.hop.Current = 1, 1, 1
	m.accounts = append(m.accounts, a)
	a.hopping = true
	m.runHop(a, 2, "test: world 2 is full")

	a.mu.RLock()
	st, hopping := a.hop, a.hopping
	a.mu.RUnlock()
	if hopping {
		t.Fatal("the hop flag is cleared when runHop returns")
	}
	if st.Leg != 0 || st.Attempts != 0 || st.Last != worldhopplan.OutcomeNone {
		t.Fatalf("the give-up left its leg behind: %+v", st)
	}
	if len(st.Recent) == 0 {
		t.Fatal("the flapping budget spans requests and must be kept")
	}
	dec := worldhopplan.Decide(worldhopplan.View{State: st, WantWorld: 2, WantKnown: true, Now: time.Now().Add(time.Hour), Distinct: true})
	if dec.Action != worldhopplan.ActionHop {
		t.Fatalf("an hour later a new request to world 2 must attempt a login, got %v (%s)", dec.Action, dec.Why)
	}
}

// M3. retryAccount's sleep honours a response 21's stated seconds (hoptimer.go's retryWaitFor
// wired into retryAccount), not the ordinary 2s-and-doubling backoff.
func TestRetryAccountWaitsTheStatedHopTimer(t *testing.T) {
	m, d := twoWorldManager(t)
	d.setErr(&client.LoginRejectedError{Response: client.HopTimerResponse, RetrySeconds: 30})
	var mu sync.Mutex
	var waits []time.Duration
	m.retryWait = func(done <-chan struct{}, wait time.Duration) bool {
		mu.Lock()
		waits = append(waits, wait)
		n := len(waits)
		mu.Unlock()
		return n < 2 // the first sleep, one failed login, the second sleep, then stop
	}
	a := &account{username: "timer", chat: chatlog.New(), activity: chatlog.New()}
	a.world, a.hop.Home, a.hop.Current = 1, 1, 1
	a.reconnecting = true
	m.accounts = append(m.accounts, a)
	m.retryAccount(a)
	mu.Lock()
	defer mu.Unlock()
	if len(waits) != 2 {
		t.Fatalf("waits %v, want two", waits)
	}
	if waits[1] != 30*time.Second+hopTimerMargin {
		t.Fatalf("after a response 21 stating 30s the retry waits %s, want %s", waits[1], 30*time.Second+hopTimerMargin)
	}
}

// M12 and M4. A hop leg that meets a response 21 teaches the fleet the timer (learnHopTimer is
// called), and the next leg waits it out before dialling (the pre-wait is used) instead of the
// plain five-second settle.
func TestAHopLegLearnsTheTimerAndTheNextLegWaitsItOut(t *testing.T) {
	m, _ := twoWorldManager(t)
	var mu sync.Mutex
	var dials []time.Time
	m.connectLogin = func(cfg client.Config) (*client.Session, error) {
		mu.Lock()
		dials = append(dials, time.Now())
		mu.Unlock()
		return nil, &client.LoginRejectedError{Response: client.HopTimerResponse, RetrySeconds: 10}
	}
	a := &account{username: "learn", chat: chatlog.New(), activity: chatlog.New()}
	a.world, a.hop.Home, a.hop.Current = 1, 1, 1
	m.accounts = append(m.accounts, a)

	if out := m.hopLeg(a, 2); out != worldhopplan.OutcomeHopTimer {
		t.Fatalf("first leg: %v, want the hop timer", out)
	}
	if w := m.hopTimerPrewait(time.Now()); w < 10*time.Second {
		t.Fatalf("a response 21 stating 10s about 5s after the logout teaches a ~15s timer; learned pre-wait %s", w)
	}
	start := time.Now()
	if out := m.hopLeg(a, 2); out != worldhopplan.OutcomeHopTimer {
		t.Fatalf("second leg: %v", out)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dials) != 2 {
		t.Fatalf("dials %d, want 2", len(dials))
	}
	// The retried leg keeps the first logout time, so ~16s after it minus the ~5s already spent:
	// ~11s, well past the 5s settle a leg without the pre-wait would use.
	if gap := dials[1].Sub(start); gap < 8*time.Second {
		t.Fatalf("the second leg dialled %s after it began; the learned timer should have held it past the 5s settle", gap.Round(time.Millisecond))
	}
}

// wiringFixture hands the Context StartBot built to the test.
type wiringFixture struct{}

func (*wiringFixture) OnStart(ctx *bot.Context) error {
	select {
	case wiringGot <- ctx:
	default:
	}
	return nil
}
func (*wiringFixture) Loop(ctx *bot.Context) error { ctx.Delay(time.Second); return nil }
func (*wiringFixture) OnStop(*bot.Context)         {}

var (
	wiringOnce sync.Once
	wiringGot  = make(chan *bot.Context, 4)
)

// M6 and M11. StartBot applies a script's pinWorld setting, and wires the three Context hooks the
// world errand reads (SetHomeWorld, HopInProgress, LoginBusy) to this Manager.
func TestStartBotWiresTheWorldHooksAndThePinSetting(t *testing.T) {
	wiringOnce.Do(func() { bot.Register("WorldWiringFixture", func() bot.Bot { return &wiringFixture{} }) })
	m := localLoginManager(t)
	m.Connect("wired", "fixture_password")
	if !m.WaitConnected("wired") {
		t.Fatal("never connected")
	}
	if err := m.StartBot("wired", "WorldWiringFixture", map[string]interface{}{"pinWorld": 1.0}); err != nil {
		t.Fatal(err)
	}
	if w, ok := m.PinnedWorld("wired"); !ok || w != 1 {
		t.Fatalf("pinWorld 1 in the settings pins the account: %d %v", w, ok)
	}
	var ctx *bot.Context
	select {
	case ctx = <-wiringGot:
	case <-time.After(10 * time.Second):
		t.Fatal("the fixture never started")
	}
	if ctx.SetHomeWorld == nil || ctx.HopInProgress == nil || ctx.LoginBusy == nil {
		t.Fatalf("world hooks not wired: SetHomeWorld %v, HopInProgress %v, LoginBusy %v", ctx.SetHomeWorld != nil, ctx.HopInProgress != nil, ctx.LoginBusy != nil)
	}
	if err := ctx.SetHomeWorld(1); err != nil {
		t.Fatalf("SetHomeWorld on the fleet's own world: %v", err)
	}
	if ctx.HopInProgress() {
		t.Fatal("no hop is running")
	}
	a := m.find("wired")
	a.mu.Lock()
	a.hopping = true
	a.mu.Unlock()
	if !ctx.HopInProgress() {
		t.Fatal("HopInProgress reads this account's hop flag")
	}
	a.mu.Lock()
	a.hopping = false
	a.mu.Unlock()
}
