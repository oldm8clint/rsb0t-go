package webwalk

import "testing"

func TestResolveDangerZonesMatchesTheTSRules(t *testing.T) {
	jail, _ := KnownDangerZoneByID("draynor-jail-guards")
	wolf, _ := KnownDangerZoneByID("white-wolf-mountain")
	wild, _ := KnownDangerZoneByID("wilderness")
	bears, _ := KnownDangerZoneByID("ardougne-seers-bears")
	lumbridge := Point{X: 3222, Z: 3218}
	inJail := Point{X: 3110, Z: 3240}
	bats, _ := KnownDangerZoneByID("seers-south-bats")
	nJail, nWild, nBears := len(jail.Rects), len(wild.Rects), len(bears.Rects)
	// Bats hunt up to combat 54, so they ride with every case below at or under it; counting
	// them into nWild keeps the expectations readable.
	nWild += len(bats.Rects)
	// The White Wolf Mountain dens apply at every combat level, so they ride along too.
	dens, _ := KnownDangerZoneByID("white-wolf-dens")
	nWild += len(dens.Rects)
	// The Rellekka and viking wolves apply at combat 64 and below, so they ride along with
	// every case here too.
	for _, id := range []string{"rellekka-road-wolves", "viking-wolves"} {
		z, _ := KnownDangerZoneByID(id)
		nWild += len(z.Rects)
	}
	// The Highwaymen (Draynor, and the two north of Seers) apply at combat 10 and below, so
	// only where combat is unknown.
	high, _ := KnownDangerZoneByID("draynor-highwayman")
	seersHigh, _ := KnownDangerZoneByID("seers-north-highwaymen")
	nHigh := len(high.Rects) + len(seersHigh.Rects)

	cases := []struct {
		name  string
		ids   []string
		ctx   ZoneContext
		rects int
	}{
		{"nothing named, no automatic", nil, ZoneContext{}, 0},
		{"unknown ids are skipped", []string{"nope"}, ZoneContext{}, 0},
		{"a named zone, twice", []string{"white-wolf-mountain", "white-wolf-mountain"}, ZoneContext{}, len(wolf.Rects)},
		{"automatic with an unknown combat level fails safe", nil, ZoneContext{IncludeAutomatic: true}, nJail + nWild + nBears + nHigh},
		{"automatic at combat 42 keeps the bears", nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 42, CombatKnown: true}, nJail + nWild + nBears},
		{"automatic at combat 50 keeps the jail but not the bears", nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 50, CombatKnown: true}, nJail + nWild},
		// The guards' huntmode is aggressive_melee with check_nottoostrong=off: no combat level is too strong for
		// them to hunt, so the jail is avoided at every level (it used to drop out above 50).
		{"automatic at combat 51 keeps the jail", nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 51, CombatKnown: true}, nJail + nWild},
		{"a destination inside exempts the jail", nil, ZoneContext{IncludeAutomatic: true, Start: &lumbridge, Dest: &inJail}, nWild + nBears + nHigh},
		{"a start inside exempts the jail", nil, ZoneContext{IncludeAutomatic: true, Start: &inJail, Dest: &lumbridge}, nWild + nBears + nHigh},
	}
	for _, c := range cases {
		got := ResolveDangerZones(c.ids, nil, c.ctx)
		if len(got) != c.rects {
			t.Errorf("%s: %d rects, want %d", c.name, len(got), c.rects)
		}
	}
	extra := []Rect{{MinX: 1, MaxX: 2, MinZ: 1, MaxZ: 2, Level: 0}}
	if got := ResolveDangerZones(nil, extra, ZoneContext{}); len(got) != 1 || got[0] != extra[0] {
		t.Errorf("ad-hoc rects = %v", got)
	}
	// ...at any level at all: the tile a combat-63 account was killed on (17 Oct 2026) is avoided at combat 126 too.
	if !InZones(Point{X: 3107, Z: 3234}, ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 126, CombatKnown: true})) {
		t.Error("the Draynor jail is not avoided at combat 126")
	}
	if !InZones(Point{X: 2850, Z: 3500, Level: 2}, wolf.Rects) {
		t.Error("white wolf mountain applies on every level")
	}
	if InZones(Point{X: 3110, Z: 3240, Level: 1}, jail.Rects) {
		t.Error("the jail rects are level 0 only")
	}
}

func TestHighwaymanZoneIsLowCombatOnly(t *testing.T) {
	spawn := Point{X: 3110, Z: 3295}
	at3 := ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 3, CombatKnown: true})
	if !InZones(spawn, at3) {
		t.Error("a combat-3 walk must avoid the Highwayman")
	}
	at11 := ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 11, CombatKnown: true})
	if InZones(spawn, at11) {
		t.Error("the Highwayman does not hunt combat 11 and must not be avoided there")
	}
	if InZones(Point{X: 3110, Z: 3264}, at3) {
		t.Error("the z=3264 road south of him must stay open")
	}
}

func TestPlannerKeepsOutOfZonesButLetsAPlayerLeaveOne(t *testing.T) {
	col := newGrid().fill(0, 0, 20, 4, 0)
	p := NewPlanner(col, nil)
	zone := []Rect{{MinX: 8, MaxX: 12, MinZ: 0, MaxZ: 3, Level: 0}}

	r := p.Plan(Point{X: 0, Z: 1}, Point{X: 20, Z: 1}, Options{AvoidZones: zone})
	if !r.OK {
		t.Fatalf("detour: %s", r.Reason)
	}
	for _, tile := range expand(stepPoints(r.Waypoints)) {
		if InZones(tile, zone) {
			t.Fatalf("route enters the zone at %s", tile)
		}
	}

	// Standing inside, the route may cross zone tiles until it is out.
	if r := p.Plan(Point{X: 10, Z: 1}, Point{X: 20, Z: 1}, Options{AvoidZones: zone}); !r.OK {
		t.Errorf("could not leave the zone: %s", r.Reason)
	}
	// A destination inside a zone the caller did not exempt is out of reach from outside.
	if r := p.Plan(Point{X: 0, Z: 1}, Point{X: 10, Z: 1}, Options{AvoidZones: zone}); r.OK {
		t.Errorf("planned into the zone: %v", r.Waypoints)
	}
	// A full-width zone with no way round blocks the walk.
	wall := []Rect{{MinX: 8, MaxX: 12, MinZ: 0, MaxZ: 4, Level: AllLevels}}
	if r := p.Plan(Point{X: 0, Z: 1}, Point{X: 20, Z: 1}, Options{AvoidZones: wall}); r.OK {
		t.Error("walked through a full-width zone")
	}
}

func TestPlannerKeepsHopsOutOfZonesAndAsksAllow(t *testing.T) {
	col := newGrid().fill(0, 0, 3, 3, 0).fill(20, 20, 23, 23, 0)
	g := &Graph{Edges: []Edge{{From: Point{X: 3, Z: 3}, To: Point{X: 20, Z: 20}, Kind: KindPortal, Cost: 20, LocName: "Lever", Action: "Pull", LocID: 1, OpenLocID: -1, LocX: 3, LocZ: 3}}}
	p := NewPlanner(col, g)
	from, to := Point{X: 0, Z: 0}, Point{X: 20, Z: 20}
	if r := p.Plan(from, to, Options{}); !r.OK {
		t.Fatalf("baseline: %s", r.Reason)
	}
	landing := []Rect{{MinX: 19, MaxX: 21, MinZ: 19, MaxZ: 21, Level: 0}}
	if r := p.Plan(from, Point{X: 23, Z: 23}, Options{AvoidZones: landing}); r.OK {
		t.Error("hopped into a zone")
	}
	asked := 0
	if r := p.Plan(from, to, Options{Allow: func(e *Edge) bool { asked++; return false }}); r.OK || asked == 0 {
		t.Errorf("Allow was not honoured: ok=%v asked=%d", r.OK, asked)
	}
}

func stepPoints(steps []Step) []Point {
	out := make([]Point, len(steps))
	for i, s := range steps {
		out[i] = s.Point
	}
	return out
}

func TestWolfDensAreAutomaticAtEveryLevelAndLeaveCatherbyOpen(t *testing.T) {
	dens, ok := KnownDangerZoneByID("white-wolf-dens")
	if !ok || !dens.Automatic || dens.AvoidAtOrBelowCombat != 0 || len(dens.Rects) == 0 {
		t.Fatalf("white-wolf-dens = %+v", dens)
	}
	got := ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 126, CombatKnown: true})
	// The foothill sentries the old rectangle missed, and the pass itself.
	for _, p := range []Point{{X: 2870, Z: 3438}, {X: 2872, Z: 3445}, {X: 2847, Z: 3474}} {
		if !InZones(p, got) {
			t.Errorf("%+v should be avoided even at combat 126", p)
		}
	}
	// Catherby's bank, fishing camp and range stay reachable.
	for _, p := range []Point{{X: 2809, Z: 3441}, {X: 2836, Z: 3431}, {X: 2853, Z: 3423}, {X: 2817, Z: 3443}} {
		if InZones(p, dens.Rects) {
			t.Errorf("%+v is Catherby and must not be a wolf den", p)
		}
	}
}

// A walk that ends at the Wilderness chaos altar (3239,3607) keeps the Wilderness north of 3647 as
// a no-go, so the Ardougne lever's landing at 3154,3924 cannot be part of its route; a walk with no
// endpoint inside keeps the whole Wilderness, and one from deep inside may still leave it.
func TestWildernessKeepsLandDeeperThanTheEndpoints(t *testing.T) {
	start := Point{X: 2655, Z: 3283}
	altar := Point{X: 3239, Z: 3607}
	lever := Point{X: 3154, Z: 3924}
	zones := ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, Start: &start, Dest: &altar})
	if InZones(altar, zones) {
		t.Error("the destination itself is avoided")
	}
	if !InZones(lever, zones) {
		t.Error("the deep Wilderness lever landing is not avoided on a walk to the altar")
	}
	if !InZones(Point{X: 3239, Z: 3648}, zones) || InZones(Point{X: 3239, Z: 3647}, zones) {
		t.Error("the cut is not 40 rows north of the altar")
	}
	if !InZones(Point{X: 3100, Z: 9950}, zones) {
		t.Error("the Wilderness dungeon, holding no endpoint, is not avoided")
	}
	home := Point{X: 3222, Z: 3218}
	zones = ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, Start: &lever, Dest: &home})
	if InZones(lever, zones) || InZones(Point{X: 3200, Z: 3700}, zones) {
		t.Error("a walk out from deep inside is fenced in")
	}
}

// errandsafe-1018: the level-based automatic zones assume a healthy account. A hurt account with nothing to eat (the caller says so with
// ZoneContext.Hurt) avoids every one of them whatever its combat level; a zone with no level condition is the same either way.
func TestHurtAppliesTheLevelBasedZonesAtEveryCombatLevel(t *testing.T) {
	bearRoad := Point{X: 2700, Z: 3335}  // the Ardougne to Seers road, bears up to combat 42
	seersBats := Point{X: 2755, Z: 3400} // the giant bats south of Seers, up to combat 54
	healthy := ZoneContext{IncludeAutomatic: true, CombatLevel: 63, CombatKnown: true}
	hurt := healthy
	hurt.Hurt = true

	if InZones(bearRoad, ResolveDangerZones(nil, nil, healthy)) || InZones(seersBats, ResolveDangerZones(nil, nil, healthy)) {
		t.Fatal("fixture: a healthy combat-63 account is over both lines, which is the premise")
	}
	got := ResolveDangerZones(nil, nil, hurt)
	if !InZones(bearRoad, got) {
		t.Error("hurt at combat 63: the bears' road is not avoided")
	}
	if !InZones(seersBats, got) {
		t.Error("hurt at combat 63: the bats south of Seers are not avoided")
	}

	// Exactly the level-limited zones come back; nothing else changes. Counted from the catalog so a zone added later is held to it too.
	want := len(ResolveDangerZones(nil, nil, healthy))
	for _, z := range KnownDangerZones {
		if z.Automatic && z.AvoidAtOrBelowCombat > 0 && z.AvoidAtOrBelowCombat < healthy.CombatLevel {
			want += len(z.Rects)
		}
	}
	if len(got) != want {
		t.Errorf("hurt at combat 63: %d rects, want the healthy %d plus the level-limited automatic zones = %d", len(got), len(ResolveDangerZones(nil, nil, healthy)), want)
	}

	// Under every line already, and with the level unknown (which fails safe), hurt changes nothing.
	for _, ctx := range []ZoneContext{{IncludeAutomatic: true, CombatLevel: 3, CombatKnown: true}, {IncludeAutomatic: true}} {
		h := ctx
		h.Hurt = true
		if a, b := len(ResolveDangerZones(nil, nil, ctx)), len(ResolveDangerZones(nil, nil, h)); a != b {
			t.Errorf("%+v: hurt changed %d rects into %d", ctx, a, b)
		}
	}
	// Hurt does not turn the automatic zones on for a walk that opted out of them.
	if n := len(ResolveDangerZones(nil, nil, ZoneContext{CombatLevel: 63, CombatKnown: true, Hurt: true})); n != 0 {
		t.Errorf("hurt with the automatic zones off resolved %d rects", n)
	}
	// An endpoint inside still exempts a zone that allows it: a hurt account can walk to the bears' road on purpose.
	dest := Point{X: 2700, Z: 3335}
	if InZones(Point{X: 2705, Z: 3340}, ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 63, CombatKnown: true, Hurt: true, Dest: &dest})) {
		t.Error("hurt: a destination inside the bears' zone no longer exempts it")
	}
}

// The Walker reads WalkOptions.Hurt into the zones a walk avoids (resolveZones is what beginWalk and Plan both use).
func TestTheWalkerAppliesHurtToItsZones(t *testing.T) {
	f, w, _ := field(t, Point{X: 0, Z: 10})
	f.combat, f.knownCmb = 63, true
	dest := Point{X: 40, Z: 10}
	plain, _ := w.resolveZones(dest, WalkOptions{})
	hurt, _ := w.resolveZones(dest, WalkOptions{Hurt: true})
	bearRoad := Point{X: 2700, Z: 3335}
	if InZones(bearRoad, plain) {
		t.Fatal("fixture: a healthy combat-63 walk avoids the bears' road")
	}
	if !InZones(bearRoad, hurt) {
		t.Error("a hurt combat-63 walk does not avoid the bears' road")
	}
}
