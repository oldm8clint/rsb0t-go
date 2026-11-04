package wcfletchplan

import (
	"strings"
	"testing"
	"time"
)

// A poll in the middle of a walk with logs to cut, nothing in flight and the walk far from its next click.
func idleWalkPoll() WalkFletchPoll {
	return WalkFletchPoll{
		HaveKnife: true, LogsToCut: 20, CloseSettled: true, Ahead: 30,
		SinceFall: time.Hour, SinceUse: time.Hour, SinceAnswer: time.Hour,
	}
}

func TestWalkFletchStartsAUseWhenIdle(t *testing.T) {
	if got := DecideWalkFletch(idleWalkPoll()); got != WalkUse {
		t.Fatalf("idle poll with logs: %v, want WalkUse", got)
	}
}

func TestWalkFletchWaitsForWhatIsInFlight(t *testing.T) {
	for name, edit := range map[string]func(p *WalkFletchPoll){
		"a batch running":      func(p *WalkFletchPoll) { p.SinceFall = time.Second },
		"a use on its way":     func(p *WalkFletchPoll) { p.SinceUse = time.Second },
		"an answer on its way": func(p *WalkFletchPoll) { p.SinceAnswer = time.Second },
		"a close not settled":  func(p *WalkFletchPoll) { p.CloseSettled = false },
		"another modal":        func(p *WalkFletchPoll) { p.OtherModal = true },
		"the next click due":   func(p *WalkFletchPoll) { p.Ahead = WalkFletchStartAhead - 1 },
		"no knife":             func(p *WalkFletchPoll) { p.HaveKnife = false },
		"only the carrier log": func(p *WalkFletchPoll) { p.LogsToCut = 0 },
		"a fight":              func(p *WalkFletchPoll) { p.InCombat = true },
	} {
		p := idleWalkPoll()
		edit(&p)
		if got := DecideWalkFletch(p); got != WalkNone {
			t.Errorf("%s: %v, want WalkNone", name, got)
		}
	}
}

func TestWalkFletchAnswersItsMenuOnce(t *testing.T) {
	p := idleWalkPoll()
	p.MenuOffers = true
	if got := DecideWalkFletch(p); got != WalkAnswer {
		t.Fatalf("menu open: %v, want WalkAnswer", got)
	}
	// Even with the click due and a close unsettled: an open menu is answered or it holds the cutting.
	p.Ahead, p.CloseSettled = 0, false
	if got := DecideWalkFletch(p); got != WalkAnswer {
		t.Fatalf("menu open, click due: %v, want WalkAnswer", got)
	}
	p.SinceAnswer = 100 * time.Millisecond
	if got := DecideWalkFletch(p); got != WalkNone {
		t.Fatalf("menu still shown right after the answer: %v, want WalkNone", got)
	}
}

func TestWalkFletchWorth(t *testing.T) {
	if WalkFletchWorth(WalkFletchMinDistance-1, true) || !WalkFletchWorth(WalkFletchMinDistance, true) || WalkFletchWorth(80, false) {
		t.Fatal("WalkFletchWorth mis-judged the distance or the switch")
	}
}

func TestWalkFletchGuardSwitchesOffAfterEmptyWalks(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	var g WalkFletchGuard
	for i := 0; i < WalkFletchFailLimit-1; i++ {
		if why := g.Note(0, now); why != "" {
			t.Fatalf("switched off after %d empty walks", i+1)
		}
	}
	if why := g.Note(0, now); why == "" {
		t.Fatal("not switched off after the limit")
	}
	if g.Allowed(now.Add(time.Minute)) {
		t.Fatal("allowed while off")
	}
	if !g.Allowed(now.Add(WalkFletchOffFor)) {
		t.Fatal("not allowed again after the stop")
	}
	// A walk that cut something clears the count.
	var g2 WalkFletchGuard
	g2.Note(0, now)
	g2.Note(0, now)
	g2.Note(5, now)
	if why := g2.Note(0, now); why != "" {
		t.Fatalf("a good walk did not clear the count: %s", why)
	}
}

func TestWalkFletchLineSaysWhatTheLeadGreps(t *testing.T) {
	line := WalkFletchLine(14, 31, 40, 100)
	for _, want := range []string{"wcfletch manip: walk-fletch: 14 logs in 31 walking ticks (45.2 per 100)", "vs plain 0.0 per 100", "= 28 ticks saved", "this run 40 logs in 100 ticks (40.0 per 100)"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q lacks %q", line, want)
		}
	}
}
