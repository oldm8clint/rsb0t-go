// Package chatlog is a tiny bounded ring buffer of recent text lines, keyed by arrival
// time, so a caller can ask "what came in since I clicked" (Since) or "show me the last
// N lines" (Recent) without owning its own packet-decode goroutine. internal/fleet feeds
// one per account from its read loop's MESSAGE_GAME case (a.chat); scripts read it back
// through internal/bot's Context, or a standalone tool (cmd/barbtest) owns one directly.
//
// This is exactly cmd/barbtest's own msgs/recordMessage/seenSince pattern, pulled out so
// both the standalone tool and the generalized bot runtime share one implementation.
// internal/fleet also reuses this exact type (not a separate one) for a second, unrelated
// purpose — a per-account activity log of Manager.logf lines (bot start/stop, guardian
// activity, script progress) surfaced to the dashboard as AccountStatus.RecentLog — since
// the shape (bounded, thread-safe, append-only, read-back-recent-lines) is identical and
// keeping it as one implementation means a fix or improvement here benefits both.
//
// WHAT IS NOT IN HERE: another player's speech. internal/fleet's read loop Records the
// MESSAGE_GAME case and nothing else, so this buffer only ever holds the engine's own lines
// ("You catch a mackerel."). Public chat arrives on PLAYER_INFO's CHAT mask instead, and
// gamestate turns it into an EventChatMessage with ChatType ChatPublic/ChatPublicMod that
// never reaches here. Measured 2026-09-21 by cmd/publicchattest, which feeds a Log exactly the
// way internal/fleet does and confirms the line it just watched arrive on the Event stream is
// absent from it. Consequences worth knowing: the control panel's chat feed is built from this
// buffer (fleet.AccountStatus.RecentChat/ChatCount), so it CANNOT show player speech and its
// silence is no evidence the fleet is not receiving any; and a script that wants to read a
// market's spoken price must call State.Subscribe itself, as scarabrunner.BuyFromMarket does —
// d.Chat will never have it.
package chatlog

import (
	"strings"
	"sync"
	"time"
)

// maxLines bounds memory per account. Sized for the larger of this type's two current
// uses: a settle check (Since) only ever looks back a few seconds, but the dashboard
// activity log (Recent) wants up to ~50 lines of scrollback, so the cap needs enough
// headroom above that for Recent(50) to actually have 50 to return most of the time.
const maxLines = 100

type entry struct {
	at   time.Time
	text string
}

// Log is a small thread-safe ring buffer of recent lines.
type Log struct {
	mu    sync.Mutex
	lines []entry
	head  int // physical index of the oldest retained line
	// total counts every Record call ever made, independent of maxLines trimming — see
	// Total's own doc comment for why a consumer needs this.
	total int
}

// New returns an empty Log.
func New() *Log { return &Log{} }

// Record appends text, timestamped now, dropping the oldest line past the buffer's cap.
func (l *Log) Record(text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := entry{at: time.Now(), text: text}
	if len(l.lines) < maxLines {
		l.lines = append(l.lines, e)
	} else {
		l.lines[l.head] = e
		l.head++
		if l.head == len(l.lines) {
			l.head = 0
		}
	}
	l.total++
}

// index maps a chronological offset to storage. The caller must hold mu.
func (l *Log) index(offset int) int {
	i := l.head + offset
	if i >= len(l.lines) {
		i -= len(l.lines)
	}
	return i
}

// Total returns how many lines have ever been Record'd, never reset and never trimmed
// (unlike Recent, which is capped at maxLines). A consumer that wants to turn "give me
// the last N lines" into "give me only the lines I haven't seen yet" can remember the
// Total() it last observed and diff against the new one on its next poll — internal/
// controlpanel's FleetSource does exactly this to turn each account's raw chat capture
// (see internal/fleet's account.chat field doc) into individual streamed chat.message
// events instead of only a polled recent-N snapshot, the same way it already diffs
// account state for its own state-change events.
func (l *Log) Total() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.total
}

// ReadAfter copies unread retained lines and advances the cursor in the same lock.
// since is used only for a consumer's initial subscription time; zero accepts all.
func (l *Log) ReadAfter(cursor int, since time.Time) ([]string, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if cursor >= l.total {
		return nil, l.total
	}
	first := l.total - len(l.lines)
	start := 0
	if cursor > first {
		start = cursor - first
	}
	var out []string
	for i := start; i < len(l.lines); i++ {
		e := l.lines[l.index(i)]
		if !e.at.Before(since) {
			out = append(out, e.text)
		}
	}
	return out, l.total
}

// Since joins every line recorded at or after t, newline-separated, for a regexp to scan
// against. Safe to call repeatedly on a poll loop rather than draining a channel, so a slow
// poll never misses a line that arrived between checks.
func (l *Log) Since(t time.Time) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out strings.Builder
	size := 0
	for _, e := range l.lines {
		if !e.at.Before(t) {
			size += len(e.text) + 1
		}
	}
	out.Grow(size)
	for i := range l.lines {
		e := l.lines[l.index(i)]
		if !e.at.Before(t) {
			out.WriteString(e.text)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// Recent returns up to the last n lines recorded, oldest first (chronological order) —
// the same order they arrived in, just without Since's time cutoff. Returns fewer than n
// (or nil) if that's all there is. Safe to call on every poll, same as Since.
func (l *Log) Recent(n int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || len(l.lines) == 0 {
		return nil
	}
	if n > len(l.lines) {
		n = len(l.lines)
	}
	start := len(l.lines) - n
	out := make([]string, n)
	for i := range out {
		out[i] = l.lines[l.index(start+i)].text
	}
	return out
}
