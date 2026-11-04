package randomevents

import (
	"testing"

	"rsb0t-go/internal/gamestate"
)

func TestChebyshev(t *testing.T) {
	cases := []struct {
		name   string
		ax, az int
		bx, bz int
		want   int
	}{
		{"same tile", 10, 20, 10, 20, 0},
		{"horizontal", 10, 20, 14, 20, 4},
		{"vertical", 10, 20, 10, 15, 5},
		{"diagonal", 10, 20, 14, 23, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := chebyshev(tc.ax, tc.az, tc.bx, tc.bz); got != tc.want {
				t.Fatalf("chebyshev() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestBuildHostileIDs(t *testing.T) {
	ids := buildHostileIDs()
	for _, id := range []int{391, 396, 411, 413, 418, 419, 424, 425, 430, 431, 436, 438, 443} {
		if !ids[id] {
			t.Errorf("hostile NPC id %d missing", id)
		}
	}
	for _, id := range []int{390, 397, 410, 412, 437, 444} {
		if ids[id] {
			t.Errorf("unexpected hostile NPC id %d", id)
		}
	}
}

func TestDetectEmptyState(t *testing.T) {
	if got := Detect(&gamestate.State{}); got != nil {
		t.Fatalf("Detect(empty state) = %+v, want nil", got)
	}
}
