package woodcutrunner

// The live half of the two woodcutting random events this script owns: putting an axe back
// together after its head flies off, and getting a broken axe repaired at Bob's.
//
// The third event, the Tree spirit (Dryad, npc 438-443), is deliberately NOT here:
// internal/randomevents already carries that id range in its own hostileEventNpcIDs, raises the
// ordinary evade, scopes it to the account the monster is actually targeting, and runs the
// shared walking flee. WaitForGuardian at the top of every Loop already inherits all of it. See
// internal/scripts/woodcutplan/events.go's header for the full argument and the content
// evidence; writing a third flee here would re-introduce the bug that scoping fixed.
//
// The Ent needs no handler either, for a reason worth repeating where someone will see it: an
// Ent REPLACES the tree (`loc_del(60)` then `npc_add` at the same coord), and the axe only
// breaks through `[opnpc1/3,_macro_event_ent]` — you have to click the Ent as an NPC. This
// package chops by loc through the loc index and never sends OpNpc at a tree, so it cannot
// reach the axe-breaking path. EntAt below is defence in depth: it lets the caller skip a tile
// an Ent is standing on instead of re-clicking a tree that has been deleted out from under it.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// takeObjOption is a ground item's default "Take". Client.ts's own menu builder falls back to
// OP_OBJ3 for an item with no bespoke option, which an axe head has none of.
const takeObjOption = 3

// EntAt reports whether one of the Ent NPCs (woodcutplan.IsEntNpc, ids 444-452) is standing on
// or immediately beside a tree's tile.
//
// Matched by NPC ID, never by name: every Ent carries the display name of the tree it replaced
// — `macro_ent_dead_tree1` is literally "Dead tree" — so a name match would hit every real tree
// in the camp and blocklist the lot.
//
// Radius 1 rather than 0 because a 2x2 or 3x3 tree's loc coord and the Ent's spawn coord are the
// same square but the tree's own footprint spreads from it, so an exact-tile test would miss the
// larger tiers.
func EntAt(d *Deps, at tile.Tile) bool {
	for _, npc := range d.Session.State().NPCs() {
		if !woodcutplan.IsEntNpc(npc.Type) {
			continue
		}
		if npc.Level == at.Level && tile.Chebyshev(tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}, at) <= 1 {
			return true
		}
	}
	return false
}

// HeldNamed reports whether the pack holds an item with that exact name.
func HeldNamed(d *Deps, name string) bool { return packCountByName(d, name) > 0 }

// HasWorkingAxe reports whether anything that can actually chop is held or worn. A handle, a
// head and a broken axe are all specifically NOT axes (woodcutplan.IsAxe excludes them), which
// is what keeps this honest.
func HasWorkingAxe(d *Deps) bool { return len(HeldAxes(d)) > 0 }

// AxeHeadOnGround finds a dropped "Axe head" within woodcutplan.AxeHeadSearchRadius of the
// account, nearest first.
//
// Content throws it with `map_findsquare(coord, 1, 7, ^map_findsquare_lineofwalk)`, so it is
// never on the account's own tile and never further than 7 — hence a search rather than a fixed
// pickup. Name matching is safe here (unlike for the Ents): content gives every tier's head the
// single shared name "Axe head".
func AxeHeadOnGround(d *Deps) (int, tile.Tile, bool) {
	here := currentTile(d)
	bestDist := woodcutplan.AxeHeadSearchRadius + 1
	var bestAt tile.Tile
	bestID, found := 0, false
	for _, g := range d.Session.State().GroundItems() {
		obj, ok := d.Config.Obj(g.ID)
		if !ok || !woodcutplan.IsAxeHead(obj.Name) {
			continue
		}
		at := tile.Tile{X: g.X, Z: g.Z, Level: g.Level}
		if at.Level != here.Level {
			continue
		}
		if dist := tile.Chebyshev(here, at); dist <= woodcutplan.AxeHeadSearchRadius && dist < bestDist {
			bestDist, bestAt, bestID, found = dist, at, g.ID, true
		}
	}
	return bestID, bestAt, found
}

// BrokenAxeHeld is the object id of a broken axe in the pack or the worn slot, and the axe it
// would become. found is false when there is none.
//
// By ID, because all seven broken hatchets share the display name "Broken axe" and only the id
// says which metal — a bronze one is worth nothing to repair and a rune one is worth 427 coins
// and a trip across the world. See woodcutplan.BrokenAxeIDs.
func BrokenAxeHeld(d *Deps) (id int, repaired string, found bool) {
	// The BEST one, not the first slot. An account can be carrying two (an Ent broke the good
	// axe and a cheap one broke earlier), and slot order is arbitrary — repairing the bronze
	// one first would spend the whole journey on the axe that is worth nothing. Found live
	// 2026-09-20: a pack holding a broken bronze and a broken rune axe answered "bronze".
	consider := func(objID int32) {
		r := woodcutplan.RepairedAxe(int(objID))
		if r == "" {
			return
		}
		if !found || woodcutplan.BetterAxe(r, repaired) {
			id, repaired, found = int(objID), r, true
		}
	}
	if inv := d.Session.State().Inventory(PlayerPack); inv != nil {
		for _, it := range inv.NonEmpty() {
			if obj, ok := d.Config.Obj(int(it.ID)); ok && !obj.Noted() {
				consider(it.ID)
			}
		}
	}
	if ContainersReadable(d) {
		for _, it := range actions.NewEquipment(d.Session, d.Components).Worn() {
			consider(it.ID)
		}
	}
	return id, repaired, found
}

// EventSnapshot builds woodcutplan.EventSnapshot from live state.
//
// bankedAxes is what the caller's last bank read saw (internal/bot/woodcutter.go keeps one);
// nil is read as "nothing known to fall back on", which is the conservative side.
func EventSnapshot(d *Deps, bankedAxes []string) woodcutplan.EventSnapshot {
	_, _, onGround := AxeHeadOnGround(d)
	brokenID, _, broken := BrokenAxeHeld(d)
	held := HeldAxes(d)
	return woodcutplan.EventSnapshot{
		HasHandle:    HeldNamed(d, woodcutplan.AxeHandle) || wornNamed(d, woodcutplan.AxeHandle),
		HasHead:      HeldNamed(d, woodcutplan.AxeHead),
		HeadOnGround: onGround,
		// Both readings kept: the name test is what content guarantees, the id test is what
		// says which tier. An id outside the seven (content changed under us) still reads as
		// a broken axe through the name, and DecideWoodEvent has a branch for exactly that.
		HasBrokenAxe:  broken || HeldNamed(d, woodcutplan.BrokenAxe) || wornNamed(d, woodcutplan.BrokenAxe),
		BrokenAxeID:   brokenID,
		HasWorkingAxe: len(held) > 0,
		HeldBest:      woodcutplan.BestAxe(held),
		BankedAxes:    bankedAxes,
	}
}

// wornNamed reports whether the worn panel holds an item by name. Content puts the handle and
// the broken axe straight into the right hand when the original was wielded
// (`inv_setslot(worn, ^wearpos_rhand, ...)`), so the pack alone is not enough to notice either.
//
// Returns false when the worn panel cannot be read at all rather than guessing — the caller's
// own snapshot already refuses to decide anything while ContainersReadable is false.
func wornNamed(d *Deps, name string) bool {
	if !ContainersReadable(d) {
		return false
	}
	for _, it := range actions.NewEquipment(d.Session, d.Components).Worn() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(strings.TrimSpace(obj.Name), name) {
			return true
		}
	}
	return false
}

// AxeHeadResult is one recovery attempt's outcome.
type AxeHeadResult struct {
	// Fixed is true only once an ordinary axe is confirmed back in the pack.
	Fixed bool
	// Reason is why not.
	Reason string
	// Stopped is true when the caller's own Delay asked to stop.
	Stopped bool
}

// RecoverAxeHead picks up the thrown head and re-attaches it to the handle.
//
// Bounded throughout, and deliberately brisk: the head is dropped with `^lootdrop_duration`
// (200 ticks, about two minutes at a 600ms tick), after which it is gone and the axe with it.
// That is why the caller runs this ahead of ordinary work rather than at the next convenient
// moment.
//
// Two things the first version could not do, both fixed 2026-10-04:
//   - A FULL PACK. Live 2026-09-22, Acct171 noticed the event at the Seers bank with 28 slots
//     full and logged "the pack is full, so the head cannot be picked up" until the head was
//     gone. A log or an unstrung bow is dropped to make room (woodcutplan.SpareForAxeHead).
//   - A WORN HANDLE. Content puts the handle in the right hand when the axe was wielded, and
//     reattach_axe reads both pieces from the inventory, so the handle is taken off first.
func RecoverAxeHead(d *Deps) AxeHeadResult {
	headHeld := packCountByName(d, woodcutplan.AxeHead) > 0
	_, handleInPack := packItemIDByName(d, woodcutplan.AxeHandle)
	wornHandleID, handleWorn := wornItemIDByName(d, woodcutplan.AxeHandle)
	handleWorn = handleWorn && !handleInPack

	var headID int
	var headAt tile.Tile
	if !headHeld {
		id, at, ok := AxeHeadOnGround(d)
		if !ok {
			return AxeHeadResult{Reason: "no axe head in sight"}
		}
		headID, headAt = id, at
	}
	if main, _, _ := d.Session.State().Modals(); main != gamestate.NoModal {
		// A bank left open blocks both the drop and the use-on-item (the engine refuses an
		// OPHELD from a component that is not on screen).
		_ = actions.CloseModal(d.Session)
		d.Delay(600 * time.Millisecond)
	}
	if need := woodcutplan.AxeHeadSlotsNeeded(headHeld, handleWorn); FreeSlots(d) < need {
		if !makeRoomForAxeHead(d, need) {
			return AxeHeadResult{Reason: "the pack is full and holds no log or bow to drop, so the head cannot be picked up"}
		}
	}
	if !headHeld {
		d.logf("axe head landed at %d,%d — going to get it", headAt.X, headAt.Z)
		// Walk onto the tile itself: a ground item is taken from its own square, not from beside
		// it, so the orthogonal-interaction rule that applies to locs does not apply here.
		if tile.Chebyshev(currentTile(d), headAt) > 0 {
			if !WalkNear(d, headAt, 0) {
				return AxeHeadResult{Reason: "could not reach the axe head"}
			}
		}
		before := packCountByName(d, woodcutplan.AxeHead)
		if err := actions.OpObj(d.Session, takeObjOption, headAt.X, headAt.Z, headID); err != nil {
			d.logf("taking the axe head: %v", err)
			return AxeHeadResult{Reason: "could not pick the head up"}
		}
		if !waitFor(d, "the axe head to reach my pack", 5*time.Second, func() bool {
			return packCountByName(d, woodcutplan.AxeHead) > before
		}) {
			return AxeHeadResult{Reason: "the head never reached my pack"}
		}
	}
	if handleWorn {
		if err := actions.NewEquipment(d.Session, d.Components).Unequip(wornHandleID); err != nil {
			d.logf("taking the axe handle off: %v", err)
			return AxeHeadResult{Reason: "could not take the worn axe handle off"}
		}
		if !waitFor(d, "the axe handle to reach my pack", 5*time.Second, func() bool {
			_, ok := packItemIDByName(d, woodcutplan.AxeHandle)
			return ok
		}) {
			return AxeHeadResult{Reason: "the worn axe handle never reached my pack"}
		}
	}
	return attachAxeHead(d)
}

// wornItemIDByName is the object id of a worn item with that name. False when the worn panel
// cannot be read, which the caller treats as "not worn" (the pack is then all there is to use).
func wornItemIDByName(d *Deps, name string) (int32, bool) {
	if !ContainersReadable(d) {
		return 0, false
	}
	for _, it := range actions.NewEquipment(d.Session, d.Components).Worn() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(strings.TrimSpace(obj.Name), name) {
			return it.ID, true
		}
	}
	return 0, false
}

// makeRoomForAxeHead drops spare pack items (woodcutplan.SpareForAxeHead) until need slots are
// free, each drop confirmed by the pack losing a slot. False when it cannot get there.
func makeRoomForAxeHead(d *Deps, need int) bool {
	eq := actions.NewEquipment(d.Session, d.Components)
	for tries := 0; FreeSlots(d) < need && tries < need; tries++ {
		id, name, ok := spareSlotForAxeHead(d)
		if !ok {
			return false
		}
		free := FreeSlots(d)
		if err := eq.InteractHeld(d.Config, id, "Drop"); err != nil {
			d.logf("dropping a %s for the axe head: %v", strings.ToLower(name), err)
			return false
		}
		if !waitFor(d, "the drop", 3*time.Second, func() bool { return FreeSlots(d) > free }) {
			return false
		}
		d.logf("dropped a %s to make room for the axe head", strings.ToLower(name))
	}
	return FreeSlots(d) >= need
}

// packHoldsID reports whether a pack slot holds exactly that object id.
func packHoldsID(d *Deps, id int32) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return false
	}
	for _, it := range inv.NonEmpty() {
		if it.ID == id {
			return true
		}
	}
	return false
}

// spareSlotForAxeHead is the first droppable pack item, logs before bows.
func spareSlotForAxeHead(d *Deps) (int32, string, bool) {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0, "", false
	}
	var bowID int32
	bowName := ""
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Noted() || obj.Stackable || !woodcutplan.SpareForAxeHead(obj.Name) {
			continue
		}
		if woodcutplan.IsLogs(obj.Name) {
			return it.ID, obj.Name, true
		}
		if bowName == "" {
			bowID, bowName = it.ID, obj.Name
		}
	}
	return bowID, bowName, bowName != ""
}

// attachAxeHead joins a held head to a held handle.
//
// Content accepts the use either way round — `[opheldu,macro_hatchethandle]` switches on the
// head, and each `[opheldu,macro_*_hatchethead]` goes through `check_axe_head` — so only one
// direction is sent and there is no fallback to try the other.
func attachAxeHead(d *Deps) AxeHeadResult {
	handleID, ok := packItemIDByName(d, woodcutplan.AxeHandle)
	if !ok {
		return AxeHeadResult{Reason: "no axe handle held"}
	}
	headID, ok := packItemIDByName(d, woodcutplan.AxeHead)
	if !ok {
		return AxeHeadResult{Reason: "no axe head held"}
	}
	axesBefore := len(HeldAxes(d))
	if err := actions.NewEquipment(d.Session, d.Components).UseOnHeld(handleID, headID); err != nil {
		d.logf("attaching the axe head: %v", err)
		return AxeHeadResult{Reason: "the engine refused the re-attach"}
	}
	// The receipt is a working AXE appearing — not the handle vanishing, and not a chat line.
	// Content's reattach_axe deletes both pieces and adds `oc_param($axe_head, fixed)`, so an
	// ordinary axe in the pack is the only thing that proves it worked.
	if !waitFor(d, "the repaired axe to appear", 5*time.Second, func() bool {
		return len(HeldAxes(d)) > axesBefore
	}) {
		return AxeHeadResult{Reason: "the pieces did not become an axe"}
	}
	d.logf("axe put back together")
	return AxeHeadResult{Fixed: true}
}

// ---- Bob's repairs ----

// RepairResult is one repair attempt's outcome, so a caller can tell "Bob fixed it" from "we
// never got there" from "we could not pay" — three things the old bool could not separate.
type RepairResult struct {
	// Fixed is true only once the SPECIFIC repaired axe is confirmed in the pack.
	Fixed bool
	// Axe is the axe that came back, on Fixed.
	Axe string
	// Paid is how many coins actually left, on Fixed.
	Paid int
	// Reason is why not.
	Reason string
}

func repairRefused(reason string) RepairResult { return RepairResult{Reason: reason} }

// RepairAtBob takes a broken axe to Bob's Brilliant Axes and has it repaired.
//
// The interaction is a use-item-on-NPC, not a shop: `[opnpcu,bob]` checks
// `oc_param($item, to_be_fixed_by_bob)` and then asks a two-option question, so this answers the
// dialogue rather than driving a shop interface. The cost is `oc_cost($item)`.
//
// The coins are checked BEFORE travelling, for the same reason market.go checks them before
// walking to the market: Seers -> Lumbridge is a cross-map leg over two paid boat fares, and
// arriving unable to pay spends all of that for nothing. bob.rs2's own first branch is
// `if (inv_total(inv, coins) < $cost) { ~chatnpc("I'll need <cost>gp...") ; return }`, so a
// short account gets a chat line and no repair — which, to a script that does not read
// MESSAGE_GAME, looks exactly like the whole thing silently failing.
func RepairAtBob(d *Deps, stand tile.Tile) RepairResult {
	brokenID, repaired, held := BrokenAxeHeld(d)
	if !held {
		return repairRefused("no broken axe held")
	}
	cost := woodcutplan.BrokenAxeRepairCost[repaired]
	if coins := CoinsHeld(d); coins < cost {
		return repairRefused(fmt.Sprintf("Bob wants %d coins for a %s and the pack holds %d", cost, strings.ToLower(repaired), coins))
	}
	if tile.Chebyshev(currentTile(d), stand) > 3 {
		// Cross-region: Bob is in Lumbridge and this script works out of Seers. TravelTo
		// plans over webwalk's door/stair/ship-fare graph with the long budget; plain
		// WalkNear answered "unreachable" here every time it was tried (see Deps.Walker).
		d.logf("taking a broken %s to Bob's Axes in Lumbridge (%d coins to repair)", strings.ToLower(repaired), cost)
		if !TravelTo(d, stand, 2) {
			return repairRefused("could not reach Bob's Axes")
		}
	}
	npc, found := nearestNpc(d, "Bob", stand, 10)
	if !found {
		return repairRefused("Bob is not in sight")
	}
	if !walkToInteract(d, tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}) {
		return repairRefused("could not stand where Bob will take the axe")
	}
	// bob.rs2 takes the axe out of the INVENTORY (inv_delslot(inv, $slot)), and an Ent puts the
	// broken axe in the right hand when the axe was wielded, so a worn one comes off first.
	if !packHoldsID(d, int32(brokenID)) {
		if err := actions.NewEquipment(d.Session, d.Components).Unequip(int32(brokenID)); err != nil {
			d.logf("taking the broken axe off: %v", err)
			return repairRefused("could not take the worn broken axe off")
		}
		if !waitFor(d, "the broken axe to reach my pack", 5*time.Second, func() bool { return packHoldsID(d, int32(brokenID)) }) {
			return repairRefused("the worn broken axe never reached my pack")
		}
	}
	coinsBefore := CoinsHeld(d)
	repairedBefore := packCountByName(d, repaired)
	if err := actions.NewEquipment(d.Session, d.Components).UseOnNpc(int32(brokenID), npc.Index); err != nil {
		d.logf("offering the broken axe to Bob: %v", err)
		return repairRefused("the engine refused the use-on-Bob")
	}
	// Bob asks "...repair it for Ngp?" with a two-option choice. Answer the affirmative one by
	// name rather than by position, so a reordered dialogue cannot decline by accident.
	dlg := actions.NewChatDialog(d.Session, d.Components)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		// The receipt is the SPECIFIC axe this broken one becomes appearing in the pack —
		// not "more axe rows than before", which a level-up dialogue or a second account
		// dropping one could also produce, and not a chat line.
		if packCountByName(d, repaired) > repairedBefore {
			paid := coinsBefore - CoinsHeld(d)
			d.logf("Bob repaired the axe: %s back for %d coins", strings.ToLower(repaired), paid)
			return RepairResult{Fixed: true, Axe: repaired, Paid: paid}
		}
		if opts := dlg.Options(); len(opts) > 0 {
			// Bob asks "Yes, please." / "No, thank you.". Matched by TEXT, never by
			// position, so a reordered dialogue declines the repair instead of accepting it
			// by accident — ChooseOption("") would press whatever happens to be first.
			if err := dlg.ChooseOption("yes"); err != nil {
				d.logf("Bob's dialogue offered no yes option (%v): %v", optionTexts(opts), err)
				return repairRefused("Bob's dialogue had no yes option")
			}
		} else if dlg.CanContinue() {
			_ = dlg.Continue()
		}
		if !d.Delay(600 * time.Millisecond) {
			return repairRefused("stopped")
		}
	}
	return repairRefused("Bob did not hand the axe back in time")
}

// optionTexts is just the visible text of a dialogue's options, for a log line.
func optionTexts(opts []actions.Option) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		out = append(out, o.Text)
	}
	return out
}
