package barbarianplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

func TestOnCourseAndInsideCourse(t *testing.T) {
	ground := tile.Tile{X: 2540, Z: 3550, Level: 0}
	if !OnCourse(&ground) || !InsideCourse(&ground) {
		t.Fatalf("expected %+v on and inside the course", ground)
	}

	outsidePipe := tile.Tile{X: 2540, Z: 3565, Level: 0}
	if !OnCourse(&outsidePipe) {
		t.Fatalf("expected %+v on the course (still in the box)", outsidePipe)
	}
	if InsideCourse(&outsidePipe) {
		t.Fatalf("expected %+v to be outside the pipe on the ground plane", outsidePipe)
	}

	ledge := tile.Tile{X: 2536, Z: 3565, Level: 1}
	if !InsideCourse(&ledge) {
		t.Fatalf("expected %+v inside the course: upper plane counts regardless of z", ledge)
	}

	elsewhere := tile.Tile{X: 3000, Z: 3000, Level: 0}
	if OnCourse(&elsewhere) || InsideCourse(&elsewhere) {
		t.Fatalf("expected %+v off the course entirely", elsewhere)
	}
	if OnCourse(nil) || InsideCourse(nil) {
		t.Fatal("nil tile must never read as on or inside the course")
	}
}

func TestParseLap(t *testing.T) {
	if got := ParseLap(""); len(got) != len(DefaultLap) {
		t.Fatalf("blank lap should fall back to the default, got %v", got)
	}
	got := ParseLap(" Ropeswing ,, Obstacle pipe ,Ladder")
	want := []string{"Ropeswing", "Ladder"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected blanks and the pipe dropped, got %v", got)
	}
}

func TestStartTileForOccurrence(t *testing.T) {
	first, ok := StartTileFor("crumbling wall", 0)
	if !ok || first.X != 2535 {
		t.Fatalf("unexpected first crumbling wall tile: %+v ok=%v", first, ok)
	}
	third, ok := StartTileFor("Crumbling Wall", 2)
	if !ok || third.X != 2541 {
		t.Fatalf("unexpected third crumbling wall tile: %+v ok=%v", third, ok)
	}
	overflow, ok := StartTileFor("crumbling wall", 99)
	if !ok || overflow != third {
		t.Fatalf("occurrence past the end should reuse the last tile, got %+v ok=%v", overflow, ok)
	}
	if _, ok := StartTileFor("nonexistent", 0); ok {
		t.Fatal("unknown obstacle name should report ok=false")
	}
}

func TestOccurrenceOf(t *testing.T) {
	lap := []string{"Crumbling wall", "Ladder", "Crumbling wall", "Crumbling wall"}
	for step, want := range map[int]int{0: 0, 1: 0, 2: 1, 3: 2} {
		if got := OccurrenceOf(lap, step); got != want {
			t.Errorf("step %d: got occurrence %d, want %d", step, got, want)
		}
	}
}

func TestHasForwardOp(t *testing.T) {
	if !HasForwardOp("ladder", []string{"Climb-down"}) {
		t.Fatal("Climb-down should be the ladder's forward op")
	}
	if HasForwardOp("ladder", []string{"Climb-up"}) {
		t.Fatal("the ladder's foot (Climb-up only) should not count as the lap's ladder")
	}
	if !HasForwardOp("unlisted obstacle", []string{"Anything"}) {
		t.Fatal("an obstacle with no preferred-op entry should accept any op")
	}
}

func TestConfirmClearedAndFall(t *testing.T) {
	if !ConfirmCleared("ropeswing", true, 0, nil) {
		t.Fatal("xp gained always confirms a clear")
	}
	ground := tile.Tile{Level: 0}
	ledgePlane := tile.Tile{Level: 1}
	if !ConfirmCleared("ladder", false, 1, &ground) {
		t.Fatal("the ladder should confirm on arriving at plane 0 from plane 1, even with no xp")
	}
	if ConfirmCleared("ladder", false, 0, &ground) {
		t.Fatal("no plane change should not confirm a no-xp obstacle")
	}
	if ConfirmCleared("ropeswing", false, 0, &ground) {
		t.Fatal("an obstacle with no DoneLevel entry should never confirm without xp")
	}

	if !ConfirmFall("ropeswing", true, &ground) {
		t.Fatal("a fall line on the ground plane should be believed for the ropeswing")
	}
	if ConfirmFall("ropeswing", true, &ledgePlane) {
		t.Fatal("a fall line seen while still on the upper plane should not be believed")
	}
	if ConfirmFall("ladder", true, &ground) {
		t.Fatal("the ladder cannot be fallen off")
	}
}

func TestPickOp(t *testing.T) {
	op, ok := PickOp("obstacle net", []string{"Examine", "Climb-up", "Climb-over"})
	if !ok || op != "Climb-over" {
		t.Fatalf("expected the preferred Climb-over first, got %q ok=%v", op, ok)
	}
	op, ok = PickOp("obstacle net", []string{"Examine", "Climb-up"})
	if !ok || op != "Climb-up" {
		t.Fatalf("expected the second-preference Climb-up, got %q ok=%v", op, ok)
	}
	if _, ok := PickOp("obstacle net", nil); ok {
		t.Fatal("no actions should report ok=false")
	}
}

func TestClassifyObstaclePriority(t *testing.T) {
	cases := []struct {
		signals ObstacleSignals
		want    SettleReason
	}{
		{ObstacleSignals{Cleared: true, Fell: true}, SettleCleared},
		{ObstacleSignals{Interrupted: true, WrongSide: true}, SettleInterrupted},
		{ObstacleSignals{WrongSide: true, Fell: true}, SettleWrongSide},
		{ObstacleSignals{Fell: true, CantReach: true}, SettleFell},
		{ObstacleSignals{CantReach: true, LowHP: true}, SettleCantReach},
		{ObstacleSignals{LowHP: true}, SettleLowHP},
		{ObstacleSignals{}, SettleTimeout},
	}
	for _, c := range cases {
		if got := ClassifyObstacle(c.signals); got != c.want {
			t.Errorf("%+v: got %s, want %s", c.signals, got, c.want)
		}
	}
}

func TestReactionMsRange(t *testing.T) {
	if got := ReactionMs(func() float64 { return 0.5 }); got != 600*time.Millisecond+450*time.Millisecond {
		t.Fatalf("common branch: got %v", got)
	}
	if got := ReactionMs(func() float64 { return 0.05 }); got != 1200*time.Millisecond+90*time.Millisecond {
		t.Fatalf("rare long-pause branch: got %v", got)
	}
}

func TestNeedsBankTripAndFoodToWithdraw(t *testing.T) {
	if !NeedsBankTrip(0, 5) {
		t.Fatal("empty pack below the floor needs a bank trip")
	}
	if NeedsBankTrip(5, 5) {
		t.Fatal("food at the floor should not need a trip")
	}
	if NeedsBankTrip(1, -3) {
		t.Fatal("a negative floor should behave like zero")
	}
	if got := FoodToWithdraw(3, 20); got != 17 {
		t.Fatalf("got %d, want 17", got)
	}
	if got := FoodToWithdraw(25, 20); got != 0 {
		t.Fatalf("held more than target should withdraw nothing, got %d", got)
	}
}

func TestDecideStagePriority(t *testing.T) {
	onCourseGround := tile.Tile{X: 2540, Z: 3550, Level: 0}
	offCourse := tile.Tile{X: 3000, Z: 3000, Level: 0}
	outsidePipe := tile.Tile{X: 2540, Z: 3565, Level: 0}

	cases := []struct {
		name string
		snap Snapshot
		want Stage
	}{
		{"dead beats everything", Snapshot{Dead: true, NeedEat: true, Food: 0, MinFood: 5}, StageRecover},
		{"hunger beats banking", Snapshot{NeedEat: true, Food: 0, MinFood: 5}, StageEat},
		{"empty pack banks", Snapshot{Food: 0, MinFood: 5, Here: &onCourseGround}, StageBank},
		{"mid bank trip keeps banking", Snapshot{Banking: true, Food: 20, MinFood: 5, Here: &onCourseGround}, StageBank},
		{"off the course travels", Snapshot{Food: 20, MinFood: 5, Here: &offCourse}, StageTravel},
		{"on the course but outside the pipe enters", Snapshot{Food: 20, MinFood: 5, Here: &outsidePipe}, StageEnter},
		{"inside the course runs laps", Snapshot{Food: 20, MinFood: 5, Here: &onCourseGround}, StageLap},
	}
	for _, c := range cases {
		if got := DecideStage(c.snap); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestChatLinePatterns(t *testing.T) {
	if !CantReach.MatchString("I can't reach that!") {
		t.Fatal("CantReach should match the server's real rejection line")
	}
	if !Fell.MatchString("You lose your footing and fall.") {
		t.Fatal("Fell should match a footing-loss line")
	}
	if !WrongSide.MatchString("You can't enter the pipe from this side.") {
		t.Fatal("WrongSide should match the pipe's wrong-side line")
	}
	if !GateRefused.MatchString("The barcrawl gate is off limits to you.") {
		t.Fatal("GateRefused should match a barcrawl mention")
	}
}

// TestFallPitLandingIsOutsideTheCourse regression-covers a real local-engine finding: falling
// off Log balance landed the account at 2809,3441,0 (observed live, well outside CourseBox),
// not at any single hardcoded pit coordinate a fixed ropeswing-only escape route once assumed.
// OnCourse must read this real landing tile as off-course so barbarianrunner's fall-recovery
// (a dynamic nearest-Ladder search from wherever the fall actually lands, see
// internal/scripts/barbarianrunner/runner.go's SettleFell case) actually triggers instead of
// silently treating the account as still mid-obstacle.
func TestFallPitLandingIsOutsideTheCourse(t *testing.T) {
	landing := tile.Tile{X: 2809, Z: 3441, Level: 0}
	if OnCourse(&landing) {
		t.Fatal("a real fall-pit landing tile should read as outside CourseBox — that's the whole reason a fall needs an escape route rather than a normal retry")
	}
}

// TestRoadNeverEntersTheWoolLoop confirms Road()'s WoolUntil=0 does what its own doc comment
// claims: whatever Crafting reads as (this script never sets it — Snapshot's zero value, 0, is
// what travelViaFreshStart's own freshStartSnapshot always sends), freshstart.DecidePhase must
// never return a wool-loop phase (PhaseDrop/PhaseStore/PhaseWool) for it, only the phases
// travelViaFreshStart actually knows how to drive (PhaseFund/PhaseSkirt/PhaseTravel/PhaseArrived).
func TestRoadNeverEntersTheWoolLoop(t *testing.T) {
	lumbridge := freshstart.LumbridgeCentre
	cases := []struct {
		name string
		snap freshstart.Snapshot
		want freshstart.Phase
	}{
		{"short of the fare near Lumbridge", freshstart.Snapshot{Tile: &lumbridge, Fare: 60}, freshstart.PhaseFund},
		{"fare and skirt cost both in hand, skirt still to buy", freshstart.Snapshot{Tile: &lumbridge, Coins: 63, Fare: 60, SkirtCost: 3}, freshstart.PhaseSkirt},
		{"fare in hand, skirt already worn", freshstart.Snapshot{Tile: &lumbridge, Coins: 63, Fare: 60, SkirtCost: 3, SkirtWorn: true}, freshstart.PhaseTravel},
		{"back at the outpost's own pipe stand", freshstart.Snapshot{Tile: &PipeNorth}, freshstart.PhaseArrived},
	}
	for _, c := range cases {
		if got := DecidePhase(c.snap); got != c.want {
			t.Errorf("%s: DecidePhase = %s, want %s", c.name, got, c.want)
		}
	}
}
