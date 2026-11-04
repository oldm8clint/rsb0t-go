package questmods

// Plague City on the real data: the production config, the baked loc index, the collision pack and the web graph (RSB0T_CONFIG_JAG,
// RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them, through csData). The questions: whether every id the plan
// carries names what it says in the config the fleet's client reads, whether the customs' own loc queries find their locs where the
// plan says and each has a tile to stand on that the engine accepts the op from, whether every walk the quest asks for is a route
// the planner finds, whether the areas the decision tells apart are the regions the pack makes, whether the walks round the
// guard dogs keep their berth, and whether the places the quest's scripted ways cross (the wall, the plague house, the stairs) are
// the places the planner cannot cross on its own, which is what the module's own legs and its pocket are for.

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

func TestPlagueIdsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for _, c := range []struct {
		id       int
		name, op string
	}{
		{questplan.PlagueObjDwellberries, "Dwellberries", "Eat"}, {questplan.PlagueObjGasMask, "Gas mask", "Wear"}, {questplan.PlagueObjPicture, "Picture", ""},
		{questplan.PlagueObjBook, "Book", ""}, {questplan.PlagueObjNote, "A scruffy note", "Read"}, {questplan.PlagueObjHangoverCure, "Hangover cure", ""},
		{questplan.PlagueObjWarrant, "Warrant", ""}, {questplan.PlagueObjKey, "A small key", ""}, {questplan.PlagueObjScroll, "A magic scroll", "Read"},
		{questplan.PlagueObjPestle, "Pestle and mortar", ""}, {questplan.PlagueObjSnapeGrass, "Snape grass", ""}, {questplan.PlagueObjChocolateBar, "Chocolate bar", "Eat"},
		{questplan.PlagueObjChocolateDust, "Chocolate dust", ""}, {questplan.PlagueObjChocolateMilk, "Chocolaty milk", ""}, {questplan.PlagueObjShark, "Shark", "Eat"},
		{questplan.ObjSpade, "Spade", "Dig"}, {questplan.ObjRope, "Rope", ""}, {questplan.ObjBucket, "Bucket", ""}, {questplan.ObjBucketOfWater, "Bucket of water", ""},
		{questplan.ObjBucketOfMilk, "Bucket of milk", ""}, {questplan.CoinsID, "Coins", ""},
	} {
		o, ok := cfg.Obj(c.id)
		if !ok || o.Name != c.name || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", c.id, o.Name, ok, o.Noted(), c.name)
			continue
		}
		if c.op != "" && cachecfg.OpIndex(actions.HeldOps(o.IOps), c.op) == 0 {
			t.Errorf("obj %d (%s) offers %v, want a %q op", c.id, o.Name, o.IOps, c.op)
		}
	}
	for id, want := range map[int]string{
		questplan.PlagueNpcEdmond: "Edmond", questplan.PlagueNpcAlrena: "Alrena", questplan.PlagueNpcJethick: "Jethick", questplan.PlagueNpcTed: "Ted Rehnison",
		questplan.PlagueNpcMartha: "Martha Rehnison", questplan.PlagueNpcMilli: "Milli Rehnison", questplan.PlagueNpcClerk: "Clerk", questplan.PlagueNpcBravek: "Bravek",
		questplan.PlagueNpcElena: "Elena", questplan.PlagueNpcMourner: "Mourner", questplan.PlagueNpcJatix: "Jatix", questplan.PlagueNpcGuardDog: "Guard dog",
		questplan.PlagueNpcHobgob: "Hobgoblin", questplan.PlagueNpcHobgobSp: "Hobgoblin", questplan.NpcDairyCow: "Cow",
		questplan.AemadStore.NpcID: "Aemad", questplan.WydinShop.NpcID: "Wydin", questplan.KhazardGeneralStore.NpcID: "Shop keeper",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (found %v), the plan says %q", id, n.Name, ok, want)
		}
	}
	if n, _ := cfg.Npc(questplan.PlagueNpcGuardDog); n.Level != 44 {
		t.Errorf("the guard dog is level %d", n.Level)
	}
	if n, _ := cfg.Npc(questplan.PlagueNpcHobgob); n.Level != 28 {
		t.Errorf("the hobgoblin is level %d", n.Level)
	}
	for _, c := range []struct {
		id       int
		name, op string
	}{
		{questplan.PlagueLocMudPatch1, "Mud patch", ""}, {questplan.PlagueLocMudPatch2, "Mud patch", ""}, {questplan.PlagueLocMudPile, "Mud pile", "Climb"},
		{questplan.PlagueLocSewerPipe, "Sewer pipe", "Search"}, {questplan.PlagueLocManholeShut, "Manhole", "Open"}, {questplan.PlagueLocManholeOpen, "Manhole", "Climb-down"},
		{questplan.PlagueLocCupboardShut, "Cupboard", "Open"}, {questplan.PlagueLocCupboardOpen, "Cupboard", "Search"}, {questplan.PlagueLocRehnisonDoor, "Door", "Open"},
		{questplan.PlagueLocRehnisonStairs, "Stairs", "Walk-up"}, {questplan.PlagueLocRehnisonTop, "Stairs", "Walk-down"}, {questplan.PlagueLocHouseDoor, "Door", "Open"},
		{questplan.PlagueLocBarrel, "Barrel", "Search"}, {questplan.PlagueLocHouseStairs, "Spooky stairs", "Walk-Down"}, {questplan.PlagueLocCellarStairs, "Spooky stairs", "Walk-Up"},
		{questplan.PlagueLocElenaGate, "Door", "Open"}, {questplan.PlagueLocBravekDoor, "Door", "Open"}, {questplan.PlagueLocFountain, "Fountain", ""},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name {
			t.Errorf("loc %d is %q (found %v), the plan says %q", c.id, l.Name, ok, c.name)
			continue
		}
		if c.op != "" && cachecfg.OpIndex(l.Ops, c.op) == 0 {
			t.Errorf("loc %d (%s) offers %v, want %q", c.id, l.Name, l.Ops, c.op)
		}
	}
	// The cupboard is searched from the east only.
	if l, _ := cfg.Loc(questplan.PlagueLocCupboardShut); l.ForceApproach == 0 {
		t.Error("Alrena's cupboard has no forced approach in the config")
	}
}

// The customs' own loc queries find their locs where the plan says, and each has a tile the engine accepts an op from.
func TestPlagueLocQueriesFindTheirLocsAndTheirStands(t *testing.T) {
	d := csDeps(t)
	for _, c := range []struct {
		what      string
		id        int
		name, op  string
		near      tile.Tile
		radius    int
		x, z, lvl int
		stands    []tile.Tile // tiles the plan relies on, each of which must be among the accepted stands
	}{
		{"the mud patch", 0, "Mud patch", "", questplan.PlagueGarden, 6, 2566, 3331, 0, nil},
		{"the market fountain", questplan.PlagueLocFountain, "", "", questplan.PlagueFountain, 8, 2629, 3311, 0, nil},
		{"Alrena's cupboard", questplan.PlagueLocCupboardShut, "", "Open", questplan.PlagueCupboard, 3, 2574, 3334, 0, []tile.Tile{{X: 2575, Z: 3334}}},
		{"the mud pile", questplan.PlagueLocMudPile, "", "Climb", questplan.PlagueMudPileStand, 4, 2561, 9738, 0, []tile.Tile{questplan.PlagueMudPileStand}},
		{"the sewer pipe", questplan.PlagueLocSewerPipe, "", "Search", questplan.PlagueSewerPipeEnd, 6, 2530, 9701, 0, []tile.Tile{questplan.PlagueSewerPipeEnd}},
		{"the manhole", questplan.PlagueLocManholeShut, "", "Open", questplan.PlagueManhole, 3, 2529, 3303, 0, []tile.Tile{questplan.PlagueWestLanding}},
		{"the Rehnisons' door", questplan.PlagueLocRehnisonDoor, "", "Open", questplan.PlagueRehnisonDoor, 3, 2531, 3328, 0,
			[]tile.Tile{questplan.PlagueRehnisonDoor, {X: 2531, Z: 3329}}},
		{"the Rehnisons' stairs", questplan.PlagueLocRehnisonStairs, "", "Walk-up", questplan.PlagueRehnisonStairs, 5, 2527, 3332, 0, nil},
		{"the stairs' top", questplan.PlagueLocRehnisonTop, "", "Walk-down", questplan.PlagueRehnisonTop, 8, 2527, 3332, 1, nil},
		{"the plague house's door", questplan.PlagueLocHouseDoor, "", "Open", questplan.PlagueHouseDoor, 3, 2533, 3272, 0,
			[]tile.Tile{questplan.PlagueHouseDoor, questplan.PlagueHouseInside}},
		{"the barrel", questplan.PlagueLocBarrel, "", "Search", questplan.PlagueBarrel, 5, 2534, 3268, 0, nil},
		{"the house's stairs down", questplan.PlagueLocHouseStairs, "", "Walk-Down", questplan.PlagueHouseStairs, 5, 2536, 3268, 0, nil},
		{"the cellar's stairs up", questplan.PlagueLocCellarStairs, "", "Walk-Up", questplan.PlagueCellarStairs, 4, 2536, 9671, 0, nil},
		{"the cell's gate", questplan.PlagueLocElenaGate, "", "Open", questplan.PlagueElenaGate, 3, 2539, 9672, 0,
			[]tile.Tile{questplan.PlagueElenaGate, {X: 2540, Z: 9672}}},
		{"Bravek's door", questplan.PlagueLocBravekDoor, "", "Open", questplan.PlagueBravekDoor, 4, 2530, 3314, 0,
			[]tile.Tile{questplan.PlagueBravekDoor, {X: 2529, Z: 3314}}},
	} {
		m, ok := questrunner.FindLoc(d, c.id, c.name, c.op, c.near, c.radius)
		if !ok {
			t.Errorf("%s: no loc found near %d,%d,%d", c.what, c.near.X, c.near.Z, c.near.Level)
			continue
		}
		if c.id != 0 && (m.X != c.x || m.Z != c.z || m.Level != c.lvl) {
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
	// The patch is one of three, all within reach of the one stand south of them.
	for _, z := range []int{3331, 3332, 3333} {
		if m, ok := questrunner.FindLoc(d, 0, "Mud patch", "", tile.Tile{X: 2566, Z: z}, 0); !ok || m.X != 2566 || m.Z != z {
			t.Errorf("no mud patch at 2566,%d (found %+v %v)", z, m, ok)
		}
	}
}

// Every walk the quest asks for is a route the planner finds on the real pack and graph.
func TestPlagueLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	bank := tile.Tile{X: 2616, Z: 3332}
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		// East of the wall.
		{"bank to Edmond", bank, questplan.PlagueEdmondAnchor}, {"bank to Alrena", bank, questplan.PlagueAlrenaAnchor},
		{"bank to the spade", bank, questplan.PlagueSpadeSpawn}, {"bank to the picture", bank, questplan.PlaguePictureSpawn},
		{"bank to Alrena's cupboard", bank, tile.Tile{X: 2575, Z: 3334}}, {"bank to the garden", bank, questplan.PlagueGarden},
		{"bank to the fountain", bank, questplan.PlagueFountain}, {"the fountain to the garden", questplan.PlagueFountain, questplan.PlagueGarden},
		{"the garden to the fountain", questplan.PlagueGarden, questplan.PlagueFountain}, {"bank to Aemad", bank, questplan.AemadStore.Anchor},
		{"bank to the bucket by Cedric's cart", bank, questplan.CogBucketSpawn}, {"the bucket to the garden", questplan.CogBucketSpawn, questplan.PlagueGarden},
		{"bank to Port Khazard's store", bank, questplan.KhazardGeneralStore.Anchor}, {"bank to the cows", bank, questplan.PlagueCowField},
		{"the cows to the garden", questplan.PlagueCowField, questplan.PlagueGarden}, {"bank to Jatix", bank, questplan.PlagueJatixShop.Anchor},
		{"Jatix to the first snape grass", questplan.PlagueJatixShop.Anchor, questplan.PlagueGrassSpawns[0]},
		{"bank to the first snape grass", bank, questplan.PlagueGrassSpawns[0]}, {"bank to Wydin", bank, questplan.WydinShop.Anchor},
		{"the grass to Falador's west bank", questplan.PlagueGrassSpawns[0], plagueGrassExit}, {"Wydin to the first snape grass", questplan.WydinShop.Anchor, questplan.PlagueGrassSpawns[0]},
		{"the grass to Wydin", questplan.PlagueGrassSpawns[0], questplan.WydinShop.Anchor}, {"the exit to the other grass", plagueGrassExit, questplan.PlagueGrassSpawns[1]},
		{"the berries from the bank", bank, questplan.PlagueBerrySpots[0]}, {"the berries back", questplan.PlagueBerrySpots[0], bank},
		// Under the garden.
		{"the dig's landing to the pipe's end", questplan.PlagueSewerLanding, questplan.PlagueSewerPipeEnd},
		{"the dig's landing to Edmond", questplan.PlagueSewerLanding, questplan.PlagueSewerEdmond},
		{"the pipe's end to Edmond", questplan.PlagueSewerPipeEnd, questplan.PlagueSewerEdmond},
		{"the pipe's end to the mud pile", questplan.PlagueSewerPipeEnd, questplan.PlagueMudPileStand},
		{"Edmond to the mud pile", questplan.PlagueSewerEdmond, questplan.PlagueMudPileStand},
		// West Ardougne.
		{"the pipe's landing to Jethick", questplan.PlagueWestLanding, questplan.PlagueJethickAnchor},
		{"the pipe's landing to the Rehnisons' door", questplan.PlagueWestLanding, questplan.PlagueRehnisonDoor},
		{"Jethick to the Rehnisons' door", questplan.PlagueJethickAnchor, questplan.PlagueRehnisonDoor},
		{"the pipe's landing to the plague house's door", questplan.PlagueWestLanding, questplan.PlagueHouseDoor},
		{"Jethick to the plague house's door", questplan.PlagueJethickAnchor, questplan.PlagueHouseDoor},
		{"the pipe's landing to the clerk", questplan.PlagueWestLanding, questplan.PlagueClerkAnchor},
		{"the clerk to Bravek's door", questplan.PlagueClerkAnchor, questplan.PlagueBravekDoor},
		{"Bravek's room to the plague house's door", questplan.PlagueBravekAnchor, questplan.PlagueHouseDoor},
		{"the plague house's door to the pipe's landing", questplan.PlagueHouseDoor, questplan.PlagueWestLanding},
		{"the Rehnisons' room to the manhole", questplan.PlagueTedAnchor, questplan.PlagueWestLanding},
		// Inside the plague house and under it.
		{"the doorstep to the barrel", questplan.PlagueHouseInside, questplan.PlagueBarrel},
		{"the doorstep to the stairs", questplan.PlagueHouseInside, questplan.PlagueHouseStairs},
		{"the cellar's landing to the cell's gate", questplan.PlagueCellarLanding, questplan.PlagueElenaGate},
		{"the cellar's landing to its stairs up", questplan.PlagueCellarLanding, questplan.PlagueCellarStairs},
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

// The places the module crosses by script are the places the planner cannot cross on its own: this is why each has a leg, and why
// the sewer, the city, the upper floor and the cellar are a pocket. If a re-bake ever makes one of them reachable, the walker would
// take it and the leg's own door or stairs would be the wrong tool; this says so.
func TestPlagueTheScriptedWaysAreNotOnTheGraph(t *testing.T) {
	p, pt := mcPlanner(t)
	bank := tile.Tile{X: 2616, Z: 3332}
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the city to the mainland", questplan.PlagueWestLanding, bank},
		{"the Jethick's street to the mainland", questplan.PlagueJethickAnchor, bank},
		{"the sewer's pipe end to the mainland (the mud pile lands on a tile the pack calls blocked)", questplan.PlagueSewerPipeEnd, bank},
		{"the mud pile to the mainland", questplan.PlagueMudPileStand, bank},
		{"the plague house's doorstep to the street", questplan.PlagueHouseInside, tile.Tile{X: 2533, Z: 3275}},
		{"the Rehnisons' ground floor to the upper floor", tile.Tile{X: 2528, Z: 3331}, tile.Tile{X: 2528, Z: 3331, Level: 1}},
		{"the upper floor to the street", questplan.PlagueMilliAnchor, questplan.PlagueWestLanding},
		{"the cellar to the doorstep", questplan.PlagueCellarLanding, questplan.PlagueHouseInside},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true}); r.OK {
			t.Errorf("%s: the planner finds a route (cost %d, hops %d): the scripted way is not the only way", l.name, r.Cost, len(r.Hops()))
		}
	}
	// The graph does have the way IN, which only a spade and a gas mask in the pack pass: the baked edges of a ported TS walk. They are
	// gated by items, and the module never asks for a destination across the wall from the mainland.
	r := p.Plan(pt(questplan.PlagueGarden), pt(questplan.PlagueWestLanding), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	if !r.OK || len(r.Hops()) != 2 {
		t.Fatalf("the garden to the pipe's landing: %v (%d hops)", r.OK, len(r.Hops()))
	}
	for i, want := range []struct{ loc, action string }{{"Mud patch", "Dig"}, {"Sewer pipe", "Search"}} {
		if h := r.Hops()[i]; h.LocName != want.loc || h.Action != want.action || h.Requires == nil || len(h.Requires.Items) != 1 {
			t.Errorf("hop %d is %+v, want an item-gated %s %s", i, h, want.loc, want.action)
		}
	}
}

// The areas the decision tells apart are the pack's components: every walkable tile the planner reaches on foot from the dig's landing
// lies in the sewer box, from the cellar's landing in the cellar box, from the pipe's landing in the city, and the mainland the
// planner reaches from Edmond's door is never in the city. The pocket's rectangles are what Travel leaves by.
func TestPlagueAreasAreTheCollisionPacksComponents(t *testing.T) {
	pk, _, _ := csData(t)
	flood := func(at tile.Tile, r int) map[[2]int]bool {
		return pk.Flood(pathfind.NavPoint{X: at.X, Z: at.Z, Level: at.Level}, r)
	}
	check := func(name string, at tile.Tile, r int, want questplan.PlagueArea, minTiles int) {
		t.Helper()
		f := flood(at, r)
		if len(f) < minTiles {
			t.Errorf("%s: %d tiles reached, the plan says at least %d", name, len(f), minTiles)
		}
		for k := range f {
			if got := questplan.PlagueAreaOf(tile.Tile{X: k[0], Z: k[1], Level: at.Level}); got != want {
				t.Errorf("%s: %v is reached on foot and the plan calls it %v, not %v", name, k, got, want)
				return
			}
		}
	}
	check("the sewer", questplan.PlagueSewerLanding, 150, questplan.PlagueSewer, 690)
	check("the cell", questplan.PlagueElenaAnchor, 20, questplan.PlagueCellar, 9)
	check("the cellar's landing", questplan.PlagueCellarLanding, 20, questplan.PlagueCellar, 12)
	check("the city", questplan.PlagueWestLanding, 160, questplan.PlagueWest, 4500)
	check("the Rehnisons' upper floor", tile.Tile{X: 2530, Z: 3331, Level: 1}, 20, questplan.PlagueUpstairs, 20)
	// The mainland round Edmond (and the whole of the free ground south and west of the city) is never the city.
	east := flood(questplan.PlagueEdmondAnchor, 120)
	for k := range east {
		if got := questplan.PlagueAreaOf(tile.Tile{X: k[0], Z: k[1]}); got != questplan.PlagueEast {
			t.Errorf("%v is reached on foot from Edmond's door and the plan calls it %v", k, got)
			break
		}
	}
	if len(east) < 20000 {
		t.Errorf("only %d tiles reached from Edmond", len(east))
	}
	// The rooms: the plague house's ground floor, the Rehnisons' ground floor and Bravek's room are what the executors' boxes say.
	for _, c := range []struct {
		name string
		at   tile.Tile
		in   func(tile.Tile) bool
		n    int
	}{
		{"the plague house", questplan.PlagueHouseInside, questplan.InPlagueHouse, 25},
		{"the Rehnisons' ground floor", tile.Tile{X: 2531, Z: 3331}, plagueInRehnisons, 19},
		{"Bravek's room", questplan.PlagueBravekAnchor, plagueInBravek, 28},
	} {
		f := flood(c.at, 40)
		if len(f) != c.n {
			t.Errorf("%s is %d tiles, the plan says %d", c.name, len(f), c.n)
		}
		for k := range f {
			if !c.in(tile.Tile{X: k[0], Z: k[1]}) {
				t.Errorf("%s: %v is reached on foot and is outside the executor's box", c.name, k)
			}
		}
	}
	// And the rest of the places the plan names are floor.
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{
		{"the garden", questplan.PlagueGarden}, {"the dig's landing", questplan.PlagueSewerLanding}, {"the mud pile's stand", questplan.PlagueMudPileStand},
		{"the pipe's end", questplan.PlagueSewerPipeEnd}, {"Edmond's sewer tile", questplan.PlagueSewerEdmond}, {"the pipe's landing", questplan.PlagueWestLanding},
		{"the Rehnisons' door", questplan.PlagueRehnisonDoor}, {"the plague house's door", questplan.PlagueHouseDoor}, {"its doorstep", questplan.PlagueHouseInside},
		{"the cellar's landing", questplan.PlagueCellarLanding}, {"Elena's tile", questplan.PlagueElenaAnchor}, {"the cow field", questplan.PlagueCowField},
		{"the snape grass 2", questplan.PlagueGrassSpawns[1]}, {"the berries", questplan.PlagueBerrySpots[0]}, {"the exit", plagueGrassExit},
	} {
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) {
			t.Errorf("%s tile %v is not walkable on the real pack", c.what, c.at)
		}
	}
	// The garden tile the mud pile lands on is blocked in the pack (the patch loc): why the sewer is a pocket the pack's walks never leave.
	if pk.Walkable(2566, 3331, 0) {
		t.Error("the mud patch's tile is walkable in the pack now: the sewer may be left on foot")
	}
}

// route tiles of a plan, corner to corner.
func plagueRouteTiles(r webwalk.Route) []tile.Tile {
	var out []tile.Tile
	for _, leg := range r.Legs() {
		if leg.Hop != nil || len(leg.Tiles) == 0 {
			continue
		}
		for i := 0; i+1 < len(leg.Tiles); i++ {
			a, b := leg.Tiles[i], leg.Tiles[i+1]
			sg := func(v int) int {
				switch {
				case v > 0:
					return 1
				case v < 0:
					return -1
				}
				return 0
			}
			dx, dz := sg(b.X-a.X), sg(b.Z-a.Z)
			x, z := a.X, a.Z
			for {
				out = append(out, tile.Tile{X: x, Z: z, Level: a.Level})
				if x == b.X && z == b.Z {
					break
				}
				if x != b.X {
					x += dx
				}
				if z != b.Z {
					z += dz
				}
			}
		}
	}
	return out
}

func plagueClosest(ts []tile.Tile, spawns []tile.Tile) (int, tile.Tile) {
	best, at := 1<<30, tile.Tile{}
	for _, q := range ts {
		for _, d := range spawns {
			if c := tile.Chebyshev(q, d); c < best {
				best, at = c, q
			}
		}
	}
	return best, at
}

// McGrubor's Wood has two gaps, each held by a guard dog, and no route enters it that keeps four tiles from every dog's spawn except
// the one to the east side's berry: the single spot the module uses. With a berth of three tiles round every spawn (the walker's
// rectangles) that route plans, out and back, and passes four from the nearest dog (a dog roams three and notices at one);
// without the rectangles the shortest way to the same berry passes a dog at one tile, and the other four spawns cannot be reached
// at the berth at all.
func TestPlagueBerryRouteKeepsClearOfTheDogs(t *testing.T) {
	p, pt := mcPlanner(t)
	bank := tile.Tile{X: 2616, Z: 3332}
	zones := plagueZones(questplan.PlagueGuardDogs, plagueDogBerth)
	spot := questplan.PlagueBerrySpots[0]
	in := p.Plan(pt(bank), pt(spot), webwalk.Options{AllowSpecial: true, AllowNPC: true, AvoidZones: zones})
	out := p.Plan(pt(spot), pt(bank), webwalk.Options{AllowSpecial: true, AllowNPC: true, AvoidZones: zones})
	if !in.OK || !out.OK {
		t.Fatalf("the berries at the berth: in %v (%s), out %v (%s)", in.OK, in.Reason, out.OK, out.Reason)
	}
	for name, r := range map[string]webwalk.Route{"in": in, "out": out} {
		if d, at := plagueClosest(plagueRouteTiles(r), questplan.PlagueGuardDogs); d < questplan.PlagueDogReach {
			t.Errorf("the way %s passes %d tiles from a dog's spawn, at %v: under its reach of %d", name, d, at, questplan.PlagueDogReach)
		}
	}
	if d, _ := plagueClosest([]tile.Tile{spot}, questplan.PlagueGuardDogs); d < questplan.PlagueDogBerth {
		t.Errorf("the berry is %d from a dog", d)
	}
	plain := p.Plan(pt(bank), pt(spot), webwalk.Options{AllowSpecial: true, AllowNPC: true})
	if !plain.OK {
		t.Fatal("no plain route to the berries")
	}
	if d, _ := plagueClosest(plagueRouteTiles(plain), questplan.PlagueGuardDogs); d > 1 {
		t.Logf("the plain route passes %d from a dog (the zones are still the walk's rule)", d)
	}
	// The other four spawns of the wood cannot be reached at the berth: the reason the module uses one.
	for _, other := range []tile.Tile{{X: 2633, Z: 3496}, {X: 2638, Z: 3479}, {X: 2645, Z: 3498}, {X: 2664, Z: 3476}} {
		if r := p.Plan(pt(bank), pt(other), webwalk.Options{AllowSpecial: true, AllowNPC: true, AvoidZones: zones}); r.OK {
			t.Errorf("the berry at %v is reached at the berth (cost %d): the module could use it", other, r.Cost)
		}
	}
	// A hop is never inside a zone (the loose railing at the wood's north east is the way in).
	for _, h := range in.Hops() {
		for _, z := range zones {
			if z.Contains(webwalk.Point{X: h.From.X, Z: h.From.Z}) || z.Contains(webwalk.Point{X: h.To.X, Z: h.To.Z}) {
				t.Errorf("hop %q from %v to %v is inside a dog's rectangle", h.LocName, h.From, h.To)
			}
		}
	}
}

// East Ardougne's market holds four guard dogs between the bank and the fountain, one a tile from the shortest way. The water walks are
// planned round them: from the bank and back the route keeps its berth, and from the garden (the usual start) no detour is needed.
func TestPlagueFountainWalksKeepClearOfTheMarketDogs(t *testing.T) {
	p, pt := mcPlanner(t)
	bank := tile.Tile{X: 2616, Z: 3332}
	zones := plagueZones(questplan.PlagueMarketDogs, plagueDogBerth)
	if plain := p.Plan(pt(bank), pt(questplan.PlagueFountain), webwalk.Options{AllowSpecial: true, AllowNPC: true}); plain.OK {
		if d, at := plagueClosest(plagueRouteTiles(plain), questplan.PlagueMarketDogs); d > 2 {
			t.Errorf("the plain way from the bank to the fountain passes %d from a dog (at %v): the zones are not what keeps it clear any more", d, at)
		}
	}
	for _, c := range []struct {
		name     string
		from, to tile.Tile
		radius   int
	}{
		{"bank to the fountain", bank, questplan.PlagueFountain, 2}, {"the fountain to the bank", questplan.PlagueFountain, bank, 0},
		{"the garden to the fountain", questplan.PlagueGarden, questplan.PlagueFountain, 2},
		{"the fountain to the garden", questplan.PlagueFountain, questplan.PlagueGarden, 1},
	} {
		r := p.Plan(pt(c.from), pt(c.to), webwalk.Options{AllowSpecial: true, AllowNPC: true, AvoidZones: zones})
		if !r.OK {
			t.Errorf("%s: no route round the dogs (%s)", c.name, r.Reason)
			continue
		}
		if d, at := plagueClosest(plagueRouteTiles(r), questplan.PlagueMarketDogs); d < questplan.PlagueDogReach {
			t.Errorf("%s passes %d tiles from a dog's spawn, at %v", c.name, d, at)
		}
	}
	// The market's dogs are reached on foot from the fountain: they are not fenced in.
	pk, _, _ := csData(t)
	fl := pk.Flood(pathfind.NavPoint{X: 2629, Z: 3309}, 60)
	for _, dog := range questplan.PlagueMarketDogs {
		if !fl[[2]int{dog.X, dog.Z}] {
			t.Errorf("the dog at %v cannot be reached on foot from the fountain: the zones are not needed for it", dog)
		}
	}
}

// Every leg a custom step walks ends on a tile an op can be sent from, and the ground spawns can be taken from some tile.
func TestPlagueGroundSpawnsAndExitsCanBeReached(t *testing.T) {
	pk, _, _ := csData(t)
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{
		{"the spade", questplan.PlagueSpadeSpawn}, {"the picture", questplan.PlaguePictureSpawn}, {"snape grass 1", questplan.PlagueGrassSpawns[0]},
		{"snape grass 2", questplan.PlagueGrassSpawns[1]}, {"snape grass 3", questplan.PlagueGrassSpawns[2]}, {"the bucket", questplan.CogBucketSpawn},
		{"the dwellberries", questplan.PlagueBerrySpots[0]},
	} {
		if tiles := pk.InteractTiles(pathfind.NavPoint{X: c.at.X, Z: c.at.Z, Level: c.at.Level}); len(tiles) == 0 {
			t.Errorf("the %s spawn at %v can be taken from no tile", c.what, c.at)
		}
	}
	// The three snape grass spawns are the order of preference the module takes them in: the first is furthest from the armed hobgoblin.
	armed := tile.Tile{X: 2909, Z: 3294}
	best := 0
	for i, s := range questplan.PlagueGrassSpawns {
		d := tile.Chebyshev(s, armed)
		if i == 0 {
			best = d
		} else if d > best {
			t.Errorf("snape grass spawn %d (%v) is further from the armed hobgoblin than the first", i, s)
		}
	}
}
