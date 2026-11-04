package evaluation

import (
	"testing"

	"rsb0t-go/internal/ml/replay"
)

func TestEvaluateValidatedRejectsEmptyEpisodes(t *testing.T) {
	if _, err := EvaluateValidated(nil); err == nil {
		t.Fatal("expected error for empty evaluation")
	}
}

func TestEvaluateValidatedRejectsNilEpisode(t *testing.T) {
	if _, err := EvaluateValidated([]*replay.Episode{nil}); err == nil {
		t.Fatal("expected error for nil episode")
	}
}

func TestEvaluateValidatedDelegatesValidEpisodes(t *testing.T) {
	episode := &replay.Episode{TotalReward: 7}
	got, err := EvaluateValidated([]*replay.Episode{episode})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Episodes != 1 || got.AverageReward != 7 {
		t.Fatalf("unexpected summary: %+v", got)
	}
}
