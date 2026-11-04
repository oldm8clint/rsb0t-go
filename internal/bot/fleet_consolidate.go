package bot

import (
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// scarabConsolidateWalk bounds the walk to the mule's post.
const scarabConsolidateWalk = 8 * time.Minute

// consolidateSafePlace is a surface tile on the mule's side of White Wolf Mountain, outside the
// Wilderness and the Mage Bank: from there the post is a plain land walk.
func consolidateSafePlace(t *tile.Tile) bool {
	if t == nil || t.Level != 0 {
		return false
	}
	return fleetplan.WestOfTheMountain(*t) && fleetplan.WildernessLevel(*t) == 0 && !fleetplan.InMageBank(*t)
}

func consolidateSorted(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
