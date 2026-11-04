// Package locreach answers one question a caller of OPLOC/OPLOCU has to get right and
// this codebase previously guessed at: which tiles may an account stand on for the engine
// to actually run a loc's trigger?
//
// Why it exists. Engine-TS accepts an OPLOCU packet whose coordinates, loc id, component
// and slot all check out, sets the interaction, and then — in Player.tryInteract — runs the
// loc's op trigger only if reachedLoc() says the account is in "operable distance". A loc
// that has only an [oplocu,...] OP trigger and no AP trigger produces NOTHING when that test
// fails except, in some cases, an "I can't reach that!" game message. The local engine runs
// with clientRoutefinder on, so it will not walk the account into position either: the
// client is expected to have done that before sending. A caller that walked to "radius 1"
// and sent is therefore rolling dice.
//
// That is not hypothetical. It is the whole of the long-standing "OpLocU silently no-ops on
// this engine/content pairing" gotcha (CLAUDE.md, internal/scripts/mulerunner/cook.go's old
// HONEST STATUS): the Catherby range at 2817,3444 is a 1x2 loc placed at angle 1 with
// forceapproach 13, so the only tiles that work are its own two footprint tiles and the
// tiles due south of them. MuleReceiver's cooking walked to "within 1 tile of 2817,3444",
// which also admits the west and north tiles, and from those the engine did exactly nothing.
// Proven live 2026-09-20 against a privately instrumented engine copy: from 2817,3444 and
// 2818,3444 and 2817,3443 the shrimps cooked; from 2816,3444 and 2817,3445 the reply was
// "I can't reach that!" and no fish was touched.
//
// What it ports. ReachStrategy.reached's RECTANGLE strategy for a size-1 source
// (engine/src/engine/routefinder/ReachStrategy.ts plus RectangleBoundary.reachRectangle1
// and Rotation.rotate/rotateFlags), which is the strategy the engine picks for loc shapes
// 10, 11 and 22 — every "scenery you use an item on": ranges, spinning wheels, crates,
// furnaces, wells.
//
// What it deliberately does NOT port:
//
//   - The wall-flag half of reachRectangle1. The engine also requires the source tile's own
//     WALL_EAST/WALL_WEST/WALL_NORTH/WALL_SOUTH collision flag to be open toward the loc.
//     Modelling that needs the collision pack, which this package has no business loading,
//     so Tiles returns candidates and the caller's walk decides which are actually
//     standable. A tile this package accepts can therefore still be refused by the engine;
//     a tile it rejects never works.
//   - The wall strategy (shapes 0-3, 9). For those shapes Rectangle reports ok=false and Tiles
//     falls back to the orthogonal neighbours the callers used before, so nothing regresses where
//     this package cannot improve on it.
//
// The wall-decoration strategy (shapes 4-8) is ported in decor.go: a straight decoration (4, 5) is
// reached from its own tile and from no other.
package locreach

import "rsb0t-go/internal/scripts/tile"

// BlockAccess bits, from engine/src/engine/routefinder/flags.ts. A loc's forceapproach
// value names the sides an account may NOT interact from, before rotation.
const (
	BlockNorth = 0x1
	BlockEast  = 0x2
	BlockSouth = 0x4
	BlockWest  = 0x8
)

// Loc is everything the reach test needs about one placed loc: its origin tile, its type's
// unrotated footprint and forceapproach mask, and its placement angle and shape.
type Loc struct {
	X, Z, Level   int
	Width, Length int
	Angle         int
	Shape         int
	ForceApproach int
}

// rectangleShape reports whether the engine picks its RECTANGLE strategy for this shape.
// ReachStrategy.exitStrategy: shapes 10 and 11 and 22.
func rectangleShape(shape int) bool {
	return shape == 10 || shape == 11 || shape == 22
}

// rotate is Rotation.rotate: an odd angle swaps the pair.
func rotate(angle, a, b int) int {
	if angle&1 != 0 {
		return b
	}
	return a
}

// rotateFlags is Rotation.rotateFlags: the forceapproach nibble rotated with the loc.
func rotateFlags(angle, flags int) int {
	if angle == 0 {
		return flags
	}
	return ((flags << angle) & 0xf) | (flags >> (4 - angle))
}

// Footprint returns the rotated south-west origin and size the loc actually occupies.
func (l Loc) Footprint() (x, z, w, h int) {
	w, h = l.Width, l.Length
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return l.X, l.Z, rotate(l.Angle, w, h), rotate(l.Angle, h, w)
}

// Reached reports whether a size-1 account standing at (srcX, srcZ) satisfies the engine's
// own reach test for this loc, ignoring the source tile's wall flags (see the package doc).
// ok is false when the loc's shape uses a strategy this package does not model, in which
// case the boolean result means nothing and the caller should not rely on it.
func Reached(srcX, srcZ int, l Loc) (reached, ok bool) {
	if !rectangleShape(l.Shape) {
		return false, false
	}
	x, z, w, h := l.Footprint()
	east, north := x+w-1, z+h-1

	// collides(): standing anywhere on the loc's own footprint always reaches it.
	if srcX >= x && srcX <= east && srcZ >= z && srcZ <= north {
		return true, true
	}

	block := rotateFlags(l.Angle, l.ForceApproach)
	switch {
	case srcX == x-1 && srcZ >= z && srcZ <= north:
		return block&BlockWest == 0, true
	case srcX == east+1 && srcZ >= z && srcZ <= north:
		return block&BlockEast == 0, true
	case srcZ == z-1 && srcX >= x && srcX <= east:
		return block&BlockSouth == 0, true
	case srcZ == north+1 && srcX >= x && srcX <= east:
		return block&BlockNorth == 0, true
	}
	return false, true
}

// Tiles returns every tile a size-1 account may stand on to interact with the loc, the
// footprint tiles last.
//
// The footprint goes last deliberately: standing inside a loc is legal for the engine but
// is a tile a real player normally cannot walk onto (most such locs block walking), so it
// is the fallback rather than the first thing a caller walks to.
//
// For a shape this package does not model, the result is the loc's orthogonal neighbours —
// exactly the candidate set the callers built by hand before — so the caller is never left
// with nothing to walk to.
func Tiles(l Loc) []tile.Tile {
	x, z, w, h := l.Footprint()
	east, north := x+w-1, z+h-1

	var sides []tile.Tile
	add := func(t tile.Tile) { sides = append(sides, t) }

	if !rectangleShape(l.Shape) {
		for i := 0; i < h; i++ {
			add(tile.Tile{X: x - 1, Z: z + i, Level: l.Level})
			add(tile.Tile{X: east + 1, Z: z + i, Level: l.Level})
		}
		for i := 0; i < w; i++ {
			add(tile.Tile{X: x + i, Z: z - 1, Level: l.Level})
			add(tile.Tile{X: x + i, Z: north + 1, Level: l.Level})
		}
		return sides
	}

	block := rotateFlags(l.Angle, l.ForceApproach)
	if block&BlockWest == 0 {
		for i := 0; i < h; i++ {
			add(tile.Tile{X: x - 1, Z: z + i, Level: l.Level})
		}
	}
	if block&BlockEast == 0 {
		for i := 0; i < h; i++ {
			add(tile.Tile{X: east + 1, Z: z + i, Level: l.Level})
		}
	}
	if block&BlockSouth == 0 {
		for i := 0; i < w; i++ {
			add(tile.Tile{X: x + i, Z: z - 1, Level: l.Level})
		}
	}
	if block&BlockNorth == 0 {
		for i := 0; i < w; i++ {
			add(tile.Tile{X: x + i, Z: north + 1, Level: l.Level})
		}
	}
	for dx := 0; dx < w; dx++ {
		for dz := 0; dz < h; dz++ {
			add(tile.Tile{X: x + dx, Z: z + dz, Level: l.Level})
		}
	}
	return sides
}

// Nearest returns the reachable tile closest to from, and ok=false when Tiles is empty.
func Nearest(from tile.Tile, l Loc) (tile.Tile, bool) {
	best, found := tile.Tile{}, false
	for _, t := range Tiles(l) {
		if !found || tile.Chebyshev(from, t) < tile.Chebyshev(from, best) {
			best, found = t, true
		}
	}
	return best, found
}
