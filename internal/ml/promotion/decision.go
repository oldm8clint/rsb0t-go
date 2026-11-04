package promotion

import "rsb0t-go/internal/ml/evaluation"

// Decision is the immutable result of the offline promotion gate.
type Decision struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

const (
	Promote = "promote"
	Reject  = "reject"
)

// Decide converts an evaluation report into an explicit promotion decision.
// Promotion remains an offline state transition; it never executes a client
// script or changes the live runtime.
func Decide(report evaluation.Report) Decision {
	if report.PassesGate {
		return Decision{Status: Promote, Reason: "candidate passed evaluation gates"}
	}
	if len(report.Reasons) > 0 {
		return Decision{Status: Reject, Reason: report.Reasons[0]}
	}
	return Decision{Status: Reject, Reason: "candidate failed evaluation gate"}
}
