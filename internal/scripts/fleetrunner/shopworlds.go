package fleetrunner

// One shop board per game world.
//
// WHY (the owner's world-2 ruling, 2026-09-23: "the Scarab cohort runs on world 2 and hops to
// world 1 to trade with the mule"). Lundail's and Aubury's stock is scope=shared -- one shelf for
// the whole WORLD -- and every world is its own server, so world 2's Aubury is not world 1's. A
// single board would let a world-1 account's reading ("Aubury: 0 mind runes") keep the whole
// world-2 cohort out of a full shop, or a world-2 reading send world-1 accounts to an empty one.
// A cohort that is all on world 2 would be fine either way; one with strays (a failed hop home, a
// failover to world 3, a staged rollout that starts on world 1) is not.

import (
	"sync"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/worlds"
)

var (
	shopWorldsMu sync.Mutex
	shopWorlds   = map[int]*fleetplan.ShopBoard{}
)

// ShopsFor is world's board. The default world (and an unknown world, 0 or less, which is what a
// one-world deployment and every standalone tool report) is Shops itself, so single-world code
// and the lab harnesses that seed Shops directly see exactly the board they always did.
func ShopsFor(world int) *fleetplan.ShopBoard {
	if world <= 0 || world == worlds.Default {
		return Shops
	}
	shopWorldsMu.Lock()
	defer shopWorldsMu.Unlock()
	b, ok := shopWorlds[world]
	if !ok {
		b = fleetplan.NewShopBoard()
		shopWorlds[world] = b
	}
	return b
}
