// Package muleledger is the fleet's persisted record of every trade a mule finished: when, which
// mule, which partner, what came in, what went out, and how it ended. It is the shared trade
// record the control panel's mule dashboard reads (GET /api/mule-ledger), and the history
// internal/mulestatus deliberately does not keep.
//
// It records, it never decides. The MuleReceiver writes one Entry per handled trade from the
// point where its own trade code has already reached a verdict: an accepted receive only after
// mulerunner's receipt check saw the promised items arrive in the pack (a07ebeb), a hand-out only
// after the order code confirmed the delivery. Nothing here is read back by any trade decision.
//
// On disk it is JSON lines, appended one per trade and bounded: once the file holds twice the
// kept count it is rewritten with the newest entries only, through a temporary file and a rename,
// so a crash mid-rewrite leaves the old file. A torn last line (a crash mid-append) is skipped
// on load. An empty path keeps everything in memory only.
package muleledger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kinds of engagement.
const (
	KindReceive = "receive" // a donor handed its load to the mule
	KindHandout = "handout" // the mule gave a blocked donor an item it asked for
)

// Outcomes.
const (
	OutcomeAccepted = "accepted" // the trade completed and the items were verified
	OutcomeFailed   = "failed"   // the trade was declined, interrupted or never verified
)

// DefaultKeep is how many entries a Ledger keeps when Open is given no bound: about eleven days of
// the live the mule. It was 5000, "about a day at 1.5 trades a minute", and the live record
// (run/mule-ledger.jsonl, 3 Oct 00:04 to 5 Oct 03:35 2026) holds 7,472 entries in 51.5 hours: 2.4
// trades a minute, 3,480 a day, so 5000 reached back only 1.4 days. OutSince answers "what has the
// mule handed out this week" for MasterCrafter's storage round, which needs a week; 40,000 entries
// are about 25 MB in memory and a file of at most 80,000 lines (about 23 MB, rewritten once the file
// holds twice the kept count).
const DefaultKeep = 40000

// Item is one stack that moved. ID is the object id (a noted id when notes moved), -1 when only
// the name is known.
type Item struct {
	ID    int    `json:"id"`
	Name  string `json:"name,omitempty"`
	Count int64  `json:"count"`
}

// Entry is one finished engagement.
type Entry struct {
	// AtMs is when the trade finished (Unix milliseconds).
	AtMs    int64  `json:"at"`
	Mule    string `json:"mule"`
	Partner string `json:"partner"`
	Kind    string `json:"kind"`
	Outcome string `json:"outcome"`
	// Reason says why a failed trade failed, when the trade code said; empty otherwise.
	Reason string `json:"reason,omitempty"`
	// In is what the mule received, Out what it gave. A failed trade moved nothing.
	In  []Item `json:"in,omitempty"`
	Out []Item `json:"out,omitempty"`
}

// Query selects entries. Zero values mean "no filter"; Limit 0 means DefaultLimit.
type Query struct {
	SinceMs int64  // only entries with AtMs >= SinceMs
	Mule    string // only this mule (exact name)
	Limit   int    // at most this many, the newest
}

// DefaultLimit and MaxLimit bound a Query's result.
const (
	DefaultLimit = 200
	MaxLimit     = 5000
)

// Ledger is the bounded, persisted trade record. Safe for concurrent use; the zero value is not
// usable, call Open.
type Ledger struct {
	mu      sync.Mutex
	path    string
	keep    int
	entries []Entry // oldest first, at most keep
	onDisk  int     // lines in the file since the last rewrite
	// torn is set when the file held an unreadable line or no final newline (a crash
	// mid-append): the next Record rewrites the file rather than appending onto the torn tail.
	torn bool
	now  func() time.Time
	err  error // the last write error, for Err
}

// Open loads path (created on the first write if missing) and keeps at most keep entries
// (DefaultKeep when keep <= 0). An empty path is memory-only. A file that cannot be read is not
// an error: the ledger starts empty and the next rewrite replaces it; Err reports why.
func Open(path string, keep int) *Ledger {
	if keep <= 0 {
		keep = DefaultKeep
	}
	l := &Ledger{path: path, keep: keep, now: time.Now}
	if path == "" {
		return l
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			l.err = err
		}
		return l
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e Entry
		if json.Unmarshal(line, &e) != nil {
			l.torn = true // a torn append; the rest of the file is still good
			continue
		}
		l.entries = append(l.entries, e)
		l.onDisk++
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		l.torn = true
	}
	sort.SliceStable(l.entries, func(i, j int) bool { return l.entries[i].AtMs < l.entries[j].AtMs })
	if len(l.entries) > keep {
		l.entries = append([]Entry(nil), l.entries[len(l.entries)-keep:]...)
	}
	return l
}

// SetClock replaces the clock Record stamps entries with (tests and the fake fleet); nil is
// time.Now.
func (l *Ledger) SetClock(now func() time.Time) {
	if l == nil {
		return
	}
	if now == nil {
		now = time.Now
	}
	l.mu.Lock()
	l.now = now
	l.mu.Unlock()
}

// Record appends e, stamping AtMs with the current time when it is zero. Nil-safe.
func (l *Ledger) Record(e Entry) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if e.AtMs == 0 {
		e.AtMs = l.now().UnixMilli()
	}
	e.In = append([]Item(nil), e.In...)
	e.Out = append([]Item(nil), e.Out...)
	l.entries = append(l.entries, e)
	if over := len(l.entries) - l.keep; over > 0 {
		// Drop the oldest in place: at DefaultKeep entries a fresh copy of the kept ones on every trade
		// would be about 5 MB of garbage a trade. clear lets the dropped entries' item lists go.
		n := copy(l.entries, l.entries[over:])
		clear(l.entries[n:])
		l.entries = l.entries[:n]
	}
	if l.path == "" {
		return
	}
	if l.torn || l.onDisk+1 > 2*l.keep {
		l.rewriteLocked()
		return
	}
	line, err := json.Marshal(e)
	if err != nil {
		l.err = err
		return
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		l.err = err
		return
	}
	_, err = f.Write(append(line, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		l.err = err
		return
	}
	l.onDisk++
}

// rewriteLocked replaces the file with the kept entries.
func (l *Ledger) rewriteLocked() {
	var buf bytes.Buffer
	for _, e := range l.entries {
		line, err := json.Marshal(e)
		if err != nil {
			continue
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	tmp, err := os.CreateTemp(filepath.Dir(l.path), filepath.Base(l.path)+".*.tmp")
	if err != nil {
		l.err = err
		return
	}
	_, err = tmp.Write(buf.Bytes())
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), l.path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		l.err = err
		return
	}
	l.onDisk = len(l.entries)
	l.torn = false
}

// Entries returns the newest entries matching q, oldest first. Nil-safe.
func (l *Ledger) Entries(q Query) []Entry {
	if l == nil {
		return nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var picked []Entry
	for i := len(l.entries) - 1; i >= 0 && len(picked) < limit; i-- {
		e := l.entries[i]
		if e.AtMs < q.SinceMs || (q.Mule != "" && e.Mule != q.Mule) {
			continue
		}
		picked = append(picked, e)
	}
	out := make([]Entry, len(picked))
	for i, e := range picked {
		out[len(picked)-1-i] = e
	}
	return out
}

// OutSince totals what mule handed out in accepted hand-outs since the given time, by lower-case
// item name (every line of a kit counts, and a qualified name such as "Herb id199" is a name of its
// own), and reports when the OLDEST entry the ledger still holds for that mule was written (the zero
// time when it holds none). The second answer is what makes the first one honest: the ledger keeps
// only the newest DefaultKeep entries, so "nothing was handed out since last week" means something
// only when the record reaches back to last week. Mule names compare ignoring case; a nil ledger
// answers nothing.
//
// It is the one read the MasterCrafter storage round makes (bot.Context.MuleOut): an item the mule
// handed out lately is in use by somebody and stays where it is.
func (l *Ledger) OutSince(mule string, since time.Time) (map[string]int64, time.Time) {
	if l == nil {
		return nil, time.Time{}
	}
	want := strings.ToLower(strings.TrimSpace(mule))
	sinceMs := since.UnixMilli()
	out := map[string]int64{}
	oldest := int64(0)
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if strings.ToLower(strings.TrimSpace(e.Mule)) != want {
			continue
		}
		if oldest == 0 || e.AtMs < oldest {
			oldest = e.AtMs
		}
		if e.Kind != KindHandout || e.Outcome != OutcomeAccepted || e.AtMs < sinceMs {
			continue
		}
		for _, it := range e.Out {
			if name := strings.ToLower(strings.TrimSpace(it.Name)); name != "" && it.Count > 0 {
				out[name] += it.Count
			}
		}
	}
	if oldest == 0 {
		return out, time.Time{}
	}
	return out, time.UnixMilli(oldest)
}

// Len is how many entries are kept.
func (l *Ledger) Len() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// Err is the most recent persistence error, nil when every write so far succeeded.
func (l *Ledger) Err() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}
