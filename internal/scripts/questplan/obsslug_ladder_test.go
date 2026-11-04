package questplan

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// The Observatory Quest and the Sea Slug Quest sit in the owner's ladder after the one-point fillers (the Clock Tower), ahead of
// Black Knights' Fortress and the finale, and their ladder rows say what the modules do.
func TestTheObservatoryAndTheSeaSlugSitAfterTheOtherFillers(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	o, s := at(ObservatoryID), at(SeaSlugID)
	// They are two fillers ahead of Black Knights' Fortress; the quests merged in the same wave sit between the Clock Tower and them, and
	// later waves between them and it (ownerFillerTail spells the whole tail).
	if o < 0 || s != o+1 || o <= at(ClockTowerID) || at(BlackKnightsFortressID) <= s || at("dragon") != len(OwnerLadder)-1 {
		t.Fatalf("OwnerLadder %v: want ... cog, ..., itgronigen, seaslug, blackknight, dragon", OwnerLadder)
	}
	for _, x := range []struct {
		id     string
		module Module
		qp     int
		xp     map[string]int
		slot   time.Duration
	}{
		{ObservatoryID, Observatory, 2, map[string]int{"crafting": 2250}, 56*time.Minute + 15*time.Second},
		{SeaSlugID, SeaSlug, 1, map[string]int{"fishing": 7175}, 50 * time.Minute},
	} {
		f, ok := LadderFacts[x.id]
		if !ok {
			t.Fatalf("%s has no ladder row", x.id)
		}
		if f.ID != x.id || f.Name != x.module.Record.Name || f.QP != x.module.Record.QuestPoints || !f.Filler || f.QP != x.qp {
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
		if x.module.ExclusiveDone == nil || x.module.ReadStage == nil || x.module.Decide == nil {
			t.Errorf("%s module lacks its slot release, its journal reader or its decision", x.id)
		}
	}
	if ObservatoryID != "itgronigen" || SeaSlugID != "seaslug" || ObservatoryName != "Observatory Quest" || SeaSlugName != "Sea Slug Quest" {
		t.Errorf("ids %q %q names %q %q", ObservatoryID, SeaSlugID, ObservatoryName, SeaSlugName)
	}
}

// The Observatory asks for no level; the Sea Slug Quest asks for Firemaking 30 in its record (which holds it back in the live ladder) and
// lists it as advised, because a hard gate must name a training rung the ladder runs and no Firemaking rung exists.
func TestTheSeaSlugIsHeldByItsRecordAndNotTrained(t *testing.T) {
	if r := Observatory.Record.Requirements; len(r.Skills) != 0 || len(r.Quests) != 0 || r.MinQuestPoints != 0 {
		t.Errorf("the Observatory's record asks for %+v: the content asks for nothing", r)
	}
	req := SeaSlug.Record.Requirements
	if len(req.Skills) != 1 || req.Skills[0].Skill != "firemaking" || req.Skills[0].Level != 30 {
		t.Fatalf("the Sea Slug record asks for %+v", req)
	}
	f := LadderFacts[SeaSlugID]
	if len(f.Gates) != 1 || f.Gates[0].Kind != GateAdvised || f.Gates[0].Skill != "firemaking" || f.Gates[0].Level != 30 || f.Gates[0].Hard() {
		t.Errorf("the row's gates are %+v", f.Gates)
	}
	// No rung trains Firemaking, so the planner inserts none and the record is the only hold.
	for id, r := range LadderFacts {
		if strings.EqualFold(r.TrainSkill, "firemaking") {
			t.Errorf("%s trains Firemaking: the Sea Slug Quest's hold on its record would be a lie of omission", id)
		}
	}
	if holds := LadderHolds(SeaSlug.Record, PlayerState{SkillLevels: map[string]int{}}); len(holds) != 0 {
		t.Errorf("the ladder's own holds are %+v: the record's requirement is the one", holds)
	}
	below := CheckRequirements(SeaSlug.Record, PlayerState{SkillLevels: map[string]int{"firemaking": 29}})
	if len(below) != 1 || below[0].OK || !strings.Contains(below[0].Reason, "Firemaking 30") {
		t.Errorf("at Firemaking 29 the record says %+v", below)
	}
}

// Both are planned in the owner's order and not inserted; the plan for an account at Firemaking 1 does not grow a training stop for the
// Sea Slug Quest, only the advice warning.
func TestTheObservatoryAndTheSeaSlugInThePlanner(t *testing.T) {
	a := stage5Graduate()
	p := PlanLadder(OwnerLadder, LadderFacts, a)
	last := -1
	for _, id := range []string{ObservatoryID, SeaSlugID, BlackKnightsFortressID, "dragon"} {
		i := indexOf(p, id)
		if i <= last || (id != "dragon" && p.Rungs[i].Inserted) {
			t.Errorf("%s sits at %d after %d (inserted %v)", id, i, last, i >= 0 && p.Rungs[i].Inserted)
		}
		last = i
	}
	for _, r := range p.Rungs {
		if strings.HasPrefix(r.ID, "train:firemaking") {
			t.Errorf("the plan inserted %s", r.ID)
		}
	}
	// A planner short of one point, with the bigger fillers done, still reaches for the Clock Tower first: the one-point quests go by id.
	short := stage5Graduate()
	short.QP = 31
	short.Owned[ObjNails] = 12
	short.Done = map[string]bool{}
	for id, f := range LadderFacts {
		if f.Filler && f.QP > 1 {
			short.Done[id] = true
		}
	}
	q := PlanLadder([]string{"dragon"}, LadderFacts, short)
	if got := planIDs(q); len(got) != 2 || got[0] != ClockTowerID {
		t.Fatalf("one point short: plan %v", got)
	}
	// Two points short, with the Observatory worth exactly two and the rest done: it is the filler.
	two := stage5Graduate()
	two.QP = 30
	two.Owned[ObjNails] = 12
	two.Done = map[string]bool{}
	for id, f := range LadderFacts {
		if f.Filler && id != ObservatoryID {
			two.Done[id] = true
		}
	}
	q = PlanLadder([]string{"dragon"}, LadderFacts, two)
	if got := planIDs(q); len(got) != 2 || got[0] != ObservatoryID {
		t.Fatalf("two points short with only the Observatory left: plan %v", got)
	}
}
