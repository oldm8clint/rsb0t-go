package freshstart

import (
	"reflect"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Translated from rsb0t-scripts test/freshStartPlan.test.ts. The crafter's road stands in for
// the TS module's Seers constants, which live in crafterplan here.

var (
	island     = tile.Tile{X: 3094, Z: 3107}
	lumbridge  = tile.Tile{X: 3222, Z: 3218}
	musa       = tile.Tile{X: 2956, Z: 3146}
	brimhaven  = tile.Tile{X: 2772, Z: 3234, Level: 1}
	ardougne   = tile.Tile{X: 2661, Z: 3301}
	seersBank  = tile.Tile{X: 2725, Z: 3491}
	seersField = tile.Tile{X: 2741, Z: 3444}
	portSarim  = tile.Tile{X: 3027, Z: 3218}

	crafterRoad = Road{Destination: tile.Tile{X: 2725, Z: 3493}, Radius: DestinationRadius, WoolUntil: 10}
)

func at(t tile.Tile) *tile.Tile { return &t }

func TestRegions(t *testing.T) {
	checks := []struct {
		name string
		got  bool
		want bool
	}{
		{"island", InTutorialIsland(island), true},
		{"lumbridge is not the island", InTutorialIsland(lumbridge), false},
		{"island upstairs", InTutorialIsland(tile.Tile{X: island.X, Z: island.Z, Level: 1}), false},
		// Content's own ~in_tutorial_island (tutorial/scripts/util.rs2) is the authority for
		// this box, because it is what decides whether login.rs2 runs @start_tutorial and
		// strips the account's sidebar. These four pin the two places the hand-rolled box
		// disagreed with it.
		{"content's northern rows of the island", InTutorialIsland(tile.Tile{X: 3094, Z: 3134}), true},
		{"the tutorial's underground area", InTutorialIsland(tile.Tile{X: 3095, Z: 9500}), true},
		{"the underground area upstairs is not the tutorial", InTutorialIsland(tile.Tile{X: 3095, Z: 9500, Level: 1}), false},
		{"the Varrock sewer is not the tutorial underground", InTutorialIsland(tile.Tile{X: 3150, Z: 9850}), false},
		{"lumbridge castle", InLumbridge(lumbridge), true},
		{"lumbridge store", InLumbridge(StoreStand), true},
		{"lumbridge pen", InLumbridge(tile.Tile{X: 3197, Z: 3266}), true},
		{"port sarim is not lumbridge", InLumbridge(portSarim), false},
		{"musa point", OnKaramjaSide(musa), true},
		{"brimhaven deck", OnKaramjaSide(brimhaven), true},
		{"port sarim is not karamja", OnKaramjaSide(portSarim), false},
		{"ardougne is not karamja", OnKaramjaSide(ardougne), false},
		{"seers bank", crafterRoad.Arrived(seersBank), true},
		{"seers field", crafterRoad.Arrived(seersField), true},
		{"ardougne is not seers", crafterRoad.Arrived(ardougne), false},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestTravelLegs(t *testing.T) {
	for _, here := range []tile.Tile{lumbridge, portSarim} {
		if TravelLeg(here) != LegPier {
			t.Errorf("TravelLeg(%v) = %s, want pier", here, TravelLeg(here))
		}
	}
	for _, here := range []tile.Tile{musa, brimhaven, ardougne, {X: 2683, Z: 3268, Level: 1}} {
		if TravelLeg(here) != LegDestination {
			t.Errorf("TravelLeg(%v) = %s, want destination", here, TravelLeg(here))
		}
	}
	if BrimhavenPier != (tile.Tile{X: 2772, Z: 3234, Level: 0}) {
		t.Errorf("BrimhavenPier = %v", BrimhavenPier)
	}
}

func TestRimmingtonDisplacementsStayOnMainland(t *testing.T) {
	// Live fleet positions that previously attempted an impossible walk across
	// the sea to Luthas instead of using the mainland fare/Port Sarim route.
	for _, here := range []tile.Tile{
		{X: 2948, Z: 3214}, {X: 2965, Z: 3214}, {X: 2956, Z: 3205},
		{X: 2956, Z: 3205, Level: 1},
	} {
		if OnKaramjaSide(here) || TravelLeg(here) != LegPier {
			t.Fatalf("mainland displacement %+v classified as island/second boat leg", here)
		}
	}
	for _, here := range []tile.Tile{
		{X: 2772, Z: 3235}, {X: 2810, Z: 3220}, {X: 2956, Z: 3146},
		KaramjaShipDeck,
	} {
		if !OnKaramjaSide(here) || TravelLeg(here) != LegDestination {
			t.Fatalf("real island/ship position %+v lost Karamja routing", here)
		}
	}
}

func TestDropPass(t *testing.T) {
	cases := []struct {
		balls, dropAt int
		full          bool
		wool          int
		want          bool
	}{
		{27, 27, false, 0, true},
		{10, 27, false, 0, false},
		{10, 27, true, 0, true},
		{10, 27, true, 17, false},
		{0, 27, true, 0, false},
		{1, 0, false, 0, true},
	}
	for _, c := range cases {
		if got := NeedsDropPass(c.balls, c.dropAt, c.full, c.wool); got != c.want {
			t.Errorf("NeedsDropPass(%+v) = %v", c, got)
		}
	}
}

func TestStoreTrip(t *testing.T) {
	cases := []struct {
		shears, full          bool
		wool, balls, sellable int
		want                  bool
	}{
		{false, false, 0, 0, 0, true},
		{true, false, 0, 0, 5, false},
		{true, true, 0, 0, 5, true},
		{true, true, 0, 3, 5, false},
		{true, true, 20, 0, 5, false},
		{true, true, 0, 0, 0, false},
	}
	for _, c := range cases {
		if got := NeedsStoreTrip(c.shears, c.full, c.wool, c.balls, c.sellable); got != c.want {
			t.Errorf("NeedsStoreTrip(%+v) = %v", c, got)
		}
	}
}

func TestRestHasHysteresis(t *testing.T) {
	if !ShouldRest(0.4, false) || ShouldRest(0.41, false) || !ShouldRest(0.6, true) || ShouldRest(0.9, true) {
		t.Error("default marks")
	}
	if !ShouldRestWith(0.5, false, 0.6, 0.95) {
		t.Error("custom low mark")
	}
	if !CoinsShort(59, 60) || CoinsShort(60, 60) {
		t.Error("coins short")
	}
}

func TestSellPlanTotalsByNameAndSkipsKeptAndBlank(t *testing.T) {
	pack := []PackItem{{"Ball of wool", 1}, {"Ball of wool", 1}, {"Coins", 12}, {"Shears", 1}, {"", 0}, {"Bones", 1}}
	want := []SellLine{{"Ball of wool", 2}, {"Bones", 1}}
	if got := SellPlan(pack, []string{"Coins", "Shears"}); !reflect.DeepEqual(got, want) {
		t.Errorf("SellPlan = %+v, want %+v", got, want)
	}
}

func TestDecidePhase(t *testing.T) {
	base := Snapshot{MainModal: -1, Tile: at(lumbridge), Crafting: 1, HasShears: true, Fare: 60, DropAt: 27, SkirtWorn: true, SkirtCost: 3}
	with := func(edit func(s *Snapshot)) Snapshot {
		s := base
		edit(&s)
		return s
	}
	crafting10 := func(coins int, where *tile.Tile) func(*Snapshot) {
		return func(s *Snapshot) { s.Crafting, s.Coins, s.Tile = 10, coins, where }
	}
	bare := func(edit func(s *Snapshot)) Snapshot {
		return with(func(s *Snapshot) {
			s.Crafting, s.SkirtWorn = 10, false
			edit(s)
		})
	}
	cases := []struct {
		name string
		s    Snapshot
		want Phase
	}{
		{"the appearance screen comes first", with(func(s *Snapshot) { s.MainModal, s.Tile = DesignModal, at(island) }), PhaseAccept},
		{"the island means the tutorial", with(func(s *Snapshot) { s.Tile = at(island) }), PhaseTutorial},
		{"missing shears", with(func(s *Snapshot) { s.HasShears = false }), PhaseStore},
		{"missing shears, brightness first", with(func(s *Snapshot) { s.HasShears, s.BrightPending = false, true }), PhaseBright},
		{"a pile of balls", with(func(s *Snapshot) { s.Balls = 27 }), PhaseDrop},
		{"a pile of balls beats the store", with(func(s *Snapshot) { s.Balls, s.HasShears = 27, false }), PhaseDrop},
		{"a full pack of balls", with(func(s *Snapshot) { s.PackFull, s.Balls = true, 3 }), PhaseDrop},
		{"a full pack of junk", with(func(s *Snapshot) { s.PackFull, s.Sellable = true, 4 }), PhaseStore},
		{"shearing", base, PhaseWool},
		{"a full pack of wool spins", with(func(s *Snapshot) { s.PackFull, s.Wool = true, 27 }), PhaseWool},
		{"a few balls below 10", with(func(s *Snapshot) { s.Crafting, s.Balls = 9, 5 }), PhaseWool},
		{"balls at 10 are dropped", with(func(s *Snapshot) { s.Crafting, s.Balls = 10, 3 }), PhaseDrop},
		{"wool at 10 is dropped", with(func(s *Snapshot) { s.Crafting, s.Wool = 10, 3 }), PhaseDrop},
		{"no coins at 10", with(crafting10(0, at(lumbridge))), PhaseFund},
		{"some coins at 10", with(crafting10(10, at(lumbridge))), PhaseFund},
		{"the fare held", with(crafting10(60, at(lumbridge))), PhaseTravel},
		{"karamja needs one fare", with(crafting10(30, at(musa))), PhaseTravel},
		{"karamja one short", with(crafting10(29, at(musa))), PhaseFund},
		{"ardougne needs none", with(crafting10(0, at(ardougne))), PhaseTravel},
		{"karamja with plenty", with(crafting10(60, at(musa))), PhaseTravel},
		{"seers bank", with(crafting10(0, at(seersBank))), PhaseArrived},
		{"seers field", with(func(s *Snapshot) { s.Crafting, s.Coins, s.Tile = 40, 500, at(seersField) }), PhaseArrived},
		{"skirt out of reach past the first ship", bare(func(s *Snapshot) { s.Coins, s.Tile = 30, at(musa) }), PhaseTravel},
		{"skirt out of reach in ardougne", bare(func(s *Snapshot) { s.Tile = at(ardougne) }), PhaseTravel},
		{"a held skirt is worn wherever", bare(func(s *Snapshot) { s.Coins, s.Tile, s.SkirtHeld = 30, at(musa), true }), PhaseSkirt},
		{"the fare but not the skirt", bare(func(s *Snapshot) { s.Coins = 60 }), PhaseFund},
		{"fare and skirt", bare(func(s *Snapshot) { s.Coins = 63 }), PhaseSkirt},
		{"a held skirt with the fare", bare(func(s *Snapshot) { s.Coins, s.SkirtHeld = 60, true }), PhaseSkirt},
		{"worn skirt", bare(func(s *Snapshot) { s.Coins, s.SkirtWorn = 63, true }), PhaseTravel},
		{"no skirt at seers", bare(func(s *Snapshot) { s.Tile = at(seersBank) }), PhaseArrived},
		{"an unknown tile shears", with(func(s *Snapshot) { s.Tile = nil }), PhaseWool},
		{"an unknown tile with the fare", with(crafting10(60, nil)), PhaseTravel},
	}
	for _, c := range cases {
		if got := DecidePhase(c.s, crafterRoad); got != c.want {
			t.Errorf("%s: DecidePhase = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestNoWoolLoopForARoadWithoutOne(t *testing.T) {
	s := Snapshot{MainModal: -1, Tile: at(lumbridge), Crafting: 1, Coins: 60, Fare: 60, SkirtWorn: true}
	if got := DecidePhase(s, Road{Destination: seersBank, Radius: DestinationRadius}); got != PhaseTravel {
		t.Errorf("DecidePhase = %s, want travel", got)
	}
}

func TestFareRemaining(t *testing.T) {
	cases := []struct {
		where *tile.Tile
		want  int
	}{
		{at(lumbridge), 60}, {at(portSarim), 60}, {at(musa), 30}, {at(brimhaven), 30}, {at(ardougne), 0}, {nil, 60},
	}
	for _, c := range cases {
		if got := FareRemaining(c.where, 60); got != c.want {
			t.Errorf("FareRemaining(%v) = %d, want %d", c.where, got, c.want)
		}
	}
}

func TestFundTargetIncludesTheSkirtOnlyBeforeTheBoats(t *testing.T) {
	bare := Snapshot{Tile: at(lumbridge), Crafting: 10, Fare: 60, SkirtCost: 3}
	if got := FundTarget(bare); got != 63 {
		t.Errorf("bare = %d", got)
	}
	held := bare
	held.SkirtHeld = true
	if got := FundTarget(held); got != 60 {
		t.Errorf("held = %d", got)
	}
	worn := bare
	worn.SkirtWorn = true
	if got := FundTarget(worn); got != 60 {
		t.Errorf("worn = %d", got)
	}
	karamja := bare
	karamja.Tile = at(musa)
	if got := FundTarget(karamja); got != 30 {
		t.Errorf("karamja = %d", got)
	}
}

func TestEveryPhaseHasADescription(t *testing.T) {
	for _, p := range Phases() {
		if Describe(p, "flax to bow strings") == "" {
			t.Errorf("no description for %s", p)
		}
	}
}
