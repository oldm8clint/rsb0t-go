package fleetrunner

import (
	"os"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// lakeDuckSpawns is every Duck content m40_53 places (npc 44 x20, 45 x39), from its NPC section.
var lakeDuckSpawns = [][2]int{{2593, 3445}, {2593, 3454}, {2594, 3452}, {2595, 3447}, {2595, 3454}, {2596, 3439}, {2597, 3443}, {2597, 3446}, {2598, 3437}, {2598, 3454}, {2599, 3434}, {2600, 3440}, {2600, 3445}, {2601, 3436}, {2602, 3433}, {2604, 3430}, {2604, 3434}, {2605, 3432}, {2607, 3433}, {2613, 3422}, {2614, 3424}, {2614, 3449}, {2615, 3421}, {2615, 3425}, {2615, 3453}, {2616, 3423}, {2617, 3405}, {2617, 3410}, {2617, 3445}, {2617, 3448}, {2618, 3406}, {2618, 3422}, {2619, 3424}, {2619, 3447}, {2619, 3450}, {2620, 3403}, {2621, 3405}, {2621, 3410}, {2621, 3444},
	{2594, 3446}, {2595, 3449}, {2596, 3453}, {2598, 3440}, {2598, 3444}, {2600, 3435}, {2605, 3433}, {2615, 3423}, {2616, 3420}, {2616, 3447}, {2616, 3450}, {2617, 3407}, {2617, 3425}, {2619, 3409}, {2619, 3421}, {2619, 3445}, {2620, 3405}, {2620, 3407}, {2620, 3449}, {2621, 3446}}

// Each lake duck pen's Spawns is the count of Duck spawns its cast tiles reach with a spell over
// the real pack's walls (the runner's own packSight), and every Duck is on water no walk reaches:
// the reason the pens are magic-only.
func TestLakeDuckPensReachTheirSpawnsWithASpellAndNoneByWalking(t *testing.T) {
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	sight := PackSight(&Deps{Pack: pk})
	for _, d := range lakeDuckSpawns {
		if pk.Walkable(d[0], d[1], 0) {
			t.Errorf("the Duck spawn %v is walkable land", d)
		}
	}
	for _, p := range fleetplan.TrainPens {
		if !p.MagicOnly {
			continue
		}
		n := 0
		for _, d := range lakeDuckSpawns {
			if _, ok := p.CastTile(nil, tile.Tile{X: d[0], Z: d[1]}, sight); ok {
				n++
			}
		}
		if n < p.Spawns {
			t.Errorf("%s reaches %d Duck spawns with a spell, says %d", p.Name, n, p.Spawns)
		}
	}
}
