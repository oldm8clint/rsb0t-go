package swarmrally

// Seats is the Queen's stand book (kqLattice, scarabplan/kqlattice.go): one numbered seat per spacing-lattice tile,
// handed out in the order the wave's roster arrives, shared by every account fighting the same Queen.
//
// WHY SHARED, AND WHY BY ORDER. The lattice is pairwise six apart, so two accounts on one seat or on seats the
// lattice does not hold both put an account inside another's ranged circle. Seats are therefore handed out by one
// book under one lock rather than computed by each account from what it can see (a view that lags and differs
// between accounts): the first Capacity members of a roster take seats 0..Capacity-1, in roster order, and the rest
// WAIT in a first-in-first-out queue. A seat frees when its holder Releases it (a death, an escape) or stops
// touching it for the stale window (a dropped connection), and the head of the queue takes the lowest free seat at
// its next Seat call. Waves overlap (the next wave is gathering while the last fights), so the queue carries
// across waves, in the order they latched.
//
// The book also holds the lattice anchor: the south-west tile of her the seats were laid against. Every account
// reads the same anchor, so a Queen that wanders one tile does not make the accounts lay slightly different
// lattices, and a larger move re-anchors all of them at once.

import (
	"sync"
	"time"
)

// SeatsStale is how long a seat holder may go without touching its seat before the seat counts as free.
const SeatsStale = 45 * time.Second

type seatHold struct {
	idx int
	at  time.Time
}

// Seats is the stand book. The zero value is not usable; use NewSeats or SeatsFor.
type Seats struct {
	now   func() time.Time
	stale time.Duration

	mu     sync.Mutex
	held   map[string]seatHold
	queue  []string
	queued map[string]bool
	gens   map[int]bool

	anchored       bool
	ax, az, alevel int
	reanchors      int
}

// NewSeats makes a stand book. now may be nil for time.Now; stale zero means SeatsStale.
func NewSeats(stale time.Duration, now func() time.Time) *Seats {
	if now == nil {
		now = time.Now
	}
	if stale <= 0 {
		stale = SeatsStale
	}
	return &Seats{now: now, stale: stale, held: map[string]seatHold{}, queued: map[string]bool{}, gens: map[int]bool{}}
}

var (
	seatsMu  sync.Mutex
	seatsReg = map[string]*Seats{}
)

// SeatsFor is the one stand book per name (one Queen per world), created on first use.
func SeatsFor(name string) *Seats {
	seatsMu.Lock()
	defer seatsMu.Unlock()
	s, ok := seatsReg[name]
	if !ok {
		s = NewSeats(0, nil)
		seatsReg[name] = s
	}
	return s
}

// Enqueue adds a wave's roster to the back of the queue, in roster order. It is idempotent per generation, so
// every member of the wave may call it. Names already holding a seat or already queued are not queued twice.
func (s *Seats) Enqueue(gen int, roster []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gens[gen] {
		return
	}
	s.gens[gen] = true
	for _, n := range roster {
		if _, h := s.held[n]; h || s.queued[n] {
			continue
		}
		s.queue = append(s.queue, n)
		s.queued[n] = true
	}
}

// Seat is name's seat index in 0..capacity-1 and true when it holds one, touching it; false while it waits (or is
// on no roster). Free seats go to the head of the queue, lowest index first, when anyone asks.
func (s *Seats) Seat(name string, capacity int) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for n, h := range s.held {
		if n != name && now.Sub(h.at) > s.stale {
			delete(s.held, n)
		}
	}
	if h, ok := s.held[name]; ok {
		if h.idx < capacity {
			s.held[name] = seatHold{idx: h.idx, at: now}
			return h.idx, true
		}
		delete(s.held, name) // the lattice shrank under it (a wall, her move): back to the front of the queue
		s.queue = append([]string{name}, s.queue...)
		s.queued[name] = true
	}
	for len(s.queue) > 0 {
		free := s.lowestFreeLocked(capacity)
		if free < 0 {
			break
		}
		head := s.queue[0]
		s.queue = s.queue[1:]
		delete(s.queued, head)
		s.held[head] = seatHold{idx: free, at: now}
	}
	if h, ok := s.held[name]; ok {
		return h.idx, true
	}
	return 0, false
}

func (s *Seats) lowestFreeLocked(capacity int) int {
	used := make(map[int]bool, len(s.held))
	for _, h := range s.held {
		used[h.idx] = true
	}
	for i := 0; i < capacity; i++ {
		if !used[i] {
			return i
		}
	}
	return -1
}

// Release gives name's seat back, or takes it out of the queue: it died, escaped or left.
func (s *Seats) Release(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.held, name)
	if s.queued[name] {
		delete(s.queued, name)
		out := s.queue[:0]
		for _, n := range s.queue {
			if n != name {
				out = append(out, n)
			}
		}
		s.queue = out
	}
}

// Holders and Waiting count the seated and the queued.
func (s *Seats) Holders() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.held) }
func (s *Seats) Waiting() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.queue) }

// Anchor is the south-west tile the lattice is laid against: the first one asked about, kept while she stays within
// slack of it (Chebyshev), replaced by the new one when she has moved further. Every account gets the same answer.
func (s *Seats) Anchor(x, z, level, slack int) (ax, az, alevel int, moved bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	far := func() bool {
		dx, dz := x-s.ax, z-s.az
		if dx < 0 {
			dx = -dx
		}
		if dz < 0 {
			dz = -dz
		}
		if dz > dx {
			dx = dz
		}
		return level != s.alevel || dx > slack
	}
	if !s.anchored || far() {
		moved = s.anchored
		if moved {
			s.reanchors++
		}
		s.anchored, s.ax, s.az, s.alevel = true, x, z, level
	}
	return s.ax, s.az, s.alevel, moved
}

// Join queues name behind everyone already waiting, unless it already holds a seat or is queued: an account that
// went in with a wave but was not on the frozen roster (it reported late) still gets a place in line.
func (s *Seats) Join(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, h := s.held[name]; h || s.queued[name] {
		return
	}
	s.queue = append(s.queue, name)
	s.queued[name] = true
}

// Placed reports whether name holds a seat or is queued for one.
func (s *Seats) Placed(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, h := s.held[name]
	return h || s.queued[name]
}

// CurrentAnchor is the anchor laid so far, and false before any account has seen her.
func (s *Seats) CurrentAnchor() (x, z, level int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ax, s.az, s.alevel, s.anchored
}
