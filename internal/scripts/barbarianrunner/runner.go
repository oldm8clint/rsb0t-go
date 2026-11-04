// Package barbarianrunner is the I/O half of the BarbarianAgility port: it drives the pure
// decisions in internal/scripts/barbarianplan against a live session, the way cmd/barbtest
// proved live first. It is shared by cmd/barbtest (a linear standalone tool) and
// internal/bot's registered "BarbarianAgility" script, so the exact live-proven sequencing —
// including the ropeswing fall-recovery fix (a settle delay before trusting the post-fall
// tile, then a real ladder escape from the fail pit) — lives in exactly one place instead of
// being re-derived or drifting between the two.
//
// Every blocking wait goes through a caller-supplied Delayer instead of time.Sleep directly,
// so a cancellable caller (internal/bot's Context.Delay) can make a running bot actually stop
// mid-wait, while a standalone tool with nothing to cancel can just pass RealDelay.
package barbarianrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/barbarianplan"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// AgilityStat is UPDATE_STAT's index for Agility, checked against the engine's own
// PlayerStat enum (engine/src/engine/entity/PlayerStat.ts) and the client's Skill.ts, which
// agree: attack,defence,strength,hitpoints,ranged,prayer,magic,cooking,woodcutting,
// fletching,fishing,firemaking,crafting,smithing,mining,herblore,agility,...
const AgilityStat = 16

// Delayer sleeps for d, returning false early (before the full duration elapses) if the
// caller should stop instead of true once d has fully elapsed.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration and never cancels — for a standalone tool with
// no runner to request a stop.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one obstacle attempt needs from the outside world.
type Deps struct {
	Session actions.Session
	Pack    *pathfind.Pack
	Locs    *locindex.Index
	Config  *cachecfg.Config
	// Components is the parsed interface archive Bank needs to resolve its deposit-side
	// component; nil makes BankForFood fail clearly rather than opening a bank it can't
	// actually read. Not needed for the rest of BarbarianAgility, only banking.
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	// Log receives one already-formatted line per notable event; nil drops them.
	Log func(string)
	// OnFall, if set, runs once a fall's settle delay has elapsed but before recovery is
	// attempted — cmd/barbtest hooks its ::getcoord diagnostic here; the generalized bot
	// runtime leaves it nil. Purely diagnostic, never load-bearing for correctness.
	OnFall func()
	// Walker, when set, is tried first for the long walks to the course and the bank, as the TS
	// original walks them with Traversal.walkResilient: doors, stairs and (with hops installed)
	// ships. Nil, or a walk it does not finish, falls back to the same-level walker below.
	Walker *webwalk.Walker
}

// walkWeb runs one d.Walker walk. died reports a death during the walk, so the caller leaves it
// to the recover stage instead of retrying with another walker. stopped reports a stop request
// (WalkTo's own OutcomeStopped) — real, live-confirmed distinction this used to lose: every
// non-arrived, non-died outcome (including a plain stop request racing with the walk, e.g. the
// runner's own shutdown or an external stop mid-route — reproduced live this session via
// cmd/barboutpostpockettest, which stops the runner the instant OnCourse first reports true,
// landing squarely mid-walk) used to fall through identically to a genuine pathing failure
// ("webwalk to ... ended stopped (stop requested)"), which TravelToCourse then treated exactly
// like an unreachable route — see its own doc comment for why that specifically matters now
// that it retries this same walker instead of downgrading to one that structurally cannot
// finish the outpost's own gated approach.
func walkWeb(d *Deps, dest tile.Tile, radius int, timeout time.Duration) (arrived, died, stopped bool) {
	res := d.Walker.WalkTo(webwalk.Point{X: dest.X, Z: dest.Z, Level: dest.Level}, webwalk.WalkOptions{
		Radius:  radius,
		Timeout: timeout,
		Combat:  webwalk.CombatOptions{RunWhenAttacked: true, RunBelowHP: 50},
	})
	switch res.Outcome {
	case webwalk.OutcomeArrived:
		return true, false, false
	case webwalk.OutcomeDied:
		d.logf("died on the way to %d,%d,%d", dest.X, dest.Z, dest.Level)
		return false, true, false
	case webwalk.OutcomeStopped:
		return false, false, true
	}
	d.logf("webwalk to %d,%d,%d ended %s (%s)", dest.X, dest.Z, dest.Level, res.Outcome, res.Reason)
	return false, false, false
}

func (d *Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

func currentTile(d *Deps) (tile.Tile, bool) {
	x, z, level, ok := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}, ok
}

func normaliseKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// firstStepNamed finds the first lap index whose obstacle name matches, case-insensitively.
func firstStepNamed(lap []string, name string) (int, bool) {
	for i, n := range lap {
		if normaliseKey(n) == normaliseKey(name) {
			return i, true
		}
	}
	return 0, false
}

// waitFor polls cond every 100ms via d.Delay until it's true or timeout elapses, returning
// false immediately (without logging a timeout) if d.Delay reports a stop was requested.
func waitFor(d *Deps, what string, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(100 * time.Millisecond) {
			return false
		}
	}
	d.logf("timed out waiting for %s", what)
	return false
}

// guardianWaitLogEvery bounds how often WaitForGuardian logs while still waiting — once
// immediately, then at most this often, instead of once per 500ms poll. A real production
// test found an "event monster" evade event take over two minutes to actually clear
// (see randomevents.HandleEvade's own doc comment for the fix to the clearing itself);
// logging "waiting" every single poll for that whole stretch produced hundreds of near-
// identical lines and buried the handler's own "still present after handling"/"cleared"
// lines that actually mattered. This is purely a logging change — the poll cadence itself
// (and therefore how quickly a resumed action actually happens once clear) is unchanged.
const guardianWaitLogEvery = 10 * time.Second

// emergencyFleeHome/emergencyFleeDriftTiles are BarbarianAgility's own EmergencyFlee
// reference and threshold — see randomevents.EmergencyFlee's own doc comment for why every
// caller now supplies its own pair instead of this package assuming every script operates
// near Catherby (the previous hardcoded default, ~2000+ tiles from this course, made every
// single poll here a standing false positive).
//
// barbarianplan.DefaultBank, not a point on the course itself, because a barbarian's own
// operating area spans BOTH the bank AND barbarianplan.CourseBox (2526-2560, 3540-3566) —
// worst case 199 tiles apart (the bank to the course's own western edge, x=2526) — so
// driftTiles is 220, not fisher's 150, to comfortably cover the whole course from the bank
// without narrowing the net for a genuine fail-teleport, which lands far outside either.
var emergencyFleeHome = pathfind.NavPoint{X: barbarianplan.DefaultBank.X, Z: barbarianplan.DefaultBank.Z, Level: barbarianplan.DefaultBank.Level}

const emergencyFleeDriftTiles = 220

// WaitForGuardian pauses while a random event is detected at all — pending or already being
// handled elsewhere — so the caller never starts a new loc interaction on top of one. Returns
// false if a stop was requested while waiting.
//
// Uses randomevents.Busy rather than raw Detect — see fisherrunner.WaitForGuardian's own
// doc comment for the real live gap this closes (Detect() alone can't tell an event
// Guardian has genuinely given up on for now from one it's still actively working).
func WaitForGuardian(d *Deps) bool {
	randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
	if !randomevents.Busy(d.Session) {
		return true
	}
	d.logf("[guardian] a random event is present; waiting")
	lastLog := time.Now()
	for randomevents.Busy(d.Session) {
		randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
		if !d.Delay(500 * time.Millisecond) {
			return false
		}
		if time.Since(lastLog) >= guardianWaitLogEvery {
			d.logf("[guardian] still waiting on a random event")
			lastLog = time.Now()
		}
	}
	return true
}

// FindObstacle tries each of the obstacle's preferred ops in turn (an obstacle can offer more
// than one forward op, e.g. the net's Climb-over/Climb-up), falling back to any op when the
// name carries no preference, matching HasForwardOp/PickOp's own precedence.
func FindObstacle(d *Deps, name string, near tile.Tile, radius int) (locindex.Match, bool) {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	ops := barbarianplan.PreferredOps[normaliseKey(name)]
	for _, op := range ops {
		if m, ok := d.Locs.Nearest(d.Config, locindex.Query{X: near.X, Z: near.Z, Level: near.Level, Radius: radius, Name: name, Op: op}, live); ok {
			return m, true
		}
	}
	return d.Locs.Nearest(d.Config, locindex.Query{X: near.X, Z: near.Z, Level: near.Level, Radius: radius, Name: name}, live)
}

// ResyncToNearest ports the original TS driver's own recovery for this exact situation
// (BarbarianAgility.ts's resyncToNearest/resyncTo): when the lap's current step can't be
// found or reached at all — most commonly a session that starts, or a fall recovery that
// lands, somewhere other than where the lap expects — search every distinct obstacle name in
// the whole lap (not just the current step's) for whichever one is actually near the
// account's real current position, then jump the lap to that specific occurrence's index
// instead of endlessly retrying an obstacle nowhere nearby. Mirrors the original exactly:
// first find which obstacle *name* is closest by an unanchored (player-centered) query, then
// pick whichever *occurrence* of that name has the nearest declared start tile — because a
// repeated name (the three crumbling walls) has one tile per occurrence and the wide search
// only tells us the name, not which placement.
func ResyncToNearest(d *Deps, lap []string) (idx int, ok bool) {
	here, hereOK := currentTile(d)
	if !hereOK {
		return 0, false
	}
	seen := map[string]bool{}
	bestName := ""
	bestDist := -1
	for _, name := range lap {
		key := normaliseKey(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		m, found := FindObstacle(d, name, here, barbarianplan.SearchRadius)
		if !found {
			continue
		}
		dist := tile.Chebyshev(here, tile.Tile{X: m.X, Z: m.Z, Level: here.Level})
		if bestName == "" || dist < bestDist {
			bestName, bestDist = name, dist
		}
	}
	if bestName == "" {
		return 0, false
	}
	return ResyncTo(lap, bestName, here)
}

// ResyncTo finds the lap index for name whose own declared start tile is nearest here — when
// name repeats (e.g. the three crumbling walls), the specific occurrence closest to the
// account's current position, not necessarily the first one in the lap.
func ResyncTo(lap []string, name string, here tile.Tile) (idx int, ok bool) {
	bestIdx, bestDist := -1, -1
	for i, n := range lap {
		if normaliseKey(n) != normaliseKey(name) {
			continue
		}
		start, ok := barbarianplan.StartTileFor(n, barbarianplan.OccurrenceOf(lap, i))
		if !ok {
			continue
		}
		if dist := tile.Chebyshev(here, start); bestIdx == -1 || dist < bestDist {
			bestIdx, bestDist = i, dist
		}
	}
	if bestIdx == -1 {
		return 0, false
	}
	return bestIdx, true
}

// WalkTo paths to dest and walks there (never running — a known position-tracking drift bug
// on running clicks is documented in COLLAB.md and not yet fixed), re-clicking from wherever
// the player stops since one click carries at most 25 corners. Returns false on a stop
// request, an unreachable destination, or a 30s overall timeout.
func WalkTo(d *Deps, dest tile.Tile) bool {
	return walkToDeadline(d, dest, time.Now().Add(30*time.Second))
}

func walkToDeadline(d *Deps, dest tile.Tile, deadline time.Time) bool {
	for time.Now().Before(deadline) {
		x, z, level, ok := d.Session.State().Tile()
		if ok && x == dest.X && z == dest.Z && level == dest.Level {
			return true
		}
		if !ok {
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			continue
		}
		res := d.Pack.FindPath(pathfind.NavPoint{X: x, Z: z, Level: level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level})
		if !res.OK {
			d.logf("no path from %d,%d,%d to %d,%d,%d: %s", x, z, level, dest.X, dest.Z, dest.Level, res.Reason)
			return false
		}
		if len(res.Waypoints) < 2 {
			return true
		}
		wp := res.Waypoints[len(res.Waypoints)-1]
		d.logf("walk from %d,%d toward %d,%d (%d corners)", x, z, wp.X, wp.Z, len(res.Waypoints))
		if err := actions.Walk(d.Session, res.Waypoints, false); err != nil {
			d.logf("walk: %v", err)
			return false
		}
		lastX, lastZ, still := x, z, time.Now()
		for time.Now().Before(deadline) {
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			cx, cz, cl, _ := d.Session.State().Tile()
			if cx == dest.X && cz == dest.Z && cl == dest.Level {
				return true
			}
			if cx != lastX || cz != lastZ {
				lastX, lastZ, still = cx, cz, time.Now()
			} else if time.Since(still) > 2*time.Second {
				break
			}
		}
	}
	x, z, level, _ := d.Session.State().Tile()
	d.logf("gave up walking; stopped at %d,%d,%d", x, z, level)
	return false
}

// TeleTo sends ::tele for a tile, using the local engine's level,mapsquareX,mapsquareZ,
// localX,localZ format. Local-engine-only.
func TeleTo(d *Deps, t tile.Tile) error {
	return actions.Cheat(d.Session, fmt.Sprintf("tele %d,%d,%d,%d,%d", t.Level, t.X>>6, t.Z>>6, t.X&0x3f, t.Z&0x3f))
}

// CheatSetup bypasses the course's real level-35 gate check and positions the account at the
// pipe's north stand via ::setstat/::tele, exactly like cmd/barbtest's non-live mode.
// Local-engine-only; never call this against a real account.
func CheatSetup(d *Deps, agilityLevel int) error {
	d.logf("::setstat agility %d (bypassing the course's level-35 gate check)", agilityLevel)
	if err := actions.Cheat(d.Session, fmt.Sprintf("setstat agility %d", agilityLevel)); err != nil {
		return err
	}
	if !d.Delay(500 * time.Millisecond) {
		return nil
	}
	d.logf("::tele to the pipe's north stand (%d,%d,%d)", barbarianplan.PipeNorth.X, barbarianplan.PipeNorth.Z, barbarianplan.PipeNorth.Level)
	if err := TeleTo(d, barbarianplan.PipeNorth); err != nil {
		return err
	}
	if !waitFor(d, "arrive at the pipe", 8*time.Second, func() bool {
		x, z, _, ok := d.Session.State().Tile()
		return ok && x == barbarianplan.PipeNorth.X && z == barbarianplan.PipeNorth.Z
	}) {
		return fmt.Errorf("did not arrive at the pipe after teleport")
	}
	d.Delay(1500 * time.Millisecond)
	return nil
}

// SqueezeIn works the pipe from its north stand and waits until the tracked tile reads as
// inside the course.
func SqueezeIn(d *Deps) bool {
	// Walk to the pipe's north stand first rather than only searching within
	// SearchRadius of it — without this, an account displaced well outside that radius
	// (an unhandled random event's own teleport, e.g., is a real live-observed cause;
	// see COLLAB.md) could never route back to the course at all. WalkTo is a no-op if
	// already close enough, so this costs nothing in the common case.
	if !WalkTo(d, barbarianplan.PipeNorth) {
		d.logf("could not walk to the pipe's north stand")
		return false
	}
	match, ok := FindObstacle(d, barbarianplan.EntryObstacle, barbarianplan.PipeNorth, barbarianplan.SearchRadius)
	if !ok {
		d.logf("could not find the obstacle pipe near its north stand")
		return false
	}
	d.logf("OPLOC%d on %q %d at %d,%d", match.Op, match.Name, match.ID, match.X, match.Z)
	if err := actions.OpLoc(d.Session, match.Op, match.X, match.Z, match.ID); err != nil {
		d.logf("OpLoc: %v", err)
		return false
	}
	return waitFor(d, "squeeze through the pipe", 8*time.Second, func() bool {
		t, ok := currentTile(d)
		return ok && barbarianplan.InsideCourse(&t)
	})
}

// EnterCourse gets the account from wherever it is onto the course: a no-op if already
// inside, otherwise a squeeze through the pipe. The bool return is false only when a stop was
// requested while waiting on the guardian, not a failure — the error return is the actual
// failure signal.
func EnterCourse(d *Deps) (bool, error) {
	if !WaitForGuardian(d) {
		return false, nil
	}
	here, ok := currentTile(d)
	if ok && barbarianplan.InsideCourse(&here) {
		d.logf("already inside the course")
		return true, nil
	}
	// Ports the original TS driver's own stage split (BarbarianAgility.ts's decideStage:
	// "anywhere off the course walks back to it [travel], the outside of the pipe
	// squeezes in [enter]") rather than going straight for the pipe: an account can end
	// up genuinely far from the whole outpost — an unhandled random event's own
	// teleport is a real, live-observed cause (see COLLAB.md's ProgressiveCrafter
	// findings) — and SqueezeIn's own search only looks within SearchRadius of the
	// pipe, so it can never route back from far away on its own.
	if !ok || !barbarianplan.OnCourse(&here) {
		if !TravelToCourse(d) {
			return false, fmt.Errorf("could not travel back to the course")
		}
	}
	if !SqueezeIn(d) {
		return false, fmt.Errorf("could not squeeze through the pipe")
	}
	d.logf("inside the course")
	return true, nil
}

// TravelToCourse walks back to the course entry from wherever the account actually is,
// ports the TS original's own travelToCourse: a resilient long-distance walk (multiple
// attempts, a generous overall budget) rather than the short, single-attempt walk
// EnterCourse/SqueezeIn use once already in the outpost's general area. The TS original's own
// walk (Traversal.walkResilient) is a full transport-graph route that silently boards ships as
// ordinary graph edges — internal/pathfind has no such support (the documented "v1: no
// doors/transports" scope), so a plain same-level walk alone can never recover a genuine
// "stranded across water" displacement (a death respawn near Lumbridge, confirmed live
// production-blocking — see COLLAB.md). Once the same-level attempts above are exhausted, this
// checks for exactly that situation (strandedAcrossWater) and, only then, falls back to
// travelViaFreshStart — the same freshstartrunner primitive ProgressiveFisher/ProgressiveCrafter
// already use for their own first-day travel, built generic and script-agnostic anticipating
// this exact use. A displacement to a different level, or a genuinely disconnected area that
// isn't across water at all, still fails outright — not something this function invents a fix
// for.
//
// d.Walker's own webwalk attempt is retried up to `attempts` times before this ever falls to
// the plain same-level walker below, not just tried once — real, live-confirmed dead end this
// closes (found and reproduced this session, teleporting a throwaway account to the exact
// reported fall-recovery position, 2809,3441,0, via cmd/barboutpostpockettest and watching a
// real live run): the outpost's own course entry sits behind at least one real closed Gate
// (confirmed against the real collision pack + static loc index via cmd/doordiag/webwalkplan —
// e.g. "Gate" id=2116 @ 2545,3569, `Open` action), which internal/pathfind's own same-level
// FindPath (the plain walker's only tool, no doors/transports in its documented v1 scope) can
// never open — a plain walk toward barbarianplan.PipeNorth from outside is not "harder", it is
// mathematically unreachable, confirmed offline (`go run ./cmd/doordiag ... -x 2754 -z 3427
// -tx 2552 -tz 3561`: unreachable, 109716 tiles expanded, the account's own entire raw-walkable
// component never reaching the gated pipe stand at all) every single time, no matter how many
// attempts or how much budget it is given. d.Walker (internal/webwalk), by contrast, already
// knows how to open exactly this class of door as an ordinary graph edge — confirmed live this
// session: a real run from 2809,3441,0 crossed a "Log balance" agility shortcut and two real
// Gates and walked itself onto the course, no cheats, only OpLoc/movement any real player could
// send. A single webwalk attempt that stalls (a repath budget hit, a random event, an ordinary
// live hiccup over a 200+ tile route — none of which make the destination actually unreachable)
// used to permanently downgrade every following attempt to the one walker that can never
// finish; retrying webwalk itself instead means a transient stall gets a fresh, re-planned shot
// with the tool that can actually open the gate, and only a real death, stop request, or
// exhausting every one of these `attempts` retries falls through to the historical same-level
// walker + strandedAcrossWater safety net below (unchanged, for callers with no Walker
// configured at all, and as a last-resort net for whatever this still doesn't cover).
func TravelToCourse(d *Deps) bool {
	const attempts = 4
	const perAttempt = 75 * time.Second // ~300s total across 4 attempts, matching the TS original's own overall budget.
	if d.Walker != nil {
		for i := 0; i < attempts; i++ {
			if here, ok := currentTile(d); ok && barbarianplan.OnCourse(&here) {
				return true
			}
			// TS: walkResilient(COURSE_ENTRY, {radius: 2, timeoutMs: 300000}).
			arrived, died, stopped := walkWeb(d, barbarianplan.CourseEntry, 2, 5*time.Minute)
			if arrived {
				return true
			}
			if died || stopped || d.Session.State().IsDead() {
				// A stop request is not a failure (see this function's own doc comment) —
				// propagate it immediately rather than burning the remaining webwalk
				// attempts or falling through to the same-level walker's own budget on a
				// route nothing was actually wrong with.
				return false
			}
			d.logf("webwalk attempt %d/%d toward the course entry did not arrive — retrying", i+1, attempts)
		}
		d.logf("falling back to the same-level walker for the course entry")
	}
	for i := 0; i < attempts; i++ {
		if here, ok := currentTile(d); ok && barbarianplan.OnCourse(&here) {
			return true
		}
		if walkToDeadline(d, barbarianplan.PipeNorth, time.Now().Add(perAttempt)) {
			return true
		}
		d.logf("could not reach the course entry — retrying (attempt %d/%d)", i+1, attempts)
	}
	here, ok := currentTile(d)
	if ok && barbarianplan.OnCourse(&here) {
		return true
	}
	if ok && strandedAcrossWater(here) {
		d.logf("stranded on the wrong side of the water at %d,%d,%d — falling back to freshstartrunner's pickpocket+boat route", here.X, here.Z, here.Level)
		return travelViaFreshStart(d)
	}
	return false
}

// strandedAcrossWater reports whether here is on the mainland side of the Port Sarim<->Karamja
// ship route — freshstart.TravelLeg's own LegPier, the same test fisherplan/crafterplan use to
// decide whether a boat leg is still ahead of them. The Barbarian Outpost's own tile (PipeNorth,
// X=2552) is already on freshstart's "Ardougne side" (west of ArdougneSideMaxX=2740), so this
// being true specifically means genuinely stranded across water from the outpost — the exact
// "died and respawned near Lumbridge" scenario a same-level walk can never reach from — rather
// than an ordinary nearby pathfind hiccup (which would read as LegDestination, since it's on the
// same side as the outpost, and should just keep retrying the plain walk instead).
func strandedAcrossWater(here tile.Tile) bool {
	return freshstart.TravelLeg(here) == freshstart.LegPier
}

// freshStartDeps adapts Deps to freshstartrunner.Deps: every field freshstartrunner needs is
// already carried by Deps under an identical name and type — this project's own established
// convention, every runner package built as a sibling of the others — so this is a plain
// field-for-field copy, not new plumbing.
func freshStartDeps(d *Deps) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay), Log: d.Log,
	}
}

// freshStartCoins counts coins in the player's pack — barbarianrunner's own copy of
// freshstartrunner's identical private helper, duplicated per this project's established
// per-package convention (see e.g. WalkNear's own doc comment) rather than exported and shared.
func freshStartCoins(d *Deps) int {
	n := 0
	for _, it := range d.Session.State().Inventory(PlayerPack).NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if ok && strings.EqualFold(obj.Name, freshstart.Coins) {
			c := it.Count
			if c < 1 {
				c = 1
			}
			n += int(c)
		}
	}
	return n
}

// anyObjIDIn reports whether items holds any of ids.
func anyObjIDIn(items []gamestate.SlotItem, ids []int) bool {
	for _, it := range items {
		for _, id := range ids {
			if int(it.ID) == id {
				return true
			}
		}
	}
	return false
}

// freshStartSnapshot builds the freshstart.Snapshot barbarianplan.DecidePhase reads for
// travelViaFreshStart, from live state. Every other Snapshot field (Crafting, HasShears, Wool,
// Balls, Sellable, PackFull, DropAt, BrightPending) is deliberately left at its zero value:
// barbarianplan.Road()'s WoolUntil is 0, so freshstart.DecidePhase's wool-loop branch — the only
// code that reads any of them — is never taken (see Road's own doc comment).
func freshStartSnapshot(d *Deps) freshstart.Snapshot {
	main, _, _ := d.Session.State().Modals()
	s := freshstart.Snapshot{MainModal: main, Fare: freshstart.DefaultFare, SkirtCost: freshstart.DefaultSkirtCost}
	if t, ok := currentTile(d); ok {
		s.Tile = &t
	}
	s.Coins = freshStartCoins(d)
	eq := actions.NewEquipment(d.Session, d.Components)
	ids := d.Config.ObjIDs(freshstart.PinkSkirt)
	s.SkirtWorn = anyObjIDIn(eq.Worn(), ids)
	s.SkirtHeld = anyObjIDIn(eq.Inventory(), ids)
	return s
}

// travelViaFreshStart is TravelToCourse's own fallback for a genuine stranded-across-water
// displacement (see strandedAcrossWater) that a plain same-level walk can never recover from.
// A thin wrapper over travelViaFreshStartTo aimed at the course's own pipe stand — see that
// function's doc comment for how the fallback itself works and BankForFood's own call site
// below for the second destination this same machinery now serves.
func travelViaFreshStart(d *Deps) bool {
	return travelViaFreshStartTo(d, barbarianplan.PipeNorth,
		func() bool {
			here, ok := currentTile(d)
			return ok && barbarianplan.OnCourse(&here)
		},
		func() bool {
			return walkToDeadline(d, barbarianplan.PipeNorth, time.Now().Add(perAttemptFreshStart))
		},
	)
}

// travelViaFreshStartToBank is BankForFood's own fallback for the identical stranded-across-
// water gap TravelToCourse used to have: a food-depleted account that respawns near Lumbridge
// hits internal/bot/barbarian.go's DecideStage priority (Banking/NeedsBankTrip is checked
// before !OnCourse) and gets stuck retrying a same-level walk to barbarianplan.DefaultBank
// (Seers' Village, across the same stretch of water as the course) forever — never even
// reaching StageTravel/StageEnter, where travelViaFreshStart above would have helped. Found
// live while verifying TravelToCourse's own fix, not by reproducing the bank case itself:
// a real production account (already carrying food) wouldn't hit this, but a genuinely
// food-depleted one would. Seers' Village sits on the same "Ardougne side" of the Port
// Sarim<->Karamja crossing as the outpost (west of freshstart.ArdougneSideMaxX), so the exact
// same fund/skirt/sail sequence applies — only the final destination and "have we arrived"
// check differ (WalkNear's radius tolerance, matching BankForFood's own travelResilient
// radius, rather than the course's exact-tile OnCourse check).
func travelViaFreshStartToBank(d *Deps, bankTile tile.Tile, radius int) bool {
	return travelViaFreshStartTo(d, bankTile,
		func() bool {
			here, ok := currentTile(d)
			return ok && tile.Chebyshev(here, bankTile) <= radius
		},
		func() bool {
			return WalkNear(d, bankTile, radius)
		},
	)
}

// travelViaFreshStartTo is the shared freshstartrunner-driven long-distance recovery loop
// behind both travelViaFreshStart (the course) and travelViaFreshStartToBank (the food bank):
// drives internal/scripts/freshstartrunner — built generic and script-agnostic exactly
// anticipating more than one destination (see that package's own doc comment) — against
// barbarianplan.Road(): pickpocket Men for the fare, buy and wear a pink skirt in Varrock (a
// real, in-game requirement for clearing the Port Sarim customs check to Karamja in this era,
// not a script invention — see freshstartrunner.BuySkirtStep's own doc comment), then sail
// Port Sarim -> Karamja -> Brimhaven -> Ardougne -> toward dest. Bounded at 20 minutes total,
// deliberately more generous than a plain same-level walk's own ~300s budget: this covers
// genuine cross-map travel, and freshstartrunner.SailLeg's own single WalkNear attempt alone is
// bounded at 10 minutes. arrived reports whether dest has already been reached (checked before
// spending an iteration on freshstart's own phase machine, and after each PhaseTravel leg via
// its own loop-top check); finalWalk is the short last-mile walk PhaseArrived attempts once
// freshstart's own state machine considers the crossing complete — an exact-tile walk for the
// course's pipe stand, a radius-tolerant WalkNear for a bank booth.
func travelViaFreshStartTo(d *Deps, dest tile.Tile, arrived func() bool, finalWalk func() bool) bool {
	fd := freshStartDeps(d)
	thief := freshstartrunner.NewThief()
	sail := freshstartrunner.NewSailProgress()
	deadline := time.Now().Add(20 * time.Minute)
	for time.Now().Before(deadline) {
		if arrived() {
			return true
		}
		if !WaitForGuardian(d) {
			return false
		}
		snap := freshStartSnapshot(d)
		phase := barbarianplan.DecidePhase(snap)
		d.logf("freshstart fallback: %s", barbarianplan.DescribePhase(phase))
		switch phase {
		case freshstart.PhaseFund:
			freshstartrunner.PickpocketStep(fd, thief, freshstartrunner.PickpocketOpts{
				Target: freshstart.DefaultPickpocketTarget, Spot: freshstart.PickpocketSpot,
				RestBelow: freshstart.RestBelowHP, Need: freshstart.FundTarget(snap),
			})
		case freshstart.PhaseSkirt:
			freshstartrunner.BuySkirtStep(fd)
		case freshstart.PhaseTravel:
			freshstartrunner.SailLeg(fd, dest, sail)
		case freshstart.PhaseArrived:
			if finalWalk() {
				return true
			}
			d.logf("freshstart fallback: close to the destination but could not finish the walk — retrying")
			if !d.Delay(3 * time.Second) {
				return false
			}
		default:
			// PhaseAccept/PhaseTutorial/PhaseBright/PhaseDrop/PhaseStore/PhaseWool never
			// apply here: a live account running BarbarianAgility is long past its first
			// day, and barbarianplan.Road()'s WoolUntil=0 skips the wool branch entirely
			// (freshstart.DecidePhase). Treated as a brief wait rather than a silent
			// stall, in case that state machine is ever extended.
			d.logf("freshstart fallback: unexpected phase %q — waiting", phase)
			if !d.Delay(2 * time.Second) {
				return false
			}
		}
	}
	d.logf("freshstart fallback: gave up after 20 minutes")
	return false
}

// perAttemptFreshStart is travelViaFreshStart's own PhaseArrived walk budget, split out as a
// named constant since it's used nowhere else and 75s (matching TravelToCourse's own
// per-attempt budget) is otherwise a bare magic number at the call site.
const perAttemptFreshStart = 75 * time.Second

// WalkNear paths to dest and walks there, stopping once within radius tiles of dest —
// used for the bank, matching the TS original's own `Traversal.walkTo(..., {radius: ...})`
// tolerance rather than WalkTo's exact-tile match (obstacles need the exact standing
// tile; the bank booth doesn't). Ported from fisherrunner.WalkNear rather than shared
// across packages, matching this codebase's existing convention of each script runner
// keeping its own small helpers.
func WalkNear(d *Deps, dest tile.Tile, radius int) bool {
	// 120s, not 30s, matching crafterrunner's own walkDeadline (which already carries this
	// identical fix from an earlier session) — see fisherrunner.WalkNear's own
	// walkNearTimeout doc comment for the live production evidence this was raised from
	// (a genuinely long route legitimately takes longer than 30s at walking pace, not
	// just occasionally).
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		here, ok := currentTile(d)
		if ok && tile.Chebyshev(here, dest) <= radius {
			return true
		}
		if !ok {
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			continue
		}
		res := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level})
		if !res.OK {
			d.logf("no path from %d,%d,%d to %d,%d,%d: %s", here.X, here.Z, here.Level, dest.X, dest.Z, dest.Level, res.Reason)
			return false
		}
		if len(res.Waypoints) < 2 {
			return true
		}
		wp := res.Waypoints[len(res.Waypoints)-1]
		d.logf("walk from %d,%d toward %d,%d (%d corners)", here.X, here.Z, wp.X, wp.Z, len(res.Waypoints))
		if err := actions.Walk(d.Session, res.Waypoints, false); err != nil {
			d.logf("walk: %v", err)
			return false
		}
		lastX, lastZ, still := here.X, here.Z, time.Now()
		for time.Now().Before(deadline) {
			// EmergencyFlee here, not just in WaitForGuardian — found during a repo-wide
			// audit following tonight's incident response (same gap as
			// fisherrunner.WalkNear's own identical fix, see its doc comment): this loop
			// can hold the account mid-walk for this whole function's deadline with
			// nothing checking combat or drift in between, since WaitForGuardian only
			// runs before/after a WalkNear call, never during one. Safe to call every
			// 200ms tick — EmergencyFlee's own TryLock already makes it a no-op whenever
			// Guardian's kick() owns the action lock instead.
			randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			cur, ok := currentTile(d)
			if ok && tile.Chebyshev(cur, dest) <= radius {
				return true
			}
			if ok && (cur.X != lastX || cur.Z != lastZ) {
				lastX, lastZ, still = cur.X, cur.Z, time.Now()
			} else if time.Since(still) > 2*time.Second {
				break
			}
		}
	}
	here, _ := currentTile(d)
	d.logf("gave up walking near %d,%d,%d; stopped at %d,%d,%d", dest.X, dest.Z, dest.Level, here.X, here.Z, here.Level)
	return false
}

// travelResilient is a radius-tolerant counterpart to TravelToCourse's own multi-attempt,
// generous-budget walk (see that function's doc comment) — used by BankForFood, which
// can start arbitrarily far from the bank (anywhere on or off the course) and only needs
// to get close, not land on one exact tile the way TravelToCourse's pipe-approach does.
func travelResilient(d *Deps, dest tile.Tile, radius int) bool {
	const attempts = 4
	const perAttempt = 75 * time.Second
	for i := 0; i < attempts; i++ {
		if here, ok := currentTile(d); ok && tile.Chebyshev(here, dest) <= radius {
			return true
		}
		if walkNearDeadline(d, dest, radius, time.Now().Add(perAttempt)) {
			return true
		}
		d.logf("could not reach %d,%d,%d — retrying (attempt %d/%d)", dest.X, dest.Z, dest.Level, i+1, attempts)
		// A genuinely unreachable destination (no baked route at all, e.g.
		// internal/pathfind's documented lack of door/transport routing) fails
		// walkNearDeadline instantly rather than consuming its own deadline — without
		// this delay, all 4 attempts complete within the same tick and this function
		// returns as fast as the CPU allows instead of actually spacing retries out.
		if !d.Delay(2 * time.Second) {
			return false
		}
	}
	here, ok := currentTile(d)
	return ok && tile.Chebyshev(here, dest) <= radius
}

// walkNearDeadline is WalkNear's inner loop with a caller-supplied deadline, so
// travelResilient can retry it under a shared overall budget without WalkNear's own
// fixed 30s cap forcing an early give-up on the first attempt.
func walkNearDeadline(d *Deps, dest tile.Tile, radius int, deadline time.Time) bool {
	for time.Now().Before(deadline) {
		here, ok := currentTile(d)
		if ok && tile.Chebyshev(here, dest) <= radius {
			return true
		}
		if !ok {
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			continue
		}
		res := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level})
		if !res.OK {
			d.logf("no path from %d,%d,%d to %d,%d,%d: %s", here.X, here.Z, here.Level, dest.X, dest.Z, dest.Level, res.Reason)
			return false
		}
		if len(res.Waypoints) < 2 {
			return true
		}
		wp := res.Waypoints[len(res.Waypoints)-1]
		d.logf("walk from %d,%d toward %d,%d (%d corners)", here.X, here.Z, wp.X, wp.Z, len(res.Waypoints))
		if err := actions.Walk(d.Session, res.Waypoints, false); err != nil {
			d.logf("walk: %v", err)
			return false
		}
		lastX, lastZ, still := here.X, here.Z, time.Now()
		for time.Now().Before(deadline) {
			// EmergencyFlee here too — see WalkNear's own identical fix just above (this
			// is its shared-deadline sibling, used by travelResilient's own retry loop);
			// same repo-wide-audit finding, same reasoning.
			randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			cur, ok := currentTile(d)
			if ok && tile.Chebyshev(cur, dest) <= radius {
				return true
			}
			if ok && (cur.X != lastX || cur.Z != lastZ) {
				lastX, lastZ, still = cur.X, cur.Z, time.Now()
			} else if time.Since(still) > 2*time.Second {
				break
			}
		}
	}
	return false
}

// PlayerPack is the persistent inventory-tab component id — the same confirmed-stable
// literal (3214) fisherrunner.PlayerPack uses, per that constant's own doc comment.
const PlayerPack = 3214

// FoodCount counts every pack slot holding any form of foodName (see
// fighterfood.IsFoodItem — multi-bite foods' partial forms all count).
// A pack not yet synced (no UPDATE_INV_FULL since login) or no config archive counts as
// none rather than panicking.
func FoodCount(d *Deps, foodName string) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if ok && fighterfood.IsFoodItem(obj.Name, foodName) {
			n++
		}
	}
	return n
}

// NeedEat reports whether to eat right now, per fighterfood.ShouldEat (ported directly
// from the TS original's own needEat(), which calls the equivalent shared shouldEat()).
func NeedEat(d *Deps, foodName string, mode fighterfood.EatMode, thresholdFrac float64) bool {
	hp, hpOK := d.Session.State().HP()
	maxHP, maxOK := d.Session.State().MaxHP()
	if !hpOK || !maxOK {
		return false
	}
	return fighterfood.ShouldEat(fighterfood.EatInput{
		HP: hp, MaxHP: maxHP, Heal: fighterfood.FoodHealAmount(foodName),
		FoodCount: FoodCount(d, foodName), Mode: mode, ThresholdFrac: thresholdFrac,
	})
}

// EatOnce finds one pack slot holding foodName (any form) and eats it (actions.Eat, food's
// own OPHELD1 "Eat"), waiting for HP to actually rise before reporting success — matching
// the TS original's eatOnce(), which does the same "click, then confirm hp increased"
// check rather than trusting the click alone.
func EatOnce(d *Deps, foodName string) bool {
	var slot int = -1
	var objID int32
	for _, it := range d.Session.State().Inventory(PlayerPack).NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if ok && fighterfood.IsFoodItem(obj.Name, foodName) {
			slot, objID = it.Slot, it.ID
			break
		}
	}
	if slot < 0 {
		d.logf("no %q in the pack to eat", foodName)
		return false
	}
	before, ok := d.Session.State().HP()
	if !ok {
		return false
	}
	if err := actions.Eat(d.Session, objID, slot, PlayerPack); err != nil {
		d.logf("eat: %v", err)
		return false
	}
	ate := waitFor(d, "hp to rise after eating", 3*time.Second, func() bool {
		hp, ok := d.Session.State().HP()
		return ok && hp > before
	})
	if ate {
		d.logf("ate a %s", foodName)
	}
	return ate
}

// BankForFood walks to the bank (a resilient long-distance walk, matching the TS
// original's own bankForFood — this can be called from anywhere, not just near the
// bank), deposits everything in the pack (matching the TS original's own
// Bank.depositInventory() — the course drops no other loot worth keeping), withdraws up
// to target foodName, and closes. Returns the pack's foodName count after the trip and
// whether the trip itself succeeded (finding/opening the bank) — a trip that succeeds
// but finds no foodName in the bank still returns ok=true with a low count, matching the
// TS original's own "warn and wait" handling of that case one level up, in the caller.
func BankForFood(d *Deps, bankTile tile.Tile, foodName string, target int) (count int, ok bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return FoodCount(d, foodName), false
	}
	d.logf("walking to the bank (%d,%d)", bankTile.X, bankTile.Z)
	reached := false
	if d.Walker != nil {
		// TS: walkResilient(stand, {radius: 3, attempts: 4, timeoutMs: 300000}).
		arrived, died, stopped := walkWeb(d, bankTile, 3, 5*time.Minute)
		if died || stopped || d.Session.State().IsDead() {
			// stopped (see walkWeb's own doc comment): a stop request is not a failure —
			// return immediately rather than spending travelResilient's own budget on a
			// route nothing was actually wrong with.
			return FoodCount(d, foodName), false
		}
		reached = arrived
	}
	if !reached && !travelResilient(d, bankTile, 4) {
		// Same gap TravelToCourse used to have (see travelViaFreshStartToBank's own doc
		// comment): a food-depleted account stranded across water from the outpost can
		// never reach Seers' Village via a plain same-level walk, and DecideStage checks
		// for a bank trip before it checks !OnCourse, so this path can be reached first.
		here, ok := currentTile(d)
		if !ok || !strandedAcrossWater(here) {
			return FoodCount(d, foodName), false
		}
		d.logf("stranded on the wrong side of the water at %d,%d,%d — falling back to freshstartrunner's pickpocket+boat route", here.X, here.Z, here.Level)
		if !travelViaFreshStartToBank(d, bankTile, 4) {
			return FoodCount(d, foodName), false
		}
	}
	x, z, level, _ := d.Session.State().Tile()
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	booth, found := d.Locs.Nearest(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: "Bank booth", Op: "Use-quickly"}, live)
	if !found {
		d.logf("could not find a bank booth near the bank")
		return FoodCount(d, foodName), false
	}
	// Onto a tile the booth answers from: the server ignores a click from a diagonal one in
	// silence (freshstartrunner.WalkToUse).
	if !freshstartrunner.WalkToUse(freshStartDeps(d), tile.Tile{X: booth.X, Z: booth.Z, Level: booth.Level}, 120*time.Second) {
		return FoodCount(d, foodName), false
	}
	if err := actions.OpLoc(d.Session, booth.Op, booth.X, booth.Z, booth.ID); err != nil {
		d.logf("OpLoc: %v", err)
		return FoodCount(d, foodName), false
	}
	bank := actions.NewBank(d.Session, d.Components)
	if !waitFor(d, "bank to open", 8*time.Second, bank.IsOpen) {
		return FoodCount(d, foodName), false
	}
	defer bank.Close()
	if !d.Delay(500 * time.Millisecond) {
		return FoodCount(d, foodName), false
	}

	d.logf("depositing everything")
	seen := map[int32]bool{}
	for _, it := range bank.Inventory() {
		if seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		if err := bank.DepositAll(it.ID); err != nil {
			d.logf("deposit %d: %v", it.ID, err)
		}
	}
	if !d.Delay(500 * time.Millisecond) {
		return FoodCount(d, foodName), false
	}

	need := barbarianplan.FoodToWithdraw(FoodCount(d, foodName), target)
	if need > 0 {
		ids := d.Config.ObjIDs(foodName)
		var foodID int32 = -1
		var banked int
		for _, id := range ids {
			if c := bankCountByID(bank.Items(), int32(id)); c > banked {
				foodID, banked = int32(id), c
			}
		}
		if foodID >= 0 && banked > 0 {
			take := need
			if take > banked {
				take = banked
			}
			d.logf("withdrawing %d %s (bank has %d)", take, foodName, banked)
			for take >= 10 {
				if err := bank.WithdrawTen(foodID); err != nil {
					d.logf("withdraw ten: %v", err)
					break
				}
				take -= 10
			}
			for take >= 5 {
				if err := bank.WithdrawFive(foodID); err != nil {
					d.logf("withdraw five: %v", err)
					break
				}
				take -= 5
			}
			for take > 0 {
				if err := bank.WithdrawOne(foodID); err != nil {
					d.logf("withdraw one: %v", err)
					break
				}
				take--
			}
			waitFor(d, "food to appear in the pack", 3*time.Second, func() bool { return FoodCount(d, foodName) > 0 })
		} else {
			d.logf("the bank has no %q", foodName)
		}
	}
	return FoodCount(d, foodName), true
}

// bankCountByID sums a bank item's count across every slot holding it, matching
// fisherrunner's own identical helper (bank items don't stack visually the way a pack's
// noted items might, so summing across slots is the correct count either way).
func bankCountByID(items []gamestate.SlotItem, id int32) int {
	var n int32
	for _, it := range items {
		if it.ID == id {
			n += it.Count
		}
	}
	return int(n)
}

// RunObstacle walks to the obstacle's standing tile, finds it through the static loc index
// (name and forward op, within the course), works it, and waits for a settle signal: agility
// xp gained, a no-xp obstacle's plane change, or a chat line telling us it failed. HP/food and
// the "interrupted" signal are out of scope, same as cmd/barbtest's own proof.
func RunObstacle(d *Deps, name string, occurrence int) barbarianplan.SettleReason {
	startTile, ok := barbarianplan.StartTileFor(name, occurrence)
	if !ok {
		d.logf("no start tile known for %q", name)
		return barbarianplan.SettleTimeout
	}
	if !WalkTo(d, startTile) {
		// Genuinely couldn't get there (unreachable, or the pack/session hiccuped) —
		// this is the "not found/not reachable" case ResyncToNearest exists for, distinct
		// from a click that fired but simply didn't settle in time (see below, which stays
		// SettleTimeout and gets a plain same-obstacle retry instead of a resync). Live
		// testing on production found the resync trigger originally covered both cases,
		// which meant a single obstacle click that fired correctly but hadn't resolved yet
		// (Obstacle net, specifically — see COLLAB.md) got abandoned via resync on its very
		// first ambiguous result instead of getting the ordinary retry every other obstacle
		// gets.
		return barbarianplan.SettleCantReach
	}
	if !d.Delay(400 * time.Millisecond) {
		return barbarianplan.SettleTimeout
	}

	match, ok := FindObstacle(d, name, startTile, barbarianplan.SearchRadius)
	if !ok {
		d.logf("could not find %q near %d,%d,%d", name, startTile.X, startTile.Z, startTile.Level)
		return barbarianplan.SettleCantReach
	}
	if here, ok := currentTile(d); ok {
		d.logf("clicking from %d,%d,%d", here.X, here.Z, here.Level)
	}
	d.logf("OPLOC%d on %q %d at %d,%d", match.Op, match.Name, match.ID, match.X, match.Z)

	beforeSkill, _ := d.Session.State().Skill(AgilityStat)
	clickAt := time.Now()
	if err := actions.OpLoc(d.Session, match.Op, match.X, match.Z, match.ID); err != nil {
		d.logf("OpLoc: %v", err)
		return barbarianplan.SettleTimeout
	}

	deadline := time.Now().Add(time.Duration(barbarianplan.ObstacleTimeoutTicks) * 600 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !d.Delay(200 * time.Millisecond) {
			return barbarianplan.SettleTimeout
		}

		afterSkill, _ := d.Session.State().Skill(AgilityStat)
		xpGained := afterSkill.Generation != beforeSkill.Generation && afterSkill.XP > beforeSkill.XP

		here, hereOK := currentTile(d)
		var herePtr *tile.Tile
		if hereOK {
			herePtr = &here
		}

		if xpGained || barbarianplan.ConfirmCleared(name, xpGained, startTile.Level, herePtr) {
			return barbarianplan.SettleCleared
		}
		since := d.Chat.Since(clickAt)
		if barbarianplan.Fell.MatchString(since) && barbarianplan.ConfirmFall(name, true, herePtr) {
			d.logf("chat matched Fell: %q", strings.TrimSpace(since))
			return barbarianplan.SettleFell
		}
		if barbarianplan.WrongSide.MatchString(since) {
			d.logf("chat matched WrongSide: %q", strings.TrimSpace(since))
			return barbarianplan.SettleWrongSide
		}
		if barbarianplan.CantReach.MatchString(since) {
			d.logf("chat matched CantReach: %q", strings.TrimSpace(since))
			return barbarianplan.SettleCantReach
		}
	}
	return barbarianplan.SettleTimeout
}

// LapState tracks progress through one lap attempt: which step is next, per-step retry
// counts and how many obstacles have cleared so far. Advance runs exactly one obstacle
// attempt, so a caller (cmd/barbtest's loop, or internal/bot's Runner calling Loop once per
// obstacle) gets a natural, cancellable unit of work.
type LapState struct {
	Lap     []string
	Step    int
	Retries map[int]int
	Cleared int
}

// NewLapState starts a fresh attempt at lap from its first step.
func NewLapState(lap []string) *LapState {
	return &LapState{Lap: lap, Retries: map[int]int{}}
}

// Done reports whether every step of the lap has been cleared.
func (ls *LapState) Done() bool { return ls.Step >= len(ls.Lap) }

// Advance runs one obstacle attempt, including the ropeswing fall-recovery sequence
// (settle delay, then either a return to an earlier step or a ladder escape from the fail
// pit) when the attempt ends in a fall. err is always nil — ports the TS original's own
// BarbarianAgility.ts onObstacleSettled exactly: a fall resets the retry counter to zero
// unconditionally (`this.stuck = 0`) rather than counting toward the limit, since falling is
// a normal, expected, fully-recoverable part of every obstacle, not a stuck condition; and
// exceeding the limit on a genuine cant_reach/timeout just skips to the next obstacle
// (`this.advance()`) rather than ending the run. An earlier version of this function
// returned a hard error once the limit was exceeded, which stopped the whole Runner over a
// single unlucky streak of falls — live-observed tonight when Log balance's fall chance hit
// 3 times in a row after 61 clean laps, ending a run that should have just kept going
// exactly like the TS original does. canceled is true when a stop was requested mid-attempt
// (via Deps.Delay returning false); the caller should stop calling Advance in that case
// rather than treating it as a failed attempt.
func (ls *LapState) Advance(d *Deps) (canceled bool, err error) {
	if ls.Done() {
		return false, nil
	}
	if !WaitForGuardian(d) {
		return true, nil
	}
	name := ls.Lap[ls.Step]
	occ := barbarianplan.OccurrenceOf(ls.Lap, ls.Step)
	d.logf("--- step %d/%d: %s (occurrence %d) ---", ls.Step+1, len(ls.Lap), name, occ)
	reason := RunObstacle(d, name, occ)
	d.logf("settled: %s", reason)

	switch reason {
	case barbarianplan.SettleCleared:
		ls.Cleared++
		ls.Step++
		if !d.Delay(barbarianplan.ReactionMs(nil)) {
			return true, nil
		}
		return false, nil

	case barbarianplan.SettleFell:
		// A fall's reposition isn't always instantaneous: live testing showed the tracked
		// tile still moving up to ~1s after the "you fall" chat line (log balance settling
		// a few tiles away over 2-3 position updates). Let it finish before trusting the
		// tile or planning a route from it — retrying immediately was the actual cause of
		// the "unreachable" finding this fix was named for, not a real geometry dead-end
		// (see COLLAB.md for the before/after diagnostic that found this).
		if !d.Delay(2 * time.Second) {
			return true, nil
		}
		if d.OnFall != nil {
			d.OnFall()
		}

		here, hereOK := currentTile(d)
		if hereOK && !barbarianplan.OnCourse(&here) {
			// Confirmed against content source (agility.rs2's agility_instant_fail
			// proc / barbarian_course.rs2's own water-splash forcemove): a fall off
			// any of the three fallable obstacles (ropeswing, log balance, balancing
			// ledge — see FallLevels) can drop the account well outside CourseBox with
			// no automatic rescue — confirmed live at two distinct real landing spots
			// so far, both well away from the course itself, with no guarantee any
			// single nearby loc is actually on the same connected walkable region as
			// the landing tile (a real local-engine run found a "Ladder" loc only ~9
			// tiles away by straight-line distance that the pathfind pack still
			// reported unreachable from there — geometrically close is not the same
			// as connected).
			//
			// This used to try walking straight to one hardcoded coordinate
			// (barbarianplan's former RopeswingPitLadder/RopeswingPitLadderID, and
			// before that a dynamic-but-still-local nearest-Ladder search), both via
			// the same-level-only WalkTo. Both failed live with "unreachable" from a
			// real fall landing tile. What actually recovered in that same run, one
			// Advance() cycle later, was the existing off-course branch below simply
			// retrying the obstacle — which calls RunObstacle -> WalkTo first, itself
			// falling through this same off-course check again — until a subsequent
			// DecideStage cycle's StageTravel/StageEnter path (EnterCourse ->
			// TravelToCourse, the identical webwalk-plus-freshstartrunner-fallback
			// machinery already proven live for a death-respawn stranding — see
			// COLLAB.md and cmd/barbtraveltest) took over and found a real route back
			// through gates and a "Coal trucks log balance" shortcut. So: call that
			// same proven-resilient primitive directly here instead of any
			// ladder-specific heuristic — no reason to reinvent a narrower, less
			// capable recovery when a broader one is already sitting right there and
			// has now been observed live to actually work from this exact situation.
			d.logf("fell off the course at %d,%d,%d — traveling back via EnterCourse", here.X, here.Z, here.Level)
			if entered, err := EnterCourse(d); err != nil {
				d.logf("EnterCourse: %v", err)
			} else if !entered {
				d.logf("EnterCourse did not report success")
			} else {
				d.logf("back on the course")
			}
		} else if ret, ok := barbarianplan.FallReturnFor(name); ok {
			if idx, ok := firstStepNamed(ls.Lap, ret); ok {
				d.logf("fell: returning to %q (step %d)", ret, idx+1)
				ls.Step = idx
				ls.Retries[ls.Step] = 0
				return false, nil
			}
		}
		// Otherwise just retry the same obstacle from wherever the fall (and any rescue)
		// left us — WalkTo re-paths fresh each call. A fall never counts toward the retry
		// limit (matches the TS original's unconditional `this.stuck = 0` on every 'fell'
		// outcome) — it's a normal, fully-recoverable part of the obstacle, not a stuck
		// condition, so skip the shared retry-counter increment below entirely.
		ls.Retries[ls.Step] = 0
		return false, nil

	case barbarianplan.SettleCantReach:
		// The current step's obstacle could not be found or reached at all (WalkTo failed,
		// or no matching loc was in range once there) — ports the original TS driver's own
		// recovery for exactly this (resyncToNearest/resyncTo): a session that starts (or a
		// prior run that ended) somewhere other than this step's own start tile should pick
		// the lap back up at whichever obstacle is actually nearby, not retry one that's
		// nowhere near the account's real position.
		//
		// Deliberately NOT triggered by SettleTimeout (a click that fired correctly but
		// simply didn't settle within the timeout window): live testing on production found
		// resyncing on every timeout too was actively wrong — Obstacle net's click was
		// confirmed (via a diagnostic log of the exact tile at click time) to be firing from
		// the right tile with the right op every time, yet resync abandoned it on the very
		// first ambiguous result instead of giving it the same plain retry every other
		// obstacle gets (see COLLAB.md). A SettleTimeout now falls through to that ordinary
		// per-step retry below, unchanged from before this resync feature existed.
		//
		// Only takes the resync as progress (skips the retry-counter increment below) when
		// it actually finds a *different* step — resyncing to the very step that just failed
		// would just repeat the same "unreachable" outcome forever.
		if idx, ok := ResyncToNearest(d, ls.Lap); ok && idx != ls.Step {
			d.logf("resync: step %d (%q) → %d (%q), nearest reachable obstacle", ls.Step+1, name, idx+1, ls.Lap[idx])
			ls.Step = idx
			return false, nil
		}
	}

	ls.Retries[ls.Step]++
	if ls.Retries[ls.Step] > barbarianplan.LapRetryLimit {
		// Matches the TS original exactly: give up on this one obstacle and move on rather
		// than ending the run — this.advance() there, ls.Step++ here, letting the caller's
		// own ls.Done() check pick up lap-completion bookkeeping the same way a normal
		// cleared-obstacle advance does.
		d.logf("%q is not completing after %d tries — moving on to the next obstacle", name, ls.Retries[ls.Step])
		ls.Retries[ls.Step] = 0
		ls.Step++
		return false, nil
	}
	d.logf("retrying %q (attempt %d/%d)", name, ls.Retries[ls.Step], barbarianplan.LapRetryLimit)
	return false, nil
}
