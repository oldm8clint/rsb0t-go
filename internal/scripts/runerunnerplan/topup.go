package runerunnerplan

// Coins from the mule, and the coin side of a station's skip rule.
//
// RuneRunner spends only its own bank. Its first live round (26 September, Acct211) spent about
// 258,000 of a 301,000 bank, and round 2 then drew 25,000 and ran out: every shop station
// skipped with NO COINS while the mule held about 4.8 million. So when bank plus pack falls under
// topUpBelow, the draw station -- which stands at the mule's post -- asks the mule for
// topUpAmount over the wall bus, the same item order every other script uses
// (donorrunner.Await; the mule's side is MuleReceiver's hand-out, bounded there by its own
// coinsPerOrder, coinsPerWindow and handoutPerDonor). Nothing new crosses the bus.
//
// THE WHOLE STACK (a user request: "youll need to request mules entire cash stack and spend it on
// everything instead of just running out"). LIVE that day the round's bill was 1,099,730 and the runner held
// 857,720, so it asked for topUpAmount, 300,000 -- 252,000 short of the round, with the mule holding 11,229,284
// -- and the ammo stops then skipped because the pack was short of the runes ahead. So when the runner holds
// less than the round's FULL bill (runes, staffs, Lundail, margin, fares, ammo and the bank's reserve), the
// ask is the mule's whole published coin stack less muleCoinFloor, in as many orders as the mule's coinsPerOrder
// needs (TopUpMaxOrders), not the shortfall and not topUpAmount. topUpAmount is then only the ask for a mule
// whose bank is not published, and a lower bound on it. topUpWholeStack false restores the old rule.
//
// The ask is still bounded in time: one ask per TopUpRetry, an order that brings nothing ends it, and the mule's
// own coinsPerOrder, coinsPerWindow and handoutPerDonor bound what it hands over (MULE LIMIT? in the log).

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

// Top-up defaults and bounds.
const (
	// DefaultTopUpBelow is the bank-plus-pack coin count under which the buyer asks the mule.
	// About two rounds of steady-state buying (roughly 70,000 an hour).
	DefaultTopUpBelow = 150000
	// DefaultTopUpAmount is what one top-up asks for: about four hours of steady-state buying.
	DefaultTopUpAmount = 300000
	// MaxTopUpAmount bounds the setting and a buy-everything circuit's shortfall ask. The mule's own coinsPerWindow
	// is the fleet-wide bound. Six million since 8 Oct 2026 (owner: "more coins should be handed over to ensure
	// every single rune is purchased"): a full circuit is estimated at up to 5.5M. It does NOT bound a whole-stack
	// ask (topUpWholeStack), which is the mule's published stack.
	MaxTopUpAmount = 6000000
	// TopUpMaxOrders bounds the orders one top-up places. The mule clamps an order to its coinsPerOrder (1,000,000
	// live since 8 Oct 2026, the order grammar's ceiling TopUpOrderMax), so a whole stack takes one order per
	// million: the mule held 11,229,284 on 14 Oct 2026, twelve orders. Forty (40,000,000) is above every
	// handoutPerDonor (16 live) and coinsPerWindow (15,000,000 live) the mule is set to, so the mule's limits end
	// an ask before this does; an order that brings nothing ends it sooner.
	TopUpMaxOrders = 40
	// DefaultTopUpWholeStack is the topUpWholeStack setting's default: on.
	DefaultTopUpWholeStack = true
	// DefaultMuleCoinFloor is the muleCoinFloor setting's default: the coins a top-up leaves in the mule's bank.
	// 10,000 is the concrete consumer's number, not a round figure: the cohort's own small coin asks are a
	// kit's 1,000-coin line (3 to 4 an hour in the 14 Oct log), a fisher's 500-coin tool fare, a trainer's
	// 3,500-coin blade purse and a Kalphite Queen kit's few hundred, and a mule found empty answers them "none in
	// the bank", which donorrunner.Refusals then replays to EVERY account for thirty minutes (donorplan
	// RefusalCooldown) although the mule's inflow (774,000 an hour) refills it within a minute or two. Ten
	// thousand covers ten of the usual 1,000-coin lines; the runner's whole-stack ask takes everything
	// above it. Before 14 Oct 2026 the floor was 100,000, which no consumer needs.
	DefaultMuleCoinFloor = 10000
	// MaxMuleCoinFloor is the muleCoinFloor setting's ceiling.
	MaxMuleCoinFloor = 10000000
	// TopUpRetry is the least time between two top-ups, whatever the last one got. A mule that
	// declined, timed out or gave short is not asked again for this long.
	TopUpRetry = 20 * time.Minute
	// TopUpAskTimeout is one order's budget: the mule queues the order behind the kit asks ahead of
	// it, then banks, withdraws and trades. It was 150 seconds, and LIVE on 7 October 2026
	// (acct152) the first order was handed over after 100 seconds but the second, queued behind
	// kits the mule quoted at "about 4m45s", was given up on after 150 and "the mule never handed
	// it over": the runner got 170,000 of the 300,000 it asked for. Six minutes covers that queue.
	TopUpAskTimeout = 6 * time.Minute
)

// TopUpInput is one top-up decision's inputs.
type TopUpInput struct {
	// Below and Amount are the settings; either at 0 turns the top-up off.
	Below, Amount int
	// BankCoins is the bank's coin row just read; PackCoins the coins in the pack.
	BankCoins, PackCoins int
	// MuleWorld is the mule's world, MuleWorldKnown whether it is known; MyWorld this account's.
	MuleWorld      int
	MuleWorldKnown bool
	MyWorld        int
	// MuleHeard reports a mule heartbeat on this world lately.
	MuleHeard bool
	// MuleCoins is the coins the mule's published bank holds, when MuleCoinsKnown (a bank published
	// within peerplan.DefaultMaxAge). Unknown asks for Amount (with WholeStack, the shortfall to Below when larger).
	MuleCoins      int
	MuleCoinsKnown bool
	// MuleFloor is the coins a top-up leaves in the mule's bank (the muleCoinFloor setting): the ask is the
	// published stack less this, and no ask is made for a spare under NoCoinsMargin. 0 takes every coin.
	MuleFloor int
	// WholeStack is the topUpWholeStack setting: with a published stack the ask is ALL of it above MuleFloor,
	// whatever Amount says (the shortfall and topUpAmount are then only the ask for an unpublished stack).
	// False is the old rule: Amount, clamped to the spare.
	WholeStack bool
	// LastAsk is when the last top-up was asked for (zero: never).
	LastAsk time.Time
	Now     time.Time
}

// TopUpVerdict is MuleTopUp's answer: Ask coins now (0: do not ask), and why.
type TopUpVerdict struct {
	Ask    int
	Reason string
}

// MuleTopUp decides whether to ask the mule for coins now. It asks when bank plus pack is under Below, the
// mule is on this world and heard, and no top-up was asked for within TopUpRetry: for the mule's whole published
// stack above MuleFloor (WholeStack), else for Amount.
func MuleTopUp(in TopUpInput) TopUpVerdict {
	held := in.BankCoins + in.PackCoins
	switch {
	case in.Below <= 0 || in.Amount <= 0:
		return TopUpVerdict{Reason: "top-up off (topUpBelow or topUpAmount is 0)"}
	case held >= in.Below:
		return TopUpVerdict{Reason: fmt.Sprintf("%d coins held, not under topUpBelow %d", held, in.Below)}
	case !in.MuleWorldKnown:
		return TopUpVerdict{Reason: "the mule's world is not known"}
	case in.MuleWorld != in.MyWorld:
		return TopUpVerdict{Reason: fmt.Sprintf("on world %d; the mule is on world %d", in.MyWorld, in.MuleWorld)}
	case !in.MuleHeard:
		return TopUpVerdict{Reason: "the mule has not been heard on this world"}
	case !in.LastAsk.IsZero() && in.Now.Sub(in.LastAsk) < TopUpRetry:
		return TopUpVerdict{Reason: fmt.Sprintf("asked %s ago; the next ask is after %s", in.Now.Sub(in.LastAsk).Round(time.Second), TopUpRetry)}
	}
	reason := fmt.Sprintf("%d coins held, under topUpBelow %d", held, in.Below)
	ask := in.Amount
	switch {
	case in.MuleCoinsKnown:
		// The runner leaves the mule MuleFloor coins: the cohort's small asks (a kit's 1,000-coin line, a
		// stranded account's fare, a trainer's blade purse) are served out of the same bank, and a decline
		// is remembered fleet-wide for thirty minutes (DefaultMuleCoinFloor).
		spare := in.MuleCoins - in.MuleFloor
		kept := ""
		if in.MuleFloor > 0 {
			kept = fmt.Sprintf(", %d of them kept for the cohort's small asks", in.MuleFloor)
		}
		switch {
		case spare < NoCoinsMargin:
			return TopUpVerdict{Reason: fmt.Sprintf("%s; the mule's published bank holds %d coins%s", reason, in.MuleCoins, kept)}
		case in.WholeStack:
			reason += fmt.Sprintf("; asking the mule's whole published stack: %d of its %d coins%s (topUpAmount %d is not a cap)", spare, in.MuleCoins, kept, in.Amount)
			ask = spare
		case ask > spare:
			reason += fmt.Sprintf("; asking %d, not %d: the mule's published bank holds %d and keeps %d", spare, ask, in.MuleCoins, in.MuleFloor)
			ask = spare
		}
	case in.WholeStack:
		// The mule's stack is not published (no bank read lately): ask for the whole shortfall to the bill,
		// at least Amount. The mule gives what it holds and an order that brings nothing ends the ask.
		if short := in.Below - held; short > ask {
			ask = short
		}
		reason += fmt.Sprintf("; the mule's stack is not published: asking %d, the round's shortfall or topUpAmount", ask)
	}
	return TopUpVerdict{Ask: ask, Reason: reason}
}

// LimitDecline reports whether a mule's decline reason is one of its hand-out limits rather than an
// empty bank: "already handed you one recently" (handoutPerDonor) or "hand-out limit for that item is
// spent" (coinsPerWindow). A first full-shelf ask is twelve orders of 500,000, and eight orders a window
// is all one donor is handed.
func LimitDecline(reason string) bool {
	r := strings.ToLower(reason)
	return strings.Contains(r, "already handed you") || strings.Contains(r, "hand-out limit")
}

// MaxCircuitFunds bounds the bank-plus-pack total a buy-everything circuit asks the mule to fund.
const MaxCircuitFunds = 8000000

// CircuitTopUp scales the top-up for a buy-everything runner: the purse should hold what the
// circuit still ahead is estimated to cost (est, from EstCost at the caps buyAll sets, and the ammo stations'
// estimate beside it: RoundNeed.Est()+RoundNeed.Ammo, the round's FULL bill) plus the
// bank's reserve and the fare, not a fixed 150,000. It returns the Below and Amount to feed
// MuleTopUp: Below raised to that need (at most MaxCircuitFunds) and Amount to cover the
// shortfall (at most MaxTopUpAmount). A top-up that is off (either setting 0) stays off, and so
// does one with buyAll off.
func CircuitTopUp(buyAll bool, below, amount, est, reserve, fare, held int) (int, int) {
	if !buyAll || below <= 0 || amount <= 0 {
		return below, amount
	}
	want := est + reserve + fare
	if want > MaxCircuitFunds {
		want = MaxCircuitFunds
	}
	if want > below {
		below = want
	}
	if short := below - held; short > amount {
		amount = short
		if amount > MaxTopUpAmount {
			amount = MaxTopUpAmount
		}
	}
	return below, amount
}

// TopUpNextOrder is the next order of a top-up that has placed `orders` orders and received
// `got` of `ask`: what is still wanted, or 0 to stop (all received, TopUpMaxOrders placed, or the
// last order brought nothing -- lastGot <= 0 after the first).
func TopUpNextOrder(ask, got, orders, lastGot int) int {
	if orders >= TopUpMaxOrders || got >= ask {
		return 0
	}
	if orders > 0 && lastGot <= 0 {
		return 0
	}
	return min(ask-got, TopUpOrderMax)
}

// TopUpOrderMax is the most one coin order asks for: the order grammar's own ceiling (orderplan.QtyMax),
// over which the mule's parser refuses the whole order as "quantity is too large" before its
// coinsPerOrder clamp is reached. Live 8 Oct 2026 every circuit top-up asked 2,000,000 in one order and
// got 0, so spare104 reached the Wizards' Guild, the only blood rune shop, with 501 coins.
const TopUpOrderMax = 1_000_000

// PurseFresh is how recent a bank reading may be for the purse station to trust it without
// walking to the post: the reading's age and the coins it held.
const PurseFresh = 15 * time.Minute

// PurseNeeded reports whether the purse station has anything to do: it does unless a reading
// younger than PurseFresh shows bank plus pack already at or over topUpBelow, or the top-up is
// off. The reading is the bank's coins as last read; the caller passes the zero time when it has
// never read one.
func PurseNeeded(below, amount, bankCoins int, readAt time.Time, packCoins int, now time.Time) (bool, string) {
	switch {
	case below <= 0 || amount <= 0:
		return false, "top-up off (topUpBelow or topUpAmount is 0)"
	case !readAt.IsZero() && now.Sub(readAt) < PurseFresh && bankCoins+packCoins >= below:
		return false, fmt.Sprintf("%d coins held (read %s ago), not under topUpBelow %d", bankCoins+packCoins, now.Sub(readAt).Round(time.Second), below)
	}
	return true, ""
}

// ---- affording a station ----

// AffordInput is one station's coin check before any walk.
type AffordInput struct {
	Shop  fleetplan.RuneShop
	Stock map[string]int
	Runes []string
	Caps  Caps
	// Coins is what the visit could spend: the pack's coins less the fare, plus what a bank on
	// the way could still spare above the reserve (each capped as the draw would cap it).
	Coins int
	// MinVisitRunes is the smallest purchase worth the walk.
	MinVisitRunes int
}

// Affordable reports whether a station's purchase, planned against the coins the visit could
// really spend, reaches MinVisitRunes. Worth (ignoring coins) can say a shelf is worth a visit
// that the purse cannot pay for: live on 26 September a Lundail trip crossed the Wilderness and
// bought nothing. It returns the runes the coins buy.
func Affordable(in AffordInput) (bool, int, string) {
	if in.Coins <= 0 {
		return false, 0, fmt.Sprintf("no coins to spend (%d)", in.Coins)
	}
	n := PlanVisit(in.Shop, in.Stock, in.Coins, in.Runes, in.Caps, nil).Runes()
	if n < in.MinVisitRunes {
		return false, n, fmt.Sprintf("%d coins buy only %d runes here, under minVisitRunes %d", in.Coins, n, in.MinVisitRunes)
	}
	return true, n, ""
}

// FundableNeed is `need` limited to what the pack plus the bank's last reading can supply (the bank
// less its reserve), so TopUpDue does not send a runner to a shop's bank for coins that bank does not
// hold. LIVE 7 October 2026: a 1.1-million-coin circuit against a 316,000-coin bank made TopUpDue true at
// every station, and the runner walked to Draynor, Varrock East and Yanille to draw 0, 60 and 60 coins.
// A bank never read (bankRead false) is taken as able to fund the need.
func FundableNeed(inPack, need int, bankRead bool, bankCoins, reserve int) int {
	if !bankRead {
		return need
	}
	spare := bankCoins - reserve
	if spare < 0 {
		spare = 0
	}
	if limit := inPack + spare; need > limit {
		return limit
	}
	return need
}

// SpendableFor is the coins a station could spend: the pack's coins less the fare, plus, when a
// bank on the way can be drawn from, the last bank reading less the reserve, with the draw part
// capped at tripCoins. A bank never read counts as drawable up to tripCoins (the draw will read
// it; the check must not skip on a missing reading).
func SpendableFor(packCoins, fare int, bankRead bool, bankCoins, reserve, tripCoins int, canDraw bool) int {
	spend := packCoins - fare
	if spend < 0 {
		spend = 0
	}
	if !canDraw {
		return spend
	}
	drawable := tripCoins
	if bankRead {
		drawable = bankCoins - reserve
		if drawable > tripCoins {
			drawable = tripCoins
		}
	}
	if drawable > 0 {
		spend += drawable
	}
	return spend
}

// GundaiShort reports whether a Gundai withdrawal left the pack unable to buy a worthwhile visit:
// the bank never opened (live, 26 September: "Gundai's dialogue never opened the bank", then
// Lundail bought nothing with an empty pouch) or drew too little. The caller skips the purchase,
// leaves the Mage Bank and strikes the pair, so three such trips take Lundail off for an hour.
func GundaiShort(in AffordInput) bool {
	ok, _, _ := Affordable(in)
	return !ok
}
