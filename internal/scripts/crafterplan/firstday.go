package crafterplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// The wool half of ProgressiveCrafter.ts: shearing at the Lumbridge pen and spinning upstairs in
// the castle until crafting 10. The phase itself is freshstart.DecidePhase over crafterplan.Road;
// these are the choices the TS makes inline inside its wool, store and drop steps.

// Timings and ranges from ProgressiveCrafter.ts.
const (
	SheepRadius  = 10
	WheelRadius  = 8
	ArriveRadius = 6
	// ShearWait is how long a shear waits for the wool (SHEAR_MS).
	ShearWait = 6 * time.Second
	// SheepSkip parks a sheep that gave no wool (SHEEP_SKIP_MS).
	SheepSkip = 60 * time.Second
	// FlaxSkip parks a stalk that stayed standing (FLAX_SKIP_MS).
	FlaxSkip = 20 * time.Second
	// BatchIdleTicks ends a spinning batch after this many ticks without progress.
	BatchIdleTicks = 8
	// DropGuard caps one drop pass.
	DropGuard = 30
)

// Sheep NPC types, checked against content 289 pack/npc.pack: 42 is sheepsheered ("Freshly
// sheared.") and 43 sheepunsheered. Both are named Sheep, and shearing changes one into the other
// for 100 ticks (shear_sheep.rs2: npc_changetype_keepall(sheepsheered, 100)). The TS api could not
// tell them apart, so it learnt shorn sheep through its skip list; the type id says it directly.
const (
	SheepName   = "Sheep"
	SheepShorn  = 42
	SheepWoolly = 43
)

// Sheep is one visible sheep.
type Sheep struct {
	Index int
	Type  int
	Tile  tile.Tile
}

// ChooseSheep picks the nearest sheep within SheepRadius of me that is not skipped and not known to
// be shorn. It returns -1 for none.
func ChooseSheep(sheep []Sheep, me tile.Tile, skipped func(index int) bool) int {
	best, bestDist := -1, 0
	for i, s := range sheep {
		if s.Type == SheepShorn || s.Tile.Level != me.Level {
			continue
		}
		d := tile.Chebyshev(s.Tile, me)
		if d > SheepRadius || (skipped != nil && skipped(s.Index)) {
			continue
		}
		if best < 0 || d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// WantsShears reports whether the store trip buys shears: missing, and still below crafting 10.
func WantsShears(hasShears bool, crafting int) bool {
	return !hasShears && crafting < BowStringLevel
}

// WoolAction is the wool step's choice.
type WoolAction string

// The wool step's choices.
const (
	// WoolSpin: a full pack with wool in it goes to the wheel.
	WoolSpin WoolAction = "spin the wool"
	// WoolMakeRoom: full of something neither wool nor balls that the store refused; drop one.
	WoolMakeRoom WoolAction = "make room"
	WoolShear    WoolAction = "shear"
)

// DecideWool is woolStep's choice.
func DecideWool(wool int, packFull bool) WoolAction {
	switch {
	case wool > 0 && packFull:
		return WoolSpin
	case packFull:
		return WoolMakeRoom
	}
	return WoolShear
}

// Junk picks the first pack slot worth dropping to make room: anything the store never sees.
// It returns -1 for none.
func Junk(pack []freshstart.PackItem) int {
	keep := StoreKeep()
	for i, item := range pack {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		kept := false
		for _, k := range keep {
			if strings.EqualFold(k, name) {
				kept = true
				break
			}
		}
		if !kept {
			return i
		}
	}
	return -1
}

// LumbridgeLayout is the castle and pen places the wool stage uses, checked offline against the
// collision pack and loc index with cmd/doordiag and cmd/locops.
type LumbridgeLayout struct {
	// Staircase is the castle's south-west tower staircase, one loc per floor at the same x,z:
	// 1738 (level 0, Climb-up), 1739 (level 1, Climb/Climb-up/Climb-down), 1740 (level 2, Climb-down).
	Staircase tile.Tile
	// Wheel is the spinning wheel loc (2644) on level 1; the TS wheel tile (3209,3213,1) is the
	// stand beside it.
	Wheel tile.Tile
	// PenGate is the pen's east double gate (1551/1553 at 3213,3261 and 3213,3262). Its wall is on
	// the west edge of its own tile, so PenOutside is the gate tile itself — found live: an Open
	// clicked from one tile further east never took — and PenInside is the tile across the wall.
	PenGate    tile.Tile
	PenOutside tile.Tile
	PenInside  tile.Tile
	// Pen is the pen's walkable bounding box.
	PenMin, PenMax tile.Tile
}

// Lumbridge returns the wool stage's layout.
func Lumbridge() LumbridgeLayout {
	return LumbridgeLayout{
		Staircase:  tile.Tile{X: 3204, Z: 3207},
		Wheel:      tile.Tile{X: 3209, Z: 3212, Level: 1},
		PenGate:    tile.Tile{X: 3213, Z: 3261},
		PenOutside: tile.Tile{X: 3213, Z: 3261},
		PenInside:  tile.Tile{X: 3212, Z: 3261},
		PenMin:     tile.Tile{X: 3193, Z: 3257},
		PenMax:     tile.Tile{X: 3212, Z: 3276},
	}
}

// InPen reports whether t is inside the sheep pen.
func (l LumbridgeLayout) InPen(t tile.Tile) bool {
	return t.Level == 0 && t.X >= l.PenMin.X && t.X <= l.PenMax.X && t.Z >= l.PenMin.Z && t.Z <= l.PenMax.Z
}

// RouteUpstairs is how to reach a level-1 wheel from me: climb down from level 2, walk beside the
// staircase and climb up from level 0, and on level 1 the wheel is here.
func RouteUpstairs(me tile.Tile, stairs tile.Tile, wheelLevel int) Move {
	switch {
	case me.Level > wheelLevel:
		return MoveClimbDown
	case me.Level < wheelLevel:
		if tile.Chebyshev(me, stairs) > 2 {
			return MoveWalk
		}
		return MoveClimbUp
	}
	return MoveHere
}

// RouteDownstairs is how to reach the ground floor from me: walk beside the staircase and climb
// down while above it.
func RouteDownstairs(me tile.Tile, stairs tile.Tile) Move {
	if me.Level == 0 {
		return MoveHere
	}
	if tile.Chebyshev(me, stairs) > 2 {
		return MoveWalk
	}
	return MoveClimbDown
}
