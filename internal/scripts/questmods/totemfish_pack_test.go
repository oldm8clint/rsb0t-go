package questmods

// Tribal Totem and the Fishing Contest on the real data: the production config, the baked loc index, the collision pack and the web
// graph (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them). The questions are whether every id
// the plans carry names what it says in the config the fleet's client reads, whether the customs' own loc queries find their locs where
// the plans say and each has a tile to stand on, whether every leg the quests walk is a route the planner finds on the real pack,
// whether the legs the customs exist for (the trapped stairs, the compound's gate) are the ones the generic walker must not take,
// whether the pockets' rectangles are the regions the pack makes them, and whether the stands the plans use for the dogs' wood are
// outside the dogs' reach.

import (
	"testing"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestTotemFishingIdsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjTribalTotem: "Totem", questplan.ObjTotemLabel: "Address label", questplan.ObjRedVineWorm: "Red vine worm",
		questplan.ObjFishingTrophy: "Fishing trophy", questplan.ObjFishingPass: "Fishing pass", questplan.ObjFishingRod: "Fishing rod",
		questplan.ObjFishingBait: "Fishing bait", questplan.ObjRawSardine: "Raw sardine", questplan.ObjRawGiantCarp: "Raw giant carp",
		questplan.ObjGarlic: "Garlic", questplan.ObjSpade: "Spade", questplan.CoinsID: "Coins",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	if o, _ := cfg.Obj(questplan.ObjRedVineWorm); !o.Stackable {
		t.Errorf("the red vine worm does not stack in the config")
	}
	for id, want := range map[int]string{
		questplan.NpcKangaiMau: "Kangai Mau", questplan.NpcRPDTEmployee: "RPDT employee", questplan.NpcWizardCromperty: "Wizard Cromperty",
		questplan.NpcMountainDwarf: "Mountain Dwarf", questplan.NpcBonzo: "Bonzo", questplan.NpcMorris: "Morris",
		questplan.NpcSinisterStranger: "A Sinister Stranger", questplan.NpcWillowSpot: "Fishing spot", questplan.NpcPipesSpot: "Fishing spot",
		questplan.NpcHarry: "Harry", questplan.NpcGuardDog: "Guard dog",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (found %v), the plan says %q", id, n.Name, ok, want)
		}
	}
	for _, id := range []int{questplan.NpcWillowSpot, questplan.NpcPipesSpot} {
		if n, _ := cfg.Npc(id); cachecfg.OpIndex(n.Ops, "Fish") != 1 || n.Size != 1 {
			t.Errorf("fishing spot %d offers %v at size %d, want Fish as op 1 at size 1", id, n.Ops, n.Size)
		}
	}
	if n, _ := cfg.Npc(questplan.NpcGuardDog); n.Size != 1 || n.Level != 44 {
		t.Errorf("the guard dog is size %d level %d: the plan prices a level 44 dog of one tile (hunts to combat 88)", n.Size, n.Level)
	}
	for _, c := range []struct {
		id       int
		name, op string
		opIndex  int
		w, l     int
	}{
		{questplan.LocHornCrate, "Crate", "Investigate", 2, 1, 1}, {questplan.LocTeleportCrate, "Crate", "Investigate", 2, 1, 1},
		{questplan.LocComboDoor, "Door", "Open", 1, 1, 1}, {questplan.LocMansionDoor, "Door", "Open", 1, 1, 1},
		{questplan.LocTrapStairs, "Stairs", "Investigate", 2, 2, 3}, {questplan.LocTrapStairs, "Stairs", "Climb-up", 1, 2, 3},
		{questplan.LocTotemChestShut, "Chest", "Open", 1, 1, 1}, {questplan.LocTotemChestOpen, "Chest", "Search", 1, 1, 1},
		{questplan.LocMansionStairsDn, "Staircase", "Climb-down", 1, 0, 0},
		{questplan.LocWallPipe, "Wall Pipe", "Check", 1, 1, 1},
		{questplan.LocHemensterGateLeft, "Gate", "Open", 1, 1, 1}, {questplan.LocHemensterGateRight, "Gate", "Open", 1, 1, 1},
		{questplan.LocMcGruborRailing, "Loose Railing", "Squeeze-through", 1, 1, 1},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name || cachecfg.OpIndex(l.Ops, c.op) != c.opIndex {
			t.Errorf("loc %d is %q offering %v (found %v), the plan says %q with %q as op %d", c.id, l.Name, l.Ops, ok, c.name, c.op, c.opIndex)
			continue
		}
		if c.w != 0 && (l.Width != c.w || l.Length != c.l) {
			t.Errorf("loc %d (%s) is %dx%d, the plan says %dx%d", c.id, l.Name, l.Width, l.Length, c.w, c.l)
		}
	}
	for _, id := range questplan.FcRedVineLocs {
		if l, ok := cfg.Loc(id); !ok || l.Name != "Vine" || cachecfg.OpIndex(l.Ops, "Check") != 1 {
			t.Errorf("vine loc %d is %q offering %v (found %v)", id, l.Name, l.Ops, ok)
		}
	}
	// The chest's forceapproach and the staircase upstairs' are the config's own: the stands below are computed from them.
	if l, _ := cfg.Loc(questplan.LocTotemChestShut); l.ForceApproach == 0 {
		t.Errorf("the shut chest has no forceapproach in the config: the TS def's stand and this plan's assume one")
	}
}

// ttPlanner is the planner over the real pack and graph, with the options a quest leg uses.
func ttPlan(t *testing.T) func(from, to tile.Tile) webwalk.Route {
	p, pt := mcPlanner(t)
	return func(from, to tile.Tile) webwalk.Route {
		return p.Plan(pt(from), pt(to), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	}
}

// The customs' own loc queries find their locs where the plans say, and each has a tile the engine accepts the op from.
func TestTotemFishLocQueriesFindTheirLocs(t *testing.T) {
	d := csDeps(t)
	type want struct {
		what      string
		id        int
		op        string
		near      tile.Tile
		radius    int
		x, z, lvl int
		stands    []tile.Tile // tiles the plan relies on, each of which must be among the accepted stands
	}
	for _, c := range []want{
		{"the mansion's crate", questplan.LocHornCrate, "Investigate", questplan.DepotCrates, 8, 2650, 3273, 0, nil},
		{"the Wizards' Tower crate", questplan.LocTeleportCrate, "Investigate", questplan.DepotCrates, 8, 2650, 3271, 0, nil},
		{"the security door", questplan.LocComboDoor, "Open", questplan.ComboStand, 6, 2634, 3323, 0, []tile.Tile{questplan.ComboStand}},
		{"the trapped stairs", questplan.LocTrapStairs, "Investigate", questplan.StairsStand, 6, 2631, 3322, 0, nil},
		{"the shut chest", questplan.LocTotemChestShut, "Open", questplan.TotemChest, 6, 2638, 3324, 1, nil},
		{"the pipes' wall pipe", questplan.LocWallPipe, "Check", questplan.FcPipeStand, 0, 2638, 3446, 0, []tile.Tile{questplan.FcPipeStand}},
		{"the gate's left half", questplan.LocHemensterGateLeft, "Open", questplan.FcGateOutside, 3, 2642, 3442, 0,
			[]tile.Tile{questplan.FcGateOutside, questplan.FcGateInside}},
		{"the gate's right half", questplan.LocHemensterGateRight, "Open", questplan.FcGateOutside, 3, 2642, 3441, 0, []tile.Tile{{X: 2643, Z: 3441}, {X: 2642, Z: 3441}}},
		{"the loose railing", questplan.LocMcGruborRailing, "Squeeze-through", questplan.FcWoodOutside, 3, 2662, 3500, 0, []tile.Tile{questplan.FcWoodOutside}},
	} {
		m, ok := questrunner.FindLoc(d, c.id, "", c.op, c.near, c.radius)
		if !ok {
			t.Errorf("%s: no loc %d found near %d,%d,%d", c.what, c.id, c.near.X, c.near.Z, c.near.Level)
			continue
		}
		if m.X != c.x || m.Z != c.z || m.Level != c.lvl {
			t.Errorf("%s found at %d,%d,%d, want %d,%d,%d", c.what, m.X, m.Z, m.Level, c.x, c.z, c.lvl)
		}
		stands := questrunner.LocStands(d, m, c.near)
		if len(stands) == 0 {
			t.Errorf("%s at %d,%d,%d has no tile the engine accepts an op from", c.what, m.X, m.Z, m.Level)
			continue
		}
		walkable := 0
		for _, s := range stands {
			if d.Pack.Walkable(s.X, s.Z, s.Level) {
				walkable++
			}
		}
		if walkable == 0 {
			t.Errorf("%s: none of the %d stands is walkable: %v", c.what, len(stands), stands)
		}
		for _, s := range c.stands {
			if !standAmong(s, stands) {
				t.Errorf("%s: %v is not among the stands %v", c.what, s, stands)
			}
		}
		t.Logf("%-26s at %d,%d,%d stands %v", c.what, m.X, m.Z, m.Level, stands)
	}
	// A straight wall decoration (the pipe) is operated from its own tile and from no other.
	pipe, _ := questrunner.FindLoc(d, questplan.LocWallPipe, "", "Check", questplan.FcPipeStand, 0)
	if stands := questrunner.LocStands(d, pipe, questplan.FcPipeStand); len(stands) != 1 || stands[0] != questplan.FcPipeStand {
		t.Errorf("the wall pipe's stands are %v, want its own tile alone", stands)
	}
	// The chest is open to be searched after the Open, and the open one is found by its own id (the shut one is gone from the live
	// placement only while another account has it open; the index holds the map's shut chest).
	if _, ok := questrunner.FindLoc(d, questplan.LocTotemChestOpen, "", "Search", questplan.TotemChest, 6); ok {
		t.Errorf("the map places an OPEN chest: the plan opens it first")
	}
	// The two crates of the depot are 2650,3273 and 2650,3271 and the id picks the right one of the five "Crate"s in the room.
	named := 0
	for _, loc := range []int{354, 355, 359, 361, questplan.LocHornCrate, questplan.LocTeleportCrate} {
		if _, ok := questrunner.FindLoc(d, loc, "", "", questplan.DepotCrates, 4); ok {
			named++
		}
	}
	if named < 5 {
		t.Errorf("only %d of the depot's crates and boxes found by id near %v", named, questplan.DepotCrates)
	}
}

func standAmong(s tile.Tile, stands []tile.Tile) bool {
	for _, got := range stands {
		if got == s {
			return true
		}
	}
	return false
}

// The talks have a tile to stand on: every npc the plans talk to or fish has a neighbour the engine accepts an op from, and the
// walkable one is reachable.
func TestTotemFishNpcStandsExist(t *testing.T) {
	pk, _, _ := csData(t)
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{
		{"Kangai Mau", questplan.KangaiAnchor}, {"the employee", questplan.RPDTAnchor}, {"the Wizard Cromperty", questplan.CrompertyAnchor},
		{"the west dwarf", questplan.FcDwarfAnchor}, {"Bonzo", questplan.FcBonzoAnchor}, {"the pipes' spot", questplan.FcPipesSpot},
		{"Harry", questplan.FcHarry.Anchor}, {"the Stranger", tile.Tile{X: 2637, Z: 3440}},
	} {
		stands := pk.InteractTiles(pathfind.NavPoint{X: c.at.X, Z: c.at.Z, Level: c.at.Level})
		if len(stands) == 0 {
			t.Errorf("%s at %v has no tile an op reaches it from", c.what, c.at)
		}
		for _, s := range stands {
			if !pk.Walkable(s.X, s.Z, s.Level) {
				t.Errorf("%s: stand %v is not walkable", c.what, s)
			}
		}
	}
	// The pipes' spot is inside the compound and so are its stands: the cast is made from the yard.
	for _, s := range pk.InteractTiles(pathfind.NavPoint{X: questplan.FcPipesSpot.X, Z: questplan.FcPipesSpot.Z}) {
		if !questplan.InFishingCompound(tile.Tile{X: s.X, Z: s.Z, Level: s.Level}) {
			t.Errorf("the pipes' spot's stand %v is outside the compound", s)
		}
	}
}

// Every leg the two quests walk plans on the real pack and graph.
func TestTotemFishLegsPlanOnTheRealPack(t *testing.T) {
	plan := ttPlan(t)
	ard := questplan.ArdougneSouthBank
	catherby := tile.Tile{X: 2809, Z: 3440}
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		// Tribal Totem.
		{"the Ardougne bank to Kangai Mau", ard, questplan.KangaiAnchor},
		{"Kangai Mau to the depot's crates", questplan.KangaiAnchor, questplan.DepotCrates},
		{"the bank to the depot", ard, questplan.DepotCrates},
		{"the depot to the employee", questplan.DepotCrates, questplan.RPDTAnchor},
		{"the employee to the Wizard Cromperty", questplan.RPDTAnchor, questplan.CrompertyAnchor},
		{"the Wizard to the bank", questplan.CrompertyAnchor, ard},
		{"the arrival bay to the security door", questplan.MansionArrival, questplan.ComboStand},
		{"the security door to the stairs", questplan.ComboStand, questplan.StairsStand},
		{"the arrival bay to the stairs", questplan.MansionArrival, questplan.StairsStand},
		{"the stairs' landing to the chest", questplan.StairsLanding, tile.Tile{X: 2638, Z: 3323, Level: 1}},
		{"the chest to Kangai Mau", tile.Tile{X: 2638, Z: 3323, Level: 1}, questplan.KangaiAnchor},
		{"the landing to the bank", questplan.StairsLanding, ard},
		{"the arrival bay to Kangai Mau", questplan.MansionArrival, questplan.KangaiAnchor},
		{"the arrival bay to the bank", questplan.MansionArrival, ard},
		{"the front door to the bank", questplan.MansionFrontOutside, ard},
		// The Fishing Contest.
		{"Seers' bank to the west dwarf", questplan.FcSeersBank, questplan.FcDwarfAnchor},
		{"Catherby to the west dwarf", catherby, questplan.FcDwarfAnchor},
		{"the west dwarf to Harry", questplan.FcDwarfAnchor, questplan.FcHarry.Anchor},
		{"Ardougne's bank to the west dwarf", ard, questplan.FcDwarfAnchor},
		{"Draynor's bank to the west dwarf", questplan.DraynorBank, questplan.FcDwarfAnchor},
		{"Falador's bank to the west dwarf", tile.Tile{X: 3013, Z: 3355}, questplan.FcDwarfAnchor},
		{"the west dwarf to the gate", questplan.FcDwarfAnchor, questplan.FcGateOutside},
		{"Seers' bank to the gate", questplan.FcSeersBank, questplan.FcGateOutside},
		{"Seers' bank to the wood's railing", questplan.FcSeersBank, questplan.FcWoodOutside},
		{"outside the railing to the vine patch", questplan.FcWoodOutside, questplan.FcVineStand},
		{"the vine patch to outside the railing", questplan.FcVineStand, questplan.FcWoodOutside},
		{"outside the railing to the gate", questplan.FcWoodOutside, questplan.FcGateOutside},
		{"the gate's inside to Bonzo", questplan.FcGateInside, questplan.FcBonzoAnchor},
		{"Bonzo to the pipe", questplan.FcBonzoAnchor, questplan.FcPipeStand},
		{"the pipe to the pipes' spot", questplan.FcPipeStand, questplan.FcPipesSpot},
		{"the pipes' spot to the gate's inside", questplan.FcPipesSpot, questplan.FcGateInside},
		{"Morgan's cupboard to the Draynor bank", questplan.GarlicCupboard, questplan.DraynorBank},
		{"the Draynor bank to Morgan's cupboard", questplan.DraynorBank, questplan.GarlicCupboard},
		{"Ardougne to the Edmond's-house spade", ard, questplan.FcSpadeSpawns[0]},
	}
	for _, l := range legs {
		r := plan(l.from, l.to)
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		t.Logf("%-42s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
}

// The trapped staircase is not a way the generic walker can take: the graph has an edge for the staircase down upstairs (1723) and none
// for the trap (2711), so no leg of any quest climbs into it, and the ground floor has no way to the floor above at all.
func TestTotemMansionStairsTheWalkerCanNotClimb(t *testing.T) {
	plan := ttPlan(t)
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	for i := range g.Edges {
		if g.Edges[i].LocID == questplan.LocTrapStairs {
			t.Errorf("the graph has an edge for the trapped stairs: %s", g.Edges[i].String())
		}
	}
	chest := tile.Tile{X: 2638, Z: 3323, Level: 1}
	for _, from := range []tile.Tile{questplan.MansionArrival, questplan.ComboStand, questplan.StairsStand} {
		if r := plan(from, chest); r.OK {
			t.Errorf("%v to the chest upstairs: a route (cost %d): the walker has a way up that is not the custom's\n%s", from, r.Cost, r.String())
		}
	}
	// The way down is on the graph, and the route from the chest to the street uses it, the security door, the front door and the garden
	// gate, and never the trap.
	r := plan(chest, questplan.ArdougneSouthBank)
	if !r.OK {
		t.Fatalf("the chest to the bank: %s", r.Reason)
	}
	var locs []int
	for _, h := range r.Hops() {
		locs = append(locs, h.LocID)
	}
	for _, id := range []int{questplan.LocMansionStairsDn, questplan.LocComboDoor, questplan.LocMansionDoor} {
		if !intsContain(locs, id) {
			t.Errorf("the way out from the chest uses locs %v, want %d among them: %s", locs, id, r.String())
		}
	}
	if intsContain(locs, questplan.LocTrapStairs) {
		t.Errorf("the way out climbs the trapped stairs")
	}
}

func intsContain(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// The front door opens from the inside only: the graph has the door in that direction alone, so no leg of any quest plans a way into
// the mansion from the garden, and the way in is the Wizard's teleport. The arrival bay reaches the street without the security door.
func TestTotemMansionFrontDoorIsOneWay(t *testing.T) {
	plan := ttPlan(t)
	hall := tile.Tile{X: 2635, Z: 3323}
	if r := plan(questplan.MansionFrontOutside, hall); r.OK {
		t.Errorf("the garden to the hall: a route (cost %d): %s", r.Cost, r.String())
	}
	if r := plan(tile.Tile{X: 2635, Z: 3310}, questplan.MansionArrival); r.OK {
		t.Errorf("the garden's path to the arrival bay: a route (cost %d)", r.Cost)
	}
	r := plan(questplan.MansionArrival, questplan.MansionFrontOutside)
	if !r.OK {
		t.Fatalf("the arrival bay to the porch: %s", r.Reason)
	}
	for _, h := range r.Hops() {
		if h.LocID == questplan.LocComboDoor {
			t.Errorf("the way out of the arrival bay goes through the security door: %s", r.String())
		}
	}
}

// The walk out of the garden runs along x 2635, three tiles or more from the whole wander box of each of the mansion's dogs (a dog hunts
// within a tile of itself and wanders three from its spawn), at every tile of the planned route.
func TestTotemTheWalkOutStaysOutOfTheDogsReach(t *testing.T) {
	plan := ttPlan(t)
	dogs := []tile.Tile{{X: 2641, Z: 3315}, {X: 2646, Z: 3325}, {X: 2624, Z: 3319}, {X: 2624, Z: 3327}}
	for _, from := range []tile.Tile{questplan.MansionArrival, questplan.StairsLanding, questplan.MansionFrontOutside} {
		r := plan(from, questplan.ArdougneSouthBank)
		if !r.OK {
			t.Fatalf("%v to the bank: %s", from, r.Reason)
		}
		for _, leg := range r.Legs() {
			if leg.Hop != nil {
				continue
			}
			for i := 1; i < len(leg.Tiles); i++ {
				for _, at := range samplePath(leg.Tiles[i-1], leg.Tiles[i]) {
					if at.Level != 0 || at.Z > 3321 {
						continue // in the mansion or on its porch: the dogs are outside the walls
					}
					for _, dog := range dogs {
						if reach := distToBox(at, dog, 3); reach <= 1 {
							t.Errorf("%v: the walk passes %v, %d from the wander box of the dog at %v", from, at, reach, dog)
						}
					}
				}
			}
		}
	}
}

// samplePath lists the tiles of a straight or diagonal run from one corner to the next.
func samplePath(a, b webwalk.Point) []webwalk.Point {
	out := []webwalk.Point{a}
	x, z := a.X, a.Z
	for x != b.X || z != b.Z {
		if x < b.X {
			x++
		} else if x > b.X {
			x--
		}
		if z < b.Z {
			z++
		} else if z > b.Z {
			z--
		}
		out = append(out, webwalk.Point{X: x, Z: z, Level: a.Level})
	}
	return out
}

// distToBox is the Chebyshev distance from a tile to the box of tiles within r of a spawn (0 inside it).
func distToBox(at webwalk.Point, spawn tile.Tile, r int) int {
	dx := max(abs(at.X-spawn.X)-r, 0)
	dz := max(abs(at.Z-spawn.Z)-r, 0)
	return max(dx, dz)
}

// The compound is a region of the pack: every tile the planner reaches on foot from Bonzo lies inside InFishingCompound, none reached on
// foot from outside the gate does, and the gate's door edges are what joins the two (so the generic walker WOULD cross it, which is
// why the compound is a pocket whose way out is the custom's).
func TestTotemFishTheCompoundIsAPocketOfThePack(t *testing.T) {
	p, pt := mcPlanner(t)
	inside := mcReachable(p, questplan.FcBonzoAnchor, 2620, 2652, 3405, 3455, true)
	outside := mcReachable(p, questplan.FcGateOutside, 2620, 2652, 3405, 3455, true)
	t.Logf("the compound reaches %d tiles on foot, the road outside %d (in the same rectangle)", len(inside), len(outside))
	if len(inside) < 150 {
		t.Fatalf("only %d tiles reached from Bonzo: the pack no longer says a yard", len(inside))
	}
	for _, c := range inside {
		if !questplan.InFishingCompound(c) {
			t.Errorf("%v is reached on foot from Bonzo and is not in InFishingCompound", c)
		}
	}
	for _, o := range outside {
		if questplan.InFishingCompound(o) {
			t.Errorf("%v is reached on foot from outside the gate and is in InFishingCompound", o)
		}
	}
	// The graph joins them through the gate's two halves (and nothing else), both ways: the generic walker would cross.
	for _, c := range []struct{ from, to tile.Tile }{{questplan.FcGateOutside, questplan.FcBonzoAnchor}, {questplan.FcBonzoAnchor, questplan.FcGateOutside}} {
		r := p.Plan(pt(c.from), pt(c.to), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		if !r.OK {
			t.Errorf("%v to %v: %s", c.from, c.to, r.Reason)
			continue
		}
		var gates []int
		for _, h := range r.Hops() {
			gates = append(gates, h.LocID)
		}
		if len(gates) != 1 || (gates[0] != questplan.LocHemensterGateLeft && gates[0] != questplan.LocHemensterGateRight) {
			t.Errorf("%v to %v crosses %v, want exactly one gate half", c.from, c.to, gates)
		}
	}
	// Walking only, there is no way between the two.
	if r := p.Plan(pt(questplan.FcGateOutside), pt(questplan.FcBonzoAnchor), webwalk.Options{WalkOnly: true}); r.OK {
		t.Errorf("a walk-only route from outside the gate to Bonzo (cost %d)", r.Cost)
	}
	// The pocket's registration is the module's own (questmods/fishing.go init).
	for _, at := range []tile.Tile{questplan.FcBonzoAnchor, questplan.FcPipeStand, questplan.FcGateInside} {
		if name, ok := questrunner.PocketAt(at); !ok || name != "the Hemenster competition ground" {
			t.Errorf("%v: pocket %q %v", at, name, ok)
		}
	}
	for _, at := range []tile.Tile{questplan.FcGateOutside, questplan.FcSeersBank, questplan.FcDwarfAnchor, questplan.FcWoodOutside} {
		if name, ok := questrunner.PocketAt(at); ok {
			t.Errorf("%v is not in the compound's pocket, got %q", at, name)
		}
	}
}

// The vine patch: the wood's one door is the railing, which the graph has both ways; the stand is three tiles or more from every dog's
// wander box; every vine within reach of it has a stand the engine accepts, and the nearest two (one tile from it) are reached from
// the stand itself.
func TestTotemFishTheVinePatch(t *testing.T) {
	d := csDeps(t)
	plan := ttPlan(t)
	for _, dog := range questplan.FcWoodDogs {
		if reach := distToBox(webwalk.Point{X: questplan.FcVineStand.X, Z: questplan.FcVineStand.Z}, dog, 3); reach < 3 {
			t.Errorf("the vine stand is %d from the wander box of the dog at %v", reach, dog)
		}
	}
	for _, c := range []struct{ from, to tile.Tile }{{questplan.FcWoodOutside, tile.Tile{X: 2662, Z: 3500}}, {tile.Tile{X: 2662, Z: 3500}, questplan.FcWoodOutside}} {
		r := plan(c.from, c.to)
		if !r.OK || len(r.Hops()) != 1 || r.Hops()[0].LocID != questplan.LocMcGruborRailing {
			t.Errorf("%v to %v: %s, want the loose railing alone", c.from, c.to, r.String())
		}
	}
	// The vines near the stand: each has a stand, and those with a stand out of the dogs' reach (the only ones the custom digs) include
	// the stand itself and enough others that a dig is never short of a vine.
	near, safe, servedFromTheStand := 0, 0, 0
	for _, m := range questplanVines(t, d) {
		stands := questrunner.LocStands(d, m, questplan.FcVineStand)
		if len(stands) == 0 {
			t.Errorf("the vine %d at %d,%d has no stand", m.ID, m.X, m.Z)
			continue
		}
		near++
		for _, s := range stands {
			if questplan.FcSafeFromDogs(s) {
				safe++
				if s == questplan.FcVineStand {
					servedFromTheStand++
				}
				break
			}
		}
	}
	t.Logf("%d vines within %d of the stand, %d with a stand out of the dogs' reach, %d served from the patch's stand itself", near, fishVineRadius, safe, servedFromTheStand)
	if near < 6 || safe < 4 || servedFromTheStand < 1 {
		t.Errorf("%d vines near the stand, %d with a safe stand, %d served from the stand", near, safe, servedFromTheStand)
	}
	if !questplan.FcSafeFromDogs(questplan.FcVineStand) {
		t.Errorf("the patch's stand %v is within a dog's reach", questplan.FcVineStand)
	}
}

// The wood cannot be walked round its dogs: the planner finds no route to the vines with even a one-tile box round each spawn. That is
// the reason the trip is priced by hitpoints and food and not by a route, and if a re-bake ever makes it false the plan should walk
// round.
func TestTotemFishTheWoodCanNotBeWalkedRoundItsDogs(t *testing.T) {
	p, pt := mcPlanner(t)
	free := p.Plan(pt(questplan.FcWoodOutside), pt(questplan.FcVineStand), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	if !free.OK {
		t.Fatalf("no route to the vines at all: %s", free.Reason)
	}
	var zones []webwalk.Rect
	for _, dog := range questplan.FcWoodDogs {
		zones = append(zones, webwalk.Rect{MinX: dog.X - 1, MaxX: dog.X + 1, MinZ: dog.Z - 1, MaxZ: dog.Z + 1, Level: 0})
	}
	if r := p.Plan(pt(questplan.FcWoodOutside), pt(questplan.FcVineStand), webwalk.Options{AllowSpecial: true, AllowNPC: true, AvoidZones: zones}); r.OK {
		t.Errorf("a route round the dogs (cost %d, against %d): the plan can walk round them\n%s", r.Cost, free.Cost, r.String())
	}
	// How near the free route passes the dogs' spawns, for the log.
	nearest := map[tile.Tile]int{}
	for _, leg := range free.Legs() {
		if leg.Hop != nil {
			continue
		}
		for i := 1; i < len(leg.Tiles); i++ {
			for _, at := range samplePath(leg.Tiles[i-1], leg.Tiles[i]) {
				for _, dog := range questplan.FcWoodDogs {
					d := max(abs(at.X-dog.X), abs(at.Z-dog.Z))
					if cur, ok := nearest[dog]; !ok || d < cur {
						nearest[dog] = d
					}
				}
			}
		}
	}
	close := 0
	for dog, d := range nearest {
		if d <= 4 { // within a dog's wander box and its hunt range
			close++
			t.Logf("the route to the vines passes %d tiles from the dog spawned at %v", d, dog)
		}
	}
	t.Logf("%d of the %d dogs can reach the route", close, len(questplan.FcWoodDogs))
	if close == 0 {
		t.Errorf("no dog reaches the route: the pricing of the trip is for nothing")
	}
}

// questplanVines is the custom's own query: the vines within fishVineRadius of the patch's stand.
func questplanVines(t *testing.T, d *questrunner.Deps) []locindex.Match {
	t.Helper()
	var out []locindex.Match
	for _, id := range questplan.FcRedVineLocs {
		for x := questplan.FcVineStand.X - fishVineRadius; x <= questplan.FcVineStand.X+fishVineRadius; x++ {
			for z := questplan.FcVineStand.Z - fishVineRadius; z <= questplan.FcVineStand.Z+fishVineRadius; z++ {
				if m, ok := questrunner.FindLoc(d, id, "", "Check", tile.Tile{X: x, Z: z}, 0); ok && m.X == x && m.Z == z {
					out = append(out, m)
				}
			}
		}
	}
	return out
}

// The dive for the custom's query through the custom itself: fishVineFor finds a vine from the stand.
func TestTotemFishTheDigQueryFindsAVine(t *testing.T) {
	d := csDeps(t)
	m, ok := fishVineFor(d, map[tile.Tile]time.Time{})
	if !ok {
		t.Fatalf("fishVineFor found no vine near %v", questplan.FcVineStand)
	}
	if tile.Chebyshev(tile.Tile{X: m.X, Z: m.Z}, questplan.FcVineStand) > 2 {
		t.Errorf("the first vine is at %d,%d, %d from the stand", m.X, m.Z, tile.Chebyshev(tile.Tile{X: m.X, Z: m.Z}, questplan.FcVineStand))
	}
	// A vine set aside is skipped.
	skip := map[tile.Tile]time.Time{{X: m.X, Z: m.Z}: time.Now().Add(time.Minute)}
	if m2, ok := fishVineFor(d, skip); !ok || (m2.X == m.X && m2.Z == m.Z) {
		t.Errorf("a vine set aside was found again: %+v %v", m2, ok)
	}
}
