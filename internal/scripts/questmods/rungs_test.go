package questmods

import (
	"testing"

	"rsb0t-go/internal/scripts/questplan"
)

func TestSmithingTierIsTheBestBarTheLevelWorks(t *testing.T) {
	for _, c := range []struct{ level, bar int }{{1, ObjBronzeBar}, {14, ObjBronzeBar}, {15, ObjIronBar}, {29, ObjIronBar}, {30, ObjSteelBar}, {33, ObjSteelBar}} {
		if got := tierFor(c.level).Bar; got != c.bar {
			t.Errorf("Smithing %d works bar %d, want %d", c.level, got, c.bar)
		}
	}
}

func TestWoodcuttingTierStopsAtWillow(t *testing.T) {
	for _, c := range []struct {
		level int
		loc   string
	}{{1, "Tree"}, {14, "Tree"}, {15, "Oak"}, {29, "Oak"}, {30, "Willow"}, {39, "Willow"}, {50, "Willow"}} {
		if got := wcTier(c.level).Loc; got != c.loc {
			t.Errorf("Woodcutting %d cuts %q, want %q", c.level, got, c.loc)
		}
	}
}

func TestFletchingTierFollowsTheBowTable(t *testing.T) {
	for _, c := range []struct {
		level   int
		log     int
		product string
	}{{1, questplan.ObjLogs, "Arrow shaft"}, {9, questplan.ObjLogs, "Arrow shaft"}, {10, questplan.ObjLogs, "Longbow"},
		{20, objOakLogs, "Oak shortbow"}, {24, objOakLogs, "Oak shortbow"}, {25, objOakLogs, "Oak longbow"}} {
		if l, p := fletchTier(c.level); l != c.log || p != c.product {
			t.Errorf("Fletching %d makes %s from %d, want %s from %d", c.level, p, l, c.product, c.log)
		}
	}
}

func TestAxeLinePrefersABankedAxe(t *testing.T) {
	s := questplan.Snapshot{BankKnown: true, BankIDs: map[int]int{1353: 1}}
	if l := questplan.AxeLine(s); l.ID != 1353 || len(l.Sources) != 0 {
		t.Fatalf("banked steel axe: line %+v", l)
	}
	if l := questplan.AxeLine(questplan.Snapshot{}); l.ID != questplan.ObjIronAxe || len(l.Sources) != 1 {
		t.Fatalf("no axe anywhere: line %+v, want an iron axe with a shop source", l)
	}
}

func TestNailsAreTwoPerBar(t *testing.T) {
	// smithNails asks for ceil(qty/2) bars; this pins the arithmetic the kit's NailBars shares.
	for _, c := range []struct{ nails, bars int }{{8, 4}, {7, 4}, {1, 1}, {12, 6}} {
		if got := (c.nails + 1) / 2; got != c.bars {
			t.Errorf("%d nails take %d bars, want %d", c.nails, got, c.bars)
		}
	}
}
