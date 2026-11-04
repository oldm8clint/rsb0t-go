package webwalk

// This file is walkLadder.ts: the escalation ladder Traversal.walkResilient climbs when a walk
// makes no progress — baked walk, same-scene walk, nearby door and unstick step, backoff, and
// a reachability check before calling the destination unreachable. It is pure; Walker runs it.

// Outcome is how a walk, or one rung of the ladder, ended.
type Outcome string

// Outcomes. The probe ones only come from the verify rung.
const (
	OutcomeNone        Outcome = ""
	OutcomeArrived     Outcome = "arrived"
	OutcomeClosest     Outcome = "closest"
	OutcomeBudget      Outcome = "budget"
	OutcomeFailed      Outcome = "failed"
	OutcomeInterrupted Outcome = "interrupted"
	OutcomeUnreachable Outcome = "unreachable"
	OutcomeStopped     Outcome = "stopped"
	outcomeProbeFresh  Outcome = "probe-fresh"
	outcomeProbeDead   Outcome = "probe-dead"
)

type rung int

const (
	rungBaked rung = iota
	rungBakedBigBudget
	rungScene
	rungUnstick
	rungBackoff
	rungVerify
	rungUnreachable
	rungArrived
	rungInterrupted
)

func (r rung) String() string {
	return [...]string{"baked", "baked (big budget)", "scene", "unstick", "backoff", "verify", "unreachable", "arrived", "interrupted"}[r]
}

type phase int

const (
	phaseBaked phase = iota
	phaseScene
	phaseUnstick
	phaseVerify
)

// ladderState is walkLadder.ts's LadderState.
type ladderState struct {
	bestDist         int
	noProgressPasses int
	phase            phase
	triedBigBudget   bool
}

// ladderObs is walkLadder.ts's LadderObs.
type ladderObs struct {
	curDist      int
	withinRadius bool
	interrupted  bool
	lastOutcome  Outcome
}

const (
	backoffMinTicks = 2
	backoffMaxTicks = 16
	// unreachablePasses is UNREACHABLE_PASSES: no-progress passes before the verify rung.
	unreachablePasses = 3
)

func initialLadder(curDist int) ladderState {
	return ladderState{bestDist: curDist, phase: phaseBaked}
}

// backoffTicks is min(16, 2 + 2*max(0, passes-1)).
func backoffTicks(passes int) int {
	extra := passes - 1
	if extra < 0 {
		extra = 0
	}
	t := backoffMinTicks + 2*extra
	if t > backoffMaxTicks {
		return backoffMaxTicks
	}
	return t
}

// advance is walkLadder.ts's advance: the next rung and state. The backoff rung's ticks come
// from backoffTicks(state.noProgressPasses) (1 after a verify).
func advance(s ladderState, obs ladderObs) (rung, ladderState, int) {
	if obs.interrupted || obs.lastOutcome == OutcomeInterrupted {
		return rungInterrupted, s, 0
	}
	if obs.withinRadius {
		return rungArrived, s, 0
	}
	best := s.bestDist
	if obs.curDist < best {
		return rungBaked, ladderState{bestDist: obs.curDist, phase: phaseBaked}, 0
	}
	if obs.lastOutcome == OutcomeNone {
		return rungBaked, ladderState{bestDist: best, noProgressPasses: s.noProgressPasses, phase: phaseBaked}, 0
	}
	switch s.phase {
	case phaseVerify:
		if obs.lastOutcome == outcomeProbeDead {
			return rungUnreachable, s, 0
		}
		return rungBackoff, ladderState{bestDist: best, phase: phaseBaked}, backoffTicks(1)
	case phaseBaked:
		if obs.lastOutcome == OutcomeBudget && !s.triedBigBudget {
			n := s
			n.bestDist, n.triedBigBudget = best, true
			return rungBakedBigBudget, n, 0
		}
		n := s
		n.bestDist, n.phase = best, phaseScene
		return rungScene, n, 0
	case phaseScene:
		n := s
		n.bestDist, n.phase = best, phaseUnstick
		return rungUnstick, n, 0
	}
	passes := s.noProgressPasses + 1
	if passes >= unreachablePasses {
		n := s
		n.bestDist, n.noProgressPasses, n.phase = best, passes, phaseVerify
		return rungVerify, n, 0
	}
	return rungBackoff, ladderState{bestDist: best, noProgressPasses: passes, phase: phaseBaked}, backoffTicks(passes)
}

// unstickDirs is walkLadder.ts's DIRS order: N, NE, E, SE, S, SW, W, NW.
var unstickDirs = [8][2]int{{0, 1}, {1, 1}, {1, 0}, {1, -1}, {0, -1}, {-1, -1}, {-1, 0}, {-1, 1}}

// pickUnstickStep returns the first steppable direction starting at startDir.
func pickUnstickStep(canStep func(dx, dz int) bool, startDir int) (dx, dz int, ok bool) {
	for i := 0; i < len(unstickDirs); i++ {
		d := unstickDirs[(startDir+i)%len(unstickDirs)]
		if canStep(d[0], d[1]) {
			return d[0], d[1], true
		}
	}
	return 0, 0, false
}

// judgeProbe is walkLadder.ts's judgeProbe: a failed probe, or one ending where the previous
// probe did, is dead.
func judgeProbe(prev *Point, ok bool, terminal Point) Outcome {
	if !ok {
		return outcomeProbeDead
	}
	if prev != nil && *prev == terminal {
		return outcomeProbeDead
	}
	return outcomeProbeFresh
}
