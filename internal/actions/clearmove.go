package actions

import "errors"

// backpackSlots is how many slots the backpack has.
const backpackSlots = 28

// ErrNoEmptyNaming is returned by ClearMoveRequest when every backpack slot holds the item it was asked to name.
var ErrNoEmptyNaming = errors.New("actions: every backpack slot holds the item, so none can be named wrongly")

// ClearMoveRequest sends the packet that stops a delay (a spell's p_delay, a rub's) from freezing a walk at a zone trigger,
// without doing anything else: an OPHELDU that names obj at a backpack slot that does not hold it.
//
// Why it works. A movement click sets the player's moveClickRequest, and Player.updateMovement refuses to move a player
// who has one, is busy (delayed, or with a modal up) and has a script waiting in the queue or the engine queue. A zone or
// map-square trigger the walk crosses queues exactly such a script (internal/zonetrigger), so a High Alchemy cast on the
// move halts the walk at every trigger until its p_delay ends: 90 tiles in 62 ticks against 124 without the cast (lab,
// 6 October 2026, claude/alchcadence-1006). Anything that clears moveClickRequest first lets the player walk on while busy.
// OpHeldHandler does for every op it accepts (a Wield, a Rub) and OpHeldUHandler does when the item it is told about is not
// at the slot it is told: `player.moveClickRequest = false; // removed early osrs`, then clearPendingAction (which a cast does
// itself) and false. No message, no delay, no change to the backpack and nothing for the content to run, which no Wield or
// Rub can say. A client whose backpack view is a tick behind sends such a packet in the ordinary way.
//
// Sent in the same write as the cast and read in the same tick before it (the Coalescer), or once after each movement click,
// it measured identical on the lab engine: 124 tiles in 62 ticks, 0.0887% run energy a tile, 77% of the ticks frozen.
//
// holds says whether the backpack holds obj at a slot; the packet names the first slot, counting back from the last, that
// does not.
func ClearMoveRequest(s Session, obj int32, com int, holds func(slot int) bool) error {
	for slot := backpackSlots - 1; slot >= 0; slot-- {
		if !holds(slot) {
			return OpHeldU(s, obj, slot, com, obj, slot, com)
		}
	}
	return ErrNoEmptyNaming
}
