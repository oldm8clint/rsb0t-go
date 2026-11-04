package questplan

import (
	"os"
	"slices"
	"testing"
	"time"
)

// Tai Bwo Wannai Trio sits in the owner's ladder at once after the Jungle Potion its chief wants, ahead of Black Knights' Fortress and
// the finale, and its ladder row says what the module does.
func TestTaiBwoSitsAfterJunglePotion(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	i := at(TaiBwoID)
	if i < 0 || i != at(JunglePotionID)+1 || i >= at(BlackKnightsFortressID) || at("dragon") != len(OwnerLadder)-1 {
		t.Fatalf("OwnerLadder %v: want ... druid, junglepotion, tbwt, ..., blackknight, dragon", OwnerLadder)
	}
	f, ok := LadderFacts[TaiBwoID]
	if !ok {
		t.Fatal("tbwt has no ladder row")
	}
	m := TaiBwoTrio
	if f.ID != TaiBwoID || f.Name != m.Record.Name || f.QP != 2 || f.QP != m.Record.QuestPoints || !f.Filler {
		t.Errorf("row %+v does not match its module record %+v", f, m.Record)
	}
	if f.Finale || f.TrainSkill != "" || f.QPNeeded != 0 || !slices.Equal(f.Prereqs, []string{JunglePotionID}) {
		t.Errorf("row %+v: no training, no finale, no quest-point minimum, Jungle Potion first", f)
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
		if r.Skill == "hitpoints" {
			continue
		}
		if listed[r.Skill] != r.Level {
			t.Errorf("the record asks %s %d and the row lists %d", r.Skill, r.Level, listed[r.Skill])
		}
	}
	if f.EstMinutes <= 0 || f.Region == "" || f.Source == "" || f.Notes == "" || f.MinCombat <= 0 {
		t.Errorf("the row lacks its estimate, region, source, notes or combat advice: %+v", f)
	}
	if len(f.XP) != 1 || f.XP["fishing"] != 1500 {
		t.Errorf("the row pays %v: Lubufu's lesson is 15,000 tenths of a Fishing level's experience, 1,500 at 1x", f.XP)
	}
	if QuestTabName(TaiBwoID) != "Tai Bwo Wannai Trio" {
		t.Errorf("QuestTabName = %q", QuestTabName(TaiBwoID))
	}
	// The slot is the estimate and a quarter (QuestSlot): long enough for the walks, and the walks are most of it.
	if got, floor := QuestSlot(TaiBwoID, 20*time.Minute), time.Duration(f.EstMinutes)*time.Minute; got < floor*5/4 {
		t.Errorf("slot %s is under its estimate and a quarter", got)
	}
	files := ModuleFiles[TaiBwoID]
	for _, want := range []string{"internal/scripts/questplan/taibwotrio.go", "internal/scripts/questplan/taibwodecide.go", "internal/scripts/questplan/taibwohazards.go",
		"internal/scripts/questmods/tbwt.go"} {
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
	if holds := LadderHolds(m.Record, PlayerState{SkillLevels: map[string]int{}, CompletedQuests: map[string]bool{JunglePotionID: true}}); len(holds) != 0 {
		t.Errorf("held by %+v for an account with no levels", holds)
	}
	if why := RungHeld(TaiBwoID); why != "" {
		t.Errorf("held back by the ladder: %s", why)
	}
	if TaiBwoName != "Tai Bwo Wannai Trio" || TaiBwoID != "tbwt" {
		t.Errorf("name %q id %q", TaiBwoName, TaiBwoID)
	}
}

// The record asks the ladder for what the module needs: an account short of any is skipped with the reason, not run.
func TestTaiBwoRecordAsksTheLadderForItsFloors(t *testing.T) {
	fail := func(p PlayerState) []string {
		var out []string
		for _, x := range CheckRequirements(TaiBwoRecord, p) {
			if !x.OK {
				out = append(out, x.Reason)
			}
		}
		return out
	}
	ready := PlayerState{SkillLevels: map[string]int{"cooking": 30, "agility": 15, "fishing": 5, "magic": TbMinMagic, "hitpoints": TbMinHitpoints},
		CompletedQuests: map[string]bool{JunglePotionID: true}}
	if got := fail(ready); len(got) != 0 {
		t.Errorf("a ready account: %v", got)
	}
	for name, mod := range map[string]func(p *PlayerState){
		"Cooking 29":       func(p *PlayerState) { p.SkillLevels["cooking"] = 29 },
		"Agility 14":       func(p *PlayerState) { p.SkillLevels["agility"] = 14 },
		"Fishing 4":        func(p *PlayerState) { p.SkillLevels["fishing"] = 4 },
		"Magic 42":         func(p *PlayerState) { p.SkillLevels["magic"] = 42 },
		"Hitpoints 34":     func(p *PlayerState) { p.SkillLevels["hitpoints"] = 34 },
		"no Jungle Potion": func(p *PlayerState) { p.CompletedQuests = map[string]bool{} },
	} {
		p := PlayerState{SkillLevels: map[string]int{}, CompletedQuests: map[string]bool{JunglePotionID: true}}
		for k, v := range ready.SkillLevels {
			p.SkillLevels[k] = v
		}
		mod(&p)
		if got := fail(p); len(got) != 1 {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// The planner keeps the quest behind Jungle Potion and Druidic Ritual whatever order it is given, and never in front of the finale.
func TestTaiBwoInThePlanner(t *testing.T) {
	p := PlanLadder(OwnerLadder, LadderFacts, stage5Graduate())
	if i, j := indexOf(p, TaiBwoID), indexOf(p, JunglePotionID); i < 0 || j < 0 || i <= j {
		t.Errorf("Tai Bwo Wannai Trio at %d, Jungle Potion at %d", i, j)
	}
	if r := rungOf(p, TaiBwoID); r.Inserted {
		t.Errorf("the owner's own rung is marked inserted: %+v", r)
	}
	// Alone in a plan, it pulls its prerequisites in front of it.
	q := PlanLadder([]string{TaiBwoID}, LadderFacts, stage5Graduate())
	if got := planIDs(q); !slices.Equal(got, []string{DruidicRitualID, JunglePotionID, TaiBwoID}) {
		t.Errorf("Tai Bwo Wannai Trio alone plans %v", got)
	}
	// Its two quest points count towards the finale's 32.
	a := stage5Graduate()
	a.QP = 20
	r := PlanLadder([]string{"dragon"}, LadderFacts, a)
	for _, x := range r.Rungs {
		if x.ID == TaiBwoID {
			t.Errorf("the planner picked the 2-point quest to make up quest points ahead of the quests with more: %v", planIDs(r))
		}
	}
}
