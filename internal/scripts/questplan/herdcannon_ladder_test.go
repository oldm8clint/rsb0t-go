package questplan

import (
	"os"
	"slices"
	"testing"
	"time"
)

// Sheep Herder and Dwarf Cannon (W13, 19 Oct 2026) sit in the owner's ladder after the Sea Slug Quest, ahead of Black Knights' Fortress (whose
// twelve quest points they help to make) and the finale, and their ladder rows say what the modules do.
func TestSheepHerderAndDwarfCannonSitAfterTheOtherFillers(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	h, c := at(SheepHerderID), at(DwarfCannonID)
	if h < 0 || c != h+1 || h <= at(SeaSlugID) || at(BlackKnightsFortressID) <= c || at("dragon") != len(OwnerLadder)-1 {
		t.Fatalf("OwnerLadder %v: want ... seaslug, sheepherder, mcannon, blackknight, dragon", OwnerLadder)
	}
	for _, x := range []struct {
		id     string
		module Module
		qp     int
		xp     map[string]int
	}{
		{SheepHerderID, SheepHerder, 4, nil},
		{DwarfCannonID, DwarfCannon, 1, map[string]int{"crafting": 750}},
	} {
		f, ok := LadderFacts[x.id]
		if !ok {
			t.Fatalf("%s has no ladder row", x.id)
		}
		if f.ID != x.id || f.Name != x.module.Record.Name || f.QP != x.module.Record.QuestPoints || !f.Filler || f.QP != x.qp {
			t.Errorf("%s row %+v does not match its module record %+v", x.id, f, x.module.Record)
		}
		if f.Finale || f.TrainSkill != "" || len(f.Prereqs) != 0 || f.QPNeeded != 0 || f.MinCombat != 0 {
			t.Errorf("%s row %+v: a filler has no prerequisite and no combat advice (its safety floor is the module's)", x.id, f)
		}
		if f.EstMinutes <= 0 || f.Region == "" || f.Source == "" || f.Notes == "" {
			t.Errorf("%s row lacks its estimate, region, source or notes: %+v", x.id, f)
		}
		if len(f.XP) != len(x.xp) {
			t.Errorf("%s pays %v, want %v", x.id, f.XP, x.xp)
		}
		for skill, xp := range x.xp {
			if f.XP[skill] != xp {
				t.Errorf("%s pays %d %s XP in its row, want %d", x.id, f.XP[skill], skill, xp)
			}
		}
		if QuestTabName(x.id) != x.module.Record.Name {
			t.Errorf("QuestTabName(%s) = %q", x.id, QuestTabName(x.id))
		}
		// The slot is the estimate and a quarter (QuestSlot), and no longer than the slot a holder of a fleet-wide slot keeps (60 minutes).
		if got := QuestSlot(x.id, 20*time.Minute); got >= time.Hour || got < time.Duration(f.EstMinutes)*time.Minute {
			t.Errorf("%s slot %s for an estimate of %d minutes", x.id, got, f.EstMinutes)
		}
		if len(ModuleFiles[x.id]) < 2 {
			t.Errorf("%s ModuleFiles %v: want its plan and its customs", x.id, ModuleFiles[x.id])
		}
		for _, file := range ModuleFiles[x.id] {
			if _, err := os.Stat("../../../" + file); err != nil {
				t.Errorf("%s ModuleFiles names %s: %v", x.id, file, err)
			}
		}
		if why := RungHeld(x.id); why != "" {
			t.Errorf("%s is held back by the ladder: %s", x.id, why)
		}
		if x.module.ExclusiveDone == nil || x.module.ReadStage == nil || x.module.Decide == nil {
			t.Errorf("%s module lacks its slot release, its journal reader or its decision", x.id)
		}
	}
	if SheepHerderID != "sheepherder" || DwarfCannonID != "mcannon" || SheepHerderName != "Sheep Herder" || DwarfCannonName != "Dwarf Cannon" {
		t.Errorf("ids %q %q names %q %q", SheepHerderID, DwarfCannonID, SheepHerderName, DwarfCannonName)
	}
}

// Both are planned in the owner's order and not inserted, with no training stop in front of either: nothing in their rows is a hard gate.
func TestSheepHerderAndDwarfCannonInThePlanner(t *testing.T) {
	a := stage5Graduate()
	p := PlanLadder(OwnerLadder, LadderFacts, a)
	last := -1
	for _, id := range []string{SheepHerderID, DwarfCannonID, BlackKnightsFortressID, "dragon"} {
		i := indexOf(p, id)
		if i <= last || (id != "dragon" && p.Rungs[i].Inserted) {
			t.Errorf("%s sits at %d after %d (inserted %v)", id, i, last, i >= 0 && p.Rungs[i].Inserted)
		}
		last = i
	}
	for _, id := range []string{SheepHerderID, DwarfCannonID} {
		for _, g := range LadderFacts[id].Gates {
			if g.Hard() {
				t.Errorf("%s has a hard gate %+v: the ladder would have to train for it", id, g)
			}
		}
	}
	// Four points short with the rest done, Sheep Herder is the filler: the largest quest point reward that is left.
	four := stage5Graduate()
	four.QP = 28
	four.Owned[ObjNails] = 12
	four.Done = map[string]bool{}
	for id, f := range LadderFacts {
		if f.Filler && id != SheepHerderID {
			four.Done[id] = true
		}
	}
	q := PlanLadder([]string{"dragon"}, LadderFacts, four)
	if got := planIDs(q); len(got) != 2 || got[0] != SheepHerderID {
		t.Fatalf("four points short with only Sheep Herder left: plan %v", got)
	}
	// One point short with every other filler done, Dwarf Cannon is the filler.
	one := stage5Graduate()
	one.QP = 31
	one.Owned[ObjNails] = 12
	one.Done = map[string]bool{}
	for id, f := range LadderFacts {
		if f.Filler && id != DwarfCannonID {
			one.Done[id] = true
		}
	}
	q = PlanLadder([]string{"dragon"}, LadderFacts, one)
	if got := planIDs(q); len(got) != 2 || got[0] != DwarfCannonID {
		t.Fatalf("one point short with only Dwarf Cannon left: plan %v", got)
	}
}
