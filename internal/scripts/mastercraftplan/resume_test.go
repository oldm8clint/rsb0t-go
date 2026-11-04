package mastercraftplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/runecraftplan"
)

// A round on the road that keeps failing is not resumed at every check (the review of 13 Oct: runeResume ignored the backoff, so a
// round whose walk to the ruins failed was resumed a minute later, again and again, with the load still on the account). Two
// failures in a row hold the resume off until the backoff is over; inside a temple or the mine the resume is the way out and
// stands.
func TestRuneResumeStandsDownAfterTwoFailedRoundsOutsideATemple(t *testing.T) {
	onTheRoad := func(s *Snapshot) {
		s.Rune.PackEssence = 27
		s.Rune.PackTalisman = map[string]bool{runecraftplan.KeyAir: true}
	}
	set := runeSet()
	fail := func(led *Ledger, n int) {
		for i := 0; i < n; i++ {
			led.Fail(ModeRunecraft, runeNow.Add(-time.Minute), set)
		}
	}
	// No failure: resumed.
	if d := Decide(set, runeSnap(onTheRoad)); d.Mode != ModeRunecraft || !d.Resume {
		t.Fatalf("a load on the road: %+v", d)
	}
	// One failure (a backoff of 30 minutes is running): still resumed once.
	led := &Ledger{}
	fail(led, 1)
	if d := Decide(set, runeSnap(func(s *Snapshot) { onTheRoad(s); s.Ledger = led })); d.Mode != ModeRunecraft || !d.Resume {
		t.Fatalf("one failure: %+v", d)
	}
	// Two in a row while backing off: no resume, and the reason is the backoff's.
	led = &Ledger{}
	fail(led, 2)
	d := Decide(set, runeSnap(func(s *Snapshot) { onTheRoad(s); s.Ledger = led }))
	if d.Resume || d.Mode == ModeRunecraft {
		t.Fatalf("two failures while backing off: %+v", d)
	}
	// The backoff over: the resume is back (it carries the load on).
	if d := Decide(set, runeSnap(func(s *Snapshot) {
		onTheRoad(s)
		s.Ledger = led
		s.Now = runeNow.Add(24 * time.Hour)
	})); d.Mode != ModeRunecraft || !d.Resume {
		t.Fatalf("after the backoff: %+v", d)
	}
	// Inside a temple (or the mine) it stands whatever the ledger says: the round is the exit.
	for name, mod := range map[string]func(s *Snapshot){
		"in the Air temple": func(s *Snapshot) { s.Rune.Where, s.Rune.Temple = runecraftplan.RegionTemple, runecraftplan.KeyAir },
		"in the mine":       func(s *Snapshot) { s.Rune.Where = runecraftplan.RegionMine },
	} {
		led := &Ledger{}
		fail(led, 5)
		if d := Decide(set, runeSnap(func(s *Snapshot) { mod(s); s.Ledger = led })); d.Mode != ModeRunecraft || !d.Resume {
			t.Errorf("%s with five failures: %+v", name, d)
		}
	}
}

func TestNatureResumeAtTheGateStandsDownAfterTwoFailedRounds(t *testing.T) {
	set := natureSet()
	led := &Ledger{}
	for i := 0; i < 2; i++ {
		led.Fail(ModeNature, runeNow.Add(-time.Minute), set)
	}
	atGate := func(s *Snapshot) { s.Rune.AtGate = true; s.Ledger = led }
	if d := Decide(set, natureSnap(atGate)); d.Resume || d.Mode == ModeNature && d.Resume {
		t.Fatalf("at the gate after two failures: %+v", d)
	}
	// One failure only: resumed. Inside the Nature temple: always.
	one := &Ledger{}
	one.Fail(ModeNature, runeNow.Add(-time.Minute), set)
	if d := Decide(set, natureSnap(func(s *Snapshot) { s.Rune.AtGate = true; s.Ledger = one })); d.Mode != ModeNature || !d.Resume {
		t.Fatalf("one failure: %+v", d)
	}
	if d := Decide(set, natureSnap(func(s *Snapshot) {
		s.Rune.Where, s.Rune.Temple = runecraftplan.RegionTemple, runecraftplan.KeyNature
		s.Ledger = led
	})); d.Mode != ModeNature || !d.Resume {
		t.Fatalf("in the temple: %+v", d)
	}
}

// A stuck round's pause stands: the decision does not start another round while it lasts.
func TestAPausedRuneRoundIsNotStartedAgain(t *testing.T) {
	set := runeSet()
	led := &Ledger{}
	led.Rune.PauseUntil = runeNow.Add(time.Hour)
	led.Rune.LastStop = "no essence source"
	d := Decide(set, runeSnap(func(s *Snapshot) { s.Ledger = led }))
	if d.Mode == ModeRunecraft {
		t.Fatalf("paused: %+v", d)
	}
}

func TestKitAskRecentInTheLedger(t *testing.T) {
	var l RuneLedger
	now := runeNow
	if l.KitAskRecent(now) {
		t.Fatal("never asked")
	}
	l.KitAskedAt = now.Add(-time.Hour)
	if !l.KitAskRecent(now) {
		t.Fatal("an hour ago is inside the gap")
	}
	l.KitAskedAt = now.Add(-runecraftplan.KitAskGap - time.Minute)
	if l.KitAskRecent(now) {
		t.Fatal("outside the gap")
	}
	var nilLedger *RuneLedger
	if nilLedger.KitAskRecent(now) {
		t.Fatal("a nil ledger has not asked")
	}
}
