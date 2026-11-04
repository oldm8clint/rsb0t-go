package evaluation

import (
	"fmt"

	"rsb0t-go/internal/ml/replay"
)

// EpisodeStatus describes the terminal outcome recorded by an evaluation
// harness. Recovery is represented by RecoveryCount rather than as a terminal
// status, so a successful episode that needed recovery still counts as
// completed.
type EpisodeStatus string

const (
	StatusCompleted EpisodeStatus = "completed"
	StatusFailed    EpisodeStatus = "failed"
)

// EpisodeEvidence attaches deterministic outcome metadata to a replay.
// RecoveryCount is the number of recovery transitions observed before the
// terminal outcome. A zero value is meaningful and is not treated as missing.
type EpisodeEvidence struct {
	Episode       *replay.Episode `json:"episode"`
	Status        EpisodeStatus   `json:"status"`
	Failure       string          `json:"failure,omitempty"`
	RecoveryCount int             `json:"recovery_count"`
}

// ValidateEvidence checks the evidence fields that are independent of a
// particular environment's success/failure semantics. Environment-specific
// rules should be enforced by the harness that produces the evidence.
func ValidateEvidence(evidence []EpisodeEvidence) error {
	if len(evidence) == 0 {
		return fmt.Errorf("evaluation requires at least one evidence record")
	}
	for i, item := range evidence {
		if item.Episode == nil {
			return fmt.Errorf("evidence %d has nil episode", i)
		}
		if item.RecoveryCount < 0 {
			return fmt.Errorf("evidence %d has negative recovery count", i)
		}
		switch item.Status {
		case StatusCompleted, StatusFailed:
		default:
			return fmt.Errorf("evidence %d has unknown status %q", i, item.Status)
		}
		if item.Status == StatusFailed && item.Failure == "" {
			return fmt.Errorf("evidence %d is failed but has no failure reason", i)
		}
		if item.Status != StatusFailed && item.Failure != "" {
			return fmt.Errorf("evidence %d has failure reason for non-failed status", i)
		}
	}
	return nil
}
