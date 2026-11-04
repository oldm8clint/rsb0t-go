package questmods

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/waterfallplan"
)

// ImpCatcher is Imp Catcher ready for the engine.
func ImpCatcher() questrunner.Quest { return questrunner.Quest{Module: questplan.ImpCatcher} }

// SkillMethods are the provisioner's skill sources the ported quests name.
func SkillMethods() map[string]questrunner.SkillMethod {
	return map[string]questrunner.SkillMethod{
		questplan.MethodImpFarm:     impFarm,
		questplan.MethodMineEssence: mineEssence,
		questplan.MethodChopLogs:    chopLogs,
		questplan.MethodChopOak:     chopOak,
		questplan.MethodMineSilver:  mineSilver,
		questplan.MethodSmithNails:  smithNails,
		questplan.MethodMakeGlass:   makeGlass,
		// Goblin Diplomacy's plain goblin mail: the Goblin Village's goblins drop it.
		questplan.MethodGoblinMail: goblinMail,
		// Demon Slayer's 25 bones for Traiborn: the Lumbridge cows' bones.
		questplan.MethodDemonBones: demonBones,
		// The Waterfall food top-up's own-skill source.
		waterfallplan.MethodThieveCake: thieveCake,
	}
}

const (
	// impFarmBudget bounds one farming run; the provisioner re-plans after it.
	impFarmBudget = 15 * time.Minute
	// impFarmMaxFailures ends the farm after this many fights in a row are lost.
	impFarmMaxFailures = 20
	// impFightBudget bounds one fight. An imp has 8 hitpoints and dies in two or three blows; one that is still there after
	// this has teleported somewhere the account is not getting to.
	impFightBudget = time.Minute
	// impSearch is how far from the account an imp may be and still be looked at; the client shows about this much.
	impSearch = 20
	// impReachWindow is the square (half its side) the open-ground flood covers around the account.
	impReachWindow = 34
	// impTakeSteps is how far, in steps over open ground, a bead on the ground is worth walking to.
	impTakeSteps = 24
	// impSkipFor is how long an imp the account gave up on is left alone.
	impSkipFor = 45 * time.Second
	// impFarmSayEvery paces the round's progress line.
	impFarmSayEvery = 3 * time.Minute
	// objAshes is the one thing an imp always drops (its death_drop), so a fight that leaves ashes beside the account killed.
	objAshes = 592
)

// impFarm kills imps at the site this account was given until every bead is held (a kill can drop any of them, so the run
// farms for the whole set, not only the task's line) or the budget runs out, and reports whether it gained a bead.
//
// What it does differently from the Ardougne-only farm it replaces (live 16 Oct 2026: 33 beads in 11.5 account hours from
// twelve accounts on nine imps; of the 15 runs that ended on their own, 4 completed and 8 ran out of their slot):
//   - The site is chosen once per slot among questplan.ImpSites by how many accounts already farm each per spawn, so twelve
//     accounts share seventeen imps, not nine.
//   - It fights only imps a walk over open ground reaches. An imp's teleport drops it anywhere within 20 tiles, which
//     is a room of the monastery for 5% of the ground round Ardougne's spawns and the Fight Arena's yard or Yanille's streets
//     for 16% of the ground round Yanille's.
//   - It takes the beads it needs from the ground wherever in view they lie, not only beside its own kills: a bead it does not
//     need drops all the same and, a hundred ticks later, anyone may take it.
//   - With no imp in reach it walks the site's spawn tiles, where a killed imp comes back, instead of idling at the middle.
//   - It waits out a random event rather than walking from the event's room, which the walker answers "unreachable" and the
//     engine counts as the quest being unable to reach its field.
//   - It reports a gain of any bead, not only the task's own line: a round that found a yellow bead for the red line's task
//     was reported as failed, marked the source exhausted and sent the provisioner back to the bank.
func impFarm(d *questrunner.Deps, task questplan.SourceTask) bool {
	site, farmers := impSiteFor(d)
	logf(d, "imp farm: %s (%d imp spawns); accounts farming now: %s", site.Name, len(site.Spawns), farmerLine(farmers))
	if !questrunner.Travel(d, site.Stand, 8) {
		return false
	}
	r := &impRun{d: d, site: site, skip: map[int]time.Time{}, skipTile: map[tile.Tile]bool{}}
	start, t0 := beadsHeld(d), time.Now()
	deadline := t0.Add(impFarmBudget)
	failed, saidAt := 0, t0
	for beadsMissing(d) && time.Now().Before(deadline) && questrunner.FreeSlots(d) > 0 {
		if !questrunner.WaitForGuardian(d) {
			break
		}
		if time.Since(saidAt) >= impFarmSayEvery {
			saidAt = time.Now()
			logf(d, "imp farm: %s, %s in: %d kills (ashes seen), %d beads taken from the ground; %d of %d beads held (+%d)",
				site.Name, time.Since(t0).Round(time.Second), r.kills, r.taken, beadsHeld(d), len(questplan.ImpBeads), beadsHeld(d)-start)
		}
		r.takeBeads()
		if !beadsMissing(d) {
			break
		}
		npcs, cands := r.candidates()
		i, ok := questplan.PickImp(site, cands)
		if !ok {
			if !r.patrol() {
				break
			}
			continue
		}
		r.idle = 0
		switch r.fight(npcs[i]) {
		case questrunner.FightStopped:
			failed = impFarmMaxFailures
		case questrunner.FightLost:
			// A fight that fails at once must not spin: live 4 Oct 2026 this loop was a quarter of the fleet's CPU.
			failed++
			if failed < impFarmMaxFailures && !d.Delay(2*time.Second) {
				failed = impFarmMaxFailures
			}
		default:
			failed = 0
		}
		if failed >= impFarmMaxFailures {
			if d.Delay(0) {
				logf(d, "imp farm: %d fights in a row lost; leaving %s", failed, site.Name)
			}
			break
		}
		r.takeBeads()
	}
	held := beadsHeld(d)
	logf(d, "imp farm: %s round over after %s: %d kills (ashes seen), %d imps lost to a teleport or the leash, %d given up, %d beads taken from the ground; %d of %d beads held (+%d)",
		site.Name, time.Since(t0).Round(time.Second), r.kills, r.vanished, r.gaveUp, r.taken, held, len(questplan.ImpBeads), held-start)
	return held > start
}

// impSiteFor is the site this account farms: the one it chose when it first asked in this slot (questrunner.SlotSite), the
// least crowded per spawn and, between equals, the nearest. farmers is how many accounts work each site now.
func impSiteFor(d *questrunner.Deps) (questplan.ImpSite, map[string]int) {
	from, known := questrunner.CurrentTileOK(d)
	now := time.Now()
	id := questrunner.SlotSite(questplan.ImpCatcherID, d, now, func(farmers map[string]int) string {
		return questplan.PickImpSite(farmers, from, known).ID
	})
	site, ok := questplan.ImpSiteByID(id)
	if !ok {
		site = questplan.ImpSiteArdougne
	}
	return site, questrunner.SlotFarmers(questplan.ImpCatcherID, now)
}

// farmerLine is farmers in the log's words, in the order of questplan.ImpSites.
func farmerLine(farmers map[string]int) string {
	var parts []string
	for _, s := range questplan.ImpSites {
		parts = append(parts, fmt.Sprintf("%s %d", s.ID, farmers[s.ID]))
	}
	return strings.Join(parts, ", ")
}

// beadsHeld is how many of the four kinds of bead the pack holds (one of a kind is all the quest takes).
func beadsHeld(d *questrunner.Deps) int {
	n := 0
	for _, b := range questplan.ImpBeads {
		if questrunner.HeldID(d, b) > 0 {
			n++
		}
	}
	return n
}

func beadsMissing(d *questrunner.Deps) bool { return beadsHeld(d) < len(questplan.ImpBeads) }

// impRun is one farming round's state.
type impRun struct {
	d    *questrunner.Deps
	site questplan.ImpSite
	// skip are imps given up on (by NPC index), until when; skipTile the ground items that could not be taken.
	skip     map[int]time.Time
	skipTile map[tile.Tile]bool
	// route is the patrol, next the stop it is at, idle how many looks in a row found no imp.
	route []tile.Tile
	next  int
	idle  int
	// The round's tally, for its closing log line.
	kills, vanished, gaveUp, taken int
}

// reach is the open ground around here, nil when the pack is not loaded or the account stands off it (the callers then
// fall back to distance).
func (r *impRun) reach(here tile.Tile) *pathfind.Reach {
	if r.d.Pack == nil {
		return nil
	}
	reach := r.d.Pack.ReachFrom(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, impReachWindow, nil)
	if reach.Size() == 0 {
		return nil
	}
	return reach
}

// stepsFrom is the steps from here to t over open ground, -1 when there is no such walk; without a flood it is the
// distance, which is how far the old farm's nearest-first pick looked.
func stepsFrom(here tile.Tile, reach *pathfind.Reach, t tile.Tile) int {
	if t.Level != here.Level {
		return -1
	}
	if reach == nil {
		return tile.Chebyshev(here, t)
	}
	if n, ok := reach.Steps(t.X, t.Z, t.Level); ok {
		return n
	}
	return -1
}

// candidates are the imps in view and, in the same order, how each stands for the farm (distance over open ground, busy).
func (r *impRun) candidates() ([]entityquery.Entity, []questplan.ImpCandidate) {
	d := r.d
	now := time.Now()
	npcs := entityquery.Npcs(d.Session.State(), d.Config).Within(impSearch).
		Where(func(e entityquery.Entity) bool { return e.ID == questplan.NpcImp && now.After(r.skip[e.Index]) }).Results()
	here := questrunner.CurrentTile(d)
	reach := r.reach(here)
	return npcs, impCandidatesFrom(npcs, d.Session.State().Self(), func(t tile.Tile) int { return stepsFrom(here, reach, t) })
}

// impCandidatesFrom turns the imps in view into the farm's candidates. steps is a walk's length to a tile over open ground
// (-1 for none); self is the account's player index, for telling an imp that faces it from one that faces another.
func impCandidatesFrom(npcs []entityquery.Entity, self int, steps func(tile.Tile) int) []questplan.ImpCandidate {
	out := make([]questplan.ImpCandidate, len(npcs))
	for i, e := range npcs {
		at := tile.Tile{X: e.X, Z: e.Z, Level: e.Level}
		busy := e.InCombat && self >= 0 && e.FaceEntity >= 32768 && e.FaceEntity-32768 != self
		out[i] = questplan.ImpCandidate{At: at, Steps: steps(at), Busy: busy}
	}
	return out
}

// fight fights one imp and keeps the tally.
func (r *impRun) fight(e entityquery.Entity) questrunner.FightResult {
	d := r.d
	ashesBefore, began := ashesInView(d), time.Now()
	giveUp := func() bool {
		cur, ok := entityquery.Npcs(d.Session.State(), d.Config).Within(impSearch + 4).
			Where(func(n entityquery.Entity) bool { return n.Index == e.Index && n.ID == e.ID }).Nearest()
		if !ok {
			return false
		}
		at := tile.Tile{X: cur.X, Z: cur.Z, Level: cur.Level}
		if !r.site.Has(at) {
			return true
		}
		here := questrunner.CurrentTile(d)
		return stepsFrom(here, r.reach(here), at) < 0
	}
	res := questrunner.FightChosen(d, e, questplan.Sustain{}, impFightBudget, giveUp)
	switch res {
	case questrunner.FightWon:
		if ashesInView(d) > ashesBefore {
			r.kills++
		} else {
			r.vanished++
			if time.Since(began) < 3*time.Second {
				// Gone from view as the fight began and nothing died: do not pick it again at once.
				r.skip[e.Index] = time.Now().Add(15 * time.Second)
			}
		}
	case questrunner.FightGaveUp, questrunner.FightTimedOut:
		r.gaveUp++
		r.skip[e.Index] = time.Now().Add(impSkipFor)
	}
	return res
}

// ashesInView counts the ashes in view (an imp's own drop: it always leaves them where it dies, and the killer sees them
// at once), so that a fight that adds some was a kill and one that does not was an imp that teleported away.
func ashesInView(d *questrunner.Deps) int {
	n := 0
	for _, g := range d.Session.State().GroundItems() {
		if g.ID == objAshes {
			n++
		}
	}
	return n
}

// patrol looks for imps when none is in reach: it walks to the next of the site's spawn tiles. False when the bot is
// stopping.
func (r *impRun) patrol() bool {
	d := r.d
	here := questrunner.CurrentTile(d)
	r.idle++
	if r.idle == 1 || r.idle%25 == 0 {
		logf(d, "imp farm: no imp in reach at %s; looking along its spawns", r.site.Name)
	}
	if here.Level != r.site.Stand.Level || tile.Chebyshev(here, r.site.Stand) > r.site.Leash+10 {
		questrunner.Travel(d, r.site.Stand, 8)
		return d.Delay(time.Second)
	}
	if len(r.route) == 0 {
		r.route = r.site.Patrol(here)
	}
	stop := r.route[r.next%len(r.route)]
	r.next++
	if tile.Chebyshev(here, stop) > 5 && !questrunner.WalkNear(d, stop, 3) {
		questrunner.Travel(d, r.site.Stand, 8)
	}
	return d.Delay(1500 * time.Millisecond)
}

// takeBeads takes the beads the pack lacks from the ground, wherever in view they lie within a short walk over open
// ground: the killer's own drop is private to it for a hundred ticks, and a drop of a kind it already holds is then every
// account's.
func (r *impRun) takeBeads() {
	d := r.d
	for tries := 0; tries < 6 && questrunner.FreeSlots(d) > 0; tries++ {
		here := questrunner.CurrentTile(d)
		reach := r.reach(here)
		g, ok := pickBead(d.Session.State().GroundItems(), func(id int) int { return questrunner.HeldID(d, id) },
			func(t tile.Tile) int { return stepsFrom(here, reach, t) }, impTakeSteps, r.skipTile)
		if !ok {
			return
		}
		at := tile.Tile{X: g.X, Z: g.Z, Level: g.Level}
		if !questrunner.TakeGround(d, g.ID, at, 0) {
			r.skipTile[at] = true
			continue
		}
		r.taken++
		logf(d, "imp farm: took %s from the ground at %d,%d (%d of %d beads held)", objName(d, g.ID), g.X, g.Z, beadsHeld(d), len(questplan.ImpBeads))
	}
}

// objName names an object for the log.
func objName(d *questrunner.Deps, id int) string {
	if d.Config != nil {
		if o, ok := d.Config.Obj(id); ok && o.Name != "" {
			return "a " + strings.ToLower(o.Name)
		}
	}
	return fmt.Sprintf("object %d", id)
}

// pickBead is the nearest bead on the ground (by steps over open ground) of a kind the pack does not hold, within maxSteps
// and not on a tile in skip.
func pickBead(items []gamestate.GroundItem, held func(id int) int, steps func(tile.Tile) int, maxSteps int, skip map[tile.Tile]bool) (gamestate.GroundItem, bool) {
	var best gamestate.GroundItem
	bestSteps, found := 0, false
	for _, g := range items {
		if !isBead(g.ID) || held(g.ID) > 0 {
			continue
		}
		at := tile.Tile{X: g.X, Z: g.Z, Level: g.Level}
		if skip[at] {
			continue
		}
		n := steps(at)
		if n < 0 || n > maxSteps {
			continue
		}
		if !found || n < bestSteps {
			best, bestSteps, found = g, n, true
		}
	}
	return best, found
}

func isBead(id int) bool {
	for _, b := range questplan.ImpBeads {
		if b == id {
			return true
		}
	}
	return false
}
