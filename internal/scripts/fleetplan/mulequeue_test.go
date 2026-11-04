package fleetplan

import (
	"testing"
	"time"
)

// Live 26 Sep, 16:30-17:24: 169 asks declined with ONE entry ahead at 1m35s against a 1m30s ask.
// The kit order those asks would have placed waits five minutes, so the gate must let them queue.
func TestOneEntryAheadIsNotTooLongForAKitOrder(t *testing.T) {
	budget := MuleQueueBudget(90*time.Second, 5*time.Minute, ShopFallbackCost)
	if budget != 5*time.Minute {
		t.Fatalf("budget = %s, want the kit order's 5m", budget)
	}
	for q := 1; q <= 3; q++ {
		if wait, long := MuleQueueWait(q, 95*time.Second, budget); long {
			t.Errorf("queue %d at 1m35s (%s) declined against a 5m kit wait", q, wait)
		}
	}
	if _, long := MuleQueueWait(4, 95*time.Second, budget); !long {
		t.Error("queue 4 at 1m35s (6m20s) should still be too long for a 5m wait")
	}
}

// The old comparison, kept as a regression marker: against the bare 90 s the same queue of one was
// refused, which is the bug.
func TestTheBareAskTimeoutRefusedAQueueOfOne(t *testing.T) {
	if _, long := MuleQueueWait(1, 95*time.Second, 90*time.Second); !long {
		t.Fatal("expected the bare 90s budget to refuse one entry at 1m35s")
	}
}

// Per-item orders (muleKitOrder=false) wait only the ask's own timeout, so their budget is that.
func TestAPerItemOrderKeepsItsOwnTimeout(t *testing.T) {
	if got := MuleQueueBudget(90*time.Second, 90*time.Second, ShopFallbackCost); got != 90*time.Second {
		t.Fatalf("budget = %s, want 90s", got)
	}
}

// A fallback that is quicker than the order's wait caps the budget, never below the ask timeout.
func TestAQuickerFallbackCapsTheBudget(t *testing.T) {
	if got := MuleQueueBudget(90*time.Second, 10*time.Minute, 4*time.Minute); got != 4*time.Minute {
		t.Errorf("budget = %s, want the 4m fallback", got)
	}
	if got := MuleQueueBudget(3*time.Minute, 10*time.Minute, time.Minute); got != 3*time.Minute {
		t.Errorf("budget = %s, want the 3m ask timeout as the floor", got)
	}
	if got := MuleQueueBudget(90*time.Second, 5*time.Minute, 0); got != 5*time.Minute {
		t.Errorf("no fallback: budget = %s, want 5m", got)
	}
}

// Nothing published is "unknown", never "too long".
func TestAnUnpublishedQueueIsNeverTooLong(t *testing.T) {
	for _, c := range []struct {
		q     int
		cycle time.Duration
	}{{0, time.Minute}, {3, 0}, {-1, time.Minute}} {
		if _, long := MuleQueueWait(c.q, c.cycle, time.Second); long {
			t.Errorf("queue %d cycle %s called too long", c.q, c.cycle)
		}
	}
}
