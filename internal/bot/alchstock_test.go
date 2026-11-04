package bot

import (
	"strings"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/peerrunner"
)

// THE STOCK AGAINST THE REAL ITEM CONFIG. Every number in scarabplan's alch table is 60% of the cost the
// cache gives the obj an ask names, and every name on the list that more than one obj carries is
// published by the mule per obj, or an ask for it could not name the one worth casting on.
func TestAlchStockMatchesTheItemConfigAndEverySharedNameIsPublishedPerObj(t *testing.T) {
	cfg := bowCfg(t)
	ask := alchAskName(cfg)
	shared := 0
	for _, name := range fleetplan.DefaultAlchItems {
		id, isShared, ok := cfg.Dearest(name)
		if !ok {
			t.Errorf("%q is not an item in the cache", name)
			continue
		}
		o, _ := cfg.Obj(id)
		if want, got := fleetplan.HighAlchValue(int(o.Cost)), fleetplan.KnownAlchValue(name); want != got {
			t.Errorf("%q: the table says %d coins a cast, the cache's obj %d costs %d: %d", name, got, id, o.Cost, want)
		}
		switch {
		case isShared:
			shared++
			if !peerrunner.IsIDKeyed(name) {
				t.Errorf("%q is carried by more than one un-noted obj and the mule does not publish it per obj: add it to peerrunner.IDKeyedNames", name)
			}
			if got, want := ask(name), cachecfg.QualifiedName(name, id); got != want {
				t.Errorf("%q is asked as %q, want %q", name, got, want)
			}
			if ids := cfg.ObjIDs(ask(name)); len(ids) == 0 || ids[0] != id && !hasInt(ids, id) {
				t.Errorf("%q does not resolve to obj %d: %v", ask(name), id, ids)
			}
		default:
			if got := ask(name); got != name {
				t.Errorf("%q is asked as %q, want the plain name", name, got)
			}
		}
	}
	if shared != 8 { // the six bows (magic, maple and yew, long and short), the Ruby ring and the Black dagger
		t.Errorf("%d names of the stock are shared by two objs, want 8", shared)
	}
	// The bows are asked by the STRUNG obj: the one ResolveIDs calls strung, which is the dearer.
	for _, bow := range []string{"Yew shortbow", "Yew longbow", "Maple shortbow", "Maple longbow"} {
		un, st, ok := bowIDsOf(&Context{Config: cfg})(bow)
		if !ok || ask(bow) != cachecfg.QualifiedName(bow, st) || strings.Contains(ask(bow), cachecfg.QualifiedName(bow, un)) {
			t.Errorf("%s is asked as %q, want the strung obj %d (unstrung %d)", bow, ask(bow), st, un)
		}
	}
	// Every bow name the fleet strings is published per obj, whether or not it is alch stock.
	for _, bow := range bowstringplan.DefaultBowNames() {
		if !peerrunner.IsIDKeyed(bow) {
			t.Errorf("%q is not published per obj", bow)
		}
	}
	if alchAskName(nil) != nil {
		t.Error("with no config the plain names are asked")
	}
}

func hasInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func askNamed(l fleetplan.AskLine, name string) bool {
	for _, c := range l.Choices {
		if strings.EqualFold(c, name) {
			return true
		}
	}
	return false
}
