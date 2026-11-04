package controlpanel

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/loot"
)

// The loot maths audit (7 Oct 2026, owner: "ensure all maths on the loot panel is correct and we arent
// doubling up on anything being tracked going in and out"). Each test is one double count found in the
// live /api/loot of build 46a4ace1, built from the live shape and the live prices.

var auditBase = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// auditBook is a price book of confident, coherent market prices for the named items.
func auditBook(rows ...MarketPrice) *PriceBook {
	m := map[string]MarketPrice{}
	for _, r := range rows {
		r.Trades, r.Units = 9, 900
		m[strings.ToLower(r.Name)] = r
	}
	return priced(m)
}

// TestACasketIsNotValuedOnTopOfWhatItPaid: the Big net brings caskets up (counted as items on the
// Fishing card at the market's 1,000 gp each) and every one is opened for its rewards (counted on the
// Casket card), so the fleet's value held the casket and its contents. A casket shows on its card, with
// its count, and is worth nothing as an item.
func TestACasketIsNotValuedOnTopOfWhatItPaid(t *testing.T) {
	a := newStatsAggregator("", nil, fixedNow(auditBase))
	a.loot.prices = auditBook(MarketPrice{Name: "Casket", Coins: 1000}, MarketPrice{Name: "Raw bass", Coins: 70})
	a.loot.record(lootReading("fisher1", 1,
		loot.Totals{Source: loot.Fishing(nil, 316), Events: 20, Items: []loot.Item{
			{ID: 405, Name: "Casket", Count: 10, Value: 1},
			{ID: 363, Name: "Raw bass", Count: 20, Value: 5},
		}},
		casketTotals(10, 3000, 0),
	), auditBase)

	p, _, _, _ := a.loot.body()
	// 20 bass at 70 and 3,000 coins from the opens. The ten caskets are counted nowhere.
	if p.Gross != 20*70+3000 {
		t.Errorf("gross = %d, want %d: the caskets must not be valued as well as what they paid", p.Gross, 20*70+3000)
	}
	shown := false
	for _, s := range p.Sources {
		for _, it := range s.Items {
			if it.ID == 405 {
				shown = true
				if it.Value != 0 || it.Count != 10 {
					t.Errorf("casket item on %s = %+v, want 10 shown at no value", s.Source.Name, it)
				}
			}
		}
	}
	if !shown {
		t.Error("the casket count is gone from the Fishing card; it should still show")
	}
}

// TestStringingABowNetsTheBowAndTheString: an unstrung bow and the strung one share a name, so the cut
// is credited at the STRUNG bow's price, and the stringing row (whose input was left unpriceable on
// purpose) then added the strung bow again with no cost at all: 90,539 Yew longbows were counted
// 44.9 million gp on the cut and 39.6 million again on the string, and every Bow string went unnetted.
// A bow cut and strung is worth the bow less its logs and its string.
func TestStringingABowNetsTheBowAndTheString(t *testing.T) {
	a := newStatsAggregator("", nil, fixedNow(auditBase))
	a.loot.prices = auditBook(
		MarketPrice{Name: "Yew logs", Coins: 80},
		MarketPrice{Name: "Yew longbow", Coins: 437},
		MarketPrice{Name: "Bow string", Coins: 150},
	)
	a.loot.record(lootReading("crafter1", 1,
		loot.Totals{Source: loot.Fletching(nil, 1515, "Yew logs"), Events: 10,
			Items: []loot.Item{{ID: 66, Name: "Yew longbow", Count: 10, Value: 8}}},
		loot.Totals{Source: loot.Stringing(66, "Yew longbow"), Events: 10,
			Items: []loot.Item{{ID: 855, Name: "Yew longbow", Count: 10, Value: 8}}},
	), auditBase)

	p, _, _, _ := a.loot.body()
	if p.Gross != 2*10*437 {
		t.Errorf("gross = %d, want the bow credited on the cut and on the string (%d)", p.Gross, 2*10*437)
	}
	// Logs 800, the unstrung bows the string used 4,370 and the strings 1,500.
	if p.Cost != 800+10*437+10*150 {
		t.Errorf("cost = %d, want logs 800 + unstrung bows 4370 + bow strings 1500", p.Cost)
	}
	if want := int64(10*437 - 800 - 10*150); p.Value != want {
		t.Errorf("net = %d, want %d: one bow, less its logs and its string", p.Value, want)
	}
	for _, s := range p.Sources {
		if s.Source.Name == "Yew longbow (u)" && !s.CostKnown {
			t.Errorf("the stringing row's cost is unknown: %+v", s)
		}
	}
}

// TestAnInputThePanelRecordedArrivingIsNettedAtItsArrivalPrice: an oyster pearl is credited when the
// oyster is opened, at the price its cache cost gives it (the market has no confident trades of it),
// and was never debited when the chisel turned it into tips, because the strict cost rule only trusts a
// confident market. 231,495 pearls were counted again as 1.39 million tips (live, 15.5 million gp).
func TestAnInputThePanelRecordedArrivingIsNettedAtItsArrivalPrice(t *testing.T) {
	a := newStatsAggregator("", nil, fixedNow(auditBase))
	a.loot.prices = auditBook(MarketPrice{Name: "Pearl bolttips", Coins: 33})
	a.loot.record(lootReading("crafter1", 1,
		loot.Totals{Source: loot.Openable("Oyster", 407), Events: 10,
			Items: []loot.Item{{ID: 411, Name: "Oyster pearl", Count: 10, Value: 100}}},
		loot.Totals{Source: loot.Fletching(nil, 411, "Oyster pearl"), Events: 10,
			Items: []loot.Item{{ID: 46, Name: "Pearl bolttips", Count: 60, Value: 5}}},
	), auditBase)

	p, _, _, _ := a.loot.body()
	// Pearls: 10 at 60 (high alchemy of 100). Tips: 60 at 33. The pearls come off when they are cut.
	if p.Gross != 10*60+60*33 {
		t.Fatalf("gross = %d, want %d", p.Gross, 10*60+60*33)
	}
	if p.Cost != 10*60 {
		t.Errorf("cost = %d, want the 10 pearls netted at the 60 they were credited at", p.Cost)
	}
	if want := int64(60 * 33); p.Value != want {
		t.Errorf("net = %d, want %d: the tips, with the pearls counted once", p.Value, want)
	}
	for _, s := range p.Sources {
		if s.Source.Kind == loot.KindProduction && !s.CostKnown {
			t.Errorf("the tips row's cost is still unknown: %+v", s)
		}
	}
}

// TestAnInputWithNoRecordedArrivalStaysUnknown keeps the strict rule where there is nothing to match:
// an input the panel never saw arriving is not guessed at (marketprice.go's first rule).
func TestAnInputWithNoRecordedArrivalStaysUnknown(t *testing.T) {
	a := newStatsAggregator("", nil, fixedNow(auditBase))
	a.loot.prices = auditBook(MarketPrice{Name: "Pearl bolttips", Coins: 33})
	a.loot.record(lootReading("crafter1", 1,
		loot.Totals{Source: loot.Fletching(nil, 411, "Oyster pearl"), Events: 10,
			Items: []loot.Item{{ID: 46, Name: "Pearl bolttips", Count: 60, Value: 5}}},
	), auditBase)
	p, _, _, _ := a.loot.body()
	if p.Cost != 0 || p.Sources[0].CostKnown {
		t.Errorf("cost %d known %v: nothing arrived to price the pearls by", p.Cost, p.Sources[0].CostKnown)
	}
}

// TestACakeEatenInThreeBitesCostsOneCake: each bite of a Cake leaves another food (a 2/3 cake, then a
// Slice, then nothing). The Cake costs its whole price at the first bite and the later bites nothing, and
// their rows are not shown (a user request: only the Cake's price counts).
func TestACakeEatenInThreeBitesCostsOneCake(t *testing.T) {
	a := newStatsAggregator("", nil, fixedNow(auditBase))
	a.loot.prices = auditBook(
		MarketPrice{Name: "Cake", Coins: 53},
		MarketPrice{Name: "2/3 cake", Coins: 18},
		MarketPrice{Name: "Slice of cake", Coins: 6},
	)
	tally := loot.NewTally(func() time.Time { return auditBase })
	for _, f := range []struct {
		id   int
		name string
	}{{1891, "Cake"}, {1893, "2/3 cake"}, {1895, "Slice of cake"}} {
		tally.Add(loot.Drop{Source: loot.Consumable(nil, f.id, loot.ActionEat, f.name), Events: 1,
			Spent: []loot.Item{{ID: f.id, Name: f.name, Count: 1, Value: 50}}})
	}
	a.loot.record(lootReading("acct1", 1, tally.Snapshot()...), auditBase)

	p, _, _, _ := a.loot.body()
	if p.Cost != 53 {
		t.Errorf("cost = %d, want the one cake (53) the three bites ate", p.Cost)
	}
	got := map[string]int64{}
	for _, s := range p.Sources {
		got[s.Source.Name] = s.Cost
	}
	if got["Cake"] != 53 || got["2/3 cake"] != 0 || got["Slice of cake"] != 0 {
		t.Errorf("bites cost %v, want the whole cake (53) on the Cake row and nothing on the later bites", got)
	}
}

// TestTheOysterTileCountsTheOpensOfTheOysterPearlsRow: the oyster row is named for its pearls, and the
// header's "Oysters opened" tile reads the "open:Oyster" count, which the rename left at nothing.
func TestTheOysterTileCountsTheOpensOfTheOysterPearlsRow(t *testing.T) {
	a := restoreLootFile(t, oysterFile)
	p := a.lootSnapshot().Lifetime
	if p.Events["open:Oyster"] != 10 {
		t.Errorf("open:Oyster = %d (events %v), want the 10 oysters opened", p.Events["open:Oyster"], p.Events)
	}
}

// TestTheHeaderSaysHowMuchGrossIsStillUnnetted: an input the panel cannot price is not subtracted (unknown
// is not zero), and the period now carries how much gross sits on such rows.
func TestTheHeaderSaysHowMuchGrossIsStillUnnetted(t *testing.T) {
	a := newStatsAggregator("", nil, fixedNow(auditBase))
	a.loot.prices = auditBook(MarketPrice{Name: "Pearl bolttips", Coins: 33})
	a.loot.record(lootReading("crafter1", 1,
		loot.Totals{Source: loot.Fletching(nil, 411, "Oyster pearl"), Events: 10,
			Items: []loot.Item{{ID: 46, Name: "Pearl bolttips", Count: 60, Value: 5}}},
		loot.Totals{Source: loot.Fishing(nil, 316), Events: 5,
			Items: []loot.Item{{ID: 363, Name: "Raw bass", Count: 5, Value: 70}}},
	), auditBase)
	p, _, _, _ := a.loot.body()
	if p.Unnetted != 60*33 {
		t.Errorf("unnetted = %d, want the tips' 1980 (the bass row consumes nothing)", p.Unnetted)
	}
}

// TestAnInputThatArrivedBeforeTheRestartIsStillMatchedSinceTheRestart: the Yew longbows strung since the
// restart were cut before it (live: 34,862 strung against 12,380 cut), so the since-restart view must
// look for the arrival in the whole lifetime, as it does for the cost's price.
func TestAnInputThatArrivedBeforeTheRestartIsStillMatchedSinceTheRestart(t *testing.T) {
	a := newStatsAggregator("", nil, fixedNow(auditBase))
	a.loot.prices = auditBook(
		MarketPrice{Name: "Yew longbow", Coins: 437},
		MarketPrice{Name: "Bow string", Coins: 150},
	)
	// Before the restart: 10 bows cut, restored into the lifetime only.
	a.loot.restore(&lootFile{Accounts: map[string]map[string]*lootRec{"crafter1": {
		loot.Fletching(nil, 1515, "Yew logs").Key(): {Source: loot.Fletching(nil, 1515, "Yew logs"), Events: 10,
			Items: []loot.Item{{ID: 66, Name: "Yew longbow", Count: 10, Value: 8}}},
	}}})
	// Since the restart: 10 of them strung.
	a.loot.record(lootReading("crafter1", 1, loot.Totals{Source: loot.Stringing(66, "Yew longbow"), Events: 10,
		Items: []loot.Item{{ID: 855, Name: "Yew longbow", Count: 10, Value: 8}}}), auditBase)
	_, session, _, _ := a.loot.body()
	if session.Cost != 10*437+10*150 || session.Unnetted != 0 {
		t.Errorf("since restart cost %d unnetted %d, want the bows 4370 and the strings 1500 netted", session.Cost, session.Unnetted)
	}
}
