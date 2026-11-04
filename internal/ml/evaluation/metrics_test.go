package evaluation

import (
	"testing"

	"rsb0t-go/internal/ml/replay"
)

func TestAggregateEvidence(t *testing.T) {
	evidence := []EpisodeEvidence{
		{Episode: &replay.Episode{}, Status: StatusCompleted},
		{Episode: &replay.Episode{}, Status: StatusFailed, Failure: "timeout", RecoveryCount: 1},
		{Episode: &replay.Episode{}, Status: StatusCompleted, RecoveryCount: 2},
	}

	got, err := AggregateEvidence(evidence)
	if err != nil {
		t.Fatalf("AggregateEvidence() error = %v", err)
	}
	if got.Episodes != 3 || got.Completed != 2 || got.Failed != 1 {
		t.Fatalf("unexpected counts: %+v", got)
	}
	if got.RecoveryCount != 3 || got.AverageRecovery != 1 {
		t.Fatalf("unexpected recovery metrics: %+v", got)
	}
	if got.CompletionRate != 2.0/3.0 || got.FailureRate != 1.0/3.0 || got.RecoveryRate != 1.0 {
		t.Fatalf("unexpected rates: %+v", got)
	}
}

func TestAggregateEvidenceRejectsInvalidInput(t *testing.T) {
	_, err := AggregateEvidence([]EpisodeEvidence{{Episode: &replay.Episode{}, Status: StatusFailed}})
	if err == nil {
		t.Fatal("AggregateEvidence() accepted failed evidence without a reason")
	}
}
