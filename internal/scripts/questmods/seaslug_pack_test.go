package questmods

// The Sea Slug Quest on the real data: the production config, the baked loc index, the collision pack and the web graph
// (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them). The questions are whether every id the plan
// carries names what it says in the config the fleet's client reads, whether the customs' own loc queries find their locs where the plan says
// and each has a tile to stand on (the ladder's own tile is none), whether every leg the quest walks is a route the planner finds on the
// real pack, whether the boat's crossings are the ones the planner cannot find, and whether the pocket's rectangles are the regions the
// pack makes them.

import (
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestSeaSlugIDsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjTorchLit: "Torch", questplan.ObjTorchUnlit: "Torch", questplan.ObjDampSticks: "Damp sticks", questplan.ObjDrySticks: "Dry sticks",
		questplan.ObjBrokenGlass: "Broken glass", questplan.ObjSwampPaste: "Swamp paste", questplan.ObjSeaSlug: "Sea slug",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	if o, _ := cfg.Obj(questplan.ObjDrySticks); cachecfg.OpIndex(o.IOps, "Rub-together") != 1 {
		t.Errorf("the dry sticks offer %v: Rub-together is expected as the first op", o.IOps)
	}
	if o, _ := cfg.Obj(questplan.ObjSwampPaste); !o.Stackable {
		t.Error("the swamp paste is not stackable: the kit line says it is")
	}
	for id, want := range map[int]string{
		questplan.NpcBailey: "Bailey", questplan.NpcCaroline: "Caroline", questplan.NpcKennith: "Kennith", questplan.NpcHolgartIsland: "Holgart",
		questplan.NpcHolgartPlat: "Holgart", questplan.NpcHolgartLand: "Holgart", questplan.NpcKent: "Kent", questplan.NpcPlatformSlug: "Sea slug",
		702: "Fisherman", 703: "Fisherman", 704: "Fisherman", 705: "Fisherman", questplan.NpcKhazardShopkeeper: "Shop keeper",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (found %v), the plan says %q", id, n.Name, ok, want)
		}
	}
	// Nothing on the platform can be attacked: no Attack option on any of them (the sea slug's only option is Take).
	for _, id := range []int{questplan.NpcBailey, questplan.NpcKennith, questplan.NpcHolgartPlat, questplan.NpcKent, questplan.NpcPlatformSlug, 702, 703, 704, 705} {
		n, _ := cfg.Npc(id)
		if cachecfg.OpIndex(n.Ops, "Attack") != 0 {
			t.Errorf("npc %d (%s) offers %v: the quest prices no fight", id, n.Name, n.Ops)
		}
	}
	if n, _ := cfg.Npc(questplan.NpcPlatformSlug); cachecfg.OpIndex(n.Ops, "Take") != 1 {
		t.Errorf("the sea slug offers %v, want Take first", n.Ops)
	}
	for _, c := range []struct {
		id       int
		name, op string
		w, l     int
	}{
		{questplan.LocSlugLadderUp, "Ladder", "Climb-up", 1, 1}, {questplan.LocSlugLadderDown, "Ladder", "Climb-down", 1, 1},
		{questplan.LocSlugPanel, "Loose panel", "Kick", 1, 1}, {questplan.LocSlugCrates, "Crates", "Shout-across", 1, 1},
		{questplan.LocSlugCrane, "Crane", "Rotate", 4, 4},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name || l.Width != c.w || l.Length != c.l {
			t.Errorf("loc %d is %q %dx%d (found %v), the plan says %q %dx%d", c.id, l.Name, l.Width, l.Length, ok, c.name, c.w, c.l)
			continue
		}
		if cachecfg.OpIndex(l.Ops, c.op) == 0 {
			t.Errorf("loc %d (%s) offers %v, want %q", c.id, l.Name, l.Ops, c.op)
		}
	}
}

// The customs' own loc queries find their locs where the plan says, and each has a tile the engine accepts the op from; the ladder's own
// tile is no stand, the plan's stands are.
func TestSeaSlugLocQueriesFindTheirLocs(t *testing.T) {
	d := csDeps(t)
	cs := []struct {
		what      string
		id        int
		op        string
		near      tile.Tile
		radius    int
		x, z, lvl int
		stands    []tile.Tile
	}{
		{"the ladder up", questplan.LocSlugLadderUp, "Climb-up", questplan.SlugLadderFoot, 4, 2784, 3286, 0, []tile.Tile{questplan.SlugLadderFoot}},
		{"the ladder down", questplan.LocSlugLadderDown, "Climb-down", questplan.SlugLadderTop, 4, 2784, 3286, 1, []tile.Tile{questplan.SlugLadderTop}},
		{"the crates", questplan.LocSlugCrates, "Shout-across", questplan.SlugCratesStand, 4, 2764, 3287, 1, []tile.Tile{questplan.SlugCratesStand}},
		{"the loose panel", questplan.LocSlugPanel, "Kick", questplan.SlugPanelStand, 3, 2765, 3288, 1, []tile.Tile{questplan.SlugPanelStand}},
		{"the crane", questplan.LocSlugCrane, "Rotate", questplan.SlugCraneStand, 6, 2768, 3286, 1, []tile.Tile{questplan.SlugCraneStand}},
	}
	for _, c := range cs {
		m, ok := questrunner.FindLoc(d, c.id, "", c.op, c.near, c.radius)
		if !ok {
			t.Errorf("%s: no loc %d found near %v", c.what, c.id, c.near)
			continue
		}
		if m.X != c.x || m.Z != c.z || m.Level != c.lvl {
			t.Errorf("%s found at %d,%d,%d, want %d,%d,%d", c.what, m.X, m.Z, m.Level, c.x, c.z, c.lvl)
		}
		stands := questrunner.LocStands(d, m, c.near)
		for _, s := range stands {
			if s.X == m.X && s.Z == m.Z && s.Level == m.Level && m.Shape == 10 {
				t.Errorf("%s: its own tile %v is among the stands", c.what, s)
			}
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
			if !d.Pack.Walkable(s.X, s.Z, s.Level) {
				t.Errorf("%s: the plan's stand %v is not walkable", c.what, s)
			}
		}
	}
	// The ladder's own tile is not walkable, which is why the plan stands beside it.
	if d.Pack.Walkable(2784, 3286, 0) || d.Pack.Walkable(2784, 3286, 1) {
		t.Error("the ladder's own tile is walkable: the plan's stand beside it is not needed")
	}
	// The crane refuses an account at or south of its third tile from the south (coordz(coord) < coordz(loc_coord) + 3): the plan's stand is
	// north of that line, and it is one of the stands the engine accepts.
	crane, _ := questrunner.FindLoc(d, questplan.LocSlugCrane, "", "Rotate", questplan.SlugCraneStand, 6)
	if questplan.SlugCraneStand.Z < crane.Z+3 {
		t.Errorf("the crane's stand %v is south of the line (loc z %d + 3) the content refuses an account at", questplan.SlugCraneStand, crane.Z)
	}
	// The two crates both answer the shout from the plan's stand; Kennith, two tiles north of it, is the npc the script looks for.
	crates := locindex.Query{X: questplan.SlugCratesStand.X, Z: questplan.SlugCratesStand.Z, Level: questplan.SlugCratesStand.Level, Radius: 4, IDs: []int{questplan.LocSlugCrates}}
	found := d.Locs.Find(d.Config, crates, nil)
	if len(found) != 2 {
		t.Errorf("%d crates near the shout's stand, want the two of Kennith's room", len(found))
	}
	for _, cr := range found {
		if tile.Chebyshev(questplan.SlugCratesStand, tile.Tile{X: cr.X, Z: cr.Z, Level: cr.Level}) > 2 {
			t.Errorf("crates at %d,%d are more than 2 from the shout's stand", cr.X, cr.Z)
		}
	}
	if k := questplan.SlugKennithAnchor; k.Z-questplan.SlugCratesStand.Z != 2 || k.X != questplan.SlugCratesStand.X {
		t.Errorf("Kennith %v is not 2 tiles north of the stand %v: npc_find(movecoord(coord, 0, 0, 2), kennith, 5, 0)", k, questplan.SlugCratesStand)
	}
}

// Every leg the quest walks plans on the real pack and graph.
func TestSeaSlugLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	south := questplan.ArdougneSouthBank
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		// The shore.
		{"bank to Caroline", south, questplan.SlugCarolineAnchor}, {"bank to Holgart", south, questplan.SlugHolgartLandAnchor},
		{"Caroline to Holgart", questplan.SlugCarolineAnchor, questplan.SlugHolgartLandAnchor},
		{"Holgart to Caroline", questplan.SlugHolgartLandAnchor, questplan.SlugCarolineAnchor},
		{"the shore to Caroline", questplan.SlugShore, questplan.SlugCarolineAnchor}, {"the shore to Holgart", questplan.SlugShore, questplan.SlugHolgartLandAnchor},
		{"bank to Port Khazard's store", south, questplan.KhazardGeneralStore.Anchor},
		{"Port Khazard's store to the bank", questplan.KhazardGeneralStore.Anchor, south},
		{"Port Khazard's store to Holgart", questplan.KhazardGeneralStore.Anchor, questplan.SlugHolgartLandAnchor},
		// The platform's lower deck.
		{"the arrival to Holgart", questplan.SlugPlatformArrive, questplan.SlugHolgartPlatform},
		{"the arrival to Bailey", questplan.SlugPlatformArrive, questplan.SlugBaileyAnchor},
		{"Bailey to the damp sticks", questplan.SlugBaileyAnchor, questplan.SlugDampSticksSpawn},
		{"Bailey to the north glass", questplan.SlugBaileyAnchor, questplan.SlugBrokenGlassNorth}, {"Bailey to the south glass", questplan.SlugBaileyAnchor, questplan.SlugBrokenGlassSouth},
		{"the sticks to the ladder", questplan.SlugDampSticksSpawn, questplan.SlugLadderFoot}, {"the north glass to the ladder", questplan.SlugBrokenGlassNorth, questplan.SlugLadderFoot},
		{"the south glass to the ladder", questplan.SlugBrokenGlassSouth, questplan.SlugLadderFoot},
		{"the ladder to Holgart", questplan.SlugLadderFoot, questplan.SlugHolgartPlatform}, {"the ladder to Bailey", questplan.SlugLadderFoot, questplan.SlugBaileyAnchor},
		{"Holgart to Bailey", questplan.SlugHolgartPlatform, questplan.SlugBaileyAnchor}, {"Holgart to the ladder", questplan.SlugHolgartPlatform, questplan.SlugLadderFoot},
		// The upper deck.
		{"the ladder's top to the crates", questplan.SlugLadderTop, questplan.SlugCratesStand}, {"the crates to the panel", questplan.SlugCratesStand, questplan.SlugPanelStand},
		{"the panel to the crane", questplan.SlugPanelStand, questplan.SlugCraneStand}, {"the crane to the crates", questplan.SlugCraneStand, questplan.SlugCratesStand},
		{"the crane to the ladder's top", questplan.SlugCraneStand, questplan.SlugLadderTop}, {"the crates to the ladder's top", questplan.SlugCratesStand, questplan.SlugLadderTop},
		// The island.
		{"the island's arrival to Kent", questplan.SlugIslandArrive, questplan.SlugKentAnchor}, {"the island's arrival to Holgart", questplan.SlugIslandArrive, questplan.SlugHolgartIsland},
		{"Kent to Holgart", questplan.SlugKentAnchor, questplan.SlugHolgartIsland}, {"Holgart to Kent", questplan.SlugHolgartIsland, questplan.SlugKentAnchor},
	}
	for _, l := range legs {
		r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true})
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		t.Logf("%-34s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
	// The boat is the only way between the shore, the platform and the island: no walk joins them, in either direction.
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the shore to the platform", south, questplan.SlugPlatformArrive}, {"the shore to the island", south, questplan.SlugIslandArrive},
		{"the platform to the shore", questplan.SlugPlatformArrive, south}, {"the island to the shore", questplan.SlugIslandArrive, south},
		{"the platform to the island", questplan.SlugPlatformArrive, questplan.SlugIslandArrive}, {"the island to the platform", questplan.SlugIslandArrive, questplan.SlugPlatformArrive},
		{"Witchaven's shore to the platform", questplan.SlugShore, questplan.SlugHolgartPlatform},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true}); r.OK {
			t.Errorf("%s: the planner finds a route (cost %d): Holgart's boat is not the only way", l.name, r.Cost)
		}
	}
}

// The pocket's rectangles are the regions the pack makes them: the tiles reached on foot from the platform's arrival (the lower deck) and
// from the ladder's top (the upper) all lie in OnSlugPlatform, those from the island's arrival in OnSlugIsland, and no mainland tile is in
// either.
func TestSeaSlugThePocketsAreTheRegionsOfThePack(t *testing.T) {
	pk, _, _ := csData(t)
	p, _ := mcPlanner(t)
	// (Doors count: the upper deck is rooms, and the walker opens a door. None of the three is joined to anything outside itself, so a plan
	// to a tile it cannot reach ends at once.)
	lower := mcReachable(p, questplan.SlugPlatformArrive, 2750, 2805, 3260, 3300, false)
	upper := mcReachable(p, questplan.SlugCratesStand, 2750, 2805, 3260, 3300, false)
	island := mcReachable(p, questplan.SlugIslandArrive, 2780, 2810, 3305, 3335, false)
	t.Logf("the lower deck reaches %d tiles, the upper %d, the island %d", len(lower), len(upper), len(island))
	if len(lower) < 200 || len(upper) < 40 || len(island) < 30 {
		t.Fatalf("the lower deck reaches %d tiles, the upper %d and the island %d: the pack no longer says that", len(lower), len(upper), len(island))
	}
	for _, c := range append(lower, upper...) {
		if !questplan.OnSlugPlatform(c) || questplan.SlugRegionOf(c) != questplan.SlugPlatform {
			t.Errorf("%v is reached on the platform and is not in OnSlugPlatform", c)
		}
	}
	for _, c := range island {
		if !questplan.OnSlugIsland(c) || questplan.OnSlugPlatform(c) {
			t.Errorf("%v is reached on the island and is not in OnSlugIsland", c)
		}
	}
	// (That nothing of the platform or the island is reached from the shore is the legs test's: the boat is the only way.)
	// Every spawn and stand of the plan is a floor tile.
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{
		{"Caroline's", questplan.SlugCarolineAnchor}, {"Holgart's", questplan.SlugHolgartLandAnchor}, {"the shore", questplan.SlugShore},
		{"the platform's arrival", questplan.SlugPlatformArrive}, {"Holgart's on the platform", questplan.SlugHolgartPlatform}, {"Bailey's", questplan.SlugBaileyAnchor},
		{"the ladder's foot", questplan.SlugLadderFoot}, {"the ladder's top", questplan.SlugLadderTop}, {"the damp sticks'", questplan.SlugDampSticksSpawn},
		{"the north glass'", questplan.SlugBrokenGlassNorth}, {"the south glass'", questplan.SlugBrokenGlassSouth}, {"the crates' stand", questplan.SlugCratesStand},
		{"the panel's stand", questplan.SlugPanelStand}, {"the crane's stand", questplan.SlugCraneStand}, {"Kennith's", questplan.SlugKennithAnchor},
		{"the island's arrival", questplan.SlugIslandArrive}, {"Kent's", questplan.SlugKentAnchor}, {"Holgart's on the island", questplan.SlugHolgartIsland},
	} {
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) {
			t.Errorf("%s tile %v is not a walkable tile on the real pack", c.what, c.at)
		}
	}
	for _, at := range []tile.Tile{questplan.SlugDampSticksSpawn, questplan.SlugBrokenGlassNorth, questplan.SlugBrokenGlassSouth} {
		if tiles := pk.InteractTiles(pathfind.NavPoint{X: at.X, Z: at.Z, Level: at.Level}); len(tiles) == 0 {
			t.Errorf("the ground spawn at %v can be taken from no tile", at)
		}
	}
}

// The pocket is registered: every tile of the platform and the island is in it, and the shore is not.
func TestSeaSlugPocketIsRegistered(t *testing.T) {
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{{questplan.SlugPlatformArrive, true}, {questplan.SlugCraneStand, true}, {questplan.SlugIslandArrive, true}, {questplan.SlugKentAnchor, true},
		{questplan.SlugShore, false}, {questplan.SlugCarolineAnchor, false}, {questplan.ArdougneSouthBank, false}} {
		name, in := questrunner.PocketAt(c.at)
		if in != c.want {
			t.Errorf("%v: pocket %q (%v), want %v", c.at, name, in, c.want)
		}
	}
}
