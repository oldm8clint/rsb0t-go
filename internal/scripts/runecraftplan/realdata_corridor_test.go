package runecraftplan

import (
	"os"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// routeTiles expands a corner-compressed route into every tile it walks (its segments are straight or diagonal).
func routeTiles(t *testing.T, r webwalk.Route) []tile.Tile {
	t.Helper()
	var out []tile.Tile
	for i, w := range r.Waypoints {
		if i == 0 {
			out = append(out, tile.Tile{X: w.X, Z: w.Z, Level: w.Level})
			continue
		}
		prev := r.Waypoints[i-1]
		dx, dz := w.X-prev.X, w.Z-prev.Z
		adx, adz := dx, dz
		if adx < 0 {
			adx = -adx
		}
		if adz < 0 {
			adz = -adz
		}
		if adx != 0 && adz != 0 && adx != adz {
			t.Fatalf("segment %v -> %v is neither straight nor diagonal", prev.Point, w.Point)
		}
		sx, sz := sign(dx), sign(dz)
		x, z := prev.X, prev.Z
		for x != w.X || z != w.Z {
			x += sx
			z += sz
			out = append(out, tile.Tile{X: x, Z: z, Level: w.Level})
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

// The corridor on the fleet's own collision pack: from Brimhaven's landing to Jiminua the shortest road is 114 steps and with a
// box around every spider, tribesman and scorpion spawn 115; from the store to the ruins' exit tile 118 and 123 (the design
// measured 119 and 122 from another tile of the shop). The road with the boxes never steps inside one, and the shortest road
// DOES (it passes 1 tile from the spider at (2796,3104)), which is why the carrier plans with them.
//
// Needs RSB0T_COLLISION_PACK; without it this skips.
func TestCorridorOnTheRealPack(t *testing.T) {
	packPath := os.Getenv("RSB0T_COLLISION_PACK")
	if packPath == "" {
		t.Skip("set RSB0T_COLLISION_PACK")
	}
	pk, err := pathfind.Load(packPath)
	if err != nil {
		t.Fatal(err)
	}
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	pl := webwalk.NewPlanner(pk, g)
	var rects []webwalk.Rect
	for _, b := range AvoidBoxes() {
		rects = append(rects, webwalk.Rect{MinX: b.MinX, MaxX: b.MaxX, MinZ: b.MinZ, MaxZ: b.MaxZ, Level: 0})
	}
	pt := func(a tile.Tile) webwalk.Point { return webwalk.Point{X: a.X, Z: a.Z, Level: a.Level} }
	for _, a := range []tile.Tile{Landing, StoreStand, JiminuaAt, Nature.Exit} {
		if !pk.Walkable(a.X, a.Z, a.Level) {
			t.Errorf("%v is not walkable", a)
		}
	}
	for _, tc := range []struct {
		name         string
		from, to     tile.Tile
		plain, avoid int
		plainEnters  bool
	}{
		{"landing to the store", Landing, StoreStand, 114, 115, false},
		{"the store to the gate", StoreStand, Nature.Exit, 118, 123, true},
	} {
		plain := pl.Plan(pt(tc.from), pt(tc.to), webwalk.Options{WalkOnly: true})
		avoid := pl.Plan(pt(tc.from), pt(tc.to), webwalk.Options{WalkOnly: true, AvoidZones: rects})
		if !plain.OK || !avoid.OK {
			t.Fatalf("%s: plain %v (%s), with the boxes %v (%s)", tc.name, plain.OK, plain.Reason, avoid.OK, avoid.Reason)
		}
		if plain.Cost != tc.plain || avoid.Cost != tc.avoid {
			t.Errorf("%s: %d steps shortest and %d around the boxes, want %d and %d", tc.name, plain.Cost, avoid.Cost, tc.plain, tc.avoid)
		}
		boxes := AvoidBoxes()
		inside := func(tiles []tile.Tile) *tile.Tile {
			for i := range tiles {
				for _, b := range boxes {
					if b.Contains(tiles[i]) {
						return &tiles[i]
					}
				}
			}
			return nil
		}
		if at := inside(routeTiles(t, avoid)); at != nil {
			t.Errorf("%s: the route around the boxes steps inside one at %v", tc.name, *at)
		}
		if tc.plainEnters {
			if at := inside(routeTiles(t, plain)); at == nil {
				t.Errorf("%s: the shortest road never enters a box, so the boxes prove nothing", tc.name)
			}
		}
		if extra := avoid.Cost - plain.Cost; extra < 0 || extra > 8 {
			t.Errorf("%s: avoiding costs %d extra steps, want 0 to 8", tc.name, extra)
		}
	}
	// Avoiding the snakes as well would cost 201 steps on the Jiminua leg, which is why they are left on the road.
	if got := len(AvoidBoxes()); got < 80 {
		t.Fatalf("%d boxes", got)
	}
}
