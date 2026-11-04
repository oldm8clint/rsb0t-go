package evaluation

// EvaluateEvidence runs the evidence portion of the evaluation pipeline and
// returns an explicit PASS, REJECT, or INVALID decision. The caller supplies
// the already-computed reward/step report so existing evaluator semantics stay
// unchanged.
func EvaluateEvidence(episodes []EpisodeEvidence, cfg EvidenceGateConfig, report Report) Decision {
	return DecideEvidence(episodes, cfg, report)
}
