package fleet

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBankStoreMemoryOnlyRoundTrips(t *testing.T) {
	s := newBankStore("")
	if _, ok := s.get("nobody"); ok {
		t.Fatal("get on an empty store: want ok=false")
	}
	rec := bankRecord{Items: []SlotStatus{{Slot: 0, ID: 995, Count: 1000}}, UpdatedAt: "2026-09-14T12:00:00Z"}
	s.set("main", rec)
	got, ok := s.get("main")
	if !ok || len(got.Items) != 1 || got.Items[0].ID != 995 || got.UpdatedAt != rec.UpdatedAt {
		t.Fatalf("get after set = %+v, %v", got, ok)
	}
}

func TestBankStorePersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bankstate.json")
	s1 := newBankStore(path)
	s1.set("casket", bankRecord{Items: []SlotStatus{{Slot: 0, ID: 1265, Count: 1}, {Slot: 1, ID: 1436, Count: 250}}, UpdatedAt: "2026-09-14T12:00:00Z"})
	s1.set("mule", bankRecord{Items: []SlotStatus{{Slot: 0, ID: 995, Count: 5000}}, UpdatedAt: "2026-09-14T12:05:00Z"})

	// A fresh store over the same path (simulating a process restart) must see both
	// records exactly as they were written, not just the most recent set call — proves
	// save persists the WHOLE map each time, not just the one changed entry.
	s2 := newBankStore(path)
	casket, ok := s2.get("casket")
	if !ok || len(casket.Items) != 2 || casket.Items[1].ID != 1436 || casket.Items[1].Count != 250 {
		t.Fatalf("casket after reload = %+v, %v", casket, ok)
	}
	muleAcct, ok := s2.get("mule")
	if !ok || len(muleAcct.Items) != 1 || muleAcct.Items[0].Count != 5000 {
		t.Fatalf("mule after reload = %+v, %v", muleAcct, ok)
	}
}

func TestBankStoreLoadFromMissingFileStartsEmptyNotError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	s := newBankStore(path)
	if _, ok := s.get("anyone"); ok {
		t.Fatal("get on a store loaded from a missing file: want ok=false")
	}
	// Still usable — set/get should work normally from here, and a later reload should
	// see what was set.
	s.set("main", bankRecord{Items: []SlotStatus{{Slot: 0, ID: 995, Count: 1}}, UpdatedAt: "2026-09-14T12:00:00Z"})
	s2 := newBankStore(path)
	if _, ok := s2.get("main"); !ok {
		t.Fatal("reload after first set: want main's record to be found")
	}
}

// TestManagerSnapshotFallsBackToCachedBankWithNoSession covers the actual account-
// owner-facing behavior: once a bank has been observed once (simulated here by priming
// the cache directly, since a real observation needs a live *client.Session — see this
// package's own white-box test file doc comment), an account with no live session
// (disconnected, or never reconnected) still reports its last-known bank via Snapshot.
func TestManagerSnapshotFallsBackToCachedBankWithNoSession(t *testing.T) {
	m := New(Config{})
	m.accounts = append(m.accounts, &account{username: "offline_acct"})
	m.bank.set("offline_acct", bankRecord{Items: []SlotStatus{{Slot: 0, ID: 995, Count: 42}}, UpdatedAt: "2026-09-14T12:00:00Z"})

	got := m.Snapshot()
	if len(got) != 1 {
		t.Fatalf("Snapshot() = %+v", got)
	}
	s := got[0]
	if len(s.BankItems) != 1 || s.BankItems[0].ID != 995 || s.BankItems[0].Count != 42 {
		t.Errorf("BankItems not served from cache: %+v", s.BankItems)
	}
	if s.BankUpdatedAt != "2026-09-14T12:00:00Z" {
		t.Errorf("BankUpdatedAt = %q, want the cached value", s.BankUpdatedAt)
	}
}

func TestManagerSnapshotReportsNoBankWhenNeverObservedOrCached(t *testing.T) {
	m := New(Config{})
	m.accounts = append(m.accounts, &account{username: "brand_new"})
	got := m.Snapshot()
	if len(got) != 1 || len(got[0].BankItems) != 0 || got[0].BankUpdatedAt != "" {
		t.Fatalf("Snapshot() for an account never seen banking = %+v", got)
	}
}

func TestUnchangedBankSnapshotDoesNotRewriteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "banks.json")
	s := newBankStore(path)
	rec := bankRecord{Items: []SlotStatus{{Slot: 0, ID: 995, Count: 100}}, UpdatedAt: "2026-09-18T00:00:00Z"}
	s.set("mule", rec)
	stamp := time.Unix(100, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 266; i++ {
		s.set("mule", rec)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(stamp) {
		t.Fatal("unchanged snapshot rewrote bank file")
	}
	rec.Items = []SlotStatus{{Slot: 0, ID: 995, Count: 101}}
	s.set("mule", rec)
	loaded := newBankStore(path)
	got, _ := loaded.get("mule")
	if got.Items[0].Count != 101 {
		t.Fatal("changed bank was not persisted")
	}
}

func TestFailedBankSaveRetriesUnchangedObservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocked.json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	s := newBankStore(path)
	rec := bankRecord{Items: []SlotStatus{{ID: 995, Count: 10}}, UpdatedAt: "2026-09-18T00:00:00Z"}
	s.set("mule", rec)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s.set("mule", rec)
	loaded := newBankStore(path)
	if _, ok := loaded.get("mule"); !ok {
		t.Fatal("failed write was not retried")
	}
}
