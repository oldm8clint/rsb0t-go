package lootchestfisherplan

import (
	"testing"
	"time"
)

// The owner's rule, 2026-09-22: "spare104 should only request crystal keys if the mule has over
// 100 keys available to hand over to spare104."
//
// Before internal/bot/lootchestfisher.go started seeding this reading from the mule's own
// PUBLISHED bank, the only way to learn the count was to ask for the ceiling and read the
// number out of the decline -- three round trips to learn something the mule already
// publishes:
//
//	23:06:45 asking "mule" for up to 500 Crystal key to find out what it is holding
//	23:06:48 "mule" declined the order: only 68 Crystal key banked, holding until 100
//	23:06:48 "mule" is holding 68 Crystal key, at or below its 100 reserve
//
// These pin the threshold the seeded reading feeds, so the seeding cannot drift from the rule.

func TestOnlyAskWhenTheMuleIsOverTheFloor(t *testing.T) {
	if MuleKeyThreshold != 100 {
		t.Fatalf("MuleKeyThreshold = %d, want 100 -- the owner's number", MuleKeyThreshold)
	}
	// The live reading that must NOT produce an order.
	if got := Available(68); got != 0 {
		t.Errorf("Available(68) = %d, want 0 (68 is under the floor)", got)
	}
	// Exactly at the floor is still nothing to give: "over 100" means over.
	if got := Available(100); got != 0 {
		t.Errorf("Available(100) = %d, want 0 -- 100 is the floor, not over it", got)
	}
	// Over it, and the WHOLE stock is askable. This wanted 50 (150 minus a 100 reserve) until
	// the owner's 2026-09-26 ruling that the floor is a gate: "it should have taken 110 keys".
	if got := Available(150); got != 150 {
		t.Errorf("Available(150) = %d, want 150", got)
	}
}

// A seeded reading only counts while it is fresh, the same as one learned from a decline --
// otherwise a published bank read once at startup would authorise orders for the rest of the
// run. This is what makes the seeding safe to take from a shared directory.
func TestASeededReadingStillHasToBeFresh(t *testing.T) {
	now := time.Now()
	fresh := MuleStock{Banked: 150, At: now, Heard: true}
	if !fresh.Fresh(now) {
		t.Fatal("a reading taken just now is not fresh")
	}
	stale := MuleStock{Banked: 150, At: now.Add(-MuleStockTTL - time.Minute), Heard: true}
	if stale.Fresh(now) {
		t.Fatal("a reading older than MuleStockTTL still reads as fresh, so a startup " +
			"snapshot would authorise orders all run")
	}
	never := MuleStock{Banked: 150, At: now}
	if never.Fresh(now) {
		t.Fatal("a reading never actually heard reads as fresh")
	}
}
