package replay

import (
	"context"
	"testing"

	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
)

type testEnv struct{ n int }

func (e *testEnv) Reset(int64) observation.State {
	e.n = 0
	return observation.State{SchemaVersion: 1, Health: 100, NearbyFish: 1, InventorySize: 28}
}
func (e *testEnv) Step(policy.Action) (observation.State, float32, bool) {
	e.n++
	return observation.State{SchemaVersion: 1, Health: 100}, 1, e.n >= 3
}

func TestRunIsBounded(t *testing.T) {
	e := &testEnv{}
	ep, err := Run(context.Background(), e, policy.DeterministicFallback{}, 9, 42, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ep.Steps) != 3 || ep.TotalReward != 3 {
		t.Fatalf("steps=%d reward=%v", len(ep.Steps), ep.TotalReward)
	}
}

func TestRunHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, &testEnv{}, policy.DeterministicFallback{}, 1, 1, 10)
	if err == nil {
		t.Fatal("expected cancellation")
	}
}
