package questplan

import "testing"

func TestVampireSlayerWaitsForADefenceAndHitpointsFloor(t *testing.T) {
	live := PlayerState{SkillLevels: map[string]int{"attack": 31, "strength": 34, "defence": 1, "hitpoints": 29}}
	blocked := false
	for _, r := range CheckRequirements(VampireSlayerRecord, live) {
		if !r.OK {
			blocked = true
		}
	}
	if !blocked {
		t.Fatal("the live Defence-1, 29-HP account that died to the Count was let into Vampire Slayer")
	}
	ready := PlayerState{SkillLevels: map[string]int{"defence": 20, "hitpoints": 30}}
	for _, r := range CheckRequirements(VampireSlayerRecord, ready) {
		if !r.OK {
			t.Fatalf("an account at the floor was blocked: %s", r.Reason)
		}
	}
}

func TestPriestInPerilWaitsForAMeleeFloor(t *testing.T) {
	blocked := false
	for _, r := range CheckRequirements(PriestInPerilRecord, PlayerState{SkillLevels: map[string]int{"attack": 25, "defence": 5, "hitpoints": 28}}) {
		if !r.OK {
			blocked = true
		}
	}
	if !blocked {
		t.Fatal("a combat-28, Defence-5 account was let into Priest in Peril's guardian fight")
	}
	for _, r := range CheckRequirements(PriestInPerilRecord, PlayerState{SkillLevels: map[string]int{"attack": 30, "defence": 20, "hitpoints": 30}}) {
		if !r.OK {
			t.Fatalf("an account at the floor was blocked: %s", r.Reason)
		}
	}
}
