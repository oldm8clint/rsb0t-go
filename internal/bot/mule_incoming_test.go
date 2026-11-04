package bot

import (
	"testing"
	"time"
)

// An announced donor holds the mule at its post only until incomingTTL: one that never arrives
// must not stop the mule cooking for good.
func TestAnnouncedDonorsExpire(t *testing.T) {
	b := &muleBot{incoming: map[string]time.Time{
		"fresh": time.Now(),
		"stale": time.Now().Add(-incomingTTL - time.Second),
	}}
	if n := b.incomingPending(); n != 1 {
		t.Fatalf("pending = %d, want only the fresh donor", n)
	}
	if _, ok := b.incoming["stale"]; ok {
		t.Fatal("an expired announcement was kept")
	}
}

// Owner 2026-09-19: the mule stood a full minute at the bank between cook rounds. With idle
// cooking on and nobody coming it only glances for a trade; a donor on the way, a queued
// request, or cooking off keeps the full wait.
func TestMuleOnlyGlancesForTradesWhenCookingAndNobodyIsComing(t *testing.T) {
	b := &muleBot{cookIdle: true, idleTimeout: time.Minute, incoming: map[string]time.Time{}}
	if got := b.cycleWait(0); got != cookGlance {
		t.Fatalf("idle cooking, nobody coming: wait %s, want %s", got, cookGlance)
	}
	if got := b.cycleWait(1); got != time.Minute {
		t.Fatalf("a queued trade request must get the full wait, got %s", got)
	}
	b.incoming["acct6"] = time.Now()
	if got := b.cycleWait(0); got != time.Minute {
		t.Fatalf("an announced donor must get the full wait, got %s", got)
	}
	off := &muleBot{cookIdle: false, idleTimeout: time.Minute}
	if got := off.cycleWait(0); got != time.Minute {
		t.Fatalf("cooking off keeps the full wait, got %s", got)
	}
}
