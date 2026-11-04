package questmods

import (
	"os"
	"slices"
	"strings"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// Ernest the Chicken's places on the real collision pack, the baked web graph, the loc index and the production config archive
// (skipped without RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG): every stand the legs send an account to is a
// walkable tile, the maze's rooms are exactly the pack's components with the doors shut, the sealed pockets are the sizes the plan
// says, every loc the legs operate is where the plan puts it and offers the op the plan sends, every stand the engine accepts
// for it is reachable from the tile the leg walks to, and every walk the quest makes plans from where the account starts it. A
// tile the walker cannot reach parks a quest in production after the walk times out; here it fails in a second.

func ernestEnv(t *testing.T) (*pathfind.Pack, *locindex.Index, *cachecfg.Config) {
	t.Helper()
	pack, locs, jag := os.Getenv("RSB0T_COLLISION_PACK"), os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if pack == "" || locs == "" || jag == "" {
		t.Skip("RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX or RSB0T_CONFIG_JAG not set")
	}
	pk, err := pathfind.Load(pack)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := locindex.Load(locs)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(jag)
	if err != nil {
		t.Fatal(err)
	}
	return pk, ix, cfg
}

func nav(t tile.Tile) pathfind.NavPoint { return pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level} }

func TestTheErnestStandsAreWalkable(t *testing.T) {
	pk, _, _ := ernestEnv(t)
	for name, at := range map[string]tile.Tile{
		"Veronica": questplan.VeronicaAnchor, "Oddenstein": questplan.OddensteinAnchor, "poison": questplan.ErnestPoisonSpawn,
		"fish food": questplan.ErnestFishFoodSpawn, "spade": questplan.ErnestSpadeSpawn, "fountain stand": questplan.ErnestFountainStand,
		"compost stand": questplan.ErnestCompostStand, "closet outside": questplan.ErnestClosetOutside, "closet inside": questplan.ErnestClosetInside,
		"tube": questplan.ErnestTubeSpawn, "bookcase stand": questplan.ErnestBookcaseStand, "alcove lever": questplan.ErnestAlcoveLever,
		"ladder down stand": questplan.ErnestLadderDownStand, "basement landing": questplan.ErnestBasementLanding,
		"ladder up stand": questplan.ErnestLadderUpStand, "oil can": questplan.ErnestOilCanSpawn,
		"alcove landing 1": {X: 3096, Z: 3358}, "alcove landing 2": {X: 3096, Z: 3359}, "ladder up landing": {X: 3092, Z: 3361},
		"bookcase stand 2": {X: 3098, Z: 3359}, "back door in": {X: 3123, Z: 3360}, "back door out": {X: 3123, Z: 3361},
	} {
		if !pk.Walkable(at.X, at.Z, at.Level) {
			t.Errorf("%s %v is not a walkable tile", name, at)
		}
	}
	for _, l := range questplan.MazeLevers {
		if !pk.Walkable(l.Tile.X, l.Tile.Z, 0) {
			t.Errorf("lever %c's tile %v is not walkable: a wall decoration is operated from it", l.Name, l.Tile)
		}
	}
	for _, d := range questplan.MazeDoors {
		for _, side := range []tile.Tile{d.Near, d.Far} {
			if !pk.Walkable(side.X, side.Z, 0) {
				t.Errorf("door %s: %v is not walkable", d.Name, side)
			}
		}
	}
	// Every NPC the quest talks to can be spoken to from some tile beside its spawn.
	for name, at := range map[string]tile.Tile{"Veronica": questplan.VeronicaAnchor, "Oddenstein": questplan.OddensteinAnchor} {
		if len(pk.InteractTiles(nav(at))) == 0 {
			t.Errorf("no tile to talk to %s at %v from", name, at)
		}
	}
}

// With every door shut, the pack's components of the basement are exactly the seven rooms the plan names: each lever and each
// door's two sides are in the room the table says, and no two rooms are one component.
func TestTheMazeRoomsAreTheCollisionPacksComponents(t *testing.T) {
	pk, _, _ := ernestEnv(t)
	rep := map[questplan.MazeRoom]tile.Tile{}
	for _, l := range questplan.MazeLevers {
		rep[l.Room] = l.Tile
	}
	for _, d := range questplan.MazeDoors {
		rep[d.NearRoom], rep[d.FarRoom] = d.Near, d.Far
	}
	if len(rep) != 7 {
		t.Fatalf("%d rooms named by the tables, want 7", len(rep))
	}
	floods := map[questplan.MazeRoom]map[[2]int]bool{}
	for room, at := range rep {
		floods[room] = pk.Flood(nav(at), 40)
		if len(floods[room]) == 0 {
			t.Fatalf("room %s: %v is not walkable", room, at)
		}
	}
	for room, f := range floods {
		for k := range f {
			if got := questplan.MazeRoomAt(tile.Tile{X: k[0], Z: k[1]}); got != room {
				t.Errorf("room %s's component holds %v, which MazeRoomAt puts in %s", room, k, got)
			}
		}
		for other, at := range rep {
			if other != room && f[[2]int{at.X, at.Z}] {
				t.Errorf("rooms %s and %s are one component with every door shut", room, other)
			}
		}
	}
	for _, l := range questplan.MazeLevers {
		if !floods[l.Room][[2]int{l.Tile.X, l.Tile.Z}] {
			t.Errorf("lever %c is not in its room's component", l.Name)
		}
	}
	for _, d := range questplan.MazeDoors {
		if !floods[d.NearRoom][[2]int{d.Near.X, d.Near.Z}] || !floods[d.FarRoom][[2]int{d.Far.X, d.Far.Z}] {
			t.Errorf("door %s's sides are not in the rooms %s and %s", d.Name, d.NearRoom, d.FarRoom)
		}
	}
	// The ladder's room holds the ladder and both of its levers, room 9 the can.
	entry := floods[questplan.MazeEntry]
	for name, at := range map[string]tile.Tile{"landing": questplan.ErnestBasementLanding, "ladder stand": questplan.ErnestLadderUpStand} {
		if !entry[[2]int{at.X, at.Z}] {
			t.Errorf("%s is not in the ladder's room", name)
		}
	}
	if !floods[questplan.MazeR9][[2]int{questplan.ErnestOilCanSpawn.X, questplan.ErnestOilCanSpawn.Z}] {
		t.Error("the oil can is not in room 9")
	}
	t.Logf("the seven rooms hold %d %d %d %d %d %d %d tiles", len(floods[questplan.MazeEntry]), len(floods[questplan.MazeR14]), len(floods[questplan.MazeR2]),
		len(floods[questplan.MazeR3]), len(floods[questplan.MazeR5]), len(floods[questplan.MazeR6]), len(floods[questplan.MazeR9]))
}

func TestThePocketsAreTheCollisionPacksComponents(t *testing.T) {
	pk, _, _ := ernestEnv(t)
	closet := pk.Flood(nav(questplan.ErnestClosetInside), 40)
	if len(closet) != 10 {
		t.Errorf("the closet is %d tiles, the plan says 10", len(closet))
	}
	for k := range closet {
		if !questplan.InErnestCloset(tile.Tile{X: k[0], Z: k[1]}) {
			t.Errorf("closet tile %v is outside InErnestCloset", k)
		}
	}
	for name, at := range map[string]tile.Tile{"inside": questplan.ErnestClosetInside, "tube": questplan.ErnestTubeSpawn} {
		if !closet[[2]int{at.X, at.Z}] {
			t.Errorf("the closet's %s tile is not in the closet", name)
		}
	}
	if closet[[2]int{questplan.ErnestClosetOutside.X, questplan.ErnestClosetOutside.Z}] {
		t.Error("the tile outside the closet's door is in the closet: the door is no door")
	}
	alcove := pk.Flood(nav(tile.Tile{X: 3096, Z: 3358}), 40)
	if len(alcove) != 36 {
		t.Errorf("the alcove is %d tiles, the plan says 36", len(alcove))
	}
	for k := range alcove {
		if !questplan.InErnestAlcove(tile.Tile{X: k[0], Z: k[1]}) {
			t.Errorf("alcove tile %v is outside InErnestAlcove", k)
		}
	}
	for name, at := range map[string]tile.Tile{"lever": questplan.ErnestAlcoveLever, "ladder stand": questplan.ErnestLadderDownStand,
		"ladder up landing": {X: 3092, Z: 3361}, "landing 2": {X: 3096, Z: 3359}} {
		if !alcove[[2]int{at.X, at.Z}] {
			t.Errorf("the alcove's %s tile is not in the alcove", name)
		}
	}
	if alcove[[2]int{questplan.ErnestBookcaseStand.X, questplan.ErnestBookcaseStand.Z}] {
		t.Error("the bookcase's east stand is in the alcove: the bookcase is no teleport")
	}
}

func TestTheErnestLocsAreWhereThePlanLooksAndOfferTheOpsItSends(t *testing.T) {
	_, ix, cfg := ernestEnv(t)
	type spot struct {
		name  string
		id    int
		at    tile.Tile
		op    string
		label string
	}
	spots := []spot{
		{"compost heap", questplan.LocManorCompost, tile.Tile{X: 3084, Z: 3360}, "Search", "Compost heap"},
		{"fountain", questplan.LocManorFountain, tile.Tile{X: 3087, Z: 3334}, "Search", "Fountain"},
		{"closet door", questplan.LocClosetDoor, tile.Tile{X: 3107, Z: 3367}, "Open", "Door"},
		{"bookcase L", questplan.LocManorBookcaseL, tile.Tile{X: 3097, Z: 3358}, "Search", "Bookcase"},
		{"bookcase R", questplan.LocManorBookcaseR, tile.Tile{X: 3097, Z: 3359}, "Search", "Bookcase"},
		{"alcove lever", questplan.LocAlcoveLever, questplan.ErnestAlcoveLever, "Pull", "Lever"},
		{"ladder down", questplan.LocPuzzleLadderDown, tile.Tile{X: 3092, Z: 3362}, "Climb-down", "Ladder"},
		{"ladder up", questplan.LocPuzzleLadderUp, tile.Tile{X: 3117, Z: 9754}, "Climb-up", "Ladder"},
	}
	for _, l := range questplan.MazeLevers {
		spots = append(spots, spot{"lever " + string(rune(l.Name)), l.LocID, l.Tile, "Pull", "Lever " + string(rune(l.Name))})
	}
	for _, d := range questplan.MazeDoors {
		spots = append(spots, spot{"door " + d.Name, d.LocID, d.Near, "Open", "Door"})
	}
	for _, s := range spots {
		found := false
		for _, l := range ix.At(s.at.X, s.at.Z, s.at.Level) {
			if l.ID == s.id {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no loc %d at %v in the loc index", s.name, s.id, s.at)
		}
		typ, ok := cfg.Loc(s.id)
		if !ok {
			t.Errorf("%s: no config for loc %d", s.name, s.id)
			continue
		}
		if typ.Name != s.label {
			t.Errorf("%s: loc %d is named %q, the plan expects %q", s.name, s.id, typ.Name, s.label)
		}
		if cachecfg.OpIndex(typ.Ops, s.op) == 0 {
			t.Errorf("%s: loc %d offers %v, not %q", s.name, s.id, typ.Ops, s.op)
		}
	}
	// The compost heap is operated by a spade and the fountain by the poisoned food: neither op is "Search" for them, and the
	// config says Search is what an account with empty hands gets (the heap answers it "with my hands").
	for id, name := range map[int]string{questplan.NpcVeronica: "Veronica", questplan.NpcOddenstein: "Professor Oddenstein", questplan.NpcManorSkeleton: "Skeleton"} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != name {
			t.Errorf("npc %d: the cache has %q, the plan means %q", id, n.Name, name)
			continue
		}
		if id != questplan.NpcManorSkeleton && cachecfg.OpIndex(n.Ops, "Talk-to") == 0 {
			t.Errorf("%s offers %v, no Talk-to", name, n.Ops)
		}
		if id == questplan.NpcManorSkeleton && (cachecfg.OpIndex(n.Ops, "Attack") == 0 || n.Level != questplan.ErnestSkeletonLevel) {
			t.Errorf("the skeleton: ops %v level %d", n.Ops, n.Level)
		}
	}
	for id, name := range map[int]string{questplan.ObjPressureGauge: "Pressure gauge", questplan.ObjFishFood: "Fish food", questplan.ObjPoison: "Poison",
		questplan.ObjPoisonedFishFood: "Poisoned fish food", questplan.ObjClosetKey: "Key", questplan.ObjRubberTube: "Rubber tube",
		questplan.ObjOilCan: "Oil can", questplan.ObjSpade: "Spade"} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != name || o.Noted() {
			t.Errorf("obj %d: the cache has %q, the plan means %q", id, o.Name, name)
		}
		if cachecfg.OpIndex(o.Ops, "Take") == 0 && id != questplan.ObjClosetKey {
			// Ground items default to Take when the config names none (questrunner.TakeGround falls back to 3).
			t.Logf("obj %d (%s) ground ops %v", id, name, o.Ops)
		}
	}
	// The foods the module eats exist and eat.
	for _, id := range questplan.ErnestChicken.Sustain.FoodIDs {
		o, ok := cfg.Obj(id)
		if !ok || cachecfg.OpIndex(o.IOps, "Eat") == 0 {
			t.Errorf("food %d (%q) has no Eat", id, o.Name)
		}
	}
}

// For every loc the legs operate, some tile the engine accepts the op from is walkable and reachable on the pack from the tile
// the leg walks to first, and for the straight walls and wall decorations the accepted tiles are the ones the plan stands on.
func TestTheStandsTheEngineAcceptsAreReachableFromWhereTheLegsWalk(t *testing.T) {
	pk, ix, cfg := ernestEnv(t)
	d := &questrunner.Deps{Pack: pk, Config: cfg, Locs: ix}
	stands := func(id int, at, from tile.Tile) []tile.Tile {
		ms := ix.Find(cfg, locindex.Query{X: at.X, Z: at.Z, Level: at.Level, Radius: 2, IDs: []int{id}}, nil)
		if len(ms) == 0 {
			t.Fatalf("loc %d not found near %v", id, at)
		}
		return questrunner.LocStands(d, ms[0], from)
	}
	reach := func(from tile.Tile, radius int) map[[2]int]bool { return pk.Flood(nav(from), radius) }
	someReachable := func(name string, ss []tile.Tile, from tile.Tile) {
		t.Helper()
		f := reach(from, 60)
		for _, s := range ss {
			if f[[2]int{s.X, s.Z}] && s.Level == from.Level {
				return
			}
		}
		t.Errorf("%s: none of the accepted stands %v is reachable from %v", name, ss, from)
	}
	has := func(name string, ss []tile.Tile, want tile.Tile) {
		t.Helper()
		if !slices.Contains(ss, want) {
			t.Errorf("%s: the accepted stands %v do not include the plan's %v", name, ss, want)
		}
	}
	// Levers: from their own tile and no other.
	for _, l := range questplan.MazeLevers {
		ss := stands(l.LocID, l.Tile, l.Tile)
		has("lever "+string(rune(l.Name)), ss, l.Tile)
	}
	// Doors: from either side of the edge, and the plan's own tile on the far side is one of them.
	for _, dr := range questplan.MazeDoors {
		ss := stands(dr.LocID, dr.Near, dr.Near)
		has("door "+dr.Name+" near", ss, dr.Near)
		has("door "+dr.Name+" far", ss, dr.Far)
	}
	has("alcove lever", stands(questplan.LocAlcoveLever, questplan.ErnestAlcoveLever, questplan.ErnestAlcoveLever), questplan.ErnestAlcoveLever)
	// The closet door from outside (the plan stands on its own tile) and from inside.
	cd := stands(questplan.LocClosetDoor, questplan.ErnestClosetOutside, questplan.ErnestClosetOutside)
	has("closet door outside", cd, questplan.ErnestClosetOutside)
	has("closet door inside", cd, questplan.ErnestClosetInside)
	// The bookcases from their east side, ladders from beside them, the fountain and the compost heap from somewhere the walk reaches.
	has("bookcase L", stands(questplan.LocManorBookcaseL, tile.Tile{X: 3097, Z: 3358}, questplan.ErnestBookcaseStand), questplan.ErnestBookcaseStand)
	has("bookcase R", stands(questplan.LocManorBookcaseR, tile.Tile{X: 3097, Z: 3359}, tile.Tile{X: 3098, Z: 3359}), tile.Tile{X: 3098, Z: 3359})
	someReachable("ladder down", stands(questplan.LocPuzzleLadderDown, tile.Tile{X: 3092, Z: 3362}, questplan.ErnestLadderDownStand), questplan.ErnestLadderDownStand)
	someReachable("ladder up", stands(questplan.LocPuzzleLadderUp, tile.Tile{X: 3117, Z: 9754}, questplan.ErnestLadderUpStand), questplan.ErnestLadderUpStand)
	someReachable("fountain", stands(questplan.LocManorFountain, tile.Tile{X: 3087, Z: 3334}, questplan.ErnestFountainStand), questplan.ErnestFountainStand)
	someReachable("compost heap", stands(questplan.LocManorCompost, tile.Tile{X: 3084, Z: 3360}, questplan.ErnestCompostStand), questplan.ErnestCompostStand)
}

// Every walk the quest makes plans from where the account starts it, on the pack and the baked graph, and the routes take the doors
// and stairs the plan counts on: in by the front door and out by the back, never the way the front door refuses.
func TestTheErnestRoutesPlanFromEveryStart(t *testing.T) {
	pk, _, _ := ernestEnv(t)
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	pl := webwalk.NewPlanner(pk, g)
	draynor, varrockWest, falEast := questplan.DraynorBank, questplan.VarrockWestBank, tile.Tile{X: 3013, Z: 3355}
	manor := questplan.ErnestBookcaseStand // the manor's ground floor, east of the bookcases
	type leg struct {
		name     string
		from, to tile.Tile
		hop      string // a hop the route must take ("" for none required)
		noFront  bool   // the route must not use the front doors
	}
	legs := []leg{
		{"Draynor bank to Veronica", draynor, questplan.VeronicaAnchor, "", false},
		{"Varrock west bank to Veronica", varrockWest, questplan.VeronicaAnchor, "", false},
		{"Falador east bank to Veronica", falEast, questplan.VeronicaAnchor, "", false},
		{"Veronica to the Professor", questplan.VeronicaAnchor, questplan.OddensteinAnchor, "Large door", false},
		{"Draynor bank to the Professor", draynor, questplan.OddensteinAnchor, "Staircase", false},
		{"Varrock west bank to the Professor", varrockWest, questplan.OddensteinAnchor, "Large door", false},
		{"the Professor to the fish food", questplan.OddensteinAnchor, questplan.ErnestFishFoodSpawn, "Staircase", false},
		{"the fish food to the poison", questplan.ErnestFishFoodSpawn, questplan.ErnestPoisonSpawn, "Staircase", false},
		{"Draynor bank to the fish food", draynor, questplan.ErnestFishFoodSpawn, "Large door", false},
		{"Draynor bank to the poison", draynor, questplan.ErnestPoisonSpawn, "Large door", false},
		{"the poison to the bookcase", questplan.ErnestPoisonSpawn, manor, "", true},
		{"the bookcase to the spade", manor, questplan.ErnestSpadeSpawn, "", true},
		{"the spade to the fountain", questplan.ErnestSpadeSpawn, questplan.ErnestFountainStand, "", true},
		{"the manor to the fountain", manor, questplan.ErnestFountainStand, "", true},
		{"the fountain to the compost heap", questplan.ErnestFountainStand, questplan.ErnestCompostStand, "", false},
		{"the compost heap to the closet's door", questplan.ErnestCompostStand, questplan.ErnestClosetOutside, "Large door", false},
		{"the closet's door to the Professor", questplan.ErnestClosetOutside, questplan.OddensteinAnchor, "Staircase", false},
		{"the manor to the Draynor bank", manor, draynor, "", true},
		{"the manor to the Varrock west bank", manor, varrockWest, "", true},
		{"the Professor to the Draynor bank", questplan.OddensteinAnchor, draynor, "", true},
		{"the Professor to the nearest bank west", questplan.OddensteinAnchor, tile.Tile{X: 3094, Z: 3491}, "", true},
		{"the oil can's alcove exit to the Professor", tile.Tile{X: 3098, Z: 3358}, questplan.OddensteinAnchor, "Staircase", false},
		{"the bookcase to the bookcase", manor, tile.Tile{X: 3098, Z: 3359}, "", false},
	}
	trees := []tile.Tile{{X: 3103, Z: 3347}, {X: 3107, Z: 3337}, {X: 3107, Z: 3342}, {X: 3107, Z: 3344}, {X: 3108, Z: 3346}, {X: 3111, Z: 3339},
		{X: 3111, Z: 3348}, {X: 3115, Z: 3344}, {X: 3120, Z: 3344}, {X: 3122, Z: 3335}}
	abs := func(a int) int {
		if a < 0 {
			return -a
		}
		return a
	}
	for _, l := range legs {
		r := pl.Plan(webwalk.Point{X: l.from.X, Z: l.from.Z, Level: l.from.Level}, webwalk.Point{X: l.to.X, Z: l.to.Z, Level: l.to.Level}, webwalk.Options{})
		if !r.OK {
			t.Errorf("%s: no route from %v to %v: %s", l.name, l.from, l.to, r.Reason)
			continue
		}
		var hops []string
		front := false
		for _, h := range r.Hops() {
			hops = append(hops, h.LocName+" "+h.Action)
			if h.LocID == 134 || h.LocID == 135 {
				front = true
			}
		}
		if l.hop != "" && !strings.Contains(strings.Join(hops, ","), l.hop) {
			t.Errorf("%s: the route takes %v, want a %s", l.name, hops, l.hop)
		}
		if l.noFront && front {
			t.Errorf("%s: the route leaves by the front doors, which refuse an account inside", l.name)
		}
		brushed := map[tile.Tile]bool{}
		for _, leg := range r.Legs() {
			if leg.Hop != nil {
				continue
			}
			for i := 0; i+1 < len(leg.Tiles); i++ {
				a, b := leg.Tiles[i], leg.Tiles[i+1]
				n := max(abs(b.X-a.X), abs(b.Z-a.Z))
				for k := 0; k <= n; k++ {
					px, pz := a.X, a.Z
					if n > 0 {
						px += (b.X - a.X) * k / n
						pz += (b.Z - a.Z) * k / n
					}
					for _, tr := range trees {
						if a.Level == 0 && abs(px-tr.X) <= 1 && abs(pz-tr.Z) <= 1 {
							brushed[tr] = true
						}
					}
				}
			}
		}
		if len(brushed) > 5 {
			t.Errorf("%s: the route brushes %d of the garden's trees, the plan's comments say at most 5", l.name, len(brushed))
		}
		t.Logf("%s: cost %d hops %v trees brushed %d", l.name, r.Cost, hops, len(brushed))
	}
}

// The baked web graph holds the crossings the plan leans on: the front door one way only (in, from the south), the back door both
// ways, the bookcases into the alcove, the lever and the ladders, and none for the closet or the maze's doors, which the module
// operates itself.
func TestTheWebGraphHasTheManorsCrossingsTheLegsAssume(t *testing.T) {
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	from := func(locID int) (n int, edges []webwalk.Edge) {
		for _, e := range g.Edges {
			if e.LocID == locID {
				edges = append(edges, e)
			}
		}
		return len(edges), edges
	}
	if n, es := from(135); n != 1 || es[0].From.Z > es[0].To.Z || es[0].From != (webwalk.Point{X: 3109, Z: 3352}) {
		t.Errorf("the front door's edges: %v, want exactly one, entering from the south", es)
	}
	if n, es := from(136); n != 2 || es[0].From == es[1].From {
		t.Errorf("the back door's edges: %v, want both ways", es)
	}
	if n, _ := from(questplan.LocPuzzleLadderDown); n != 1 {
		t.Errorf("%d edges for the ladder down", n)
	}
	if n, _ := from(questplan.LocPuzzleLadderUp); n != 1 {
		t.Errorf("%d edges for the ladder up", n)
	}
	if n, _ := from(questplan.LocManorBookcaseL); n != 1 {
		t.Errorf("%d edges for the left bookcase", n)
	}
	if n, _ := from(questplan.LocManorBookcaseR); n != 1 {
		t.Errorf("%d edges for the right bookcase", n)
	}
	if n, _ := from(questplan.LocAlcoveLever); n != 2 {
		t.Errorf("%d edges for the alcove's lever, want its two landings", n)
	}
	for _, id := range []int{questplan.LocClosetDoor, 137, 138, 139, 140, 141, 142, 143, 144, 145} {
		if n, _ := from(id); n != 0 {
			t.Errorf("loc %d has %d graph edges: the walker could route through a door only a key or a lever opens", id, n)
		}
	}
}

// The solved levers on the real pack. For every room and every lever state with a way out, the shortest way to the ladder's room is
// replayed tile by tile: every lever the way pulls is on a tile the account can walk to from where it stands (the rooms are the
// pack's components with the doors shut, so a step is walked only inside a room), every door is crossed from the tile on its
// side and lands on the tile across, and the ladder up is reachable from where the way ends. The two accounts stranded on 19 Oct
// 2026 stood on the 3to6 door's near tile (room 3) and the 4to7 door's near tile (rooms 1 and 4); both are walkable, in the rooms
// the plan names, and reach their rooms' levers.
func TestTheLadderIsReachableOnThePackFromEveryRoomUnderEverySolvedLeverState(t *testing.T) {
	pk, _, _ := ernestEnv(t)
	floods := map[tile.Tile]map[[2]int]bool{}
	reach := func(from, to tile.Tile) bool {
		f, ok := floods[from]
		if !ok {
			f = pk.Flood(nav(from), 60)
			floods[from] = f
		}
		return f[[2]int{to.X, to.Z}]
	}
	starts := map[questplan.MazeRoom]tile.Tile{}
	for _, l := range questplan.MazeLevers {
		starts[l.Room] = l.Tile
	}
	for _, d := range questplan.MazeDoors {
		starts[d.NearRoom], starts[d.FarRoom] = d.Near, d.Far
	}
	rooms := []questplan.MazeRoom{questplan.MazeEntry, questplan.MazeR14, questplan.MazeR2, questplan.MazeR3, questplan.MazeR5, questplan.MazeR6, questplan.MazeR9}
	replayed := 0
	for _, room := range rooms {
		start, ok := starts[room]
		if !ok {
			t.Fatalf("no start tile for %s", room)
		}
		for bits := 0; bits < 64; bits++ {
			if !questplan.MazeEscapable(room, uint8(bits)) {
				continue
			}
			moves, ok := questplan.MazeSolve(questplan.MazeState{Room: room, Bits: uint8(bits)}, questplan.MazeEntry)
			if !ok {
				t.Fatalf("%s %s: escapable, but no way found", room, questplan.MazeBitsString(uint8(bits)))
			}
			cur, st := start, questplan.MazeState{Room: room, Bits: uint8(bits)}
			for i, mv := range moves {
				next, legal := st.Apply(mv)
				if !legal {
					t.Fatalf("%s %s: move %d (%s) is not legal", room, questplan.MazeBitsString(uint8(bits)), i, mv)
				}
				if mv.IsPull() {
					l, _ := questplan.MazeLeverByName(mv.Lever)
					if !reach(cur, l.Tile) {
						t.Errorf("%s %s: lever %c at %v is not reachable from %v on the pack", room, questplan.MazeBitsString(uint8(bits)), l.Name, l.Tile, cur)
						break
					}
					cur = l.Tile
				} else {
					d, _ := questplan.MazeDoorByName(mv.Door)
					stand, arrive, _ := d.Side(mv.From)
					if !reach(cur, stand) {
						t.Errorf("%s %s: door %s's stand %v is not reachable from %v on the pack", room, questplan.MazeBitsString(uint8(bits)), d.Name, stand, cur)
						break
					}
					if questplan.MazeRoomAt(arrive) != mv.To {
						t.Errorf("door %s lands on %v, which is %s, not %s", d.Name, arrive, questplan.MazeRoomAt(arrive), mv.To)
					}
					cur = arrive
				}
				st = next
			}
			if st.Room != questplan.MazeEntry {
				t.Errorf("%s %s: the way ends in %s", room, questplan.MazeBitsString(uint8(bits)), st.Room)
				continue
			}
			if !reach(cur, questplan.ErnestLadderUpStand) {
				t.Errorf("%s %s: the ladder's stand %v is not reachable from %v on the pack", room, questplan.MazeBitsString(uint8(bits)), questplan.ErnestLadderUpStand, cur)
			}
			replayed++
		}
	}
	t.Logf("%d (room, levers) ways out replayed on the pack", replayed)

	for name, c := range map[string]struct {
		at   tile.Tile
		room questplan.MazeRoom
	}{
		"stranded in room 3 at the 3to6 door":    {tile.Tile{X: 3097, Z: 9763}, questplan.MazeR3},
		"stranded in rooms 1/4 at the 4to7 door": {tile.Tile{X: 3108, Z: 9758}, questplan.MazeR14},
	} {
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) || questplan.MazeRoomAt(c.at) != c.room {
			t.Errorf("%s: %v walkable %v in %s", name, c.at, pk.Walkable(c.at.X, c.at.Z, c.at.Level), questplan.MazeRoomAt(c.at))
		}
		for _, l := range questplan.MazeLevers {
			if l.Room == c.room && !reach(c.at, l.Tile) {
				t.Errorf("%s: lever %c at %v is not reachable from %v", name, l.Name, l.Tile, c.at)
			}
		}
	}
}

// The search reads a lever with its Inspect op, which the content answers with the lever's state ([oploc2,_haunted_lever]): the
// production config offers it as the lever's second op, and the loc index finds each lever by it where the leg looks.
func TestEveryLeverOffersInspectAsItsSecondOpAndIsFoundByIt(t *testing.T) {
	_, ix, cfg := ernestEnv(t)
	for _, l := range questplan.MazeLevers {
		typ, ok := cfg.Loc(l.LocID)
		if !ok {
			t.Fatalf("no config for lever %c (loc %d)", l.Name, l.LocID)
		}
		if got := cachecfg.OpIndex(typ.Ops, "Inspect"); got != 2 {
			t.Errorf("lever %c offers %v: Inspect is op %d, the content answers op 2", l.Name, typ.Ops, got)
		}
		if got := cachecfg.OpIndex(typ.Ops, "Pull"); got != 1 {
			t.Errorf("lever %c offers %v: Pull is op %d, the content answers op 1", l.Name, typ.Ops, got)
		}
		ms := ix.Find(cfg, locindex.Query{X: l.Tile.X, Z: l.Tile.Z, Level: l.Tile.Level, Radius: 3, IDs: []int{l.LocID}, Op: "Inspect"}, nil)
		if len(ms) == 0 {
			t.Errorf("lever %c: no loc %d offering Inspect near %v", l.Name, l.LocID, l.Tile)
		}
	}
}
