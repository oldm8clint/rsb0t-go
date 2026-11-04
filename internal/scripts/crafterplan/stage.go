// Package crafterplan is ProgressiveCrafter's own decisions: which half of the crafting ladder it
// is on, the Seers layout the flax half runs in, the flax loop's next step, its settings, and
// which bank rows go to the mule. stage.go is ported from rsb0t-scripts' craftStage.ts; crafter.go
// holds the choices ProgressiveCrafter.ts makes inline.
//
// It is pure: no client, actions, fleet or bot imports and no I/O. It builds on freshstart for the
// first day and donorplan for the hand-over.
package crafterplan

import "rsb0t-go/internal/scripts/tile"

// BowStringLevel is the crafting level bow strings need; below it, wool balls are the only spin
// worth doing. Checked against content: spinning.struct [flax] levelrequire=10.
const BowStringLevel = 10

// Stage is which half of the ladder the account is on.
type Stage string

// The two stages.
const (
	StageWool Stage = "wool"
	StageFlax Stage = "flax"
)

// StageOptions are the stage pin's choices.
func StageOptions() []string {
	return []string{"Auto", "Wool", "Flax"}
}

// StageFor picks the stage. Auto moves to flax the moment crafting 10 lands. A Wool pin holds the
// account on sheep; a Flax pin below level 10 falls back to wool, since the wheel would refuse.
func StageFor(craftingLevel int, pin string) Stage {
	if pin == "Wool" {
		return StageWool
	}
	if craftingLevel >= BowStringLevel {
		return StageFlax
	}
	return StageWool
}

// LevelsToFlax is the crafting levels still owed before the flax stage opens, 0 once it has.
func LevelsToFlax(craftingLevel int) int {
	if craftingLevel >= BowStringLevel {
		return 0
	}
	return BowStringLevel - craftingLevel
}

// Where the flax on the wheel comes from.
const (
	PickFlax = "Pick it"
	BankFlax = "Bank first, then pick"
)

// FlaxSourceOptions are the flax source setting's choices, picking first. MuleFlax (muleflax.go)
// draws the bank like BankFlax and then asks the mule for more.
func FlaxSourceOptions() []string {
	return []string{PickFlax, BankFlax, MuleFlax}
}

// DrawsFlaxFromBank reports whether this trip draws its flax out of the bank. A bank nobody has
// opened yet (flaxInBank nil) is worth one look; once a visit finds none it is a no, and every bank
// visit re-reads the count, so flax put in later starts it drawing again.
func DrawsFlaxFromBank(source string, flaxInBank *int) bool {
	if source != BankFlax && source != MuleFlax {
		return false
	}
	return flaxInBank == nil || *flaxInBank > 0
}

// KeepsBankFlax reports whether banked flax is held back from a hand-over: while the run draws its
// flax from the bank, that flax is stock rather than surplus. Flax the mule sent is never handed
// back to it.
func KeepsBankFlax(source string) bool {
	return source == BankFlax || source == MuleFlax
}

// Layout is the Seers' Village places the flax loop uses.
type Layout struct {
	// Field is the centre of the flax field.
	Field tile.Tile
	// FieldScope: flax further than this from the centre belongs to someone else's trip.
	FieldScope int
	// Ladder is the walkable tile beside the ladder, inside the wheel house.
	Ladder tile.Tile
	// Wheel is the spinning wheel, upstairs.
	Wheel tile.Tile
	// Bank is the middle of the Seers booth-stand row, where the mule posts too.
	Bank tile.Tile
}

// Seers returns the Seers layout, taken from the client's own FlaxAIO route.
func Seers() Layout {
	return Layout{
		Field:      tile.Tile{X: 2741, Z: 3444},
		FieldScope: 12,
		Ladder:     tile.Tile{X: 2714, Z: 3471},
		Wheel:      tile.Tile{X: 2711, Z: 3471, Level: 1},
		Bank:       tile.Tile{X: 2725, Z: 3493},
	}
}

// Names and ops, checked against content (branch 289): crafting.obj, spinning_wheels.loc.
const (
	Flax      = "Flax"
	PickOp    = "Pick"
	BowString = "Bow string"
	// BallOfWool is the wool spin's product, matched on the make menu by name.
	BallOfWool = "Ball of wool"
	Wheel      = "Spinning wheel"
	SpinOp     = "Spin"
	Ladder     = "Ladder"
	ClimbUp    = "Climb-up"
	ClimbDown  = "Climb-down"
)
