package combatplan

import "rsb0t-go/internal/scripts/tile"

// RungGroundMargin is how far past a rung's leash its ground is counted: the herd's own aggression range and the tiles it
// follows an account out of the pen by (the Moss giants' wander is 5 more than their spawns).
const RungGroundMargin = 6

// RungAt is the trainer rung whose ground t is on: within the rung's Leash plus RungGroundMargin of its Stand, or within
// RungGroundMargin of one of its spawns, on the ground level, the nearest first. It reads every rung there is, the
// Wilderness ones too, because an account that is stood in a pen is in it whichever ladder the picker chose for it.
//
// WHY IT EXISTS. An account that waits (Stage 3's Phase3Wait) stood still where it was, and the walk to a bank that the wait
// makes for an account in a hunting pen (scarabswarm_waitaside.go) knew only the pens of Stage 3's own list. The Stage 5
// trainer's rungs are a different list: 14 Oct 2026, 11:15, an account that had been stopped in the North Ardougne Moss
// giants' rung and logged in at 11/54 began a Stage 3 magic slice where it stood, waited in place, and ate ten bites in
// five minutes beside the giant until it was out of food (and then died).
func RungAt(t tile.Tile) (Rung, bool) {
	if t.Level != 0 {
		return Rung{}, false
	}
	here := tile.Tile{X: t.X, Z: t.Z}
	var best Rung
	bestD, found := 0, false
	for _, r := range SpreadLadderWith(SpreadOptions{Wilderness: true}) {
		// d is how far past the ground's edge t is (negative inside it): from the stand's leash, or from the nearest spawn.
		d := tile.Chebyshev(here, tile.Tile{X: r.Stand.X, Z: r.Stand.Z}) - r.Leash
		// The herd's own spawns count too: a stand is chosen inside several of them, not on top of every one.
		for _, sp := range r.Spawns {
			if sd := tile.Chebyshev(here, tile.Tile{X: sp.X, Z: sp.Z}); sd < d {
				d = sd
			}
		}
		if d > RungGroundMargin {
			continue
		}
		if !found || d < bestD {
			best, bestD, found = r, d, true
		}
	}
	return best, found
}
