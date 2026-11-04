package mulereceiverplan

// Translated from rsb0t-scripts test/mulePlan.test.ts (this port's own edge cases, following the
// crafterplan/fisherplan/barbarianplan test style).

import (
	"reflect"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func heldFrom(m map[string]int) func(string) int {
	return func(rune string) int { return m[rune] }
}

func TestDescribeCosts(t *testing.T) {
	if got := DescribeCosts(nil); got != "nothing" {
		t.Errorf("DescribeCosts(nil) = %q, want nothing", got)
	}
	got := DescribeCosts([]RuneCost{{Rune: LawRune, Count: 1}, {Rune: AirRune, Count: 5}})
	if got != "1 law, 5 air" {
		t.Errorf("DescribeCosts = %q, want %q", got, "1 law, 5 air")
	}
}

func TestSumCostsMergesByNameFirstSeenOrder(t *testing.T) {
	a := []RuneCost{{Rune: LawRune, Count: 1}, {Rune: AirRune, Count: 3}}
	b := []RuneCost{{Rune: "air rune", Count: 2}, {Rune: FireRune, Count: 1}}
	got := SumCosts(a, b)
	want := []RuneCost{{Rune: LawRune, Count: 1}, {Rune: AirRune, Count: 5}, {Rune: FireRune, Count: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SumCosts = %+v, want %+v", got, want)
	}
}

func TestShortfallAndCanAfford(t *testing.T) {
	costs := []RuneCost{{Rune: LawRune, Count: 2}, {Rune: AirRune, Count: 5}}
	held := heldFrom(map[string]int{LawRune: 1, AirRune: 10})
	got := Shortfall(costs, held)
	want := []RuneCost{{Rune: LawRune, Count: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Shortfall = %+v, want %+v", got, want)
	}
	if CanAfford(costs, held) {
		t.Error("CanAfford should be false when short a law rune")
	}
	if !CanAfford(costs, heldFrom(map[string]int{LawRune: 2, AirRune: 5})) {
		t.Error("CanAfford should be true when held matches exactly")
	}
}

func TestIsWilderness(t *testing.T) {
	cases := []struct {
		t    tile.Tile
		want bool
	}{
		{tile.Tile{X: 3000, Z: 3520}, true},
		{tile.Tile{X: 2944, Z: 3600}, true},
		{tile.Tile{X: 3392, Z: 3600}, true},
		{tile.Tile{X: 2943, Z: 3600}, false},
		{tile.Tile{X: 3393, Z: 3600}, false},
		{tile.Tile{X: 3000, Z: 3519}, false},
	}
	for _, c := range cases {
		if got := IsWilderness(c.t); got != c.want {
			t.Errorf("IsWilderness(%+v) = %v, want %v", c.t, got, c.want)
		}
	}
}

func TestCastable(t *testing.T) {
	varrock, _ := TeleportByID(Varrock)
	ardougne, _ := TeleportByID(Ardougne)
	world := CasterWorld{MagicLevel: 25, QuestStatus: func(string) string { return "not_started" }}
	if !Castable(varrock, world) {
		t.Error("varrock should be castable at exactly its level")
	}
	if Castable(varrock, CasterWorld{MagicLevel: 24, QuestStatus: world.QuestStatus}) {
		t.Error("varrock should not be castable one level short")
	}
	if Castable(ardougne, CasterWorld{MagicLevel: 99, QuestStatus: func(string) string { return "not_started" }}) {
		t.Error("ardougne should refuse an incomplete quest")
	}
	if !Castable(ardougne, CasterWorld{MagicLevel: 99, QuestStatus: func(string) string { return QuestComplete }}) {
		t.Error("ardougne should allow a completed quest")
	}
}

func TestRunesForStaffProvidesFree(t *testing.T) {
	varrock, _ := TeleportByID(Varrock)
	full := RunesFor(varrock, nil)
	want := []RuneCost{{Rune: FireRune, Count: 1}, {Rune: AirRune, Count: 3}, {Rune: LawRune, Count: 1}}
	if !reflect.DeepEqual(full, want) {
		t.Fatalf("RunesFor(no staff) = %+v, want %+v", full, want)
	}
	withStaff := RunesFor(varrock, []string{"Staff of air"})
	want2 := []RuneCost{{Rune: FireRune, Count: 1}, {Rune: LawRune, Count: 1}}
	if !reflect.DeepEqual(withStaff, want2) {
		t.Fatalf("RunesFor(air staff) = %+v, want %+v (air rune dropped)", withStaff, want2)
	}
	// Case-insensitive, and a staff worn among other items.
	withStaff2 := RunesFor(varrock, []string{"Rune platebody", "STAFF OF AIR"})
	if !reflect.DeepEqual(withStaff2, want2) {
		t.Fatalf("RunesFor should match a worn staff case-insensitively among other items")
	}
}

func TestAirStavesIsAirStaffIsRuneStaff(t *testing.T) {
	staves := AirStaves()
	if len(staves) == 0 || staves[0] != "Staff of air" {
		t.Fatalf("AirStaves() = %v, want the plainest (Staff of air) first", staves)
	}
	if !IsAirStaff("staff of air") {
		t.Error("IsAirStaff should match case-insensitively")
	}
	if IsAirStaff("Staff of water") {
		t.Error("a water staff is not an air staff")
	}
	if !IsRuneStaff("Lava battlestaff") {
		t.Error("Lava battlestaff should count as a rune staff (it supplies earth and fire)")
	}
	if IsRuneStaff("Iron platebody") {
		t.Error("a non-staff item should not count")
	}
}

func TestClosestTeleportPicksNearestCastable(t *testing.T) {
	world := CasterWorld{MagicLevel: 45, QuestStatus: func(string) string { return "not_started" }, Held: heldFrom(nil)}
	camelot, _ := TeleportByID(Camelot)
	pick, ok := ClosestTeleport(camelot.Landing, world)
	if !ok || pick.Spell.ID != Camelot {
		t.Fatalf("ClosestTeleport at camelot's own landing = %+v, ok=%v, want camelot", pick, ok)
	}
	// Below every teleport's level: nothing castable at all.
	low := CasterWorld{MagicLevel: 1, QuestStatus: world.QuestStatus, Held: world.Held}
	if _, ok := ClosestTeleport(camelot.Landing, low); ok {
		t.Error("ClosestTeleport should find nothing castable at magic 1")
	}
}

func TestClosestTeleportTieBreaksOnLowerLevel(t *testing.T) {
	// Falador (37) and Camelot (45) are both reachable; a target exactly between their landings
	// ties on distance, so the lower-level spell (falador) should win.
	falador, _ := TeleportByID(Falador)
	camelot, _ := TeleportByID(Camelot)
	mid := tile.Tile{X: (falador.Landing.X + camelot.Landing.X) / 2, Z: (falador.Landing.Z + camelot.Landing.Z) / 2}
	// Nudge to force an exact tie in Chebyshev distance from both landings.
	dFal := tile.Chebyshev(falador.Landing, mid)
	dCam := tile.Chebyshev(camelot.Landing, mid)
	if dFal != dCam {
		t.Skipf("midpoint tile does not tie (fal=%d cam=%d); geometry changed", dFal, dCam)
	}
	world := CasterWorld{MagicLevel: 45, QuestStatus: func(string) string { return "not_started" }}
	pick, ok := ClosestTeleport(mid, world)
	if !ok || pick.Spell.ID != Falador {
		t.Fatalf("ClosestTeleport tie should prefer the lower level spell, got %+v", pick)
	}
}

func TestPickTeleportRequiresAffordingBothLegs(t *testing.T) {
	world := CasterWorld{
		MagicLevel:  45,
		QuestStatus: func(string) string { return "not_started" },
		Held:        heldFrom(map[string]int{LawRune: 1, AirRune: 3}),
	}
	falador, _ := TeleportByID(Falador)
	homeCost := []RuneCost{{Rune: LawRune, Count: 1}, {Rune: AirRune, Count: 3}}
	// Not enough runes for both legs (falador needs its own air+law+water on top of home's).
	if _, ok := PickTeleport(falador.Landing, world, homeCost); ok {
		t.Error("PickTeleport should refuse a target it cannot afford alongside the trip home")
	}
	rich := CasterWorld{MagicLevel: 45, QuestStatus: world.QuestStatus, Held: heldFrom(map[string]int{LawRune: 2, AirRune: 8, WaterRune: 1})}
	pick, ok := PickTeleport(falador.Landing, rich, homeCost)
	if !ok || pick.Spell.ID != Falador {
		t.Fatalf("PickTeleport should afford falador+home now, got %+v ok=%v", pick, ok)
	}
}

func richWorld(magic int) CasterWorld {
	return CasterWorld{
		MagicLevel:  magic,
		QuestStatus: func(string) string { return QuestComplete },
		Held:        heldFrom(map[string]int{LawRune: 1000, AirRune: 1000, FireRune: 1000, WaterRune: 1000, EarthRune: 1000}),
	}
}

func TestPlanTripDeclinesWilderness(t *testing.T) {
	got := PlanTrip(PlanTripInput{Me: SeersStand, Home: SeersStand, Target: tile.Tile{X: 3100, Z: 3600}, NearRadius: 50, MaxLegTiles: 150, World: richWorld(99)})
	if got.Mode != TripDecline || got.Reason == "" {
		t.Fatalf("PlanTrip into the wilderness = %+v, want decline", got)
	}
}

func TestPlanTripWalksWhenNear(t *testing.T) {
	target := tile.Tile{X: SeersStand.X + 10, Z: SeersStand.Z}
	got := PlanTrip(PlanTripInput{Me: SeersStand, Home: SeersStand, Target: target, NearRadius: 50, MaxLegTiles: 150, World: richWorld(99)})
	if got.Mode != TripWalk || got.Tiles != 10 {
		t.Fatalf("PlanTrip nearby = %+v, want a 10-tile walk", got)
	}
}

func TestPlanTripDeclinesWithNoCastableTeleportHome(t *testing.T) {
	far := tile.Tile{X: SeersStand.X + 200, Z: SeersStand.Z}
	world := CasterWorld{MagicLevel: 1, QuestStatus: func(string) string { return "not_started" }, Held: heldFrom(nil)}
	got := PlanTrip(PlanTripInput{Me: SeersStand, Home: SeersStand, Target: far, NearRadius: 50, MaxLegTiles: 150, World: world})
	if got.Mode != TripDecline {
		t.Fatalf("PlanTrip with no castable teleport home = %+v, want decline", got)
	}
}

func TestPlanTripDeclinesWhenHomeLegOverCap(t *testing.T) {
	// Home itself is far from every teleport landing, so the trip home alone exceeds the cap.
	home := tile.Tile{X: 100, Z: 100}
	target := tile.Tile{X: home.X + 200, Z: home.Z}
	got := PlanTrip(PlanTripInput{Me: home, Home: home, Target: target, NearRadius: 10, MaxLegTiles: 50, World: richWorld(99)})
	if got.Mode != TripDecline {
		t.Fatalf("PlanTrip with the home leg over cap = %+v, want decline", got)
	}
}

func TestPlanTripDeclinesShortOfRunes(t *testing.T) {
	falador, _ := TeleportByID(Falador)
	world := CasterWorld{MagicLevel: 45, QuestStatus: func(string) string { return "not_started" }, Held: heldFrom(nil)}
	target := tile.Tile{X: falador.Landing.X + 5, Z: falador.Landing.Z}
	got := PlanTrip(PlanTripInput{Me: SeersStand, Home: SeersStand, Target: target, NearRadius: 5, MaxLegTiles: 150, World: world})
	if got.Mode != TripDecline {
		t.Fatalf("PlanTrip with no runes at all = %+v, want decline", got)
	}
}

func TestPlanTripWalksWhenNoLandingIsCloser(t *testing.T) {
	// West of Seers, away from Camelot (the landing nearest Seers, 32 tiles off): every teleport
	// landing ends up farther from this target than a direct walk from Seers is, so no teleport
	// buys anything. Past NearRadius so the "nearby" shortcut does not fire first.
	target := tile.Tile{X: SeersStand.X - 70, Z: SeersStand.Z}
	got := PlanTrip(PlanTripInput{Me: SeersStand, Home: SeersStand, Target: target, NearRadius: 50, MaxLegTiles: 150, World: richWorld(99)})
	if got.Mode != TripWalk || got.Note != "no teleport lands closer" {
		t.Fatalf("PlanTrip = %+v, want a walk with note 'no teleport lands closer'", got)
	}
}

func TestPlanTripTeleportsWhenALandingIsCloser(t *testing.T) {
	falador, _ := TeleportByID(Falador)
	target := tile.Tile{X: falador.Landing.X + 3, Z: falador.Landing.Z}
	got := PlanTrip(PlanTripInput{Me: SeersStand, Home: SeersStand, Target: target, NearRadius: 5, MaxLegTiles: 150, World: richWorld(99)})
	if got.Mode != TripTeleport || got.Spell.ID != Falador {
		t.Fatalf("PlanTrip = %+v, want a falador teleport", got)
	}
	if len(got.HomeCost) == 0 {
		t.Error("a teleport trip plan should carry the cost of the return leg too")
	}
}

func TestPlanReturnWalksWhenNear(t *testing.T) {
	got := PlanReturn(PlanReturnInput{Me: tile.Tile{X: SeersStand.X + 5, Z: SeersStand.Z}, Home: SeersStand, NearRadius: 50, World: richWorld(99)})
	if got.Mode != ReturnWalk {
		t.Fatalf("PlanReturn nearby = %+v, want a walk", got)
	}
}

func TestPlanReturnFallsBackToWalkWithoutRunes(t *testing.T) {
	far := tile.Tile{X: SeersStand.X + 500, Z: SeersStand.Z}
	world := CasterWorld{MagicLevel: 45, QuestStatus: func(string) string { return "not_started" }, Held: heldFrom(nil)}
	got := PlanReturn(PlanReturnInput{Me: far, Home: SeersStand, NearRadius: 10, World: world})
	if got.Mode != ReturnWalkFallback {
		t.Fatalf("PlanReturn short of runes = %+v, want walk-fallback (home is mandatory, never a refusal)", got)
	}
}

func TestPlanReturnTeleportsWhenAffordable(t *testing.T) {
	far := tile.Tile{X: SeersStand.X + 500, Z: SeersStand.Z}
	got := PlanReturn(PlanReturnInput{Me: far, Home: SeersStand, NearRadius: 10, World: richWorld(99)})
	if got.Mode != ReturnTeleport {
		t.Fatalf("PlanReturn with runes = %+v, want a teleport", got)
	}
}

func TestPlanReturnWalksWhenNoLandingCloserThanHere(t *testing.T) {
	// Standing right on a landing tile already: no teleport gets any closer to home.
	camelot, _ := TeleportByID(Camelot)
	got := PlanReturn(PlanReturnInput{Me: camelot.Landing, Home: camelot.Landing, NearRadius: 0, World: richWorld(99)})
	if got.Mode != ReturnWalk {
		t.Fatalf("PlanReturn standing at home's own landing = %+v, want a walk", got)
	}
}

func TestArrivedAt(t *testing.T) {
	landing := tile.Tile{X: 100, Z: 100, Level: 0}
	near := tile.Tile{X: 104, Z: 100, Level: 0}
	if !ArrivedAt(&near, landing, DefaultLandingRadius) {
		t.Error("ArrivedAt should be true within the default radius")
	}
	far := tile.Tile{X: 200, Z: 100, Level: 0}
	if ArrivedAt(&far, landing, DefaultLandingRadius) {
		t.Error("ArrivedAt should be false far from the landing")
	}
	wrongLevel := tile.Tile{X: 100, Z: 100, Level: 1}
	if ArrivedAt(&wrongLevel, landing, DefaultLandingRadius) {
		t.Error("ArrivedAt should be false on a different plane even at the same x,z")
	}
	if ArrivedAt(nil, landing, DefaultLandingRadius) {
		t.Error("ArrivedAt should be false when the position is not known yet")
	}
}

func TestLegTimeoutClampsAndScales(t *testing.T) {
	if got := LegTimeout(-5); got != legMin {
		t.Errorf("LegTimeout(-5) = %v, want the floor %v (negative tiles treated as 0)", got, legMin)
	}
	if got := LegTimeout(0); got != legMin {
		t.Errorf("LegTimeout(0) = %v, want the floor %v", got, legMin)
	}
	if got := LegTimeout(10000); got != legMax {
		t.Errorf("LegTimeout(10000) = %v, want the ceiling %v", got, legMax)
	}
	// Between the floor and the ceiling it scales with distance.
	short, long := LegTimeout(5), LegTimeout(50)
	if !(short < long) {
		t.Errorf("LegTimeout should grow with tiles: LegTimeout(5)=%v LegTimeout(50)=%v", short, long)
	}
}

func TestKitFromSettingsDropsZeroAndNegative(t *testing.T) {
	got := KitFromSettings(RuneCounts{Law: 10, Air: 0, Fire: -5, Water: 5, Earth: 5})
	want := []RuneCost{{Rune: LawRune, Count: 10}, {Rune: WaterRune, Count: 5}, {Rune: EarthRune, Count: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("KitFromSettings = %+v, want %+v", got, want)
	}
}

func TestKitAfterStaffRemovesFreeRunes(t *testing.T) {
	kit := KitFromSettings(RuneCounts{Law: 10, Air: 40})
	got := KitAfterStaff(kit, []string{"Staff of air"})
	want := []RuneCost{{Rune: LawRune, Count: 10}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("KitAfterStaff = %+v, want %+v (air dropped)", got, want)
	}
	if got2 := KitAfterStaff(kit, nil); !reflect.DeepEqual(got2, kit) {
		t.Fatalf("KitAfterStaff with nothing worn should return the kit unchanged")
	}
}

func TestKitNames(t *testing.T) {
	kit := []RuneCost{{Rune: LawRune, Count: 10}, {Rune: AirRune, Count: 0}, {Rune: FireRune, Count: 5}}
	got := KitNames(kit)
	want := []string{LawRune, FireRune}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("KitNames = %v, want %v (a zero-count rune drops out)", got, want)
	}
}

func TestKitShortfallCappedByBank(t *testing.T) {
	kit := []RuneCost{{Rune: LawRune, Count: 10}, {Rune: AirRune, Count: 40}}
	held := heldFrom(map[string]int{LawRune: 2, AirRune: 40})
	bank := heldFrom(map[string]int{LawRune: 5, AirRune: 1000})
	got := KitShortfall(kit, held, bank)
	want := []KitWithdrawal{{Rune: LawRune, Withdraw: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("KitShortfall = %+v, want %+v (missing 8 laws, bank only has 5; air is already full)", got, want)
	}
}

func TestKitExcess(t *testing.T) {
	kit := []RuneCost{{Rune: LawRune, Count: 10}, {Rune: AirRune, Count: 40}}
	held := heldFrom(map[string]int{LawRune: 50, AirRune: 40})
	got := KitExcess(kit, held)
	want := []string{LawRune}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("KitExcess = %v, want %v", got, want)
	}
}

func TestHaulSlots(t *testing.T) {
	kit := KitFromSettings(RuneCounts{Law: 10, Air: 40})
	names := []string{"Law rune", "Air rune", "Coins", "Shark"}
	if got := HaulSlots(names, kit); got != 2 {
		t.Fatalf("HaulSlots = %d, want 2 (coins and shark, kit runes spared)", got)
	}
	if got := HaulSlots(nil, kit); got != 0 {
		t.Fatalf("HaulSlots(nil) = %d, want 0", got)
	}
}

func TestParsePostTile(t *testing.T) {
	cases := []struct {
		raw  string
		want tile.Tile
		ok   bool
	}{
		{"3200,3200", tile.Tile{X: 3200, Z: 3200}, true},
		{" 3200 , 3200 , 1 ", tile.Tile{X: 3200, Z: 3200, Level: 1}, true},
		{"3200.9,3200", tile.Tile{X: 3200, Z: 3200}, true}, // floors, does not round
		{"", tile.Tile{}, false},
		{"3200", tile.Tile{}, false},
		{"3200,3200,1,1", tile.Tile{}, false},
		{"abc,3200", tile.Tile{}, false},
	}
	for _, c := range cases {
		got, ok := ParsePostTile(c.raw)
		if ok != c.ok {
			t.Errorf("ParsePostTile(%q) ok = %v, want %v", c.raw, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("ParsePostTile(%q) = %+v, want %+v", c.raw, got, c.want)
		}
	}
}

func TestResolvePlaceCoordsFirst(t *testing.T) {
	banks := []BankPlace{{Name: "Varrock West", Tile: tile.Tile{X: 3183, Z: 3436}}}
	place, ok := ResolvePlace("100,200", banks)
	if !ok || place.Tile != (tile.Tile{X: 100, Z: 200}) {
		t.Fatalf("ResolvePlace coords = %+v ok=%v", place, ok)
	}
}

func TestResolvePlaceExactBankMatch(t *testing.T) {
	banks := []BankPlace{
		{Name: "Varrock West", Tile: tile.Tile{X: 3183, Z: 3436}},
		{Name: "Seers' Village Bank", Tile: SeersStand},
	}
	place, ok := ResolvePlace("seers village", banks)
	if !ok || place.Name != "Seers' Village Bank" {
		t.Fatalf("ResolvePlace(%q) = %+v ok=%v, want an exact match on the Seers bank (stop words stripped)", "seers village", place, ok)
	}
}

func TestResolvePlaceTeleportTownBeforePrefixMatch(t *testing.T) {
	// Two Varrock banks exist, but the bare town name should resolve to the teleport landing, not
	// guess between them.
	banks := []BankPlace{
		{Name: "Varrock West", Tile: tile.Tile{X: 3183, Z: 3436}},
		{Name: "Varrock East", Tile: tile.Tile{X: 3254, Z: 3420}},
	}
	place, ok := ResolvePlace("varrock", banks)
	varrock, _ := TeleportByID(Varrock)
	if !ok || place.Tile != varrock.Landing {
		t.Fatalf("ResolvePlace(varrock) = %+v ok=%v, want the teleport landing (ambiguous between two banks)", place, ok)
	}
}

func TestResolvePlaceUniquePrefixMatch(t *testing.T) {
	banks := []BankPlace{{Name: "Shilo Village", Tile: tile.Tile{X: 2853, Z: 2954}}}
	place, ok := ResolvePlace("shilo", banks)
	if !ok || place.Name != "Shilo Village" {
		t.Fatalf("ResolvePlace(shilo) = %+v ok=%v, want the unique prefix match", place, ok)
	}
}

func TestResolvePlaceAmbiguousPrefixFails(t *testing.T) {
	banks := []BankPlace{
		{Name: "Varrock West", Tile: tile.Tile{X: 3183, Z: 3436}},
		{Name: "Varrock East", Tile: tile.Tile{X: 3254, Z: 3420}},
	}
	if _, ok := ResolvePlace("varrock w", banks); !ok {
		t.Error("ResolvePlace(varrock w) should match Varrock West uniquely")
	}
	if _, ok := ResolvePlace("nowhere", banks); ok {
		t.Error("ResolvePlace(nowhere) should fail: no bank, no teleport, no prefix")
	}
}

// Coverage that Teleports/TeleportByID and the staff table stay internally consistent — a typo in
// either would silently break trip planning without ever failing a build.
func TestTeleportByIDAndTableConsistency(t *testing.T) {
	for _, want := range Teleports {
		got, ok := TeleportByID(want.ID)
		if !ok || got.ID != want.ID {
			t.Errorf("TeleportByID(%q) = %+v, ok=%v", want.ID, got, ok)
		}
	}
	if _, ok := TeleportByID("nonexistent"); ok {
		t.Error("TeleportByID should report ok=false for an unknown id")
	}
	ids := map[TeleportID]bool{}
	for _, sp := range Teleports {
		if ids[sp.ID] {
			t.Errorf("duplicate teleport id %q", sp.ID)
		}
		ids[sp.ID] = true
	}
}

func TestKitRunesOrder(t *testing.T) {
	got := KitRunes()
	want := []string{LawRune, AirRune, FireRune, WaterRune, EarthRune}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("KitRunes() = %v, want %v", got, want)
	}
}
