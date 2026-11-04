package questplan

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// ---- snapshots ----

// bkSnap is a Black Knights' Fortress snapshot: the stage, where the account stands, what it holds and wears, a mid-level
// account (combat 32: the knights hunt it).
func bkSnap(stage int, at tile.Tile, held map[int]int, worn ...int) Snapshot {
	s := Snapshot{
		Journal: StatusInProgress, Stage: stage, StageKnown: true, Tile: at, TileKnown: true,
		InvIDs: map[int]int{}, WornIDs: map[int]int{}, Inv: map[string]int{}, Worn: map[string]int{}, FreeSlots: 20,
		HP: 40, MaxHP: 40, QuestPoints: 20, Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{},
		Levels: map[string]int{"attack": 20, "strength": 20, "defence": 15, "hitpoints": 40, "prayer": 1, "magic": 20, "ranged": 1},
	}
	for id, n := range held {
		s.InvIDs[id] = n
	}
	for _, id := range worn {
		s.WornIDs[id] = 1
	}
	if stage == BKNotStarted {
		s.Journal, s.StageKnown = StatusNotStarted, false
	}
	return s
}

var (
	bkFalador = t3(2960, 3340, 0)
	bkInFront = t3(3016, 3513, 0)
	bkInHall  = t3(3020, 3516, 0)
	bkInWare  = t3(3030, 3507, 1)
)

var bkDisguise = []int{ObjIronChainbody, ObjBronzeMedHelm}

func bkKitRan(s Snapshot) Snapshot { s.Ran["provision:"+KitBlackKnight] = true; return s }

// ---- the module's surface ----

func TestBlackKnightsFortressRecordAndModule(t *testing.T) {
	r := BlackKnightsFortressRecord
	if r.ID != "blackknight" || r.Name != "Black Knight's Fortress" || r.QuestPoints != 3 || r.Requirements.MinQuestPoints != 12 {
		t.Errorf("record %+v: want blackknight, the tab's spelling, 3 quest points, 12 required", r)
	}
	if BlackKnightsFortressID != BlackKnightsFortress.Record.ID || QuestTabName(BlackKnightsFortressID) != BlackKnightsFortressName {
		t.Errorf("the ladder facts name it %q, the tab %q", QuestTabName(BlackKnightsFortressID), BlackKnightsFortressName)
	}
	m := BlackKnightsFortress
	if m.MaxDeaths != 1 || !m.NoRetaliate || m.ReadStage == nil || m.ExclusiveDone == nil || m.Decide == nil {
		t.Errorf("module %+v: one death parks it, it never fights, it reads the stage and gives its slot up", m)
	}
	if !slices.Equal(m.Sustain.FoodIDs, BKFoods) || m.Sustain.EatAt != 18 {
		t.Errorf("sustain %+v", m.Sustain)
	}
	for _, id := range []int{CoinsID, ObjIronChainbody, ObjBronzeMedHelm, ObjCabbage} {
		if !slices.Contains(m.ToolIDs, id) {
			t.Errorf("tool %d is not kept by the module", id)
		}
	}
	if slices.Contains(m.ToolIDs, ObjMagicCabbage) {
		t.Error("the magic cabbage is the wrong one: the module must not keep it")
	}
	// The module's food is the table's, so every bite the sustain eats has a heal the fight model knows.
	for _, f := range BKFoods {
		if FoodBiteHeal(f) == 0 {
			t.Errorf("food %d is not in the food table", f)
		}
	}
}

func TestBlackKnightsFortressFactsRow(t *testing.T) {
	f, ok := LadderFacts[BlackKnightsFortressID]
	if !ok {
		t.Fatal("no ladder row")
	}
	if f.ID != BlackKnightsFortressID || f.Name != BlackKnightsFortressName || f.QP != 3 || !f.Filler || f.QPNeeded != 12 || f.Finale ||
		len(f.Gates) != 0 || len(f.Prereqs) != 0 || f.TrainSkill != "" || f.EstMinutes <= 0 || f.Region == "" || f.Source == "" {
		t.Errorf("row %+v", f)
	}
	// Its slot: 45 minutes and a quarter.
	if got := QuestSlot(BlackKnightsFortressID, 20*time.Minute); got != 56*time.Minute+15*time.Second {
		t.Errorf("slot %s", got)
	}
	if _, ok := ModuleFiles[BlackKnightsFortressID]; !ok {
		t.Error("no ModuleFiles entry")
	}
	// It sits just before Dragon Slayer, after every filler that makes the points it asks for.
	n := len(OwnerLadder)
	if n < 6 || OwnerLadder[n-1] != "dragon" || OwnerLadder[n-2] != BlackKnightsFortressID || !slices.Contains(OwnerLadder[:n-2], RomeoJulietID) {
		t.Fatalf("the ladder ends %v, want ... romeo, the other fillers, blackknight dragon", OwnerLadder[max(0, n-8):])
	}
	// Its minimum is held in two places that must agree: the record (CheckRequirements) and the ladder's own table.
	holds := LadderHolds(BlackKnightsFortressRecord, PlayerState{QuestPoints: 11, SkillLevels: map[string]int{}})
	if len(holds) != 0 {
		t.Errorf("the record already states the quest points, so the ladder table adds no second hold: %+v", holds)
	}
	for _, res := range CheckRequirements(BlackKnightsFortressRecord, PlayerState{QuestPoints: 11}) {
		if res.OK {
			t.Errorf("11 quest points pass the record's requirement")
		}
	}
	if el := Evaluate(BlackKnightsFortressRecord, PlayerState{QuestPoints: 12}, BankSnapshot{}, StatusNotStarted); el.Status != EligReady {
		t.Errorf("12 quest points: %+v", el)
	}
	if el := Evaluate(BlackKnightsFortressRecord, PlayerState{QuestPoints: 9}, BankSnapshot{}, StatusNotStarted); el.Status != EligBlocked {
		t.Errorf("9 quest points: %+v", el)
	}
}

// The planner does not hand out Black Knights' Fortress to a plan that has not made 12 quest points, and does after: the filler
// picker passes over a filler whose own minimum it has not met.
func TestAFillerWithAQuestPointMinimumIsNotPickedBelowIt(t *testing.T) {
	facts := map[string]RungFacts{
		"big":   {ID: "big", Name: "Big", QP: 5, Filler: true},
		"gated": {ID: "gated", Name: "Gated", QP: 3, Filler: true, QPNeeded: 12},
		"small": {ID: "small", Name: "Small", QP: 1, Filler: true},
		"final": {ID: "final", Name: "Final", QP: 1, QPNeeded: 18, Finale: true},
	}
	// 10 points: the big filler makes 15, then the gated one (15 >= 12) makes 18, which is what the finale asks.
	a := Account{XP: map[string]int{}, Done: map[string]bool{}, QP: 10}
	p := PlanLadder([]string{"final"}, facts, a)
	if got := planIDs(p); strings.Join(got, ",") != "big,gated,final" {
		t.Errorf("from 10: %v, want big, gated, final", got)
	}
	// 0 points: the big and the small make 6, the gated one is still out of reach (12), and the plan stops short with a warning
	// instead of taking it early.
	a.QP = 0
	p = PlanLadder([]string{"final"}, facts, a)
	got := planIDs(p)
	if slices.Contains(got, "gated") {
		t.Errorf("from 0 the plan took the gated filler below its minimum: %v", got)
	}
	if len(p.Warnings) == 0 {
		t.Errorf("from 0 the plan reaches 6 points and should say the finale wants more: %v", got)
	}
}

// ---- journal ----

func TestParseBlackKnightsFortressJournal(t *testing.T) {
	for stage, text := range bkJournalFixtures {
		got, ok := ParseBlackKnightsFortressJournal([]string{text})
		if !ok || got != stage {
			t.Errorf("stage %d: parsed %d (ok %v) from %q", stage, got, ok, text)
		}
		// Split on the client's line break and shuffled into several lines: the same answer.
		got, ok = ParseBlackKnightsFortressJournal(strings.Split(text, "|"))
		if !ok || got != stage {
			t.Errorf("stage %d split into lines: parsed %d (ok %v)", stage, got, ok)
		}
	}
	for _, empty := range [][]string{nil, {""}, {"   "}, {"@dbl@Something else entirely"}} {
		if got, ok := ParseBlackKnightsFortressJournal(empty); ok {
			t.Errorf("%q parsed as stage %d: an unrecognised journal is unknown, never stage 0", empty, got)
		}
	}
}

// ---- Decide ----

func TestDecideBlackKnightsFortressByState(t *testing.T) {
	money := map[int]int{CoinsID: 1000}
	food := func(h map[int]int) map[int]int {
		out := map[int]int{ObjShark: 4}
		for k, v := range h {
			out[k] = v
		}
		return out
	}
	with := func(s Snapshot, f func(*Snapshot)) Snapshot { f(&s); return s }
	cases := []struct {
		name string
		s    Snapshot
		kind StepKind
		want func(Step) bool
	}{
		{"complete", with(bkSnap(0, bkFalador, nil), func(s *Snapshot) { s.Journal = StatusComplete }), StepDone, nil},
		{"tab not coloured", with(bkSnap(0, bkFalador, nil), func(s *Snapshot) { s.Journal = StatusUnknown }), StepWait,
			func(st Step) bool { return st.ParkFor == 0 }},
		{"not started, 12 points: Sir Amik", bkSnap(0, bkFalador, nil), StepTalk,
			func(st Step) bool { return st.Stop.ID == NpcSirAmik && st.Stop.Anchor.Level == 2 }},
		{"not started, 11 points: parks for the points", with(bkSnap(0, bkFalador, nil), func(s *Snapshot) { s.QuestPoints = 11 }), StepWait,
			func(st Step) bool {
				return st.ParkFor == BKQuestPointRetry && strings.Contains(st.Reason, "12 quest points")
			}},
		{"not started, 12 points, 29 hitpoints: parks for the hitpoints",
			with(bkSnap(0, bkFalador, nil), func(s *Snapshot) { s.MaxHP, s.Levels["hitpoints"] = 29, 29 }), StepWait,
			func(st Step) bool { return st.ParkFor == BKFitRetry && strings.Contains(st.Reason, "29 hitpoints") }},
		{"stage unread, outside: waits", with(bkSnap(1, bkFalador, nil), func(s *Snapshot) { s.StageKnown = false }), StepWait,
			func(st Step) bool { return st.ParkFor == 0 }},
		{"stage unread, inside: out", with(bkSnap(1, bkInHall, nil), func(s *Snapshot) { s.StageKnown = false }), StepCustom,
			func(st Step) bool { return st.Custom == CustomBKLeave }},

		// Stage 1 outside: the kit, the disguise, the cabbage, the gate, then the grill.
		{"stage 1: the kit first", bkSnap(1, bkFalador, nil), StepProvision, func(st Step) bool {
			return st.KitName == KitBlackKnight && st.BankSet && len(st.Kit) == 5 && slices.Equal(st.KeepIDs, BKKeepIDs())
		}},
		{"stage 1: the kit failed, nothing held: parks for the disguise", with(bkSnap(1, bkFalador, nil), func(s *Snapshot) { s.Failed["provision:"+KitBlackKnight] = true }),
			StepWait, func(st Step) bool {
				return st.ParkFor == BKDisguiseRetry && strings.Contains(st.Reason, "iron chainbody")
			}},
		{"stage 1: chainbody held, not worn: wear it", bkKitRan(bkSnap(1, bkFalador, food(map[int]int{ObjIronChainbody: 1, ObjBronzeMedHelm: 1}))), StepEquip,
			func(st Step) bool { return st.ItemID == ObjIronChainbody }},
		{"stage 1: chainbody worn, helm held: wear the helm", bkKitRan(bkSnap(1, bkFalador, food(map[int]int{ObjBronzeMedHelm: 1}), ObjIronChainbody)), StepEquip,
			func(st Step) bool { return st.ItemID == ObjBronzeMedHelm }},
		{"stage 1: chainbody worn, no helm anywhere: parks for it", bkKitRan(bkSnap(1, bkFalador, food(nil), ObjIronChainbody)), StepWait,
			func(st Step) bool {
				return st.ParkFor == BKDisguiseRetry && strings.Contains(st.Reason, "bronze med helm")
			}},
		{"stage 1: disguised, no cabbage: the field", bkKitRan(bkSnap(1, bkFalador, food(money), bkDisguise...)), StepPickLoc,
			func(st Step) bool { return st.LocID == LocCabbage && st.ExpectItemID == ObjCabbage && st.Op == "Pick" }},
		{"stage 1: disguised, no cabbage, full pack: bank first", with(bkKitRan(bkSnap(1, bkFalador, food(money), bkDisguise...)), func(s *Snapshot) { s.FreeSlots = 1 }),
			StepDeposit, func(st Step) bool {
				return slices.Contains(st.KeepIDs, ObjShark) && slices.Contains(st.KeepIDs, ObjCabbage)
			}},
		{"stage 1: disguised, cabbage, no food: the gate parks it", bkKitRan(bkSnap(1, bkFalador, map[int]int{ObjCabbage: 1}, bkDisguise...)), StepWait,
			func(st Step) bool {
				return st.ParkFor == BKFitRetry && strings.Contains(st.Reason, "not going into the fortress")
			}},
		{"stage 1: disguised, cabbage, food: the grill", bkKitRan(bkSnap(1, bkFalador, food(map[int]int{ObjCabbage: 1}), bkDisguise...)), StepCustom,
			func(st Step) bool { return st.Custom == CustomBKSpy }},
		{"stage 1: inside, nothing else asked", bkSnap(1, bkInHall, nil), StepCustom, func(st Step) bool { return st.Custom == CustomBKSpy }},
		{"stage 1: at the door, the same kit and gate apply", bkSnap(1, bkInFront, nil), StepProvision, nil},

		// A walk that failed on its last try: out if inside, and a long wait if outside; never the same walk again.
		{"stage 1: the grill walk failed, inside: out", with(bkSnap(1, bkInHall, nil), func(s *Snapshot) { s.Failed[CustomBKSpy] = true }), StepCustom,
			func(st Step) bool { return st.Custom == CustomBKLeave }},
		{"stage 1: the grill walk failed, outside: waits for hours", with(bkKitRan(bkSnap(1, bkFalador, food(map[int]int{ObjCabbage: 1}), bkDisguise...)), func(s *Snapshot) { s.Failed[CustomBKSpy] = true }),
			StepWait, func(st Step) bool {
				return st.ParkFor == BKRouteRetry && strings.Contains(st.Reason, "failed on its last try")
			}},
		{"stage 2: the hole walk failed, inside: out", with(bkSnap(2, bkInWare, map[int]int{ObjCabbage: 1}), func(s *Snapshot) { s.Failed[CustomBKSabotage] = true }), StepCustom,
			func(st Step) bool { return st.Custom == CustomBKLeave }},
		{"stage 2: the hole walk failed, outside: waits for hours", with(bkKitRan(bkSnap(2, bkFalador, food(map[int]int{ObjCabbage: 1}), bkDisguise...)), func(s *Snapshot) { s.Failed[CustomBKSabotage] = true }),
			StepWait, func(st Step) bool { return st.ParkFor == BKRouteRetry }},
		// On the open ground by the door: west first (the walker's own way from there runs south past the sentries), once.
		{"stage 1: the grill walk failed at the door: west first", with(bkSnap(1, bkInFront, nil), func(s *Snapshot) { s.Failed[CustomBKSpy] = true }), StepCustom,
			func(st Step) bool {
				return st.Custom == CustomBKLeave && strings.Contains(st.Reason, "clear of the knights")
			}},
		{"stage 2: the hole walk failed at the door: west first", with(bkSnap(2, bkInFront, map[int]int{ObjCabbage: 1}), func(s *Snapshot) { s.Failed[CustomBKSabotage] = true }), StepCustom,
			func(st Step) bool { return st.Custom == CustomBKLeave }},
		{"stage 1: failed at the door, the walk west failed: waits", with(bkSnap(1, bkInFront, nil), func(s *Snapshot) { s.Failed[CustomBKSpy], s.Failed[CustomBKLeave] = true, true }),
			StepWait, func(st Step) bool { return st.ParkFor == BKRouteRetry }},
		{"stage 1: failed at the door, west already walked: waits", with(bkSnap(1, bkInFront, nil), func(s *Snapshot) { s.Failed[CustomBKSpy], s.Ran[CustomBKLeave] = true, true }),
			StepWait, func(st Step) bool { return st.ParkFor == BKRouteRetry }},
		{"stage 1: failed, standing at the west waypoint: waits", with(bkSnap(1, BKWestWaypoint, nil), func(s *Snapshot) { s.Failed[CustomBKSpy] = true }),
			StepWait, func(st Step) bool { return st.ParkFor == BKRouteRetry }},
		{"stage 1: failed, a tile short of the waypoint's reach east: west first", with(bkSnap(1, t3(3005, 3510, 0), nil), func(s *Snapshot) { s.Failed[CustomBKSpy] = true }),
			StepCustom, func(st Step) bool { return st.Custom == CustomBKLeave }},
		{"stage 1: failed, at the waypoint's reach: waits", with(bkSnap(1, t3(3004, 3510, 0), nil), func(s *Snapshot) { s.Failed[CustomBKSpy] = true }),
			StepWait, func(st Step) bool { return st.ParkFor == BKRouteRetry }},
		{"stage 2: a failed leave does not stop it leaving", with(bkSnap(3, bkInWare, nil), func(s *Snapshot) { s.Failed[CustomBKLeave] = true }), StepCustom,
			func(st Step) bool { return st.Custom == CustomBKLeave }},
		{"an unrecognised stage inside: out", bkSnap(9, bkInHall, nil), StepCustom, func(st Step) bool { return st.Custom == CustomBKLeave }},

		// Stage 2.
		{"stage 2: the hole, with the cabbage", bkKitRan(bkSnap(2, bkFalador, food(map[int]int{ObjCabbage: 1}), bkDisguise...)), StepCustom,
			func(st Step) bool { return st.Custom == CustomBKSabotage }},
		{"stage 2: inside with the cabbage: the hole", bkSnap(2, t3(3022, 3510, 0), map[int]int{ObjCabbage: 1}), StepCustom,
			func(st Step) bool { return st.Custom == CustomBKSabotage }},
		{"stage 2: inside with no cabbage: out", bkSnap(2, t3(3022, 3510, 0), nil), StepCustom, func(st Step) bool { return st.Custom == CustomBKLeave }},
		{"stage 2: outside with no cabbage: kit, then the field", bkSnap(2, bkFalador, nil), StepProvision, nil},

		// Stage 3.
		{"stage 3: inside the warehouse: out", bkSnap(3, bkInWare, nil), StepCustom, func(st Step) bool { return st.Custom == CustomBKLeave }},
		{"stage 3: outside: Sir Amik", bkSnap(3, bkFalador, nil), StepTalk, func(st Step) bool { return st.Stop.ID == NpcSirAmik }},
		{"stage 3: at the door: west first, then Sir Amik", bkSnap(3, bkInFront, nil), StepCustom,
			func(st Step) bool {
				return st.Custom == CustomBKLeave && strings.Contains(st.Reason, "before the walk to Sir Amik")
			}},
		{"stage 3: at the door, west already walked: Sir Amik", with(bkSnap(3, bkInFront, nil), func(s *Snapshot) { s.Ran[CustomBKLeave] = true }), StepTalk,
			func(st Step) bool { return st.Stop.ID == NpcSirAmik }},
		{"stage 3: at the door, the walk west failed: Sir Amik", with(bkSnap(3, bkInFront, nil), func(s *Snapshot) { s.Failed[CustomBKLeave] = true }), StepTalk,
			func(st Step) bool { return st.Stop.ID == NpcSirAmik }},
		{"stage 3: at the west waypoint: Sir Amik", bkSnap(3, BKWestWaypoint, nil), StepTalk, func(st Step) bool { return st.Stop.ID == NpcSirAmik }},
		{"an unrecognised stage waits", bkSnap(9, bkFalador, nil), StepWait, func(st Step) bool { return st.ParkFor == 0 }},
	}
	for _, c := range cases {
		st := bkDecide(c.s)
		if st.Kind != c.kind || (c.want != nil && !c.want(st)) {
			t.Errorf("%s: %s %+v", c.name, st.Describe(), st)
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed step: %s", c.name, why)
		}
	}
}

// bkDecide is DecideBlackKnightsFortress through the module.
func bkDecide(s Snapshot) Step { return BlackKnightsFortress.Decide(s) }

// The Sir Amik stop answers both of his menus and has no third: each option text is the content's (the content test pins it).
func TestSirAmikStopPicksTheOptionsThatStartTheQuest(t *testing.T) {
	first := []string{"I seek a quest!", "I don't, I'm just looking around."}
	second := []string{"I laugh in the face of danger!", "I go and cower in a corner at the first sign of danger!"}
	third := []string{"Oh, I suppose I'll give it a go then.", "No, I'm not convinced."}
	for _, c := range []struct {
		menu []string
		want string
	}{{first, "I seek a quest!"}, {second, "I laugh in the face of danger!"}} {
		got, ok := PickPreferred(c.menu, SirAmikStop.Prefer)
		if !ok || got != c.want {
			t.Errorf("menu %v: picked %q (%v), want %q", c.menu, got, ok, c.want)
		}
	}
	if got, ok := PickPreferred(third, SirAmikStop.Prefer); ok {
		t.Errorf("the cower branch's menu %v matched %q: the stop must never take that branch", third, got)
	}
}

// ---- the gate ----

func TestBlackKnightsFortressGate(t *testing.T) {
	levels := func(def, hp, att, mag int) map[string]int {
		return map[string]int{"attack": att, "strength": att, "defence": def, "hitpoints": hp, "prayer": 1, "magic": mag, "ranged": 1}
	}
	mk := func(def, hp, att, mag int, held map[int]int) Snapshot {
		s := bkSnap(1, bkFalador, held)
		s.Levels, s.MaxHP, s.HP = levels(def, hp, att, mag), hp, hp
		return s
	}
	// A knight on a Defence-1 account: the model's expected damage per attack (hit 90%, up to 4, so about 1.8).
	if got := FoePerAttack(FoeBlackKnight, MeleeFighter{Defence: 1}); got < 1.5 || got > 2.0 {
		t.Errorf("a knight's attack on Defence 1 is priced at %.2f hitpoints", got)
	}
	if FoeBlackKnight.PeakHit() != 4 {
		t.Errorf("a knight's hardest hit is %d, content (strength 25 and bonus 16) says 4", FoeBlackKnight.PeakHit())
	}
	cases := []struct {
		name string
		s    Snapshot
		fit  bool
	}{
		{"hunted, 40 hitpoints, Defence 15, 4 sharks", mk(15, 40, 20, 20, map[int]int{ObjShark: 4}), true},
		{"hunted, 40 hitpoints, Defence 15, nothing to eat", mk(15, 40, 20, 20, nil), false},
		{"hunted, 29 hitpoints with food: under the floor", mk(15, 29, 20, 20, map[int]int{ObjShark: 8}), false},
		{"hunted, 30 hitpoints, Defence 1, 2 sharks: short of twice the price", mk(1, 30, 1, 1, map[int]int{ObjShark: 2}), false},
		{"hunted, 30 hitpoints, Defence 1, 4 sharks and 4 bass", mk(1, 30, 1, 1, map[int]int{ObjShark: 4, ObjBass: 4}), true},
		{"a bank's worth of food but not the hitpoints", mk(30, 25, 40, 40, map[int]int{ObjShark: 20}), false},
		{"hunted, 45 hitpoints, Defence 30, 3 sharks", mk(30, 45, 40, 30, map[int]int{ObjShark: 3}), true},
		// Combat above 66: the knights never hunt, only the scripted aggro (6 attacks, not 16).
		{"immune, 22 hitpoints, 1 shark", mk(60, 22, 80, 20, map[int]int{ObjShark: 1}), true},
		{"immune, 19 hitpoints with food: under the immune floor", mk(60, 19, 90, 20, map[int]int{ObjShark: 6}), false},
		{"hitpoints not read: nothing to judge", func() Snapshot { s := mk(15, 40, 20, 20, nil); s.MaxHP = 0; return s }(), true},
	}
	for _, c := range cases {
		ok, why := BKFit(c.s)
		if ok != c.fit {
			t.Errorf("%s: fit %v (%s), want %v; combat %d, damage %.1f, food %d", c.name, ok, why, c.fit, bkCombat(c.s.Levels), BKGauntletDamage(c.s), bkFoodHeld(c.s))
		}
		if !ok && why == "" {
			t.Errorf("%s: refused with no reason", c.name)
		}
	}
	// The combat formula: Fremennik-style accounts. (Defence 15, Hitpoints 40, Prayer 1, Attack and Strength 20, Magic 20) =
	// 0.25*(15+40+0) + 0.325*(40) = 13.75 + 13 = 26; a Magic-61 account: 0.325*floor(91.5) = 29.575 over the base 13.75 -> 43.
	if got := bkCombat(map[string]int{"attack": 20, "strength": 20, "defence": 15, "hitpoints": 40, "prayer": 1}); got != 26 {
		t.Errorf("combat %d, want 26", got)
	}
	if got := bkCombat(map[string]int{"attack": 1, "strength": 1, "defence": 15, "hitpoints": 40, "prayer": 1, "magic": 61}); got != 43 {
		t.Errorf("a Magic 61 account's combat %d, want 43", got)
	}
	// The hunt ceiling is twice a knight's level (Npc.ts: player.combatLevel > type.vislevel * 2).
	if BKHuntCeiling != 2*33 {
		t.Errorf("hunt ceiling %d", BKHuntCeiling)
	}
	s67 := mk(60, 60, 60, 60, nil)
	if c := bkCombat(s67.Levels); c <= 66 || !BKImmune(s67) {
		t.Errorf("combat %d is not above the ceiling", c)
	}
	if BKImmune(mk(1, 10, 1, 1, nil)) || BKImmune(Snapshot{}) {
		t.Error("a weak account or an unread one is not immune")
	}
}

// ---- properties over every state ----

// For every combination of stage, place, held items, worn items and kit state the step is well formed, a wait names its reason, and
// the module never sends an account into the fortress unprepared: a custom grill or hole step outside the fortress is only ever
// returned with the disguise worn, a cabbage held and the gate passed.
func TestBlackKnightsFortressDecideProperties(t *testing.T) {
	places := []tile.Tile{bkFalador, bkInFront, bkInHall, t3(3022, 3510, 0), t3(3016, 3515, 0), bkInWare, t3(3016, 3518, 2)}
	holds := []map[int]int{nil, {ObjCabbage: 1}, {ObjShark: 4}, {ObjShark: 4, ObjCabbage: 1}, {ObjIronChainbody: 1, ObjBronzeMedHelm: 1},
		{ObjIronChainbody: 1, ObjBronzeMedHelm: 1, ObjShark: 4, ObjCabbage: 1}, {ObjMagicCabbage: 1, ObjShark: 8}}
	wears := [][]int{nil, {ObjIronChainbody}, {ObjBronzeMedHelm}, bkDisguise}
	n := 0
	for _, journal := range []Status{StatusNotStarted, StatusInProgress, StatusComplete, StatusUnknown} {
		for stage := 0; stage <= 4; stage++ {
			for _, at := range places {
				for _, h := range holds {
					for _, w := range wears {
						for kit := 0; kit < 5; kit++ {
							for _, free := range []int{0, 1, 20} {
								for _, known := range []bool{true, false} {
									s := bkSnap(stage, at, h, w...)
									s.Journal, s.StageKnown, s.FreeSlots = journal, known && stage > 0, free
									s.QuestPoints = []int{5, 20}[(stage+kit+free+len(h))%2]
									switch kit {
									case 1:
										s.Ran["provision:"+KitBlackKnight] = true
									case 2:
										s.Failed["provision:"+KitBlackKnight] = true
									case 3:
										s.Failed[CustomBKSpy] = true
									case 4:
										s.Failed[CustomBKSabotage] = true
									}
									st := BlackKnightsFortress.Decide(s)
									n++
									if why := ValidateStep(st); why != "" {
										t.Fatalf("malformed step %+v: %s", st, why)
									}
									if st.Kind == StepWait && st.Reason == "" {
										t.Fatalf("a wait with no reason: %+v", st)
									}
									if st.Kind == StepTalk && st.Stop.ID == NpcSirAmik && s.Journal == StatusNotStarted && s.QuestPoints < 12 {
										t.Fatalf("Sir Amik asked for under 12 quest points: %+v", s)
									}
									if st.Kind == StepCustom && (st.Custom == CustomBKSpy || st.Custom == CustomBKSabotage) && !BKInside(at) {
										for _, id := range bkDisguise {
											if s.WornIDs[id] == 0 {
												t.Fatalf("%s from outside with %d not worn: %+v", st.Custom, id, s)
											}
										}
										if s.HeldID(ObjCabbage) == 0 {
											t.Fatalf("%s from outside with no cabbage", st.Custom)
										}
										if ok, why := BKFit(s); !ok {
											t.Fatalf("%s from outside past the gate (%s)", st.Custom, why)
										}
									}
									if st.Kind == StepCustom && st.Custom == CustomBKSabotage && s.HeldID(ObjCabbage) == 0 {
										t.Fatalf("the hole with no cabbage: %+v", s)
									}
									if st.Kind == StepCustom && st.Custom == CustomBKLeave && !BKInside(at) {
										// Outside the fortress it is only the walk west, from the open ground east of the waypoint, once.
										if !BKNeedsClearing(at) || s.Failed[CustomBKLeave] || s.Ran[CustomBKLeave] {
											t.Fatalf("leave from outside the fortress: %+v", s)
										}
									}
									if kit >= 3 && st.Kind == StepCustom && (st.Custom == CustomBKSpy || st.Custom == CustomBKSabotage) {
										t.Fatalf("the walk that failed on its last try was decided again: %+v", s)
									}
									if st.Kind == StepPickLoc && s.HeldID(ObjCabbage) > 0 {
										t.Fatalf("picking a cabbage with one in the pack")
									}
								}
							}
						}
					}
				}
			}
		}
	}
	t.Logf("%d states", n)
}

// ---- the route's plans ----

func legNames(plan []BKLeg) []string {
	var out []string
	for _, l := range plan {
		out = append(out, l.Name)
	}
	return out
}

// The open ground a walk out of the fortress (or one that never got in) ends on, east of the waypoint's reach, is the ground the walker's
// default route crosses to the sentries; the waypoint, the ground west of it, the fortress and everywhere else are not.
func TestBKNeedsClearing(t *testing.T) {
	for _, c := range []struct {
		name string
		at   tile.Tile
		want bool
	}{
		{"the front door's approach", BKDoorApproach, true},
		{"the front door's stand", BKLegs[0].Stand, true},
		{"the sentries' ground", t3(3030, 3502, 0), true},
		{"the open ground a tile past the waypoint's reach", t3(BKWestWaypoint.X+BKWaypointRadius+1, BKWestWaypoint.Z, 0), true},
		{"the waypoint's reach", t3(BKWestWaypoint.X+BKWaypointRadius, BKWestWaypoint.Z, 0), false},
		{"the waypoint", BKWestWaypoint, false},
		{"west of the waypoint", t3(BKWestWaypoint.X-5, BKWestWaypoint.Z, 0), false},
		{"inside the hall", bkInHall, false},
		{"inside the warehouse", bkInWare, false},
		{"Falador", bkFalador, false},
		{"another level over the open ground", t3(3024, 3500, 1), false},
		{"the Wilderness side", t3(3024, 3540, 0), false},
	} {
		if got := BKNeedsClearing(c.at); got != c.want {
			t.Errorf("%s (%v): %v, want %v", c.name, c.at, got, c.want)
		}
	}
	// The waypoint is walked to within the radius the predicate uses: every tile that walk can end on is clear.
	for dx := -BKWaypointRadius; dx <= BKWaypointRadius; dx++ {
		for dz := -BKWaypointRadius; dz <= BKWaypointRadius; dz++ {
			if at := t3(BKWestWaypoint.X+dx, BKWestWaypoint.Z+dz, 0); BKNeedsClearing(at) {
				t.Errorf("%v is within %d of the waypoint and still needs clearing", at, BKWaypointRadius)
			}
		}
	}
}

func TestBKPlans(t *testing.T) {
	toGrill := []string{"the front door, in", "the secret wall, in", "the alcove ladder, up", "the alcove's upper ladder, up", "room A's ladder, down",
		"room A's east door, east", "the cell's east door, east", "room C's ladder, up", "room D's ladder, down", "the sturdy door, west", "the grill ladder, down"}
	if got := legNames(BKPlan(BKOutside, BKGrillShaft)); !slices.Equal(got, toGrill) {
		t.Errorf("outside to the grill:\n got %v\nwant %v", got, toGrill)
	}
	toHole := []string{"the grill ladder, up", "the sturdy door, east", "room D's ladder, up", "room C's ladder, down", "the cell's east door, west", "room A's east door, west",
		"room A's ladder, up", "the alcove's upper ladder, down", "the alcove ladder, down", "the secret wall, out", "the banquet hall's door, in", "the hall ladder, up",
		"the warehouse wall, in"}
	if got := legNames(BKPlan(BKGrillShaft, BKWarehouse)); !slices.Equal(got, toHole) {
		t.Errorf("the grill to the hole:\n got %v\nwant %v", got, toHole)
	}
	out := []string{"the warehouse wall, out", "the hall ladder, down", "the banquet hall's door, out", "the front door, out"}
	if got := legNames(BKPlan(BKWarehouse, BKOutside)); !slices.Equal(got, out) {
		t.Errorf("the warehouse to the open:\n got %v\nwant %v", got, out)
	}
	// From the middle: the plan resumes where the account stands.
	if got := legNames(BKPlan(BKRoomCSouth, BKWarehouse)); len(got) != 12 || got[0] != "the sturdy door, east" {
		t.Errorf("room C south to the hole: %v", got)
	}
	if got := BKPlan(BKGrillShaft, BKGrillShaft); got != nil {
		t.Errorf("a plan from a region to itself: %v", got)
	}
	if got := BKPlan("nowhere", BKOutside); got != nil {
		t.Errorf("a plan from an unknown region: %v", got)
	}
	if l, ok := BKLegNext(BKOutside, BKGrillShaft); !ok || l.Name != "the front door, in" || !l.Disguise {
		t.Errorf("the first crossing: %+v %v", l, ok)
	}
	// Every pair of regions the route joins, joins both ways, and every crossing's regions are distinct.
	for _, l := range BKLegs {
		if l.From == l.To {
			t.Errorf("%s stays in %s", l.Name, l.From)
		}
		back := false
		for _, m := range BKLegs {
			if m.From == l.To && m.To == l.From && m.Loc.X == l.Loc.X && m.Loc.Z == l.Loc.Z {
				back = true
			}
		}
		if !back {
			t.Errorf("%s has no crossing back", l.Name)
		}
		if l.Stand == l.Land {
			t.Errorf("%s: stand and landing are the same tile", l.Name)
		}
		if l.Op == "" || l.Name == "" || l.Loc.ID == 0 {
			t.Errorf("incomplete leg %+v", l)
		}
	}
	// Only the front door checks the disguise; the hall's door is the one with a dialogue; three crossings set the knights on the account.
	var disguise, dialogue, aggro []string
	for _, l := range BKLegs {
		if l.Disguise {
			disguise = append(disguise, l.Name)
		}
		if len(l.Dialogue) > 0 {
			dialogue = append(dialogue, l.Name)
		}
		if l.Aggro {
			aggro = append(aggro, l.Name)
		}
	}
	if !slices.Equal(disguise, []string{"the front door, in"}) || !slices.Equal(dialogue, []string{"the banquet hall's door, in"}) ||
		!slices.Equal(aggro, []string{"the banquet hall's door, in", "the sturdy door, west", "the hall ladder, down"}) {
		t.Errorf("disguise %v dialogue %v aggro %v", disguise, dialogue, aggro)
	}
	// Four crossings are plain doors (loc 1530, which only opens: the account walks through), the rest move the account themselves.
	var opens []string
	for _, l := range BKLegs {
		if l.Opens {
			opens = append(opens, l.Name)
			if l.Loc.ID != LocBKDoor {
				t.Errorf("%s is marked a plain door and is loc %d", l.Name, l.Loc.ID)
			}
		} else if l.Loc.ID == LocBKDoor {
			t.Errorf("%s is a plain door and is not marked", l.Name)
		}
	}
	if !slices.Equal(opens, []string{"room A's east door, east", "room A's east door, west", "the cell's east door, east", "the cell's east door, west"}) {
		t.Errorf("plain doors %v", opens)
	}
	// The aggro legs cost more, so a plan has taken the fewest of them the geometry allows (it needs all three).
	for _, l := range BKLegs {
		if (l.Cost() > 1) != l.Aggro {
			t.Errorf("%s cost %d", l.Name, l.Cost())
		}
	}
}

func TestBKInsideAndNear(t *testing.T) {
	in := []tile.Tile{t3(3016, 3515, 0), t3(3020, 3515, 0), t3(3022, 3517, 0), t3(3025, 3507, 0), t3(3022, 3510, 0), t3(3030, 3509, 0),
		t3(3016, 3518, 1), t3(3030, 3507, 1), t3(3016, 3518, 2), t3(3010, 3515, 3)}
	out := []tile.Tile{bkFalador, bkInFront, t3(3016, 3514, 0), t3(3001, 3510, 0), t3(3024, 3506, 0), t3(3030, 3502, 0), t3(3017, 3512, 0), t3(2999, 3508, 0)}
	for _, p := range in {
		if !BKInside(p) {
			t.Errorf("%v is not inside", p)
		}
	}
	for _, p := range out {
		if BKInside(p) {
			t.Errorf("%v is inside", p)
		}
	}
	if !BKNearFortress(bkInFront) || !BKNearFortress(t3(3030, 3502, 0)) || BKNearFortress(bkFalador) {
		t.Error("BKNearFortress")
	}
	for _, r := range bkInsideL0 {
		if r.Level != 0 {
			t.Errorf("rect %+v is not level 0", r)
		}
	}
}

// ---- the slot and the keep ----

func TestBlackKnightsPastFortress(t *testing.T) {
	cases := []struct {
		s    Snapshot
		want bool
	}{
		{bkSnap(1, bkFalador, nil), false},
		{bkSnap(2, bkInHall, nil), false},
		{bkSnap(3, bkInWare, nil), false},
		{bkSnap(3, bkInFront, nil), true},
		{bkSnap(3, bkFalador, nil), true},
		{with0(bkSnap(1, bkInHall, nil), func(s *Snapshot) { s.Journal = StatusComplete }), true},
	}
	for i, c := range cases {
		if got := BKPastFortress(c.s); got != c.want {
			t.Errorf("case %d: %v", i, got)
		}
	}
}

func with0(s Snapshot, f func(*Snapshot)) Snapshot { f(&s); return s }

func TestBKKitIsWellFormed(t *testing.T) {
	kit := bkKit(bkSnap(1, bkFalador, nil))
	for _, l := range kit {
		if why := ValidateKitLine(l); why != "" {
			t.Error(why)
		}
	}
	byID := map[int]KitLine{}
	for _, l := range kit {
		byID[l.ID] = l
	}
	chain, helm := byID[ObjIronChainbody], byID[ObjBronzeMedHelm]
	if chain.Optional || helm.Optional || !chain.Worn || !helm.Worn || chain.Qty != 1 || helm.Qty != 1 {
		t.Errorf("the disguise is the quest's, wanted worn or held: %+v %+v", chain, helm)
	}
	if len(chain.Sources) != 2 || chain.Sources[0].Shop != WayneShop || chain.Sources[1].Shop != HorvikShop || len(helm.Sources) != 1 || helm.Sources[0].Shop != PeksaShop {
		t.Errorf("shop sources: chainbody %+v, helm %+v", chain.Sources, helm.Sources)
	}
	if c := byID[ObjCabbage]; !c.Optional || c.Qty != 1 || len(c.Sources) != 0 {
		t.Errorf("the cabbage line: %+v (the field is the step's, not the kit's; an account in Falador is nearer the field than Wydin)", c)
	}
	// An account at Port Sarim is nearer Wydin's shelf than the field: a shop before gathering. An account whose position is unknown
	// takes the field.
	portSarim := bkSnap(1, t3(3014, 3212, 0), nil)
	for _, l := range bkKit(portSarim) {
		if l.ID == ObjCabbage && (len(l.Sources) != 1 || l.Sources[0].Kind != SourceShop || l.Sources[0].Shop != WydinShop) {
			t.Errorf("Port Sarim's cabbage line: %+v", l)
		}
	}
	unknown := bkSnap(1, bkFalador, nil)
	unknown.TileKnown = false
	for _, l := range bkKit(unknown) {
		if l.ID == ObjCabbage && len(l.Sources) != 0 {
			t.Errorf("a cabbage line with no position: %+v", l)
		}
	}
	for _, id := range []int{ObjShark, ObjBass} {
		if l := byID[id]; !l.Optional || l.Qty != 4 {
			t.Errorf("food line %+v", l)
		}
		if !slices.Contains(BKFoods, id) {
			t.Errorf("food %d is not eaten by the sustain", id)
		}
	}
	// A bank that holds the lot satisfies the kit from the bank alone, and one that holds only the food asks the shops for the
	// disguise (a coin line is the provisioner's to add).
	bank := map[int]int{ObjIronChainbody: 1, ObjBronzeMedHelm: 1, ObjCabbage: 1, ObjShark: 9, ObjBass: 9}
	plan := PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Bank: bank, BankKnown: true, FreeSlots: 28, Levels: map[string]int{}, Exhausted: map[string]bool{}})
	if plan.Phase != PhaseBank {
		t.Errorf("a stocked bank: phase %v", plan.Phase)
	}
	drawn := map[int]int{}
	for _, c := range plan.Withdraw {
		drawn[c.ID] += c.Count
	}
	for id, n := range map[int]int{ObjIronChainbody: 1, ObjBronzeMedHelm: 1, ObjCabbage: 1, ObjShark: 4, ObjBass: 4} {
		if drawn[id] != n {
			t.Errorf("the bank draw of %d is %d, want %d (%+v)", id, drawn[id], n, plan.Withdraw)
		}
	}
	// With the disguise already worn the kit asks for none of it.
	plan = PlanProvision(kit, ProvisionState{Pack: map[int]int{ObjCabbage: 1, ObjShark: 4, ObjBass: 4}, Worn: map[int]int{ObjIronChainbody: 1, ObjBronzeMedHelm: 1},
		Bank: map[int]int{}, BankKnown: true, FreeSlots: 20, Levels: map[string]int{}, Exhausted: map[string]bool{}})
	if plan.Phase != PhaseSatisfied {
		t.Errorf("a worn disguise and a full pack: phase %v (%+v)", plan.Phase, plan)
	}
	// Keep list: the disguise, the cabbage, the coins and every food.
	keep := BKKeepIDs()
	for _, id := range append([]int{CoinsID, ObjIronChainbody, ObjBronzeMedHelm, ObjCabbage}, BKFoods...) {
		if !slices.Contains(keep, id) {
			t.Errorf("keep list lacks %d", id)
		}
	}
	keep[0] = -1
	if BKKeepIDs()[0] == -1 {
		t.Error("BKKeepIDs returns the module's own slice")
	}
}

func TestBKDescribesItsStepsForTheLog(t *testing.T) {
	s := bkSnap(1, bkInHall, nil)
	if d := bkDecide(s).Describe(); !strings.HasPrefix(d, CustomBKSpy) {
		t.Errorf("describe %q", d)
	}
	s = bkSnap(2, bkInHall, map[int]int{ObjCabbage: 1})
	if d := bkDecide(s).Describe(); !strings.HasPrefix(d, CustomBKSabotage) {
		t.Errorf("describe %q", d)
	}
	_ = fmt.Sprint
}

// bkJournalFixtures are the journal's rendering of each stage: every string of blackknight_journal.rs2's branches joined in order
// (the content test renders them from the script and compares).
var bkJournalFixtures = map[int]string{
	0: "@dbl@I can start this quest at the @dre@White Knight's Castle@dbl@ in|@dre@Falador@dbl@ by speaking to @dre@Sir Amik Varze|@dbl@I must be able to kill @dre@Level 33 Knights@dbl@ and I must have a|@dbl@total of at least @dre@12 Quest Points",
	1: "@dre@Sir Amik Varze @dbl@asked me to investigate the @dre@Black|@dre@Knight's Fortress @dbl@which is located on @dre@Ice Mountain.|",
	2: "@str@Sir Amik Varze asked me to investigate the Black Knight's|@str@Fortress. I sneaked inside disguised as a Guard.|@dbl@I eavesdropped on a Witch and the Black Knight Captain|@dbl@and discovered that their invincibility potion can be|@dbl@destroyed with a normal @dre@cabbage.",
	3: "@str@Sir Amik Varze asked me to investigate the Black Knight's|@str@Fortress. I sneaked inside disguised as a Guard.|@str@I eavesdropped on a Witch and the Black Knight Captain|@str@and discovered that their invincibility potion can be|@str@destroyed with a normal cabbage.|@dbl@Now that I have sabotaged the witch's potion, I can claim my|@dre@reward @dbl@from @dre@Sir Amik Varze @dbl@in the @dre@White Knight's Castle",
	4: "@str@Sir Amik Varze asked me to investigate the Black Knight's|@str@Fortress. I sneaked inside disguised as a Guard.|@str@I eavesdropped on a Witch and the Black Knight Captain|@str@and discovered that their invincibility potion can be|@str@destroyed with a normal cabbage.|@str@I found a cabbage, and used it to destroy the potion, then|@str@claimed my reward for a job well done.||@red@QUEST COMPLETE!|@dre@Reward:|@dbl@3 Quest Points|@dbl@2500gp",
}
