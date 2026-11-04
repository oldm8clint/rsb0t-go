package fleetrunner

import (
	"sync"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// HurtHeldTileCost is what a tile some pen's hunters hold adds to a step onto it in a walk made hurt and unfed. Four times a healthy
// pen walk's HeldTileCost: sixty steps of detour buy one tile less of a herd's ground, which is a detour of seven hundred steps round a
// thirteen-tile box, longer than any of the coast roads round the Feldip, Yanille and Ardougne herds.
const HurtHeldTileCost = 60

// walkHurt reports an account hurt and with nothing to eat (scarabplan.HurtUnfed), read from the session. Hitpoints are read first and the
// pack only when they are low, so a healthy walk pays nothing for it.
func walkHurt(d *Deps) bool {
	if d == nil || d.Session == nil {
		return false
	}
	hp, maxHP := ownHitpoints(d)
	return fleetplan.WalkHurt(hp, maxHP) && TotalFoodHeld(d) == 0
}

// hurtGrids is the held-tile bitmap of every pen with a table of threats, built once (penHeld).
var hurtGrids = sync.OnceValue(func() []*fleetplan.HeldGrid {
	var out []*fleetplan.HeldGrid
	seen := map[string]bool{}
	for _, p := range fleetplan.TrainPens {
		if len(p.Threats) == 0 || seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		out = append(out, PenHeld(p))
	}
	return out
})

// hurtStepCost is the webwalk.Options.StepCost of a walk made hurt and unfed: HurtHeldTileCost on every tile any pen's hunters hold,
// whatever pen the walk starts in (or none).
func hurtStepCost() func(x, z, level int) int {
	grids := hurtGrids()
	return func(x, z, level int) int {
		for _, g := range grids {
			if g.Holds(x, z, level) {
				return HurtHeldTileCost
			}
		}
		return 0
	}
}

// hurtWalkEvery spaces the line a hurt walk says, per account: a retrying caller must not fill the log with it.
const hurtWalkEvery = 2 * time.Minute

var hurtWalkSaid sync.Map // account -> time.Time of the last line

// sayHurtWalk says, at most once per hurtWalkEvery per account, that a walk is being made hurt and unfed and what that changes.
func sayHurtWalk(d *Deps, dest tile.Tile) {
	if d == nil {
		return
	}
	now := time.Now()
	if v, ok := hurtWalkSaid.Load(d.Me); ok && now.Sub(v.(time.Time)) < hurtWalkEvery {
		return
	}
	hurtWalkSaid.Store(d.Me, now)
	hp, maxHP := ownHitpoints(d)
	d.Logf("walk to %d,%d,%d at %d/%d hitpoints with no food: every danger zone applies whatever the combat level and the route keeps off every pen's hunting ground it can", dest.X, dest.Z, dest.Level, hp, maxHP)
}
