package fleetrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The seek memory is what turns "nearest spawn" into a tour: a spawn walked to is remembered, and a nil memory (a
// Deps built without one) forgets nothing and does not panic.
func TestPickSeekRemembersTheSpawnsWalkedTo(t *testing.T) {
	var s PickSeek
	spawn := tile.Tile{X: 2669, Z: 3298}
	if _, ok := s.wentAt(spawn); ok {
		t.Fatal("a spawn nobody walked to is remembered")
	}
	at := time.Now()
	s.note(spawn, at)
	if got, ok := s.wentAt(spawn); !ok || !got.Equal(at) {
		t.Fatalf("wentAt = %v %v, want %v", got, ok, at)
	}
	if _, ok := s.wentAt(tile.Tile{X: 2652, Z: 3318}); ok {
		t.Fatal("another spawn is remembered")
	}
	var none *PickSeek
	none.note(spawn, at)
	if _, ok := none.wentAt(spawn); ok {
		t.Fatal("a nil memory remembers")
	}
}
