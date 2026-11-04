package randomevents

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
)

// compass mirrors eventEvade.ts's own 8-direction ring order.
var compass = [8][2]int{
	{1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1},
}

// fleeCandidates ports eventEvade.ts's fleeCandidates: rings outward from dist down to
// a 4-tile floor, farthest-from-the-threat first, deduplicated.
func fleeCandidates(fromX, fromZ, level int, threatX, threatZ, dist int) []pathfind.NavPoint {
	const minFleeDist = 4
	const ringStep = 2
	seen := make(map[[2]int]bool)
	var out []pathfind.NavPoint
	for d := dist; d >= minFleeDist; d -= ringStep {
		for _, c := range compass {
			p := [2]int{fromX + c[0]*d, fromZ + c[1]*d}
			if seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, pathfind.NavPoint{X: p[0], Z: p[1], Level: level})
		}
	}
	// Farthest-from-threat first, matching the original's sort.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			di := chebyshev(out[j].X, out[j].Z, threatX, threatZ)
			dj := chebyshev(out[j-1].X, out[j-1].Z, threatX, threatZ)
			if di <= dj {
				break
			}
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// CatherbyBankRef is a known-safe reference point (every ProgressiveFisher account
// already walks here to bank) southwest of both the Catherby fishing spots and every
// White wolf sighting logged this session (2842,3450 / 2847,3475 / 2849,3487 — three
// separate live reports, all north/northeast of the fishing spots). Used by
// biasedFleeCandidates below, and exported so fisherrunner can pass it as EmergencyFlee's
// own home/driftTiles pair — see that function's own doc comment for why every caller
// needs to supply its own reference now instead of this package assuming Catherby.
var CatherbyBankRef = pathfind.NavPoint{X: 2809, Z: 3441, Level: 0}

// biasedFleeCandidates ports fleeCandidates' own ring generation and farthest-from-threat
// sort, then prefers only the candidates that also move generally toward `toward` over
// ones that don't (falls back to the unfiltered list if that leaves nothing, e.g. already
// at/past the target). Live-found 2026-09-15: fleeCandidates' "farthest from threat" sort
// is a no-op whenever threat==self (HandleHazard, and guardian.go's own in-combat
// immediate flee both call it that way, since neither has a specific NPC to point away
// from) — every ring candidate sits at the same Chebyshev distance from a same-as-self
// "threat", so the stable sort leaves candidates in raw compass-generation order, and
// Catherby's coastline means most compass directions are simply unwalkable (water) — so
// the same inland direction kept winning by elimination every single time, straight into
// an unmapped White wolf pack, no matter how far dangerZones below were widened (user's
// diagnosis: "theyre moving way too far north probably when avoiding random events").
// Rather than keep chasing the pack's true extent one live sighting at a time, this biases
// every "flee, no specific direction to avoid" toward known-safe ground instead.
func biasedFleeCandidates(fromX, fromZ, level, threatX, threatZ int, toward pathfind.NavPoint, dist int) []pathfind.NavPoint {
	all := fleeCandidates(fromX, fromZ, level, threatX, threatZ, dist)
	tx, tz := toward.X-fromX, toward.Z-fromZ
	var biased []pathfind.NavPoint
	for _, c := range all {
		dx, dz := c.X-fromX, c.Z-fromZ
		if dx*tx+dz*tz > 0 {
			biased = append(biased, c)
		}
	}
	if len(biased) > 0 {
		return biased
	}
	return all
}

// dangerZone is a Chebyshev-radius area walkableFlee should never pick a flee candidate
// inside of. Unlike a route-through avoidance (which can break travel entirely if the
// zone sits on the only path — see freshstartrunner's own dangerZones, deliberately left
// empty for exactly that reason), this is safe here: fleeCandidates already offers up to
// 8 compass directions at each ring distance, so excluding candidates in one direction
// still leaves plenty of others to pick from instead of forcing a route through the
// excluded area.
type dangerZone struct {
	Center pathfind.NavPoint
	Radius int
}

// dangerZones lists areas a flee should never land in. Found live 2026-09-15 (a
// 200-account soak): several ProgressiveFisher accounts fled a hostile event-monster
// random event near the Catherby fishing spots and were killed by aggressive White wolves
// (npc type 96, confirmed via a live ::tele + NPC scan at the actual flee destinations
// several accounts landed on: 2858,3457 and nearby, wolves visible at 2872,3444 and
// 2863,3452) — the threat NPC's own position relative to the fishing spot happens to make
// the farthest-from-threat candidates consistently fall in the wolves' direction.
var dangerZones = []dangerZone{
	// Widened 2026-09-15 mid-soak: the original 20-tile radius (centered on the first
	// sighting) was too small — a second live ::tele+scan at a NEW death location
	// (2848,3473) found three more White wolves (2846,3479 / 2850,3487 / 2846,3477),
	// confirming the pack's real footprint runs at least from z~3444 to z~3487, well past
	// the original zone's z~3468 edge. Centered/sized generously over the whole observed
	// footprint with real margin rather than the exact sighted points, since fleeCandidates
	// can place a candidate anywhere on an 8-direction ring and further sightings would
	// likely just keep finding the edge again.
	{Center: pathfind.NavPoint{X: 2860, Z: 3475, Level: 0}, Radius: 45},
}

func inDangerZone(p pathfind.NavPoint) bool {
	for _, z := range dangerZones {
		if z.Center.Level != p.Level {
			continue
		}
		if chebyshev(p.X, p.Z, z.Center.X, z.Center.Z) <= z.Radius {
			return true
		}
	}
	return false
}

// walkableFlee finds the first flee candidate the collision pack can actually route
// to, mirroring the original's Reachability.canReach filter.
func walkableFlee(pack *pathfind.Pack, fromX, fromZ, level int, candidates []pathfind.NavPoint) (pathfind.NavPoint, bool) {
	from := pathfind.NavPoint{X: fromX, Z: fromZ, Level: level}
	for _, c := range candidates {
		if inDangerZone(c) {
			continue
		}
		if pack.FindPath(from, c).OK {
			return c, true
		}
	}
	return pathfind.NavPoint{}, false
}

// walkTo is this file's own shared "issue one recovery/escape walk" helper — HandleHazard's
// step-aside-and-wait and emergencyFlee's drift-recovery branch (the no-cheats path; the
// CheatEnabled path teleports instead and never reaches here) both funnel through it. Turns
// running on first, reusing wantRunForFlee/flee_walking.go's own "spend down to the very
// last point of energy" floor rather than fisherrunner's much higher cruising threshold:
// both of walkTo's callers are themselves already escape/recovery actions (a hazard to step
// away from, or a punishment-teleport/stranding to walk back from), the same urgency
// fleeFromCombatWalking's own combat flee already applies energy for, not ordinary cruising
// travel. actions.Walk's own run bit is set true too, exactly like fleeFromCombatWalking's
// identical call — SetRun's IF_BUTTON is a separate round trip the server may not have
// acknowledged yet by the time this click goes out, so ctrlHeld forces this specific click to
// run regardless, the same belt-and-suspenders reasoning that call already relies on.
func walkTo(s actions.Session, pack *pathfind.Pack, fromX, fromZ, level int, dest pathfind.NavPoint) error {
	res := pack.FindPath(pathfind.NavPoint{X: fromX, Z: fromZ, Level: level}, dest)
	if !res.OK {
		return nil
	}
	if energy, energyOK := s.State().Energy(); wantRunForFlee(s.State().RunEnabled(), energy, energyOK) {
		_ = actions.SetRun(s, true)
	}
	// What one MOVE_GAMECLICK can actually carry; the caller's next attempt walks the rest.
	sent := actions.SendableWaypoints(res.Waypoints)
	if err := actions.Walk(s, sent, true); err != nil {
		return err
	}
	// The rough travel budget matching cmd/walktest/npctest's pacing, now BOUNDED and ended early
	// on arrival: it used to be an untruncated sleep of 0.7 s per waypoint, and with walks on the
	// live overlay reaching further it could hold the guardian's action lock for most of a
	// minute after the account had already arrived (review of claude/handoff-misses).
	last := sent[len(sent)-1]
	budget := time.Duration(len(sent)+3) * 700 * time.Millisecond
	if budget > walkToMaxWait {
		budget = walkToMaxWait
	}
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if x, z, l, ok := s.State().Tile(); ok && x == last.X && z == last.Z && l == last.Level {
			return nil
		}
		time.Sleep(walkToPoll)
	}
	return nil
}

// walkToMaxWait bounds walkTo's wait for one click's walk; walkToPoll is how often it checks.
const (
	walkToMaxWait = 20 * time.Second
	walkToPoll    = 300 * time.Millisecond
)

// whirlpoolEngaged reports whether this account may be rolling on the whirlpool ev names: it
// faces it, or it stands beside it (a click that crossed the type change can land before the
// face update shows it, the same allowance fisherrunner's rollingOnWhirlpool makes).
//
// Nothing else about a whirlpool can hurt an account. The type change clears everyone's
// interaction with the spot, and content's macro_event_whirlpool only rolls for a player whose
// click reached the server while it was a whirlpool (fisherrunner/whirlpool.go has the
// content trace). Detect reports every whirlpool within 3 tiles, whoever it belongs to, so on
// a crowded fishing camp most handles were for someone else's: live on build e35420b6 over 37
// hours, 20,128 of 29,100 whirlpool handles came with no step-off by the account's own fisher
// in the 5 seconds before, and each walked the account 4 tiles off the spot it was fishing
// (21.6 hours of handling, plus the walk back and the re-click).
func whirlpoolEngaged(state *gamestate.State, ev *Event) bool {
	if face := state.SelfFaceEntity(); face >= 0 && face == ev.NpcIndex {
		return true
	}
	x, z, _, ok := state.Tile()
	if !ok {
		return true
	}
	return chebyshev(x, z, ev.ThreatX, ev.ThreatZ) <= 1
}

// HandleHazard ports handleHazard: step a short distance away and wait it out. Fully
// numeric — no name resolution needed, since the event itself was detected by id.
func HandleHazard(s actions.Session, pack *pathfind.Pack, ev *Event, log func(string)) error {
	preX, preZ, preLevel, preTileOK := s.State().Tile()
	if !preTileOK {
		return nil
	}
	if whirlpoolNpcIDs[ev.NpcType] && !whirlpoolEngaged(s.State(), ev) {
		// Nothing to step away from: see whirlpoolEngaged. Walking anyway pulled the account
		// off whatever spot it was fishing, for a whirlpool it never clicked.
		deferEvent(s, ev, whirlpoolLifetime)
		if log != nil {
			log(fmt.Sprintf("whirlpool at %d,%d: %d tiles away and not fishing it — nothing to step away from", ev.ThreatX, ev.ThreatZ, chebyshev(preX, preZ, ev.ThreatX, ev.ThreatZ)))
		}
		return nil
	}
	candidates := biasedFleeCandidates(preX, preZ, preLevel, preX, preZ, safeRefFor(s), 4)
	// Chosen on the same live door overlay the walk uses (review of claude/handoff-misses: the
	// step-aside planned on the static pack and walked on the live one).
	live := fleePack(s, pack)
	if dest, found := walkableFlee(live, preX, preZ, preLevel, candidates); found {
		if err := walkTo(s, live, preX, preZ, preLevel, dest); err != nil {
			return err
		}
	}
	// A whirlpool is harmless unless clicked: content macro_event_whirlpool only takes the
	// owner's equipment after repeated clicks, never attacks or teleports, and the spot
	// reverts after 60 ticks. The fisher already skips spots beside one (WhirlpoolNearby).
	// Waiting it out below held the whole Guardian for 36s: live 2026-09-19 that was 530
	// waits in one run, by every account at the spot, not only the owner, and acct6 died
	// when its own Strange plant turned angry during one. Step clear and leave it deferred.
	if whirlpoolNpcIDs[ev.NpcType] {
		deferEvent(s, ev, whirlpoolLifetime)
		if log != nil {
			log("whirlpool: stepped clear; not waiting for it to revert")
		}
		return nil
	}
	// sleepWatchingCombatOrDrift, not sleepWatchingCombat -- live-found 2026-09-16
	// (lc2b_50/lc2b_53/lc2b_133, same 200-account soak): checking drift only once at the
	// end of this 36-second wait (~60 ticks, matching Execution.delayTicks(60)) meant a
	// whirlpool's own "ignored too long" fail-teleport landing partway through the wait
	// went unnoticed for however much of the 36s was left — long enough for the account's
	// own fishing loop to declare a stranding and fall into freshstartrunner's
	// pickpocket+boat fallback (no combat protection at all) before this function ever
	// got a chance to call checkFailTeleport itself. Polling both combat AND drift every
	// 300ms closes that window the same way every other handler's own per-chunk check
	// already does elsewhere in this file. See sleepWatchingCombatOrDrift's own doc
	// comment for the live trace this was found from.
	deadline := time.Now().Add(36 * time.Second)
	for time.Now().Before(deadline) {
		if combat, drifted := sleepWatchingCombatOrDrift(s, 300*time.Millisecond, preX, preZ, preLevel); combat {
			fleeFromCombat(s, pack, baseFleeDist, log)
			return nil
		} else if drifted {
			checkFailTeleport(s, pack, "whirlpool", preX, preZ, preLevel, preTileOK, log)
			return nil
		}
		if yieldSeparatedEvent(s, ev, log) {
			return nil
		}
	}
	// Live-found 2026-09-16 (lc2b_78, same soak, right after the SAME class of gap was
	// fixed for the dialog-NPC events): a whirlpool is not just a passive step-aside-
	// and-wait hazard the way gas/smoking-rocks are -- "whirlpool — cleared" was
	// immediately followed by the account showing up on a completely different level in
	// a different part of the map (2954,3388,2), exactly the same "ignored/mishandled"
	// fail-teleport shape already found for Genie/Mysterious Old Man/Drunken Dwarf/
	// Strange Plant, just never checked for here since a hazard was assumed to always
	// resolve in place. Kept as a final catch-all in case the account drifted in the
	// instant between the last poll and the deadline.
	checkFailTeleport(s, pack, "whirlpool", preX, preZ, preLevel, preTileOK, log)
	return nil
}

// baseFleeDist/fleeDistStep/maxFleeDist control HandleEvade's escalation — see its own
// doc comment for why this exists: a live production test found a real, reproducible
// case (an "event monster" — one of the hostile evade-set NPCs, e.g. Swarm/Tree
// Spirit, both documented to actively chase rather than stand still) that took over two
// minutes and multiple failed attempts to actually clear, each attempt fleeing to the
// same fixed 12-tile ring and finding the threat still present afterward. Escalating the
// flee distance on each successive failed attempt against the *same* event gives a
// chasing NPC an actual chance of being outrun rather than repeating an identical,
// already-failed flee forever (bounded regardless by Guardian's own maxAttempts/
// giveUpCooldown either way).
const (
	// baseFleeDist raised 12 -> 20 2026-09-16: live 200-account soak data (v87) showed
	// repeated 12-tile flees against the same lingering danger never actually created
	// separation — the combat-flee streak that's supposed to escalate distance kept
	// resetting to 1 between hits (InCombat's own 6s window clears between separate hits
	// from a hostile that isn't attacking every single tick), so real encounters were
	// effectively always fleeing the original, insufficient 12-tile distance repeatedly.
	// Several accounts died despite multiple logged flees, all at "streak 1, dist 12".
	// Raising the floor itself (rather than only relying on escalation, which isn't
	// reliably reached) directly narrows the exposure window on the very first flee.
	baseFleeDist = 20
	fleeDistStep = 8
	maxFleeDist  = 40
	// farTeleportGuard is how far from its own safe reference an account may be and still
	// be teleported to it by the combat flee — see fleeFromCombat's own use of it for the
	// live cross-map stranding this stops. Matches the 150-tile drift threshold every
	// EmergencyFlee caller already uses to decide "this account is not where it should
	// be", deliberately: the two questions are the same one.
	farTeleportGuard = 150
)

// fleeDistFor returns the flee ring distance for the given 1-based attempt number
// (Guardian.noteAttempt's own return value; attempt<=1 uses baseFleeDist unescalated).
func fleeDistFor(attempt int) int {
	d := baseFleeDist + fleeDistStep*(attempt-1)
	if d > maxFleeDist {
		d = maxFleeDist
	}
	if d < baseFleeDist {
		d = baseFleeDist
	}
	return d
}

// combatAvoidWindow/combatAvoidRadius govern how long and how wide a spot fleeFromCombat
// just ran from stays marked dangerous — see RecentlyAttackedNear's own doc comment for the
// live re-engagement bug this exists to close. Matches markPlantRejected's own 2-minute
// sticky-exclusion window: the same "don't immediately go back to the thing that just
// caused trouble" shape already established in this file, for combat instead of a rejected
// dialog NPC. The radius is generous relative to fisherplan.Catherby's own SpotRange (20)
// so a fresh flee actually rules out a meaningful chunk of the camp, not just the one exact
// spot tile a wandering hostile has likely already left.
const (
	combatAvoidWindow = 2 * time.Minute
	combatAvoidRadius = 15
)

// recentCombatSpot is one session's most recent fleeFromCombat: where the account was
// standing when the fight started (not the flee destination), and until when that spot
// should still be treated as dangerous.
type recentCombatSpot struct {
	x, z, level int
	until       time.Time
	// attackers are the NPCs identified as the source of the fight when it was recorded. When
	// non-empty, the spot stops counting as dangerous the moment none of them is still in view
	// — see RecentlyAttackedNear. Empty means the attacker could not be identified, and the
	// full window applies unchanged.
	attackers []combatAttacker
}

// combatAttacker identifies one NPC by index AND type. The type is what makes a despawn
// detectable: the server reuses NPC indices, so an index alone could be taken over by an
// unrelated NPC and keep a spot blocked for no reason — or, worse, a reused index could be
// mistaken for the attacker having left. Requiring both to match makes a reused index read as
// "the attacker is gone", which is correct.
type combatAttacker struct {
	index, typ int
}

// recentCombat maps actions.Session -> recentCombatSpot, the same unbounded-but-harmless
// per-session-map shape driftLogThrottle already uses in this file (accounts are
// process-lifetime objects, not something this map needs to evict).
var recentCombat sync.Map

// noteCombatFlee records that fleeFromCombat just ran from x,z,level, so
// RecentlyAttackedNear can steer callers away from it for combatAvoidWindow — or for less,
// once the attacker recorded here is no longer in view.
func noteCombatFlee(s actions.Session, x, z, level int) {
	noteCombatFleeFrom(s, x, z, level, -1, -1)
}

// noteCombatFleeFrom is noteCombatFlee for a caller that already KNOWS which NPC it is fleeing
// (threatIndex/threatType, both -1 when unknown). The known threat is recorded alongside
// whatever identifyAttackers can infer from facing.
//
// Why this exists — measured live 2026-09-19, one 1.6h run on f1abfe3: almost every combat
// block the fisher honours comes from HandleEvade's "event monster" path (1,496 combat flees
// and 1,425 evades, against 25 generic fights). Those monsters are gone fast — median 6s from
// first sighting to "cleared" — yet accounts waited a median 120s after "cleared" before
// catching the next fish, i.e. the full combatAvoidWindow. f31720f's early lift never fired for
// them because HandleEvade flees PROACTIVELY, before the monster has attacked: it is not yet
// facing the player and auto-retaliate has not turned toward it, so identifyAttackers found
// nobody and the unknown-attacker fallback kept the full two minutes. HandleEvade has the exact
// NPC in hand (ev.NpcIndex/NpcType); it was simply never passed down. ~800 of those two-minute
// waits in 1.6h is on the order of 15% of the whole fleet's fishing time.
func noteCombatFleeFrom(s actions.Session, x, z, level, threatIndex, threatType int) {
	attackers := identifyAttackers(s.State())
	if threatIndex >= 0 && threatType >= 0 {
		attackers = addAttacker(attackers, combatAttacker{index: threatIndex, typ: threatType})
	}
	// Merge into a still-live record instead of replacing it. HandleEvade records the known
	// threat, and a combat flee moments later (the monster has now attacked) records again;
	// a plain replace could drop the known threat if facing happened not to identify it at
	// that instant, silently restoring the full window. A union can only keep a spot
	// blocked longer, never lift it wrongly.
	if v, ok := recentCombat.Load(s); ok {
		if prev := v.(recentCombatSpot); time.Now().Before(prev.until) {
			for _, a := range prev.attackers {
				attackers = addAttacker(attackers, a)
			}
		}
	}
	recentCombat.Store(s, recentCombatSpot{
		x: x, z: z, level: level,
		until:     time.Now().Add(combatAvoidWindow),
		attackers: attackers,
	})
}

// identifyAttackers returns the NPCs this account is fighting: the one auto-retaliate turned
// the player toward (SelfFaceEntity is an NPC index below 32768), plus any NPC facing the
// player. Returns nil when neither can be established, which RecentlyAttackedNear treats as
// "unknown attacker — keep the full window". Only NPCs that can fight count (canAttack): a
// fisher faces the fishing spot it is netting, and that spot is not an attacker.
func identifyAttackers(state *gamestate.State) []combatAttacker {
	if state == nil {
		return nil
	}
	self := state.Self()
	face := state.SelfFaceEntity()
	var out []combatAttacker
	for _, n := range state.NPCs() {
		if (n.Index == face || (self >= 0 && n.TargetsPlayer(self))) && canAttack(n) {
			out = append(out, combatAttacker{index: n.Index, typ: n.Type})
		}
	}
	return out
}

// RecentCombatDiag explains, for a log line, why this account's post-fight spot block is
// holding: how long the window has left and each recorded attacker with whether it is still in
// view. Diagnostic only — nothing branches on it.
//
// Added 2026-09-19 after two fixes aimed at the same idle time both failed to move it (the gap
// from "event monster — cleared" to the next catch stayed at a median ~120s). Content says a
// river troll deletes itself within ~12-24s of losing its target (macro_event_lost_hostile on a
// 20-tick ai_timer), yet the block keeps finding its recorded attacker in view. Rather than guess
// a third time, the pause line now says which NPC is holding it.
func RecentCombatDiag(s actions.Session) string {
	v, ok := recentCombat.Load(s)
	if !ok {
		return "no combat record"
	}
	spot := v.(recentCombatSpot)
	left := time.Until(spot.until).Round(time.Second)
	if len(spot.attackers) == 0 {
		return fmt.Sprintf("window %s left, attacker unidentified", left)
	}
	var npcs []gamestate.NPC
	if st := s.State(); st != nil {
		npcs = st.NPCs()
	}
	parts := make([]string, 0, len(spot.attackers))
	for _, a := range spot.attackers {
		where := "gone"
		for _, n := range npcs {
			if n.Index == a.index && n.Type == a.typ {
				where = fmt.Sprintf("in view at %d,%d", n.X, n.Z)
				break
			}
		}
		parts = append(parts, fmt.Sprintf("npc %d type %d %s", a.index, a.typ, where))
	}
	return fmt.Sprintf("window %s left, attackers: %s", left, strings.Join(parts, "; "))
}

// addAttacker appends a to list unless it is already there.
func addAttacker(list []combatAttacker, a combatAttacker) []combatAttacker {
	for _, x := range list {
		if x == a {
			return list
		}
	}
	return append(list, a)
}

// attackerStillPresent reports whether any recorded attacker is still in this account's view,
// matched by index and type together (see combatAttacker).
func attackerStillPresent(state *gamestate.State, attackers []combatAttacker) bool {
	if state == nil {
		return true // no way to tell — err on the side of keeping the spot blocked
	}
	npcs := state.NPCs()
	self := state.Self()
	for _, a := range attackers {
		for _, n := range npcs {
			// A monster now locked onto another player is chasing its own owner, not us.
			if n.Index == a.index && n.Type == a.typ && !n.TargetsAnotherPlayer(self) {
				return true
			}
		}
	}
	return false
}

// RecentlyAttackedNear reports whether x,z,level sits within radius of session's own most
// recent fleeFromCombat, inside that flee's own avoidance window — the combat equivalent of
// markPlantRejected's stickyRejectedIndex, for a spot-selection caller like
// fisherrunner.FindSpot (mirroring how that function already skips a candidate near an
// active whirlpool via randomevents.WhirlpoolNearby).
//
// Live-found 2026-09-16 (lc2b_36, lc2b_67, same 200-account soak): fleeFromCombat/
// EmergencyFlee correctly walked/teleported the account to the Catherby bank every single
// time it fired — confirmed genuinely landing there ("in combat while waiting — fleeing
// immediately" followed by a real arrival at 2809,3441 each time) — but nothing downstream
// remembered a fight had just happened, so the fisher script's own resume logic immediately
// walked straight back toward the same fishing spot it had just been attacked at (e.g.
// "walk from 2809,3441 toward 2845,3431"). lc2b_67's own log showed this exact
// flee -> walk back -> flee cycle three times in about 15 seconds (06:57:44, 06:57:49,
// 06:57:55) before dying, each cycle taking a few hits before the flee registered. With
// eatFood off (the account owner's own explicit standing setting), there is no healing
// between rounds, so it is the cumulative chip damage across repeated re-engagements that
// actually kills the account, not any single fight. Architecturally the same shape as the
// Guardian.isRejected/stickyRejectedIndex fix already in this file for dialog-NPC
// rejection — this is that same "sticky exclusion window" idea, for combat.
//
// Lifted early once the attacker is gone (2026-09-19). The window and radius alone produced a
// large, measured XP loss: combatAvoidRadius (15) covers every fishing spot in Catherby's
// ~24-tile camp strip, so ANY fight blanked every spot for that account for the full two
// minutes. One 1h46m run logged 8,266 "fishing paused: all matching spots unsafe" lines across
// 114 accounts, every one of them "recent combat" and none whirlpool, with a median longest
// continuous pause of 140s (p90 178s) — the owner's "a lot of accounts will stand around"
// report. The rule exists to stop the account walking back INTO THE THING THAT HIT IT; once
// that thing has left view there is nothing left to walk back into. So when the attacker was
// identified at flee time (see identifyAttackers) and none of them is still visible, the spot
// is safe again immediately. When the attacker could NOT be identified, behaviour is exactly
// what it was — the change can only ever unblock sooner, and only on positive evidence.
func RecentlyAttackedNear(s actions.Session, x, z, level, radius int) bool {
	v, ok := recentCombat.Load(s)
	if !ok {
		return false
	}
	spot := v.(recentCombatSpot)
	if time.Now().After(spot.until) {
		return false
	}
	if level != spot.level || chebyshev(x, z, spot.x, spot.z) > radius {
		return false
	}
	if len(spot.attackers) > 0 && !attackerStillPresent(s.State(), spot.attackers) {
		return false
	}
	return true
}

// fleeFromCombat is the shared "get away from whatever's hitting me right now" action —
// the same bank-biased flee guardian.go's own general in-combat check uses, factored out
// so any long-running solve loop that can hold Guardian's inFlight for many seconds
// (solveStrangePlant, solveGenie, the Drunken Dwarf retry loop) can react to real combat
// damage from ANY source, not just the one specific NPC it happens to be tracking — see
// solveStrangePlant's own doc comment for the live deaths this closes. dist is typically
// baseFleeDist; callers that already track an escalating attempt count may pass
// fleeDistFor(attempt) instead.
//
// Uses an instant ::tele cheat rather than walkTo, local-engine-only (matches every other
// rescue-teleport already in this codebase — see fisherrunner.TeleTo). Live-found
// 2026-09-16 (a 200-account soak): raising baseFleeDist 12->20 and fixing a
// double-command race (see EmergencyFlee's own doc comment) did not stop deaths — traced
// live and found several accounts still dying within a second or two of their very first
// logged flee, at low Hitpoints levels, against genuinely dangerous "event monster"
// hostiles. A walked flee takes several ticks just to start moving, and the account can
// take lethal damage during that entire window regardless of the destination distance or
// direction — the fix isn't a bigger or better-aimed walk, it's removing the travel time
// itself. A real client has no such option; this is only safe/available because this
// whole soak runs against the local test engine, never production.
func fleeFromCombat(s actions.Session, pack *pathfind.Pack, dist int, log func(string)) {
	// Record where the fight actually happened (not the flee destination) before doing
	// anything else — see RecentlyAttackedNear's own doc comment for why this exists at
	// all. Every combat-flee call site in this file goes through this one function, so
	// this single line gives fisherrunner.FindSpot-style callers full coverage rather
	// than needing a copy at each site.
	if x, z, level, ok := s.State().Tile(); ok {
		noteCombatFlee(s, x, z, level)
	}
	// actions.CheatEnabled gates this — added 2026-09-16 right before this codebase's
	// first live server run. Everything below the fallback branch is local-test-engine-
	// only (see actions.CheatEnabled's own doc comment for why a bare, unprivileged
	// CLIENT_CHEAT packet is itself a bot-detection signal on production, independent of
	// whether the server honors it). fleeFromCombatWalking is the already-built,
	// unit-tested production-viable replacement (see its own doc comment) — built earlier
	// today specifically because the account owner flagged that every tele-based fix in
	// this file is a local-only shortcut and a real client needs to run from combat
	// properly instead. This is that plan finally taking effect, not a new decision.
	if !actions.CheatEnabled {
		if err := fleeFromCombatWalking(s, pack, dist, log); err != nil && log != nil {
			log("combat flee (walking): " + err.Error())
		}
		return
	}
	// Always teleport straight to the fixed Catherby bank reference point rather than a
	// nearby computed candidate — live-found 2026-09-16 (200-account soak, a "acct"
	// event): a ~20-40 tile biased-nearby destination can still land inside the affected
	// radius during a large simultaneous multi-account event, since many accounts compute
	// nearly-identical nearby destination tiles and several died shortly after
	// teleporting there anyway. The bank is always safe regardless of where the account
	// currently is, so there's no reason to compute or reachability-check a nearer tile.
	dest := safeRefFor(s)
	// Never teleport an account that is nowhere near its own safe reference. Live-found
	// 2026-09-18 (4/4 reproducible): this branch fired for a fresh Lumbridge account
	// pickpocketing at 3222,3218 and threw it ~400 tiles to the Catherby bank, where it
	// could not navigate back and ended STRANDED. "The bank is always safe regardless of
	// where the account currently is" (the comment above) is true about the DESTINATION
	// and false about the consequence: landing somewhere safe but unreachable-from is
	// worse than the fight being fled. Beyond this range, fall through to the walking
	// flee, which stays local by construction. farTeleportGuard deliberately matches the
	// drift threshold every EmergencyFlee caller already uses for "this account is not
	// where it should be".
	if x, z, level, ok := s.State().Tile(); ok &&
		(level != dest.Level || chebyshev(x, z, dest.X, dest.Z) > farTeleportGuard) {
		if log != nil {
			log(fmt.Sprintf("combat flee: %d,%d,%d is too far from the safe point %d,%d,%d to teleport there — walking instead",
				x, z, level, dest.X, dest.Z, dest.Level))
		}
		if err := fleeFromCombatWalking(s, pack, dist, log); err != nil && log != nil {
			log("combat flee (walking): " + err.Error())
		}
		return
	}
	cmd := fmt.Sprintf("tele %d,%d,%d,%d,%d", dest.Level, dest.X>>6, dest.Z>>6, dest.X&0x3f, dest.Z&0x3f)
	if err := actions.Cheat(s, cmd); err != nil {
		if log != nil {
			log("combat flee: teleport handler error: " + err.Error())
		}
	}
}

// EmergencyFlee is an independent, Guardian-bypassing combat check for a script's own
// outer wait loop (fisherrunner.WaitForGuardian and its barbarianrunner/crafterrunner/
// mule.go counterparts). Live-found 2026-09-16 (a 200-account soak, a mass hostile-
// transformation wave that killed a dozen-plus accounts in under two minutes despite
// v86's in-loop InCombat checks): those checks only run INSIDE a solve loop that
// Guardian has already started — an account whose event Guardian hasn't picked up yet
// (still within the ~600ms poll gap, or genuinely under attack from something Detect()
// never classified as an event at all) has zero combat protection for as long as the
// outer script loop just sleeps polling Busy(), which says nothing about incoming
// damage. Called on every poll tick of those loops regardless of Busy()/inFlight state
// — redundant with Guardian's own flee logic when Guardian does get there first
// (walking away twice is harmless), but closes the gap when it doesn't. Returns true if
// it took a flee action.
// driftLogThrottle rate-limits EmergencyFlee's own drift-check log lines per session —
// live-found 2026-09-16 (lc2b_123/lc2b_171, same 200-account soak, right after the
// lock-contention visibility line was added): the maze/mime event's own destination is
// always >150 tiles from Catherby by construction (it's a whole separate mapsquare), so
// every account legitimately solving one spams this line on every single 500ms poll for
// the entire multi-door traversal — thousands of near-duplicate lines per maze, enough to
// make the log-tail monitor itself start dropping events ("output rate too high"). This
// isn't a new bug the throttle is hiding: it's the same information logged once every few
// seconds instead of twice a second, which is all a human or another monitor ever needed
// from it.
var driftLogThrottle sync.Map // actions.Session -> time.Time of last log

func shouldLogDrift(s actions.Session) bool {
	now := time.Now()
	if last, ok := driftLogThrottle.Load(s); ok {
		if now.Sub(last.(time.Time)) < 5*time.Second {
			return false
		}
	}
	driftLogThrottle.Store(s, now)
	return true
}

// driftStationaryWindow is how long an account must sit at the exact same tile before the
// drift check below will actually act on it — see driftObservation's own doc comment for
// the live bug this closes. Deliberately longer than walkNearCore's own 2s per-segment
// stall threshold (internal/scripts/freshstartrunner/runner.go), so that function's own
// faster, targeted recovery (tryOpenDoor/tryClimbOver on the exact stalled segment) gets a
// real chance to resolve an ordinary stall before this much more drastic "give up and go to
// Catherby" action ever fires on top of it.
const driftStationaryWindow = 5 * time.Second

// driftObservation is the last tile EmergencyFlee's own drift check saw for a session, and
// since when it has sat there.
//
// Live-found 2026-09-16 (a 45-minute soak, right after walkNearCore/WalkNear's own inner
// loops started calling EmergencyFlee every poll to close an earlier combat-monitoring gap
// — see those functions' own doc comments): 12 of 20 fresh accounts, plus one already-
// established account, got PERMANENTLY stuck. Root cause: the drift check above judges
// purely by raw distance from CatherbyBankRef, with no notion of "is this account actively,
// legitimately walking a long way under its own supervision (SailLeg, travelViaFreshStart,
// a plain WalkNear covering real distance) right now" versus "is nothing moving this
// account at all." Both look identical by distance alone — the Ardougne-to-Catherby leg of
// an ordinary fresh-start journey is `>150` tiles from Catherby for most of its length, same
// as a genuine fail-teleport destination — but only the second one needs rescuing. Firing
// walkTo/Cheat unconditionally issued a SECOND, competing walk command on top of whatever
// walk was already in progress, and that competing command could route the account through
// a door into a pocket it could never get back out of — confirmed live, strictly worse than
// the stranding this check exists to prevent, the opposite of its own intent. Gating the
// actual recovery on "stationary for driftStationaryWindow" (below) instead of raw distance
// keeps the original lc2b_88/lc2b_54 case caught (nothing is walking a fail-teleported
// account anywhere, so it sits at one tile until this fires) while leaving an actively
// travelling account alone.
type driftObservation struct {
	x, z, level int
	since       time.Time
}

// driftStationary maps actions.Session -> driftObservation, the same unbounded-but-harmless
// per-session-map shape driftLogThrottle/recentCombat already use in this file.
var driftStationary sync.Map

// stationaryFor reports whether session has sat at exactly x,z,level for at least window,
// updating the tracked observation as a side effect on every call. A tile different from
// the last observed one resets the clock — any real movement at all, however slow, is
// progress, not stranding.
func stationaryFor(s actions.Session, x, z, level int, window time.Duration) bool {
	now := time.Now()
	if v, ok := driftStationary.Load(s); ok {
		obs := v.(driftObservation)
		if obs.x == x && obs.z == z && obs.level == level {
			return now.Sub(obs.since) >= window
		}
	}
	driftStationary.Store(s, driftObservation{x: x, z: z, level: level, since: now})
	return false
}

// driftReach is one cached answer to "can a walk get from this tile to this home".
type driftReach struct {
	x, z, level int
	home        pathfind.NavPoint
	// changes is how many live loc changes the answer was computed with: a door opening or
	// closing is a new question.
	changes int
	ok      bool
}

// driftReachable maps actions.Session -> driftReach, the same unbounded-but-harmless
// per-session-map shape driftLogThrottle/driftStationary already use in this file.
var driftReachable sync.Map

// driftCanWalkHome reports whether the pack has any walking route from the account's tile to
// home, memoised per session for as long as both stay the same.
//
// The memo is the point, not an optimisation detail. The drift check runs on every 500ms
// poll of every script's wait loop, and the answer that matters here is a FAILING A*, which
// is the expensive direction: a search that cannot reach its goal empties the open set
// before it gives up — measured at 21,951 expansions and 13ms for Brimhaven dock to
// Catherby, against 7ms for a Catherby-to-Ardougne search that succeeds. Asking that twice
// a second for every stranded account, forever, is precisely the shape of the /api/bank-all
// regression (see CLAUDE.md's panel-performance row). A stranded account sits on one tile by
// definition, so one answer per tile is all that is ever needed; any real movement
// invalidates it, which is also exactly when it should be re-asked.
func driftCanWalkHome(s actions.Session, pack *pathfind.Pack, x, z, level int, home pathfind.NavPoint) bool {
	changes := len(s.State().LocChanges())
	if v, ok := driftReachable.Load(s); ok {
		c := v.(driftReach)
		if c.x == x && c.z == z && c.level == level && c.home == home && c.changes == changes {
			return c.ok
		}
	}
	// The live door overlay, as the recovery walk itself uses (review of claude/handoff-misses).
	ok := fleePack(s, pack).FindPath(pathfind.NavPoint{X: x, Z: z, Level: level}, home).OK
	driftReachable.Store(s, driftReach{x: x, z: z, level: level, home: home, changes: changes, ok: ok})
	return ok
}

// EmergencyFlee is called from every script's own WaitForGuardian/walk-monitoring loop
// (fisherrunner, barbarianrunner, crafterrunner, mulerunner, freshstartrunner, donorrunner,
// internal/bot/mule.go) with that caller's own home/driftTiles: the "known-safe operating
// area" reference this specific script's accounts are normally near, and how far (in tiles,
// checked independently per axis, matching the historical Catherby-only check this
// generalizes) counts as suspicious drift for THAT script. driftTiles <= 0 disables the
// drift-check branch entirely — the InCombat branch below still runs regardless — for a
// caller with no single well-defined operating point of its own to check against.
//
// Live-found 2026-09-16: this used to hardcode CatherbyBankRef/150 unconditionally, which
// is exactly right for ProgressiveFisher (every account's real operating area) but a
// standing false positive for every other script sharing this same function — Barbarian's
// agility course sits ~2550-2560, 2000+ tiles from Catherby by way of the drift check's own
// 150-tile threshold; Seers' Village crafting is closer but still >150 away. Confirmed live
// via cmd/doordiag that the Seers/Barbarian bank tile (2725,3491,0) has a REAL walkable
// route toward Catherby (28 waypoints) — so this was never a harmless no-op for those
// scripts, it would genuinely drag a correctly-working crafter/barbarian account away from
// its own bank toward a reference point it has no reason to be near, exactly the kind of
// competing-walk stranding this whole drift check exists to prevent, not cause.
// Whether the InCombat branch below applies is NOT a property of the call site — it is a
// property of the ACCOUNT, and the account's own registered Guardian already knows it. So this
// resolves it through the same registry lookup safeRefFor/chatFor use rather than taking it as
// an argument, which is what lets one flag cover every caller without touching any signature.
//
// Live-found 2026-09-18 (Scarab Swarm KQ reconnaissance): Guardian.CombatExpected was honoured
// by guardian.go's own kick() and by the three runners that call EmergencyFleeCombatAware
// explicitly (fighterrunner, flinchrunner, and cmd/advancedfightertest), and by nothing else.
// Every other path calls plain EmergencyFlee — including freshstartrunner.WalkNear's two
// internal calls and every skilling runner's own WaitForGuardian loop — so an account whose
// script had correctly declared "my InCombat is expected" was still fled out of its own fight by
// the very next walk. Two concrete consequences on the tree as it stands:
//
//   - internal/bot/fisher.go sets CombatExpected dynamically (phase == PhaseFund && atSpot) and
//     then does its fund-phase walking through freshstartrunner, which ignored it. Half a fix.
//   - Driving freshstartrunner.WalkNear inside the Kalphite lair, the resident kalphites put the
//     account InCombat, this branch fired, localEscapeAvoiding found no reachable candidate in
//     an enclosed cave ("no reachable flee candidate found" a dozen times over), it gave up
//     "still threatened", and the account was walked clean out of the content and back up to the
//     desert surface with its quest items gone.
//
// A session with no registered Guardian resolves to false, i.e. exactly today's behaviour, so
// every caller that never sets the flag is byte-for-byte unchanged.
func combatExpectedFor(session actions.Session) bool {
	registryMu.Lock()
	g := registry[session]
	registryMu.Unlock()
	return g != nil && (g.CombatExpected || g.cornered())
}

func EmergencyFlee(s actions.Session, pack *pathfind.Pack, home pathfind.NavPoint, driftTiles int, log func(string)) bool {
	return emergencyFlee(s, pack, home, driftTiles, combatExpectedFor(s), log)
}

// EmergencyFleeCombatAware is EmergencyFlee for a script whose own InCombat is expected,
// ordinary operating state (AdvancedFighter/MeleeFlincher), not itself a threat signal —
// see Guardian.CombatExpected's own doc comment for the identical reasoning and the live
// evidence behind it (cmd/advancedfightertest, 2026-09-16: every fight ended within a
// couple of poll ticks because this function's own unconditional "InCombat -> flee" branch
// below has no way to tell a wanted fight from an unwanted one). combatExpected only
// disables that one branch; the drift-recovery branch above it (a fail-teleport punishment
// zone, or a walk that genuinely wandered off) still applies exactly as before — a fighter
// script drifting from its own anchor is still a real problem this should still catch.
//
// Since 2026-09-18 plain EmergencyFlee also honours a registered Guardian.CombatExpected (see
// combatExpectedFor), so for a script that sets that flag the two functions now agree. This one
// remains the explicit, unconditional form, for a caller that has no registered Guardian at all
// (a standalone test tool, say) or that wants the behaviour regardless of registry state — and
// fighterrunner/flinchrunner keep calling it precisely because saying so at the call site is
// clearer than depending on their bot having set the flag.
func EmergencyFleeCombatAware(s actions.Session, pack *pathfind.Pack, home pathfind.NavPoint, driftTiles int, log func(string)) bool {
	return emergencyFlee(s, pack, home, driftTiles, true, log)
}

func emergencyFlee(s actions.Session, pack *pathfind.Pack, home pathfind.NavPoint, driftTiles int, combatExpected bool, log func(string)) bool {
	// An account the strand ladder is working belongs to the ladder: every script's WaitForGuardian
	// calls this every 500ms while the ladder runs, and a flee from the Swarm on Rogues' Castle's roof
	// would walk the account off the stand tile the ladder is waiting on (in the dragon's lair running
	// about is no escape at all). See strand_exec.go.
	if strandHolds(s) {
		return false
	}
	// Drift check first, ahead of the InCombat gate below — live-found 2026-09-16
	// (lc2b_88, same 200-account soak, right after HandleHazard's own drift-polling fix):
	// InCombat-only protection here is reactive, not preventive — an account can be
	// punishment-teleported into the fixed far-away "ignored too long" zone by whatever
	// event Guardian is CURRENTLY dispatching elsewhere (this loop has no visibility into
	// that), and if the hostile content there lands a lethal hit before the account's own
	// InCombat flag has even had a chance to be observed on a 500ms poll, EmergencyFlee's
	// combat branch never gets a chance to run at all. Traced live: lc2b_88 was safely
	// teleported to the Catherby bank by this exact function at 04:33:40, then died at
	// 3221,3218 (the same fixed zone every other confirmed fail-teleport this session has
	// landed in) sometime in the next ~27 seconds while WaitForGuardian's own loop just
	// logged "still waiting on a random event" — meaning some OTHER event, dispatched
	// after this one cleared, fail-teleported the account again before its own handler's
	// end-of-wait checkFailTeleport call ever got a chance to run. Checking drift against
	// the caller's own home reference here, unconditionally, on every 500ms poll regardless
	// of combat state, catches that displacement the instant it happens instead of only
	// after (or never, if the encounter is lethal before InCombat is ever observed).
	if driftTiles > 0 {
		if x, z, level, ok := s.State().Tile(); ok {
			dx, dz := x-home.X, z-home.Z
			if dx < 0 {
				dx = -dx
			}
			if dz < 0 {
				dz = -dz
			}
			if level != home.Level || dx > driftTiles || dz > driftTiles {
				// Only act if genuinely stranded (stationary — see driftObservation's own doc
				// comment) or already in combat (urgent regardless of movement, and combat can
				// legitimately interrupt a supervised walk without that walk's own logic ever
				// noticing). stationaryFor is called unconditionally, not short-circuited behind
				// InCombat, so its own tracked observation stays fresh even through a fight.
				// This is the ONLY thing gated — checkFailTeleport/HandleHazard's own drift
				// checks compare a specific "before this click" tile rather than a fixed global
				// reference, so they were never exposed to this same false-positive-against-a-
				// legitimate-long-walk class of bug and need no equivalent change.
				stationary := stationaryFor(s, x, z, level, driftStationaryWindow)
				if !stationary && !s.State().InCombat() {
					return false
				}
				// ...and then only if a walk could actually get home from here. The
				// stationary gate above assumes the account is stuck BECAUSE nothing is
				// moving it; a fail-teleport onto another landmass breaks that assumption,
				// because there the account is stationary while its own script waits at a
				// gangplank, and no walk this function issues can ever succeed.
				//
				// Live 2026-09-22, production: eight ProgressiveFisher accounts sat at
				// 2780,3214 — "Brimhaven, northern dock", one of the engine's own catalogued
				// fail-teleport destinations (failteleport.go) and on Karamja, an island.
				// Each had already logged "phase: sailing to Catherby", so its own
				// across-water recovery (fisherrunner.acrossWaterFromCatherby, written for
				// this exact case after the mule was stranded on the same dock on
				// 2026-09-19) was under way. This check then took the lock every poll for
				// half an hour and issued a walk home that cannot exist, cancelling the sail
				// each time: 96 "recovering immediately" lines and 0 xp for the whole run,
				// on 8 accounts at once.
				//
				// So: when home is not walkable from here, stand down rather than compete.
				// The guardian has nothing to offer a stranding only a boat resolves, and
				// the scripts already know how to sail. Say so once, by name where the
				// location is catalogued, instead of the throttled generic line that made
				// this look like ordinary drift for half an hour.
				//
				// THIS CANNOT DISARM A RESCUE THAT USED TO WORK, and that is the whole
				// safety argument. The gate asks pack.FindPath exactly the question walkTo
				// (above, line ~162) already asks, and walkTo's own first act on a failing
				// search is `return nil` — it does nothing and reports SUCCESS. So on every
				// tile where this new gate stands down, the old code took the lock, walked
				// nowhere, and returned true; and on every tile where the old code actually
				// moved the account, FindPath answers OK and this gate passes straight
				// through. The only behaviour that changes is that the lock is no longer
				// taken from the script for a walk that was never going to happen.
				//
				// That silent `return nil` is also why this read as ordinary drift for half
				// an hour: the "drift recovery (walking): ..." error line below can never
				// fire for an impossible route, so the only trace was the throttled generic
				// line. Left as it is rather than widened here — it has many callers — but
				// it is the reason this needed a gate rather than better error handling.
				//
				// Not gated on cheats: with them enabled the recovery is a teleport, which
				// works from anywhere and is what the lab wants.
				if pack != nil && !actions.CheatEnabled && !driftCanWalkHome(s, pack, x, z, level, home) {
					NoteNoWayHome(s)
					if log != nil && shouldLogDrift(s) {
						where := DescribeStrandedLocation(x, z, level)
						if where == "" {
							where = "a landmass with no walking route home"
						}
						log(fmt.Sprintf("[guardian] far from home at %d,%d,%d (%s) and NO WALK REACHES HOME — "+
							"standing down so the script's own crossing can run", x, z, level, where))
					}
					return false
				}
				unlock, ok := TryLock(s)
				if !ok {
					// Ask box/lamp handling to step aside so the next poll gets the lock
					// (see requestDriftYield for the live starvation this ends).
					requestDriftYield(s)
					// Visibility only, not a behavior change — added 2026-09-16 (lc2b_22, same
					// 200-account soak, right after this drift check itself was added): a death
					// happened with a ~14s gap of zero log output beforehand, ending in this
					// check's own recovery line arriving well after the death was reported.
					// Before this log line existed there was no way to tell "the lock was held
					// by something else the whole time" (this branch) apart from "the drift
					// check has a logic bug and only started working 14s late" — this makes that
					// distinguishable on the next occurrence instead of guessing. Throttled via
					// shouldLogDrift — see its own doc comment for why (maze/mime legitimately
					// holds the lock and sits far from home for its entire multi-door
					// traversal, so this branch fires on every single poll for the whole event).
					if log != nil && shouldLogDrift(s) {
						log(fmt.Sprintf("[guardian] drifted far from home (now %d,%d,%d) but the action lock is held elsewhere — will retry", x, z, level))
					}
					return false
				}
				defer unlock()
				if log != nil && shouldLogDrift(s) {
					log(fmt.Sprintf("[guardian] drifted far from home while waiting (now %d,%d,%d) — recovering immediately", x, z, level))
				}
				// Not checkFailTeleport: that function compares a BEFORE tile against the
				// CURRENT one, and there is no "before" here — the drift was already detected
				// above from the caller's own home reference, so this recovers directly
				// instead of re-deriving a before/after pair that would show zero drift
				// against itself.
				if actions.CheatEnabled {
					cmd := fmt.Sprintf("tele %d,%d,%d,%d,%d", home.Level, home.X>>6, home.Z>>6, home.X&0x3f, home.Z&0x3f)
					if err := actions.Cheat(s, cmd); err != nil && log != nil {
						log("drift recovery: teleport handler error: " + err.Error())
					}
				} else if pack != nil {
					if err := walkTo(s, fleePack(s, pack), x, z, level, home); err != nil && log != nil {
						log("drift recovery (walking): " + err.Error())
					}
				}
				return true
			}
		}
	}
	if !s.State().InCombat() || combatExpected {
		return false
	}
	// TryLock, not Lock: live 200-account data (v87) showed this running unsynchronized
	// against Guardian's own kick() let both fire a walk for the same account within the
	// same second, repeatedly — kick()'s own in-progress combat-flee (held for up to 36s)
	// getting a second, possibly conflicting walk command stacked on top of it instead of
	// being left alone to finish. If Guardian already owns the action lock, skip this poll
	// entirely rather than race it.
	unlock, ok := TryLock(s)
	if !ok {
		return false
	}
	defer unlock()
	// Re-check after acquiring the lock: whatever Guardian's own action just finished may
	// have already resolved the combat while this call was waiting to acquire it.
	if !s.State().InCombat() {
		return false
	}
	if log != nil {
		log("[guardian] in combat while waiting — fleeing immediately")
	}
	fleeFromCombat(s, pack, baseFleeDist, log)
	return true
}

// HandleEvade ports handleEvade: flee well away from the specific threat NPC, wait for
// that exact index to disappear or stop threatening this account, then return.
//
// Deliberate deviation from the TS original, found live not guessed: RandomEvents.ts's
// own handleEvade always walks back to the original tile after the wait, whether or not
// the threat actually despawned — safe there only because it's paired with the same
// per-signature retry cap this package's own Guardian now also has (see guardian.go).
// Before that cap existed, walking straight back into a threat that hadn't actually left
// yet just re-triggered Detect on the very next tick from a worse position (right next to
// the threat again) — confirmed live in production tonight (see COLLAB.md): an "event
// monster" event took 2+ minutes and multiple failed attempts to clear, each ending in
// "still present after handling." This version only walks back to origin once the threat
// is confirmed gone; if it's still there when the wait times out, it stays put at the
// flee tile (farther from the threat than the origin was) and lets the next Guardian
// attempt escalate the flee distance instead of repeating the identical failed move.
func HandleEvade(s actions.Session, pack *pathfind.Pack, ev *Event, attempt int, log func(string)) error {
	x, z, level, ok := s.State().Tile()
	if !ok || ev.NpcIndex < 0 {
		return nil
	}
	origin := pathfind.NavPoint{X: x, Z: z, Level: level}
	if log != nil {
		log(evadeThreatDiag(s.State(), ev, x, z))
	}
	// Same bookkeeping fleeFromCombat now does on every flee (see RecentlyAttackedNear's
	// own doc comment) — this is a separate flee path (a tracked hostile "event monster"
	// NPC, not fleeFromCombat's generic "something is hitting me" case), but the same
	// re-engagement risk applies: origin is exactly the kind of "just got the account
	// chased off" spot a fishing-spot search could otherwise walk straight back to.
	// Pass the threat we are fleeing: it has not attacked yet, so it cannot be inferred from
	// facing, and without it the spot stays blocked for the full window after the monster is
	// long gone (see noteCombatFleeFrom).
	noteCombatFleeFrom(s, origin.X, origin.Z, origin.Level, ev.NpcIndex, ev.NpcType)
	// A leashed event monster is added where it is first seen: remember that tile, because the leash it is
	// shaken off by is measured from there, not from wherever it has followed the account to
	// (event_leash.go).
	if isLeashedEvent(ev.NpcType) {
		noteLeashSpawn(s, ev.NpcIndex, ev.NpcType, ev.ThreatX, ev.ThreatZ, level)
	}
	// PLANNED ON THE LIVE PACK, with this session's observed door and wall changes applied
	// (fleePack), not the static collision pack, which bakes every door shut. Live 2026-09-23
	// 15:15:35 (build f3017f87) the mule was cooking in the Catherby range room when a Swarm
	// spawned beside it. On the static pack that room is a sealed 13-tile component (its only
	// exit is the door at 2816,3438, baked shut), so no escape existed at any distance and the
	// guardian re-planned and failed about twice a second for two and a half minutes, serving
	// no trade -- through a door the mule had walked in by, open, twelve seconds earlier. The
	// walking flee below (fleeFromCombatWalking) already plans on the live pack; only this
	// "is there anywhere to go at all" check did not.
	distance := evadeDistance(attempt)
	live := fleePack(s, pack)
	// An event monster with a leash (the Swarm, the Strange plant, the Shade, the Zombie, the Golem, the
	// Watchman, the River troll, the Tree spirit) is left by walking out of its leash, the shortest way
	// along the ground the account can reach and clear of anything else that attacks (event_leash.go); the
	// ring search below is for every other event monster, and for these only when no reachable tile is
	// outside the leash.
	var dest pathfind.NavPoint
	var found bool
	var fellBack int
	leashNote := ""
	if res, ok := evadeLeash(s, live, origin, ev, attempt); ok {
		dest, found, leashNote = res.Dest, true, "; "+describeLeash(s, res)
	}
	// No tile on this floor is outside the leash (a tower's top room, a cellar): the engine drops a monster's
	// target when its target changes level, so the staircase or ladder the walker knows is the way out
	// (levelexit.go). Before the ring search, which cannot shake a leashed monster either, and long before the
	// cornered path, which attacked a Swarm for ten minutes at food 0 (17 Oct 2026, the Wizards' Tower).
	if !found && isLeashedEvent(ev.NpcType) && evadeLevelExit(s, live, origin, ev, attempt, log) {
		return nil
	}
	if !found {
		dest, found, fellBack = evadeEscape(live, origin, ev.ThreatX, ev.ThreatZ, attempt, safeRefFor(s))
	}
	// Behind shut doors: open the first one on the way out and plan again, as many times as it
	// takes (evade_door.go). Here, not on the guardian's next attempt: that attempt waits out the
	// retry window, and an account under attack meanwhile falls to kick()'s generic combat flee,
	// which cannot see past the doors either. Lab 2026-09-25, the Lumbridge kitchen, a Swarm
	// beside a 10-hitpoint account: returning after one door lost 20 s to "no reachable flee
	// candidate found" behind the castle's Large doors, and the account died at 23.6 s.
	from := origin
	for opened := 0; !found && opened < evadeMaxDoors; opened++ {
		if !evadeThroughDoor(s, live, from, ev, attempt, log) {
			break
		}
		if nx, nz, nl, ok := s.State().Tile(); ok {
			from = pathfind.NavPoint{X: nx, Z: nz, Level: nl}
		}
		live = fleePack(s, pack)
		dest, found, fellBack = evadeEscape(live, from, ev.ThreatX, ev.ThreatZ, attempt, safeRefFor(s))
	}
	if found && fellBack > 0 && log != nil {
		log(fmt.Sprintf("evade: no escape at the preferred distance from %d,%d,%d — falling back to %d", from.X, from.Z, from.Level, fellBack))
	}
	if !found && threatAttacking(s.State(), ev) && corneredStandDownBecause(s, ev, "evade: cornered", log) {
		// Nowhere to go and the threat is already hitting: a teleport when hurt enough to need one and able to
		// pay for it, else fight back (where that is worth anything and there is food to carry it) and let the
		// script eat (cornered.go) rather than stand still re-planning every half second.
		return nil
	}
	if !found {
		// Nowhere to go even on the live pack: a dead-end room whose door is shut. Re-planning
		// at once changes nothing, and the guardian retries this handler straight away, so
		// hold still a moment before reporting it -- returning at once if an attack starts,
		// because an attack is no longer this handler's to wait out. It is a real danger: the
		// live Swarm above stood diagonally beside the mule and never hit it (10/10), but in
		// the lab one spawned orthogonally beside a 10-hitpoint account on the same stand and
		// killed it in seventeen seconds (cmd/hofhandofftest -phase rangeswarm, control run).
		sleepWatchingCombat(s, evadeNoEscapeWait)
		return fmt.Errorf("evade: no safe reachable local escape from %d,%d,%d", x, z, level)
	}
	// Log the planned separation; the monitored walker logs actual redirects.
	if log != nil {
		log(fmt.Sprintf("evade: local escape to %d,%d,%d from %d,%d (attempt %d)%s", dest.X, dest.Z, dest.Level, x, z, attempt, leashNote))
	}
	// Teleporting is allowed only in explicit lab setup. Production uses the
	// monitored walker, which reacts during travel and enables available running.
	if actions.CheatEnabled {
		cmd := fmt.Sprintf("tele %d,%d,%d,%d,%d", dest.Level, dest.X>>6, dest.Z>>6, dest.X&0x3f, dest.Z&0x3f)
		if err := actions.Cheat(s, cmd); err != nil {
			return err
		}
	} else if pack != nil {
		// Monitor separation and spend available run energy during the initial
		// escape, rather than leaving combat unchecked through a blind walk.
		stuck, err := fleeWalkingFrom(s, pack, distance, attempt, log)
		if err != nil {
			return err
		}
		if stuck {
			// The walker planned for itself and found nowhere to go, though the plan above did: the two
			// disagreed, and the account has stood on one tile for the whole walk. Hand it to the cornered
			// path (cornered.go) rather than hold the action lock, and with it the script that would eat or
			// break off, for another 45 s wait here and another 20 s walk after it. Live 16-17 Oct 2026, three
			// deaths: ten minutes, three and a half, and 1.5 minutes of exactly that.
			if corneredStandDown(s, ev, log) {
				return nil
			}
		}
	}

	deadline := time.Now().Add(45 * time.Second)
	gone := false
	var heldSince, lastHold time.Time // heldSince: when the monster was first found still on an account in combat
	for time.Now().Before(deadline) {
		inCombat, expected := s.State().InCombat(), combatExpectedFor(s)
		// Not for an account whose combat is expected: its warriors keep hitting it, and the generic flee from them
		// (a ring 20 tiles off) is the opposite of what a trainer wants once the event monster is out of its way.
		if inCombat && !expected {
			fleeFromCombat(s, pack, baseFleeDist, log)
			return nil
		}
		gone = true
		for _, npc := range s.State().NPCs() {
			if npc.Index == ev.NpcIndex {
				cx, cz, _, _ := s.State().Tile()
				gone = !threatensAccount(npc, s.State(), cx, cz)
				break
			}
		}
		if gone {
			break
		}
		if yieldSeparatedEvent(s, ev, log) {
			return nil
		}
		if inCombat {
			// Combat is expected and the monster is still on the account, so the walk did not shake it. Idling
			// here holds the lock the script needs to eat, and the generic flee is skipped: the handler eats,
			// teleports and hands the lock back itself (evade_wait.go).
			if heldSince.IsZero() {
				heldSince = time.Now()
			}
			if evadeHeld(s, ev, attempt, heldSince, &lastHold, log) {
				return nil
			}
		} else {
			heldSince = time.Time{}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !gone {
		// Still there after the wait — stay at the (farther-away) flee tile rather than
		// walking straight back into it; see the doc comment above for why.
		return nil
	}

	nx, nz, nl, ok := s.State().Tile()
	if !ok {
		return nil
	}
	// The live pack again: the way back to a spot behind a door is only open through it.
	if err := walkTo(s, fleePack(s, pack), nx, nz, nl, origin); err != nil {
		return err
	}
	// Drift check after the walk-back, not just the tele — live-found 2026-09-16
	// (lc2b_103, 200-account soak): this was the one solver in this file with no
	// checkFailTeleport safety net at all. walkTo's own blind fixed-duration sleep
	// (see its doc comment) never verifies the walk actually reached `origin`; when
	// it silently stalls or gets routed somewhere wrong, the account is left far from
	// Catherby with nothing noticing. The NEXT random event's own solver then
	// captures its "before" position already at that wrong spot, sees no further
	// drift on its own click, and logs "cleared" as if nothing happened — exactly
	// what masked this for lc2b_103 (traced: event monster evade cleared, then a
	// Mysterious Old Man "no dialogue ever opened" a few seconds later showed zero
	// drift of its own, because the account was already 260+ tiles from Catherby
	// before that click ever happened). Comparing against `origin` here, right after
	// the walk-back that's supposed to land there, closes the gap at its source.
	checkFailTeleport(s, pack, "event monster evade", origin.X, origin.Z, origin.Level, true, log)
	return nil
}

// evadeDistance is the separation HandleEvade asks for on its attempt'th try: 20 tiles, growing
// by two an attempt up to 32.
func evadeDistance(attempt int) int {
	if attempt > 1 {
		return min(32, 20+2*attempt)
	}
	return 20
}

// evadeNoEscapeWait is how long HandleEvade holds still before reporting that nowhere is
// reachable, so the guardian's immediate retry is not a re-plan several times a second.
const evadeNoEscapeWait = 3 * time.Second

// evadeEscape is HandleEvade's choice of where to go: a reachable local destination at least 15
// tiles from both the account and the threat (localEscape), the separation growing on repeated
// attempts, and when that preferred distance is unreachable, the shorter ladder below. fellBack
// is the shorter distance used, 0 when the preferred one worked.
//
// FALL BACK TO A SHORTER ESCAPE RATHER THAN STANDING STILL. This killed the live fleet's mule on
// 2026-09-21, and the account it killed holds the whole fleet's banked economy. An "event
// monster" spawned one tile away while the mule was cooking at the Catherby range, at 2817,3443
// -- a spot with no tile satisfying the preferred separation. The handler returned an error, the
// Guardian retried, the search failed identically, and that repeated about thirteen times a
// second until a level-3 account with 10 hitpoints was dead. It never moved a single tile.
//
// The escalation was also pointed the wrong way for this case: on repeated attempts it RAISES the
// required distance to 32, so each retry demands something harder while the account is being hit.
// That is right when a flee succeeded but did not separate enough, and exactly wrong when nothing
// was reachable at all. So when the preferred distance is unreachable, ask for less; the ladder
// stops at 4, below which "escape" is not meaningfully different from where it already is.
//
// pack must already carry the live door overlay (fleePack): see HandleEvade.
func evadeEscape(pack *pathfind.Pack, origin pathfind.NavPoint, threatX, threatZ, attempt int, toward pathfind.NavPoint) (dest pathfind.NavPoint, found bool, fellBack int) {
	// One planner for the whole ladder: it floods the reachable ground once and every rung shares
	// that flood and one route-search budget (flee_plan.go).
	plan, fellBack, found := newFleePlanner(pack, origin, threatX, threatZ, nil, toward).ladder(evadeDistance(attempt))
	if !found {
		return pathfind.NavPoint{}, false, 0
	}
	return plan.Dest, true, fellBack
}

func localEscape(pack *pathfind.Pack, origin pathfind.NavPoint, threatX, threatZ, distance int, toward pathfind.NavPoint) (pathfind.NavPoint, bool) {
	return localEscapeAvoiding(pack, origin, threatX, threatZ, distance, nil, toward)
}

// localEscapeAvoiding is the flee choice for an account at origin: the landing tile of the first ring
// candidate that can be walked to, is clear of the threat and the dangerous ground, and stays out of
// wolf territory. The rules, and why each exists, are on fleePlanner.escape (flee_plan.go), which is
// bounded: one flood of the reachable ground and a capped number of route searches, however many
// candidates are unreachable.
func localEscapeAvoiding(pack *pathfind.Pack, origin pathfind.NavPoint, threatX, threatZ, distance int, avoid func(int, int, int) bool, toward pathfind.NavPoint) (pathfind.NavPoint, bool) {
	plan, ok := newFleePlanner(pack, origin, threatX, threatZ, avoid, toward).escape(distance)
	return plan.Dest, ok
}

// LocalEscapeForLab exposes the real selection to the loopback regression harness.
func LocalEscapeForLab(pack *pathfind.Pack, origin pathfind.NavPoint, threatX, threatZ int) (pathfind.NavPoint, bool) {
	return localEscape(pack, origin, threatX, threatZ, 20, CatherbyBankRef)
}

// dialogNavigateDeadline bounds how long HandleDialogNpc/HandleBoxLamp will keep
// clicking through an open chat modal before giving up and letting the caller
// re-Detect — a reconstructed number (see detect.go's package doc comment), not a
// ported one; picked generously since most of these dialogues are short.
const dialogNavigateDeadline = 20 * time.Second

// navigateDialog repeatedly presses "continue" where available, otherwise the first
// listed option, until the chat modal closes or the deadline passes. This is
// deliberately not a solver for any specific NPC/item's dialogue tree — see
// HandleDialogNpc's doc comment for why "click through" is the whole strategy.
//
// Returns whether the dialogue was ever actually seen open, so a caller (HandleDialogNpc)
// can tell "clicked Talk-to, dialogue opened and was navigated" apart from "clicked
// Talk-to, no dialogue ever appeared" — the latter is a real, diagnosable failure mode
// (a missed click, an NPC that walked off, wrong option number) rather than success,
// and previously looked identical from the caller's own perspective (nil error either
// way). Added 2026-09-15 alongside HandleDialogNpc's own settle-delay fix — see its doc
// comment — specifically to make the next live run's logs distinguish the two cases
// instead of guessing from timing alone, the way this session's own COLLAB.md writeup
// on the "5a" race condition had to.
func navigateDialog(s actions.Session, pack *pathfind.Pack, components archive.Components, log func(string)) bool {
	opened, _ := navigateEventDialog(s, pack, nil, components, log)
	return opened
}

func navigateEventDialog(s actions.Session, pack *pathfind.Pack, ev *Event, components archive.Components, log func(string)) (opened, interrupted bool) {
	chat := actions.NewChatDialog(s, components)
	deadline := time.Now().Add(dialogNavigateDeadline)
	lastProgress := time.Now()
	lastDialog := ""
	sawOpen := false
	for time.Now().Before(deadline) {
		if !chat.IsOpen() {
			return sawOpen, false
		}
		sawOpen = true
		main, chatModal, side := s.State().Modals()
		dialog := fmt.Sprintf("%d/%d/%d:%v:%v", main, chatModal, side, chat.Texts(), chat.Options())
		if dialog != lastDialog {
			lastDialog = dialog
			lastProgress = time.Now()
		}
		if ev != nil {
			if matchesAnyPhrase(strings.Join(chat.Texts(), " "), dialogRejectionPhrases) {
				_ = rejectDialogNPC(s, ev, log)
				return sawOpen, true
			}
			if waitEventReply(s, pack, ev, 0, nil, log) {
				return sawOpen, true
			}
		}
		// Combat check — found during a repo-wide audit following tonight's incident
		// response: this loop can run for up to dialogNavigateDeadline (20s) clicking
		// through a dialogue with nothing checking combat the whole time, the same
		// blind-wait shape already fixed today for every other multi-second retry loop
		// in this file (solveStrangePlant/solveGenie/HandleHazard/HandleDialogNpc's own
		// Drunken Dwarf retry). Flees and returns immediately rather than continuing to
		// click through a dialogue while something is actively hitting the account.
		if s.State().InCombat() && abortForCombat(s, pack, ev, "while navigating a dialogue", log) {
			return sawOpen, true
		}
		if chat.CanContinue() {
			_ = chat.Continue()
		} else if opts := chat.Options(); len(opts) > 0 {
			_ = chat.ChooseOption(eventDialogChoice(opts))
		}
		if ev != nil && time.Since(lastProgress) >= 2*time.Second {
			_ = actions.CloseModal(s)
			deferEvent(s, ev, 2*pollInterval)
			if log != nil {
				log("dialogue stopped progressing — checking other events")
			}
			return sawOpen, true
		}
		if ev != nil {
			if waitEventReply(s, pack, ev, 600*time.Millisecond, nil, log) {
				return sawOpen, true
			}
		} else {
			time.Sleep(600 * time.Millisecond)
		}
	}
	if ev != nil {
		_ = actions.CloseModal(s)
		deferEvent(s, ev, 2*pollInterval)
		return sawOpen, true
	}
	return sawOpen, false
}

// eventDialogChoice is the option navigateEventDialog picks: the first, except on a jewellery teleport
// menu, where it is "Nowhere.". The run staller's rubs open that menu ("Where would you like to teleport
// to?" with "Burthorpe Games Rooms." first), and live 7 Oct 2026 a Drunken Dwarf's handler clicked the
// first option through it and sent Communicate to the Games Room at 2211,4936, which content gives no
// stairs out of.
func eventDialogChoice(opts []actions.Option) string {
	for _, o := range opts {
		if strings.EqualFold(strings.TrimSpace(o.Text), "Nowhere.") {
			return "Nowhere."
		}
	}
	return ""
}

// talkToOption resolves the "Talk-to" option number for a dialog-NPC event via the
// registered NameResolver (see names.go), falling back to option 1 — "Talk-to" is
// virtually always an NPC's first option for these friendly event NPCs, and this keeps
// the handler working even before/without SetNameResolver being wired to full op data.
func talkToOption(ev *Event) int {
	res := currentResolver()
	if res == nil {
		return 1
	}
	info, ok := res.Npc(ev.NpcType)
	if !ok {
		return 1
	}
	if idx := cachecfg.OpIndex(info.Ops, "Talk-to"); idx > 0 {
		return idx
	}
	return 1
}

// sleepWatchingCombat sleeps up to dur, checking s.State().InCombat() every chunk instead
// of blocking blind for the whole duration — returns true the moment combat is detected
// (having slept less than dur), false once the full duration elapses with no combat.
// Live-found 2026-09-16: a hostile-transformed Strange Plant (macro_triffidseed_angry,
// content-config vislevel=hide -- see npcHasGoneHostile's own doc comment for why the
// type-lookup check can never catch this specific transformation) can deal real, fast
// damage, and solveStrangePlant's own retry loop previously only checked InCombat once
// per iteration with up to ~2s (or a full unchecked 4s on the "not ready yet" branch)
// between checks -- real deaths traced to exactly this gap. Chunking every 300ms doesn't
// close a same-tick lethal hit, but it meaningfully shrinks the worst-case reaction delay
// from seconds down to a few hundred milliseconds for every other case.
func sleepWatchingCombat(s actions.Session, dur time.Duration) bool {
	const chunk = 300 * time.Millisecond
	deadline := time.Now().Add(dur)
	for time.Now().Before(deadline) {
		if s.State().InCombat() {
			return true
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if remaining < chunk {
			time.Sleep(remaining)
		} else {
			time.Sleep(chunk)
		}
	}
	return s.State().InCombat()
}

// sleepWatchingCombatOrDrift is sleepWatchingCombat plus a per-chunk checkFailTeleport-
// style position check, for callers whose wait is long enough that a real "ignored too
// long" fail-teleport can land AND get walked away from by the account's own follow-up
// logic before the caller's own single end-of-wait check ever runs.
//
// Live-found 2026-09-16 (lc2b_50/lc2b_53/lc2b_133, same 200-account soak, right after the
// HandleEvade drift gap was closed): HandleHazard's own 36-second wait only ever checked
// for drift once, at the very end — fine when nothing else happens in between, but a
// whirlpool's own "ignored too long" fail-teleport can land partway through that 36s, and
// by the time the wait finishes and Detect() runs again, the account's own fishing loop
// has already noticed "no matching fishing spot" and moved on into its own STRANDED/
// freshstartrunner recovery path — which has no combat protection at all and is what
// actually killed these three, not the fail-teleport itself. Checking every 300ms instead
// of once at 36s turns a many-second window of being at an unrecognized, unprotected
// location into the same sub-second recovery every other handler already gets.
//
// Returns (combat, driftedAwayFromDest) — combat takes priority when both are true in the
// same chunk (checked first below), matching every other combat-vs-hazard priority choice
// already made in this file.
func sleepWatchingCombatOrDrift(s actions.Session, dur time.Duration, destX, destZ, destLevel int) (combat bool, drifted bool) {
	const chunk = 300 * time.Millisecond
	const driftTiles = 100
	deadline := time.Now().Add(dur)
	check := func() (bool, bool) {
		if s.State().InCombat() {
			return true, false
		}
		x, z, level, ok := s.State().Tile()
		if !ok {
			return false, false
		}
		dx, dz := x-destX, z-destZ
		if dx < 0 {
			dx = -dx
		}
		if dz < 0 {
			dz = -dz
		}
		return false, level != destLevel || dx > driftTiles || dz > driftTiles
	}
	for time.Now().Before(deadline) {
		if c, d := check(); c || d {
			return c, d
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if remaining < chunk {
			time.Sleep(remaining)
		} else {
			time.Sleep(chunk)
		}
	}
	return check()
}

// checkFailTeleport compares the account's current tile against (preX,preZ,preLevel) —
// captured earlier via s.State().Tile(), right before whatever click this is guarding —
// and, if it looks like the account got silently relocated (the Genie/Mysterious Old
// Man/Drunken Dwarf/Strange Plant "ignored too long" fail-teleport mechanic; see
// dialogRejectionPhrases' own doc comment and this session's live Wizards' Tower
// coordinate finds), recovers immediately with the same local-engine-only ::tele
// mechanism used everywhere else in this codebase, logs it, and returns true.
//
// The distance threshold is deliberately generous (100 tiles) — live-found 2026-09-16 (a
// 200-account soak): an earlier, tighter 15-tile threshold mass-misfired on perfectly
// normal interactions, and even reports of an account supposedly still standing right at
// the fishing camp turned up. Every fail-teleport actually confirmed this session (by
// checking the account's real, respawn-independent position) landed thousands of tiles
// away (Wizards' Tower) or in a different mapsquare entirely (maze/mime) — there is no
// confirmed case of a real fail-teleport landing only a few dozen tiles off, so a
// generous threshold avoids the false-positive class without giving up real detection.
// preTileOK false (state not known yet) always skips the check.
func checkFailTeleport(s actions.Session, pack *pathfind.Pack, npcName string, preX, preZ, preLevel int, preTileOK bool, log func(string)) bool {
	if !preTileOK {
		return false
	}
	x, z, level, ok := s.State().Tile()
	if !ok {
		return false
	}
	dx, dz := x-preX, z-preZ
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	if level == preLevel && dx <= 100 && dz <= 100 {
		return false
	}
	home, why := failTeleportHome(pack, preX, preZ, preLevel, safeRefFor(s))
	if log != nil {
		log(fmt.Sprintf("%s appears to have fail-teleported the account — now at %d,%d,%d, recovering to %d,%d,%d (%s)",
			npcName, x, z, level, home.X, home.Z, home.Level, why))
	}
	// actions.CheatEnabled gates this the same way fleeFromCombat/EmergencyFlee already do
	// — see their own doc comments. walkTo is the production-safe fallback so a real
	// fail-teleport still gets an actual recovery attempt against example.invalid instead of
	// silently no-opping (Cheat already fails safe and logs on its own when disabled).
	if actions.CheatEnabled {
		cmd := fmt.Sprintf("tele %d,%d,%d,%d,%d", home.Level, home.X>>6, home.Z>>6, home.X&0x3f, home.Z&0x3f)
		if err := actions.Cheat(s, cmd); err != nil && log != nil {
			log("fail-teleport recovery: teleport handler error: " + err.Error())
		}
	} else if pack != nil {
		// On the live door overlay (fleePack), as HandleEvade now plans: on the static pack a
		// room behind a door, such as the Catherby range room, has no way out, and the walk
		// home found no path and quietly did nothing (review of claude/handoff0923-r4: the
		// Strange plant solver's recovery walk was the same trap as the range-room Swarm).
		if err := walkTo(s, fleePack(s, pack), x, z, level, home); err != nil && log != nil {
			log("fail-teleport recovery (walking): " + err.Error())
		} else if log != nil {
			log(fmt.Sprintf("fail-teleport recovery: walking to %d,%d,%d", home.X, home.Z, home.Level))
		}
	}
	return true
}

// failTeleportHome chooses where to put an account back after a fail-teleport.
//
// The destination was a hardcoded CatherbyBankRef until 2026-09-19, which is right for the
// ProgressiveFisher accounts this package was first written for and wrong for every other
// script — the same assumption that once dragged a fresh Lumbridge account 400 tiles across the
// map and stranded it there (see Guardian.SafeRef's own doc comment). Genie and Mysterious Old
// Man queue a fail-teleport on their own ignore timer with no detection needed from this client
// at all, so this fires for accounts that are not fishing and were never asked about it.
//
// The account's own tile from immediately before the jump is the per-account correct answer by
// construction: it is where that account was working a moment ago, whatever script it runs. The
// script's own SafeRef is the fallback for the two cases where that tile cannot be used — no
// reading at all, or a tile the collision pack says nothing can stand on, which would leave a
// walk with nowhere to aim.
func failTeleportHome(pack *pathfind.Pack, preX, preZ, preLevel int, ref pathfind.NavPoint) (pathfind.NavPoint, string) {
	pre := pathfind.NavPoint{X: preX, Z: preZ, Level: preLevel}
	if pre.X <= 0 || pre.Z <= 0 {
		return ref, "no usable tile from before the jump, so this account's own script home"
	}
	if pack != nil && !pack.Walkable(pre.X, pre.Z, pre.Level) {
		return ref, "nothing can stand on the tile from before the jump, so this account's own script home"
	}
	return pre, "the tile the account was standing on before the jump"
}

// npcHasGoneHostile re-reads ev.NpcIndex's CURRENT live type (not the type Detect first
// saw it as) and reports whether that type's own ops now include "Attack" — the real,
// live signal that content/scripts/macro events/scripts/general/macro_event_triffid.rs2's
// npc_changetype_keepall+opplayer2 switch (see hostileCapableNames' own doc comment) has
// already fired for this specific NPC instance. Confirmed live 2026-09-15 (a 200-account
// Catherby soak): a "Strange plant" NPC visible via a live ::tele+scan had type 408 with
// ops=[Attack], distinct from the ordinary passive "Strange plant" (type 407, ops=[Pick])
// — same display name, different type id, exactly the mechanism this checks for.
func npcHasGoneHostile(s actions.Session, npcIndex int) bool {
	res := currentResolver()
	if res == nil {
		return false
	}
	for _, n := range s.State().NPCs() {
		if n.Index != npcIndex {
			continue
		}
		info, ok := res.Npc(n.Type)
		return ok && cachecfg.OpIndex(info.Ops, "Attack") != 0
	}
	return false
}

// npcStillPresent reports whether npcIndex currently appears anywhere in the account's own
// live NPCs() list at all — see solveStrangePlant's own call site for the real gap this
// closes: a plant's content script (npc_del on a genuine pick) can remove it entirely, and
// a click against an index that no longer refers to anything produces no server response
// whatsoever, indistinguishable from silence otherwise.
func npcStillPresent(s actions.Session, npcIndex int) bool {
	for _, n := range s.State().NPCs() {
		if n.Index == npcIndex {
			return true
		}
	}
	return false
}

// HandleDialogNpc ports the shape of RandomEvents.ts's "dialog"/"pick" handling
// (Genie, Mysterious Old Man, Sandwich Lady, Frog, Drunken Dwarf, Strange Plant):
// approach the NPC, talk to it, then click through whatever dialogue appears. This is
// deliberately not a solver for any one NPC's actual reward-bearing dialogue tree (the
// real minigame behind several of these is gated behind a specific option or item) —
// per COLLAB.md's task scoping, the bar here is "don't get stuck", the same bar
// already accepted for this package's numeric-id handlers. A script's own loop should
// re-check its own state after this returns rather than assume anything was "solved".
//
// Settle delay added 2026-09-15: live production data (32-account fleet, ~2 hours)
// showed Drunken Dwarf/Strange Plant/Mysterious Old Man — all three dispatched through
// this same function — failing Guardian's own 4-attempt retry cap 30/25/11 times
// respectively (Genie, the fourth NPC on this path, only once), the single largest
// contributor to random-event unreliability found this session, bigger than the
// box/lamp opcode bugs fixed earlier. Root cause: the click-then-check sequence had a
// zero-delay race — actions.OpNpc's click and navigateDialog's very first
// chat.IsOpen() check happened back to back with nothing in between, so if the
// dialogue interface took even a moment to open server-side after the click (a real
// possibility, not a corner case — every OTHER handler fixed this same session needed
// an identical settle delay after its own click: HandleMime, solveStrangeBox, handleLamp,
// tryChangeLevel, tryOpenDoor), navigateDialog's first check could see "not open yet"
// and return immediately, indistinguishable from "dialogue already closed" — burning a
// whole Guardian attempt for nothing every time the race lost. Not confirmed as the
// full explanation (no per-step logging existed to prove it before this fix — see log
// below, added at the same time so the NEXT live run's data settles the question
// instead of guessing again), but a well-supported, low-risk fix matching an established
// pattern, not a leap.
func HandleDialogNpc(s actions.Session, pack *pathfind.Pack, ev *Event, log func(string)) error {
	if ev.NpcIndex < 0 {
		return nil
	}
	if targetsAnotherAccount(s, ev) {
		return rejectDialogNPC(s, ev, log)
	}
	// Check for the hostile-transformed type BEFORE ever walking toward or clicking this
	// NPC — see npcHasGoneHostile's own doc comment for the real live mechanism this
	// catches. Only meaningful for the two names already known to carry this risk
	// (hostileCapableNames); the extra live-NPC scan costs nothing for the other two
	// (Genie, Mysterious Old Man), which don't have a hostile variant to check for.
	if hostileCapableNames[ev.Name] && npcHasGoneHostile(s, ev.NpcIndex) {
		if log != nil {
			log(fmt.Sprintf("%s has gone hostile — fleeing instead of approaching", ev.Name))
		}
		return HandleEvade(s, pack, ev, 1, log)
	}
	// A Strange plant that is too old to be walked up to, or that the account has already left, is not approached: the walk to it is
	// the walk into its leash. solveStrangePlant decides (plantleash.go).
	if strings.EqualFold(ev.Name, "Strange Plant") && plantNotToBeApproached(s, ev) {
		if chat := chatFor(s); chat != nil {
			return solveStrangePlant(s, pack, ev, chat, log)
		}
	}
	if x, z, level, ok := s.State().Tile(); ok && pack != nil {
		if log != nil {
			log(fmt.Sprintf("walking to %d,%d", ev.ThreatX, ev.ThreatZ))
		}
		interrupted, err := approachDialogNPC(s, pack, ev, x, z, level, log)
		if err != nil {
			return err
		}
		if interrupted {
			return nil
		}
	}
	// Re-check right before the click too — HandleDialogNpc's own walkTo above can take
	// several seconds on a longer approach, real time in which the same transformation
	// this function opened by checking for could still fire.
	if targetsAnotherAccount(s, ev) {
		return rejectDialogNPC(s, ev, log)
	}
	if hostileCapableNames[ev.Name] && npcHasGoneHostile(s, ev.NpcIndex) {
		if log != nil {
			log(fmt.Sprintf("%s went hostile while approaching — fleeing instead of talking", ev.Name))
		}
		return HandleEvade(s, pack, ev, 1, log)
	}
	// Genie specifically gets its own solve path for the same reason Strange Plant does
	// (see that case's own doc comment just below) plus one more: Detect's own dialog-NPC
	// scan is checked BEFORE the box/lamp inventory scan (detectBoxLamp, at the very
	// bottom of Detect's own function body) and returns on the first match, so a lamp
	// already sitting in the pack from an earlier successful Genie talk can be starved
	// indefinitely by any OTHER dialog NPC (Strange Plant chief among them, given how
	// dense this session's own soak got) matching first on every subsequent poll — live-
	// confirmed 2026-09-15/16: dozens of "Genie — cleared" lines across the whole 200-
	// account fleet, zero "Lamp" lines anywhere, the account owner's own sharp catch, not
	// a guess. Chaining handleLamp directly onto a confirmed-real Genie success, inside
	// this same call, sidesteps Detect's own priority order entirely rather than trying to
	// reorder it (which risks deprioritizing a hazard/hostile a real dialog NPC's own nag
	// timer needs promptly). Only takes this path when the interface archive is
	// registered (SetInterfaceArchive) — the same requirement navigateDialog's own
	// generic flow below already has, since genie's response text lives on the chat
	// modal's own component tree, not the plain chat log (see solveGenie's own doc
	// comment for the live-caught bug this fixes: an earlier version of this read
	// chat.Since(...) instead, which NEVER once saw the real text across two live trials,
	// because chatnpc() renders to an interface component, not a MESSAGE_GAME line).
	if strings.EqualFold(ev.Name, "Genie") {
		if components := currentInterfaceArchive(); components != nil {
			return solveGenie(s, pack, ev, components, log)
		}
	}
	// Strange Plant specifically gets its own confirm-before-leaving path rather than the
	// generic "no dialogue = done" assumption below. Real content script confirmed
	// (content/scripts/macro events/scripts/general/macro_event_triffid.rs2):
	// "The fruit isn't ready to be picked yet..." is a genuine rejection, not success —
	// the fruit is still growing and needs picking again later. "It's not here for you."
	// is the ownership-mismatch case (a different account's own plant). "You pick the
	// fruit from the plant." is the only real success line. Live-found this session (a
	// 200-account soak, a large repeated wave of deaths): the generic "no dialogue opened
	// = resolved" assumption below treats ALL THREE of these identically and moves on,
	// which for the "not ready yet" case abandons a plant that's still very much alive
	// and growing completely unattended — nothing else in this codebase ever comes back
	// to check on it, so it grows all the way to its own hostile-transformation stage
	// with zero supervision. Only takes this path when a chat log is actually available
	// (chatFor, guardian.go) — falls through to the existing generic handling otherwise,
	// exactly matching today's behavior for any caller that hasn't wired one up.
	if strings.EqualFold(ev.Name, "Strange Plant") {
		if chat := chatFor(s); chat != nil {
			return solveStrangePlant(s, pack, ev, chat, log)
		}
	}
	// Captured right before the click, not up at function entry — a first cut of this
	// fail-teleport check (below) captured it before the approach walkTo above, and
	// immediately mass-misfired live (2026-09-16): that walk alone can legitimately cover
	// many tiles closing on the NPC, which the check then wrongly read as the NPC having
	// fail-teleported the account, sending dozens of accounts on a needless round trip to
	// the bank for doing nothing wrong at all. Capturing here means only the click itself
	// (which never moves the account more than a tile or two on its own) is being
	// measured against.
	preX, preZ, preLevel, preTileOK := s.State().Tile()
	if err := actions.CloseModal(s); err != nil {
		return err
	}
	if err := actions.OpNpc(s, talkToOption(ev), ev.NpcIndex); err != nil {
		return err
	}
	components := currentInterfaceArchive()
	if waitEventReply(s, pack, ev, 2*time.Second, func() bool {
		return components != nil && len(actions.NewChatDialog(s, components).Texts()) > 0
	}, log) {
		return nil
	}

	if components == nil {
		// No interface archive registered (see SetInterfaceArchive) — no reliable way
		// to find the continue/option component ids, so just wait out a rough
		// dialogue duration and let the caller re-Detect.
		deferEvent(s, ev, 2*pollInterval)
		return nil
	}

	// Generic ownership-rejection check for the remaining dialog NPCs (Mysterious Old
	// Man, Sandwich Lady, Frog, Drunken Dwarf) — Genie and Strange Plant get their own
	// dedicated solve path above. Each NPC's rejection line is voiced differently —
	// confirmed directly against content, not assumed the same across all of them after
	// one only turned out right for two: Mysterious Old Man (macro_event_mysterious_
	// old_man.rs2) uses the same "...trying to talk to <player>... appears to be
	// ignoring me/ignorning me [sic]..." wording as Genie, but Drunken Dwarf
	// (macro_event_drunken_dwarf.rs2) uses a completely different, unrelated line:
	// "You're not my matey!" — dialogRejectionPhrases below is the real, checked list,
	// not a single assumed pattern. Read via ChatDialog.Texts() (the chat MODAL's own
	// component tree), not the plain chat log — a real, live-caught bug this fix went
	// through once already for Genie specifically (see solveGenie's own doc comment):
	// chatnpc() renders to an interface component, not a MESSAGE_GAME line, so
	// chat.Since(...) would NEVER see this text no matter how long it waited. Catching
	// this here, before ever calling navigateDialog's own click-through (which would
	// otherwise dismiss the rejection line unread), stops the same class of bug: without
	// it, a rejected NPC still counts as "handled"/"cleared" and this account's Guardian
	// moves on, never revisiting it — fine for THIS npc, but the false "cleared" masked
	// how often that was happening at all.
	chat := actions.NewChatDialog(s, components)
	if chat.IsOpen() {
		texts := strings.Join(chat.Texts(), " ")
		if matchesAnyPhrase(texts, dialogRejectionPhrases) {
			return rejectDialogNPC(s, ev, log)
		}
		// Diagnostic only, not a behavior change: live-found 2026-09-16 (lc2b_15, a
		// 200-account soak) an account's own (never-rejected) Drunken Dwarf cycling
		// "dialogue opened and navigated" -> "still present after handling" three times
		// in a row before going hostile -- meaning the click+navigate genuinely opened
		// SOME dialogue and dismissed it, yet the NPC's own real success branch
		// (%npc_int=drunken_dwarf_happy, which deletes the NPC) was never actually
		// reached. Logging the real text here, once, before navigateDialog dismisses it,
		// is the fastest way to tell "wrong dialogue entirely" from "right dialogue, but
		// something about our click sequence doesn't trigger the same server-side effect
		// a real client's click would" without guessing further.
		if log != nil && texts != "" {
			log(ev.Name + " dialogue text: " + texts)
		}
	}
	opened, interrupted := navigateEventDialog(s, pack, ev, components, log)
	if interrupted {
		return nil
	}
	// Retry unanswered clicks on a later poll so other events can run first.
	if !opened {
		checkFailTeleport(s, pack, ev.Name, preX, preZ, preLevel, preTileOK, log)
		deferEvent(s, ev, 2*pollInterval)
	}

	if opened {
		if log != nil {
			log("dialogue opened and navigated")
		}
	} else if log != nil {
		log("no dialogue opened; checking other events before retrying")
	}

	return nil
}

// A plant attempt yields as soon as its response is known. Growing fruit retries
// after four seconds while the guardian handles other NPCs and real threats.
//
// The plant has a clock (plantclock.go): pickable from tick 45, hostile at tick 90, and an angry one cannot reach an
// account that is more than its leash from where it was added. So the attempt is judged against the plant's age in
// ticks, not against two seconds of wall time: a click is waited on for as long as the route is known to take, and once
// the pick has not landed by plantLeaveAtTicks (less the lag the view has shown) the account walks out of the leash
// while the plant is still passive (plantleash.go), instead of clicking on until it turns.
func solveStrangePlant(s actions.Session, pack *pathfind.Pack, ev *Event, chat *chatlog.Log, log func(string)) error {
	if !npcStillPresent(s, ev.NpcIndex) {
		forgetPlant(s, ev.NpcIndex)
		return nil
	}
	if targetsAnotherAccount(s, ev) {
		return rejectDialogNPC(s, ev, log)
	}
	if npcHasGoneHostile(s, ev.NpcIndex) {
		return HandleEvade(s, pack, ev, 1, log)
	}
	preX, preZ, preLevel, preTileOK := s.State().Tile()
	if preTileOK {
		// Where the seed stands is where it was added, and the leash it turns hostile on is measured from
		// there: remembered now, while it is still passive, not from wherever it has chased the account
		// to by the time it is first seen as the angry one (event_leash.go).
		noteLeashSpawn(s, ev.NpcIndex, ev.NpcType, ev.ThreatX, ev.ThreatZ, preLevel)
	}
	// This account's plant from here on: Busy holds its script through the plant's dangerous stretch (inputhold.go).
	if p := plantsOf(s); p != nil {
		p.engage(ev.NpcIndex)
		if p.hasLeft(ev.NpcIndex) {
			// Already walked out of its leash to wait it out: not walking back to it.
			deferEvent(s, ev, plantStayOutDefer)
			return nil
		}
	}
	lagTicks := s.State().RecentViewLag().Ticks
	if age, ok := plantAgeTicks(s.State(), ev.NpcIndex); ok && plantMustLeave(age, lagTicks) {
		if left, err := leavePlantLeash(s, pack, ev, age, lagTicks, log); left || err != nil {
			return err
		}
	}
	if err := actions.CloseModal(s); err != nil {
		return err
	}
	clickAt := time.Now()
	beforeFruit := strangeFruitCount(s)
	if err := actions.OpNpc(s, talkToOption(ev), ev.NpcIndex); err != nil {
		return err
	}
	wait := plantReplyWait(s)
	if waitEventReply(s, pack, ev, wait, func() bool {
		if plantReply(chat.Since(clickAt)) != "" || strangeFruitCount(s) > beforeFruit || !npcStillPresent(s, ev.NpcIndex) {
			return true
		}
		// Waiting longer on a click the server has not answered must not run the plant's clock out.
		age, ok := plantAgeTicks(s.State(), ev.NpcIndex)
		return ok && plantMustLeave(age, s.State().RecentViewLag().Ticks)
	}, log) {
		return nil
	}
	reply := plantReply(chat.Since(clickAt))
	if strangeFruitCount(s) > beforeFruit {
		// Content grants the fruit before its 22-tick despawn animation. A
		// fresh backpack gain proves the pick even if chat delivery was lost.
		reply = "picked"
	}
	switch reply {
	case "rejected":
		return rejectDialogNPC(s, ev, log)
	case "growing":
		deferEvent(s, ev, 4*time.Second)
		if log != nil {
			log("fruit still growing; checking other events before the next pick (" + plantSpawnSound(s.State(), ev.NpcIndex) + ")")
		}
	case "picked":
		// npc_del follows a 22-tick animation in content 289. Do not re-click a
		// successfully picked plant while that animation is still visible.
		deferEvent(s, ev, 15*time.Second)
		if log != nil {
			log("picked the fruit; waiting for the plant to disappear (" + plantSpawnSound(s.State(), ev.NpcIndex) + ")")
		}
	default:
		if checkFailTeleport(s, pack, ev.Name, preX, preZ, preLevel, preTileOK, log) {
			return nil
		}
		if npcHasGoneHostile(s, ev.NpcIndex) {
			return HandleEvade(s, pack, ev, 1, log)
		}
		age, aged := plantAgeTicks(s.State(), ev.NpcIndex)
		if aged && plantMustLeave(age, s.State().RecentViewLag().Ticks) {
			if left, err := leavePlantLeash(s, pack, ev, age, s.State().RecentViewLag().Ticks, log); left || err != nil {
				return err
			}
		}
		deferEvent(s, ev, 2*pollInterval)
		if log != nil {
			log(fmt.Sprintf("no plant reply yet; checking other events before retrying (%s)", plantClockDiag(s, ev.NpcIndex, wait)))
		}
	}
	return nil
}

func strangeFruitCount(s actions.Session) int64 {
	res := currentResolver()
	if res == nil {
		return 0
	}
	// Revision 289's ordinary inventory component. Bank/trade caches must not
	// count as a new plant receipt, and existing fruit is only a baseline.
	inv := s.State().Inventory(3214)
	if inv == nil {
		return 0
	}
	var count int64
	for _, item := range inv.Slots {
		obj, ok := res.Obj(int(item.ID))
		if ok && strings.EqualFold(obj.Name, "Strange fruit") && !obj.Noted() {
			count += int64(item.Count)
		}
	}
	return count
}

func plantReply(text string) string {
	text = strings.ToLower(text)
	switch {
	case strings.Contains(text, "not here for you"):
		return "rejected"
	case strings.Contains(text, "isn't ready to be picked"):
		return "growing"
	case strings.Contains(text, "pick the fruit"):
		return "picked"
	}
	return ""
}

// Genie needs one click and a short reply wait; silence releases the action lock
// instead of retrying the same index throughout a ten-second transaction.
func solveGenie(s actions.Session, pack *pathfind.Pack, ev *Event, components archive.Components, log func(string)) error {
	if !npcStillPresent(s, ev.NpcIndex) {
		return claimGenieLamp(s, pack, log)
	}
	if targetsAnotherAccount(s, ev) {
		return rejectDialogNPC(s, ev, log)
	}
	if err := actions.CloseModal(s); err != nil {
		return err
	}
	preX, preZ, preLevel, preTileOK := s.State().Tile()
	if err := actions.OpNpc(s, talkToOption(ev), ev.NpcIndex); err != nil {
		return err
	}
	chat := actions.NewChatDialog(s, components)
	if waitEventReply(s, pack, ev, 2*time.Second, func() bool {
		texts := strings.Join(chat.Texts(), " ")
		return matchesAnyPhrase(texts, dialogRejectionPhrases) || genieAccepted(texts)
	}, log) {
		return nil
	}
	texts := strings.Join(chat.Texts(), " ")
	if matchesAnyPhrase(texts, dialogRejectionPhrases) {
		return rejectDialogNPC(s, ev, log)
	}
	if genieAccepted(texts) {
		_, interrupted := navigateEventDialog(s, pack, ev, components, log)
		if interrupted || waitEventReply(s, pack, ev, 0, nil, log) {
			return nil
		}
		if log != nil {
			log(GenieTalkedLine)
		}
		return claimGenieLamp(s, pack, log)
	}
	if checkFailTeleport(s, pack, ev.Name, preX, preZ, preLevel, preTileOK, log) {
		return nil
	}
	_ = actions.CloseModal(s)
	deferEvent(s, ev, 2*pollInterval)
	if log != nil {
		log("no recognised Genie reply yet; checking other events before retrying")
	}
	return nil
}

func genieAccepted(text string) bool {
	text = strings.ToLower(text)
	return strings.Contains(text, "take this lamp") || strings.Contains(text, "happy with your wish")
}

// The two progress lines of a Genie's hand-over that internal/fleet's RandomEventLoot reads to
// count the lamp (randomloot.go), and that cmd/crowdgenietest waits for. Constants so the writer
// here and the readers there cannot drift apart: a rewording used to be enough to make the Loot
// panel's Genie silently count nothing.
//
// GenieTalkedLine is logged whenever the Genie's reply was accepted, which is the first talk
// ("Please take this lamp") AND a repeat one ("I hope you're happy with your wish", content's
// [opnpc1,macro_geni]) -- so it is a cue to look at the pack, not proof a lamp was given.
// LampClaimedLine is logged only when a lamp this claim found is gone from the pack again.
const (
	GenieTalkedLine = "talked to the Genie; claiming the lamp"
	LampClaimedLine = "lamp claimed"
)

// claimGenieLamp finds a "Lamp" in any tracked inventory and runs it through handleLamp
// (Rub -> pick Fishing -> Confirm) immediately. A no-op, not an error, if no lamp is
// present — covers the "already resolved by an earlier click" case in solveGenie above.
func claimGenieLamp(s actions.Session, pack *pathfind.Pack, log func(string)) error {
	res := currentResolver()
	if res == nil {
		return nil
	}
	ev := detectBoxLampEligible(res, heldEventInventories(s.State()), func(candidate *Event) bool {
		return strings.EqualFold(candidate.Name, "Lamp")
	})
	if ev == nil {
		return nil
	}
	if err := handleLamp(s, pack, ev, log); err != nil {
		return err
	}
	if log != nil && !lampStillPresent(s, ev) {
		log(LampClaimedLine)
	}
	return nil
}

// mimeEmoteSeqID maps the 8 emote seq ids the Mime NPC ever animates through to their
// exact button label on the macro_mime_emotes interface. Confirmed two independent ways
// before writing this, not guessed: the numeric ids come from content/pack/seq.pack (a
// plain symbol=name text file used by the server's own script compiler — id 860 =
// emote_cry, etc; this cache format has no name field at all for seq entries, unlike
// obj/npc/loc, which is why this is hardcoded rather than resolved via internal/cachecfg
// the way NPC/loc names are), and the button text comes directly from the real interface
// source (content/scripts/macro events/interfaces/macro_mime_emotes.if's own com_2..com_9
// text= lines). The order here also matches the real script's own emote_cry=0,
// emote_think=1, ..., emote_glass_box=7 numbering (content/scripts/macro events/scripts/
// general/macro_event_mime.rs2's ai_timer switch and if_button table), though this
// function only ever needs the seq-id-to-text mapping, not the numeric index.
var mimeEmoteSeqID = map[int]string{
	860:  "Cry",
	857:  "Think",
	861:  "Laugh",
	866:  "Dance",
	1130: "Climb Rope",
	1129: "Lean on air",
	1128: "Glass Wall",
	1131: "Glass Box",
}

// mimeDialogDeadline bounds one HandleMime call. Originally 4s on the (wrong) assumption
// that the interface opens and shows the answer in the same short window; live testing
// (cmd/guidecrash -tele against the real mime cage) showed a real emote animation at
// t+7s/t+8s after entering and the interface only opening at t+9s — a single call needs
// to span both the "watch" and "click" phases of one cycle to remember the right answer
// (see HandleMime's own doc comment on lastLabel), so this is generous rather than tight.
// Still a hard, bounded cap — never blocks indefinitely — matching every other handler's
// own "wait a real amount of time, then let the caller retry" shape, not a busy loop.
const mimeDialogDeadline = 12 * time.Second

// HandleMime solves the Mime random event for real, not just detection — added
// 2026-09-15 after finding live that roughly half a concurrent fleet could get
// simultaneously trapped with no way out (Mime has no self-timeout the way Mysterious
// Old Man's own abduction does; see macro_event_mime.rs2's own ai_timer, which just
// re-arms itself every 2 ticks forever). The real mechanic, read directly from that
// script: the server tracks which of 8 emotes the Mime is CURRENTLY performing in its
// own %npc_int var, and a click on the macro_mime_emotes interface succeeds exactly when
// it names that same emote — 4 correct in a row (tracked server-side, not by this
// function) releases the player. So solving it is just: read the Mime's own current
// animation (now decoded — see internal/gamestate/npc.go's applyNpcExtended), map it to
// the one button with that emote's name, click it.
//
// REAL, LIVE-FOUND BUG in the first version of this function, not a guess: it returned
// nil the INSTANT chat.IsOpen() was false, with no delay of its own — and unlike every
// other handler's own "nothing to do, return nil" paths (which only fire in rare/
// transient cases), that path is the COMMON case for Mime: the emote interface is only
// open briefly once every ~32-tick cycle, so almost every call hit it. The caller
// (Guardian) treats a nil return as "handled — check again," with no pacing of its own to
// cover for a handler whose own "nothing to do yet" path is near-instant. Redeployed and
// watched live: a genuine zero-delay busy loop across dozens of concurrent accounts
// within seconds, caught and the process killed before it could run longer. Fixed the
// same way every other busy loop tonight was: this function now guarantees a minimum
// real-time floor on every call, by polling for the interface to open for the FULL
// deadline (not bailing the moment it isn't open yet) before ever returning nil.
func HandleMime(s actions.Session, pack *pathfind.Pack, ev *Event, log func(string)) error {
	deadline := time.Now().Add(mimeDialogDeadline)
	if ev.NpcIndex < 0 {
		// The Mime NPC itself wasn't found nearby yet — nothing to read, but still
		// wait out the deadline before returning: see this function's own doc
		// comment on why an instant nil return here is exactly the bug that caused
		// tonight's busy loop.
		//
		// sleepWatchingCombat, not a bare time.Sleep — found during a repo-wide audit
		// following tonight's incident response: Guardian holds inFlight for this
		// entire mimeDialogDeadline (12s) floor, which makes its own next tick a
		// guaranteed no-op the whole time (kick()'s very first line is
		// `if g.inFlight.Load() { return }`), so this floor was a genuinely blind
		// 12s window — the same shape as every other now-fixed blind wait in this file.
		if sleepWatchingCombat(s, mimeDialogDeadline) {
			if log != nil {
				log("in combat while waiting on the Mime cage — fleeing immediately")
			}
			fleeFromCombat(s, pack, baseFleeDist, log)
		}
		return nil
	}
	components := currentInterfaceArchive()
	if components == nil {
		if sleepWatchingCombat(s, mimeDialogDeadline) {
			if log != nil {
				log("in combat while waiting on the Mime cage — fleeing immediately")
			}
			fleeFromCombat(s, pack, baseFleeDist, log)
		}
		return nil
	}
	chat := actions.NewChatDialog(s, components)
	// lastLabel remembers the most recent KNOWN emote seen from this NPC, updated on
	// every poll regardless of whether the interface happens to be open yet. Real,
	// live-found bug this closes, not a guess: the Mime's own "tell you the emote"
	// animation (macro_event_mime.rs2's ai_timer, $count=1) fires BEFORE the interface
	// actually opens ($count=4) — by the time chat.IsOpen() goes true, the Mime has
	// already reverted to an idle pose (a real, observed anim id, 858, that is not one
	// of the 8 puzzle emotes at all) — confirmed live via cmd/guidecrash's own
	// -tele + repeated NPC listing against the real mime cage (2008,4764): a real
	// emote (Climb Rope, then Glass Wall) played at t+7s/t+8s, the interface opened at
	// t+9s, and by then the NPC's own anim had already reverted to 858. Sampling anim
	// only at click-time (the first version of this function) would therefore almost
	// always see the idle pose, not the answer — this remembers the last real one
	// seen anywhere in the same call's polling window instead.
	lastLabel := ""
	for time.Now().Before(deadline) {
		// Combat check — found during a repo-wide audit following tonight's incident
		// response: this loop already polls at 200ms, but nothing in it ever checked
		// combat, so a hostile landing a hit while this account stood in the Mime cage
		// solving the puzzle went completely unnoticed for however much of the 12s
		// deadline was left, the same gap already fixed everywhere else in this file.
		if s.State().InCombat() {
			if log != nil {
				log("in combat while solving the Mime puzzle — fleeing immediately")
			}
			fleeFromCombat(s, pack, baseFleeDist, log)
			return nil
		}
		anim := -1
		for _, npc := range s.State().NPCs() {
			if npc.Index == ev.NpcIndex {
				anim = npc.Anim
				break
			}
		}
		if label, ok := mimeEmoteSeqID[anim]; ok {
			lastLabel = label
		}
		if chat.IsOpen() && lastLabel != "" {
			err := chat.ChooseOption(lastLabel)
			// Real, live-found bug this closes, not a guess: Guardian only serializes
			// ITS OWN re-entry into Handle via inFlight (see guardian.go's kick — it
			// cannot call this function again until this call returns), so an instant
			// return right after a successful click was itself the missing floor —
			// Guardian's poll ticker (600ms) fired again immediately, Detect() still
			// saw "mime" (the click had not visibly resolved server-side yet — the
			// interface can legitimately still read as open for a beat after a correct
			// answer, moving to the next of the 4 required stages), and this function's
			// own lastLabel/anim re-read the SAME still-showing state and clicked AGAIN
			// before the first click had a chance to register. Redeployed at 30
			// concurrent accounts (this exact race is proportionally likelier to be
			// caught by SOME account the more are running, which is why the smaller
			// 5-account verification run didn't show it) and caught live: dozens of
			// accounts repeating "handling"/"still present" within under 2 seconds of
			// each other. A short settle delay after every click — success or not —
			// gives the server time to actually process it before this function can be
			// invoked again at all.
			time.Sleep(1500 * time.Millisecond)
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

// Strange box ("Open" it, name "Strange box" not "Mysterious box" — see names.go's own
// doc comment) puzzle interface ids, found live via a throwaway archive search (grep the
// parsed interface archive for the compiled-in default text "Which shape is blue?" from
// content/scripts/macro events/interfaces/macro_cube.if, then read every component
// sharing its LayerID): main modal 6554, question text 6561, answer buttons 6562/6563/
// 6564 (type=graphic, clickable), answer labels 6565/6566/6567 (their live text is the
// three candidate colours or shapes, in center/side/top order — see
// macro_event_strange_box.rs2's own if_settext calls).
// Lamp ("Rub" it — genie's own follow-up reward, see names.go) skill-choice interface
// ids, found the same way as macro_cube's above: search the parsed archive for the
// compiled-in label "Choose the stat you wish to be advanced!" from
// content/scripts/general/interfaces/xplamp.if, then read every component sharing its
// LayerID. Skill buttons are laid out in the exact order xplamp.if declares them
// (attack, strength, ranged, magic, defence, hitpoints, prayer, agility, herblore,
// thieving, crafting, runecraft, mining, smithing, fishing, ...), one clickable
// component id per skill in that order starting at 2812 — fishing is the 15th, id 2826.
// Fishing is the default choice (every script that first earned a Lamp was a fisher); a
// script that wants another skill sets it per session with SetLampSkill (lamp.go, which also
// lists the content facts: the skill buttons, the confirm button, the xp awarded).
const (
	xplampModalID   = 2808
	xplampFishingID = 2826
	xplampConfirmID = 2831
)

// Component ids below are read directly off content/pack/interface.pack's macro_cube
// entries (6554=macro_cube root ... 6567=answer_box3), cross-checked against
// macro_cube.if's own component order — not guessed or derived from the modal id.
const (
	cubeModalID       = 6554
	cubeCenterModelID = 6555 // macro_cube:center_shape_model
	cubeSideModelID   = 6557 // macro_cube:side_shape_model
	cubeTopModelID    = 6559 // macro_cube:top_shape_model
	cubeQuestionID    = 6561 // macro_cube:question
	cubeAnswer1ID     = 6562 // macro_cube:answer_button1 (center)
	cubeAnswer2ID     = 6563 // macro_cube:answer_button2 (side)
	cubeAnswer3ID     = 6564 // macro_cube:answer_button3 (top)
	// cubeSolveDeadline bounds solveStrangeBox's own internal retry loop — see that
	// function's doc comment for why this needs to be a loop with real headroom, not a
	// single attempt. decodeCubeAnswer resolves the correct button deterministically in
	// the overwhelmingly common case, but a blind guess (~1/3 odds, safe by design — see
	// macro_cube_question_fail) is still the fallback whenever decode comes up short, so
	// this deadline still needs to comfortably cover a run of guessing bad luck.
	cubeSolveDeadline = 90 * time.Second
)

// cubeShapeByObjID maps each of the 15 macro_cube_<colour><shape> object ids (see
// content/pack/obj.pack, ids 3063-3091) to the (shape, colour) pair
// macro_event_strange_box.rs2's get_shape_name/get_shape_colour_name produce for it, so
// decodeCubeAnswer can turn an IF_SETOBJECT model id straight into the same text the
// question/answer components would show for that position.
var cubeShapeByObjID = map[int]struct{ shape, colour string }{
	3063: {"Triangle", "Red"}, 3065: {"Triangle", "Blue"}, 3067: {"Triangle", "Yellow"},
	3069: {"Square", "Red"}, 3071: {"Square", "Blue"}, 3073: {"Square", "Yellow"},
	3075: {"Circle", "Red"}, 3077: {"Circle", "Blue"}, 3079: {"Circle", "Yellow"},
	3081: {"Star", "Red"}, 3083: {"Star", "Blue"}, 3085: {"Star", "Yellow"},
	3087: {"Half Moon", "Red"}, 3089: {"Half Moon", "Blue"}, 3091: {"Half Moon", "Yellow"},
}

// decodeCubeAnswer reads the three shape-model components' IF_SETOBJECT ids (decoded
// generically by gamestate/ifobject.go — this was the missing piece when solveStrangeBox
// was first written; it exists now) plus the question's IF_SETTEXT, and works out which
// button the server-side %xplamp actually points at instead of guessing. The question is
// always one of two exact forms (macro_event_strange_box.rs2's macro_cube_open): "What
// colour is the <shape>?" names one of the three shapes and wants its colour; "Which
// shape is <colour>?" names one of the three colours and wants its shape. Either way, the
// OTHER attribute of whichever position matches is irrelevant — matching the named
// attribute against center/side/top is enough to know which button (1/2/3) is correct.
//
// avoid is the signature (see below) of the round this solve last actually answered —
// decodeCubeAnswer refuses to return a match against a snapshot identical to it. Live-
// found 2026-09-15 (a 200-account soak, real reports of the box "taking too long" and
// "duplicating"): gamestate's texts/ifObjects maps (state.go) are plain last-value maps,
// never cleared on IF_CLOSE — so the instant this solve's own click reopens the modal for
// a fresh question, decodeCubeAnswer's very first poll can read the PREVIOUS round's still-
// cached IF_SETTEXT/IF_SETOBJECT values (the new packets for the new question genuinely
// hadn't landed yet) and confidently answer the ALREADY-RESOLVED question instead of
// waiting for the real new one — indistinguishable from a correct decode by its own
// success/fail return, so every round after the first was silently degraded to ~1/3-odds
// guessing (matching a real one-in-three chance of the stale answer still being right by
// coincidence), which is exactly the shape of the live symptom: some solves cleared in one
// or two rounds, others needed a long tail of replication cycles before accidentally
// landing correct.
func decodeCubeAnswer(s actions.Session, avoid string) (buttonComID int, sig string, ok bool) {
	question, hasQuestion := s.State().TextKnown(cubeQuestionID)
	if !hasQuestion {
		return 0, "", false
	}
	positions := []struct {
		comID  int
		button int
	}{
		{cubeCenterModelID, cubeAnswer1ID},
		{cubeSideModelID, cubeAnswer2ID},
		{cubeTopModelID, cubeAnswer3ID},
	}
	type shaped struct {
		shape, colour string
		button        int
	}
	shapes := make([]shaped, 0, 3)
	objIDs := make([]int, 0, 3)
	for _, p := range positions {
		objID, hasObj := s.State().InterfaceObject(p.comID)
		if !hasObj {
			return 0, "", false
		}
		sc, known := cubeShapeByObjID[objID]
		if !known {
			return 0, "", false
		}
		shapes = append(shapes, shaped{sc.shape, sc.colour, p.button})
		objIDs = append(objIDs, objID)
	}
	sig = fmt.Sprintf("%s|%d,%d,%d", question, objIDs[0], objIDs[1], objIDs[2])
	if sig == avoid {
		// Same question+shapes as the round already answered — the new packets for this
		// round genuinely haven't landed yet, not a real (if stale-coincidental) answer.
		return 0, sig, false
	}

	const shapeQuestionPrefix = "What colour is the "
	const colourQuestionPrefix = "Which shape is "
	switch {
	case strings.HasPrefix(question, shapeQuestionPrefix):
		target := strings.TrimSuffix(strings.TrimPrefix(question, shapeQuestionPrefix), "?")
		for _, sh := range shapes {
			if sh.shape == target {
				return sh.button, sig, true
			}
		}
	case strings.HasPrefix(question, colourQuestionPrefix):
		target := strings.TrimSuffix(strings.TrimPrefix(question, colourQuestionPrefix), "?")
		for _, sh := range shapes {
			if sh.colour == target {
				return sh.button, sig, true
			}
		}
	}
	return 0, sig, false
}

// HandleBoxLamp ports the shape of RandomEvents.ts's "box"/"lamp" handling for the Lamp
// (click InvButton option 1 — "Rub" — then navigate whatever reward-choice dialogue
// follows, the same "don't get stuck, not a solver" scope as HandleDialogNpc) but
// actually SOLVES the Strange box's own question puzzle rather than trying to route it
// through navigateDialog, which only knows the standard chat-dialog interface and would
// never recognize this custom modal (chat.IsOpen() would just stay false forever while
// the box sits open, unresolved, with Guardian re-detecting and re-triggering this
// handler every 600ms — the same zero-delay busy-loop class this package has hit before).
//
// The puzzle asks the player to match a randomly chosen shape/colour against one of 3
// on-screen 3D models — genuinely solvable in principle by decoding which IF_SETOBJECT
// model id landed on each of the three shape components and cross-referencing
// content/scripts/macro events/configs/antimacro.obj's shape/colour-per-id table, but
// that's a new protocol decode this client doesn't have yet (IF_SETOBJECT is never
// parsed — see gamestate's own "only decode what's needed" convention). Instead this
// always guesses answer button 1: per macro_event_strange_box.rs2's own
// macro_cube_question_fail, a WRONG guess is explicitly safe — it only replicates the
// cube (capped at 28) and re-asks with a freshly randomized correct answer — so a fixed
// guess succeeds with independent ~1/3 odds per attempt, overwhelmingly likely within a
// handful of tries, with no risk to the account either way.
//
// Solves start-to-finish inside solveStrangeBox rather than one click per call: every
// answer_button click closes the modal first (if_button,macro_cube:answer_button1's own
// if_close, unconditional) whether the guess was right or wrong, and after a wrong guess
// (or a right guess that only cleared one of several already-replicated copies) the box
// needs a fresh OPHELD1 "Open" to show the next question — a real multi-round exchange,
// not a single action. Guardian's own per-event-signature retry cap (4 attempts before a
// 45s cooldown — see guardian.go) exists for a handler that keeps failing to make any
// progress; it doesn't know the difference between that and a puzzle that's genuinely
// advancing one guess at a time, so calling it once per guess made it give up mid-solve
// on live runs that needed more than 4 rounds (confirmed via cmd/guidecrash -forcerandom
// box: real "answered incorrectly"/"completed this cube, but there are more" progress,
// still capped at "gave up after 4 attempts"). Looping here keeps the whole solve as a
// single Guardian attempt, the same fix already applied to HandleMime for the same class
// of problem.
func HandleBoxLamp(s actions.Session, pack *pathfind.Pack, ev *Event, log func(string)) error {
	if ev.ItemID < 0 {
		return nil
	}
	if ev.Name != "Strange box" {
		return handleLamp(s, pack, ev, log)
	}
	return solveStrangeBox(s, pack, ev, log)
}

// handleLamp covers the Lamp reward (genie's own follow-up gift, see names.go): click
// Rub (OPHELD1 — see the shared doc comment on why this isn't InvButton), which opens
// xplamp's own skill-choice modal directly (if_openmain(xplamp) — no intermediate
// dialogue), then clicks Fishing and Confirm. Not routed through navigateDialog/
// ChatDialog: xplamp is a custom modal, not the standard chat-dialog interface, so
// ChatDialog.IsOpen() would never recognize it — the same class of bug this package hit
// on the Strange box (see that function's own doc comment), confirmed live here too: the
// old navigateDialog-based version left the Lamp sitting open and unconsumed, Guardian
// re-detecting it every tick and hitting its 4-attempt retry cap over and over
// ("gave up on Lamp after 4 attempts") without ever actually claiming the XP.
func handleLamp(s actions.Session, pack *pathfind.Pack, ev *Event, log func(string)) error {
	comID, objID := ev.InvComID, ev.ItemID
	skill := lampSkillFor(s) // Fishing unless the script chose another (lamp.go)
	xp0 := lampXP(s, skill)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if waitEventReply(s, pack, ev, 0, nil, log) {
			return nil
		}
		if main, _, _ := s.State().Modals(); main == xplampModalID {
			if err := actions.IfButton(s, skill.ButtonID); err != nil {
				return err
			}
			if waitEventReply(s, pack, ev, 600*time.Millisecond, nil, log) {
				return nil
			}
			err := actions.IfButton(s, xplampConfirmID)
			waitEventReply(s, pack, ev, 1500*time.Millisecond, func() bool { return !lampStillPresent(s, ev) }, log)
			if err == nil && !lampStillPresent(s, ev) {
				if log != nil {
					log(LampRubbedLine(skill, lampXPGained(s, skill, xp0)))
				}
				continueLampReply(s, log)
			}
			return err
		}

		slot := -1
		for _, it := range s.State().KnownInventories()[comID] {
			if it.ID == objID {
				slot = it.Slot
				break
			}
		}
		if slot == -1 {
			return nil // no longer in inventory — already claimed.
		}
		if err := actions.OpHeld(s, 1, objID, slot, comID); err != nil {
			return err
		}
		if waitEventReply(s, pack, ev, 800*time.Millisecond, func() bool {
			main, _, _ := s.State().Modals()
			return main == xplampModalID || !lampStillPresent(s, ev)
		}, log) {
			return nil
		}
	}
	return nil
}

// lampReplyWait is how long a spent Lamp's reply ("Your wish has been granted! ... Click here to continue") is looked for.
const lampReplyWait = 3 * time.Second

// continueLampReply presses continue on the chat box a spent Lamp leaves open. Live 16 Oct 2026 the box stayed up after a Genie's
// lamp was claimed at the Nature gate, and the crafter, whose trade screens cannot open over it, answered nobody for 15 minutes
// until the watchdog restarted him.
func continueLampReply(s actions.Session, log func(string)) {
	components := currentInterfaceArchive()
	if components == nil {
		return
	}
	dlg := actions.NewChatDialog(s, components)
	deadline := time.Now().Add(lampReplyWait)
	for time.Now().Before(deadline) {
		if dlg.IsOpen() && dlg.CanContinue() {
			if err := dlg.Continue(); err == nil && log != nil {
				log("lamp: pressed continue on the reply")
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func lampStillPresent(s actions.Session, ev *Event) bool {
	for _, item := range s.State().KnownInventories()[ev.InvComID] {
		if item.ID == ev.ItemID {
			return true
		}
	}
	return false
}

func solveStrangeBox(s actions.Session, pack *pathfind.Pack, ev *Event, log func(string)) error {
	comID, objID := ev.InvComID, ev.ItemID
	deadline := time.Now().Add(cubeSolveDeadline)
	var lastSig string
	tally := newCubeTally(s, ev)
	defer tally.report(s, ev, log)
	for time.Now().Before(deadline) {
		if waitEventReply(s, pack, ev, 0, nil, log) {
			return nil
		}
		// Combat check — found during a repo-wide audit following tonight's incident
		// response: this loop can run for the full cubeSolveDeadline (90s) clicking
		// through the cube puzzle with nothing checking combat the whole time, the same
		// blind-wait shape already fixed today for solveStrangePlant/solveGenie/
		// HandleHazard. Guardian's own inFlight guard neuters its background ticker for
		// this entire call, so this loop is the only thing that can notice real damage
		// landing for as long as it runs.
		if s.State().InCombat() && abortForCombat(s, pack, ev, "while solving the cube puzzle", log) {
			return nil
		}
		if main, _, _ := s.State().Modals(); main == cubeModalID {
			// The question/model IF_SETTEXT/IF_SETOBJECT packets can land a beat after
			// the modal itself opens, so decode gets a settle window. decodeCubeAnswer's own
			// avoid parameter (see its doc comment) is what stops this settle-poll from
			// confidently reusing the PREVIOUS round's still-cached question/shapes the
			// instant the modal reopens, before the new round's real packets land.
			tally.rounds++
			button, sig, decoded := 0, lastSig, false
			settleDeadline := time.Now().Add(cubeDecodeSettle)
			for time.Now().Before(settleDeadline) {
				if b, s2, ok := decodeCubeAnswer(s, lastSig); ok {
					button, sig, decoded = b, s2, true
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if !decoded {
				// Never answer blind. Live 2026-09-19, seven fishers held 20-28 boxes for up
				// to two hours: a wrong answer replicates the cube, so a one-in-three guess
				// adds a box two times in three and the pack drifts up to the 28 cap. Closing
				// and reopening costs nothing and brings a fresh question.
				tally.undecoded++
				tally.noteUndecoded(s)
				_ = actions.CloseModal(s)
				if waitEventReply(s, pack, ev, cubeModalWait, func() bool { return !cubeModalOpen(s) }, log) {
					return nil
				}
				continue
			}
			tally.decoded++
			lastSig = sig
			if err := actions.IfButton(s, button); err != nil {
				return err
			}
			// Wait for the answer's if_close before anything else is sent, so the next
			// Open can never reach the server while this answer is still in flight.
			if waitEventReply(s, pack, ev, cubeModalWait, func() bool { return !cubeModalOpen(s) }, log) {
				return nil
			}
			continue
		}

		slot := -1
		for _, it := range s.State().KnownInventories()[comID] {
			if it.ID == objID {
				slot = it.Slot
				break
			}
		}
		if slot == -1 {
			// No longer in inventory: either the reward was already granted (solved),
			// or this was never actually confirmed present — either way, done.
			return nil
		}
		if err := actions.OpHeld(s, 1, objID, slot, comID); err != nil {
			return err
		}
		// Wait for the modal rather than re-sending Open after a fixed second: on the
		// production round trip a second Open can land after the first question is already
		// showing, and the server then if_closes it and asks a new one, so an answer worked
		// out from the first question is wrong two times in three.
		if waitEventReply(s, pack, ev, cubeModalWait, func() bool { return cubeModalOpen(s) || !lampStillPresent(s, ev) }, log) {
			return nil
		}
	}
	return nil
}

// Handle dispatches a detected event to its handler. Both trapped events are actually
// solved, not just detected: "maze" via SolveMazeEventVerbose (see maze.go's package doc
// comment for the full mechanics and COLLAB.md for the live proof), "mime" via
// HandleMime just below (watches the Mime NPC's own decoded animation id —
// internal/gamestate/npc.go's anim field — and clicks the matching emote button on the
// macro_mime_emotes interface; see HandleMime's own doc comment and COLLAB.md's Mime
// entries for the live verification trail). Ent "hijack" remains the one confirmed gap
// (needs the local player's own faceEntity, part of PLAYER_INFO's extended per-entity
// data that internal/gamestate/position.go deliberately never decodes) — unlike Mime,
// nothing in this package resolves it.
//
// attempt is the 1-based count of how many times this exact event (by signature — see
// guardian.go's eventSignature) has been handled in a row without clearing, from the
// caller's own bookkeeping (Guardian.noteAttempt). Only HandleEvade currently uses it
// (to escalate its flee distance — see that function's own doc comment for the live
// production case this was added for); every other handler ignores it. Pass 1 for a
// caller with no such bookkeeping of its own.
func Handle(s actions.Session, pack *pathfind.Pack, ev *Event, attempt int) error {
	return HandleWithLog(s, pack, ev, attempt, nil)
}

// HandleWithLog is Handle with an optional progress-line callback, currently only
// used by the maze solver (one line per door confirmed open/refused, route summary,
// modal dismissals). Guardian passes its own Log so a production account's log shows
// the actual door-by-door trail rather than just "handling"/"handler error" — the
// 2026-09-14 `spare2` incident was diagnosed blind precisely because it didn't.
func HandleWithLog(s actions.Session, pack *pathfind.Pack, ev *Event, attempt int, log func(string)) error {
	switch ev.Kind {
	case KindHazard:
		return HandleHazard(s, pack, ev, log)
	case KindEvade:
		return HandleEvade(s, pack, ev, attempt, log)
	case KindTrapped:
		if ev.Name == "mime" {
			return HandleMime(s, pack, ev, log)
		}
		if ev.Name != "maze" {
			return nil
		}
		var lr LocResolver
		if res := currentResolver(); res != nil {
			lr, _ = res.(LocResolver)
		}
		_, err := SolveMazeEventVerbose(s, lr, 0, log)
		return err
	case KindDialogNpc:
		return HandleDialogNpc(s, pack, ev, log)
	case KindBoxLamp:
		return HandleBoxLamp(s, pack, ev, log)
	default:
		return nil
	}
}

// whirlpoolLifetime is how long a fishing spot stays a whirlpool: npc_changetype(..., 60).
const whirlpoolLifetime = 36 * time.Second

// evadeThreatDiag names exactly what an evade is reacting to. Live 2026-09-19, after the
// fresh-spawn radius fix, 70 accounts still took up "event monster" in the same second at
// the Catherby camp; this records whether the NPC still counted as a threat when handled.
func evadeThreatDiag(state *gamestate.State, ev *Event, x, z int) string {
	for _, n := range state.NPCs() {
		if n.Index != ev.NpcIndex {
			continue
		}
		return fmt.Sprintf("evade threat: %s face %d self %d seen %dt threatens=%v",
			describeNPC(n, x, z), n.FaceEntity, state.Self(), n.VisibleTicks, threatensAccount(n, state, x, z))
	}
	return fmt.Sprintf("evade threat: npc %d type %d no longer in view", ev.NpcIndex, ev.NpcType)
}

// plantSpawnSound says whether this account heard triffid_appear when the plant came into
// view. Content plays it only to the plant's owner, so once live logs confirm owners hear it
// and rejected accounts do not, bystanders can skip walking over to ask.
func plantSpawnSound(state *gamestate.State, npcIndex int) string {
	for _, n := range state.NPCs() {
		if n.Index != npcIndex {
			continue
		}
		heard, any := state.HeardSoundNear(gamestate.SoundTriffidAppear, state.Tick()-n.VisibleTicks, 3)
		return fmt.Sprintf("spawn sound heard=%v, sounds seen=%v, in view %dt", heard, any, n.VisibleTicks)
	}
	return "spawn sound: plant not in view"
}
