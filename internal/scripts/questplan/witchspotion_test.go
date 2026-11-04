package questplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func hettySnap() Snapshot {
	s := Snapshot{Journal: StatusInProgress, InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 20,
		BankKnown: true, BankIDs: map[int]int{}, Ran: map[string]bool{"provision:" + KitHetty: true}, Failed: map[string]bool{}}
	s.Stage, s.StageKnown = HettyStarted, true
	return s
}

func TestWitchsPotionSteps(t *testing.T) {
	check := func(name string, s Snapshot, kind StepKind, detail func(Step) bool) {
		t.Helper()
		step := DecideWitchsPotion(s)
		if why := ValidateStep(step); why != "" {
			t.Errorf("%s: malformed: %s", name, why)
		}
		if step.Kind != kind || (detail != nil && !detail(step)) {
			t.Errorf("%s: got %s", name, step.Describe())
		}
	}
	s := hettySnap()
	s.Journal = StatusNotStarted
	check("not started", s, StepTalk, func(st Step) bool { return st.Stop.ID == NpcHetty })

	s = hettySnap()
	s.Ran = map[string]bool{}
	check("first the provision pass", s, StepProvision, func(st Step) bool { return st.KitName == KitHetty })

	s = hettySnap()
	check("eye of newt through the provisioner", s, StepProvision, func(st Step) bool { return st.KitName == KitHettyNewt })
	s.Failed["provision:"+KitHettyNewt] = true
	check("no eye of newt anywhere: wait, never a bare buy", s, StepWait, nil)
	delete(s.Failed, "provision:"+KitHettyNewt)

	s.InvIDs[ObjEyeOfNewt] = 1
	check("onion picked", s, StepPickLoc, func(st Step) bool { return st.ExpectItemID == ObjOnion })

	s.InvIDs[ObjOnion] = 1
	check("raw beef through the provisioner", s, StepProvision, func(st Step) bool { return st.KitName == KitHettyBeef })
	s.Failed["provision:"+KitHettyBeef] = true
	check("no beef anywhere: wait", s, StepWait, nil)
	delete(s.Failed, "provision:"+KitHettyBeef)

	s.InvIDs[ObjRawBeef] = 1
	check("beef on the range", s, StepUseOn, func(st Step) bool { return st.TargetID == LocHettyRange })

	delete(s.InvIDs, ObjRawBeef)
	s.InvIDs[ObjBurntMeat] = 1
	check("a rat for its tail", s, StepCustom, func(st Step) bool { return st.Custom == CustomHettyRat })

	s.BankIDs[ObjRatsTail] = 1
	check("a banked tail is withdrawn, never farmed", s, StepWithdraw, nil)

	delete(s.BankIDs, ObjRatsTail)
	s.InvIDs[ObjRatsTail] = 1
	check("hand in", s, StepCustom, func(st Step) bool { return st.Custom == CustomHettyHandIn })

	s = hettySnap()
	s.Stage = HettyObjectsGiven
	check("after a restart at stage 2: drink, continuing the message", s, StepCustom, func(st Step) bool { return st.Custom == CustomHettyDrink })

	s = hettySnap()
	s.Journal = StatusComplete
	check("complete", s, StepDone, nil)
}

// A8: an EMPTY mule never gets asked; the eye of newt falls to Betty.
func TestWitchsPotionKitWithAnEmptyMule(t *testing.T) {
	p := PlanProvision(hettyKit(), ProvisionState{BankKnown: true, Pack: map[int]int{CoinsID: 50}, MuleKnown: true, FreeSlots: 20})
	if len(p.MuleAsk) != 0 || p.Phase != PhaseSource || p.Tasks[0].Source.Shop.Npc != "Betty" {
		t.Fatalf("plan %s", p.Summary())
	}
}

func TestParseWitchsPotionJournal(t *testing.T) {
	cases := map[string]int{
		"@str@I spoke to Hetty in her house at Rimmington.|@dbl@Hetty needs me to bring her the following:|": HettyStarted,
		"@str@I brought her an onion|@dbl@I should drink from the @dre@cauldron@dbl@ and improve my magic!":  HettyObjectsGiven,
	}
	for text, want := range cases {
		if got, ok := ParseWitchsPotionJournal([]string{text}); !ok || got != want {
			t.Errorf("%q: %d %v", text, got, ok)
		}
	}
}

func TestTheRatHouseHoldsEveryRatSpawnAndNotTheDoorstep(t *testing.T) {
	for _, sp := range []tile.Tile{{X: 2953, Z: 3204}, {X: 2955, Z: 3202}, {X: 2959, Z: 3202}, {X: 2960, Z: 3205}, RimmingtonRats} {
		if !InRatHouse(sp) {
			t.Errorf("%v is a rat spawn outside the house", sp)
		}
	}
	if InRatHouse(tile.Tile{X: 2956, Z: 3206}) {
		t.Error("the doorstep outside the north door reads as inside")
	}
}
