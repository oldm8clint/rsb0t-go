package fisherplan

import (
	"math"
	"strings"

	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/freshstart"
)

// ---- staying alive ----
//
// Survival is the fisher's own eat, flee and rest rules. ProgressiveFisher.ts has none: it never
// carries food, and the Go port's only protection was Guardian's in-combat flee, which reacts after
// a hit lands and cannot outrun an already-fatal hit or a second one mid-flee. These rules act on
// the HP left instead, before it is critical.
//
// Why the default margin is 4: the content's own NPC melee max hit
// (skill_combat/scripts/npc/npc_combat_melee.rs2, (((strength + 9) * 64) + 320) / 640 with no
// strength bonus) gives 3 for the level-25 White wolf (strength 16) and the Giant bat (strength 22),
// and 4 for the level-38 White wolf (strength 31) that killed accounts near the Catherby fishing
// spots. Eating at or below MaxHit+1 leaves one worst-case hit of room.

// Survival settings defaults.
const (
	DefaultFoodCount   = 3
	DefaultEatBelowHP  = 50
	DefaultFleeBelowHP = 30
	DefaultMaxHit      = 4
	// MaxFoodCount caps the food kept in the pack: every slot of food is a slot not holding fish.
	MaxFoodCount = 14
)

// Survival is the resolved eat/flee settings.
type Survival struct {
	Enabled bool
	// Foods are the FoodOptions names the fisher may eat and withdraw; empty means any of them.
	Foods []string
	// FoodCount is how many bites the bank trip leaves in the pack.
	FoodCount int
	// EatBelow and FleeBelow are shares of max HP, 0-1.
	EatBelow  float64
	FleeBelow float64
	// MaxHit is the largest single hit expected; eating starts at MaxHit+1 HP whatever EatBelow says.
	MaxHit int
}

// DefaultSurvival is on, with any food, three bites, eat at 50%, flee at 30% and a 4 HP margin.
func DefaultSurvival() Survival {
	return Survival{
		Enabled:   false,
		FoodCount: DefaultFoodCount,
		EatBelow:  DefaultEatBelowHP / 100.0,
		FleeBelow: DefaultFleeBelowHP / 100.0,
		MaxHit:    DefaultMaxHit,
	}
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Normalise clamps every field into range: FoodCount 0-14, EatBelow 10-90%, FleeBelow 5-80% and never
// above EatBelow, MaxHit 0-30, and Foods reduced to known foods in their FoodOptions spelling.
func (s Survival) Normalise() Survival {
	s.FoodCount = clampI(s.FoodCount, 0, MaxFoodCount)
	s.EatBelow = clampF(s.EatBelow, 0.1, 0.9)
	s.FleeBelow = clampF(s.FleeBelow, 0.05, 0.8)
	if s.FleeBelow > s.EatBelow {
		s.FleeBelow = s.EatBelow
	}
	s.MaxHit = clampI(s.MaxHit, 0, 30)
	var foods []string
	seen := map[string]bool{}
	for _, f := range s.Foods {
		if base, ok := fighterfood.BaseFood(f); ok && !seen[base] {
			seen[base] = true
			foods = append(foods, base)
		}
	}
	s.Foods = foods
	return s
}

// ParseFoodList reads the food setting: comma-separated food names, where blank or "any" means every
// food. Unknown names are dropped by Normalise, not here.
func ParseFoodList(csv string) []string {
	var out []string
	for _, part := range strings.Split(csv, ",") {
		name := strings.TrimSpace(part)
		if name == "" || strings.EqualFold(name, "any") {
			continue
		}
		out = append(out, name)
	}
	return out
}

// Allows reports whether name (any pack form) is a food this fisher eats.
func (s Survival) Allows(name string) bool {
	base, ok := fighterfood.BaseFood(name)
	if !ok {
		return false
	}
	if len(s.Foods) == 0 {
		return true
	}
	for _, f := range s.Foods {
		if strings.EqualFold(f, base) {
			return true
		}
	}
	return false
}

// EatAt is the HP at or below which to eat: EatBelow of max, raised to MaxHit+1, and never at full HP.
func (s Survival) EatAt(maxHP int) int {
	at := int(math.Ceil(s.EatBelow * float64(maxHP)))
	if s.MaxHit+1 > at {
		at = s.MaxHit + 1
	}
	if at > maxHP-1 {
		at = maxHP - 1
	}
	return at
}

// FleeAt is the HP at or below which the account is critical: FleeBelow of max, raised to MaxHit,
// and never above EatAt.
func (s Survival) FleeAt(maxHP int) int {
	at := int(math.Ceil(s.FleeBelow * float64(maxHP)))
	if s.MaxHit > at {
		at = s.MaxHit
	}
	if eat := s.EatAt(maxHP); at > eat {
		at = eat
	}
	return at
}

// ResumeAt is the HP a resting account waits for with no food: freshstart.ResumeAtHP of max, the same
// share pickpocketing resumes at.
func ResumeAt(maxHP int) int {
	return int(math.Ceil(freshstart.ResumeAtHP * float64(maxHP)))
}

// SurvivalInput is the live state DecideSurvival reads.
type SurvivalInput struct {
	HP, MaxHP int
	// Known is false until the server has sent the Hitpoints stat.
	Known bool
	// Food is the bites of allowed food in the pack.
	Food     int
	InCombat bool
	// InCatherby and AtBank place the account: fleeing and resting only happen around Catherby, where
	// the bank is the refuge.
	InCatherby bool
	AtBank     bool
	// Resting is the previous pass's Rest, for the hysteresis.
	Resting bool
}

// SurvivalAction is what to do about HP this pass. Eat and Flee can both be set: eating does not
// clear the walk queue (Engine-TS Player.clearPendingAction leaves it intact), so a critical account
// in combat eats on the way to the bank.
type SurvivalAction struct {
	Eat  bool
	Flee bool
	// Rest holds the account at the bank with no food until HP reaches ResumeAt.
	Rest bool
}

// Interrupts reports whether the action must cut into whatever the loop is doing.
func (a SurvivalAction) Interrupts() bool { return a.Eat || a.Flee }

// DecideSurvival picks the survival action from live state alone.
//
// Eat whenever allowed food is held and HP is at or below EatAt. Flee to the bank when HP is at or
// below FleeAt and either no food is left or the account is under attack, since one bite does not
// outheal a pack of wolves. With no food, flee at the first hit in combat whatever the HP: nothing
// will heal it, and two wolves took a live 10 HP account from 7 to 3 between two checks, past the
// flee threshold before it could act. With no food, rest once critical and keep resting until
// ResumeAt.
func DecideSurvival(s Survival, in SurvivalInput) SurvivalAction {
	var a SurvivalAction
	if !s.Enabled || !in.Known || in.MaxHP <= 0 || in.HP <= 0 {
		return a
	}
	critical := in.HP <= s.FleeAt(in.MaxHP)
	a.Eat = in.Food > 0 && in.HP <= s.EatAt(in.MaxHP)
	a.Flee = in.InCatherby && !in.AtBank && ((critical && in.InCombat) || (in.Food == 0 && (critical || in.InCombat)))
	if in.InCatherby && in.Food == 0 {
		if in.Resting {
			a.Rest = in.HP < ResumeAt(in.MaxHP)
		} else {
			a.Rest = critical
		}
	}
	return a
}

// FoodHeld counts the bites of allowed food in pack (one per item; the caller leaves notes out).
func (s Survival) FoodHeld(pack []freshstart.PackItem) int {
	n := 0
	for _, item := range pack {
		if s.Allows(item.Name) {
			c := item.Count
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// FoodNames are the allowed food names in pack, once each in pack order: the names a bank deposit
// must keep.
func (s Survival) FoodNames(pack []freshstart.PackItem) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range pack {
		name := strings.TrimSpace(item.Name)
		key := strings.ToLower(name)
		if s.Allows(name) && !seen[key] {
			seen[key] = true
			out = append(out, name)
		}
	}
	return out
}

// KeepNames are every pack form of every allowed food, for the mule hand-over's keep list: banked
// food stays with the fisher. Nil when survival is off.
func (s Survival) KeepNames() []string {
	if !s.Enabled {
		return nil
	}
	foods := s.Foods
	if len(foods) == 0 {
		foods = fighterfood.FoodOptions
	}
	var out []string
	for _, f := range foods {
		out = append(out, fighterfood.FoodForms(f)...)
	}
	return out
}

// PickFood is which pack food to eat: the biggest bite that does not overheal, or the smallest when
// every bite overheals. A critical account takes the biggest bite whatever it wastes.
func (s Survival) PickFood(pack []freshstart.PackItem, hp, maxHP int) (string, bool) {
	missing := maxHP - hp
	critical := hp <= s.FleeAt(maxHP)
	best, bestHeal := "", 0
	fits := false
	for _, item := range pack {
		name := strings.TrimSpace(item.Name)
		if !s.Allows(name) {
			continue
		}
		heal := fighterfood.FoodHealAmount(name)
		switch {
		case best == "":
			best, bestHeal, fits = name, heal, heal <= missing
		case critical:
			if heal > bestHeal {
				best, bestHeal = name, heal
			}
		case heal <= missing && (!fits || heal > bestHeal):
			best, bestHeal, fits = name, heal, true
		case !fits && heal < bestHeal:
			best, bestHeal = name, heal
		}
	}
	return best, best != ""
}

// FoodToWithdraw is which banked food tops the pack up to FoodCount given held bites. Foods rank by
// the heal this account can use (capped at its max HP), then the smaller bite, so a 10 HP account
// takes tuna before lobster and saves the lobster for later.
func (s Survival) FoodToWithdraw(bank []freshstart.PackItem, held, maxHP int) []freshstart.SellLine {
	want := s.FoodCount - held
	if !s.Enabled || want <= 0 {
		return nil
	}
	totals := map[string]int{}
	var names []string
	for _, item := range bank {
		name := strings.TrimSpace(item.Name)
		if !s.Allows(name) {
			continue
		}
		if _, ok := totals[name]; !ok {
			names = append(names, name)
		}
		c := item.Count
		if c < 1 {
			c = 1
		}
		totals[name] += c
	}
	useful := func(name string) int {
		h := fighterfood.FoodHealAmount(name)
		if maxHP > 0 && h > maxHP {
			return maxHP
		}
		return h
	}
	less := func(a, b string) bool {
		if ua, ub := useful(a), useful(b); ua != ub {
			return ua > ub
		}
		if ha, hb := fighterfood.FoodHealAmount(a), fighterfood.FoodHealAmount(b); ha != hb {
			return ha < hb
		}
		return a < b
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && less(names[j], names[j-1]); j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	var out []freshstart.SellLine
	for _, name := range names {
		if want <= 0 {
			break
		}
		n := totals[name]
		if n > want {
			n = want
		}
		out = append(out, freshstart.SellLine{Name: name, Count: n})
		want -= n
	}
	return out
}
