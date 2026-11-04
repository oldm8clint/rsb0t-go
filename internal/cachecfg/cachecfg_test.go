package cachecfg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// table builds a .idx/.dat pair from raw records.
func table(records ...[]byte) (idx, dat []byte) {
	idx = []byte{byte(len(records) >> 8), byte(len(records))}
	dat = []byte{0, 0}
	for _, r := range records {
		idx = append(idx, byte(len(r)>>8), byte(len(r)))
		dat = append(dat, r...)
	}
	return idx, dat
}

// rec concatenates bytes and strings; a string is written with its newline terminator.
func rec(parts ...interface{}) []byte {
	var out []byte
	for _, p := range parts {
		switch v := p.(type) {
		case int:
			out = append(out, byte(v))
		case string:
			out = append(out, v...)
			out = append(out, '\n')
		}
	}
	return out
}

func synthetic(t *testing.T) *Config {
	t.Helper()
	objIdx, objDat := table(
		rec(2, "Flax", 16, 12, 0, 0, 0, 5, 0),
		rec(97, 0, 0, 98, 0, 2, 0),
		rec(2, "Note", 0),
		rec(30, "HiDdEn", 31, "Take", 35, "Drop", 0),
		rec(200, 2, "X", 0),
	)
	npcIdx, npcDat := table(
		rec(2, "Man", 12, 2, 95, 0, 2, 30, "Talk-to", 37, "Ignored", 1, 2, 0, 1, 0, 2, 0),
	)
	locIdx, locDat := table(
		rec(5, 1, 0, 1, 0),
		rec(1, 1, 0, 1, 22, 0),
		rec(1, 2, 0, 1, 10, 0, 2, 22, 0),
		rec(19, 0, 30, "Open", 0),
		rec(2, "Bank booth", 14, 2, 15, 3, 74, 69, 4, 31, "Use", 77, 0, 1, 1, 0, 5, 0, 6, 0),
	)
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

// coinsConfig builds the shape the real 289 cache actually has: several objs all named
// "Coins", with the currency NOT first in id order. In that cache they are 617 (a members
// obj) and 995 (the currency); here they are compressed to small ids so the ordering, not
// the numbers, is what is under test.
func coinsConfig(t *testing.T) *Config {
	t.Helper()
	objIdx, objDat := table(
		rec(2, "Flax", 16, 12, 0, 0, 0, 5, 0),
		rec(2, "Coins", 16, 0),                 // members, not stackable — the 617-alike
		rec(97, 0, 3, 98, 0, 2, 0),             // a NOTE of the currency: same name, Noted()
		rec(2, "Coins", 11, 12, 0, 0, 0, 1, 0), // the real currency — the 995-alike
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

// The bug this guards: ObjIDs returns ids in id order, so ObjIDs("Coins")[0] is the
// members obj, and anything counting a pack or a bank by that id reads zero however much
// money moved. The preference for id 995 itself cannot be reached by a synthetic table
// this small; it is exercised against the real archive every time a live harness resolves
// coins through this helper.
func TestCurrencyIDSkipsTheMembersObjAndTheNoteOfTheSameName(t *testing.T) {
	c := coinsConfig(t)
	ids := c.ObjIDs("Coins")
	if len(ids) != 3 || ids[0] != 1 {
		t.Fatalf("fixture: ObjIDs(%q) = %v, want three ids with the members obj first", "Coins", ids)
	}
	if first, _ := c.Obj(ids[0]); !first.Members || first.Stackable {
		t.Fatalf("fixture: obj %d should be the members, non-stackable one: %+v", ids[0], first)
	}
	if note, _ := c.Obj(ids[1]); !note.Noted() {
		t.Fatalf("fixture: obj %d should be the note: %+v", ids[1], note)
	}
	got, ok := c.CurrencyID()
	if !ok || got != 3 {
		t.Errorf("CurrencyID() = %d, %v; want the stackable, non-members, non-noted obj 3 — never the first id %d", got, ok, ids[0])
	}
}

func TestCurrencyIDReportsWhenNothingIsNamedCoins(t *testing.T) {
	c := synthetic(t)
	if got, ok := c.CurrencyID(); ok {
		t.Errorf("CurrencyID() = %d, true; want false when no obj is named Coins", got)
	}
}

func TestSyntheticRecords(t *testing.T) {
	c := synthetic(t)
	if o, n, l := c.Counts(); o != 5 || n != 1 || l != 5 {
		t.Fatalf("counts %d %d %d", o, n, l)
	}

	flax, _ := c.Obj(0)
	if flax.Name != "Flax" || !flax.Members || flax.Cost != 5 || flax.Stackable || flax.Noted() {
		t.Errorf("obj 0 %+v", flax)
	}
	note, _ := c.Obj(1)
	if note.Name != "Flax" || !note.Members || note.Cost != 5 || !note.Stackable || !note.Noted() || note.CertLink != 0 {
		t.Errorf("note did not take its link's name, members, cost and stackability: %+v", note)
	}
	ops, _ := c.Obj(3)
	if ops.Ops != [5]string{"", "Take"} || ops.IOps != [5]string{"Drop"} {
		t.Errorf("hidden op not removed, or ops misplaced: %+v", ops)
	}
	if x, _ := c.Obj(4); x.Name != "X" {
		t.Errorf("unknown opcode consumed bytes: %+v", x)
	}

	man, _ := c.Npc(0)
	if man.Name != "Man" || man.Size != 2 || man.Level != 2 || man.Ops != [5]string{"Talk-to"} {
		t.Errorf("npc %+v", man)
	}

	wantActive := []bool{true, false, true, false, true}
	for id, want := range wantActive {
		if l, _ := c.Loc(id); l.Active != want {
			t.Errorf("loc %d active = %v, want %v", id, l.Active, want)
		}
	}
	booth, _ := c.Loc(4)
	if booth.Width != 2 || booth.Length != 3 || booth.BlockWalk || booth.BlockRange || booth.ForceApproach != 4 || OpIndex(booth.Ops, "use") != 2 {
		t.Errorf("booth %+v", booth)
	}
	if l, _ := c.Loc(0); !l.BlockWalk || !l.BlockRange || l.Width != 1 {
		t.Errorf("loc defaults %+v", l)
	}

	if got := c.LocIDs("BANK BOOTH"); !reflect.DeepEqual(got, []int{4}) {
		t.Errorf("LocIDs = %v", got)
	}
	if got := c.ObjIDs("flax"); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Errorf("ObjIDs = %v", got)
	}
	if got := c.NpcIDs("nobody"); got != nil {
		t.Errorf("NpcIDs = %v", got)
	}
	if _, ok := c.Loc(99); ok {
		t.Error("out of range loc found")
	}
	if OpIndex(booth.Ops, "Search") != 0 {
		t.Error("OpIndex matched a missing option")
	}

	allocs := testing.AllocsPerRun(100, func() {
		c.Loc(4)
		c.Obj(1)
		c.Npc(0)
	})
	if allocs != 0 {
		t.Errorf("accessors allocated %v times", allocs)
	}
}

func TestTruncatedRecordIsAnError(t *testing.T) {
	objIdx, objDat := table()
	npcIdx, npcDat := table()
	locIdx, locDat := table(rec(14))
	if _, err := build(map[string][]byte{"obj.idx": objIdx, "obj.dat": objDat, "npc.idx": npcIdx, "npc.dat": npcDat, "loc.idx": locIdx, "loc.dat": locDat}); err == nil {
		t.Fatal("a record that runs off the end decoded without error")
	}
	if _, err := build(map[string][]byte{"obj.idx": {0}, "obj.dat": nil}); err == nil {
		t.Fatal("a short .idx decoded without error")
	}
}

type vectorTable struct {
	Count  int              `json:"count"`
	Digest string           `json:"digest"`
	Sample []string         `json:"sample"`
	Named  map[string][]int `json:"named"`
}

type vector struct {
	ArchiveSHA256 string      `json:"archiveSHA256"`
	Obj           vectorTable `json:"obj"`
	Npc           vectorTable `json:"npc"`
	Loc           vectorTable `json:"loc"`
}

func bit(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func itoa(v int) string { return strconv.Itoa(v) }

// The line formats match cachecfg_reference_vector.test.ts exactly.
func objLine(o Obj) string {
	return strings.Join([]string{itoa(o.ID), o.Name, bit(o.Members), bit(o.Stackable), strconv.Itoa(int(o.Cost)), itoa(o.CertLink), itoa(o.CertTemplate), strings.Join(o.Ops[:], "|"), strings.Join(o.IOps[:], "|")}, "\t")
}

func npcLine(n Npc) string {
	return strings.Join([]string{itoa(n.ID), n.Name, itoa(n.Size), itoa(n.Level), strings.Join(n.Ops[:], "|")}, "\t")
}

func locLine(l Loc) string {
	return strings.Join([]string{itoa(l.ID), l.Name, itoa(l.Width), itoa(l.Length), bit(l.BlockWalk), bit(l.BlockRange), bit(l.Active), itoa(l.ForceApproach), strings.Join(l.Ops[:], "|")}, "\t")
}

func checkTable(t *testing.T, kind string, want vectorTable, count int, line func(id int) string, ids func(name string) []int) {
	t.Helper()
	if count != want.Count {
		t.Fatalf("%s: %d types, TS decoded %d", kind, count, want.Count)
	}
	h := sha256.New()
	byLine := map[string]bool{}
	for id := 0; id < count; id++ {
		l := line(id)
		h.Write([]byte(l + "\n"))
		byLine[l] = true
	}
	for _, s := range want.Sample {
		if !byLine[s] {
			id, _ := strconv.Atoi(strings.SplitN(s, "\t", 2)[0])
			t.Errorf("%s %d:\n got %q\nwant %q", kind, id, line(id), s)
		}
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want.Digest {
		t.Errorf("%s: digest over every type differs from the TS decoders (%s vs %s)", kind, got[:12], want.Digest[:12])
	}
	for name, wantIDs := range want.Named {
		if got := ids(name); !reflect.DeepEqual(got, wantIDs) {
			t.Errorf("%s ids for %q = %v, want %v", kind, name, got, wantIDs)
		}
	}
}

// TestMatchesTypeScriptDecoders compares every obj, npc and loc type against the vendored
// client's own decoders run over the same archive (test/fixtures/cachecfg_vector.json).
// Set RSB0T_CONFIG_JAG to that archive (GET /config<crc> from the engine) to run it.
func TestMatchesTypeScriptDecoders(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("set RSB0T_CONFIG_JAG to the config archive to compare against the TS decoders")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "cachecfg_vector.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v vector
	if err := json.Unmarshal(fixture, &v); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != v.ArchiveSHA256 {
		t.Skipf("the fixture was generated from a different archive (%s)", v.ArchiveSHA256[:12])
	}
	c, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	objs, npcs, locs := c.Counts()
	checkTable(t, "obj", v.Obj, objs, func(id int) string { o, _ := c.Obj(id); return objLine(o) }, c.ObjIDs)
	checkTable(t, "npc", v.Npc, npcs, func(id int) string { n, _ := c.Npc(id); return npcLine(n) }, c.NpcIDs)
	checkTable(t, "loc", v.Loc, locs, func(id int) string { l, _ := c.Loc(id); return locLine(l) }, c.LocIDs)
}
