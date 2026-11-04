package questmods

// The Lumbridge cow pasture's farm, shared by Demon Slayer's bones (demonBones) and the cows rungs (cowsTo, which the Prayer rungs'
// fallback runs). questplan/cowpasture.go has the pen's geometry, the reason for this file and the yield model.
//
// The farm is a loop over a cowWorld (runCowFarm), the way the Prayer rungs' bones are a loop over a boneIO: the loop's rules (come
// in by the gate, fight only the cows a walk over open ground reaches, walk the pasture when none is, give a fight up that is going
// nowhere, say what the round yields) are tested against a simulated world, and liveCows is the real one.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

const (
	// demonCowSearch is how far from the account a cow may be and still be chosen; demonCowWindow is the square (half its side) the flood
	// over open ground covers around the account, and demonCowFloodEvery the longest a flood is reused while it stands still.
	demonCowSearch     = 24
	demonCowWindow     = 34
	demonCowFloodEvery = 2 * time.Second
	// demonCowFightBudget bounds one fight: a cow has eight hitpoints.
	demonCowFightBudget = 45 * time.Second
	// demonCowNoHitAfter is how long a fight may go without a blow landing before the cow is given up (one behind a fence never gets one).
	demonCowNoHitAfter = 30 * time.Second
	// demonCowBusyAfter is how long into a fight a cow another player is fighting is given up.
	demonCowBusyAfter = 4 * time.Second
	// demonCowSkipFor and demonCowSkipBusy are how long a cow given up on is left alone.
	demonCowSkipFor  = 2 * time.Minute
	demonCowSkipBusy = 20 * time.Second
	// demonCowSayEvery paces the round's progress line.
	demonCowSayEvery = 3 * time.Minute

	// cowEnterTries is how many walks into the pen in a row may end outside it before the round is given up: the gate is shut and
	// would not open, or the walk is blocked.
	cowEnterTries = 3
	// cowEnterRetry is the wait between two such walks.
	cowEnterRetry = 5 * time.Second
	// cowIdleWait is the wait between two looks at an empty scene.
	cowIdleWait = 3 * time.Second
	// cowSweepNear is how near a sweep stop may be to the account and still be skipped for the next: a walk of five tiles shows
	// the account nothing new.
	cowSweepNear = 5
	// cowBonesWait is how long a kill's bones are waited for (the death animation, then the drop).
	cowBonesWait = 6 * time.Second
	// cowBonesNear is how near the account the bones of its kill lie, and cowLooseNear and cowLooseSteps how far bones lying about
	// (left by a missed pickup, ours or another account's) are fetched from: within that many tiles and that many steps over open ground.
	cowBonesNear  = 4
	cowLooseNear  = 6
	cowLooseSteps = 8
	// cowLooseSkipFor is how long a bone that could not be taken is left alone.
	cowLooseSkipFor = time.Minute
	// cowFightRetry is the wait after a fight that failed at once, cowCrowdRadius how far the players counted in a yield line are.
	cowFightRetry  = 2 * time.Second
	cowCrowdRadius = 30
)

// ---------------------------------------------------------------------------------------------------------
// The ground.
// ---------------------------------------------------------------------------------------------------------

// cowGround is the open ground around the account, flooded over the collision pack, and what it says about the pen. The pack has the
// gate shut, so the flood from outside the pen never reaches a cow and the flood from inside never leaves it.
type cowGround struct {
	pk *pathfind.Pack
	// reach is the flood taken from `from` at `at`; it is reused while the account stands on the same tile.
	reach *pathfind.Reach
	from  tile.Tile
	at    time.Time
}

// floodFrom is the open ground around here, nil when the pack is not loaded or the account stands off it (the callers then fall back
// to distance).
func (g *cowGround) floodFrom(here tile.Tile) *pathfind.Reach {
	if g.pk == nil {
		return nil
	}
	if g.reach != nil && g.from == here && time.Since(g.at) < demonCowFloodEvery {
		return g.reach
	}
	reach := g.pk.ReachFrom(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, demonCowWindow, nil)
	if reach.Size() == 0 {
		reach = nil
	}
	g.reach, g.from, g.at = reach, here, time.Now()
	return reach
}

// stepsTo is the steps from here to a tile the engine accepts a melee attack on whatever stands at t from: see interactSteps.
func (g *cowGround) stepsTo(here, t tile.Tile) int {
	return interactSteps(g.pk, g.floodFrom(here), here, t)
}

// inside reports whether here is in the pen: the pen's stand tile is a walk over open ground away. From the road side of the gate it is
// not, whatever the distance. Without a pack it cannot be told, and the farm behaves as it did before it knew about the gate.
func (g *cowGround) inside(here tile.Tile) bool {
	if g.pk == nil {
		return true
	}
	reach := g.floodFrom(here)
	if reach == nil {
		return false
	}
	_, ok := reach.Steps(questplan.CowPastureStand.X, questplan.CowPastureStand.Z, questplan.CowPastureStand.Level)
	return ok
}

// ---------------------------------------------------------------------------------------------------------
// Which cow, and when to leave it.
// ---------------------------------------------------------------------------------------------------------

// demonCowBusy reports a cow another player is fighting: it faces that player and has just been hit.
func demonCowBusy(e entityquery.Entity, self int) bool {
	return e.InCombat && self >= 0 && e.FaceEntity >= 32768 && e.FaceEntity-32768 != self
}

// pickCow is the cow to fight among npcs: not given up on lately (skip, by NPC index, until when), not in another player's fight, and a
// walk over open ground away (steps, -1 for none). The nearest by that walk wins, and between equals the lower NPC index.
func pickCow(npcs []entityquery.Entity, self int, now time.Time, skip map[int]time.Time, steps func(tile.Tile) int) (entityquery.Entity, bool) {
	var best entityquery.Entity
	bestSteps, found := 0, false
	for _, e := range npcs {
		if !strings.EqualFold(e.Name, "Cow") || !now.After(skip[e.Index]) || demonCowBusy(e, self) {
			continue
		}
		n := steps(tile.Tile{X: e.X, Z: e.Z, Level: e.Level})
		if n < 0 {
			continue
		}
		if !found || n < bestSteps || (n == bestSteps && e.Index < best.Index) {
			best, bestSteps, found = e, n, true
		}
	}
	return best, found
}

// cowGiveUp says why a fight with cur (the cow as it stands now), begun at began, is not worth going on with, "" when it is, and how
// long the cow is then left alone. steps is the walk to it over open ground now (-1 for none).
func cowGiveUp(cur entityquery.Entity, self int, began, now time.Time, steps int) (why string, skip time.Duration) {
	since := now.Sub(began)
	switch {
	case steps < 0:
		return "no walk over open ground reaches it", demonCowSkipFor
	case since >= demonCowBusyAfter && demonCowBusy(cur, self):
		return "another player is fighting it", demonCowSkipBusy
	case since >= demonCowNoHitAfter && cur.TotalHealth == 0:
		return "no blow has landed on it", demonCowSkipFor
	}
	return "", 0
}

// nextSweep is the stop of the pasture's beat the account walks to next: the one after from (an index into the beat, advanced) that is
// not within cowSweepNear of here. ok is false when every stop is (the account is somewhere in the middle of a short beat).
func nextSweep(beat []tile.Tile, from *int, here tile.Tile) (tile.Tile, bool) {
	for i := 0; i < len(beat); i++ {
		stop := beat[*from%len(beat)]
		*from++
		if tile.Chebyshev(here, stop) > cowSweepNear {
			return stop, true
		}
	}
	return tile.Tile{}, false
}

// ---------------------------------------------------------------------------------------------------------
// The loop.
// ---------------------------------------------------------------------------------------------------------

// cowFight is how one fight went.
type cowFight struct {
	Result questrunner.FightResult
	// Why and Skip are, for a fight given up, the reason and how long the cow is left alone.
	Why  string
	Skip time.Duration
	Took time.Duration
}

// cowWorld is what the farm reads and does. liveCows is the real one; the tests' is a simulation.
type cowWorld interface {
	// Now is the clock the farm keeps its windows by, and Wait pauses on it (false: the bot is stopping).
	Now() time.Time
	Wait(time.Duration) bool
	// Quiet holds the farm through a random event; false: the bot is stopping.
	Quiet() bool
	// Done is whether the farm has what it came for, Room whether the pack can take more.
	Done() bool
	Room() bool
	// Here is the account's tile, Self its player index.
	Here() tile.Tile
	Self() int
	// Cows are the cows in view, Steps the walk over open ground from here to a tile a melee attack reaches (-1 for none), Inside
	// whether here is in the pen.
	Cows() []entityquery.Entity
	Steps(here, t tile.Tile) int
	Inside(here tile.Tile) bool
	// Enter walks in by the gate, WalkTo to a stop of the beat; both report whether the walk arrived.
	Enter() bool
	WalkTo(t tile.Tile) bool
	// Fight fights the chosen cow to the end.
	Fight(c entityquery.Entity) cowFight
	// Collect takes the bones a won fight left (and buries them, on a rung that buries); Loose takes bones lying about within reach.
	// Both report how many bones the pack gained.
	Collect(c entityquery.Entity) int
	Loose(here tile.Tile) int
	// Progress is what the round has to show, in the log's words; Crowd the other players in view.
	Progress() string
	Crowd() int
	Logf(format string, args ...any)
}

// cowSpec is one farming round.
type cowSpec struct {
	// Budget bounds the round; the clock starts when the loop does.
	Budget time.Duration
	// MaxFailures ends the round when this many fights in a row fail to start.
	MaxFailures int
}

// Why a round ended.
const (
	cowEndGoal    = "goal"
	cowEndBudget  = "budget"
	cowEndFull    = "full"
	cowEndStopped = "stopped"
	cowEndFailed  = "failed"
	cowEndGate    = "gate"
)

// cowTally is a round's account of itself.
type cowTally struct {
	Won, GaveUp, TimedOut int
	// Entries is how many walks in by the gate the round made, Sweeps how many stops of the beat it walked to, Bones the bones the
	// pack gained.
	Entries, Sweeps, Bones int
	Elapsed                time.Duration
	End                    string
}

// perMinute is the kills a minute the round has made.
func (t cowTally) perMinute() float64 {
	if t.Elapsed <= 0 {
		return 0
	}
	return float64(t.Won) / t.Elapsed.Minutes()
}

// line is the round's yield in the log's words.
func (t cowTally) line(label string, w cowWorld) string {
	crowd := ""
	if n := w.Crowd(); n > 0 {
		crowd = fmt.Sprintf("; %d other players in view", n)
	}
	return fmt.Sprintf("%s: %s in: %d cows killed (%.1f a minute), %d given up on, %d timed out; %s%s",
		label, t.Elapsed.Round(time.Second), t.Won, t.perMinute(), t.GaveUp, t.TimedOut, w.Progress(), crowd)
}

// runCowFarm is the farm: until the goal is met, the budget is spent, the pack is full or the bot stops, it keeps the account inside
// the pen (walking in by the gate when it is not), fights the nearest cow a walk over open ground reaches, takes the bones, and when no
// cow is to be had walks the pasture's beat (questplan.CowPastureSweep) rather than standing where it looked.
func runCowFarm(w cowWorld, label string, sp cowSpec) cowTally {
	var t cowTally
	skip := map[int]time.Time{}
	t0 := w.Now()
	deadline := t0.Add(sp.Budget)
	saidAt := t0
	failed, idle, enterFails, sweepAt := 0, 0, 0, 0
	end := func(why string) cowTally {
		t.End = why
		t.Elapsed = w.Now().Sub(t0)
		return t
	}
	for {
		switch {
		case w.Done():
			return end(cowEndGoal)
		case !w.Now().Before(deadline):
			return end(cowEndBudget)
		case !w.Room():
			return end(cowEndFull)
		case !w.Quiet():
			return end(cowEndStopped)
		}
		now := w.Now()
		if now.Sub(saidAt) >= demonCowSayEvery {
			saidAt = now
			t.Elapsed = now.Sub(t0)
			w.Logf("%s", t.line(label, w))
		}
		here := w.Here()
		if !w.Inside(here) {
			if enterFails >= cowEnterTries {
				w.Logf("%s: %d walks in by the gate (to %v) ended outside the pen, at %v; leaving the round", label, enterFails, questplan.CowPastureStand, here)
				return end(cowEndGate)
			}
			w.Logf("%s: at %v, outside the pen: walking in by the gate to %v", label, here, questplan.CowPastureStand)
			t.Entries++
			if !w.Enter() || !w.Inside(w.Here()) {
				enterFails++
				if !w.Wait(cowEnterRetry) {
					return end(cowEndStopped)
				}
			} else {
				enterFails = 0
			}
			continue
		}
		enterFails = 0
		if n := w.Loose(here); n > 0 {
			t.Bones += n
			continue
		}
		cows := w.Cows()
		c, ok := pickCow(cows, w.Self(), now, skip, func(x tile.Tile) int { return w.Steps(here, x) })
		if !ok {
			idle++
			if idle == 1 {
				// The cows of a scene the account has just come into are read a tick or two after the arrival.
				if !w.Wait(cowIdleWait) {
					return end(cowEndStopped)
				}
				continue
			}
			stop, walk := nextSweep(questplan.CowPastureSweep, &sweepAt, here)
			if idle == 2 || idle%20 == 0 {
				if walk {
					w.Logf("%s: no cow in reach of %v (%d in view); walking the pasture to %v", label, here, len(cows), stop)
				} else {
					w.Logf("%s: no cow in reach of %v (%d in view)", label, here, len(cows))
				}
			}
			if walk {
				t.Sweeps++
				if w.WalkTo(stop) {
					continue
				}
			}
			if !w.Wait(cowIdleWait) {
				return end(cowEndStopped)
			}
			continue
		}
		idle = 0
		fr := w.Fight(c)
		switch fr.Result {
		case questrunner.FightStopped:
			return end(cowEndStopped)
		case questrunner.FightLost:
			// A fight that fails at once must not spin (live 4 Oct 2026: the imp farm's identical loop was a quarter of the fleet's CPU).
			failed++
			if failed >= sp.MaxFailures {
				w.Logf("%s: %d fights in a row failed; leaving the pasture", label, failed)
				return end(cowEndFailed)
			}
			if !w.Wait(cowFightRetry) {
				return end(cowEndStopped)
			}
		case questrunner.FightWon:
			failed = 0
			t.Won++
			t.Bones += w.Collect(c)
		case questrunner.FightGaveUp:
			failed = 0
			t.GaveUp++
			skip[c.Index] = w.Now().Add(fr.Skip)
			w.Logf("%s: gave up on the cow at %d,%d after %s: %s", label, c.X, c.Z, fr.Took.Round(time.Second), fr.Why)
		case questrunner.FightTimedOut:
			failed = 0
			t.TimedOut++
			skip[c.Index] = w.Now().Add(demonCowSkipFor)
			w.Logf("%s: the cow at %d,%d is still standing after %s; leaving it for %s", label, c.X, c.Z, demonCowFightBudget, demonCowSkipFor)
		default:
			failed = 0
		}
	}
}

// ---------------------------------------------------------------------------------------------------------
// The live world.
// ---------------------------------------------------------------------------------------------------------

// liveCows is the cowWorld of an account in an engine. What it is for (the bones of Demon Slayer's key, the experience of a rung) is
// the done, room, collect and progress it is built with.
type liveCows struct {
	d      *questrunner.Deps
	sus    questplan.Sustain
	label  string
	ground cowGround
	// done is whether the farm has what it came for; room whether the pack can take more.
	done, room func() bool
	// bury is whether the bones are buried where they are taken (a rung) rather than kept (Demon Slayer's key).
	bury bool
	// progress is what the round has to show so far.
	progress func() string
	// looseSkip is the bones that could not be taken, left alone until the time (a bone across a fence the flood did not see).
	looseSkip map[tile.Tile]time.Time
}

func newLiveCows(d *questrunner.Deps, label string, sus questplan.Sustain) *liveCows {
	return &liveCows{d: d, sus: sus, label: label, ground: cowGround{pk: d.Pack}, looseSkip: map[tile.Tile]time.Time{}}
}

func (l *liveCows) Now() time.Time              { return time.Now() }
func (l *liveCows) Wait(t time.Duration) bool   { return l.d.Delay(t) }
func (l *liveCows) Quiet() bool                 { return questrunner.WaitForGuardian(l.d) }
func (l *liveCows) Done() bool                  { return l.done() }
func (l *liveCows) Room() bool                  { return l.room() }
func (l *liveCows) Here() tile.Tile             { return questrunner.CurrentTile(l.d) }
func (l *liveCows) Self() int                   { return l.d.Session.State().Self() }
func (l *liveCows) Steps(here, t tile.Tile) int { return l.ground.stepsTo(here, t) }
func (l *liveCows) Inside(here tile.Tile) bool  { return l.ground.inside(here) }
func (l *liveCows) Progress() string            { return l.progress() }

func (l *liveCows) Cows() []entityquery.Entity {
	return entityquery.Npcs(l.d.Session.State(), l.d.Config).Within(demonCowSearch).Name("Cow").Results()
}

func (l *liveCows) Crowd() int {
	return len(entityquery.Players(l.d.Session.State()).Within(cowCrowdRadius).Results())
}

func (l *liveCows) Logf(format string, args ...any) { logf(l.d, format, args...) }

// Enter walks to the pen's stand, which the walker does through the gate (it opens a shut door on the route). The radius is the one
// every tile of which is inside the pen: a walk that stops at the first tile inside a larger one stops on the road.
func (l *liveCows) Enter() bool {
	return questrunner.Travel(l.d, questplan.CowPastureStand, questplan.CowPastureEnterRadius)
}

func (l *liveCows) WalkTo(t tile.Tile) bool { return questrunner.Travel(l.d, t, 2) }

// Fight fights one cow and says why it gave a fight up, if it did.
func (l *liveCows) Fight(e entityquery.Entity) cowFight {
	d := l.d
	began := time.Now()
	why, skip := "", demonCowSkipFor
	giveUp := func() bool {
		cur, ok := entityquery.Npcs(d.Session.State(), d.Config).Within(demonCowSearch + 4).
			Where(func(n entityquery.Entity) bool { return n.Index == e.Index && n.ID == e.ID }).Nearest()
		if !ok {
			return false
		}
		here := questrunner.CurrentTile(d)
		w, s := cowGiveUp(cur, d.Session.State().Self(), began, time.Now(), l.ground.stepsTo(here, tile.Tile{X: cur.X, Z: cur.Z, Level: cur.Level}))
		if w != "" {
			why, skip = w, s
		}
		return w != ""
	}
	res := questrunner.FightChosen(d, e, l.sus, demonCowFightBudget, giveUp)
	return cowFight{Result: res, Why: why, Skip: skip, Took: time.Since(began)}
}

// Collect takes the bones a won fight left: they lie where the cow fell, which is beside the account (a cow chases) and a few ticks
// after the last blow. A rung buries them.
func (l *liveCows) Collect(entityquery.Entity) int {
	d := l.d
	here := questrunner.CurrentTile(d)
	before := questrunner.HeldID(d, objBones)
	if waitUntil(d, cowBonesWait, func() bool { return len(questrunner.GroundItemsNear(d, objBones, here, cowBonesNear)) > 0 }) {
		questrunner.TakeGround(d, objBones, here, cowBonesNear)
	}
	got := questrunner.HeldID(d, objBones) - before
	if l.bury {
		buryAll(d)
	}
	return max(got, 0)
}

// Loose takes a bone lying within reach of here: the walk to it is over open ground (a bone across the fence is not fetched).
func (l *liveCows) Loose(here tile.Tile) int {
	d := l.d
	now := time.Now()
	for _, g := range questrunner.GroundItemsNear(d, objBones, here, cowLooseNear) {
		at := tile.Tile{X: g.X, Z: g.Z, Level: g.Level}
		if !now.After(l.looseSkip[at]) {
			continue
		}
		if n := l.ground.stepsTo(here, at); n < 0 || n > cowLooseSteps {
			continue
		}
		before := questrunner.HeldID(d, objBones)
		if !questrunner.TakeGround(d, objBones, at, 0) {
			l.looseSkip[at] = now.Add(cowLooseSkipFor)
			continue
		}
		got := questrunner.HeldID(d, objBones) - before
		if l.bury {
			buryAll(d)
		}
		return max(got, 1)
	}
	return 0
}
