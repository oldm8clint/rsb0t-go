package fleetplan

import "testing"

func TestMarketOrderAndPriceText(t *testing.T) {
	if got := MarketOrderText("Chaos rune", 100); got != "buying 100 chaos rune" {
		t.Fatalf("order text %q", got)
	}
	cases := map[string]int{
		"100 chaos rune for 2,500 coins": 2500,
		"That'll be 12.5k gp":            12500,
		"1m coins please":                1000000,
		"price: 750gp":                   750,
	}
	for text, want := range cases {
		if got, ok := ParseMarketPrice(text); !ok || got != want {
			t.Errorf("%q -> %d %v, want %d", text, got, ok, want)
		}
	}
	if _, ok := ParseMarketPrice("sorry, out of stock"); ok {
		t.Error("no price in a refusal")
	}
}

func TestDecideMarketScreenNeverPaysForTheWrongThing(t *testing.T) {
	base := MarketScreen{Partner: "Seers Market", Market: "Seers_Market", Item: "Chaos rune", Qty: 100, Price: 2500, MaxPrice: 3000}
	with := func(f func(*MarketScreen)) MarketScreen { s := base; f(&s); return s }
	right := []ItemCount{{Item: "Chaos rune", Count: 100}}
	coins := []ItemCount{{Item: "Coins", Count: 2500}}
	cases := []struct {
		name string
		s    MarketScreen
		want MarketAction
	}{
		{"nothing up yet", base, MarketWait},
		{"their order is up: put the coins on", with(func(s *MarketScreen) { s.Theirs = right }), MarketOffer},
		{"both right on the offer screen", with(func(s *MarketScreen) { s.Theirs, s.Mine = right, coins }), MarketAccept},
		{"both right on the confirm screen", with(func(s *MarketScreen) { s.Theirs, s.Mine, s.Confirm = right, coins, true }), MarketAccept},
		{"short on the confirm screen", with(func(s *MarketScreen) {
			s.Theirs, s.Mine, s.Confirm = []ItemCount{{Item: "Chaos rune", Count: 90}}, coins, true
		}), MarketDecline},
		{"wrong item", with(func(s *MarketScreen) { s.Theirs = []ItemCount{{Item: "Fire rune", Count: 100}} }), MarketDecline},
		{"more than ordered", with(func(s *MarketScreen) { s.Theirs = []ItemCount{{Item: "Chaos rune", Count: 101}} }), MarketDecline},
		{"an extra item", with(func(s *MarketScreen) { s.Theirs = append(right, ItemCount{Item: "Coal", Count: 1}) }), MarketDecline},
		{"somebody else's trade", with(func(s *MarketScreen) { s.Partner, s.Theirs = "scammer", right }), MarketDecline},
		{"price over the cap", with(func(s *MarketScreen) { s.Price, s.Theirs = 3001, right }), MarketDecline},
		{"too many of my coins", with(func(s *MarketScreen) { s.Theirs, s.Mine = right, []ItemCount{{Item: "Coins", Count: 2600}} }), MarketDecline},
		{"something else of mine", with(func(s *MarketScreen) { s.Theirs, s.Mine = right, append(coins, ItemCount{Item: "Shark", Count: 1}) }), MarketDecline},
		{"confirm with no coins", with(func(s *MarketScreen) { s.Theirs, s.Confirm = right, true }), MarketDecline},
	}
	for _, c := range cases {
		if got, why := DecideMarketScreen(c.s); got != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, got, why, c.want)
		}
	}
}
