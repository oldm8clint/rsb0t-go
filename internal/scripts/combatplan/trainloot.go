package combatplan

import "strings"

// What a training kill's drop is worth picking up (a user request): "when the fleet kills npcs
// they should loot all stackable loot provided they have space for it, prioritise keeping food over
// looting but make sure once we eat we loot anything valuable on the floor from npc drops, we could
// be getting feathers from chickens runes from giants".
//
// So: every stackable drop the pack has room for (a slot it already stacks into, or a free one),
// and a non-stackable one only into a free slot and only when it is worth TrainLootMinCost. Food is
// never dropped to make room; the room a bite frees is used by the next kill's pass. Bones are the
// bone pass's (LootAndBuryBones) and are left to it.

// TrainLootMinCost is the least oc_cost a non-stackable drop must have to take a pack slot.
const TrainLootMinCost = 100

// TrainLootDrop is what the decision reads about one ground item.
type TrainLootDrop struct {
	Name      string
	Stackable bool
	Cost      int
	// Stacks is true when the pack already holds this item as a stack (no new slot needed).
	Stacks bool
	// ID is the obj id; 0 when unknown. Grimy herbs are told apart only by it (herbloot.go).
	ID int
}

// WorthTrainLoot reports whether a drop should be picked up with freeSlots empty pack slots. A
// grimy herb (by ID) is taken by its rank, best herbs into the last free slots (HerbMinFree).
func WorthTrainLoot(drop TrainLootDrop, freeSlots int) bool {
	if worth, herb := WorthHerb(drop.ID, freeSlots); herb {
		return worth
	}
	n := strings.ToLower(strings.TrimSpace(drop.Name))
	if n == "" || strings.Contains(n, "bones") {
		return false
	}
	if drop.Stackable {
		return drop.Stacks || freeSlots > 0
	}
	if n == SpinachRoll {
		return freeSlots > 0 // eaten as soon as it fits a missing 2 HP, so it never holds a loot slot long
	}
	if n == LimpwurtRoot {
		return freeSlots >= LimpwurtMinFree // a potion herb the giants drop (content giant.rs2), worth 7 coins: only into spare slots
	}
	return freeSlots > 0 && drop.Cost >= TrainLootMinCost
}

// SpinachRoll (lower case) heals 2 and is worth 1 coin, below TrainLootMinCost, but a kill that drops
// one extends the trip for free (owner, 4 Oct: "use it to extend the trip a little longer and it
// wont take up a loot spot").
const SpinachRoll = "spinach roll"

// LimpwurtRoot (lower case) is the root the giants and the druids drop (content drop tables giant.rs2, druid.rs2,
// hobgoblin.rs2): an ingredient of the super attack, strength and defence potions, and of the Ikov quest's twenty
// roots for Winelda. Worth 7 coins, so TrainLootMinCost never took it ("pick up Limpwurt roots dropped by giants",
// a user request). Taken, one slot each, while LimpwurtMinFree slots are free, so food and the bones keep the
// last ones.
const (
	LimpwurtRoot    = "limpwurt root"
	LimpwurtMinFree = 3
)
