package combatplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestRungAtFindsTheGroundOfTheRungsAnAccountDiedIn(t *testing.T) {
	for _, c := range []struct {
		name string
		at   tile.Tile
		want string // the rung key, "" for none
	}{
		{"the North Ardougne Moss giants (14 Oct 11:15, 2554,3406)", tile.Tile{X: 2554, Z: 3406}, "mossgiants-ardougne"},
		{"a tile of its stand", tile.Tile{X: 2559, Z: 3405}, "mossgiants-ardougne"},
		{"the East Ardougne south bank", tile.Tile{X: 2655, Z: 3286}, ""},
		{"the East Ardougne market", tile.Tile{X: 2662, Z: 3305}, ""},
		{"an upper floor over a rung", tile.Tile{X: 2559, Z: 3405, Level: 1}, ""},
	} {
		got, ok := RungAt(c.at)
		switch {
		case c.want == "" && ok:
			t.Errorf("%s: %v is on the ground of rung %s", c.name, c.at, got.Key)
		case c.want != "" && (!ok || got.Key != c.want):
			t.Errorf("%s: RungAt(%v) = %q ok %v, want %s", c.name, c.at, got.Key, ok, c.want)
		}
	}
}

// Every rung's own stand and spawns are on its ground.
func TestEveryRungStandsOnItsGround(t *testing.T) {
	for _, r := range SpreadLadderWith(SpreadOptions{Wilderness: true}) {
		for _, p := range append([]tile.Tile{r.Stand}, r.Spawns...) {
			if p.Level != 0 {
				continue
			}
			if got, ok := RungAt(p); !ok {
				t.Errorf("rung %s: %v (its stand or a spawn) is on no rung's ground", r.Key, p)
			} else if got.Key != r.Key {
				t.Logf("rung %s: %v is also the ground of rung %s (rungs overlap)", r.Key, p, got.Key)
			}
		}
		// A rung whose bank is on its own ground (the Al-Kharid warriors patrol the bank) has no better place to wait than
		// where it stands: the caller must see that and not walk (scarabswarm_waitaside.go).
		if (r.Bank != tile.Tile{}) {
			if got, ok := RungAt(r.Bank); ok {
				t.Logf("rung %s: its bank %v is on the ground of rung %s", r.Key, r.Bank, got.Key)
			}
		}
	}
}
