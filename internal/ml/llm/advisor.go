package llm

import (
	"encoding/json"
	"errors"

	"rsb0t-go/internal/ml/evidence"
	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
)

// Advisor is the provider-neutral boundary between the ML Lab and an LLM.
// Implementations return structured recommendations; they do not execute code
// or receive live client handles.
type Advisor interface {
	Recommend(evidence.Record, observation.State) (policy.Recommendation, error)
}

// MockAdvisor provides a deterministic local implementation for development
// and integration tests without an API key or network access.
type MockAdvisor struct{}

func (MockAdvisor) Recommend(_ evidence.Record, s observation.State) (policy.Recommendation, error) {
	if s.Health == 0 {
		return policy.Recommendation{Action: policy.Recover, Confidence: 1}, nil
	}
	if s.InventorySize > 0 && s.InventoryUsed >= s.InventorySize {
		if s.DistanceToBank > 0 {
			return policy.Recommendation{Action: policy.WalkToBank, Confidence: .95}, nil
		}
		return policy.Recommendation{Action: policy.Bank, Confidence: .95}, nil
	}
	if s.DistanceToFish > 0 {
		return policy.Recommendation{Action: policy.WalkToFish, Confidence: .9}, nil
	}
	if s.NearbyFish > 0 {
		return policy.Recommendation{Action: policy.Fish, Confidence: .9}, nil
	}
	return policy.Recommendation{Action: policy.Wait, Confidence: .25}, nil
}

// DecodeRecommendation validates JSON returned by a future provider without executing it.
func DecodeRecommendation(data []byte) (policy.Recommendation, error) {
	var r policy.Recommendation
	if err := json.Unmarshal(data, &r); err != nil {
		return r, err
	}
	if r.Action == "" {
		return r, errors.New("llm recommendation missing action")
	}
	if r.Confidence < 0 || r.Confidence > 1 {
		return r, errors.New("llm recommendation confidence outside 0..1")
	}
	switch r.Action {
	case policy.WalkToFish, policy.Fish, policy.WalkToBank, policy.Bank, policy.Wait, policy.Recover:
		return r, nil
	default:
		return r, errors.New("llm recommendation contains unsupported action")
	}
}
