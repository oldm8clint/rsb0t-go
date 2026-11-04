package questplan

import (
	"fmt"
	"strings"
	"testing"
)

func TestNoteCompletionKeepsTheNewestAndStampsTheRecord(t *testing.T) {
	s := NewStore()
	for i := 1; i <= MaxCompletions+3; i++ {
		s.NoteCompletion("ghost", Completion{At: int64(i), Account: fmt.Sprintf("acct%d", i), Build: "b", Ticks: i + 1})
	}
	r := s.Record("ghost")
	if len(r.Completions) != MaxCompletions {
		t.Fatalf("kept %d completions, want the newest %d", len(r.Completions), MaxCompletions)
	}
	if first, last := r.Completions[0], r.Completions[len(r.Completions)-1]; first.Account != "acct4" || last.Account != fmt.Sprintf("acct%d", MaxCompletions+3) {
		t.Fatalf("kept the wrong ones: first %+v last %+v", first, last)
	}
	if r.UpdatedAt != int64(MaxCompletions+3) {
		t.Fatalf("UpdatedAt %d, want the last completion's time", r.UpdatedAt)
	}
}

// A store written before completions existed decodes unchanged, and one with completions survives the
// round trip: the side store is read leniently across builds (AIO-PLAN A2).
func TestCompletionsSurviveTheStoreRoundTripAndAnOldStoreStillDecodes(t *testing.T) {
	old := []byte(`{"version":1,"quests":{"ghost":{"attempts":3,"seenComplete":true,"updatedAt":1790000000000}}}`)
	s, err := DecodeStore(old)
	if err != nil {
		t.Fatal(err)
	}
	if r := s.Record("ghost"); !r.SeenComplete || len(r.Completions) != 0 {
		t.Fatalf("old record changed on decode: %+v", r)
	}
	s.NoteCompletion("ghost", Completion{At: 1790000001000, Account: "Test Player", Build: strings.Repeat("a", 40), Ticks: 12})
	data, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeStore(data)
	if err != nil {
		t.Fatal(err)
	}
	got := back.Record("ghost").Completions
	if len(got) != 1 || got[0].Account != "Test Player" || got[0].Ticks != 12 || got[0].Build != strings.Repeat("a", 40) {
		t.Fatalf("round trip: %+v", got)
	}
}

// A completion whose first decision was already Done did no work this build, so it is not evidence;
// a record that does not say how long it ran is given the benefit of the doubt.
func TestACompletionOnTheFirstTickIsNotEarned(t *testing.T) {
	for _, c := range []struct {
		ticks int
		want  bool
	}{{1, false}, {2, true}, {40, true}, {0, true}} {
		if got := (Completion{Ticks: c.ticks}).Earned(); got != c.want {
			t.Errorf("ticks %d: Earned %v, want %v", c.ticks, got, c.want)
		}
	}
}

func TestTheCompletionLogLineReadsBack(t *testing.T) {
	build := strings.Repeat("0123456789", 4)
	line := CompletionLogLine("ghost", Completion{Account: "Test Player", Build: build + "+dirty", Ticks: 17})
	id, c, ok := ParseCompletionLog(line)
	if !ok || id != "ghost" || c.Account != "Test Player" || c.Build != build+"+dirty" || c.Ticks != 17 {
		t.Fatalf("%q read back as %q %+v %v", line, id, c, ok)
	}
	// No VCS stamping is written as "unknown", never as an empty build that would parse as a different line.
	if _, c, ok := ParseCompletionLog(CompletionLogLine("imp", Completion{Account: "x", Ticks: 3})); !ok || c.Build != "unknown" {
		t.Fatalf("an unstamped build: %+v %v", c, ok)
	}
	for _, bad := range []string{"", "[ghost] parking: x", "[ghost] COMPLETED build= ticks=3 account=x", "[ghost] COMPLETED build=abc ticks=x account=y"} {
		if _, _, ok := ParseCompletionLog(bad); ok {
			t.Errorf("%q parsed as a completion", bad)
		}
	}
}
