package questplan

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// Hazeel Cult and the Elemental Workshop sit in the owner's ladder after the Sea Slug Quest, ahead of Black Knights' Fortress and the
// finale, and their ladder rows say what the modules do.
func TestHazeelCultAndTheElementalWorkshopSitAfterTheSeaSlug(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	h, e := at(HazeelCultID), at(ElementalWorkshopID)
	// They are two fillers ahead of Black Knights' Fortress, after the Sea Slug Quest and the quests merged between them, and the Tourist
	// Trap follows them (ownerFillerTail spells the whole tail).
	if h < 0 || e != h+1 || h <= at(SeaSlugID) || at(TouristTrapID) != e+1 || at(BlackKnightsFortressID) != e+2 || at("dragon") != len(OwnerLadder)-1 {
		t.Fatalf("OwnerLadder %v: want ... seaslug, ..., hazeelcult, elemental_workshop, desertrescue, blackknight, dragon", OwnerLadder)
	}
	for _, x := range []struct {
		id     string
		module Module
		xp     map[string]int
		slot   time.Duration
	}{
		{HazeelCultID, HazeelCult, map[string]int{"thieving": 1500}, 50 * time.Minute},
		{ElementalWorkshopID, ElementalWorkshop, map[string]int{"crafting": 5000, "smithing": 5028}, 56*time.Minute + 15*time.Second},
	} {
		f, ok := LadderFacts[x.id]
		if !ok {
			t.Fatalf("%s has no ladder row", x.id)
		}
		if f.ID != x.id || f.Name != x.module.Record.Name || f.QP != x.module.Record.QuestPoints || !f.Filler || f.QP != 1 {
			t.Errorf("%s row %+v does not match its module record %+v", x.id, f, x.module.Record)
		}
		if f.Finale || f.TrainSkill != "" || len(f.Prereqs) != 0 || f.QPNeeded != 0 || f.MinCombat != 0 {
			t.Errorf("%s row %+v: a filler has no prerequisite and no combat advice (its safety floor is the plan's)", x.id, f)
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
		if got := QuestSlot(x.id, 20*time.Minute); got != x.slot || got >= time.Hour {
			t.Errorf("%s slot %s, want %s", x.id, got, x.slot)
		}
		if len(ModuleFiles[x.id]) != 2 {
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
		if x.module.ReadStage == nil || x.module.Decide == nil {
			t.Errorf("%s module lacks its journal reader or its decision", x.id)
		}
		// Neither is an exclusive-slot module: nothing in either is shared (questrunner's slot tests pin the table).
		if x.module.ExclusiveDone != nil {
			t.Errorf("%s has a slot release but no slot", x.id)
		}
	}
	if HazeelCultID != "hazeelcult" || ElementalWorkshopID != "elemental_workshop" || HazeelCultName != "Hazeel Cult" || ElementalWorkshopName != "Elemental Workshop" {
		t.Errorf("ids %q %q names %q %q", HazeelCultID, ElementalWorkshopID, HazeelCultName, ElementalWorkshopName)
	}
}

// Hazeel Cult asks for nothing; the Elemental Workshop asks for Mining, Smithing and Crafting 20 in its record and advises them in its row
// (a hard gate would insert a training rung: none trains Mining, the Smithing rung is for nails and the Crafting rungs for flax, and none
// is run for one filler).
func TestTheElementalWorkshopIsHeldByItsRecordAndNotTrained(t *testing.T) {
	if r := HazeelCult.Record.Requirements; len(r.Skills) != 0 || len(r.Quests) != 0 || r.MinQuestPoints != 0 {
		t.Errorf("Hazeel Cult's record asks for %+v: the content asks for nothing", r)
	}
	f := LadderFacts[ElementalWorkshopID]
	if len(f.Gates) != 3 {
		t.Fatalf("the row's gates are %+v", f.Gates)
	}
	for _, g := range f.Gates {
		if g.Kind != GateAdvised || g.Level != 20 || g.Hard() {
			t.Errorf("gate %+v: advised at 20 only", g)
		}
	}
	// No rung trains Mining.
	for id, r := range LadderFacts {
		if strings.EqualFold(r.TrainSkill, "mining") {
			t.Errorf("%s trains Mining: the Elemental Workshop's hold on its record would be a lie of omission", id)
		}
	}
	// And the planner inserts no training for an account with none of the three: the record is the hold, not a stop.
	none := stage5Graduate()
	if got := planIDs(PlanLadder([]string{ElementalWorkshopID}, LadderFacts, none)); !slices.Equal(got, []string{ElementalWorkshopID}) {
		t.Errorf("an account with no Mining, Smithing or Crafting is planned %v", got)
	}
	if holds := LadderHolds(ElementalWorkshop.Record, PlayerState{SkillLevels: map[string]int{}}); len(holds) != 0 {
		t.Errorf("the ladder's own holds are %+v: the record's requirement is the one", holds)
	}
	below := CheckRequirements(ElementalWorkshop.Record, PlayerState{SkillLevels: map[string]int{"mining": 19, "smithing": 20, "crafting": 25}})
	failed := 0
	for _, r := range below {
		if !r.OK {
			failed++
			if !strings.Contains(r.Reason, "Mining 20 (have 19)") {
				t.Errorf("the record says %q", r.Reason)
			}
		}
	}
	if failed != 1 {
		t.Errorf("at Mining 19 the record fails %d of its three skills: %+v", failed, below)
	}
	for _, r := range CheckRequirements(ElementalWorkshop.Record, PlayerState{SkillLevels: map[string]int{"mining": 20, "smithing": 20, "crafting": 20}}) {
		if !r.OK {
			t.Errorf("at 20 in all three the record says %q", r.Reason)
		}
	}
}

// Both are planned in the owner's order and not inserted; one a point short makes the Hazeel Cult its filler like any one-point quest.
func TestHazeelCultAndTheElementalWorkshopInThePlanner(t *testing.T) {
	a := stage5Graduate()
	p := PlanLadder(OwnerLadder, LadderFacts, a)
	last := -1
	for _, id := range []string{HazeelCultID, ElementalWorkshopID, BlackKnightsFortressID, "dragon"} {
		i := indexOf(p, id)
		if i <= last || (id != "dragon" && p.Rungs[i].Inserted) {
			t.Errorf("%s sits at %d after %d (inserted %v)", id, i, last, i >= 0 && p.Rungs[i].Inserted)
		}
		last = i
	}
	// One point short with every other filler done: the one-point quests go by id, and these two are among them.
	short := stage5Graduate()
	short.QP = 31
	short.Owned[ObjNails] = 12
	short.Done = map[string]bool{}
	for id, f := range LadderFacts {
		if f.Filler && id != HazeelCultID {
			short.Done[id] = true
		}
	}
	q := PlanLadder([]string{"dragon"}, LadderFacts, short)
	if got := planIDs(q); len(got) != 2 || got[0] != HazeelCultID {
		t.Fatalf("one point short with only Hazeel Cult left: plan %v", got)
	}
}
