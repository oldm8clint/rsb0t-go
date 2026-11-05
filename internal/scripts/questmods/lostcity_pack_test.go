package questmods

// Lost City on the real data: the production config, the baked loc index and the collision pack (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX,
// RSB0T_COLLISION_PACK; each test skips without them). The questions are whether every id the plan carries names what it says in the config the
// fleet's client reads, whether the customs' own loc queries find their locs where the plan says and each has a tile to stand on, whether every
// walk the quest makes is a route the planner finds (the Wilderness exit, the dungeon's legs round the black demons), whether the dungeon is the
// pocket the module says it is and Zanaris is not one, and whether the corridor of the greater demons is the only way, as the pricing says.

import (
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestLostCityIDsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjLcBranch: "Dramen branch", questplan.ObjLcStaff: "Dramen staff", questplan.ObjKnife: "Knife", questplan.ObjShark: "Shark", questplan.CoinsID: "Coins",
		questplan.ObjBronzeAxe: "Bronze axe", questplan.ObjIronAxe: "Iron axe", questplan.ObjSteelAxe: "Steel axe", questplan.ObjMithrilAxe: "Mithril axe",
		questplan.ObjAdamantAxe: "Adamant axe", questplan.ObjRuneAxe: "Rune axe", questplan.ObjBlackAxe: "Black axe",
		questplan.ObjDeathRune: "Death rune", questplan.ObjFireRune: "Fire rune", questplan.ObjAirRune: "Air rune", questplan.ObjEarthRune: "Earth rune",
		questplan.ObjLcLawRune: "Law rune",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	// The staff is worn (Wield), the branch is carved from the pack (Use): the config's own ops.
	if o, _ := cfg.Obj(questplan.ObjLcStaff); cachecfg.OpIndex(o.IOps, "Wield") == 0 {
		t.Errorf("the staff offers %v in the pack, want Wield", o.IOps)
	}
	for id, want := range map[int]string{
		questplan.NpcLcWarrior: "Warrior", questplan.NpcLcShamus: "Shamus", questplan.NpcLcTreeSpirit: "Tree spirit", questplan.NpcLcCaveMonk: "Cave monk",
		questplan.NpcShipMonk: "Monk of Entrana", questplan.NpcLcZombie: "Zombie", questplan.NpcLcGreaterDemon: "Greater demon", questplan.NpcLcBlackDemon: "Black Demon",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (found %v), the plan says %q", id, n.Name, ok, want)
		}
	}
	for _, id := range []int{questplan.NpcLcWarrior, questplan.NpcLcShamus, questplan.NpcLcCaveMonk, questplan.NpcShipMonk} {
		if n, _ := cfg.Npc(id); cachecfg.OpIndex(n.Ops, "Talk-to") != 1 {
			t.Errorf("npc %d offers %v: Talk-to is expected first", id, n.Ops)
		}
	}
	if n, _ := cfg.Npc(questplan.NpcLcTreeSpirit); n.Level != 101 || cachecfg.OpIndex(n.Ops, "Attack") != 2 {
		t.Errorf("the spirit is level %d and offers %v", n.Level, n.Ops)
	}
	if n, _ := cfg.Npc(questplan.NpcLcGreaterDemon); n.Level != 92 || n.Size != 3 {
		t.Errorf("the greater demon is level %d size %d", n.Level, n.Size)
	}
	if n, _ := cfg.Npc(questplan.NpcLcBlackDemon); n.Level != 172 || n.Size != 3 {
		t.Errorf("the black demon is level %d size %d", n.Level, n.Size)
	}
	if n, _ := cfg.Npc(questplan.NpcLcZombie); n.Level != 25 {
		t.Errorf("the zombie is level %d", n.Level)
	}
	for _, c := range []struct {
		id       int
		name, op string
		w, l     int
	}{
		{questplan.LocLcLeprechaunTree, "Tree", "Chop", 2, 2}, {questplan.LocLcDramenTree, "Dramen tree", "Chop down", 2, 2}, {questplan.LocLcShedDoor, "Door", "Open", 1, 1},
		{questplan.LocLcMagicDoor, "Magic door", "Open", 1, 1}, {questplan.LocLcEntranaLadder, "Ladder", "Climb-down", 1, 1},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name || l.Width != c.w || l.Length != c.l {
			t.Errorf("loc %d is %q %dx%d (found %v), the plan says %q %dx%d", c.id, l.Name, l.Width, l.Length, ok, c.name, c.w, c.l)
			continue
		}
		if cachecfg.OpIndex(l.Ops, c.op) != 1 {
			t.Errorf("loc %d (%s) offers %v, want %q as its first op", c.id, l.Name, l.Ops, c.op)
		}
	}
}

// The customs' own loc queries find their locs where the plan says, each at the tile and angle the plan reasons from, and each has a stand the
// engine accepts the op from that the plan walks to.
func TestLostCityLocQueriesFindTheirLocs(t *testing.T) {
	d := csDeps(t)
	pk, _, _ := csData(t)
	for _, c := range []struct {
		what    string
		id      int
		op      string
		at      tile.Tile
		radius  int
		angle   int
		stand   tile.Tile
		hasWall bool
	}{
		{"the leprechaun's tree", questplan.LocLcLeprechaunTree, "Chop", questplan.LcShamusTreeAt, 5, 0, tile.Tile{X: 3138, Z: 3211}, false},
		{"the Dramen tree", questplan.LocLcDramenTree, "Chop down", questplan.LcTreeAt, 4, 0, questplan.LcTreeStand, false},
		{"the shed's door", questplan.LocLcShedDoor, "Open", questplan.LcShedDoorAt, 3, 0, questplan.LcShedOutside, true},
		{"the Magic door", questplan.LocLcMagicDoor, "Open", questplan.LcDoorAt, 3, 0, questplan.LcDoorStand, true},
		{"the ladder on Entrana", questplan.LocLcEntranaLadder, "Climb-down", questplan.LcEntranaLadderAt, 3, 2, tile.Tile{X: 2821, Z: 3374}, false},
	} {
		m, ok := questrunner.FindLoc(d, c.id, "", c.op, c.at, c.radius)
		if !ok || m.X != c.at.X || m.Z != c.at.Z || m.Level != c.at.Level || m.Angle != c.angle {
			t.Errorf("%s: found %+v (%v) near %v", c.what, m, ok, c.at)
			continue
		}
		stands := questrunner.LocStands(d, m, c.stand)
		if !stood(stands, c.stand) {
			t.Errorf("%s: the stands %v lack the plan's %v", c.what, stands, c.stand)
		}
		if !pk.Walkable(c.stand.X, c.stand.Z, c.stand.Level) {
			t.Errorf("%s: the stand %v is not ground", c.what, c.stand)
		}
		if c.hasWall && len(stands) != 2 {
			t.Errorf("%s: a straight wall has two stands, got %v", c.what, stands)
		}
	}
	// The shed's door is opened from the column the plan says: not the door's own (check_axis), the tile across the edge it sits on.
	door, _ := questrunner.FindLoc(d, questplan.LocLcShedDoor, "", "Open", questplan.LcShedDoorAt, 3)
	stands := questrunner.LocStands(d, door, questplan.LcShedOutside)
	if stands[0] != questplan.LcShedOutside || stands[1].X != questplan.LcShedDoorAt.X {
		t.Errorf("the shed's door stands from outside: %v", stands)
	}
	// The tiles the three jumps land on, and the stands the customs walk to, are ground.
	for _, at := range []tile.Tile{questplan.LcDungeonArrival, questplan.LcWildernessLanding, questplan.LcZanarisArrival, questplan.LcZombieStand, questplan.LcTreeStand,
		questplan.LcDoorStand, questplan.LcShedOutside, questplan.LcCaveMonkAnchor, questplan.LcWarriorAnchor, questplan.LcKnifeSpawn,
		questplan.LcExits()[0].Landing, questplan.LcExits()[1].Landing} {
		if !pk.Walkable(at.X, at.Z, at.Level) {
			t.Errorf("tile %v is not ground", at)
		}
	}
	// The Dramen tree is a 2x2 loc on 2860,9734: its tiles are not ground, and the tree's stand is among its stands.
	for _, at := range []tile.Tile{{X: 2860, Z: 9734}, {X: 2861, Z: 9735}} {
		if pk.Walkable(at.X, at.Z, 0) {
			t.Errorf("the Dramen tree's tile %v is walkable", at)
		}
	}
}

// lcZones is the automatic danger zones a walk from one tile to another avoids, for a combat level 3 account (the strictest).
func lcZones(from, to tile.Tile) []webwalk.Rect {
	start, dest := webwalk.Point{X: from.X, Z: from.Z, Level: from.Level}, webwalk.Point{X: to.X, Z: to.Z, Level: to.Level}
	return webwalk.ResolveDangerZones(nil, nil, webwalk.ZoneContext{IncludeAutomatic: true, CombatKnown: true, CombatLevel: 3, Start: &start, Dest: &dest})
}

func lcPlan(p *webwalk.Planner, pt func(tile.Tile) webwalk.Point, from, to tile.Tile, extra []webwalk.Rect) webwalk.Route {
	zones := append(lcZones(from, to), extra...)
	return p.Plan(pt(from), pt(to), webwalk.Options{AllowSpecial: true, AllowNPC: true, AvoidZones: zones})
}

// Every walk the quest makes plans on the real pack: the mainland errands, the ship's two ends, the dungeon's legs round the black demons, the walks
// from the two exit teleports' landings to the shed and the bank, the walk out of the Wilderness from the Magic door's landing (the last resort, which
// starts inside it) and out of Zanaris by its ladder.
func TestLostCityLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	deck := tile.Tile{X: 2834, Z: 3331, Level: 1}
	avoid := make([]webwalk.Rect, 0, 4)
	for _, r := range questplan.LcBlackDemonAvoid() {
		avoid = append(avoid, webwalk.Rect{MinX: r.MinX, MaxX: r.MaxX, MinZ: r.MinZ, MaxZ: r.MaxZ, Level: r.Level})
	}
	type leg struct {
		name     string
		from, to tile.Tile
		avoid    []webwalk.Rect
	}
	legs := []leg{
		{"Draynor's bank to the warrior", questplan.DraynorBank, questplan.LcWarriorAnchor, nil},
		{"the warrior to the leprechaun's tree", questplan.LcWarriorAnchor, questplan.LcShamusTreeAt, nil},
		{"Draynor's bank to the leprechaun's tree", questplan.DraynorBank, questplan.LcShamusTreeAt, nil},
		{"the tree to Draynor's bank", questplan.LcShamusTreeAt, questplan.DraynorBank, nil},
		{"the Lumbridge knife to Draynor's bank", questplan.LcKnifeSpawn, questplan.DraynorBank, nil},
		{"Draynor's bank to the Port Sarim monks", questplan.DraynorBank, questplan.PortSarimMonks, nil},
		{"the monks to Draynor's bank", questplan.PortSarimMonks, questplan.DraynorBank, nil},
		{"Entrana's deck to the cave monk", deck, questplan.LcCaveMonkAnchor, nil},
		// The dungeon: the black demons' ground is kept out of on every leg but the corridor's own.
		{"the ladder's foot to the zombies' stand", questplan.LcDungeonArrival, questplan.LcZombieStand, avoid},
		{"the zombies' stand to the tree's stand", questplan.LcZombieStand, questplan.LcTreeStand, avoid},
		{"the ladder's foot to the tree's stand", questplan.LcDungeonArrival, questplan.LcTreeStand, avoid},
		{"the tree's stand to the Magic door's stand", questplan.LcTreeStand, questplan.LcDoorStand, avoid},
		{"the zombies' stand to the Magic door's stand", questplan.LcZombieStand, questplan.LcDoorStand, avoid},
		{"the ladder's foot to the Magic door's stand", questplan.LcDungeonArrival, questplan.LcDoorStand, avoid},
		{"the Magic door's stand to the zombies' stand", questplan.LcDoorStand, questplan.LcZombieStand, avoid},
		// The way out is a teleport: the walks from where it lands.
		{"Varrock's landing to the shed", questplan.LcExits()[1].Landing, questplan.LcShedOutside, nil},
		{"Varrock's landing to Draynor's bank", questplan.LcExits()[1].Landing, questplan.DraynorBank, nil},
		{"Lumbridge's landing to the shed", questplan.LcExits()[0].Landing, questplan.LcShedOutside, nil},
		{"Lumbridge's landing to Draynor's bank", questplan.LcExits()[0].Landing, questplan.DraynorBank, nil},
		// The last resort, out of the Wilderness from level 32: the walk starts inside it.
		{"the Wilderness landing to the shed", questplan.LcWildernessLanding, questplan.LcShedOutside, nil},
		{"the Wilderness landing to Draynor's bank", questplan.LcWildernessLanding, questplan.DraynorBank, nil},
		{"the Wilderness landing to Varrock's west bank", questplan.LcWildernessLanding, tile.Tile{X: 3185, Z: 3436}, nil},
		// Zanaris is left by the entrance ladder, which the graph has.
		{"Zanaris's arrival to the shed", questplan.LcZanarisArrival, questplan.LcShedOutside, nil},
		{"Zanaris's arrival to Draynor's bank", questplan.LcZanarisArrival, questplan.DraynorBank, nil},
		{"the shed to Draynor's bank", questplan.LcShedOutside, questplan.DraynorBank, nil},
	}
	for _, l := range legs {
		r := lcPlan(p, pt, l.from, l.to, l.avoid)
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		for _, w := range r.Waypoints {
			if len(l.avoid) > 0 && webwalk.InZones(w.Point, l.avoid) {
				t.Errorf("%s: the route enters a black demon's ground at %v", l.name, w.Point)
			}
		}
		t.Logf("%-46s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
	// The walks from the teleports' landings stay out of the Wilderness altogether: Varrock is south of its line, and the road to the shed runs south.
	for _, e := range questplan.LcExits() {
		r := lcPlan(p, pt, e.Landing, questplan.LcShedOutside, nil)
		for _, w := range r.Waypoints {
			if questplan.LcInWilderness(tile.Tile{X: w.Point.X, Z: w.Point.Z, Level: w.Point.Level}) {
				t.Errorf("the walk from %s's landing to the shed enters the Wilderness at %v", e.Name, w.Point)
			}
		}
	}
	// The Wilderness walk never goes deeper than where it starts: the landing is level 32 and the route runs south.
	r := lcPlan(p, pt, questplan.LcWildernessLanding, questplan.LcShedOutside, nil)
	for _, w := range r.Waypoints {
		if w.Point.Level == 0 && w.Point.Z > questplan.LcWildernessLanding.Z+16*8 {
			t.Errorf("the walk out of the Wilderness goes deeper, to %v", w.Point)
		}
	}
	// Zanaris's way out is the entrance ladder: the second waypoint of the walk to the shed is the shed's own side.
	r = lcPlan(p, pt, questplan.LcZanarisArrival, questplan.LcShedOutside, nil)
	if len(r.Waypoints) < 3 || r.Waypoints[len(r.Waypoints)-1].Point != pt(questplan.LcShedOutside) {
		t.Errorf("Zanaris to the shed: %v", r.Waypoints)
	}
	hopped := false
	for _, w := range r.Waypoints {
		if w.Point.Z < 9000 {
			hopped = true
		}
	}
	if !hopped {
		t.Errorf("the walk out of Zanaris never reaches the surface: %v", r.Waypoints)
	}
}

// The corridor of the two greater demons is the dungeon's only way to the tree and the door, which is why it is priced and not avoided: with the
// black demons' ground kept out of a route exists, with the greater demons' ground too none does. The route that exists passes within hunting
// reach of both.
func TestLostCityTheGreaterDemonsStandInTheOnlyWay(t *testing.T) {
	p, pt := mcPlanner(t)
	rects := func(spawns []tile.Tile, pad int) []webwalk.Rect {
		var out []webwalk.Rect
		for _, h := range spawns {
			out = append(out, webwalk.Rect{MinX: h.X - pad, MaxX: h.X + pad, MinZ: h.Z - pad, MaxZ: h.Z + pad})
		}
		return out
	}
	greater := rects(questplan.LcGreaterDemonSpawns(), questplan.LcDemonPad)
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the zombies to the tree", questplan.LcZombieStand, questplan.LcTreeStand},
		{"the ladder to the tree", questplan.LcDungeonArrival, questplan.LcTreeStand},
		{"the tree to the door", questplan.LcTreeStand, questplan.LcDoorStand},
	} {
		with := lcPlan(p, pt, l.from, l.to, nil)
		if !with.OK {
			t.Errorf("%s: no route at all (%s)", l.name, with.Reason)
			continue
		}
		if r := lcPlan(p, pt, l.from, l.to, greater); r.OK {
			t.Errorf("%s: a route exists round the greater demons (cost %d): the corridor is not the only way, and it need not be priced", l.name, r.Cost)
		}
		reach := map[tile.Tile]bool{}
		for _, h := range questplan.LcGreaterDemonSpawns() {
			for _, w := range cannonRoute(p, l.from, l.to) {
				if tile.Chebyshev(w, h) <= 3+6 {
					reach[h] = true
				}
			}
		}
		if len(reach) != 2 {
			t.Errorf("%s: the route passes within the hunting reach of %d of the two greater demons", l.name, len(reach))
		}
	}
}

// The dungeon is the pocket the module says it is: every tile the planner reaches from the ladder's foot lies in the registered rectangle, the
// rectangle holds nothing that is reached from outside, and the registered pocket is the one the engine leaves by a teleport (the Magic door is the
// last resort). Zanaris is not a pocket: the walk graph has its entrance ladder.
func TestLostCityTheDungeonIsARegisteredPocketAndZanarisIsNot(t *testing.T) {
	pk, _, _ := csData(t)
	p, pt := mcPlanner(t)
	reach := pk.ReachFrom(pathfind.NavPoint{X: questplan.LcDungeonArrival.X, Z: questplan.LcDungeonArrival.Z}, 160, nil)
	n, minX, maxX, minZ, maxZ := 0, 99999, 0, 99999, 0
	for x := 2700; x <= 3000; x++ {
		for z := 9600; z <= 9950; z++ {
			if _, ok := reach.Steps(x, z, 0); !ok {
				continue
			}
			n++
			minX, maxX, minZ, maxZ = min(minX, x), max(maxX, x), min(minZ, z), max(maxZ, z)
			if !questplan.InLcDungeon(tile.Tile{X: x, Z: z}) {
				t.Errorf("%d,%d is reached from the ladder's foot and is not in InLcDungeon", x, z)
			}
		}
	}
	t.Logf("the dungeon reaches %d tiles, x %d-%d, z %d-%d", n, minX, maxX, minZ, maxZ)
	if n < 600 {
		t.Fatalf("the dungeon reaches %d tiles: the pack no longer says 652", n)
	}
	// Nothing in the rectangle is reached on foot from the surface, and nothing in it leaves it: the planner finds no way either direction.
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"Draynor to the ladder's foot", questplan.DraynorBank, questplan.LcDungeonArrival},
		{"the Wilderness landing to the tree", questplan.LcWildernessLanding, questplan.LcTreeStand},
		{"the ladder's foot to Draynor", questplan.LcDungeonArrival, questplan.DraynorBank},
		{"the tree to the Wilderness landing", questplan.LcTreeStand, questplan.LcWildernessLanding},
		{"Varrock's landing to the tree", questplan.LcExits()[1].Landing, questplan.LcTreeStand},
		{"the tree to Varrock's landing", questplan.LcTreeStand, questplan.LcExits()[1].Landing},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true}); r.OK {
			t.Errorf("%s: the planner finds a route (cost %d): the dungeon is not a pocket", l.name, r.Cost)
		}
	}
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{{questplan.LcDungeonArrival, true}, {questplan.LcZombieStand, true}, {questplan.LcTreeStand, true}, {questplan.LcDoorStand, true},
		{questplan.LcWildernessLanding, false}, {questplan.DraynorBank, false}, {questplan.LcZanarisArrival, false}, {questplan.LcCaveMonkAnchor, false}} {
		name, _ := questrunner.PocketAt(c.at)
		if got := name == "the Entrana dungeon"; got != c.want {
			t.Errorf("%v: pocket %q, in the dungeon's pocket %v, want %v", c.at, name, got, c.want)
		}
	}
	// Zanaris: the walk out exists and its entrance ladder's tile is not the dungeon's.
	if r := p.Plan(pt(questplan.LcZanarisArrival), pt(questplan.DraynorBank), webwalk.Options{AllowSpecial: true, AllowNPC: true}); !r.OK {
		t.Errorf("Zanaris to Draynor: no route (%s): Zanaris would be a pocket", r.Reason)
	}
	zr := pk.ReachFrom(pathfind.NavPoint{X: questplan.LcZanarisArrival.X, Z: questplan.LcZanarisArrival.Z}, 120, nil)
	for x := 3100; x <= 3350; x++ {
		for z := 9450; z <= 9700; z++ {
			if _, ok := zr.Steps(x, z, 0); ok && !questplan.InLcZanaris(tile.Tile{X: x, Z: z}) {
				t.Errorf("%d,%d is reached from Zanaris's arrival and is not in InLcZanaris", x, z)
			}
		}
	}
}

// The zombies are fought from ground the account reaches: every zombie spawn of the west side has a tile a melee attack reaches from the stand,
// the stand and the zombies are outside the black demons' ground, and the pack's own rule (Pack.InteractTiles) says the stand is a place to fight from.
func TestLostCityTheZombiesAreWithinReachOfTheStand(t *testing.T) {
	pk, _, _ := csData(t)
	spawns := []tile.Tile{{X: 2842, Z: 9766}, {X: 2843, Z: 9761}, {X: 2846, Z: 9756}, {X: 2846, Z: 9760}, {X: 2849, Z: 9763}}
	reach := pk.ReachFrom(pathfind.NavPoint{X: questplan.LcZombieStand.X, Z: questplan.LcZombieStand.Z}, lcZombieWindowForTest, nil)
	for _, z := range spawns {
		if questplan.InLcBlackDemonGround(z) {
			t.Errorf("the zombie spawn %v is in a black demon's ground", z)
		}
		if n := interactSteps(pk, reach, questplan.LcZombieStand, z); n < 0 || n > 12 {
			t.Errorf("the zombie spawn %v is %d steps from the stand", z, n)
		}
	}
	if questplan.InLcBlackDemonGround(questplan.LcZombieStand) {
		t.Error("the stand is in a black demon's ground")
	}
}

const lcZombieWindowForTest = 30

// The island is a registered pocket too (the walk graph has no way off it), and the tiles the Holy Grail and this quest walk on it are in it: a walk from
// one of them to another is not hooked, a walk from one to the mainland is.
func TestLostCityTheIslandIsARegisteredPocket(t *testing.T) {
	for _, at := range []tile.Tile{{X: 2834, Z: 3331, Level: 1}, questplan.LcCaveMonkAnchor, questplan.HighPriestAnchor, questplan.EntranaDock} {
		if name, ok := questrunner.PocketAt(at); !ok || name != "holy Entrana" {
			t.Errorf("%v: pocket %q (%v), want holy Entrana", at, name, ok)
		}
	}
	for _, at := range []tile.Tile{questplan.PortSarimMonks, questplan.DraynorBank, questplan.LcShedOutside} {
		if name, ok := questrunner.PocketAt(at); ok {
			t.Errorf("%v is in the pocket %q", at, name)
		}
	}
	// The dungeon is under the island and is no part of it.
	if name, _ := questrunner.PocketAt(questplan.LcDungeonArrival); name != "the Entrana dungeon" {
		t.Errorf("the dungeon's arrival is in %q", name)
	}
}

// The two exit teleports are casts the client knows by name: the spellbook buttons actions.Teleport presses are the ones of the spells whose levels
// and runes the plan carries.
func TestLostCityTheExitSpellsAreSpellbookButtons(t *testing.T) {
	want := map[string]int{"Lumbridge Teleport": 1167, "Varrock Teleport": 1164}
	for _, e := range questplan.LcExits() {
		id, ok := actions.TeleportComponent(e.Name)
		if !ok || id != want[e.Name] {
			t.Errorf("%s is the spellbook button %d (known %v), want %d", e.Name, id, ok, want[e.Name])
		}
	}
}
