// Package questevidence turns the fleet's logs and the quest side stores into the evidence for promoting
// a quest into bot.ProvenQuests, and judges it against the promotion rule. It is pure: it reads text and
// JSON it is handed, runs no git and touches no file but the ones named to ReadStores (cmd/questpromote
// does the I/O and asks git).
//
// WHY IT EXISTS. A quest's promotion was argued from a survey of whatever the logs held: "no ladder parks"
// was true of the 14 and 15 October window, in which every account that could finish The Restless Ghost
// already had, and false of the 156 runs made on builds holding the module's last change (118 of them
// parks). The rule the lead set on 15 Oct 2026 is 10 or more completions on 10 or more distinct accounts
// since the module last changed, no deaths, and at most 0.5 parks per run; this package counts exactly that,
// from the lines the engine and the fleet already write.
package questevidence

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Kind is what a log line says happened to a quest on an account.
type Kind string

const (
	// Run is "[ladder] running <id> (...)": the ladder started the quest's slot.
	Run Kind = "run"
	// Complete is "[ladder] <id> ended complete": the quest tab read complete at the end of the run.
	// RunLadder only starts a quest whose tab does not read complete, so this is a quest that was
	// finished during the run.
	Complete Kind = "complete"
	// Parked is "[ladder] <id> ended parked (...)".
	Parked Kind = "parked"
	// Timeout is "[ladder] <id> ended timeout": the slot ran out.
	Timeout Kind = "timeout"
	// Stopped is "[ladder] <id> ended stopped": the bot was stopped (a relaunch, the watchdog).
	Stopped Kind = "stopped"
	// Died is "[<id>] DIED during ...".
	Died Kind = "died"
	// LostFight is "[<id>] LOST A FIGHT during ...": a fight the account was losing and walked away from.
	LostFight Kind = "lost-fight"
	// Completed is the engine's own "[<id>] COMPLETED build=..." line (questplan.CompletionLogLine), which
	// names the build and the decisions the run took.
	Completed Kind = "completed"
)

// Event is one quest-relevant fleet log line.
type Event struct {
	At      time.Time
	Account string
	Quest   string
	Kind    Kind
	// Build is the build the event ran on: for Completed the build the line names, for every other kind the
	// build of the log segment (the last "controlpanel: build" line before it), "" when the file named none.
	Build string
	// Dirty is the segment's build having been made from a modified tree (never set for Completed, whose
	// Build carries its own "+dirty").
	Dirty bool
	// Ticks is Completed's decision count.
	Ticks int
}

// BuildLine is a fleet start's "controlpanel: build <sha>, committed <time>[ (with uncommitted changes ...)]".
type BuildLine struct {
	At    time.Time
	Build string
	Dirty bool
}

const stampLen = len("2006/01/02 15:04:05")

// parseStamp reads the fleet log's local-time prefix.
func parseStamp(line string, loc *time.Location) (time.Time, bool) {
	if len(line) < stampLen+1 || line[stampLen] != ' ' {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006/01/02 15:04:05", line[:stampLen], loc)
	return t, err == nil
}

// ParseBuildLine reads the fleet's build line (cmd/controlpanel logs it once at start).
func ParseBuildLine(line string, loc *time.Location) (BuildLine, bool) {
	at, ok := parseStamp(line, loc)
	if !ok {
		return BuildLine{}, false
	}
	rest := line[stampLen+1:]
	const marker = "controlpanel: build "
	if !strings.HasPrefix(rest, marker) {
		return BuildLine{}, false
	}
	rest = rest[len(marker):]
	end := strings.IndexAny(rest, ", ")
	if end < 0 {
		end = len(rest)
	}
	sha := rest[:end]
	if len(sha) < 7 || !isHex(sha) {
		return BuildLine{}, false
	}
	return BuildLine{At: at, Build: sha, Dirty: strings.Contains(rest, "uncommitted changes")}, true
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return s != ""
}

// ParseLine reads a quest event from one fleet log line, in the form
//
//	2026/10/15 19:29:52 [account name/ScarabSwarm] message
//
// ok is false for every other line, including the build line (ParseBuildLine).
func ParseLine(line string, loc *time.Location) (Event, bool) {
	at, ok := parseStamp(line, loc)
	if !ok {
		return Event{}, false
	}
	rest := line[stampLen+1:]
	if len(rest) < 4 || rest[0] != '[' {
		return Event{}, false
	}
	// The account may hold spaces and digits but not "] ": the first "] " ends the prefix, and its last "/"
	// separates the account from the script that logged it.
	end := strings.Index(rest, "] ")
	if end < 0 {
		return Event{}, false
	}
	prefix := rest[1:end]
	slash := strings.LastIndexByte(prefix, '/')
	if slash <= 0 {
		return Event{}, false
	}
	account, msg := prefix[:slash], rest[end+2:]
	ev := Event{At: at, Account: account}

	if strings.HasPrefix(msg, "[ladder] ") {
		f := strings.Fields(msg[len("[ladder] "):])
		switch {
		case len(f) >= 2 && f[0] == "running":
			ev.Quest, ev.Kind = f[1], Run
		case len(f) >= 3 && f[1] == "ended":
			ev.Quest = f[0]
			switch f[2] {
			case "complete":
				ev.Kind = Complete
			case "parked":
				ev.Kind = Parked
			case "timeout":
				ev.Kind = Timeout
			case "stopped":
				ev.Kind = Stopped
			default:
				return Event{}, false
			}
		default:
			return Event{}, false
		}
		return ev, validQuest(ev.Quest)
	}
	if strings.HasPrefix(msg, "[") {
		if id, c, ok := questplan.ParseCompletionLog(msg); ok {
			ev.Quest, ev.Kind, ev.Build, ev.Ticks = id, Completed, c.Build, c.Ticks
			if c.Account != "" {
				ev.Account = c.Account
			}
			return ev, validQuest(id)
		}
		close := strings.IndexByte(msg, ']')
		if close < 2 {
			return Event{}, false
		}
		ev.Quest = msg[1:close]
		switch tail := msg[close+1:]; {
		case strings.HasPrefix(tail, " DIED during"):
			ev.Kind = Died
		case strings.HasPrefix(tail, " LOST A FIGHT during"):
			ev.Kind = LostFight
		default:
			return Event{}, false
		}
		return ev, validQuest(ev.Quest)
	}
	return Event{}, false
}

// validQuest accepts a quest id as the engine spells one: lower-case letters and digits.
func validQuest(id string) bool {
	if id == "" || id == "ladder" {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// quickMarkers are the substrings a line must hold to be worth parsing; the logs are gigabytes and almost
// every line is something else.
var quickMarkers = [][]byte{
	[]byte("[ladder] "), []byte("DIED during"), []byte("LOST A FIGHT during"), []byte("] COMPLETED build="),
	[]byte("controlpanel: build "),
}

func interesting(line []byte) bool {
	for _, m := range quickMarkers {
		if bytes.Contains(line, m) {
			return true
		}
	}
	return false
}

// ReadLines calls fn with every line of r, cut to its first max bytes (the rest of a longer line is
// dropped: every line this package reads is short, and a log may hold a line of megabytes). fn's slice is
// only valid for the call.
func ReadLines(r io.Reader, max int, fn func(line []byte)) error {
	br := bufio.NewReaderSize(r, 256<<10)
	var buf []byte
	for {
		chunk, isPrefix, err := br.ReadLine()
		if len(chunk) > max {
			chunk = chunk[:max]
		}
		if err != nil {
			if err == io.EOF {
				if len(buf) > 0 {
					fn(buf)
				}
				return nil
			}
			return err
		}
		if !isPrefix && len(buf) == 0 {
			// The common case: the whole line is in the reader's buffer, no copy.
			fn(chunk)
			continue
		}
		if room := max - len(buf); room > 0 {
			if len(chunk) > room {
				chunk = chunk[:room]
			}
			buf = append(buf, chunk...)
		}
		if !isPrefix {
			fn(buf)
			buf = buf[:0]
		}
	}
}

// Scanner folds log lines, file after file in time order, into events and build segments.
type Scanner struct {
	loc *time.Location
	// After drops every line stamped before it (a window floor; zero keeps all).
	After time.Time

	Events   []Event
	Segments []Segment
	// Lines counts the lines fed, Parsed the events and builds taken from them.
	Lines, Parsed int

	cur Segment // the build in force
}

// Segment is a stretch of one log run under one build, from its build line to the next.
type Segment struct {
	Build string
	Dirty bool
	Start time.Time
}

// NewScanner makes a Scanner reading stamps in loc (the machine's local time, nil for time.Local).
func NewScanner(loc *time.Location) *Scanner {
	if loc == nil {
		loc = time.Local
	}
	return &Scanner{loc: loc}
}

// StartFile marks a new log file: the build in force is not carried over from the previous one (a file
// that names no build is "" and its events cannot be attributed).
func (s *Scanner) StartFile() { s.cur = Segment{} }

// Feed takes one line.
func (s *Scanner) Feed(line string) { s.FeedBytes([]byte(line)) }

// FeedBytes takes one line without copying it unless it is worth parsing.
func (s *Scanner) FeedBytes(b []byte) {
	s.Lines++
	if !interesting(b) {
		return
	}
	line := string(b)
	if bl, ok := ParseBuildLine(line, s.loc); ok {
		s.cur = Segment{Build: bl.Build, Dirty: bl.Dirty, Start: bl.At}
		if s.After.IsZero() || !bl.At.Before(s.After) {
			// A build older than the floor still governs the lines after it, but is not listed.
			s.Segments = append(s.Segments, s.cur)
			s.Parsed++
		}
		return
	}
	ev, ok := ParseLine(line, s.loc)
	if !ok {
		return
	}
	if !s.After.IsZero() && ev.At.Before(s.After) {
		return
	}
	if ev.Kind != Completed {
		ev.Build, ev.Dirty = s.cur.Build, s.cur.Dirty
	}
	s.Events = append(s.Events, ev)
	s.Parsed++
}

// FeedReader feeds every line of r.
func (s *Scanner) FeedReader(r io.Reader) error {
	return ReadLines(r, 16<<10, s.FeedBytes)
}
