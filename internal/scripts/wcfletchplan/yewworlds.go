package wcfletchplan

// Which WORLD each yew-rung account lives on.
//
// # The loss this exists for (live build b1699c90, 11 October 12:00 to 12 October 07:42, 19.7 hours)
//
// All 23 ProgressiveWCFletch accounts were on the yew rung (Woodcutting 89-91, Fletching 80-83, rune
// axes, chopManip on) and all 23 on world 1, at the eleven yews the Seers bank can reach (church 3,
// Catherby 7, road 1: yewcamps.go). What the fleet log says, by camp (a "no yew in sight" line is one
// idle pass, two seconds, the chop's own Delay):
//
//	camp (yews)            accounts   idle passes / account-hour   idle     logs / account-hour
//	Catherby yews (7)        about 15            77                  4.3%          125
//	Seers church yews (3)    about 5            232                 12.9%          143
//	Catherby road yew (1)    3                  888                 49.3%          104
//	whole cohort           23                                                    about 2,900 logs an hour
//
// The 98,198 "no yew in sight" lines and the 12,068 "woodcutter: WARNING: no yew in sight" lines of
// that window are those idle passes, and the road yew's three accounts are the "idle all evening"
// accounts: one tree, three choppers, a stump for 112 seconds after every 8 logs. But the idle PASSES are
// the visible part. An account with a tree to itself chops 5 logs per 100 ticks (the chop manipulation's
// own "chop manip: N logs in M ticks" line), 300 logs an hour of swinging, and takes 150 to 180 an hour
// once its bank trips are paid. It took 125. The other 30% went on the things a crowd costs: a tree felled
// by the account beside it mid-swing, the walk to the next standing tree, stand tiles taken.
//
// ALL OF IT IS ONE FACT: eleven trees feed about 2,950 logs an hour (a yew is a 1-in-8 deplete on a
// 200-tick stump: yewcamps.go), and 23 accounts want twice that. No arrangement of the cohort over
// ONE world's yews changes the supply. Every world has its own 11 yews, with their own stumps.
//
// # What it does about it
//
// It spreads the yew cohort over the fleet's worlds, so each world's yews feed a third of it. The
// model the yew board is priced with (CohortXP, the live cohort's own levels, rune axes):
//
//	arrangement                              experience an hour per account
//	23 accounts on world 1 (the live case)       23.6k   (the log measured 31k: the model runs 27% under)
//	12 / 11 over two worlds                      31.7k   (+35%)
//	8 / 8 / 7 over three worlds                  35.5k   (+51%)
//
// One account alone at a camp earns 42k by the same model, which is the ceiling: with a third of the
// cohort on each world no camp is over-subscribed, and the idle passes the tables above count go to
// the walk between trees alone.
//
// # How
//
// A world is only a different WebSocket; a hop is a logout and a login (internal/fleet/worldhop.go) and
// the script restarts on the far side. Nothing here performs one. The yew account is given a HOME
// world by WorldSpread.Assign and the script's existing homeWorld machinery (internal/bot/homeworld.go)
// moves the account there: the same machinery ScarabSwarm's world-2 and world-3 cohorts live on, with its
// pacing, its give-up rules and its four-hops-in-thirty-minutes bound.
//
//   - Assignment is by LOAD: the accounts already assigned to a world plus the strangers its yew board
//     has counted (campcrowd.go), the least loaded world first, an exact tie going to the world the
//     account is already on, so a cohort already split by hand is not hopped again. Sequential
//     assignments therefore deal the cohort out 8 / 8 / 7.
//   - It is STICKY. A script restarts at every hop and a hop must not undo the assignment, so the
//     assignment outlives the script in this process-wide registry and is refreshed every pass; one
//     not refreshed for WorldSpreadTTL (the account stopped) frees its place.
//   - The mule lives on world 1 and a hand-off is a trade, which needs both accounts on one world.
//     donorrunner.Run already hops to the mule's world and back; the hand-off TRIP (trips.go) stops
//     this package's own "go home" from undoing the first of those two hops the moment the script
//     restarts on the mule's world, which would hop the account away from the mule for ever.
//   - An account that loses at least WorldRebalanceIdle of a quarter hour to waiting for a yew and
//     stands on a world carrying WorldRebalanceGap more of ours than another is moved there
//     (Rebalance): the "hop to a world whose yews stand" users asked for, driven by what the
//     account measured, at most once an hour.
//
// The registry is per process: a relaunch starts empty and deals the cohort out again in login order.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// WorldSpreadTTL is how long an assignment stands without the account being seen. Every Loop
	// pass refreshes it, so it only ever lapses for an account that has stopped; a hop (a minute or
	// three of logout, login and script restart) is well inside it.
	WorldSpreadTTL = 30 * time.Minute
	// TripTTL is how long a hand-off trip may keep an account on the mule's world. A hand-off that
	// has not finished by then is abandoned and the account goes home.
	TripTTL = 25 * time.Minute
	// WorldRebalanceIdle is the fraction of a report window an account must have spent waiting for a
	// yew before it asks to move world.
	WorldRebalanceIdle = 0.15
	// WorldRebalanceGap is how many more of ours (and strangers) the account's world must carry than
	// the lightest world before a move is worth its hop: after the move the two differ by gap-2.
	WorldRebalanceGap = 2
	// WorldRebalanceEvery is the least time between two moves of one account.
	WorldRebalanceEvery = time.Hour
	// MaxStrangerLoad caps how many strangers one world's board adds to its load. A crowd of forty
	// at a world event is not forty accounts' worth of trees the cohort has to avoid for ever.
	MaxStrangerLoad = 4
)

// ParseWorlds reads a settings list of worlds, "1,2,3" (commas, semicolons or spaces), into world
// numbers in the order written, without repeats. Anything that is not a world number (1-255) is
// dropped. Nil for an empty or unreadable list.
func ParseWorlds(s string) []int {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' })
	var out []int
	seen := map[int]bool{}
	for _, f := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || n < 1 || n > 255 || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// DescribeWorlds is a list of worlds as the log says it: "1, 2, 3".
func DescribeWorlds(worlds []int) string { return joinWorlds(worlds, ", ") }

// DescribeWorldsCompact is a list of worlds as the setting says it: "1,2,3".
func DescribeWorldsCompact(worlds []int) string { return joinWorlds(worlds, ",") }

func joinWorlds(worlds []int, sep string) string {
	parts := make([]string, len(worlds))
	for i, w := range worlds {
		parts[i] = strconv.Itoa(w)
	}
	return strings.Join(parts, sep)
}

// worldSeat is one account's place in the registry.
type worldSeat struct {
	world int
	seen  time.Time
	// movedAt is when the account was last moved by Rebalance (zero: never).
	movedAt time.Time
}

// WorldSpread is the registry: which world each yew-rung account lives on. The zero value is not
// usable; build one with NewWorldSpread.
type WorldSpread struct {
	mu    sync.Mutex
	seats map[string]*worldSeat
}

// NewWorldSpread returns an empty registry.
func NewWorldSpread() *WorldSpread { return &WorldSpread{seats: map[string]*worldSeat{}} }

var sharedWorldSpread = NewWorldSpread()

// SharedWorldSpread is the process-wide registry the fleet's accounts share: one process holds the
// whole cohort, so a script restart (a hop) keeps its place in it.
func SharedWorldSpread() *WorldSpread { return sharedWorldSpread }

// WorldAssignment is what Assign decided.
type WorldAssignment struct {
	// World is the world the account lives on.
	World int
	// Fresh is true when this call placed the account (a first assignment, one that had lapsed, or
	// one for a world the list no longer holds), false when it kept a standing one.
	Fresh bool
	// Loads is each listed world's load when a NEW place was decided, this account not counted. Nil for
	// a standing place, which costs the caller nothing to keep (Loads prices it on request).
	Loads map[int]int
}

// sweepLocked drops the seats not refreshed for WorldSpreadTTL.
func (s *WorldSpread) sweepLocked(now time.Time) {
	for k, e := range s.seats {
		if now.Sub(e.seen) > WorldSpreadTTL {
			delete(s.seats, k)
		}
	}
}

func inWorlds(w int, worlds []int) bool {
	for _, x := range worlds {
		if x == w {
			return true
		}
	}
	return false
}

// loadsLocked is each listed world's load: the accounts seated on it (key excepted) plus the strangers
// its board counts, capped at MaxStrangerLoad. strangers may be nil.
func (s *WorldSpread) loadsLocked(key string, worlds []int, strangers func(world int) int) map[int]int {
	loads := make(map[int]int, len(worlds))
	for _, w := range worlds {
		loads[w] = 0
	}
	for k, e := range s.seats {
		if k == key {
			continue
		}
		if _, ok := loads[e.world]; ok {
			loads[e.world]++
		}
	}
	if strangers != nil {
		for _, w := range worlds {
			n := strangers(w)
			if n > MaxStrangerLoad {
				n = MaxStrangerLoad
			}
			if n > 0 {
				loads[w] += n
			}
		}
	}
	return loads
}

// lightest is the world with the least load: ties go to prefer when it is among them, else to the
// earliest in worlds. worlds must not be empty.
func lightest(worlds []int, loads map[int]int, prefer int) int {
	best := worlds[0]
	for _, w := range worlds[1:] {
		if loads[w] < loads[best] {
			best = w
		}
	}
	if prefer != 0 && inWorlds(prefer, worlds) && loads[prefer] == loads[best] {
		return prefer
	}
	return best
}

// Assign returns the world who lives on, placing it if it has no standing place. worlds is the list
// to spread over (nothing is assigned from an empty list: the account's current world comes back,
// not Fresh); current is the world the account is on now, which wins an exact tie; strangers, when
// not nil, says how many players that are not ours a world's yews are counted as holding. A standing
// place is kept and refreshed.
func (s *WorldSpread) Assign(who string, worlds []int, current int, strangers func(world int) int, now time.Time) WorldAssignment {
	if len(worlds) == 0 {
		return WorldAssignment{World: current}
	}
	key := normWho(who)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	if e, ok := s.seats[key]; ok && inWorlds(e.world, worlds) {
		e.seen = now
		return WorldAssignment{World: e.world}
	}
	loads := s.loadsLocked(key, worlds, strangers)
	w := lightest(worlds, loads, current)
	s.seats[key] = &worldSeat{world: w, seen: now}
	return WorldAssignment{World: w, Fresh: true, Loads: loads}
}

// Rebalance moves who to the lightest world when the account has been losing idleFraction of its
// time to waiting for a yew, its own world carries WorldRebalanceGap or more above that one, and it
// was not moved in the last WorldRebalanceEvery. It returns the new world and true when it moved.
// The account's own place is not counted in its world's load, so "carries" means the others: a world
// holding 9 of ours and one holding 7 differ by 2 for the ninth account (8 against 7 once it is
// counted out), and the move leaves 8 and 8.
func (s *WorldSpread) Rebalance(who string, worlds []int, idleFraction float64, strangers func(world int) int, now time.Time) (int, bool) {
	if len(worlds) < 2 || idleFraction < WorldRebalanceIdle {
		return 0, false
	}
	key := normWho(who)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	e, ok := s.seats[key]
	if !ok || !inWorlds(e.world, worlds) {
		return 0, false
	}
	if !e.movedAt.IsZero() && now.Sub(e.movedAt) < WorldRebalanceEvery {
		return 0, false
	}
	loads := s.loadsLocked(key, worlds, strangers)
	best := lightest(worlds, loads, 0)
	// The account stands on its own world, so its world's load already leaves it out and the world it
	// would go to would count it in: moving pays when the others on its own world exceed those on the
	// target by the gap less the one it brings with it.
	if best == e.world || loads[e.world]-loads[best] < WorldRebalanceGap-1 {
		return 0, false
	}
	e.world, e.movedAt, e.seen = best, now, now
	return best, true
}

// Release gives who's place back.
func (s *WorldSpread) Release(who string) {
	s.mu.Lock()
	delete(s.seats, normWho(who))
	s.mu.Unlock()
}

// World is the world who is assigned to and whether it has a standing place.
func (s *WorldSpread) World(who string, now time.Time) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	if e, ok := s.seats[normWho(who)]; ok {
		return e.world, true
	}
	return 0, false
}

// Loads is each listed world's load, nobody left out, for a log line.
func (s *WorldSpread) Loads(worlds []int, strangers func(world int) int, now time.Time) map[int]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	return s.loadsLocked("", worlds, strangers)
}

// DescribeLoads is loads as the log says them, in the order of worlds: "world 1: 8, world 2: 8, world 3: 7".
func DescribeLoads(worlds []int, loads map[int]int) string {
	parts := make([]string, len(worlds))
	for i, w := range worlds {
		parts[i] = fmt.Sprintf("world %d: %d", w, loads[w])
	}
	return strings.Join(parts, ", ")
}

// Assigned lists who holds a place on world, sorted, for a harness. Names are in normWho's form.
func (s *WorldSpread) Assigned(world int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k, e := range s.seats {
		if e.world == world {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
