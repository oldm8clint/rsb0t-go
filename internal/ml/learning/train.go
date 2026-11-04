package learning

import (
	"context"
	"fmt"

	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
	"rsb0t-go/internal/ml/replay"
)

// TrainConfig controls a bounded, deterministic Q-learning run. It is intended
// for offline ML Lab workers and never belongs in the live client tick path.
type TrainConfig struct {
	Episodes     int
	SeedBase     int64
	MaxSteps     int
	Epsilon      float32
	EpsilonMin   float32
	EpsilonDecay float32
}

// TrainStats contains compact progress information suitable for a CLI or UI.
type TrainStats struct {
	Episodes      int
	Steps         int
	States        int
	TotalReward   float64
	AverageReward float64
}

// Train runs tabular Q-learning against an offline environment. The same seed
// schedule can be replayed later to diagnose a candidate policy.
func Train(ctx context.Context, env replay.Environment, q *QTable, cfg TrainConfig) (TrainStats, error) {
	if env == nil || q == nil {
		return TrainStats{}, fmt.Errorf("ml training requires environment and learner")
	}
	if cfg.Episodes < 1 {
		return TrainStats{}, nil
	}
	if cfg.MaxSteps < 1 {
		cfg.MaxSteps = 1
	}
	if cfg.Epsilon < 0 {
		cfg.Epsilon = 0
	}
	if cfg.Epsilon > 1 {
		cfg.Epsilon = 1
	}
	if cfg.EpsilonMin < 0 {
		cfg.EpsilonMin = 0
	}
	if cfg.EpsilonMin > cfg.Epsilon {
		cfg.EpsilonMin = cfg.Epsilon
	}
	if cfg.EpsilonDecay <= 0 || cfg.EpsilonDecay > 1 {
		cfg.EpsilonDecay = 0.995
	}

	stats := TrainStats{Episodes: cfg.Episodes}
	epsilon := cfg.Epsilon
	for episode := 0; episode < cfg.Episodes; episode++ {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}
		state := env.Reset(cfg.SeedBase + int64(episode))
		for step := 0; step < cfg.MaxSteps; step++ {
			select {
			case <-ctx.Done():
				return stats, ctx.Err()
			default:
			}
			rec := q.Choose(state, epsilon)
			next, reward, done := env.Step(rec.Action)
			q.Update(state, rec.Action, reward, next, done)
			stats.Steps++
			stats.TotalReward += float64(reward)
			state = next
			if done {
				break
			}
		}
		if epsilon > cfg.EpsilonMin {
			epsilon *= cfg.EpsilonDecay
			if epsilon < cfg.EpsilonMin {
				epsilon = cfg.EpsilonMin
			}
		}
	}
	stats.States = q.States()
	stats.AverageReward = stats.TotalReward / float64(stats.Episodes)
	return stats, nil
}

// Recommend makes a trained QTable directly usable anywhere a policy.Advisor
// is expected, including replay and the bounded ML sandbox.
func (q *QTable) Recommend(s observation.State) policy.Recommendation { return q.Best(s) }

var _ policy.Advisor = (*QTable)(nil)
