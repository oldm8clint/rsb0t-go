package donorrunner

import (
	"testing"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/tile"
)

// Measured on one 72-minute uninterrupted run, 2026-09-22: 225 hand-off loads drawn across 64
// accounts and 21 delivered; 204 donors logged "giving up waiting for the mule"; the mule yielded
// to an incoming trade 11 times against 29 cook rounds. The donor announced only once it was
// already within announceWithin of the stand, which gave the mule about ten seconds -- and
// winding down a cook round means comeBackAndBank: cross the Catherby door, walk to the bank,
// open it, deposit.

// The announcement must not be tied to arrival. This pins the property rather than the call
// site: a donor far from the stand still has something to tell the mule.
func TestTheMuleIsToldBeforeTheDonorArrives(t *testing.T) {
	bus := mulebus.NewBus()
	w := mulebus.NewWatcher()
	done := announceHandoff(&Deps{Bus: bus, Me: "acct168"}, Options{Mule: "mule", Drawn: 20})
	var ready int
	for _, m := range w.Poll(bus) {
		if m.Kind == mulebus.KindReady && m.To == "mule" && m.From == "acct168" {
			ready++
		}
	}
	if ready != 1 {
		t.Fatalf("KindReady messages addressed to the mule = %d, want 1", ready)
	}
	done()
	var cancel int
	for _, m := range w.Poll(bus) {
		if m.Kind == mulebus.KindCancel && m.From == "acct168" {
			cancel++
		}
	}
	if cancel != 1 {
		t.Fatalf("KindCancel on finish = %d, want 1 -- an announcement with no cancel holds "+
			"the mule at its post for the whole incomingTTL", cancel)
	}
}

// announceWithin still exists and still means "near the stand", because publishCollect keeps
// using it for a different question: never summon a TRAVELLING mule to wherever a donor
// happens to be standing mid-walk. If this constant is ever repurposed, that gate needs
// rechecking too.
func TestAnnounceWithinStillDescribesArrival(t *testing.T) {
	stand := tile.Tile{X: 2809, Z: 3441}
	near := tile.Tile{X: stand.X + announceWithin, Z: stand.Z}
	far := tile.Tile{X: stand.X + announceWithin + 1, Z: stand.Z}
	if tile.Chebyshev(near, stand) > announceWithin {
		t.Fatal("a tile exactly announceWithin away reads as far")
	}
	if tile.Chebyshev(far, stand) <= announceWithin {
		t.Fatal("a tile past announceWithin reads as near")
	}
}
