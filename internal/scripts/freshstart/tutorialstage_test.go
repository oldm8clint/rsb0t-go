package freshstart

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

func TestDecideTutorialStage(t *testing.T) {
	underground := tile.Tile{X: 3095, Z: 9500}
	cases := []struct {
		name      string
		mainModal int
		at        *tile.Tile
		want      TutorialStage
	}{
		{"the appearance screen wins even with a position", DesignModal, at(island), TutorialStageDesign},
		{"the appearance screen with no position yet", DesignModal, nil, TutorialStageDesign},
		{"no position and no modal", 0, nil, TutorialStageUnknown},
		{"on the island", 0, at(island), TutorialStageIsland},
		{"in the tutorial's underground", 0, at(underground), TutorialStageIsland},
		{"lumbridge is off the island", 0, at(lumbridge), TutorialStageOff},
	}
	for _, c := range cases {
		if got := DecideTutorialStage(c.mainModal, c.at); got != c.want {
			t.Errorf("%s: DecideTutorialStage = %q, want %q", c.name, got, c.want)
		}
	}
}

// The completion test has two halves and BOTH are required. An account that has been teleported
// off the island but whose sidebar has not come back yet is still blind, and a caller that
// resumes there starts reading a pack it cannot see. See tutorialstage.go's package comment.
func TestTutorialWatchNeedsPositionAndSidebarTogether(t *testing.T) {
	now := time.Now()
	var w TutorialWatch

	if done, _ := w.Note(TutorialProgress{Stage: TutorialStageOff, Action: TutorialWaitTabs, Readable: false}, now); done {
		t.Fatal("off the island but still blind must NOT read as done")
	}
	if done, _ := w.Note(TutorialProgress{Stage: TutorialStageIsland, Action: TutorialTalk, Readable: true}, now); done {
		t.Fatal("readable but still on the island must NOT read as done")
	}
	done, giveUp := w.Note(TutorialProgress{Stage: TutorialStageOff, Action: TutorialWaitTabs, Readable: true}, now)
	if !done || giveUp != "" {
		t.Fatalf("off the island with the sidebar back must be done, got done=%v giveUp=%q", done, giveUp)
	}
}

// An account that was never on the island at all — every account already in the fleet — must be
// done on its very first pass, with no waiting and no budget consumed.
func TestTutorialWatchIsDoneImmediatelyForAFinishedAccount(t *testing.T) {
	var w TutorialWatch
	done, giveUp := w.Note(TutorialProgress{Stage: TutorialStageOff, Action: TutorialWaitTabs, Readable: true}, time.Now())
	if !done || giveUp != "" {
		t.Fatalf("a finished account must be done on the first pass, got done=%v giveUp=%q", done, giveUp)
	}
}

// The whole point of the watch: a tutorial that cannot be finished must END the run, and the
// reason must name what it was waiting for rather than being a bare timeout.
func TestTutorialWatchGivesUpOnTheOverallLimit(t *testing.T) {
	start := time.Now()
	var w TutorialWatch
	// Keep the situation CHANGING every pass, so the stuck limit never fires and only the
	// overall limit can end this. Without the overall limit a dialogue that alternates between
	// two pages forever would spin for the life of the process.
	for i := 0; i < 40; i++ {
		action := TutorialTalk
		if i%2 == 0 {
			action = TutorialContinue
		}
		at := start.Add(time.Duration(i) * 15 * time.Second)
		done, giveUp := w.Note(TutorialProgress{Stage: TutorialStageIsland, Action: action}, at)
		if done {
			t.Fatal("never done: the account never left the island")
		}
		if at.Sub(start) < TutorialLimit && giveUp != "" {
			t.Fatalf("gave up after %s, before the %s budget: %q", at.Sub(start), TutorialLimit, giveUp)
		}
		if at.Sub(start) >= TutorialLimit {
			if giveUp == "" {
				t.Fatalf("still had not given up %s in, past the %s budget", at.Sub(start), TutorialLimit)
			}
			if !strings.Contains(giveUp, string(TutorialStageIsland)) || !strings.Contains(giveUp, string(action)) {
				t.Fatalf("the give-up must name the stage and the action, got %q", giveUp)
			}
			return
		}
	}
	t.Fatal("the overall limit never fired")
}

// The common real failure is the guide never being in reach. That should be reported in
// TutorialStuckLimit with a precise reason, not in TutorialLimit with a vague one.
func TestTutorialWatchGivesUpOnAnUnchangingSituation(t *testing.T) {
	start := time.Now()
	var w TutorialWatch
	p := TutorialProgress{Stage: TutorialStageIsland, Action: TutorialNoGuide}

	if _, giveUp := w.Note(p, start); giveUp != "" {
		t.Fatalf("the first pass must not give up: %q", giveUp)
	}
	if _, giveUp := w.Note(p, start.Add(TutorialStuckLimit-time.Second)); giveUp != "" {
		t.Fatalf("gave up a second early: %q", giveUp)
	}
	_, giveUp := w.Note(p, start.Add(TutorialStuckLimit))
	if giveUp == "" {
		t.Fatalf("an unchanging situation must give up at %s", TutorialStuckLimit)
	}
	if !strings.Contains(giveUp, string(TutorialNoGuide)) {
		t.Fatalf("the give-up must name what it was waiting for, got %q", giveUp)
	}
	if TutorialStuckLimit >= TutorialLimit {
		t.Fatalf("the stuck limit (%s) must be shorter than the overall one (%s), or it can never fire", TutorialStuckLimit, TutorialLimit)
	}
}

// A situation that keeps changing must not trip the stuck limit — otherwise an ordinary
// multi-page dialogue would be killed mid-tutorial.
func TestTutorialWatchDoesNotGiveUpWhileTheSituationChanges(t *testing.T) {
	start := time.Now()
	var w TutorialWatch
	actions := []TutorialAction{TutorialTalk, TutorialContinue, TutorialChoose, TutorialWalk}
	for i := 0; i < 8; i++ {
		at := start.Add(time.Duration(i) * 30 * time.Second)
		if _, giveUp := w.Note(TutorialProgress{Stage: TutorialStageIsland, Action: actions[i%len(actions)]}, at); giveUp != "" {
			t.Fatalf("gave up %s in while the situation was still changing: %q", at.Sub(start), giveUp)
		}
	}
}

func TestTutorialWatchElapsed(t *testing.T) {
	var w TutorialWatch
	now := time.Now()
	if d := w.Elapsed(now); d != 0 {
		t.Fatalf("Elapsed before the first Note must be 0, got %s", d)
	}
	w.Note(TutorialProgress{Stage: TutorialStageIsland, Action: TutorialTalk}, now)
	if d := w.Elapsed(now.Add(90 * time.Second)); d != 90*time.Second {
		t.Fatalf("Elapsed = %s, want 90s", d)
	}
}
