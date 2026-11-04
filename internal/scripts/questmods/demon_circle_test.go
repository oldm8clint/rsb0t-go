package questmods

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
)

// ---- the circle's lock-out watch ----

// The circle is single-way combat: while a dark wizard's spell has landed in the last eight ticks the server refuses the attack on
// Delrith, and the visit leaves at the second refusal rather than send the attack again and again.
func TestTheCircleWatchLeavesAtTheSecondRefusal(t *testing.T) {
	w := newExecWorld()
	chat := chatlog.New()
	w.e.D.Chat = chat
	watch := &demonCircleWatch{d: w.e.D}
	settle := func() { time.Sleep(10 * time.Millisecond) }

	// An attack that took: no refusal, nothing counted.
	first := time.Now()
	if watch.refusedSince(first) || watch.note(first) || watch.refused != 0 {
		t.Fatalf("a quiet chat counted: %d", watch.refused)
	}
	// Other refusals and ordinary lines are not this one.
	chat.Record("Someone else is fighting that.")
	chat.Record("You hit Delrith.")
	if watch.refusedSince(first) || watch.note(first) || watch.refused != 0 {
		t.Fatalf("another line counted: %d", watch.refused)
	}
	// The first refusal is counted and the visit stays.
	chat.Record("I'm already under attack!")
	if !watch.refusedSince(first) || watch.note(first) || watch.refused != 1 {
		t.Fatalf("the first refusal: %d, want 1 and staying", watch.refused)
	}
	// The next attack is judged on the chat said after it went out: the old refusal is not counted twice.
	settle()
	second := time.Now()
	if watch.refusedSince(second) || watch.note(second) || watch.refused != 1 {
		t.Fatalf("the old refusal counted again: %d", watch.refused)
	}
	// The second refusal ends the visit, once per attack however many lines come with it.
	chat.Record("I'm already under attack!")
	chat.Record("I'm already under attack!")
	if !watch.note(second) || watch.refused != 2 {
		t.Fatalf("the second refusal: %d, want 2 and leaving", watch.refused)
	}
	if joined := strings.Join(*w.log, "\n"); strings.Count(joined, "refused the attack on Delrith") != 2 || !strings.Contains(joined, "2 of 2") {
		t.Errorf("the log does not say what happened: %v", *w.log)
	}
}

// With no chat log the watch sees nothing and does not fail: the other guards (the budget, the retreat) still end the visit.
func TestTheCircleWatchWithoutAChatLogSeesNothing(t *testing.T) {
	w := newExecWorld()
	w.e.D.Chat = nil
	watch := &demonCircleWatch{d: w.e.D}
	if watch.refusedSince(time.Now()) || watch.note(time.Now()) || watch.refused != 0 {
		t.Error("a watch with no chat log counted a refusal")
	}
}
