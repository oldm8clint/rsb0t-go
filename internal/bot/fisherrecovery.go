package bot

// ProgressiveFisher's recovery from a lost tool without camping Harry's counter.
//
// Live 2026-09-20 (build c36f809): a whirlpool deleted 72 Big fishing nets in five seconds.
// Harry stocks five and restocks one a minute (content catherby.inv, big_net,5,100), so 73
// accounts spent the next quarter-hour at his counter logging "Harry would not sell a Big
// fishing net ... waiting for his restock" every five seconds; 55 of them never got one before
// the restart. One (acct84) was fail-teleported to Brimhaven when a Genie it ignored between
// shop clicks timed out, and died walking home.
//
// The owner's order for a fisher short of the price (2026-09-20): coins already in the bank
// (bankStep, unchanged), banked caskets, 500 coins from the mule, and only then selling the
// catch. While Harry is out of stock the fisher fishes with the best tool it owns (or a small
// net bought instead) and comes back every few minutes, rather than standing at the shop.

import (
	"fmt"
	"math/rand"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/fisherrunner"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// shopRetryMin/shopRetryJitter space retries at an out-of-stock Harry: a few minutes, jittered
// so a crowd that lost its nets together does not come back together.
const (
	shopRetryMin    = 2 * time.Minute
	shopRetryJitter = 2 * time.Minute
)

// shopMissLimit is how many visits in a row may fail to open Harry's shop before the account
// stops trying for a while — the same back-off as out of stock.
const shopMissLimit = 3

// casketRetry is how long to leave banked caskets alone after an opening that opened none.
const casketRetry = 10 * time.Minute

// coinAskRetry is how long to leave the mule alone after a coin ask; the mule's own ledger
// allows one order per donor per 30 minutes (orderplan.DefaultPolicy) anyway.
const coinAskRetry = 10 * time.Minute

// restockWait is one idle beat while waiting out Harry's restock with nothing to fish with.
const restockWait = 15 * time.Second

// fisherRecovery is the fisher's memory of the recovery above. Zero value is "nothing tried".
type fisherRecovery struct {
	shopOutUntil  time.Time
	shopMisses    int
	casketRetryAt time.Time
	coinAskAt     time.Time
	coinAsks      int
	coinsGot      int
}

// fill adds the recovery state to a snapshot.
func (r *fisherRecovery) fill(b *fisherBot, ctx *Context, s *fisherplan.FishSnapshot) {
	now := time.Now()
	s.ShopOut = now.Before(r.shopOutUntil)
	if b.bankRead != nil && !now.Before(r.casketRetryAt) {
		s.BankCaskets = b.bankRead.Caskets
	}
	s.MuleCoinsReady = b.mule != "" && ctx.Bus != nil && !now.Before(r.coinAskAt) &&
		s.Tile != nil && fisherplan.InCatherby(*s.Tile)
}

func (r *fisherRecovery) shopOut(ctx *Context, why string) {
	wait := shopRetryMin + time.Duration(rand.Int63n(int64(shopRetryJitter)))
	r.shopOutUntil = time.Now().Add(wait)
	r.shopMisses = 0
	ctx.logf("fisher: %s — fishing with what this account owns and trying Harry again in %s", why, wait.Round(time.Second))
}

// buyStep buys the current rung's tool. With no ladder tool owned at all it also accepts a
// Small fishing net when the wanted tool is out of stock, so the account fishes while waiting.
func (b *fisherBot) buyStep(ctx *Context, d *fisherrunner.Deps, s fisherplan.FishSnapshot) error {
	rung := fisherplan.CurrentRung(s.Level, s.Target)
	// PhaseBuy is reached straight from PhaseBank, ~24 tiles from Harry's stand — too far for
	// Harry himself to be in view, so walk near the stand first (see the history in git).
	if !fisherrunner.WalkNear(d, b.layout.HarryStand, 10) {
		if b.survivalDue(ctx) {
			return nil
		}
		ctx.logf("fisher: could not walk to Harry's Fishing Shop — waiting before retrying")
		b.pause(ctx, 5*time.Second)
		return nil
	}
	tools := []string{rung.Tool}
	owned := append(append([]string{}, s.HeldTools...), s.BankTools...)
	if _, anyTool := fisherplan.ActiveRung(s.Level, s.Target, owned); !anyTool && rung.Tool != fisherplan.Rungs[0].Tool {
		tools = append(tools, fisherplan.Rungs[0].Tool)
	}
	before := fisherrunner.Coins(d)
	res := fisherrunner.BuyToolOr(d, b.layout.HarryStand, tools...)
	switch {
	case res.Yielded:
		// A random event came first; the Guardian owns the next move.
		return nil
	case res.Bought == rung.Tool:
		b.toolBuys++
		if !hasToolName(b.boughtTools, rung.Tool) {
			b.boughtTools = append(b.boughtTools, rung.Tool)
		}
		b.recover.shopMisses = 0
		ctx.logf("bought a %s for %d gp — %s from fishing %d", rung.Tool, before-fisherrunner.Coins(d), rung.Label, rung.Level)
		return nil
	case res.Bought != "":
		b.toolBuys++
		if !hasToolName(b.boughtTools, res.Bought) {
			b.boughtTools = append(b.boughtTools, res.Bought)
		}
		ctx.logf("bought a %s for %d gp to fish with while Harry has no %s", res.Bought, before-fisherrunner.Coins(d), rung.Tool)
		b.recover.shopOut(ctx, "Harry has no "+rung.Tool)
		return nil
	case res.OutOfStock:
		b.recover.shopOut(ctx, "Harry would not sell a "+rung.Tool+" (out of stock, or dearer than the coins held)")
		return nil
	}
	b.recover.shopMisses++
	if b.recover.shopMisses >= shopMissLimit {
		b.recover.shopOut(ctx, fmt.Sprintf("Harry's shop did not open %d times running", shopMissLimit))
		return nil
	}
	ctx.logf("fisher: Harry's shop did not open (%d/%d) — trying again shortly", b.recover.shopMisses, shopMissLimit)
	b.pause(ctx, 3*time.Second)
	return nil
}

// openStep opens banked caskets for their coins (fisherrunner.OpenBankedOpenables, the same
// opening the mule hand-off runs) and re-reads the bank.
func (b *fisherBot) openStep(ctx *Context, d *fisherrunner.Deps, s fisherplan.FishSnapshot) error {
	bank, ok := fisherrunner.OpenCatherbyBank(d, b.layout)
	if !ok {
		if b.survivalDue(ctx) {
			return nil
		}
		ctx.logf("fisher: could not reach or open the Catherby bank to open caskets — waiting before retrying")
		b.pause(ctx, 5*time.Second)
		return nil
	}
	rung := fisherplan.CurrentRung(s.Level, s.Target)
	keep := fisherBankKeep(fisherplan.BankKeep(true, &rung), b.surv, fisherrunner.EdibleFood(d))
	keep = append(keep, fisherplan.ToolNames()...)
	coinsBefore := fisherrunner.Coins(d) + bankCoinsOf(s)
	bank, opened, ok := fisherrunner.OpenBankedOpenables(d, bank, func() (*actions.Bank, bool) {
		return fisherrunner.OpenCatherbyBank(d, b.layout)
	}, keep)
	if !ok {
		ctx.logf("fisher: lost the bank while opening caskets — will retry")
		b.pause(ctx, 5*time.Second)
		return nil
	}
	read := fisherrunner.ReadBank(d, bank)
	b.bankRead = &read
	_ = bank.Close()
	coinsAfter := fisherrunner.Coins(d) + read.Coins
	ctx.logf("fisher: opened %d banked casket(s)/oyster(s) toward a %s: coins %d -> %d (need %d)", opened, rung.Tool, coinsBefore, coinsAfter, rung.ToolCost)
	if opened == 0 || read.Caskets > 0 {
		b.recover.casketRetryAt = time.Now().Add(casketRetry)
	}
	return nil
}

// askCoinsStep asks the mule for fisherplan.MuleCoinAsk coins over the wall bus and waits for
// the hand-out trade (donorrunner.Await: the mule's allow list, catalogue and caps decide, and
// the receipt is checked in the pack).
func (b *fisherBot) askCoinsStep(ctx *Context, d *fisherrunner.Deps, s fisherplan.FishSnapshot) error {
	b.recover.coinAskAt = time.Now().Add(coinAskRetry)
	if tile.Chebyshev(*s.Tile, b.muleStand) > 6 && !fisherrunner.WalkNear(d, b.muleStand, 4) {
		ctx.logf("fisher: could not reach the mule's post at %d,%d to ask for coins", b.muleStand.X, b.muleStand.Z)
		return nil
	}
	rung := fisherplan.CurrentRung(s.Level, s.Target)
	ctx.logf("fisher: %d coins (bank %d) for a %s at %d gp, no caskets — asking %q for %d coins",
		s.Coins, bankCoinsOf(s), rung.Tool, rung.ToolCost, b.mule, fisherplan.MuleCoinAsk)
	b.recover.coinAsks++
	before := fisherrunner.Coins(d)
	res := donorrunner.Await(&donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}, donorrunner.AwaitOptions{
		Mule: b.mule, Me: ctx.Username, Item: freshstart.Coins, Qty: fisherplan.MuleCoinAsk, Bus: ctx.Bus,
	})
	got := fisherrunner.Coins(d) - before
	if res.Got {
		b.recover.coinsGot += got
		ctx.logf("fisher: %q handed over %d coins (%d asked) — back to buying the %s", b.mule, got, fisherplan.MuleCoinAsk, rung.Tool)
		return nil
	}
	if res.Stopped {
		return nil
	}
	ctx.logf("fisher: %q did not hand over coins (%s) — next ask in %s", b.mule, res.Reason, coinAskRetry)
	return nil
}

// restockStep waits out Harry's restock when the account owns no tool at all: ask the mule for
// one (askMuleForATool, fisherhandout.go), else wait by the bank — never at the counter.
func (b *fisherBot) restockStep(ctx *Context, d *fisherrunner.Deps, s fisherplan.FishSnapshot) error {
	if s.Tile != nil && fisherplan.InCatherby(*s.Tile) && b.askMuleForATool(ctx, s) {
		return nil
	}
	if s.Tile != nil && tile.Chebyshev(*s.Tile, b.layout.Bank) > 8 {
		fisherrunner.WalkNear(d, b.layout.Bank, 6)
	}
	left := time.Until(b.recover.shopOutUntil).Round(time.Second)
	ctx.logf("fisher: no tool to fish with and Harry has none — waiting by the bank, next try at the shop in %s", left)
	b.pause(ctx, restockWait)
	return nil
}
