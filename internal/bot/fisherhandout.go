package bot

// ProgressiveFisher's side of the mule hand-OUT: when the fisher is genuinely blocked on a
// missing tool, ask the mule standing at the same bank for one before walking off to
// pickpocket the price of a new one.
//
// Kept in its own file, with all of its state on one field of fisherBot, deliberately: the main
// session is working in internal/bot/fisher.go and internal/scripts/fisherplan tonight, so this
// is an additive hook (one field, one call site) rather than edits scattered through a file
// someone else is mid-change in. See COLLAB.md.
//
// The incident, exactly (2026-09-18): production account `spare28`, Fishing 55 and therefore
// genuinely valuable, lost its Big fishing net with 0 coins and nothing sellable and sat at the
// Catherby bank at 0 XP/hour for hours — while `mule` stood about two tiles away holding a
// bank full of both nets and coins. fisherplan.DecideFishPhase already identifies that state
// precisely: it returns PhaseFund for "no usable tool held or banked, not enough coins for one
// (including the bank's own), and nothing to sell." The pickpocket recovery that PhaseFund runs
// does eventually work, but it is slow (a cross-map walk to a Man, then coins, then a shop) and
// fragile (see freshstartrunner's own STRANDED branch and the whole class of bugs around it).
// Asking the account standing next to it is both faster and simpler.
//
// Two things make this safe to try first and cheap to be wrong about:
//   - It is bounded. donorrunner.Await returns within its own timeout whether the mule answers,
//     declines or says nothing, and this returns false in every one of those cases, so the
//     ordinary PhaseFund pickpocketing runs exactly as it does today. Asking must never be able
//     to make a stuck account MORE stuck than it already was.
//   - It has a cooldown. A mule with no tools in its bank, or one that is not running at all,
//     must not turn into an ask on every single Loop call.

import (
	"time"

	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/orderplan"
)

// askRetry is how long to leave the mule alone after an ask that did not produce a tool.
// Deliberately much shorter than donorplan.HandoffRetry (5 minutes): a hand-OVER that failed
// can simply wait, but an account in this state is earning nothing at all, so retrying sooner
// costs nothing and the mule's own per-donor window cap (orderplan.Policy.MaxOrdersPerDonor)
// is what actually stops an ask loop from being a nuisance.
const askRetry = 2 * time.Minute

// muleAsk is this account's own memory of asking. Zero value means "never asked".
type muleAsk struct {
	nextAllowed time.Time
	// fallback flips to true once an ask for the current rung's own tool came back with the
	// mule having none. The next ask then asks for the ladder's first rung instead: a fleet
	// mule is far more likely to hold a Small fishing net (5gp, every account buys one) than
	// a members-only Big fishing net, and ANY usable tool ends the blocked state — from there
	// DecideFishPhase goes back to fishing, sells the catch and buys its way back up the
	// ladder on its own.
	fallback bool
	asks     int
	got      int
}

// askMuleForATool asks the configured mule for the tool this account is blocked without, and
// reports whether one actually arrived. False means "carry on with the ordinary recovery".
//
// Only ever called for PhaseFund while already IN Catherby — see the call site in fisher.go's
// own Loop for why that combination is exactly the blocked-at-the-bank case and nothing else
// (the displaced-but-reachable reading of PhaseFund has already been redirected to PhaseTravel
// by then, which is the fix a53c937/a53c927 landed for the hot loop this same account caused).
func (b *fisherBot) askMuleForATool(ctx *Context, s fisherplan.FishSnapshot) bool {
	if b.ask == nil {
		b.ask = &muleAsk{}
	}
	if b.mule == "" || ctx.Bus == nil {
		return false
	}
	if time.Now().Before(b.ask.nextAllowed) {
		return false
	}
	item := fisherplan.CurrentRung(s.Level, s.Target).Tool
	if b.ask.fallback {
		item = fisherplan.Rungs[0].Tool
	}
	// Never ask for something the mule would refuse on sight: the catalogue is the mule's,
	// but it is a plain value, so the donor can check it and spare both sides a round trip.
	if _, listed := orderplan.InCatalog(orderplan.DefaultPolicy(nil), item); !listed {
		return false
	}

	ctx.logf("fisher: blocked with no usable tool — %s", donorrunner.DescribeAsk(b.mule, item))
	b.ask.asks++
	res := donorrunner.Await(&donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}, donorrunner.AwaitOptions{
		Mule: b.mule, Me: ctx.Username, Item: item, Qty: 1, Bus: ctx.Bus,
	})
	switch {
	case res.Got:
		b.ask.got++
		b.ask.fallback = false
		ctx.logf("fisher: %q handed over a %s — back to work", b.mule, item)
		return true
	case res.Stopped:
		return false
	default:
		// Any refusal means try the cheap, always-in-stock tool next time instead of asking
		// for the same thing again and being told the same thing again.
		b.ask.fallback = true
		b.ask.nextAllowed = time.Now().Add(askRetry)
		ctx.logf("fisher: %q did not hand over a %s (%s) — falling back to earning one, next ask in %s",
			b.mule, item, res.Reason, askRetry)
		return false
	}
}
