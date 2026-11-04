// Package sandbox provides a deliberately non-executable validation runner for
// LLM-generated script candidates. Candidates are reduced to their declared,
// allow-listed action plan and evaluated against an offline ML environment.
// Generated source is never interpreted or executed here.
package sandbox

import (
	"context"
	"errors"
	"fmt"

	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/replay"
	"rsb0t-go/internal/ml/scriptcontract"
)

type Config struct{ MaxSteps int }

type Result struct {
	Valid       bool              `json:"valid"`
	Steps       int               `json:"steps"`
	TotalReward float64           `json:"total_reward"`
	Final       observation.State `json:"final"`
	Errors      []string          `json:"errors,omitempty"`
}

// Run executes only the candidate's declared action plan against an offline
// environment. Source code, tests and arbitrary I/O are intentionally ignored.
func Run(ctx context.Context, env replay.Environment, candidate scriptcontract.Candidate, cfg Config) Result {
	validation := scriptcontract.Validate(candidate)
	if !validation.Valid {
		return Result{Errors: validation.Errors}
	}
	if env == nil {
		return Result{Errors: []string{"environment is required"}}
	}
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 256
	}

	obs := env.Reset(0)
	result := Result{Valid: true, Final: obs}
	if len(candidate.Actions) == 0 {
		result.Errors = []string{"action plan is empty"}
		result.Valid = false
		return result
	}

	for result.Steps < cfg.MaxSteps {
		select {
		case <-ctx.Done():
			result.Errors = append(result.Errors, ctx.Err().Error())
			return result
		default:
		}
		action := candidate.Actions[result.Steps%len(candidate.Actions)]
		next, reward, done := env.Step(action)
		result.TotalReward += float64(reward)
		result.Steps++
		result.Final = next
		if done {
			return result
		}
	}
	result.Errors = append(result.Errors, fmt.Sprintf("sandbox step limit %d reached", cfg.MaxSteps))
	result.Valid = false
	return result
}

var ErrExecutionDisabled = errors.New("arbitrary generated source execution is disabled")

// ExecuteSource is intentionally unavailable. Candidate.Source must not be
// treated as trusted code by callers.
func ExecuteSource(string) error { return ErrExecutionDisabled }
