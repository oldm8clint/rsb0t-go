package bot

// RuneRunner's shortage priority and mule purse (runerunnerplan/shortage.go, runerunnerplan/topup.go).

import (
	"fmt"
	"time"

	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
)

// runeRunnerShortageEvery bounds how often an unchanged shortage line is logged.
const runeRunnerShortageEvery = 30 * time.Minute

// rememberBase keeps the runes and caps exactly as the settings wrote them, so every refresh
// plans from them and a mule that has recovered gives back the original priority and caps.
func (b *runeRunnerBot) rememberBase() {
	b.baseRunes = append([]string(nil), b.set.RuneList...)
	b.baseCaps = runerunnerplan.Caps{}
	for k, v := range b.set.CapTable {
		b.baseCaps[k] = v
	}
}

// refreshShortage sets this run's priority order and caps from what the mule's published bank
// holds (runerunnerplan.PlanShortage): the scarce runes it is out of first and up to the price
// ceiling. A mule that published nothing, or shortageFirst off, leaves the settings' own.
func (b *runeRunnerBot) refreshShortage(ctx *Context) {
	if b.baseRunes == nil {
		b.rememberBase()
	}
	if !b.set.ShortageFirst {
		return
	}
	plan := runerunnerplan.PlanShortage(runerunnerplan.ShortageInput{
		Runes: b.baseRunes, Caps: b.baseCaps, Mule: muleBankCount(ctx, b.set.Mule),
	})
	b.set.RuneList, b.set.CapTable = plan.Runes, plan.Caps
	b.muleShort = len(plan.Short) > 0
	line := plan.String()
	if line == b.shortageLine && time.Since(b.shortageAt) < runeRunnerShortageEvery {
		return
	}
	b.shortageLine, b.shortageAt = line, time.Now()
	if len(plan.Short) == 0 {
		b.logf(ctx, "shortage: %s", line)
		return
	}
	b.logf(ctx, "shortage: the mule is short of %s -- bought first and up to the price ceiling; order now %v", line, plan.Runes)
}

// purseStation is the rotation's top-up alone, for a circuit that starts at Lundail and so has no
// draw station: at the mule's post it asks the mule for coins when bank plus pack is under
// topUpBelow (mulePurse), banks every coin and draws none -- Gundai draws from this bank inside
// the Mage Bank, and Aubury's and Betty's stations from the nearest bank's own top-up.
func (b *runeRunnerBot) purseStation(ctx *Context, d *fleetrunner.Deps, s runerunnerplan.Station) {
	// The whole round's purse: Lundail is first on this circuit, and the bank pays it at Gundai.
	rn := b.roundNeed(ctx, b.st.Cursor+1)
	below, amount := b.purseAsk(rn, b.st.BankCoins+fleetrunner.CoinsHeld(d))
	need, why := runerunnerplan.PurseNeeded(below, amount, b.st.BankCoins, b.st.BankReadAt, fleetrunner.CoinsHeld(d), time.Now())
	if !need {
		b.finish(ctx, skipped("purse: %s", why))
		return
	}
	if m, known := ctx.MuleWorld(b.set.Mule); known && m != ctx.World() {
		// The mule answers the wall bus only on its own world; the next round's delivery hops back.
		b.finish(ctx, skipped("purse: on world %d; the mule is on world %d", ctx.World(), m))
		return
	}
	post := b.mulePost(ctx)
	if !runerunner.WalkTries(d, s.Key(), post, 4, runerunner.ShopWalkTimeout, 3) {
		b.stuck(ctx, fmt.Sprintf("stuck: could not reach the bank at %d,%d from %s", post.X, post.Z, fleetrunner.HereIs(d)))
		return
	}
	bank, _, ok := runerunner.OpenBankAt(d, post)
	if !ok {
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeAborted, Reason: "the bank would not open"})
		return
	}
	runerunner.DepositExcept(d, bank, []string{runerunner.CoinsName})
	read := runerunner.ReadBank(d, bank)
	b.st.NoteBank(read.Runes, read.Coins, time.Now())
	bank, read, ok = b.mulePurse(ctx, d, bank, read, post, rn)
	if !ok {
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeAborted, Reason: "the bank would not reopen after the mule's top-up"})
		return
	}
	// Whatever coins the pack still holds go in too, so Gundai (and the shop banks) see them.
	runerunner.DepositExcept(d, bank, nil)
	read = runerunner.ReadBank(d, bank)
	b.st.NoteBank(read.Runes, read.Coins, time.Now())
	runerunner.CloseBank(d, bank)
	b.st.BankCoins = read.Coins
	b.logf(ctx, "purse: bank holds %d coins at %d,%d", read.Coins, post.X, post.Z)
	b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeDrew, Reason: fmt.Sprintf("purse: bank holds %d coins", read.Coins)})
}
