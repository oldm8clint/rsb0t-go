package fleetrunner

import (
	"sync"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// WALKING OUT OF A MONSTER PEN (ogresafe-1017, 17 Oct 2026).
//
// Every ScarabSwarm walk goes through WalkTo, and its only knowledge of monsters was two lists of rectangles: the wolves' dens
// and the Ardougne bears. From any of the Yanille west ogres' nine safespots the shortest way to anywhere north (the mule's post,
// the Ardougne banks, the other pens) runs through the herd's own ground: of the first 35 tiles of the route to the post, 24 are
// tiles an Ogre holds (scarabplan.PenThreat.Holds), where the way out by the pen's east gate costs 523 steps against 489 and
// holds none. One percent of cost, and the herd had no weight in it; no rectangle can say so either, because a walk that
// starts inside a zone moves freely until it leaves (webwalk.Options.AvoidZones). The walk itself is a continuous run and
// usually survives it (146 north walks from these spots in one four-hour log, one death); what kills is a walk that dwells
// there, and a lagging view, a stall or a retry made it dwell.
//
// So a walk that starts on a pen's ground is planned with the herd's tiles costing HeldTileCost each (webwalk.Options.StepCost,
// a soft avoid: where every way out crosses the herd, which every Feldip spot does, the walk still goes, by the way that crosses
// least), and does not click blind there (webwalk.WalkOptions.NoBlind): the ladder's scene and unstick rungs aim straight at
// the destination with no regard for what stands between.

// HeldTileCost is what a tile some hunter holds adds to a step onto it in a walk that starts on a pen's ground. Fifteen steps
// of detour buy one tile less of the herd's ground: the 4-step difference between the Yanille spots' two ways out is nothing
// against 24 held tiles, and a way with no held tile is worth a walk across a pen's width.
const HeldTileCost = 15

var penGrids = struct {
	sync.Mutex
	m map[string]*fleetplan.HeldGrid
}{m: map[string]*fleetplan.HeldGrid{}}

// PenHeld is the pen's held-tile bitmap (scarabplan.NewHeldGrid over its threats), built once.
func PenHeld(pen fleetplan.TrainPen) *fleetplan.HeldGrid {
	penGrids.Lock()
	defer penGrids.Unlock()
	if g, ok := penGrids.m[pen.Name]; ok {
		return g
	}
	g := fleetplan.NewHeldGrid(pen.Threats)
	penGrids.m[pen.Name] = g
	return g
}

// PensAround is every pen with a table of threats (the safespot pens) whose ground t is on (TrainPen.Near).
func PensAround(t tile.Tile) []fleetplan.TrainPen {
	var out []fleetplan.TrainPen
	for _, p := range fleetplan.TrainPens {
		if len(p.Threats) > 0 && p.Near(t) {
			out = append(out, p)
		}
	}
	return out
}

// PenWalkGuard is the soft cost and the blind-click rule for a walk that starts at from: both nil when from is on no pen's
// ground, and the walk is planned and clicked as before.
func PenWalkGuard(from tile.Tile) (stepCost func(x, z, level int) int, noBlind func(webwalk.Point) bool, pens []fleetplan.TrainPen) {
	pens = PensAround(from)
	if len(pens) == 0 {
		return nil, nil, nil
	}
	grids := make([]*fleetplan.HeldGrid, 0, len(pens))
	for _, p := range pens {
		grids = append(grids, PenHeld(p))
	}
	stepCost = func(x, z, level int) int {
		for _, g := range grids {
			if g.Holds(x, z, level) {
				return HeldTileCost
			}
		}
		return 0
	}
	noBlind = func(me webwalk.Point) bool {
		t := tile.Tile{X: me.X, Z: me.Z, Level: me.Level}
		for _, p := range pens {
			if p.Near(t) {
				return true
			}
		}
		return false
	}
	return stepCost, noBlind, pens
}
