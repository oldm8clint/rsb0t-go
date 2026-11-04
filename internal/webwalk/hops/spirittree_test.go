package hops

import (
	"strings"
	"testing"

	"rsb0t-go/internal/webwalk"
)

// locWorld is a walkWorld whose FindLocs answers a name query the way the live client does
// (ignoring case) and whose OpLoc records what it was asked to operate.
type locWorld struct {
	*walkWorld
	locs    []webwalk.LocMatch
	queries []webwalk.LocQuery
	ops     []webwalk.LocMatch
}

func (f *locWorld) FindLocs(q webwalk.LocQuery) []webwalk.LocMatch {
	f.queries = append(f.queries, q)
	var out []webwalk.LocMatch
	for _, m := range f.locs {
		for _, n := range q.Names {
			if strings.EqualFold(n, m.Name) {
				out = append(out, m)
			}
		}
	}
	return out
}

func (f *locWorld) OpLoc(op, x, z, id int) error {
	f.ops = append(f.ops, webwalk.LocMatch{ID: id, X: x, Z: z, Op: op})
	return nil
}

// The spirit trees are locs named "Spirit tree" in this content, while the walk graph names them
// "Spirit Tree" on hops with no loc id, which run as NPC hops. Live on 1306c66a not one crossing
// succeeded ("'Spirit Tree' not interactable"): the NPC hop's loc fallback must find and operate
// the tree.
func TestAnNPCHopFallsBackToALocOfThatName(t *testing.T) {
	stand := webwalk.Point{X: 3179, Z: 3507}
	w := &locWorld{walkWorld: newWalkWorld(webwalk.Point{X: 3181, Z: 3509}),
		locs: []webwalk.LocMatch{{ID: 1317, X: 3178, Z: 3506, Name: "Spirit tree", Op: 1}}}
	h := &Handler{d: Deps{Client: w}}
	m, ok := h.opLocByName("Spirit Tree", "Talk-to", stand)
	if !ok || m.ID != 1317 {
		t.Fatalf("no loc fallback: %+v %v", m, ok)
	}
	q := w.queries[len(w.queries)-1]
	if q.Op != "Talk-to" || q.Radius != 5 || q.At != stand || len(q.Names) != 1 {
		t.Fatalf("query %+v", q)
	}
	if len(w.ops) != 1 || w.ops[0].ID != 1317 || w.ops[0].Op != 1 || w.ops[0].X != 3178 {
		t.Fatalf("operated %+v", w.ops)
	}
	// Nothing of that name: no op, the hop still reports "not interactable".
	empty := &locWorld{walkWorld: newWalkWorld(stand)}
	if _, ok := (&Handler{d: Deps{Client: empty}}).opLocByName("Spirit Tree", "Talk-to", stand); ok || len(empty.ops) != 0 {
		t.Fatal("operated a loc that is not there")
	}
}
