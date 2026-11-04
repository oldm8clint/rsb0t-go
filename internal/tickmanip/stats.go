package tickmanip

import (
	"fmt"
	"math"
	"sort"
)

// Rate is a count of events over a number of server ticks: the unit every number in the lab report is
// quoted in, with its sample size attached so nobody reads 21 successes as if it were 2,100.
type Rate struct {
	Count int
	Ticks int
}

// Per100 is events per 100 ticks.
func (r Rate) Per100() float64 { return PerHundred(r.Count, r.Ticks) }

// PerTick is events per tick.
func (r Rate) PerTick() float64 {
	if r.Ticks <= 0 {
		return 0
	}
	return float64(r.Count) / float64(r.Ticks)
}

// z975 is the 97.5th percentile of the standard normal, for a two-sided 95% interval.
const z975 = 1.959964

// CI95 is a 95% confidence interval for the underlying rate per 100 ticks, treating the count as
// Poisson (a roll or a catch is an event in a fixed observation window). It uses the Wilson-Hilferty
// approximation to the exact (chi-square) limits, which stays sensible at the small counts a harpoon
// spot gives: with Count 0 the lower limit is 0 and the upper limit is 3.7 events.
func (r Rate) CI95() (lo, hi float64) {
	if r.Ticks <= 0 {
		return 0, 0
	}
	c := float64(r.Count)
	if r.Count > 0 {
		lo = c * math.Pow(1-1/(9*c)-z975/(3*math.Sqrt(c)), 3)
		if lo < 0 {
			lo = 0
		}
	}
	c1 := c + 1
	hi = c1 * math.Pow(1-1/(9*c1)+z975/(3*math.Sqrt(c1)), 3)
	return 100 * lo / float64(r.Ticks), 100 * hi / float64(r.Ticks)
}

// String prints "12.34/100 ticks [11.0, 13.8] (n=37 over 300 ticks)".
func (r Rate) String() string {
	lo, hi := r.CI95()
	return fmt.Sprintf("%.2f/100t [%.1f, %.1f] (n=%d in %d ticks)", r.Per100(), lo, hi, r.Count, r.Ticks)
}

// Ratio is a's rate over b's, with a 95% interval from the standard log-ratio approximation: the
// variance of log(count) is 1/count for a Poisson count. ok is false when either count is zero, where
// the approximation is meaningless and the honest report is the two counts themselves.
func Ratio(a, b Rate) (ratio, lo, hi float64, ok bool) {
	if a.Count == 0 || b.Count == 0 || a.Ticks <= 0 || b.Ticks <= 0 {
		return 0, 0, 0, false
	}
	ratio = a.PerTick() / b.PerTick()
	sd := math.Sqrt(1/float64(a.Count) + 1/float64(b.Count))
	return ratio, ratio * math.Exp(-z975*sd), ratio * math.Exp(z975*sd), true
}

// Histogram counts each distinct value, for roll intervals ("3 x 41, 4 x 2").
func Histogram(vals []int) map[int]int {
	out := map[int]int{}
	for _, v := range vals {
		out[v]++
	}
	return out
}

// HistogramString prints a histogram in key order, "3x41 4x2".
func HistogramString(h map[int]int) string {
	keys := make([]int, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	s := ""
	for i, k := range keys {
		if i > 0 {
			s += " "
		}
		s += fmt.Sprintf("%dx%d", k, h[k])
	}
	if s == "" {
		return "none"
	}
	return s
}

// Mean is the arithmetic mean (0 for no values).
func Mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

// SD is the sample standard deviation (0 for fewer than two values).
func SD(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	m := Mean(vals)
	ss := 0.0
	for _, v := range vals {
		ss += (v - m) * (v - m)
	}
	return math.Sqrt(ss / float64(len(vals)-1))
}

// SameInts reports whether two int slices are equal.
func SameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// DiffInts says how two tick lists differ: the values only in want ("missed") and only in got
// ("extra"). Both inputs are ascending.
func DiffInts(want, got []int) (missed, extra []int) {
	i, j := 0, 0
	for i < len(want) || j < len(got) {
		switch {
		case i < len(want) && j < len(got) && want[i] == got[j]:
			i++
			j++
		case j >= len(got) || (i < len(want) && want[i] < got[j]):
			missed = append(missed, want[i])
			i++
		default:
			extra = append(extra, got[j])
			j++
		}
	}
	return missed, extra
}
