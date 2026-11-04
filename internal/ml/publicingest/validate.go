package publicingest

import "fmt"

// Validate rejects malformed or unsafe public lessons before they enter the
// private evidence store. The public project sends measurements, never code.
func Validate(l Lesson) error {
	if l.SchemaVersion != "v1" {
		return fmt.Errorf("unsupported schema version %q", l.SchemaVersion)
	}
	if l.Source == "" || l.ExperimentID == "" || l.StrategyID == "" {
		return fmt.Errorf("missing provenance")
	}
	if l.Episodes <= 0 {
		return fmt.Errorf("episodes must be positive")
	}
	if l.Metrics.SuccessRate < 0 || l.Metrics.SuccessRate > 1 {
		return fmt.Errorf("success rate out of range")
	}
	if l.Metrics.MeanSteps < 0 {
		return fmt.Errorf("mean steps cannot be negative")
	}
	for _, e := range l.Evidence {
		if e.Claim == "" || e.SampleSize <= 0 || e.Confidence < 0 || e.Confidence > 1 {
			return fmt.Errorf("invalid evidence")
		}
	}
	return nil
}
