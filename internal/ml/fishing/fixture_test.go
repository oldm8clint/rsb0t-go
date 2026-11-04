package fishing

import "testing"

func TestFishingRequiresVerifiedTarget(t *testing.T) {
	f := Fixture{SpotVisible: false, SpotValid: false, Capacity: 2, FishPerCatch: 1, Energy: 10}
	o := f.Reset()
	n, reward, done := f.Step(o, ActionFish)
	if reward >= 0 || done || n.Inventory != 0 {
		t.Fatalf("invalid target must not produce a catch: %+v reward=%v done=%v", n, reward, done)
	}
}

func TestFishingProgressAndCompletion(t *testing.T) {
	f := Fixture{SpotVisible: true, SpotValid: true, Capacity: 2, FishPerCatch: 1, Energy: 10}
	o := f.Reset()
	o, reward, done := f.Step(o, ActionFish)
	if reward != 1 || done || o.Inventory != 1 {
		t.Fatalf("first catch mismatch: %+v reward=%v done=%v", o, reward, done)
	}
	o, reward, done = f.Step(o, ActionFish)
	if reward != 1 || !done || o.Inventory != 2 {
		t.Fatalf("completion mismatch: %+v reward=%v done=%v", o, reward, done)
	}
}

func TestRecoveryRestoresTargetObservation(t *testing.T) {
	f := Fixture{}
	o := Observation{}
	o, _, _ = f.Step(o, ActionRecover)
	if !o.SpotVisible || !o.SpotValid {
		t.Fatalf("recovery did not restore target: %+v", o)
	}
}
