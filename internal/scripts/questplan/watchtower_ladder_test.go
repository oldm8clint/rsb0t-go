package questplan

import (
	"os"
	"slices"
	"testing"
	"time"
)

// The Watch Tower sits in the owner's ladder after Dwarf Cannon, ahead of Hazeel Cult, the Elemental Workshop, Black Knights' Fortress and the finale, and
// its ladder row says what the module does.
func TestWatchtowerSitsAfterDwarfCannon(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	w := at(WatchtowerID)
	if w < 0 || w != at(DwarfCannonID)+1 || w >= at(HazeelCultID) || w >= at(BlackKnightsFortressID) || at("dragon") != len(OwnerLadder)-1 {
		t.Fatalf("OwnerLadder %v: want ... sheepherder, mcannon, itwatchtower, hazeelcult, elemental_workshop, blackknight, dragon", OwnerLadder)
	}
	f, ok := LadderFacts[WatchtowerID]
	if !ok {
		t.Fatal("itwatchtower has no ladder row")
	}
	m := Watchtower
	if f.ID != WatchtowerID || f.Name != m.Record.Name || f.QP != m.Record.QuestPoints || f.QP != 4 || !f.Filler {
		t.Errorf("row %+v does not match its module record %+v", f, m.Record)
	}
	if f.Finale || f.TrainSkill != "" || len(f.Prereqs) != 0 || f.QPNeeded != 0 || f.MinCombat != 0 {
		t.Errorf("row %+v: a filler has no prerequisite and no combat advice (its safety floors are the module's)", f)
	}
	if f.EstMinutes <= 0 || f.Region == "" || f.Source == "" || f.Notes == "" {
		t.Errorf("row lacks its estimate, region, source or notes: %+v", f)
	}
	if len(f.XP) != 1 || f.XP["magic"] != 15300 {
		t.Errorf("the row pays %v: stat_advance(magic, 153000) is 15,300 at 1x", f.XP)
	}
	// Each level the content asks is its step's gate, listed as advised and never as a hard gate (no rung trains Herblore, Mining, Thieving or Magic).
	listed := map[string]int{}
	for _, g := range f.Gates {
		if g.Hard() {
			t.Errorf("the hard gate %+v: no rung trains it", g)
		}
		listed[g.Skill] = g.Level
	}
	for skill, level := range map[string]int{"agility": WtAgilityChasm, "thieving": WtThieving, "herblore": WtHerblore, "magic": WtMagic, "mining": WtMining} {
		if listed[skill] != level {
			t.Errorf("the row lists %s %d, the gate is %d", skill, listed[skill], level)
		}
	}
	// The record states none: an account goes as far as it can and parks at the first step it cannot do (the owner's rule).
	if len(m.Record.Requirements.Skills) != 0 || len(m.Record.Requirements.Quests) != 0 {
		t.Errorf("the record states %+v", m.Record.Requirements)
	}
	if QuestTabName(WatchtowerID) != "Watch Tower" || WatchtowerName != "Watch Tower" || WatchtowerID != "itwatchtower" {
		t.Errorf("QuestTabName = %q, name %q, id %q", QuestTabName(WatchtowerID), WatchtowerName, WatchtowerID)
	}
	// The slot is the estimate and a quarter (QuestSlot): the walks are most of it.
	if got, floor := QuestSlot(WatchtowerID, 20*time.Minute), time.Duration(f.EstMinutes)*time.Minute; got < floor*5/4 {
		t.Errorf("slot %s is under its estimate and a quarter", got)
	}
	files := ModuleFiles[WatchtowerID]
	for _, want := range []string{"internal/scripts/questplan/watchtower.go", "internal/scripts/questplan/watchtowerdecide.go", "internal/scripts/questmods/watchtower.go",
		"internal/scripts/questmods/watchtowercity.go", "internal/scripts/questmods/watchtowerenclave.go"} {
		if !slices.Contains(files, want) {
			t.Errorf("ModuleFiles %v lacks %s", files, want)
		}
	}
	for _, file := range files {
		if _, err := os.Stat("../../../" + file); err != nil {
			t.Errorf("ModuleFiles names %s: %v", file, err)
		}
	}
	// The ladder holds nothing back for it: the safety floors are the steps', and the rung is not on the hold list.
	if holds := LadderHolds(m.Record, PlayerState{SkillLevels: map[string]int{}, CompletedQuests: map[string]bool{}}); len(holds) != 0 {
		t.Errorf("held by %+v for an account with no levels", holds)
	}
	if why := RungHeld(WatchtowerID); why != "" {
		t.Errorf("held back by the ladder: %s", why)
	}
	if m.ReadStage == nil || m.Decide == nil || m.ExclusiveDone != nil {
		t.Error("the module lacks its journal reader or its decision, or releases its slot early (it holds it for the whole run: the shamans come last)")
	}
}

// It is planned in the owner's order and not inserted, with no training stop in front of it: nothing in its row is a hard gate.
func TestWatchtowerInThePlanner(t *testing.T) {
	a := stage5Graduate()
	p := PlanLadder(OwnerLadder, LadderFacts, a)
	last := -1
	for _, id := range []string{DwarfCannonID, WatchtowerID, HazeelCultID, BlackKnightsFortressID, "dragon"} {
		i := indexOf(p, id)
		if i <= last || (id != "dragon" && p.Rungs[i].Inserted) {
			t.Errorf("%s sits at %d after %d (inserted %v)", id, i, last, i >= 0 && p.Rungs[i].Inserted)
		}
		last = i
	}
	// Alone in a plan it needs no one before it.
	if got := planIDs(PlanLadder([]string{WatchtowerID}, LadderFacts, stage5Graduate())); !slices.Equal(got, []string{WatchtowerID}) {
		t.Errorf("the Watch Tower alone plans %v", got)
	}
	// Its 15,300 Magic experience is credited by the plan.
	if r := rungOf(p, WatchtowerID); r.Inserted {
		t.Errorf("the owner's own rung is marked inserted: %+v", r)
	}
}
