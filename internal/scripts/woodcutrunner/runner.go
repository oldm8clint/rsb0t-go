// Package woodcutrunner is the I/O half of the ProgressiveWoodcutter port: it drives the pure
// ladder/phase/axe/mule decisions in internal/scripts/woodcutplan against a live session,
// following the exact split internal/scripts/essencerunner and internal/scripts/crafterrunner
// already proved — a pure plan package decides, this package does the walking/clicking/
// banking, and internal/bot/woodcutter.go wires the two together. See
// docs/script_porting_framework.md for the recipe this package follows, and
// internal/scripts/essencerunner/runner.go (the most recently completed port before this one)
// for the primitive shapes duplicated here on purpose (WaitForGuardian/bank-booth retry — see
// that package's own doc comment on "why duplicated, not shared").
//
// WalkNear is the one exception to that "duplicate a small WalkNear per package" convention,
// and deliberately so: found live (2026-09-16, this port's own first live run) that EVERY
// route from the Seers bank to EVERY tree tier crosses at least one closed door the static
// collision pack's own FindPath graph reports as traversable but the real server silently
// refuses to walk through (confirmed via cmd/doordiag and a live repro — a simple
// recompute-and-rewalk WalkNear span the same 6-waypoint route dozens of times over two full
// minutes without moving one tile). crafterrunner.WalkNear hits the identical class of gap for
// its own Seers wheelhouse door and works around it with a hand-built, Seers-specific
// crossSeersDoor; internal/scripts/freshstartrunner's own WalkNear already has a GENERIC
// version of that same recovery (tryOpenDoor, tryChangeLevel, tryClimbOver — see its own doc
// comments for the several live incidents each one fixed), battle-tested across
// ProgressiveFisher/ProgressiveCrafter's own first-day travel. Rather than hand-porting a
// second Seers-specific door hack (this package's own tree route's stuck door was not even the
// same one crafterrunner's crossSeersDoor already knows about — a different, previously
// undocumented closed door on the bank-to-trees leg), this package's own WalkNear is a thin
// wrapper around freshstartrunner.WalkNear, which already solves this class of problem
// generally.
//
// Every blocking wait goes through a caller-supplied Delayer instead of time.Sleep directly, so
// internal/bot's Context.Delay can actually cancel a running chopper mid-wait.
//
// The appearance screen, the tutorial skip and the brightness press are NOT re-implemented
// here: internal/scripts/freshstartrunner already has AcceptAppearance/SkipTutorial/
// BrightnessStep, shared with ProgressiveFisher/ProgressiveCrafter, and internal/bot/
// woodcutter.go calls those directly. The boat crossing (Port Sarim -> Karamja -> Brimhaven ->
// Ardougne -> Seers) is freshstartrunner.SailLeg, likewise shared and called directly from the
// bot adapter.
//
// Deliberately NOT ported here, and why:
//   - woodcutHud.ts — the Electron wall's on-screen overlay; no headless equivalent.
//   - asking the mule for a rune AXE (ProgressiveWoodcutter.ts's own askForAxe, driven by
//     muleOrder.ts/orderPlan.ts). This note used to say the order could never be answered
//     because MuleReceiver did not read KindOrder; that is no longer true — internal/bot/
//     mule.go now has a `case mulebus.KindOrder` running mulerunner.Handout, and
//     donorrunner.Await is a complete, receipt-verified donor half. It is still not ported,
//     for a different and better reason: a mule hands out what is in its OWN bank, a fleet
//     mule's bank holds logs and casket loot rather than rune axes, and nothing can restock
//     one, because no NPC shop on this server sells an axe above steel (measured — see
//     internal/scripts/woodcutplan/axe.go). Asking for COINS and buying the axe is what
//     actually works, and that is what market.go and internal/bot/woodcutteraxe.go do.
package woodcutrunner

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/chopmanipplan"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/webwalk"
)

// WoodcuttingStat is UPDATE_STAT's index for Woodcutting, per the same PlayerStat enum
// essencerunner.MiningStat (14) and barbarianrunner.AgilityStat (16) cite — confirmed against
// gamestate/skills.go's own names table: Attack,Defence,Strength,Hitpoints,Ranged,Prayer,
// Magic,Cooking,Woodcutting,... so Woodcutting is index 8.
const WoodcuttingStat = 8

// PlayerPack is the persistent inventory-tab component id — see essencerunner.PlayerPack's own
// doc comment for why this fixed, always-current component is read instead of scanning every
// known inventory (a shop's own stock would otherwise contaminate the count).
const PlayerPack = 3214

// PackSlots is a full pack.
const PackSlots = 28

// Delayer sleeps for d, returning false early if the caller should stop instead of true once d
// has fully elapsed — the same shape every sibling runner package uses.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration — for a standalone tool with nothing to cancel.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one chop/bank/travel/hand-off step needs from the outside world.
type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	// Blocks are trees not to chop right now, because an Ent was seen at one. Optional: a nil
	// Blocks means no exclusions, which is the behaviour this package had before Ents were
	// handled at all. internal/bot/woodcutter.go owns one per account for the life of the run.
	Blocks *woodcutplan.TreeBlocks
	// Claims de-conflicts tree targets ACROSS accounts, so a camp full of woodcutters does not
	// all click the same tree — which matters because a normal tree yields exactly one log and
	// then becomes a stump (internal/scripts/woodcutplan/treeclaim.go quotes the content that
	// says so).
	//
	// NIL IS NOT "OFF": nil means the process-wide registry, woodcutplan.SharedTreeClaims().
	// That is deliberate, and it is the opposite of Blocks above. Blocks is per-account state
	// only its own owner can populate, so nil sensibly means "none". Claims is SHARED state
	// whose entire value is that every account in the process uses the same one, so a caller
	// that simply did not know about the field would otherwise get no de-confliction at all —
	// which is the bug, not the default. A test or a single-account harness that wants
	// isolation passes its own woodcutplan.NewTreeClaims().
	Claims *woodcutplan.TreeClaims
	// Walker, when set, plans long legs over internal/webwalk's door/gate/stair/ship-fare
	// graph instead of one straight A* across the collision pack.
	//
	// Live-found 2026-09-20 (cmd/woodkilltest -stage axes): BuyFromBob walked with plain
	// freshstartrunner.WalkNear, which pathfinds Seers -> Lumbridge as ONE search, reports
	// "unreachable", and then spends its whole 120s budget thrashing doors. NOTHING in the
	// axe path used webwalk at all, so an account that broke its axe or needed one from Bob
	// could never get there. Nil falls straight through to the plain-pathfind behaviour this
	// package had before, so a caller that predates this field is unchanged — the same shape
	// fisherrunner.Deps.Walker already uses, and for the same reason.
	Walker *webwalk.Walker
	// Loot receives what a chop really yielded, for the Loot panel's Woodcutting card (see
	// chopcount.go). Reporting only, and nil records nothing. internal/bot/woodcutter.go passes
	// bot.Context.Loot.
	Loot func(loot.Drop)
	// Log receives one already-formatted line per notable event; nil drops them.
	Log func(string)
	// World is the game world this account is logged in to, for the per-world tree-claim
	// registry. Zero means "the fleet's own world", which is what a caller outside a fleet
	// has and what keeps a single-world run behaving exactly as it did.
	World int
	// Manip, when set and Enabled, makes Chop write a knife-on-log batch after each roll so the tree rolls every 3 ticks
	// instead of 4 (chopmanip.go, internal/scripts/chopmanipplan). Nil is the plain chop this package always had;
	// only ProgressiveWCFletch sets it.
	Manip *ManipConfig
}

func (d *Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

func currentTile(d *Deps) tile.Tile {
	x, z, level, _ := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
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

// emergencyFleeDriftTiles is 0 — this script has no single fixed "home" the way
// fisherrunner's Catherby camp does (its own leg spends real time crossing the whole map, then
// chopping several different Seers camps depending on level); see essencerunner's identical
// choice and doc comment.
const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint // unused while emergencyFleeDriftTiles <= 0

const guardianWaitLogEvery = 10 * time.Second

// WaitForGuardian pauses while a random event is detected at all — mirrors
// essencerunner.WaitForGuardian/fisherrunner.WaitForGuardian exactly.
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

// walkNearTimeout bounds WalkNear's entire operation — see essencerunner.walkNearTimeout's own
// doc comment for the live evidence behind 120s.
const walkNearTimeout = 120 * time.Second

// fresh adapts d to freshstartrunner.Deps — same field-for-field copy every sibling runner
// that calls into freshstartrunner already does (see e.g. crafterrunner.Fresh).
func fresh(d *Deps) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay), Log: d.Log,
		Walker: d.Walker,
	}
}

// WalkNear paths to dest and walks there, stopping once within radius tiles of dest — a thin
// wrapper around freshstartrunner.WalkNear (see this package's own doc comment for why this is
// NOT one of the small hand-duplicated WalkNears every other sibling runner has: the Seers
// bank-to-trees route needs the generic door-opening recovery only freshstartrunner's own
// WalkNear already has).
// Routes through freshstartrunner.WalkWithWalker rather than plain freshstartrunner.WalkNear:
// with a Deps.Walker set it plans over webwalk's door/gate/stair/ship-fare graph, and with no
// Walker it IS plain freshstartrunner.WalkNear, call for call. See Deps.Walker for the live
// failure that made the difference.
func WalkNear(d *Deps, dest tile.Tile, radius int) bool {
	return freshstartrunner.WalkWithWalker(fresh(d), dest, radius, walkNearTimeout)
}

// travelTimeout bounds a genuine cross-region leg. walkNearTimeout (120s) is sized for a walk
// inside one camp; Seers to Lumbridge is most of the map and several doors, and a leg cut off
// half way leaves the account stranded somewhere it has no business being.
//
// Ten minutes is sized from the route itself rather than guessed: cmd/webwalkplan puts the
// quest-less Seers -> Lumbridge plan at about 400 walk corners plus two paid ship hops, and a
// ship hop alone is up to 40 dialogue passes at a tick each on top of the sailing. At a
// production 600ms tick that is several minutes of genuine progress, and cutting it short
// strands the account mid-route, which is strictly worse than waiting.
const travelTimeout = 10 * time.Minute

// travelAttempts is how many times a cross-region leg is re-planned before giving up.
//
// Not a nervous retry: MEASURED on the lab engine, 2026-09-20, two consecutive Seers -> Bob's
// runs. The first arrived in 3m28s. The second failed at the Ardougne pier with
//
//	[hop] Ardougne->Brimhaven ship: could not get beside Captain Barnaby at 2674,3270,0
//	[walk] walk to 3232,3203,0: baked walk failed: no path: unreachable
//
// because Barnaby had WANDERED nine tiles off the graph edge's own From tile and hops.Handler
// gives an approach six seconds. One failed NPC hop then leaves webwalk with no route at all
// from where it is standing, so the whole leg reports unreachable — and the very next WalkTo
// from the identical tile planned a complete route again (cost 574). The failure is a moving
// NPC, not a missing path, and the cure is to plan again rather than to abandon the account
// halfway across the world. The hop's own approach budget lives in internal/webwalk/hops,
// which this work does not own.
const travelAttempts = 3

// TravelTo is WalkNear with the cross-region budget, for the places this script leaves the
// Seers area entirely: Bob's Axes for a repair or a cheap axe, and the Lumbridge store.
func TravelTo(d *Deps, dest tile.Tile, radius int) bool {
	for attempt := 1; attempt <= travelAttempts; attempt++ {
		if freshstartrunner.WalkWithWalker(fresh(d), dest, radius, travelTimeout) {
			return true
		}
		if tile.Chebyshev(currentTile(d), dest) <= radius {
			// Arrived anyway: a walk that reports failure from its last poll while the
			// account is standing on the destination is a false negative, and treating it
			// as a failure would send the account round again.
			return true
		}
		if attempt < travelAttempts {
			d.logf("the leg to %d,%d did not finish (attempt %d of %d) — planning again from %v",
				dest.X, dest.Z, attempt, travelAttempts, currentTile(d))
			if !d.Delay(3 * time.Second) {
				return false
			}
		}
	}
	return false
}

// interactApproachTimeout bounds one candidate stand-tile walk. These are a few tiles at most,
// so the 120s walkNearTimeout would only ever be spent waiting out a tile some other account is
// standing on, when trying the next candidate is the better move.
const interactApproachTimeout = 20 * time.Second

// maxInteractApproaches caps how many stand tiles are tried before falling back, so a tree
// hemmed in on three sides costs a couple of short walks rather than a minute.
const maxInteractApproaches = 4

// walkToInteract puts the account on a tile the engine will actually accept an interaction with
// target from, rather than merely near it.
//
// Why this is not `Chebyshev(here, target) <= 1`: the engine's own reachedEntity/reachRectangle1
// require the tiles to touch ORTHOGONALLY, and Chebyshev 1 includes the four diagonals, which
// touch only at a corner and can have a wall between them. A chop clicked from a diagonal is
// accepted by the client, ignored by the server, and reported as nothing at all — the account
// stands beside a tree swinging at nothing until the stall timeout, then re-clicks the same
// tree from the same tile forever. This repo has shipped that exact bug three times, most
// recently in CombatTrainer's fight loop; pathfind.CanInteractFrom is the shared answer.
//
// Falls back to the old WalkNear(target, 1) when there is no pack or none of the candidate
// tiles can be walked to, so this is never worse than the behaviour it replaces.
//
// NOT ENOUGH ON ITS OWN, and that was MEASURED rather than reasoned about — see
// walkToInteractLoc below, which every caller that knows the loc should use instead. This
// function only ever sees a TILE, so pathfind.CanInteractFrom and InteractTiles treat the loc
// as one square. A tree is 2x2 (content: every `tree`/`deadtree*` row in
// skill_woodcutting/configs/trees/normal.loc carries width=2 length=2), so the engine's own
// rectangle test works off a 2x2 footprint this function cannot see, and the two disagree.
func walkToInteract(d *Deps, target tile.Tile) bool {
	if d.Pack == nil {
		return WalkNear(d, target, 1)
	}
	at := pathfind.NavPoint{X: target.X, Z: target.Z, Level: target.Level}
	here := currentTile(d)
	if d.Pack.CanInteractFrom(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, at) {
		return true
	}
	stands := d.Pack.InteractTiles(at)
	sort.Slice(stands, func(i, j int) bool {
		return tile.Chebyshev(here, tile.Tile{X: stands[i].X, Z: stands[i].Z, Level: stands[i].Level}) <
			tile.Chebyshev(here, tile.Tile{X: stands[j].X, Z: stands[j].Z, Level: stands[j].Level})
	})
	for i, stand := range stands {
		if i >= maxInteractApproaches {
			break
		}
		if freshstartrunner.WalkNear(fresh(d), tile.Tile{X: stand.X, Z: stand.Z, Level: stand.Level}, 0, interactApproachTimeout) {
			return true
		}
	}
	return WalkNear(d, target, 1)
}

// walkToInteractLoc is walkToInteract for a caller that knows WHICH loc it is about to click,
// and it is the one to use. It models the engine's own reach test with internal/locreach —
// the loc's real width, length, placement angle and forceapproach mask — instead of treating
// the loc as a single square.
//
// MEASURED, 2026-09-20, lab engine, the Draynor Manor start camp. A fresh account stood at
// 3091,3345 and the script clicked `Dead tree` id 1282 at 3089,3346 and id 1289 at 3090,3345,
// over and over, for three minutes: Woodcutting stayed at 0 xp, no log ever landed, and the
// engine's answer was the one ordinary game message nothing in this package reads —
//
//	[chat] I can't reach that!
//
// That is CLAUDE.md's own gotcha, the second of its four reasons a loc interaction "does
// nothing": the engine sets the interaction, then runs the trigger only from a tile
// reachedLoc accepts, and with clientRoutefinder on it will NOT walk the account into
// position. pathfind.CanInteractFrom had said yes, because it was comparing the account's
// tile with the loc's ORIGIN tile; the engine was comparing it with a 2x2 footprint.
//
// The same package doc predicted exactly this ("use internal/locreach instead of 'walk to
// radius 1'"), and internal/scripts/mulerunner/cook.go's approachRange is the shape copied
// here. Shapes locreach cannot model (walls, wall decoration) fall back to walkToInteract, so
// nothing regresses where this cannot improve on it.
func walkToInteractLoc(d *Deps, m locindex.Match) approach {
	target := tile.Tile{X: m.X, Z: m.Z, Level: m.Level}
	if d.Config == nil {
		return approach{Reached: walkToInteract(d, target)}
	}
	typ, ok := d.Config.Loc(m.ID)
	if !ok {
		return approach{Reached: walkToInteract(d, target)}
	}
	l := locreach.Loc{
		X: m.X, Z: m.Z, Level: m.Level,
		Width: typ.Width, Length: typ.Length,
		Angle: m.Angle, Shape: m.Shape, ForceApproach: typ.ForceApproach,
	}
	here := currentTile(d)
	if _, modelled := locreach.Reached(here.X, here.Z, l); !modelled {
		// A shape locreach does not model — the old behaviour, which is all there was before.
		return approach{Reached: walkToInteract(d, target)}
	}
	if standOK(d, here, l, target) {
		return approach{Reached: true}
	}
	// The tiles the ENGINE accepts, nearest first. locreach.Tiles puts the footprint last for a
	// reason: a tree's own squares are legal for the engine and unwalkable in practice.
	stands := locreach.Tiles(l)
	sort.SliceStable(stands, func(i, j int) bool {
		return tile.Chebyshev(here, stands[i]) < tile.Chebyshev(here, stands[j])
	})
	legal := make([]tile.Tile, 0, len(stands))
	for _, to := range stands {
		if standOK(d, to, l, target) {
			legal = append(legal, to)
		}
	}
	// Free tiles first, then the ones another account is standing on — ordered, NOT dropped. See
	// freeFirst for the measurement that changed this.
	ordered, occupiedBy := freeFirst(legal, func(t tile.Tile) (bool, string) { return standOccupied(d, t) })
	res := approach{Legal: len(legal)}
	tried, unroutable, busy := 0, 0, 0
	for i, to := range ordered {
		if tried >= maxInteractApproaches {
			break
		}
		if to == here {
			return approach{Reached: true, Legal: len(legal)}
		}
		// CAN THIS ACCOUNT ACTUALLY GET THERE? A tile can satisfy the engine's reach model, be
		// walkable, and have no wall in the way, and still have no ROUTE from where the account
		// is standing — the trees themselves block, so a stand tile on the far side of a cluster
		// is one you could stand on and cannot reach.
		//
		// MEASURED OFFLINE against the real collision pack (cmd/woodanchortest now asks this
		// too): 1 of the 18 stand tiles the Seers normal camp's trees accept has no route from
		// the anchor, while every other tier has none at all. A single-account 6-minute soak
		// logged 14 "unreachable" lines walking between those trees.
		//
		// WHAT THIS IS NOT, because the first version of this comment said it was and was
		// wrong. A production report of two accounts at this camp (+200 xp with 33 stalls
		// against +950 with 5) was first read as the camp being pathing-bound, and the
		// reporter RETRACTED that within the hour: over nine more minutes both accounts
		// accelerated hard (to roughly 3,300 and 7,500 xp/h) WHILE the fleet stall counter
		// rose 182 -> 221, and the gap narrowed from 4.8x to 2.7x and kept closing. Stalls
		// rise WITH throughput, because chopping faster means more tree-to-tree walks and
		// every walk can stall. They are frequent but cheap — a retry, not a wedge.
		//
		// So this is an EFFICIENCY measure with no measured throughput gain, not a fix for a
		// binding constraint, and it must not be quoted as one. The lesson the reporter drew
		// is worth keeping next to it: a rising fault counter says nothing until it is put
		// beside a throughput number. The same 6-minute soak that logged those 14 lines also
		// banked 54 logs and gained 1,625 xp, and PASSED.
		//
		// Asking first turns a stalled walk into one short A*. Candidates are ordered
		// nearest-first and this returns on the first that works, so the usual cost is a single
		// successful search over a couple of tiles; only a genuinely trapped tile pays for a
		// failed one. Exhausting them reports what it found, and Chop then either excludes the
		// tree for UnreachableBlockFor and picks another, or — when the camp's own anchor has no
		// route from here either — walks the account back to the camp (Chop's "cut-off").
		if !Routable(d.Pack, here, to) {
			unroutable++
			continue
		}
		// SOMEBODY ALREADY STANDING ON IT does not stop this account standing there too, so an
		// occupied tile is walked at — after every free one, never instead of one. See freeFirst.
		if occupiedBy[i] != "" {
			busy++
			if ChopDiag {
				d.logf("stand tile %d,%d for loc %d is occupied by %q — walking onto it anyway (players do not block players)", to.X, to.Z, m.ID, occupiedBy[i])
			}
		}
		tried++
		if approachWalk(d, to) {
			return approach{Reached: true, Legal: len(legal), Tried: tried, Occupied: busy}
		}
	}
	res.Tried, res.Unroutable, res.Occupied = tried, unroutable, busy
	if ChopDiag && unroutable > 0 {
		d.logf("loc %d at %d,%d: %d of %d stand tile(s) have no route from %d,%d (%d occupied by another account)",
			m.ID, m.X, m.Z, unroutable, len(legal), here.X, here.Z, busy)
	}
	// Nothing the engine accepts could be reached. Say so rather than clicking from where we
	// stand: that click is the three silent minutes this function was written for.
	if ChopDiag {
		d.logf("no tile the engine accepts an interaction with loc %d at %d,%d from could be reached (tried %d of %d candidates)",
			m.ID, m.X, m.Z, tried, len(stands))
	}
	return res
}

// approach is what walkToInteractLoc found, so Chop can tell "this tree has no reachable stand
// tile" from "this ACCOUNT can reach none of the camp" — the two want opposite answers.
type approach struct {
	// Reached: the account is on a tile the engine accepts the interaction from.
	Reached bool
	// Legal is how many tiles the engine would accept (standOK), Unroutable how many of those
	// had no route from where the account stands, Tried how many were actually walked at, and
	// Occupied how many of the walked-at-or-skipped ones had another account on them.
	Legal, Unroutable, Tried, Occupied int
}

// noRouteToAny reports that the tree has stand tiles and the account can route to none of them —
// the shape of an account on the wrong side of a wall, as opposed to a tree hemmed in by its
// neighbours (no legal tile at all) or a walk that stalled (Tried > 0).
func (a approach) noRouteToAny() bool {
	return !a.Reached && a.Tried == 0 && a.Legal > 0 && a.Unroutable == a.Legal
}

// approachWalk is the one walk walkToInteractLoc sends at a stand tile. A variable only so a test
// can record where the approach went without a live engine to walk on.
var approachWalk = func(d *Deps, to tile.Tile) bool {
	return freshstartrunner.WalkNear(fresh(d), to, 0, interactApproachTimeout)
}

// freeFirst orders stand tiles free ones first, then the ones another account stands on, each
// group keeping its incoming (nearest-first) order. occupiedBy[i] names who is on ordered[i], or
// "" when nobody is. NOTHING IS DROPPED.
//
// It used to drop them. The reasoning was that "a tile another account occupies cannot be walked
// onto", and its own comment said the rule was "reasoned from the geometry, NOT measured". It was
// then measured, and it is false:
//
//   - The engine (the lab checkout's src/engine/entity): a standing player marks its tile
//     PLAYER_OCC (BlockWalk.PLAYER in PathingEntity.refreshZonePresence), and only an NPC's
//     movement tests that flag (Npc.blockWalkFlag). A player's own movement tests
//     BLOCK_NPC_AND_PLAYERS (Player.blockWalkFlag), which no player sets, so players stack.
//   - The lab, 2026-09-23 (occtest, two fresh accounts): with account A on 2711,3510, the only
//     stand tile of the willow at 2711,3511, account B's real Chop skipped it as occupied,
//     returned "unreachable" and excluded the tree; B then walked onto the same tile while A
//     stayed on it, and chopped the willow for +675 xp.
//   - The cost, live on build 3dfb543a, 02:08-03:17: most trees at the Seers camps have one or two
//     stand tiles, and PickTreeFor's own fallback deliberately doubles accounts up on one tree when
//     there are more accounts than trees. So a crowd excluded the very trees it was working, each
//     for UnreachableBlockFor, until every account saw every candidate excluded: 5,921 "every tree
//     ... has an Ent at it" lines over 80 WCFletch accounts in 69 minutes, about 172 account-
//     minutes an hour, and a 16-account lab crowd attributed 343 of 353 exclusions to an occupied
//     tile and none to an Ent.
//
// Free tiles still go first, because stacking is legal but a free tile of the same tree is at
// least as good and spreads the camp out.
func freeFirst(tiles []tile.Tile, occupied func(tile.Tile) (bool, string)) (ordered []tile.Tile, occupiedBy []string) {
	ordered = make([]tile.Tile, 0, len(tiles))
	occupiedBy = make([]string, 0, len(tiles))
	var busy []tile.Tile
	var busyBy []string
	for _, t := range tiles {
		if occ, who := occupied(t); occ {
			if who == "" {
				who = "another player"
			}
			busy = append(busy, t)
			busyBy = append(busyBy, who)
			continue
		}
		ordered = append(ordered, t)
		occupiedBy = append(occupiedBy, "")
	}
	return append(ordered, busy...), append(occupiedBy, busyBy...)
}

// standOK reports whether an interaction with l sent from t would actually be accepted.
//
// IT ASKS BOTH PACKAGES, AND THAT IS THE POINT: each models a different half of the engine's
// reachRectangle1, each is necessary, and NEITHER IS SUFFICIENT. Both halves were measured
// failing on the lab engine on 2026-09-20, one per camp, in the same afternoon:
//
//   - internal/locreach knows the loc's REAL FOOTPRINT (width, length, placement angle,
//     forceapproach) and rejects the diagonals. pathfind.CanInteractFrom does not: it resolves a
//     blocked target through the collision pack's blocked REGION, and at the Draynor camp the
//     dead trees are packed tightly enough that the blob spans several of them, so it accepted
//     3091,3345 for the tree at 3089,3346 — two tiles away and diagonal to its own footprint.
//     The engine answered "I can't reach that!" and the account chopped nothing for 3 minutes.
//   - pathfind.CanInteractFrom knows the WALLS, which locreach's own package doc says it
//     deliberately does not model ("a tile this package accepts can therefore still be refused
//     by the engine"). At the Seers normal-tree camp the tile 2732,3483 is the north side of the
//     2x2 tree at 2732,3481 and locreach accepts it — but the map puts a shape-0 wall (loc 980)
//     on that very tile, so the engine refuses. Two independent accounts wedged there, on the
//     same tree, gaining nothing for over three minutes each.
//
// So a tile is good only when both say yes. The cost of asking twice is two map lookups; the
// cost of asking once is an account that stands still and reports nothing at all.
// LocOf builds the locreach.Loc for a matched loc, resolving its real width, length and
// forceapproach through the config archive. ok is false when the config cannot name the type,
// in which case no reach model applies and the caller must fall back.
//
// Exported so a harness can ask the SAME question the runner asks, rather than reimplementing
// the reach model and then proving its own copy instead of the code that ships.
func LocOf(cfg *cachecfg.Config, m locindex.Match) (locreach.Loc, bool) {
	if cfg == nil {
		return locreach.Loc{}, false
	}
	typ, ok := cfg.Loc(m.ID)
	if !ok {
		return locreach.Loc{}, false
	}
	return locreach.Loc{
		X: m.X, Z: m.Z, Level: m.Level,
		Width: typ.Width, Length: typ.Length,
		Angle: m.Angle, Shape: m.Shape, ForceApproach: typ.ForceApproach,
	}, true
}

// StandTiles is every tile an account could interact with l from, nearest to from first, keeping
// only those the collision pack says can be stood on with the facing side clear.
//
// This is exactly the candidate set walkToInteractLoc walks to, factored out so cmd/woodanchortest
// can count them offline against the real pack. Routability is deliberately NOT part of it: whether
// a candidate can be WALKED to depends on where the account is standing, and separating "the engine
// would accept this tile" from "I can get to this tile" is the whole point of the split.
func StandTiles(pk *pathfind.Pack, l locreach.Loc, from tile.Tile) []tile.Tile {
	target := tile.Tile{X: l.X, Z: l.Z, Level: l.Level}
	out := make([]tile.Tile, 0, 12)
	for _, t := range locreach.Tiles(l) {
		if standAccepts(pk, t, l, target) {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return tile.Chebyshev(from, out[i]) < tile.Chebyshev(from, out[j])
	})
	return out
}

// standAccepts is standOK without a live session: the pure pack-and-model half.
func standAccepts(pk *pathfind.Pack, t tile.Tile, l locreach.Loc, target tile.Tile) bool {
	if t.Level != l.Level {
		return false
	}
	if reached, modelled := locreach.Reached(t.X, t.Z, l); modelled && !reached {
		return false
	}
	if pk == nil {
		return true
	}
	if !pk.Walkable(t.X, t.Z, t.Level) {
		return false
	}
	return pk.CanInteractFrom(
		pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level},
		pathfind.NavPoint{X: target.X, Z: target.Z, Level: target.Level},
	)
}

// standOccupied reports whether another visible account is standing on t, and who.
//
// Deliberately EXACT-tile: a player one tile away is not in the way, and being shy of a
// neighbour would rule out most of a dense camp. This account itself is never counted —
// State.Players() is the list of OTHER players, and standing where we already are is
// handled by the caller's own `to == here` branch before this is reached.
func standOccupied(d *Deps, t tile.Tile) (bool, string) {
	if d == nil || d.Session == nil {
		return false, ""
	}
	return standOccupiedBy(d.Session.State().Players(), t)
}

// standOccupiedBy is standOccupied's decision with the player list passed in, so it can be
// tested without a live session. gamestate.State fills Players() from PLAYER_INFO packets and
// offers no way to inject one.
func standOccupiedBy(players []gamestate.Player, t tile.Tile) (bool, string) {
	for _, p := range players {
		if p.X == t.X && p.Z == t.Z && p.Level == t.Level {
			return true, p.Name
		}
	}
	return false, ""
}

// Routable reports whether the pack can plan a walk from here to t. Cheap at these distances:
// every caller uses it over a handful of tiles around one tree.
func Routable(pk *pathfind.Pack, from, t tile.Tile) bool {
	if pk == nil {
		return true
	}
	if from == t {
		return true
	}
	return pk.FindPath(
		pathfind.NavPoint{X: from.X, Z: from.Z, Level: from.Level},
		pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level},
	).OK
}

func standOK(d *Deps, t tile.Tile, l locreach.Loc, target tile.Tile) bool {
	if t.Level != l.Level {
		return false
	}
	if reached, modelled := locreach.Reached(t.X, t.Z, l); modelled && !reached {
		return false
	}
	if d.Pack == nil {
		return true
	}
	if !d.Pack.Walkable(t.X, t.Z, t.Level) {
		return false
	}
	return d.Pack.CanInteractFrom(
		pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level},
		pathfind.NavPoint{X: target.X, Z: target.Z, Level: target.Level},
	)
}

// ---- live reads ----

// WoodcuttingLevel is the account's live Woodcutting level.
func WoodcuttingLevel(d *Deps) int {
	skill, ok := d.Session.State().Skill(WoodcuttingStat)
	if !ok {
		return 0
	}
	return skill.Level
}

// WoodcuttingXP is the account's live Woodcutting experience.
func WoodcuttingXP(d *Deps) int {
	skill, ok := d.Session.State().Skill(WoodcuttingStat)
	if !ok {
		return 0
	}
	return skill.XP
}

func packCountByName(d *Deps, name string) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		// A NOTE IS NOT THE ITEM, and every caller of this asks a USABILITY question: can I
		// chop with it, spend it, cut it, attach it. A noted axe cannot chop, noted coins
		// cannot pay and a noted log cannot be fletched, so counting one is always a lie that
		// sends the caller down a path that cannot work.
		//
		// This is the other half of the rule bankfletchrunner.packItemID already carries, and
		// the gap between the two is a live deadlock. acct150, seen 2026-09-22 and still
		// running on 2026-09-23: it came back from an abandoned mule hand-off holding 243
		// NOTED Logs, so PlainLogsHeld returned 243, DecideStartPhase chose StartFletch on a
		// full pack, and Fletch's own note-aware id lookup then found no logs at all —
		// "fletching made no arrow shafts (no-logs)" 34 times, forever, at nearly zero xp
		// across four restarts. One helper counted the note and the other refused it, so the
		// camp could neither fletch nor conclude that it could not.
		//
		// notedCountByName is the deliberate exception, for the one caller that is asking
		// about the note itself.
		obj, ok := d.Config.Obj(int(it.ID))
		if ok && !obj.Noted() && strings.EqualFold(strings.TrimSpace(obj.Name), name) {
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// notedCountByName is how many NOTED of an item the pack holds — the mirror of
// packCountByName, for the one caller that is tracking a note rather than a usable item
// (unnoteAxe, waiting for the note it just deposited to leave the pack).
func notedCountByName(d *Deps, name string) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Noted() && strings.EqualFold(strings.TrimSpace(obj.Name), name) {
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// CoinsHeld is the player's own pack coins.
func CoinsHeld(d *Deps) int { return packCountByName(d, freshstart.Coins) }

// ContainersReadable reports whether the backpack and worn panels resolve to real components
// right now, so a caller can tell "this account is holding no axe" apart from "this client cannot
// see what it is holding". Same shape and same reason as combatrunner.ContainersReadable: content
// nulls sidebar tabs 3 and 4 in ~macro_events_set_tabs (macro_events.rs2:218), in ~set_sail
// (interface_boat/scripts/sail.rs2) and in ~set_sail_cairn/captain_shanks — and this script
// sails, twice, on its way to Seers. The two windows are nothing like the same length, and this
// comment used to say otherwise: a mime restores nothing until its mapzoneexit, while a crossing
// gets the tabs back at ~initalltabs (which re-inv_transmits and re-assigns tabs 3 and 4) before
// the arrival telejump, so that window is the route's own p_delay and NOT the whole voyage.
// Short is not the same as harmless — a WaitForGuardian return was measured with each window
// still open — so a caller that must not read "unknown" as "nothing" asks this rather than
// assuming the crossing is over.
func ContainersReadable(d *Deps) bool {
	return actions.NewEquipment(d.Session, d.Components).Readable()
}

// HeldAxes are axes that can chop right now: an unnoted axe item in the pack, or one worn.
//
// The pack half reads PlayerPack, which cannot go blind (see ContainersReadable, and
// essencerunner.HeldNames for the engine argument). The WORN half can, and it matters here
// because every axe in this build is wieldable (skill_woodcutting/configs/axes/axes.obj gives
// them wearpos=righthand and iop2=Wield) and a chopping account is normally wielding one. Read
// blind, a wielded-axe account looks axeless, and DecideWoodPhase answers that with a bank trip,
// a redundant withdrawal of a worse axe (AxeToWithdraw sees an empty hand), or PhaseOrder — which
// puts a rune-axe order on the mule bus, a cross-account action no later pass takes back.
//
// So the worn panel is only consulted when it can actually be read. Callers must not read an
// empty result as "no axe": internal/bot/woodcutter.go's snapshot refuses to build a phase
// decision at all while this is unreadable, which is what keeps the empty list from reaching
// DecideWoodPhase.
func HeldAxes(d *Deps) []string {
	var out []string
	inv := d.Session.State().Inventory(PlayerPack)
	if inv != nil {
		for _, it := range inv.NonEmpty() {
			obj, ok := d.Config.Obj(int(it.ID))
			if !ok || obj.Noted() || !woodcutplan.IsAxe(obj.Name) {
				continue
			}
			out = append(out, obj.Name)
		}
	}
	if !ContainersReadable(d) {
		return out
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	for _, it := range eq.Worn() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && woodcutplan.IsAxe(obj.Name) {
			out = append(out, obj.Name)
		}
	}
	return out
}

// RuneAxeNoted reports whether the pack holds a noted rune axe — the mule's delivery, unusable
// until the next bank visit un-notes it.
func RuneAxeNoted(d *Deps) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return false
	}
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Noted() && strings.EqualFold(strings.TrimSpace(obj.Name), woodcutplan.RuneAxe) {
			return true
		}
	}
	return false
}

// LogSlots counts pack slots holding a log of any species — logs are unstackable on this
// server, so one slot is one log, matching woodcutPlan.ts's own logSlots()/isLogs() shape.
func LogSlots(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && woodcutplan.IsLogs(obj.Name) {
			n++
		}
	}
	return n
}

// PackFull reports whether the pack holds PackSlots non-empty slots.
func PackFull(d *Deps) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	return inv != nil && len(inv.NonEmpty()) >= PackSlots
}

// FreeSlots is the pack's empty slots.
func FreeSlots(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return PackSlots
	}
	free := PackSlots - len(inv.NonEmpty())
	if free < 0 {
		return 0
	}
	return free
}

// PackView is the pack merged by name, for freshstart.SellQueue/SellPlan.
func PackView(d *Deps) []freshstart.PackItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	index := map[string]int{}
	var out []freshstart.PackItem
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		if i, seen := index[obj.Name]; seen {
			out[i].Count += c
			continue
		}
		index[obj.Name] = len(out)
		out = append(out, freshstart.PackItem{Name: obj.Name, Count: c})
	}
	return out
}

// PackSlotRows reads the pack one row per slot (not merged by name), as woodcutplan.MuleHaul/
// MuleLoad need — a merged stack of unstackable logs must not be counted as a single mule slot.
func PackSlotRows(d *Deps) []donorplan.ItemRow {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	var out []donorplan.ItemRow
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		out = append(out, donorplan.ItemRow{Name: obj.Name, ID: int(it.ID), Count: c})
	}
	return out
}

// BankItemRows reads an already-open bank's contents as donorplan.ItemRow, for
// woodcutplan.MuleRows.
func BankItemRows(d *Deps, bank *actions.Bank) []donorplan.ItemRow {
	type key struct {
		name string
		id   int
	}
	totals := map[key]int{}
	var order []key
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" {
			continue
		}
		k := key{name: obj.Name, id: int(it.ID)}
		if _, seen := totals[k]; !seen {
			order = append(order, k)
		}
		c := it.Count
		if c < 1 {
			c = 1
		}
		totals[k] += int(c)
	}
	out := make([]donorplan.ItemRow, 0, len(order))
	for _, k := range order {
		out = append(out, donorplan.ItemRow{Name: k.name, ID: k.id, Count: totals[k]})
	}
	return out
}

// BankLogTotal counts logs of EVERY species in an already-open bank.
//
// This exists because counting only the TARGET species is wrong for anything that watches the
// registered bot rather than a single hand-driven tier: the bot chops CurrentTier, which is the
// ladder's own current rung (gated on Tier.StartAt), not TargetTier. An account at Woodcutting
// 15 with target Oak is correctly chopping NORMAL trees — Oak's StartAt is 25 — so a watcher
// counting "Oak logs" reads a flat zero while the bank fills with plain Logs. That exact
// mistake made cmd/woodcuttest's own -soak verdict report FAIL ("it chopped without ever
// banking") for a run that had in fact banked 99 logs, on 2026-09-20.
func BankLogTotal(d *Deps, bank *actions.Bank) int {
	n := 0
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !woodcutplan.IsLogs(obj.Name) {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		n += c
	}
	return n
}

// BankAxes is every distinct axe name currently in an already-open bank.
func BankAxes(d *Deps, bank *actions.Bank) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" || seen[obj.Name] || !woodcutplan.IsAxe(obj.Name) {
			continue
		}
		seen[obj.Name] = true
		out = append(out, obj.Name)
	}
	return out
}

// BankTargetLogs counts logs of the target species held in an already-open bank.
func BankTargetLogs(d *Deps, bank *actions.Bank, targetLogs string) int {
	n := 0
	for _, it := range bank.Items() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(strings.TrimSpace(obj.Name), targetLogs) {
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// ---- the Seers bank ----

// openNearestBankBooth finds every "Bank booth" candidate near (x,z,level) and tries them
// nearest-first — duplicated per this project's established convention (see
// essencerunner.openNearestBankBooth's own doc comment for the live bake-artifact bug this
// nearest-first retry recovers from).
func openNearestBankBooth(d *Deps, x, z, level int, timeout time.Duration) (*actions.Bank, bool) {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: "Bank booth", Op: "Use-quickly"}, live)
	if len(candidates) == 0 {
		d.logf("could not find a bank booth near the bank")
		return nil, false
	}
	const maxTries = 4
	for i, booth := range candidates {
		if i >= maxTries {
			d.logf("gave up after %d bank booth candidates near %d,%d", maxTries, x, z)
			return nil, false
		}
		if !walkToInteract(d, tile.Tile{X: booth.X, Z: booth.Z, Level: booth.Level}) {
			continue
		}
		mark := actions.MarkBankOpen(d.Session)
		if err := actions.OpLoc(d.Session, booth.Op, booth.X, booth.Z, booth.ID); err != nil {
			d.logf("OpLoc: %v", err)
			continue
		}
		bank := actions.NewBank(d.Session, d.Components)
		if open, _ := bank.AwaitOpen(mark, actions.DelayWaiter(d.Delay), timeout); open {
			return bank, true
		}
	}
	return nil, false
}

// OpenSeersBank walks to woodcutplan.Seers and opens its booth, leaving it open for the caller
// to drive its own deposit/withdraw sequence.
func OpenSeersBank(d *Deps) (*actions.Bank, bool) {
	return OpenBankAt(d, woodcutplan.Seers)
}

// OpenBankAt is OpenSeersBank for any bank: it walks to within 3 tiles of at and opens the nearest
// booth there (a "Bank booth" with Use-quickly within 15 tiles), leaving it open. ProgressiveWCFletch
// opens the bank of the camp it is seated at with it (wcfletchplan.Camp.Bank), which is the Seers
// bank for every camp today.
func OpenBankAt(d *Deps, at tile.Tile) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	if !WalkNear(d, at, 3) {
		return nil, false
	}
	x, z, level, _ := d.Session.State().Tile()
	bank, ok := openNearestBankBooth(d, x, z, level, 20*time.Second)
	if !ok {
		return nil, false
	}
	return bank, true
}

// DepositAllExcept deposits every pack item not named in keep (case-insensitive) into an
// already-open bank and returns how many kinds went in. Every kind goes out in one burst, five
// Deposit-Alls to a game tick, confirmed once, with the one-at-a-time path behind it for a kind the
// burst did not bank (twopertick-1012: this was a Deposit-All and a wait for the bank to show it, per
// kind).
func DepositAllExcept(d *Deps, bank *actions.Bank, keep []string) int {
	kept := make(map[string]bool, len(keep))
	for _, k := range keep {
		kept[strings.ToLower(strings.TrimSpace(k))] = true
	}
	var ops []actions.BankOp
	seen := map[int32]bool{}
	for _, it := range bank.Inventory() {
		if seen[it.ID] {
			continue
		}
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || kept[strings.ToLower(obj.Name)] {
			continue
		}
		seen[it.ID] = true
		ops = append(ops, actions.DepositAllOp(it.ID))
	}
	if len(ops) == 0 {
		return 0
	}
	r, retried := bank.Visit(ops, Waiter(d), actions.BatchOptions{})
	d.logf("%s", actions.VisitLog("woodcut deposit", len(ops), r, retried))
	if r.Err != nil {
		return 0
	}
	return len(ops) - len(r.Short)
}

func packCountByID(items []gamestate.SlotItem, id int32) int32 {
	var n int32
	for _, it := range items {
		if it.ID == id {
			n += it.Count
		}
	}
	return n
}

func bankCountByID(items []gamestate.SlotItem, id int32) int32 {
	var n int32
	for _, it := range items {
		if it.ID == id {
			n += it.Count
		}
	}
	return n
}

// WithdrawOneOf withdraws exactly one of name from an already-open bank into the pack, waiting
// for the pack to reflect it.
func WithdrawOneOf(d *Deps, bank *actions.Bank, name string) bool {
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !strings.EqualFold(obj.Name, name) {
			continue
		}
		before := packCountByID(bank.Inventory(), it.ID)
		if err := bank.WithdrawOne(it.ID); err != nil {
			d.logf("withdraw one %q: %v", name, err)
			return false
		}
		return waitFor(d, fmt.Sprintf("%s in pack after withdrawing", name), 3*time.Second, func() bool {
			return packCountByID(bank.Inventory(), it.ID) > before
		})
	}
	return false
}

// waiter adapts a Deps' Delayer to actions.Waiter.
type waiter struct{ d *Deps }

func (w waiter) Delay(t time.Duration) bool { return w.d.Delay(t) }
func (w waiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		if !w.d.Delay(100 * time.Millisecond) {
			return false
		}
	}
}
func (w waiter) DelayTicks(n int) bool { return w.d.Delay(time.Duration(n) * 600 * time.Millisecond) }

// Waiter is d as an actions.Waiter.
func Waiter(d *Deps) actions.Waiter { return waiter{d} }

// SetNoteMode sets the open bank's withdraw mode.
func SetNoteMode(d *Deps, bank *actions.Bank, on bool) error {
	return bank.SetNoteMode(on, Waiter(d))
}

// BankRowID is the open bank's first un-noted row named name (the row a Withdraw names), if it holds any.
func BankRowID(d *Deps, bank *actions.Bank, name string) (int32, bool) {
	for _, it := range bank.Items() {
		if it.Count <= 0 {
			continue
		}
		if obj, ok := d.Config.Obj(int(it.ID)); ok && !obj.Noted() && strings.EqualFold(strings.TrimSpace(obj.Name), strings.TrimSpace(name)) {
			return it.ID, true
		}
	}
	return 0, false
}

// WithdrawAllByName withdraws every one of name with Withdraw-All and waits until the pack
// holds more of that name — counts by name (not id) so it confirms a withdrawal that lands as a
// note in note mode.
func WithdrawAllByName(d *Deps, bank *actions.Bank, name string) bool {
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !strings.EqualFold(obj.Name, name) {
			continue
		}
		before := packCountByName(d, name)
		used := len(bank.Inventory())
		if err := bank.WithdrawAll(it.ID); err != nil {
			d.logf("withdraw all %q: %v", name, err)
			return false
		}
		return waitFor(d, fmt.Sprintf("%s in pack after withdrawing", name), 3*time.Second, func() bool {
			return packCountByName(d, name) > before || len(bank.Inventory()) > used
		})
	}
	return false
}

// ---- the general store ----

// StoreResult is one store pass's outcome.
type StoreResult struct {
	Opened   bool
	Sold, GP int
}

// StoreTrip sells everything the pack holds beyond keep at the Lumbridge general store —
// ported from ProgressiveWoodcutter.ts's own storeTrip(), which never buys anything (an axe
// has no Woodcutting requirement on this server, so even the bronze starter axe is sold).
func StoreTrip(d *Deps, stand tile.Tile, keep []string, refused map[string]bool) StoreResult {
	var res StoreResult
	here := currentTile(d)
	if tile.Chebyshev(here, stand) > 3 {
		if !WalkNear(d, stand, 2) {
			d.logf("could not reach the general store — retrying")
		}
		return res
	}
	shop := openShop(d, freshstart.ShopKeepers(), stand)
	if shop == nil {
		d.logf("could not open the general store (keepers: %s) — retrying", strings.Join(freshstart.ShopKeepers(), " / "))
		d.Delay(2 * time.Second)
		return res
	}
	res.Opened = true
	defer shop.Close()
	w := Waiter(d)
	for _, line := range freshstart.SellQueue(PackView(d), keep, refused) {
		id, found := itemInStock(d, shop.Pack(), line.Name)
		if !found {
			// An item the shop does not stock is refused just as surely as one it declines,
			// and until 2026-09-22 only the second was recorded — so this branch dropped the
			// line silently, Sellable never fell, and DecideWoodPhase went on returning
			// PhaseStore forever.
			//
			// Live that day: acct155 logged `sold the starter kit: 0 items for 0 gp (24
			// coins)` followed by `walk to 3209,3247,0`, over and over, 200 times in the log
			// tail at roughly one round every two seconds, for the whole run at 0 xp. Its
			// sell queue held Arrow shafts, Leather gloves, Leather boots and Seaweed; the
			// Lumbridge general store's stock that moment was Pot, Jug, Shears, Bucket,
			// Tinderbox, Chisel, Hammer and Newcomer map. Nothing in the queue could ever
			// match, and nothing recorded that.
			//
			// Guarded on the stock actually having loaded: an empty shop pane is "I do not
			// know yet", not "it stocks nothing", and writing the whole queue off on a pane
			// that has not populated would turn this livelock into a silent refusal to sell
			// anything at all — the same bug facing the other way.
			//
			// This does NOT decide whether a general store would buy an unstocked item if
			// asked by pack id rather than by shop id (in this content it generally would,
			// adding the item to its stock). That is a real question and worth measuring,
			// but it is a behaviour change to what the account sells, not this loop's fix.
			if storeShouldRecordRefusal(len(shop.Pack())) {
				refused[strings.ToLower(line.Name)] = true
			}
			continue
		}
		before := packCountByName(d, freshstart.Coins)
		sold, err := shop.SellCount(id, int32(line.Count), w)
		d.Delay(600 * time.Millisecond)
		gained := packCountByName(d, freshstart.Coins) - before
		if err != nil || freshstart.Refused(int(sold), gained) {
			refused[strings.ToLower(line.Name)] = true
			continue
		}
		res.Sold += int(sold)
		res.GP += gained
	}
	return res
}

// storeShouldRecordRefusal reports whether a sell line the shop's stock does not contain may
// be written off, given how many rows that stock pane actually showed.
//
// Its own function so the rule is testable without a session, and so the distinction it
// carries cannot be lost in a later edit: a LOADED pane that lacks the item is a real refusal
// and has to be recorded or StoreTrip loops on it for ever; an EMPTY pane is "not known yet"
// and recording it would quietly stop the account ever selling anything.
func storeShouldRecordRefusal(stockRows int) bool { return stockRows > 0 }

func itemInStock(d *Deps, items []gamestate.SlotItem, name string) (int32, bool) {
	for _, it := range items {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && !obj.Noted() && strings.EqualFold(strings.TrimSpace(obj.Name), name) && it.Count > 0 {
			return it.ID, true
		}
	}
	return 0, false
}

func openShop(d *Deps, names []string, near tile.Tile) *actions.Shop {
	shop := actions.NewShop(d.Session)
	for _, name := range names {
		npc, ok := nearestNpc(d, name, near, 10)
		if !ok {
			continue
		}
		WalkNear(d, tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}, 1)
		npcType, _ := d.Config.Npc(npc.Type)
		op := cachecfg.OpIndex(npcType.Ops, "Trade")
		if op == 0 {
			op = 3
		}
		if err := actions.OpNpc(d.Session, op, npc.Index); err != nil {
			continue
		}
		if waitFor(d, name+"'s shop to open", 5*time.Second, shop.IsOpen) {
			return shop
		}
	}
	return nil
}

func nearestNpc(d *Deps, name string, near tile.Tile, radius int) (gamestate.NPC, bool) {
	var best gamestate.NPC
	bestDist, found := radius+1, false
	for _, n := range d.Session.State().NPCs() {
		t, ok := d.Config.Npc(n.Type)
		if !ok || !strings.EqualFold(t.Name, name) {
			continue
		}
		if dist := tile.Chebyshev(near, tile.Tile{X: n.X, Z: n.Z, Level: n.Level}); dist <= radius && dist < bestDist {
			best, bestDist, found = n, dist, true
		}
	}
	return best, found
}

// ---- chopping ----

// ChopResult is one Chop call's outcome.
type ChopResult struct {
	// Gained is log slots caught during this call.
	Gained int
	// Reason is why the session ended: "no-tree", "all-blocked", "unreachable", "cut-off",
	// "ent", "full", "stalled", "dialog", "moved", or "stopped".
	//
	// "all-blocked" means trees were in reach and every one is excluded — Excluded says why.
	// "cut-off" means the account has no route to the camp's anchor or to any stand tile of the
	// tree it picked: it is on the wrong side of a wall, and the answer is ReturnToCamp, not a
	// wait. "no-tree" means there was nothing of the species in reach at all, whatever the
	// exclusion set holds.
	Reason string
	// Excluded is the tally of the candidates in reach, by exclusion kind, on "all-blocked".
	Excluded woodcutplan.BlockTally
}

// ChopDiag makes Chop log the loc id, the tile and the stand tile of every click it sends.
//
// OFF in production and meant to stay that way — one line per tree across a 269-account fleet is
// noise — but a diagnostic worth having wired rather than re-added by hand each time, because a
// chop that silently does nothing is indistinguishable from a chop that is merely slow, and
// CLAUDE.md's OpLocU gotcha lists four different ordinary reasons for the first. Two of them
// (no loc of that TYPE at that tile server-side; a stand tile the engine's reachedLoc refuses)
// can only be told apart from the id and the tile the click actually carried.
var ChopDiag bool

// chopStallTimeout: no new log AND no swing for this long with the tree still standing means the
// swing stopped for some reason the client can't see (an Ent taking its place, a queued interrupt) —
// re-click rather than hold forever, matching essenceplan.MineStallTimeout's own role.
//
// A swing counts (chopmanipplan.Swinging, read from the animation the server starts on the account)
// because a tree can be slow without being stuck. At Woodcutting 60 a yew gives a log in about one
// swing in six, so more than a third of the fresh chops at one went a quarter of a minute without a
// log: each of those was read as "the tree gave nothing", the tree was excluded for
// UnreachableBlockFor, and an account at the church camp's three yews found "every yew within reach
// is excluded" and cut the one log it held into a longbow (acct149, 6 October 2026, live). The wedge
// this rule exists for has no swings in it: the click leaves the client and the server does nothing.
const chopStallTimeout = 15 * time.Second

const chopTickInterval = 600 * time.Millisecond

// Chop finds the nearest tree of tier within its own leash, clicks it, and holds the click
// until the pack fills, no log lands for chopStallTimeout, a dialog interrupts (a level-up), or
// the tree itself is gone (chopped down by someone else, or this account walked off) — ported
// from ProgressiveWoodcutter.ts's own chopStep(), minus the TS's own Game.animating() check
// (no equivalent live signal in this client; a stall timeout serves the identical purpose, the
// same trade essencerunner.Mine already makes for the essence rock).
func Chop(d *Deps, tier woodcutplan.Tier) ChopResult {
	m, pick, excluded, ok := pickTree(d, tier)
	if !ok {
		if excluded.Total() > 0 {
			// Every candidate IN REACH is excluded. The caller must NOT fall back to the nearest
			// one: when the reason is an Ent, that is exactly the click that puts the axe into
			// it. The exclusions expire on their own; the line says which kind they are, because
			// the answer to "every tree is excluded" depends on it and this line used to blame an
			// Ent for all of them (see woodcutplan.BlockReason).
			d.logf("every %s within reach is excluded (%s; the first lifts in %s) — not chopping one",
				strings.ToLower(tier.Loc), excluded, liftsIn(excluded, time.Now()))
			return ChopResult{Reason: "all-blocked", Excluded: excluded}
		}
		d.logf("no %s in sight — waiting for one to grow back", strings.ToLower(tier.Loc))
		return ChopResult{Reason: "no-tree"}
	}
	treeTile := tile.Tile{X: m.X, Z: m.Z, Level: m.Level}
	// The claim this account just took (if any) covers exactly the walk and the chop, which is
	// the contended window: the whole loss this de-confliction exists to stop is N accounts
	// WALKING to one tree that only has one log in it. keepClaim survives the release for the
	// two outcomes that mean "I am coming straight back to this same tree" — everything else,
	// including the bank trip a full pack starts, hands it back rather than making the next
	// account wait out woodcutplan.TreeClaimTTL for a tree nobody is standing at.
	keepClaim := false
	defer func() {
		if !keepClaim {
			releaseTree(d, treeTile)
		}
	}()
	if ChopDiag && pick.Shared {
		d.logf("every %s in reach (%d) is claimed by another account — doubling up on the one at %d,%d",
			strings.ToLower(tier.Loc), pick.Claimed, treeTile.X, treeTile.Z)
	}
	if ap := walkToInteractLoc(d, m); !ap.Reached {
		// IS IT THE TREE, OR IS IT US? When no stand tile of the tree has a route from here AND
		// neither does the camp's own anchor, the account is on the wrong side of a wall, and
		// excluding the tree is the wrong answer: every tree in the camp then reads the same,
		// each is excluded in turn, and the account waits for exclusions whose cause — where it
		// is standing — never changes.
		//
		// MEASURED live, 2026-09-23, build 3dfb543a: Acct212 spent 21.9 minutes at 2730,3478,
		// inside the walled 252-tile building south of the Seers normal camp whose only exit is
		// the Large door pair at 2736-2737,3477, at zero xp, logging "every tree within reach has
		// an Ent at it" 658 times. All 15 trees in reach have their stand tiles outside, and the
		// lab reproduced it exactly (cmd/treetraptest). The static pack bakes that door shut, so
		// no plain FindPath gets out, open or not; ReturnToCamp goes through the door-aware
		// walker, which plans the door hop.
		//
		// Checked only on the one shape that can mean it (every legal tile unroutable, nothing
		// walked at), so the extra A* is paid only where a tree was already going to be excluded.
		// From inside the camp's main region the anchor is always routable (cmd/woodanchortest
		// validates every anchor), so an ordinary unreachable tree is still excluded as before.
		if here := currentTile(d); ap.noRouteToAny() && !Routable(d.Pack, here, tier.Anchor) {
			d.logf("cut off from the %s camp at %d,%d: no route from %d,%d to the camp's anchor or to any of the %d tile(s) the %s at %d,%d can be chopped from — not excluding it, walking back to the camp",
				strings.ToLower(tier.Label), tier.Anchor.X, tier.Anchor.Z, here.X, here.Z, ap.Legal, strings.ToLower(tier.Loc), treeTile.X, treeTile.Z)
			return ChopResult{Reason: "cut-off"}
		}
		// Exclude it briefly, or pickTree chooses the same unreachable tree every pass and the
		// account stands there for the rest of the run. MEASURED at the Draynor camp, where the
		// trees are dense enough that all twelve of a tree's candidate stand tiles can be blocked
		// by its neighbours: "no tile the engine accepts an interaction with loc 1282 at
		// 3088,3345 from could be reached (tried 0 of 12 candidates)", three passes running, on
		// an account that had just chopped two logs perfectly well one tree over.
		if d.Blocks != nil {
			d.Blocks.BlockBecause(treeTile, time.Now(), woodcutplan.UnreachableBlockFor, woodcutplan.BlockUnreachable)
		}
		return ChopResult{Reason: "unreachable"}
	}
	// Which loc, by id and tile, and from where. A chop that produces nothing is otherwise
	// indistinguishable from a chop that is merely slow, and the four things that make a loc
	// interaction silently do nothing (CLAUDE.md's OpLocU gotcha: no loc of that TYPE at that
	// tile server-side, a stand tile the engine's reachedLoc refuses, a content precondition,
	// or an invisible backpack component) can only be told apart if the id and the tile the
	// click actually carried are on the record. Verbose only — one line per click.
	if ChopDiag {
		d.logf("chopping %s id %d at %d,%d,%d from %v (op %q)", strings.ToLower(tier.Loc), m.ID, m.X, m.Z, m.Level, currentTile(d), m.Op)
	}
	res, keep := holdChop(d, tier, m, pick, func() bool {
		return treeStillThere(d, m.X, m.Z, m.Level, tier.Loc, woodcutplan.ChopOp)
	})
	keepClaim = keep
	return res
}

// holdChop clicks the tree m and holds the click until the pack fills, no log lands for chopStallTimeout, a chat it cannot answer
// interrupts, or the tree itself is gone. It returns the result and whether the account's claim on the tree is still wanted (the
// caller's own bookkeeping, see keepClaim in Chop). stillThere says whether the tree is still standing (Chop reads the loc index
// for it). Split out of Chop so the hold can be driven in a test without a loc index, a collision pack and a walk.
func holdChop(d *Deps, tier woodcutplan.Tier, m locindex.Match, pick woodcutplan.TreePick, stillThere func() bool) (ChopResult, bool) {
	// The opening click is a knife-on-log batch when the chop manipulation is on, the controller wants one and a carrier
	// log and a knife are held (chopmanip.go); otherwise it is the plain click this function always sent, through the click
	// gate (actions.HoldChat): the same tree is not clicked more than once per actions.ClickGapTicks game ticks, however often
	// this is called and whatever chat is up.
	treeTile := tile.Tile{X: m.X, Z: m.Z, Level: m.Level}
	keepClaim := false
	hold := actions.NewHoldChat(d.Session, actions.DelayWaiter(d.Delay), d.Components, actions.LocKey(m.X, m.Z, int(m.ID)), func() error {
		return actions.OpLoc(d.Session, m.Op, m.X, m.Z, m.ID)
	})
	hold.Busy = func() bool { return randomevents.Busy(d.Session) }
	hold.Held = func(ticks int) {
		d.logf("chop: holding the next click on the %s for %d tick(s): the server reads five clicks a tick", strings.ToLower(tier.Loc), ticks)
	}
	// click clicks the tree once the gap allows and returns "" or why it did not.
	click := func() string {
		if err := hold.Click(); err != nil {
			if errors.Is(err, actions.ErrClickStopped) {
				return "stopped"
			}
			d.logf("OpLoc(%s) on the %s: %v", woodcutplan.ChopOp, tier.Loc, err)
			return "no-tree"
		}
		return ""
	}
	mp := startManip(d, m)
	if mp != nil && mp.open() {
		hold.Clicked() // the batch carried the click
	} else if why := click(); why != "" {
		if mp != nil {
			mp.watch.stop()
		}
		return ChopResult{Reason: why}, false
	}
	// What this tree yields, for the Loot panel's Woodcutting card (see chopcount.go).
	// Reporting only; the window spans exactly this call.
	chop := beginChop(d, int(m.ID), tier.Loc)
	logs := LogSlots(d)
	gained := 0
	hold.OnAnswer = func(chat int) {
		d.logf("chop: answered chat %d (a level-up or a message box) with its Continue after %d log(s)", chat, gained)
	}
	if mp != nil {
		defer mp.finish(&gained)
	}
	// lastAlive is when the account last got a log or was seen swinging; chopStallTimeout counts from it.
	lastAlive := time.Now()
	lastRefresh := time.Now()
	st := d.Session.State()
	for {
		// Keep the claim alive while the swing is still ours. An oak or a willow survives about
		// eight logs (trees.dbrow: respawnrate 30 with the 1/8 deplete roll), so this loop can
		// legitimately hold one tree for longer than woodcutplan.TreeClaimTTL; without the
		// refresh the tree would come free under a working account. The TTL stays short anyway,
		// because the case it really has to cover is the account that stops refreshing because
		// it died, disconnected or was teleported out mid-walk.
		if pick.Mine && time.Since(lastRefresh) >= woodcutplan.TreeClaimRefresh {
			refreshTree(d, treeTile)
			lastRefresh = time.Now()
		}
		now := LogSlots(d)
		if now > logs {
			chop.Got(now - logs)
			gained += now - logs
			logs = now
			lastAlive = time.Now()
		} else if mp != nil && now < logs {
			// A batch that was split by the engine's per-tick limit fletches the carrier log: the count fell, and the
			// baseline must fall with it or the logs that follow would not be counted.
			logs = now
		}
		if PackFull(d) {
			return ChopResult{Gained: chop.Done(gained), Reason: "full"}, keepClaim
		}
		// A chat: the click's own to close, a level-up to answer here (actions.HoldChat), or one to hand back. The account comes
		// straight back to this tree, so the claim is held across a hand-back rather than given to somebody walking over.
		switch hold.Step() {
		case actions.HoldClick:
			if why := click(); why != "" {
				return ChopResult{Gained: chop.Done(gained), Reason: why}, keepClaim
			}
		case actions.HoldHandBack:
			keepClaim = pick.Mine
			return ChopResult{Gained: chop.Done(gained), Reason: "dialog"}, keepClaim
		}
		if seq, at := st.SelfAnim(); chopmanipplan.Swinging(seq, at, st.Tick()) {
			lastAlive = time.Now()
		}
		if time.Since(lastAlive) > chopStallTimeout {
			// A stall that produced NOTHING is the shape of every wedge this script has had: the
			// click leaves the client, the server does nothing it can see, pickTree chooses the
			// same nearest tree next pass, and the account stands there for the rest of the run.
			// MEASURED twice on the lab engine, two independent accounts, the same tree at
			// 2732,3481 and the same tile: over three minutes each at zero xp. Excluding the
			// tree for UnreachableBlockFor makes the next pass pick a different one, and the
			// exclusion expires on its own.
			//
			// Only when it gained nothing AT ALL. A tree that gave a log and then went quiet is
			// an ordinary depleting tree and must not be blocked. The cost of the rule when it
			// is wrong is 30 seconds on one tree out of 12 (Seers) or 56 (Draynor); the cost of
			// not having it was measured above.
			if gained == 0 && d.Blocks != nil {
				d.Blocks.BlockBecause(treeTile, time.Now(), woodcutplan.UnreachableBlockFor, woodcutplan.BlockStall)
				if ChopDiag {
					d.logf("%s at %d,%d gave nothing in %s — excluding it for %s and picking another",
						strings.ToLower(tier.Loc), treeTile.X, treeTile.Z, chopStallTimeout, woodcutplan.UnreachableBlockFor)
				}
			}
			// A tree that HAS been giving logs and went quiet is an ordinary depleting tree
			// this account will re-click next pass (the branch above deliberately does not
			// block it), so its claim is still live work. One that gave nothing was just
			// blocked for UnreachableBlockFor and will not be picked again, so let it go.
			keepClaim = pick.Mine && gained > 0
			return ChopResult{Gained: chop.Done(gained), Reason: "stalled"}, keepClaim
		}
		// An Ent at THIS tree. The fleet owner's requirement, in his own words: "as soon as ent
		// shows, swap to a tree without an ent." Returning immediately is what does that — the
		// caller's next Chop call clicks a different tree, and that click is the only thing
		// that takes the player off the Ent, because the ENGINE put them on it (see
		// macro_event_ent_spawn's closing p_opnpc(1), and cut_ent's own p_opnpc(3) at the end
		// of every damage cycle). This client never sends OpNpc at a tree, so there is no click
		// of ours to withhold; speed of the NEXT action is the whole defence.
		if ent, at := entAtTree(d, treeTile); at {
			if d.Blocks != nil {
				d.Blocks.Block(treeTile, time.Now())
			}
			// A random event: the engine has already put the account on it, so whatever
			// the pack does from here is the event's business, not the tree's.
			chop.Taint()
			d.logf("an Ent (npc %d) is standing at the %s at %d,%d — leaving it alone for %s and switching trees",
				ent, strings.ToLower(tier.Loc), treeTile.X, treeTile.Z, woodcutplan.EntBlockFor)
			return ChopResult{Gained: chop.Done(gained), Reason: "ent"}, keepClaim
		}
		// Confirm the tree loc is still there (this account's own stump/felled state, not just
		// "some tree of this species somewhere") — a fallen tree respawns as a different loc, so
		// this looks the same instance up by exact tile rather than by name+op again.
		if !stillThere() {
			return ChopResult{Gained: chop.Done(gained), Reason: "stalled"}, keepClaim
		}
		if mp != nil {
			if mp.pace() {
				return ChopResult{Gained: chop.Done(gained), Reason: "stopped"}, keepClaim
			}
			continue
		}
		if !d.Delay(chopTickInterval) {
			return ChopResult{Gained: chop.Done(gained), Reason: "stopped"}, keepClaim
		}
	}
}

// pickTree chooses which tree to chop: the nearest one that is not currently excluded, has no
// Ent standing on it, and that no OTHER ACCOUNT in this process has already reserved.
//
// This replaces a bare Locs.Nearest, for two reasons that are worth keeping apart.
//
// The first is the Ent: when the nearest tree has one at it, "nearest" is the wrong answer, and
// chopping it is what breaks the axe. Any tree found with an Ent on it is excluded here as well
// as skipped, so the next pass does not have to rediscover it.
//
// The second is the crowd, and it is why this returns a woodcutplan.TreePick rather than a bare
// match. Ninety-four accounts run this script, most of them packed into two small camps, and a
// normal tree gives exactly ONE log before it becomes a stump — so ninety-three of ninety-four
// nearest-first clicks are a walk to a tree that will be gone on arrival.
// woodcutplan.PickTreeFor takes an atomic claim on the chosen tree so the accounts fan out
// across the camp instead, and falls back to chopping a claimed tree (never to waiting) when
// there are more accounts than trees, which at these camps is the normal state.
//
// THE STUMPS ARE ALREADY HANDLED, one layer down, and that is worth knowing before adding a
// second mechanism for it: the tree the server felled arrives as a live loc change, and
// locindex.Find masks the static loc on any tile/layer a live change covers, so a felled tree
// simply is not in `all`. That is this account's OWN observation — an account that was not in
// the zone when the change arrived does not have it, which is exactly the gap the claims close
// from the other side.
//
// When nothing can be picked, the tally says why, counted over THIS pass's candidates only (see
// woodcutplan.TreeBlocks.Tally): a zero tally with ok=false means there was no tree of the species
// in reach at all, which the caller reports as "no-tree" whatever else the exclusion set holds.
func pickTree(d *Deps, tier woodcutplan.Tier) (locindex.Match, woodcutplan.TreePick, woodcutplan.BlockTally, bool) {
	here := currentTile(d)
	radius := tier.Leash + 2
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	all := d.Locs.Find(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: tier.Anchor.Level, Radius: radius, Name: tier.Loc, Op: woodcutplan.ChopOp}, live)
	if len(all) == 0 {
		return locindex.Match{}, woodcutplan.TreePick{}, woodcutplan.BlockTally{}, false
	}
	now := time.Now()
	if d.Blocks != nil {
		d.Blocks.Sweep(now)
	}
	byTile := make(map[tile.Tile]locindex.Match, len(all))
	trees := make([]tile.Tile, 0, len(all))
	cands := make([]woodcutplan.TreeCandidate, 0, len(all))
	ents := 0
	for _, m := range all {
		at := tile.Tile{X: m.X, Z: m.Z, Level: m.Level}
		if _, seen := byTile[at]; seen {
			continue
		}
		byTile[at] = m
		trees = append(trees, at)
		if npc, ent := entAtTree(d, at); ent {
			ents++
			if d.Blocks != nil {
				// Said once, when the exclusion is new — this branch runs every pass the Ent is in
				// view, and it used to be silent, which left the log unable to say whether an
				// "every tree is excluded" line was ever about an Ent at all.
				if why, _, blocked := d.Blocks.Why(at, now); !blocked || why != woodcutplan.BlockEnt {
					d.logf("an Ent (npc %d) is standing at the %s at %d,%d — excluding it for %s",
						npc, strings.ToLower(tier.Loc), at.X, at.Z, woodcutplan.EntBlockFor)
				}
				d.Blocks.Block(at, now)
			}
			continue
		}
		cands = append(cands, woodcutplan.TreeCandidate{At: at, Distance: m.Distance})
	}
	pick, ok := woodcutplan.PickTreeFor(cands, d.Blocks, treeClaims(d), claimant(d), now)
	if !ok {
		tally := d.Blocks.Tally(trees, now)
		if d.Blocks == nil {
			// No exclusion set to count, but the Ents skipped above are still why.
			tally.Ent = ents
		}
		return locindex.Match{}, woodcutplan.TreePick{}, tally, false
	}
	return byTile[pick.Tree.At], pick, woodcutplan.BlockTally{}, true
}

// liftsIn is how long until the first exclusion in t lifts, for a log line.
func liftsIn(t woodcutplan.BlockTally, now time.Time) time.Duration {
	if t.Lifts.IsZero() || !t.Lifts.After(now) {
		return 0
	}
	return t.Lifts.Sub(now).Round(time.Second)
}

// DeconflictTrees switches cross-account tree de-confliction off, restoring the plain
// nearest-first pick this package had before it.
//
// ON by default and meant to stay that way. It exists for two reasons, both of them about
// evidence rather than about doubt: a fleet operator needs a way to take a behaviour change out
// without rebuilding and redeploying, and a harness needs to be able to measure BOTH arms of an
// A/B on one binary against one engine, rather than comparing a run on today's build with a run
// on last week's and calling the difference the change. Same shape and same reasoning as
// ChopDiag above.
var DeconflictTrees = true

// treeClaims is the registry this account de-conflicts through. Nil means the process-wide one
// — see Deps.Claims for why nil is the shared registry rather than "off".
func treeClaims(d *Deps) *woodcutplan.TreeClaims {
	if !DeconflictTrees {
		return nil
	}
	if d.Claims != nil {
		return d.Claims
	}
	// Per WORLD. A tree claim only means something between accounts that can contend for the
	// tree, and accounts on different worlds cannot. See SharedTreeClaimsFor.
	return woodcutplan.SharedTreeClaimsFor(d.World)
}

// claimant is this account's identity in the claim registry.
//
// THE SERVER-ASSIGNED PLAYER ID, not the display name, and the choice matters. Deps carries a
// session and not a username (adding one would mean editing internal/bot/woodcutter.go, which
// another session holds), so the identity has to come off the wire, and there are two
// candidates:
//
//   - UPDATE_PID, `this.write(new UpdatePid(this.slot, ...))` in the engine's own login
//     sequence — written unconditionally, before the login trigger runs, so it is there from
//     the account's first tick. `slot` is its index among everyone on the world, so it is
//     unique by construction.
//   - The display name, which rides the local player's APPEARANCE update and therefore arrives
//     LATER. Using it would mean an account claims trees as "pid#12" for its first seconds and
//     as "Bob" afterwards, and its own earlier claim would then look like somebody else's and
//     sit there until woodcutplan.TreeClaimTTL. An identity that changes mid-run is worse than
//     an unreadable one, and nothing logs this string.
//
// Returns "" only before UPDATE_PID has arrived, which is before the account is in the game at
// all; woodcutplan.PickTreeFor then falls through to plain nearest-first, the behaviour this
// package had before.
func claimant(d *Deps) string {
	if d == nil || d.Session == nil {
		return ""
	}
	if pid := d.Session.State().Self(); pid >= 0 {
		return "pid#" + itoa(pid)
	}
	return ""
}

// refreshTree re-stamps this account's claim while it is still working the tree.
func refreshTree(d *Deps, at tile.Tile) {
	c := treeClaims(d)
	if c == nil {
		return
	}
	if me := claimant(d); me != "" {
		c.Claim(at, me, time.Now())
	}
}

// releaseTree hands a tree back. A release from an account that does not hold it is a no-op, so
// this is safe to call unconditionally — including on a pick that was a deliberate double-up on
// somebody else's tree.
//
// THE NIL CHECK IS LOAD-BEARING and it is here because it was missing. With DeconflictTrees off
// treeClaims returns nil, Chop's deferred release called Release on it, and the nil-pointer
// panic was swallowed by internal/bot/runner.go's recover(): every account logged an ordinary
// "stopped" and the A/B arm that was supposed to reproduce the old behaviour came back at 25
// cohort xp against the 3,624 a real build of origin/main produced on the same engine minutes
// later. woodcutplan.TreeClaims now tolerates a nil receiver as well, so neither layer alone
// can bring that back.
func releaseTree(d *Deps, at tile.Tile) {
	c := treeClaims(d)
	if c == nil {
		return
	}
	if me := claimant(d); me != "" {
		c.Release(at, me)
	}
}

// entAtTree names the Ent standing at a tree, for the log line. The "is there one" question
// itself is EntAt's (events.go), which is the shared answer; this only adds the type id so the
// account can say which Ent it walked away from.
//
// Matching is by npc TYPE ID, never by name: every Ent is named after the tree it replaces, so
// a name check would match the very thing it is meant to exclude. See woodcutplan.IsEntNpc.
func entAtTree(d *Deps, at tile.Tile) (int, bool) {
	for _, n := range d.Session.State().NPCs() {
		if !woodcutplan.IsEntNpc(n.Type) || n.Level != at.Level {
			continue
		}
		if tile.Chebyshev(tile.Tile{X: n.X, Z: n.Z, Level: n.Level}, at) <= 1 {
			return n.Type, true
		}
	}
	return 0, false
}

// StepAwayFrom sends one real walk back toward the tier's own anchor, as the interrupt of last
// resort when an Ent is present and there is no other tree to click.
//
// Why a walk and not a wait: an account that sends nothing stays on the Ent, and the Ent keeps
// chopping back. Walk is the cheapest packet that clears a server-side interaction, and the
// anchor is always a sensible place to be heading anyway. It is deliberately NOT WalkNear — a
// two-minute pathfinding walk is not an interrupt — just one walk, sent and not waited on.
//
// PLANNED, NOT A RAW WAYPOINT. It used to send the anchor as the only waypoint, and the server
// steps straight at a waypoint and stops at the first wall. Acct212 (live, 2026-09-23) sent that
// walk from 2730,3478, due south of the anchor with the building's north wall in between, every
// two seconds for 21.9 minutes without moving a tile — the same straight line that had put it
// against that wall in the first place. It still clears the interaction either way; planned, it
// also goes somewhere. With no route (a cut-off account: ReturnToCamp is the answer to that) it
// falls back to the raw waypoint, which is still the interrupt.
func StepAwayFrom(d *Deps, tier woodcutplan.Tier) {
	here := currentTile(d)
	dest := tier.Anchor
	if here == dest {
		dest = tile.Tile{X: here.X + 1, Z: here.Z, Level: here.Level}
	}
	way := []pathfind.NavPoint{{X: dest.X, Z: dest.Z, Level: dest.Level}}
	if d.Pack != nil && here.Level == dest.Level {
		if r := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, way[0]); r.OK && len(r.Waypoints) > 1 {
			way = r.Waypoints
		}
	}
	if err := actions.Walk(d.Session, way, true); err != nil {
		d.logf("could not step away from the Ent: %v", err)
	}
}

// returnToCampRadius is how close to the anchor ReturnToCamp stops: close enough that the next
// pass's tree search is centred on the camp, loose enough not to fight other accounts for the
// anchor tile itself.
const returnToCampRadius = 2

// ReturnToCamp walks the account back to the tier's anchor with WalkNear — the door-aware walker,
// not the single raw waypoint StepAwayFrom sends — for a "cut-off" Chop, and for an account that
// has stood away from the anchor finding every tree in reach excluded for woodcutplan's
// StuckReturnAfter passes in a row. The lab trap arm proves it (cmd/treetraptest): from inside the
// building at 2730,3478 the walker plans the Large door hop at 2736,3477 and walks out.
func ReturnToCamp(d *Deps, tier woodcutplan.Tier) bool {
	return WalkNear(d, tier.Anchor, returnToCampRadius)
}

func treeStillThere(d *Deps, x, z, level int, name, op string) bool {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	_, ok := d.Locs.Nearest(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 0, Name: name, Op: op}, live)
	return ok
}
