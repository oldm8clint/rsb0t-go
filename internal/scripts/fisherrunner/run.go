package fisherrunner

import "rsb0t-go/internal/actions"

// This file wires the persistent run toggle (actions.SetRun / gamestate.State.RunEnabled,
// the controls tab's "Run" button — see actions/controls.go's own doc comment for the real
// IfButton ids) into WalkNear, so ordinary fishing travel moves at running speed whenever
// there's energy to spare, instead of always walking at normal speed. Confirmed via grep
// before this was written: no script anywhere in the fleet used SetRun for cruising travel
// — randomevents/flee_walking.go's wantRunForFlee is the only other caller, and that one is
// deliberately an emergency policy (spend down to the very last point of energy to escape a
// hostile), not a fit for ordinary walking between fish and bank.
//
// Position tracking while running was the reason WalkNear's own doc comment gives for never
// running in the first place ("per the position-tracking drift bug on running clicks
// already documented in COLLAB.md/barbarianrunner") — but that bug (REBUILD_NORMAL not
// shifting the tracked local player/NPCs/players by the new build-area origin) was root-
// caused and fixed in `e4e59a6` (internal/gamestate/position.go's applyRebuildNormal) and
// has since been live-verified: COLLAB.md's 2026-09-15 fisher entry records "two running
// walks crossing five build-area rebuilds, Tile() matching ::getcoord at every one of 152
// checks", and randomevents/flee_walking.go's own combat-flee path has been running (and
// production-proven) against real drift ever since. So turning running on here for cruising
// travel, not just emergency flees, is no longer the known-bad idea WalkNear's comment
// describes — it's the same primitive already proven safe elsewhere in this codebase.
//
// Deliberately one-directional: this only ever turns run ON, never OFF. A fishing bot has
// no reason to conserve run energy by walking on purpose — energy regenerates for free over
// time regardless of what the account does, and the server itself already disables run
// automatically once energy hits 0%, so there is nothing left for this code to do at the
// low end beyond simply not fighting that.

// RunEnergyThreshold is the run-energy percentage (gamestate.State.Energy's own 0-100
// scale) at or above which WalkNear turns running on.
//
// 1, the owner's ruling of 2026-09-25 (it was 20, their earlier "roughly 20%" ask). Fishers are
// energy-bound (energy sits at 90%+ in 2.47% of live samples), and a fishing bot has no threat
// to keep a reserve for, so every point between 1 and 20 was being held back from a walk while
// the account walked the same tiles anyway. The server turns running off at 0%, and this turns it
// back on from the first regenerated point; the cost is an extra run-button packet each time
// that happens mid-leg.
//
// A var, not a const, so cmd/labfleettest -mode rung-rate -run-floor 20 can run the old policy as
// the control arm on the same build. Exported so cmd/fisherruntest drives its own waits off the
// exact same value this package acts on. freshstartrunner.RunEnergyThreshold (the shared walk
// path for every other script) is separate and still 20.
var RunEnergyThreshold = 1

// wantRunWhileWalking decides whether WalkNear should call actions.SetRun(true) right now.
// Pure and unit-testable, mirroring randomevents.wantRunForFlee's own (runOn, energy,
// energyOK) shape: false once run is already on (nothing to do — and critically, this is
// what stops WalkNear from re-clicking the run button every poll tick once it's already
// enabled), false while energy is unknown (no UPDATE_RUNENERGY observed yet — don't assume
// it's safe), and true once energy has been observed at or above RunEnergyThreshold.
func wantRunWhileWalking(runOn bool, energy int, energyOK bool) bool {
	if runOn {
		return false
	}
	return energyOK && energy >= RunEnergyThreshold
}

// maybeEnableRun turns running on via actions.SetRun when wantRunWhileWalking says it's
// worth it. A failed SetRun is logged but never treated as fatal — WalkNear keeps walking
// (just not running) exactly as it did before this file existed, the same "best effort"
// spirit as the identical SetRun(true) call in randomevents/flee_walking.go.
func maybeEnableRun(d *Deps) {
	energy, ok := d.Session.State().Energy()
	if !wantRunWhileWalking(d.Session.State().RunEnabled(), energy, ok) {
		return
	}
	if err := actions.SetRun(d.Session, true); err != nil {
		d.logf("enable run: %v", err)
	}
}
