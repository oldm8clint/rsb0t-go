package essenceplan

// The whole-stack buy-out (runecraft P4, docs/runecraft_p4_2026-10-14.md). a user request: "request mules entire cash stack and
// spend it on everything instead of just running out". The RuneRunner's purse was the first of it (the mule's whole published coin
// stack less a floor, asked in as many orders as the mule's per-order cap needs); this is the same rule for the Nature campaign's
// essence, which the mule's Rune essence row feeds the carriers from:
//
//	the ask    the mule's published coins less BuyOutDefaultFloor (or craftRuneMuleCoinFloor), capped at what the campaign still lacks
//	           at the worst price (Want x MarketUnitCeiling), in trips of at most craftRuneBuyTrip coins (0: the whole stack in one)
//	the spend  every stall that lists Rune essence in the owner's order, a lot at a time (BuyOutLot essence, under every stall's
//	           per-trade coin limit), until the coins or the want are gone: a stall that lists less than the want is bought OUT, not
//	           skipped, and the next stall takes the rest (questplan.PickBuyOut)
//	the end    the essence arrives as notes and, with the coins left over, goes to the mule in one hand-over
//
// What stops it from being repeated for nothing (review of 14 Oct 2026): the price page is read BEFORE the mule is asked for a coin
// (a buy-out needs a stall that lists the essence, and asks for no more than the stalls list), a buy-out that bought under BuyOutMin
// is remembered for BuyOutNoStockWait, and a gate that ran dry gets one refill without a pause, not one every time (MaxDryRefills).
//
// The bounded market lot of P2 (PlanMarketLot: a budget, a million-coin floor, one stall) stays what it was, for the levelling round's
// last rung. This file is pure.

import (
	"fmt"
	"time"
)

// The buy-out's numbers.
const (
	// BuyOutDefaultFloor is the coins the mule keeps when the main account takes its whole stack (craftRuneMuleCoinFloor): the cohort's small
	// asks (a kit's thousand-coin line, a fisher's fare), the RuneRunner's own floor (muleCoinFloor).
	BuyOutDefaultFloor = 10_000
	// BuyOutPriceHint is the price an essence is expected to cost at the stalls (the last-sold price every priced listing showed on
	// 13 Oct 2026, questplan.PipEssenceUnitFallback): what the coins are SAID to buy. The ceiling (MarketUnitCeiling) is what one
	// may be paid, never more.
	BuyOutPriceHint = 100
	// BuyOutMin is the fewest essence a buy-out is worth a trip to the market for.
	BuyOutMin = 1000
	// BuyOutLot is the most essence one stall purchase buys: 20,000 is 2,000,000 coins at the stalls' 100, 3,000,000 at the ceiling,
	// under the per-trade limit of every stall known (Seers Market about 5,000,000, S15 about 1,000,000 and not in the order).
	BuyOutLot = 20_000
	// BuyOutMaxLots bounds the purchases of one trip (100,000 essence in lots of 20,000, with failures).
	BuyOutMaxLots = 24
	// BuyOutMaxTrips bounds the trips of one buy-out.
	BuyOutMaxTrips = 12
	// BuyOutNoStockWait is how long a buy-out that reached the stalls and bought under BuyOutMin (BuyOutShort) holds the next one
	// off: the three player-run stalls restock when their owners do, not when asked, and every attempt asks the mule for its whole
	// coin stack and walks it to Seers and back.
	BuyOutNoStockWait = 3 * time.Hour
	// MaxDryRefills is how many times in a row a gate round that ran dry is followed by a refill with no pause. The next round of such
	// a refill that ends dry again without a single trade pauses the campaign instead: the refill did not help.
	MaxDryRefills = 1
)

// BuyOutShort says a buy-out that reached the stalls came up short: it bought under the least worth a trip (BuyOutMin, or the want
// when that is smaller).
func BuyOutShort(bought, want int) bool { return want > 0 && bought < min(BuyOutMin, want) }

// BuyOutTerms are PlanBuyOut's inputs.
type BuyOutTerms struct {
	// Want is the essence the campaign still lacks (runecraftplan.CampaignNeed).
	Want int
	// MuleKnown says the mule's bank is published; MuleCoins is what it holds and Floor what it keeps.
	MuleKnown bool
	MuleCoins int
	Floor     int
	// Held is the coins the main account already holds (what an earlier trip left).
	Held int
	// Asked is the coins already asked of the mule by earlier trips of this buy-out (the published figure lags: it still counts them).
	Asked int
	// Trip is craftRuneBuyTrip: the most coins one trip asks for (0: the whole stack in one trip).
	Trip int
	// Listed is the essence the stalls list in all, from the price page (questplan.BuyOutStalls); the buy-out looks for no more than
	// that and asks the mule for no more coins than that costs. 0 is unknown (a stall that does not show its stock), not none: a
	// price page that lists no essence ends the buy-out before it is planned.
	Listed int
	// OnMarketWorld says the stalls stand on this world (they do not on worlds 2 and 3).
	OnMarketWorld bool
}

// BuyOutPlan is what PlanBuyOut allows.
type BuyOutPlan struct {
	// Ask is the coins to ask the mule for on this trip; Qty the essence to look for (an upper bound: the stalls' own prices end the
	// buying); MaxCoins what the trip may spend (Ask plus what is held).
	Ask, Qty, MaxCoins int
	// Whole is the mule's whole stack less its floor, less what is already asked; Trips how many trips the buy-out takes in all at the
	// trip size.
	Whole, Trips int
}

// PlanBuyOut is the whole-stack buy-out this trip may make, or the reason there is none.
func PlanBuyOut(t BuyOutTerms) (BuyOutPlan, string) {
	switch {
	case t.Want <= 0:
		return BuyOutPlan{}, "no essence is short"
	case !t.OnMarketWorld:
		return BuyOutPlan{}, "the Seers stalls do not stand on this world"
	case !t.MuleKnown:
		return BuyOutPlan{}, "the mule's coins are not published, so a buy-out cannot be funded"
	}
	whole := t.MuleCoins - max(0, t.Floor) - max(0, t.Asked)
	if whole+t.Held <= 0 {
		return BuyOutPlan{}, fmt.Sprintf("the mule holds %d coins, not above its %d floor", t.MuleCoins, t.Floor)
	}
	// The essence worth looking for: what the campaign lacks, and no more than the stalls list.
	want := t.Want
	if t.Listed > 0 {
		want = min(want, t.Listed)
		if want < min(BuyOutMin, t.Want) {
			return BuyOutPlan{}, fmt.Sprintf("the stalls list only %d essence in all, under the %d worth a trip", t.Listed, min(BuyOutMin, t.Want))
		}
	}
	// The coins the want needs at the worst price, less what is already in hand.
	needed := max(0, want*MarketUnitCeiling-t.Held)
	ask := max(0, min(whole, needed))
	if t.Trip > 0 {
		ask = min(ask, t.Trip)
	}
	funds := ask + t.Held
	qty := min(want, funds/BuyOutPriceHint)
	if qty < min(BuyOutMin, want) {
		return BuyOutPlan{}, fmt.Sprintf("%d coins buy %d essence at about %d each, under the %d worth a trip", funds, funds/BuyOutPriceHint, BuyOutPriceHint, min(BuyOutMin, want))
	}
	p := BuyOutPlan{Ask: ask, Qty: qty, MaxCoins: funds, Whole: whole, Trips: 1}
	if t.Trip > 0 && ask > 0 {
		total := min(whole, max(0, want*MarketUnitCeiling-t.Held))
		p.Trips = (total + t.Trip - 1) / t.Trip
	}
	return p, ""
}
