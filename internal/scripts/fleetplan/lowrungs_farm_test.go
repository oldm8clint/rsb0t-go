package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Moving the anchor must not shrink the home ground: an account in the farmhouse, the wheat field or
// the sheep pen is still on the Farmer rung's ground, so Stage 3 walks it out of the gate rather than
// ~80 tiles "home" from an Ardougne it never left.
func TestTheWholeFarmStaysHomeGround(t *testing.T) {
	for _, at := range []tile.Tile{
		{X: 2646, Z: 3364}, // farmhouse
		{X: 2652, Z: 3351}, // wheat field
		{X: 2659, Z: 3362}, // sheep pen, where acct57 was stuck
		{X: 2650, Z: 3374}, // sheep pen, where Acct225 was stuck
		{X: 2641, Z: 3372}, // the anchor
	} {
		if !OnArdougneLowRung(at) {
			t.Errorf("%d,%d is no longer the Farmer rung's home ground", at.X, at.Z)
		}
	}
}
