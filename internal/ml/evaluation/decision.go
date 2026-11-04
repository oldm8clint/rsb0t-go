package evaluation

// Decision describes the outcome of an evaluation pipeline without conflating
// invalid evidence/configuration with a legitimate candidate rejection.
type Decision string

const (
	DecisionPass    Decision = "PASS"
	DecisionReject  Decision = "REJECT"
	DecisionInvalid Decision = "INVALID"
)

// DecideEvidence validates and evaluates evidence gates. Invalid evidence or
// configuration is INVALID; valid evidence that fails a gate is REJECT.
func DecideEvidence(episodes []EpisodeEvidence, cfg EvidenceGateConfig, report Report) Decision {
	if err := ValidateEvidence(episodes); err != nil {
		return DecisionInvalid
	}
	if err := ValidateEvidenceGateConfig(cfg); err != nil {
		return DecisionInvalid
	}

	metrics := aggregateValidatedEvidence(episodes)
	report = ApplyEvidenceGates(report, metrics, cfg)
	if !report.PassesGate {
		return DecisionReject
	}
	return DecisionPass
}
