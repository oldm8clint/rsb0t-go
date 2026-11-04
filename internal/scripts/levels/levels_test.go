package levels

import (
	"math"
	"testing"
)

// Translated from rsb0t-scripts test/levels.test.ts.

func TestXPAtLevelMatchesTheClassicTable(t *testing.T) {
	want := map[int]int{1: 0, 2: 83, 10: 1154, 50: 101333, 99: 13034431}
	for level, xp := range want {
		if got := XPAtLevel(level); got != xp {
			t.Errorf("XPAtLevel(%d) = %d, want %d", level, got, xp)
		}
	}
}

func TestXPAtLevelClampsOffEitherEnd(t *testing.T) {
	if XPAtLevel(0) != XPAtLevel(1) || XPAtLevel(-5) != XPAtLevel(1) || XPAtLevel(120) != XPAtLevel(99) {
		t.Error("out-of-range levels did not clamp")
	}
}

func TestLevelFromXPIsXPAtLevelsInverse(t *testing.T) {
	for level := 1; level <= MaxLevel; level++ {
		if got := LevelFromXP(XPAtLevel(level)); got != level {
			t.Errorf("LevelFromXP(XPAtLevel(%d)) = %d, want %d", level, got, level)
		}
	}
}

func TestLevelFromXPJustBelowAThresholdIsThePreviousLevel(t *testing.T) {
	if got := LevelFromXP(XPAtLevel(50) - 1); got != 49 {
		t.Errorf("LevelFromXP(XPAtLevel(50)-1) = %d, want 49", got)
	}
}

func TestLevelFromXPClampsAtBothEnds(t *testing.T) {
	if got := LevelFromXP(0); got != 1 {
		t.Errorf("LevelFromXP(0) = %d, want 1", got)
	}
	if got := LevelFromXP(XPAtLevel(99) + 1_000_000); got != 99 {
		t.Errorf("LevelFromXP(way above 99) = %d, want 99", got)
	}
}

func TestLevelProgress(t *testing.T) {
	p := LevelProgress(2, 100)
	if p.Level != 2 || p.Remaining != 174-100 || math.Abs(p.Fraction-17.0/91.0) > 1e-9 {
		t.Errorf("LevelProgress(2, 100) = %+v", p)
	}
	if got := LevelProgress(99, 20_000_000); got != (Progress{Level: 99, Fraction: 1, Remaining: 0}) {
		t.Errorf("LevelProgress(99, 20M) = %+v", got)
	}
	if got := LevelProgress(10, 0).Fraction; got != 0 {
		t.Errorf("experience below the level read as progress %v", got)
	}
}

func TestETAHours(t *testing.T) {
	if h, ok := ETAHours(1000, 500); !ok || h != 2 {
		t.Errorf("ETAHours(1000, 500) = %v, %v", h, ok)
	}
	if _, ok := ETAHours(0, 500); ok {
		t.Error("a shut gap has an eta")
	}
	if _, ok := ETAHours(1000, 0); ok {
		t.Error("a zero rate has an eta")
	}
}
