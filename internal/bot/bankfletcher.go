package bot

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/bankfletchplan"
	"rsb0t-go/internal/scripts/bankfletchrunner"
)

func init() {
	Register("BankFletcher", func() Bot { return &bankFletchBot{} })
}

// bankFletchBot ports rsb0t's public-catalogue BankFletcher.ts (the TypeScript reference client,
// src/bot/scripts/BankFletcher/BankFletcher.ts) to this codebase's own three-layer recipe — see
// docs/script_porting_framework.md. This first port covers "cut" mode only (Logs + Knife ->
// Arrow shafts via the make menu) — see bankfletchplan's own doc comment for exactly what
// "String"/"cut+string"/"attach" modes are not ported here, and why.
//
// No mule: confirmed by reading the whole TS source before writing this — BankFletcher.ts
// imports no donorPlan/muleProtocol at all.
//
// Settings, all optional (read via bankfletchplan.Settings):
//
//	material  string  default "Logs"    — the log item fletched; any name accepted, but only
//	                                       plain Logs is proven live by this port.
//	logDetail string  default "Normal"  — "Verbose" logs every stage change.
type bankFletchBot struct {
	settings bankfletchplan.Settings

	stage     string
	fletched  int
	trips     int
	startedAt time.Time
	warnedAt  map[string]time.Time
}

func (b *bankFletchBot) deps(ctx *Context) *bankfletchrunner.Deps {
	return &bankfletchrunner.Deps{
		Session:    ctx.Session,
		Pack:       ctx.Pack,
		Locs:       ctx.Locs,
		Config:     ctx.Config,
		Components: ctx.Components,
		Chat:       ctx.Chat,
		Delay:      ctx.Delay,
		Log:        ctx.Log,
		// What each batch makes, for the Loot panel's Fletching card (bankfletchrunner/
		// fletchcount.go). Reporting only.
		Loot: ctx.Loot,
	}
}

func (b *bankFletchBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("bankfletch: no collision pack configured (fleet.config.json needs a packPath) — refusing to start rather than panic later inside pathfinding")
	}
	b.settings = bankfletchplan.Settings{
		Material:  StringSetting(ctx.Settings, "material", bankfletchplan.Logs),
		LogDetail: StringSetting(ctx.Settings, "logDetail", "Normal"),
	}.Normalised()
	b.warnedAt = map[string]time.Time{}
	b.startedAt = time.Now()
	ctx.logf("bankfletch: cutting %s into %s at %d,%d", b.settings.Material, bankfletchplan.ArrowShaft, bankfletchplan.BankStand.X, bankfletchplan.BankStand.Z)
	return nil
}

func (b *bankFletchBot) vlog(ctx *Context, format string, args ...any) {
	if b.settings.Verbose() {
		ctx.logf("bankfletch: "+format, args...)
	}
}

func (b *bankFletchBot) warn(ctx *Context, key string, everyMs time.Duration, format string, args ...any) {
	if time.Since(b.warnedAt[key]) < everyMs {
		return
	}
	b.warnedAt[key] = time.Now()
	ctx.logf("bankfletch: WARNING: "+format, args...)
}

func (b *bankFletchBot) setStage(ctx *Context, stage string) {
	if stage != b.stage {
		b.stage = stage
		b.vlog(ctx, "-> %s", stage)
	}
}

func (b *bankFletchBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !bankfletchrunner.WaitForGuardian(d) {
		return nil
	}
	if ctx.Session.State().IsDead() {
		if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
			ctx.Session.State().ClearDead()
		}
	}
	dlg := actions.NewChatDialog(ctx.Session, ctx.Components)
	if dlg.CanContinue() && !dlg.IsMakeMenu() {
		_ = dlg.Continue()
		ctx.Delay(300 * time.Millisecond)
		return nil
	}

	stage := bankfletchplan.DecideStage(bankfletchplan.Snapshot{
		HasKnife: bankfletchrunner.HasKnife(d),
		HasLogs:  bankfletchrunner.HasLogs(d, b.settings.Material),
	})
	b.setStage(ctx, string(stage))
	switch stage {
	case bankfletchplan.StageBank:
		b.bankStep(ctx, d)
	case bankfletchplan.StageFletch:
		b.fletchStep(ctx, d)
	}
	return nil
}

func (b *bankFletchBot) OnStop(ctx *Context) {
	ctx.logf("bankfletch: stopped — %d fletch batch(es) made in %d bank trips", b.fletched, b.trips)
}

func (b *bankFletchBot) bankStep(ctx *Context, d *bankfletchrunner.Deps) {
	bank, ok := bankfletchrunner.OpenBank(d)
	if !ok {
		b.warn(ctx, "bank-open", 60*time.Second, "could not reach or open the bank — retrying")
		ctx.Delay(3 * time.Second)
		return
	}
	// Bank arrow shafts and anything else made, keep the knife.
	bankfletchrunner.DepositAllExcept(d, bank, []string{bankfletchplan.Knife})
	if !bankfletchrunner.HasKnife(d) {
		if !bankfletchrunner.WithdrawOneOf(d, bank, bankfletchplan.Knife) {
			_ = bank.Close()
			b.warn(ctx, "no-knife", 60*time.Second, "no %s in the bank — put one in and the fletcher resumes", bankfletchplan.Knife)
			ctx.Delay(3 * time.Second)
			return
		}
	}
	if !bankfletchrunner.HasLogs(d, b.settings.Material) {
		if !bankfletchrunner.WithdrawLoad(d, bank, b.settings.Material) {
			_ = bank.Close()
			b.warn(ctx, "no-logs", 60*time.Second, "no %s in the bank — put some in and the fletcher resumes", b.settings.Material)
			ctx.Delay(3 * time.Second)
			return
		}
	}
	_ = bank.Close()
	b.trips++
	ctx.logf("bankfletch: bank trip %d — restocked knife/%s", b.trips, b.settings.Material)
}

func (b *bankFletchBot) fletchStep(ctx *Context, d *bankfletchrunner.Deps) {
	res := bankfletchrunner.Fletch(d, b.settings.Material)
	switch res.Reason {
	case "made":
		b.fletched++
		b.vlog(ctx, "fletched a batch of %s (batch %d)", bankfletchplan.ArrowShaft, b.fletched)
	case "no-menu":
		b.warn(ctx, "no-menu", 60*time.Second, "the make menu did not open — retrying")
		ctx.Delay(1 * time.Second)
	case "use-failed":
		b.warn(ctx, "use-failed", 60*time.Second, "could not use the knife on the %s — retrying", b.settings.Material)
		ctx.Delay(2 * time.Second)
	case "no-knife", "no-logs", "stopped":
		// The next Loop call re-derives the stage from live state, but only something changing
		// can make that useful: every one of these reasons leaves the stage exactly as it was.
		// Back off rather than spin — with no delay here a single refused use produced 26.9
		// million log lines in one live run.
		ctx.Delay(1 * time.Second)
	}
}
