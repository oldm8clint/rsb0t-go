package actions

import (
	"errors"
	"regexp"
	"strings"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
)

// Item options by name: the TS scripting API's GroundItem.interact, InvItem.interact and
// Equipment.equip, which all pick an option by its label rather than its number. Labels come
// from the object's config (internal/cachecfg), with the same client-side defaults
// ClientAdapter's groundOps and heldOps apply: a ground item with no third option gets "Take",
// and an inventory item with no fifth option gets "Drop".

// ObjTypes resolves object ids to their config; *cachecfg.Config satisfies it.
type ObjTypes interface {
	Obj(id int) (cachecfg.Obj, bool)
}

var (
	// ErrNoSuchAction is returned when an item or entity has no option with the requested label.
	ErrNoSuchAction = errors.New("actions: no option with that label")
)

// GroundOps ports groundOps: ops with "Take" in the third slot when the type leaves it empty.
func GroundOps(ops [5]string) [5]string {
	if ops[2] == "" {
		ops[2] = "Take"
	}
	return ops
}

// HeldOps ports heldOps: inventory ops with "Drop" in the fifth slot when the type leaves it
// empty.
func HeldOps(iops [5]string) [5]string {
	if iops[4] == "" {
		iops[4] = "Drop"
	}
	return iops
}

// OptionIndex ports opIndex: the 1-based slot whose label equals action ignoring case, or 0.
func OptionIndex(ops [5]string, action string) int {
	for i, op := range ops {
		if op != "" && strings.EqualFold(op, action) {
			return i + 1
		}
	}
	return 0
}

// InteractGroundItem ports GroundItem.interact: OP_OBJn for the option labelled action. With nil
// types only the default "Take" resolves.
func InteractGroundItem(s Session, types ObjTypes, item gamestate.GroundItem, action string) error {
	var ops [5]string
	if types != nil {
		if obj, ok := types.Obj(item.ID); ok {
			ops = obj.Ops
		}
	}
	option := OptionIndex(GroundOps(ops), action)
	if option == 0 {
		return ErrNoSuchAction
	}
	return OpObj(s, option, item.X, item.Z, item.ID)
}

// TakeGroundItem picks up a ground item with its "Take" option. Like OpObj it does not walk.
func TakeGroundItem(s Session, types ObjTypes, item gamestate.GroundItem) error {
	return InteractGroundItem(s, types, item, "Take")
}

var equipVerb = regexp.MustCompile(`(?i)wield|wear|equip`)

// EquipOption ports Equipment.equip's verb choice: the first inventory option whose label
// contains Wield, Wear or Equip, 1-based, or 0 when the item cannot be equipped.
func EquipOption(iops [5]string) int {
	for i, op := range iops {
		if equipVerb.MatchString(op) {
			return i + 1
		}
	}
	return 0
}

// EquipItem wears objID from the backpack with the option its config labels Wield, Wear or
// Equip. With nil types, or an id the config does not know, it falls back to held option 2,
// which Equip uses for every equippable in this content build.
func (e *Equipment) EquipItem(types ObjTypes, objID int32) error {
	option := wearOption
	if types != nil {
		if obj, ok := types.Obj(int(objID)); ok {
			if option = EquipOption(obj.IOps); option == 0 {
				return ErrNoSuchAction
			}
		}
	}
	return e.heldOp(option, objID)
}

// ContainsWorn ports Equipment.contains: a worn item's config name equals name, ignoring case.
func (e *Equipment) ContainsWorn(types ObjTypes, name string) bool {
	_, ok := FindByName(e.Worn(), types, name)
	return ok
}

// InteractHeld ports InvItem.interact for the backpack: OPHELDn for the option labelled action
// on the first backpack slot holding objID.
func (e *Equipment) InteractHeld(types ObjTypes, objID int32, action string) error {
	if types == nil {
		return ErrNoSuchAction
	}
	obj, ok := types.Obj(int(objID))
	if !ok {
		return ErrNoSuchAction
	}
	option := OptionIndex(HeldOps(obj.IOps), action)
	if option == 0 {
		return ErrNoSuchAction
	}
	return e.heldOp(option, objID)
}

func (e *Equipment) heldOp(option int, objID int32) error {
	comID := e.inventoryComponent()
	if comID == -1 {
		return ErrNotOnScreen
	}
	for _, it := range e.Inventory() {
		if it.ID == objID {
			return OpHeld(e.s, option, it.ID, it.Slot, comID)
		}
	}
	return ErrItemNotFound
}
