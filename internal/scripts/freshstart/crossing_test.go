package freshstart

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestLandmassOfNamedPlaces(t *testing.T) {
	cases := []struct {
		name string
		at   tile.Tile
		want Landmass
	}{
		{"East Ardougne bank", tile.Tile{X: 2655, Z: 3283}, LandKandarin},
		{"the Ardougne imp field", tile.Tile{X: 2630, Z: 3220}, LandKandarin},
		{"Port Khazard", tile.Tile{X: 2654, Z: 3152}, LandKandarin},
		{"Seers Village", tile.Tile{X: 2725, Z: 3490}, LandKandarin},
		{"Catherby shore", tile.Tile{X: 2840, Z: 3432}, LandKandarin},
		{"Taverley", tile.Tile{X: 2880, Z: 3450}, LandMainland},
		{"Falador, Cassie's shield shop", tile.Tile{X: 2979, Z: 3383}, LandMainland},
		{"Barbarian Village, Peksa", tile.Tile{X: 3076, Z: 3429}, LandMainland},
		{"Draynor bank", tile.Tile{X: 3092, Z: 3245}, LandMainland},
		{"Port Sarim", tile.Tile{X: 3030, Z: 3220}, LandMainland},
		{"Rimmington", tile.Tile{X: 2957, Z: 3214}, LandMainland},
		{"Varrock", tile.Tile{X: 3253, Z: 3419}, LandMainland},
		{"Brimhaven", tile.Tile{X: 2772, Z: 3234}, LandKaramja},
		{"Musa Point", tile.Tile{X: 2925, Z: 3170}, LandKaramja},
		{"Shilo Village", tile.Tile{X: 2850, Z: 2960}, LandKaramja},
		{"a dungeon", tile.Tile{X: 3097, Z: 9868}, LandUnknown},
	}
	for _, c := range cases {
		if got := LandmassOf(c.at); got != c.want {
			t.Errorf("%s (%d,%d): %q, want %q", c.name, c.at.X, c.at.Z, got, c.want)
		}
	}
}

func TestFareToCross(t *testing.T) {
	ardougne := tile.Tile{X: 2655, Z: 3283}
	seers := tile.Tile{X: 2725, Z: 3490}
	falador := tile.Tile{X: 2979, Z: 3383}
	draynor := tile.Tile{X: 3092, Z: 3245}
	brimhaven := tile.Tile{X: 2772, Z: 3234}
	dungeon := tile.Tile{X: 3097, Z: 9868}
	cases := []struct {
		name     string
		from, to *tile.Tile
		want     int
	}{
		{"Ardougne to Seers is one landmass", &ardougne, &seers, 0},
		{"Ardougne to Falador crosses the water (the wolves keep the land route shut)", &ardougne, &falador, DefaultFare},
		{"Draynor to Ardougne", &draynor, &ardougne, DefaultFare},
		{"Falador to Draynor is one landmass", &falador, &draynor, 0},
		{"Brimhaven to Ardougne is one ship", &brimhaven, &ardougne, DefaultFare / 2},
		{"Draynor to Brimhaven is one ship", &draynor, &brimhaven, DefaultFare / 2},
		{"a dungeon decides nothing", &dungeon, &ardougne, 0},
		{"no position decides nothing", nil, &ardougne, 0},
		{"no destination decides nothing", &ardougne, nil, 0},
	}
	for _, c := range cases {
		if got := FareToCross(c.from, c.to); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}
