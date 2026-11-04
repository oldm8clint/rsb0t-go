package thieverrunner

import (
	"strings"
	"testing"
	"time"
)

type fakeSession struct{ n int }

func withClock(t *testing.T) *time.Time {
	t.Helper()
	now := time.Date(2026, 10, 1, 21, 0, 0, 0, time.UTC)
	oldClock := restockClock
	restockClock = func() time.Time { return now }
	restockMu.Lock()
	oldSays := restockSays
	restockSays = map[any]*restockSay{}
	restockMu.Unlock()
	t.Cleanup(func() {
		restockClock = oldClock
		restockMu.Lock()
		restockSays = oldSays
		restockMu.Unlock()
	})
	return &now
}

// LIVE 30 Sep - 2 Oct: "waiting for the baker's stall to restock" was 17.5 percent of every
// ScarabSwarm log line. One account polls it every second or two; it may say so once per half minute.
func TestTheRestockLineIsSaidOncePerHalfMinutePerAccount(t *testing.T) {
	now := withClock(t)
	a, b := &fakeSession{1}, &fakeSession{2}
	if got := restockLine(a, "baker's stall"); got != "waiting for the baker's stall to restock" {
		t.Fatalf("first line = %q", got)
	}
	for i := 0; i < 19; i++ { // nineteen polls, 1.5 s apart: 28.5 s in
		*now = now.Add(1500 * time.Millisecond)
		if got := restockLine(a, "baker's stall"); got != "" {
			t.Fatalf("poll %d inside the half minute said %q", i, got)
		}
	}
	*now = now.Add(1500 * time.Millisecond) // the 30 s mark
	if got := restockLine(a, "baker's stall"); got == "" {
		t.Fatal("the line never came back after half a minute")
	}
	// Another account has its own clock.
	if got := restockLine(b, "baker's stall"); got == "" {
		t.Fatal("a second account was silenced by the first")
	}
}

// After the half minute the line comes back, saying how many checks it stands for, so a long wait is
// still visible as a long wait.
func TestTheRestockLineCountsTheChecksItSkipped(t *testing.T) {
	now := withClock(t)
	a := &fakeSession{1}
	restockLine(a, "baker's stall")
	for i := 0; i < 9; i++ {
		*now = now.Add(2 * time.Second)
		if restockLine(a, "baker's stall") != "" {
			t.Fatalf("poll %d inside the window said something", i)
		}
	}
	*now = now.Add(15 * time.Second) // 33 s since the first
	got := restockLine(a, "baker's stall")
	if !strings.HasPrefix(got, "waiting for the baker's stall to restock (") || !strings.Contains(got, "10 checks in 33s") {
		t.Fatalf("second line = %q, want the count of the checks it stands for", got)
	}
}

// A session that never comes back must not be held in memory for ever: the next new account's first
// line sweeps entries unused for restockForget.
func TestAGoneSessionIsForgotten(t *testing.T) {
	now := withClock(t)
	old := &fakeSession{1}
	restockLine(old, "baker's stall")
	*now = now.Add(restockForget + time.Second)
	restockLine(&fakeSession{2}, "baker's stall")
	restockMu.Lock()
	_, kept := restockSays[old]
	restockMu.Unlock()
	if kept {
		t.Fatal("a session unused for the forget window is still held")
	}
}

// A key that cannot be hashed (or no key at all) bypasses the limit rather than panicking.
func TestAnUnhashableKeyJustSaysTheLine(t *testing.T) {
	withClock(t)
	type slicey struct{ s []int }
	for _, key := range []any{nil, slicey{}} {
		if got := restockLine(key, "baker's stall"); got != "waiting for the baker's stall to restock" {
			t.Fatalf("key %T: %q", key, got)
		}
	}
}
