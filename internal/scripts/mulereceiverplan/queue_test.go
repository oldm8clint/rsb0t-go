package mulereceiverplan

import (
	"testing"
	"time"
)

func TestQueuePositionFollowsAnnouncementOrder(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	q := map[string]time.Time{
		"acct139": t0,
		"acct144": t0.Add(10 * time.Second),
		"acct171": t0.Add(20 * time.Second),
		"acct153": t0.Add(10 * time.Second), // same instant as acct144: name breaks the tie
	}
	for key, want := range map[string]int{"acct139": 1, "acct144": 2, "acct153": 3, "acct171": 4} {
		if got := QueuePosition(q, key); got != want {
			t.Errorf("QueuePosition(%s) = %d, want %d", key, got, want)
		}
	}
	if got := QueuePosition(q, "latecomer"); got != 5 {
		t.Errorf("an unqueued donor goes to the back: got %d, want 5", got)
	}
	order := QueueOrder(q)
	for i, want := range []string{"acct139", "acct144", "acct153", "acct171"} {
		if order[i] != want {
			t.Fatalf("QueueOrder = %v", order)
		}
	}
}

// The quote is a turn time, not len(queue) x cycle for everybody: the live code quoted the
// donor at the head of a 12-deep queue twelve cycles, the same as the donor at the back.
func TestQueueETAIsPositionTimesCycle(t *testing.T) {
	if got := QueueETA(1, 15*time.Second); got != 15*time.Second {
		t.Errorf("head of the queue: %s", got)
	}
	if got := QueueETA(12, 15*time.Second); got != 3*time.Minute {
		t.Errorf("twelfth: %s", got)
	}
	if got := QueueETA(0, 15*time.Second); got != 15*time.Second {
		t.Errorf("a position below 1 is one cycle: %s", got)
	}
}
