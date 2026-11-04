package marketstockplan

import (
	"strings"
	"testing"
)

func held(m map[string]int) func(string) (int, bool) {
	return func(r string) (int, bool) { n, ok := m[r]; return n, ok }
}

func book(m map[string]Book) func(string) (Book, bool) {
	return func(i string) (Book, bool) { b, ok := m[i]; return b, ok }
}

// The 6-7 October state: the mule short of Nature, S15's page lists Nature at its last price.
func baseInput() Input {
	return Input{
		Targets: []Target{{"Nature rune", 20000}, {"Death rune", 6000}},
		Held:    held(map[string]int{"Nature rune": 109, "Death rune": 0}),
		Listings: []Listing{
			{Market: "S15", Item: "Nature rune", Stock: 10000, Ask: 600},
			{Market: "Seers Market", Item: "Nature rune", Stock: 300, Ask: 700},
			{Market: "Sil Stuff", Item: "Nature rune", Stock: 13},
		},
		Book:       book(map[string]Book{"Nature rune": {Sell: 600, SellTrades: 11, Coherent: true}}),
		Stalls:     DefaultStalls(),
		Coins:      3000000,
		MinLot:     100,
		TradeCoins: 1000000,
	}
}

func TestBuysTheCheapestStallUnderTheCeiling(t *testing.T) {
	p := MakePlan(baseInput())
	// 3M coins at a 750 unit cap is 4000 runes: three 1M trades of 1333 and the last one (MaxTradesPerStation 30).
	if len(p.Trades) != 4 {
		t.Fatalf("want 4 trades (the 3M coins at 750 a unit cap under a 1M limit), got %d: %+v (%s)", len(p.Trades), p.Trades, p.Reason())
	}
	first := p.Trades[0]
	if first.Market != "S15" || first.Item != "Nature rune" || first.Unit != 600 {
		t.Fatalf("first trade %+v", first)
	}
	// 600 listed, 25% slack = 750, ceiling 900 (1.5 x 600): the cap is 750.
	if first.UnitCap != 750 || first.Ceiling != 900 {
		t.Fatalf("cap %d ceiling %d, want 750 and 900", first.UnitCap, first.Ceiling)
	}
	if first.Qty != 1333 || first.MaxCoins != 1333*750 { // 1,000,000 / 750
		t.Fatalf("qty %d maxCoins %d", first.Qty, first.MaxCoins)
	}
	total := 0
	for _, tr := range p.Trades {
		if tr.MaxCoins > 1000000 {
			t.Fatalf("trade over the stall's coin limit: %+v", tr)
		}
		total += tr.MaxCoins
	}
	if total > 3000000 {
		t.Fatalf("spends %d of 3,000,000", total)
	}
}

func TestPriceCeilingRefusesAnOverpricedStall(t *testing.T) {
	in := baseInput()
	in.Listings = []Listing{{Market: "S15", Item: "Nature rune", Stock: 10000, Ask: 950}} // over 1.5 x 600 = 900
	p := MakePlan(in)
	if len(p.Trades) != 0 {
		t.Fatalf("bought at 950 over a ceiling of 900: %+v", p.Trades)
	}
	if !strings.Contains(p.Reason(), "asks 950, over the ceiling") {
		t.Fatalf("reason %q", p.Reason())
	}
}

func TestTheHardCeilingBoundsASkewedBook(t *testing.T) {
	in := baseInput()
	in.Book = book(map[string]Book{"Nature rune": {Sell: 5000, SellTrades: 4, Coherent: true}})
	if c, _ := Ceiling("Nature rune", in.mustBook("Nature rune"), true, 150); c != 1000 {
		t.Fatalf("ceiling %d, want the hard 1000", c)
	}
	in.Listings = []Listing{{Market: "S15", Item: "Nature rune", Stock: 5000, Ask: 1500}}
	if p := MakePlan(in); len(p.Trades) != 0 {
		t.Fatalf("bought at 1500: %+v", p.Trades)
	}
}

func (in Input) mustBook(item string) Book { b, _ := in.Book(item); return b }

func TestAnUntrustedBookFallsBackToTheHardCeiling(t *testing.T) {
	for name, b := range map[string]Book{
		"one trade":  {Sell: 600, SellTrades: 1, Coherent: true},
		"incoherent": {Sell: 600, SellTrades: 9, Coherent: false},
		"no sells":   {},
	} {
		if c, why := Ceiling("Nature rune", b, true, 150); c != 1000 || !strings.Contains(why, "hard ceiling") {
			t.Fatalf("%s: ceiling %d (%s)", name, c, why)
		}
	}
	if c, _ := Ceiling("Nature rune", Book{}, false, 150); c != 1000 {
		t.Fatalf("no book at all: %d", c)
	}
	if c, _ := Ceiling("Mystery rune", Book{}, false, 150); c != 0 {
		t.Fatalf("unknown rune has a ceiling %d", c)
	}
}

func TestStopsAtTheTarget(t *testing.T) {
	in := baseInput()
	in.Held = held(map[string]int{"Nature rune": 19950, "Death rune": 6000})
	p := MakePlan(in)
	if len(p.Trades) != 1 || p.Trades[0].Qty != 50 {
		t.Fatalf("a shortfall of 50 is bought whole, no more: %+v", p.Trades)
	}
	in.Held = held(map[string]int{"Nature rune": 20000, "Death rune": 6000})
	p = MakePlan(in)
	if len(p.Trades) != 0 || !strings.Contains(p.Reason(), "the mule holds 20000") {
		t.Fatalf("at target: %+v %q", p.Trades, p.Reason())
	}
}

func TestPendingRunesCountTowardTheTarget(t *testing.T) {
	in := baseInput()
	in.Pending = map[string]int{"Nature rune": 19891} // 109 + 19,891 = 20,000
	if p := MakePlan(in); len(p.Trades) != 0 {
		t.Fatalf("bought what is already on the way: %+v", p.Trades)
	}
}

func TestASmallShortfallIsBoughtWholeButASmallLotIsNotWorthTheWalk(t *testing.T) {
	in := baseInput()
	in.Held = held(map[string]int{"Nature rune": 19960, "Death rune": 6000}) // short 40, minLot 100
	p := MakePlan(in)
	if len(p.Trades) != 1 || p.Trades[0].Qty != 40 {
		t.Fatalf("a shortfall of 40 should be bought whole: %+v (%s)", p.Trades, p.Reason())
	}
	// A big shortfall against a stall with 13 in stock: under the lot, not worth the walk.
	in = baseInput()
	in.Listings = []Listing{{Market: "Sil Stuff", Item: "Nature rune", Stock: 13, Ask: 600}}
	p = MakePlan(in)
	if len(p.Trades) != 0 || !strings.Contains(p.Reason(), "only 13 can be bought") {
		t.Fatalf("walked for 13 natures: %+v %q", p.Trades, p.Reason())
	}
}

func TestCoinsBoundTheLotAndMustCoverTheLot(t *testing.T) {
	in := baseInput()
	in.Coins = 75000 // 100 at 750
	p := MakePlan(in)
	if len(p.Trades) != 1 || p.Trades[0].Qty != 100 || p.Trades[0].MaxCoins != 75000 {
		t.Fatalf("%+v (%s)", p.Trades, p.Reason())
	}
	in.Coins = 70000 // 93 affordable, under the lot of 100
	if p := MakePlan(in); len(p.Trades) != 0 {
		t.Fatalf("bought a lot it cannot pay for: %+v", p.Trades)
	}
}

func TestRestingAndUnlistedStallsAreSkipped(t *testing.T) {
	in := baseInput()
	in.Resting = map[string]bool{RestKey("S15", "Nature rune"): true}
	p := MakePlan(in)
	if len(p.Trades) == 0 || p.Trades[0].Market != "Seers Market" {
		t.Fatalf("a resting S15 should leave the Seers Market: %+v (%s)", p.Trades, p.Reason())
	}
	in = baseInput()
	in.Stalls = []string{"Tradevault"}
	if p := MakePlan(in); len(p.Trades) != 0 {
		t.Fatalf("asked a stall that is not in the list: %+v", p.Trades)
	}
}

func TestAListingWithNoPriceIsOfferedTheBookMedian(t *testing.T) {
	in := baseInput()
	in.Targets = []Target{{"Death rune", 6000}}
	in.Held = held(map[string]int{"Death rune": 0})
	in.Book = book(map[string]Book{"Death rune": {Sell: 300, SellTrades: 9, Coherent: true}})
	in.Listings = []Listing{{Market: "S15", Item: "Death rune", Stock: 3596}}
	p := MakePlan(in)
	if len(p.Trades) == 0 || p.Trades[0].Unit != 300 || p.Trades[0].UnitCap != 375 || p.Trades[0].Qty != 2666 {
		t.Fatalf("%+v (%s)", p.Trades, p.Reason())
	}
	in.Book = book(nil)
	if p := MakePlan(in); len(p.Trades) != 0 {
		t.Fatalf("priced a line with no price and no book: %+v", p.Trades)
	}
}

func TestRunesTheMuleHasNotPublishedAreLeftAlone(t *testing.T) {
	in := baseInput()
	in.Held = func(string) (int, bool) { return 0, false }
	p := MakePlan(in)
	if len(p.Trades) != 0 || !strings.Contains(p.Reason(), "published no bank") {
		t.Fatalf("%+v %q", p.Trades, p.Reason())
	}
}

func TestOnlyDeliverableRunesAreBought(t *testing.T) {
	in := baseInput()
	in.Deliverable = func(r string) bool { return r != "Nature rune" }
	p := MakePlan(in)
	if len(p.Trades) != 0 || !strings.Contains(p.Reason(), "Nature rune: not a rune this runner delivers") {
		t.Fatalf("%+v %q", p.Trades, p.Reason())
	}
}

func TestEmptyMarketCostsNothing(t *testing.T) {
	in := baseInput()
	in.Listings = []Listing{{Market: "Constructor", Item: "Nature rune", Stock: 4, Ask: 0}, {Market: "Sil Stuff", Item: "Nature rune", Stock: 13}}
	p := MakePlan(in)
	if len(p.Trades) != 0 {
		t.Fatalf("%+v", p.Trades)
	}
	if p.Reason() == "" {
		t.Fatal("no reason for the log")
	}
}

func TestTradesPerStationAreBounded(t *testing.T) {
	in := baseInput()
	in.Coins = 100000000
	in.Targets = []Target{{"Nature rune", 10000000}}
	in.Listings = []Listing{{Market: "S15", Item: "Nature rune", Stock: 5000000, Ask: 600}}
	if p := MakePlan(in); len(p.Trades) != MaxTradesPerStation {
		t.Fatalf("%d trades", len(p.Trades))
	}
}

func TestParseTargetsAndStalls(t *testing.T) {
	got, err := ParseTargets(nil)
	if err != nil || len(got) != 4 || got[0] != (Target{"Nature rune", 20000}) {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = ParseTargets([]string{"nature=500", "Death rune=10"})
	if err != nil || got[0].Rune != "Nature rune" || got[1].Stock != 10 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range [][]string{{"Nature rune"}, {"Gold rune=5"}, {"Nature rune=0"}, {"Nature rune=x"}, {"Nature rune=5", "nature=6"}} {
		if _, err := ParseTargets(bad); err == nil {
			t.Fatalf("%v accepted", bad)
		}
	}
	if s, err := ParseStalls(nil); err != nil || len(s) != 4 {
		t.Fatalf("%v %v", s, err)
	}
	if _, err := ParseStalls([]string{"A", "a"}); err == nil {
		t.Fatal("duplicate stall accepted")
	}
	if _, err := ParseStalls([]string{" "}); err == nil {
		t.Fatal("blank stall accepted")
	}
}

func TestBoughtLine(t *testing.T) {
	if got := BoughtLine(1000, "Nature rune", 612000, 1109); got != "market: bought 1000 Nature rune at 612 each (mule now 1109)" {
		t.Fatal(got)
	}
	if got := BoughtLine(0, "Nature rune", 0, 5); !strings.Contains(got, "at 0 each") {
		t.Fatal(got)
	}
}
