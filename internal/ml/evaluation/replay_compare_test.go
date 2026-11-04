package evaluation

import (
	"testing"

	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
	"rsb0t-go/internal/ml/replay"
)

func testReplayEpisode() *replay.Episode {
	return &replay.Episode{
		SchemaVersion: 1,
		EpisodeID:     42,
		Steps: []replay.Step{
			{Observation: observation.State{X: 1, Y: 2}, Action: policy.WalkToFish, Confidence: 1, Reward: 1},
			{Observation: observation.State{X: 2, Y: 2}, Action: policy.WalkToFish, Confidence: 1, Reward: 2},
		},
	}
}

func TestCompareReplayMatch(t *testing.T) {
	result, err := CompareReplay(testReplayEpisode(), testReplayEpisode())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Match || result.FirstDivergence != -1 || result.ComparedSteps != 2 {
		t.Fatalf("unexpected match result: %+v", result)
	}
}

func TestCompareReplayIgnoresReward(t *testing.T) {
	candidate := testReplayEpisode()
	candidate.Steps[0].Reward = 99
	candidate.Steps[1].Reward = -20

	result, err := CompareReplay(testReplayEpisode(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Match || result.FirstDivergence != -1 || result.ComparedSteps != 2 {
		t.Fatalf("reward-only changes should match: %+v", result)
	}
}

func TestCompareReplayReportsFirstDivergence(t *testing.T) {
	candidate := testReplayEpisode()
	candidate.Steps[1].Action = policy.WalkToBank

	result, err := CompareReplay(testReplayEpisode(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if result.Match || result.FirstDivergence != 1 || result.ComparedSteps != 1 {
		t.Fatalf("unexpected divergence result: %+v", result)
	}
}

func TestCompareReplayReportsLengthDivergence(t *testing.T) {
	candidate := testReplayEpisode()
	candidate.Steps = candidate.Steps[:1]

	result, err := CompareReplay(testReplayEpisode(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if result.Match || result.FirstDivergence != 1 || result.ComparedSteps != 1 {
		t.Fatalf("unexpected length result: %+v", result)
	}
}

func TestCompareReplayRejectsNil(t *testing.T) {
	if _, err := CompareReplay(nil, testReplayEpisode()); err == nil {
		t.Fatal("expected nil replay error")
	}
}
