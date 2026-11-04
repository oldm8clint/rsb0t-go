package runerunnerplan

// Shortage priority: the runes the mule is out of are bought first, and up to the shop's own
// price ceiling.
//
// LIVE, 7 October 2026 07:15: the mule held 5.6 million coins, 395,000 Water, 194,000 Body, 83,000
// Air and 40,000 Law runes, and 109 Nature, no Fire, no Chaos, no Death. Magic training (Wind
// Bolt is a Chaos rune a cast), walk alch and fight alch (a Nature and five Fire a cast) were
// starving fleet-wide while the two RuneRunners that were running spent their coins on Water and
// Body runes nobody casts and left the scarce lines at the price caps.
//
// Why the caps, not the hops, were the wall. A shelf regrows one rune every RateTicks (Chaos one
// a minute, Death one in ninety seconds, Nature one in three minutes) and only while it is below
// its base stock, so the hourly flow does not depend on how deep a runner buys. The stock that is
// already down at the caps is another matter: Aubury's Chaos shelf stood at 89 (of 1000) at 90
// coins, and the default cap is three times the base price (45), so the whole deep end of every
// shelf in the circuit -- hundreds of runes a shelf, nine shelves -- was left unbought while the
// mule held coins enough to buy all of it (a rune at the ceiling costs six times its base price:
// Chaos 90, Death 180, Nature 120, Fire 24).
//
// So when the mule's published bank holds fewer than ShortageLevels[rune] of a scarce rune the
// runner (1) moves that rune to the front of its priority list, so the coins of a trip go to it
// before the Water and Body that fill the mule's bank, and (2) raises its cap to the price
// ceiling. A rune the mule holds enough of keeps its place and its cap. Nothing changes when the
// mule has published no bank.

import (
	"fmt"
	"strings"
)

// CeilingMultiple is the price ceiling as a multiple of a rune's base price: shop.rs2 stops the
// rise at six times (scarabplan.RuneShop.PriceAt).
const CeilingMultiple = 6

// ShortageOrder is the order short runes are bought in. Nature first: only Lundail and the
// Wizards' Guild stock it, a rune every three minutes a shelf, so it is the one that runs out for
// good. Chaos next (every Wind Bolt), then Death (Wind Blast) and Fire (alchemy, five a cast).
var ShortageOrder = []string{"Nature rune", "Chaos rune", "Death rune", "Fire rune"}

// ShortageLevels is how many of each scarce rune the mule should hold before the runner stops
// treating it as short: about twenty kits of it. Under it the rune is short.
var ShortageLevels = map[string]int{
	"Nature rune": 2000,
	"Chaos rune":  3000,
	"Death rune":  2000,
	"Fire rune":   5000,
}

// ShortRune is one rune found short and what the mule held.
type ShortRune struct {
	Rune string
	Held int
	// Cap is the price cap the rune is bought under now.
	Cap int
}

// ShortageInput is what a shortage plan is made from.
type ShortageInput struct {
	// Runes and Caps are the runner's own settings: the priority order and the price caps.
	Runes []string
	Caps  Caps
	// Mule answers how many of an item the mule's published bank holds; nil when it has published
	// none (no shortage can be told, so nothing changes).
	Mule func(item string) (int, bool)
}

// ShortagePlan is the priority order and caps to buy under now.
type ShortagePlan struct {
	Runes []string
	Caps  Caps
	Short []ShortRune
}

// CeilingCap is the price cap that buys a rune down to the price ceiling (CeilingMultiple times
// its base price), never above MaxPriceCap; 0 for a rune no built shop sells.
func CeilingCap(rune string) int {
	for _, key := range DefaultSites() {
		s, _ := Site(key)
		if l, ok := s.Shop.Line(rune); ok {
			c := CeilingMultiple * l.Cost
			if c > MaxPriceCap {
				c = MaxPriceCap
			}
			return c
		}
	}
	return 0
}

// PlanShortage reorders and re-caps the runner's runes for a mule that is short of some. The
// input slices and map are not changed.
func PlanShortage(in ShortageInput) ShortagePlan {
	out := ShortagePlan{Runes: append([]string{}, in.Runes...), Caps: Caps{}}
	for k, v := range in.Caps {
		out.Caps[k] = v
	}
	if in.Mule == nil {
		return out
	}
	listed := map[string]string{}
	for _, r := range in.Runes {
		listed[strings.ToLower(r)] = r
	}
	short := map[string]bool{}
	var front []string
	for _, name := range ShortageOrder {
		r, ok := listed[strings.ToLower(name)]
		if !ok {
			continue
		}
		level := ShortageLevels[name]
		held, known := in.Mule(r)
		if !known || held >= level {
			continue
		}
		short[r] = true
		front = append(front, r)
		capGP := out.Caps.For(r)
		if ceil := CeilingCap(r); ceil > capGP {
			capGP = ceil
		}
		out.Caps[r] = capGP
		out.Short = append(out.Short, ShortRune{Rune: r, Held: held, Cap: capGP})
	}
	if len(front) == 0 {
		return out
	}
	rest := make([]string, 0, len(in.Runes))
	for _, r := range in.Runes {
		if !short[r] {
			rest = append(rest, r)
		}
	}
	out.Runes = append(front, rest...)
	return out
}

// String is the plan's one log line body: the short runes with what the mule holds and the cap.
func (p ShortagePlan) String() string {
	if len(p.Short) == 0 {
		return "the mule is not short of any scarce rune"
	}
	parts := make([]string, 0, len(p.Short))
	for _, s := range p.Short {
		parts = append(parts, fmt.Sprintf("%s held=%d cap=%d", s.Rune, s.Held, s.Cap))
	}
	return strings.Join(parts, ", ")
}
