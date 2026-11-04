package questplan

import (
	"strings"
	"testing"
)

// HeldRungs holds Dragon Slayer alone (Tree Gnome Village was held until W18 and runs again), and names rungs
// the ladder really has: an id typed wrong would hold nothing and nobody would notice until the module ran.
func TestOnlyDragonSlayerIsHeldAndItIsARealRung(t *testing.T) {
	if len(HeldRungs) != 1 || RungHeld(DragonSlayerID) == "" {
		t.Fatalf("HeldRungs %v, want Dragon Slayer alone", HeldRungs)
	}
	if why := RungHeld(TreeGnomeVillageID); why != "" {
		t.Fatalf("Tree Gnome Village is held again: %q", why)
	}
	for id, why := range HeldRungs {
		if _, ok := LadderFacts[id]; !ok {
			t.Errorf("held rung %q has no LadderFacts row", id)
		}
		if !inOrder(OwnerLadder, id) {
			t.Errorf("held rung %q is not on the owner's ladder", id)
		}
		if strings.TrimSpace(why) == "" || !strings.Contains(why, "W19") {
			t.Errorf("held rung %q has reason %q: it must say which work item lifts it", id, why)
		}
	}
	// The fillers the hold exists for, and every other rung, are not held.
	for id := range LadderFacts {
		if id != DragonSlayerID && RungHeld(id) != "" {
			t.Errorf("%s is held: %q", id, RungHeld(id))
		}
	}
}

// THE COUPLING the hold answers. Dragon Slayer has never run live and its only live gate is 32 quest
// points. The live log of 16 October 2026 showed accounts blocked at 20, 26 and 31; the two fillers
// give +10, which carries the 26 and the 31 over the line. While Dragon Slayer is held the fillers
// cannot do that. W19 lifts the hold (delete the HeldRungs entry) and this test with it.
func TestTheFillersDoNotCarryAnAccountIntoDragonSlayer(t *testing.T) {
	gain := LadderFacts[GoblinDiplomacyID].QP + LadderFacts[RomeoJulietID].QP
	need := LadderFacts[DragonSlayerID].QPNeeded
	if gain != 10 || need != 32 {
		t.Fatalf("the fillers give %d and Dragon Slayer wants %d: the numbers this test is about have moved", gain, need)
	}
	for _, qp := range []int{26, 31} {
		if qp >= need {
			t.Fatalf("%d quest points already reaches Dragon Slayer", qp)
		}
		if qp+gain < need {
			t.Fatalf("%d + the fillers does not reach Dragon Slayer: nothing for the hold to do", qp)
		}
	}
	// Dragon Slayer's own record states the 32; at the quest points the fillers give, the ladder's
	// table has no other hold on it, so the one thing in the way is HeldRungs.
	st := PlayerState{QuestPoints: 31 + gain, SkillLevels: map[string]int{"smithing": 99}}
	if h := LadderHolds(DragonSlayerRecord, st); len(h) != 0 {
		t.Fatalf("holds %+v at %d quest points: the test would not show the hold doing the work", h, st.QuestPoints)
	}
	for _, r := range CheckRequirements(DragonSlayerRecord, st) {
		if !r.OK {
			t.Fatalf("requirement fails at %d quest points: %s", st.QuestPoints, r.Reason)
		}
	}
	if RungHeld(DragonSlayerID) == "" {
		t.Fatalf("Dragon Slayer is not held, so an account at %d quest points would start it", st.QuestPoints)
	}
}
