package replay

import (
	"context"

	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
)

type Environment interface {
	Reset(seed int64) observation.State
	Step(action policy.Action) (next observation.State, reward float32, done bool)
}

const initialStepCapacity = 256

func Run(ctx context.Context, env Environment, advisor policy.Advisor, episodeID uint64, seed int64, maxSteps int) (Episode, error) {
	if maxSteps < 1 {
		maxSteps = 1
	}
	capacity := maxSteps
	if capacity > initialStepCapacity {
		capacity = initialStepCapacity
	}
	e := Episode{SchemaVersion: 1, EpisodeID: episodeID, Steps: make([]Step, 0, capacity)}
	state := env.Reset(seed)
	for i := 0; i < maxSteps; i++ {
		select {
		case <-ctx.Done():
			return e, ctx.Err()
		default:
		}
		rec := advisor.Recommend(state)
		next, reward, done := env.Step(rec.Action)
		e.Add(Step{Observation: state, Action: rec.Action, Confidence: rec.Confidence, Reward: reward})
		state = next
		if done {
			break
		}
	}
	return e, nil
}
