package fishing

// Fixture is a deterministic fishing scenario used by the ML Lab. It is
// independent of the live client so hypotheses can be tested and replayed
// before integration with a local game server.
type Fixture struct {
	Seed         int64
	SpotVisible  bool
	SpotValid    bool
	Inventory    int
	Capacity     int
	FishPerCatch int
	Energy       int
	// InvalidateAfter exercises target-loss recovery. Zero disables it.
	InvalidateAfter int
}

type Observation struct {
	SpotVisible  bool
	SpotValid    bool
	Inventory    int
	Capacity     int
	FishPerCatch int
	Energy       int
	FishCount    int
}

type Action uint8

const (
	ActionObserve Action = iota
	ActionFish
	ActionRecover
	ActionBank
)

func (f Fixture) Reset() Observation {
	if f.Capacity <= 0 {
		f.Capacity = 28
	}
	if f.FishPerCatch <= 0 {
		f.FishPerCatch = 1
	}
	if f.Energy <= 0 {
		f.Energy = 100
	}
	return Observation{SpotVisible: f.SpotVisible, SpotValid: f.SpotValid, Inventory: f.Inventory, Capacity: f.Capacity, FishPerCatch: f.FishPerCatch, Energy: f.Energy}
}

// Step models the important prior that dispatch is not proof of success:
// fishing changes state only when the observed target is visible and valid.
func (f Fixture) Step(o Observation, a Action) (Observation, float64, bool) {
	switch a {
	case ActionFish:
		if !o.SpotVisible || !o.SpotValid || o.Inventory >= o.Capacity {
			return o, -1, false
		}
		n := o.Inventory + o.FishPerCatch
		if n > o.Capacity {
			n = o.Capacity
		}
		o.Inventory, o.Energy, o.FishCount = n, o.Energy-1, o.FishCount+1
		if f.InvalidateAfter > 0 && o.FishCount >= f.InvalidateAfter {
			o.SpotValid = false
		}
		return o, 1, n >= o.Capacity
	case ActionBank:
		o.Inventory = 0
		return o, 0.2, false
	case ActionRecover:
		o.SpotVisible, o.SpotValid = true, true
		return o, 0.1, false
	default:
		return o, 0, false
	}
}
