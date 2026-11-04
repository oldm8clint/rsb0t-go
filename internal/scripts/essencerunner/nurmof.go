package essencerunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/tile"
)

// The way back up from Nurmof's pickaxe shop. The trapdoor at (3019,3450) takes a miner down (BuyFromNurmof); the
// "Ladder" (loc 1755, "Climb-up") at (3019,9850) takes it up again, thirty tiles from Nurmof's stand and on the pack's own
// paths (checked against the real loc index and collision pack). Nothing in the original script came back up: a miner that
// bought a pickaxe and then walked to Aubury would have walked underground for good.
const (
	DwarvenLadderName = "Ladder"
	DwarvenLadderOp   = "Climb-up"
)

// DwarvenLadder is where the ladder is (loc 1755), for a walk when none is in sight.
var DwarvenLadder = tile.Tile{X: 3019, Z: 9850}

// Underground reports whether the account is in the Dwarven Mine (its mapsquares are z 9000 and up).
func Underground(d *Deps) bool {
	_, z, _, ok := d.Session.State().Tile()
	return ok && z >= essenceplan.NurmofHopZThreshold
}

// LeaveDwarvenMine climbs the ladder up out of the Dwarven Mine, up to tries times, and reports whether the account is on
// the surface. Already there: nothing is clicked.
func LeaveDwarvenMine(d *Deps, tries int) bool {
	for try := 1; try <= tries && Underground(d); try++ {
		here := currentTile(d)
		m, ok := d.Locs.Nearest(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: here.Level, Radius: 60, Name: DwarvenLadderName, Op: DwarvenLadderOp}, locindex.ChangesFrom(d.Session.State().LocChanges()))
		if !ok {
			d.logf("no ladder in sight in the Dwarven Mine (try %d of %d): walking to %d,%d", try, tries, DwarvenLadder.X, DwarvenLadder.Z)
			WalkNear(d, DwarvenLadder, 2)
			continue
		}
		at := tile.Tile{X: m.X, Z: m.Z, Level: m.Level}
		if tile.Chebyshev(here, at) > 1 {
			WalkNear(d, at, 1)
		}
		if err := actions.OpLoc(d.Session, m.Op, m.X, m.Z, m.ID); err != nil {
			d.logf("OpLoc(%s) on the ladder (try %d of %d): %v", DwarvenLadderOp, try, tries, err)
			continue
		}
		waitFor(d, "climbing out of the Dwarven Mine", 10*time.Second, func() bool { return !Underground(d) })
	}
	return !Underground(d)
}

// BuyPickaxeAtNurmof takes the trapdoor down, buys the first of items Nurmof has on his shelf and the account can pay for
// (best first: a shelf that is out falls to the next), and climbs back up whatever came of it. It reports the item bought
// or why none was, and says so when the account could not get out.
func BuyPickaxeAtNurmof(d *Deps, items []string) (bought, why string) {
	var tried []string
	for _, item := range items {
		tried = append(tried, item)
		if BuyFromNurmof(d, item) {
			bought = item
			break
		}
		if !Underground(d) {
			break // never got down (the walk, the trapdoor): the next item would fail the same way
		}
	}
	out := LeaveDwarvenMine(d, 3)
	switch {
	case bought == "" && !out:
		return "", fmt.Sprintf("nothing was bought (tried %s) and the account is STILL IN the Dwarven Mine", strings.Join(tried, ", "))
	case bought == "":
		return "", fmt.Sprintf("nothing was bought (tried %s)", strings.Join(tried, ", "))
	case !out:
		return bought, "bought, but the account is STILL IN the Dwarven Mine"
	}
	return bought, ""
}
