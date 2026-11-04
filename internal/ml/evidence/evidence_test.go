package evidence

import (
	"testing"

	"rsb0t-go/internal/ml/evaluation"
)

func TestBuildPassingEvidence(t *testing.T) {
	report := evaluation.BuildReport(
		evaluation.Summary{AverageReward: 10, AverageSteps: 100},
		evaluation.Summary{AverageReward: 12, AverageSteps: 95},
		evaluation.GateConfig{MinRewardDelta: 1, MaxStepIncreaseRatio: 0.10},
	)
	r := Build("fishing", Metrics{10, 100}, Metrics{12, 95}, report)
	if !report.PassesGate || len(r.Recommendations) == 0 {
		t.Fatalf("expected useful passing evidence: %#v", r)
	}
}

func TestBuildRejectsCandidate(t *testing.T) {
	report := evaluation.BuildReport(
		evaluation.Summary{AverageReward: 10, AverageSteps: 100},
		evaluation.Summary{AverageReward: 9, AverageSteps: 130},
		evaluation.GateConfig{MinRewardDelta: 1, MaxStepIncreaseRatio: 0.10},
	)
	r := Build("fishing", Metrics{10, 100}, Metrics{9, 130}, report)
	if report.PassesGate || len(r.Observations) == 0 {
		t.Fatalf("expected rejection evidence: %#v", r)
	}
}
