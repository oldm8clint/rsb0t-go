package doorescape

// Placement is a loc as the map (or a script) places it: its type, its origin tile, its angle and its shape.
type Placement struct {
	ID, X, Z, Level, Angle, Shape int
}

// LocType is the part of a loc's config the reach test reads.
type LocType struct {
	Width, Length int
	// ForceApproach is the sides an account may NOT operate the loc from, before rotation (flags.ts
	// BlockAccessFlag: 1 north, 2 east, 4 south, 8 west).
	ForceApproach int
}

// Loc shapes the reach test distinguishes (flags.ts LocShape).
const (
	ShapeWallStraight = 0
	ShapeWallDiagonal = 9
	ShapeCentrepiece  = 10
	ShapeCentreDiag   = 11
	ShapeGroundDecor  = 22
)

// BlockAccess bits (flags.ts BlockAccessFlag).
const (
	blockAccessNorth = 0x1
	blockAccessEast  = 0x2
	blockAccessSouth = 0x4
	blockAccessWest  = 0x8
)

// Reached is ReachStrategy.reached for a size-1 account standing at (srcX, srcZ): whether the engine will run
// the loc's op trigger from there (Player.inOperableDistance), which is the whole of what an OPLOC needs
// beyond the loc being there. The engine's clientRoutefinder never walks an account to a loc, so an op sent
// from a tile this refuses answers "I can't reach that!" and does nothing.
//
// Walls (shapes 0 and 9) and rectangles (shapes 10, 11 and 22, the staircases) are ported; the other shapes
// (corner walls, wall decorations, roofs) answer false, which is safe: nothing here operates one.
func Reached(f FlagsFunc, level, srcX, srcZ int, p Placement, t LocType) bool {
	switch {
	case p.Shape == ShapeWallStraight || p.Shape == ShapeWallDiagonal:
		if srcX == p.X && srcZ == p.Z {
			return true
		}
		return reachWall1(f, level, srcX, srcZ, p.X, p.Z, p.Shape, p.Angle)
	case p.Shape == ShapeCentrepiece || p.Shape == ShapeCentreDiag || p.Shape == ShapeGroundDecor:
		if srcX == p.X && srcZ == p.Z {
			return true
		}
		return reachRectangle(f, level, srcX, srcZ, p.X, p.Z, t.Width, t.Length, p.Angle, t.ForceApproach)
	}
	return false
}

// reachWall1 is ReachStrategy.reachWall1 for the straight and diagonal wall shapes. The tests on the source
// tile's own flags are the BLOCK_* masks, so a source tile that is itself solid never reaches.
func reachWall1(f FlagsFunc, level, srcX, srcZ, destX, destZ, shape, angle int) bool {
	src := f(srcX, srcZ, level)
	switch shape {
	case ShapeWallStraight:
		// Angles are flags.ts LocAngle: 0 west, 1 north, 2 east, 3 south.
		switch angle & 3 {
		case 0:
			return (srcX == destX-1 && srcZ == destZ) ||
				(srcX == destX && srcZ == destZ+1 && src&blockNorth == 0) ||
				(srcX == destX && srcZ == destZ-1 && src&blockSouth == 0)
		case 1:
			return (srcX == destX && srcZ == destZ+1) ||
				(srcX == destX-1 && srcZ == destZ && src&blockWest == 0) ||
				(srcX == destX+1 && srcZ == destZ && src&blockEast == 0)
		case 2:
			return (srcX == destX+1 && srcZ == destZ) ||
				(srcX == destX && srcZ == destZ+1 && src&blockNorth == 0) ||
				(srcX == destX && srcZ == destZ-1 && src&blockSouth == 0)
		default:
			return (srcX == destX && srcZ == destZ-1) ||
				(srcX == destX-1 && srcZ == destZ && src&blockWest == 0) ||
				(srcX == destX+1 && srcZ == destZ && src&blockEast == 0)
		}
	case ShapeWallDiagonal:
		return (srcX == destX && srcZ == destZ+1 && src&WallSouth == 0) ||
			(srcX == destX && srcZ == destZ-1 && src&WallNorth == 0) ||
			(srcX == destX-1 && srcZ == destZ && src&WallEast == 0) ||
			(srcX == destX+1 && srcZ == destZ && src&WallWest == 0)
	}
	return false
}

// rotate is Rotation.rotate: an odd angle swaps the pair.
func rotate(angle, a, b int) int {
	if angle&1 != 0 {
		return b
	}
	return a
}

// rotateFlags is Rotation.rotateFlags: the force-approach nibble turned with the loc.
func rotateFlags(angle, flags int) int {
	if angle == 0 {
		return flags
	}
	return ((flags << uint(angle)) & 0xf) | (flags >> uint(4-angle))
}

// reachRectangle is ReachStrategy.reachRectangle for a size-1 source: standing on the loc's footprint, or on
// a tile beside it whose own wall flag toward it is open and whose side the loc's force-approach allows.
func reachRectangle(f FlagsFunc, level, srcX, srcZ, destX, destZ, width, length, angle, blockAccess int) bool {
	w := rotate(angle, width, length)
	h := rotate(angle, length, width)
	access := rotateFlags(angle, blockAccess)
	// collides(): any overlap of the source's tile with the loc's rectangle.
	if srcX >= destX && srcX < destX+w && srcZ >= destZ && srcZ < destZ+h {
		return true
	}
	src := f(srcX, srcZ, level)
	east, north := destX+w-1, destZ+h-1
	if srcX == destX-1 && srcZ >= destZ && srcZ <= north && src&WallEast == 0 && access&blockAccessWest == 0 {
		return true
	}
	if srcX == east+1 && srcZ >= destZ && srcZ <= north && src&WallWest == 0 && access&blockAccessEast == 0 {
		return true
	}
	if srcZ+1 == destZ && srcX >= destX && srcX <= east && src&WallNorth == 0 && access&blockAccessSouth == 0 {
		return true
	}
	return srcZ == north+1 && srcX >= destX && srcX <= east && src&WallSouth == 0 && access&blockAccessNorth == 0
}
