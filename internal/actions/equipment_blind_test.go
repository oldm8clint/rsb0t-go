package actions

// A blinded sidebar tab must not make a worn panel look empty. Content nulls tabs 0-6 in
// ~macro_events_set_tabs (any macro event) and for the whole of a ship crossing (~set_sail,
// ~set_sail_cairn), and a worn read taken then used to return nil — indistinguishable from
// wearing nothing, which is how a run once decided it had lost its gear.

import "testing"

// blind nulls a sidebar tab the way the server does: IF_SETICON carries 65535, which
// gamestate.applyIfSetIcon stores as -1.
func blind(f *fakeSession, tab int) { setIcon(f, tab, 65535) }

// The fallback must be the SAME id the tab resolves to, not a second opinion. What this pins is
// the RESOLUTION: that tabInvComponent walks the tab's root down to PlayerWorn and not to some
// other TYPE_INV component beneath it. It cannot notice the archive moving, because
// equipmentSession() hand-builds its components to match these constants and would keep agreeing
// with itself. TestConstantsMatchTheRealArchive (equipment_archive_test.go) is the one that
// parses the real archive and would.
func TestWornTabResolvesToPlayerWorn(t *testing.T) {
	_, e := equipmentSession()
	if got := e.wornComponent(); got != PlayerWorn {
		t.Fatalf("tab 4 resolved to %d, want PlayerWorn %d", got, PlayerWorn)
	}
}

func TestWornReadSurvivesANulledTab(t *testing.T) {
	f, e := equipmentSession() // leather body 1129 worn in slot 3 of 1688
	if !e.IsWorn(1129) {
		t.Fatalf("precondition: 1129 should read as worn before blinding")
	}
	blind(f, tabEquipment)

	if e.wornComponent() != -1 {
		t.Fatalf("precondition: tab 4 should be unresolvable after blinding")
	}
	worn := e.Worn()
	if len(worn) != 1 || worn[0].ID != 1129 {
		t.Fatalf("Worn() while blind = %v, want the leather body still readable", worn)
	}
	if !e.IsWorn(1129) {
		t.Fatalf("IsWorn(1129) while blind = false, want true")
	}
}

// The guards stay, and this pins that they do: Readable() still reports the blinding, and the
// two panels' ACTIONS still refuse, because the server rejects a click on a component that sits
// in no tab (Player.isComponentVisible).
func TestBlindingStillReportsAndStillRefusesActions(t *testing.T) {
	f, e := equipmentSession()
	blind(f, tabEquipment)
	if e.Readable() {
		t.Fatalf("Readable() = true while tab 4 is nulled, want false")
	}

	before := f.sendCount()
	if err := e.Unequip(1129); err != ErrNotOnScreen {
		t.Fatalf("Unequip while blind = %v, want ErrNotOnScreen", err)
	}
	if f.sendCount() != before {
		t.Fatalf("Unequip while blind sent %d packet(s), want none", f.sendCount()-before)
	}

	// Previously this returned nil — a silent false success, because IsWorn read the blinded
	// panel as empty and UnequipAndWait treats "not worn" as already done. Now the read sees the
	// item and the action reports honestly that it cannot be performed.
	if err := e.UnequipAndWait(1129, nil); err != ErrNotOnScreen {
		t.Fatalf("UnequipAndWait while blind = %v, want ErrNotOnScreen", err)
	}
}

// The pack's fallback must be the SAME id tab 3 resolves to, not a second opinion: that is what
// makes reading through it behaviour-neutral everywhere except the blinded case. As with the worn
// twin above, what this pins is the RESOLUTION and not the id — equipmentSession()'s components
// are hand-built to match these constants, so a content rebuild that moved 3214 would leave this
// test passing. TestConstantsMatchTheRealArchive (equipment_archive_test.go) is the drift check.
func TestPackTabResolvesToPlayerPack(t *testing.T) {
	_, e := equipmentSession()
	if got := e.inventoryComponent(); got != PlayerPack {
		t.Fatalf("tab 3 resolved to %d, want PlayerPack %d", got, PlayerPack)
	}
}

func TestPackReadSurvivesANulledTab(t *testing.T) {
	f, e := equipmentSession() // five occupied backpack slots, including 100 coins
	if got := len(e.Inventory()); got != 5 {
		t.Fatalf("precondition: pack should hold 5 slots before blinding, got %d", got)
	}
	blind(f, tabInventory)

	if e.inventoryComponent() != -1 {
		t.Fatalf("precondition: tab 3 should be unresolvable after blinding")
	}
	pack := e.Inventory()
	if len(pack) != 5 {
		t.Fatalf("Inventory() while blind = %v, want the pack still readable", pack)
	}
	if got := CountByID(pack, 995); got != 100 {
		t.Fatalf("coins while blind = %d, want 100", got)
	}
	// This is the reading that did the damage: blinded, a full pack and an empty one were
	// indistinguishable, so every free-slot and "do I still have it" check silently inverted.
	if got := FreeSlots(pack); got != BackpackCapacity-5 {
		t.Fatalf("FreeSlots while blind = %d, want %d", got, BackpackCapacity-5)
	}
}

// The guards stay on the pack side too, and the WRITES stay strict deliberately: the server gates
// OPHELD, INV_BUTTON and the whole OPLOCU/OPNPCU/OPOBJU/OPHELDU use-on family on
// Player.isComponentVisible, which needs the component's root layer to sit in a modal or a tab,
// so anything sent while tab 3 is null is rejected server-side anyway.
func TestPackBlindingStillReportsAndStillRefusesActions(t *testing.T) {
	f, e := equipmentSession()
	blind(f, tabInventory)
	if e.Readable() {
		t.Fatalf("Readable() = true while tab 3 is nulled, want false")
	}

	before := f.sendCount()
	if err := e.Equip(590); err != ErrNotOnScreen {
		t.Fatalf("Equip while blind = %v, want ErrNotOnScreen", err)
	}
	if err := e.EquipItem(testObjs, 1205); err != ErrNotOnScreen {
		t.Fatalf("EquipItem while blind = %v, want ErrNotOnScreen", err)
	}
	if err := e.UseOnLoc(1205, 3200, 3200, 1276); err != ErrNotOnScreen {
		t.Fatalf("UseOnLoc while blind = %v, want ErrNotOnScreen", err)
	}
	if err := e.UseOnHeld(1205, 590); err != ErrNotOnScreen {
		t.Fatalf("UseOnHeld while blind = %v, want ErrNotOnScreen", err)
	}
	if f.sendCount() != before {
		t.Fatalf("a blinded pack sent %d packet(s), want none", f.sendCount()-before)
	}
}
