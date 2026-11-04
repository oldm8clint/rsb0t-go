package actions

import (
	"strings"

	"rsb0t-go/internal/gamestate"
)

// Inventory helpers: the TS scripting API's Inventory.first/contains/count/countById/used/
// free/isFull over any item panel's slots (Equipment.Inventory, Bank.Inventory or Shop.Pack,
// whichever view of the backpack is on screen). Names come from the object config and match the
// way upstream matches them: whole name, ignoring case.

// BackpackCapacity is the backpack's slot count.
const BackpackCapacity = 28

// CountByID ports Inventory.countById: the total count of objID across items.
func CountByID(items []gamestate.SlotItem, objID int32) int32 {
	return sumCount(items, objID)
}

// FindByName ports Inventory.first: the first slot whose config name equals name ignoring
// case. It finds nothing with nil types.
func FindByName(items []gamestate.SlotItem, types ObjTypes, name string) (gamestate.SlotItem, bool) {
	if types == nil {
		return gamestate.SlotItem{}, false
	}
	for _, it := range items {
		if obj, ok := types.Obj(int(it.ID)); ok && strings.EqualFold(obj.Name, name) {
			return it, true
		}
	}
	return gamestate.SlotItem{}, false
}

// CountByName ports Inventory.count: the total count of every slot whose config name equals
// name ignoring case, so noted and unnoted stacks of the same name add up.
func CountByName(items []gamestate.SlotItem, types ObjTypes, name string) int32 {
	if types == nil {
		return 0
	}
	var total int32
	for _, it := range items {
		if obj, ok := types.Obj(int(it.ID)); ok && strings.EqualFold(obj.Name, name) {
			total += it.Count
		}
	}
	return total
}

// FreeSlots ports Inventory.free for the 28-slot backpack.
func FreeSlots(items []gamestate.SlotItem) int {
	if free := BackpackCapacity - len(items); free > 0 {
		return free
	}
	return 0
}

// IsFull ports Inventory.isFull for the 28-slot backpack.
func IsFull(items []gamestate.SlotItem) bool {
	return len(items) >= BackpackCapacity
}
