package randomevents

import "testing"

func TestInTrapEventSquare(t *testing.T) {
	for _, c := range []struct {
		x, z int
		want bool
	}{
		{2008, 4762, true},             // the Mime stage, where the mule was held on 2026-09-19
		{45*64 + 10, 71*64 + 20, true}, // the maze
		{2809, 3441, false},            // Catherby bank
		{1983, 4762, false},            // just west of the Mime square
	} {
		if got := InTrapEventSquare(c.x, c.z); got != c.want {
			t.Errorf("InTrapEventSquare(%d,%d) = %v, want %v", c.x, c.z, got, c.want)
		}
	}
}
