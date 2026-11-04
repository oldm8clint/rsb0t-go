package webwalk

import (
	"strings"
	"testing"
)

func TestDefaultGraphLoads(t *testing.T) {
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(g.Source, "PathFinder.addEdges") {
		t.Errorf("source = %q", g.Source)
	}
	kinds := map[Kind]int{}
	for i := range g.Edges {
		e := &g.Edges[i]
		kinds[e.Kind]++
		if e.Cost <= 0 {
			t.Fatalf("edge %s has no cost", e)
		}
		if (e.Kind == KindDoor || e.Kind == KindStair) && !e.HasLoc() {
			t.Errorf("%s edge with no loc: %s", e.Kind, e)
		}
	}
	// Counts from the generator run this file came from; a regenerated graph may move them.
	if len(g.Edges) < 3000 || kinds[KindDoor] < 2000 || kinds[KindStair] < 900 || kinds[KindShip] < 4 || kinds[KindGangplank] < 4 {
		t.Errorf("unexpected graph shape: %d edges, kinds %v", len(g.Edges), kinds)
	}
}

func TestPortSarimShipIsAnNPCHop(t *testing.T) {
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.Kind == KindShip && e.LocName == "Seaman Thresnor" {
			if e.HasLoc() || !e.LandsOnTile || e.To != (Point{X: 2956, Z: 3143, Level: 1}) {
				t.Errorf("ship edge = %s (landsOnTile %v)", e, e.LandsOnTile)
			}
			return
		}
	}
	t.Fatal("no Seaman Thresnor ship edge")
}

func TestApproachOverridesMatchBakedEdges(t *testing.T) {
	g, err := ParseGraph(defaultGraphJSON)
	if err != nil {
		t.Fatal(err)
	}
	hits := g.ApplyOverrides(ApproachOverrides)
	for i, n := range hits {
		if n == 0 {
			t.Errorf("override %d (%+v) matched no baked edge", i, ApproachOverrides[i])
		}
	}
	def, _ := Default()
	for _, o := range ApproachOverrides {
		found := false
		for i := range def.Edges {
			e := &def.Edges[i]
			if e.LocID == o.LocID && e.Action == o.Action && e.LocX == o.LocX && e.LocZ == o.LocZ {
				found = true
				if e.From != o.From {
					t.Errorf("default graph edge %s not moved to %s", e, o.From)
				}
			}
		}
		if !found {
			t.Errorf("override for loc %d not in the default graph", o.LocID)
		}
	}
}

func TestParseGraphRejectsAnEdgeWithoutAKind(t *testing.T) {
	if _, err := ParseGraph([]byte(`{"edges":[{"from":{"x":1,"z":1,"level":0},"to":{"x":1,"z":2,"level":0}}]}`)); err == nil {
		t.Fatal("expected an error")
	}
}

func TestRequiresCheck(t *testing.T) {
	two := 2
	r := &Requires{
		Members:   true,
		Skills:    []SkillReq{{Name: "Agility", Level: 10}},
		Items:     []ItemReq{{Name: "Coins", Count: 30}},
		Worn:      []ItemReq{{Name: "Chef's hat", Count: 1}},
		Quests:    []QuestReq{{Quest: "Dragon Slayer", MinStatus: QuestStarted}},
		FreeSlots: &two,
	}
	if ok, _ := r.Check(nil); !ok {
		t.Error("nil state should fail open")
	}
	st := &WorldState{
		Members:     true,
		Skills:      map[string]int{"agility": 10},
		FreeSlots:   2,
		ItemCount:   func(string) int { return 30 },
		WornCount:   func(string) int { return 1 },
		QuestStatus: func(string) QuestProgress { return QuestStarted },
	}
	if ok, why := r.Check(st); !ok {
		t.Errorf("met requires refused: %s", why)
	}
	cases := []func(s *WorldState){
		func(s *WorldState) { s.Members = false },
		func(s *WorldState) { s.Skills = map[string]int{"agility": 9} },
		func(s *WorldState) { s.FreeSlots = 1 },
		func(s *WorldState) { s.ItemCount = func(string) int { return 29 } },
		func(s *WorldState) { s.WornCount = nil },
		func(s *WorldState) { s.QuestStatus = nil },
	}
	for i, mutate := range cases {
		c := *st
		mutate(&c)
		if ok, _ := r.Check(&c); ok {
			t.Errorf("case %d: unmet requires passed", i)
		}
	}
}
