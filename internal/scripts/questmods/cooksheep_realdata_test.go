package questmods

// Cook's Assistant and Sheep Shearer on the real data: the production config, the baked loc index and the collision pack
// (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them). The question is whether every id
// the plans carry names what they say in the config the fleet's client reads, whether the customs' own loc queries find
// their locs where the plans say, whether a stand exists for each, and whether every leg of both quests is a route the
// planner finds on the real pack.

import (
	"os"
	"sync"
	"testing"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

var csReal struct {
	once sync.Once
	pk   *pathfind.Pack
	ix   *locindex.Index
	cfg  *cachecfg.Config
	err  error
}

// csData loads the three files once; t skips when any is not configured.
func csData(t *testing.T) (*pathfind.Pack, *locindex.Index, *cachecfg.Config) {
	t.Helper()
	pp, ip, cp := os.Getenv("RSB0T_COLLISION_PACK"), os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if pp == "" || ip == "" || cp == "" {
		t.Skip("RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX or RSB0T_CONFIG_JAG not set")
	}
	csReal.once.Do(func() {
		if csReal.pk, csReal.err = pathfind.Load(pp); csReal.err != nil {
			return
		}
		if csReal.ix, csReal.err = locindex.Load(ip); csReal.err != nil {
			return
		}
		csReal.cfg, csReal.err = cachecfg.LoadFile(cp)
	})
	if csReal.err != nil {
		t.Fatal(csReal.err)
	}
	return csReal.pk, csReal.ix, csReal.cfg
}

func csDeps(t *testing.T) *questrunner.Deps {
	pk, ix, cfg := csData(t)
	return &questrunner.Deps{Session: actionsim.New(actionsim.Objs{}, nil), Pack: pk, Locs: ix, Config: cfg, Log: func(string) {}}
}

// Every id the plans carry names what the plan says it is, in the config the client reads.
func TestCookSheepIdsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjEgg: "Egg", questplan.ObjBucketOfMilk: "Bucket of milk", questplan.ObjPotOfFlour: "Pot of flour",
		questplan.ObjBucket: "Bucket", questplan.ObjEmptyPot: "Pot", questplan.ObjGrain: "Grain",
		questplan.ObjShears: "Shears", questplan.ObjWool: "Wool", questplan.ObjBallOfWool: "Ball of wool",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	for id, want := range map[int]string{
		questplan.NpcCook: "Cook", questplan.NpcFred: "Fred the Farmer", questplan.NpcDairyCow: "Cow", questplan.NpcWoollySheep: "Sheep",
		questplan.LumbridgeStore.NpcID: "Shop keeper", questplan.WydinShop.NpcID: "Wydin", questplan.AemadStore.NpcID: "Aemad",
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
		{questplan.LocWheat, "Wheat", "Pick", 2}, {questplan.LocHopperContr, "Hopper controls", "Operate", 1},
		{questplan.LocMillHopper, "Hopper", "", 0}, {questplan.LocFlourBin, "Flour bin", "Empty", 1},
		{1782, "Flour bin", "Empty", 1}, {2644, "Spinning wheel", "Spin", 2},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name {
			t.Errorf("loc %d is %q (found %v), the plan says %q", c.id, l.Name, ok, c.name)
			continue
		}
		if c.op != "" && cachecfg.OpIndex(l.Ops, c.op) != c.opPosition {
			t.Errorf("loc %d (%s) offers %v, want %q as op %d", c.id, l.Name, l.Ops, c.op, c.opPosition)
		}
	}
}

// The customs' own loc queries find their locs where the plans say, and each has a tile to stand on.
func TestCookSheepLocQueriesFindTheMill(t *testing.T) {
	d := csDeps(t)
	for _, c := range []struct {
		what       string
		id         int
		name, op   string
		near       tile.Tile
		radius     int
		x, z, lvl  int
		standsFrom tile.Tile
	}{
		{"the hopper", questplan.LocMillHopper, "", "", questplan.MillTop, 6, 3166, 3307, 2, questplan.MillTop},
		{"the hopper controls", questplan.LocHopperContr, "", "Operate", questplan.MillTop, 6, 3166, 3305, 2, questplan.MillTop},
		{"the flour bin", 0, "Flour bin", "Empty", questplan.MillBin, 6, 3166, 3307, 0, questplan.MillBin},
		{"a wheat stalk", questplan.LocWheat, "", "Pick", questplan.MillWheat, 12, 3160, 3302, 0, questplan.MillWheat},
	} {
		m, ok := questrunner.FindLoc(d, c.id, c.name, c.op, c.near, c.radius)
		if !ok {
			t.Errorf("%s: no loc found near %d,%d,%d", c.what, c.near.X, c.near.Z, c.near.Level)
			continue
		}
		if c.what != "a wheat stalk" && (m.X != c.x || m.Z != c.z || m.Level != c.lvl) {
			t.Errorf("%s found at %d,%d,%d, want %d,%d,%d", c.what, m.X, m.Z, m.Level, c.x, c.z, c.lvl)
		}
		stands := questrunner.LocStands(d, m, c.standsFrom)
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
	}
	// The name search for "Hopper" would also match the controls ("Hopper controls"): the custom asks by id for that reason.
	if m, ok := questrunner.FindLoc(d, questplan.LocMillHopper, "", "", questplan.MillTop, 6); !ok || m.ID != questplan.LocMillHopper {
		t.Errorf("hopper by id: %+v %v", m, ok)
	}
	// The stand between the hopper and its controls is a real floor tile, and the two floors are joined by the mill's ladder.
	if !d.Pack.Walkable(questplan.MillTop.X, questplan.MillTop.Z, questplan.MillTop.Level) {
		t.Errorf("the mill top stand %v is not walkable", questplan.MillTop)
	}
}

// Every leg of both quests plans on the real pack and graph.
func TestCookSheepLegsPlanOnTheRealPack(t *testing.T) {
	pk, _, _ := csData(t)
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	p := webwalk.NewPlanner(pk, g)
	pt := func(t tile.Tile) webwalk.Point { return webwalk.Point{X: t.X, Z: t.Z, Level: t.Level} }
	cook, fred := questplan.CookAnchor, questplan.FredAnchor
	wheel := tile.Tile{X: 3209, Z: 3213, Level: 1}
	penGate := tile.Tile{X: 3203, Z: 3266}
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		{"cook to the first egg", cook, questplan.CookEggSpawns[0]},
		{"cook to the second egg", cook, questplan.CookEggSpawns[1]},
		{"egg to the farmhouse bucket", questplan.CookEggSpawns[0], questplan.FarmBucketSpawn},
		{"bucket to the dairy field", questplan.FarmBucketSpawn, questplan.DairyField},
		{"dairy field to the wheat", questplan.DairyField, questplan.MillWheat},
		{"wheat to the mill's top floor", questplan.MillWheat, questplan.MillTop},
		{"the mill's top floor to the flour bin", questplan.MillTop, questplan.MillBin},
		{"flour bin to the cook", questplan.MillBin, cook},
		{"cook to the kitchen pot", cook, questplan.KitchenPotSpawn},
		{"cook to the general store", cook, questplan.LumbridgeStore.Anchor},
		{"cook to Wydin's", cook, questplan.WydinShop.Anchor},
		{"cook to the Draynor bank", cook, questplan.DraynorBank},
		{"Draynor bank to the cook", questplan.DraynorBank, cook},
		{"cook to Fred", cook, fred},
		{"Fred to the farm's shears", fred, questplan.ShearsFarm},
		{"Fred to the pen", fred, penGate},
		{"the pen to the castle wheel", penGate, wheel},
		{"the castle wheel to Fred", wheel, fred},
		{"Draynor bank to Fred", questplan.DraynorBank, fred},
	}
	for _, l := range legs {
		r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		t.Logf("%-40s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
	// Aemad's shelf is a long way from Fred on foot and still a route; the East Ardougne bank next to it is the one an
	// account in Ardougne uses.
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"Aemad to Fred", questplan.AemadStore.Anchor, fred},
		{"the Ardougne south bank to Aemad", questplan.NearestBank(tile.Tile{X: 2640, Z: 3290}).Tile, questplan.AemadStore.Anchor},
		{"Fred to Aemad", fred, questplan.AemadStore.Anchor},
	} {
		r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		t.Logf("%-40s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
	// ShopOnTheWay agrees with the planner's costs: for an account at Fred's the shelf is the far end of the map.
	here := questplan.Snapshot{TileKnown: true, Tile: fred}
	if questplan.ShopOnTheWay(here, questplan.AemadStore, fred) || questplan.ShopOnTheWay(here, questplan.WydinShop, questplan.CookAnchor) {
		t.Error("a shop on the way for an account at Fred's")
	}
}

// Every tile the quests stand on is a floor tile on the real pack, and every item they pick up from the ground can be reached
// from a tile the engine accepts a Take from (the pot lies on the kitchen table and the bucket on a crate, so they are not
// floor tiles themselves, and the walker aims for an orthogonal neighbour of such a goal).
func TestCookSheepStandsAndPickupsAreReachable(t *testing.T) {
	pk, _, _ := csData(t)
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{
		{"Fred's stand", questplan.FredAnchor}, {"the pen's middle", tile.Tile{X: 3202, Z: 3266}},
		{"the castle wheel's stand", tile.Tile{X: 3209, Z: 3213, Level: 1}}, {"the mill's top stand", questplan.MillTop},
		{"the Cook's stand", questplan.CookAnchor}, {"the shears' spawn", questplan.ShearsFarm},
		{"the first egg", questplan.CookEggSpawns[0]}, {"the second egg", questplan.CookEggSpawns[1]},
	} {
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) {
			t.Errorf("%s at %d,%d,%d is not a walkable tile on the real pack", c.what, c.at.X, c.at.Z, c.at.Level)
		}
	}
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{
		{"the kitchen pot", questplan.KitchenPotSpawn}, {"the farmhouse bucket", questplan.FarmBucketSpawn},
		{"the shears", questplan.ShearsFarm}, {"the first egg", questplan.CookEggSpawns[0]}, {"the second egg", questplan.CookEggSpawns[1]},
	} {
		if tiles := pk.InteractTiles(pathfind.NavPoint{X: c.at.X, Z: c.at.Z, Level: c.at.Level}); len(tiles) == 0 {
			t.Errorf("%s at %d,%d,%d can be taken from no tile", c.what, c.at.X, c.at.Z, c.at.Level)
		}
	}
}
