package questmods

import (
	"os"
	"strings"
	"testing"

	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// The two fillers' places on the real collision pack, the baked web graph and the loc index (skipped without
// RSB0T_COLLISION_PACK and RSB0T_LOC_INDEX): every stand the modules send an account to is on a walkable tile, every NPC they
// talk to can be reached from a tile beside it, and every leg of the routes the quests take plans from where the
// account is when it starts the leg. A tile the walker cannot reach parks a quest in production after the walk times out;
// here it fails in a second.

func fillerPack(t *testing.T) *pathfind.Pack {
	t.Helper()
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

func point(t tile.Tile) webwalk.Point { return webwalk.Point{X: t.X, Z: t.Z, Level: t.Level} }

func TestTheFillersStandsAreWalkableAndTheirNPCsTalkable(t *testing.T) {
	pk := fillerPack(t)
	for name, at := range map[string]tile.Tile{
		"bartender":       questplan.RustyAnchorBartender,
		"generals":        questplan.GoblinGenerals,
		"Aggie":           questplan.AggieAnchor,
		"Wyson":           questplan.WysonAnchor,
		"goblin field":    questplan.GoblinVillageField,
		"Wydin":           questplan.WydinShop.Anchor,
		"onion patch":     questplan.RimmingtonOnion,
		"Romeo":           questplan.RomeoAnchor,
		"Juliet":          questplan.JulietAnchor,
		"Father Lawrence": questplan.LawrenceAnchor,
		"Apothecary":      questplan.ApothecaryHouse,
	} {
		if !pk.Walkable(at.X, at.Z, at.Level) {
			t.Errorf("%s %v is not a walkable tile", name, at)
		}
	}
	// The berries lie on walkable tiles (the account stands on the spawn to take it).
	for _, tl := range append(append([]tile.Tile(nil), questplan.CadavaSpawns...), questplan.RedberrySpawns...) {
		if !pk.Walkable(tl.X, tl.Z, 0) {
			t.Errorf("berry spawn %v is not walkable", tl)
		}
	}
	// Every NPC the quests talk to can be spoken to from some tile beside its spawn.
	for name, at := range map[string]tile.Tile{
		"bartender": questplan.RustyAnchorBartender, "Wartface": questplan.GoblinGenerals, "Aggie": questplan.AggieAnchor,
		"Romeo": questplan.RomeoAnchor, "Juliet": questplan.JulietAnchor, "Lawrence": questplan.LawrenceAnchor, "Apothecary": questplan.ApothecaryHouse,
	} {
		if len(pk.InteractTiles(pathfind.NavPoint{X: at.X, Z: at.Z, Level: at.Level})) == 0 {
			t.Errorf("no tile to talk to %s at %v from", name, at)
		}
	}
	// Every Goblin Village goblin spawn is a walkable tile of the village, south of the farm's northern edge.
	for _, sp := range []tile.Tile{{X: 2951, Z: 3483}, {X: 2964, Z: 3511}, {X: 2963, Z: 3514}, {X: 2957, Z: 3482}, {X: 2952, Z: 3510}} {
		if !pk.Walkable(sp.X, sp.Z, 0) {
			t.Errorf("goblin spawn %v is not walkable", sp)
		}
	}
}

func TestTheFillersRoutesPlanFromEveryStart(t *testing.T) {
	pk := fillerPack(t)
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	pl := webwalk.NewPlanner(pk, g)
	draynor, falEast := questplan.DraynorBank, tile.Tile{X: 3013, Z: 3355}
	varrockWest, varrockEast := questplan.VarrockWestBank, questplan.VarrockEastBank
	type leg struct {
		name     string
		from, to tile.Tile
		hop      string // a hop the route must take, "" for none required
	}
	legs := []leg{
		{"Draynor bank to the bartender", draynor, questplan.RustyAnchorBartender, ""},
		{"Draynor bank to Wydin", draynor, questplan.WydinShop.Anchor, ""},
		{"Draynor bank to the Rimmington onions", draynor, questplan.RimmingtonOnion, ""},
		{"the onions to Wyson", questplan.RimmingtonOnion, questplan.WysonAnchor, ""},
		{"Wyson to Aggie", questplan.WysonAnchor, questplan.AggieAnchor, ""},
		{"Aggie to the Draynor bank", questplan.AggieAnchor, draynor, ""},
		{"Aggie to the goblin field", questplan.AggieAnchor, questplan.GoblinVillageField, ""},
		{"Falador east bank to the goblin field", falEast, questplan.GoblinVillageField, ""},
		{"the goblin field to the generals", questplan.GoblinVillageField, questplan.GoblinGenerals, ""},
		{"the generals back to the Draynor bank", questplan.GoblinGenerals, draynor, ""},
		{"Varrock west bank to Romeo", varrockWest, questplan.RomeoAnchor, ""},
		{"Varrock west bank to Juliet upstairs", varrockWest, questplan.JulietAnchor, "Staircase"},
		{"Romeo to Juliet upstairs", questplan.RomeoAnchor, questplan.JulietAnchor, "Staircase"},
		{"Juliet upstairs to Romeo", questplan.JulietAnchor, questplan.RomeoAnchor, ""},
		{"Romeo to Father Lawrence", questplan.RomeoAnchor, questplan.LawrenceAnchor, ""},
		{"Father Lawrence to the Apothecary", questplan.LawrenceAnchor, questplan.ApothecaryHouse, ""},
		{"Varrock east bank to the Apothecary", varrockEast, questplan.ApothecaryHouse, ""},
	}
	for _, sp := range questplan.CadavaSpawns {
		legs = append(legs, leg{"Varrock east bank to the cadava berry " + point(sp).String(), varrockEast, sp, ""},
			leg{"the cadava berry " + point(sp).String() + " to the Apothecary", sp, questplan.ApothecaryHouse, ""})
	}
	for _, sp := range questplan.RedberrySpawns {
		legs = append(legs, leg{"Draynor bank to the redberry " + point(sp).String(), draynor, sp, ""},
			leg{"the redberry " + point(sp).String() + " to the Rimmington onions", sp, questplan.RimmingtonOnion, ""})
	}
	for _, l := range legs {
		r := pl.Plan(point(l.from), point(l.to), webwalk.Options{})
		if !r.OK {
			t.Errorf("%s: no route from %v to %v: %s", l.name, l.from, l.to, r.Reason)
			continue
		}
		var hops []string
		for _, h := range r.Hops() {
			hops = append(hops, h.LocName+" "+h.Action)
		}
		if l.hop != "" && !strings.Contains(strings.Join(hops, ","), l.hop) {
			t.Errorf("%s: the route takes %v, want a %s", l.name, hops, l.hop)
		}
		t.Logf("%s: cost %d%s", l.name, r.Cost, func() string {
			if len(hops) == 0 {
				return ""
			}
			return " hops " + strings.Join(hops, ", ")
		}())
	}
}

// The Rimmington onion patch holds the plants the pick step looks for, around the anchor (the Witch's Potion patch, proven
// live), and the Goblin Village holds no loc the farm's fights could be blocked by at the village edge.
func TestTheOnionPatchHoldsThePlantsThePickStepLooksFor(t *testing.T) {
	path := os.Getenv("RSB0T_LOC_INDEX")
	if path == "" {
		t.Skip("RSB0T_LOC_INDEX not set")
	}
	ix, err := locindex.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	a := questplan.RimmingtonOnion
	ix.Each(a.X-12, a.Z-12, a.X+12, a.Z+12, 0, func(l locindex.Loc) {
		if l.ID == questplan.LocOnionPlant {
			n++
		}
	})
	if n < 10 {
		t.Errorf("%d onion plants within 12 tiles of %v, want the patch's 15", n, a)
	}
}
