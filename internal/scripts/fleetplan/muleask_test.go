package fleetplan

import (
	"testing"
)

// An unpublished (or stale) bank asks blind, as the code always did, but a fair-share line is
// still bounded by BlindShare.
func TestFilterAskByBankUnknownBankAsksBlind(t *testing.T) {
	plan, none := FilterAskByBank([]AskLine{
		{Choices: []string{"Cake", "Bread"}, Count: 500, FairShare: true},
		{Choices: []string{"Chaos rune"}, Count: 150},
	}, nil)
	if len(none) != 0 || len(plan) != 2 {
		t.Fatalf("plan %v none %v", plan, none)
	}
	if r := plan[0].Rungs; len(r) != 1 || r[0].Item != "Cake" || r[0].Count != BlindAskCap {
		t.Fatalf("blind food: %v", r)
	}
	if plan[1].Rungs[0].Count != 150 {
		t.Fatalf("blind chaos: %v", plan[1].Rungs)
	}
}
