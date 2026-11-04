package webwalk

import "testing"

func TestEdgeCorrectionsAreApplied(t *testing.T) {
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := ParseGraph(defaultGraphJSON)
	if err != nil {
		t.Fatal(err)
	}
	hits := fresh.ApplyCorrections(EdgeCorrections)
	for i, n := range hits {
		if n == 0 {
			t.Errorf("correction %d (loc %d at %d,%d) matched no edge", i, EdgeCorrections[i].LocID, EdgeCorrections[i].LocX, EdgeCorrections[i].LocZ)
		}
	}
	for _, e := range g.Edges {
		switch {
		case e.LocID == 4469 || e.LocID == 4470:
			t.Errorf("castle wars barrier kept: %s", e.String())
		case e.LocID == 1530 && e.LocX == 2929 && e.LocZ == 3536:
			t.Errorf("the missing door kept: %s", e.String())
		case e.LocID == 881 && e.OpenLocID != 882:
			t.Errorf("closed manhole without its open id: %s", e.String())
		case e.LocX == 2529 && e.LocZ == 3303 && (e.LocID != 2543 || e.OpenLocID != 2544):
			t.Errorf("West Ardougne manhole: %s", e.String())
		case e.LocX == 2378 && e.LocZ == 3134 && e.From.Level == 1 && e.LocID != 4911:
			t.Errorf("castle wars ladder: %s", e.String())
		}
	}
}

func TestWalkerOpensAShutTrapdoorFirst(t *testing.T) {
	col := newGrid().fill(0, 0, 4, 4, 0).fill(0, 0, 4, 4, 1)
	f := newFakeWorld(t, col, Point{X: 0, Z: 0})
	// A shut manhole at (3,3): the Door model gives it Open while shut; once opened, the hop's
	// own action appears on its open id.
	door := &fakeDoor{at: Point{X: 3, Z: 3}, beyond: Point{X: 3, Z: 3}, id: 881, openID: 882, name: "Manhole"}
	f.doors = append(f.doors, door)
	f.ladders = append(f.ladders, &fakeLadder{loc: Point{X: 3, Z: 3}, stand: Point{X: 2, Z: 3}, landing: Point{X: 2, Z: 3, Level: 1}, id: 882, name: "Manhole", op: "Climb down"})
	g := &Graph{Edges: []Edge{{From: Point{X: 2, Z: 3}, To: Point{X: 2, Z: 3, Level: 1}, Kind: KindDungeon, Cost: 8, LocName: "Manhole", Action: "Climb down", LocID: 881, OpenLocID: 882, LocX: 3, LocZ: 3}}}
	w := NewWalker(NewPlanner(col, g), &shutWorld{fakeWorld: f, door: door})
	w.Log = func(s string) { t.Log(s) }
	if res := w.WalkTo(Point{X: 0, Z: 0, Level: 1}, WalkOptions{}); !res.Arrived() {
		t.Fatalf("result = %+v (ops %v)", res, f.ops)
	}
	if len(f.ops) < 2 || f.ops[0] != "881" || f.ops[1] != "882" {
		t.Errorf("ops = %v, want the shut manhole opened then climbed", f.ops)
	}
}

// shutWorld hides the open manhole's climb option until the shut one has been opened.
type shutWorld struct {
	*fakeWorld
	door *fakeDoor
}

func (s *shutWorld) FindLocs(q LocQuery) []LocMatch {
	var out []LocMatch
	for _, m := range s.fakeWorld.FindLocs(q) {
		if m.ID == 882 && !s.door.open {
			continue
		}
		if q.Op == "Close" || (m.ID == 882 && q.Op == "Open") {
			continue
		}
		out = append(out, m)
	}
	return out
}

func (s *shutWorld) OpLoc(op, x, z, id int) error {
	if id == 881 {
		s.fakeWorld.ops = append(s.fakeWorld.ops, "881")
		if !s.door.open {
			s.door.open = true
		}
		return nil
	}
	return s.fakeWorld.OpLoc(op, x, z, id)
}
