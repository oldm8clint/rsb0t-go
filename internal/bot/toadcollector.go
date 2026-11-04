package bot

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/toadcollectplan"
	"rsb0t-go/internal/scripts/toadcollectrunner"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("ToadCollector", func() Bot { return &toadCollectorBot{} })
}

// toadCollectorBot picks up the Swamp toad ground spawns in the Gnome swamp (toadcollectplan.Spawns, content map
// m37_54, around 2418,3514), pulls the legs off at once ("Remove-legs" turns the toad into Toad's legs) and hands the
// Toad's legs to the mule: they are the second ingredient of an Agility potion (Toadflax potion (unf) + Toad's legs,
// Herblore 34). Ground spawns are per world, so run one collector per world.
//
// Settings (toadcollectplan.DecodeSettings; an unknown key is an error), all optional:
//
//	mule            string  default "mule"  — the MuleReceiver the legs go to; "" never hands over (a full pack is
//	                                          then unloaded at the nearest ordinary bank)
//	handOffAt       int     default 24      — legs held that make a hand-off due (1..28)
//	handOffMinutes  int     default 30      — hand over whatever is held after this long since the last hand-off
//	logDetail       string  default "Normal"
//
// The hand-off is donorrunner.Run at the mule's own post (scarabMulePresenceOf), with WireDonorWorlds when the mule is
// on another world, exactly as AntipoisonCollector does it.
type toadCollectorBot struct {
	settings toadcollectplan.Settings
	walker   *webwalk.Walker

	lastHandOff time.Time
	retryAt     time.Time
	lastSaid    time.Time

	picked     int
	skinned    int
	handedLegs int
	banked     int
	handOffs   int
	lastStep   toadcollectplan.StepKind
	bankLegs   int // legs left in this account's bank at the last visit; the next hand-off draws them
}

func (b *toadCollectorBot) deps(ctx *Context) *toadcollectrunner.Deps {
	return &toadcollectrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log, Walker: b.walker,
	}
}

func (b *toadCollectorBot) say(ctx *Context, format string, args ...any) {
	b.lastSaid = time.Now()
	ctx.logf("toads: "+format, args...)
}

func (b *toadCollectorBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("toads: no collision pack configured (fleet.config.json needs a packPath)")
	}
	set, err := toadcollectplan.DecodeSettings(ctx.Settings)
	if err != nil {
		return err
	}
	b.settings = set
	if w, werr := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); werr != nil {
		ctx.logf("toads: no webwalk (%v) — walks fall back to plain pathfinding", werr)
	} else {
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{Client: w.Client, Session: ctx.Session, Config: ctx.Config, Components: ctx.Components, Log: ctx.Log}))
		}
		b.walker = w
	}
	b.lastHandOff = time.Now()
	b.lastSaid = b.lastHandOff
	ctx.logf("toads: collecting Swamp toads at the Gnome swamp (%d spawns around %d,%d); hand legs to %q at %d or every %d min",
		len(toadcollectplan.Spawns), toadcollectplan.Post.X, toadcollectplan.Post.Z, set.Mule, set.HandOffAt, set.HandOffMinutes)
	return nil
}

func (b *toadCollectorBot) OnStop(ctx *Context) {
	ctx.logf("toads: stopped — %d toad(s) picked, %d skinned, %d leg(s) handed to %q in %d hand-off(s), %d banked",
		b.picked, b.skinned, b.handedLegs, b.settings.Mule, b.handOffs, b.banked)
}

func (b *toadCollectorBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !toadcollectrunner.WaitForGuardian(d) {
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

	legs, toads := toadcollectrunner.Held(d)
	now := time.Now()
	step := toadcollectplan.Decide(toadcollectplan.Snapshot{
		Here:     toadcollectrunner.Here(d),
		HeldLegs: legs, HeldToads: toads, BankLegs: b.bankLegs,
		Kit:            toadcollectrunner.Kit(d),
		FreeSlots:      toadcollectrunner.FreeSlots(d),
		Visible:        toadcollectrunner.Visible(d),
		MuleName:       b.settings.Mule,
		HandOffAt:      b.settings.HandOffAt,
		HandOffEvery:   time.Duration(b.settings.HandOffMinutes) * time.Minute,
		SinceHandOff:   now.Sub(b.lastHandOff),
		HandOffRetryOK: !now.Before(b.retryAt),
	})
	if b.settings.Verbose() && step.Kind != b.lastStep {
		ctx.logf("toads: -> %s", step.Kind)
	}
	b.lastStep = step.Kind

	switch step.Kind {
	case toadcollectplan.StepClear:
		at := questplan.NearestBank(toadcollectrunner.Here(d)).Tile
		if b.visitBank(ctx, d, at, true) {
			b.say(ctx, "banked the previous kit at %d,%d", at.X, at.Z)
		}
	case toadcollectplan.StepSkin:
		if n := toadcollectrunner.Skin(d); n > 0 {
			b.skinned += n
			l, _ := toadcollectrunner.Held(d)
			b.say(ctx, "pulled the legs off %d toad(s) (%d legs held)", n, l)
		} else {
			ctx.Delay(2 * time.Second)
		}
	case toadcollectplan.StepHandOff:
		if b.bankLegs > 0 {
			// Draw the legs banked earlier at the bank by the mule's own post, so the trade carries them too.
			if !b.visitBank(ctx, d, b.muleBank(ctx, d), true) {
				return nil
			}
			legs, _ = toadcollectrunner.Held(d)
			if legs == 0 {
				return nil
			}
		}
		b.handOff(ctx, d, legs)
	case toadcollectplan.StepBank:
		at := b.muleBank(ctx, d)
		if b.visitBank(ctx, d, at, false) {
			b.banked += legs
			b.say(ctx, "banked %d Toad's legs at %d,%d (the pack was full and the mule could not take them)", legs, at.X, at.Z)
		}
	case toadcollectplan.StepPick:
		res := toadcollectrunner.Collect(d, step.Spot)
		switch {
		case res.Picked:
			b.picked++
		case res.Gone:
			if b.settings.Verbose() {
				ctx.logf("toads: the one at %d,%d was taken by someone else", step.Spot.Tile.X, step.Spot.Tile.Z)
			}
		default:
			ctx.Delay(2 * time.Second)
		}
	case toadcollectplan.StepTravel:
		if !toadcollectrunner.WalkNear(d, toadcollectplan.Post, 3) {
			b.say(ctx, "WARNING: could not reach the swamp at %d,%d — retrying", toadcollectplan.Post.X, toadcollectplan.Post.Z)
			ctx.Delay(5 * time.Second)
		}
	default: // wait for a respawn; poll cheaply
		if time.Since(b.lastSaid) >= toadcollectplan.HeartbeatEvery {
			b.say(ctx, "waiting for respawns (%d legs held; %d picked, %d legs handed over)", legs, b.picked, b.handedLegs)
		}
		ctx.Delay(2 * time.Second)
	}
	return nil
}

func (b *toadCollectorBot) handOff(ctx *Context, d *toadcollectrunner.Deps, legs int) {
	mule := b.settings.Mule
	rd := &donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		Bus: ctx.Bus, Me: ctx.Username,
	}
	WireDonorWorlds(ctx, rd)
	packRows := func() []donorplan.ItemRow { return toadcollectrunner.PackSlotRows(d) }
	b.say(ctx, "handing %d Toad's legs to %s", legs, mule)
	res := donorrunner.Run(rd, donorrunner.Options{
		Mule: mule, Stand: scarabMulePresenceOf(ctx, mule).Post,
		RowsInBank: legs, Drawn: legs,
		Load: func() []donorplan.LoadGroup {
			return toadcollectplan.MuleLoad(packRows(), donorplan.DefaultMuleFreeSlots)
		},
		HaulLeft: func() int { return toadcollectplan.MuleHaul(packRows()) },
	})
	switch {
	case res.Stopped:
		return
	case res.Traded:
		left, _ := toadcollectrunner.Held(d)
		given := max(0, legs-left)
		b.handedLegs += given
		b.handOffs++
		b.lastHandOff = time.Now()
		b.retryAt = time.Time{}
		b.say(ctx, "handed %d Toad's legs to %s (%d left in the pack)", given, mule, left)
	case res.GaveUp:
		b.retryAt = time.Now().Add(donorplan.HandoffRetry)
		b.say(ctx, "hand-off to %s failed (%s) — picking on, next try no sooner than %s", mule, res.Reason, b.retryAt.Format(time.Kitchen))
	default:
		b.retryAt = time.Now().Add(donorplan.HandoffRetry)
		b.say(ctx, "hand-off to %s ended without trading (%s) — next try no sooner than %s", mule, res.Reason, b.retryAt.Format(time.Kitchen))
	}
}

// muleBank is the ordinary bank nearest the mule's own post, or nearest this account when there is no mule.
func (b *toadCollectorBot) muleBank(ctx *Context, d *toadcollectrunner.Deps) tile.Tile {
	if b.settings.Mule == "" {
		return questplan.NearestBank(toadcollectrunner.Here(d)).Tile
	}
	return questplan.NearestBank(scarabMulePresenceOf(ctx, b.settings.Mule).Post).Tile
}

// visitBank runs toadcollectrunner.VisitBank and records what the bank still holds; false after a failure (logged,
// with a pause before the next pass).
func (b *toadCollectorBot) visitBank(ctx *Context, d *toadcollectrunner.Deps, at tile.Tile, draw bool) bool {
	v, err := toadcollectrunner.VisitBank(d, at, draw)
	if err != nil {
		b.say(ctx, "WARNING: bank visit at %d,%d failed: %v - retrying", at.X, at.Z, err)
		ctx.Delay(10 * time.Second)
		return false
	}
	b.bankLegs = v.BankLegs
	return true
}
