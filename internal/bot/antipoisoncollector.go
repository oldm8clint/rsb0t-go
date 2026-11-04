package bot

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/antipoisoncollectplan"
	"rsb0t-go/internal/scripts/antipoisoncollectrunner"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("AntipoisonCollector", func() Bot { return &antipoisonCollectorBot{} })
}

// antipoisonCollectorBot picks up the two Antipoison(1) ground spawns at the Observatory (north of Castle Wars,
// 2437,3164 and 2467,3177), over and over, and hands the doses to the mule so Kalphite Queen fighters can be
// supplied (the mule's Stage 6 ask adds an antipoison line once its published bank shows any). Ground
// spawns are per world, so run one collector per world.
//
// Settings (antipoisoncollectplan.DecodeSettings; an unknown key is an error), all optional:
//
//	mule            string  default "mule"  — the MuleReceiver the doses go to; "" never hands over (a full pack is
//	                                          then unloaded at the nearest ordinary bank)
//	handOffAt       int     default 20      — doses held that make a hand-off due
//	handOffMinutes  int     default 30      — hand over whatever is held after this long since the last hand-off
//	logDetail       string  default "Normal"
//
// The hand-off is internal/scripts/donorrunner.Run at the mule's own post (scarabMulePresenceOf), with
// donorrunner's world errands (WireDonorWorlds) when the mule is on another world. A refused
// or failed hand-off is retried no sooner than donorplan.HandoffRetry; meanwhile picking continues, and a full
// pack is banked at the bank by the mule's post. A previous script's kit is banked first at the nearest bank, and
// every bank visit before a hand-off draws the doses banked earlier, so they reach the mule too. The Castle Wars
// chest is not used: on 1 Nov the walker could not get the collectors back out of that building.
type antipoisonCollectorBot struct {
	settings antipoisoncollectplan.Settings
	// walker plans over webwalk's door graph: the 2467,3177 spawn is inside the Observatory, and plain pathfinding
	// with door recovery walked in and then stranded every collector in that pocket on its first live run.
	walker *webwalk.Walker

	startedAt   time.Time
	lastHandOff time.Time
	retryAt     time.Time
	lastSaid    time.Time

	picked      int // potions taken
	pickedDoses int
	handedDoses int
	banked      int
	handOffs    int
	lastStep    antipoisoncollectplan.StepKind
	// bankPots and bankDoses are the antipoison left in this account's bank at the last visit; the next hand-off draws them.
	bankPots, bankDoses int
}

func (b *antipoisonCollectorBot) deps(ctx *Context) *antipoisoncollectrunner.Deps {
	return &antipoisoncollectrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log, Walker: b.walker,
	}
}

func (b *antipoisonCollectorBot) say(ctx *Context, format string, args ...any) {
	b.lastSaid = time.Now()
	ctx.logf("antipoison: "+format, args...)
}

func (b *antipoisonCollectorBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("antipoison: no collision pack configured (fleet.config.json needs a packPath)")
	}
	set, err := antipoisoncollectplan.DecodeSettings(ctx.Settings)
	if err != nil {
		return err
	}
	b.settings = set
	if w, werr := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); werr != nil {
		ctx.logf("antipoison: no webwalk (%v) — walks fall back to plain pathfinding", werr)
	} else {
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{Client: w.Client, Session: ctx.Session, Config: ctx.Config, Components: ctx.Components, Log: ctx.Log}))
		}
		b.walker = w
	}
	b.startedAt = time.Now()
	b.lastHandOff = b.startedAt
	b.lastSaid = b.startedAt
	ctx.logf("antipoison: collecting Antipoison(1) at %d,%d and %d,%d; hand off to %q at %d doses or every %d min",
		antipoisoncollectplan.Spawns[0].Tile.X, antipoisoncollectplan.Spawns[0].Tile.Z,
		antipoisoncollectplan.Spawns[1].Tile.X, antipoisoncollectplan.Spawns[1].Tile.Z,
		set.Mule, set.HandOffAt, set.HandOffMinutes)
	return nil
}

func (b *antipoisonCollectorBot) OnStop(ctx *Context) {
	ctx.logf("antipoison: stopped — %d potion(s) picked, %d dose(s) handed to %q in %d hand-off(s), %d potion(s) banked",
		b.picked, b.handedDoses, b.settings.Mule, b.handOffs, b.banked)
}

func (b *antipoisonCollectorBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !antipoisoncollectrunner.WaitForGuardian(d) {
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

	pots, doses := antipoisoncollectrunner.Held(d)
	now := time.Now()
	step := antipoisoncollectplan.Decide(antipoisoncollectplan.Snapshot{
		Here:     antipoisoncollectrunner.Here(d),
		HeldPots: pots, HeldDoses: doses,
		BankPots: b.bankPots, BankDoses: b.bankDoses,
		Kit:            antipoisoncollectrunner.Kit(d),
		FreeSlots:      antipoisoncollectrunner.FreeSlots(d),
		Visible:        antipoisoncollectrunner.Visible(d),
		MuleName:       b.settings.Mule,
		HandOffAt:      b.settings.HandOffAt,
		HandOffEvery:   time.Duration(b.settings.HandOffMinutes) * time.Minute,
		SinceHandOff:   now.Sub(b.lastHandOff),
		HandOffRetryOK: !now.Before(b.retryAt),
	})
	if b.settings.Verbose() && step.Kind != b.lastStep {
		ctx.logf("antipoison: -> %s", step.Kind)
	}
	b.lastStep = step.Kind

	switch step.Kind {
	case antipoisoncollectplan.StepClear:
		at := questplan.NearestBank(antipoisoncollectrunner.Here(d)).Tile
		if b.visitBank(ctx, d, at, true) {
			b.say(ctx, "banked the previous kit at %d,%d", at.X, at.Z)
		}
	case antipoisoncollectplan.StepHandOff:
		if b.bankPots > 0 || antipoisoncollectrunner.Kit(d) {
			// Draw the doses banked earlier at the bank by the mule's own post, so the trade carries them too.
			if !b.visitBank(ctx, d, b.muleBank(ctx, d), true) {
				return nil
			}
			pots, doses = antipoisoncollectrunner.Held(d)
			if pots == 0 {
				return nil
			}
		}
		b.handOff(ctx, d, pots, doses)
	case antipoisoncollectplan.StepBank:
		at := b.muleBank(ctx, d)
		if b.visitBank(ctx, d, at, false) {
			b.banked += pots
			b.say(ctx, "banked %d antipoison potion(s) at %d,%d (the pack was full and the mule could not take them)", pots, at.X, at.Z)
		}
	case antipoisoncollectplan.StepPick:
		res := antipoisoncollectrunner.Collect(d, step.Spot)
		switch {
		case res.Picked:
			b.picked++
			antipoisoncollectrunner.Decant(d)
			p, ds := antipoisoncollectrunner.Held(d)
			b.pickedDoses += ds - doses
			b.say(ctx, "picked 1 at %d,%d (%d held, %d doses)", step.Spot.Tile.X, step.Spot.Tile.Z, p, ds)
		case res.Gone:
			if b.settings.Verbose() {
				ctx.logf("antipoison: the one at %d,%d was taken by someone else", step.Spot.Tile.X, step.Spot.Tile.Z)
			}
		default:
			ctx.Delay(2 * time.Second)
		}
	case antipoisoncollectplan.StepTravel:
		if !antipoisoncollectrunner.WalkNear(d, antipoisoncollectplan.Post, 3) {
			b.say(ctx, "WARNING: could not reach the spawns at %d,%d — retrying", antipoisoncollectplan.Post.X, antipoisoncollectplan.Post.Z)
			ctx.Delay(5 * time.Second)
		}
	default: // wait for a respawn (about 60 s each); poll cheaply
		if time.Since(b.lastSaid) >= antipoisoncollectplan.HeartbeatEvery {
			b.say(ctx, "waiting for respawns (%d held, %d doses; %d picked, %d doses handed over)", pots, doses, b.picked, b.handedDoses)
		}
		ctx.Delay(2 * time.Second)
	}
	return nil
}

func (b *antipoisonCollectorBot) handOff(ctx *Context, d *antipoisoncollectrunner.Deps, pots, doses int) {
	mule := b.settings.Mule
	rd := &donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		Bus: ctx.Bus, Me: ctx.Username,
	}
	// Reach a mule that is logged in to a different world (bounded by worldhopplan; a one-world fleet no-ops).
	WireDonorWorlds(ctx, rd)
	packRows := func() []donorplan.ItemRow { return antipoisoncollectrunner.PackSlotRows(d) }
	b.say(ctx, "handing %d dose(s) (%d potion(s)) to %s", doses, pots, mule)
	res := donorrunner.Run(rd, donorrunner.Options{
		// The mule's own post as its heartbeat publishes it (Catherby for mule1), not MuleReceiver's Seers default.
		Mule: mule, Stand: scarabMulePresenceOf(ctx, mule).Post,
		RowsInBank: pots, Drawn: pots,
		Load: func() []donorplan.LoadGroup {
			return antipoisoncollectplan.MuleLoad(packRows(), donorplan.DefaultMuleFreeSlots)
		},
		HaulLeft: func() int { return antipoisoncollectplan.MuleHaul(packRows()) },
	})
	switch {
	case res.Stopped:
		return
	case res.Traded:
		_, left := antipoisoncollectrunner.Held(d)
		given := doses - left
		if given < 0 {
			given = 0
		}
		b.handedDoses += given
		b.handOffs++
		b.lastHandOff = time.Now()
		b.retryAt = time.Time{}
		b.say(ctx, "handed %d doses to %s (%d left in the pack)", given, mule, left)
	case res.GaveUp:
		b.retryAt = time.Now().Add(donorplan.HandoffRetry)
		b.say(ctx, "hand-off to %s failed (%s) — picking on, next try no sooner than %s", mule, res.Reason, b.retryAt.Format(time.Kitchen))
	default:
		b.retryAt = time.Now().Add(donorplan.HandoffRetry)
		b.say(ctx, "hand-off to %s ended without trading (%s) — next try no sooner than %s", mule, res.Reason, b.retryAt.Format(time.Kitchen))
	}
}

// muleBank is the ordinary bank nearest the mule's own post (Catherby's for mule1), or nearest this account when
// there is no mule.
func (b *antipoisonCollectorBot) muleBank(ctx *Context, d *antipoisoncollectrunner.Deps) tile.Tile {
	if b.settings.Mule == "" {
		return questplan.NearestBank(antipoisoncollectrunner.Here(d)).Tile
	}
	return questplan.NearestBank(scarabMulePresenceOf(ctx, b.settings.Mule).Post).Tile
}

// visitBank runs antipoisoncollectrunner.VisitBank and records what the bank still holds; false after a failure (logged,
// with a pause before the next pass).
func (b *antipoisonCollectorBot) visitBank(ctx *Context, d *antipoisoncollectrunner.Deps, at tile.Tile, draw bool) bool {
	v, err := antipoisoncollectrunner.VisitBank(d, at, draw)
	if err != nil {
		b.say(ctx, "WARNING: bank visit at %d,%d failed: %v - retrying", at.X, at.Z, err)
		ctx.Delay(10 * time.Second)
		return false
	}
	b.bankPots, b.bankDoses = v.BankPots, v.BankDoses
	if draw {
		antipoisoncollectrunner.Decant(d)
	}
	return true
}
