package combatplan

import (
	"strings"
	"testing"
)

// A shop listing an item is not the same as a shop having one.
//
// This table records which shop stocks what, read from each shop's own .inv, but those lines
// carry a stock COUNT as well and several are zero. Falador's shield shop is the case that
// found it: `stock8=mithril_sq_shield,0,1500` — Cassie lists the best shield this table can
// name for a Defence 20 account and keeps none of it, as she also keeps none of the steel
// shields. A run can only discover that in game, and what it does with the discovery is the
// difference between one more trip and walking to Falador for every tier in turn.
func TestBuyPlanExceptFallsBackToATierTheShelfActuallyHas(t *testing.T) {
	l := Levels{Attack: 20, Strength: 20, Defence: 20}

	shieldIn := func(plan []Purchase) Item {
		for _, p := range plan {
			if p.Slot == SlotShield {
				return p.Item
			}
		}
		return Item{}
	}

	first := shieldIn(BuyPlan(l, nil, nil, nil))
	if first.Name != "Mithril sq shield" {
		t.Fatalf("shield slot with nothing excluded: got %q, want Mithril sq shield", first.Name)
	}

	// What the run learns at Cassie's counter in one trip: the mithril and steel shields are
	// listed and absent, so they are struck together rather than one wasted walk at a time.
	empty := map[string]bool{
		"mithril sq shield": true,
		"steel sq shield":   true,
		"steel kiteshield":  true,
		"iron kiteshield":   true,
	}
	next := shieldIn(BuyPlanExcept(l, nil, nil, nil, empty))
	if next.Name == "" {
		t.Fatal("shield slot after striking the empty shelves: got nothing, want the best tier still stocked")
	}
	if empty[strings.ToLower(next.Name)] {
		t.Fatalf("shield slot: %q was struck as unavailable and came back anyway", next.Name)
	}
	if next.Level > l.Defence {
		t.Fatalf("fallback shield %q needs Defence %d, above the account's %d", next.Name, next.Level, l.Defence)
	}
	// Bronze rather than iron, and that is the table's documented tie-break rather than an
	// accident: both need Defence 1, and BestWearable resolves a tie to the first in table
	// order, which is the cheaper metal. Cassie keeps three of these, so it is also a shield
	// the run can actually walk out with.
	if next.Name != "Bronze sq shield" {
		t.Fatalf("fallback shield: got %q, want Bronze sq shield (bronze and iron both need Defence 1, and the tie goes to the first in table order)", next.Name)
	}
}

// A nil exclusion set must change nothing at all, so a run that has learned nothing behaves
// exactly as it did before this existed.
func TestBuyPlanExceptWithNothingExcludedMatchesBuyPlan(t *testing.T) {
	l := Levels{Attack: 30, Strength: 30, Defence: 30}
	a := BuyPlan(l, nil, nil, nil)
	b := BuyPlanExcept(l, nil, nil, nil, nil)
	if len(a) != len(b) {
		t.Fatalf("BuyPlan gave %d purchases, BuyPlanExcept with a nil set gave %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Item.Name != b[i].Item.Name || a[i].Slot != b[i].Slot {
			t.Fatalf("purchase %d: BuyPlan %s/%s, BuyPlanExcept %s/%s", i, a[i].Slot, a[i].Item.Name, b[i].Slot, b[i].Item.Name)
		}
	}
}
