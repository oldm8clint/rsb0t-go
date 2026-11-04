package evaluation

import (
	"fmt"
	"math"
)

// ValidateEvidenceGateConfig rejects NaN thresholds, which otherwise make all
// comparisons false and can turn a configuration typo into an unexplained gate
// rejection. Infinite thresholds remain valid: +Inf can intentionally disable a
// maximum, while -Inf can intentionally make a minimum impossible.
func ValidateEvidenceGateConfig(cfg EvidenceGateConfig) error {
	if math.IsNaN(cfg.MinCompletionRate) {
		return fmt.Errorf("min completion rate is NaN")
	}
	// Unlike MaxFailureRate, a negative MinCompletionRate has no documented
	// "disable this gate" meaning (zero already is that sentinel — see
	// EvidenceGateConfig's own doc comment), so treat it as a configuration
	// mistake rather than silently accepting every completion rate.
	if cfg.MinCompletionRate < 0 {
		return fmt.Errorf("min completion rate is negative")
	}
	if math.IsNaN(cfg.MaxFailureRate) {
		return fmt.Errorf("max failure rate is NaN")
	}
	if math.IsNaN(cfg.MaxAverageRecovery) {
		return fmt.Errorf("max average recovery is NaN")
	}
	return nil
}
