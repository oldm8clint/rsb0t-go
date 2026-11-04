package tile

import "testing"

func TestChebyshev(t *testing.T) {
	cases := []struct {
		a, b Tile
		want int
	}{
		{Tile{X: 0, Z: 0}, Tile{X: 3, Z: 1}, 3},
		{Tile{X: 5, Z: 5}, Tile{X: 4, Z: 9}, 4},
		{Tile{X: 2, Z: 2, Level: 0}, Tile{X: 2, Z: 2, Level: 1}, 0},
	}
	for _, c := range cases {
		if got := Chebyshev(c.a, c.b); got != c.want {
			t.Errorf("Chebyshev(%v, %v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
