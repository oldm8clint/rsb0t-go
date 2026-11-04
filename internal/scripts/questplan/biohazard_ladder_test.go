package questplan

import (
	"os"
	"slices"
	"testing"
	"time"
)

// Biohazard sits in the owner's ladder at once after Plague City (its prerequisite) and ahead of the last one-point fillers, Black
// Knights' Fortress and the finale, and its ladder row says what the module does.
func TestBiohazardSitsAfterPlagueCity(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	e, b := at(PlagueCityID), at(BiohazardID)
	if e < 0 || b != e+1 || b >= at(BlackKnightsFortressID) || at("dragon") != len(OwnerLadder)-1 {
		t.Fatalf("OwnerLadder %v: want ... elena, biohazard, ..., blackknight, dragon", OwnerLadder)
	}
	f, ok := LadderFacts[BiohazardID]
	if !ok {
		t.Fatal("biohazard has no ladder row")
	}
	if f.ID != BiohazardID || f.Name != Biohazard.Record.Name || f.QP != 3 || f.QP != Biohazard.Record.QuestPoints || !f.Filler {
		t.Errorf("row %+v does not match its module record %+v", f, Biohazard.Record)
	}
	if f.Finale || f.TrainSkill != "" || f.QPNeeded != 0 || !slices.Equal(f.Prereqs, []string{PlagueCityID}) {
		t.Errorf("row %+v: no training, no finale, no quest-point minimum, Plague City first", f)
	}
	for _, g := range f.Gates {
		if g.Hard() {
			t.Errorf("biohazard has the hard gate %+v: no rung trains it", g)
		}
	}
	if f.EstMinutes <= 0 || f.EstMinutes >= 60 || f.Region == "" || f.Source == "" || f.Notes == "" || f.MinCombat <= 0 {
		t.Errorf("the row lacks its estimate (under a slot's hour), region, source, notes or combat advice: %+v", f)
	}
	if len(f.XP) != 1 || f.XP["thieving"] != 1250 {
		t.Errorf("biohazard pays %v, want 1,250 Thieving XP", f.XP)
	}
	if QuestTabName(BiohazardID) != "Biohazard" || BiohazardName != "Biohazard" || BiohazardID != "biohazard" {
		t.Errorf("QuestTabName %q, name %q, id %q", QuestTabName(BiohazardID), BiohazardName, BiohazardID)
	}
	if got, floor := QuestSlot(BiohazardID, 20*time.Minute), time.Duration(f.EstMinutes)*time.Minute; got < floor {
		t.Errorf("the slot %s is under the estimate %s", got, floor)
	}
	files := ModuleFiles[BiohazardID]
	if len(files) != 4 || !slices.Contains(files, "internal/scripts/questplan/biohazard.go") || !slices.Contains(files, "internal/scripts/questmods/biohazard.go") ||
		!slices.Contains(files, "internal/scripts/questplan/plaguecity.go") || !slices.Contains(files, "internal/scripts/questmods/plague.go") {
		t.Errorf("ModuleFiles %v: want its plan, its customs and Plague City's, whose helpers both share", files)
	}
	for _, file := range files {
		if _, err := os.Stat("../../../" + file); err != nil {
			t.Errorf("ModuleFiles names %s: %v", file, err)
		}
	}
	// Not held back by a hard gate the ladder knows, nor by the ladder's own hold list.
	if holds := LadderHolds(Biohazard.Record, PlayerState{SkillLevels: map[string]int{}, CompletedQuests: map[string]bool{PlagueCityID: true}}); len(holds) != 0 {
		t.Errorf("biohazard is held by %+v for an account with no levels", holds)
	}
	if why := RungHeld(BiohazardID); why != "" {
		t.Errorf("biohazard is held back by the ladder: %s", why)
	}
}

// The record asks the ladder (RunLadder, through CheckRequirements) for Plague City: an account short of it is skipped with the reason.
func TestBiohazardRecordAsksTheLadderForPlagueCity(t *testing.T) {
	fail := func(p PlayerState) []string {
		var out []string
		for _, x := range CheckRequirements(BiohazardRecord, p) {
			if !x.OK {
				out = append(out, x.Reason)
			}
		}
		return out
	}
	if got := fail(PlayerState{SkillLevels: map[string]int{}, CompletedQuests: map[string]bool{PlagueCityID: true}}); len(got) != 0 {
		t.Errorf("with Plague City done: %v", got)
	}
	if got := fail(PlayerState{SkillLevels: map[string]int{}, CompletedQuests: map[string]bool{}}); len(got) != 1 {
		t.Errorf("without Plague City: %v", got)
	}
}

// The planner keeps Biohazard behind Plague City whatever order it is given, never inserts it without it, counts its three points,
// and the Thieving it pays is on the plan.
func TestBiohazardInThePlanner(t *testing.T) {
	p := PlanLadder(OwnerLadder, LadderFacts, stage5Graduate())
	if r := rungOf(p, BiohazardID); indexOf(p, BiohazardID) < 0 || r.Inserted {
		t.Errorf("biohazard: at %d, inserted %v", indexOf(p, BiohazardID), r.Inserted)
	}
	if e, b := indexOf(p, PlagueCityID), indexOf(p, BiohazardID); e < 0 || b != e+1 {
		t.Errorf("Plague City at %d and Biohazard at %d", e, b)
	}
	// Alone in a plan, Biohazard pulls its prerequisite in front of it.
	q := PlanLadder([]string{BiohazardID}, LadderFacts, stage5Graduate())
	if got := planIDs(q); !slices.Equal(got, []string{PlagueCityID, BiohazardID}) {
		t.Errorf("Biohazard alone plans %v", got)
	}
	if r := rungOf(q, PlagueCityID); !r.Inserted {
		t.Error("Plague City is not marked inserted when pulled in")
	}
	// A graduate with Plague City done plans it alone, and the points rise by three.
	done := stage5Graduate()
	done.Done = map[string]bool{PlagueCityID: true}
	d := PlanLadder([]string{BiohazardID}, LadderFacts, done)
	if got := planIDs(d); !slices.Equal(got, []string{BiohazardID}) {
		t.Errorf("Biohazard with Plague City done plans %v", got)
	}
	if got := d.Rungs[len(d.Rungs)-1].QP - done.QP; got != 3 {
		t.Errorf("Biohazard adds %d quest points to the plan, want 3", got)
	}
	// A finished Biohazard is not planned again.
	done.Done[BiohazardID] = true
	if got := planIDs(PlanLadder(OwnerLadder, LadderFacts, done)); slices.Contains(got, BiohazardID) {
		t.Errorf("a finished Biohazard is planned again: %v", got)
	}
}
