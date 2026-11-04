package randomevents

import (
	"os"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
)

// componentTiles is every tile walkable from s on its level (the pack's own exit bits), up to max.
func componentTiles(pack *pathfind.Pack, s StrandTile, max int) map[StrandTile]bool {
	dx := [8]int{0, 1, 0, -1, 1, 1, -1, -1}
	dz := [8]int{1, 0, -1, 0, 1, -1, -1, 1}
	seen := map[StrandTile]bool{s: true}
	queue := []StrandTile{s}
	for len(queue) > 0 && len(seen) < max {
		cur := queue[0]
		queue = queue[1:]
		mask := pack.ExitMask(cur.X, cur.Z, cur.Level)
		for i := 0; i < 8; i++ {
			if mask&(1<<uint(i)) == 0 {
				continue
			}
			n := StrandTile{X: cur.X + dx[i], Z: cur.Z + dz[i], Level: cur.Level}
			if seen[n] || !pack.Walkable(n.X, n.Z, n.Level) {
				continue
			}
			seen[n] = true
			queue = append(queue, n)
		}
	}
	return seen
}

// pocketBox is the pocket's rectangle (strandsites.go): the tiles whose membership the carve-out decides.
var pocketBox = StrandRect{MinX: 3200, MaxX: 3239, MinZ: 9895, MaxZ: 9930, Level: 0}

// TestVarrockSewerFloorAgainstTheRealPack proves every number behind the pocket's carve-out against the 289 map data. The live finding
// (14 Oct 2026, build d5362bb2): of the box of "the underground pocket below Varrock", most of what is not the summon's ground is the
// sewers' own floor, the corridor of the route to the moss giants, which the walker crosses on purpose; three accounts were held there as
// stranded, one for 33 minutes of void hops toward a rally no plain walk could reach from it. The first fix took the 217-tile north cavern
// out and left the rest of the corridor in; the review found 3210,9895-9897, the first tiles after the web, still read as the pocket.
// So the pocket here is the arrival's own ground and nothing else: a tile of the box is in it exactly when the pack joins that tile to
// the arrival, whatever else lies in the rectangle.
//
// Needs RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG; without them it skips.
func TestVarrockSewerFloorAgainstTheRealPack(t *testing.T) {
	pack := realCollisionPack(t)
	ixPath, cfgPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if ixPath == "" || cfgPath == "" {
		t.Skip("set RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG")
	}
	ix, err := locindex.Load(ixPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	nav := func(p StrandTile) pathfind.NavPoint { return pathfind.NavPoint{X: p.X, Z: p.Z, Level: p.Level} }
	arrival := StrandTile{X: 3217, Z: 9910}
	big := componentTiles(pack, arrival, 200000)
	if len(big) < 20000 {
		t.Fatalf("the summon's ground is %d tiles, want the dungeon network of tens of thousands", len(big))
	}

	// 1. The table is the sewers' floor: exactly the walkable tiles of the box that the pack does not join to the arrival, and nothing else.
	outside := map[StrandTile]bool{}
	summon := 0
	for z := pocketBox.MinZ; z <= pocketBox.MaxZ; z++ {
		for x := pocketBox.MinX; x <= pocketBox.MaxX; x++ {
			p := StrandTile{X: x, Z: z}
			switch {
			case !pack.Walkable(x, z, 0):
			case big[p]:
				summon++
			default:
				outside[p] = true
			}
		}
	}
	if summon == 0 || len(outside) == 0 {
		t.Fatalf("the box holds %d tiles of the summon's ground and %d of other floor, want both", summon, len(outside))
	}
	tableTiles := 0
	for z, spans := range varrockSewerFloor {
		for _, sp := range spans {
			for x := sp[0]; x <= sp[1]; x++ {
				tableTiles++
				if !outside[StrandTile{X: x, Z: z}] {
					t.Errorf("the table holds %d,%d, which is not a walkable box tile outside the summon's ground", x, z)
				}
			}
		}
	}
	if tableTiles != len(outside) {
		t.Errorf("the table holds %d tiles, the pack's floor outside the summon's ground %d", tableTiles, len(outside))
	}
	for k := range outside {
		if !onVarrockSewerFloor(k.X, k.Z, k.Level) {
			t.Errorf("the pack's sewer floor tile %d,%d is not in the table", k.X, k.Z)
		}
	}

	// The north cavern, the piece the first fix carved out, is all in it; so is the moss giants' ground beside the web.
	cavern := componentTiles(pack, StrandTile{X: 3230, Z: 9905}, 100000)
	if len(cavern) != 217 {
		t.Errorf("the cavern is %d tiles, want the 217 the catalogue names", len(cavern))
	}
	inBox := 0
	for k := range cavern {
		if !pocketBox.Contains(k.X, k.Z, k.Level) {
			continue
		}
		inBox++
		if !onVarrockSewerFloor(k.X, k.Z, k.Level) {
			t.Errorf("the cavern tile %d,%d is not in the table", k.X, k.Z)
		}
	}
	if inBox != 210 {
		t.Errorf("%d cavern tiles in the box, want 210", inBox)
	}
	moss := StrandTile{X: 3159, Z: 9900}
	south := componentTiles(pack, StrandTile{X: 3210, Z: 9897}, 200000)
	if len(south) != 552 || !south[moss] {
		t.Errorf("the ground beyond the web is %d tiles and reaches the moss giants' rung %v: %v, want 552 and true", len(south), moss, south[moss])
	}
	for _, c := range sewerRouteTiles {
		if !south[StrandTile{X: c[0], Z: c[1]}] || !outside[StrandTile{X: c[0], Z: c[1]}] {
			t.Errorf("%d,%d is not moss giants' ground outside the summon's", c[0], c[1])
		}
	}
	// The other pieces the box holds, by the size the catalogue's comment gives them.
	for c, want := range sewerSideSizes {
		if got := len(componentTiles(pack, StrandTile{X: c[0], Z: c[1]}, 200000)); got != want || !outside[StrandTile{X: c[0], Z: c[1]}] {
			t.Errorf("the piece of floor at %d,%d is %d tiles (outside the summon's ground: %v), want %d", c[0], c[1], got, outside[StrandTile{X: c[0], Z: c[1]}], want)
		}
	}
	for _, c := range sewerFloorTiles() {
		if !outside[StrandTile{X: c[0], Z: c[1]}] {
			t.Errorf("%d,%d is not a walkable tile outside the summon's ground", c[0], c[1])
		}
	}
	for _, c := range summonGroundTiles {
		if !big[StrandTile{X: c[0], Z: c[1]}] {
			t.Errorf("%d,%d is not the summon's ground", c[0], c[1])
		}
	}

	// 2. The carve-out is exact: over the whole box, every walkable tile of the summon's ground is in the pocket, and every walkable tile that is
	// not is in no site at all: not the pocket, not the no-walk-home strip, not a pocket the walker plans from, not a tile the Guardian owns or
	// plans, whatever the learned sites say.
	var pocket, strip *StrandSite
	for i := range strandSites {
		switch strandSites[i].Name {
		case "the underground pocket below Varrock (3217,9910 summons)":
			pocket = &strandSites[i]
		case "the dungeons south of the underground Wilderness (no walk home)":
			strip = &strandSites[i]
		}
	}
	if pocket == nil || strip == nil || !pocket.HandBack || pocket.Except == nil || strip.Except == nil {
		t.Fatal("the pocket and the strip are not catalogued with the carve-out")
	}
	for z := pocketBox.MinZ; z <= pocketBox.MaxZ; z++ {
		for x := pocketBox.MinX; x <= pocketBox.MaxX; x++ {
			p := StrandTile{X: x, Z: z}
			if !pack.Walkable(x, z, 0) {
				continue
			}
			if big[p] {
				if !pocket.Contains(x, z, 0) {
					t.Errorf("%d,%d is the summon's ground and not in the pocket", x, z)
				}
				if s, ok := StrandSiteAt(x, z, 0); !ok || s.Name != pocket.Name {
					t.Errorf("%d,%d is the summon's ground and StrandSiteAt says %q %v", x, z, s.Name, ok)
				}
				continue
			}
			if pocket.Contains(x, z, 0) || strip.Contains(x, z, 0) {
				t.Errorf("%d,%d is sewer floor and in the pocket (%v) or the strip (%v)", x, z, pocket.Contains(x, z, 0), strip.Contains(x, z, 0))
			}
			if s, ok := StrandSiteAt(x, z, 0); ok {
				t.Errorf("%d,%d is sewer floor and in the site %q", x, z, s.Name)
			}
			if name, ok := PocketAt(x, z, 0); ok {
				t.Errorf("%d,%d is sewer floor and a pocket (%q)", x, z, name)
			}
			if name, ok := StrandOwner(x, z, 0); ok {
				t.Errorf("%d,%d is sewer floor and owned by the Guardian's ladder (%q)", x, z, name)
			}
			if name, ok := StrandPlanned(x, z, 0); ok {
				t.Errorf("%d,%d is sewer floor and planned by the Guardian (%q)", x, z, name)
			}
			if name, ok := StrandCompoundAt(x, z, 0); ok {
				t.Errorf("%d,%d is sewer floor and inside the walls of the site %q", x, z, name)
			}
		}
	}

	// 3. Why the void hops could not get out: no plain walk joins the sewer floor to the PvP rally, while the summon's arrival has one.
	rally := StrandTile{X: pvpRallySouth.X, Z: pvpRallySouth.Z, Level: 0}
	if !big[rally] {
		t.Errorf("the rally %v is not on the summon's ground", rally)
	}
	for _, c := range sewerFloorTiles() {
		if big[StrandTile{X: c[0], Z: c[1]}] {
			t.Errorf("the sewer floor tile %d,%d is joined to the rally: the carve-out is wrong", c[0], c[1])
		}
	}
	// (FindPath snaps a start that has no neighbours to the nearest tile that has some, so it is asked of the tiles with a piece of floor
	// to stand on: the log's and the route's.)
	for _, c := range append(append([][2]int{}, cavernLogTiles...), sewerRouteTiles...) {
		if r := pack.FindPath(nav(StrandTile{X: c[0], Z: c[1]}), nav(rally)); r.OK {
			t.Errorf("a plain walk joins the sewer floor tile %d,%d to the rally: the carve-out is wrong", c[0], c[1])
		}
	}
	if r := pack.FindPath(nav(arrival), nav(rally)); !r.OK {
		t.Errorf("no walk from the summon's arrival %v to the rally %v: the PvP walk would have no route either", arrival, rally)
	}

	// 4. The cavern's real exits are where the walker's graph has them.
	has := func(x, z, id, op int, label string) {
		t.Helper()
		for _, l := range ix.At(x, z, 0) {
			if l.ID != id {
				continue
			}
			lt, ok := cfg.Loc(id)
			if !ok || lt.Ops[op-1] != label {
				t.Errorf("loc %d at %d,%d: option %d is %q, want %q", id, x, z, op, lt.Ops[op-1], label)
			}
			return
		}
		t.Errorf("no loc %d at %d,%d", id, x, z)
	}
	has(3241, 9910, 1530, 1, "Open")
	has(3210, 9898, 733, 1, "Slash")
	has(3230, 9904, 1755, 1, "Climb-up")
	// The web leads from the cavern's 3210,9899 to 3210,9897, ground of its own that runs on to the moss giants' rung; the door from 3241,9910
	// to 3241,9911, ground of its own too.
	if !cavern[StrandTile{X: 3210, Z: 9899}] || cavern[StrandTile{X: 3210, Z: 9897}] || !south[moss] {
		t.Errorf("the web at 3210,9898 does not join the cavern to the ground that reaches the moss giants (cavern side %v, far side in the cavern %v, far side reaches %v %v)",
			cavern[StrandTile{X: 3210, Z: 9899}], cavern[StrandTile{X: 3210, Z: 9897}], moss, south[moss])
	}
	if beyond := (StrandTile{X: 3241, Z: 9911}); cavern[beyond] || big[beyond] {
		t.Errorf("the door's far side %v is the cavern or the big ground: it is not a door between them", beyond)
	}

	// 5. The north ladder is no way out: its top is a 33-tile pocket that joins nothing.
	top := componentTiles(pack, StrandTile{X: 3230, Z: 3505}, 5000)
	if len(top) != 33 {
		t.Errorf("the pocket above the north ladder is %d tiles, want the 33 the catalogue's comment names", len(top))
	}
	if top[StrandTile{X: 3213, Z: 3428}] || top[StrandTile{X: 3237, Z: 3459}] {
		t.Error("the pocket above the north ladder reaches Varrock")
	}
}
