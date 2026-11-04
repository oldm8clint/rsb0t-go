package woodcutplan

// Asking for an axe that is ACTUALLY there: filtering the mule's ask ladder by the mule's own
// bank, remembering which sources have said no, and laddering a market purchase down to a tier
// the account can really buy. Pure, like the rest of this package.
//
// THE FLEET OWNER'S REPORT, 2026-09-22, verbatim: "woodcutters are asking the mule for axes they
// dont even have instock... they should beable to know whats in their bank to request, if the
// market has a better axe, they should request coins from the mule if they dont have any coins
// to make the purchase at seer market".
//
// EVERY NUMBER BELOW IS MEASURED OFF THE LIVE PANEL (read-only GETs against 127.0.0.1:7300 at
// 2026-09-22 04:11 UTC), not assumed, because the whole defect is code asking for things
// nobody checked were there:
//
//   - `mule`'s bank holds exactly ONE woodcutting axe: **Adamant axe x1** (obj 1357, slot
//     180), plus 2,331,324 coins. There is NO mithril, steel, iron, bronze, black or rune axe
//     in it. (`Steel battleaxe x4` and the four pickaxes in the same bank are not chopping
//     axes -- see BobStock's own note.)
//   - `orderplan.AxeHandouts()`, which is the ladder a donor walks, is
//     **Mithril -> Steel -> Iron -> Bronze**. Adamant and rune are deliberately excluded from
//     it (its own doc comment explains why). So the mule holds NONE of the four axes a
//     woodcutter is able to ask it for, and every rung is declined "none in the bank" by
//     orderplan.Decide -- correctly, having actually read the bank.
//   - The old code discarded that answer. `askMuleForOneAxeTier` advanced a rung per decline,
//     ran out after four, waited `axeAskRetry` (2 minutes) and started again from Mithril,
//     forever. That is the loop the owner is watching: a walk to Catherby and four questions
//     about axes that are not there, every two minutes, at zero experience.
//   - The Seers market's own axe stock at the same moment: **Mithril axe x9 at 600 coins**,
//     Iron x172, Black x15, Steel x1, Bronze x7 -- and **no adamant axe and no rune axe at
//     all** (only Adamant battleaxe x10 and Rune battleaxe x28, which are weapons). Yet
//     `AxeWanted` defaults to a rune axe and `DecideAxeBuy` only ever wanted that ONE tier, so
//     `acct134` -- holding **150,000 coins and no axe** -- said "buying 1 rune axe" at a stall
//     that has none, gave up for 15 minutes, and repeated. The 150,000 is exactly
//     `DefaultAxePrices[RuneAxe]`: it had already been funded for a purchase that cannot
//     happen, while a 600-coin mithril axe sat nine deep in front of it.
//
// A CLAIM IN THE OLD CODE THAT WAS NOT TRUE, recorded rather than quietly fixed, because it is
// what hid the bug. woodcutteraxe.go's comment above the `priceOf` line read "EVERY tier it
// knows a price for, not just the configured one ... This is what lets the account fall back
// when the market has no rune axe: adamant at 3,150 and mithril at 600 are real, measured
// prices". `DecideAxeBuy` never did that -- it called `priceOf(want)` for the single configured
// `want` and nothing else. The fallback was documented and absent, which is the same shape as
// `orderplan.Handout.Floor` being documented as a gate and implemented as a reserve.
//
// WHAT THIS FILE DELIBERATELY DOES NOT DO. It does not read a bank, a market page or a bus.
// Each function is handed an answer (a `holds` closure, a `priceOf` closure, a memory) and
// decides from it; a caller with no answer to give passes nil and gets exactly the old
// behaviour back. That is the nil-safety rule this change is held to: an account with no mule,
// no market and no peers must behave as it did before.

import (
	"strings"
	"time"
)

// ---- what the mule is known to hold ----

// MuleAxeLadder filters a mule's ask ladder down to the axes that mule is actually known to
// hold, keeping the caller's best-first order.
//
// holds answers "how many of this axe does the mule have, and do we know at all?". The second
// return is the important half and it is the caller's responsibility to get right: a mule's
// bank reading is a WHOLE reading, so a caller that has one must answer ok=true for every axe
// (count 0 included), and a caller with no reading must pass nil, or answer ok=false. Answering
// ok=false per-axe means "no opinion about this one" and keeps the rung, because a directory
// row that predates a deposit must never hide an axe that is really there.
//
// known reports whether any answer at all was available. It is what separates "the mule holds
// no axe, so do not walk to it" from "nobody has told us, so ask blind exactly as before" --
// two states an empty ladder alone cannot distinguish, and conflating them would turn an
// unconfigured fleet into an account that never asks its mule for anything.
func MuleAxeLadder(ladder []string, holds func(axe string) (int, bool)) (ask []string, known bool) {
	if holds == nil {
		return ladder, false
	}
	out := make([]string, 0, len(ladder))
	for _, a := range ladder {
		n, ok := holds(a)
		if ok {
			known = true
			if n <= 0 {
				continue
			}
		}
		out = append(out, a)
	}
	return out, known
}

// ---- remembering a source that has said no ----

// DefaultUnavailableFor is how long a "no" is remembered by default.
//
// Ten minutes, and the two ends of the range are what set it rather than a round number. Too
// short and the account is straight back into the loop this file exists to stop: the mule's
// own ask cycle is 2 minutes (askMuleForAnAxeRetry) and the market's give-up is 15, so
// anything under a few minutes would be forgotten before the next attempt even happens. Too
// long and a real restock goes unnoticed -- and both sources do restock, the mule from the
// fleet's own hand-overs and the stall from whoever runs it. A stale "no" costs one wasted
// attempt when it lapses; a stale "yes" would cost nothing at all, which is why this errs
// short.
const DefaultUnavailableFor = 10 * time.Minute

// AxeUnavailable remembers which axes a source has said it cannot supply, so the next pass
// asks for something else instead of repeating a question that was already answered.
//
// The zero value is usable and every method is nil-safe, deliberately: a bot that never
// records anything must behave exactly as it did before this type existed, and a bot field of
// this type needs no initialisation anywhere.
//
// One memory per SOURCE, never one shared between them. "The mule has no mithril axe" and "the
// stall will not sell a mithril axe" are different facts with different remedies, and a single
// map would let one source's shortage silently veto the other.
type AxeUnavailable struct {
	until map[string]time.Time
}

func axeKey(axe string) string { return strings.ToLower(strings.TrimSpace(axe)) }

// Mark records that axe cannot be had from this source until now+forHow. A non-positive
// duration uses DefaultUnavailableFor, so a caller cannot accidentally record a "no" that has
// already lapsed.
func (m *AxeUnavailable) Mark(axe string, now time.Time, forHow time.Duration) {
	if m == nil {
		return
	}
	key := axeKey(axe)
	if key == "" {
		return
	}
	if forHow <= 0 {
		forHow = DefaultUnavailableFor
	}
	if m.until == nil {
		m.until = map[string]time.Time{}
	}
	// Never shorten a standing "no". Two refusals in a row are not evidence that the second
	// one expires sooner.
	at := now.Add(forHow)
	if was, seen := m.until[key]; seen && was.After(at) {
		return
	}
	m.until[key] = at
}

// Unavailable reports whether axe is currently known to be unavailable from this source.
func (m *AxeUnavailable) Unavailable(axe string, now time.Time) bool {
	if m == nil || len(m.until) == 0 {
		return false
	}
	at, seen := m.until[axeKey(axe)]
	return seen && now.Before(at)
}

// Clear forgets any standing "no" for axe. Call it when one actually arrives from this source:
// a mule that has just handed over a mithril axe plainly has them again.
func (m *AxeUnavailable) Clear(axe string) {
	if m == nil || len(m.until) == 0 {
		return
	}
	delete(m.until, axeKey(axe))
}

// Skip returns a predicate for AxeTargets, fixed at now so one decision cannot see a memory
// lapse half way through. Safe on a nil receiver.
func (m *AxeUnavailable) Skip(now time.Time) func(axe string) bool {
	return func(axe string) bool { return m.Unavailable(axe, now) }
}

// Available filters axes down to the ones not currently refused, keeping order.
func (m *AxeUnavailable) Available(axes []string, now time.Time) []string {
	out := make([]string, 0, len(axes))
	for _, a := range axes {
		if m.Unavailable(a, now) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// ---- reading a refusal ----

// MuleDeclineIsEmptyStock reports whether a mule's decline reason means it has none of the
// item, as opposed to any of the other reasons an order is refused.
//
// The wording is orderplan.Decide's own, for the item-stock branch it takes after really
// reading its bank. Matched by substring rather than equality because a Reason is truncated at
// mulebus.ReasonMax (80) on the wire.
//
// Deliberately NOT a re-export of anything in orderplan, and deliberately narrow: the other
// declines must not land here. "you are not in sight of me" says nothing about stock (that is
// orderplan.DeclineIsPositional, and the live incident behind it is written up in
// woodcutterhandout.go), a window cap is temporary by nature, and "cannot read my own stock" is
// the mule admitting it does not know. Remembering any of those as "there is no such axe" would
// be exactly the lie this file was written to remove -- one in the other direction.
//
// If orderplan's wording ever drifts, this returns false and the account falls back to the
// blind ladder it used before. That is a lost optimisation, not a breakage, and it is why this
// is a substring test on one specific phrase rather than a guess at a family of them.
func MuleDeclineIsEmptyStock(reason string) bool {
	return strings.Contains(strings.ToLower(reason), "none in the bank")
}

// MarketRefusalIsStock reports whether a woodcutrunner.MarketBuyResult.Reason means the stall
// genuinely cannot supply that item, rather than the trip having failed for some other reason.
//
// Only two of MarketBuyResult's twenty-odd reasons are about stock:
//
//   - "<seller> said it cannot sell one (...)" -- the stall answered in public chat, which
//     woodcutrunner.awaitSellerReply already reads and already calls sellerRefused.
//   - "the seller never offered it" -- the trade opened, our coins went up, and the item never
//     appeared on their side. That is the same fact arrived at silently.
//
// Everything else must NOT be remembered as a shortage, and the ones that would do real harm
// are worth naming: "not carrying the coins to pay for it" and "the price is over the
// configured budget" are about THIS account, "could not reach the market" is about walking, and
// "nobody at the market answered" is an absent stall that will be back. Marking any of them
// would teach the account that an axe it can plainly buy does not exist.
func MarketRefusalIsStock(reason string) bool {
	r := strings.ToLower(reason)
	return strings.Contains(r, "said it cannot sell one") || strings.Contains(r, "the seller never offered it")
}

// ---- laddering a purchase down to a tier that can actually be bought ----

// AxeTargets is the ladder of axes worth trying to BUY, best first: the wanted axe, then every
// cheaper tier that has a known price, skipping any a caller's memory says is unavailable.
//
// This is the fallback woodcutteraxe.go's own comment claimed and DecideAxeBuy did not have.
// Measured need for it: `AxeWanted` defaults to a rune axe, the Seers market's rune-axe stock
// was ZERO when this was written, and a mithril axe was 600 coins and nine deep. Without a
// ladder the account asks for the one tier forever.
//
// Two rules about which tiers get in, and both are about not spending blind:
//
//   - The WANTED tier is always included (unless skipped), priced or not. That keeps
//     DecideAxeBuy's existing contract exactly -- an unpriced want still comes back as a
//     market target with Priced false, and the caller still refuses to pay for it -- so this
//     is additive rather than a behaviour change for anyone who has not configured prices.
//   - A CHEAPER tier is included only when priceOf knows its price. An unpriced tier has no
//     safe offer to make, and woodcutrunner.BuyAtMarket refuses a zero price anyway. In
//     practice this is also what keeps the ladder honest about the mule: steel, iron, bronze
//     and black are unpriced by default precisely because orderplan.AxeHandouts gives those
//     away free, so buying one is never the right move.
//
// Nothing above the wanted tier is ever included. "Better than asked for" is not this
// function's call to make, and an operator's target is a budget statement as much as a
// preference.
func AxeTargets(want string, priceOf func(axe string) (int, bool), skip func(axe string) bool) []string {
	want = canonicalAxe(strings.TrimSpace(want))
	if want == "" || !IsAxe(want) {
		return nil
	}
	top := axeIndex(want)
	out := make([]string, 0, top+1)
	for i := top; i >= 0; i-- {
		a := Axes[i]
		if skip != nil && skip(a) {
			continue
		}
		if i == top {
			out = append(out, a)
			continue
		}
		if priceOf == nil {
			continue
		}
		if p, ok := priceOf(a); !ok || p <= 0 {
			continue
		}
		out = append(out, a)
	}
	return out
}

// DecideAxeBuyAmong is DecideAxeBuy over a ladder of acceptable targets, best first: the first
// target that produces something to do wins, and the best target's own answer is returned when
// none of them does.
//
// The ladder terminates itself on the axe already in hand, which is the property that makes it
// safe to hand a long list. An account holding a mithril axe, wanting a rune axe, with rune and
// adamant both refused, has targets [Mithril axe] left and gets back "already holding a mithril
// axe" -- not a pointless purchase of what it is already using.
func DecideAxeBuyAmong(held, banked, targets []string, coins int, priceOf func(axe string) (int, bool)) AxeBuy {
	if len(targets) == 0 {
		return AxeBuy{Why: "no axe target configured"}
	}
	var first AxeBuy
	for i, want := range targets {
		buy := DecideAxeBuy(held, banked, want, coins, priceOf)
		if i == 0 {
			first = buy
		}
		if buy.Want != "" {
			return buy
		}
	}
	return first
}
