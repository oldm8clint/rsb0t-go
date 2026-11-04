package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/tile"
)

// The mule's heartbeat carries its POST as the Home tile, so a donor can meet it there even while
// it stands at the Catherby range on an idle cook round (the first live bug of the Scarab cohort,
// 2026-09-23: asks walked into the range house and timed out).
func TestTheMuleHeartbeatCarriesItsPostWhileItCooks(t *testing.T) {
	s := newAPISession()
	cook := tile.Tile{X: 2817, Z: 3443}
	rebuildAt(s.st, cook.X>>3, cook.Z>>3)
	placeAt(s.st, 0, cook.X-((cook.X>>3)-6)*8, cook.Z-((cook.Z>>3)-6)*8)
	bus := mulebus.NewBus()
	ctx := NewContext(s, "mule", nil, nil, nil, nil, bus, nil, nil, nil, func(string) {})
	b := &muleBot{}
	since := time.Now().Add(-time.Second)
	b.announce(ctx)
	hb, ok := bus.Latest(mulebus.KindMuleHere, "mule", since)
	if !ok {
		t.Fatal("no heartbeat")
	}
	if hb.Tile.X != cook.X || hb.Tile.Z != cook.Z {
		t.Fatalf("fixture: the heartbeat's live tile is %v, want the range %v", hb.Tile, cook)
	}
	if hb.Home == nil || *hb.Home != b.postTile() {
		t.Fatalf("the heartbeat's Home is %v, want the post %v", hb.Home, b.postTile())
	}
	if got := scarabMulePostFrom(hb); got != (tile.Tile{X: 2809, Z: 3441}) {
		t.Fatalf("a Scarab ask would walk to %v", got)
	}
}
