package mulerunner

import (
	"testing"
	"time"
)

// donorDrawMedian is the measured half of the trade cycle that the mule can only wait through:
// CLAUDE.md's mule-throughput row, "20s the donor spends drawing and settling its offer (the
// mule only waits)". Named here so the relationship below is checkable rather than remembered.
const donorDrawMedian = 20 * time.Second

// Live 2026-09-22, the failure this guards against, with the decline and the donor's arrival
// landing in the same second:
//
//	22:51:43 [mule]  trade with "Acct80" not completed
//	22:51:43 [acct80] donor: trade with "mule" reached the confirm screen
//
// A timeout equal to the median of the thing it waits for fails about half the time.
func TestTheStableOfferWaitOutlastsTheDonorsDraw(t *testing.T) {
	if stableOfferWait <= donorDrawMedian {
		t.Fatalf("stableOfferWait = %s against a measured donor draw median of %s: a timeout "+
			"at or below the median loses roughly half of all trades", stableOfferWait, donorDrawMedian)
	}
	if stableOfferWait < 2*donorDrawMedian {
		t.Errorf("stableOfferWait = %s is under twice the %s median, so the slow half of the "+
			"distribution still times out", stableOfferWait, donorDrawMedian)
	}
}

// ...and it must stay inside the budget that bounds the whole offer stage, or the hard cap
// stops being the thing that ends a stuck trade.
func TestTheStableOfferWaitStaysUnderTheHardCap(t *testing.T) {
	if stableOfferWait >= confirmHardCap {
		t.Fatalf("stableOfferWait = %s is not under confirmHardCap = %s", stableOfferWait, confirmHardCap)
	}
	// Room for the confirm stage afterwards, which is what the cap actually has to cover.
	if stableOfferWait+confirmWait >= confirmHardCap {
		t.Errorf("stableOfferWait (%s) plus confirmWait (%s) leaves nothing under the %s cap "+
			"for the rest of the offer stage", stableOfferWait, confirmWait, confirmHardCap)
	}
}
