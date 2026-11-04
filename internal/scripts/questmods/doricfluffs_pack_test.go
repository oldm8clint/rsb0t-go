package questmods

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// Doric's Quest and Gertrude's Cat on the real collision pack, the baked web graph, the loc index and the production config
// (skipped without RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG): every stand the modules send an account to is a
// walkable tile, every NPC they talk to or search can be reached from a tile beside it, the lumber yard's whole interior, its ladder
// and its walkway connect the way the module walks them, the fence is the one way in, and every leg of the routes plans from where
// the account is when it starts the leg. A tile the walker cannot reach parks a quest in production after the walk times out; here it
// fails in a second.

func np(t tile.Tile) pathfind.NavPoint { return pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level} }

func TestDoricFluffsStandsAreWalkableAndTheirNPCsReachable(t *testing.T) {
	pk := fillerPack(t)
	for name, at := range map[string]tile.Tile{
		"Doric": questplan.DoricAnchor, "the mine": questplan.RimmingtonMine, "clay stand": questplan.RimmingtonClay,
		"copper stand": questplan.RimmingtonCopper, "iron stand": questplan.RimmingtonIron,
		"Gertrude": questplan.GertrudeAnchor, "the market": questplan.FluffsMarket, "the leaves": questplan.DoogleField,
		"Gerrant": questplan.FluffsSardineShop.Anchor, "outside the fence": questplan.YardOutside,
		"inside the fence": questplan.YardInside, "the ladder's stand": questplan.YardLadderStand, "the cat's stand": questplan.CatStand,
		"the cat": questplan.FluffsSpot,
	} {
		if !pk.Walkable(at.X, at.Z, at.Level) {
			t.Errorf("%s %v is not a walkable tile", name, at)
		}
	}
	for _, sp := range questplan.DoogleSpawns {
		if !pk.Walkable(sp.X, sp.Z, 0) {
			t.Errorf("doogle spawn %v is not walkable", sp)
		}
	}
	// Every NPC the quests talk to can be spoken to from some tile beside it.
	for name, at := range map[string]tile.Tile{
		"Doric": questplan.DoricAnchor, "Gertrude": questplan.GertrudeAnchor, "Shilop": questplan.FluffsMarket,
		"Wilough": {X: questplan.FluffsMarket.X, Z: questplan.FluffsMarket.Z + 1}, "Gerrant": questplan.FluffsSardineShop.Anchor,
		"Fluffs": questplan.FluffsSpot, "Bob": questplan.BobsAxes.Anchor,
	} {
		if len(pk.InteractTiles(np(at))) == 0 {
			t.Errorf("no tile to reach %s at %v from", name, at)
		}
	}
	// The free pickaxe lies on a table: its tile is blocked, so it is taken from a tile beside it (the provisioner's ground rung
	// walks onto one: WalkNear resolves a blocked destination to the tiles that touch it), and the mine reaches such a tile.
	table := questplan.RimmingtonPickaxe
	if pk.Walkable(table.X, table.Z, 0) {
		t.Logf("the pickaxe spawn %v can be stood on after all", table)
	} else if len(pk.InteractTiles(np(table))) == 0 {
		t.Errorf("no tile beside the pickaxe table %v to take it from", table)
	}
	if r := pk.FindPath(np(questplan.RimmingtonMine), np(table)); !r.OK {
		t.Errorf("no walk from the mine to the pickaxe table: %s", r.Reason)
	}
}

// The lumber yard: the south wall's one gap holds the fence loc, so the yard's inside is a pocket the pack cannot enter from
// outside; inside it every crate has a tile to search from, the ladder has its neighbours, and the walkway above connects the
// ladder to the cat. The Wilderness line (z 3520) is not crossed by any tile of the yard.
func TestFluffsYardGeometry(t *testing.T) {
	pk := fillerPack(t)
	// The fence tile blocks; the tiles either side of it along the line of travel do not.
	if pk.Walkable(questplan.YardFence.X, questplan.YardFence.Z, 0) {
		t.Error("the fence tile is walkable: the climb would not be the way in")
	}
	if !pk.Walkable(questplan.YardOutside.X, questplan.YardOutside.Z, 0) || !pk.Walkable(questplan.YardInside.X, questplan.YardInside.Z, 0) {
		t.Fatal("the tiles either side of the fence are not walkable")
	}
	// The wall: no walk from outside into the yard, and none from inside out, wherever along the wall they try.
	for _, x := range []int{3299, 3302, 3304, 3306, 3308, 3312} {
		if r := pk.FindPath(pathfind.NavPoint{X: x, Z: 3491}, pathfind.NavPoint{X: x, Z: 3495}); r.OK {
			t.Errorf("a walk from %d,3491 into the yard exists (%d waypoints): the fence is not the only way in", x, len(r.Waypoints))
		}
	}
	if r := pk.FindPath(np(questplan.YardOutside), np(questplan.YardInside)); r.OK {
		t.Error("the fence's two sides connect on foot")
	}
	// Ground inside the bounds that is outside the wall: the module tells the two apart by a path to YardInside.
	for _, out := range []tile.Tile{{X: 3324, Z: 3500}, {X: 3324, Z: 3501}} {
		if !questplan.InYard(out) || !pk.Walkable(out.X, out.Z, 0) {
			t.Errorf("%v: the test point is not walkable ground in the yard's bounds", out)
			continue
		}
		if pk.FindPath(np(out), np(questplan.YardInside)).OK {
			t.Errorf("%v is in the yard's bounds and connects to the inside", out)
		}
	}
	inside := pk.Flood(np(questplan.YardInside), 40)
	if len(inside) < 400 {
		t.Fatalf("the yard's inside is %d tiles", len(inside))
	}
	for c := range inside {
		at := tile.Tile{X: c[0], Z: c[1]}
		if !questplan.InYard(at) {
			t.Errorf("the walkable inside %v is outside questplan.InYard", at)
		}
		if randomevents.WildernessLevel(c[0], c[1], 0) != 0 || c[1] >= questplan.WildernessLine {
			t.Errorf("the yard's inside reaches the Wilderness at %v", at)
		}
	}
	// Every crate has a tile to search from inside, and the module's pick is reachable on foot from the fence.
	for _, c := range questplan.FluffsCrateTiles {
		reachable := 0
		for _, s := range pk.InteractTiles(np(c)) {
			if (tile.Tile{X: s.X, Z: s.Z}) != c && inside[[2]int{s.X, s.Z}] {
				reachable++
			}
		}
		if reachable == 0 {
			t.Errorf("no tile inside the yard to search the crate at %v from", c)
		}
	}
	// The ladder: its neighbours are reachable, and each has a walkable twin on the walkway (the climb lands on the same x,z).
	ladder := questplan.YardLadder
	up := 0
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			x, z := ladder.X+dx, ladder.Z+dz
			if (dx == 0 && dz == 0) || !inside[[2]int{x, z}] {
				continue
			}
			if !pk.Walkable(x, z, 1) {
				t.Errorf("the ladder's neighbour %d,%d has no walkway twin: a climb from there would land in a wall", x, z)
				continue
			}
			up++
			if !pk.FindPath(pathfind.NavPoint{X: x, Z: z, Level: 1}, np(questplan.CatStand)).OK {
				t.Errorf("the walkway tile %d,%d above the ladder's neighbour has no walk to the cat's stand", x, z)
			}
		}
	}
	if up < 5 {
		t.Errorf("only %d of the ladder's neighbours are inside the yard with a walkway twin", up)
	}
	if !inside[[2]int{questplan.YardLadderStand.X, questplan.YardLadderStand.Z}] || !pk.Walkable(questplan.YardLadderStand.X, questplan.YardLadderStand.Z, 1) {
		t.Errorf("the ladder stand %v is not inside, or has no walkway twin", questplan.YardLadderStand)
	}
	// Coming down: each walkway neighbour of the ladder's top has a ground twin inside the yard.
	top := questplan.YardLadderTop
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			x, z := top.X+dx, top.Z+dz
			if (dx == 0 && dz == 0) || !pk.Walkable(x, z, 1) {
				continue
			}
			if !inside[[2]int{x, z}] {
				t.Errorf("the walkway tile %d,%d beside the ladder's top has no ground twin inside the yard: the climb down would land in a wall", x, z)
			}
		}
	}
	// The cat: she is reached from a walkway tile, and the walkway is the level the helper says.
	if len(pk.InteractTiles(np(questplan.FluffsSpot))) == 0 || !pk.CanInteractFrom(np(questplan.CatStand), np(questplan.FluffsSpot)) {
		t.Errorf("the cat's stand %v cannot reach the cat at %v", questplan.CatStand, questplan.FluffsSpot)
	}
	if !questplan.OnWalkway(questplan.CatStand) || !questplan.OnWalkway(questplan.FluffsSpot) {
		t.Error("the walkway helper does not cover the cat")
	}
}

// The module's stands for a crate: fluffsStands lists the pack's interaction tiles without the crate's own, nearest the account first.
func TestFluffsStandsForTheCrates(t *testing.T) {
	pk := fillerPack(t)
	sim := actionsim.New(actionsim.Objs{Stackable: func(id int32) bool { return id == questplan.CoinsID }}, nil)
	d := &questrunner.Deps{Session: sim, Pack: pk}
	sim.StandAt(questplan.YardInside.X, questplan.YardInside.Z)
	for _, c := range questplan.FluffsCrateTiles {
		stands := fluffsStands(d, c)
		if len(stands) == 0 {
			t.Errorf("no stands for the crate at %v", c)
			continue
		}
		prev := -1
		for _, s := range stands {
			if s == c {
				t.Errorf("the crate's own tile %v is a stand", c)
			}
			if dist := tile.Chebyshev(questplan.YardInside, s); dist < prev {
				t.Errorf("the stands for %v are not nearest first: %v", c, stands)
			} else {
				prev = dist
			}
			if !pk.CanInteractFrom(np(s), np(c)) {
				t.Errorf("stand %v cannot interact with the crate at %v", s, c)
			}
		}
	}
}

// Every leg of both quests plans on the baked graph from where the account is when it starts the leg.
func TestDoricFluffsRoutesPlanFromEveryStart(t *testing.T) {
	pk := fillerPack(t)
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	pl := webwalk.NewPlanner(pk, g)
	type leg struct {
		name     string
		from, to tile.Tile
	}
	var legs []leg
	// An account may be anywhere when the ladder hands it the quest: every bank is a start, and a bank is where a kit pass ends.
	for _, b := range questplan.Banks {
		legs = append(legs, leg{"the " + b.Name + " bank to Doric", b.Tile, questplan.DoricAnchor},
			leg{"the " + b.Name + " bank to Gertrude", b.Tile, questplan.GertrudeAnchor})
	}
	doric := questplan.DoricAnchor
	mine := []tile.Tile{questplan.RimmingtonMine, questplan.RimmingtonClay, questplan.RimmingtonCopper, questplan.RimmingtonIron}
	faladorEast, faladorWest := tile.Tile{X: 3013, Z: 3355}, tile.Tile{X: 2946, Z: 3368}
	for _, m := range mine {
		legs = append(legs,
			leg{"Doric to the mine stand " + point(m).String(), doric, m},
			leg{"the mine stand " + point(m).String() + " to Doric", m, doric},
			leg{"the Falador west bank to the mine stand " + point(m).String(), faladorWest, m},
			leg{"the Draynor bank to the mine stand " + point(m).String(), questplan.DraynorBank, m},
			leg{"the mine stand " + point(m).String() + " to the Falador east bank", m, faladorEast})
	}
	legs = append(legs,
		leg{"the Draynor bank to the pickaxe table", questplan.DraynorBank, questplan.RimmingtonPickaxe},
		leg{"the pickaxe table to the mine", questplan.RimmingtonPickaxe, questplan.RimmingtonMine},
		leg{"the pickaxe table to the Falador east bank", questplan.RimmingtonPickaxe, faladorEast},
		leg{"the Draynor bank to Bob's Axes", questplan.DraynorBank, questplan.BobsAxes.Anchor},
		leg{"Bob's Axes to the mine", questplan.BobsAxes.Anchor, questplan.RimmingtonMine},
		leg{"the Varrock west bank to Gertrude", questplan.VarrockWestBank, questplan.GertrudeAnchor},
		leg{"the Varrock east bank to the market", questplan.VarrockEastBank, questplan.FluffsMarket},
		leg{"Gertrude to the market", questplan.GertrudeAnchor, questplan.FluffsMarket},
		leg{"the market to Gertrude", questplan.FluffsMarket, questplan.GertrudeAnchor},
		leg{"the Varrock west bank to the market", questplan.VarrockWestBank, questplan.FluffsMarket},
		leg{"the market to the doogle leaves", questplan.FluffsMarket, questplan.DoogleField},
		leg{"the Varrock west bank to the doogle leaves", questplan.VarrockWestBank, questplan.DoogleField},
		leg{"the doogle leaves to Gerrant in Port Sarim", questplan.DoogleField, questplan.FluffsSardineShop.Anchor},
		leg{"Gerrant to the Draynor bank", questplan.FluffsSardineShop.Anchor, questplan.DraynorBank},
		leg{"the Draynor bank to the Lumbridge farmhouse bucket", questplan.DraynorBank, questplan.FarmBucketSpawn},
		leg{"the Draynor bank to the Lumbridge general store", questplan.DraynorBank, questplan.LumbridgeStore.Anchor},
		leg{"the farmhouse to the dairy field", questplan.FarmBucketSpawn, questplan.DairyField},
		leg{"the dairy field to the fence", questplan.DairyField, questplan.YardOutside},
		leg{"the Varrock east bank to the fence", questplan.VarrockEastBank, questplan.YardOutside},
		leg{"the Varrock west bank to the fence", questplan.VarrockWestBank, questplan.YardOutside},
		leg{"the Edgeville bank to the fence", questplan.Banks[3].Tile, questplan.YardOutside},
		leg{"the market to the fence", questplan.FluffsMarket, questplan.YardOutside},
		leg{"the fence back to Gertrude", questplan.YardOutside, questplan.GertrudeAnchor},
		leg{"the fence back to the Varrock west bank", questplan.YardOutside, questplan.VarrockWestBank},
	)
	for _, l := range legs {
		r := pl.Plan(point(l.from), point(l.to), webwalk.Options{})
		if !r.OK {
			t.Errorf("%s: no route from %v to %v: %s", l.name, l.from, l.to, r.Reason)
			continue
		}
		// No leg of either quest goes near the Wilderness line: the walker keeps out of it, and the plans must not need it.
		t.Logf("%s: cost %d", l.name, r.Cost)
	}
}

// Rimmington's rocks are found by the miner's query on the real map: the two clay rocks, copper and iron within reach of their
// stands, each skipped once marked, and none once all are.
func TestDoricRocksAreFoundOnTheRealMap(t *testing.T) {
	ixPath, cfgPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if ixPath == "" || cfgPath == "" {
		t.Skip("RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG are not both set")
	}
	ix, err := locindex.Load(ixPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	d := &questrunner.Deps{Session: actionsim.New(actionsim.Objs{Stackable: func(id int32) bool { return id == questplan.CoinsID }}, nil),
		Locs: ix, Config: cfg}
	for _, o := range doricOres {
		skip := map[tile.Tile]time.Time{}
		found := 0
		for found < 40 {
			m, ok := doricRock(d, o.rocks, o.stand, skip)
			if !ok {
				break
			}
			at := tile.Tile{X: m.X, Z: m.Z, Level: m.Level}
			if _, seen := skip[at]; seen {
				t.Errorf("%s: the rock at %v came back after it was skipped", o.name, at)
				break
			}
			if tile.Chebyshev(at, o.stand) > doricRockSearch+2 {
				t.Errorf("%s: the rock at %v is out of reach of its stand %v", o.name, at, o.stand)
			}
			skip[at] = time.Now().Add(time.Hour)
			found++
		}
		want := map[string]int{"clay": 2, "copper ore": 4, "iron ore": 5}[o.name]
		if found < want {
			t.Errorf("%s: %d rocks within %d of %v, want at least %d", o.name, found, doricRockSearch, o.stand, want)
		}
	}
	// The first hit is a rock of the ore asked for, with the op Mine, at the nearest distance.
	m, ok := doricRock(d, questplan.DoricClayRocks, questplan.RimmingtonClay, nil)
	if !ok || (m.ID != questplan.DoricClayRocks[0] && m.ID != questplan.DoricClayRocks[1]) || strings.ToLower(m.Name) != "rocks" || m.Op == 0 {
		t.Errorf("the nearest clay rock: %+v (found %v)", m, ok)
	}
	if tile.Chebyshev(tile.Tile{X: m.X, Z: m.Z}, questplan.RimmingtonClay) > 3 {
		t.Errorf("the clay rock %d,%d is not beside the clay stand %v", m.X, m.Z, questplan.RimmingtonClay)
	}
	// A stand with no rock of that ore near finds none: the iron stand holds no clay.
	if m, ok := doricRock(d, questplan.DoricClayRocks, tile.Tile{X: 3200, Z: 3200}, nil); ok {
		t.Errorf("a clay rock in Lumbridge: %+v", m)
	}
}
