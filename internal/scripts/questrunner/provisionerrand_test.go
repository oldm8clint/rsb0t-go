package questrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// errandMule publishes a Raw shark stock and answers every ask the way the fleet does when the mule is on another world.
type errandMule struct{ asks int }

func (m *errandMule) Published() (map[int]int, bool) { return map[int]int{383: 93000}, true }
func (m *errandMule) Ask(string, []questplan.ItemCount) (map[int]int, string) {
	m.asks++
	return map[int]int{}, "the mule is on another world: " + ErrandMarker + " (scarabswarm_world.go)"
}

// Olaf's Raw shark has no source but the bank and the mule. The mule's ask rides a world errand, so the round must say so (the
// engine then parks the quest for the errand) instead of ending as an anonymous dead kit.
func TestAKitWhoseMuleAskRidesAWorldErrandSaysSo(t *testing.T) {
	e := fakeEngineAt(&logSink{}, map[string]int{})
	p := emptyBankProvisioner()
	m := &errandMule{}
	p.Mule = m
	kit := []questplan.KitLine{{ID: 383, Name: "Raw shark", Qty: 1}}
	ok, _ := p.Provision(e.D, "frem-olaf-kit", kit, nil, tile.Tile{X: 3092, Z: 3245})
	if ok {
		t.Fatal("the kit cannot be filled this pass")
	}
	if m.asks != 1 {
		t.Errorf("the mule was asked %d times, want one bundled ask", m.asks)
	}
	if why := p.TakeErrand(); why == "" {
		t.Error("the errand deferral was not recorded")
	}
	if p.TakeErrand() != "" {
		t.Error("the errand note was read twice")
	}
}
