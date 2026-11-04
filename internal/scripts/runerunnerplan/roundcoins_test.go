package runerunnerplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

// liveRoundRing is the live ring of the one RuneRunner (13 October 2026): Magic 66, rotationPhase 2,
// worlds 1 2 3, the mule on world 1, Zaff's staffs on. Betty@3,1,2 Aubury@2,1,3 Lundail@3,1,2
// WizardsGuild@2,1,3.
func liveRoundRing() []Station {
	return BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2, 3}, Shops: DefaultSites(), Phase: 2, Staffs: true})
}

func roundRunes() ([]string, Caps) {
	runes := FullStockRunes(DefaultSites())
	return runes, BuyAllCaps(runes)
}

func fullRound(t *testing.T) (RoundNeed, []Station) {
	t.Helper()
	runes, caps := roundRunes()
	ring := liveRoundRing()
	return PlanRoundCoins(PurseInput{
		Ring: ring, From: 2, Now: time.Now(), Runes: runes, Caps: caps, Fares: DefaultFareCoins,
		StaffCost: StaffEstCost(2, 1600),
	}), ring
}

// A round whose pairs nobody has looked at (a fleet restart empties the board) is every shelf full:
// the investigation of 13 October priced it at 5,490,585 pack-paid (Betty 316,165 + Aubury 412,165 +
// Wizards' Guild 1,101,865, times three worlds), 384,603 for Lundail and 9,090 for Zaff. The margin
// never takes a pair above a full shelf, so a full round carries none.
func TestPlanRoundCoinsFullShelves(t *testing.T) {
	n, ring := fullRound(t)
	if n.Pack != 5490585 || n.Lundail != 384603 || n.Staffs != 9090 {
		t.Fatalf("full round: pack %d lundail %d staffs %d", n.Pack, n.Lundail, n.Staffs)
	}
	if n.Margin() != 0 {
		t.Errorf("a full shelf cannot cost more: margin %d", n.Margin())
	}
	if want := 5490585 + 384603 + 9090; n.Est() != want || n.Total() != want+DefaultFareCoins {
		t.Errorf("est %d total %d, want %d and %d", n.Est(), n.Total(), want, want+DefaultFareCoins)
	}
	if n.PackNeed() != 5490585+9090 {
		t.Errorf("pack need %d", n.PackNeed())
	}
	// 4 shops x 3 worlds + 3 Zaff stations.
	if n.Stations != 15 {
		t.Errorf("stations %d in %s", n.Stations, PlanString(ring, 0))
	}
	if !strings.Contains(n.String(), "round_need=5884578") {
		t.Errorf("log form %q", n.String())
	}
	// Fits what a single ask can carry: the purse target is under MaxCircuitFunds, and an ask for
	// the whole of it from an empty bank is under MaxTopUpAmount.
	below, amount := CircuitTopUp(true, DefaultTopUpBelow, DefaultTopUpAmount, n.Est(), DefaultCoinReserve, DefaultFareCoins, 0)
	if below != n.Total()+DefaultCoinReserve || below > MaxCircuitFunds {
		t.Errorf("below %d for a total of %d", below, n.Total())
	}
	if amount != below || amount > MaxTopUpAmount {
		t.Errorf("a full-shelf ask from nothing is %d (below %d, max %d)", amount, below, MaxTopUpAmount)
	}
}

// Steady state: every shelf was bought empty one round ago and has regrown by the time the round
// reaches it. The investigation priced a 38-minute gap at Betty 58,080, Aubury 58,080, Wizards' Guild
// 43,692 and Lundail 21,150 per world; the margin is 20% of each.
func TestPlanRoundCoinsSteadyState(t *testing.T) {
	runes, caps := roundRunes()
	ring := liveRoundRing()
	gap := 38 * time.Minute
	tick := 600 * time.Millisecond
	now := time.Now()
	regrown := func(site ShopSite, world int, at time.Time) map[string]int {
		out := map[string]int{}
		for _, r := range runes {
			if l, ok := site.Shop.Line(r); ok {
				out[r] = l.EstimateStock(0, at.Add(-gap), at, tick)
			}
		}
		return out
	}
	n := PlanRoundCoins(PurseInput{Ring: ring, From: 2, Now: now, Runes: runes, Caps: caps, Fares: DefaultFareCoins,
		StaffCost: StaffEstCost(2, 1600), Stock: regrown})
	if want := 3 * (58080 + 58080 + 43692); n.Pack != want {
		t.Errorf("pack %d, want %d", n.Pack, want)
	}
	if want := 3 * 21150; n.Lundail != want {
		t.Errorf("lundail %d, want %d", n.Lundail, want)
	}
	// 20% on each station, so within a coin per station of 20% of the whole.
	if want := (n.Pack + n.Lundail) / 5; n.Margin() < want-15 || n.Margin() > want+15 {
		t.Errorf("margin %d, want about %d", n.Margin(), want)
	}
	// The ask for it, with the bank at the reserve and nothing in the pack.
	below, amount := CircuitTopUp(true, DefaultTopUpBelow, DefaultTopUpAmount, n.Est(), DefaultCoinReserve, DefaultFareCoins, DefaultCoinReserve)
	if below != n.Total()+DefaultCoinReserve || amount != n.Total() {
		t.Errorf("below %d amount %d for a total of %d", below, amount, n.Total())
	}
	// The pack draw is the pack-paid part with its margin, one draw under tripCoins.
	d := DrawAmount(DrawInput{TripCoins: DefaultTripCoins, BankCoins: 2000000, CoinReserve: DefaultCoinReserve, FareCoins: DefaultFareCoins, EstCost: n.PackNeed()})
	if d.Amount != n.PackNeed()+DefaultFareCoins || n.PackNeed() >= DefaultTripCoins {
		t.Errorf("draw %+v for a pack need of %d", d, n.PackNeed())
	}
}

// A stop at the shop's own bank is worth it when the pack cannot pay THIS station its expected cost and
// the bank can add to it, even when the shortfall is a small share of the round.
func TestStationShortFetchesCoinsTheBankHolds(t *testing.T) {
	n, _ := fullRound(t)
	if n.Next != 316165 { // Betty, the first pack-paid station of the ring, off a full shelf
		t.Fatalf("next %d", n.Next)
	}
	cases := []struct {
		name               string
		inPack, next, fare int
		bankRead           bool
		bankCoins, reserve int
		want               bool
	}{
		{"pack pays it", 330000, 316165, 300, true, 900000, 10000, false},
		{"20k short, bank holds more", 296465, 316165, 300, true, 900000, 10000, true},
		{"short, bank at its reserve", 296465, 316165, 300, true, 10300, 10000, false},
		{"short, bank barely above reserve", 296465, 316165, 300, true, 10500, 10000, false},
		{"short, bank never read", 296465, 316165, 300, false, 0, 10000, true},
		{"under the margin", 315600, 316165, 300, true, 900000, 10000, false},
		{"nothing expected here", 0, 0, 300, true, 900000, 10000, false},
		{"empty pack", 0, 316165, 300, true, 900000, 10000, true},
		{"a big fare is no reason to walk to an empty bank", 1000000, 1000000, 5000, true, 10000, 10000, false},
	}
	for _, c := range cases {
		if got := StationShort(c.inPack, c.next, c.fare, c.bankRead, c.bankCoins, c.reserve); got != c.want {
			t.Errorf("%s: StationShort = %v, want %v", c.name, got, c.want)
		}
	}
}

// The same shelves valued at the draw instant (no lead) come to about half: the live rounds spent
// 1.77, 2.13 and 2.15 times what they were estimated at.
func TestPlanRoundCoinsProjectsToTheArrival(t *testing.T) {
	runes, caps := roundRunes()
	ring := liveRoundRing()
	tick := 600 * time.Millisecond
	now := time.Now()
	// The shelves were last seen empty at `now`, so a station reached later has regrown by its lead.
	proj := func(site ShopSite, world int, at time.Time) map[string]int {
		out := map[string]int{}
		for _, r := range runes {
			if l, ok := site.Shop.Line(r); ok {
				out[r] = l.EstimateStock(0, now, at, tick)
			}
		}
		return out
	}
	later := PlanRoundCoins(PurseInput{Ring: ring, From: 2, Now: now, Runes: runes, Caps: caps, Stock: proj})
	atDraw := PlanRoundCoins(PurseInput{Ring: ring, From: 2, Now: now, Runes: runes, Caps: caps,
		Stock: func(site ShopSite, world int, _ time.Time) map[string]int { return proj(site, world, now) }})
	if atDraw.Pack != 0 || later.Pack <= 0 {
		t.Fatalf("draw-instant pack %d, projected pack %d", atDraw.Pack, later.Pack)
	}
	// Later stations cost more than earlier ones: the lead grows along the ring.
	first := PlanRoundCoins(PurseInput{Ring: ring, From: 2, Now: now, Runes: runes, Caps: caps, Stock: proj, Skip: func(s Station) bool { return s.Kind != KindBuy || s.Site != SiteBetty || s.World != 3 }})
	last := PlanRoundCoins(PurseInput{Ring: ring, From: 2, Now: now, Runes: runes, Caps: caps, Stock: proj, Skip: func(s Station) bool { return s.Kind != KindBuy || s.Site != SiteWizardsGuild || s.World != 3 }})
	if first.Pack <= 0 || first.Pack >= later.Pack || last.Pack <= first.Pack {
		t.Errorf("first station %d, last station %d, all %d", first.Pack, last.Pack, later.Pack)
	}
}

func TestStationLead(t *testing.T) {
	prev := time.Duration(0)
	for pos := 0; pos < 20; pos++ {
		l := StationLead(pos, 20)
		if l <= prev || l > RoundLeadMax {
			t.Fatalf("lead(%d) = %s after %s", pos, l, prev)
		}
		prev = l
	}
	if StationLead(19, 20) != RoundLeadMax || StationLead(0, 1) != RoundLeadMax {
		t.Error("the last station is reached at RoundLeadMax")
	}
	if StationLead(-1, 5) != 0 || StationLead(0, 0) != 0 || StationLead(9, 5) != RoundLeadMax {
		t.Error("out-of-range leads")
	}
}

// A station the account cannot visit is not counted; neither are the draw, hops or market.
func TestPlanRoundCoinsSkipsWhatCannotBeVisited(t *testing.T) {
	runes, caps := roundRunes()
	ring := liveRoundRing()
	all, _ := fullRound(t)
	noLundail := PlanRoundCoins(PurseInput{Ring: ring, From: 2, Now: time.Now(), Runes: runes, Caps: caps, Fares: DefaultFareCoins,
		StaffCost: StaffEstCost(2, 1600), Skip: func(s Station) bool { return s.Kind == KindLundail }})
	if noLundail.Lundail != 0 || noLundail.Pack != all.Pack || noLundail.Stations != all.Stations-3 {
		t.Errorf("without Lundail: %+v of %+v", noLundail, all)
	}
	pairOff := PlanRoundCoins(PurseInput{Ring: ring, From: 2, Now: time.Now(), Runes: runes, Caps: caps, Fares: DefaultFareCoins,
		StaffCost: StaffEstCost(2, 1600), Skip: func(s Station) bool { return s.Key() == "Aubury@2" || s.Key() == "Zaff@2" }})
	if pairOff.Pack != all.Pack-412165 || pairOff.Staffs != 2*3030 {
		t.Errorf("without Aubury@2: pack %d staffs %d", pairOff.Pack, pairOff.Staffs)
	}
	// From the cursor on: the stations already done are not counted.
	cut := 0
	for i, s := range ring {
		if s.Key() == "Aubury@2" {
			cut = i
		}
	}
	rest := PlanRoundCoins(PurseInput{Ring: ring, From: cut, Now: time.Now(), Runes: runes, Caps: caps, StaffCost: StaffEstCost(2, 1600)})
	if rest.Pack != all.Pack-3*316165 || rest.Stations >= all.Stations {
		t.Errorf("from Aubury@2: pack %d, stations %d", rest.Pack, rest.Stations)
	}
	// A negative start counts from the top; an empty ring needs nothing.
	if n := PlanRoundCoins(PurseInput{Ring: ring, From: -4, Now: time.Now(), Runes: runes, Caps: caps}); n.Pack != all.Pack {
		t.Errorf("from -4: %d", n.Pack)
	}
	if n := PlanRoundCoins(PurseInput{Fares: 300}); n.Total() != 300 || n.Stations != 0 {
		t.Errorf("empty ring: %+v", n)
	}
}

// The margin is a fifth of a station's cost but never lifts it past a full shelf.
func TestStationNeedMarginIsCappedAtAFullShelf(t *testing.T) {
	runes, caps := roundRunes()
	sh := fleetplan.Aubury
	full := FullShelfCost(sh, runes, caps)
	if full != 412165 {
		t.Fatalf("Aubury's full shelf %d", full)
	}
	half := map[string]int{}
	for r, n := range BaseStock(sh, runes) {
		half[r] = n / 2
	}
	cost, margin := stationNeed(StationEstimate{Shop: sh, Stock: half}, runes, caps)
	if margin != cost*RoundMarginPercent/100 || cost+margin >= full {
		t.Errorf("half shelf: cost %d margin %d (full %d)", cost, margin, full)
	}
	nearly := map[string]int{}
	for r, n := range BaseStock(sh, runes) {
		nearly[r] = n * 95 / 100
	}
	cost, margin = stationNeed(StationEstimate{Shop: sh, Stock: nearly}, runes, caps)
	if cost+margin != full {
		t.Errorf("95%% shelf: cost %d + margin %d should stop at the full shelf %d", cost, margin, full)
	}
	if cost, margin = stationNeed(StationEstimate{Shop: sh, Stock: BaseStock(sh, runes)}, runes, caps); margin != 0 || cost != full {
		t.Errorf("full shelf: cost %d margin %d", cost, margin)
	}
	if cost, margin = stationNeed(StationEstimate{Shop: sh}, runes, caps); cost != 0 || margin != 0 {
		t.Errorf("empty shelf: cost %d margin %d", cost, margin)
	}
}

// Lundail's Gundai withdrawal is its purchase and a fifth more, never past a full shelf or the bank.
func TestLundailWithdraw(t *testing.T) {
	cases := []struct {
		plan, full, spend, want int
	}{
		{20950, 128201, 500000, 25140},   // the live acct152 plan: 608 runes planned, 713 seen
		{0, 128201, 500000, 0},           // nothing planned, nothing drawn
		{120000, 128201, 500000, 128201}, // never above a full shelf
		{20950, 128201, 22000, 22000},    // nor above what the bank spares
		{20950, 0, 500000, 25140},        // no full-shelf figure: just the margin
		{20950, 128201, 10000, 20950},    // a plan already made is never cut below itself
	}
	for _, c := range cases {
		if got := LundailWithdraw(c.plan, c.full, c.spend); got != c.want {
			t.Errorf("LundailWithdraw(%d, %d, %d) = %d, want %d", c.plan, c.full, c.spend, got, c.want)
		}
	}
	runes, caps := roundRunes()
	if full := FullShelfCost(fleetplan.Lundail, runes, caps); full != 128201 {
		t.Errorf("Lundail's full shelf %d", full)
	}
}

// A purse that cannot cover the round keeps Lundail a pro-rata share instead of letting the pack-paid
// draw take every coin above the reserve; a purse that can keeps all of it.
func TestBankKeepSharesAShortPurse(t *testing.T) {
	n := RoundNeed{Pack: 400000, PackMargin: 80000, Lundail: 100000, LundailMargin: 20000}
	if n.Est() != 600000 {
		t.Fatalf("est %d", n.Est())
	}
	if k := n.BankKeep(2000000, 10000); k != 120000 {
		t.Errorf("a rich bank keeps Lundail's whole share, kept %d", k)
	}
	if k := n.BankKeep(310000, 10000); k != 60000 { // 300,000 spare of 600,000: half of Lundail's 120,000
		t.Errorf("a half-funded bank keeps %d, want 60000", k)
	}
	if k := n.BankKeep(10000, 10000); k != 0 {
		t.Errorf("a bank at its reserve keeps %d", k)
	}
	if k := (RoundNeed{Pack: 5}).BankKeep(900000, 10000); k != 0 {
		t.Errorf("no Lundail, no keep: %d", k)
	}
	// What the pack-paid draw then sees is spare less the keep, and it still draws something.
	keep := n.BankKeep(310000, 10000)
	d := DrawAmount(DrawInput{TripCoins: DefaultTripCoins, BankCoins: 310000 - keep, CoinReserve: 10000, FareCoins: 300, EstCost: n.PackNeed()})
	if d.NoCoins || d.Amount != 240000 {
		t.Errorf("draw %+v", d)
	}
}

// The market may spend only what the shops do not need: the round's estimate less the pack's coins
// beyond the fare.
func TestShopFloat(t *testing.T) {
	if f := ShopFloat(900000, 300000+300, 300); f != 600000 {
		t.Errorf("float %d", f)
	}
	if f := ShopFloat(900000, 5000000, 300); f != 0 {
		t.Errorf("a pack that carries it all leaves no float: %d", f)
	}
	if f := ShopFloat(900000, 100, 300); f != 900000 {
		t.Errorf("a pack under its fare carries nothing: %d", f)
	}
}

// The worst case still fits the ask machinery: a full-shelf round from an empty bank is asked in
// at most TopUpMaxOrders orders at the live mule's 500,000 a order.
func TestWorstCaseAskFitsTheOrders(t *testing.T) {
	n, _ := fullRound(t)
	_, amount := CircuitTopUp(true, DefaultTopUpBelow, DefaultTopUpAmount, n.Est(), DefaultCoinReserve, DefaultFareCoins, 0)
	got, orders, last := 0, 0, 0
	for {
		want := TopUpNextOrder(amount, got, orders, last)
		if want <= 0 {
			break
		}
		if want > TopUpOrderMax {
			t.Fatalf("order of %d is over the order grammar's ceiling %d", want, TopUpOrderMax)
		}
		last = min(want, 500000) // the live mule clamps each order to 500,000
		got += last
		orders++
	}
	if got < amount {
		t.Errorf("asked %d, a 500,000-a-order mule delivered %d in %d orders (TopUpMaxOrders %d)", amount, got, orders, TopUpMaxOrders)
	}
}

// A short purse's Lundail share is worked out once, at the draw, and held through the shop-bank top-ups.
// Review of 13 October: with a 610,000-coin bank against the 5.9M full-shelf round the keep was a
// fresh pro-rata share of whatever the bank then held, so it fell 39,216 -> about 2,500 -> under 200 and Lundail
// met 190 coins at Gundai.
func TestLundailKeepSurvivesTheShopBankTopUps(t *testing.T) {
	n, _ := fullRound(t)
	const reserve = 10000
	bank := 610000
	stored := n.BankKeep(bank, reserve)
	if stored < 39000 || stored > 40000 {
		t.Fatalf("the draw's keep %d, want about 39,216", stored)
	}
	// The draw, then two shop-bank top-ups, each with an empty pack. The first leg draws all the spare
	// above the reserve and the keep; the legs after it find the bank at reserve + keep.
	for leg := 0; leg < 3; leg++ {
		keep := stored
		if leg > 0 {
			keep = n.KeepNow(stored)
		}
		d := DrawAmount(DrawInput{TripCoins: DefaultTripCoins, BankCoins: bank - keep, CoinReserve: reserve, FareCoins: DefaultFareCoins, EstCost: n.PackNeed()})
		bank -= d.Amount
		if bank < reserve+stored {
			t.Fatalf("leg %d left the bank at %d, under the reserve %d plus Lundail's keep %d", leg, bank, reserve, stored)
		}
		if leg > 0 && (!d.NoCoins || d.Amount != 0) {
			t.Errorf("leg %d: a bank holding only the keep should draw nothing, drew %+v", leg, d)
		}
	}
	if bank != reserve+stored {
		t.Errorf("bank %d, want reserve + keep %d", bank, reserve+stored)
	}

	// The keep falls away with the stations it is for: less of Lundail ahead than was kept, only that
	// much is held; none left, none held.
	low := n
	low.Lundail, low.LundailMargin = n.Lundail/200, 0 // 1,923 left, under the stored keep
	if k := low.KeepNow(stored); k != low.Lundail {
		t.Errorf("keep with %d of Lundail ahead: %d", low.Lundail, k)
	}
	none := n
	none.Lundail, none.LundailMargin = 0, 0
	if k := none.KeepNow(stored); k != 0 {
		t.Errorf("Lundail passed, keep %d", k)
	}
	if k := n.KeepNow(0); k != 0 {
		t.Errorf("no stored keep, kept %d", k)
	}
	if k := n.KeepNow(-5); k != 0 {
		t.Errorf("negative keep %d", k)
	}
	// Once Lundail has passed the last top-up may take the keep's coins.
	d := DrawAmount(DrawInput{TripCoins: DefaultTripCoins, BankCoins: bank - none.KeepNow(stored), CoinReserve: reserve, FareCoins: DefaultFareCoins, EstCost: n.PackNeed()})
	if d.NoCoins || d.Amount != stored {
		t.Errorf("after Lundail the keep is free: %+v, want %d", d, stored)
	}
}

// The visit's reserve for the higher-priority runes ahead is cut to the share of the stations ahead the
// bank cannot fund. First full-shelf round, the Wizards' Guild with two more guild visits ahead and a
// pack at tripCoins: the half-purse hold left Death, Nature and Law on the shelf (about 350,000 coins
// at each visit) while the bank held the float that the next top-up would have brought.
func TestReserveUncoveredLetsTheBankFundTheTail(t *testing.T) {
	runes, caps := roundRunes()
	guild := fleetplan.WizardsGuild
	full := FullShelfCost(guild, runes, caps)
	stock := BaseStock(guild, runes)
	ahead := []StationEstimate{{Shop: guild, Stock: stock}, {Shop: guild, Stock: stock}}
	reserve := ReserveAhead(ahead, runes, caps)
	spendable := DefaultTripCoins - DefaultFareCoins

	held := PlanVisit(guild, stock, spendable, runes, caps, reserve)
	if held.Coins >= full {
		t.Fatalf("setup: the raw reserve should leave part of the shelf, spent %d of %d", held.Coins, full)
	}
	t.Logf("raw reserve: the guild visit spends %d of a %d shelf, leaving %d coins of runes", held.Coins, full, full-held.Coins)

	aheadNeed := 2 * full
	if got := ReserveUncovered(reserve, aheadNeed, aheadNeed); got != nil {
		t.Errorf("a bank that covers the stations ahead holds nothing back: %v", got)
	}
	free := PlanVisit(guild, stock, spendable, runes, caps, ReserveUncovered(reserve, aheadNeed, 3*aheadNeed))
	if free.Coins != full {
		t.Errorf("with the bank holding the float the visit spent %d, want the full shelf %d", free.Coins, full)
	}

	// Nothing in the bank to cover: the hold stands, whole.
	if got := ReserveUncovered(reserve, aheadNeed, 0); len(got) != len(reserve) {
		t.Fatalf("an empty bank changed the reserve: %v", got)
	}
	for r, hold := range reserve {
		if ReserveUncovered(reserve, aheadNeed, 0)[r] != hold || ReserveUncovered(reserve, aheadNeed, -50)[r] != hold {
			t.Errorf("%s: hold %d changed with an empty bank", r, hold)
		}
	}
	// Half covered: each hold is halved, so a purse that is genuinely short still protects the dear runes.
	halved := ReserveUncovered(reserve, aheadNeed, aheadNeed/2)
	for r, hold := range reserve {
		if want := hold / 2; halved[r] < want-1 || halved[r] > want+1 {
			t.Errorf("%s: half-covered hold %d, want about %d of %d", r, halved[r], want, hold)
		}
	}
	// Degenerate inputs leave the reserve as it was.
	if got := ReserveUncovered(nil, 100, 0); got != nil {
		t.Errorf("no reserve: %v", got)
	}
	if got := ReserveUncovered(reserve, 0, 50); len(got) != len(reserve) {
		t.Errorf("nothing ahead: %v", got)
	}
}

// The round's full bill is the rune, staff and Lundail need, the fares and the ammo stations' estimate beside it.
// The 14 October 2026 purse line: round_need=1099730 ... ammo=27750.
func TestRoundNeedBillCarriesTheAmmo(t *testing.T) {
	n := RoundNeed{Pack: 824635, Lundail: 83986, Staffs: 9090, PackMargin: 150000, LundailMargin: 31719, Ammo: 27750, Fares: 300}
	if n.Total() != n.Est()+300 || n.Bill() != n.Total()+27750 {
		t.Fatalf("total %d bill %d", n.Total(), n.Bill())
	}
	if n.Bill()-n.Total() != n.Ammo {
		t.Errorf("the bill is the total and the ammo: %d", n.Bill())
	}
	// The pack-paid figures the shop-bank top-ups, the float and Lundail's keep read do not move with ammo.
	plain := n
	plain.Ammo = 0
	if n.PackNeed() != plain.PackNeed() || n.Est() != plain.Est() || n.Total() != plain.Total() {
		t.Errorf("ammo moved the rune need: %d/%d/%d against %d/%d/%d", n.PackNeed(), n.Est(), n.Total(), plain.PackNeed(), plain.Est(), plain.Total())
	}
	// The mule is asked to fill the bill, so a runner at the rune total alone (the old target) is still under it.
	below, _ := CircuitTopUp(true, DefaultTopUpBelow, DefaultTopUpAmount, n.Est()+n.Ammo, DefaultCoinReserve, DefaultFareCoins, 0)
	if below != n.Bill()+DefaultCoinReserve {
		t.Errorf("below %d, want the bill and the reserve %d", below, n.Bill()+DefaultCoinReserve)
	}
}

// HoldAhead is what an ammo stop leaves for the runes ahead once the bank is counted. LIVE 14 October 2026 13:2x: the pack
// held 824,379, the runes ahead needed 949,886 and the bank 764,906 (about 670,000 of it spare): the old hold was the
// whole 949,886 and all three ammo stops skipped.
func TestHoldAheadCountsTheBank(t *testing.T) {
	for _, c := range []struct {
		name      string
		need      int
		bankRead  bool
		bankSpare int
		want      int
	}{
		{"the 14 Oct round: the bank covers most of it", 949886, true, 670000, 279886},
		{"a bank that holds the whole need holds nothing back", 949886, true, 11000000, 0},
		{"a bank that holds exactly the need", 949886, true, 949886, 0},
		{"an empty bank holds all of it back", 949886, true, 0, 949886},
		{"a bank under its reserve (negative spare) adds nothing", 949886, true, -5000, 949886},
		{"a bank never read adds nothing: the whole need, as before", 949886, false, 11000000, 949886},
		{"nothing ahead, nothing held", 0, true, 0, 0},
		{"a negative need is nothing", -10, false, 0, 0},
	} {
		if got := HoldAhead(c.need, c.bankRead, c.bankSpare); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

// The station's own margin is part of what the pack must carry before the visit: a pack that holds the estimate but not
// the 20% beyond it stops at the shop's bank when the bank can add, so a shelf that regrew past the estimate is not
// left "for coins" with the bank full.
func TestPlanRoundCoinsCarriesTheNextStationsMargin(t *testing.T) {
	runes, caps := roundRunes()
	ring := liveRoundRing()
	// Every shelf at half its base stock: the estimate is half a full shelf and the margin a fifth of it.
	half := func(site ShopSite, _ int, _ time.Time) map[string]int {
		out := map[string]int{}
		for r, n := range BaseStock(site.Shop, runes) {
			out[r] = n / 2
		}
		return out
	}
	n := PlanRoundCoins(PurseInput{Ring: ring, From: 2, Now: time.Now(), Runes: runes, Caps: caps, Fares: DefaultFareCoins, Stock: half})
	if n.Next <= 0 || n.NextMargin <= 0 || n.NextMargin > n.Next/4 {
		t.Fatalf("next %d margin %d", n.Next, n.NextMargin)
	}
	if want := n.Next * RoundMarginPercent / 100; n.NextMargin < want-5 || n.NextMargin > want+5 {
		t.Errorf("margin %d, want about %d", n.NextMargin, want)
	}
	if n.Margin() < n.NextMargin {
		t.Errorf("the round's margin %d is under its first station's %d", n.Margin(), n.NextMargin)
	}
	// A pack at the estimate alone is short of estimate and margin by more than NoCoinsMargin: the top-up is due.
	pack := n.Next + DefaultFareCoins
	if StationShort(pack, n.Next, DefaultFareCoins, true, 900000, DefaultCoinReserve) {
		t.Error("the estimate alone is short of itself")
	}
	if !StationShort(pack, n.Next+n.NextMargin, DefaultFareCoins, true, 900000, DefaultCoinReserve) {
		t.Errorf("a pack of the estimate (%d) is not short of the estimate and its margin (%d)", pack, n.Next+n.NextMargin)
	}
	// A full shelf cannot cost more: its margin is nothing.
	full, _ := fullRound(t)
	if full.NextMargin != 0 {
		t.Errorf("a full shelf carries a margin of %d", full.NextMargin)
	}
}
