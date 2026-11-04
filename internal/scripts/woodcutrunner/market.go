package woodcutrunner

// Buying an axe. Two genuinely different mechanisms, because the two places that sell one are
// not the same kind of thing:
//
//   - BuyFromBob is an ordinary NPC shop (Bob's Brilliant Axes, Lumbridge). It opens a shop
//     interface and buys. It can only ever reach a steel axe — see woodcutplan/axe.go for the
//     measured stock.
//
//   - BuyAtMarket is the Seers market, which is a PLAYER-run shop with no interface at all.
//     The sequence, in the fleet owner's own description: walk to the market character, say
//     `buying <n> <item>` in public chat, the other side tallies the price and draws the item
//     from its own bank, IT opens the trade, and we complete it. None of internal/actions'
//     shop path applies.
//
//     THE SELLER IS A BOT AND IS ALWAYS THERE (fleet owner, 2026-09-21). Earlier comments in
//     this repo called it "a human" and concluded the path could not run unattended; that was
//     wrong and it is why axePrice/axeBudget were left at their never-buy defaults. A stall
//     that is always present makes this an ordinary autonomous purchase, and the only reason
//     it is not an NPC shop is that it has no shop interface.
//
// Three things about the market path are deliberate and worth stating, because the failure
// modes are not the ones a shop has:
//
//  1. THE COINS MUST ALREADY BE THERE. A shop refuses a purchase you cannot afford and nothing
//     is lost. Here the seller has already drawn the item out of their bank by the time we find
//     out, and a trip that ends in "I cannot pay" wastes their time and ours. So the caller
//     confirms the coins are in the pack BEFORE travelling, and this function refuses outright
//     rather than travelling hopefully.
//
//  2. THE OTHER SIDE IS A PERSON, SO THE WAIT IS UNBOUNDED BY NATURE. A shop always answers;
//     a person may be asleep. Every wait here is therefore explicitly bounded, the asks are
//     counted and spaced, and running out gives up cleanly and returns the account to chopping
//     rather than parking it at the bank forever. Those numbers are named constants below with
//     the reasoning attached, not magic literals.
//
//  3. THE PRICE IS NOT NEGOTIATED HERE, ON PURPOSE. We cannot know what the seller will ask,
//     and a bot that reads a number out of a stranger's chat line and pays it is one typo away
//     from handing over everything it has. So the operator configures the price (the control
//     panel's Seers market panel shows the market's own last-traded figure, which is exactly
//     what it is for), this function offers EXACTLY that, and MaxCoins is a hard ceiling it
//     will never cross whatever anyone says or offers. A confirm screen that does not match
//     precisely is declined.
//
// The receipt rule is the one this codebase already uses everywhere else: a closed trade proves
// nothing (a cancelled confirm screen also closes). The purchase is real only when the axe is
// observed in this account's OWN pack, read through the fixed PlayerPack component — never a
// tab-resolved view, which content blanks during events and voyages and which would read 0 and
// pass a false receipt.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// marketAskEvery spaces the public-chat asks. A person needs time to read the line, check their
// bank and open a trade; re-saying it every few seconds reads as spam and gets ignored, which is
// the opposite of what it is for.
const marketAskEvery = 30 * time.Second

// marketMaxAsks bounds how many times the line is said before giving up on the trip. Three is
// about ninety seconds of standing there, which is long enough for someone who is present and
// short enough that an absent seller does not cost an account its afternoon.
const marketMaxAsks = 3

// marketTradeTimeout bounds the trade itself once it has opened — generous, because the human
// on the other side is doing bank trips between screens.
const marketTradeTimeout = 60 * time.Second

// MarketBuyOptions is one attempt to buy from the player market.
type MarketBuyOptions struct {
	// Stand is where the market character is expected to be.
	Stand tile.Tile
	// Item and Qty are what to ask for.
	Item string
	Qty  int
	// Price is what the operator expects to pay per unit, in coins. Zero refuses the attempt
	// outright: without a price there is nothing safe to offer.
	Price int
	// MaxCoins is the hard ceiling on what may leave this account in this trade, whatever the
	// seller offers or says. Zero refuses.
	MaxCoins int
	// Seller, when set, is the only account name a trade will be completed with. Left empty,
	// any partner is accepted — which is the ordinary case for a market stall, but an operator
	// who knows the name should set it.
	Seller string
	// Me is this account's own name, used to tell OUR invitation from someone else's when the
	// stall addresses one ("Trade me, acct223."). Empty accepts any invitation, which is what
	// a single account buying alone wants.
	Me string
	// Background marks a purchase that can wait -- an axe UPGRADE, bought by an account that is
	// chopping with the axe it has the whole time. It yields the stall's turn to any ordinary
	// buyer (ScarabSwarm's runes, MarketBulkBuy), does not queue behind one, and does not queue at
	// all when another background buyer is already waiting. See marketlease.go.
	Background bool
}

// MarketBuyResult is what happened.
type MarketBuyResult struct {
	// Got is true only once the item is confirmed in this account's own pack.
	Got bool
	// Paid is how many coins actually left, on Got.
	Paid int
	// Reason is why nothing crossed.
	Reason string
	// Stopped is true when the caller's own Delay asked to stop.
	Stopped bool
	// Busy is true when the trip ended only because another account held the stall's turn.
	// That is contention, not a market that will not sell, so a caller should come back
	// SOON rather than serving the long give-up cooldown a real refusal earns.
	Busy bool
	// SoldOut is true when a BACKGROUND trip was not made at all because the stall has already
	// told an account in this fleet it has none of the item (MarketSoldOutUntil). Reason still reads
	// "... said it cannot sell one ...", so woodcutplan.MarketRefusalIsStock treats it as the
	// stock answer it is.
	SoldOut bool
}

func marketRefused(reason string) MarketBuyResult { return MarketBuyResult{Reason: reason} }

// marketBusy is a lost turn. The wording is unchanged because callers and log readers match it.
func marketBusy(seller string) MarketBuyResult {
	res := marketRefused("another account is buying from " + seller + " right now")
	res.Busy = true
	return res
}

// BuyAtMarket asks the Seers player market for an item and completes the trade the seller opens.
func BuyAtMarket(d *Deps, o MarketBuyOptions) MarketBuyResult {
	qty := o.Qty
	if qty < 1 {
		qty = 1
	}
	// THE STALL HAS ALREADY ANSWERED THIS, for the whole fleet. Its "I have no rune axe right now"
	// is about its one stock, not about the account that asked, so asking again from another
	// account only spends the stall's turn to hear it again -- which is what 24 accounts did 40.9
	// times an hour for 40.9 hours (see woodcutplan/marketfleet.go). Checked before anything else:
	// no walk, no turn, no coins drawn.
	//
	// ONLY A BACKGROUND BUYER IS TURNED AWAY BY IT. Every buyer's "none" feeds the note, but a
	// buyer with priority (ScarabSwarm's runes, MarketBulkBuy) still asks: a stall's "none" can be
	// momentary, a chaos buy is not an upgrade that can wait two hours, and a priority buyer that
	// does get one clears the note for everybody.
	if until, sold := MarketSoldOutUntil(o.Seller, o.Item, time.Now()); sold && o.Background {
		res := marketRefused(fmt.Sprintf("%s said it cannot sell one (it told this fleet it has no %s; leaving it alone until %s)",
			o.Seller, strings.ToLower(o.Item), until.Format("15:04")))
		res.SoldOut = true
		return res
	}
	total := o.Price * qty
	if o.Price <= 0 || total <= 0 {
		return marketRefused("no price is configured for " + strings.ToLower(o.Item))
	}
	if o.MaxCoins <= 0 || total > o.MaxCoins {
		return marketRefused("the price is over the configured budget")
	}
	coinsID, ok := packItemIDByName(d, freshstart.Coins)
	if !ok || packCountByName(d, freshstart.Coins) < total {
		// Refused BEFORE walking: see this file's header — the seller draws the item out of
		// their own bank on the strength of our ask, so turning up unable to pay is worse than
		// not turning up.
		return marketRefused("not carrying the coins to pay for it")
	}
	// Resolved only to confirm the name is known to this cache at all. The trade screen itself
	// is matched BY NAME (countOfNamed) -- a name can map to several ids and the stall offers
	// whichever it holds, so an id comparison here missed an axe that was already on the screen.
	if _, ok := objIDByName(d, o.Item); !ok {
		return marketRefused("I cannot identify " + strings.ToLower(o.Item))
	}
	// A BACKGROUND BUYER DOES NOT EVEN WALK OVER when it could not wait for the turn anyway: an
	// ordinary buyer is waiting, or another background buyer already holds the one queue place.
	if o.Seller != "" && o.Background && !turnFor(o.Seller).mayQueue(true) {
		return marketBusy(o.Seller)
	}

	if tile.Chebyshev(currentTile(d), o.Stand) > 4 {
		if !WalkNear(d, o.Stand, 3) {
			return marketRefused("could not reach the market")
		}
	}
	// STAND BESIDE THE STALL, NOT BESIDE A TILE WE GUESSED IT WOULD BE ON.
	//
	// Stand is a fixed configured tile -- the Seers bank -- and the approach above leaves the
	// account up to four tiles the far side of it, so the real gap to the seller was never
	// controlled. Public chat carries only 15 tiles and a trade needs both parties in view, so
	// an uncontrolled gap is a trade waiting to die.
	//
	// It did die, and the instrumentation caught it exactly (2026-09-21): a trade opened with
	// the right partner, our coins registered, and a minute later the screen read "partner
	// \"Seers Market\", my side empty, their side empty" -- with the stall NOT AMONG THE VISIBLE
	// PLAYERS twelve seconds after that. The stall says "Give me a moment" and walks to its own
	// bank to fetch the item; if that walk takes it out of view the trade is lost, and both
	// sides going empty is what that looks like from here.
	//
	// So close the gap that is actually ours to close. The seller's live position is known --
	// PLAYER_INFO carries it -- so use it rather than a constant. Failing to get adjacent is
	// not fatal: the ask still goes out from wherever the account stands, exactly as before.
	if o.Seller != "" {
		if at, ok := sellerTile(d, o.Seller); ok {
			if gap := tile.Chebyshev(currentTile(d), at); gap > sellerStandRadius {
				d.logf("market: %q is %d tiles away — closing to %d", o.Seller, gap, sellerStandRadius)
				if !WalkNear(d, at, sellerStandRadius) {
					d.logf("market: could not get beside %q; asking from here", o.Seller)
				}
			}
		}
	}

	// ONE BUYER AT THIS STALL AT A TIME.
	//
	// A market stall serves one trade at a time, and the whole fleet runs in a single process,
	// so twelve woodcutters arriving together do not queue -- they interleave. Measured live on
	// 2026-09-21, after both earlier market fixes were in: five accounts read "says it has one"
	// within seconds of each other, every one asked to trade, and every one came back "the
	// seller never offered it", because the stall had promised the axe to whichever buyer asked
	// last. Even a correctly addressed buyer (acct195 got "Trade me, acct195.") lost its axe
	// to a sibling's request.
	//
	// So take a turn. An account that cannot get one goes back to chopping and tries later,
	// which is productive, rather than standing in a queue that could run to eighteen minutes
	// at twelve accounts and ninety seconds a trip.
	//
	// The turn has two kinds of buyer now (marketlease.go): an axe upgrade is a background
	// buyer that yields to everyone else, waits only marketTurnWait, and queues at most one deep;
	// an ordinary buyer waits marketTurnWaitPriority, long enough to outlast one background ask.
	var turn *marketTurnGrant
	if o.Seller != "" {
		wait := marketTurnWaitPriority
		if o.Background {
			wait = marketTurnWait
		}
		g, got := acquireMarketTurnAs(d, o.Seller, wait, o.Background)
		if !got {
			return marketBusy(o.Seller)
		}
		turn = g
		defer turn.release()
	}

	trade := actions.NewTrade(d.Session)
	// NOTED OR NOT: the stall hands an axe over as a note, and the receipt below compares
	// against this. See marketReceiptCount.
	held := marketHeld(d, o.Item)
	line := woodcutplan.MarketAsk(o.Item, qty)

	for ask := 0; ask < marketMaxAsks; ask++ {
		// A BACKGROUND HOLDER STEPS ASIDE between asks the moment an ordinary buyer is waiting.
		// Nothing is on a trade screen at this point, so giving the turn up loses only the rest
		// of an upgrade trip, and the buyer behind -- chaos runes for a cohort, or the mule's bulk
		// order after a 106-tile walk -- is the one the stall's time is worth more to.
		if o.Background && turn.priorityWaiting() {
			d.logf("market: another buyer with priority is waiting for %q — giving it the turn", o.Seller)
			res := marketBusy(o.Seller)
			res.Reason = "gave " + o.Seller + "'s turn to a buyer with priority"
			return res
		}
		// WAIT OUT A RANDOM EVENT RATHER THAN THROWING THE TRIP AWAY.
		//
		// This used to abandon the purchase outright the moment the guardian was handling
		// anything. That is far too harsh for what actually fires here: the fleet owner watched
		// a market run die to a Strange plant, which is PASSIVE and which the guardian clears in
		// about a second (live, 2026-09-21 -- "Strange plant — handling" then "cleared" one
		// second later, while a sibling was mid-ask at the same stall).
		//
		// The cost of abandoning is not one trip. The account has already walked to Seers
		// holding 150,000 coins it drew from the mule, and giving up there sends it back to
		// chopping with the money unspent -- which is what made accounts re-ask and drained the
		// mule's hand-out window earlier today. Waiting a few seconds for a plant to clear is
		// strictly better.
		//
		// Still bounded, and still gives up on something genuinely long: a teleport-other or a
		// dialogue event that outlasts the wait ends the trip exactly as before, because
		// standing at a stall inside an unresolved event is its own hazard.
		if randomevents.Busy(d.Session) {
			d.logf("market: a random event is being handled — waiting up to %s before asking", eventClearWait)
			if !waitFor(d, "the random event to clear", eventClearWait, func() bool {
				return !randomevents.Busy(d.Session)
			}) {
				return marketRefused("busy with a random event")
			}
		}
		if err := actions.SayPublic(d.Session, line); err != nil {
			d.logf("market: saying %q: %v", line, err)
		} else {
			d.logf("market: said %q (ask %d of %d)", line, ask+1, marketMaxAsks)
		}
		// THEN OPEN THE TRADE OURSELVES. The stall does not open it -- it says so itself.
		//
		// This loop used to say the line and wait, on the reasoning that the seller opens the
		// trade and we cannot tell which player on a busy tile is the stall. Both halves were
		// wrong, and we now have the stalls' own words for it. Once the fleet could hear public
		// chat (2026-09-21, internal/fleet), four different Seers stalls turned out to be
		// repeating their instructions out loud:
		//
		//     Tradevault: To buy from me: say 'buying 100 iron ore', then trade me and put up coins.
		//     Seers Market: Put items in and i price them as you go. To buy, say what you want first.
		//
		// Say what you want, THEN TRADE THEM, then put up the coins. The wording we already use
		// matches their format exactly; the missing step was the trade request. Waiting for the
		// seller cost twelve live woodcutters their rune axes: each held the 150,000 coins, said
		// "buying 1 rune axe" three times, and gave up with "nobody at the market answered".
		//
		// Naming the stall is now possible for the same reason: PLAYER_INFO carries names, and
		// the sellers are ordinary accounts standing two tiles from the Seers bank. Requesting is
		// safe even if the name resolves to the wrong player, because completeMarketBuy below
		// still refuses anything that is not exactly the item asked for at the expected price,
		// and Seller still pins who a trade may be COMPLETED with. With no Seller configured the
		// old wait-only behaviour is unchanged.
		if o.Seller != "" {
			// WAIT FOR THE STALL TO SAY IT HAS THE ITEM before asking to trade.
			//
			// Requesting immediately does not work, and the first live run proved it: the
			// request lands, the stall accepts, and the trade opens with nothing in it --
			// "that trade did not complete (the seller never offered it)". The stall's own
			// script explains why. It answers the ask with a quote, goes to its bank, and only
			// then invites the trade:
			//
			//     Seers Market: 1 X rune axe = 150,000gp (150,000ea). Give me a moment.
			//     Seers Market: Got your rune axe. Trade me, acct223.
			//
			// So the invitation is a real, observable signal and this waits for it rather than
			// guessing a delay -- which is the standard everywhere else in this codebase.
			// Reading another player's speech only became possible today (internal/fleet now
			// subscribes public chat into Deps.Chat); before that this had to be a timer.
			switch verdict, line := awaitSellerReply(d, o.Seller, o.Item, o.Me, sellerReplyWait); verdict {
			case sellerRefused:
				// "I have no rune axe right now" / "I don't sell 'rune axe'". Believe it: three
				// of the four Seers stalls answer exactly this, and asking twice more just
				// burns ninety seconds and another coin hand-out.
				//
				// And believe it FLEET-WIDE: the stall has one stock, so this answer is the same
				// for every account, and nobody in this process asks for it again for hours.
				until := fleetSoldOut.Mark(o.Seller, o.Item, time.Now())
				d.logf("market: %q has no %s — the whole fleet leaves it alone until %s",
					o.Seller, strings.ToLower(o.Item), until.Format("15:04"))
				return marketRefused(fmt.Sprintf("%s said it cannot sell one (%q)", o.Seller, line))
			case sellerReady:
				// It has one, whatever it told anyone earlier.
				fleetSoldOut.Clear(o.Seller, o.Item)
				d.logf("market: %q says it has one (%q)", o.Seller, line)
			default:
				// Said nothing we recognised. Ask anyway rather than give up: an unspeaking
				// stall still trades, and this is no worse than the behaviour before the wait.
				d.logf("market: %q said nothing about it within %s — asking to trade anyway", o.Seller, sellerReplyWait)
			}
			if slot, found := resolvePlayerSlot(d, o.Seller); found {
				if err := trade.Request(slot); err != nil {
					d.logf("market: asking %q to trade: %v", o.Seller, err)
				} else {
					d.logf("market: asked %q to trade (attempt %d of %d)", o.Seller, ask+1, marketMaxAsks)
				}
			} else {
				d.logf("market: %q is not among the visible players at the stand", o.Seller)
			}
		}
		// Then wait for the offer screen -- ours if the request landed, otherwise the seller's.
		// The original reasoning, kept because it still applies to an UNNAMED stall:
		// A DECLINE IS AN ANSWER, AND IT COMES WITH THE RETRY TIME IN IT.
		//
		// The stall rate-limits, and until the fleet could hear it this looked like silence:
		//
		//     Seers Market: Trade declined. Ask again in 15s.
		//
		// The request had already been refused, and this then blocked the full marketAskEvery
		// waiting for an offer that was never coming, reporting "the seller never offered it".
		// Honouring the stated cooldown turns a wasted thirty-second block into a short wait
		// and a retry that can actually succeed. The number is the seller's own, not a guess.
		if pause, declined := sellerDeclinedFor(d, o.Seller, declineWatch); declined {
			d.logf("market: %q declined and asked for %s — waiting", o.Seller, pause)
			if !d.Delay(pause + declineMargin) {
				return MarketBuyResult{Stopped: true}
			}
			continue
		}
		if waitFor(d, "the market to open a trade", marketAskEvery, trade.Active) {
			res := completeMarketBuy(d, trade, o, qty, total, coinsID, held)
			if res.Got || res.Stopped {
				return res
			}
			d.logf("market: that trade did not complete (%s)", res.Reason)
			continue
		}
		if !d.Delay(500 * time.Millisecond) {
			return MarketBuyResult{Stopped: true}
		}
	}
	return marketRefused("nobody at the market answered")
}

// completeMarketBuy drives one trade the seller opened: check they are offering exactly what was
// asked for, put exactly the agreed coins up, and refuse anything that does not match.
func completeMarketBuy(d *Deps, trade *actions.Trade, o MarketBuyOptions, qty, total int, coinsID int32, heldBefore int) MarketBuyResult {
	if o.Seller != "" {
		// A wrong partner here means paying a stranger, which is strictly worse than a wasted
		// round, so the name is waited for and then insisted on. Trade.Partner reads a
		// component nothing clears on close, so it can briefly still name a previous partner.
		waitFor(d, "the trade partner name", 3*time.Second, func() bool {
			return strings.EqualFold(strings.TrimSpace(trade.Partner()), strings.TrimSpace(o.Seller))
		})
		if !strings.EqualFold(strings.TrimSpace(trade.Partner()), strings.TrimSpace(o.Seller)) {
			_ = trade.Decline(3 * time.Second)
			return marketRefused("the trade screen named " + trade.Partner() + ", not the seller")
		}
	}
	if !waitFor(d, "the market's offer screen", 10*time.Second, trade.OnOfferScreen) {
		_ = trade.Decline(3 * time.Second)
		return marketRefused("the offer screen never opened")
	}
	// THE COINS GO UP FIRST. That is the stall's protocol, and waiting for the item instead
	// deadlocks against it.
	//
	// This used to wait for the seller to offer the axe "before paying anything", which sounds
	// like the careful order and is the wrong one for this counterparty. The stalls say so
	// plainly, and once the fleet could hear them (2026-09-21) they were saying it constantly:
	//
	//     Tradevault: To buy from me: say 'buying 100 iron ore', then trade me and put up coins.
	//     Seers Market: Put items in and i price them as you go.
	//
	// So both sides waited for the other and nothing moved. Live, with an exclusive turn at the
	// stall and the axe confirmed in its hand, one account still failed three times in a row
	// with "the seller never offered it" -- which measured a deadlock, not a missing axe.
	//
	// Offering first is NOT paying, which is the whole reason this is safe. Items in a trade
	// window move only when BOTH sides accept, and every guard below still stands between the
	// offer and the accept: the partner must be the named seller, the item must be on their
	// side at the agreed quantity, and our own side must show no more than the agreed total
	// (and never more than MaxCoins). A seller who takes the coins up and offers nothing gets
	// a decline and keeps nothing.
	coinsBefore := packCountByName(d, freshstart.Coins)
	if err := trade.Offer(coinsID, int32(total), 3*time.Second); err != nil {
		_ = trade.Decline(3 * time.Second)
		return marketRefused("could not put the coins up")
	}
	if !waitFor(d, "my coins to register on the screen", 10*time.Second, func() bool {
		return countOfID(trade.MyOffer(), coinsID) >= int32(total)
	}) {
		_ = trade.Decline(3 * time.Second)
		return marketRefused("my coins never registered")
	}
	// Now the seller's half of the bargain. Still never accepted without it.
	if !waitFor(d, "the seller to offer the item", marketTradeTimeout, func() bool {
		return countOfNamed(d, trade.TheirOffer(), o.Item) >= qty
	}) {
		// SAY WHAT WAS ACTUALLY ON THE SCREEN, because "never offered it" is a TIMEOUT and
		// not a diagnosis, and it has been mistaken for one repeatedly (2026-09-21: five
		// market fixes shipped against this one message, four of them real bugs, none of them
		// this). The three readings below separate cases the message cannot:
		//   - our side empty        -> our offer never landed as an offer
		//   - their side non-empty  -> they offered something, just not what was asked for
		//   - both empty, partner ok-> the stall is refusing for a reason it has not spoken
		// The panel cannot show any of this: it tracks neither 3415 nor 3416.
		d.logf("market: no item after %s — partner %q, my side %s, their side %s",
			marketTradeTimeout, trade.Partner(),
			describeOffer(d, trade.MyOffer()), describeOffer(d, trade.TheirOffer()))
		_ = trade.Decline(3 * time.Second)
		return marketRefused("the seller never offered it")
	}
	// Never let more than the agreed amount sit on the screen, whatever happened.
	if got := countOfID(trade.MyOffer(), coinsID); int(got) > total || int(got) > o.MaxCoins {
		d.logf("market: my own offer shows %d coins, over the agreed %d — declining", got, total)
		_ = trade.Decline(3 * time.Second)
		return marketRefused("more coins went up than agreed")
	}
	if err := trade.Accept(); err != nil {
		_ = trade.Decline(3 * time.Second)
		return marketRefused("could not accept the offer screen")
	}
	if !waitFor(d, "the market's confirm screen", marketTradeTimeout, trade.OnConfirmScreen) {
		_ = trade.Decline(3 * time.Second)
		return marketRefused("the confirm screen never came")
	}
	// The confirm screen is the last point anything can be changed, so it is checked exactly:
	// they give the item and only the item, we give the coins and only the coins.
	mine, theirs := trade.ConfirmOffers()
	switch {
	case countOfNamed(d, theirs, o.Item) != qty || len(theirs) != 1:
		d.logf("market: the confirm screen does not offer exactly %d %s — declining", qty, o.Item)
		_ = trade.Decline(3 * time.Second)
		return marketRefused("the confirm screen did not match what was asked for")
	case countOfID(mine, coinsID) != int32(total) || len(mine) != 1:
		d.logf("market: the confirm screen wants something other than exactly %d coins — declining", total)
		_ = trade.Decline(3 * time.Second)
		return marketRefused("the confirm screen wanted a different payment")
	}
	if err := trade.Accept(); err != nil {
		return marketRefused("could not accept the confirm screen")
	}
	if !waitFor(d, "the market trade to close", 20*time.Second, func() bool { return !trade.Active() }) {
		return marketRefused("the trade never closed")
	}
	// The receipt. A cancelled confirm screen closes too, so only the item genuinely arriving
	// in this account's own pack counts. marketHeld counts a note as well, which is what the
	// stall hands over, so this is the arrival check and NOT proof the axe is usable yet.
	//
	// This line used packCountByName until 2026-09-25, and packCountByName stopped counting
	// notes on 2026-09-23 -- so every noted axe that arrived read as "closed without the item
	// arriving", and the trip asked again and paid again. See marketReceiptCount.
	if !waitFor(d, "the axe to reach my pack", 8*time.Second, func() bool {
		return marketHeld(d, o.Item) > heldBefore
	}) {
		return marketRefused("the trade closed without the item arriving")
	}
	// A noted axe cannot chop. Bank it to turn it back into one, which costs a few tiles here
	// because the stall stands beside this account's own bank.
	if !unnoteAxe(d, o.Item) {
		d.logf("market: bought a noted %s and could not un-note it yet — the next bank trip will",
			strings.ToLower(o.Item))
	}
	paid := coinsBefore - packCountByName(d, freshstart.Coins)
	fleetSoldOut.Clear(o.Seller, o.Item)
	d.logf("market: bought %d %s for %d coins", qty, strings.ToLower(o.Item), paid)
	return MarketBuyResult{Got: true, Paid: paid}
}

// ---- Bob's Brilliant Axes ----

// BuyFromBob buys one axe from Bob's shop in Lumbridge — an ordinary shop interface, unlike the
// market. Only ever worth calling for an axe woodcutplan.BobSells reports, since nothing above
// steel is stocked.
func BuyFromBob(d *Deps, stand tile.Tile, axe string) bool {
	if !woodcutplan.BobSells(axe) {
		d.logf("Bob's Axes does not stock a %s (bronze, iron and steel only)", strings.ToLower(axe))
		return false
	}
	here := currentTile(d)
	if tile.Chebyshev(here, stand) > 3 {
		// TravelTo, not WalkNear: Bob's is in LUMBRIDGE and this script lives in Seers, so
		// this is a cross-region leg every time. Measured 2026-09-20 with the old plain
		// WalkNear: the single A* across the collision pack answered "unreachable" and the
		// door recovery then thrashed until the 120s budget ran out, every run, so no
		// account ever reached the shop. See Deps.Walker.
		if !TravelTo(d, stand, 2) {
			d.logf("could not reach Bob's Axes")
			return false
		}
	}
	shop := openShop(d, []string{"Bob"}, stand)
	if shop == nil {
		d.logf("could not open Bob's Axes")
		return false
	}
	defer shop.Close()
	// shop.Stock(), NOT shop.Pack(). A shop screen has two sides and Pack() is the PLAYER's;
	// StoreTrip reads it because selling looks for the item in our own inventory, and this
	// function was copied from it. Measured 2026-09-20 (cmd/woodkilltest -stage repair, part
	// C): the account stood at the open shop with 4,897 coins and was told "Bob has no steel
	// axe in stock right now", because the only place it looked was its own pack. That was
	// the SECOND bug in this path — the first was that nothing ever routed here to find out.
	id, found := itemInStock(d, shop.Stock(), axe)
	if !found {
		d.logf("Bob has no %s in stock right now (his stock: %s)", strings.ToLower(axe), stockNames(d, shop.Stock()))
		return false
	}
	before := packCountByName(d, axe)
	if err := shop.Buy(id, 1); err != nil {
		d.logf("buying a %s from Bob: %v", strings.ToLower(axe), err)
		return false
	}
	// Same receipt rule as the market: the buy is real only when the pack shows it.
	return waitFor(d, axe+" in my pack after buying", 5*time.Second, func() bool {
		return packCountByName(d, axe) > before
	})
}

// stockNames lists a shop's stock by name, for the log line that says why a buy was refused.
// Without it "no steel axe in stock" cannot be told apart from "we looked in the wrong place",
// which is exactly the confusion that hid the Pack()/Stock() bug above.
func stockNames(d *Deps, items []gamestate.SlotItem) string {
	var out []string
	for _, it := range items {
		if obj, ok := d.Config.Obj(int(it.ID)); ok {
			out = append(out, obj.Name)
		}
	}
	if len(out) == 0 {
		return "nothing visible"
	}
	return strings.Join(out, ", ")
}

// ---- small shared reads ----

func countOfID(items []gamestate.SlotItem, id int32) int32 {
	var n int32
	for _, it := range items {
		if it.ID != id {
			continue
		}
		c := it.Count
		if c < 1 {
			c = 1
		}
		n += c
	}
	return n
}

// packItemIDByName is the id of a non-noted pack item with that name.
func packItemIDByName(d *Deps, name string) (int32, bool) {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0, false
	}
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && !obj.Noted() && strings.EqualFold(strings.TrimSpace(obj.Name), name) {
			return it.ID, true
		}
	}
	return 0, false
}

// objIDByName resolves an item name to its ordinary, un-noted object id. A name can resolve to
// several ids (the item and its bank note), and a note is not the thing — see
// mulerunner.nonNotedID's own doc comment for the live incident behind that rule.
func objIDByName(d *Deps, name string) (int32, bool) {
	if d.Config == nil {
		return 0, false
	}
	for _, id := range d.Config.ObjIDs(name) {
		if obj, ok := d.Config.Obj(id); ok && !obj.Noted() {
			return int32(id), true
		}
	}
	return 0, false
}

// resolvePlayerSlot turns a player name into a live PLAYER_INFO index, so a trade can be opened
// with a specific account rather than whoever happens to answer.
//
// Deliberately a copy of mulerunner.ResolvePlayerSlot's few lines rather than an import:
// mulerunner pulls in the whole mule receiver -- bank, cooking, the wall bus -- and a woodcutter
// asking a market stall to trade needs none of it. Matching ignores case and surrounding space
// because a stall's displayed name is whatever its owner typed.
func resolvePlayerSlot(d *Deps, name string) (int32, bool) {
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return 0, false
	}
	for _, p := range d.Session.State().Players() {
		if strings.ToLower(strings.TrimSpace(p.Name)) == target {
			return int32(p.Index), true
		}
	}
	return 0, false
}

// sellerReplyWait bounds how long to wait for the stall to answer an ask before trading anyway.
//
// It has to cover the stall walking to its own bank and back, which is what "Give me a moment"
// means. Twelve seconds is comfortably more than that was observed to take and still well inside
// marketAskEvery, so a silent stall does not cost the ask its slot.
const sellerReplyWait = 12 * time.Second

// sellerVerdict is what a stall's public reply to an ask means.
type sellerVerdict int

const (
	sellerSilent sellerVerdict = iota
	sellerReady
	sellerRefused
)

// awaitSellerReply watches public chat for the named stall's answer to an ask, returning the
// verdict and the line it was read from.
//
// Matching is deliberately loose. Public chat comes back SENTENCE-CASED with a possible trailing
// space (measured by cmd/publicchattest), the stalls phrase themselves inconsistently, and the
// account's own name is embedded in the invitation ("Trade me, acct223."). So this compares
// lower-cased and by substring, never with equality.
func awaitSellerReply(d *Deps, seller, item, me string, within time.Duration) (sellerVerdict, string) {
	if d.Chat == nil {
		return sellerSilent, ""
	}
	prefix := strings.ToLower(strings.TrimSpace(seller)) + ":"
	wantItem := strings.ToLower(strings.TrimSpace(item))
	cursor := d.Chat.Total()
	deadline := time.Now().Add(within)
	for {
		var lines []string
		lines, cursor = d.Chat.ReadAfter(cursor, time.Time{})
		for _, raw := range lines {
			line := strings.ToLower(strings.TrimSpace(raw))
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			// A refusal names the item and says it has none of it.
			if strings.Contains(line, wantItem) &&
				(strings.Contains(line, "no ") || strings.Contains(line, "don't sell") || strings.Contains(line, "dont sell")) {
				return sellerRefused, strings.TrimSpace(raw)
			}
			if i := strings.Index(line, "trade me"); i >= 0 {
				// AN INVITATION ADDRESSED TO SOMEONE ELSE IS NOT OURS.
				//
				// The stall uses both forms, and only one of them names a buyer:
				//
				//     Seers Market: Got your rune axe. Trade me, acct223.
				//     Seers Market: 1 X rune axe = 150,000gp (150,000ea). Trade me.
				//
				// Accepting any "trade me" made twelve live woodcutters read each OTHER'S
				// invitations and pile onto a stall that trades one person at a time, so the
				// trade opened and the seller offered nothing -- the axe was promised to a
				// different account. An unaddressed invitation is still taken, because a single
				// account buying alone would otherwise never proceed.
				who := strings.Trim(line[i+len("trade me"):], " ,.!")
				if who == "" || me == "" || who == strings.ToLower(strings.TrimSpace(me)) {
					return sellerReady, strings.TrimSpace(raw)
				}
				// Addressed to another buyer: keep waiting for ours.
			}
		}
		if !time.Now().Before(deadline) {
			return sellerSilent, ""
		}
		if !d.Delay(500 * time.Millisecond) {
			return sellerSilent, ""
		}
	}
}

// declineWatch is how long to watch for a decline before assuming the request is still pending.
// Short: the stall answers within a tick or two, and anything longer just delays the real wait.
const declineWatch = 3 * time.Second

// declineMargin is added to the stall's stated cooldown, so a retry never lands a tick early and
// earn another decline.
const declineMargin = 2 * time.Second

// declineCooldown is the fallback when a decline names no time.
const declineCooldown = 15 * time.Second

// sellerDeclinedFor watches for the stall refusing the trade, returning how long it asked us to
// wait. "Trade declined. Ask again in 15s." is the live wording; the seconds are parsed out of
// it so the stall sets its own pace rather than this file guessing one.
func sellerDeclinedFor(d *Deps, seller string, within time.Duration) (time.Duration, bool) {
	if d.Chat == nil {
		return 0, false
	}
	prefix := strings.ToLower(strings.TrimSpace(seller)) + ":"
	cursor := d.Chat.Total()
	deadline := time.Now().Add(within)
	for {
		var lines []string
		lines, cursor = d.Chat.ReadAfter(cursor, time.Time{})
		for _, raw := range lines {
			line := strings.ToLower(strings.TrimSpace(raw))
			if !strings.HasPrefix(line, prefix) || !strings.Contains(line, "declin") {
				continue
			}
			return parseAskAgainIn(line), true
		}
		if !time.Now().Before(deadline) {
			return 0, false
		}
		if d.Delay == nil || !d.Delay(300*time.Millisecond) {
			return 0, false
		}
	}
}

// parseAskAgainIn pulls the seconds out of "...ask again in 15s", falling back to
// declineCooldown when the stall names no time.
func parseAskAgainIn(line string) time.Duration {
	i := strings.Index(line, "again in ")
	if i < 0 {
		return declineCooldown
	}
	rest := line[i+len("again in "):]
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits == 0 {
		return declineCooldown
	}
	n, err := strconv.Atoi(rest[:digits])
	if err != nil || n <= 0 {
		return declineCooldown
	}
	if d := time.Duration(n) * time.Second; d <= 2*time.Minute {
		return d
	}
	return declineCooldown
}

// describeOffer renders one side of a trade screen for a diagnostic line, resolving ids to
// names so a reader does not have to. "empty" is stated explicitly rather than printed as a
// blank, because an empty side and an unread side are different findings.
func describeOffer(d *Deps, items []gamestate.SlotItem) string {
	parts := make([]string, 0, 4)
	for _, it := range items {
		if it.ID <= 0 || it.Count <= 0 {
			continue
		}
		name := "id " + strconv.Itoa(int(it.ID))
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Name != "" {
			name = obj.Name
		}
		parts = append(parts, fmt.Sprintf("%s x%d", name, it.Count))
		if len(parts) == 4 {
			parts = append(parts, "...")
			break
		}
	}
	if len(parts) == 0 {
		return "empty"
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// sellerStandRadius is how close to stand to the stall before asking.
//
// Two tiles, not fifteen. Public chat reaches 15 and a trade needs both parties in view, so the
// margin here is not about hearing the stall -- it is about surviving the stall's own walk to
// its bank without falling out of range of each other.
const sellerStandRadius = 2

// sellerTile is the stall's live position, or false when it is not currently visible.
func sellerTile(d *Deps, seller string) (tile.Tile, bool) {
	target := strings.ToLower(strings.TrimSpace(seller))
	for _, p := range d.Session.State().Players() {
		if strings.ToLower(strings.TrimSpace(p.Name)) == target {
			return tile.Tile{X: p.X, Z: p.Z, Level: p.Level}, true
		}
	}
	return tile.Tile{}, false
}

// countOfNamed counts items on one side of a trade screen BY NAME rather than by a single
// resolved id.
//
// Comparing one id was wrong and it cost the whole purchase. objIDByName returns the FIRST
// non-noted id carrying a name, but a name can map to several ids, and the stall offers whichever
// one it happens to hold. So a screen reading exactly "their side [Rune axe x1]" failed a test
// asking "is id N present", and the account declined an axe it had already paid for and was
// looking straight at (live, 2026-09-21):
//
//	no item after 1m0s — partner "Seers Market", my side [Coins x150000], their side [Rune axe x1]
//
// This is the same lesson CLAUDE.md already records for OpLocU: resolve by NAME through the
// config, never by an id assumed to be the only one.
//
// NOTED ITEMS COUNT, and excluding them was my own second mistake on the same line. I skipped
// them on the reasoning that "a bank note is not a usable axe", which is true in general and
// wrong here: the Seers stall hands the axe over AS A NOTE, so the exclusion made the account
// stare at an axe it had paid for and decline it. The owner spotted it from the diagnostic,
// which printed the item while the test refused it -- describeOffer resolves the name, this
// skipped the note, and the disagreement between those two lines is the whole bug:
//
//	no item after 1m0s — partner "Seers Market", my side [Coins x150000], their side [Rune axe x1]
//
// Taking the note is safe because of WHERE this happens: the stall stands two tiles from the
// Seers bank, which is this account's own bank, and a note un-notes the moment it is deposited.
// unnoteAxe below does exactly that before the axe is reported as bought.
func countOfNamed(d *Deps, items []gamestate.SlotItem, name string) int {
	if d.Config == nil {
		return 0
	}
	want := strings.ToLower(strings.TrimSpace(name))
	total := 0
	for _, it := range items {
		if it.ID <= 0 || it.Count <= 0 {
			continue
		}
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok {
			continue
		}
		if strings.ToLower(strings.TrimSpace(obj.Name)) == want {
			total += int(it.Count)
		}
	}
	return total
}

// eventClearWait is how long a market trip will wait for the guardian to finish a random event
// before giving the trip up.
//
// Generous on purpose. The account is already standing at the stall with the coins drawn, so the
// walk is spent either way; the only thing waiting costs is time, while abandoning costs the
// whole trip and leaves the money unspent. Passive events clear in a second or two, and the
// bound is what stops a long dialogue event parking an account at a market stall indefinitely.
const eventClearWait = 45 * time.Second

// unnoteAxe turns a bank-noted axe back into a real one by depositing it and drawing it out.
//
// A bank always stores the un-noted form, which is the same property mulerunner relies on when
// it hands crystal keys over as notes. Returns true when an UNNOTED one is in the pack -- which
// is also true immediately when the axe was never noted, so callers can run it unconditionally.
func unnoteAxe(d *Deps, item string) bool {
	if unnotedCount(d, item) > 0 {
		return true
	}
	bank, opened := OpenSeersBank(d)
	if !opened {
		return false
	}
	defer func() { _ = bank.Close() }()
	// Deposit whatever the pack is holding under that name -- it is the NOTE we just received,
	// and the bank stores the un-noted form whatever goes in.
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return false
	}
	deposited := false
	want := strings.ToLower(strings.TrimSpace(item))
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || strings.ToLower(strings.TrimSpace(obj.Name)) != want {
			continue
		}
		if err := bank.DepositAll(it.ID); err != nil {
			d.logf("market: depositing the noted %s: %v", strings.ToLower(item), err)
			return false
		}
		deposited = true
	}
	if !deposited {
		return false
	}
	// notedCountByName, not packCountByName: this wait is about the NOTE, and packCountByName
	// deliberately does not see notes at all (see its own comment), so asking it here would
	// answer "gone" before the deposit had landed.
	if !waitFor(d, "the noted axe to reach the bank", 5*time.Second, func() bool {
		return notedCountByName(d, item) == 0
	}) {
		return false
	}
	WithdrawAllByName(d, bank, item)
	return waitFor(d, "the un-noted axe to come back", 5*time.Second, func() bool {
		return unnotedCount(d, item) > 0
	})
}

// unnotedCount is how many USABLE (un-noted) of an item the pack holds.
func unnotedCount(d *Deps, item string) int {
	if d.Config == nil {
		return 0
	}
	want := strings.ToLower(strings.TrimSpace(item))
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	total := 0
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Noted() {
			continue
		}
		if strings.ToLower(strings.TrimSpace(obj.Name)) == want {
			total += int(it.Count)
		}
	}
	return total
}
