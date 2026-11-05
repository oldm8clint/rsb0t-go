package mulereceiverplan

import (
	"testing"
	"time"
)

func TestKitDue(t *testing.T) {
	const max = 2 * time.Minute
	cases := []struct {
		name        string
		quiet, owed bool
		age         time.Duration
		want        bool
	}{
		{"quiet always", true, true, 0, true},
		{"busy and fresh", false, false, 10 * time.Second, false},
		{"busy and overdue", false, false, 2 * time.Minute, true},
		{"busy, overdue but turn owed", false, true, 5 * time.Minute, false},
	}
	for _, c := range cases {
		if got := KitDue(c.quiet, c.owed, c.age, max); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestOrderServable(t *testing.T) {
	base := OrderGate{KitMaxDefer: 2 * time.Minute}
	if OrderServable(base) {
		t.Error("nothing queued must not be servable")
	}
	g := base
	g.Kits, g.FishersQuiet = 1, true
	if !OrderServable(g) {
		t.Error("a kit with a quiet queue is servable")
	}
	g.FishersQuiet = false
	if OrderServable(g) {
		t.Error("a fresh kit behind waiting fishers is not servable")
	}
	g.OldestKitAge = 3 * time.Minute
	if !OrderServable(g) {
		t.Error("an overdue kit is servable")
	}
	g.KitTurnOwed = true
	if OrderServable(g) {
		t.Error("the fishing queue's turn comes first")
	}
	s := base
	s.SingleOrders = 1
	if !OrderServable(s) {
		t.Error("a single-item order is served ahead of donors")
	}
	s.KitTurnOwed = true
	if OrderServable(s) {
		t.Error("a single order also waits for the fishing turn when fishers wait")
	}
	s.FishersQuiet = true
	if !OrderServable(s) {
		t.Error("a quiet queue ignores the owed turn")
	}
}

func TestLeaveWait(t *testing.T) {
	g := OrderGate{Kits: 1, FishersQuiet: true, KitMaxDefer: 2 * time.Minute}
	if !LeaveWait(g, 0, time.Minute) {
		t.Error("a servable kit ends the wait")
	}
	if LeaveWait(g, 1, time.Minute) {
		t.Error("a queued trade request is never delayed by leaving the wait")
	}
	if LeaveWait(g, 0, time.Second) {
		t.Error("a second early exit inside the gap must not happen (spin guard)")
	}
	g.FishersQuiet = false
	if LeaveWait(g, 0, time.Minute) {
		t.Error("a kit still held behind waiting fishers keeps waiting")
	}
}
