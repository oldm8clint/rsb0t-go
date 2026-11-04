package evaluation

import (
	"rsb0t-go/internal/ml/replay"
	"testing"
)

func TestEvaluateEmpty(t *testing.T) {
	if got := Evaluate(nil); got.Episodes != 0 {
		t.Fatalf("expected empty summary, got %+v", got)
	}
}

func TestEvaluateAggregates(t *testing.T) {
	a := &replay.Episode{TotalReward: 2, Steps: make([]replay.Step, 4)}
	b := &replay.Episode{TotalReward: 6, Steps: make([]replay.Step, 2)}
	got := Evaluate([]*replay.Episode{a, b})
	if got.AverageReward != 4 || got.AverageSteps != 3 || got.MinReward != 2 || got.MaxReward != 6 {
		t.Fatalf("unexpected summary: %+v", got)
	}
}
