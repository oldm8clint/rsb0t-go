package webwalk

import (
	"fmt"
	"testing"

	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// hunterSpawn is one hunting NPC spawn near Seers, as content has it: the map file's NPC
// section for the tile, scripts/_unpack/225/all.npc for the rest (no area config overrides
// any of these names). maxrange is NpcType's default, wanderrange + 2, since none of these
// configs sets one.
type hunterSpawn struct {
	name                             string
	x, z                             int
	vislevel, wander, maxrange, hunt int
	size                             int
	cowardly                         bool // huntmode cowardly (else ranged); both test combat <= 2 x vislevel
}

// reach is how far from its spawn the npc can notice a player: it hunts from its own tile
// (HuntIterator, distanceToSW from the npc's south-west tile) and that tile can be up to
// maxrange from the spawn while it chases. The owner's brief asked for wander + hunt + size,
// which is never larger for these spawns; both are checked.
func (h hunterSpawn) reach() int {
	return max(h.maxrange+h.hunt, h.wander+h.hunt+h.size)
}

// seersHunters is the two groups agent "wcrung" found in no zone on 2026-09-23. Tiles from
// maps m42_55 (NPC local 47,1 and 61,1) and m44_53/m44_54, cross-checked against a scan of
// every content map's NPC section joined to pack/npc.pack and the .npc configs.
var seersHunters = []hunterSpawn{
	{"highwayman", 2735, 3521, 5, 8, 10, 5, 1, false},
	{"highwayman", 2749, 3521, 5, 8, 10, 5, 1, false},
	// White Wolf Mountain's southern foot and the sentries above it.
	{"whitewolf_sentry", 2842, 3450, 25, 3, 5, 2, 2, true},
	{"whitewolf_sentry", 2846, 3477, 25, 3, 5, 2, 2, true},
	{"whitewolf_sentry", 2847, 3474, 25, 3, 5, 2, 2, true},
	{"whitewolf_sentry", 2864, 3453, 25, 3, 5, 2, 2, true},
	{"whitewolf_sentry", 2870, 3438, 25, 3, 5, 2, 2, true},
	{"whitewolf_sentry", 2872, 3445, 25, 3, 5, 2, 2, true},
	{"whitewolf", 2848, 3487, 38, 3, 5, 2, 2, true},
	{"whitewolf", 2850, 3484, 38, 3, 5, 2, 2, true},
}

// TestSeersHighwaymenZoneCoversTheirReach pins the new zone to content and to the engine's
// hunt rules: it applies at combat 10 and below (not-too-strong is combat <= 2 x vislevel 5),
// covers every tile either Highwayman can notice a player from, lets a walk that starts or
// ends inside go, and leaves the Draynor rectangle exactly where it was.
func TestSeersHighwaymenZoneCoversTheirReach(t *testing.T) {
	z, ok := KnownDangerZoneByID("seers-north-highwaymen")
	if !ok {
		t.Fatal("no seers-north-highwaymen zone")
	}
	if !z.Automatic || !z.AllowWhenEndpointInside || z.AvoidAtOrBelowCombat != 10 || z.FromTS {
		t.Fatalf("seers-north-highwaymen = %+v", z)
	}
	want := []Rect{{MinX: 2720, MaxX: 2750, MinZ: 3506, MaxZ: 3536, Level: 0}, {MinX: 2734, MaxX: 2764, MinZ: 3506, MaxZ: 3536, Level: 0}}
	if fmt.Sprint(z.Rects) != fmt.Sprint(want) {
		t.Errorf("rects = %v, want %v", z.Rects, want)
	}
	draynor, _ := KnownDangerZoneByID("draynor-highwayman")
	if fmt.Sprint(draynor.Rects) != fmt.Sprint([]Rect{{MinX: 3095, MaxX: 3125, MinZ: 3280, MaxZ: 3310, Level: 0}}) || draynor.AvoidAtOrBelowCombat != 10 {
		t.Errorf("the Draynor zone moved: %+v", draynor)
	}

	for _, h := range seersHunters[:2] {
		if got := 2 * h.vislevel; got != z.AvoidAtOrBelowCombat {
			t.Errorf("%s: 2 x vislevel = %d, zone applies at or below %d", h.name, got, z.AvoidAtOrBelowCombat)
		}
		r := h.reach()
		for x := h.x - r; x <= h.x+r; x++ {
			for zz := h.z - r; zz <= h.z+r; zz++ {
				if !InZones(Point{X: x, Z: zz}, z.Rects) {
					t.Fatalf("%s at %d,%d: %d,%d is within its reach %d but outside the zone", h.name, h.x, h.z, x, zz, r)
				}
			}
		}
	}

	spawn := Point{X: 2735, Z: 3521}
	seers := Point{X: 2725, Z: 3493}
	north := Point{X: 2731, Z: 3543}
	for _, c := range []struct {
		combat int
		avoid  bool
	}{{3, true}, {10, true}, {11, false}, {126, false}} {
		got := ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: c.combat, CombatKnown: true, Start: &seers, Dest: &north})
		if InZones(spawn, got) != c.avoid {
			t.Errorf("combat %d: avoided %v, want %v", c.combat, !c.avoid, c.avoid)
		}
	}
	// A destination inside (the river fishing spot 2727,3524) or a start inside drops the zone.
	river := Point{X: 2727, Z: 3524}
	if InZones(spawn, ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 3, CombatKnown: true, Start: &seers, Dest: &river})) {
		t.Error("a walk to a tile inside the zone still avoids it")
	}
	if InZones(spawn, ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 3, CombatKnown: true, Start: &river, Dest: &seers})) {
		t.Error("a walk out of the zone still avoids it")
	}
}

// TestSeersHuntersAreAllAvoidedAtCombat3 is the brief's two groups against every automatic
// zone a combat-3 walk resolves. The Highwaymen are the new zone. The White Wolf Mountain
// foot was already covered, by "white-wolf-dens" (wolfdens.go, automatic at every level since
// 2026-09-19), whose squares are exactly each spawn's reach (maxrange 5 + huntrange 2 = 7 =
// wanderrange 3 + huntrange 2 + size 2): the "white-wolf-mountain" rectangle the brief
// measured against (z 3468 and up) is the TS catalog's named zone, which no default walk uses.
func TestSeersHuntersAreAllAvoidedAtCombat3(t *testing.T) {
	far := Point{X: 3222, Z: 3218} // endpoints outside every zone, so none is exempted
	zones := ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 3, CombatKnown: true, Start: &far, Dest: &far})
	for _, h := range seersHunters {
		r := h.reach()
		for x := h.x - r; x <= h.x+r; x++ {
			for z := h.z - r; z <= h.z+r; z++ {
				if !InZones(Point{X: x, Z: z}, zones) {
					t.Fatalf("%s at %d,%d: %d,%d is within its reach %d but no combat-3 zone covers it", h.name, h.x, h.z, x, z, r)
				}
			}
		}
		if h.cowardly {
			// White Wolf Mountain stays avoided at every level, above 2 x vislevel too.
			top := ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 126, CombatKnown: true, Start: &far, Dest: &far})
			if !InZones(Point{X: h.x, Z: h.z}, top) {
				t.Errorf("%s at %d,%d is no longer avoided at combat 126", h.name, h.x, h.z)
			}
		}
	}
}

// fleetPlaces are the tiles the Seers and Catherby scripts stand on and walk between, from the
// script packages themselves wherever they export them.
func fleetPlaces() map[string]tile.Tile {
	c, g := fisherplan.Catherby(), fisherplan.FishingGuild()
	places := map[string]tile.Tile{
		"Catherby bank":          c.Bank,
		"Catherby camp":          c.Camp,
		"Harry":                  c.HarryStand,
		"Arhein":                 c.ArheinStand,
		"Catherby range":         mulereceiverplan.CatherbyRange,
		"Catherby camp east end": {X: 2865, Z: 3428}, // the measured east end of the fishing camp
		"Guild bank":             g.Bank,
		"Guild camp":             g.Camp,
		"Seers bank":             woodcutplan.Seers,
		"Seers mule stand":       mulereceiverplan.SeersStand,
		"Seers knife":            woodcutplan.SeersKnife,
		"Camelot teleport":       {X: 2757, Z: 3478},
		"crafter flax field":     {X: 2741, Z: 3444},
		"crafter wheelhouse":     {X: 2711, Z: 3471, Level: 1},
		// The members fail-teleports near here (content macro_events_teleports.enum): an
		// account dropped at either must still walk home.
		"fail-teleport Camelot topiary":      {X: 2787, Z: 3515},
		"fail-teleport Seers spinning wheel": {X: 2708, Z: 3472, Level: 1},
		"wcfletch south field":               {X: 2718, Z: 3442}, // claude/wcrung's NormalPatches, not on main yet
		"wcfletch Catherby road":             {X: 2765, Z: 3463},
		"wcfletch Ranging Guild E":           {X: 2692, Z: 3442},
		"wcfletch Catherby west":             {X: 2776, Z: 3442},
		"wcfletch south-west":                {X: 2699, Z: 3426},
		"wcfletch Catherby north":            {X: 2792, Z: 3459},
		"wcfletch Seers dead trees":          {X: 2709, Z: 3462},
		"wcfletch Ranging Guild":             {X: 2675, Z: 3445},
		"wcfletch Seers south maples":        {X: 2724, Z: 3473}, // wcfletchplan.MapleCamps
		"wcfletch Camelot maples":            {X: 2748, Z: 3466},
		"wcfletch north-west field":          {X: 2654, Z: 3525},
		"wcfletch north-west field east":     {X: 2662, Z: 3532},
		"wcfletch Catherby willows":          {X: 2777, Z: 3436},
		"wcfletch Catherby yews":             {X: 2762, Z: 3430}, // wcfletchplan.YewCamps (the church camp is "woodcut Yew")
		"wcfletch Catherby road yew":         {X: 2767, Z: 3451},
	}
	for _, tier := range woodcutplan.TIERS {
		places["woodcut "+tier.Label] = tier.Anchor
	}
	return places
}

// catherbySpots is content's fishing_movement_catherby_enum (skill_fishing/configs/
// fishing_movement.enum): every tile a Catherby fishing spot moves between, mapsquare 44,53.
var catherbySpots = []Point{
	{X: 2836, Z: 3431}, {X: 2837, Z: 3431}, {X: 2838, Z: 3431},
	{X: 2844, Z: 3429}, {X: 2845, Z: 3429}, {X: 2846, Z: 3429},
	{X: 2853, Z: 3423}, {X: 2854, Z: 3423}, {X: 2855, Z: 3423},
	{X: 2859, Z: 3426}, {X: 2860, Z: 3426},
}

func pointOf(t tile.Tile) Point { return Point{X: t.X, Z: t.Z, Level: t.Level} }

// zonesWithoutIDs is zonesWithout for several catalog entries at once.
func zonesWithoutIDs(ids []string, from, to Point, combat int) []Rect {
	saved := KnownDangerZones
	defer func() { KnownDangerZones = saved }()
	drop := map[string]bool{}
	for _, id := range ids {
		drop[id] = true
	}
	kept := make([]DangerZone, 0, len(saved))
	for _, z := range saved {
		if !drop[z.ID] {
			kept = append(kept, z)
		}
	}
	KnownDangerZones = kept
	return zonesWithout("", from, to, combat)
}

// zonesWithout resolves a combat-3 walk's automatic zones with one catalog entry left out:
// the walk exactly as it planned before that entry existed.
func zonesWithout(id string, from, to Point, combat int) []Rect {
	ctx := ZoneContext{IncludeAutomatic: true, CombatLevel: combat, CombatKnown: true, Start: &from, Dest: &to}
	saved := KnownDangerZones
	defer func() { KnownDangerZones = saved }()
	if id != "" {
		kept := make([]DangerZone, 0, len(saved))
		for _, z := range saved {
			if z.ID != id {
				kept = append(kept, z)
			}
		}
		KnownDangerZones = kept
	}
	return ResolveDangerZones(nil, nil, ctx)
}

// closestTo is the route's nearest approach (Chebyshev, surface tiles) to any of spawns.
func closestTo(r Route, spawns []Point) (int, Point) {
	best, at := 1<<30, Point{}
	pts := make([]Point, 0, len(r.Waypoints))
	for _, w := range r.Waypoints {
		pts = append(pts, w.Point)
	}
	for _, p := range expand(pts) {
		for _, s := range spawns {
			if d := Chebyshev(p, s); p.Level == 0 && d < best {
				best, at = d, p
			}
		}
	}
	return best, at
}

var seersHighwaymen = []Point{{X: 2735, Z: 3521}, {X: 2749, Z: 3521}}

// TestSeersHighwaymenLeaveTheFleetsRoutesAlone plans every leg the Catherby fishers, the
// Seers woodcutters and the mule walk, both ways, for a combat-3 account, with and without
// the new zone, and requires the same outcome and cost: no fishing spot, stand tile, bank,
// shop, range, mule post or camp is newly blocked or detoured. The legs: bank and camp to
// every tile beside every Catherby spot, bank to every Catherby place, Seers bank to every
// woodcutting anchor and wcfletch patch, the Seers-Catherby road, the Fishing Guild route,
// the Barbarian Outpost and the Ardougne ship's walk to Catherby.
//
//	RSB0T_COLLISION_PACK=<collision.lcnav.gz> go test -run TestSeersHighwaymenLeaveTheFleetsRoutesAlone ./internal/webwalk/
func TestSeersHighwaymenLeaveTheFleetsRoutesAlone(t *testing.T) {
	p := realPack(t)
	places := fleetPlaces()
	type leg struct {
		name     string
		from, to Point
	}
	var legs []leg
	bank, camp := pointOf(places["Catherby bank"]), pointOf(places["Catherby camp"])
	for _, s := range catherbySpots {
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				st := Point{X: s.X + dx, Z: s.Z + dz}
				if (dx == 0 && dz == 0) || !p.col.Walkable(st.X, st.Z, 0) {
					continue
				}
				legs = append(legs, leg{fmt.Sprintf("Catherby bank -> stand %s beside spot %s", st, s), bank, st})
				legs = append(legs, leg{fmt.Sprintf("Catherby camp -> stand %s beside spot %s", st, s), camp, st})
			}
		}
	}
	for name, pl := range places {
		if name != "Catherby bank" {
			legs = append(legs, leg{"Catherby bank -> " + name, bank, pointOf(pl)})
		}
		if name != "Seers bank" {
			legs = append(legs, leg{"Seers bank -> " + name, pointOf(places["Seers bank"]), pointOf(pl)})
		}
	}
	legs = append(legs,
		leg{"Seers mule stand -> Barbarian Outpost pipe", pointOf(places["Seers mule stand"]), Point{X: 2552, Z: 3561}},
		leg{"Ardougne ship deck -> Catherby bank", Point{X: 2683, Z: 3268, Level: 1}, bank},
		leg{"Guild bank -> Guild camp", pointOf(places["Guild bank"]), pointOf(places["Guild camp"])},
	)

	changed, planned := 0, 0
	for _, l := range legs {
		for dir := 0; dir < 2; dir++ {
			from, to, name := l.from, l.to, l.name
			if dir == 1 {
				from, to, name = l.to, l.from, l.name+" (back)"
			}
			opt := Options{AllowNPC: true, AllowSpecial: true}
			opt.AvoidZones = zonesWithout("seers-north-highwaymen", from, to, 3)
			before := p.Plan(from, to, opt)
			opt.AvoidZones = zonesWithout("", from, to, 3)
			after := p.Plan(from, to, opt)
			planned++
			if before.OK != after.OK || before.Cost != after.Cost {
				changed++
				t.Errorf("%s: before ok=%v cost %d, after ok=%v cost %d (%s)", name, before.OK, before.Cost, after.OK, after.Cost, after.Reason)
				continue
			}
			if !after.OK {
				t.Logf("%s: unplannable before and after (%s)", name, after.Reason)
				continue
			}
			if d, at := closestTo(after, seersHighwaymen); d <= highwaymanReach {
				t.Errorf("%s: passes %d tiles from a Highwayman at %s", name, d, at)
			}
		}
	}
	t.Logf("%d legs planned both ways at combat 3: %d changed by the Seers Highwaymen zone", planned, changed)
}

// TestSeersHighwaymenCloseTheRoadNorthBelowCombat11 records what the zone costs, because it
// is not free: the road north out of Seers runs between the two spawns (x 2735-2737 at
// z 3508-3535, 2 tiles from the west one), every tile of it within their reach, and the
// planner finds no other way north on foot. At combat 3 the walk the brief names, Seers bank
// -> 2731,3543, plans past them at 1-2 tiles without the zone; with it, an account without
// Agility 35 has no route at all, and one with it is sent round by the Coal-trucks log balance
// and the Fremennik basalt rocks, several times longer (and past a level-64 wolf at 2647,3584,
// which no zone covers). At combat 11 the Highwaymen no longer hunt it and the road is back.
func TestSeersHighwaymenCloseTheRoadNorthBelowCombat11(t *testing.T) {
	p := realPack(t)
	from, to := Point{X: 2725, Z: 3493}, Point{X: 2731, Z: 3543}
	opt := Options{AllowNPC: true, AllowSpecial: true}

	opt.AvoidZones = zonesWithout("seers-north-highwaymen", from, to, 3)
	road := p.Plan(from, to, opt)
	if !road.OK {
		t.Fatalf("no road north without the zone: %s", road.Reason)
	}
	if d, at := closestTo(road, seersHighwaymen); d > highwaymanHuntRange {
		t.Errorf("the road north passes %d tiles from a Highwayman (at %s); the zone was meant for a road inside his hunt range", d, at)
	} else {
		t.Logf("without the zone: cost %d, passing %d tile(s) from a Highwayman at %s", road.Cost, d, at)
	}

	opt.AvoidZones = zonesWithout("", from, to, 3)
	fresh := &WorldState{Members: true, Skills: map[string]int{"agility": 1}, FreeSlots: 28,
		QuestStatus: func(string) QuestProgress { return QuestNotStarted }, ItemCount: func(string) int { return 0 }, WornCount: func(string) int { return 0 }}
	opt.State = fresh
	if r := p.Plan(from, to, opt); r.OK {
		t.Errorf("a fresh combat-3 account still has a route north: cost %d", r.Cost)
	}
	agile := *fresh
	agile.Skills = map[string]int{"agility": 35}
	opt.State = &agile
	// The way round for an Agility-35 account passes a level-64 Rellekka-road wolf, so since the
	// wolf zones (rellekkawolves.go, owner's ruling 2026-09-25) it too is closed at combat 3. The
	// control plans it without those two zones: it exists, and it runs inside a wolf's reach.
	opt.AvoidZones = zonesWithoutIDs([]string{"rellekka-road-wolves", "viking-wolves"}, from, to, 3)
	round := p.Plan(from, to, opt)
	if !round.OK {
		t.Fatalf("control: no way round for an Agility-35 account without the wolf zones: %s", round.Reason)
	}
	if d, at := closestTo(round, seersHighwaymen); d <= highwaymanReach {
		t.Errorf("the way round passes %d tiles from a Highwayman at %s", d, at)
	}
	wolves := make([]Point, 0, len(rellekkaWolfSpawns))
	for _, w := range rellekkaWolfSpawns {
		wolves = append(wolves, Point{X: w[0], Z: w[1]})
	}
	if d, at := closestTo(round, wolves); d > rellekkaWolfReach {
		t.Errorf("control: the way round no longer passes a wolf (closest %d at %s); the wolf zone check below proves nothing", d, at)
	} else {
		t.Logf("without the wolf zones: Agility 35 goes round at cost %d (%.1fx), %d tiles from the wolf at %s", round.Cost, float64(round.Cost)/float64(road.Cost), d, at)
	}
	opt.AvoidZones = zonesWithout("", from, to, 3)
	if r := p.Plan(from, to, opt); r.OK {
		t.Errorf("with the wolf zones a combat-3 Agility-35 account still goes round, cost %d", r.Cost)
	}

	opt.State = nil
	opt.AvoidZones = zonesWithout("", from, to, 11)
	if r := p.Plan(from, to, opt); !r.OK || r.Cost != road.Cost {
		t.Errorf("combat 11: ok=%v cost %d, want the road's %d", r.OK, r.Cost, road.Cost)
	}
}
