package bot

import (
	"strconv"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/boneburierrunner"
	"rsb0t-go/internal/scripts/boneplan"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/tile"
)

func init() {
	Register("BoneBurier", func() Bot { return &boneBurierBot{} })
}

// boneBurierBot ports BoneBurier.ts's own loop() 1:1: every Loop call re-derives the run's
// stage from live state (boneplan.DecideStage) the same way every other ported script's stage
// machine does. What must survive one Loop call to the next is BoneBurier.ts's own private
// field list — anchor, stage, returning/finishing/clearing, idleSince, the per-drop skip list
// (skips), and the run's own counters — ported 1:1.
//
// No mule hand-off: BoneBurier.ts imports no donorPlan/muleProtocol at all — every bone it
// keeps goes to its own bank, never to a mule. Confirmed by reading the whole TS source before
// writing this, per docs/script_porting_framework.md's own instruction to check rather than
// assume.
//
// Settings (see boneplan.Settings' own doc comment for why these keys, not a TS manifest
// spelling, since BoneBurier.ts carries no settingsSchema at all — everything lives on its own
// overlay settings page):
//
//	source       string default "ground"  — "ground" or "bank".
//	names        string default "bones"   — comma-separated, matched as "contains".
//	bankNames    string default "big bones,dragon bones" — kept for the bank in split mode.
//	disposal     string default "bury"    — "bury", "bank" or "split".
//	radius       int    default 15        — 0 means the whole loaded scene.
//	buryWhenFull bool   default false     — bury as each comes up, or gather a full pack first.
//	idleMins     int    default 0         — 0 waits forever; otherwise stop (after a last bank
//	                                        trip) once nothing has been found for this long.
//	goalLevel    int    default 0         — 0 is no goal; otherwise log once when Prayer reaches it.
//	logDetail    string default "Normal"  — "Verbose" logs every stage change.
type boneBurierBot struct {
	settings boneplan.Settings

	anchor    *tile.Tile
	stage     boneplan.Stage
	stopped   string
	returning bool
	finishing bool
	clearing  bool
	idleSince *time.Time

	skips map[string]time.Time

	picked, buried, banked, trips, missed, withdrawn int
	goalAnnounced                                    bool

	startedAt time.Time
	xpAtStart int
	warnedAt  map[string]time.Time
}

func (b *boneBurierBot) deps(ctx *Context) *boneburierrunner.Deps {
	return &boneburierrunner.Deps{
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

func (b *boneBurierBot) OnStart(ctx *Context) error {
	b.settings = boneplan.Settings{
		Source:       StringSetting(ctx.Settings, "source", boneplan.DefaultSettings().Source),
		Names:        StringSetting(ctx.Settings, "names", boneplan.DefaultSettings().Names),
		BankNames:    StringSetting(ctx.Settings, "bankNames", boneplan.DefaultSettings().BankNames),
		Disposal:     StringSetting(ctx.Settings, "disposal", boneplan.DefaultSettings().Disposal),
		Radius:       IntSetting(ctx.Settings, "radius", boneplan.DefaultRadius),
		BuryWhenFull: BoolSetting(ctx.Settings, "buryWhenFull", false),
		IdleMins:     IntSetting(ctx.Settings, "idleMins", 0),
		GoalLevel:    IntSetting(ctx.Settings, "goalLevel", 0),
		LogDetail:    StringSetting(ctx.Settings, "logDetail", "Normal"),
	}.Normalised()
	b.skips = map[string]time.Time{}
	b.warnedAt = map[string]time.Time{}
	b.startedAt = time.Now()
	d := b.deps(ctx)
	b.xpAtStart = boneburierrunner.PrayerXP(d)
	if x, z, level, ok := ctx.Session.State().Tile(); ok {
		t := tile.Tile{X: x, Z: z, Level: level}
		b.anchor = &t
	}
	if len(b.settings.NameList()) == 0 {
		ctx.logf("boneburier: nothing listed to pick up — set names")
	}
	if b.settings.SourceMode() == boneplan.SourceBank {
		ctx.logf("boneburier: drawing [%s] out of the nearest bank and burying them", b.settings.Names)
	} else {
		ctx.logf("boneburier: [%s] within %d tiles, disposal %s", b.settings.Names, b.settings.Radius, b.settings.Mode())
	}
	return nil
}

func (b *boneBurierBot) vlog(ctx *Context, format string, args ...any) {
	if b.settings.Verbose() {
		ctx.logf("boneburier: "+format, args...)
	}
}

func (b *boneBurierBot) warn(ctx *Context, key string, everyMs time.Duration, format string, args ...any) {
	if time.Since(b.warnedAt[key]) < everyMs {
		return
	}
	b.warnedAt[key] = time.Now()
	ctx.logf("boneburier: WARNING: "+format, args...)
}

func (b *boneBurierBot) setStage(ctx *Context, stage boneplan.Stage) {
	if stage != b.stage {
		b.stage = stage
		b.vlog(ctx, "-> %s", boneplan.DescribeStage(stage))
	}
}

// skipped reports whether key is still parked (a drop that refused to come up), matching
// SkipList's own PICKUP_SKIP_MS window.
func (b *boneBurierBot) skipped(key string) bool {
	until, ok := b.skips[key]
	return ok && time.Now().Before(until)
}

func (b *boneBurierBot) skip(key string) {
	b.skips[key] = time.Now().Add(boneplan.PickupSkipMs * time.Millisecond)
}

func (b *boneBurierBot) clearSkip(key string) { delete(b.skips, key) }

func (b *boneBurierBot) stop(ctx *Context, reason string) {
	b.stopped = reason
	ctx.logf("boneburier: stopped: %s", reason)
}

func (b *boneBurierBot) Loop(ctx *Context) error {
	if b.stopped != "" {
		ctx.Delay(2 * time.Second)
		return nil
	}
	d := b.deps(ctx)
	if !boneburierrunner.WaitForGuardian(d) {
		return nil
	}
	if ctx.Session.State().IsDead() {
		if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
			ctx.Session.State().ClearDead()
		}
	}
	if actions.NewTrade(ctx.Session).Active() {
		ctx.Delay(2 * time.Second)
		return nil
	}
	dlg := actions.NewChatDialog(ctx.Session, ctx.Components)
	if dlg.CanContinue() {
		_ = dlg.Continue()
		ctx.Delay(300 * time.Millisecond)
		return nil
	}

	b.checkGoal(ctx, d)
	if b.anchor == nil {
		if x, z, level, ok := ctx.Session.State().Tile(); ok {
			t := tile.Tile{X: x, Z: z, Level: level}
			b.anchor = &t
		}
	}

	names, bankNames, mode := b.settings.NameList(), b.settings.BankNameList(), b.settings.Mode()
	source := b.settings.SourceMode()

	var bone gamestate.GroundItem
	var haveBone bool
	if !b.returning && source != boneplan.SourceBank && b.anchor != nil {
		if g, ok := boneburierrunner.FindBone(d, *b.anchor, b.settings.Radius, names, b.skipsSnapshot()); ok {
			bone, haveBone = g, true
		}
	}

	buryInPack := boneburierrunner.CountBound(d, boneplan.ToBury, names, bankNames, mode)
	bankInPack := boneburierrunner.CountBound(d, boneplan.ToBank, names, bankNames, mode)
	packFull := boneburierrunner.PackFull(d)
	b.clearing = boneplan.StillClearing(b.clearing, buryInPack, packFull, b.settings.BuryWhenFull)

	stage := boneplan.DecideStage(boneplan.StageState{
		BuryInPack:   buryInPack,
		BankInPack:   bankInPack,
		PackFull:     packFull,
		GroundBone:   haveBone,
		BuryWhenFull: b.settings.BuryWhenFull,
		Clearing:     b.clearing,
		Returning:    b.returning,
		ForceBank:    b.finishing,
		Source:       source,
	})
	b.setStage(ctx, stage)
	switch stage {
	case boneplan.StageCollect:
		b.idleSince = nil
		b.collectStep(ctx, d, bone)
	case boneplan.StageBury:
		b.idleSince = nil
		b.buryStep(ctx, d, names, bankNames, mode)
	case boneplan.StageBank:
		b.idleSince = nil
		b.bankStep(ctx, d, names, bankNames, mode)
	case boneplan.StageWithdraw:
		b.idleSince = nil
		b.withdrawStep(ctx, d, names)
	case boneplan.StageReturn:
		b.idleSince = nil
		b.walkBackStep(ctx, d)
	case boneplan.StageWait:
		b.idle(ctx, d)
	}
	return nil
}

func (b *boneBurierBot) OnStop(ctx *Context) {
	ctx.logf("boneburier: stopped — %d buried, %d banked over %d trips, %d picked up, %d left behind",
		b.buried, b.banked, b.trips, b.picked, b.missed)
}

func (b *boneBurierBot) skipsSnapshot() map[string]bool {
	out := make(map[string]bool, len(b.skips))
	now := time.Now()
	for k, until := range b.skips {
		if now.Before(until) {
			out[k] = true
		}
	}
	return out
}

// collectStep walks to drop and takes it, parking the drop's key in the skip list on any
// failure so the next Loop call moves on to the next bone instead of re-clicking the same one
// — ported from collect()'s own skips.skip/skips.clear calls.
func (b *boneBurierBot) collectStep(ctx *Context, d *boneburierrunner.Deps, drop gamestate.GroundItem) {
	obj, _ := ctx.Config.Obj(drop.ID)
	key := boneburierrunner.DropKey(drop, obj.Name)
	before := lootPackBefore(ctx)
	res := boneburierrunner.Collect(d, drop)
	switch {
	case res.Picked:
		b.picked++
		// What the take put in the pack reaches the loot panel: nothing this script kills made
		// it, so it is a pickup of the bone itself (lootcombat.go's NotePickup). The burial that
		// follows is recorded by the Runner's bury watch (lootprayer.go).
		ctx.NotePickup(before, drop.ID, drop.X, drop.Z, drop.Level)
		b.clearSkip(key)
		b.vlog(ctx, "picked up %s (%d total)", res.Name, b.picked)
	case res.Gone:
		b.clearSkip(key)
		b.vlog(ctx, "%s was gone by the time we arrived", res.Name)
	default:
		b.missed++
		b.skip(key)
		b.vlog(ctx, "%s did not come up — leaving it for a while", res.Name)
	}
}

func (b *boneBurierBot) buryStep(ctx *Context, d *boneburierrunner.Deps, names, bankNames []string, mode boneplan.DisposalMode) {
	const buriesPerLoop = 4
	for i := 0; i < buriesPerLoop; i++ {
		if ctx.Stopped() {
			return
		}
		if actions.NewChatDialog(ctx.Session, ctx.Components).CanContinue() {
			return
		}
		it, op, ok := boneburierrunner.HeldBoneToBury(d, names, bankNames, mode)
		if !ok {
			return
		}
		if !boneburierrunner.BuryOne(d, it, op) {
			b.warn(ctx, "bury-refused", 10*time.Second, "%s was refused or did not land — retrying", op)
			ctx.Delay(1200 * time.Millisecond)
			return
		}
		b.buried++
	}
}

func (b *boneBurierBot) bankStep(ctx *Context, d *boneburierrunner.Deps, names, bankNames []string, mode boneplan.DisposalMode) {
	carried := boneburierrunner.CountBound(d, boneplan.ToBank, names, bankNames, mode)
	bank, ok := boneburierrunner.OpenNearestBank(d, 30)
	if !ok {
		b.warn(ctx, "bank-open", 30*time.Second, "could not find or open a bank — retrying")
		ctx.Delay(3 * time.Second)
		return
	}
	deposited := boneburierrunner.DepositBoneNames(d, bank, names, bankNames, mode)
	_ = bank.Close()
	if deposited > 0 {
		b.trips++
		b.banked += carried
		ctx.logf("boneburier: bank trip %d: deposited %d bones (%d banked in all)", b.trips, carried, b.banked)
	}
	left := boneburierrunner.CountBound(d, boneplan.ToBank, names, bankNames, mode)
	if left > 0 {
		b.warn(ctx, "bank-deposit", 30*time.Second, "%d bones would not go into the bank — retrying", left)
		return
	}
	if b.finishing {
		b.stop(ctx, "nothing left in range — "+strconv.Itoa(b.banked)+" bones banked")
		return
	}
	b.returning = true
}

func (b *boneBurierBot) withdrawStep(ctx *Context, d *boneburierrunner.Deps, names []string) {
	bank, ok := boneburierrunner.OpenNearestBank(d, 30)
	if !ok {
		b.warn(ctx, "bank-open", 30*time.Second, "could not find or open a bank — retrying")
		ctx.Delay(3 * time.Second)
		return
	}
	if boneburierrunner.PackFull(d) {
		_ = bank.Close()
		b.stop(ctx, "the pack is full of something that will not bury — empty it and start again")
		return
	}
	row, name, ok := boneburierrunner.BankRowToWithdraw(d, bank, names)
	if !ok {
		_ = bank.Close()
		b.stop(ctx, "no more listed bones in the bank — "+strconv.Itoa(b.buried)+" buried")
		return
	}
	if err := boneburierrunner.SetNoteMode(d, bank, false); err != nil {
		ctx.logf("boneburier: could not switch the bank to items: %v", err)
	}
	drew := boneburierrunner.WithdrawAllOf(d, bank, row)
	_ = bank.Close()
	if !drew {
		b.warn(ctx, "withdraw", 30*time.Second, "%s would not come out of the bank — retrying", name)
		return
	}
	b.trips++
	ctx.logf("boneburier: drew %s from the bank (trip %d)", name, b.trips)
}

func (b *boneBurierBot) walkBackStep(ctx *Context, d *boneburierrunner.Deps) {
	if b.anchor == nil {
		b.returning = false
		return
	}
	radius := b.settings.Radius
	if radius > 4 || radius <= 0 {
		radius = 4
	}
	if !boneburierrunner.WalkNear(d, *b.anchor, radius) {
		b.warn(ctx, "walk-back", 30*time.Second, "could not get back to the working ground — retrying")
		ctx.Delay(3 * time.Second)
		return
	}
	b.returning = false
}

func (b *boneBurierBot) idle(ctx *Context, d *boneburierrunner.Deps) {
	now := time.Now()
	if b.idleSince == nil {
		b.idleSince = &now
	}
	limitMs, has := boneplan.IdleStopMs(b.settings.IdleMins)
	waited := now.Sub(*b.idleSince)
	if boneburierrunner.PackFull(d) && (!has || int(waited.Milliseconds()) < limitMs) {
		b.warn(ctx, "pack-full", 30*time.Second, "the pack is full and holds nothing to bury or bank — free a slot to carry on")
		ctx.Delay(3 * time.Second)
		return
	}
	if has && int(waited.Milliseconds()) >= limitMs {
		names, bankNames, mode := b.settings.NameList(), b.settings.BankNameList(), b.settings.Mode()
		if boneburierrunner.CountBound(d, boneplan.ToBank, names, bankNames, mode) > 0 {
			b.finishing = true
			ctx.logf("boneburier: no bones in range for a while — banking what is held, then stopping")
			return
		}
		b.stop(ctx, "no bones in range for a while")
		return
	}
	ctx.Delay(2 * time.Second)
}

func (b *boneBurierBot) checkGoal(ctx *Context, d *boneburierrunner.Deps) {
	if b.settings.GoalLevel <= 0 || b.settings.Mode() == boneplan.DisposalBank {
		return
	}
	xpForGoal := levels.XPAtLevel(b.settings.GoalLevel)
	minutes := time.Since(b.startedAt).Minutes()
	goal := boneplan.Goal(xpForGoal, boneburierrunner.PrayerXP(d), b.xpAtStart, minutes)
	if !goal.Reached {
		b.goalAnnounced = false
		return
	}
	if !b.goalAnnounced {
		b.goalAnnounced = true
		ctx.logf("boneburier: prayer %d reached — still burying", b.settings.GoalLevel)
	}
}
