package policy

import (
	"rsb0t-go/internal/ml/observation"
	"testing"
)

func TestFallbackChoosesBankWhenInventoryFull(t *testing.T) {
	r := (DeterministicFallback{}).Recommend(observation.State{Health: 100, InventoryUsed: 28, InventorySize: 28, DistanceToBank: 5})
	if r.Action != WalkToBank {
		t.Fatalf("got %q", r.Action)
	}
}

func TestFallbackChoosesFishWhenAtFishingSpot(t *testing.T) {
	r := (DeterministicFallback{}).Recommend(observation.State{Health: 100, InventorySize: 28, NearbyFish: 1})
	if r.Action != Fish {
		t.Fatalf("got %q", r.Action)
	}
}
