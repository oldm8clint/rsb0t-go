package fleetplan

import (
	"sort"

	"rsb0t-go/internal/scripts/fighterfood"
)

// EmergencyEatHP is fighterfood.MinEatHP: at or below it, eat whatever heals most, overheal or not.
const EmergencyEatHP = fighterfood.MinEatHP

// CarriedFood is one food stack the account holds, by base food name (a "2/3 cake" counts as
// Cake — fighterfood.BaseFood's own mapping).
type CarriedFood struct {
	Food  string
	Count int
}

// FoodChoice is ChooseFood's answer.
type FoodChoice struct {
	Food string
	Heal int
	// Emergency is true when the choice was made under the emergency override.
	Emergency bool
}

// ChooseFood picks the bite to take now, or none.
//
//   - Normally: only foods that heal IN FULL (heal <= missing HP) are eligible, and of those the
//     BIGGEST heal is taken — the largest gap it can close without waste. So a shark is eaten only
//     when 20 or more is missing, a lobster when 12-19 is missing and there is no bass or
//     swordfish, and so on; with 6 missing and only sharks and lobsters carried, nothing is eaten.
//   - Emergency (HP at or below emergencyHP): the biggest heal carried, whatever it overheals —
//     survival wins over efficiency. emergencyHP <= 0 means EmergencyEatHP.
//
// Ties (two foods with the same heal) go to the one with more carried, so the pack thins evenly.
func ChooseFood(carried []CarriedFood, hp, maxHP, emergencyHP int) (FoodChoice, bool) {
	if hp <= 0 || maxHP <= 0 {
		return FoodChoice{}, false
	}
	if emergencyHP <= 0 {
		emergencyHP = EmergencyEatHP
	}
	type option struct {
		food  string
		heal  int
		count int
	}
	var opts []option
	for _, c := range carried {
		if c.Count <= 0 || c.Food == "" {
			continue
		}
		opts = append(opts, option{c.Food, fighterfood.FoodHealAmount(c.Food), c.Count})
	}
	if len(opts) == 0 {
		return FoodChoice{}, false
	}
	sort.SliceStable(opts, func(a, b int) bool {
		if opts[a].heal != opts[b].heal {
			return opts[a].heal > opts[b].heal
		}
		return opts[a].count > opts[b].count
	})
	if hp <= emergencyHP {
		return FoodChoice{Food: opts[0].food, Heal: opts[0].heal, Emergency: true}, true
	}
	missing := maxHP - hp
	for _, o := range opts {
		if o.heal <= missing {
			return FoodChoice{Food: o.food, Heal: o.heal}, true
		}
	}
	return FoodChoice{}, false
}

// CarriedFoodsOf is CarriedFoods for a pack read as stacks: one base-food lookup a stack, not one
// a unit. Expanding the pack to one name per unit first made a 39,656-coin stack 39,656 lookups on
// every Stage 3 snapshot (pprof 14 Oct 2026: 5% of the fleet's CPU in TotalFoodHeld).
func CarriedFoodsOf(items []KitItem) []CarriedFood {
	counts := map[string]int{}
	var order []string
	for _, it := range items {
		if it.Count <= 0 {
			continue
		}
		base, ok := fighterfood.BaseFood(it.Name)
		if !ok {
			continue
		}
		if counts[base] == 0 {
			order = append(order, base)
		}
		counts[base] += it.Count
	}
	out := make([]CarriedFood, 0, len(order))
	for _, f := range order {
		out = append(out, CarriedFood{Food: f, Count: counts[f]})
	}
	return out
}
