package evaluation

import "testing"

func TestApplyEvidenceGatesRejectsLowCompletion(t *testing.T) {
	report := Report{PassesGate: true}
	metrics := EvidenceMetrics{CompletionRate: 0.4, FailureRate: 0.1}

	got := ApplyEvidenceGates(report, metrics, EvidenceGateConfig{
		MinCompletionRate: 0.5,
		MaxFailureRate:    -1,
	})
	if got.PassesGate {
		t.Fatal("expected low completion rate to reject the report")
	}
	if len(got.Reasons) != 1 {
		t.Fatalf("expected one rejection reason, got %d", len(got.Reasons))
	}
}

func TestApplyEvidenceGatesRejectsFailureAndRecoveryLimits(t *testing.T) {
	report := Report{PassesGate: true}
	metrics := EvidenceMetrics{
		CompletionRate:  0.9,
		FailureRate:     0.2,
		AverageRecovery: 2.5,
	}

	got := ApplyEvidenceGates(report, metrics, EvidenceGateConfig{
		MinCompletionRate:  0.5,
		MaxFailureRate:     0.1,
		MaxAverageRecovery: 2,
	})
	if got.PassesGate {
		t.Fatal("expected failure and recovery limits to reject the report")
	}
	if len(got.Reasons) != 2 {
		t.Fatalf("expected two rejection reasons, got %d", len(got.Reasons))
	}
}

func TestApplyEvidenceGatesPreservesExistingRejection(t *testing.T) {
	report := Report{PassesGate: false, Reasons: []string{"reward gate failed"}}
	metrics := EvidenceMetrics{CompletionRate: 1, FailureRate: 0}

	got := ApplyEvidenceGates(report, metrics, EvidenceGateConfig{
		MinCompletionRate: 0.5,
		MaxFailureRate:    0,
	})
	if got.PassesGate {
		t.Fatal("evidence gates must not turn an existing rejection into a pass")
	}
	if len(got.Reasons) != 1 || got.Reasons[0] != "reward gate failed" {
		t.Fatalf("unexpected reasons: %#v", got.Reasons)
	}
}
