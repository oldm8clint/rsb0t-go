// Package levels is the game's experience curve as plain arithmetic, ported from rsb0t-scripts'
// levels.ts. It is pure, so every script planner can use it without touching the client.
package levels

import "math"

// MaxLevel is the highest skill level.
const MaxLevel = 99

// xpTable holds the experience each level starts at, built once and only read afterwards.
var xpTable = func() [MaxLevel + 1]int {
	var out [MaxLevel + 1]int
	points := 0
	for level := 1; level < MaxLevel; level++ {
		points += int(math.Floor(float64(level) + 300*math.Pow(2, float64(level)/7)))
		out[level+1] = points / 4
	}
	return out
}()

// XPAtLevel returns the experience a level starts at. Out-of-range levels clamp to 1 and 99.
func XPAtLevel(level int) int {
	if level < 1 {
		level = 1
	}
	if level > MaxLevel {
		level = MaxLevel
	}
	return xpTable[level]
}

// LevelFromXP returns the level a total experience amount has reached — the inverse of
// XPAtLevel, found by scanning the same table (99 entries, not worth a binary search).
func LevelFromXP(xp int) int {
	level := 1
	for l := 2; l <= MaxLevel; l++ {
		if xp < xpTable[l] {
			break
		}
		level = l
	}
	return level
}

// Progress is how far through its level an experience total is.
type Progress struct {
	Level int
	// Fraction is 0 to 1 through the current level; 1 at 99.
	Fraction  float64
	Remaining int
}

// LevelProgress reports how far through level an experience total is, and how much is left.
func LevelProgress(level, xp int) Progress {
	if level >= MaxLevel {
		return Progress{Level: MaxLevel, Fraction: 1, Remaining: 0}
	}
	base, next := XPAtLevel(level), XPAtLevel(level+1)
	span := next - base
	into := xp - base
	if into < 0 {
		into = 0
	}
	if into > span {
		into = span
	}
	p := Progress{Level: level, Fraction: 1, Remaining: next - xp}
	if span > 0 {
		p.Fraction = float64(into) / float64(span)
	}
	if p.Remaining < 0 {
		p.Remaining = 0
	}
	return p
}

// ETAHours is the hours to close a gap at a rate; ok is false when the gap is shut or nothing is
// going in.
func ETAHours(remaining int, xpPerHour float64) (hours float64, ok bool) {
	if remaining <= 0 || xpPerHour <= 0 {
		return 0, false
	}
	return float64(remaining) / xpPerHour, true
}
