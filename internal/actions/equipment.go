package actions

import (
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

// Sidebar tab indices for the two inv-shaped panels equipping needs, matching
// Client.ts's own `sideIcon` array position (see gamestate.State.SideIcon /
// IF_SETICON): tab 3 is always the backpack, tab 4 is always Worn Equipment. Neither
// panel's component id is a fixed literal the way Trade's ids are — the real client
// resolves them the same way, live, via ClientAdapter's findTabInvComponent.
const (
	tabInventory = 3
	tabEquipment = 4

	// wearOption is the backpack's held-item option that wears an equippable item.
	// Every equippable obj in this content build defines iop2 as Wear/Wield/Equip
	// (content/scripts/skill_crafting/configs/leather/leather_gear.obj's
	// `iop2=Wear`, .../areas/area_varrock/configs/varrock.inv's `iop2=Wield` for
	// bronze_sword, 87 obj configs total — none use iop1), and
	// content/scripts/player/scripts/equip.rs2 hooks it by object name, falling back
	// to the wildcard `[opheld2,_] ~equip(last_slot);` for anything with no more
	// specific override. Confirmed live this is genuinely a *different wire opcode*
	// from InvButton, not just a different option number on the same one: sending
	// INV_BUTTON2 on the backpack panel gets "No trigger for [inv_button2,inventory:
	// inv]" back — wearing goes out as OPHELD2 (see opHeld below), keyed server-side
	// by the clicked object's own name/wildcard, never by interface:component the way
	// inv_button's bank/wornitems verbs are.
	wearOption = 2

	// removeOption is the worn-items panel's own option that unequips an item, per
	// content/scripts/player/interfaces/wornitems.if's `[wear]` component
	// (`type=inv`, `option1=Remove`) and equip.rs2's
	// `[inv_button1,wornitems:wear] ~unequip(last_slot);` — this one really is
	// inv_button (an interface-specific verb, like bank's Withdraw), confirmed live.
	removeOption = 1
)

// PlayerWorn is the worn-items panel's own TYPE_INV component, the worn-side counterpart of the
// PlayerPack 3214 every runner names. Unlike the tab lookup below it cannot be taken away, and
// that is an engine property rather than luck:
//
//   - Player.setTab (engine/src/engine/entity/Player.ts:2121) only reassigns this.tabs[tab] and
//     writes IF_SETTAB. It never calls invStopListenOnCom, so if_settab(null, 4) cancels nothing.
//   - invStopListenOnCom is reachable from exactly two places: clearComListeners, which is only
//     ever called with a modal or overlay root (Player.ts:787, 798, 809, 2029), and the
//     inv_stoptransmit opcode. wornitems is a tab and never a modal, and none of content's 43
//     inv_stoptransmit calls names it. (43, not the 45 a grep reports: two of those hits are the
//     [command,inv_stoptransmit] and [command,.inv_stoptransmit] declarations in engine.rs2,
//     which define the opcode rather than call it.)
//   - So the inv_transmit(worn, wornitems:wear) listener ~initalltabs registers at login.rs2:101
//     stays registered, and UPDATE_INV keeps arriving for this component throughout a blinding.
//     The client never drops it either: gamestate has no handler for UPDATE_INV_STOP_TRANSMIT
//     and deletes no inventory anywhere.
//
// The id is 1688 (root layer 1644), read out of the engine's own client interface archive with
// internal/archive rather than guessed: it is the only TYPE_INV component in the entire archive
// whose first item option is "Remove", which is wornitems.if's [wear]. It is also exactly what
// the tab lookup resolves to when the tab IS readable, so the fallback below is the same answer,
// not a different one. TestConstantsMatchTheRealArchive re-derives that from the archive itself
// and is the only test here that would notice a content rebuild moving it.
const PlayerWorn = 1688

// PlayerPack is the backpack's own TYPE_INV component, the pack-side counterpart of PlayerWorn
// and the same 3214 nearly every runner package already declares a private copy of. Like the
// worn panel's, it cannot be taken away, and for the same engine reasons:
//
//   - Player.setTab (engine/src/engine/entity/Player.ts:2121) only reassigns this.tabs[tab] and
//     writes IF_SETTAB. It never calls invStopListenOnCom, so if_settab(null, 3) cancels nothing.
//   - invStopListenOnCom is reachable from exactly two places: clearComListeners, which is only
//     ever called with a modal or overlay root (Player.ts:787, 798, 809, 2029), and the
//     inv_stoptransmit opcode. None of content's 43 inv_stoptransmit calls names inventory:inv:
//     they are bank 2, trade 13, duel 12, partyroom 3, trawler 1, skill guide 1, shop 2,
//     jewellery 3 and smithing 6.
//   - inventory is never opened as a modal, and never as the overlay either. openMainOverlay
//     (Player.ts:2024) clears listeners only when the overlay is being CLOSED, and of content's
//     60 if_openoverlay calls not one passes inventory. That is the load-bearing fact and it is
//     checked by searching for the call, not by enumerating what content does open: the 15 named
//     overlays it passes (wilderness above and below ground, mazetimer, the agility arena, three
//     castlewars panels plus castlewars_shopside, duel, both boardgames, trawler, flamtaer_status,
//     the Trollweiss snow flakes and Eadgar's fade-to-black) are a list that grows whenever
//     content does, while "inventory is not among them" survives anything content adds.
//   - So the inv_transmit(inv, inventory:inv) listener ~initalltabs registers at login.rs2:98
//     stays registered, and UPDATE_INV keeps arriving for this component throughout a blinding.
//     The client never drops it either: gamestate has no handler for UPDATE_INV_STOP_TRANSMIT
//     and deletes no inventory anywhere.
//
// One constructible state does defeat that argument, and it is worth naming rather than leaving
// for the next reader to rediscover: ::openoverlay (ClientCheatHandler.ts:529, inside the
// staffModLevel >= 3 block) opens any component whose rootLayer is itself as the main overlay,
// and inventory's root layer 3213 qualifies, as does wornitems' 1644. ::closeoverlay then calls
// openMainOverlay(-1), which really does clearComListeners on it and really would stop the
// transmit. It takes a staff account issuing two cheats by hand, so it cannot happen to a bot on
// a production server — but it is the boundary of the guarantee, which is "no content script
// takes this listener away", not "nothing can".
//
// The id is 3214 (root layer 3213), read out of the engine's own client interface archive with
// internal/archive rather than inherited from the worn change: of the 37 TYPE_INV components in
// the archive's 11,942 it is the only one with item options enabled, "Use" offered and no
// item-option labels at all, which is inventory.if's [inv]; and it is layer 3213's only child.
// FindInvComponentIn(3213) returns 3214 under the very predicate tabInvComponent uses, so — as
// with PlayerWorn — the fallback below is the same answer the tab gives when it works, not a
// second opinion. TestConstantsMatchTheRealArchive re-derives all of that from the archive and is
// the drift check; the tab-resolution tests around it run on a hand-built fixture and cannot be.
const PlayerPack = 3214

// opHeldOpcodes maps held-item option 1..5 to the OPHELDn opcode Client.ts sends for
// an item's own generic op menu (Client.ts:9154-9184) — distinct from InvButton's
// INV_BUTTONn, which is for an *interface's* own button verbs (bank/trade/wornitems).
// Only option 4 carries an anti-cheat side-packet (ANTICHEAT_OPLOGIC9, Client.ts:9168-
// 9172), which OpHeld writes via anticheat.Counters.BeforeOpHeld4.
var opHeldOpcodes = [5]int{
	clientprot.OPHELD1,
	clientprot.OPHELD2,
	clientprot.OPHELD3,
	clientprot.OPHELD4,
	clientprot.OPHELD5,
}

// OpHeld ports the OPHELD1..5 branch of doAction: opcode for the option, then
// p2(objId) p2(slot) p2(comId) — same payload shape as InvButton, different opcode
// family and dispatch semantics server-side (see wearOption's doc comment above).
// Exported for callers outside this package that need a backpack item's own generic
// op menu (e.g. internal/randomevents' box/lamp handling — see its own doc comment for
// the live-caught bug where InvButton was used here by mistake: InvButton's INV_BUTTONn
// is for an *interface's* own iop-labeled button, like a bank/trade panel, and the
// server has no [inv_button1,...] trigger registered for a plain backpack item, only
// [opheld1,...] — sending the wrong one is silently rejected with a "No trigger for"
// debug message, not a visible error).
func OpHeld(s Session, option int, objID int32, slot int, comID int) error {
	if option < 1 || option > 5 {
		return errBadOption
	}
	// Option 1 is Eat, Drink and Open: an account whose input is held still eats (movehold.go).
	if option != 1 && interactionHeld(s) {
		return nil
	}
	return s.Send(func(w *packet.Packet) {
		if option == 4 {
			s.Cheats().BeforeOpHeld4(w)
		}
		w.P1Enc(byte(opHeldOpcodes[option-1]))
		w.P2(uint16(objID))
		w.P2(uint16(slot))
		w.P2(uint16(comID))
	})
}

// Equipment is the Equipment.* surface: wearing an item from the backpack and removing
// a worn one, plus reading both panels. Ported from equip.rs2 and wornitems.if —
// wearing has no interface of its own at all (it's just the ordinary backpack panel's
// second held-item option); only removing has a dedicated component. Bound to a
// session the same way Bank is, needing the parsed interface archive only to walk from
// a tab's root interface id down to its actual TYPE_INV component (see
// archive.FindInvComponentIn) — there's a single such component under each of these
// two roots, so root and target are frequently the same id, but this doesn't assume
// that in case a build wraps either root in a layer.
type Equipment struct {
	s       Session
	archive archive.Components
}

// NewEquipment binds an Equipment to a session using an already-parsed interface
// archive (see archive.ParseInterfaces via cmd/archivetest's fetch-and-parse sequence).
func NewEquipment(s Session, components archive.Components) *Equipment {
	return &Equipment{s: s, archive: components}
}

// tabInvComponent ports ClientAdapter.findTabInvComponent: the sidebar tab's live root
// interface id (IF_SETICON via gamestate.State.SideIcon), then a breadth-first search
// for its TYPE_INV component. For the equipment tab any TYPE_INV component qualifies
// (there's exactly one, the worn-items panel, and it doesn't necessarily have objOps
// set) — matching ClientAdapter's own `com.objOps === true || tabIndex === 4`.
func (e *Equipment) tabInvComponent(tab int) int {
	root := e.s.State().SideIcon(tab)
	if root == -1 {
		return -1
	}
	return archive.FindInvComponentIn(e.archive, root, func(c *archive.Component) bool {
		return c.ObjOps || tab == tabEquipment
	})
}

func (e *Equipment) inventoryComponent() int { return e.tabInvComponent(tabInventory) }
func (e *Equipment) wornComponent() int      { return e.tabInvComponent(tabEquipment) }

// InventoryComponent is the backpack's live component id, or -1 when the server has not set
// the tab up for this account (see Readable, and CLAUDE.md's OpLocU gotcha #4: an OPHELDU or
// OPLOCU naming a component the server does not consider visible is refused silently).
//
// Exported for a caller that must send SEVERAL use-on-item packets against one pack snapshot
// rather than one at a time — mulerunner's key-half joining, which fires the engine's whole
// per-tick allowance of OPHELDU at once and so cannot go through UseOnHeld, since that
// resolves "the first slot holding X" afresh on every call and would name the same two slots
// five times. Anything sending one at a time should keep using UseOnHeld.
func (e *Equipment) InventoryComponent() int { return e.inventoryComponent() }

// wornReadComponent is wornComponent for READS: the tab lookup while it works, and the fixed
// PlayerWorn component once the tab has been nulled. That is safe because the server keeps
// sending UPDATE_INV for that component throughout a blinding (see PlayerWorn), and because it
// is the same id the tab resolves to anyway.
//
// Only reads get the fallback. Equip and Unequip still resolve through wornComponent and still
// refuse while blind, deliberately: the server gates every inv_button and opheld on
// Player.isComponentVisible, which asks whether the component's root layer is currently in a
// modal or in a TAB, and while tab 4 is null it is in neither. A Remove sent then is rejected
// server-side, so the action has to keep saying it cannot be done.
func (e *Equipment) wornReadComponent() int {
	if comID := e.wornComponent(); comID != -1 {
		return comID
	}
	return PlayerWorn
}

// inventoryReadComponent is inventoryComponent for READS: the tab lookup while it works, and the
// fixed PlayerPack component once the tab has been nulled — the pack-side twin of
// wornReadComponent, safe for the same reasons (see PlayerPack) and returning the same id the
// tab resolves to anyway.
//
// Only reads get the fallback. Equip here, heldOp in items.go and heldSlot in useon.go still
// resolve through the strict inventoryComponent and still refuse while blind, deliberately: the
// server gates OPHELD1-5, INV_BUTTON and the whole OPLOCU/OPNPCU/OPOBJU/OPHELDU use-on family on
// Player.isComponentVisible, which asks whether the component's root layer is currently in a
// modal or in a TAB, and while tab 3 is null it is in neither. Anything sent then is rejected
// server-side, so those calls have to keep saying it cannot be done.
func (e *Equipment) inventoryReadComponent() int {
	if comID := e.inventoryComponent(); comID != -1 {
		return comID
	}
	return PlayerPack
}

// Readable reports whether the backpack and worn panels both resolve to a real component right
// now, so a caller can tell "the account is holding nothing" apart from "this client cannot see
// what the account is holding".
//
// The distinction is not theoretical and it is not only the fresh-account case. Several macro
// events take the tabs away mid-run: the mime's entry proc calls ~macro_events_set_tabs
// (content, macro_events.rs2:218), which is if_settab(null, ...) for tabs 0-6, 11 and 12 —
// backpack and worn included, and tab 3 goes away in eleven content places all told: the shared
// ~macro_events_set_tabs, both ship procs (~set_sail, ~set_sail_cairn/captain_shanks),
// misc_sailor, boardgames (twice — the second is the other player's .-prefixed form),
// routequest_morton, route_cutscene, viking_sailor, castlewars_transmog and tutorial. The server
// sends IF_SETICON 65535 and gamestate stores -1, so the tab lookup finds nothing for as long as
// the event holds the account, which is until [mapzoneexit] runs ~initalltabs. A script that read
// that as an empty pack decided it had lost its gear, and it ended a 23-minute run on the
// reading.
//
// Neither Worn() nor Inventory() returns nil while blind any more — they read the fixed
// PlayerWorn and PlayerPack components, which the server keeps updating throughout — but this
// stays, and every caller's guard stays with it, for two reasons: the ACTIONS on both panels
// still refuse while blind, because the server will not accept a click on a component that is in
// no tab; and a caller that wants to know whether it is seeing the account through its own
// sidebar still deserves a way to ask. Belt and braces, not a replacement.
//
// This only ever reports whether the panels can be READ. It sends nothing and changes nothing.
func (e *Equipment) Readable() bool {
	return e.inventoryComponent() != -1 && e.wornComponent() != -1
}

// Inventory returns the backpack's contents. Read through inventoryReadComponent, so a nulled
// tab 3 — a macro event, or the whole of a ship crossing — no longer turns a full pack into an
// empty-looking one.
func (e *Equipment) Inventory() []gamestate.SlotItem {
	return items(e.s.State(), e.inventoryReadComponent())
}

// Worn returns what's currently equipped. Read through wornReadComponent, so a nulled tab 4 —
// a macro event, or the whole of a ship crossing — no longer turns a full worn panel into an
// empty-looking one.
func (e *Equipment) Worn() []gamestate.SlotItem {
	return items(e.s.State(), e.wornReadComponent())
}

// IsWorn reports whether objID currently appears in the worn-items panel.
func (e *Equipment) IsWorn(objID int32) bool {
	for _, it := range e.Worn() {
		if it.ID == objID {
			return true
		}
	}
	return false
}

// Equip wears objID from the backpack via its Wield/Wear/Equip option (OPHELD2, held
// option 2 — not InvButton; see wearOption's doc comment).
func (e *Equipment) Equip(objID int32) error {
	comID := e.inventoryComponent()
	if comID == -1 {
		return ErrNotOnScreen
	}
	for _, it := range e.Inventory() {
		if it.ID == objID {
			return OpHeld(e.s, wearOption, it.ID, it.Slot, comID)
		}
	}
	return ErrItemNotFound
}

// Unequip removes objID from the worn-items panel (option1=Remove).
func (e *Equipment) Unequip(objID int32) error {
	comID := e.wornComponent()
	if comID == -1 {
		return ErrNotOnScreen
	}
	for _, it := range e.Worn() {
		if it.ID == objID {
			return InvButton(e.s, removeOption, it.ID, it.Slot, comID)
		}
	}
	return ErrItemNotFound
}
