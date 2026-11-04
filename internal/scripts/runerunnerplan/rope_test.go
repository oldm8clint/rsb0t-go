package runerunnerplan

import (
	"strings"
	"testing"
)

// Port Khazard's shelf and prices, read from content 289 (port_khazard.inv [khazardshop] stock9=rope,30,20; port_khazard.npc
// [khazard_shopkeeper] shop_sell_multiplier 1400, shop_delta 10; _unpack/225/all.obj [rope] cost=18). The literal said 1300 and 30.
func TestPortKhazardRopeShelfMatchesContent(t *testing.T) {
	if PortKhazard.SellMultiplier != 1400 || PortKhazard.Delta != 10 || PortKhazard.NPCID != 555 {
		t.Fatalf("Port Khazard %+v", PortKhazard)
	}
	l, ok := PortKhazard.Line(AmmoRope)
	if !ok || l.Base != 30 || l.RateTicks != 20 || l.Cost != 18 {
		t.Fatalf("Rope line %+v %v", l, ok)
	}
	// 18 * 1400 / 1000 = 25 at the full shelf; each unit below base adds delta/1000 of the cost: 25 at 29, 30 at 1.
	for stock, want := range map[int]int{30: 25, 29: 25, 20: 27, 10: 28, 1: 30} {
		if got := PortKhazard.PriceAt(AmmoRope, stock); got != want {
			t.Errorf("Rope at stock %d costs %d, want %d", stock, got, want)
		}
	}
	whole := PortKhazard.CostToBuy(AmmoRope, 30, 30)
	if whole < 750 || whole > 850 {
		t.Errorf("the whole 30-Rope shelf costs %d, want about 800", whole)
	}
	// 20 ticks a unit at 0.6 s: 5 a minute, 300 an hour; a bought-out shelf is whole again in 6 minutes.
	if got := l.RegenPerHour(600 * 1e6); got != 300 {
		t.Errorf("Khazard regrows %v Ropes an hour, want 300", got)
	}
}

// Ned sells at 15 coins whatever the stock (ned.rs2 [label,ned_rope]) and keeps the obj's cost 18 so the registry's cost check holds.
func TestNedShopPricesRopeAtFifteen(t *testing.T) {
	for _, stock := range []int{0, 1, 12, NedShelf, 1000} {
		if got := NedShop.PriceAt(AmmoRope, stock); got != NedRopeCost {
			t.Errorf("Ned's Rope at stock %d costs %d, want %d", stock, got, NedRopeCost)
		}
	}
	if got := NedShop.CostToBuy(AmmoRope, NedShelf, 24); got != 24*15 {
		t.Errorf("24 Ropes from Ned cost %d, want %d", got, 24*15)
	}
	if it, _ := AmmoItemFor(AmmoRope); it.Cost != 18 || !it.Unstackable {
		t.Errorf("Rope part %+v", it)
	}
	ned, ok := AmmoSiteFor("ned")
	if !ok || !ned.Built || !ned.Dialogue || !ned.BankAfter || ned.Anchor != SiteBetty || ned.Bank != DraynorBank || ned.Stand.X != 3100 || ned.Stand.Z != 3258 {
		t.Errorf("Ned %+v", ned)
	}
	kh, _ := AmmoSiteFor(SitePortKhazard)
	if !kh.Built || !kh.BankAfter || !kh.CrewShelf || kh.Anchor != SiteLundail || kh.Bank != ArdougneBank || kh.MinVisit(DefaultAmmoMinVisitCoins) != KhazardMinVisitCoins {
		t.Errorf("Port Khazard %+v", kh)
	}
	// A rope-only Khazard stop (about 10 Ropes) is worth the walk, a handful is not.
	if est := AmmoStationCost(PortKhazard, map[string]int{AmmoRope: 30}, []string{AmmoRope}, AmmoCaps([]string{AmmoRope}, nil, true), 0); est < kh.MinVisit(DefaultAmmoMinVisitCoins) {
		t.Errorf("a full Khazard Rope shelf is worth %d, under the stop's minimum %d", est, KhazardMinVisitCoins)
	}
}

// Port Khazard rides with Lundail ahead of Aemad on every world, Ned closes every Betty pair; the hops and the rune blocks do not change.
func TestKhazardAndNedPlaceInTheRing(t *testing.T) {
	ring := liveRing([]string{SitePortKhazard, SiteAemad, SiteNed})
	got := PlanString(ring, 0)
	const want = "[deliver@1 draw@1 market@1 hop3 Betty@3 Ned@3 hop1 Betty@1 Ned@1 hop2 Betty@2 Ned@2 hop2 Aubury@2 Zaff@2 hop1 Aubury@1 Zaff@1 " +
		"hop3 Aubury@3 Zaff@3 hop3 PortKhazard@3 Aemad@3 Lundail@3 hop1 PortKhazard@1 Aemad@1 Lundail@1 hop2 PortKhazard@2 Aemad@2 Lundail@2 " +
		"hop2 WizardsGuild@2 hop1 WizardsGuild@1 hop3 WizardsGuild@3 hop1]"
	if got != want {
		t.Fatalf("ring\n got %s\nwant %s", got, want)
	}
	base := liveRing(nil)
	if realHops(ring, 1) != realHops(base, 1) || Hops(ring) != Hops(base) {
		t.Errorf("Rope stops changed the hops: %d real of %d, base %d of %d", realHops(ring, 1), Hops(ring), realHops(base, 1), Hops(base))
	}
	// An account in Ardougne on a world of the ring starts with the first ammo stop of that world's block, Khazard's.
	if i, _ := RotationEntry(ring, AreaArdougne, 3); ring[i].Key() != "PortKhazard@3" {
		t.Errorf("Ardougne on world 3 starts at %s", ring[i].Key())
	}
	if i, _ := RotationEntry(ring, AreaArdougne, 9); ring[i].Kind != KindHop || ring[i+1].Key() != "PortKhazard@3" {
		t.Errorf("Ardougne on world 9: %s then %s", ring[i].Key(), ring[i+1].Key())
	}
	// East of the Wilderness the entry is Betty's (Ned follows it, so the entry never lands on him).
	if i, _ := RotationEntry(ring, AreaEast, 1); ring[i].Kind == KindAmmo && ring[i].Site == SiteNed {
		t.Errorf("East on world 1 starts at Ned")
	}
	// Without Lundail or Betty in the round there is nothing to place them with.
	for _, c := range []struct{ site, shops string }{{SitePortKhazard, "Betty,Aubury"}, {SiteNed, "Aubury,Lundail"}} {
		if placed, left := SplitAmmoSites([]string{c.site}, strings.Split(c.shops, ",")); len(placed) != 0 || len(left) != 1 || !strings.Contains(left[0], "placed with") {
			t.Errorf("%s with %s: placed %v left %v", c.site, c.shops, placed, left)
		}
	}
}

// trawlerRope adds the sites and Rope to the listed ones when ammoBuy is on, gates an unlisted Rope, and leaves a written gate alone.
func TestTrawlerRopeSettingAddsTheRopeRound(t *testing.T) {
	r := resolveAmmo(t, `{"ammoBuy": true}`)
	if got := strings.Join(r.AmmoSiteList, ","); got != "Gerrant,Lowe,Hickton,PortKhazard,Aemad,Ned" {
		t.Errorf("sites %s", got)
	}
	if got := r.AmmoItemList[len(r.AmmoItemList)-1]; got != AmmoRope {
		t.Errorf("items %v", r.AmmoItemList)
	}
	if r.AmmoStop[AmmoRope] != TrawlerRopeStopAbove {
		t.Errorf("unlisted Rope gate %d, want %d", r.AmmoStop[AmmoRope], TrawlerRopeStopAbove)
	}
	if r.AmmoCapTable.For(AmmoRope) != MaxPriceCap {
		t.Errorf("Rope cap %d", r.AmmoCapTable.For(AmmoRope))
	}
	if got := strings.Join(r.DeliverNotedList, ","); got != "Rope" {
		t.Errorf("deliverNoted %s", got)
	}
	// ropeFloor 0 leaves Ned out; ammoBuy off or trawlerRope off adds nothing.
	r = resolveAmmo(t, `{"ammoBuy": true, "ropeFloor": 0}`)
	if got := strings.Join(r.AmmoSiteList, ","); strings.Contains(got, "Ned") || !strings.Contains(got, "PortKhazard") {
		t.Errorf("ropeFloor 0 sites %s", got)
	}
	for _, raw := range []string{`{}`, `{"ammoBuy": true, "trawlerRope": false}`} {
		r = resolveAmmo(t, raw)
		if got := strings.Join(r.AmmoSiteList, ","); got != "Gerrant,Lowe,Hickton" || len(r.AmmoItemList) != 7 {
			t.Errorf("%s: sites %s items %v", raw, got, r.AmmoItemList)
		}
	}
	// A Rope the operator listed keeps their order and their gate (0 is no gate); listed sites are not doubled.
	r = resolveAmmo(t, `{"ammoBuy": true, "ammoShops": ["PortKhazard", "Aemad"], "ammoItems": ["Rope", "Bolts"], "ammoStopAbove": ["Rope=0"]}`)
	if got := strings.Join(r.AmmoSiteList, ","); got != "PortKhazard,Aemad,Ned" || r.AmmoItemList[0] != AmmoRope || len(r.AmmoItemList) != 2 || r.AmmoStop[AmmoRope] != 0 {
		t.Errorf("listed: sites %s items %v stop %v", got, r.AmmoItemList, r.AmmoStop)
	}
}

func TestDeliverNotedRefusals(t *testing.T) {
	for raw, want := range map[string]string{
		`{"deliverNoted": ["Feather"]}`:      "stacks",
		`{"deliverNoted": ["Rope", "rope"]}`: "listed twice",
		`{"deliverNoted": ["Cabbage"]}`:      "not an ammo part",
		`{"ropeFloor": 99999}`:               "ropeFloor",
	} {
		s, err := decode(t, raw)
		if err == nil {
			err = s.Validate()
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", raw, err, want)
		}
	}
	if r := resolveAmmo(t, `{"deliverNoted": []}`); len(r.DeliverNotedList) != 0 {
		t.Errorf("empty deliverNoted: %v", r.DeliverNotedList)
	}
}

func TestNedRopesWanted(t *testing.T) {
	for _, c := range []struct {
		name             string
		floor, mule      int
		known            bool
		held, free, want int
		whyHas           string
	}{
		{"off", 0, 0, true, 0, 20, 0, "Ned is off"},
		{"met by the mule", 250, 300, true, 0, 20, 0, "floor 250 is met"},
		{"met with the pack", 250, 200, true, 50, 20, 0, "is met"},
		{"mule unread buys", 250, 999, false, 0, 24, 24, "mule holds 0"},
		{"short, pack room caps", 250, 37, true, 0, 24, 24, "floor is 250"},
		{"short by 10", 250, 240, true, 0, 24, 10, "floor is 250"},
		{"short by 4 is not worth the walk", 250, 246, true, 0, 24, 0, "not worth the walk"},
		{"no room", 250, 0, true, 0, 0, 0, "no free pack slot"},
		{"room for 8", 250, 0, true, 0, 8, 8, "floor is 250"},
		{"room for 5 is under the minimum", 250, 0, true, 0, 5, 0, "not worth the walk"},
	} {
		n, why := NedRopesWanted(c.floor, c.mule, c.known, c.held, c.free)
		if n != c.want || !strings.Contains(why, c.whyHas) {
			t.Errorf("%s: %d %q, want %d containing %q", c.name, n, why, c.want, c.whyHas)
		}
	}
}

func TestBankAfterWanted(t *testing.T) {
	kh, _ := AmmoSiteFor(SitePortKhazard)
	lowe, _ := AmmoSiteFor(SiteLowe)
	if !BankAfterWanted(kh, map[string]int{AmmoRope: AmmoBankAfterMin}) || BankAfterWanted(kh, map[string]int{AmmoRope: AmmoBankAfterMin - 1}) {
		t.Error("Khazard banks from the minimum, not below")
	}
	if BankAfterWanted(kh, map[string]int{AmmoPaste: 500, AmmoFeather: 900}) {
		t.Error("stackable parts need no bank trip")
	}
	if BankAfterWanted(lowe, map[string]int{AmmoRope: 20}) {
		t.Error("Lowe is not a BankAfter site")
	}
	if got := RopeOnly([]string{AmmoPaste, AmmoRope, AmmoBucket}); len(got) != 1 || got[0] != AmmoRope {
		t.Errorf("RopeOnly %v", got)
	}
}
