package mulerunner

import (
	"testing"

	"rsb0t-go/internal/gamestate"
)

// The Prayer rungs ask the mule for Bones, a name content 289 gives to several objects (the members' 280-283 come before the
// 526 every bank holds). The mule draws the object its bank holds and travels it as that object's own bank note, so a
// thousand-bone kit is the 527 note and not the first note of the name.
func TestTheMuleServesTheBonesItsBankHolds(t *testing.T) {
	cfg := kitConfig(t)
	bank := []gamestate.SlotItem{{Item: gamestate.Item{ID: 526, Count: 17154}}, {Item: gamestate.Item{ID: 532, Count: 1488}}}
	for _, c := range []struct {
		name       string
		item, note int32
	}{{"Bones", 526, 527}, {"Big bones", 532, 533}} {
		id, ok := bankedID(cfg, bank, c.name)
		if !ok || id != c.item {
			t.Errorf("%s resolves to %d (%v), want the banked %d", c.name, id, ok, c.item)
			continue
		}
		if note, ok := noteFor(cfg, id); !ok || note != c.note {
			t.Errorf("%s travels as note %d (%v), want %d", c.name, note, ok, c.note)
		}
	}
}
