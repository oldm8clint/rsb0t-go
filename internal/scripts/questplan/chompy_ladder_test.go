package questplan

import (
	"os"
	"slices"
	"testing"
	"time"
)

// Big Chompy Bird Hunting sits in the owner's ladder after Dwarf Cannon (and the Watch Tower and Lost City, merged ahead of it at the same place), ahead of Hazeel Cult, Black Knights' Fortress and the finale, and its ladder
// row says what the module does.
func TestChompySitsAfterDwarfCannon(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	i := at(ChompyID)
	if i < 0 || i <= at("mcannon") || i >= at(HazeelCultID) || i >= at(BlackKnightsFortressID) || at("dragon") != len(OwnerLadder)-1 {
		t.Fatalf("OwnerLadder %v: want ... sheepherder, mcannon, itwatchtower, zanaris, chompybird, mortton, hazeelcult, elemental_workshop, desertrescue, blackknight, dragon", OwnerLadder)
	}
	f, ok := LadderFacts[ChompyID]
	if !ok {
		t.Fatal("chompybird has no ladder row")
	}
	m := BigChompy
	if f.ID != ChompyID || f.Name != m.Record.Name || f.QP != 2 || f.QP != m.Record.QuestPoints || !f.Filler {
		t.Errorf("row %+v does not match its module record %+v", f, m.Record)
	}
	if f.Finale || f.TrainSkill != "" || f.QPNeeded != 0 || len(f.Prereqs) != 0 {
		t.Errorf("row %+v: no training, no finale, no quest-point minimum, no prerequisite", f)
	}
	for _, g := range f.Gates {
		if g.Hard() {
			t.Errorf("the hard gate %+v: no rung trains it", g)
		}
	}
	// The row lists the record's levels, each as advised.
	listed := map[string]int{}
	for _, g := range f.Gates {
		listed[g.Skill] = g.Level
	}
	for _, r := range m.Record.Requirements.Skills {
		if listed[r.Skill] != r.Level {
			t.Errorf("the record asks %s %d and the row lists %d", r.Skill, r.Level, listed[r.Skill])
		}
	}
	if f.EstMinutes <= 0 || f.Region == "" || f.Source == "" || f.Notes == "" || f.MinCombat <= 0 {
		t.Errorf("the row lacks its estimate, region, source, notes or combat advice: %+v", f)
	}
	if len(f.XP) != 3 || f.XP["fletching"] != 262 || f.XP["cooking"] != 1470 || f.XP["ranged"] != 735 {
		t.Errorf("the row pays %v: the completion queue pays 2,620, 14,700 and 7,350 tenths", f.XP)
	}
	if QuestTabName(ChompyID) != "Big Chompy Bird Hunting" {
		t.Errorf("QuestTabName = %q", QuestTabName(ChompyID))
	}
	// The slot is the estimate and a quarter (QuestSlot): the walks, the wolves and the roast are most of it.
	if got, floor := QuestSlot(ChompyID, 20*time.Minute), time.Duration(f.EstMinutes)*time.Minute; got < floor*5/4 {
		t.Errorf("slot %s is under its estimate and a quarter", got)
	}
	files := ModuleFiles[ChompyID]
	for _, want := range []string{"internal/scripts/questplan/bigchompy.go", "internal/scripts/questmods/chompy.go"} {
		if !slices.Contains(files, want) {
			t.Errorf("ModuleFiles %v lacks %s", files, want)
		}
	}
	for _, file := range files {
		if _, err := os.Stat("../../../" + file); err != nil {
			t.Errorf("ModuleFiles names %s: %v", file, err)
		}
	}
	// The ladder holds nothing back for it: the safety floors are record requirements, and the rung is not on the hold list.
	if holds := LadderHolds(m.Record, PlayerState{SkillLevels: map[string]int{"ranged": 30, "cooking": 30, "fletching": 5, "crafting": 5}, CompletedQuests: map[string]bool{}}); len(holds) != 0 {
		t.Errorf("held by %+v for an account with the levels", holds)
	}
	if why := RungHeld(ChompyID); why != "" {
		t.Errorf("held back by the ladder: %s", why)
	}
	if ChompyName != "Big Chompy Bird Hunting" || ChompyID != "chompybird" {
		t.Errorf("name %q id %q", ChompyName, ChompyID)
	}
}

// The record asks the ladder for what the module needs: an account short of any of the four levels is skipped with the reason, not run.
func TestChompyRecordAsksTheLadderForItsLevels(t *testing.T) {
	fail := func(p PlayerState) []string {
		var out []string
		for _, x := range CheckRequirements(ChompyRecord, p) {
			if !x.OK {
				out = append(out, x.Reason)
			}
		}
		return out
	}
	ready := PlayerState{SkillLevels: map[string]int{"ranged": 30, "cooking": 30, "fletching": 5, "crafting": 5}, CompletedQuests: map[string]bool{}}
	if got := fail(ready); len(got) != 0 {
		t.Errorf("a ready account: %v", got)
	}
	for name, mod := range map[string]func(p *PlayerState){
		"Ranged 29":    func(p *PlayerState) { p.SkillLevels["ranged"] = 29 },
		"Cooking 29":   func(p *PlayerState) { p.SkillLevels["cooking"] = 29 },
		"Fletching 4":  func(p *PlayerState) { p.SkillLevels["fletching"] = 4 },
		"Crafting 4":   func(p *PlayerState) { p.SkillLevels["crafting"] = 4 },
		"two levels":   func(p *PlayerState) { p.SkillLevels["ranged"], p.SkillLevels["cooking"] = 10, 10 },
		"an empty map": func(p *PlayerState) { p.SkillLevels = map[string]int{} },
	} {
		p := PlayerState{SkillLevels: map[string]int{}, CompletedQuests: map[string]bool{}}
		for k, v := range ready.SkillLevels {
			p.SkillLevels[k] = v
		}
		mod(&p)
		got := fail(p)
		want := 1
		if name == "two levels" {
			want = 2
		}
		if name == "an empty map" {
			want = 4
		}
		if len(got) != want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// The planner keeps the quest ahead of the finale and counts its two quest points towards the finale's 32.
func TestChompyInThePlanner(t *testing.T) {
	p := PlanLadder(OwnerLadder, LadderFacts, stage5Graduate())
	i := indexOf(p, ChompyID)
	if i < 0 || i >= indexOf(p, "dragon") {
		t.Errorf("Big Chompy Bird Hunting at %d, the finale at %d", i, indexOf(p, "dragon"))
	}
	if r := rungOf(p, ChompyID); r.Inserted {
		t.Errorf("the owner's own rung is marked inserted: %+v", r)
	}
	// Alone in a plan, it needs nothing in front of it.
	if got := planIDs(PlanLadder([]string{ChompyID}, LadderFacts, stage5Graduate())); !slices.Equal(got, []string{ChompyID}) {
		t.Errorf("Big Chompy Bird Hunting alone plans %v", got)
	}
}
