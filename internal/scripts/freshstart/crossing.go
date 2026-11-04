package freshstart

import "rsb0t-go/internal/scripts/tile"

// Which side of the water a tile is on, and what crossing costs.
//
// Why this exists beside FareRemaining and scarabplan.HomeByLand. Both answer "is the ship chain
// still ahead" for the Lumbridge to Ardougne road, and both call anything west of x 3000 one side.
// That is wrong for Falador and Taverley (x 2880-3000): they are Asgarnia, joined to Kandarin only
// by White Wolf Mountain, which a walk avoids at every combat level (webwalk's white-wolf-dens zone),
// so a walk from Ardougne to Falador takes the Port Sarim - Karamja - Brimhaven - Ardougne ships
// exactly as one to Draynor does, and needs their fares. Live 7 Oct 2026: a combat-trainer account
// at Ardougne chose the Falador shield and Barbarian Village helm shops, three failed trips each,
// and a quest account at Draynor with no coins asked for walks to Ardougne's imp field every five
// seconds for twenty minutes.

// Landmass is a stretch of ground joined by land.
type Landmass string

// The landmasses the ship chain joins.
const (
	// LandUnknown is a tile with no known landmass (underground, upstairs, no position): no crossing
	// is assumed for it.
	LandUnknown Landmass = ""
	// LandKandarin is Ardougne, Seers' Village, Catherby, Camelot and everything west of White Wolf
	// Mountain.
	LandKandarin Landmass = "Kandarin"
	// LandMainland is Asgarnia and Misthalin: Taverley, Falador, Port Sarim, Draynor, Varrock,
	// Lumbridge and east.
	LandMainland Landmass = "Asgarnia/Misthalin"
	// LandKaramja is the island between the two ships.
	LandKaramja Landmass = "Karamja"
)

// KandarinMaxX is the first x that is no longer Kandarin: Catherby's shore (x 2840) is Kandarin,
// Taverley (x 2880) is not, and White Wolf Mountain's pass lies between.
const KandarinMaxX = 2850

// undergroundMinZ is where the map's dungeon copy starts; nothing there has a side.
const undergroundMinZ = 6400

// LandmassOf says which landmass t is on.
func LandmassOf(t tile.Tile) Landmass {
	if t.Z >= undergroundMinZ {
		return LandUnknown
	}
	if OnKaramjaSide(t) {
		return LandKaramja
	}
	// The island's southern half (Shilo Village, Tai Bwo Wannai) is below the route box.
	if t.Level == 0 && t.X >= ArdougneSideMaxX && t.X < 3000 && t.Z < Karamja.MinZ && t.Z >= 2800 {
		return LandKaramja
	}
	if t.X < KandarinMaxX {
		return LandKandarin
	}
	return LandMainland
}

// FareToCross is the coins a walk from one tile to the other spends on ships: nothing on one
// landmass or when either end is unknown, one ship (half the default fare) to or from Karamja,
// the whole default fare between Kandarin and the Asgarnia/Misthalin mainland.
func FareToCross(from, to *tile.Tile) int {
	if from == nil || to == nil {
		return 0
	}
	a, b := LandmassOf(*from), LandmassOf(*to)
	switch {
	case a == LandUnknown || b == LandUnknown || a == b:
		return 0
	case a == LandKaramja || b == LandKaramja:
		return (DefaultFare + 1) / 2
	}
	return DefaultFare
}
