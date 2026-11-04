package locreach

import "rsb0t-go/internal/scripts/tile"

// The wall-decoration strategy: ReachStrategy.reachWallDecor1 for a size-1 account
// (engine/src/engine/routefinder/ReachStrategy.ts, branch 289), the strategy the engine picks for loc
// shapes 4 to 8 (the pipes, plaques, torches and brackets bolted to a wall).
//
// Why it matters: a straight decoration (shape 4, WALLDECOR_STRAIGHT_NOOFFSET, and shape 5, _OFFSET) is
// reached from ITS OWN TILE and from no other. reachWallDecor1 returns false for every neighbour of a
// straight decoration, and a loc with only an [oploc1,...] or [oplocu,...] trigger runs nothing then. The
// quest engine stood on the "interact tile" the collision pack names for any walkable loc (the tile itself
// or one of its four neighbours, nearest first), so an account that arrived on the east side of the Rellekka
// longhall's drain pipe (loc 4162, shape 5, 2663,3674) sent "use the lit firecracker on the pipe" from a
// neighbour, the server ran nothing, and Manni's trial parked on "the pipe took no firecracker" (90 log
// lines on four accounts, 14 Oct 2026).

// Wall nibble bits, as pathfind.Pack.WallMask: the face of a tile that carries a wall.
const (
	WallNorth = 0x1
	WallEast  = 0x2
	WallSouth = 0x4
	WallWest  = 0x8
)

// DecorShape reports whether the engine picks its wall-decoration strategy for this shape.
func DecorShape(shape int) bool { return shape >= 4 && shape <= 8 }

// DecorStand is a tile an account may interact with a wall decoration from.
type DecorStand struct {
	Tile tile.Tile
	// Open is the wall-nibble bit of Tile that must be CLEAR for the engine to accept the stand (the
	// face toward the decoration), or 0 when the tile is accepted whatever its walls.
	Open int
}

// WallDecorStands lists the tiles a size-1 account may stand on to interact with the wall decoration l:
// its own tile first (always accepted), then, for the diagonal shapes (6 to 8), the neighbours the engine
// accepts when the wall between is open. A straight decoration (4, 5) has its own tile only. It returns
// nil for a shape that is not a wall decoration.
func WallDecorStands(l Loc) []DecorStand {
	if !DecorShape(l.Shape) {
		return nil
	}
	at := func(dx, dz int) tile.Tile { return tile.Tile{X: l.X + dx, Z: l.Z + dz, Level: l.Level} }
	out := []DecorStand{{Tile: at(0, 0)}}
	switch l.Shape {
	case 6, 7:
		// alteredRotation: shape 7 is read two quarter turns on.
		rot := l.Angle & 3
		if l.Shape == 7 {
			rot = (rot + 2) & 3
		}
		switch rot {
		case 0: // west
			out = append(out, DecorStand{at(1, 0), WallWest}, DecorStand{at(0, -1), WallNorth})
		case 1: // north
			out = append(out, DecorStand{at(-1, 0), WallEast}, DecorStand{at(0, -1), WallNorth})
		case 2: // east
			out = append(out, DecorStand{at(-1, 0), WallEast}, DecorStand{at(0, 1), WallSouth})
		default: // south
			out = append(out, DecorStand{at(1, 0), WallWest}, DecorStand{at(0, 1), WallSouth})
		}
	case 8:
		out = append(out, DecorStand{at(0, 1), WallSouth}, DecorStand{at(0, -1), WallNorth},
			DecorStand{at(-1, 0), WallEast}, DecorStand{at(1, 0), WallWest})
	}
	return out
}
