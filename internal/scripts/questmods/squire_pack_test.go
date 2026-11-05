package questmods

// The Knight's Sword on the real data: the production config, the baked loc index, the collision pack and the web graph
// (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them). The questions are whether every id the plan
// carries names what it says in the config the fleet's client reads, whether the customs' own loc queries find their locs where the plan
// says and each has a tile to stand on (the cupboard's two approaches and the rocks' east stands among them), whether every walk the quest
// makes is a route on the real pack and none enters the Wilderness, and what the Asgarnian Ice Dungeon's routes cost in tiles an Ice warrior
// holds (the numbers the plan prices the visit at).

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestSquireIdsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjSqPortrait: "Portrait", questplan.ObjSqSword: "Blurite sword", questplan.ObjSqBluriteOre: "Blurite ore", questplan.ObjSqPieDish: "Pie dish",
		questplan.ObjSqPieShell: "Pie shell", questplan.ObjSqUncookedPie: "Uncooked berry pie", questplan.ObjSqRedberryPie: "Redberry pie",
		questplan.ObjSqBurntPie: "Burnt pie", questplan.ObjSqPastryDough: "Pastry dough", questplan.ObjSqRedberries: "Redberries",
		questplan.ObjSqPotOfFlour: "Pot of flour", questplan.ObjSqEmptyPot: "Pot", questplan.ObjSqBucket: "Bucket", questplan.ObjSqBucketWater: "Bucket of water",
		questplan.ObjSqIronBar: "Iron bar", questplan.ObjShark: "Shark", questplan.ObjBass: "Bass",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	// The pie is food to the floor bite (it has an Eat op), which is why it is one of the module's tools; the burnt pie empties; the sword
	// wields.
	for id, op := range map[int]string{questplan.ObjSqRedberryPie: "Eat", questplan.ObjSqBurntPie: "Empty Dish", questplan.ObjSqSword: "Wield"} {
		o, _ := cfg.Obj(id)
		if cachecfg.OpIndex(actions.HeldOps(o.IOps), op) == 0 {
			t.Errorf("obj %d (%s) offers %v, the plan says an %q op", id, o.Name, o.IOps, op)
		}
	}
	// Every pickaxe the rates name.
	for id := range questplan.SqPickaxeRates() {
		if o, ok := cfg.Obj(id); !ok || !strings.HasSuffix(o.Name, " pickaxe") {
			t.Errorf("pickaxe %d is %q", id, o.Name)
		}
	}
	for id, c := range map[int]struct {
		name  string
		level int
		op    string
	}{
		questplan.NpcSquire: {"Squire", 0, "Talk-to"}, questplan.NpcVyvin: {"Sir Vyvin", 0, "Talk-to"}, questplan.NpcReldo: {"Reldo", 0, "Talk-to"},
		questplan.NpcThurgo: {"Thurgo", 0, "Talk-to"}, questplan.WydinShop.NpcID: {"Wydin", 0, "Talk-to"}, 125: {"Ice warrior", 57, "Attack"}, 111: {"Ice giant", 53, "Attack"},
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != c.name || n.Level != c.level || !strings.Contains(strings.Join(n.Ops[:], "|"), c.op) {
			t.Errorf("npc %d is %q level %d ops %v (found %v), the plan says %q level %d with %s", id, n.Name, n.Level, n.Ops, ok, c.name, c.level, c.op)
		}
	}
	for _, c := range []struct {
		id       int
		name, op string
	}{
		{questplan.LocSqCupboardShut, "Cupboard", "Open"}, {questplan.LocSqCupboardOpen, "Cupboard", "Search"}, {questplan.LocSqBluriteRock, "Rocks", "Mine"},
		{questplan.LocSqRange, "Range", ""}, {questplan.LocSqSink, "Sink", ""}, {questplan.LocSqSinkAlt, "Sink", ""}, {questplan.LocSqFountain, "Fountain", ""},
		{questplan.LocSqWell, "Well", ""},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name || (c.op != "" && cachecfg.OpIndex(l.Ops, c.op) == 0) {
			t.Errorf("loc %d is %q %v (found %v), the plan says %q %q", c.id, l.Name, l.Ops, ok, c.name, c.op)
		}
	}
	// The cupboard is two tiles long and approached from one side only; the config says forceapproach (13 is every bit but east).
	if l, _ := cfg.Loc(questplan.LocSqCupboardShut); l.Length != 2 || l.Width != 1 || l.ForceApproach != 13 {
		t.Errorf("the cupboard is %dx%d with forceapproach %d", l.Width, l.Length, l.ForceApproach)
	}
}

// Every tile the quest stands on is walkable, every keeper can be reached from a tile beside, and the cupboard's approaches are exactly
// the stands the engine accepts.
func TestSquireStandsAreWalkableAndTheirNPCsReachable(t *testing.T) {
	d := csDeps(t)
	pk := d.Pack
	for name, at := range map[string]tile.Tile{
		"the Squire": questplan.SqSquireAnchor, "Reldo": questplan.SqReldoAnchor, "Thurgo": questplan.SqThurgoAnchor, "Wydin": questplan.WydinShop.Anchor,
		"the ladder's top": questplan.SqLadderTop, "the dungeon's landing": questplan.SqLanding, "the retreat in Sir Vyvin's room": questplan.SqVyvinRetreat,
	} {
		if !pk.Walkable(at.X, at.Z, at.Level) {
			t.Errorf("%s %v is not a walkable tile", name, at)
		}
	}
	for _, a := range questplan.SqVyvinApproaches {
		if !pk.Walkable(a.X, a.Z, a.Level) {
			t.Errorf("the cupboard's approach %v is not walkable", a)
		}
	}
	// The pie dish lies on a counter in the palace kitchen: its tile is blocked, so it is taken from a tile beside it (the provisioner's
	// ground rung resolves a blocked destination to the tiles that touch it).
	if at := (tile.Tile{X: 3222, Z: 3494}); !pk.Walkable(at.X, at.Z, 0) && len(pk.InteractTiles(np(at))) == 0 {
		t.Errorf("no tile beside the pie dish spawn %v to take it from", at)
	}
	for _, st := range questplan.SqStands {
		if !pk.Walkable(st.Stand.X, st.Stand.Z, st.Stand.Level) {
			t.Errorf("the rock stand %v is not walkable", st.Stand)
		}
	}
	for name, at := range map[string]tile.Tile{"the Squire": questplan.SqSquireAnchor, "Reldo": questplan.SqReldoAnchor, "Thurgo": questplan.SqThurgoAnchor, "Wydin": questplan.WydinShop.Anchor} {
		if len(pk.InteractTiles(np(at))) == 0 {
			t.Errorf("no tile to reach %s at %v from", name, at)
		}
	}
	// The cupboard's stands are exactly the two approaches.
	m, ok := questrunner.FindLoc(d, questplan.LocSqCupboardShut, "", "Open", questplan.SqCupboardAt, 3)
	if !ok || m.X != questplan.SqCupboardAt.X || m.Z != questplan.SqCupboardAt.Z || m.Level != 2 || m.Angle != 1 {
		t.Fatalf("the cupboard: %+v %v", m, ok)
	}
	stands := questrunner.LocStands(d, m, questplan.SqVyvinApproaches[0])
	if !slices.Equal(sortedTiles(stands), sortedTiles(questplan.SqVyvinApproaches)) {
		t.Errorf("the engine accepts an op on the cupboard from %v, the plan says %v", stands, questplan.SqVyvinApproaches)
	}
	// Every approach is south of the cupboard and next to the other.
	for _, a := range questplan.SqVyvinApproaches {
		if a.Z != m.Z-1 {
			t.Errorf("approach %v is not on the south row of the cupboard %d,%d", a, m.X, m.Z)
		}
	}
}

func sortedTiles(ts []tile.Tile) []tile.Tile {
	out := append([]tile.Tile(nil), ts...)
	slices.SortFunc(out, func(a, b tile.Tile) int {
		if a.X != b.X {
			return a.X - b.X
		}
		return a.Z - b.Z
	})
	return out
}

// The customs' own loc queries find their locs where the plan says, and each has a tile to stand on: the water, the ranges, the rocks.
func TestSquireLocQueriesFindTheirLocs(t *testing.T) {
	d := csDeps(t)
	for _, w := range questplan.SqWaterSpots {
		found := false
		for _, id := range w.IDs {
			m, ok := questrunner.FindLoc(d, id, "", "", w.Loc, 4)
			if !ok {
				continue
			}
			found = true
			if m.X != w.Loc.X || m.Z != w.Loc.Z {
				t.Errorf("%s found at %d,%d, the plan says %v", w.Name, m.X, m.Z, w.Loc)
			}
			if !standsWalk(d, m, w.Loc) {
				t.Errorf("%s has no walkable tile the engine accepts an op from", w.Name)
			}
		}
		if !found {
			t.Errorf("%s: no loc of %v near %v", w.Name, w.IDs, w.Loc)
		}
	}
	for _, r := range questplan.SqRangeSpots {
		m, ok := questrunner.FindLoc(d, questplan.LocSqRange, "", "", r.Loc, 3)
		if !ok || m.X != r.Loc.X || m.Z != r.Loc.Z {
			t.Errorf("%s: %+v %v, the plan says %v", r.Name, m, ok, r.Loc)
			continue
		}
		if !standsWalk(d, m, r.Loc) {
			t.Errorf("%s has no walkable tile the engine accepts an op from", r.Name)
		}
	}
	// Each rock is a loc 2110 offering Mine, and the stand the module walks to is one of the stands the engine accepts for it (so the Mine
	// click needs no further step).
	for _, st := range questplan.SqStands {
		m, ok := questrunner.FindLoc(d, questplan.LocSqBluriteRock, "", "Mine", st.Rock, 1)
		if !ok || m.X != st.Rock.X || m.Z != st.Rock.Z {
			t.Errorf("the rock at %v: %+v %v", st.Rock, m, ok)
			continue
		}
		if !slices.Contains(questrunner.LocStands(d, m, st.Stand), st.Stand) {
			t.Errorf("the stand %v is not a stand the engine accepts for the rock at %v (%v)", st.Stand, st.Rock, questrunner.LocStands(d, m, st.Stand))
		}
	}
	// The other two rocks stand where the content says too (they are not used, and say why in the exposure test below).
	for _, at := range []tile.Tile{{X: 3049, Z: 9566}, {X: 3059, Z: 9564}} {
		if _, ok := questrunner.FindLoc(d, questplan.LocSqBluriteRock, "", "Mine", at, 1); !ok {
			t.Errorf("no rock at %v", at)
		}
	}
}

// standsWalk reports a walkable tile among the stands the engine accepts an op on m from.
func standsWalk(d *questrunner.Deps, m locindex.Match, near tile.Tile) bool {
	for _, s := range questrunner.LocStands(d, m, near) {
		if d.Pack.Walkable(s.X, s.Z, s.Level) {
			return true
		}
	}
	return false
}

// Every walk the quest makes is a route on the real pack and the graph, from where the quest's steps leave the account, and none of them
// enters the Wilderness.
func TestSquireEveryLegPlansAndNoneEntersTheWilderness(t *testing.T) {
	p, pt := mcPlanner(t)
	sink := tile.Tile{X: 2965, Z: 3209}
	rng := tile.Tile{X: 2969, Z: 3209}
	psRange := tile.Tile{X: 3018, Z: 3237}
	falGS := questplan.FaladorGeneral.Anchor
	varGS := questplan.VarrockGenStore.Anchor
	palaceSink := tile.Tile{X: 3223, Z: 3494}
	dish := tile.Tile{X: 3222, Z: 3494}
	type leg struct {
		name     string
		from, to tile.Tile
	}
	legs := []leg{
		{"the Squire to Reldo", questplan.SqSquireAnchor, questplan.SqReldoAnchor},
		{"Reldo to the pie dish", questplan.SqReldoAnchor, dish}, {"the dish to the palace sink", dish, palaceSink},
		{"Reldo to Varrock's general store", questplan.SqReldoAnchor, varGS}, {"Varrock's general store to Wydin", varGS, questplan.WydinShop.Anchor},
		{"Reldo to Wydin", questplan.SqReldoAnchor, questplan.WydinShop.Anchor}, {"the Squire to Falador's general store", questplan.SqSquireAnchor, falGS},
		{"Falador's general store to Wydin", falGS, questplan.WydinShop.Anchor}, {"Wydin to Rimmington's sink", questplan.WydinShop.Anchor, sink},
		{"the sink to the range", sink, rng}, {"the range to Thurgo", rng, questplan.SqThurgoAnchor}, {"Wydin to Port Sarim's range", questplan.WydinShop.Anchor, psRange},
		{"Port Sarim's range to Thurgo", psRange, questplan.SqThurgoAnchor}, {"Thurgo to the Squire", questplan.SqThurgoAnchor, questplan.SqSquireAnchor},
		{"the Squire to Sir Vyvin's room", questplan.SqSquireAnchor, questplan.SqVyvinApproaches[0]},
		{"Sir Vyvin's room to the retreat", questplan.SqVyvinApproaches[0], questplan.SqVyvinRetreat},
		{"the retreat to the other approach", questplan.SqVyvinRetreat, questplan.SqVyvinApproaches[1]},
		{"Sir Vyvin's room to Thurgo", questplan.SqVyvinApproaches[0], questplan.SqThurgoAnchor},
		{"Thurgo to the ladder's top", questplan.SqThurgoAnchor, questplan.SqLadderTop}, {"the ladder's top to Thurgo", questplan.SqLadderTop, questplan.SqThurgoAnchor},
		{"the ladder's top to the Squire", questplan.SqLadderTop, questplan.SqSquireAnchor},
		{"the Falador bank to the Squire", questplan.NearestBank(questplan.SqSquireAnchor).Tile, questplan.SqSquireAnchor},
		{"Falador's fountain to Wydin", questplan.SqWaterSpots[2].Loc, questplan.WydinShop.Anchor},
		{"the Squire to the Falador bank", questplan.SqSquireAnchor, questplan.NearestBank(questplan.SqSquireAnchor).Tile},
	}
	for _, st := range questplan.SqStands {
		at := fmt.Sprint(st.Stand)
		legs = append(legs, leg{"the ladder's top to the stand " + at, questplan.SqLadderTop, st.Stand}, leg{"the stand " + at + " to the ladder's top", st.Stand, questplan.SqLadderTop},
			leg{"the landing to the stand " + at, questplan.SqLanding, st.Stand}, leg{"the stand " + at + " to the landing", st.Stand, questplan.SqLanding})
	}
	for _, l := range legs {
		r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		for _, c := range walkedTiles(r) {
			if randomevents.WildernessLevel(c.X, c.Z, c.Level) != 0 {
				t.Errorf("%s: the route enters the Wilderness at %v", l.name, c)
				break
			}
		}
		t.Logf("%-42s cost %4d", l.name, r.Cost)
	}
	// The ladder is a hop of the walk graph, in both directions, from the stand east of it.
	down := p.Plan(pt(questplan.SqLadderTop), pt(questplan.SqLanding), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	up := p.Plan(pt(questplan.SqLanding), pt(questplan.SqLadderTop), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	// Down by loc 1759 at 3008,3150 from the tile east of it; up by loc 1755 at 3008,9550, which sets the account on 3008,3151: a tile
	// from which the ladder's top (3009,3150) is one step, the radius the module leaves the dungeon with.
	for name, c := range map[string]struct {
		r        webwalk.Route
		id, x, z int
		lands    webwalk.Point
	}{"down": {down, 1759, 3008, 3150, pt(questplan.SqLanding)}, "up": {up, 1755, 3008, 9550, webwalk.Point{X: 3008, Z: 3151}}} {
		hops := c.r.Hops()
		if !c.r.OK || len(hops) != 1 || hops[0].LocID != c.id || hops[0].LocX != c.x || hops[0].LocZ != c.z || hops[0].To != c.lands {
			t.Errorf("the ladder %s: ok %v hops %v", name, c.r.OK, hops)
		}
	}
	if hops := down.Hops(); len(hops) == 1 && hops[0].From != pt(questplan.SqLadderTop) {
		t.Errorf("the ladder is climbed down from %v, not from the tile east of it", hops[0].From)
	}
	if tile.Chebyshev(tile.Tile{X: 3008, Z: 3151}, questplan.SqLadderTop) > 1 {
		t.Errorf("the tile the ladder up lands on is not within one of the ladder's top")
	}
}

// walkedTiles is every tile a route walks, on every level: a hop's landing is not interpolated from the tile it was operated at (the
// ladder into the dungeon lands 6,400 tiles north of where it is climbed).
func walkedTiles(r webwalk.Route) []tile.Tile {
	var out []tile.Tile
	for i := 1; i < len(r.Waypoints); i++ {
		a, b := r.Waypoints[i-1], r.Waypoints[i]
		if b.Hop != nil || a.Level != b.Level {
			continue
		}
		dx, dz := b.X-a.X, b.Z-a.Z
		steps := max(absInt(dx), absInt(dz))
		for k := 0; k <= steps; k++ {
			x, z := a.X, a.Z
			if steps > 0 {
				x += dx * k / steps
				z += dz * k / steps
			}
			out = append(out, tile.Tile{X: x, Z: z, Level: a.Level})
		}
	}
	return out
}

// The Rimmington house holds the sink and the range four tiles apart, and Wydin's shop is a short walk from its door.
func TestSquireRimmingtonHouseIsOneStop(t *testing.T) {
	p, pt := mcPlanner(t)
	sink, rng := tile.Tile{X: 2965, Z: 3209}, tile.Tile{X: 2969, Z: 3209}
	r := p.Plan(pt(sink), pt(rng), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	if !r.OK || r.Cost > 10 {
		t.Errorf("the sink to the range: %+v", r)
	}
	if r := p.Plan(pt(questplan.WydinShop.Anchor), pt(sink), webwalk.Options{AllowSpecial: true, AllowNPC: true}); !r.OK || r.Cost > 90 {
		t.Errorf("Wydin to the sink: ok %v cost %d", r.OK, r.Cost)
	}
	// The nearest water and range to the shop are Rimmington's sink and Port Sarim's range, and to the sink the house's own range.
	if got := questplan.SqNearestWater(questplan.WydinShop.Anchor).Name; got != "Rimmington's sink" {
		t.Errorf("the water nearest Wydin is %s", got)
	}
	if got := questplan.SqNearestRange(sink).Name; got != "Rimmington's range" {
		t.Errorf("the range nearest the sink is %s", got)
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The Asgarnian Ice Dungeon
// ---------------------------------------------------------------------------------------------------------------------

// sqThreats are the warriors' and the giants' ground in the model the Magic pens use (fleetplan.PenThreat.Holds): within the wander range,
// the size and two more of the spawn, minus the corner.
func sqThreats(spawns []tile.Tile, size int) []fleetplan.PenThreat {
	var out []fleetplan.PenThreat
	for _, s := range spawns {
		out = append(out, fleetplan.PenThreat{At: s, Size: size, Cut: 6, Herd: true})
	}
	return out
}

func heldBy(g *fleetplan.HeldGrid, route []tile.Tile) int {
	n := 0
	for _, c := range route {
		if g.Holds(c.X, c.Z, c.Level) {
			n++
		}
	}
	return n
}

// The planner's route from the landing to each stand is held by the warriors for no more tiles than the plan pins, by no giant and by
// nothing else a combat-58 account is hunted by; the stand itself is held by the two warriors the plan names; and the east rocks are the
// cheaper pair against the northern two.
func TestSquireTheRouteIsHeldOnlyByTheWarriorsAndThePlanPinsTheCount(t *testing.T) {
	d := csDeps(t)
	p, pt := mcPlanner(t)
	warriors := fleetplan.NewHeldGrid(sqThreats(questplan.SqIceWarriorSpawns, 1))
	giants := fleetplan.NewHeldGrid(sqThreats(questplan.SqIceGiantSpawns, 2))
	// The armed Hobgoblin (npc 123, wander 3, hunts combat 84 and under) at 3019,9591.
	armed := fleetplan.NewHeldGrid([]fleetplan.PenThreat{{At: tile.Tile{X: 3019, Z: 9591}, Size: 1, Cut: 6, Herd: false}})
	plan := func(from, to tile.Tile) []tile.Tile {
		r := p.Plan(pt(from), pt(to), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		if !r.OK {
			t.Fatalf("no route from %v to %v: %s", from, to, r.Reason)
		}
		return tilesOnRoute(r, 0)
	}
	for _, st := range questplan.SqStands {
		in, out := plan(questplan.SqLanding, st.Stand), plan(st.Stand, questplan.SqLanding)
		w := heldBy(warriors, in)
		if w > st.HeldIn || st.HeldIn-w > 6 {
			t.Errorf("the walk in to %v is held by warriors for %d tiles; the plan pins %d (an upper bound within 6)", st.Stand, w, st.HeldIn)
		}
		if wo := heldBy(warriors, out); wo > questplan.SqHeldWalkTiles() {
			t.Errorf("the walk out of %v is held for %d tiles, the plan prices %d each way", st.Stand, wo, questplan.SqHeldWalkTiles())
		}
		if g := heldBy(giants, in) + heldBy(giants, out); g != 0 {
			t.Errorf("a giant holds %d tiles of the walk to %v and back", g, st.Stand)
		}
		if h := heldBy(armed, in) + heldBy(armed, out); h != 0 {
			t.Errorf("the armed Hobgoblin holds %d tiles of the walk to %v and back", h, st.Stand)
		}
		// The stand: the two warriors at 3062,9576 and 3062,9581 hold it, no giant does.
		var holders []tile.Tile
		for _, s := range questplan.SqIceWarriorSpawns {
			if (fleetplan.PenThreat{At: s, Size: 1, Cut: 6}).Holds(st.Stand) {
				holders = append(holders, s)
			}
		}
		if !slices.Equal(sortedTiles(holders), sortedTiles([]tile.Tile{{X: 3062, Z: 9576}, {X: 3062, Z: 9581}})) || giants.Holds(st.Stand.X, st.Stand.Z, 0) {
			t.Errorf("the stand %v is held by warriors %v", st.Stand, holders)
		}
		t.Logf("stand %v: %d tiles in, held by warriors %d (pinned %d)", st.Stand, len(in), w, st.HeldIn)
	}
	// The four rocks priced by the module's own measure (a quarter attack a tick of the strongest hunter that holds each tile, the walk in and
	// out, and the stand for the mining time) at Defence 40 and Mining 10 with a bronze pickaxe: the east pair is no dearer than the best
	// of the four by more than a tenth, and its stands are held by no giant (a hit of 6 and not 7). The northern pair is not used: the
	// first stands under a giant and a warrior and is a single rock, the second walks through the giants' ground.
	f := questplan.MeleeFighter{Defence: 40}
	perTick := func(foe questplan.MeleeFoe) float64 { return questplan.FoePerAttack(foe, f) / float64(foe.AttackTicks) }
	price := func(stand tile.Tile) (float64, bool) {
		cost := func(route []tile.Tile) float64 {
			sum := 0.0
			for _, c := range route {
				switch {
				case giants.Holds(c.X, c.Z, 0):
					sum += perTick(questplan.FoeIceGiant)
				case warriors.Holds(c.X, c.Z, 0):
					sum += perTick(questplan.FoeIceWarrior)
				}
			}
			return sum
		}
		stood := 0.0
		if warriors.Holds(stand.X, stand.Z, 0) {
			stood = perTick(questplan.FoeIceWarrior)
		}
		withGiant := giants.Holds(stand.X, stand.Z, 0)
		if withGiant {
			stood = max(stood, perTick(questplan.FoeIceGiant))
		}
		return cost(plan(questplan.SqLanding, stand)) + cost(plan(stand, questplan.SqLanding)) + float64(questplan.SqMineTicks(10, 7))*stood, withGiant
	}
	best, east := 1e9, 0.0
	prices := map[tile.Tile]float64{}
	for _, rock := range []tile.Tile{{X: 3049, Z: 9566}, {X: 3059, Z: 9564}, {X: 3067, Z: 9581}, {X: 3067, Z: 9582}} {
		m, ok := questrunner.FindLoc(d, questplan.LocSqBluriteRock, "", "Mine", rock, 1)
		if !ok {
			t.Fatalf("no rock at %v", rock)
		}
		for _, s := range questrunner.LocStands(d, m, questplan.SqLanding) {
			if !d.Pack.Walkable(s.X, s.Z, s.Level) {
				continue
			}
			pr, giant := price(s)
			prices[s] = pr
			best = min(best, pr)
			t.Logf("rock %v stand %v: priced %.1f hitpoints at Defence 40, a giant at the stand: %v", rock, s, pr, giant)
		}
	}
	for _, st := range questplan.SqStands {
		east = max(east, prices[st.Stand])
	}
	if east > best*1.1 {
		t.Errorf("the east stands are priced %.1f, the best of the four rocks %.1f", east, best)
	}
}

// The reach of a hunter that notices an account is its wander range and its hunt range (and its size) from its spawn; the route to the
// stands passes the warriors inside that reach whatever it avoids, which is why the visit is priced and not avoided: no route to the east
// stands is free of the warriors' reach, and a route that weights the reach costs more tiles in the herd's hold than the shortest.
func TestSquireNoRouteToTheRocksAvoidsTheHerd(t *testing.T) {
	p, pt := mcPlanner(t)
	held := fleetplan.NewHeldGrid(sqThreats(questplan.SqIceWarriorSpawns, 1))
	reach := func(x, z int) int {
		n := 0
		for _, s := range questplan.SqIceWarriorSpawns {
			if tile.Chebyshev(tile.Tile{X: x, Z: z}, s) <= 3+5 {
				n++
			}
		}
		return n
	}
	for _, st := range questplan.SqStands {
		plain := p.Plan(pt(questplan.SqLanding), pt(st.Stand), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		weighted := p.Plan(pt(questplan.SqLanding), pt(st.Stand), webwalk.Options{AllowSpecial: true, AllowNPC: true,
			StepCost: func(x, z, level int) int { return 20 * reach(x, z) }})
		if !plain.OK || !weighted.OK {
			t.Fatalf("no route to %v: %s %s", st.Stand, plain.Reason, weighted.Reason)
		}
		inReach := func(r webwalk.Route) int {
			n := 0
			for _, c := range tilesOnRoute(r, 0) {
				if reach(c.X, c.Z) > 0 {
					n++
				}
			}
			return n
		}
		if inReach(plain) == 0 || inReach(weighted) == 0 {
			t.Errorf("a route to %v outside every warrior's reach exists", st.Stand)
		}
		if heldBy(held, tilesOnRoute(weighted, 0)) < heldBy(held, tilesOnRoute(plain, 0)) {
			t.Errorf("the route weighted by the reach holds fewer tiles (%d) than the shortest (%d): the plan should price that one",
				heldBy(held, tilesOnRoute(weighted, 0)), heldBy(held, tilesOnRoute(plain, 0)))
		}
	}
}
