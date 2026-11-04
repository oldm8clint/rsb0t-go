package questevidence

import (
	"strings"
	"testing"
	"time"
)

var utc = time.UTC

func mustEvent(t *testing.T, line string) Event {
	t.Helper()
	ev, ok := ParseLine(line, utc)
	if !ok {
		t.Fatalf("not parsed: %q", line)
	}
	return ev
}

func TestParseLineReadsTheLadderLines(t *testing.T) {
	ev := mustEvent(t, "2026/10/15 19:29:51 [alpha/ScarabSwarm] [ladder] running scorpion (14/21 complete, slot 1h52m30s)")
	if ev.Kind != Run || ev.Quest != "scorpion" || ev.Account != "alpha" || !ev.At.Equal(time.Date(2026, 10, 15, 19, 29, 51, 0, utc)) {
		t.Fatalf("run: %+v", ev)
	}
	// An account name holds spaces, and the reason holds slashes and brackets.
	ev = mustEvent(t, `2026/10/07 03:39:57 [Test Account/ScarabSwarm] [ladder] ghost ended parked (its walks cannot get there: 5 walks to 3120,9567,0 ended unreachable [a/b] x)`)
	if ev.Kind != Parked || ev.Quest != "ghost" || ev.Account != "Test Account" {
		t.Fatalf("parked: %+v", ev)
	}
	for _, c := range []struct {
		outcome string
		kind    Kind
	}{{"complete", Complete}, {"timeout", Timeout}, {"stopped", Stopped}, {"parked", Parked}} {
		ev = mustEvent(t, "2026/10/07 10:00:00 [a b/ScarabSwarm] [ladder] imp ended "+c.outcome+" ()")
		if ev.Kind != c.kind || ev.Quest != "imp" {
			t.Errorf("%s: %+v", c.outcome, ev)
		}
	}
}

func TestParseLineReadsDeathsAndFightsLost(t *testing.T) {
	ev := mustEvent(t, `2026/10/07 04:00:00 [Some One/ScarabSwarm] [ghost] DIED during "ghost-skull (the skull)" (death 1 for this quest)`)
	if ev.Kind != Died || ev.Quest != "ghost" || ev.Account != "Some One" {
		t.Fatalf("died: %+v", ev)
	}
	ev = mustEvent(t, `2026/10/07 04:01:29 [Some One/ScarabSwarm] [pip] LOST A FIGHT during "pip-monk" and walked away (near-death 1, the cap is 2 inside 24h0m0s): Monk: hp low`)
	if ev.Kind != LostFight || ev.Quest != "pip" {
		t.Fatalf("lost fight: %+v", ev)
	}
}

func TestParseLineReadsTheEnginesOwnCompletionLine(t *testing.T) {
	build := strings.Repeat("ab", 20)
	ev := mustEvent(t, "2026/10/16 09:00:00 [Some One/ScarabSwarm] [ghost] COMPLETED build="+build+" ticks=17 account=Some One")
	if ev.Kind != Completed || ev.Quest != "ghost" || ev.Build != build || ev.Ticks != 17 || ev.Account != "Some One" {
		t.Fatalf("completed: %+v", ev)
	}
}

func TestParseLineIgnoresEverythingElse(t *testing.T) {
	for _, line := range []string{
		"",
		"no stamp [a/ScarabSwarm] [ladder] running ghost (0/1)",
		"2026/10/15 19:29:49 [beta/ScarabSwarm] [ladder] horror is held back, not run: needs Prayer 43",
		"2026/10/15 19:29:49 [beta/ScarabSwarm] [ladder] not starting grail: 3m of the pass budget is left",
		"2026/10/15 19:29:49 [beta/ScarabSwarm] [ladder] ghost: clearing a park an older build wrote (x)",
		"2026/10/15 19:29:49 [beta/ScarabSwarm] [ghost] parking: no progress for 8 ticks",
		"2026/10/15 19:29:49 [beta/ScarabSwarm] scarabswarm: stage 5 - quest ladder done",
		"2026/10/15 19:29:49 controlpanel: connecting 359 of 359 accounts",
		"2026/10/15 19:29:49 [ladder] running ghost (0/1)", // no account prefix
		"2026/10/15 19:29:49 [beta/ScarabSwarm] [ladder] ghost ended weird ()",
	} {
		if ev, ok := ParseLine(line, utc); ok {
			t.Errorf("%q parsed as %+v", line, ev)
		}
	}
}

func TestParseBuildLine(t *testing.T) {
	sha := strings.Repeat("1f", 20)
	bl, ok := ParseBuildLine("2026/10/15 18:04:37 controlpanel: build "+sha+", committed 2026-10-15T09:49:52Z", utc)
	if !ok || bl.Build != sha || bl.Dirty || !bl.At.Equal(time.Date(2026, 10, 15, 18, 4, 37, 0, utc)) {
		t.Fatalf("clean: %+v %v", bl, ok)
	}
	bl, ok = ParseBuildLine("2026/09/18 01:07:43 controlpanel: build "+sha+", committed 2026-09-17T17:05:42Z (with uncommitted changes - the update check below compares against the LAST COMMIT)", utc)
	if !ok || !bl.Dirty || bl.Build != sha {
		t.Fatalf("dirty: %+v %v", bl, ok)
	}
	for _, bad := range []string{"2026/10/15 18:04:37 controlpanel: no VCS build info available", "2026/10/15 18:04:37 controlpanel: build zzzz, committed x", "controlpanel: build " + sha} {
		if _, ok := ParseBuildLine(bad, utc); ok {
			t.Errorf("%q parsed as a build line", bad)
		}
	}
}

func TestReadLinesCutsLongLinesAndKeepsTheRest(t *testing.T) {
	long := strings.Repeat("x", 100000)
	in := "first\r\n" + long + "\n\nlast without newline"
	var got []string
	if err := ReadLines(strings.NewReader(in), 50, func(b []byte) { got = append(got, string(b)) }); err != nil {
		t.Fatal(err)
	}
	want := []string{"first", strings.Repeat("x", 50), "", "last without newline"}
	if len(got) != len(want) {
		t.Fatalf("got %d lines %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: %q, want %q", i, got[i], want[i])
		}
	}
}

// The scanner attributes each event to the build in force when it was logged, forgets the build at a new
// file, and drops what is stamped before its floor.
func TestScannerTagsEventsWithTheirBuildSegment(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	s := NewScanner(utc)
	feed := func(lines ...string) {
		for _, l := range lines {
			s.Feed(l)
		}
	}
	s.StartFile()
	feed(
		"2026/10/07 01:00:00 [x/ScarabSwarm] [ladder] running ghost (0/1)", // before any build line: no build
		"2026/10/07 02:00:00 controlpanel: build "+a+", committed 2026-10-07T00:00:00Z",
		"2026/10/07 02:10:00 [x/ScarabSwarm] [ladder] running ghost (0/1)",
		"2026/10/07 03:00:00 controlpanel: build "+b+", committed 2026-10-07T01:00:00Z (with uncommitted changes)",
		"2026/10/07 03:10:00 [x/ScarabSwarm] [ladder] ghost ended complete ()",
	)
	s.StartFile()
	feed("2026/10/07 04:00:00 [y/ScarabSwarm] [ladder] running imp (0/1)") // a new file: the last build is not carried over
	if len(s.Events) != 4 || len(s.Segments) != 2 {
		t.Fatalf("events %d segments %d", len(s.Events), len(s.Segments))
	}
	if s.Events[0].Build != "" || s.Events[1].Build != a || s.Events[1].Dirty || s.Events[2].Build != b || !s.Events[2].Dirty || s.Events[3].Build != "" {
		t.Fatalf("builds: %+v", s.Events)
	}

	floor := NewScanner(utc)
	floor.After = time.Date(2026, 10, 7, 2, 30, 0, 0, utc)
	floor.StartFile()
	floor.Feed("2026/10/07 02:00:00 controlpanel: build " + a + ", committed x")
	floor.Feed("2026/10/07 02:10:00 [x/ScarabSwarm] [ladder] running ghost (0/1)")    // before the floor
	floor.Feed("2026/10/07 02:40:00 [x/ScarabSwarm] [ladder] ghost ended parked (x)") // after it, under the older build
	if len(floor.Events) != 1 || floor.Events[0].Build != a || len(floor.Segments) != 0 {
		t.Fatalf("floor: events %+v segments %+v", floor.Events, floor.Segments)
	}
}
