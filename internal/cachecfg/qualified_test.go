package cachecfg

import (
	"reflect"
	"testing"
)

// A qualified name addresses one of several objs that share a display name (every grimy herb in
// content 289 is "Herb"), and its note, so a published bank and a kit order can name it.
func TestQualifiedNamesResolveOneObjAndItsNote(t *testing.T) {
	c := coinsConfig(t) // objs 1 and 3 are both "Coins"; 2 is the note of 3
	if got := c.ObjIDs("Coins id3"); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Errorf("Coins id3 = %v, want the obj and its note", got)
	}
	if got := c.ObjIDs("coins ID1"); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("coins ID1 = %v, want obj 1 alone", got)
	}
	if got := c.ObjIDs("Flax id1"); got != nil {
		t.Errorf("Flax id1 = %v, want nothing: obj 1 is not Flax", got)
	}
	if got := c.ObjIDs("Coins"); len(got) != 3 {
		t.Errorf("the plain name changed: %v", got)
	}
}

func TestSplitQualified(t *testing.T) {
	for name, want := range map[string]struct {
		base string
		id   int
		ok   bool
	}{
		"Herb id199":   {"Herb", 199, true},
		"Herb":         {"", 0, false},
		"Herb id":      {"", 0, false},
		"Herb id19x":   {"", 0, false},
		"Squid ink":    {"", 0, false},
		"Rune id 5":    {"", 0, false},
		" Herb id207 ": {"Herb", 207, true},
	} {
		base, id, ok := SplitQualified(name)
		if base != want.base || id != want.id || ok != want.ok {
			t.Errorf("%q: %q %d %v, want %+v", name, base, id, ok, want)
		}
	}
	if q := QualifiedName("Herb", 199); q != "Herb id199" {
		t.Errorf("QualifiedName = %q", q)
	}
}

// bowConfig is the shape content 289 gives every bow: the unstrung and the strung bow carry one
// display name, the strung one costing twice as much, each with its own bank note.
//
//	0 Flax; 1 Yew shortbow (unstrung, 400) and its note 2; 3 Yew shortbow (strung, 800) and its note 4.
func bowConfig(t *testing.T) *Config {
	t.Helper()
	objIdx, objDat := table(
		rec(2, "Flax", 0),
		rec(2, "Yew shortbow", 12, 0, 0, 1, 144, 0),
		rec(97, 0, 1, 98, 0, 9, 0),
		rec(2, "Yew shortbow", 12, 0, 0, 3, 32, 0),
		rec(97, 0, 3, 98, 0, 9, 0),
	)
	npcIdx, npcDat := table()
	locIdx, locDat := table()
	c, err := build(map[string][]byte{
		"obj.idx": objIdx, "obj.dat": objDat,
		"npc.idx": npcIdx, "npc.dat": npcDat,
		"loc.idx": locIdx, "loc.dat": locDat,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// A pack count or a trade screen asks "is this obj what was asked for": a plain name is every obj
// with the display name, a qualified one only that obj and its notes.
func TestIsNamedSeparatesTheStrungBowFromTheUnstrungOne(t *testing.T) {
	c := bowConfig(t)
	for id, want := range map[int]bool{0: false, 1: true, 2: true, 3: true, 4: true, 99: false} {
		if got := c.IsNamed("yew SHORTBOW", id); got != want {
			t.Errorf("IsNamed(plain, %d) = %v, want %v", id, got, want)
		}
	}
	for _, tc := range []struct {
		name string
		id   int
		want bool
	}{
		{"Yew shortbow id3", 3, true}, {"Yew shortbow id3", 4, true}, {"Yew shortbow id3", 1, false}, {"Yew shortbow id3", 2, false},
		{"Yew shortbow id1", 1, true}, {"Yew shortbow id1", 2, true}, {"Yew shortbow id1", 3, false}, {"Yew shortbow id1", 4, false},
		{"Flax id3", 3, false}, // obj 3 is not Flax
		{"Yew shortbow id3", 0, false},
	} {
		if got := c.IsNamed(tc.name, tc.id); got != tc.want {
			t.Errorf("IsNamed(%q, %d) = %v, want %v", tc.name, tc.id, got, tc.want)
		}
	}
}

// Dearest is the obj a High Alchemy cast pays most for, and says when a plain name is ambiguous.
func TestDearestPicksTheStrungBowAndSaysTheNameIsShared(t *testing.T) {
	c := bowConfig(t)
	if id, shared, ok := c.Dearest("Yew shortbow"); !ok || !shared || id != 3 {
		t.Errorf("Dearest(Yew shortbow) = %d shared=%v ok=%v, want the strung bow 3, shared", id, shared, ok)
	}
	if id, shared, ok := c.Dearest("Yew shortbow id1"); !ok || shared || id != 1 {
		t.Errorf("Dearest(Yew shortbow id1) = %d shared=%v ok=%v, want the unstrung bow 1 alone", id, shared, ok)
	}
	if id, shared, ok := c.Dearest("Flax"); !ok || shared || id != 0 {
		t.Errorf("Dearest(Flax) = %d shared=%v ok=%v", id, shared, ok)
	}
	if _, _, ok := c.Dearest("Nothing at all"); ok {
		t.Error("Dearest of an unknown name should not be ok")
	}
}
