package swarmrally

import (
	"testing"
	"time"
)

func TestSeatsFirstCapacityMembersSeatedInRosterOrderRestWait(t *testing.T) {
	s := NewSeats(0, nil)
	s.Enqueue(1, []string{"a", "b", "c", "d"})
	for i, n := range []string{"a", "b"} {
		if idx, ok := s.Seat(n, 2); !ok || idx != i {
			t.Errorf("%s: seat %d ok %v, want %d", n, idx, ok, i)
		}
	}
	for _, n := range []string{"c", "d"} {
		if _, ok := s.Seat(n, 2); ok {
			t.Errorf("%s was seated past capacity", n)
		}
	}
	if s.Holders() != 2 || s.Waiting() != 2 {
		t.Errorf("holders %d waiting %d", s.Holders(), s.Waiting())
	}
}

func TestSeatsFreedSeatGoesToHeadOfQueue(t *testing.T) {
	s := NewSeats(0, nil)
	s.Enqueue(1, []string{"a", "b", "c", "d"})
	s.Seat("a", 2)
	s.Seat("b", 2)
	s.Release("a")
	if _, ok := s.Seat("d", 2); ok {
		t.Errorf("d jumped the queue")
	}
	if idx, ok := s.Seat("c", 2); !ok || idx != 0 {
		t.Errorf("c: seat %d ok %v, want the freed seat 0", idx, ok)
	}
}

func TestSeatsReleaseOfWaiterAndIdempotentEnqueue(t *testing.T) {
	s := NewSeats(0, nil)
	s.Enqueue(1, []string{"a", "b"})
	s.Enqueue(1, []string{"a", "b", "z"}) // the same wave again: nothing added
	if s.Waiting() != 2 {
		t.Errorf("waiting %d", s.Waiting())
	}
	s.Release("a")
	if s.Waiting() != 1 {
		t.Errorf("a left the queue, waiting %d", s.Waiting())
	}
	s.Enqueue(2, []string{"b", "c"}) // b already queued
	if s.Waiting() != 2 {
		t.Errorf("waiting %d after the second wave", s.Waiting())
	}
}

func TestSeatsStaleHolderLosesSeat(t *testing.T) {
	c := &fakeClock{t: time.Unix(1000, 0)}
	s := NewSeats(10*time.Second, c.now)
	s.Enqueue(1, []string{"a", "b"})
	s.Seat("a", 1)
	c.t = c.t.Add(11 * time.Second)
	if idx, ok := s.Seat("b", 1); !ok || idx != 0 {
		t.Errorf("b should take the silent holder's seat: %d %v", idx, ok)
	}
}

func TestSeatsShrinkingCapacityRequeuesHolder(t *testing.T) {
	s := NewSeats(0, nil)
	s.Enqueue(1, []string{"a", "b"})
	s.Seat("a", 2)
	s.Seat("b", 2)
	if _, ok := s.Seat("b", 1); ok {
		t.Errorf("b holds seat 1 of a one-seat lattice")
	}
	if s.Waiting() != 1 {
		t.Errorf("b should be waiting, waiting %d", s.Waiting())
	}
}

func TestSeatsUnrosteredNameGetsNothing(t *testing.T) {
	s := NewSeats(0, nil)
	if _, ok := s.Seat("nobody", 5); ok {
		t.Errorf("an account on no roster was seated")
	}
}

func TestSeatsAnchorHysteresis(t *testing.T) {
	s := NewSeats(0, nil)
	x, z, _, moved := s.Anchor(3474, 9496, 0, 1)
	if moved || x != 3474 || z != 9496 {
		t.Fatalf("first anchor %d,%d moved %v", x, z, moved)
	}
	if x, z, _, moved = s.Anchor(3475, 9497, 0, 1); moved || x != 3474 || z != 9496 {
		t.Errorf("a one-tile drift moved the anchor to %d,%d", x, z)
	}
	if x, z, _, moved = s.Anchor(3477, 9496, 0, 1); !moved || x != 3477 {
		t.Errorf("a three-tile drift kept the anchor at %d,%d", x, z)
	}
}

func TestRallyRosterFrozenAtLatch(t *testing.T) {
	c := &fakeClock{t: time.Unix(1000, 0)}
	r, err := New(Policy{Quorum: 2, MinEngage: 1, MaxWait: time.Hour, Stale: time.Minute}, c.now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Roster() != nil {
		t.Errorf("roster before the latch: %v", r.Roster())
	}
	r.Report("b", true)
	r.Report("a", true)
	r.Report("late", false)
	if !r.Decide().Verdict.Engaged() {
		t.Fatalf("not engaged: %+v", r.Decide())
	}
	got := r.Roster()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("roster %v", got)
	}
	r.Report("late", true)
	r.Leave("a")
	if got2 := r.Roster(); len(got2) != 2 {
		t.Errorf("the roster changed after the latch: %v", got2)
	}
}
