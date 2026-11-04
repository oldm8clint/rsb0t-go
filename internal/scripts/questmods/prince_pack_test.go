package questmods

import (
	"os"
	"strings"
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

// Prince Ali Rescue's places on the real collision pack, the baked web graph, the loc index and the production config (skipped
// without RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG): every stand the module sends an account to is on a walkable
// tile, every NPC it talks to can be reached from a tile beside it, every walk it makes plans from where the account is when it
// starts it (the Al-Kharid toll gate included) and the Prison Door is operable from the stand the key is used from. A tile the
// walker cannot reach parks a quest in production after the walk times out; here it fails in a second.

func princeLocIndex(t *testing.T) *locindex.Index {
	t.Helper()
	path := os.Getenv("RSB0T_LOC_INDEX")
	if path == "" {
		t.Skip("RSB0T_LOC_INDEX not set")
	}
	ix, err := locindex.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func princeConfig(t *testing.T) *cachecfg.Config {
	t.Helper()
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestPrinceAliStandsAreWalkableAndTheirNPCsTalkable(t *testing.T) {
	pk := fillerPack(t)
	for name, at := range map[string]tile.Tile{
		"Hassan": questplan.HassanAnchor, "Osman": questplan.OsmanAnchor, "Leela": questplan.LeelaAnchor, "Ned": questplan.NedAnchor,
		"Aggie": questplan.AggieAnchor, "Joe": questplan.JoeAnchor, "Keli": questplan.KeliAnchor, "the prince": questplan.PrinceAliAnchor,
		"the key stand": questplan.PrisonDoorStand, "the cell's door row": questplan.PrisonDoorTile, "Shantay": questplan.ShantayAnchor,
		"Wydin": questplan.WydinShop.Anchor, "Thessalia": questplan.ThessaliaShop.Anchor, "the bartender": questplan.RustyAnchorBartender,
		"the fire site": questplan.PrinceFireSite, "onion patch": questplan.RimmingtonOnion,
		"the Lumbridge store": questplan.LumbridgeStore.Anchor, "the Draynor bank": questplan.DraynorBank, "Aemad": questplan.AemadStore.Anchor,
		"the Al Kharid bank": {X: 3269, Z: 3167},
	} {
		if !pk.Walkable(at.X, at.Z, at.Level) {
			t.Errorf("%s %v is not a walkable tile", name, at)
		}
	}
	// The log spawns, the ground items the kits take, lie on walkable tiles.
	for _, at := range questplan.PrinceLogSpawns {
		if !pk.Walkable(at.X, at.Z, 0) {
			t.Errorf("log spawn %v is not walkable", at)
		}
	}
	// Every cell tile is floor; the cell holds the prince's three-by-four and no more.
	n := 0
	for x := questplan.PrinceCell.MinX; x <= questplan.PrinceCell.MaxX; x++ {
		for z := questplan.PrinceCell.MinZ; z <= questplan.PrinceCell.MaxZ; z++ {
			if pk.Walkable(x, z, 0) {
				n++
			}
		}
	}
	if n < 6 {
		t.Errorf("only %d walkable tiles in the cell %+v", n, questplan.PrinceCell)
	}
	// Every NPC the quest talks to, and the rocks and the table the kits take from, can be reached from some tile beside them.
	for name, at := range map[string]tile.Tile{
		"Hassan": questplan.HassanAnchor, "Osman": questplan.OsmanAnchor, "Leela": questplan.LeelaAnchor, "Ned": questplan.NedAnchor,
		"Aggie": questplan.AggieAnchor, "Joe": questplan.JoeAnchor, "Keli": questplan.KeliAnchor, "the prince": questplan.PrinceAliAnchor,
		"Shantay": questplan.ShantayAnchor, "Wydin": questplan.WydinShop.Anchor, "Thessalia": questplan.ThessaliaShop.Anchor,
		"the bartender":  questplan.RustyAnchorBartender,
		"clay rock 2108": {X: 2986, Z: 3239}, "clay rock 2109": {X: 2987, Z: 3240}, "the pickaxe's table": questplan.PrincePickaxeSpawn,
	} {
		if len(pk.InteractTiles(pathfind.NavPoint{X: at.X, Z: at.Z, Level: at.Level})) == 0 {
			t.Errorf("no tile to reach %s at %v from", name, at)
		}
	}
	// The key stand is a tile the door is operated from: the door's north face is its own wall bit (south of the stand).
	if pk.WallMask(questplan.PrisonDoorStand.X, questplan.PrisonDoorStand.Z, 0)&(1<<2) == 0 {
		t.Errorf("the stand %v has no wall on its south face: the Prison Door is not where the plan says", questplan.PrisonDoorStand)
	}
}

// The Prison Door is a straight wall whose two stands are the door's own tile (inside the cell) and the tile north of it: the
// stands LocStands offers, which ApproachLoc walks to, and FindLoc finds the door by id and op from either side.
func TestThePrisonDoorIsOperableFromTheKeyStandAndFromInside(t *testing.T) {
	pk := fillerPack(t)
	ix, cfg := princeLocIndex(t), princeConfig(t)
	d := &questrunner.Deps{Session: actionsim.New(actionsim.Objs{}, nil), Pack: pk, Locs: ix, Config: cfg}
	for _, from := range []tile.Tile{questplan.PrisonDoorStand, questplan.PrisonDoorTile, questplan.PrinceAliAnchor} {
		m, ok := questrunner.FindLoc(d, questplan.LocPrisonDoor, "Prison Door", "Open", questplan.PrisonDoorTile, 4)
		if !ok {
			t.Fatalf("no Prison Door by id and op near %v", questplan.PrisonDoorTile)
		}
		if m.X != questplan.PrisonDoorTile.X || m.Z != questplan.PrisonDoorTile.Z || m.Shape != 0 || m.Angle != 1 {
			t.Fatalf("the door is at %d,%d shape %d angle %d", m.X, m.Z, m.Shape, m.Angle)
		}
		stands := questrunner.LocStands(d, m, from)
		if len(stands) != 2 {
			t.Fatalf("the door offers stands %v, want its own tile and the one north of it", stands)
		}
		want := map[tile.Tile]bool{questplan.PrisonDoorTile: true, questplan.PrisonDoorStand: true}
		for _, s := range stands {
			if !want[s] {
				t.Errorf("stand %v is not one of the door's two", s)
			}
		}
		// Nearest first: from the north the stand is the key stand, from the cell the door's own tile.
		first := questplan.PrisonDoorStand
		if from == questplan.PrisonDoorTile || from == questplan.PrinceAliAnchor {
			first = questplan.PrisonDoorTile
		}
		if stands[0] != first {
			t.Errorf("from %v the first stand is %v, want %v", from, stands[0], first)
		}
	}
}

// Every walk the quest makes plans from where the account is when it begins it. The Al-Kharid toll gate is a special crossing, so
// the planner is allowed them; the compound's danger zone is the live walker's automatic one, which lets a walk end inside it.
func TestPrinceAliRoutesPlanFromEveryStart(t *testing.T) {
	pk := fillerPack(t)
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	pl := webwalk.NewPlanner(pk, g)
	draynor, alKharid := questplan.DraynorBank, tile.Tile{X: 3269, Z: 3167}
	shantay := tile.Tile{X: 3304, Z: 3124}
	type leg struct {
		name     string
		from, to tile.Tile
		hop      string // a hop the route must take, "" for none required
	}
	legs := []leg{
		{"Draynor bank to Hassan (through the toll gate)", draynor, questplan.HassanAnchor, ""},
		{"Hassan to Osman", questplan.HassanAnchor, questplan.OsmanAnchor, ""},
		{"Osman to Shantay", questplan.OsmanAnchor, shantay, ""},
		{"Shantay to the Al Kharid bank", shantay, alKharid, ""},
		{"Shantay back to the Draynor bank", shantay, draynor, ""},
		{"Draynor bank to Wydin", draynor, questplan.WydinShop.Anchor, ""},
		{"Wydin to the Rusty Anchor's bartender", questplan.WydinShop.Anchor, questplan.RustyAnchorBartender, ""},
		{"the bartender to the clay rocks", questplan.RustyAnchorBartender, questplan.ClayRocks, ""},
		{"Draynor bank to Thessalia", draynor, questplan.ThessaliaShop.Anchor, ""},
		{"Thessalia to the Draynor bank", questplan.ThessaliaShop.Anchor, draynor, ""},
		{"Draynor bank to the Lumbridge store", draynor, questplan.LumbridgeStore.Anchor, ""},
		{"Draynor bank to the pickaxe's table", draynor, questplan.PrincePickaxeSpawn, ""},
		{"the pickaxe to the clay rocks", questplan.PrincePickaxeSpawn, questplan.ClayRocks, ""},
		{"the clay rocks to the onions", questplan.ClayRocks, questplan.RimmingtonOnion, ""},
		{"the onions to the fire site", questplan.RimmingtonOnion, questplan.PrinceFireSite, ""},
		{"the fire site to Aggie", questplan.PrinceFireSite, questplan.AggieAnchor, ""},
		{"Aggie to Ned", questplan.AggieAnchor, questplan.NedAnchor, ""},
		{"Ned to Leela", questplan.NedAnchor, questplan.LeelaAnchor, ""},
		{"Ned to Keli", questplan.NedAnchor, questplan.KeliAnchor, "Door"},
		{"Keli to Osman (through the toll gate)", questplan.KeliAnchor, questplan.OsmanAnchor, ""},
		{"Osman to Leela (through the toll gate)", questplan.OsmanAnchor, questplan.LeelaAnchor, ""},
		{"Leela to Joe", questplan.LeelaAnchor, questplan.JoeAnchor, "Door"},
		{"Leela to the key stand", questplan.LeelaAnchor, questplan.PrisonDoorStand, "Door"},
		{"Joe to Keli", questplan.JoeAnchor, questplan.KeliAnchor, ""},
		{"Keli to the key stand", questplan.KeliAnchor, questplan.PrisonDoorStand, ""},
		{"the cell to the key stand (the door opens from inside)", questplan.PrinceAliAnchor, questplan.PrisonDoorStand, "Prison Door"},
		{"the cell to Hassan", questplan.PrinceAliAnchor, questplan.HassanAnchor, "Prison Door"},
		{"the Draynor bank to Leela", draynor, questplan.LeelaAnchor, ""},
		{"the Draynor bank to the fire site", draynor, questplan.PrinceFireSite, ""},
		{"the mule's East Ardougne bank to Aemad's shop", tile.Tile{X: 2655, Z: 3286}, questplan.AemadStore.Anchor, ""},
	}
	for _, l := range legs {
		from, to := point(l.from), point(l.to)
		opts := webwalk.Options{AllowSpecial: true,
			AvoidZones: webwalk.ResolveDangerZones(nil, nil, webwalk.ZoneContext{IncludeAutomatic: true, Start: &from, Dest: &to})}
		r := pl.Plan(from, to, opts)
		if !r.OK {
			t.Errorf("%s: no route from %v to %v: %s", l.name, l.from, l.to, r.Reason)
			continue
		}
		var hops []string
		for _, h := range r.Hops() {
			hops = append(hops, h.LocName+" "+h.Action)
		}
		if l.hop != "" && !strings.Contains(strings.Join(hops, ","), l.hop) {
			t.Errorf("%s: the route takes %v, want a %s", l.name, hops, l.hop)
		}
		t.Logf("%s: cost %d%s", l.name, r.Cost, func() string {
			if len(hops) == 0 {
				return ""
			}
			return " hops " + strings.Join(hops, ", ")
		}())
	}
}

// The clay rocks and the onion plants are where the steps look for them: the rocks by id near the anchor offering Mine, the
// plants in the patch the Witch's Potion module picks (proven live).
func TestPrinceAliTheRocksAndThePlantsAreWhereTheStepsLook(t *testing.T) {
	ix, cfg := princeLocIndex(t), princeConfig(t)
	d := &questrunner.Deps{Session: actionsim.New(actionsim.Objs{}, nil), Locs: ix, Config: cfg}
	for _, id := range []int{questplan.LocClayRockA, questplan.LocClayRockB} {
		m, ok := questrunner.FindLoc(d, id, "Rocks", "Mine", questplan.ClayRocks, 12)
		if !ok {
			t.Errorf("no clay rock %d offering Mine within 12 tiles of %v", id, questplan.ClayRocks)
			continue
		}
		if tile.Chebyshev(tile.Tile{X: m.X, Z: m.Z}, questplan.ClayRocks) > 2 {
			t.Errorf("clay rock %d at %d,%d is more than 2 tiles from the anchor %v", id, m.X, m.Z, questplan.ClayRocks)
		}
		// Some stand beside the rock is a floor tile the account can reach the rock from.
		d.Pack = fillerPack(t)
		walkable := 0
		stands := questrunner.LocStands(d, m, questplan.ClayRocks)
		for _, s := range stands {
			if d.Pack.Walkable(s.X, s.Z, s.Level) {
				walkable++
			}
		}
		if walkable == 0 {
			t.Errorf("clay rock %d at %d,%d has no walkable stand among %v", id, m.X, m.Z, stands)
		}
	}
	// A Mine search by name would take the tin and copper rocks beside them: the step names the id.
	n := 0
	a := questplan.RimmingtonOnion
	ix.Each(a.X-12, a.Z-12, a.X+12, a.Z+12, 0, func(l locindex.Loc) {
		if l.ID == questplan.LocOnionPlant {
			n++
		}
	})
	if n < 10 {
		t.Errorf("%d onion plants within 12 tiles of %v, want the patch's 15", n, a)
	}
}
