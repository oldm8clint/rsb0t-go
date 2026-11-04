// Package randomevents ports classic RS random events that this Go client can detect
// (and, for the simpler ones, evade/dismiss) from rsb0t-main's
// src/bot/runtime/randomevents/RandomEvents.ts.
//
// What's covered:
//   - The three numeric-id "hazard" events (poisonous gas, smoking rock, whirlpool)
//     and the numeric-id-set hostile "evade" events (River troll, Swarm, Rock Golem,
//     Zombie, Shade, Watchman, Tree spirit) — matched by raw type id, no name
//     resolution needed.
//   - Mapsquare-based detection of the Mime and maze events, which trap the player on a
//     fixed mapsquare. Both are also actually SOLVED, not just detected — see
//     handle.go's HandleMime and maze.go's SolveMazeEvent.
//   - "dialog"/"pick" events (Genie, Mysterious Old Man, Sandwich Lady, Frog, Drunken
//     Dwarf, Strange Plant) — matched by NPC *name* via internal/cachecfg against
//     nearby NPCs, now that name resolution exists (see names.go). No RandomEvents.ts
//     source was available in reference/rsb0t-scripts/ to port this from verbatim
//     (checked before writing this — see COLLAB.md), so the exact NPC name list is
//     carried over from this file's own long-standing doc comment (a faithful port of
//     what was already documented here) but the engage distance and per-NPC handling
//     are a reasonable reconstruction, not a line-for-line port — flagged as such in
//     names.go and handle.go.
//   - "box"/"lamp" events (Mysterious box, Antique lamp) — matched by inventory item
//     *name* via internal/cachecfg against every tracked inventory component. Same
//     "reconstruction, not verbatim port" caveat as above.
//   - "Big fish" (BigFishNearby, added 2026-09-16 — see its own doc comment for the full
//     ground-truthing trail against the local engine's own content scripts): an NPC-name
//     match exactly like the dialog events above, exposed as its own predicate rather than
//     wired into Detect()/Guardian's kind-based pipeline, since actually recovering the
//     eaten fishing tool needs fisherplan's own ladder knowledge — see
//     internal/scripts/fisherrunner.RecoverLostTool, the layer that actually handles it.
//
// Deliberately NOT covered, both needing something this client doesn't have or
// doesn't attempt:
//   - "hijack" (Ent) — needs the local player's own faceEntity, part of PLAYER_INFO's
//     extended per-entity data that internal/gamestate/position.go deliberately never
//     decodes (see its doc comment).
//
// Both Mime and maze (mapsquare 31,74 and 45,71) ARE actually solved, not just detected
// — see handle.go's HandleMime (reads the Mime NPC's own decoded animation id via
// internal/gamestate/npc.go's anim field, then clicks the matching emote button on the
// macro_mime_emotes interface) and maze.go/internal/randomevents/mazesolve for the
// from-scratch graph-based maze solver (RandomEvents.ts's own maze/ subpackage was never
// available to this project to port — checked reference/rsb0t-scripts/, see its README;
// see COLLAB.md's "maze worktree" entries for the full investigation and live-proof
// trail, and its Mime entries for that solver's own live verification).
package randomevents

import (
	"strings"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
)

// Kind mirrors RandomEvents.ts's EventKind, restricted to what this package detects.
type Kind int

const (
	KindHazard Kind = iota
	KindEvade
	KindTrapped
	// KindDialogNpc is one of the "dialog"/"pick" NPCs (Genie, Mysterious Old Man,
	// Sandwich Lady, Frog, Drunken Dwarf, Strange Plant) — matched by name, see names.go.
	KindDialogNpc
	// KindBoxLamp is a "Mysterious box"/"Antique lamp" sitting in a tracked inventory
	// — matched by name, see names.go.
	KindBoxLamp
)

// Event is a detected random event. NpcIndex/ThreatX/ThreatZ locate the specific
// source (an NPC or a loc tile) when the event has one, so a handler can track that
// exact instance disappearing rather than needing a name to match against — arguably
// more precise than RandomEvents.ts's own name-based re-check, and the only option
// available without NPC name resolution. ItemID/Slot/InvComID locate a KindBoxLamp
// event's inventory item instead; NpcType carries the NPC type id for a KindDialogNpc
// event so a handler can resolve its ops (e.g. "Talk-to") via cachecfg. Fields that
// don't apply to a given Kind are left at their -1 sentinel.
type Event struct {
	Kind             Kind
	Name             string
	NpcIndex         int // -1 if this event isn't tied to a specific NPC
	NpcType          int // -1 unless this event is tied to an NPC
	ThreatX, ThreatZ int

	ItemID   int32 // -1 unless Kind == KindBoxLamp
	Slot     int   // -1 unless Kind == KindBoxLamp
	InvComID int   // -1 unless Kind == KindBoxLamp
}

// Numeric ids ported directly from RandomEvents.ts's own constants.
const (
	gasChestLocID    = 2141
	smokingRockIDMin = 2119
	smokingRockIDMax = 2138
	// hostileEngageDist was 8 (RandomEvents.ts's own original value) until 2026-09-15:
	// widened to match dialogEngageDist's own 15 for the identical reason (see that
	// const's doc comment in names.go) — HOSTILE_EVENT_NPC_IDS ("event monster") is a
	// real combat NPC exactly like the dwarf/plant pair, and a live death this session
	// was traced to Guardian only noticing one within 8 tiles, by which point it may
	// already be close enough to be attacking. 15 is this project's own established
	// ceiling for "detect a genuinely dangerous NPC generously," not an arbitrary bump.
	hostileEngageDist = 15
)

var whirlpoolNpcIDs = map[int]bool{403: true, 404: true, 405: true, 406: true}

// WhirlpoolNearby reports whether any whirlpool NPC (whirlpoolNpcIDs — the same ids Detect's
// own hazard check matches, and fisherplan.WhirlpoolIDs' identical list: a fishing spot
// literally turns into one of these types while a whirlpool event is active on it) is within
// radius tiles of (x,z). Exposed for internal/scripts/fisherrunner.FindSpot to check a
// CANDIDATE fishing spot before ever walking to it — the account owner's own live request
// 2026-09-16 ("when a whirlpool spawns move to a different fishing spot that doesn't have a
// whirlpool or go and bank"): until this, every whirlpool check in this package only ever
// fired reactively, once the account was already standing at (or very near) an active one —
// see HandleHazard. Reuses whirlpoolNpcIDs/chebyshev rather than making fisherrunner
// duplicate this package's own id list.
func WhirlpoolNearby(state *gamestate.State, x, z, radius int) bool {
	for _, npc := range state.NPCs() {
		if whirlpoolNpcIDs[npc.Type] && chebyshev(npc.X, npc.Z, x, z) <= radius {
			return true
		}
	}
	return false
}

// bigFishNpcName is the "Big fish" antimacro NPC's cachecfg name. Ground-truthed
// 2026-09-16 directly against the local test engine's own content scripts — this
// project's real server-side implementation of the classic "Big fish" random event,
// not the OSRS wiki's own necessarily-incomplete summary — since
// grep -rni "big fish" internal/randomevents/ found zero existing handling:
//
//   - content/scripts/macro events/scripts/fishing/macro_event_fishing.rs2 is the exact
//     same random switch WhirlpoolNearby's own doc comment already cites for whirlpool/
//     river troll (`~macro_event_bigfish_spawn`, `~macro_event_river_troll_spawn`,
//     `~macro_event_whirlpool_spawn`), called from every fishing spot's own
//     attempt_fish_* label (e.g. scripts/skill_fishing/scripts/fishing_spots/saltfish.rs2)
//     only while afk_event rolls true on an actual fishing attempt.
//   - content/scripts/macro events/scripts/fishing/macro_event_big_fish.rs2
//     (macro_event_bigfish_spawn) sends NO chat or mesbox line of its own — just
//     npc_changetype(macro_bigfish, 5) on the fishing spot NPC itself (not a new NPC
//     spawn), an animation/projectile sequence, then
//     obj_add($random_coord, $fishing_equipment, 1, ...) to spit the tool onto the
//     ground within map_findsquare(coord, 1, 7, ^map_findsquare_lineofwalk) tiles of the
//     spot (the wiki's own "usually 3-10 tiles"), and inv_del(inv, $fishing_equipment, 1)
//     to eat exactly one of it from the player's INVENTORY (fishing tools live in the
//     ordinary backpack in this content build — see fishing.rs2's own
//     check_fish_equipment proc, `inv_total(inv, $fish_equipment)` — never in a worn
//     equipment slot, and never degrade/break on their own, so an inventory-diff signal
//     is safe here with no ordinary-wear-and-tear confound to guard against).
//   - content/scripts/macro events/configs/antimacro.npc's own [macro_bigfish] entry:
//     `name=Big fish`, `vislevel=hide`. That "hide" level is the SAME visibility class
//     already proven live-detectable by this package's own dialogNpcNames matching —
//     [macro_geni] (Genie) and [macro_mage] (Mysterious Old Man), both `vislevel=hide`
//     too, are both already matched successfully by name against nearby NPCs (see
//     names.go/Detect's KindDialogNpc branch) — so "hidden from other players, visible
//     to the account it belongs to" reliably still reaches this client's own NPC_INFO
//     stream, the same way it does for those two.
//
// No chat-message-based detection exists to fall back to (there genuinely is none, not
// just none found) — this NPC-by-name signal is the only reliable one available, mirroring
// exactly how Genie/Mysterious Old Man are already detected here.
const bigFishNpcName = "Big fish"

// BigFishNearby reports whether a Big fish random event NPC (bigFishNpcName) is within
// radius of x,z. Exposed the same shape as WhirlpoolNearby/HostileEventNpcNearby just
// above (same state/x/z/radius signature, same NPC-nearby-scan shape), for
// internal/scripts/fisherrunner's own fishing-tool-loss recovery to use as a
// confirmation/diagnostic signal alongside its own inventory-diff check — see
// fisherrunner.RecoverLostTool's own doc comment for why the actual recovery trigger is
// the tool disappearing from the pack, not this NPC's presence: recovering the specific
// lost tool needs fishing-specific knowledge (which item name was being fished with,
// tracked by fisherplan's own ladder) that this general package has no way to supply, and
// the "Big fish" NPC itself is momentary (no revert/despawn logic was found in the
// content scripts for it, but nothing guarantees it is still standing there by the time a
// caller polls next) — unlike Whirlpool/River troll's own membership in this exact same
// macro_event_fishing.rs2 switch, which needs no script-specific context to detect or
// evade and so stayed in this package's own kind-based Detect()/Guardian pipeline.
func BigFishNearby(state *gamestate.State, x, z, radius int) bool {
	res := currentResolver()
	if res == nil {
		return false
	}
	for _, npc := range state.NPCs() {
		if chebyshev(npc.X, npc.Z, x, z) > radius {
			continue
		}
		if info, ok := res.Npc(npc.Type); ok && strings.EqualFold(info.Name, bigFishNpcName) {
			return true
		}
	}
	return false
}

// hostileEventNpcIDs mirrors HOSTILE_EVENT_NPC_IDS: antimacro ids that only ever
// spawn as a player's own random event, never as a world mob.
var hostileEventNpcIDs = buildHostileIDs()

func buildHostileIDs() map[int]bool {
	// Swarm, and the Strange plant once it has turned on its owner (macro_triffidseed_angry: the seed, 407,
	// attacks nobody). The angry plant used to be missing, so a plant beside the account was not a threat to
	// stillThreatened, the cornered fight-back rule or the leash.
	ids := map[int]bool{swarmNpcType: true, angryPlantType: true}
	idRange := func(lo, hi int) {
		for i := lo; i <= hi; i++ {
			ids[i] = true
		}
	}
	idRange(391, 396) // River troll
	idRange(413, 418) // Rock Golem
	idRange(419, 424) // Zombie
	idRange(425, 430) // Shade
	idRange(431, 436) // Watchman
	idRange(438, 443) // Tree spirit
	return ids
}

// HostileEventNpcNearby reports whether a known "event monster" hostile (the same
// hostileEventNpcIDs set Detect uses to raise KindEvade — River troll, Swarm, Rock
// Golem, Zombie, Shade, Watchman, Tree spirit, all documented to actively chase rather
// than stand still) currently sits within radius of x,z. Real NPC_INFO position data,
// decoded identically regardless of actions.CheatEnabled — unlike InCombat()'s own
// hit-recency signal (a rolling ~6s "was I hit recently" window, see baseFleeDist's own
// doc comment in handle.go) it cannot read "clear" just because the hostile happens to be
// between attack ticks.
//
// Live-found 2026-09-16 (lc2b_40 on the main 200-account soak, corroborated locally on
// wfixtest2 in a 40-account soak the same night): guardian.go's kick() "in combat with no
// tracked event" branch and fleeFromCombatWalking below both previously treated
// !InCombat() alone as proof the flee worked, then called it done. A chasing event
// monster that is still standing adjacent between hits makes InCombat() read false for
// several seconds at a time while never actually having been shaken off — both accounts
// show the identical shape: fleeing, InCombat() lapsing, combat resuming a few seconds
// later, streak escalating each time, with no real separation ever confirmed in between.
// Exported and shaped exactly like WhirlpoolNearby just above (same state/x/z/radius
// signature, same reuse of hostileEventNpcIDs/chebyshev) since the same "is a known
// NPC set anywhere nearby" primitive applies to both.
func HostileEventNpcNearby(state *gamestate.State, x, z, radius int) bool {
	for _, npc := range state.NPCs() {
		if hostileEventNpcIDs[npc.Type] && chebyshev(npc.X, npc.Z, x, z) <= radius && threatensAccount(npc, state, x, z) {
			return true
		}
	}
	return false
}

// Visibility alone does not establish aggression. Idle personal event NPCs can
// remain in a crowded bank after their owner has fled. A fresh target update will
// be detected on the next poll; unassigned adjacent NPCs still trigger when hit.
//
// The fresh-NPC allowance is bounded by distance. Every hostile macro event (river troll,
// swarm, rock golem, shade, zombie, watchman, dryad) is npc_add'ed exactly one tile from
// its owner (map_findsquare(coord, 1, 1)) and switches to opplayer2 on its owner within
// 0-3 ticks. So an unassigned new monster more than freshOwnerRadius tiles away was
// spawned for somebody else. Measured live 2026-09-19: without this bound one account's
// event spawning at the Catherby camp sent 70-110 fleet accounts fleeing at once, roughly
// every five minutes, and each then paused its fishing spot behind the recent-fight block.
func threatensAccount(npc gamestate.NPC, state *gamestate.State, x, z int) bool {
	if npc.TargetsPlayer(state.Self()) {
		return true
	}
	if npc.TargetsAnotherPlayer(state.Self()) {
		return false
	}
	dist := chebyshev(npc.X, npc.Z, x, z)
	fresh := npc.VisibleTicks < 6 && dist <= freshOwnerRadius && !otherPlayerAsClose(state, npc, dist)
	return fresh || (state.InCombat() && dist <= 2)
}

// otherPlayerAsClose reports whether another visible player stands at least as close to npc
// as this account does. Then a new, unassigned monster may be theirs, and its first target
// update (at most three ticks away) settles it. Live 2026-09-19 on 1515cb3: accounts stack on
// the tiles beside a busy spot, and 53 of them stood within one tile of one account's new
// swarm (face -1, seen 0 ticks); the distance bound alone could not tell them apart.
func otherPlayerAsClose(state *gamestate.State, npc gamestate.NPC, dist int) bool {
	for _, p := range state.Players() {
		if chebyshev(p.X, p.Z, npc.X, npc.Z) <= dist {
			return true
		}
	}
	return false
}

// freshOwnerRadius is how far a just-spawned event monster can be from its own owner when
// first seen: one spawn tile plus up to two ticks of running (two tiles a tick) before the
// guardian's poll notices it.
const freshOwnerRadius = 5

// mazeSquare/mimeSquare are the fixed mapsquares (64-tile blocks) these two events
// trap the player on, ported from maze/solveMaze.ts and solvers/Mime.ts.
const (
	mimeSquareMX, mimeSquareMZ = 31, 74
	mazeSquareMX, mazeSquareMZ = 45, 71
)

// InTrapEventSquare reports whether tile x,z lies in the Mime's or the maze's fixed
// mapsquare: the event holds the account there until it ends and sends it back, so walking
// "home" from inside only fights the event (live 2026-09-19: the mule retried an unreachable
// 1321-tile walk home for a minute during a Mime).
func InTrapEventSquare(x, z int) bool {
	mx, mz := x>>6, z>>6
	return (mx == mimeSquareMX && mz == mimeSquareMZ) || (mx == mazeSquareMX && mz == mazeSquareMZ)
}

func chebyshev(ax, az, bx, bz int) int {
	dx := ax - bx
	if dx < 0 {
		dx = -dx
	}
	dz := az - bz
	if dz < 0 {
		dz = -dz
	}
	if dx > dz {
		return dx
	}
	return dz
}

// min/max shadow the Go 1.21 builtins of the same name, which this repo cannot use:
// CLAUDE.md pins the build to Go 1.19.3, where they do not exist. Used by handle.go's
// escape-distance ramp and flee_walking.go's wolf-exclusion radius.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// newEvent returns an Event with every optional field at its -1 "not applicable"
// sentinel, for callers to fill in just the fields their Kind actually uses.
func newEvent(kind Kind, name string) *Event {
	return &Event{Kind: kind, Name: name, NpcIndex: -1, NpcType: -1, ItemID: -1, Slot: -1, InvComID: -1}
}

// Detect checks every event kind this package knows against the session's current
// state. Order mirrors detectRaw's own priority: trapped-by-mapsquare first (these
// override everything else since nothing else matters while stuck), then hazards and
// hostile/dialog NPCs from the visible scene, then a box/lamp check over tracked
// inventories. The dialog-NPC and box/lamp checks are skipped entirely when no
// NameResolver has been registered via SetNameResolver (see names.go) — this keeps
// Detect's own signature and behavior unchanged for callers that never call it, such
// as cmd/barbtest, which only wants the numeric-id subset.
func Detect(state *gamestate.State) *Event {
	return detectEligible(state, -1, nil)
}

// Filter while scanning, so an ignored event cannot hide another nearby event.
// Ownership takes precedence over proximity and a previous unconfirmed choice.
func detectEligible(state *gamestate.State, preferredNpcIndex int, accept func(*Event) bool) *Event {
	eligible := func(ev *Event) bool { return accept == nil || accept(ev) }
	if x, z, level, ok := state.Tile(); ok && level == 0 {
		mx, mz := x>>6, z>>6
		if mx == mimeSquareMX && mz == mimeSquareMZ {
			ev := newEvent(KindTrapped, "mime")
			// Find the Mime NPC itself so HandleMime can read its current animation —
			// see that function's own doc comment. Matched by name, the same way
			// KindDialogNpc's own NPCs are, not by a fixed local coordinate: the real
			// script's own npc_find call (0_31_74_27_26) is a hint at roughly where it
			// stands, not a guarantee, and name matching is what this package already
			// has a proven mechanism for.
			if res := currentResolver(); res != nil {
				for _, npc := range state.NPCs() {
					if info, ok := res.Npc(npc.Type); ok && strings.EqualFold(info.Name, "Mime") {
						ev.NpcIndex, ev.NpcType = npc.Index, npc.Type
						break
					}
				}
			}
			if eligible(ev) {
				return ev
			}
		}
		if mx == mazeSquareMX && mz == mazeSquareMZ {
			ev := newEvent(KindTrapped, "maze")
			if eligible(ev) {
				return ev
			}
		}
	}

	for _, loc := range state.Locs() {
		if loc.Type == gasChestLocID {
			ev := newEvent(KindHazard, "poisonous gas")
			ev.ThreatX, ev.ThreatZ = loc.X, loc.Z
			if eligible(ev) {
				return ev
			}
		}
		// A smoking rock only hurts the account mining it (macro_event_gas.rs2: the spark comes
		// from the miner's own swing), so one merely in view is not this account's event. Live
		// 4 Oct: wario fought Al-Kharid warriors beside one and the guardian fled and gave up
		// on it 56 times. Only a rock the account stands next to, where it mines from, counts.
		if loc.Type >= smokingRockIDMin && loc.Type <= smokingRockIDMax && besideSelf(state, loc.X, loc.Z, loc.Level) {
			ev := newEvent(KindHazard, "smoking rock")
			ev.ThreatX, ev.ThreatZ = loc.X, loc.Z
			if eligible(ev) {
				return ev
			}
		}
	}

	x, z, _, ok := state.Tile()
	if !ok {
		return nil
	}
	res := currentResolver()
	mainModal, _, _ := state.Modals()
	trading := mainModal == actions.TradeOfferScreen || mainModal == actions.TradeConfirmScreen
	self := state.Self()
	selfName, named := state.SelfName()
	selfName = strings.ReplaceAll(strings.ToLower(selfName), "_", " ")
	var nearestDialog *Event
	nearestDialogDist := -1
	nearestOwned := false
	for _, npc := range state.NPCs() {
		dist := chebyshev(npc.X, npc.Z, x, z)
		if whirlpoolNpcIDs[npc.Type] && dist <= 3 {
			ev := newEvent(KindHazard, "whirlpool")
			ev.NpcIndex, ev.ThreatX, ev.ThreatZ = npc.Index, npc.X, npc.Z
			ev.NpcType = npc.Type
			if eligible(ev) {
				return ev
			}
		}
		// hostileEventNpcIDs only ever spawn as a player's own personal random event,
		// never as a shared world mob (see this var's own doc comment) — so at a busy
		// shared camp (many fleet accounts fishing within sight of each other), this NPC
		// is very often someone ELSE's own event monster, plainly visible but never going
		// to attack this account. Excluding a confirmed different-target match (not a
		// not-yet-targeting one — FaceEntity briefly reads "nobody" for a couple of ticks
		// right after spawn, before the NPC locks onto its own owner, and treating that
		// as safe-to-ignore would risk missing a genuine new threat) stops every other
		// nearby account from fleeing to the bank over a monster that was never theirs to
		// begin with, the same TargetsAnotherPlayer check advancedfighter.go already uses
		// for the identical "is this actually mine" question in combat.
		if hostileEventNpcIDs[npc.Type] && dist <= hostileEngageDist && threatensAccount(npc, state, x, z) {
			ev := newEvent(KindEvade, "event monster")
			ev.NpcIndex, ev.ThreatX, ev.ThreatZ = npc.Index, npc.X, npc.Z
			ev.NpcType = npc.Type
			if eligible(ev) {
				return ev
			}
		}
		// Nearest match wins, not first-in-iteration-order — real, live-reported gap: at
		// a busy shared camp, "when multiple randoms spawn for multiple people in our
		// area" (the account owner's own words) means more than one dialog-shaped NPC
		// (most of which turn out to be a NEARBY account's own event, not this one's —
		// HandleDialogNpc's own ownership-rejection check below is what actually finds
		// that out) can be visible at once, and state.NPCs()'s own order has nothing to
		// do with distance. Picking the first match arbitrarily walked accounts toward a
		// farther candidate while a closer, cheaper-to-check-and-dismiss one sat right
		// there. This only changes WHICH of several simultaneous candidates gets checked
		// first, not whether ownership gets checked at all — see this var's own doc
		// comment.
		if res != nil && dist <= dialogEngageDist {
			if info, ok := res.Npc(npc.Type); ok && matchesAnyName(info.Name, dialogNpcNames) {
				if eventOver(info.Name, npc.Speech) {
					// Its despawn or fail line: nothing is left to claim, for anyone.
					continue
				}
				owned := npc.TargetsPlayer(self)
				owner := npcSpeechOwner(info.Name, npc)
				if owner != "" && named {
					if owner != selfName {
						continue
					}
					owned = true
				} else if npc.TargetsAnotherPlayer(self) {
					continue
				} else if !owned && !unknownOwnerMayBeOurs(state, info.Name) {
					// Neither greeting nor face says whose it is, and its owner-only sound
					// never reached this account: someone else's (ownerSounds).
					continue
				}
				ev := newEvent(KindDialogNpc, info.Name)
				ev.NpcIndex, ev.NpcType, ev.ThreatX, ev.ThreatZ = npc.Index, npc.Type, npc.X, npc.Z
				// An angry plant shares the passive plant's name. It is a threat,
				// not a dialogue candidate, even if another NPC is preferred.
				if strings.EqualFold(info.Name, "Strange plant") && cachecfg.OpIndex(info.Ops, "Attack") != 0 {
					if !threatensAccount(npc, state, x, z) {
						continue
					}
					ev.Kind = KindEvade
					if eligible(ev) {
						return ev
					}
					continue
				}
				if strings.EqualFold(info.Name, "Strange plant") && !plantMayBeOurs(state) {
					continue
				}
				// Do not close a live trade merely to probe an unknown nearby owner.
				// Named/facing-owned dialogue and aggressive threats still preempt it.
				if trading && !owned {
					continue
				}
				if !eligible(ev) {
					continue
				}
				better := nearestDialog == nil || (owned && !nearestOwned)
				if nearestDialog != nil && owned == nearestOwned {
					preferred := npc.Index == preferredNpcIndex
					previousPreferred := nearestDialog.NpcIndex == preferredNpcIndex
					better = (preferred && !previousPreferred) || (preferred == previousPreferred && dist < nearestDialogDist)
				}
				if better {
					nearestDialog, nearestDialogDist, nearestOwned = ev, dist, owned
				}
			}
		}
	}
	if nearestDialog != nil {
		return nearestDialog
	}

	if res != nil {
		if ev := detectBoxLampEligible(res, heldEventInventories(state), accept); ev != nil && !trading {
			return ev
		}
	}

	return nil
}

// DetectPreferring is Detect, but when preferredNpcIndex is a known Strange-Plant-or-
// similar dialog NPC this account has already engaged (see guardian.go's own
// stickyPlantIndex field), it keeps returning that SAME NPC as the active dialog event
// instead of whatever Detect's own first-match scan finds, as long as nothing more urgent
// (a hazard, an evade-worthy hostile, mime/maze) is present — those always take priority
// unchanged. Never invents an event Detect wouldn't otherwise report on its own; only
// substitutes one dialog-NPC choice for another.
//
// Live-found 2026-09-15/16 (a 200-account soak, real deaths reported by the account
// owner): several Strange Plant instances can be growing near the same mass-spawn point at
// once, and Detect's own scan just returns whichever matches first in state.NPCs()'s
// current ordering — with no memory of which one this account already confirmed as its
// own. That let an account bounce between different nearby plants from one poll to the
// next (quickly resolving each wrong one via "not here for you"), while its own actual
// plant sat completely unattended for however long it took Detect to happen to land on it
// again — long enough, in several confirmed cases, for that plant's own hostile-
// transformation timer to elapse unattended. The account owner's own diagnosis: "the bots
// whose plant it is should stay at the plant until its ready to pick... while the others
// check if its theirs or not." This is that fix.
func DetectPreferring(state *gamestate.State, preferredNpcIndex int) *Event {
	return detectEligible(state, preferredNpcIndex, nil)
}

// plantLifetimeTicks bounds how long a passive Strange plant can stand: content's
// macro_triffidseed timer runs twice at 90 ticks (grow, then turn hostile), with margin.
const plantLifetimeTicks = 200

// plantMayBeOurs reports whether a passive Strange plant in view could be this account's.
// Content plays triffid_appear to the plant's owner alone. Live 2026-09-19 on 23704ea: 111 of
// 111 accounts told "not here for you" had not heard it, and both owners had. Without that
// sound in the plant's whole lifetime the plant is someone else's, so there is no need to walk
// over and ask; stacked fishers did that ~400 times per 45 minutes. A session that has
// received no SYNTH_SOUND at all keeps the old check-every-plant behaviour.
func plantMayBeOurs(state *gamestate.State) bool {
	heard, anySounds := state.HeardSoundNear(gamestate.SoundTriffidAppear, state.Tick()-plantLifetimeTicks/2, plantLifetimeTicks/2)
	return heard || !anySounds
}

// besideSelf reports whether the tile at x,z,level is the account's own tile or one next to it.
func besideSelf(state *gamestate.State, x, z, level int) bool {
	sx, sz, sl, ok := state.Tile()
	return ok && sl == level && chebyshev(x, z, sx, sz) <= 1
}
