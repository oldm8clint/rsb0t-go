package controlpanel

import (
	"testing"
	"time"

	"rsb0t-go/internal/loot"
)

// Netting a source's input off its earnings, as /api/loot delivers it (owner, 2026-09-21: "loot
// panel should consider the cost of a log or any other fletching material when its putting a
// profit price or gp earned through fletching", and "crystal chest loot total should consider it
// costs 1 crystal key to open the chest").

func priced(rows map[string]MarketPrice) *PriceBook { return &PriceBook{prices: rows} }

func chestOpens(opens int64, items ...loot.Item) loot.Totals {
	return loot.Totals{Source: loot.CrystalChest(), Events: opens, Items: items}
}

// TestFletchingProfitIsNetOfTheLogsItCut.
//
// The case that makes the rule obvious: a log is worth more than the fifteen shafts it becomes.
// 100 logs at 80 gp cut into 1,500 shafts at 1 gp is 1,500 gross and MINUS 6,500 net, and a panel
// reporting 1,500 gp "earned through fletching" would be reporting a loss as a profit.
func TestFletchingProfitIsNetOfTheLogsItCut(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	a.loot.prices = priced(map[string]MarketPrice{
		"logs":        {Name: "Logs", Coins: 80, Trades: 9, Units: 4000},
		"arrow shaft": {Name: "Arrow shaft", Coins: 1, Trades: 40, Units: 90000},
	})

	a.loot.record(lootReading("camper1", 1, loot.Totals{
		Source: loot.Fletching(nil, 1511, "Logs"),
		Events: 100, // 100 logs cut
		Items:  []loot.Item{{ID: loot.ObjArrowShaft, Name: "Arrow shaft", Count: 1500, Value: 1}},
	}), base)

	lifetime, _, _, _ := a.loot.body()
	if lifetime.Gross != 1500 {
		t.Errorf("gross = %d, want the 1500 shafts at 1 gp", lifetime.Gross)
	}
	if lifetime.Cost != 8000 {
		t.Errorf("cost = %d, want 100 logs at 80 gp", lifetime.Cost)
	}
	if lifetime.Value != -6500 {
		t.Errorf("net value = %d, want 1500 - 8000; cutting these logs LOST money", lifetime.Value)
	}
	if len(lifetime.Accounts) != 1 || lifetime.Accounts[0].Value != -6500 || lifetime.Accounts[0].Gross != 1500 {
		t.Errorf("per-account view = %+v, want the same netting", lifetime.Accounts)
	}

	if len(lifetime.Sources) != 1 {
		t.Fatalf("got %d sources, want 1", len(lifetime.Sources))
	}
	s := lifetime.Sources[0]
	if !s.CostKnown || s.Cost != 8000 || s.CostUnits != 100 || s.CostItem != "Logs" {
		t.Errorf("source cost = %+v, want 100 known Logs at 8000 gp", s)
	}
}

// TestACrystalChestOpenCostsAKey. One open, one key, whatever the chest gave back.
func TestACrystalChestOpenCostsAKey(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	a.loot.prices = priced(map[string]MarketPrice{
		"crystal key":         {Name: "Crystal key", Coins: 5000, Trades: 12, Units: 300},
		"uncut ruby":          {Name: "Uncut ruby", Coins: 2800, Trades: 30, Units: 900},
		"tooth half of a key": {Name: "Tooth half of a key", Coins: 1200, Trades: 8, Units: 140},
	})

	a.loot.record(lootReading("chester", 1, chestOpens(10,
		loot.Item{ID: 1619, Name: "Uncut ruby", Count: 3, Value: 100},
		loot.Item{ID: 985, Name: "Tooth half of a key", Count: 4, Value: 1},
	)), base)

	lifetime, _, _, _ := a.loot.body()
	// 3 x 2800 + 4 x 1200 = 13,200 gross; 10 keys at 5,000 = 50,000 spent.
	if lifetime.Gross != 13200 {
		t.Errorf("gross = %d, want 13200", lifetime.Gross)
	}
	if lifetime.Cost != 50000 {
		t.Errorf("cost = %d, want 10 crystal keys at 5000", lifetime.Cost)
	}
	if lifetime.Value != -36800 {
		t.Errorf("net = %d, want 13200 - 50000", lifetime.Value)
	}
	// The key halves the chest paid out are still counted as loot, and must be: the chest really
	// gave them. Netting the KEY off is what stops the loop (halves -> key -> chest -> halves)
	// reading as free money; removing the halves as well would subtract the same thing twice.
	s := lifetime.Sources[0]
	var halves int64
	for _, it := range s.Items {
		if it.ID == 985 {
			halves = it.Count
		}
	}
	if halves != 4 {
		t.Errorf("key halves on the chest card = %d, want the 4 it actually paid out", halves)
	}
}

// TestAnUnpriceableInputIsReportedUnknownAndNotSubtracted.
//
// marketprice.go's own first rule, and it matters more for a cost than for a value: treating an
// unknown input price as zero would claim the input was free and inflate the very profit figure
// this is supposed to correct. A Crystal key is exactly the item that can hit it — its trades can
// fail MarketPrice.Coherent, and the book then declines to answer.
func TestAnUnpriceableInputIsReportedUnknownAndNotSubtracted(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	a.loot.prices = priced(map[string]MarketPrice{
		// A p75 more than ten times the p25: incoherent, so Value declines it.
		"crystal key": {Name: "Crystal key", Coins: 5000, Trades: 12, Units: 300, Low: 200, High: 90000},
		"uncut ruby":  {Name: "Uncut ruby", Coins: 2800, Trades: 30, Units: 900},
	})

	a.loot.record(lootReading("chester", 1, chestOpens(10,
		loot.Item{ID: 1619, Name: "Uncut ruby", Count: 3, Value: 100},
	)), base)

	lifetime, _, _, _ := a.loot.body()
	if lifetime.Cost != 0 {
		t.Errorf("cost = %d; an unpriceable input must not be subtracted at a guess", lifetime.Cost)
	}
	if lifetime.Value != lifetime.Gross {
		t.Errorf("net %d != gross %d; with nothing defensible to subtract they are the same figure",
			lifetime.Value, lifetime.Gross)
	}
	s := lifetime.Sources[0]
	if s.CostKnown {
		t.Error("CostKnown is true for a price the book declined")
	}
	// ...but the panel must still be able to SAY a key was spent, which is the difference
	// between "unknown" and "free".
	if s.CostItem != loot.CrystalKeyName || s.CostUnits != 10 {
		t.Errorf("unknown cost lost its units: %q x%d, want %q x10", s.CostItem, s.CostUnits, loot.CrystalKeyName)
	}
	if s.Cost != 0 {
		t.Errorf("Cost = %d, want 0 alongside CostKnown false", s.Cost)
	}
}

// TestASingleUncorroboratedTradeCannotPriceACost.
//
// The other half of the same guard, and the one that actually bit while this was being written.
// PriceBook.Value keeps a last-resort branch for an item with no cache cost: one trade is better
// than calling it worthless. Every cost asks with a cache cost of zero — it has no loot.Item to
// take one from — so that branch is armed for every input, and it fires for exactly the prices
// the book's own confidence gate had just rejected.
func TestASingleUncorroboratedTradeCannotPriceACost(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	a.loot.prices = priced(map[string]MarketPrice{
		// One trade: below minConfidentTrades, so Value would only answer it through the
		// zero-cache-cost escape hatch.
		"logs":        {Name: "Logs", Coins: 9000, Trades: 1, Units: 1},
		"arrow shaft": {Name: "Arrow shaft", Coins: 1, Trades: 40, Units: 90000},
	})

	a.loot.record(lootReading("camper1", 1, loot.Totals{
		Source: loot.Fletching(nil, 1511, "Logs"),
		Events: 100,
		Items:  []loot.Item{{ID: loot.ObjArrowShaft, Name: "Arrow shaft", Count: 1500, Value: 1}},
	}), base)

	lifetime, _, _, _ := a.loot.body()
	if lifetime.Cost != 0 {
		t.Errorf("cost = %d; one trade of one log at 9000 gp is not evidence enough to charge 900,000 against the fleet", lifetime.Cost)
	}
	if s := lifetime.Sources[0]; s.CostKnown {
		t.Error("CostKnown is true on a single uncorroborated trade")
	}
}

// TestAKeyPricedFromItsHalvesStillCostsTheChest.
//
// The Crystal key has never traded on this market at all, so its own record is absent and the
// only price it gets is ValueFromParts — two half-of-a-key at the halves' own median
// (marketprice.go's madeOf). That path must be TRUSTED by the cost side: madeOf already applies
// the confidence and coherence gates to the component, and re-checking the key's own missing
// record would make every chest open unpriceable and quietly stop netting the keys off.
//
// Pinned because the two changes met in mid-air: the parts pricing landed while this netting was
// being written, and nothing else asserts they compose.
func TestAKeyPricedFromItsHalvesStillCostsTheChest(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	a.loot.prices = priced(map[string]MarketPrice{
		// No "crystal key" row at all — exactly the live situation.
		"half of a key": {Name: "Half of a key", Coins: 120000, Trades: 13, Units: 26, Low: 115000, High: 125000},
		"uncut ruby":    {Name: "Uncut ruby", Coins: 2800, Trades: 30, Units: 900},
	})

	a.loot.record(lootReading("chester", 1, chestOpens(4,
		loot.Item{ID: 1619, Name: "Uncut ruby", Count: 2, Value: 100},
	)), base)

	lifetime, _, _, _ := a.loot.body()
	// Two halves make a key: 240,000 each, four opens.
	if lifetime.Cost != 960000 {
		t.Errorf("cost = %d, want 4 keys at 2 x 120,000", lifetime.Cost)
	}
	if s := lifetime.Sources[0]; !s.CostKnown {
		t.Error("CostKnown is false for a key the book prices from its halves")
	}
}

// TestSourcesThatConsumeNothingAreUnchanged: the netting must not touch the fleet's main
// activity. Fishing, kills and pickpockets have no input to subtract, so net must equal gross.
func TestSourcesThatConsumeNothingAreUnchanged(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	a.loot.prices = priced(map[string]MarketPrice{
		"raw lobster": {Name: "Raw lobster", Coins: 200, Trades: 22, Units: 281692},
	})

	a.loot.record(lootReading("fisher1", 1, loot.Totals{
		Source: loot.Fishing(nil, 316),
		Events: 50,
		Items:  []loot.Item{{ID: 377, Name: "Raw lobster", Count: 50, Value: 20}},
	}), base)

	lifetime, _, _, _ := a.loot.body()
	if lifetime.Cost != 0 || lifetime.Value != lifetime.Gross || lifetime.Value != 10000 {
		t.Errorf("fishing netted something: gross %d cost %d net %d, want 10000/0/10000",
			lifetime.Gross, lifetime.Cost, lifetime.Value)
	}
	if s := lifetime.Sources[0]; s.CostItem != "" || s.CostUnits != 0 || s.CostKnown {
		t.Errorf("fishing source carries a cost: %+v", s)
	}
}
