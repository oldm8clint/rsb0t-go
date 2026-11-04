package questmods

// Monk's Friend and the Clock Tower on the real data: the production config, the baked loc index, the collision pack and the web
// graph (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them). The questions are whether
// every id the plans carry names what it says in the config the fleet's client reads, whether the customs' own loc queries
// find their locs where the plans say and each has a tile to stand on, whether every leg the quests walk is a route the planner
// finds on the real pack, whether the legs the customs exist for (the secret wall, the rat gate, the dynamic ladder) are the
// ones the planner cannot find, and whether the pockets' rectangles are the regions the pack makes them.

import (
	"fmt"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

var (
	mcCellarTop  = tile.Tile{X: 2566, Z: 3243}
	mcCellarFoot = tile.Tile{X: 2566, Z: 9643}
)

func TestMonkCogIdsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjChildsBlanket: "Child's blanket", questplan.ObjJug: "Jug", questplan.ObjJugOfWater: "Jug of water",
		questplan.ObjLogs: "Logs", questplan.ObjBucket: "Bucket", questplan.ObjBucketOfWater: "Bucket of water",
		questplan.ObjCogWhite: "Cog", questplan.ObjCogBlack: "Cog", questplan.ObjCogBlue: "Cog", questplan.ObjCogRed: "Cog",
		questplan.ObjRatPoison: "Rat poison", questplan.ObjShark: "Shark", 365: "Bass", questplan.ObjIronAxe: "Iron axe",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	for id, want := range map[int]string{
		questplan.NpcBrotherOmad: "Brother Omad", questplan.NpcBrotherCedric: "Brother Cedric", questplan.NpcBrotherKojo: "Brother Kojo",
		questplan.NpcOgre: "Ogre", questplan.NpcKhazardShopkeeper: "Shop keeper",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (found %v), the plan says %q", id, n.Name, ok, want)
		}
	}
	if o, _ := cfg.Npc(questplan.NpcOgre); o.Size != 2 || o.Level != 53 {
		t.Errorf("the ogre is size %d level %d: OgreDistance assumes a 2x2 footprint and the guard a level 53 (hunts to combat 106)", o.Size, o.Level)
	}
	for _, c := range []struct {
		id       int
		name, op string
		w, l     int
	}{
		{questplan.LocMonkRingLadder, "Ladder", "Climb-down", 1, 1}, {questplan.LocCaveLadderUp, "Ladder", "Climb-up", 1, 1},
		{questplan.LocSink, "Sink", "", 1, 2}, {questplan.LocSink2, "Sink", "", 1, 2},
		{questplan.LocSpindleRed, "Clock spindle", "", 1, 1}, {questplan.LocSpindleBlack, "Clock spindle", "", 1, 1},
		{questplan.LocSpindleWhite, "Clock spindle", "", 1, 1}, {questplan.LocSpindleBlue, "Clock spindle", "", 1, 1},
		{questplan.LocCogLever, "Lever", "Pull", 1, 1}, {questplan.LocCogLeverDummy, "Lever", "Pull", 1, 1}, {questplan.LocCogLeverPulled, "Lever", "Pull", 1, 1},
		{questplan.LocPenGate, "Gate", "Open", 1, 1}, {questplan.LocRatGate, "Gate", "Go-through", 1, 1},
		{questplan.LocFoodTrough, "Food trough", "", 2, 1}, {questplan.LocCogSecretWall, "Wall", "Push", 1, 1}, {questplan.LocWell, "Well", "", 2, 2},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name || l.Width != c.w || l.Length != c.l {
			t.Errorf("loc %d is %q %dx%d (found %v), the plan says %q %dx%d", c.id, l.Name, l.Width, l.Length, ok, c.name, c.w, c.l)
			continue
		}
		if c.op != "" && cachecfg.OpIndex(l.Ops, c.op) == 0 {
			t.Errorf("loc %d (%s) offers %v, want %q", c.id, l.Name, l.Ops, c.op)
		}
	}
	// The look-alike spindles refuse every cog and so must not be the ones the plan names: 25 to 28 are not 29 to 32.
	for _, c := range questplan.CogColours {
		if c.Spindle < 29 || c.Spindle > 32 {
			t.Errorf("the %s spindle is loc %d", c.Name, c.Spindle)
		}
	}
}

// The customs' own loc queries find their locs where the plans say, and each has a tile the engine accepts the op from.
func TestMonkCogLocQueriesFindTheirLocs(t *testing.T) {
	d := csDeps(t)
	type want struct {
		what      string
		id        int
		name, op  string
		near      tile.Tile
		radius    int
		x, z, lvl int
		stands    []tile.Tile // tiles the plan relies on, each of which must be among the accepted stands
	}
	cs := []want{
		{"the cave's way up", questplan.LocCaveLadderUp, "", "Climb-up", questplan.MonkCaveFoot, 2, 2561, 9622, 0, []tile.Tile{questplan.MonkCaveFoot}},
		{"lever A", questplan.LocCogLever, "", "Pull", questplan.CogLeverTile, 3, 2591, 9661, 0, nil},
		{"the pen gate", questplan.LocPenGate, "", "Open", questplan.PenGateTile, 1, 2595, 9657, 0, []tile.Tile{questplan.PenGateTile, questplan.PenGateOutside}},
		{"the rat gate", questplan.LocRatGate, "", "Go-through", questplan.RatGateTile, 3, 2579, 9656, 0, []tile.Tile{questplan.RatGateTile}},
		{"the food trough", questplan.LocFoodTrough, "", "", questplan.TroughStand, 4, 2586, 9654, 0, []tile.Tile{questplan.TroughStand}},
		{"the secret wall", questplan.LocCogSecretWall, "", "Push", questplan.CogSecretWallStand, 3, 2575, 9631, 0, []tile.Tile{questplan.CogSecretWallStand}},
		{"the well", questplan.LocWell, "", "", questplan.WellStand, 4, 2612, 3254, 0, []tile.Tile{questplan.WellStand}},
	}
	for _, c := range questplan.CogColours {
		cs = append(cs, want{"the " + c.Name + " spindle", c.Spindle, "", "", c.Stand, 5, 0, 0, c.Stand.Level, []tile.Tile{c.Stand}})
	}
	for _, c := range cs {
		m, ok := questrunner.FindLoc(d, c.id, c.name, c.op, c.near, c.radius)
		if !ok {
			t.Errorf("%s: no loc %d found near %d,%d,%d", c.what, c.id, c.near.X, c.near.Z, c.near.Level)
			continue
		}
		if c.x != 0 && (m.X != c.x || m.Z != c.z || m.Level != c.lvl) {
			t.Errorf("%s found at %d,%d,%d, want %d,%d,%d", c.what, m.X, m.Z, m.Level, c.x, c.z, c.lvl)
		}
		if m.Level != c.lvl {
			t.Errorf("%s found on level %d, want %d", c.what, m.Level, c.lvl)
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
			found := false
			for _, got := range stands {
				if got == s {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: %v is not among the stands %v", c.what, s, stands)
			}
		}
	}
	// The three other levers' and spindles' decoys are the reason every use is by id: the cellar holds four spindles within three
	// tiles of the black one, and only one takes a cog.
	named := 0
	for _, loc := range []int{25, 27, 28, questplan.LocSpindleBlack} {
		if _, ok := questrunner.FindLoc(d, loc, "", "", questplan.CogColours[1].Stand, 5); ok {
			named++
		}
	}
	if named != 4 {
		t.Errorf("%d of the four cellar spindles found near the black one's stand", named)
	}
	// Every sink the jug is filled at is a Sink loc where the plan says, with a stand.
	for _, spot := range questplan.MonkSinks {
		m, ok := questrunner.FindLoc(d, 0, "Sink", "", spot, 3)
		if !ok || m.X != spot.X || m.Z != spot.Z {
			t.Errorf("no sink at %v (found %+v %v)", spot, m, ok)
			continue
		}
		if len(questrunner.LocStands(d, m, spot)) == 0 {
			t.Errorf("the sink at %v has no stand", spot)
		}
	}
	// The wall decoration lever is reached from where the plan stands (its own tile).
	lever, _ := questrunner.FindLoc(d, questplan.LocCogLever, "", "Pull", questplan.CogLeverTile, 3)
	got := questrunner.LocStands(d, lever, questplan.CogLeverTile)
	t.Logf("lever A stands: %v", got)
}

func mcPlanner(t *testing.T) (*webwalk.Planner, func(tile.Tile) webwalk.Point) {
	pk, _, _ := csData(t)
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	return webwalk.NewPlanner(pk, g), func(t tile.Tile) webwalk.Point { return webwalk.Point{X: t.X, Z: t.Z, Level: t.Level} }
}

// Every leg the two quests walk plans on the real pack and graph.
func TestMonkCogLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	bank := questplan.ArdougneSouthBank
	red, black, white, blue := questplan.CogColours[3], questplan.CogColours[1], questplan.CogColours[2], questplan.CogColours[0]
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		// Monk's Friend.
		{"bank to Omad", bank, questplan.OmadAnchor}, {"bank to the ring of stones", bank, questplan.MonkRingStand},
		{"the ring of stones to Omad", questplan.MonkRingStand, questplan.OmadAnchor},
		{"Omad to Cedric", questplan.OmadAnchor, questplan.CedricAnchor}, {"Cedric to Omad", questplan.CedricAnchor, questplan.OmadAnchor},
		{"bank to Cedric", bank, questplan.CedricAnchor}, {"Cedric to the bank", questplan.CedricAnchor, bank},
		{"bank to the trees", bank, questplan.MonkTree}, {"the trees to Cedric", questplan.MonkTree, questplan.CedricAnchor},
		{"bank to Port Khazard's store", bank, questplan.KhazardGeneralStore.Anchor},
		{"Port Khazard's store to Cedric", questplan.KhazardGeneralStore.Anchor, questplan.CedricAnchor},
		{"the cave's foot to the blanket", questplan.MonkCaveFoot, questplan.MonkBlanketSpawn},
		{"the blanket to the cave's foot", questplan.MonkBlanketSpawn, questplan.MonkCaveFoot},
		// Clock Tower.
		{"bank to Kojo", bank, questplan.KojoAnchor}, {"Kojo to the cellar's ladder", questplan.KojoAnchor, mcCellarTop},
		{"bank to the well", bank, questplan.WellStand}, {"the well to the bucket", questplan.WellStand, questplan.CogBucketSpawn},
		{"the bucket to the secret wall's corridor", questplan.CogBucketSpawn, questplan.CogSecretWallStand},
		{"the cellar's foot to the black spindle", mcCellarFoot, black.Stand}, {"the cellar's foot to the black cog", mcCellarFoot, black.Spawn},
		{"the black cog to the black spindle", black.Spawn, black.Stand},
		{"the cellar's foot to the poison", mcCellarFoot, questplan.PoisonSpawn},
		{"the poison to the lever", questplan.PoisonSpawn, questplan.CogLeverTile},
		{"the lever to outside the pen gate", questplan.CogLeverTile, questplan.PenGateOutside},
		{"the cellar's foot to the ogre cave's mouth", mcCellarFoot, questplan.RedCogStage},
		{"the cave's mouth to the red cog", questplan.RedCogStage, red.Spawn}, {"the red cog to the cave's mouth", red.Spawn, questplan.RedCogStage},
		{"the cave's mouth to the red spindle", questplan.RedCogStage, red.Stand}, {"the red cog to the red spindle", red.Spawn, red.Stand},
		{"the blue room's ladder to the blue spindle", tile.Tile{X: 2572, Z: 9632}, blue.Stand},
		{"the blue cog to the blue spindle", blue.Spawn, blue.Stand},
		{"the white room's ladder to the white spindle", tile.Tile{X: 2575, Z: 9656}, white.Stand},
		{"the white cog to the white spindle", white.Spawn, white.Stand},
		{"the white cog to the bank", white.Spawn, bank},
		{"the red spindle to Kojo", red.Stand, questplan.KojoAnchor}, {"the blue spindle to Kojo", blue.Stand, questplan.KojoAnchor},
		{"the white spindle to Kojo", white.Stand, questplan.KojoAnchor},
	}
	for _, s := range questplan.MonkSinks {
		legs = append(legs, struct {
			name     string
			from, to tile.Tile
		}{fmt.Sprintf("bank to the sink at %d,%d", s.X, s.Z), bank, s})
	}
	for _, l := range legs {
		r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		t.Logf("%-46s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
}

// The places the customs exist for are the places the planner cannot reach on foot: a wall that opens by a push, a gate that
// opens by a lever and a poison, a ladder the player's own timer grows. If a re-bake ever makes one of them reachable, the walker
// would take it and the custom's leg is the wrong tool; this says so.
func TestMonkCogTheScriptedWaysAreNotOnTheGraph(t *testing.T) {
	p, pt := mcPlanner(t)
	blue, white := questplan.CogColours[0], questplan.CogColours[2]
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the surface to the blue cog", questplan.KojoAnchor, blue.Spawn},
		{"the corridor east of the secret wall to the blue cog", questplan.CogSecretWallStand, blue.Spawn},
		{"the cellar to the white cog", mcCellarFoot, white.Spawn},
		{"the lever's corridor to the pen's gate tile", questplan.CogLeverTile, questplan.PenGateTile},
		{"the lever's corridor to the trough", questplan.CogLeverTile, questplan.TroughStand},
		{"the surface to the thieves' cave's foot", questplan.MonkRingStand, questplan.MonkCaveFoot},
		{"the surface to the blanket", questplan.MonkRingStand, questplan.MonkBlanketSpawn},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true}); r.OK {
			t.Errorf("%s: the planner finds a route (cost %d): the scripted way is not the only way", l.name, r.Cost)
		}
	}
	// And the other direction out of each, where the graph does have the ladder or the account has a pocket leave.
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the blue room to the bank", tile.Tile{X: 2573, Z: 9632}, questplan.ArdougneSouthBank},
		{"the white room to the bank", tile.Tile{X: 2577, Z: 9655}, questplan.ArdougneSouthBank},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true}); !r.OK {
			t.Errorf("%s: no route (%s): the room's ladder is not in the graph", l.name, r.Reason)
		}
	}
	// The thieves' cave and the rat pen have no way out the planner finds: they are the pockets.
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the thieves' cave to the bank", questplan.MonkCaveFoot, questplan.ArdougneSouthBank},
		{"the rat pen to the bank", questplan.TroughStand, questplan.ArdougneSouthBank},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true}); r.OK {
			t.Errorf("%s: the planner finds a route (cost %d): there is no pocket", l.name, r.Cost)
		}
	}
}

// reachable lists the tiles of the rectangle the planner reaches from a start, with the given options.
func mcReachable(p *webwalk.Planner, from tile.Tile, minX, maxX, minZ, maxZ int, walkOnly bool) []tile.Tile {
	var out []tile.Tile
	for x := minX; x <= maxX; x++ {
		for z := minZ; z <= maxZ; z++ {
			r := p.Plan(webwalk.Point{X: from.X, Z: from.Z, Level: from.Level}, webwalk.Point{X: x, Z: z, Level: from.Level},
				webwalk.Options{AllowSpecial: true, AllowNPC: true, WalkOnly: walkOnly})
			if r.OK && r.End().X == x && r.End().Z == z {
				out = append(out, tile.Tile{X: x, Z: z, Level: from.Level})
			}
		}
	}
	return out
}

// The pockets' rectangles are the regions the pack makes them: every tile the planner reaches from the cave's foot (doors
// included) and from the trough (walking only) lies inside the rectangle, and the rectangle holds nothing that is reached from the
// cellar's foot.
func TestMonkCogThePocketsAreTheRegionsOfThePack(t *testing.T) {
	pk, _, _ := csData(t)
	p, _ := mcPlanner(t)
	cave := mcReachable(p, questplan.MonkCaveFoot, 2556, 2584, 9598, 9628, false)
	pen := mcReachable(p, questplan.TroughStand, 2574, 2600, 9648, 9664, true)
	t.Logf("the thieves' cave reaches %d tiles, the rat pen %d", len(cave), len(pen))
	if len(cave) < 140 || len(pen) < 110 {
		t.Fatalf("the cave reaches %d tiles and the pen %d: the pack no longer says 151 and 116", len(cave), len(pen))
	}
	for _, c := range cave {
		if !questplan.InMonkCave(c) {
			t.Errorf("%v is reached from the cave's foot and is not in InMonkCave", c)
		}
	}
	for _, c := range pen {
		if !questplan.InCogPen(c) {
			t.Errorf("%v is reached from the trough and is not in InCogPen", c)
		}
	}
	// Nothing in either rectangle is reached on foot from the cellar's foot, where the account comes from when it is not in the
	// pocket (walking only: a plan to a tile nothing reaches searches the whole graph, a second each). This is the check that
	// found the pen's rectangle holding two tiles of the cellar's corridor.
	for _, o := range mcReachable(p, mcCellarFoot, 2556, 2584, 9598, 9628, true) {
		if questplan.InMonkCave(o) {
			t.Errorf("%v is reached on foot from the cellar's foot and is in the thieves' cave", o)
		}
	}
	for _, o := range mcReachable(p, mcCellarFoot, 2574, 2600, 9648, 9664, true) {
		if questplan.InCogPen(o) {
			t.Errorf("%v is reached on foot from the cellar's foot and is in the rat pen", o)
		}
	}
	// With the doors, from the places the account walks the pocket's neighbourhood: the lever's corridor, outside the gate, the
	// poison room. A few corner tiles of each rectangle, which a plan finds nothing to reach.
	for _, from := range []tile.Tile{questplan.CogLeverTile, questplan.PenGateOutside, questplan.PoisonSpawn} {
		for _, corner := range []tile.Tile{{X: 2579, Z: 9655}, {X: 2595, Z: 9660}, {X: 2593, Z: 9652}, {X: 2584, Z: 9654}} {
			if !questplan.InCogPen(corner) || !pk.Walkable(corner.X, corner.Z, 0) {
				t.Fatalf("%v is not a floor tile of the pen", corner)
			}
			if r := p.Plan(webwalk.Point{X: from.X, Z: from.Z}, webwalk.Point{X: corner.X, Z: corner.Z}, webwalk.Options{AllowSpecial: true, AllowNPC: true}); r.OK && r.End().X == corner.X && r.End().Z == corner.Z {
				t.Errorf("the pen's corner %v is reached from %v (cost %d)", corner, from, r.Cost)
			}
		}
	}
	// The rooms: the blue room's 18 tiles and the white room's 10.
	if blue := mcReachable(p, tile.Tile{X: 2572, Z: 9632}, 2564, 2582, 9622, 9640, true); len(blue) != 18 {
		t.Errorf("the blue room is %d tiles, the plan says 18 (x 2571-2575, z 9630-9633)", len(blue))
	} else {
		for _, b := range blue {
			if !questplan.InCogBlueRoom(b) {
				t.Errorf("%v is in the blue room and not in InCogBlueRoom", b)
			}
		}
	}
	if white := mcReachable(p, tile.Tile{X: 2575, Z: 9656}, 2570, 2584, 9650, 9662, true); len(white) != 10 {
		t.Errorf("the white room is %d tiles, the plan says 10 (x 2574-2578, z 9655-9658)", len(white))
	} else {
		for _, w := range white {
			if !questplan.InCogWhiteRoom(w) {
				t.Errorf("%v is in the white room and not in InCogWhiteRoom", w)
			}
		}
	}
	// Every spawn and stand of the plan is a floor tile.
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{
		{"the cave's foot", questplan.MonkCaveFoot}, {"the ring's stand", questplan.MonkRingStand},
		{"the poison's", questplan.PoisonSpawn}, {"the lever's", questplan.CogLeverTile}, {"the pen gate's inside", questplan.PenGateTile},
		{"the pen gate's outside", questplan.PenGateOutside}, {"the trough's stand", questplan.TroughStand}, {"the rat gate's", questplan.RatGateTile},
		{"the secret wall's stand", questplan.CogSecretWallStand}, {"the red cog's staging tile", questplan.RedCogStage}, {"Kojo's", questplan.KojoAnchor},
		{"Omad's", questplan.OmadAnchor}, {"Cedric's", questplan.CedricAnchor}, {"the well's stand", questplan.WellStand},
	} {
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) {
			t.Errorf("%s tile %d,%d,%d is not a walkable tile on the real pack", c.what, c.at.X, c.at.Z, c.at.Level)
		}
	}
	for _, c := range questplan.CogColours {
		if !pk.Walkable(c.Stand.X, c.Stand.Z, c.Stand.Level) || !pk.Walkable(c.Spawn.X, c.Spawn.Z, c.Spawn.Level) {
			t.Errorf("the %s cog's stand %v or spawn %v is not a floor tile", c.Name, c.Stand, c.Spawn)
		}
	}
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{{"the bucket's", questplan.CogBucketSpawn}, {"the blanket's", questplan.MonkBlanketSpawn}, {"the poison's", questplan.PoisonSpawn}} {
		if tiles := pk.InteractTiles(pathfind.NavPoint{X: c.at.X, Z: c.at.Z, Level: c.at.Level}); len(tiles) == 0 {
			t.Errorf("%s spawn at %v can be taken from no tile", c.what, c.at)
		}
	}
}

// The ogres' spawns lie where the guard thinks, in the cave the pocket test found, and the staging tile keeps the guard's distance
// from them.
func TestMonkCogTheOgresStandWhereTheGuardThinks(t *testing.T) {
	p, pt := mcPlanner(t)
	spawns := []tile.Tile{{X: 2585, Z: 9608}, {X: 2586, Z: 9614}, {X: 2588, Z: 9606}}
	for _, o := range spawns {
		if d := questplan.OgreDistance(o, questplan.RedCogStage); d < 6 {
			t.Errorf("the staging tile is only %d from the ogre at %v", d, o)
		}
		if r := p.Plan(pt(questplan.RedCogStage), pt(o), webwalk.Options{AllowSpecial: true, AllowNPC: true, WalkOnly: true}); !r.OK {
			t.Errorf("the ogre at %v cannot be reached from the staging tile: its spawn is not in the cave", o)
		}
	}
	// The way in to the cog from the mouth passes every ogre (the reason for the guard): the plan's route has a waypoint within
	// 3 of each spawn.
	r := p.Plan(pt(questplan.RedCogStage), pt(questplan.CogColours[3].Spawn), webwalk.Options{AllowSpecial: true, AllowNPC: true, WalkOnly: true})
	if !r.OK {
		t.Fatalf("no route from the mouth to the cog: %s", r.Reason)
	}
	for _, o := range spawns {
		near := false
		for _, w := range r.Waypoints {
			if questplan.OgreDistance(o, tile.Tile{X: w.X, Z: w.Z, Level: w.Level}) <= 3 {
				near = true
			}
		}
		if !near {
			t.Logf("the route to the cog keeps more than 3 tiles from the ogre spawned at %v", o)
		}
	}
}

// The ladder the ring of stones grows is a live loc: the plan's query finds it once the server has added it, and the engine accepts
// an op on it from the ring's stand.
func TestMonkCogTheRingsLadderIsFoundAndReachedOnceTheServerAddsIt(t *testing.T) {
	d := csDeps(t)
	q := locindex.Query{X: questplan.MonkRingStand.X, Z: questplan.MonkRingStand.Z, Radius: 3, IDs: []int{questplan.LocMonkRingLadder}, Op: "Climb-down"}
	if got := d.Locs.Find(d.Config, q, nil); len(got) != 0 {
		t.Fatalf("the ladder is in the static map: %+v", got)
	}
	// LOC_ADD_CHANGE: loc 1765, shape 10, rotation 1 at 2561,3222 (loc_add(^blanket_ladder_coord, wildymirrorladdertop1, 1, centrepiece_straight)).
	live := []locindex.Change{{X: 2561, Z: 3222, ID: questplan.LocMonkRingLadder, Shape: 10, Angle: 1}}
	got := d.Locs.Find(d.Config, q, live)
	if len(got) != 1 || !got[0].Live || got[0].X != 2561 || got[0].Z != 3222 || got[0].Op == 0 {
		t.Fatalf("the live ladder: %+v", got)
	}
	stands := questrunner.LocStands(d, got[0], questplan.MonkRingStand)
	found := false
	for _, s := range stands {
		if s == questplan.MonkRingStand {
			found = true
		}
	}
	if !found {
		t.Errorf("the ring's stand %v is not among the ladder's stands %v", questplan.MonkRingStand, stands)
	}
	// And 200 ticks later the server takes it away again: the query finds nothing.
	gone := []locindex.Change{{X: 2561, Z: 3222, ID: -1, Shape: 10, Deleted: true}}
	if got := d.Locs.Find(d.Config, q, gone); len(got) != 0 {
		t.Errorf("the ladder after it was deleted: %+v", got)
	}
}

// What the pen's gate does to the planner: closed, the pen is not reached from outside it; with the walls the server's changes
// leave (the gate deleted, its open leaf added one tile east, rotated), the step west through it is one tile. This is the walker's own
// overlay (locindex.WallEdits over pathfind.Pack.WithWalls), the check the module relies on for its fallback walk.
func TestMonkCogAnOpenPenGateIsAStepOnTheWallOverlay(t *testing.T) {
	pk, ix, cfg := csData(t)
	from, to := pathfind.NavPoint{X: 2596, Z: 9657}, pathfind.NavPoint{X: 2595, Z: 9657}
	if r := pk.FindPath(from, to); r.OK && len(r.Waypoints) <= 2 {
		t.Fatalf("the shut gate lets the pack's own path through: %+v", r.Waypoints)
	}
	changes := []gamestate.LocInstance{
		{Type: -1, Shape: 0, Rotate: 2, X: 2595, Z: 9657, Deleted: true},
		{Type: 1541, Shape: 0, Rotate: 3, X: 2596, Z: 9657},
	}
	edits := locindex.WallEdits(ix, cfg, changes)
	if len(edits) == 0 {
		t.Fatal("the gate's deletion makes no wall edit: the base loc is not in the index")
	}
	open := pk.WithWalls(edits)
	r := open.FindPath(from, to)
	if !r.OK || len(r.Waypoints) > 2 {
		t.Fatalf("the open gate is not one step on the overlay: %+v %s", r.Waypoints, r.Reason)
	}
	// The pen's far end is reached from outside on the overlay, and not on the shut pack.
	deep := pathfind.NavPoint{X: questplan.TroughStand.X, Z: questplan.TroughStand.Z}
	if r := pk.FindPath(from, deep); r.OK {
		t.Errorf("the trough is reached through the shut gate: %+v", r.Waypoints)
	}
	if r := open.FindPath(from, deep); !r.OK {
		t.Errorf("the trough is not reached through the open gate: %s", r.Reason)
	}
}
