package runerunnerplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
)

// ---- settings ----

func resolveAmmo(t *testing.T, raw string) Resolved {
	t.Helper()
	s, err := decode(t, raw)
	if err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	r, err := s.Resolve()
	if err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return r
}

// Ammo is off by default and changes nothing about a runner that does not ask for it: the same delivery items,
// the same shops, no ammo station.
func TestAmmoSettingsDefaultOff(t *testing.T) {
	r := resolveAmmo(t, `{}`)
	if r.AmmoBuy || r.AmmoOn() || len(r.DeliverAmmoItems()) != 0 {
		t.Errorf("ammo is on by default: %+v", r.Settings)
	}
	if strings.Join(r.DeliverItems(), ",") != strings.Join(r.DeliverRuneItems(), ",") {
		t.Errorf("deliver items %v", r.DeliverItems())
	}
	if placed, left := r.AmmoPlaced(r.ShopList); placed != nil || left != nil {
		t.Errorf("ammo off still places %v %v", placed, left)
	}
	if strings.Join(r.AmmoSiteList, ",") != "Gerrant,Lowe,Hickton" || len(r.AmmoItemList) != 7 || !r.AmmoBuyAll || r.AmmoCoins != 150000 ||
		r.AmmoMinVisitCoins != 3000 || r.AmmoSlotReserve != 1 || r.AmmoStop[AmmoMithril] != 20000 {
		t.Errorf("defaults: sites %v items %v buyAll %v coins %d min %d slots %d stop %v", r.AmmoSiteList, r.AmmoItemList, r.AmmoBuyAll,
			r.AmmoCoins, r.AmmoMinVisitCoins, r.AmmoSlotReserve, r.AmmoStop)
	}
	// Buy-all is the default cap table; the derived one is three times each cost.
	if r.AmmoCapTable.For(AmmoBolts) != MaxPriceCap || r.AmmoCapTable.For(AmmoRune) != MaxPriceCap {
		t.Errorf("buy-all caps %v", r.AmmoCapTable)
	}
	r = resolveAmmo(t, `{"ammoBuyAll": false}`)
	if r.AmmoCapTable.For(AmmoBolts) != 9 || r.AmmoCapTable.For(AmmoSteel) != 18 || r.AmmoCapTable.For(AmmoFeather) != 6 {
		t.Errorf("derived caps %v", r.AmmoCapTable)
	}
	// An empty list means the default (the panel's empty field), as for the runes.
	r = resolveAmmo(t, `{"ammoShops": [], "ammoItems": []}`)
	if len(r.AmmoSiteList) != 3 || len(r.AmmoItemList) != 7 {
		t.Errorf("empty lists: %v %v", r.AmmoSiteList, r.AmmoItemList)
	}
}

// ammoBuy on a rotating runner carries the ammo to the mule: the delivery lists the runes first, then the
// ammo parts, in priority order.
func TestAmmoSettingsOn(t *testing.T) {
	r := resolveAmmo(t, `{"ammoBuy": true, "trawlerRope": false, "zaffStaffs": 2}`)
	if !r.AmmoOn() {
		t.Fatal("ammoBuy true on a rotation is not on")
	}
	items := r.DeliverItems()
	if len(items) != len(r.RuneList)+1+7 || items[len(r.RuneList)] != "Staff of air" || items[len(r.RuneList)+1] != AmmoBolts || items[len(items)-1] != AmmoShaft {
		t.Errorf("deliver items %v", items)
	}
	if got := strings.Join(r.DeliverAmmoItems(), ","); got != strings.Join(DefaultAmmoItems(), ",") {
		t.Errorf("ammo deliver items %s", got)
	}
	placed, left := r.AmmoPlaced(r.ShopList)
	if strings.Join(placed, ",") != "Gerrant,Lowe,Hickton" || len(left) != 0 {
		t.Errorf("placed %v left %v", placed, left)
	}
	// A written cap wins for its part; the rest take the buy-all rule. A shortened part list shortens the delivery.
	r = resolveAmmo(t, `{"ammoBuy": true, "trawlerRope": false, "ammoItems": ["Feather", "Bolts"], "ammoPriceCaps": ["bolts=12"], "ammoStopAbove": ["Feather=5"]}`)
	if r.AmmoCapTable.For(AmmoFeather) != MaxPriceCap || r.AmmoCapTable.For(AmmoBolts) != 12 || len(r.AmmoItemList) != 2 || r.AmmoStop[AmmoFeather] != 5 {
		t.Errorf("shortened: caps %v items %v stop %v", r.AmmoCapTable, r.AmmoItemList, r.AmmoStop)
	}
	// A cap on a part that is listed.
	r = resolveAmmo(t, `{"ammoBuy": true, "trawlerRope": false, "ammoPriceCaps": ["steel arrowtips=30"]}`)
	if r.AmmoCapTable.For(AmmoSteel) != 30 || r.AmmoCapTable.For(AmmoBolts) != MaxPriceCap {
		t.Errorf("written cap: %v", r.AmmoCapTable)
	}
	// The classic ring has no ammo stations: on there it is a no-op, and the delivery does not carry ammo.
	r = resolveAmmo(t, `{"ammoBuy": true, "trawlerRope": false, "rotate": "off"}`)
	if r.AmmoOn() || len(r.DeliverAmmoItems()) != 0 {
		t.Errorf("ammo on the classic ring: %v", r.DeliverItems())
	}
	// Only unbuilt shops listed, or the shops they are placed with left out, is nothing to place.
	if r = resolveAmmo(t, `{"ammoBuy": true, "trawlerRope": false, "ammoShops": ["Shantay", "FishingGuild"]}`); r.AmmoOn() {
		t.Error("only unbuilt shops, yet ammo is on")
	}
	if r = resolveAmmo(t, `{"ammoBuy": true, "trawlerRope": false, "ammoShops": ["Gerrant"], "shops": ["Aubury", "Lundail"]}`); r.AmmoOn() {
		t.Error("Gerrant without Betty, yet ammo is on")
	}
	if r = resolveAmmo(t, `{"ammoBuy": true, "trawlerRope": false, "ammoShops": ["Gerrant", "Hickton"], "shops": ["Aubury", "Lundail"]}`); !r.AmmoOn() {
		t.Error("Hickton is placed with the delivery: ammo is on")
	}
	// The round's own shops (the guild shut below Magic 66) decide the placement too.
	r = resolveAmmo(t, `{"ammoBuy": true, "trawlerRope": false}`)
	if placed, left := r.AmmoPlaced([]string{SiteAubury, SiteLundail}); strings.Join(placed, ",") != "Lowe,Hickton" || len(left) != 1 {
		t.Errorf("a round without Betty: %v %v", placed, left)
	}
}

func TestAmmoSettingsRefusals(t *testing.T) {
	for _, c := range []struct{ name, raw, want string }{
		{"unknown ammo shop", `{"ammoShops": ["Fionella"]}`, "ammoShops"},
		{"duplicate ammo shop", `{"ammoShops": ["Lowe", "lowe"]}`, "listed twice"},
		{"unknown part", `{"ammoItems": ["Bronze arrowtips"]}`, "ammoItems"},
		{"duplicate part", `{"ammoItems": ["Bolts", "bolts"]}`, "listed twice"},
		{"a rune as a part", `{"ammoItems": ["Mind rune"]}`, "ammoItems"},
		{"cap for an unlisted part", `{"ammoItems": ["Bolts"], "ammoPriceCaps": ["Feather=6"]}`, "which is not in ammoItems"},
		{"cap of zero", `{"ammoPriceCaps": ["Bolts=0"]}`, "the cap must be 1-1000"},
		{"cap over the ceiling", `{"ammoPriceCaps": ["Rune arrowtips=1200"]}`, "the cap must be 1-1000"},
		{"cap not a pair", `{"ammoPriceCaps": ["Bolts"]}`, "not Item=number"},
		{"gate negative", `{"ammoStopAbove": ["Bolts=-5"]}`, "ammoStopAbove"},
		{"gate for an unlisted part", `{"ammoItems": ["Bolts"], "ammoStopAbove": ["Steel arrowtips=5"]}`, "which is not in ammoItems"},
		{"budget negative", `{"ammoCoins": -1}`, "ammoCoins"},
		{"budget too big", `{"ammoCoins": 10000001}`, "ammoCoins"},
		{"min visit negative", `{"ammoMinVisitCoins": -1}`, "ammoMinVisitCoins"},
		{"slot reserve too big", `{"ammoSlotReserve": 11}`, "ammoSlotReserve"},
	} {
		if _, err := decode(t, c.raw); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	// 0 is a legal budget (unlimited), a legal reserve and a legal minimum.
	if _, err := decode(t, `{"ammoCoins": 0, "ammoMinVisitCoins": 0, "ammoSlotReserve": 0}`); err != nil {
		t.Errorf("zeros refused: %v", err)
	}
	// The ammo keys are in SettingKeys (and so in the panel's schema, which a bot test keeps in step).
	keys := strings.Join(SettingKeys(), ",")
	for _, k := range []string{"ammoBuy", "ammoShops", "ammoItems", "ammoBuyAll", "ammoPriceCaps", "ammoStopAbove", "ammoCoins", "ammoMinVisitCoins", "ammoSlotReserve"} {
		if !strings.Contains(keys, k) {
			t.Errorf("setting %s is missing", k)
		}
	}
}

// ---- the round's purse ----

func ammoCostOf(per int) func(Station, time.Time) int {
	return func(s Station, _ time.Time) int { return per }
}

// The ammo stops are valued (RoundNeed.Ammo, held to the ammoCoins budget the round has left) but are NOT a need:
// PackNeed, Est and Total, which size the mule ask, the draw, the top-ups, the market float and Lundail's keep,
// are what they were without ammo. A purse short of the runes must not be asked to carry ammo ahead of them.
func TestPlanRoundCoinsCountsAmmo(t *testing.T) {
	runes, caps := roundRunes()
	ring := liveRing([]string{SiteGerrant, SiteLowe, SiteHickton})
	in := PurseInput{Ring: ring, From: 3, Now: time.Now(), Runes: runes, Caps: caps, Fares: DefaultFareCoins, StaffCost: StaffEstCost(2, 1600)}
	without := PlanRoundCoins(in)
	in.AmmoCost = ammoCostOf(20000)
	with := PlanRoundCoins(in)
	// 12 rune pairs' ammo stops: Gerrant x3, Lowe x3, Hickton at the far end (the near one is before the draw).
	if with.Ammo != 7*20000 {
		t.Errorf("ammo %d for 7 stops of 20,000", with.Ammo)
	}
	if with.Pack != without.Pack || with.Lundail != without.Lundail || with.Staffs != without.Staffs || with.Margin() != without.Margin() {
		t.Errorf("the rune figures moved:\n%s\n%s", without, with)
	}
	if with.PackNeed() != without.PackNeed() || with.Est() != without.Est() || with.Total() != without.Total() {
		t.Errorf("ammo moved PackNeed/Est/Total: %d/%d/%d against %d/%d/%d", with.PackNeed(), with.Est(), with.Total(),
			without.PackNeed(), without.Est(), without.Total())
	}
	// So nothing a purse is sized from sees it: the market float, the mule ask and Lundail's keep.
	if ShopFloat(with.Est(), 100000, DefaultFareCoins) != ShopFloat(without.Est(), 100000, DefaultFareCoins) {
		t.Error("the market station's float carries the ammo")
	}
	wb, wa := CircuitTopUp(true, 150000, 500000, with.Est(), 10000, DefaultFareCoins, 400000)
	nb, na := CircuitTopUp(true, 150000, 500000, without.Est(), 10000, DefaultFareCoins, 400000)
	if wb != nb || wa != na {
		t.Errorf("the mule ask carries the ammo: %d/%d against %d/%d", wb, wa, nb, na)
	}
	if with.BankKeep(600000, 10000) != without.BankKeep(600000, 10000) {
		t.Error("Lundail's keep moved with the ammo")
	}
	if ReserveUncovered(map[string]int{"Nature rune": 100000}, with.PackNeed(), 200000)["Nature rune"] !=
		ReserveUncovered(map[string]int{"Nature rune": 100000}, without.PackNeed(), 200000)["Nature rune"] {
		t.Error("the rune reserve moved with the ammo")
	}
	if with.Stations != without.Stations+7 {
		t.Errorf("stations %d, want %d", with.Stations, without.Stations+7)
	}
	if !strings.Contains(with.String(), "ammo=140000") || !strings.Contains(without.String(), "ammo=0") {
		t.Errorf("log form %q / %q", with.String(), without.String())
	}
	// The budget caps the figure.
	in.AmmoLimit = 100000
	if got := PlanRoundCoins(in).Ammo; got != 100000 {
		t.Errorf("limit 100,000: ammo %d", got)
	}
	in.AmmoLimit = 500000
	if got := PlanRoundCoins(in).Ammo; got != 140000 {
		t.Errorf("a limit above the estimate: ammo %d", got)
	}
	// A station that cannot be visited is not counted; from the middle of the round only what is left.
	in.AmmoLimit = 0
	in.Skip = func(s Station) bool { return s.Kind == KindAmmo && s.Site == SiteLowe }
	if got := PlanRoundCoins(in).Ammo; got != 4*20000 {
		t.Errorf("Lowe skipped: ammo %d", got)
	}
	in.Skip = nil
	in.From = Index(ring, "Aubury@3")
	if got := PlanRoundCoins(in).Ammo; got != 2*20000 { // Lowe@3 and Hickton@3
		t.Errorf("from Aubury@3: ammo %d", got)
	}
	// With ammo off (no cost function) a ring that still carries ammo stations counts none of them: a restart
	// with the setting switched off mid-round.
	in.From = 3
	in.AmmoCost = nil
	if got := PlanRoundCoins(in); got.Ammo != 0 || got.Stations != without.Stations {
		t.Errorf("no cost function: %s", got)
	}
}

// AmmoDrawShare funds the ammo estimate only from the coins left once the whole rune need is covered, and a
// purse short of the runes gets exactly the draw it would get without ammo.
func TestAmmoDrawShareIsSurplusOnly(t *testing.T) {
	const fare = 300
	for _, c := range []struct {
		name                                string
		ammo, need, inPack, bankSpare, want int
	}{
		{"flush bank, empty pack: the whole estimate", 100000, 1000000, 0, 3000000, 100000},
		{"bank covers the need, the fare and half the estimate", 100000, 1000000, 0, 1050300, 50000},
		{"bank covers the need but not the fare", 100000, 1000000, 0, 1000000, 0},
		{"bank short of the need: nothing", 100000, 1000000, 0, 600000, 0},
		{"pack plus bank short of the need: nothing", 100000, 1000000, 200300, 700000, 0},
		{"a full pack and a small bank", 100000, 1000000, 1000300, 40000, 40000},
		{"a full pack and no bank", 100000, 1000000, 1000300, 0, 0},
		{"a pack over the need and no bank", 100000, 1000000, 1300300, 0, 100000},
		{"no estimate", 0, 1000000, 0, 3000000, 0},
		{"negative bank spare", 100000, 1000000, 1000300, -5000, 0},
		{"no rune need at all", 100000, 0, 300, 70000, 70000},
	} {
		if got := AmmoDrawShare(c.ammo, c.need, c.inPack, fare, c.bankSpare); got != c.want {
			t.Errorf("%s: share %d, want %d", c.name, got, c.want)
		}
	}
	// Through DrawAmount: a short bank draws what it always did; a flush one draws the ammo on top.
	draw := func(ammo, bank, inPack int) Draw {
		need := 1000000
		share := AmmoDrawShare(ammo, need, inPack, fare, bank-10000)
		return DrawAmount(DrawInput{TripCoins: 1500000, BankCoins: bank, CoinReserve: 10000, FareCoins: fare, InPack: inPack, EstCost: need + share})
	}
	if draw(100000, 600000, 0) != draw(0, 600000, 0) {
		t.Error("a bank short of the runes draws differently with ammo")
	}
	if got, base := draw(100000, 3000000, 0), draw(0, 3000000, 0); got.Amount != base.Amount+100000 {
		t.Errorf("a flush bank: draw %d, want %d and the 100000 of ammo", got.Amount, base.Amount)
	}
	// tripCoins still bounds the draw: a need at the trip limit leaves no room for ammo.
	if got := DrawAmount(DrawInput{TripCoins: 1000000, BankCoins: 3000000, CoinReserve: 10000, FareCoins: fare, InPack: 0, EstCost: 1000000 + 100000}); got.Amount != 1000000+fare {
		t.Errorf("tripCoins: draw %d", got.Amount)
	}
}

// A round that has left runes on a shelf, or passed a station, for coins buys no more ammo.
func TestAmmoWaitsForRunes(t *testing.T) {
	st := NewState()
	if waits, _ := AmmoWaitsForRunes(st); waits {
		t.Error("a fresh round waits")
	}
	st.Done["Betty@2"] = StationResult{Outcome: OutcomeBought, Spent: 50000}
	if waits, _ := AmmoWaitsForRunes(st); waits {
		t.Error("a round that bought everything waits")
	}
	st.Done["Aubury@2"] = StationResult{Outcome: OutcomeBought, ShortRunes: 40, ShortCoins: 12000}
	waits, why := AmmoWaitsForRunes(st)
	if !waits || !strings.Contains(why, "40 runes worth 12000") || !strings.Contains(why, "the runes come first") {
		t.Errorf("runes left for coins: %v %q", waits, why)
	}
	st = NewState()
	st.Done["Lundail@3"] = StationResult{Outcome: OutcomeSkipped, NoCoins: true}
	if waits, why = AmmoWaitsForRunes(st); !waits || !strings.Contains(why, "1 stations passed for coins") {
		t.Errorf("a station passed for coins: %v %q", waits, why)
	}
}

// ---- the delivery ----

func TestDeliverDueAmmo(t *testing.T) {
	now := time.Now()
	maxAge := 180 * time.Minute
	for _, c := range []struct {
		name         string
		held, ammo   int
		last         time.Time
		due          bool
		reasonHas    string
		viaPlainRule bool
	}{
		{"runes over the minimum", 400, 0, now.Add(-time.Minute), true, "deliverMinRunes", true},
		{"runes over the minimum with ammo", 400, 3000, now.Add(-time.Minute), true, "deliverMinRunes", true},
		{"ammo alone is not worth a trade", 0, 3000, now.Add(-time.Minute), false, "ammo parts", false},
		{"runes under the minimum and ammo", 20, 3000, now.Add(-time.Minute), false, "under deliverMinRunes", false},
		{"ammo alone, never delivered", 0, 3000, time.Time{}, true, "nothing delivered yet", false},
		{"ammo alone, last delivery old", 0, 3000, now.Add(-4 * time.Hour), true, "the last delivery was", false},
		{"nothing held at all", 0, 0, now.Add(-4 * time.Hour), false, "nothing held", true},
		{"a few runes, old delivery", 20, 0, now.Add(-4 * time.Hour), true, "the last delivery was", true},
	} {
		due, why := DeliverDueAmmo(c.held, c.ammo, 300, c.last, now, maxAge)
		if due != c.due || !strings.Contains(why, c.reasonHas) {
			t.Errorf("%s: due %v (%s), want %v with %q", c.name, due, why, c.due, c.reasonHas)
		}
		if c.viaPlainRule {
			if pd, _ := DeliverDue(c.held, 300, c.last, now, maxAge); pd != due {
				t.Errorf("%s: DeliverDue says %v", c.name, pd)
			}
		}
	}
}

// 3,000 Feathers must not send a hop or a trade for zero runes (the gate reads the runes), yet the ammo goes along
// whenever a delivery is due.
func TestDeliverDecisionIgnoresAmmoForSize(t *testing.T) {
	now := time.Now()
	in := DeliverInput{Held: 0, HeldAmmo: 3000, LastDelivery: now.Add(-20 * time.Minute), Now: now, MinRunes: 300, MaxAge: 180 * time.Minute,
		MuleWorld: 1, MuleWorldKnown: true, MyWorld: 1, MuleHeard: true, HopAllowed: true}
	if v := DeliverDecision(in); v.Action != DeliverSkip {
		t.Errorf("ammo alone, delivered 20 minutes ago: %+v", v)
	}
	in.MyWorld = 3
	if v := DeliverDecision(in); v.Action != DeliverSkip {
		t.Errorf("ammo alone on another world must not hop: %+v", v)
	}
	in.Held = 600
	if v := DeliverDecision(in); v.Action != DeliverHop {
		t.Errorf("600 runes on another world hops: %+v", v)
	}
	in.MyWorld = 1
	if v := DeliverDecision(in); v.Action != DeliverNow {
		t.Errorf("600 runes and ammo: %+v", v)
	}
	in.Held, in.LastDelivery = 0, now.Add(-5*time.Hour)
	if v := DeliverDecision(in); v.Action != DeliverNow {
		t.Errorf("ammo alone after the age limit: %+v", v)
	}
}

func TestDeliverablesSplit(t *testing.T) {
	pack := map[string]int{"Mind rune": 100, "Coins": 50, "Bolts": 2000, "feather": 1000}
	bank := map[string]int{"mind rune": 200, "Steel arrowtips": 330, "Staff of air": 4}
	runes := []string{"Mind rune", "Staff of air"}
	ammo := []string{"Bolts", "Steel arrowtips", "Feather", "Arrow shaft"}
	by, r, a := DeliverablesSplit(pack, bank, runes, ammo)
	if r != 304 || a != 3330 || by["Mind rune"] != 300 || by["Staff of air"] != 4 || by["Bolts"] != 2000 || by["Feather"] != 1000 || by["Steel arrowtips"] != 330 {
		t.Errorf("by %v runes %d ammo %d", by, r, a)
	}
	if _, has := by["Arrow shaft"]; has {
		t.Errorf("an item held none of is listed: %v", by)
	}
	// MuleRules lets the ammo cross, and nothing else new.
	rules := MuleRules(append(append([]string{}, runes...), ammo...))
	// The real object ids: donorplan keeps every untradeable object (id 1 is one) at home.
	for name, id := range map[string]int{"Bolts": 877, "Steel arrowtips": 41, "Feather": 314, "Arrow shaft": 52} {
		if rules.Stays(donorplan.ItemRow{Name: name, ID: id, Count: 1}) {
			t.Errorf("%s stays behind under the hand-over rules", name)
		}
	}
	for name, id := range map[string]int{"Bronze arrowtips": 39, "Coins": 995, "Iron arrow": 884, "Bronze arrow": 882} {
		if !rules.Stays(donorplan.ItemRow{Name: name, ID: id, Count: 1}) {
			t.Errorf("%s is not listed and crosses", name)
		}
	}
	// A pack of runes, bought ammo and a coin float hands over every listed stack, minds first, and keeps the coins.
	load := donorplan.PlanLoad([]donorplan.ItemRow{
		{Name: "Coins", ID: 995, Count: 900000}, {Name: "Bolts", ID: 877, Count: 2380}, {Name: "Feather", ID: 314, Count: 3000},
		{Name: "Steel arrowtips", ID: 41, Count: 330}, {Name: "Mind rune", ID: 558, Count: 2664}, {Name: "Staff of air", ID: 1381, Count: 1},
		{Name: "Bronze arrowtips", ID: 39, Count: 10},
	}, donorplan.DefaultMuleFreeSlots, rules)
	var names []string
	for _, g := range load {
		names = append(names, g.Name)
	}
	if len(load) != 5 || names[0] != "Mind rune" {
		t.Errorf("load %v", names)
	}
}
