package fleetplan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// LineOfSight against the real engine: test/fixtures/lineofsight_vector.json is Engine-TS's own
// LineValidator.hasLineOfSight (the check Player.inApproachDistance makes before a spell fires)
// over a seeded grid of projectile-blocking walls and full blockers, 4,000 random pairs, from
// lineofsight_reference_vector.ts at the repo root.
func TestLineOfSightMatchesTheEngine(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "lineofsight_vector.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Grid  [][3]int `json:"grid"`
		Cases [][5]int `json:"cases"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Cases) < 1000 || len(v.Grid) < 50 {
		t.Fatalf("fixture too small: %d cases over %d blockers", len(v.Cases), len(v.Grid))
	}
	g := walls{}
	for _, c := range v.Grid {
		g[[2]int{c[0], c[1]}] = Sight(c[2])
	}
	seen, hidden, bad := 0, 0, 0
	for _, c := range v.Cases {
		want := c[4] == 1
		got := LineOfSight(tile.Tile{X: c[0], Z: c[1]}, tile.Tile{X: c[2], Z: c[3]}, g.at)
		if want {
			seen++
		} else {
			hidden++
		}
		if got != want {
			bad++
			if bad <= 10 {
				t.Errorf("%d,%d -> %d,%d: engine %v, ours %v", c[0], c[1], c[2], c[3], want, got)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d of %d pairs disagree with the engine", bad, len(v.Cases))
	}
	if seen == 0 || hidden == 0 {
		t.Fatalf("a vector that only ever answers one way proves nothing: %d seen, %d hidden", seen, hidden)
	}
}
