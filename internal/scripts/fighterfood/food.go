// Package fighterfood is the slice of rsb0t-scripts' fighterData.ts/fighterPlan.ts that
// BarbarianAgility.ts actually imports: what heals how much, when to eat, and the death chat
// line. Not a full auto-fighter port — combat-specific decisions (prayer, potions, panic/flee,
// ammo) stay in fighterData.ts/fighterPlan.ts, out of scope until a combat script needs them.
//
// It is pure: no client, actions, fleet or bot imports and no I/O.
package fighterfood

import (
	"regexp"
	"strings"
	"sync"
)

// FoodOptions are the foods a fighter script can be pointed at.
var FoodOptions = []string{
	"Manta ray", "Sea turtle", "Shark", "Lobster", "Swordfish", "Tuna", "Salmon", "Trout", "Pike", "Bass", "Cod", "Mackerel", "Herring", "Sardine", "Anchovies", "Shrimps",
	"Cooked meat", "Cooked chicken", "Bread", "Stew",
	"Cake", "Chocolate cake", "Plain pizza", "Meat pizza", "Anchovy pizza", "Pineapple pizza", "Redberry pie", "Meat pie", "Apple pie",
	// Jug of wine: iop1=Drink, the same OPHELD1 slot as Eat, and consume_effects.dbrow's
	// [wine_effect] heals 11 (Attack -2 for a while). Heroes and giants drop it (owner, 29 Sep).
	"Jug of wine",
	// Kebab: iop1=Eat (cooking.obj); consume_effect_kebab heals 3 + 7% of max HP most of the
	// time, more 9 times in 32, and 2 times in 32 deals 3-4 damage but never below 3 HP. the mule
	// held 10,165 on 3 Oct (owner: "the mule holds kebabs they can request as a food source").
	"Kebab",
	// Spinach roll: iop1=Eat (cooking.obj), consume_normal.dbrow's [heal_2]. The Mysterious Old
	// Man's random event hands them out and the mule holds them (owner, 4 Oct).
	"Spinach roll",
}

// foodForms are multi-bite foods and the partial forms they leave in the pack.
var foodForms = map[string][]string{
	"cake":            {"cake", "2/3 cake", "slice of cake"},
	"chocolate cake":  {"chocolate cake", "2/3 chocolate cake", "chocolate slice"},
	"plain pizza":     {"plain pizza", "1/2 plain pizza"},
	"meat pizza":      {"meat pizza", "1/2 meat pizza"},
	"anchovy pizza":   {"anchovy pizza", "1/2 anchovy pizza"},
	"pineapple pizza": {"pineapple pizza", "1/2 pineapple pizza"},
	"redberry pie":    {"redberry pie", "half a redberry pie"},
	"meat pie":        {"meat pie", "half a meat pie"},
	"apple pie":       {"apple pie", "half an apple pie"},
	"jug of wine":     {"jug of wine", "half full wine jug"},
}

// foodHeal is HP restored by one bite. Multi-bite foods list the per-bite heal.
//
// Why: the values are the content's own, from content/scripts/player/configs/consumption/
// consume_normal.dbrow (the heal_N rows, stat_heal,hitpoints,N) on branch 289. The earlier table
// came from fighterData.ts and disagreed for anchovies (3, not 1), bread (4, not 5), the pies,
// the pizzas and stew.
var foodHeal = map[string]int{
	"shrimps": 3, "anchovies": 3, "sardine": 4, "herring": 5, "mackerel": 6, "trout": 7, "cod": 7, "pike": 8,
	"salmon": 9, "tuna": 10, "lobster": 12, "bass": 13, "swordfish": 14, "shark": 20,
	"sea turtle": 21, "manta ray": 22, // consume_normal.dbrow heal_21, heal_22
	"cooked meat": 3, "cooked chicken": 3, "bread": 4, "stew": 9,
	"cake": 4, "chocolate cake": 5, "plain pizza": 5, "meat pizza": 7, "anchovy pizza": 8, "pineapple pizza": 10,
	"redberry pie": 3, "meat pie": 4, "apple pie": 5,
	"jug of wine":  11,
	"kebab":        3, // the common case's floor; see FoodOptions
	"spinach roll": 2,
}

// DefaultFoodHeal is used when the food name is not in the table, so smart eating still has a
// room size.
const DefaultFoodHeal = 8

// MinEatHP is always eat at or below this HP while food remains.
const MinEatHP = 5

func key(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// FoodForms is every pack name that counts as foodName, partial forms included.
func FoodForms(foodName string) []string {
	k := key(foodName)
	if forms, ok := foodForms[k]; ok {
		return forms
	}
	return []string{k}
}

// IsFoodItem reports whether name is any form of foodName.
func IsFoodItem(name, foodName string) bool {
	target := key(name)
	for _, f := range FoodForms(foodName) {
		if f == target {
			return true
		}
	}
	return false
}

// BaseFood is the FoodOptions entry name is a form of (a partial cake counts as its cake), and
// whether it is one at all. Matching is exact on the form, never by substring, so "Raw shrimps"
// is not food.
func BaseFood(name string) (string, bool) {
	target := key(name)
	if target == "" {
		return "", false
	}
	food, ok := baseByForm()[target]
	return food, ok
}

// baseByForm maps every lower-case pack form to its FoodOptions entry, the first entry winning,
// exactly as BaseFood's old scan over FoodOptions did. Built once: live on 6965fa66 (27 Sep
// 12:46) that scan (FoodForms and ToLower for every option, for every pack item, on every Stage 3
// snapshot of ~130 accounts) was 77% of the fleet's CPU and held the laptop at 100%.
var baseByForm = sync.OnceValue(func() map[string]string {
	m := map[string]string{}
	for _, food := range FoodOptions {
		for _, form := range FoodForms(food) {
			if _, seen := m[form]; !seen {
				m[form] = food
			}
		}
	}
	return m
})

// AllFoodForms is every pack name of every FoodOptions food, partial forms included, lower-case.
func AllFoodForms() []string {
	var out []string
	for _, food := range FoodOptions {
		out = append(out, FoodForms(food)...)
	}
	return out
}

// FoodHealAmount is the HP one bite of foodName restores.
func FoodHealAmount(foodName string) int {
	k := key(foodName)
	if k == "" {
		return DefaultFoodHeal
	}
	if heal, ok := foodHeal[k]; ok {
		return heal
	}
	for parent, forms := range foodForms {
		for _, f := range forms {
			if f == k {
				if heal, ok := foodHeal[parent]; ok {
					return heal
				}
				return DefaultFoodHeal
			}
		}
	}
	for name, heal := range foodHeal {
		if strings.Contains(k, name) {
			return heal
		}
	}
	return DefaultFoodHeal
}

// EatMode is how ShouldEat decides.
type EatMode string

const (
	EatSmart     EatMode = "smart"
	EatThreshold EatMode = "threshold"
)

// EatModeOptions are the eat-mode setting's choices.
var EatModeOptions = []string{"Smart", "Threshold"}

// ParseEatMode reads a setting label; anything but "threshold" (case-insensitive) is smart.
func ParseEatMode(label string) EatMode {
	if key(label) == "threshold" {
		return EatThreshold
	}
	return EatSmart
}

// EatInput is what ShouldEat reads.
type EatInput struct {
	HP, MaxHP, Heal, FoodCount int
	Mode                       EatMode
	// ThresholdFrac is 0-1, threshold mode only.
	ThresholdFrac float64
}

// ShouldEat reports whether to eat now: when a full heal fits (smart), or below the threshold;
// always at the safety floor.
func ShouldEat(i EatInput) bool {
	if i.FoodCount <= 0 || i.HP <= 0 || i.MaxHP <= 0 {
		return false
	}
	if i.HP <= MinEatHP {
		return true
	}
	if i.Mode == EatThreshold {
		return float64(i.HP)/float64(i.MaxHP) < i.ThresholdFrac
	}
	heal := i.Heal
	if heal < 1 {
		heal = 1
	}
	return i.MaxHP-i.HP >= heal
}

var deathLine = regexp.MustCompile(`(?i)oh dear.*you are dead`)

// IsDeathLine reports whether text is the game's death message.
func IsDeathLine(text string) bool {
	return deathLine.MatchString(text)
}
