package runerunnerplan

// What a visit buys and what it costs: the per-rune price cap, the visit plan with its
// priority order and the reserve that keeps a later station funded, the coin draw, and the
// check that the shop's real prices are the ones the model predicts.

import (
	"fmt"
	"math"
	"strings"

	"rsb0t-go/internal/scripts/fleetplan"
)

// Caps is the per-rune price cap in coins, keyed by the rune's canonical name.
type Caps map[string]int

// For is the cap on one rune, matched ignoring case; 0 when the rune has none (never bought).
func (c Caps) For(rune string) int {
	if n, ok := c[rune]; ok {
		return n
	}
	for k, n := range c {
		if strings.EqualFold(k, rune) {
			return n
		}
	}
	return 0
}

// countOf reads a rune count out of a map keyed by display name, ignoring case.
func countOf(m map[string]int, rune string) (int, bool) {
	if n, ok := m[rune]; ok {
		return n, true
	}
	for k, n := range m {
		if strings.EqualFold(k, rune) {
			return n, true
		}
	}
	return 0, false
}

// BuyableUnderCap is how many of a rune a shop holding `stock` sells while each unit costs at
// most capGP, and what they cost -- each unit priced at the stock it leaves, exactly as the
// shop's own @buy_item walk prices it. It ignores coins (the caller's budget is a separate
// limit). A cap of 0 or less buys nothing: an unlisted rune has no cap and is never bought.
func BuyableUnderCap(sh fleetplan.RuneShop, rune string, stock, capGP int) (n, cost int) {
	if stock <= 0 || capGP <= 0 {
		return 0, 0
	}
	return sh.AffordableCapped(rune, stock, math.MaxInt32, stock, capGP)
}

// PlanVisit is one visit's purchase: `runes` walked in priority order, each line buying what
// the shelf holds under its cap and within what is left of `spendable`, less reserve[rune] --
// the coins held back for HIGHER-priority runes at the stations still ahead (ReserveAhead).
// Runes the shop does not sell are dropped, and so is a line that comes to nothing. The plan's
// Coins is exactly the sum of its lines and never more than spendable.
//
// A reserve never holds back more than MaxReserveShare of `spendable`. LIVE, 7 October 2026
// (spare104 at Aubury): a 100,000-coin pack met a 1,063,659-coin reserve for the Nature and Death
// the stations ahead were estimated to hold, so every rune after the first planned to buy nothing
// ("seen=1858 bought=0" for Air, Water, Body) and the account walked a boat chain for no runes.
// The reserve is an order of preference, not a veto: at least half the purse is always free to
// spend here.
//
// `seen` is the shelf: the real stock read off the open shop, or an estimate when planning
// ahead. A rune missing from it is treated as sold out.
func PlanVisit(sh fleetplan.RuneShop, seen map[string]int, spendable int, runes []string, caps Caps, reserve map[string]int) fleetplan.PurchasePlan {
	var p fleetplan.PurchasePlan
	left := spendable
	for _, r := range runes {
		l, ok := sh.Line(r)
		if !ok {
			continue
		}
		stock, _ := countOf(seen, r)
		budget := left
		if hold, ok := countOf(reserve, r); ok {
			budget -= CappedReserve(hold, spendable)
		}
		capGP := caps.For(l.Rune)
		if capGP <= 0 || budget <= 0 || stock <= 0 {
			continue
		}
		n, cost := sh.AffordableCapped(l.Rune, stock, budget, stock, capGP)
		if n <= 0 {
			continue
		}
		p.Lines = append(p.Lines, fleetplan.PlannedBuy{Rune: l.Rune, Count: n, Cost: cost, AssumedStock: stock})
		p.Coins += cost
		left -= cost
	}
	return p
}

// MaxReserveShare is the most of a purse a reserve may hold back: the numerator over two.
const MaxReserveShare = 1

// CappedReserve is `hold` limited to MaxReserveShare/2 of `spendable` (never negative).
func CappedReserve(hold, spendable int) int {
	if spendable <= 0 || hold <= 0 {
		return 0
	}
	if limit := spendable * MaxReserveShare / 2; hold > limit {
		return limit
	}
	return hold
}

// StationEstimate is one station still ahead in the round: its shop and the stock the board
// expects there.
type StationEstimate struct {
	Shop  fleetplan.RuneShop
	Stock map[string]int
}

// ReserveAhead is, for each rune, the coins a visit must hold back so the stations still ahead
// can buy every HIGHER-priority rune they are expected to hold under its cap: rune i reserves the
// estimated capped cost of runes 0..i-1 at every station in `ahead`. The first rune reserves
// nothing (buying it here is as good as buying it there). Without it, chaos at Betty on the
// mule's world -- 70,975 coins for a full shelf -- would spend the purse that the minds at
// Aubury on the other world need.
//
// Only stations paid from the pack belong in `ahead`: Lundail's coins come out of the bank at
// Gundai.
func ReserveAhead(ahead []StationEstimate, runes []string, caps Caps) map[string]int {
	out := make(map[string]int, len(runes))
	for i, r := range runes {
		hold := 0
		for _, st := range ahead {
			for _, higher := range runes[:i] {
				stock, _ := countOf(st.Stock, higher)
				_, cost := BuyableUnderCap(st.Shop, higher, stock, caps.For(higher))
				hold += cost
			}
		}
		out[r] = hold
	}
	return out
}

// EstCost is what the listed runes cost at these stations under their caps, from the
// estimates: the coins an east leg should carry.
func EstCost(stations []StationEstimate, runes []string, caps Caps) int {
	total := 0
	for _, st := range stations {
		for _, r := range runes {
			stock, _ := countOf(st.Stock, r)
			_, cost := BuyableUnderCap(st.Shop, r, stock, caps.For(r))
			total += cost
		}
	}
	return total
}

// Worth is how many runes a station would buy under the caps from `stock`, ignoring coins: the
// number the skip rule (minVisitRunes) and the other world's hop rule (hopMinGain) compare.
func Worth(sh fleetplan.RuneShop, stock map[string]int, runes []string, caps Caps) int {
	n := 0
	for _, r := range runes {
		s, _ := countOf(stock, r)
		k, _ := BuyableUnderCap(sh, r, s, caps.For(r))
		n += k
	}
	return n
}

// ---- Zaff's staffs ----

// PlanStaffs is one Zaff visit's purchase: up to `want` Staffs of air from a shelf holding
// `stock`, each priced by shop.rs2's walk (the Zaff table) and none dearer than capGP, within
// `spendable` coins. The staff is the ring's lowest priority, so the caller's spendable already
// leaves the fare and every listed rune still ahead funded.
func PlanStaffs(stock, want, capGP, spendable int) (n, cost int) {
	return PlanZaff(fleetplan.StaffOfAir, stock, want, capGP, spendable)
}

// PlanZaff is PlanStaffs for any of Zaff's lines (a Staff of air, a Battlestaff).
func PlanZaff(item string, stock, want, capGP, spendable int) (n, cost int) {
	if want <= 0 || capGP <= 0 || stock <= 0 || spendable <= 0 {
		return 0, 0
	}
	return Zaff.AffordableCapped(item, stock, spendable, want, capGP)
}

// ZaffEstCost is what a Zaff visit is expected to cost for one line from a full shelf.
func ZaffEstCost(item string, want, capGP int) int {
	l, ok := Zaff.Line(item)
	if !ok {
		return 0
	}
	_, cost := PlanZaff(item, l.Base, want, capGP, math.MaxInt32)
	return cost
}

// StaffEstCost is what a Zaff visit is expected to cost from a full shelf: the coins a draw adds
// for each staff station ahead.
func StaffEstCost(want, capGP int) int {
	return ZaffEstCost(fleetplan.StaffOfAir, want, capGP)
}

// StaffPlan is PlanStaffs as a PurchasePlan, for the price check (PredictedSpend) to read.
func StaffPlan(stock, want, capGP, spendable int) fleetplan.PurchasePlan {
	return ZaffPlan(fleetplan.StaffOfAir, stock, want, capGP, spendable)
}

// ZaffPlan is PlanZaff as a PurchasePlan.
func ZaffPlan(item string, stock, want, capGP, spendable int) fleetplan.PurchasePlan {
	n, cost := PlanZaff(item, stock, want, capGP, spendable)
	if n <= 0 {
		return fleetplan.PurchasePlan{}
	}
	return fleetplan.PurchasePlan{Coins: cost, Lines: []fleetplan.PlannedBuy{{Rune: item, Count: n, Cost: cost, AssumedStock: stock}}}
}

// ---- coins ----

// NoCoinsMargin is how many coins above the fare the bank must spare (after the reserve) for an
// east leg to be worth drawing for at all.
const NoCoinsMargin = 1000

// DrawInput is one coin draw's inputs, all in coins.
type DrawInput struct {
	// TripCoins bounds one draw (the tripCoins setting).
	TripCoins int
	// BankCoins is the bank's coin row; CoinReserve what must stay in it.
	BankCoins, CoinReserve int
	// FareCoins is carried for the boat fares and never spent in a shop.
	FareCoins int
	// InPack is the coins already in the pack.
	InPack int
	// EstCost is the leg's estimated spend (EstCost over the stations that will run).
	EstCost int
}

// Draw is DrawAmount's answer.
type Draw struct {
	// Amount is what to withdraw now (0 for nothing).
	Amount int
	// NoCoins is true when the bank cannot fund a leg at all: the buyer only delivers and idles.
	NoCoins bool
	// Need is what the leg wanted in the pack in all: spend plus fares.
	Need int
	// Why says in a sentence what bounded the draw, for the log line: "the pack already holds
	// enough", "bank spare under the leg's cost", "tripCoins" and so on.
	Why string
}

// DrawAmount is min(tripCoins, bankCoins-coinReserve, estCost) + fareCoins - coinsInPack, never
// negative and never more than the bank can spare above its reserve. A bank that cannot spare
// the fare plus NoCoinsMargin is NO COINS.
func DrawAmount(in DrawInput) Draw {
	spare := in.BankCoins - in.CoinReserve
	if spare < in.FareCoins+NoCoinsMargin {
		return Draw{NoCoins: true, Need: in.FareCoins + NoCoinsMargin}
	}
	spend := in.EstCost
	if spend > in.TripCoins {
		spend = in.TripCoins
	}
	if spend > spare {
		spend = spare
	}
	if spend < 0 {
		spend = 0
	}
	need := spend + in.FareCoins
	amt := need - in.InPack
	why := "the leg's estimated cost plus the fare"
	switch {
	case in.EstCost > in.TripCoins && in.TripCoins <= spare:
		why = fmt.Sprintf("tripCoins %d is under the leg's estimated cost %d", in.TripCoins, in.EstCost)
	case in.EstCost > spare:
		why = fmt.Sprintf("the bank spares %d (above the %d reserve), under the leg's estimated cost %d", spare, in.CoinReserve, in.EstCost)
	}
	if amt > spare {
		amt = spare
	}
	if amt <= 0 {
		amt = 0
		why = fmt.Sprintf("the pack already holds %d of the %d the leg wants (%s)", in.InPack, need, why)
	}
	return Draw{Amount: amt, Need: need, Why: why}
}

// TopUpDue reports whether the pack is short enough of what the rest of the east leg needs to be
// worth a stop at the nearest bank: at least NoCoinsMargin short, and short by at least a quarter
// of the need. It is what sends a respawned or restarted buyer to Draynor or Varrock East before
// a shop it could not otherwise pay.
func TopUpDue(inPack, need, fare int) bool {
	short := need + fare - inPack
	return short >= NoCoinsMargin && short*4 >= need
}

// ---- the price check ----

// PriceMismatchTolerance is how far a visit's real spend may stray from the model, as a fraction
// of the model, before it counts as a mismatch.
const PriceMismatchTolerance = 0.10

// PriceMismatchLimit is how many mismatches disable a shop and world pair until an operator
// restarts the script.
const PriceMismatchLimit = 2

// PredictedSpend is what the model says the runes that actually landed should have cost: each
// line priced by CostToBuy from the stock it was planned at, for the count bought.
func PredictedSpend(sh fleetplan.RuneShop, plan fleetplan.PurchasePlan, bought map[string]int) int {
	total := 0
	for _, l := range plan.Lines {
		got, _ := countOf(bought, l.Rune)
		if got <= 0 {
			continue
		}
		total += sh.CostToBuy(l.Rune, l.AssumedStock, got)
	}
	return total
}

// PriceMismatch reports whether a visit's real spend differs from the model's prediction by more
// than PriceMismatchTolerance. Nothing predicted is never a mismatch (nothing was bought, or the
// model had no line to compare).
func PriceMismatch(predicted, spent int) bool {
	if predicted <= 0 {
		return false
	}
	diff := spent - predicted
	if diff < 0 {
		diff = -diff
	}
	return float64(diff) > PriceMismatchTolerance*float64(predicted)
}

// ---- what a visit left on the shelf ----

// ShelfLeft is one rune a visit left on the shelf and why. Users asked for exactly this line
// per station (7 October 2026): "left N <rune>: <reason>", so a visit that bought less than the
// shelf holds can be checked against its coins and its caps.
type ShelfLeft struct {
	Rune   string
	Left   int
	Reason string
	// ForCoins is true when the purse was the reason, and Cost what the rest of the shelf would
	// cost at the shelf's own prices: the round's "left for coins" total is their sum.
	ForCoins bool
	Cost     int
}

// LeftInput is one visit's facts for LeftOnShelf.
type LeftInput struct {
	Shop  fleetplan.RuneShop
	Runes []string
	Caps  Caps
	// Seen is the shelf as read when the shop opened; Bought what landed in the pack.
	Seen, Bought map[string]int
	// Plan is what the visit decided to buy.
	Plan fleetplan.PurchasePlan
	// Spendable is the coins the plan was made from (pack less the fare); Reserve the per-rune
	// hold for higher-priority runes at the stations ahead (before CappedReserve).
	Spendable int
	Reserve   map[string]int
	// VisitReason is the shop visit's own failure text (the purchase's stop reason), "" when none.
	VisitReason string
}

// LeftOnShelf lists every rune the shop sells that the visit left some of on the shelf, with the
// reason: the price at the shelf's new level over the cap, coins (the purse less the reserve for
// dearer runes ahead could not pay for the next unit), or a purchase that fell short of its plan.
//
// The coins reason names what was LEFT when the rune's turn came, not the visit's opening purse:
// the runes are bought in priority order, so on 12 October the Wizards' Guild's "145,056 spendable"
// was the purse before the elementals took it, and read as plenty for a Law rune that had nothing
// left to buy it with.
func LeftOnShelf(in LeftInput) []ShelfLeft {
	var out []ShelfLeft
	remaining := in.Spendable
	for _, r := range in.Runes {
		l, ok := in.Shop.Line(r)
		if !ok {
			continue
		}
		seen, _ := countOf(in.Seen, r)
		got, _ := countOf(in.Bought, r)
		// What this rune's turn had to spend, then what it spent.
		turn := remaining
		if got > 0 && seen > 0 {
			remaining -= in.Shop.CostToBuy(l.Rune, seen, got)
		}
		left := seen - got
		if seen <= 0 || left <= 0 {
			continue
		}
		capGP := in.Caps.For(l.Rune)
		next := in.Shop.PriceAt(l.Rune, left)
		planned := 0
		for _, pl := range in.Plan.Lines {
			if strings.EqualFold(pl.Rune, l.Rune) {
				planned = pl.Count
			}
		}
		var why string
		forCoins := false
		switch {
		case capGP <= 0:
			why = "no price cap for this rune"
		case next > capGP:
			why = fmt.Sprintf("price %d over cap %d", next, capGP)
		case got < planned:
			why = fmt.Sprintf("bought %d of %d planned", got, planned)
			if in.VisitReason != "" {
				why += ": " + in.VisitReason
			}
		default:
			hold, _ := countOf(in.Reserve, l.Rune)
			why = fmt.Sprintf("coins: %d left of the visit's %d spendable when it came up, next unit %d, %d held for higher-priority runes ahead",
				turn, in.Spendable, next, CappedReserve(hold, in.Spendable))
			forCoins = true
		}
		sl := ShelfLeft{Rune: l.Rune, Left: left, Reason: why, ForCoins: forCoins}
		if forCoins {
			sl.Cost = in.Shop.CostToBuy(l.Rune, left, left)
		}
		out = append(out, sl)
	}
	return out
}
