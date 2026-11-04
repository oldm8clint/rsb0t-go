package fishing

import "testing"

func TestFixtureInvalidatesTargetAfterConfiguredCatches(t *testing.T) {
	f := Fixture{SpotVisible: true, SpotValid: true, Capacity: 10, FishPerCatch: 1, Energy: 20, InvalidateAfter: 2}
	o := f.Reset()
	o, _, _ = f.Step(o, ActionFish)
	if !o.SpotValid {
		t.Fatal("target invalidated too early")
	}
	o, _, _ = f.Step(o, ActionFish)
	if o.SpotValid {
		t.Fatal("target should be invalid after configured catches")
	}
	o, reward, _ := f.Step(o, ActionFish)
	if reward >= 0 || o.Inventory != 2 {
		t.Fatal("invalid target must not produce another catch")
	}
	o, reward, _ = f.Step(o, ActionRecover)
	if reward <= 0 || !o.SpotValid {
		t.Fatal("recovery should restore a valid target")
	}
}
