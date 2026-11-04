package bot

// MuleReceiver's side of REPLENISHING ITSELF: noticing it is running low on coins, appealing to
// the fleet, sending for a bounded few of the accounts that answer, and counting what actually
// arrived.
//
// Kept in its own file with all of its state on ONE field of muleBot, deliberately, exactly as
// fisherhandout.go and woodcutterhandout.go already are: mule.go is a 1,400-line file other
// sessions work in, so this is an additive hook (one field, two call sites) rather than edits
// scattered through it. See COLLAB.md.
//
// THE PROBLEM, with the numbers from the day it was written (2026-09-21). The roster went from
// 12 woodcutters to 94, each buying a rune axe from the Seers player market for 150,000 coins
// drawn from this mule. `mule` started at 29,837,076 coins and was at roughly 9M by evening
// with 82 accounts still unequipped — about 12.3M still to pay out, so it runs dry before the
// cohort is equipped. Its own hand-out window ("hand-out limit for that item is spent") paces
// the burn but creates no coins, because THE MULE CANNOT RESTOCK ANYTHING: there is no
// market-buy path and no acquiretask caller anywhere in it. It holds only what donors gave it.
//
// WHY THE MULE ORIGINATES, rather than accounts volunteering their balances. The owner's ask
// was "check all the other accounts with a decent amount of coins on them" — but a mule cannot
// read another account's bank, so "check" has to become either (a) every account publishing its
// balance continuously for the mule to shop from, or (b) the mule broadcasting a need and
// whoever can answer answering. (b) is what is built: it needs no new state anywhere, it keeps
// every account's balance its own business, and — the part that actually matters — the answer
// is computed by the only side that knows what its coins are FOR. See restockplan.Spare's own
// doc comment on Earmarked: a woodcutter holding the 150,000 coins this mule just gave it to
// buy an axe with is, by arithmetic alone, the richest donor in the fleet, and a mule shopping
// from published balances would take it straight back in an infinite round trip.
//
// WHAT IT COSTS THE MULE WHEN IT IS SWITCHED ON. One bank open every restockCheckEvery while
// genuinely idle, and nothing else: the appeal, the pledges and the grants are bus messages,
// and the trade a donor brings is an ordinary incoming hand-over the mule was already built to
// take. Nothing in here reciprocates, walks or trades on its own.

import (
	"time"

	"rsb0t-go/internal/scripts/mulerunner"
	"rsb0t-go/internal/scripts/restockplan"
	"rsb0t-go/internal/scripts/restockrunner"
)

// restockCheckEvery is how often the mule re-reads its own coin count from a REAL bank open.
//
// Three minutes, and the reason is a measured one from this repo rather than a guess: CLAUDE.md
// records `/api/bank-all` taking 267 whole-fleet snapshots per request at a 266-account roster
// and starving `/api/status` of the same gamestate locks, because bank reads are expensive and
// were being taken far too often. One open every three minutes is two orders of magnitude below
// anything like that, and it is far faster than the mule can spend itself past its floor: the
// floor is two of its own maximum hand-out windows, which at the live configuration is a full
// hour of legally-permitted spending.
const restockCheckEvery = 3 * time.Minute

// muleRestock is the whole feature's state on the mule, on one field of muleBot.
type muleRestock struct {
	campaign *restockrunner.Campaign
	// floor/target are derived once at start from this mule's OWN configured per-window coin
	// ceiling (restockplan.FloorAndTarget over restockplan.WindowFor), so a mule whose
	// handoutCoinsPerWindow was raised for market purchases gets a floor sized for that.
	floor, target int
	// held is the last real reading of this mule's own coins, bank plus pack, and checkedAt
	// when it was taken. Zero checkedAt means never — and a campaign never appeals off a
	// reading it has not taken, because "I hold nothing" and "I have not looked" must not
	// produce the same behaviour.
	held      int
	checkedAt time.Time
	// coinIDs is every un-noted object id that spells "Coins", resolved once. A bank read
	// comes back keyed by id, and coins are not a single id.
	coinIDs []int
}

// setUpRestock is called from OnStart. It returns nil when restocking is off, which is the
// default: this is a wealth-movement feature that has been proven in the lab and not yet at
// fleet scale, so an operator turns it on deliberately (`"restock": true`) rather than
// discovering it running.
func (b *muleBot) setUpRestock(ctx *Context) {
	if !BoolSetting(ctx.Settings, "restock", false) {
		ctx.logf("restock: OFF (set \"restock\": true to let this mule ask the fleet to top it up)")
		return
	}
	perWindow := restockplan.WindowFor(b.handoutPolicy, restockplan.CoinsItem)
	floor, target := restockplan.FloorAndTarget(perWindow)
	// An operator may name both explicitly. Both or neither: a floor without a target (or the
	// reverse) is a configuration restockplan refuses to act on, and silently half-applying
	// one would be worse than ignoring it.
	if f, t := IntSetting(ctx.Settings, "restockFloor", 0), IntSetting(ctx.Settings, "restockTarget", 0); f > 0 && t > f {
		floor, target = f, t
		ctx.logf("restock: floor %d and target %d taken from settings", floor, target)
	}
	if floor <= 0 || target <= floor {
		ctx.logf("restock: ON was asked for, but this mule hands out no coins (per-window ceiling %d) — nothing to restock, staying off", perWindow)
		return
	}
	var ids []int
	if ctx.Config != nil {
		for _, id := range ctx.Config.ObjIDs(restockplan.CoinsItem) {
			if obj, ok := ctx.Config.Obj(id); ok && obj.Noted() {
				continue
			}
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		ctx.logf("restock: ON was asked for, but %q could not be resolved to an object id — staying off", restockplan.CoinsItem)
		return
	}
	b.restock = &muleRestock{
		campaign: restockrunner.NewCampaign(IntSetting(ctx.Settings, "restockDonorsAtOnce", restockplan.DefaultMaxInFlight)),
		floor:    floor, target: target, coinIDs: ids,
	}
	ctx.logf("restock: ON — appealing to the fleet below %d coins, topping up to %d, at most %d donor(s) walking at once",
		floor, target, b.restock.campaign.Board().MaxInFlight())
}

// restockIdle reports whether the mule is quiet enough to spend a bank open on a coin count.
// The same gate openBankedOpenables and the idle cooking round already use, for the same
// reason: a mule that walks to a booth is a mule not serving the donors it exists for.
func (b *muleBot) restockIdle() bool {
	return b.trip == nil && b.watcher.QueuedRequests() == 0 && b.incomingPending() == 0 && len(b.orders) == 0
}

// stepRestock is the one call site in muleBot.Loop's wall-bus block. It refreshes the coin
// count when it is stale and the mule is idle, then runs one step of the appeal conversation.
//
// It never blocks on a trade and never initiates one: everything it can do is publish messages
// and, at most, open a bank to count. The donors it sends for arrive as ordinary incoming trade
// requests that RunCycle was already serving.
func (b *muleBot) stepRestock(ctx *Context, d *mulerunner.Deps) {
	r := b.restock
	if r == nil || ctx.Bus == nil {
		return
	}
	now := time.Now()
	if (r.checkedAt.IsZero() || now.Sub(r.checkedAt) >= restockCheckEvery) && b.restockIdle() {
		if totals, ok := mulerunner.ReadBankTotals(d); ok {
			held := 0
			for _, id := range r.coinIDs {
				held += int(totals[id])
			}
			held += coinsInPack(ctx, r.coinIDs)
			r.held, r.checkedAt = held, now
		} else {
			// Do NOT stamp checkedAt on a failed read. A mule that could not reach a booth
			// has not learned anything, and recording the attempt would make it wait another
			// three minutes before trying to find out again.
			ctx.logf("restock: could not open a bank to count my own coins — will try again")
		}
	}
	if r.checkedAt.IsZero() {
		return // never counted: appealing off a number we do not have is exactly the trap.
	}
	step := r.campaign.Step(now, ctx.Bus, ctx.Username, b.postTile(), restockplan.Need{
		Item: restockplan.CoinsItem, Held: r.held, Floor: r.floor, Target: r.target,
	})
	if step.Appealed != nil {
		ctx.logf("restock: %d coins in hand against a floor of %d — asking the fleet for %d",
			r.held, r.floor, step.Appealed.Qty)
	}
	if step.Pledged > 0 {
		ctx.logf("restock: %d account(s) offered to help; %s", step.Pledged, r.campaign.Board().Describe(now))
	}
	for _, g := range step.Granted {
		ctx.logf("restock: sent for %q to bring %d coins", g.To, g.Qty)
	}
}

// noteRestockReceipt is the second call site, straight after RunCycle. It is the ONLY place a
// restock donation is ever counted, and it counts what the mule's own pack actually gained.
//
// mulerunner.CycleResult.Received is set only when completeTrade confirmed both screens and
// read the items in — the same receipt discipline the whole package is built on. A trade
// closing, a grant being issued or a donor saying it was coming proves nothing and is never
// used here.
func (b *muleBot) noteRestockReceipt(ctx *Context, res mulerunner.CycleResult) {
	r := b.restock
	if r == nil || res.Partner == "" {
		return
	}
	now := time.Now()
	if !r.campaign.Board().Granted(now, res.Partner) {
		return // an ordinary hand-over, not a donor we sent for.
	}
	gained := 0
	if res.Accepted {
		for _, it := range res.Received {
			for _, id := range r.coinIDs {
				if int(it.ID) != id {
					continue
				}
				c := int(it.Count)
				if c < 1 {
					c = 1
				}
				gained += c
			}
		}
	}
	// Received is called whatever happened, including a gain of zero: the slot is about
	// occupancy, not success (see restockplan.Board.Arrived). A donor that arrived and failed
	// is no longer walking, and holding its slot for the rest of GrantTTL would throttle the
	// queue for nothing.
	r.campaign.Received(now, res.Partner, gained)
	if gained > 0 {
		// The count is now stale by exactly what just arrived. Adjusting it rather than
		// forcing another bank open keeps the mule from appealing again for money it is
		// already holding, and the next scheduled read corrects any drift.
		r.held += gained
		_, _, _, total := r.campaign.Stats()
		ctx.logf("restock: %q handed over %d coins — %d received this run, now holding about %d against a floor of %d",
			res.Partner, gained, total, r.held, r.floor)
	} else {
		ctx.logf("restock: %q arrived but no coins crossed (%s) — its slot is free again", res.Partner, res.Reason)
	}
}

// coinsInPack counts the mule's own held coins by object id — the same fixed PlayerPack
// component every read in this area uses after a bank open or close.
func coinsInPack(ctx *Context, ids []int) int {
	inv := ctx.Session.State().Inventory(3214)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		for _, id := range ids {
			if int(it.ID) != id {
				continue
			}
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// restockDescribe is one operator-readable line for the log, or "" when restocking is off.
func (b *muleBot) restockDescribe() string {
	if b.restock == nil {
		return ""
	}
	return b.restock.campaign.Describe(time.Now())
}
