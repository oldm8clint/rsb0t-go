package evaluation

import (
	"fmt"

	"rsb0t-go/internal/ml/replay"
)

// ReplayComparison describes whether two episodes reproduce the same ordered
// observation/action trace. Reward values are intentionally excluded: reward
// is an evaluation metric, not part of replay identity.
type ReplayComparison struct {
	Match           bool   `json:"match"`
	ComparedSteps   int    `json:"compared_steps"`
	FirstDivergence int    `json:"first_divergence"`
	Reason          string `json:"reason,omitempty"`
}

// CompareReplay deterministically compares two replay traces and reports the
// first divergent step. FirstDivergence is -1 when the traces match.
func CompareReplay(baseline, candidate *replay.Episode) (ReplayComparison, error) {
	if baseline == nil || candidate == nil {
		return ReplayComparison{}, fmt.Errorf("replay comparison requires two episodes")
	}

	if baseline.SchemaVersion != candidate.SchemaVersion {
		return ReplayComparison{
			Match:           false,
			FirstDivergence: 0,
			Reason:          "schema version differs",
		}, nil
	}

	limit := len(baseline.Steps)
	if len(candidate.Steps) < limit {
		limit = len(candidate.Steps)
	}
	for i := 0; i < limit; i++ {
		if !sameReplayStep(baseline.Steps[i], candidate.Steps[i]) {
			return ReplayComparison{
				Match:           false,
				ComparedSteps:   i,
				FirstDivergence: i,
				Reason:          "observation, action, or confidence differs",
			}, nil
		}
	}

	if len(baseline.Steps) != len(candidate.Steps) {
		return ReplayComparison{
			Match:           false,
			ComparedSteps:   limit,
			FirstDivergence: limit,
			Reason:          "trace length differs",
		}, nil
	}

	return ReplayComparison{
		Match:           true,
		ComparedSteps:   limit,
		FirstDivergence: -1,
	}, nil
}

func sameReplayStep(a, b replay.Step) bool {
	return a.Observation == b.Observation &&
		a.Action == b.Action &&
		a.Confidence == b.Confidence
}
