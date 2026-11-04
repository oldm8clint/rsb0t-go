package wcfletchplan_test

import (
	"fmt"
	"os"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/wcfletchplan"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/webwalk"
)

// TestMagicCampsAgreeWithTheLocIndex re-derives every magic camp's tree count and walk from the real
// loc index and pack and holds it to the yew camps' rules (maplecamps_data_test.go's shared checker,
// stand tiles within the leash included), with each camp's walk measured from its own bank. A content
// or pack change fails here instead of sending the magic cohort to a camp that is not there or past
// something that kills it.
//
//	RSB0T_LOC_INDEX=<locs.lcli.gz> RSB0T_CONFIG_JAG=<config archive> \
//	RSB0T_COLLISION_PACK=<collision.lcnav.gz> go test -run 'Magic' ./internal/scripts/wcfletchplan/
func TestMagicCampsAgreeWithTheLocIndex(t *testing.T) {
	checkCampsAgainstTheData(t, wcfletchplan.MagicCamps, true)
}

// TestNoOtherMagicTreeIsSafeAndInReach is the other half: every magic tree an account on the Seers side
// can walk to (woodcutplan.SeersSideRadius, where DecideWoodPhase still takes the Seers branch) is in a
// camp. Of content's thirteen the other six are out of it: three single trees in the Gnome Stronghold
// (235 to 354 tiles out, behind its gate, its bank upstairs where the walker has no stairs) and three in
// Tirannwn (behind the elf-lands quests, no route at all).
func TestNoOtherMagicTreeIsSafeAndInReach(t *testing.T) {
	checkNoOtherTreeIsSafeWithin(t, "Magic tree", wcfletchplan.MagicCamps, nil, woodcutplan.SeersSideRadius)
}

// bankCandidates is every bank a magic camp's account could carry its load to on foot: the ground-floor
// banks within 160 tiles of either camp that anyone may use. Left out, and why:
//   - the Legends' Guild bankers (2731-2733,3377-3381, upstairs), 87 steps from the garden: the guild's
//     gates need the Legends' Quest, and webwalk's edges through them carry no requirement;
//   - the Fishing Guild bank (2585,3419): Fishing 68, which this cohort does not train;
//   - the Gnome Stronghold banks (upstairs at 2444-2450,3424-3489): no route the walker can take.
var bankCandidates = []wcfletchplan.CampBank{
	wcfletchplan.SeersBank,
	{Name: "Catherby", Stand: tile.Tile{X: 2809, Z: 3440}, Floor: tile.Tile{X: 2809, Z: 3440}},
	{Name: "Ardougne north", Stand: tile.Tile{X: 2617, Z: 3333}, Floor: tile.Tile{X: 2617, Z: 3333}},
}

// TestEachMagicCampBanksAtItsNearestSafeBank measures the door-free walk from every candidate bank to
// every magic camp and holds the camp's Bank to the shortest one whose walk keeps clear of every hunter
// and danger zone (the camp rules' route margins): "each camp banks at its own nearest safe bank" is
// measured, not assumed. Today that is the Seers bank for both camps, so nothing in the bot's bank,
// fletch or hand-off steps had to learn another bank.
func TestEachMagicCampBanksAtItsNearestSafeBank(t *testing.T) {
	locsPath, cfgPath, packPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG"), os.Getenv("RSB0T_COLLISION_PACK")
	if locsPath == "" || cfgPath == "" || packPath == "" {
		t.Skip("set RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG and RSB0T_COLLISION_PACK")
	}
	if _, err := locindex.Load(locsPath); err != nil {
		t.Fatal(err)
	}
	if _, err := cachecfg.LoadFile(cfgPath); err != nil {
		t.Fatal(err)
	}
	pk, err := pathfind.Load(packPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bankCandidates {
		if !pk.Walkable(b.Floor.X, b.Floor.Z, 0) {
			t.Errorf("the %s bank floor %v is not walkable", b.Name, b.Floor)
		}
	}
	for _, c := range wcfletchplan.MagicCamps {
		best, bestSteps := "", 1<<30
		for _, b := range bankCandidates {
			route := pk.FindPath(pathfind.NavPoint{X: b.Floor.X, Z: b.Floor.Z}, pathfind.NavPoint{X: c.Anchor.X, Z: c.Anchor.Z})
			if !route.OK {
				t.Logf("%-24s %-15s no door-free route (%s)", c.Name, b.Name, route.Reason)
				continue
			}
			hazard := routeHazard(b.Floor, route.Waypoints)
			t.Logf("%-24s %-15s %3d steps %s", c.Name, b.Name, route.Steps(), hazard)
			if hazard == "" && route.Steps() < bestSteps {
				best, bestSteps = b.Name, route.Steps()
			}
		}
		if best == "" {
			t.Errorf("%s: no bank has a safe door-free walk to it", c.Name)
			continue
		}
		if got := c.BankOf(); got.Name != best || c.Steps != bestSteps {
			t.Errorf("%s banks at %s (%d steps); the nearest safe bank is %s at %d steps", c.Name, got.Name, c.Steps, best, bestSteps)
		}
	}
}

// routeHazard says what is wrong with a walk that starts at from and follows waypoints: the first danger
// zone a combat-3 account would enter, or the nearest hunter's reach closer than minRouteGap. "" when clear.
func routeHazard(from tile.Tile, waypoints []pathfind.NavPoint) string {
	gap, who, at := 1<<30, "", tile.Tile{}
	prev := from
	for _, w := range waypoints {
		next := tile.Tile{X: w.X, Z: w.Z}
		for _, step := range line(prev, next) {
			if z := zoneAt(step); z != "" {
				return fmt.Sprintf("(enters danger zone %s at %v)", z, step)
			}
			if g, h := huntGap(step); g < gap {
				gap, who, at = g, h, step
			}
		}
		prev = next
	}
	if gap < minRouteGap {
		return fmt.Sprintf("(passes %d tile(s) outside a %s's reach at %v)", gap, who, at)
	}
	return ""
}

// TestNoMagicCampWalkCrossesADoorOrAGate holds every walk an account makes between a magic camp, its bank and
// the mule to what the walker can do without opening anything. Planned the most permissively the walker
// plans (NPC hops and special crossings allowed, the automatic danger zones avoided for a combat-3
// account), each walk must still be one plain walk leg with no hop in it; and no tile of it may hold a door,
// gate or other wall loc with an Open or Close option, which is what a "door-free" pack route can still
// brush past. The Sorcerers' Tower garden is the case this is for: a hedged garden entered by a gap, whose
// walls are hedges (no option) and whose only gate-like loc is the tower's own door, nowhere on the route.
func TestNoMagicCampWalkCrossesADoorOrAGate(t *testing.T) {
	locsPath, cfgPath, packPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG"), os.Getenv("RSB0T_COLLISION_PACK")
	if locsPath == "" || cfgPath == "" || packPath == "" {
		t.Skip("set RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG and RSB0T_COLLISION_PACK")
	}
	ix, err := locindex.Load(locsPath)
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
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	planner := webwalk.NewPlanner(pk, g)
	// The scan must not be blind: Seers village and the tower beside the garden hold doors and gates.
	known := 0
	ix.Each(2690, 3390, 2735, 3495, 0, func(l locindex.Loc) {
		if info, ok := cfg.Loc(l.ID); ok && info.BlockWalk && (cachecfg.OpIndex(info.Ops, "Open") != 0 || cachecfg.OpIndex(info.Ops, "Close") != 0) {
			known++
		}
	})
	if known < 5 {
		t.Fatalf("only %d door or gate locs between the garden and the Seers bank: the scan below would pass blind", known)
	}
	t.Logf("%d door or gate locs between the garden and the Seers bank, none of them on a route", known)
	mule := tile.Tile{X: 2809, Z: 3441} // Catherby, the live muleAtTile
	for _, c := range wcfletchplan.MagicCamps {
		bank := c.BankOf().Floor
		for _, w := range []struct {
			what     string
			from, to tile.Tile
		}{
			{"bank to camp", bank, c.Anchor}, {"camp to bank", c.Anchor, bank},
			{"mule to camp", mule, c.Anchor}, {"camp to mule", c.Anchor, mule},
		} {
			from, to := webwalk.Point{X: w.from.X, Z: w.from.Z}, webwalk.Point{X: w.to.X, Z: w.to.Z}
			opt := webwalk.Options{AllowNPC: true, AllowSpecial: true}
			opt.AvoidZones = webwalk.ResolveDangerZones(nil, nil, webwalk.ZoneContext{IncludeAutomatic: true, CombatLevel: accountCombat, CombatKnown: true, Start: &from, Dest: &to})
			r := planner.Plan(from, to, opt)
			if !r.OK {
				t.Errorf("%s, %s: no route (%s)", c.Name, w.what, r.Reason)
				continue
			}
			if hops := r.Hops(); len(hops) != 0 {
				t.Errorf("%s, %s: the plan uses %d hop(s), first %s", c.Name, w.what, len(hops), hops[0])
			}
			doors := map[string]bool{}
			steps := 0
			for _, leg := range r.Legs() {
				prev := tile.Tile{X: leg.Tiles[0].X, Z: leg.Tiles[0].Z}
				for _, p := range leg.Tiles {
					next := tile.Tile{X: p.X, Z: p.Z}
					for _, at := range line(prev, next) {
						steps++
						for _, l := range ix.At(at.X, at.Z, 0) {
							info, ok := cfg.Loc(l.ID)
							if !ok || !info.BlockWalk {
								continue
							}
							if cachecfg.OpIndex(info.Ops, "Open") != 0 || cachecfg.OpIndex(info.Ops, "Close") != 0 {
								doors[fmt.Sprintf("%s %d at %d,%d", info.Name, l.ID, l.X, l.Z)] = true
							}
						}
					}
					prev = next
				}
			}
			for d := range doors {
				t.Errorf("%s, %s: the walk passes a %s", c.Name, w.what, d)
			}
			t.Logf("%-24s %-14s cost %3d, %3d tiles, %d hop(s), %d door/gate loc(s) on the route", c.Name, w.what, r.Cost, steps, len(r.Hops()), len(doors))
		}
	}
}
