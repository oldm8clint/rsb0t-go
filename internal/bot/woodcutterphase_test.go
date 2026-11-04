package bot

import (
	"testing"

	"rsb0t-go/internal/phaselog"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// allWoodPhases is every WoodPhase DecideWoodPhase can return. Listed rather than derived
// because WoodPhase is a string type with no registry to iterate: if a new phase is added to
// woodcutplan and not added here, TestWoodcutterPhaseNameCoversEveryPhase below will not catch
// it — but the default arm keeps the panel showing "setup" rather than an uncoloured name, so
// the failure mode is a wrong label, never a crash.
var allWoodPhases = []woodcutplan.WoodPhase{
	woodcutplan.PhaseAccept, woodcutplan.PhaseTutorial, woodcutplan.PhaseWait,
	woodcutplan.PhaseBright, woodcutplan.PhaseStore, woodcutplan.PhaseStart,
	woodcutplan.PhaseFund, woodcutplan.PhaseTravel, woodcutplan.PhaseBank,
	woodcutplan.PhaseOrder, woodcutplan.PhaseWalk, woodcutplan.PhaseChop,
	woodcutplan.PhaseStuck,
}

// canonical is phaselog's own vocabulary. A phase name outside it still records, but the panel
// does not colour it consistently and it competes for one of the interning slots, so a typo
// here would be invisible in tests and quietly odd on screen.
var canonical = map[string]bool{
	phaselog.Fishing: true, phaselog.Banking: true, phaselog.Walking: true,
	phaselog.Handoff: true, phaselog.Recovering: true, phaselog.Stranded: true,
	phaselog.Shopping: true, phaselog.Setup: true, phaselog.Idle: true,
	phaselog.Trading: true, phaselog.Cooking: true, phaselog.Handout: true,
	phaselog.Looting: true, phaselog.Woodcutting: true, phaselog.Event: true,
}

func TestWoodcutterPhaseNameCoversEveryPhase(t *testing.T) {
	for _, p := range allWoodPhases {
		got := woodcutterPhaseName(p)
		if got == "" {
			t.Errorf("%q maps to the empty string, which the sampler reads as 'never reported'", p)
		}
		if !canonical[got] {
			t.Errorf("%q maps to %q, which is not one of phaselog's canonical names", p, got)
		}
	}
}

// The whole point: the productive phase must be its own name, not folded into something else.
// Reporting chopping as "setup" or "idle" would leave the timeline just as misleading as
// reporting nothing, which is the bug this replaced.
func TestWoodcutterReportsChoppingAsItsOwnPhase(t *testing.T) {
	if got := woodcutterPhaseName(woodcutplan.PhaseChop); got != phaselog.Woodcutting {
		t.Fatalf("PhaseChop maps to %q, want %q", got, phaselog.Woodcutting)
	}
	// And nothing else may claim it, or the timeline would over-report the productive share.
	for _, p := range allWoodPhases {
		if p == woodcutplan.PhaseChop {
			continue
		}
		if woodcutterPhaseName(p) == phaselog.Woodcutting {
			t.Errorf("%q also reports as woodcutting, which inflates the productive share", p)
		}
	}
}

// Each of these is a judgement recorded in woodcutterPhaseName's own doc comment, pinned so it
// is changed deliberately rather than by accident.
func TestWoodcutterPhaseNamesFollowWhatTheAccountIsDoing(t *testing.T) {
	for _, c := range []struct {
		phase woodcutplan.WoodPhase
		want  string
		why   string
	}{
		{woodcutplan.PhaseWalk, phaselog.Walking, "walking out to the tier anchor is walking"},
		{woodcutplan.PhaseTravel, phaselog.Walking, "the ship leg to Seers is travel, not chopping"},
		{woodcutplan.PhaseBank, phaselog.Banking, ""},
		{woodcutplan.PhaseOrder, phaselog.Handout, "asking the mule for an axe is the hand-out side of a hand-off"},
		{woodcutplan.PhaseFund, phaselog.Recovering, "pickpocketing for a fare means the account is broke or displaced"},
		{woodcutplan.PhaseStuck, phaselog.Stranded, "no axe and no mule: nothing it does by itself fixes this"},
		{woodcutplan.PhaseWait, phaselog.Idle, ""},
		{woodcutplan.PhaseTutorial, phaselog.Setup, ""},
		{woodcutplan.PhaseStore, phaselog.Setup, ""},
	} {
		if got := woodcutterPhaseName(c.phase); got != c.want {
			t.Errorf("%q maps to %q, want %q (%s)", c.phase, got, c.want, c.why)
		}
	}
}
