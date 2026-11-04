package evaluation

import (
	"context"
	"fmt"

	"rsb0t-go/internal/ml/policy"
	"rsb0t-go/internal/ml/replay"
)

// PairConfig controls a matched baseline/candidate experiment.
type PairConfig struct {
	Episodes int
	SeedBase int64
	MaxSteps int
	Gate     GateConfig
}

// PairResult contains the offline comparison needed by the next ML stage.
type PairResult struct {
	Baseline  Summary `json:"baseline"`
	Candidate Summary `json:"candidate"`
	Report    Report  `json:"report"`
}

// RunPair evaluates two policies against matched seeds and applies the normal
// quality gate. It must remain outside the live client tick path.
func RunPair(ctx context.Context, env replay.Environment, baseline, candidate policy.Advisor, cfg PairConfig) (PairResult, error) {
	if env == nil || baseline == nil || candidate == nil {
		return PairResult{}, fmt.Errorf("environment and advisors are required")
	}
	if cfg.Episodes <= 0 {
		return PairResult{}, fmt.Errorf("episodes must be positive")
	}
	if cfg.MaxSteps < 1 {
		cfg.MaxSteps = 1
	}

	run := func(advisor policy.Advisor) (Summary, error) {
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

	base, err := run(baseline)
	if err != nil {
		return PairResult{}, fmt.Errorf("baseline: %w", err)
	}
	cand, err := run(candidate)
	if err != nil {
		return PairResult{}, fmt.Errorf("candidate: %w", err)
	}
	return PairResult{Baseline: base, Candidate: cand, Report: BuildReport(base, cand, cfg.Gate)}, nil
}
