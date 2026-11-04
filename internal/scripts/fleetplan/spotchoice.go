package fleetplan

import (
	"rsb0t-go/internal/scripts/tile"
)

// HerdSpawns is the pen's herd the safespots are judged on: the Threats flagged Herd within the leash
// of the stand, which is the herd safeTargets (scarabrunner) can see.
func (p TrainPen) HerdSpawns() []PenThreat {
	var out []PenThreat
	for _, th := range p.Threats {
		if th.Herd && th.At.Level == p.Stand.Level && tile.Chebyshev(th.At, p.Stand) <= p.Leash {
			out = append(out, th)
		}
	}
	return out
}

// WanderRange is how far an NPC of this threat wanders from its spawn: its chase cut is maxrange + 1 and
// maxrange defaults to wanderrange + 2 (every herd the pens cast at has wanderrange 3).
func (t PenThreat) WanderRange() int { return max(t.Cut-3, 1) }
