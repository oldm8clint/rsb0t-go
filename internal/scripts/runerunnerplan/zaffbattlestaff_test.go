package runerunnerplan

import "testing"

// A full members' shelf of five Battlestaffs costs 7000, 7140, 7280, 7420 and 7560 (Delta 20 on cost 7000), so
// the default cap takes all five and a cap of 7300 takes three.
func TestZaffSellsAFullShelfOfBattlestaffs(t *testing.T) {
	if n, cost := PlanZaff(Battlestaff, 5, 5, DefaultZaffBattlestaffCap, 1_000_000); n != 5 || cost != 36400 {
		t.Fatalf("full shelf: %d for %d, want 5 for 36400", n, cost)
	}
	if n, _ := PlanZaff(Battlestaff, 5, 5, 7300, 1_000_000); n != 3 {
		t.Fatalf("cap 7300: %d, want 3", n)
	}
	if n, _ := PlanZaff(Battlestaff, 5, 5, DefaultZaffBattlestaffCap, 14_000); n != 1 {
		t.Fatalf("14,000 coins: %d, want 1", n)
	}
	if got := ZaffEstCost(Battlestaff, 5, DefaultZaffBattlestaffCap); got != 36400 {
		t.Fatalf("estimate %d, want 36400", got)
	}
	s := DefaultSettings()
	if s.ZaffBattlestaffs != 0 {
		t.Fatalf("Battlestaffs are bought by default (%d)", s.ZaffBattlestaffs)
	}
}
