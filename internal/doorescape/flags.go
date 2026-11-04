// Package doorescape plans the way out of a walled building for an account whose only way out is a
// door, a staircase, or both, and knows exactly how the server's doors behave while it does.
//
// Why it exists. On 6 October 2026 a server-side summon put 356 accounts on the roof of Rogues' Castle
// and, six hours later, seventeen of them were still inside. Every one of the fleet's walkers had a model
// of a door as "stand on the tile beside it, open it, step across", which is true of a plain door in a
// wall and false of what the castle has:
//
//   - a door does not open in place. The engine's door procs (content/scripts/doors/scripts/door_procs.rs2)
//     DELETE the closed leaf and ADD an open leaf on a neighbouring tile, turned a quarter, so opening a
//     door moves a wall: the passage appears where the leaf was and a new wall appears beside it. Two doors
//     whose leaves land on each other's passage seal the tile between them (Rogues' Castle's east door and
//     the door of the chamber beside it do exactly that to 3293,3931);
//   - a DIAGONAL door (loc shape 9, the castle's main door and the one between its first room and its
//     hall) is not a wall at all but a loc that blocks its whole tile, and opening one moves that block to
//     the neighbouring tile and pushes any account standing there one tile aside. The tile an account
//     opens it from is therefore very often the tile the leaf lands on: it is pushed off, the planner's
//     "stand" is now solid, and a walk that starts from the old stand tile never starts;
//   - what a step or an op needs from a tile depends on the collision flags the server holds, which a door
//     changes, not on the static map the walkers plan on.
//
// What it is. A port of the parts of Engine-TS a player's movement and a loc op depend on, small enough to
// be checked against the engine itself: StepValidator.canTravel for a size-1 player (flags.go),
// ReachStrategy.reached for walls, diagonal walls and rectangles (reach.go), CollisionEngine's wall and loc
// flag arithmetic and the doors' own scripts (door.go), and a planner over the castle's tiles and door
// states that finds the cheapest route out (plan.go). Its collision data is castle_collision.json, the
// movement bits of every tile of the castle's region as the engine holds them after GameMap.init, produced
// by castle_collision_reference_vector.ts from the same Engine-TS and checked against the collision pack
// the fleet walks on. test/fixtures/castle_collision_golden.json holds answers captured from the engine's
// own code, and TestAgainstTheEngine compares this package to them for 200 door states, tile by tile.
//
// The package is pure: it reads no session, takes no clock and sends nothing. randomevents' escape ladder
// turns a Plan into clicks.
package doorescape

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

// Collision flag bits, from Engine-TS engine/routefinder/flags.ts. Only the bits a step of a player reads are
// named: the eight wall faces, LOC, FLOOR_DECORATION and FLOOR. (The projectile blockers, the roof and the
// entity-occupancy bits never change where a player may step.)
const (
	WallNorthWest uint32 = 0x1
	WallNorth     uint32 = 0x2
	WallNorthEast uint32 = 0x4
	WallEast      uint32 = 0x8
	WallSouthEast uint32 = 0x10
	WallSouth     uint32 = 0x20
	WallSouthWest uint32 = 0x40
	WallWest      uint32 = 0x80
	Loc           uint32 = 0x100
	FloorDecor    uint32 = 0x40000
	Floor         uint32 = 0x200000

	// MoveMask is every bit above.
	MoveMask = 0xff | Loc | FloorDecor | Floor

	// blockWalk is flags.ts WALK_BLOCKED: a tile with any of these cannot be entered at all.
	blockWalk = Floor | FloorDecor | Loc

	// The BLOCK_* masks of flags.ts: moving in a direction INTO a tile is refused when the tile holds any bit
	// of the mask for that direction, that is when it is blocked outright or has a wall on the face the step
	// comes through (or, for a diagonal, a wall on either face of the corner).
	blockNorth     = blockWalk | WallSouth
	blockSouth     = blockWalk | WallNorth
	blockEast      = blockWalk | WallWest
	blockWest      = blockWalk | WallEast
	blockNorthEast = blockWalk | WallSouth | WallSouthWest | WallWest
	blockSouthEast = blockWalk | WallNorthWest | WallNorth | WallWest
	blockSouthWest = blockWalk | WallNorth | WallNorthEast | WallEast
	blockNorthWest = blockWalk | WallEast | WallSouthEast | WallSouth

	// blockAll is what the engine's collision map answers for a tile in a zone it never allocated.
	blockAll uint32 = 0x7fffffff
)

// Dir indexes the eight steps, in the order of the collision pack's exit mask (internal/pathfind's DX/DZ):
// bit n of an exit mask is a legal step in direction n.
const (
	North = iota
	East
	South
	West
	NorthEast
	SouthEast
	SouthWest
	NorthWest
)

// DX and DZ are the offsets of the eight steps.
var (
	DX = [8]int{0, 1, 0, -1, 1, 1, -1, -1}
	DZ = [8]int{1, 0, -1, 0, 1, -1, -1, 1}
)

// Tile is a world tile.
type Tile struct{ X, Z, Level int }

func (t Tile) String() string { return fmt.Sprintf("%d,%d,%d", t.X, t.Z, t.Level) }

// Grid is a rectangle of collision flags on every level, as the server holds them with every door in its map
// state (closed). It is never written: a door state lays its changes over it (Catalog.flagsAt).
type Grid struct {
	X0, Z0, W, H, Levels int
	flags                []uint32
}

// NewGrid builds a grid from flags[level][z-z0][x-x0].
func NewGrid(x0, z0, w, h int, flags [][][]uint32) *Grid {
	g := &Grid{X0: x0, Z0: z0, W: w, H: h, Levels: len(flags), flags: make([]uint32, len(flags)*w*h)}
	for l := range flags {
		for z := 0; z < h; z++ {
			copy(g.flags[(l*h+z)*w:(l*h+z)*w+w], flags[l][z])
		}
	}
	return g
}

// Index is the slot of a tile in the grid, or -1 when the tile is outside it.
func (g *Grid) Index(x, z, level int) int {
	x -= g.X0
	z -= g.Z0
	if x < 0 || z < 0 || x >= g.W || z >= g.H || level < 0 || level >= g.Levels {
		return -1
	}
	return (level*g.H+z)*g.W + x
}

// TileAt is the tile of a slot.
func (g *Grid) TileAt(i int) Tile {
	x := i % g.W
	z := (i / g.W) % g.H
	return Tile{X: g.X0 + x, Z: g.Z0 + z, Level: i / (g.W * g.H)}
}

// Len is the number of slots.
func (g *Grid) Len() int { return len(g.flags) }

// Base is a tile's flags with every door as the map has it; a tile outside the grid is solid, as a zone the
// engine never allocated is.
func (g *Grid) Base(x, z, level int) uint32 {
	i := g.Index(x, z, level)
	if i < 0 {
		return blockAll
	}
	return g.flags[i]
}

// FlagsFunc reads one tile's flags.
type FlagsFunc func(x, z, level int) uint32

// CanStep is StepValidator.canTravel for a size-1 player on collision type NORMAL: whether a step of (dx, dz)
// from (x, z) is legal, which reads only the tiles the step enters (the engine never looks at the tile the
// player stands on, which is why an account on a solid tile can still walk off it).
func CanStep(f FlagsFunc, level, x, z, dx, dz int) bool {
	switch {
	case dx == 0 && dz == 1:
		return f(x, z+1, level)&blockNorth == 0
	case dx == 0 && dz == -1:
		return f(x, z-1, level)&blockSouth == 0
	case dx == 1 && dz == 0:
		return f(x+1, z, level)&blockEast == 0
	case dx == -1 && dz == 0:
		return f(x-1, z, level)&blockWest == 0
	case dx == 1 && dz == 1:
		return f(x+1, z+1, level)&blockNorthEast == 0 && f(x+1, z, level)&blockEast == 0 && f(x, z+1, level)&blockNorth == 0
	case dx == 1 && dz == -1:
		return f(x+1, z-1, level)&blockSouthEast == 0 && f(x+1, z, level)&blockEast == 0 && f(x, z-1, level)&blockSouth == 0
	case dx == -1 && dz == -1:
		return f(x-1, z-1, level)&blockSouthWest == 0 && f(x-1, z, level)&blockWest == 0 && f(x, z-1, level)&blockSouth == 0
	case dx == -1 && dz == 1:
		return f(x-1, z+1, level)&blockNorthWest == 0 && f(x-1, z, level)&blockWest == 0 && f(x, z+1, level)&blockNorth == 0
	}
	return false
}

// ExitMask is the eight-direction step mask of a tile, in the collision pack's bit order.
func ExitMask(f FlagsFunc, level, x, z int) uint8 {
	var m uint8
	for d := 0; d < 8; d++ {
		if CanStep(f, level, x, z, DX[d], DZ[d]) {
			m |= 1 << uint(d)
		}
	}
	return m
}

// Enterable reports whether a tile can be stood on: nothing in it blocks walking. (The pack's own Walkable.)
func Enterable(f FlagsFunc, x, z, level int) bool {
	return f(x, z, level)&blockWalk == 0
}

//go:embed castle_collision.json
var castleCollisionJSON []byte

type castleFile struct {
	X0     int          `json:"x0"`
	Z0     int          `json:"z0"`
	Width  int          `json:"width"`
	Height int          `json:"height"`
	Levels int          `json:"levels"`
	Mask   uint32       `json:"moveMask"`
	Flags  [][][]uint32 `json:"flags"`
}

var (
	castleGridOnce sync.Once
	castleGrid     *Grid
	castleGridErr  error
)

// CastleGrid is the embedded collision grid of Rogues' Castle's region: x 3262-3310, z 3910-3950, all four levels.
func CastleGrid() (*Grid, error) {
	castleGridOnce.Do(func() {
		var f castleFile
		if err := json.Unmarshal(castleCollisionJSON, &f); err != nil {
			castleGridErr = fmt.Errorf("doorescape: castle_collision.json: %w", err)
			return
		}
		if f.Mask != MoveMask {
			castleGridErr = fmt.Errorf("doorescape: castle_collision.json holds mask %#x, this build reads %#x", f.Mask, uint32(MoveMask))
			return
		}
		if f.Levels != len(f.Flags) || f.Height <= 0 || f.Width <= 0 {
			castleGridErr = fmt.Errorf("doorescape: castle_collision.json is malformed (%d levels, %dx%d)", len(f.Flags), f.Width, f.Height)
			return
		}
		for l := range f.Flags {
			if len(f.Flags[l]) != f.Height {
				castleGridErr = fmt.Errorf("doorescape: level %d has %d rows, want %d", l, len(f.Flags[l]), f.Height)
				return
			}
			for z := range f.Flags[l] {
				if len(f.Flags[l][z]) != f.Width {
					castleGridErr = fmt.Errorf("doorescape: level %d row %d has %d columns, want %d", l, z, len(f.Flags[l][z]), f.Width)
					return
				}
			}
		}
		castleGrid = NewGrid(f.X0, f.Z0, f.Width, f.Height, f.Flags)
	})
	return castleGrid, castleGridErr
}
