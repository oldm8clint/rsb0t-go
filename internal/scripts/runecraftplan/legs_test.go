package runecraftplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The Nature temple's inside walk in the numbers the live log gave: the account lands on the Enter tile, the altar is four tiles north
// of it and the portal is the Enter tile's neighbour.
var (
	legsStand   = tile.Tile{X: 2867, Z: 3020}
	legsEnter   = Nature.Enter
	legsAltarAt = tile.Tile{X: 2400, Z: 4839}
)

var legsSpots = LegSpots{
	Inside:   Nature.Inside,
	AtAltar:  func(t tile.Tile) bool { return t == legsAltarAt },
	AtPortal: func(t tile.Tile) bool { return t == legsEnter },
}

// at is the Enter tile n tiles north.
func legsNorth(n int) tile.Tile { return tile.Tile{X: legsEnter.X, Z: legsEnter.Z + n} }

// walkCycle is a cycle walked a tile a tick: the trade closes at 100, the click is sent at 101, he lands inside at 103, walks four tiles to
// the altar (arrives 107), crafts (seen 108), holds to 112, walks four back (arrives 116) and is outside at 117.
func walkCycle() LegInput {
	s := []LegSample{
		{Tick: 100, At: legsStand, Run: false, Energy: 5},
		{Tick: 101, At: legsStand, Run: false, Energy: 5},
		{Tick: 102, At: legsStand, Run: false, Energy: 5},
		{Tick: 103, At: legsEnter, Run: false, Energy: 5},
		{Tick: 104, At: legsNorth(1), Run: false, Energy: 5},
		{Tick: 105, At: legsNorth(2), Run: false, Energy: 5},
		{Tick: 106, At: legsNorth(3), Run: false, Energy: 4},
		{Tick: 107, At: legsNorth(4), Run: false, Energy: 4},
	}
	for tick := 108; tick <= 112; tick++ {
		s = append(s, LegSample{Tick: tick, At: legsNorth(4), Energy: 4})
	}
	for i, tick := 1, 113; tick <= 116; i, tick = i+1, tick+1 {
		s = append(s, LegSample{Tick: tick, At: legsNorth(4 - i), Energy: 3})
	}
	s = append(s, LegSample{Tick: 117, At: Nature.Exit, Energy: 3})
	return LegInput{Close: 100, Click: 101, Craft: 108, TalismanClicks: 1, CraftClicks: 1, PortalClicks: 1, PortalHeld: 4, Samples: s}
}

func TestMeasureLegsReadsEveryMarkOffTheSamples(t *testing.T) {
	l := MeasureLegs(walkCycle(), legsSpots)
	want := Legs{Close: 100, Click: 101, Inside: 103, Altar: 107, Craft: 108, Hold: 112, Portal: 116, Exit: 117}
	if l.Close != want.Close || l.Click != want.Click || l.Inside != want.Inside || l.Altar != want.Altar || l.Craft != want.Craft ||
		l.Hold != want.Hold || l.Portal != want.Portal || l.Exit != want.Exit {
		t.Fatalf("marks %+v, want %+v", l, want)
	}
	if !l.Complete() || l.Total() != 17 || l.Retried() {
		t.Fatalf("complete %v, total %d, retried %v", l.Complete(), l.Total(), l.Retried())
	}
	if l.AltarTiles != 4 || l.PortalTiles != 4 {
		t.Errorf("walked %d tiles to the altar and %d to the portal, want 4 and 4", l.AltarTiles, l.PortalTiles)
	}
	if l.AltarRun != 0 || l.PortalRun != 0 {
		t.Errorf("run flags %d and %d, want off (0) both", l.AltarRun, l.PortalRun)
	}
	if l.EnergyClose != 5 || l.EnergyExit != 3 || l.AltarEnergy != 5 || l.PortalEnergy != 4 {
		t.Errorf("energy close %d exit %d altar leg %d portal leg %d", l.EnergyClose, l.EnergyExit, l.AltarEnergy, l.PortalEnergy)
	}
}

// The line is the contract the live proofs grep: its fields, order and the deltas.
func TestLegsLineFormat(t *testing.T) {
	got := MeasureLegs(walkCycle(), legsSpots).Line()
	want := "legs: close 100, click 101 (+1), inside 103 (+2), altar 107 (+4; 4 tile(s); run off; energy 5%), craft 108 (+1), hold 112 (+4), " +
		"portal 116 (+4; 4 tile(s); run off; energy 4%), exit 117 (+1); 17 ticks close to exit; energy 5% -> 3%; clicks talisman 1 craft 1 portal 1 (held back 4 ticks)"
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

// A run covers two tiles a tick: the leg is half as long and the tiles say so whatever the flag shows.
func TestLegsRunningHalvesTheWalkAndShowsInTheTiles(t *testing.T) {
	in := LegInput{Close: 10, Click: 11, Craft: 18, TalismanClicks: 1, CraftClicks: 1, PortalClicks: 1, PortalHeld: 4}
	in.Samples = []LegSample{
		{Tick: 10, At: legsStand, Run: true, Energy: 40},
		{Tick: 13, At: legsEnter, Run: true, Energy: 40},
		{Tick: 14, At: legsNorth(2), Run: true, Energy: 39},
		{Tick: 15, At: legsNorth(4), Run: true, Energy: 38},
		{Tick: 18, At: legsNorth(4), Run: true, Energy: 38},
		{Tick: 22, At: legsNorth(4), Run: true, Energy: 38},
		{Tick: 23, At: legsNorth(2), Run: true, Energy: 37},
		{Tick: 24, At: legsEnter, Run: true, Energy: 36},
		{Tick: 25, At: Nature.Exit, Run: false, Energy: 36},
	}
	l := MeasureLegs(in, legsSpots)
	if l.Altar != 15 || l.AltarTiles != 4 || l.AltarRun != 1 {
		t.Errorf("altar leg: tick %d, %d tiles, run %d", l.Altar, l.AltarTiles, l.AltarRun)
	}
	if l.Portal != 24 || l.PortalTiles != 4 || l.PortalRun != 1 {
		t.Errorf("portal leg: tick %d, %d tiles, run %d", l.Portal, l.PortalTiles, l.PortalRun)
	}
	if line := l.Line(); !strings.Contains(line, "altar 15 (+2; 4 tile(s); run on; energy 40%)") || !strings.Contains(line, "portal 24 (+2; 4 tile(s); run on; energy 38%)") {
		t.Errorf("line: %s", line)
	}
}

// A craft read at the portal's own tile (a stand that serves both) cannot be left before the hold is over: the account does not walk in a
// hold, and the portal click is read no earlier than the tick after it.
func TestLegsPortalIsNoEarlierThanTheHoldsEnd(t *testing.T) {
	spots := LegSpots{Inside: Nature.Inside, AtAltar: func(t tile.Tile) bool { return t == legsEnter }, AtPortal: func(t tile.Tile) bool { return t == legsEnter }}
	in := LegInput{Close: 100, Click: 101, Craft: 105, TalismanClicks: 1, CraftClicks: 1, PortalClicks: 1, PortalHeld: 4}
	for tick := 100; tick <= 110; tick++ {
		at := legsStand
		if tick >= 103 {
			at = legsEnter
		}
		if tick >= 110 {
			at = Nature.Exit
		}
		in.Samples = append(in.Samples, LegSample{Tick: tick, At: at, Energy: 9})
	}
	l := MeasureLegs(in, spots)
	if l.Altar != 103 || l.Hold != 109 || l.Portal != 109 || l.Exit != 110 {
		t.Fatalf("%+v", l)
	}
	if l.AltarTiles != 0 || l.PortalTiles != 0 {
		t.Errorf("tiles %d and %d, want none walked", l.AltarTiles, l.PortalTiles)
	}
}

// A cycle that was cut short is not a line: a restart in the temple has no trade close, a lost sample has no exit.
func TestLegsIncompleteCyclesHaveNoLine(t *testing.T) {
	in := walkCycle()
	in.Close = 0
	if MeasureLegs(in, legsSpots).Complete() {
		t.Error("a cycle with no trade close was complete")
	}
	in = walkCycle()
	in.Samples = in.Samples[:12] // the walk back and the exit are missing
	if l := MeasureLegs(in, legsSpots); l.Complete() || l.Exit != 0 {
		t.Errorf("a cycle with no exit sample was complete: %+v", l)
	}
	in = walkCycle()
	in.Samples = nil
	if l := MeasureLegs(in, legsSpots); l.Complete() || l.Inside != 0 {
		t.Errorf("a cycle with no samples was complete: %+v", l)
	}
	// Places the tests never name are never reached.
	if l := MeasureLegs(walkCycle(), LegSpots{}); l.Complete() {
		t.Errorf("no place tests, yet complete: %+v", l)
	}
}

// A retry anywhere is said, because the legs then include it.
func TestLegsRetriesAreSaid(t *testing.T) {
	for name, edit := range map[string]func(*LegInput){
		"two talisman clicks": func(in *LegInput) { in.TalismanClicks = 2 },
		"two craft clicks":    func(in *LegInput) { in.CraftClicks = 2 },
		"two portal clicks":   func(in *LegInput) { in.PortalClicks = 2 },
		"a stage run again":   func(in *LegInput) { in.ExtraTries = 1 },
	} {
		in := walkCycle()
		edit(&in)
		l := MeasureLegs(in, legsSpots)
		if !l.Retried() || !strings.Contains(l.Line(), "RETRIED") {
			t.Errorf("%s: not said: %s", name, l.Line())
		}
	}
	if strings.Contains(MeasureLegs(walkCycle(), legsSpots).Line(), "RETRIED") {
		t.Error("a clean cycle was called a retry")
	}
}

// Marks that were not observed are "?" and the cycle still reads (the click tick is the one a caller may not have).
func TestLegsLineMarksTheUnobserved(t *testing.T) {
	in := walkCycle()
	in.Click = 0
	l := MeasureLegs(in, legsSpots)
	if line := l.Line(); strings.Contains(line, ", click ") || !strings.Contains(line, "inside 103 (+3)") {
		t.Errorf("no click tick: %s", line)
	}
	in = walkCycle()
	for i := range in.Samples {
		in.Samples[i].Energy = -1
	}
	if line := MeasureLegs(in, legsSpots).Line(); strings.Contains(line, "energy") {
		t.Errorf("energy unknown, yet said: %s", line)
	}
}
