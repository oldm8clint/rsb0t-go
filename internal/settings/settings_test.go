package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/mulereceiverplan"
)

// TestStoreRoundTripsMuleConfig proves the actual gap this package fills: a real *plan settings
// shape (mulereceiverplan.MuleConfig) survives Encode -> Save -> Load -> Decode with every value
// intact, using t.TempDir() so nothing touches a real file.
func TestStoreRoundTripsMuleConfig(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	want := mulereceiverplan.MuleDefaults()
	// Change enough fields away from the defaults that a decode bug (wrong key, dropped field,
	// zero-value fallback masking a real value) would show up as a mismatch.
	want.Partners = []string{"Foo", "Bar"}
	want.PmOrders = false
	want.TrustBus = true
	want.Post = "Falador"
	want.PostTile = "3093,3437,0"
	want.WalkRadius = 77
	want.KitLaw = 15
	want.KitAir = 250
	want.AlchFireStaff = false
	want.AlchItems = []string{"Iron ore", "Gold ore"}
	want.AlchCasts = 12

	bag := NewBag()
	if err := bag.Encode(want); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := store.Save("main", mulereceiverplan.MuleStoreID, bag); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The file should actually be on disk, under the account/script layout the doc comment
	// promises, and readable as plain JSON (not just round-trippable through this package).
	path, err := store.Path("main", mulereceiverplan.MuleStoreID)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if filepath.Dir(path) != filepath.Join(dir, "main") {
		t.Fatalf("Path = %s, want under %s", path, filepath.Join(dir, "main"))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file missing after Save: %v", err)
	}

	loaded, err := store.Load("main", mulereceiverplan.MuleStoreID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var got mulereceiverplan.MuleConfig
	if err := loaded.Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("round-tripped config differs:\n want %+v\n  got %+v", want, got)
	}
}

// TestStoreLoadMissingFileIsEmptyBag confirms a script's first run (nothing saved yet) is not an
// error: Load returns an empty Bag, so decoding into a settings struct yields its zero value and
// the caller's own defaults constructor (e.g. MuleDefaults) is expected to take over from there.
func TestStoreLoadMissingFileIsEmptyBag(t *testing.T) {
	store := NewStore(t.TempDir())
	bag, err := store.Load("nobody", "nothing-saved")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	raw := bag.Raw()
	if len(raw) != 0 {
		t.Fatalf("expected an empty Bag, got %v", raw)
	}
}

// TestBagTypedAccessors exercises the SettingsBag-shaped Bool/Num/Str/List accessors and their
// fallback behaviour on a wrong-typed or absent key.
func TestBagTypedAccessors(t *testing.T) {
	bag, err := ParseBag([]byte(`{"pickups": true, "walkRadius": 50, "post": "Seers", "partners": ["A", "B"], "wrongType": "not-a-bool"}`))
	if err != nil {
		t.Fatalf("ParseBag: %v", err)
	}
	if got := bag.Bool("pickups", false); got != true {
		t.Fatalf("Bool(pickups) = %v, want true", got)
	}
	if got := bag.Bool("missing", true); got != true {
		t.Fatalf("Bool(missing) fallback = %v, want true", got)
	}
	if got := bag.Int("walkRadius", -1); got != 50 {
		t.Fatalf("Int(walkRadius) = %d, want 50", got)
	}
	if got := bag.Str("post", "default"); got != "Seers" {
		t.Fatalf("Str(post) = %q, want Seers", got)
	}
	if got := bag.List("partners", nil); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("List(partners) = %v, want [A B]", got)
	}
	if got := bag.Bool("wrongType", false); got != false {
		t.Fatalf("Bool(wrongType) fallback = %v, want false", got)
	}

	bag.Set("pickups", false)
	if got := bag.Bool("pickups", true); got != false {
		t.Fatalf("Bool(pickups) after Set = %v, want false", got)
	}
	bag.Delete("post")
	if got := bag.Str("post", "default"); got != "default" {
		t.Fatalf("Str(post) after Delete = %q, want default", got)
	}
}

// TestStoreRejectsBadNames confirms account/script names can't be used to escape the settings
// directory or otherwise carry filesystem-hostile characters.
func TestStoreRejectsBadNames(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.Path("../escape", "script"); err == nil {
		t.Fatal("expected an error for a path-traversal account name")
	}
	if _, err := store.Path("account", ""); err == nil {
		t.Fatal("expected an error for an empty script name")
	}
}

// Thirteen accounts on the live roster have spaces in their names, and before 2026-09-22
// every settings write for them failed with "must be 1-64 letters, digits, '.', '_' or '-'".
// ScarabSwarm persists its stage checkpoint through this Store, so those accounts could never
// save one and restarted from Stage 1 on every fleet restart — 373 and 325 failures for two
// accounts in a single run.
func TestPathAcceptsTheSpacesRealUsernamesHave(t *testing.T) {
	s := NewStore(t.TempDir())

	got, err := s.Path("bakers stall", "ScarabSwarm")
	if err != nil {
		t.Fatalf("Path(%q) = %v, want a usable path — real accounts have spaces in their names",
			"bakers stall", err)
	}
	if !strings.Contains(got, "bakers_stall") {
		t.Errorf("Path = %q, want the space written as an underscore so it is a legal path component", got)
	}

	// The game compares usernames by userhash, so these two names ARE the same account. Giving
	// them one path is correct, not a collision.
	under, err := s.Path("bakers_stall", "ScarabSwarm")
	if err != nil {
		t.Fatalf("Path with an underscore: %v", err)
	}
	if got != under {
		t.Errorf("%q and %q resolved to different paths (%q vs %q) — the server treats them as "+
			"one account, so their settings must not split", "bakers stall", "bakers_stall", got, under)
	}
}

// The substitution must not weaken the traversal guard it sits in front of.
func TestPathStillRefusesTraversalAndOddCharacters(t *testing.T) {
	s := NewStore(t.TempDir())
	for _, bad := range []string{"..", "../escape", "a/b", `a\b`, "", "a:b", "a*b"} {
		if _, err := s.Path(bad, "ScarabSwarm"); err == nil {
			t.Errorf("Path(%q) was accepted; it must still be refused", bad)
		}
	}
	// A name that is only spaces collapses to only underscores, which is a legal component and
	// is fine — it cannot escape the directory.
	if _, err := s.Path("  ", "ScarabSwarm"); err != nil {
		t.Errorf("Path(%q) = %v; underscores are a legal component", "  ", err)
	}
}

// A save and load round-trip for a spaced name, which is what actually broke.
func TestSaveAndLoadRoundTripForASpacedName(t *testing.T) {
	s := NewStore(t.TempDir())
	bag := NewBagFrom(map[string]interface{}{"stage": 3})
	if err := s.Save("choc slice", "ScarabSwarm", bag); err != nil {
		t.Fatalf("Save for a spaced name: %v", err)
	}
	back, err := s.Load("choc slice", "ScarabSwarm")
	if err != nil {
		t.Fatalf("Load for a spaced name: %v", err)
	}
	if got := back.Int("stage", 0); got != 3 {
		t.Errorf("stage came back %d, want 3 — the checkpoint must survive a restart", got)
	}
}

// Save skips a write whose bytes match the file this Store last wrote, and writes again once
// the file changed under it.
func TestSaveSkipsUnchangedWrite(t *testing.T) {
	s := NewStore(t.TempDir())
	bag := NewBag()
	bag.Set("n", 1)
	if err := s.Save("acct", "Script", bag); err != nil {
		t.Fatal(err)
	}
	path, _ := s.Path("acct", "Script")
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	// The file changed under the Store (its time moved), so this writes.
	if err := s.Save("acct", "Script", bag); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	if fi.ModTime().Equal(old) {
		t.Fatalf("a file changed on disk was not rewritten")
	}
	written := fi.ModTime()
	if err := s.Save("acct", "Script", bag); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); !fi.ModTime().Equal(written) {
		t.Fatalf("an unchanged save rewrote the file")
	}
	if err := os.WriteFile(path, []byte(`{"n": 2}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("acct", "Script", bag); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), `"n": 1`) {
		t.Fatalf("a hand-edited file was not overwritten by a real save: %s", got)
	}
}
