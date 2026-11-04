package combatrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestClickAtOnceNeedsARangeAndAPack(t *testing.T) {
	target := tile.Tile{X: 3100, Z: 3500}
	if ClickAtOnce(&Deps{}, target) {
		t.Fatal("clicked at once with no click range")
	}
	if ClickAtOnce(&Deps{ClickRange: 10}, target) {
		t.Fatal("clicked at once with no collision pack to say the route is plain")
	}
}
