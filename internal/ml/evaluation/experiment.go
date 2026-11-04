package evaluation

import (
	"context"

	"rsb0t-go/internal/ml/policy"
	"rsb0t-go/internal/ml/replay"
)

// Experiment controls a reproducible offline policy evaluation run.
type Experiment struct {
	Episodes int
	SeedBase int64
	MaxSteps int
}

// Run reuses the canonical replay runner for each seeded episode. It is intended
// for offline workers and never belongs in the live client tick path.
func Run(ctx context.Context, env replay.Environment, advisor policy.Advisor, cfg Experiment) (Summary, error) {
	if cfg.Episodes <= 0 {
		return Summary{}, nil
	}
	if cfg.MaxSteps < 1 {
		cfg.MaxSteps = 1
	}
	episodes := make([]*replay.Episode, 0, cfg.Episodes)
	for i := 0; i < cfg.Episodes; i++ {
		episode, err := replay.Run(ctx, env, advisor, uint64(i+1), cfg.SeedBase+int64(i), cfg.MaxSteps)
		if err != nil {
			return Summary{}, err
		}
		episodes = append(episodes, &episode)
	}
	return Evaluate(episodes), nil
}
