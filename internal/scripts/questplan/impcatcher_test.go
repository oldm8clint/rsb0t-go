package questplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestImpCatcherSteps(t *testing.T) {
	s := Snapshot{Journal: StatusNotStarted, InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 20}
	if st := DecideImpCatcher(s); st.Kind != StepTalk || st.Stop.ID != NpcMizgog {
		t.Fatalf("not started: %s", st.Describe())
	}
	s.Journal = StatusInProgress
	st := DecideImpCatcher(s)
	if st.Kind != StepProvision || len(st.Kit) != 4 || ValidateStep(st) != "" {
		t.Fatalf("beads: %s (%s)", st.Describe(), ValidateStep(st))
	}
	for _, b := range ImpBeads {
		s.InvIDs[b] = 1
	}
	if st := DecideImpCatcher(s); st.Kind != StepTalk {
		t.Fatalf("hand-in: %s", st.Describe())
	}
	delete(s.InvIDs, ObjRedBead)
	s.FreeSlots = 1
	if st := DecideImpCatcher(s); st.Kind != StepDeposit {
		t.Fatalf("a full pack banks first: %s", st.Describe())
	}
}

// A8: with an EMPTY mule the beads fall to the account's own skill (imp farming), never a mule
// walk; with a mule that publishes beads they come in one ask.
func TestImpCatcherBeadsByTheItemRule(t *testing.T) {
	p := PlanProvision(impKit(), ProvisionState{BankKnown: true, MuleKnown: true, FreeSlots: 20, Levels: map[string]int{"attack": 30}})
	if p.Phase != PhaseSource || p.Tasks[0].Source.Method != MethodImpFarm || len(p.MuleAsk) != 0 {
		t.Fatalf("empty mule: %s", p.Summary())
	}
	p = PlanProvision(impKit(), ProvisionState{BankKnown: true, MuleKnown: true, FreeSlots: 20,
		Mule: map[int]int{ObjRedBead: 3, ObjBlackBead: 1}, Levels: map[string]int{"attack": 30}})
	if p.Phase != PhaseMule || len(p.MuleAsk) != 2 {
		t.Fatalf("stocked mule: %s", p.Summary())
	}
}

// The bank visits of the beads' provisioning go to the bank nearest the account, never across the sea to Draynor: every bank
// holds the same items, the imps are at Ardougne's south field, and the walk to 3093,3243 from Kandarin or Karamja was where
// 5 of the quest's 6 deaths and 14 of its 30 parks since the door fix happened (live 4-8 Oct 2026).
func TestImpCatcherBanksAtTheNearestBank(t *testing.T) {
	ardougneSouth := tile.Tile{X: 2655, Z: 3286}
	cases := []struct {
		name string
		at   tile.Tile
		want tile.Tile
	}{
		{"the imp field", ImpField, ardougneSouth},
		{"the Ardougne market", tile.Tile{X: 2661, Z: 3306}, ardougneSouth},
		{"where the Fisher King's whistle sets a Karamja account down (2742,3236)", tile.Tile{X: 2742, Z: 3236}, ardougneSouth},
		{"the Karamja south coast", tile.Tile{X: 2830, Z: 3186}, ardougneSouth},
		{"Draynor", tile.Tile{X: 3100, Z: 3250}, DraynorBank},
		{"the Wizards' Tower", tile.Tile{X: 3103, Z: 3163}, DraynorBank},
		{"Port Sarim", tile.Tile{X: 3029, Z: 3217}, DraynorBank},
	}
	for _, c := range cases {
		s := Snapshot{Journal: StatusInProgress, InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 20, Tile: c.at, TileKnown: true}
		st := DecideImpCatcher(s)
		if st.Kind != StepProvision || !st.BankSet || st.Bank != c.want {
			t.Errorf("%s: the beads' provisioning banks at %v (set %v), want %v: %s", c.name, st.Bank, st.BankSet, c.want, st.Describe())
		}
		s.FreeSlots = 1
		st = DecideImpCatcher(s)
		if st.Kind != StepDeposit || !st.BankSet || st.Bank != c.want {
			t.Errorf("%s: a full pack banks at %v (set %v), want %v: %s", c.name, st.Bank, st.BankSet, c.want, st.Describe())
		}
	}

	// An unread position keeps Draynor, the TS def's bank.
	s := Snapshot{Journal: StatusInProgress, InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 20}
	if st := DecideImpCatcher(s); st.Bank != DraynorBank || !st.BankSet {
		t.Errorf("no position: banks at %v (set %v), want Draynor", st.Bank, st.BankSet)
	}
	// The module forces no bank of its own: a lost fight retreats to the nearest bank (Deps.retreatDest), not to Draynor.
	if ImpCatcher.BankSet {
		t.Error("the Imp Catcher module forces Draynor's bank on a lost fight's retreat")
	}
}
