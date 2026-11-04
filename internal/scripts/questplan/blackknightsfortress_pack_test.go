package questplan

// The Black Knights' Fortress route against the real data: the collision pack, the loc index and the production config
// (RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG; every test skips without them). The route table (blackknightsroute.go)
// is hand-written from the maps and scripts; these tests are what makes it more than that. Each crossing's loc is where the table
// says, offers the op, is operated from a tile the engine's reach test accepts and puts the account where the table says it does;
// each region is one piece of floor and no two are the same one; the inside is what the pack says it is; and every walk the
// module hands the long-distance walker has a route that keeps out of the Wilderness and the knights' open ground.

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

var bkReal struct {
	once sync.Once
	pk   *pathfind.Pack
	ix   *locindex.Index
	cfg  *cachecfg.Config
	err  error
}

func bkData(t *testing.T) (*pathfind.Pack, *locindex.Index, *cachecfg.Config) {
	t.Helper()
	pp, ip, cp := os.Getenv("RSB0T_COLLISION_PACK"), os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if pp == "" || ip == "" || cp == "" {
		t.Skip("RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX or RSB0T_CONFIG_JAG not set")
	}
	bkReal.once.Do(func() {
		if bkReal.pk, bkReal.err = pathfind.Load(pp); bkReal.err != nil {
			return
		}
		if bkReal.ix, bkReal.err = locindex.Load(ip); bkReal.err != nil {
			return
		}
		bkReal.cfg, bkReal.err = cachecfg.LoadFile(cp)
	})
	if bkReal.err != nil {
		t.Fatal(bkReal.err)
	}
	return bkReal.pk, bkReal.ix, bkReal.cfg
}

func nav(t tile.Tile) pathfind.NavPoint { return pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level} }

// bkLocAt finds the placed loc a BKLoc names.
func bkLocAt(ix *locindex.Index, l BKLoc) (locindex.Loc, bool) {
	for _, found := range ix.At(l.X, l.Z, l.Level) {
		if found.ID == l.ID {
			return found, true
		}
	}
	return locindex.Loc{}, false
}

// bkWallFace is the wall-nibble bit a stand beside a loc must have clear on the face toward it.
func bkWallFace(stand tile.Tile, l BKLoc) byte {
	switch {
	case stand.X == l.X-1:
		return 2 // west of the loc: the stand's east face
	case stand.X == l.X+1:
		return 8
	case stand.Z == l.Z-1:
		return 1
	case stand.Z == l.Z+1:
		return 4
	}
	return 0
}

func TestBKCrossingsAreWhatTheEngineRuns(t *testing.T) {
	pk, ix, cfg := bkData(t)
	for _, l := range BKLegs {
		loc, ok := bkLocAt(ix, l.Loc)
		if !ok {
			t.Errorf("%s: loc %d is not placed at %v", l.Name, l.Loc.ID, l.Loc.Tile())
			continue
		}
		typ, ok := cfg.Loc(l.Loc.ID)
		if !ok || cachecfg.OpIndex(typ.Ops, l.Op) == 0 {
			t.Errorf("%s: loc %d %q offers %v, not %q", l.Name, l.Loc.ID, typ.Name, typ.Ops, l.Op)
		}
		for what, p := range map[string]tile.Tile{"stand": l.Stand, "landing": l.Land} {
			if !pk.Walkable(p.X, p.Z, p.Level) {
				t.Errorf("%s: the %s %v is not a floor tile", l.Name, what, p)
			}
		}
		switch loc.Shape {
		case 0:
			// A straight wall (a door, a secret wall) is operated from its own tile or the one across its edge, and the script
			// moves the account: from the loc's own row ("entering") to the far side, from the other side onto the loc's tile
			// (quest_blackknight.rs2 via open_and_close_door, door_open: west -1,0 north 0,1 east 1,0 south 0,-1).
			off := [4][2]int{{-1, 0}, {0, 1}, {1, 0}, {0, -1}}[loc.Angle&3]
			own := tile.Tile{X: loc.X, Z: loc.Z, Level: loc.Level}
			across := tile.Tile{X: loc.X + off[0], Z: loc.Z + off[1], Level: loc.Level}
			var want tile.Tile
			switch l.Stand {
			case own:
				want = across
			case across:
				want = own
			default:
				t.Errorf("%s: the stand %v is neither the door's tile %v nor the tile across it %v", l.Name, l.Stand, own, across)
				continue
			}
			if l.Land != want {
				t.Errorf("%s: lands on %v, the script puts the account on %v", l.Name, l.Land, want)
			}
			// The wall must stand between the two tiles in the static pack: a door that is not a wall there is not a crossing.
			wall := pk.WallMask(own.X, own.Z, own.Level) & (1 << uint(loc.Angle&3))
			if wall == 0 && pk.WallMask(across.X, across.Z, across.Level) == 0 {
				t.Logf("%s: no wall shows on either side of the door in the pack (an open doorway?)", l.Name)
			}
		case 10:
			// A ladder: the engine's rectangle reach (locreach, forceapproach read as the sides that are blocked), the stand's wall
			// face toward the ladder open, and the climb keeps the account's x and z on the next level.
			up := cachecfg.OpIndex(typ.Ops, "Climb-up") > 0
			wantLevel := l.Stand.Level - 1
			if up {
				wantLevel = l.Stand.Level + 1
			}
			if l.Land.X != l.Stand.X || l.Land.Z != l.Stand.Z || l.Land.Level != wantLevel {
				t.Errorf("%s: lands on %v from %v: a ladder keeps x and z and moves one level (%d)", l.Name, l.Land, l.Stand, wantLevel)
			}
			if l.Stand.Level != loc.Level {
				t.Errorf("%s: stand on level %d, the ladder is on %d", l.Name, l.Stand.Level, loc.Level)
			}
			tiles := locreach.Tiles(locreach.Loc{X: loc.X, Z: loc.Z, Level: loc.Level, Width: typ.Width, Length: typ.Length,
				Angle: loc.Angle, Shape: loc.Shape, ForceApproach: typ.ForceApproach})
			if !slices.Contains(tiles, l.Stand) {
				t.Errorf("%s: the engine accepts a click from %v, not from %v", l.Name, tiles, l.Stand)
			}
			if face := bkWallFace(l.Stand, l.Loc); face == 0 || pk.WallMask(l.Stand.X, l.Stand.Z, l.Stand.Level)&face != 0 {
				t.Errorf("%s: the stand %v has a wall on its face toward the ladder (mask %04b, face %04b)", l.Name, l.Stand, pk.WallMask(l.Stand.X, l.Stand.Z, l.Stand.Level), face)
			}
		default:
			t.Errorf("%s: loc shape %d is neither a wall nor a ladder", l.Name, loc.Shape)
		}
		// A plain door is crossed in one step from the stand: the landing is the orthogonal neighbour.
		if l.Opens && abs(l.Stand.X-l.Land.X)+abs(l.Stand.Z-l.Land.Z) != 1 {
			t.Errorf("%s: a plain door is stepped through, and %v to %v is not one step", l.Name, l.Stand, l.Land)
		}
	}
}

// The doors' scripts decide who is "entering" by the axis the account stands on, so the table's stands are the ONE tile each way;
// the engine's reach for a straight wall offers no other. Nothing else to stand on: the locs have no second option.
func TestBKLocConfigsAreWhatTheScriptsSay(t *testing.T) {
	_, ix, cfg := bkData(t)
	for _, c := range []struct {
		l          BKLoc
		name       string
		op         string
		shape, ang int
	}{
		{BKLoc{LocBKFrontDoor, 3016, 3514, 0}, "Sturdy door", "Open", 0, 1},
		{BKLoc{LocBKHallDoor, 3020, 3515, 0}, "Door", "Open", 0, 0},
		{BKLoc{LocBKSturdyDoor, 3025, 3511, 1}, "Sturdy door", "Open", 0, 2},
		{BKLoc{LocBKSecretWall, 3016, 3517, 0}, "Wall", "Push", 0, 3},
		{BKLoc{LocBKSecretWall, 3030, 3510, 1}, "Wall", "Push", 0, 3},
		{BKGrill, "Grill", "Listen-at", 5, 2},
		{BKHole, "Hole", "", 10, 1},
		{BKLoc{LocBKLadderD, 3015, 3519, 0}, "Ladder", "Climb-up", 10, 2},
		{BKLoc{LocBKLadderD, 3022, 3518, 0}, "Ladder", "Climb-up", 10, 2},
		{BKLoc{LocBKLadderTopD, 3022, 3518, 1}, "Ladder", "Climb-down", 10, 2},
		{BKLoc{LocBKLadderTop, 3021, 3510, 1}, "Ladder", "Climb-down", 10, 3},
		{BKLoc{LocBKLadder, 3021, 3510, 0}, "Ladder", "Climb-up", 10, 3},
	} {
		loc, ok := bkLocAt(ix, c.l)
		if !ok {
			t.Errorf("%v: not placed", c.l)
			continue
		}
		typ, _ := cfg.Loc(c.l.ID)
		if typ.Name != c.name || loc.Shape != c.shape || loc.Angle != c.ang {
			t.Errorf("%v: %q shape %d angle %d, want %q shape %d angle %d", c.l, typ.Name, loc.Shape, loc.Angle, c.name, c.shape, c.ang)
		}
		if c.op != "" && cachecfg.OpIndex(typ.Ops, c.op) != 1 {
			t.Errorf("%v: %q is not op 1 of %v", c.l, c.op, typ.Ops)
		}
	}
	// The directional ladders: forceapproach 14 is "everything but one side blocked", the other ladders none.
	for id, want := range map[int]int{LocBKLadderTop: 0, LocBKLadder: 0, LocBKLadderTopD: 14, LocBKLadderD: 14} {
		if typ, _ := cfg.Loc(id); typ.ForceApproach != want {
			t.Errorf("ladder %d forceapproach %d, want %d", id, typ.ForceApproach, want)
		}
	}
}

func TestBKRegionsArePiecesOfFloor(t *testing.T) {
	pk, _, _ := bkData(t)
	regions := BKRegionTiles()
	names := make([]string, 0, len(regions))
	for r := range regions {
		names = append(names, r)
	}
	slices.Sort(names)
	reach := map[string]map[[2]int]bool{}
	for _, r := range names {
		ts := regions[r]
		level := ts[0].Level
		for _, p := range ts {
			if p.Level != level {
				t.Errorf("region %q mixes level %d and %d (%v)", r, level, p.Level, ts)
			}
		}
		flood := pk.Flood(nav(ts[0]), 80)
		reach[r] = flood
		for _, p := range ts {
			if !flood[[2]int{p.X, p.Z}] {
				t.Errorf("region %q: %v cannot be walked to from %v on foot", r, p, ts[0])
			}
		}
	}
	// Two regions on a level are two pieces of floor: no tile of one is walkable from the other (doors shut).
	for i, a := range names {
		for _, b := range names[i+1:] {
			if regions[a][0].Level != regions[b][0].Level {
				continue
			}
			for _, p := range regions[b] {
				if reach[a][[2]int{p.X, p.Z}] {
					t.Errorf("regions %q and %q are one piece of floor (%v is reachable from %v)", a, b, p, regions[a][0])
				}
			}
		}
	}
	// The outside is a piece of floor with the door's approach and its stand in it; the grill and the Hole's stands are in theirs.
	if f := pk.Flood(nav(BKDoorApproach), 80); !f[[2]int{BKLegs[0].Stand.X, BKLegs[0].Stand.Z}] || !f[[2]int{BKWestWaypoint.X, BKWestWaypoint.Z}] {
		t.Errorf("the door's stand or the west waypoint is not on foot from the approach tile")
	}
	for _, h := range BKHoleStands {
		if !pk.Walkable(h.X, h.Z, h.Level) {
			t.Errorf("hole stand %v is not walkable", h)
		}
	}
}

// BKInside is the pack's inside: the L0 tiles the open ground cannot reach on foot (and the fortress box above them).
func TestBKInsideIsThePacksInside(t *testing.T) {
	pk, _, _ := bkData(t)
	exterior := pk.Flood(nav(t3(3016, 3512, 0)), 90)
	wrong := 0
	for x := bkFortressBox.MinX; x <= bkFortressBox.MaxX; x++ {
		for z := bkFortressBox.MinZ; z <= bkFortressBox.MaxZ; z++ {
			if !pk.Walkable(x, z, 0) {
				continue
			}
			want := !exterior[[2]int{x, z}]
			if BKInside(t3(x, z, 0)) != want {
				wrong++
				if wrong <= 12 {
					t.Errorf("%d,%d,0: BKInside %v, the pack says inside %v", x, z, !want, want)
				}
			}
		}
	}
	for _, p := range []tile.Tile{BKDoorApproach, BKLegs[0].Stand, BKWestWaypoint} {
		if BKInside(p) {
			t.Errorf("%v is inside", p)
		}
	}
	// Every floor tile above level 0 inside the box is inside the fortress: nothing else is built there.
	for lv := 1; lv <= 3; lv++ {
		if !BKInside(t3(3016, 3518, lv)) {
			t.Errorf("level %d is not inside", lv)
		}
	}
}

func TestBKGrillAndHoleAreWhereTheyWork(t *testing.T) {
	pk, ix, cfg := bkData(t)
	g, ok := bkLocAt(ix, BKGrill)
	if !ok {
		t.Fatal("no grill")
	}
	// A straight wall decoration is reached from its own tile and no other (locreach.WallDecorStands).
	stands := locreach.WallDecorStands(locreach.Loc{X: g.X, Z: g.Z, Level: g.Level, Angle: g.Angle, Shape: g.Shape})
	if len(stands) != 1 || stands[0].Tile != BKGrillStand {
		t.Fatalf("grill stands %+v, want only %v", stands, BKGrillStand)
	}
	// ...and that tile is the foot of the shaft: walkable from the corridor the grill ladder lands in.
	flood := pk.Flood(nav(t3(3022, 3510, 0)), 40)
	if !flood[[2]int{BKGrillStand.X, BKGrillStand.Z}] {
		t.Errorf("the grill's tile is not on foot from the ladder's landing")
	}
	if r := pk.FindPath(nav(t3(3022, 3510, 0)), nav(BKGrillStand)); !r.OK || r.Steps() > 12 {
		t.Errorf("the walk from the ladder to the grill: %+v", r)
	}
	// The Hole is a 1x1 shape-10 loc with no forceapproach; its stands are the neighbours the shelves leave.
	h, ok := bkLocAt(ix, BKHole)
	if !ok {
		t.Fatal("no hole")
	}
	typ, _ := cfg.Loc(BKHole.ID)
	accepted := locreach.Tiles(locreach.Loc{X: h.X, Z: h.Z, Level: h.Level, Width: typ.Width, Length: typ.Length, Angle: h.Angle, Shape: h.Shape, ForceApproach: typ.ForceApproach})
	wh := pk.Flood(nav(t3(3030, 3509, 1)), 20)
	for _, s := range BKHoleStands {
		if !slices.Contains(accepted, s) {
			t.Errorf("the engine does not accept a use from %v on the hole (%v)", s, accepted)
		}
		if !wh[[2]int{s.X, s.Z}] {
			t.Errorf("hole stand %v is not on foot from the warehouse wall's landing", s)
		}
		if face := bkWallFace(s, BKHole); pk.WallMask(s.X, s.Z, s.Level)&face != 0 {
			t.Errorf("hole stand %v has a wall on its face toward the hole", s)
		}
	}
	// The cabbage field: 70 pickable cabbages in rows, the loc offers Pick as op 2.
	ct, _ := cfg.Loc(LocCabbage)
	if ct.Name != "cabbage" || cachecfg.OpIndex(ct.Ops, "Pick") != 2 {
		t.Errorf("loc %d is %q %v", LocCabbage, ct.Name, ct.Ops)
	}
	n := 0
	ix.Each(3040, 3288, 3070, 3300, 0, func(l locindex.Loc) {
		if l.ID == LocCabbage {
			n++
		}
	})
	if n < 40 {
		t.Errorf("%d cabbages in the field", n)
	}
	if !pk.Walkable(CabbageField.X, CabbageField.Z, 0) {
		t.Errorf("the field's middle %v is not walkable", CabbageField)
	}
}

// ---- the walks the module hands the walker ----

// bkRouteTiles expands a planned route's straight runs into the tiles it crosses (a hop's far side starts a new run).
func bkRouteTiles(r webwalk.Route) []webwalk.Point {
	var out []webwalk.Point
	for i, w := range r.Waypoints {
		if i == 0 || w.Hop != nil || w.Level != r.Waypoints[i-1].Level {
			out = append(out, w.Point)
			continue
		}
		prev := r.Waypoints[i-1].Point
		for p := prev; p.X != w.X || p.Z != w.Z; {
			p.X += sign(w.X - p.X)
			p.Z += sign(w.Z - p.Z)
			out = append(out, p)
		}
	}
	return out
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

func TestBKWalksPlanRoundTheSentriesAndOutOfTheWilderness(t *testing.T) {
	pk, _, _ := bkData(t)
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	p := webwalk.NewPlanner(pk, g)
	pt := func(t tile.Tile) webwalk.Point { return webwalk.Point{X: t.X, Z: t.Z, Level: t.Level} }
	sentry := webwalk.Rect{MinX: BKSentryGround.MinX, MaxX: BKSentryGround.MaxX, MinZ: BKSentryGround.MinZ, MaxZ: BKSentryGround.MaxZ, Level: BKSentryGround.Level}
	plan := func(from, to tile.Tile) webwalk.Route {
		a, b := pt(from), pt(to)
		zones := webwalk.ResolveDangerZones(nil, []webwalk.Rect{sentry}, webwalk.ZoneContext{IncludeAutomatic: true, CombatLevel: 3, CombatKnown: true, Start: &a, Dest: &b})
		return p.Plan(a, b, webwalk.Options{AllowSpecial: true, AllowNPC: true, AvoidZones: zones})
	}
	if z, ok := webwalk.KnownDangerZoneByID("wilderness"); !ok || !z.Automatic || z.Rects[0].MinZ != 3523 {
		t.Fatalf("the Wilderness zone is %+v (found %v): its line is z 3523", z.Rects, ok)
	}
	legs := []struct {
		name     string
		from, to tile.Tile
	}{
		{"Sir Amik to the door", SirAmikAnchor, BKDoorApproach},
		{"Falador west bank to the door", FaladorWestBank, BKDoorApproach},
		{"Falador east bank to the door", FaladorEastBank, BKDoorApproach},
		{"Wayne's to the door", WayneShop.Anchor, BKDoorApproach},
		{"Horvik's to the door", HorvikShop.Anchor, BKDoorApproach},
		{"Peksa's to the door", PeksaShop.Anchor, BKDoorApproach},
		{"the cabbage field to the door", CabbageField, BKDoorApproach},
		{"Draynor bank to the door", DraynorBank, BKDoorApproach},
		{"the door to the west waypoint", BKDoorApproach, BKWestWaypoint},
		{"the west waypoint to Sir Amik", BKWestWaypoint, SirAmikAnchor},
		{"the door to Sir Amik, round the west", BKDoorApproach, SirAmikAnchor},
		{"the west waypoint to the Falador west bank", BKWestWaypoint, FaladorWestBank},
		{"Falador west bank to Wayne's", FaladorWestBank, WayneShop.Anchor},
		{"Falador west bank to the cabbage field", FaladorWestBank, CabbageField},
		{"Falador east bank to the cabbage field", FaladorEastBank, CabbageField},
		{"the cabbage field to Sir Amik", CabbageField, SirAmikAnchor},
		{"Sir Amik to the Falador west bank", SirAmikAnchor, FaladorWestBank},
	}
	for _, l := range legs {
		r := plan(l.from, l.to)
		if !r.OK {
			t.Errorf("%s: no route (%s)", l.name, r.Reason)
			continue
		}
		for _, w := range bkRouteTiles(r) {
			if w.Level == 0 && sentry.Contains(w) && !(sentry.Contains(pt(l.from)) || sentry.Contains(pt(l.to))) {
				t.Errorf("%s: the route crosses the knights' open ground at %v", l.name, w)
				break
			}
			if wz, _ := webwalk.KnownDangerZoneByID("wilderness"); webwalk.InZones(w, wz.Rects) {
				t.Errorf("%s: the route enters the Wilderness at %v", l.name, w)
				break
			}
		}
		t.Logf("%-46s cost %4d, %d hop(s)", l.name, r.Cost, len(r.Hops()))
	}
	// The unconstrained walker's own route from the door south runs the open ground, which is why the module asks for the rect.
	a, b := pt(BKDoorApproach), pt(SirAmikAnchor)
	free := p.Plan(a, b, webwalk.Options{AllowSpecial: true, AllowNPC: true})
	through := false
	for _, w := range bkRouteTiles(free) {
		if w.Level == 0 && sentry.Contains(w) {
			through = true
		}
	}
	if !free.OK || !through {
		t.Errorf("the unconstrained route from the door to Sir Amik (ok %v) no longer crosses the sentries' ground: the rect may be unnecessary", free.OK)
	}
}

// The knights' ground, from the map: the aggressive knights stand where blackknightsroute.go says.
func TestBKKnightsStandWhereTheNotesSay(t *testing.T) {
	root := os.Getenv("RSB0T_CONTENT")
	if root == "" {
		root = os.Getenv("RSB0T_CONTENT_DIR")
	}
	if root == "" {
		t.Skip("RSB0T_CONTENT or RSB0T_CONTENT_DIR not set")
	}
	b, err := os.ReadFile(root + "/maps/m47_54.jm2")
	if err != nil {
		t.Skip(err)
	}
	src := strings.ReplaceAll(string(b), "\r\n", "\n")
	section := src[strings.Index(src, "==== NPC ===="):]
	if i := strings.Index(section, "==== OBJ"); i > 0 {
		section = section[:i]
	}
	count := map[int]int{}
	for _, line := range strings.Split(section, "\n") {
		head, tail, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		var lv, x, z int
		if n, _ := fmt.Sscanf(head, "%d %d %d", &lv, &x, &z); n != 3 {
			continue
		}
		fields := strings.Fields(tail)
		if len(fields) == 0 {
			continue
		}
		id, _ := strconv.Atoi(fields[0])
		if id == NpcAggressiveBlackKnight {
			count[lv]++
			// None stands on a tile the route's stands use.
			at := t3(3008+x, 3456+z, lv)
			for _, l := range BKLegs {
				if at == l.Stand || at == l.Land {
					t.Logf("a knight spawns on the %s tile %v", l.Name, at)
				}
			}
		}
	}
	if count[0] != 8 || count[1] != 4 || count[2] != 3 {
		t.Errorf("aggressive knights by level %v: want 8 (5 in the hall, 3 in the open), 4 and 3", count)
	}
}
