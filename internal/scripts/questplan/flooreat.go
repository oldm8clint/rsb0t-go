package questplan

// The floor under every quest's sustain: what an account eats on a quest leg when its module names no food, or names
// other food than the pack carries (pure; the I/O is questrunner.EatFloor).
//
// Live 8 Oct 22:00 to 12 Oct 04:30 2026, after the cautious kit legs shipped: 21 quest deaths on "provision" legs (Death
// Plateau 12, Witch's House 6, Priest in Peril 2, Tree Gnome 1), 18 of them with a DEATH line and every one of those "killed
// by poison", at 1 to 3 of 34 to 39 hitpoints on the Karamja coast, 15 of the 18 with 5 to 20 food still in the pack. The run
// staller stood aside "so a bite can land" and none was ever sent: Engine.Run's eat loop eats only the module's own Sustain
// list, and returns at once for a module with none (Imp Catcher, Hetty, Murder Mystery, Rune Mysteries, Barcrawl), so an
// account carrying other food walked on to its death. The 12 Oct walk eater (scarabrunner.EatIfLow) rides only
// scarabrunner.WalkTo's own walks, not a quest leg's.

import (
	"sort"
	"time"

	"rsb0t-go/internal/scripts/fighterfood"
)

// FloorEatPercent is the share of the maximum hitpoints at or under which a quest walk eats whatever food it carries. It is
// scarabrunner.WalkEatPercent, the percentage the other walks of the account already use.
const FloorEatPercent = 50

// FloorEatGap is the least time between two floor bites: content's eat delay (2 ticks) with room.
const FloorEatGap = 2 * time.Second

// FloorEatDue reports whether a quest walk should take a floor bite now: hitpoints known and at or under FloorEatPercent of the
// maximum, the last bite at least FloorEatGap ago.
func FloorEatDue(hp, maxHP int, sinceBite time.Duration) bool {
	return FloorEatDueAt(hp, maxHP, FloorEatLine(maxHP), sinceBite)
}

// FloorEatLine is the hitpoints at or under which FloorEatDue is true: FloorEatPercent of the maximum, rounded down.
func FloorEatLine(maxHP int) int { return maxHP * FloorEatPercent / 100 }

// FloorEatDueAt is FloorEatDue against a line the caller has raised (after a view lag: fighterfood.LagEatLine of FloorEatLine).
func FloorEatDueAt(hp, maxHP, line int, sinceBite time.Duration) bool {
	return maxHP > 0 && hp > 0 && hp <= line && sinceBite >= FloorEatGap
}

// FloorFood is one stack of carried food: its object id, its display name and how many.
type FloorFood struct {
	ID    int
	Name  string
	Count int
}

// RankFloorFoods orders the food a floor bite may eat, the biggest heal first (at or under half the hitpoints any bite is
// worth it, scarabplan.ChooseFood's emergency rule). A stack that is not a food by name (fighterfood.BaseFood), an empty one
// and a quest tool (tool true: Merlin's bread, Death Plateau's trout) are left out; equal heals keep the pack's order.
func RankFloorFoods(carried []FloorFood, tool func(id int) bool) []FloorFood {
	var out []FloorFood
	heal := map[int]int{}
	for _, f := range carried {
		if f.Count <= 0 || (tool != nil && tool(f.ID)) {
			continue
		}
		base, ok := fighterfood.BaseFood(f.Name)
		if !ok {
			continue
		}
		heal[f.ID] = fighterfood.FoodHealAmount(base)
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool { return heal[out[i].ID] > heal[out[j].ID] })
	return out
}
