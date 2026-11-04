package muleledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLedgerPersistsAndReloadsTrades(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mule-ledger.jsonl")
	l := Open(path, 10)
	l.Record(Entry{AtMs: 1000, Mule: "mule", Partner: "Fisher1", Kind: KindReceive, Outcome: OutcomeAccepted,
		In: []Item{{ID: 996, Name: "Coins", Count: 250}, {ID: 1624, Name: "Uncut sapphire", Count: 2}}})
	l.Record(Entry{AtMs: 2000, Mule: "mule", Partner: "Fisher2", Kind: KindReceive, Outcome: OutcomeFailed, Reason: "confirm screen timeout"})
	l.Record(Entry{AtMs: 3000, Mule: "mule", Partner: "Acct1", Kind: KindHandout, Outcome: OutcomeAccepted,
		Out: []Item{{ID: -1, Name: "Lobster pot", Count: 1}}})
	if err := l.Err(); err != nil {
		t.Fatal(err)
	}

	again := Open(path, 10)
	got := again.Entries(Query{})
	if len(got) != 3 {
		t.Fatalf("reloaded %d entries, want 3", len(got))
	}
	if got[0].Partner != "Fisher1" || len(got[0].In) != 2 || got[0].In[0].Count != 250 {
		t.Fatalf("first entry not preserved: %+v", got[0])
	}
	if got[1].Outcome != OutcomeFailed || got[1].Reason != "confirm screen timeout" || len(got[1].In) != 0 {
		t.Fatalf("failed entry not preserved: %+v", got[1])
	}
	if got[2].Kind != KindHandout || got[2].Out[0].Name != "Lobster pot" {
		t.Fatalf("hand-out not preserved: %+v", got[2])
	}
}

func TestLedgerStaysBoundedOnDiskAndInMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := Open(path, 5)
	for i := 1; i <= 23; i++ {
		l.Record(Entry{AtMs: int64(i), Mule: "m", Partner: "p", Kind: KindReceive, Outcome: OutcomeAccepted})
	}
	if l.Len() != 5 {
		t.Fatalf("kept %d in memory, want 5", l.Len())
	}
	if kept := l.Entries(Query{}); len(kept) != 5 || kept[0].AtMs != 19 || kept[4].AtMs != 23 {
		t.Fatalf("kept the wrong window in memory: %+v", kept)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(data), "\n"); lines > 10 {
		t.Fatalf("file holds %d lines, want at most twice the kept 5", lines)
	}
	reloaded := Open(path, 5).Entries(Query{})
	if len(reloaded) != 5 || reloaded[0].AtMs != 19 || reloaded[4].AtMs != 23 {
		t.Fatalf("reloaded the wrong window: %+v", reloaded)
	}
	// No temporary file is left behind by the rewrites.
	matches, _ := filepath.Glob(path + ".*.tmp")
	if len(matches) != 0 {
		t.Fatalf("left temporary files: %v", matches)
	}
}

func TestLedgerSkipsATornLastLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	content := `{"at":1,"mule":"m","partner":"a","kind":"receive","outcome":"accepted"}` + "\n" +
		`{"at":2,"mule":"m","partner":"b","kind":"rec`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	l := Open(path, 10)
	if l.Len() != 1 || l.Err() != nil {
		t.Fatalf("len %d err %v, want the one whole line and no error", l.Len(), l.Err())
	}
	l.Record(Entry{AtMs: 3, Mule: "m", Partner: "c", Kind: KindReceive, Outcome: OutcomeAccepted})
	if got := Open(path, 10).Entries(Query{}); len(got) != 2 || got[1].Partner != "c" {
		t.Fatalf("append after a torn line: %+v", got)
	}
}

func TestLedgerQueryFiltersNewestFirstWindow(t *testing.T) {
	l := Open("", 0)
	for i := 1; i <= 10; i++ {
		mule := "a"
		if i%2 == 0 {
			mule = "b"
		}
		l.Record(Entry{AtMs: int64(i * 100), Mule: mule, Kind: KindReceive, Outcome: OutcomeAccepted})
	}
	got := l.Entries(Query{SinceMs: 300, Mule: "b", Limit: 2})
	if len(got) != 2 || got[0].AtMs != 800 || got[1].AtMs != 1000 {
		t.Fatalf("query picked %+v, want mule b's newest two since 300, oldest first", got)
	}
	if all := l.Entries(Query{}); len(all) != 10 {
		t.Fatalf("unfiltered query returned %d", len(all))
	}
}

func TestLedgerStampsTimeAndIsNilSafe(t *testing.T) {
	l := Open("", 3)
	l.now = func() time.Time { return time.UnixMilli(42) }
	l.Record(Entry{Mule: "m"})
	if got := l.Entries(Query{}); len(got) != 1 || got[0].AtMs != 42 {
		t.Fatalf("time not stamped: %+v", got)
	}
	var none *Ledger
	none.Record(Entry{})
	if none.Entries(Query{}) != nil || none.Len() != 0 || none.Err() != nil {
		t.Fatal("nil ledger is not a no-op")
	}
}

// OutSince is what a storage round asks: which items has this mule handed out since a time, and how
// far back does the record reach. Only accepted hand-outs count, every line of a kit counts, a name
// is compared ignoring case, and another mule's trades are not this mule's.
func TestOutSinceTotalsAcceptedHandoutsByNameAndSaysHowFarBackItReaches(t *testing.T) {
	const day = int64(24 * 60 * 60 * 1000)
	l := Open("", 0)
	l.Record(Entry{AtMs: 1 * day, Mule: "mule", Partner: "Fisher1", Kind: KindReceive, Outcome: OutcomeAccepted,
		In: []Item{{ID: 995, Name: "Coins", Count: 5}}})
	l.Record(Entry{AtMs: 2 * day, Mule: "mule", Partner: "Acct1", Kind: KindHandout, Outcome: OutcomeAccepted,
		Out: []Item{{ID: 1359, Name: "Rune axe", Count: 1}, {ID: -1, Name: "Shark", Count: 20}}})
	l.Record(Entry{AtMs: 8 * day, Mule: "mule", Partner: "Acct2", Kind: KindHandout, Outcome: OutcomeAccepted,
		Out: []Item{{ID: -1, Name: "Shark", Count: 5}, {ID: 199, Name: "Herb id199", Count: 7}, {ID: -1, Name: "", Count: 3}}})
	l.Record(Entry{AtMs: 9 * day, Mule: "mule", Partner: "Acct3", Kind: KindHandout, Outcome: OutcomeFailed, Reason: "the kit never crossed",
		Out: []Item{{ID: -1, Name: "Dragon dagger", Count: 1}}})
	l.Record(Entry{AtMs: 9 * day, Mule: "mule2", Partner: "Acct4", Kind: KindHandout, Outcome: OutcomeAccepted,
		Out: []Item{{ID: -1, Name: "Steel claws", Count: 1}}})

	got, from := l.OutSince(" Mule ", time.UnixMilli(5*day))
	if len(got) != 2 || got["shark"] != 5 || got["herb id199"] != 7 {
		t.Fatalf("since day 5: %v, want the 5 Sharks and the 7 Herb id199 of the one hand-out after it", got)
	}
	if !from.Equal(time.UnixMilli(1 * day)) {
		t.Fatalf("the record reaches back to %v, want the mule's oldest entry (day 1), whatever its kind", from)
	}
	all, _ := l.OutSince("mule", time.Time{})
	if all["shark"] != 25 || all["rune axe"] != 1 || all["dragon dagger"] != 0 || all["steel claws"] != 0 {
		t.Fatalf("from the beginning: %v (a failed hand-out and another mule's moved nothing here)", all)
	}
	// A mule the record never saw: nothing handed out, and no horizon to speak of.
	none, from := l.OutSince("nobody", time.Time{})
	if len(none) != 0 || !from.IsZero() {
		t.Fatalf("an unknown mule: %v %v", none, from)
	}
	var nilLedger *Ledger
	if got, from := nilLedger.OutSince("mule", time.Time{}); got != nil || !from.IsZero() {
		t.Fatalf("a nil ledger answers nothing: %v %v", got, from)
	}
}

// MasterCrafter's storage round calls an item unused when the mule has not handed it out for a week,
// which the record can only say if it reaches back a week: the live the mule wrote 3,480 entries a day
// (7,472 in the 51.5 hours of 3 to 5 Oct 2026), and DefaultKeep was 5000, a day and a half.
func TestDefaultKeepReachesBackAWeekAtTheLiveRate(t *testing.T) {
	const entriesPerDay = 3480
	if DefaultKeep < 7*entriesPerDay {
		t.Fatalf("DefaultKeep %d reaches back %.1f days at %d entries a day, want a week", DefaultKeep, float64(DefaultKeep)/entriesPerDay, entriesPerDay)
	}
}
