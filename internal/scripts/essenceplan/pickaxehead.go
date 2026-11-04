package essenceplan

import "strings"

// THE LOST PICKAXE (macro events/scripts/mining/macro_event_lost_pickaxe.rs2). The random event swaps the pickaxe for a
// "Pickaxe handle" (in the right hand when it was wielded) and throws its "Pickaxe head" onto a square up to 7 tiles away
// (map_findsquare(coord, 1, 7, ^map_findsquare_lineofwalk)), where it lies for ^lootdrop_duration (200 ticks, two minutes).
// Using the head on the handle (either way round, with both in the pack) gives the pickaxe back. Every tier's head has the
// same name. Nothing recovered it before 19 Oct 2026: five of the seven miners lost every pickaxe they had, kept clicking the
// rock through "You need a Pickaxe to mine this rock" and banked nothing for hours.
const (
	PickaxeHandle = "Pickaxe handle"
	PickaxeHead   = "Pickaxe head"
	// PickaxeHeadSearchRadius is how far from the account the head is looked for: content's 7, and one more for a step taken
	// since it landed.
	PickaxeHeadSearchRadius = 8
)

// IsPickaxeHandle and IsPickaxeHead match the event's two pieces by name.
func IsPickaxeHandle(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), PickaxeHandle)
}
func IsPickaxeHead(name string) bool { return strings.EqualFold(strings.TrimSpace(name), PickaxeHead) }

// PickaxeHeadSlotsNeeded is the free pack slots putting the pickaxe back together takes: one for the head unless it is held,
// and one more for a worn handle, which content's reattach reads from the inventory and so must be taken off first.
func PickaxeHeadSlotsNeeded(headHeld, handleWorn bool) int {
	n := 0
	if !headHeld {
		n++
	}
	if handleWorn {
		n++
	}
	return n
}
