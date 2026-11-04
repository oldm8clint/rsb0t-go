package randomevents

import "testing"

// fleeCandidates is the one piece of handle.go COLLAB.md's task calls out as likely
// testable in isolation: a pure ring-search with no network or pathfind.Pack involved.
// walkableFlee, walkTo, HandleHazard, HandleEvade and Handle itself all need a real
// *pathfind.Pack (FindPath against actual collision data) and/or a live actions.Session
// — deliberately not faked here, since a fake FindPath that always says "reachable"
// would prove nothing about the real routing decision, and constructing a genuine
// synthetic Pack requires internal/pathfind's own binary pack format (see its
// pack_test.go) which is that package's concern, not this one's. Skipped for the same
// reason COLLAB.md's task description gives for the rest of this package: it can only
// be judged against a live server's actual reachability.

func TestFleeCandidatesFloorsAtMinDistance(t *testing.T) {
	// dist=4 equals minFleeDist, so exactly one ring (8 compass points) comes back.
	out := fleeCandidates(100, 100, 0, 100, 100, 4)
	if len(out) != 8 {
		t.Fatalf("fleeCandidates(dist=4) returned %d points, want 8 (one ring, no dedup collisions)", len(out))
	}
	for _, p := range out {
		if p.Level != 0 {
			t.Errorf("candidate %+v has wrong level", p)
		}
		if d := chebyshev(p.X, p.Z, 100, 100); d != 4 {
			t.Errorf("candidate %+v is %d tiles from the origin, want 4", p, d)
		}
	}
}

func TestFleeCandidatesGeneratesEveryRingDownToTheFloor(t *testing.T) {
	// dist=8, ringStep=2, floor=4: rings at 8, 6, 4 — 24 points total, none deduped
	// since every compass direction at each distinct ring radius is a distinct tile.
	out := fleeCandidates(0, 0, 2, 0, 0, 8)
	if len(out) != 24 {
		t.Fatalf("fleeCandidates(dist=8) returned %d points, want 24 (three rings of 8)", len(out))
	}
	seen := map[[2]int]bool{}
	for _, p := range out {
		key := [2]int{p.X, p.Z}
		if seen[key] {
			t.Errorf("duplicate candidate %+v", p)
		}
		seen[key] = true
		if p.Level != 2 {
			t.Errorf("candidate %+v has wrong level, want 2", p)
		}
	}
}

func TestFleeCandidatesOrderedFarthestFromThreatFirst(t *testing.T) {
	// The threat sits well off to one side of the flee origin. The list must be sorted
	// by descending distance from the threat throughout (not just "farthest ring
	// first", which is already true by construction across ring boundaries), and the
	// very first candidate must actually be one of the farthest ones available —
	// Chebyshev distance ties along a ring's flat edges are expected and fine, so this
	// checks the sort property directly rather than assuming a single unique winner.
	out := fleeCandidates(0, 0, 0, 50, 0, 8)
	if len(out) < 2 {
		t.Fatalf("fleeCandidates returned too few points: %d", len(out))
	}
	maxDist := 0
	prevDist := chebyshev(out[0].X, out[0].Z, 50, 0)
	for i, p := range out {
		d := chebyshev(p.X, p.Z, 50, 0)
		if d > maxDist {
			maxDist = d
		}
		if i > 0 && d > prevDist {
			t.Fatalf("candidate %d (%+v, dist %d from threat) is farther than candidate %d (dist %d) — not sorted farthest-first", i, p, d, i-1, prevDist)
		}
		prevDist = d
	}
	if got := chebyshev(out[0].X, out[0].Z, 50, 0); got != maxDist {
		t.Fatalf("first candidate is %d tiles from the threat, want the overall farthest (%d)", got, maxDist)
	}
}

func TestFleeCandidatesBelowFloorProducesNoRings(t *testing.T) {
	out := fleeCandidates(0, 0, 0, 0, 0, 3) // below minFleeDist(4)
	if len(out) != 0 {
		t.Fatalf("fleeCandidates(dist=3) = %+v, want no candidates (below the 4-tile floor)", out)
	}
}

// TestFleeDistForEscalatesWithAttemptAndCaps covers the escalation HandleEvade now uses
// (see its own doc comment for the real production case this was added for): repeated
// failed attempts against the same chasing threat should try progressively farther, not
// repeat the identical already-failed distance, but must stay bounded.
func TestFleeDistForEscalatesWithAttemptAndCaps(t *testing.T) {
	if got := fleeDistFor(1); got != baseFleeDist {
		t.Errorf("fleeDistFor(1) = %d, want baseFleeDist (%d)", got, baseFleeDist)
	}
	if got := fleeDistFor(0); got != baseFleeDist {
		t.Errorf("fleeDistFor(0) = %d, want floored to baseFleeDist (%d)", got, baseFleeDist)
	}
	d1 := fleeDistFor(1)
	d2 := fleeDistFor(2)
	d3 := fleeDistFor(3)
	if !(d1 < d2 && d2 < d3) {
		t.Fatalf("fleeDistFor must strictly increase with attempt: got %d, %d, %d", d1, d2, d3)
	}
	if got := fleeDistFor(1000); got != maxFleeDist {
		t.Errorf("fleeDistFor(1000) = %d, want capped at maxFleeDist (%d)", got, maxFleeDist)
	}
}
