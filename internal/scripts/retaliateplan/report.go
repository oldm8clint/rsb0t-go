package retaliateplan

import "fmt"

// Window is what the loop counted since its last log line.
type Window struct {
	Ticks int
	// Logs is the "You get some ... logs" messages, XP the Woodcutting experience gained.
	Logs int
	XP   float64
	// Rolls is the roll animations seen; Gaps2 the roll gaps that were exactly 2 ticks, Gaps the roll gaps counted.
	Rolls, Gaps2, Gaps int
	// HitGaps is the last hit gaps seen (up to 6), for the line.
	HitGaps []int
	// Clears is the clear actions written and Drops the logs they dropped.
	Clears, Drops int
	// Lead is the controller's aim now.
	Lead int
	// Pattern is the hit spacing shape.
	Pattern Pattern
	// Locked is the controller's phase state at the end of the window.
	Locked bool
}

// PerHour scales n over ticks (0.6 s each) to an hour.
func PerHour(n float64, ticks int) float64 {
	if ticks <= 0 {
		return 0
	}
	return n * float64(TicksPerHour) / float64(ticks)
}

// TicksPerRoll is the measured ticks per roll, 0 when no roll was seen.
func (w Window) TicksPerRoll() float64 {
	if w.Rolls <= 0 {
		return 0
	}
	return float64(w.Ticks) / float64(w.Rolls)
}

// Share2 is the share of roll gaps that were 2 ticks.
func (w Window) Share2() float64 {
	if w.Gaps <= 0 {
		return 0
	}
	return float64(w.Gaps2) / float64(w.Gaps)
}

// Line is the rolling log line: ticks per roll, hit spacing, logs per hour and xp per hour.
func (w Window) Line() string {
	state := "locked"
	if !w.Locked {
		state = "NOT locked"
	}
	return fmt.Sprintf("%d ticks: %.2f ticks per roll (%d rolls, %.0f%% of the gaps were 2 ticks), hits %s (gaps %v), %s, lead %d; %.0f logs/h, %.0f xp/h; %d clears (%d logs dropped)",
		w.Ticks, w.TicksPerRoll(), w.Rolls, w.Share2()*100, w.Pattern, w.HitGaps, state, w.Lead, PerHour(float64(w.Logs), w.Ticks), PerHour(w.XP, w.Ticks), w.Clears, w.Drops)
}

// HitGapsOf returns the gaps between the last n+1 hits.
func HitGapsOf(hits []int, n int) []int {
	var g []int
	for i := len(hits) - n; i < len(hits); i++ {
		if i >= 1 {
			g = append(g, hits[i]-hits[i-1])
		}
	}
	return g
}
