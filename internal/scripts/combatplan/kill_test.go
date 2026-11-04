package combatplan

import "testing"

func TestJudgeKill(t *testing.T) {
	cases := []struct {
		name   string
		before TargetState
		after  TargetState
		want   Verdict
	}{
		{
			"health observed at zero while still listed",
			TargetState{Index: 1, Present: true, Health: 3, TotalHealth: 10, FightingUs: true},
			TargetState{Index: 1, Present: true, Health: 0, TotalHealth: 10, FightingUs: true},
			Killed,
		},
		{
			"zero health seen, then it left the list",
			TargetState{Index: 1, Present: true, Health: 0, TotalHealth: 10, FightingUs: true},
			TargetState{Index: 1, Present: false},
			Killed,
		},
		{
			"vanished at low health while fighting us",
			TargetState{Index: 1, Present: true, Health: 2, TotalHealth: 10, FightingUs: true},
			TargetState{Index: 1, Present: false},
			LikelyKilled,
		},
		{
			"vanished at full health: it walked away",
			TargetState{Index: 1, Present: true, Health: 10, TotalHealth: 10, FightingUs: true},
			TargetState{Index: 1, Present: false},
			Vanished,
		},
		{
			"vanished having never been hit proves nothing",
			TargetState{Index: 1, Present: true},
			TargetState{Index: 1, Present: false},
			Vanished,
		},
		{
			"vanished at low health but it was not our fight",
			TargetState{Index: 1, Present: true, Health: 1, TotalHealth: 10, FightingUs: false},
			TargetState{Index: 1, Present: false},
			Vanished,
		},
		{
			"still up and taking damage",
			TargetState{Index: 1, Present: true, Health: 8, TotalHealth: 10, FightingUs: true},
			TargetState{Index: 1, Present: true, Health: 4, TotalHealth: 10, FightingUs: true},
			Alive,
		},
		{
			"listed but never hit",
			TargetState{Index: 1, Present: true},
			TargetState{Index: 1, Present: true},
			Unknown,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := JudgeKill(c.before, c.after); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// The whole point of the split: only an observed zero-health reading may be reported as a
// kill. A harness that counted LikelyKilled would be the "disappearance counted as the
// outcome" trap all over again.
func TestOnlyKilledIsConfirmed(t *testing.T) {
	if !Killed.Confirmed() {
		t.Error("Killed must be confirmed")
	}
	for _, v := range []Verdict{Alive, LikelyKilled, Vanished, Unknown} {
		if v.Confirmed() {
			t.Errorf("%q must not count as a confirmed kill", v)
		}
	}
}

// An NPC that has never been hit reads Health 0 and TotalHealth 0, which must never be
// mistaken for a corpse.
func TestUnhitNpcIsNotADeadNpc(t *testing.T) {
	fresh := TargetState{Index: 7, Present: true}
	if fresh.HealthKnown() {
		t.Fatal("no hitmark means no health reading")
	}
	if got := JudgeKill(fresh, fresh); got == Killed {
		t.Fatal("an NPC nobody has hit must not judge as killed")
	}
}

func TestDropFromKill(t *testing.T) {
	kills := []KillSite{{X: 100, Z: 100, Level: 0, AtMs: 1000}}
	const now, window = 2000, 5000

	if !DropFromKill(100, 100, 0, kills, now, window) {
		t.Error("a drop on the recorded tile is the kill's")
	}
	if !DropFromKill(98, 98, 0, kills, now, window) {
		t.Error("two tiles south-west is within the multi-tile NPC offset")
	}
	if DropFromKill(97, 100, 0, kills, now, window) {
		t.Error("three tiles west is outside the offset")
	}
	if DropFromKill(102, 102, 0, kills, now, window) {
		t.Error("the offset is directional: north-east is another player's drop")
	}
	if DropFromKill(100, 100, 1, kills, now, window) {
		t.Error("a different level is a different place")
	}
	if DropFromKill(100, 100, 0, kills, 99000, window) {
		t.Error("a kill long past must not claim a fresh drop")
	}
	if DropFromKill(100, 100, 0, nil, now, window) {
		t.Error("no kills means no attribution")
	}
}

func TestDecideApproach(t *testing.T) {
	cases := []struct {
		name                       string
		distance, takeRadius, leas int
		want                       ApproachDecision
	}{
		{"adjacent", 1, 1, 15, TakeNow},
		{"on top of it", 0, 1, 15, TakeNow},
		{"a few tiles away", 5, 1, 15, WalkCloser},
		{"beyond the leash", 40, 1, 15, TooFar},
		{"no leash walks any distance", 40, 1, 0, WalkCloser},
		{"a generous take radius", 3, 3, 15, TakeNow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DecideApproach(c.distance, c.takeRadius, c.leas); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
