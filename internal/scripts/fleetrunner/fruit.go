package fleetrunner

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/fleetplan"
)

// StrangeFruitHeld is the Strange fruit in the pack (its bank note is another id, so not counted).
func StrangeFruitHeld(d *Deps) int { return PackCountOf(d, fleetplan.StrangeFruitID) }

// EatStrangeFruit eats one Strange fruit (op 1, Eat) and reports whether it left the pack.
func EatStrangeFruit(d *Deps) bool {
	before := StrangeFruitHeld(d)
	for _, it := range PackSlotItems(d) {
		if int(it.ID) != fleetplan.StrangeFruitID {
			continue
		}
		if err := actions.Eat(d.Session, it.ID, it.Slot, PlayerPack); err != nil {
			d.Logf("eating a Strange fruit: %v", err)
			return false
		}
		return WaitFor(d, "the Strange fruit to be eaten", 3*time.Second, func() bool { return StrangeFruitHeld(d) < before })
	}
	return false
}
