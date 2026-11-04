package fleetplan

import (
	"time"

	"rsb0t-go/internal/scripts/tile"
)

const (
	// HoldBankRadius is how close (Chebyshev) to a bank, on the ground floor, an account already is where it
	// should wait: a bank's own square, not the walk to the booth.
	HoldBankRadius = 10

	// HoldBankMax is the farthest bank the hold walks to. Beyond it the account stays where it is: a walk across the
	// map for a place to idle is a bigger risk than idling.
	HoldBankMax = 160

	// HoldFoodTake is how much food a hold takes out of the bank when its pack has none.
	HoldFoodTake = 8

	// HoldFoodEvery spaces the hold's visits to the bank's food: an empty bank is not asked again at once.
	HoldFoodEvery = 30 * time.Minute

	// HoldKitEvery spaces the hold's visits to the bank's runes for a teleport kit: an account whose bank holds none is not
	// asked again at once.
	HoldKitEvery = time.Hour

	// HoldWalkRetryBase and HoldWalkRetryMax bound the wait after a walk to the bank that failed.
	HoldWalkRetryBase = 10 * time.Minute
	HoldWalkRetryMax  = time.Hour
)

// HoldSettledFresh is how long after the hold's last idle pass an account still counts as idling at its bank: a hold pass is a five second
// wait, and a ladder pass that has taken the account away is longer than this.
const HoldSettledFresh = time.Minute

// HoldSettled reports whether an account at here is where the hold waits: on the ground floor and within HoldBankRadius of bank. It is the
// opposite of HoldWalkWanted for a bank that is near enough to walk to, and false (rather than "stays put") for one that is not.
func HoldSettled(here, bank tile.Tile) bool {
	return here.Level == 0 && tile.Chebyshev(tile.Tile{X: here.X, Z: here.Z}, tile.Tile{X: bank.X, Z: bank.Z}) <= HoldBankRadius
}
