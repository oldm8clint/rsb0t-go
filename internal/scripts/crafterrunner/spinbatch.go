package crafterrunner

// The Spin menu's batch (crafterplan/spinmanip.go has the content facts and the guard): the wheel's "Spin" op (OPLOC2) opens
// the What-would-you-like-to-spin menu, Make X with the number of flax held starts a weak-queue batch of one flax every two
// ticks, and the account stands still until the pack holds no flax. Every flax is a string either way; the batch only spins
// them faster than SpinFlaxUntil's one-use-at-a-time can.

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/crafterplan"
)

const (
	// spinMenuWait is how long the Spin menu has to open offering flax after the op (a step to the wheel, a tick or two).
	spinMenuWait = 5 * time.Second
	// spinFirstWait is how long after the count is answered the first string has: the batch's own start-up is three ticks.
	spinFirstWait = 4 * time.Second
	// spinQuiet is how long the strings may stop coming, flax still in the pack, before the batch is called ended: the gap
	// between two is two ticks (1.2 s), so this is more than three missed.
	spinQuiet = 3600 * time.Millisecond
	// spinBatchCap bounds one batch: 28 flax are 3 + 2*27 ticks, 35 s at the production tick.
	spinBatchCap = 90 * time.Second
	spinPoll     = 100 * time.Millisecond
)

// unnotedFlax is the flax the pack holds that can be spun: notes cannot.
func unnotedFlax(d *Deps) int { return max(FlaxCount(d)-NotedFlaxCount(d), 0) }

// spinBatch spins up to want flax through the Spin menu and reports how many strings came and whether the caller should
// stop (a stop was requested). The guard hears of the batch (strings per tick, or a menu failure).
func spinBatch(d *Deps, wheel locindex.Match, want int) (spun int, stop bool) {
	have := unnotedFlax(d)
	count := min(have, want)
	if count <= 0 {
		return 0, false
	}
	st := d.Session.State()
	w := Waiter(d)
	dlg := actions.NewChatDialog(d.Session, d.Components)
	stringsBefore := BowStringCount(d)
	fail := func(why string) (int, bool) {
		d.logf("crafter manip: the Spin menu: %s", why)
		if dlg.IsOpen() || dlg.IsMakeMenu() {
			_ = actions.CloseModal(d.Session)
		}
		if reason := d.SpinGuard.RecordMenuFail(time.Now()); reason != "" {
			d.logf("crafter manip: OFF for %s: %s — spinning one flax at a time meanwhile", crafterplan.SpinDisableFor, reason)
		}
		return max(BowStringCount(d)-stringsBefore, 0), false
	}

	// A bank closed a moment ago would close the menu this opens (gamestate/modalclose.go).
	if !actions.SettleAfterClose(d.Session, w) {
		return 0, true
	}
	tick0 := st.Tick()
	if err := actions.OpLoc(d.Session, wheel.Op, wheel.X, wheel.Z, wheel.ID); err != nil {
		return fail("the Spin op was refused: " + err.Error())
	}
	offers := func() bool { return dlg.Offers(d.Config, crafterplan.Flax) }
	if !waitQuiet(d, spinMenuWait, offers) {
		return fail("no menu offering flax opened (" + dlg.DescribeProducts(d.Config) + ")")
	}
	if err := dlg.MakeX(d.Config, crafterplan.Flax, int32(count), w); err != nil {
		return fail("Make X: " + err.Error())
	}

	// The batch runs by itself from here. Watch the strings, answer a level-up, and call it ended when the pack holds no flax
	// or the strings have stopped coming.
	deadline := time.Now().Add(spinBatchCap)
	last, lastMoved, lastTick := stringsBefore, time.Now(), tick0
	first := true
	for time.Now().Before(deadline) {
		if !d.Delay(spinPoll) {
			return max(BowStringCount(d)-stringsBefore, 0), true
		}
		now := BowStringCount(d)
		if now != last {
			last, lastMoved, lastTick, first = now, time.Now(), st.Tick(), false
		}
		if now-stringsBefore >= count || unnotedFlax(d) == 0 {
			break
		}
		if !randomevents.Busy(d.Session) && dlg.CanContinue() {
			// A level-up: the weak queue waits for the dialogue and the Continue ends it (and the batch). Never a dialogue of a random
			// event, which the Guardian answers its own way.
			_ = dlg.Continue()
			lastMoved = time.Now()
			continue
		}
		wait := spinQuiet
		if first {
			wait = spinFirstWait + spinQuiet
		}
		if time.Since(lastMoved) >= wait {
			break
		}
	}
	spun = max(BowStringCount(d)-stringsBefore, 0)
	ticks := lastTick - tick0
	if spun > 0 {
		if reason := d.SpinGuard.RecordBatch(spun, max(ticks, 1), time.Now()); reason != "" {
			d.logf("crafter manip: OFF for %s: %s — spinning one flax at a time meanwhile", crafterplan.SpinDisableFor, reason)
		}
		reportSpin(d)
	} else {
		_, _ = fail("no string came after the count was answered")
	}
	return spun, false
}

// waitQuiet is waitFor without the "timed out" line: a menu that does not open is the caller's own report.
func waitQuiet(d *Deps, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(spinPoll) {
			return false
		}
	}
	return false
}

// reportSpin logs the guard's measured line when it has something new and the last one is old enough: "crafter manip: N flax
// in M ticks with the Spin menu (X per 100) vs plain Y in T (Z per 100) = xR".
func reportSpin(d *Deps) {
	if line, ok := d.SpinGuard.Line(time.Now(), false); ok {
		d.logf("%s", line)
	}
}
