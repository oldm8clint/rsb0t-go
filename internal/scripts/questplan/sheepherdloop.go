package questplan

import (
	"fmt"
	"strings"
)

// Sheep Herder's push loop: the pure half of one herding leg, ported from the TypeScript def's herd.ts herdToPen. It decides, push by
// push, which copy of the sheep to follow, which way to send it and where to stand, against a HerdWorld that is either the live
// session (questmods) or a simulation of the content's sheep (the tests). The loop never trusts a plan past the push it was made for:
// the sheep's ai_timer fires every 10 to 29 ticks and returns it to wander mode, which walks it back toward its own spawn between
// pushes, so every push re-derives the route from the sheep's tile (TS pitfalls 23, "the route is worthless the moment it is computed").

// HerdPos is a ground-floor tile.
type HerdPos struct{ X, Z int }

// Loop limits, from the TS def's measurements (11 and 20 minutes for the whole quest on two clean accounts, 37 to 255 pushes for one sheep).
const (
	// HerdPushBudget is the pushes one leg may spend: the shortest route is 31 pushes, the longest 58, and drift near a sheep's spawn
	// roughly triples that.
	HerdPushBudget = 260
	// HerdStuckLimit is the consecutive pushes that move nothing before the leg gives up on this sheep.
	HerdStuckLimit = 8
	// HerdStrayLimit is the off-axis landings in a row before the loop stops trusting the server to take the last step and walks the
	// stand until one lands on the axis.
	HerdStrayLimit = 6
	// HerdPinnedWait is the ticks given a sheep on a trap to wander clear, and HerdPinnedLimit the waits a leg allows.
	HerdPinnedWait  = 3
	HerdPinnedLimit = 8
	// HerdLockRadius is how far the herded sheep may be from where the last push left it and still be the same sheep: three copies
	// of every sheep share one name and one quest bit, and the client's slot churns when the scene is rebuilt.
	HerdLockRadius = 5
	// HerdApproachRay is how far back along the push axis a Prod may be issued from without walking first: the server takes the last
	// step to the stand when the account is on the line behind the sheep.
	HerdApproachRay = 3
	// HerdProgressEvery paces the loop's progress line.
	HerdProgressEvery = 25
)

// HerdWorld is what the loop sees and does.
type HerdWorld interface {
	// Copies lists the tile of every copy of the herded sheep in view.
	Copies() []HerdPos
	// Find is the copy to herd: the one in view with the shortest push route home, found by walking to the sheep's field first when
	// none is in view (an npc enters the client's list only within about fifteen tiles, so "none in the scene" says where the account
	// stands, not where the sheep is). ok is false when no sheep with a push route can be found.
	Find() (HerdPos, bool)
	// Approach walks the account to within a few tiles of the sheep at p and reports whether it got there.
	Approach(p HerdPos) bool
	// Me is where the account stands; ok is false before a position has been read.
	Me() (HerdPos, bool)
	// WalkTo walks to exactly (x, z) and reports whether the account stands there.
	WalkTo(x, z int) bool
	// Prod sends the Prod op on the copy standing at at and waits for the sheep to leave that tile or for the wait to run out.
	Prod(at HerdPos) HerdProd
	// Wait lets ticks game ticks pass.
	Wait(ticks int)
	// Stop reports that the leg should end (the quest's time is up).
	Stop() bool
	// Logf writes a line to the log.
	Logf(format string, args ...any)
}

// HerdProd is what one Prod came to.
type HerdProd struct {
	// Sent is whether the op went out at all (a copy without a Prod option, a closed pack of ops).
	Sent bool
	// Moved is whether the sheep left the tile (or went out of view) inside the wait.
	Moved bool
	// After is the sheep's tile once it moved, valid when Seen.
	After HerdPos
	Seen  bool
}

// HerdResult is how a leg ended.
type HerdResult struct {
	Penned bool
	Pushes int
	// Why says what stopped an unsuccessful leg.
	Why string
}

// HerdNearest is the copy within HerdLockRadius of last, nearest first (Chebyshev); ok is false when there is none.
func HerdNearest(copies []HerdPos, last HerdPos) (HerdPos, bool) {
	best, bestD, found := HerdPos{}, 0, false
	for _, c := range copies {
		d := max(abs(c.X-last.X), abs(c.Z-last.Z))
		if d <= HerdLockRadius && (!found || d < bestD) {
			best, bestD, found = c, d, true
		}
	}
	return best, found
}

// HerdPick is the copy with the shortest push route to the goal, ignoring any that no push can move: a sheep in a trap has no route and
// the other two copies carry the same quest bit, so fetching one of those skips the 500 tick teleport wait.
func HerdPick(g HerdGrid, goal HerdBox, copies []HerdPos) (HerdPos, bool) {
	best, bestCost, found := HerdPos{}, 0, false
	for _, c := range copies {
		plan := NewHerdPlan(g, c.X, c.Z, goal)
		cost, ok := plan.Cost(c.X, c.Z)
		if !ok {
			continue
		}
		if !found || cost < bestCost {
			best, bestCost, found = c, cost, true
		}
	}
	return best, found
}

// HerdBehind reports whether a Prod issued from me will be run from the tile the push needs: me is on the line directly behind the
// sheep, one to HerdApproachRay tiles back.
func HerdBehind(me, sheep HerdPos, dir HerdDir) bool {
	for step := 1; step <= HerdApproachRay; step++ {
		if me.X == sheep.X-dir.DX*step && me.Z == sheep.Z-dir.DZ*step {
			return true
		}
	}
	return false
}

// HerdLoop pushes the sheep last stands on out to the gate zone and over the fence, until it is in the enclosure, the leg runs out of
// pushes, the sheep is lost, pinned, or refuses, or the world says stop. pen is the enclosure's box and gate the zone a prod jumps the
// sheep from.
func HerdLoop(w HerdWorld, g HerdGrid, pen, gate HerdBox, last HerdPos) HerdResult {
	stuck, pinned, strayed := 0, 0, 0
	for push := 0; push < HerdPushBudget; push++ {
		if w.Stop() {
			return HerdResult{Pushes: push, Why: "out of time"}
		}
		copies := w.Copies()
		sheep, ok := HerdNearest(copies, last)
		if !ok {
			sheep, ok = HerdPick(g, gate, copies)
		}
		if !ok {
			return HerdResult{Pushes: push, Why: "the sheep was lost mid-herd"}
		}
		last = sheep
		if pen.Contains(sheep.X, sheep.Z) {
			w.Logf("the sheep is in the enclosure after %d push(es)", push)
			return HerdResult{Penned: true, Pushes: push}
		}
		me, meOK := w.Me()
		if push > 0 && push%HerdProgressEvery == 0 {
			w.Logf("the sheep is at (%d,%d) after %d push(es), the herder at (%d,%d)", sheep.X, sheep.Z, push, me.X, me.Z)
		}
		var dirs []HerdDir
		if gate.Contains(sheep.X, sheep.Z) {
			dirs = HerdAnyStand(g, sheep.X, sheep.Z, me.X, me.Z)
		} else {
			dirs = HerdDirections(g, sheep.X, sheep.Z, gate, nil)
		}
		// A sheep on a tile with no standable side is pinned: wait it out and keep the pushes.
		if len(dirs) == 0 {
			pinned++
			if pinned > HerdPinnedLimit {
				return HerdResult{Pushes: push, Why: fmt.Sprintf("the sheep stayed pinned at (%d,%d)", sheep.X, sheep.Z)}
			}
			w.Wait(HerdPinnedWait)
			continue
		}
		pinned = 0
		if stuck >= HerdStuckLimit {
			return HerdResult{Pushes: push, Why: fmt.Sprintf("the sheep refused %d pushes near (%d,%d)", HerdStuckLimit, sheep.X, sheep.Z)}
		}
		// A refused push falls through to the next-best direction.
		dir := dirs[min(stuck, len(dirs)-1)]
		ready := strayed < HerdStrayLimit && meOK && HerdBehind(me, sheep, dir)
		if !ready && !w.WalkTo(sheep.X-dir.DX, sheep.Z-dir.DZ) {
			stuck++
			continue
		}
		// The sheep may have wandered while the walk landed, so the stand behind it is the wrong tile now.
		now, still := HerdNearest(w.Copies(), sheep)
		if !still || now != sheep {
			w.Wait(1)
			continue
		}
		res := w.Prod(sheep)
		if !res.Sent || !res.Moved {
			strayed++
			stuck++
			continue
		}
		// An off-axis landing is usually the sheep's own wander step, so the count is a streak: six in a row means the server is
		// picking its own approach side, and one good push clears it.
		wanted := res.Seen && res.After.X == sheep.X+dir.DX && res.After.Z == sheep.Z+dir.DZ
		if wanted {
			strayed = 0
		} else {
			strayed++
			if strayed == HerdStrayLimit {
				w.Logf("the sheep kept landing off the push axis: walking the stand until one lands")
			}
		}
		stuck = 0
	}
	return HerdResult{Pushes: HerdPushBudget, Why: fmt.Sprintf("the sheep was not penned within %d pushes", HerdPushBudget)}
}

// HerdAttempts is how many copies a leg tries: a sheep that wandered into a trap, was lost or refused its pushes costs the attempt, not
// the leg, and the next Find picks a copy that has a route.
const HerdAttempts = 4

// herdRetryable says a failed loop is worth another copy.
func herdRetryable(r HerdResult) bool {
	return !r.Penned && r.Why != "" && r.Why != "out of time" && !strings.HasPrefix(r.Why, "the sheep was not penned within")
}

// HerdLeg herds one sheep into the enclosure: find a copy, go to it, push it over the fence, and when that copy is lost, pinned in a
// trap or refuses its pushes try another, up to HerdAttempts, until the world says stop. Pushes counts every attempt's.
func HerdLeg(w HerdWorld, g HerdGrid, pen, gate HerdBox) HerdResult {
	total := HerdResult{}
	for attempt := 1; attempt <= HerdAttempts; attempt++ {
		first, ok := w.Find()
		if !ok {
			total.Why = "no sheep with a push route to the fence could be found"
			return total
		}
		if !w.Approach(first) {
			total.Why = fmt.Sprintf("the sheep at (%d,%d) was not reached", first.X, first.Z)
			return total
		}
		res := HerdLoop(w, g, pen, gate, first)
		total.Pushes += res.Pushes
		if res.Penned {
			total.Penned, total.Why = true, ""
			return total
		}
		total.Why = res.Why
		if !herdRetryable(res) || w.Stop() {
			return total
		}
		w.Logf("attempt %d of %d failed (%s): trying another copy", attempt, HerdAttempts, res.Why)
	}
	return total
}
