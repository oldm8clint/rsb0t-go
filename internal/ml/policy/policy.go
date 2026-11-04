package policy

import "rsb0t-go/internal/ml/observation"

type Action string

const (
	WalkToFish Action = "WALK_TO_FISH"
	Fish       Action = "FISH"
	WalkToBank Action = "WALK_TO_BANK"
	Bank       Action = "BANK"
	Wait       Action = "WAIT"
	Recover    Action = "RECOVER"
)

type Recommendation struct {
	Action     Action
	Confidence float32
}

type Advisor interface {
	Recommend(observation.State) Recommendation
}

// DeterministicFallback provides a safe baseline for simulations and for
// periods when no trained model is available.
type DeterministicFallback struct{}

func (DeterministicFallback) Recommend(s observation.State) Recommendation {
	if s.Health == 0 {
		return Recommendation{Action: Recover, Confidence: 1}
	}
	if s.InventorySize > 0 && s.InventoryUsed >= s.InventorySize {
		if s.DistanceToBank > 0 {
			return Recommendation{Action: WalkToBank, Confidence: 1}
		}
		return Recommendation{Action: Bank, Confidence: 1}
	}
	if s.DistanceToFish > 0 {
		return Recommendation{Action: WalkToFish, Confidence: 1}
	}
	if s.NearbyFish > 0 {
		return Recommendation{Action: Fish, Confidence: 1}
	}
	return Recommendation{Action: Wait, Confidence: .5}
}
