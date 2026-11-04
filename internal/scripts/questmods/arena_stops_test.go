package questmods

import (
	"testing"

	"rsb0t-go/internal/scripts/questplan"
)

// arena-empty-1 parked on a talk step the engine refused as malformed.
func TestArenaTalkStopsAreWellFormed(t *testing.T) {
	for _, st := range []questplan.NpcStop{hengradStop, drunkStop, jeremyStop} {
		if why := questplan.ValidateStep(questplan.Step{Kind: questplan.StepTalk, Stop: st}); why != "" {
			t.Errorf("%s: %s", st.Npc, why)
		}
	}
}
