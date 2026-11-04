package questplan

import (
	"os"
	"slices"
	"testing"
	"time"
)

// Monk's Friend and the Clock Tower sit in the owner's ladder after the other one-point fillers, ahead of Black Knights' Fortress and
// the finale, and their ladder rows say what the modules do.
func TestMonksFriendAndTheClockTowerSitAfterTheOtherFillers(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	d, c, r := at(MonksFriendID), at(ClockTowerID), at(RomeoJulietID)
	if d < 0 || c != d+1 || d <= r || at("dragon") != len(OwnerLadder)-1 || c >= at("dragon") || at(GertrudesCatID) >= d {
		t.Fatalf("OwnerLadder %v: want ... romeo ... fluffs, drunkmonk, cog ... dragon", OwnerLadder)
	}
	for _, x := range []struct {
		id     string
		module Module
		xp     map[string]int
		slot   time.Duration
	}{
		{MonksFriendID, MonksFriend, map[string]int{"woodcutting": 2000}, 37*time.Minute + 30*time.Second},
		{ClockTowerID, ClockTower, nil, 56*time.Minute + 15*time.Second},
	} {
		f, ok := LadderFacts[x.id]
		if !ok {
			t.Fatalf("%s has no ladder row", x.id)
		}
		if f.ID != x.id || f.Name != x.module.Record.Name || f.QP != x.module.Record.QuestPoints || !f.Filler || f.QP != 1 {
			t.Errorf("%s row %+v does not match its module record %+v", x.id, f, x.module.Record)
		}
		if f.Finale || f.TrainSkill != "" || len(f.Gates) != 0 || len(f.Prereqs) != 0 || f.QPNeeded != 0 || f.MinCombat != 0 {
			t.Errorf("%s row %+v: a filler has no gate, no prerequisite and no combat advice (its safety floor is the record's)", x.id, f)
		}
		if f.EstMinutes <= 0 || f.Region == "" || f.Source == "" || f.Notes == "" {
			t.Errorf("%s row lacks its estimate, region, source or notes: %+v", x.id, f)
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
		// The slot is the estimate and a quarter (QuestSlot): long enough for the waits the legs make.
		if got := QuestSlot(x.id, 20*time.Minute); got != x.slot {
			t.Errorf("%s slot %s, want %s", x.id, got, x.slot)
		}
		if _, ok := ModuleFiles[x.id]; !ok || len(ModuleFiles[x.id]) != 2 {
			t.Errorf("%s ModuleFiles %v: want its plan and its customs", x.id, ModuleFiles[x.id])
		}
		for _, file := range ModuleFiles[x.id] {
			if _, err := os.Stat("../../../" + file); err != nil {
				t.Errorf("%s ModuleFiles names %s: %v", x.id, file, err)
			}
		}
		// Neither is held back by a hard gate the ladder knows (the safety floors are record requirements, not holds).
		if holds := LadderHolds(x.module.Record, PlayerState{SkillLevels: map[string]int{}}); len(holds) != 0 {
			t.Errorf("%s is held by %+v for an account with no levels", x.id, holds)
		}
		if why := RungHeld(x.id); why != "" {
			t.Errorf("%s is held back by the ladder: %s", x.id, why)
		}
	}
	// The ids are what the quest tab calls them (internal/quests table: questlist:drunkmonk, questlist:cog).
	if MonksFriendName != "Monk's Friend" || ClockTowerName != "Clock Tower" || MonksFriendID != "drunkmonk" || ClockTowerID != "cog" {
		t.Errorf("names %q %q ids %q %q", MonksFriendName, ClockTowerName, MonksFriendID, ClockTowerID)
	}
}

// The two fillers are planned in the owner's order and not pulled ahead of a deferred quest; each is a plain one-point quest to a
// planner that is short of points: it picks them after the big fillers, in the order of their ids.
func TestMonksFriendAndTheClockTowerInThePlanner(t *testing.T) {
	a := stage5Graduate()
	p := PlanLadder(OwnerLadder, LadderFacts, a)
	for _, id := range []string{MonksFriendID, ClockTowerID} {
		r := rungOf(p, id)
		if indexOf(p, id) < 0 || r.Inserted {
			t.Errorf("%s: at %d, inserted %v", id, indexOf(p, id), r.Inserted)
		}
	}
	// A planner short of exactly one point, with every filler worth more than a point done, reaches for the first of the one-point
	// fillers by id.
	short := stage5Graduate()
	short.QP = 31
	short.Owned[ObjNails] = 12
	short.Done = map[string]bool{}
	for id, f := range LadderFacts {
		if f.Filler && f.QP > 1 {
			short.Done[id] = true
		}
	}
	q := PlanLadder([]string{"dragon"}, LadderFacts, short)
	if got := planIDs(q); len(got) != 2 || got[0] != ClockTowerID || got[1] != "dragon" {
		t.Fatalf("one point short: plan %v:\n%s", got, q.Describe(LadderFacts))
	}
	// A finished quest is never planned again.
	short.Done[ClockTowerID] = true
	q = PlanLadder([]string{"dragon"}, LadderFacts, short)
	if got := planIDs(q); len(got) != 2 || got[0] != CooksAssistantID || got[1] != "dragon" {
		t.Fatalf("one point short with the Clock Tower done: plan %v", got)
	}
	short.Done[CooksAssistantID] = true
	short.Done[DoricsQuestID] = true
	q = PlanLadder([]string{"dragon"}, LadderFacts, short)
	if got := planIDs(q); len(got) != 2 || got[0] != MonksFriendID || got[1] != "dragon" {
		t.Fatalf("one point short with the Clock Tower, Cook's Assistant and Doric's Quest done: plan %v", got)
	}
}
