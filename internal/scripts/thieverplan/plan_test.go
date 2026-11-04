package thieverplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

func TestMethodsTableIntegrity(t *testing.T) {
	if len(METHODS) != 24 {
		t.Fatalf("METHODS has %d entries, want 24 (11 pickpocket + 7 stall + 6 chest)", len(METHODS))
	}
	seen := map[string]bool{}
	pickpockets, stalls, chests := 0, 0, 0
	for _, m := range METHODS {
		if seen[m.Label] {
			t.Errorf("duplicate label %q", m.Label)
		}
		seen[m.Label] = true
		if m.XP <= 0 {
			t.Errorf("%s: XP <= 0", m.Label)
		}
		if len(m.Loot) == 0 {
			t.Errorf("%s: no loot", m.Label)
		}
		switch m.Kind {
		case KindPickpocket:
			pickpockets++
			if m.NPC == "" {
				t.Errorf("%s: pickpocket with no NPC", m.Label)
			}
		case KindStall:
			stalls++
			if m.Loc == "" || m.Op == "" {
				t.Errorf("%s: stall missing loc/op", m.Label)
			}
		case KindChest:
			chests++
			if m.Loc != ChestLoc {
				t.Errorf("%s: chest loc = %q, want %q", m.Label, m.Loc, ChestLoc)
			}
		default:
			t.Errorf("%s: unknown kind %q", m.Label, m.Kind)
		}
	}
	if pickpockets != 11 || stalls != 7 || chests != 6 {
		t.Errorf("counts = pickpockets:%d stalls:%d chests:%d, want 11/7/6", pickpockets, stalls, chests)
	}
	if len(METHOD_LABELS) != len(METHODS) {
		t.Errorf("METHOD_LABELS length mismatch")
	}
}

func TestXPPerTick(t *testing.T) {
	man, _ := MethodByLabel("Man")
	if got := XPPerTick(man); got != man.XP/AttemptTicks {
		t.Errorf("XPPerTick(Man) = %v", got)
	}
	stall, _ := MethodByLabel("Baker's stall")
	if got := XPPerTick(stall); got != stall.XP/float64(stall.RespawnTicks) {
		t.Errorf("XPPerTick(stall) = %v", got)
	}
}

func TestNPCNames(t *testing.T) {
	fremennik, _ := MethodByLabel("Fremennik")
	names := NPCNames(fremennik)
	if len(names) != 10 || names[0] != "Agnar" {
		t.Errorf("NPCNames(Fremennik) = %v", names)
	}
}

func TestMethodByLabelIsForgiving(t *testing.T) {
	cases := []string{"man", "MAN", " Man ", "knight of ardougne", "Knight of Ardougne!!"}
	for _, c := range cases {
		if _, ok := MethodByLabel(c); !ok {
			t.Errorf("MethodByLabel(%q) not found", c)
		}
	}
	if _, ok := MethodByLabel("nonsense method"); ok {
		t.Error("MethodByLabel(nonsense) should not match")
	}
}

func TestParseScheduleDefault(t *testing.T) {
	s := ParseSchedule(DefaultSchedule)
	if s.FellBack {
		t.Error("the default schedule itself should not read as fellBack")
	}
	if len(s.Rungs) != 6 {
		t.Fatalf("default schedule has %d rungs, want 6", len(s.Rungs))
	}
	want := []struct {
		level int
		label string
	}{{1, "Man"}, {25, "Warrior woman"}, {40, "Guard"}, {55, "Knight of Ardougne"}, {70, "Paladin"}, {80, "Hero"}}
	for i, w := range want {
		if s.Rungs[i].Level != w.level || s.Rungs[i].Method.Label != w.label {
			t.Errorf("rung %d = %+v, want level %d %s", i, s.Rungs[i], w.level, w.label)
		}
	}
}

func TestParseScheduleAliasesAndCasing(t *testing.T) {
	s := ParseSchedule("1: men, 40 knights")
	if len(s.Rungs) != 2 {
		t.Fatalf("rungs = %+v", s.Rungs)
	}
	if s.Rungs[0].Method.Label != "Man" || s.Rungs[1].Method.Label != "Knight of Ardougne" {
		t.Errorf("alias resolution failed: %+v", s.Rungs)
	}
}

func TestParseScheduleRejectsUnknownSegments(t *testing.T) {
	s := ParseSchedule("1: Man, 40: NotARealMethod, 55: Knight of Ardougne")
	if len(s.Rejected) != 1 || s.Rejected[0] != "40: NotARealMethod" {
		t.Errorf("Rejected = %v", s.Rejected)
	}
	if len(s.Rungs) != 2 {
		t.Errorf("the rest of the schedule should still parse: %+v", s.Rungs)
	}
}

func TestParseScheduleRaisesBelowRequirement(t *testing.T) {
	s := ParseSchedule("1: Paladin")
	if len(s.Raised) != 1 {
		t.Fatalf("Raised = %v", s.Raised)
	}
	// Paladin needs level 70; asking for it at 1 should raise it, and Man should be prepended
	// for the gap at level 1.
	if s.Rungs[0].Method.Label != "Man" || s.Rungs[0].Level != 1 {
		t.Errorf("Man should be prepended: %+v", s.Rungs)
	}
	found := false
	for _, r := range s.Rungs {
		if r.Method.Label == "Paladin" && r.Level == 70 {
			found = true
		}
	}
	if !found {
		t.Errorf("Paladin should be raised to level 70: %+v", s.Rungs)
	}
}

func TestParseScheduleFallsBackWhenNothingReads(t *testing.T) {
	s := ParseSchedule("complete garbage, more garbage")
	if !s.FellBack {
		t.Error("should have fallen back")
	}
	if len(s.Rungs) != 6 {
		t.Errorf("fallback should carry the default's own rungs, got %d", len(s.Rungs))
	}
	if len(s.Rejected) != 2 {
		t.Errorf("Rejected = %v", s.Rejected)
	}
}

func TestParseScheduleLastMentionWins(t *testing.T) {
	s := ParseSchedule("1: Man, 30: Man")
	if len(s.Rungs) != 1 || s.Rungs[0].Level != 30 {
		t.Errorf("last mention should win: %+v", s.Rungs)
	}
}

func TestMethodForAndNextRung(t *testing.T) {
	s := ParseSchedule(DefaultSchedule)
	if got := MethodFor(1, s).Label; got != "Man" {
		t.Errorf("MethodFor(1) = %q", got)
	}
	if got := MethodFor(54, s).Label; got != "Guard" {
		t.Errorf("MethodFor(54) = %q", got)
	}
	if got := MethodFor(200, s).Label; got != "Hero" {
		t.Errorf("MethodFor(200) = %q", got)
	}
	if n := NextRung(1, s); n == nil || n.Method.Label != "Warrior woman" {
		t.Errorf("NextRung(1) = %+v", n)
	}
	if n := NextRung(80, s); n != nil {
		t.Errorf("NextRung(80) should be nil at the top, got %+v", n)
	}
}

func TestScheduleView(t *testing.T) {
	s := ParseSchedule(DefaultSchedule)
	rows := ScheduleView(40, s)
	if len(rows) != 6 {
		t.Fatalf("rows = %d, want 6", len(rows))
	}
	states := map[string]RungState{}
	for _, r := range rows {
		states[r.Label] = r.State
	}
	if states["Man"] != RungDone || states["Warrior woman"] != RungDone || states["Guard"] != RungNow || states["Knight of Ardougne"] != RungNext {
		t.Errorf("states = %+v", states)
	}
}

func TestAtMethod(t *testing.T) {
	man, _ := MethodByLabel("Man")
	if !AtMethod(man.Anchor, man) {
		t.Error("the anchor itself should be at the method")
	}
	far := tile.Tile{X: man.Anchor.X + man.Leash + 1, Z: man.Anchor.Z}
	if AtMethod(far, man) {
		t.Error("past the leash should not be at the method")
	}
}

func manMethod() Method {
	m, _ := MethodByLabel("Man")
	return m
}

func TestSuicideNow(t *testing.T) {
	m := manMethod()
	lb := freshstart.LumbridgeCentre
	base := ThieveSnapshot{SuicideAllowed: true, Food: 0, NotedFood: 0, Tile: &lb}
	if !SuicideNow(base, m) {
		t.Error("a fresh Man rung with no food in Lumbridge should suicide-farm")
	}
	withFood := base
	withFood.Food = 1
	if SuicideNow(withFood, m) {
		t.Error("any food should stop the suicide farm")
	}
	notAllowed := base
	notAllowed.SuicideAllowed = false
	if SuicideNow(notAllowed, m) {
		t.Error("suicide=false should stop it")
	}
	far := tile.Tile{X: lb.X + 500, Z: lb.Z + 500}
	awayFromLumbridge := base
	awayFromLumbridge.Tile = &far
	if SuicideNow(awayFromLumbridge, m) {
		t.Error("outside Lumbridge should stop it")
	}
	guard, _ := MethodByLabel("Guard")
	wrongRung := base
	if SuicideNow(wrongRung, guard) {
		t.Error("a rung whose respawn is not the spot should never suicide-farm")
	}
}

func fullHealthLumbridge() ThieveSnapshot {
	lb := freshstart.LumbridgeCentre
	sch := ParseSchedule(DefaultSchedule)
	return ThieveSnapshot{
		Tile: &lb, Level: 1, Schedule: sch, Bright: true,
		HP: 10, MaxHP: 10, EatAt: 0.5, Heal: 12,
		BankAt: 20, Coins: 0, Fare: freshstart.DefaultFare,
		SuicideAllowed: false, // isolate the phases below from the suicide branch by default
	}
}

func TestDecideThievePhaseAcceptTutorialWaitBright(t *testing.T) {
	if got := DecideThievePhase(ThieveSnapshot{MainModal: freshstart.DesignModal}); got != PhaseAccept {
		t.Errorf("accept: got %v", got)
	}
	if got := DecideThievePhase(ThieveSnapshot{}); got != PhaseWait {
		t.Errorf("wait: got %v", got)
	}
	tt := tile.Tile{X: freshstart.TutorialIsland.MinX + 1, Z: freshstart.TutorialIsland.MinZ + 1}
	if got := DecideThievePhase(ThieveSnapshot{Tile: &tt}); got != PhaseTutorial {
		t.Errorf("tutorial: got %v", got)
	}
	lb := freshstart.LumbridgeCentre
	if got := DecideThievePhase(ThieveSnapshot{Tile: &lb, Bright: false}); got != PhaseBright {
		t.Errorf("bright: got %v", got)
	}
}

func TestDecideThievePhaseSuicideBeatsEverything(t *testing.T) {
	s := fullHealthLumbridge()
	s.SuicideAllowed = true
	s.Food, s.NotedFood = 0, 0
	if got := DecideThievePhase(s); got != PhaseSuicide {
		t.Errorf("suicide: got %v", got)
	}
}

func TestDecideThievePhaseDeadAfterSuicideIsRuledOut(t *testing.T) {
	s := fullHealthLumbridge()
	s.Died = true
	s.Food = 5 // suicide farming needs zero food, so this forces the death branch instead
	if got := DecideThievePhase(s); got != PhaseDead {
		t.Errorf("dead: got %v", got)
	}
}

func TestDecideThievePhaseFundAndTravel(t *testing.T) {
	s := fullHealthLumbridge()
	s.Food = 5
	s.Level = 40 // Guard: region ardougne
	pier := freshstart.PortSarimDock
	s.Tile = &pier
	s.Coins = 0
	if got := DecideThievePhase(s); got != PhaseFund {
		t.Errorf("fund: got %v", got)
	}
	s.Coins = s.Fare
	if got := DecideThievePhase(s); got != PhaseTravel {
		t.Errorf("travel: got %v", got)
	}
}

func TestDecideThievePhaseEatBeatsBankAndWork(t *testing.T) {
	s := fullHealthLumbridge()
	s.Food = 3
	s.HP, s.MaxHP = 3, 10 // well under the 0.5 EatAt threshold
	if got := DecideThievePhase(s); got != PhaseEat {
		t.Errorf("eat: got %v", got)
	}
}

func TestDecideThievePhaseNotedFoodBanksBeforeAnythingElse(t *testing.T) {
	s := fullHealthLumbridge()
	s.Food = 0
	s.NotedFood = 5
	s.HP, s.MaxHP = 10, 10
	if got := DecideThievePhase(s); got != PhaseBank {
		t.Errorf("noted food should force a bank trip: got %v", got)
	}
}

func TestDecideThievePhaseWantsFoodBankOrderStuck(t *testing.T) {
	s := fullHealthLumbridge()
	s.Food = 0
	s.MinFood = 1
	s.HP, s.MaxHP = 10, 10 // full health, so PhaseEat never intercepts

	unread := s
	if got := DecideThievePhase(unread); got != PhaseBank {
		t.Errorf("unread bank food: got %v, want bank", got)
	}
	some := 5
	s.BankFood = &some
	if got := DecideThievePhase(s); got != PhaseBank {
		t.Errorf("bank has food: got %v, want bank", got)
	}
	none := 0
	s.BankFood = &none
	s.HasMule = false
	if got := DecideThievePhase(s); got != PhaseStuck {
		t.Errorf("no bank food, no mule: got %v, want stuck", got)
	}
	s.HasMule = true
	if got := DecideThievePhase(s); got != PhaseOrder {
		t.Errorf("no bank food, has mule: got %v, want order", got)
	}
}

func TestDecideThievePhaseBankOnLootOrPackFull(t *testing.T) {
	s := fullHealthLumbridge()
	s.Food = 5 // enough that wantsFood is false for the level-1 Man rung
	s.HP, s.MaxHP = 10, 10
	s.Loot, s.BankAt = 20, 20
	if got := DecideThievePhase(s); got != PhaseBank {
		t.Errorf("loot at threshold: got %v", got)
	}
	s.Loot = 0
	s.PackFull = true
	if got := DecideThievePhase(s); got != PhaseBank {
		t.Errorf("pack full: got %v", got)
	}
}

func TestDecideThievePhaseMoveAndWork(t *testing.T) {
	s := fullHealthLumbridge()
	s.Food = 5
	s.HP, s.MaxHP = 10, 10
	far := tile.Tile{X: freshstart.LumbridgeCentre.X + 500, Z: freshstart.LumbridgeCentre.Z}
	s.Tile = &far
	if got := DecideThievePhase(s); got != PhaseMove {
		t.Errorf("far from the method: got %v, want move", got)
	}
	here := manMethod().Anchor
	s.Tile = &here
	if got := DecideThievePhase(s); got != PhaseWork {
		t.Errorf("at the method: got %v, want work", got)
	}
}

func TestChooseTarget(t *testing.T) {
	m := manMethod()
	npcs := []freshstart.NPC{
		{Name: "Man", Tile: m.Anchor, Distance: 2, Ops: []string{PickpocketOp}},
		{Name: "Woman", Tile: m.Anchor, Distance: 1, Ops: []string{PickpocketOp}},
		{Name: "Guard", Tile: m.Anchor, Distance: 0, Ops: []string{PickpocketOp}},
	}
	if idx := ChooseTarget(npcs, NPCNames(m), m.Anchor, m.Leash); idx != 1 {
		t.Errorf("ChooseTarget = %d, want 1 (nearest Man/Woman)", idx)
	}
	if idx := ChooseTarget(nil, NPCNames(m), m.Anchor, m.Leash); idx != -1 {
		t.Errorf("ChooseTarget(nil) = %d, want -1", idx)
	}
	noOp := []freshstart.NPC{{Name: "Man", Tile: m.Anchor, Distance: 0, Ops: []string{"Talk-to"}}}
	if idx := ChooseTarget(noOp, NPCNames(m), m.Anchor, m.Leash); idx != -1 {
		t.Errorf("a target with no Pickpocket op should not match, got %d", idx)
	}
}

func TestDescribeThievePhaseCoversEveryPhase(t *testing.T) {
	all := []ThievePhase{PhaseAccept, PhaseTutorial, PhaseBright, PhaseWait, PhaseSuicide, PhaseFund, PhaseTravel, PhaseMove, PhaseBank, PhaseOrder, PhaseEat, PhaseWork, PhaseDead, PhaseStuck}
	for _, p := range all {
		if DescribeThievePhase(p) == "" || DescribeThievePhase(p) == string(p) {
			t.Errorf("DescribeThievePhase(%v) looks unhandled", p)
		}
	}
}

func TestDefaultSettingsAndNormalised(t *testing.T) {
	s := DefaultSettings()
	if s.Schedule != DefaultSchedule || s.Mule != "" || s.MuleAt != MuleCoinsAt || s.MuleKeep != "Coins" {
		t.Errorf("DefaultSettings = %+v", s)
	}
	if !s.Suicide {
		t.Error("suicide should default true")
	}

	n := Settings{Food: "  ", BankAt: 999, MinFood: 50, FoodWithdraw: 10, MuleAt: donorplan.MaxMuleAt + 1, Fare: -1}.Normalised()
	if n.Food != "Lobster" {
		t.Errorf("blank food should fall back, got %q", n.Food)
	}
	if n.BankAt != 27 {
		t.Errorf("bankAt should clamp to 27, got %d", n.BankAt)
	}
	if n.MinFood != 10 {
		t.Errorf("minFood should clamp to foodWithdraw, got %d", n.MinFood)
	}
	if n.MuleAt != donorplan.MaxMuleAt {
		t.Errorf("muleAt should clamp to MaxMuleAt, got %d", n.MuleAt)
	}
	if n.Fare != 0 {
		t.Errorf("negative fare should clamp to 0, got %d", n.Fare)
	}
}

func TestEatAtAndRestBelow(t *testing.T) {
	if got := (Settings{EatBelowHP: 0}).EatAt(); got != 0.05 {
		t.Errorf("EatAt(0) = %v", got)
	}
	if got := (Settings{EatBelowHP: 200}).EatAt(); got != 0.95 {
		t.Errorf("EatAt(200) = %v", got)
	}
	if got := (Settings{RestBelowHP: 200}).RestBelow(); got != 0.9 {
		t.Errorf("RestBelow(200) = %v", got)
	}
}

func TestDonorAndHandoffDue(t *testing.T) {
	s := DefaultSettings()
	s.Mule = "mulecon"
	d := s.Donor()
	if !d.Enabled() || d.MuleAt != MuleCoinsAt {
		t.Errorf("Donor() = %+v", d)
	}
	now := time.Now()
	if s.HandoffDue(nil, now, time.Time{}) {
		t.Error("HandoffDue(nil) should be false: the bank has not been counted yet")
	}
	under := MuleCoinsAt - 1
	if s.HandoffDue(&under, now, time.Time{}) {
		t.Error("HandoffDue() under the threshold should be false")
	}
	at := MuleCoinsAt
	if !s.HandoffDue(&at, now, time.Time{}) {
		t.Error("HandoffDue() at the threshold should be true")
	}
	if s.HandoffDue(&at, now, now.Add(time.Minute)) {
		t.Error("HandoffDue() before nextAllowed should be false")
	}
	noMule := s
	noMule.Mule = ""
	if noMule.HandoffDue(&at, now, time.Time{}) {
		t.Error("HandoffDue() with no mule configured should be false")
	}
}

func TestMuleRowsHaulAndLoad(t *testing.T) {
	keep := donorplan.ParseList("Shears")
	food := "Lobster"
	bank := []donorplan.ItemRow{
		{Name: "Shears", ID: 1735, Count: 1},
		{Name: "Coins", ID: 995, Count: 50_000},
		{Name: "Lobster", ID: 379, Count: 10},
		{Name: "Ball of wool", ID: 1759, Count: 3},
	}
	rows := MuleRows(bank, keep, food)
	if len(rows) != 2 || rows[0].Name != "Coins" || rows[1].Name != "Ball of wool" {
		t.Errorf("MuleRows should put coins first and keep the food and shears back: %+v", rows)
	}

	pack := []donorplan.ItemRow{{Name: "Coins", ID: 995, Count: 50_000}, {Name: "Ball of wool", ID: 1759, Count: 3}}
	if got := MuleHaul(pack, keep, food); got != 2 {
		t.Errorf("MuleHaul = %d", got)
	}
	load := MuleLoad(pack, 28, keep, food)
	if len(load) != 2 || load[0].Name != "Coins" {
		t.Errorf("MuleLoad = %+v", load)
	}
}

func TestProgressPassesThrough(t *testing.T) {
	if Progress(60, 300000) != Progress(60, 300000) {
		t.Error("Progress should be deterministic")
	}
}
