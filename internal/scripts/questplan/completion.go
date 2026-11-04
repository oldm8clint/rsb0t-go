package questplan

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// A COMPLETION is the evidence behind a promotion. The side store used to keep one bit about it
// (QuestRecord.SeenComplete: "this account's tab once read complete"), which says nothing about WHICH
// build finished the quest, so "ten accounts finished it on code that still reads the way it does today"
// could not be checked, and a quest was promoted into ProvenQuests on a survey of whatever the logs held
// (live 15 Oct 2026: the promotion rule needs 10+ completions on 10+ accounts since the module last
// changed). cmd/questpromote reads these records, and the log line CompletionLogLine writes, to answer it.

// MaxCompletions is how many completions a quest's record keeps, newest last. A quest completes once
// per account, so one or two is the normal count; the cap only bounds a record that is somehow driven
// to its end again and again.
const MaxCompletions = 8

// Completion is one time the engine saw a quest reach its end on this account.
type Completion struct {
	// At is when (unix ms).
	At int64 `json:"at"`
	// Account is the player's name as the game spells it (the side store's directory has underscores).
	Account string `json:"account,omitempty"`
	// Build is the binary's VCS revision, the full 40 hex digits, "+dirty" when it was built from a
	// modified tree (bot.BuildIdentity); "unknown" without VCS stamping. A dirty or unknown build cannot be
	// matched to a module's last change, so cmd/questpromote does not count it.
	Build string `json:"build,omitempty"`
	// Ticks is how many decide passes the run took. 1 is a quest that was complete before the run did
	// anything (the first decision was Done): not a completion this build earned, and not counted.
	Ticks int `json:"ticks,omitempty"`
}

// Earned reports whether the run did work: the first decision was not already Done. A record that
// does not know (Ticks 0, written by nothing today) is given the benefit of the doubt.
func (c Completion) Earned() bool { return c.Ticks != 1 }

// NoteCompletion records a completion on a quest's record and keeps the newest MaxCompletions.
func (s *Store) NoteCompletion(id string, c Completion) {
	r := s.Record(id)
	r.Completions = append(r.Completions, c)
	if n := len(r.Completions); n > MaxCompletions {
		r.Completions = append([]Completion(nil), r.Completions[n-MaxCompletions:]...)
	}
	r.UpdatedAt = c.At
}

// CompletionLogLine is the log line a completion writes beside the store record, so the evidence
// survives a lost store and a log can be read on its own:
//
//	[ghost] COMPLETED build=<40 hex>[+dirty] ticks=17 account=Some Name
//
// The account is last because a player name may hold spaces.
func CompletionLogLine(id string, c Completion) string {
	build := c.Build
	if build == "" {
		build = "unknown"
	}
	return fmt.Sprintf("[%s] COMPLETED build=%s ticks=%d account=%s", id, build, c.Ticks, c.Account)
}

var completionLine = regexp.MustCompile(`^\[([A-Za-z0-9_]+)\] COMPLETED build=(\S+) ticks=(\d+) account=(.*)$`)

// ParseCompletionLog reads a CompletionLogLine back (the part of a fleet log line after its
// "[account/Script] " prefix).
func ParseCompletionLog(msg string) (id string, c Completion, ok bool) {
	m := completionLine.FindStringSubmatch(strings.TrimRight(msg, " \r\n"))
	if m == nil {
		return "", Completion{}, false
	}
	ticks, err := strconv.Atoi(m[3])
	if err != nil {
		return "", Completion{}, false
	}
	return m[1], Completion{Account: m[4], Build: m[2], Ticks: ticks}, true
}
