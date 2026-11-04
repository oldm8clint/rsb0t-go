package fleetplan

// WithoutMule is the snapshot as an account that never asks the mule must see it: no ask due, and
// nothing the mule holds to wait for. Every DecideStage3Phase branch that answers Phase3Mule, or
// Phase3Wait for a mule's runes or staff, then falls through to the shops, the market (when the mode
// allows it), thieving for coins and chaos, the food phase or Phase3Hungry's stall -- the account's
// own supply. CoinsShort only ever routes to the mule, so it goes too.
func (s Stage3Snapshot) WithoutMule() Stage3Snapshot {
	s.MuleAskDue = false
	s.MuleFoodAskDue = false
	s.MuleHoldsWant = false
	s.MuleHoldsStaff = false
	s.CoinsShort = false
	return s
}
