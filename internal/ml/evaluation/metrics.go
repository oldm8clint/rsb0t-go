package evaluation

// EvidenceMetrics contains aggregate outcome metrics derived from validated
// episode evidence. Rates are fractions in the range [0,1].
type EvidenceMetrics struct {
	Episodes        int     `json:"episodes"`
	Completed       int     `json:"completed"`
	Failed          int     `json:"failed"`
	RecoveryCount   int     `json:"recovery_count"`
	CompletionRate  float64 `json:"completion_rate"`
	FailureRate     float64 `json:"failure_rate"`
	RecoveryRate    float64 `json:"recovery_rate"`
	AverageRecovery float64 `json:"average_recovery"`
}

// AggregateEvidence validates and aggregates deterministic episode outcomes.
// A failed validation returns no metrics so callers cannot accidentally treat
// malformed evidence as a zero-valued evaluation.
func AggregateEvidence(evidence []EpisodeEvidence) (EvidenceMetrics, error) {
	if err := ValidateEvidence(evidence); err != nil {
		return EvidenceMetrics{}, err
	}
	return aggregateValidatedEvidence(evidence), nil
}

// aggregateValidatedEvidence assumes the evidence has already passed
// ValidateEvidence. Keeping this internal avoids validating the same records
// twice in the decision path while preserving validation for public callers.
func aggregateValidatedEvidence(evidence []EpisodeEvidence) EvidenceMetrics {
	metrics := EvidenceMetrics{Episodes: len(evidence)}
	for _, item := range evidence {
		metrics.RecoveryCount += item.RecoveryCount
		switch item.Status {
		case StatusCompleted:
			metrics.Completed++
		case StatusFailed:
			metrics.Failed++
		}
	}

	count := float64(metrics.Episodes)
	metrics.CompletionRate = float64(metrics.Completed) / count
	metrics.FailureRate = float64(metrics.Failed) / count
	metrics.RecoveryRate = float64(metrics.RecoveryCount) / count
	metrics.AverageRecovery = float64(metrics.RecoveryCount) / count
	return metrics
}
