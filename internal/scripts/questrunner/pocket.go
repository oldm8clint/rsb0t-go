package questrunner

import (
	"sync"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Pocket is a place no walk leaves: its only exit is a scripted door the walk graph does not
// have, so a walk from inside to anywhere outside comes back "unreachable" however often it is
// tried. Khazard's building is one (Fight Arena's door1s open from inside only by script): live
// 4 Oct 12:10-12:45, three Stage 5 accounts left there by Fight Arena parked every later quest of
// the ladder on its first walk. A quest module registers its pocket with the way out (Leave), and
// Travel leaves a pocket before any walk to a destination outside it, whichever quest is running.
type Pocket struct {
	// Name is for the log.
	Name string
	// In reports a tile inside the pocket.
	In func(tile.Tile) bool
	// Leave walks the account out and reports whether it is out. It may Travel to tiles inside
	// the pocket (the door's inside tile); a Travel it makes to a tile outside is not hooked again.
	Leave func(d *Deps) bool
}

var (
	pocketsMu sync.RWMutex
	pockets   []Pocket
)

// RegisterPocket adds a pocket; a module's init registers its own.
func RegisterPocket(p Pocket) {
	if p.In == nil || p.Leave == nil {
		return
	}
	pocketsMu.Lock()
	defer pocketsMu.Unlock()
	pockets = append(pockets, p)
}

func registeredPockets() []Pocket {
	pocketsMu.RLock()
	defer pocketsMu.RUnlock()
	return append([]Pocket(nil), pockets...)
}

// PocketAt is the name of the registered pocket t lies in, if any.
func PocketAt(t tile.Tile) (string, bool) {
	for _, p := range registeredPockets() {
		if p.In(t) {
			return p.Name, true
		}
	}
	return "", false
}

// pocketFor is the registered pocket here lies in and dest does not, if any.
func pocketFor(ps []Pocket, here, dest tile.Tile) (Pocket, bool) {
	for _, p := range ps {
		if p.In(here) && !p.In(dest) {
			return p, true
		}
	}
	return Pocket{}, false
}

// leavePocketFor leaves the pocket the account stands in before a walk to dest outside it. It is
// a no-op inside a pocket's own Leave (which travels to the exit, inside the pocket, anyway).
func leavePocketFor(d *Deps, here, dest tile.Tile) {
	if d.leavingPocket {
		return
	}
	p, ok := pocketFor(registeredPockets(), here, dest)
	if !ok {
		return
	}
	d.logf("standing in %s at %d,%d, which no walk leaves: going out by its own exit before the walk to %d,%d,%d",
		p.Name, here.X, here.Z, dest.X, dest.Z, dest.Level)
	d.leavingPocket = true
	out := p.Leave(d)
	d.leavingPocket = false
	if !out {
		d.logf("could not leave %s from %v", p.Name, currentTile(d))
	}
}

// stuckPocketGap is how long a stuck ladder waits before it takes the account out of a pocket again: the way out of some pockets
// is a search of minutes (the Draynor Manor basement's levers), and the ladder is asked again every few seconds.
const stuckPocketGap = 2 * time.Minute

// leaveStuckPocket takes the account out of the registered pocket it stands in, by that pocket's own way out, when the ladder has
// nothing it can run. Live 19 Oct 2026, two Stage 5 accounts stood in the Draynor Manor basement for hours with the ladder stuck
// and every later walk the stage made (to Draynor, by the jail-avoiding webwalk) ending "unreachable" from inside it, because no
// quest was running to walk them out. It reports whether the account was in a pocket and is out of it now.
func (e *Engine) leaveStuckPocket(here tile.Tile) bool {
	p, ok := pocketFor(registeredPockets(), here, tile.Tile{})
	if !ok || e.D == nil || time.Since(e.lastStuckPocket) < stuckPocketGap {
		return false
	}
	e.lastStuckPocket = time.Now()
	e.D.logf("[ladder] stuck while standing in %s at %d,%d: going out by its own exit", p.Name, here.X, here.Z)
	e.D.leavingPocket = true
	out := p.Leave(e.D)
	e.D.leavingPocket = false
	if !out {
		e.D.logf("[ladder] could not leave %s from %v", p.Name, currentTile(e.D))
	}
	return out
}
