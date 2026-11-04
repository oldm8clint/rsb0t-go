package questmods

// Hazeel Cult on the real data: the production config, the baked loc index, the collision pack and the web graph (RSB0T_CONFIG_JAG,
// RSB0T_LOC_INDEX, RSB0T_COLLISION_PACK; each test skips without them). The questions are whether every id the plan carries names what it
// says in the config the fleet's client reads, whether the customs' own loc queries find their locs where the plan says and each has a
// tile to stand on, whether every leg the quest walks is a route the planner finds on the real pack, whether the cave, the stairs, the
// raft and the secret wall are the crossings the planner cannot find (so that the pockets are real), and whether the pockets' rectangles
// are the pieces of ground the pack makes them.

import (
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestHazeelIDsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjPoison: "Poison", questplan.ObjHzScroll: "Scroll of hazeel", questplan.ObjHzKey: "Key", questplan.ObjHzArmour: "Carnillean armour",
		questplan.ObjHzMark: "Mark of hazeel",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	for id, want := range map[int]string{
		questplan.NpcHzCeril: "Ceril Carnillean", questplan.NpcHzClaus: "Claus the chef", questplan.NpcHzJones: "Butler Jones", questplan.NpcHzAlomone: "Alomone",
		questplan.NpcHzHazeel: "Hazeel", questplan.NpcHzClivet: "Clivet", questplan.NpcHzCultist: "Hazeel Cultist",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (found %v), the plan says %q", id, n.Name, ok, want)
		}
	}
	// The stops' names are the config's (FindNpc looks by id, the talk op by the config's op).
	for _, stop := range []questplan.NpcStop{questplan.HzCerilStop, questplan.HzClivetStop, questplan.HzAlomoneStop} {
		n, _ := cfg.Npc(stop.ID)
		if n.Name != stop.Npc || cachecfg.OpIndex(n.Ops, "Talk-to") != 1 {
			t.Errorf("stop %q: npc %d is %q offering %v", stop.Npc, stop.ID, n.Name, n.Ops)
		}
	}
	for _, c := range []struct {
		id       int
		name, op string
		w, l     int
	}{
		{questplan.LocHzValve1, "Sewer valve", "Turn-right", 1, 1}, {questplan.LocHzValve3, "Sewer valve", "Turn-left", 1, 1},
		{questplan.LocHzRaft, "Raft", "Board", 1, 2}, {questplan.LocHzCave, "Cave entrance", "Enter", 4, 4}, {questplan.LocHzStairs, "Stairs", "Climb-up", 2, 3},
		{questplan.LocHzSecretWall, "Wall", "Knock-at", 1, 1}, {questplan.LocHzChestShut, "Chest", "Open", 1, 1}, {questplan.LocHzChestOpen, "Chest", "Search", 1, 1},
		{questplan.LocHzCrate, "Crate", "Search", 1, 1}, {questplan.LocHzRange, "Range", "", 1, 2}, {questplan.LocHzKitchenLadder, "Ladder", "Climb-up", 1, 1},
		{questplan.LocHzAtticLadderUp, "Ladder", "Climb-up", 1, 1}, {questplan.LocHzAtticLadderDn, "Ladder", "Climb-down", 1, 1},
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
	// All five valves offer Turn-left first and Turn-right second.
	for id := questplan.LocHzValve1; id <= questplan.LocHzValve5; id++ {
		l, _ := cfg.Loc(id)
		if cachecfg.OpIndex(l.Ops, "Turn-left") != 1 || cachecfg.OpIndex(l.Ops, "Turn-right") != 2 {
			t.Errorf("valve %d offers %v", id, l.Ops)
		}
	}
	// The hops' loc names are the config's.
	for _, h := range questplan.HzHops {
		found := false
		for _, id := range []int{questplan.LocHzCave, questplan.LocHzStairs} {
			l, _ := cfg.Loc(id)
			if l.Name == h.LocName && cachecfg.OpIndex(l.Ops, h.Op) > 0 {
				found = true
			}
		}
		if !found {
			t.Errorf("hop %+v names no loc of the config", h)
		}
	}
}

// The customs' own loc queries find their locs where the plan says, and each has a tile the engine accepts the op from.
func TestHazeelLocQueriesFindTheirLocsAndStands(t *testing.T) {
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
		{"the cave", questplan.LocHzCave, "Enter", questplan.HzCaveAt, 6, 2585, 3233, 0, []tile.Tile{questplan.HzCaveStand}},
		{"the stairs", questplan.LocHzStairs, "Climb-up", questplan.HzStairsAt, 6, 2570, 9683, 0, []tile.Tile{{X: 2569, Z: 9684}, {X: 2570, Z: 9682}}},
		{"the landing's raft", questplan.LocHzRaft, "Board", questplan.HzRaftMouth, 3, 2567, 9679, 0, []tile.Tile{questplan.HzRaftMouthStand}},
		{"the hideout's raft", questplan.LocHzRaft, "Board", questplan.HzHideoutRaft, 3, 2606, 9693, 0, []tile.Tile{questplan.HzHideoutStand}},
		{"the range", questplan.LocHzRange, "", questplan.HzRangeAt, 4, 2564, 9672, 0, []tile.Tile{questplan.HzRangeStand}},
		{"the crate", questplan.LocHzCrate, "Search", questplan.HzCrateAt, 3, 2571, 9669, 0, []tile.Tile{questplan.HzCrateStand}},
		{"the secret wall", questplan.LocHzSecretWall, "Knock-at", questplan.HzWallAt, 3, 2572, 3270, 1, []tile.Tile{questplan.HzWallSouth, questplan.HzWallNorth}},
		{"the attic ladder up", questplan.LocHzAtticLadderUp, "Climb-up", questplan.HzAtticLadder, 3, 2573, 3271, 1, []tile.Tile{questplan.HzWallNorth}},
		{"the attic ladder down", questplan.LocHzAtticLadderDn, "Climb-down", tile.Tile{X: 2573, Z: 3271, Level: 2}, 3, 2573, 3271, 2, []tile.Tile{{X: 2572, Z: 3271, Level: 2}}},
		{"the chest", questplan.LocHzChestShut, "Open", questplan.HzChestAt, 3, 2571, 3269, 2, []tile.Tile{questplan.HzChestStand}},
	}
	for i, v := range questplan.HzValves {
		cs = append(cs, struct {
			what      string
			id        int
			op        string
			near      tile.Tile
			radius    int
			x, z, lvl int
			stands    []tile.Tile
		}{"valve " + string(rune('1'+i)), v.Loc, v.Op, v.At, 3, v.At.X, v.At.Z, 0, nil})
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
		if len(stands) == 0 {
			t.Errorf("%s: no stand", c.what)
		}
		for _, s := range stands {
			if m.Shape == 10 && s.X == m.X && s.Z == m.Z && s.Level == m.Level {
				t.Errorf("%s: its own tile %v is among the stands", c.what, s)
			}
		}
		for _, want := range c.stands {
			found := false
			for _, got := range stands {
				found = found || got == want
			}
			if !found {
				t.Errorf("%s: %v is not among the stands %v", c.what, want, stands)
			}
			if !d.Pack.Walkable(want.X, want.Z, want.Level) {
				t.Errorf("%s: the plan's stand %v is not walkable", c.what, want)
			}
		}
	}
	// The chest's forced approach (forceapproach=north at angle 1 turns to the east): the stand is its east side and no other.
	chest, _ := questrunner.FindLoc(d, questplan.LocHzChestShut, "", "Open", questplan.HzChestAt, 3)
	if stands := questrunner.LocStands(d, chest, questplan.HzChestStand); len(stands) != 1 || stands[0] != questplan.HzChestStand {
		t.Errorf("the chest's stands are %v, want %v alone", stands, questplan.HzChestStand)
	}
	// The open chest is a different loc id that is not in the baked index: the customs find the form the live loc changes say, both are looked
	// for (hzChestOf) and the open form is the one that offers Search.
	if cfg := d.Config; true {
		l, _ := cfg.Loc(questplan.LocHzChestOpen)
		if cachecfg.OpIndex(l.Ops, "Search") != 1 {
			t.Errorf("the open chest offers %v", l.Ops)
		}
	}
	// The valves' ops: every valve's stands exist and the op the plan sends is the loc's second (Turn-right) or first (the third).
	for i, v := range questplan.HzValves {
		l, _ := d.Config.Loc(v.Loc)
		want := 2
		if i == 2 {
			want = 1
		}
		if cachecfg.OpIndex(l.Ops, v.Op) != want {
			t.Errorf("valve %d: op %q is index %d, want %d", i+1, v.Op, cachecfg.OpIndex(l.Ops, v.Op), want)
		}
	}
	// The island rafts (hzIslandRafts) are rafts, each with a stand on its own island; every island raft but the landing's is the one the
	// content sends back to the landing.
	for i, at := range hzIslandRafts {
		m, ok := questrunner.FindLoc(d, questplan.LocHzRaft, "", "Board", at, 3)
		if !ok || m.X != at.X || m.Z != at.Z {
			t.Errorf("island %d: no raft at %v", i+1, at)
			continue
		}
		onIsland := false
		for _, s := range questrunner.LocStands(d, m, at) {
			if questplan.HzIslandOf(s) == i+1 {
				onIsland = true
			}
		}
		if !onIsland {
			t.Errorf("island %d: no stand of its raft is on the island", i+1)
		}
		if r, ok := hzRaftHere(tile.Tile{X: at.X, Z: at.Z + 1}); ok && r != at {
			// (a tile beside the raft is on the island for islands whose stand is north of it)
			t.Errorf("island %d: its raft is %v by hzRaftHere", i+1, r)
		}
	}
}

// Every leg the quest walks plans on the real pack and graph.
func TestHazeelLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	opts := webwalk.Options{AllowSpecial: true, AllowNPC: true}
	ceril := questplan.HzCerilAnchor
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		// The surface.
		{"Ceril to the cave", ceril, questplan.HzCaveStand}, {"the cave to Ceril", questplan.HzCaveStand, ceril}, {"the stairs' top to Ceril", questplan.HzStairsTop, ceril},
		{"the stairs' top to the cave", questplan.HzStairsTop, questplan.HzCaveStand},
		{"Ceril to the bank", ceril, questplan.EastArdougneNorth}, {"the bank to Ceril", questplan.EastArdougneNorth, ceril},
		{"the stairs' top to the first valve", questplan.HzStairsTop, tile.Tile{X: 2561, Z: 3247}},
		{"the first valve to the second", tile.Tile{X: 2561, Z: 3247}, tile.Tile{X: 2571, Z: 3263}},
		{"the second valve to the third", tile.Tile{X: 2571, Z: 3263}, tile.Tile{X: 2584, Z: 3245}},
		{"the third valve to the fourth", tile.Tile{X: 2584, Z: 3245}, tile.Tile{X: 2598, Z: 3263}},
		{"the fourth valve to the fifth", tile.Tile{X: 2598, Z: 3263}, tile.Tile{X: 2610, Z: 3243}},
		{"the fifth valve to the cave", tile.Tile{X: 2610, Z: 3243}, questplan.HzCaveStand},
		{"Ceril to the first valve", ceril, tile.Tile{X: 2561, Z: 3247}}, {"the fifth valve to Ceril", tile.Tile{X: 2610, Z: 3243}, ceril},
		// The kitchen cellar (the walker takes the mansion's ladder) and the first floor.
		{"Ceril to the range", ceril, questplan.HzRangeStand}, {"the range to the crate", questplan.HzRangeStand, questplan.HzCrateStand},
		{"the crate to the range", questplan.HzCrateStand, questplan.HzRangeStand}, {"the crate to Ceril", questplan.HzCrateStand, ceril},
		{"the range to the stairs' top", questplan.HzRangeStand, questplan.HzStairsTop}, {"the stairs' top to the range", questplan.HzStairsTop, questplan.HzRangeStand},
		{"the stairs' top to the crate", questplan.HzStairsTop, questplan.HzCrateStand},
		{"the crate to the wall's bedroom side", questplan.HzCrateStand, questplan.HzWallSouth}, {"Ceril to the wall's bedroom side", ceril, questplan.HzWallSouth},
		{"the wall's bedroom side to Ceril", questplan.HzWallSouth, ceril},
		// The ladder room and the attic (the walker takes the ladder; the wall is the module's).
		{"the ladder room to the chest's stand", questplan.HzWallNorth, questplan.HzChestStand}, {"the chest's stand to the ladder room", questplan.HzChestStand, questplan.HzWallNorth},
		// The sewer's three pieces of ground, each walked inside itself.
		{"Clivet to the landing's raft", questplan.HzClivetAnchor, questplan.HzRaftMouthStand}, {"the landing's raft to Clivet", questplan.HzRaftMouthStand, questplan.HzClivetAnchor},
		{"Clivet to the stairs", questplan.HzClivetAnchor, tile.Tile{X: 2569, Z: 9684}}, {"the landing to the stairs", questplan.HzLanding, tile.Tile{X: 2570, Z: 9682}},
		{"the hideout's raft to Alomone", questplan.HzHideoutStand, questplan.HzAlomoneAnchor}, {"Alomone to the hideout's raft", questplan.HzAlomoneAnchor, questplan.HzHideoutStand},
	}
	for _, l := range legs {
		r := p.Plan(pt(l.from), pt(l.to), opts)
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		t.Logf("%-40s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
	// The cave, the stairs, the raft and the secret wall are the crossings the planner cannot find, in either direction: the pockets are real.
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the surface to the landing", ceril, questplan.HzClivetAnchor}, {"the forest to the landing", questplan.HzCaveStand, questplan.HzLanding},
		{"the landing to the surface", questplan.HzLanding, ceril}, {"the landing to the stairs' top", questplan.HzLanding, questplan.HzStairsTop},
		{"the landing to the hideout", questplan.HzLanding, questplan.HzAlomoneAnchor}, {"the hideout to the landing", questplan.HzHideoutStand, questplan.HzLanding},
		{"the hideout to the surface", questplan.HzAlomoneAnchor, ceril}, {"the surface to the hideout", ceril, questplan.HzAlomoneAnchor},
		{"the landing to the first island", questplan.HzLanding, tile.Tile{X: 2578, Z: 9687}},
		{"the kitchen to the landing", questplan.HzCrateStand, questplan.HzClivetAnchor},
		{"the bedroom to the ladder room", questplan.HzWallSouth, questplan.HzWallNorth}, {"the ladder room to the bedroom", questplan.HzWallNorth, questplan.HzWallSouth},
		{"the ground floor to the ladder room", ceril, questplan.HzWallNorth}, {"the chest's stand to Ceril", questplan.HzChestStand, ceril},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), opts); r.OK && r.End().X == l.to.X && r.End().Z == l.to.Z {
			t.Errorf("%s: the planner finds a route (cost %d): the crossing is not the module's alone", l.name, r.Cost)
		}
	}
}

// The pockets' rectangles are the pieces of ground the pack makes them: the tiles reached on foot from the landing, from the hideout's
// raft and from each island lie in their rectangle, and the rectangle holds no other walkable ground; the kitchen cellar is a piece of its
// own, joined to the mansion and not to the sewer; the ladder room and the attic are the two pieces behind the wall.
func TestHazeelThePocketsAreTheGroundOfThePack(t *testing.T) {
	pk, _, _ := csData(t)
	flood := func(at tile.Tile) map[[2]int]bool {
		return pk.Flood(pathfind.NavPoint{X: at.X, Z: at.Z, Level: at.Level}, 60)
	}
	// Each piece of the sewer: its flood is the walkable ground of its rectangle, and its rectangle's tiles are in the pocket.
	for _, c := range []struct {
		what    string
		from    tile.Tile
		in      func(tile.Tile) bool
		tiles   int
		island  int
		isIn    bool
		notIn   bool
		isKitch bool
	}{
		{"the landing", questplan.HzLanding, questplan.InHzLanding, 34, 0, true, false, false},
		{"the hideout", questplan.HzHideoutStand, questplan.InHzHideout, 157, 0, true, false, false},
		{"island 1", tile.Tile{X: 2578, Z: 9687}, func(t tile.Tile) bool { return questplan.HzIslandOf(t) == 1 }, 36, 1, true, false, false},
		{"island 2", tile.Tile{X: 2593, Z: 9694}, func(t tile.Tile) bool { return questplan.HzIslandOf(t) == 2 }, 12, 2, true, false, false},
		{"island 3", tile.Tile{X: 2599, Z: 9712}, func(t tile.Tile) bool { return questplan.HzIslandOf(t) == 3 }, 18, 3, true, false, false},
		{"island 4", tile.Tile{X: 2616, Z: 9725}, func(t tile.Tile) bool { return questplan.HzIslandOf(t) == 4 }, 23, 4, true, false, false},
		{"the kitchen cellar", questplan.HzRangeStand, questplan.InHzKitchen, 20, 0, false, true, true},
	} {
		fl := flood(c.from)
		if len(fl) != c.tiles {
			t.Errorf("%s: the pack's ground is %d tiles, the plan was made from %d", c.what, len(fl), c.tiles)
		}
		for k := range fl {
			at := tile.Tile{X: k[0], Z: k[1]}
			if !c.in(at) {
				t.Errorf("%s: %v is reached on foot and is outside its rectangle", c.what, at)
			}
			if questplan.InHzSewer(at) != c.isIn {
				t.Errorf("%s: %v has InHzSewer %v", c.what, at, questplan.InHzSewer(at))
			}
		}
	}
	// (The rectangles are the bounding boxes of the pieces; the void ground round a cave, which the pack does not clip, is walkable on
	// paper and joined to nothing, so "every walkable tile of the rectangle" is not a test.)
	// The ladder room and the attic.
	room := pk.Flood(pathfind.NavPoint{X: questplan.HzWallNorth.X, Z: questplan.HzWallNorth.Z, Level: 1}, 30)
	if len(room) != 2 {
		t.Errorf("the ladder room is %d tiles on the first floor, the plan was made from 2", len(room))
	}
	for k := range room {
		if !questplan.InHzAttic(tile.Tile{X: k[0], Z: k[1], Level: 1}) {
			t.Errorf("%v of the ladder room is outside InHzAttic", k)
		}
	}
	attic := pk.Flood(pathfind.NavPoint{X: questplan.HzChestStand.X, Z: questplan.HzChestStand.Z, Level: 2}, 30)
	if len(attic) != 6 {
		t.Errorf("the attic is %d tiles, the plan was made from 6", len(attic))
	}
	for k := range attic {
		if !questplan.InHzAttic(tile.Tile{X: k[0], Z: k[1], Level: 2}) {
			t.Errorf("%v of the attic is outside InHzAttic", k)
		}
	}
	// The bedroom side of the wall is outside the pocket and on the walker's ground.
	if bedroom := pk.Flood(pathfind.NavPoint{X: questplan.HzWallSouth.X, Z: questplan.HzWallSouth.Z, Level: 1}, 30); bedroom[[2]int{questplan.HzWallNorth.X, questplan.HzWallNorth.Z}] {
		t.Error("the bedroom reaches the ladder room on foot: the wall is not a wall")
	}
	// Every spawn and stand of the plan is a floor tile.
	for _, c := range []struct {
		what string
		at   tile.Tile
	}{
		{"Ceril's", questplan.HzCerilAnchor}, {"Clivet's", questplan.HzClivetAnchor}, {"Alomone's", questplan.HzAlomoneAnchor}, {"the cave's stand", questplan.HzCaveStand},
		{"the landing", questplan.HzLanding}, {"the stairs' top", questplan.HzStairsTop}, {"the landing raft's stand", questplan.HzRaftMouthStand},
		{"the hideout's landing", questplan.HzHideoutStand}, {"the range's stand", questplan.HzRangeStand}, {"the crate's stand", questplan.HzCrateStand},
		{"the wall's bedroom side", questplan.HzWallSouth}, {"the wall's ladder room side", questplan.HzWallNorth}, {"the chest's stand", questplan.HzChestStand},
	} {
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) {
			t.Errorf("%s tile %v is not a walkable tile on the real pack", c.what, c.at)
		}
	}
	for _, v := range questplan.HzValves {
		if len(pk.InteractTiles(pathfind.NavPoint{X: v.At.X, Z: v.At.Z, Level: v.At.Level})) == 0 {
			t.Errorf("the valve at %v can be operated from no tile", v.At)
		}
	}
}

// The pockets are registered: every tile of the sewer and of the ladder room and the attic is in one, the mansion, the kitchen and the
// forest are not.
func TestHazeelPocketsAreRegistered(t *testing.T) {
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{
		{questplan.HzLanding, true}, {questplan.HzClivetAnchor, true}, {questplan.HzAlomoneAnchor, true}, {questplan.HzHideoutStand, true}, {tile.Tile{X: 2578, Z: 9687}, true},
		{questplan.HzWallNorth, true}, {questplan.HzChestStand, true},
		{questplan.HzCerilAnchor, false}, {questplan.HzRangeStand, false}, {questplan.HzCrateStand, false}, {questplan.HzWallSouth, false}, {questplan.HzCaveStand, false},
		{questplan.HzStairsTop, false}, {questplan.EastArdougneNorth, false},
	} {
		name, in := questrunner.PocketAt(c.at)
		if in != c.want {
			t.Errorf("%v: pocket %q (%v), want %v", c.at, name, in, c.want)
		}
	}
}
