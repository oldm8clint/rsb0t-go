package bot

// ProgressiveWoodcutter's side of the mule hand-OUT: when the account has no axe anywhere it
// can reach one, ask the mule for an axe before falling through to the coins-and-player-market
// path that needs a human.
//
// Kept in its own file, with all of its state on one field of woodcutterBot, for the same
// reason internal/bot/fisherhandout.go is — it is an additive hook (one field, one call site)
// rather than edits scattered through a file another session may be mid-change in.
//
// THE INCIDENT, exactly (2026-09-21, PRODUCTION). Five accounts (`Acct218-222`) were swapped
// from ProgressiveFisher to ProgressiveWoodcutter with the mule and meet tile configured
// correctly. They logged in, reached the Seers bank correctly, and then all parked:
//
//	woodcutter: phase: at the Seers bank
//	woodcutter: phase: waiting for the mule to bring a rune axe
//	woodcutter: WARNING: no axe purchase is configured (axePrice and axeBudget are both needed)
//
// A swapped fisher has no axe. PhaseOrder routed to axeStep, which asks the mule for COINS and
// then buys an axe at the Seers PLAYER market — it says "buying 1 rune axe" out loud and waits
// for a human to sell it one — and axePrice/axeBudget both default to 0, where zero never buys.
// So an unattended cohort could not start at all. Meanwhile `mule`'s own bank held **Mithril
// axe x6, Bronze axe x15, Iron axe x4, Steel axe x1, Adamant axe x1**. The fleet owned the
// axes; nothing could move one.
//
// THE REASON THAT HAD BEEN RECORDED FOR NOT DOING THIS IS FALSIFIED, and that is worth saying
// rather than quietly fixing. internal/scripts/woodcutrunner's package doc said asking the mule
// for an axe was not ported because "a fleet mule's bank holds logs and casket loot rather than
// rune axes, and nothing can restock one". The first half is measurably untrue of the real
// mule. The second half was only ever true of a RUNE axe — and woodcutplan/axe.go's own content
// research had already established that **any axe chops from Woodcutting 1**, so a mithril axe
// ends the blocked state just as completely. The cheap fix was available the whole time and the
// recorded reasoning hid it.
//
// Two things make this safe to try first and cheap to be wrong about, the same two that make
// the fisher's version safe:
//   - It is bounded. donorrunner.Await returns within its own timeout whether the mule answers,
//     declines or says nothing, and this returns false in every one of those cases, so the
//     existing axeStep path runs exactly as it does today. Asking must never be able to make a
//     stuck account MORE stuck.
//   - It has a cooldown, so a mule with no axes — or one that is not running at all — does not
//     turn into an ask on every Loop call.

import (
	"time"

	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// axeAsk is this account's own memory of asking the mule for an axe. The zero value means
// "never asked".
type axeAsk struct {
	nextAllowed time.Time
	// rung is how far down orderplan.AxeHandouts the next ask should start. It only ever moves
	// DOWN, on a refusal, and resets when an axe actually arrives.
	//
	// This is the fisher's own `fallback` bool generalised, and for the same reason: asking for
	// the same thing that was just refused and being told the same thing again is the one
	// behaviour worth ruling out. The list is ordered best-first, so walking down it asks for
	// the best axe the mule will actually part with. ANY axe ends the blocked state, because
	// axes have no Woodcutting requirement on this server.
	rung int
	asks int
	got  int
}

// askMuleForAnAxeReady reports whether an ask would actually be sent right now: a mule is
// configured, there is a bus to carry it, and this account is not inside its own cooldown.
//
// Split out of askMuleForAnAxe so the CALLER can ask the question before committing to the walk
// to the mule's post. Without it, an account whose ask is cooling down — or that has no mule at
// all — would still cross the map to stand next to nobody, every single pass.
func (b *woodcutterBot) askMuleForAnAxeReady() bool {
	if b.settings.Mule == "" {
		return false
	}
	if b.axeAsk != nil && time.Now().Before(b.axeAsk.nextAllowed) {
		return false
	}
	return true
}

// muleAxeLadder is the axes worth asking THIS mule for right now, best first, and whether
// anything is actually known about what it holds.
//
// THE FLEET OWNER'S REPORT, 2026-09-22: "woodcutters are asking the mule for axes they dont even
// have instock ... they should beable to know whats in their bank to request". Three filters,
// cheapest first, and each one measured rather than guessed:
//
//  1. THE MULE'S OWN SAFETY CATALOGUE. Unchanged, and still first: a name the mule would refuse
//     on sight is not worth a bus message. (This check used to sit inside the per-rung ask,
//     where an unlisted rung silently ended the whole ladder instead of being skipped.)
//
//  2. THE MULE'S OWN PUBLISHED BANK, which is new and is the answer to the report. Measured
//     2026-09-22 from the live panel: `mule` held exactly ONE chopping axe, an **Adamant axe**,
//     and adamant is deliberately absent from orderplan.AxeHandouts() -- so all four rungs a
//     woodcutter could ask for were empty, and the old code asked for all four anyway, every
//     two minutes, after an 83-tile walk. When nothing has been published this filter does
//     nothing at all and the blind ladder is asked exactly as before (known=false).
//
//  3. THIS ACCOUNT'S MEMORY OF WHAT THE MULE HAS ALREADY SAID IT HAS NONE OF. The decline was
//     always truthful -- orderplan.Decide reads the real bank and answers "none in the bank" --
//     and the old code logged it and threw it away. Remembering it is what breaks the loop even
//     on a fleet where nothing is published, which is why both this and filter 2 exist rather
//     than only the tidier one.
func (b *woodcutterBot) muleAxeLadder(ctx *Context) (ask []string, known bool) {
	return b.muleAxeLadderWhere(ctx, nil)
}

// muleAxeLadderWhere is muleAxeLadder keeping only the axes keep accepts (nil keeps all): the
// upgrade and broken-axe asks want only axes better than, or as good as, a given one.
func (b *woodcutterBot) muleAxeLadderWhere(ctx *Context, keep func(axe string) bool) (ask []string, known bool) {
	pol := orderplan.DefaultPolicy(nil)
	listed := make([]string, 0, 4)
	for _, h := range orderplan.AxeHandouts() {
		if _, ok := orderplan.InCatalog(pol, h.Item); !ok {
			continue
		}
		if keep != nil && !keep(h.Item) {
			continue
		}
		listed = append(listed, h.Item)
	}
	inStock, known := woodcutplan.MuleAxeLadder(listed, muleBankCount(ctx, b.settings.Mule))
	return b.muleNoAxe.Available(inStock, time.Now()), known
}

// muleMightHaveAnAxe reports whether it is worth WALKING to the mule for an axe at all.
//
// This is the expensive half of the report. Seers to the mule's Catherby post is 83 tiles each
// way, and the account earns nothing the whole time. Live on acct134, 2026-09-22: a 106-tile
// round trip about every 70 seconds, at zero experience, for as long as it was watched — for
// four axes the mule did not have.
//
// False ONLY when something is genuinely known and says there is nothing to ask for. An
// unconfigured fleet, an unpublished mule or a stale row all answer true and walk, exactly as
// before: this must never be the reason an account stops asking a mule that does have an axe.
func (b *woodcutterBot) muleMightHaveAnAxe(ctx *Context) bool {
	ask, known := b.muleAxeLadder(ctx)
	if len(ask) > 0 {
		// Includes the "nothing is known and nothing has been refused" case, where the ladder
		// comes back unfiltered and the account asks blind exactly as it always did.
		return true
	}
	// An empty ladder always means there is nothing to ask for, but WHY differs and an
	// operator reading the log needs to know which: a published bank that holds none of them,
	// or four declines this account collected itself.
	source := "it has already declined every one of them"
	if known {
		source = "its published bank holds none of them"
	}
	b.warn(ctx, "mule-no-axe", 10*time.Minute,
		"%q has no axe this account may be handed (%s; the hand-out catalogue is %v) — not walking to its post; buying at the Seers market is the only source until it restocks",
		b.settings.Mule, source, axeNames(orderplan.AxeHandouts()))
	return false
}

// axeNames is orderplan.AxeHandouts's items, for a log line.
func axeNames(h []orderplan.Handout) []string {
	out := make([]string, 0, len(h))
	for _, e := range h {
		out = append(out, e.Item)
	}
	return out
}

// askMuleForAnAxe asks the configured mule for an axe and reports whether one actually arrived.
// False means "carry on with the ordinary axeStep recovery".
func (b *woodcutterBot) askMuleForAnAxe(ctx *Context, d *woodcutrunner.Deps) bool {
	return b.askMuleForAnAxeWhere(ctx, d, nil)
}

// askMuleForAnAxeWhere is askMuleForAnAxe asking only for the axes keep accepts (nil: any).
func (b *woodcutterBot) askMuleForAnAxeWhere(ctx *Context, d *woodcutrunner.Deps, keep func(axe string) bool) bool {
	if b.axeAsk == nil {
		b.axeAsk = &axeAsk{}
	}
	if b.settings.Mule == "" || ctx.Bus == nil {
		return false
	}
	if time.Now().Before(b.axeAsk.nextAllowed) {
		return false
	}
	// WALK THE LADDER DOWN WITHOUT WALKING THE ACCOUNT ANYWHERE.
	//
	// Each rung used to return false after a stock decline, which handed control back to
	// orderStep -- and orderStep's next move is the Seers market, 83 tiles away. So an account
	// refused Mithril walked to Seers, walked BACK to the mule, was refused Steel, walked to
	// Seers... Measured live on acct134 (2026-09-22): a 106-tile round trip about every 70
	// seconds, at zero experience, for as long as it was watched.
	//
	// The account is ALREADY standing at the mule's post when it gets here (orderStep put it
	// there), so asking the next tier costs one bus message and nothing else. Only a genuine
	// stock decline advances the rung -- a positional failure or a silence still stops, exactly
	// as the cases below explain, because neither says anything about what the mule holds.
	// THE LADDER IS NOW WHAT THE MULE IS KNOWN TO HAVE, not a fixed list of four.
	//
	// Computed once per call, and the rung restarted against it, because the filtering has
	// already done the work the rung used to do by trial: a tier the mule has published zero
	// of, or has already answered "none in the bank" about, is simply not on this list. See
	// muleAxeLadder for the three filters and the measurements behind them.
	ask, known := b.muleAxeLadderWhere(ctx, keep)
	if len(ask) == 0 {
		b.axeAsk.rung = 0
		b.axeAsk.nextAllowed = time.Now().Add(axeAskRetry)
		if known {
			// Not a warning and not a loop: this is the correct, quiet answer to "the mule
			// has none", and the account carries straight on to the purchase path.
			ctx.logf("woodcutter: %q holds none of the axes it may hand out — not asking; trying to buy one instead",
				b.settings.Mule)
		} else {
			ctx.logf("woodcutter: %q has already said it has none of %v — not asking again for now",
				b.settings.Mule, axeNames(orderplan.AxeHandouts()))
		}
		return false
	}
	b.axeAsk.rung = 0
	for {
		got, advanced := b.askMuleForOneAxeTier(ctx, d, ask)
		if got {
			return true
		}
		if !advanced {
			return false
		}
	}
}

// askMuleForOneAxeTier asks for the current rung of ask (best first). got reports an axe really
// arrived; advanced reports the rung moved down because the mule answered about STOCK, which is
// the only answer that makes asking for something cheaper sensible.
func (b *woodcutterBot) askMuleForOneAxeTier(ctx *Context, d *woodcutrunner.Deps, ask []string) (got, advanced bool) {
	if b.axeAsk.rung >= len(ask) {
		// Every tier has been refused this cycle. Start again from the best after the
		// cooldown rather than never asking again: a mule restocks, and the whole point of
		// this hook is that the account is earning nothing until it works.
		b.axeAsk.rung = 0
		b.axeAsk.nextAllowed = time.Now().Add(axeAskRetry)
		return false, false
	}
	item := ask[b.axeAsk.rung]

	ctx.logf("woodcutter: no axe anywhere — %s", donorrunner.DescribeAsk(b.settings.Mule, item))
	b.axeAsk.asks++
	b.coinAsks++
	res := donorrunner.Await(&donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}, donorrunner.AwaitOptions{
		Mule: b.settings.Mule, Me: ctx.Username, Item: item, Qty: 1, Bus: ctx.Bus,
	})
	switch {
	case res.Got:
		// The receipt is this account's own pack, not the trade screen and not the mule's
		// word: donorrunner.Await's own waitForHeld reads PlayerPack 3214 directly. Re-read
		// the held axes here as well, because "an axe arrived" and "an axe I can chop with
		// arrived" are the claim worth making — woodcutplan.IsAxe excludes a broken axe, a
		// handle and a head.
		if !woodcutrunner.HasWorkingAxe(d) {
			ctx.logf("woodcutter: %q handed over a %s but no usable axe is in the pack — treating the ask as failed", b.settings.Mule, item)
			b.axeAsk.rung++
			return false, true
		}
		b.axeAsk.got++
		b.axeAsk.rung = 0
		b.coinsGot++
		// A mule that has just handed one over plainly has them, whatever it said last time.
		b.muleNoAxe.Clear(item)
		ctx.logf("woodcutter: %q handed over a %s — back to work with %v", b.settings.Mule, item, woodcutrunner.HeldAxes(d))
		return true, false
	case res.Stopped:
		return false, false
	case res.Declined && orderplan.DeclineIsPositional(res.Reason):
		// A decline about WHERE THIS ACCOUNT IS STANDING, not about what the mule has. It says
		// nothing whatever about stock, so it must not walk the ladder down — the same rule a
		// silence already follows, and for the same reason.
		//
		// MEASURED on the PRODUCTION fleet 2026-09-21, before orderStep learned to walk to the
		// mule's post: an account asking from the Seers bank while the mule posts at Catherby, 83
		// tiles away, was told "you are not in sight of me" for Mithril, then Iron, then Bronze
		// in turn — positional failures read as scarcity answers, ending on the worst axe for a
		// reason unrelated to the mule's bank, which held Mithril x6, Bronze x15 and Iron x4.
		// The walk is the real cure and orderStep now does it; this is the guard that stops the
		// ladder lying when the walk has not worked.
		b.axeAsk.nextAllowed = time.Now().Add(axeAskRetry)
		ctx.logf("woodcutter: %q could not see me to hand over a %s (%s) — keeping the same axe, next ask in %s",
			b.settings.Mule, item, res.Reason, axeAskRetry)
		return false, false
	case res.Declined:
		// The mule ANSWERED ABOUT THE ITEM and said no — out of stock, not allow-listed, over
		// its window. That is the only answer that means "ask for something cheaper": this tier
		// is genuinely unavailable, so try the next one down.
		//
		// AND REMEMBER IT, which is new and is the owner's report of 2026-09-22. The answer was
		// always truthful — orderplan.Decide reads the mule's real bank before it says "none in
		// the bank" — and this used to log it and throw it away, so the same four questions
		// were asked again after axeAskRetry, forever. Only an EMPTY-STOCK decline is
		// remembered: a window cap lapses on its own and a positional decline (handled above)
		// says nothing about stock at all, so recording either would be the same mistake in
		// the other direction. See woodcutplan.MuleDeclineIsEmptyStock.
		if woodcutplan.MuleDeclineIsEmptyStock(res.Reason) {
			b.muleNoAxe.Mark(item, time.Now(), woodcutplan.DefaultUnavailableFor)
		}
		ctx.logf("woodcutter: %q declined a %s (%s) — trying the next axe down",
			b.settings.Mule, item, res.Reason)
		b.axeAsk.rung++
		return false, true
	default:
		// NO ANSWER, which is a different thing and must NOT walk down the ladder.
		//
		// Found by the live proof, before it passed: the mule account had not finished logging
		// in when the subject first asked, so the ask timed out. Treating that as a refusal
		// would have burned Mithril -> Steel -> Iron -> Bronze on four timeouts and then sat
		// out the cooldown — while the mule, once up, held three mithril axes and nothing else.
		// The account would have asked only for axes the mule did not have.
		//
		// So a silence keeps the rung and backs off instead. donorrunner.AwaitResult.Declined
		// is what separates the two: it is set only where a real decline message arrived.
		b.axeAsk.nextAllowed = time.Now().Add(axeAskRetry)
		ctx.logf("woodcutter: %q did not answer about a %s (%s) — keeping the same axe and asking again in %s",
			b.settings.Mule, item, res.Reason, axeAskRetry)
		return false, false
	}
}

// axeAskRetry is how long to leave the mule alone after every axe tier has been refused.
//
// Deliberately much shorter than donorplan.HandoffRetry (5 minutes), exactly as the fisher's
// own askRetry is: a hand-OVER that failed can simply wait, but an account in THIS state is
// earning nothing at all, so retrying sooner costs nothing — and the mule's own per-donor
// window cap (orderplan.Policy.MaxOrdersPerDonor) is what actually stops an ask loop from being
// a nuisance.
const axeAskRetry = 2 * time.Minute
