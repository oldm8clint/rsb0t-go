package fleetplan

// TrainRoundReady reports whether the runes held make a training round worth walking to the pen
// for: at least MinTrainCasts casts, or every cast still needed to the Magic target when that is
// fewer. An account already part-way through a round (Training) finishes it; MinTrainCasts 0
// keeps the old any-cast rule.
func TrainRoundReady(s Stage3Snapshot) bool {
	if s.TrainCasts <= 0 {
		return false
	}
	if s.Training || s.MinTrainCasts <= 0 {
		return true
	}
	need := s.MinTrainCasts
	if s.CastsToTarget > 0 && s.CastsToTarget < need {
		need = s.CastsToTarget
	}
	return s.TrainCasts >= need
}
