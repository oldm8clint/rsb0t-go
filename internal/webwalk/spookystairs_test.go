package webwalk

import (
	"os"
	"strings"
	"sync"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
)

// The spooky staircases (content's spookystairs 1730 and spookystairstop 1731,
// scripts/_unpack/225/all.loc) as content defines them. Every placement is angle 0, shape 10
// (cmd/findloc on the production loc index). forceapproach is stored as the content
// compiler writes it: 0b1111 with the named side's bit cleared, so the named side is the one
// left legal (engine tools/pack/config/LocConfig.ts).
var spookyStairs = map[int]struct{ width, length, forceApproach int }{
	1730: {2, 3, 0b1111 &^ locreach.BlockSouth}, // forceapproach=south
	1731: {2, 2, 0b1111 &^ locreach.BlockNorth}, // forceapproach=north
}

// Every spooky-staircase edge must stand on a tile the engine's reach test accepts.
// Draynor Manor's Walk-down stood at 3108,3367,1, one row north of the only legal row, and
// three Holy Grail accounts were refused there more than 5,000 times on 2026-10-04 without
// ever coming down.
func TestSpookyStairEdgesStandWhereTheServerReaches(t *testing.T) {
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]int{}
	for i := range g.Edges {
		e := &g.Edges[i]
		st, ok := spookyStairs[e.LocID]
		if !ok {
			continue
		}
		seen[e.LocID]++
		l := locreach.Loc{X: e.LocX, Z: e.LocZ, Level: e.From.Level, Width: st.width, Length: st.length, Shape: 10, ForceApproach: st.forceApproach}
		reached, modelled := locreach.Reached(e.From.X, e.From.Z, l)
		if !modelled || !reached {
			t.Errorf("%s stands where the server refuses the op; legal stands are %v", e, locreach.Tiles(l)[:2])
		}
	}
	// Draynor, Witch's House and the Grail castle: two baked edges per staircase.
	if seen[1730] != 6 || seen[1731] != 6 {
		t.Errorf("spooky staircase edges: %d up, %d down, want 6 and 6 (did the bake change?)", seen[1730], seen[1731])
	}

	// The exact tile the trapped accounts stood on now has the descent: an account there plans
	// one step south and then the hop.
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.LocID == 1731 && e.LocX == 3108 && e.LocZ == 3364 && e.From != (Point{X: 3108, Z: 3366, Level: 1}) {
			t.Errorf("Draynor Manor's Walk-down still stands at %s", e.From)
		}
	}
}

var realLocs struct {
	once sync.Once
	ix   *locindex.Index
	cfg  *cachecfg.Config
	err  error
}

func realLocData(t *testing.T) (*locindex.Index, *cachecfg.Config) {
	t.Helper()
	lp, cp := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if lp == "" || cp == "" {
		t.Skip("RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG not set")
	}
	realLocs.once.Do(func() {
		if realLocs.ix, realLocs.err = locindex.Load(lp); realLocs.err != nil {
			return
		}
		realLocs.cfg, realLocs.err = cachecfg.LoadFile(cp)
	})
	if realLocs.err != nil {
		t.Fatal(realLocs.err)
	}
	return realLocs.ix, realLocs.cfg
}

// The same check against the production loc index and config instead of the table above, so
// a placement or type that differs from content is caught. Skips without RSB0T_LOC_INDEX and
// RSB0T_CONFIG_JAG.
func TestSpookyStairEdgesOnTheRealLocs(t *testing.T) {
	ix, cfg := realLocData(t)
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for i := range g.Edges {
		e := &g.Edges[i]
		want, ok := spookyStairs[e.LocID]
		if !ok {
			continue
		}
		var m *locindex.Match
		for _, c := range ix.Find(cfg, locindex.Query{X: e.LocX, Z: e.LocZ, Level: e.From.Level, IDs: []int{e.LocID}}, nil) {
			if c.X == e.LocX && c.Z == e.LocZ {
				cc := c
				m = &cc
				break
			}
		}
		if m == nil {
			t.Errorf("%s: no loc %d at %d,%d,%d in the index", e, e.LocID, e.LocX, e.LocZ, e.From.Level)
			continue
		}
		typ, _ := cfg.Loc(m.ID)
		if typ.Width != want.width || typ.Length != want.length || typ.ForceApproach != want.forceApproach || m.Angle != 0 || m.Shape != 10 {
			t.Errorf("loc %d at %d,%d: %dx%d forceapproach %d angle %d shape %d, want %dx%d %d angle 0 shape 10",
				m.ID, m.X, m.Z, typ.Width, typ.Length, typ.ForceApproach, m.Angle, m.Shape, want.width, want.length, want.forceApproach)
		}
		l := locreach.Loc{X: m.X, Z: m.Z, Level: m.Level, Width: typ.Width, Length: typ.Length, Angle: m.Angle, Shape: m.Shape, ForceApproach: typ.ForceApproach}
		if reached, modelled := locreach.Reached(e.From.X, e.From.Z, l); !modelled || !reached {
			t.Errorf("%s stands where the server refuses the op", e)
		}
		if !strings.EqualFold(typ.Name, "Staircase") {
			t.Errorf("loc %d is %q, not a Staircase", m.ID, typ.Name)
		}
		checked++
	}
	if checked != 12 {
		t.Errorf("checked %d spooky staircase edges, want 12", checked)
	}
}

// On the real pack, an account anywhere upstairs in Draynor Manor plans its way down through
// 3108,3366,1, and that tile has no wall on its south face (reachRectangle1 checks the
// source tile's WALL_SOUTH before accepting the north side). Skips without
// RSB0T_COLLISION_PACK.
func TestDraynorManorUpstairsPlansDownFromTheLegalStand(t *testing.T) {
	p := realPack(t)
	stand := Point{X: 3108, Z: 3366, Level: 1}
	if p.col.WallMask(stand.X, stand.Z, stand.Level)&(1<<2) != 0 {
		t.Fatalf("%s has a south wall, so the server would refuse it too", stand)
	}
	// Where the three accounts were trapped, the whistles' corner and the room's west side.
	for _, from := range []Point{{X: 3108, Z: 3367, Level: 1}, {X: 3109, Z: 3369, Level: 1}, {X: 3105, Z: 3366, Level: 1}} {
		if !p.col.Walkable(from.X, from.Z, from.Level) {
			t.Errorf("%s is not walkable in the pack", from)
			continue
		}
		r := p.Plan(from, Point{X: 3244, Z: 3206, Level: 0}, Options{})
		if !r.OK {
			t.Errorf("from %s: %s", from, r.Reason)
			continue
		}
		down := hopWith(&r, 1731, "Walk-down")
		if down == nil {
			t.Errorf("from %s the route does not take the spooky stairs down: %s", from, r.String())
			continue
		}
		if down.From != stand {
			t.Errorf("from %s the stairs are taken from %s, want %s", from, down.From, stand)
		}
	}
}

// approachWorld is a fakeWorld that is also an ApproachClient.
type approachWorld struct {
	*fakeWorld
	tiles []Point
	asked int
}

func (a *approachWorld) Approaches(LocMatch) []Point { a.asked++; return a.tiles }

// A 2x2 top-of-stairs loc operable only from the row north of it, planned from one row
// further north: Draynor Manor's 1731 at 3108,3364,1 and the trapped accounts' 3108,3367,1,
// moved to the origin (2,2). The origin's four neighbours are inside the loc or refused, so
// only a walker that asks the Client where the server reaches the loc gets down.
func TestWalkerRetriesA2x2StaircaseFromTheTilesTheServerReaches(t *testing.T) {
	build := func(withApproaches bool) (*approachWorld, *fakeWorld, *Walker, *[]string) {
		col := newGrid().fill(0, 0, 6, 6, 0).fill(0, 0, 6, 6, 1)
		for _, p := range []Point{{X: 2, Z: 2, Level: 1}, {X: 3, Z: 2, Level: 1}, {X: 2, Z: 3, Level: 1}, {X: 3, Z: 3, Level: 1}} {
			delete(col.walk, p)
		}
		f := newFakeWorld(t, col, Point{X: 2, Z: 5, Level: 1})
		f.ladders = append(f.ladders, &fakeLadder{loc: Point{X: 2, Z: 2, Level: 1}, stand: Point{X: 2, Z: 4, Level: 1}, landing: Point{X: 2, Z: 1, Level: 0}, id: 1731, name: "Staircase", op: "Walk-down"})
		g := &Graph{Edges: []Edge{{From: Point{X: 2, Z: 5, Level: 1}, To: Point{X: 2, Z: 1, Level: 0}, Kind: KindStair, Cost: 6, LocName: "Staircase", Action: "Walk-down", LocID: 1731, OpenLocID: -1, LocX: 2, LocZ: 2}}}
		var c Client = f
		var aw *approachWorld
		if withApproaches {
			l := locreach.Loc{X: 2, Z: 2, Level: 1, Width: 2, Length: 2, Shape: 10, ForceApproach: spookyStairs[1731].forceApproach}
			var tiles []Point
			for _, tl := range locreach.Tiles(l) {
				tiles = append(tiles, Point{X: tl.X, Z: tl.Z, Level: tl.Level})
			}
			aw = &approachWorld{fakeWorld: f, tiles: tiles}
			c = aw
		}
		w := NewWalker(NewPlanner(col, g), c)
		logs := &[]string{}
		w.Log = func(s string) { *logs = append(*logs, s); t.Log(s) }
		return aw, f, w, logs
	}

	// Without the reach tiles the walker cannot get down: this is the live failure.
	_, f, w, _ := build(false)
	if res := w.WalkTo(Point{X: 5, Z: 0, Level: 0}, WalkOptions{}); res.Arrived() || f.me.Level != 1 {
		t.Fatalf("without Approaches the walker got down (%+v at %s); the fixture no longer reproduces the live failure", res, f.me)
	}

	aw, f, w, logs := build(true)
	if res := w.WalkTo(Point{X: 5, Z: 0, Level: 0}, WalkOptions{}); !res.Arrived() {
		t.Fatalf("result = %+v, player at %s", res, f.me)
	}
	if aw.asked == 0 {
		t.Error("the walker never asked for the loc's approach tiles")
	}
	if all := strings.Join(*logs, "\n"); !strings.Contains(all, "only answered from 2,4,1") {
		t.Errorf("the working stand was not reported:\n%s", all)
	}
}
