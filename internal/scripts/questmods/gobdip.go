package questmods

import (
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// GoblinDiplomacy is Goblin Diplomacy ready for the engine. Every step of its module is a generic one (the talks to the
// bartender, Wyson, Aggie and the generals, picking onions, the dyes on the mail) or the provisioner's (the pantry, the
// redberries at the Varrock patch or Wydin's shelf); the one live executor is the plain mail's skill source, goblinMail.
func GoblinDiplomacy() questrunner.Quest { return questrunner.Quest{Module: questplan.GoblinDiplomacy} }

// goblinMailBudget bounds one farming run; the provisioner re-plans after it. The village's goblins drop the mail 10 in
// 128, so the three the quest needs take 38 kills on average (a few minutes), and this is several times that.
const goblinMailBudget = 12 * time.Minute

// goblinMailMaxFailures ends the farm after this many fights in a row fail to start.
const goblinMailMaxFailures = 20

// goblinMailSearch is how far from the account a village goblin may be and still be chosen.
const goblinMailSearch = 24

const (
	// goblinFightBudget bounds one fight. A village goblin has 12 hitpoints and dies in a few blows (the log's median
	// gap between two kills is about twenty seconds, walking included); one still standing after this is one the account
	// is not reaching. It was the shared fight step's three minutes, and 181 fights ran them out in four hours of the
	// fleet's log (live 17 Oct 2026: 33,398 of the farm's 63,153 seconds), each for nothing.
	goblinFightBudget = time.Minute
	// goblinNoHitAfter is how long a fight may go without a single hit landing on the goblin (a health bar drawn) before
	// the account gives it up: a goblin behind a wall or a door never gets one.
	goblinNoHitAfter = 30 * time.Second
	// goblinBusyAfter is how long into a fight a goblin that another player is fighting is given up: the village is single
	// combat, so the attack on it is refused ("Someone else is fighting that") however often it is sent.
	goblinBusyAfter = 4 * time.Second
	// goblinSkipFor is how long a goblin the account gave up on is left alone (by NPC index), goblinSkipBusy how long one that
	// another player was fighting: that fight is over in half a minute, where a goblin behind a wall stays there.
	goblinSkipFor  = 2 * time.Minute
	goblinSkipBusy = 20 * time.Second
	// goblinReachWindow is the square (half its side) the open-ground flood covers around the account: the village is 34
	// tiles from the first goblin spawn to the last.
	goblinReachWindow = 34
	// goblinReachEvery is the longest a flood is reused while the account stands where it was taken: the fight loop asks
	// every 600 ms, and a flood is a few thousand tiles.
	goblinReachEvery = 2 * time.Second
	// goblinSayEvery paces the round's progress line.
	goblinSayEvery = 3 * time.Minute
)

// goblinMail kills Goblin Village goblins and takes the goblin mail they drop until the task's quantity more is held (or
// the budget runs out). The goblins are level 5 with 12 hitpoints and a max hit of 2: the quest's module sustain eats if
// anything goes wrong, and a lost fight walks away and counts toward the quest's park. It never goes north of the
// village (questplan.GoblinVillageNorth): the Wilderness line is six tiles beyond the last goblin.
//
// What it does differently from the farm it replaces (live 17 Oct 2026, the fleet's log from 18:02: 105 rounds of up to
// twelve minutes made 110 mail in 1,218 farm-minutes, 11.1 minutes a mail where the village's table says 4.6; 45 of the
// 105 made none; 181 fights ran into the shared step's three-minute limit and burned 53% of the farm's seconds, and 115 of
// those were preceded by "no path ... unreachable"):
//   - It fights only goblins a walk over open ground reaches from where the account stands. Two of the village's spawns
//     (2951,3501 and 2952,3507) are inside huts whose doors are shut in the collision data, and FightNpc took the nearest
//     goblin by distance: the walk to it failed, the attack was sent anyway and went nowhere ("I can't reach that!"), and
//     the same goblin stayed the nearest, so the same account repeated it for the whole round. This is the Imp Catcher
//     farm's fix (questmods/imp.go, questrunner/fightchosen.go).
//   - A fight is a minute at most, is given up when no blow has landed in thirty seconds or when another player is the one
//     fighting the goblin, and the goblin is then left alone for two minutes (twenty seconds when it was another player's
//     fight, which is soon over).
//   - It waits out a random event (questrunner.WaitForGuardian) rather than fighting through it.
func goblinMail(d *questrunner.Deps, task questplan.SourceTask) bool {
	field := task.Source.Tile
	if !questrunner.Travel(d, field, 6) {
		return false
	}
	start := questrunner.HeldID(d, questplan.ObjGoblinMail)
	want := start + task.Qty
	t0 := time.Now()
	deadline := t0.Add(goblinMailBudget)
	logf(d, "goblin mail: farming the Goblin Village for %d (holding %d), up to %s", task.Qty, start, goblinMailBudget)
	r := &goblinRun{d: d, skip: map[int]time.Time{}}
	failed, saidAt := 0, t0
	for questrunner.HeldID(d, questplan.ObjGoblinMail) < want && time.Now().Before(deadline) && questrunner.FreeSlots(d) > 0 {
		if !questrunner.WaitForGuardian(d) {
			break
		}
		if time.Since(saidAt) >= goblinSayEvery {
			saidAt = time.Now()
			logf(d, "goblin mail: %s in: %d fights won, %d goblins given up on, %d timed out; %d of %d taken",
				time.Since(t0).Round(time.Second), r.won, r.gaveUp, r.timedOut, questrunner.HeldID(d, questplan.ObjGoblinMail)-start, task.Qty)
		}
		// Mail lying about from a kill (ours, whose pickup missed it, or another account's).
		if here := questrunner.CurrentTile(d); here.Z <= questplan.GoblinVillageNorth && questrunner.TakeGround(d, questplan.ObjGoblinMail, here, 6) {
			continue
		}
		g, ok := r.pick()
		if !ok {
			// No goblin in reach: stand at the field, where the spawns are, and look again.
			r.idle++
			if r.idle == 1 || r.idle%20 == 0 {
				logf(d, "goblin mail: no goblin in reach of %v; looking along the village", questrunner.CurrentTile(d))
			}
			if tile.Chebyshev(questrunner.CurrentTile(d), field) > 6 {
				questrunner.Travel(d, field, 4)
			}
			d.Delay(3 * time.Second)
			continue
		}
		r.idle = 0
		switch r.fight(g) {
		case questrunner.FightStopped:
			failed = goblinMailMaxFailures
		case questrunner.FightLost:
			// A fight that fails at once must not spin: live 4 Oct 2026 the imp farm's identical loop was a quarter of the
			// fleet's CPU.
			failed++
			if failed < goblinMailMaxFailures && !d.Delay(2*time.Second) {
				failed = goblinMailMaxFailures
			}
		default:
			failed = 0
		}
		if failed >= goblinMailMaxFailures {
			if d.Delay(0) {
				logf(d, "goblin mail: %d fights in a row failed; leaving the village", failed)
			}
			break
		}
		questrunner.TakeGround(d, questplan.ObjGoblinMail, questrunner.CurrentTile(d), 4)
	}
	got := questrunner.HeldID(d, questplan.ObjGoblinMail) - start
	logf(d, "goblin mail: %d of %d taken from the village goblins in %s (%d fights won, %d goblins given up on, %d timed out)",
		got, task.Qty, time.Since(t0).Round(time.Second), r.won, r.gaveUp, r.timedOut)
	return got > 0
}

// goblinRun is one farming round's state.
type goblinRun struct {
	d *questrunner.Deps
	// skip are goblins given up on (by NPC index), until when.
	skip map[int]time.Time
	// reach is the open ground around reachFrom, taken at reachAt; the flood is reused while the account stands still.
	reach     *pathfind.Reach
	reachFrom tile.Tile
	reachAt   time.Time
	// idle is how many looks in a row found no goblin.
	idle int
	// The round's tally, for its progress and closing lines.
	won, gaveUp, timedOut int
}

// floodFrom is the open ground around here, nil when the pack is not loaded or the account stands off it (the callers then
// fall back to distance). It is the flood taken within goblinReachEvery at the same tile when there is one.
func (r *goblinRun) floodFrom(here tile.Tile) *pathfind.Reach {
	if r.d.Pack == nil {
		return nil
	}
	if r.reach != nil && r.reachFrom == here && time.Since(r.reachAt) < goblinReachEvery {
		return r.reach
	}
	reach := r.d.Pack.ReachFrom(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, goblinReachWindow, nil)
	if reach.Size() == 0 {
		reach = nil
	}
	r.reach, r.reachFrom, r.reachAt = reach, here, time.Now()
	return reach
}

// stepsTo is the steps from here to the goblin at t: see interactSteps.
func (r *goblinRun) stepsTo(here, t tile.Tile) int {
	return interactSteps(r.d.Pack, r.floodFrom(here), here, t)
}

// interactSteps is the steps from here to a tile the engine accepts a melee attack on whatever stands at t from (the pack's own
// rule, Pack.InteractTiles: t itself, or a neighbour it touches with no wall between), over the open ground reach covers; -1
// when there is none. Not the steps to t's own tile: a goblin may stand on a tile the baked collision calls blocked (the spawn
// at 2951,3481 is one) and be as fightable as any from the tile beside it. Without a flood (no pack) it is the distance.
func interactSteps(pk *pathfind.Pack, reach *pathfind.Reach, here, t tile.Tile) int {
	if pk == nil || reach == nil {
		return stepsFrom(here, reach, t)
	}
	if t.Level != here.Level {
		return -1
	}
	best := -1
	for _, p := range pk.InteractTiles(pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level}) {
		if n, ok := reach.Steps(p.X, p.Z, p.Level); ok && (best < 0 || n < best) {
			best = n
		}
	}
	return best
}

// pick is the goblin to fight now, among those in view.
func (r *goblinRun) pick() (entityquery.Entity, bool) {
	d := r.d
	npcs := entityquery.Npcs(d.Session.State(), d.Config).Within(goblinMailSearch).Results()
	here := questrunner.CurrentTile(d)
	g, _, ok := pickGoblin(npcs, d.Session.State().Self(), time.Now(), r.skip, func(t tile.Tile) int { return r.stepsTo(here, t) })
	return g, ok
}

// pickGoblin is the village goblin to fight among npcs: one of the farm's goblins, south of the village's northern edge, not
// given up on lately (skip, by NPC index, until when), not in another player's fight, and a walk over open ground away
// (steps, -1 for none). The nearest by that walk wins, and between equals the lower NPC index, so two accounts looking at the
// same village do not oscillate. It answers its steps too.
func pickGoblin(npcs []entityquery.Entity, self int, now time.Time, skip map[int]time.Time, steps func(tile.Tile) int) (entityquery.Entity, int, bool) {
	ids := map[int]bool{}
	for _, id := range questplan.GobdipFarmGoblinIDs {
		ids[id] = true
	}
	var best entityquery.Entity
	bestSteps, found := 0, false
	for _, e := range npcs {
		if !ids[e.ID] || e.Z > questplan.GoblinVillageNorth || !now.After(skip[e.Index]) || goblinBusy(e, self) {
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
	return best, bestSteps, found
}

// goblinBusy reports a goblin that another player is fighting: it faces that player and has just been hit. One that merely
// still faces the last player to hit it is free, and so is every goblin when the account's own index is unknown.
func goblinBusy(e entityquery.Entity, self int) bool {
	return e.InCombat && self >= 0 && e.FaceEntity >= 32768 && e.FaceEntity-32768 != self
}

// goblinGiveUp says why a fight with cur (the goblin as it stands now), begun at began, is not worth going on with, "" when it
// is, and how long the goblin is then left alone. steps is the walk to it over open ground now (-1 for none).
func goblinGiveUp(cur entityquery.Entity, self int, began, now time.Time, steps int) (why string, skip time.Duration) {
	since := now.Sub(began)
	switch {
	case cur.Z > questplan.GoblinVillageNorth:
		return "it has wandered north of the village", goblinSkipFor
	case steps < 0:
		return "no walk over open ground reaches it", goblinSkipFor
	case since >= goblinBusyAfter && goblinBusy(cur, self):
		return "another player is fighting it", goblinSkipBusy
	case since >= goblinNoHitAfter && cur.TotalHealth == 0:
		return "no blow has landed on it", goblinSkipFor
	}
	return "", 0
}

// fight fights one goblin and keeps the tally.
func (r *goblinRun) fight(e entityquery.Entity) questrunner.FightResult {
	d := r.d
	began := time.Now()
	why, skip := "", goblinSkipFor
	giveUp := func() bool {
		cur, ok := entityquery.Npcs(d.Session.State(), d.Config).Within(goblinMailSearch + 4).
			Where(func(n entityquery.Entity) bool { return n.Index == e.Index && n.ID == e.ID }).Nearest()
		if !ok {
			return false
		}
		here := questrunner.CurrentTile(d)
		w, s := goblinGiveUp(cur, d.Session.State().Self(), began, time.Now(), r.stepsTo(here, tile.Tile{X: cur.X, Z: cur.Z, Level: cur.Level}))
		if w != "" {
			why, skip = w, s
		}
		return w != ""
	}
	res := questrunner.FightChosen(d, e, questplan.GoblinDiplomacy.Sustain, goblinFightBudget, giveUp)
	switch res {
	case questrunner.FightWon:
		r.won++
	case questrunner.FightGaveUp:
		r.gaveUp++
		r.skip[e.Index] = time.Now().Add(skip)
		logf(d, "goblin mail: gave up on the goblin at %d,%d after %s: %s", e.X, e.Z, time.Since(began).Round(time.Second), why)
	case questrunner.FightTimedOut:
		r.timedOut++
		r.skip[e.Index] = time.Now().Add(goblinSkipFor)
		logf(d, "goblin mail: the goblin at %d,%d is still standing after %s; leaving it for %s", e.X, e.Z, goblinFightBudget, goblinSkipFor)
	}
	return res
}
