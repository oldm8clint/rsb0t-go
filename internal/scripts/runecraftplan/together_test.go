package runecraftplan

import (
	"strings"
	"testing"
)

// A round starts with the one-write accept on, and a cycle that works leaves it on.
func TestTogetherStartsOnAndStaysOnWhileItWorks(t *testing.T) {
	o := NewTogetherOps(true)
	for i := 0; i < 50; i++ {
		if !o.Try() {
			t.Fatalf("cycle %d was not tried", i)
		}
		if line := o.Note(DirectOK, ""); line != "" {
			t.Fatalf("a cycle that works said %q", line)
		}
	}
	if o.Tried() != 50 || o.GivenUp() {
		t.Errorf("tried %d, given up %v", o.Tried(), o.GivenUp())
	}
}

// ONE failure sends the next TogetherBackoff cycles the plain way, then it is tried again and says it worked.
func TestAFailedCycleIsPlainForTheBackoffAndThenTriedAgain(t *testing.T) {
	o := NewTogetherOps(true)
	if !o.Try() {
		t.Fatal("not tried")
	}
	line := o.Note(DirectFailed, "no essence came")
	for _, want := range []string{"one-write accept", "failed", "no essence came", "plain way", "10 cycle(s)"} {
		if !strings.Contains(line, want) {
			t.Errorf("the line lacks %q: %s", want, line)
		}
	}
	for i := 1; i <= TogetherBackoff; i++ {
		if o.Try() {
			t.Fatalf("plain cycle %d of %d was tried", i, TogetherBackoff)
		}
	}
	if !o.Try() {
		t.Fatalf("not tried again after %d plain cycles", TogetherBackoff)
	}
	if line := o.Note(DirectOK, ""); !strings.Contains(line, "worked again") {
		t.Errorf("the recovery said %q", line)
	}
	if line := o.Note(DirectOK, ""); line != "" {
		t.Errorf("a second success said %q", line)
	}
}

// TogetherGiveUp failures in a row end it for the round; a success in between starts the count again.
func TestAGivenUpOneWriteStaysPlainAndASuccessResetsTheCount(t *testing.T) {
	o := NewTogetherOps(true)
	fail := func() string {
		for !o.Try() && !o.GivenUp() {
		}
		return o.Note(DirectFailed, "no essence came")
	}
	fail()
	fail()
	o.Try()
	o.Note(DirectOK, "")
	for i := 0; i < TogetherGiveUp-1; i++ {
		fail()
		if o.GivenUp() {
			t.Fatalf("given up after %d failures since a success", i+1)
		}
	}
	line := fail()
	if !o.GivenUp() || !strings.Contains(line, "for the rest of the round") || !strings.Contains(line, "3 times in a row") {
		t.Fatalf("not given up after %d in a row: %q", TogetherGiveUp, line)
	}
	for i := 0; i < 40; i++ {
		if o.Try() {
			t.Fatal("a given-up one-write accept was tried")
		}
	}
	if s := o.Summary(); !strings.Contains(s, "(given up)") {
		t.Errorf("summary %s", s)
	}
}

// A result not seen in a view that lags says nothing: no back-off, no failure counted.
func TestAnUnseenCycleChangesNothing(t *testing.T) {
	o := NewTogetherOps(true)
	for i := 0; i < 20; i++ {
		if !o.Try() {
			t.Fatalf("cycle %d was not tried after not-seen results", i)
		}
		if line := o.Note(DirectUnseen, "not seen"); line != "" {
			t.Fatalf("an unseen cycle said %q", line)
		}
	}
	if o.GivenUp() || o.Tried() != 20 {
		t.Errorf("given up %v, tried %d", o.GivenUp(), o.Tried())
	}
}

// Off, and nil, never try and say nothing.
func TestTogetherOffAndNilNeverTry(t *testing.T) {
	for name, o := range map[string]*TogetherOps{"off": NewTogetherOps(false), "nil": nil} {
		if o.On() || o.Try() || o.GivenUp() || o.Tried() != 0 {
			t.Errorf("%s: on %v", name, o.On())
		}
		o.NoteWaitedOut()
		if line := o.Note(DirectFailed, "x"); line != "" {
			t.Errorf("%s said %q", name, line)
		}
		if s := o.Summary(); s != "one-write accept: off" {
			t.Errorf("%s: %s", name, s)
		}
	}
}

// The summary counts what the cycles did, the waits that ran out and the cycles spent in a back-off.
func TestTogetherSummary(t *testing.T) {
	o := NewTogetherOps(true)
	o.Try()
	o.Note(DirectOK, "")
	o.Try()
	o.NoteWaitedOut()
	o.Try()
	o.Note(DirectFailed, "x")
	o.Try() // in the back-off
	o.Try()
	want := "one-write accept: 1 worked, 1 failed, 0 not seen; 3 plain (1 waited out, 2 in a back-off)"
	if got := o.Summary(); got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

// The wait is three ticks: long enough for a carrier on an ordinary connection to be heard, short enough that a stuck one costs one cycle.
func TestTheWaitForTheCarriersAcceptIsThreeTicks(t *testing.T) {
	if GateTogetherWait.Milliseconds() != 3*600 {
		t.Fatalf("%v", GateTogetherWait)
	}
}

// With the talisman written WITH the accept, the legs line says so: the click is sent before the close is seen, so its delta is from the
// close (negative here) and the inside mark is measured from the close, which is what the plan's pass (close to inside at most 3) reads.
func TestTheLegsLineOfAOneWriteCycle(t *testing.T) {
	in := walkCycle()
	in.Click = 99 // the accept and the talisman written in tick 99, the close seen in 100
	in.Together = true
	in.Open = 94 // the trade screen opened in 94: the saving of the lane is read from here, not from the close
	l := MeasureLegs(in, legsSpots)
	line := l.Line()
	for _, want := range []string{"close 100, click 99 (-1; with the accept), inside 103 (+3),", "clicks talisman 1 craft 1 portal 1", "; one write: the confirm accept and the talisman", "; 9 ticks open to inside"} {
		if !strings.Contains(line, want) {
			t.Errorf("the line lacks %q:\n%s", want, line)
		}
	}
	// The same cycle without the tag keeps its line.
	if plain := MeasureLegs(walkCycle(), legsSpots).Line(); strings.Contains(plain, "one write") {
		t.Errorf("a plain cycle's line mentions it: %s", plain)
	}
}

// A line that never shows (a server that words it otherwise, a component that is not the one) would cost every cycle the whole wait: four
// waits that run out in a row stand the one-write accept down for the back-off, and a second stand-down with no success between gives it up.
func TestALineThatNeverShowsStandsTheOneWriteDownAndThenGivesItUp(t *testing.T) {
	o := NewTogetherOps(true)
	waitOut := func(n int) string {
		line := ""
		for i := 0; i < n; i++ {
			if !o.Try() {
				t.Fatalf("wait %d of %d: not tried", i+1, n)
			}
			line = o.NoteWaitedOut()
			if line != "" && i < n-1 {
				t.Fatalf("wait %d of %d already said %q", i+1, n, line)
			}
		}
		return line
	}
	line := waitOut(TogetherWaitedOutLimit)
	for _, want := range []string{"4 trades in a row", "plain way", "next 10 cycle(s)"} {
		if !strings.Contains(line, want) {
			t.Errorf("the stand-down line lacks %q: %s", want, line)
		}
	}
	for i := 1; i <= TogetherBackoff; i++ {
		if o.Try() {
			t.Fatalf("plain cycle %d of %d was tried", i, TogetherBackoff)
		}
	}
	line = waitOut(TogetherWaitedOutLimit)
	if !o.GivenUp() || !strings.Contains(line, "twice") || !strings.Contains(line, "rest of the round") {
		t.Fatalf("not given up by the second stand-down: %q", line)
	}
	if o.Try() {
		t.Error("a given-up one-write accept was tried")
	}
}

// A cycle that was written is one in which the line DID show: it ends a run of waits, and clears the stand-downs.
func TestAWrittenCycleBreaksARunOfWaitsThatRanOut(t *testing.T) {
	o := NewTogetherOps(true)
	for round := 0; round < 5; round++ {
		for i := 0; i < TogetherWaitedOutLimit-1; i++ {
			if !o.Try() {
				t.Fatalf("round %d: not tried", round)
			}
			if line := o.NoteWaitedOut(); line != "" {
				t.Fatalf("round %d: said %q", round, line)
			}
		}
		o.Try()
		o.Note(DirectOK, "")
	}
	if o.GivenUp() || !o.Try() {
		t.Fatal("waits broken up by cycles that worked stood it down")
	}
	// A stand-down is forgotten once a cycle works after it.
	for i := 0; i < TogetherWaitedOutLimit; i++ {
		o.Try()
		o.NoteWaitedOut()
	}
	for o.Try() == false {
	}
	o.Note(DirectOK, "")
	for i := 0; i < TogetherWaitedOutLimit; i++ {
		o.Try()
		o.NoteWaitedOut()
	}
	if o.GivenUp() {
		t.Error("a second stand-down after a cycle that worked gave it up")
	}
}
