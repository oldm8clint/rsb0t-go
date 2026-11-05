package questplan

import (
	"os"
	"slices"
	"testing"
	"time"
)

// Shades of Mort'ton sits in the owner's ladder after Big Chompy Bird Hunting, ahead of Hazeel Cult, Black Knights' Fortress and the finale, behind Nature Spirit (the
// swamp's gate) and the Digsite Quest (the fleet's Herblore), and its ladder row says what the module does.
func TestMorttonSitsAfterChompy(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	i := at(MorttonID)
	if i < 0 || i != at(ChompyID)+1 || i >= at(HazeelCultID) || i >= at(BlackKnightsFortressID) || i <= at("itexam") || i <= at("spirit") || at("dragon") != len(OwnerLadder)-1 {
		t.Fatalf("OwnerLadder %v: want ... itexam, ..., mcannon, chompybird, mortton, hazeelcult, elemental_workshop, blackknight, dragon", OwnerLadder)
	}
	f, ok := LadderFacts[MorttonID]
	if !ok {
		t.Fatal("mortton has no ladder row")
	}
	m := MorttonShades
	if f.ID != MorttonID || f.Name != m.Record.Name || f.QP != 3 || f.QP != m.Record.QuestPoints || !f.Filler {
		t.Errorf("row %+v does not match its module record %+v", f, m.Record)
	}
	if f.Finale || f.TrainSkill != "" || f.QPNeeded != 0 || !slices.Equal(f.Prereqs, []string{NatureSpiritID}) {
		t.Errorf("row %+v: no training, no finale, no quest-point minimum, Nature Spirit as the one prerequisite", f)
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
	if len(f.XP) != 2 || f.XP["crafting"] != 2000 || f.XP["herblore"] != 2000 {
		t.Errorf("the row pays %v: the quest pays 2,000 Crafting and 2,000 Herblore experience", f.XP)
	}
	if QuestTabName(MorttonID) != "Shades of Mortton" || MorttonName != "Shades of Mortton" || MorttonID != "mortton" {
		t.Errorf("QuestTabName = %q name %q id %q", QuestTabName(MorttonID), MorttonName, MorttonID)
	}
	// The slot is the estimate and a quarter (QuestSlot): the crossing, the hunt and the building are most of it.
	if got, floor := QuestSlot(MorttonID, 20*time.Minute), time.Duration(f.EstMinutes)*time.Minute; got < floor*5/4 {
		t.Errorf("slot %s is under its estimate and a quarter", got)
	}
	files := ModuleFiles[MorttonID]
	for _, want := range []string{"internal/scripts/questplan/shadesofmortton.go", "internal/scripts/questmods/mortton.go", "internal/scripts/questmods/fieldhunt.go"} {
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
	ready := PlayerState{SkillLevels: map[string]int{"herblore": 15, "crafting": 20}, CompletedQuests: map[string]bool{NatureSpiritID: true}}
	if holds := LadderHolds(m.Record, ready); len(holds) != 0 {
		t.Errorf("held by %+v for an account with the levels", holds)
	}
	if why := RungHeld(MorttonID); why != "" {
		t.Errorf("held back by the ladder: %s", why)
	}
}

// The record asks the ladder for what the module needs: an account short of a level or of Nature Spirit is skipped with the reason, not run.
func TestMorttonRecordAsksTheLadderForItsLevels(t *testing.T) {
	fail := func(p PlayerState) []string {
		var out []string
		for _, x := range CheckRequirements(MorttonRecord, p) {
			if !x.OK {
				out = append(out, x.Reason)
			}
		}
		return out
	}
	ready := PlayerState{SkillLevels: map[string]int{"herblore": 15, "crafting": 20}, CompletedQuests: map[string]bool{NatureSpiritID: true}}
	if got := fail(ready); len(got) != 0 {
		t.Errorf("a ready account: %v", got)
	}
	for name, c := range map[string]struct {
		mod  func(p *PlayerState)
		want int
	}{
		"Herblore 14":      {func(p *PlayerState) { p.SkillLevels["herblore"] = 14 }, 1},
		"Crafting 19":      {func(p *PlayerState) { p.SkillLevels["crafting"] = 19 }, 1},
		"no Nature Spirit": {func(p *PlayerState) { p.CompletedQuests = map[string]bool{} }, 1},
		"two levels":       {func(p *PlayerState) { p.SkillLevels["herblore"], p.SkillLevels["crafting"] = 5, 5 }, 2},
		"an empty map":     {func(p *PlayerState) { p.SkillLevels = map[string]int{} }, 2},
	} {
		p := PlayerState{SkillLevels: map[string]int{}, CompletedQuests: map[string]bool{NatureSpiritID: true}}
		for k, v := range ready.SkillLevels {
			p.SkillLevels[k] = v
		}
		c.mod(&p)
		if got := fail(p); len(got) != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// The planner keeps the quest ahead of the finale, behind Nature Spirit, and counts its three quest points towards the finale's 32.
func TestMorttonInThePlanner(t *testing.T) {
	p := PlanLadder(OwnerLadder, LadderFacts, stage5Graduate())
	i := indexOf(p, MorttonID)
	if i < 0 || i >= indexOf(p, "dragon") || i < indexOf(p, "spirit") {
		t.Errorf("Shades of Mort'ton at %d, Nature Spirit at %d, the finale at %d", i, indexOf(p, "spirit"), indexOf(p, "dragon"))
	}
	if r := rungOf(p, MorttonID); r.Inserted {
		t.Errorf("the owner's own rung is marked inserted: %+v", r)
	}
	// Alone in a plan, it brings its prerequisite in front of it.
	got := planIDs(PlanLadder([]string{MorttonID}, LadderFacts, stage5Graduate()))
	if len(got) == 0 || got[len(got)-1] != MorttonID || !slices.Contains(got, "spirit") {
		t.Errorf("Shades of Mort'ton alone plans %v", got)
	}
}
