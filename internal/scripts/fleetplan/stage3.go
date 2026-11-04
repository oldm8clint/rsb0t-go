package fleetplan

import (
	"strings"
)

// Rune is one rune cost line of a spell.
type Rune struct {
	Name  string
	Count int
}

// Spell is one standard-spellbook combat spell, exactly as the content defines it.
type Spell struct {
	// Name is the display name, and the name internal/actions.CombatSpellComponent resolves.
	Name string
	// Level is the content's own levelrequired.
	Level int
	// MaxHit is the content's own maxhit — the ceiling on one cast, not the average.
	MaxHit int
	// XP is the content's own experience field, in tenths (the dbrow's own units).
	XP int
	// Runes is the content's own runesrequired, null entries dropped.
	Runes []Rune
	// Members is the content's own members flag. A free world refuses a members spell outright,
	// so this is a hard gate, not a preference.
	Members bool
}

// CombatSpells is the standard spellbook's attack ladder, ported verbatim from
// magic_combat_spells.dbrow. Ordered by level, which is also damage order.
var CombatSpells = []Spell{
	{Name: "Wind Strike", Level: 1, MaxHit: 2, XP: 55, Runes: []Rune{{"Mind rune", 1}, {"Air rune", 1}}},
	{Name: "Water Strike", Level: 5, MaxHit: 4, XP: 75, Runes: []Rune{{"Mind rune", 1}, {"Water rune", 1}, {"Air rune", 1}}},
	{Name: "Earth Strike", Level: 9, MaxHit: 6, XP: 95, Runes: []Rune{{"Mind rune", 1}, {"Earth rune", 2}, {"Air rune", 1}}},
	{Name: "Fire Strike", Level: 13, MaxHit: 8, XP: 115, Runes: []Rune{{"Mind rune", 1}, {"Fire rune", 3}, {"Air rune", 2}}},
	{Name: "Wind Bolt", Level: 17, MaxHit: 9, XP: 135, Runes: []Rune{{"Chaos rune", 1}, {"Air rune", 2}}},
	{Name: "Water Bolt", Level: 23, MaxHit: 10, XP: 165, Runes: []Rune{{"Chaos rune", 1}, {"Water rune", 2}, {"Air rune", 2}}},
	{Name: "Earth Bolt", Level: 29, MaxHit: 11, XP: 195, Runes: []Rune{{"Chaos rune", 1}, {"Earth rune", 3}, {"Air rune", 2}}},
	{Name: "Fire Bolt", Level: 35, MaxHit: 12, XP: 225, Runes: []Rune{{"Chaos rune", 1}, {"Fire rune", 4}, {"Air rune", 3}}},
	{Name: "Wind Blast", Level: 41, MaxHit: 13, XP: 255, Runes: []Rune{{"Death rune", 1}, {"Air rune", 3}}},
	{Name: "Water Blast", Level: 47, MaxHit: 14, XP: 285, Runes: []Rune{{"Death rune", 1}, {"Water rune", 3}, {"Air rune", 3}}},
	{Name: "Earth Blast", Level: 53, MaxHit: 15, XP: 315, Runes: []Rune{{"Death rune", 1}, {"Earth rune", 4}, {"Air rune", 3}}},
	{Name: "Fire Blast", Level: 59, MaxHit: 16, XP: 345, Runes: []Rune{{"Death rune", 1}, {"Fire rune", 5}, {"Air rune", 4}}},
	{Name: "Wind Wave", Level: 62, MaxHit: 17, XP: 360, Members: true, Runes: []Rune{{"Blood rune", 1}, {"Air rune", 5}}},
	{Name: "Water Wave", Level: 65, MaxHit: 18, XP: 375, Members: true, Runes: []Rune{{"Blood rune", 1}, {"Water rune", 7}, {"Air rune", 5}}},
	{Name: "Earth Wave", Level: 70, MaxHit: 19, XP: 400, Members: true, Runes: []Rune{{"Blood rune", 1}, {"Earth rune", 7}, {"Air rune", 5}}},
	{Name: "Fire Wave", Level: 75, MaxHit: 20, XP: 425, Members: true, Runes: []Rune{{"Blood rune", 1}, {"Fire rune", 7}, {"Air rune", 5}}},
}

// ScarceRunes are the runes a hundred-account swarm cannot actually buy, as opposed to the ones
// it can. This is a supply fact read from the shops, not a price judgement:
//
//   - Aubury's runeshop (content/scripts/areas/area_varrock/configs/varrock.inv, [runeshop]) is
//     `scope=shared` — ONE stock pool for the whole world, not per account. It holds air 2000,
//     water/earth/fire 2000, mind 1000, body 1000, and then chaos 1000 and death 1000.
//   - Wave spells need blood runes, which Aubury does not stock at all.
//
// So a spell whose cost line includes one of these has a supply ceiling measured in hundreds of
// casts across the ENTIRE cohort, while a spell built from mind/air/water/earth/fire does not.
// Nothing here refuses a scarce spell — Prefer below only breaks ties the damage numbers leave
// open, and an operator who has solved rune supply another way (Runecrafting, the Mage Bank) can
// ignore it entirely.
var ScarceRunes = map[string]bool{
	"Chaos rune": true,
	"Death rune": true,
	"Blood rune": true,
}

// DefaultStage3MagicTarget is the Magic level Stage 3 trains to: 13, which is Fire Strike's own
// gate — and which is exactly the "initial 13 Magic target" the owner's table asked for.
//
// I previously recommended raising this to 17 for Wind Bolt and that recommendation was WRONG.
// It was made from a two-point comparison (Wind Strike vs Wind Bolt) that skipped the spell that
// actually matters. Measured live against the Kalphite Queen, naked with a staff of air, tank
// fixture, ~9 tiles out:
//
//	Wind Strike (level 1)   44 casts, 13 damage  = 0.30 a cast   1 mind + 1 air
//	Wind Bolt   (level 17)  60 casts, 37 damage  = 0.62 a cast   1 CHAOS + 2 air
//	Fire Strike (level 13)  39 casts, 32 damage  = 0.82 a cast   1 mind + 3 fire + 2 air
//
// Fire Strike beats Wind Bolt on damage AND costs none of the scarce shared-stock runes, which
// is the combination that matters for a cohort rather than one account. The content's own maxhit
// column says the same thing more cheaply — Fire Strike 8 against Wind Bolt 9 — so the bolt tier
// buys one point of ceiling for a rune the world only has a thousand of.
//
// The table's own "the initial 13 target does not imply all those spells are unlocked" line was
// right, and so was its 13.
const DefaultStage3MagicTarget = 13

// SpellByName finds a spell in the ladder, matched case-insensitively.
func SpellByName(name string) (Spell, bool) {
	want := strings.ToLower(strings.TrimSpace(name))
	for _, s := range CombatSpells {
		if strings.ToLower(s.Name) == want {
			return s, true
		}
	}
	return Spell{}, false
}

// NeedsScarceRune reports whether any of the spell's runes is one the shared shops cannot supply
// at cohort scale.
func (s Spell) NeedsScarceRune() bool {
	for _, r := range s.Runes {
		if ScarceRunes[r.Name] {
			return true
		}
	}
	return false
}

// RuneStock is what the account is actually carrying, by rune name.
type RuneStock map[string]int

// Provided is what a wielded staff supplies for free, by rune name — a Staff of air makes every
// Air rune cost zero, and so on. An empty/zero value means no staff.
type Provided map[string]bool

// CanCast reports whether the account can pay for one cast of s right now: every rune line is
// either supplied by the staff or present in the stock in sufficient quantity.
func (s Spell) CanCast(stock RuneStock, staff Provided) bool {
	for _, r := range s.Runes {
		if staff[r.Name] {
			continue
		}
		if stock[r.Name] < r.Count {
			return false
		}
	}
	return true
}

// CastsAffordable is how many casts of s the stock pays for, with the staff's runes free.
// Returns a large number when every rune is staff-supplied.
func (s Spell) CastsAffordable(stock RuneStock, staff Provided) int {
	const unlimited = 1 << 30
	best := unlimited
	for _, r := range s.Runes {
		if staff[r.Name] || r.Count <= 0 {
			continue
		}
		if n := stock[r.Name] / r.Count; n < best {
			best = n
		}
	}
	return best
}

// RunesForCasts is the shopping list for n casts of s, with the staff's runes excluded — the
// input a rune-acquisition strategy takes, whatever that strategy turns out to be.
//
// Deliberately expressed as "what is needed", not "buy this from Aubury": the acquisition side is
// an open plan-level question (the shared shops cannot supply a cohort — see ScarceRunes), so
// this stage computes the requirement and leaves where it comes from to the caller.
func RunesForCasts(s Spell, n int, staff Provided) []Rune {
	if n < 0 {
		n = 0
	}
	out := make([]Rune, 0, len(s.Runes))
	for _, r := range s.Runes {
		if staff[r.Name] {
			continue
		}
		out = append(out, Rune{Name: r.Name, Count: r.Count * n})
	}
	return out
}

// StaffProvides maps a staff's item name to the rune it supplies for free. The four elemental
// staves are the whole free-to-play set (content/scripts/skill_combat/configs/magic/staves.obj);
// a plain staff or magic staff supplies nothing.
func StaffProvides(staffName string) Provided {
	switch strings.ToLower(strings.TrimSpace(staffName)) {
	case "staff of air":
		return Provided{"Air rune": true}
	case "staff of water":
		return Provided{"Water rune": true}
	case "staff of earth":
		return Provided{"Earth rune": true}
	case "staff of fire":
		return Provided{"Fire rune": true}
	}
	return Provided{}
}

func Itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
