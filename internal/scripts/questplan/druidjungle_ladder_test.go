package questplan

import (
	"os"
	"slices"
	"testing"
	"time"
)

// Druidic Ritual and Jungle Potion sit in the owner's ladder after the one-point fillers, Jungle Potion at once after the quest it
// wants, both ahead of Black Knights' Fortress and the finale, and their ladder rows say what the modules do.
func TestDruidAndJunglePotionSitAfterTheOnePointFillers(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	d, j := at(DruidicRitualID), at(JunglePotionID)
	if d < 0 || j != d+1 || d <= at(ClockTowerID) || j >= at(BlackKnightsFortressID) || at("dragon") != len(OwnerLadder)-1 {
		t.Fatalf("OwnerLadder %v: want ... cog, druid, junglepotion, blackknight, dragon", OwnerLadder)
	}
	for _, x := range []struct {
		id     string
		module Module
		qp     int
		xp     map[string]int
		prereq []string
	}{
		{DruidicRitualID, DruidicRitual, 4, map[string]int{"herblore": 250}, nil},
		{JunglePotionID, JunglePotion, 1, map[string]int{"herblore": 775}, []string{DruidicRitualID}},
	} {
		f, ok := LadderFacts[x.id]
		if !ok {
			t.Fatalf("%s has no ladder row", x.id)
		}
		if f.ID != x.id || f.Name != x.module.Record.Name || f.QP != x.qp || f.QP != x.module.Record.QuestPoints || !f.Filler {
			t.Errorf("%s row %+v does not match its module record %+v", x.id, f, x.module.Record)
		}
		if f.Finale || f.TrainSkill != "" || f.QPNeeded != 0 || !slices.Equal(f.Prereqs, x.prereq) {
			t.Errorf("%s row %+v: no training, no finale, no quest-point minimum, prerequisites %v", x.id, f, x.prereq)
		}
		// A hard gate would need a named training rung (TestEveryHardGateHasATrainingRung): the levels are the record's.
		for _, g := range f.Gates {
			if g.Hard() {
				t.Errorf("%s has the hard gate %+v: no rung trains it", x.id, g)
			}
		}
		if f.EstMinutes <= 0 || f.Region == "" || f.Source == "" || f.Notes == "" || f.MinCombat <= 0 {
			t.Errorf("%s row lacks its estimate, region, source, notes or combat advice: %+v", x.id, f)
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
		// The slot is the estimate and a quarter (QuestSlot): long enough for the walks.
		if got, floor := QuestSlot(x.id, 20*time.Minute), time.Duration(f.EstMinutes)*time.Minute; got < floor {
			t.Errorf("%s slot %s is under its estimate %s", x.id, got, floor)
		}
		files := ModuleFiles[x.id]
		if len(files) != 3 || !slices.Contains(files, "internal/scripts/questplan/shopontheway.go") {
			t.Errorf("%s ModuleFiles %v: want its plan, the shared shop rule and its customs", x.id, files)
		}
		for _, file := range files {
			if _, err := os.Stat("../../../" + file); err != nil {
				t.Errorf("%s ModuleFiles names %s: %v", x.id, file, err)
			}
		}
		// Neither is held back by a hard gate the ladder knows (the safety floors are record requirements, not holds), nor by the
		// ladder's own hold list.
		if holds := LadderHolds(x.module.Record, PlayerState{SkillLevels: map[string]int{}, CompletedQuests: map[string]bool{DruidicRitualID: true}}); len(holds) != 0 {
			t.Errorf("%s is held by %+v for an account with no levels", x.id, holds)
		}
		if why := RungHeld(x.id); why != "" {
			t.Errorf("%s is held back by the ladder: %s", x.id, why)
		}
	}
	if DruidicRitualName != "Druidic Ritual" || JunglePotionName != "Jungle Potion" || DruidicRitualID != "druid" || JunglePotionID != "junglepotion" {
		t.Errorf("names %q %q ids %q %q", DruidicRitualName, JunglePotionName, DruidicRitualID, JunglePotionID)
	}
}

// The record asks the ladder (RunLadder, through CheckRequirements) for Hitpoints 30 and, for Jungle Potion, Hitpoints 35, Herblore 3
// and Druidic Ritual: an account short of any is skipped with the reason, not run.
func TestDruidAndJunglePotionRecordsAskTheLadderForTheirFloors(t *testing.T) {
	fail := func(r Record, p PlayerState) []string {
		var out []string
		for _, x := range CheckRequirements(r, p) {
			if !x.OK {
				out = append(out, x.Reason)
			}
		}
		return out
	}
	ready := PlayerState{SkillLevels: map[string]int{"hitpoints": 40, "herblore": 3}, CompletedQuests: map[string]bool{"druid": true}}
	if got := fail(DruidicRitualRecord, PlayerState{SkillLevels: map[string]int{"hitpoints": 30}}); len(got) != 0 {
		t.Errorf("Druidic Ritual at Hitpoints 30: %v", got)
	}
	if got := fail(DruidicRitualRecord, PlayerState{SkillLevels: map[string]int{"hitpoints": 29}}); len(got) != 1 {
		t.Errorf("Druidic Ritual at Hitpoints 29: %v", got)
	}
	if got := fail(JunglePotionRecord, ready); len(got) != 0 {
		t.Errorf("Jungle Potion for a ready account: %v", got)
	}
	for name, p := range map[string]PlayerState{
		"no Druidic Ritual": {SkillLevels: ready.SkillLevels, CompletedQuests: map[string]bool{}},
		"Herblore 2":        {SkillLevels: map[string]int{"hitpoints": 40, "herblore": 2}, CompletedQuests: ready.CompletedQuests},
		"Hitpoints 34":      {SkillLevels: map[string]int{"hitpoints": 34, "herblore": 3}, CompletedQuests: ready.CompletedQuests},
	} {
		if got := fail(JunglePotionRecord, p); len(got) != 1 {
			t.Errorf("Jungle Potion with %s: %v", name, got)
		}
	}
}

// The planner keeps Jungle Potion behind Druidic Ritual whatever order it is given, takes neither ahead of a deferred quest and
// never inserts Jungle Potion without it; and the Herblore the ritual pays is the level the herbs are identified at.
func TestDruidAndJunglePotionInThePlanner(t *testing.T) {
	p := PlanLadder(OwnerLadder, LadderFacts, stage5Graduate())
	for _, id := range []string{DruidicRitualID, JunglePotionID} {
		if r := rungOf(p, id); indexOf(p, id) < 0 || r.Inserted {
			t.Errorf("%s: at %d, inserted %v", id, indexOf(p, id), r.Inserted)
		}
	}
	if d, j := indexOf(p, DruidicRitualID), indexOf(p, JunglePotionID); d < 0 || j != d+1 {
		t.Errorf("Druidic Ritual at %d and Jungle Potion at %d", d, j)
	}
	// Alone in a plan, Jungle Potion pulls its prerequisite in front of it.
	q := PlanLadder([]string{JunglePotionID}, LadderFacts, stage5Graduate())
	if got := planIDs(q); !slices.Equal(got, []string{DruidicRitualID, JunglePotionID}) {
		t.Errorf("Jungle Potion alone plans %v", got)
	}
	if r := rungOf(q, DruidicRitualID); !r.Inserted {
		t.Error("Druidic Ritual is not marked inserted when pulled in")
	}
	// The ritual's experience is Herblore 3, so no account that has done the ritual is short of the herbs' level.
	if lv := q.Rungs[0].Levels["herblore"]; lv != 3 {
		t.Errorf("Herblore %d after Druidic Ritual", lv)
	}
}
