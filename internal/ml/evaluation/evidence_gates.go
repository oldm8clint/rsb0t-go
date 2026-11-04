package evaluation

import "fmt"

// EvidenceGateConfig defines optional outcome-quality limits. MinCompletionRate
// and MaxAverageRecovery use zero as their disabled value. MaxFailureRate is
// the exception: zero means no failures are allowed; use a negative value to
// disable that gate.
type EvidenceGateConfig struct {
	MinCompletionRate  float64
	MaxFailureRate     float64
	MaxAverageRecovery float64
}

// ApplyEvidenceGates evaluates already-validated aggregate evidence. It does
// not alter reward/step gates and returns a copy of the report with evidence
// reasons appended. Callers should validate/aggregate evidence before calling
// this function.
func ApplyEvidenceGates(report Report, metrics EvidenceMetrics, cfg EvidenceGateConfig) Report {
	passCompletion := metrics.CompletionRate >= cfg.MinCompletionRate
	passFailure := cfg.MaxFailureRate < 0 || metrics.FailureRate <= cfg.MaxFailureRate
	passRecovery := cfg.MaxAverageRecovery <= 0 || metrics.AverageRecovery <= cfg.MaxAverageRecovery

	if !passCompletion || !passFailure || !passRecovery {
		report.Reasons = append([]string(nil), report.Reasons...)
	}
	if !passCompletion {
		report.Reasons = append(report.Reasons, fmt.Sprintf("completion rate %.2f%% is below required %.2f%%", metrics.CompletionRate*100, cfg.MinCompletionRate*100))
	}
	if !passFailure {
		report.Reasons = append(report.Reasons, fmt.Sprintf("failure rate %.2f%% exceeds allowed %.2f%%", metrics.FailureRate*100, cfg.MaxFailureRate*100))
	}
	if !passRecovery {
		report.Reasons = append(report.Reasons, fmt.Sprintf("average recoveries %.4f exceeds allowed %.4f", metrics.AverageRecovery, cfg.MaxAverageRecovery))
	}

	report.PassesGate = report.PassesGate && passCompletion && passFailure && passRecovery
	return report
}
