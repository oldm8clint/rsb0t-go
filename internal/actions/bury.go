package actions

// buryOption is bones' own held-item option that buries them, per
// content/scripts/skill_prayer/scripts/bury_bone.rs2's
// `[opheld1,_bones] @bury_bones(last_slot);` and content/scripts/skill_prayer/configs/
// bones.obj's `iop1=Bury` (every bone variant defined there — bones, bones_burnt,
// bat_bones, big_bones, babydragon_bones, dragon_bones, wolf_bones, and the TBWT jogre
// variants — sets iop1=Bury, none use a different option). Same OPHELD family
// equipment.go's Equip uses for OPHELD2 (Wear/Wield), confirmed by the same
// content-script pattern: dispatched server-side by the bones object's own name/
// wildcard handler (`_bones`), not by interface:component the way InvButton is.
const buryOption = 1

// Bury sends bones' own generic op-menu action (OPHELD1) to bury objID out of the
// backpack slot slot, in the backpack's live TYPE_INV component comID (see
// equipment.go's tabInvComponent/inventoryComponent for how a caller resolves that
// live, e.g. via Equipment.inventoryComponent() or the confirmed-stable literal 3214 —
// see COLLAB.md). Uses the same opHeld helper and wire payload
// (p2(objId) p2(slot) p2(comId)) equipment.go's Equip already proved live for OPHELD2;
// bury is the same family at option 1, just a different object-side op-menu entry, not
// a different opcode family or payload shape.
func Bury(s Session, objID int32, slot int, comID int) error {
	return OpHeld(s, buryOption, objID, slot, comID)
}

// eatOption is food's own held-item option, confirmed against content (branch 289):
// content/scripts/skill_cooking/configs/cooking_generic/food_cooked.obj's `iop1=Eat`
// (checked across cooked_chicken, cooked_meat, lava_eel, cooked_ugthanki_meat — every
// variant sampled sets iop1=Eat) — same OPHELD1 family bury_bone.rs2's bones use, just a
// different object-side op-menu entry.
const eatOption = 1

// Eat sends food's own generic op-menu action (OPHELD1) to eat objID out of the backpack
// slot slot, in the backpack's live TYPE_INV component comID. Same family/wire shape as
// Bury and Equip's OPHELD2 — see Bury's own doc comment for the shared precedent.
func Eat(s Session, objID int32, slot int, comID int) error {
	return OpHeld(s, eatOption, objID, slot, comID)
}

// openOption is a casket's own held-item option, confirmed against content (branch 289):
// content/scripts/skill_fishing/configs/fishing.obj's [casket] block sets `iop1=Open` — same
// OPHELD1 family bury_bone.rs2/food_cooked.obj use, just a different object-side op-menu
// entry (name "Casket", not a fixed id — CasketOpener resolves it live via
// cachecfg.Config.ObjIDs("Casket") rather than the TS source's own hardcoded 405, since this
// client's content pack numbering is not guaranteed to match rsb0t-scripts' upstream ids).
const openOption = 1

// Open sends a casket's own generic op-menu action (OPHELD1) to open objID out of the
// backpack slot slot, in the backpack's live TYPE_INV component comID. Same family/wire shape
// as Bury/Eat — see Bury's own doc comment for the shared precedent.
func Open(s Session, objID int32, slot int, comID int) error {
	return OpHeld(s, openOption, objID, slot, comID)
}
