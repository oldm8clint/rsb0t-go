package fisherrunner

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The Catherby bank and the sealed tile netrescue pinned: 2844,3427 is 35 tiles from the bank, so
// fisherplan.InCatherby's 60-tile radius calls it home while nothing can walk out of it.
var (
	noteBank   = tile.Tile{X: 2809, Z: 3441}
	noteSealed = tile.Tile{X: 2844, Z: 3427}
	noteCamp   = tile.Tile{X: 2845, Z: 3431}
)

// TestArrivalNoteSaysNothingWhenTheRouteIsReal is the assertion that keeps this off 45 live
// accounts. An account standing at the fishing camp when something asks it to go home has not
// moved either, and is 36 tiles out, so distance and movement alone would both fire here. Only
// connectivity separates the camp from a sealed pocket, and the camp has it.
func TestArrivalNoteSaysNothingWhenTheRouteIsReal(t *testing.T) {
	if got := arrivalNote("ReturnToCatherby", noteCamp, noteCamp, noteBank, true); got != "" {
		t.Fatalf("a reachable arrival should say nothing, got %q", got)
	}
}

// TestArrivalNoteFiresOnTheSilentSuccess is the wedge: the check reported home, the account never
// moved, and there is no route to the place it claims to have reached.
func TestArrivalNoteFiresOnTheSilentSuccess(t *testing.T) {
	got := arrivalNote("ReturnToCatherby", noteSealed, noteSealed, noteBank, false)
	if got == "" {
		t.Fatal("an unreachable arrival should produce a line")
	}
	for _, want := range []string{
		"WARNING",
		"ReturnToCatherby",
		"2844,3427,0", // where the account actually is
		"2809,3441,0", // where it claims to have arrived
		"35 tiles",    // how far apart those really are
		"no movement occurred",
		"NO walkable route",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the line does not name %q:\n%s", want, got)
		}
	}
}

// TestArrivalNoteDistinguishesAPartialWalk keeps the line honest when the account did move — after
// a sealed-pocket escape, for instance — and still landed somewhere it cannot walk home from.
// "no movement occurred" would be a false statement there.
func TestArrivalNoteDistinguishesAPartialWalk(t *testing.T) {
	from := tile.Tile{X: 2840, Z: 3420}
	got := arrivalNote("ReturnToCatherby, after its walk attempts", from, noteSealed, noteBank, false)
	if strings.Contains(got, "no movement occurred") {
		t.Errorf("an account that moved should not be reported as motionless:\n%s", got)
	}
	if !strings.Contains(got, "having moved from 2840,3420,0") {
		t.Errorf("the line should name where the recovery started:\n%s", got)
	}
}

// TestNoteArrivalWithNoPackStaysQuiet: a nil pack is no opinion, not a warning. Several standalone
// tools and every bare unit-test Context run without one.
func TestNoteArrivalWithNoPackStaysQuiet(t *testing.T) {
	said := 0
	NoteArrival(nil, func(string) { said++ }, "ReturnToCatherby", noteSealed, noteSealed, noteBank)
	if said != 0 {
		t.Fatalf("a nil pack wrote %d line(s), want 0", said)
	}
}

// TestNoteArrivalWithNoLoggerDoesNotPanic covers the other nil: Deps.Log is documented as
// droppable and internal/bot's Context.Log can be nil too.
func TestNoteArrivalWithNoLoggerDoesNotPanic(t *testing.T) {
	NoteArrival(nil, nil, "ReturnToCatherby", noteSealed, noteSealed, noteBank)
}

// TestNoteArrivalThroughNilDepsDoesNotPanic guards the in-package wrapper the five runner.go call
// sites use.
func TestNoteArrivalThroughNilDepsDoesNotPanic(t *testing.T) {
	noteArrival(nil, "ReturnToCatherby", noteSealed, noteSealed, noteBank)
}
