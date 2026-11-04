package questplan

import (
	"slices"
	"testing"
	"time"
)

// Doric's Quest and Gertrude's Cat are quest-point fillers of one point each: the owner's ladder runs them after the larger
// fillers and before the finale, they gate on nothing (the bank, the mule and the world are the module's) and their
// rows say what the modules do.
func TestDoricAndGertrudesCatLadderRows(t *testing.T) {
	for _, c := range []struct {
		id     string
		module Module
		xp     map[string]int
		slot   time.Duration
	}{
		{DoricsQuestID, DoricsQuest, map[string]int{"mining": 1300}, 37*time.Minute + 30*time.Second},
		{GertrudesCatID, GertrudesCat, map[string]int{"cooking": 1525}, 56*time.Minute + 15*time.Second},
	} {
		f, ok := LadderFacts[c.id]
		if !ok {
			t.Fatalf("%s has no ladder row", c.id)
		}
		if f.ID != c.id || f.Name != c.module.Record.Name || f.QP != 1 || f.QP != c.module.Record.QuestPoints || !f.Filler {
			t.Errorf("%s row %+v does not match its module record %+v", c.id, f, c.module.Record)
		}
		if f.Finale || f.TrainSkill != "" || len(f.Gates) != 0 || len(f.Prereqs) != 0 || len(f.SoftPrereqs) != 0 || f.QPNeeded != 0 || f.MinCombat != 0 {
			t.Errorf("%s row %+v: a filler has no gate, no prerequisite and no combat advice (the iron ore and the coins are the module's)", c.id, f)
		}
		if f.EstMinutes <= 0 || f.Region == "" || f.Source == "" || f.Notes == "" {
			t.Errorf("%s row lacks its estimate, region, source or notes: %+v", c.id, f)
		}
		for skill, xp := range c.xp {
			if f.XP[skill] != xp || len(f.XP) != len(c.xp) {
				t.Errorf("%s pays %v in its row, want %v (content's stat_advance / 10)", c.id, f.XP, c.xp)
			}
		}
		if QuestTabName(c.id) != c.module.Record.Name {
			t.Errorf("QuestTabName(%s) = %q", c.id, QuestTabName(c.id))
		}
		if got := QuestSlot(c.id, 20*time.Minute); got != c.slot {
			t.Errorf("%s slot %s, want %s", c.id, got, c.slot)
		}
		files, ok := ModuleFiles[c.id]
		if !ok || len(files) < 2 {
			t.Errorf("%s has no ModuleFiles entry: %v", c.id, files)
		}
		if holds := LadderHolds(c.module.Record, PlayerState{SkillLevels: map[string]int{}}); len(holds) != 0 {
			t.Errorf("%s is held by %+v for an account with no levels", c.id, holds)
		}
		if RungHeld(c.id) != "" {
			t.Errorf("%s is held: %q", c.id, RungHeld(c.id))
		}
		if evs := CheckRequirements(c.module.Record, PlayerState{}); len(evs) != 0 {
			t.Errorf("%s has requirements %+v", c.id, evs)
		}
	}
	// Gertrude's Cat leans on Cook's Assistant's milking and bucket, so a change to either resets its evidence.
	files := ModuleFiles[GertrudesCatID]
	for _, want := range []string{"internal/scripts/questplan/gertrudescat.go", "internal/scripts/questplan/cooksassistant.go",
		"internal/scripts/questmods/fluffs.go", "internal/scripts/questmods/cook.go"} {
		if !slices.Contains(files, want) {
			t.Errorf("fluffs ModuleFiles lacks %s: %v", want, files)
		}
	}
}

// The planner takes the one-point fillers after the larger ones, in name order, and an account with every one of them done
// is not asked for more than it needs. Here every filler but these two and Sheep Shearer is done.
func TestDoricAndGertrudesCatMakeUpTheLastQuestPoints(t *testing.T) {
	done := map[string]bool{}
	for id, f := range LadderFacts {
		if f.Filler && id != DoricsQuestID && id != GertrudesCatID && id != SheepShearerID {
			done[id] = true
		}
	}
	a := Account{XP: map[string]int{}, Done: done, Owned: map[int]int{ObjNails: 12}, QP: 30}
	p := PlanLadder([]string{"dragon"}, LadderFacts, a)
	var got []string
	for _, r := range p.Rungs {
		got = append(got, r.ID)
	}
	if !slices.Equal(got, []string{"doric", "fluffs", "dragon"}) {
		t.Errorf("30 quest points with the big three done: plan %v, want Doric's Quest, Gertrude's Cat (d, f before Sheep Shearer's s) then Dragon Slayer", got)
	}
	a.Done["doric"] = true
	a.QP = 31
	p = PlanLadder([]string{"dragon"}, LadderFacts, a)
	got = nil
	for _, r := range p.Rungs {
		got = append(got, r.ID)
	}
	if !slices.Equal(got, []string{"fluffs", "dragon"}) {
		t.Errorf("31 quest points: plan %v, want Gertrude's Cat then Dragon Slayer", got)
	}
}
