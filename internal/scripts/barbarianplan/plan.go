// Package barbarianplan is the Barbarian Outpost agility course, decided from plain records: the
// lap order and which op each obstacle wants, where the course is, the starting side of each
// obstacle, what a finished attempt meant, and when the pack is out of food. Ported from
// rsb0t-scripts' barbarianPlan.ts.
//
// It is pure: no client, actions, fleet or bot imports and no I/O. Obstacles are named strings,
// resolved to a live loc by whatever finds locs by name near a tile (a static loc index, once one
// exists — see COLLAB.md).
package barbarianplan

import (
	"math/rand"
	"regexp"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// CourseMinAgility is the level the outpost's gate guard checks for.
const CourseMinAgility = 35

// DefaultLap is the lap inside the enclosure, as the outpost lays it out; three crumbling walls
// end it and leave the runner a short walk from the ropeswing. The pipe is the door in, not a step.
var DefaultLap = []string{"Ropeswing", "Log balance", "Obstacle net", "Balancing ledge", "Ladder", "Crumbling wall", "Crumbling wall", "Crumbling wall"}

// PreferredOps is the op an obstacle is worked with; anything else on it is a way back, not
// forward. Names and op strings checked against content (branch 289):
// skill_agility/configs/barbarian_course.loc.
var PreferredOps = map[string][]string{
	"obstacle pipe":   {"Squeeze-through"},
	"ropeswing":       {"Swing-on"},
	"log balance":     {"Walk-across"},
	"obstacle net":    {"Climb-over", "Climb-up"},
	"balancing ledge": {"Walk-across"},
	"ladder":          {"Climb-down"},
	"crumbling wall":  {"Climb-over"},
}

// StartTiles is the standing side of each obstacle, walked to before the click so it is never
// worked backwards. A name that repeats in the lap lists one tile per occurrence, in lap order:
// the three crumbling walls stand three tiles apart on one row, each climbed from the landing of
// the one before, so every start is east of the wall before it, never west of it.
var StartTiles = map[string][]tile.Tile{
	"obstacle pipe":   {{X: 2552, Z: 3561, Level: 0}},
	"ropeswing":       {{X: 2551, Z: 3554, Level: 0}},
	"log balance":     {{X: 2551, Z: 3546, Level: 0}},
	"obstacle net":    {{X: 2539, Z: 3546, Level: 0}},
	"balancing ledge": {{X: 2536, Z: 3547, Level: 1}},
	"ladder":          {{X: 2532, Z: 3546, Level: 1}},
	"crumbling wall": {
		{X: 2535, Z: 3553, Level: 0},
		{X: 2538, Z: 3553, Level: 0},
		{X: 2541, Z: 3553, Level: 0},
	},
}

// EntryObstacle is the enclosure's door: squeezed once from its north stand to get in, and taken
// by the walker on the way out to the bank. It is never a lap step, and is dropped from a lap
// setting that lists it.
const EntryObstacle = "Obstacle pipe"

// PipeNorth and PipeSouth are the stands either side of the pipe, from the walker's transport
// data (loc 2287 at 2552,3559 against the local engine's own content).
var (
	PipeNorth = tile.Tile{X: 2552, Z: 3561, Level: 0}
	PipeSouth = tile.Tile{X: 2552, Z: 3558, Level: 0}
)

// FallReturns is where a fall puts the runner back in the lap. The ledge is on the upper plane
// and a fall off it lands on the ground below, so the way back up is the net; the ropeswing and
// log drop you at their own start and are simply gone at again.
var FallReturns = map[string]string{
	"balancing ledge": "obstacle net",
}

// DoneLevels is the plane a cleared obstacle leaves the runner on when it pays no xp. The ladder
// is a plain climb down from the ledge plane, so it is judged by the climb rather than by a
// reward it never gives; the rest of the lap is judged by agility xp.
var DoneLevels = map[string]int{
	"ladder": 0,
}

// FallLevels is the obstacles a runner can fall off, and the plane a fall lands on. A fall line
// in the chat is only believed once the runner is on that plane: the ledge is on the upper plane
// and a fall lands on the ground, so a line seen while still up top is not a fall.
var FallLevels = map[string]int{
	"ropeswing":       0,
	"log balance":     0,
	"balancing ledge": 0,
}

// CourseEntry is where a trip back from the bank ends and the way in begins.
var CourseEntry = PipeNorth

// Box is an inclusive x/z bounding box.
type Box struct{ MinX, MaxX, MinZ, MaxZ int }

// CourseBox is the outpost enclosure the course runs in, ground and the ledge plane.
var CourseBox = Box{MinX: 2526, MaxX: 2560, MinZ: 3540, MaxZ: 3566}

// A fall off any of the three fallable obstacles (see FallLevels) can drop the account well
// outside CourseBox with no automatic rescue — content confirms this is by design: the fail
// case calls `agility_instant_fail` (skill_agility/scripts/agility.rs2) or an equivalent
// forcemove/p_teleport splash (barbarian_course.rs2's own Log balance handler), landing at a
// fixed point outside the course. There used to be a single hardcoded RopeswingPitLadder
// tile/id here, named for the ropeswing specifically and believed "confirmed live" — a real
// local-engine run instead fell off Log balance and landed nowhere near it (WalkTo correctly
// reported that coordinate unreachable), which makes sense once you notice the three fallable
// obstacles don't share one pit. barbarianrunner's fall-recovery now searches for the nearest
// real Ladder/Climb-up from wherever the fall actually landed (the same FindObstacle/
// d.Locs.Nearest technique every other obstacle already uses) instead of trusting one
// remembered coordinate; see internal/scripts/barbarianrunner/runner.go's SettleFell case.

// SearchRadius is how far a loc lookup near a start tile should look.
const SearchRadius = 20

// DefaultBank is Seers' Village, the nearest bank to the outpost on this server.
var DefaultBank = tile.Tile{X: 2725, Z: 3491, Level: 0}

const (
	DefaultFood          = "Lobster"
	DefaultFoodWithdraw  = 20
	ObstacleTimeoutTicks = 24
	LapRetryLimit        = 2
)

func key(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// OnCourse reports whether here is inside the outpost's bounding box.
func OnCourse(here *tile.Tile) bool {
	return here != nil && here.X >= CourseBox.MinX && here.X <= CourseBox.MaxX && here.Z >= CourseBox.MinZ && here.Z <= CourseBox.MaxZ
}

// InsideCourse reports whether here is on the course and south of the pipe's mouth, or up on the
// ledge plane.
func InsideCourse(here *tile.Tile) bool {
	return OnCourse(here) && (here.Level > 0 || here.Z <= PipeSouth.Z)
}

// Road is the outpost's own freshstart.Road — where an account stranded across water (see
// barbarianrunner.TravelToCourse's own fallback for exactly when this applies) sails back to
// once a plain same-level walk cannot reach the course at all. Radius uses SearchRadius, the
// same "close enough to work with" distance the rest of this package already uses for loc
// lookups — once within it, TravelToCourse's own ordinary same-level walk is expected to finish
// the last stretch itself. WoolUntil stays zero, unlike crafterplan's own Road(): this script has
// no wool loop, so freshstart.DecidePhase's wool-loop branch (PhaseDrop/PhaseStore/PhaseWool) is
// never taken for it — 0 < 0 is always false, whatever Crafting reads as.
func Road() freshstart.Road {
	return freshstart.Road{Destination: PipeNorth, Radius: SearchRadius}
}

// ArrivedDescription describes Road's own destination loop for DescribePhase — reaching it just
// means back in the outpost's general area, not a phase of its own the way Seers' flax loop is
// for crafterplan.
const ArrivedDescription = "back within reach of the outpost"

// DecidePhase is the outpost's own freshstart phase from live state, fixed to Road().
func DecidePhase(s freshstart.Snapshot) freshstart.Phase {
	return freshstart.DecidePhase(s, Road())
}

// DescribePhase is the outpost's own status line for a freshstart phase.
func DescribePhase(p freshstart.Phase) string {
	return freshstart.Describe(p, ArrivedDescription)
}

// NearTile reports whether here is within radius tiles of dest on the same level.
func NearTile(here *tile.Tile, dest tile.Tile, radius int) bool {
	return here != nil && here.Level == dest.Level && tile.Chebyshev(*here, dest) <= radius
}

// IsEntryObstacle reports whether name is the pipe, case- and space-insensitively.
func IsEntryObstacle(name string) bool {
	return key(name) == key(EntryObstacle)
}

// ParseLap splits comma-separated names, trims them, drops blanks and the pipe; nothing left
// means the default lap.
func ParseLap(raw string) []string {
	var names []string
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" || IsEntryObstacle(s) {
			continue
		}
		names = append(names, s)
	}
	if len(names) == 0 {
		out := make([]string, len(DefaultLap))
		copy(out, DefaultLap)
		return out
	}
	return names
}

// FallReturnFor is the obstacle to go back to after falling off name, or ok false when it is
// simply retried.
func FallReturnFor(name string) (string, bool) {
	v, ok := FallReturns[key(name)]
	return v, ok
}

// StartTileFor is the start of the occurrence-th step with this name (0-based); a name with
// fewer tiles reuses its last.
func StartTileFor(name string, occurrence int) (tile.Tile, bool) {
	tiles, ok := StartTiles[key(name)]
	if !ok || len(tiles) == 0 {
		return tile.Tile{}, false
	}
	if occurrence < 0 {
		occurrence = 0
	}
	if occurrence >= len(tiles) {
		occurrence = len(tiles) - 1
	}
	return tiles[occurrence], true
}

// OccurrenceOf is how many earlier steps of lap share the name at step, so a repeated obstacle
// knows which one it is.
func OccurrenceOf(lap []string, step int) int {
	if step < 0 || step >= len(lap) {
		return 0
	}
	name := key(lap[step])
	n := 0
	for i := 0; i < step && i < len(lap); i++ {
		if key(lap[i]) == name {
			n++
		}
	}
	return n
}

// DoneLevelFor is the plane a no-xp obstacle ends on, ok false when it pays xp instead.
func DoneLevelFor(name string) (int, bool) {
	v, ok := DoneLevels[key(name)]
	return v, ok
}

// FallLevelFor is the plane a fall off name lands on, ok false when it cannot be fallen off.
func FallLevelFor(name string) (int, bool) {
	v, ok := FallLevels[key(name)]
	return v, ok
}

// HasForwardOp reports whether a loc is the working end of its obstacle: it offers a forward op
// when the obstacle has one (the ladder's foot only offers Climb-up, so it is not the lap's
// ladder), else any op.
func HasForwardOp(name string, actions []string) bool {
	wanted, ok := PreferredOps[key(name)]
	if !ok {
		return len(actions) > 0
	}
	for _, w := range wanted {
		for _, a := range actions {
			if strings.EqualFold(a, w) {
				return true
			}
		}
	}
	return false
}

// ConfirmCleared reports whether an obstacle is cleared: on xp, or, for one that pays none, on
// arriving at the plane it ends on.
func ConfirmCleared(name string, xpGained bool, startLevel int, here *tile.Tile) bool {
	if xpGained {
		return true
	}
	level, ok := DoneLevelFor(name)
	return ok && level != startLevel && here != nil && here.Level == level
}

// ConfirmFall reports whether a fall line is believed: only on an obstacle that can drop the
// runner, and only on the plane a fall lands on.
func ConfirmFall(name string, lineSeen bool, here *tile.Tile) bool {
	level, ok := FallLevelFor(name)
	return lineSeen && ok && here != nil && here.Level == level
}

// PickOp is the op to click on an obstacle: its preferred forward op when it carries one, else
// its first, ok false when actions is empty.
func PickOp(name string, actions []string) (string, bool) {
	for _, w := range PreferredOps[key(name)] {
		for _, a := range actions {
			if strings.EqualFold(a, w) {
				return a, true
			}
		}
	}
	if len(actions) > 0 {
		return actions[0], true
	}
	return "", false
}

// Chat lines the course prints back.
var (
	// WrongSide is clicking an obstacle from the wrong approach side; no damage.
	WrongSide = regexp.MustCompile(`(?i)^(?:you cannot do that from here|you can't enter the pipe from this side)`)
	// CantReach is the server's real "can't reach that" rejection.
	CantReach = regexp.MustCompile(`(?i)^i can't reach that`)
	// Fell is a fall off the ropeswing, log or ledge.
	Fell = regexp.MustCompile(`(?i)(?:you slip and fall|you lose your footing|you fall)`)
	// GateRefused is the gate guard turning the account away.
	GateRefused = regexp.MustCompile(`(?i)barcrawl`)
)

// SettleReason is what a finished obstacle wait meant.
type SettleReason string

const (
	SettleCleared     SettleReason = "cleared"
	SettleWrongSide   SettleReason = "wrong_side"
	SettleFell        SettleReason = "fell"
	SettleCantReach   SettleReason = "cant_reach"
	SettleInterrupted SettleReason = "interrupted"
	SettleLowHP       SettleReason = "low_hp"
	SettleTimeout     SettleReason = "timeout"
)

// ObstacleSignals is what was observed while waiting on an obstacle.
type ObstacleSignals struct {
	Cleared     bool
	WrongSide   bool
	Fell        bool
	CantReach   bool
	Interrupted bool
	LowHP       bool
}

// ClassifyObstacle decides what a finished obstacle wait meant: a clear first, then the
// interrupt, then the lines, then hunger.
func ClassifyObstacle(s ObstacleSignals) SettleReason {
	switch {
	case s.Cleared:
		return SettleCleared
	case s.Interrupted:
		return SettleInterrupted
	case s.WrongSide:
		return SettleWrongSide
	case s.Fell:
		return SettleFell
	case s.CantReach:
		return SettleCantReach
	case s.LowHP:
		return SettleLowHP
	default:
		return SettleTimeout
	}
}

// ReactionMs is a human pause after clearing an obstacle; a tick is 600ms and the character has
// to settle. rng defaults to rand.Float64 when nil.
func ReactionMs(rng func() float64) time.Duration {
	if rng == nil {
		rng = rand.Float64
	}
	var ms float64
	if rng() < 0.1 {
		ms = 1200 + rng()*1800
	} else {
		ms = 600 + rng()*900
	}
	return time.Duration(ms * float64(time.Millisecond))
}

// NeedsBankTrip reports whether food is out, unless a lap is mid-obstacle; below the floor at
// start means the same.
func NeedsBankTrip(food, minFood int) bool {
	if minFood < 0 {
		minFood = 0
	}
	return food < minFood
}

// FoodToWithdraw is how much more food to withdraw to reach target, never negative.
func FoodToWithdraw(held, target int) int {
	if v := target - held; v > 0 {
		return v
	}
	return 0
}

// Stage is where the runner belongs.
type Stage string

const (
	StageRecover Stage = "recover"
	StageEat     Stage = "eat"
	StageBank    Stage = "bank"
	StageTravel  Stage = "travel"
	StageEnter   Stage = "enter"
	StageLap     Stage = "lap"
)

// Snapshot is the state DecideStage reads.
type Snapshot struct {
	Dead    bool
	NeedEat bool
	Food    int
	MinFood int
	Here    *tile.Tile
	// Banking is a bank trip already under way, finished before anything else.
	Banking bool
}

// DecideStage picks the stage. A death is recovered first, then hunger, then an empty pack goes
// to the bank, then anywhere off the course walks back to it, the outside of the pipe squeezes
// in, and the rest is laps.
func DecideStage(s Snapshot) Stage {
	switch {
	case s.Dead:
		return StageRecover
	case s.NeedEat:
		return StageEat
	case s.Banking || NeedsBankTrip(s.Food, s.MinFood):
		return StageBank
	case !OnCourse(s.Here):
		return StageTravel
	case !InsideCourse(s.Here):
		return StageEnter
	default:
		return StageLap
	}
}

// DescribeStage is a human label for stage.
func DescribeStage(stage Stage) string {
	switch stage {
	case StageRecover:
		return "recovering from a death"
	case StageEat:
		return "eating"
	case StageBank:
		return "banking for food"
	case StageTravel:
		return "walking to the course"
	case StageEnter:
		return "squeezing in through the pipe"
	case StageLap:
		return "running the course"
	default:
		return ""
	}
}
