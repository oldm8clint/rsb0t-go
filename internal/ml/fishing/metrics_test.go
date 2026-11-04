package fishing

import "testing"

func TestRunCountsVerifiedCatches(t *testing.T) {
	m := Run(Fixture{SpotVisible: true, SpotValid: true, Capacity: 2, FishPerCatch: 1, Energy: 10}, func(Observation) Action { return ActionFish }, 10)
	if !m.Completed || m.Catches != 2 || m.Failures != 0 {
		t.Fatalf("unexpected metrics: %+v", m)
	}
}

func TestRunCountsFailedFishAttempts(t *testing.T) {
	m := Run(Fixture{SpotVisible: false, SpotValid: false, Capacity: 2, FishPerCatch: 1, Energy: 10}, func(Observation) Action { return ActionFish }, 3)
	if m.Failures != 3 || m.Catches != 0 || m.Completed {
		t.Fatalf("unexpected metrics: %+v", m)
	}
}
