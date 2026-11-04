package questplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/levels"
)

// stage5Graduate is what an account holds when the quest ladder starts: Stage 3's Magic, the
// Cows rung's 20 Hitpoints, nothing else.
func stage5Graduate() Account {
	return Account{
		XP: map[string]int{
			"hitpoints": levels.XPAtLevel(20),
			"magic":     levels.XPAtLevel(17),
			"thieving":  levels.XPAtLevel(25),
		},
		Done: map[string]bool{}, Owned: map[int]int{},
	}
}

func indexOf(p LadderPlan, id string) int {
	for i, r := range p.Rungs {
		if r.ID == id {
			return i
		}
	}
	return -1
}

func rungOf(p LadderPlan, id string) PlannedRung {
	return p.Rungs[indexOf(p, id)]
}

func TestTheOwnersLadderFromAStage5Graduate(t *testing.T) {
	p := PlanLadder(OwnerLadder, LadderFacts, stage5Graduate())
	t.Logf("\n%s", p.Describe(LadderFacts))

	if indexOf(p, "waterfall") != 0 {
		t.Fatalf("Waterfall is the owner's first rung: %v", p.Rungs[0])
	}
	for _, id := range OwnerLadder {
		if indexOf(p, id) < 0 {
			t.Errorf("%s never scheduled", id)
		}
	}
	// Scorpion Catcher's scorpion B is inside the Barbarian Outpost: the Barcrawl is pulled
	// ahead of it, out of its owner position after Agility 35.
	if indexOf(p, "barcrawl") > indexOf(p, "scorpion") {
		t.Error("the Barcrawl must come before Scorpion Catcher")
	}
	// Nature Spirit's sickle needs Crafting 18: a stop to 18, not the whole Crafting 40 stretch.
	if c := indexOf(p, "crafting18"); c < 0 || c > indexOf(p, "spirit") || indexOf(p, "crafting40") < indexOf(p, "spirit") {
		t.Errorf("Crafting 18 stop at %d, spirit at %d, crafting40 at %d", c, indexOf(p, "spirit"), indexOf(p, "crafting40"))
	}
	// Prayer: the Grail, Restless Ghost and Priest in Peril give 13,531 XP, which is Prayer 30,
	// one short of the Monastery's 31 — so a Prayer stop sits right before Scorpion Catcher.
	pray := indexOf(p, "prayer31")
	if pray < 0 || pray > indexOf(p, "scorpion") {
		t.Errorf("no Prayer 31 stop before Scorpion Catcher (prayer stop at %d, scorpion at %d)", pray, indexOf(p, "scorpion"))
	}
	// Priest in Peril's 50 essence: the account owns none, so Rune Mysteries is pulled in.
	if rm := indexOf(p, "runemysteries"); rm < 0 || rm > indexOf(p, "pip") || !rungOf(p, "runemysteries").Inserted {
		t.Errorf("Rune Mysteries should be inserted before Priest in Peril (at %d)", rm)
	}
	// The Fremennik Trials' lyre: Woodcutting 40 and Fletching 25 stops are inserted.
	for _, id := range []string{"woodcutting40", "fletching25", "crafting40"} {
		if i := indexOf(p, id); i < 0 || i > indexOf(p, "fremennik") {
			t.Errorf("%s must come before the Fremennik Trials (at %d)", id, i)
		}
	}
	// Nails: nobody sells them and the account owns none, so Smithing 34 is trained before the
	// first quest that needs them (Horror from the Deep), and Dragon Slayer then needs no stop.
	sm := indexOf(p, "smithing34")
	if sm < 0 || sm > indexOf(p, "horror") {
		t.Errorf("no Smithing 34 stop before Horror from the Deep (at %d)", sm)
	}
	// The quest-point fillers are the owner ladder's own last stops before Dragon Slayer (W13, 15-17 Oct 2026), so
	// they are scheduled in the owner's order and are not insertions: the ladder reaches 32 quest points by
	// itself, and the planner adds a filler of its own only when it does not (TestQuestPointFillersOnlyWhenShort).
	fillers := ownerFillerTail[2 : len(ownerFillerTail)-1]
	for off, id := range fillers {
		i := indexOf(p, id)
		if i < 0 || p.Rungs[i].Inserted || i != indexOf(p, "dragon")-len(fillers)+off {
			t.Errorf("%s sits at %d (inserted %v), want the owner's order, %d from the end", id, i, i >= 0 && p.Rungs[i].Inserted, len(fillers)-off)
		}
	}
	before := p.Rungs[indexOf(p, "dragon")-1]
	if before.QP < 32 {
		t.Errorf("reached Dragon Slayer with %d quest points", before.QP)
	}
	if indexOf(p, "dragon") != len(p.Rungs)-1 {
		t.Errorf("Dragon Slayer should be the last rung, is %d of %d", indexOf(p, "dragon"), len(p.Rungs))
	}
}

func TestOwnedItemsWaiveTheirLevelStops(t *testing.T) {
	a := stage5Graduate()
	a.Owned[ObjNails] = 20
	a.Owned[ObjRuneEssence] = 50
	a.Owned[ObjSilverSickle] = 1
	p := PlanLadder(OwnerLadder, LadderFacts, a)
	if indexOf(p, "smithing34") >= 0 {
		t.Error("20 owned nails should waive the Smithing stop")
	}
	if indexOf(p, "runemysteries") >= 0 {
		t.Error("50 owned essence should waive Rune Mysteries")
	}
	if indexOf(p, "crafting18") >= 0 {
		t.Error("an owned silver sickle should waive the Crafting 18 stop")
	}
}

func TestBelowTwentyHitpointsWaterfallWaitsForAHitpointsStop(t *testing.T) {
	a := stage5Graduate()
	a.XP["hitpoints"] = levels.XPAtLevel(15)
	p := PlanLadder([]string{"waterfall"}, LadderFacts, a)
	if len(p.Rungs) != 2 || p.Rungs[0].ID != "cows20" || p.Rungs[1].ID != "waterfall" {
		t.Fatalf("plan %s", p.Describe(LadderFacts))
	}
	if !strings.Contains(p.Rungs[0].Reason, "owner") {
		t.Errorf("reason %q", p.Rungs[0].Reason)
	}
}

func TestQuestPointFillersOnlyWhenShort(t *testing.T) {
	a := stage5Graduate()
	a.QP = 25
	a.Owned[ObjNails] = 12 // keep the Smithing stop out of this test
	p := PlanLadder([]string{"dragon"}, LadderFacts, a)
	if indexOf(p, "gobdip") < 0 || indexOf(p, "romeo") < 0 || indexOf(p, "dragon") != 2 {
		t.Fatalf("25 QP needs both fillers first:\n%s", p.Describe(LadderFacts))
	}
	a.QP = 29
	p = PlanLadder([]string{"dragon"}, LadderFacts, a)
	if len(p.Rungs) != 2 || indexOf(p, "dragon") != 1 {
		t.Fatalf("29 QP needs one filler:\n%s", p.Describe(LadderFacts))
	}
	a.QP = 32
	p = PlanLadder([]string{"dragon"}, LadderFacts, a)
	if len(p.Rungs) != 1 {
		t.Fatalf("32 QP needs none:\n%s", p.Describe(LadderFacts))
	}
}

func TestAPracticalShortfallDefersBehindAReadyRungThenRunsWithAWarning(t *testing.T) {
	facts := map[string]RungFacts{
		"hard": {ID: "hard", Name: "Hard", MinCombat: 90},
		"easy": {ID: "easy", Name: "Easy", QP: 1},
	}
	p := PlanLadder([]string{"hard", "easy"}, facts, stage5Graduate())
	if len(p.Rungs) != 2 || p.Rungs[0].ID != "easy" || p.Rungs[1].ID != "hard" {
		t.Fatalf("plan %s", p.Describe(facts))
	}
	if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "below its advice") {
		t.Fatalf("warnings %v", p.Warnings)
	}
}

func TestDoneRungsAreSkippedAndTheirRewardsNotRecounted(t *testing.T) {
	a := stage5Graduate()
	a.Done["waterfall"] = true
	p := PlanLadder([]string{"waterfall", "hetty"}, LadderFacts, a)
	if len(p.Rungs) != 1 || p.Rungs[0].ID != "hetty" {
		t.Fatalf("plan %s", p.Describe(LadderFacts))
	}
}

func TestGrandTreeAgilityCountsTowardAgility35(t *testing.T) {
	a := stage5Graduate()
	p := PlanLadder([]string{"agility25", "grandtree", "agility35"}, LadderFacts, a)
	after := rungOf(p, "grandtree")
	// 25 is 8,740 XP; the Grand Tree's 7,900 lands the account at Agility 31, so the stop to 35
	// that follows is the 31-35 stretch rather than 25-35.
	if got := after.Levels["agility"]; got < 30 {
		t.Fatalf("agility after The Grand Tree is %d", got)
	}
}

func TestCombatFormula(t *testing.T) {
	a := Account{XP: map[string]int{
		"attack": levels.XPAtLevel(30), "strength": levels.XPAtLevel(30),
		"hitpoints": levels.XPAtLevel(20),
	}}
	// 0.25*(1+20+0) + 0.325*60 = 24.75
	if got := a.Combat(); got != 24 {
		t.Fatalf("combat %d", got)
	}
}
