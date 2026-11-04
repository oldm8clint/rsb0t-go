package bot

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/vialplan"
	"rsb0t-go/internal/scripts/vialrunner"
)

func init() {
	Register("VialFiller", func() Bot { return &vialBot{} })
}

// vialBot ports rsb0t's public-catalogue VialFiller.ts (the TypeScript reference client,
// src/bot/scripts/VialFiller/VialFiller.ts + VialFillerLogic.ts) to this codebase's own
// three-layer recipe. VialFiller.ts's own loop is a fixed per-trip sequence (bank, optional
// Jatix restock, fill) rather than a live-state dispatcher, so — matching
// docs/script_porting_framework.md's own "one blocking Loop() call" rule, the same way
// essenceBot.handoffStep/fisherBot's own twin run a whole hand-off as one call — one Loop call
// here runs one whole trip: bank, maybe shop, fill, then returns.
//
// No mule: confirmed by reading the whole TS source before writing this — VialFiller.ts imports
// no donorPlan/muleProtocol at all. Every filled vial goes to its own bank, never to a mule.
//
// Settings, all optional (read via vialplan.Settings):
//
//	bank         string  default "Falador West" — or "Falador East".
//	buyVials     bool    default false           — enables the periodic Jatix restock leg.
//	buyEveryRuns int     default 5               — trips between restocks, when enabled.
//	buyQty       int     default 27              — vials bought per restock.
//	coinsPerTrip int     default 1000            — coin withdrawal target on a restock trip.
//	logDetail    string  default "Normal"        — "Verbose" logs every trip's own steps.
type vialBot struct {
	settings vialplan.Settings

	trips       int
	vialsFilled int
	vialsBought int
	startedAt   time.Time
	warnedAt    map[string]time.Time
}

func (b *vialBot) deps(ctx *Context) *vialrunner.Deps {
	return &vialrunner.Deps{
		Session:    ctx.Session,
		Pack:       ctx.Pack,
		Locs:       ctx.Locs,
		Config:     ctx.Config,
		Components: ctx.Components,
		Chat:       ctx.Chat,
		Delay:      ctx.Delay,
		Log:        ctx.Log,
	}
}

func (b *vialBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("vial: no collision pack configured (fleet.config.json needs a packPath) — refusing to start rather than panic later inside pathfinding")
	}
	b.settings = vialplan.Settings{
		Bank:         StringSetting(ctx.Settings, "bank", "Falador West"),
		BuyVials:     BoolSetting(ctx.Settings, "buyVials", false),
		BuyEveryRuns: IntSetting(ctx.Settings, "buyEveryRuns", vialplan.DefaultBuyEveryRuns),
		BuyQty:       IntSetting(ctx.Settings, "buyQty", vialplan.DefaultBuyQty),
		CoinsPerTrip: IntSetting(ctx.Settings, "coinsPerTrip", vialplan.DefaultCoinsPerTrip),
		LogDetail:    StringSetting(ctx.Settings, "logDetail", "Normal"),
	}.Normalised()
	b.warnedAt = map[string]time.Time{}
	b.startedAt = time.Now()
	shopMsg := "no restock (buyVials disabled)"
	if b.settings.BuyVials {
		shopMsg = fmt.Sprintf("restocking %d vials from Jatix every %d trips", b.settings.BuyQty, b.settings.BuyEveryRuns)
	}
	ctx.logf("vial: filling at %s bank, %s", b.settings.Bank, shopMsg)
	return nil
}

func (b *vialBot) vlog(ctx *Context, format string, args ...any) {
	if b.settings.Verbose() {
		ctx.logf("vial: "+format, args...)
	}
}

func (b *vialBot) warn(ctx *Context, key string, everyMs time.Duration, format string, args ...any) {
	if time.Since(b.warnedAt[key]) < everyMs {
		return
	}
	b.warnedAt[key] = time.Now()
	ctx.logf("vial: WARNING: "+format, args...)
}

// Loop runs one whole trip: bank, maybe restock, fill — ported from VialFiller.ts's own
// bankLeg/shopLeg/fillLeg sequence.
func (b *vialBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !vialrunner.WaitForGuardian(d) {
		return nil
	}
	if ctx.Session.State().IsDead() {
		if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
			ctx.Session.State().ClearDead()
		}
	}
	dlg := actions.NewChatDialog(ctx.Session, ctx.Components)
	if dlg.CanContinue() {
		_ = dlg.Continue()
		ctx.Delay(300 * time.Millisecond)
		return nil
	}

	bankTile := vialplan.BankTile(b.settings.Bank)
	bank, ok := vialrunner.OpenBank(d, bankTile)
	if !ok {
		b.warn(ctx, "bank-open", 60*time.Second, "could not reach or open the %s bank — retrying", b.settings.Bank)
		ctx.Delay(3 * time.Second)
		return nil
	}
	vialrunner.DepositAllExceptCoins(d, bank)
	shopDue := vialplan.IsShopRun(b.trips, b.settings.BuyVials, b.settings.BuyEveryRuns)
	if shopDue {
		bankCoins := vialrunner.BankCoins(d, bank)
		draw := vialplan.CoinsToWithdraw(bankCoins, b.settings.CoinsPerTrip)
		if draw > 0 {
			vialrunner.WithdrawCoins(d, bank, draw)
		}
	} else {
		// Ordinary trip: withdraw as many empty vials as the pack has room for — ported from
		// VialFiller.ts's own bankLeg ("quantity = free inventory slots").
		vialrunner.WithdrawEmptyVials(d, bank, vialrunner.FreeSlots(d))
	}
	_ = bank.Close()

	if shopDue {
		want := vialplan.VialsToBuy(vialrunner.FreeSlots(d), b.settings.BuyQty)
		if want > 0 {
			if vialrunner.BuyVialsFromJatix(d, want) {
				b.vialsBought += want
				ctx.logf("vial: bought %d vials from Jatix", want)
			} else {
				b.warn(ctx, "jatix", 60*time.Second, "could not buy vials from Jatix this trip")
			}
		}
	}

	if vialrunner.EmptyVials(d) == 0 {
		b.warn(ctx, "no-vials", 60*time.Second, "no empty vials to fill and none withdrawn — check the bank stock or enable buyVials")
		ctx.Delay(5 * time.Second)
		return nil
	}

	res := vialrunner.FillLeg(d)
	b.vialsFilled += res.Filled
	b.trips++
	ctx.logf("vial: trip %d complete — %d filled this trip (%d total), reason=%s", b.trips, res.Filled, b.vialsFilled, res.Reason)
	return nil
}

func (b *vialBot) OnStop(ctx *Context) {
	ctx.logf("vial: stopped — %d vials filled, %d bought, %d trips", b.vialsFilled, b.vialsBought, b.trips)
}
