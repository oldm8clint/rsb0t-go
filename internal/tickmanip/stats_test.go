package tickmanip

import (
	"math"
	"reflect"
	"testing"
)

func TestRatePer100(t *testing.T) {
	r := Rate{Count: 25, Ticks: 100}
	if r.Per100() != 25 {
		t.Errorf("Per100 = %v", r.Per100())
	}
	if (Rate{Count: 3}).Per100() != 0 {
		t.Errorf("a rate over zero ticks must be 0, not NaN")
	}
}

func TestPoissonCI(t *testing.T) {
	// Exact 95% Poisson limits for a count of 10 are 4.795 and 18.39.
	lo, hi := Rate{Count: 10, Ticks: 100}.CI95()
	if math.Abs(lo-4.8) > 0.2 || math.Abs(hi-18.4) > 0.3 {
		t.Errorf("CI for 10 events = [%.2f, %.2f], want about [4.8, 18.4]", lo, hi)
	}
	// Zero events: lower limit 0, upper 3.69 (the rule of three's cousin).
	lo, hi = Rate{Count: 0, Ticks: 100}.CI95()
	if lo != 0 || math.Abs(hi-3.7) > 0.2 {
		t.Errorf("CI for 0 events = [%.2f, %.2f], want about [0, 3.7]", lo, hi)
	}
	// The interval always contains the point estimate and narrows with more events.
	prev := math.Inf(1)
	for _, c := range []int{5, 20, 80, 320} {
		lo, hi := Rate{Count: c, Ticks: c * 4}.CI95()
		pt := Rate{Count: c, Ticks: c * 4}.Per100()
		if !(lo <= pt && pt <= hi) {
			t.Errorf("count %d: %.2f outside [%.2f, %.2f]", c, pt, lo, hi)
		}
		if w := (hi - lo) / pt; w >= prev {
			t.Errorf("count %d: relative width %.3f did not narrow from %.3f", c, w, prev)
		} else {
			prev = w
		}
	}
}

func TestRatio(t *testing.T) {
	a := Rate{Count: 200, Ticks: 600}
	b := Rate{Count: 100, Ticks: 600}
	ratio, lo, hi, ok := Ratio(a, b)
	if !ok || math.Abs(ratio-2) > 1e-9 || !(lo < 2 && 2 < hi) {
		t.Errorf("ratio %.3f [%.3f, %.3f] ok=%v", ratio, lo, hi, ok)
	}
	if _, _, _, ok := Ratio(Rate{Count: 0, Ticks: 10}, b); ok {
		t.Errorf("a ratio with a zero count is meaningless and must say so")
	}
}

func TestHistogram(t *testing.T) {
	h := Histogram([]int{3, 3, 4, 3, 5})
	if !reflect.DeepEqual(h, map[int]int{3: 3, 4: 1, 5: 1}) {
		t.Errorf("histogram %v", h)
	}
	if got := HistogramString(h); got != "3x3 4x1 5x1" {
		t.Errorf("HistogramString = %q", got)
	}
	if HistogramString(nil) != "none" {
		t.Errorf("empty histogram should print none")
	}
}

func TestMeanSD(t *testing.T) {
	v := []float64{2, 4, 4, 4, 5, 5, 7, 9}
	if Mean(v) != 5 {
		t.Errorf("mean %v", Mean(v))
	}
	if math.Abs(SD(v)-2.13809) > 1e-4 { // sample SD
		t.Errorf("sd %v", SD(v))
	}
	if SD([]float64{1}) != 0 || Mean(nil) != 0 {
		t.Errorf("degenerate inputs")
	}
}

func TestDiffInts(t *testing.T) {
	missed, extra := DiffInts([]int{2, 5, 8, 11}, []int{2, 6, 8, 12, 15})
	if !reflect.DeepEqual(missed, []int{5, 11}) || !reflect.DeepEqual(extra, []int{6, 12, 15}) {
		t.Errorf("missed %v extra %v", missed, extra)
	}
	if m, e := DiffInts(nil, nil); len(m) != 0 || len(e) != 0 {
		t.Errorf("empty diff")
	}
	if !SameInts([]int{1, 2}, []int{1, 2}) || SameInts([]int{1}, []int{1, 2}) {
		t.Errorf("SameInts")
	}
}

func TestParsePack(t *testing.T) {
	m, err := ParsePack("0=a\r\n946=knife\n\n8889=skill_multi3:make1_a\nnoequals\n946=knife_dup\n")
	if err != nil {
		t.Fatal(err)
	}
	if m["knife"] != 946 || m["skill_multi3:make1_a"] != 8889 || m["a"] != 0 {
		t.Errorf("parsed %v", m)
	}
	if _, dup := m["knife_dup"]; !dup {
		t.Errorf("a different name with a repeated id is still a name")
	}
	if _, err := ParsePack("x=knife"); err == nil {
		t.Errorf("a non-numeric id must be an error")
	}
	rev := Reverse(m)
	if rev[946] != "knife" && rev[946] != "knife_dup" {
		t.Errorf("reverse %v", rev)
	}
}

func TestPlanDescribe(t *testing.T) {
	got := Describe(Recommended(NetFishing), 7)
	want := []string{
		"t+0: knife-on-log , make-1-shafts , click",
		"t+2: click",
		"t+3: knife-on-log , make-1-shafts , click",
		"t+5: click",
		"t+6: knife-on-log , make-1-shafts , click",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("describe = %q, want %q", got, want)
	}
}

func TestJitteredPlan(t *testing.T) {
	base := Recommended(FlyFishing)
	// Every batch two ticks late except the first.
	late := Jittered(base, 30, func(tick int) int {
		if tick == 0 {
			return 0
		}
		return 2
	})
	if len(late(0)) == 0 || len(late(3)) != 0 || len(late(5)) == 0 {
		t.Errorf("jitter did not move the batches: %v / %v / %v", late(0), late(3), late(5))
	}
}
