package evidence

import (
	"rsb0t-go/internal/ml/evaluation"
	"testing"
)

func TestFromPairPreservesAggregateMetrics(t *testing.T) {
	result := evaluation.PairResult{
		Baseline:  evaluation.Summary{Episodes: 2, AverageReward: 1.5, AverageSteps: 8},
		Candidate: evaluation.Summary{Episodes: 2, AverageReward: 2.5, AverageSteps: 6},
	}
	r := FromPair("fishing", result)
	if r.Task != "fishing" || r.Baseline.AverageReward != 1.5 || r.Candidate.AverageSteps != 6 {
		t.Fatalf("unexpected evidence: %+v", r)
	}
}
