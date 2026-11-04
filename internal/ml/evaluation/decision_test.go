package evaluation

import (
	"testing"

	"rsb0t-go/internal/ml/replay"
)

func validEvidence() []EpisodeEvidence {
	return []EpisodeEvidence{{Episode: &replay.Episode{}, Status: StatusCompleted}}
}

func TestDecideEvidenceInvalidEvidence(t *testing.T) {
	report := Report{PassesGate: true}
	evidence := []EpisodeEvidence{{Episode: &replay.Episode{}, Status: StatusFailed}}
	got := DecideEvidence(evidence, EvidenceGateConfig{}, report)
	if got != DecisionInvalid {
		t.Fatalf("expected INVALID, got %s", got)
	}
}

func TestDecideEvidenceInvalidConfig(t *testing.T) {
	report := Report{PassesGate: true}
	cfg := EvidenceGateConfig{MinCompletionRate: -0.1}
	got := DecideEvidence(validEvidence(), cfg, report)
	if got != DecisionInvalid {
		t.Fatalf("expected INVALID, got %s", got)
	}
}

func TestDecideEvidenceRejectsFailedGate(t *testing.T) {
	report := Report{PassesGate: false}
	got := DecideEvidence(validEvidence(), EvidenceGateConfig{}, report)
	if got != DecisionReject {
		t.Fatalf("expected REJECT, got %s", got)
	}
}

func TestDecideEvidencePassesValidReport(t *testing.T) {
	report := Report{PassesGate: true}
	got := DecideEvidence(validEvidence(), EvidenceGateConfig{}, report)
	if got != DecisionPass {
		t.Fatalf("expected PASS, got %s", got)
	}
}
