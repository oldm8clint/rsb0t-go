package tickmanip

import (
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
)

func TestRunLoss(t *testing.T) {
	cases := []struct{ grams, want int }{
		{0, 67},       // an unburdened runner
		{-5000, 67},   // negative weight clamps to zero (weight-reducing gear)
		{32000, 100},  // 67 + 67*32/64 = 100.5 -> 100
		{64000, 134},  // 67 + 67 = 134
		{200000, 134}, // clamped at 64 kg
	}
	for _, c := range cases {
		if got := RunLoss(c.grams); got != c.want {
			t.Errorf("RunLoss(%d g) = %d, want %d", c.grams, got, c.want)
		}
	}
}

func TestRecovery(t *testing.T) {
	cases := []struct{ agility, want int }{{1, 8}, {5, 8}, {6, 9}, {70, 19}, {99, 24}}
	for _, c := range cases {
		if got := Recovery(c.agility); got != c.want {
			t.Errorf("Recovery(%d) = %d, want %d", c.agility, got, c.want)
		}
	}
}

func TestApplyTickRules(t *testing.T) {
	// A running tick drains, a standing tick recovers, a frozen tick does neither.
	if got := ApplyTick(5000, EnergyTick{Steps: 2}, 99, 0); got != 5000-67 {
		t.Errorf("running: %d", got)
	}
	if got := ApplyTick(5000, EnergyTick{Steps: 0}, 99, 0); got != 5000+24 {
		t.Errorf("standing: %d", got)
	}
	if got := ApplyTick(5000, EnergyTick{Steps: 1}, 1, 0); got != 5000+8 {
		t.Errorf("one step recovers: %d", got)
	}
	if got := ApplyTick(5000, EnergyTick{Steps: 2, Frozen: true}, 99, 0); got != 5000 {
		t.Errorf("frozen while running must change nothing, got %d", got)
	}
	// Caps.
	if got := ApplyTick(9990, EnergyTick{}, 99, 0); got != MaxEnergy {
		t.Errorf("recovery must cap at 10000, got %d", got)
	}
	if got := ApplyTick(30, EnergyTick{Steps: 2}, 99, 0); got != 0 {
		t.Errorf("drain must floor at 0, got %d", got)
	}
}

// TestFrozenFractionPerVariant: p_delay(n) freezes n+1 ticks of every n+2, and the cost per tile of a
// steady run follows.
func TestFrozenFractionPerVariant(t *testing.T) {
	cases := []struct {
		v          DelayVariant
		period     int
		frozen     int
		perTile    float64 // at weight 0
		saving     float64 // against a plain run
		savingNote string
	}{
		{DelayVariant{Name: "high alchemy", DelayArg: 3}, 5, 4, 67.0 / 5 / 2, 0.80, "4 of 5 ticks frozen"},
		{DelayVariant{Name: "low alchemy", DelayArg: 1}, 3, 2, 67.0 / 3 / 2, 2.0 / 3, "2 of 3"},
		{DelayVariant{Name: "ring of dueling", DelayArg: 1}, 3, 2, 67.0 / 3 / 2, 2.0 / 3, "2 of 3"},
	}
	for _, c := range cases {
		if got := c.v.Period(); got != c.period {
			t.Errorf("%s period %d, want %d", c.v.Name, got, c.period)
		}
		if got := c.v.FrozenTicks(); got != c.frozen {
			t.Errorf("%s frozen %d, want %d", c.v.Name, got, c.frozen)
		}
		if got := c.v.UnitsPerTile(0); math.Abs(got-c.perTile) > 1e-9 {
			t.Errorf("%s units/tile %.3f, want %.3f", c.v.Name, got, c.perTile)
		}
		base := BaselineUnitsPerTile(0)
		if got := 1 - c.v.UnitsPerTile(0)/base; math.Abs(got-c.saving) > 1e-9 {
			t.Errorf("%s saving %.3f, want %.3f (%s)", c.v.Name, got, c.saving, c.savingNote)
		}
	}
	if got := BaselineUnitsPerTile(0); math.Abs(got-33.5) > 1e-9 {
		t.Errorf("a plain run costs %.2f units a tile, want 33.5", got)
	}
}

// TestBuryCannotKeepWalking: bury_bone.rs2 runs p_stopaction before p_delay(0), so the walk queue is
// cleared; the best cycle is three ticks for two tiles and saves almost no energy per tile.
func TestBuryCannotKeepWalking(t *testing.T) {
	bury := DelayVariant{Name: "bury bones", DelayArg: 0, StopsWalk: true}
	perTile, tilesPerTick := PredictStopped(bury, 99, 0)
	if math.Abs(tilesPerTick-2.0/3) > 1e-9 {
		t.Errorf("tiles per tick %.3f, want 2/3", tilesPerTick)
	}
	// (67 - 24) / 2 tiles = 21.5 units a tile at Agility 99 against a plain run's 33.5.
	if math.Abs(perTile-21.5) > 1e-9 {
		t.Errorf("units per tile %.2f, want 21.5", perTile)
	}
	if perTile >= BaselineUnitsPerTile(0) {
		t.Errorf("a stopped walk should still cost less per tile than a plain run")
	}
}

// TestFitStartAcceptsTheTruthAndRejectsAWrongPattern is the property the lab relies on: a per-tick
// percent series is reproduced exactly by SOME start energy when the tick pattern is right, and by
// none when the pattern is wrong.
func TestFitStartAcceptsTheTruthAndRejectsAWrongPattern(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 9))
	for trial := 0; trial < 200; trial++ {
		n := 30 + rng.IntN(60)
		ticks := make([]EnergyTick, n)
		for i := range ticks {
			ticks[i] = EnergyTick{Steps: 2, Frozen: i%5 != 4} // a high-alchemy run
		}
		start := 3000 + rng.IntN(6000)
		agility := 1 + rng.IntN(99)
		weight := rng.IntN(20000)
		obs := Trajectory(start, ticks, agility, weight)
		survivors, first := FitStart(Percent(start), obs, ticks, agility, weight)
		if survivors == 0 {
			t.Fatalf("trial %d: the true pattern was rejected", trial)
		}
		_ = first

		// Shift the frozen pattern by one tick: a wrong hypothesis.
		wrong := make([]EnergyTick, n)
		for i := range wrong {
			wrong[i] = EnergyTick{Steps: 2, Frozen: (i+1)%5 != 4}
		}
		if s, _ := FitStart(Percent(start), obs, wrong, agility, weight); s != 0 && n > 40 {
			// A one-tick shift of a long pattern changes the total by exactly one drain, which is bigger
			// than the 100-unit band's slack for most weights; tolerate only coincidences.
			if s > 3 {
				t.Errorf("trial %d: a shifted pattern still fits from %d starts", trial, s)
			}
		}
	}
}

func TestFitStartLengthMismatch(t *testing.T) {
	if s, _ := FitStart(50, []int{50}, nil, 1, 0); s != 0 {
		t.Errorf("mismatched lengths must fit nothing, got %d", s)
	}
}

func TestFrozenFromCasts(t *testing.T) {
	got := FrozenFromCasts(12, []int{2, 7}, 3)
	want := []bool{false, false, true, true, true, true, false, true, true, true, true, false}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("frozen = %v, want %v", got, want)
	}
	// A cast whose freeze runs past the window is cut off, one before it is ignored.
	if got := FrozenFromCasts(4, []int{-5, 3}, 3); !reflect.DeepEqual(got, []bool{false, false, false, true}) {
		t.Errorf("frozen at the edges = %v", got)
	}
}

func TestStepsOf(t *testing.T) {
	xs := []int{100, 100, 102, 104, 105, 105}
	zs := []int{50, 50, 50, 51, 52, 52}
	// ticks: stood, ran 2, ran 2 (diagonal-ish), walked 1, stood
	if got := StepsOf(xs, zs); !reflect.DeepEqual(got, []int{0, 2, 2, 1, 0}) {
		t.Errorf("StepsOf = %v, want [0 2 2 1 0]", got)
	}
}

// TestFitLossRecoversTheRunLoss: a trajectory produced at one loss is fitted back at that loss. A plain run drains
// every tick, so over 60 ticks it separates losses one unit apart; a High Alchemy rhythm drains one tick in five, so
// its percent series cannot tell neighbouring losses apart (the true one must still fit).
func TestFitLossRecoversTheRunLoss(t *testing.T) {
	const agility = 1
	run := make([]EnergyTick, 60)
	for i := range run {
		run[i] = EnergyTick{Steps: 2}
	}
	var alch []EnergyTick
	for i := 0; i < 40; i++ {
		alch = append(alch, EnergyTick{Steps: 2, Frozen: i%5 != 4})
	}
	series := func(ticks []EnergyTick, start, loss int) []int {
		u := start
		var obs []int
		for _, tk := range ticks {
			u = applyLoss(u, tk, agility, loss)
			obs = append(obs, Percent(u))
		}
		return obs
	}
	for _, loss := range []int{67, 69, 72} {
		fit := FitLoss(99, series(run, 9950, loss), run, agility, []int{66, 67, 68, 69, 70, 71, 72, 73})
		if fit[loss] == 0 {
			t.Errorf("a run at loss %d: FitLoss rules the true loss out: %v", loss, fit)
		}
		for _, other := range []int{loss - 2, loss + 2} {
			if fit[other] != 0 {
				t.Errorf("a run at loss %d: loss %d also fits (%v), a 60-tick run should separate them", loss, other, fit)
			}
		}
		fitA := FitLoss(99, series(alch, 9950, loss), alch, agility, []int{66, 67, 68, 69, 70, 71, 72, 73})
		if fitA[loss] == 0 {
			t.Errorf("an alch rhythm at loss %d: FitLoss rules the true loss out: %v", loss, fitA)
		}
	}
}

func TestUnitsUsedCountsTickKinds(t *testing.T) {
	ticks := []EnergyTick{{Steps: 0}, {Steps: 2}, {Steps: 2, Frozen: true}, {Steps: 1}, {Steps: 2}}
	end, drained, recovered, frozen := UnitsUsed(5000, ticks, 1, 67)
	if drained != 2 || recovered != 2 || frozen != 1 {
		t.Errorf("drained %d recovered %d frozen %d, want 2, 2, 1", drained, recovered, frozen)
	}
	if want := 5000 + 8 - 67 + 8 - 67; end != want {
		t.Errorf("end = %d, want %d", end, want)
	}
}
