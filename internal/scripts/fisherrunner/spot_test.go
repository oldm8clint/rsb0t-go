package fisherrunner

import (
	"testing"

	"rsb0t-go/internal/gamestate"
)

// naiveNearestPick is the OLD FindSpot rule this task's fix replaces: pure nearest-by-Dist,
// with no memory of which spot was already being fished. Reproduced here (not in runner.go —
// the real fix already replaced it there) purely so this test can demonstrate, side by side,
// the exact oscillation the account owner reported live and confirm pickSpot no longer has it.
func naiveNearestPick(candidates []spotCandidate) (spot gamestate.NPC, found bool) {
	best := gamestate.NPC{}
	bestDist := 0
	for _, c := range candidates {
		if !found || c.Dist < bestDist {
			best, bestDist, found = c.NPC, c.Dist, true
		}
	}
	return best, found
}

// TestNaiveNearestPickOscillatesBetweenTwoCloseSpots reproduces the account owner's live
// report against the OLD rule: two real "Fishing spot" NPCs near Catherby's camp — confirmed
// live 2026-09-16 via a throwaway account teleported to fisherplan.Catherby().Camp — sit at
// slightly different, and slightly SHIFTING, distances from camp as they periodically
// despawn/respawn a few tiles away (real Catherby spot behavior, not a test fixture
// assumption — see FindSpot's own doc comment for the live transcript this matches: spot
// index 2171 at 2853,3423 despawned mid-session and was replaced by index 2170, whose own
// respawn tile then put it at very different distances from camp call to call). Each call
// below models one such respawn tick: spot A and spot B trade which one is nearer. The naive
// rule has no concept of "the spot already being fished" so it dutifully follows the nearer
// one back and forth every single call — exactly the wasted-walk oscillation reported live.
func TestNaiveNearestPickOscillatesBetweenTwoCloseSpots(t *testing.T) {
	spotA := gamestate.NPC{Index: 100, X: 2836, Z: 3431}
	spotB := gamestate.NPC{Index: 200, X: 2853, Z: 3423}

	// Tick 1: A is nearer (9 vs 10) — picked.
	got, ok := naiveNearestPick([]spotCandidate{{NPC: spotA, Dist: 9, Here: 9, Crowd: 0}, {NPC: spotB, Dist: 10, Here: 10, Crowd: 0}})
	if !ok || got.Index != spotA.Index {
		t.Fatalf("tick 1: got index %d, want A (100)", got.Index)
	}
	// Tick 2: a respawn nudges B closer than A — the naive rule abandons A for B, even though
	// nothing about A itself changed or went bad.
	got, ok = naiveNearestPick([]spotCandidate{{NPC: spotA, Dist: 9, Here: 9, Crowd: 0}, {NPC: spotB, Dist: 8, Here: 8, Crowd: 0}})
	if !ok || got.Index != spotB.Index {
		t.Fatalf("tick 2: got index %d, want B (200) — the naive rule should have swung to the marginally closer spot", got.Index)
	}
	// Tick 3: another respawn nudges A closer again — swings right back, having caught
	// nothing at B in between. This back-and-forth, repeated indefinitely as ordinary respawn
	// jitter continues, is the live-reported bug: walking, not fishing.
	got, ok = naiveNearestPick([]spotCandidate{{NPC: spotA, Dist: 7, Here: 7, Crowd: 0}, {NPC: spotB, Dist: 8, Here: 8, Crowd: 0}})
	if !ok || got.Index != spotA.Index {
		t.Fatalf("tick 3: got index %d, want A (100) — demonstrating the flip back, the oscillation this task closes", got.Index)
	}
}

// TestPickSpotStaysOnCurrentThroughTheSameJitter runs the identical three respawn ticks from
// TestNaiveNearestPickOscillatesBetweenTwoCloseSpots through the actual fixed selection rule
// (pickSpot, as FindSpot now calls it), each tick feeding the previous tick's own result back
// in as current/hasCurrent exactly the way FishUntil's lastSpot/haveLastSpot do live. Spot A
// is chosen once, same as the naive rule, but then never abandoned for B despite B becoming
// marginally nearer on ticks 2 and 3 — because A never actually left the candidate list, only
// FishUntil's own genuine "not still there" detection (npcStillThere) can lose it. This is the
// stability the account owner's live oscillation needed and the naive rule never had.
func TestPickSpotStaysOnCurrentThroughTheSameJitter(t *testing.T) {
	spotA := gamestate.NPC{Index: 100, X: 2836, Z: 3431}
	spotB := gamestate.NPC{Index: 200, X: 2853, Z: 3423}

	var current gamestate.NPC
	hasCurrent := false

	ticks := []struct {
		distA, distB int
	}{
		{9, 10}, // A nearer — first pick, no current yet
		{9, 8},  // B marginally nearer now — should NOT switch
		{7, 8},  // A marginally nearer again — should still not switch (never left A)
	}
	for i, tk := range ticks {
		got, ok := pickSpot([]spotCandidate{{NPC: spotA, Dist: tk.distA, Here: tk.distA}, {NPC: spotB, Dist: tk.distB, Here: tk.distB}}, current, hasCurrent, nil)
		if !ok {
			t.Fatalf("tick %d: expected a spot to be found", i+1)
		}
		if got.Index != spotA.Index {
			t.Fatalf("tick %d: got index %d, want A (100) to stay selected — pickSpot should not abandon a still-present current spot for a merely closer one", i+1, got.Index)
		}
		current, hasCurrent = got, true
	}
}

// TestPickSpotSwitchesOnceCurrentTrulyVanishes confirms pickSpot is not stuck forever on a
// stale current: once the previously-held spot's identity (index+tile, npcStillThere's own
// test) no longer appears among the live candidates at all — a real despawn, not a mere
// respawn-jitter distance change — it falls back to the ordinary nearest search exactly like
// the naive rule, matching FindSpot's own doc comment ("stability beats marginal optimality"
// — not "current beats everything forever").
func TestPickSpotSwitchesOnceCurrentTrulyVanishes(t *testing.T) {
	spotA := gamestate.NPC{Index: 100, X: 2836, Z: 3431}
	spotB := gamestate.NPC{Index: 200, X: 2853, Z: 3423}
	// current was spot A, but A is no longer in the candidate list at all (a genuine
	// despawn) — only B remains.
	got, ok := pickSpot([]spotCandidate{{NPC: spotB, Dist: 8, Here: 8}}, spotA, true, nil)
	if !ok || got.Index != spotB.Index {
		t.Fatalf("got index %d (ok=%v), want B (200) once A is genuinely gone", got.Index, ok)
	}
}

// TestPickSpotIndexReuseAtADifferentTileIsNotTreatedAsCurrent covers the same identity edge
// case npcStillThere's own doc comment already calls out: a despawned spot's NPC index can be
// reused by an unrelated fresh spot elsewhere. A candidate sharing current's Index but not its
// tile must NOT be treated as "still current" — that would wrongly keep the account glued to
// whatever fresh spot happened to reuse the old index, however far away it actually respawned.
func TestPickSpotIndexReuseAtADifferentTileIsNotTreatedAsCurrent(t *testing.T) {
	current := gamestate.NPC{Index: 100, X: 2836, Z: 3431}
	reused := gamestate.NPC{Index: 100, X: 2900, Z: 3500} // same index, far tile — a new spot
	other := gamestate.NPC{Index: 200, X: 2853, Z: 3423}

	got, ok := pickSpot([]spotCandidate{{NPC: reused, Dist: 90, Here: 90}, {NPC: other, Dist: 8, Here: 8}}, current, true, nil)
	if !ok || got.Index != other.Index {
		t.Fatalf("got index %d at %d,%d (ok=%v), want the nearer genuine candidate (200), not the reused index at a different tile", got.Index, got.X, got.Z, ok)
	}
}

// TestPickSpotNoCandidatesReturnsNotFound is the plain empty case FindSpot's own blocked
// return already relies on: no eligible candidates at all (everything filtered out by name,
// range, or safety before pickSpot is even called) yields found=false regardless of current.
func TestPickSpotNoCandidatesReturnsNotFound(t *testing.T) {
	if _, ok := pickSpot(nil, gamestate.NPC{}, false, nil); ok {
		t.Fatalf("expected found=false with no candidates")
	}
	if _, ok := pickSpot(nil, gamestate.NPC{Index: 1}, true, nil); ok {
		t.Fatalf("expected found=false with no candidates even with a current set")
	}
}

// TestPickSpotSpreadsARealisedCrowd: a fresh choice weighs the fishers already at a spot, so a
// released crowd does not all take the nearest one (live 2026-09-20, 2855,3423).
func TestPickSpotSpreadsARealisedCrowd(t *testing.T) {
	near := gamestate.NPC{Index: 1, X: 2855, Z: 3423}
	far := gamestate.NPC{Index: 2, X: 2838, Z: 3431}
	got, _ := pickSpot([]spotCandidate{{NPC: near, Dist: 3, Here: 3, Crowd: 0}, {NPC: far, Dist: 9, Here: 9, Crowd: 0}}, gamestate.NPC{}, false, nil)
	if got.Index != near.Index {
		t.Fatalf("with nobody at either, want the nearer spot, got %d", got.Index)
	}
	got, _ = pickSpot([]spotCandidate{{NPC: near, Dist: 3, Here: 3, Crowd: 12}, {NPC: far, Dist: 9, Here: 9, Crowd: 1}}, gamestate.NPC{}, false, nil)
	if got.Index != far.Index {
		t.Fatalf("with twelve at the near spot and one at the far, want the far spot, got %d", got.Index)
	}
	// Staying on the spot being fished still beats everything, crowded or not.
	got, _ = pickSpot([]spotCandidate{{NPC: near, Dist: 3, Here: 3, Crowd: 12}, {NPC: far, Dist: 9, Here: 9, Crowd: 1}}, near, true, nil)
	if got.Index != near.Index {
		t.Fatalf("current spot abandoned for crowding, got %d", got.Index)
	}
	// Jitter decides between two equal spots instead of every account taking the first.
	pickedFar := 0
	for i := 0; i < 4; i++ {
		// The jitter source is consulted once per candidate, near first.
		draws := []int{3, 0}
		if i%2 == 1 {
			draws = []int{0, 3}
		}
		k := 0
		got, _ = pickSpot([]spotCandidate{{NPC: near, Dist: 5, Here: 5, Crowd: 0}, {NPC: far, Dist: 5, Here: 5, Crowd: 0}}, gamestate.NPC{}, false, func(int) int {
			v := draws[k]
			k++
			return v
		})
		if got.Index == far.Index {
			pickedFar++
		}
	}
	if pickedFar == 0 || pickedFar == 4 {
		t.Fatalf("jitter never split equal spots (far picked %d of 4)", pickedFar)
	}
}

// --- Choosing by the walk the account actually pays, not by distance from the camp tile ---
//
// Live 2026-09-21, the whole 269-account fleet over an hour: 4,458 spot changes, median hop
// 6 tiles, p90 10, 38% of hops 8 tiles or more, 13.4 account-hours = 5.4% of all fleet time.
// The rule that paid for those hops scored candidates by Chebyshev from the CAMP, so an
// account whose spot vanished ten tiles out was sent to whatever sat nearest the camp centre.

// This is the live shape directly: the account is standing at the eastern spot it was just
// fishing, that spot despawns, and there are two replacements — one right beside it and one
// back at the camp. The camp-distance rule picks the camp one and pays a ten-tile walk.
func TestPickSpotTakesTheSpotItIsStandingNextTo(t *testing.T) {
	camp := gamestate.NPC{Index: 300, X: 2845, Z: 3431}   // 0 from the camp, 10 from the account
	beside := gamestate.NPC{Index: 400, X: 2836, Z: 3431} // 9 from the camp, 1 from the account

	got, ok := pickSpot([]spotCandidate{
		{NPC: camp, Dist: 0, Here: 10},
		{NPC: beside, Dist: 9, Here: 1},
	}, gamestate.NPC{}, false, nil)
	if !ok {
		t.Fatal("expected a spot to be found")
	}
	if got.Index != beside.Index {
		t.Fatalf("got index %d (the camp-nearest spot, a 10-tile walk), want %d — the one the account is standing beside",
			got.Index, beside.Index)
	}
}

// The crowd term must still outrank a shorter walk, or the 2026-09-20 incident comes back:
// pure nearest sent about 139 released accounts onto one spot in the same second and a
// whirlpool took them. crowdWeight is 2 tiles per other fisher, so five fishers on the near
// spot is worth ten tiles of walking away from it.
func TestPickSpotStillAvoidsACrowdedSpot(t *testing.T) {
	crowded := gamestate.NPC{Index: 500, X: 2836, Z: 3431}
	empty := gamestate.NPC{Index: 600, X: 2850, Z: 3431}

	got, ok := pickSpot([]spotCandidate{
		{NPC: crowded, Dist: 9, Here: 1, Crowd: 5}, // score 1 + 10 = 11
		{NPC: empty, Dist: 5, Here: 8},             // score 8
	}, gamestate.NPC{}, false, nil)
	if !ok || got.Index != empty.Index {
		t.Fatalf("got index %d (ok=%v), want the empty spot %d — the crowd term must still outweigh a shorter walk",
			got.Index, ok, empty.Index)
	}
}

// Equal walk and equal crowd: prefer the one nearer the camp, so a released crowd gravitates
// back to the camp core instead of settling wherever the NPC list happened to be ordered.
func TestPickSpotBreaksAnEqualWalkTowardTheCamp(t *testing.T) {
	far := gamestate.NPC{Index: 700, X: 2860, Z: 3431}
	near := gamestate.NPC{Index: 800, X: 2843, Z: 3431}

	// far comes FIRST in the list, so only the tie-break can choose near.
	got, ok := pickSpot([]spotCandidate{
		{NPC: far, Dist: 15, Here: 6},
		{NPC: near, Dist: 2, Here: 6},
	}, gamestate.NPC{}, false, nil)
	if !ok || got.Index != near.Index {
		t.Fatalf("got index %d (ok=%v), want %d — an equal walk should be broken toward the camp",
			got.Index, ok, near.Index)
	}
}

// The oscillation fix outranks everything: a still-present current spot wins even when a
// candidate is both a shorter walk AND nearer the camp. Without this, scoring on the walk
// would reintroduce the flip-flop the camp rule was originally changed to stop, because the
// account's own tile moves as it fishes.
func TestPickSpotWalkCostDoesNotOverrideAStillPresentCurrent(t *testing.T) {
	current := gamestate.NPC{Index: 100, X: 2836, Z: 3431}
	tempting := gamestate.NPC{Index: 200, X: 2837, Z: 3431}

	got, ok := pickSpot([]spotCandidate{
		{NPC: current, Dist: 9, Here: 4},
		{NPC: tempting, Dist: 1, Here: 1},
	}, current, true, nil)
	if !ok || got.Index != current.Index {
		t.Fatalf("got index %d (ok=%v), want the current spot %d to keep winning outright",
			got.Index, ok, current.Index)
	}
}

// Walking away is never chosen over standing still when nothing else differs — the property
// the whole change exists for, asserted over a spread of layouts rather than one case.
func TestPickSpotNeverPrefersTheLongerWalkAllElseEqual(t *testing.T) {
	for here := 1; here <= 20; here++ {
		for extra := 1; extra <= 5; extra++ {
			near := gamestate.NPC{Index: 1, X: 2836, Z: 3431}
			farther := gamestate.NPC{Index: 2, X: 2850, Z: 3431}
			// farther is nearer the CAMP, which is exactly what the old rule chose on.
			got, ok := pickSpot([]spotCandidate{
				{NPC: farther, Dist: 0, Here: here + extra},
				{NPC: near, Dist: 20, Here: here},
			}, gamestate.NPC{}, false, nil)
			if !ok || got.Index != near.Index {
				t.Fatalf("here=%d extra=%d: got index %d, want the shorter walk (1)", here, extra, got.Index)
			}
		}
	}
}
