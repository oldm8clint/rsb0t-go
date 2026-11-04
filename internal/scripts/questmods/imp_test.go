package questmods

import (
	"os"
	"testing"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/live"
)

func impNpc(index, x, z int) entityquery.Entity {
	return entityquery.Entity{Kind: entityquery.KindNpc, Index: index, ID: questplan.NpcImp, Name: "Imp", X: x, Z: z, FaceEntity: -1}
}

// A busy imp is one another player is fighting: it faces that player and has just been hit. An imp that merely still
// faces the last player to hit it is free, and so is every imp when the account's own index is unknown.
func TestImpCandidatesReadBusyFromTheFaceAndTheHitmark(t *testing.T) {
	const self = 7
	facing := func(e entityquery.Entity, player int, inCombat bool) entityquery.Entity {
		e.FaceEntity, e.InCombat = 32768+player, inCombat
		return e
	}
	npcs := []entityquery.Entity{
		impNpc(1, 10, 10),                     // alone
		facing(impNpc(2, 11, 10), 3, true),    // another player's fight
		facing(impNpc(3, 12, 10), 3, false),   // still facing a player who has gone
		facing(impNpc(4, 13, 10), self, true), // this account's own
		facing(impNpc(5, 14, 10), 3, true),    // another player's, again
	}
	steps := func(t tile.Tile) int { return t.X - 10 }
	got := impCandidatesFrom(npcs, self, steps)
	wantBusy := []bool{false, true, false, false, true}
	for i, c := range got {
		if c.Busy != wantBusy[i] || c.Steps != i || c.At.X != 10+i {
			t.Errorf("imp %d: busy %v steps %d at %v, want busy %v steps %d", npcs[i].Index, c.Busy, c.Steps, c.At, wantBusy[i], i)
		}
	}
	for i, c := range impCandidatesFrom(npcs, -1, steps) {
		if c.Busy {
			t.Errorf("imp %d is busy though the account's own index is unknown", npcs[i].Index)
		}
	}
}

// pickBead takes the nearest bead of a kind the pack lacks, within the walk, off the tiles that did not work.
func TestPickBeadTakesOnlyWhatIsNeededAndNear(t *testing.T) {
	at := func(id, x int) gamestate.GroundItem { return gamestate.GroundItem{ID: id, Count: 1, X: x, Z: 0} }
	items := []gamestate.GroundItem{
		at(questplan.ObjRedBead, 5),     // held already
		at(questplan.ObjYellowBead, 30), // beyond the walk
		at(questplan.ObjBlackBead, 12),
		at(questplan.ObjWhiteBead, 8),
		at(592, 1), // ashes
	}
	held := func(id int) int {
		if id == questplan.ObjRedBead {
			return 1
		}
		return 0
	}
	steps := func(t tile.Tile) int { return t.X }
	g, ok := pickBead(items, held, steps, 24, nil)
	if !ok || g.ID != questplan.ObjWhiteBead {
		t.Fatalf("took %+v (%v), want the white bead 8 steps away", g, ok)
	}
	g, ok = pickBead(items, held, steps, 24, map[tile.Tile]bool{{X: 8}: true})
	if !ok || g.ID != questplan.ObjBlackBead {
		t.Fatalf("with the white bead's tile skipped took %+v (%v), want the black bead", g, ok)
	}
	if _, ok := pickBead(items, held, func(t tile.Tile) int { return -1 }, 24, nil); ok {
		t.Error("took a bead no walk reaches")
	}
	if _, ok := pickBead(items, func(int) int { return 1 }, steps, 24, nil); ok {
		t.Error("took a bead with all four held")
	}
	if _, ok := pickBead(nil, held, steps, 24, nil); ok {
		t.Error("took a bead from an empty ground")
	}
}

func TestIsBead(t *testing.T) {
	for _, b := range questplan.ImpBeads {
		if !isBead(b) {
			t.Errorf("%d is a bead", b)
		}
	}
	if isBead(592) || isBead(0) {
		t.Error("ashes or nothing is a bead")
	}
}

var impReal struct {
	pk *pathfind.Pack
}

func impPackOrSkip(t *testing.T) *pathfind.Pack {
	t.Helper()
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	if impReal.pk == nil {
		pk, err := pathfind.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		impReal.pk = pk
	}
	return impReal.pk
}

// The farm's pick on the real ground. Live 16 Oct 2026 the old pick took the nearest imp by distance, and an imp whose
// teleport had put it in a monastery room or the Fight Arena's yard, a few tiles over a wall, was nearer than any in the
// open: the walker opened the monastery's doors (twenty crossings an hour for one account) or the fight loop stood in front
// of the wall for its three minutes.
func TestTheFarmPicksAnImpOnOpenGroundNotOneBehindAWall(t *testing.T) {
	pk := impPackOrSkip(t)
	cases := []struct {
		site   questplan.ImpSite
		stand  tile.Tile // where the account stands
		pocket tile.Tile // an imp in a pocket, near
		open   tile.Tile // an imp on open ground, farther
	}{
		{questplan.ImpSiteArdougne, tile.Tile{X: 2603, Z: 3213}, tile.Tile{X: 2606, Z: 3216}, tile.Tile{X: 2612, Z: 3198}},
		{questplan.ImpSiteYanille, tile.Tile{X: 2606, Z: 3137}, tile.Tile{X: 2606, Z: 3141}, tile.Tile{X: 2616, Z: 3126}},
	}
	for _, c := range cases {
		reach := pk.ReachFrom(pathfind.NavPoint{X: c.stand.X, Z: c.stand.Z}, impReachWindow, nil)
		steps := func(t tile.Tile) int { return stepsFrom(c.stand, reach, t) }
		npcs := []entityquery.Entity{impNpc(1, c.pocket.X, c.pocket.Z), impNpc(2, c.open.X, c.open.Z)}
		if tile.Chebyshev(c.stand, c.pocket) >= tile.Chebyshev(c.stand, c.open) {
			t.Fatalf("%s: the pocket imp is not the nearer by distance: the case proves nothing", c.site.ID)
		}
		cands := impCandidatesFrom(npcs, 0, steps)
		if cands[0].Steps != -1 {
			t.Errorf("%s: the imp at %v is %d steps over open ground, want none", c.site.ID, c.pocket, cands[0].Steps)
		}
		if cands[1].Steps <= 0 {
			t.Errorf("%s: the imp at %v on open ground has %d steps", c.site.ID, c.open, cands[1].Steps)
		}
		if i, ok := questplan.PickImp(c.site, cands); !ok || i != 1 {
			t.Errorf("%s: picked %d (%v), want the imp on open ground", c.site.ID, i, ok)
		}
		// Only the pocket's imp in view: nothing is picked and the account looks along the spawns.
		if _, ok := questplan.PickImp(c.site, cands[:1]); ok {
			t.Errorf("%s: picked an imp that only a door reaches", c.site.ID)
		}
	}
}

// Without a collision pack (a harness) the pick falls back to distance, as before.
func TestStepsFromFallsBackToDistanceWithoutAFlood(t *testing.T) {
	here := tile.Tile{X: 100, Z: 100}
	if n := stepsFrom(here, nil, tile.Tile{X: 104, Z: 97}); n != 4 {
		t.Errorf("distance fallback = %d, want 4", n)
	}
	if n := stepsFrom(here, nil, tile.Tile{X: 104, Z: 97, Level: 1}); n != -1 {
		t.Errorf("another level = %d, want -1", n)
	}
}

// An account sent to either site can get there from where the ladder's accounts arrive: the teleports' landings, the banks
// and the Wizards' Tower, at the lowest combat level and at the usual one, with the walker's danger zones on. Yanille's
// strip costs about a minute's walk more than Ardougne's from Kandarin and Misthalin (the price of the spread).
func TestBothImpSitesAreWalkableFromWhereTheLadderArrives(t *testing.T) {
	pk := impPackOrSkip(t)
	pl, err := live.PlannerFor(pk)
	if err != nil {
		t.Fatal(err)
	}
	starts := map[string]webwalk.Point{
		"Camelot Teleport landing": {X: 2757, Z: 3478}, "Seers' Village bank": {X: 2725, Z: 3491}, "Catherby bank": {X: 2809, Z: 3440},
		"Varrock Teleport landing": {X: 3213, Z: 3424}, "Falador Teleport landing": {X: 2964, Z: 3377}, "Wizards' Tower": {X: 3105, Z: 3160},
		"Draynor bank": {X: 3093, Z: 3243}, "East Ardougne south bank": {X: 2655, Z: 3286}, "Yanille bank": {X: 2613, Z: 3094},
	}
	for _, combat := range []int{5, 61} {
		for name, from := range starts {
			cost := map[string]int{}
			for _, site := range questplan.ImpSites {
				to := webwalk.Point{X: site.Stand.X, Z: site.Stand.Z}
				zones := webwalk.ResolveDangerZones(nil, nil, webwalk.ZoneContext{IncludeAutomatic: true, CombatLevel: combat, CombatKnown: true, Start: &from, Dest: &to})
				rt := pl.Plan(from, to, webwalk.Options{AvoidZones: zones, AllowNPC: true, AllowSpecial: true})
				if !rt.OK || rt.Cost > 600 {
					t.Errorf("combat %d: %s -> %s: route ok=%v cost %d (%s)", combat, name, site.ID, rt.OK, rt.Cost, rt.Reason)
				}
				cost[site.ID] = rt.Cost
			}
			if from.X != 2613 && cost[questplan.ImpSiteYanille.ID] > cost[questplan.ImpSiteArdougne.ID]+120 {
				t.Errorf("combat %d: from %s Yanille costs %d against Ardougne's %d: more than a two minute detour", combat, name, cost[questplan.ImpSiteYanille.ID], cost[questplan.ImpSiteArdougne.ID])
			}
		}
	}
}
