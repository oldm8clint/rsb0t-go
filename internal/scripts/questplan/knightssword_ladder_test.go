package questplan

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// The Knight's Sword sits in the owner's ladder after Dwarf Cannon, ahead of Hazeel Cult, the Elemental Workshop, Black Knights' Fortress
// and the finale, and its ladder row says what the module does.
func TestKnightsSwordSitsAfterDwarfCannonAheadOfHazeelCult(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	s := at(KnightsSwordID)
	if s < 0 || s <= at(DwarfCannonID) || s >= at(HazeelCultID) || s >= at(BlackKnightsFortressID) || at("dragon") != len(OwnerLadder)-1 {
		t.Fatalf("OwnerLadder %v: want ... sheepherder, mcannon, itwatchtower, zanaris, chompybird, mortton, squire, hazeelcult, elemental_workshop, desertrescue, blackknight, dragon", OwnerLadder)
	}
	f, ok := LadderFacts[KnightsSwordID]
	if !ok {
		t.Fatalf("no ladder row")
	}
	m := KnightsSword
	if f.ID != KnightsSwordID || f.Name != m.Record.Name || f.QP != m.Record.QuestPoints || !f.Filler || f.QP != 1 {
		t.Errorf("row %+v does not match its module record %+v", f, m.Record)
	}
	if f.Finale || f.TrainSkill != "" || len(f.Prereqs) != 0 || f.QPNeeded != 0 || f.MinCombat != 0 || len(f.SoftPrereqs) != 0 {
		t.Errorf("row %+v: a filler has no prerequisite and no combat advice (its safety floor is the plan's)", f)
	}
	if f.EstMinutes <= 0 || f.Region == "" || f.Source == "" || f.Notes == "" {
		t.Errorf("row lacks its estimate, region, source or notes: %+v", f)
	}
	if len(f.XP) != 1 || f.XP["smithing"] != 12725 {
		t.Errorf("the row pays %v, want 12,725 Smithing", f.XP)
	}
	if QuestTabName(KnightsSwordID) != "The Knight's Sword" || m.Record.Name != "The Knight's Sword" {
		t.Errorf("QuestTabName = %q, record %q", QuestTabName(KnightsSwordID), m.Record.Name)
	}
	// The slot is the estimate and a quarter (QuestSlot), and no longer than the slot a holder of a fleet-wide slot keeps (60 minutes).
	if got := QuestSlot(KnightsSwordID, 20*time.Minute); got != 56*time.Minute+15*time.Second || got >= time.Hour {
		t.Errorf("slot %s", got)
	}
	if len(ModuleFiles[KnightsSwordID]) != 3 {
		t.Errorf("ModuleFiles %v: want the plan, the dungeon's price and the customs", ModuleFiles[KnightsSwordID])
	}
	for _, file := range ModuleFiles[KnightsSwordID] {
		if _, err := os.Stat("../../../" + file); err != nil {
			t.Errorf("ModuleFiles names %s: %v", file, err)
		}
	}
	if why := RungHeld(KnightsSwordID); why != "" {
		t.Errorf("the ladder holds it back: %s", why)
	}
	if m.ReadStage == nil || m.Decide == nil || m.ExclusiveDone != nil {
		t.Errorf("the module lacks its journal reader or its decision, or releases a slot early")
	}
	if KnightsSwordID != "squire" || KnightsSwordName != "The Knight's Sword" {
		t.Errorf("id %q name %q", KnightsSwordID, KnightsSwordName)
	}
	// The row's note says the dangerous part in the numbers the plan pins.
	for _, want := range []string{"Ice warrior", "never fights", "three days", "never smelted", "12,725"} {
		if !strings.Contains(f.Notes, want) {
			t.Errorf("the row's notes lack %q", want)
		}
	}
}

// The quest asks for Mining 10, Cooking 10 and 60 hitpoints in its record and advises them in its row (a hard gate would insert a training
// rung: none trains Mining or Cooking, and none is run for one filler).
func TestKnightsSwordIsHeldByItsRecordAndNotTrained(t *testing.T) {
	r := KnightsSword.Record.Requirements
	if len(r.Skills) != 3 || len(r.Quests) != 0 || r.MinQuestPoints != 0 {
		t.Fatalf("the record asks for %+v", r)
	}
	want := map[string]int{"mining": 10, "cooking": 10, "hitpoints": 60}
	for _, s := range r.Skills {
		if want[s.Skill] != s.Level {
			t.Errorf("the record asks for %s %d", s.Skill, s.Level)
		}
		delete(want, s.Skill)
	}
	if len(want) != 0 {
		t.Errorf("the record lacks %v", want)
	}
	f := LadderFacts[KnightsSwordID]
	if len(f.Gates) != 3 {
		t.Fatalf("the row's gates are %+v", f.Gates)
	}
	for _, g := range f.Gates {
		if g.Kind != GateAdvised || g.Hard() {
			t.Errorf("gate %+v: advised only", g)
		}
	}
	// No rung trains Mining or Cooking, and the planner inserts no training for an account with none of the three.
	for id, r := range LadderFacts {
		if strings.EqualFold(r.TrainSkill, "mining") || strings.EqualFold(r.TrainSkill, "cooking") {
			t.Errorf("%s trains %s: the hold on the record would be a lie of omission", id, r.TrainSkill)
		}
	}
	if got := planIDs(PlanLadder([]string{KnightsSwordID}, LadderFacts, stage5Graduate())); !slices.Equal(got, []string{KnightsSwordID}) {
		t.Errorf("an account with none of the three is planned %v", got)
	}
	if holds := LadderHolds(KnightsSword.Record, PlayerState{SkillLevels: map[string]int{}}); len(holds) != 0 {
		t.Errorf("the ladder's own holds are %+v: the record's requirement is the one", holds)
	}
	below := CheckRequirements(KnightsSword.Record, PlayerState{SkillLevels: map[string]int{"mining": 9, "cooking": 10, "hitpoints": 59}})
	failed := 0
	for _, r := range below {
		if !r.OK {
			failed++
		}
	}
	if failed != 2 {
		t.Errorf("at Mining 9 and Hitpoints 59 the record fails %d of its three skills: %+v", failed, below)
	}
	for _, r := range CheckRequirements(KnightsSword.Record, PlayerState{SkillLevels: map[string]int{"mining": 10, "cooking": 10, "hitpoints": 60}}) {
		if !r.OK {
			t.Errorf("at the levels the record says %q", r.Reason)
		}
	}
}

// It is planned in the owner's order and not inserted; one point short with every other filler done makes it the filler by id.
func TestKnightsSwordInThePlanner(t *testing.T) {
	p := PlanLadder(OwnerLadder, LadderFacts, stage5Graduate())
	last := indexOf(p, DwarfCannonID)
	for _, id := range []string{KnightsSwordID, HazeelCultID, ElementalWorkshopID, BlackKnightsFortressID, "dragon"} {
		i := indexOf(p, id)
		if i <= last || (id != "dragon" && p.Rungs[i].Inserted) {
			t.Errorf("%s sits at %d after %d (inserted %v)", id, i, last, i >= 0 && p.Rungs[i].Inserted)
		}
		last = i
	}
	// Its 12,725 Smithing experience is counted by the planner (an account that runs it needs less Smithing training later).
	end := p.Final
	if end.XP["smithing"] < 12725 {
		t.Errorf("the planner's account ends with %d Smithing XP", end.XP["smithing"])
	}
	short := stage5Graduate()
	short.QP = 31
	short.Owned[ObjNails] = 12
	short.Done = map[string]bool{}
	for id, f := range LadderFacts {
		if f.Filler && id != KnightsSwordID {
			short.Done[id] = true
		}
	}
	q := PlanLadder([]string{"dragon"}, LadderFacts, short)
	if got := planIDs(q); len(got) != 2 || got[0] != KnightsSwordID {
		t.Fatalf("one point short with only the Knight's Sword left: plan %v", got)
	}
}
