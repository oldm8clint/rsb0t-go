package webwalk

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"rsb0t-go/internal/randomevents"
)

//go:embed graph.json
var defaultGraphJSON []byte

// Graph is a set of directed transport edges.
type Graph struct {
	// Source says where the edges came from.
	Source string
	Edges  []Edge
}

type rawEdge struct {
	RawFrom          rawPoint  `json:"from"`
	RawTo            rawPoint  `json:"to"`
	Kind             Kind      `json:"kind"`
	Cost             int       `json:"cost"`
	LocName          string    `json:"locName"`
	Action           string    `json:"action"`
	LocID            *int      `json:"locId"`
	OpenLocID        *int      `json:"openLocId"`
	LocX             int       `json:"locX"`
	LocZ             int       `json:"locZ"`
	LandsOnTile      bool      `json:"landsOnTile"`
	AcceptAnyLanding bool      `json:"acceptAnyLanding"`
	Requires         *Requires `json:"requires"`
	Special          *Special  `json:"special"`
}

type rawPoint struct {
	X     int `json:"x"`
	Z     int `json:"z"`
	Level int `json:"level"`
}

// ParseGraph reads a graph in graph.json's format.
func ParseGraph(b []byte) (*Graph, error) {
	var file struct {
		Source string    `json:"source"`
		Edges  []rawEdge `json:"edges"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("webwalk: graph: %w", err)
	}
	g := &Graph{Source: file.Source, Edges: make([]Edge, 0, len(file.Edges))}
	for i, r := range file.Edges {
		if r.Kind == "" {
			return nil, fmt.Errorf("webwalk: graph edge %d has no kind", i)
		}
		e := Edge{
			From:             Point{X: r.RawFrom.X, Z: r.RawFrom.Z, Level: r.RawFrom.Level},
			To:               Point{X: r.RawTo.X, Z: r.RawTo.Z, Level: r.RawTo.Level},
			Kind:             r.Kind,
			Cost:             r.Cost,
			LocName:          r.LocName,
			Action:           r.Action,
			LocID:            -1,
			OpenLocID:        -1,
			LocX:             r.LocX,
			LocZ:             r.LocZ,
			LandsOnTile:      r.LandsOnTile,
			AcceptAnyLanding: r.AcceptAnyLanding,
			Requires:         r.Requires,
			Special:          r.Special,
		}
		if r.LocID != nil {
			e.LocID = *r.LocID
		}
		if r.OpenLocID != nil {
			e.OpenLocID = *r.OpenLocID
		}
		if e.Cost <= 0 {
			e.Cost = DefaultCost(e.Kind)
		}
		g.Edges = append(g.Edges, e)
	}
	return g, nil
}

// Override moves one baked edge's stand tile to the tile live testing proved the server
// accepts the op from.
type Override struct {
	// LocID, LocX, LocZ and Level (the stand's level) and Action pick the edge.
	LocID      int
	LocX, LocZ int
	Level      int
	Action     string
	// From is the proven stand tile.
	From Point
	// Evidence says where it was proven.
	Evidence string
}

// ApproachOverrides are the stand tiles live runs proved, where the baked data picks a
// different neighbour. See COLLAB.md's ship-crossing entries and crafterrunner's doc comment.
var ApproachOverrides = []Override{
	{
		LocID: 273, LocX: 2682, LocZ: 3267, Level: 1, Action: "Climb-down",
		From:     Point{X: 2681, Z: 3267, Level: 1},
		Evidence: "Ardougne ship ladder: cmd/guidecrash -testloc from each side, only the west tile works (freshstart.ArdougneLadderApproach)",
	},
	{
		LocID: 1749, LocX: 3225, LocZ: 3472, Level: 2, Action: "Climb-down",
		From: Point{X: 3224, Z: 3472, Level: 2},
		Evidence: "Varrock castle ladder, level 2: live 2026-09-21, acct102 recovering from a random-event fail-teleport got " +
			"\"server can't reach Ladder at 3225,3472 from 3225,3473,2\" and then climbed from the WEST tile 3224,3472,2. " +
			"The baked stand is the north one. Recorded from that live pair rather than derived: the loc is " +
			"laddertop_directional (forceapproach=north) placed at shape 10 ANGLE 3 (maps/m50_54.jm2 \"2 25 16: 1749 10 3\"), " +
			"and rotating the named side by that angle would make WEST the blocked one -- which is the tile that actually " +
			"worked. So either the rotation runs the other way or the named side is the permitted one rather than the " +
			"forbidden one; this entry does not guess which, and anyone extending the rule should settle it with " +
			"cmd/guidecrash -testloc from all eight neighbours before trusting a derivation over a live climb.",
	},
	{
		LocID: 1749, LocX: 2954, LocZ: 3390, Level: 2, Action: "Climb-down",
		From:     Point{X: 2955, Z: 3390, Level: 2},
		Evidence: "Port Sarim ladder: cmd/guidecrash -testloc, only the east tile works (freshstart.PortSarimLadderApproach)",
	},
	{
		LocID: 1750, LocX: 2954, LocZ: 3390, Level: 1, Action: "Climb-up",
		From:     Point{X: 2955, Z: 3390, Level: 1},
		Evidence: "Port Sarim ladder going up: webwalk2's live walk got \"I can't reach that!\" four times from the baked west tile 2953,3390,1; the east tile, as for 1749 above it",
	},
	{
		LocID: 4626, LocX: 2897, LocZ: 3566, Level: 0, Action: "Climb-up",
		From:     Point{X: 2897, Z: 3565, Level: 0},
		Evidence: "Burthorpe castle stairs going up: the questproof lab run deathplateau-empty-1 (2026-09-27) was refused from the baked west tile 2896,3566,0 and climbed from 2897,3565,0 (the walker's own \"worth an ApproachOverride\"); locreach gives 2897,3565 and 2898,3565 for this 2x3 forceapproach loc",
	},
	{
		LocID: 4625, LocX: 2897, LocZ: 3567, Level: 1, Action: "Climb-down",
		From:     Point{X: 2897, Z: 3569, Level: 1},
		Evidence: "Burthorpe castle stairs going down: deathplateau-empty-1 got \"server can't reach Staircase at 2897,3567 from 2896,3567,1\" five times and parked, and otherStands tries only the origin tile's orthogonal neighbours, all inside this 2x2 loc or refused. locreach gives 2897,3569 and 2898,3569, and 2897,3569,1 is where the up-stairs land the account",
	},
	// The spooky staircases (content scripts/ladders+stairs/scripts/stairs.rs2, [oploc1,spookystairs]
	// and [oploc1,spookystairstop]) at Draynor Manor, Witch's House and the Grail castle. Both
	// types are angle 0, shape 10 at every placement: spookystairs (1730) is 2x3 with
	// forceapproach=south and spookystairstop (1731) is 2x2 with forceapproach=north, so the
	// engine's ReachStrategy.reachRectangle accepts an op only from the two tiles on that one
	// side (locreach.Tiles). With the server's clientRoutefinder on, Player.pathToPathingTarget
	// does not path to a loc at all, so an op sent from any other tile is "I can't reach that!".
	{
		LocID: 1731, LocX: 3108, LocZ: 3364, Level: 1, Action: "Walk-down",
		From: Point{X: 3108, Z: 3366, Level: 1},
		Evidence: "Draynor Manor spooky stairs going down: live 2026-10-04 mackerel, acct81 and spare103 (Holy Grail's grail-whistles step) got " +
			"\"server can't reach Staircase at 3108,3364 from 3108,3367,1\" more than 5,000 times between them and were trapped on level 1; otherStands tries " +
			"only the origin's orthogonal neighbours, which are inside the loc or on its blocked south and west sides. " +
			"The 2x2 loc covers 3108-3109,3364-3365 and forceapproach=north leaves 3108,3366 and 3109,3366 as the only legal stands",
	},
	{
		LocID: 1730, LocX: 3108, LocZ: 3363, Level: 0, Action: "Walk-up",
		From: Point{X: 3108, Z: 3362, Level: 0},
		Evidence: "Draynor Manor spooky stairs going up: live 2026-10-04 \"Walk-up Staircase at 3108,3363 only answered from 3108,3362,0, not the planned " +
			"3107,3363,0\" for acct81, mackerel and spare103. The 2x3 loc's forceapproach=south leaves 3108,3362 and 3109,3362; the other baked " +
			"stand, 3108,3360,0, is two tiles short of the loc",
	},
	{
		LocID: 1730, LocX: 2906, LocZ: 3469, Level: 0, Action: "Walk-up",
		From:     Point{X: 2906, Z: 3468, Level: 0},
		Evidence: "Witch's House spooky stairs going up: same loc and rule as Draynor's (locreach gives 2906,3468 and 2907,3468); the baked west stand 2905,3469,0 is a blocked side. The other baked edge already stands at 2906,3468",
	},
	{
		LocID: 1730, LocX: 2768, LocZ: 4682, Level: 1, Action: "Walk-up",
		From:     Point{X: 2768, Z: 4681, Level: 1},
		Evidence: "Grail castle spooky stairs going up (level 1 to 2): same loc and rule as Draynor's (locreach gives 2768,4681 and 2769,4681); the baked west stand 2767,4682,1 is a blocked side. The other baked edge already stands at 2768,4681",
	},
	{
		LocID: 1746, LocX: 2954, LocZ: 3389, Level: 1, Action: "Climb-down",
		From:     Point{X: 2955, Z: 3389, Level: 1},
		Evidence: "Port Sarim ladder, lower half: 1749 above only reaches level 1, a sealed 7-tile pocket whose way out is this ladder. cmd/guidecrash -testloc from all 8 neighbours 2026-09-18 — the four orthogonal ones reach level 0, the four diagonals are refused (freshstart.PortSarimLadderLowerApproach)",
	},
	{
		LocID: 2082, LocX: 2956, LocZ: 3144, Level: 1, Action: "Cross",
		From:     Point{X: 2956, Z: 3143, Level: 1},
		Evidence: "Karamja gangplank: all 8 neighbours tested live, orthogonal only (freshstart.KaramjaGangplankApproach); same as the baked tile",
	},
	{
		LocID: 1759, LocX: 3008, LocZ: 3150, Level: 0, Action: "Climb-Down",
		From: Point{X: 3009, Z: 3150, Level: 0},
		Evidence: "Asgarnian ice dungeon ladder: cmd/dungeonroutetest 2026-09-18 got \"I can't reach that!\" seven times from the baked north stand 3008,3151. " +
			"Loc 1759 is forceapproach=13 at placement angle 0, and the content compiler clears only the named side's bit, so east is the one legal approach: the stand is 3009,3150 (cmd/webwalkplan -check)",
	},
	{
		LocID: 1750, LocX: 2946, LocZ: 3363, Level: 0, Action: "Climb-up",
		From: Point{X: 2947, Z: 3363, Level: 0},
		Evidence: "Falador west bank ladder going up: cmd/approachsweeptest -mode sweep 2026-09-18 got \"I can't reach that!\" from the baked north stand 2946,3364,0, " +
			"and Walker.otherStands climbed from the east tile 2947,3363,0. Loc 1750 is forceapproach=14 at angle 1, east only",
	},
	{
		LocID: 273, LocX: 2810, LocZ: 3419, Level: 1, Action: "Climb-down",
		From: Point{X: 2810, Z: 3418, Level: 1},
		Evidence: "Catherby ship's ladder going down: cmd/approachsweeptest 2026-09-18 was refused from the baked north stand 2810,3420,1 and recovered to the south tile 2810,3418,1. " +
			"Loc 273 is forceapproach=11 at angle 0, south only. The deck itself is an isolated component in the baked pack (the Catherby bank plans to it as unreachable), so this edge is only ever entered by climbing 272 up",
	},
	{
		LocID: 1749, LocX: 2946, LocZ: 3363, Level: 1, Action: "Climb-down",
		From: Point{X: 2947, Z: 3363, Level: 1},
		Evidence: "Falador west bank ladder: cmd/approachsweeptest -mode sweep 2026-09-18 got \"I can't reach that!\" from the baked north stand 2946,3364,1, " +
			"and Walker.otherStands climbed from the east tile 2947,3363,1. Loc 1749 is forceapproach=14 at angle 1, so east is the one legal side",
	},
	{
		LocID: 272, LocX: 3034, LocZ: 3215, Level: 0, Action: "Climb-up",
		From: Point{X: 3034, Z: 3214, Level: 0},
		Evidence: "Port Sarim ship's ladder: cmd/approachsweeptest 2026-09-18 got \"I can't reach that!\" twice from the baked west stand 3033,3215,0, " +
			"and recovered to the south tile 3034,3214,0. Loc 272 is forceapproach=11 at angle 0, south only",
	},
	{
		LocID: 273, LocX: 3034, LocZ: 3215, Level: 1, Action: "Climb-down",
		From: Point{X: 3034, Z: 3214, Level: 1},
		Evidence: "The same Port Sarim ship's ladder going down: cmd/approachsweeptest 2026-09-18 was refused twice from the baked west stand 3033,3215,1 " +
			"and recovered to the south tile 3034,3214,1. Loc 273 is forceapproach=11 at angle 0, south only",
	},
	{
		LocID: 272, LocX: 2810, LocZ: 3419, Level: 0, Action: "Climb-up",
		From: Point{X: 2810, Z: 3418, Level: 0},
		Evidence: "Catherby ship's ladder: cmd/approachsweeptest 2026-09-18 got \"I can't reach that!\" from the baked north stand 2810,3420,0, " +
			"and recovered to the south tile 2810,3418,0. Loc 272 is forceapproach=11 at angle 0, south only",
	},
	{
		LocID: 1750, LocX: 2699, LocZ: 3476, Level: 0, Action: "Climb-up",
		From: Point{X: 2699, Z: 3475, Level: 0},
		Evidence: "Seers ladder west of the bank: cmd/approachsweeptest -mode sweep 2026-09-18 got \"I can't reach that!\" from the baked east stand 2700,3476,0, " +
			"and Walker.otherStands then climbed from the south tile 2699,3475,0. Loc 1750 is forceapproach=14 at angle 2, so south is the one legal side (cmd/webwalkplan -check)",
	},
	{
		LocID: 1749, LocX: 2699, LocZ: 3476, Level: 1, Action: "Climb-down",
		From: Point{X: 2699, Z: 3475, Level: 1},
		Evidence: "The same Seers ladder going down: cmd/approachsweeptest 2026-09-18 got \"I can't reach that!\" from the baked east stand 2700,3476,1, " +
			"and recovered to the south tile 2699,3475,1. Loc 1749 is forceapproach=14 at angle 2, south only",
	},
	{
		LocID: 1747, LocX: 2715, LocZ: 3470, Level: 0, Action: "Climb-up",
		From:     Point{X: 2714, Z: 3470, Level: 0},
		Evidence: "Seers wheelhouse ladder: live climbs from the west tile (crafterrunner.ladderApproach); the diagonal was refused",
	},
	{
		LocID: 1746, LocX: 2715, LocZ: 3470, Level: 1, Action: "Climb-down",
		From:     Point{X: 2714, Z: 3470, Level: 1},
		Evidence: "Seers wheelhouse ladder top: live climb-down from the west tile (crafterrunner.ladderApproach)",
	},
}

// ApplyOverrides moves the From tile of every edge an override picks, returning how many
// edges each override matched (by index). Two stand tiles of one loc can both match; the
// duplicates this leaves are harmless to the search.
func (g *Graph) ApplyOverrides(overrides []Override) []int {
	hits := make([]int, len(overrides))
	for i := range g.Edges {
		e := &g.Edges[i]
		for j, o := range overrides {
			if e.LocID == o.LocID && e.LocX == o.LocX && e.LocZ == o.LocZ && e.From.Level == o.Level && e.Action == o.Action {
				if e.From != o.From {
					e.From = o.From
					e.Override = o.Evidence
				}
				hits[j]++
			}
		}
	}
	return hits
}

// Correction fixes baked edges whose loc data disagrees with the 289 map files and loc
// config (cmd/webwalkplan -check finds them). It picks edges like Override does, with Level
// the stand's level; an Action of "" matches any action.
type Correction struct {
	LocID      int
	LocX, LocZ int
	Level      int
	Action     string
	// FromTile, when set, also requires the edge to start there. Two directions of one barrier
	// share a loc id, a placement and an action, so the stand tile is the only thing that tells
	// them apart — which a correction that moves a landing tile has to do.
	FromTile *Point
	// Drop removes the edges.
	Drop bool
	// SetLocID and SetOpenLocID, when not zero, replace the loc id and the open-state id.
	SetLocID, SetOpenLocID int
	// SetTo, when set, moves where the edge claims to land. Needed where the server puts the
	// player somewhere the planner cannot continue from: see the Al Kharid toll gate below.
	SetTo *Point
	// ClearRequires drops the edge's plan-time gate. It is for a barrier the baked graph gates
	// in both directions although content gates only one: see the Fishing Guild doors below.
	ClearRequires bool
	// ClearSkills drops the edge's skill gates and keeps the rest of its requirements (members). It is for a
	// crossing the baked graph gates on a level that content never checks: see basalt.go.
	ClearSkills bool
	// SetSlip records the edge's dice roll (Edge.Slip): what content does when the crossing's skill roll misses.
	SetSlip *Slip
	// AnyPlacement matches every placement of LocID (LocX and LocZ are ignored).
	AnyPlacement bool
	Why          string
}

// tollGateWhy explains the four toll-gate corrections below in one place, because the reason is
// long and identical for all of them.
//
// content/scripts/areas/area_alkharid/scripts/border_gate.rs2 is explicit about it.
// [label,pass_toll_gate] sets $coord to 0_51_50_4_27, which is 3268,3227 — the gate's OWN
// square. From the west it teleports the payer onto that square, and from that square
// [label,enter_through_border_gate_toll] with $west=false computes movecoord($loc_coord,-1,0,0)
// and teleports them back west. So paying does not put you through the gate, it shuttles you
// between 3267 and 3268, and the baked edge's landing tile was the middle of that shuttle.
//
// enter_through_border_gate_toll then loc_adds an inviswall on all four sides of the landing
// square for THREE TICKS. internal/webwalk/live overlays live loc changes onto the collision
// data, so for those three ticks the planner sees the tile the player is standing on as
// enclosed — and Walker.walkOnce replans the instant a hop returns. The plan made inside that
// window cannot step east, so it reaches for the only edge out, which is the toll edge again.
// hops.Handler.landed accepted anything within two tiles of the landing, and the two ends of
// this crossing are one tile apart, so every wrong-way crossing also reported success.
//
// Measured: 953 crossings at 10 coins each in one gear-buying run, and 181 in a run with gear
// buying OFF, which is what proved this afflicts ordinary rung travel and not just shopping.
//
// The fix is to land the edge one tile BEYOND the gate square, on a tile that is walkable in the
// baked pack (checked with cmd/webwalkplan: every one of the four below plans at cost 1 from the
// gate square) and outside the three-tick inviswall box. hops then has to step off the shuttle
// square onto it and verify that exactly, which is the other half of this fix.
const tollGateWhy = "Al Kharid toll gate: pass_toll_gate lands the payer ON the gate square 3268, " +
	"and paying again from there teleports them straight back, while enter_through_border_gate_toll walls " +
	"all four sides for three ticks. Landing one tile beyond the gate instead gives the planner a tile it " +
	"can continue from. See tollGateWhy in graph.go"

func pt(x, z int) *Point { return &Point{X: x, Z: z} }

// EdgeCorrections are the graph's disagreements with the map files, checked with
// cmd/webwalkplan -check against locs.lcli.gz and config.jag (see COLLAB.md, webwalk2).
var EdgeCorrections = []Correction{
	{
		LocID: 881, Action: "Climb down", AnyPlacement: true, SetOpenLocID: 882,
		Why: "manholeclosed (881) only offers Open; manholeopen (882) is the one with Climb down, so the walker opens it first",
	},
	{
		LocID: 2544, LocX: 2529, LocZ: 3303, Level: 0, SetLocID: 2543, SetOpenLocID: 2544,
		Why: "the map places the closed West Ardougne manhole (2543, Open); 2544 is its open state",
	},
	{
		LocID: 1746, LocX: 2378, LocZ: 3134, Level: 1, SetLocID: 4911,
		Why: "the map places loc 4911 (Ladder, Climb-down) here, not laddertop 1746",
	},
	{
		LocID: 1746, LocX: 2421, LocZ: 3073, Level: 1, SetLocID: 4911,
		Why: "the map places loc 4911 (Ladder, Climb-down) here, not laddertop 1746",
	},
	{
		LocID: 4469, AnyPlacement: true, Drop: true,
		Why: "castlewars_saradomin_spawndoor: an Energy Barrier with Pass, open only to a Castle Wars team",
	},
	{
		LocID: 4470, AnyPlacement: true, Drop: true,
		Why: "castlewars_zamorak_spawndoor: an Energy Barrier with Pass, open only to a Castle Wars team",
	},
	{
		LocID: 1530, LocX: 2929, LocZ: 3536, Level: 0, Drop: true,
		Why: "no door at 2929,3536 in the 289 map: only an inactive wall piece (1532)",
	},
	// The Al Kharid toll gate, both leaves and both directions. See tollGateWhy.
	{
		LocID: 2882, LocX: 3268, LocZ: 3227, Level: 0, Action: "Open",
		FromTile: pt(3267, 3227), SetTo: pt(3269, 3227), Why: tollGateWhy,
	},
	{
		LocID: 2882, LocX: 3268, LocZ: 3227, Level: 0, Action: "Open",
		FromTile: pt(3268, 3227), SetTo: pt(3266, 3227), Why: tollGateWhy,
	},
	{
		LocID: 2883, LocX: 3268, LocZ: 3228, Level: 0, Action: "Open",
		FromTile: pt(3267, 3228), SetTo: pt(3269, 3228), Why: tollGateWhy,
	},
	{
		LocID: 2883, LocX: 3268, LocZ: 3228, Level: 0, Action: "Open",
		FromTile: pt(3268, 3228), SetTo: pt(3266, 3228), Why: tollGateWhy,
	},
	// The Wizards' Tower's diagonal door, all four baked edges, replaced by wizardsDoorEdges in ExtraEdges.
	{
		LocID: 1536, LocX: 3107, LocZ: 3162, Level: 0, Drop: true, Why: wizardsDoorWhy,
	},
	// The two Fishing Guild doors, OUTWARD only. See guildDoorWhy.
	{
		LocID: 2025, LocX: 2611, LocZ: 3394, Level: 0, Action: "Open",
		FromTile: pt(2611, 3394), ClearRequires: true, Why: guildDoorWhy,
	},
	{
		LocID: 1530, LocX: 2611, LocZ: 3398, Level: 0, Action: "Open",
		FromTile: pt(2611, 3399), ClearRequires: true, Why: guildDoorWhy,
	},
}

// wizardsDoorWhy explains the Wizards' Tower door's replacement edges (wizardsDoorEdges).
//
// Door 1536 at 3107,3162 (maps/m48_49.jm2 "0 35 26: 1536 9 3": shape 9, angle 3) joins the tower's 24-tile east hall
// (3108,3162) to its west room (3103-3107 by 3159-3163), where the staircase up to Wizard Mizgog's floor (1738 at
// 3103,3159) and the ladder down to Sedridor stand. A diagonal door is not a wall: closed, it is a loc that blocks its
// whole tile, and content's open_door (doors/scripts/doors.rs2, door_open for wall_diagonal) moves that block one
// tile in a direction given by the angle, here to 3106,3162, the very tile the baked edges asked the walker to cross
// to or stand on, and sets down any account standing on the tile it lands on one tile aside (door_open_move_player_
// out_of_way: angle 3 pushes south, 3106,3162 to 3106,3161). So the baked edges failed both ways, every time:
//
//   - east to west, "Open" from 3108,3162 and a walk to 3106,3162: the door opens, the account walks into the
//     doorway 3107,3162 and the tile it was asked for is now solid ("did not step through Door at 3107,3162 (still
//     at 3107,3162,0, wanted 3106,3162,0)", 7 Oct 2026: ten accounts, dozens of times each);
//   - west to east, "Open" from 3106,3162: the push sets the account on 3106,3161, so it is no longer on the stand
//     the hop walks from ("still at 3106,3161,0, wanted 3108,3162,0").
//
// The Guardian's strand escape then walked each account out of the room and the quest started over, for hours (Spare33
// Be Spare33, 32 laps). The way across, with the leaf on 3106,3162 and the doorway 3107,3162 free, is through 3107,3161:
// the account opens the door from 3108,3162 (east) or from 3107,3161 (west, south of the door's own tile: nothing
// pushes it, and internal/doorescape.Reached accepts that tile), then the server's own route walks it 3108,3162 ->
// 3107,3162 -> 3107,3161 -> 3106,3161 or the reverse; a diagonal step 3107,3162 <-> 3106,3161 is refused with 3106,3162
// solid, and the server finds the orthogonal way round without help. Both are one click on the far tile.
const wizardsDoorWhy = "Wizards' Tower diagonal door 1536 at 3107,3162: opening it puts its leaf on 3106,3162 and pushes an " +
	"account there to 3106,3161, so the baked 3106,3162 <-> 3108,3162 edges can never be crossed; the way across is " +
	"3108,3162 <-> 3106,3161 with the west stand at 3107,3161. See wizardsDoorWhy in graph.go"

// wizardsDoorEdges are the crossings that replace the baked ones.
var wizardsDoorEdges = []ExtraEdge{
	{
		Edge: Edge{
			From: Point{X: 3108, Z: 3162}, To: Point{X: 3106, Z: 3161}, Kind: KindDoor, Cost: 6,
			LocName: "Door", Action: "Open", LocID: 1536, OpenLocID: -1, LocX: 3107, LocZ: 3162,
		},
		Why: wizardsDoorWhy,
	},
	{
		Edge: Edge{
			From: Point{X: 3107, Z: 3161}, To: Point{X: 3108, Z: 3162}, Kind: KindDoor, Cost: 6,
			LocName: "Door", Action: "Open", LocID: 1536, OpenLocID: -1, LocX: 3107, LocZ: 3162,
		},
		Why: wizardsDoorWhy,
	},
}

// guildDoorWhy explains the two Fishing Guild corrections above.
//
// The baked graph gates BOTH directions of both guild doors on Fishing 68. Content gates only
// the way in. skill_fishing/scripts/fishing_guild.rs2 reads
//
//	def_boolean $is_inside = ~check_axis(coord, loc_coord, loc_angle);
//	if ($is_inside = false) { if (stat(fishing) < 68) { ...refuse... return; } }
//
// and loc 2025 is placed at maps/m40_53.jm2 "0 51 2: 2025 0 3" — mapsquare 40,53 so origin
// 2560,3392, local (51,2) = 2611,3394, shape 0 (wall_straight), angle 3 (loc_south). check_axis
// on a north/south angle compares coordz, so from the guild side (z 3394, the loc's own tile)
// it is true and no level is checked; only from 2611,3393 outside is it false and gated. The
// other guild door, loc 1530 at 2611,3398, is an ordinary `_door_open_and_close` door with no
// level check in content at all; its baked gate is curation, and only its outward direction
// (from 2611,3399) is cleared here, so the planner still refuses to route a sub-68 account INTO
// the guild, which is what the gate is for.
//
// Why it matters: internal/webwalk/live.WorldState reads a skill the server has not reported
// yet as 0 and fails gated edges closed, so an account inside the guild has NO exit at all
// until its stats have synced — and the guild is a sealed room with exactly these two doors.
// An account that gets inside without the level (a fail-teleport, a script change) is likewise
// sealed in for good. This is a latent trap rather than the cause of the 2026-09-22 strandings:
// the 68 accounts stuck at 2610-2616,3394-3398 that day all had Fishing 72-75 on the panel, and
// what stranded them was the door ban in walker.go's noteRefusal.
const guildDoorWhy = "Fishing Guild doors: content checks Fishing 68 only when check_axis says the " +
	"player is outside, so the way OUT is ungated. The baked graph gated both ways, which seals an " +
	"account in whenever its skills have not synced. See guildDoorWhy in graph.go"

func (c *Correction) matches(e *Edge) bool {
	if e.LocID != c.LocID || (c.Action != "" && e.Action != c.Action) {
		return false
	}
	if c.FromTile != nil && (e.From.X != c.FromTile.X || e.From.Z != c.FromTile.Z) {
		return false
	}
	return c.AnyPlacement || (e.LocX == c.LocX && e.LocZ == c.LocZ && e.From.Level == c.Level)
}

// ApplyCorrections edits or drops the edges each correction picks, returning how many edges
// each one matched.
func (g *Graph) ApplyCorrections(corrections []Correction) []int {
	hits := make([]int, len(corrections))
	kept := g.Edges[:0]
	for i := range g.Edges {
		e := g.Edges[i]
		drop := false
		for j := range corrections {
			c := &corrections[j]
			if !c.matches(&e) {
				continue
			}
			hits[j]++
			if c.Drop {
				drop = true
				continue
			}
			if c.SetLocID != 0 {
				e.LocID = c.SetLocID
			}
			if c.SetOpenLocID != 0 {
				e.OpenLocID = c.SetOpenLocID
			}
			if c.ClearRequires {
				e.Requires = nil
			}
			if c.ClearSkills && e.Requires != nil {
				r := *e.Requires
				r.Skills = nil
				e.Requires = &r
			}
			if c.SetSlip != nil {
				s := *c.SetSlip
				e.Slip = &s
			}
			if c.SetTo != nil {
				to := *c.SetTo
				if to.Level == 0 {
					to.Level = e.To.Level
				}
				e.To = to
			}
		}
		if !drop {
			kept = append(kept, e)
		}
	}
	g.Edges = kept
	return hits
}

// EdgeAdditions are loc edges the baked graph lacks, each checked against the 289 map files and
// content's own crossing script. Default appends them before EdgeCorrections run.
//
// The Ardougne sheep pen stile. The baked graph has the wheat-field stile north of East Ardougne
// (loc 993 at 2638,3350, both ways) but not the sheep pen's, loc 993 at 2647,3375 (maps/m41_52.jm2,
// shape 10, angle 1). The pen is a 269-tile field, x 2647..2660 z 3355..3380 on the real pack, and
// that stile is its only way in or out (cmd/doordiag), so a web walk that starts inside it has no
// route anywhere. Live 2026-09-25: four ScarabSwarm accounts shut in there logged "walk to
// 2655,3283,0 ... unreachable (best 81 tiles)" -- the East Ardougne bank -- and the same for every
// other walk, for hours. The stand and landing tiles are content's own: fullstyle is one tile wide
// and two long (skill_agility/configs/shortcuts.loc, length=2), so at angle 1 it covers
// 2647..2648,3375, and [oploc1,fullstyle] (skill_agility/scripts/shortcuts.rs2) crosses it by the
// player's x against the loc's: from x 2647 or less it lands on 2649,3375, from east of that on
// 2646,3375. The same shape as the wheat-field stile's baked edges, 2637 -> 2640 across 2638.
var EdgeAdditions = []Edge{
	{
		From: Point{X: 2646, Z: 3375}, To: Point{X: 2649, Z: 3375}, Kind: KindShortcut, Cost: 10,
		LocName: "Stile", Action: "Climb-over", LocID: 993, OpenLocID: -1, LocX: 2647, LocZ: 3375, LandsOnTile: true,
	},
	{
		From: Point{X: 2649, Z: 3375}, To: Point{X: 2646, Z: 3375}, Kind: KindShortcut, Cost: 10,
		LocName: "Stile", Action: "Climb-over", LocID: 993, OpenLocID: -1, LocX: 2647, LocZ: 3375, LandsOnTile: true,
	},
}

// ExtraEdge is a hop the TS graph (doors.json, transports.json, stairEdges.json) does not have, added
// after the corrections and overrides. Why says what measured it. It is EdgeAdditions' idea with the
// evidence carried on each edge, kept as its own list so the two lanes' edits cannot collide.
type ExtraEdge struct {
	Edge Edge
	Why  string
}

// ExtraEdges are the way out of the places the production server drops whole cohorts of accounts into
// (internal/randomevents/strandsites.go has the evidence for each site). Every edge here is an EXIT: none
// leads into a Wilderness, a lair or a pocket, so the planner can never route an account into one.
//
// Rogues' Castle is NOT here, though its main door used to be: the door is a shape-9 loc, which per content's door
// script (doors/scripts/door_procs.rs2) opens by moving its leaf onto the very tile this edge asked the walker to
// stand on and pushing the account aside, and the walker's "stand, open, step across" never crossed it ("did not
// step through Door at 3279,3939 (still at 3280,3938,0, wanted 3279,3940,0)": 20 lines in the 6 October 2026 log).
// The way out of the castle is the Guardian's (randomevents' escape plan over internal/doorescape's model of the
// engine), and Default() takes every graph edge inside the castle's walls out, so no walk plans through a castle
// door either way (see withoutCastleEdges).
//
// The King Black Dragon's lair: a sealed 430-tile cave whose only exit is the Lever (1817) on its own
// tile at 2717,9801. It sets the account down at 3067,10254, the 23-tile pocket in the Wilderness
// dungeon whose ladder (1766, Climb-up to 3016,3849) IS in the graph, so from there the plan runs on.
//
// The Deserted Keep lever (1815 at 3153,3923, Pull, operated from its own tile) is the way out of the
// Wilderness the Mage Bank trip already uses (scarabplan.KeepLever), landing at 2562,3311 in East
// Ardougne. With it in the graph an account anywhere near the Keep or in the Mage Bank plans home by the
// lever instead of by a thousand tiles of overland route.
var ExtraEdges = []ExtraEdge{
	{
		Edge: Edge{
			From: Point{X: 2717, Z: 9801, Level: 0}, To: Point{X: 3067, Z: 10254, Level: 0},
			Kind: KindPortal, Cost: 20, LocName: "Lever", Action: "Pull", LocID: 1817, OpenLocID: -1, LocX: 2717, LocZ: 9801,
			LandsOnTile: true,
		},
		Why: "the King Black Dragon lair's lever out (king_black_dragon.rs2 dragonkingoutlever, 0_47_160_59_14)",
	},
	{
		Edge: Edge{
			From: Point{X: 3153, Z: 3923, Level: 0}, To: Point{X: 2562, Z: 3311, Level: 0},
			Kind: KindPortal, Cost: 20, LocName: "Lever", Action: "Pull", LocID: 1815, OpenLocID: -1, LocX: 3153, LocZ: 3923,
			LandsOnTile: true,
		},
		Why: "the Deserted Keep lever back to East Ardougne (^wilderness_to_ardougne_coord), the Mage Bank trip's own way out",
	},
}

var (
	defaultOnce  sync.Once
	defaultGraph *Graph
	defaultErr   error
)

// Default returns the embedded graph with EdgeAdditions, EdgeCorrections and ApproachOverrides
// applied and ExtraEdges added last. It is parsed once and shared; callers must not modify it (copy
// Edges first).
func Default() (*Graph, error) {
	defaultOnce.Do(func() {
		defaultGraph, defaultErr = ParseGraph(defaultGraphJSON)
		if defaultErr == nil {
			defaultGraph.Edges = append(defaultGraph.Edges, EdgeAdditions...)
			defaultGraph.ApplyCorrections(EdgeCorrections)
			defaultGraph.ApplyOverrides(ApproachOverrides)
			for _, x := range ExtraEdges {
				defaultGraph.Edges = append(defaultGraph.Edges, x.Edge)
			}
			for _, x := range wizardsDoorEdges {
				defaultGraph.Edges = append(defaultGraph.Edges, x.Edge)
			}
			for _, x := range khazardStrongholdEdges {
				defaultGraph.Edges = append(defaultGraph.Edges, x.Edge)
			}
			for _, x := range grandTreeDoorEdges {
				defaultGraph.Edges = append(defaultGraph.Edges, x.Edge)
			}
			defaultGraph.Edges = withoutCastleEdges(defaultGraph.Edges)
			penaliseHurtingShortcuts(defaultGraph.Edges)
		}
	})
	return defaultGraph, defaultErr
}

// hurtingShortcutPenalty is added to the cost of a skill-gated shortcut whose failure hurts (the Fremennik
// basalt rocks, cost 10 in graph.json), so the planner takes a road unless the shortcut saves more than this.
// Live 7 Oct 2026: walks to Rellekka went over the rocks to save a few tiles; spare23, sike and
// mackerel ended on the far side with the crossing banned after its slips (mackerel died), every walk home
// "unreachable". A crossing whose Slip is known pays slipShortcutPenalty instead (basalt.go).
const hurtingShortcutPenalty = 150

func penaliseHurtingShortcuts(edges []Edge) {
	for i := range edges {
		switch {
		case edges[i].Slip != nil:
			edges[i].Cost += slipShortcutPenalty
		case edges[i].failureHurts():
			edges[i].Cost += hurtingShortcutPenalty
		}
	}
}

// withoutCastleEdges drops every edge that starts, ends or operates inside Rogues' Castle's walls: its straight
// doors (ten of them, twenty edges in graph.json: the map's two diagonal doors, the main one among them, were never
// in it) and its staircases (twelve edges). A walk never needs the castle (nothing is there to walk to); only the
// accounts the server put in it do, and for those the Guardian's escape plan is the way out: it knows each door's
// leaves (where the open one lands, whom the door pushes aside, what the other doors then leave open), which the
// Walker's "stand, open, step across" does not, and two owners of one door fought. After the 01:32 summon on
// 6 October 2026 the Walker's castle routes stalled again and again: 317 "did not step through Door" lines in one
// log, 53 of them at the west room's north door alone (still on the door's tile 40 times, on the far side 12), each
// repath a plan of 240,000 nodes. No cause for those stalls was established; the log has neither the door's state
// nor the server's answer to the click.
func withoutCastleEdges(edges []Edge) []Edge {
	out := edges[:0:0]
	for _, e := range edges {
		_, a := randomevents.StrandCompoundAt(e.From.X, e.From.Z, e.From.Level)
		_, b := randomevents.StrandCompoundAt(e.To.X, e.To.Z, e.To.Level)
		_, c := randomevents.StrandCompoundAt(e.LocX, e.LocZ, e.From.Level)
		if a || b || c {
			continue
		}
		out = append(out, e)
	}
	return out
}
