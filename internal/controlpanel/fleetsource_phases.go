package controlpanel

// Phases implements PhaseSource from the live fleet (fleet.Manager.Phases: each account's own
// lock once, no gamestate read). A FleetSource built without a manager (tests) reports none.
func (f *FleetSource) Phases(dst []AccountPhase) []AccountPhase {
	if f.manager == nil {
		return dst[:0]
	}
	return f.manager.Phases(dst)
}

// MuleStatuses implements MuleSource from the live fleet: every running MuleReceiver's
// published status (fleet.Manager.MuleStatuses).
func (f *FleetSource) MuleStatuses() []MuleStatus {
	if f.manager == nil {
		return nil
	}
	return f.manager.MuleStatuses()
}
