package woodcutrunner

import (
	"reflect"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// TestFreeFirstOrdersOccupiedTilesLastAndDropsNone: an occupied stand tile goes after every free
// one, and is still there. Dropping it is what excluded the very trees a crowd was working — see
// freeFirst's doc comment for the lab and live measurements.
func TestFreeFirstOrdersOccupiedTilesLastAndDropsNone(t *testing.T) {
	a, b, c, dd := tile.Tile{X: 1}, tile.Tile{X: 2}, tile.Tile{X: 3}, tile.Tile{X: 4}
	busy := map[tile.Tile]string{a: "Acct1", c: ""}
	occupied := func(t tile.Tile) (bool, string) { who, ok := busy[t]; return ok, who }

	got, by := freeFirst([]tile.Tile{a, b, c, dd}, occupied)
	if want := []tile.Tile{b, dd, a, c}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v (free nearest-first, then occupied nearest-first)", got, want)
	}
	if want := []string{"", "", "Acct1", "another player"}; !reflect.DeepEqual(by, want) {
		t.Errorf("occupiedBy = %q, want %q", by, want)
	}

	// THE LIVE SHAPE: a willow with ONE stand tile and somebody on it. The old code had nothing
	// left to walk at and excluded the willow; now the one tile is still there to walk onto.
	only := []tile.Tile{{X: 2711, Z: 3510}}
	got, by = freeFirst(only, func(tile.Tile) (bool, string) { return true, "Wctocc0322a" })
	if len(got) != 1 || got[0] != only[0] || by[0] != "Wctocc0322a" {
		t.Errorf("a lone occupied tile must survive: got %v %q", got, by)
	}

	if got, by := freeFirst(nil, occupied); len(got) != 0 || len(by) != 0 {
		t.Errorf("nothing in, nothing out: %v %v", got, by)
	}
}

// TestNoRouteToAnyIsOnlyTheWrongSideOfAWall: the cut-off check runs only on the approach shape
// that can mean "the account is on the wrong side of a wall" — every legal stand tile unroutable
// and nothing walked at. A tree hemmed in by its neighbours (no legal tile at all) or a walk that
// stalled is an ordinary unreachable tree and must still be excluded.
func TestNoRouteToAnyIsOnlyTheWrongSideOfAWall(t *testing.T) {
	for _, c := range []struct {
		name string
		a    approach
		want bool
	}{
		{"every legal tile unroutable, nothing walked", approach{Legal: 2, Unroutable: 2}, true},
		{"reached", approach{Reached: true, Legal: 2, Unroutable: 2}, false},
		{"one tile routable, its walk stalled", approach{Legal: 2, Unroutable: 1, Tried: 1}, false},
		{"hemmed in: no legal tile at all", approach{}, false},
		{"some unroutable, the rest never reached the cap", approach{Legal: 3, Unroutable: 2}, false},
	} {
		if got := c.a.noRouteToAny(); got != c.want {
			t.Errorf("%s: noRouteToAny = %v, want %v", c.name, got, c.want)
		}
	}
}
