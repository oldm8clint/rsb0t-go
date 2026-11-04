package webwalk

import "testing"

func TestLadderEscalatesInTheTSOrder(t *testing.T) {
	s := initialLadder(50)
	var got []rung
	last := OutcomeNone
	for i := 0; i < 20; i++ {
		r, next, _ := advance(s, ladderObs{curDist: 50, lastOutcome: last})
		s = next
		got = append(got, r)
		switch r {
		case rungBackoff:
			last = OutcomeNone
		case rungVerify:
			last = outcomeProbeFresh
			if len(got) > 12 {
				last = outcomeProbeDead
			}
		case rungUnreachable:
			i = 99
		default:
			last = OutcomeFailed
		}
	}
	want := []rung{
		rungBaked, rungScene, rungUnstick, rungBackoff,
		rungBaked, rungScene, rungUnstick, rungBackoff,
		rungBaked, rungScene, rungUnstick, rungVerify, rungBackoff,
		rungBaked, rungScene, rungUnstick, rungBackoff,
	}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("rungs = %v\nwant    %v", got, want)
		}
	}
}

func TestLadderProgressResetsAndBudgetRetriesOnce(t *testing.T) {
	s := initialLadder(50)
	r, s, _ := advance(s, ladderObs{curDist: 50, lastOutcome: OutcomeBudget})
	if r != rungBakedBigBudget {
		t.Fatalf("budget: got %v", r)
	}
	r, s, _ = advance(s, ladderObs{curDist: 50, lastOutcome: OutcomeBudget})
	if r != rungScene {
		t.Fatalf("second budget: got %v", r)
	}
	r, s, _ = advance(s, ladderObs{curDist: 40, lastOutcome: OutcomeFailed})
	if r != rungBaked || s.bestDist != 40 || s.phase != phaseBaked || s.triedBigBudget {
		t.Fatalf("progress: got %v %+v", r, s)
	}
	if r, _, _ := advance(s, ladderObs{curDist: 40, withinRadius: true}); r != rungArrived {
		t.Fatalf("arrived: got %v", r)
	}
	if r, _, _ := advance(s, ladderObs{curDist: 40, lastOutcome: OutcomeInterrupted}); r != rungInterrupted {
		t.Fatalf("interrupted: got %v", r)
	}
	if r, _, _ := advance(ladderState{bestDist: 40, phase: phaseVerify}, ladderObs{curDist: 40, lastOutcome: outcomeProbeDead}); r != rungUnreachable {
		t.Fatalf("dead probe: got %v", r)
	}
}

func TestBackoffTicks(t *testing.T) {
	for passes, want := range map[int]int{0: 2, 1: 2, 2: 4, 3: 6, 8: 16, 50: 16} {
		if got := backoffTicks(passes); got != want {
			t.Errorf("backoffTicks(%d) = %d, want %d", passes, got, want)
		}
	}
}

func TestPickUnstickStepAndJudgeProbe(t *testing.T) {
	dx, dz, ok := pickUnstickStep(func(dx, dz int) bool { return dx == -1 && dz == 0 }, 3)
	if !ok || dx != -1 || dz != 0 {
		t.Errorf("pickUnstickStep = %d,%d,%v", dx, dz, ok)
	}
	if _, _, ok := pickUnstickStep(func(int, int) bool { return false }, 0); ok {
		t.Error("no step should be found")
	}
	a := Point{X: 1, Z: 2}
	if judgeProbe(nil, true, a) != outcomeProbeFresh || judgeProbe(&a, true, a) != outcomeProbeDead || judgeProbe(nil, false, a) != outcomeProbeDead {
		t.Error("judgeProbe")
	}
}
