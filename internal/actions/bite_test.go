package actions

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// biteRig plays the server's side of an eat for EatBite: a virtual clock the bite's waits spend, a list of effects due at given
// times (the stat update and the pack change a landed bite sends), and the engine's way with an eat read while the player is
// delayed (OpHeldHandler: `if (player.delayed) return false`) -- the click is dropped, no effect is ever scheduled.
type biteRig struct {
	f     *fakeSession
	base  time.Time
	clock time.Duration
	// due are the scheduled effects, in the order they were added; Delay runs those whose time has come.
	due  []biteEffect
	logs []string
	// eats is the clock time of every eat click that reached the server's socket; arrived the ones the engine processed.
	eats    []time.Duration
	arrived []time.Duration
	// latency is how long a bite takes from the click to the stat update reaching the client; zero means the effect is set by a
	// test through landAt.
	latency time.Duration
	// heal is what a landed bite adds to the hitpoints.
	heal int
	// serverDelayedUntil is the clock time until which the engine has the player delayed; a click read earlier is dropped.
	serverDelayedUntil time.Duration
	// pick is the pack evidence of the bite most recently picked: a landed bite sets it (unless noPack). Each bite has its own, as
	// each has its own baseline.
	pick   *bool
	noPack bool
	// landAt, when set, picks the clock time a bite sent at the given time lands (<0: it never does).
	landAt  func(sent time.Duration) time.Duration
	stopped bool
}

type biteEffect struct {
	at time.Duration
	fn func()
}

func newBiteRig(hp, maxHP int) *biteRig {
	r := &biteRig{f: newFakeSession(), base: time.Date(2026, 10, 15, 13, 6, 0, 0, time.UTC), heal: 4}
	r.stat(maxHP) // the maximum seen
	r.stat(hp)
	r.f.afterSend = func(*fakeSession) { r.onClick() }
	return r
}

func (r *biteRig) stat(level int) {
	r.f.state.Handle(serverprot.UPDATE_STAT, payload(func(p *packet.Packet) {
		p.P1(byte(gamestate.HitpointsStat))
		p.P4(100)
		p.P1(byte(level))
	}))
}

func (r *biteRig) hp() int { h, _ := r.f.state.HP(); return h }

// onClick is the engine reading one eat click.
func (r *biteRig) onClick() {
	r.eats = append(r.eats, r.clock)
	if r.clock < r.serverDelayedUntil {
		return // dropped: the player is delayed
	}
	r.arrived = append(r.arrived, r.clock)
	at := r.clock + r.latency
	if r.landAt != nil {
		at = r.landAt(r.clock)
	}
	if at < 0 {
		return
	}
	changed := r.pick
	r.at(at, func() {
		r.stat(min(r.hp()+r.heal, 53))
		if !r.noPack && changed != nil {
			*changed = true
		}
	})
}

func (r *biteRig) at(t time.Duration, fn func()) { r.due = append(r.due, biteEffect{at: t, fn: fn}) }

func (r *biteRig) now() time.Time { return r.base.Add(r.clock) }

// Delay spends virtual time and runs the effects that fall due, in time order.
func (r *biteRig) Delay(d time.Duration) bool {
	if r.stopped {
		return false
	}
	target := r.clock + d
	for {
		sort.SliceStable(r.due, func(i, j int) bool { return r.due[i].at < r.due[j].at })
		if len(r.due) == 0 || r.due[0].at > target {
			break
		}
		e := r.due[0]
		r.due = r.due[1:]
		r.clock = max(r.clock, e.at)
		e.fn()
	}
	r.clock = target
	return true
}

func (r *biteRig) logf(f string, a ...any) { r.logs = append(r.logs, fmt.Sprintf(f, a...)) }

func (r *biteRig) env() BiteEnv {
	return BiteEnv{Session: r.f, Now: r.now, Delay: r.Delay, Logf: r.logf}
}

func (r *biteRig) req(food string) BiteRequest {
	return BiteRequest{Food: food, Pick: func() (BitePick, bool) {
		changed := new(bool)
		r.pick = changed
		return BitePick{Obj: 1891, Slot: 3, Changed: func() bool { return *changed }}, true
	}}
}

func (r *biteRig) said(sub string) bool {
	for _, l := range r.logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func (r *biteRig) inFlight() bool { _, ok := BiteInFlightAt(r.f.state, r.now()); return ok }

// An ordinary bite shows inside a second or two: confirmed at once, nothing left in the ledger, no second click.
func TestABiteThatShowsInTimeIsConfirmedAtOnce(t *testing.T) {
	r := newBiteRig(30, 53)
	r.latency = 1200 * time.Millisecond
	if !EatBite(r.env(), r.req("Cake")) {
		t.Fatal("a bite that landed in 1.2 s was called failed")
	}
	if len(r.eats) != 1 || r.inFlight() || r.clock > 2*time.Second {
		t.Fatalf("eats %v, in flight %v, clock %v: want one click, nothing in flight, back inside two seconds", r.eats, r.inFlight(), r.clock)
	}
	if len(r.logs) != 0 {
		t.Fatalf("a quick bite said %v", r.logs)
	}
}

// LIVE 15 Oct 2026 13:06 (a Stage 3 account): 13/53 hitpoints, Bread, then Cake, Cake, Cake. Each bite landed seven to nine seconds after
// its click; each was called failed at six seconds and the account was held off thieving. The first call gives up its wait but
// the bite stays in the ledger; the next finds it landed, credits it and does NOT click again.
func TestALateLandingIsCreditedByTheNextBiteAndNeverEatenTwice(t *testing.T) {
	r := newBiteRig(13, 53)
	r.latency = 7500 * time.Millisecond
	if EatBite(r.env(), r.req("Bread")) {
		t.Fatal("a bite that had not shown at six seconds was confirmed")
	}
	if !r.inFlight() {
		t.Fatal("the bite that had not shown yet is not in flight: the caller would record a failure and hold the account")
	}
	if !r.said("has not shown after 6s at 13/53") {
		t.Fatalf("the unconfirmed bite said %v", r.logs)
	}
	r.Delay(2 * time.Second) // the caller's loop; the stat update arrives at 7.5 s
	if r.hp() != 17 {
		t.Fatalf("hitpoints %d, want 17: the rig did not land the bite", r.hp())
	}
	clicks := len(r.eats)
	if !EatBite(r.env(), r.req("Bread")) {
		t.Fatal("the bite that landed at 7.5 s was not credited")
	}
	if len(r.eats) != clicks {
		t.Fatalf("a late landing was eaten again: %d clicks, want %d", len(r.eats), clicks)
	}
	if r.inFlight() {
		t.Fatal("the credited bite is still in flight")
	}
	if !r.said("landed 8s after its click") || !r.said("a late landing, credited and not eaten again") {
		t.Fatalf("the credit was not said: %v", r.logs)
	}
}

// The same account the next bite on: four bites, each landing late, each credited by the following call, one click apiece.
func TestAChainOfLateBitesIsFourClicksAndNoFailure(t *testing.T) {
	r := newBiteRig(13, 53)
	r.latency = 8 * time.Second
	for want := 17; want <= 29; want += 4 {
		EatBite(r.env(), r.req("Cake")) // false at six seconds, in flight
		if !r.inFlight() {
			t.Fatalf("bite to %d not in flight", want)
		}
		r.Delay(3 * time.Second)
		if r.hp() != want {
			t.Fatalf("hitpoints %d, want %d", r.hp(), want)
		}
		if !EatBite(r.env(), r.req("Cake")) {
			t.Fatalf("the bite to %d was not credited", want)
		}
	}
	if len(r.eats) != 4 {
		t.Fatalf("%d clicks for four bites: a landing was eaten again", len(r.eats))
	}
}

// Above the urgent line the next call waits for the bite in flight, up to its watch window, rather than click again.
func TestABiteInFlightIsWaitedForWhileHitpointsAllowIt(t *testing.T) {
	r := newBiteRig(30, 53) // 57%
	r.latency = 9 * time.Second
	if EatBite(r.env(), r.req("Bass")) {
		t.Fatal("not shown at six seconds, yet confirmed")
	}
	started := r.clock
	if !EatBite(r.env(), r.req("Bass")) {
		t.Fatal("the bite landing at 9 s was not waited for")
	}
	if len(r.eats) != 1 {
		t.Fatalf("%d clicks: the wait for the bite in flight clicked again", len(r.eats))
	}
	if took := r.clock - started; took > 4*time.Second {
		t.Fatalf("waited %s for a bite due 3 s later", took)
	}
}

// A bite the engine dropped never lands. Above the urgent line it is waited for until its window closes, then called lost: the
// call returns false with nothing in flight, which is what a caller records as a failed bite. The next call clicks again.
func TestABiteThatNeverLandsIsLostAfterTheWindow(t *testing.T) {
	r := newBiteRig(30, 53)
	r.landAt = func(time.Duration) time.Duration { return -1 }
	if EatBite(r.env(), r.req("Bass")) {
		t.Fatal("a dropped bite was confirmed")
	}
	if !r.inFlight() {
		t.Fatal("not in flight right after the six seconds")
	}
	if EatBite(r.env(), r.req("Bass")) {
		t.Fatal("a dropped bite was credited")
	}
	if r.inFlight() {
		t.Fatal("a bite that outlived its window is still in flight: the caller would never record the failure")
	}
	if r.clock < BiteWatchWindow || r.clock > BiteWatchWindow+time.Second {
		t.Fatalf("the bite was called lost at %s, want about %s after its click", r.clock, BiteWatchWindow)
	}
	if !r.said("timed out waiting for the bite to register") {
		t.Fatalf("the loss was not said: %v", r.logs)
	}
	if len(r.eats) != 1 {
		t.Fatalf("%d clicks before the loss was called", len(r.eats))
	}
	r.landAt = nil
	r.latency = time.Second
	if !EatBite(r.env(), r.req("Bass")) || len(r.eats) != 2 {
		t.Fatalf("the bite after a loss: clicks %d", len(r.eats))
	}
}

// Safety, unchanged: with the hitpoints low the account does not wait for a bite in flight, it eats again at once, as every eater
// did before the ledger.
func TestAnUrgentBiteInFlightIsNotWaitedFor(t *testing.T) {
	r := newBiteRig(13, 53) // 24%
	r.landAt = func(time.Duration) time.Duration { return -1 }
	EatBite(r.env(), r.req("Bass"))
	before := r.clock
	r.Delay(time.Second)
	EatBite(r.env(), r.req("Bass"))
	if len(r.eats) != 2 {
		t.Fatalf("%d clicks: at 13/53 the second bite should have gone out", len(r.eats))
	}
	if gap := r.eats[1] - before; gap > 1500*time.Millisecond {
		t.Fatalf("the second bite waited %s", gap)
	}
	if !r.said("not waiting for the bite of Bass") {
		t.Fatalf("the resend was not said: %v", r.logs)
	}
}

// The wait for a bite in flight ends the moment the account turns urgent: a hit lands three seconds in, and the next bite goes out
// then, not at the end of the window.
func TestTheWaitForABiteInFlightEndsWhenTheAccountIsHurt(t *testing.T) {
	r := newBiteRig(40, 53)
	r.landAt = func(time.Duration) time.Duration { return -1 }
	EatBite(r.env(), r.req("Bass"))
	r.at(r.clock+2*time.Second, func() { r.stat(30) }) // a monster hits for ten
	started := r.clock
	r.landAt = func(time.Duration) time.Duration { return -1 }
	EatBite(r.env(), r.req("Bass"))
	if len(r.eats) != 2 {
		t.Fatalf("%d clicks: the wait went on while the account was being hurt", len(r.eats))
	}
	if gap := r.eats[1] - started; gap > 3*time.Second {
		t.Fatalf("the second bite went out %s after the call began, want within 3 s of the hit", gap)
	}
	if !r.said("not waiting any longer") {
		t.Fatalf("the early end was not said: %v", r.logs)
	}
}

// Taking damage while a bite is out is urgent whatever the percentage: the account is being hurt.
func TestHurtWhileABiteIsOutIsUrgent(t *testing.T) {
	r := newBiteRig(30, 53)
	r.landAt = func(time.Duration) time.Duration { return -1 }
	EatBite(r.env(), r.req("Bass"))
	r.stat(26) // a guard hit
	EatBite(r.env(), r.req("Bass"))
	if len(r.eats) != 2 {
		t.Fatalf("%d clicks: an account being hurt must not wait for a bite that has not shown", len(r.eats))
	}
}

// In a fight the hitpoints hover (a hit, a heal, a hit), so they can stand at or above the baseline while something is still
// swinging: a hit in the last ten ticks is urgent too, however healthy the percentage reads.
func TestAHitInTheLastTenTicksIsUrgent(t *testing.T) {
	r := newBiteRig(40, 53) // 75%: not urgent on hitpoints
	r.landAt = func(time.Duration) time.Duration { return -1 }
	EatBite(r.env(), r.req("Bass"))
	underFire = func(*gamestate.State) bool { return true }
	t.Cleanup(func() { underFire = func(st *gamestate.State) bool { return st.InCombat() } })
	EatBite(r.env(), r.req("Bass"))
	if len(r.eats) != 2 {
		t.Fatalf("%d clicks: an account that is being hit must not wait for a bite that has not shown", len(r.eats))
	}
}

// Under fire the heal can be eaten up between samples; the food leaving its slot is the evidence that does not depend on it.
func TestTheFoodLeavingItsSlotCreditsALateLandingWhateverTheHitpointsDid(t *testing.T) {
	r := newBiteRig(30, 53)
	r.landAt = func(time.Duration) time.Duration { return -1 }
	EatBite(r.env(), r.req("Cake"))
	r.Delay(time.Second)
	r.stat(26)     // a guard's hit ate the heal: lower than at the click
	*r.pick = true // the cake shows as a 2/3 cake
	clicks := len(r.eats)
	if !EatBite(r.env(), r.req("Cake")) {
		t.Fatal("the food had changed form, yet the bite was not credited")
	}
	if len(r.eats) != clicks {
		t.Fatal("clicked again over a bite the pack showed")
	}
}

// Regeneration is a point a minute: one point of hitpoints is not evidence of a bite. The bite is waited for, never credited on
// that alone, and called lost at the end of its window.
func TestOnePointOfRegenerationIsNotACredit(t *testing.T) {
	r := newBiteRig(30, 53)
	r.landAt = func(time.Duration) time.Duration { return -1 }
	EatBite(r.env(), r.req("Cake"))
	r.at(r.clock+3*time.Second, func() { r.stat(31) })
	if EatBite(r.env(), r.req("Cake")) {
		t.Fatal("a rise of one point credited a bite that never landed")
	}
	if len(r.eats) != 1 {
		t.Fatalf("%d clicks", len(r.eats))
	}
}

// A record older than its window cannot be told from anything else that moved the hitpoints or the pack: forgotten, silently, and
// the new bite goes out.
func TestAnOldBiteIsForgottenNotCredited(t *testing.T) {
	r := newBiteRig(30, 53)
	r.landAt = func(time.Duration) time.Duration { return -1 }
	EatBite(r.env(), r.req("Cake"))
	r.Delay(2 * time.Minute)
	r.stat(40) // a potion, a long rest
	r.landAt = nil
	r.latency = time.Second
	r.logs = nil
	if !EatBite(r.env(), r.req("Cake")) || len(r.eats) != 2 {
		t.Fatalf("clicks %d", len(r.eats))
	}
	if r.said("landed") {
		t.Fatalf("a two minute old bite was credited: %v", r.logs)
	}
}

// A script told to stop leaves the wait at once and the bite where it was.
func TestAStoppedWaitReturnsFalseAndKeepsTheBite(t *testing.T) {
	r := newBiteRig(30, 53)
	r.landAt = func(time.Duration) time.Duration { return -1 }
	r.f.afterSend = func(*fakeSession) { r.onClick(); r.stopped = true }
	if EatBite(r.env(), r.req("Cake")) {
		t.Fatal("a stopped wait confirmed the bite")
	}
	if !r.inFlight() {
		t.Fatal("a stopped wait dropped the bite")
	}
}

// With the hitpoints unread there is no baseline: only the pack can confirm, and nothing crashes.
func TestUnreadHitpointsConfirmByThePackAlone(t *testing.T) {
	r := &biteRig{f: newFakeSession(), base: time.Date(2026, 10, 15, 13, 6, 0, 0, time.UTC), heal: 4}
	r.f.afterSend = func(*fakeSession) { r.onClick() }
	r.latency = time.Second
	if !EatBite(r.env(), r.req("Cake")) {
		t.Fatal("the food left its slot, yet the bite was not confirmed")
	}
}

// ---- the delay ----

// The engine drops an eat read while the player is delayed (OpHeldHandler). A High Level Alchemy cast's p_delay(3) holds the
// next four ticks. An eat sent the moment the cast went out is lost; EatBite, told of the cast, waits for the horizon.
func TestABiteWaitsOutTheDelayACastHolds(t *testing.T) {
	r := newBiteRig(20, 53)
	r.latency = time.Second
	r.serverDelayedUntil = 3 * time.Second
	NoteDelay(r.f, r.base.Add(3*time.Second))
	if !EatBite(r.env(), r.req("Cake")) {
		t.Fatalf("the bite sent after the delay was not confirmed: eats %v arrived %v", r.eats, r.arrived)
	}
	if len(r.arrived) != 1 || r.arrived[0] < 3*time.Second {
		t.Fatalf("the click went out at %v (arrived %v): inside the delay the engine drops it", r.eats, r.arrived)
	}
	if !r.said("held for another") {
		t.Fatalf("the wait was not said: %v", r.logs)
	}
}

// Control: with no note of the cast the same click goes out at once, is dropped, and the bite is a loss -- the failure the log showed.
func TestWithoutTheNoteTheClickInsideTheDelayIsDropped(t *testing.T) {
	r := newBiteRig(20, 53)
	r.latency = time.Second
	r.serverDelayedUntil = 3 * time.Second
	if EatBite(r.env(), r.req("Cake")) {
		t.Fatal("a click inside the delay was confirmed")
	}
	if len(r.arrived) != 0 || len(r.eats) != 1 {
		t.Fatalf("eats %v arrived %v: the click should have been sent and dropped", r.eats, r.arrived)
	}
}

// The wait for a delay is bounded: a horizon far away holds a bite no longer than BiteDelayWaitMax.
func TestTheWaitForADelayIsBounded(t *testing.T) {
	r := newBiteRig(20, 53)
	r.latency = time.Second
	NoteDelay(r.f, r.base.Add(time.Minute))
	EatBite(r.env(), r.req("Cake"))
	if len(r.eats) != 1 || r.eats[0] > BiteDelayWaitMax+100*time.Millisecond {
		t.Fatalf("clicks at %v, want one within %s", r.eats, BiteDelayWaitMax)
	}
}

func TestADelayNoteKeepsTheLaterTimeAndIsPerSession(t *testing.T) {
	a, b := newBiteRig(20, 53), newBiteRig(20, 53)
	NoteDelay(a.f, a.base.Add(5*time.Second))
	NoteDelay(a.f, a.base.Add(2*time.Second))
	if got := DelayLeft(a.f, a.base); got != 5*time.Second {
		t.Errorf("an earlier note shortened the delay: %v", got)
	}
	if got := DelayLeft(b.f, b.base); got != 0 {
		t.Errorf("another account's cast held this one for %v", got)
	}
	if got := DelayLeft(a.f, a.base.Add(6*time.Second)); got != 0 {
		t.Errorf("a delay in the past still holds: %v", got)
	}
}

// Every item spell the walks cast notes its delay: High Level Alchemy holds four ticks, the others two, each with a tick over for the click's trip.
func TestCastOnItemNotesTheDelayItHolds(t *testing.T) {
	cases := []struct {
		spell string
		want  int // ticks
	}{
		{"High Level Alchemy", 5},
		{"Low Level Alchemy", 3},
		{"Superheat Item", 3},
		{"Lvl-1 Enchant", 3},
	}
	for _, c := range cases {
		f := newFakeSession()
		if err := CastOnItem(f, c.spell, 995, 4, PlayerPack); err != nil {
			t.Fatal(err)
		}
		left := DelayLeft(f, time.Now())
		want := time.Duration(c.want) * sleepTick
		if left > want || left < want-time.Second {
			t.Errorf("%s noted a delay of %v, want about %v", c.spell, left, want)
		}
	}
	f := newFakeSession()
	f.sendErr = errBoom
	if err := CastOnItem(f, "High Level Alchemy", 995, 4, PlayerPack); err == nil {
		t.Fatal("a failed send returned nil")
	}
	if DelayLeft(f, time.Now()) != 0 {
		t.Error("a cast that was never sent held the player")
	}
}

// ---- the settle pass ----

func TestSettleBiteCreditsALandingOnceAndLogsIt(t *testing.T) {
	r := newBiteRig(13, 53)
	r.latency = 8 * time.Second
	EatBite(r.env(), r.req("Bread"))
	r.Delay(3 * time.Second)
	SettleBite(r.env())
	if r.inFlight() {
		t.Fatal("a landed bite is still in flight after the settle pass")
	}
	n := len(r.logs)
	SettleBite(r.env())
	if len(r.logs) != n {
		t.Fatalf("the second settle said %v again", r.logs[n:])
	}
	if !r.said("landed") || !r.said("credited and not eaten again") {
		t.Fatalf("the credit was not said: %v", r.logs)
	}
}

func TestSettleBiteLogsABiteThatNeverShowedOnceItsWindowIsOut(t *testing.T) {
	r := newBiteRig(30, 53)
	r.landAt = func(time.Duration) time.Duration { return -1 }
	EatBite(r.env(), r.req("Bread"))
	r.Delay(BiteWatchWindow)
	r.logs = nil
	SettleBite(r.env())
	if r.inFlight() || !r.said("never showed") {
		t.Fatalf("in flight %v, logs %v", r.inFlight(), r.logs)
	}
}

func TestBiteInFlightIsFalseForASessionThatNeverAte(t *testing.T) {
	r := newBiteRig(30, 53)
	if _, ok := BiteInFlightAt(r.f.state, r.now()); ok {
		t.Fatal("a session that never ate has a bite in flight")
	}
	if _, ok := BiteInFlightAt(nil, r.now()); ok {
		t.Fatal("a nil state has a bite in flight")
	}
}

// The ledger is per session: one account's bite is never another's.
func TestTheLedgerIsPerSession(t *testing.T) {
	a, b := newBiteRig(13, 53), newBiteRig(13, 53)
	a.landAt = func(time.Duration) time.Duration { return -1 }
	EatBite(a.env(), a.req("Cake"))
	if _, ok := BiteInFlightAt(b.f.state, b.now()); ok {
		t.Fatal("a bite of another account is in flight here")
	}
}

// The pick is made after the waits, so a slot a landed bite has just emptied is never named.
func TestThePickIsReadAfterTheWaits(t *testing.T) {
	r := newBiteRig(30, 53)
	r.latency = time.Second
	NoteDelay(r.f, r.base.Add(2*time.Second))
	var pickedAt time.Duration = -1
	req := BiteRequest{Food: "Cake", Pick: func() (BitePick, bool) {
		pickedAt = r.clock
		return BitePick{Obj: 1891, Slot: 3, Changed: func() bool { return false }}, true
	}}
	EatBite(r.env(), req)
	if pickedAt < 2*time.Second {
		t.Fatalf("the food was picked at %v, before the delay was over", pickedAt)
	}
}

// A pick that finds no food sends nothing and records nothing.
func TestNoFoodSendsNothing(t *testing.T) {
	r := newBiteRig(30, 53)
	req := BiteRequest{Food: "Cake", Pick: func() (BitePick, bool) { return BitePick{}, false }}
	if EatBite(r.env(), req) || len(r.eats) != 0 || r.inFlight() {
		t.Fatalf("eats %v, in flight %v", r.eats, r.inFlight())
	}
}

func TestLogSlowBiteSpeaksOnlyForABiteTheOldWaitWouldHaveFailed(t *testing.T) {
	var lines []string
	logf := func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }
	LogSlowBite(logf, "Shrimps", 900*time.Millisecond)
	if len(lines) != 0 {
		t.Fatalf("a quick bite was logged: %v", lines)
	}
	LogSlowBite(logf, "Shrimps", 3140*time.Millisecond)
	if len(lines) != 1 || !strings.Contains(lines[0], "3.1s") {
		t.Fatalf("a slow bite logged %v", lines)
	}
	LogSlowBite(nil, "Shrimps", time.Minute)
}
