package fishing

import (
	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
	"rsb0t-go/internal/ml/replay"
)

// ReplayEnvironment adapts the fishing fixture to the common ML replay interface.
type ReplayEnvironment struct {
	Fixture Fixture
	state   Observation
}

var _ replay.Environment = (*ReplayEnvironment)(nil)

func NewReplayEnvironment(f Fixture) *ReplayEnvironment { return &ReplayEnvironment{Fixture: f} }

func (e *ReplayEnvironment) Reset(seed int64) observation.State {
	e.Fixture.Seed = seed
	e.state = e.Fixture.Reset()
	return toState(e.state, "", true)
}

func (e *ReplayEnvironment) Step(action policy.Action) (observation.State, float32, bool) {
	n, reward, done := e.Fixture.Step(e.state, toFishingAction(action))
	e.state = n
	return toState(n, string(action), reward >= 0), float32(reward), done
}

func toFishingAction(a policy.Action) Action {
	switch a {
	case policy.Fish:
		return ActionFish
	case policy.Bank:
		return ActionBank
	case policy.Recover:
		return ActionRecover
	default:
		return ActionObserve
	}
}

func toState(o Observation, last string, ok bool) observation.State {
	return observation.State{
		SchemaVersion:  1,
		InventoryUsed:  uint8(o.Inventory),
		InventorySize:  uint8(o.Capacity),
		Health:         uint16(maxInt(o.Energy, 0)),
		DistanceToFish: distance(o.SpotVisible && o.SpotValid),
		DistanceToBank: distance(o.Inventory < o.Capacity),
		NearbyFish:     boolCount(o.SpotVisible && o.SpotValid),
		NearbyBank:     1,
		LastAction:     last,
		LastActionOK:   ok,
	}
}

func distance(near bool) uint16 {
	if near {
		return 0
	}
	return 1
}
func boolCount(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
