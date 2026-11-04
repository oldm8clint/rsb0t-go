// Package packtest builds small collision packs for tests: ground described by a predicate instead of a file. A tile is either walkable
// or solid; a step needs the destination walkable and, for a diagonal, both orthogonal neighbours too (the baked pack clears a diagonal
// bit whenever a cardinal step round the corner is blocked, so a step cannot squeeze between two solid tiles). No walls: a pack built
// here has no wall faces, so reach questions are about solid tiles only.
package packtest

import (
	"rsb0t-go/internal/pathfind"
)

// Ground builds a level-0 pack covering the mapsquares that hold the rectangle (x0,z0)-(x1,z1); a tile is walkable when solid(x,z) is
// false. Tiles in those mapsquares outside the rectangle are solid too, so a walk cannot leave the rectangle by a path the test did not
// draw.
func Ground(x0, z0, x1, z1 int, solid func(x, z int) bool) (*pathfind.Pack, error) {
	walkable := func(x, z int) bool {
		return x >= x0 && x <= x1 && z >= z0 && z <= z1 && !solid(x, z)
	}
	dxs := [8]int{0, 1, 0, -1, 1, 1, -1, -1}
	dzs := [8]int{1, 0, -1, 0, 1, -1, -1, 1}
	var squares [][2]int
	for mx := x0 >> 6; mx <= x1>>6; mx++ {
		for mz := z0 >> 6; mz <= z1>>6; mz++ {
			squares = append(squares, [2]int{mx, mz})
		}
	}
	raw := []byte{'L', 'C', 'N', 'V', 1, 1, 0, 0, byte(len(squares)), byte(len(squares) >> 8)}
	for _, sq := range squares {
		exit := make([]byte, 4096)
		walk := make([]byte, 512)
		for lx := 0; lx < 64; lx++ {
			for lz := 0; lz < 64; lz++ {
				x, z := sq[0]<<6+lx, sq[1]<<6+lz
				if !walkable(x, z) {
					continue
				}
				idx := lx*64 + lz
				walk[idx>>3] |= 1 << uint(idx&7)
				for d := 0; d < 8; d++ {
					nx, nz := x+dxs[d], z+dzs[d]
					if !walkable(nx, nz) {
						continue
					}
					if d >= 4 && (!walkable(x+dxs[d], z) || !walkable(x, z+dzs[d])) {
						continue
					}
					exit[idx] |= 1 << uint(d)
				}
			}
		}
		raw = append(raw, byte(sq[0]), byte(sq[1]), 0b0001)
		raw = append(raw, exit...)
		raw = append(raw, walk...)
	}
	return pathfind.Parse(raw)
}

// NatureGate is the Nature ruins' doorstep in miniature, in its real coordinates: open ground from 2855,3010 to 2880,3030 with the two
// solid tiles that matter to the gate, the Ruined Pillar at 2865,3020 (between the stand at 2867,3020 and the tile west of it) and the
// Mysterious stone at 2866,3022 (beside the portal's exit tile 2865,3022). The real pack is richer (rubble, trees, the ruins' own
// footprint); the live-pack tests in runecraftrunner check the same claims against it.
func NatureGate() (*pathfind.Pack, error) {
	solid := map[[2]int]bool{{2865, 3020}: true, {2866, 3022}: true}
	return Ground(2855, 3010, 2880, 3030, func(x, z int) bool { return solid[[2]int{x, z}] })
}
