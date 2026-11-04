package mulereceiverplan

import "testing"

func TestDecideIdleWork(t *testing.T) {
	// the mule's real figures on 2026-09-21, for scale: Cooking 2,371,380 and Fishing 750. The
	// switch is a long way off, which is exactly why the rule is pinned here rather than
	// discovered live.
	const live = 2_371_380

	for _, c := range []struct {
		name          string
		enabled       bool
		cooking, fish int
		want          IdleWork
	}{
		{"off: never does idle work at all", false, MaxSkillXP, MaxSkillXP, IdleNone},
		{"off even with everything to do", false, 0, 0, IdleNone},
		{"today: cooks", true, live, 750, IdleCook},
		{"one xp short of the cap: still cooks", true, MaxSkillXP - 1, 0, IdleCook},
		{"cooking capped: fishes", true, MaxSkillXP, 0, IdleFish},
		{"cooking past the cap: still fishes", true, MaxSkillXP + 1, 12345, IdleFish},
		{"fishing one short: still fishes", true, MaxSkillXP, MaxSkillXP - 1, IdleFish},
		{"both capped: nothing left", true, MaxSkillXP, MaxSkillXP, IdleNone},
	} {
		if got := DecideIdleWork(c.enabled, c.cooking, c.fish); got != c.want {
			t.Errorf("%s: DecideIdleWork(%v, %d, %d) = %q, want %q",
				c.name, c.enabled, c.cooking, c.fish, got, c.want)
		}
	}
}

// The order matters: cooking must not be abandoned while it still pays, because the fleet's
// banked raw fish is what the mule is cooking and nobody else is.
func TestCookingIsNotAbandonedEarly(t *testing.T) {
	for xp := 0; xp < MaxSkillXP; xp += MaxSkillXP / 8 {
		if got := DecideIdleWork(true, xp, 0); got != IdleCook {
			t.Fatalf("cooking xp %d: got %q, want %q", xp, got, IdleCook)
		}
	}
}
