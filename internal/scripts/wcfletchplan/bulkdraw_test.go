package wcfletchplan

import "testing"

func TestBulkDrawDue(t *testing.T) {
	due := BulkSnapshot{Fletching: true, Stacking: true, BankedLogs: 262, BankReadIsThisRung: true, NearBank: true}
	if !BulkDrawDue(due) {
		t.Fatal("262 plain logs still banked, the last draw moved, knife in hand, at the bank: the account must stay for the next draw")
	}
	for name, mut := range map[string]func(*BulkSnapshot){
		// Without a knife the draw would be logs carried to the trees uncut.
		"not fletching": func(s *BulkSnapshot) { s.Fletching = false },
		// A bow rung comes back on its own: a cut pack of bows is a full pack.
		"product does not stack": func(s *BulkSnapshot) { s.Stacking = false },
		"bank empty":             func(s *BulkSnapshot) { s.BankedLogs = 0 },
		// The count was for another species, read before a rung change.
		"stale read from another rung": func(s *BulkSnapshot) { s.BankReadIsThisRung = false },
		// A sequence broken off across the map (a random event's teleport) is not chased back.
		"away from the bank": func(s *BulkSnapshot) { s.NearBank = false },
		// Draws that keep moving nothing must end the run, or a refused withdrawal loops.
		"too many failed draws in a row": func(s *BulkSnapshot) { s.DrawFails = MaxBulkDrawFails },
	} {
		s := due
		mut(&s)
		if BulkDrawDue(s) {
			t.Errorf("%s: BulkDrawDue = true, want false", name)
		}
	}
	// One failed draw -- a random event interrupting the bank, say -- is not the end of it.
	for fails := 1; fails < MaxBulkDrawFails; fails++ {
		s := due
		s.DrawFails = fails
		if !BulkDrawDue(s) {
			t.Errorf("%d failed draw(s) in a row ended the sequence; it should retry up to %d", fails, MaxBulkDrawFails)
		}
	}
}
