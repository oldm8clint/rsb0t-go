package randomevents

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/doorescape"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
)

// The Guardian's half of the escape ladder (strandladder.go): read the account, ask the ladder for the
// one thing to do this poll, do it. Everything with a decision in it is in strandladder.go and
// strandsites.go and is unit-tested there; this file only reads the session and sends packets.
//
// It lives in the Guardian, not in any script, because the accounts are in these places whatever they
// are running (ScarabSwarm, RuneRunner, MuleReceiver all appear in the displaced lines) and a script that
// has been dropped on a roof cannot be expected to carry the way off it: ScarabSwarm's Stage 3 walked
// "to 3158,3950,0: unreachable" every thirteen seconds for twelve hours without once trying the stairs
// two tiles away. Guardian.Busy is true while the ladder has something to try, so every script's
// WaitForGuardian holds its loop for exactly that long and no longer.

// strandClock is the ladder's clock. A variable only so a test can drive the Guardian's real glue through minutes of
// escape in milliseconds; nothing else assigns it.
var strandClock = time.Now

// strandSpellMagicStat is UPDATE_STAT's Magic index (gamestate.SkillNames[6]).
const strandSpellMagicStat = 6

// strandStay is when the account entered the site it is in, for StrandSite.MinStay. Guarded by mu.
type strandStay struct {
	site string
	at   time.Time
}

// strandEatable is a pack item the ladder may eat: option 1 is Eat, and nothing poisoned.
type strandEatable struct {
	id   int32
	slot int
	name string
}

// recoverStrand runs one poll of the ladder and reports whether it took the poll (acted, or has an
// action in the air that nothing else should step on). It needs the action lock like every handler:
// when a script's own sequence holds it, the poll is skipped.
func (g *Guardian) recoverStrand() bool {
	if g.session == nil {
		return false
	}
	x, z, level, ok := g.session.State().Tile()
	if !ok {
		return false
	}
	site, in := g.siteAt(x, z, level)
	if !in {
		g.mu.Lock()
		was := g.strand.started
		clicks := g.strand.Clicks()
		since := g.strand.since
		name := g.strand.site
		held := g.strandHold
		g.strand.Reset()
		g.strandStay = strandStay{}
		g.strandHold = strandStay{}
		g.handedBack = ""
		g.mu.Unlock()
		// The void walk's and the PvP escape's memories belong to the visit: the next one starts them afresh (a walk that was
		// boxed in must not hand the next visit back on its first poll). Only this goroutine, the Guardian's own poll, touches them.
		g.void, g.pvp = voidWalk{}, pvpState{}
		if was {
			g.log(fmt.Sprintf("strand: left %s after %s (%d failed clicks), now at %d,%d,%d",
				name, strandClock().Sub(since).Round(time.Second), clicks, x, z, level))
		} else if held.site != "" {
			g.log(fmt.Sprintf("strand: left %s after %s, now at %d,%d,%d",
				held.site, time.Since(held.at).Round(time.Second), x, z, level))
		}
		return false
	}
	if site.MinStay > 0 {
		g.mu.Lock()
		if g.strandStay.site != site.Name {
			g.strandStay.site, g.strandStay.at = site.Name, time.Now()
		}
		stayed := time.Since(g.strandStay.at)
		g.mu.Unlock()
		if stayed < site.MinStay {
			return false
		}
	}
	// The PvP escape and the void walk run as long as the account is in the site, so they hold every
	// script from the moment it arrives, before the action lock is even tried: a script left running
	// walked the accounts back south to Wilderness level 9 and its eating and fleeing broke up every
	// fight (live 3 Oct 11:40, 343 attack clicks, 10 deaths).
	g.mu.Lock()
	if site.HandBack && g.handedBack == site.Name {
		// The walk gave the account to the ladder (handBackToLadder): no PvP, no void hops, no hold.
		site.PvP, site.VoidWalkTo = false, nil
	}
	voidWalk := false
	if !site.PvP && site.VoidWalkTo != nil {
		clicks := 0
		if g.strand.site == site.Name {
			clicks = g.strand.Clicks()
		}
		voidWalk = len(site.Steps) == 0 || clicks >= VoidWalkAfterClicks
	}
	if (site.PvP || voidWalk) && g.strandHold.site != site.Name {
		g.strandHold = strandStay{site: site.Name, at: time.Now()}
	}
	g.mu.Unlock()

	if !g.actionMu.TryLock() {
		return false
	}
	g.inFlight.Store(true)
	defer func() { g.inFlight.Store(false); g.actionMu.Unlock() }()

	if (site.PvP || voidWalk) && site.HandBack && g.void.site == site.Name && g.void.boxed {
		g.handBackToLadder(&site, x, z, level)
		site.PvP, site.VoidWalkTo, voidWalk = false, nil, false
	}
	if site.PvP {
		return g.strandPvP(&site, x, z, level)
	}
	if voidWalk {
		return g.strandVoidWalk(&site, x, z, level)
	}

	obs := g.strandObs(&site, x, z, level)
	g.mu.Lock()
	act := g.strand.Next(obs)
	pending := g.strand.pending
	announce := g.strand.TakeAnnounce()
	gaveUpLogged := false
	if act.Rung == RungGiveUp {
		gaveUpLogged = g.strand.NoteGaveUpLogged()
	}
	g.mu.Unlock()

	if announce {
		if site.Escape != nil {
			g.log(fmt.Sprintf("strand: %s at %d,%d,%d — %s. Planning the way out of the walls from here, door by door and floor by floor", site.Name, x, z, level, site.Why))
		} else {
			g.log(fmt.Sprintf("strand: %s at %d,%d,%d — %s. Running the escape ladder (eat, teleport, the catalogued exit)", site.Name, x, z, level, site.Why))
		}
	}
	switch act.Rung {
	case RungNone:
		return false
	case RungWait:
		// The ladder keeps the account on the polls between its own actions (act.Hold), so no other
		// handler walks it off the tile the ladder is waiting on; while asleep, in grace, or with nothing
		// to try it lets the rest of kick() run as it always did.
		return act.Hold || pending
	case RungGiveUp:
		if !gaveUpLogged {
			g.mu.Lock()
			handed := g.handedBack == site.Name
			g.mu.Unlock()
			if handed {
				g.log(fmt.Sprintf("strand: %s at %d,%d,%d — %s; the ladder is out of rungs, so the account is its script's again for the rest of this visit (its own walk crosses the doors, webs and ladders the void hops cannot)", site.Name, x, z, level, act.Note))
			} else {
				g.log(fmt.Sprintf("strand: STRANDED in %s at %d,%d,%d — %s; the account needs an operator (a relog does not move it)", site.Name, x, z, level, act.Note))
			}
		}
		return false
	case RungEat:
		food, ok := g.strandFood()
		if !ok {
			return true
		}
		if err := actions.Eat(g.session, food.id, food.slot, actions.PlayerPack); err != nil {
			g.log("strand: eating failed: " + err.Error())
			return true
		}
		g.log(fmt.Sprintf("strand: %s — eating %s (%s)", site.Name, food.name, act.Note))
	case RungTeleport:
		if err := actions.Teleport(g.session, act.Spell); err != nil {
			g.log(fmt.Sprintf("strand: %s — casting %s teleport failed: %v", site.Name, act.Spell, err))
			return true
		}
		g.log(fmt.Sprintf("strand: %s — casting %s teleport (%s)", site.Name, act.Spell, act.Note))
	case RungWalk:
		if len(act.Path) > 0 {
			// The escape rung's walk: every tile of the path is one legal step from the one before, so the
			// server's own stepping (diagonal, then along x, then along z) takes exactly the steps planned.
			path := make([]pathfind.NavPoint, 0, len(act.Path)+1)
			path = append(path, pathfind.NavPoint{X: x, Z: z, Level: level})
			for _, p := range act.Path {
				path = append(path, pathfind.NavPoint{X: p.X, Z: p.Z, Level: p.Level})
			}
			if err := actions.Walk(g.session, path, true); err != nil {
				g.log(fmt.Sprintf("strand: %s — walking %d tiles to %d,%d,%d: %v", site.Name, len(act.Path), act.Stand.X, act.Stand.Z, act.Stand.Level, err))
			} else {
				g.log(fmt.Sprintf("strand: %s — walking from %d,%d,%d: %s", site.Name, x, z, level, act.Note))
			}
			break
		}
		if err := g.strandWalk(x, z, level, act.Stand); err != nil {
			g.log(fmt.Sprintf("strand: %s — walking to the stand tile %d,%d,%d: %v", site.Name, act.Stand.X, act.Stand.Z, act.Stand.Level, err))
		}
	case RungClick:
		g.strandClick(&site, act, x, z, level)
	case RungThrough:
		if err := actions.Walk(g.session, []pathfind.NavPoint{{X: x, Z: z, Level: level}, {X: act.Stand.X, Z: act.Stand.Z, Level: act.Stand.Level}}, true); err != nil {
			g.log(fmt.Sprintf("strand: %s — walking through the door: %v", site.Name, err))
		} else {
			g.log(fmt.Sprintf("strand: %s — through the door to %d,%d,%d", site.Name, act.Stand.X, act.Stand.Z, act.Stand.Level))
		}
	}
	return true
}

// handBackToLadder ends the void or PvP walk of a HandBack site that made no net progress in voidFlatHops hops and gives
// the account to the escape ladder for the rest of the visit: the hold is let go (the scripts run again between the ladder's
// own actions), and the ladder casts a teleport when the account can pay for one and otherwise gives up and leaves the
// account to its script. Called with actionMu held, before the walk's next hop.
func (g *Guardian) handBackToLadder(site *StrandSite, x, z, level int) {
	v := g.void
	target := "its target"
	switch {
	case site.PvP:
		r := g.rallyFor(z)
		target = fmt.Sprintf("the PvP rally at %d,%d", r.X, r.Z)
	case site.VoidWalkTo != nil:
		target = fmt.Sprintf("%d,%d", site.VoidWalkTo.X, site.VoidWalkTo.Z)
	}
	g.mu.Lock()
	g.handedBack = site.Name
	g.strandHold = strandStay{}
	g.mu.Unlock()
	g.log(fmt.Sprintf("strand: %s at %d,%d,%d — the walk toward %s made no net progress in %d hops (the nearest it came was %d tiles; %d hops in all), "+
		"so it stops: the ladder tries a teleport if the account can pay for one, and then the account is its script's again",
		site.Name, x, z, level, target, v.flat, v.best, v.hops))
}

// strandClick operates the step's loc. A door that is already standing open is not clicked again (the
// next poll walks through it).
func (g *Guardian) strandClick(site *StrandSite, act StrandAction, x, z, level int) {
	st := act.Step
	if st == nil {
		return
	}
	if st.Kind == StepDoor && g.strandDoorOpen(st) {
		g.log(fmt.Sprintf("strand: %s — the door at %d,%d is already open", site.Name, st.LocX, st.LocZ))
		return
	}
	if err := actions.OpLoc(g.session, st.Op, st.LocX, st.LocZ, st.LocID); err != nil {
		g.log(fmt.Sprintf("strand: %s — %s (%s at %d,%d): %v", site.Name, st.Name, st.OpLabel, st.LocX, st.LocZ, err))
		return
	}
	g.log(fmt.Sprintf("strand: %s — %s: %s on loc %d at %d,%d from %d,%d,%d", site.Name, st.Name, st.OpLabel, st.LocID, st.LocX, st.LocZ, x, z, level))
}

// strandDoorOpen reports whether a live loc change has replaced the catalogued closed door with another
// loc (its open state), or removed it.
func (g *Guardian) strandDoorOpen(st *ExitStep) bool {
	for _, c := range g.session.State().LocChanges() {
		if c.X == st.LocX && c.Z == st.LocZ && c.Level == st.Stands[0].Level && (c.Deleted || c.Type != st.LocID) {
			return true
		}
	}
	return false
}

// strandWalk sends one walk click toward a stand tile: the route on the live overlay when the pack finds
// one, otherwise the bare two-point click, which the server then routes itself.
func (g *Guardian) strandWalk(x, z, level int, stand StrandTile) error {
	from := pathfind.NavPoint{X: x, Z: z, Level: level}
	to := pathfind.NavPoint{X: stand.X, Z: stand.Z, Level: stand.Level}
	var path []pathfind.NavPoint
	if pack := fleePack(g.session, g.pack); pack != nil {
		if res := pack.FindPath(from, to); res.OK && len(res.Waypoints) > 1 {
			path = actions.SendableWaypoints(res.Waypoints)
		}
	}
	if len(path) == 0 {
		path = []pathfind.NavPoint{from, to}
	}
	return actions.Walk(g.session, path, true)
}

// strandObs reads everything the ladder decides from.
func (g *Guardian) strandObs(site *StrandSite, x, z, level int) StrandObs {
	st := g.session.State()
	o := StrandObs{Now: strandClock(), X: x, Z: z, Level: level, Site: site}
	o.HP, o.HPKnown = st.HP()
	if max, ok := st.MaxHP(); ok {
		o.MaxHP = max
	} else {
		o.HPKnown = false
	}
	o.Attacked = underAttack(g.session)
	_, _, chat := st.Modals()
	o.DialogueOpen = chat != gamestate.NoModal
	_, o.HasFood = g.strandFood()
	if !site.NoTeleport {
		o.Spell = g.strandSpell()
	}
	if site.Escape != nil {
		if cat, err := site.Escape(); err == nil {
			o.Escape = &EscapeObs{Cat: cat, State: cat.StateFromChanges(escapeChanges(st, level))}
		}
	}
	return o
}

// escapeChanges are the client's live loc changes on the account's level, in the form doorescape reads (a client
// holds only its own level's changes, so nothing else can be about a door).
func escapeChanges(st *gamestate.State, level int) []doorescape.LocChange {
	var out []doorescape.LocChange
	for _, c := range st.LocChanges() {
		if c.Level != level {
			continue
		}
		out = append(out, doorescape.LocChange{X: c.X, Z: c.Z, Level: c.Level, Type: c.Type, Shape: c.Shape, Rotate: c.Rotate, Deleted: c.Deleted})
	}
	return out
}

// strandFood is the pack's best food: the item with an Eat option that heals most (bestFood), never a poisoned one or the Strange
// fruit a plant gives, which has an Eat option and restores run energy, not hitpoints (edibleHeal). It was the first item with an Eat
// option, so a pack of a Strange fruit and nothing else read as fed.
func (g *Guardian) strandFood() (strandEatable, bool) {
	res, ok := currentResolver().(holdGroundResolver)
	if !ok {
		return strandEatable{}, false
	}
	inv := g.session.State().Inventory(actions.PlayerPack)
	if inv == nil {
		return strandEatable{}, false
	}
	var food []holdGroundFood
	for _, it := range inv.NonEmpty() {
		if o, ok := res.Obj(int(it.ID)); ok && strings.EqualFold(o.IOps[0], "Eat") {
			food = append(food, holdGroundFood{id: it.ID, slot: it.Slot, name: o.Name, op: o.IOps[0]})
		}
	}
	best, ok := bestFood(food)
	if !ok {
		return strandEatable{}, false
	}
	return strandEatable{id: best.id, slot: best.slot, name: best.name}, true
}

// teleportInputs is what PickTeleport decides from, read off the session: its Magic level, the pack counted by item name and
// the names it wears. ok is false when the Magic level has not been reported, is under the lowest teleport's, or no name
// resolver is registered to read the pack with.
func teleportInputs(st *gamestate.State) (magic int, known bool, pack map[string]int, worn []string, ok bool) {
	sk, haveSkill := st.Skill(strandSpellMagicStat)
	known = haveSkill && sk.Generation > 0
	if !known || sk.Level < teleportSpells[0].level {
		return 0, known, nil, nil, false
	}
	res := currentResolver()
	if res == nil {
		return 0, known, nil, nil, false
	}
	pack = map[string]int{}
	if inv := st.Inventory(actions.PlayerPack); inv != nil {
		for _, it := range inv.NonEmpty() {
			if o, ok := res.Obj(int(it.ID)); ok {
				n := int(it.Count)
				if n < 1 {
					n = 1
				}
				pack[o.Name] += n
			}
		}
	}
	if inv := st.Inventory(actions.PlayerWorn); inv != nil {
		for _, it := range inv.NonEmpty() {
			if o, ok := res.Obj(int(it.ID)); ok {
				worn = append(worn, o.Name)
			}
		}
	}
	return sk.Level, known, pack, worn, true
}

// strandSpell is the teleport the account can pay for right now, "" when none.
func (g *Guardian) strandSpell() string {
	magic, known, pack, worn, ok := teleportInputs(g.session.State())
	if !ok {
		return ""
	}
	var prefer *StrandTile
	if g.SafeRef != nil {
		prefer = &StrandTile{X: g.SafeRef.X, Z: g.SafeRef.Z, Level: g.SafeRef.Level}
	}
	spell, _ := PickTeleport(magic, known, pack, worn, prefer)
	return spell
}

// strandHolds is whether session's Guardian has its strand ladder working a visit right now; false for a
// session with no registered Guardian. EmergencyFlee steps aside for it.
func strandHolds(session actions.Session) bool {
	registryMu.Lock()
	g := registry[session]
	registryMu.Unlock()
	return g != nil && g.strandActive()
}

// strandActive is Guardian.Busy's question: is the ladder working a visit, or the PvP escape or void
// walk holding the account in its site.
func (g *Guardian) strandActive() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.strand.Active(strandClock()) || g.strandHold.site != ""
}

// noWayHomeFor is how long one failed walk-home check keeps a NoWayHomeOnly site in force for an
// account the site is not yet holding.
const noWayHomeFor = 2 * time.Minute

// siteAt is StrandSiteAt for this account: it adds the NoWayHomeOnly sites, which count once the
// drift check has found no walk home, and for as long as the site holds the account after that.
func (g *Guardian) siteAt(x, z, level int) (StrandSite, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.siteAtLocked(x, z, level, time.Now())
}

// siteAtLocked is siteAt with g.mu held.
func (g *Guardian) siteAtLocked(x, z, level int, now time.Time) (StrandSite, bool) {
	for i := range strandSites {
		s := &strandSites[i]
		if !s.Contains(x, z, level) {
			continue
		}
		if s.NoWayHomeOnly && g.strandHold.site != s.Name && now.Sub(g.noWayHome) > noWayHomeFor {
			continue
		}
		return *s, true
	}
	return learnedSiteAt(x, z, level, now)
}

// NoteNoWayHome records that no walk home exists from session's tile: the drift check found none,
// or a script's walk failed to find a path several times in a row (freshstartrunner).
func NoteNoWayHome(session actions.Session) {
	registryMu.Lock()
	g := registry[session]
	registryMu.Unlock()
	if g == nil {
		return
	}
	g.mu.Lock()
	g.noWayHome = time.Now()
	g.mu.Unlock()
}

// strandHandles is whether a stranded account still handles ev: a Strange box, or a trapped event
// (the maze or the mime has already moved the account out of the site). Everything else is left to
// kill the account or fail-teleport it out.
func strandHandles(ev *Event) bool {
	return ev.Kind == KindTrapped || (ev.Kind == KindBoxLamp && ev.Name == "Strange box")
}

// strandBoxDue is whether a held account has a Strange box to open: kick() lets the box handler have
// that poll instead of the strand hold.
func (g *Guardian) strandBoxDue() bool {
	if !g.strandActive() {
		return false
	}
	ev := g.nextEvent()
	return ev != nil && ev.Kind == KindBoxLamp
}
