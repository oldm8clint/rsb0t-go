package randomevents

// WolfSquare is one White Wolf Mountain spawn's reach as an inclusive surface (level 0)
// tile square.
type WolfSquare struct {
	MinX, MaxX, MinZ, MaxZ int
}

// WolfTerritorySquares is wolfDens (see wolf_territory.go) as tile squares, each widened by
// margin tiles, so a walk planner can keep a whole route out of reach of the mountain's
// cowardly hunters rather than only keeping a flee landing off it. The data stays in one
// place: this reads wolfDens and copies nothing.
//
// Why a route planner needs it (live 2026-09-19, the mule `mule`, combat 3 and 10 hp): the
// only land route from Taverley or Falador to Catherby is the pass, and the named webwalk
// zone "white-wolf-mountain" covers only its northern half (x 2828-2878, z 3468-3538). The
// mule was attacked again and again on the eastern foothills, around the whitewolf_sentry
// spawns at 2870,3438 and 2872,3445, which that rectangle does not reach.
func WolfTerritorySquares(margin int) []WolfSquare {
	if margin < 0 {
		margin = 0
	}
	out := make([]WolfSquare, 0, len(wolfDens))
	for _, d := range wolfDens {
		r := d.r + margin
		out = append(out, WolfSquare{MinX: d.x - r, MaxX: d.x + r, MinZ: d.z - r, MaxZ: d.z + r})
	}
	return out
}
