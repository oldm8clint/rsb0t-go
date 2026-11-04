package bot

import (
	"strings"
	"testing"
)

// The pick line is content's own, matched however the server cases or pads it.
func TestPickDropMatchesContentsFlaxLine(t *testing.T) {
	p, ok := pickDrop("You pick some flax. ")
	if !ok || p.name != "Flax" {
		t.Fatalf("pickDrop = %+v, %v", p, ok)
	}
	if _, ok := pickDrop("You can't carry any more flax."); ok {
		t.Fatal("a full-pack line is not a pick")
	}
}

// The ids are content 289's: the pick adds a Flax and the picture is the Flax plant.
func TestPickablesAreContentsIdsRealConfig(t *testing.T) {
	cfg := bowCfg(t)
	for line, p := range pickLines {
		if o, ok := cfg.Obj(p.obj); !ok || !strings.EqualFold(o.Name, p.name) {
			t.Errorf("%q: obj %d is %+v, want %s", line, p.obj, o.Name, p.name)
		}
		if l, ok := cfg.Loc(p.loc); !ok || !strings.EqualFold(l.Name, p.name) {
			t.Errorf("%q: loc %d is %q, want %s", line, p.loc, l.Name, p.name)
		}
	}
}

func TestWatchPickingWithNoSessionIsANoOp(t *testing.T) {
	(&Context{}).watchPicking()
}
