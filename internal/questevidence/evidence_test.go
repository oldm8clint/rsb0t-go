package questevidence

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

func sha(c string) string { return strings.Repeat(c, 40) }

var t0 = time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)

func at(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }

// gitLike places builds: "old" builds predate the change, "since" ones hold it, anything else is unknown
// to the repository.
func gitLike(old, since map[string]bool) func(string) Attribution {
	return func(b string) Attribution {
		switch {
		case old[b]:
			return Before
		case since[b]:
			return Since
		}
		return Unknown
	}
}

type evb struct {
	h     int
	kind  Kind
	acct  string
	build string
	dirty bool
	ticks int
}

func events(quest string, in ...evb) []Event {
	var out []Event
	for _, e := range in {
		out = append(out, Event{At: at(e.h), Account: e.acct, Quest: quest, Kind: e.kind, Build: e.build, Dirty: e.dirty, Ticks: e.ticks})
	}
	return out
}

func TestEvaluateCountsOnlyWhatRanTheModuleAsItIsNow(t *testing.T) {
	oldB, newB, strayB := sha("a"), sha("b"), sha("c")
	w := Window{Bounded: true, Contains: gitLike(map[string]bool{oldB: true}, map[string]bool{newB: true})}
	evs := events("ghost",
		evb{h: 1, kind: Run, acct: "A", build: oldB},
		evb{h: 1, kind: Parked, acct: "A", build: oldB}, // the old module's park: not counted
		evb{h: 2, kind: Run, acct: "A", build: newB},
		evb{h: 3, kind: Complete, acct: "A", build: newB},
		evb{h: 2, kind: Run, acct: "B", build: newB},
		evb{h: 3, kind: Parked, acct: "B", build: newB},
		evb{h: 4, kind: Run, acct: "B", build: newB},
		evb{h: 5, kind: Complete, acct: "B", build: newB},
		evb{h: 5, kind: Run, acct: "C", build: strayB},                // a build git does not know: reported, not counted
		evb{h: 6, kind: Run, acct: "D", build: sha("d"), dirty: true}, // a modified tree, whatever commit it started from
		evb{h: 7, kind: Died, acct: "E", build: newB},
		evb{h: 8, kind: LostFight, acct: "E", build: newB},
	)
	segs := []Segment{{Build: oldB, Start: at(0)}, {Build: newB, Start: at(2)}}
	in := NewInput(evs, segs, nil)
	ev := Evaluate("ghost", in, w)
	if ev.Runs != 3 || ev.Parks != 1 || ev.Completions != 2 || ev.Accounts != 2 {
		t.Fatalf("runs %d parks %d completions %d accounts %d: %+v", ev.Runs, ev.Parks, ev.Completions, ev.Accounts, ev)
	}
	if ev.Died != 1 || ev.LostFight != 1 || ev.Deaths() != 2 || ev.DeathAccounts != 1 {
		t.Fatalf("deaths: died %d lost %d accounts %d", ev.Died, ev.LostFight, ev.DeathAccounts)
	}
	if ev.Unattributed != 2 {
		t.Fatalf("unattributed %d, want the stray build and the dirty one", ev.Unattributed)
	}
	if len(ev.Builds) != 1 || ev.Builds[0].Build != newB || ev.Builds[0].Runs != 3 {
		t.Fatalf("builds %+v", ev.Builds)
	}
	if ev.FirstBuild != newB || !ev.FirstAt.Equal(at(2)) {
		t.Fatalf("first build %q at %v", ev.FirstBuild, ev.FirstAt)
	}
	if ppr, ok := ev.ParksPerRun(); !ok || ppr < 0.33 || ppr > 0.34 {
		t.Fatalf("parks per run %v %v", ppr, ok)
	}

	// The same dirty build counts when the operator says it may (its revision is a build git places).
	w.AllowDirty = true
	w.Contains = gitLike(map[string]bool{oldB: true}, map[string]bool{newB: true, sha("d"): true})
	if ev := Evaluate("ghost", in, w); ev.Runs != 4 || ev.Unattributed != 1 {
		t.Fatalf("allow dirty: runs %d unattributed %d", ev.Runs, ev.Unattributed)
	}
}

// One account seen three ways is one completion, and a quest finished on the first decision is not one.
func TestEvaluateCountsAnAccountOnceAcrossLogsAndStores(t *testing.T) {
	b := sha("b")
	w := Window{Bounded: true, Contains: gitLike(nil, map[string]bool{b: true})}
	evs := events("imp",
		evb{h: 3, kind: Complete, acct: "Some One", build: b},
		evb{h: 3, kind: Completed, acct: "Some One", build: b, ticks: 9},
		evb{h: 4, kind: Completed, acct: "Idle", build: b, ticks: 1}, // already done when the run began
	)
	store := func(acct string, rec *questplan.QuestRecord) StoreFile {
		s := questplan.NewStore()
		s.Quests["imp"] = rec
		return StoreFile{Account: acct, Store: s}
	}
	stores := []StoreFile{
		// the same player, spelled as the store directory spells it, recorded too
		store("Some_One", &questplan.QuestRecord{SeenComplete: true, Completions: []questplan.Completion{{At: at(3).UnixMilli(), Account: "Some One", Build: b, Ticks: 9}}}),
		// a player only the store knows
		store("Store_Only", &questplan.QuestRecord{SeenComplete: true, Completions: []questplan.Completion{{At: at(5).UnixMilli(), Account: "Store Only", Build: b, Ticks: 4}}}),
		// a recorded completion on the first decision
		store("Idle2", &questplan.QuestRecord{SeenComplete: true, Completions: []questplan.Completion{{At: at(5).UnixMilli(), Build: b, Ticks: 1}}}),
		// an old record naming no build, last changed after the first build holding the change went live
		store("Legacy_New", &questplan.QuestRecord{SeenComplete: true, UpdatedAt: at(6).UnixMilli()}),
		// one last changed before it
		store("Legacy_Old", &questplan.QuestRecord{SeenComplete: true, UpdatedAt: at(1).UnixMilli()}),
		// not complete
		store("Nope", &questplan.QuestRecord{Attempts: 3, Parks: 3}),
	}
	segs := []Segment{{Build: sha("a"), Start: at(0)}, {Build: b, Start: at(2)}}
	ev := Evaluate("imp", NewInput(evs, segs, stores), w)
	if ev.Accounts != 3 || ev.Completions != 3 {
		t.Fatalf("accounts %d completions %d: %v", ev.Accounts, ev.Completions, ev.AccountList)
	}
	if ev.FromLog != 1 || ev.FromRecorded != 2 || ev.FromLegacy != 1 {
		t.Fatalf("sources: log %d recorded %d legacy %d", ev.FromLog, ev.FromRecorded, ev.FromLegacy)
	}
	if ev.Unearned != 2 || ev.LegacyBefore != 1 {
		t.Fatalf("unearned %d legacyBefore %d", ev.Unearned, ev.LegacyBefore)
	}
}

// A quest is finished once per account. An account whose "ended complete" line repeats (the Barcrawl logs
// one 49,000 times) is one completion and the rest are repeats, so a loop cannot buy a pass.
func TestEvaluateCountsARepeatedCompletionLineOnce(t *testing.T) {
	b := sha("b")
	w := Window{Bounded: true, Contains: gitLike(nil, map[string]bool{b: true})}
	var evs []evb
	for i := 0; i < 5; i++ {
		evs = append(evs, evb{h: 3 + i, kind: Complete, acct: "Looper", build: b})
	}
	evs = append(evs, evb{h: 9, kind: Complete, acct: "Other", build: b})
	ev := Evaluate("barcrawl", NewInput(events("barcrawl", evs...), nil, nil), w)
	if ev.Completions != 2 || ev.Accounts != 2 || ev.Repeats != 4 {
		t.Fatalf("completions %d accounts %d repeats %d, want 2, 2 and 4", ev.Completions, ev.Accounts, ev.Repeats)
	}
}

// With no log segment holding the change, a record that names no build cannot be placed.
func TestEvaluateCannotPlaceALegacyRecordWithoutALogSegment(t *testing.T) {
	w := Window{Bounded: true, Contains: gitLike(nil, map[string]bool{sha("b"): true})}
	s := questplan.NewStore()
	s.Quests["ghost"] = &questplan.QuestRecord{SeenComplete: true, UpdatedAt: at(9).UnixMilli()}
	ev := Evaluate("ghost", NewInput(nil, []Segment{{Build: sha("a"), Start: at(0)}}, []StoreFile{{Account: "x", Store: s}}), w)
	if ev.Completions != 0 || ev.Unattributed != 1 {
		t.Fatalf("completions %d unattributed %d", ev.Completions, ev.Unattributed)
	}
}

func TestEvaluateTimeModeUsesTheFloor(t *testing.T) {
	evs := events("imp",
		evb{h: 1, kind: Run, acct: "A"}, evb{h: 1, kind: Complete, acct: "A"},
		evb{h: 5, kind: Run, acct: "B"}, evb{h: 5, kind: Complete, acct: "B"},
	)
	s := questplan.NewStore()
	s.Quests["imp"] = &questplan.QuestRecord{SeenComplete: true, UpdatedAt: at(6).UnixMilli()}
	in := NewInput(evs, nil, []StoreFile{{Account: "C", Store: s}})
	ev := Evaluate("imp", in, Window{Bounded: true, From: at(4)})
	if ev.Runs != 1 || ev.Accounts != 2 || ev.FromLegacy != 1 {
		t.Fatalf("runs %d accounts %d legacy %d", ev.Runs, ev.Accounts, ev.FromLegacy)
	}
}

func TestTheLeadRuleAtItsEdges(t *testing.T) {
	good := Evidence{Quest: "q", Bounded: true, Completions: 10, Accounts: 10, Runs: 20, Parks: 10}
	if v := LeadRule.Judge(good); !v.Pass {
		t.Fatalf("exactly at the thresholds must pass: %v", v)
	}
	for name, mut := range map[string]func(e *Evidence){
		"9 completions":          func(e *Evidence) { e.Completions = 9 },
		"9 accounts":             func(e *Evidence) { e.Accounts = 9 },
		"one death":              func(e *Evidence) { e.Died = 1 },
		"one fight lost":         func(e *Evidence) { e.LostFight = 1 },
		"parks just over a half": func(e *Evidence) { e.Parks = 11 },
		"no runs":                func(e *Evidence) { e.Runs, e.Parks = 0, 0 },
		"unbounded window":       func(e *Evidence) { e.Bounded = false },
	} {
		e := good
		mut(&e)
		if v := LeadRule.Judge(e); v.Pass || len(v.Failures) == 0 {
			t.Errorf("%s passed: %v", name, v)
		}
	}
	// Every failing check is named, not just the first.
	bad := Evidence{Quest: "q", Bounded: true, Completions: 3, Accounts: 3, Runs: 10, Parks: 9, Died: 1}
	if v := LeadRule.Judge(bad); len(v.Failures) != 4 {
		t.Fatalf("failures %v", v.Failures)
	}
}

func TestReadStoresReadsGoodFilesAndReportsBadOnesWithoutTouchingThem(t *testing.T) {
	dir := t.TempDir()
	write := func(acct, content string) string {
		d := filepath.Join(dir, acct)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(d, StoreFileName)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("Good_One", `{"version":1,"quests":{"ghost":{"seenComplete":true,"completions":[{"at":5,"account":"Good One","build":"abc","ticks":3}]}}}`)
	badPath := write("Broken", `{"quests":`)
	if err := os.MkdirAll(filepath.Join(dir, "NoStore"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stores, bad, err := ReadStores(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(stores) != 1 || stores[0].Account != "Good_One" || len(stores[0].Store.Quests["ghost"].Completions) != 1 {
		t.Fatalf("stores %+v", stores)
	}
	if len(bad) != 1 || !strings.Contains(bad[0], "Broken") {
		t.Fatalf("bad %v", bad)
	}
	if _, err := os.Stat(badPath); err != nil {
		t.Fatalf("the broken store was moved or removed: %v", err)
	}
	if _, _, err := ReadStores(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing directory read as empty")
	}
}

// This package names the store file itself rather than link the engine for it; the two must agree.
func TestStoreFileNameMatchesTheEngines(t *testing.T) {
	if StoreFileName != questrunner.StoreFileName {
		t.Fatalf("%q != %q", StoreFileName, questrunner.StoreFileName)
	}
}

func TestQuestsListsEverythingSeen(t *testing.T) {
	s := questplan.NewStore()
	s.Record("zeta")
	in := NewInput(events("alpha", evb{h: 1, kind: Run}), nil, []StoreFile{{Account: "x", Store: s}})
	if got := fmt.Sprint(in.Quests()); got != "[alpha zeta]" {
		t.Fatalf("%s", got)
	}
}
