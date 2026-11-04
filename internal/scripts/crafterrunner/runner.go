// Package crafterrunner is the I/O half of the ProgressiveCrafter port: it drives the pure
// decisions in internal/scripts/crafterplan against a live session, the way
// internal/scripts/barbarianrunner/fisherrunner/mulerunner already did for the first three
// scripts. Scope, deliberately narrow for this first pass (matching every other script's own
// first-proof precedent): the flax loop only (crafterplan.StageFlax's mechanism) — pick flax
// at the Seers' Village field, climb to the spinning wheel, spin flax into bow strings, climb
// back down, bank the strings. freshstart's tutorial-to-crafting-10 wool bootstrap and
// donorplan's mule hand-over are NOT implemented here; see internal/bot/crafter.go and
// COLLAB.md for exactly what is and isn't covered.
//
// History: this package started life on branch agent/crafterbot (commit 4cc807c, paused
// mid-task for a machine-load checkpoint — see that commit's own PROGRESS_NOTES.md), which
// live-proved picking flax, climbing the ladder, and spinning 3 real flax into bow strings via
// the new internal/actions.OpLocU wire action, all against the local engine. This version
// carries that proof forward with two fixes that branch's own notes flagged as still open:
//
//  1. The ladder's approach tile: crafterplan.Seers().Ladder (2714,3471) is diagonally
//     adjacent to the actual ladder loc (2715,3470) — confirmed live back then, the engine
//     genuinely rejects an interaction from there ("I can't reach that!") even though it
//     satisfies RouteToWheel/RouteToField's own Chebyshev<=1 check (a diagonal neighbour
//     counts the same as an orthogonal one under that metric, but the server's own reach
//     check does not). ladderApproach below targets the orthogonal tile (2714,3470) instead,
//     which the earlier session confirmed does work, for both the cheat and the genuine walk
//     path — the earlier session only patched the cheat-teleport side of this; this version
//     fixes the real WalkNear/GoToWheel/GoToField/GoToBank path too, which is what a real
//     (non-cheat) account actually needs.
//  2. The wheelhouse door: internal/pathfind's collision pack bakes every door permanently
//     shut ("v1: no doors", see pack.go's own doc comment) with no live-state notion, so
//     WalkNear/FindPath can never route through the Seers wheelhouse's one entrance (a "Door"
//     loc, op "Open") regardless of whether it's actually open — confirmed live: a WalkNear
//     to the ladder from outside the wheelhouse always fails "unreachable". crossSeersDoor
//     below is the real (non-cheat) fix, not a cheat workaround: walk up to the door (an
//     ordinary WalkNear — the approach itself needs no pathfind fix), open it, then send one
//     raw two-point MOVE_GAMECLICK straight through, bypassing FindPath for that one step and
//     trusting the *server's* own live collision (which does know the door just opened) to
//     accept it — exactly what a real client's floor-click on the far side of an open door
//     does. This mirrors BarbarianAgility's own established pattern for a structurally
//     identical problem (SqueezeIn: walk close via ordinary pathfind, then a manual,
//     non-pathfind interaction to cross the one barrier the collision pack can't route
//     through) rather than inventing a new approach. The earlier session's own TeleToLadder
//     (still kept below, unused by default) was a LOCAL-ENGINE-ONLY cheat stand-in for
//     exactly this gap; crossSeersDoor is what makes the genuine walk work instead, which is
//     what a real, non-cheat account (this session's own live-verification target,
//     crafter1) actually needs.
//
// Fixing internal/pathfind itself to route through open doors generally remains real,
// valuable, cross-cutting work for whoever picks it up next — every future script that needs
// to enter a building will hit this same gap. crossSeersDoor is a narrow, hardcoded-to-Seers
// stopgap, not that fix.
//
// Every blocking wait goes through a caller-supplied Delayer instead of time.Sleep directly,
// so a cancellable caller (internal/bot's Context.Delay) can make a running bot actually stop
// mid-wait, while a standalone tool with nothing to cancel can just pass RealDelay — the same
// shape barbarianrunner/fisherrunner/mulerunner already settled on.
package crafterrunner

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
	"rsb0t-go/internal/scripts/crafterplan"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// CraftingStat is UPDATE_STAT's index for Crafting, confirmed against the engine's own
// enum (engine/src/engine/entity/PlayerStat.ts): attack,defence,strength,hitpoints,ranged,
// prayer,magic,cooking,woodcutting,fletching,fishing,firemaking,crafting,... — crafting is
// index 12. Matches barbarianrunner.AgilityStat/fisherrunner.FishingStat's own citation.
const CraftingStat = 12

// PlayerPack is the persistent backpack component id — the same real, live-confirmed
// constant fisherrunner.PlayerPack/barbarianrunner.PlayerPack already established. Kept as
// its own copy rather than importing another runner package for one constant, matching how
// each runner package is otherwise self-contained.
const PlayerPack = 3214

// Delayer sleeps for d, returning false early (before the full duration elapses) if the
// caller should stop instead of true once d has fully elapsed. Same shape as
// barbarianrunner.Delayer/fisherrunner.Delayer, deliberately — these runner packages are
// siblings.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration and never cancels — for a standalone tool with
// no runner to request a stop.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one flax-loop step needs from the outside world.
type Deps struct {
	Session actions.Session
	Pack    *pathfind.Pack
	Locs    *locindex.Index
	Config  *cachecfg.Config
	// Components is the parsed interface archive BankAll needs to resolve its
	// deposit-side component; nil makes BankAll fail clearly rather than opening a bank
	// it can't actually read.
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	// Log receives one already-formatted line per notable event; nil drops them.
	Log func(string)
	// Walker, when set, walks the wool stage's Lumbridge legs through internal/webwalk (doors,
	// gates and the castle staircase), as ProgressiveCrafter.ts walks them with
	// Traversal.walkResilient. Nil falls back to freshstartrunner.WalkNear.
	Walker *webwalk.Walker
	// SpinGuard, when set and On, spins the flax through the wheel's Spin menu in batches of one flax every two ticks,
	// measuring itself against the plain use-on-wheel (spinbatch.go, crafterplan/spinmanip.go). The account's own, kept
	// across SpinFlaxUntil calls. Nil is the plain use.
	SpinGuard *crafterplan.SpinGuard
	// KeepExtra names stay in the pack through every deposit (the run staller's jewellery); AtBank, when set, is called with the
	// open bank after a visit's deposit (DrawFlax), the one place the account draws that jewellery from its own bank.
	KeepExtra []string
	AtBank    func(bank *actions.Bank)
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

// waitFor polls cond every 100ms via d.Delay until it's true or timeout elapses, returning
// false immediately (without logging a timeout) if d.Delay reports a stop was requested.
// Mirrors barbarianrunner/fisherrunner's own unexported waitFor.
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

// guardianWaitLogEvery matches barbarianrunner/fisherrunner's own identical constant —
// see their doc comments for the real production case (a 2+ minute evade event, hundreds
// of near-identical "waiting" lines burying the handler's own more important ones) this
// logging change is for. crafterrunner never got this same fix when it landed elsewhere
// — found live 2026-09-15 (see COLLAB.md): three concurrent ProgressiveCrafter accounts
// each logging "[guardian] a random event is present; waiting" on every single 500ms
// poll while waiting, unthrottled.
const guardianWaitLogEvery = 10 * time.Second

// emergencyFleeHome/emergencyFleeDriftTiles: this package opts OUT of EmergencyFlee's own
// drift check (driftTiles 0 — see that function's own doc comment for why every caller now
// supplies its own pair instead of this package assuming every script operates near
// Catherby), on purpose.
//
// First attempt here used crafterplan.Seers().Bank/150, mirroring fisherrunner — reasonable
// for the flax loop itself (field/ladder/wheel/bank all sit within ~50 tiles of the bank),
// but live-found wrong minutes later in the same soak used to verify it: internal/bot/
// crafter.go's Loop() calls this package's WaitForGuardian as its ONE guardian gate
// regardless of phase, including during woolStep's own Lumbridge-area wool-to-crafting-10
// bootstrap (hundreds of tiles from Seers, sheep-shearing waits that sit at one tile long
// enough to satisfy stationaryFor) — a false positive of exactly the same shape this whole
// fix exists to remove, just narrower (crafter-vs-Seers instead of every-non-fisher-script-
// vs-Catherby). Unlike fisherrunner's own bootstrap (freshstartrunner, a separate package
// with its own already-opted-out EmergencyFlee calls), crafter's wool bootstrap shares this
// exact call site with the main flax loop, so there is no clean per-phase home to thread in
// without deeper plumbing than this task's scope — opting out entirely, the same choice
// already made for freshstartrunner/donorrunner, is the safe one. EmergencyFlee's InCombat
// branch (unaffected by driftTiles<=0) still runs regardless of phase.
var emergencyFleeHome pathfind.NavPoint // unused while emergencyFleeDriftTiles <= 0

const emergencyFleeDriftTiles = 0

// WaitForGuardian pauses while a random event is detected at all — pending or already being
// handled elsewhere — so the caller never starts a new interaction on top of one. Returns
// false if a stop was requested while waiting. Identical in shape and purpose to
// barbarianrunner/fisherrunner's own WaitForGuardian, including their use of
// randomevents.Busy (cooldown-aware) instead of raw Detect — see fisherrunner's own doc
// comment for the real live gap that closes.
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

// TeleTo sends ::tele for a tile, using the local engine's level,mapsquareX,mapsquareZ,
// localX,localZ format. Local-engine-only.
func TeleTo(d *Deps, t tile.Tile) error {
	return actions.Cheat(d.Session, fmt.Sprintf("tele %d,%d,%d,%d,%d", t.Level, t.X>>6, t.Z>>6, t.X&0x3f, t.Z&0x3f))
}

// TeleToLadder is a LOCAL-ENGINE-ONLY convenience kept from the earlier session's own proof
// (see this package's doc comment, fix #2) — never required by the default Loop path any
// more (crossSeersDoor below is the real fix a genuine account needs), but harmless to keep
// around for a quick diagnostic. Never call this against a real account.
func TeleToLadder(d *Deps) error {
	ladder := ladderApproach(crafterplan.Seers().Ladder.Level)
	if err := TeleTo(d, ladder); err != nil {
		return err
	}
	if !waitFor(d, "arrive at the ladder", 8*time.Second, func() bool {
		x, z, _, ok := d.Session.State().Tile()
		return ok && x == ladder.X && z == ladder.Z
	}) {
		return fmt.Errorf("did not arrive at the ladder after teleport")
	}
	d.Delay(500 * time.Millisecond)
	return nil
}

// walkDeadline is longer than barbarianrunner's/fisherrunner's own 30s (obstacle-to-
// obstacle and camp-to-bank hops that stay short): the Seers field-to-bank leg alone is
// roughly 50 tiles, so a single leg can legitimately take the better part of a minute even
// on a clean path — confirmed live during the earlier session's own proof, where a 30s cap
// cut the walk off mid-route with the tracked tile nowhere near either end.
const walkDeadline = 120 * time.Second

// WalkNear paths to dest and walks there (never running, per the position-tracking drift
// bug on running clicks already documented in COLLAB.md/barbarianrunner/fisherrunner),
// re-clicking from wherever the player stops, stopping once within radius tiles of dest.
// Returns false on a stop request, an unreachable destination, or a walkDeadline timeout.
func WalkNear(d *Deps, dest tile.Tile, radius int) bool {
	deadline := time.Now().Add(walkDeadline)
	for time.Now().Before(deadline) {
		here, ok := currentTile(d)
		if !ok {
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			continue
		}
		if here.Level == dest.Level && tile.Chebyshev(here, dest) <= radius {
			return true
		}
		res := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level})
		if !res.OK {
			d.logf("no path from %d,%d,%d to %d,%d,%d: %s", here.X, here.Z, here.Level, dest.X, dest.Z, dest.Level, res.Reason)
			// DIAG (investigating the ~44-tile unexplained mid-walk displacement flagged
			// in COLLAB.md/PROGRESS_NOTES.md): dump recent chat in case a random event or
			// server message explains how the tracked position got here. Harmless in the
			// common "genuinely unreachable destination" case too — just extra log noise.
			d.logf("  DIAG: recent chat: %q", d.Chat.Since(time.Now().Add(-10*time.Second)))
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
			// audit following tonight's incident response (same gap fisherrunner.WalkNear/
			// barbarianrunner.WalkNear got fixed for): this loop can hold the account
			// mid-walk for this whole walkDeadline (120s) with nothing checking combat or
			// drift in between, since WaitForGuardian only runs before/after a WalkNear
			// call, never during one. Safe every 200ms tick — TryLock inside EmergencyFlee
			// already makes it a no-op whenever Guardian's kick() owns the action lock.
			randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			cur, ok := currentTile(d)
			if !ok {
				continue
			}
			if cur.Level == dest.Level && tile.Chebyshev(cur, dest) <= radius {
				return true
			}
			if cur.X != lastX || cur.Z != lastZ {
				// DIAG: a jump bigger than a normal single-tile step (>2 Chebyshev
				// between consecutive 200ms polls) is exactly the signature of the
				// unexplained mid-walk displacement — log it loudly so it's not lost
				// in the ordinary per-tile progress lines.
				if tile.Chebyshev(tile.Tile{X: lastX, Z: lastZ, Level: cur.Level}, cur) > 2 {
					d.logf("  DIAG: unexpected jump from %d,%d to %d,%d (%d tiles) — chat: %q", lastX, lastZ, cur.X, cur.Z, tile.Chebyshev(tile.Tile{X: lastX, Z: lastZ, Level: cur.Level}, cur), d.Chat.Since(time.Now().Add(-5*time.Second)))
				}
				lastX, lastZ, still = cur.X, cur.Z, time.Now()
			} else if time.Since(still) > 2*time.Second {
				break
			}
		}
	}
	here, _ := currentTile(d)
	d.logf("gave up walking; stopped at %d,%d,%d", here.X, here.Z, here.Level)
	return false
}

func sign(n int) int {
	if n > 0 {
		return 1
	} else if n < 0 {
		return -1
	}
	return 0
}

// crossSeersDoor is the real, non-cheat fix for gap #2 in this package's own doc comment:
// internal/pathfind's static collision pack bakes the Seers wheelhouse's one door
// permanently shut, so no ordinary WalkNear/FindPath call can ever route through it, even
// once it is genuinely open. This walks up to the door (an ordinary WalkNear — approaching
// it needs no fix, only crossing it does), opens it, then steps exactly one tile across the
// threshold via a raw single-corner MOVE_GAMECLICK, bypassing FindPath for that single step
// and trusting the server's own live collision (which does know the door just opened) to
// accept it — the same thing a real client's click on the far side of an open door does.
// Once across, hands off to an ordinary WalkNear(dest, ...) for whatever's left, since the
// inside of the wheelhouse is a normal, connected part of the pack's graph — only the
// threshold tile itself is the gap. Mirrors barbarianrunner's own SqueezeIn: walk close via
// ordinary pathfind, then a manual, non-pathfind interaction for the one obstacle the
// collision pack can't route through.
//
// Searches for the door near crafterplan.Seers().Ladder (a fixed, known-good anchor), NOT
// the caller's current position — found live during this session's own testing: a
// current-position-centered radius search from a tile roughly equidistant between the
// wheelhouse and an unrelated door elsewhere (the same "Door" loc type, id 1530, is reused
// all over the map, not unique to the wheelhouse) picked up the WRONG door, ~13 tiles south
// of the real entrance, and the resulting "walk straight to dest" step then had to cover
// that whole remaining distance in one raw click, which just timed out. Anchoring the
// search on the ladder's own fixed position always finds the one true wheelhouse door
// regardless of which side (field or bank) the account is approaching from.
func crossSeersDoor(d *Deps, dest tile.Tile) bool {
	l := crafterplan.Seers()
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	door, foundClosed := d.Locs.Nearest(d.Config, locindex.Query{X: l.Ladder.X, Z: l.Ladder.Z, Level: l.Ladder.Level, Radius: 10, Name: "Door", Op: "Open"}, live)
	if !foundClosed {
		// Already open — found live, mid-session: once opened, this door no longer
		// offers "Open" at all (it becomes a different loc/op state, "Close"), so a
		// second crossing attempt in the same run (or a fresh account arriving after
		// someone else already opened it — this door has no confirmed auto-reclose
		// timer) must not treat "no closed door found" as failure. Locate it by name
		// alone instead, purely to walk up to and step onto its tile; nothing needs
		// clicking.
		var foundAny bool
		door, foundAny = d.Locs.Nearest(d.Config, locindex.Query{X: l.Ladder.X, Z: l.Ladder.Z, Level: l.Ladder.Level, Radius: 10, Name: "Door"}, live)
		if !foundAny {
			d.logf("could not find the wheelhouse door near the ladder (%d,%d)", l.Ladder.X, l.Ladder.Z)
			return false
		}
		d.logf("wheelhouse door %d at %d,%d is already open — walking straight through", door.ID, door.X, door.Z)
	}
	if !walkOrthogonal(d, tile.Tile{X: door.X, Z: door.Z, Level: door.Level}) {
		d.logf("could not walk up to the door at %d,%d", door.X, door.Z)
		return false
	}
	if foundClosed {
		d.logf("OPLOC%d on %q %d at %d,%d (opening the way through)", door.Op, door.Name, door.ID, door.X, door.Z)
		if err := actions.OpLoc(d.Session, door.Op, door.X, door.Z, door.ID); err != nil {
			d.logf("OpLoc (door): %v", err)
			return false
		}
		if !d.Delay(900 * time.Millisecond) {
			return false
		}
	}
	// Step onto the door's own tile — a guaranteed single orthogonal step, since
	// walkOrthogonal above just placed the caller on one of its four orthogonal
	// neighbors. A diagonal single-tile step straight past the door (tried first, before
	// this version) failed live ("timed out waiting for stepping through the door") —
	// this local engine's movement processing apparently only honours a straight
	// orthogonal crossing, matching the same orthogonal-only pattern already found for
	// the ladder and bank booth (see orthogonalNeighbors' own doc comment).
	doorTile := tile.Tile{X: door.X, Z: door.Z, Level: door.Level}
	if !rawStep(d, doorTile) {
		return false
	}
	// Now try the ordinary connected walk first (the pack may already treat the door's
	// own tile as connected to the inside); if not, take one more single orthogonal step
	// past the door — trying each of its unused orthogonal neighbors in turn — before
	// trying again. This is a small, bounded search rather than assuming which side of
	// the door tile is "inside," since that depends on which wall the door sits in.
	if WalkNear(d, dest, 1) {
		return true
	}
	for _, n := range orthogonalNeighbors(doorTile) {
		if !rawStep(d, n) {
			continue
		}
		if WalkNear(d, dest, 1) {
			return true
		}
	}
	t, _ := currentTile(d)
	d.logf("  DIAG: crossed the door to %d,%d,%d but still could not reach %d,%d,%d", t.X, t.Z, t.Level, dest.X, dest.Z, dest.Level)
	return false
}

// rawStep sends one raw single-tile MOVE_GAMECLICK from the caller's current tile to next
// (which must be orthogonally or diagonally adjacent — this is a single game-movement step,
// not a multi-tile route), bypassing FindPath entirely. Used only for crossing the
// wheelhouse door threshold itself, where FindPath can never produce a route at all (see
// crossSeersDoor's own doc comment).
func rawStep(d *Deps, next tile.Tile) bool {
	here, ok := currentTile(d)
	if !ok {
		return false
	}
	if here.X == next.X && here.Z == next.Z {
		return true
	}
	clickAt := time.Now()
	if err := actions.Walk(d.Session, []pathfind.NavPoint{
		{X: here.X, Z: here.Z, Level: here.Level},
		{X: next.X, Z: next.Z, Level: next.Level},
	}, false); err != nil {
		d.logf("rawStep: %v", err)
		return false
	}
	ok = waitFor(d, "a raw single step", 5*time.Second, func() bool {
		t, ok := currentTile(d)
		return ok && t.X == next.X && t.Z == next.Z
	})
	if !ok {
		t, _ := currentTile(d)
		d.logf("  DIAG after rawStep: tile=%d,%d,%d (started from %d,%d, wanted %d,%d) chat=%q", t.X, t.Z, t.Level, here.X, here.Z, next.X, next.Z, d.Chat.Since(clickAt))
	}
	return ok
}

// walkOrCrossDoor is WalkNear with crossSeersDoor as a fallback for exactly the case
// WalkNear can never solve on its own: a route that must cross the wheelhouse's door.
// Tried in this order (cheap, ordinary case first) rather than always going through the
// door helper, since most legs of the flax loop (the field itself, the bank) never need it.
func walkOrCrossDoor(d *Deps, dest tile.Tile, radius int) bool {
	if WalkNear(d, dest, radius) {
		return true
	}
	return crossSeersDoor(d, dest)
}

// CheatSetup bypasses the wool-to-crafting-10 grind via ::setstat and positions the account
// at the Seers flax field via ::tele, exactly mirroring barbarianrunner.CheatSetup/
// fisherrunner.CheatSetup's own local-engine-only convention. Local-engine-only; never call
// this against a real account.
func CheatSetup(d *Deps, craftingLevel int) error {
	d.logf("::setstat crafting %d (bypassing the wool-to-10 bootstrap)", craftingLevel)
	if err := actions.Cheat(d.Session, fmt.Sprintf("setstat crafting %d", craftingLevel)); err != nil {
		return err
	}
	if !d.Delay(500 * time.Millisecond) {
		return nil
	}
	field := crafterplan.Seers().Field
	d.logf("::tele to the Seers flax field (%d,%d,%d)", field.X, field.Z, field.Level)
	if err := TeleTo(d, field); err != nil {
		return err
	}
	if !waitFor(d, "arrive at the flax field", 8*time.Second, func() bool {
		x, z, _, ok := d.Session.State().Tile()
		return ok && x == field.X && z == field.Z
	}) {
		return fmt.Errorf("did not arrive at the flax field after teleport")
	}
	d.Delay(1500 * time.Millisecond)
	return nil
}

// orthogonalNeighbors returns the four tiles orthogonally adjacent to t (never a diagonal
// one) — this local engine's own reach-check rejects an interaction from a diagonal
// neighbor even though it satisfies an ordinary Chebyshev<=1 distance check, confirmed live
// twice during this task's own testing: the Seers ladder (see ladderApproach's own doc
// comment, the earlier session's original finding) and, found fresh during this session's
// own live run, the Seers bank booth itself — WalkNear(boothTile, 1) can legitimately stop
// on the booth's diagonal neighbor, and OpLoc from there gets a genuine server chat
// rejection ("I can't reach that!"), not a client-side bug. walkOrthogonal below is the
// general-purpose fix, used for both.
func orthogonalNeighbors(t tile.Tile) []tile.Tile {
	return []tile.Tile{
		{X: t.X, Z: t.Z - 1, Level: t.Level},
		{X: t.X, Z: t.Z + 1, Level: t.Level},
		{X: t.X - 1, Z: t.Z, Level: t.Level},
		{X: t.X + 1, Z: t.Z, Level: t.Level},
	}
}

// walkOrthogonal walks to one of target's four orthogonal neighbors (trying each in turn),
// so a subsequent OpLoc on target is never rejected by this engine's own stricter
// reach-check — see orthogonalNeighbors' own doc comment. Falls back to a plain
// WalkNear(target, 1) if none of the four orthogonal tiles are reachable (e.g. the loc sits
// against a wall with only a diagonal approach genuinely available), so this is never worse
// than the old behaviour, only better in the common case.
func walkOrthogonal(d *Deps, target tile.Tile) bool {
	for _, n := range orthogonalNeighbors(target) {
		if WalkNear(d, n, 0) {
			return true
		}
	}
	return WalkNear(d, target, 1)
}

// ladderApproach is the walkable tile the engine actually accepts a climb interaction from,
// on level — see this package's own doc comment, fix #1: crafterplan.Seers().Ladder
// (2714,3471) is diagonally adjacent to the real ladder loc (2715,3470) and the server
// rejects an interaction from there ("I can't reach that!"), even though it satisfies
// RouteToWheel/RouteToField's own Chebyshev<=1 check. The working, orthogonal tile is
// (2714,3470), confirmed live. Kept in crafterrunner rather than crafterplan since it's a
// live-interaction quirk, not routing data the pure package should encode.
//
// Never hardcode a fixed Level here — the ladder is two separate loc placements at the same
// X/Z (id 1747 "Climb-up" on level 0, id 1746 "Climb-down" on level 1, both confirmed live),
// so the tile a caller needs to stand on is always whatever level the caller is currently
// on. Using crafterplan.Seers().Ladder.Level directly (which is 0, the climb-up side's own
// zero value) while already on level 1 sends a same-tile-different-level FindPath, which
// internal/pathfind (no cross-level/transport routing — "v1" scope) always reports
// unreachable — confirmed live during the earlier session's own proof of the climb-down leg.
func ladderApproach(level int) tile.Tile {
	return tile.Tile{X: 2714, Z: 3470, Level: level}
}

// climbLadder works the Seers ladder in one direction and waits until the tracked level
// flips, per crafterplan.Ladder/ClimbUp/ClimbDown. The ladder is two separate loc
// placements, one per level (confirmed live: id 1747 "Climb-up" on level 0, id 1746
// "Climb-down" on level 1, both at 2715,3470, one tile from ladderApproach) —
// RouteToWheel/RouteToBank already tell the caller which direction is needed, this just
// executes it.
func climbLadder(d *Deps, up bool) bool {
	here, ok := currentTile(d)
	if !ok {
		return false
	}
	op := crafterplan.ClimbDown
	wantLevel := 0
	if up {
		op = crafterplan.ClimbUp
		wantLevel = 1
	}
	ladder := crafterplan.Seers().Ladder
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	m, ok := d.Locs.Nearest(d.Config, locindex.Query{X: ladder.X, Z: ladder.Z, Level: here.Level, Radius: 2, Name: crafterplan.Ladder, Op: op}, live)
	if !ok {
		d.logf("could not find the Seers ladder (%q) near %d,%d,%d", op, ladder.X, ladder.Z, here.Level)
		return false
	}
	d.logf("OPLOC%d on %q %d at %d,%d (%s)", m.Op, m.Name, m.ID, m.X, m.Z, op)
	if err := actions.OpLoc(d.Session, m.Op, m.X, m.Z, m.ID); err != nil {
		d.logf("OpLoc: %v", err)
		return false
	}
	before := time.Now()
	ok = waitFor(d, "climb the ladder", 8*time.Second, func() bool {
		t, ok := currentTile(d)
		return ok && t.Level == wantLevel
	})
	if !ok {
		t, _ := currentTile(d)
		d.logf("  DIAG after climb attempt: tile=%d,%d,%d chat=%q", t.X, t.Z, t.Level, d.Chat.Since(before))
	}
	return ok
}

// GoToField gets the account to within the flax field's scope on the ground floor,
// following crafterplan.RouteToField: climb down from the wheel if needed (crossing the
// wheelhouse door on the way, via walkOrCrossDoor), then walk.
func GoToField(d *Deps) bool {
	l := crafterplan.Seers()
	if d.Walker != nil {
		// webwalk's Seers route (door and ladder) is live-proven; see webwalk's adoption plan.
		if here, ok := currentTile(d); ok && crafterplan.RouteToField(here) == crafterplan.MoveHere {
			return true
		}
		return Walk(d, l.Field, l.FieldScope/2, 3*time.Minute)
	}
	for i := 0; i < 6; i++ {
		here, ok := currentTile(d)
		if !ok {
			return false
		}
		switch crafterplan.RouteToField(here) {
		case crafterplan.MoveHere:
			return true
		case crafterplan.MoveWalk:
			// walkOrCrossDoor, not a plain WalkNear: after a climb-down, the account
			// is still inside the wheelhouse (level 0, same as the field, so
			// RouteToField already reads this as "just walk") and must cross the
			// door outward before an ordinary walk to the field can succeed — found
			// live this session (see GoToBank's identical fix and its own doc
			// comment for the full account of the failure this caused).
			if !walkOrCrossDoor(d, l.Field, l.FieldScope/2) {
				return false
			}
		case crafterplan.MoveClimbDown:
			if !walkOrCrossDoor(d, ladderApproach(here.Level), 1) || !climbLadder(d, false) {
				return false
			}
		default:
			return false
		}
	}
	here, _ := currentTile(d)
	return crafterplan.RouteToField(here) == crafterplan.MoveHere
}

// GoToWheel gets the account up to the spinning wheel, following crafterplan.RouteToWheel:
// walk beside the ladder if not already close (crossing the wheelhouse door via
// walkOrCrossDoor — see this package's own doc comment, fix #2), then climb up.
func GoToWheel(d *Deps) bool {
	l := crafterplan.Seers()
	if d.Walker != nil {
		// Up the ladder through webwalk, then onto the wheel's east tile, the side OPLOCU was
		// live-proven from (kept, per webwalk's adoption plan).
		east := tile.Tile{X: l.Wheel.X + 1, Z: l.Wheel.Z, Level: l.Wheel.Level}
		if here, ok := currentTile(d); ok && here == east {
			return true
		}
		return Walk(d, east, 0, 3*time.Minute)
	}
	for i := 0; i < 6; i++ {
		here, ok := currentTile(d)
		if !ok {
			return false
		}
		switch crafterplan.RouteToWheel(here) {
		case crafterplan.MoveHere:
			// RouteToWheel's "MoveHere" only means "on the wheel's level" — the top
			// of the ladder lands a few tiles from the wheel itself (confirmed live:
			// climbing puts the player near 2715,3470,1, four tiles from the wheel at
			// 2711,3471, too far for OPLOCU to reach), so still close the last stretch
			// with a real short walk. No door/pathfind gap up here (confirmed live);
			// unlike the ladder itself, this one is a genuine walk, not a cheat.
			if tile.Chebyshev(here, l.Wheel) > 1 {
				// The wheel's east tile (l.Wheel.X+1, one tile east of 2711,3471) is
				// the specific approach OPLOCU was first live-confirmed to accept
				// (this session's earlier long-walk proof: "walk ... toward
				// 2712,3471"); walkOrthogonal's own generic neighbor order can also
				// land on the south tile, which OPLOCU rejected live during this
				// session's own short-loop testing (repeated "timed out waiting for
				// a bow string after spinning" with no chat/error — the click simply
				// never lands, the same silent-reach-rejection shape as the ladder
				// and bank booth's diagonal quirk, just a different side this time).
				// Try the known-good tile first, then fall back to the generic
				// search for resilience if the map ever changes.
				east := tile.Tile{X: l.Wheel.X + 1, Z: l.Wheel.Z, Level: l.Wheel.Level}
				if WalkNear(d, east, 0) {
					return true
				}
				return walkOrthogonal(d, l.Wheel)
			}
			return true
		case crafterplan.MoveWalk:
			if !walkOrCrossDoor(d, ladderApproach(here.Level), 1) {
				return false
			}
		case crafterplan.MoveClimbUp:
			if !climbLadder(d, true) {
				return false
			}
		default:
			return false
		}
	}
	here, _ := currentTile(d)
	return crafterplan.RouteToWheel(here) == crafterplan.MoveHere
}

// GoToBank gets the account down to the Seers bank, following crafterplan.RouteToBank:
// climb down from the wheel if needed (crossing the wheelhouse door via walkOrCrossDoor),
// then walk to the bank tile.
func GoToBank(d *Deps) bool {
	l := crafterplan.Seers()
	if d.Walker != nil {
		if here, ok := currentTile(d); ok && here.Level == l.Bank.Level && tile.Chebyshev(here, l.Bank) <= 3 {
			return true
		}
		return Walk(d, l.Bank, 3, 3*time.Minute)
	}
	for i := 0; i < 6; i++ {
		here, ok := currentTile(d)
		if !ok {
			return false
		}
		switch crafterplan.RouteToBank(here) {
		case crafterplan.MoveHere:
			// walkOrCrossDoor, not a plain WalkNear: RouteToBank's "MoveHere" only
			// means "on the bank's level" — right after a climb-down, that's true
			// but the account is still inside the wheelhouse, behind the same door
			// it came in through, and a plain WalkNear can never route out (found
			// live this session: "no path from 2714,3470,0 to 2725,3493,0:
			// unreachable" straight after a successful climb-down, the very first
			// full climb/spin/climb-down/bank cycle this session got working).
			return walkOrCrossDoor(d, l.Bank, 8)
		case crafterplan.MoveClimbDown:
			if !walkOrCrossDoor(d, ladderApproach(here.Level), 1) || !climbLadder(d, false) {
				return false
			}
		default:
			return false
		}
	}
	return false
}

// FindFlaxLocs lists every pickable Flax within the field's scope, as
// crafterplan.ChooseFlax expects: name, tile and (synthetically, since the query already
// filtered on it) the Pick op. Merges the static loc index with live LOC_DEL/LOC_ADD_CHANGE
// so an already-picked stalk is never offered twice.
func FindFlaxLocs(d *Deps) []crafterplan.Loc {
	l := crafterplan.Seers()
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	matches := d.Locs.Find(d.Config, locindex.Query{X: l.Field.X, Z: l.Field.Z, Level: l.Field.Level, Radius: l.FieldScope, Name: crafterplan.Flax, Op: crafterplan.PickOp}, live)
	out := make([]crafterplan.Loc, len(matches))
	for i, m := range matches {
		out[i] = crafterplan.Loc{Name: m.Name, Tile: tile.Tile{X: m.X, Z: m.Z, Level: m.Level}, Ops: []string{crafterplan.PickOp}}
	}
	return out
}

func stillHasFlax(d *Deps, t tile.Tile) bool {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	m, ok := d.Locs.Nearest(d.Config, locindex.Query{X: t.X, Z: t.Z, Level: t.Level, Radius: 0, Name: crafterplan.Flax, Op: crafterplan.PickOp}, live)
	return ok && m.X == t.X && m.Z == t.Z
}

func countOfName(d *Deps, name string) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if ok && strings.EqualFold(strings.TrimSpace(obj.Name), name) {
			c := it.Count
			if c < 1 {
				c = 1
			}
			n += int(c)
		}
	}
	return n
}

// FlaxCount is how much Flax the player's own pack currently holds.
func FlaxCount(d *Deps) int { return countOfName(d, crafterplan.Flax) }

// BowStringCount is how many Bow strings the player's own pack currently holds.
func BowStringCount(d *Deps) int { return countOfName(d, crafterplan.BowString) }

// PackFull reports whether the player's own pack (PlayerPack) holds 28 non-empty slots.
func PackFull(d *Deps) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	return inv != nil && len(inv.NonEmpty()) >= 28
}

// PackView reads the player's pack as crafterplan.DecideSeers expects it: one
// freshstart.PackItem per non-empty slot, by cachecfg name.
func PackView(d *Deps) []freshstart.PackItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	items := inv.NonEmpty()
	out := make([]freshstart.PackItem, 0, len(items))
	for _, it := range items {
		name := ""
		if obj, ok := d.Config.Obj(int(it.ID)); ok {
			name = obj.Name
		}
		count := int(it.Count)
		if count < 1 {
			count = 1
		}
		out = append(out, freshstart.PackItem{Name: name, Count: count})
	}
	return out
}

// PickFlaxUntil walks to the field and repeatedly picks the nearest reachable, unskipped
// flax (crafterplan.ChooseFlax) until the pack holds at least target flax, the timeout
// expires, or a stop is requested. A tile that a pick attempt leaves standing
// (crafterplan.PickUnreachable — someone else is between us and it, or it's behind the
// fence) is skipped for 30s rather than retried immediately, since ChooseFlax has no other
// way to route around an obstruction; a tile that vanishes without giving anything
// (crafterplan.PickTaken — another player got there first) needs no such skip, since the
// next FindFlaxLocs call will no longer offer it at all. Returns the flax count reached.
func PickFlaxUntil(d *Deps, target int, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	skip := map[string]time.Time{}
	skipped := func(key string) bool {
		until, ok := skip[key]
		return ok && time.Now().Before(until)
	}
	count := FlaxCount(d)
	// A full pack ends the batch, as pickFlax's `!Inventory.isFull()` does. Found live: 27 flax
	// plus one pick filled the pack, and the step kept picking toward before+30, logging "still
	// standing but nothing gained" stalk after stalk until its timeout.
	for time.Now().Before(deadline) && count < target && !PackFull(d) {
		if !WaitForGuardian(d) {
			return count
		}
		if !GoToField(d) {
			d.logf("could not reach the flax field")
			return count
		}
		here, ok := currentTile(d)
		if !ok {
			if !d.Delay(500 * time.Millisecond) {
				return count
			}
			continue
		}
		locs := FindFlaxLocs(d)
		idx := crafterplan.ChooseFlax(locs, here, skipped)
		if idx < 0 {
			d.logf("no reachable, unskipped flax right now — waiting")
			if !d.Delay(2 * time.Second) {
				return count
			}
			continue
		}
		target_ := locs[idx]
		if !WalkNear(d, target_.Tile, 1) {
			skip[crafterplan.FlaxKey(target_.Tile)] = time.Now().Add(30 * time.Second)
			continue
		}
		live := locindex.ChangesFrom(d.Session.State().LocChanges())
		m, ok := d.Locs.Nearest(d.Config, locindex.Query{X: target_.Tile.X, Z: target_.Tile.Z, Level: target_.Tile.Level, Radius: 0, Name: crafterplan.Flax, Op: crafterplan.PickOp}, live)
		if !ok {
			// Someone got it while we were walking over.
			continue
		}
		before := count
		d.logf("OPLOC%d on %q %d at %d,%d", m.Op, m.Name, m.ID, m.X, m.Z)
		if err := actions.OpLoc(d.Session, m.Op, m.X, m.Z, m.ID); err != nil {
			d.logf("OpLoc: %v", err)
			if !d.Delay(time.Second) {
				return count
			}
			continue
		}
		if !d.Delay(1200 * time.Millisecond) {
			return count
		}
		count = FlaxCount(d)
		gained := count > before
		standing := stillHasFlax(d, target_.Tile)
		switch crafterplan.AfterPick(gained, standing) {
		case crafterplan.PickGot:
			d.logf("picked flax (%d in pack now)", count)
		case crafterplan.PickUnreachable:
			d.logf("still standing but nothing gained — skipping that stalk for a while")
			skip[crafterplan.FlaxKey(target_.Tile)] = time.Now().Add(30 * time.Second)
		case crafterplan.PickTaken:
			d.logf("that stalk was gone without a pick — someone else got it")
		}
	}
	return count
}

// findWheel finds the Seers spinning wheel through the static loc index, merged with any
// live change.
func findWheel(d *Deps) (locindex.Match, bool) {
	l := crafterplan.Seers()
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	return d.Locs.Nearest(d.Config, locindex.Query{X: l.Wheel.X, Z: l.Wheel.Z, Level: l.Wheel.Level, Radius: 3, Name: crafterplan.Wheel, Op: crafterplan.SpinOp}, live)
}

// flaxSlot returns the pack slot and object id of the first Flax the pack holds, or
// ok=false when there is none.
func flaxSlot(d *Deps) (slot int, objID int32, ok bool) {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0, 0, false
	}
	for _, it := range inv.NonEmpty() {
		obj, found := d.Config.Obj(int(it.ID))
		if found && strings.EqualFold(strings.TrimSpace(obj.Name), crafterplan.Flax) {
			return it.Slot, it.ID, true
		}
	}
	return 0, 0, false
}

// spinRedelay is how long the next OPLOCU waits after a bow string lands. content's craft_spinning
// ends in p_delay(2): engine sets delayedUntil to the spin's tick + 3 and clears it in that
// tick's player processing, which runs after the tick has read client input, and OpLocUHandler
// drops an OPLOCU it reads while the player is delayed. So the next OPLOCU must reach the server
// after the third tick from the spin starts, about 1.8 s after a string the client sees a moment
// after the spin's tick. Live on 73ff4228 the next flax went 300 ms after each string, was
// dropped, and waited out the full 5 s before being sent again: every second flax cost about 6 s.
const spinRedelay = 1900 * time.Millisecond

// spinAnswerWait bounds one OPLOCU: a spin the server reads answers within that tick, so a flax
// with no string after four ticks was dropped and is sent again.
const spinAnswerWait = 2400 * time.Millisecond

// SpinFlaxUntil walks up to the wheel and uses each flax on it in turn (OPLOCU — the same
// "use inventory item on loc" interaction content/scripts/skill_crafting/scripts/
// spinning/spinning.rs2's [oplocu,_spinning_wheel] handler answers directly with one bow
// string per flax, no "what would you like to spin"/make-count menu needed the way the
// wheel's own generic "Spin" op would require) until the pack is out of flax, target bow
// strings have been produced, the timeout expires, or a stop is requested. Requires
// crafting level 10 (content: spinning.struct [flax] levelrequire=10) — a spin attempt
// below that level just gets a server chat message and no item change, which this reports
// as a stall via the per-attempt waitFor timing out, not a hang.
func SpinFlaxUntil(d *Deps, target int, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	spun := 0
	// The plain baseline for the guard (spinbatch.go): each plain spin costs the ticks since the last string of the same run of
	// spins, or since its own click when it is the first of a run, so a run's total is first click to last string.
	prevTick, inRun := 0, false
	defer func() { reportSpin(d) }()
	for time.Now().Before(deadline) && spun < target {
		if !WaitForGuardian(d) {
			return spun
		}
		slot, objID, ok := flaxSlot(d)
		if !ok {
			d.logf("out of flax to spin")
			return spun
		}
		if !GoToWheel(d) {
			d.logf("could not reach the spinning wheel")
			return spun
		}
		wheel, ok := findWheel(d)
		if !ok {
			d.logf("could not find the spinning wheel near %d,%d,%d", crafterplan.Seers().Wheel.X, crafterplan.Seers().Wheel.Z, crafterplan.Seers().Wheel.Level)
			return spun
		}
		if d.SpinGuard.WantBatch(time.Now()) && unnotedFlax(d) > 0 {
			inRun = false
			n, stop := spinBatch(d, wheel, target-spun)
			spun += n
			if stop {
				return spun
			}
			if n == 0 {
				// The menu would not start: a plain flax now, so the account still makes progress while the guard counts.
				if !d.Delay(time.Second) {
					return spun
				}
			} else {
				continue
			}
		}
		before := BowStringCount(d)
		tick0 := d.Session.State().Tick()
		d.logf("OPLOCU flax (slot %d) on %q %d at %d,%d", slot, wheel.Name, wheel.ID, wheel.X, wheel.Z)
		if err := actions.OpLocU(d.Session, wheel.X, wheel.Z, wheel.ID, objID, slot, PlayerPack); err != nil {
			d.logf("OpLocU: %v", err)
			if !d.Delay(time.Second) {
				return spun
			}
			continue
		}
		if !waitFor(d, "a bow string after spinning", spinAnswerWait, func() bool { return BowStringCount(d) > before }) {
			// Either the animation is slower than usual, or the level check silently
			// refused (shouldn't happen once CheatSetup has run) — re-check the loop
			// condition rather than spinning forever on one stuck attempt. waitFor also gives up at
			// once when a stop is requested or the session drops, and the loop then had no delay
			// at all: live 3 Nov 2026 07:40, "Hi Yes Hello" logged ~20,000 OPLOCU a second for
			// minutes after a disconnect, the log growing 100 MB a minute.
			if !d.Delay(100 * time.Millisecond) {
				return spun
			}
			continue
		}
		spun++
		if !inRun {
			prevTick, inRun = tick0, true
		}
		nowTick := d.Session.State().Tick()
		d.SpinGuard.RecordPlain(1, max(nowTick-prevTick, 1))
		prevTick = nowTick
		d.logf("spun a flax into a bow string (%d spun this run)", spun)
		if !d.Delay(spinRedelay) {
			return spun
		}
	}
	return spun
}

// BankAll walks down to the Seers bank, finds its booth through the static loc index,
// opens it, and deposits every pack item whose name is not in keep — used both to bank
// bow strings (keep = crafterplan.SeersKeep(), i.e. only Coins stays) and, before that, to
// clear out whatever foreign leftovers a reused throwaway account might already be
// carrying (crafterplan.DecideSeers's own "bank everything" step for exactly that case).
// Requires d.Components; fails clearly rather than opening a bank it can't actually read.
func BankAll(d *Deps, keep []string) (int, bool) {
	// Same fix, same session, same root cause as fisherrunner.BankFish (see COLLAB.md's
	// "Strange box" entry): opening the bank touches the account's single shared
	// MainModalID, which a concurrent Guardian handler (e.g. solveStrangeBox's own puzzle
	// interface) also reads and writes. A one-shot Busy()/WaitForGuardian check here
	// isn't enough — proven live via cmd/boxracetest — since Guardian's own 600ms poll
	// can start handling a freshly-spawned event AFTER such a check passes but DURING
	// the bank sequence. randomevents.Lock (acquired below, right before the booth
	// click, not here at the top — walking to the bank touches no modal state and
	// shouldn't block Guardian from an unrelated urgent event) is a real mutex against
	// Guardian's own handling for this sequence's entire duration, not a point-in-time
	// check.
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return 0, false
	}
	if !GoToBank(d) {
		return 0, false
	}
	x, z, level, _ := d.Session.State().Tile()
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	booth, ok := d.Locs.Nearest(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: "Bank booth", Op: "Use-quickly"}, live)
	if !ok {
		d.logf("could not find a bank booth near the Seers bank")
		return 0, false
	}
	if !walkOrthogonal(d, tile.Tile{X: booth.X, Z: booth.Z, Level: booth.Level}) {
		return 0, false
	}
	unlock := randomevents.Lock(d.Session)
	defer unlock()
	d.logf("OPLOC%d on bank booth %d at %d,%d", booth.Op, booth.ID, booth.X, booth.Z)
	clickAt := time.Now()
	if err := actions.OpLoc(d.Session, booth.Op, booth.X, booth.Z, booth.ID); err != nil {
		d.logf("OpLoc: %v", err)
		return 0, false
	}
	bank := actions.NewBank(d.Session, d.Components)
	if !waitFor(d, "bank to open", 8*time.Second, bank.IsOpen) {
		main, sub, tab := d.Session.State().Modals()
		d.logf("  DIAG bank-open timeout: modals main=%d sub=%d tab=%d chat=%q", main, sub, tab, d.Chat.Since(clickAt))
		return 0, false
	}
	defer bank.Close()
	if !d.Delay(500 * time.Millisecond) { // let UPDATE_INV_FULL for both panels land
		return 0, false
	}

	deposited := 0
	seen := map[int32]bool{}
	pack := bank.Inventory()
	for _, it := range pack {
		if seen[it.ID] {
			continue
		}
		obj, ok := d.Config.Obj(int(it.ID))
		name := ""
		if ok {
			name = obj.Name
		}
		if donorplan.InKeep(name, keep) {
			continue
		}
		seen[it.ID] = true
		heldBefore := int(sumByID(pack, it.ID))
		before := sumByID(bank.Items(), it.ID)
		if err := bank.DepositAll(it.ID); err != nil {
			d.logf("deposit %q: %v", name, err)
			continue
		}
		if waitFor(d, fmt.Sprintf("bank shows %s after deposit", name), 5*time.Second, func() bool {
			return sumByID(bank.Items(), it.ID) > before
		}) {
			deposited += heldBefore
			d.logf("deposited %d %s", heldBefore, name)
		}
	}
	return deposited, true
}

// sumByID totals the count held across every slot matching id — bow strings and flax
// aren't stackable, so a batch of either sits in several separate slots.
func sumByID(items []gamestate.SlotItem, id int32) int32 {
	var n int32
	for _, it := range items {
		if it.ID == id {
			n += it.Count
		}
	}
	return n
}
