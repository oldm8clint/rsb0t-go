package webwalk

import "rsb0t-go/internal/randomevents"

// wolfDenMargin widens each White Wolf Mountain spawn's wander-plus-hunt reach so a planned
// route passes with room to spare, the margin the mule's own route home uses.
const wolfDenMargin = 2

// The White Wolf Mountain dens as an automatic zone, for every walk at every combat level.
//
// Owner, 2026-09-19: "there should always be a way to avoid going over white wolf mountain,
// especially on low level accounts". The catalog's "white-wolf-mountain" rectangle is not
// automatic and covers only the pass's northern half (z 3468-3538), so a default walk from
// the Taverley or Falador side went straight over it: the mule died there repeatedly, a
// combat-3 Scarab account on the way to Zaff died there, and fishers were still routed that
// way. The mule (internal/bot/mulehome.go) and Stage 3 (scarabrunner.WalkTo) each passed the
// dens as AvoidRects; this makes it the default for every script. The rectangles come from
// the content spawn list (randomevents.WolfTerritorySquares), not a hand-drawn box. There is
// no wolf-free land route between the Taverley side and Catherby, so such walks take the
// ship chain (Port Sarim, Karamja, Brimhaven, Ardougne) and need its fares. A walk that
// starts or ends inside a den may still leave or reach it.
func init() {
	squares := randomevents.WolfTerritorySquares(wolfDenMargin)
	rects := make([]Rect, 0, len(squares))
	for _, s := range squares {
		rects = append(rects, Rect{MinX: s.MinX, MaxX: s.MaxX, MinZ: s.MinZ, MaxZ: s.MaxZ, Level: 0})
	}
	KnownDangerZones = append(KnownDangerZones, DangerZone{
		ID:    "white-wolf-dens",
		Label: "White Wolf Mountain dens",
		Help: "Every White Wolf Mountain wolf spawn (content whitewolf_sentry and white wolves), each widened by its " +
			"wander and hunt range plus 2 tiles. Automatic at every combat level: routes between the Taverley side " +
			"and Catherby take the ships instead. Not in the TS catalog.",
		Automatic:               true,
		AllowWhenEndpointInside: true,
		Rects:                   rects,
	})
}
