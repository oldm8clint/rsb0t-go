package publicingest

import (
	"strconv"
	"sync"
)

// Store is an in-memory evidence registry for the offline lab. It is bounded
// and deduplicates by experiment + strategy + seed, making it safe for replay
// ingestion without creating an unbounded dataset in a long-running process.
type Store struct {
	mu       sync.RWMutex
	capacity int
	lessons  []Lesson
	seen     map[string]struct{}
}

func NewStore(capacity int) *Store {
	if capacity < 1 {
		capacity = 1
	}
	return &Store{capacity: capacity, seen: make(map[string]struct{})}
}

func lessonKey(l Lesson) string {
	return l.ExperimentID + "\x00" + l.StrategyID + "\x00" + strconv.FormatInt(l.Seed, 10)
}

func (s *Store) Add(l Lesson) bool {
	key := lessonKey(l)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[key]; ok {
		return false
	}
	if len(s.lessons) >= s.capacity {
		delete(s.seen, lessonKey(s.lessons[0]))
		s.lessons = s.lessons[1:]
	}
	s.lessons = append(s.lessons, l)
	s.seen[key] = struct{}{}
	return true
}

func (s *Store) List() []Lesson {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Lesson, len(s.lessons))
	copy(out, s.lessons)
	return out
}
