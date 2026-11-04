package essencerunner

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/tile"
)

// takeObjOption is a ground item's Take op (OPOBJ3).
const takeObjOption = 3

// PickaxeHeadResult is one recovery attempt's outcome.
type PickaxeHeadResult struct {
	// Tried is false when there was nothing to recover: no handle in the pack or worn.
	Tried bool
	// Fixed is true only once a pickaxe is confirmed back in the pack.
	Fixed bool
	// Reason is why not.
	Reason string
}

// pickaxesHeld counts the pickaxes in the pack and worn.
func pickaxesHeld(d *Deps) int {
	n := 0
	for _, name := range HeldNames(d) {
		if _, ok := essenceplan.PickPeriod(name); ok {
			n++
		}
	}
	return n
}

// PickaxeHeadOnGround finds a "Pickaxe head" within essenceplan.PickaxeHeadSearchRadius, nearest first.
func PickaxeHeadOnGround(d *Deps) (int, tile.Tile, bool) {
	here := currentTile(d)
	bestDist := essenceplan.PickaxeHeadSearchRadius + 1
	var bestAt tile.Tile
	bestID, found := 0, false
	for _, g := range d.Session.State().GroundItems() {
		obj, ok := d.Config.Obj(g.ID)
		if !ok || !essenceplan.IsPickaxeHead(obj.Name) {
			continue
		}
		at := tile.Tile{X: g.X, Z: g.Z, Level: g.Level}
		if at.Level != here.Level {
			continue
		}
		if dist := tile.Chebyshev(here, at); dist <= essenceplan.PickaxeHeadSearchRadius && dist < bestDist {
			bestDist, bestAt, bestID, found = dist, at, g.ID, true
		}
	}
	return bestID, bestAt, found
}

// packItemID is the object id of the first un-noted pack item the match accepts.
func packItemID(d *Deps, match func(string) bool) (int32, bool) {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0, false
	}
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && !obj.Noted() && match(obj.Name) {
			return it.ID, true
		}
	}
	return 0, false
}

// wornItemID is the object id of a worn item the match accepts.
func wornItemID(d *Deps, match func(string) bool) (int32, bool) {
	for _, it := range actions.NewEquipment(d.Session, d.Components).Worn() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && match(obj.Name) {
			return it.ID, true
		}
	}
	return 0, false
}

// pickaxeHandleWorn reports whether the handle is in the right hand (the pickaxe was wielded when the event took it).
func pickaxeHandleWorn(d *Deps) bool {
	_, ok := wornItemID(d, essenceplan.IsPickaxeHandle)
	return ok
}

// waitUntil polls cond every 100 ms until it holds, the timeout passes or the delay asks to stop.
func waitUntil(d *Deps, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(100 * time.Millisecond) {
			return false
		}
	}
	return false
}

func isEssence(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), essenceplan.Essence)
}

// RecoverPickaxeHead picks up the thrown head and puts it back on the handle, the mining twin of
// woodcutrunner.RecoverAxeHead (see essenceplan/pickaxehead.go). It is brisk on purpose: the head is gone two minutes after
// it lands. A full pack drops one essence for the room.
func RecoverPickaxeHead(d *Deps) PickaxeHeadResult {
	_, handleInPack := packItemID(d, essenceplan.IsPickaxeHandle)
	wornHandle, handleWorn := wornItemID(d, essenceplan.IsPickaxeHandle)
	if !handleInPack && !handleWorn {
		return PickaxeHeadResult{}
	}
	handleWorn = handleWorn && !handleInPack
	res := PickaxeHeadResult{Tried: true}
	_, headHeld := packItemID(d, essenceplan.IsPickaxeHead)
	var headID int
	var headAt tile.Tile
	if !headHeld {
		id, at, ok := PickaxeHeadOnGround(d)
		if !ok {
			res.Reason = "no pickaxe head in sight"
			return res
		}
		headID, headAt = id, at
	}
	if main, _, _ := d.Session.State().Modals(); main != gamestate.NoModal {
		_ = actions.CloseModal(d.Session)
		d.Delay(600 * time.Millisecond)
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	for need := essenceplan.PickaxeHeadSlotsNeeded(headHeld, handleWorn); FreeSlots(d) < need; {
		id, ok := packItemID(d, isEssence)
		if !ok {
			res.Reason = "the pack is full and holds no essence to drop, so the head cannot be picked up"
			return res
		}
		free := FreeSlots(d)
		if err := eq.InteractHeld(d.Config, id, "Drop"); err != nil || !waitUntil(d, 3*time.Second, func() bool { return FreeSlots(d) > free }) {
			res.Reason = "could not drop an essence to make room for the head"
			return res
		}
		d.logf("pickaxe head: dropped an essence to make room")
	}
	if !headHeld {
		d.logf("pickaxe head: it landed at %d,%d - going to get it", headAt.X, headAt.Z)
		if tile.Chebyshev(currentTile(d), headAt) > 0 && !WalkNear(d, headAt, 0) {
			res.Reason = "could not reach the pickaxe head"
			return res
		}
		if err := actions.OpObj(d.Session, takeObjOption, headAt.X, headAt.Z, headID); err != nil {
			d.logf("pickaxe head: taking it: %v", err)
			res.Reason = "could not pick the head up"
			return res
		}
		if !waitUntil(d, 5*time.Second, func() bool { _, ok := packItemID(d, essenceplan.IsPickaxeHead); return ok }) {
			res.Reason = "the head never reached the pack"
			return res
		}
	}
	if handleWorn {
		if err := eq.Unequip(wornHandle); err != nil || !waitUntil(d, 5*time.Second, func() bool { _, ok := packItemID(d, essenceplan.IsPickaxeHandle); return ok }) {
			res.Reason = "could not take the worn handle off"
			return res
		}
	}
	handle, okHandle := packItemID(d, essenceplan.IsPickaxeHandle)
	head, okHead := packItemID(d, essenceplan.IsPickaxeHead)
	if !okHandle || !okHead {
		res.Reason = "the handle and the head are not both in the pack"
		return res
	}
	before := pickaxesHeld(d)
	if err := eq.UseOnHeld(handle, head); err != nil {
		d.logf("pickaxe head: attaching it: %v", err)
		res.Reason = "the engine refused the re-attach"
		return res
	}
	// The receipt is a pickaxe appearing: content deletes both pieces and adds oc_param(head, fixed).
	if !waitUntil(d, 5*time.Second, func() bool { return pickaxesHeld(d) > before }) {
		res.Reason = "the pieces did not become a pickaxe"
		return res
	}
	d.logf("pickaxe head: the pickaxe is put back together")
	res.Fixed = true
	return res
}
