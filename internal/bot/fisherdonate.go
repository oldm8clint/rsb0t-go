package bot

// ProgressiveFisher's side of a mule RESTOCK: hearing the mule say it is running out of coins,
// offering what this account can genuinely spare, and — only if the mule sends for it — walking
// over with it.
//
// Kept in its own file with all of its state on ONE field of fisherBot, exactly as
// fisherhandout.go is and for the same reason it gives: the main session is working in
// internal/bot/fisher.go, so this is an additive hook (one field, one call site) rather than
// edits scattered through a file someone else is mid-change in. See COLLAB.md.
//
// WHY THE FISHER IS THE DONOR. The coins the mule hands out as axe money are the coins the
// FISHING half of the fleet earned and handed over in the first place — casket loot, pearls,
// gems and sold fish. A woodcutter drawing 150,000 for a rune axe is spending a fisher's take.
// So when the mule runs short, the fishers are where the money is, and this closes the loop
// without inventing any new source of coins.
//
// THREE THINGS THAT MAKE THIS SAFE TO LEAVE ON, each of which is a rule and not a hope:
//
//   - It only ever answers while the account is FISHING NORMALLY. Not while funding, buying,
//     selling, travelling, mid-hand-off or mid-random-event — a fisher in any of those states
//     either needs its coins or is already busy, and restockplan.DonorState.Busy is what says
//     so. This is why Earmarked can honestly be 0 here: the one phase this runs in is the phase
//     where the account has its tool, has nothing queued to buy, and its coins are surplus.
//   - It keeps restockplan.DonorReserveCoins back, always. See that constant: it is four times
//     fisherplan.DeathFloatCoins, the float this project already sized for a full death
//     recovery, and a test pins it at or above that number so it cannot drift below.
//   - It is bounded and reversible. restockrunner.Donate returns within its own timeouts, puts
//     anything that did not cross back in the bank, and the cooldown then leaves this account
//     alone for restockplan.AnswerCooldown. A donation must never be able to make a working
//     account stop working.

import (
	"time"

	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/fisherrunner"
	"rsb0t-go/internal/scripts/restockplan"
	"rsb0t-go/internal/scripts/restockrunner"
)

// fisherDonor is the whole feature's state on the fisher, on one field of fisherBot.
type fisherDonor struct {
	answerer *restockrunner.Answerer
	trips    int
	given    int
}

// setUpDonating is called from OnStart. Off by default, like the mule's own half: this moves
// real wealth between real accounts, and an operator turns it on deliberately
// (`"donateCoins": true`) rather than discovering it running.
func (b *fisherBot) setUpDonating(ctx *Context) {
	if !BoolSetting(ctx.Settings, "donateCoins", false) {
		return
	}
	if b.mule == "" || ctx.Bus == nil {
		ctx.logf("fisher: donateCoins is on but there is no mule name or no wall bus — staying off")
		return
	}
	b.donor = &fisherDonor{answerer: restockrunner.NewAnswerer(b.mule)}
	ctx.logf("fisher: donateCoins ON — will answer %q if it runs low, keeping %d coins back and only offering %d or more",
		b.mule, restockplan.DonorReserveCoins, restockplan.MinDonationCoins)
}

// donateStep is the one call site in fisherBot.Loop. It returns true when it actually spent the
// Loop call on a donation, so the caller hands control straight back rather than also running a
// fishing phase in the same pass.
//
// bankedCoins is this account's own last real bank reading of its coin row; a caller that has
// never opened its bank passes 0, which pledges nothing rather than guessing.
func (b *fisherBot) donateStep(ctx *Context, d *fisherrunner.Deps, phase fisherplan.FishPhase, bankedCoins int) bool {
	dn := b.donor
	if dn == nil || ctx.Bus == nil {
		return false
	}
	now := time.Now()
	state := restockplan.DonorState{
		Banked: bankedCoins,
		Pocket: fisherrunner.Coins(d),
		// Zero, and this is the one number in the whole feature that could be dangerously
		// wrong if it were guessed (see restockplan.Spare's own doc comment). It is honest
		// here for one specific reason: Busy below is true for every phase in which this
		// account has a purchase pending — PhaseBuy, PhaseSell, PhaseFund, PhaseStore — so
		// the only phase that reaches the arithmetic is the one where nothing is earmarked.
		Earmarked: 0,
		// Anything that is not ordinary fishing is busy. A hand-off already in flight counts
		// too: donorrunner can only drive one trade at a time.
		Busy: phase != fisherplan.PhaseFish || b.handoffPending,
	}
	call, called, why := dn.answerer.Poll(now, ctx.Bus, ctx.Username, state)
	if why != "" {
		ctx.logf("fisher: restock: %s", why)
	}
	if !called {
		return false
	}
	ctx.logf("fisher: restock: %q needs %d coins and has sent for me — heading over with them", call.Mule, call.Qty)
	res := restockrunner.Donate(&restockrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		Bus: ctx.Bus, Me: ctx.Username,
	}, restockrunner.DonateOptions{
		Mule: call.Mule, Post: call.Post, BankStand: b.layout.Bank,
	})
	// The cooldown starts on ANY terminal outcome, including a failure: the trip has been
	// spent either way, and an account that immediately tries again is how a crowd at the
	// mule's post sustains itself (see the hand-off jitter measurement in fisher.go).
	dn.answerer.Donated(time.Now())
	dn.trips++
	if res.Given > 0 {
		dn.given += res.Given
		ctx.logf("fisher: restock: donated %d coins to %q (%d across %d trip(s) this run); %d put back in the bank",
			res.Given, call.Mule, dn.given, dn.trips, res.Returned)
	} else {
		ctx.logf("fisher: restock: donated nothing to %q — %s", call.Mule, res.Reason)
	}
	return true
}
