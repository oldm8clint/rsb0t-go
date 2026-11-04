package fleetrunner

import (
	"time"

	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

// WithdrawCoinsTo tops the pack's coins up to want from the bank at bankTile, as far as the bank
// holds them. Returns the coins carried afterwards, and a reason when the bank could not be opened.
func WithdrawCoinsTo(d *Deps, bankTile tile.Tile, want int) (int, string) {
	if CoinsHeld(d) >= want {
		return CoinsHeld(d), ""
	}
	bank, ok := OpenBank(d, bankTile)
	if !ok {
		return CoinsHeld(d), "could not open the bank"
	}
	for _, it := range bank.Items() {
		if it.ID == CoinsID {
			m := want - CoinsHeld(d)
			if int(it.Count) < m {
				m = int(it.Count)
			}
			if m > 0 {
				_ = bank.QuickWithdrawX(CoinsID, int32(m), thieverrunner.Waiter(Thiev(d)))
				WaitFor(d, "coins", 3*time.Second, func() bool { return CoinsHeld(d) > 0 })
			}
			break
		}
	}
	_ = bank.Close()
	d.Delay(600 * time.Millisecond)
	return CoinsHeld(d), ""
}
