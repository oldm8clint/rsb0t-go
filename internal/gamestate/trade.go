package gamestate

func tradeModal(id int) bool { return id == 3323 || id == 3443 }

// Clear only when leaving trade: inventory packets can precede opening the
// interface, and the offer-to-confirm transition must preserve fresh packets.
// Caller holds the state lock. Bank and backpack snapshots remain intact.
func (s *State) clearTradeInventories() {
	for _, id := range []int{3415, 3416, 3542, 3538, 3532, 3539} {
		if inv := s.inventories[id]; inv != nil {
			inv.Slots = nil
			inv.Generation++
			inv.FullGeneration = 0
		}
	}
}
