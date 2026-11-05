package questmods

import (
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// fieldHunt is one hunt of a kind of monster in a field of the map for what it leaves: Big Chompy Bird Hunting's Feldip wolves (their bones) and Shades of Mort'ton's Loar shades
// (their remains). It is the shape of Druidic Ritual's meat hunt (druid.go) with the kind, the field and the drop as parameters: the foe is chosen by a walk over open ground to a
// tile the engine accepts an attack from, never by distance; a fight is given up when no blow has landed in a while or when another player is the one fighting the monster; a
// lost fight ends the hunt (the engine counts it against the quest and walks the account to a bank). Whoever starts it has priced the foe (questplan's Decide) and refused the hunt
// when the model loses.
type fieldHunt struct {
	d     *questrunner.Deps
	label string
	// ids are the monster's npc ids (a shade is a shadow until it hunts: both), field and radius where it is hunted, drop the object it leaves and done says the hunt has what it came for.
	ids    map[int]bool
	field  tile.Tile
	radius int
	drop   int
	done   func() bool
	sus    questplan.Sustain
	// fightBudget bounds one fight.
	fightBudget time.Duration
	skip        map[int]time.Time
	// reach is the open ground round reachFrom, taken at reachAt; the flood is reused while the account stands still.
	reach                          *pathfind.Reach
	reachFrom                      tile.Tile
	reachAt                        time.Time
	idle                           int
	won, gaveUp, timedOut, nothing int
}

const (
	// fhSearch is how far from the account a monster may be and still be chosen; fhStray how far beyond the field's radius it may stand.
	fhSearch = 22
	fhStray  = 5
	// fhReachWindow is the square (half its side) the open-ground flood covers; fhReachEvery the longest a flood is reused while the account stands still.
	fhReachWindow = 34
	fhReachEvery  = 2 * time.Second
	// fhIdleLooks is how many looks in a row may find no monster before the hunt walks back to its field.
	fhIdleLooks = 8
	// fhNoHitAfter is how long a fight may go without a blow landing (a health bar drawn) before the monster is given up; fhBusyAfter how long into one a monster another player is
	// fighting is given up.
	fhNoHitAfter = 40 * time.Second
	fhBusyAfter  = 4 * time.Second
	// fhSkipFor and fhSkipBusy are how long a monster given up on is left alone (by NPC index), and one another player was fighting.
	fhSkipFor  = 2 * time.Minute
	fhSkipBusy = 20 * time.Second
	// fhSayEvery paces the hunt's progress line.
	fhSayEvery = 3 * time.Minute
)

func (h *fieldHunt) floodFrom(here tile.Tile) *pathfind.Reach {
	if h.d.Pack == nil {
		return nil
	}
	if h.reach != nil && h.reachFrom == here && time.Since(h.reachAt) < fhReachEvery {
		return h.reach
	}
	reach := h.d.Pack.ReachFrom(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, fhReachWindow, nil)
	if reach.Size() == 0 {
		reach = nil
	}
	h.reach, h.reachFrom, h.reachAt = reach, here, time.Now()
	return reach
}

func (h *fieldHunt) stepsTo(here, t tile.Tile) int {
	return interactSteps(h.d.Pack, h.floodFrom(here), here, t)
}

// pick is the monster to fight now, among those in view: no other player is fighting it, not given up on, not strayed from the field, and a walk over open ground away. The nearest by
// that walk wins, and between equals the lower NPC index, so two accounts looking at the same field do not oscillate.
func (h *fieldHunt) pick() (entityquery.Entity, bool) {
	d := h.d
	now, self, here := time.Now(), d.Session.State().Self(), questrunner.CurrentTile(d)
	var best entityquery.Entity
	bestSteps, found := 0, false
	for _, e := range entityquery.Npcs(d.Session.State(), d.Config).Within(fhSearch).Results() {
		at := tile.Tile{X: e.X, Z: e.Z, Level: e.Level}
		if !h.ids[e.ID] || !now.After(h.skip[e.Index]) || goblinBusy(e, self) || tile.Chebyshev(at, h.field) > h.radius+fhStray {
			continue
		}
		n := h.stepsTo(here, at)
		if n < 0 {
			continue
		}
		if !found || n < bestSteps || (n == bestSteps && e.Index < best.Index) {
			best, bestSteps, found = e, n, true
		}
	}
	return best, found
}

// fieldGiveUp says why a fight with cur (the monster as it stands now), begun at began, is not worth going on with, "" when it is, and how long the monster is then left alone. steps
// is the walk to it over open ground now (-1 for none).
func fieldGiveUp(cur entityquery.Entity, self int, began, now time.Time, steps int, field tile.Tile, radius int) (why string, skip time.Duration) {
	since := now.Sub(began)
	switch {
	case tile.Chebyshev(tile.Tile{X: cur.X, Z: cur.Z, Level: cur.Level}, field) > radius+2*fhStray:
		return "it has wandered off the field", fhSkipFor
	case steps < 0:
		return "no walk over open ground reaches it", fhSkipFor
	case since >= fhBusyAfter && goblinBusy(cur, self):
		return "another player is fighting it", fhSkipBusy
	case since >= fhNoHitAfter && cur.TotalHealth == 0:
		return "no blow has landed on it", fhSkipFor
	}
	return "", 0
}

// fight fights one monster and takes what it dropped.
func (h *fieldHunt) fight(e entityquery.Entity) questrunner.FightResult {
	d := h.d
	began := time.Now()
	why, skip := "", fhSkipFor
	last := tile.Tile{X: e.X, Z: e.Z, Level: e.Level}
	giveUp := func() bool {
		cur, ok := entityquery.Npcs(d.Session.State(), d.Config).Within(fhSearch + 4).Where(func(n entityquery.Entity) bool { return n.Index == e.Index }).Nearest()
		if !ok {
			return false
		}
		last = tile.Tile{X: cur.X, Z: cur.Z, Level: cur.Level}
		w, s := fieldGiveUp(cur, d.Session.State().Self(), began, time.Now(), h.stepsTo(questrunner.CurrentTile(d), last), h.field, h.radius)
		if w != "" {
			why, skip = w, s
		}
		return w != ""
	}
	res := questrunner.FightChosen(d, e, h.sus, h.fightBudget, giveUp)
	switch res {
	case questrunner.FightWon:
		h.won++
		// The monster drops what it leaves where it dies, which is where it was last seen.
		if questrunner.TakeGround(d, h.drop, last, 3) || h.done() {
			logf(d, "%s: took the drop of a kill at %d,%d", h.label, last.X, last.Z)
		} else {
			h.nothing++
			logf(d, "%s: the one at %d,%d died and nothing was found beside it", h.label, last.X, last.Z)
		}
	case questrunner.FightGaveUp:
		h.gaveUp++
		h.skip[e.Index] = time.Now().Add(skip)
		logf(d, "%s: gave up on the one at %d,%d after %s: %s", h.label, e.X, e.Z, time.Since(began).Round(time.Second), why)
	case questrunner.FightTimedOut:
		h.timedOut++
		h.skip[e.Index] = time.Now().Add(fhSkipFor)
		logf(d, "%s: the one at %d,%d is still standing after %s; leaving it for %s", h.label, e.X, e.Z, h.fightBudget, fhSkipFor)
	}
	return res
}

// run hunts until done holds or the budget runs out, and reports whether it does.
func (h *fieldHunt) run(budget time.Duration) bool {
	d := h.d
	if h.skip == nil {
		h.skip = map[int]time.Time{}
	}
	if h.done() {
		return true
	}
	if !questrunner.Travel(d, h.field, 6) {
		logf(d, "%s: the field at %v was not reached from %v", h.label, h.field, questrunner.CurrentTile(d))
		return false
	}
	t0 := time.Now()
	deadline := t0.Add(budget)
	saidAt := t0
	logf(d, "%s: hunting in the field at %v, up to %s", h.label, h.field, budget)
	for !h.done() && time.Now().Before(deadline) {
		if !questrunner.WaitForGuardian(d) {
			break
		}
		if time.Since(saidAt) >= fhSayEvery {
			saidAt = time.Now()
			logf(d, "%s: %s in: %d killed, %d given up on, %d timed out, %d drops missed", h.label, time.Since(t0).Round(time.Second), h.won, h.gaveUp, h.timedOut, h.nothing)
		}
		// A drop lying about from a kill whose pickup missed it (ours or another account's: it is the same item).
		if h.drop > 0 && questrunner.TakeGround(d, h.drop, questrunner.CurrentTile(d), 6) && h.done() {
			break
		}
		target, ok := h.pick()
		if !ok {
			h.idle++
			if h.idle == 1 || h.idle%20 == 0 {
				logf(d, "%s: none in reach of %v; looking again", h.label, questrunner.CurrentTile(d))
			}
			if h.idle%fhIdleLooks == 0 || tile.Chebyshev(questrunner.CurrentTile(d), h.field) > h.radius {
				questrunner.Travel(d, h.field, 6)
			}
			if !d.Delay(3 * time.Second) {
				break
			}
			continue
		}
		h.idle = 0
		switch h.fight(target) {
		case questrunner.FightStopped:
			return h.done()
		case questrunner.FightLost:
			logf(d, "%s: the fight was lost; ending the hunt", h.label)
			return h.done()
		}
	}
	logf(d, "%s: the hunt ends after %s with %d killed, %d given up on, %d timed out: done %v", h.label, time.Since(t0).Round(time.Second), h.won, h.gaveUp, h.timedOut, h.done())
	return h.done()
}
