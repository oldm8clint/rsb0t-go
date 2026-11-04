package fleetplan

import "testing"

func TestBulkFoodAskTakesTheDeepestStockInFull(t *testing.T) {
	bank := map[string]int{"Cake": 25816, "Bread": 77138, "Shrimps": 2251}
	stock := func(item string) (int, bool) { n, ok := bank[item]; return n, ok }
	line := AskLine{Choices: []string{"Cake", "Bread", "Shrimps"}, Count: 1000, FairShare: true, Bulk: true}
	plan, none := FilterAskByBank([]AskLine{line}, stock)
	if len(none) != 0 || len(plan) != 1 || plan[0].Rungs[0] != (ItemCount{Item: "Bread", Count: 1000}) {
		t.Fatalf("bulk plan = %+v none %v, want 1000 Bread", plan, none)
	}
	// Capped at the stock less its reserve.
	bank = map[string]int{"Cake": 500}
	plan, _ = FilterAskByBank([]AskLine{line}, stock)
	if got := plan[0].Rungs[0].Count; got != 450 {
		t.Fatalf("capped bulk = %d, want 450 (500 less a tenth)", got)
	}
	// Control: the same line without Bulk keeps the fair share of the first rung held.
	bank = map[string]int{"Cake": 25816, "Bread": 77138}
	line.Bulk = false
	plan, _ = FilterAskByBank([]AskLine{line}, stock)
	if r := plan[0].Rungs[0]; r.Item != "Cake" || r.Count >= 1000 {
		t.Fatalf("fair-share control = %+v, want a share of Cake under 1000", r)
	}
}
