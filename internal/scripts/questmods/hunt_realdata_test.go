package questmods

// Pirate's Treasure on the real data: the production config, the baked loc index and the collision pack (RSB0T_CONFIG_JAG,
// RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them, through csData). The questions: whether every id the plan
// carries names what it says in the config the fleet's client reads, whether the customs' own loc queries find their locs where
// the plan says and each has a tile to stand on, and whether every leg of the quest is a route the planner finds on the real
// pack and graph, through the ships and the back room's door, and never through the Wilderness.

import (
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// Every id the plans carry names what the plan says it is, in the config the client reads.
func TestHuntIdsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for _, c := range []struct {
		id       int
		name, op string
	}{
		{questplan.ObjKaramjaRum, "Karamjan rum", ""}, {questplan.ObjChestKey, "Chest key", ""}, {questplan.ObjPirateMessage, "Pirate message", "Read"},
		{questplan.ObjSpade, "Spade", "Dig"}, {questplan.ObjWhiteApron, "White apron", "Wear"}, {questplan.ObjBanana, "Banana", "Eat"},
		{questplan.CoinsID, "Coins", ""},
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
	// Two objects are called "Karamjan rum" or "Banana" in this build; the plan names the quest's by id and nothing else.
	for id, want := range map[int]string{
		questplan.NpcRedbeardFrank: "Redbeard Frank", questplan.NpcLuthas: "Luthas", questplan.NpcZambo: "Zambo",
		questplan.NpcWydin: "Wydin", questplan.NpcFaladorGardener: "Gardener", 548: "Thessalia",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (found %v), the plan says %q", id, n.Name, ok, want)
		}
	}
	for _, c := range []struct {
		id         int
		name, op   string
		opPosition int
	}{
		{questplan.LocBananaCrate, "Crate", "Search", 1}, {questplan.LocGroceryCrate, "Crate", "Search", 1},
		{questplan.LocPirateChest, "Chest", "Open", 1}, {questplan.LocWydinDoor, "Door", "Open", 1},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name || cachecfg.OpIndex(l.Ops, c.op) != c.opPosition {
			t.Errorf("loc %d is %q offering %v (found %v), the plan says %q op %d %q", c.id, l.Name, l.Ops, ok, c.name, c.opPosition, c.op)
		}
	}
	for _, id := range questplan.BananaTreeLocs {
		l, ok := cfg.Loc(id)
		if !ok || l.Name != "Banana Tree" || cachecfg.OpIndex(l.Ops, "Search") != 1 || l.Width != 2 || l.Length != 2 {
			t.Errorf("banana tree %d: %q %v %dx%d (found %v)", id, l.Name, l.Ops, l.Width, l.Length, ok)
		}
	}
	// The chest is reached from the north only, which is why the plan names the stand.
	if l, _ := cfg.Loc(questplan.LocPirateChest); l.ForceApproach == 0 {
		t.Error("the Pirate chest has no forced approach in the config")
	}
	// The empty tree keeps the name and the op (the reason the trees are found by id).
	if l, ok := cfg.Loc(questplan.BananaTreeLocs[4] + 1); !ok || l.Name != "Banana Tree" || cachecfg.OpIndex(l.Ops, "Search") != 1 {
		t.Errorf("the empty banana tree: %q %v (found %v)", l.Name, l.Ops, ok)
	}
}

// The customs' own loc queries find their locs where the plans say, and each has a tile to stand on.
func TestHuntLocQueriesFindTheirLocs(t *testing.T) {
	d := csDeps(t)
	for _, c := range []struct {
		what      string
		id        int
		op        string
		near      tile.Tile
		radius    int
		x, z, lvl int
	}{
		{"the plantation crate", questplan.LocBananaCrate, "Search", questplan.PlantationCrate, 12, 2943, 3151, 0},
		{"Wydin's grocery crate", questplan.LocGroceryCrate, "Search", questplan.StoreCrate, 8, 3009, 3207, 0},
		{"Hector's chest", questplan.LocPirateChest, "Open", questplan.ChestStand, 12, 3218, 3396, 1},
		{"the back room's door", questplan.LocWydinDoor, "Open", tile.Tile{X: 3012, Z: 3204}, 4, 3012, 3204, 0},
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
		walkable := 0
		for _, s := range stands {
			if d.Pack.Walkable(s.X, s.Z, s.Level) {
				walkable++
			}
		}
		if walkable == 0 {
			t.Errorf("%s at %d,%d,%d has no walkable stand among %v", c.what, m.X, m.Z, m.Level, stands)
		}
		// The ordinary crates beside both crates offer the same Search under the same name: a name query would pick them.
		if c.id == questplan.LocBananaCrate || c.id == questplan.LocGroceryCrate {
			byName, ok := questrunner.FindLoc(d, 0, "Crate", "Search", c.near, 8)
			if !ok {
				t.Errorf("%s: no crate by name either", c.what)
			} else if byName.ID == c.id && byName.X == c.x && byName.Z == c.z {
				t.Logf("%s: the nearest Crate by name happens to be the right one here", c.what)
			}
		}
	}
	// The chest is operated from the east, the one side the engine reaches it from (the config's forceapproach=north is a blocked
	// side that the placement's angle turns).
	m, _ := questrunner.FindLoc(d, questplan.LocPirateChest, "", "Open", questplan.ChestStand, 12)
	stands := questrunner.LocStands(d, m, questplan.ChestStand)
	if len(stands) == 0 || stands[0] != questplan.ChestStand {
		t.Errorf("the chest's first stand is %v, want %v", stands, questplan.ChestStand)
	}
	// The grove: the bearing trees are found by id in a crowd, each a 2x2 loc with a stand, and the empty variant is not.
	trees := d.Locs.Find(d.Config, locindex.Query{X: questplan.BananaGrove.X, Z: questplan.BananaGrove.Z, Radius: 25, IDs: questplan.BananaTreeLocs, Op: "Search"}, nil)
	if len(trees) < 25 {
		t.Fatalf("%d bearing banana trees within 25 of the grove anchor, want the plantation's 30 or so", len(trees))
	}
	bare := 0
	for _, m := range trees {
		if m.ID == questplan.BananaTreeLocs[4]+1 {
			bare++
		}
		if s := questrunner.LocStands(d, m, questplan.BananaGrove); len(s) == 0 {
			t.Errorf("the tree at %d,%d has no stand", m.X, m.Z)
		}
	}
	if bare != 0 {
		t.Errorf("%d empty trees among the bearing ones", bare)
	}
	// And the trees' changes arrive as locs of the next stage: a tree the live state shows as 'four' is still found.
	live := []locindex.Change{{X: trees[0].X, Z: trees[0].Z, Level: 0, ID: questplan.BananaTreeLocs[1], Shape: trees[0].Shape, Angle: trees[0].Angle}}
	found := false
	for _, m := range d.Locs.Find(d.Config, locindex.Query{X: trees[0].X, Z: trees[0].Z, Radius: 1, IDs: questplan.BananaTreeLocs, Op: "Search"}, live) {
		if m.X == trees[0].X && m.Z == trees[0].Z && m.ID == questplan.BananaTreeLocs[1] {
			found = true
		}
	}
	if !found {
		t.Error("a tree changed to the next stage is not found as that stage")
	}
}

// huntWorld is a planner state with coins for the ships and the apron worn.
func huntWorld(coins int) *webwalk.WorldState {
	return &webwalk.WorldState{Members: true, Skills: map[string]int{}, FreeSlots: 20,
		ItemCount: func(name string) int {
			if name == "Coins" {
				return coins
			}
			return 0
		},
		WornCount: func(name string) int {
			if name == "White apron" {
				return 1
			}
			return 0
		}}
}

func huntPoint(t tile.Tile) webwalk.Point { return webwalk.Point{X: t.X, Z: t.Z, Level: t.Level} }

// inWilderness is the level 0 Wilderness: north of the Edgeville ditch line, between the two western and eastern edges.
func inWilderness(p webwalk.Point) bool {
	return p.Level == 0 && p.X >= 2944 && p.X <= 3391 && p.Z >= 3523 && p.Z <= 3967
}

// Every leg of the quest plans on the real pack and graph, the crossings take the ships, the back room is entered by its door,
// and nothing goes near the Wilderness.
func TestHuntLegsPlanOnTheRealPack(t *testing.T) {
	pk, _, _ := csData(t)
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	p := webwalk.NewPlanner(pk, g)
	bankVarrock := questplan.VarrockWestBank
	crateStand := tile.Tile{X: 2943, Z: 3150}
	backRoom := tile.Tile{X: 3010, Z: 3206}
	legs := []struct {
		name     string
		from, to tile.Tile
		hops     []string // hop locations the route must take (LocName), in order
	}{
		{"Frank to the Draynor bank", questplan.FrankAnchor, questplan.DraynorBank, nil},
		{"the Draynor bank to Thessalia", questplan.DraynorBank, questplan.ThessaliaShop.Anchor, nil},
		{"Thessalia to Frank", questplan.ThessaliaShop.Anchor, questplan.FrankAnchor, nil},
		{"Frank to Zambo, over the Port Sarim ship", questplan.FrankAnchor, questplan.ZamboShop.Anchor, []string{"Seaman Thresnor"}},
		{"Zambo to Luthas", questplan.ZamboShop.Anchor, questplan.LuthasAnchor, nil},
		{"Luthas to the plantation crate", questplan.LuthasAnchor, crateStand, nil},
		{"the plantation crate to the grove", questplan.PlantationCrate, questplan.BananaGrove, nil},
		{"the grove back to the crate", questplan.BananaGrove, crateStand, nil},
		{"the crate to Luthas", crateStand, questplan.LuthasAnchor, nil},
		{"Luthas to Wydin, over the Musa ship (the customs officer's)", questplan.LuthasAnchor, questplan.WydinShop.Anchor, []string{"Customs officer"}},
		{"Wydin to the back room, through the apron's door", questplan.WydinShop.Anchor, backRoom, []string{"Door"}},
		{"the back room to Frank", backRoom, questplan.FrankAnchor, []string{"Door"}},
		{"Wydin to Frank", questplan.WydinShop.Anchor, questplan.FrankAnchor, nil},
		{"Frank to the Varrock west bank", questplan.FrankAnchor, bankVarrock, nil},
		{"the Varrock west bank to the chest's stand upstairs", bankVarrock, questplan.ChestStand, nil},
		{"the chest to the spade by the park", questplan.ChestStand, questplan.SpadeSpawn, nil},
		{"the spade to the X", questplan.SpadeSpawn, questplan.DigSite, nil},
		{"the Varrock west bank to the X", bankVarrock, questplan.DigSite, nil},
		{"the X to the Falador east bank", questplan.DigSite, tile.Tile{X: 3013, Z: 3355}, nil},
		{"the Draynor bank to Frank", questplan.DraynorBank, questplan.FrankAnchor, nil},
		{"Frank to the Varrock west bank for a banked message", questplan.FrankAnchor, bankVarrock, nil},
	}
	for _, l := range legs {
		r := p.Plan(huntPoint(l.from), huntPoint(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true, State: huntWorld(500)})
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		for _, w := range r.Waypoints {
			if inWilderness(w.Point) {
				t.Errorf("%s: the route steps on %v, in the Wilderness", l.name, w.Point)
				break
			}
		}
		var hops []string
		for _, h := range r.Hops() {
			switch h.LocName {
			case "Seaman Thresnor", "Customs officer", "Door":
				if h.LocName == "Door" && h.LocID != questplan.LocWydinDoor {
					continue
				}
				hops = append(hops, h.LocName)
			}
		}
		for i, want := range l.hops {
			if i >= len(hops) || hops[i] != want {
				t.Errorf("%s: the route's hops %v, want %v first", l.name, hops, l.hops)
				break
			}
		}
		t.Logf("%-62s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}

	// The fares are what the ships cost: with 29 coins the island is out of reach, which is why the supplies pass draws the coins
	// before the account sets out.
	r := p.Plan(huntPoint(questplan.FrankAnchor), huntPoint(questplan.ZamboShop.Anchor), webwalk.Options{AllowSpecial: true, AllowNPC: true, State: huntWorld(29)})
	if r.OK {
		t.Error("the planner boarded the 30-coin Port Sarim ship with 29 coins")
	}
	// The way back is the customs officer's ship, which searches the pack: every route home from the island takes it, which is
	// why the rum goes in the crate.
	back := p.Plan(huntPoint(questplan.PlantationCrate), huntPoint(questplan.FrankAnchor), webwalk.Options{AllowSpecial: true, AllowNPC: true, State: huntWorld(500)})
	if !back.OK {
		t.Fatalf("no way back from the plantation: %s", back.Reason)
	}
	customs := false
	for _, h := range back.Hops() {
		if h.LocName == "Customs officer" && h.Special != nil {
			customs = true
		}
	}
	if !customs {
		t.Error("the way back from Karamja does not take the customs officer's ship")
	}
}

// Every tile the quest stands on is a floor tile on the real pack, and every item it picks up from the ground can be taken from
// a tile the engine accepts a Take from.
func TestHuntStandsAndPickupsAreReachable(t *testing.T) {
	pk, _, _ := csData(t)
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{
		{"Frank's stand", questplan.FrankAnchor}, {"Luthas's stand", questplan.LuthasAnchor}, {"Zambo's stand", questplan.ZamboShop.Anchor},
		{"the X", questplan.DigSite}, {"the chest's stand upstairs", questplan.ChestStand}, {"the grove's anchor", questplan.BananaGrove},
		{"Wydin's stand", questplan.WydinShop.Anchor},
	} {
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) {
			t.Errorf("%s at %d,%d,%d is not a walkable tile on the real pack", c.what, c.at.X, c.at.Z, c.at.Level)
		}
	}
	// The spade lies on a tile that is not itself a floor tile (a bench or a rail), so it is taken from a neighbour.
	if tiles := pk.InteractTiles(pathfind.NavPoint{X: questplan.SpadeSpawn.X, Z: questplan.SpadeSpawn.Z, Level: questplan.SpadeSpawn.Level}); len(tiles) == 0 {
		t.Error("the spade can be taken from no tile")
	}
	// The apron lying in Wydin's back room (m47_50, 3010,3208) is behind the door the apron opens, which is why the supplies pass
	// does not list it as a ground source: the account that could take it could already be in the room.
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	room := webwalk.NewPlanner(pk, g).Plan(huntPoint(questplan.WydinShop.Anchor), webwalk.Point{X: 3010, Z: 3208}, webwalk.Options{AllowSpecial: true, AllowNPC: true})
	if !room.OK {
		t.Fatalf("no route to the apron's spawn: %s", room.Reason)
	}
	doors := 0
	for _, h := range room.Hops() {
		if h.LocID == questplan.LocWydinDoor {
			doors++
		}
	}
	if doors != 1 {
		t.Errorf("the route to the back room's apron takes the apron's door %d times, want once", doors)
	}
}
