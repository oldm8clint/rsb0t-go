package randomevents

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"rsb0t-go/internal/doorescape"
)

// This file is the catalogue of places the production server drops WHOLE COHORTS of accounts into,
// and of the only ways out of them. It sits beside failteleport.go, which catalogues the stock
// Genie/Mysterious Old Man destinations; these two are not in that enum and are not an ignored
// event's doing. They are the server's own mass summons.
//
// What the live log showed (run/fleet.log, 2026-09-29 20:15 to 2026-10-02 17:07, 3,722
// "[guardian] displaced ... UNEXPLAINED" lines):
//
//   - 2026-10-01 21:07:56-21:08:01: 224 accounts, within five seconds, whatever they were doing (the
//     Baker's stall, the cow field, the Edgeville dungeon's giants, a walk to the bank), landed on
//     2718,9816 and the tiles beside it. That is mapsquare 42,153 level 0, the King Black Dragon's lair
//     in THIS content build: npc 50 "King black dragon" is placed at 2716,9817 in maps/m42_153.jm2, and
//     content/scripts/areas/area_wilderness/scripts/king_black_dragon.rs2 puts the in-lever's landing at
//     0_42_153_29_10 = 2717,9802. (The owner's note placed the lair at about 2272,4700, mapsquare
//     35,73, the later-revision lair; nothing in the whole log touches that square.) The old classifier
//     called every one of these an "ordinary dungeon entrance" because the destination's mapsquareZ is
//     above 100. 224 deaths followed, a median of 68 minutes after arrival; 65 more exits were a Mime
//     or maze teleport; not one account walked out.
//   - 2026-10-02 04:35:01-04:35:33: 237 accounts, one each, landed on 3282,3936 LEVEL 3: the top
//     floor of Rogues' Castle in the Wilderness at level 53 (maps/m51_61.jm2; the staircase pair in
//     content/scripts/ladders+stairs/scripts/stairs.rs2 is 3280,3936 on levels 0 to 3). Swarm event
//     monsters stood beside them. 106 reached the stairs down (median 13.5 minutes after arrival, the
//     slowest an hour), 139 died and eight were still walking "to 3158,3950,0: unreachable" twelve
//     hours later.
//
// Neither is a stock macro event (macro_events_teleports.enum has neither), so both are treated as
// empirical, exactly like failteleport.go's own pockets: copied from what was measured, with the
// real-pack test (strandsites_realpack_test.go) as the proof the exits below exist.

// StrandTile is an absolute world tile.
type StrandTile struct{ X, Z, Level int }

// StrandRect is an inclusive rectangle of tiles on one level.
type StrandRect struct{ MinX, MaxX, MinZ, MaxZ, Level int }

// Contains reports whether the tile is inside the rectangle on its level.
func (r StrandRect) Contains(x, z, level int) bool {
	return level == r.Level && x >= r.MinX && x <= r.MaxX && z >= r.MinZ && z <= r.MaxZ
}

// StepKind says how an exit step is run.
type StepKind string

const (
	// StepLoc operates a loc (a staircase, a ladder, a lever) from a stand tile, and is done when
	// the account has changed level or moved MinMove tiles.
	StepLoc StepKind = "loc"
	// StepDoor opens a door from the stand tile and then walks onto Beyond, the first tile past it.
	StepDoor StepKind = "door"
)

// ExitStep is one thing to do to get from where an account stands toward the way out.
type ExitStep struct {
	Name string
	Kind StepKind
	// Where is the tiles this step applies to; a site's steps are tried in order and the first whose
	// Where holds the account's tile is the one to run.
	Where StrandRect
	// Stands are the tiles to operate the loc from, tried in turn: a failed click moves to the next.
	// Live runs keep finding loc fixtures that answer from one side only (webwalk's ApproachOverrides),
	// and the catalogued first stand is the one the transport graph plans from.
	Stands []StrandTile
	// LocID, LocX and LocZ are the loc's id and placement; Op is the 1-based option number for it,
	// and OpLabel the label the config gives that option (the real-pack test checks both).
	LocID, LocX, LocZ int
	Op                int
	OpLabel           string
	// Beyond is a door step's far side.
	Beyond StrandTile
	// BeyondRoom marks a far side that is another room of the same building, not open ground: the ordinary walker
	// finishes the way out through that room's own doors (the Wizards' Tower's east hall, a 24-tile room with a
	// door to the street). The real-pack test then expects a room-sized component that is not the stand's own.
	BeyondRoom bool
	// MinMove is how many tiles a lever's teleport carries the account at least.
	MinMove int
}

// StrandSite is a place accounts are dropped into by the server, with the way out of it.
type StrandSite struct {
	Name string
	// Why is the evidence, one line.
	Why string
	// Areas are the tiles that count as being in the site.
	Areas []StrandRect
	// Steps are the exits, walked in order from the account's own tile (see StepFor).
	Steps []ExitStep
	// Escape, when set, replaces Steps: the way out is not a fixed list of clicks but a plan, made on every
	// poll from the account's own tile and the doors as the client sees them (internal/doorescape), because
	// the way out depends on which doors are open and where their leaves have landed. See strandladder.go's
	// escape rung. A site with an Escape owns an account only where the catalogue says it cannot walk out
	// (Catalog.Interior) and escorts the rest after castleStayLimit.
	Escape func() (*doorescape.Catalog, error)
	// Lethal marks a site where something hostile stands beside the arrival tile, so the ladder
	// acts at once and eats while it does.
	Lethal bool
	// NoTeleport marks a site a teleport spell cannot leave: content refuses one above Wilderness
	// level 20 (skill_magic/scripts/spells/teleport.rs2).
	NoTeleport bool
	// ExitFirst marks a site whose catalogued exit costs nothing and leaves the account where its script wants it, so a
	// teleport spell waits until the exit has failed strandExitFirstClicks times. The Gnome course's own obstacles take
	// an account down in twenty seconds and leave it on the course; a Varrock teleport cost the runes and a long walk back.
	ExitFirst bool
	// Arrivals are the exact tiles the server has set accounts down on.
	Arrivals []StrandTile
	// Learned marks a site found at run time from a burst of unexplained arrivals (see massTracker),
	// which has no steps: only a teleport spell can be tried from one.
	Learned bool
	// VoidWalkTo, when set, is where the account walks in straight hops that ignore the collision
	// pack (VoidHop) once the catalogued exit has failed VoidWalkAfterClicks times, or at once when
	// the site has no steps. The server treats a mapsquare with no map file as open ground, while the
	// pack treats it as solid, so only a walk that trusts the server's moves can cross it.
	VoidWalkTo *StrandTile
	// MinStay, when set, is how long an account must have been inside the site before the ladder
	// acts: for a room a script uses on purpose (the Wizards' Tower ladder), only an account that
	// has stayed is stranded.
	MinStay time.Duration
	// PvP, when set, sends a stranded account to attack other stranded accounts until it dies and
	// respawns in Lumbridge (strand_pvp.go). An account in a PvP site south of the Wilderness finds
	// nobody it may fight, so it walks north to the rally like everyone else.
	PvP bool
	// NoWayHomeOnly marks a site scripts also use on purpose (a combat ladder rung): it counts only
	// for an account whose Guardian has just found no walk home from here (handle.go's drift check),
	// and holds that account for as long as it stays inside. catalogueSiteAt never returns one.
	NoWayHomeOnly bool
	// Boarding marks a ship deck: arriving on it is an ordinary boarding, so ClassifyDisplacement
	// does not report it as a strand site.
	Boarding bool
	// Except, when set, takes tiles back out of Areas: ground a rectangle cannot leave out, because the same rectangle holds
	// the stranded ground too (the Varrock sewers' floor lies inside the rectangle of the summon pocket, wall to wall
	// with it, and is a corridor scripts walk on purpose; see varrockSewerFloor).
	Except func(x, z, level int) bool
	// HandBack marks a site whose void or PvP walk toward its target can fail for a reason the walk cannot see (a door, a
	// web, a ladder between the account and the target, which the walker's own planner crosses and straight hops do not):
	// when the walk makes no net progress for voidFlatHops hops the Guardian stops walking and holding the account and runs
	// the ordinary escape ladder instead, which casts a teleport when the account can pay for one and otherwise gives the
	// account back to its script (strand_exec.go), instead of hopping into the same wall every three seconds for ever
	// (14 Oct 2026: 684 hops, 33 minutes).
	HandBack bool
}

// VoidWalkAfterClicks is how many failed exit clicks a site with VoidWalkTo allows before it walks.
const VoidWalkAfterClicks = 6

// Contains reports whether the tile is in the site.
func (s *StrandSite) Contains(x, z, level int) bool {
	for _, a := range s.Areas {
		if a.Contains(x, z, level) {
			return s.Except == nil || !s.Except(x, z, level)
		}
	}
	return false
}

// StepFor is the first exit step that applies to the tile, or nil when none does.
func (s *StrandSite) StepFor(x, z, level int) *ExitStep {
	for i := range s.Steps {
		if s.Steps[i].Where.Contains(x, z, level) {
			return &s.Steps[i]
		}
	}
	return nil
}

// Rogues' Castle. The roof is 3282,3936,3 and every floor of the castle is sealed from the outside except by
// the staircase down and, on the ground floor, doors. Measured on the real pack (the strandsites_realpack_test.go
// assertions): the level-3 room is 20 walkable tiles with ONE way out, "Staircase" 1740 at 3280,3936
// (Climb-down); level 2 is 18 tiles and the same stairs (1739, options Climb / Climb-up / Climb-down, so
// Climb-down is option 3); level 1 is 63 tiles and the same, with two doors into two more rooms; level 0 is the
// stairs room (15 tiles) behind a diagonal door (1530, Open) at 3279,3939 onto the open ground at 3279,3940,
// and a second diagonal door at 3282,3934 into the hall, and the hall, the range room, the east room, the east
// chamber, the west room and the two cells behind iron doors are all behind doors of their own. The TS
// transport graph knows the staircases, the straight doors and not the diagonal ones, which is why every
// planner said "unreachable" from the roof.
//
// What the first version of this entry did: one fixed click list (three staircases, then the main door from the
// tile 3279,3938) for a 15-tile room. In the log of the 6 October run it wrote 170 "left" lines for 194 arrivals:
// 54 without a failed click, the rest after one to four rounds of nine failed clicks and a sleep (a stand the open
// leaf has made solid cannot be walked to, so, as the model in strand_escape_test.go predicts, an account got out
// when the leaf had gone back by itself), and 9 of them at 3283,3934, the hall behind the stairs room's east door,
// which is still inside the walls and was in no site. Nothing worked on the accounts a door had shut in: the hall,
// the range room, the east room, the east chamber, the west room and the cells had no ladder at all, and an open
// leaf walls a room in (with the main door and the east chamber's south door open together, 83 tiles of the ground
// floor have no way out that shuts no door).
//
// So the castle's entry has no Steps. Its Escape plans the way out from the account's tile on every poll, over
// the doors as they are, and the Areas are the whole castle on every level.
var (
	// roguesCastleAreas are the castle's walls and the courtyards inside them on the ground (the main door's
	// doorway is 3279,3939; the open ground beyond it, 3279,3940, is out) and the floors above, which reach a
	// little further north.
	roguesCastleAreas = []StrandRect{
		{MinX: 3275, MaxX: 3298, MinZ: 3925, MaxZ: 3939, Level: 0},
		{MinX: 3275, MaxX: 3298, MinZ: 3925, MaxZ: 3941, Level: 1},
		{MinX: 3275, MaxX: 3298, MinZ: 3925, MaxZ: 3941, Level: 2},
		{MinX: 3275, MaxX: 3298, MinZ: 3925, MaxZ: 3941, Level: 3},
	}
)

// KBD lair. The lever is a wall decoration on its own tile (Lever 1817, Pull, at 2717,9801), operated
// from that tile exactly as the Ardougne and Deserted Keep levers are (scarabplan.ArdougneLever: a
// radius-1 stand answers "I can't reach that!"). It sets the account down at 0_47_160_59_14 =
// 3067,10254 (king_black_dragon.rs2's dragonkingoutlever), which is the 23-tile pocket below.
var (
	kbdLair = StrandRect{MinX: 2700, MaxX: 2735, MinZ: 9795, MaxZ: 9835, Level: 0}
	// gamesRoomFloor and gamesRoomGallery are the Burthorpe Games Room's map square (34_77), level 0 and 1.
	gamesRoomFloor   = StrandRect{MinX: 2176, MaxX: 2239, MinZ: 4928, MaxZ: 4991, Level: 0}
	gamesRoomGallery = StrandRect{MinX: 2176, MaxX: 2239, MinZ: 4928, MaxZ: 4991, Level: 1}
	kbdLeverTile     = StrandTile{X: 2717, Z: 9801, Level: 0}
	// kbdEntrance is the KBD lever's own pocket in the Wilderness dungeon (level 42): 23 tiles, whose
	// only way out is the Ladder (1766, Climb-up) at 3069,10256 up to 3016,3849 on the surface.
	kbdEntrance = StrandRect{MinX: 3064, MaxX: 3071, MinZ: 10252, MaxZ: 10261, Level: 0}
)

// The Gnome Stronghold agility course's upper floors. The course runs one way round
// (skill_agility/scripts/gnome_course.rs2) and the walk planner holds none of its obstacles, so an
// account whose laps stop up here cannot walk anywhere. gnome_course.loc and maps/m38_53.jm2: Tree
// branch 2313 (op1 Climb) at 2473,3422 on level 1, Balancing rope 2312 (op1 Walk-on) at 2478,3420 on
// level 2, Tree branch 2314 (op1 Climb-down) at 2486,3419 on level 2 (questplan.GnomeCourse).
var (
	gnomeCourseLevel1   = StrandRect{MinX: 2470, MaxX: 2477, MinZ: 3421, MaxZ: 3425, Level: 1}
	gnomeCourseRopeWest = StrandRect{MinX: 2470, MaxX: 2480, MinZ: 3416, MaxZ: 3423, Level: 2}
	gnomeCourseRopeEast = StrandRect{MinX: 2481, MaxX: 2490, MinZ: 3416, MaxZ: 3423, Level: 2}
)

// strandSites is the catalogue, in the order StrandSiteAt checks it.
var strandSites = []StrandSite{
	{
		// The agility35 quest rung runs Gnome course laps (questmods/gnome.go). A lap cut off up here (its
		// slot ended, a relaunch) left the account with no walk anywhere: on 6 Oct 2026 one stood on
		// 2475,3419,2 for over three hours, every walk "unreachable from here". MinStay leaves a lap in
		// progress to the laps (an obstacle takes seconds; a whole lap about a minute). The rung's laps end
		// on the ground and every quest walk takes an account down first (questmods.LeaveGnomeCourse), so
		// what reaches the Guardian is an account nothing else could take down: it finishes the course
		// forward by the same obstacles, and casts a teleport only after the exit has failed (ExitFirst).
		Name:      "the Gnome agility course's upper floors",
		Why:       "6 Oct 2026: an account left on 2475,3419,2 by an interrupted agility35 lap, 'unreachable from here' for 3 hours; the course's own obstacles are the only way down",
		Areas:     []StrandRect{gnomeCourseLevel1, gnomeCourseRopeWest, gnomeCourseRopeEast},
		MinStay:   3 * time.Minute,
		ExitFirst: true,
		Steps: []ExitStep{
			{
				Name: "climb the tree branch", Kind: StepLoc, Where: gnomeCourseLevel1,
				Stands: []StrandTile{{X: 2473, Z: 3423, Level: 1}},
				LocID:  2313, LocX: 2473, LocZ: 3422, Op: 1, OpLabel: "Climb", MinMove: 1,
			},
			{
				Name: "walk the balancing rope", Kind: StepLoc, Where: gnomeCourseRopeWest,
				Stands: []StrandTile{{X: 2477, Z: 3420, Level: 2}},
				LocID:  2312, LocX: 2478, LocZ: 3420, Op: 1, OpLabel: "Walk-on", MinMove: 3,
			},
			{
				Name: "climb down the tree branch", Kind: StepLoc, Where: gnomeCourseRopeEast,
				// West of the branch, orthogonally: the engine reaches a 1x1 centrepiece only from an
				// orthogonal neighbour, so the diagonal 2485,3420 (questplan.GnomeCourse's stand, which
				// the laps runner's own approach corrects) sent 36 refused clicks on bdd92f8d. A lap
				// climbed down from 2485,3419 at 04:20 on 6 Oct.
				Stands: []StrandTile{{X: 2485, Z: 3419, Level: 2}},
				LocID:  2314, LocX: 2486, LocZ: 3419, Op: 1, OpLabel: "Climb-down", MinMove: 1,
			},
		},
		Arrivals: []StrandTile{{X: 2475, Z: 3419, Level: 2}},
	},

	{
		// A ship deck the collision bake treats as an isolated level-1 pocket: an account that
		// logs in or relogs on it never gets the sailing code's own gangplank step. MinStay leaves
		// an ordinary landing to that code.
		Name:     "the Brimhaven ship deck",
		Why:      "3 Oct 18:01-18:36: moderator stood on the deck at 2776,3235,1 for 35 minutes, every walk 'no path'; gangplank ardougneshipplank_off (2088) at 2774,3234, op1 Cross (content gangplank.loc)",
		Areas:    []StrandRect{{MinX: 2775, MaxX: 2785, MinZ: 3228, MaxZ: 3240, Level: 1}},
		MinStay:  2 * time.Minute,
		Boarding: true,
		Steps: []ExitStep{{
			Name: "cross the gangplank", Kind: StepLoc, Where: StrandRect{MinX: 2775, MaxX: 2785, MinZ: 3228, MaxZ: 3240, Level: 1},
			Stands: []StrandTile{{X: 2775, Z: 3234, Level: 1}},
			LocID:  2088, LocX: 2774, LocZ: 3234, Op: 1, OpLabel: "Cross", MinMove: 1,
		}},
	},
	{
		// A ship deck the collision bake treats as an isolated level-1 pocket: an account that
		// logs in or relogs on it never gets the sailing code's own gangplank step. MinStay leaves
		// an ordinary landing to that code.
		Name:     "the Ardougne ship deck",
		Why:      "the deck p_telejump sets a Brimhaven -> Ardougne passenger on (freshstart.ArdougneArrival 2683,3268,1); gangplank brimhavenshipplank_off (2086) at 2683,3269, op1 Cross",
		Areas:    []StrandRect{{MinX: 2676, MaxX: 2690, MinZ: 3260, MaxZ: 3268, Level: 1}},
		MinStay:  2 * time.Minute,
		Boarding: true,
		Steps: []ExitStep{{
			Name: "cross the gangplank", Kind: StepLoc, Where: StrandRect{MinX: 2676, MaxX: 2690, MinZ: 3260, MaxZ: 3268, Level: 1},
			Stands: []StrandTile{{X: 2683, Z: 3268, Level: 1}},
			LocID:  2086, LocX: 2683, LocZ: 3269, Op: 1, OpLabel: "Cross", MinMove: 1,
		}},
	},
	{
		// A ship deck the collision bake treats as an isolated level-1 pocket: an account that
		// logs in or relogs on it never gets the sailing code's own gangplank step. MinStay leaves
		// an ordinary landing to that code.
		Name:     "the Musa Point ship deck",
		Why:      "the 48-tile deck at 2951-2963,3139-3144,1 (freshstart.KaramjaShipDeck); gangplank sarimshipplank_off (2082) at 2956,3144, op1 Cross from the orthogonal 2956,3143",
		Areas:    []StrandRect{{MinX: 2951, MaxX: 2963, MinZ: 3139, MaxZ: 3143, Level: 1}},
		MinStay:  2 * time.Minute,
		Boarding: true,
		Steps: []ExitStep{{
			Name: "cross the gangplank", Kind: StepLoc, Where: StrandRect{MinX: 2951, MaxX: 2963, MinZ: 3139, MaxZ: 3143, Level: 1},
			Stands: []StrandTile{{X: 2956, Z: 3143, Level: 1}},
			LocID:  2082, LocX: 2956, LocZ: 3144, Op: 1, OpLabel: "Cross", MinMove: 1,
		}},
	},
	{
		// Content's way out: the level-0 room's stairs (board_game_stairs_base, 4622 at 2207,4935) up to the
		// gallery, then the grey stairs (board_game_stairs_grey_base2, 4627 at 2205,4935,1) to Burthorpe
		// at 2899,3564. The walk graph has neither edge.
		Name:  "the Burthorpe Games Room",
		Why:   "7 Oct 2026 02:24: a Drunken Dwarf handler clicked 'Burthorpe Games Rooms.' on the run staller's Games necklace menu; Communicate and acct16 landed at 2211,4936 and every walk was unreachable (best 1653)",
		Areas: []StrandRect{gamesRoomFloor, gamesRoomGallery},
		Steps: []ExitStep{
			{
				Name: "climb the stairs to the gallery", Kind: StepLoc, Where: gamesRoomFloor,
				Stands: []StrandTile{{X: 2207, Z: 4938}, {X: 2208, Z: 4938}, {X: 2207, Z: 4934}, {X: 2208, Z: 4934}},
				LocID:  4622, LocX: 2207, LocZ: 4935, Op: 1, OpLabel: "Climb-up", MinMove: 1,
			},
			{
				Name: "climb the grey stairs to Burthorpe", Kind: StepLoc, Where: gamesRoomGallery,
				Stands: []StrandTile{{X: 2205, Z: 4934, Level: 1}, {X: 2206, Z: 4934, Level: 1}, {X: 2205, Z: 4938, Level: 1}, {X: 2206, Z: 4938, Level: 1}},
				LocID:  4627, LocX: 2205, LocZ: 4935, Op: 1, OpLabel: "Climb-up", MinMove: 100,
			},
		},
	},
	{
		Name:  "King Black Dragon's lair",
		Why:   "mass summon 2026-10-01 21:07, 224 accounts to 2718,9816 in five seconds; 224 deaths, no walk out; maps/m42_153.jm2 places the dragon at 2716,9817",
		Areas: []StrandRect{kbdLair},
		Steps: []ExitStep{{
			Name: "pull the lever out of the lair", Kind: StepLoc, Where: kbdLair,
			Stands: []StrandTile{kbdLeverTile},
			LocID:  1817, LocX: 2717, LocZ: 9801, Op: 1, OpLabel: "Pull", MinMove: 100,
		}},
		Lethal: true,
		Arrivals: []StrandTile{
			{X: 2718, Z: 9816}, {X: 2719, Z: 9816}, {X: 2718, Z: 9817}, {X: 2717, Z: 9802}, {X: 2710, Z: 9807}, {X: 2710, Z: 9808},
		},
	},
	{
		Name:       "the KBD lever's pocket in the Wilderness dungeon",
		Why:        "where the lair's own lever sets an account down (dragonkingoutlever, 0_47_160_59_14): 23 tiles and one ladder",
		Areas:      []StrandRect{kbdEntrance},
		NoTeleport: true,
		Steps: []ExitStep{{
			Name: "climb the ladder to the surface", Kind: StepLoc, Where: kbdEntrance,
			Stands: []StrandTile{{X: 3069, Z: 10257, Level: 0}, {X: 3068, Z: 10256, Level: 0}},
			LocID:  1766, LocX: 3069, LocZ: 10256, Op: 1, OpLabel: "Climb-up", MinMove: 100,
		}},
		Arrivals: []StrandTile{{X: 3067, Z: 10254}},
	},
	{
		Name:       "Rogues' Castle (the roof and its floors, deep Wilderness)",
		Why:        "mass summons 2026-10-02 04:35 (237 accounts to 3282,3936 level 3: 139 died, eight still stuck twelve hours later) and 2026-10-06 01:32 (356 accounts to the same tiles: seventeen still inside five hours on, most at the doors of the rooms below)",
		Areas:      roguesCastleAreas,
		NoTeleport: true, // Wilderness level 53
		// Swarm event monsters stood beside the arrivals ("event monster — cornered at 3282,3936,3 with
		// no escape and npc 15892 type 411 'Swarm' attacking"), and 139 of the 245 arrivals died.
		Lethal:   true,
		Escape:   doorescape.Castle,
		Arrivals: []StrandTile{{X: 3282, Z: 3936, Level: 3}, {X: 3282, Z: 3937, Level: 3}, {X: 3281, Z: 3937, Level: 3}, {X: 3281, Z: 3936, Level: 3}},
	},
	{
		Name:  "the gated Wilderness dungeon section below 3040,3936",
		Why:   "mass summon 2026-10-03 00:13, 272 accounts to 3040,10336; the real pack's walkable component around the arrival reaches no ladder, lever or gate stand, only Staircase 1734 at 3044,10324 (stands 3043-3046,10327)",
		Areas: []StrandRect{wildDungeonSection},
		// Wilderness level 52: content refuses a teleport spell there.
		NoTeleport: true,
		Steps: []ExitStep{{
			Name: "climb the staircase to the surface", Kind: StepLoc, Where: wildDungeonSection,
			Stands: []StrandTile{{X: 3044, Z: 10327, Level: 0}, {X: 3045, Z: 10327, Level: 0}, {X: 3043, Z: 10327, Level: 0}, {X: 3046, Z: 10327, Level: 0}},
			LocID:  1734, LocX: 3044, LocZ: 10324, Op: 1, OpLabel: "Climb-up", MinMove: 100,
		}},
		Arrivals: []StrandTile{{X: 3040, Z: 10336}},
		PvP:      true,
		// Live 3 Oct 01:30: 572 Climb-up clicks on the staircase moved nobody, while accounts that
		// wandered reached 3079-3097,10268-10288 in mapsquare 48,160, which has no map file at all.
		VoidWalkTo: &voidWalkTarget,
	},
	{
		Name:       "the void below the Wilderness (mapsquares with no map file)",
		Why:        "3 Oct: accounts summoned to 3040,10336 reached 48,160, a square the pack holds no tiles for; the server lets them walk there, the planner calls it solid",
		Areas:      voidBelowWilderness,
		NoTeleport: true,
		VoidWalkTo: &voidWalkTarget,
		PvP:        true,
	},
	{
		// Fail-teleport escapees land here, and the walker cannot cross the room's diagonal door: open,
		// it blocks the stand tile 3106,3162, so every step "is blocked live" (3 Oct 08:50, 11 accounts,
		// 1,254 stalls in 30 minutes). Rune Mysteries uses the room's ladder on purpose, so the ladder
		// waits MinStay before treating an account as stranded. MinStay is 45 s, not minutes: an
		// account going down crosses the room to the ladder in about 10 s, but every account coming
		// UP from Sedridor lands here, and with 5 minutes its walk to Duke Horacio timed out in the
		// room before the exit ran, so the quest never advanced (3 Oct 22:31, sike: stuck 12 minutes
		// over two trips; the exit itself took 16-22 s).
		Name:    "the Wizards' Tower west room",
		Why:     "3 Oct 08:50: 11 accounts stalled at 3106,3161 beside Door 1536 (diagonal, 3107,3162), the only way out of a 15-tile room",
		Areas:   []StrandRect{wizardsWestRoom},
		MinStay: 45 * time.Second,
		Steps: []ExitStep{{
			Name: "open the diagonal door and step out", Kind: StepDoor, Where: wizardsWestRoom,
			// 3107,3161 first: the door's leaf lands on 3106,3162 and the server sets an account standing there
			// on 3106,3161 (webwalk's wizardsDoorWhy), so opening from 3107,3161 pushes nobody and is the stand the
			// walker's own edge uses. The other two answered live (7 Oct 2026: left the room after 13-14 s with one
			// failed click) and stay as the fallbacks.
			Stands: []StrandTile{{X: 3107, Z: 3161, Level: 0}, {X: 3106, Z: 3162, Level: 0}, {X: 3106, Z: 3161, Level: 0}},
			LocID:  1536, LocX: 3107, LocZ: 3162, Op: 1, OpLabel: "Open",
			Beyond: StrandTile{X: 3108, Z: 3162, Level: 0}, BeyondRoom: true,
		}},
		VoidWalkTo: &StrandTile{X: 3109, Z: 3162, Level: 0},
	},
	{
		// The arrival component reaches a ladder only through doors (1530 at 3247,9892 to the sewer
		// ladder at 3237,9858), which nothing here has proven live, so the account walks north for the
		// PvP rally (owner, 3 Oct 12:30: "all accounts should walk to acct111 and fight there deep in
		// the wilderness") while its own Genie or Old Man is left to fail-teleport it out. The box
		// stops short of the moss giants rung at 3159,9900.
		//
		// The box is not all pocket (14 Oct 2026, build d5362bb2, 04:05-04:45): of its 910 walkable tiles 329 are the
		// Varrock sewers' own floor, the corridor of the route to the moss giants (the manhole, three doors, the north
		// cavern, the web at 3210,9898 and the ground beyond it) that the walker crosses on purpose, wall to wall with
		// the summon's 53,000-tile ground and joined to it by nothing. Three accounts were held there as "stranded": one
		// resumed in the cavern at login and was held for 33 minutes (684 void hops toward a rally it could not reach), two
		// passing through were taken to the PvP world and walked toward the rally. The first fix took the 217-tile cavern
		// out and left the rest of the corridor in: the three tiles 3210,9895-9897, the first the walk reaches after the
		// web, still read as the pocket, so every account that finished the cavern was caught on its next poll. Except now
		// takes out every walkable tile of the box that is not the arrival's own ground (varrockSewerFloor, regenerated from
		// the pack by a test); HandBack bounds the walk for whatever else is found on the summon's ground that the walk
		// cannot cross.
		Name:       "the underground pocket below Varrock (3217,9910 summons)",
		Why:        "3 Oct 07:48: 15 accounts at 3217,9910 (mapsquare 50,154), a 9,796-tile area whose ladders (3230,9904 and 3237,9858) are behind doors; outside every site, so they solved their Genie/Old Man instead of being fail-teleported out",
		Areas:      []StrandRect{{MinX: 3200, MaxX: 3239, MinZ: 9895, MaxZ: 9930, Level: 0}},
		Except:     onVarrockSewerFloor,
		VoidWalkTo: &StrandTile{X: 3217, Z: 9910, Level: 0},
		PvP:        true,
		HandBack:   true,
	},
	{
		// The dungeons just south of the underground Wilderness (Edgeville's and the west Varrock
		// sewers) hold the moss giants rung, so only an account with no walk home counts as stranded.
		Name: "the dungeons south of the underground Wilderness (no walk home)",
		Why:  "3 Oct 12:20: 8 ProgressiveFishers at 3143-3183,9901-9919 (Wilderness level 0, outside every site) searched for a path to Draynor about once a second each, 25% of the fleet's CPU",
		// x to 3239 since 3 Oct 14:30: three accounts waited at 3206,9891 for a Man to pickpocket.
		Areas:         []StrandRect{{MinX: 2944, MaxX: 3239, MinZ: 9856, MaxZ: 9919, Level: 0}},
		Except:        onVarrockSewerFloor,
		VoidWalkTo:    &voidWalkTarget,
		PvP:           true,
		NoWayHomeOnly: true,
	},
	{
		// Checked last: every catalogued site above takes precedence. Nothing any script does goes into
		// the underground Wilderness except the KBD lair's lever pocket (its own site above), so an
		// account down here was put here. Its Genie or Old Man is left alone (guardian.go) so the
		// fail-teleport carries it out, and the void walk keeps it moving toward real map.
		Name:       "the underground Wilderness (catch-all)",
		Why:        "3 Oct 07:30: 51 accounts reached real map at 3100,10040 (48,156) and 18 sat at 3064,10321, outside every site, so they solved their Genie/Old Man instead of being fail-teleported out (0 escapes in 30 minutes)",
		Areas:      []StrandRect{{MinX: 2944, MaxX: 3391, MinZ: 9920, MaxZ: 10431, Level: 0}},
		VoidWalkTo: &voidWalkTarget,
		PvP:        true,
	},
}

// varrockSewerFloor is the Varrock sewers' own floor inside the pocket's box (x 3200-3239, z 9895-9930), tile by tile: the 329 walkable
// tiles the real pack does NOT join to the summon's arrival at 3217,9910, as {first x, last x} spans by row. The box holds two kinds of
// floor wall to wall, and only the arrival's own ground (a 53,405-tile network that reaches the PvP rally on foot) is the pocket. The rest
// is the corridor of ScarabSwarm's moss-giants route, which the walker crosses on purpose:
//
//	the manhole 3237,3458 -> 3237,9859, the door 3247,9892, the door 3245,9916, the door 3241,9910 (3241,9911 outside, 3241,9910 in),
//	the cavern, the web 3210,9898 (Slash; the walk graph's door edge 3210,9897 <-> 3210,9899), then the moss giants at 3159,9900.
//
// By piece of floor (the pack's own exit bits, doors and webs not crossed; tiles in the box of each): the north cavern, 217 tiles (210);
// the moss giants' ground beyond the web, 552 tiles (3: 3210,9895-9897, the first tiles the walk reaches after the web); the manhole
// ladder's piece, 599 tiles (36); a piece of 411 tiles (67); the 41-tile room behind the door 3245,9916 (6); and two fragments of 6 tiles (6)
// and 1 tile (1). None of them joins the arrival's ground, and nothing walks in either direction between them and it (the walker's graph
// says "unreachable" both ways), so an account on them was never put there by the summon.
//
// The cavern's other exit, the ladder at 3230,9904, comes up at 3230,3505 in a 33-tile pocket walled on every side by the unnamed loc 1902
// (shapes 0, 1 and 9 in maps/m50_54.jm2, an invisible wall the pack holds as solid): nothing is gained by taking it, so the catalogue gives
// the pocket no exit step. TestVarrockSewerFloorAgainstTheRealPack regenerates this table from the pack.
var varrockSewerFloor = map[int][][2]int{
	9895: {{3210, 3210}, {3231, 3239}},
	9896: {{3210, 3210}, {3227, 3227}, {3231, 3239}},
	9897: {{3210, 3210}, {3225, 3239}},
	9898: {{3222, 3239}},
	9899: {{3210, 3210}, {3222, 3239}},
	9900: {{3210, 3210}, {3228, 3239}},
	9901: {{3210, 3210}, {3231, 3239}},
	9902: {{3209, 3211}, {3225, 3227}, {3234, 3239}},
	9903: {{3208, 3213}, {3225, 3230}, {3234, 3239}},
	9904: {{3206, 3214}, {3222, 3229}, {3231, 3232}, {3236, 3239}},
	9905: {{3205, 3215}, {3221, 3239}},
	9906: {{3202, 3215}, {3221, 3239}},
	9907: {{3202, 3217}, {3219, 3239}},
	9908: {{3203, 3237}},
	9909: {{3202, 3205}, {3208, 3215}, {3221, 3228}},
	9910: {{3203, 3205}, {3209, 3214}, {3222, 3228}},
	9911: {{3202, 3202}, {3204, 3204}},
	9913: {{3239, 3239}},
	9914: {{3238, 3239}},
	9915: {{3238, 3239}},
	9916: {{3239, 3239}},
}

// onVarrockSewerFloor reports whether the tile is one of varrockSewerFloor's.
func onVarrockSewerFloor(x, z, level int) bool {
	if level != 0 {
		return false
	}
	if x == varrockSewerWebX && z == varrockSewerWebZ {
		return true
	}
	for _, span := range varrockSewerFloor[z] {
		if x >= span[0] && x <= span[1] {
			return true
		}
	}
	return false
}

// varrockSewerWebX/Z is the web's own tile on the moss-giants route. The baked pack blocks it (the web stands there until it is
// slashed), so varrockSewerFloor, read off the pack, leaves it out; but an account that slashed the web stands on it on its way
// through. Live 14 Oct 2026 (build cf956332): one account at 3210,9898 logged 70 strand lines in 5 minutes and was hopped to the
// PvP world.
const varrockSewerWebX, varrockSewerWebZ = 3210, 9898

// wizardsWestRoom is the 15 tiles the pack connects to 3106,3161: x 3103-3107, z 3159-3163.
var wizardsWestRoom = StrandRect{MinX: 3103, MaxX: 3107, MinZ: 3159, MaxZ: 3163, Level: 0}

// voidWalkTarget is in mapsquare 48,156, the first square south of the void with real map data (4,011
// walkable tiles on the pack), from where the ordinary walker can plan through the dungeon's doors.
var voidWalkTarget = StrandTile{X: 3100, Z: 10040, Level: 0}

// voidBelowWilderness are the squares between the Wilderness dungeon and the Edgeville dungeon that the
// pack holds no tiles for (46-49,157-159; 48-49,160-161; 46,160): the server's open ground.
var voidBelowWilderness = []StrandRect{
	{MinX: 46 * 64, MaxX: 50*64 - 1, MinZ: 157 * 64, MaxZ: 160*64 - 1, Level: 0},
	{MinX: 48 * 64, MaxX: 50*64 - 1, MinZ: 160 * 64, MaxZ: 162*64 - 1, Level: 0},
	{MinX: 46 * 64, MaxX: 47*64 - 1, MinZ: 160 * 64, MaxZ: 161*64 - 1, Level: 0},
}

// VoidHop is the next straight hop toward target from cur, at most voidHopMax tiles on each axis.
// stuck counts the hops in a row that did not move the account; each one turns the heading further
// from the direct line (45 then 90 degrees either side, then back the way it came) so a wall the
// server does have is walked around rather than pushed against forever.
func VoidHop(curX, curZ, targetX, targetZ, stuck int) (int, int) {
	dx, dz := clampHop(targetX-curX), clampHop(targetZ-curZ)
	if dx == 0 && dz == 0 {
		return curX, curZ
	}
	// Unit heading, then rotate by 45-degree steps: 0, +1, -1, +2, -2, +4.
	ux, uz := sgn(dx), sgn(dz)
	turns := []int{0, 1, -1, 2, -2, 4}
	t := turns[stuck%len(turns)]
	for i := 0; i < (t+8)%8; i++ {
		ux, uz = rotate45(ux, uz)
	}
	if t == 0 {
		return curX + dx, curZ + dz
	}
	return curX + ux*voidHopMax, curZ + uz*voidHopMax
}

const voidHopMax = 10

func clampHop(v int) int { return max(-voidHopMax, min(voidHopMax, v)) }

func sgn(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// rotate45 turns a unit 8-direction heading 45 degrees anticlockwise.
func rotate45(x, z int) (int, int) {
	dirs := [8][2]int{{1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1}}
	for i, d := range dirs {
		if d[0] == x && d[1] == z {
			n := dirs[(i+1)%8]
			return n[0], n[1]
		}
	}
	return x, z
}

// wildDungeonSection is the walled section of the Wilderness dungeon the 3 Oct summon used: the
// arrival 3040,10336 and the staircase both lie inside it, the gates at 3044,10341, 3040,10307 and
// 3022,10311 and the webs bound it. It stops short of the KBD lever's pocket (x 3064+).
// Widened 3 Oct 07:30 to the whole square 47,161: accounts that left the first box at x 3061-3071
// were in no site, so nothing walked them on and their Genie or Old Man was solved, not ignored.
var wildDungeonSection = StrandRect{MinX: 47 * 64, MaxX: 48*64 - 1, MinZ: 161 * 64, MaxZ: 162*64 - 1, Level: 0}

// catalogueSiteAt is the catalogued site the tile is in, never a learned one: ClassifyDisplacement is
// pure, and what the watcher has learned at run time is not an input to it.
func catalogueSiteAt(x, z, level int) (StrandSite, bool) {
	for i := range strandSites {
		if !strandSites[i].NoWayHomeOnly && strandSites[i].Contains(x, z, level) {
			return strandSites[i], true
		}
	}
	return StrandSite{}, false
}

// StrandSiteAt is the catalogued (or learned) site the tile is in. The returned site is a copy.
func StrandSiteAt(x, z, level int) (StrandSite, bool) {
	if s, ok := catalogueSiteAt(x, z, level); ok {
		return s, true
	}
	return learnedSiteAt(x, z, level, time.Now())
}

// mageBankPocket is the Mage Bank's own mapsquare (39,73): not a place the server sends anyone, but a
// pocket all the same, since its only way out is the ladder up into the Wilderness (scarabplan's trip
// leaves by it and the Deserted Keep lever). It is not in strandSites: ScarabSwarm's own Mage Bank
// trip is the one thing that legitimately stands there, and the Guardian's ladder must never take it
// off an account mid-trip.
var mageBankPocket = StrandRect{MinX: 2496, MaxX: 2559, MinZ: 4672, MaxZ: 4735, Level: 0}

// siteOwns reports whether the Guardian's ladder owns the way out of this tile of the site: every tile of a
// site with exit steps, and, for a site with an Escape, the tiles that are rooms, doorways and floors (the
// catalogue's Interior) but not the open ground inside its walls, which an account can walk out of by itself.
func siteOwns(s *StrandSite, x, z, level int) bool {
	if s.Escape != nil {
		cat, err := s.Escape()
		return err == nil && cat.Interior(x, z, level)
	}
	return len(s.Steps) > 0
}

// PocketAt reports whether the tile is in a place whose way out leads INTO the Wilderness danger zone
// (a strand site or the Mage Bank), and names it. The Walker plans from one with the automatic zones
// off: the zone rule refuses every hop into a zone from outside it, which made "unreachable" the
// only answer from the Mage Bank ("travel to 2747,3577,0: unreachable (best 1135 tiles)", three accounts
// for twelve hours) and from the dragon's lair, whose lever lands in the Wilderness dungeon.
func PocketAt(x, z, level int) (string, bool) {
	if s, ok := catalogueSiteAt(x, z, level); ok && (s.Escape == nil || siteOwns(&s, x, z, level)) {
		return s.Name, true
	}
	if mageBankPocket.Contains(x, z, level) {
		return "the Mage Bank (its only way out is the ladder up into the Wilderness)", true
	}
	return "", false
}

// StrandOwner reports whether the tile is in a catalogued site whose way out the Guardian's escape ladder is
// working (a site with exit steps or an escape plan, and, for the latter, a tile that is not open ground): a
// walk that finds no path from there need not spend half a minute on the walker's own scene, unstick and
// backoff passes before saying so.
func StrandOwner(x, z, level int) (string, bool) {
	if s, ok := catalogueSiteAt(x, z, level); ok && siteOwns(&s, x, z, level) {
		return s.Name, true
	}
	return "", false
}

// StrandHoldsWalks is StrandOwner for the Walker: whether a walk from the tile is the Guardian's to refuse. It is every
// StrandOwner site except a ship deck (Boarding). A deck is where a sailing leaves the account on purpose, and its
// gangplank is an ordinary Cross hop in the walk graph, so a walk from it plans the way off at once; the Guardian's
// entry for a deck is only the backstop for an account that logged in on it (MinStay), never a reason to turn a
// script's walk away. The Walker used to treat the deck like Rogues' Castle: a ferry that landed a moment after its
// hop gave up, or a walk whose deadline the crossing used up, ended on the deck as "stranded", every walk from there
// was refused for five minutes, and a quest that asked three times parked for twenty (16 Oct 2026: 33 quest parks in the
// eight hours to 09:02 and 20 more by 10:37, every one on the Brimhaven to Ardougne deck at 2683,3268,1, whose planned
// way off was found 334 times in the logs to 09:40).
func StrandHoldsWalks(x, z, level int) (string, bool) {
	if s, ok := catalogueSiteAt(x, z, level); ok && !s.Boarding && siteOwns(&s, x, z, level) {
		return s.Name, true
	}
	return "", false
}

// StrandPlanned reports whether the tile is one whose way out the Guardian PLANS (a site with an Escape, and a tile
// that is not open ground): no walk of a script's own can do better from there, so the Walker refuses to start one
// (it used to try, fail on a diagonal door and try again).
func StrandPlanned(x, z, level int) (string, bool) {
	if s, ok := catalogueSiteAt(x, z, level); ok && s.Escape != nil && siteOwns(&s, x, z, level) {
		return s.Name, true
	}
	return "", false
}

// StrandCompoundAt reports whether the tile is inside the walls of a site whose way out the Guardian plans, its
// open courtyards included. Every door, staircase and wall in there is the Guardian's: a script's own search for a
// door to open or a stile to climb must keep out of it, because from open ground nearby (the account's home is
// "unreachable" on the raw pack from anywhere in the deep Wilderness) it picks the castle's doors, walks into the
// courtyards and shuts itself in again (6 October 2026: ProgressiveCrafter and ProgressiveWCFletch accounts for
// two hours).
func StrandCompoundAt(x, z, level int) (string, bool) {
	if s, ok := catalogueSiteAt(x, z, level); ok && s.Escape != nil {
		return s.Name, true
	}
	return "", false
}

// ---- fixtures -------------------------------------------------------------------------------

// Fixture is a move the fleet's own scripts make on purpose, and which the displacement watcher
// used to log as UNEXPLAINED because nothing named it. From and To are tile boxes (the radius
// absorbs the few tiles a click lands on); a zero box matches anywhere.
type Fixture struct {
	Name     string
	From, To fixtureBox
	// FromTrap matches a source inside the Mime or maze mapsquare, whatever the destination.
	FromTrap bool
}

type fixtureBox struct {
	X, Z, Radius int
	any          bool
}

func box(x, z, r int) fixtureBox { return fixtureBox{X: x, Z: z, Radius: r} }

func (b fixtureBox) has(x, z int) bool {
	return b.any || chebyshev(x, z, b.X, b.Z) <= b.Radius
}

var anywhere = fixtureBox{any: true}

// fixtures were read off the 3,722 UNEXPLAINED lines: 3,500 of them (94%) were these moves, each
// the bot's own planned step, and only the mass summons above were not.
var fixtures = []Fixture{
	{Name: "the Ardougne lever up to the Deserted Keep (ScarabSwarm's Mage Bank trip)", From: box(2561, 3311, 2), To: box(3154, 3924, 3)},
	{Name: "the Deserted Keep lever back to Ardougne (ScarabSwarm's Mage Bank trip)", From: box(3153, 3923, 2), To: box(2562, 3311, 3)},
	{Name: "the Mage Bank ladder down (ScarabSwarm's Mage Bank trip)", From: box(3092, 3957, 4), To: box(2543, 4713, 4)},
	{Name: "the Mage Bank ladder up (ScarabSwarm's Mage Bank trip)", From: box(2543, 4713, 4), To: box(3092, 3957, 4)},
	{Name: "the Waterfall dungeon door back to the ledge", From: box(2575, 9861, 3), To: box(2511, 3463, 4)},
	{Name: "the Waterfall washout", From: anywhere, To: box(2527, 3413, 3)},
	{Name: "the end of a Mime or maze event, which sends the account back", FromTrap: true, To: anywhere},
	// The Fishing Trawler (game_trawler, 2 Nov 2026): the whole lobby is taken aboard at once, which the watcher learned as
	// a mass summon and walked the crew away from the leaks. The boat (mapsquare 29,75), its flooded copy (31,75, +128 x)
	// and the wreck (30,75) are the content's own moves, as is the barrel's swim to Port Sarim.
	{Name: "the Fishing Trawler sailing from the Port Khazard lobby", From: box(2672, 3170, 4), To: trawlerBoat},
	{Name: "the Fishing Trawler flooding or drying out", From: trawlerBoats, To: trawlerBoats},
	{Name: "the Fishing Trawler sinking", From: trawlerBoats, To: trawlerWreck},
	{Name: "the Fishing Trawler's barrel to Port Sarim", From: trawlerWreck, To: box(3026, 3156, 3)},
	{Name: "the Fishing Trawler's end of game back to Port Khazard", From: trawlerBoats, To: box(2666, 3167, 8)},
}

// The Fishing Trawler's mapsquares: the boat 29,75, the wreck 30,75 and the flooded boat 31,75 (one box spans all three).
var (
	trawlerBoat  = box(1888, 4832, 32)
	trawlerWreck = box(1952, 4832, 32)
	trawlerBoats = fixtureBox{X: 1952, Z: 4832, Radius: 96}
)

// KnownFixture names a planned move the fleet's own scripts make, or reports false.
func KnownFixture(fromX, fromZ, toX, toZ int) (string, bool) {
	for _, f := range fixtures {
		if f.FromTrap {
			// Leaving: a jump from one trap square to another (or within one) is not the end of the event.
			if InTrapEventSquare(fromX, fromZ) && !InTrapEventSquare(toX, toZ) {
				return f.Name, true
			}
			continue
		}
		if f.From.has(fromX, fromZ) && f.To.has(toX, toZ) {
			return f.Name, true
		}
	}
	return "", false
}

// ---- learned mass sites ---------------------------------------------------------------------

// A server that has summoned a cohort twice in a day will summon it somewhere else a third time, so
// the watcher also learns a site from the burst itself: massThreshold distinct accounts arriving
// unexplained in one mapsquare and level within massWindow. The site is remembered for massTTL. It has
// no exit steps (nothing is known about it), so only a teleport spell is tried from it, and the log says
// so once, loudly, which is what turned out to be missing: the first two events were each noticed
// only by reading 3,722 lines afterwards.
const (
	massThreshold = 12
	massWindow    = 90 * time.Second
	massTTL       = 12 * time.Hour
)

type massKey struct{ mx, mz, level int }

type massArrival struct {
	who any
	at  time.Time
}

type massTracker struct {
	mu      sync.Mutex
	arrived map[massKey][]massArrival
	learned map[massKey]time.Time // when each learned site expires
	names   map[massKey]string
}

var mass = newMassTracker()

func newMassTracker() *massTracker {
	return &massTracker{arrived: map[massKey][]massArrival{}, learned: map[massKey]time.Time{}, names: map[massKey]string{}}
}

// note records one unexplained arrival by who (any comparable identity: the account's session) at
// the tile and reports how many distinct accounts the square has had within the window, and whether
// this arrival is the one that made it a mass site (true exactly once per learned site).
func (m *massTracker) note(now time.Time, who any, x, z, level int) (count int, learnedNow bool) {
	key := massKey{x >> 6, z >> 6, level}
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.arrived[key][:0]
	for _, a := range m.arrived[key] {
		if now.Sub(a.at) <= massWindow && a.who != who {
			list = append(list, a)
		}
	}
	list = append(list, massArrival{who: who, at: now})
	m.arrived[key] = list
	count = len(list)
	if count >= massThreshold {
		if until, ok := m.learned[key]; !ok || !now.Before(until) {
			m.learned[key] = now.Add(massTTL)
			m.names[key] = fmt.Sprintf("a mass summon site at mapsquare %d,%d level %d (learned: %d accounts arrived within %s)", key.mx, key.mz, key.level, count, massWindow)
			learnedNow = true
		}
	}
	return count, learnedNow
}

func learnedSiteAt(x, z, level int, now time.Time) (StrandSite, bool) {
	return mass.siteAt(x, z, level, now)
}

func (m *massTracker) siteAt(x, z, level int, now time.Time) (StrandSite, bool) {
	key := massKey{x >> 6, z >> 6, level}
	m.mu.Lock()
	defer m.mu.Unlock()
	until, ok := m.learned[key]
	if !ok || !now.Before(until) {
		return StrandSite{}, false
	}
	return StrandSite{
		Name:    m.names[key],
		Why:     "a burst of unexplained arrivals; nothing else is known about it",
		Areas:   []StrandRect{{MinX: key.mx << 6, MaxX: key.mx<<6 + 63, MinZ: key.mz << 6, MaxZ: key.mz<<6 + 63, Level: level}},
		Learned: true,
	}, true
}

// LearnedMassSites lists the learned sites still remembered, for the log and the dashboard.
func LearnedMassSites() []string {
	now := time.Now()
	mass.mu.Lock()
	defer mass.mu.Unlock()
	var out []string
	for k, until := range mass.learned {
		if now.Before(until) {
			out = append(out, mass.names[k])
		}
	}
	sort.Strings(out)
	return out
}
