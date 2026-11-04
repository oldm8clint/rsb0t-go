package bot

import "testing"

// The food policy's default is the whole point of it, so it is pinned here rather than left for
// a live run to discover.
//
// Why it defaults OFF: with no mule, an empty bank is the end of the food supply. Every
// cooked-food stock line in a shop a free-to-play combat account can actually reach is 0 in
// content 289 — Port Sarim's bread and every cooked fish on Gerrant's shelf, and nothing edible
// at all in the Varrock, Falador, Lumbridge, Al Kharid, Edgeville, Barbarian Village, Draynor or
// Rimmington stores — and World.ts:1183 tops a stock line up only TOWARD its stockcount, so a 0
// never rises. There is no trip that fixes an empty bank, which is why carrying on is not a
// recovery. It was measured: that arm died 18 times, 13 of them after the food ran out.
func TestCombatTrainerFightsWithoutFoodOnlyWhenAsked(t *testing.T) {
	if s := resolveTrainer(map[string]interface{}{}); s.fightWithoutFood {
		t.Fatal("fightWithoutFood must default OFF: a run that cannot feed itself donates its levels to the respawn")
	}
	if s := resolveTrainer(map[string]interface{}{"fightWithoutFood": true}); !s.fightWithoutFood {
		t.Fatal("fightWithoutFood true should resolve true")
	}
	// The panel hands every value over as a string, which is the case that has broken settings
	// in this repo before.
	if s := resolveTrainer(map[string]interface{}{"fightWithoutFood": "true"}); !s.fightWithoutFood {
		t.Fatal(`fightWithoutFood "true" should resolve true`)
	}
	if s := resolveTrainer(map[string]interface{}{"fightWithoutFood": "false"}); s.fightWithoutFood {
		t.Fatal(`fightWithoutFood "false" should resolve false`)
	}
}

// A setting the schema does not declare is a setting the panel never offers and a stored value
// the resolver drops on the way in, so the declaration is worth its own assertion.
func TestCombatTrainerDeclaresTheFoodSetting(t *testing.T) {
	spec, ok := CombatTrainerSettings["fightWithoutFood"]
	if !ok {
		t.Fatal("the schema does not declare fightWithoutFood, so nothing can set it")
	}
	if spec.Default != false {
		t.Fatalf("schema default = %v, want false — the safe direction is to stop", spec.Default)
	}
}
