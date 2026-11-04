package mastercraftplan

import "testing"

func TestHerbRoundSkipsTheUntradeableQuestHerbs(t *testing.T) {
	for _, h := range HerbsCleanable(99) {
		if untradeableHerb[h.Key] {
			t.Fatalf("%s is untradeable and must not be cleaned for the mule", h.Name)
		}
	}
	ok, resume, _ := HerbsReady(Settings{MembersWorld: true, HerbThreshold: 1000}, 99, true, nil, map[int32]int{1533: 1})
	if ok || resume {
		t.Fatal("a Rogues purse in the bank must not start a herb round")
	}
}
