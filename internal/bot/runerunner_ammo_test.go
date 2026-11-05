package bot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/scripts/tile"
)

// The ammo stations of the one RuneRunner (spare104, 13 October 2026 owner ask): ring, purse, gate, budget and
// the station itself. The shop visit and the pack reading are replaced (ammoBuySeam, ammoPackSeam): a rig has
// no item config to name an object, and the live visit is proved live. Boards are per world and process-wide,
// so each test works on worlds of its own.

// ammoRig is a rotation runner at the mule's post with ammo on and its round planned over worlds base..base+2
// (the mule's world is base), Magic 66, phase 2, Zaff's staffs on: the live ring with the worlds renamed.
func ammoRig(t *testing.T, name string, base int, extra map[string]interface{}) *rotRig {
	t.Helper()
	w := float64(base)
	set := map[string]interface{}{
		"worlds": []interface{}{w, w + 1, w + 2}, "rotationPhase": 2.0, "zaffStaffs": 2.0, "marketBuy": false, "ammoBuy": true,
		// The rig's ring is the one these tests were written against (Hickton at two ends); the every-world tail has its own tests.
		"ammoEveryWorld": false,
		// ... and without the 4 Nov 2026 Rope round (Khazard, Ned): ropeRig below has its own tests.
		"trawlerRope": false,
	}
	for k, v := range extra {
		set[k] = v
	}
	r := newRotRig(t, name, base, 2809, 3441, set)
	r.setMagic(66)
	r.ctx.Peers = peerbank.New()
	r.b.newRound(r.ctx, runerunnerplan.AreaOf(fleetrunner.Here(r.b.deps(r.ctx))), false)
	return r
}

// at puts the rig's round at the station with this key.
func (r *rotRig) atKey(t *testing.T, key string) {
	t.Helper()
	i := runerunnerplan.Index(r.b.st.Ring, key)
	if i < 0 {
		t.Fatalf("no %s in %s", key, runerunnerplan.PlanString(r.b.st.Ring, 0))
	}
	r.b.st.JumpTo(i)
	r.b.resumed = true
}

// pack sets what the ammo stop reads off the pack.
func (r *rotRig) pack(coins, free int, ammo map[string]int) {
	if ammo == nil {
		ammo = map[string]int{}
	}
	r.b.ammoPackSeam = func() (int, int, map[string]int) { return coins, free, ammo }
}

// shelf makes the visit a real one over this stock: the plan the bot decides is bought in full and paid at the
// shop's own prices. visits counts the walks; spentTimes multiplies the paid coins (1 is honest).
func (r *rotRig) shelf(stock map[string]int, packCoins int, visits *int, spentTimes int) {
	r.b.ammoBuySeam = func(site runerunnerplan.AmmoSite, planFor func(map[string]int, int) fleetplan.PurchasePlan) runerunner.AmmoVisit {
		if visits != nil {
			*visits++
		}
		seen := map[string]int{}
		for item, n := range stock {
			if site.Shop.Sells(item) {
				seen[item] = n
			}
		}
		plan := planFor(seen, packCoins)
		v := runerunner.AmmoVisit{Walked: true, Plan: plan, ShopVisit: fleetrunner.ShopVisit{Seen: seen, Bought: map[string]int{}}}
		for _, l := range plan.Lines {
			v.Bought[l.Rune] = l.Count
			v.Spent += site.Shop.CostToBuy(l.Rune, seen[l.Rune], l.Count) * spentTimes
		}
		return v
	}
}

// rich is a pack that covers the pack-paid rune and staff need of the WHOLE ring and has 1,300,000 coins to spare.
// The tests of the station's other rules (the mule gate, slots, the budget, strikes, the price check, the log) run
// funded: an ammo stop buys only beyond that need, and the rig's round, on full shelves, holds millions for the runes.
func (r *rotRig) rich() int { return r.b.roundNeed(r.ctx, 0).PackNeed() + r.b.set.FareCoins + 1300000 }

func (r *rotRig) runAmmo() runerunnerplan.StationResult {
	s, _ := r.b.st.Current()
	r.b.runStation(r.ctx, r.b.deps(r.ctx), s, fleetrunner.Here(r.b.deps(r.ctx)))
	return r.b.st.Done[s.Key()]
}

func hicktonShelf() map[string]int {
	return map[string]int{"Bolts": 200, "Steel arrowtips": 600, "Mithril arrowtips": 400, "Adamant arrowtips": 200, "Rune arrowtips": 100}
}

// The ring a runner with ammoBuy plans is the live one with Hickton at both ends, Gerrant beside Betty and Lowe
// beside Aubury, and a runner whose Magic shuts the guild's door plans the same without the guild.
func TestRuneRunnerRoundCarriesTheAmmoStations(t *testing.T) {
	r := ammoRig(t, "ammorig1", 71, nil)
	w1, w2, w3 := 71, 72, 73
	want := fmt.Sprintf("[deliver@%d Hickton@%d draw@%d hop%d Gerrant@%d Betty@%d hop%d Betty@%d Gerrant@%d hop%d Gerrant@%d Betty@%d "+
		"hop%d Aubury@%d Zaff@%d Lowe@%d hop%d Aubury@%d Zaff@%d Lowe@%d hop%d Aubury@%d Zaff@%d Lowe@%d "+
		"hop%d Lundail@%d hop%d Lundail@%d hop%d Lundail@%d hop%d WizardsGuild@%d hop%d WizardsGuild@%d hop%d WizardsGuild@%d Hickton@%d hop%d]",
		w1, w1, w1, w3, w3, w3, w1, w1, w1, w2, w2, w2,
		w2, w2, w2, w2, w1, w1, w1, w1, w3, w3, w3, w3,
		w3, w3, w1, w1, w2, w2, w2, w2, w1, w1, w3, w3, w3, w1)
	if got := runerunnerplan.PlanString(r.b.st.Ring, 0); got != want {
		t.Fatalf("ring\n got %s\nwant %s", got, want)
	}
	// Magic 50: no guild; Lundail is last on world 73 and Hickton follows it.
	r.setMagic(50)
	r.b.newRound(r.ctx, runerunnerplan.AreaWest, false)
	if got := runerunnerplan.PlanString(r.b.st.Ring, 0); strings.Contains(got, "WizardsGuild") || !strings.HasSuffix(got, "Lundail@72 Hickton@72 hop71]") {
		t.Errorf("Magic 50: %s", got)
	}
	// ammoBuy off, or the classic ring, plans no ammo station.
	off := newRotRig(t, "ammorig1b", 74, 2809, 3441, map[string]interface{}{"worlds": []interface{}{74.0, 75.0, 76.0}, "ammoBuy": false})
	off.b.newRound(off.ctx, runerunnerplan.AreaWest, false)
	if strings.Contains(runerunnerplan.PlanString(off.b.st.Ring, 0), "Hickton") {
		t.Errorf("ammoBuy off still plans ammo: %s", runerunnerplan.PlanString(off.b.st.Ring, 0))
	}
	classic := newRotRig(t, "ammorig1c", 77, 2809, 3441, map[string]interface{}{"rotate": "off", "shops": []interface{}{"Aubury", "Betty"}, "worlds": []interface{}{77.0, 78.0}, "ammoBuy": true})
	classic.b.newRound(classic.ctx, runerunnerplan.AreaWest, false)
	if strings.Contains(runerunnerplan.PlanString(classic.b.st.Ring, 0), "Hickton") || classic.b.set.AmmoOn() {
		t.Errorf("the classic ring planned ammo: %s", runerunnerplan.PlanString(classic.b.st.Ring, 0))
	}
	if !strings.Contains(classic.log(), "the classic ring has no ammo stations") {
		t.Errorf("no start line saying so\n%s", classic.log())
	}
	if !strings.Contains(r.log(), "ammo: on sites=Gerrant,Lowe,Hickton items=Bolts,") || !strings.Contains(r.log(), "Arrow shaft is sold by no shop on this circuit") {
		t.Errorf("the start line\n%s", r.log())
	}
}

// A first visit to Hickton with the mule's bank unread buys the consumed parts only (Bolts, Steel arrowtips); with
// the live mule published (39,330 Mithril arrowtips idle) it buys Bolts, Steel, Adamant and Rune and leaves the
// Mithril, exactly the design's 60,718 for the four lines on a full shelf.
func TestAmmoStationBuysHicktonAndRespectsTheMuleGate(t *testing.T) {
	r := ammoRig(t, "ammostation1", 80, nil)
	r.atKey(t, "Hickton@80")
	r.pack(r.rich(), 20, nil)
	visits := 0
	r.shelf(hicktonShelf(), r.rich(), &visits, 1)
	got := r.runAmmo()
	if got.Outcome != runerunnerplan.OutcomeBought || got.Spent != 1098+13840 || got.Bought["Bolts"] != 200 || got.Bought["Steel arrowtips"] != 600 || len(got.Bought) != 2 || visits != 1 {
		t.Fatalf("unread mule bank: %+v visits %d\n%s", got, visits, r.log())
	}
	for _, want := range []string{
		"buy shop=Hickton world=80 item=Bolts seen=200 bought=200 spent=1098 left=0 cap=1000",
		"buy shop=Hickton world=80 item=Steel arrowtips seen=600 bought=600 spent=13840 left=0 cap=1000",
		"ammo-skip item=Mithril arrowtips reason=\"the mule's bank was not read lately",
	} {
		if !strings.Contains(r.log(), want) {
			t.Errorf("log lacks %q\n%s", want, r.log())
		}
	}
	if r.b.st.Cursor != 2 || r.b.st.Bought["Bolts"] != 200 || r.b.st.Spent != 14938 {
		t.Errorf("state: cursor %d bought %v spent %d", r.b.st.Cursor, r.b.st.Bought, r.b.st.Spent)
	}
	// The board remembers what the visit left: the next estimate starts from an emptied shelf.
	site, _ := runerunnerplan.AmmoSiteFor("Hickton")
	if left := fleetrunner.ShopsFor(80).Estimate(site.Shop, "Steel arrowtips", time.Now(), r.b.set.Tick()); left > 5 {
		t.Errorf("the board thinks %d Steel arrowtips are left", left)
	}

	// The live mule: Bolts 40,306, Mithril arrowtips 39,330 (gated at 20,000), Feather 9,079.
	r2 := ammoRig(t, "ammostation2", 83, nil)
	publishRuneMule(r2, map[string]int{"Bolts": 40306, "Mithril arrowtips": 39330, "Feather": 9079, "Coins": 4672242})
	r2.atKey(t, "Hickton@83")
	r2.pack(r2.rich(), 20, nil)
	r2.shelf(hicktonShelf(), r2.rich(), nil, 1)
	got = r2.runAmmo()
	if got.Outcome != runerunnerplan.OutcomeBought || got.Spent != 60718 || len(got.Bought) != 4 || got.Bought["Mithril arrowtips"] != 0 ||
		got.Bought["Adamant arrowtips"] != 200 || got.Bought["Rune arrowtips"] != 100 {
		t.Fatalf("live mule: %+v\n%s", got, r2.log())
	}
	if !strings.Contains(r2.log(), "ammo-skip item=Mithril arrowtips reason=\"the mule holds 39330 (ammoStopAbove 20000)\"") {
		t.Errorf("no gate line\n%s", r2.log())
	}
	// Everything the mule holds enough of: nothing is wanted and the shop is not even walked to.
	r3 := ammoRig(t, "ammostation3", 86, nil)
	publishRuneMule(r3, map[string]int{"Bolts": 2000000, "Steel arrowtips": 200000, "Mithril arrowtips": 39330, "Adamant arrowtips": 21000, "Rune arrowtips": 20000})
	r3.atKey(t, "Hickton@86")
	r3.pack(r3.rich(), 20, nil)
	visits = 0
	r3.shelf(hicktonShelf(), r3.rich(), &visits, 1)
	if got = r3.runAmmo(); got.Outcome != runerunnerplan.OutcomeSkipped || !strings.Contains(got.Reason, "nothing wanted at Hickton") || visits != 0 {
		t.Errorf("a stocked mule: %+v visits %d", got, visits)
	}
}

// The round's budget: a visit buys within what ammoCoins has left, in priority order; a spent budget skips the
// station without a walk.
func TestAmmoStationStaysInTheRoundBudget(t *testing.T) {
	r := ammoRig(t, "ammostation4", 89, map[string]interface{}{"ammoCoins": 20000.0})
	publishRuneMule(r, map[string]int{"Bolts": 1})
	r.atKey(t, "Hickton@89")
	r.pack(r.rich(), 20, nil)
	r.shelf(hicktonShelf(), r.rich(), nil, 1)
	got := r.runAmmo()
	if got.Spent <= 14938 || got.Spent > 20000 || got.Bought["Bolts"] != 200 || got.Bought["Steel arrowtips"] != 600 {
		t.Fatalf("a 20,000 budget: %+v\n%s", got, r.log())
	}
	if !strings.Contains(r.log(), "ammoCoins") {
		t.Errorf("no budget reason in the log\n%s", r.log())
	}
	// Now the budget is spent: the next station skips on the budget before it walks.
	r.b.st.Done["Hickton@89"] = runerunnerplan.StationResult{Spent: 20000}
	visits := 0
	r.shelf(map[string]int{"Feather": 1000}, r.rich(), &visits, 1)
	r.atKey(t, "Gerrant@91")
	r.ctx.CurrentWorld = 91
	if got = r.runAmmo(); got.Outcome != runerunnerplan.OutcomeSkipped || !strings.Contains(got.Reason, "ammoCoins 20000 is spent this round") || visits != 0 {
		t.Errorf("spent budget: %+v visits %d", got, visits)
	}
	// ammoCoins 0 is no budget at all.
	u := ammoRig(t, "ammostation4b", 92, map[string]interface{}{"ammoCoins": 0.0})
	publishRuneMule(u, map[string]int{"Bolts": 1})
	u.atKey(t, "Hickton@92")
	u.pack(u.rich(), 20, nil)
	u.shelf(hicktonShelf(), u.rich(), nil, 1)
	if got = u.runAmmo(); got.Spent != 60718+18976 || got.Bought["Mithril arrowtips"] != 400 {
		t.Errorf("unlimited budget: %+v", got)
	}
}

// Ammo is the lowest priority: it spends only what the pack holds beyond the fare and the WHOLE pack-paid need of
// the rune and staff stations still ahead (not half of it, as a rune visit's reserve is cut), and a pack with
// nothing to spare buys none. Reviewer, 13 October: with the cap, 5,000 coins against a round of millions still spent
// half of what the runes were short of.
func TestAmmoStationIsLowestPriority(t *testing.T) {
	r := ammoRig(t, "ammostation5", 95, nil)
	r.atKey(t, "Hickton@95")
	r.pack(300, 20, nil) // the fare only
	visits := 0
	r.shelf(hicktonShelf(), 300, &visits, 1)
	got := r.runAmmo()
	if got.Outcome != runerunnerplan.OutcomeSkipped || !strings.Contains(got.Reason, "nothing to spend on ammo") || visits != 0 || got.NoCoins {
		t.Errorf("a pack of just the fare: %+v visits %d", got, visits)
	}
	// 5,000 coins against a round of hundreds of thousands: every coin is the runes' (it used to spend half).
	r2 := ammoRig(t, "ammostation6", 98, nil)
	r2.atKey(t, "Hickton@98")
	if hold := r2.b.ammoHold(r2.ctx); hold < 300000 {
		t.Fatalf("the rig's round holds only %d for the runes ahead", hold)
	}
	r2.pack(5000, 20, nil)
	visits = 0
	r2.shelf(hicktonShelf(), 5000, &visits, 1)
	got = r2.runAmmo()
	if got.Outcome != runerunnerplan.OutcomeSkipped || got.Spent != 0 || visits != 0 || !strings.Contains(got.Reason, "nothing to spend on ammo") {
		t.Errorf("5,000 coins: %+v visits %d", got, visits)
	}
	// The same at a station in the middle of the round (Gerrant beside Betty), with a purse of 40% of the rune need:
	// not a coin of it goes on feathers, however small the shelf's cost.
	r2.atKey(t, "Gerrant@100")
	r2.ctx.CurrentWorld = 100
	mid := r2.b.ammoHold(r2.ctx)
	r2.pack(mid*4/10, 20, nil)
	r2.shelf(map[string]int{"Feather": 1000}, mid*4/10, &visits, 1)
	if got = r2.runAmmo(); got.Outcome != runerunnerplan.OutcomeSkipped || got.Spent != 0 || visits != 0 {
		t.Errorf("a purse at 40%% of the rune need (%d): %+v visits %d", mid, got, visits)
	}
	// A purse that is exactly the rune need and the fare is still not a coin of ammo.
	r2.atKey(t, "Gerrant@100")
	r2.b.st.Done = map[string]runerunnerplan.StationResult{}
	r2.pack(mid+300, 20, nil)
	r2.shelf(map[string]int{"Feather": 1000}, mid+300, &visits, 1)
	if got = r2.runAmmo(); got.Outcome != runerunnerplan.OutcomeSkipped || got.Spent != 0 || visits != 0 {
		t.Errorf("exactly the rune need: %+v visits %d", got, visits)
	}
	// With 20,000 coins beyond the rune need and the fare, ammo spends only that much.
	r4 := ammoRig(t, "ammostation6b", 200, nil)
	r4.atKey(t, "Hickton@200")
	hold := r4.b.ammoHold(r4.ctx)
	r4.pack(hold+300+20000, 20, nil)
	r4.shelf(hicktonShelf(), hold+300+20000, nil, 1)
	got = r4.runAmmo()
	if got.Outcome != runerunnerplan.OutcomeBought || got.Spent < 14000 || got.Spent > 20000 {
		t.Errorf("20,000 coins beyond the rune need: %+v\n%s", got, r4.log())
	}
	// The last station of the round has no rune stations ahead: nothing is held back.
	r3 := ammoRig(t, "ammostation7", 101, nil)
	r3.atKey(t, "Hickton@103")
	r3.ctx.CurrentWorld = 103
	r3.pack(15500, 20, nil)
	r3.shelf(hicktonShelf(), 15500, nil, 1)
	got = r3.runAmmo()
	if got.Spent != 14938 {
		t.Errorf("the round's last stop with 15,500 coins: %+v", got)
	}
}

// A round that has already left runes on a shelf for coins, or passed a station for coins, buys no more ammo, however
// full the pack: that covers the stop that closes the round, which has no rune station ahead to hold coins for.
func TestAmmoStationWaitsWhenTheRoundIsShort(t *testing.T) {
	r := ammoRig(t, "ammostation6c", 203, nil)
	r.ctx.CurrentWorld = 205
	r.pack(5000000, 20, nil)
	visits := 0
	r.shelf(hicktonShelf(), 5000000, &visits, 1)
	r.atKey(t, "Hickton@205")
	r.b.st.Done["Aubury@204"] = runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeBought, ShortRunes: 90, ShortCoins: 30000}
	got := r.runAmmo()
	if got.Outcome != runerunnerplan.OutcomeSkipped || visits != 0 || !strings.Contains(got.Reason, "this round is short of coins") || !strings.Contains(got.Reason, "the runes come first") {
		t.Errorf("a round that left runes for coins: %+v visits %d", got, visits)
	}
	// A station passed for coins counts the same.
	r.atKey(t, "Hickton@205")
	r.b.st.Done = map[string]runerunnerplan.StationResult{"Lundail@204": {Outcome: runerunnerplan.OutcomeSkipped, NoCoins: true}}
	if got = r.runAmmo(); got.Outcome != runerunnerplan.OutcomeSkipped || visits != 0 || !strings.Contains(got.Reason, "this round is short of coins") {
		t.Errorf("a station passed for coins: %+v visits %d", got, visits)
	}
	// And a round that bought everything it wanted buys ammo with its spare coins.
	r.atKey(t, "Hickton@205")
	r.b.st.Done = map[string]runerunnerplan.StationResult{"Aubury@204": {Outcome: runerunnerplan.OutcomeBought, Spent: 400000}}
	if got = r.runAmmo(); got.Outcome != runerunnerplan.OutcomeBought || got.Spent == 0 || visits != 1 {
		t.Errorf("a funded round: %+v visits %d", got, visits)
	}
}

// THE COIN DRAW. The ammo estimate reaches the pack through the draw only from coins left once the whole rune need
// is covered, so a purse short of the runes draws exactly what it drew before ammo existed.
func TestBankDrawAddsAmmoOnlyFromSurplus(t *testing.T) {
	r := ammoRig(t, "ammodraw1", 206, nil)
	set := r.b.set
	rn := runerunnerplan.RoundNeed{Pack: 600000, PackMargin: 120000, Staffs: 9090, Ammo: 100000, Fares: set.FareCoins}
	plain := rn
	plain.Ammo = 0
	need := rn.PackNeed() // 729,090: the ammo is not in it
	if need != plain.PackNeed() || rn.Est() != plain.Est() || rn.Total() != plain.Total() {
		t.Fatalf("ammo is in the need: %d %d %d", need, rn.Est(), rn.Total())
	}
	draw := func(rn runerunnerplan.RoundNeed, first bool, bank, inPack int) runerunnerplan.Draw {
		d, _ := r.b.bankDraw(rn, first, bank, inPack, need)
		return d
	}
	// A bank that covers the rune need and the ammo: the draw carries both.
	flush, base := draw(rn, true, 3000000, 0), draw(plain, true, 3000000, 0)
	if flush.Amount != base.Amount+100000 || !strings.Contains(flush.Why, "for ammo") || strings.Contains(base.Why, "ammo") {
		t.Errorf("a flush bank: %+v against %+v", flush, base)
	}
	// A bank with only part of the ammo beyond the rune need.
	part, none := draw(rn, true, set.CoinReserve+need+set.FareCoins+40000, 0), draw(plain, true, set.CoinReserve+need+set.FareCoins+40000, 0)
	if part.Amount != none.Amount+40000 {
		t.Errorf("40,000 beyond the need: %+v against %+v", part, none)
	}
	// A bank short of the runes: exactly the draw there is without ammo.
	for _, bank := range []int{set.CoinReserve + need - 1, set.CoinReserve + need/2, set.CoinReserve + 5000, set.CoinReserve} {
		if got, want := draw(rn, true, bank, 0), draw(plain, true, bank, 0); got != want {
			t.Errorf("bank %d short of the need: %+v, want %+v", bank, got, want)
		}
	}
	// A top-up at a shop's bank follows the same rule, and the pack's own coins count toward the surplus: a pack and
	// a bank under the need draw as they did, a pack at half the need and a flush bank draw the ammo on top.
	if got, want := draw(rn, false, set.CoinReserve+200000, 300+500000), draw(plain, false, set.CoinReserve+200000, 300+500000); got != want {
		t.Errorf("top-up with the pack and bank under the need: %+v, want %+v", got, want)
	}
	if got, want := draw(rn, false, 3000000, 300+need/2), draw(plain, false, 3000000, 300+need/2); got.Amount != want.Amount+100000 {
		t.Errorf("top-up with a flush bank: %+v, want the ammo on top of %+v", got, want)
	}
	// tripCoins still bounds the draw: a need at the trip limit leaves no room for ammo.
	big := runerunnerplan.RoundNeed{Pack: set.TripCoins, Ammo: 100000, Fares: set.FareCoins}
	bigPlain := big
	bigPlain.Ammo = 0
	d1, _ := r.b.bankDraw(big, true, 9000000, 0, big.PackNeed())
	d2, _ := r.b.bankDraw(bigPlain, true, 9000000, 0, bigPlain.PackNeed())
	if d1.Amount != d2.Amount || d1.Amount != set.TripCoins+set.FareCoins {
		t.Errorf("tripCoins: %d against %d, want %d", d1.Amount, d2.Amount, set.TripCoins+set.FareCoins)
	}
	// A round that has already left runes on a shelf for coins draws nothing for ammo, flush bank or not.
	r.b.st.Done["Betty@207"] = runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeBought, ShortRunes: 12, ShortCoins: 9000}
	if got := draw(rn, false, 3000000, 0); got != draw(plain, false, 3000000, 0) {
		t.Errorf("a short round draws for ammo: %+v", got)
	}
}

// The mule ask, the market station's float and the shop-bank top-up read the rune need alone: switching ammo on
// does not move any of them (reviewer, 13 October: ammo in Est reserved its estimate ahead of the market's runes).
func TestAmmoIsOutsideEveryPurseSizedFromTheNeed(t *testing.T) {
	r := ammoRig(t, "ammopurse3", 209, map[string]interface{}{"marketCoins": 1000000.0})
	from := runerunnerplan.Index(r.b.st.Ring, "draw@209") + 1
	on := r.b.roundNeed(r.ctx, from)
	if on.Ammo <= 0 {
		t.Fatalf("the rig's round carries no ammo: %s", on)
	}
	r.b.st.BankCoins = 1200000
	marketOn := r.b.marketBudget(r.ctx, 300300)
	r.b.set.AmmoBuy = false
	off := r.b.roundNeed(r.ctx, from)
	marketOff := r.b.marketBudget(r.ctx, 300300)
	if on.PackNeed() != off.PackNeed() || on.Est() != off.Est() || on.Total() != off.Total() || marketOn != marketOff {
		t.Errorf("ammo moved the need: %d/%d/%d market %d, without %d/%d/%d market %d", on.PackNeed(), on.Est(), on.Total(), marketOn,
			off.PackNeed(), off.Est(), off.Total(), marketOff)
	}
	ob, oa := runerunnerplan.CircuitTopUp(true, 150000, 500000, on.Est(), 10000, 300, 400000)
	fb, fa := runerunnerplan.CircuitTopUp(true, 150000, 500000, off.Est(), 10000, 300, 400000)
	if ob != fb || oa != fa {
		t.Errorf("the mule ask moved with ammo: %d/%d against %d/%d", ob, oa, fb, fa)
	}
}

// A new stack needs a free pack slot beyond the reserve for the runes; a part already stacked in the pack needs
// none.
func TestAmmoStationNeedsASlotForANewStack(t *testing.T) {
	r := ammoRig(t, "ammostation8", 104, nil)
	r.atKey(t, "Hickton@104")
	r.pack(r.rich(), 1, nil) // one free slot, one kept for a rune stack
	visits := 0
	r.shelf(hicktonShelf(), r.rich(), &visits, 1)
	if got := r.runAmmo(); got.Outcome != runerunnerplan.OutcomeSkipped || !strings.Contains(got.Reason, "no room for an ammo stack") || visits != 0 {
		t.Errorf("no free slot: %+v visits %d", got, visits)
	}
	r2 := ammoRig(t, "ammostation9", 107, nil)
	r2.atKey(t, "Hickton@107")
	r2.pack(r2.rich(), 1, map[string]int{"Steel arrowtips": 330}) // the steel is stacked already
	r2.shelf(hicktonShelf(), r2.rich(), nil, 1)
	got := r2.runAmmo()
	if got.Outcome != runerunnerplan.OutcomeBought || got.Bought["Steel arrowtips"] != 600 || len(got.Bought) != 1 {
		t.Errorf("a stacked part: %+v\n%s", got, r2.log())
	}
	// ammoSlotReserve 0 spends the last slot.
	r3 := ammoRig(t, "ammostation10", 110, map[string]interface{}{"ammoSlotReserve": 0.0})
	r3.atKey(t, "Hickton@110")
	r3.pack(r3.rich(), 1, nil)
	r3.shelf(hicktonShelf(), r3.rich(), nil, 1)
	if got = r3.runAmmo(); len(got.Bought) != 1 || got.Bought["Bolts"] != 200 {
		t.Errorf("one slot, no reserve: %+v", got)
	}
}

// The station skips by rule and not for coins, and never walks when it cannot buy.
func TestAmmoStationSkips(t *testing.T) {
	r := ammoRig(t, "ammostation11", 113, nil)
	visits := 0
	r.pack(r.rich(), 20, nil)
	r.shelf(hicktonShelf(), r.rich(), &visits, 1)
	// The wrong world.
	r.atKey(t, "Hickton@113")
	r.ctx.CurrentWorld = 114
	if got := r.runAmmo(); got.Outcome != runerunnerplan.OutcomeSkipped || !strings.Contains(got.Reason, "on world 114; the station is for world 113") {
		t.Errorf("wrong world: %+v", got)
	}
	r.ctx.CurrentWorld = 113
	// The draw found nothing in the bank.
	r.atKey(t, "Hickton@113")
	r.b.st.Done = map[string]runerunnerplan.StationResult{}
	r.b.noCoins = true
	if got := r.runAmmo(); got.Outcome != runerunnerplan.OutcomeSkipped || got.NoCoins || !strings.Contains(got.Reason, "no coins") {
		t.Errorf("no coins: %+v", got)
	}
	r.b.noCoins = false
	// A pair struck off the ring.
	r.atKey(t, "Hickton@113")
	r.b.st.PairOffUntil["Hickton@113"] = time.Now().Add(time.Hour)
	if got := r.runAmmo(); got.Outcome != runerunnerplan.OutcomeSkipped || !strings.Contains(got.Reason, "off until") {
		t.Errorf("pair off: %+v", got)
	}
	delete(r.b.st.PairOffUntil, "Hickton@113")
	// A shelf worth less than ammoMinVisitCoins: Lowe's bolts sighted nearly emptied (100 left, 18 coins each).
	lowe, _ := runerunnerplan.AmmoSiteFor("Lowe")
	fleetrunner.ShopsFor(113).Observe(lowe.Shop, "Bolts", 100, time.Now(), "other")
	r.atKey(t, "Lowe@113")
	r.b.ammoPackSeam = func() (int, int, map[string]int) { return r.rich(), 20, map[string]int{} }
	visits = 0
	if got := r.runAmmo(); got.Outcome != runerunnerplan.OutcomeSkipped || !strings.Contains(got.Reason, "under ammoMinVisitCoins 3000") || visits != 0 {
		t.Errorf("a nearly empty shelf: %+v visits %d\n%s", got, visits, r.log())
	}
	// Ammo switched off with a ring that still holds the stations (a restart on new settings).
	r.b.set.AmmoBuy = false
	r.atKey(t, "Hickton@113")
	if got := r.runAmmo(); got.Outcome != runerunnerplan.OutcomeSkipped || !strings.Contains(got.Reason, "ammo is off") {
		t.Errorf("ammo off: %+v", got)
	}
	// A saved ring whose shop the operator has since taken out of ammoShops.
	r.b.set.AmmoBuy = true
	r.b.set.AmmoSiteList = []string{"Gerrant", "Lowe"}
	r.b.st.Done = map[string]runerunnerplan.StationResult{}
	r.atKey(t, "Hickton@113")
	visits = 0
	r.shelf(hicktonShelf(), r.rich(), &visits, 1)
	if got := r.runAmmo(); got.Outcome != runerunnerplan.OutcomeSkipped || !strings.Contains(got.Reason, "Hickton is no longer in ammoShops") || visits != 0 {
		t.Errorf("a shop out of ammoShops: %+v visits %d", got, visits)
	}
}

// A keeper that will not be reached or opened strikes the pair; three in a row take it off the ring for an hour,
// and a visit that opens clears the strikes.
func TestAmmoStationStrikesAndPriceCheck(t *testing.T) {
	r := ammoRig(t, "ammostation12", 116, nil)
	r.pack(r.rich(), 20, nil)
	r.b.ammoBuySeam = func(runerunnerplan.AmmoSite, func(map[string]int, int) fleetplan.PurchasePlan) runerunner.AmmoVisit {
		return runerunner.AmmoVisit{ShopVisit: fleetrunner.ShopVisit{Reason: "walk-failed"}}
	}
	for i := 1; i <= 3; i++ {
		r.atKey(t, "Hickton@116")
		got := r.runAmmo()
		if got.Outcome != runerunnerplan.OutcomeWalkFailed || !strings.Contains(got.Reason, "could not reach Hickton") {
			t.Fatalf("walk %d: %+v", i, got)
		}
		r.b.st.Done = map[string]runerunnerplan.StationResult{}
	}
	if off, _ := r.b.st.PairOff("Hickton@116", time.Now()); !off {
		t.Error("three failed walks did not take the pair off the ring")
	}
	// The shop that will not open: Aborted, a strike, and the keeper-out-of-reach line is in the log.
	r2 := ammoRig(t, "ammostation13", 119, nil)
	r2.pack(r2.rich(), 20, nil)
	r2.b.ammoBuySeam = func(runerunnerplan.AmmoSite, func(map[string]int, int) fleetplan.PurchasePlan) runerunner.AmmoVisit {
		return runerunner.AmmoVisit{Walked: true, OpenFailed: true,
			ShopVisit: fleetrunner.ShopVisit{Reason: "opening Hickton's shop: entityquery: the keeper is out of reach and the account is not getting closer"}}
	}
	r2.atKey(t, "Hickton@119")
	got := r2.runAmmo()
	if got.Outcome != runerunnerplan.OutcomeAborted || r2.b.st.PairStrikes["Hickton@119"] != 1 || !strings.Contains(r2.log(), "the keeper is out of reach") {
		t.Errorf("open failed: %+v strikes %v\n%s", got, r2.b.st.PairStrikes, r2.log())
	}
	// A visit that opens clears them.
	r2.shelf(hicktonShelf(), r2.rich(), nil, 1)
	r2.b.st.Done = map[string]runerunnerplan.StationResult{}
	r2.atKey(t, "Hickton@119")
	r2.runAmmo()
	if r2.b.st.PairStrikes["Hickton@119"] != 0 {
		t.Errorf("strikes after a good visit: %v", r2.b.st.PairStrikes)
	}

	// The price check: a shop that charges double what the model says is a mismatch, and two disable the pair.
	r3 := ammoRig(t, "ammostation14", 122, nil)
	r3.pack(r3.rich(), 20, nil)
	r3.shelf(hicktonShelf(), r3.rich(), nil, 2)
	hickton, _ := runerunnerplan.AmmoSiteFor("Hickton")
	for i := 1; i <= 2; i++ {
		r3.b.st.Done = map[string]runerunnerplan.StationResult{}
		// The shelf the first visit emptied has regrown by the next round.
		for _, l := range hickton.Shop.Lines {
			fleetrunner.ShopsFor(122).Observe(hickton.Shop, l.Rune, l.Base, time.Now().Add(time.Duration(i)*time.Second), "other")
		}
		r3.atKey(t, "Hickton@122")
		r3.runAmmo()
	}
	if !strings.Contains(r3.log(), "PRICE MODEL MISMATCH shop=Hickton world=122") || !strings.Contains(r3.log(), "disabled=true") {
		t.Errorf("no mismatch line\n%s", r3.log())
	}
	if off, why := r3.b.st.PairOff("Hickton@122", time.Now()); !off || !strings.Contains(why, "price-model mismatches") {
		t.Errorf("pair off %v %q", off, why)
	}
	r3.atKey(t, "Hickton@122")
	if got = r3.runAmmo(); got.Outcome != runerunnerplan.OutcomeSkipped {
		t.Errorf("a disabled pair: %+v", got)
	}
}

// What a short visit leaves on the shelf is logged with its reason, as the rune visits do.
func TestAmmoStationLogsWhatItLeft(t *testing.T) {
	r := ammoRig(t, "ammostation15", 125, nil)
	publishRuneMule(r, map[string]int{"Bolts": 1})
	r.atKey(t, "Hickton@125")
	r.pack(r.rich(), 20, nil)
	// The Rune arrowtips shelf only has 40 left and Adamant is empty.
	shelf := hicktonShelf()
	shelf["Adamant arrowtips"] = 0
	shelf["Rune arrowtips"] = 40
	r.shelf(shelf, r.rich(), nil, 1)
	r.runAmmo()
	for _, want := range []string{"empty shop=Hickton world=125 item=Adamant arrowtips", "buy shop=Hickton world=125 item=Rune arrowtips seen=40 bought=40"} {
		if !strings.Contains(r.log(), want) {
			t.Errorf("log lacks %q\n%s", want, r.log())
		}
	}
	// A cap of 10 on the Steel arrowtips leaves most of the shelf, and says so.
	r2 := ammoRig(t, "ammostation16", 128, map[string]interface{}{"ammoPriceCaps": []interface{}{"Steel arrowtips=10"}})
	publishRuneMule(r2, map[string]int{"Bolts": 1})
	r2.atKey(t, "Hickton@128")
	r2.pack(r2.rich(), 20, nil)
	r2.shelf(hicktonShelf(), r2.rich(), nil, 1)
	r2.runAmmo()
	n, _ := runerunnerplan.BuyableUnderCap(runerunnerplan.Hickton, "Steel arrowtips", 600, 10)
	if want := fmt.Sprintf("left %d Steel arrowtips shop=Hickton world=128: price 11 over cap 10", 600-n); n != 84 || !strings.Contains(r2.log(), want) {
		t.Errorf("no left line for the capped steel\n%s", r2.log())
	}
}

// THE PURSE. The ammo stops still ahead are valued beside the round's need (RoundNeed.Ammo, not in PackNeed, Est or
// Total), each off its shelf when the round reaches it, held to the budget the round has left, and counted once:
// Hickton's start, before the draw, is not in the figure the draw reads.
func TestRoundNeedCountsTheAmmoStops(t *testing.T) {
	r := ammoRig(t, "ammopurse1", 131, nil)
	from := runerunnerplan.Index(r.b.st.Ring, "draw@131") + 1
	// The mule's bank unread: the consumed parts only. Gerrant 9,250 x 3, Lowe 22,995 x 3, Hickton at the
	// far end 1,098 + 13,840: 111,673.
	n := r.b.roundNeed(r.ctx, from)
	if n.Ammo != 3*9250+3*22995+1098+13840 {
		t.Fatalf("ammo %d in %s", n.Ammo, n)
	}
	if !strings.Contains(n.String(), "ammo=111673") || n.PackNeed() != n.Pack+n.PackMargin+n.Staffs {
		t.Errorf("log form %q", n.String())
	}
	// A mule that has none of anything wants the lot: 60,718 at Hickton makes 157,453, and the 150,000 budget
	// holds it.
	publishRuneMule(r, map[string]int{"Coins": 5000000})
	if n = r.b.roundNeed(r.ctx, from); n.Ammo != 150000 {
		t.Errorf("a mule holding none: ammo %d, want the 150000 budget", n.Ammo)
	}
	// What the round has spent comes off the budget; a spent budget counts none.
	r.b.st.Done["Hickton@131"] = runerunnerplan.StationResult{Spent: 100000}
	if n = r.b.roundNeed(r.ctx, from); n.Ammo != 50000 {
		t.Errorf("100,000 spent: ammo %d", n.Ammo)
	}
	r.b.st.Done["Hickton@131"] = runerunnerplan.StationResult{Spent: 150000}
	if n = r.b.roundNeed(r.ctx, from); n.Ammo != 0 {
		t.Errorf("budget spent: ammo %d", n.Ammo)
	}
	r.b.st.Done = map[string]runerunnerplan.StationResult{}
	// A pair off the ring is not counted.
	r.b.st.PairOffUntil["Hickton@133"] = time.Now().Add(time.Hour)
	r.b.st.PairOffUntil["Lowe@132"] = time.Now().Add(time.Hour)
	withOff := r.b.roundNeed(r.ctx, from)
	if withOff.Ammo >= 150000 || withOff.Ammo <= 0 {
		t.Errorf("two stops off: ammo %d", withOff.Ammo)
	}
	// Ammo off counts none, whatever the ring holds.
	r.b.set.AmmoBuy = false
	if n = r.b.roundNeed(r.ctx, from); n.Ammo != 0 {
		t.Errorf("ammo off: %d", n.Ammo)
	}
	// The rune figures are what they were without ammo.
	plain := newRotRig(t, "ammopurse2", 134, 2809, 3441, map[string]interface{}{"worlds": []interface{}{134.0, 135.0, 136.0}, "rotationPhase": 2.0, "zaffStaffs": 2.0, "marketBuy": false})
	plain.setMagic(66)
	plain.b.newRound(plain.ctx, runerunnerplan.AreaWest, false)
	pn := plain.b.roundNeed(plain.ctx, runerunnerplan.Index(plain.b.st.Ring, "draw@134")+1)
	r.b.set.AmmoBuy = true
	an := r.b.roundNeed(r.ctx, from)
	if pn.Pack != an.Pack || pn.Lundail != an.Lundail || pn.Staffs != an.Staffs || pn.Margin() != an.Margin() {
		t.Errorf("rune figures moved:\n%s\n%s", pn, an)
	}
	if pn.PackNeed() != an.PackNeed() || pn.Est() != an.Est() || pn.Total() != an.Total() {
		t.Errorf("ammo is in the need:\n%s\n%s", pn, an)
	}
}

// Ammo never makes a round productive and a failed ammo stop never stretches the idle wait.
func TestRoundProductivityLeavesAmmoOut(t *testing.T) {
	r := ammoRig(t, "ammoround1", 137, nil)
	r.b.st.Done = map[string]runerunnerplan.StationResult{
		"Hickton@137": {Outcome: runerunnerplan.OutcomeBought, Spent: 14938},
		"Gerrant@139": {Outcome: runerunnerplan.OutcomeWalkFailed},
		"Lowe@138":    {Outcome: runerunnerplan.OutcomeAborted},
		"Betty@139":   {Outcome: runerunnerplan.OutcomeSkipped},
	}
	if productive, failures := r.b.roundProductivity(); productive || failures != 0 {
		t.Errorf("ammo alone: productive %v failures %d", productive, failures)
	}
	r.b.st.Done["Betty@139"] = runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeBought}
	r.b.st.Done["Aubury@138"] = runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeWalkFailed}
	if productive, failures := r.b.roundProductivity(); !productive || failures != 1 {
		t.Errorf("a rune buy and a failed rune walk: productive %v failures %d", productive, failures)
	}
	r.b.ammoRoundLine(r.ctx)
	if !strings.Contains(r.log(), "ammo round=") || !strings.Contains(r.log(), "spent=14938") || !strings.Contains(r.log(), "budget=150000") {
		t.Errorf("no round line\n%s", r.log())
	}
}

// The delivery's size reads the runes: ammo is counted apart.
func TestSplitHeldAndTheDeliveryGate(t *testing.T) {
	runes, ammo := splitHeld(map[string]int{"Mind rune": 100, "Staff of air": 4, "Bolts": 2380, "Feather": 3000, "Steel arrowtips": 330})
	if runes != 104 || ammo != 5710 {
		t.Errorf("runes %d ammo %d", runes, ammo)
	}
	r := ammoRig(t, "ammodeliver1", 140, nil)
	// 3,000 Feathers alone, delivered 20 minutes ago, are not worth a trade.
	r.b.st.LastDelivery = time.Now().Add(-20 * time.Minute)
	here := fleetrunner.Here(r.b.deps(r.ctx))
	if v := r.b.deliverVerdictAmmo(r.ctx, 0, 3000, here); v.Action != runerunnerplan.DeliverSkip {
		t.Errorf("ammo alone: %+v", v)
	}
	// The runes-only verdict is the same function with no ammo.
	if v := r.b.deliverVerdict(r.ctx, 5, here); v.Action != runerunnerplan.DeliverSkip {
		t.Errorf("5 runes: %+v", v)
	}
	// With ammo on the delivery lists the ammo after the runes and the staffs.
	items := r.b.set.DeliverItems()
	if items[len(items)-7] != "Bolts" || items[len(items)-1] != "Arrow shaft" {
		t.Errorf("delivery items %v", items)
	}
}

// The panel's strings load, an out-of-range number is refused rather than clamped, and a bad ammo shop names its
// setting.
func TestRuneRunnerAmmoSettingsDecode(t *testing.T) {
	s, err := decodeRuneRunnerSettings(map[string]interface{}{
		"ammoBuy": "true", "ammoShops": "Hickton,Lowe", "ammoItems": []interface{}{"Bolts", "Steel arrowtips"},
		"ammoPriceCaps": "Bolts=12", "ammoStopAbove": "Bolts=500000,Steel arrowtips=0", "ammoCoins": "90000", "ammoBuyAll": "false",
		"ammoMinVisitCoins": 5000.0, "ammoSlotReserve": "2", "trawlerRope": "false",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !s.AmmoBuy || !s.AmmoOn() || strings.Join(s.AmmoSiteList, ",") != "Hickton,Lowe" || strings.Join(s.AmmoItemList, ",") != "Bolts,Steel arrowtips" ||
		s.AmmoCapTable.For("Bolts") != 12 || s.AmmoCapTable.For("Steel arrowtips") != 18 || s.AmmoStop["Bolts"] != 500000 || s.AmmoCoins != 90000 ||
		s.AmmoMinVisitCoins != 5000 || s.AmmoSlotReserve != 2 || s.AmmoBuyAll {
		t.Errorf("decoded %+v", s)
	}
	for _, c := range []struct {
		name string
		raw  map[string]interface{}
		want string
	}{
		{"budget over the maximum is refused, not clamped", map[string]interface{}{"ammoCoins": 20000000.0}, "ammoCoins"},
		{"a negative min visit", map[string]interface{}{"ammoMinVisitCoins": -1.0}, "ammoMinVisitCoins"},
		{"a slot reserve over 10", map[string]interface{}{"ammoSlotReserve": 11.0}, "ammoSlotReserve"},
		{"ammoBuy that is not a boolean", map[string]interface{}{"ammoBuy": "maybe"}, "not true or false"},
		{"an unknown ammo shop", map[string]interface{}{"ammoShops": "Fionella"}, "ammoShops"},
		{"an unknown part", map[string]interface{}{"ammoItems": "Bronze arrowtips"}, "ammoItems"},
		{"a cap of 0", map[string]interface{}{"ammoPriceCaps": "Bolts=0"}, "the cap must be 1-1000"},
		{"a list holding a number", map[string]interface{}{"ammoShops": []interface{}{1.0}}, "not a string"},
	} {
		if _, err := decodeRuneRunnerSettings(c.raw); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

// shopRoundSettings lists the 2 November 2026 parts and Aemad beside the defaults.
func shopRoundSettings() map[string]interface{} {
	return map[string]interface{}{
		"ammoShops":     []interface{}{"Gerrant", "Lowe", "Hickton", "Aemad"},
		"ammoItems":     []interface{}{"Bolts", "Steel arrowtips", "Feather", "Bronze arrow", "Vial of water", "Steel arrow", "Rope"},
		"ammoStopAbove": []interface{}{"Bolts=1000000", "Steel arrowtips=150000", "Feather=250000", "Bronze arrow=200000", "Vial of water=20000", "Steel arrow=100000", "Rope=50000"},
	}
}

// Aemad's stop is in the round before every Lundail pair, it buys the Vials the pack has slots for, the Bronze arrows
// (one stack) and the Ropes, at the shelf's prices, and walks to the stand the Antipoison maker proved.
func TestAmmoStationBuysAemadsShelf(t *testing.T) {
	set := shopRoundSettings()
	set["vialTrips"] = 0.0 // the shuttle has its own test (runerunner_vial_test.go)
	r := ammoRig(t, "aemadrig1", 140, set)
	ring := runerunnerplan.PlanString(r.b.st.Ring, 0)
	if !strings.Contains(ring, "hop142 Aemad@142 Lundail@142 hop140 Aemad@140 Lundail@140 hop141 Aemad@141 Lundail@141") {
		t.Fatalf("ring %s", ring)
	}
	if !strings.Contains(r.log(), "ammo: on sites=Gerrant,Lowe,Hickton,Aemad items=Bolts,Steel arrowtips,Feather,Bronze arrow,Vial of water,Steel arrow,Rope") {
		t.Errorf("start line\n%s", r.log())
	}
	r.atKey(t, "Aemad@142")
	r.ctx.CurrentWorld = 142
	r.pack(r.rich(), 20, nil)
	var stand tile.Tile
	r.b.ammoBuySeam = func(site runerunnerplan.AmmoSite, planFor func(map[string]int, int) fleetplan.PurchasePlan) runerunner.AmmoVisit {
		stand = site.Stand
		seen := map[string]int{}
		for item, n := range map[string]int{"Vial of water": 500, "Bronze arrow": 500, "Rope": 20} {
			if site.Shop.Sells(item) {
				seen[item] = n
			}
		}
		plan := planFor(seen, r.rich())
		v := runerunner.AmmoVisit{Walked: true, Plan: plan, ShopVisit: fleetrunner.ShopVisit{Seen: seen, Bought: map[string]int{}}}
		for _, l := range plan.Lines {
			v.Bought[l.Rune] = l.Count
			v.Spent += site.Shop.CostToBuy(l.Rune, seen[l.Rune], l.Count)
		}
		return v
	}
	got := r.runAmmo()
	// 20 free slots less the slots kept for runes (ammoSlotReserve) is the room: the bronze stack is listed first and takes one, the
	// vials the rest, and no slot is left for the Rope. The mule's bank is unread, so the Always parts only.
	if got.Outcome != runerunnerplan.OutcomeBought || got.Bought["Vial of water"] == 0 || got.Bought["Bronze arrow"] != 500 || stand != runerunnerplan.AemadStand {
		t.Fatalf("Aemad: %+v stand %v\n%s", got, stand, r.log())
	}
	if want := r.b.set.AmmoSlotReserve; got.Bought["Vial of water"] != 20-want-1 {
		t.Errorf("%d vials with 20 free slots, %d kept for runes and one for the arrows", got.Bought["Vial of water"], want)
	}
	if !strings.Contains(r.log(), "buy shop=Aemad world=142 item=Bronze arrow seen=500 bought=500 spent=2325") {
		t.Errorf("no buy line\n%s", r.log())
	}
}

// Hickton on every world: the tail's hop is made for a shelf worth the visit and skipped, with its stop, for one that is not.
func TestHicktonEveryWorldHopIsWorthTheShelf(t *testing.T) {
	r := ammoRig(t, "hickworld1", 150, map[string]interface{}{"ammoEveryWorld": true})
	ring := runerunnerplan.PlanString(r.b.st.Ring, 0)
	if !strings.HasSuffix(ring, "WizardsGuild@152 Hickton@152 hop151 Hickton@151 hop150]") {
		t.Fatalf("ring %s", ring)
	}
	r.ctx.CurrentWorld = 152
	hopIdx := runerunnerplan.Index(r.b.st.Ring, "Hickton@151") - 1
	r.b.st.JumpTo(hopIdx)
	r.b.resumed = true
	r.b.runStation(r.ctx, r.b.deps(r.ctx), r.b.st.Ring[hopIdx], fleetrunner.Here(r.b.deps(r.ctx)))
	if len(r.hops) != 1 || r.hops[0] != 151 || r.b.st.Cursor != hopIdx+1 {
		t.Fatalf("hops %v cursor %d\n%s", r.hops, r.b.st.Cursor, r.log())
	}

	// A shelf not worth ammoMinVisitCoins: the block is skipped, Hickton@151 with it, and the way home is next.
	r2 := ammoRig(t, "hickworld2", 153, map[string]interface{}{"ammoEveryWorld": true, "ammoMinVisitCoins": 1000000.0})
	r2.ctx.CurrentWorld = 155
	hopIdx = runerunnerplan.Index(r2.b.st.Ring, "Hickton@154") - 1
	r2.b.st.JumpTo(hopIdx)
	r2.b.resumed = true
	r2.b.runStation(r2.ctx, r2.b.deps(r2.ctx), r2.b.st.Ring[hopIdx], fleetrunner.Here(r2.b.deps(r2.ctx)))
	if len(r2.hops) != 0 {
		t.Fatalf("hopped %v for a shelf under ammoMinVisitCoins\n%s", r2.hops, r2.log())
	}
	if s, _ := r2.b.st.Current(); s.Key() != "hop153" || !strings.Contains(r2.log(), "ammoMinVisitCoins") {
		t.Errorf("after the skip the cursor is at %s\n%s", s.Key(), r2.log())
	}
	// A hop with a rune shop in its block is unchanged: ammo never decides it (Aubury@... holds the block).
	if got := runerunnerplan.PlanString(runerunnerplan.BlockAmmo(r2.b.st.Ring, runerunnerplan.Index(r2.b.st.Ring, "Hickton@154")-1), 0); got != "[Hickton@154]" {
		t.Errorf("block ammo %s", got)
	}
}

// What Zaff's station buys goes on the mule's delivery list, and a Staff of fire is bought like a Staff of air.
func TestRuneRunnerStaffOfFireIsBoughtAndDelivered(t *testing.T) {
	r := ammoRig(t, "firestaff1", 160, map[string]interface{}{"zaffStaffs": 0.0, "zaffFireStaffs": 2.0, "zaffBattlestaffs": 5.0})
	items := strings.Join(r.b.set.DeliverItems(), ",")
	if !strings.Contains(items, "Staff of fire") || !strings.Contains(items, "Battlestaff") {
		t.Errorf("deliver items %s", items)
	}
	if !r.b.set.Staffs() || !strings.Contains(runerunnerplan.PlanString(r.b.st.Ring, 0), "Zaff@") {
		t.Errorf("no Zaff station: %s", runerunnerplan.PlanString(r.b.st.Ring, 0))
	}
	need := r.b.roundNeed(r.ctx, 0)
	// 3 Zaff stations (one per Aubury pair) x (2 fire staffs 3,030 + 5 battlestaffs 36,400).
	if want := 3 * (3030 + 36400); need.Staffs != want {
		t.Errorf("staff estimate %d, want %d", need.Staffs, want)
	}
}
