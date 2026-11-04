package entityquery

import (
	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
)

// UseItemOn ports InvItem.useOn for a queried target: the first backpack slot holding objID
// used on an NPC (OPNPCU), a loc (OPLOCU) or a ground item (OPOBJU). Using an item on another
// backpack item is actions.Equipment.UseOnHeld. Players return ErrNoActions, since upstream's
// useOn has no player target. Like Interact it does not walk.
func UseItemOn(e *actions.Equipment, objID int32, target Entity) error {
	switch target.Kind {
	case KindNpc:
		return e.UseOnNpc(objID, target.Index)
	case KindLoc:
		return e.UseOnLoc(objID, target.X, target.Z, target.ID)
	case KindGroundItem:
		return e.UseOnGroundItem(objID, gamestate.GroundItem{ID: target.ID, X: target.X, Z: target.Z})
	}
	return ErrNoActions
}
