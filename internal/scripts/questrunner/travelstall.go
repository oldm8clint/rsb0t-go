package questrunner

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// A quest whose walks keep ending where they cannot go on is parked, not retried for its whole slot.
//
// Live 7 October 2026: ten accounts (Spare 25 32 times, 596 log lines) ran Imp Catcher's "talk to Wizard
// Mizgog" through the Wizards' Tower west room, whose diagonal door the walker could not cross; the Guardian walked
// each one out after a few seconds, the quest tried again, and the loop never ended. Every one of those walks
// ended "stranded in the Wizards' Tower west room: the Guardian's escape ladder owns the way out, not this walk".
// The walk's own answer is the signal: a walk that ends stranded says the Guardian, not the quest, has the account,
// and a quest that keeps asking for the same walk is not going to get it.
const (
	// StrandedTravelParkRounds is how many of a quest run's walks may end "stranded" before the quest parks.
	StrandedTravelParkRounds = 3
	// SameTargetUnreachableParkRounds is how many of a quest run's walks to one tile may end unreachable (for any
	// reason) before the quest parks.
	SameTargetUnreachableParkRounds = 5
	// TravelParkFor is how long a quest parked by its walks is left alone: long enough for the Guardian to put the
	// account somewhere ordinary and for the ladder to give the slot to the other quests.
	TravelParkFor = 20 * time.Minute
)

// travelStalls counts the walks of one quest run that ended without arriving, for the park decision. The zero value
// is ready.
type travelStalls struct {
	mu       sync.Mutex
	stranded int
	targets  map[tile.Tile]int
	verdict  string
	// verdictTarget is the tile the verdict is about when it came from SameTargetUnreachableParkRounds walks to it, so
	// that a later walk to that tile which arrives can take the verdict back (arrived).
	verdictTarget    tile.Tile
	verdictIsATarget bool
}

// reset forgets a finished run's walks.
func (s *travelStalls) reset() {
	s.mu.Lock()
	s.stranded, s.targets, s.verdict = 0, nil, ""
	s.verdictTarget, s.verdictIsATarget = tile.Tile{}, false
	s.mu.Unlock()
}

// arrived records a walk that reached dest: the tile is not unreachable, whatever the walks that failed to it said, so
// they stop counting, and a verdict they had reached is taken back. Live 16 Oct 2026 an Imp Catcher account was carried to
// the mime's room, 1,500 tiles from the imp field, and its farm loop's walks to the field's tile ended unreachable five
// times in a minute; the event ended, the account walked the field for fourteen more minutes, and then the engine, which
// only reads the verdict between steps, parked a run that had three of its four beads for twenty minutes.
func (s *travelStalls) arrived(dest tile.Tile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.targets, dest)
	if s.verdictIsATarget && s.verdictTarget == dest {
		s.verdict, s.verdictTarget, s.verdictIsATarget = "", tile.Tile{}, false
	}
}

// strandedReason reports whether a webwalk reason says the Guardian owns the account ("stranded in <site>: ...").
func strandedReason(reason string) bool {
	return strings.Contains(reason, "stranded in ") || strings.Contains(reason, "escape ladder owns") || strings.Contains(reason, "escape plan owns")
}

// note records one walk that ended without arriving at dest. Only an unreachable outcome counts: a timeout or a
// stop is not the walk saying it cannot be done.
func (s *travelStalls) note(dest tile.Tile, out webwalk.Outcome, reason string) {
	if out != webwalk.OutcomeUnreachable {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.verdict != "" {
		return
	}
	if strandedReason(reason) {
		s.stranded++
		if s.stranded >= StrandedTravelParkRounds {
			s.verdict = fmt.Sprintf("%d walks in this run ended stranded (last to %d,%d,%d: %s): the Guardian owns the way out, not the quest", s.stranded, dest.X, dest.Z, dest.Level, reason)
			return
		}
	}
	if s.targets == nil {
		s.targets = map[tile.Tile]int{}
	}
	s.targets[dest]++
	if n := s.targets[dest]; n >= SameTargetUnreachableParkRounds {
		s.verdict = fmt.Sprintf("%d walks in this run to %d,%d,%d ended unreachable (%s)", n, dest.X, dest.Z, dest.Level, reason)
		s.verdictTarget, s.verdictIsATarget = dest, true
	}
}

// parkReason is the reason the quest should park now, "" while the walks are still worth another try.
func (s *travelStalls) parkReason() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.verdict
}
