package evaluation

import (
	"testing"

	"rsb0t-go/internal/ml/replay"
)

func pipelineEvidence() []EpisodeEvidence {
	return []EpisodeEvidence{{Episode: &replay.Episode{}, Status: StatusCompleted}}
}

func TestEvaluateEvidenceRejectsEvidenceGateFailure(t *testing.T) {
	episodes := []EpisodeEvidence{
		{Episode: &replay.Episode{}, Status: StatusCompleted},
		{Episode: &replay.Episode{}, Status: StatusFailed, Failure: "timeout"},
	}
	cfg := EvidenceGateConfig{MaxFailureRate: 0}
	got := EvaluateEvidence(episodes, cfg, Report{PassesGate: true})
	if got != DecisionReject {
		t.Fatalf("expected REJECT, got %s", got)
	}
}

func TestEvaluateEvidencePreservesExistingReject(t *testing.T) {
	got := EvaluateEvidence(pipelineEvidence(), EvidenceGateConfig{}, Report{PassesGate: false})
	if got != DecisionReject {
		t.Fatalf("expected REJECT, got %s", got)
	}
}

func TestEvaluateEvidenceRejectsInvalidEvidence(t *testing.T) {
	episodes := []EpisodeEvidence{{Episode: &replay.Episode{}, Status: StatusFailed}}
	got := EvaluateEvidence(episodes, EvidenceGateConfig{}, Report{PassesGate: true})
	if got != DecisionInvalid {
		t.Fatalf("expected INVALID, got %s", got)
	}
}

func TestEvaluateEvidencePassesValidEvidence(t *testing.T) {
	got := EvaluateEvidence(pipelineEvidence(), EvidenceGateConfig{}, Report{PassesGate: true})
	if got != DecisionPass {
		t.Fatalf("expected PASS, got %s", got)
	}
}
