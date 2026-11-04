package wcfletchplan

// Players the board does not know about: how many of them stand at a camp, what that does to a
// seat there, and when every camp on a world is too crowded to be worth a seat.
//
// # Why the board has to be told
//
// CampBoard (maplecamps.go) is an exact view of THIS fleet's ProgressiveWCFletch cohort and of
// nothing else. A tree does not care whose account is swinging at it: the stranger who chops the
// church yews all evening takes a share of their supply just as one more of ours would, and a
// board that prices a camp as if only its own seats were there will keep sending accounts to it.
// The a user request: "accounts progressivewcfletch should also consider how many other
// players are also chopping the same tree and if they should hop or find another location to
// chop at."
//
// # What an account can see, and what it cannot
//
// An account at a camp sees every player within about fifteen tiles (PLAYER_INFO), by name and
// tile. It cannot see what they are doing, so "chopping the same tree" is read as "standing in
// the camp": within the camp's leash of its anchor, the ground its trees are on. That includes
// the walker-by, which is why a reading is a maximum over a few minutes rather than the latest
// glance, and why nobody is believed after CrowdTTL. It excludes the fleet's own cohort, which
// the board already counts as seats: every account that has been seated on any board, or has
// started this script, is in the registry below (NoteOwn), so ProgressiveWCFletch accounts at
// the rung-1 dead trees beside the church yews are not mistaken for strangers.
//
// WHAT IS LEFT is every other player near the trees: real players, and the fleet's own accounts
// of OTHER scripts (a ProgressiveWoodcutter at Woodcutting 70 chops these same yews, and takes
// a tree's logs exactly as a stranger does), which is deliberate. The context offers no
// fleet-membership test by screen name to separate them: Context.WorldOf is an exact match on the
// roster's username, "acct163" or "Acct189" as typed there, and a screen name is that
// capitalised ("Acct163"), so a lookup by what PLAYER_INFO says misses roughly half the roster.
//
// # What a reading does
//
// A camp with s strangers at it is priced for a new seat as if s more accounts were seated there
// (CampPrice.Share(occupied + s + 1)): the same water-filling that already spreads our own
// cohort now also steps round a camp somebody else is using, for a new seat and for the camp an
// old seat might be moved TO. It never moves an account off the camp it is seated at: the
// rebalance prices that camp over our own seats alone (CampBoard.Seat), because a stranger who
// comes to chop the tree an account is working is not a reason to leave it, and the account that
// backed off a standing tree and found nothing else to chop stood idle or cut logs
// (the a user request). What a crowd of strangers DOES move is the account's world, not
// its tree: see Standing, AdviseWorld and the hop in internal/bot/wcfletch_hop.go.

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// What a reading is worth, and for how long.
const (
	// CrowdWindow is how far back the board looks for the busiest a camp has been. A stranger who
	// steps out of sight for a minute is probably coming back, so the board does not forget him the
	// moment an account looks again and sees nobody; and a lower reading never cancels a higher
	// one that is still inside the window.
	CrowdWindow = 3 * time.Minute
	// CrowdTTL is how long the last reading of a camp stands once nobody is left there to take a
	// new one. Past CrowdWindow the camp is judged by its last glance until this long after it was
	// taken, then by nothing: a crowd nobody has seen for five minutes is not a crowd the board
	// can price.
	CrowdTTL = 5 * time.Minute
	// CrowdSenseEvery is how often an account at its camp counts the players round it. Far
	// shorter than CrowdWindow, so a stranger is seen several times before the window forgets him,
	// and long enough that the count (one snapshot of the session's player list) costs the control
	// panel nothing.
	CrowdSenseEvery = 15 * time.Second
)

// crowdReading is one account's count of the strangers at a camp.
type crowdReading struct {
	at     time.Time
	others int
}

// crowdPeaks is one camp's readings as a sliding-window maximum: the peaks still inside
// CrowdWindow, oldest first and strictly falling, so the first of them is the busiest the camp has
// been in the window. A reading no bigger than a newer one can never be the busiest again and is
// dropped when the newer one arrives, which is what keeps this a handful of entries however many
// accounts are looking.
type crowdPeaks struct {
	peaks []crowdReading
	last  crowdReading
}

func (c *crowdPeaks) observe(others int, now time.Time) {
	if others < 0 {
		others = 0
	}
	c.expire(now)
	for n := len(c.peaks); n > 0 && c.peaks[n-1].others <= others; n = len(c.peaks) {
		c.peaks = c.peaks[:n-1]
	}
	c.peaks = append(c.peaks, crowdReading{at: now, others: others})
	c.last = crowdReading{at: now, others: others}
}

func (c *crowdPeaks) expire(now time.Time) {
	drop := 0
	for drop < len(c.peaks) && now.Sub(c.peaks[drop].at) > CrowdWindow {
		drop++
	}
	if drop > 0 {
		c.peaks = append(c.peaks[:0], c.peaks[drop:]...)
	}
}

// value is the strangers the board counts at the camp at now: the busiest reading inside
// CrowdWindow; failing that, the last reading while it is younger than CrowdTTL; failing that, none.
func (c *crowdPeaks) value(now time.Time) int {
	for _, p := range c.peaks {
		if now.Sub(p.at) <= CrowdWindow {
			return p.others
		}
	}
	if !c.last.at.IsZero() && now.Sub(c.last.at) <= CrowdTTL {
		return c.last.others
	}
	return 0
}

// ObserveCrowd records that an account seated at camp, standing in it, counted others players
// near its trees that this fleet's ProgressiveWCFletch cohort does not account for. An index
// outside the board is ignored, and so is a nil board.
func (b *CampBoard) ObserveCrowd(camp, others int, now time.Time) {
	if b == nil || camp < 0 || camp >= len(b.crowds) {
		return
	}
	b.mu.Lock()
	b.crowds[camp].observe(others, now)
	b.mu.Unlock()
}

// Crowd is how many strangers the board counts at camp now (see crowdPeaks.value), 0 for a camp
// outside the board.
func (b *CampBoard) Crowd(camp int, now time.Time) int {
	if b == nil || camp < 0 || camp >= len(b.crowds) {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.crowdLocked(camp, now)
}

// Crowds is Crowd for every camp.
func (b *CampBoard) Crowds(now time.Time) []int {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]int, len(b.camps))
	for i := range out {
		out[i] = b.crowdLocked(i, now)
	}
	return out
}

// ---- who is ours ----

// The cohort's own names, process-wide. Every board's Seat records the account it seats, and the
// script records itself when it starts, so an account of ours is known before it has stood at a
// camp. Names are kept in normWho's form, so the screen name PLAYER_INFO reports ("Acct 163")
// and the roster's username ("acct_163") are one entry.
var (
	ownMu    sync.RWMutex
	ownNames = map[string]struct{}{}
)

// NoteOwn records name as one of this fleet's ProgressiveWCFletch accounts.
func NoteOwn(name string) { noteOwnKey(normWho(name)) }

// noteOwnKey is NoteOwn for a name already in normWho's form, which is what a board's Seat has.
func noteOwnKey(key string) {
	if key == "" {
		return
	}
	ownMu.RLock()
	_, known := ownNames[key]
	ownMu.RUnlock()
	if known {
		return
	}
	ownMu.Lock()
	ownNames[key] = struct{}{}
	ownMu.Unlock()
}

// IsOwn reports whether name is an account NoteOwn has recorded: one of this fleet's
// ProgressiveWCFletch cohort, which a board counts as seats and not as strangers.
func IsOwn(name string) bool {
	key := normWho(name)
	if key == "" {
		return false
	}
	ownMu.RLock()
	_, ok := ownNames[key]
	ownMu.RUnlock()
	return ok
}

// ---- when every camp is too crowded ----

// CrowdedBelow is the fraction of what one account alone would earn at a camp below which a new
// seat there counts as crowded: 40%.
const CrowdedBelow = 0.4

// Standing is how a board looks to the next account that might take a seat on it.
type Standing struct {
	// Camp is the camp where a new seat earns the most, and Share is what it earns there: experience
	// an hour by the supply model, with the board's own seats and the strangers counted.
	Camp  int
	Share float64
	// Ratio is the best any camp offers a new seat as a fraction of what ONE account alone would
	// earn at that camp (CampPrice.Share(1)): 1 on a board nobody is on, falling as a camp fills.
	Ratio float64
	// Seated is the accounts holding a seat on the board and Strangers the players the camps'
	// readings put at the trees, summed over camps.
	Seated, Strangers int
}

// Crowded reports whether EVERY camp would give a new seat less than CrowdedBelow of what a lone
// account earns there: nowhere on this world is worth taking a seat.
func (s Standing) Crowded() bool { return s.Ratio < CrowdedBelow }

// Observed reports whether the board has anything to go on at all: an account seated, or a
// stranger counted. A board with neither prices every camp as empty, which on a world none of
// our accounts is on is a guess, not a finding.
func (s Standing) Observed() bool { return s.Seated > 0 || s.Strangers > 0 }

// Standing prices a new seat at every camp, as of now.
func (b *CampBoard) Standing(now time.Time) Standing {
	if b == nil || len(b.camps) == 0 {
		return Standing{Camp: -1, Ratio: 1}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked(now)
	occ := make([]int, len(b.camps))
	for _, s := range b.seats {
		if s.patch >= 0 && s.patch < len(occ) {
			occ[s.patch]++
		}
	}
	st := Standing{Camp: -1}
	priced := false
	for i := range b.camps {
		crowd := b.crowdLocked(i, now)
		st.Seated += occ[i]
		st.Strangers += crowd
		share := b.prices[i].Share(occ[i] + crowd + 1)
		if st.Camp < 0 || share > st.Share {
			st.Camp, st.Share = i, share
		}
		if alone := b.prices[i].Share(1); alone > 0 {
			priced = true
			if r := share / alone; r > st.Ratio {
				st.Ratio = r
			}
		}
	}
	if !priced {
		// Nothing here has a price (a camp with no supply for this tier): there is no way to say a
		// seat is worth less than another, so the board is not called crowded.
		st.Ratio = 1
	}
	return st
}

// crowdLocked is Crowd with the board's lock held.
func (b *CampBoard) crowdLocked(camp int, now time.Time) int {
	if camp < 0 || camp >= len(b.crowds) {
		return 0
	}
	return b.crowds[camp].value(now)
}

const (
	// CrowdAdviceAfter is how long a board must have stayed crowded before an account says so.
	CrowdAdviceAfter = 10 * time.Minute
	// CrowdAdviceEvery is how long an account then stays quiet about it.
	CrowdAdviceEvery = 30 * time.Minute
	// WorldHopGain is how much better another world's best seat must be before an account is
	// told to consider it. A hop is a logout and a login, a reset of the mule hand-offs' world
	// bookkeeping, and a few minutes at the camp not chopping, so a world that is only a little
	// better is not worth the trouble.
	WorldHopGain = 1.5
)

// AdviceWorlds are the worlds the fleet runs on, the candidates a crowded world's advice names.
var AdviceWorlds = []int{1, 2, 3}

// CrowdWatch is one account's clock on "every camp on my world is crowded": it says so once the
// board has stayed crowded for CrowdAdviceAfter, and then no more often than CrowdAdviceEvery.
type CrowdWatch struct {
	since   time.Time
	advised time.Time
}

// Step feeds the watch the board's verdict at now and reports whether to say so.
func (w *CrowdWatch) Step(crowded bool, now time.Time) bool {
	if !crowded {
		w.since = time.Time{}
		return false
	}
	if w.since.IsZero() {
		w.since = now
	}
	if now.Sub(w.since) < CrowdAdviceAfter {
		return false
	}
	if !w.advised.IsZero() && now.Sub(w.advised) < CrowdAdviceEvery {
		return false
	}
	w.advised = now
	return true
}

// WorldAdvice is a crowded world's board against the other worlds' boards.
type WorldAdvice struct {
	// Here is the world asked about and Standings every candidate's board, here included.
	Here      int
	Standings map[int]Standing
	// Worlds are the candidates in the order they were given, for a stable line.
	Worlds []int
	// Better is the world whose best seat beats this one's by WorldHopGain, or 0 when none does.
	Better int
}

// AdviseWorld compares boardFor(world) across candidates, the first best winning a tie. A world
// no account of ours is on has an empty board and prices as uncontended: WorldAdvice.Standings
// carries Observed so a caller can say that is a guess.
func AdviseWorld(here int, candidates []int, boardFor func(world int) *CampBoard, now time.Time) WorldAdvice {
	adv := WorldAdvice{Here: here, Standings: map[int]Standing{}}
	seen := map[int]bool{}
	worlds := append([]int{here}, candidates...)
	best, bestShare := 0, 0.0
	for _, w := range worlds {
		if w <= 0 || seen[w] {
			continue
		}
		seen[w] = true
		adv.Worlds = append(adv.Worlds, w)
		adv.Standings[w] = boardFor(w).Standing(now)
	}
	for _, w := range adv.Worlds {
		if w == here {
			continue
		}
		if s := adv.Standings[w].Share; s > bestShare {
			best, bestShare = w, s
		}
	}
	if best > 0 && bestShare >= WorldHopGain*adv.Standings[here].Share {
		adv.Better = best
	}
	return adv
}

// Describe is the advice as the tail of one log line, after "wcfletch: yew camps crowded on world
// N (...)": the world to consider, or that none looks clearly better, with what a new seat earns
// on each.
func (a WorldAdvice) Describe() string {
	xp := func(s Standing) string { return strconv.Itoa(int(s.Share+0.5)) + " xp/h" }
	here := a.Standings[a.Here]
	var others []string
	for _, w := range a.Worlds {
		if w == a.Here || w == a.Better {
			continue
		}
		s := a.Standings[w]
		o := "world " + strconv.Itoa(w) + " about " + xp(s)
		if !s.Observed() {
			o += " (none of ours there)"
		}
		others = append(others, o)
	}
	tail := ""
	if len(others) > 0 {
		tail = "; " + strings.Join(others, "; ")
	}
	if a.Better > 0 {
		s := a.Standings[a.Better]
		note := ""
		if !s.Observed() {
			note = " (none of ours is seated there, so who else chops there is unknown)"
		}
		return "consider homeWorld " + strconv.Itoa(a.Better) + ": a new seat there would earn about " + xp(s) +
			" against " + xp(here) + " here" + note + tail
	}
	return "no other world looks clearly better: a new seat here would earn about " + xp(here) + tail
}

// DescribeCampsWithCrowd is DescribeCamps with the strangers each camp is counted as holding:
// "church yews 9/3 +2 others, Catherby yews 14/7". A camp with strangers and no seat of ours is
// listed too, as "church yews 0/3 +4 others".
func DescribeCampsWithCrowd(camps []Camp, occ, crowd []int) string {
	type row struct {
		name     string
		n, of, s int
	}
	var rows []row
	for i, c := range camps {
		n, s := 0, 0
		if i < len(occ) {
			n = occ[i]
		}
		if i < len(crowd) {
			s = crowd[i]
		}
		if n > 0 || s > 0 {
			rows = append(rows, row{c.Name, n, c.Trees, s})
		}
	}
	if len(rows) == 0 {
		return "nobody seated"
	}
	// Most accounts first, then the most strangers, then board order.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return rows[i].s > rows[j].s
	})
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		p := r.name + " " + strconv.Itoa(r.n) + "/" + strconv.Itoa(r.of)
		if r.s > 0 {
			p += " +" + strconv.Itoa(r.s) + " others"
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, ", ")
}
