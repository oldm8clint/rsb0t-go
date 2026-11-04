package questplan

import (
	"slices"
	"testing"

	"rsb0t-go/internal/scripts/levels"
)

// The built fillers sit in the owner's ladder right before the finale (Cook's Assistant and Sheep Shearer, then Goblin
// Diplomacy, Romeo & Juliet, Ernest the Chicken, Pirate's Treasure, Prince Ali Rescue, Doric's Quest, Gertrude's Cat, Monk's Friend,
// the Clock Tower, Demon Slayer and, last, Black Knights' Fortress, which wants the 12 points they make), and their ladder rows say
// what the modules do.
func TestCookAndSheepSitBeforeTheFinale(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	if at("cook") < 0 || !slices.Equal(OwnerLadder[at("cook"):], ownerFillerTail) {
		t.Fatalf("OwnerLadder %v: want %v at the end", OwnerLadder, ownerFillerTail)
	}
	if at("fremennik") != at("cook")-1 {
		t.Errorf("the fillers follow the Fremennik Trials: %v", OwnerLadder)
	}
	for _, c := range []struct {
		id     string
		module Module
		xp     map[string]int
	}{
		{"cook", CooksAssistant, map[string]int{"cooking": 300}},
		{"sheep", SheepShearer, map[string]int{"crafting": 150}},
		{"doric", DoricsQuest, map[string]int{"mining": 1300}},
		{"fluffs", GertrudesCat, map[string]int{"cooking": 1525}},
	} {
		f, ok := LadderFacts[c.id]
		if !ok {
			t.Fatalf("%s has no ladder row", c.id)
		}
		if f.ID != c.id || f.Name != c.module.Record.Name || f.QP != c.module.Record.QuestPoints || !f.Filler {
			t.Errorf("%s row %+v does not match its module record %+v", c.id, f, c.module.Record)
		}
		if f.Finale || f.TrainSkill != "" || len(f.Gates) != 0 || len(f.Prereqs) != 0 || f.QPNeeded != 0 || f.MinCombat != 0 {
			t.Errorf("%s row %+v: a filler has no gate, no prerequisite and no combat advice", c.id, f)
		}
		if f.EstMinutes <= 0 || f.Region == "" || f.Source == "" {
			t.Errorf("%s row lacks its estimate, region or source: %+v", c.id, f)
		}
		for skill, xp := range c.xp {
			if f.XP[skill] != xp {
				t.Errorf("%s pays %d %s XP in its row, want %d (content's stat_advance / 10)", c.id, f.XP[skill], skill, xp)
			}
		}
		if QuestTabName(c.id) != c.module.Record.Name {
			t.Errorf("QuestTabName(%s) = %q", c.id, QuestTabName(c.id))
		}
		if slot := QuestSlot(c.id, 20*60*1e9); slot <= 0 {
			t.Errorf("%s slot %v", c.id, slot)
		}
	}
}

// The fillers together are the 12 quest points the 15 accounts sitting at 20 are short of for Dragon Slayer's 32: the
// planner takes them by quest points, then name (Goblin Diplomacy and Romeo & Juliet five each, Druidic Ritual and Ernest the
// Chicken four, Prince Ali Rescue three, Pirate's Treasure two, Cook's Assistant and Sheep Shearer one each), and Dragon waits for
// as many as it takes.
func TestCookSheepGobdipRomeoMakeUpTwelveQuestPoints(t *testing.T) {
	a := Account{XP: map[string]int{"hitpoints": levels.XPAtLevel(20)}, Done: map[string]bool{}, Owned: map[int]int{ObjNails: 12}, QP: 20}
	p := PlanLadder([]string{"dragon"}, LadderFacts, a)
	want := []string{"gobdip", "romeo", "druid", "dragon"}
	var got []string
	for _, r := range p.Rungs {
		got = append(got, r.ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("plan %v, want %v:\n%s", got, want, p.Describe(LadderFacts))
	}
	if q := p.Rungs[len(p.Rungs)-2].QP; q != 34 {
		t.Errorf("quest points before Dragon Slayer: %d, want 34 (20 and 5 and 5 and 4)", q)
	}
	// A plan that starts under Black Knights' Fortress's 12 points is never handed it before it has made them.
	a.QP = 0
	for _, r := range PlanLadder([]string{"dragon"}, LadderFacts, a).Rungs {
		if r.ID == "blackknight" && r.QP-3 < 12 {
			t.Errorf("Black Knights' Fortress taken at %d quest points", r.QP-3)
		}
	}
	a.QP = 20
	// Eight short: the two big ones are enough, and the 4-point quest is not reached for.
	a.QP = 24
	p = PlanLadder([]string{"dragon"}, LadderFacts, a)
	got = nil
	for _, r := range p.Rungs {
		got = append(got, r.ID)
	}
	if !slices.Equal(got, []string{"gobdip", "romeo", "dragon"}) {
		t.Errorf("24 quest points: plan %v, want the two big fillers then Dragon", got)
	}
	// Two short of the cap: one big filler is enough, and none of the smaller ones is reached for.
	a.QP = 30
	p = PlanLadder([]string{"dragon"}, LadderFacts, a)
	got = nil
	for _, r := range p.Rungs {
		got = append(got, r.ID)
	}
	if !slices.Equal(got, []string{"gobdip", "dragon"}) {
		t.Errorf("30 quest points: plan %v, want one big filler then Dragon", got)
	}
}
