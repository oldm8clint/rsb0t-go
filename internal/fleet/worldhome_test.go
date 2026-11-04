package fleet

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/client"
)

func TestSetHomeWorldTakesAListedWorldAndRefusesTheRest(t *testing.T) {
	m, _ := twoWorldManager(t)
	if err := m.worldMap().List([]int{1, 2}); err != nil {
		t.Fatal(err)
	}
	a := &account{username: "sw2", chat: chatlog.New(), activity: chatlog.New()}
	a.world, a.hop.Home = 1, 1
	m.accounts = append(m.accounts, a)

	if err := m.SetHomeWorld("sw2", 2); err != nil {
		t.Fatalf("world 2 is listed: %v", err)
	}
	if got := m.HomeWorldOf("sw2"); got != 2 {
		t.Fatalf("home world %d, want 2", got)
	}
	if m.WorldOf("sw2") != 1 {
		t.Fatal("making world 2 home must not move the account's next login")
	}
	if err := m.SetHomeWorld("sw2", 3); err == nil {
		t.Fatal("world 3 is not listed (it was removed 2026-09-22) and must be refused")
	}
	if err := m.SetHomeWorld("nobody", 2); err == nil {
		t.Fatal("an unknown account is refused")
	}
}

func TestStartWorldTakesAListedScriptHomeWorld(t *testing.T) {
	m, _ := twoWorldManager(t)
	if err := m.worldMap().List([]int{1, 2}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		why      string
		roster   int
		settings map[string]interface{}
		want     int
	}{
		{"no settings keep the roster's world", 1, nil, 1},
		{"no homeWorld keeps the roster's world", 0, map[string]interface{}{"mule": "mule"}, 0},
		{"a saved assignment's homeWorld 2 (JSON number) lands the account there", 0, map[string]interface{}{"homeWorld": 2.0}, 2},
		{"homeWorld wins over a roster world", 1, map[string]interface{}{"homeWorld": 2}, 2},
		{"homeWorld 0 changes nothing", 1, map[string]interface{}{"homeWorld": 0.0}, 1},
		{"a world this deployment does not list changes nothing", 1, map[string]interface{}{"homeWorld": 3.0}, 1},
		{"a non-number changes nothing", 1, map[string]interface{}{"homeWorld": "2"}, 1},
	} {
		if got := m.StartWorld(c.roster, c.settings); got != c.want {
			t.Errorf("%s: StartWorld(%d, %v) = %d, want %d", c.why, c.roster, c.settings, got, c.want)
		}
	}
}

func TestHoppingReportsTheHopFlag(t *testing.T) {
	m, _ := twoWorldManager(t)
	a := &account{username: "h", chat: chatlog.New(), activity: chatlog.New()}
	m.accounts = append(m.accounts, a)
	if m.Hopping("h") {
		t.Fatal("not hopping")
	}
	a.mu.Lock()
	a.hopping = true
	a.mu.Unlock()
	if !m.Hopping("h") || m.Hopping("nobody") {
		t.Fatal("hop flag")
	}
}

func TestLoginBusyCountsQueuedAndConcurrentLogins(t *testing.T) {
	m, _ := twoWorldManager(t)
	for i := 0; i < 6; i++ {
		m.accounts = append(m.accounts, &account{username: fmt.Sprintf("a%d", i), chat: chatlog.New(), activity: chatlog.New()})
	}
	fresh := func() { m.loginBusy.at.Store(0) }
	if m.LoginBusy() {
		t.Fatal("idle fleet")
	}
	fresh()
	for i := 0; i < loginBusyConnecting; i++ {
		m.accounts[i].connecting = true
	}
	if m.LoginBusy() {
		t.Fatal("a handful of reconnects is steady state")
	}
	fresh()
	m.accounts[loginBusyConnecting].connecting = true
	if !m.LoginBusy() {
		t.Fatal("more than a handful inside a login is a ramp")
	}
	fresh()
	for _, a := range m.accounts {
		a.connecting = false
	}
	m.accounts[5].routeWait = true
	if m.LoginBusy() {
		t.Fatal("ONE account queued for a full route is steady state, not a ramp (it used to hold every errand off)")
	}
	fresh()
	for _, a := range m.accounts {
		a.connecting, a.routeWait = true, true // inside loginOnce, still queued for a slot
	}
	if m.LoginBusy() {
		t.Fatal("accounts queued for route slots send no logins: a down route's queue is not a ramp")
	}
	fresh()
	for i := 0; i <= loginBusyConnecting; i++ {
		m.accounts[i].routeWait = false // admitted: waiting at the route's gate or logging in
	}
	if !m.LoginBusy() {
		t.Fatal("more than a handful admitted and inside a login is a ramp")
	}
	// Cached: a change inside loginBusyEvery is not seen.
	for _, a := range m.accounts {
		a.connecting = false
	}
	if !m.LoginBusy() {
		t.Fatal("the answer is cached for loginBusyEvery")
	}
}

func TestTheHopTimerIsLearnedOnceAndWaitedOut(t *testing.T) {
	m, _ := twoWorldManager(t)
	gone := time.Now().Add(-10 * time.Second)
	if w := m.hopTimerPrewait(gone); w != 0 {
		t.Fatalf("nothing learned yet: %s", w)
	}
	// A response 21 met 8s after the logout stating 37s: the timer is ~45s.
	m.learnHopTimer(8*time.Second, 37*time.Second)
	w := m.hopTimerPrewait(gone)
	if w < 35*time.Second || w > 37*time.Second {
		t.Fatalf("10s after the logout a 45s timer has ~36s left (+1s margin): %s", w)
	}
	if w := m.hopTimerPrewait(time.Now().Add(-time.Minute)); w != 0 {
		t.Fatalf("a timer that has run out: %s", w)
	}
	m.learnHopTimer(0, 255*time.Second)
	if w := m.hopTimerPrewait(time.Now()); w > hopTimerMax+hopTimerMargin {
		t.Fatalf("a learned timer is bounded: %s", w)
	}
	// It follows the latest observation down again (it used to keep the largest ever seen).
	m.learnHopTimer(2*time.Second, 13*time.Second)
	if w := m.hopTimerPrewait(time.Now()); w < 15*time.Second || w > 16*time.Second {
		t.Fatalf("a shorter timer observed later replaces the longer one: %s", w)
	}
	// A reference older than the cap cannot be the timer (the server would have admitted the
	// login): not learned from.
	m.learnHopTimer(10*time.Minute, 40*time.Second)
	if w := m.hopTimerPrewait(time.Now()); w > 16*time.Second {
		t.Fatalf("an observation against a stale logout time is ignored: %s", w)
	}
}

func TestReconnectHonoursTheStatedHopTimerOnce(t *testing.T) {
	m, _ := twoWorldManager(t)
	a := &account{username: "r", chat: chatlog.New(), activity: chatlog.New()}
	m.accounts = append(m.accounts, a)
	m.noteRetryHopTimer(a, fmt.Errorf("wrapped: %w", &client.LoginRejectedError{Response: client.HopTimerResponse, RetrySeconds: 15}))
	if got := m.retryWaitFor(a, 3*time.Second); got != 16*time.Second {
		t.Fatalf("the stated 15s plus the margin, not the 3s backoff: %s", got)
	}
	if got := m.retryWaitFor(a, 3*time.Second); got != 3*time.Second {
		t.Fatalf("consumed once: %s", got)
	}
	m.noteRetryHopTimer(a, &client.LoginRejectedError{Response: 16})
	if got := m.retryWaitFor(a, 3*time.Second); got != 3*time.Second {
		t.Fatalf("any other refusal keeps the backoff: %s", got)
	}
}

// THE FAILED LEG. hopLeg points the account at the target before logging in; when the login
// fails it must point it back, or a plan that then gives up ("world 2 is full — staying on world
// 1") hands the account to ConnectExisting, which dials the world that just refused it.
func TestAFailedHopLegLeavesTheAccountOnItsOldWorld(t *testing.T) {
	m, d := twoWorldManager(t)
	a := &account{username: "leg", chat: chatlog.New(), activity: chatlog.New()}
	a.world, a.hop.Home, a.hop.Current = 1, 1, 1
	m.accounts = append(m.accounts, a)

	out := m.hopLeg(a, 2)
	if out == "" {
		t.Fatal("the leg must report an outcome")
	}
	seen := d.seen()
	if len(seen) != 1 || !strings.Contains(seen[0], "w2.") {
		t.Fatalf("the leg dialled %v, want world 2 once", seen)
	}
	a.mu.RLock()
	world, current := a.world, a.hop.Current
	a.mu.RUnlock()
	if world != 1 || current != 1 {
		t.Fatalf("after a failed leg the account is on world %d (hop current %d), want 1", world, current)
	}
}

func TestPinWorldSetting(t *testing.T) {
	m, _ := twoWorldManager(t)
	a := &account{username: "mule", chat: chatlog.New(), activity: chatlog.New()}
	m.accounts = append(m.accounts, a)
	m.applyPinSetting("mule", map[string]interface{}{"pinWorld": 1.0})
	if w, ok := m.PinnedWorld("mule"); !ok || w != 1 {
		t.Fatalf("pinWorld 1 pins: %d %v", w, ok)
	}
	m.applyPinSetting("mule", map[string]interface{}{})
	if _, ok := m.PinnedWorld("mule"); !ok {
		t.Fatal("no setting leaves the pin alone")
	}
}

// A rotating RuneRunner asks for a bigger hop budget than the fleet's four in thirty minutes; the
// plan then lets it hop where the default would have kept it home (7 October 2026: 18 of 77 hops).
func TestSetHopBudgetRaisesTheFlappingBoundForOneAccount(t *testing.T) {
	m, _ := twoWorldManager(t)
	a := &account{username: "rr1", chat: chatlog.New(), activity: chatlog.New()}
	m.accounts = append(m.accounts, a)
	if err := m.SetHopBudget("nobody", 6, 30*time.Minute); err == nil {
		t.Fatal("an unknown account is refused")
	}
	if err := m.SetHopBudget("rr1", 6, 30*time.Minute); err != nil {
		t.Fatal(err)
	}
	if a.hopLimits.MaxHops != 6 || a.hopLimits.Window != 30*time.Minute {
		t.Fatalf("limits %+v", a.hopLimits)
	}
	// Clamped: never above 30 hops, and a window of five minutes to an hour.
	if err := m.SetHopBudget("rr1", 99, time.Second); err != nil {
		t.Fatal(err)
	}
	if a.hopLimits.MaxHops != 30 || a.hopLimits.Window != 5*time.Minute {
		t.Fatalf("clamped limits %+v", a.hopLimits)
	}
	if err := m.SetHopBudget("rr1", 0, 0); err != nil {
		t.Fatal(err)
	}
	if a.hopLimits.MaxHops != 0 {
		t.Fatalf("zero restores the default: %+v", a.hopLimits)
	}
}
