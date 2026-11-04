package bot

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/flaxplan"
	"rsb0t-go/internal/scripts/flaxrunner"
)

func init() {
	Register("FlaxPicker", func() Bot { return &flaxBot{} })
}

// flaxBot ports rsb0t's public-catalogue FlaxPicker.ts (the TypeScript reference client,
// src/bot/scripts/FlaxPicker/FlaxPicker.ts) to this codebase's own three-layer recipe — see
// docs/script_porting_framework.md. Every Loop call re-derives the account's stage from live
// state alone (flaxplan.DecideStage), the same shape internal/bot/essence.go and
// internal/bot/woodcutter.go already proved: nothing is remembered across calls except the
// small bit of state that must survive one to the next (startupPending, the running counters).
//
// This is the simplest script ported so far from the public catalogue: no mule (the TS source
// has none — confirmed by reading the whole file before writing this, the same check every
// no-mule port in this codebase already documents), no combat, no NPC beyond the bank, no shop.
// Settings, all optional (read via flaxplan.Settings):
//
//	flaxName  string  default "Flax"        — the loc/item name picked.
//	pickOp    string  default "Pick"        — the loc option used to pick it.
//	bankBooth string  default "Bank booth"  — the bank loc's display name.
//	logDetail string  default "Normal"      — "Verbose" logs every stage change.
//
// Deliberately not ported (see flaxrunner's own package doc comment): the paint/status overlay
// and EscapeFlaxTrap's carving recovery.
type flaxBot struct {
	settings flaxplan.Settings

	startupPending bool

	stage     string
	picked    int
	banked    int
	trips     int
	startedAt time.Time
	warnedAt  map[string]time.Time
}

func (b *flaxBot) deps(ctx *Context) *flaxrunner.Deps {
	return &flaxrunner.Deps{
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

func (b *flaxBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("flax: no collision pack configured (fleet.config.json needs a packPath) — refusing to start rather than panic later inside pathfinding")
	}
	b.settings = flaxplan.Settings{
		FlaxName:  StringSetting(ctx.Settings, "flaxName", flaxplan.Flax),
		PickOp:    StringSetting(ctx.Settings, "pickOp", flaxplan.PickOp),
		BankBooth: StringSetting(ctx.Settings, "bankBooth", flaxplan.BoothName),
		LogDetail: StringSetting(ctx.Settings, "logDetail", "Normal"),
	}.Normalised()
	b.startupPending = true
	b.warnedAt = map[string]time.Time{}
	b.startedAt = time.Now()
	ctx.logf("flax: picking %q at %d,%d, banking at %d,%d", b.settings.FlaxName, flaxplan.FieldTile.X, flaxplan.FieldTile.Z, flaxplan.BankStand.X, flaxplan.BankStand.Z)
	return nil
}

func (b *flaxBot) vlog(ctx *Context, format string, args ...any) {
	if b.settings.Verbose() {
		ctx.logf("flax: "+format, args...)
	}
}

func (b *flaxBot) warn(ctx *Context, key string, everyMs time.Duration, format string, args ...any) {
	if time.Since(b.warnedAt[key]) < everyMs {
		return
	}
	b.warnedAt[key] = time.Now()
	ctx.logf("flax: WARNING: "+format, args...)
}

func (b *flaxBot) setStage(ctx *Context, stage string) {
	if stage != b.stage {
		b.stage = stage
		b.vlog(ctx, "-> %s", stage)
	}
}

func (b *flaxBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !flaxrunner.WaitForGuardian(d) {
		return nil
	}
	// A death here just drops the account near Lumbridge; the ordinary bank/travel dispatch
	// resumes it the same way essenceBot/fisherBot's own doc comments describe — no bespoke
	// recovery path needed. Still clear the one-way dead latch so a dashboard does not show
	// dead=true forever after a single death several minutes ago.
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

	stage := flaxplan.DecideStage(flaxplan.Snapshot{
		PackFull:       flaxrunner.PackFull(d),
		Haul:           flaxrunner.Haul(d),
		StartupPending: b.startupPending,
		FlaxNearby:     flaxrunner.FlaxNearby(d, b.settings.FlaxName, b.settings.PickOp),
	})
	b.setStage(ctx, string(stage))
	switch stage {
	case flaxplan.StagePick:
		b.pickStep(ctx, d)
	case flaxplan.StageBank:
		b.bankStep(ctx, d)
	case flaxplan.StageTravel:
		if !flaxrunner.WalkNear(d, flaxplan.FieldTile, flaxplan.FieldArriveRadius) {
			b.warn(ctx, "travel", 60*time.Second, "could not reach the flax field — retrying")
		}
	}
	return nil
}

func (b *flaxBot) OnStop(ctx *Context) {
	ctx.logf("flax: stopped — %d flax picked, %d banked in %d trips", b.picked, b.banked, b.trips)
}

func (b *flaxBot) pickStep(ctx *Context, d *flaxrunner.Deps) {
	res := flaxrunner.Pick(d, b.settings.FlaxName, b.settings.PickOp)
	b.picked += res.Gained
	switch res.Reason {
	case "no-flax":
		b.warn(ctx, "flax", 60*time.Second, "no %s in sight — waiting", b.settings.FlaxName)
		b.pauseCtx(ctx, 2*time.Second)
	case "stalled":
		b.vlog(ctx, "no %s for a while — moving on", b.settings.FlaxName)
	case "full":
		b.vlog(ctx, "pack full (%d %s)", flaxrunner.FlaxHeld(d, b.settings.FlaxName), b.settings.FlaxName)
	case "dialog", "stopped":
		// Nothing to do — the next Loop call re-derives the stage from live state.
	}
}

func (b *flaxBot) bankStep(ctx *Context, d *flaxrunner.Deps) {
	n := flaxrunner.FlaxHeld(d, b.settings.FlaxName)
	bank, ok := flaxrunner.OpenBank(d, b.settings.BankBooth)
	if !ok {
		b.warn(ctx, "bank-open", 60*time.Second, "could not reach or open the bank — retrying")
		b.pauseCtx(ctx, 3*time.Second)
		return
	}
	deposited := flaxrunner.DepositAll(d, bank)
	_ = bank.Close()
	if b.startupPending {
		b.startupPending = false
	}
	if deposited > 0 {
		b.trips++
		b.banked += n
		ctx.logf("flax: banked %d %s (trip %d)", n, b.settings.FlaxName, b.trips)
	}
}

func (b *flaxBot) pauseCtx(ctx *Context, d time.Duration) {
	ctx.Delay(d)
}
