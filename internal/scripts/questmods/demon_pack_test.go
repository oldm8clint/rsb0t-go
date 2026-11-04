package questmods

// Demon Slayer's executors against the real data (RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG, and RSB0T_CONTENT for the
// cows' spawns; each test skips without them), the way blackknight_pack_test.go does for the fortress: the lookups the pour, the sink,
// the manhole and the sewer's ladder make find their locs where the plan says, the tile each is sent from is one questrunner's own
// LocStands accepts, and the cow pasture's cows are in reach of the farm's tile over open ground.

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

func TestDemonSlayerLocsFindTheirStands(t *testing.T) {
	d := csDeps(t)
	for _, c := range []struct {
		what       string
		id         int
		name, op   string
		near       tile.Tile
		radius     int
		x, z, lvl  int
		stand      tile.Tile
		standsOnly bool
	}{
		{"the drain", questplan.LocQuestDrain, "", "", questplan.DemonDrain, 4, 3225, 3495, 0, questplan.DemonDrain, true},
		{"the kitchen's sink", 0, "Sink", "", questplan.DemonSinkStand, 6, 3224, 3494, 0, questplan.DemonSinkStand, false},
		{"the manhole", questplan.LocManholeClosed, "", "Open", questplan.DemonManhole, 0, 3237, 3458, 0, questplan.DemonManholeStand, false},
		{"the sewer's ladder", questplan.LocCaveLadderUp, "", "Climb-up", questplan.DemonSewerLadder, 2, 3237, 9858, 0, questplan.DemonSewerLanding, false},
	} {
		m, ok := questrunner.FindLoc(d, c.id, c.name, c.op, c.near, c.radius)
		if !ok || m.X != c.x || m.Z != c.z || m.Level != c.lvl {
			t.Errorf("%s: FindLoc = %+v (%v), want %d,%d,%d", c.what, m, ok, c.x, c.z, c.lvl)
			continue
		}
		stands := questrunner.LocStands(d, m, c.stand)
		if !slices.Contains(stands, c.stand) {
			t.Errorf("%s: questrunner accepts %v for %s, not the plan's %v", c.what, stands, c.what, c.stand)
		}
		if c.standsOnly && !slices.Equal(stands, []tile.Tile{c.stand}) {
			t.Errorf("%s: stands %v, the plan's single stand is %v", c.what, stands, c.stand)
		}
		// The stand is the nearest accepted tile to itself, so ApproachLoc (which UseItemOnLoc runs) sends from where the account is.
		if len(stands) == 0 || stands[0] != c.stand {
			t.Errorf("%s: standing on %v, the first accepted tile is %v", c.what, c.stand, stands[0])
		}
		if !d.Pack.Walkable(c.stand.X, c.stand.Z, c.stand.Level) {
			t.Errorf("%s: the stand %v is not a floor tile", c.what, c.stand)
		}
	}
	// A manhole already opened by another account is the open manhole, found by its own id and its own op; the closed one is not found
	// by that op.
	if _, ok := questrunner.FindLoc(d, questplan.LocManholeClosed, "", "Climb down", questplan.DemonManhole, 0); ok {
		t.Error("the closed manhole offers Climb down")
	}
}

// The drain and the sewer's key are the same step's two ends: the pour's key is added at the tile the plan walks to.
func TestDemonSlayerKeyTileIsWhereThePourPutsIt(t *testing.T) {
	d := csDeps(t)
	if !d.Pack.Walkable(questplan.DemonSewerKey.X, questplan.DemonSewerKey.Z, 0) {
		t.Errorf("the key's tile %v is not a floor tile", questplan.DemonSewerKey)
	}
	// 0_50_154_25_41: mapsquare 50,154, local 25,41.
	if questplan.DemonSewerKey != (tile.Tile{X: 50*64 + 25, Z: 154*64 + 41}) {
		t.Errorf("the key's tile %v", questplan.DemonSewerKey)
	}
	// From the key, the ladder's foot is the first tile the sewer walk ends on: both on the pack's own path of fewer than 60 steps.
	r := d.Pack.FindPath(pathfind.NavPoint{X: questplan.DemonSewerKey.X, Z: questplan.DemonSewerKey.Z},
		pathfind.NavPoint{X: questplan.DemonSewerLanding.X, Z: questplan.DemonSewerLanding.Z})
	if !r.OK || r.Steps() > 60 {
		t.Errorf("key to the ladder's foot: ok %v, %d steps", r.OK, r.Steps())
	}
}

// demonCowSpawns reads the cow spawns (npc 81) of the two map squares the Lumbridge pasture is in from a Content checkout.
func demonCowSpawns(t *testing.T) []tile.Tile {
	t.Helper()
	root := os.Getenv("RSB0T_CONTENT")
	if root == "" {
		t.Skip("RSB0T_CONTENT not set")
	}
	var out []tile.Tile
	for _, sq := range []struct{ x, z int }{{50, 50}, {50, 51}} {
		b, err := os.ReadFile(filepath.Join(root, "maps", "m"+strconv.Itoa(sq.x)+"_"+strconv.Itoa(sq.z)+".jm2"))
		if err != nil {
			t.Skipf("no map square %d_%d: %v", sq.x, sq.z, err)
		}
		in := false
		for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
			if strings.HasPrefix(line, "==== ") {
				in = line == "==== NPC ===="
				continue
			}
			head, tail, ok := strings.Cut(line, ":")
			if !in || !ok {
				continue
			}
			f := strings.Fields(head)
			if len(f) != 3 || strings.TrimSpace(tail) != "81" {
				continue
			}
			lvl, _ := strconv.Atoi(f[0])
			x, _ := strconv.Atoi(f[1])
			z, _ := strconv.Atoi(f[2])
			out = append(out, tile.Tile{X: sq.x*64 + x, Z: sq.z*64 + z, Level: lvl})
		}
	}
	return out
}

// The old form of this test asked whether the cows were in reach of the pasture's middle (3259,3270), which they are, and passed for the
// whole time the live farm stood at the road outside the gate (the walk "within 8" of the middle ends there) and found no cow. The cows'
// reach is now tested from the tiles the farm's walks end on, in cowfarm_pack_test.go.

// Every id the plan carries names what the plan says it is in the production config the fleet's client reads, with the ops the module
// sends.
func TestDemonSlayerIdsMatchTheProductionConfig(t *testing.T) {
	_, _, cfg := csData(t)
	for id, want := range map[int]string{
		questplan.ObjKeyTraiborn: "Key", questplan.ObjKeyRovin: "Key", questplan.ObjKeyDrain: "Key", questplan.ObjSilverlight: "Silverlight",
		questplan.ObjBucket: "Bucket", questplan.ObjBucketOfWater: "Bucket of water", questplan.ObjBones: "Bones", questplan.ObjShark: "Shark",
		questplan.ObjBass: "Bass",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want || o.Noted() {
			t.Errorf("obj %d is %q (found %v, noted %v), the plan says %q", id, o.Name, ok, o.Noted(), want)
		}
	}
	// Silverlight is worn from the pack by its second held op, bones are buried and the foods eaten.
	if o, _ := cfg.Obj(questplan.ObjSilverlight); cachecfg.OpIndex(o.IOps, "Wield") != 2 {
		t.Errorf("Silverlight's held ops %v", o.IOps)
	}
	for id, want := range map[int]string{
		questplan.NpcGypsyAris: "Gypsy", questplan.NpcSirPrysin: "Sir Prysin", questplan.NpcCaptainRovin: "Captain Rovin", questplan.NpcTraiborn: "Traiborn",
		questplan.NpcDelrith: "Delrith", questplan.NpcDelrithWeakened: "Weakened Delrith", questplan.NpcYoungDarkWizard: "Dark wizard",
		questplan.NpcBeardedDarkWizard: "Dark wizard", questplan.NpcSewerZombie: "Zombie", questplan.NpcSewerGiantSpider: "Giant spider", 81: "Cow",
	} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (found %v), the plan says %q", id, n.Name, ok, want)
		}
	}
	// The four talks offer Talk-to, Delrith and the cows Attack, and the weakened Delrith nothing at all (the chat comes to the account).
	for _, id := range []int{questplan.NpcGypsyAris, questplan.NpcSirPrysin, questplan.NpcCaptainRovin, questplan.NpcTraiborn} {
		if n, _ := cfg.Npc(id); n.Ops[0] != "Talk-to" {
			t.Errorf("npc %d offers %v", id, n.Ops)
		}
	}
	for _, id := range []int{questplan.NpcDelrith, 81} {
		if n, _ := cfg.Npc(id); cachecfg.OpIndex(n.Ops, "Attack") != 2 {
			t.Errorf("npc %d offers %v", id, n.Ops)
		}
	}
	if n, _ := cfg.Npc(questplan.NpcDelrithWeakened); cachecfg.OpIndex(n.Ops, "Attack") != 0 {
		t.Errorf("the weakened Delrith offers %v: it must not be attackable", n.Ops)
	}
	for _, c := range []struct {
		id         int
		name, op   string
		opPosition int
	}{
		{questplan.LocQuestDrain, "Drain", "Search", 1}, {questplan.LocManholeClosed, "Manhole", "Open", 1}, {questplan.LocManholeOpen, "Manhole", "Climb down", 1},
		{questplan.LocCaveLadderUp, "Ladder", "Climb-up", 1}, {questplan.LocSink, "Sink", "", 0}, {questplan.LocSink2, "Sink", "", 0},
	} {
		l, ok := cfg.Loc(c.id)
		if !ok || l.Name != c.name {
			t.Errorf("loc %d is %q (found %v), the plan says %q", c.id, l.Name, ok, c.name)
			continue
		}
		if c.op != "" && cachecfg.OpIndex(l.Ops, c.op) != c.opPosition {
			t.Errorf("loc %d (%s) offers %v, want %q as op %d", c.id, l.Name, l.Ops, c.op, c.opPosition)
		}
	}
}
