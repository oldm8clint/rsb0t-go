package questplan

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// The quest side store: what the engine remembers about each quest (and each training rung)
// between runs, per account.
//
// AIO-PLAN Part A2 is the rule this follows: nobody adds a field to scarabplan.Checkpoint, new
// state goes in its own store beside it under data/scarabswarm-checkpoints/<acct>/, each side
// store decodes leniently (unknown fields ignored), and a failed decode is kept as
// <name>.bad-<ts> rather than silently overwritten (the runner does the file half). And Part A4:
// the server is the source of truth. Nothing here says a quest is DONE — that is the quest tab
// and varp 101, read live every time. What is here is hints, counters and timers: attempts,
// deaths, why a quest is parked and until when.

// StoreVersion is the current schema version. A reader accepts any version; the field exists
// so a later reader can migrate knowingly.
const StoreVersion = 1

// QuestRecord is one quest's (or rung's) remembered counters.
type QuestRecord struct {
	// Attempts counts engine runs started for this quest.
	Attempts int `json:"attempts,omitempty"`
	// Deaths counts deaths the engine saw while this quest was running.
	Deaths int `json:"deaths,omitempty"`
	// Parks counts how many times the quest parked.
	Parks int `json:"parks,omitempty"`
	// ParkedReason is the last parking reason, empty when not parked.
	ParkedReason string `json:"parkedReason,omitempty"`
	// BackoffUntil is when (unix ms) a parked quest may be tried again; 0 is now.
	BackoffUntil int64 `json:"backoffUntil,omitempty"`
	// LastStep is the last step the engine described, for the panel and for a human.
	LastStep string `json:"lastStep,omitempty"`
	// SeenComplete is a HINT that the quest tab once read complete. Never a gate: the ladder
	// re-reads the tab.
	SeenComplete bool `json:"seenComplete,omitempty"`
	// Completions are the completions the engine saw this quest make on this account: which build
	// finished it and when (completion.go). SeenComplete says only that the tab once read complete; this
	// is what cmd/questpromote counts for a quest's promotion evidence.
	Completions []Completion `json:"completions,omitempty"`
	// StepDeaths are the deaths (unix ms) on each capped custom step inside its window
	// (Module.StepDeathCaps), oldest first; older ones are dropped as new ones are noted.
	StepDeaths map[string][]int64 `json:"stepDeaths,omitempty"`
	// NearDeaths are the fights (unix ms) the account was losing and walked away from inside the
	// near-death window, oldest first; older ones are dropped as new ones are noted
	// (NoteNearDeath). NearDeathsToPark of them park the quest for a day.
	NearDeaths []int64 `json:"nearDeaths,omitempty"`
	// Hints are a module's own remembered facts the journal cannot show (a keg already swapped,
	// a wall slot already filled). Hints only: a module must still work, slower, without them.
	Hints map[string]bool `json:"hints,omitempty"`
	// UpdatedAt is unix ms.
	UpdatedAt int64 `json:"updatedAt,omitempty"`
}

// Store is the whole side store.
type Store struct {
	Version int                     `json:"version"`
	Quests  map[string]*QuestRecord `json:"quests"`
}

// NewStore is an empty store.
func NewStore() *Store { return &Store{Version: StoreVersion, Quests: map[string]*QuestRecord{}} }

// DecodeStore reads a store leniently: unknown fields are ignored, a missing map is made, and
// only malformed JSON is an error (which the runner answers by keeping the file aside, never by
// overwriting it with an empty store).
func DecodeStore(data []byte) (*Store, error) {
	s := NewStore()
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("questplan: decoding the quest store: %w", err)
	}
	if s.Quests == nil {
		s.Quests = map[string]*QuestRecord{}
	}
	for id, r := range s.Quests {
		if r == nil {
			s.Quests[id] = &QuestRecord{}
		}
	}
	return s, nil
}

// Encode writes the store as indented JSON with sorted keys (encoding/json sorts map keys).
func (s *Store) Encode() ([]byte, error) {
	if s.Version == 0 {
		s.Version = StoreVersion
	}
	return json.MarshalIndent(s, "", "  ")
}

// Record returns (creating) a quest's record.
func (s *Store) Record(id string) *QuestRecord {
	if s.Quests == nil {
		s.Quests = map[string]*QuestRecord{}
	}
	r := s.Quests[id]
	if r == nil {
		r = &QuestRecord{}
		s.Quests[id] = r
	}
	return r
}

// IDs lists the quests with a record, sorted.
func (s *Store) IDs() []string {
	out := make([]string, 0, len(s.Quests))
	for id := range s.Quests {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Parked reports whether a quest is inside its backoff at now, and why.
func (s *Store) Parked(id string, now time.Time) (bool, string) {
	r := s.Quests[id]
	if r == nil || r.ParkedReason == "" {
		return false, ""
	}
	if r.BackoffUntil > 0 && now.UnixMilli() >= r.BackoffUntil {
		return false, ""
	}
	return true, r.ParkedReason
}

// Backoff is how long the n-th park of a quest waits: 5, 10, 20, 40 minutes, then 2 hours.
// A quest that keeps parking is a design failure for a human to look at, but the ladder moves
// on to the next quest meanwhile rather than wedging on it.
func Backoff(parks int) time.Duration {
	switch {
	case parks <= 1:
		return 5 * time.Minute
	case parks == 2:
		return 10 * time.Minute
	case parks == 3:
		return 20 * time.Minute
	case parks == 4:
		return 40 * time.Minute
	}
	return 2 * time.Hour
}

// NotePark records a park and sets the backoff.
func (s *Store) NotePark(id, reason string, now time.Time) {
	r := s.Record(id)
	r.Parks++
	r.ParkedReason = reason
	r.BackoffUntil = now.Add(Backoff(r.Parks)).UnixMilli()
	r.UpdatedAt = now.UnixMilli()
}

// NoteTimeout records a run that ran its whole slot out. A run that moved the quest on (the
// journal stage rose, the quest was started, or it went through enough different steps) is not a
// failure: the quest waits only the shortest backoff, so the pass that ran it does not run it
// again at once, and its Parks count is not raised. A quest that needs three slots must not be
// sitting out two-hour backoffs by its fifth. A run that spun in place is parked like any other
// failure.
func (s *Store) NoteTimeout(id, reason string, now time.Time, progressed bool) {
	if !progressed {
		s.NotePark(id, reason, now)
		return
	}
	r := s.Record(id)
	r.ParkedReason = reason
	r.BackoffUntil = now.Add(Backoff(1)).UnixMilli()
	r.UpdatedAt = now.UnixMilli()
}

// ClearPark un-parks a quest (it made progress, or it completed).
func (s *Store) ClearPark(id string, now time.Time) {
	r := s.Record(id)
	r.ParkedReason = ""
	r.BackoffUntil = 0
	r.UpdatedAt = now.UnixMilli()
}

// NoteDeath counts a death against a quest.
func (s *Store) NoteDeath(id string, now time.Time) int {
	r := s.Record(id)
	r.Deaths++
	r.UpdatedAt = now.UnixMilli()
	return r.Deaths
}

// NoteStepDeath records a death on one step and reports how many deaths that step has had
// inside window up to now (this one included); older ones are forgotten.
func (s *Store) NoteStepDeath(id, step string, now time.Time, window time.Duration) int {
	r := s.Record(id)
	if r.StepDeaths == nil {
		r.StepDeaths = map[string][]int64{}
	}
	cut := now.Add(-window).UnixMilli()
	var kept []int64
	for _, at := range r.StepDeaths[step] {
		if at > cut {
			kept = append(kept, at)
		}
	}
	kept = append(kept, now.UnixMilli())
	r.StepDeaths[step] = kept
	r.UpdatedAt = now.UnixMilli()
	return len(kept)
}

// NoteNearDeath records a fight the account was losing and walked away from, and reports how many
// such retreats the quest has had inside window up to now (this one included); older ones are
// forgotten.
func (s *Store) NoteNearDeath(id string, now time.Time, window time.Duration) int {
	r := s.Record(id)
	cut := now.Add(-window).UnixMilli()
	var kept []int64
	for _, at := range r.NearDeaths {
		if at > cut {
			kept = append(kept, at)
		}
	}
	kept = append(kept, now.UnixMilli())
	r.NearDeaths = kept
	r.UpdatedAt = now.UnixMilli()
	return len(kept)
}

// NoteParkFor records a park whose backoff is skip, not the escalating Backoff: a step death cap
// (Module.StepDeathCaps) keeps the quest out for its whole skip.
func (s *Store) NoteParkFor(id, reason string, now time.Time, skip time.Duration) {
	r := s.Record(id)
	r.Parks++
	r.ParkedReason = reason
	r.BackoffUntil = now.Add(skip).UnixMilli()
	r.UpdatedAt = now.UnixMilli()
}

// NoteAttempt counts a run started.
func (s *Store) NoteAttempt(id string, now time.Time) {
	r := s.Record(id)
	r.Attempts++
	r.UpdatedAt = now.UnixMilli()
}
