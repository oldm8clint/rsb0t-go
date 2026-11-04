package evaluation

import (
	"context"
	"testing"

	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
)

type testEnv struct{ n int }

func (e *testEnv) Reset(int64) observation.State { e.n = 0; return observation.State{Health: 1} }
func (e *testEnv) Step(policy.Action) (observation.State, float32, bool) {
	e.n++
	return observation.State{Health: 1}, float32(e.n), e.n == 3
}

type testAdvisor struct{}

func (testAdvisor) Recommend(observation.State) policy.Recommendation {
	return policy.Recommendation{Action: policy.Wait, Confidence: 1}
}

func TestRunProducesDeterministicEpisodeCount(t *testing.T) {
	got, err := Run(context.Background(), &testEnv{}, testAdvisor{}, Experiment{Episodes: 4, SeedBase: 10, MaxSteps: 100})
	if err != nil {
		t.Fatal(err)
	}
	if got.Episodes != 4 {
		t.Fatalf("episodes=%d", got.Episodes)
	}
	if got.AverageReward != 6 {
		t.Fatalf("average reward=%v", got.AverageReward)
	}
}
