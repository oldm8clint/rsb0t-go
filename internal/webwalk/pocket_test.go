package webwalk

import (
	"strings"
	"testing"
)

// ring is four strips of zone around the 3x3 interior x 4..6, z 9..11: a pocket whose tiles are
// outside every rectangle, so the endpoint exemption never frees a walk that starts there.
var ring = []Rect{
	{MinX: 3, MaxX: 7, MinZ: 8, MaxZ: 8, Level: 0},
	{MinX: 3, MaxX: 7, MinZ: 12, MaxZ: 12, Level: 0},
	{MinX: 3, MaxX: 3, MinZ: 9, MaxZ: 11, Level: 0},
	{MinX: 7, MaxX: 7, MinZ: 9, MaxZ: 11, Level: 0},
}

// withTestZone registers ring as a catalog zone for the length of one test.
func withTestZone(t *testing.T) {
	was := KnownDangerZones
	KnownDangerZones = append(append([]DangerZone(nil), was...), DangerZone{ID: "test-ring", Rects: ring, AllowWhenEndpointInside: true})
	t.Cleanup(func() { KnownDangerZones = was })
}

// TestWalkerWalksOutOfAPocketTheZonesWallIn is wcrg13's ledge in miniature: a start the zones
// surround, with every way out crossing one. Before planOutOfPocket the walk failed outright.
func TestWalkerWalksOutOfAPocketTheZonesWallIn(t *testing.T) {
	withTestZone(t)
	f, w, logs := field(t, Point{X: 5, Z: 10})
	if r := w.Planner.Plan(f.me, Point{X: 40, Z: 10}, Options{AvoidZones: ring}); r.OK {
		t.Fatal("control: the zone-aware search should find no way out of the ring")
	}
	res := w.WalkTo(Point{X: 40, Z: 10}, WalkOptions{AvoidZones: []string{"test-ring"}})
	if !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	all := strings.Join(*logs, "\n")
	if !strings.Contains(all, "wall in a pocket") {
		t.Error("the escape was not logged")
	}
	// Without the escape the planned walk fails and only the ladder's scene rung, which ignores
	// every zone, gets the account moving; on real terrain it did not (wcrg13).
	if strings.Contains(all, "baked walk failed") {
		t.Errorf("the planned walk failed and fell back to the ladder:\n%s", all)
	}
}

// TestCallerRectsStayHardInAPocket: a rectangle the caller named is not the catalog's to set
// aside, so planOutOfPocket leaves the same pocket walled by AvoidRects without a way out.
func TestCallerRectsStayHardInAPocket(t *testing.T) {
	f, w, _ := field(t, Point{X: 5, Z: 10})
	dest := Point{X: 40, Z: 10}
	opt := Options{AvoidZones: ring}
	failed := w.Planner.Plan(f.me, dest, opt)
	w.walk.zones, w.walk.catalog = ring, nil
	if r, _ := w.planOutOfPocket(f.me, dest, opt, failed); r.OK {
		t.Fatal("walked out through the caller's own rectangles")
	}
	w.walk.catalog = ring
	if r, _ := w.planOutOfPocket(f.me, dest, opt, failed); !r.OK {
		t.Fatalf("catalog zones: no way out: %s", r.Reason)
	}
}

// TestAPlainZoneWallIsNotAPocket: an ordinary catalog zone across the route, with room to walk
// round it, is still walked round rather than through.
func TestAPlainZoneWallIsNotAPocket(t *testing.T) {
	withTestZone(t)
	f, w, _ := field(t, Point{X: 0, Z: 0})
	var visited []Point
	f.onDelay = func(int) { visited = append(visited, f.me) }
	if res := w.WalkTo(Point{X: 40, Z: 10}, WalkOptions{AvoidZones: []string{"test-ring"}}); !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	for _, p := range visited {
		if InZones(p, ring) {
			t.Fatalf("walked into the zone at %s", p)
		}
	}
}

func TestRectsWithoutRemovesEachOnce(t *testing.T) {
	a, b := Rect{MinX: 1}, Rect{MinX: 2}
	got := rectsWithout([]Rect{a, b, a}, []Rect{a})
	if len(got) != 2 || got[0] != b || got[1] != a {
		t.Fatalf("got %v, want [b a]", got)
	}
}

// TestSceneRungStopsShortOfAZone: a zone wall across the whole field makes the destination
// unreachable, so the ladder falls through to its scene rung. That rung used to click straight
// through the wall; now no tile of the walk is inside it.
func TestSceneRungStopsShortOfAZone(t *testing.T) {
	f, w, logs := field(t, Point{X: 0, Z: 10})
	wall := []Rect{{MinX: 20, MaxX: 22, MinZ: 0, MaxZ: 20, Level: 0}}
	var visited []Point
	f.onDelay = func(int) { visited = append(visited, f.me) }
	if res := w.WalkTo(Point{X: 40, Z: 10}, WalkOptions{AvoidRects: wall, MaxPasses: 3}); res.Arrived() {
		t.Fatal("arrived through a zone that spans the field")
	}
	if !strings.Contains(strings.Join(*logs, "\n"), "scene") {
		t.Fatal("control: the ladder never reached its scene rung")
	}
	for _, p := range visited {
		if InZones(p, wall) {
			t.Fatalf("walked into the zone at %s", p)
		}
	}
}

func TestStopShortOfZones(t *testing.T) {
	wall := []Rect{{MinX: 20, MaxX: 22, MinZ: 0, MaxZ: 20, Level: 0}}
	me := Point{X: 10, Z: 10}
	if got := stopShortOfZones(me, Point{X: 30, Z: 14}, wall); got != (Point{X: 19, Z: 14}) {
		t.Fatalf("got %s, want 19,14,0 (diagonal first, then straight, stopping before x 20)", got)
	}
	if got := stopShortOfZones(me, Point{X: 15, Z: 10}, wall); got != (Point{X: 15, Z: 10}) {
		t.Fatalf("a click short of the zone was changed to %s", got)
	}
	inside := Point{X: 21, Z: 10}
	if got := stopShortOfZones(inside, Point{X: 30, Z: 10}, wall); got != (Point{X: 30, Z: 10}) {
		t.Fatalf("a player inside the zone was held back: %s", got)
	}
}

// TestPocketEscapeCrossesTheZoneRatherThanFollowingIt is the review's wolf-pass case: the zone
// walling the pocket in also runs as a band straight toward the destination. Planning the escape
// all the way to dest without the zones walked the length of the band; the escape now crosses to
// the nearest tile outside, and the rest of the walk goes round the band.
func TestPocketEscapeCrossesTheZoneRatherThanFollowingIt(t *testing.T) {
	band := append(append([]Rect(nil), ring...), Rect{MinX: 7, MaxX: 36, MinZ: 8, MaxZ: 12, Level: 0})
	was := KnownDangerZones
	KnownDangerZones = append(append([]DangerZone(nil), was...), DangerZone{ID: "test-band", Rects: band})
	t.Cleanup(func() { KnownDangerZones = was })

	f, w, _ := field(t, Point{X: 5, Z: 10})
	var visited []Point
	f.onDelay = func(int) { visited = append(visited, f.me) }
	if res := w.WalkTo(Point{X: 40, Z: 10}, WalkOptions{AvoidZones: []string{"test-band"}}); !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	inZone := map[Point]bool{}
	for _, p := range visited {
		if InZones(p, band) {
			inZone[p] = true
		}
	}
	if len(inZone) > 1 {
		t.Fatalf("walked %d zoned tiles (%v), want only the one crossing out of the pocket", len(inZone), inZone)
	}
}

func TestPocketExitIsNeverBackInThePocket(t *testing.T) {
	col := newGrid().fill(0, 0, 40, 20, 0)
	exit, ok := pocketExit(col, Point{X: 5, Z: 10}, Point{X: 40, Z: 10}, ring, nil)
	if !ok {
		t.Fatal("no exit found")
	}
	if InZones(exit, ring) || (exit.X >= 4 && exit.X <= 6 && exit.Z >= 9 && exit.Z <= 11) {
		t.Fatalf("exit %s is in the zone or back in the pocket", exit)
	}
	if Chebyshev(exit, Point{X: 5, Z: 10}) != 3 {
		t.Fatalf("exit %s is not the nearest way out (3 steps)", exit)
	}
}

// TestPocketExitSkipsAGapThatIsItselfWalledIn is the live ledge case in miniature: past the ring's
// east wall is a one-tile gap walled by a second strip, and only past that is open ground. The
// nearest tile outside the zones is that gap, from which the zones wall the player in again; the
// exit must be on the open ground beyond it, or the walk ping-pongs between the two.
func TestPocketExitSkipsAGapThatIsItselfWalledIn(t *testing.T) {
	col := newGrid().fill(0, 0, 40, 20, 0)
	// Everything west of x 10 is zoned except the pocket (x 4..6, z 9..11) and the gap 8,10,
	// so the gap (3 steps out) is strictly nearer than the open ground from x 10 (5 steps).
	zones := []Rect{
		{MinX: 0, MaxX: 3, MinZ: 0, MaxZ: 20, Level: 0},
		{MinX: 4, MaxX: 6, MinZ: 0, MaxZ: 8, Level: 0},
		{MinX: 4, MaxX: 6, MinZ: 12, MaxZ: 20, Level: 0},
		{MinX: 7, MaxX: 7, MinZ: 0, MaxZ: 20, Level: 0},
		{MinX: 8, MaxX: 8, MinZ: 0, MaxZ: 9, Level: 0},
		{MinX: 8, MaxX: 8, MinZ: 11, MaxZ: 20, Level: 0},
		{MinX: 9, MaxX: 9, MinZ: 0, MaxZ: 20, Level: 0},
	}
	exit, ok := pocketExit(col, Point{X: 5, Z: 10}, Point{X: 40, Z: 10}, zones, nil)
	if !ok {
		t.Fatal("no exit found")
	}
	if exit == (Point{X: 8, Z: 10}) {
		t.Fatalf("exit %s is the walled-in gap", exit)
	}
	if exit.X < 10 || InZones(exit, zones) {
		t.Fatalf("exit %s is not on the open ground", exit)
	}
}
