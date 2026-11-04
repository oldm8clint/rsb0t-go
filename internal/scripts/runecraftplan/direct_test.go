package runecraftplan

import (
	"strings"
	"testing"
)

// A round starts with both legs direct, and a leg that works stays direct.
func TestDirectOpsStartDirectAndStayDirectWhileTheyWork(t *testing.T) {
	d := NewDirectOps(true)
	for i := 0; i < 50; i++ {
		for _, l := range []Leg{LegAltar, LegPortal} {
			if !d.Try(l) {
				t.Fatalf("cycle %d: the %s leg was not tried", i, l)
			}
			if line := d.Note(l, DirectOK, ""); line != "" {
				t.Fatalf("a leg that works said %q", line)
			}
		}
	}
	if d.Tried(LegAltar) != 50 || d.Tried(LegPortal) != 50 || d.GivenUp(LegAltar) {
		t.Errorf("tried %d and %d", d.Tried(LegAltar), d.Tried(LegPortal))
	}
}

// ONE failure sends the leg back to walk-then-click for DirectBackoff cycles of that leg; the other leg is untouched; then it is tried again.
func TestAFailedLegIsPlainForTheBackoffAndThenTriedAgain(t *testing.T) {
	d := NewDirectOps(true)
	if !d.Try(LegAltar) {
		t.Fatal("not tried")
	}
	line := d.Note(LegAltar, DirectFailed, "the engine said: I can't reach that")
	for _, want := range []string{"altar leg failed", "I can't reach that", "walking to the stand and clicking instead", "10 cycle(s)"} {
		if !strings.Contains(line, want) {
			t.Errorf("the line lacks %q: %s", want, line)
		}
	}
	for i := 1; i <= DirectBackoff; i++ {
		if d.Try(LegAltar) {
			t.Fatalf("plain cycle %d of %d was tried as a direct op", i, DirectBackoff)
		}
		if !d.Try(LegPortal) {
			t.Fatalf("the portal leg was held back by the altar's failure at cycle %d", i)
		}
	}
	if !d.Try(LegAltar) {
		t.Fatalf("not tried again after %d plain cycles", DirectBackoff)
	}
	if line := d.Note(LegAltar, DirectOK, ""); !strings.Contains(line, "worked again") {
		t.Errorf("the recovery said %q", line)
	}
	if line := d.Note(LegAltar, DirectOK, ""); line != "" {
		t.Errorf("a second success said %q", line)
	}
}

// DirectGiveUp failures in a row end the leg for the round; a success in between starts the count again.
func TestAGivenUpLegStaysPlainAndASuccessResetsTheCount(t *testing.T) {
	d := NewDirectOps(true)
	fail := func() string {
		if !d.Try(LegPortal) {
			t.Fatal("a due attempt was not tried")
		}
		return d.Note(LegPortal, DirectFailed, "nothing happened")
	}
	wait := func() {
		for i := 0; i < DirectBackoff; i++ {
			if d.Try(LegPortal) {
				t.Fatalf("tried in the back-off at %d", i)
			}
		}
	}
	fail()
	wait()
	fail()
	wait()
	// A success clears the two failures: the next ones start from nothing.
	if !d.Try(LegPortal) {
		t.Fatal("not tried after the back-off")
	}
	d.Note(LegPortal, DirectOK, "")
	for i := 0; i < DirectGiveUp-1; i++ {
		if line := fail(); strings.Contains(line, "rest of the round") {
			t.Fatalf("given up after %d failures since a success: %s", i+1, line)
		}
		wait()
	}
	line := fail()
	if !strings.Contains(line, "failed 3 times in a row") || !strings.Contains(line, "for the rest of the round") {
		t.Fatalf("the give-up line: %s", line)
	}
	if !d.GivenUp(LegPortal) || d.GivenUp(LegAltar) {
		t.Errorf("given up: portal %v altar %v", d.GivenUp(LegPortal), d.GivenUp(LegAltar))
	}
	for i := 0; i < 100; i++ {
		if d.Try(LegPortal) {
			t.Fatal("a given-up leg was tried")
		}
	}
	if !d.Try(LegAltar) {
		t.Error("the altar leg was given up with the portal's")
	}
}

// A result that was not SEEN in a lagging view says nothing about the op: no back-off, no count toward giving up.
func TestAnUnseenResultChangesNothing(t *testing.T) {
	d := NewDirectOps(true)
	for i := 0; i < 10; i++ {
		if !d.Try(LegAltar) {
			t.Fatalf("attempt %d not tried", i)
		}
		if line := d.Note(LegAltar, DirectUnseen, "the view was lagging"); line != "" {
			t.Fatalf("an unseen result said %q", line)
		}
	}
	if d.GivenUp(LegAltar) || d.Tried(LegAltar) != 10 {
		t.Errorf("given up %v, tried %d", d.GivenUp(LegAltar), d.Tried(LegAltar))
	}
	// Two failures with unseen results between them are still two in a row: lag does not forgive a failure either.
	d.Try(LegAltar)
	d.Note(LegAltar, DirectFailed, "x")
	for i := 0; i < DirectBackoff; i++ {
		d.Try(LegAltar)
	}
	d.Try(LegAltar)
	d.Note(LegAltar, DirectUnseen, "lag")
	d.Try(LegAltar)
	if line := d.Note(LegAltar, DirectFailed, "y"); strings.Contains(line, "rest of the round") {
		t.Errorf("given up after two failures: %s", line)
	}
}

// Off, and a nil state, never try and never say anything: the plain walk-then-click is every cycle.
func TestDirectOpsOffAndNilNeverTry(t *testing.T) {
	var nilOps *DirectOps
	for _, d := range []*DirectOps{nilOps, NewDirectOps(false)} {
		if d.On() || d.Try(LegAltar) || d.Try(LegPortal) || d.Note(LegAltar, DirectFailed, "x") != "" || d.GivenUp(LegAltar) || d.Tried(LegAltar) != 0 {
			t.Errorf("%+v acted", d)
		}
		if got := d.Summary(); got != "direct ops: off" {
			t.Errorf("summary %q", got)
		}
	}
	// A leg outside the table is never tried either.
	d := NewDirectOps(true)
	if d.Try(Leg(-1)) || d.Try(Leg(7)) || d.Note(Leg(7), DirectOK, "") != "" {
		t.Error("an unknown leg acted")
	}
}

func TestDirectOpsSummary(t *testing.T) {
	d := NewDirectOps(true)
	d.Try(LegAltar)
	d.Note(LegAltar, DirectOK, "")
	d.Try(LegPortal)
	d.Note(LegPortal, DirectFailed, "nothing happened")
	d.Try(LegPortal) // plain
	d.Try(LegAltar)
	d.Note(LegAltar, DirectUnseen, "")
	got := d.Summary()
	want := "direct ops: altar 1 direct, 0 failed, 1 not seen, 0 plain; portal 0 direct, 1 failed, 0 not seen, 1 plain"
	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

// The cycle's line says how each leg was done when a direct op was in play, and nothing at all when none was (the line the live proofs
// grep keeps its format).
func TestLegsLineTagsTheDirectOps(t *testing.T) {
	in := walkCycle()
	plain := MeasureLegs(in, legsSpots).Line()
	if strings.Contains(plain, "direct ops") {
		t.Fatalf("a plain cycle names direct ops: %s", plain)
	}
	in.AltarMode, in.PortalMode = ModeDirect, ModeDirect
	if line := MeasureLegs(in, legsSpots).Line(); !strings.HasSuffix(line, "; direct ops: altar direct, portal direct") || !strings.HasPrefix(line, plain) {
		t.Errorf("both direct: %s", line)
	}
	in.AltarMode, in.PortalMode = ModeFallback, ""
	if line := MeasureLegs(in, legsSpots).Line(); !strings.HasSuffix(line, "; direct ops: altar fell back, portal plain") {
		t.Errorf("a fallback: %s", line)
	}
}
