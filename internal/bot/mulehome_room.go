package bot

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fisherrunner"
)

// A displaced mule may still carry its cooking batch. With no coin stack and a
// full pack, pickpocketing cannot earn the boat fare. Clear only burnt fish or
// consume edible food; never throw away stock, equipment or unidentified items.
func muleFareClearItem(items []gamestate.SlotItem, types actions.ObjTypes) (gamestate.SlotItem, int) {
	if types == nil {
		return gamestate.SlotItem{}, 0
	}
	var food gamestate.SlotItem
	foodOp := 0
	for _, it := range items {
		o, ok := types.Obj(int(it.ID))
		if !ok || o.Noted() || it.Count != 1 {
			continue
		}
		burnt := strings.EqualFold(o.Name, "Burnt shark") || strings.EqualFold(o.Name, "Burnt fish") || strings.EqualFold(o.Name, "Burnt lobster")
		if burnt && (o.IOps[4] == "" || strings.EqualFold(o.IOps[4], "Drop")) {
			return it, 5 // the client's default Drop option
		}
		for i, op := range o.IOps {
			if strings.EqualFold(op, "Eat") && foodOp == 0 {
				food, foodOp = it, i+1
			}
		}
	}
	return food, foodOp
}

func muleMakeFareRoom(ctx *Context, types actions.ObjTypes) bool {
	if cfg, ok := types.(*cachecfg.Config); ok && cfg == nil {
		types = nil
	}
	if ctx.Stopped() {
		return false
	}
	inv := ctx.Session.State().Inventory(fisherrunner.PlayerPack)
	if inv == nil {
		return false
	} // no observed pack is not an empty pack
	items := inv.NonEmpty()
	if !actions.IsFull(items) {
		return true
	}
	// Coins already in the pack can stack without another slot.
	if types != nil && actions.CountByName(items, types, "Coins") > 0 {
		return true
	}
	it, op := muleFareClearItem(items, types)
	if op == 0 {
		ctx.logf("mule home: full pack with no coins and no safe item to clear; banking or operator assistance needed before earning fare")
		return false
	}
	ctx.logf("mule home: clearing one food slot for the boat fare (item %d, slot %d)", it.ID, it.Slot)
	if err := actions.OpHeld(ctx.Session, op, it.ID, it.Slot, fisherrunner.PlayerPack); err != nil {
		return false
	}
	// A bite can transform a cake without freeing a slot. Observe actual room;
	// never start pickpocketing merely because the click was sent.
	deadline := time.Now().Add(3 * time.Second)
	for !ctx.Stopped() && time.Now().Before(deadline) {
		fresh := ctx.Session.State().Inventory(fisherrunner.PlayerPack)
		if fresh != nil && !actions.IsFull(fresh.NonEmpty()) {
			return true
		}
		if !ctx.Delay(100 * time.Millisecond) {
			return false
		}
	}
	return false
}
