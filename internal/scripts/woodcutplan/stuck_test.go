package woodcutplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// TestTallyNamesTheKindAndCountsOnlyTheCandidates is the log line's half of the 2026-09-23 wedge:
// "every tree within reach has an Ent at it (N excluded)" was printed for every kind of exclusion,
// with N counting every live entry in the set rather than the trees in reach. Acct212 printed it
// 658 times with no Ent within sight of it.
func TestTallyNamesTheKindAndCountsOnlyTheCandidates(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var b TreeBlocks
	ent, unreach, stall, other, far := at(1, 1), at(2, 2), at(3, 3), at(4, 4), at(90, 90)
	b.Block(ent, now)
	b.BlockBecause(unreach, now, UnreachableBlockFor, BlockUnreachable)
	b.BlockBecause(stall, now.Add(-10*time.Second), UnreachableBlockFor, BlockStall)
	b.BlockFor(other, now, UnreachableBlockFor)
	b.Block(far, now) // excluded, but not one of this pass's candidates

	got := b.Tally([]tile.Tile{ent, unreach, stall, other, at(5, 5)}, now)
	if got.Ent != 1 || got.Unreachable != 1 || got.Stall != 1 || got.Other != 1 {
		t.Errorf("Tally = %+v, want one of each kind", got)
	}
	if got.Total() != 4 {
		t.Errorf("Total = %d, want 4 — the far tree is not a candidate and the fifth is not excluded", got.Total())
	}
	if want := now.Add(UnreachableBlockFor - 10*time.Second); !got.Lifts.Equal(want) {
		t.Errorf("Lifts = %v, want the stall block's expiry %v (the soonest)", got.Lifts, want)
	}
	if s := got.String(); s != "1 Ent, 1 unreachable, 1 gave nothing, 1 other" {
		t.Errorf("String = %q", s)
	}

	// THE CASE THAT WAS LOGGED 658 TIMES: fifteen trees, all excluded as unreachable, no Ent.
	var trap TreeBlocks
	trees := make([]tile.Tile, 15)
	for i := range trees {
		trees[i] = at(2720+i, 3480)
		trap.BlockBecause(trees[i], now, UnreachableBlockFor, BlockUnreachable)
	}
	tally := trap.Tally(trees, now)
	if tally.Ent != 0 || tally.Unreachable != 15 {
		t.Errorf("trap tally = %+v, want 15 unreachable and no Ent", tally)
	}
	if strings.Contains(tally.String(), "Ent") {
		t.Errorf("a camp with no Ent in it must not be described as having one: %q", tally.String())
	}

	// No candidates means nothing to tally, whatever the set holds — "no tree in sight", not an
	// exclusion. The old Len-based check reported this as "every tree has an Ent at it".
	if n := b.Tally(nil, now).Total(); n != 0 {
		t.Errorf("Tally(nil) = %d, want 0", n)
	}
	var nilBlocks *TreeBlocks
	if n := nilBlocks.Tally(trees, now).Total(); n != 0 {
		t.Errorf("a nil set tallies nothing, got %d", n)
	}
}

// TestTheReasonFollowsTheNeverShortenRule: an Ent block that an unreachable block lands on a second
// later keeps both its lifetime (the existing rule) and its reason, or the axe-protecting exclusion
// would be reported as a harmless one.
func TestTheReasonFollowsTheNeverShortenRule(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tree := at(2709, 3510)
	var b TreeBlocks
	b.Block(tree, now)
	b.BlockBecause(tree, now.Add(time.Second), UnreachableBlockFor, BlockUnreachable)
	if why, _, ok := b.Why(tree, now.Add(2*time.Second)); !ok || why != BlockEnt {
		t.Errorf("Why = %v, %v — the Ent block must keep its reason", why, ok)
	}
	// The other way round the longer block wins, reason and all.
	var c TreeBlocks
	c.BlockBecause(tree, now, UnreachableBlockFor, BlockUnreachable)
	c.Block(tree, now)
	if why, until, ok := c.Why(tree, now); !ok || why != BlockEnt || !until.Equal(now.Add(EntBlockFor)) {
		t.Errorf("Why = %v until %v ok=%v, want Ent until %v", why, until, ok, now.Add(EntBlockFor))
	}
	// Sweep drops the reason with the entry.
	c.Sweep(now.Add(EntBlockFor))
	if _, _, ok := c.Why(tree, now.Add(EntBlockFor)); ok || len(c.why) != 0 {
		t.Errorf("Sweep left a reason behind: %v", c.why)
	}
}

// TestStuckRunCountsConsecutivePassesAndRestartsAfterAGap: the run the return decision reads.
func TestStuckRunCountsConsecutivePassesAndRestartsAfterAGap(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var r StuckRun
	for i := 1; i <= 3; i++ {
		if got := r.Note(now.Add(time.Duration(i) * 2 * time.Second)); got != i {
			t.Fatalf("pass %d: Note = %d", i, got)
		}
	}
	// A long gap (a bank trip, a fletch) is not the same run.
	if got := r.Note(now.Add(6*time.Second + StuckRunGap + time.Second)); got != 1 {
		t.Errorf("after a gap Note = %d, want 1", got)
	}
	r.Note(now.Add(StuckRunGap + 9*time.Second))
	r.Reset()
	if r.Passes() != 0 {
		t.Errorf("Reset left %d passes", r.Passes())
	}
}

// TestDecideStuckReturnsOnlyAwayFromTheAnchor pins the three answers that matter.
//
//  1. Acct212's tile: away from the anchor, still inside the leash, stuck for five passes — walk
//     back. The old loop never did, and waited 21.9 minutes.
//  2. At the anchor with every tree excluded (Ents at all three yews): wait, never walk away from
//     the trees, and never for longer than the bound.
//  3. The first few passes away from the anchor: wait, because most "every tree excluded" passes
//     clear on their own inside the exclusions' lifetimes.
func TestDecideStuckReturnsOnlyAwayFromTheAnchor(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	normal := TIERS[0] // anchor 2730,3486, leash 10
	trapTile := tile.Tile{X: 2730, Z: 3478}
	if !AtCamp(trapTile, normal) {
		t.Fatal("setup: the trap tile must be inside the leash — that is why the phase machine never walked it out")
	}
	unreach := BlockTally{Unreachable: 15, Lifts: now.Add(20 * time.Second)}

	for passes := 1; passes < StuckReturnAfter; passes++ {
		act, wait := DecideStuck(StuckSnapshot{Passes: passes, Here: trapTile, Tier: normal, Excluded: unreach, Now: now})
		if act != StuckWait || wait != StuckWaitMax {
			t.Errorf("pass %d away from the anchor: %v %v, want a bounded wait of %v", passes, act, wait, StuckWaitMax)
		}
	}
	if act, _ := DecideStuck(StuckSnapshot{Passes: StuckReturnAfter, Here: trapTile, Tier: normal, Excluded: unreach, Now: now}); act != StuckReturn {
		t.Errorf("pass %d at %v: %v, want a walk back to the anchor", StuckReturnAfter, trapTile, act)
	}

	yew := TIERS[4]
	ents := BlockTally{Ent: 3, Lifts: now.Add(EntBlockFor)}
	for _, passes := range []int{1, StuckReturnAfter, 500} {
		act, wait := DecideStuck(StuckSnapshot{Passes: passes, Here: yew.Anchor, Tier: yew, Excluded: ents, Now: now})
		if act != StuckWait {
			t.Errorf("pass %d at the yew anchor with Ents at every tree: %v, want a wait", passes, act)
		}
		if wait < StuckWaitMin || wait > StuckWaitMax {
			t.Errorf("pass %d: wait %v is outside [%v, %v]", passes, wait, StuckWaitMin, StuckWaitMax)
		}
	}
	// Another level is away, however close in x and z.
	if !AwayFromAnchor(tile.Tile{X: yew.Anchor.X, Z: yew.Anchor.Z, Level: 1}, yew) {
		t.Error("a tile on another level is not at the anchor")
	}
}

func TestStuckWaitForIsBounded(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, c := range []struct {
		lifts time.Time
		want  time.Duration
	}{
		{time.Time{}, StuckWaitMin},
		{now.Add(-time.Second), StuckWaitMin},
		{now.Add(time.Second), StuckWaitMin},
		{now.Add(3 * time.Second), 3 * time.Second},
		{now.Add(EntBlockFor), StuckWaitMax},
	} {
		if got := StuckWaitFor(BlockTally{Ent: 1, Lifts: c.lifts}, now); got != c.want {
			t.Errorf("lifts %v: wait %v, want %v", c.lifts.Sub(now), got, c.want)
		}
	}
}
