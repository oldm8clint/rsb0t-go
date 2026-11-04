package questplan

import (
	"os"
	"slices"
	"testing"
	"time"
)

// Tribal Totem and the Fishing Contest sit in the owner's ladder after the other one-point fillers, ahead of Black Knights' Fortress and the
// finale, and their ladder rows say what the modules do.
func TestTotemAndTheFishingContestSitAfterTheOtherFillers(t *testing.T) {
	at := func(id string) int { return slices.Index(OwnerLadder, id) }
	to, fc, cog, bk := at(TribalTotemID), at(FishingContestID), at(ClockTowerID), at(BlackKnightsFortressID)
	// The quests merged in the same wave sit between the Clock Tower and them, and the Observatory and Sea Slug between them and Black
	// Knights' Fortress (ownerFillerTail spells the whole tail).
	if to < 0 || fc != to+1 || to <= cog || bk <= fc || at("dragon") != len(OwnerLadder)-1 || at(RomeoJulietID) >= to {
		t.Fatalf("OwnerLadder %v: want ... drunkmonk, cog, ..., totem, fishingcompo, ..., blackknight, dragon", OwnerLadder)
	}
	for _, x := range []struct {
		id     string
		module Module
		xp     map[string]int
		slot   time.Duration
	}{
		{TribalTotemID, TribalTotem, map[string]int{"thieving": 1775}, 50 * time.Minute},
		{FishingContestID, FishingContest, map[string]int{"fishing": 2437}, 56*time.Minute + 15*time.Second},
	} {
		f, ok := LadderFacts[x.id]
		if !ok {
			t.Fatalf("%s has no ladder row", x.id)
		}
		if f.ID != x.id || f.Name != x.module.Record.Name || f.QP != x.module.Record.QuestPoints || !f.Filler || f.QP != 1 {
			t.Errorf("%s row %+v does not match its module record %+v", x.id, f, x.module.Record)
		}
		// The skill gate is the record's, not the table's: a gate in the table needs a training rung (TestEveryHardGateHasANamedTrainingRung),
		// and no rung trains Thieving or Fishing; the ladder holds an account short of it through the record's requirements.
		if f.Finale || f.TrainSkill != "" || len(f.Gates) != 0 || len(f.Prereqs) != 0 || len(f.SoftPrereqs) != 0 || f.QPNeeded != 0 || f.MinCombat != 0 {
			t.Errorf("%s row %+v: a filler has no gate, no prerequisite and no combat advice (its floors are the record's)", x.id, f)
		}
		if len(x.module.Record.Requirements.Skills) != 1 {
			t.Errorf("%s record %+v states no skill gate", x.id, x.module.Record.Requirements)
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
		// The slot is the estimate and a quarter (QuestSlot), and stays under the hour a slot is held before it is called stale.
		got := QuestSlot(x.id, 20*time.Minute)
		if got != x.slot || got >= time.Hour {
			t.Errorf("%s slot %s, want %s (under an hour)", x.id, got, x.slot)
		}
		if _, ok := ModuleFiles[x.id]; !ok || len(ModuleFiles[x.id]) < 2 {
			t.Errorf("%s ModuleFiles %v: want its plan and its customs", x.id, ModuleFiles[x.id])
		}
		for _, file := range ModuleFiles[x.id] {
			if _, err := os.Stat("../../../" + file); err != nil {
				t.Errorf("%s ModuleFiles names %s: %v", x.id, file, err)
			}
		}
		// Neither is held back by a ladder hold or a held rung (the safety floors are record requirements).
		if holds := LadderHolds(x.module.Record, PlayerState{SkillLevels: map[string]int{}}); len(holds) != 0 {
			t.Errorf("%s is held by %+v for an account with no levels", x.id, holds)
		}
		if why := RungHeld(x.id); why != "" {
			t.Errorf("%s is held back by the ladder: %s", x.id, why)
		}
	}
	// The fishing contest's files include the garlic cupboard it borrows from Vampire Slayer, and the totem's are its own two.
	for _, want := range []string{"internal/scripts/questplan/vampireslayer.go", "internal/scripts/questmods/vampire.go"} {
		if !slices.Contains(ModuleFiles[FishingContestID], want) {
			t.Errorf("fishingcompo ModuleFiles lacks %s", want)
		}
	}
	// The ids are what the quest tab calls them (internal/quests table: questlist:totem, questlist:fishingcompo).
	if TribalTotemName != "Tribal Totem" || FishingContestName != "Fishing Contest" || TribalTotemID != "totem" || FishingContestID != "fishingcompo" {
		t.Errorf("names %q %q ids %q %q", TribalTotemName, FishingContestName, TribalTotemID, FishingContestID)
	}
}

// The two are planned in the owner's order, ahead of Black Knights' Fortress (which wants 12 quest points the fillers above it pay),
// and the planner takes them as one-point fillers, after the larger ones and in the order of their ids.
func TestTotemAndTheFishingContestInThePlanner(t *testing.T) {
	a := stage5Graduate()
	p := PlanLadder(OwnerLadder, LadderFacts, a)
	last := indexOf(p, ClockTowerID)
	for _, id := range []string{TribalTotemID, FishingContestID, BlackKnightsFortressID} {
		r := rungOf(p, id)
		i := indexOf(p, id)
		if i < 0 || r.Inserted || i <= last || i >= indexOf(p, "dragon") {
			t.Errorf("%s: at %d (inserted %v) after %d, want the owner's order ahead of Dragon Slayer", id, i, r.Inserted, last)
		}
		last = i
	}
	// A planner short of exactly one point, with every larger filler done, reaches for the first one-point filler by id; with the
	// ones before them done, the Fishing Contest comes before Tribal Totem and Sheep Shearer (f, s, t).
	short := stage5Graduate()
	short.QP = 31
	short.Owned[ObjNails] = 12
	short.Done = map[string]bool{}
	for id, f := range LadderFacts {
		if f.Filler && f.QP > 1 {
			short.Done[id] = true
		}
	}
	for _, id := range []string{ClockTowerID, CooksAssistantID, DoricsQuestID, MonksFriendID} {
		q := PlanLadder([]string{"dragon"}, LadderFacts, short)
		if got := planIDs(q); len(got) != 2 || got[0] != id || got[1] != "dragon" {
			t.Fatalf("one point short: plan %v, want %s then dragon", got, id)
		}
		short.Done[id] = true
	}
	// The planner knows no Fishing level for a graduate with none, but it does not treat the record's requirement as a gate it must
	// train: a finished quest is never planned again.
	short.Done[FishingContestID] = true
	short.Done[GertrudesCatID] = true
	// The other one-point fillers whose ids sort before Sheep Shearer (Plague City, Jungle Potion, the Sea Slug Quest, Dwarf Cannon, Hazeel
	// Cult, the Elemental Workshop) are done too.
	short.Done[PlagueCityID] = true
	short.Done[JunglePotionID] = true
	short.Done[SeaSlugID] = true
	short.Done[DwarfCannonID] = true
	short.Done[HazeelCultID] = true
	short.Done[ElementalWorkshopID] = true
	q := PlanLadder([]string{"dragon"}, LadderFacts, short)
	if got := planIDs(q); len(got) != 2 || got[0] != SheepShearerID || got[1] != "dragon" {
		t.Fatalf("one point short with Fishing Contest and Gertrude's Cat done: plan %v", got)
	}
	short.Done[SheepShearerID] = true
	q = PlanLadder([]string{"dragon"}, LadderFacts, short)
	if got := planIDs(q); len(got) != 2 || got[0] != TribalTotemID || got[1] != "dragon" {
		t.Fatalf("one point short with all the rest done: plan %v", got)
	}
}
