package pathfind

import "testing"

// pillarPack is open ground with one solid tile, the Nature ruins' Ruined Pillar in miniature: the pillar at (45,40), a target stand
// two tiles beyond it at (47,40).
func pillarPack() *Pack {
	p := openPack()
	makeSolid(p, 45, 40)
	return p
}

func TestEngineStepIsDiagonalThenAlongXThenAlongZ(t *testing.T) {
	p := openPack()
	at := NavPoint{X: 30, Z: 30}
	if to, moved := p.EngineStep(at, NavPoint{X: 35, Z: 33}); !moved || to != (NavPoint{X: 31, Z: 31}) {
		t.Fatalf("open ground: stepped to %v (moved %v), want the diagonal 31,31", to, moved)
	}
	// The diagonal refused: along x first.
	cutOffTile(p, 31, 31)
	if to, moved := p.EngineStep(at, NavPoint{X: 35, Z: 33}); !moved || to != (NavPoint{X: 31, Z: 30}) {
		t.Fatalf("diagonal refused: stepped to %v (moved %v), want along x to 31,30", to, moved)
	}
	// Both of those refused: along z.
	cutOffTile(p, 31, 30)
	if to, moved := p.EngineStep(at, NavPoint{X: 35, Z: 33}); !moved || to != (NavPoint{X: 30, Z: 31}) {
		t.Fatalf("diagonal and x refused: stepped to %v (moved %v), want along z to 30,31", to, moved)
	}
	// Straight east with the east tile refused: nothing else is tried (dz is 0), the entity stays.
	if to, moved := p.EngineStep(at, NavPoint{X: 35, Z: 30}); moved || to != at {
		t.Fatalf("straight east into a refused tile moved to %v", to)
	}
	// On the waypoint: no step.
	if _, moved := p.EngineStep(at, at); moved {
		t.Fatal("a step was taken from the waypoint itself")
	}
}

// THE INCIDENT (18 Oct 2026): a lone waypoint on the far side of a pillar from the same row parks the player against it, for good.
func TestALoneWaypointBehindAPillarParksThePlayerForGood(t *testing.T) {
	p := pillarPack()
	from := NavPoint{X: 44, Z: 40}
	stand := NavPoint{X: 47, Z: 40}
	end, arrived := p.EngineWalk(from, []NavPoint{stand}, 200)
	if arrived || end != from {
		t.Fatalf("a lone waypoint behind the pillar ended at %v (arrived %v), want parked at %v", end, arrived, from)
	}
	// A wider run-up does not help: every start on the row west of the pillar and the diagonals that funnel into it park too.
	for _, x := range []int{40, 41, 42, 43} {
		end, arrived := p.EngineWalk(NavPoint{X: x, Z: 40}, []NavPoint{stand}, 200)
		if arrived || end != (NavPoint{X: 44, Z: 40}) {
			t.Fatalf("from %d,40 ended at %v (arrived %v), want parked at 44,40", x, end, arrived)
		}
	}
	// The same walk as a route round the pillar arrives.
	route := p.FindPath(from, stand)
	if !route.OK || len(route.Waypoints) < 3 {
		t.Fatalf("no route round the pillar: %+v", route)
	}
	if end, arrived := p.EngineWalk(from, route.Waypoints, 200); !arrived || end != stand {
		t.Fatalf("the routed walk ended at %v (arrived %v), want %v; waypoints %v", end, arrived, stand, route.Waypoints)
	}
}

func TestEngineWalkFollowsWaypointsInOrderAndHonoursTheStepLimit(t *testing.T) {
	p := openPack()
	from := NavPoint{X: 10, Z: 10}
	end, arrived := p.EngineWalk(from, []NavPoint{{X: 14, Z: 10}, {X: 14, Z: 14}}, 100)
	if !arrived || end != (NavPoint{X: 14, Z: 14}) {
		t.Fatalf("ended at %v (arrived %v)", end, arrived)
	}
	if end, arrived := p.EngineWalk(from, []NavPoint{{X: 14, Z: 10}, {X: 14, Z: 14}}, 5); arrived || end != (NavPoint{X: 14, Z: 11}) {
		t.Fatalf("five steps ended at %v (arrived %v), want 14,11", end, arrived)
	}
	if end, arrived := p.EngineWalk(from, []NavPoint{{X: 10, Z: 10, Level: 1}}, 100); arrived || end != from {
		t.Fatalf("a waypoint on another level was walked to: %v %v", end, arrived)
	}
}

// The naive approach to a player: the waypoint is on the asker's side of the target and is walked greedily; a pillar on that side is a
// dead end, and the asker is never told.
func TestNaiveApproachDeadEndsOnTheFarSideOfAPillar(t *testing.T) {
	p := pillarPack()
	target := NavPoint{X: 44, Z: 40} // the crafter, west of the pillar
	// East of the pillar, on its row and above: the waypoint is the pillar's tile (the engine's tile on the asker's side of him).
	for _, from := range []NavPoint{{X: 47, Z: 40}, {X: 47, Z: 41}, {X: 48, Z: 41}, {X: 50, Z: 40}, {X: 47, Z: 39}} {
		end, reached := p.NaiveApproach(from, target, 100)
		if reached {
			t.Errorf("from %v the approach reached the crafter (ended %v), want a dead end against the pillar", from, end)
		}
		if end.X != 46 {
			t.Errorf("from %v the asker ended at %v, want parked against the pillar at x=46", from, end)
		}
	}
	// North of him, west of him and beside him: the op runs.
	for _, from := range []NavPoint{{X: 44, Z: 43}, {X: 41, Z: 40}, {X: 44, Z: 41}, {X: 43, Z: 40}, {X: 40, Z: 44}} {
		if end, reached := p.NaiveApproach(from, target, 100); !reached {
			t.Errorf("from %v the approach did not reach the crafter (ended %v)", from, end)
		}
	}
}

func TestNaiveApproachOnOpenGroundReachesAnyAsker(t *testing.T) {
	p := openPack()
	target := NavPoint{X: 30, Z: 30}
	for dx := -6; dx <= 6; dx++ {
		for dz := -6; dz <= 6; dz++ {
			from := NavPoint{X: 30 + dx, Z: 30 + dz}
			if from == target {
				continue
			}
			if end, reached := p.NaiveApproach(from, target, 60); !reached {
				t.Fatalf("from %v on open ground the approach ended at %v without reaching", from, end)
			}
		}
	}
}

func TestOperableFromIsOrthogonalAndNotOnTheTarget(t *testing.T) {
	p := openPack()
	target := NavPoint{X: 20, Z: 20}
	for _, from := range []NavPoint{{X: 21, Z: 20}, {X: 19, Z: 20}, {X: 20, Z: 21}, {X: 20, Z: 19}} {
		if !p.OperableFrom(from, target) {
			t.Errorf("%v is beside the target and answered not operable", from)
		}
	}
	for _, from := range []NavPoint{{X: 21, Z: 21}, {X: 19, Z: 19}, {X: 22, Z: 20}, target} {
		if p.OperableFrom(from, target) {
			t.Errorf("%v answered operable", from)
		}
	}
}
