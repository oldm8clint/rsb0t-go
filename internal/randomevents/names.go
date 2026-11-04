package randomevents

import (
	"strings"
	"sync"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
)

// NameResolver is the minimal subset of *cachecfg.Config (see that package's own doc
// comment) Detect needs for the name-based event kinds: type-id -> name/ops lookups
// for NPCs and items. Declared as a small interface here — rather than wiring
// *cachecfg.Config directly into Detect's own signature or logic — purely so tests can
// inject a trivial synthetic resolver instead of building a real config archive
// end-to-end (cachecfg's own synthetic-record test helper is unexported to its
// package). *cachecfg.Config already satisfies this with no changes needed there.
type NameResolver interface {
	Npc(id int) (cachecfg.Npc, bool)
	Obj(id int) (cachecfg.Obj, bool)
}

// dialogNpcNames are the "dialog"/"pick" event NPCs, matched by exact name
// (case-insensitive, same convention cachecfg.NpcIDs/ObjIDs use). Carried over
// verbatim from this package's own long-standing doc comment — no RandomEvents.ts
// source was available in reference/rsb0t-scripts/ to check this list against
// verbatim (see COLLAB.md's write-up on this task), so this is a faithful port of what
// was already documented here, not a fresh guess.
var dialogNpcNames = []string{
	"Genie", "Mysterious Old Man", "Sandwich Lady", "Frog", "Drunken Dwarf", "Strange Plant",
}

// dialogRejectionPhrases are every known "this isn't your event" line across the dialog
// NPCs that share HandleDialogNpc's generic fallback path (see that function's own doc
// comment on why this is a checked list, not one assumed pattern). Genie and Mysterious
// Old Man (macro_event_genie.rs2, macro_event_mysterious_old_man.rs2) share the same
// "...trying to talk to <player>... appears to be ignoring me/ignorning me [sic, real
// content typo]..." wording; Drunken Dwarf (macro_event_drunken_dwarf.rs2) uses a
// completely different, unrelated line, confirmed by reading its own opnpc1 handler
// directly after the account owner pointed out it also tells the player in dialog whether
// it's theirs — checking only the first pattern would have silently never caught a
// rejected dwarf at all.
var dialogRejectionPhrases = []string{
	"trying to talk to",
	"not my matey",
}

// matchesAnyPhrase reports whether text contains any of phrases as a substring.
func matchesAnyPhrase(text string, phrases []string) bool {
	for _, p := range phrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// boxLampItemNames are the "box"/"lamp" event items. Corrected 2026-09-15 against this
// project's own real content config, not RandomEvents.ts (still unavailable — see
// detect.go's package doc comment): "Mysterious box"/"Antique lamp" (this file's
// original, long-standing names) turned out to be the in-chat wording used when the
// item is handed over ("You have been given a Mysterious box!"), not the item's actual
// name field — confirmed via content/scripts/macro events/configs/antimacro.obj
// ([macro_cube] name=Strange box) and content/scripts/general/configs/xplamp.obj
// ([macro_genilamp] name=Lamp, given by macro_event_genie.rs2 as a post-genie reward).
// Live-verified: the old names never matched, so detectBoxLamp silently never fired for
// a real Strange box sitting in inventory — see cmd/guidecrash -forcerandom box's write-up
// in COLLAB.md for the live trace that caught this.
var boxLampItemNames = []string{
	"Strange box", "Lamp",
}

// dialogEngageDist is a reconstructed threshold, not ported from a source this task
// could read (see detect.go's package doc comment).
//
// Widened from hostileEngageDist's own 8 tiles to 15 (the largest value this project's
// own NPC_INFO test helpers can represent via the protocol's real 5-bit signed per-tick
// delta field — see detect_names_test.go's addNPC), 2026-09-15, after a real, live
// death: a Drunken Dwarf event handled correctly and promptly by HandleDialogNpc still
// went hostile and killed the account (freshtest3, confirmed by real-time observation —
// see COLLAB.md). Root cause, well-supported not guessed: the dwarf's own ai_timer nag
// sequence (content/scripts/macro events/scripts/general/macro_event_drunken_dwarf.rs2)
// starts advancing from the moment it spawns and follows the player
// (npc_setmode(playerfollow)) — it does NOT wait for this client to notice it. If the
// player is actively moving (fishing, walking between spots) faster than the NPC's own
// follow speed can consistently keep up with, real gaps beyond the old 8-tile radius are
// plausible, during which the nag counter keeps climbing toward its hostile-fail state
// with Guardian never seeing it at all. By the time it finally comes back within 8
// tiles and Guardian's very first Detect() notices it, the nag budget may already be
// nearly spent — matching the observed live timing (dialogue never opened, "cleared"
// within 6 seconds of first detection, immediately followed by a Lumbridge-area
// position consistent with a death respawn). A wider 15-tile radius gives Guardian a
// real chance to notice and engage well before the timer runs out, rather than
// engaging correctly but too late. Does not eliminate the risk (a still-longer gap, or a
// handler that itself fails to open a dialogue in time, remains possible) — see
// fisherrunner.ReturnToCatherby for the companion recovery fix for when this still
// happens.
const dialogEngageDist = 15

func matchesAnyName(name string, list []string) bool {
	for _, n := range list {
		if strings.EqualFold(name, n) {
			return true
		}
	}
	return false
}

var (
	resolverMu sync.RWMutex
	resolver   NameResolver

	archiveMu        sync.RWMutex
	interfaceArchive archive.Components
)

// SetNameResolver registers the shared resolver Detect uses for the dialog/pick and
// box/lamp event kinds. Call once at startup with the process's *cachecfg.Config (see
// that package's own doc comment: one shared, read-only copy per process, same as this
// registration). Detect's own signature is unchanged and simply skips these two checks
// when no resolver has been set, so existing callers that only want the numeric-id
// subset (e.g. cmd/barbtest) keep working exactly as before, untouched.
func SetNameResolver(r NameResolver) {
	resolverMu.Lock()
	resolver = r
	resolverMu.Unlock()
}

func currentResolver() NameResolver {
	resolverMu.RLock()
	defer resolverMu.RUnlock()
	return resolver
}

// SetInterfaceArchive registers the shared, already-parsed interface archive (the same
// one actions.NewBank/actions.NewChatDialog take — see NewBank's own doc comment for
// how a real client wires this in once at startup) HandleDialogNpc/HandleBoxLamp use to
// click through a dialog's continue prompt and option list via actions.ChatDialog. Left
// unset, those two handlers still perform their initial action (talk-to / open the
// item) but then just wait out a rough duration instead of navigating the dialog, since
// without the archive there's no reliable way to find the continue/option component
// ids. cmd/barbtest never calls this and is unaffected either way.
func SetInterfaceArchive(components archive.Components) {
	archiveMu.Lock()
	interfaceArchive = components
	archiveMu.Unlock()
}

func currentInterfaceArchive() archive.Components {
	archiveMu.RLock()
	defer archiveMu.RUnlock()
	return interfaceArchive
}

// heldEventInventories is where a held event item (a Strange box, a genie Lamp) can be: the
// BACKPACK ONLY, once the backpack component is known at all -- empty or not. gamestate never
// forgets an inventory component, so after a bank visit KnownInventories still holds the bank's
// deposit-side mirror of the pack (component 2006 on this build) exactly as it was when the bank
// closed. A lamp seen THERE is a ghost: an OPHELD against a closed interface's component is
// dropped by the server, lampStillPresent reads the same frozen mirror, and the item "stays
// present" after it has been rubbed.
//
// Measured 2026-09-23 with cmd/scarabstage3test -phases ghostlamp (a Lamp given while the bank is
// open, so both 3214 and 2006 hold it): the guardian rubbed the real lamp in under two seconds
// (Fishing XP 0 -> 100), then handled the ghost in 2006 four more times and "gave up on Lamp after
// 4 attempts". The lab e2e mules, which bank before every trade, did the same for the rest of
// their runs while their MuleReceiver refused every hand-out as "busy with a random event".
//
// KnownInventories omits EMPTY components, which is why this asks State.Inventory whether the
// backpack is known rather than looking for it in that map: the ghost bites precisely when the
// real pack has just been emptied of the lamp. With no backpack known yet the old any-component
// scan stands.
func heldEventInventories(state *gamestate.State) map[int][]gamestate.SlotItem {
	invs := state.KnownInventories()
	if state.Inventory(actions.PlayerPack) == nil {
		return invs
	}
	return map[int][]gamestate.SlotItem{actions.PlayerPack: invs[actions.PlayerPack]}
}

// detectBoxLamp scans every inventory component the client has ever received an
// UPDATE_INV_FULL/PARTIAL for (gamestate.State.KnownInventories() — the same source
// internal/statlog.Logger already consumes) for a box/lamp item. Not restricted to
// "the player's own pack" specifically: this client has no fixed component-id constant
// for that yet — see COLLAB.md's write-up on this task for why — so in practice this
// checks whichever component is actually the backpack, which is normally the only one
// tracked while no bank/shop/trade screen is open.
func detectBoxLamp(r NameResolver, inventories map[int][]gamestate.SlotItem) *Event {
	return detectBoxLampEligible(r, inventories, nil)
}

func detectBoxLampEligible(r NameResolver, inventories map[int][]gamestate.SlotItem, accept func(*Event) bool) *Event {
	for comID, slots := range inventories {
		for _, it := range slots {
			info, ok := r.Obj(int(it.ID))
			if !ok || !matchesAnyName(info.Name, boxLampItemNames) {
				continue
			}
			ev := newEvent(KindBoxLamp, info.Name)
			ev.ItemID, ev.Slot, ev.InvComID = it.ID, it.Slot, comID
			if accept == nil || accept(ev) {
				return ev
			}
		}
	}
	return nil
}
