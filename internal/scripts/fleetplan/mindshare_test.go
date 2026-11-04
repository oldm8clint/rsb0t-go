package fleetplan

func cohortStage3() Stage3Settings {
	st := DefaultStage3Settings()
	st.Spell, st.MagicTarget, st.CastsTarget = "Wind Bolt", 17, 150
	st.Staff = StaffOfAir
	return st
}
