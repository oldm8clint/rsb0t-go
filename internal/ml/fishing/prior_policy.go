package fishing

import (
	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
)

// VerifiedFishingPrior is a small candidate advisor seeded from the reviewed
// fishing learnings. It treats a missing/invalid target as a recovery signal
// instead of assuming a previous interaction succeeded.
type VerifiedFishingPrior struct{}

func (VerifiedFishingPrior) Recommend(s observation.State) policy.Recommendation {
	if s.InventorySize > 0 && s.InventoryUsed >= s.InventorySize {
		return policy.Recommendation{Action: policy.Bank, Confidence: 1}
	}
	if s.NearbyFish == 0 {
		return policy.Recommendation{Action: policy.Recover, Confidence: .9}
	}
	return policy.Recommendation{Action: policy.Fish, Confidence: .9}
}
