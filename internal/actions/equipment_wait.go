package actions

import "time"

// Equipment.equip and Equipment.unequip from the TS scripting API wait up to 3 s for the worn
// panel to change before returning. Equip, EquipItem and Unequip in equipment.go and items.go
// only send; these add that wait.

// equipConfirmTimeout is upstream's wait for the worn panel.
const equipConfirmTimeout = 3 * time.Second

// EquipItemAndWait ports Equipment.equip with its wait: EquipItem, then until objID shows in the
// worn panel. An item already worn returns nil without sending.
func (e *Equipment) EquipItemAndWait(types ObjTypes, objID int32, w Waiter) error {
	w = waiterOr(w)
	if e.IsWorn(objID) {
		return nil
	}
	if err := e.EquipItem(types, objID); err != nil {
		return err
	}
	if !w.DelayUntil(func() bool { return e.IsWorn(objID) }, equipConfirmTimeout) {
		return ErrNotConfirmed
	}
	return nil
}

// UnequipAndWait ports Equipment.unequip with its wait: Remove on the worn item, then until it
// leaves the worn panel. An item not worn returns nil without sending, as upstream returns true.
func (e *Equipment) UnequipAndWait(objID int32, w Waiter) error {
	w = waiterOr(w)
	if !e.IsWorn(objID) {
		return nil
	}
	if err := e.Unequip(objID); err != nil {
		return err
	}
	if !w.DelayUntil(func() bool { return !e.IsWorn(objID) }, equipConfirmTimeout) {
		return ErrNotConfirmed
	}
	return nil
}

// UnequipByName is UnequipAndWait for the worn item whose config name equals name ignoring
// case, as upstream's unequip(name) takes. Nothing worn by that name returns nil.
func (e *Equipment) UnequipByName(types ObjTypes, name string, w Waiter) error {
	it, ok := FindByName(e.Worn(), types, name)
	if !ok {
		return nil
	}
	return e.UnequipAndWait(it.ID, w)
}
