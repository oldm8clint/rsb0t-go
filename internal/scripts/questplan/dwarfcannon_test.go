package questplan

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Dwarf Cannon (W13, 19 Oct 2026): the journal parser over the pages the content renders, every state of Decide, the bats' price, the
// railings' floor and the module's identity. The ids, tiles, rolls and scripts the plan rests on are checked against the content in
// dwarfcannon_content_test.go and against the real pack in questmods.

const cannonRecruit = "@str@I have spoken to the Commander, he recruited me|@str@into the Black Guard and asked me to help the dwarves.||"

// cannonPages are the pages mcannon_journal.rs2 renders for each %mcannon value (and the two sub-states of stages 1 and 2), `|` the
// client's line break.
var cannonPages = map[int]string{
	McNotStarted:   "@dbl@I can start this quest by speaking to the @dre@Commander of the Black Watch@dbl@. He is defending an area @dre@north-west of the Fishing Guild@dbl@ against @dre@goblin@dbl@ attack.|",
	McRailings:     cannonRecruit + "@dbl@My first task is to @dre@fix the broken railings@dbl@ in the dwarves defensive perimeter.",
	McRailingsDone: cannonRecruit + "@str@I have repaired all the broken railings,|@dbl@I should report back to the Commander.",
	McTower:        cannonRecruit + "@str@I have repaired all the broken railings,|@dbl@The Commander has asked me to check up on his guards at @dre@the watchtower@dbl@ to the South of his camp.",
	McTowerRemains: cannonRecruit + "@str@I went to the watchtower where I found some dwarf|@str@remains.|@dbl@I should take them back to @dre@the Commander@dbl@.",
	McGoblinCave:   cannonRecruit + "@str@I gave the remains to the Commander.|@dbl@He sent me to find the @dre@Goblin base@dbl@.",
	McFindChild:    cannonRecruit + "@str@I found the Goblin's base.|@dbl@Next I need to find the @dre@Dwarf child@dbl@.",
	McChildRescued: cannonRecruit + "@str@I have rescued the dwarf child and sent him back to the|@str@Commander.|@dbl@I need to @dre@speak to the Commander@dbl@ again.",
	McFixCannon:    cannonRecruit + "@dbl@The Commander has asked me to @dre@fix the multicannon@dbl@.",
	McCannonFixed:  cannonRecruit + "@str@I've fixed the broken multicannon,|@dbl@I need to @dre@speak to the Commander@dbl@ again.",
	McSeeNulodion:  cannonRecruit + "@dbl@The Commander asked me to find @dre@Nulodion the Cannon Engineer@dbl@, he needs to know what @dre@ammunition the multicannon@dbl@ fires.",
	McReturnNotes:  cannonRecruit + "@str@I've spoken to Nulodion,|@str@He gave me an ammo mould and notes,|@dbl@I need to @dre@speak to the Commander@dbl@ again.",
	McComplete: cannonRecruit + "@str@I've helped the dwarves keep out the Goblins, and found|@str@the remains of their friend.|@str@I fixed the cannon and got a mould for the Commander.|" +
		"@dbl@I can now @dre@buy a multicannon@dbl@ from @dre@Nulodion@dbl@ as a reward.||@red@QUEST COMPLETE!",
}

func TestDwarfCannonJournalPages(t *testing.T) {
	for want, page := range cannonPages {
		got, ok := ParseDwarfCannonJournal(strings.Split(page, "|"))
		if !ok || got != want {
			t.Errorf("stage %d: parsed as %d (ok %v)", want, got, ok)
		}
	}
	// Stage 7 reads like stage 6 (the page is the same text), so the parser has one answer for both.
	if got, ok := ParseDwarfCannonJournal(strings.Split(cannonPages[McFixCannon], "|")); !ok || got != McFixCannon {
		t.Errorf("stage 6/7: %d (ok %v)", got, ok)
	}
	for _, page := range []string{"", "@dbl@Another quest's page"} {
		if got, ok := ParseDwarfCannonJournal([]string{page}); ok {
			t.Errorf("%q parsed as stage %d", page, got)
		}
	}
	for stage, want := range map[int]int{McRailingsDone: McRailings, McTowerRemains: McTower, McChildRescued: McChildRescued, McNotStarted: McNotStarted, McComplete: McComplete} {
		if got := CannonStageNumber(stage); got != want {
			t.Errorf("CannonStageNumber(%d) = %d, want %d", stage, got, want)
		}
	}
}

func cannonSnap(stage int, inv map[int]int) Snapshot {
	s := Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 20,
		Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{}, TileKnown: true, Tile: CannonCommanderAnchor,
		MaxHP: 60, HP: 60, Levels: map[string]int{"attack": 70, "strength": 70, "defence": 70, "hitpoints": 60}}
	for id, n := range inv {
		s.InvIDs[id] = n
	}
	return s
}

func TestDecideDwarfCannon(t *testing.T) {
	talks := func(st Step, npc string) bool { return st.Kind == StepTalk && st.Stop.Npc == npc }
	custom := func(st Step, name string) bool { return st.Kind == StepCustom && st.Custom == name }
	food := map[int]int{ObjShark: 4}

	if st := DecideDwarfCannon(Snapshot{Journal: StatusComplete}); st.Kind != StepDone {
		t.Errorf("complete: %v", st)
	}
	if st := DecideDwarfCannon(Snapshot{Journal: StatusUnknown}); st.Kind != StepWait || st.ParkFor != 0 {
		t.Errorf("unknown tab: %v", st)
	}
	if st := DecideDwarfCannon(Snapshot{Journal: StatusInProgress}); st.Kind != StepWait {
		t.Errorf("a stage that was not read: %v", st)
	}
	// Start: the food round once a run, room for six railings, then the Commander.
	fed := map[string]bool{"provision:" + KitCannonFood: true}
	if st := DecideDwarfCannon(Snapshot{Journal: StatusNotStarted, FreeSlots: 20}); st.Kind != StepProvision || st.KitName != KitCannonFood {
		t.Errorf("not started, no food: %v", st)
	}
	if st := DecideDwarfCannon(Snapshot{Journal: StatusNotStarted, FreeSlots: 20, Ran: fed}); !talks(st, "Dwarf Commander") {
		t.Errorf("not started: %v", st)
	}
	full := Snapshot{Journal: StatusNotStarted, FreeSlots: CannonStartRoom - 1, Ran: fed}
	if st := DecideDwarfCannon(full); st.Kind != StepDeposit || !slices.Contains(st.KeepIDs, ObjCannonRailing) {
		t.Errorf("a full pack at the start: %v", st)
	}
	atStart := cannonSnap(McNotStarted, nil)
	atStart.Ran = fed
	if st := DecideDwarfCannon(atStart); !talks(st, "Dwarf Commander") {
		t.Errorf("stage 0: %v", st)
	}
	weak := cannonSnap(McNotStarted, nil)
	weak.Ran = fed
	weak.MaxHP, weak.Levels["hitpoints"] = 15, 15
	if st := DecideDwarfCannon(weak); st.Kind != StepWait || st.ParkFor != CannonFitPark || !strings.Contains(st.Reason, "Dwarf Cannon") {
		t.Errorf("15 hitpoints: %v", st)
	}
	// A hunted account without food is parked at the start, after the food round came back empty.
	hunted := cannonSnap(McNotStarted, nil)
	hunted.Ran = fed
	hunted.Levels = map[string]int{"attack": 40, "strength": 40, "defence": 40, "hitpoints": 45}
	hunted.MaxHP, hunted.HP = 45, 45
	if st := DecideDwarfCannon(hunted); st.Kind != StepWait || st.ParkFor != CannonFitPark || !strings.Contains(st.Reason, "giant bats") {
		t.Errorf("a hunted account with no food: %v", st)
	}
	hunted.InvIDs[ObjShark] = 4
	if st := DecideDwarfCannon(hunted); !talks(st, "Dwarf Commander") {
		t.Errorf("a hunted account with sharks: %v", st)
	}

	// Stage 1: food once a run (the account is strong enough not to be priced against the bats, but the railings cut), then the railings.
	rail := cannonSnap(McRailings, map[int]int{ObjCannonRailing: 6})
	if st := DecideDwarfCannon(rail); st.Kind != StepProvision || st.KitName != KitCannonFood {
		t.Errorf("railings with no food: %v", st)
	}
	rail.Ran["provision:"+KitCannonFood] = true
	if st := DecideDwarfCannon(rail); !custom(st, CustomCannonRailings) {
		t.Errorf("railings: %v", st)
	}
	rail.Failed[CustomCannonRailings] = true
	if st := DecideDwarfCannon(rail); st.Kind != StepWait || st.ParkFor != CannonLegPark {
		t.Errorf("failed railings: %v", st)
	}
	withFood := cannonSnap(McRailings, map[int]int{ObjCannonRailing: 3, ObjShark: 2})
	if st := DecideDwarfCannon(withFood); !custom(st, CustomCannonRailings) {
		t.Errorf("railings with food: %v", st)
	}
	none := cannonSnap(McRailings, map[int]int{ObjShark: 2})
	if st := DecideDwarfCannon(none); !talks(st, "Dwarf Commander") {
		t.Errorf("no railings left in the pack: %v", st)
	}
	if st := DecideDwarfCannon(cannonSnap(McRailingsDone, nil)); !talks(st, "Dwarf Commander") {
		t.Errorf("railings done: %v", st)
	}

	// Stage 2: the tower, then the Commander.
	if st := DecideDwarfCannon(cannonSnap(McTower, nil)); !custom(st, CustomCannonTower) {
		t.Errorf("tower: %v", st)
	}
	tower := cannonSnap(McTower, nil)
	tower.Failed[CustomCannonTower] = true
	if st := DecideDwarfCannon(tower); st.Kind != StepWait || st.ParkFor != CannonLegPark {
		t.Errorf("failed tower: %v", st)
	}
	if st := DecideDwarfCannon(cannonSnap(McTowerRemains, nil)); !talks(st, "Dwarf Commander") {
		t.Errorf("remains held: %v", st)
	}

	// Stages 3 and 4: food, then the cave; stage 5: out of the cave, then the Commander.
	for _, stage := range []int{McGoblinCave, McFindChild} {
		if st := DecideDwarfCannon(cannonSnap(stage, nil)); st.Kind != StepProvision || st.KitName != KitCannonFood {
			t.Errorf("stage %d with no food: %v", stage, st)
		}
		if st := DecideDwarfCannon(cannonSnap(stage, food)); !custom(st, CustomCannonChild) {
			t.Errorf("stage %d: %v", stage, st)
		}
	}
	cave := cannonSnap(McChildRescued, nil)
	cave.Tile = tile.Tile{X: 2571, Z: 9849}
	if st := DecideDwarfCannon(cave); !custom(st, CustomCannonOut) {
		t.Errorf("child rescued, in the cave: %v", st)
	}
	if st := DecideDwarfCannon(cannonSnap(McChildRescued, nil)); !talks(st, "Dwarf Commander") {
		t.Errorf("child rescued, out of it: %v", st)
	}

	// Stages 6 and 7: the tool kit, the repair.
	if st := DecideDwarfCannon(cannonSnap(McFixCannon, nil)); !talks(st, "Dwarf Commander") {
		t.Errorf("no tool kit: %v", st)
	}
	if st := DecideDwarfCannon(cannonSnap(McFixCannon, map[int]int{ObjCannonToolkit: 1})); !custom(st, CustomCannonRepair) {
		t.Errorf("tool kit: %v", st)
	}
	// Stage 8 is the long conversation, 9 and 10 Nulodion and the hand-in.
	if st := DecideDwarfCannon(cannonSnap(McCannonFixed, nil)); !custom(st, CustomCannonReport) {
		t.Errorf("cannon fixed: %v", st)
	}
	if st := DecideDwarfCannon(cannonSnap(McSeeNulodion, nil)); !talks(st, "Nulodion") {
		t.Errorf("see Nulodion: %v", st)
	}
	crowded := cannonSnap(McSeeNulodion, nil)
	crowded.FreeSlots = 2
	if st := DecideDwarfCannon(crowded); st.Kind != StepDeposit {
		t.Errorf("a full pack at Nulodion: %v", st)
	}
	if st := DecideDwarfCannon(cannonSnap(McReturnNotes, map[int]int{ObjCannonNotes: 1, ObjCannonMould: 1})); !talks(st, "Dwarf Commander") {
		t.Errorf("both items: %v", st)
	}
	if st := DecideDwarfCannon(cannonSnap(McReturnNotes, map[int]int{ObjCannonNotes: 1})); !talks(st, "Nulodion") {
		t.Errorf("a lost mould: %v", st)
	}
	if st := DecideDwarfCannon(cannonSnap(McComplete, nil)); st.Kind != StepDone {
		t.Errorf("stage 11: %v", st)
	}
	if st := DecideDwarfCannon(cannonSnap(99, nil)); st.Kind != StepWait {
		t.Errorf("a stage nothing names: %v", st)
	}
}

// Every step the module asks for over a sweep of states is well formed.
func TestDwarfCannonStepsAreWellFormed(t *testing.T) {
	for _, stage := range []int{McNotStarted, McRailings, McRailingsDone, McTower, McTowerRemains, McGoblinCave, McFindChild, McChildRescued, McFixCannon, McCannonFixed,
		McSeeNulodion, McReturnNotes, McComplete} {
		for mask := 0; mask < 1<<6; mask++ {
			inv := map[int]int{}
			for i, id := range []int{ObjCannonRailing, ObjCannonToolkit, ObjCannonNotes, ObjCannonMould, ObjShark, 2309} {
				if mask>>i&1 == 1 {
					inv[id] = 3
				}
			}
			s := cannonSnap(stage, inv)
			s.FreeSlots = (mask % 4) * 3
			if mask%5 == 0 {
				s.Tile = tile.Tile{X: 2600, Z: 9800}
			}
			if mask%3 == 0 {
				s.Levels = map[string]int{"attack": 10, "strength": 10, "defence": 10, "hitpoints": 25}
				s.MaxHP, s.HP = 25, 25
			}
			st := DecideDwarfCannon(s)
			if st.Kind == StepWait || st.Kind == StepDone {
				continue
			}
			if why := ValidateStep(st); why != "" {
				t.Fatalf("stage %d mask %b: %s (%s)", stage, mask, why, st.Describe())
			}
		}
	}
}

// The bats hunt an account of combat 54 and under, and that account is priced against two of them on its food; one above is not.
func TestCannonHuntFit(t *testing.T) {
	strong := cannonSnap(McRailings, nil)
	if c := DemonCombat(strong); c <= CannonBatCeiling+1 {
		t.Fatalf("the strong account is combat %d", c)
	}
	if ok, why := CannonHuntFit(strong); !ok {
		t.Errorf("a combat %d account with no food is priced: %s", DemonCombat(strong), why)
	}
	mid := cannonSnap(McRailings, nil)
	mid.Levels = map[string]int{"attack": 40, "strength": 40, "defence": 40, "hitpoints": 45}
	mid.MaxHP, mid.HP = 45, 45
	if c := DemonCombat(mid); c > CannonBatCeiling {
		t.Fatalf("the mid account is combat %d", c)
	}
	if ok, why := CannonHuntFit(mid); ok || !strings.Contains(why, "giant bats") {
		t.Errorf("a hunted account with no food: %v %q", ok, why)
	}
	mid.InvIDs[ObjShark] = 4
	if ok, why := CannonHuntFit(mid); !ok {
		t.Errorf("a hunted account of 40s with four sharks is refused: %s", why)
	}
	weak := cannonSnap(McRailings, map[int]int{ObjShark: 4})
	weak.Levels = map[string]int{"attack": 5, "strength": 5, "defence": 5, "hitpoints": 25}
	weak.MaxHP, weak.HP = 25, 25
	if ok, _ := CannonHuntFit(weak); ok {
		t.Error("a level 5 fighter with 25 hitpoints is sent among the bats")
	}
	// Unread levels pass, as every other gate treats an unread stat.
	if ok, why := CannonHuntFit(Snapshot{}); !ok {
		t.Errorf("an unread account is refused: %s", why)
	}
}

// The railings' floor keeps a failed roll from killing the account: two cuts in a row leave it standing, and it is never under two fifths
// of its health.
func TestCannonRailingFloor(t *testing.T) {
	for _, c := range []struct{ maxHP, cut, floor int }{{10, 1, 10}, {20, 2, 13}, {29, 2, 13}, {30, 3, 13}, {50, 3, 20}, {99, 3, 39}} {
		if got := CannonRailingCut(c.maxHP); got != c.cut {
			t.Errorf("max %d: a cut is %d, want %d", c.maxHP, got, c.cut)
		}
		if got := CannonRailingFloor(c.maxHP); got != c.floor {
			t.Errorf("max %d: floor %d, want %d", c.maxHP, got, c.floor)
		}
		if f := CannonRailingFloor(c.maxHP); f-2*CannonRailingCut(c.maxHP) < 1 || (c.maxHP > cannonSustain.EatAt+1 && f <= cannonSustain.EatAt) {
			t.Errorf("max %d: floor %d does not survive two cuts, or is at the engine's eat line", c.maxHP, f)
		}
	}
}

func TestDwarfCannonModuleAndRow(t *testing.T) {
	m := DwarfCannon
	if m.Record.ID != "mcannon" || m.Record.Name != "Dwarf Cannon" || m.Record.QuestPoints != 1 {
		t.Errorf("record %+v", m.Record)
	}
	if req := m.Record.Requirements; len(req.Skills) != 1 || req.Skills[0].Skill != "hitpoints" || req.Skills[0].Level != CannonMinHitpoints || len(req.Quests) != 0 {
		t.Errorf("requirements %+v", req)
	}
	if m.NoRetaliate || m.MaxDeaths != 1 || m.ReadStage == nil || m.ExclusiveDone == nil || len(m.ToolIDs) == 0 || m.Bank != EastArdougneNorth || len(m.Sustain.FoodIDs) == 0 {
		t.Errorf("module %+v", m)
	}
	for _, id := range []int{ObjCannonRailing, ObjCannonToolkit, ObjCannonNotes, ObjCannonMould} {
		if !slices.Contains(m.ToolIDs, id) || !slices.Contains(cannonKeep(), id) {
			t.Errorf("item %d is not kept", id)
		}
	}
	for _, c := range []struct {
		s    Snapshot
		want bool
	}{
		{Snapshot{Journal: StatusComplete}, true},
		{Snapshot{Journal: StatusInProgress, Stage: McChildRescued, StageKnown: true}, true},
		{Snapshot{Journal: StatusInProgress, Stage: McFixCannon, StageKnown: true}, true},
		{Snapshot{Journal: StatusInProgress, Stage: McFindChild, StageKnown: true}, false},
		{Snapshot{Journal: StatusInProgress, Stage: McRailingsDone, StageKnown: true}, false},
		{Snapshot{Journal: StatusInProgress, Stage: McTowerRemains, StageKnown: true}, false},
		{Snapshot{Journal: StatusInProgress, Stage: McRailings}, false},
	} {
		if got := m.ExclusiveDone(c.s); got != c.want {
			t.Errorf("ExclusiveDone(%+v) = %v, want %v", c.s, got, c.want)
		}
	}
	f, ok := LadderFacts[DwarfCannonID]
	if !ok || f.ID != DwarfCannonID || f.Name != DwarfCannonName || f.QP != 1 || !f.Filler || f.Finale || f.XP["crafting"] != 750 || f.EstMinutes <= 0 {
		t.Errorf("ladder row %+v", f)
	}
	if len(f.Gates) != 1 || f.Gates[0].Hard() || f.Gates[0].Skill != "hitpoints" {
		t.Errorf("the row's gates %+v: the floor is advised, the record holds it", f.Gates)
	}
	if QuestTabName(DwarfCannonID) != "Dwarf Cannon" {
		t.Errorf("QuestTabName = %q", QuestTabName(DwarfCannonID))
	}
	if got := QuestSlot(DwarfCannonID, 20*time.Minute); got >= time.Hour {
		t.Errorf("slot %s is not under an hour", got)
	}
	// The cave's rectangle is the mapsquare.
	for _, c := range []struct {
		t    tile.Tile
		want bool
	}{{CannonCaveArrive, true}, {CannonCrate, true}, {CannonMudPile, true}, {CannonCaveExit, false}, {CannonCommanderAnchor, false}, {tile.Tile{X: 2571, Z: 9850, Level: 1}, false}} {
		if InCannonCave(c.t) != c.want {
			t.Errorf("InCannonCave(%v) = %v", c.t, !c.want)
		}
	}
	for i, spot := range CannonRailingSpots {
		if CannonRailingLoc(i) != 15+i || spot.X < 2550 || spot.X > 2575 || spot.Z < 3455 || spot.Z > 3480 {
			t.Errorf("railing %d: loc %d at %v", i, CannonRailingLoc(i), spot)
		}
	}
}
