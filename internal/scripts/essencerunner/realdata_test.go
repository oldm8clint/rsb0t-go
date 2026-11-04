package essencerunner

import (
	"os"
	"testing"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/essenceplan"
)

// The Nurmof trip's way home is where the code looks for it, in the fleet's own loc index and collision pack: the trapdoor
// down at the surface, the ladder up thirty tiles from Nurmof's stand, a path between them. Needs RSB0T_LOC_INDEX,
// RSB0T_CONFIG_JAG and RSB0T_COLLISION_PACK.
func TestTheDwarvenMineLadderIsWhereTheCodeLooks(t *testing.T) {
	ixPath, cfgPath, packPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG"), os.Getenv("RSB0T_COLLISION_PACK")
	if ixPath == "" || cfgPath == "" || packPath == "" {
		t.Skip("set RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG and RSB0T_COLLISION_PACK")
	}
	ix, err := locindex.Load(ixPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := pathfind.Load(packPath)
	if err != nil {
		t.Fatal(err)
	}
	trap := ix.Find(cfg, locindex.Query{X: essenceplan.NurmofHopFrom.X, Z: essenceplan.NurmofHopFrom.Z, Radius: 6, Name: essenceplan.NurmofHopLoc, Op: essenceplan.NurmofHopOp}, nil)
	if len(trap) != 1 {
		t.Fatalf("the trapdoor near %v: %d matches", essenceplan.NurmofHopFrom, len(trap))
	}
	ladder := ix.Find(cfg, locindex.Query{X: essenceplan.NurmofStand.X, Z: essenceplan.NurmofStand.Z, Radius: 60, Name: DwarvenLadderName, Op: DwarvenLadderOp}, nil)
	if len(ladder) != 1 || ladder[0].X != DwarvenLadder.X || ladder[0].Z != DwarvenLadder.Z {
		t.Fatalf("the ladder near Nurmof: %+v, want one at %v", ladder, DwarvenLadder)
	}
	res := pk.FindPath(pathfind.NavPoint{X: essenceplan.NurmofStand.X, Z: essenceplan.NurmofStand.Z}, pathfind.NavPoint{X: DwarvenLadder.X, Z: DwarvenLadder.Z - 1})
	if !res.OK {
		t.Fatalf("no path from Nurmof's stand to the ladder: %s", res.Reason)
	}
	t.Logf("trapdoor %d at %d,%d; ladder %d at %d,%d; %d waypoints from Nurmof's stand", trap[0].ID, trap[0].X, trap[0].Z, ladder[0].ID, ladder[0].X, ladder[0].Z, len(res.Waypoints))
}

// Underground reads the mapsquare the account stands in: the mine is z 9000 and up.
func TestUndergroundIsTheDwarvenMinesZ(t *testing.T) {
	for _, c := range []struct {
		x, z int
		want bool
	}{{3019, 3450, false}, {3019, 9849, true}, {2997, 9844, true}, {2594, 3089, false}, {2891, 4830, false}} {
		sim := actionsim.New(actionsim.Objs{}, nil)
		sim.StandAt(c.x, c.z)
		d, _ := gatewayDeps(sim)
		if got := Underground(d); got != c.want {
			t.Errorf("%d,%d underground = %v, want %v", c.x, c.z, got, c.want)
		}
	}
}

// With the account on the surface the ladder is not clicked and the call is already done.
func TestLeaveDwarvenMineOnTheSurfaceDoesNothing(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(3019, 3450)
	d, _ := gatewayDeps(sim)
	if !LeaveDwarvenMine(d, 3) || len(sim.Sent()) != 0 {
		t.Errorf("sent %d packets", len(sim.Sent()))
	}
}

// The gateways' "Teleport" option is where the design says in the item config: Aubury's is option 4 (after Trade and
// Talk-to...), the two wizards' option 3. The click goes out by NAME (cachecfg.OpIndex), so this pins the numbers the
// design cites, not what the code relies on.
func TestTheGatewaysTeleportOptionIsWhereTheDesignSays(t *testing.T) {
	cfgPath := os.Getenv("RSB0T_CONFIG_JAG")
	if cfgPath == "" {
		t.Skip("set RSB0T_CONFIG_JAG")
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		key string
		op  int
	}{{essenceplan.GatewayAubury, 4}, {essenceplan.GatewayGuild, 3}, {essenceplan.GatewayCromperty, 3}} {
		gw, _ := essenceplan.GatewayByKey(c.key)
		found := false
		for _, id := range cfg.NpcIDs(gw.NPC) {
			n, ok := cfg.Npc(id)
			if !ok {
				continue
			}
			if got := cachecfg.OpIndex(n.Ops, gw.Op); got != 0 {
				found = true
				if got != c.op {
					t.Errorf("%s (%s npc %d): %q is option %d, the design says %d", c.key, gw.NPC, id, gw.Op, got, c.op)
				}
			}
		}
		if !found {
			t.Errorf("%s: no npc named %q has a %q option", c.key, gw.NPC, gw.Op)
		}
	}
}
