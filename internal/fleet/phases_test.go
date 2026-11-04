package fleet

import (
	"testing"
	"time"
)

func TestGuardianActivityFollowsHandlingLines(t *testing.T) {
	var g GuardianActivity
	t0 := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	g.Note("random event: Genie — handling", t0)
	if name, since := g.Current(t0.Add(time.Second)); name != "Genie" || !since.Equal(t0) {
		t.Fatalf("current = %q since %v", name, since)
	}
	// A progress line from the handler does not end it.
	g.Note("random event: Genie — talking", t0.Add(2*time.Second))
	if name, _ := g.Current(t0.Add(3 * time.Second)); name != "Genie" {
		t.Fatalf("a progress line ended the event: %q", name)
	}
	for _, terminal := range []string{"cleared", "still present after handling", "handler error: boom", "rejected for this account; checking other events next"} {
		g.Note("random event: Genie — handling", t0)
		g.Note("random event: Genie — "+terminal, t0.Add(4*time.Second))
		if name, _ := g.Current(t0.Add(5 * time.Second)); name != "" {
			t.Fatalf("%q did not end the event", terminal)
		}
	}
}

func TestGuardianActivityCombatFleeLapses(t *testing.T) {
	var g GuardianActivity
	t0 := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	g.Note("in combat with no tracked event — fleeing immediately (streak 1, dist 20)", t0)
	if name, _ := g.Current(t0.Add(10 * time.Second)); name != "combat flee" {
		t.Fatalf("current = %q", name)
	}
	if name, _ := g.Current(t0.Add(combatFleeFor + time.Second)); name != "" {
		t.Fatalf("the combat flee never lapsed: %q", name)
	}
	// An open handling line lapses after the cap, and unrelated lines are ignored.
	g.Note("random event: Mime — handling", t0)
	g.Note("drifted far from home while waiting", t0)
	if name, _ := g.Current(t0.Add(handlingCap + time.Second)); name != "" {
		t.Fatalf("an unterminated event never lapsed")
	}
	g.Note("random event: Mime — handling", t0)
	g.Reset()
	if name, _ := g.Current(t0); name != "" {
		t.Fatal("reset kept the event")
	}
}
