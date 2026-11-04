package webwalk

// This file is data/dangerZones.ts: tile rectangles a route must not enter, curated by id,
// with conditions on the player's combat level and on the route's endpoints.

// AllLevels is a Rect.Level that applies the rectangle on every level (caves and surface).
const AllLevels = -1

// Rect is an inclusive tile rectangle. Level is the one level it applies on, or AllLevels.
type Rect struct {
	MinX, MaxX, MinZ, MaxZ int
	Level                  int
}

// Contains reports whether p is inside the rectangle.
func (r Rect) Contains(p Point) bool {
	if r.Level != AllLevels && r.Level != p.Level {
		return false
	}
	return p.X >= r.MinX && p.X <= r.MaxX && p.Z >= r.MinZ && p.Z <= r.MaxZ
}

// InZones is dangerZones.ts's tileInDangerZones.
func InZones(p Point, zones []Rect) bool {
	for _, r := range zones {
		if r.Contains(p) {
			return true
		}
	}
	return false
}

// DangerZone is one curated no-go area.
type DangerZone struct {
	ID, Label, Help string
	Rects           []Rect
	// Automatic zones apply to every walk that does not opt out, without being named.
	Automatic bool
	// AvoidAtOrBelowCombat, when above 0, applies the zone only while the player's combat
	// level is at or below it. An unknown combat level keeps the zone (fail safe).
	AvoidAtOrBelowCombat int
	// AllowWhenEndpointInside skips the zone when the walk starts or ends inside it, so a
	// player can leave it or walk to a destination in it on purpose.
	AllowWhenEndpointInside bool
	// DeeperThanEndpoint, when above 0, narrows AllowWhenEndpointInside: a rectangle holding an
	// endpoint still avoids every row more than this many tiles north of the deepest endpoint in
	// it, so a walk to a shallow destination cannot be planned through deeper land (a lever or
	// portal that lands far north). Rectangles holding no endpoint are kept whole.
	DeeperThanEndpoint int
	// FromTS is false for zones this port added that the TS catalog does not have.
	FromTS bool
}

// KnownDangerZones is KNOWN_DANGER_ZONES, plus the Go additions marked FromTS false.
var KnownDangerZones = []DangerZone{
	{
		ID:     "white-wolf-mountain",
		Label:  "White Wolf Mountain",
		Help:   "Wolf-heavy pass between Taverley and Catherby. Low combat accounts should avoid and take the long coastal route or a ship instead.",
		Rects:  []Rect{{MinX: 2828, MaxX: 2878, MinZ: 3468, MaxZ: 3538, Level: AllLevels}},
		FromTS: true,
	},
	{
		ID:    "draynor-jail-guards",
		Label: "Draynor jail guards",
		Help: "Four level-26 jail guards aggressively hunt players around the jail compound. Avoid as transit at every combat level, but permit destinations inside. " +
			"The TS catalog stopped at combat 50 (twice the guards' level), which is the rule for a cowardly hunter; content gives the guard huntmode aggressive_melee, " +
			"and that mode sets check_nottoostrong=off (scripts/_unpack/225/all.hunt), so it hunts a combat-63 account as readily as a combat-3 one " +
			"(live 17 Oct 2026: a combat-63 account standing at 3107,3234 was killed by one, HP 55 to 0).",
		Automatic:               true,
		AllowWhenEndpointInside: true,
		Rects: []Rect{
			{MinX: 3096, MaxX: 3122, MinZ: 3224, MaxZ: 3250, Level: 0},
			{MinX: 3107, MaxX: 3133, MinZ: 3225, MaxZ: 3251, Level: 0},
			{MinX: 3108, MaxX: 3134, MinZ: 3236, MaxZ: 3262, Level: 0},
			{MinX: 3114, MaxX: 3140, MinZ: 3235, MaxZ: 3261, Level: 0},
		},
		FromTS: true,
	},
	{
		ID:    "ardougne-seers-bears",
		Label: "Bears on the Ardougne to Seers road",
		Help: "Nine level-21 brown bears (content m42_52, npc brownbear: huntmode cowardly, huntrange 1, wanderrange 4, size 2) " +
			"around the road north of East Ardougne. Cowardly hunting only picks players it is not too weak for, " +
			"taken here as combat 42 and below. Not in the TS catalog: a fresh account was killed walking this road, " +
			"and a live rerun was attacked by one at 2719,3337.",
		Automatic:               true,
		AvoidAtOrBelowCombat:    42,
		AllowWhenEndpointInside: true,
		// The spawns span 2691..2718, 3330..3344; padded by wander range, hunt range and size.
		Rects: []Rect{{MinX: 2684, MaxX: 2725, MinZ: 3323, MaxZ: 3351, Level: 0}},
	},
	{
		ID:    "seers-south-bats",
		Label: "Giant bats south of Seers' Village",
		Help: "Three level-27 giant bats (content m43_53, npc bat: huntmode cowardly, huntrange 2, wanderrange 8, size 2) " +
			"on the road south of Seers' Village, taken as hunting combat 54 and below. Not in the TS catalog: a live " +
			"walk from Ardougne to Catherby was attacked by one at 2751,3398.",
		Automatic:               true,
		AvoidAtOrBelowCombat:    54,
		AllowWhenEndpointInside: true,
		// The spawns span 2752..2759, 3396..3409; padded by wander range, hunt range and size.
		Rects: []Rect{{MinX: 2740, MaxX: 2771, MinZ: 3384, MaxZ: 3421, Level: 0}},
	},
	{
		ID:                      "wilderness",
		Label:                   "Wilderness",
		Help:                    "Everything north of the Wilderness ditch and the Wilderness dungeons under it. Not in the TS catalog: it keeps lever and portal hops from routing an account through PvP land. Walks that start or end inside still go, but never deeper than their deepest endpoint (7 Oct 2026: a combat-28 account bound for the chaos altar at 3239,3607 was routed through the Ardougne lever to 3154,3924 and killed).",
		Automatic:               true,
		AllowWhenEndpointInside: true,
		DeeperThanEndpoint:      16,
		Rects: []Rect{
			{MinX: 2944, MaxX: 3391, MinZ: 3523, MaxZ: 3967, Level: AllLevels},
			{MinX: 2944, MaxX: 3391, MinZ: 9923, MaxZ: 10367, Level: AllLevels},
		},
	},
}

// KnownDangerZoneByID returns a catalog zone.
func KnownDangerZoneByID(id string) (DangerZone, bool) {
	for _, z := range KnownDangerZones {
		if z.ID == id {
			return z, true
		}
	}
	return DangerZone{}, false
}

// ZoneContext is DangerZoneResolveContext.
type ZoneContext struct {
	// IncludeAutomatic adds every Automatic catalog zone.
	IncludeAutomatic bool
	// CombatLevel is the player's combat level when CombatKnown.
	CombatLevel int
	CombatKnown bool
	// Hurt is an account that is hurt with nothing to eat: a zone that applies only up to a combat level (AvoidAtOrBelowCombat)
	// applies at every level, since the levels assume a healthy account that can eat or fight on. Zones without a level condition are
	// unchanged.
	Hurt bool
	// Start and Dest are the walk's endpoints, for AllowWhenEndpointInside; nil skips the check.
	Start, Dest *Point
}

// ResolveDangerZones is resolveDangerZones: catalog ids (unknown ones are skipped, each id
// once) plus ad-hoc rectangles, as one flat list.
func ResolveDangerZones(ids []string, rects []Rect, ctx ZoneContext) []Rect {
	requested := append([]string(nil), ids...)
	if ctx.IncludeAutomatic {
		for _, z := range KnownDangerZones {
			if z.Automatic {
				requested = append(requested, z.ID)
			}
		}
	}
	var out []Rect
	seen := map[string]bool{}
	for _, id := range requested {
		if seen[id] {
			continue
		}
		seen[id] = true
		z, ok := KnownDangerZoneByID(id)
		if !ok {
			continue
		}
		if z.AvoidAtOrBelowCombat > 0 && !ctx.Hurt && ctx.CombatKnown && ctx.CombatLevel > z.AvoidAtOrBelowCombat {
			continue
		}
		if z.AllowWhenEndpointInside && ((ctx.Start != nil && InZones(*ctx.Start, z.Rects)) || (ctx.Dest != nil && InZones(*ctx.Dest, z.Rects))) {
			if z.DeeperThanEndpoint > 0 {
				out = append(out, deeperThanEndpoints(z.Rects, z.DeeperThanEndpoint, ctx.Start, ctx.Dest)...)
			}
			continue
		}
		out = append(out, z.Rects...)
	}
	return append(out, rects...)
}

// deeperThanEndpoints is DangerZone.DeeperThanEndpoint: each rectangle cut to the rows more than
// margin tiles north of the deepest endpoint inside it, or kept whole when no endpoint is inside.
func deeperThanEndpoints(rects []Rect, margin int, ends ...*Point) []Rect {
	var out []Rect
	for _, r := range rects {
		deepest, inside := 0, false
		for _, p := range ends {
			if p != nil && r.Contains(*p) && (!inside || p.Z > deepest) {
				deepest, inside = p.Z, true
			}
		}
		if !inside {
			out = append(out, r)
			continue
		}
		if from := deepest + margin + 1; from <= r.MaxZ {
			r.MinZ = from
			out = append(out, r)
		}
	}
	return out
}
