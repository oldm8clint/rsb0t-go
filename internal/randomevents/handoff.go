package randomevents

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
)

// targetsAnotherAccount decides the handler's ownership question the same way Detect
// does: a greeting that names an owner wins over the NPC's face. Live 2026-09-19
// (android 02:37:54, craft 03:23:12): each account's own Mysterious Old Man was
// selected by Detect (its greeting named the account), then rejected here because the
// NPC was facing another player; 48s later (four 20-tick macro_mage timers) the ignored
// Old Man fail-teleported each of them to 2898,3423,1 (Taverley), and both died walking
// home over White Wolf Mountain. See speechNamesSelf for why the face moves.
func targetsAnotherAccount(s actions.Session, ev *Event) bool {
	for _, npc := range s.State().NPCs() {
		if npc.Index == ev.NpcIndex {
			if self, named := speechNamesSelf(s.State(), ev.Name, npc); named {
				return !self
			}
			return npc.TargetsAnotherPlayer(s.State().Self())
		}
	}
	return false
}

func rejectDialogNPC(s actions.Session, ev *Event, log func(string)) error {
	// Evidence first: the live the mule/android/craft rejections logged only the verdict, so
	// whether the face, the greeting or a dialogue decided it had to be inferred.
	evidence := rejectionEvidence(s, ev)
	markPlantRejected(s, ev.NpcIndex)
	if log != nil {
		log("not this account's " + ev.Name + " (" + evidence + ") — checking other events")
	}
	// No reason to navigate a rejection through a 20-second dialogue loop.
	return actions.CloseModal(s)
}

func urgentOtherEvent(s actions.Session, ev *Event) *Event {
	return detectEligible(s.State(), -1, func(candidate *Event) bool {
		return (candidate.Kind == KindTrapped || candidate.Kind == KindHazard || candidate.Kind == KindEvade) &&
			eventSignature(candidate) != eventSignature(ev)
	})
}

// Only release a danger handler after live movement has created separation.
// Never return to its original fishing tile as part of this handoff.
func yieldSeparatedEvent(s actions.Session, ev *Event, log func(string)) bool {
	x, z, _, ok := s.State().Tile()
	if !ok || s.State().InCombat() {
		return false
	}
	if ev.Kind == KindEvade {
		for _, npc := range s.State().NPCs() {
			if npc.Index == ev.NpcIndex && chebyshev(x, z, npc.X, npc.Z) <= 15 {
				return false
			}
		}
	} else if chebyshev(x, z, ev.ThreatX, ev.ThreatZ) <= 3 {
		return false
	}
	registryMu.Lock()
	g := registry[s]
	registryMu.Unlock()
	if g == nil {
		return false
	}
	next := g.nextEventExcept(eventSignature(ev))
	if next == nil {
		return false
	}
	deferEvent(s, ev, 2*pollInterval)
	if log != nil {
		log(fmt.Sprintf("safe separation from %s; checking %s", ev.Name, next.Name))
	}
	return true
}

// approachDialogNPC walks close enough to talk to a random event's npc.
//
// It asks pathfind.CanInteractFrom/InteractTiles rather than measuring a distance, because
// both of the obvious shortcuts are wrong here and both were live (2026-09-20: 60 "no
// approach path to Mysterious Old Man at 2615,3397" errors across 21 different accounts in
// one 38-minute run, plus 4 escalating give-ups):
//
//   - `chebyshev(here, target) <= 1` accepts a DIAGONAL. The server's own check does not:
//     an interaction has to come from a tile that orthogonally touches the target with no
//     wall on the facing side. A diagonal click is accepted by the client and ignored by the
//     engine, so the handler waits out its timeout against an npc that never answers. See
//     pathfind/interact.go's own doc comment — this repo had shipped that bug three times
//     before that helper existed.
//   - Pathing to the npc's OWN tile fails whenever that tile is blocked in the collision
//     pack, which is the ordinary case for an npc that teleported next to scenery. FindPath
//     resolves a blocked destination itself, but an npc standing on a WALKABLE tile that is
//     nonetheless unreachable head-on (a fishing spot ledge, a fenced corner) still returns
//     no path, and the account then reports a hard error instead of stepping one tile aside.
//
// InteractTiles answers with every tile the engine would accept, nearest-corner first, so
// walking to the first one that has a route is both correct and the shortest such approach.
//
// All of it is asked of fleePack, the static pack with this session's live door and wall
// changes applied, not of the static pack alone. The map files place most doors CLOSED, so on
// the static pack an account behind any door somebody has opened is sealed in. Live on build
// e35420b6 (2026-09-24 18:16, the mule): the mule's own Mysterious Old Man stood in the Catherby
// bank while the mule was cooking in the range house, whose door (loc 1530 at 2816,3438) was
// standing open — the mule had walked in through it seconds earlier. Every approach answered
// "no approach path", the guardian gave up after four tries in four seconds, the Old Man
// fail-teleported the mule to the Lumbridge basement 38 seconds later, and the whole fleet had
// no mule for the next three hours and eighteen minutes.
func approachDialogNPC(s actions.Session, pack *pathfind.Pack, ev *Event, x, z, level int, log func(string)) (bool, error) {
	nav := fleePack(s, pack)
	here := pathfind.NavPoint{X: x, Z: z, Level: level}
	target := pathfind.NavPoint{X: ev.ThreatX, Z: ev.ThreatZ, Level: level}
	if nav.CanInteractFrom(here, target) {
		return false, nil
	}
	var res pathfind.PathResult
	for _, goal := range nav.InteractTiles(target) {
		if r := nav.FindPath(here, goal); r.OK {
			res = r
			break
		}
	}
	if !res.OK {
		// Behind a door that is shut on the live overlay too: open the first closed door on the
		// way (evade_door.go) and let the guardian's next attempt walk through it. The Ardougne
		// Drunken Dwarf and Strange plant approaches that failed live on e35420b6 had doors
		// within six tiles of the npc.
		if approachThroughDoor(s, nav, here, target, log) {
			return true, nil
		}
		if unreachableForeignPlant(s.State(), ev) {
			markPlantRejected(s, ev.NpcIndex)
			if log != nil {
				log(fmt.Sprintf("not this account's %s (no route to %d,%d from here, and %s) — checking other events",
					ev.Name, ev.ThreatX, ev.ThreatZ, plantSpawnSound(s.State(), ev.NpcIndex)))
			}
			return true, nil
		}
		// Only for the two NPCs whose ignored timer is a relocation: a Drunken Dwarf or a Strange plant that cannot be
		// reached turns hostile, so those keep trying and keep saying so.
		if !failTeleportDialogNames[ev.Name] {
			deferEvent(s, ev, 2*pollInterval)
			return true, fmt.Errorf("no approach path to %s at %d,%d", ev.Name, ev.ThreatX, ev.ThreatZ)
		}
		if n := noteNoApproachPath(s, ev, time.Now()); n > noApproachPathErrors {
			// Nothing about the world has changed in n tries: stop reporting an error every four seconds and
			// recheck at a slow pace instead. The event is left to its own timer (a Genie or Old Man fail-teleports
			// the account, which is what happens to one that cannot be answered anyway).
			if n == noApproachPathErrors+1 {
				if log != nil {
					log(fmt.Sprintf("no way to %s at %d,%d from %d,%d after %d tries (no interaction tile has a route, and no closed door on the way): leaving it to its own timer, rechecking every %s",
						ev.Name, ev.ThreatX, ev.ThreatZ, x, z, n-1, noApproachPathRecheck))
				}
			}
			deferEvent(s, ev, noApproachPathRecheck)
			return true, nil
		}
		deferEvent(s, ev, 2*pollInterval)
		return true, fmt.Errorf("no approach path to %s at %d,%d", ev.Name, ev.ThreatX, ev.ThreatZ)
	}
	forgetNoApproachPath(s, ev)
	if energy, known := s.State().Energy(); wantRunForFlee(s.State().RunEnabled(), energy, known) {
		_ = actions.SetRun(s, true)
	}
	if err := actions.Walk(s, res.Waypoints, true); err != nil {
		return false, err
	}
	interrupted := waitEventReply(s, pack, ev, time.Duration(len(res.Waypoints)+3)*700*time.Millisecond, func() bool {
		nx, nz, nl, ok := s.State().Tile()
		// Same rule as the approach above, not chebyshev: arriving on a diagonal is not
		// arriving, and treating it as such is what left the account standing beside an
		// npc it could never talk to.
		return (ok && nl == level && nav.CanInteractFrom(pathfind.NavPoint{X: nx, Z: nz, Level: nl}, target)) || targetsAnotherAccount(s, ev) || !npcStillPresent(s, ev.NpcIndex) || npcHasGoneHostile(s, ev.NpcIndex)
	}, log)
	return interrupted || !npcStillPresent(s, ev.NpcIndex), nil
}

// noApproachPathErrors is how many consecutive "no approach path" answers an event's approach reports as errors
// before it goes quiet (noApproachPathRecheck). Live 6 Oct 2026, acct21 at the Wizards' Tower west room: its
// Mysterious Old Man stood at 3105,3162 inside the room the strand exit had just walked the account out of (the
// room's diagonal door is sealed in the pack), and the handler answered "no approach path" every four seconds
// for 38 seconds until the Old Man fail-teleported the account.
const noApproachPathErrors = 3

// noApproachPathRecheck is how long an event with no route to it waits before its next look once the errors stop.
const noApproachPathRecheck = 12 * time.Second

type noPathKey struct {
	s   actions.Session
	npc int
}

type noPathRun struct {
	n    int
	last time.Time
}

var (
	noPathMu   sync.Mutex
	noPathRuns = map[noPathKey]*noPathRun{}
)

// noteNoApproachPath counts one more consecutive failed approach on the event's NPC for this session and
// returns the count. A run not added to for a minute starts again (the NPC left, or the account moved).
func noteNoApproachPath(s actions.Session, ev *Event, now time.Time) int {
	noPathMu.Lock()
	defer noPathMu.Unlock()
	for k, r := range noPathRuns {
		if now.Sub(r.last) > time.Minute {
			delete(noPathRuns, k)
		}
	}
	k := noPathKey{s, ev.NpcIndex}
	r := noPathRuns[k]
	if r == nil {
		r = &noPathRun{}
		noPathRuns[k] = r
	}
	r.n++
	r.last = now
	return r.n
}

// forgetNoApproachPath ends the run: a route exists again.
func forgetNoApproachPath(s actions.Session, ev *Event) {
	noPathMu.Lock()
	delete(noPathRuns, noPathKey{s, ev.NpcIndex})
	noPathMu.Unlock()
}

// unreachableForeignPlant is whether ev is a Strange plant that this account has no proof is its
// own, called once no route to it exists. Content plays triffid_appear to the plant's owner alone, so
// a plant this account owns is one whose spawn sound it heard. A plant that no route reaches and whose
// sound this account has not heard in a plant's whole lifetime is another player's, seen over a fence;
// trying again cannot make it ours.
//
// The sound is looked for in plantMayBeOurs' window (a plant's lifetime), not around the tick the plant
// came into view as plantSpawnSound's evidence line does: a plant that leaves the account's view and
// comes back starts counting again, and the owner of one read "spawn sound heard=false" at 10 ticks in
// view (acct16, 17:28:17) and picked its fruit 12 seconds later.
//
// Live 5 Oct 2026: acct141 (14:55:19) and Acct226 (16:57:57), walking the Ardougne to Catherby road
// within sight of the Ranging Guild's compound (a fence of spear walls, its doors diagonal and shut,
// the competition judge at 2669,3418 inside), answered "no approach path to Strange plant at
// 2672,3418" (2672,3420) 7 and 8 times, two seconds apart and a Guardian attempt each, and Acct171
// (15:54:34), standing at 2684,3311, 12 times for a plant 13 tiles off at 2671,3316, until each plant
// left view. plantMayBeOurs admits every plant to an account that has heard no sound since login (the
// log does not say whether these three had), and the approach then fails for lack of any path. An
// account that did hear the plant appear keeps trying as before.
func unreachableForeignPlant(state *gamestate.State, ev *Event) bool {
	if ev == nil || !strings.EqualFold(ev.Name, "Strange plant") {
		return false
	}
	for _, n := range state.NPCs() {
		if n.Index == ev.NpcIndex {
			heard, _ := state.HeardSoundNear(gamestate.SoundTriffidAppear, state.Tick()-plantLifetimeTicks/2, plantLifetimeTicks/2)
			return !heard
		}
	}
	return false
}

// combatReaction is what an event handler does when the account comes under attack while it works.
type combatReaction int

const (
	// reactFlee: get away from it, the ordinary reaction and the only one for a script whose combat is not
	// expected.
	reactFlee combatReaction = iota
	// reactCarryOn: the fight is the script's own and the event is harmless, so keep working on the event.
	reactCarryOn
	// reactStepAside: the fight needs the script (to eat, or to break off for the bank), so defer the event
	// and give the lock back.
	reactStepAside
)

const (
	// carryOnFromHP is the share of maximum hitpoints at or above which an account whose combat is expected
	// keeps working on a harmless event while it is under attack. Below it the handler steps aside so its
	// script can eat or bank.
	carryOnFromHP = 0.5

	// combatEventDefer is how long a harmless event is left alone after a handler stepped aside for the
	// account's sake.
	combatEventDefer = 20 * time.Second
)

// combatReactionFor decides what a handler working on ev does now that the account is under attack, pure
// over the session's state.
//
// WHY AN ACCOUNT THAT IS FIGHTING ON PURPOSE DOES NOT FLEE FROM A BOX. Every handler used to answer an attack
// by walking away for up to fleeWalkDeadline (20 s), holding the action lock the whole time, and then
// answer the next poll's attack the same way. For a trainer standing among an aggressive herd (a cave full
// of Ice warriors, always in combat by design) that was a loop with no way out: a harmless Strange box, left
// by a Mysterious Old Man's fail line, was "handled" 21 times in 7 minutes at 3059,9577, each ending in a
// 20 s flee that had nowhere to go, while the trainer, held out of the way by Busy(), could not eat, and the
// account died with an unopened box in its pack (live 17 Oct 2026, 01:15-01:22). The same loop met a Strange
// plant that was never picked, because the pick's reply was never read, and the plant turned on its owner 54
// seconds after it spawned (death at 16:07 on 16 Oct and three before it).
//
// An event that is only a puzzle or a dialogue (KindBoxLamp, KindDialogNpc) cannot hurt the account, and
// whatever is attacking it is the script's business, not the handler's: the handler carries on while the
// account has hitpoints to spare, and steps aside when it has not. An event that can hurt (KindEvade,
// KindHazard) and a dialogue NPC that has turned hostile are fled as before, and so is every event of an
// account whose combat is NOT expected, where being attacked is a surprise.
func combatReactionFor(s actions.Session, ev *Event) combatReaction {
	if ev == nil || !combatExpectedFor(s) {
		return reactFlee
	}
	if ev.Kind != KindBoxLamp && ev.Kind != KindDialogNpc {
		return reactFlee
	}
	if ev.NpcIndex >= 0 && npcHasGoneHostile(s, ev.NpcIndex) {
		return reactFlee
	}
	hp, okHP := s.State().HP()
	max, okMax := s.State().MaxHP()
	if !okHP || !okMax || max <= 0 || float64(hp) < carryOnFromHP*float64(max) {
		return reactStepAside
	}
	return reactCarryOn
}

// refundAttempt takes back the attempt kick() counted for ev's signature before it ran the handler.
func refundAttempt(session actions.Session, ev *Event) {
	registryMu.Lock()
	g := registry[session]
	registryMu.Unlock()
	if g == nil || ev == nil {
		return
	}
	sig := eventSignature(ev)
	g.mu.Lock()
	if g.attempts[sig] > 0 {
		g.attempts[sig]--
	}
	g.mu.Unlock()
}

// abortForCombat applies combatReactionFor for a handler that has found the account under attack. what
// finishes the log line ("while checking the event"). It reports whether the handler must return now: after
// fleeing, or after deferring the event and giving the lock back; false for a handler that carries on.
func abortForCombat(s actions.Session, pack *pathfind.Pack, ev *Event, what string, log func(string)) bool {
	switch combatReactionFor(s, ev) {
	case reactCarryOn:
		return false
	case reactStepAside:
		_ = actions.CloseModal(s)
		deferEvent(s, ev, combatEventDefer)
		refundAttempt(s, ev) // stepping aside is not a try: four of them must not make the guardian give the event up
		if log != nil {
			hp, _ := s.State().HP()
			max, _ := s.State().MaxHP()
			log(fmt.Sprintf("in combat %s at %d/%d hitpoints — the fight is the script's, so %s waits %s (%s)",
				what, hp, max, ev.Name, combatEventDefer, attackerDiag(s.State())))
		}
		return true
	}
	if log != nil {
		log("in combat " + what + " — fleeing immediately (" + attackerDiag(s.State()) + ")")
	}
	fleeFromCombat(s, pack, baseFleeDist, log)
	return true
}

// Poll replies as they arrive, checking combat and new urgent events every 150ms.
// Returning true releases this handler so the guardian can dispatch the next event.
func waitEventReply(s actions.Session, pack *pathfind.Pack, ev *Event, duration time.Duration, ready func() bool, log func(string)) bool {
	deadline := time.Now().Add(duration)
	for {
		registryMu.Lock()
		g := registry[s]
		registryMu.Unlock()
		if g != nil {
			select {
			case <-g.stop:
				return true
			default:
			}
		}
		// underAttack, not only InCombat: a recorded attacker between its hits still wins
		// over the event being checked (see pursuingAttacker).
		if underAttack(s) && abortForCombat(s, pack, ev, "while checking the event", log) {
			return true
		}
		if ev != nil && ev.Kind == KindBoxLamp && driftRecoveryWaiting(s) {
			if log != nil {
				log("yielding " + ev.Name + " to drift recovery")
			}
			return true
		}
		if next := urgentOtherEvent(s, ev); next != nil {
			_ = actions.CloseModal(s)
			deferEvent(s, ev, pollInterval)
			if log != nil {
				log(fmt.Sprintf("yielding to %s instead of waiting on %s", next.Name, ev.Name))
			}
			return true
		}
		if ready != nil && ready() {
			return false
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		if remaining > 150*time.Millisecond {
			remaining = 150 * time.Millisecond
		}
		time.Sleep(remaining)
	}
}

// rejectionEvidence describes what the ownership verdict was based on: the NPC's face
// against this account's slot, the owner its greeting names, and any open dialogue text.
func rejectionEvidence(s actions.Session, ev *Event) string {
	state := s.State()
	out := fmt.Sprintf("self slot %d", state.Self())
	for _, npc := range state.NPCs() {
		if npc.Index != ev.NpcIndex {
			continue
		}
		out += fmt.Sprintf(", face %d", npc.FaceEntity)
		if strings.EqualFold(ev.Name, "Strange plant") {
			out += ", " + plantSpawnSound(state, ev.NpcIndex)
		}
		if sound := ownerSoundEvidence(state, ev.Name); sound != "" {
			out += ", " + sound
		}
		if owner := npcSpeechOwner(ev.Name, npc); owner != "" {
			out += ", greeting names " + owner
		}
		if npc.Speech != "" && speechOwner(ev.Name, npc.Speech) == "" {
			out += fmt.Sprintf(", speech %q", npc.Speech)
		}
		break
	}
	if components := currentInterfaceArchive(); components != nil {
		if chat := actions.NewChatDialog(s, components); chat.IsOpen() {
			out += fmt.Sprintf(", dialogue %q", strings.Join(chat.Texts(), " "))
		}
	}
	return out
}
