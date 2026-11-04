package bot

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/flinchplan"
	"rsb0t-go/internal/scripts/flinchrunner"
	"rsb0t-go/internal/scripts/tile"
)

func init() {
	Register("MeleeFlincher", func() Bot { return &meleeFlincherBot{} })
}

// meleeFlincherBot ports MeleeFlincher.ts's own hit-and-hide cycle: wait for a target's
// combat timer to reset (both health bars gone, plus a settle margin that adapts to what
// swings actually cost — flinchplan.NextSettle), step out for one hit, and be back on the
// safe tile before the target's reaction delay runs out. Every Loop call does exactly one
// stage's worth of work (flinchplan.DecideStage), the same per-call dispatch every other
// port here uses — this maps naturally onto MeleeFlincher.ts's own loop() shape, which
// already ran roughly once per tick.
//
// Not ported (see flinchplan's own doc comment for why): the HUD, prayer flicking (no
// Prayer primitive in this client yet).
//
// Settings, all optional:
//
//	target     string  default ""  — the NPC name to flinch; blank takes the nearest
//	                                 attackable NPC within flinchplan.LEASH of the safe tile.
//	safeTile   string  default ""  — "x,z" or "x,z,level"; blank uses the tile the run is
//	                                 started on. Has to be a tile the target cannot reach.
type meleeFlincherBot struct {
	target   string
	safeTile tile.Tile

	watch *flinchrunner.SwingWatch

	targetIdx      int
	targetName     string
	haveTarget     bool
	swingsOnTarget int
	waitedTicks    int
	ticksClear     int
	settle         flinchplan.SettleState
	swung          bool
	hpAtStrike     int
	// kill judges the held target's death and remembers what it was, so a kill can be named
	// on the loot panel even when the corpse has already left the NPC list (killwatch.go).
	kill killWatch

	skipUntil map[int]int64

	kills, swings, forced, hitsTaken, eats int
	startedAt                              time.Time
	xpAtStart                              map[int]int
	stopped                                string
	lastHP                                 int
}

func (b *meleeFlincherBot) deps(ctx *Context) *flinchrunner.Deps {
	return &flinchrunner.Deps{
		Session: ctx.Session,
		Pack:    ctx.Pack,
		Locs:    ctx.Locs,
		Config:  ctx.Config,
		Chat:    ctx.Chat,
		Delay:   ctx.Delay,
		Log:     ctx.Log,
	}
}

func (b *meleeFlincherBot) OnStart(ctx *Context) error {
	if ctx.Guardian != nil {
		// See advancedFighterBot.OnStart's identical line and
		// randomevents.Guardian.CombatExpected's own doc comment: this script's InCombat
		// is its ordinary, desired state, not a hazard.
		ctx.Guardian.CombatExpected = true
	}

	b.target = strings.TrimSpace(StringSetting(ctx.Settings, "target", ""))
	typed := strings.TrimSpace(StringSetting(ctx.Settings, "safeTile", ""))
	if typed != "" {
		if x, z, level, ok := flinchplan.ParseTile(typed); ok {
			b.safeTile = tile.Tile{X: x, Z: z, Level: level}
		} else {
			ctx.logf("meleeflincher: WARNING: %q is not a tile — write it as x,z or x,z,level. Standing where the run was started instead.", typed)
		}
	}
	if b.safeTile == (tile.Tile{}) {
		if !ctx.DelayUntil(func() bool { _, _, _, ok := ctx.Session.State().Tile(); return ok }, 15*time.Second) {
			ctx.logf("meleeflincher: never got a starting position")
		}
		if x, z, level, ok := ctx.Session.State().Tile(); ok {
			b.safeTile = tile.Tile{X: x, Z: z, Level: level}
		}
	}

	b.watch = flinchrunner.NewSwingWatch()
	b.settle = flinchplan.FreshSettle
	b.skipUntil = map[int]int64{}
	b.startedAt = time.Now()
	b.xpAtStart = map[int]int{}
	for _, stat := range fighterrunnerCombatSkillStats() {
		sk, _ := ctx.Session.State().Skill(stat)
		b.xpAtStart[stat] = sk.XP
	}
	b.lastHP, _ = ctx.Session.State().HP()
	b.targetIdx = -1

	if actions.SetAutoRetaliate(ctx.Session, false) == nil {
		ctx.DelayUntil(func() bool { return !ctx.Session.State().AutoRetaliateOn() }, 2*time.Second)
	} else {
		ctx.logf("meleeflincher: WARNING: could not turn off auto retaliate — turn it off by hand, or it will walk out to answer a hit on its own")
	}

	ctx.logf("meleeflincher: %s, safe tile %d,%d,%d — the target has to be unable to reach that tile, or the bars never clear",
		targetLabel(b.target), b.safeTile.X, b.safeTile.Z, b.safeTile.Level)
	return nil
}

func targetLabel(name string) string {
	if name == "" {
		return "anything attackable"
	}
	return name
}

// fighterrunnerCombatSkillStats avoids importing fighterrunner into this file just for its
// stat-index constants; the values are the same ones fighterrunner.CombatSkillStats uses
// (gamestate/skills.go's own SkillNames table: Attack=0, Defence=1, Strength=2, Hitpoints=3).
func fighterrunnerCombatSkillStats() []int { return []int{0, 1, 2, 3} }

func (b *meleeFlincherBot) minutes() float64 { return time.Since(b.startedAt).Minutes() }

func (b *meleeFlincherBot) Loop(ctx *Context) error {
	if b.stopped != "" {
		ctx.Delay(2 * time.Second)
		return nil
	}
	d := b.deps(ctx)
	if !flinchrunner.WaitForGuardian(d) {
		return nil
	}
	if flinchrunner.CanContinueDialogue(d, ctx.Components) {
		_ = actions.NewChatDialog(ctx.Session, ctx.Components).Continue()
		ctx.Delay(300 * time.Millisecond)
		return nil
	}
	if ctx.Session.State().IsDead() {
		b.stopped = "died"
		ctx.logf("meleeflincher: died with %d kills — nothing is walked back for, so the gear is where it fell", b.kills)
		return nil
	}
	if ctx.Session.State().AutoRetaliateOn() {
		_ = actions.SetAutoRetaliate(ctx.Session, false)
	}

	flinchrunner.ObserveSelf(d, b.watch)
	b.countHits(ctx)

	var meTile tile.Tile
	if x, z, l, ok := ctx.Session.State().Tile(); ok {
		meTile = tile.Tile{X: x, Z: z, Level: l}
	}

	target, targetOK := b.held(ctx, d)
	verdict := flinchplan.ResetVerdictFrom(flinchplan.ResetInput{
		MyBar: ctx.Session.State().InCombat(), TargetBar: targetOK && target.InCombat,
		TicksWaited: b.waitedTicks, TicksClear: b.ticksClear, SettleTicks: b.settle.Ticks,
	})
	needEat := b.needEat(d)
	stage := flinchplan.DecideStage(flinchplan.StageInput{
		HasTarget: targetOK, OnSafeTile: meTile == b.safeTile, Swung: b.swung,
		Reset: verdict != flinchplan.ResetWaiting, NeedEat: needEat, Urgent: flinchrunner.HPFraction(d) <= 0.35,
	})

	switch stage {
	case flinchplan.StageEat:
		if flinchrunner.EatOnce(d) {
			b.eats++
		} else {
			ctx.Delay(600 * time.Millisecond)
		}
	case flinchplan.StageRetreat:
		b.returnToSafeTile(ctx, d, meTile)
	case flinchplan.StageReset:
		b.waitForReset(ctx, d, target, verdict)
	case flinchplan.StageSeek:
		b.seek(ctx, d)
	default:
		b.strike(ctx, d, target, verdict)
	}
	return nil
}

func (b *meleeFlincherBot) countHits(ctx *Context) {
	hp, ok := ctx.Session.State().HP()
	if !ok {
		return
	}
	if b.lastHP >= 0 && hp < b.lastHP {
		b.hitsTaken++
	}
	b.lastHP = hp
}

func (b *meleeFlincherBot) needEat(d *flinchrunner.Deps) bool {
	return flinchrunner.HPFraction(d) < 0.5 && flinchrunner.AnyFoodCount(d) > 0
}

// held is the target we hold if it is still standing, counting the kill when it is not —
// ported from MeleeFlincher.ts's own held().
//
// Takes ctx as well as the runner Deps because this is the one place in this script that
// decides a target died, and so the only honest place to record the kill for the control
// panel's loot tracker. Until 2026-09-22 nothing here did: b.kills went up, the panel showed
// nothing, and a flinching account's monsters never appeared on it at all.
func (b *meleeFlincherBot) held(ctx *Context, d *flinchrunner.Deps) (npc gamestate.NPC, ok bool) {
	if !b.haveTarget {
		return gamestate.NPC{}, false
	}
	live, found := flinchrunner.TrackNPC(d, b.targetIdx)
	// Judged from this reading, before the target is dropped below: a target that has left the
	// NPC list carries no type id, so the kill has to be named from the last reading in which
	// it was present (killwatch.go weighs the pair with combatplan.JudgeKill).
	verdict := b.kill.observe(b.targetIdx, live, found, found && live.TargetsPlayer(flinchrunner.SelfSlot(d)))
	down := !found || (live.TotalHealth > 0 && live.Health == 0)
	if !down {
		return live, true
	}
	if b.swingsOnTarget > 0 {
		b.kills++
		// Gated on the same swingsOnTarget > 0 as the counter, so what the panel shows and
		// what OnStop prints cannot disagree: a target this run never landed a swing on is
		// neither counted nor reported, whichever way it left.
		if npcType, x, z, level, took := b.kill.killed(verdict); took {
			ctx.NoteKill(npcType, x, z, level)
		}
	}
	b.dropTarget()
	return gamestate.NPC{}, false
}

func (b *meleeFlincherBot) dropTarget() {
	b.haveTarget = false
	b.targetIdx = -1
	b.targetName = ""
	b.swingsOnTarget = 0
	b.waitedTicks = 0
}

func (b *meleeFlincherBot) skipTarget(index int) {
	b.skipUntil[index] = time.Now().Add(20 * time.Second).UnixMilli()
}

func (b *meleeFlincherBot) seek(ctx *Context, d *flinchrunner.Deps) {
	npc, ok := flinchrunner.Pick(d, b.target, b.safeTile, b.targetIdx, b.haveTarget, b.skipUntil, time.Now().UnixMilli())
	if ok {
		b.haveTarget = true
		b.targetIdx = npc.Index
		b.targetName = b.target
		b.swingsOnTarget = 0
		b.waitedTicks = 0
		return
	}
	ctx.Delay(1200 * time.Millisecond)
}

func (b *meleeFlincherBot) waitForReset(ctx *Context, d *flinchrunner.Deps, npc gamestate.NPC, verdict flinchplan.ResetVerdict) {
	b.waitedTicks++
	clear := !ctx.Session.State().InCombat() && !npc.InCombat
	if clear {
		b.ticksClear++
	} else {
		b.ticksClear = 0
	}
	if verdict == flinchplan.ResetTimeout {
		b.forced++
	}
	ctx.Delay(600 * time.Millisecond)
}

func (b *meleeFlincherBot) strike(ctx *Context, d *flinchrunner.Deps, npc gamestate.NPC, verdict flinchplan.ResetVerdict) {
	// The held target is re-struck pass after pass, so the gate in Pick is not enough on its
	// own: a target that was reachable when it was chosen can wander behind a wall, and one
	// chosen before the pack could answer was never checked at all. Without this the strike
	// clicks through the wall, burns its nine-tick swing wait on a swing that never comes, and
	// gets the same NPC back twenty seconds later.
	at := tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}
	if reach, why := flinchrunner.Reachable(d, at); !reach {
		ctx.logf("meleeflincher: no route from the safe tile to the target at %d,%d,%d (%s) — passing over it", at.X, at.Z, at.Level, why)
		b.skipTarget(npc.Index)
		b.dropTarget()
		return
	}
	startHealth := npc.Health
	fromTick := flinchrunner.Tick(d)
	if !flinchrunner.AttackNPC(d, npc) {
		b.skipTarget(npc.Index)
		b.dropTarget()
		return
	}
	b.hpAtStrike, _ = ctx.Session.State().HP()

	swung := ctx.DelayUntil(func() bool {
		flinchrunner.ObserveSelf(d, b.watch)
		if b.watch.SwungSince(fromTick + 1) {
			return true
		}
		cur, found := flinchrunner.TrackNPC(d, npc.Index)
		return !found || cur.Health != startHealth
	}, 9*600*time.Millisecond)
	b.waitedTicks = 0
	b.ticksClear = 0
	if !swung {
		b.skipTarget(npc.Index)
		b.dropTarget()
		return
	}
	b.swings++
	b.swingsOnTarget++
	b.swung = true
	if verdict == flinchplan.ResetTimeout {
		b.forced++
	}
	_ = flinchrunner.DirectWalk(d, b.safeTile)
}

func (b *meleeFlincherBot) returnToSafeTile(ctx *Context, d *flinchrunner.Deps, me tile.Tile) {
	if me == b.safeTile {
		b.judgeSwing(ctx)
		b.swung = false
		b.waitedTicks = 0
		b.ticksClear = 0
		return
	}
	if tile.Chebyshev(me, b.safeTile) > 6 {
		flinchrunner.WalkNear(d, b.safeTile, 0)
		return
	}
	_ = flinchrunner.DirectWalk(d, b.safeTile)
	ctx.DelayTicks(1)
}

func (b *meleeFlincherBot) judgeSwing(ctx *Context) {
	hp, _ := ctx.Session.State().HP()
	tookHit := b.hpAtStrike >= 0 && hp < b.hpAtStrike
	b.hpAtStrike = -1
	b.settle = flinchplan.NextSettle(b.settle, tookHit)
}

func (b *meleeFlincherBot) OnStop(ctx *Context) {
	mins := b.minutes()
	quality := flinchplan.FlinchQuality(flinchplan.FlinchTally{Swings: b.swings, HitsTaken: b.hitsTaken})
	ctx.logf("meleeflincher: stopped — %d kills, %d swings (%d forced), %d hits taken, %.0f%% clean, %d bites, %.1f min",
		b.kills, b.swings, b.forced, b.hitsTaken, quality*100, b.eats, mins)
}
