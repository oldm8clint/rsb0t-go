package evaluation

import "testing"

func TestBuildReportGate(t *testing.T) {
	base := Summary{Episodes: 10, AverageReward: 10, AverageSteps: 100}
	learned := Summary{Episodes: 10, AverageReward: 12, AverageSteps: 105}

	r := BuildReport(base, learned, GateConfig{MinRewardDelta: 1, MaxStepIncreaseRatio: 0.10})
	if !r.PassesGate {
		t.Fatalf("expected candidate to pass: %#v", r)
	}
	if r.RewardDeltaRatio != 0.2 {
		t.Fatalf("reward ratio = %v", r.RewardDeltaRatio)
	}
	if r.StepDeltaRatio != 0.05 {
		t.Fatalf("step ratio = %v", r.StepDeltaRatio)
	}
}

func TestBuildReportRejectsStepRegression(t *testing.T) {
	base := Summary{Episodes: 10, AverageReward: 10, AverageSteps: 100}
	learned := Summary{Episodes: 10, AverageReward: 20, AverageSteps: 130}

	r := BuildReport(base, learned, GateConfig{MinRewardDelta: 1, MaxStepIncreaseRatio: 0.10})
	if r.PassesGate {
		t.Fatal("expected candidate to fail step gate")
	}
}
