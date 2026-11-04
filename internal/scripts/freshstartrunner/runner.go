// Package freshstartrunner is the I/O half of internal/scripts/freshstart: it drives that
// package's pure "brand-new account's first day" decisions (FareThief pickpocketing,
// TravelLeg/BoatLeg's two-ship route, the appearance screen, the tutorial skip, the
// brightness setting, the Lumbridge general store) against a live session.
//
// It is deliberately script-agnostic — freshstart itself is already destination-parameterized
// (Road{Destination: ...}) and shared by more than one script's pure logic (fisherplan's own
// PhaseFund/PhaseTravel call freshstart's FareRemaining/InLumbridge/TravelLeg directly;
// crafterplan's Road-based DecidePhase uses the same package for its own wool-then-boats
// first day) — so this runner is written once here rather than as a fisher-only copy, the
// same way barbarianrunner/fisherrunner/mulerunner each extracted one script's own live
// mechanics into a shared package other tools/bots can call.
//
// Why this exists at all: every one of ProgressiveFisher.ts's/ProgressiveCrafter.ts's phases
// that get an account from a cold Lumbridge start to its workplace is ALSO, unlisted and
// unremarked in the TS source, that same script's own death-recovery path — a death drops an
// account back near Lumbridge with little or no money, which is exactly the "short of the
// fare" state DecideFishPhase's/freshstart.DecidePhase's PhaseFund/PhaseTravel already detect
// and resolve on their own, with no separate "recover from death" code anywhere in the
// TS originals. There is nothing to invent here beyond wiring the already-ported, already
// unit-tested pure logic (freshstart.FareThief, freshstart.ChooseTarget, freshstart.NoTargetAction,
// freshstart.TravelLeg/BoatLeg) to real actions — the same "pure logic exists, never driven
// live" gap found repeatedly tonight (barbarianplan.DecideStage, donorplan.Handover).
//
// Honest limitation, not papered over: the boat leg itself (Port Sarim -> Karamja ->
// Brimhaven -> Ardougne -> destination) is, in the TS original, nothing more than one
// Traversal.walkResilient call to the leg's destination tile — the TS client's own web-walk
// silently handles boarding both ships as ordinary graph edges. internal/pathfind has no
// such transport-graph support yet (the same documented "v1: no doors/transports" gap
// BarbarianAgility's Seers bank trip and ProgressiveCrafter's ladder both hit live tonight),
// so SailLeg below can only ever succeed if the plain tile-to-tile walk it delegates to
// happens to not need a ship — otherwise it fails clearly (logging the pathfind reason) rather
// than pretending to travel. Closing that gap for real is the same standing "highest-leverage
// next infrastructure investment" everyone hit tonight, not something this package works
// around on its own.
package freshstartrunner

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// LocalRescueTeleport, when true, makes walkNearCore's STRANDED detection (see its own doc
// comment) send a real ::tele cheat back to LocalRescueTile instead of just reporting and
// giving up — for unattended LOCAL TEST ENGINE soaks only, where an account trapped in a
// genuinely sealed pocket (most likely a random-event fail-teleport destination — confirmed
// live 2026-09-15 via cmd/doordiag against several real stranding locations, each a small,
// fully disconnected connected component with no walkable exit at all) would otherwise sit
// idle forever with no way to path out on its own.
//
// Defaults to false and is never set by anything in this package — only cmd/controlpanel's
// own explicit, opt-in CLI flag (see its own flag registration) turns this on, and only that
// one process decides to. There is no equivalent cheat on example.invalid production at all
// (::tele requires a staff-level account no real production login has), so this can never
// silently activate against a live account even if the flag were passed by mistake — the
// OpLoc-equivalent Cheat() call would just get ignored/rejected server-side. Still gated
// behind an explicit flag rather than relying on that alone, on the same "belt and
// suspenders" principle every other local-only cheat in this codebase already follows
// (cmd/gatetest, cmd/npcscan, cmd/guidecrash's own doc comments all say "LOCAL-ENGINE-ONLY"
// even though none of them could functionally do anything on production either).
var LocalRescueTeleport = false

// LocalRescueTile is where LocalRescueTeleport sends a stranded account back to — the
// Lumbridge castle courtyard, the same well-connected, high-traffic tile freshstart's own
// PickpocketSpot/LumbridgeCentre already treat as a safe, reliable hub.
var LocalRescueTile = tile.Tile{X: 3222, Z: 3218, Level: 0}

// dangerZone is a circular (Chebyshev-radius) area this package's own routing should never
// walk a route through — see dangerZones below and findPathAvoidingDanger.
type dangerZone struct {
	Center tile.Tile
	Radius int
	Note   string
}

// dangerZones lists areas this package's own routing should hard-avoid. Deliberately
// empty right now: a 2755,3402,0-centered zone was requested (giant bats killing fresh
// accounts en route to Catherby) but checked BEFORE shipping via cmd/dangerzonecheck
// against the real collision pack, and any radius of 2 tiles or more makes the whole
// Catherby leg unreachable — that area isn't open ground with room to route around, it's
// a walled compound with a single gate near the zone's center, and the only walkable path
// funnels straight through it (radius 1 is the largest value that doesn't break the
// route, which isn't wide enough to matter for actually avoiding a hostile NPC). A hard
// avoid-by-radius can't solve this without a genuinely different, longer overland or
// boat route existing in this collision pack, which hasn't been found. Left as an empty,
// safe no-op rather than shipping something that breaks travel to Catherby entirely —
// see COLLAB.md for the full finding and the options considered.
var dangerZones = []dangerZone{}

// inDangerZone reports whether x,z,level falls inside any configured dangerZones entry.
func inDangerZone(x, z, level int) bool {
	for _, z2 := range dangerZones {
		if z2.Center.Level != level {
			continue
		}
		if tile.Chebyshev(tile.Tile{X: x, Z: z, Level: level}, z2.Center) <= z2.Radius {
			return true
		}
	}
	return false
}

// findPathAvoidingDanger is this package's one on-ramp to internal/pathfind's FindPath —
// both call sites in this file (walkNearCore's own main search and tryOpenDoor's
// through-tile FindPath check) go through this instead of calling d.Pack.FindPath
// directly, so a configured dangerZones entry is honored everywhere this package computes
// a route, not just in one call site that happens to remember to check it. It also honours
// lowCombatZoneIDs (lowcombatzones.go) for this account's combat level, except out of a pocket
// those zones wall in (planAvoidingZones).
func findPathAvoidingDanger(d *Deps, from, to pathfind.NavPoint) pathfind.PathResult {
	res, escaped := planAvoidingZones(liveNavigationPack(d), from, to, zoneAvoid(d, from, to))
	if escaped {
		d.logf("%d,%d,%d is walled in by the danger zones, with no way out that stays clear of them: the route to %d,%d,%d leaves through one", from.X, from.Z, from.Level, to.X, to.Z, to.Level)
	}
	return res
}

// Delayer sleeps for d, returning false early if the caller should stop instead of true
// once d has fully elapsed. Same shape as barbarianrunner.Delayer/fisherrunner.Delayer/
// mulerunner.Delayer, deliberately — every runner package in this project is a sibling.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration and never cancels — for a standalone tool with
// no runner to request a stop.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// emergencyFleeHome/emergencyFleeDriftTiles: this package opts OUT of EmergencyFlee's own
// drift check (driftTiles 0 — see that function's own doc comment for why every caller now
// supplies its own pair instead of this package assuming every script operates near
// Catherby), on purpose, for two reasons specific to this package: it is genuinely
// script-agnostic (shared bootstrap logic for fisher/barbarian/crafter's own first-day
// flow, with no single fixed "home" of its own to check against), and its own calls happen
// DURING supervised long-distance travel (SailLeg, a cross-map WalkNear) — exactly the
// "actively, legitimately walking a long way right now" case a fixed-reference drift check
// cannot distinguish from a genuine stranding (see driftObservation's own doc comment in
// randomevents/handle.go for the live bug that shape of false positive already caused).
// This package's own checkFailTeleport already covers fail-teleport detection correctly
// here, comparing a real before/after tile pair per travel leg instead of a fixed global
// point — EmergencyFlee's InCombat branch (unaffected by driftTiles<=0) still runs.
var emergencyFleeHome pathfind.NavPoint // unused while emergencyFleeDriftTiles <= 0

const emergencyFleeDriftTiles = 0

// Deps is everything one first-day step needs from the outside world.
type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	// Log receives one already-formatted line per notable event; nil drops them.
	Log func(string)
	// Walker plans over webwalk's door/gate/stair/ship-fare graph — see walkWithWalker's own
	// doc comment for what having one changes. Optional and nil-safe: every existing caller
	// that never sets it keeps exactly today's plain-pathfind-only behavior.
	Walker *webwalk.Walker
	// Loot receives what each successful pickpocket really took (PickpocketStep); nil records
	// nothing. Set from bot.Context.Loot by the scripts that pickpocket for their fare.
	Loot func(loot.Drop)
	// Aside, when set, is the walk's side work (webwalk.WalkOptions.Aside) for the walks that go through
	// the Walker: called once a poll of a straight leg, on the walk's goroutine, so it must return
	// quickly and send no movement. The fishers' drop cycle and run stall use it
	// (fisherrunner.walkNearUntil). Nil changes nothing.
	Aside func(webwalk.AsidePoll)
}

// walkWithWalker is freshstartrunner's own copy of crafterrunner.Walk's fallback shape: with a
// Walker, plan over webwalk's door/gate/stair/ship-fare graph (the real Brimhaven->Ardougne
// Customs-officer ship edge, 30-coin fare, is already in internal/webwalk/graph.json); without
// one, fall back to the plain WalkNear this package has always used. Real, live-reproduced gap
// this closes: an account stranded at the Brimhaven pier short of boat fare, with no local
// pickpocket target, used to walk toward the mainland PickpocketSpot fallback forever — that
// tile is physically unreachable from Karamja by plain same-level pathfind (internal/pathfind
// has no ship-crossing edges at all, see this file's own top-of-file "Honest limitation" doc
// comment), so the walk failed identically every single retry with no way to ever recover.
// Kept deliberately narrow to this one call site rather than threading Walker through every
// WalkNear call in this file — a broader migration is real, separate work, not something to
// fold in unreviewed alongside a targeted bug fix.
func walkWithWalker(d *Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	return walkWithWalkerUntil(d, dest, radius, timeout, nil)
}

// WalkWithWalkerUntil is WalkWithWalker that also ends, as arrived, once until reports true.
func WalkWithWalkerUntil(d *Deps, dest tile.Tile, radius int, timeout time.Duration, until func() bool) bool {
	return walkWithWalkerUntil(d, dest, radius, timeout, until)
}

func walkWithWalkerUntil(d *Deps, dest tile.Tile, radius int, timeout time.Duration, until func() bool) bool {
	maybeEnableRun(d)
	if d.Walker == nil {
		if until == nil {
			return WalkNear(d, dest, radius, timeout)
		}
		met := false
		fd := *d
		fd.Delay = func(dur time.Duration) bool {
			if until() {
				met = true
				return false
			}
			return d.Delay(dur)
		}
		return WalkNear(&fd, dest, radius, timeout) || met
	}
	// AvoidHostiles is the same pre-emptive break-off walkNearCore's own poll runs (see
	// hostileevade.go). Without it here, an account that reaches Catherby through this Walker
	// instead of through walkNearCore had no cover from the White wolves at all.
	res := d.Walker.WalkTo(webwalk.Point{X: dest.X, Z: dest.Z, Level: dest.Level}, webwalk.WalkOptions{Radius: radius, Timeout: timeout, AvoidHostiles: HostileEvadeEnabled, Until: until, Aside: d.Aside})
	if res.Arrived() {
		return true
	}
	d.logf("walk to %d,%d,%d did not arrive (%v: %s), stopped at %v", dest.X, dest.Z, dest.Level, res.Outcome, res.Reason, res.End)
	return false
}

// strandedWalkerEscape hands a STRANDED walk to the Walker, whose webwalk graph opens the doors and
// gates internal/pathfind cannot plan through. It reports whether the walk arrived and whether the
// account moved at all; neither, and the caller's own escapes run as before. No Walker, or no time
// left, does nothing.
func strandedWalkerEscape(d *Deps, from, dest tile.Tile, radius int, timeout time.Duration) (arrived, moved bool) {
	if d.Walker == nil || timeout <= 0 {
		return false, false
	}
	d.logf("rescue: handing the walk to %d,%d,%d to the webwalk route, which opens doors and gates the plain route cannot", dest.X, dest.Z, dest.Level)
	res := d.Walker.WalkTo(webwalk.Point{X: dest.X, Z: dest.Z, Level: dest.Level}, webwalk.WalkOptions{Radius: radius, Timeout: timeout, AvoidHostiles: HostileEvadeEnabled})
	if res.Arrived() {
		d.logf("rescue: the webwalk route arrived at %d,%d,%d", dest.X, dest.Z, dest.Level)
		return true, true
	}
	cur := currentTile(d)
	moved = cur != nil && *cur != from
	d.logf("rescue: the webwalk route to %d,%d,%d did not arrive (%v: %s), stopped at %v", dest.X, dest.Z, dest.Level, res.Outcome, res.Reason, res.End)
	return false, moved
}

// WalkWithWalker exports walkWithWalker for sibling runner packages that bridge their own
// Deps into this package's Deps for shared door/level/pocket recovery (fisherrunner.WalkNear
// via its own freshStartDeps helper is the first caller) but weren't themselves passing a
// Walker through. Live-found 2026-09-18: an account already past its first day (ProgressiveFisher's
// ordinary steady-state fishing/banking loop, not the fresh-start flow) that gets displaced
// across a real cross-region gap -- confirmed via cmd/doordiag against the real production
// pack: crafter1's own stranded tile sat in a genuine 216,661-tile connected component
// that does not include Catherby bank's own 109,716-tile component, even though their
// bounding boxes overlap -- had no access to this package's own long-distance Walker at all,
// because fisherrunner.Deps never had a Walker field to forward through freshStartDeps in the
// first place. Only a brand-new account's own first-day flow (which already gets a real
// live.NewWalker wired in internal/bot/fisher.go's OnStart) benefited from long-distance
// routing before this; an established account's ordinary WalkNear calls used plain
// same-level FindPath only, exactly like the original Brimhaven/Karamja fare-recovery gap
// this same mechanism was built to close, just for everyday navigation instead of a specific
// first-day script step.
func WalkWithWalker(d *Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	return walkWithWalker(d, dest, radius, timeout)
}

func (d *Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

func currentTile(d *Deps) *tile.Tile {
	x, z, level, ok := d.Session.State().Tile()
	if !ok {
		return nil
	}
	return &tile.Tile{X: x, Z: z, Level: level}
}

// rescueTeleport sends a real ::tele cheat to LocalRescueTile and waits for it to land —
// only ever called from walkNearCore's STRANDED branch, and only when LocalRescueTeleport
// is true (see that var's own doc comment for why this is safe to leave compiled into every
// build rather than needing a build tag: it is inert unless a caller explicitly opts in, and
// even then ::tele itself requires a staff-level account no production login has). from is
// logged purely for the "before" side of the rescue log line, not otherwise used.
func rescueTeleport(d *Deps, from tile.Tile) bool {
	t := LocalRescueTile
	d.logf("rescue: ::tele from %d,%d,%d to %d,%d,%d", from.X, from.Z, from.Level, t.X, t.Z, t.Level)
	if err := actions.Cheat(d.Session, fmt.Sprintf("tele %d,%d,%d,%d,%d", t.Level, t.X>>6, t.Z>>6, t.X&0x3f, t.Z&0x3f)); err != nil {
		d.logf("rescue: ::tele command failed: %v", err)
		return false
	}
	return waitFor(d, "the rescue teleport to land", 5*time.Second, func() bool {
		cur := currentTile(d)
		return cur != nil && cur.X == t.X && cur.Z == t.Z && cur.Level == t.Level
	})
}

// lumbridgeTeleportSpellName is the standard-spellbook teleport actions.Teleport recognizes
// ("lumbridge" -> component 1167, level 31 Magic, air/earth/law runes — see
// internal/actions/magic.go's teleportSpells table) — the one real, unprivileged in-game
// action available to a production account (no staff ::tele) that can escape a genuinely
// sealed pocket a teleport spell bypasses collision/pathing entirely, the same way it would
// for a real player caught in one of these. Not guaranteed to work for any given account —
// a fresh/low-level character may lack the Magic level or the runes — but worth a real
// attempt before giving up, which the old STRANDED branch never made at all.
const lumbridgeTeleportSpellName = "lumbridge"

// spellRescueTimeout bounds how long attemptSpellRescue waits for the cast to actually move
// the player, matching internal/bot's own Context.Teleport default (see that function's doc
// comment) — long enough to cover the cast animation with room to spare, short enough not to
// eat much of the STRANDED backoff if it fails outright (no runes/level: the server rejects
// instantly, so this mostly returns fast anyway).
const spellRescueTimeout = 10 * time.Second

// spellRescueArrivalDistance mirrors internal/bot's own teleportArrivalDistance: how far
// (tiles, either axis) the player must move for a cast to count as having actually landed,
// distinguishing a real teleport from mere jitter/rounding in tracked position.
const spellRescueArrivalDistance = 10

// attemptSpellRescue casts lumbridgeTeleportSpellName and waits to see whether the player
// actually moved — the real-in-game-action recovery walkNearCore's own STRANDED branch never
// had before this (see rescueTeleport's own doc comment for why ::tele can't help here: no
// production account has staff level). Returns true only on a confirmed move (a different
// tile, more than spellRescueArrivalDistance away on either axis, or a level change) within
// spellRescueTimeout. A missing rune or too-low Magic level shows up as false with the
// server's own reason visible in the account's chat log, not as an error here — the same
// "arrived bool, not a hard error" contract internal/bot.Context.Teleport already uses, kept
// self-contained here (not calling into internal/bot) since internal/bot itself imports this
// package (internal/bot/{fisher,crafter,mule}.go all call into freshstartrunner) — importing
// it back here would be a cycle.
//
// It casts only what the account can cast (spellrescue.go): below Magic 31, or short of the law,
// earth or air runes a wielded staff does not pay for, it logs that one reason and returns
// cast=false at once, with no packet sent and no landing wait. cast=true means a cast was sent and
// landed reports whether it moved the account.
func attemptSpellRescue(d *Deps, from tile.Tile) (landed, cast bool) {
	if why := spellRescueRefusal(d); why != "" {
		d.logf("rescue: not casting Lumbridge Teleport from %d,%d,%d: %s — this account needs an operator to look at it", from.X, from.Z, from.Level, why)
		return false, false
	}
	d.logf("rescue: attempting to cast Lumbridge Teleport (no production ::tele available) from %d,%d,%d", from.X, from.Z, from.Level)
	if err := actions.Teleport(d.Session, lumbridgeTeleportSpellName); err != nil {
		d.logf("rescue: casting Lumbridge Teleport failed: %v", err)
		return false, true
	}
	landed = waitFor(d, "the Lumbridge Teleport cast to land", spellRescueTimeout, func() bool {
		cur := currentTile(d)
		if cur == nil {
			return false
		}
		if cur.Level != from.Level {
			return true
		}
		dx, dz := cur.X-from.X, cur.Z-from.Z
		if dx < 0 {
			dx = -dx
		}
		if dz < 0 {
			dz = -dz
		}
		return dx > spellRescueArrivalDistance || dz > spellRescueArrivalDistance
	})
	return landed, true
}

// pocketStairSearchRadius bounds how far tryEscapeKnownPocket looks for a real
// staircase/ladder out of a random-event fail-teleport pocket — generous enough to cover
// the largest pocket ground-truthed so far ("lubmy basement": a 79-tile, roughly 12x11
// room; its real "Ladder" sits 3 tiles Chebyshev from the live acct24 incident's own
// tile, confirmed offline against a real collision pack + baked loc index — see
// failteleport.go's own doc comment for the exact evidence) with real headroom, without
// being so large it could ever reach into a completely different room the account isn't
// actually standing in. Matches tryChangeLevel's own stair-search radius (25) plus a small
// margin rather than inventing a different tuning from scratch.
const pocketStairSearchRadius = 30

// tryEscapeKnownPocket looks for a real staircase/ladder near `from` and, if the collision
// pack shows a genuine walkable route to stand beside it, climbs it — a real, unprivileged,
// Magic-level/rune-independent escape from a random-event fail-teleport "underground
// counterpart" pocket (see randomevents.DescribeStrandedLocation/
// LooksLikeUndergroundMapsquare's own doc comments), built specifically because
// attemptSpellRescue below can never actually succeed for a fresh/low-level fleet (Magic
// level 1, no runes — confirmed live twice already, accounts "acct24"/"acct63") even
// though the STRANDED branch now correctly labels the condition.
//
// Deliberately reuses stairNames/stairOpsUp and pickDirectionalMatch/walkAdjacent/OpLoc
// verbatim — the exact same fixture-search machinery tryChangeLevel already uses for an
// ordinary multi-level stranding — rather than a hand-maintained per-mapsquare id/tile
// table. Every pocket this session could ground-truth offline (cmd/doordiag plus a
// throwaway locindex/pathfind dump against a real baked collision pack and loc index, no
// live session needed — see failteleport.go's own doc comment for the exact evidence)
// turned out to be a perfectly ordinary "Ladder"/"Staircase" object with a perfectly
// ordinary "Climb-up" op; the ONE thing that makes these pockets different from an
// ordinary same-level-mismatch stranding is that tryChangeLevel's own
// `from.Level != dest.Level` gate can never fire for them — a pocket shares its surface
// counterpart's own level NUMBER (only mapsquareZ is offset +100, this content build's own
// convention — see failteleport.go). This function is simply tryChangeLevel's fixture
// search with that gate removed. NOTE (correcting an earlier version of this comment): the
// one call site in walkNearCore actually calls this unconditionally on every STRANDED
// escalation, not only when DescribeStrandedLocation already recognizes the spot as
// pocket-shaped — see the real live incident (account "acct", Ardougne Monastery ground
// floor, an ORDINARY surface building, not a z+6400 pocket) documented on leftPocket's own
// doc comment, which hit this exact function via that unconditional call. A false "looks
// like a pocket" guess still costs nothing beyond one failed walkAdjacent — this returns
// false exactly like tryChangeLevel/tryOpenDoor do when nothing pans out, letting the caller
// move on to attemptSpellRescue exactly as before.
//
// Ground-truthed offline this session: the "lubmy basement" pocket (mapsquare 50,150) has
// a real "Ladder" (id 1755, op "Climb-up") 3 tiles from the live acct24 incident's own
// tile, with a genuine 6-waypoint FindPath route to stand beside it — climbing it lands
// beside a matching "Trapdoor" (id 1568, op "Open") at the same x,z minus 6400 in z, back
// on the ordinary surface, exactly the "real staircase down there for a human player"
// shape this whole approach assumed. The Wizards' Tower basement pocket (mapsquare
// 48,149) does have a "wizards_tower_ladder" object nearby (content/pack/loc.pack id
// 2148), but the collision pack shows a wall between the landing room's own connected
// component and that object's tile — no confirmed route exists there yet with the
// CURRENT pack (which may simply be under-explored for this mapsquare, not proof the
// real room has no exit at all — see failteleport.go's own doc comment for the honest
// caveat). This function still tries there too; it just currently returns false for
// that one specific pocket, same as it would for any other unverified/genuinely sealed
// one, falling through to attemptSpellRescue unchanged.
func tryEscapeKnownPocket(d *Deps, from tile.Tile) bool {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	// A reachable gangplank goes first: a ship deck is the pocket this was mistaken for most often
	// (reachablefixture.go), and a gangplank only ever leads down onto the dock. Then a stair the
	// account can actually reach, then the old nearest stair.
	pack := liveNavigationPack(d)
	var found *locindex.Match
	var foundName string
	if from.Level > 0 {
		// Only from a raised floor: on the ground a gangplank BOARDS a ship (review nit).
		found, foundName = pickReachableMatch(pack, d.Locs, d.Config, live, from, gangplankNames, gangplankOps, pocketStairSearchRadius)
	}
	// The way out of a pocket on a raised floor is DOWN. Live 2026-10-02, Rogues' Castle: a stair there
	// offers Climb, Climb-up and Climb-down, so the old always-up order climbed from the second floor
	// back to the roof it had just come down from ("pocket escape: trying Staircase at 3280,3936,2" then
	// "only reached another sealed room at 3281,3936,3"), and on a ground floor the up order is still the
	// one the basement ladders need.
	ops := pocketStairOps(from.Level)
	if found == nil {
		found, foundName = pickReachableMatch(pack, d.Locs, d.Config, live, from, stairNames, ops, pocketStairSearchRadius)
	}
	if found == nil {
		found, foundName = pickDirectionalMatch(d.Locs, d.Config, live, from, stairNames, ops, pocketStairSearchRadius)
	}
	if found == nil {
		return false
	}
	d.logf("pocket escape: trying %q at %d,%d,%d", foundName, found.X, found.Z, found.Level)
	reached := false
	for _, off := range doorApproach {
		if walkAdjacent(d, tile.Tile{X: found.X + off.dx, Z: found.Z + off.dz, Level: found.Level}, 0, 8*time.Second) {
			reached = true
			break
		}
	}
	if !reached {
		d.logf("pocket escape: %q at %d,%d,%d has no reachable approach tile from here — no confirmed route out this way", foundName, found.X, found.Z, found.Level)
		return false
	}
	if err := actions.OpLoc(d.Session, found.Op, found.X, found.Z, found.ID); err != nil {
		d.logf("pocket escape: OpLoc on %q: %v", foundName, err)
		return false
	}
	landed := waitFor(d, "the pocket escape climb to land", 5*time.Second, func() bool {
		cur := currentTile(d)
		return cur != nil && leftPocket(from, *cur)
	})
	if !landed {
		return false
	}
	d.Delay(1500 * time.Millisecond)
	// Why: a level change is not progress when it lands in a room just as sealed as the one it
	// left. See escapedToOpenGround for the Ardougne Monastery staircase pair this rejects,
	// where reporting the dead-end climb as a rescue skipped the door chain that is the
	// building's actual exit.
	after := currentTile(d)
	if after == nil {
		return true
	}
	if !escapedToOpenGround(d.Pack, from, *after, largeComponentThreshold) {
		d.logf("pocket escape: %q at %d,%d,%d only reached another sealed room at %d,%d,%d, not an escape — falling through to the other rescues", foundName, found.X, found.Z, found.Level, after.X, after.Z, after.Level)
		return false
	}
	return true
}

// pocketStairOps is the op order tryEscapeKnownPocket searches a fixture by: down from a raised floor,
// up from the ground (a basement's ladder is a Climb-up). See the call site for the castle incident.
func pocketStairOps(fromLevel int) []string {
	if fromLevel > 0 {
		return stairOpsDown
	}
	return stairOpsUp
}

// leftPocket reports whether `after` looks like a genuine escape from wherever `before` was
// — tryEscapeKnownPocket's own success check. Originally named leftPocketMapsquare and
// checked ONLY a mapsquare change, on the reasoning that a pocket escape can never be
// confirmed by a level change the way tryChangeLevel's is, since a z+6400-offset pocket
// shares its surface counterpart's own level NUMBER (see tryEscapeKnownPocket's own doc
// comment) — a real climb out of one of THOSE pockets is a big jump (at minimum 6400 tiles
// in z for the two confirmed so far), so "moved to a different mapsquare at all" was a safe,
// simple, false-positive-free check for that one specific shape.
//
// Real, live-found bug in that original version (2026-09-17, production account "acct",
// Ardougne Monastery ground floor, mapsquare 40,50 — reproduced live via
// cmd/fisherstrandtest -x 2617 -z 3207): tryEscapeKnownPocket is called unconditionally from
// every STRANDED escalation, not only for the z+6400-offset "known pocket" shape — an
// ORDINARY surface building with a real staircase to the ordinary room directly above hits
// it too (tryEscapeSealedPocket's own doc comment already documents this same mapsquare,
// account "acct63", as exactly this shape). For that shape, a successful climb changes
// LEVEL (0 -> 1) while staying in the SAME mapsquare — the old mapsquare-only check reported
// this as a FAILED climb ("timed out waiting for the pocket escape climb to land") even
// though the OpLoc genuinely landed the account on level 1, confirmed live: the very next
// line, tryEscapeSealedPocket's own fresh room search, computed its door chain from
// 2618,3208,1 — level 1, not the level-0 tile the misreported "failure" implied. That
// door-chain search then (by design — see tryEscapeSealedPocket's own doc comment) still
// used the STALE pre-climb `*here` snapshot walkNearCore's caller passed to it, so it
// correctly found the real, ground-truthed level-0 door chain (2610,3209 then 2606,3219) but
// then failed to walk to it ("no path from 2618,3208,1 to 2611,3209,0: unreachable" — level 1
// to level 0, a level pathfind can never cross), forcing a fallback all the way to
// attemptSpellRescue every single cycle instead of the caller simply re-deriving its route
// from the account's real (now level 1) position the moment the climb actually landed.
//
// Fixed by treating a level change as an equally valid escape signal alongside a mapsquare
// change: a staircase/ladder climb that lands the account on a different mapsquare OR a
// different level is real, verified progress out of wherever `before` was — ordinary in-room
// movement from a single OpLoc click (the one shape this check must still reject) never
// changes either.
func leftPocket(before, after tile.Tile) bool {
	return before.X>>6 != after.X>>6 || before.Z>>6 != after.Z>>6 || before.Level != after.Level
}

// escapedToOpenGround is tryEscapeKnownPocket's real success test; leftPocket alone is not
// it. Ground-truthed at the Ardougne Monastery (mapsquare 40,50) with cmd/doordiag against
// the real collision pack and a freshly baked loc index: the ground-floor room (61 tiles,
// 2611-2620,3204-3214) holds exactly ONE staircase, id 1738, and its only op is "Climb-up".
// tryEscapeKnownPocket searches stairOpsUp, so that is the one fixture it can ever pick
// there, and climbing it reaches the level-1 bedroom above, itself a sealed 24-tile room
// (2613-2618,3207-3211) with no door within 20 tiles and only the matching "Climb-down"
// staircase 1740 leading back down again. Confirmed live with cmd/guidecrash -testloc:
// OpLoc("Climb-up", 1738) from 2616,3207,0 lands the account on 2618,3208,1, inside that
// bedroom. leftPocket accepts any level change, so that dead-end climb reported as a
// successful rescue: the caller returned before tryEscapeSealedPocket could run (the
// two-door chain at 2610,3209 then 2606,3219 is the building's real exit, and it is on level
// 0), and the account was back on level 1, where tryChangeLevel correctly descends again.
// One `changing level via "Staircase" at 2617,3208,1` per cycle, which is the shape reported
// in COLLAB.md.
//
// So "the fixture moved us" is necessary but not sufficient. A mapsquare change still counts
// on its own: the z+6400-offset basement pockets jump at least 6400 tiles in z and their
// surface landing is ordinary open ground by construction (the lubmy Ladder/Trapdoor pair).
// A level change WITHIN the same mapsquare only counts when the landing tile's own connected
// component clears threshold, which is what tells an ordinary staircase back out to the world
// apart from a staircase into one more sealed room. Nil-safe: with no pack there is nothing
// to measure, so this degrades to leftPocket's own answer rather than refusing every escape.
func escapedToOpenGround(pk *pathfind.Pack, before, after tile.Tile, threshold int) bool {
	if !leftPocket(before, after) {
		return false
	}
	if pk == nil || before.X>>6 != after.X>>6 || before.Z>>6 != after.Z>>6 {
		return true
	}
	return len(localComponent(pk, after, threshold)) >= threshold
}

// maxPocketChainHops bounds how many doors in a row tryEscapeSealedPocket's own room-BFS
// (see its own doc comment) will cross while hunting for the real way out of a small,
// disconnected interior — generous enough for the deepest real incident ground-truthed so
// far (account "acct63", mapsquare 40,50: two closed "Door" locs in a row — the account's
// own sealed room at 2611-2620,3204-3214, then a second sealed room at 2602-2610,3207-3218,
// then a real "Door" at 2606,3219 opening onto a genuine 109,716-tile outdoor component —
// see this function's own doc comment) with real headroom for a deeper building this session
// never saw, without letting a dense multi-door complex explode into an unbounded search.
const maxPocketChainHops = 6

// pocketChainRoomCap bounds localComponent's own per-room flood-fill size during the chain
// search below. Large enough to fully measure every real interior room ground-truthed so
// far (the biggest, a Varrock-Palace-style multi-room suite found live this session, was
// 244 tiles) with a lot of headroom, while still being far cheaper than flood-filling an
// entire real outdoor landmass (109,716+ tiles) for every intermediate room tried — once a
// room's own fill reaches largeComponentThreshold below, that alone already answers "is
// this the real world," so this cap only needs to be bigger than any genuine SMALL interior
// room, never bigger than the biggest real outdoor area.
const pocketChainRoomCap = 4000

// largeComponentThreshold is how big a room's own raw ExitMask-connected component has to
// be before tryEscapeSealedPocket treats it as "the real outdoor world" rather than just
// another interior room worth chaining through — ground-truthed against every real room
// size seen this session: sealed interior pockets/rooms range from 24 tiles (a small
// bedroom) up to 244 tiles (a real multi-room suite), while a real outdoor component is
// 109,716 tiles — several orders of magnitude apart. Set with a lot of headroom above the
// largest known interior room and a lot of headroom below any real outdoor area, so neither
// a bigger interior complex nor a smaller real outdoor pocket this session never saw is
// likely to be misclassified.
const largeComponentThreshold = 2000

// pocketChainDoorRadius is how far past a room's own bounding box tryEscapeSealedPocket
// looks for the door/gate that leads out of it — matches pickDoorCandidateRadius's own
// reasoning (see its doc comment) but is added as a margin around the room's own extent
// (see findDoorsBoundingRoom) rather than measured from a single tile, since a room can be
// bigger than one tile and its exit doors sit anywhere on its boundary, not just near
// wherever the account happens to be standing.
const pocketChainDoorRadius = 20

// doorChainOps is every op tryEscapeSealedPocket's own door search tries on each name in
// doorNames — both the closed-door "Open" ops (doorOps) and the already-open "Close" ops
// (doorOpenOps), the same combination pickDoorCandidate already searches for the identical
// reason (see doorOpenOps' own doc comment): a chain search must not miss a door only
// because it happens to already be open.
var doorChainOps = append(append([]string{}, doorOps...), doorOpenOps...)

// doorChainHop is one link in the door chain tryEscapeSealedPocket's own room-BFS finds —
// the door/gate loc crossed, the tile just inside it (where the account approaches from to
// open it) and the tile just outside it (in the next room), so the walk phase can retrace
// the exact same chain the search already verified room-by-room.
type doorChainHop struct {
	match locindex.Match
	from  tile.Tile
	to    tile.Tile
}

// roomBBox returns the smallest box containing every tile in room — tryEscapeSealedPocket's
// own room-BFS uses this both to bound the next door search (see findDoorsBoundingRoom) and
// to recognize a room it has already visited (see its own visited map), rather than
// comparing the full tile sets themselves.
func roomBBox(room map[tile.Tile]bool) [4]int {
	first := true
	var bbox [4]int
	for t := range room {
		if first {
			bbox = [4]int{t.X, t.Z, t.X, t.Z}
			first = false
			continue
		}
		if t.X < bbox[0] {
			bbox[0] = t.X
		}
		if t.Z < bbox[1] {
			bbox[1] = t.Z
		}
		if t.X > bbox[2] {
			bbox[2] = t.X
		}
		if t.Z > bbox[3] {
			bbox[3] = t.Z
		}
	}
	return bbox
}

// findDoorsBoundingRoom searches every door/gate name+op combination (see doorChainOps)
// within pocketChainDoorRadius of room's own bounding box, centered and sized off that box
// rather than a single tile — a real room can be many tiles across, and its exit doors can
// sit anywhere on its boundary, not just near wherever the search happens to start.
// Deduplicates by (X, Z, ID) since the same physical door can match more than one
// name/op combination search.
func findDoorsBoundingRoom(d *Deps, live []locindex.Change, bbox [4]int, level int) []locindex.Match {
	cx := (bbox[0] + bbox[2]) / 2
	cz := (bbox[1] + bbox[3]) / 2
	halfW := (bbox[2] - bbox[0]) / 2
	halfH := (bbox[3] - bbox[1]) / 2
	radius := halfW
	if halfH > radius {
		radius = halfH
	}
	radius += pocketChainDoorRadius
	type key struct {
		x, z, id int
	}
	seen := map[key]bool{}
	var out []locindex.Match
	for _, name := range doorNames {
		for _, op := range doorChainOps {
			for _, m := range d.Locs.Find(d.Config, locindex.Query{X: cx, Z: cz, Level: level, Radius: radius, Name: name, Op: op}, live) {
				if guardianOwned(m) {
					continue
				}
				k := key{m.X, m.Z, m.ID}
				if seen[k] {
					continue
				}
				seen[k] = true
				out = append(out, m)
			}
		}
	}
	return out
}

// tryEscapeSealedPocket looks for a real chain of doors/gates leading out of a small,
// genuinely disconnected interior — built for exactly the gap tryEscapeKnownPocket's own
// single-stair search and tryOpenDoor's own single-door search each leave open: a real
// multi-room interior where NO single fixture near the account's own tile leads directly
// back to the wider world, but a real, walkable chain of two or more doors does.
//
// Ground-truthed live this session, account "acct63" (mapsquare 40,50, an ORDINARY surface
// mapsquare — not one of the z+6400-offset basement pockets tryEscapeKnownPocket's own
// stranding list already covers): the account's own sealed room (2611-2620,3204-3214, 61
// tiles) has a real "Staircase" (id 1738, "Climb-up") a couple of tiles from the incident's
// own tile, but climbing it only reaches an EQUALLY sealed 24-tile bedroom directly above
// (level 1, bbox 2613-2618,3207-3211) — confirmed offline via cmd/doordiag, a genuine
// dead end, not a real exit. The room's own real exit is a "Door" (id 1530) at 2610,3209,
// but its far side only leads to a SECOND sealed room (2602-2610,3207-3218, 64 tiles) — and
// THAT room's own real exit is a second "Door" (id 1530) at 2606,3219, whose far side is a
// genuine 109,716-tile outdoor component. tryOpenDoor/pickDoorCandidate already searches for
// exactly this shape of fixture, but its own Strategy 2 tie-break (see its own doc comment)
// ranks candidates by raw Chebyshev distance to dest when no candidate offers a directly
// confirmed FindPath route — which every candidate here fails, since the real dest
// (Lumbridge) sits on a different, water-separated landmass FindPath can never cross same-
// level regardless of which door is used. Raw distance then always prefers the FIRST door
// (back toward the account's own starting room, since Lumbridge happens to be numerically
// closer that way) over the SECOND (the real exit), so tryOpenDoor oscillates between the
// two sealed rooms forever and never tries the real chain — exactly the "genuinely sealed
// two-room pocket" shape walkNearCore's own maxConsecutiveUnreachable doc comment already
// described as a known, unsolved gap.
//
// This function does not share tryOpenDoor's per-iteration hot path or its distance-to-dest
// heuristic at all: it is tried only once, from the STRANDED branch, precisely because a
// real multi-room BFS (walking every nearby door's far side, not just picking one by
// distance) costs more than is worth paying on every ordinary WalkNear iteration. Instead of
// ranking by distance to a dest that may not even be on the same landmass, it ranks by
// component SIZE — a room's own raw ExitMask-connected tile count — since every real
// interior room/pocket ground-truthed so far (24 to 244 tiles) is several orders of
// magnitude smaller than any real outdoor area (109,716 tiles): see largeComponentThreshold's
// own doc comment. The first room-chain search finds whose final component clears that
// threshold is treated as "found the real world" and its exact door chain is walked and
// opened in order; a genuinely sealed complex (every reachable room via every door still
// small, out to maxPocketChainHops) returns false exactly like tryEscapeKnownPocket does,
// falling through to attemptSpellRescue unchanged.
func tryEscapeSealedPocket(d *Deps, from tile.Tile) bool {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	startRoom := localComponent(d.Pack, from, pocketChainRoomCap)
	if len(startRoom) >= largeComponentThreshold {
		// Not actually a small sealed pocket at all (e.g. called on an ordinary open-world
		// stranding for some other reason) — nothing for this function to usefully do.
		return false
	}
	type queued struct {
		bbox  [4]int
		chain []doorChainHop
	}
	startBBox := roomBBox(startRoom)
	visited := map[[4]int]bool{startBBox: true}
	queue := []queued{{startBBox, nil}}
	var winner []doorChainHop
search:
	for hop := 0; hop < maxPocketChainHops && len(queue) > 0; hop++ {
		var nextQueue []queued
		for _, q := range queue {
			// Re-derive this room's own tile set fresh each time rather than carrying it
			// alongside queue — only the bbox is kept across iterations (see roomBBox's own
			// doc comment on why bbox alone is enough to recognize a revisit), so this is
			// the one place that needs the full set, cheaply recomputed from any tile
			// already known to be inside it.
			var anyTile tile.Tile
			for t := range startRoom {
				anyTile = t
				break
			}
			if hop > 0 {
				// q is not the start room; recover a representative tile from its own
				// chain's last hop instead.
				anyTile = q.chain[len(q.chain)-1].to
			}
			room := localComponent(d.Pack, anyTile, pocketChainRoomCap)
			for _, m := range findDoorsBoundingRoom(d, live, q.bbox, from.Level) {
				var insideTile, outsideTile tile.Tile
				foundInside, foundOutside := false, false
				for _, off := range doorApproach {
					cand := tile.Tile{X: m.X + off.dx, Z: m.Z + off.dz, Level: from.Level}
					if !d.Pack.Walkable(cand.X, cand.Z, cand.Level) {
						continue
					}
					if room[cand] {
						insideTile, foundInside = cand, true
					} else if !foundOutside {
						outsideTile, foundOutside = cand, true
					}
				}
				if !foundInside || !foundOutside {
					continue
				}
				nextRoom := localComponent(d.Pack, outsideTile, pocketChainRoomCap)
				nextBBox := roomBBox(nextRoom)
				if visited[nextBBox] {
					continue
				}
				visited[nextBBox] = true
				chain := append(append([]doorChainHop{}, q.chain...), doorChainHop{match: m, from: insideTile, to: outsideTile})
				if len(nextRoom) >= largeComponentThreshold {
					winner = chain
					break search
				}
				nextQueue = append(nextQueue, queued{nextBBox, chain})
			}
		}
		queue = nextQueue
	}
	if winner == nil {
		return false
	}
	d.logf("pocket escape: found a %d-door chain to a real %d+-tile area — walking it", len(winner), largeComponentThreshold)
	for _, hop := range winner {
		if !walkAdjacent(d, hop.from, 0, 8*time.Second) {
			d.logf("pocket escape: could not reach %q at %d,%d,%d to open it", hop.match.Name, hop.match.X, hop.match.Z, hop.match.Level)
			return false
		}
		if err := actions.OpLoc(d.Session, hop.match.Op, hop.match.X, hop.match.Z, hop.match.ID); err != nil {
			d.logf("pocket escape: OpLoc on %q: %v", hop.match.Name, err)
			return false
		}
		d.Delay(900 * time.Millisecond)
		// Real, ground-truthed bug (account "acct14", mapsquare 46,50): this used to be a
		// second walkAdjacent(d, hop.to, ...) call here, routing the crossing step through
		// FindPath exactly like the approach walk above. That's wrong for this one specific
		// step — internal/pathfind's collision Pack is a static file baked once from the
		// map's own default (closed) door state and has no live-override hook at all (see
		// tryOpenDoor's own doc comment on this exact limitation, confirmed there the same
		// way: neither Pack nor FindPath takes any form of "live change" parameter). The
		// OpLoc call just above genuinely opens the door server-side, but FindPath keeps
		// reporting the edge it just crossed as unreachable forever, no matter how long this
		// waits — confirmed offline against the real collision pack and loc data for this
		// exact incident (cmd/doordiag: pack.FindPath({2956,3205,0},{3222,3218,0}) fails with
		// reason "unreachable", expanded 19 — the account's own 19-tile sealed room — while a
		// raw hop from 2956,3206 immediately after the door's own tile finds an 85-waypoint
		// route all the way to Lumbridge; the door chain search itself was already finding
		// this exact one-hop chain correctly, so door-search radius and maxPocketChainHops
		// were never the actual gap). tryOpenDoor's own step-through already works around
		// this identical gap with one raw two-waypoint actions.Walk that bypasses FindPath
		// entirely for the single edge just opened — the same MOVE_GAMECLICK a real player's
		// own next click sends — so this hop does the same thing instead of routing through
		// walkAdjacent for this one step.
		here := currentTile(d)
		if here == nil {
			d.logf("pocket escape: lost position after opening %q at %d,%d,%d", hop.match.Name, hop.match.X, hop.match.Z, hop.match.Level)
			return false
		}
		if err := actions.Walk(d.Session, []pathfind.NavPoint{{X: here.X, Z: here.Z, Level: here.Level}, {X: hop.to.X, Z: hop.to.Z, Level: hop.to.Level}}, false); err != nil {
			d.logf("pocket escape: stepping through %q at %d,%d,%d: %v", hop.match.Name, hop.match.X, hop.match.Z, hop.match.Level, err)
			return false
		}
		if !waitFor(d, "the step through the door chain", 5*time.Second, func() bool {
			cur := currentTile(d)
			return cur != nil && cur.X == hop.to.X && cur.Z == hop.to.Z
		}) {
			d.logf("pocket escape: opened %q at %d,%d,%d but the step through to %d,%d,%d did not land", hop.match.Name, hop.match.X, hop.match.Z, hop.match.Level, hop.to.X, hop.to.Z, hop.to.Level)
			return false
		}
	}
	d.Delay(1 * time.Second)
	return true
}

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

// delayWatchingCombat sleeps for dur in short chunks, calling randomevents.EmergencyFlee
// every chunk instead of blocking blind for the whole duration — for the handful of
// genuinely long give-up waits in this file (walkNearCore's/PickpocketStep's/SailLeg's own
// STRANDED backoff, 30s each). Found during a repo-wide audit following tonight's incident
// response: those waits used a bare d.Delay(30 * time.Second) with nothing checking combat
// or drift for the entire half-minute — the exact same "blind wait, sometimes 30+ seconds,
// account can already be dead by the time it ends" shape already fixed today for
// HandleHazard's whirlpool wait (see internal/randomevents/handle.go's
// sleepWatchingCombatOrDrift, the direct model for this). Returns false (matching Delayer's
// own contract) the moment a stop is requested mid-wait.
func delayWatchingCombat(d *Deps, dur time.Duration) bool {
	const chunk = 300 * time.Millisecond
	deadline := time.Now().Add(dur)
	for time.Now().Before(deadline) {
		randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
		remaining := time.Until(deadline)
		wait := chunk
		if remaining < chunk {
			wait = remaining
		}
		if wait <= 0 {
			break
		}
		if !d.Delay(wait) {
			return false
		}
	}
	return true
}

// WalkNear paths to dest and walks there, re-clicking from wherever the player stops,
// stopping once within radius tiles of dest. Returns false on a stop request, an
// unreachable destination (logged with internal/pathfind's own reason — see this package's
// doc comment on the boat-leg limitation), or a timeout. Duplicated per this project's own
// established convention (see mulerunner's walkNear doc comment) rather than shared, so each
// runner package can tune its own timeout/backoff without touching the others.
// RunEnergyThreshold is the run-energy percentage at or above which the shared walk helpers
// below turn running on. The same 20 fisherrunner has always used, and the number the fleet
// users asked for directly (2026-09-21).
const RunEnergyThreshold = 20

// maybeEnableRun turns running on when there is energy to spare.
//
// AUDITED 2026-09-21 AND THIS IS WHY IT IS HERE: of the 22 registered scripts, exactly ONE
// (ProgressiveFisher, via fisherrunner/run.go) ever called actions.SetRun, plus ScarabSwarm's
// own stage 3. Woodcutter, LootChestFisher, Crafter, Thiever, Essence, Fighter, Mule and the
// rest walked everywhere, forever. The fleet owner reported it watching LootChestFisher "walk a
// long way without running", and the cause was that THIS package — the shared walk path for
// mule, crafter, fisher, lootchest, scarab, thiever, vial and woodcut — never enabled it, while
// webwalk.Walker.Run is never assigned by anything either.
//
// Note this is a different situation from the fisher's. Fishers DO run and are ENERGY-bound:
// measured on the live fleet the same day, energy sits at 90%+ in only 2.47% of samples, so the
// threshold is not hoarding anything there. A script that never runs at all saturates at 100 and
// wastes every point it regenerates, which is pure loss rather than a policy question.
//
// One-directional on purpose, exactly as fisherrunner's own copy is: it only ever turns running
// ON. Energy regenerates for free and the server disables running by itself at 0%, so there is
// nothing for this to do at the low end beyond not fighting that. Cheap enough to call on every
// walk: it returns immediately once the toggle is already on.
func maybeEnableRun(d *Deps) {
	if d == nil || d.Session == nil {
		return
	}
	st := d.Session.State()
	energy, ok := st.Energy()
	if !wantRun(st.RunEnabled(), energy, ok) {
		return
	}
	if err := actions.SetRun(d.Session, true); err != nil {
		d.logf("enable run: %v", err)
	}
}

// wantRun is maybeEnableRun's decision, split out pure so it can be tested without a session.
// "energy not observed yet" is deliberately NOT treated as plenty: no UPDATE_RUNENERGY has
// arrived, so the zero value means unknown rather than empty.
func wantRun(runOn bool, energy int, energyKnown bool) bool {
	if runOn {
		return false
	}
	return energyKnown && energy >= RunEnergyThreshold
}

func WalkNear(d *Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	return walkNearCore(d, dest, radius, timeout, true)
}

// WalkAdjacent makes a short approach without starting door recovery or waiting
// for Guardian. Use only inside an action sequence that already holds its lock.
func WalkAdjacent(d *Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	return walkAdjacent(d, dest, radius, timeout)
}

// walkAdjacent is WalkNear with the unreachable-branch recovery attempts (tryChangeLevel,
// tryOpenDoor) switched off — used only by those two recovery helpers themselves for their own
// "walk beside the fixture" step.
//
// Real, live-found bug this exists to close, not a defensive guess: tryOpenDoor's own WalkNear
// call to reach beside a closed door can itself report "unreachable" (the door's own approach
// tile needing something else opened first, or simply outside the room's connected component
// from certain angles — a real structural case, confirmed live, not fully root-caused before
// this fix). Before this split, that inner WalkNear call re-entered WalkNear's own recovery
// branch, which found the SAME closed door again (still the nearest match within radius of the
// original position) and called tryOpenDoor again — genuine unbounded Go call-stack recursion,
// not a logical retry loop, confirmed live by the actual crash: **fatal error: stack overflow**,
// on the very next run after the false-positive verification bug above was fixed, from the
// identical sealed-building incident. WalkNear itself (every other caller in this package) keeps
// both recovery attempts; only the recovery helpers' own inner walk is structurally prevented
// from recursing into them again, so a fixture whose own approach is blocked by something this
// package can't yet clear fails cleanly (logged, delayed, returns false) instead of crashing the
// whole process.
func walkAdjacent(d *Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	return walkNearCore(d, dest, radius, timeout, false)
}

func walkNearCore(d *Deps, dest tile.Tile, radius int, timeout time.Duration, recover bool) bool {
	maybeEnableRun(d)
	deadline := time.Now().Add(timeout)
	// consecutiveUnreachable counts outer-loop iterations in a row where FindPath never
	// once succeeded (res.OK stayed false throughout, no matter what tryOpenDoor/
	// tryChangeLevel/tryClimbOver did in between) — real, live-found gap (2026-09-15, a
	// 200-account soak): a genuinely sealed two-room pocket (a real-content interior area,
	// most likely reached via a random-event fail-teleport, not a live bug in any single
	// door/gate), where EVERY door on EITHER side leads to another fully disconnected
	// small room, has both sides individually "solvable" by tryOpenDoor (there's always a
	// tile outside whichever pocket you're currently in) — so this loop crossed back and
	// forth between the two dead-end rooms forever, hundreds of times, with zero
	// indication anything was wrong: every single recovery attempt "succeeded" (the door
	// really did open, the account really did step through), just never toward dest.
	// SailLeg already has its own SailProgress counter for exactly this shape of problem;
	// this is the same idea at the level every WalkNear/walkAdjacent caller benefits from
	// automatically, since none of this package's other six call sites paced their own
	// give-up logic either. maxConsecutiveUnreachable is deliberately small (not the 45s+
	// SailProgress uses) — a real, connected route either succeeds on FindPath's own
	// first honest look or doesn't exist at all; there is no "try a few more times, it
	// might just be busy" case here the way a shared door has.
	const maxConsecutiveUnreachable = 6
	consecutiveUnreachable := 0
	// lastStallTile tracks where the PREVIOUS stall (if any) happened this call, so the
	// stall-handling block below can tell a brand-new stall from a repeat of the same one
	// — see that block's own doc comment for why this distinction matters.
	var lastStallTile *tile.Tile
	for time.Now().Before(deadline) {
		// Defer to Guardian before ever computing/issuing our own walk this iteration —
		// real, live-found race (2026-09-16, account "acct", still ongoing 40+ minutes
		// into a live soak when found: 21 straight "boat leg ... retrying" cycles, never
		// once progressing past 3207-3215,3225). WaitForGuardian (fisherrunner.go) already
		// gates the START of each SailLeg/PickpocketStep/etc. call, but SailLeg's own
		// internal WalkNear can run for up to 10 minutes (walkNearTimeout) — plenty of
		// room for a NEW random event to spawn and get handled entirely between
		// WaitForGuardian checkpoints. HandleDialogNpc (randomevents/handle.go) walks the
		// account toward the event NPC via the exact same session's actions.Walk this loop
		// itself calls below, and nothing before this fix stopped the two from firing at
		// once: this loop's own EmergencyFlee call further down already defers to Guardian
		// via TryLock, but that only covers the flee-specific danger check — the ordinary
		// FindPath+actions.Walk issuance a few lines below had no Guardian-awareness at
		// all. Confirmed live: mario's own log shows "[guardian] random event: Drunken
		// Dwarf — walking to 3212,3225" seconds before/after "the pier leg did not
		// complete ... retrying" fired from that same tile, repeatedly, while Guardian
		// cycled through Mysterious Old Man / event monster / Drunken Dwarf the entire
		// time — two goroutines both driving movement on the same account, neither aware
		// of the other. randomevents.Busy is the same cooldown-aware check
		// WaitForGuardian itself already uses (see its own doc comment): it returns true
		// for a merely-present-but-not-yet-picked-up event too, not just an in-flight
		// handle, which is exactly the "don't start a competing walk" semantics this loop
		// needs. Deliberately does NOT touch consecutiveUnreachable in either direction —
		// waiting out a random event is neither progress nor a sign the destination is
		// unreachable, so the STRANDED counter simply holds where it was and resumes
		// counting once Guardian is done. Gated on `recover` (only the top-level
		// WalkNear/SailLeg call, never the short internal walkAdjacent recovery helpers
		// tryOpenDoor/tryChangeLevel/tryClimbOver use for their own few-second,
		// radius-0 approach-tile walks) — those already run under a tight, live-tuned
		// timeout budget of their own, and mario's actual incident was a multi-minute
		// top-level call, not one of those short ones; forcing them to also wait out
		// Guardian would just eat their whole budget on nothing for a class of call
		// this fix doesn't need to touch.
		if recover && randomevents.Busy(d.Session) {
			if !d.Delay(300 * time.Millisecond) {
				return false
			}
			continue
		}
		here := currentTile(d)
		if here == nil {
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			continue
		}
		if tile.Chebyshev(*here, dest) <= radius {
			return true
		}
		res := findPathAvoidingDanger(d, pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level})
		if res.OK && len(res.Waypoints) > 0 {
			last := res.Waypoints[len(res.Waypoints)-1]
			d.logf("[diag] FindPath %d,%d,%d -> %d,%d,%d: %d waypoints, last %d,%d,%d", here.X, here.Z, here.Level, dest.X, dest.Z, dest.Level, len(res.Waypoints), last.X, last.Z, last.Level)
		}
		// consecutiveUnreachable is deliberately NOT reset just because res.OK is true —
		// see the stall-handling block below's own doc comment for why: a route can be
		// perfectly valid (FindPath says OK every single time) while the actual walk
		// keeps stalling mid-route for some unrelated reason, in which case res.OK being
		// true carries no information about whether real progress is happening at all.
		// It's reset instead at the one place that actually represents progress: after a
		// walk segment completes without ever stalling.
		if !res.OK {
			// A stopping bot (a stop request, a dropped connection) answers every Delay false, so none of the
			// recovery below can walk anywhere: it would only flood the whole connected component again for each
			// door, each approach tile and each of six STRANDED passes. Live 14 Oct 2026: one account's connection
			// dropped at 04:17:13 and this loop went on until 04:18:36, 83 seconds and 89 "no path" lines,
			// hunting "Door" 2815,3449 (12 tiles away, not on any route) and walking the Catherby bank booths'
			// far side; the fleet stop at 04:45:07 did the same to two more accounts.
			if !d.Delay(0) {
				d.logf("walk toward %d,%d,%d from %d,%d,%d: no route and the bot is stopping — not hunting for a door", dest.X, dest.Z, dest.Level, here.X, here.Z, here.Level)
				return false
			}
			if recover && tryRangeDoor(d, *here, dest) {
				consecutiveUnreachable = 0
				continue
			}
			// Refused by the danger zones alone (zoneBlocked, lowcombatzones.go): the pack has a
			// route, and it runs where this account must not walk. No door, stile or stair below
			// changes that, and six tries of them would call the account STRANDED, so hand the walk
			// back to the caller instead (a woodcutter then excludes the tree and picks another).
			if zoneBlocked(liveNavigationPack(d), pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level}, res) {
				d.logf("no path from %d,%d,%d to %d,%d,%d that stays out of the danger zones: the only route runs through one, so not walking it", here.X, here.Z, here.Level, dest.X, dest.Z, dest.Level)
				d.Delay(2 * time.Second)
				return false
			}
			d.logf("no path from %d,%d,%d to %d,%d,%d: %s", here.X, here.Z, here.Level, dest.X, dest.Z, dest.Level, res.Reason)
			if res.Reason != "start not walkable" {
				consecutiveUnreachable++
				if consecutiveUnreachable >= maxConsecutiveUnreachable {
					// Lets a strand site that scripts also use on purpose (StrandSite.NoWayHomeOnly)
					// hold this account: 3 Oct 12:55, 8 ScarabSwarm accounts at 3149,9899 looped
					// SailWest here, 16% of the fleet's CPU, without ever running the drift check.
					randomevents.NoteNoWayHome(d.Session)
					// Distinct, named condition instead of a generic "no path" line whenever the
					// current mapsquare matches a real, ground-truthed cause — see
					// randomevents.DescribeStrandedLocation's own doc comment (built from this
					// exact incident: account "acct24", frozen at 3212,9615,0 logging this same
					// generic STRANDED line forever, mapsquare 50,150 = the "lubmy basement"
					// Genie/Mysterious Old Man fail-teleport destination — content-confirmed, not
					// an instance/offset guess). An operator or dashboard scanning logs for
					// "STRANDED" should be able to tell this class of problem apart from an
					// ordinary sealed-building/door bug at a glance.
					if reason := randomevents.DescribeStrandedLocation(here.X, here.Z, here.Level); reason != "" {
						d.logf("STRANDED IN KNOWN RANDOM-EVENT FAIL-TELEPORT POCKET: %s (mapsquare %d,%d) — no path to %d,%d,%d has succeeded in %d attempts in a row; this is a real, named displacement (most likely an ignored Genie/Mysterious Old Man event), not an ordinary pathing bug", reason, here.X>>6, here.Z>>6, dest.X, dest.Z, dest.Level, consecutiveUnreachable)
					} else {
						d.logf("STRANDED: no path to %d,%d,%d has succeeded in %d attempts in a row from around %d,%d,%d — likely a sealed pocket with no walkable exit (e.g. a random-event fail-teleport destination); this account needs an operator to look at it", dest.X, dest.Z, dest.Level, consecutiveUnreachable, here.X, here.Z, here.Level)
					}
					// Try a real, unprivileged walk-based escape FIRST, before either rescue path
					// below and regardless of LocalRescueTeleport: attemptSpellRescue needs Magic
					// level 31 plus air/earth/law runes, which this fleet's real accounts (Magic
					// level 1, no runes) can never have, so that branch was guaranteed to fail
					// every single time for exactly the incidents ("acct24"/"acct63") that
					// motivated it. A walk-based escape needs neither.
					//
					// The door chain goes first, and that ORDER is the fix for the Ardougne
					// Monastery oscillation — see escapedToOpenGround's own doc comment for the
					// ground truth. tryEscapeSealedPocket verifies that the component it is
					// heading into is genuinely large BEFORE committing to walk anything, while
					// tryEscapeKnownPocket cannot know where a staircase leads until it has
					// already climbed it. In that building the stair search's only candidate
					// climbs into a sealed room and leaves the account one level above the door
					// chain that is the real exit, so trying the verified escape first is what
					// keeps the account on the level its own exit is on. This costs nothing where
					// it does not apply: the one other ground-truthed pocket ("lubmy basement",
					// mapsquare 50,150) has no door or gate within 20 tiles at all (re-confirmed
					// with cmd/doordiag this session), so the chain search returns false off its
					// own room-BFS and the stair climb runs exactly as it always did.
					//
					// Re-reads the account's CURRENT tile rather than reusing the outer loop's
					// own `*here` snapshot — see leftPocket's own doc comment for the live
					// incident (account "acct", Ardougne Monastery ground floor) behind this: a
					// rescue can genuinely move the account while still returning false, and
					// handing the next one a stale position makes its own room/door search
					// compute an entirely wrong level or tile. Falls back to `*here` only if the
					// position cannot be read this instant, matching tryOpenDoor's own
					// postApproachTile discipline.
					// The webwalk route goes first when this account has a Walker: its graph opens
					// doors and gates anywhere along the way, where internal/pathfind plans none and
					// the door searches below look 15 and 48 tiles out. Live 6 Oct 2026: craft
					// (ProgressiveCrafter) and Acct194 (ProgressiveWCFletch) logged STRANDED at the
					// Rogues' Castle for hours, fenced in by the Gate (loc 1596) at 3225,3904, 53 to 63
					// tiles off; cmd/webwalkplan walks both out through it. Only a top-level walk
					// (recover) hands over: the short approach walks inside a recovery must not start
					// a cross-map one.
					if recover {
						if arrived, moved := strandedWalkerEscape(d, *here, dest, radius, time.Until(deadline)); arrived {
							return true
						} else if moved {
							d.Delay(1 * time.Second)
							return false
						}
					}
					sealedFrom := *here
					if cur := currentTile(d); cur != nil {
						sealedFrom = *cur
					}
					if tryEscapeSealedPocket(d, sealedFrom) {
						d.logf("rescue: escaped the pocket via a real door chain — resuming from the new position")
						d.Delay(1 * time.Second)
						return false
					}
					// A door chain is not the only real shape a sealed interior takes: a single
					// staircase or ladder out of a z+6400-offset basement pocket is the other, and
					// tryChangeLevel's own `from.Level != dest.Level` gate can never fire for one
					// of those, since a pocket shares its surface counterpart's own level NUMBER.
					// Same fresh-position discipline as above, since the chain search can walk
					// part of a chain before failing on a later hop.
					knownFrom := sealedFrom
					if cur := currentTile(d); cur != nil {
						knownFrom = *cur
					}
					if tryEscapeKnownPocket(d, knownFrom) {
						d.logf("rescue: escaped the pocket via a real staircase/ladder — resuming from the new position")
						d.Delay(1 * time.Second)
						return false
					}
					if !LocalRescueTeleport {
						// No staff-level ::tele cheat exists on production (see
						// LocalRescueTeleport's own doc comment) — attemptSpellRescue is the one
						// real, unprivileged in-game action that can still escape a genuinely
						// sealed pocket (a teleport spell bypasses collision/pathing entirely).
						// Worth trying regardless of whether DescribeStrandedLocation recognized
						// this specific spot: an unrecognized-but-genuinely-sealed pocket benefits
						// identically, and a normal reachable destination just never hits this
						// branch (consecutiveUnreachable wouldn't have climbed this high). An
						// account that cannot cast it is told so in one line and falls through
						// like a failed cast, with nothing sent (spellrescue.go).
						landed, cast := attemptSpellRescue(d, *here)
						if landed {
							d.logf("rescue: Lumbridge Teleport landed — resuming from the new position")
							d.Delay(1 * time.Second)
							return false
						}
						if cast {
							d.logf("rescue: Lumbridge Teleport did not move the account (likely missing runes or Magic level) — this account needs an operator to look at it")
						}
					}
					if LocalRescueTeleport {
						// Real, live-found bug in the first version of this rescue (2026-09-15,
						// a 200-account soak): this used to reset the counter and `continue`,
						// staying inside THIS SAME walkNearCore call with the SAME (now stale)
						// dest/walkTarget the caller computed before the account ever got
						// stranded here. For a caller like SailLeg, whose own walkTarget is
						// only recomputed once per call from freshstart.BoatLeg(here, dest) —
						// itself only valid for whatever leg `here` was on when SailLeg started
						// — rescuing back to Lumbridge and then re-trying the SAME stale target
						// (e.g. the raw final Catherby tile, unreachable by plain walk from the
						// mainland without re-boarding both ships) just re-hits STRANDED every
						// ~6 attempts forever: same account, same rescue, same failure, on
						// repeat, confirmed live (freshcyc "lc2b_2": 3 STRANDED+rescue cycles in
						// under 2 minutes, never once falling back through to a fresh
						// TravelLeg/BoatLeg recomputation). Returning false instead hands
						// control back to the caller exactly the same way the no-rescue path
						// already does, so the NEXT call re-derives leg/target from the
						// account's real (now rescued) position instead of blindly retrying a
						// target that assumed it was still somewhere else entirely.
						if rescueTeleport(d, *here) {
							d.Delay(1 * time.Second)
							return false
						}
						d.logf("rescue ::tele failed or didn't land — falling back to reporting stranded and giving up this attempt")
					}
					delayWatchingCombat(d, 30*time.Second)
					return false
				}
			}
			// "start not walkable" specifically (not "unreachable") means OUR OWN tracked
			// tile fails Walkable() — a live account is never actually standing on a
			// genuinely non-walkable tile, so this is the strongest available signal of the
			// still-open position-tracking desync documented in COLLAB.md (2026-09-15,
			// cmd/gatetest, ::getcoord-confirmed: the server's own authoritative position can
			// differ from ours by roughly one zone-rebuild's worth of tiles after a long,
			// multi-zone walk). No production-safe way to force a resync exists (::getcoord
			// is local-engine-only), so this can't self-heal on the spot — but backing off
			// longer than the ordinary unreachable case gives real gameplay ticks a chance to
			// deliver a fresh, corrective PLAYER_INFO update naturally, and logging it
			// distinctly means an operator sees "probable position desync" instead of a
			// generic unreachable spam.
			if res.Reason == "start not walkable" {
				d.logf("probable position-tracking desync at %d,%d,%d — backing off", here.X, here.Z, here.Level)
				d.Delay(5 * time.Second)
				continue
			}
			if recover {
				// internal/pathfind is same-level only (no doors/transports/stairs — the
				// documented v1 scope), so a level mismatch always fails here even when the
				// destination is otherwise perfectly reachable once on the right level. Found
				// live twice tonight stranding real production accounts indoors/upstairs after
				// a random-event teleport, in both cases requiring a manual walk to recover —
				// try a real staircase/ladder near the CURRENT position first, once, before
				// giving up: this is the one place low enough in the call stack that every
				// caller (PickpocketStep, SailLeg, and anything else that calls WalkNear)
				// benefits automatically, without each higher-level runner needing its own copy.
				if here.Level != dest.Level && tryChangeLevel(d, *here, dest) {
					continue
				}
				// A same-level "unreachable" is often not actually a dead end — it is the same
				// v1 "no doors/transports" gap as the level-mismatch case just above, just
				// without a level change involved: the account is walled into a room/building
				// by a real closed door the collision pack bakes as solid (this exact case,
				// live: casket teleported by a random event into a small sealed building
				// northeast of Yanille — expanded=30, the entire building, with zero exits — a
				// closed "Door" loc, Op "Open", sat right on its wall). internal/pathfind has no
				// notion of a door as a passable-with-an-action edge, so it reports this
				// identically to a genuinely unreachable destination; try opening a real nearby
				// door once before believing that, the same one-shot-then-let-the-caller-
				// re-evaluate shape as tryChangeLevel. tryOpenDoor's own inner walk uses
				// walkAdjacent (recover=false), not WalkNear, so this branch can never recurse
				// into itself — see walkAdjacent's own doc comment for the real crash that
				// discipline closes.
				// tryOpenDoor's return value used to gate whether this branch continues at
				// all — but that stopped being safe the moment tryOpenDoor started honestly
				// reporting a step-through that never landed (see its own doc comment on
				// why that changed): a wrong door/alias now correctly returns false on
				// (almost) every call, which used to mean bailing out of THIS WHOLE
				// walkNearCore call after a single attempt, via the "return false" below —
				// ending the call before consecutiveUnreachable (already incremented once,
				// above) ever had a chance to climb toward maxConsecutiveUnreachable, since
				// that counter is scoped to one call and the caller just invokes a fresh one
				// next time. Found live 2026-09-15 (lc2b_196, right after this same
				// session's earlier tryOpenDoor fix landed): 0 STRANDED hits, still, despite
				// the fix, for exactly this reason. Attempt whichever recovery applies and
				// ALWAYS continue regardless of whether it reports success — the counter
				// check at the top of the next iteration (not this return value) is what
				// decides when enough attempts have failed, so this loop needs to actually
				// keep looping for that check to ever run again.
				tryOpenDoor(d, *here, dest)
				// A Stile/Crumbling wall isn't a door and doesn't toggle state — see
				// tryClimbOver's own doc comment — so it needs its own one-shot attempt
				// here, same shape as the two recovery attempts just above. Also always
				// attempted (not gated behind tryOpenDoor's own result) since a stalled
				// account can be blocked by either kind of obstacle independently of
				// whether a door was even found nearby at all.
				tryClimbOver(d, *here, dest)
				// Same busy-loop pacing this branch has always needed (see the recover=false
				// early-return below for the original version of this reasoning) — now paid
				// here instead of only on the old early-return path, since this branch no
				// longer bails after one attempt.
				d.Delay(1 * time.Second)
				continue
			}
			// A genuinely unreachable destination (no baked route at all, and no door or
			// stair nearby that changed anything) determines this instantly, with no I/O of
			// its own. None of this function's six call sites pace their own retry after a
			// false return (most just call WalkNear once and return, letting their own
			// caller's loop call them again), so without this delay a real "no path" result
			// turns into the exact tight busy-loop found live tonight (see
			// travelViaFreshStart's own COLLAB.md findings entry) — 20,000+ identical log
			// lines in seconds once BuySkirtStep hit an unreachable Thessalia's stand.
			// Matches barbarianrunner.travelResilient's own identical fix for the identical
			// class of bug. Only reached now when recover is false (walkAdjacent's own
			// internal search, which must never recurse into WalkNear's own recovery — see
			// tryOpenDoor's doc comment) since the recover=true path above always continues.
			d.Delay(2 * time.Second)
			return false
		}
		if len(res.Waypoints) < 2 {
			d.logf("route ends at current tile %d,%d,%d without reaching requested radius %d of %d,%d,%d", here.X, here.Z, here.Level, radius, dest.X, dest.Z, dest.Level)
			d.Delay(time.Second)
			return false
		}
		if err := actions.Walk(d.Session, res.Waypoints, false); err != nil {
			d.logf("walk: %v", err)
			d.Delay(2 * time.Second)
			return false
		}
		lastX, lastZ, still := here.X, here.Z, time.Now()
		stalledAt := (*tile.Tile)(nil)
		for time.Now().Before(deadline) {
			// EmergencyFlee here, not just in the outer scripts' own WaitForGuardian —
			// found during a repo-wide audit following tonight's incident response: this
			// loop is walkNearCore's own per-segment progress poll, and its deadline can
			// be as long as SailLeg's own 10-minute boat-leg budget with nothing checking
			// combat or drift in between, since WaitForGuardian only runs before/after a
			// whole WalkNear/SailLeg call, never during one. Same fix already applied to
			// every sibling runner's own WalkNear (fisherrunner/barbarianrunner/
			// crafterrunner) — safe every 200ms tick, since EmergencyFlee's own TryLock
			// makes it a no-op whenever Guardian's kick() already owns the action lock.
			randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
			// Break off from a permanently-spawned hostile before it engages, rather than
			// walking on through it — see hostileevade.go's own doc comment for the giant
			// bats and White wolves this exists for, and for why a route-avoidance danger
			// zone could not solve it. This is the one place worth checking from: it is the
			// only poll that runs continuously WHILE the account is travelling, which is
			// when it walks into them. A true return means the account has moved, so break
			// out to the outer loop and re-plan the route from where it now stands; that
			// counts as progress, not a stall, which is exactly what the stalledAt == nil
			// branch below already does.
			if EvadeStaticHostiles(d) {
				break
			}
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			cur := currentTile(d)
			if cur == nil {
				continue
			}
			if tile.Chebyshev(*cur, dest) <= radius {
				return true
			}
			if cur.X != lastX || cur.Z != lastZ {
				lastX, lastZ, still = cur.X, cur.Z, time.Now()
			} else if time.Since(still) > 2*time.Second {
				stalledAt = cur
				break
			}
		}
		if stalledAt == nil {
			// The walk kept moving the whole time (or the outer deadline simply ran out
			// while still doing so) — genuine progress, whether or not this particular
			// segment finished. This is the one place that actually resets
			// consecutiveUnreachable now that res.OK alone no longer does (see that
			// field's own doc comment above).
			consecutiveUnreachable = 0
		}
		// A stall mid-walk (no movement for 2s, still short of radius) is functionally the
		// same "closed door pathfind doesn't model" gap as the res.OK==false branch above,
		// just discovered live instead of by the offline pack — a route the static bake
		// thinks is clear can still be blocked by a door that's actually shut right now.
		// Before this fix, hitting `break` here fell straight back to the top of the outer
		// loop with zero logging: FindPath sees the same "OK" route, re-walks it, stalls
		// again, forever — the exact silent infinite loop confirmed live 2026-09-15
		// (BuySkirtStep looping "buying a pink skirt in Varrock" every ~20s for 9+ minutes
		// with no other log line at all, draining coins on every re-funding cycle in
		// between). Try the same tryOpenDoor recovery this function already trusts for the
		// unreachable case, from wherever the account actually stalled, and log either way
		// so a real dead end is at least visible instead of silent.
		if stalledAt != nil {
			// Give a stall ONE plain retry (the natural next outer-loop iteration, reached
			// by falling through below) before ever hunting for a door — tryOpenDoor/
			// tryClimbOver search within a fixed radius of the stall tile regardless of
			// whether anything they find is actually on the real route, and that search's
			// own walk can physically relocate the account far away chasing an irrelevant
			// fixture. Real, live-found cost (2026-09-15, a 200-account soak, door "Large
			// door" 3063,3380): FindPath's own route from the stall point to dest was a
			// clean 8-waypoint path through a 216,661-tile component that never went
			// anywhere near this door (confirmed via cmd/doordiag) — the stall itself was
			// something else entirely (most likely transient multi-account tile
			// congestion, which usually clears in well under a second), yet door-hunting
			// still walked the account ~40 tiles away to the door's approach tile every
			// single cycle, back and forth, burning minutes per account with zero chance of
			// ever helping. Only escalate to door-hunting once the SAME exact tile stalls
			// twice in a row — a stall that clears with a plain retry never pays this cost
			// at all now; one that doesn't still gets the full recovery ladder, just one
			// iteration later.
			sameTileAsLastStall := lastStallTile != nil && *lastStallTile == *stalledAt
			lastStallTile = stalledAt
			if recover && sameTileAsLastStall && tryOpenDoor(d, *stalledAt, dest) {
				consecutiveUnreachable = 0
				continue
			}
			if recover && sameTileAsLastStall && tryClimbOver(d, *stalledAt, dest) {
				consecutiveUnreachable = 0
				continue
			}
			if sameTileAsLastStall {
				d.logf("walk stalled at %d,%d,%d heading to %d,%d,%d — no door found to clear it", stalledAt.X, stalledAt.Z, stalledAt.Level, dest.X, dest.Z, dest.Level)
			} else {
				d.logf("walk stalled at %d,%d,%d heading to %d,%d,%d — first time at this tile, retrying before hunting for a door", stalledAt.X, stalledAt.Z, stalledAt.Level, dest.X, dest.Z, dest.Level)
			}
			// A stall with neither recovery helping is just as much "not making progress
			// toward dest" as a repeated FindPath failure — sharing the same counter
			// closes a real gap found live 2026-09-15 (a 200-account soak): FindPath
			// itself reported a perfectly clean, direct route the whole time (confirmed
			// offline via cmd/doordiag: an 8-waypoint path through a 216,661-tile
			// component, nowhere near any door), so consecutiveUnreachable never moved —
			// the walk just stalled mid-route for some unrelated reason (multiplayer
			// congestion, an NPC briefly blocking a tile, a transient collision), the
			// stall-recovery above went hunting for the nearest door anyway (11 tiles off
			// the real route, not actually on it), failed to help since it was never the
			// real obstacle, and this loop repeated with zero escalation forever — two
			// separate accounts independently stalled at the exact same tile pair,
			// confirming it's a real, reproducible dead end for this route rather than a
			// one-off fluke. Folding stalls into the same counter/threshold means this
			// gets caught and rescued exactly like every other "not making progress" case
			// instead of being invisible to the one detector meant to catch it.
			consecutiveUnreachable++
			if consecutiveUnreachable >= maxConsecutiveUnreachable {
				d.logf("STRANDED: walk toward %d,%d,%d has stalled at the same tile %d times in a row with no door clearing it — this account needs an operator to look at it", dest.X, dest.Z, dest.Level, consecutiveUnreachable)
				if LocalRescueTeleport {
					if rescueTeleport(d, *stalledAt) {
						d.Delay(1 * time.Second)
						return false
					}
					d.logf("rescue ::tele failed or didn't land — falling back to reporting stranded and giving up this attempt")
				}
				delayWatchingCombat(d, 30*time.Second)
				return false
			}
			d.Delay(1 * time.Second)
		}
	}
	return false
}

// stairNames/stairOps are tried together (every name against every op) since a real
// staircase/ladder's exact display name and option wording both vary — "Staircase",
// "Ladder", "Stairs", "Steps" are the common ones across this era's content, and the
// climb option itself is sometimes "Climb-up"/"Climb-down", sometimes just "Climb", and
// occasionally "Walk-up"/"Walk-down"/"Use". No direction preference: from an unknown
// stranding, any real level change is progress — WalkNear's own outer retry loop tries
// again with a fresh search if this particular one didn't happen to help.
// "Ship's ladder" was tried here live (2026-09-15) and reverted the same session: two
// DIFFERENT loc ids share that exact display name back to back on real ships — id 273
// ("Climb-down") and id 245 ("Climb-up") sit right next to each other at, e.g., 2963,3143
// near Karamja — so a generic name+op search matches whichever one is nearest with no
// sense of which direction actually helps. Confirmed live: multiple accounts climbed one,
// found the other adjacent on the very next WalkNear retry, climbed back, and oscillated
// between the two with NO delay between iterations — a real busy loop, hundreds of
// "changing level via" log lines per second across several accounts before it was caught
// and killed. Left OUT of this generic list on purpose; see SailLeg's own narrow,
// direction-locked handling of the one specific "Ship's ladder" this project actually
// needs (the Ardougne dock arrival), which avoids the trap by hardcoding a single known id
// and op instead of searching by name.
var stairNames = []string{"Staircase", "Ladder", "Stairs", "Steps"}

// gangplankNames/gangplankOps are tried by tryChangeLevel only as a fallback, after the
// stair search above finds nothing — a real, live-found gap (2026-09-16, this codebase's
// first real production run): an account fail-teleported onto a docked ship's deck (a
// distinct level, e.g. Port Sarim's ship at level 1) has no staircase/ladder anywhere near
// it, only a Gangplank — confirmed live via cmd/findloc against production's own baked
// locs: id 2084 "Gangplank" op "Cross" at 3031,3217,1 (the deck) and a SEPARATE id 2083 at
// 3030,3217,0 (the dock), one tile apart in X, exactly the same "adjacent same-name,
// different id" shape that got "Ship's ladder" reverted from the generic stairNames list
// (see that var's own doc comment) — EXCEPT a gangplank's single "Cross" op is not
// direction-specific the way "Climb-up"/"Climb-down" are, and each level only ever has its
// own one gangplank instance, so scoping the query to from.Level (already done below)
// picks the correct instance for the account's actual current level with no ambiguity: the
// deck's own "Cross" always leads down to the dock, the dock's own "Cross" always leads up
// to the deck. Kept as a fallback rather than merged into stairNames/stairOps so a real
// staircase/ladder is still preferred whenever one exists — a gangplank is a boat-deck-only
// fixture, never a substitute for an ordinary building's stairs.
var gangplankNames = []string{"Gangplank"}
var gangplankOps = []string{"Cross"}

// stairOpsDown/stairOpsUp order the SAME op set by which direction they most likely mean
// first, falling back to the other direction's ops (and the direction-ambiguous
// "Climb"/"Use") only if nothing matching the preferred direction is found nearby. Real
// live bug found 2026-09-15 (see COLLAB.md): the old single stairOps list always tried
// "Climb-up" before "Climb-down" regardless of which way the account actually needed to
// go, so an account needing to descend toward dest could climb UP instead, simply because
// an up-going fixture happened to be nearest and "Climb-up" was checked first — then, one
// WalkNear iteration later, find itself needing to come back down, matching whatever
// down-going fixture was nearest FROM THERE (not necessarily the same one), and oscillate
// between two ordinary staircases indefinitely. Confirmed live: swarmlab6 alternated
// between levels 2 and 3 via a real "Staircase" for over a minute while its actual
// destination was on level 0 — not a packet-flood busy loop (tryChangeLevel's own 1.5s
// floor delay below already closed that), but never real progress either, since nothing
// about the search ever preferred the direction that would actually help.
var stairOpsDown = []string{"Climb-down", "Walk-down", "Climb", "Use", "Climb-up", "Walk-up"}
var stairOpsUp = []string{"Climb-up", "Walk-up", "Climb", "Use", "Climb-down", "Walk-down"}

// pickDirectionalMatch searches names x ops for the fixture tryChangeLevel should use,
// treating ops's ORDER as priority tiers rather than just another axis to flatten into a
// single "globally nearest" search. Real, live-found bug this closes (2026-09-16, accounts
// "oi" and "acct17", independently, both stuck at 2699,3406,2 heading to 2809,3441,0 —
// i.e. needing to go DOWN): that tile sits one step from a "Ladder" with op "Climb-up" at
// 2699,3405,2 and about six tiles from a SEPARATE "Ladder" id with op "Climb-down" at
// 2704,3403,2 — the exact "two adjacent same-name, different-id, direction-specific
// fixtures" shape stairOpsDown/stairOpsUp's own doc comment above already documents as a
// normal map layout. The naive flattened search (every name x every op, keep whichever
// single match is physically nearest, added 2025-09-15 to fix a DIFFERENT bug — see
// tryChangeLevel's old inline comment, kept in git history) does not know that "Climb-up"
// only appears in stairOpsDown as a last-resort fallback: it just sees one match at distance
// 1 (Climb-up) and one at distance 6 (Climb-down) and picks the nearer one, so an account
// needing to go down picked the up-going ladder purely because it happened to be closer —
// confirmed live via the account's own logged activity, "changing level via 'Ladder' at
// 2699,3405,3", moving it AWAY from level 0. Fix: iterate ops in priority order as the OUTER
// loop, and only within one op do the "nearest across every name" search that closed the
// original far-Staircase-over-near-Ladder bug; stop at the first op tier that matches
// anything at all, rather than letting a later, lower-priority op's closer match steal the
// pick from an earlier, higher-priority op's farther one. This keeps both fixes: name order
// still never matters (all names are still checked together within a tier), and op priority
// (i.e. direction) now actually dominates raw distance the way stairOpsDown/stairOpsUp's own
// doc comment always intended.
func pickDirectionalMatch(locs *locindex.Index, types locindex.Types, live []locindex.Change, from tile.Tile, names []string, opsByPriority []string, radius int) (*locindex.Match, string) {
	// The Draynor Manor basement's ladder up is behind lever doors that no plain walk opens (leverOwned): this search would name it,
	// walk four approach tiles for it and report "no reachable approach tile from here" every cycle, 172 times on 19 Oct 2026.
	if questplan.InErnestBasement(from) {
		return nil, ""
	}
	for _, op := range opsByPriority {
		var found *locindex.Match
		var foundName string
		bestDist := -1
		for _, name := range names {
			if m, ok := locs.Nearest(types, locindex.Query{X: from.X, Z: from.Z, Level: from.Level, Radius: radius, Name: name, Op: op}, live); ok {
				dist := tile.Chebyshev(from, tile.Tile{X: m.X, Z: m.Z, Level: m.Level})
				if found == nil || dist < bestDist {
					mCopy := m
					found, foundName, bestDist = &mCopy, name, dist
				}
			}
		}
		if found != nil {
			return found, foundName
		}
	}
	return nil, ""
}

// tryChangeLevel looks for a real staircase/ladder within a generous radius of from and, if
// found, walks to it and uses it — a one-shot attempt, not a loop: the caller (WalkNear)
// re-evaluates from scratch on its own next iteration once this returns, since a level change
// invalidates any distance/path reasoning computed against the old level. dest is used only to
// pick which direction's ops to prefer (see stairOpsDown/stairOpsUp) — this does not path-check
// against dest the way tryOpenDoor does against its own candidates, since a multi-level building
// can legitimately need several successive level changes before dest is actually reachable.
// Returns true only once the account's own tracked level has actually changed, confirmed live
// rather than assumed from a successful click (matches this project's own "verify the game
// state changed" discipline elsewhere). LOCAL ENGINE AND PRODUCTION safe: no cheats, just the
// same OpLoc a real player's own staircase click sends.
func tryChangeLevel(d *Deps, from tile.Tile, dest tile.Tile) bool {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	ops := stairOpsDown
	if dest.Level > from.Level {
		ops = stairOpsUp
	}
	// A fixture the account can reach first: a stair, then a gangplank. See reachablefixture.go
	// for the ship deck whose only exit is a gangplank while an unreachable ladder sat 17 tiles
	// away in the same mapsquare. Only when nothing within reach qualifies does the old
	// nearest-by-distance choice run, unchanged, in case the pack is wrong about a room.
	pack := liveNavigationPack(d)
	found, foundName := pickReachableMatch(pack, d.Locs, d.Config, live, from, stairNames, ops, 25)
	if found == nil && dest.Level < from.Level {
		// A gangplank only for going DOWN (deck to dock): going up from a dock, the reachable
		// plank is the one that boards the ship, and a stair judged unreachable (a door the pack
		// thinks shut) must not lose to it (review of claude/handoff-deck).
		found, foundName = pickReachableMatch(pack, d.Locs, d.Config, live, from, gangplankNames, gangplankOps, 25)
	}
	if found == nil {
		found, foundName = pickDirectionalMatch(d.Locs, d.Config, live, from, stairNames, ops, 25)
	}
	if found == nil {
		// No staircase/ladder nearby — try a gangplank (see gangplankNames' own doc
		// comment for why this is a safe fallback rather than folded into the search above).
		found, foundName = pickDirectionalMatch(d.Locs, d.Config, live, from, gangplankNames, gangplankOps, 25)
	}
	if found == nil {
		return false
	}
	d.logf("changing level via %q at %d,%d,%d", foundName, found.X, found.Z, found.Level)
	// Try each of the 4 orthogonal neighbours individually (radius 0, exact tile) rather
	// than one generic walkAdjacent(radius=1) — the same fix already applied to
	// tryOpenDoor's own doorApproach loop, for the identical reason: a directional
	// fixture (confirmed live this session for ArdougneLadder/PortSarimLadder/
	// KaramjaGangplank, each hardcoded individually because this generic search didn't
	// exist yet) only accepts an interaction from ONE specific side, and a radius-1 walk
	// can land on any of up to 8 neighbours with no preference — landing on a rejected
	// side produces exactly the "OpLoc sent, waitFor times out" symptom found live
	// 2026-09-15 (a 200-account soak: one account retried the identical "Ladder" every
	// ~7 seconds for 3+ minutes straight, always timing out, since nothing before this
	// fix ever tried a different side). This generalizes that fix to any such fixture
	// instead of requiring another one-off hardcoded id/tile pair every time a new one is
	// found live. The 1.5s delay after a confirmed change is deliberate, not an
	// afterthought: two ordinary "Ladder"/"Staircase" fixtures a single tile apart, one
	// climbing each direction, is a completely normal map layout (found live at
	// 2699,3405, levels 2/3) — WalkNear's own `continue` right after tryChangeLevel
	// returns has no delay of its own, so a genuine real level change straight back
	// toward a fixture going the other way produced a zero-delay oscillation live once
	// already (30 concurrent accounts, one sent 90,000+ packets bouncing between levels
	// before it was noticed). A real level change is genuine progress, so this can't
	// just refuse to act — but it can make sure a second one can't follow the first
	// with zero delay, giving the account's own path back toward dest a chance to be
	// re-evaluated as "closer" before trying yet another fixture.
	width, length := 1, 1
	if t, ok := d.Config.Loc(found.ID); ok {
		width, length = t.Width, t.Length
	}
	levelChanged := func(what string, timeout time.Duration) bool {
		if err := actions.OpLoc(d.Session, found.Op, found.X, found.Z, found.ID); err != nil {
			d.logf("OpLoc on %q: %v", foundName, err)
			return false
		}
		if waitFor(d, what, timeout, func() bool {
			cur := currentTile(d)
			return cur != nil && cur.Level != from.Level
		}) {
			d.Delay(1500 * time.Millisecond)
			return true
		}
		return false
	}
	here := from
	if cur := currentTile(d); cur != nil {
		here = *cur
	}
	for _, approach := range footprintApproaches(found.X, found.Z, found.Level, width, length, found.Angle, here) {
		// Why: FindPath answers OK with a path to the nearest reachable tile, and walkNearCore
		// takes a one-waypoint answer as arrival, so an unreachable approach tile used to send
		// the OpLoc from wherever the account already stood.
		if d.Pack != nil {
			cur := currentTile(d)
			if cur == nil {
				continue
			}
			res := d.Pack.FindPath(pathfind.NavPoint{X: cur.X, Z: cur.Z, Level: cur.Level}, pathfind.NavPoint{X: approach.X, Z: approach.Z, Level: approach.Level})
			if !res.OK || len(res.Waypoints) == 0 {
				continue
			}
			if last := res.Waypoints[len(res.Waypoints)-1]; last.X != approach.X || last.Z != approach.Z || last.Level != approach.Level {
				continue
			}
		}
		if !walkAdjacent(d, approach, 0, 8*time.Second) {
			continue
		}
		if cur := currentTile(d); cur == nil || *cur != approach {
			continue
		}
		if levelChanged("the level to change", 5*time.Second) {
			return true
		}
	}
	// The engine walks the player to an OPLOC target itself when it is not already in reach,
	// which reaches a forced-approach side none of the tiles above could.
	d.logf("no approach tile to %q worked — sending the option from %d,%d,%d and letting the server approach", foundName, here.X, here.Z, here.Level)
	return levelChanged("the level to change after a server-side approach", 10*time.Second)
}

// climbOverNames/climbOverOps are agility-shortcut-style obstacles — a real, distinct
// fixture class from doorNames/stairNames, found live 2026-09-15 stranding fresh
// accounts near Rimmington (3044,3306, a real "Stile" one tile away, op "Climb-over"):
// content/scripts/skill_agility/configs/shortcuts.loc's own [fullstyle] entry (op1=
// "Climb-over") and its handler (content/scripts/skill_agility/scripts/shortcuts.rs2's
// [oploc1,fullstyle]) have no level/stat check at all — any account can use one, unlike a
// real agility-shortcut obstacle course. A closed door TOGGLES state (Open<->Close), which
// is what tryOpenDoor's own re-verify-via-find() logic depends on; a Stile does not — one
// click teleports the account straight across (p_teleport twice, either side of a walk
// animation) and the fixture itself never changes. tryClimbOver (climbover.go) is
// deliberately a simpler, one-shot version of the same "walk adjacent, click, wait for
// progress" shape: success is confirmed by the account's own tile actually changing, not by
// re-querying the fixture's own state.
var climbOverNames = []string{"Stile", "Crumbling wall"}
var climbOverOps = []string{"Climb-over"}

// doorNames/doorOps mirror stairNames/stairOps's own "try every name against every op"
// shape, for the same reason: this era's content is not consistent about a door/gate's
// exact display name. "Open" covers every closed door/gate actually found live so far
// (confirmed against a real baked loc index — id 1530 "Door", Ops ["Open"], the one that
// sealed casket into a building northeast of Yanille); the extra names are a defensive
// net for gates/large doors this project hasn't hit live yet, not guesses about ops.
var doorNames = []string{"Door", "Gate", "Large door", "Iron gate", "Wooden door", "Metal door"}
var doorOps = []string{"Open"}

// doorOpenOps are the OPENED-state ops for the same door names above — confirmed against the
// real content config (content/scripts/doors/configs/doors.loc: every closed variant's
// op1=Open counterpart is op1=Close on its opened stage). Used by tryOpenDoor's
// already-open detection, never to click (clicking "Close" would toggle a door someone else
// just opened back shut) — only to recognize "this fixture is already open, just walk
// through it."
var doorOpenOps = []string{"Close"}

// doorApproach lists a door's four orthogonal neighbours, tried in turn — see tryOpenDoor's own
// doc comment for why the door's own tile is never a valid WalkNear/walkAdjacent target.
var doorApproach = [4]struct{ dx, dz int }{{0, 1}, {0, -1}, {1, 0}, {-1, 0}}

// componentDirs are the 8 directions localComponent expands through, matching
// pathfind.Pack's own ExitMask bit order (see internal/pathfind/pack.go and cmd/doordiag's
// identical dx/dz arrays, which this mirrors exactly).
var componentDx = [8]int{0, 1, 0, -1, 1, 1, -1, -1}
var componentDz = [8]int{1, 0, -1, 0, 1, -1, -1, 1}

// localComponent returns every tile reachable from start by walking through open ExitMask
// edges only (no doors/stairs — the same "raw collision" view cmd/doordiag's own connected-
// component check uses offline), capped at maxTiles so a large open area can't make this
// expensive. Used by tryOpenDoor's second through-tile strategy: a small sealed room's real
// exit is whichever neighbour this flood fill can't reach within the cap.
func localComponent(pk *pathfind.Pack, start tile.Tile, maxTiles int) map[tile.Tile]bool {
	seen := map[tile.Tile]bool{start: true}
	queue := []tile.Tile{start}
	for len(queue) > 0 && len(seen) < maxTiles {
		cur := queue[0]
		queue = queue[1:]
		mask := pk.ExitMask(cur.X, cur.Z, cur.Level)
		for i := 0; i < 8; i++ {
			if mask&(1<<uint(i)) == 0 {
				continue
			}
			n := tile.Tile{X: cur.X + componentDx[i], Z: cur.Z + componentDz[i], Level: cur.Level}
			if seen[n] || !pk.Walkable(n.X, n.Z, n.Level) {
				continue
			}
			seen[n] = true
			queue = append(queue, n)
		}
	}
	return seen
}

// pickFloodFillThrough is tryOpenDoor's second through-tile strategy (the flood-fill exclusion
// one) in isolation, so it can be unit-tested without a live session: among the door's 4
// orthogonal neighbours (excluding usedOff, the tile just approached from), it flood-fills from
// "from" and returns whichever walkable candidate that fill could NOT reach — on the reasoning
// that a sealed room's one true exit leads somewhere the flood fill can't reach within the cap
// (see localComponent's own doc comment). When more than one candidate qualifies, the one closer
// to dest wins — see this function's call site in tryOpenDoor for the live incident (two
// production doors alternating between their two flood-fill-excluded neighbours forever) that
// made this tie-break necessary: "from" is wherever the account happens to be standing THIS
// call, which is not a stable reference across repeated calls, but dest is, for the life of one
// WalkNear call.
// The pocket size returned alongside is len(reachable) — purely for the caller's own log line,
// the same "pocket size N" diagnostic this strategy has always printed.
func pickFloodFillThrough(pk *pathfind.Pack, found tile.Tile, usedOff struct{ dx, dz int }, from, dest tile.Tile) (tile.Tile, int, bool) {
	reachable := localComponent(pk, from, 1000)
	usedCand := tile.Tile{X: found.X + usedOff.dx, Z: found.Z + usedOff.dz, Level: found.Level}
	var best tile.Tile
	bestDist := -1
	ok := false
	for _, off := range doorApproach {
		cand := tile.Tile{X: found.X + off.dx, Z: found.Z + off.dz, Level: found.Level}
		if cand == usedCand {
			continue // the tile we approached from — never a useful "through" target
		}
		if !pk.Walkable(cand.X, cand.Z, cand.Level) {
			continue
		}
		if reachable[cand] {
			continue
		}
		if d := tile.Chebyshev(cand, dest); !ok || d < bestDist {
			best, bestDist, ok = cand, d, true
		}
	}
	return best, len(reachable), ok
}

// doorCandidate is one nearby door/gate pickDoorCandidate considered, kept alongside its own
// alreadyOpen state (see doorOpenOps' own doc comment — an already-open fixture is walked
// through, never clicked again).
type doorCandidate struct {
	match       locindex.Match
	name        string
	alreadyOpen bool
}

// pickDoorCandidateRadius is generous enough to reach a real second door in a multi-room
// building (the Lumbridge castle incident this function was built for needed one ~5 tiles past
// the first) without scanning the whole map — the same 15-tile radius tryOpenDoor's old
// single-door find()/findOpen() always used.
const pickDoorCandidateRadius = 15

// pickDoorCandidateMaxScored bounds how many of the nearest candidates get the full
// FindPath/flood-fill evaluation below — a dense multi-door building (a real live shape, not
// hypothetical: the Lumbridge castle incident this function fixes has 18+ door/gate locs within
// 25 tiles) could otherwise cost several dozen pk.FindPath calls per tryOpenDoor call. The
// nearest candidates are the ones actually worth walking to anyway (each candidate still needs a
// real approach-walk once picked), so capping here rather than lowering the radius keeps the
// wide net for finding the RIGHT door without paying to evaluate every far one too.
const pickDoorCandidateMaxScored = 12

// pickDoorCandidate is tryOpenDoor's door-SELECTION strategy, factored out the same way
// pickFloodFillThrough/pickDirectionalMatch already are so it can be unit-tested without a live
// session. Real, live-found gap this closes (2026-09-16, production account "spare 22", HCIM
// fleet, Fishing level 60 — ground-truthed offline via cmd/doordiag against the real collision
// pack, not guessed): tryOpenDoor's old find()/findOpen() always took whichever closed or
// already-open door/gate was globally NEAREST to the account's own tile, with no regard at all
// for whether opening it actually leads anywhere. A single room can have more than one closed
// door on its boundary — one a dead end (a small, separately-sealed side room with no further
// exit of its own), another the real way out — and "nearest" has no way to prefer the second
// over the first. Confirmed live: spare 22 sat on a "Door" (id 1536) at 3215,3225 — distance 0,
// the account was standing ON it — whose only unexplored side (3215,3226) leads into an
// isolated 31-tile pocket with no further exit pickFloodFillThrough could find (confirmed via
// cmd/doordiag: FindPath from inside that pocket to Draynor, 3093,3243, fails identically,
// expanded only 31 nodes); a "Large door" (id 1519) about 5 tiles away leads directly out of the
// SAME local room and, from its far side, straight to Draynor (confirmed: expanded 5292 nodes,
// FindPath OK). "Nearest" always picked the dead end, burned walkNearCore's entire
// maxConsecutiveUnreachable STRANDED budget re-discovering and re-opening the SAME dead-end door
// every cycle (find()'s own nearest-wins query returns the identical id 1536 every single time
// from the account's own unchanged position), and never once tried the real exit — the exact
// "STRANDED ... likely a sealed pocket" / "the pier leg did not complete ... retrying" cycle
// logged forever with zero progress.
//
// Fix: rank EVERY closed/open door or gate within radius (not just the nearest) by how much
// dest-ward promise its own four sides show, using the identical two strategies tryOpenDoor's
// through-tile picker already trusted for one door — a confirmed direct FindPath from a
// candidate's own neighbour beats a guess, and a flood-fill exclusion tie-break (now compared
// across every candidate, not just the nearest door's own sides) is the fallback for the common
// multi-door-chain case a same-level static pack can never confirm directly — and commit to
// walking toward whichever door ranks best, not whichever is closest to from.
func pickDoorCandidate(pk *pathfind.Pack, locs *locindex.Index, types locindex.Types, live []locindex.Change, from, dest tile.Tile) (locindex.Match, string, bool, bool) {
	var candidates []doorCandidate
	for _, name := range doorNames {
		for _, op := range doorOps {
			for _, m := range locs.Find(types, locindex.Query{X: from.X, Z: from.Z, Level: from.Level, Radius: pickDoorCandidateRadius, Name: name, Op: op}, live) {
				if guardianOwned(m) {
					continue
				}
				candidates = append(candidates, doorCandidate{m, name, false})
			}
		}
		for _, op := range doorOpenOps {
			for _, m := range locs.Find(types, locindex.Query{X: from.X, Z: from.Z, Level: from.Level, Radius: pickDoorCandidateRadius, Name: name, Op: op}, live) {
				if guardianOwned(m) {
					continue
				}
				candidates = append(candidates, doorCandidate{m, name, true})
			}
		}
	}
	if len(candidates) == 0 {
		return locindex.Match{}, "", false, false
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].match.Distance < candidates[j].match.Distance })
	if len(candidates) > pickDoorCandidateMaxScored {
		candidates = candidates[:pickDoorCandidateMaxScored]
	}

	// Flood fill from "from" ONCE — used both to gate candidate eligibility (the fix below)
	// and, in Strategy 2, as the "not yet reached" test for a candidate's own far side.
	reachable := localComponent(pk, from, 1000)

	// boundaryAdjacent reports whether candidate c is actually ON the boundary of the
	// account's own current room — i.e., at least one of its 4 approach neighbours is a tile
	// the flood fill above already reached, meaning the account could plausibly stand beside
	// it in a single hop, as opposed to a candidate sitting behind at least one MORE door this
	// same room doesn't yet reach.
	boundaryAdjacent := func(c doorCandidate) bool {
		for _, off := range doorApproach {
			nb := tile.Tile{X: c.match.X + off.dx, Z: c.match.Z + off.dz, Level: c.match.Level}
			if reachable[nb] {
				return true
			}
		}
		return false
	}

	// attempt runs both existing strategies — Strategy 1 (a confirmed direct route out of a
	// candidate's own far side) then Strategy 2 (the flood-fill-exclusion/closest-to-dest
	// tie-break) — restricted to whichever candidates allow accepts (every candidate, when
	// allow is nil), returning ok=false only if NEITHER strategy found anything among the
	// allowed candidates. Factored out so both strategies can be tried twice below: once
	// gated on real room-boundary adjacency, and once wide open as the pre-existing fallback.
	attempt := func(allow func(doorCandidate) bool) (locindex.Match, string, bool, bool) {
		// Strategy 1: a confirmed direct route. Any candidate with a walkable neighbour
		// FindPath can already route to dest from wins outright — candidates is
		// distance-sorted, so the nearest candidate that qualifies at all is preferred among
		// those with a confirmed route.
		for _, c := range candidates {
			if allow != nil && !allow(c) {
				continue
			}
			for _, off := range doorApproach {
				nb := tile.Tile{X: c.match.X + off.dx, Z: c.match.Z + off.dz, Level: c.match.Level}
				if !pk.Walkable(nb.X, nb.Z, nb.Level) {
					continue
				}
				if res := pk.FindPath(pathfind.NavPoint{X: nb.X, Z: nb.Z, Level: nb.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level}); res.OK {
					return c.match, c.name, c.alreadyOpen, true
				}
			}
		}

		// Strategy 2: no candidate's own sides has a confirmed direct route — the common
		// multi-door-chain case (dest is beyond more than one closed door, so no single
		// same-level FindPath from any nearby door's own neighbour can reach it directly).
		// Across every nearby door's sides (the original fix here — the old code only ever
		// compared the nearest door's own four sides), take whichever candidate the flood
		// fill could not reach and is closest to dest, the same tie-break pickFloodFillThrough
		// already uses within a single door.
		bestIdx, bestDist := -1, -1
		for i, c := range candidates {
			if allow != nil && !allow(c) {
				continue
			}
			for _, off := range doorApproach {
				nb := tile.Tile{X: c.match.X + off.dx, Z: c.match.Z + off.dz, Level: c.match.Level}
				if !pk.Walkable(nb.X, nb.Z, nb.Level) || reachable[nb] {
					continue
				}
				if d := tile.Chebyshev(nb, dest); bestIdx == -1 || d < bestDist {
					bestIdx, bestDist = i, d
				}
			}
		}
		if bestIdx == -1 {
			return locindex.Match{}, "", false, false
		}
		return candidates[bestIdx].match, candidates[bestIdx].name, candidates[bestIdx].alreadyOpen, true
	}

	// Real, live-found gap this closes (2026-09-17, production accounts "acct"/"acct12"/
	// "acct29"/"acct64", the "lubmy basement" pocket, mapsquare 50,50 — reproduced live via
	// cmd/fisherstrandtest -x 3209 -z 3217, confirmed offline too via cmd/doordiag against the
	// real collision pack: the connected component from 3209,3217,0 is a genuine 29-tile room
	// whose real boundary doors are id 1536 at 3207,3217 and id 1530 at 3208,3211 (each has a
	// neighbour inside that 29-tile room), while the "Large door" id 1516 at 3217,3218 has
	// ZERO neighbours inside it — genuinely two rooms away — despite sitting closest to dest
	// as the crow flies; see docs/script_parity_status.md's own write-up):
	//
	// An EARLIER version of this fix only gated Strategy 2 on boundary adjacency, leaving
	// Strategy 1 completely unguarded — and that was not enough. Confirmed live even after
	// that first fix (cmd/fisherstrandtest -x 3209 -z 3217, account "bugaBfixed1"): tryOpenDoor
	// kept picking the exact same "Large door" forever, because Strategy 1 does not check
	// whether the ACCOUNT can reach a candidate's neighbour at all — only whether that
	// neighbour has a real FindPath onward to dest. The "Large door"'s far side genuinely
	// does eventually connect to the wider world dest sits in (it just isn't reachable from
	// the account's own current room without crossing at least one more closed door first),
	// so Strategy 1 "confirmed" it immediately and returned before Strategy 2 (and its new
	// adjacency gate) ever ran at all.
	//
	// Fix: gate BOTH strategies on real room-boundary adjacency together (via the shared
	// `attempt` closure above), not just Strategy 2 — mirroring the inside/outside
	// distinction tryEscapeSealedPocket's own BFS already gets right, and only when the flood
	// fill above found a genuine, explored multi-tile room (more than just the account's own
	// standing tile). Deliberately gated on len(reachable) > 1, not applied unconditionally:
	// see TestPickDoorCandidatePrefersConfirmedRouteOverNearestDeadEnd and
	// TestPickDoorCandidateFloodFillPrefersDestCloserCandidateAcrossDoors (both guarding a
	// different real, already-fixed incident, production account "spare 22", Lumbridge
	// castle) for why an unconditional adjacency requirement is unsafe — both tests' own pack
	// deliberately makes `from` a totally isolated 1-tile "room" with no edges to anything at
	// all, so reachable={from} only, and EVERY candidate within one tile of `from` would
	// trivially "pass" an adjacency check that merely asks whether a candidate borders
	// `reachable` (since `reachable` always trivially contains `from` itself) — that carries
	// no real boundary information when the fill couldn't explore anything beyond the single
	// starting tile, and the farther, genuinely non-adjacent door is the intended winner in
	// both. A real, live standing tile is never that degenerate (there is always at least
	// some real room around it), so gating on room size — not skipping the adjacency
	// requirement altogether — separates the two shapes: a real multi-tile room enforces
	// adjacency on both strategies and fixes lubmy basement, while the synthetic single-tile
	// "room" falls through to the exact pre-existing unrestricted behaviour both rex-hunt
	// tests lock in.
	if len(reachable) > 1 {
		if m, n, ao, ok := attempt(boundaryAdjacent); ok {
			return m, n, ao, ok
		}
	}
	if m, n, ao, ok := attempt(nil); ok {
		// Either the room was too degenerate to judge adjacency at all, or no boundary
		// candidate discriminated anything in either strategy (every boundary door was
		// either a dead end with no confirmed route AND had its own unreached side already
		// reachable/unwalkable) — fall back to the original unrestricted behaviour rather
		// than reporting no candidate: still sometimes right, and tryOpenDoor's own final
		// mechanical-opposite through-tile guess still applies downstream regardless of which
		// door gets walked to.
		return m, n, ao, ok
	}
	// Nothing discriminates at all (every candidate's sides are either unwalkable or already
	// reachable from from, and none has a confirmed route either) — fall back to the plain
	// nearest candidate rather than reporting no candidate at all: still sometimes right, and
	// tryOpenDoor's own final mechanical-opposite through-tile guess still applies downstream
	// regardless of which door gets walked to.
	return candidates[0].match, candidates[0].name, candidates[0].alreadyOpen, true
}

// tryOpenDoor looks for a real closed door/gate within a generous radius of from and, if found,
// walks beside it and opens it — the same one-shot, re-evaluate-on-the-caller's-next-iteration
// shape as tryChangeLevel, called from WalkNear's own "unreachable" branch for the identical
// reason: internal/pathfind bakes a closed door as a solid wall (no notion of "passable with an
// action" edges — the documented v1 scope), so an account sealed into a room by one looks
// identical to a genuinely unreachable destination until something actually opens it. Verifies
// the door's own state really changed (per this project's "verify the game state changed"
// discipline) rather than trusting a successful click.
//
// Real bug found and fixed live, not anticipated going in: the first version of this
// verification re-ran the discovery query at Radius:0 pinned exactly to the door's own
// reported X/Z. That's wrong for a wall-shape loc — footprintDistance's own origin/rotation
// math (see query.go) does not guarantee a self-distance of exactly 0 for every angle, so the
// query silently matched nothing from the very first check, before OpLoc had even had a
// chance to take effect. waitFor's cond() was therefore true on its very first poll every
// time, so tryOpenDoor returned true instantly whether or not the door actually opened —
// WalkNear's own "continue" then re-ran FindPath, found the same closed door still blocking
// the same path, and called tryOpenDoor again, in a near-zero-cost loop. Confirmed live
// against a real sealed-building incident (a throwaway account teleported to 2651,3165,0,
// the same coordinates casket got random-evented to on production): thousands of identical
// "opening \"Door\" at 2656,3161,0 to clear the way" lines with no delay between them. Fixed
// by re-running the exact same *discovery* query (same shape that reliably found the door in
// the first place, from the player's own position rather than a Radius:0 self-match at the
// door's own tile) and checking whether it still resolves to the same loc ID: a real open
// door replaces the closed one with a different ID on the same wall layer (see locindex's own
// TestLiveChangesOverrideTheMapFiles), so a changed ID (or no match at all) means real
// progress, and an unchanged ID means the click has not taken effect yet.
// LOCAL ENGINE AND PRODUCTION safe: no cheats, the same OpLoc a real player's own door click
// sends.
func tryOpenDoor(d *Deps, from, dest tile.Tile) bool {
	find := func() (locindex.Match, string, bool) {
		live := locindex.ChangesFrom(d.Session.State().LocChanges())
		// Scan every name/op combination and keep the globally NEAREST match, rather than
		// returning on the first name in doorNames that has any hit within radius 15. Real
		// bug found live (2026-09-15, the Karamja mainland gate at 2816,3182): "Door" is
		// tried before "Gate" in doorNames, so an entirely unrelated "Door" 12 tiles away
		// won every time over the actual blocking "Gate" sitting on the account's own
		// tile — the account walked in circles opening/approaching the wrong fixture
		// forever while standing right next to the real one.
		var best locindex.Match
		var bestName string
		found := false
		for _, name := range doorNames {
			for _, op := range doorOps {
				if m, ok := d.Locs.Nearest(d.Config, locindex.Query{X: from.X, Z: from.Z, Level: from.Level, Radius: 15, Name: name, Op: op}, live); ok {
					if !found || m.Distance < best.Distance {
						best, bestName, found = m, name, true
					}
				}
			}
		}
		return best, bestName, found
	}
	// Door SELECTION itself (which nearby closed/open door or gate to actually walk toward)
	// is pickDoorCandidate's job now, not a plain "nearest wins" comparison here — see its own
	// doc comment for the real live incident (account "spare 22", Lumbridge castle) that made
	// "nearest" the wrong question: a room can have more than one closed door on its boundary,
	// and only ranking every nearby candidate by dest-ward promise (not just proximity to from)
	// reliably picks the one that actually leads somewhere instead of the closest dead end.
	// find() above is kept only for the later re-verify-by-id check once a specific door has
	// already been committed to (line ~1233) — a plain nearest-to-from query is exactly right
	// there, since that check just needs to know whether THIS door's own state changed, not
	// which door to pick.
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	found, foundName, alreadyOpen, ok := pickPlannedDoor(d, from, dest)
	if !ok {
		found, foundName, alreadyOpen, ok = pickDoorCandidate(liveNavigationPack(d), d.Locs, d.Config, live, from, dest)
	}
	if !ok {
		return false
	}
	if alreadyOpen {
		d.logf("%q at %d,%d,%d is already open — walking through instead of clicking it again", foundName, found.X, found.Z, found.Level)
	} else {
		d.logf("opening %q at %d,%d,%d to clear the way", foundName, found.X, found.Z, found.Level)
	}
	// Try the wall's own tile first when reachable. ReachStrategy accepts that
	// tile for an Open interaction. Moving one tile away can make a previously
	// reachable door unclickable (live Lumbridge 3207,3225, reproduced locally
	// with "I can't reach that!"). Other sides may not reach the wall's tile,
	// so retain the orthogonal-neighbour fallback for those approaches.
	// Historical reason for that fallback:
	// Real, live-found bug, not a guess: a closed door sits on its OWN walkable tile with only
	// the crossing edge blocked (per-tile ExitMask, confirmed against the real baked pack —
	// door tile (2656,3161): Walkable=true, but the tile south of it has no exit bit pointing
	// at it) — unlike a stair/ladder, which is a genuinely solid object occupying its tile, so
	// internal/pathfind's own approach-tile fallback (astar.go's cardinalGoals, what makes
	// tryChangeLevel's identical-looking WalkNear-to-the-fixture call work) only ever engages
	// for a truly unwalkable destination and never engages for a door. Routing straight at the
	// door's own coordinate therefore reports "unreachable" even standing right beside it,
	// which is exactly what kept happening after the stack-overflow fix above (a clean,
	// correctly-paced retry loop that still never made progress). Trying each neighbour in
	// turn and taking the first plain FindPath can actually reach is the same thing a real
	// player's own approach amounts to — no engine change, no new capability, just not asking
	// the router a question its collision model can't answer.
	reachedDoor := false
	var usedOff struct{ dx, dz int }
	if d.Pack.Walkable(found.X, found.Z, found.Level) {
		reachedDoor = walkAdjacent(d, tile.Tile{X: found.X, Z: found.Z, Level: found.Level}, 0, 8*time.Second)
	}
	for _, off := range doorApproach {
		if reachedDoor {
			break
		}
		if walkAdjacent(d, tile.Tile{X: found.X + off.dx, Z: found.Z + off.dz, Level: found.Level}, 0, 8*time.Second) {
			reachedDoor, usedOff = true, off
			break
		}
	}
	if !reachedDoor {
		// Said only for a live bot: a stopping one fails every approach at its first poll, and the line would
		// read as a door with no way to it (the 14 Oct log's "Door" 2815,3449 has a reachable south tile).
		if d.Delay(0) {
			d.logf("%q at %d,%d,%d: could not reach any of its 4 approach tiles", foundName, found.X, found.Z, found.Level)
		}
		return false
	}
	// postApproachTile is the account's real, CURRENT tile right after the approach-walk
	// above — NOT `from`, this whole function's own parameter, which is the account's
	// position BEFORE that walk. Real, live-found bug this closes (2026-09-16, "Large
	// door" 3212,3220,0, independently on accounts "acct" and "acct45", still
	// oscillating live when found): the approach loop just above always tries
	// doorApproach's fixed South/North/East/West order, regardless of which side the
	// account already happens to be standing on, so it routinely ends up walking the
	// account to a DIFFERENT side of the door than wherever `from` was. Below,
	// pickFloodFillThrough's flood-fill used to be seeded from that now-stale `from` —
	// measuring reachability from the side the account had just walked AWAY from, not
	// the side it is actually standing on when the fill runs. Confirmed live: mario
	// alternated between the door's West and East approach tiles every single retry
	// (3211,3220 <-> 3213,3220, pocket sizes 54/43 flipping in lockstep) — the "closer
	// to dest" tie-break the 2026-09-15 fix relies on (see pickFloodFillThrough's own
	// doc comment) was evaluating reachability from the wrong side every time it
	// mattered, so it kept "breaking the tie" toward whichever side was NOT the stale
	// `from`, i.e. toward wherever the account had just left — the exact opposite of
	// convergence. Falls back to `from` itself only if the account's tile can't be read
	// this exact instant, rather than aborting a recovery this deep into a stall.
	postApproachTile := from
	if cur := currentTile(d); cur != nil {
		postApproachTile = *cur
	}
	if !alreadyOpen {
		if err := actions.OpLoc(d.Session, found.Op, found.X, found.Z, found.ID); err != nil {
			d.logf("OpLoc on %q: %v", foundName, err)
			return false
		}
		beforeID := found.ID
		if !waitFor(d, "the door to open", 5*time.Second, func() bool {
			m, _, ok := find()
			return !ok || m.ID != beforeID
		}) {
			// Don't give up here — OpLoc returned no error, so the server almost
			// certainly accepted the click; find()'s own "still closed" read can lag or
			// race with a live multi-account world (findOpen's own doc comment above
			// already documents shared doors getting toggled by other accounts
			// concurrently). Found live 2026-09-15 running many accounts on the same
			// door-dense stretch near Lumbridge/Varrock: this branch used to hard-fail
			// every retry forever on a door that had, in fact, already opened,
			// permanently stranding the account (including after a death respawn,
			// misread as "isn't recovering back to Catherby" when the real cause was
			// this false negative). Fall through to the same raw step-through the
			// already-open path takes below instead of aborting.
			d.logf("%q at %d,%d,%d: find() still reports it closed after 5s — trying the step through anyway", foundName, found.X, found.Z, found.Level)
		}
	}
	// The door is now genuinely open (just confirmed live, above) — but internal/pathfind's
	// collision Pack is a static file baked once from the map's own default (closed) state and
	// has no live-override hook at all (checked directly: neither Pack nor FindPath takes any
	// form of "live change" parameter, unlike locindex.Index.Nearest's own live []Change
	// argument). So FindPath will keep reporting this exact edge "unreachable" forever, no
	// matter how many times this whole function successfully reopens the same already-open
	// door — confirmed live: after this exact fix landed, the account reached the tile beside
	// the door, but every subsequent SailLeg attempt still failed with the identical "no path
	// ... unreachable", now from the far side, because WalkNear's very next FindPath call
	// consulted the same static pack that has never heard of the click that just happened.
	// One raw step straight across the threshold — bypassing FindPath entirely for this single
	// edge, the same MOVE_GAMECLICK a real player's own next click sends — sidesteps that
	// gap completely: the tiles on the far side of a real door are correctly baked in the
	// static pack like everywhere else in the world, so ordinary FindPath-based routing works
	// again immediately once the account is actually standing past it.
	//
	// through used to be computed as the door's own coordinate MINUS the approach offset — the
	// mechanical opposite side, assuming every door is a simple single-file passage with
	// exactly two sides (inside/outside, opposite each other). Real bug found live (2026-09-15,
	// a "Gate" stranding near Brimhaven, 3107,3274): that assumption breaks for a door that is
	// part of a larger walled compound, where 3 of its 4 orthogonal neighbours are already
	// inside the same reachable area and only ONE side is genuinely outside — doorApproach's
	// fixed N/S/E/W trial order picks whichever side is reachable FIRST (almost always one of
	// the 3 "inside" ones, since that's the side the account is already standing on), so the
	// mechanical opposite of THAT is just another "inside" tile: real position changes, zero
	// real progress, the exact "opening Gate... still unreachable" loop found live. Fixed by
	// testing all 4 neighbours (skipping the approach tile itself) against a real
	// d.Pack.FindPath toward dest and taking the first one FindPath says it can actually reach
	// from — the same router already used everywhere else in this function, just asked a
	// question its static pack CAN answer (ordinary same-side routing) instead of guessing.
	through := tile.Tile{X: found.X, Z: found.Z, Level: found.Level}
	throughFound := false
	for _, off := range doorApproach {
		cand := tile.Tile{X: found.X + off.dx, Z: found.Z + off.dz, Level: found.Level}
		if cand == (tile.Tile{X: found.X + usedOff.dx, Z: found.Z + usedOff.dz, Level: found.Level}) {
			continue // the tile we approached from — never a useful "through" target
		}
		if res := findPathAvoidingDanger(d, pathfind.NavPoint{X: cand.X, Z: cand.Z, Level: cand.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level}); res.OK {
			through, throughFound = cand, true
			d.logf("%q at %d,%d,%d: through-tile %d,%d,%d chosen via direct FindPath (approached from %d,%d)", foundName, found.X, found.Z, found.Level, cand.X, cand.Z, cand.Level, usedOff.dx, usedOff.dz)
			break
		}
	}
	if !throughFound {
		// No neighbour's own FindPath reaches dest directly. The most common real reason,
		// found live the same session as the fix above (devlab13, an Ardougne-area level-1
		// room, door at 2709,3472): dest is on a DIFFERENT level than this door (e.g. the
		// account still needs a staircase down after this door), so d.Pack.FindPath — which
		// never crosses levels — fails identically for every same-level candidate and can't
		// discriminate at all. Second strategy: a small bounded flood fill from the account's
		// OWN current tile (the same technique cmd/doordiag's own "connected component" check
		// already uses offline, ported here for runtime use) — whichever candidate falls
		// OUTSIDE that reachable set is the real exit, on the reasoning that a sealed room's
		// interior is small and its one true exit leads somewhere the flood fill can't reach
		// within the cap.
		// 1000, not 400: live-found (2026-09-15, this exact Gate at 3107,3274) that the
		// real local pocket here is 528 tiles (confirmed offline via cmd/doordiag's own
		// uncapped flood fill) — bigger than the old 400-tile cap, so the BFS below hit
		// its cap before ever reaching the gate's own true outside neighbour, leaving
		// every candidate marked "not yet seen" (indistinguishable from "genuinely
		// outside") and letting the loop below pick the first untested candidate at
		// random rather than the real exit. 1000 comfortably covers this and similar
		// real building/compound interiors while staying far below doordiag's own
		// 400,000 default (which only matters for a one-off offline diagnostic, not a
		// per-door runtime check).
		// localComponent's own reachable set (see its doc comment/implementation) only ever
		// contains WALKABLE tiles the flood fill actually visited — so "not reachable" is true
		// both for a genuine outside-the-pocket exit AND for a candidate that's simply
		// unwalkable (a wall, the door's own solid tile, etc.), which can't be told apart
		// without checking walkability itself. Found live 2026-09-15 (lc2b_196, door "Door"
		// 3107,3162, pocket size 15): doorApproach's fixed offset order tried the N neighbour
		// first, an unwalkable tile per the pack, and picked it as the "through" tile purely
		// because it happened to come before the real exit (E, walkable, correctly outside the
		// pocket) in the loop order — an unwalkable "exit" then made every subsequent
		// step-through attempt time out forever, looping this exact door/pocket pair
		// indefinitely with no escalation (each retry re-entered this same function fresh
		// rather than accumulating toward WalkNear's own STRANDED counter). pickFloodFillThrough
		// skips unwalkable candidates up front so the exclusion test actually means "walkable
		// AND outside", which is what "the real exit" has always needed to mean.
		//
		// Real bug found live 2026-09-16, not anticipated going in (30-account production
		// fleet, two separate Lumbridge castle "Door"s, ids 1530 at 3208,3211,0 and 1536 at
		// 3215,3211,0, both already opened by other accounts by the time these accounts arrived):
		// this used to take the FIRST candidate doorApproach's fixed {0,1},{0,-1},{1,0},{-1,0}
		// order excluded, with no regard for which one actually leads toward dest. Confirmed via
		// production logs (accounts "acct"/"runescape" on the 3208 door, "Spare 24" on the
		// 3215 door): the already-open door's own reported tile (3208,3212,0 / 3215,3212,0 — one
		// tile off from the closed door's own static address, which is where the live-open
		// loc-change actually lands; internal/locindex is decoding this correctly, see
		// TestFindPrefersLiveChangeOverStaticEvenWhenAdjacent) sits in a one-tile vestibule
		// between two small rooms of near-identical size (both flood-fills reported "pocket
		// size 29"), so EVERY call's localComponent — run fresh from wherever the account is
		// STANDING RIGHT NOW, i.e. wherever the PREVIOUS call's own pick just walked it to —
		// found the account's current side "reachable" and the opposite side "excluded",
		// whichever side that happened to be. The two candidates (3208,3211,0 and 3208,3213,0;
		// 3215,3211,0 and 3215,3213,0) then took turns being "the excluded one" and the account
		// walked back and forth between them forever, escalating to STRANDED, backing off 30s,
		// and repeating identically — confirmed live across multiple STRANDED cycles with zero
		// real progress. "from" (the account's current tile) is not a stable reference across
		// calls, but dest is, for the life of one WalkNear call — so when more than one
		// candidate is excluded, pickFloodFillThrough now breaks the tie toward whichever one is
		// actually closer to dest instead of whichever the fixed loop order hit first,
		// converging on the dest-ward side instead of alternating with it.
		foundTile := tile.Tile{X: found.X, Z: found.Z, Level: found.Level}
		if cand, pocketSize, ok := pickFloodFillThrough(d.Pack, foundTile, usedOff, postApproachTile, dest); ok {
			through, throughFound = cand, true
			d.logf("%q at %d,%d,%d: through-tile %d,%d,%d chosen via flood-fill exclusion (pocket size %d)", foundName, found.X, found.Z, found.Level, cand.X, cand.Z, cand.Level, pocketSize)
		}
	}
	if !throughFound {
		// Neither strategy above found a discriminating candidate (a genuinely ambiguous or
		// fully-enclosed case) — fall back to the old mechanical-opposite guess rather than
		// giving up, since it is still sometimes right and doing nothing is strictly worse.
		through = tile.Tile{X: found.X - usedOff.dx, Z: found.Z - usedOff.dz, Level: found.Level}
		d.logf("%q at %d,%d,%d: no strategy could pick a through-tile — guessing mechanical opposite %d,%d,%d", foundName, found.X, found.Z, found.Level, through.X, through.Z, through.Level)
	}
	// actions.Walk's own contract requires waypoints[0] to be the player's CURRENT tile
	// (it becomes the packet's absolute start, with every later corner encoded as a
	// delta from it — see its own doc comment). A real, live-found bug: this call used
	// to pass just the through tile alone, a single-element list — meaning THAT tile
	// was wrongly treated as "where I already am," so the resulting click told the
	// server "I'm already at my destination," moved the account nowhere, and every
	// following waitFor "the step through the door" timed out identically forever
	// (confirmed live 2026-09-15, cyclev8_2, a real door at 3102,3258: the exact same
	// "timed out waiting for the step through the door" / re-pick-the-same-through-tile
	// cycle repeating unbroken for 45+ seconds). Prefix the account's own current tile.
	here := currentTile(d)
	if here == nil {
		return true
	}
	if err := actions.Walk(d.Session, []pathfind.NavPoint{{X: here.X, Z: here.Z, Level: here.Level}, {X: through.X, Z: through.Z, Level: through.Level}}, false); err != nil {
		d.logf("walking through %q: %v", foundName, err)
		return true // the door did open; a later WalkNear retry can still finish the step
	}
	// Propagate whether the step-through actually verified, instead of the previous
	// unconditional "return true" that discarded waitFor's own result. Found live
	// 2026-09-15 (lc2b_196, a wall-shaped "Door" whose locindex match aliases between two
	// adjacent tiles — 3106,3162 vs 3107,3162 — depending on which query matched it): every
	// candidate this function computes is built from whichever alias happened to resolve,
	// so a wrong alias makes EVERY strategy above pick a wrong through-tile, and the step
	// there never actually lands. Because this function always returned true regardless,
	// walkNearCore's stall branch (`if recover && tryOpenDoor(...) { consecutiveUnreachable
	// = 0; continue }`) reset the STRANDED counter to zero every single cycle even though
	// zero real progress had been made — the account genuinely could never escalate to
	// STRANDED/rescue no matter how many times this looped, confirmed live: 0 STRANDED
	// hits after 8+ minutes and dozens of identical cycles. Returning the real outcome lets
	// a through-tile that never lands count as a failed recovery attempt like any other,
	// so it now accumulates toward the same STRANDED/rescue path a genuinely sealed pocket
	// already gets — closing the gap without needing to fix locindex's own aliasing (a much
	// larger, riskier change touching every door/gate interaction, not attempted here).
	return waitFor(d, "the step through the door", 5*time.Second, func() bool {
		cur := currentTile(d)
		return cur != nil && cur.X == through.X && cur.Z == through.Z
	})
}

// ---- the appearance screen ----

// findButtonByText is chatContinueComId's cousin for the appearance screen: a BFS from
// rootComID over d.Components looking for any button-typed component (any ButtonType, not
// just BUTTON_OK the way ChatDialog.Options restricts itself) whose Text or ButtonText
// contains want, case-insensitively. Ported from readerX.buttonByText.
func findButtonByText(components archive.Components, rootComID int, want string) int {
	want = strings.ToLower(want)
	queue := []int{rootComID}
	seen := map[int]bool{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		com, ok := components[id]
		if !ok {
			continue
		}
		if com.ButtonType != 0 {
			label := com.Text
			if label == "" {
				label = com.ButtonText
			}
			if label != "" && strings.Contains(strings.ToLower(label), want) {
				return com.ID
			}
		}
		queue = append(queue, com.Children...)
	}
	return -1
}

// AcceptAppearance presses the appearance screen's Accept button by caption (it has no
// close cross), ported from acceptAppearance. Requires d.Components; without it, logs and
// waits rather than failing outright, since a brand-new account still needs a human (or a
// later retry once the archive is available) to get past this screen.
func AcceptAppearance(d *Deps) {
	if d.Components == nil {
		d.logf("no interface archive configured — cannot find the Accept button; accept the appearance by hand")
		d.Delay(3 * time.Second)
		return
	}
	comID := findButtonByText(d.Components, freshstart.DesignModal, "accept")
	if comID == -1 {
		d.logf("no Accept button found under interface %d — accept the appearance by hand", freshstart.DesignModal)
		d.Delay(3 * time.Second)
		return
	}
	if err := actions.IfButton(d.Session, comID); err != nil {
		d.logf("press Accept: %v", err)
		d.Delay(2 * time.Second)
		return
	}
	if waitFor(d, "the appearance screen to close", 5*time.Second, func() bool {
		main, _, _ := d.Session.State().Modals()
		return main != freshstart.DesignModal
	}) {
		d.logf("accepted the appearance")
	}
}

// ---- the tutorial skip ----

// SkipTutorial answers the RuneScape Guide's "skip the tutorial?" dialogue, ported from
// skipTutorial: an open option list is answered with skipOption (falling back to any
// option containing "yes", then the first option at all); an open continue prompt is
// pressed; otherwise the guide is found by name (requiring a Talk-to op) and walked to.
func SkipTutorial(d *Deps, guideNpc, skipOption string) {
	dlg := actions.NewChatDialog(d.Session, d.Components)
	if len(dlg.Options()) > 0 {
		if err := dlg.ChooseOption(skipOption); err != nil {
			if err2 := dlg.ChooseOption("yes"); err2 != nil {
				d.logf("no dialogue option matching %q or \"yes\" — choosing the first option", skipOption)
				_ = dlg.ChooseOption("")
			}
		}
		d.Delay(1200 * time.Millisecond)
		return
	}
	if dlg.CanContinue() {
		_ = dlg.Continue()
		// Same unconditional-floor reasoning as the guide-click path below: Continue's
		// own one-press-per-open latch (PauseButtonResumed) means the very next Loop()
		// call can find CanContinue() false again before the *next* dialogue page has
		// actually loaded — with no delay here, that fell through past this branch
		// entirely into the guide-click code below and re-clicked the NPC mid-dialogue.
		d.Delay(700 * time.Millisecond)
		return
	}
	if dlg.IsOpen() {
		// Found live (2026-09-15): a dialogue can be genuinely open — the guide is
		// mid-speech — while BOTH Options() and CanContinue() are momentarily false
		// (between pages, right after a Continue press, before the next page's own
		// continue/option button has loaded). Falling through to the NPC-click code
		// below in that state re-sent "Talk-to" on an NPC we were already mid-dialogue
		// with, which is wrong even with the click itself now rate-limited — it can
		// confuse or restart the guide's own dialogue tree. Just wait for the next
		// page instead of re-clicking.
		d.Delay(400 * time.Millisecond)
		return
	}
	guide, ok := findNpcByNameOp(d, guideNpc, currentTileOrZero(d), 30, "Talk-to")
	if !ok {
		d.logf("no %q to talk to nearby — check the guide NPC setting", guideNpc)
		d.Delay(3 * time.Second)
		return
	}
	if tile.Chebyshev(currentTileOrZero(d), tile.Tile{X: guide.X, Z: guide.Z, Level: guide.Level}) > 3 {
		WalkNear(d, tile.Tile{X: guide.X, Z: guide.Z, Level: guide.Level}, 2, 20*time.Second)
		return
	}
	npcType, _ := d.Config.Npc(guide.Type)
	op := cachecfg.OpIndex(npcType.Ops, "Talk-to")
	if op == 0 {
		op = 1
	}
	if err := actions.OpNpc(d.Session, op, guide.Index); err != nil {
		d.logf("Talk-to %s: %v", guideNpc, err)
		d.Delay(600 * time.Millisecond)
		return
	}
	waitFor(d, "the guide's dialogue to open", 6*time.Second, dlg.IsOpen)
	// Unconditional floor, deliberately not folded into waitFor's own delay: found live
	// (2026-09-15) that dlg.IsOpen() can read true on waitFor's very FIRST check — before
	// any real delay elapses — for a reason not yet root-caused (plausibly some leftover/
	// unrelated modal state right after the appearance screen closes). Runner.run()'s own
	// loop calls Bot.Loop back-to-back with zero delay of its own, so a zero-delay return
	// here let SkipTutorial re-enter and resend the identical "Talk-to" click ~25 times in
	// under a millisecond — the production/local-lab symptom was every fresh account
	// getting forcibly disconnected right at this exact point, which is exactly what the
	// server does to a click rate no real client can produce. This floor makes that
	// impossible regardless of why IsOpen() was wrong, the same "guard Loop()'s zero-delay
	// re-entry" fix already applied twice elsewhere tonight (donorrunner, crafter.go) — the
	// difference here is the zero-delay loop got caught by the server's own anti-cheat
	// instead of just burning CPU.
	d.Delay(700 * time.Millisecond)
}

func currentTileOrZero(d *Deps) tile.Tile {
	if t := currentTile(d); t != nil {
		return *t
	}
	return tile.Tile{}
}

// ---- the general store ----

// SellStarterKit walks to the Lumbridge general store stand, opens it (through either
// keeper freshstart.ShopKeepers names), and sells everything but keep — ported from
// storeTrip/sellPack. Reuses fisherplan.StarterKeep()'s own keep-list for the caller's
// convenience is deliberately NOT done here (this package stays script-agnostic); pass the
// caller's own keep list.
func SellStarterKit(d *Deps, standTile tile.Tile, keep []string, refused map[string]bool) (sold int, gp int) {
	here := currentTileOrZero(d)
	if tile.Chebyshev(here, standTile) > 3 {
		WalkNear(d, standTile, 2, 20*time.Second)
		return 0, 0
	}
	shop := actions.NewShop(d.Session)
	opened := false
	for _, keeper := range freshstart.ShopKeepers() {
		npc, ok := findNpcByNameOp(d, keeper, standTile, 10, "")
		if !ok {
			continue
		}
		npcType, _ := d.Config.Npc(npc.Type)
		op := cachecfg.OpIndex(npcType.Ops, "Trade")
		if op == 0 {
			op = 3
		}
		if err := actions.OpNpc(d.Session, op, npc.Index); err != nil {
			continue
		}
		if waitFor(d, "the general store to open", 4*time.Second, shop.IsOpen) {
			opened = true
			break
		}
	}
	if !opened {
		d.logf("could not open the general store (neither keeper found or answering)")
		d.Delay(2 * time.Second)
		return 0, 0
	}
	defer shop.Close()

	pack := shop.Pack()
	items := make([]freshstart.PackItem, 0, len(pack))
	ids := map[string]int32{}
	for _, it := range pack {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok {
			continue
		}
		items = append(items, freshstart.PackItem{Name: obj.Name, Count: int(it.Count)})
		ids[strings.ToLower(obj.Name)] = it.ID
	}
	plan := freshstart.SellQueue(items, keep, refused)
	before := coinsHeld(d)
	for _, line := range plan {
		id, ok := ids[strings.ToLower(line.Name)]
		if !ok {
			continue
		}
		if err := shop.Sell(id, int32(line.Count)); err != nil {
			d.logf("sell %q: %v", line.Name, err)
			continue
		}
		d.Delay(300 * time.Millisecond)
		after := coinsHeld(d)
		if after <= before {
			refused[strings.ToLower(line.Name)] = true
			continue
		}
		gp += after - before
		before = after
		sold += line.Count
	}
	return sold, gp
}

// ---- the pink skirt ----

// skirtObjIDs is every item type id named freshstart.PinkSkirt in this content build —
// ordinarily exactly one, looked up by name (like barbarianrunner.BankForFood's own food-id
// lookup) rather than hardcoded, so a different content build's id needs no code change.
func skirtObjIDs(d *Deps) []int32 {
	ids := d.Config.ObjIDs(freshstart.PinkSkirt)
	out := make([]int32, len(ids))
	for i, id := range ids {
		out[i] = int32(id)
	}
	return out
}

// containsAnyID reports whether items holds any of ids, returning the matching id.
func containsAnyID(items []gamestate.SlotItem, ids []int32) (int32, bool) {
	for _, it := range items {
		for _, id := range ids {
			if it.ID == id {
				return it.ID, true
			}
		}
	}
	return 0, false
}

// BuySkirtStep is one pass of the pink-skirt purchase, ported from newAccount.ts's/
// ProgressiveCrafter.ts's own skirtStep: wear one already held, walk to Thessalia's Fine
// Clothes stand in Varrock if far, otherwise open her shop and buy one. The skirt is a real,
// in-game requirement (not a script invention) for clearing the Port Sarim customs check to
// Karamja in this era — ProgressiveCrafter.ts's own comment calls it out the same way. Returns
// true once the skirt is worn; false means try again next call (mirrors SellStarterKit's own
// "one call, one step" shape).
func BuySkirtStep(d *Deps) bool {
	ids := skirtObjIDs(d)
	if len(ids) == 0 {
		d.logf("no %q item type known in this content build — cannot buy or wear one", freshstart.PinkSkirt)
		d.Delay(3 * time.Second)
		return false
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	if _, worn := containsAnyID(eq.Worn(), ids); worn {
		return true
	}
	if heldID, held := containsAnyID(eq.Inventory(), ids); held {
		if err := eq.Equip(heldID); err != nil {
			d.logf("equip pink skirt: %v", err)
			d.Delay(600 * time.Millisecond)
			return false
		}
		worn := waitFor(d, "the pink skirt to be worn", 3*time.Second, func() bool {
			_, w := containsAnyID(eq.Worn(), ids)
			return w
		})
		if worn {
			d.logf("wearing the pink skirt")
		}
		return worn
	}
	here := currentTileOrZero(d)
	if tile.Chebyshev(here, freshstart.ThessaliaStand) > 3 {
		WalkNear(d, freshstart.ThessaliaStand, 2, 20*time.Second)
		return false
	}
	npc, ok := findNpcByNameOp(d, freshstart.Thessalia, freshstart.ThessaliaStand, 10, "")
	if !ok {
		d.logf("could not find %s nearby", freshstart.Thessalia)
		d.Delay(2 * time.Second)
		return false
	}
	npcType, _ := d.Config.Npc(npc.Type)
	op := cachecfg.OpIndex(npcType.Ops, "Trade")
	if op == 0 {
		op = 3
	}
	shop := actions.NewShop(d.Session)
	if err := actions.OpNpc(d.Session, op, npc.Index); err != nil {
		d.logf("trade %s: %v", freshstart.Thessalia, err)
		return false
	}
	if !waitFor(d, "Thessalia's shop to open", 4*time.Second, shop.IsOpen) {
		return false
	}
	defer shop.Close()
	var stockID int32 = -1
	for _, it := range shop.Stock() {
		for _, id := range ids {
			if it.ID == id {
				stockID = id
				break
			}
		}
		if stockID >= 0 {
			break
		}
	}
	if stockID < 0 {
		d.logf("%s has no %s in stock — waiting for a restock", freshstart.Thessalia, freshstart.PinkSkirt)
		d.Delay(5 * time.Second)
		return false
	}
	before := coinsHeld(d)
	if err := shop.Buy(stockID, 1); err != nil {
		d.logf("buy pink skirt: %v", err)
		return false
	}
	waitFor(d, "the pink skirt to appear in the pack", 3*time.Second, func() bool { return before-coinsHeld(d) > 0 })
	d.logf("bought a pink skirt (%d coins left)", coinsHeld(d))
	return false
}

// ---- pickpocketing for the boat fare ----

// Thief is the live pickpocketing state one FareThief-driven step needs beyond what the
// pure freshstart.FareThief tracks: this project's runner convention uses wall-clock
// Delayer-driven waits throughout rather than a live tick counter (there is no
// State().Tick() anywhere in this client, unlike the TS original's Game.tick()), so the
// stun timer here is tracked in wall-clock time instead of ticks — freshstart.StunTicks (8
// ticks, "about five seconds" per its own doc comment) converted at 600ms/tick, the same
// tick length every other runner package in this project already assumes (see e.g.
// mulerunner.Trade.Offer's own comment on Execution.delayTicks(1)).
type Thief struct {
	banana       bananaFare
	Pockets      int
	StolenGP     int
	stunnedUntil time.Time
	resting      bool
	// restSince is when the current rest began; restOverUntil is the end of the window after a rest
	// that ran freshstart.RestMax out, during which the account works without resting.
	restSince, restOverUntil time.Time
	restModalSaid            bool
	// awayWaits counts consecutive PickpocketStep calls that took the FundWaitAway
	// branch — see that branch's own doc comment in PickpocketStep for why this exists.
	awayWaits int
	// walkStuck counts consecutive PickpocketStep calls whose FundWalk branch's own
	// WalkNear call returned false (never reached the fare spot) — see that branch's own
	// doc comment for why this needs to be tracked here rather than trusting WalkNear's
	// own internal STRANDED counter.
	walkStuck int
	// noCoinGain counts attempts since the coin total last went UP, and bestCoins is the
	// most this account has held during the current funding window.
	//
	// Every other counter here escalates on a specific failure shape: awayWaits on waiting
	// for a target, walkStuck on not reaching the spot, combatStuck on being dragged away
	// mid-attempt. None of them bound the case where nothing is going wrong per attempt and
	// the account simply never gets anywhere — combatStuck explicitly RESETS when the account
	// is still standing next to its target, because an in-place miss is ordinary thieving
	// variance and must not read as STRANDED.
	//
	// That reasoning is right per attempt and leaves the aggregate unbounded. Live on
	// 2026-09-20 two accounts ground in place for as long as they were watched: one logged
	// 183 "timed out waiting for the pickpocket attempt to resolve" in 17 minutes at Thieving
	// 3 holding 0 coins, a second 27 more. Both were standing still and missing, so every
	// existing counter kept resetting.
	//
	// The bound is on COINS rather than attempts or XP, because coins are the objective — and
	// because the live pair is the telling case: their Thieving XP rose (so they were landing
	// pockets) while their coins stayed at zero, which no XP-based check would ever catch.
	noCoinGain int
	bestCoins  int
	// combatStuck counts consecutive PickpocketStep calls that reached the actual
	// attempt (found a target within range, clicked it) but landed no pocket — see that
	// branch's own doc comment for the live gap this closes: unlike awayWaits/walkStuck,
	// this specific branch had no escalation at all, so an account whose every attempt
	// keeps getting cut short by a hostile nearby (EmergencyFlee yanking it away mid-wait,
	// live-found 2026-09-16 fail-teleported into the known punishment zone near
	// LocalRescueTile) just repeated "pickpocketing for the fare" -> interrupted ->
	// flee -> retry forever, with no operator-visible signal that it was never actually
	// making progress.
	combatStuck int
	// spotUnreachableUntil is set when the STRANDED escape walk to o.Spot came back
	// UNREACHABLE rather than merely slow, so the account stops re-attempting a journey the
	// planner has already said has no route.
	//
	// Live-found 2026-09-22 on a 110-account cohort. A hundred accounts were converted from
	// fishing, so they start in the west; Stage 1's spot is the Lumbridge courtyard, and the
	// planner answered "unreachable from here (best 615 tiles)" from Ardougne. Each account
	// then waited its 24 fruitless polls (2 minutes), walked at an impossible target, failed,
	// and repeated — forever. Remembering the refusal turns that into a rare re-probe and
	// lets the account get on with working where it stands, which PickpocketSpotFor's own
	// `away` branch already does correctly.
	spotUnreachableUntil time.Time
}

// NewThief starts unstunned and not resting.
func NewThief() *Thief { return &Thief{} }

const stunFor = time.Duration(freshstart.StunTicks) * 600 * time.Millisecond

// pickpocketFailed reports whether text carries the server's own answer that an attempt
// FAILED — ported from FareThief.noteChat's two substrings.
//
// One copy of the rule, because two callers now need the same question answered:
// NoteChat, which times the stun from it, and PickpocketStep's resolve wait, which uses
// it to stop waiting. Keeping them as one predicate is what stops the wait and the stun
// timer disagreeing about what a failure looks like.
func pickpocketFailed(text string) bool {
	l := strings.ToLower(text)
	return strings.Contains(l, "been stunned") || strings.Contains(l, "fail to pick")
}

// NoteChat feeds one chat line in — ported from FareThief.noteChat verbatim (same two
// substrings), so a failed attempt's own message is what times the stun, not a click's
// return value.
func (t *Thief) NoteChat(text string) {
	if pickpocketFailed(text) {
		t.stunnedUntil = time.Now().Add(stunFor)
	}
}

func (t *Thief) stunned() bool { return time.Now().Before(t.stunnedUntil) }

// HitpointsStat is UPDATE_STAT's Hitpoints index — the same literal
// barbarianrunner/fighterfood's own callers already rely on (see dashboardvis's
// cross-check against gamestate.HitpointsStat=3 in COLLAB.md).
const HitpointsStat = 3

func hpFraction(d *Deps) float64 {
	hp, hpOK := d.Session.State().HP()
	maxHP, maxOK := d.Session.State().MaxHP()
	if !hpOK || !maxOK || maxHP <= 0 {
		return 1
	}
	return float64(hp) / float64(maxHP)
}

// PickpocketOpts mirrors newAccount.ts's own PickpocketOpts.
type PickpocketOpts struct {
	Target    string
	Spot      tile.Tile
	RestBelow float64
	Need      int
	// Rescue, when set, is a way to obtain the shortfall in coins from somewhere OTHER than a
	// Man, tried once per STRANDED escalation of the "nobody to pickpocket here" branch below
	// and before the long escape walk that branch would otherwise make. It is handed the
	// shortfall (Need minus what the account holds) and reports whether coins actually
	// arrived. nil keeps the branch exactly as it was.
	//
	// A callback rather than a mule client of this package's own, because the dependency only
	// runs the other way: internal/scripts/donorrunner (the hand-out path, donorrunner.Await)
	// already imports THIS package for PickpocketStep, so this package cannot import it back.
	//
	// THE LIVE DEADLOCK THIS EXISTS FOR, 2026-09-22, 110-account ScarabSwarm cohort. An
	// account displaced onto the Kandarin landmass needs a 60-coin ship fare to get back, earns
	// coins by pickpocketing Men, and has no Man inside freshstart.PickpocketLeash:
	//
	//	18:05:37 [acct96] no Man nearby and short of the fare (0 coins, need 60) — waiting
	//	18:05:42 [acct96] STRANDED: waited 24 times with no Man ever found near 2766,3423,0
	//	18:06:05 [acct96] walk to 3222,3218,0: unreachable (unreachable from here (best 455 tiles))
	//	18:06:05 [acct96] could not reach the pickpocket spot at 3222,3218 — working where we stand
	//
	// The escape walk cannot work BECAUSE the account has no coins: webwalk prices the ship
	// hops home (internal/webwalk.Requirements.Met refuses an edge whose Currency the account
	// cannot pay), so a 0-coin account has no route at all and the walker correctly answers
	// "unreachable". Coins are therefore not just what the step is FOR, they are what unlocks
	// its own escape — and the fleet's mule was standing 43 tiles away holding 2.3M of them.
	Rescue func(need int) bool
}

func findNpcByNameOp(d *Deps, name string, near tile.Tile, radius int, requireOp string) (npc struct {
	Index, Type, X, Z, Level int
}, ok bool) {
	bestDist := radius + 1
	for _, n := range d.Session.State().NPCs() {
		npcType, found := d.Config.Npc(n.Type)
		if !found || !strings.EqualFold(npcType.Name, name) {
			continue
		}
		if requireOp != "" && cachecfg.OpIndex(npcType.Ops, requireOp) == 0 {
			continue
		}
		dist := tile.Chebyshev(near, tile.Tile{X: n.X, Z: n.Z, Level: n.Level})
		if dist <= radius && dist < bestDist {
			npc.Index, npc.Type, npc.X, npc.Z, npc.Level = n.Index, int(n.Type), n.X, n.Z, n.Level
			bestDist = dist
			ok = true
		}
	}
	return npc, ok
}

// pickpocketTargets gathers every visible NPC matching name into freshstart.NPC rows (with
// their live ops), for freshstart.ChooseTarget's own sort/leash/level filter — kept as a
// separate live-to-pure conversion step rather than reimplementing ChooseTarget's filter
// here, so the actual selection logic lives in exactly one, already-tested place.
func pickpocketTargets(d *Deps, name string) []freshstart.NPC {
	var out []freshstart.NPC
	here := currentTileOrZero(d)
	for _, n := range d.Session.State().NPCs() {
		npcType, found := d.Config.Npc(n.Type)
		if !found || !strings.EqualFold(npcType.Name, name) {
			continue
		}
		out = append(out, freshstart.NPC{
			Name: npcType.Name, Tile: tile.Tile{X: n.X, Z: n.Z, Level: n.Level},
			Distance: tile.Chebyshev(here, tile.Tile{X: n.X, Z: n.Z, Level: n.Level}),
			Ops:      npcType.Ops[:],
		})
	}
	return out
}

// PickpocketStep is one pass of FareThief.step: rest for health if needed, wait out a
// stun, otherwise pickpocket the nearest reachable target (or walk to/wait at the spot).
// Ported 1:1 from newAccount.ts's own FareThief.step, including the "away from the
// mainland — use whatever's nearest" fallback (freshstart.PickpocketSpotFor).
// strandedEscapeWalkTimeout bounds the cross-map "give up waiting here and walk to the
// known-good pickpocket spot" escapes below. Deliberately longer than the 120s
// walkNearTimeout every sibling runner package uses (alchrunner, fisherrunner,
// thieverrunner and ten others): those bound ordinary in-region hops, whereas these three
// calls are the longest walks in this codebase by construction — a last-resort escape that
// can legitimately span the map.
//
// Live-found 2026-09-18: crafter1 (2933,3319) and acct41 (2924,3304) each hit
// "STRANDED: waited 24 times with no Man ever found", planned a real route to Lumbridge
// 3222,3218 of cost 330 / 176 waypoints, then hit the old flat 30s cap having covered only
// ~50 tiles, and dropped straight back into the same wait-24-then-walk cycle — pinned at 0
// coins and 0 XP/hour indefinitely. 30s could never finish that route at roughly 0.6s/tile
// walking, so the escape could not work at all at real distances; it only ever appeared to
// on short ones.
const strandedEscapeWalkTimeout = 240 * time.Second

// sailDirectArrivalRadius is the arrival tolerance SailLeg's no-boat-needed short-circuit
// walks to, and sailNoOpArrivalDelay is what that branch costs when the account is already
// inside it and the walk is therefore a no-op. See the branch's own comment for the live
// incident: one account drove it ~14,800 times a second at 0 XP/hour and wrote a 6.7 GB log.
//
// 250ms is chosen to be shorter than a game tick (600ms), so a caller in a legitimate
// poll loop is not slowed in any way it could notice, while still capping a runaway
// re-entry at 4/second instead of five figures.
const (
	sailDirectArrivalRadius = 3
	sailNoOpArrivalDelay    = 250 * time.Millisecond
)

// pickpocketResolveWait is how long the server gets to answer an attempt it has already begun,
// and pickpocketApproachPerTile how long the account is given to close each tile between where
// it clicked and the NPC it clicked on. See the resolve wait's own comment for the live
// measurement: a flat wait charged the approach to the timeout and called ordinary walking a
// fault. One tile per game tick is the conservative bound — a running account covers two, so
// this over-estimates rather than expiring early, and every real answer ends the wait anyway.
const (
	pickpocketResolveWait     = 2500 * time.Millisecond
	pickpocketApproachPerTile = 600 * time.Millisecond
)

// pickpocketResolveBudget is how long one attempt on a target `distance` tiles away is given
// before the wait calls it silence. A negative or zero distance is the adjacent case.
func pickpocketResolveBudget(distance int) time.Duration {
	if distance < 0 {
		distance = 0
	}
	return pickpocketResolveWait + time.Duration(distance)*pickpocketApproachPerTile
}

// rescueShortfall is what PickpocketOpts.Rescue is asked for: the coins still missing, or 0
// when there is nothing to ask for.
//
// Zero matters as much as the positive answer. Stage 1 of ScarabSwarm runs this step with
// Need 0 — it is grinding a Thieving LEVEL, not a fare (see scarabrunner.PickpocketOpts) — and
// an account in that state is not short of anything a mule can hand over, so it must never
// spend a trip to the post finding that out.
func rescueShortfall(need, held int) int {
	if need <= held {
		return 0
	}
	return need - held
}

// clearRestModal closes a main or chat modal left open while the account rests, and reports whether
// it did. Health regenerates on a normal player timer, which the engine runs only while no main or
// chat modal is open (Player.processTimers, canAccess), and a rest sends nothing that would close
// one: live on 5 Oct 2026 Howdy Folk rested 1h14m behind the Genie's last page and regenerated
// nothing. A modal a random event is still using is left to its handler.
func clearRestModal(d *Deps, said *bool) bool {
	main, _, chat := d.Session.State().Modals()
	if main == gamestate.NoModal && chat == gamestate.NoModal {
		return false
	}
	if randomevents.Busy(d.Session) {
		return false
	}
	if err := actions.CloseModal(d.Session); err != nil {
		d.logf("rest: closing the modal left open (main %d, chat %d): %v", main, chat, err)
		return false
	}
	if !*said {
		*said = true // once a rest: a modal the server keeps opening must not fill the log
		d.logf("rest: a modal was left open (main %d, chat %d) and health does not regenerate under one: closed it", main, chat)
	}
	return true
}

// restStep decides whether the account rests this pass and keeps the rest's bookkeeping: it logs a
// rest's start, closes a modal left open under it (clearRestModal), and gives a rest up once it has
// run freshstart.RestMax out (the log line reads the hitpoints and the modals), after which the
// account works for freshstart.RestCooldown. It reports whether to rest now.
func (t *Thief) restStep(d *Deps, restBelow float64, now time.Time) bool {
	hp := hpFraction(d)
	wasResting := t.resting
	var restFor time.Duration
	if wasResting {
		restFor = now.Sub(t.restSince)
	}
	rest, gaveUp := freshstart.ShouldRestBounded(hp, wasResting, restFor, restBelow, freshstart.ResumeAtHP, freshstart.RestMax)
	if rest && now.Before(t.restOverUntil) {
		rest = false // a rest was just given up: work for the cooldown
	}
	t.resting = rest
	if gaveUp {
		cur, _ := d.Session.State().HP()
		max, _ := d.Session.State().MaxHP()
		main, side, chat := d.Session.State().Modals()
		d.logf("rest: still at %d/%d hitpoints after %s of resting (modals main/side/chat %d/%d/%d) — working without resting for %s",
			cur, max, restFor.Round(time.Second), main, side, chat, freshstart.RestCooldown)
		t.restOverUntil = now.Add(freshstart.RestCooldown)
	}
	if rest {
		if !wasResting {
			t.restSince, t.restModalSaid = now, false
			d.logf("health at %.0f%% — resting until it regenerates", hp*100)
		}
		clearRestModal(d, &t.restModalSaid)
	}
	return rest
}

func PickpocketStep(d *Deps, t *Thief, o PickpocketOpts) {
	if t.restStep(d, o.RestBelow, time.Now()) {
		d.Delay(3 * time.Second)
		return
	}
	if t.stunned() {
		waitFor(d, "the stun to wear off", 9*time.Second, func() bool { return !t.stunned() })
		return
	}
	here := currentTile(d)
	spot, away := freshstart.PickpocketSpotFor(here, o.Spot)
	targets := pickpocketTargets(d, o.Target)
	idx := freshstart.ChooseTarget(targets, o.Target, spot)
	// A mainland fare spot cannot be reached from Karamja without paying another
	// fare. Earn the missing coins locally rather than escalating into that walk.
	if needsBananaFare(here, coinsHeld(d), o.Need, idx, t.banana.active) {
		bananaFareStep(d, &t.banana, o.Need)
		return
	}
	t.banana = bananaFare{}
	if idx == -1 {
		action := freshstart.NoTargetAction(away, gapTo(d, spot))
		if action != freshstart.FundWaitAway {
			t.awayWaits = 0
		}
		switch action {
		case freshstart.FundWaitAway:
			// PickpocketSpotFor's own "away" classification (>150 tiles from Lumbridge)
			// assumes waiting passively for a wandering Man to turn up nearby is
			// reasonable — true for the case it was built for (e.g. briefly on Karamja
			// mid-boat-leg), false for a genuinely displaced/lost account sitting
			// somewhere with no Men at all (an NPC-sparse wilderness/dungeon area, e.g.
			// a fail-teleport or death-respawn landing spot other than Lumbridge itself)
			// — that account waits here literally forever, with zero chance of ever
			// finding a target, and nothing before this counter ever noticed. Live-found
			// 2026-09-15, a 200-account soak: several accounts sitting at z=3417/3473
			// (nowhere near Lumbridge or any known Man spawn) logging "no Man nearby —
			// waiting" every 5s indefinitely. Escalate after ~2 minutes of this exact
			// same fruitless wait: same shape as walkNearCore's own STRANDED detection,
			// just for a step that has no walk of its own to give up on.
			t.awayWaits++
			const maxAwayWaits = 24 // 24 * 5s = 2 minutes
			if t.awayWaits >= maxAwayWaits {
				// Walk to the known-good spot instead of just logging and waiting another
				// 30s forever — live-found 2026-09-16, this codebase's first real
				// production run (account owner's own catch): passive waiting here assumed
				// a wandering Man would eventually pass by, but an account that's
				// genuinely landed somewhere sparse (a specific room/level with no NPC
				// traffic, not just "far from Lumbridge") can wait indefinitely with zero
				// chance of ever finding one — 2 minutes of confirmed failure is strong
				// evidence passive waiting isn't working, not a reason to keep repeating
				// the identical failed strategy. o.Spot (the same known-good tile the
				// FundWalk case below walks to) is a real, reachable location with
				// reliable targets regardless of how far away it is — distance alone was
				// only ever a reason to prefer waiting when waiting looked likely to work,
				// which this counter has now disproven for this specific case.
				//
				// Real, live-found bug this closes (2026-09-16, confirmed independently at
				// TWO unrelated locations — Port Sarim's ship deck, account "acct", and
				// ordinary open farmland near 3069,3304,0, accounts "acct35"/"acct9"):
				// this used to walk toward the local `spot` variable, not o.Spot. `spot`
				// here is PickpocketSpotFor's return value, and this whole branch only
				// ever runs when action==FundWaitAway, which NoTargetAction only returns
				// when away==true — and PickpocketSpotFor's own doc comment confirms
				// `away` deliberately makes it return `*here` (the account's OWN current
				// tile), not o.Spot, in exactly that case ("the account works where it
				// stands instead"). So this call was really WalkNear(d, hereSnapshot, 3,
				// 30s): since the account hadn't moved, Chebyshev(now, hereSnapshot) was
				// always 0, walkNearCore's own very first check ("already within radius")
				// returned true INSTANTLY — no FindPath ever attempted, confirmed live by
				// the total absence of any "[diag] FindPath"/"no path" line between the
				// STRANDED line and the very next "short of the fare" one — resetting
				// awayWaits and returning without the account moving a single tile, so
				// the exact same wait-24-then-instantly-reset cycle just repeated forever.
				// Using o.Spot directly (bypassing the away-aware local `spot`, which is
				// correct for ChooseTarget's own "look near wherever I am" search above but
				// wrong for this branch's explicit "give up waiting, walk to the known-good
				// spot" purpose) actually issues the walk this branch has always claimed to.
				d.logf("STRANDED: waited %d times with no %s ever found near %d,%d,%d (>150 tiles from Lumbridge) — walking to the known pickpocket spot instead of waiting", t.awayWaits, o.Target, here.X, here.Z, here.Level)
				// Live 3 Oct 14:30: bs, acct12 and Send Dono waited here at 3206,9891 for hours, in
				// no strand site; this lets the NoWayHomeOnly dungeon strip hold them.
				randomevents.NoteNoWayHome(d.Session)
				if LocalRescueTeleport {
					if rescueTeleport(d, *here) {
						t.awayWaits = 0
						d.Delay(1 * time.Second)
						return
					}
					d.logf("rescue ::tele failed or didn't land")
				}
				// Reset BEFORE the escalated walk, not only on its success — the same
				// discipline the sibling FundWalk branch just below already follows (see its
				// own "t.walkStuck = 0" placed right before its own escalated WalkNear call).
				// Real bug this closes, confirmed by direct code reading rather than a
				// defensive guess: leaving awayWaits pinned at/above maxAwayWaits on a FAILED
				// WalkNear meant every subsequent PickpocketStep call re-entered this exact
				// branch again (the outer "action != FundWaitAway" reset above never fires
				// while action keeps resolving to FundWaitAway), re-issuing the IDENTICAL
				// WalkNear(d, o.Spot, ...) call forever with nothing about the account's
				// situation having changed. This matches, line for line, the Barbarian Outpost
				// Stile-area incident this session was asked to investigate (see
				// docs/script_parity_status.md's BarbarianAgility "Remaining" section): "STRANDED:
				// no path to 3222,3218,0 has succeeded in 6 attempts in a row" repeating for the
				// rest of travelViaFreshStartTo's own 20-minute budget, with zero variation
				// between cycles — this is the exact call path (o.Spot is
				// freshstart.PickpocketSpot, the Lumbridge courtyard at 3222,3218,0) that
				// produces that log line. Resetting here (whether the walk
				// that follows succeeds or not) lets a failed escalation fall back through the
				// ordinary wait/ChooseTarget cycle for another maxAwayWaits*5s window instead of
				// busy-retrying the same failing walk every single tick — it can still escalate
				// and try again later if the account is genuinely stuck, just not in a tight
				// loop indistinguishable from the anti-pattern this file's other STRANDED
				// branches were already fixed to avoid.
				t.awayWaits = 0
				// ASK FOR THE COINS BEFORE WALKING FOR THEM. See PickpocketOpts.Rescue for
				// the live deadlock and for why this comes first: the escape walk below is
				// itself priced in coins, so for an account short of the fare it is the
				// less likely of the two to work, and it costs up to
				// strandedEscapeWalkTimeout to find that out. Held to the same discipline
				// as every other branch here — one attempt per escalation, no retry loop,
				// and a false answer falls straight through to exactly what happened before.
				if o.Rescue != nil {
					if short := rescueShortfall(o.Need, coinsHeld(d)); short > 0 && o.Rescue(short) {
						return
					}
				}
				if time.Now().Before(t.spotUnreachableUntil) {
					// The planner has already said there is no route from here. Wait local
					// rather than walk at it again; a wandering target may still turn up, and
					// the account re-probes once the window lapses.
					delayWatchingCombat(d, 30*time.Second)
					return
				}
				if walkWithWalker(d, o.Spot, 3, strandedEscapeWalkTimeout) {
					return
				}
				// Back off on ANY failed escape walk, not only one the raw collision pack
				// also calls unreachable.
				//
				// Asking the pack was wrong: it does not know about the danger zones the
				// WALKER refuses to cross (the White Wolf dens above all), so for an account
				// whose only blocked route is a danger zone the pack answers "reachable", the
				// memory is never set, and the escape is retried forever. Measured live
				// 2026-09-22: the memory fired 7 times while Acct220 alone made 107 attempts
				// at the same spot. The walker's verdict is the operative one, and it already
				// said no.
				//
				// Backing off on a transient failure costs 15 minutes of escape attempts and
				// nothing else: the account works where it stands in the meantime, which is
				// what PickpocketSpotFor's own `away` branch wants it to do anyway.
				t.spotUnreachableUntil = time.Now().Add(spotUnreachableFor)
				d.logf("could not reach the pickpocket spot at %d,%d — working where we stand and re-checking in %s",
					o.Spot.X, o.Spot.Z, spotUnreachableFor)
				delayWatchingCombat(d, 30*time.Second)
				return
			}
			// Report the coins WITHOUT claiming a shortfall that may not exist. This line
			// used to open "short of the fare (%d coins, need %d)" unconditionally, and on
			// the live fleet 2026-09-22 it printed "short of the fare (102 coins, need 60)"
			// — self-contradictory, and it sent a reader hunting for a coin bug that was not
			// there. The account was pickpocketing for a Thieving LEVEL (ScarabSwarm's
			// stage 1 target), not for a fare it had already covered; o.Need is only ever
			// the fare, so it says nothing about why this step is still running. What is
			// actually true here, and all that is, is that there is no target in sight.
			if coins := coinsHeld(d); coins < o.Need {
				d.logf("no %s nearby and short of the fare (%d coins, need %d) — waiting", o.Target, coins, o.Need)
			} else {
				d.logf("no %s nearby (%d coins, fare needs %d) — waiting", o.Target, coins, o.Need)
			}
			d.Delay(5 * time.Second)
		case freshstart.FundWalk:
			// WalkNear's own internal STRANDED counter is scoped to a single call and
			// never survives to the next one — fine for a caller that makes one
			// long-lived call per journey (SailLeg), wrong for this one, which calls
			// WalkNear fresh every single PickpocketStep invocation with a short 20s
			// timeout. Live-found 2026-09-15 (lc2b_196, stuck in a maze-reward room whose
			// one door is a wall-shaped loc aliasing between two adjacent tiles — see
			// tryOpenDoor's own doc comment): each 20s call only ever got through one or
			// two stall-recovery cycles before its own deadline ran out, so WalkNear kept
			// returning false having never come close to its own 6-in-a-row threshold,
			// and PickpocketStep just called it fresh again next tick forever, with no
			// escalation at this level either. Track "how many calls in a row made no
			// progress" here instead, the same shape as awayWaits just above.
			if WalkNear(d, spot, 3, 20*time.Second) {
				t.walkStuck = 0
			} else {
				t.walkStuck++
				const maxWalkStuck = 8 // roughly 8 * 20s = ~2.5min across repeated calls
				if t.walkStuck >= maxWalkStuck {
					d.logf("STRANDED: %d consecutive walks toward %d,%d,%d (the fare spot) never arrived — walking to the known pickpocket spot instead of retrying the same route", t.walkStuck, spot.X, spot.Z, spot.Level)
					if LocalRescueTeleport {
						if here := currentTile(d); here != nil && rescueTeleport(d, *here) {
							t.walkStuck = 0
							d.Delay(1 * time.Second)
							return
						}
						d.logf("rescue ::tele failed or didn't land")
					}
					// Live-found 2026-09-16 (account "acct", same production run as the
					// awayWaits/o.Spot fix and the combatStuck fix earlier tonight): this was
					// the THIRD instance of the same anti-pattern in this file — reset the
					// counter and delay 30s, betting the identical WalkNear(d, spot, ...) call
					// would somehow succeed next time with nothing about the account's
					// situation having changed. o.Spot is the same known-good, reachable
					// location the sibling FundWaitAway/combatStuck branches already trust as
					// a real recovery target — moving there is an actual attempt to get
					// unstuck, not just a hopeful pause. `spot` itself is deliberately NOT
					// reused here, for the same reason FundWaitAway's own fix doesn't reuse
					// its away-aware local `spot`: if `spot` were reachable, WalkNear(d, spot,
					// ...) above would already have succeeded.
					t.walkStuck = 0
					if walkWithWalker(d, o.Spot, 3, strandedEscapeWalkTimeout) {
						return
					}
					delayWatchingCombat(d, 30*time.Second)
				}
			}
		default:
			d.Delay(2 * time.Second)
		}
		return
	}
	t.awayWaits = 0
	target := targets[idx]
	if target.Distance > 8 {
		t.combatStuck = 0
		WalkNear(d, target.Tile, 1, 15*time.Second)
		return
	}
	// Both failure paths below were found live tonight producing an instant busy-loop
	// (dozens of identical "pickpocketing for the fare" log lines within one second): a
	// target found by the broader pickpocketTargets scan can still miss this stricter
	// re-resolution (findNpcByNameOp's own tighter radius/op match), and neither failure
	// case had a delay before returning — matching the same "every retry loop needs a
	// real delay" class of bug fixed elsewhere in barbarianrunner/freshstartrunner tonight.
	npc, ok := findNpcByNameOp(d, o.Target, target.Tile, 3, freshstart.PickpocketOp)
	if !ok {
		d.Delay(1 * time.Second)
		return
	}
	npcType, _ := d.Config.Npc(npc.Type)
	op := cachecfg.OpIndex(npcType.Ops, freshstart.PickpocketOp)
	if op == 0 {
		d.Delay(1 * time.Second)
		return
	}
	xpBefore := xpOf(d, ThievingStat)
	coinsBefore := coinsHeld(d)
	var packBefore loot.Counts
	if d.Loot != nil {
		packBefore = loot.PackCounts(d.Session.State().Inventory(playerPack))
	}
	clickAt := time.Now()
	if err := actions.OpNpc(d.Session, op, npc.Index); err != nil {
		d.logf("Pickpocket %s: %v", o.Target, err)
		d.Delay(400 * time.Millisecond)
		return
	}
	// The wait ends on any of the three answers the server can give, and the third one is
	// why this reads the chat rather than only the two state checks it used to.
	//
	// A FAILED attempt gains no Thieving xp, and t.stunned() cannot be true yet — the stun
	// is recorded by the NoteChat call BELOW, after this wait has already returned. So the
	// old predicate had no way at all to observe a failure, and every miss ran the full
	// 2500ms and logged "timed out waiting for the pickpocket attempt to resolve". That
	// line was never an error: it was the ordinary failure path wearing a fault's name,
	// which is why it appears in its hundreds (183 in 17 minutes at Thieving 3, recorded
	// in pickpocket_progress_test.go) and why it was read as a stall when it was not one.
	//
	// Live-confirmed on production 2026-09-22, the first ScarabSwarm run on the live fleet:
	// Acct226 at Thieving 1 logged 13 of these against 20 landed pockets in 100 seconds.
	// At a low Thieving level most attempts fail, so most attempts were paying the full
	// timeout — the failure is known from the server's own line well inside it.
	//
	// ...and the budget now covers the walk the SERVER makes before the attempt runs at all,
	// which is the second path that was still paying the timeout in full.
	//
	// ChooseTarget accepts anything inside freshstart.PickpocketLeash and only the branch
	// above (Distance > 8) walks there first, so an ordinary click is aimed at an NPC up to
	// EIGHT tiles away. The engine then routes the account to it and runs [opnpc,...] on
	// arrival — content 289's own ~pick_pocket does not say its first word ("You attempt to
	// pick the <$pocket>.") until then — so at one tile per 600ms tick, a click at 5 tiles
	// could not possibly resolve inside a flat 2500ms however well everything worked.
	//
	// Live-confirmed on the fleet 2026-09-22 (run/logs/fleet-20260922-101543): acct60 logged
	// 562 of these and NOTHING else, having gained 136 Thieving xp — about 17 landed pockets
	// against 562 "timeouts", which no success rate explains. It then tripped the sibling
	// combatStuck branch, whose own test is "the account is more than 3 tiles from the target
	// it clicked" — true of an account that is simply still WALKING there:
	//
	//	18:14:15 [acct60] STRANDED: 8 consecutive pickpocket attempts on Man near 2815,3445,0
	//	         were interrupted before resolving (something nearby keeps fleeing this account
	//	         away mid-attempt) — walking to the known pickpocket spot instead
	//	18:14:29 [acct60] walk to 3222,3218,0: unreachable (best 410 tiles)
	//
	// Nothing was fleeing it. Paying for the approach removes the false timeout AND the false
	// STRANDED escalation it fed, and costs nothing when the attempt does resolve: every one
	// of the three real answers still ends the wait the moment it arrives.
	waited := fmt.Sprintf("the pickpocket attempt to resolve (%s was %d tile(s) away when clicked)", o.Target, target.Distance)
	waitFor(d, waited, pickpocketResolveBudget(target.Distance), func() bool {
		if xpOf(d, ThievingStat) > xpBefore || t.stunned() {
			return true
		}
		return d.Chat != nil && pickpocketFailed(d.Chat.Since(clickAt))
	})
	if d.Loot != nil && xpOf(d, ThievingStat) > xpBefore {
		RecordPickpocketLoot(d, npc.Type, npcType.Name, packBefore)
	}
	// Feed the server's own reply back into t so a real "You've been stunned!"/"You fail
	// to pick..." line actually sets t.stunned() for the NEXT call — found live tonight
	// (see COLLAB.md): without this, a real stun (confirmed live: "You attempt to pick
	// the man's pocket. You fail to pick the man's pocket. You've been stunned!") was
	// never recorded anywhere, so every subsequent call re-clicked immediately into the
	// same stun window instead of taking this function's own `if t.stunned()` branch
	// above — the server silently drops a pickpocket attempt made while already stunned
	// (no chat, no xp), so a Thief that never learns it's stunned looks identical to one
	// that is failing every single roll forever, when it is really just hammering a
	// locked-out character. NoteChat is a plain substring match (see its own doc
	// comment), so this is safe to call on every attempt, successful or not.
	t.NoteChat(d.Chat.Since(clickAt))
	// combatStuck: only counts an attempt as "externally interrupted" (as opposed to an
	// ordinary missed roll, which leaves the character standing right where it clicked)
	// when the tile has moved away from the target since the click — an in-place miss is
	// normal thieving variance and must not trip this, or a run of ordinary bad luck at a
	// low Thieving level would false-positive as STRANDED. Live-found 2026-09-16 (a
	// no-cheats soak, "whirltest_24"): fail-teleported into the known punishment zone near
	// LocalRescueTile, every pickpocket attempt kept getting cut short by a hostile nearby
	// (EmergencyFlee walking it away mid-wait), and unlike awayWaits/walkStuck this branch
	// had no escalation at all — the account just repeated "pickpocketing for the fare" ->
	// interrupted -> flee -> retry for minutes with zero net progress and no operator-
	// visible signal.
	if xpOf(d, ThievingStat) == xpBefore {
		if here := currentTile(d); here == nil || tile.Chebyshev(*here, target.Tile) > 3 {
			t.combatStuck++
			const maxCombatStuck = 8 // roughly the same "~2.5min across repeated calls" budget as maxWalkStuck
			if t.combatStuck >= maxCombatStuck {
				d.logf("STRANDED: %d consecutive pickpocket attempts on %s near %d,%d,%d were interrupted before resolving (something nearby keeps fleeing this account away mid-attempt) — walking to the known pickpocket spot instead of retrying in place", t.combatStuck, o.Target, target.Tile.X, target.Tile.Z, target.Tile.Level)
				if LocalRescueTeleport {
					if here := currentTile(d); here != nil && rescueTeleport(d, *here) {
						t.combatStuck = 0
						d.Delay(1 * time.Second)
						return
					}
					d.logf("rescue ::tele failed or didn't land")
				}
				// Live-found 2026-09-16 (account "acct", same production run as the
				// FundWaitAway fix above): this branch used to just reset the counter and
				// delay 30s, betting the interrupting hostile/event would clear on its own —
				// no different from the awayWaits bug this same session already fixed for
				// the sibling wait-for-a-Man case, and it has the exact same failure mode:
				// if whatever's interrupting the attempt (a nearby random event, a fleeing
				// NPC) doesn't clear, the account just repeats "interrupted -> wait 30s ->
				// interrupted" forever with zero net progress. o.Spot is the same
				// known-good, reachable pickpocket location the FundWaitAway branch already
				// trusts — moving away from whatever's causing the interruption is a real
				// recovery attempt, not just a hopeful pause.
				t.combatStuck = 0
				if walkWithWalker(d, o.Spot, 3, strandedEscapeWalkTimeout) {
					return
				}
				d.Delay(30 * time.Second)
				return
			}
		} else {
			t.combatStuck = 0
		}
	} else {
		t.combatStuck = 0
	}
	if xpOf(d, ThievingStat) > xpBefore {
		t.Pockets++
		gained := coinsHeld(d) - coinsBefore
		if gained > 0 {
			t.StolenGP += gained
		}
		if coinsHeld(d) >= o.Need {
			d.logf("funded: %d coins from %d pockets", coinsHeld(d), t.Pockets)
		}
	}
	noteCoinProgress(d, t, o)
}

// maxNoCoinGain is how many attempts may pass with no increase in coins before the account
// stops grinding and says so.
//
// Deliberately generous, so ordinary bad luck never trips it. A Man gives a few coins and a
// low-Thieving account lands maybe a third of its attempts, so funding a 30-coin fare is
// normally a couple of dozen attempts; at roughly 3 seconds each, 60 is about three minutes
// of getting nowhere. The accounts this was written for were seen doing 183 and counting.
const maxNoCoinGain = 60

// coinProgress records one attempt's coin reading and reports whether the account has now gone
// maxNoCoinGain attempts without its coins rising — the STRANDED condition.
//
// The window is "since coins last went up OR last went down". A DROP starts a new funding window:
// nothing in this step ever spends coins, so a drop is always something outside it — a fare paid
// at the dock, a death (the coins stay on the floor where the account died), a bank deposit, a
// mule hand-off — and the account is now funding from its new, lower balance. Until 2026-09-25
// the best-ever mark survived all of those, so an account that funded a fare to 62, paid it, died,
// and was funding the next one from 0 could never beat 62 before the fare was covered, and was
// called STRANDED after 60 attempts while its coins were visibly rising: 32 such lines on the live
// fleet between 20:16 and 22:40, 30 of them holding more than the 0-2 coins a death leaves
// ("holding 57 of the 60 needed, 119 pockets landed"). A false STRANDED hides real strandings in
// the same log, and each one walks the account to the spot or waits out 60s.
func (t *Thief) coinProgress(coins int) (stranded bool) {
	if coins != t.bestCoins {
		// Up: progress. Down: a new window from the new balance (see above). Either way the
		// count starts again.
		t.bestCoins = coins
		t.noCoinGain = 0
		return false
	}
	t.noCoinGain++
	return t.noCoinGain >= maxNoCoinGain
}

// noteCoinProgress bounds the whole funding attempt, as opposed to any single try.
//
// It escalates the same way the neighbouring branches do — walk to the known-good spot, and
// only fall back to a wait if even that fails — because the lesson those branches each
// recorded is that pausing and hoping is not a recovery. The difference here is that it also
// says plainly what the account has been doing, since an operator looking at "timed out
// waiting for the pickpocket attempt to resolve" repeated 183 times has no way to tell an
// unlucky run from a wedged one.
func noteCoinProgress(d *Deps, t *Thief, o PickpocketOpts) {
	coins := coinsHeld(d)
	if !t.coinProgress(coins) {
		return
	}
	lvl := 0
	if skill, ok := d.Session.State().Skill(ThievingStat); ok {
		lvl = skill.Level
	}
	d.logf("STRANDED: %d pickpocket attempts on %s with no coin gained (Thieving %d, holding %d of the %d needed, %d pockets landed) — this is not paying for the fare, moving to the known pickpocket spot instead of grinding here",
		t.noCoinGain, o.Target, lvl, coins, o.Need, t.Pockets)
	t.noCoinGain = 0
	t.bestCoins = coins
	if walkWithWalker(d, o.Spot, 3, strandedEscapeWalkTimeout) {
		return
	}
	// The spot is unreachable too. A long wait here is not a fix, but it stops a wedged
	// account spending the rest of its run clicking every three seconds, and leaves the line
	// above in the log for whoever looks.
	d.Delay(60 * time.Second)
}

// ThievingStat is UPDATE_STAT's Thieving index (attack,defence,strength,hitpoints,ranged,
// prayer,magic,cooking,woodcutting,fletching,fishing,firemaking,crafting,smithing,mining,
// herblore,agility,thieving,...) — thieving is index 17, matching thieverplan's own use.
const ThievingStat = 17

func xpOf(d *Deps, statIdx int) int {
	skill, ok := d.Session.State().Skill(statIdx)
	if !ok {
		return 0
	}
	return skill.XP
}

func gapTo(d *Deps, t tile.Tile) int {
	here := currentTile(d)
	if here == nil {
		return 1 << 30
	}
	return tile.Chebyshev(*here, t)
}

// ---- the boats ----

// SailProgress tracks SailLeg's own repeated failures against the exact same leg+target from
// the exact same tile so a genuinely stranded account escalates instead of repeating one
// identical log line forever — the specific complaint from a real live incident tonight
// (casket, sealed in a building northeast of Yanille, logging "boat leg: ... -> destination"
// then "no path from 2651,3165,0 to 2809,3441,0: unreachable" on an unbroken loop). WalkNear's
// own tryChangeLevel/tryOpenDoor fixes close the most common real cause of this (a level change
// or a closed door genuinely blocking the way), but neither is guaranteed to apply to every
// possible stranding, so this is the backstop: when nothing WalkNear tries helps, at least make
// that fact loud and infrequent rather than silent and constant — this project's own "never
// silently loop forever" standard (see WalkNear's own busy-loop fix). Owned by the caller across
// repeated SailLeg calls (the same shape as Thief/FareThief); the zero value is ready to use.
type SailProgress struct {
	leg      freshstart.Leg
	target   tile.Tile
	lastTile tile.Tile
	fails    int
	// board is BoardShip's own memory across calls: the dialogue being driven, the coins held
	// when its Talk-to was sent, and how many boarding attempts in a row have failed. See
	// boardship_wait.go.
	board boardAttempt
	// fallbackFails counts webwalk-route attempts in a row that did not get across either;
	// it sizes the back-off before boarding is tried again. See sailFallback.
	fallbackFails int
	// noBoardingAt is when the "nothing here to board" line was last written, so a dock full of
	// accounts waiting for one wandering NPC does not write it every few seconds each.
	noBoardingAt time.Time
	// unboardedAt is the same idea for the mid-boarding return below, which until 2026-09-22
	// wrote nothing at all. See noteUnboarded.
	unboardedAt time.Time
}

// noteUnboarded reports whether the "BoardShip has not finished" line should be written this
// time, on the same once-a-minute-per-account budget as noteNoBoarding.
//
// THE SILENCE THIS ENDS, measured live 2026-09-22. Eight ProgressiveFisher accounts sat at
// 2780,3214 on Karamja for the entire run at 0 xp. Each logged "phase: sailing to Catherby"
// ONCE and then, across 57 minutes, produced not one further line of its own -- acct80's
// whole run is six startup lines and 178 copies of the Guardian's unrelated drift message.
// They were returning through SailLeg's `if !boarded { return false }`, which logs nothing,
// so an account stuck at the boarding step was indistinguishable from one that had never
// tried. The tell that it was this branch and not the two beside it: "boat leg:" is written
// immediately after, and none of the eight ever wrote it, while acct92 -- genuinely trying
// from a sealed pocket 22 tiles away -- wrote it 32 times.
//
// Silence is the thing that cost the time here, not the stall: the account was doing nothing
// visible for half an hour and nothing in the fleet log said which of a dozen steps it was
// stuck on. Rate-limited rather than unconditional because the opposite mistake is already on
// record in this same function -- 8,367 "boat leg" lines in one afternoon (2026-09-22).
func (p *SailProgress) noteUnboarded() bool {
	if p == nil {
		return true
	}
	if time.Since(p.unboardedAt) < time.Minute {
		return false
	}
	p.unboardedAt = time.Now()
	return true
}

// NewSailProgress returns a ready-to-use SailProgress.
func NewSailProgress() *SailProgress { return &SailProgress{} }

// sailStuckEvery is how many consecutive identical failures (same leg, same target, the
// account's own tile unchanged since the last failure) elapse between STRANDED escalations —
// frequent enough that an operator watching the log notices well before a real 13-account fleet
// shift ends, rare enough that it reads as a distinct alarm rather than routine retry noise.
const sailStuckEvery = 5

// sailCompleteTimeout bounds how long SailLeg waits for the server's own set_sail teleport to
// actually land after BoardShip's dialogue closes. Was 5s, raised to 20s (2026-09-16, lc2b_24 —
// see the wait's own call site doc comment for that incident). Raised again here to 35s
// (2026-09-18): a full-fleet ~266-account soak (run/fleet.log) still logged 15 "timed out
// waiting for the sail to complete" hits across roughly 65 boat-leg attempts in under 90 minutes,
// spread across many different accounts/legs (not one repeatedly-stuck account), and every single
// occurrence checked timed out at EXACTLY the prior 20s boundary (e.g. "Spare 24" 16:58:48,
// "big net" 17:13:35->17:14:00, "acct18"/"acct46"/"crafter1"/"spare28" likewise) — consistent
// with the same under-concurrent-load server latency this constant was already raised once for,
// now simply exceeding the new fleet size's worst case too. Each occurrence self-recovered within
// a few retries (confirmed live for "Spare 24": stuck ~16:58-17:04, sailing again normally
// after), so this is a real but non-fatal tax, not a stranding on its own — raising the budget
// directly cuts how often an account pays it under this fleet's real load.
const sailCompleteTimeout = 35 * time.Second

// sailNoBoardingWait is how long an account holds station at a departure dock whose boarding NPC
// is not currently in reach. Long enough that a wandering NPC has time to come back and that a
// hundred accounts are not each re-scanning every tick, short enough that the ship is not missed
// by much. Deliberately shorter than sailCompleteTimeout: this is a look-again interval, not a
// wait for something already in flight.
const sailNoBoardingWait = 8 * time.Second

// noteNoBoarding reports whether the "nothing here to board" line should be written this time.
// A nil SailProgress (stuck-detection off) always writes, matching every other log in SailLeg.
func (p *SailProgress) noteNoBoarding() bool {
	if p == nil {
		return true
	}
	if time.Since(p.noBoardingAt) < time.Minute {
		return false
	}
	p.noBoardingAt = time.Now()
	return true
}

// note records one SailLeg outcome and reports whether this is the call that should escalate.
// A success, a different leg, a different target, or the account's own tile having actually
// moved since the last failure all reset the streak to zero, since any of those mean real
// progress happened even if this particular attempt did not finish — only the exact same
// no-progress failure repeating counts toward getting stuck.
func (p *SailProgress) note(ok bool, leg freshstart.Leg, target tile.Tile, here *tile.Tile) (stuck bool) {
	if p == nil {
		return false
	}
	sameSpot := here != nil && *here == p.lastTile
	if here != nil {
		p.lastTile = *here
	}
	if ok || leg != p.leg || target != p.target || !sameSpot {
		p.leg, p.target, p.fails = leg, target, 0
		return false
	}
	p.fails++
	return p.fails%sailStuckEvery == 0
}

// sailorNPCNames are the Port Sarim sailors whose category=sailor handler offers the
// Karamja trip (content/scripts/areas/area_port_sarim/scripts/sailors.rs2's
// [opnpc1,_sailor]/[label,karamja_sailor_dialogue]): one dialogue question, "Do you want
// to go to Karamja? / trip costs 30 coins", answered "Yes please."
var sailorNPCNames = []string{"Seaman Lorris", "Seaman Thresnor", "Captain Tobias"}

// customsOfficerName boards the Brimhaven <-> Ardougne (or Karamja <-> Port Sarim) ship,
// ported from content/scripts/areas/area_karamja/scripts/customs_officer.rs2's
// [opnpc1,customs_officer]: for a fresh account (no quest progress, no rum in the pack —
// the only path a brand-new character can actually take) the dialogue is a fixed 3-step
// line: "Can I journey on this ship?" -> "Search away, I have nothing to hide." (nothing
// to confiscate) -> "Ok." (pays the 30 coin boarding charge). Real branches exist for
// rum-smuggling/quest-progress accounts but no fresh character can reach them.
const customsOfficerName = "Customs officer"

// sailDialogueChoices returns which NPCs can board a given leg and, in order, the
// substring each successive Options() screen should be answered with. Both dialogue
// trees a fresh account can actually reach are linear — not a general dialogue solver.
func sailDialogueChoices(leg freshstart.Leg) (npcNames []string, choices []string) {
	if leg == freshstart.LegPier {
		return sailorNPCNames, []string{"yes please"}
	}
	return []string{customsOfficerName}, []string{"journey", "search away", "ok"}
}

// BoardShip drives one leg's real ship-boarding dialogue — the missing piece SailLeg's
// own doc comment used to flag: internal/pathfind has no transport-graph edge for these
// ships, so unlike the TS client's own web-walk (which treats boarding as an ordinary
// graph edge, silently), this client must drive the real NPC interaction explicitly, the
// same shape as freshstartrunner.SkipTutorial's own guide dialogue.
//
// done is true once there is nothing left to board here (either a boarding dialogue just
// finished — the account should be sailing — or no boarding NPC was found nearby at all,
// e.g. mid-walk on dry land); false while a dialogue is being driven or an NPC was just
// clicked, so the caller should not also try a plain walk this same tick.
//
// boarded distinguishes those two very different reasons for done, and it exists because
// SailLeg used to treat them as the same thing and then wait sailCompleteTimeout (35s) for
// a teleport that was never coming. Live 2026-09-22, the ScarabSwarm cohort
// (run/logs/fleet-20260922-101543-232-16204.log): 8,367 "boat leg: Brimhaven ship ->
// Ardougne -> destination" lines against 6,409 "timed out waiting for the sail to complete"
// — 77% — and accounts such as acct42 sat two tiles off freshstart.BrimhavenPier emitting
// that identical pair every 41 seconds for 25 minutes with nothing else in between, which is
// this function returning done with no NPC ever found. cmd/npcscan's own doc comment records
// the same 15-tile search coming up empty at that pier as far back as 2026-09-15. At 35
// seconds a turn that is about 62 hours of fleet time spent waiting for a boat nobody
// boarded.
// approachFailedAt throttles the "could not walk up to the boarding NPC" line to one a minute
// per account, the same budget noteNoBoarding and noteUnboarded use.
//
// Per SESSION rather than per SailProgress because BoardShip is not given the progress record.
// Keyed the same unbounded-but-harmless way internal/randomevents keeps its own per-session
// drift bookkeeping; an entry is a timestamp and a dead account simply stops being asked about.
var approachFailedAt sync.Map // actions.Session -> time.Time

// noteApproachFailed reports whether the line should be written this time. A Deps with no
// session always writes, matching every other log in this file.
func noteApproachFailed(d *Deps) bool {
	if d == nil || d.Session == nil {
		return true
	}
	now := time.Now()
	if last, ok := approachFailedAt.Load(d.Session); ok {
		if now.Sub(last.(time.Time)) < time.Minute {
			return false
		}
	}
	approachFailedAt.Store(d.Session, now)
	return true
}

// BoardShip is boardShip for a caller that keeps only a step counter between calls. It keeps
// no record of the Talk-to (neither the coins held then nor when it was sent), so it cannot
// read the fare off the coins or the "You pay" message and judges a closed dialogue on the
// ship interface alone. SailLeg keeps the full record and does not go through here; nothing
// else calls this outside the tests.
func BoardShip(d *Deps, leg freshstart.Leg, step *int) (done, boarded bool) {
	a := boardAttempt{step: *step}
	done, boarded = boardShip(d, leg, &a)
	*step = a.step
	return done, boarded
}

// boardShip is BoardShip with its memory across calls in a. See boardship_wait.go for why
// every click in here now waits for the server's answer before the next one.
func boardShip(d *Deps, leg freshstart.Leg, a *boardAttempt) (done, boarded bool) {
	step := &a.step
	dlg := actions.NewChatDialog(d.Session, d.Components)
	if len(dlg.Options()) > 0 {
		a.noteDialogue()
		_, choices := sailDialogueChoices(leg)
		// Past the scripted line, STOP driving rather than guessing.
		//
		// This used to fall back to wanting "yes" and, when that matched nothing, to
		// clicking the first option — then increment and do it again. Since *step advances
		// on a failure too, a dialogue that went off-script span forever: live on 2026-09-20
		// one account sat on Karamja emitting "no option matching \"yes\" — choosing the
		// first option" every 800ms indefinitely, clicking option 1 each time.
		//
		// Both real boarding trees are linear and short (see sailDialogueChoices), so
		// running past the end means this is not the dialogue we think it is — a quest or
		// rum-smuggling branch, or simply another npc. Guessing at an unknown dialogue by
		// clicking its first option is how an account ends up somewhere worse, so the honest
		// move is to leave it for the caller to re-plan.
		// DRIVE BY MATCHING, NOT BY POSITION. The tree is linear but it is not always the
		// SAME length, so a fixed step index desynchronises and then answers the wrong
		// screen.
		//
		// Content proves it (area_karamja/scripts/customs_officer.rs2, [label,customs_journey]):
		// an account the officer recognises — "Hey, I know you, you work at the plantation" —
		// SKIPS the search entirely and goes straight to the 30-coin charge. That is not a
		// rare branch here: it is every account that has done Luthas's banana job, which
		// freshstartrunner's own bananafare.go exists to do. For those accounts the second
		// screen is "Ok." / "Oh, I'll not bother then.", while the fixed script was still
		// answering "search away" — and, having advanced its index on the miss, it answered
		// the third screen with nothing left and gave up.
		//
		// Measured live 2026-09-22: 707 boat legs started against 66 gangplank crossings, with
		// 61 "no option matching \"search away\"" and 62 "ran past its scripted steps".
		//
		// Matching instead is robust to both shapes and cannot desynchronise, because it asks
		// the screen what it is offering rather than assuming. Every entry in choices is a
		// correct answer wherever it appears, so the first that matches is the right one.
		before := dialogueSignature(d, dlg)
		matched := ""
		for _, want := range choices {
			if err := dlg.ChooseOption(want); err == nil {
				matched = want
				break
			}
		}
		if matched == "" {
			// None of this leg's own answers is on screen. That means this is not the
			// dialogue we think it is (a quest or rum-smuggling branch, or another npc), and
			// guessing at an unknown dialogue by clicking its first option is how an account
			// ends up somewhere worse. Leave it for the caller to re-plan.
			d.logf("boarding dialogue offers none of this leg's answers %v — not guessing; leaving it to be re-planned", choices)
			// Only a dialogue our own Talk-to opened is a failed boarding. Any other one on
			// screen (an event's, another npc's) says nothing about this dock, and counting it
			// would hand the crossing to the fallback early.
			ours := !a.talkedAt.IsZero()
			a.end()
			if ours {
				a.fails++
			}
			// Nothing was paid for, so there is no sail on the way.
			return true, false
		}
		// *step is now only a loop bound, not a position: a dialogue that keeps offering
		// answers without ever finishing must not be driven forever. Since every answer now
		// waits for the next screen (below), it also counts screens honestly: it used to
		// count the same stale screen twice whenever the server took longer than 700ms to
		// answer, and ran into this bound mid-dialogue on the proxy routes (11 times live on
		// 2026-09-23), abandoning a dialogue that was still going.
		*step++
		if *step > len(choices)+2 {
			d.logf("boarding dialogue answered %d screens without boarding — leaving it to be re-planned", *step)
			// An answer may already have been accepted and paid for before this gave up, so
			// ask the evidence rather than assume either way. Live 2026-09-22 (acct66,
			// 15:45:03 -> 15:45:44) one of these did board, 41s after the last click.
			paid := awaitFarePaid(d, a)
			a.end()
			if !paid {
				a.fails++
			}
			return true, paid
		}
		// ONE ANSWER PER SCREEN. Wait for the server's next screen before looking again, so
		// a screen still on show only because its successor is on the wire is not answered
		// twice. See boardship_wait.go.
		awaitScreenChange(d, dlg, before, boardScreenWait)
		return false, false
	}
	if dlg.CanContinue() {
		a.noteDialogue()
		before := dialogueSignature(d, dlg)
		_ = dlg.Continue()
		awaitScreenChange(d, dlg, before, boardScreenWait)
		return false, false
	}
	if dlg.IsOpen() {
		d.Delay(400 * time.Millisecond)
		return false, false
	}
	if a.inDialogue() {
		// A dialogue we were driving has closed. That alone says nothing about a sail: it
		// ends the same way after "You pay 30 coins and board the ship." as after "I'm not
		// that sort of customs officer.", and treating the two alike is what the 35-second
		// sail wait was spent on 201 times in one live window. Ask whether the fare went.
		paid := awaitFarePaid(d, a)
		answered, coins := a.step, coinsHeld(d)
		a.end()
		if paid {
			a.fails = 0
			return true, true
		}
		a.fails++
		d.logf("the boarding dialogue closed without taking the fare (%d coins held, %d answers given, attempt %d in a row) — boarding again",
			coins, answered, a.fails)
		return false, false
	}
	if sailUnderWay(d) {
		// Paid and sailing: nothing to board, and nothing to click until the ship lands.
		d.Delay(600 * time.Millisecond)
		return false, false
	}
	npcNames, _ := sailDialogueChoices(leg)
	for _, name := range npcNames {
		npc, ok := findNpcByNameOp(d, name, currentTileOrZero(d), 15, "Talk-to")
		if !ok {
			continue
		}
		if leg == freshstart.LegDestination && name == customsOfficerName && npc.X >= freshstart.BrimhavenCustomsMaxX {
			// The Musa Point officer sails back to Port Sarim, not on to Ardougne.
			continue
		}
		npcTile := tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}
		if tile.Chebyshev(currentTileOrZero(d), npcTile) > 3 {
			// HONOUR THE WALK'S RESULT. This used to discard it and return "still
			// boarding" regardless, which made the failure unrecoverable: the function
			// can only reach its own `return true, false` below — the answer that tells
			// SailLeg to stage through the pier instead — by running OUT of candidate
			// NPCs, and returning here every time meant it never did. So an account that
			// could SEE a boarding NPC but could not walk up to it re-attempted the same
			// failing 20-second walk for ever, while the recovery it needed sat three
			// lines further down.
			//
			// Live 2026-09-22: eight ProgressiveFisher accounts stacked on the single
			// tile 2780,3214 on Karamja, all eight at 0 xp for a whole run, all eight
			// logging `still boarding the destination leg ... (step 0)` — step 0 because
			// this branch is the one path out of BoardShip that never touches it. The
			// cadence matched too: 178 log lines in 57 minutes, one per 19.2s, which is
			// this WalkNear's own 20-second timeout. The lab sails home from that exact
			// tile in 81 seconds with ONE account (cmd/labfleettest -mode mule-strand
			// -mule-strand-x 2780 -mule-strand-z 3214), so the tile is fine and the
			// standing hypothesis is that eight accounts on one tile cannot step past
			// each other — which is precisely when giving up on the NPC and walking to
			// the pier is the right move.
			//
			// THAT HYPOTHESIS WAS WRONG: the walk never ran at all. SailLeg calls BoardShip
			// holding randomevents.Lock, which marks the account's own Guardian in flight, and
			// WalkNear's first act on every pass is to defer while randomevents.Busy, which that
			// same in-flight flag makes true. So WalkNear sat out its whole 20 seconds waiting
			// on the lock its own caller held, sent no walk, and returned false: the 19.2-second
			// cadence above is exactly that. cmd/sailtest shows it on the lab engine (no MOVE
			// packet at all between the dock and "could not get within talking distance ... in
			// 20s", whenever the officer stood more than 3 tiles off). walkAdjacent is WalkNear
			// without the Guardian wait and the door recovery, which is what its own doc
			// comment says it is for: a short approach inside an action that holds the lock.
			if walkAdjacent(d, npcTile, 2, 20*time.Second) {
				return false, false
			}
			a.fails++
			if noteApproachFailed(d) {
				d.logf("could not get within talking distance of %s at %d,%d,%d in 20s — giving up on boarding here and staging through the pier instead",
					name, npc.X, npc.Z, npc.Level)
			}
			continue
		}
		npcType, _ := d.Config.Npc(npc.Type)
		op := cachecfg.OpIndex(npcType.Ops, "Talk-to")
		if op == 0 {
			op = 1
		}
		a.begin(coinsHeld(d), currentTileOrZero(d), time.Now())
		if err := actions.OpNpc(d.Session, op, npc.Index); err != nil {
			a.end()
			d.logf("Talk-to %s: %v", name, err)
			d.Delay(600 * time.Millisecond)
			return false, false
		}
		// ONE TALK-TO PER ATTEMPT. This used to wait 700ms and, seeing no dialogue yet, click
		// again. A second OPNPC kills the dialogue the first has just opened on the server
		// (OpNpcHandler -> clearPendingAction -> closeModal) and starts it over, and the
		// open-close-open that reaches the client a round trip later is what answered the
		// officer's option screen with the wrong line. See boardship_wait.go.
		opened, why := awaitBoardingDialogue(d, dlg, a.talkedAt, boardTalkWait)
		if opened {
			a.sawDialogue = true
			return false, false
		}
		a.end()
		a.fails++
		d.logf("Talk-to %s at %d,%d,%d did not open the boarding dialogue: %s (attempt %d in a row)",
			name, npc.X, npc.Z, npc.Level, why, a.fails)
		return false, false
	}
	// No boarding NPC nearby right now — not at a departure dock (e.g. mid-walk across
	// Karamja island toward Brimhaven, which needs no ship at all).
	return true, false
}

// SailLeg drives one leg of the account owner's own real boat route (freshstart.BoatLeg):
// Port Sarim -> Karamja -> Brimhaven pier, or Brimhaven -> Ardougne -> dest. Tries
// boarding first (BoardShip — real dialogue-driven NPC interaction; see its own doc
// comment for why that exists at all), and only falls back to a plain walk once there is
// no boarding NPC nearby. For the pier leg specifically, the walk target is
// freshstart.PortSarimDock rather than the leg's own far-side target
// (freshstart.BrimhavenPier) whenever the account is still on the Port Sarim side of the
// water — found live (2026-09-15): every fresh account was trying to pathfind straight to
// BrimhavenPier from as far away as Lumbridge and failing instantly with no route at all,
// since internal/pathfind has no edge across the water; it needs Port Sarim's dock as an
// explicit intermediate waypoint to reach in the first place, and BoardShip only finds
// the sailors once actually there. Once past the first ship (closer to BrimhavenPier than
// to PortSarimDock — i.e. already on Karamja) the walk target reverts to the leg's own
// target, matching what a plain same-landmass walk across Karamja needs.
// progress is optional (nil skips stuck-detection entirely) but every real caller should pass
// one it owns across calls — see SailProgress's own doc comment for why.
func SailLeg(d *Deps, dest tile.Tile, progress *SailProgress) bool {
	// freshstartrunner (this package) previously had no Guardian-awareness at all —
	// BoardShip below drives real dialogue (options/continue clicks) touching the same
	// account-wide modal state a concurrent Guardian handler (e.g. solveStrangeBox's own
	// puzzle interface) also reads and writes, with nothing to stop the two from
	// colliding. Same root cause as fisherrunner.BankFish's own fix this session (see
	// COLLAB.md's "Strange box" entry and BankFish's own doc comment) — and the same
	// upgrade from a one-shot check to a real held lock: a point-in-time Busy() check
	// here can pass a moment before Guardian's own poll picks up a freshly-spawned event,
	// same as BankFish's own first (insufficient) fix, proven via cmd/boxracetest.
	// randomevents.Lock is only held around the actual BoardShip call below — the
	// position/leg-lookup lines above touch no modal state and shouldn't block Guardian
	// from an unrelated urgent event. Falls back to a no-op for a session with no
	// registered Guardian, harmless for any caller that never built one.
	here := currentTile(d)
	if here == nil {
		d.Delay(1200 * time.Millisecond)
		return false
	}

	// Direct-reachability short-circuit — found during a staging audit following the
	// Draynor-staging fix just below (see its own doc comment): freshstart.TravelLeg's
	// `t.X < ArdougneSideMaxX` (2740) check assumes every valid destination sits west of
	// Ardougne — true for the crafter's own Seers bank (x=2725) this whole system was
	// designed against, but false for the fisher's own destination, Catherby's bank
	// (x=2809, EAST of that threshold). TravelLeg has no idea what `dest` even is (BoatLeg
	// calls it with `here` alone), so any position still classified LegPier purely by that
	// x-threshold — including large stretches of Catherby's own ~109,716-tile landmass
	// itself, confirmed via cmd/doordiag, whenever an account ends up there outside
	// DecidePhase's own DestinationRadius (a death or fail-teleport landing it away from
	// the exact bank tile, say) — falls straight into the same broken staging the Draynor
	// fix exists for, except now Draynor is ALSO confirmed unreachable from that same
	// landmass (cmd/doordiag: FindPath from Catherby's bank to Draynor fails outright,
	// "unreachable", the whole component never reaching it) — reproducing the identical
	// STRANDED loop one region over. Checking real pack connectivity to dest FIRST, before
	// ever trusting TravelLeg's geometry, closes this at its root: if a plain walk already
	// gets there, no boat is needed regardless of which side of x=2740 the account is on.
	if d.Pack != nil {
		if res := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level}); res.OK {
			// If we are ALREADY inside the arrival radius, WalkNear's own first check
			// (`Chebyshev(here, dest) <= radius` → return true) answers instantly, with no
			// delay and no packet sent, and this whole branch becomes a zero-cost "yes".
			// That is correct as an answer and catastrophic as a loop: any caller that
			// recomputes its phase from the result and calls straight back in spins at
			// whatever rate the CPU allows.
			//
			// Not hypothetical. Live 2026-09-18, production account "spare28" (ProgressiveFisher,
			// Fishing 55) sat two tiles from the Catherby bank and drove this branch
			// **398,648 times in 27 seconds** — ~14,800 iterations/second from one account —
			// at 0 XP/hour. It alone produced a 6.7 GB fleet log, which then made every other
			// account's behaviour in that run nearly impossible to read. The phase-level cause
			// (DecideFishPhase returning PhaseFund at the bank, an override flipping it to
			// PhaseTravel, SailLeg answering instantly, repeat) is fixed at that call site in
			// internal/bot/fisher.go via fisherplan.FundIsReallyJustWalkHome. This is the
			// floor underneath it: the primitive itself must not be able to spin, because
			// donorrunner and fisherrunner both have their own "already on the destination
			// side" fallbacks and a future caller will make the same mistake.
			//
			// A no-op arrival therefore costs one short delay. A real walk is untouched —
			// it was never going to return in microseconds anyway.
			noOpArrival := tile.Chebyshev(*here, dest) <= sailDirectArrivalRadius
			if !noOpArrival {
				d.logf("already on the destination side (no boat needed) — walking directly to %d,%d,%d", dest.X, dest.Z, dest.Level)
			}
			ok := WalkNear(d, dest, sailDirectArrivalRadius, 10*time.Minute)
			// The plain walk refuses a route through a danger zone and plans no ship, so "the
			// destination side" can still be out of its reach: live 6 Oct 2026, fishermeme
			// (ProgressiveFisher) south-west of Falador logged "no path ... that stays out of the
			// danger zones" 335 times aiming for Catherby, whose only plain route crosses White Wolf
			// Mountain. The webwalk route plans round the zones and through the ship fares.
			if !ok && !noOpArrival && d.Walker != nil {
				d.logf("the direct walk to %d,%d,%d failed — handing it to the webwalk route (zones and ship fares)", dest.X, dest.Z, dest.Level)
				ok = walkWithWalker(d, dest, sailDirectArrivalRadius, 10*time.Minute)
			}
			if ok && noOpArrival {
				d.Delay(sailNoOpArrivalDelay)
			}
			stuck := progress.note(ok, freshstart.LegDestination, dest, currentTile(d))
			if !ok {
				d.logf("a direct walk to the destination (no boat needed) did not complete — retrying")
				if stuck {
					d.logf("STRANDED: a direct walk to the destination has failed %d times in a row with no progress — this account needs an operator to look at it (stuck near %d,%d,%d, aiming for %d,%d,%d)",
						progress.fails, here.X, here.Z, here.Level, dest.X, dest.Z, dest.Level)
					if LocalRescueTeleport && rescueTeleport(d, *here) {
						progress.fails = 0
					}
					delayWatchingCombat(d, 30*time.Second)
				} else {
					d.Delay(3 * time.Second)
				}
			}
			return ok
		}
	}

	target, leg := freshstart.BoatLeg(*here, dest)

	// While still crossing Karamja on foot toward the Brimhaven pier, don't call
	// BoardShip at all. content/scripts/areas/area_karamja/scripts/customs_officer.rs2's
	// dialogue is reused verbatim by a SECOND, identically-named "Customs officer" who
	// stands right where the first (Port Sarim) ship drops accounts off at Musa Point —
	// he offers the return trip to Port Sarim, not the Brimhaven -> Ardougne one.
	// BoardShip's NPC search only matches by name within a flat radius (see
	// findNpcByNameOp in BoardShip above), so the moment an account lands at Musa Point
	// it found and boarded that decoy instead of walking on. Found live (2026-09-15,
	// freshcyc10): every account that reached Musa Point immediately reboarded straight
	// back to Port Sarim instead of heading for Brimhaven. Gate the search on actually
	// being close to the real (Brimhaven-side) Customs officer; until then just fall
	// through to the walk-target logic below, which now stages through BrimhavenPier.
	skipBoard := leg == freshstart.LegDestination && freshstart.OnKaramjaSide(*here) &&
		tile.Chebyshev(*here, freshstart.BrimhavenPier) > 20

	// board is BoardShip's memory across calls. A caller that passes no SailProgress gets a
	// fresh one each call, so no attempt is ever in flight when a call starts: it never sees a
	// boarding dialogue close, never waits out a sail, and never reaches the fallback, relying
	// instead on a later call finding the account across the water. Every real caller passes
	// one.
	board := &boardAttempt{}
	if progress != nil {
		board = &progress.board
	}

	// DO NOT RETRY THE SAME FAILING THING FOREVER. Once boarding has failed sailFallbackAfter
	// times in a row at this dock, hand the crossing to the webwalk route instead. Live
	// 2026-09-23 Acct175 re-ran the identical failing boarding for the whole 40-minute window
	// at 0 xp. See sailFallback, and boardship_wait.go for the cause of that failure.
	if !skipBoard && progress != nil && board.fails >= sailFallbackAfter {
		return sailFallback(d, dest, leg, *here, progress)
	}

	// preBoard is the dock the sail must carry the account away from. When an attempt is in
	// flight that is where its Talk-to was sent, captured before boardShip ends the attempt.
	preBoard := *here
	if !board.talkedAt.IsZero() {
		preBoard = board.from
	}
	boarded := true
	// sailStarted is the difference between "BoardShip has nothing left to do here" and "a
	// boarding dialogue took the fare", which is the only case where a set_sail teleport can
	// be on its way. See BoardShip's own doc comment for the 6,409 live sail timeouts that came
	// of conflating the first two, and boardship_wait.go for the 201 that came of conflating
	// "the dialogue closed" with "the fare was paid".
	sailStarted := false
	if !skipBoard {
		unlock := randomevents.Lock(d.Session)
		boarded, sailStarted = boardShip(d, leg, board)
		unlock()
	}
	if !boarded {
		// Say so. See noteUnboarded for the eight accounts this branch swallowed for an
		// hour. The step is included because it is the whole state of the boarding
		// machine: a step that never advances across successive lines is a stuck
		// dialogue, while one that keeps resetting to 0 is a boarding that keeps being
		// started over, and the two need different fixes.
		if progress.noteUnboarded() {
			d.logf("still boarding the %s leg at %d,%d,%d (step %d) — BoardShip has not finished; retrying",
				leg, here.X, here.Z, here.Level, board.step)
		}
		return false
	}

	// Refresh `here`: it was captured ABOVE, before BoardShip was ever called, and
	// BoardShip returning true only means its own dialogue closed with nothing left to
	// click — the underlying set_sail teleport (customs_officer.rs2's own real
	// mechanism) can land a moment later, not necessarily on the same tick. Real,
	// live-confirmed bug (2026-09-15, the first-ever fresh-account run with cheatSetup
	// off): the Ardougne-ladder fast path just below is gated on the CURRENT tile
	// being near freshstart.ArdougneArrival, but with `here` left stale at the
	// pre-boarding Brimhaven-pier position, that gate could never fire — so accounts
	// that landed exactly on the real, fixed set_sail coordinate fell through to
	// tryChangeLevel's generic same-display-name "Ship's ladder" search instead, which
	// this same file's own doc comment already documents as unreliable for this exact
	// ambiguous case (two different loc ids, opposite directions, identical name).
	// That explains the wildly inconsistent post-boat positions observed live. Wait
	// briefly for the tile to actually move away from the pre-board position (the real
	// signal a teleport happened) before trusting it for the checks below. Skipped
	// entirely when boarding itself was skipped above — nothing moved, so there is
	// nothing to wait for — and, since 2026-09-22, skipped equally when BoardShip found
	// nothing to board: waiting 35 seconds for a ship nobody boarded is not caution, it is
	// the account doing nothing at all, 8,367 times over one afternoon.
	if !skipBoard && sailStarted {
		var landed *tile.Tile
		// sailCompleteTimeout (now 35s, was 20s, was 5s before that -- see its own doc
		// comment) -- live-found 2026-09-16 (lc2b_24, a 200-account soak): BoardShip
		// returning true (its own dialogue closed with nothing left to click) does not
		// mean the server's own set_sail teleport has landed yet, and under this soak's
		// heavy simultaneous server load that teleport can genuinely take longer than
		// 5s to arrive -- the exact same class of under-load timeout this project
		// already found and fixed once before for walkNearTimeout (30s -> 120s, see its
		// own doc comment). When this wait times out, `here` stays at the pre-board
		// tile, SailLeg reports the leg incomplete, and the caller falls back to plain
		// land pathfinding toward a destination that is only reachable by boat --
		// which fails identically forever, looping the account through repeated
		// stranding rescues that can never actually succeed. Confirmed live: lc2b_24
		// cycled through this exact "STRANDED -> rescue -> retry -> STRANDED" sequence
		// five times in under six minutes, always at the same real door leading out of
		// Catherby's own enormous (109,716-tile) connected component toward the
		// Brimhaven pier, which the door-approach logic itself was picking correctly.
		waitFor(d, "the sail to complete", sailCompleteTimeout, func() bool {
			cur := currentTile(d)
			if cur == nil || !sailLanded(preBoard, *cur) {
				return false
			}
			landed = cur
			return true
		})
		if landed != nil {
			here = landed
			board.fails = 0
			if progress != nil {
				progress.fallbackFails = 0
			}
		} else {
			// The fare was taken and still no sail: count it with the other failed attempts,
			// so a dock where this keeps happening reaches the fallback too.
			board.fails++
			if cur := currentTile(d); cur != nil {
				here = cur
			}
		}
		// target/leg were computed ONCE, at the top of this function, from wherever the
		// account was standing BEFORE boarding — stale the moment the sail actually lands
		// it somewhere new, which is exactly what just happened above. Every disembark
		// special-case below (Ardougne gangplank, Karamja gangplank, Port Sarim ladder) is
		// gated on `leg` matching a specific value, so a stale leg silently skips all of
		// them even when `here` is sitting right on the known ship-deck tile they exist to
		// handle. Live-found 2026-09-15 (lc2b_167/185/188, a 200-account soak): all three
		// independently landed within a couple tiles of freshstart.KaramjaShipDeck
		// (2957,3141,1) after this exact wait, yet none of them ever logged the Karamja
		// gangplank block's own lines — confirming that block's `leg == LegDestination`
		// check never matched, because `leg` still reflected the Port-Sarim-side value from
		// before boarding. Recomputing here from the freshly-landed tile is exactly what
		// the initial computation at the top of this function already does, just re-run
		// now that the position it depends on has actually changed.
		target, leg = freshstart.BoatLeg(*here, dest)
	}

	// One direction-locked step down the Ardougne dock's gangplank before ever asking
	// FindPath about this tile — see freshstart.ArdougneArrival/ArdougneGangplank's own doc
	// comments. Addressed by hardcoded loc id + op, not by name/the generic
	// tryChangeLevel/stairNames search, same reasoning as the Karamja gangplank block
	// below. Originally used ArdougneLadder (a "Ship's ladder", Climb-down) here, which
	// LOOKED plausible (confirmed live to drop a level) but did not land accounts
	// anywhere useful — account-owner ground truth (2026-09-15) plus a direct
	// cmd/guidecrash -testloc confirmation identified the REAL fixture as this Gangplank
	// (Cross), landing exactly on 2683,3271,0. This check only ever calls the one
	// known-down id/op and verifies cur.Level < before strictly (not just "changed").
	if leg == freshstart.LegDestination && here.Level == freshstart.ArdougneArrival.Level &&
		tile.Chebyshev(*here, freshstart.ArdougneArrival) <= 4 {
		beforeLevel := here.Level
		if t, ok := d.Config.Loc(freshstart.ArdougneGangplankID); ok {
			if op := cachecfg.OpIndex(t.Ops, freshstart.ArdougneGangplankOp); op != 0 {
				if walkAdjacent(d, freshstart.ArdougneArrival, 0, 10*time.Second) {
					if err := actions.OpLoc(d.Session, op, freshstart.ArdougneGangplank.X, freshstart.ArdougneGangplank.Z, freshstart.ArdougneGangplankID); err != nil {
						d.logf("crossing the Ardougne dock gangplank: %v", err)
					} else {
						waitFor(d, "the Ardougne dock gangplank to be crossed", 5*time.Second, func() bool {
							cur := currentTile(d)
							return cur != nil && cur.Level < beforeLevel
						})
					}
				}
			}
		}
		if cur := currentTile(d); cur != nil {
			here = cur
		}
	}

	// Same idea for the BRIMHAVEN ship deck, which is where Captain Barnaby's Ardougne ->
	// Brimhaven ship sets a passenger down and which had no handler at all until 2026-09-22 —
	// see freshstart.BrimhavenShipDeck's own doc comment for the 707 arrivals against 0
	// departures that measured its absence. Walks to the deck tile itself at radius 0, the
	// same shape as the Karamja block below, because the whole deck is a handful of tiles and
	// the graph edge this is taken from names that exact tile as the crossing's From.
	//
	// The leg here is LegDestination: freshstart.TravelLeg classifies the deck as Karamja, so
	// an account standing on it is between the two ships with the Ardougne crossing still
	// ahead of it.
	if leg == freshstart.LegDestination && here.Level == freshstart.BrimhavenShipDeck.Level &&
		tile.Chebyshev(*here, freshstart.BrimhavenShipDeck) <= 4 {
		beforeLevel := here.Level
		if t, ok := d.Config.Loc(freshstart.BrimhavenGangplankID); ok {
			if op := cachecfg.OpIndex(t.Ops, freshstart.BrimhavenGangplankOp); op != 0 {
				if walkAdjacent(d, freshstart.BrimhavenShipDeck, 0, 10*time.Second) {
					if err := actions.OpLoc(d.Session, op, freshstart.BrimhavenGangplank.X, freshstart.BrimhavenGangplank.Z, freshstart.BrimhavenGangplankID); err != nil {
						d.logf("crossing the Brimhaven ship gangplank: %v", err)
					} else {
						waitFor(d, "the Brimhaven ship gangplank to be crossed", 5*time.Second, func() bool {
							cur := currentTile(d)
							return cur != nil && cur.Level < beforeLevel
						})
					}
				}
			}
		}
		if cur := currentTile(d); cur != nil {
			here = cur
		}
	}

	// Same idea for the Karamja ship deck near Musa Point that strands accounts walking
	// toward Brimhaven — see freshstart.KaramjaShipDeck/KaramjaGangplank's own doc comments.
	// Walks to the one confirmed-orthogonal approach tile exactly (radius 0), not "anywhere
	// within 1 tile" — found live that a diagonal approach gets the same "I can't reach
	// that!" rejection the Ardougne ladder's wrong sides did.
	if leg == freshstart.LegDestination && here.Level == freshstart.KaramjaShipDeck.Level &&
		tile.Chebyshev(*here, freshstart.KaramjaShipDeck) <= 8 {
		beforeLevel := here.Level
		if t, ok := d.Config.Loc(freshstart.KaramjaGangplankID); ok {
			if op := cachecfg.OpIndex(t.Ops, freshstart.KaramjaGangplankOp); op != 0 {
				if walkAdjacent(d, freshstart.KaramjaGangplankApproach, 0, 10*time.Second) {
					if err := actions.OpLoc(d.Session, op, freshstart.KaramjaGangplank.X, freshstart.KaramjaGangplank.Z, freshstart.KaramjaGangplankID); err != nil {
						d.logf("crossing the Karamja gangplank: %v", err)
					} else {
						waitFor(d, "the Karamja gangplank to be crossed", 5*time.Second, func() bool {
							cur := currentTile(d)
							return cur != nil && cur.Level < beforeLevel
						})
					}
				}
			}
		}
		if cur := currentTile(d); cur != nil {
			here = cur
		}
	}

	// A third, same-shaped fix: a plain "Ladder" near Port Sarim itself that already
	// matched the generic tryChangeLevel search by name, but whose one-sided approach
	// restriction made that search land on the wrong neighbour just like the two cases
	// above — see freshstart.PortSarimLadder's own doc comment.
	if leg == freshstart.LegPier && here.Level == freshstart.PortSarimLadder.Level &&
		tile.Chebyshev(*here, freshstart.PortSarimLadder) <= 4 {
		beforeLevel := here.Level
		if t, ok := d.Config.Loc(freshstart.PortSarimLadderID); ok {
			if op := cachecfg.OpIndex(t.Ops, freshstart.PortSarimLadderOp); op != 0 {
				if walkAdjacent(d, freshstart.PortSarimLadderApproach, 0, 10*time.Second) {
					if err := actions.OpLoc(d.Session, op, freshstart.PortSarimLadder.X, freshstart.PortSarimLadder.Z, freshstart.PortSarimLadderID); err != nil {
						d.logf("climbing down the Port Sarim ladder: %v", err)
					} else {
						waitFor(d, "the Port Sarim ladder to go down", 5*time.Second, func() bool {
							cur := currentTile(d)
							return cur != nil && cur.Level < beforeLevel
						})
					}
				}
			}
		}
		if cur := currentTile(d); cur != nil {
			here = cur
		}
	}

	// The second half of that same descent, and the part that was missing: 1749 only reaches
	// level 1, which is a sealed 7-tile pocket, so an account that climbed it was still
	// stranded — just one level lower. Ladder 1746 at 2954,3389 is the way out to the
	// mainland. Walks to one confirmed-orthogonal approach tile at radius 0, because the four
	// diagonals are refused live; see freshstart.PortSarimLadderLower's own doc comment for
	// the full 8-neighbour evidence and for why this cannot go through the generic
	// stairNames search (1746 and 1750 share the display name "Ladder" one tile apart, so a
	// failed reach can climb back UP and oscillate).
	if leg == freshstart.LegPier && here.Level == freshstart.PortSarimLadderLower.Level &&
		tile.Chebyshev(*here, freshstart.PortSarimLadderLower) <= 4 {
		beforeLevel := here.Level
		if t, ok := d.Config.Loc(freshstart.PortSarimLadderLowerID); ok {
			if op := cachecfg.OpIndex(t.Ops, freshstart.PortSarimLadderLowerOp); op != 0 {
				if walkAdjacent(d, freshstart.PortSarimLadderLowerApproach, 0, 10*time.Second) {
					if err := actions.OpLoc(d.Session, op, freshstart.PortSarimLadderLower.X, freshstart.PortSarimLadderLower.Z, freshstart.PortSarimLadderLowerID); err != nil {
						d.logf("climbing down the lower Port Sarim ladder: %v", err)
					} else {
						waitFor(d, "the lower Port Sarim ladder to go down", 5*time.Second, func() bool {
							cur := currentTile(d)
							return cur != nil && cur.Level < beforeLevel
						})
					}
				}
			}
		}
		if cur := currentTile(d); cur != nil {
			here = cur
		}
	}

	// The pier leg stages through Draynor before the dock — account-owner-supplied
	// waypoints (2026-09-15): "we need to go from lumbridge, to draynor to port sarim."
	// A single unconstrained FindPath straight from Lumbridge to PortSarimDock kept
	// wandering north through Falador/Rimmington-adjacent buildings instead of the real
	// road; breaking it into two shorter, real-landmark-anchored legs keeps each
	// individual FindPath call short and on the correct road.
	//
	// Real, live-found bug in the first version of this staging logic (2026-09-15,
	// freshcyc9, all 6 surviving accounts): comparing "distance to dock" against
	// "distance to Draynor" breaks the instant the account actually ARRIVES at Draynor,
	// since distance-to-Draynor is then ~0 and nothing can be smaller than that — the
	// switch fell through to the second case and re-targeted Draynor forever, an
	// account stuck bouncing on the same tile it already reached, never advancing to
	// the dock. Fixed: check ARRIVAL at Draynor (within WalkNear's own radius 3)
	// directly, rather than a relative distance comparison that assumes "at Draynor"
	// never actually happens.
	walkTarget := target
	switch {
	case leg == freshstart.LegPier:
		// Not a distance comparison against BrimhavenPier — live-found 2026-09-16 (a
		// 200-account soak, accounts starting from Catherby rather than the Lumbridge
		// this staging was originally built around): Catherby is geographically CLOSER
		// to Brimhaven's pier in a straight Chebyshev line than to PortSarimDock despite
		// having zero walking connectivity there (confirmed via cmd/doordiag: the whole
		// 109,716-tile Catherby-side component never reaches BrimhavenPier). The old
		// `Chebyshev(dock) < Chebyshev(pier)` check silently fell through to the raw
		// `target` (BrimhavenPier itself) for any such starting point, and WalkNear's own
		// FindPath call against an unreachable tile just failed forever — the account
		// cycled STRANDED -> rescue-to-Lumbridge -> immediately re-tried the identical
		// broken route -> STRANDED again, endlessly. TravelLeg only returns LegPier for
		// positions that are neither on Karamja nor already past Ardougne, so unlike
		// leg==LegDestination below there is no legitimate reason for this whole leg to
		// ever target the far-side pier directly — always stage through Draynor first
		// unless already there.
		if tile.Chebyshev(*here, freshstart.Draynor) <= 3 {
			walkTarget = freshstart.PortSarimDock
		} else {
			walkTarget = freshstart.Draynor
		}
	case leg == freshstart.LegDestination && freshstart.OnKaramjaSide(*here):
		// Still on foot on Karamja, short of the Brimhaven pier — target.go's BoatLeg
		// returns the FAR destination (e.g. Catherby) directly for this leg, which is
		// unreachable by plain walk from here (it's across the second, uncrossed ship).
		// Stage through the pier; skipBoard above keeps BoardShip from acting on the
		// decoy Customs officer along the way. Musa Point and the pier are themselves two
		// separate collision-pack components (see freshstart.KaramjaMainlandGate's own
		// doc comment) — stage through the connecting gate first, the same two-hop
		// pattern LegPier uses for Draynor before PortSarimDock, so the account gets
		// close enough for the generic door-recovery in walkNearCore to actually find and
		// open it.
		//
		// Real, live-found bug (2026-09-15, freshcyc13): once the account had ALREADY
		// reached BrimhavenPier and BoardShip found no Customs officer nearby that
		// specific tick (they wander — confirmed live via cmd/npcscan, seen anywhere from
		// 6 to 12+ tiles off), this switch fell through to the gate-distance check, which
		// is far now that the account is past it, so it re-targeted the gate and walked
		// the account BACKWARD across the same gate it had just crossed, forever. Check
		// arrival at the pier FIRST and hold position there (walkTarget = here, a no-op
		// for WalkNear) so BoardShip simply gets to keep retrying its own search next
		// tick instead of the account being yanked away from the one place the boarding
		// NPC actually is.
		switch {
		case tile.Chebyshev(*here, freshstart.BrimhavenPier) <= 5:
			walkTarget = *here
		case tile.Chebyshev(*here, freshstart.KaramjaMainlandGate) <= 3:
			walkTarget = freshstart.BrimhavenPier
		default:
			walkTarget = freshstart.KaramjaMainlandGate
		}
		// A STAGING WAYPOINT THAT CANNOT BE REACHED IS NOT A STAGING WAYPOINT. The ladder
		// above is by DISTANCE, and distance is the wrong question on an island made of two
		// collision-pack components: it can send an account to the gate when the gate is in
		// the component the account is not in, and the walk then fails identically for ever.
		//
		// Live 2026-09-22: acct92 sat at 2758,3196 for two entire runs at 0 xp, logging
		// "no path from 2758,3196,0 to 2816,3182,0: unreachable" 118 times. It is 38 tiles
		// from the pier and 58 from the gate, so it fell to the default and was aimed at the
		// gate. Measured against the real pack from that exact tile: the gate is UNREACHABLE
		// (21,951 expansions, the whole component exhausted) and the pier is reachable in
		// SEVENTY-SIX. It was being staged through the one tile it could not get to, while
		// the one it needed was a short walk away.
		//
		// So ask the pack instead of the ruler, and keep the ladder's own order for the case
		// it was written for: an account at Musa Point genuinely cannot reach the pier without
		// crossing the gate first, and there the pier fails this test and the gate passes it.
		// Only when nothing is reachable does this fall back to the distance answer, which is
		// no worse than what it replaces.
		if d.Pack != nil && walkTarget != *here {
			for _, cand := range []tile.Tile{freshstart.BrimhavenPier, freshstart.KaramjaMainlandGate} {
				if cand == walkTarget {
					break // the ladder already chose the best reachable one
				}
				if d.Pack.FindPath(
					pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level},
					pathfind.NavPoint{X: cand.X, Z: cand.Z, Level: cand.Level},
				).OK {
					d.logf("staging through %d,%d instead of %d,%d for the %s leg: the pack has no route to the one distance chose",
						cand.X, cand.Z, walkTarget.X, walkTarget.Z, leg)
					walkTarget = cand
					break
				}
			}
		}
	}

	// HOLDING POSITION AT A DOCK IS NOT A FREE LOOP. When BoardShip found no boarding NPC and
	// the walk target is the tile the account is already standing on, WalkNear below answers
	// instantly with no packet sent and SailLeg returns true, so a caller that re-derives its
	// phase and calls straight back in spins as fast as the CPU allows — the same shape as the
	// direct-reachability short-circuit's own 398,648-calls-in-27-seconds incident at the top
	// of this function. Until 2026-09-22 the accidental brake on this path was the 35-second
	// sail wait, which now (correctly) does not run when nothing was boarded, so the wait has
	// to be real and deliberate instead.
	//
	// The wait is also the right behaviour on its own terms: these ships' boarding NPCs wander
	// (cmd/npcscan exists because the Brimhaven-side Customs officer repeatedly came up empty
	// inside BoardShip's 15-tile search), so the account is waiting for one to walk back into
	// view. The log line is rate-limited because a hundred accounts sharing one dock would
	// otherwise drown the fleet log in it.
	//
	// It returns here rather than falling through, because the "boat leg" line below and the
	// no-op WalkNear after it both describe work this call is not doing. Emitting them anyway
	// is how run/fleet.log came to hold 8,367 "boat leg: Brimhaven ship -> Ardougne ->
	// destination" lines in one afternoon for journeys that never began. progress is told the
	// same "nothing failed" the no-op walk used to tell it, so the STRANDED escalation behaves
	// exactly as before.
	if !skipBoard && !sailStarted && walkTarget == *here {
		names, _ := sailDialogueChoices(leg)
		if progress.noteNoBoarding() {
			d.logf("holding at %d,%d,%d for the %s leg: none of %v is within reach to board — waiting for one to come back into view",
				here.X, here.Z, here.Level, leg, names)
		}
		progress.note(true, leg, walkTarget, here)
		delayWatchingCombat(d, sailNoBoardingWait)
		return true
	}

	d.logf("boat leg: %s (%d coins)", legLabel(leg), coinsHeld(d))
	ok := WalkNear(d, walkTarget, 3, 10*time.Minute)
	stuck := progress.note(ok, leg, walkTarget, currentTile(d))
	if !ok {
		d.logf("the %s leg did not complete — see the pathfind reason above; retrying", leg)
		if stuck {
			d.logf("STRANDED: the %s leg has failed %d times in a row from the same tile with no progress — this account needs an operator to look at it (stuck near %d,%d,%d, aiming for %d,%d,%d)",
				leg, progress.fails, here.X, here.Z, here.Level, walkTarget.X, walkTarget.Z, walkTarget.Level)
			// SailProgress predates, and is entirely separate from, walkNearCore's own
			// consecutiveUnreachable/LocalRescueTeleport pair — this whole function's own
			// stuck-detection never got that same hook wired in, so an account genuinely
			// stuck on a boat leg (as opposed to inside walkNearCore's own internal
			// retry loop) just repeated this exact log line every ~90s forever with no
			// recovery at all. Live-found the same 200-account soak that verified
			// walkNearCore's own version: `progress.fails` climbing 5, 10, 15, 20... with
			// identical here/walkTarget every time. Same fix, same place: rescue and let
			// the caller re-derive on its own next call rather than resetting fails to 0
			// and looping inside this same call (SailProgress has no equivalent "continue
			// with a fresh target" to loop into anyway — the caller already re-invokes
			// SailLeg fresh each time, so returning false here already does the right
			// thing once the account is actually somewhere sane again).
			if LocalRescueTeleport && rescueTeleport(d, *here) {
				progress.fails = 0
			}
			delayWatchingCombat(d, 30*time.Second)
		} else {
			d.Delay(3 * time.Second)
		}
	}
	return ok
}

func legLabel(leg freshstart.Leg) string {
	if leg == freshstart.LegPier {
		return "Port Sarim ship -> Karamja -> Brimhaven pier"
	}
	return "Brimhaven ship -> Ardougne -> destination"
}

func coinsHeld(d *Deps) int {
	inv := d.Session.State().Inventory(playerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
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

// playerPack is the persistent inventory-tab component id — the same confirmed-stable
// literal barbarianrunner.PlayerPack/fisherrunner.PlayerPack use, per those constants' own
// doc comments.
const playerPack = 3214

// spotUnreachableFor is how long a "no route to the spot" answer is trusted before the
// account probes again. Long enough that a hundred accounts are not each re-planning a
// cross-map route every two minutes, short enough that a ship fare earned in the meantime
// still gets used.
const spotUnreachableFor = 15 * time.Minute
