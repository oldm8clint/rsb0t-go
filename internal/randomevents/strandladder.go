package randomevents

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/doorescape"
)

// The escape ladder: what to try, in what order, how often, and when to stop, for an account standing
// in a StrandSite. Pure (time and every reading come in through StrandObs), so the whole policy is
// unit-tested, and the Guardian's glue (strand_exec.go) only reads, calls Next and performs the one
// action it names.
//
// Rungs, tried in this order on every poll:
//
//  1. Eat. Only at a Lethal site, or while attacked, and only below holdGroundEatBelow of maximum
//     hitpoints with food in the pack. The King Black Dragon's breath does up to 65, so the pack is
//     the only thing standing between an arrival at 1 tile from it and the lever 15 tiles away.
//  2. Teleport. A spell the account can pay for (PickTeleport), from a site that allows one, at
//     most strandTeleportCasts times per visit and strandTeleportEvery apart. It beats every walk:
//     nothing is carried across a floor or a lever. A site marked ExitFirst (the Gnome course's
//     platforms, whose own obstacles are a free way down) holds it back until the exit has failed
//     strandExitFirstClicks times.
//  3. The exit step the site catalogues for the tile the account stands on (stairs, a door, a
//     lever), stand tile by stand tile; each click is judged by where the account is on a LATER poll
//     (a level change, or a move of MinMove tiles, or out of the room past a door). A site with an
//     Escape (Rogues' Castle) has no fixed list: this rung plans the way out on every poll from the
//     account's tile and the doors as they stand (escape below).
//  4. Give up, loudly and boundedly. strandClicksPerRound failed clicks end a round; the ladder then
//     sleeps strandBackoff[round] and starts the next, and after len(strandBackoff) sleeps it stops
//     for the rest of the visit and says STRANDED once. It never loops forever and it never holds the
//     account longer than a bounded time: Busy (which the scripts wait on) is false while it sleeps
//     and after it has given up.
//
// Relogging is not a rung. A login restores the saved position (content/scripts/login_logout/login.rs2
// moves nobody), so a relog cannot take an account off a roof; the fleet watchdog's relogin remains
// the cure for a stuck interface, which is a different fault.
const (
	strandClicksPerRound = 9
	strandTeleportCasts  = 2
	strandTeleportEvery  = 20 * time.Second
	// strandClickEvery spaces exit clicks and is how long one is given to land before the next poll
	// judges it: a stair or a lever takes a few ticks.
	strandClickEvery = 3 * time.Second
	// strandDoorSettle is how long a door is given to open before the account walks through it.
	strandDoorSettle = 1200 * time.Millisecond
	// strandWalkEvery spaces the walks onto a stand tile.
	strandWalkEvery = 2400 * time.Millisecond
	// strandEatEvery spaces bites: food is eaten on a tick delay.
	strandEatEvery = 1800 * time.Millisecond
	// strandGrace is how long an account stands in a non-lethal site before the ladder acts, so a
	// script's own travel (and a random event's handler, which owns the account first) gets the
	// first look. A lethal site has none.
	strandGrace = 4 * time.Second
	// strandWalksPerStand bounds the walk attempts toward one stand tile before the next is tried.
	strandWalksPerStand = 3
	// strandExitFirstClicks is how many failed exit clicks a site marked ExitFirst allows before the ladder may cast a
	// teleport.
	strandExitFirstClicks = 3

	// The escape rung's timings. An account on open ground inside a site's walls is left to its script for
	// escapeStayLimit and then escorted out; a refusal (a walk that never moved, a click that changed no door
	// and no level) is remembered for escapeSuspectFor so the next plan goes another way; one walk click carries
	// at most escapeWalkTiles tiles (the client's own limit is 25 corners, and every tile of an escape walk is a
	// corner, so that the server takes exactly the steps the plan counted); a walk that has not moved the
	// account for escapeWalkStall is refused, and an account that moved within escapeMovingFor is walking. A round
	// of the escape ends after escapeRoundLimit however it is going (a way out takes a minute at the very most:
	// the roof's three staircases and the door are about fifteen seconds), so that no loop of plans, walks and
	// clicks that never leaves can hold the scripts for longer than that before the ladder sleeps.
	escapeStayLimit  = 45 * time.Second
	escapeSuspectFor = 2 * time.Minute
	escapeWalkTiles  = 24
	escapeWalkStall  = 3 * time.Second
	escapeMovingFor  = 1200 * time.Millisecond
	escapeRoundLimit = 4 * time.Minute
	// A door is never shut while waiting would do. An open leaf that walls an account in goes back by itself 500
	// ticks (five minutes) after the door was opened, and a close is how accounts fight over a door, so a plan that
	// needs one waits escapeTrapWait, looking again every escapeTrapPoll, before it may use it.
	escapeTrapWait = 6 * time.Minute
	escapeTrapPoll = 20 * time.Second
)

// strandBackoff is the sleep after each failed round; the last round ends the visit.
var strandBackoff = []time.Duration{30 * time.Second, 2 * time.Minute, 6 * time.Minute, 15 * time.Minute}

// StrandRung names what the ladder decided.
type StrandRung string

const (
	RungNone     StrandRung = ""
	RungEat      StrandRung = "eat"
	RungTeleport StrandRung = "teleport"
	RungWalk     StrandRung = "walk"         // onto the next stand tile
	RungClick    StrandRung = "click"        // the step's loc op
	RungThrough  StrandRung = "walk-through" // a door's far side
	RungWait     StrandRung = "wait"         // nothing to do this poll, and not finished
	RungGiveUp   StrandRung = "give-up"
)

// StrandObs is everything the ladder reads on one poll.
type StrandObs struct {
	Now          time.Time
	X, Z, Level  int
	Site         *StrandSite // nil when the account is in no site
	HP, MaxHP    int
	HPKnown      bool
	Attacked     bool   // underAttack
	HasFood      bool   // an eatable item is in the pack
	DialogueOpen bool   // eating closes a dialogue, and an event's must not be dismissed
	Spell        string // the teleport PickTeleport chose, "" when none can be cast
	// Escape is what a site with an Escape needs besides the position: its catalogue and the doors' leaves.
	// nil for every other site.
	Escape *EscapeObs
}

// EscapeObs is what the escape rung reads beside the position: the site's door catalogue and the state of its
// doors as the client's own loc changes say they are (doorescape.Catalog.StateFromChanges).
type EscapeObs struct {
	Cat   *doorescape.Catalog
	State doorescape.State
}

// StrandAction is the ladder's answer.
type StrandAction struct {
	Rung  StrandRung
	Step  *ExitStep
	Stand StrandTile
	Spell string
	// Path is a RungWalk's tiles, each one legal step from the one before and the first one step from where the
	// account stands: the escape rung sends exactly these, so the server takes the steps the plan counted. Empty
	// for a walk onto a stand tile, which the executor routes itself.
	Path []StrandTile
	// Note is the one-line reason, for the log.
	Note string
	// Hold is set on a RungWait that is the ladder keeping the account for itself: an action in the
	// air, or the gap before the next walk or click. The Guardian must not let any other handler move
	// the account on such a poll (a flee from a Swarm on the roof, say, would walk it off the stand tile
	// the ladder is waiting on). It is not set while the ladder is asleep, still in its grace, or has
	// nothing to do, when the account is free to be handled as before.
	Hold bool
}

// hold is a RungWait that holds the account.
func hold(note string) StrandAction { return StrandAction{Rung: RungWait, Note: note, Hold: true} }

// StrandLadder is one account's memory of its current visit to a site. The zero value is ready.
type StrandLadder struct {
	site    string
	since   time.Time
	started bool

	lastEat, lastTele, lastWalk, lastClick time.Time

	casts          int
	stepName       string // the step the counters below belong to
	standIdx       int
	walks          int
	walkDist       int // distance to the stand tile when the last walk click was sent
	clicks         int // failed clicks this round
	clicksLifetime int
	round          int
	sleepUntil     time.Time
	gaveUp         bool
	gaveUpLogged   bool

	// pending is the position the last click, walk-through or cast was sent from; a later poll judges it.
	pending     bool
	pendingFrom StrandTile
	pendingStep *ExitStep
	pendingAt   time.Time
	pendingCast bool
	// doorStage is 1 once a door's Open was sent and 2 once the walk through it was.
	doorStage int

	// The escape rung's own memory (see escape). escapeSite is set for a visit to a site with an Escape;
	// escaping is whether the ladder has taken the account (false while it waits out escapeStayLimit on open
	// ground, so scripts are not held); announced is whether the arrival line has been taken.
	escapeSite, escaping, announced bool
	esc                             escapeMem
	// roundAt is when the current round of the escape began (zero between rounds).
	roundAt time.Time
}

// escapeMem is what the escape rung remembers between polls.
type escapeMem struct {
	lastPos    doorescape.Tile
	lastMoveAt time.Time

	// The walk click in flight.
	walkLive bool
	walkAt   time.Time
	walkFrom doorescape.Tile
	walkTo   doorescape.Tile
	walkPath []doorescape.Tile

	// The door or staircase click in flight, and the doors' leaves and the level as it was sent.
	opLive   bool
	opAt     time.Time
	opStep   doorescape.Step
	opBefore doorescape.State
	opLevel  int

	// What refusals taught: operations that did nothing from a stand, steps the server would not take.
	badOps   map[doorescape.OpKey]time.Time
	badSteps map[[2]doorescape.Tile]time.Time

	// trapAt is when the account was first found walled in by a leaf that only a shut door would get it past
	// (zero while a plan with no shut door exists).
	trapAt time.Time

	ops int // door and staircase clicks sent this visit
}

func (e *escapeMem) forget() {
	e.badOps, e.badSteps = nil, nil
}

func (e *escapeMem) liveBadOps(now time.Time) map[doorescape.OpKey]bool {
	var out map[doorescape.OpKey]bool
	for k, until := range e.badOps {
		if !now.Before(until) {
			delete(e.badOps, k)
			continue
		}
		if out == nil {
			out = map[doorescape.OpKey]bool{}
		}
		out[k] = true
	}
	return out
}

func (e *escapeMem) liveBadSteps(now time.Time) map[[2]doorescape.Tile]bool {
	var out map[[2]doorescape.Tile]bool
	for k, until := range e.badSteps {
		if !now.Before(until) {
			delete(e.badSteps, k)
			continue
		}
		if out == nil {
			out = map[[2]doorescape.Tile]bool{}
		}
		out[k] = true
	}
	return out
}

// Active reports whether the ladder is working a visit now: the account is in a site and the ladder
// has neither given up nor is asleep between rounds. This is what Guardian.Busy answers with, so the
// scripts wait while there is something to wait for and never longer.
func (l *StrandLadder) Active(now time.Time) bool {
	return l.started && (!l.escapeSite || l.escaping) && !l.gaveUp && !now.Before(l.sleepUntil)
}

// TakeAnnounce reports whether the visit's arrival line is due, once per visit: at the first poll for an
// ordinary site, and for a site with an Escape when the ladder first takes the account (an account passing
// through its open ground is not announced).
func (l *StrandLadder) TakeAnnounce() bool {
	if !l.started || (l.escapeSite && !l.escaping) || l.announced {
		return false
	}
	l.announced = true
	return true
}

// GaveUp reports whether the visit has run out of rounds.
func (l *StrandLadder) GaveUp() bool { return l.gaveUp }

// Reset forgets the visit (the account left the site).
func (l *StrandLadder) Reset() { *l = StrandLadder{} }

// Clicks is how many failed clicks the visit has made in all, for the log.
func (l *StrandLadder) Clicks() int { return l.clicksLifetime }

type strandVerdict int

const (
	verdictNone strandVerdict = iota
	verdictWorked
	verdictFailed
)

// judge reads the position against the pending action, once the action has had time to land.
func (l *StrandLadder) judge(o StrandObs) strandVerdict {
	if !l.pending || o.Now.Sub(l.pendingAt) < strandClickEvery {
		return verdictNone
	}
	st := l.pendingStep
	// A door's Open has only half done its job until the walk through it has been sent.
	if !l.pendingCast && st != nil && st.Kind == StepDoor && l.doorStage == 1 {
		return verdictNone
	}
	l.pending = false
	from := l.pendingFrom
	moved := chebyshev(o.X, o.Z, from.X, from.Z)
	levelChanged := o.Level != from.Level
	if l.pendingCast {
		if levelChanged || moved > 10 {
			return verdictWorked
		}
		return verdictFailed
	}
	if st == nil {
		return verdictNone
	}
	if st.Kind == StepDoor {
		// Judged by where the account is now: out of the room, or beside the far tile.
		if o.Site == nil || chebyshev(o.X, o.Z, st.Beyond.X, st.Beyond.Z) <= 1 {
			return verdictWorked
		}
		return verdictFailed
	}
	if levelChanged || (st.MinMove > 0 && moved >= st.MinMove) {
		return verdictWorked
	}
	return verdictFailed
}

// Next advances the ladder by one poll and returns the single thing to do.
func (l *StrandLadder) Next(o StrandObs) StrandAction {
	if o.Site == nil {
		if l.started {
			l.Reset()
		}
		return StrandAction{Rung: RungNone}
	}
	if !l.started || l.site != o.Site.Name {
		l.Reset()
		l.started, l.site, l.since = true, o.Site.Name, o.Now
		l.escapeSite = o.Site.Escape != nil && o.Escape != nil
	}
	here := StrandTile{X: o.X, Z: o.Z, Level: o.Level}

	// Judge what an earlier poll sent, before choosing the next thing.
	wasCast := l.pendingCast
	switch l.judge(o) {
	case verdictFailed:
		if !wasCast {
			l.clicks++
			l.clicksLifetime++
			// A failed click moves to the next stand tile: the loc may answer from one side only.
			l.standIdx++
			l.walks, l.walkDist = 0, 0
			l.doorStage = 0
		}
	case verdictWorked:
		// The account moved: the poll's tile is judged afresh against whatever step it is on now.
		l.clicks, l.standIdx, l.walks, l.walkDist, l.doorStage = 0, 0, 0, 0, 0
	}

	if l.gaveUp {
		return StrandAction{Rung: RungNone}
	}
	// 1. Eat. Ahead of the sleep between rounds: an account hurt in the dragon's lair is not less hurt
	// for the ladder having failed a round.
	lethal := o.Site.Lethal
	if l.escapeSite {
		// Open ground inside the walls is not where the danger was: eat there only when attacked.
		lethal = lethal && o.Escape.Cat.Interior(o.X, o.Z, o.Level)
	}
	if (lethal || o.Attacked) && o.HasFood && !o.DialogueOpen && o.HPKnown && o.MaxHP > 0 &&
		float64(o.HP) < holdGroundEatBelow*float64(o.MaxHP) && o.Now.Sub(l.lastEat) >= strandEatEvery {
		l.lastEat = o.Now
		return StrandAction{Rung: RungEat, Note: fmt.Sprintf("%d/%d hitpoints", o.HP, o.MaxHP)}
	}
	if o.Now.Before(l.sleepUntil) {
		return StrandAction{Rung: RungWait, Note: fmt.Sprintf("asleep until %s", l.sleepUntil.Format("15:04:05"))}
	}
	if !o.Site.Lethal && o.Now.Sub(l.since) < strandGrace {
		return StrandAction{Rung: RungWait, Note: "giving the script's own travel the first look"}
	}

	// 2. Teleport. A site that is ExitFirst has a free exit that works: the spell waits until the exit has failed.
	exitFirst := o.Site.ExitFirst && l.clicksLifetime < strandExitFirstClicks
	if !o.Site.NoTeleport && !exitFirst && o.Spell != "" && l.casts < strandTeleportCasts && !l.pending &&
		o.Now.Sub(l.lastTele) >= strandTeleportEvery {
		l.casts++
		l.lastTele = o.Now
		l.pending, l.pendingCast, l.pendingFrom, l.pendingAt, l.pendingStep = true, true, here, o.Now, nil
		return StrandAction{Rung: RungTeleport, Spell: o.Spell, Note: fmt.Sprintf("cast %d of %d", l.casts, strandTeleportCasts)}
	}

	if l.escapeSite {
		return l.escape(o)
	}

	// A door that has had time to open: walk through it.
	if l.pending && !l.pendingCast && l.pendingStep != nil && l.pendingStep.Kind == StepDoor && l.doorStage == 1 &&
		o.Now.Sub(l.pendingAt) >= strandDoorSettle {
		l.doorStage = 2
		l.pendingAt = o.Now
		return StrandAction{Rung: RungThrough, Step: l.pendingStep, Stand: l.pendingStep.Beyond, Note: l.pendingStep.Name}
	}
	if l.pending {
		return hold("waiting for the last action to land")
	}

	// 3. The catalogued exit.
	step := o.Site.StepFor(o.X, o.Z, o.Level)
	if step == nil {
		// A learned site has no steps, and a catalogued one has no step for this tile: all that is
		// left is a cast already in the air or a cooldown, and past that nothing, so stop rather than
		// keep the scripts waiting on a ladder with no rung.
		if o.Spell == "" || o.Site.NoTeleport || l.casts >= strandTeleportCasts {
			l.gaveUp = true
			return StrandAction{Rung: RungGiveUp, Note: "no catalogued exit from this tile and nothing castable"}
		}
		return hold("no catalogued exit from this tile; waiting to cast again")
	}
	if l.stepName != step.Name {
		l.stepName, l.standIdx, l.walks, l.walkDist, l.doorStage = step.Name, 0, 0, 0, 0
	}
	if l.clicks >= strandClicksPerRound {
		return l.endRound(o)
	}
	stand := step.Stands[l.standIdx%len(step.Stands)]
	if here != stand {
		// A walk that is getting closer is not stuck: the lair's lever is fifteen tiles from the arrival
		// tile, three walk clicks 2.4 seconds apart are not enough for an account without run energy,
		// and counting that as a failed stand would end rounds on an account that is simply still
		// running.
		if d := chebyshev(here.X, here.Z, stand.X, stand.Z); l.walkDist > 0 && d < l.walkDist {
			l.walks = 0
		} else {
			l.walkDist = d
		}
		if l.walks >= strandWalksPerStand {
			// This stand will not be reached from here: try the next, and count it a failure so a room
			// with no reachable stand ends the round instead of walking forever.
			l.standIdx++
			l.walks, l.walkDist = 0, 0
			l.clicks++
			l.clicksLifetime++
			return hold("could not reach the stand tile")
		}
		if o.Now.Sub(l.lastWalk) < strandWalkEvery {
			return hold("between walk clicks")
		}
		l.lastWalk = o.Now
		l.walks++
		l.walkDist = chebyshev(here.X, here.Z, stand.X, stand.Z)
		return StrandAction{Rung: RungWalk, Step: step, Stand: stand, Note: step.Name}
	}
	if o.Now.Sub(l.lastClick) < strandClickEvery {
		return hold("between clicks")
	}
	l.lastClick = o.Now
	l.pending, l.pendingCast, l.pendingFrom, l.pendingAt, l.pendingStep = true, false, here, o.Now, step
	if step.Kind == StepDoor {
		l.doorStage = 1
	}
	return StrandAction{Rung: RungClick, Step: step, Stand: stand, Note: step.Name}
}

// escape is the rung of a site with an Escape: plan the way out from the account's own tile and the doors as
// they are now, and do the first thing of the plan.
//
// The plan is made afresh on every poll that has something to decide (internal/doorescape: the engine's own step
// and reach rules over the castle's collision, every door's two leaves, staircases), so it follows what the doors
// actually did: an open leaf that lands in the way is a wall to the next step, a door another account opened is
// a way through, a door that was shut behind the account is opened again. Nothing is remembered about the route
// itself, only what failed: a walk that never moved the account blames its first step, a click that changed no
// door and no level blames the stand it was sent from, and the next plan goes round those for escapeSuspectFor.
// When the plans run out of ways that have not failed, the ladder forgets the failures and tries them again; when
// there is no way out from the tile at all, with nothing held against any route, it says so and gives up.
//
// An account on open ground inside the walls (Catalog.Interior is false) can walk out by itself, so for
// escapeStayLimit it is left alone and not held; past that it is escorted out the same way, by a plan that uses no
// door when it can.
//
// A plan never shuts a door unless the account has been walled in for escapeTrapWait: an open leaf that blocks the
// way out (the two iron cell doors, with the room's account inside, are the cases the map has) goes back by itself
// within five minutes, so the ladder waits, polling every escapeTrapPoll with the scripts let go, and only a leaf
// that something keeps open is shut.
func (l *StrandLadder) escape(o StrandObs) StrandAction {
	e := &l.esc
	cat, st := o.Escape.Cat, o.Escape.State
	now := o.Now
	tile := doorescape.Tile{X: o.X, Z: o.Z, Level: o.Level}
	if tile != e.lastPos || e.lastMoveAt.IsZero() {
		e.lastPos, e.lastMoveAt = tile, now
	}

	if !cat.Interior(o.X, o.Z, o.Level) && !e.opLive && !l.escaping {
		if cat.Plan(tile, doorescape.Options{State: st, NoOps: true}).OK && now.Sub(l.since) < escapeStayLimit {
			return StrandAction{Rung: RungNone}
		}
	}
	l.escaping = true
	if l.roundAt.IsZero() {
		l.roundAt = now
	}
	if now.Sub(l.roundAt) >= escapeRoundLimit {
		e.walkLive, e.opLive = false, false
		e.forget()
		return l.endRound(o)
	}

	// Judge the click in flight: it worked when a door's leaves or the level changed, failed when it has had time.
	if e.opLive {
		switch {
		case e.opLanded(o):
			e.opLive = false
			l.clicks = 0
		case now.Sub(e.opAt) >= strandClickEvery:
			e.opLive = false
			if e.badOps == nil {
				e.badOps = map[doorescape.OpKey]time.Time{}
			}
			s := e.opStep
			e.badOps[doorescape.OpKey{Kind: s.Kind, Index: opIndex(s), Close: s.Close, Stand: s.Stand}] = now.Add(escapeSuspectFor)
			l.clicks++
			l.clicksLifetime++
		default:
			return hold("waiting for the last click to land")
		}
	}
	// Judge the walk in flight: arrived, still walking, or refused.
	if e.walkLive {
		moving := now.Sub(e.lastMoveAt) < escapeMovingFor
		switch {
		case tile == e.walkTo:
			// Arriving is not progress: only a click that changed a door or the level clears the failures, or a
			// walk between two doors that will not open would never run a round out.
			e.walkLive = false
		case moving:
			return hold("walking")
		case now.Sub(e.walkAt) >= escapeWalkStall:
			e.walkLive = false
			if from, to, ok := e.stalledStep(tile); ok {
				if e.badSteps == nil {
					e.badSteps = map[[2]doorescape.Tile]time.Time{}
				}
				e.badSteps[[2]doorescape.Tile{from, to}] = now.Add(escapeSuspectFor)
			}
			l.clicks++
			l.clicksLifetime++
		default:
			return hold("waiting for the walk to start")
		}
	}
	if l.clicks >= strandClicksPerRound {
		e.walkLive, e.opLive = false, false
		e.forget()
		return l.endRound(o)
	}

	opts := doorescape.Options{State: st, AvoidOp: e.liveBadOps(now), AvoidStep: e.liveBadSteps(now)}
	// An escort of an account on open ground uses no door when it can walk.
	if !cat.Interior(o.X, o.Z, o.Level) {
		walk := opts
		walk.NoOps = true
		if p := cat.Plan(tile, walk); p.OK {
			return l.escapeStep(o, p)
		}
	}
	opts.NoClose = true
	plan := cat.Plan(tile, opts)
	if plan.OK {
		e.trapAt = time.Time{}
	} else {
		opts.NoClose = false
		plan = cat.Plan(tile, opts)
		if plan.OK {
			if e.trapAt.IsZero() {
				e.trapAt = now
			}
			if waited := now.Sub(e.trapAt); waited < escapeTrapWait {
				l.sleepUntil = now.Add(escapeTrapPoll)
				return StrandAction{Rung: RungWait, Note: fmt.Sprintf("an open leaf walls the account in and goes back by itself; not shutting a door for another %s", (escapeTrapWait - waited).Round(time.Second))}
			}
		}
	}
	if !plan.OK {
		if len(opts.AvoidOp)+len(opts.AvoidStep) > 0 {
			// Everything left has failed before: forget the failures and go round again.
			e.forget()
			return hold("every route tried has failed; trying them again")
		}
		// Nothing frees this tile with the doors as the client sees them. Say so (the executor logs it once),
		// sleep the longest backoff and look again: a door another account was holding is let go, a client that was
		// not told yet is told. The plan costs a few thousand states, so looking again is cheap.
		wait := strandBackoff[len(strandBackoff)-1]
		l.sleepUntil = now.Add(wait)
		return StrandAction{Rung: RungGiveUp, Note: fmt.Sprintf("no way out of the walls from this tile (%s); looking again every %s", plan.Reason, wait)}
	}
	return l.escapeStep(o, plan)
}

// escapeStep does the first step of a plan: a walk click, or a door or staircase click from the tile it needs.
func (l *StrandLadder) escapeStep(o StrandObs, plan doorescape.Plan) StrandAction {
	e := &l.esc
	now := o.Now
	tile := doorescape.Tile{X: o.X, Z: o.Z, Level: o.Level}
	if len(plan.Steps) == 0 {
		return StrandAction{Rung: RungNone}
	}
	first := plan.Steps[0]
	switch first.Kind {
	case doorescape.StepWalk:
		path := first.Tiles
		if len(path) > escapeWalkTiles {
			path = path[:escapeWalkTiles]
		}
		e.walkLive, e.walkAt, e.walkFrom, e.walkTo, e.walkPath = true, now, tile, path[len(path)-1], path
		out := make([]StrandTile, len(path))
		for i, p := range path {
			out[i] = StrandTile{X: p.X, Z: p.Z, Level: p.Level}
		}
		to := out[len(out)-1]
		return StrandAction{Rung: RungWalk, Path: out, Stand: to, Note: fmt.Sprintf("%d tiles to %d,%d,%d, %d door or stair step(s) after it", len(path), to.X, to.Z, to.Level, plan.Ops())}
	default:
		if tile != first.Stand {
			return hold("the plan's next click is from " + first.Stand.String() + ", not from here")
		}
		if now.Sub(l.lastClick) < strandClickEvery {
			return hold("between clicks")
		}
		l.lastClick = now
		e.opLive, e.opAt, e.opStep, e.opBefore, e.opLevel = true, now, first, o.Escape.State, o.Level
		e.ops++
		cat := o.Escape.Cat
		name, label := "", ""
		if first.Kind == doorescape.StepDoor {
			d := cat.Doors[first.Door]
			name = d.Name
			label = "Open"
			if first.Close {
				label = "Close"
			}
		} else {
			s := cat.Stairs[first.Stair]
			name, label = s.Name, s.Label
		}
		step := &ExitStep{
			Name: name, Kind: StepLoc, Where: StrandRect{MinX: o.X, MaxX: o.X, MinZ: o.Z, MaxZ: o.Z, Level: o.Level},
			Stands: []StrandTile{{X: first.Stand.X, Z: first.Stand.Z, Level: first.Stand.Level}},
			LocID:  first.Loc.ID, LocX: first.Loc.X, LocZ: first.Loc.Z, Op: first.Op, OpLabel: label,
		}
		return StrandAction{Rung: RungClick, Step: step, Stand: step.Stands[0], Note: name}
	}
}

// opIndex is the catalogue index of the door or staircase a step operates.
func opIndex(s doorescape.Step) int {
	if s.Kind == doorescape.StepStairs {
		return s.Stair
	}
	return s.Door
}

// opLanded reports whether the click in flight did what it was for: a door's leaves are not as they were, or the
// account is on another level.
func (e *escapeMem) opLanded(o StrandObs) bool {
	switch e.opStep.Kind {
	case doorescape.StepStairs:
		return o.Level != e.opLevel
	case doorescape.StepDoor:
		i := uint(2 * e.opStep.Door)
		return (o.Escape.State>>i)&3 != (e.opBefore>>i)&3
	}
	return false
}

// stalledStep is the step of the walk in flight that the account has not taken: from the tile it stands on to the
// next tile of the path. ok is false when the account is not on the path.
func (e *escapeMem) stalledStep(at doorescape.Tile) (from, to doorescape.Tile, ok bool) {
	if len(e.walkPath) == 0 {
		return doorescape.Tile{}, doorescape.Tile{}, false
	}
	if at == e.walkFrom {
		return at, e.walkPath[0], true
	}
	for i, p := range e.walkPath {
		if p == at && i+1 < len(e.walkPath) {
			return at, e.walkPath[i+1], true
		}
	}
	return doorescape.Tile{}, doorescape.Tile{}, false
}

// endRound closes a round of failed clicks: sleep, or give up for the visit. A site with an escape plan never gives
// up for good: its plan found a way out in the model, so after the last round it says so (the executor logs it once)
// and goes on trying every strandBackoff[last], holding the scripts for no more than escapeRoundLimit each time.
func (l *StrandLadder) endRound(o StrandObs) StrandAction {
	l.clicks, l.standIdx, l.walks, l.walkDist, l.pending, l.doorStage = 0, 0, 0, 0, false, 0
	l.roundAt = time.Time{}
	if l.round >= len(strandBackoff) {
		if l.escapeSite {
			wait := strandBackoff[len(strandBackoff)-1]
			l.sleepUntil = o.Now.Add(wait)
			return StrandAction{Rung: RungGiveUp, Note: fmt.Sprintf("%d rounds and %d clicks without leaving; trying again every %s", len(strandBackoff)+1, l.clicksLifetime, wait)}
		}
		l.gaveUp = true
		return StrandAction{Rung: RungGiveUp, Note: fmt.Sprintf("%d rounds and %d clicks without leaving", len(strandBackoff)+1, l.clicksLifetime)}
	}
	l.sleepUntil = o.Now.Add(strandBackoff[l.round])
	l.round++
	return StrandAction{Rung: RungWait, Note: fmt.Sprintf("round %d failed; sleeping %s", l.round, strandBackoff[l.round-1])}
}

// NoteGaveUpLogged reports whether the give-up line has already been written, and marks it written:
// STRANDED is said once per visit.
func (l *StrandLadder) NoteGaveUpLogged() bool {
	if l.gaveUpLogged {
		return true
	}
	l.gaveUpLogged = true
	return false
}

// ---- the teleport spell ----------------------------------------------------------------------

// A teleport the ladder may cast, from content/scripts/skill_magic/configs/magic_spells.dbrow. Only the
// three free-world spells: Camelot and Ardougne are members spells, and Ardougne also wants Plague City.
type teleportSpell struct {
	name         string
	level        int
	law          int
	elementRune  string
	elementCount int
	air          int
	landX, landZ int
}

var teleportSpells = []teleportSpell{
	{name: "varrock", level: 25, law: 1, elementRune: "Fire rune", elementCount: 1, air: 3, landX: 3213, landZ: 3424},
	{name: "lumbridge", level: 31, law: 1, elementRune: "Earth rune", elementCount: 1, air: 3, landX: 3221, landZ: 3218},
	{name: "falador", level: 37, law: 1, elementRune: "Water rune", elementCount: 1, air: 3, landX: 2965, landZ: 3378},
}

// content's magic_staff_table rows: a wielded staff pays for its element.
var (
	fireStaves  = []string{"Staff of fire", "Fire battlestaff", "Mystic fire staff", "Lava battlestaff", "Mystic lava staff"}
	waterStaves = []string{"Staff of water", "Water battlestaff", "Mystic water staff", "Mud battlestaff", "Mystic mud staff"}
	airStaves   = []string{"Staff of air", "Air battlestaff", "Mystic air staff"}
	earthStaves = []string{"Staff of earth", "Earth battlestaff", "Mystic earth staff", "Lava battlestaff", "Mystic lava staff", "Mud battlestaff", "Mystic mud staff"}
)

func wornAny(worn []string, staves []string) bool {
	for _, w := range worn {
		for _, s := range staves {
			if strings.EqualFold(strings.TrimSpace(w), s) {
				return true
			}
		}
	}
	return false
}

func packCount(pack map[string]int, name string) int {
	n := 0
	for k, v := range pack {
		if strings.EqualFold(strings.TrimSpace(k), name) {
			n += v
		}
	}
	return n
}

// PickTeleport chooses the teleport an account can pay for: its Magic level, the pack counted by item
// name, the names it wears (a wielded staff pays for its element), and a tile to prefer landing near
// (the account's own home; nil for none). It returns the spell name actions.Teleport takes, or why
// none can be cast. Pure.
func PickTeleport(magic int, magicKnown bool, pack map[string]int, worn []string, prefer *StrandTile) (spell string, why string) {
	if !magicKnown {
		return "", "its Magic level has not been reported yet"
	}
	best, bestDist := "", 1<<30
	var refusals []string
	for _, s := range teleportSpells {
		if magic < s.level {
			refusals = append(refusals, fmt.Sprintf("%s needs Magic %d (has %d)", s.name, s.level, magic))
			continue
		}
		var short []string
		if packCount(pack, "Law rune") < s.law {
			short = append(short, "a Law rune")
		}
		covered := false
		switch s.elementRune {
		case "Fire rune":
			covered = wornAny(worn, fireStaves)
		case "Earth rune":
			covered = wornAny(worn, earthStaves)
		case "Water rune":
			covered = wornAny(worn, waterStaves)
		}
		if packCount(pack, s.elementRune) < s.elementCount && !covered {
			short = append(short, "a "+s.elementRune)
		}
		if packCount(pack, "Air rune") < s.air && !wornAny(worn, airStaves) {
			short = append(short, fmt.Sprintf("%d Air runes", s.air))
		}
		if len(short) > 0 {
			refusals = append(refusals, fmt.Sprintf("%s lacks %s", s.name, strings.Join(short, ", ")))
			continue
		}
		d := 0
		if prefer != nil {
			d = chebyshev(s.landX, s.landZ, prefer.X, prefer.Z)
		}
		if best == "" || d < bestDist {
			best, bestDist = s.name, d
		}
	}
	if best == "" {
		return "", strings.Join(refusals, "; ")
	}
	return best, ""
}
