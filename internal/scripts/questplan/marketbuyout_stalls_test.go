package questplan

import (
	"strings"
	"testing"
)

// The price page read before the mule is asked for a coin: with the page of 13 Oct 2026 every stall can sell and the total is the sum.
func TestBuyOutStallsSumsWhatTheStallsList(t *testing.T) {
	s, why := BuyOutStalls(buyOutTerms())
	if why != "" || len(s.Stalls) != 3 || !s.Known || s.Listed != 298249+1213889+262411 {
		t.Fatalf("%+v %q", s, why)
	}
}

// A stall that lists no essence, one resting after a failure and one over the ceiling are not asked; the ones left are.
func TestBuyOutStallsPassesOverWhatCannotSell(t *testing.T) {
	tm := buyOutTerms()
	tm.Listings[0].Item = "Pure essence" // Seers Market lists no Rune essence
	tm.Skip = map[string]bool{"Tradevault": true}
	s, why := BuyOutStalls(tm)
	if why != "" || len(s.Stalls) != 1 || s.Stalls[0] != "Sil Stuff" || s.Listed != 262411 {
		t.Fatalf("%+v %q", s, why)
	}
	// Every stall ruled out: no buy-out, and why says what ruled each out.
	tm.Skip["Sil Stuff"] = true
	if _, why := BuyOutStalls(tm); why == "" || !strings.Contains(why, "Seers Market: its page lists no rune essence") ||
		!strings.Contains(why, "Tradevault: asked already or resting") || !strings.Contains(why, "Sil Stuff: asked already or resting") {
		t.Fatalf("%q", why)
	}
	tm = buyOutTerms()
	for i := range tm.Listings {
		tm.Listings[i].LastSold = 400
	}
	if _, why := BuyOutStalls(tm); !strings.Contains(why, "last sold at 400 each, over the ceiling of 150") {
		t.Fatalf("%q", why)
	}
}

// After a buy-out the stalls hold none: their rows are gone, and the page lists other things. That is no stall to buy from.
func TestBuyOutStallsAfterTheStallsAreBoughtOut(t *testing.T) {
	tm := buyOutTerms()
	tm.Listings = []MarketStock{
		{Market: "Seers Market", Item: "Nature rune", Stock: 40000, LastSold: 200},
		{Market: "Tradevault", Item: "Law rune", Stock: 3000},
		{Market: "Sil Stuff", Item: "Chaos rune", Stock: 900},
	}
	if s, why := BuyOutStalls(tm); why == "" || len(s.Stalls) != 0 || !strings.Contains(why, "its page lists no rune essence") {
		t.Fatalf("%+v %q", s, why)
	}
}

// A price page that could not be read is no reading: a buy-out of the whole stack does not go in blind (PickBuyOut would ask every
// stall at the fallback). A stall whose own page shows nothing is not counted: the buy-out is sized on what the page confirms.
func TestBuyOutStallsNeedsAReadablePage(t *testing.T) {
	tm := buyOutTerms()
	tm.Listings = nil
	if _, why := BuyOutStalls(tm); !strings.Contains(why, "price page could not be read") {
		t.Fatalf("%q", why)
	}
	// Three stalls whose pages show nothing of the item and one other market's row: nothing is confirmed.
	tm.Listings = []MarketStock{{Market: "Somewhere Else", Item: "Law rune", Stock: 3000}}
	if _, why := BuyOutStalls(tm); why == "" {
		t.Fatal("stalls the page does not show cannot be the only ones asked")
	}
	// One stall the page confirms and two it shows nothing of: the one is the supply (the others are not guessed at).
	tm.Listings = []MarketStock{{Market: "Seers Market", Item: "Rune essence", Stock: 5000, LastSold: 100}}
	s, why := BuyOutStalls(tm)
	if why != "" || len(s.Stalls) != 1 || s.Stalls[0] != "Seers Market" || !s.Known || s.Listed != 5000 {
		t.Fatalf("%+v %q", s, why)
	}
}

// A listed row that shows no stock (0) is a stall that can be asked, with its stock unknown: the existing readings treat 0 as not shown.
func TestBuyOutStallsWithAStockNotShown(t *testing.T) {
	tm := buyOutTerms()
	tm.Listings[2].Stock = 0
	s, why := BuyOutStalls(tm)
	if why != "" || s.Known || s.Listed != 0 || len(s.Stalls) != 3 {
		t.Fatalf("%+v %q", s, why)
	}
	if _, why := BuyOutStalls(MarketTerms{Order: PipEssenceMarkets, Item: "Rune essence", Listings: buyOutTerms().Listings, Fallback: 200, Ceiling: 150}); !strings.Contains(why, "no sane price") {
		t.Fatalf("%q", why)
	}
}
