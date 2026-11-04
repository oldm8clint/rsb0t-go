package questrunner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStorePathSitsBesideTheCheckpoint(t *testing.T) {
	p, err := StorePath("data/scarabswarm-checkpoints", "bakers stall")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("data/scarabswarm-checkpoints", "bakers_stall", StoreFileName); p != want {
		t.Fatalf("path %q, want %q", p, want)
	}
	for _, bad := range []string{"", "..", "a/b", `a\b`} {
		if _, err := StorePath("d", bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestStoreRoundTripAndAMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "qst1", StoreFileName)
	s, err := LoadStore(path)
	if err != nil || len(s.Quests) != 0 {
		t.Fatalf("a missing file is an empty store: %v %+v", err, s)
	}
	s.NoteDeath("waterfall", time.Unix(1_800_000_000, 0))
	if err := SaveStore(path, s); err != nil {
		t.Fatal(err)
	}
	back, err := LoadStore(path)
	if err != nil || back.Record("waterfall").Deaths != 1 {
		t.Fatalf("round trip: %v %+v", err, back.Quests)
	}
}

// AIO-PLAN A2: a file that will not decode is kept aside, never silently overwritten.
func TestAnUnreadableStoreIsKeptAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, StoreFileName)
	if err := os.WriteFile(path, []byte(`{"quests":`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadStore(path)
	if err == nil || s == nil || len(s.Quests) != 0 {
		t.Fatalf("want an empty store and an error, got %v %+v", err, s)
	}
	entries, _ := os.ReadDir(dir)
	kept := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), StoreFileName+".bad-") {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("the bad file was not kept: %v", entries)
	}
}
