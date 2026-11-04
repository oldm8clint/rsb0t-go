package consolidateplan

import "testing"

// The Ghostspeak amulet The Restless Ghost hands out is needed again by Nature Spirit (the server wants it
// WORN for every word with Filliman and Drezel's spirit). Promoting the Ghost to every account must not let
// the loot hand-off carry it to the mule: it is kit while a quest ladder is running, and for as long as it
// is worn. Banked with no ladder left it is loot, and that is safe too: Father Urhney re-issues it to an
// account that owns none anywhere ("I've lost the amulet.", questplan.UrhneyStop).
func TestTheGhostspeakAmuletIsNeverHandedToTheMuleWhileAQuestWantsIt(t *testing.T) {
	const amulet = "Ghostspeak amulet"
	if got, why := Classify(amulet, KitInput{QuestsPending: true}); got != KindKit {
		t.Errorf("banked while the quest ladder runs: %s (%s), want kit", got, why)
	}
	if got, why := Classify(amulet, KitInput{Worn: []string{amulet}}); got != KindKit {
		t.Errorf("worn after the ladder: %s (%s), want kit", got, why)
	}
}
