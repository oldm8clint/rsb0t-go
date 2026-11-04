package bot

// The CombatTrainer at a rung whose ground is a fenced pen (combatplan.Rung.Pen): the Lumbridge cows. combatplan/pen.go has the reason
// and the figures, combatrunner/pen.go the questions asked of the collision pack; this is the trainer's use of them, the same rules
// the quest farms follow (questmods/cowfarm.go): come in by the gate, fight only the cows a walk over open ground reaches, and when
// none is in reach walk the pen's beat instead of standing where it looked.

import (
	"sort"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/combatrunner"
	"rsb0t-go/internal/scripts/tile"
)

const (
	// penBeatAfter is how long no monster may have been in reach before the account walks the pen's beat. The cows in view are taken
	// as they come back (a respawn is 54 seconds) and a walk of the beat shows the account those its view of one corner does not,
	// so the wait is a few seconds, not a respawn.
	penBeatAfter = 6 * time.Second
	// penBeatTries is how many walks of the beat in a row may fail before it is left alone until a monster has been in reach again.
	penBeatTries = 3
)

// penBeatWalkTimeout bounds one walk to a stop of the beat (a variable so a test of a walk that fails need not wait it out).
var penBeatWalkTimeout = 60 * time.Second

// penOf is the ground of the chosen rung's pen for this run, built when the rung is first asked about and again when the rung changes;
// nil at a rung that is not a fenced pen.
func (b *combatTrainerBot) penOf(d *combatrunner.Deps) *combatrunner.PenGround {
	if !b.haveRung || b.rung.Pen == nil {
		return nil
	}
	if b.pen == nil || b.penKey != b.rung.Key {
		b.pen, b.penKey = combatrunner.NewPenGround(d.Pack, *b.rung.Pen), b.rung.Key
		b.penIdleSince, b.penBeatAt, b.penWalks, b.penBeatFails = time.Time{}, 0, 0, 0
	}
	return b.pen
}

// penFoundTarget notes that a monster is in reach: the wait for one starts again from nothing.
func (b *combatTrainerBot) penFoundTarget() {
	b.penIdleSince, b.penBeatFails = time.Time{}, 0
}

// travelToRung walks the account to the chosen rung and reports whether it got there. At a fenced pen that is in by the gate to the
// pen's entry (combatrunner.EnterPen), whose radius leaves no stopping place on the road; everywhere else it is the walk to the stand
// within three tiles that it always was.
func (b *combatTrainerBot) travelToRung(ctx *Context, d *combatrunner.Deps) bool {
	g := b.penOf(d)
	if g == nil {
		return combatrunner.TravelTo(d, b.rung.Stand, 3, b.travelBudget(ctx, b.rung.Stand))
	}
	if !combatrunner.EnterPen(d, g, b.travelBudget(ctx, g.Pen().Enter)) {
		return false
	}
	// In. The entry is inside the leash (a test holds that), but the pen's two ends are not: an account inside the fence and past the
	// leash walks on to the stand.
	if x, z, level, ok := ctx.Session.State().Tile(); ok {
		if here := (tile.Tile{X: x, Z: z, Level: level}); here.Level != b.rung.Stand.Level || tile.Chebyshev(here, b.rung.Stand) > b.rung.Leash {
			return combatrunner.TravelTo(d, b.rung.Stand, 3, b.travelBudget(ctx, b.rung.Stand))
		}
	}
	return true
}

// strayedFromPen reports whether the account is outside the fence of the chosen rung's pen, and says so. A fight loop that finds it so
// ends its pass: Loop walks the account in by the gate, and there is no cow to pick from the road.
func (b *combatTrainerBot) strayedFromPen(ctx *Context, d *combatrunner.Deps) bool {
	g := b.penOf(d)
	if g == nil {
		return false
	}
	x, z, level, ok := ctx.Session.State().Tile()
	if !ok {
		return false
	}
	here := tile.Tile{X: x, Z: z, Level: level}
	if g.Inside(here) {
		return false
	}
	b.warn(ctx, "outpen", 30*time.Second, "at %v, outside rung %q's pen — leaving the fight to walk in by the gate", here, b.rung.Key)
	return true
}

// pickPenTarget is the monster to attack at a fenced pen among cands (those inside the rung's leash and not passed over lately): not
// one with no walk over open ground to it, which is how a monster behind a fence reads; the one already on the account first, then the
// free ones nearest BY THE WALK, then the ones another player is fighting (rankTargetsBy). Between equals the lower NPC index, so two
// accounts looking at the same pen do not swap targets.
func (b *combatTrainerBot) pickPenTarget(ctx *Context, g *combatrunner.PenGround, here tile.Tile, cands []gamestate.NPC, self int, multiway, known bool) (gamestate.NPC, bool) {
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Index < cands[j].Index })
	steps := make(map[int]int, len(cands))
	reachable := make([]gamestate.NPC, 0, len(cands))
	for _, n := range cands {
		s := g.Steps(here, tile.Tile{X: n.X, Z: n.Z, Level: n.Level})
		if s < 0 {
			b.noteNoRoute(ctx, n, "no walk over open ground reaches it")
			continue
		}
		steps[n.Index] = s
		reachable = append(reachable, n)
	}
	ranked := rankTargetsBy(reachable, self, multiway, known, func(n gamestate.NPC) int { return steps[n.Index] })
	if len(ranked) == 0 {
		return gamestate.NPC{}, false
	}
	return ranked[0], true
}

// walkBeat spends a wait with no monster in reach walking the next stop of the pen's beat, and reports whether it walked (the caller
// then looks again at once). It waits penBeatAfter of nothing first, and a walk gives the pen another penBeatAfter before the next, so
// it cannot spin; a walk that fails is not counted as one.
func (b *combatTrainerBot) walkBeat(ctx *Context, d *combatrunner.Deps) bool {
	g := b.penOf(d)
	if g == nil || b.penBeatFails >= penBeatTries {
		return false
	}
	now := time.Now()
	if b.penIdleSince.IsZero() {
		b.penIdleSince = now
		return false
	}
	if now.Sub(b.penIdleSince) < penBeatAfter {
		return false
	}
	x, z, level, ok := ctx.Session.State().Tile()
	if !ok {
		return false
	}
	here := tile.Tile{X: x, Z: z, Level: level}
	stop, ok := g.Pen().NextBeat(&b.penBeatAt, here)
	b.penIdleSince = now
	if !ok {
		return false
	}
	b.warn(ctx, "penbeat", 30*time.Second, "no %s in reach of %v for %s — walking the pen to %v (walk %d of this run)",
		b.rung.Monster.Name, here, penBeatAfter, stop, b.penWalks+1)
	if !combatrunner.TravelTo(d, stop, 2, penBeatWalkTimeout) {
		b.penBeatFails++
		return false
	}
	b.penWalks++
	b.penBeatFails = 0
	return true
}
