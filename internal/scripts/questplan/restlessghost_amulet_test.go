package questplan

import "testing"

// The Ghostspeak amulet this quest hands out must survive into Nature Spirit: the module lists it as a tool,
// and no bank trip it plans (a full pack at the skull, a worn amulet already on) leaves it behind.
func TestTheGhostModuleNeverBanksTheGhostspeakAmulet(t *testing.T) {
	has := func(ids []int, want int) bool {
		for _, id := range ids {
			if id == want {
				return true
			}
		}
		return false
	}
	if !has(RestlessGhost.ToolIDs, ObjGhostspeak) {
		t.Fatalf("the amulet is not a tool of the module: %v", RestlessGhost.ToolIDs)
	}
	for name, snap := range map[string]func() Snapshot{
		"a full pack with no amulet, held": func() Snapshot {
			s := ghostSnap(StatusInProgress)
			s.FreeSlots = 0
			return s
		},
		"a full pack at the skull, amulet worn": func() Snapshot {
			s := ghostSnap(StatusInProgress)
			s.WornIDs[ObjGhostspeak] = 1
			s.Stage, s.StageKnown = GhostSpokeGhost, true
			s.FreeSlots = 0
			return s
		},
	} {
		step := DecideRestlessGhost(snap())
		if step.Kind == StepDeposit && !has(step.KeepIDs, ObjGhostspeak) {
			t.Errorf("%s: a deposit that does not keep the amulet: %s", name, step.Describe())
		}
	}
}
