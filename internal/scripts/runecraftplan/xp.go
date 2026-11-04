package runecraftplan

import (
	"strconv"
	"strings"

	"rsb0t-go/internal/scripts/levels"
)

// XPForLevel is the experience Runecrafting level starts at (the game's curve, internal/scripts/levels).
func XPForLevel(level int) int { return levels.XPAtLevel(level) }

// LevelForXP is the level a total experience has reached.
func LevelForXP(xp int) int { return levels.LevelFromXP(xp) }

// RunesFor is the runes essence bound at level make at altar a.
func RunesFor(a Altar, level, essence int) int {
	if essence < 0 {
		return 0
	}
	return essence * a.RunesPerEssence(level)
}

// XP10For is the experience (tenths of a point, as the engine counts it) essence bound at a gives. The
// server then multiplies by the world's rate; the first live craft measures it (Verify below).
func XP10For(a Altar, essence int) int { return max(0, essence) * a.XP10 }

// XPFor is XP10For in whole points, as the client reads them (UPDATE_STAT carries the engine's tenths
// divided by ten and floored).
func XPFor(a Altar, essence int) int { return XP10For(a, essence) / 10 }

// EssenceForXP is the essence that takes xp experience at altar a, rounded up (the level plan is a
// budget: one load too many is better than a level short).
func EssenceForXP(a Altar, xp int) int {
	if xp <= 0 || a.XP10 <= 0 {
		return 0
	}
	return (xp*10 + a.XP10 - 1) / a.XP10
}

// Segment is one altar's part of a level plan: the essence from a starting experience up to the
// level where the next altar takes over, or the target.
type Segment struct {
	Altar Altar
	// FromLevel and ToLevel are the levels the segment starts and ends at; Essence the essence it takes.
	FromLevel, ToLevel int
	Essence            int
}

// Budget is the whole plan from an experience total to a target level.
type Budget struct {
	Segments []Segment
	// Total is the essence of every segment.
	Total int
}

// Of is the essence of the segment at altar key (0 for none).
func (b Budget) Of(key string) int {
	for _, s := range b.Segments {
		if s.Altar.Key == key {
			return s.Essence
		}
	}
	return 0
}

// BudgetFrom plans from experience xp to level target on the altars PickAltar chooses (members false
// stops at Body: Nature is refused). It is exact in the game's own arithmetic -- the essence that
// takes the experience from where it is to where the next altar's level starts, rounded up -- so
// Runecraft 1 to 44 is Air 894 and Body 6,824, 7,718 essence (Air alone would need 11,130 for the same
// experience at 5.0 a rune essence).
func BudgetFrom(xp, target int, members bool) Budget {
	var b Budget
	level := LevelForXP(xp)
	for level < target && level < 99 {
		a, ok, _ := PickAltar(level, members, nil)
		if !ok {
			break
		}
		end := target
		for _, next := range Altars() {
			if next.Level > level && next.Level < end {
				end = next.Level
				break
			}
		}
		need := XPForLevel(end) - max(xp, XPForLevel(level))
		seg := Segment{Altar: a, FromLevel: level, ToLevel: end, Essence: EssenceForXP(a, need)}
		if seg.Essence > 0 {
			b.Segments = append(b.Segments, seg)
			b.Total += seg.Essence
		}
		xp = max(xp, XPForLevel(end))
		level = end
	}
	return b
}

// LevelBudget is BudgetFrom the start of level from to level target.
func LevelBudget(from, target int, members bool) Budget {
	return BudgetFrom(XPForLevel(from), target, members)
}

// Describe is the budget as a line for a log: "Air 894, Body 6,824".
func (b Budget) Describe() string {
	if len(b.Segments) == 0 {
		return "nothing"
	}
	parts := make([]string, 0, len(b.Segments))
	for _, s := range b.Segments {
		parts = append(parts, s.Altar.Name+" "+Thousands(s.Essence))
	}
	return strings.Join(parts, ", ")
}

// Thousands writes n with thousands separators: 7,718.
func Thousands(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

// Verify is the check a craft must pass to count: the essence is gone and the Runecrafting experience
// rose by what the table says. gain is the points the stat gained; ok is false when the essence left
// but no experience came (the craft did not happen, or the server rates XP differently -- the caller
// logs the measured rate either way).
type Verify struct {
	Essence, Left int
	XPGain        int
	ExpectedXP    int
	// Rate is XPGain over ExpectedXP (1.0 at the live rate the owner measured on 13 Oct 2026), 0 when
	// nothing was expected.
	Rate float64
	OK   bool
}

// VerifyCraft judges a craft of essence at a: left is the essence still in the pack, xpGain the
// Runecrafting experience gained meanwhile. A craft is done when no essence is left AND experience
// came; the rate is measured, not assumed.
func VerifyCraft(a Altar, essence, left, xpGain int) Verify {
	v := Verify{Essence: essence, Left: left, XPGain: xpGain, ExpectedXP: XPFor(a, essence)}
	if v.ExpectedXP > 0 {
		v.Rate = float64(xpGain) / float64(v.ExpectedXP)
	}
	v.OK = essence > 0 && left == 0 && xpGain > 0
	return v
}
