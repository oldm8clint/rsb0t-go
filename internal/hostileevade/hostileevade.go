// Package hostileevade walks an account clear of a permanently-spawned hostile NPC before it
// engages. It is the impure half of internal/scripts/hostileavoid: that package decides, with no
// session and no I/O, and this one moves the account.
//
// Why this is its own package rather than a method on one runner. The same break-off has to run
// from two unrelated walk loops: internal/scripts/freshstartrunner's own walkNearCore poll, and
// internal/webwalk's Walker, which fisherrunner.WalkNear routes through once an account is
// around Catherby. Before this split only the first had it, which is exactly why the White
// wolves north of the Catherby fishing camp were never covered — the hook was not on the code
// path those accounts actually walk. Two copies of a 150-line escape loop is how the two would
// drift apart, so both callers share this one.
//
// What this deliberately does NOT do: eat, or run for the bank. Every wait goes through
// Deps.Delay, which for a running bot is that bot's own guarded delay — it returns false the
// moment the script's own eat/flee rules are due, and BreakOff returns at once so the script can
// take a bite or run for the bank. internal/scripts/fisherplan owns those rules and keeps owning
// them; a second copy down here is how the two would end up disagreeing.
//
// Deliberately NOT in internal/randomevents: handle.go and guardian.go have open claims from
// other sessions in COLLAB.md. This takes randomevents.TryLock exactly as the previous call site
// did, so it never drives movement while Guardian owns the account.
package hostileevade

import (
	"fmt"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/hostileavoid"
)

// The budget is what stops a break-off turning into a travel-blocking loop. Breaking off and
// resuming naturally walks the account back toward the same route, so a hostile parked on the
// only way through would otherwise be evaded forever and the leg would never complete — trading
// a death for a stall, which is not a trade worth making. After Budget break-offs inside Window,
// evasion stands down for Rest and the account pushes through, where Guardian's own in-combat
// flee and the script's own eat/flee rules take over.
//
// Vars rather than consts so a live harness can vary them while measuring; nothing in the
// shipping code changes them.
var (
	Budget = 6
	Window = 2 * time.Minute
	Rest   = 60 * time.Second
)

const (
	// deadline bounds one whole break-off, the same shape as this project's other escape loops
	// (randomevents' fleeWalkDeadline is 20s for the same reason): a genuinely stuck case must
	// hand control back rather than hold the account forever.
	deadline = 20 * time.Second
	// leg is how long one escape walk gets to either arrive or clear the threat before a fresh,
	// farther destination is picked. A chasing npc closes distance, so waiting out a whole walk
	// unmonitored is the bug, not the fix.
	leg = 2 * time.Second
	// DefaultPoll is how often position and threat are re-sampled mid-walk.
	DefaultPoll = 250 * time.Millisecond
	// arrival is how close counts as having reached an escape tile. Generous: the goal is
	// separation, not an exact tile.
	arrival = 2
	// sightingLogEvery throttles the "hostile in view" diagnostic. Without it this would log on
	// every poll for as long as a wolf is in view, the busy-logging shape this project has been
	// bitten by before (one account producing a 6.7 GB fleet log).
	sightingLogEvery = 15 * time.Second
	// sightingRange is how far out a catalogued hostile is worth mentioning in the log. Wider
	// than any notice ring on purpose: the question it answers is "did the route go anywhere
	// near one at all", which only a generous radius can answer.
	sightingRange = 15
)

// Deps is the minimum one break-off needs. Deliberately not any runner's own Deps: this package
// is shared by two callers whose dependency structs have nothing else in common.
type Deps struct {
	Session actions.Session
	Pack    *pathfind.Pack
	// Delay is the caller's cancellable wait. For a running bot this is its guarded delay, which
	// is what keeps the script's own survival rules in charge — see the package doc comment.
	Delay func(time.Duration) bool
	// Log receives one already-formatted line per notable event; nil drops them.
	Log func(string)
	// Note, when set, returns a short suffix for the break-off log line. The fisher uses it to
	// say its own HP threshold is already crossed. Keeps this package free of any one script's
	// survival rules while still letting a log line mention them.
	Note func() string
	// Cfg tunes the notice and break-off margins. Cfg.Enabled false turns this off outright, so
	// a caller that has not thought about its margins gets nothing rather than a wrong default —
	// the right margin depends on how often the caller polls, which only the caller knows.
	Cfg hostileavoid.Config
	// Poll overrides how often position and threat are re-sampled during a break-off; 0 uses
	// DefaultPoll.
	Poll time.Duration
	// Blocked, when set, is called the moment this package concludes it cannot get the account
	// past a hostile: it has broken off Budget times in Window and the hostile out-levels the
	// account far enough that pushing through would kill it. It names the threat so the CALLER
	// can route the next walk around it.
	//
	// It exists because before 2026-09-22 that conclusion had nowhere to go. The branch below
	// has only ever had two outcomes -- push on through, or keep breaking off -- and for a weak
	// account against a strong hostile neither one can ever end. Live that day, six
	// ProgressiveFisher accounts walking home from Karamja to Catherby produced 167 break-offs
	// against 163 clear-and-resumes (a ratio of almost exactly one, which is an oscillation,
	// not progress) and 20 "NOT pushing on through: level 27 against combat 3" lines, all at
	// 0 xp. The route kept leading back through the same Giant bats and nothing could change
	// the route.
	//
	// Why a callback rather than acting here: this package has no destination and no walker --
	// it deliberately knows nothing about where the account is going (see the package doc
	// comment). Only the caller can decide whether routing around is possible, and
	// internal/webwalk already has the machinery for it in the flee squares it keeps for real
	// attackers. That machinery never fired for this case precisely because a successful
	// break-off means no attacker ever lands, so there was nothing for it to see.
	Blocked func(name string, x, z, level int)
}

func (d Deps) logf(format string, args ...interface{}) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

func (d Deps) poll() time.Duration {
	if d.Poll > 0 {
		return d.Poll
	}
	return DefaultPoll
}

func (d Deps) wait(t time.Duration) bool {
	if d.Delay == nil {
		time.Sleep(t)
		return true
	}
	return d.Delay(t)
}

func (d Deps) note() string {
	if d.Note == nil {
		return ""
	}
	return d.Note()
}

type state struct {
	mu        sync.Mutex
	count     int
	windowEnd time.Time
	restUntil time.Time
	// seenLoggedAt throttles the sighting diagnostic below.
	seenLoggedAt time.Time
}

// states is per-session, keyed the same way randomevents keys its own per-session bookkeeping:
// several accounts share this process, so none of this can be package-global state. Both callers
// share one budget per session, which is correct — it is one account either way, and a route
// that keeps re-entering the same pack should not get a fresh allowance by changing walk loop.
var states sync.Map // actions.Session -> *state

func stateFor(s actions.Session) *state {
	if v, ok := states.Load(s); ok {
		return v.(*state)
	}
	v, _ := states.LoadOrStore(s, &state{})
	return v.(*state)
}

// claim reports whether a break-off may start now, and books it against the budget.
func (st *state) claim(now time.Time) (ok bool, exhausted bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if now.Before(st.restUntil) {
		return false, false
	}
	if now.After(st.windowEnd) {
		st.count = 0
		st.windowEnd = now.Add(Window)
	}
	st.count++
	if st.count > Budget {
		st.restUntil = now.Add(Rest)
		st.count = 0
		st.windowEnd = now.Add(Window)
		return false, true
	}
	return true, false
}

// noteSighting reports whether a diagnostic line may be logged now.
func (st *state) noteSighting(now time.Time) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if now.Sub(st.seenLoggedAt) < sightingLogEvery {
		return false
	}
	st.seenLoggedAt = now
	return true
}

// Snapshot captures everything a decision needs in one pass, so the two halves of a decision can
// never disagree about where anything is.
func Snapshot(d Deps) (hostileavoid.Snapshot, bool) {
	var snap hostileavoid.Snapshot
	if d.Session == nil {
		return snap, false
	}
	st := d.Session.State()
	x, z, level, ok := st.Tile()
	if !ok {
		return snap, false
	}
	snap.X, snap.Z, snap.Level = x, z, level
	snap.InCombat = st.InCombat()
	if lvl, known := st.CombatLevel(); known {
		snap.CombatLevel = lvl
	}
	self := st.Self()
	for _, npc := range st.NPCs() {
		if _, known := hostileavoid.Known(npc.Type); !known {
			continue
		}
		snap.NPCs = append(snap.NPCs, hostileavoid.NPC{
			Index: npc.Index, Type: npc.Type, X: npc.X, Z: npc.Z, Level: npc.Level,
			TargetsUs: npc.TargetsPlayer(self),
		})
	}
	return snap, true
}

// nearestKnownHostile returns the closest catalogued hostile within sightingRange on the
// account's own level, for the diagnostic line only.
func nearestKnownHostile(snap hostileavoid.Snapshot) (*hostileavoid.NPC, hostileavoid.Facts) {
	var best *hostileavoid.NPC
	var bestFacts hostileavoid.Facts
	bestDist := 0
	for i := range snap.NPCs {
		npc := snap.NPCs[i]
		if npc.Level != snap.Level {
			continue
		}
		f, ok := hostileavoid.Known(npc.Type)
		if !ok {
			continue
		}
		dist := hostileavoid.Chebyshev(npc.X, npc.Z, snap.X, snap.Z)
		if dist > sightingRange {
			continue
		}
		if best == nil || dist < bestDist {
			found := npc
			best, bestDist, bestFacts = &found, dist, f
		}
	}
	return best, bestFacts
}

// avoidance keeps an escape route from being planned through a second hostile's own hunt range.
// Each exclusion radius is capped at how far that npc already is, so a hostile the account is
// standing next to can never block the account's own tile and make the escape unplannable — the
// same discipline randomevents' visibleWolfAvoidance uses for the identical reason.
func avoidance(snap hostileavoid.Snapshot, cfg hostileavoid.Config) func(int, int, int) bool {
	type exclusion struct{ x, z, r int }
	var zones []exclusion
	for _, npc := range snap.NPCs {
		if npc.Level != snap.Level {
			continue
		}
		f, ok := hostileavoid.Known(npc.Type)
		if !ok {
			continue
		}
		r := f.NoticeRange(cfg.NoticeMargin)
		if here := hostileavoid.Chebyshev(snap.X, snap.Z, npc.X, npc.Z); r > here {
			r = here
		}
		if r < 1 {
			continue
		}
		zones = append(zones, exclusion{npc.X, npc.Z, r})
	}
	if len(zones) == 0 {
		return nil
	}
	level := snap.Level
	return func(x, z, l int) bool {
		if l != level {
			return false
		}
		for _, e := range zones {
			if hostileavoid.Chebyshev(x, z, e.x, e.z) < e.r {
				return true
			}
		}
		return false
	}
}

// runForEscape turns running on for a break-off. Ports the same rule randomevents' own combat
// flee uses (RunManager.ts ENERGY_MIN_ATTACKED=1): getting away is worth the last point of
// energy, never gated behind the cruising threshold ordinary travel uses.
func runForEscape(d Deps) {
	st := d.Session.State()
	if st.RunEnabled() {
		return
	}
	if energy, ok := st.Energy(); ok && energy >= 1 {
		_ = actions.SetRun(d.Session, true)
	}
}

// reachableEscape picks the first ring candidate the collision pack can actually route to,
// mirroring randomevents' own walkableFlee: candidate generation is pure and tested in
// hostileavoid, and reachability is checked here against the real pack.
func reachableEscape(d Deps, snap hostileavoid.Snapshot, threat hostileavoid.NPC, dist int, avoid func(int, int, int) bool) (pathfind.NavPoint, bool) {
	from := pathfind.NavPoint{X: snap.X, Z: snap.Z, Level: snap.Level}
	for _, c := range hostileavoid.EscapeCandidates(snap.X, snap.Z, snap.Level, threat.X, threat.Z, dist) {
		if hostileavoid.AnyNear(snap, c.X, c.Z, c.Level, d.Cfg.NoticeMargin+1) {
			// Escaping one wolf into the middle of the rest of the pack is how the flee
			// destinations near Catherby killed accounts in the first place.
			continue
		}
		to := pathfind.NavPoint{X: c.X, Z: c.Z, Level: c.Level}
		if d.Pack.FindPathAvoiding(from, to, avoid).OK {
			return to, true
		}
	}
	return pathfind.NavPoint{}, false
}

// BreakOff walks clear of a permanently-spawned hostile that is about to engage, and reports
// whether it moved the account. A true return means the caller should plan its route again from
// wherever the account now stands.
func BreakOff(d Deps) (bool, string) {
	if !d.Cfg.Enabled || d.Session == nil || d.Pack == nil {
		return false, ""
	}
	snap, ok := Snapshot(d)
	if !ok {
		return false, ""
	}
	act := hostileavoid.Decide(d.Cfg, snap)
	st := stateFor(d.Session)
	// Say so when a catalogued hostile is merely in view. Without this line a quiet log is
	// ambiguous in the one way that matters: "the route never went near a wolf" and "it did and
	// this code declined to act" read identically, and the first is the far more likely answer
	// on any given run.
	if !act.Evade {
		if n, nearest := nearestKnownHostile(snap); n != nil && st.noteSighting(time.Now()) {
			d.logf("hostile: %s in view %d tiles away at %d,%d — outside its %d-tile notice ring, walking on",
				nearest.Name, hostileavoid.Chebyshev(n.X, n.Z, snap.X, snap.Z), n.X, n.Z,
				nearest.NoticeRange(d.Cfg.NoticeMargin))
		}
		return false, ""
	}
	// A hostile behind walls is no threat on this walk: NPCs step naively toward a target and
	// cannot route round a building. Live 2026-09-20 the White wolf at 2842,3450 sat 7-8 tiles
	// from Harry's counter in a straight line but 25-32 steps round the buildings, and every
	// approach to the shop broke off and replanned on repeat while the shop never opened.
	// Not once it is already on us: then the Guardian's combat flee needs the account moving.
	if steps, walled := walledOff(d.Pack, snap, act.Threat, act.BreakOff); walled && !act.Threat.TargetsUs && !snap.InCombat {
		if st.noteSighting(time.Now()) {
			d.logf("hostile: %s at %d,%d is %d tiles away in a straight line but %s round the walls — walking on",
				act.Facts.Name, act.Threat.X, act.Threat.Z, hostileavoid.Chebyshev(act.Threat.X, act.Threat.Z, snap.X, snap.Z), steps)
		}
		return false, ""
	}
	// Never book a break-off the account is already clear of. hostileavoid.Decide now clamps
	// the break-off ring so it can no longer sit inside the notice ring, which is what made
	// this happen; this is the belt to that braces, and it costs one cheap comparison. Checked
	// BEFORE claiming, so a no-op can never spend a budget that exists to bound real ones.
	if hostileavoid.Cleared(snap, act.Threat, act.BreakOff) {
		return false, ""
	}
	allowed, exhausted := st.claim(time.Now())
	if !allowed && act.Facts.Outclasses(snap.CombatLevel) {
		// The budget bounds break-offs from a pack sitting on the only way through, but pushing
		// on through something that out-levels the account this far is how three combat-3
		// fishers died to Giant bats (see Facts.Outclasses). Keep breaking off instead: the
		// walk's own timeout then hands the account back to its script alive.
		if exhausted {
			d.logf("hostile: broke off from %s %d times in %s without clearing it — NOT pushing on through: level %d against combat %d, still breaking off",
				act.Facts.Name, Budget, Window, act.Facts.Level, snap.CombatLevel)
			// ...and tell the caller, so the next walk can go a different way. Only on the
			// exhausted edge, which st.claim reports once per Window, so this is at most one
			// call per account per Window however many times the account breaks off.
			if d.Blocked != nil {
				d.Blocked(act.Facts.Name, act.Threat.X, act.Threat.Z, act.Threat.Level)
			}
		}
		allowed = true
	}
	if !allowed {
		if exhausted {
			d.logf("hostile: broke off from %s %d times in %s without clearing it — pushing on through instead for %s, Guardian and the script's own flee rules cover it from here",
				act.Facts.Name, Budget, Window, Rest)
		}
		return false, ""
	}
	// Never drive movement while Guardian owns the account: a random-event solver walking the
	// account toward its NPC at the same time as this walks it away is two goroutines fighting
	// over one session.
	unlock, got := randomevents.TryLock(d.Session)
	if !got {
		// Worth a line: once combat actually starts, Guardian's own flee takes the lock and
		// holds it, so a break-off that loses this race is exactly the case where this code was
		// too late to help. Seeing it in a log is how that gets tuned rather than guessed.
		if st.noteSighting(time.Now()) {
			d.logf("hostile: wanted to break off from the %s but Guardian holds the account — leaving it to Guardian's own flee", act.Facts.Name)
		}
		return false, ""
	}
	defer unlock()

	why := fmt.Sprintf("broke off from the %s", act.Facts.Name)
	d.logf("hostile: %s (level %d) at %d,%d is %d tiles away and %s — breaking off to %d tiles before it engages%s",
		act.Facts.Name, act.Facts.Level, act.Threat.X, act.Threat.Z,
		hostileavoid.Chebyshev(act.Threat.X, act.Threat.Z, snap.X, snap.Z), act.Reason, act.BreakOff, d.note())

	end := time.Now().Add(deadline)
	dist := act.BreakOff
	moved := false
	for time.Now().Before(end) {
		cur, ok := Snapshot(d)
		if !ok {
			return moved, why
		}
		if hostileavoid.Cleared(cur, act.Threat, act.BreakOff) {
			// moved, not true: only a break-off that actually walked the account somewhere may
			// tell the caller to plan again. Reporting a move that never happened is what put
			// internal/webwalk's walkLeg into a replan-every-poll loop.
			if moved {
				d.logf("hostile: clear of the %s — resuming the route from %d,%d,%d", act.Facts.Name, cur.X, cur.Z, cur.Level)
			}
			return moved, why
		}
		avoid := avoidance(cur, d.Cfg)
		dest, found := reachableEscape(d, cur, act.Threat, dist, avoid)
		if !found {
			// Nowhere to go at this ring: widen it and try again rather than standing still.
			if !d.wait(d.poll()) {
				return moved, why
			}
			dist += 4
			if dist > act.BreakOff+16 {
				d.logf("hostile: no reachable tile clear of the %s — standing and letting the script's own rules handle it", act.Facts.Name)
				return moved, why
			}
			continue
		}
		res := d.Pack.FindPathAvoiding(pathfind.NavPoint{X: cur.X, Z: cur.Z, Level: cur.Level}, dest, avoid)
		if !res.OK || len(res.Waypoints) < 2 {
			dist += 4
			continue
		}
		runForEscape(d)
		if err := actions.Walk(d.Session, res.Waypoints, true); err != nil {
			d.logf("hostile: could not walk away from the %s: %v", act.Facts.Name, err)
			return moved, why
		}
		moved = true

		legEnd := time.Now().Add(leg)
		for time.Now().Before(legEnd) && time.Now().Before(end) {
			if !d.wait(d.poll()) {
				return moved, why
			}
			now, ok := Snapshot(d)
			if !ok {
				return moved, why
			}
			if hostileavoid.Cleared(now, act.Threat, act.BreakOff) {
				d.logf("hostile: clear of the %s — resuming the route from %d,%d,%d", act.Facts.Name, now.X, now.Z, now.Level)
				return true, why
			}
			if now.Level == dest.Level && hostileavoid.Chebyshev(now.X, now.Z, dest.X, dest.Z) <= arrival {
				break
			}
		}
		dist += 4
	}
	d.logf("hostile: still not clear of the %s after %s — resuming the route anyway", act.Facts.Name, deadline)
	return moved, why
}

// walledOffFactor is how many times the break-off distance a hostile's own walking route to the
// account must exceed before walls, not distance, are what stands between them.
const walledOffFactor = 2

// walledOff reports whether threat's own walking route to the account (on the static collision
// pack) is missing or longer than walledOffFactor times breakOff; steps describes it for a log.
func walledOff(pack *pathfind.Pack, snap hostileavoid.Snapshot, threat hostileavoid.NPC, breakOff int) (string, bool) {
	if pack == nil || breakOff <= 0 {
		return "", false
	}
	res := pack.FindPath(pathfind.NavPoint{X: threat.X, Z: threat.Z, Level: snap.Level}, pathfind.NavPoint{X: snap.X, Z: snap.Z, Level: snap.Level})
	if !res.OK {
		return "no route", true
	}
	n := routeSteps(res.Waypoints)
	return fmt.Sprintf("%d steps", n), n > walledOffFactor*breakOff
}

// routeSteps is the walking length of a corner-compressed route.
func routeSteps(w []pathfind.NavPoint) int {
	n := 0
	for i := 1; i < len(w); i++ {
		n += hostileavoid.Chebyshev(w[i-1].X, w[i-1].Z, w[i].X, w[i].Z)
	}
	return n
}
