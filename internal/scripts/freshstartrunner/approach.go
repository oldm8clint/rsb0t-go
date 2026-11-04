package freshstartrunner

import (
	"sort"

	"rsb0t-go/internal/scripts/tile"
)

// footprintApproaches returns every tile orthogonally outside a loc's footprint, nearest to
// from first. width and length are the type's own, swapped for angle 1 or 3 the way
// locindex's footprint distance does.
//
// Why: tryChangeLevel used the four tiles beside the loc's origin tile. For a 2x2 staircase
// (stairstop at Morgan's house in Draynor, spiralstairsmiddle in the Wizards' Tower, both
// macro-event fail-teleport destinations) two of those four are inside the staircase itself,
// so the matrix run stranded a genie-ignoring account upstairs.
func footprintApproaches(x, z, level, width, length, angle int, from tile.Tile) []tile.Tile {
	w, h := width, length
	if angle == 1 || angle == 3 {
		w, h = h, w
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	out := make([]tile.Tile, 0, 2*(w+h))
	for i := 0; i < w; i++ {
		out = append(out, tile.Tile{X: x + i, Z: z + h, Level: level}, tile.Tile{X: x + i, Z: z - 1, Level: level})
	}
	for j := 0; j < h; j++ {
		out = append(out, tile.Tile{X: x + w, Z: z + j, Level: level}, tile.Tile{X: x - 1, Z: z + j, Level: level})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		da, db := tile.Chebyshev(a, from), tile.Chebyshev(b, from)
		if da != db {
			return da < db
		}
		sa, sb := sqDist(a, from), sqDist(b, from)
		if sa != sb {
			return sa < sb
		}
		if a.X != b.X {
			return a.X < b.X
		}
		return a.Z < b.Z
	})
	return out
}

func sqDist(a, b tile.Tile) int {
	dx, dz := a.X-b.X, a.Z-b.Z
	return dx*dx + dz*dz
}
