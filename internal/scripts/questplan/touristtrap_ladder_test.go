package questplan

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// The Tourist Trap sits in the owner's ladder behind Elemental Workshop I (the last filler before it) and ahead of Black Knights' Fortress, which
// wants twelve quest points, and its row says what the module does.
func TestTouristTrapSitsAheadOfBlackKnightsFortress(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	e, d, b := at("elemental_workshop"), at(TouristTrapID), at(BlackKnightsFortressID)
	if e < 0 || d != e+1 || b != d+1 || at("dragon") != len(OwnerLadder)-1 {
		t.Fatalf("OwnerLadder %v: want ... elemental_workshop, desertrescue, blackknight, dragon", OwnerLadder)
	}
	f, ok := LadderFacts[TouristTrapID]
	if !ok {
		t.Fatal("desertrescue has no ladder row")
	}
	if f.ID != TouristTrapID || f.Name != TouristTrap.Record.Name || f.QP != 2 || f.QP != TouristTrap.Record.QuestPoints || !f.Filler {
		t.Errorf("row %+v does not match its module record %+v", f, TouristTrap.Record)
	}
	if f.Finale || f.TrainSkill != "" || f.QPNeeded != 0 || len(f.Prereqs) != 0 {
		t.Errorf("row %+v: no training, no finale, no quest-point minimum and no prerequisite quest", f)
	}
	// The levels are advised on the row and stated on the record: a hard gate on the row would name a training rung the ladder can run, and
	// no rung trains Smithing 20 or Fletching 10 for one filler.
	want := map[string]int{"smithing": TtSmithing, "fletching": TtFletching, "hitpoints": TtMinHitpoints}
	for _, g := range f.Gates {
		if g.Hard() {
			t.Errorf("desertrescue has the hard gate %+v: no rung is run for it", g)
		}
		if want[g.Skill] != g.Level {
			t.Errorf("the gate %+v is not one of %v", g, want)
		}
		delete(want, g.Skill)
	}
	if len(want) != 0 {
		t.Errorf("the row lacks the advised gates %v", want)
	}
	if f.EstMinutes < 90 || f.Region == "" || f.Source == "" || f.Notes == "" || f.MinCombat < 40 {
		t.Errorf("the row lacks its estimate (the run is two hours or more), region, source, notes or combat advice: %+v", f)
	}
	if len(f.XP) != 1 || f.XP["thieving"] != 9300 {
		t.Errorf("desertrescue pays %v, want 9,300 Thieving XP (4,650 twice)", f.XP)
	}
	for _, must := range []string{"Thieving", "heat", "Captain", "unproven"} {
		if !strings.Contains(f.Notes, must) && !strings.Contains(strings.ToLower(f.Notes), strings.ToLower(must)) {
			t.Errorf("the row's notes do not mention %q", must)
		}
	}
	if QuestTabName(TouristTrapID) != "The Tourist Trap" || TouristTrapName != "The Tourist Trap" || TouristTrapID != "desertrescue" {
		t.Errorf("QuestTabName %q, name %q, id %q", QuestTabName(TouristTrapID), TouristTrapName, TouristTrapID)
	}
	if got, floor := QuestSlot(TouristTrapID, 20*time.Minute), time.Duration(f.EstMinutes)*time.Minute; got < floor {
		t.Errorf("the slot %s is under the estimate %s", got, floor)
	}
	// Its files: the plan and the decision, the Prince Ali and Shop on the Way helpers they share, the customs and Hazeel's helper.
	files := ModuleFiles[TouristTrapID]
	for _, must := range []string{"internal/scripts/questplan/touristtrap.go", "internal/scripts/questplan/touristtrapdecide.go", "internal/scripts/questmods/touristtrap.go",
		"internal/scripts/questmods/touristtrap_camp.go", "internal/scripts/questmods/touristtrap_dart.go", "internal/scripts/questmods/touristtrap_mine.go"} {
		if !slices.Contains(files, must) {
			t.Errorf("ModuleFiles %v lacks %s", files, must)
		}
	}
	for _, file := range files {
		if _, err := os.Stat("../../../" + file); err != nil {
			t.Errorf("ModuleFiles names %s: %v", file, err)
		}
	}
	// The ladder's own hold list does not hold it, and neither does the rung hold-back list.
	if why := RungHeld(TouristTrapID); why != "" {
		t.Errorf("desertrescue is held back by the ladder: %s", why)
	}
	if holds := LadderHolds(TouristTrap.Record, PlayerState{SkillLevels: map[string]int{}}); len(holds) != 0 {
		t.Errorf("desertrescue is held by %+v: its levels are advised, not hard gates", holds)
	}
}

// The record asks the ladder (RunLadder, through CheckRequirements) for Smithing 20 and Fletching 10: an account short of either is skipped with
// the reason, never started.
func TestTouristTrapRecordAsksTheLadderForTheLevels(t *testing.T) {
	fail := func(smithing, fletching int) []string {
		var out []string
		for _, x := range CheckRequirements(TouristTrapRecord, PlayerState{SkillLevels: map[string]int{"smithing": smithing, "fletching": fletching}}) {
			if !x.OK {
				out = append(out, x.Reason)
			}
		}
		return out
	}
	if got := fail(20, 10); len(got) != 0 {
		t.Errorf("with the levels: %v", got)
	}
	if got := fail(19, 10); len(got) != 1 || !strings.Contains(got[0], "Smithing 20") {
		t.Errorf("under Smithing 20: %v", got)
	}
	if got := fail(20, 9); len(got) != 1 || !strings.Contains(got[0], "Fletching 10") {
		t.Errorf("under Fletching 10: %v", got)
	}
	if got := fail(1, 1); len(got) != 2 {
		t.Errorf("with neither: %v", got)
	}
}

// The planner schedules it after Elemental Workshop and before Black Knights' Fortress, counts its two points, and a finished one is not planned
// again.
func TestTouristTrapInThePlanner(t *testing.T) {
	p := PlanLadder(OwnerLadder, LadderFacts, stage5Graduate())
	if indexOf(p, TouristTrapID) < 0 {
		t.Fatalf("desertrescue is not in the plan:\n%s", p.Describe(LadderFacts))
	}
	if r := rungOf(p, TouristTrapID); r.Inserted {
		t.Errorf("desertrescue is marked inserted")
	}
	if e, d, b := indexOf(p, "elemental_workshop"), indexOf(p, TouristTrapID), indexOf(p, BlackKnightsFortressID); !(e < d && d < b) {
		t.Errorf("elemental_workshop at %d, desertrescue at %d, blackknight at %d", e, d, b)
	}
	alone := PlanLadder([]string{TouristTrapID}, LadderFacts, stage5Graduate())
	if got := planIDs(alone); !slices.Equal(got, []string{TouristTrapID}) {
		t.Errorf("the Tourist Trap alone plans %v", got)
	}
	if got := alone.Rungs[len(alone.Rungs)-1].QP - stage5Graduate().QP; got != 2 {
		t.Errorf("the Tourist Trap adds %d quest points to the plan, want 2", got)
	}
	done := stage5Graduate()
	done.Done = map[string]bool{TouristTrapID: true}
	if got := planIDs(PlanLadder(OwnerLadder, LadderFacts, done)); slices.Contains(got, TouristTrapID) {
		t.Errorf("a finished Tourist Trap is planned again: %v", got)
	}
}
