// Command webwalkplan is the offline proof for internal/webwalk: it plans a route over the
// collision pack and the transport graph and prints every walk leg and hop, with no server.
// With -locs and -config it also checks every loc-backed graph edge against the static loc
// index (is that loc id placed at that tile, and does it offer that op).
//
//	go run ./cmd/webwalkplan -pack <collision.lcnav.gz> -from 2741,3444,0 -to 2711,3471,1
//	go run ./cmd/webwalkplan -pack <pack> -from 3029,3217,0 -to 2924,3152,0 -npc -special
//	go run ./cmd/webwalkplan -pack <pack> -locs <locs.lcli.gz> -config <config.jag> -check
//
// -pack defaults to $RSB0T_COLLISION_PACK. A planned route is not a walked route: this only
// shows what a Walker would try.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/webwalk"
)

func main() {
	packPath := flag.String("pack", os.Getenv("RSB0T_COLLISION_PACK"), "collision pack (default $RSB0T_COLLISION_PACK)")
	fromFlag := flag.String("from", "", "start tile x,z,level")
	toFlag := flag.String("to", "", "destination tile x,z,level")
	npc := flag.Bool("npc", false, "allow NPC-operated hops (ship fares, spirit trees); a Walker needs a handler for them")
	special := flag.Bool("special", false, "allow special crossings (tolls, fares, dialogue gates); a Walker needs a handler for them")
	walkOnly := flag.Bool("walkonly", false, "ignore the transport graph")
	avoidKinds := flag.String("avoidkinds", "", "comma-separated edge kinds to leave out (ship,shortcut,...)")
	budget := flag.Int("budget", 0, "search expansion cap (0 = default)")
	corners := flag.Bool("corners", false, "print every walk corner")
	zones := flag.String("zones", "", "comma-separated KnownDangerZones ids to avoid")
	avoidRect := flag.String("avoidrect", "", "semicolon-separated no-go rectangles minX,maxX,minZ,maxZ,level (level -1 for all)")
	auto := flag.Bool("auto", false, "add the automatic danger zones, as a Walker does by default")
	combat := flag.Int("combat", 0, "player combat level for conditional zones (0 = unknown, which keeps them)")
	check := flag.Bool("check", false, "check graph edges against -locs and -config")
	checkAll := flag.Bool("check-all", false, "with -check, list every flagged edge instead of only the first few")
	traffic := flag.Bool("traffic", false, "with -check, rank the flagged edges by the real script legs that plan through them")
	locsPath := flag.String("locs", "", "static loc index (cmd/locbake) for -check")
	configPath := flag.String("config", "", "config archive for -check")
	flag.Parse()

	if *packPath == "" {
		fail("set -pack or $RSB0T_COLLISION_PACK")
	}
	started := time.Now()
	pk, err := pathfind.Load(*packPath)
	check1(err)
	g, err := webwalk.Default()
	check1(err)
	p := webwalk.NewPlanner(pk, g)
	fmt.Printf("pack %d mapsquares, graph %d edges (%d kept, %d with unwalkable ends), loaded in %v\n  source: %s\n",
		pk.Mapsquares, len(g.Edges), p.Edges, p.Dropped, time.Since(started).Round(time.Millisecond), g.Source)

	if *check {
		if *locsPath == "" || *configPath == "" {
			fail("-check needs -locs and -config")
		}
		ix, err := locindex.Load(*locsPath)
		check1(err)
		cfg, err := cachecfg.LoadFile(*configPath)
		check1(err)
		flagged := checkEdges(g, pk, ix, cfg, *checkAll)
		if *traffic {
			trafficReport(p, flagged)
		}
	}
	if *fromFlag == "" && *toFlag == "" {
		return
	}
	from, to := parsePoint(*fromFlag), parsePoint(*toFlag)
	opt := webwalk.Options{MaxExpansions: *budget, AllowNPC: *npc, AllowSpecial: *special, WalkOnly: *walkOnly}
	var ids []string
	if *zones != "" {
		ids = strings.Split(*zones, ",")
	}
	var rects []webwalk.Rect
	if *avoidRect != "" {
		for _, spec := range strings.Split(*avoidRect, ";") {
			var r webwalk.Rect
			if _, err := fmt.Sscanf(spec, "%d,%d,%d,%d,%d", &r.MinX, &r.MaxX, &r.MinZ, &r.MaxZ, &r.Level); err != nil {
				fail("-avoidrect %q: want minX,maxX,minZ,maxZ,level (level -1 for all): %v", spec, err)
			}
			rects = append(rects, r)
		}
	}
	if len(ids) > 0 || len(rects) > 0 || *auto {
		ctx := webwalk.ZoneContext{IncludeAutomatic: *auto, CombatLevel: *combat, CombatKnown: *combat > 0, Start: &from, Dest: &to}
		opt.AvoidZones = webwalk.ResolveDangerZones(ids, rects, ctx)
		fmt.Printf("avoiding %d zone rectangle(s)\n", len(opt.AvoidZones))
	}
	if *avoidKinds != "" {
		opt.AvoidKinds = map[webwalk.Kind]bool{}
		for _, k := range strings.Split(*avoidKinds, ",") {
			opt.AvoidKinds[webwalk.Kind(strings.TrimSpace(k))] = true
		}
	}
	started = time.Now()
	r := p.Plan(from, to, opt)
	fmt.Printf("plan %s -> %s in %v: %s\n", from, to, time.Since(started).Round(time.Millisecond), r.String())
	if r.OK && *corners {
		for _, leg := range r.Legs() {
			if leg.Hop == nil {
				fmt.Printf("  walk corners %v\n", leg.Tiles)
			}
		}
	}
	if !r.OK {
		os.Exit(1)
	}
}

// checkEdges reports, for every loc-backed edge whose stand tile is in the pack, whether the
// loc index places that id at the edge's loc tile and whether its type offers the action.
// DirectionFlag bits, as the client's dash3d/DirectionFlag.ts. A bit SET in a loc type's
// forceapproach BLOCKS that side: the content compiler starts from 0b1111 and clears the one
// side its `forceapproach=<side>` names (engine tools/pack/config/LocConfig.ts), so
// `forceapproach=east` is 13 and east is the only side left legal.
const (
	dirNorth = 0x1
	dirEast  = 0x2
	dirSouth = 0x4
	dirWest  = 0x8
)

// approachBit is the forceapproach bit the stand tile's side of a 1x1 loc is tested against,
// as CollisionMap.testLoc does, or 0 when the stand is not orthogonally beside the loc.
func approachBit(standX, standZ, locX, locZ int) int {
	switch {
	case standX == locX-1 && standZ == locZ:
		return dirWest
	case standX == locX+1 && standZ == locZ:
		return dirEast
	case standX == locX && standZ == locZ-1:
		return dirSouth
	case standX == locX && standZ == locZ+1:
		return dirNorth
	}
	return 0
}

// rotateApproach turns a loc type's forceapproach by its placement angle, as Client.ts does
// before calling testLoc.
func rotateApproach(forceapproach, angle int) int {
	if angle == 0 {
		return forceapproach
	}
	return ((forceapproach << uint(angle)) & 0xf) + (forceapproach >> uint(4-angle))
}

func sideName(bit int) string {
	switch bit {
	case dirNorth:
		return "north"
	case dirEast:
		return "east"
	case dirSouth:
		return "south"
	case dirWest:
		return "west"
	}
	return "?"
}

// consultsForceApproach reports whether the server tests a loc's forceapproach at all when
// it is placed at this shape. ReachStrategy.exitStrategy (engine routefinder) sends shapes
// 0-3 and 9 to the wall strategy and 4-8 to the wall-decor strategy, neither of which takes
// a blockAccessFlags argument; only the rectangle strategies (10, 11, 22, and the exclusive
// -2) ever read it. A forceapproach on any other shape is data the engine never looks at.
func consultsForceApproach(shape int) bool {
	return shape == -2 || shape == 10 || shape == 11 || shape == 22
}

// legalSides lists the sides of a 1x1 loc a rotated forceapproach still allows.
func legalSides(rotated int) []string {
	var out []string
	for _, bit := range []int{dirNorth, dirEast, dirSouth, dirWest} {
		if rotated&bit == 0 {
			out = append(out, sideName(bit))
		}
	}
	return out
}

// edgeKey identifies one graph edge, including its stand tile. The stand is part of the
// identity because one loc can back two edges that share its id, level and action and differ
// only in which side they stand on: loc 1740 at 2698,3496 has both a north stand (which the
// audit flags) and a south one (which it does not). Keying without the stand silently merges
// the two, so a route using the good twin would be read as traffic over the flagged one.
func edgeKey(e *webwalk.Edge) string {
	return fmt.Sprintf("%d@%d,%d,%d:%s from %d,%d", e.LocID, e.LocX, e.LocZ, e.From.Level, e.Action, e.From.X, e.From.Z)
}

// checkEdges reports on every loc-backed edge and returns the flagged ones (edgeKey to the
// one-line reason), so -traffic can rank them by the routes that actually use them.
func checkEdges(g *webwalk.Graph, pk *pathfind.Pack, ix *locindex.Index, cfg *cachecfg.Config, all bool) map[string]string {
	flagged := map[string]string{}
	var exact, nearby, missing, badOp, skipped, badApproach, shapeExempt int
	misses := map[string]int{}
	var examples []string
	var approachExamples []string
	var shapeExamples []string
	for i := range g.Edges {
		e := &g.Edges[i]
		if !e.HasLoc() {
			continue
		}
		if !pk.Walkable(e.From.X, e.From.Z, e.From.Level) || !pk.Walkable(e.To.X, e.To.Z, e.To.Level) {
			skipped++
			continue
		}
		ids := map[int]bool{e.LocID: true}
		if e.OpenLocID >= 0 {
			ids[e.OpenLocID] = true
		}
		found, near := false, false
		var foundID, foundAngle, foundShape int
		for _, l := range ix.At(e.LocX, e.LocZ, e.From.Level) {
			if ids[l.ID] {
				found, foundID, foundAngle, foundShape = true, l.ID, l.Angle, l.Shape
			}
		}
		if !found {
			ix.Each(e.LocX-2, e.LocZ-2, e.LocX+2, e.LocZ+2, e.From.Level, func(l locindex.Loc) {
				if ids[l.ID] {
					near, foundID, foundAngle, foundShape = true, l.ID, l.Angle, l.Shape
				}
			})
		}
		switch {
		case found:
			exact++
		case near:
			nearby++
		default:
			missing++
			misses[string(e.Kind)]++
			if len(examples) < 12 {
				examples = append(examples, e.String())
			}
			continue
		}
		if e.Special != nil && e.Special.UseItem != "" {
			continue // an item is used on the loc; it has no op of its own
		}
		// A door's action is on its closed loc; a shut trapdoor's is on its open state, which
		// the walker opens first. Either offering it is enough.
		offers := func(id int) bool {
			t, ok := cfg.Loc(id)
			return ok && cachecfg.OpIndex(t.Ops, e.Action) != 0
		}
		if _, ok := cfg.Loc(foundID); ok && !offers(foundID) && (e.OpenLocID < 0 || !offers(e.OpenLocID)) {
			t, _ := cfg.Loc(foundID)
			badOp++
			if len(examples) < 24 {
				examples = append(examples, "op missing: "+e.String()+fmt.Sprintf(" ops=%q", t.Ops))
			}
		}
		// The stand tile against the loc's forceapproach. The engine refuses an op from a
		// blocked side (GameMap.reachedLoc), and nothing in internal/webwalk knows the field
		// exists, so a baked stand can name a side the server will never accept from.
		opID := foundID
		if e.OpenLocID >= 0 && offers(e.OpenLocID) {
			opID = e.OpenLocID
		}
		if t, ok := cfg.Loc(opID); ok && t.ForceApproach != 0 && t.Width == 1 && t.Length == 1 {
			rotated := rotateApproach(t.ForceApproach, foundAngle)
			bit := approachBit(e.From.X, e.From.Z, e.LocX, e.LocZ)
			if bit != 0 && rotated&bit != 0 {
				// Why the shape gate: the server only consults forceapproach for rectangle
				// shapes. ReachStrategy.reached dispatches on shape, and the wall (0-3, 9)
				// and wall-decor (4-8) strategies are never handed blockAccessFlags at all,
				// so a forceapproach on a loc placed at one of those shapes is dead data.
				if !consultsForceApproach(foundShape) {
					shapeExempt++
					if all || len(shapeExamples) < 12 {
						shapeExamples = append(shapeExamples, fmt.Sprintf(
							"forceapproach not consulted, shape %d: %s — loc %d forceapproach=%d angle=%d, stand is %s of it",
							foundShape, e.String(), opID, t.ForceApproach, foundAngle, sideName(bit)))
					}
					continue
				}
				badApproach++
				reason := fmt.Sprintf("loc %d forceapproach=%d angle=%d, stand is %s of it, only %v legal",
					opID, t.ForceApproach, foundAngle, sideName(bit), legalSides(rotated))
				flagged[edgeKey(e)] = reason
				if all || len(approachExamples) < 12 {
					approachExamples = append(approachExamples, "stand on a blocked side: "+e.String()+" — "+reason)
				}
			}
		}
	}
	fmt.Printf("check: %d loc edges placed exactly, %d within 2 tiles, %d not found, %d whose loc lacks the op, %d whose stand is on a forceapproach-blocked side, %d whose forceapproach the server never consults at this shape, %d skipped (ends not walkable)\n",
		exact, nearby, missing, badOp, badApproach, shapeExempt, skipped)
	kinds := make([]string, 0, len(misses))
	for k := range misses {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		fmt.Printf("  not found, %s: %d\n", k, misses[k])
	}
	for _, ex := range examples {
		fmt.Println("  e.g. " + ex)
	}
	for _, ex := range approachExamples {
		fmt.Println("  " + ex)
	}
	for _, ex := range shapeExamples {
		fmt.Println("  " + ex)
	}
	return flagged
}

// scriptRoute is one leg a registered script or freshstartrunner really walks. The tiles are
// the ones the script packages themselves name, not invented waypoints, so the ranking below
// reflects real traffic rather than a guess about which ladders look important.
type scriptRoute struct {
	name     string
	from, to webwalk.Point
}

func pt(x, z int, level ...int) webwalk.Point {
	p := webwalk.Point{X: x, Z: z}
	if len(level) > 0 {
		p.Level = level[0]
	}
	return p
}

// scriptRoutes is sourced from the plan packages: freshstart/plan.go, barbarianplan,
// crafterplan/flaxplan/gemplan/herbplan/bankfletchplan/woodcutplan (all Seers), fisherplan
// (Catherby and the Guild), essenceplan, thieverplan, vialplan, smeltplan, shopplan and
// mulereceiverplan. Each is planned in both directions.
var scriptRoutes = []scriptRoute{
	// freshstartrunner's first day: Lumbridge, the Draynor road, Port Sarim, the ships.
	{"freshstart lumbridge-store", pt(3222, 3218), pt(3209, 3247)},
	{"freshstart lumbridge-pen", pt(3222, 3218), pt(3197, 3266)},
	{"freshstart lumbridge-wheel", pt(3222, 3218), pt(3209, 3213, 1)},
	{"freshstart lumbridge-shears", pt(3222, 3218), pt(3152, 3306)},
	{"freshstart lumbridge-draynor", pt(3222, 3218), pt(3093, 3243)},
	{"freshstart draynor-portsarim", pt(3093, 3243), pt(3027, 3218)},
	{"freshstart portsarim-karamjadeck", pt(3027, 3218), pt(2957, 3141, 1)},
	{"freshstart karamja-mainlandgate", pt(2956, 3143, 1), pt(2816, 3182)},
	{"freshstart karamja-brimhaven", pt(2816, 3182), pt(2772, 3234)},
	{"freshstart ardougne-catherby", pt(2683, 3268, 1), pt(2809, 3441)},
	{"freshstart varrock-thessalia", pt(3222, 3218), pt(3204, 3417)},
	// The Seers cluster: ProgressiveCrafter, FlaxPicker, BankFletcher, gem, herb, woodcutter.
	{"crafter bank-field", pt(2725, 3493), pt(2741, 3444)},
	{"crafter bank-wheelhouse", pt(2725, 3493), pt(2711, 3471, 1)},
	{"flax bank-field", pt(2725, 3493), pt(2742, 3452)},
	{"woodcut bank-normal", pt(2725, 3493), pt(2730, 3486)},
	{"woodcut bank-oak", pt(2725, 3493), pt(2724, 3488)},
	{"woodcut bank-willow", pt(2725, 3493), pt(2711, 3508)},
	{"woodcut bank-maple", pt(2725, 3493), pt(2727, 3500)},
	{"woodcut bank-yew", pt(2725, 3493), pt(2709, 3462)},
	{"woodcut bank-magic", pt(2725, 3493), pt(2693, 3425)},
	{"mule seers-post", pt(2725, 3491), pt(2725, 3493)},
	// BarbarianAgility: the Seers bank to the outpost pipe.
	{"barbarian bank-pipe", pt(2725, 3491), pt(2552, 3561)},
	// ProgressiveFisher: Catherby, and the Fishing Guild.
	{"fisher catherby-bank-spot", pt(2809, 3441), pt(2837, 3432)},
	{"fisher guild-bank-camp", pt(2586, 3420), pt(2603, 3416)},
	{"fisher catherby-guild", pt(2809, 3441), pt(2586, 3420)},
	// EssenceMiner: Varrock east bank, Aubury, and the Nurmof/essence hop.
	{"essence bank-aubury", pt(3251, 3420), pt(3253, 3402)},
	{"essence varrock-nurmofhop", pt(3251, 3420), pt(3019, 3449)},
	{"essence nurmofhop-nurmof", pt(3019, 3449), pt(2997, 9844)},
	// ProgressiveThiever: Ardougne market, and its other banks.
	{"thiever ardougne-bank-market", pt(2655, 3283), pt(2661, 3306)},
	{"thiever ardougne-varrock", pt(2655, 3283), pt(3253, 3420)},
	{"thiever ardougne-yanille", pt(2655, 3283), pt(2612, 3092)},
	{"thiever ardougne-grandtree", pt(2655, 3283), pt(2449, 3482, 1)},
	// VialFiller: Falador's two banks, the fountain and Jatix.
	{"vial faladorwest-fountain", pt(2946, 3368), pt(2949, 3381)},
	{"vial faladorwest-east", pt(2946, 3368), pt(3013, 3355)},
	{"vial faladoreast-jatix", pt(3013, 3355), pt(2899, 3430)},
	// SmelterBot and the shop runner.
	{"smelt bank-furnace", pt(3269, 3167), pt(3275, 3185)},
	{"shop varrock-bank", pt(3253, 3420), pt(3251, 3420)},
}

// trafficReport ranks the flagged edges by how many real script legs plan through them. An
// edge no route touches is real but idle; one several routes cross is worth proving live.
func trafficReport(p *webwalk.Planner, flagged map[string]string) {
	uses := map[string][]string{}
	planned, failed, hopLegs := 0, 0, 0
	// Why the hop coverage is reported: "no flagged edge carries traffic" is only a finding
	// if these routes cross loc hops at all. If they turned out to be nearly pure walking,
	// the zero would say the measure was broken rather than that the edges are idle.
	allHops := map[string]bool{}
	for _, r := range scriptRoutes {
		for dir := 0; dir < 2; dir++ {
			from, to, name := r.from, r.to, r.name
			if dir == 1 {
				from, to, name = r.to, r.from, r.name+" (back)"
			}
			route := p.Plan(from, to, webwalk.Options{AllowNPC: true, AllowSpecial: true})
			if !route.OK {
				failed++
				fmt.Printf("  no plan for %s: %s\n", name, route.Reason)
				continue
			}
			planned++
			seen := map[string]bool{}
			for _, leg := range route.Legs() {
				if leg.Hop == nil {
					continue
				}
				hopLegs++
				k := edgeKey(leg.Hop)
				allHops[k] = true
				if _, bad := flagged[k]; bad && !seen[k] {
					seen[k] = true
					uses[k] = append(uses[k], name)
				}
			}
		}
	}
	keys := make([]string, 0, len(uses))
	for k := range uses {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(uses[keys[i]]) != len(uses[keys[j]]) {
			return len(uses[keys[i]]) > len(uses[keys[j]])
		}
		return keys[i] < keys[j]
	})
	fmt.Printf("traffic: %d script legs planned, %d unplannable; they cross %d loc hops (%d distinct edges), and %d of the %d flagged edges carry real traffic\n",
		planned, failed, hopLegs, len(allHops), len(uses), len(flagged))
	for _, k := range keys {
		fmt.Printf("  %2d leg(s)  %s — %s\n     used by: %s\n", len(uses[k]), k, flagged[k], strings.Join(uses[k], ", "))
	}
}

func parsePoint(s string) webwalk.Point {
	parts := strings.Split(s, ",")
	if len(parts) < 2 || len(parts) > 3 {
		fail("tile %q: want x,z[,level]", s)
	}
	var v [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			fail("tile %q: %v", s, err)
		}
		v[i] = n
	}
	return webwalk.Point{X: v[0], Z: v[1], Level: v[2]}
}

func check1(err error) {
	if err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "webwalkplan: "+format+"\n", args...)
	os.Exit(2)
}
