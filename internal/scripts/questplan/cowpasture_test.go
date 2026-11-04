package questplan

import (
	"testing"
	"time"
)

// The pen's constants hold together: the stand is inside the fence by more than a walk into it may stop short, the spawns and the
// beat are within the pen's box, and the gate's two leaves are on the fence line.
func TestCowPastureConstantsHoldTogether(t *testing.T) {
	if len(CowPastureGates) != 2 || CowPastureGates[0].X != 3253 || CowPastureGates[1].X != 3253 || CowPastureGates[1].Z != CowPastureGates[0].Z+1 {
		t.Errorf("the gate %v is not the fence's double gate", CowPastureGates)
	}
	// The fence is at x 3252|3253: every tile a walk within the radius of the stand may stop on is east of it.
	if CowPastureStand.X-CowPastureEnterRadius <= CowPastureGates[0].X {
		t.Errorf("a walk within %d of the stand %v may stop at x %d, on the fence line %d or outside", CowPastureEnterRadius, CowPastureStand,
			CowPastureStand.X-CowPastureEnterRadius, CowPastureGates[0].X)
	}
	inBox := func(x, z int) bool { return x >= 3242 && x <= 3265 && z >= 3255 && z <= 3298 }
	if len(CowPastureSpawns) != 10 {
		t.Errorf("%d cow spawns, the content maps have 10 in the pen", len(CowPastureSpawns))
	}
	for _, sp := range CowPastureSpawns {
		if !inBox(sp.X, sp.Z) {
			t.Errorf("spawn %v is outside the pen's box", sp)
		}
	}
	for _, stop := range CowPastureSweep {
		if !inBox(stop.X, stop.Z) {
			t.Errorf("stop %v of the beat is outside the pen's box", stop)
		}
	}
	if !inBox(CowPastureStand.X, CowPastureStand.Z) || !inBox(DemonCowField.X, DemonCowField.Z) {
		t.Error("the stand or the Demon Slayer field is outside the pen's box")
	}
	// The beat covers the spawns: every spawn is within the client's 15 tiles of a stop (or of the stand).
	stops := append([]struct{ x, z int }{{CowPastureStand.X, CowPastureStand.Z}}, func() []struct{ x, z int } {
		var out []struct{ x, z int }
		for _, s := range CowPastureSweep {
			out = append(out, struct{ x, z int }{s.X, s.Z})
		}
		return out
	}()...)
	for _, sp := range CowPastureSpawns {
		seen := false
		for _, s := range stops {
			if max(abs(s.x-sp.X), abs(s.z-sp.Z)) <= 15 {
				seen = true
			}
		}
		if !seen {
			t.Errorf("the spawn %v is never in view from the stand or the beat", sp)
		}
	}
}

// The yield model's numbers, as the plan states them: ten spawns of a 79 second cycle give about 7.6 cows a minute to everyone, an
// account alone kills 2.4 a minute, and four accounts sharing the pen each 1.9.
func TestCowYieldModel(t *testing.T) {
	near := func(got, want float64) bool { return got > want-0.02 && got < want+0.02 }
	if got := CowSupplyPerMinute(); !near(got, 7.59) {
		t.Errorf("supply %.2f a minute, want 7.59", got)
	}
	for farmers, want := range map[int]float64{0: 2.4, 1: 2.4, 3: 2.4, 4: 1.90, 6: 1.27, 18: 0.42} {
		if got := CowRatePerMinute(farmers); !near(got, want) {
			t.Errorf("%d farmers: %.2f cows a minute each, want %.2f", farmers, got, want)
		}
	}
	// More accounts never give one of them more, and never more than the pen holds in all.
	prev := CowRatePerMinute(1)
	for n := 2; n <= 40; n++ {
		r := CowRatePerMinute(n)
		if r > prev+1e-9 {
			t.Errorf("%d farmers each kill %.2f a minute, more than %d did (%.2f)", n, r, n-1, prev)
		}
		if r*float64(n) > CowSupplyPerMinute()+1e-9 {
			t.Errorf("%d farmers kill %.2f a minute together, over the supply %.2f", n, r*float64(n), CowSupplyPerMinute())
		}
		prev = r
	}
	if CowFarmEstimate(0, 1) != 0 || CowExpectedKills(0, 1) != 0 {
		t.Error("nothing to kill takes time")
	}
}

// Demon Slayer's round is given the time its 25 bones take among the accounts it is planned for with the margin on it: more than the
// solo time, at least the floor, and (the figure the log's evidence needs) 18 minutes.
func TestDemonBonesWindow(t *testing.T) {
	w := DemonBonesWindow()
	solo := CowFarmEstimate(DemonBonesNeeded, 1)
	planned := CowFarmEstimate(DemonBonesNeeded, DemonCowFarmers)
	if w <= planned || w <= solo {
		t.Errorf("window %s is not over the planned %s and the solo %s time", w, planned, solo)
	}
	if w < CowWindowFloor || w%time.Minute != 0 {
		t.Errorf("window %s: not whole minutes of at least %s", w, CowWindowFloor)
	}
	if want := 18 * time.Minute; w != want {
		t.Errorf("window %s, the plan's arithmetic (25 bones among 4 accounts at 79 s a cow, a third over) is %s", w, want)
	}
	// The ten rounds of 14 minutes in the 18 October log were the old flat window: solo, 25 bones take 10 minutes 25 seconds in it.
	if solo > 14*time.Minute {
		t.Errorf("solo, 25 bones take %s, over the old 14 minute window", solo)
	}
	// A bigger crowd is a longer window, and an empty ask gets the floor.
	if CowFarmWindow(25, 10) <= CowFarmWindow(25, 4) {
		t.Error("ten accounts in the pen get no more time than four")
	}
	if CowFarmWindow(0, 1) != CowWindowFloor {
		t.Errorf("no kills: %s, want the floor", CowFarmWindow(0, 1))
	}
}

// A Prayer rung's gap in stints: Prayer 43 from the 18 October log's accounts (11,000 to 22,000 xp short) is a hundred and more stints
// of twenty minutes alone in the pen, which is why the cows are a fallback and not the way there; Prayer 37 with 3,290 to go is thirty.
func TestCowStints(t *testing.T) {
	window := 20 * time.Minute
	if n := CowStints(11191, window, 1); n < 40 || n > 100 {
		t.Errorf("11191 xp alone in the pen: %d stints", n)
	}
	if a, b := CowStints(22811, window, 1), CowStints(11191, window, 1); a <= b {
		t.Errorf("more xp to go is no more stints: %d against %d", a, b)
	}
	if a, b := CowStints(5000, window, 12), CowStints(5000, window, 1); a <= b {
		t.Errorf("twelve accounts sharing the pen need no more stints than one: %d against %d", a, b)
	}
	if CowStints(0, window, 1) != 0 || CowStints(-5, window, 1) != 0 {
		t.Error("no xp to go still takes stints")
	}
	if n := CowStints(5, window, 1); n != 1 {
		t.Errorf("5 xp: %d stints, want 1", n)
	}
}
