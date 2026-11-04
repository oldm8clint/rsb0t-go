package fleetplan

import "rsb0t-go/internal/scripts/tile"

// THE GROUND A PEN'S HUNTERS HOLD (ogresafe-1017, 17 Oct 2026).
//
// PenThreat.Holds says whether one NPC keeps a melee target on one tile. A walk out of a pen is judged by how many of its tiles
// are held by anything, and a planner asking that of every neighbour of every node cannot afford a scan of sixty threats a time:
// HeldGrid answers it from a bitmap over the threats' own ground.

// HeldGrid is every tile some threat holds, as a bitmap per level over the box the threats' chase ranges cover.
type HeldGrid struct {
	levels map[int]*heldLevel
}

type heldLevel struct {
	minX, minZ, w, h int
	held             []bool
}

// NewHeldGrid marks every tile PenThreat.Holds is true of, for every threat (the pen's herd and everything else that hunts near
// it alike: a tile either can melee is a tile the account must not stand on).
func NewHeldGrid(threats []PenThreat) *HeldGrid {
	g := &HeldGrid{levels: map[int]*heldLevel{}}
	type box struct{ x0, z0, x1, z1 int }
	boxes := map[int]*box{}
	for _, th := range threats {
		b := boxes[th.At.Level]
		x0, z0, x1, z1 := th.At.X-th.Cut, th.At.Z-th.Cut, th.At.X+th.Cut, th.At.Z+th.Cut
		if b == nil {
			boxes[th.At.Level] = &box{x0, z0, x1, z1}
			continue
		}
		b.x0, b.z0, b.x1, b.z1 = min(b.x0, x0), min(b.z0, z0), max(b.x1, x1), max(b.z1, z1)
	}
	for level, b := range boxes {
		g.levels[level] = &heldLevel{minX: b.x0, minZ: b.z0, w: b.x1 - b.x0 + 1, h: b.z1 - b.z0 + 1}
		g.levels[level].held = make([]bool, g.levels[level].w*g.levels[level].h)
	}
	for _, th := range threats {
		l := g.levels[th.At.Level]
		for x := th.At.X - th.Cut; x <= th.At.X+th.Cut; x++ {
			for z := th.At.Z - th.Cut; z <= th.At.Z+th.Cut; z++ {
				if th.Holds(tile.Tile{X: x, Z: z, Level: th.At.Level}) {
					l.held[(x-l.minX)*l.h+(z-l.minZ)] = true
				}
			}
		}
	}
	return g
}

// Holds reports whether any threat holds the tile.
func (g *HeldGrid) Holds(x, z, level int) bool {
	if g == nil {
		return false
	}
	l := g.levels[level]
	if l == nil {
		return false
	}
	dx, dz := x-l.minX, z-l.minZ
	if dx < 0 || dz < 0 || dx >= l.w || dz >= l.h {
		return false
	}
	return l.held[dx*l.h+dz]
}

// Count is how many of the tiles are held.
func (g *HeldGrid) Count(path []tile.Tile) int {
	n := 0
	for _, t := range path {
		if g.Holds(t.X, t.Z, t.Level) {
			n++
		}
	}
	return n
}
