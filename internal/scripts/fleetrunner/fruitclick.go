package fleetrunner

import (
	"errors"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/fleetplan"
)

// ErrNoStrangeFruit is SendEatStrangeFruit's answer when the pack holds none.
var ErrNoStrangeFruit = errors.New("scarabrunner: no Strange fruit in the pack")

// SendEatStrangeFruit sends the Eat click (op 1) for one Strange fruit in the pack and returns at once, without waiting for it to leave the
// pack (EatStrangeFruit waits up to three seconds). For a caller that must not stand still for that: the next look at the pack says whether
// the click landed.
func SendEatStrangeFruit(d *Deps) error {
	for _, it := range PackSlotItems(d) {
		if int(it.ID) == fleetplan.StrangeFruitID {
			return actions.Eat(d.Session, it.ID, it.Slot, PlayerPack)
		}
	}
	return ErrNoStrangeFruit
}
