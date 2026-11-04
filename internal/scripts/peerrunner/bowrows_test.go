package peerrunner

import (
	"os"
	"reflect"
	"sort"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/peerbank"
)

// realConfig is the real 289 object config (RSB0T_CONFIG_JAG, e.g. fleet-data/prod-config.jag);
// these tests name real items and skip without it.
func realConfig(t *testing.T) *cachecfg.Config {
	t.Helper()
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return cfg
}

func slot(id int32, n int32) gamestate.SlotItem {
	return gamestate.SlotItem{Item: gamestate.Item{ID: id, Count: n}}
}

func rowsByItem(hs []peerbank.Holding) map[string]int {
	out := map[string]int{}
	for _, h := range hs {
		out[h.Item] += h.Count
	}
	return out
}

// THE BUG (live, 6 Oct 2026): the mule's bank holds 130,712 STRUNG Yew shortbows (obj 857) and its
// Yew longbows as 7,922 unstrung (obj 66) and 5,519 strung (obj 855). Content 289 gives a strung bow
// and its unstrung form one name, so the published bank merged them: a reader asking "how many Yew
// shortbows are unstrung" got 130,712. Every bow is now published once per obj as well, so a reader
// can ask for the unstrung one by name AND id.
func TestHoldingsPublishesEveryBowOnceAMergedRowAndOncePerObj(t *testing.T) {
	cfg := realConfig(t)
	bank := []gamestate.SlotItem{
		slot(857, 130712),            // Yew shortbow, strung
		slot(66, 7922),               // Yew longbow, unstrung
		slot(855, 5519),              // Yew longbow, strung
		slot(856, 3),                 // a bank note of the strung Yew longbow
		slot(1777, 40000),            // Bow string: one obj, not keyed
		slot(995, 12),                // coins
		slot(1389, 2),                // Magic staff: one obj
		slot(1641, 5), slot(1651, 1), // Ruby ring: the ring and a one-coin obj of the same name
	}
	got := rowsByItem(Holdings(cfg, bank))
	want := map[string]int{
		// the merged rows, as every reader before the per-obj rows saw them
		"Yew shortbow": 130712, "Yew longbow": 7922 + 5519 + 3, "Bow string": 40000, "Coins": 12, "Magic staff": 2, "Ruby ring": 6,
		// and one row per obj of a keyed name, a note counted under the obj it is a note of
		"Yew shortbow id857": 130712, "Yew longbow id66": 7922, "Yew longbow id855": 5519 + 3,
		"Ruby ring id1641": 5, "Ruby ring id1651": 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("published rows:\n got %v\nwant %v", got, want)
	}
	// The unstrung Yew shortbow is NOT held: it has no row, and so none of the 130,712 count as one.
	if n, ok := got["Yew shortbow id68"]; ok || n != 0 {
		t.Fatalf("an unstrung Yew shortbow row appeared: %d", n)
	}
}

// Every bow name content strings is keyed, and the qualified row of each resolves back to its obj
// and that obj's note through cachecfg.ObjIDs, which is what the mule's hand-out reads.
func TestEveryBowNameIsKeyedAndItsRowsResolveToOneObj(t *testing.T) {
	cfg := realConfig(t)
	for _, name := range []string{"Magic longbow", "Magic shortbow", "Yew longbow", "Yew shortbow", "Maple longbow", "Maple shortbow",
		"Willow longbow", "Willow shortbow", "Oak longbow", "Oak shortbow", "Longbow", "Shortbow"} {
		if !IsIDKeyed(name) {
			t.Errorf("%s is not published per obj: a count by name cannot tell its strung form from its unstrung one", name)
			continue
		}
		var plain []int
		for _, id := range cfg.ObjIDs(name) {
			if o, ok := cfg.Obj(id); ok && !o.Noted() {
				plain = append(plain, id)
			}
		}
		if len(plain) != 2 {
			t.Errorf("%s: %d un-noted objs, want the strung and the unstrung bow", name, len(plain))
			continue
		}
		for _, id := range plain {
			ids := cfg.ObjIDs(cachecfg.QualifiedName(name, id))
			sort.Ints(ids)
			if len(ids) != 2 || ids[0] != id {
				t.Errorf("%s id%d resolves to %v, want the obj and its note", name, id, ids)
			}
		}
	}
}

func TestPlainRowsAndObjRowsDropEachKindOfExtraRow(t *testing.T) {
	hs := []peerbank.Holding{
		{Item: "Yew shortbow", Count: 130712 + 4}, {Item: "Yew shortbow id857", Count: 130712}, {Item: "Yew shortbow id68", Count: 4},
		{Item: "Bow string", Count: 9},
		{Item: "Herb", Count: 3}, {Item: "Herb id199", Count: 3},
		{Item: "Magic longbow", Count: 2}, // keyed, but published with no per-obj row (an older reading)
	}
	plain := PlainRows(hs)
	if got := rowsByItem(plain); !reflect.DeepEqual(got, map[string]int{"Yew shortbow": 130716, "Bow string": 9, "Herb": 3, "Magic longbow": 2}) {
		t.Errorf("PlainRows = %v", got)
	}
	obj := ObjRows(hs)
	if got := rowsByItem(obj); !reflect.DeepEqual(got, map[string]int{"Yew shortbow id857": 130712, "Yew shortbow id68": 4, "Bow string": 9, "Herb id199": 3, "Magic longbow": 2}) {
		t.Errorf("ObjRows = %v (a keyed name keeps its merged row only when no per-obj row exists)", got)
	}
	total := func(rows []peerbank.Holding) int {
		n := 0
		for _, h := range rows {
			n += h.Count
		}
		return n
	}
	if total(plain) != total(obj) {
		t.Errorf("the two readings of one bank disagree: %d against %d (a reader that sums must not count a bow twice)", total(plain), total(obj))
	}
}
