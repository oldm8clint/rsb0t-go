package webwalk

import "testing"

// Plan sees exactly the zones WalkTo would, and does not move the player.
func TestPlanPreviewsTheWalkWithoutMoving(t *testing.T) {
	f, w, _ := field(t, Point{X: 0, Z: 10})
	// A wall of zone across the whole field: no route can get past it.
	wall := []Rect{{MinX: 20, MaxX: 22, MinZ: 0, MaxZ: 20, Level: 0}}
	dest := Point{X: 40, Z: 10}
	if r := w.Plan(dest, WalkOptions{AvoidRects: wall}); r.OK {
		t.Fatalf("planned through a zone that spans the field: %s", r.String())
	}
	if f.walks != 0 || f.me != (Point{X: 0, Z: 10}) {
		t.Fatalf("Plan moved the player: %d walk clicks, now at %s", f.walks, f.me)
	}
	// A gap in the zone is found, and the route stays out of both halves.
	gapped := []Rect{{MinX: 20, MaxX: 22, MinZ: 0, MaxZ: 14, Level: 0}, {MinX: 20, MaxX: 22, MinZ: 18, MaxZ: 20, Level: 0}}
	r := w.Plan(dest, WalkOptions{AvoidRects: gapped})
	if !r.OK {
		t.Fatalf("no route through the gap: %s", r.Reason)
	}
	for _, p := range expand(stepPoints(r.Waypoints)) {
		if InZones(p, gapped) {
			t.Fatalf("planned route enters the zone at %s", p)
		}
	}
	if w.Plan(dest, WalkOptions{}).OK != true {
		t.Fatal("an open field must plan without zones")
	}
}

// A zone left over from the last WalkTo must not leak into a preview.
func TestPlanIgnoresTheLastWalksZones(t *testing.T) {
	_, w, _ := field(t, Point{X: 0, Z: 10})
	w.walk.zones = []Rect{{MinX: 20, MaxX: 22, MinZ: 0, MaxZ: 20, Level: 0}}
	w.walk.flee = []Rect{{MinX: 30, MaxX: 32, MinZ: 0, MaxZ: 20, Level: 0}}
	if r := w.Plan(Point{X: 40, Z: 10}, WalkOptions{}); !r.OK {
		t.Fatalf("a finished walk's zones blocked the preview: %s", r.Reason)
	}
	if len(w.walk.zones) != 1 || len(w.walk.flee) != 1 {
		t.Fatal("Plan changed the walk state")
	}
}
