package woodcutplan

// The three woodcutting random events, and what this script has to decide about each.
//
// All of it is MEASURED from content/scripts/macro events/scripts/woodcutting/, and reading
// those scripts changed the picture materially in two places — one event is already handled by
// machinery this script inherits, and the scariest one turns out to be something this client's
// architecture cannot walk into. Both are recorded here so a later session does not "fix" a
// problem that is not there.
//
// ---- 1. The Ent (npc 444-452) ----
//
// macro_event_ent.rs2's `macro_event_ent_spawn` does two things that matter:
//
//	loc_del(60);                   // the TREE loc is removed for 60 ticks
//	npc_add($coord, $ent, 60);     // an Ent NPC stands on the tree's own tile
//
// So an Ent does not sit BESIDE the tree — it REPLACES it. The axe only breaks through
// `macro_event_damage_axe`, which is reached solely from `[opnpc1,_macro_event_ent]` and
// `[opnpc3,_macro_event_ent]`: you have to click the ENT, as an NPC, six times
// (`%macro_ent_counter = 6`) before it swaps your axe for a Broken axe.
//
// **This client chops by LOC, but that is NOT an immunity** (corrected 2026-10-04 against
// content). macro_event_ent_spawn ends with `p_opnpc(1)`: the ENGINE puts the account on the
// Ent itself, and every damage cycle re-queues `p_opnpc(3)`. The account keeps "chopping" the
// Ent, with the woodcutting animation, until this client sends another action; each cycle
// damages the axe and the seventh breaks it (`%macro_ent_counter`, kept across Ents). Live:
// two "Broken axe" events on ProgressiveWCFletch in September. The defence is speed of the next
// action: woodcutrunner.Chop returns "ent" the moment an Ent stands at its tree, the caller clicks
// a different tree at once or steps away (internal/bot/woodcutter.go chopOutcome), and
// TreeBlocks keeps the Ent's tile out of the next pick.
//
// **The Ents are named exactly like the trees they replace** — `macro_ent_dead_tree1` and
// `macro_ent_dead_tree2` are both `name=Dead tree`, `macro_ent_oak` is `Oak`, and so on. This is
// the second time this exact disguise appears at the Draynor camp (see startcamp.go on
// `nasty_tree`, which reuses the dead tree's model and recolours). **Never identify a tree by
// name from NPC state.** Match Ents by ID, which is what IsEntNpc is for.
//
// ---- 2. The axe head flying off (the one genuinely new handler) ----
//
// macro_event_lost_axe.rs2 swaps the axe for `macro_hatchethandle` (obj 492, name "Axe handle")
// and throws the head to `map_findsquare(coord, 1, 7, ^map_findsquare_lineofwalk)` — so it lands
// between 1 and 7 tiles away, on a tile that can actually be walked to, NOT on the account's own
// tile. It is dropped with `^lootdrop_duration` (= 200 ticks, `drop tables/configs/lootdrop.constant`),
// which at this engine's 600ms tick is about two minutes before it disappears.
//
// Re-attaching is a plain item-on-item use, and content accepts it BOTH ways round
// (`[opheldu,macro_hatchethandle]` switching on the head, and `[opheldu,macro_*_hatchethead]`
// via `check_axe_head`), so either direction works.
//
// Every tier's head is `name=Axe head` and every broken axe is `name=Broken axe` — one name each,
// not one per tier — so name matching is safe here even though it is not for the Ents.
//
// This is the one that has to PRE-EMPT ordinary work: the account is holding a useless handle,
// cannot chop at all until it is fixed, and the head is on a two-minute timer.
//
// ---- 3. The Tree spirit / Dryad (npc 438-443) — ALREADY HANDLED, do not rebuild ----
//
// The "tree spirit" is `macro_dryhadguardian_1..6`, which resolve to npc ids **438 through 443**
// (content/pack/npc.pack). internal/randomevents' own `hostileEventNpcIDs` already contains
// exactly `idRange(438, 443) // Tree spirit`, so Detect raises its ordinary KindEvade and the
// Guardian's walking flee runs — the same flee that was rebuilt to hold a direction away from
// the attacker.
//
// The "only the targeted account runs" requirement is also already met, and by the very code
// written for the incident it describes: `threatensAccount` checks `npc.TargetsPlayer(self)` /
// `TargetsAnotherPlayer(self)` first, and bounds a not-yet-assigned monster by `freshOwnerRadius`
// plus `otherPlayerAsClose`, because without that bound "one account's event spawning at the
// Catherby camp sent 70-110 fleet accounts fleeing at once". Content agrees that the binding is
// exclusive: macro_event_dryad.rs2 sets `%npc_macro_event_target = uid` and
// `npc_setmode(opplayer2)` on one player.
//
// ProgressiveWoodcutter calls woodcutrunner.WaitForGuardian first thing in every Loop, so it
// already inherits all of this. Nothing is added here for the dryad on purpose — a second flee
// would be the third one in this repo and would re-introduce the bug the scoping fixed.

import "strings"

// Ent npc ids, from content/pack/npc.pack: 444 macro_ent_tree1, 445 macro_ent_tree2,
// 446 macro_ent_dead_tree1, 447 macro_ent_dead_tree2, 448 macro_ent_oak, 449 macro_ent_willow,
// 450 macro_ent_yew, 451 macro_ent_maple, 452 macro_ent_magic.
//
// Both dead-tree variants are in the list, so the Draynor start camp gets Ents like anywhere
// else.
const (
	EntNpcFirst = 444
	EntNpcLast  = 452
)

// IsEntNpc reports whether an NPC type id is one of the Ents.
//
// By ID, never by name: every Ent carries the display name of the tree it replaced
// (`macro_ent_dead_tree1` is "Dead tree"), so a name check would match the real tree and a
// name-based exclusion would blocklist the whole camp.
func IsEntNpc(npcType int) bool {
	return npcType >= EntNpcFirst && npcType <= EntNpcLast
}

// Dryad (Tree spirit) npc ids, recorded for the record only — internal/randomevents already
// carries this exact range as `idRange(438, 443) // Tree spirit` and handles the event. Nothing
// in this package acts on them; see this file's own header for why that is deliberate.
const (
	DryadNpcFirst = 438
	DryadNpcLast  = 443
)

// The item names this script has to recognise. One name each across every tier — content gives
// all seven broken hatchets `name=Broken axe` and all seven heads `name=Axe head` — so unlike
// the Ents, matching these by name is safe and is what the rest of this package already does.
const (
	// AxeHandle is what the axe becomes when its head flies off. Useless for chopping.
	AxeHandle = "Axe handle"
	// AxeHead is the piece that lands on the ground nearby.
	AxeHead = "Axe head"
	// BrokenAxe is what an axe becomes if an Ent is chopped six times. Only Bob repairs it.
	BrokenAxe = "Broken axe"
)

// BrokenAxeIDs maps each broken hatchet's OBJECT ID to the axe Bob hands back for it.
//
// This exists because all seven are named "Broken axe" — the name that makes IsBrokenAxe safe
// is exactly what makes the tier invisible. The ids and their `param=fixed` targets are read
// from content/pack/obj.pack and content/scripts/macro events/configs/antimacro.obj: 494
// bronze, 496 iron, 498 steel, 500 black, 502 mithril, 504 adamant, 506 rune. The odd numbers
// in between are the `cert_` bank notes, which are not the thing (see woodcutrunner's own
// nonNoted rule) and are deliberately absent.
//
// The broken PICKAXES at 468-478 are NOT here, and must never be added: they carry
// `param=to_be_fixed_by_nurmof` and bob.rs2 answers them with "Sorry friend, but I can't do
// anything with that."
var BrokenAxeIDs = map[int]string{
	494: "Bronze axe",
	496: "Iron axe",
	498: "Steel axe",
	500: "Black axe",
	502: "Mithril axe",
	504: "Adamant axe",
	506: "Rune axe",
}

// RepairedAxe is the axe a broken one becomes, by object id, or "" for anything that is not one
// of the seven.
func RepairedAxe(objID int) string { return BrokenAxeIDs[objID] }

// AxeHeadSearchRadius is how far to look for a thrown head: content throws it with
// `map_findsquare(coord, 1, 7, ...)`, so 7 tiles is the exact maximum, not an estimate. One tile
// of slack is added because the account may have taken a step before noticing.
const AxeHeadSearchRadius = 8

// AxeHeadLifetimeTicks is `^lootdrop_duration` (drop tables/configs/lootdrop.constant): 200
// ticks. At a 600ms tick that is about two minutes before the head disappears for good, which is
// why recovering it pre-empts ordinary work instead of waiting for a convenient moment.
const AxeHeadLifetimeTicks = 200

// IsAxeHead / IsBrokenAxe / IsAxeHandle match those names.
func IsAxeHead(name string) bool   { return same(strings.TrimSpace(name), AxeHead) }
func IsBrokenAxe(name string) bool { return same(strings.TrimSpace(name), BrokenAxe) }
func IsAxeHandle(name string) bool { return same(strings.TrimSpace(name), AxeHandle) }

// AxeHeadSlotsNeeded is how many free pack slots putting the axe back together takes: one for
// the head unless it is already held, and one more when the handle is WORN, because content's
// reattach_axe reads both pieces from the inventory (`inv_total(inv, macro_hatchethandle)`) and a
// worn handle has to be taken off first.
func AxeHeadSlotsNeeded(headHeld, handleWorn bool) int {
	n := 0
	if !headHeld {
		n++
	}
	if handleWorn {
		n++
	}
	return n
}

// SpareForAxeHead reports whether a pack item may be dropped to make room for the axe head: a
// log, or an unstrung bow (content names a bow the same strung or not). They are the cheapest
// things a woodcutter carries, and the head is the whole axe: it despawns two minutes after it
// lands. Live 2026-09-22, Acct171 at the Seers bank with a full pack logged "the pack is full,
// so the head cannot be picked up" and lost its axe.
func SpareForAxeHead(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return IsLogs(name) || strings.HasSuffix(n, " shortbow") || strings.HasSuffix(n, " longbow") ||
		n == "shortbow" || n == "longbow"
}

// WoodEvent is a woodcutting event this script has to act on itself, as opposed to the ones the
// shared Guardian already handles.
type WoodEvent string

// The events, in the order DecideWoodEvent prefers them.
const (
	// EventNone: nothing of ours is happening.
	EventNone WoodEvent = "none"
	// EventAxeHead: the head is off and has to be found and re-attached, now.
	EventAxeHead WoodEvent = "axe-head"
	// EventBrokenAxe: an Ent broke the axe; only Bob in Lumbridge can repair it.
	EventBrokenAxe WoodEvent = "broken-axe"
)

// EventSnapshot is the live state DecideWoodEvent reads.
type EventSnapshot struct {
	// HasHandle is true when an "Axe handle" is in the pack or worn.
	HasHandle bool
	// HasHead is true when an "Axe head" is already in the pack.
	HasHead bool
	// HeadOnGround is true when an "Axe head" is visible on the ground within
	// AxeHeadSearchRadius.
	HeadOnGround bool
	// HasBrokenAxe is true when a "Broken axe" is in the pack or worn.
	HasBrokenAxe bool
	// BrokenAxeID is that broken axe's object id, so RepairedAxe can say which tier it is —
	// all seven share the name "Broken axe", so the name cannot. Zero when none is held.
	BrokenAxeID int
	// HasWorkingAxe is true when some ordinary axe can still chop.
	HasWorkingAxe bool
	// HeldBest is the best of those, by name, or "" when there is none.
	HeldBest string
	// BankedAxes are the axes this account's own bank holds, as the last bank read saw them.
	// Empty means either an empty bank or a bank that has not been read yet; both are
	// treated the same, because both mean "nothing known to fall back on".
	BankedAxes []string
}

// DecideWoodEvent picks what to do about this script's own events.
//
// The axe head comes first even when a working axe is also held, because the head is on a
// two-minute timer and the axe is not going anywhere: losing the head loses that axe for good,
// while a delayed chop costs a few seconds.
//
// A broken axe is only worth a trip to Bob when the repaired axe would genuinely beat
// everything else the account can reach without leaving Seers, because the trip is NOT cheap:
// measured offline against the real transport graph on 2026-09-20, a quest-less account's only
// Seers -> Lumbridge route is Ardougne -> Brimhaven -> Karamja -> Port Sarim -> Lumbridge, two
// boat fares at 30 coins each way (120 coins round trip) and several hundred walk corners.
// Meanwhile five of the seven repairs — bronze, iron, steel, black at 10gp and mithril at 18gp
// — are worth less than the fares.
//
// So the rule is a comparison, not "have I got anything at all":
//
//   - an axe in the PACK that is at least as good as the repaired one: carry on chopping, the
//     broken one is dead weight the ordinary bank step deposits.
//   - an axe in this account's own BANK that is at least as good: let the phase machine walk
//     to the bank it is standing next to, not across the world. DecideWoodPhase already
//     returns PhaseBank for an empty hand with a banked axe.
//   - otherwise: Bob, because he is then the only way to be chopping again at that tier.
//
// An unknown BrokenAxeID (a broken axe whose id is not one of the seven — which should not
// happen, and would mean the content changed) is treated as worth repairing only when there is
// nothing to chop with at all, which is the old behaviour and the safe one.
func DecideWoodEvent(s EventSnapshot) WoodEvent {
	if s.HasHandle && (s.HasHead || s.HeadOnGround) {
		return EventAxeHead
	}
	if !s.HasBrokenAxe {
		return EventNone
	}
	repaired := RepairedAxe(s.BrokenAxeID)
	if repaired == "" {
		// Unknown tier: fall back to the old, conservative test.
		if !s.HasWorkingAxe {
			return EventBrokenAxe
		}
		return EventNone
	}
	if s.HasWorkingAxe && !BetterAxe(repaired, s.HeldBest) {
		return EventNone
	}
	if !BetterAxe(repaired, BestAxe(s.BankedAxes)) {
		return EventNone
	}
	return EventBrokenAxe
}

// BetterAxe reports whether axe is strictly better than other. An empty other is worse than
// anything, so the first axe an account can get always wins.
func BetterAxe(axe, other string) bool {
	if strings.TrimSpace(other) == "" {
		return strings.TrimSpace(axe) != ""
	}
	return axeIndex(axe) > axeIndex(other)
}

// DescribeWoodEvent is the status line for an event.
func DescribeWoodEvent(e WoodEvent) string {
	switch e {
	case EventAxeHead:
		return "putting the axe back together"
	case EventBrokenAxe:
		return "taking a broken axe to Bob's"
	}
	return "nothing"
}
