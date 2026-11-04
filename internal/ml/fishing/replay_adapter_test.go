package fishing

import (
	"testing"

	"rsb0t-go/internal/ml/policy"
)

func TestReplayAdapterUsesCommonActions(t *testing.T) {
	e := NewReplayEnvironment(Fixture{SpotVisible: true, SpotValid: true, Capacity: 2, FishPerCatch: 1, Energy: 10})
	o := e.Reset(42)
	if o.InventorySize != 2 || o.NearbyFish != 1 {
		t.Fatalf("unexpected reset state: %+v", o)
	}
	n, reward, done := e.Step(policy.Fish)
	if reward <= 0 || done || n.InventoryUsed != 1 {
		t.Fatalf("unexpected fish transition: %+v reward=%v done=%v", n, reward, done)
	}
}
