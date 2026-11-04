package freshstartrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
)

// What these bound, and the live reading that motivated them.
//
// PickpocketStep clicks, then waits for the attempt to resolve. The wait used to end on
// only two conditions: Thieving xp going up, or t.stunned(). A FAILED attempt satisfies
// neither — it gains no xp, and the stun is recorded by the NoteChat call that runs AFTER
// the wait returns — so a miss could not be observed at all and always ran the full
// 2500ms before logging "timed out waiting for the pickpocket attempt to resolve".
//
// That line therefore never meant a fault. It is the ordinary failure path, which is why
// it shows up in its hundreds (183 in 17 minutes at Thieving 3, see
// pickpocket_progress_test.go) and why it was read as a stall when it was not one.
// Measured again on the live fleet 2026-09-22, on the first ScarabSwarm run ever made
// against production: Acct226 at Thieving 1 logged 13 of them against 20 landed pockets
// inside 100 seconds — at a low Thieving level most attempts miss, so most attempts were
// paying the whole timeout for an answer the server had already given.
//
// The fix reads the chat inside the wait. These cases pin the two halves of that: the
// predicate recognises what the server actually says, and the wait ends on it promptly
// rather than at the deadline.

func TestPickpocketFailedRecognisesTheServersOwnLines(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		// The exact pair confirmed live and quoted in PickpocketStep's own comment.
		{"stunned", "You've been stunned!", true},
		{"fail to pick", "You fail to pick the man's pocket.", true},
		{"the whole live sequence", "You attempt to pick the man's pocket.\nYou fail to pick the man's pocket.\nYou've been stunned!", true},
		// Case folding matters: Since joins raw server lines, not normalised ones.
		{"shouted", "YOU'VE BEEN STUNNED!", true},
		// A success must NOT read as a failure, or the stun timer would fire on every
		// landed pocket and the account would stand still waiting out a stun it never got.
		{"success", "You pick the man's pocket.", false},
		{"unrelated", "You need a higher Thieving level to do that.", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickpocketFailed(tc.text); got != tc.want {
				t.Errorf("pickpocketFailed(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// NoteChat is the other caller of the extracted predicate. This is a regression guard on
// the extraction itself: the stun must still be timed from exactly the same lines.
func TestNoteChatStillTimesTheStunFromTheSameLines(t *testing.T) {
	th := NewThief()
	if th.stunned() {
		t.Fatal("a fresh Thief must not start stunned")
	}
	th.NoteChat("You pick the man's pocket.")
	if th.stunned() {
		t.Error("a SUCCESSFUL pickpocket must not set the stun timer")
	}
	th.NoteChat("You fail to pick the man's pocket.")
	if !th.stunned() {
		t.Error("a failure line must set the stun timer")
	}
}

// The behaviour that actually costs time: the wait must end when the server's answer
// arrives, not at the deadline. Exercises waitFor, the predicate and chatlog.Since
// together, in the same composition PickpocketStep uses.
func TestResolveWaitEndsOnTheFailureLineNotTheDeadline(t *testing.T) {
	const timeout = 2500 * time.Millisecond

	log := chatlog.New()
	d := &Deps{Chat: log, Delay: func(dur time.Duration) bool { time.Sleep(dur); return true }}

	clickAt := time.Now()
	// The server answers partway through the window, as it does live.
	go func() {
		time.Sleep(300 * time.Millisecond)
		log.Record("You fail to pick the man's pocket.")
		log.Record("You've been stunned!")
	}()

	start := time.Now()
	ok := waitFor(d, "the pickpocket attempt to resolve", timeout, func() bool {
		return d.Chat != nil && pickpocketFailed(d.Chat.Since(clickAt))
	})
	elapsed := time.Since(start)

	if !ok {
		t.Fatalf("the wait timed out after %v — a failure line inside the window must end it", elapsed)
	}
	// Generous bound: the point is that it did not run to 2500ms, not that it is instant.
	if elapsed > timeout/2 {
		t.Errorf("the wait took %v of a %v timeout — it should end when the line lands (~300ms), not at the deadline", elapsed, timeout)
	}
}

// The complement, so the timeout is not simply defeated: with the server saying nothing,
// the wait still runs its full course and still reports a genuine timeout. This is what
// makes the "timed out" line meaningful again — it now means 2500ms of silence.
func TestResolveWaitStillTimesOutOnRealSilence(t *testing.T) {
	const timeout = 300 * time.Millisecond

	d := &Deps{Chat: chatlog.New(), Delay: func(dur time.Duration) bool { time.Sleep(dur); return true }}
	clickAt := time.Now()

	start := time.Now()
	ok := waitFor(d, "the pickpocket attempt to resolve", timeout, func() bool {
		return d.Chat != nil && pickpocketFailed(d.Chat.Since(clickAt))
	})
	elapsed := time.Since(start)

	if ok {
		t.Error("the wait reported success with nothing in the chat at all")
	}
	if elapsed < timeout {
		t.Errorf("the wait returned after %v, before its own %v deadline", elapsed, timeout)
	}
}

// A nil Chat must not panic. Deps is built by several cmd/ harnesses and not all of them
// wire a chat log, and the predicate runs on every poll of every attempt.
func TestResolveWaitToleratesNoChatLog(t *testing.T) {
	d := &Deps{Delay: func(dur time.Duration) bool { time.Sleep(dur); return true }}
	clickAt := time.Now()
	ok := waitFor(d, "the pickpocket attempt to resolve", 150*time.Millisecond, func() bool {
		return d.Chat != nil && pickpocketFailed(d.Chat.Since(clickAt))
	})
	if ok {
		t.Error("want a plain timeout with no chat log, got success")
	}
}
