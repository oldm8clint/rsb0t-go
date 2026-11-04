package runecraftrunner

import (
	"os"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

type realData struct {
	cfg *cachecfg.Config
	ix  *locindex.Index
	pk  *pathfind.Pack
}

// loadReal loads the fleet's loc index, cache config and collision pack (RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG,
// RSB0T_COLLISION_PACK); without them the test skips.
func loadReal(t *testing.T) realData {
	t.Helper()
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
	return realData{cfg: cfg, ix: ix, pk: pk}
}

func (r realData) one(t *testing.T, name, op string, at tile.Tile) locindex.Match {
	t.Helper()
	ms := r.ix.Find(r.cfg, locindex.Query{X: at.X, Z: at.Z, Radius: runecraftplan.RuinsRadius, Name: name, Op: op}, nil)
	if len(ms) != 1 {
		t.Fatalf("%q near %v: %d matches", name, at, len(ms))
	}
	return ms[0]
}

// Inside each temple the shortest walk is a few steps each way, and the plan names tiles the engine accepts.
func TestTemplePlansOnTheRealData(t *testing.T) {
	r := loadReal(t)
	for _, a := range runecraftplan.Altars() {
		altar := r.one(t, runecraftplan.AltarName, runecraftplan.AltarOp, a.AltarAt)
		portal := r.one(t, runecraftplan.PortalName, runecraftplan.PortalOp, a.PortalAt)
		plan, ok := PlanTemple(r.pk, r.cfg, a, altar, portal)
		if !ok {
			t.Errorf("%s: no plan", a.Name)
			continue
		}
		t.Logf("%s: arrival %v -> altar stand %v (%d steps) -> portal stand %v (%d steps)", a.Name, a.Enter, plan.AltarStand, plan.ToAltar, plan.PortalStand, plan.ToPortal)
		if plan.ToAltar > 8 || plan.ToPortal > 8 || plan.Steps > 14 { // Air 3+3, Nature 4+4, Body 7+5
			t.Errorf("%s: %+v is longer than a temple should need", a.Name, plan)
		}
		if got, _ := runecraftplan.WhereIs(plan.AltarStand); got != runecraftplan.RegionTemple {
			t.Errorf("%s: the altar stand %v is outside", a.Name, plan.AltarStand)
		}
	}
}

// The legs a round walks plan on the walker's own graph and collision pack: bank to ruins, and (the owner's
// best mining place) the Wizards' Guild's Wizard Distentor from the Yanille bank and back through the
// guild's doors -- which the miner's plain walk cannot open.
func TestTheRoundsLegsPlanOnTheRealPack(t *testing.T) {
	r := loadReal(t)
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	planner := webwalk.NewPlanner(r.pk, g)
	plan := func(from, to tile.Tile) webwalk.Route {
		return planner.Plan(webwalk.Point{X: from.X, Z: from.Z, Level: from.Level}, webwalk.Point{X: to.X, Z: to.Z, Level: to.Level}, webwalk.Options{})
	}
	guild := essenceplan.Gateways()[1]
	for _, c := range []struct {
		name     string
		from, to tile.Tile
		max      int
	}{
		{"Falador east bank to the Air ruins", runecraftplan.Air.Bank, runecraftplan.Air.Exit, 100},
		{"the Air exit to the Falador east bank", runecraftplan.Air.Exit, runecraftplan.Air.Bank, 100},
		{"Edgeville bank to the Body ruins", runecraftplan.Body.Bank, runecraftplan.Body.Exit, 140},
		{"the Body exit to the Edgeville bank", runecraftplan.Body.Exit, runecraftplan.Body.Bank, 140},
		{"the Yanille bank to Wizard Distentor", guild.Bank, guild.Stand, 60},
		{"Wizard Distentor to the Yanille bank (the mine's exit is beside him)", tile.Tile{X: 2591, Z: 3086}, guild.Bank, 60},
	} {
		route := plan(c.from, c.to)
		if !route.OK || route.Cost > c.max {
			t.Errorf("%s: ok %v cost %d (%s), want at most %d", c.name, route.OK, route.Cost, route.Reason, c.max)
			continue
		}
		t.Logf("%s: cost %d", c.name, route.Cost)
	}
	// The walk from the mule's post to the ruins exists (ships, the spirit trees) for the account with no
	// teleport runes; it is the long way the teleport travel saves.
	post := tile.Tile{X: 2809, Z: 3441}
	for _, a := range []runecraftplan.Altar{runecraftplan.Air, runecraftplan.Body} {
		route := plan(post, a.Exit)
		t.Logf("the mule's post to the %s exit on foot: ok %v cost %d %s", a.Name, route.OK, route.Cost, route.Reason)
	}
}
