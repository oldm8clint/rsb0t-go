package fleetplan

import (
	"os"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// packSightWithLocs is the line of sight as far as the baked data knows it: the pack's wall faces
// plus every centrepiece loc (shapes 10 and 11) whose type blocks projectiles, over its whole
// footprint. The pack alone cannot tell a tree from water (castreach.go), so without the loc index a
// safespot's sight would be claimed through the trees.
func packSightWithLocs(t *testing.T, pk *pathfind.Pack, around tile.Tile) func(x, z int) Sight {
	t.Helper()
	ip, cp := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if ip == "" || cp == "" {
		t.Skip("RSB0T_LOC_INDEX or RSB0T_CONFIG_JAG not set")
	}
	ix, err := locindex.Load(ip)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	full := map[[2]int]bool{}
	ix.Each(around.X-40, around.Z-40, around.X+40, around.Z+40, 0, func(l locindex.Loc) {
		if l.Shape != 10 && l.Shape != 11 {
			return
		}
		c, ok := cfg.Loc(l.ID)
		if !ok || !c.BlockRange {
			return
		}
		w, h := max(c.Width, 1), max(c.Length, 1)
		if l.Angle == 1 || l.Angle == 3 {
			w, h = h, w
		}
		for dx := 0; dx < w; dx++ {
			for dz := 0; dz < h; dz++ {
				full[[2]int{l.X + dx, l.Z + dz}] = true
			}
		}
	})
	return func(x, z int) Sight {
		s := Sight(pk.WallMask(x, z, 0) & 0xf)
		if full[[2]int{x, z}] {
			s |= SightFull
		}
		return s
	}
}
