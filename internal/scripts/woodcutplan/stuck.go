package woodcutplan

// What an account does when it keeps finding trees in reach and chopping none of them.
//
// MEASURED on the live fleet, 2026-09-23 02:35-02:57 Perth, build 3dfb543a: Acct212 stood on
// 2730,3478 for 21.9 minutes at zero experience, logging "every tree within reach has an Ent at
// it" 658 times, and only an external restart freed it. It was inside a walled building whose one
// door was shut; every tree's stand tiles were outside. Each pass excluded the trees it could not
// route to, reported them all excluded, sent one raw walk at the camp anchor straight through the
// wall, waited two seconds and did it again — and because the tile is inside the leash, the phase
// machine never took it back to PhaseWalk, which is the one that uses the door-aware walker.
//
// Nothing about the tile or the exclusions changes by waiting, so "wait and look again" is only
// the right answer when the account is where the trees are. These rules say when it is not.

import (
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// StuckReturnAfter is how many consecutive passes that found trees in reach and chopped none an
// account AWAY from its camp's anchor waits through before it walks back to the anchor with the
// door-aware walker instead.
//
// Five, not one: a pass that finds every tree excluded is usually a passing state (an Ent, a tree
// that gave nothing, a stand tile blocked a moment ago) that clears within the exclusions' own
// lifetimes, and walking to the anchor costs a few seconds of chopping each time. Five passes is
// ten to twenty-five seconds, well inside the 30-second unreachable exclusion, so a real wedge
// costs that and no more — against the 21.9 minutes measured above.
const StuckReturnAfter = 5

// StuckAnchorRadius is how close to the anchor counts as already there: walking two tiles back to
// the anchor buys nothing, because the tree search from there sees the same trees.
const StuckAnchorRadius = 2

// StuckRunGap is how long a gap between stuck passes restarts the count. An account that banked,
// fletched or walked in between has not been stuck for the whole time.
const StuckRunGap = 60 * time.Second

// StuckWaitMin and StuckWaitMax bound one wait. The minimum is the two seconds every other waiting
// branch of this script already uses; the maximum keeps the account re-checking the camp often
// enough to take a tree the moment one comes free, and keeps a ProgressiveWCFletch account's
// fletch-while-waiting (it fletches on the pass after a wait) from being held back.
const (
	StuckWaitMin = 2 * time.Second
	StuckWaitMax = 5 * time.Second
)

// StuckRun counts consecutive passes that found trees in reach and chopped none. The zero value is
// ready to use.
type StuckRun struct {
	passes int
	last   time.Time
}

// Note records one stuck pass and returns how many there have now been in a row.
func (r *StuckRun) Note(now time.Time) int {
	if r.passes == 0 || now.Sub(r.last) > StuckRunGap {
		r.passes = 0
	}
	r.passes++
	r.last = now
	return r.passes
}

// Reset ends the run: the pass chopped something, or did something other than fail to.
func (r *StuckRun) Reset() { r.passes = 0 }

// Passes is the current run length.
func (r *StuckRun) Passes() int { return r.passes }

// StuckAction is what the account does about this stuck pass.
type StuckAction int

const (
	// StuckWait: wait a bounded time where it is, for an exclusion to lift.
	StuckWait StuckAction = iota
	// StuckReturn: walk back to the camp's anchor with the door-aware walker.
	StuckReturn
)

func (a StuckAction) String() string {
	if a == StuckReturn {
		return "return"
	}
	return "wait"
}

// StuckSnapshot is everything DecideStuck looks at.
type StuckSnapshot struct {
	// Passes is the run length INCLUDING this pass (StuckRun.Note's answer).
	Passes int
	Here   tile.Tile
	Tier   Tier
	// Excluded is the tally of the candidates in reach; its Lifts bounds the wait.
	Excluded BlockTally
	Now      time.Time
}

// AwayFromAnchor reports whether here is further from the tier's anchor than StuckAnchorRadius, or
// on another level.
func AwayFromAnchor(here tile.Tile, tier Tier) bool {
	return here.Level != tier.Anchor.Level || tile.Chebyshev(here, tier.Anchor) > StuckAnchorRadius
}

// DecideStuck chooses between waiting and walking back to the anchor, and how long to wait.
//
// The wait is always bounded: until the first exclusion among the candidates lifts, clamped to
// [StuckWaitMin, StuckWaitMax]. The walk back is only ever chosen away from the anchor, so an
// account at its camp with every tree excluded — an Ent at each of the three yews, say — waits
// there, which is what keeps the axe in one piece.
func DecideStuck(s StuckSnapshot) (StuckAction, time.Duration) {
	if s.Passes >= StuckReturnAfter && AwayFromAnchor(s.Here, s.Tier) {
		return StuckReturn, 0
	}
	return StuckWait, StuckWaitFor(s.Excluded, s.Now)
}

// StuckWaitFor is the bounded wait for a tally: until its first exclusion lifts, clamped.
func StuckWaitFor(t BlockTally, now time.Time) time.Duration {
	if t.Lifts.IsZero() {
		return StuckWaitMin
	}
	d := t.Lifts.Sub(now)
	if d < StuckWaitMin {
		return StuckWaitMin
	}
	if d > StuckWaitMax {
		return StuckWaitMax
	}
	return d
}
