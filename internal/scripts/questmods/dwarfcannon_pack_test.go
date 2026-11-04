package questmods

// Dwarf Cannon on the real data: the production config, the baked loc index and the collision pack (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX,
// RSB0T_COLLISION_PACK; each test skips without them), and the content's map for the monsters (RSB0T_CONTENT). The questions are whether
// every id the plan carries names what it says in the config the fleet's client reads, whether the customs' own loc queries find their locs
// where the plan says and each has a tile to stand on (and a landing the ladders' telejump can land on), whether every leg the quest walks is a
// route the planner finds, whether the cave is the pocket the module says it is, and how many hunters can reach the routes.

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

func TestDwarfCannonIDsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjCannonRemains: "Dwarf remains", questplan.ObjCannonToolkit: "Tool kit", questplan.ObjCannonNotes: "Nulodion's notes.",
		questplan.ObjCannonMould: "Ammo mould", questplan.ObjCannonRailing: "Railing", questplan.ObjShark: "Shark",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	// The config carries no ground options for any object (the client's default "Take" is option 3, [opobj3,mcannonremains]): the remains
	// are the same as the bones, and the custom takes them with option 3 when the config names none.
	if o, _ := cfg.Obj(questplan.ObjCannonRemains); cachecfg.OpIndex(o.Ops, "Take") != 0 {
		t.Errorf("the remains offer %v on the ground: the config now names a Take, and the custom should use its index", o.Ops)
	}
	for id, want := range map[int]string{
		questplan.NpcCannonCommander: "Dwarf Commander", questplan.NpcNulodion: "Nulodion", questplan.NpcCannonChild: "Dwarf youngster",
		questplan.NpcCannonGuard: "Dwarf", questplan.NpcGiantBat: "Giant bat",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (found %v), the plan says %q", id, n.Name, ok, want)
		}
	}
	for _, id := range []int{questplan.NpcCannonCommander, questplan.NpcNulodion} {
		if n, _ := cfg.Npc(id); cachecfg.OpIndex(n.Ops, "Talk-to") != 1 {
			t.Errorf("npc %d offers %v: Talk-to is expected first", id, n.Ops)
		}
	}
	if n, _ := cfg.Npc(questplan.NpcGiantBat); n.Level != 27 || n.Size != 2 {
		t.Errorf("the giant bat is level %d size %d, the plan prices a level 27 of size 2", n.Level, n.Size)
	}
	for _, c := range []struct {
		id       int
		name, op string
		w, l     int
	}{
		{questplan.LocCannonCrate, "Crate", "Search", 1, 1}, {questplan.LocCannonCave, "Cave Entrance", "Enter", 4, 2}, {questplan.LocCannonNulodionDoor, "Door", "Open", 1, 1},
		{questplan.LocCannonShedDoor, "Door", "Open", 1, 1}, {questplan.LocCannonBroken, "Broken MultiCannon", "Inspect", 3, 3}, {questplan.LocCannonMudPile, "Mud pile", "Climb-over", 1, 2},
		{questplan.LocCannonLadderUp, "Ladder", "Climb-up", 1, 1}, {questplan.LocCannonLadderDown, "Ladder", "Climb-down", 1, 1},
		{questplan.CannonRailingLoc(0), "Railing", "Inspect", 1, 1}, {questplan.CannonRailingLoc(5), "Railing", "Inspect", 1, 1},
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
	for _, name := range questplan.CannonParts {
		if name == "" {
			t.Error("a part has no name")
		}
	}
}

// stood reports whether want is among stands.
func stood(stands []tile.Tile, want tile.Tile) bool {
	for _, s := range stands {
		if s == want {
			return true
		}
	}
	return false
}

// The customs' own loc queries find their locs where the plan says, and each has a tile the engine accepts the op from.
func TestDwarfCannonLocQueriesFindTheirLocs(t *testing.T) {
	d := csDeps(t)
	pk, _, _ := csData(t)
	// The six railings, each at the spot the plan names, reached from its own tile or the one across the edge.
	for i, spot := range questplan.CannonRailingSpots {
		m, ok := questrunner.FindLoc(d, questplan.CannonRailingLoc(i), "", "Inspect", spot, 3)
		if !ok || m.X != spot.X || m.Z != spot.Z {
			t.Errorf("railing %d: found %+v (%v) near %v", i+1, m, ok, spot)
			continue
		}
		stands := questrunner.LocStands(d, m, spot)
		usable := 0
		for _, s := range stands {
			if pk.Walkable(s.X, s.Z, s.Level) {
				usable++
			}
		}
		if usable == 0 {
			t.Errorf("railing %d at %v: stands %v, none of them ground", i+1, spot, stands)
		}
	}
	// The crate: stands on the cave's floor.
	crate, ok := questrunner.FindLoc(d, questplan.LocCannonCrate, "", "Search", questplan.CannonCrate, 4)
	if !ok || crate.X != questplan.CannonCrate.X || crate.Z != questplan.CannonCrate.Z {
		t.Fatalf("the crate is %+v (found %v)", crate, ok)
	}
	if stands := questrunner.LocStands(d, crate, questplan.CannonCaveArrive); len(stands) == 0 {
		t.Error("the crate has no stand")
	}
	// The Cave Entrance (4x2) is entered from a stand the plan walks to, and the mud pile from the arrival tile.
	ce, ok := questrunner.FindLoc(d, questplan.LocCannonCave, "", "Enter", questplan.CannonCaveEntrance, 4)
	if !ok || ce.X != 2622 || ce.Z != 3392 {
		t.Fatalf("the Cave Entrance is %+v (found %v)", ce, ok)
	}
	if st := questrunner.LocStands(d, ce, questplan.CannonCaveStand); !stood(st, questplan.CannonCaveStand) {
		t.Errorf("the Cave Entrance's stands %v lack the plan's %v", st, questplan.CannonCaveStand)
	}
	mp, ok := questrunner.FindLoc(d, questplan.LocCannonMudPile, "", "Climb-over", questplan.CannonMudPile, 5)
	if !ok || mp.X != questplan.CannonMudPile.X || mp.Z != questplan.CannonMudPile.Z {
		t.Fatalf("the mud pile is %+v (found %v)", mp, ok)
	}
	if st := questrunner.LocStands(d, mp, questplan.CannonCaveArrive); !stood(st, questplan.CannonCaveArrive) {
		t.Errorf("the mud pile's stands %v lack the arrival tile %v", st, questplan.CannonCaveArrive)
	}
	// The cannon (3x3) is inspected from just inside the shed's door.
	cannon, ok := questrunner.FindLoc(d, questplan.LocCannonBroken, "", "Inspect", questplan.CannonShedCannon, 4)
	if !ok || cannon.X != questplan.CannonShedCannon.X || cannon.Z != questplan.CannonShedCannon.Z {
		t.Fatalf("the cannon is %+v (found %v)", cannon, ok)
	}
	if st := questrunner.LocStands(d, cannon, questplan.CannonShedStand); !stood(st, questplan.CannonShedStand) {
		t.Errorf("the cannon's stands %v lack the plan's %v", st, questplan.CannonShedStand)
	}
	// The watchtower: each ladder is found from the stand between them, with the stand among its own, and the telejump of the climb (the
	// account's own tile one floor up or down) lands on ground.
	for _, c := range []struct {
		level   int
		id      int
		op      string
		landing int
	}{
		{0, questplan.LocCannonLadderUp, "Climb-up", 1}, {1, questplan.LocCannonLadderUp, "Climb-up", 2}, {1, questplan.LocCannonLadderDown, "Climb-down", 0},
		{2, questplan.LocCannonLadderDown, "Climb-down", 1},
	} {
		stand := questplan.CannonTowerStand
		stand.Level = c.level
		m, ok := questrunner.FindLoc(d, c.id, "", c.op, stand, 3)
		if !ok {
			t.Errorf("floor %d: no ladder %d offering %q near %v", c.level, c.id, c.op, stand)
			continue
		}
		if st := questrunner.LocStands(d, m, stand); !stood(st, stand) {
			t.Errorf("floor %d: the ladder at %d,%d,%d has stands %v, not the plan's %v", c.level, m.X, m.Z, m.Level, st, stand)
		}
		if !pk.Walkable(stand.X, stand.Z, stand.Level) || !pk.Walkable(stand.X, stand.Z, c.landing) {
			t.Errorf("floor %d: the stand %v or its landing on floor %d is not ground", c.level, stand, c.landing)
		}
		// The ladder's own tile is not ground, so the plan cannot stand on it.
		if pk.Walkable(m.X, m.Z, m.Level) {
			t.Errorf("floor %d: the ladder's own tile is walkable", c.level)
		}
	}
	// The remains' floor tile is ground, and both telejumps land on ground.
	for _, at := range []tile.Tile{questplan.CannonRemainsSpawn, questplan.CannonCaveArrive, questplan.CannonCaveExit, questplan.CannonCaveStand, questplan.CannonShedStand,
		questplan.CannonCommanderAnchor, questplan.NulodionAnchor, tile.Tile{X: 2571, Z: 9851}} {
		if !pk.Walkable(at.X, at.Z, at.Level) {
			t.Errorf("tile %v is not ground", at)
		}
	}
}

// Every leg the quest walks plans on the real pack and graph.
func TestDwarfCannonLegsPlanOnTheRealPack(t *testing.T) {
	p, pt := mcPlanner(t)
	bank := questplan.EastArdougneNorth
	cmd := questplan.CannonCommanderAnchor
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		{"bank to the Commander", bank, cmd}, {"the Commander to the bank", cmd, bank},
		{"the Commander to the shed", cmd, questplan.CannonShedStand}, {"the shed to the Commander", questplan.CannonShedStand, cmd},
		{"the Commander to the tower", cmd, tile.Tile{X: 2570, Z: 3440}}, {"the tower to the Commander", tile.Tile{X: 2570, Z: 3440}, cmd},
		{"the Commander to the cave", cmd, questplan.CannonCaveStand}, {"the cave's exit to the Commander", questplan.CannonCaveExit, cmd},
		{"the cave's exit to the bank", questplan.CannonCaveExit, bank},
		{"the cave's arrival to the crate", questplan.CannonCaveArrive, tile.Tile{X: 2570, Z: 9850}},
		{"the cave's arrival to the crate (north)", questplan.CannonCaveArrive, tile.Tile{X: 2571, Z: 9851}},
		{"the crate to the mud pile", tile.Tile{X: 2570, Z: 9850}, questplan.CannonCaveArrive},
		{"the tower's first floor to its second", questplan.CannonTowerStand1, questplan.CannonTowerStand2},
		{"the top floor to the remains", questplan.CannonTowerStand2, questplan.CannonRemainsSpawn},
		{"the remains to the top floor's stand", questplan.CannonRemainsSpawn, questplan.CannonTowerStand2},
		{"the Commander to Nulodion", cmd, questplan.NulodionAnchor}, {"Nulodion to the Commander", questplan.NulodionAnchor, cmd},
		{"bank to Nulodion", bank, questplan.NulodionAnchor},
	}
	for i, spot := range questplan.CannonRailingSpots {
		legs = append(legs, struct {
			name     string
			from, to tile.Tile
		}{fmt.Sprintf("the Commander to railing %d", i+1), cmd, spot})
	}
	for i := 1; i < len(questplan.CannonRailingSpots); i++ {
		legs = append(legs, struct {
			name     string
			from, to tile.Tile
		}{fmt.Sprintf("railing %d to railing %d", i, i+1), questplan.CannonRailingSpots[i-1], questplan.CannonRailingSpots[i]})
	}
	for _, l := range legs {
		// The live walker avoids the catalog's automatic zones (the Wilderness among them) on every walk, for the strictest account (combat 3).
		r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true, AvoidZones: cannonZones(l.from, l.to)})
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		zones := cannonZones(l.from, l.to)
		for _, w := range r.Waypoints {
			if webwalk.InZones(w.Point, zones) {
				t.Errorf("%s: the route enters a danger zone at %v", l.name, w.Point)
			}
			if w.Point.Z >= 3523 && w.Point.Level == 0 && w.Point.X >= 2944 && w.Point.X <= 3391 {
				t.Errorf("%s: the route enters the Wilderness at %v", l.name, w.Point)
			}
		}
		t.Logf("%-40s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
	// The scripted ways are the only ways: the cave is entered and left by telejump, the tower's first floor is not reached from the ground
	// on foot (the ladders there are climbed by the custom, from stage 2).
	for _, l := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"bank to the cave's arrival", bank, questplan.CannonCaveArrive}, {"the cave's arrival to the bank", questplan.CannonCaveArrive, bank},
		{"the cave to the Commander", questplan.CannonCaveArrive, cmd},
	} {
		if r := p.Plan(pt(l.from), pt(l.to), webwalk.Options{AllowSpecial: true, AllowNPC: true}); r.OK {
			t.Errorf("%s: the planner finds a route (cost %d): the cave is not a pocket", l.name, r.Cost)
		}
	}
}

// cannonZones is the automatic danger zones a walk from one tile to another avoids, for a combat level 3 account.
func cannonZones(from, to tile.Tile) []webwalk.Rect {
	start, dest := webwalk.Point{X: from.X, Z: from.Z, Level: from.Level}, webwalk.Point{X: to.X, Z: to.Z, Level: to.Level}
	return webwalk.ResolveDangerZones(nil, nil, webwalk.ZoneContext{IncludeAutomatic: true, CombatKnown: true, CombatLevel: 3, Start: &start, Dest: &dest})
}

// Nulodion's camp (Ice Mountain) is in Asgarnia and the Commander's in Kandarin: no walk joins them but the Wilderness, which every walk avoids,
// and the spirit tree (an account that has finished Tree Gnome Village). An account without it crosses by the fleet's own travel teleport
// (questrunner.Deps.BeforeTravel: Falador and Camelot casts) and walks the rest, so the plan walks on both sides of the crossing; with no
// teleport the two legs end unreachable and the engine parks the quest on its stalled walks (TravelParkFor), once and with the reason.
func TestDwarfCannonNulodionNeedsATeleportOrTheSpiritTree(t *testing.T) {
	p, pt := mcPlanner(t)
	poor := &webwalk.WorldState{Members: true, Skills: map[string]int{}, FreeSlots: 20,
		QuestStatus: func(string) webwalk.QuestProgress { return webwalk.QuestNotStarted },
		ItemCount:   func(string) int { return 0 }, WornCount: func(string) int { return 0 }}
	gnome := *poor
	gnome.QuestStatus = func(q string) webwalk.QuestProgress {
		if q == "Tree Gnome Village" {
			return webwalk.QuestComplete
		}
		return webwalk.QuestNotStarted
	}
	plan := func(from, to tile.Tile, st *webwalk.WorldState) bool {
		return p.Plan(pt(from), pt(to), webwalk.Options{AllowSpecial: true, AllowNPC: true, AvoidZones: cannonZones(from, to), State: st}).OK
	}
	faladorEast, faladorWest := tile.Tile{X: 3013, Z: 3355}, tile.Tile{X: 2946, Z: 3368}
	seers, bank, cmd := tile.Tile{X: 2725, Z: 3491}, questplan.EastArdougneNorth, questplan.CannonCommanderAnchor
	for _, c := range []struct {
		name     string
		from, to tile.Tile
		st       *webwalk.WorldState
		want     bool
	}{
		// After a Falador cast (or from Falador's banks) the walk to Nulodion and back is a walk, whatever the account has done.
		{"Falador east bank to Nulodion", faladorEast, questplan.NulodionAnchor, poor, true},
		{"Falador west bank to Nulodion", faladorWest, questplan.NulodionAnchor, poor, true},
		{"Nulodion to Falador east bank", questplan.NulodionAnchor, faladorEast, poor, true},
		// After a Camelot cast the Commander is a walk.
		{"Seers' bank to the Commander", seers, cmd, poor, true},
		// Without a cast, Kandarin to Asgarnia has no walk but the spirit tree.
		{"Ardougne's bank to Nulodion, no Tree Gnome Village", bank, questplan.NulodionAnchor, poor, false},
		{"the Commander to Nulodion, no Tree Gnome Village", cmd, questplan.NulodionAnchor, poor, false},
		{"Nulodion to the Commander, no Tree Gnome Village", questplan.NulodionAnchor, cmd, poor, false},
		{"Ardougne's bank to Nulodion, Tree Gnome Village done", bank, questplan.NulodionAnchor, &gnome, true},
		{"Nulodion to the Commander, Tree Gnome Village done", questplan.NulodionAnchor, cmd, &gnome, true},
	} {
		if got := plan(c.from, c.to, c.st); got != c.want {
			t.Errorf("%s: a route exists = %v, want %v", c.name, got, c.want)
		}
	}
}

// The cave is a pocket: every tile the planner reaches from its arrival lies in the registered rectangle, and the rectangle holds nothing
// that is reached from outside.
func TestDwarfCannonTheCaveIsARegisteredPocket(t *testing.T) {
	p, _ := mcPlanner(t)
	cave := mcReachable(p, questplan.CannonCaveArrive, 2530, 2660, 9770, 9870, false)
	t.Logf("the cave reaches %d tiles", len(cave))
	if len(cave) < 900 {
		t.Fatalf("the cave reaches %d tiles: the pack no longer says 1049", len(cave))
	}
	for _, c := range cave {
		if !questplan.InCannonCave(c) {
			t.Errorf("%v is reached from the cave's arrival and is not in InCannonCave", c)
		}
	}
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{{questplan.CannonCaveArrive, true}, {questplan.CannonCrate, true}, {questplan.CannonMudPile, true}, {questplan.CannonCaveExit, false},
		{questplan.CannonCommanderAnchor, false}, {questplan.EastArdougneNorth, false}} {
		name, got := questrunner.PocketAt(c.at)
		if got != c.want {
			t.Errorf("%v: pocket %q (%v), want %v", c.at, name, got, c.want)
		}
	}
}

// cannonRoute lists the tiles of the planner's route, the straight runs between its corners filled in.
func cannonRoute(p *webwalk.Planner, from, to tile.Tile) []tile.Tile {
	r := p.Plan(webwalk.Point{X: from.X, Z: from.Z, Level: from.Level}, webwalk.Point{X: to.X, Z: to.Z, Level: to.Level}, webwalk.Options{AllowSpecial: true, AllowNPC: true})
	var out []tile.Tile
	for i, w := range r.Waypoints {
		if i == 0 || w.Point.Level != out[len(out)-1].Level {
			out = append(out, tile.Tile{X: w.Point.X, Z: w.Point.Z, Level: w.Point.Level})
			continue
		}
		cur := out[len(out)-1]
		for cur.X != w.Point.X || cur.Z != w.Point.Z {
			cur.X += sign(w.Point.X - cur.X)
			cur.Z += sign(w.Point.Z - cur.Z)
			out = append(out, cur)
		}
	}
	return out
}

func sign(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	}
	return 0
}

// cannonSpawns lists the spawn tiles of the named npcs in one map square of the content checkout.
func cannonSpawns(t *testing.T, square string, mx, mz int, names ...string) map[string][]tile.Tile {
	t.Helper()
	root := os.Getenv("RSB0T_CONTENT")
	if root == "" {
		t.Skip("set RSB0T_CONTENT to a Content checkout to count the monsters near the routes")
	}
	pack, err := os.ReadFile(root + "/pack/npc.pack")
	if err != nil {
		t.Skip(err)
	}
	src, err := os.ReadFile(root + "/maps/" + square + ".jm2")
	if err != nil {
		t.Skip(err)
	}
	ids := map[string]string{}
	for _, l := range strings.Split(strings.ReplaceAll(string(pack), "\r\n", "\n"), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			ids[k] = v
		}
	}
	out := map[string][]tile.Tile{}
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "==== ") {
			in = line == "==== NPC ===="
			continue
		}
		head, tail, ok := strings.Cut(line, ":")
		if !in || !ok {
			continue
		}
		var lvl, lx, lz int
		fmt.Sscanf(head, "%d %d %d", &lvl, &lx, &lz)
		fields := strings.Fields(tail)
		for _, n := range names {
			if len(fields) > 0 && ids[fields[0]] == n {
				out[n] = append(out[n], tile.Tile{X: mx<<6 + lx, Z: mz<<6 + lz, Level: lvl})
			}
		}
	}
	return out
}

// The hunters that can reach the routes: the bats' wander (8) and hunt range (2) put them within ten tiles of a route tile, and the plan
// prices CannonBatsAtOnce of them at a time. At most that many are within ten tiles of any tile of the cave's route (in and out), the camp's
// railings are out of every bat's reach, and the Commander's tile and the shed's stand have one bat within ten.
func TestDwarfCannonTheHuntersTheRoutesMeet(t *testing.T) {
	p, _ := mcPlanner(t)
	in := cannonRoute(p, questplan.CannonCaveArrive, tile.Tile{X: 2570, Z: 9850})
	out := cannonRoute(p, tile.Tile{X: 2571, Z: 9851}, questplan.CannonCaveArrive)
	if len(in) < 50 || len(out) < 50 {
		t.Fatalf("the routes are %d and %d tiles", len(in), len(out))
	}
	sp := cannonSpawns(t, "m40_153", 40, 153, "bat", "dungeon_rat")
	const reach = 8 + 2
	maxBats, reachBats := 0, map[tile.Tile]bool{}
	for _, r := range append(append([]tile.Tile{}, in...), out...) {
		n := 0
		for _, s := range sp["bat"] {
			if tile.Chebyshev(s, r) <= reach {
				n++
				reachBats[s] = true
			}
		}
		maxBats = max(maxBats, n)
	}
	t.Logf("%d bats can reach the cave's route, at most %d at one tile of it", len(reachBats), maxBats)
	if maxBats > questplan.CannonBatsAtOnce {
		t.Errorf("%d bats within %d tiles of one route tile, the plan prices %d at once", maxBats, reach, questplan.CannonBatsAtOnce)
	}
	if len(sp["bat"]) == 0 || len(sp["dungeon_rat"]) == 0 {
		t.Errorf("the cave census is %v", sp)
	}
	camp := cannonSpawns(t, "m40_54", 40, 54, "bat")
	for i, spot := range questplan.CannonRailingSpots {
		for _, b := range camp["bat"] {
			if d := tile.Chebyshev(b, spot); d <= reach {
				t.Errorf("railing %d at %v is %d tiles from a bat at %v: within its reach", i+1, spot, d, b)
			}
		}
	}
	near := 0
	for _, b := range camp["bat"] {
		if tile.Chebyshev(b, questplan.CannonCommanderAnchor) <= reach {
			near++
		}
	}
	if near > questplan.CannonBatsAtOnce {
		t.Errorf("%d bats within %d of the Commander", near, reach)
	}
}
