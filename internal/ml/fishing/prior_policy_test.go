package fishing

import (
	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
	"testing"
)

func TestVerifiedFishingPriorRecoversMissingSpot(t *testing.T) {
	r := VerifiedFishingPrior{}.Recommend(observation.State{InventorySize: 28, NearbyFish: 0})
	if r.Action != policy.Recover {
		t.Fatalf("expected recovery, got %s", r.Action)
	}
}

func TestVerifiedFishingPriorFishesValidSpot(t *testing.T) {
	r := VerifiedFishingPrior{}.Recommend(observation.State{InventorySize: 28, InventoryUsed: 2, NearbyFish: 1})
	if r.Action != policy.Fish {
		t.Fatalf("expected fish, got %s", r.Action)
	}
}

func TestVerifiedFishingPriorBanksFullInventory(t *testing.T) {
	r := VerifiedFishingPrior{}.Recommend(observation.State{InventorySize: 28, InventoryUsed: 28, NearbyFish: 1})
	if r.Action != policy.Bank {
		t.Fatalf("expected bank, got %s", r.Action)
	}
}
