package bot

// Rune axes from the mule: the upgrade ask and the broken-axe replacement.
//
// THE OWNER'S INSTRUCTION, 2026-10-04: the mule holds 45 Rune axes and 222 Knives for the
// ProgressiveWCFletch cohort. Until now the mule was only ever asked for an axe by an account that
// held NONE (orderStep), because its catalogue stopped at mithril and an axe-holder asking for an
// upgrade it could not have was the 2026-09-21 loop (twelve accounts, 104-tile round trips, zero
// experience). Rune and adamant are on the catalogue now (orderplan.AxeHandouts), so two more asks
// make sense, each bounded so that loop cannot come back:
//
//   - THE UPGRADE: an account chopping with a worse axe asks for a better one, but ONLY when the
//     mule's published bank shows one (known stock, never the blind ladder), at most once per
//     axeRetry, and only for axes strictly better than the one in hand.
//   - THE BROKEN AXE: an Ent broke the axe (content's macro_event_damage_axe swaps it for a
//     "Broken axe" after the account chopped the Ent seven times). Bob in Lumbridge repairs it, but
//     from Seers that is two boat fares each way and a route that failed live (acct144,
//     2026-09-24: "could not reach Bob's Axes"). The mule post at Catherby is one walk. So the mule
//     is asked first for an axe at least as good as the repaired one would be, and Bob's stays the
//     fallback when the mule has none or says no.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// axesBetterThan keeps the axes strictly better than floor.
func axesBetterThan(floor string) func(string) bool {
	return func(axe string) bool { return woodcutplan.BetterAxe(axe, floor) }
}

// axesAtLeast keeps the axes at least as good as min.
func axesAtLeast(min string) func(string) bool {
	return func(axe string) bool { return !woodcutplan.BetterAxe(min, axe) }
}

// muleUpgradeAvailable reports whether the mule's PUBLISHED bank holds an axe better than held,
// and an ask is not cooling down. Known stock only: an upgrade is optional, and asking blind is
// what walked twelve accounts in circles on 2026-09-21.
func (b *woodcutterBot) muleUpgradeAvailable(ctx *Context, held string) bool {
	if held == "" || !b.askMuleForAnAxeReady() {
		return false
	}
	ask, known := b.muleAxeLadderWhere(ctx, axesBetterThan(held))
	return known && len(ask) > 0
}

// fetchAxeFromMule walks to the mule's post and asks for an axe keep accepts, best first. True
// only once one is in the pack.
func (b *woodcutterBot) fetchAxeFromMule(ctx *Context, d *woodcutrunner.Deps, keep func(string) bool, why string) bool {
	stand := b.settings.MuleStand
	if tile.Chebyshev(currentTileOf(d), stand) > muleMeetRadius {
		ctx.logf("woodcutter: %s — walking to %q's post at %d,%d", why, b.settings.Mule, stand.X, stand.Z)
		// TravelTo, as orderStep uses: Seers to Catherby is a cross-region leg.
		if !woodcutrunner.TravelTo(d, stand, muleMeetRadius) {
			b.warn(ctx, "mule-walk", 60*time.Second, "could not reach %q's post at %d,%d to be handed an axe", b.settings.Mule, stand.X, stand.Z)
			return false
		}
	}
	return b.askMuleForAnAxeWhere(ctx, d, keep)
}

// upgradeFromMule is orderStep's ask for an account that already holds an axe. False leaves the
// market path to run as before, and holds the next upgrade attempt off for axeRetry.
func (b *woodcutterBot) upgradeFromMule(ctx *Context, d *woodcutrunner.Deps) bool {
	held := woodcutplan.BestAxe(woodcutrunner.HeldAxes(d))
	if !b.muleUpgradeAvailable(ctx, held) {
		return false
	}
	if b.fetchAxeFromMule(ctx, d, axesBetterThan(held), fmt.Sprintf("%q holds a better axe than this %s", b.settings.Mule, strings.ToLower(held))) {
		ctx.logf("woodcutter: upgraded from a %s with the mule's axe — now holding %v", strings.ToLower(held), woodcutrunner.HeldAxes(d))
		return true
	}
	b.axeNext = time.Now().Add(axeRetry)
	return false
}

// replaceBrokenAxeFromMule asks the mule for an axe at least as good as the broken one would be
// once repaired. False sends the caller on to Bob's, exactly as before.
func (b *woodcutterBot) replaceBrokenAxeFromMule(ctx *Context, d *woodcutrunner.Deps) bool {
	_, repaired, held := woodcutrunner.BrokenAxeHeld(d)
	if !held || repaired == "" || !b.askMuleForAnAxeReady() {
		return false
	}
	keep := axesAtLeast(repaired)
	// Blind is allowed here, unlike the upgrade: the alternative is the trip to Lumbridge, and an
	// account with a broken axe and nothing better is not chopping at all.
	if ask, _ := b.muleAxeLadderWhere(ctx, keep); len(ask) == 0 {
		return false
	}
	why := fmt.Sprintf("an Ent broke the %s; asking %q for one rather than going to Bob's", strings.ToLower(repaired), b.settings.Mule)
	if !b.fetchAxeFromMule(ctx, d, keep, why) {
		return false
	}
	b.brokenReplaced++
	ctx.logf("woodcutter: replaced the broken %s from %q — now holding %v", strings.ToLower(repaired), b.settings.Mule, woodcutrunner.HeldAxes(d))
	return true
}
