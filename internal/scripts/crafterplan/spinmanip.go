package crafterplan

// SPIN MANIPULATION: the Spin menu's batch against the one-flax-at-a-time use on the wheel.
//
// Content 289 (skill_crafting/scripts/spinning/spinning.rs2) has two ways to turn a flax into a bow string:
//
//   - [oplocu,_spinning_wheel] with a flax (the way ProgressiveCrafter has always done it) runs [label,craft_spinning]:
//     one flax is spun at once and the script ends in p_delay(2). The engine drops an OPLOCU it reads while the player is
//     delayed (OpLocUHandler), so one flax is spun every THREE ticks at best, and every FOUR live (strings 2.4 s apart on
//     7 Oct 2026: a 1.9 s wait after each string plus the link's lateness).
//   - [oploc2,_spinning_wheel] "Spin" opens ~skill_multi2_header ("What would you like to spin?": Wool or Flax, Make 1 / 5 /
//     10 / X each), and the chosen count is spun by a self-re-queueing WEAK queue: weakqueue(crafting_spinning, 3) the
//     first time and weakqueue(crafting_spinning, 2) for every flax after it ([label,process_spin]), with no p_delay at all.
//     So the batch spins one flax every TWO ticks after a start-up of three, whatever the link does, and sends nothing
//     between flax. The same wheel, the same 15 xp a flax, 1.5 times the rate of the best a use-on-loop could do and about
//     twice the rate it does.
//
// A weak queue is dropped by anything that closes the modal (Player.closeModal(clearWeakQueue = true): a move click, any
// op click, a CLOSE_MODAL) and does not run while a modal is open (a level-up dialogue), so the runner stands still at the
// wheel, answers a level-up with Continue, and writes the batch again whenever it finds the pack still holding flax and no
// strings coming.
//
// SpinGuard is the live measure of what that is worth on this account, in the unit the fleet's other manipulations log:
// strings per 100 ticks, with the batch against the plain use measured on the same account. The first plain window comes
// first (SpinPlainWindow flax spun the ordinary way), then batches; another plain window follows every SpinResampleEvery
// batched flax so the baseline stays the account's own. A batch that is not clearly better (SpinGuardRatio) after
// SpinGuardSpins flax, or a menu that will not open SpinMenuFails times in a row, puts the plain use back for SpinDisableFor.
// Pure: the runner reads the ticks and the pack.

import (
	"fmt"
	"time"
)

const (
	// SpinMenuOpName is the wheel's op that opens the Spin menu; content's [oploc2,_spinning_wheel].
	SpinMenuOpName = "Spin"
	// SpinPlainWindow is how many flax are spun the plain way to measure the baseline, at the start and after every resample.
	SpinPlainWindow = 6
	// SpinResampleEvery is how many batched flax go between two plain windows (about 9 packs: roughly 15 minutes of a cycle).
	SpinResampleEvery = 250
	// SpinGuardSpins is how many batched flax the guard waits for before it judges.
	SpinGuardSpins = 24
	// SpinGuardRatio is the share of the plain rate the batched rate must reach.
	SpinGuardRatio = 0.85
	// SpinMenuFails is how many batches in a row that never started (no menu, no strings) turn the manipulation off.
	SpinMenuFails = 3
	// SpinDisableFor is how long the manipulation stays off after the guard or the menu failures stop it.
	SpinDisableFor = 20 * time.Minute
)

// SpinGuard is one account's measured spin rates and its on/off state.
type SpinGuard struct {
	// On is the spinManip setting; an Off guard is the plain use always.
	On bool

	// PlainSpins/PlainTicks and BatchSpins/BatchTicks are the cumulative flax and ticks of each mode; sinceResample counts the
	// batched flax since the last plain window began, plainLeft the plain flax still to spin in the current window.
	PlainSpins, PlainTicks int
	BatchSpins, BatchTicks int
	sinceResample          int
	plainLeft              int
	menuFails              int
	disabledUntil          time.Time
	// reported* mark what the last log line already covered.
	reportedBatchSpins int
	reportedAt         time.Time
}

// NewSpinGuard returns a guard; on false never batches.
func NewSpinGuard(on bool) *SpinGuard { return &SpinGuard{On: on, plainLeft: SpinPlainWindow} }

// WantBatch reports whether the next flax should go through the Spin menu: the manipulation is on, not stopped, and not
// inside a plain window.
func (g *SpinGuard) WantBatch(now time.Time) bool {
	if g == nil || !g.On {
		return false
	}
	if now.Before(g.disabledUntil) {
		return false
	}
	if g.plainLeft > 0 {
		return false
	}
	if g.sinceResample >= SpinResampleEvery {
		g.sinceResample, g.plainLeft = 0, SpinPlainWindow
		return false
	}
	return true
}

// DisabledFor is how long the manipulation stays off from now, zero when it is not.
func (g *SpinGuard) DisabledFor(now time.Time) time.Duration {
	if g == nil || !now.Before(g.disabledUntil) {
		return 0
	}
	return g.disabledUntil.Sub(now)
}

// RecordPlain notes spins flax spun the plain way in ticks ticks, counting down the plain window.
func (g *SpinGuard) RecordPlain(spins, ticks int) {
	if g == nil || spins <= 0 || ticks <= 0 {
		return
	}
	g.PlainSpins += spins
	g.PlainTicks += ticks
	if g.plainLeft > 0 {
		g.plainLeft = max(g.plainLeft-spins, 0)
	}
}

// RecordBatch notes a batch that spun spins flax in ticks ticks (from the Spin op to the last string) and judges it: it
// returns a non-empty reason when the manipulation was just stopped. A batch that spun nothing is a menu failure.
func (g *SpinGuard) RecordBatch(spins, ticks int, now time.Time) (stopped string) {
	if g == nil || !g.On {
		return ""
	}
	if spins <= 0 || ticks <= 0 {
		return g.recordMenuFail(now)
	}
	g.menuFails = 0
	g.BatchSpins += spins
	g.BatchTicks += ticks
	g.sinceResample += spins
	plain, batch := g.PlainRate(), g.BatchRate()
	if g.BatchSpins >= SpinGuardSpins && g.PlainSpins >= SpinPlainWindow-1 && plain > 0 && batch < SpinGuardRatio*plain {
		g.disabledUntil = now.Add(SpinDisableFor)
		reason := fmt.Sprintf("the Spin menu spun %.1f flax per 100 ticks against %.1f plain, under %.0f%% of it", batch, plain, 100*SpinGuardRatio)
		g.BatchSpins, g.BatchTicks, g.sinceResample = 0, 0, 0
		g.plainLeft = SpinPlainWindow
		return reason
	}
	return ""
}

// RecordMenuFail notes a batch that never started (the menu did not open or offer flax, or no string followed): after
// SpinMenuFails in a row the manipulation stops. It returns the reason when it did.
func (g *SpinGuard) RecordMenuFail(now time.Time) string {
	if g == nil || !g.On {
		return ""
	}
	return g.recordMenuFail(now)
}

func (g *SpinGuard) recordMenuFail(now time.Time) string {
	g.menuFails++
	if g.menuFails < SpinMenuFails {
		return ""
	}
	g.menuFails = 0
	g.disabledUntil = now.Add(SpinDisableFor)
	g.plainLeft = SpinPlainWindow
	return fmt.Sprintf("%d Spin menus in a row gave no strings", SpinMenuFails)
}

// PlainRate and BatchRate are flax per 100 ticks, 0 with no sample.
func (g *SpinGuard) PlainRate() float64 { return per100(g.PlainSpins, g.PlainTicks) }
func (g *SpinGuard) BatchRate() float64 { return per100(g.BatchSpins, g.BatchTicks) }

func per100(n, ticks int) float64 {
	if ticks <= 0 {
		return 0
	}
	return 100 * float64(n) / float64(ticks)
}

// SpinReportEvery is the least time between two log lines of a running manipulation.
const SpinReportEvery = 5 * time.Minute

// Line is the log line the lead greps for: "crafter manip: 54 flax in 118 ticks with the Spin menu (45.8 per 100) vs plain
// 6 in 25 (24.0 per 100) = x1.9". ok is false when there is nothing new to say (no batched flax since the last line, or
// the last line is under SpinReportEvery old and force is false).
func (g *SpinGuard) Line(now time.Time, force bool) (line string, ok bool) {
	if g == nil || g.BatchSpins <= g.reportedBatchSpins {
		return "", false
	}
	if !force && !g.reportedAt.IsZero() && now.Sub(g.reportedAt) < SpinReportEvery {
		return "", false
	}
	g.reportedBatchSpins, g.reportedAt = g.BatchSpins, now
	vs := "no plain baseline yet"
	if g.PlainSpins > 0 {
		vs = fmt.Sprintf("plain %d in %d (%.1f per 100)", g.PlainSpins, g.PlainTicks, g.PlainRate())
		if g.PlainRate() > 0 {
			vs += fmt.Sprintf(" = x%.1f", g.BatchRate()/g.PlainRate())
		}
	}
	return fmt.Sprintf("crafter manip: %d flax in %d ticks with the Spin menu (%.1f per 100) vs %s", g.BatchSpins, g.BatchTicks, g.BatchRate(), vs), true
}
