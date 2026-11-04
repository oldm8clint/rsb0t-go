package runerunnerplan

import (
	"strings"
	"testing"
)

// The market station sits right after the draw (or the purse), before any hop, and only when asked.
func TestRingHasTheMarketStationAfterTheDraw(t *testing.T) {
	plain := keys(BuildRing(RingInput{M: 1, O: 2, Shops: BuiltSites(), OBlock: true}))
	with := keys(BuildRing(RingInput{M: 1, O: 2, Shops: BuiltSites(), OBlock: true, Market: true}))
	if strings.Contains(plain, "market") {
		t.Fatalf("a ring built without Market has a market station: %s", plain)
	}
	if want := strings.Replace(plain, "draw@1", "draw@1 market@1", 1); with != want {
		t.Fatalf("ring %q, want %q", with, want)
	}
	p := PlanRound(RoundInput{M: 1, O: 2, Shops: BuiltSites(), MaxHopsPerRound: 2, HopMinGain: 200, HopsAvailable: true, Worth: allWorth(500), Market: true})
	if got := keys(p.Ring); got != "deliver@1 draw@1 market@1 Betty@1 Aubury@1 hop2 Aubury@2 Betty@2 Lundail@2 hop1 Lundail@1" {
		t.Fatalf("PlanRound ring %q", got)
	}
	// The O block's own indices move by one with the market station in front of it.
	open, close, ok := OBlock(p.Ring)
	if !ok || open != 5 || close != 9 {
		t.Fatalf("O block %d..%d %v", open, close, ok)
	}
	if i, _ := Entry(p.Ring, AreaWest, 1, 1); i != 0 {
		t.Fatalf("Catherby on the mule's world starts at %d, want the delivery", i)
	}
}

func TestRotationRingHasTheMarketStationAfterDrawOrPurse(t *testing.T) {
	betty := keys(BuildRotationRing(RotationInput{M: 1, Worlds: []int{1}, Shops: []string{SiteBetty, SiteAubury}, Phase: 0, Market: true}))
	if !strings.HasPrefix(betty, "deliver@1 draw@1 market@1 hop1 Betty@1") {
		t.Fatalf("Betty-first rotation: %s", betty)
	}
	// Phase 1 of one world runs the circuit backwards: Lundail first, so the purse station.
	lundail := keys(BuildRotationRing(RotationInput{M: 1, Worlds: []int{1}, Shops: []string{SiteBetty, SiteLundail}, Phase: 1, Market: true}))
	if !strings.HasPrefix(lundail, "deliver@1 purse@1 market@1 hop1 Lundail@1") {
		t.Fatalf("Lundail-first rotation: %s", lundail)
	}
	off := keys(BuildRotationRing(RotationInput{M: 1, Worlds: []int{1}, Shops: []string{SiteBetty, SiteAubury}, Phase: 0}))
	if strings.Contains(off, "market") {
		t.Fatalf("market station without Market: %s", off)
	}
	if got := (Station{Kind: KindMarket, World: 1}).Key(); got != "market@1" {
		t.Fatalf("key %q", got)
	}
	if IsShopStation(Station{Kind: KindMarket, World: 1}) {
		t.Fatal("the market station is not a rune shop: a hop's shelf holds must not read it")
	}
}

func TestMarketSettings(t *testing.T) {
	r, err := DefaultSettings().Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if !r.MarketBuy || r.MarketPricePercent != 150 || r.MarketCoins != 1000000 || r.MarketCheckMinutes != 15 ||
		r.MarketMinLot != 100 || r.MarketTradeCoins != 1000000 {
		t.Fatalf("defaults %+v", r.Settings)
	}
	if len(r.MarketTargets) != 4 || r.MarketTargets[0].Rune != "Nature rune" || r.MarketTargets[0].Stock != 20000 {
		t.Fatalf("targets %+v", r.MarketTargets)
	}
	if strings.Join(r.MarketStallList, ",") != "Seers Market,Tradevault,Sil Stuff,S15" {
		t.Fatalf("stalls %v", r.MarketStallList)
	}
	for _, key := range []string{"marketBuy", "marketRunes", "marketStalls", "marketPricePercent", "marketCoins", "marketCheckMinutes", "marketMinLot", "marketTradeCoins"} {
		found := false
		for _, k := range SettingKeys() {
			found = found || k == key
		}
		if !found {
			t.Errorf("setting %s is not a key", key)
		}
	}
	s, err := decode(t, `{"marketRunes": ["Nature rune=500", "Death rune=100"], "marketStalls": ["S15"], "marketPricePercent": 120, "marketBuy": false}`)
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Resolve()
	if err != nil || r.MarketBuy || len(r.MarketTargets) != 2 || r.MarketTargets[1].Stock != 100 || len(r.MarketStallList) != 1 {
		t.Fatalf("custom %+v %v", r.Settings, err)
	}
	for _, bad := range []string{
		`{"marketRunes": ["Nature rune"]}`,
		`{"marketRunes": ["Bogus rune=5"]}`,
		`{"marketStalls": ["S15", "s15"]}`,
		`{"marketPricePercent": 99}`,
		`{"marketPricePercent": 401}`,
		`{"marketCoins": 10}`,
		`{"marketCheckMinutes": 0}`,
		`{"marketMinLot": 0}`,
		`{"marketTradeCoins": 6000000}`,
	} {
		if _, err := decode(t, bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}
