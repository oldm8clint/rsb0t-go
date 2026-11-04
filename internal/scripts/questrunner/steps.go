package questrunner

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// Executors for every step kind beyond talk. Each returns false when the step did not do its
// job; the engine's watchdog is what decides whether that matters, by whether the snapshot
// moved.

// locSearchRadius is how far from a step's anchor a named loc may be.
const locSearchRadius = 12

// effectTimeout is how long an op on a loc or an item gets to show its effect.
const effectTimeout = 8 * time.Second

func toRects(rs []questplan.Rect) []webwalk.Rect {
	out := make([]webwalk.Rect, 0, len(rs))
	for _, r := range rs {
		out = append(out, webwalk.Rect{MinX: r.MinX, MaxX: r.MaxX, MinZ: r.MinZ, MaxZ: r.MaxZ, Level: r.Level})
	}
	return out
}

// travelToAnchor brings the account within a scene of a step's anchor, unless the anchor is
// unset (the step works wherever the account stands).
func travelToAnchor(d *Deps, anchor tile.Tile, avoid []questplan.Rect) bool {
	if anchor == (tile.Tile{}) {
		return true
	}
	here, ok := currentTileOK(d)
	if ok && here.Level == anchor.Level && tile.Chebyshev(here, anchor) <= 6 {
		return true
	}
	return TravelAvoiding(d, anchor, 3, toRects(avoid))
}

func execInteractLoc(d *Deps, s questplan.Step) bool {
	if !travelToAnchor(d, s.Anchor, s.Avoid) {
		return false
	}
	near := s.Anchor
	if near == (tile.Tile{}) {
		near = currentTile(d)
	}
	m, ok := FindLoc(d, s.LocID, s.Loc, s.Op, near, locSearchRadius)
	if !ok {
		d.logf("no %s (loc %d) offering %q near %d,%d,%d", s.Loc, s.LocID, s.Op, near.X, near.Z, near.Level)
		return false
	}
	expect := s.ExpectItemID
	if expect == 0 && s.ExpectItem != "" {
		expect, _ = ResolveItem(d, 0, s.ExpectItem)
	}
	before, from := HeldID(d, expect), currentTile(d)
	if err := OperateLoc(d, m, s.Op); err != nil {
		d.logf("%s %s: %v", s.Op, s.Loc, err)
		return false
	}
	if expect > 0 {
		return waitQuietly(d, func() bool { return HeldID(d, expect) > before }, effectTimeout)
	}
	// No item to wait for: give the op a moment to move the account or open something.
	waitQuietly(d, func() bool {
		main, _, chat := d.Session.State().Modals()
		return currentTile(d) != from || main != gamestate.NoModal || chat != gamestate.NoModal
	}, 3*time.Second)
	return true
}

func execUseOn(d *Deps, s questplan.Step) bool {
	item, ok := ResolveItem(d, s.ItemID, s.Item)
	if !ok || HeldID(d, item) == 0 {
		d.logf("use %s: not held", s.Item)
		return false
	}
	if !travelToAnchor(d, s.Anchor, s.Avoid) {
		return false
	}
	from, heldBefore := currentTile(d), HeldID(d, item)
	switch s.TargetKind {
	case questplan.TargetLoc:
		near := s.Anchor
		if near == (tile.Tile{}) {
			near = currentTile(d)
		}
		m, found := FindLoc(d, s.TargetID, s.Target, "", near, locSearchRadius)
		if !found {
			d.logf("no %s (loc %d) near %d,%d,%d to use %s on", s.Target, s.TargetID, near.X, near.Z, near.Level, s.Item)
			return false
		}
		if err := UseItemOnLoc(d, item, m); err != nil {
			d.logf("use %s on %s: %v", s.Item, s.Target, err)
			return false
		}
	case questplan.TargetNpc:
		npc, found := FindNpcByID(d, s.TargetID, s.Target, locSearchRadius+4)
		if !found {
			d.logf("no %s near to use %s on", s.Target, s.Item)
			return false
		}
		if err := UseItemOnNpc(d, item, npc); err != nil {
			d.logf("use %s on %s: %v", s.Item, s.Target, err)
			return false
		}
	case questplan.TargetItem:
		target, found := ResolveItem(d, s.TargetID, s.Target)
		if !found {
			return false
		}
		if !PackVisible(d) {
			return false
		}
		if err := actions.NewEquipment(d.Session, d.Components).UseOnHeld(int32(item), int32(target)); err != nil {
			d.logf("use %s on %s: %v", s.Item, s.Target, err)
			return false
		}
	}
	// Most use-ons answer with a teleport (a door, a tombstone), a consumed item or a dialogue a
	// tick or several later; wait for any of them rather than for a fixed pause, so the next
	// decide() sees the result and does not send the same use again mid-script.
	waitQuietly(d, func() bool {
		main, _, chat := d.Session.State().Modals()
		return currentTile(d) != from || HeldID(d, item) != heldBefore ||
			main != gamestate.NoModal || chat != gamestate.NoModal
	}, effectTimeout)
	return true
}

func execOpHeld(d *Deps, s questplan.Step) bool {
	item, ok := ResolveItem(d, s.ItemID, s.Item)
	if !ok || HeldID(d, item) == 0 {
		d.logf("%s %s: not held", s.Op, s.Item)
		return false
	}
	if err := OpHeldByName(d, item, s.Op); err != nil {
		d.logf("%s %s: %v", s.Op, s.Item, err)
		return false
	}
	d.Delay(1200 * time.Millisecond)
	if s.Op == "Read" {
		// A book or scroll opens a main modal that blocks every later interaction.
		ClearLeftoverModal(d)
	}
	return true
}

func execEquip(d *Deps, s questplan.Step) bool {
	item, ok := ResolveItem(d, s.ItemID, s.Item)
	if !ok {
		return false
	}
	return Equip(d, item)
}

func execGrabGround(d *Deps, s questplan.Step) bool {
	item, ok := ResolveItem(d, s.ItemID, s.Item)
	if !ok {
		return false
	}
	if !travelToAnchor(d, s.Anchor, s.Avoid) {
		return false
	}
	near := s.Anchor
	if near == (tile.Tile{}) {
		near = currentTile(d)
	}
	if TakeGround(d, item, near, 6) {
		return true
	}
	if s.Radius > 0 {
		// Radius on a grab is how long to wait for a respawn, in seconds.
		if waitQuietly(d, func() bool { return len(GroundItemsNear(d, item, near, 6)) > 0 }, time.Duration(s.Radius)*time.Second) {
			return TakeGround(d, item, near, 6)
		}
	}
	return false
}

func execPickLoc(d *Deps, s questplan.Step) bool {
	return execInteractLoc(d, s)
}

func execMineRock(d *Deps, s questplan.Step) bool {
	item, ok := ResolveItem(d, s.ItemID, s.Item)
	if !ok {
		return false
	}
	if !travelToAnchor(d, s.Anchor, s.Avoid) {
		return false
	}
	start := HeldID(d, item)
	deadline := time.Now().Add(3 * time.Minute)
	for HeldID(d, item)-start < s.Qty && time.Now().Before(deadline) && FreeSlots(d) > 0 {
		near := s.Anchor
		if near == (tile.Tile{}) {
			near = currentTile(d)
		}
		m, found := FindLoc(d, s.LocID, s.Loc, "Mine", near, locSearchRadius)
		if !found {
			d.logf("no %s to mine near %d,%d", s.Loc, near.X, near.Z)
			break
		}
		before := HeldID(d, item)
		if err := OperateLoc(d, m, "Mine"); err != nil {
			d.logf("mine %s: %v", s.Loc, err)
			break
		}
		waitQuietly(d, func() bool { return HeldID(d, item) > before }, 20*time.Second)
	}
	return HeldID(d, item) > start
}

func execBuy(d *Deps, s questplan.Step) bool {
	item, ok := ResolveItem(d, s.ItemID, s.Item)
	if !ok {
		return false
	}
	return BuyFromShop(d, s.Shop, item, s.Qty) > 0
}

// FightNpc is the fight step's executor for a skill method or a custom step that has no engine
// in hand.
func FightNpc(d *Deps, s questplan.Step, sus questplan.Sustain) bool { return execFight(d, s, sus) }

// fightTimeout bounds one fight step.
const fightTimeout = 3 * time.Minute

// execFight attacks the named NPC and waits for it to die (it leaves the scene) or for the
// account to die or run out of food, eating by the sustain meanwhile. The loop is meleeFight
// (fightloop.go): it sends the attack again when eating or a dropped click has stopped it (eating
// clears the attack, and Priest in Peril's monk was never killed by an account that had to eat),
// and it gives a fight up and walks away when the food is nearly gone or the target is not losing
// health, counting that toward the quest's park logic. It prefers a target that is not already
// fighting another account (a single-combat area refuses an attack on it).
func execFight(d *Deps, s questplan.Step, sus questplan.Sustain) bool {
	if s.Spell != "" {
		return FightByMagic(d, s, sus)
	}
	if !travelToAnchor(d, s.Anchor, s.Avoid) {
		return false
	}
	npc, ok := pickFightTarget(d, s.TargetID, s.Target, 16)
	if !ok {
		d.logf("no %s to fight", s.Target)
		return false
	}
	// The eat line comes from the foe's hardest hit when the tables know it, and the account is topped up
	// to the level a fight starts at before the first blow (fightguard.go).
	sus, foe, _ := VsFoe(d, sus, npc.ID)
	PrepareForFight(d, sus.FoodIDs, foe.PeakHit())
	at := tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}
	if tile.Chebyshev(currentTile(d), at) > 1 {
		WalkNear(d, at, 1)
	}
	defer ensureRetaliate(d)()
	f := newLiveFight(d, npc, sus)
	end := meleeFightVs(f, sus, foe.PeakHit(), fightTimeout, func() {
		d.logf("fight: attacking %s again (eating or a dropped click had stopped the attack)", fightLabel(npc))
	})
	return finishFight(d, f, end)
}
