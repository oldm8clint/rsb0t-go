package retaliateplan

import (
	"fmt"
	"sort"
	"strings"
)

// ---- gear ----

// Worn slots (content wearpos constants).
const (
	// WearRHand is the weapon slot, WearQuiver the ammo slot.
	WearRHand  = 3
	WearQuiver = 13
)

// BowStyleRapid is the com_mode value of the Rapid button on a bow's combat tab (actions.CombatModes, component 1771).
const BowStyleRapid = 1

var shortbowNames = map[string]int{
	"shortbow": 1, "oak shortbow": 5, "willow shortbow": 20, "maple shortbow": 30, "yew shortbow": 40, "magic shortbow": 50,
}

// IsShortbow reports whether name is a strung shortbow, which has the default attackrate of 4 (bows.obj: no attackrate line),
// and so flinches by 1 with the Rapid style. A longbow is 6 and an unstrung bow is not a weapon.
func IsShortbow(name string) bool {
	_, ok := shortbowNames[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// BowRanged is the Ranged level a shortbow needs (0 for a name that is not one).
func BowRanged(name string) int { return shortbowNames[strings.ToLower(strings.TrimSpace(name))] }

// IsChopAxe reports whether name is an axe a woodcutter swings (a pickaxe and a battleaxe are not).
func IsChopAxe(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.HasSuffix(n, " axe") && !strings.Contains(n, "pickaxe") && !strings.Contains(n, "battleaxe")
}

// GearStep is one thing the setup does to the equipment.
type GearStep int

// The gear steps.
const (
	GearOK GearStep = iota
	// GearRemoveQuiver takes the ammo off: a worn quiver turns a slipped click into fired arrows and a dead rat.
	GearRemoveQuiver
	// GearWield wields the named bow from the pack.
	GearWield
	// GearNoBow means no shortbow is worn or held: the owner must give the account one.
	GearNoBow
	// GearNoAxe means no axe is held (a worn axe is not enough: the bow takes the weapon slot).
	GearNoAxe
)

// GearView is what the setup read of the equipment and the pack.
type GearView struct {
	// Weapon is the worn weapon's name ("" none), Ammo the worn ammo's name ("" none).
	Weapon, Ammo string
	// PackBows are the pack's items that are shortbows, best (lowest Ranged need) first is NOT assumed: PlanGear sorts.
	PackBows []string
	// AxeHeld is whether the pack holds an axe.
	AxeHeld bool
	// Ranged is the account's Ranged level.
	Ranged int
}

// PlanGear says the next equipment step. It is called again after each step until it answers GearOK.
func PlanGear(v GearView) (GearStep, string) {
	if v.Ammo != "" {
		return GearRemoveQuiver, v.Ammo
	}
	if !IsShortbow(v.Weapon) {
		bows := append([]string(nil), v.PackBows...)
		sort.SliceStable(bows, func(i, j int) bool { return BowRanged(bows[i]) < BowRanged(bows[j]) })
		for _, b := range bows {
			if BowRanged(b) <= v.Ranged {
				return GearWield, b
			}
		}
		return GearNoBow, ""
	}
	if !v.AxeHeld {
		return GearNoAxe, ""
	}
	return GearOK, ""
}

// ---- aggro ----

// Spell is a rune-costing spell the setup wakes a rat with.
type Spell struct {
	Name  string
	Level int
	// Runes are rune names and counts a cast costs.
	Runes map[string]int
	// Damages is true for a spell that can hurt (and kill) a rat: used only when the settings list it.
	Damages bool
}

// Spells are the spells the setup may use (content magic_combat_spells.dbrow). Confuse, Weaken and Curse lower a stat and deal
// no damage; Wind Strike deals up to 2, which can kill a 2-hitpoint rat.
var Spells = []Spell{
	{Name: "confuse", Level: 3, Runes: map[string]int{"body rune": 1, "water rune": 3, "earth rune": 2}},
	{Name: "weaken", Level: 11, Runes: map[string]int{"body rune": 1, "water rune": 3, "earth rune": 2}},
	{Name: "curse", Level: 19, Runes: map[string]int{"body rune": 1, "water rune": 2, "earth rune": 3}},
	{Name: "wind strike", Level: 1, Runes: map[string]int{"air rune": 1, "mind rune": 1}, Damages: true},
}

// SpellByName finds a spell in Spells.
func SpellByName(name string) (Spell, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, s := range Spells {
		if s.Name == n {
			return s, true
		}
	}
	return Spell{}, false
}

// CastsPossible is how many casts of s the held runes (lower-case name to count) pay for.
func (s Spell) CastsPossible(held map[string]int) int {
	best := -1
	for name, need := range s.Runes {
		n := held[name] / need
		if best < 0 || n < best {
			best = n
		}
	}
	if best < 0 {
		return 0
	}
	return best
}

// ChooseAggro picks the first spell in prefs that the Magic level allows and the runes pay for two casts of (one per rat).
// The reason says, for the log, why the earlier ones were skipped or why nothing fits.
func ChooseAggro(prefs []string, magic int, held map[string]int) (Spell, string, bool) {
	var skipped []string
	for _, p := range prefs {
		s, ok := SpellByName(p)
		if !ok {
			skipped = append(skipped, fmt.Sprintf("%q is not an aggro spell this script knows", p))
			continue
		}
		if magic < s.Level {
			skipped = append(skipped, fmt.Sprintf("%s needs Magic %d (have %d)", s.Name, s.Level, magic))
			continue
		}
		if s.CastsPossible(held) < 2 {
			skipped = append(skipped, fmt.Sprintf("%s needs %s for each of two casts", s.Name, RuneNeeds(s)))
			continue
		}
		return s, strings.Join(skipped, "; "), true
	}
	return Spell{}, strings.Join(skipped, "; "), false
}

// RuneNeeds is "1 body rune, 3 water rune, 2 earth rune" in a stable order.
func RuneNeeds(s Spell) string {
	names := make([]string, 0, len(s.Runes))
	for n := range s.Runes {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%d %s", s.Runes[n], n))
	}
	return strings.Join(parts, ", ")
}

// AggroGap is the ticks between the two casts: the spell sets %action_delay to clock+5 (player_magic.rs2), so the second
// cast cannot be sooner, and 6 is 2 mod 4, the offset two attackrate-4 monsters need (research doc section 6.3).
const AggroGap = 6

// ---- safety ----

// HPAbort says the account should stop: hitpoints below pct percent of the maximum. The rats hit for 0, so this is another
// monster, or poison. ok is false when the reading is unknown.
func HPAbort(hp, maxHP, pct int) bool {
	if maxHP <= 0 || hp < 0 {
		return false
	}
	return hp*100 < maxHP*pct
}

// NpcView is the part of an NPC the rat-damage check reads.
type NpcView struct {
	Name                string
	Health, TotalHealth int
}

// RatsHurt says a rat shows a hitmark with less than full health: the account is hurting one, which it must never do (a dead
// rat respawns somewhere else and the phase is gone). It is checked every tick.
func RatsHurt(npcs []NpcView, ratName string) (string, bool) {
	for _, n := range npcs {
		if !strings.EqualFold(n.Name, ratName) || n.TotalHealth <= 0 {
			continue
		}
		if n.Health < n.TotalHealth {
			return fmt.Sprintf("a %s is at %d of %d hitpoints", strings.ToLower(n.Name), n.Health, n.TotalHealth), true
		}
	}
	return "", false
}
