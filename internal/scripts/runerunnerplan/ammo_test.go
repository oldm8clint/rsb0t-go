package runerunnerplan

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/shopplan"
	"rsb0t-go/internal/scripts/tile"
)

// The prices of the three ammo shops are shop.rs2's walk, and every headline number is one an independent
// script (a re-derivation of adjusted_item_cost_buying and calc_shop_value) gave: a full shelf at the
// shop's own prices, the same shelf as it stands 66 minutes after being emptied (the live round of 13 October
// ran 66 minutes), and the one live purchase there is.
func TestAmmoShelvesPriceAsTheContentRuleSays(t *testing.T) {
	for _, c := range []struct {
		shop        fleetplan.RuneShop
		item        string
		stock, take int
		want        int
	}{
		// Full shelves, bought out.
		{Hickton, AmmoSteel, 600, 600, 13840},
		{Hickton, AmmoMithril, 400, 400, 18976},
		{Hickton, AmmoAdamant, 200, 200, 15880},
		{Hickton, AmmoRune, 100, 100, 29900},
		{Hickton, AmmoBolts, 200, 200, 1098},
		{Lowe, AmmoBolts, 1500, 1500, 22995},
		{Gerrant, AmmoFeather, 1000, 1000, 9250},
		{FishingGuildShop, AmmoFeather, 1500, 1500, 15250},
		// A shelf emptied 66 minutes ago: 6,600 ticks, so 165 back on a 40-tick line, 660 on a 10-tick line,
		// and the whole shelf on a line that regrows a unit a tick.
		{Hickton, AmmoSteel, 165, 165, 5779},
		{Hickton, AmmoMithril, 165, 165, 10929},
		{Hickton, AmmoAdamant, 165, 165, 14256},
		{Lowe, AmmoBolts, 660, 660, 11880},
		// MasterCrafter's live purchase (5 October): 237 Steel arrowtips off a full shelf for 2,985. The old
		// cost of 1 in shopplan said 411.
		{Hickton, AmmoSteel, 600, 237, 2985},
	} {
		if got := c.shop.CostToBuy(c.item, c.stock, c.take); got != c.want {
			t.Errorf("%s %d of %d %s cost %d, want %d", c.shop.Keeper, c.take, c.stock, c.item, got, c.want)
		}
	}
	// The round the 13 October fleet ran: every line of Hickton at the 66-minute steady state is
	// 5,779 + 10,929 + 14,256 + 29,900 + 1,098, and a first full-shelf visit 79,694.
	full := 0
	for _, l := range Hickton.Lines {
		if l.Rune == AmmoBronzeArrow || l.Rune == AmmoIronArrow {
			continue // the arrow lines of 2 November 2026 are not part of that round
		}
		full += Hickton.CostToBuy(l.Rune, l.Base, l.Base)
	}
	if full != 79694 {
		t.Errorf("Hickton's five lines at full shelves cost %d, want 79694", full)
	}
	// Restock: the board's projection after 66 minutes.
	t0 := time.Now()
	tick := 600 * time.Millisecond
	for _, c := range []struct {
		shop fleetplan.RuneShop
		item string
		want int
	}{{Hickton, AmmoSteel, 165}, {Hickton, AmmoMithril, 165}, {Hickton, AmmoRune, 100}, {Hickton, AmmoBolts, 200}, {Lowe, AmmoBolts, 660}, {Gerrant, AmmoFeather, 1000}} {
		l, _ := c.shop.Line(c.item)
		if got := l.EstimateStock(0, t0, t0.Add(66*time.Minute), tick); got != c.want {
			t.Errorf("%s %s after 66 minutes: %d, want %d", c.shop.Keeper, c.item, got, c.want)
		}
	}
}

// The shop itself is the ceiling: the dearest a unit ever costs is the price with the shelf empty, six times
// the cost only where the shelf holds 500 or more. Mithril, Adamant and Rune shelves hold less, so their
// ceilings are 80, 120 and 400 -- not the 96, 240 and 1200 a six-times rule would say, and 1200 is over
// MaxPriceCap, which settings.go would refuse.
func TestAmmoCeilings(t *testing.T) {
	hickton, _ := AmmoSiteFor("hickton")
	for item, want := range map[string]int{AmmoBolts: 9, AmmoSteel: 36, AmmoMithril: 80, AmmoAdamant: 120, AmmoRune: 400} {
		if got := hickton.AmmoCeiling(item); got != want {
			t.Errorf("Hickton %s ceiling %d, want %d", item, got, want)
		}
		if want > MaxPriceCap {
			t.Errorf("%s's ceiling %d is over MaxPriceCap", item, want)
		}
	}
	lowe, _ := AmmoSiteFor("Lowe")
	gerrant, _ := AmmoSiteFor("GERRANT")
	if lowe.AmmoCeiling(AmmoBolts) != 18 || gerrant.AmmoCeiling(AmmoFeather) != 12 || gerrant.AmmoCeiling(AmmoBolts) != 0 {
		t.Errorf("ceilings: Lowe bolts %d, Gerrant feather %d, Gerrant bolts %d", lowe.AmmoCeiling(AmmoBolts), gerrant.AmmoCeiling(AmmoFeather), gerrant.AmmoCeiling(AmmoBolts))
	}
	// Buy-all is the cap at MaxPriceCap: the full shelf is bought to the last unit.
	caps := AmmoCaps(DefaultAmmoItems(), nil, true)
	for _, c := range []struct {
		shop        fleetplan.RuneShop
		item        string
		stock, want int
		cost        int
	}{{Hickton, AmmoSteel, 600, 600, 13840}, {Hickton, AmmoRune, 100, 100, 29900}, {Lowe, AmmoBolts, 1500, 1500, 22995}, {Gerrant, AmmoFeather, 1000, 1000, 9250}} {
		n, cost := BuyableUnderCap(c.shop, c.item, c.stock, caps.For(c.item))
		if n != c.want || cost != c.cost {
			t.Errorf("buy-all %s %s: %d for %d, want %d for %d", c.shop.Keeper, c.item, n, cost, c.want, c.cost)
		}
	}
}

// The derived caps (ammoBuyAll false) are three times each part's cost, and what they buy is the top of a
// shelf: 217 Steel arrowtips for 2,602, the top 234 of Lowe's full bolt shelf for 1,404, and nothing at all
// from a Lowe shelf that was emptied (every unit under 1,000 costs the 18 ceiling).
func TestDerivedAmmoCaps(t *testing.T) {
	for item, want := range map[string]int{AmmoBolts: 9, AmmoSteel: 18, AmmoFeather: 6, AmmoMithril: 48, AmmoAdamant: 120, AmmoRune: 600, AmmoShaft: 3} {
		if got := DerivedAmmoCap(item); got != want {
			t.Errorf("%s derived cap %d, want %d", item, got, want)
		}
	}
	if DerivedAmmoCap("Rune") != 0 {
		t.Error("a name that is not an ammo part has no cap")
	}
	caps := AmmoCaps(DefaultAmmoItems(), map[string]int{AmmoBolts: 12}, false)
	if caps.For(AmmoBolts) != 12 || caps.For(AmmoSteel) != 18 {
		t.Errorf("a written cap wins and the rest are derived: %v", caps)
	}
	if n, c := BuyableUnderCap(Hickton, AmmoSteel, 600, 18); n != 217 || c != 2602 {
		t.Errorf("Hickton steel at cap 18: %d for %d", n, c)
	}
	if n, c := BuyableUnderCap(Lowe, AmmoBolts, 1500, 9); n != 234 || c != 1404 {
		t.Errorf("Lowe bolts at cap 9 off a full shelf: %d for %d", n, c)
	}
	if n, _ := BuyableUnderCap(Lowe, AmmoBolts, 660, 9); n != 0 {
		t.Errorf("Lowe bolts at cap 9 off a shelf that regrew to 660: %d", n)
	}
}

// The three modelled sites carry the numbers MasterCrafter's shop trip already uses for the same shelves, and
// the stands are the ones it reached (or, for Gerrant, the keeper's spawn from the map).
func TestAmmoSitesAgreeWithShopplan(t *testing.T) {
	for _, c := range []struct {
		key   string
		inv   string
		stand tile.Tile
		items []string
	}{
		{SiteHickton, "archeryshop2", HicktonStand, []string{AmmoBolts, AmmoSteel}},
		{SiteLowe, "archeryshop", LoweStand, []string{AmmoBolts}},
	} {
		site, _ := AmmoSiteFor(c.key)
		var sp shopplan.Shop
		for _, s := range shopplan.SupplyShops {
			if s.Inv == c.inv {
				sp = s
			}
		}
		if sp.Inv == "" {
			t.Fatalf("no shopplan shop %s", c.inv)
		}
		if site.Stand.X != sp.ShopStand.X || site.Stand.Z != sp.ShopStand.Z || c.stand != site.Stand {
			t.Errorf("%s stand %v, shopplan %v", c.key, site.Stand, sp.ShopStand)
		}
		if site.Shop.SellMultiplier != int(sp.Sell) || site.Shop.Delta != int(sp.Delta) || site.Shop.Keeper != sp.Keeper {
			t.Errorf("%s sells at %d/%d to %q, shopplan %d/%d %q", c.key, site.Shop.SellMultiplier, site.Shop.Delta, site.Shop.Keeper, sp.Sell, sp.Delta, sp.Keeper)
		}
		for _, name := range c.items {
			l, _ := site.Shop.Line(name)
			var it shopplan.Item
			for _, i := range sp.Items {
				if i.Name == name {
					it = i
				}
			}
			if it.Name == "" || int(it.Baseline) != l.Base || int(it.RestockTicks) != l.RateTicks || int(it.Cost) != l.Cost {
				t.Errorf("%s %s: line %+v, shopplan %+v", c.key, name, l, it)
			}
		}
	}
	// The stands are near the banks and the shops the ring pairs them with.
	for _, c := range []struct {
		key  string
		near tile.Tile
		max  int
	}{{SiteHickton, CatherbyBank, 15}, {SiteLowe, VarrockEastBank, 25}, {SiteGerrant, DraynorBank, 80}} {
		site, _ := AmmoSiteFor(c.key)
		if d := tile.Chebyshev(site.Stand, c.near); d > c.max {
			t.Errorf("%s is %d tiles from %v (max %d)", c.key, d, c.near, c.max)
		}
	}
	// Gerrant is 34 tiles from Betty and Lowe 25 from Aubury, the distances the placement is built on.
	if d := tile.Chebyshev(GerrantStand, BettyStand); d != 34 {
		t.Errorf("Gerrant is %d tiles from Betty, want 34", d)
	}
	if d := tile.Chebyshev(LoweStand, fleetplan.AuburyStand); d < 20 || d > 30 {
		t.Errorf("Lowe is %d tiles from Aubury", d)
	}
}

func TestAmmoRegistryAndSplit(t *testing.T) {
	if got := strings.Join(BuiltAmmoSites(), ","); got != "Gerrant,Lowe,Hickton" {
		t.Errorf("built ammo sites %s", got)
	}
	for _, k := range AmmoSiteKeys() {
		site, ok := AmmoSiteFor(strings.ToUpper(k))
		if !ok || site.Key != k {
			t.Errorf("%s not found in any case", k)
			continue
		}
		if site.Built == (site.Gate != "") {
			t.Errorf("%s: built %v with gate %q", k, site.Built, site.Gate)
		}
		if len(site.Shop.Lines) == 0 || site.Shop.Keeper == "" {
			t.Errorf("%s has no shop lines or keeper", k)
		}
	}
	if _, ok := AmmoSiteFor("Fionella"); ok {
		t.Error("an unknown shop was found")
	}
	// Dargaud is the one shop that sells Arrow shafts, and it is not built: the reason is Ranged 40.
	dargaud, _ := AmmoSiteFor(SiteDargaud)
	if !dargaud.Shop.Sells(AmmoShaft) || dargaud.Built || !strings.Contains(dargaud.Gate, "Ranged 40") {
		t.Errorf("Dargaud: %+v", dargaud)
	}
	for _, k := range BuiltAmmoSites() {
		site, _ := AmmoSiteFor(k)
		if site.Shop.Sells(AmmoShaft) {
			t.Errorf("%s sells Arrow shafts: only Dargaud does in content 289", k)
		}
	}
	// Split: built sites whose anchor is in the round are placed; the rest say why.
	placed, left := SplitAmmoSites([]string{"Gerrant", "Lowe", "Hickton", "Dargaud", "FishingGuild"}, []string{SiteBetty, SiteAubury, SiteLundail})
	if strings.Join(placed, ",") != "Gerrant,Lowe,Hickton" || len(left) != 2 || !strings.Contains(left[0], "Ranged 40") || !strings.Contains(left[1], "dropped") {
		t.Errorf("placed %v left %v", placed, left)
	}
	placed, left = SplitAmmoSites([]string{"Gerrant", "Lowe", "Hickton"}, []string{SiteAubury})
	if strings.Join(placed, ",") != "Lowe,Hickton" || len(left) != 1 || !strings.Contains(left[0], "Gerrant: it is placed with Betty") {
		t.Errorf("without Betty: placed %v left %v", placed, left)
	}
}

func TestAmmoSettingsLists(t *testing.T) {
	s := DefaultSettings()
	sites, err := s.AmmoShopKeys()
	if err != nil || strings.Join(sites, ",") != "Gerrant,Lowe,Hickton" {
		t.Fatalf("default sites %v %v", sites, err)
	}
	s.AmmoShops = []string{"hickton", "DARGAUD"}
	if sites, err = s.AmmoShopKeys(); err != nil || strings.Join(sites, ",") != "Hickton,Dargaud" {
		t.Errorf("canonical sites %v %v", sites, err)
	}
	for _, bad := range [][]string{{"Hickton", "hickton"}, {"Fionella"}} {
		s.AmmoShops = bad
		if _, err := s.AmmoShopKeys(); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	s = DefaultSettings()
	items, err := s.AmmoItemNames()
	if err != nil || strings.Join(items, ",") != "Bolts,Steel arrowtips,Feather,Mithril arrowtips,Adamant arrowtips,Rune arrowtips,Arrow shaft" {
		t.Fatalf("default items %v %v", items, err)
	}
	s.AmmoItems = []string{"feather", "BOLTS"}
	if items, err = s.AmmoItemNames(); err != nil || strings.Join(items, ",") != "Feather,Bolts" {
		t.Errorf("canonical items %v %v", items, err)
	}
	for _, bad := range [][]string{{"Bolts", "bolts"}, {"Bronze arrowtips"}, {"Mind rune"}} {
		s.AmmoItems = bad
		if _, err := s.AmmoItemNames(); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	listed := []string{AmmoBolts, AmmoSteel}
	caps, err := ParseAmmoCaps([]string{"bolts=12", "Steel arrowtips = 30"}, listed)
	if err != nil || caps[AmmoBolts] != 12 || caps[AmmoSteel] != 30 {
		t.Errorf("caps %v %v", caps, err)
	}
	for _, bad := range []string{"Bolts", "Bolts=0", "Bolts=1001", "Bolts=lots", "Feather=5", "Rune=5", "Bolts=5,6"} {
		entries := []string{bad}
		if _, err := ParseAmmoCaps(entries, listed); err == nil {
			t.Errorf("cap %q accepted", bad)
		}
	}
	if _, err := ParseAmmoCaps([]string{"Bolts=5", "bolts=6"}, listed); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Errorf("a part capped twice: %v", err)
	}
	stop, err := ParseAmmoStop(DefaultAmmoStopAbove(), DefaultAmmoItems())
	if err != nil || stop[AmmoSteel] != 150000 || stop[AmmoBolts] != 1000000 || stop[AmmoFeather] != 250000 || stop[AmmoShaft] != 300000 ||
		stop[AmmoMithril] != 20000 || stop[AmmoAdamant] != 20000 || stop[AmmoRune] != 20000 {
		t.Errorf("default gates %v %v", stop, err)
	}
	if stop, err = ParseAmmoStop([]string{"Bolts=0"}, listed); err != nil || stop[AmmoBolts] != 0 {
		t.Errorf("a gate of 0 (none): %v %v", stop, err)
	}
	if _, err = ParseAmmoStop([]string{"Bolts=-1"}, listed); err == nil {
		t.Error("a negative gate accepted")
	}
}

func muleWith(m map[string]int) MuleStock {
	return func(item string) (int, bool) { return m[item], true }
}

// THE MULE-STOCK GATE. The live mule (13 October) holds 39,330 Mithril arrowtips nothing uses: the gate leaves
// them, and buys the rest. With the mule's bank unread only the consumed parts are bought.
func TestAmmoWantedGate(t *testing.T) {
	stop, _ := ParseAmmoStop(DefaultAmmoStopAbove(), DefaultAmmoItems())
	live := muleWith(map[string]int{AmmoBolts: 40306, AmmoMithril: 39330, AmmoFeather: 9079})
	want, skipped := AmmoWanted(DefaultAmmoItems(), stop, live, nil)
	if strings.Join(want, ",") != "Bolts,Steel arrowtips,Feather,Adamant arrowtips,Rune arrowtips,Arrow shaft" {
		t.Errorf("live mule: want %v", want)
	}
	if len(skipped) != 1 || skipped[0].Item != AmmoMithril || !strings.Contains(skipped[0].Reason, "the mule holds 39330 (ammoStopAbove 20000)") {
		t.Errorf("live mule: skipped %+v", skipped)
	}
	// Exactly the threshold is enough; one under is not.
	if want, _ = AmmoWanted([]string{AmmoMithril}, stop, muleWith(map[string]int{AmmoMithril: 19999}), nil); len(want) != 1 {
		t.Errorf("19,999 Mithril arrowtips: %v", want)
	}
	if want, _ = AmmoWanted([]string{AmmoMithril}, stop, muleWith(map[string]int{AmmoMithril: 20000}), nil); len(want) != 0 {
		t.Errorf("20,000 Mithril arrowtips: %v", want)
	}
	// What the runner carries counts toward the mule's stock.
	pending := func(item string) int {
		if item == AmmoSteel {
			return 100000
		}
		return 0
	}
	if want, skipped = AmmoWanted([]string{AmmoSteel}, stop, muleWith(map[string]int{AmmoSteel: 60000}), pending); len(want) != 0 || len(skipped) != 1 {
		t.Errorf("60,000 on the mule and 100,000 carried: %v %v", want, skipped)
	}
	// An unread bank buys the consumed parts only.
	want, skipped = AmmoWanted(DefaultAmmoItems(), stop, nil, nil)
	if strings.Join(want, ",") != "Bolts,Steel arrowtips,Feather" || len(skipped) != 4 {
		t.Errorf("unread mule bank: want %v skipped %+v", want, skipped)
	}
	unread := func(string) (int, bool) { return 0, false }
	if want, _ = AmmoWanted(DefaultAmmoItems(), stop, unread, nil); strings.Join(want, ",") != "Bolts,Steel arrowtips,Feather" {
		t.Errorf("a stale mule bank: %v", want)
	}
	// No gate (0 or absent) never skips.
	if want, _ = AmmoWanted([]string{AmmoRune}, map[string]int{}, muleWith(map[string]int{AmmoRune: 1 << 30}), nil); len(want) != 1 {
		t.Errorf("no gate: %v", want)
	}
}

func ammoInput(shop fleetplan.RuneShop, seen map[string]int, items ...string) AmmoVisitInput {
	return AmmoVisitInput{Shop: shop, Items: items, Seen: seen, Caps: AmmoCaps(DefaultAmmoItems(), nil, true),
		Spendable: 1 << 30, FreeSlots: -1}
}

func hicktonFull() map[string]int {
	return map[string]int{AmmoBolts: 200, AmmoSteel: 600, AmmoMithril: 400, AmmoAdamant: 200, AmmoRune: 100}
}

// A first visit to Hickton with the purse to buy it all takes all five lines in the priority order and
// pays 79,694; a plan's coins are exactly the sum of its lines.
func TestPlanAmmoVisitBuysEverythingInPriorityOrder(t *testing.T) {
	in := ammoInput(Hickton, hicktonFull(), DefaultAmmoItems()...)
	p := PlanAmmoVisit(in)
	var names []string
	sum := 0
	for _, l := range p.Lines {
		names = append(names, fmt.Sprintf("%s=%d", l.Rune, l.Count))
		sum += l.Cost
		if l.AssumedStock != hicktonFull()[l.Rune] {
			t.Errorf("%s planned at stock %d", l.Rune, l.AssumedStock)
		}
	}
	if strings.Join(names, ",") != "Bolts=200,Steel arrowtips=600,Mithril arrowtips=400,Adamant arrowtips=200,Rune arrowtips=100" {
		t.Errorf("lines %v", names)
	}
	if p.Coins != 79694 || sum != p.Coins {
		t.Errorf("coins %d, lines sum %d, want 79694", p.Coins, sum)
	}
	// Arrow shaft is not a Hickton line: dropped without a word.
	if len(p.Skips) != 0 {
		t.Errorf("skips %+v", p.Skips)
	}
	// The same plan prices to the coin in the price check.
	bought := map[string]int{AmmoBolts: 200, AmmoSteel: 600, AmmoMithril: 400, AmmoAdamant: 200, AmmoRune: 100}
	if got := PredictedSpend(Hickton, p.PurchasePlan, bought); got != 79694 {
		t.Errorf("predicted spend %d", got)
	}
	if PriceMismatch(79694, 79694) || !PriceMismatch(79694, 90000) {
		t.Error("the price check does not read ammo spend")
	}
}

// A short purse buys the consumed parts first: with 20,000 Bolts and Steel are whole (14,938) and the rest of
// the purse goes down the list, never past it.
func TestPlanAmmoVisitStopsAtThePurseAndTheRoundBudget(t *testing.T) {
	in := ammoInput(Hickton, hicktonFull(), DefaultAmmoItems()...)
	in.Spendable = 20000
	p := PlanAmmoVisit(in)
	if p.Coins > 20000 || len(p.Lines) < 3 || p.Lines[0].Rune != AmmoBolts || p.Lines[0].Count != 200 || p.Lines[1].Rune != AmmoSteel || p.Lines[1].Count != 600 {
		t.Fatalf("20,000 coins: %+v", p.Lines)
	}
	if p.Lines[2].Rune != AmmoMithril || p.Lines[2].Count >= 400 || p.Lines[2].Count == 0 {
		t.Errorf("the rest of the purse goes to the next part: %+v", p.Lines[2])
	}
	// A purse of 1,000 does not even finish the bolts (187 of 200 for 994); the 6 coins left buy one Steel
	// arrowtip, as a rune visit's leftover goes down its list.
	in.Spendable = 1000
	if p = PlanAmmoVisit(in); len(p.Lines) != 2 || p.Lines[0].Rune != AmmoBolts || p.Lines[0].Count != 187 || p.Lines[1].Count != 1 || p.Coins != 1000 {
		t.Errorf("1,000 coins: %+v", p.Lines)
	}
	// The round's limit bounds the visit like the purse does, whichever is smaller.
	in = ammoInput(Hickton, hicktonFull(), DefaultAmmoItems()...)
	in.Limit = 15000
	p = PlanAmmoVisit(in)
	if p.Coins > 15000 || len(p.Lines) < 2 || p.Lines[1].Rune != AmmoSteel || p.Lines[1].Count != 600 {
		t.Errorf("limit 15,000: coins %d lines %+v", p.Coins, p.Lines)
	}
	in.Spendable = 5000
	if p = PlanAmmoVisit(in); p.Coins > 5000 {
		t.Errorf("the smaller of purse and limit: %d", p.Coins)
	}
	// Nothing to spend plans nothing.
	in.Spendable = 0
	if p = PlanAmmoVisit(in); len(p.Lines) != 0 || p.Coins != 0 {
		t.Errorf("no coins: %+v", p)
	}
}

// A new stack needs a free pack slot; a part already stacked in the pack needs none.
func TestPlanAmmoVisitSlots(t *testing.T) {
	in := ammoInput(Hickton, hicktonFull(), DefaultAmmoItems()...)
	in.FreeSlots = 1
	p := PlanAmmoVisit(in)
	if len(p.Lines) != 1 || p.Lines[0].Rune != AmmoBolts {
		t.Fatalf("one free slot: %+v", p.Lines)
	}
	if len(p.Skips) != 4 || !strings.Contains(p.Skips[0].Reason, "no free pack slot") {
		t.Errorf("skips %+v", p.Skips)
	}
	in.Stacked = map[string]bool{AmmoSteel: true, AmmoRune: true}
	p = PlanAmmoVisit(in)
	var names []string
	for _, l := range p.Lines {
		names = append(names, l.Rune)
	}
	if strings.Join(names, ",") != "Bolts,Steel arrowtips,Rune arrowtips" {
		t.Errorf("stacked parts need no slot: %v", names)
	}
	in.FreeSlots = 0
	p = PlanAmmoVisit(in)
	names = nil
	for _, l := range p.Lines {
		names = append(names, l.Rune)
	}
	if strings.Join(names, ",") != "Steel arrowtips,Rune arrowtips" {
		t.Errorf("no free slot: only the stacked parts: %v", names)
	}
}

// Caps and empty shelves: Lowe's bolts, the shelf as it stands 66 minutes after it was emptied.
func TestPlanAmmoVisitCapsAndShelves(t *testing.T) {
	in := ammoInput(Lowe, map[string]int{AmmoBolts: 660}, AmmoBolts)
	if p := PlanAmmoVisit(in); p.Coins != 11880 || p.Lines[0].Count != 660 {
		t.Errorf("buy-all at 660: %+v", p)
	}
	in.Caps = AmmoCaps(DefaultAmmoItems(), nil, false)
	p := PlanAmmoVisit(in)
	if len(p.Lines) != 0 || len(p.Skips) != 1 || !strings.Contains(p.Skips[0].Reason, "price 18 over the cap 9") {
		t.Errorf("derived cap at 660: %+v", p)
	}
	// An empty shelf, an unlisted part and an item with no cap.
	in = ammoInput(Gerrant, map[string]int{AmmoFeather: 0}, AmmoFeather, AmmoBolts)
	if p = PlanAmmoVisit(in); len(p.Lines) != 0 || len(p.Skips) != 1 || p.Skips[0].Item != AmmoFeather {
		t.Errorf("empty: %+v", p)
	}
	in = ammoInput(Gerrant, map[string]int{AmmoFeather: 1000}, AmmoFeather)
	in.Caps = Caps{}
	if p = PlanAmmoVisit(in); len(p.Lines) != 0 || !strings.Contains(p.Skips[0].Reason, "no price cap") {
		t.Errorf("no cap: %+v", p)
	}
}

func TestAmmoStationCostAndBudget(t *testing.T) {
	caps := AmmoCaps(DefaultAmmoItems(), nil, true)
	// The steady-state bill of the three consumed parts at Hickton: Steel 165 back, Bolts a full 200.
	est := map[string]int{AmmoBolts: 200, AmmoSteel: 165}
	if got := AmmoStationCost(Hickton, est, []string{AmmoBolts, AmmoSteel}, caps, 0); got != 1098+5779 {
		t.Errorf("Hickton bolts and steel %d", got)
	}
	if got := AmmoStationCost(Hickton, est, []string{AmmoBolts, AmmoSteel}, caps, 3000); got > 3000 || got < 1098 {
		t.Errorf("limit 3000: %d", got)
	}
	if got := AmmoStationCost(Hickton, est, []string{AmmoRune}, caps, 0); got != 0 {
		t.Errorf("a part with no shelf estimate costs %d", got)
	}
	if ok, why := AmmoWorthVisiting(1800, 3000); ok || !strings.Contains(why, "under ammoMinVisitCoins 3000") {
		t.Errorf("worth: %v %q", ok, why)
	}
	if ok, _ := AmmoWorthVisiting(3000, 3000); !ok {
		t.Error("exactly the minimum is worth a stop")
	}
	// The round's budget: spent so far is the ammo stations' Done spend, and nothing else.
	ring := []Station{{Kind: KindAmmo, Site: SiteHickton, World: 1}, {Kind: KindBuy, Site: SiteBetty, World: 1}, {Kind: KindAmmo, Site: SiteGerrant, World: 1}, {Kind: KindAmmo, Site: SiteLowe, World: 1}}
	done := map[string]StationResult{"Hickton@1": {Spent: 40000}, "Betty@1": {Spent: 900000}, "Gerrant@1": {Spent: 9250}}
	if got := AmmoSpentThisRound(ring, done); got != 49250 {
		t.Errorf("spent %d", got)
	}
	if left, ok := AmmoLeft(150000, 49250); !ok || left != 100750 {
		t.Errorf("left %d %v", left, ok)
	}
	if _, ok := AmmoLeft(150000, 150000); ok {
		t.Error("a spent budget has nothing left")
	}
	if left, ok := AmmoLeft(0, 1<<30); !ok || left != 0 {
		t.Errorf("no budget set is unlimited: %d %v", left, ok)
	}
	if got := AmmoBoughtLine(map[string]int{"Mind rune": 5, AmmoFeather: 3000, AmmoBolts: 2380, AmmoSteel: 0}); got != "Bolts=2380,Feather=3000" {
		t.Errorf("bought line %q", got)
	}
}

func TestAmmoItemRegistry(t *testing.T) {
	if got := len(AmmoItems()); got != 7 {
		t.Errorf("%d ammo parts", got)
	}
	// Every part's cost is the obj's own (arrows.obj, bolts.obj, fishing.obj), and every shop line says the same.
	for _, site := range AmmoSiteKeys() {
		s, _ := AmmoSiteFor(site)
		for _, l := range s.Shop.Lines {
			it, ok := AmmoItemFor(l.Rune)
			if !ok {
				t.Errorf("%s line %q is not an ammo part", site, l.Rune)
				continue
			}
			if it.Cost != l.Cost {
				t.Errorf("%s %s costs %d, the part says %d", site, l.Rune, l.Cost, it.Cost)
			}
		}
	}
	// The consumed parts are bought with the mule's bank unread; the dead stock is not.
	for _, it := range AmmoItems() {
		if it.Always != (it.Name == AmmoBolts || it.Name == AmmoSteel || it.Name == AmmoFeather) {
			t.Errorf("%s always=%v", it.Name, it.Always)
		}
	}
	if _, ok := AmmoItemFor("  steel ARROWTIPS "); !ok {
		t.Error("lookup is case- and space-insensitive")
	}
}
