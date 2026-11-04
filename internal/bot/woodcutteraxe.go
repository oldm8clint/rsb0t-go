package bot

// ProgressiveWoodcutter's axe upgrade: ask the mule for the coins, then buy an axe with them.
//
// Kept in its own file, one method per step and all of its state on woodcutterBot's existing
// fields, deliberately — the same additive shape internal/bot/fisherhandout.go uses, so this
// does not scatter edits through a file other sessions are working in. See COLLAB.md.
//
// WHY THIS REPLACES THE OLD orderStep. The previous PhaseOrder published a
// mulebus.OrderMessage for a rune axe and then logged a warning that nothing would ever answer
// it, because MuleReceiver did not read KindOrder. That is no longer true: internal/bot/mule.go
// now has a `case mulebus.KindOrder` that runs mulerunner.Handout, and the donor half
// (donorrunner.Await) is a complete, receipt-verified ask-and-receive. So the ask is real now.
//
// But asking the mule for an AXE is still the wrong request, for a measured reason: the mule
// hands out what is in its own bank, and a fleet mule holds logs and casket loot, not rune axes
// — and nothing on this server can restock one, because NO NPC SHOP SELLS AN AXE ABOVE STEEL
// (see woodcutplan/axe.go for the content evidence). What the mule genuinely does have is
// COINS, which is exactly what users asked for: request coins, then buy the axe.
//
// One number makes the difference between the two routes stark, and an operator should see it
// before configuring anything. Measured from the live market page on 2026-09-20: a rune axe
// last sold for 150,000 coins and a mithril axe for 600. orderplan's own defaults hand out
// CoinsPerOrder = 500 per request and cap a donor at CoinsPerWindow = 25,000 per 30 minutes, so
// a rune axe is roughly six full windows — about three hours of asking — while a mithril axe is
// two requests. A fleet that wants rune axes needs the mule's catalogue raised deliberately;
// a fleet that just wants a big upgrade cheaply should point AxeWanted at a mithril axe. This
// file does not make that choice, but it logs the shortfall so the choice is visible.

import (
	"math/rand"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// axeRetry is how long to leave the whole upgrade alone after an attempt that produced nothing.
// Longer than fisherhandout's askRetry because this is an OPTIMISATION, not a rescue: an
// account without the axe it wants is still chopping and still earning, so a failed attempt
// costs nothing and there is no reason to keep pestering either the mule or a human seller.
const axeRetry = 15 * time.Minute

// casketFareCeiling is the largest need worth trying to pay out of banked caskets: a ship
// fare and its like, not an axe. See askMuleForCoins.
const casketFareCeiling = 1000

// marketBusyWait is how long to stay away after losing the stall's turn, and counts the loss.
//
// Contention is not a refusal: the stall will sell, it is simply serving somebody else. The first
// wait is still roughly one trip (woodcutplan.MarketBusyFirst, the old fixed axeBusyRetry), so an
// account that was merely second in line gets its turn soon; consecutive losses double it, and
// every wait is jittered so a cohort that lost together does not come back together.
func (b *woodcutterBot) marketBusyWait() time.Duration {
	b.marketLosses++
	return woodcutplan.MarketBusyBackoff(b.marketLosses, rand.Float64())
}

// marketSkip is the ladder's "do not ask the stall for this" test: what the stall told THIS
// account (marketNo, ten minutes) or told ANY account in the fleet (woodcutrunner's shared note,
// hours). The stall has one stock, so its "I have no rune axe right now" to acct164 is just as
// true for acct138 -- and until 2026-09-25 each of 24 accounts had to hear it for itself, every
// ten minutes, which is what kept the Seers Market's turn busy 85% of the time.
func (b *woodcutterBot) marketSkip(now time.Time) func(axe string) bool {
	mine := b.marketNo.Skip(now)
	seller := b.settings.MarketSeller
	return func(axe string) bool {
		if mine(axe) {
			return true
		}
		_, sold := woodcutrunner.MarketSoldOutUntil(seller, axe, now)
		return sold
	}
}

// soldOutTiers lists the tiers, from the wanted one down, that the fleet's note says the stall
// has none of -- for the "keeping my axe" line, so it names what is missing.
func (b *woodcutterBot) soldOutTiers(now time.Time) []string {
	all := woodcutplan.AxeTargets(b.settings.AxeWanted, b.settings.PriceOfAxe, nil)
	out := make([]string, 0, len(all))
	for _, a := range all {
		if _, sold := woodcutrunner.MarketSoldOutUntil(b.settings.MarketSeller, a, now); sold {
			out = append(out, a)
		}
	}
	return out
}

// What an empty purchase ladder means for this account, from emptyLadder.
const (
	ladderNothing = iota // nothing configured or nothing the market is known to lack
	ladderKeepAxe        // the market is out of every tier worth buying; chop with the axe in hand
	ladderRescue         // the market is out AND the account has no axe at all: Bob's
)

// emptyLadder decides what an empty ladder means: whether the fleet's sold-out note is WHY it is
// empty, and whether this account has anything to chop with meanwhile. gone is the tiers the note
// covers, best first.
func (b *woodcutterBot) emptyLadder(held []string, now time.Time) (action int, gone []string) {
	gone = b.soldOutTiers(now)
	switch {
	case len(gone) == 0:
		return ladderNothing, nil
	case len(woodcutplan.AxelessRescue(held)) > 0:
		return ladderRescue, gone
	default:
		return ladderKeepAxe, gone
	}
}

// rescueFromBob buys the best axe Bob has for an account holding NONE, reporting whether one
// arrived. want is the axe that could not be had, for the log line.
func (b *woodcutterBot) rescueFromBob(ctx *Context, d *woodcutrunner.Deps, want string, rescue []string) bool {
	for _, a := range rescue {
		if woodcutrunner.BuyFromBob(d, b.settings.BobStand, a) {
			b.axesBought++
			ctx.logf("woodcutter: no %s to be had and this account has NO axe at all — bought a %s from Bob's Axes so it can chop now",
				strings.ToLower(want), strings.ToLower(a))
			b.axeNext = time.Now().Add(axeRetry)
			return true
		}
	}
	ctx.logf("woodcutter: this account has NO axe at all, the market has no %s, and Bob's Axes could not supply %v either — retrying in %s",
		strings.ToLower(want), rescue, axeRetry)
	return false
}

// axeTierRetry is how long to wait after a stall refusal that leaves a CHEAPER tier still worth
// trying.
//
// A different question deserves a different wait. axeRetry's fifteen minutes is sized for "this
// upgrade cannot be had", and the account is chopping either way, so it costs nothing to sit
// out — but "the stall has no rune axe, try adamant" is a fresh attempt with a real chance, and
// making it wait fifteen minutes per tier means three quarters of an hour to walk a three-rung
// ladder. A minute is about one trip, matching axeBusyRetry's own reasoning.
const axeTierRetry = 60 * time.Second

// axeStep runs one pass of the upgrade: decide what to buy, get the coins if they are short,
// then buy it. Returns without doing anything at all when buying is switched off, which is the
// default.
func (b *woodcutterBot) axeStep(ctx *Context, d *woodcutrunner.Deps) {
	if !b.settings.BuysAxes() {
		// Nothing configured to buy with. Say so once in a while rather than silently doing
		// nothing forever, because "no axe and no mule" is otherwise indistinguishable from a
		// misconfiguration.
		b.warn(ctx, "axe-off", 300*time.Second,
			"no axe purchase is configured (axePrice and axeBudget are both needed) — put an axe in this account's own Seers bank, or set them")
		ctx.Delay(3 * time.Second)
		return
	}
	if time.Now().Before(b.axeNext) {
		ctx.Delay(3 * time.Second)
		return
	}

	held := woodcutrunner.HeldAxes(d)
	coins := woodcutrunner.CoinsHeld(d)
	// EVERY tier it knows a price for, not just the configured one. Anything unpriced is
	// genuinely unknown and still must never read as affordable -- see Settings.PriceOfAxe.
	//
	// This is what lets the account fall back when the market has no rune axe: adamant at
	// 3,150 and mithril at 600 are real, measured prices, and any axe chops from Woodcutting 1.
	//
	// THAT SENTENCE WAS A LIE UNTIL 2026-09-22, and it is worth recording rather than quietly
	// fixing, because it is what hid the defect the fleet owner reported. DecideAxeBuy called
	// priceOf for the ONE configured `want` and nothing else -- there was no ladder and no
	// fallback. So with AxeWanted at a rune axe, and the Seers market's rune-axe stock measured
	// at ZERO that morning, `acct134` said "buying 1 rune axe" at a stall that had none, gave
	// up for axeRetry, and repeated -- while carrying 150,000 coins (exactly the rune price it
	// had already been funded for) with a mithril axe nine deep at 600. AxeTargets is the
	// ladder the comment described; marketNo is why a refusal moves the account down it instead
	// of asking the same question again.
	priceOf := b.settings.PriceOfAxe
	targets := woodcutplan.AxeTargets(b.settings.AxeWanted, priceOf, b.marketSkip(time.Now()))
	buy := woodcutplan.DecideAxeBuyAmong(held, b.bankAxes, targets, coins, priceOf)
	if buy.Want == "" {
		switch action, gone := b.emptyLadder(held, time.Now()); action {
		case ladderKeepAxe:
			// KEEP THE AXE IN HAND when the stall has none of any tier worth buying. A better
			// axe is a speed upgrade, not a blocker: the account has been chopping with this one
			// all along. Said once in a while so an operator can see why nothing is bought.
			b.warn(ctx, "axe-sold-out", 30*time.Minute,
				"%q has told this fleet it has no %s — keeping my %s and not asking again until it restocks (the fleet re-checks in a few hours)",
				b.settings.MarketSeller, strings.ToLower(strings.Join(gone, ", ")), strings.ToLower(woodcutplan.BestAxe(held)))
		case ladderRescue:
			// NO AXE AT ALL and the fleet already knows the market has none: straight to Bob's,
			// which a refused market trip used to reach at the bottom of this function. Without
			// this an axeless account that had never asked would skip the market (the fleet
			// note) AND the rescue that followed the market's refusal.
			if b.rescueFromBob(ctx, d, gone[0], woodcutplan.AxelessRescue(held)) {
				return
			}
		}
		b.axeNext = time.Now().Add(axeRetry)
		ctx.Delay(2 * time.Second)
		return
	}
	if buy.From == woodcutplan.AxeFromBank {
		// The ordinary bank step already withdraws the best banked axe; nothing to buy.
		b.axeNext = time.Now().Add(axeRetry)
		return
	}

	// The coins have to be in the pack BEFORE travelling to a player market: the seller draws
	// the item out of their own bank on the strength of the ask, so arriving unable to pay
	// wastes the trip and their time. See woodcutrunner/market.go's own header.
	if buy.Need > 0 {
		if !b.askMuleForCoins(ctx, d, buy.Want, buy.Need) {
			b.axeNext = time.Now().Add(axeRetry)
			ctx.Delay(2 * time.Second)
			return
		}
		coins = woodcutrunner.CoinsHeld(d)
		want, _ := b.settings.PriceOfAxe(buy.Want)
		if coins < want {
			ctx.logf("woodcutter: still %d coins short of a %s after asking %q — trying again in %s",
				want-coins, strings.ToLower(buy.Want), b.settings.Mule, axeRetry)
			b.axeNext = time.Now().Add(axeRetry)
			return
		}
	}

	res := woodcutrunner.BuyAtMarket(d, woodcutrunner.MarketBuyOptions{
		Stand: b.settings.MarketStand, Item: buy.Want, Qty: 1,
		// The price and the ceiling both follow the axe being bought, never the top tier's.
		// axeBudget alone is 165,000 -- sized for a rune axe, and it used to bound a 600-coin
		// mithril purchase too.
		Price: mustPriceAxe(b, buy.Want), MaxCoins: b.settings.MaxCoinsForAxe(buy.Want), Seller: b.settings.MarketSeller,
		Me: ctx.Username,
		// An upgrade can wait: ScarabSwarm's runes and MarketBulkBuy go first at the stall, and
		// this does not queue behind them (woodcutrunner/marketlease.go).
		Background: true,
	})
	if !res.Busy {
		b.marketLosses = 0
	}
	switch {
	case res.Stopped:
		return
	case res.Got:
		b.axesBought++
		b.axeSpent += res.Paid
		// The stall plainly has them, whatever it said last time.
		b.marketNo.Clear(buy.Want)
		ctx.logf("woodcutter: bought a %s at the Seers market for %d coins", strings.ToLower(buy.Want), res.Paid)
		b.bankAfterPurchase(ctx, d, buy.Want)
		return
	}
	if res.Busy {
		// LOSING A TURN IS NOT A REFUSAL. The stall serves one buyer at a time, so with a
		// cohort at one market somebody always loses the race -- and serving the full give-up
		// cooldown for it means an account that was merely second in the queue waits fifteen
		// minutes while the stall stands idle. Live on 2026-09-21: Acct224 drew its own
		// million, walked to the stall, lost the turn by seconds and then sat out the
		// cooldown; the fleet owner noticed it had not upgraded.
		//
		// Come back in a turn's length instead. The account keeps chopping in the meantime,
		// which is why a short retry costs nothing.
		//
		// BUT NOT A FIXED LENGTH (2026-09-25). A fixed 90 s kept the losers in step -- they came
		// back together and lost together again, 158 times an hour across the WCFletch cohort --
		// so the wait now doubles per consecutive loss and is jittered. See marketBusyWait.
		wait := b.marketBusyWait()
		b.axeNext = time.Now().Add(wait)
		ctx.logf("woodcutter: the Seers market was busy with another account (%s) — trying again in %s",
			res.Reason, wait.Round(time.Second))
		return
	}
	ctx.logf("woodcutter: the Seers market did not sell a %s (%s)", strings.ToLower(buy.Want), res.Reason)

	// A STALL THAT SAID IT HAS NONE HAS ANSWERED A QUESTION, so do not ask it again — ask for
	// a cheaper tier instead. This is the owner's third requirement working in the direction
	// nobody had built: measured 2026-09-22, the Seers market stocked NO rune axe and NO
	// adamant axe, and nine mithril axes at 600 coins. Without this the account re-asks for the
	// one configured tier every axeRetry forever, which is exactly what acct134 was doing with
	// 150,000 coins in its pack.
	//
	// Only a STOCK refusal is remembered. woodcutplan.MarketRefusalIsStock names the two
	// reasons that mean it; the ones about this account's own coins, its budget or a walk that
	// failed must never be recorded, because doing so would teach the account that an axe it
	// can plainly buy does not exist.
	if woodcutplan.MarketRefusalIsStock(res.Reason) {
		b.marketNo.Mark(buy.Want, time.Now(), woodcutplan.DefaultUnavailableFor)
		// Is there a cheaper tier left to try? If so this is not a give-up, it is a different
		// question, and it deserves the short retry rather than axeRetry's fifteen minutes —
		// the account is chopping the whole time either way, and the next trip is the one that
		// actually gets it an axe.
		if next := woodcutplan.AxeTargets(b.settings.AxeWanted, priceOf, b.marketSkip(time.Now())); len(next) > 0 {
			b.axeNext = time.Now().Add(axeTierRetry)
			ctx.logf("woodcutter: %q has no %s to sell — trying a %s instead in %s",
				b.settings.MarketSeller, strings.ToLower(buy.Want), strings.ToLower(next[0]), axeTierRetry)
			return
		}
	}

	// Bob's Brilliant Axes is the fallback, and it is a real shop interface rather than a
	// person — but it only stocks bronze, iron and steel, so most of the time there is no
	// fallback at all and saying so is more useful than a silent give-up.
	if fallback := woodcutplan.FallbackFrom(buy.From, buy.Want); fallback == woodcutplan.AxeFromBob {
		if woodcutrunner.BuyFromBob(d, b.settings.BobStand, buy.Want) {
			b.axesBought++
			ctx.logf("woodcutter: bought a %s from Bob's Axes instead", strings.ToLower(buy.Want))
			return
		}
	} else if rescue := woodcutplan.AxelessRescue(held); len(rescue) > 0 {
		// NOTHING TO CHOP WITH IS AN EMERGENCY, not a purchase decision. See
		// woodcutplan.AxelessRescue for acct134, which sat at Seers for 25 minutes at
		// Woodcutting 1, holding 150,000 coins and no axe, asking a market that never
		// answered. Buy whatever Bob has, best first, and go back to wanting the real one on
		// the ordinary cadence once the account is actually earning.
		if b.rescueFromBob(ctx, d, buy.Want, rescue) {
			return
		}
	} else {
		ctx.logf("woodcutter: no shop on this server stocks a %s — the market is the only source",
			strings.ToLower(buy.Want))
	}
	b.axeNext = time.Now().Add(axeRetry)
}

// bankAfterPurchase puts away what the account no longer needs to carry: the axes the new one
// supersedes, and the coins left over.
//
// The fleet owner caught this the moment the first purchases landed -- accounts walked away
// from the market still holding bronze axes and over a million coins. Both are pure dead weight
// in a 28-slot pack whose whole job is carrying logs, and the coins are worse than dead weight:
// an account wandering the world with a seven-figure stack is one death away from losing it,
// and that money came out of the mule.
//
// Cheap exactly here. The stall stands beside the Seers bank, which is this account's own, so
// this is a few tiles at the one moment the account is certain to be standing there. Failure is
// not fatal and not retried: the ordinary bank trip that follows chopping will do it anyway,
// and refusing to leave the market over a bank that would not open would be worse than
// carrying the weight for one more load.
func (b *woodcutterBot) bankAfterPurchase(ctx *Context, d *woodcutrunner.Deps, bought string) {
	bank, opened := woodcutrunner.OpenSeersBank(d)
	if !opened {
		ctx.logf("woodcutter: bought the %s but the Seers bank would not open to put the old one away",
			strings.ToLower(bought))
		return
	}
	defer func() { _ = bank.Close() }()

	// Every axe except the one just bought. AxeToWithdraw's own ladder decides what is best
	// elsewhere; here the answer is simply "the one we just paid for".
	keep := strings.ToLower(strings.TrimSpace(bought))
	stowed := 0
	if inv := ctx.Session.State().Inventory(woodcutrunner.PlayerPack); inv != nil {
		for _, it := range inv.NonEmpty() {
			obj, ok := d.Config.Obj(int(it.ID))
			if !ok || !woodcutplan.IsAxe(obj.Name) {
				continue
			}
			if strings.ToLower(strings.TrimSpace(obj.Name)) == keep {
				continue
			}
			if err := bank.DepositAll(it.ID); err != nil {
				ctx.logf("woodcutter: banking the old %s: %v", strings.ToLower(obj.Name), err)
				continue
			}
			stowed++
		}
	}

	// Coins, keeping only what the next trip could need. Leaving a fare behind means the
	// account is never stranded by this tidy-up, which is the one way it could do harm.
	reserve := b.settings.Fare
	banked := 0
	if held := woodcutrunner.CoinsHeld(d); held > reserve {
		if inv := ctx.Session.State().Inventory(woodcutrunner.PlayerPack); inv != nil {
			for _, it := range inv.NonEmpty() {
				obj, ok := d.Config.Obj(int(it.ID))
				if !ok || !strings.EqualFold(strings.TrimSpace(obj.Name), freshstart.Coins) {
					continue
				}
				if err := bank.DepositAll(it.ID); err != nil {
					ctx.logf("woodcutter: banking the leftover coins: %v", err)
					break
				}
				banked = held
				break
			}
		}
	}
	b.bankAxes = woodcutrunner.BankAxes(d, bank)
	b.bankRead = true
	if stowed > 0 || banked > 0 {
		ctx.logf("woodcutter: put away %d superseded axe(s) and %d coin(s) after buying the %s",
			stowed, banked, strings.ToLower(bought))
	}
}

// askMuleForCoins asks the configured mule for the coins an axe needs, and reports whether any
// actually arrived. Mirrors fisherBot.askMuleForATool: donorrunner.Await is the complete,
// receipt-verified donor half of the mule's hand-OUT path, and this adds nothing to it.
//
// Bounded by Await's own timeout in every case, so a mule that is not running, has no coins or
// declines outright costs one bounded wait and the run carries straight on chopping.
func (b *woodcutterBot) askMuleForCoins(ctx *Context, d *woodcutrunner.Deps, axe string, need int) bool {
	// SPEND THIS ACCOUNT'S OWN MONEY FIRST.
	//
	// DecideAxeBuy reads CoinsHeld, which is the PACK, so an account with a fortune in its own
	// bank still looked broke and asked the mule for more. That is not hypothetical: the fleet
	// owner spotted it, and three accounts checked live on 2026-09-21 held 1,500,000,
	// 1,550,025 and 1,200,006 coins in their Seers banks -- 4.25M stranded across three
	// accounts, every one of them still carrying a BRONZE axe and still asking for another
	// 150,000.
	//
	// The loop that built it: ask the mule, fail to buy, bank the coins on the next bank trip,
	// arrive with an empty pack, ask again. That is what exhausted the mule's hand-out window
	// earlier today, and raising the window treated the symptom.
	//
	// Cheap and safe to check here. The script lives at the Seers bank and this is its own
	// bank, so the trip is a few tiles against the 83 to the mule; if the bank covers the need
	// the mule is never asked at all. Only from Seers, matching repairFunding's rule that a
	// bank trip must not cost more than the journey it funds.
	if woodcutrunner.CoinsHeld(d) < need {
		if x, z, level, ok := ctx.Session.State().Tile(); ok &&
			woodcutplan.NearSeers(tile.Tile{X: x, Z: z, Level: level}) {
			if bank, opened := woodcutrunner.OpenSeersBank(d); opened {
				before := woodcutrunner.CoinsHeld(d)
				woodcutrunner.WithdrawAllByName(d, bank, freshstart.Coins)
				b.bankAxes = woodcutrunner.BankAxes(d, bank)
				b.bankRead = true
				_ = bank.Close()
				if drawn := woodcutrunner.CoinsHeld(d) - before; drawn > 0 {
					ctx.logf("woodcutter: drew %d coins from my own bank toward a %s (had %d in hand, need %d)",
						drawn, strings.ToLower(axe), before, need)
				}
				if woodcutrunner.CoinsHeld(d) >= need {
					ctx.logf("woodcutter: my own bank covers the %s — not asking %q", strings.ToLower(axe), b.settings.Mule)
					return true
				}
			}
		}
	}
	// THEN OPEN WHAT IS ALREADY OWNED. A casket pays out coins among its rewards, and the
	// accounts that stalled waiting on a 60-coin ship fare were the ones richest in them: the
	// four converted fishers checked live on 2026-09-21 held 86, 74, 41 and 12 banked caskets
	// and up to two hundred oysters each, and zero coins. Walking 83 tiles to the mule to be
	// handed 60 coins, while carrying dozens of unopened boxes that each might hold more than
	// that, is the trip this skips -- the owner's call, and the numbers agree with it.
	//
	// Only for a small need. A 150,000-coin rune axe will never come out of a casket, and
	// three bank rounds spent finding that out is time the account could have spent chopping.
	if need <= casketFareCeiling && woodcutrunner.CoinsHeld(d) < need {
		if before := woodcutrunner.CoinsHeld(d); woodcutrunner.OpenBankedForCoins(d, need) > before {
			got := woodcutrunner.CoinsHeld(d)
			ctx.logf("woodcutter: opened banked caskets and got %d coins toward the %d needed for a %s",
				got-before, need, strings.ToLower(axe))
			if got >= need {
				ctx.logf("woodcutter: my own caskets covered it — not asking %q", b.settings.Mule)
				return true
			}
		}
	}
	// THEN ROB A MAN, IF ONE IS STANDING CLOSER THAN THE MULE. The owner's third rung
	// (2026-09-21): "if they have no caskets and no coins, they should find a nearby man if its
	// closer than walking all the way to the mule and queuing". The mule's real cost is the
	// queue, not the walk -- one trade partner at a time against a measured 3.3x demand
	// shortfall -- so a Man two tiles away is worth taking. See woodcutplan.PreferRobbing.
	if need <= casketFareCeiling && woodcutrunner.CoinsHeld(d) < need {
		if before := woodcutrunner.CoinsHeld(d); woodcutrunner.RobNearbyForCoins(d, need, b.settings.MuleStand) > before {
			got := woodcutrunner.CoinsHeld(d)
			ctx.logf("woodcutter: picked %d coin(s) out of nearby pockets toward the %d needed for a %s",
				got-before, need, strings.ToLower(axe))
			if got >= need {
				ctx.logf("woodcutter: covered the fare without the mule — not asking %q", b.settings.Mule)
				return true
			}
		}
	}
	if b.settings.Mule == "" || ctx.Bus == nil {
		return false
	}
	// STAND WITH THE MULE BEFORE ASKING. A hand-out is a TRADE: mulerunner.Handout resolves the
	// donor's player slot before it draws anything and declines outright when it cannot see
	// them. This script lives at the Seers bank and the fleet's mule posts at Catherby, 83
	// tiles away, so asking from where the script happens to be standing can only fail.
	//
	// Live on 2026-09-21, minutes after axePrice/axeBudget were first switched on: Acct218,
	// Acct220, Acct222 and Acct225 all logged `"mule" declined the order: you are not in
	// sight of me`. The AXE hand-out already walks there (woodcutter.go's orderStep, with its
	// own note recording the same failure); the COIN ask did not, and it is the same trade.
	stand := b.settings.MuleStand
	if tile.Chebyshev(currentTileOf(d), stand) > muleMeetRadius {
		// TravelTo, not WalkNear: Seers -> Catherby is a cross-region leg.
		if !woodcutrunner.TravelTo(d, stand, muleMeetRadius) {
			b.warn(ctx, "coin-walk", 60*time.Second,
				"could not reach %q's post at %d,%d to be handed coins for a %s — retrying",
				b.settings.Mule, stand.X, stand.Z, strings.ToLower(axe))
			return false
		}
	}

	// ASK FOR WHAT IS NEEDED AND LET THE MULE CLAMP IT.
	//
	// This used to clamp against orderplan.DefaultPolicy(nil) — the DEFAULT catalogue, whose
	// CoinsPerOrder is 500, a ladder tool at stock price. That is not this mule's policy: a
	// mule funding market purchases carries its own coinsPerOrder (orderplan.WithCoinLimits),
	// and the mule's is 170,000. Clamping to the default meant an account short of a 150,000
	// rune axe asked for 500 and would have kept asking for 500 forever, however much the mule
	// was configured to give.
	//
	// The donor cannot see the mule's policy, so it must not guess at it. The mule applies its
	// own ceiling on receipt and hands over what it allows; the caller already handles being
	// given less than it asked for by coming back for the rest.
	if _, listed := orderplan.InCatalog(orderplan.DefaultPolicy(nil), freshstart.Coins); !listed {
		ctx.logf("woodcutter: coins are not a hand-out item at all — cannot fund a %s", strings.ToLower(axe))
		return false
	}
	ask := need

	b.coinAsks++
	ctx.logf("woodcutter: %s", donorrunner.DescribeAsk(b.settings.Mule, freshstart.Coins))
	res := donorrunner.Await(&donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}, donorrunner.AwaitOptions{
		Mule: b.settings.Mule, Me: ctx.Username, Item: freshstart.Coins, Qty: ask, Bus: ctx.Bus,
	})
	switch {
	case res.Got:
		b.coinsGot++
		ctx.logf("woodcutter: %q handed over coins toward a %s", b.settings.Mule, strings.ToLower(axe))
		return true
	case res.Stopped:
		return false
	default:
		ctx.logf("woodcutter: %q did not hand over coins (%s)", b.settings.Mule, res.Reason)
		return false
	}
}

// fundRepairTrip makes sure the account is carrying Bob's fee AND the fare home before it sets
// out, drawing the difference from its own Seers bank when it is standing next to one.
//
// Returns true when the trip may go ahead. False means "not this pass": either coins were just
// withdrawn (the next Loop re-decides with them in the pack) or there is genuinely no money
// anywhere, which is said plainly rather than by walking half way to Lumbridge and stopping.
func (b *woodcutterBot) fundRepairTrip(ctx *Context, d *woodcutrunner.Deps) bool {
	_, repaired, held := woodcutrunner.BrokenAxeHeld(d)
	if !held {
		return true
	}
	need := woodcutplan.RepairTripCoins(repaired, b.settings.Fare)
	coins := woodcutrunner.CoinsHeld(d)
	if coins >= need {
		return true
	}
	// Only the Seers bank is this account's own, and only from Seers is a bank trip cheaper
	// than the journey it is funding. Anywhere else, carry on and let RepairAtBob refuse on
	// its own coin check rather than adding a second cross-map leg to pay for the first.
	x, z, level, ok := ctx.Session.State().Tile()
	if !ok || !woodcutplan.NearSeers(tile.Tile{X: x, Z: z, Level: level}) {
		return true
	}
	bank, opened := woodcutrunner.OpenSeersBank(d)
	if !opened {
		b.warn(ctx, "repair-fund", 60*time.Second,
			"a broken %s needs %d coins (Bob's fee plus the fare each way) and the pack holds %d, and the Seers bank would not open",
			strings.ToLower(repaired), need, coins)
		ctx.Delay(3 * time.Second)
		return false
	}
	got := woodcutrunner.WithdrawAllByName(d, bank, freshstart.Coins)
	b.bankAxes = woodcutrunner.BankAxes(d, bank)
	b.bankRead = true
	_ = bank.Close()
	if !got || woodcutrunner.CoinsHeld(d) < need {
		b.warn(ctx, "repair-fund", 120*time.Second,
			"a broken %s costs %d coins to fix and get home with, and this account has %d in hand with none to draw — leave coins in its Seers bank, or a spare axe",
			strings.ToLower(repaired), need, woodcutrunner.CoinsHeld(d))
		ctx.Delay(5 * time.Second)
		return false
	}
	ctx.logf("woodcutter: drew %d coins for the trip to Bob's (fee plus the fare each way)", woodcutrunner.CoinsHeld(d))
	return false
}

// ---- the woodcutting random events this script owns ----

// eventStep handles one pass of an axe-head or broken-axe event.
//
// Both are bounded and both fall through to ordinary work on failure rather than looping: an
// account that cannot find its axe head is better off going to buy an axe than standing in a
// clearing forever.
func (b *woodcutterBot) eventStep(ctx *Context, d *woodcutrunner.Deps, ev woodcutplan.WoodEvent) {
	if ev != b.event {
		b.event = ev
		ctx.logf("woodcutter: random event: %s", woodcutplan.DescribeWoodEvent(ev))
	}
	switch ev {
	case woodcutplan.EventAxeHead:
		res := woodcutrunner.RecoverAxeHead(d)
		switch {
		case res.Stopped:
			return
		case res.Fixed:
			b.axeHeadsFixed++
			b.event = woodcutplan.EventNone
			ctx.logf("woodcutter: axe head re-attached — back to chopping")
		default:
			// Deliberately short: the head despawns about two minutes after it lands
			// (^lootdrop_duration = 200 ticks), so a slow retry is the same as giving up.
			b.warn(ctx, "axe-head", 20*time.Second, "could not put the axe back together (%s)", res.Reason)
			ctx.Delay(1500 * time.Millisecond)
		}
	case woodcutplan.EventBrokenAxe:
		// The mule first: one walk to Catherby against two boat fares each way to Lumbridge.
		// Bob's below stays the fallback. See woodcuttermuleaxe.go.
		if b.replaceBrokenAxeFromMule(ctx, d) {
			b.event = woodcutplan.EventNone
			return
		}
		if !b.fundRepairTrip(ctx, d) {
			return
		}
		res := woodcutrunner.RepairAtBob(d, b.settings.BobStand)
		if res.Fixed {
			b.axesRepaired++
			b.repairSpent += res.Paid
			b.event = woodcutplan.EventNone
			ctx.logf("woodcutter: Bob repaired the broken axe — %s back for %d coins", strings.ToLower(res.Axe), res.Paid)
			return
		}
		// Named reason, never one shared message: the three ways this fails (no coins, could
		// not get there, Bob would not take it) want three different operator responses, and
		// docs/trustworthy_harnesses.md's own "every decline path logs its specific reason"
		// rule came out of exactly this shape in the mule code.
		b.warn(ctx, "axe-repair", 120*time.Second,
			"could not get the broken axe repaired (%s) — only Bob's in Lumbridge repairs one", res.Reason)
		ctx.Delay(5 * time.Second)
	}
}

// mustPriceAxe is the price for the axe DecideAxeBuy chose. It cannot be unknown: priceOf is
// what chose it, and an unpriced axe is never chosen. Zero would mean "never buys", so a bug
// here fails closed rather than paying whatever is asked.
func mustPriceAxe(b *woodcutterBot, axe string) int {
	p, _ := b.settings.PriceOfAxe(axe)
	return p
}
