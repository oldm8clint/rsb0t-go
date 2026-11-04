package questplan

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// The Digsite Quest sits in the owner's ladder among the one- and two-point fillers, ahead of Black Knights' Fortress and the finale, behind
// Druidic Ritual and the Jungle Potion that pay the Herblore towards its level (ownerFillerTail spells the whole tail), and its ladder row says
// what the module does.
func TestTheDigsiteSitsAmongTheFillers(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	d := at(DigsiteID)
	if d < 0 || d <= at(ClockTowerID) || d <= at(DruidicRitualID) || d <= at(JunglePotionID) || d >= at(BlackKnightsFortressID) || at("dragon") != len(OwnerLadder)-1 {
		t.Fatalf("OwnerLadder %v: want ... druid, junglepotion, ..., itexam, ..., blackknight, dragon", OwnerLadder)
	}
	f, ok := LadderFacts[DigsiteID]
	if !ok {
		t.Fatal("the Digsite has no ladder row")
	}
	m := Digsite
	if f.ID != DigsiteID || f.Name != m.Record.Name || f.QP != m.Record.QuestPoints || !f.Filler || f.QP != 2 {
		t.Errorf("row %+v does not match the module record %+v", f, m.Record)
	}
	if f.Finale || f.TrainSkill != "" || len(f.Prereqs) != 0 || f.QPNeeded != 0 || f.MinCombat != 0 {
		t.Errorf("row %+v: a filler has no prerequisite and no combat advice (its safety floor is the plan's)", f)
	}
	if f.EstMinutes <= 0 || f.Region == "" || f.Source == "" || f.Notes == "" {
		t.Errorf("row lacks its estimate, region, source or notes: %+v", f)
	}
	if f.XP["mining"] != 15300 || f.XP["herblore"] != 2000 || len(f.XP) != 2 {
		t.Errorf("the row pays %v", f.XP)
	}
	if QuestTabName(DigsiteID) != m.Record.Name || DigsiteID != "itexam" || DigsiteName != "Digsite Quest" {
		t.Errorf("tab %q id %q name %q", QuestTabName(DigsiteID), DigsiteID, DigsiteName)
	}
	// The slot is the estimate and a quarter, no longer than the slot a holder of a fleet-wide slot keeps (60 minutes) and no longer than the longest
	// slot of the ladder (the Observatory's), so the pass budget is not raised for it.
	got := QuestSlot(DigsiteID, 20*time.Minute)
	if got != 56*time.Minute+15*time.Second || got >= time.Hour || got > QuestSlot(ObservatoryID, 20*time.Minute) {
		t.Errorf("slot %s", got)
	}
	if len(ModuleFiles[DigsiteID]) != 2 {
		t.Errorf("ModuleFiles %v: want its plan and its customs", ModuleFiles[DigsiteID])
	}
	for _, file := range ModuleFiles[DigsiteID] {
		if _, err := os.Stat("../../../" + file); err != nil {
			t.Errorf("ModuleFiles names %s: %v", file, err)
		}
	}
	if why := RungHeld(DigsiteID); why != "" {
		t.Errorf("held back by the ladder: %s", why)
	}
	if m.ReadStage == nil || m.Decide == nil || m.ExclusiveDone != nil {
		t.Error("module wiring: it reads the journal, decides, and holds its fleet slot for the whole run (the tray, the cave workman and the tablet are needed at different stages)")
	}
}

// The levels are the record's and listed as advised: a hard gate must name a training rung the ladder runs, and none trains Thieving or
// Herblore.
func TestTheDigsiteIsHeldByItsRecordAndNotTrained(t *testing.T) {
	req := Digsite.Record.Requirements
	skills := map[string]int{}
	for _, r := range req.Skills {
		skills[r.Skill] = r.Level
	}
	if len(req.Quests) != 0 || req.MinQuestPoints != 0 || len(skills) != 3 || skills["thieving"] != 25 || skills["agility"] != 10 || skills["herblore"] != 10 {
		t.Fatalf("the Digsite record asks for %+v", req)
	}
	f := LadderFacts[DigsiteID]
	if len(f.Gates) != 3 {
		t.Fatalf("the row's gates are %+v", f.Gates)
	}
	for _, g := range f.Gates {
		if g.Kind != GateAdvised || g.Hard() || g.Level != skills[g.Skill] {
			t.Errorf("gate %+v: want an advised gate at the record's level", g)
		}
	}
	for id, r := range LadderFacts {
		if s := strings.ToLower(r.TrainSkill); s == "thieving" || s == "herblore" {
			t.Errorf("%s trains %s: the Digsite's hold on its record would be a lie of omission", id, s)
		}
	}
	if holds := LadderHolds(Digsite.Record, PlayerState{SkillLevels: map[string]int{}}); len(holds) != 0 {
		t.Errorf("the ladder's own holds are %+v: the record's requirements are the ones", holds)
	}
	for _, c := range []struct {
		skill string
		level int
		word  string
	}{{"thieving", 24, "Thieving 25"}, {"agility", 9, "Agility 10"}, {"herblore", 9, "Herblore 10"}} {
		lv := map[string]int{"thieving": 25, "agility": 10, "herblore": 10}
		lv[c.skill] = c.level
		below := CheckRequirements(Digsite.Record, PlayerState{SkillLevels: lv})
		bad := 0
		for _, r := range below {
			if !r.OK {
				bad++
				if !strings.Contains(r.Reason, c.word) {
					t.Errorf("at %s %d the record says %q", c.skill, c.level, r.Reason)
				}
			}
		}
		if bad != 1 {
			t.Errorf("at %s %d the record fails %d requirements, want 1", c.skill, c.level, bad)
		}
	}
	if ev := Evaluate(Digsite.Record, PlayerState{SkillLevels: map[string]int{"thieving": 25, "agility": 10, "herblore": 10}}, BankSnapshot{}, StatusNotStarted); ev.Status != EligReady {
		t.Errorf("an account at the levels is %+v", ev)
	}
}

// It is planned in the owner's order and not inserted: an account at Thieving 1 does not grow a training stop for it, and a planner short of the
// two points reaches for it when it is the only filler left.
func TestTheDigsiteInThePlanner(t *testing.T) {
	a := stage5Graduate()
	p := PlanLadder(OwnerLadder, LadderFacts, a)
	i := indexOf(p, DigsiteID)
	if i < 0 || p.Rungs[i].Inserted {
		t.Fatalf("the plan puts the Digsite at %d (inserted %v)", i, i >= 0 && p.Rungs[i].Inserted)
	}
	if i > indexOf(p, "dragon") || i > indexOf(p, BlackKnightsFortressID) {
		t.Errorf("the Digsite is planned after the finale or Black Knights' Fortress: %v", planIDs(p))
	}
	for _, r := range p.Rungs {
		if strings.HasPrefix(r.ID, "train:thieving") || strings.HasPrefix(r.ID, "train:herblore") {
			t.Errorf("the plan inserted %s", r.ID)
		}
	}
	two := stage5Graduate()
	two.QP = 30
	two.Owned[ObjNails] = 12
	two.Done = map[string]bool{}
	for id, f := range LadderFacts {
		if f.Filler && id != DigsiteID && id != ObservatoryID {
			two.Done[id] = true
		}
	}
	two.Done[ObservatoryID] = false
	q := PlanLadder([]string{"dragon"}, LadderFacts, two)
	got := planIDs(q)
	if len(got) != 2 || (got[0] != DigsiteID && got[0] != ObservatoryID) {
		t.Fatalf("two points short with only the two-point fillers left: plan %v", got)
	}
}
