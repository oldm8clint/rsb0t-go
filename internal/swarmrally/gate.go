package swarmrally

// The Queen gate (kqGate): the census of how many accounts are geared for the Kalphite Queen, the
// latch that releases held graduates to gear up, and the CALL that sends a hundred of them at once.
//
// THE OWNER'S RULE (13 Oct 2026): "accounts should gather and fight kq when there are 100 or more
// accounts geared, correct prayer level, 66 magic + 50 hitpoints + and 50+ defence level". The wave
// (waves.go) already holds a crossing at the burrow landing until enough are READY there; it cannot
// stop the lone lives that arrive before, between and after waves, and a wave that gives up crosses
// alone (lab record: 0 of 39 lone lives reached her). So the gate sits one trip earlier: geared
// accounts STAGE at the Shantay pass counter and nobody walks to the lair until a hundred are
// staged. Then the gate CALLS them: it names exactly the staged set at that instant (the roster),
// for a bounded time, on one world.
//
// This package is the pure coordinator, the same shape as Rally and Waves: no client, actions, fleet
// or bot imports, an explicit clock on every method so a test drives it with a fake one, and a
// process-wide registry by name because every account is a goroutine of one process. The plan
// half (what "geared" means, where the account waits) is the ScarabSwarm plan package's kqgate.go; the
// glue is internal/bot/scarabswarm_kqgate.go.
//
// WHAT THE GATE DELIBERATELY DOES NOT HAVE, each for a recorded reason:
//
//   - No open latch with a floor and a closing hysteresis. An open gate lets every later life walk in
//     alone, and "committed" never falls under a floor while accounts keep dying and re-kitting. A
//     CALL is a roster with a time limit: an account that dies re-stages and waits for the next
//     hundred. The roster also shrinks while the call is live: a member seen back before the Shantay
//     counter (died, teleported out, walked back) is dropped by Report at once, so it cannot re-kit
//     with the door still open and walk to the lair alone inside the window.
//   - No Leave on stop or hop. A report goes stale on its own (a staged account reports every pass,
//     about ten seconds, so 90 s is nine misses), and a called account is meant to hop.
//   - Nothing persisted. A process relaunch ends a call, as it ends every wave; accounts already past
//     the gate carry on and the census rebuilds as they log in.

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// GateMemberState is where a gate member stands.
type GateMemberState string

const (
	// MemberTraining: still short of a stat (or not yet through Stage 5).
	MemberTraining GateMemberState = "training"
	// MemberReady: a graduate with every stat, held (doing its held work) until the gear-up is released.
	MemberReady GateMemberState = "ready"
	// MemberGearing: in Stage 6 getting its kit (bank, food, ropes, pass, waterskin) or on its way to the counter.
	MemberGearing GateMemberState = "gearing"
	// MemberStaged: at the Shantay counter with the whole kit, waiting for the call.
	MemberStaged GateMemberState = "staged"
	// MemberCalled: past the gate, on the way to the Queen (or in her lair).
	MemberCalled GateMemberState = "called"
)

// GatePlace is where a reporting account stands relative to the gate. Only the glue knows (it reads the
// tile); the zero value says nothing, and a report with no place is judged by its State alone.
type GatePlace string

const (
	// PlaceUnknown: no tile yet (loading, hopping).
	PlaceUnknown GatePlace = ""
	// PlaceBefore: on the near side of the Shantay counter, in the west or on the respawn side. A staged
	// account is never here, so a member of a live call reporting this has died, teleported out or walked
	// back.
	PlaceBefore GatePlace = "before"
	// PlaceCounter: at the Shantay pass counter or walking out of Al Kharid to the pass.
	PlaceCounter GatePlace = "counter"
	// PlacePast: past the gate, on the desert surface, in the burrow or in the Queen's chamber.
	PlacePast GatePlace = "past"
)

// Shortfall is one stat under its floor.
type Shortfall struct {
	Stat       string
	Have, Need int
}

func (s Shortfall) String() string { return fmt.Sprintf("%s %d/%d", s.Stat, s.Have, s.Need) }

// Member is one account's latest report.
type Member struct {
	// World is the world the account is on.
	World int
	State GateMemberState
	// Graduate is Stage 5 finished (held or in Stage 6); with RequireStage5 off it is simply true.
	Graduate bool
	// Short is every stat under its floor; empty means stat-ready.
	Short []Shortfall
	// Place is where the account stands relative to the gate (see GatePlace).
	Place GatePlace
}

// StatsOK reports stat-ready.
func (m Member) StatsOK() bool { return len(m.Short) == 0 }

// GatePolicy is the gate's rule.
type GatePolicy struct {
	// Quorum is how many STAGED accounts make a call. The owner's number is 100.
	Quorum int
	// GearAhead is how many graduates beyond Quorum must exist before held graduates are released to
	// gear. 0 releases them at once.
	GearAhead int
	// CallFor is how long a call's roster may take to leave the staging point. A member that turns back
	// inside the window is dropped from the roster (Report).
	CallFor time.Duration
	// HomeHold is how long the call world stays a called account's home after the call lapses.
	HomeHold time.Duration
	// World forces the call world; 0 is the caller's preferred world (the mule's), else the world
	// with the most staged accounts.
	World int
	// StagedStale, ReadyStale and TrainingStale are how long a report of that kind still counts. A
	// staged account reports every pass (about ten seconds); a graduate held in a long thieving
	// round or an account walking from the west reports far less often.
	StagedStale, ReadyStale, TrainingStale time.Duration
}

// DefaultGatePolicy is the owner's rule with the measured cadences.
func DefaultGatePolicy() GatePolicy {
	return GatePolicy{Quorum: 100, GearAhead: 10, CallFor: 45 * time.Minute, HomeHold: 120 * time.Minute,
		StagedStale: 90 * time.Second, ReadyStale: 15 * time.Minute, TrainingStale: 10 * time.Minute}
}

// Valid reports whether the policy can work.
func (p GatePolicy) Valid() error {
	switch {
	case p.Quorum < 1:
		return fmt.Errorf("quorum %d: a gate with no quorum calls an empty roster", p.Quorum)
	case p.GearAhead < 0:
		return fmt.Errorf("gearAhead %d is negative", p.GearAhead)
	case p.CallFor <= 0:
		return fmt.Errorf("callFor %s: a call must last", p.CallFor)
	case p.HomeHold < 0:
		return fmt.Errorf("homeHold %s is negative", p.HomeHold)
	case p.World < 0:
		return fmt.Errorf("world %d is negative", p.World)
	case p.StagedStale <= 0 || p.ReadyStale <= 0 || p.TrainingStale <= 0:
		return fmt.Errorf("a stale limit is not positive (%s, %s, %s)", p.StagedStale, p.ReadyStale, p.TrainingStale)
	}
	return nil
}

func (p GatePolicy) staleFor(s GateMemberState) time.Duration {
	switch s {
	case MemberStaged:
		return p.StagedStale
	case MemberTraining:
		return p.TrainingStale
	}
	return p.ReadyStale
}

type gateRow struct {
	Member
	at time.Time
}

// GateCall is one call: a roster and the world it fights on.
type GateCall struct {
	ID    int
	World int
	At    time.Time
	Until time.Time
	// Roster is the staged set the call named when it was issued. It never shrinks: it is the call's size
	// and the closing line's denominator. Who is still named is has (Roster less back).
	Roster map[string]struct{}
	// ByWorld is the staged count per world when the call was issued.
	ByWorld map[int]int
	// left is the roster members seen past the gate (a PlacePast report) while the call was live.
	left map[string]struct{}
	// back is the roster members dropped while the call was live: seen back before the counter after
	// being called (died, teleported out, walked back). They are no longer named, so their gate is shut
	// and they stage again for the NEXT call.
	back  map[string]struct{}
	ended bool
}

// Size is the roster's size as issued.
func (c *GateCall) Size() int { return len(c.Roster) }

// has reports whether the call still names k: on the roster and not dropped from it.
func (c *GateCall) has(k string) bool {
	if _, ok := c.Roster[k]; !ok {
		return false
	}
	_, gone := c.back[k]
	return !gone
}

// CallView is one account's view of the newest call. The zero value is "no live call".
type CallView struct {
	ID    int
	World int
	// Live is a call whose time has not run out.
	Live bool
	// Member is this account being on its roster and not dropped from it.
	Member bool
	Until  time.Time
	// Size is the roster as issued; Back how many of them have been dropped since.
	Size int
	Back int
}

type homeRec struct {
	world int
	until time.Time
}

// maxGateEvents bounds the announcements waiting for a reader.
const maxGateEvents = 64

// Gate is the census and the caller. All methods are safe for concurrent use.
type Gate struct {
	mu   sync.Mutex
	p    GatePolicy
	rows map[string]*gateRow
	// gearOpen is the monotone latch: once enough graduates exist, held graduates are released for
	// the life of the process.
	gearOpen bool
	call     *GateCall
	calls    int
	homes    map[string]homeRec
	events   []string
	logAt    time.Time
	// peakStaged is the most staged accounts seen at once.
	peakStaged int
}

// NewGate opens a gate; the policy must be valid.
func NewGate(p GatePolicy) (*Gate, error) {
	if err := p.Valid(); err != nil {
		return nil, err
	}
	return &Gate{p: p, rows: map[string]*gateRow{}, homes: map[string]homeRec{}}, nil
}

// Policy is the rule in force.
func (g *Gate) Policy() GatePolicy {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.p
}

// SetPolicy replaces the rule, keeping the census, the latch and any call in flight. It reports whether
// the policy changed. A call already issued keeps the rule it was issued under.
func (g *Gate) SetPolicy(p GatePolicy) (bool, error) {
	if err := p.Valid(); err != nil {
		return false, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.p == p {
		return false, nil
	}
	g.p = p
	return true, nil
}

func gateKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// Report records an account's latest state, and returns true exactly when this report DROPPED it from the
// live call's roster.
//
// A member of the live call is followed by where it stands (Member.Place):
//
//   - PlacePast (the desert surface, the burrow, the chamber) marks it as having LEFT the gate, which the
//     call's closing line counts.
//   - PlaceBefore (the west, the respawn side), or a member that has left reporting Staged or Gearing
//     anywhere but past the gate, means it died, teleported out or walked back. It is DROPPED: Call stops
//     naming it, so its plan shuts the gate on it and it re-kits, stages again and waits for the NEXT call.
//     Its HomeWorldFor entry is kept, so it is not hopped home and back meanwhile. Without this a member
//     stayed named for the whole window after its death, re-kitted with the door still open and walked to
//     the lair alone: the lone life the gate exists to stop.
//
// A called account still walking out of Al Kharid reports PlaceCounter (State Called), and a member held at
// the counter on another world until it has hopped reports PlaceCounter (State Staged); neither has left, so
// neither is dropped.
func (g *Gate) Report(name string, m Member, now time.Time) (dropped bool) {
	k := gateKey(name)
	if k == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rows[k] = &gateRow{Member: m, at: now}
	c := g.call
	if c == nil || c.ended || !c.has(k) {
		return false
	}
	_, left := c.left[k]
	switch {
	case m.Place == PlacePast:
		c.left[k] = struct{}{}
	case m.Place == PlaceBefore, left && (m.State == MemberStaged || m.State == MemberGearing):
		c.back[k] = struct{}{}
		return true
	}
	return false
}

// census is the counts one pass over the rows gives.
type census struct {
	byState   map[GateMemberState]int
	graduates int
	staged    []string
	stagedW   map[int]int
	// almost counts accounts short of exactly one stat, by stat; short counts every shortfall's text.
	almost map[string]int
	short  map[string]*shortAgg
}

type shortAgg struct {
	n   int
	gap int
}

// census is one pass over the rows. full also builds the shortfall aggregates, which only the fleet line (Snapshot)
// reads; the per-call paths (Call, GearOpen) skip them.
func (g *Gate) census(now time.Time, full bool) census {
	c := census{byState: map[GateMemberState]int{}, stagedW: map[int]int{}, almost: map[string]int{}, short: map[string]*shortAgg{}}
	for k, r := range g.rows {
		if now.Sub(r.at) > g.p.staleFor(r.State) {
			continue
		}
		c.byState[r.State]++
		if r.Graduate && r.StatsOK() {
			c.graduates++
		}
		if r.State == MemberStaged {
			c.staged = append(c.staged, k)
			c.stagedW[r.World]++
		}
		if !full || len(r.Short) == 0 {
			continue
		}
		if len(r.Short) == 1 {
			c.almost[r.Short[0].Stat]++
		}
		for _, s := range r.Short {
			a := c.short[s.String()]
			if a == nil {
				a = &shortAgg{gap: s.Need - s.Have}
				c.short[s.String()] = a
			}
			a.n++
		}
	}
	sort.Strings(c.staged)
	return c
}

// latch lifts the gear-up release when enough graduates exist. GearAhead 0 releases at once.
func (g *Gate) latch(c census, now time.Time) {
	if g.gearOpen {
		return
	}
	if g.p.GearAhead == 0 || c.graduates >= g.p.Quorum+g.p.GearAhead {
		g.gearOpen = true
		g.event("kq gate - GEAR-UP RELEASED: %d graduate(s) (%d needed): held graduates now advance to stage 6 to kit, buy a pass and stand at the Shantay counter",
			c.graduates, g.p.Quorum+g.p.GearAhead)
	}
}

func (g *Gate) event(format string, args ...interface{}) {
	if len(g.events) >= maxGateEvents {
		g.events = g.events[1:]
	}
	g.events = append(g.events, fmt.Sprintf(format, args...))
}

// GearOpen reports the monotone latch: enough graduates exist (or GearAhead is 0), so held graduates
// may advance to Stage 6 and gear. It never unlatches in this process.
func (g *Gate) GearOpen(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.gearOpen {
		return true
	}
	g.latch(g.census(now, false), now)
	return g.gearOpen
}

// GateInput is what DecideGate reads.
type GateInput struct {
	Policy GatePolicy
	// Graduates is fresh graduates that meet every stat.
	Graduates int
	GearOpen  bool
	// Staged is the fresh staged names (any order) and StagedWorlds their count per world.
	Staged       []string
	StagedWorlds map[int]int
	// Live is a call whose time has not run out.
	Live bool
	// Prefer is the caller's preferred world (the mule's), 0 for none.
	Prefer int
}

// GateDecision is what DecideGate decided.
type GateDecision struct {
	GearOpen bool
	// NewCall is a call to issue now, with its world and the roster (sorted).
	NewCall bool
	World   int
	Roster  []string
}

// DecideGate is the gate's rule, pure: the gear-up latch, and a new call exactly when no call is live
// and a fresh quorum is staged. The roster is the staged set at this instant; the world is the forced
// one, else the preferred one, else the world holding the most staged accounts (ties to the lowest).
func DecideGate(in GateInput) GateDecision {
	d := GateDecision{GearOpen: in.GearOpen || in.Policy.GearAhead == 0 || in.Graduates >= in.Policy.Quorum+in.Policy.GearAhead}
	if in.Live || len(in.Staged) < in.Policy.Quorum {
		return d
	}
	d.NewCall = true
	d.Roster = append([]string(nil), in.Staged...)
	sort.Strings(d.Roster)
	d.World = in.Policy.World
	if d.World <= 0 {
		d.World = in.Prefer
	}
	if d.World <= 0 {
		best, bestN := 0, -1
		for w, n := range in.StagedWorlds {
			if n > bestN || (n == bestN && w < best) {
				best, bestN = w, n
			}
		}
		d.World = best
	}
	return d
}

// Call runs the gate's rule at now and returns name's view of the newest call. prefer is the world the
// caller would like the Queen fought on (the mule's, when known). It is the only way a call is issued
// and the only place one lapses, so a gate nobody asks is a gate that issues nothing.
func (g *Gate) Call(name string, now time.Time, prefer int) CallView {
	k := gateKey(name)
	g.mu.Lock()
	defer g.mu.Unlock()
	c := g.census(now, false)
	if n := len(c.staged); n > g.peakStaged {
		g.peakStaged = n
	}
	if cur := g.call; cur != nil && !cur.ended && !now.Before(cur.Until) {
		g.endCall(cur, now)
	}
	live := g.call != nil && !g.call.ended
	d := DecideGate(GateInput{Policy: g.p, Graduates: c.graduates, GearOpen: g.gearOpen, Staged: c.staged,
		StagedWorlds: c.stagedW, Live: live, Prefer: prefer})
	if d.GearOpen && !g.gearOpen {
		g.latch(c, now) // the same rule DecideGate just applied, so it opens and announces itself
	}
	if d.NewCall {
		g.issue(d, c, now)
	}
	cur := g.call
	if cur == nil || cur.ended {
		return CallView{}
	}
	return CallView{ID: cur.ID, World: cur.World, Live: true, Member: cur.has(k), Until: cur.Until, Size: len(cur.Roster), Back: len(cur.back)}
}

func (g *Gate) issue(d GateDecision, c census, now time.Time) {
	g.calls++
	call := &GateCall{ID: g.calls, World: d.World, At: now, Until: now.Add(g.p.CallFor),
		Roster: map[string]struct{}{}, ByWorld: map[int]int{}, left: map[string]struct{}{}, back: map[string]struct{}{}}
	hop := 0
	for _, k := range d.Roster {
		call.Roster[k] = struct{}{}
		if r := g.rows[k]; r != nil {
			call.ByWorld[r.World]++
			if r.World != d.World {
				hop++
			}
		}
		g.homes[k] = homeRec{world: d.World, until: call.Until.Add(g.p.HomeHold)}
	}
	for k, h := range g.homes { // bounded: one row per account, dropped once its hold is over
		if !now.Before(h.until) {
			delete(g.homes, k)
		}
	}
	g.call = call
	g.event("KQ CALL %d - %d staged (%s); the Queen is fought on world %d; roster valid %s; %d account(s) hop",
		call.ID, len(call.Roster), worldCounts(call.ByWorld), call.World, ShortDur(g.p.CallFor), hop)
}

func (g *Gate) endCall(c *GateCall, now time.Time) {
	c.ended = true
	stayed := 0
	for k := range c.Roster {
		_, left := c.left[k]
		_, back := c.back[k]
		if !left && !back {
			stayed++
		}
	}
	turned := ""
	if len(c.back) > 0 { // only said when it happened: a death inside the window is the thing worth a line
		turned = fmt.Sprintf(", %d turned back (died or teleported out) and re-stage for the next %d", len(c.back), g.p.Quorum)
	}
	g.event("kq gate - call %d over after %s (roster %d, %d left the gate%s, %d stayed behind, peak %d staged); accounts return to their home worlds %s after the call, staged accounts wait for the next %d",
		c.ID, ShortDur(now.Sub(c.At)), len(c.Roster), len(c.left), turned, stayed, g.peakStaged,
		ShortDur(g.p.HomeHold), g.p.Quorum)
}

// HomeWorldFor is the world name should call its home right now: the call world for every roster member
// until HomeHold after the call lapses (so a death and a re-stage do not hop it home and back), else 0
// for "your own home".
func (g *Gate) HomeWorldFor(name string, now time.Time) int {
	k := gateKey(name)
	g.mu.Lock()
	defer g.mu.Unlock()
	h, ok := g.homes[k]
	if !ok || !now.Before(h.until) {
		return 0
	}
	return h.world
}

// Events drains the announcements (a call issued or over, the gear-up released) so exactly one reader
// logs each.
func (g *Gate) Events() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := g.events
	g.events = nil
	return out
}

// LogDue reports true for exactly one caller per interval.
func (g *Gate) LogDue(now time.Time, every time.Duration) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.logAt.IsZero() && now.Sub(g.logAt) < every {
		return false
	}
	g.logAt = now
	return true
}

// ShortCount is one shortfall text and how many accounts have it.
type ShortCount struct {
	Text string
	N    int
}

// GateSnapshot is the census at an instant, for the fleet line.
type GateSnapshot struct {
	Quorum    int
	GearNeed  int // graduates that release the gear-up (Quorum + GearAhead)
	GearOpen  bool
	Graduates int
	Training  int
	Ready     int
	Gearing   int
	Staged    int
	Called    int
	// StagedByWorld is the staged count per world.
	StagedByWorld map[int]int
	// AlmostN accounts are short of exactly one stat, AlmostStat the commonest one.
	AlmostN    int
	AlmostStat string
	// Closest are the commonest shortfalls among the accounts nearest a floor (smallest gap first).
	Closest    []ShortCount
	Calls      int
	PeakStaged int
	Live       CallView
}

// Snapshot is the census at now.
func (g *Gate) Snapshot(now time.Time) GateSnapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	c := g.census(now, true)
	s := GateSnapshot{Quorum: g.p.Quorum, GearNeed: g.p.Quorum + g.p.GearAhead, GearOpen: g.gearOpen, Graduates: c.graduates,
		Training: c.byState[MemberTraining], Ready: c.byState[MemberReady], Gearing: c.byState[MemberGearing],
		Staged: c.byState[MemberStaged], Called: c.byState[MemberCalled], StagedByWorld: c.stagedW,
		Calls: g.calls, PeakStaged: g.peakStaged}
	for stat, n := range c.almost {
		s.AlmostN += n
		if n > c.almost[s.AlmostStat] || (n == c.almost[s.AlmostStat] && stat < s.AlmostStat) {
			s.AlmostStat = stat
		}
	}
	type kv struct {
		text string
		a    *shortAgg
	}
	var all []kv
	for t, a := range c.short {
		all = append(all, kv{t, a})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].a.gap != all[j].a.gap {
			return all[i].a.gap < all[j].a.gap
		}
		if all[i].a.n != all[j].a.n {
			return all[i].a.n > all[j].a.n
		}
		return all[i].text < all[j].text
	})
	for i := 0; i < len(all) && i < 3; i++ {
		s.Closest = append(s.Closest, ShortCount{Text: all[i].text, N: all[i].a.n})
	}
	if cur := g.call; cur != nil && !cur.ended {
		s.Live = CallView{ID: cur.ID, World: cur.World, Live: true, Until: cur.Until, Size: len(cur.Roster), Back: len(cur.back)}
	}
	return s
}

// Line is the fleet's one census line.
func (s GateSnapshot) Line() string {
	var b strings.Builder
	fmt.Fprintf(&b, "kq gate - fleet: %d of %d staged, %d gearing, %d called", s.Staged, s.Quorum, s.Gearing, s.Called)
	if s.GearOpen {
		fmt.Fprintf(&b, ", gear-up released (%d graduates)", s.Graduates)
	} else {
		fmt.Fprintf(&b, ", %d graduates of %d needed", s.Graduates, s.GearNeed)
	}
	if s.AlmostN > 0 {
		fmt.Fprintf(&b, ", %d almost (all but %s)", s.AlmostN, s.AlmostStat)
	}
	fmt.Fprintf(&b, "; staged by world %s", worldCounts(s.StagedByWorld))
	if len(s.Closest) > 0 {
		parts := make([]string, len(s.Closest))
		for i, c := range s.Closest {
			parts[i] = fmt.Sprintf("%s (x%d)", c.Text, c.N)
		}
		fmt.Fprintf(&b, "; closest shortfalls: %s", strings.Join(parts, ", "))
	}
	if s.Live.Live {
		fmt.Fprintf(&b, "; call %d live to world %d", s.Live.ID, s.Live.World)
		if s.Live.Back > 0 {
			fmt.Fprintf(&b, " (%d of %d turned back)", s.Live.Back, s.Live.Size)
		}
	}
	return b.String()
}

// ShortDur renders a duration to the minute as "45m", "2h" or "1h30m", for the gate's log lines.
func ShortDur(d time.Duration) string {
	d = d.Round(time.Minute)
	h, m := int(d/time.Hour), int(d%time.Hour/time.Minute)
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}

// worldCounts renders "w1 41, w2 33" in world order ("none" for nothing).
func worldCounts(m map[int]int) string {
	if len(m) == 0 {
		return "none"
	}
	ws := make([]int, 0, len(m))
	for w := range m {
		ws = append(ws, w)
	}
	sort.Ints(ws)
	parts := make([]string, len(ws))
	for i, w := range ws {
		parts[i] = fmt.Sprintf("w%d %d", w, m[w])
	}
	return strings.Join(parts, ", ")
}

// ---- the registry ----

// GateStatus says what SharedGate did.
type GateStatus int

const (
	// GateExisting: the gate was open and its policy is the one asked for.
	GateExisting GateStatus = iota
	// GateCreated: this call opened it.
	GateCreated
	// GatePolicyChanged: the gate was open under another policy and now has this one.
	GatePolicyChanged
)

var (
	gatesMu sync.Mutex
	gates   = map[string]*Gate{}
)

// SharedGate returns the gate called name, creating it from p the first time. UNLIKE Shared and
// SharedWaves, a later caller with a DIFFERENT policy REPLACES the rule (keeping the census, the latch
// and any call in flight) and the status says so: those two pin the first caller's policy for the life
// of the process, and a settings change only restarts scripts, not the process, so a typo'd or rehearsal
// quorum could not be corrected without a full relaunch. Gate names are a namespace of their own.
func SharedGate(name string, p GatePolicy) (*Gate, GateStatus, error) {
	if strings.TrimSpace(name) == "" {
		return nil, GateExisting, fmt.Errorf("a gate needs a name; an empty one would pool every script in the process")
	}
	gatesMu.Lock()
	defer gatesMu.Unlock()
	if g, ok := gates[name]; ok {
		changed, err := g.SetPolicy(p)
		if err != nil {
			return nil, GateExisting, err
		}
		if changed {
			return g, GatePolicyChanged, nil
		}
		return g, GateExisting, nil
	}
	g, err := NewGate(p)
	if err != nil {
		return nil, GateExisting, err
	}
	gates[name] = g
	return g, GateCreated, nil
}

// LookupGate returns an existing gate, or nil. It never creates one.
func LookupGate(name string) *Gate {
	gatesMu.Lock()
	defer gatesMu.Unlock()
	return gates[name]
}

// ForgetGate drops a gate. For tests and harnesses only.
func ForgetGate(name string) {
	gatesMu.Lock()
	defer gatesMu.Unlock()
	delete(gates, name)
}
