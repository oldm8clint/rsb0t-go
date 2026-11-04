package deathreport

import "fmt"

// A place name for a death tile, for a reader of the log: the places the fleet's accounts actually die in, read off the
// content's maps. Best effort and first match wins, so a small rectangle goes before the town that holds it. A tile no
// rectangle names is reported as its mapsquare (the content's own m<x>_<z> name), which is still a place a map can find.

type area struct {
	name                   string
	minX, maxX, minZ, maxZ int
	level                  int // -1: any level
}

var areas = []area{
	// Pens and hunting grounds the Stage 5 magic half and the trainers use.
	{"Yanille west ogres", 2495, 2525, 3085, 3130, 0},
	{"Feldip ogres (west)", 2490, 2550, 2940, 2995, 0},
	{"Feldip ogres (east)", 2551, 2625, 2940, 3010, 0},
	{"North Ardougne moss giants", 2540, 2600, 3370, 3420, 0},
	{"Khazard battlefield", 2500, 2575, 3200, 3262, 0},
	{"Fight Arena", 2580, 2625, 3140, 3175, -1},
	{"Gnome Stronghold course", 2465, 2495, 3410, 3445, -1},
	{"Wizards' Tower", 3100, 3125, 3150, 3180, -1},
	// Underground, before the towns they sit under.
	{"Mage Bank", 2495, 2565, 4690, 4745, -1},
	{"Kalphite lair", 3190, 3530, 9470, 9560, -1},
	{"Varrock sewers", 3150, 3290, 9850, 9930, -1},
	{"Edgeville dungeon", 3085, 3145, 9800, 9995, -1},
	{"Wilderness (underground)", 2944, 3400, 10000, 10400, -1},
	// Towns.
	{"East Ardougne", 2590, 2695, 3260, 3345, -1},
	{"West Ardougne", 2500, 2589, 3262, 3345, -1},
	{"Yanille", 2560, 2645, 3050, 3125, -1},
	{"Port Khazard", 2630, 2680, 3135, 3180, -1},
	{"Seers' Village", 2680, 2745, 3450, 3515, -1},
	{"Catherby", 2780, 2860, 3410, 3480, -1},
	{"Taverley", 2880, 2945, 3410, 3480, -1},
	{"Burthorpe", 2880, 2945, 3480, 3580, -1},
	{"Falador", 2940, 3070, 3300, 3400, -1},
	{"Port Sarim", 2990, 3060, 3170, 3260, -1},
	{"Draynor", 3070, 3115, 3225, 3295, -1},
	{"Lumbridge", 3190, 3270, 3190, 3262, -1},
	{"Al Kharid", 3270, 3345, 3140, 3260, -1},
	{"Varrock", 3130, 3290, 3380, 3520, -1},
	{"Edgeville", 3070, 3112, 3455, 3520, -1},
	{"Karamja", 2800, 2960, 3120, 3205, -1},
}

// Area names the place of a tile: a named area, the Wilderness with its level, or the mapsquare.
func Area(x, z, level int) string {
	for _, a := range areas {
		if (a.level < 0 || a.level == level) && x >= a.minX && x <= a.maxX && z >= a.minZ && z <= a.maxZ {
			return a.name
		}
	}
	if lv := wildernessLevel(x, z); lv > 0 && level == 0 {
		return fmt.Sprintf("Wilderness level %d", lv)
	}
	return fmt.Sprintf("m%d_%d", x>>6, z>>6)
}

// wildernessLevel is the surface Wilderness level of a tile, 0 outside it: the band north of z 3520 between x 2944 and
// 3392 (content's wilderness_level), one level per eight rows.
func wildernessLevel(x, z int) int {
	if x < 2944 || x > 3391 || z < 3520 || z > 3967 {
		return 0
	}
	return (z-3520)/8 + 1
}
