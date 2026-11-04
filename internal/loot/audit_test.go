package loot

import "testing"

func TestOnlyACasketIsAnUnvaluedContainer(t *testing.T) {
	if !IsUnvaluedContainer(ObjCasket) {
		t.Error("a casket is worth its contents, not its own price on top")
	}
	for _, id := range []int{OysterID, OysterPearlID, 995, 989} {
		if IsUnvaluedContainer(id) {
			t.Errorf("obj %d is valued as an item", id)
		}
	}
}

func TestBiteOfFollowsEachFoodToTheOneItLeaves(t *testing.T) {
	chains := [][]string{
		{"Cake", "2/3 cake", "Slice of cake"},
		{"Chocolate cake", "2/3 chocolate cake", "Chocolate slice"},
		{"Meat pizza", "1/2 meat pizza"},
		{"Apple pie", "Half an apple pie"},
	}
	for _, chain := range chains {
		for i, name := range chain {
			next, ok := BiteOf(name)
			if i == len(chain)-1 {
				if ok {
					t.Errorf("%s is the last bite but leaves %s", name, next)
				}
				continue
			}
			if !ok || next != chain[i+1] {
				t.Errorf("BiteOf(%q) = %q, %v, want %q", name, next, ok, chain[i+1])
			}
		}
	}
	if _, ok := BiteOf("Shark"); ok {
		t.Error("a shark is eaten whole")
	}
}

func TestLaterBiteNamesWhatABiteLeavesBehind(t *testing.T) {
	for _, n := range []string{"2/3 cake", "Slice of cake", "Chocolate slice", "1/2 meat pizza", "Half an apple pie"} {
		if !LaterBite(n) {
			t.Errorf("LaterBite(%q) = false", n)
		}
	}
	for _, n := range []string{"Cake", "Chocolate cake", "Meat pizza", "Apple pie", "Shark"} {
		if LaterBite(n) {
			t.Errorf("LaterBite(%q) = true: a whole food is costed", n)
		}
	}
}
