// Package deathreport words the one log line every death gets, from what the client read at it
// (gamestate.DeathContext) and the names the cache gives the ids. It is pure: no client, no fleet,
// no cache of its own, so the wording and the area table are unit-tested.
//
// The line is account-agnostic on purpose (the log is searched by cause, and run logs hold account
// names): where, what was facing the account, the hitpoints of the fight, the food left, and the
// alchemy stock that was in the pack -- the owner's complaint of 8 Oct 2026 was that a death costs
// the stock, and nothing said how much a death had carried.
//
//	DEATH at 2508,3097,0 (Yanille west ogres): killed by 2 Ogre, HP 50/50 before, 7 left, food left 0,
//	carrying 946 alchables ~560,000 coins (721 Yew shortbow, 225 Yew longbow), 132 Nature rune, 179 Fire rune
package deathreport

import (
	"fmt"
	"sort"
	"strings"
)

// PackItem is one backpack slot with its name resolved.
type PackItem struct {
	Name  string
	Count int
	// Noted is a bank note (it carries the item's name and cannot be eaten).
	Noted bool
}

// Input is everything the line says.
type Input struct {
	HaveTile    bool
	X, Z, Level int
	// HPPeak is the highest hitpoints reading in the last 30 client ticks before the death (gamestate.DeathContext), NOT the fight's
	// starting hitpoints: a fight that began minutes earlier at full health still reads as the peak of its last 18 seconds
	// (the 17 Oct 12:23 line "HP 18/55 before, 7 left" read at or under 18 for all of its 18 seconds, and those readings came out
	// of a lagging view's replayed backlog). HPLast is the last reading above zero and MaxHP the level.
	HPPeak, HPLast, MaxHP int
	// Attackers and Nearby are NPC names (one entry per NPC); Players is how many players faced the account.
	Attackers, Nearby []string
	Players           int
	Poisoned          bool
	Pack              []PackItem
	// IsFood says whether an unnoted pack item is food; IsAlch whether a name is alchemy stock (the allow-list);
	// AlchValue is the coins one High Level Alchemy cast pays for the name. Any may be nil.
	IsFood    func(name string) bool
	IsAlch    func(name string) bool
	AlchValue func(name string) int
}

// Stock is the alchemy stock a pack held.
type Stock struct {
	// Alchables is the count of alch-list items (casts they fund); Value the coins those casts pay.
	Alchables, Value int
	// Lines are "721 Yew shortbow" style entries, biggest first; Natures and Fire the runes.
	Lines         []string
	Natures, Fire int
}

// Summarise reads the alchemy stock off a pack.
func Summarise(pack []PackItem, isAlch func(string) bool, value func(string) int) Stock {
	var st Stock
	type row struct {
		name  string
		count int
	}
	var rows []row
	for _, it := range pack {
		switch {
		case strings.EqualFold(it.Name, "Nature rune"):
			st.Natures += it.Count
		case strings.EqualFold(it.Name, "Fire rune"):
			st.Fire += it.Count
		case isAlch != nil && isAlch(it.Name):
			st.Alchables += it.Count
			if value != nil {
				st.Value += value(it.Name) * it.Count
			}
			merged := false
			for i := range rows {
				if rows[i].name == it.Name {
					rows[i].count += it.Count
					merged = true
				}
			}
			if !merged {
				rows = append(rows, row{it.Name, it.Count})
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].count > rows[j].count })
	for _, r := range rows {
		st.Lines = append(st.Lines, fmt.Sprintf("%d %s", r.count, r.name))
	}
	return st
}

// Describe is the stock as the tail of the line: "946 alchables ~560,000 coins (721 Yew shortbow, ...), 132 Nature
// rune, 179 Fire rune", or "no alch stock".
func (s Stock) Describe() string {
	if s.Alchables == 0 && s.Natures == 0 && s.Fire == 0 {
		return "no alch stock"
	}
	var parts []string
	if s.Alchables > 0 {
		head := fmt.Sprintf("%d alchables", s.Alchables)
		if s.Value > 0 {
			head += " ~" + Commas(s.Value) + " coins"
		}
		lines := s.Lines
		if len(lines) > 6 {
			lines = append(append([]string(nil), lines[:6]...), fmt.Sprintf("%d more kind(s)", len(s.Lines)-6))
		}
		parts = append(parts, head+" ("+strings.Join(lines, ", ")+")")
	}
	if s.Natures > 0 {
		parts = append(parts, fmt.Sprintf("%d Nature rune", s.Natures))
	}
	if s.Fire > 0 {
		parts = append(parts, fmt.Sprintf("%d Fire rune", s.Fire))
	}
	return strings.Join(parts, ", ")
}

// Commas writes n with thousands separators.
func Commas(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprint(n)
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// Killers words what was facing the account: "2 Ogre, Moss giant", "1 player", "poison", or "unknown".
func Killers(in Input) string {
	var parts []string
	counts := map[string]int{}
	var order []string
	for _, n := range in.Attackers {
		if counts[n] == 0 {
			order = append(order, n)
		}
		counts[n]++
	}
	for _, n := range order {
		if counts[n] == 1 {
			parts = append(parts, n)
		} else {
			parts = append(parts, fmt.Sprintf("%d %s", counts[n], n))
		}
	}
	if in.Players > 0 {
		parts = append(parts, fmt.Sprintf("%d player(s)", in.Players))
	}
	if in.Poisoned {
		parts = append(parts, "poison")
	}
	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}
	if len(in.Nearby) > 0 {
		return "unknown (nothing faced the account; near it: " + strings.Join(dedupe(in.Nearby), ", ") + ")"
	}
	return "unknown (nothing faced the account, nothing near it)"
}

func dedupe(names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// Line is the death line.
func Line(in Input) string {
	where := "an unknown tile"
	if in.HaveTile {
		where = fmt.Sprintf("%d,%d,%d (%s)", in.X, in.Z, in.Level, Area(in.X, in.Z, in.Level))
	}
	hp := "HP unknown"
	if in.HPPeak > 0 || in.HPLast > 0 {
		hp = fmt.Sprintf("HP %d/%d before, %d left", in.HPPeak, in.MaxHP, in.HPLast)
	}
	food := 0
	for _, it := range in.Pack {
		if !it.Noted && in.IsFood != nil && in.IsFood(it.Name) {
			food += it.Count
		}
	}
	stock := Summarise(in.Pack, in.IsAlch, in.AlchValue)
	return fmt.Sprintf("DEATH at %s: killed by %s, %s, food left %d, carrying %s",
		where, Killers(in), hp, food, stock.Describe())
}

// CauseKey is what faced the account, as a short label the death panel can group by: the distinct NPC names that
// were facing it (counts left out, "Ogre" whether one or three), "player" when a player was, "poison" when poisoned, or "unknown".
func CauseKey(in Input) string {
	var parts []string
	seen := map[string]bool{}
	for _, n := range in.Attackers {
		if !seen[n] {
			seen[n] = true
			parts = append(parts, n)
		}
	}
	sort.Strings(parts)
	if in.Players > 0 {
		parts = append(parts, "player")
	}
	if in.Poisoned {
		parts = append(parts, "poison")
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, ", ")
}
