package woodcutplan

import (
	"strings"
	"testing"
)

// TestBobStockMatchesTheContent pins what Bob's Brilliant Axes actually stocks, MEASURED from
// content/scripts/areas/area_lumbridge/configs/lumbridge.inv's `[axeshop]` section:
// bronze_axe x10, iron_axe x5, steel_axe x3. The fleet owner was unsure how far up Bob stocks;
// this is the answer, and steel is the ceiling.
func TestBobStockMatchesTheContent(t *testing.T) {
	want := []string{"Bronze axe", "Iron axe", "Steel axe"}
	if len(BobStock) != len(want) {
		t.Fatalf("BobStock = %v, want %v", BobStock, want)
	}
	for i, a := range want {
		if BobStock[i] != a {
			t.Errorf("BobStock[%d] = %q, want %q", i, BobStock[i], a)
		}
	}
	if BobBest != "Steel axe" {
		t.Errorf("BobBest = %q, want Steel axe", BobBest)
	}
	// The battleaxes and the bronze pickaxe share the same shop inventory but neither chops,
	// so neither may ever appear here.
	for _, no := range []string{"Bronze pickaxe", "Iron battleaxe", "Steel battleaxe", "Mithril battleaxe"} {
		if BobSells(no) {
			t.Errorf("BobSells(%q) is true — that is not a chopping axe", no)
		}
	}
	// Nothing above steel is buyable from any NPC shop on this server.
	for _, no := range []string{"Black axe", "Mithril axe", "Adamant axe", "Rune axe"} {
		if BobSells(no) {
			t.Errorf("BobSells(%q) is true — no shop on this server stocks it", no)
		}
	}
	if !BobSells("bronze axe") {
		t.Error("BobSells should be case-insensitive")
	}
}

func TestDecideAxeBuyTakesTheFreeOnesFirst(t *testing.T) {
	price := func(string) (int, bool) { return 150000, true }

	// Already holding the target, or better: nothing to do at all.
	if got := DecideAxeBuy([]string{"Rune axe"}, nil, "Rune axe", 0, price); got.Want != "" {
		t.Errorf("holding the target should decide nothing, got %+v", got)
	}
	if got := DecideAxeBuy([]string{"Rune axe"}, nil, "Mithril axe", 0, price); got.Want != "" {
		t.Errorf("holding better than the target should decide nothing, got %+v", got)
	}
	// One in the bank is free, and must win over buying a second.
	got := DecideAxeBuy([]string{"Bronze axe"}, []string{"Rune axe"}, "Rune axe", 0, price)
	if got.From != AxeFromBank || got.Want != "Rune axe" {
		t.Errorf("a banked target should come from the bank, got %+v", got)
	}
	if got.Need != 0 {
		t.Errorf("a banked axe costs nothing, got Need=%d", got.Need)
	}
}

func TestDecideAxeBuyRoutesByWhatCanActuallySellIt(t *testing.T) {
	price := func(axe string) (int, bool) {
		switch {
		case same(axe, "Rune axe"):
			return 150000, true
		case same(axe, "Steel axe"):
			return 400, true
		}
		return 0, false
	}

	// Above steel there is no shop at all, so the market is the only route and the reason
	// must say so rather than silently falling back to a Bob trip that cannot work.
	rune := DecideAxeBuy(nil, nil, "Rune axe", 0, price)
	if rune.From != AxeFromMarket {
		t.Errorf("a rune axe must come from the market, got %+v", rune)
	}
	if !strings.Contains(rune.Why, "no shop") {
		t.Errorf("the reason should say no shop sells it, got %q", rune.Why)
	}
	if FallbackFrom(rune.From, "Rune axe") != AxeFromNowhere {
		t.Error("a rune axe has no Bob fallback")
	}

	// At or below steel both routes exist, so a market refusal falls back to Bob.
	steel := DecideAxeBuy(nil, nil, "Steel axe", 0, price)
	if steel.From != AxeFromMarket {
		t.Errorf("steel should try the market first, got %+v", steel)
	}
	if FallbackFrom(steel.From, "Steel axe") != AxeFromBob {
		t.Error("steel should fall back to Bob")
	}
	if FallbackFrom(AxeFromBob, "Steel axe") != AxeFromNowhere {
		t.Error("there is nothing after Bob")
	}
}

// TestDecideAxeBuyNeverReadsAnUnknownPriceAsAffordable is the trap this codebase has hit in
// other shapes: an absent number is not a zero. The market lists plenty of items that have
// never traded and therefore have NO price, and treating that as "costs nothing" would send an
// account across the map to buy something it cannot pay for.
func TestDecideAxeBuyNeverReadsAnUnknownPriceAsAffordable(t *testing.T) {
	unknown := func(string) (int, bool) { return 0, false }
	got := DecideAxeBuy(nil, nil, "Black axe", 0, unknown)
	if got.Priced {
		t.Errorf("an unknown price must not report as priced, got %+v", got)
	}
	if got.Need != 0 {
		t.Errorf("Need is meaningless without a price, got %d", got.Need)
	}
	// And with no priceOf supplied at all.
	if got := DecideAxeBuy(nil, nil, "Rune axe", 0, nil); got.Priced {
		t.Errorf("a nil priceOf must not report as priced, got %+v", got)
	}
}

func TestDecideAxeBuyCountsTheShortfall(t *testing.T) {
	price := func(string) (int, bool) { return 150000, true }
	if got := DecideAxeBuy(nil, nil, "Rune axe", 20000, price); got.Need != 130000 || !got.Priced {
		t.Errorf("Need = %d priced=%v, want 130000 / true", got.Need, got.Priced)
	}
	// Enough coins already: nothing short.
	if got := DecideAxeBuy(nil, nil, "Rune axe", 150000, price); got.Need != 0 {
		t.Errorf("Need = %d, want 0 when the coins are already held", got.Need)
	}
}

func TestDecideAxeBuyRefusesNonsense(t *testing.T) {
	for _, want := range []string{"", "   ", "Rune scimitar", "Logs"} {
		if got := DecideAxeBuy(nil, nil, want, 0, nil); got.Want != "" {
			t.Errorf("DecideAxeBuy(want=%q) should decide nothing, got %+v", want, got)
		}
	}
}

func TestMarketAskReadsTheWayAPersonWouldSayIt(t *testing.T) {
	if got := MarketAsk("Rune axe", 1); got != "buying 1 rune axe" {
		t.Errorf("MarketAsk = %q, want %q", got, "buying 1 rune axe")
	}
	if got := MarketAsk("  Mithril axe  ", 2); got != "buying 2 mithril axe" {
		t.Errorf("MarketAsk = %q", got)
	}
	// A nonsense quantity still produces a sane line rather than "buying 0 ...", which reads
	// as a joke to the person on the other side.
	if got := MarketAsk("Rune axe", 0); got != "buying 1 rune axe" {
		t.Errorf("MarketAsk(qty 0) = %q", got)
	}
	// The client packs at most MaxChatLine characters; the line must be cut here rather than
	// silently truncated mid-word by the client.
	long := MarketAsk(strings.Repeat("verylongitemname", 20), 1)
	if len(long) > MaxChatLine {
		t.Errorf("MarketAsk produced %d characters, over the %d limit", len(long), MaxChatLine)
	}
}

func TestCoinsToRequestAsksForTheShortfallPlusAMargin(t *testing.T) {
	// Nothing to ask for when the coins are already there.
	if got := CoinsToRequest(600, 600); got != 0 {
		t.Errorf("CoinsToRequest(600,600) = %d, want 0", got)
	}
	if got := CoinsToRequest(600, 1000); got != 0 {
		t.Errorf("more coins than the price should ask for nothing, got %d", got)
	}
	if got := CoinsToRequest(0, 0); got != 0 {
		t.Errorf("an unknown price should ask for nothing, got %d", got)
	}
	// The margin applies to the SHORTFALL, not to the price: 100 short of a 600 item asks for
	// 110, not for 660.
	if got := CoinsToRequest(600, 500); got != 110 {
		t.Errorf("CoinsToRequest(600,500) = %d, want 110", got)
	}
	// Holding nothing asks for the whole price plus the margin.
	if got := CoinsToRequest(600, 0); got != 660 {
		t.Errorf("CoinsToRequest(600,0) = %d, want 660", got)
	}
	// Never rounds down to nothing.
	if got := CoinsToRequest(1, 0); got < 1 {
		t.Errorf("CoinsToRequest(1,0) = %d, want at least 1", got)
	}
}
