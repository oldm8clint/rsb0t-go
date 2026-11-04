package controlpanel

// Positions implements PositionSource from the live fleet: one gamestate lock per connected
// session (fleet.Manager.Positions), never a whole-fleet status snapshot. A FleetSource
// built without a manager (tests) reads positions out of its coalesced snapshot instead.
func (f *FleetSource) Positions(dst []AccountPosition) []AccountPosition {
	dst = dst[:0]
	if f.manager == nil {
		if f.snapshot == nil {
			return dst
		}
		for _, a := range f.cachedSnapshot() {
			if a.Connected && a.Pos != nil {
				dst = append(dst, AccountPosition{Name: a.Username, X: a.Pos.X, Z: a.Pos.Z, Level: a.Pos.Level})
			}
		}
		return dst
	}
	for _, p := range f.manager.Positions(nil) {
		dst = append(dst, AccountPosition{Name: p.Username, X: p.X, Z: p.Z, Level: p.Level})
	}
	return dst
}

// Positions implements PositionSource for the simulated fleet: every in-game fake account's
// SYNTHETIC random-walk tile.
func (f *FakeSource) Positions(dst []AccountPosition) []AccountPosition {
	f.mu.Lock()
	defer f.mu.Unlock()
	dst = dst[:0]
	for _, a := range f.accounts {
		if a.State == StateIngame && a.Pos != nil {
			dst = append(dst, AccountPosition{Name: a.Name, X: a.Pos.X, Z: a.Pos.Z, Level: a.Pos.Level})
		}
	}
	return dst
}
