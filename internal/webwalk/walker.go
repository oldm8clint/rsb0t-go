package webwalk

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"rsb0t-go/internal/randomevents"
)

// LocQuery asks the client for live locs: map-file locs merged with the server's changes.
type LocQuery struct {
	// At is the centre; its Level is the level searched.
	At     Point
	Radius int
	// IDs matches any of these loc ids; when empty, Names matches any name, ignoring case.
	IDs   []int
	Names []string
	// Op, when set, requires the loc to offer this option.
	Op string
}

// LocMatch is one live loc.
type LocMatch struct {
	ID, X, Z, Level int
	Name            string
	// Op is the 1-based option number of LocQuery.Op, 0 when the query named none.
	Op int
}

// Client is what a Walker needs from a game session. internal/webwalk/live implements it.
type Client interface {
	// Tile is the player's tile; false before the first position update.
	Tile() (Point, bool)
	// Walk sends one movement click along corners. path[0] is the first tile the player walks to
	// (the engine's first waypoint); a path that starts on the player's own tile is sent without it
	// (actions.Walk), as the real client's are.
	Walk(path []Point, run bool) error
	// FindLocs returns matching locs, nearest first.
	FindLocs(q LocQuery) []LocMatch
	OpLoc(op, x, z, id int) error
	// CantReachSince reports whether the server said "I can't reach that!" at or after mark.
	CantReachSince(mark time.Time) bool
	// Delay waits, returning false at once when the bot is asked to stop. Delay(0) only
	// checks for a stop.
	Delay(d time.Duration) bool
	// Now is the clock Delay advances.
	Now() time.Time
}

// ModalClient is optional on a Client. When the Client implements it, a door or gate that
// answers its open click with a dialogue instead of opening is recognised, and the dialogue
// is dismissed rather than left standing.
//
// Live-found 2026-09-18 at the Barbarian Outpost's west gates (2116 at 2545,3569 and 2115 at
// 2545,3570): content's own outpost_gate.rs2 answers OPLOC1 with
// `if(%barcrawl ! ^barcrawl_complete) { if(npc_find(coord, barbguard1, 5, 0)) { @outpost_guard_talk } }`,
// and the two Barbarian guards stand on the gate tiles themselves, so for any account that has
// not completed the Alfred Grimhand Barcrawl the click opens the guard's chat dialogue and the
// gate never opens. Two things then go wrong, and this interface exists for the second: the
// hop cannot succeed no matter how often it is retried, and the dialogue it leaves up makes the
// engine refuse every LATER OPLOC too (canAccess() is false while any modal is open — the same
// mechanism already documented in randomevents/maze.go's dismissModals), so the account is
// wedged for everything that follows, not just for this gate. Confirmed with cmd/gatetest: the
// position ticker showed `2546,3569,0 STILL, modals main=-1 side=-1 chat=4882` unchanged for
// the rest of the run.
type ModalClient interface {
	// ModalOpen reports whether any main, side or chat modal is currently up.
	ModalOpen() bool
	// CloseModal dismisses whatever modal is up.
	CloseModal() error
}

// EventModalClient is optional on a ModalClient: whether the modal up now belongs to a random event's
// handler, which a stalled walk leaves alone.
type EventModalClient interface {
	ModalOwnedByEvent() bool
}

// ContinueModalClient is optional on a ModalClient: press "Click here to continue" on a chat that offers it, the
// way a paused message box or level-up is answered, and report whether it did.
type ContinueModalClient interface {
	ContinueChat() bool
}

// closeStallModal closes a modal found up when a walk stalls, and reports whether it did. Engine-TS takes no
// step while the player has a modal open and anything queued (Player.updateMovement), and a walk click
// closes nothing, so a level-up chat opened by a skilling action left the account clicking a walk that never
// moved: live 15 Oct 2026 a Mining 3 essence miner stood ten minutes, twice. Without this the second stall
// would also teach the planner a blocked step that is not blocked.
func (w *Walker) closeStallModal(me Point) bool {
	mc := w.modalClient()
	if mc == nil || !mc.ModalOpen() {
		return false
	}
	if ec, ok := mc.(EventModalClient); ok && ec.ModalOwnedByEvent() {
		return false
	}
	if cc, ok := mc.(ContinueModalClient); ok && cc.ContinueChat() {
		w.logf("stalled at %s under a paused chat (a level-up or a message box): pressed continue, the server takes no step under one", me)
		return true
	}
	if err := mc.CloseModal(); err != nil {
		w.logf("stalled at %s under an open modal; closing it: %v", me, err)
		return false
	}
	w.logf("stalled at %s under an open modal (a level-up or a dialogue left up): closed it, the server takes no step under one", me)
	return true
}

func (w *Walker) modalClient() ModalClient {
	mc, _ := w.Client.(ModalClient)
	return mc
}

// ApproachClient is optional on a Client. When the Client implements it, a loc hop whose
// stand the server cannot accept is not tried from there, and the retry after "I can't reach
// that!" walks to the tiles the server's own reach test accepts instead of guessing at the
// loc origin's four neighbours.
//
// Why, found live 2026-10-04: the Draynor Manor spooky staircase down (1731 at 3108,3364,1)
// is a 2x2 loc with forceapproach=north, so the engine (routefinder/ReachStrategy.ts
// reachRectangle, RectangleBoundary.ts reachRectangle1) accepts Walk-down only from 3108,3366
// and 3109,3366. The baked stand was 3108,3367, and the origin's neighbours are inside the loc
// or on its blocked sides, so three Holy Grail accounts were refused more than 5,000 times and
// never came down. With the engine's clientRoutefinder on, the server never paths a player to
// a loc (Player.pathToPathingTarget returns for anything that is not a PathingEntity), so the
// stand tile has to be one of these exactly.
type ApproachClient interface {
	// Approaches returns the tiles a size-1 account may op m from, the loc's own footprint
	// last, or nil when the loc's shape or type is unknown. It is internal/locreach.Tiles:
	// every tile the server accepts is in it, because locreach ignores only the source tile's
	// wall flags, which can refuse a tile but never accept one.
	Approaches(m LocMatch) []Point
}

// approaches is ApproachClient.Approaches, nil when the Client does not implement it.
func (w *Walker) approaches(m *LocMatch) []Point {
	ac, ok := w.Client.(ApproachClient)
	if !ok || m == nil {
		return nil
	}
	return ac.Approaches(*m)
}

func containsPoint(ps []Point, p Point) bool {
	for _, q := range ps {
		if q == p {
			return true
		}
	}
	return false
}

// JumpClient is optional on a Client. When the Client implements it, every hop the Walker is about to take that lands far from
// where it is operated (a spirit tree, a portal, a cave, a glider) is announced first, so the random-event Guardian reads the
// jump as the crossing it is and not as a fail-teleport or an UNEXPLAINED move.
//
// Live 14 Oct 2026 (build d5362bb2, 04:05-04:45): 151 "[guardian] displaced ... UNEXPLAINED" lines for 22 accounts, the spirit trees
// 2555,3259 <-> 2542,3169 <-> 3179,3507 and 2461,3444 ("Village spirit -> Varrock forest: continue" is the dialogue the line
// follows). The Guardian only logs such a jump, but it also counts it toward a learned strand site (twelve accounts in one
// mapsquare within 90 seconds), and the site's ladder then casts a teleport on accounts that merely arrived. The announcer is the
// walker because it is the one place that knows a hop is a hop: no coordinate list anywhere else has to be kept in step with
// the graph.
type JumpClient interface {
	// ExpectHop announces a hop named name from -> to, valid for ttl. The returned func withdraws it (the hop failed).
	ExpectHop(name string, from, to Point, ttl time.Duration) (withdraw func())
}

const (
	// hopAnnounceMin is the shortest hop (Chebyshev) announced: a hop a walk could have covered in a poll or two reads as ordinary
	// whatever the Guardian is told.
	hopAnnounceMin = 30
	// hopAnnounceTTL is how long an announced hop's landing is expected: the longest dialogue the hop handler drives is
	// forty ticks (24 s), and the landing is read within a poll after it.
	hopAnnounceTTL = 45 * time.Second
)

// hopName is the edge's name in the Guardian's log line: its special's own label when it has one.
func hopName(e *Edge) string {
	if e.Special != nil && e.Special.Label != "" {
		return "hop " + e.Special.Label
	}
	return fmt.Sprintf("hop %s %q", e.Action, e.LocName)
}

// announceHop tells the Guardian (through the Client, when it can be told) that e is about to be taken. The result withdraws the
// announcement; it is never nil.
func (w *Walker) announceHop(e *Edge) func() {
	jc, ok := w.Client.(JumpClient)
	if !ok || Chebyshev(e.From, e.To) < hopAnnounceMin {
		return func() {}
	}
	return jc.ExpectHop(hopName(e), e.From, e.To, hopAnnounceTTL)
}

// HopHandler runs one hop the Walker cannot run itself (an NPC fare, a toll dialogue), with
// the player standing at or near e.From. It returns true once the player has crossed.
type HopHandler func(e *Edge) bool

// Timings, mostly WalkExecutor.ts's and exec/doorCrossing.ts's.
const (
	tick       = 600 * time.Millisecond
	chunkSteps = 20 // TARGET_STEPS: how far ahead one scene-walk click aims
	// legClickSteps is how far along the route one walkLeg click aims: twice TARGET_STEPS. A click no
	// longer costs the tick it lands in (click sends no start tile the player stands on), but each one
	// still re-plans, and a farther target leaves walk alch more ticks between clicks to cast in
	// (scarabplan.WalkAlchAhead). The engine takes 25 corners within 127 tiles of the first
	// (MoveClickDecoder), which a 40-tile walk-only sub-plan fills only on a maze of a route; click
	// refuses one that does not fit and walkLeg aims nearer.
	legClickSteps = 40
	// MaxClickSteps is the furthest one walk click may be asked to aim (WalkOptions.ClickSteps): the engine takes the
	// click's corners as offsets from the first, which reach 127 tiles.
	MaxClickSteps   = 120
	maxClickCorners = 25
	arriveRadius    = 4  // ARRIVE_RADIUS: re-click once this close to the click target
	progressWindow  = 26 // PROGRESS_WINDOW
	deviation       = 10 // pathFollow deviationChebyshev
	stallFor        = 6 * tick
	maxRepaths      = 5 // MAX_REPATHS
	maxHopsPerWalk  = 60
	transportWait   = 8 * time.Second // TRANSPORT_WAIT_MS
	sceneRebuild    = 3 * time.Second // SCENE_REBUILD_MS
	openWait        = 4 * time.Second // OPEN_WAIT_MS
	stepWait        = 4 * time.Second // STEP_WAIT_MS
	sceneWalkFor    = 6 * time.Second // SCENE_TIMEOUT_MS
	unstickStepFor  = 3 * time.Second
	defaultTimeout  = 5 * time.Minute
	chunkBudget     = 20000
	levelChangeLag  = 2 * tick
	// shortcutBanStrikes is the refusals of a skill-gated shortcut (Edge.failureHurts) after which it is banned for
	// shortcutBanFor, whether or not it is the only way. A shortcut with a Slip is banned only when a way round it
	// exists, and only while the client can say how many hitpoints the account has (noteRefusal): the rest
	// (restForSlip) is what keeps such an account alive, so the ban need not strand it.
	shortcutBanStrikes = 4
	shortcutBanFor     = 30 * time.Minute
	doorAvoidStrike    = 2 // DOOR_AVOID_STRIKES
	doorBanStrikes     = 3 // DOOR_SESSION_STRIKES
	// doorBanFor is how long a banned barrier stays out of every plan before it may be tried
	// again. The TS WalkExecutor's ban lasts for the walker's life, and that is what stranded
	// 68 accounts inside the Fishing Guild on 2026-09-22: see noteRefusal's doc comment.
	doorBanFor = 5 * time.Minute
	// doorAttempts is how many times crossDoor operates a barrier before giving up on the hop.
	//
	// Three, not the TS two, measured on the live fleet 2026-09-22 (run/fleet.log, 269
	// accounts): the Fishing Guild entrance door (loc 2025 at 2611,3394) answered 97 crossings
	// and refused 98 in the same hour, an even coin flip that nobody has yet explained, while
	// the guild's other door (1530 at 2611,3398) crossed 103 times and refused none. At two
	// attempts a coin-flip barrier fails a whole hop a quarter of the time, which is what drove
	// the strike counter to its ban. A third attempt leaves that at one in eight and costs
	// about four seconds only on the hops that were failing anyway.
	doorAttempts = 3
	// slashAttempts is that count for a slashable web, and the difference is content's rather
	// than a tuning preference: web.rs2's [label,cut_web] cuts only on `random(2) = 1`, so two
	// swings fail outright a quarter of the time. Eight leave that at 1 in 256, which is where a
	// failed web crossing becomes evidence about the account instead of about the dice.
	slashAttempts = 8
	// teleportJump is more than running covers between two polls a tick or two apart.
	teleportJump = 6
)

// strandRetryEvery and strandRefuseWait bound walks from a catalogued strand site (strandBlocked):
// once one has failed there, another is not planned for strandRetryEvery, and each refusal waits
// strandRefuseWait so a caller's loop does not spin on it.
// unreachableMemoFor and unreachableMemoWait bound a walk that already ended unreachable: for
// unreachableMemoFor the same destination is not planned again from the same 8x8 zone, and each
// refusal waits unreachableMemoWait so a caller's retry loop does not spin. One unreachable WalkTo
// costs three normal plans and a verify probe at four times the budget, every one of them searching
// until the budget or the reachable region runs out; live on 6e041f0d (6 Oct 2026, 03:20) twenty-odd
// accounts stuck behind gates and doors retried them every few seconds and the control panel used
// 2.4 cores, against 0.7-1.3 normally.
const (
	unreachableMemoFor  = 3 * time.Minute
	unreachableMemoWait = 5 * time.Second
)

// memoKey is a destination and the 8x8 zone a walk to it started from.
type memoKey struct {
	zoneX, zoneZ, level int
	dest                Point
}

const (
	strandRetryEvery = 5 * time.Minute
	strandRefuseWait = 10 * time.Second
)

// noProgressFor is how long a leg may go without getting any further along its route before the
// walk plans again from where the account stands. The stall check only sees an account that stands
// still; one that keeps moving without getting anywhere (the click storm clickNeeded now prevents, or
// a push and pull with some other movement) changes tile every poll and so never trips it. Live 5
// October 2026: the main account lost 10 minutes at 2951,3430 on the road from the Falador west bank to the
// Taverley chest, one tile either way, with no line in the log.
const noProgressFor = 60 * time.Second

// Walker follows planned routes on a live client. One Walker belongs to one account; its
// door strikes last for its lifetime, as the TS WalkExecutor's do.
type Walker struct {
	// until is the active WalkTo's WalkOptions.Until, for the per-leg poll; aside its Aside; clickSteps how far its
	// clicks aim (WalkOptions.ClickSteps, legClickSteps when unset).
	until      func() bool
	aside      func(AsidePoll)
	clickSteps int
	// watch is the active WalkTo's Watch (WalkOptions.Watch, else Walker.Watch); noBlind its NoBlind; stepCost its
	// Search.StepCost, which the walk's own movement clicks plan with too.
	watch    func()
	noBlind  func(me Point) bool
	stepCost func(x, z, level int) int
	// Watch, when set, runs at every wait of every WalkTo this Walker makes (the ladder's rungs, the stall waits, a hop's
	// walk to its stand), not only at a straight leg's polls where Aside runs: side work the account must do whenever it
	// is on the move, which a walk that stalls and ladders for minutes would otherwise starve (eating). It must return
	// quickly and must not send movement. A WalkOptions.Watch overrides it for one walk.
	Watch   func()
	Planner *Planner
	Client  Client
	// Log receives one line per notable event; nil drops them.
	Log func(string)
	// State returns the player snapshot requires are checked against at each plan. Nil lets
	// gated edges through, which only suits offline planning.
	State func() *WorldState
	// NPCHop runs hops with no loc (ship fares, spirit trees). While nil, plans leave them out.
	NPCHop HopHandler
	// SpecialHop runs special crossings (tolls, dialogue gates). While nil, plans leave them out.
	SpecialHop HopHandler
	// Run sends movement clicks with run held.
	Run bool
	// CanHop, when set, is asked about every NPC hop and special crossing before it is
	// planned; false leaves it out. A handler that runs only some crossings sets it, so the
	// planner never routes through one the handler would refuse (a wilderness lever, a
	// use-item gate).
	CanHop func(e *Edge) bool
	// Prepare, when set, is called at the start of every WalkTo with the options its caller gave and may complete
	// them before the walk begins: side work that belongs to the account and not to any one caller (ScarabSwarm's
	// run staller: Aside and ClickSteps) reaches every walk the Walker makes this way, whichever package makes the
	// call and whether or not it knows of the work. An Aside the caller set is the caller's own and is left to it
	// (the function sees it in o and may keep out). The function Prepare returns, when it is not nil, runs once as
	// the walk returns, whatever the outcome. Nil (every Walker but ScarabSwarm's) changes nothing.
	Prepare func(o *WalkOptions) (done func())
	// BeforeFollow, when set, is asked once the walk has planned a route and before it follows it, and may move the player
	// another way (a teleport spell) in which case the walk plans again from where the player now stands. See FollowQuery
	// (beforefollow.go). Nil (every Walker whose account has no teleports to cast) changes nothing.
	BeforeFollow func(q FollowQuery) BeforeFollowResult

	walk    walkState
	learned map[[2]Point]time.Time
	// strandFail is the strand site where a walk last failed, and when (strandBlocked).
	// unreachableAt is when a walk from a zone to a destination last ended unreachable
	// (unreachableMemoFor).
	unreachableAt map[memoKey]time.Time
	strandFail    struct {
		site string
		at   time.Time
	}
	repath  int32
	strikes map[Point]int
	// onlyWay counts, per barrier placement, the refusals in a row of a crossing that is the only
	// way to the walk's destination (noteRefusal); a crossing clears it.
	onlyWay map[Point]int
	// banned is when each barrier placement was banned; see isBanned and doorBanFor.
	banned    map[Point]time.Time
	doorNames []string
	// hopDeadline is the deadline of the walk whose route is being followed, for a hop that waits (restForSlip).
	// slipped and rested are what the crossing just made left for follow: the dice rolled the slip (Edge.Slip), or
	// the walker gave up resting before one, with the reason.
	hopDeadline time.Time
	slipped     bool
	rested      string
}

// NewWalker returns a Walker over p and c.
func NewWalker(p *Planner, c Client) *Walker {
	names := map[string]bool{}
	for _, list := range p.edges {
		for _, e := range list {
			if e.Kind == KindDoor || e.Kind == KindGate {
				names[e.LocName] = true
			}
		}
	}
	if len(names) == 0 {
		names["Door"], names["Gate"], names["Large door"] = true, true, true
	}
	w := &Walker{Planner: p, Client: c, strikes: map[Point]int{}, onlyWay: map[Point]int{}, banned: map[Point]time.Time{}}
	for n := range names {
		w.doorNames = append(w.doorNames, n)
	}
	sort.Strings(w.doorNames)
	return w
}

// WalkOptions tune one WalkTo.
type WalkOptions struct {
	// Dest is the tile the walk is to: WalkTo fills it in itself before Walker.Prepare runs, so a Prepare can name the
	// walk in its log. A caller's own value is overwritten.
	Dest Point
	// Radius is how close counts as arrived (Chebyshev, same level). An unwalkable
	// destination always accepts standing beside it.
	Radius int
	// Timeout bounds the whole walk; 0 means five minutes.
	Timeout time.Duration
	// MaxPasses stops after this many passes without progress; 0 leaves it to the ladder.
	MaxPasses int
	// Search is the base search options; the Walker adds its avoids, handlers and State.
	Search Options
	// AvoidZones names KnownDangerZones to keep out of (WalkOptions.avoidZones ids);
	// AvoidRects adds ad-hoc rectangles.
	AvoidZones []string
	AvoidRects []Rect
	// NoAutomaticZones leaves out the Automatic catalog zones, which every walk otherwise
	// avoids, as the TS WalkExecutor does (the Draynor jail guards at combat 50 and below,
	// and the Wilderness). Endpoints inside a zone always exempt the zones that allow it.
	NoAutomaticZones bool
	// Hurt is a walk made by an account that is hurt and has nothing to eat (hitpoints under the caller's safe line, no food in
	// the pack): every zone that applies only up to a combat level (the bears on the Ardougne road, the bats south of Seers)
	// applies whatever the account's combat level, as the levels assume a healthy account that can eat or fight on (ZoneContext.Hurt;
	// errandsafe-1018: a combat-63 account at 18 of 55 hitpoints and no food died walking out of the Feldip ogres' pen).
	// The caller decides what hurt means; the Walker only applies it.
	Hurt bool
	// Combat is what to do when the player is attacked; see CombatOptions. It needs a Client
	// that implements CombatClient.
	Combat CombatOptions
	// Until, when set, ends the walk as arrived as soon as it returns true; it is checked every
	// poll. A caller heading for an area to find something there (a fishing spot, say) can stop
	// the moment it is in view instead of walking on to the area's fixed centre.
	Until func() bool
	// AvoidHostiles lets the walk break off from a permanently-spawned hostile NPC (a giant
	// bat, a White wolf) before it engages, rather than only reacting once it has hit. It
	// needs a Client that implements HostileClient, and is off by default so no existing
	// caller's walks change.
	AvoidHostiles bool
	// Aside, when set, is side work the walk's own poll loop makes room for (ScarabSwarm's High
	// Alchemy while walking): it is called once a poll of a straight leg, after the poll's click
	// if it made one, with where the current click is heading. It runs on the walk's goroutine,
	// so it must return quickly and must not send movement; the walk carries on regardless of
	// what it does. Nil (every existing caller) changes nothing.
	Aside func(AsidePoll)
	// ClickSteps, when above the default of 40, is how far along the route one movement click aims, up to MaxClickSteps.
	// A walk whose Aside works between the clicks (High Alchemy, a rub) wants fewer of them: every click is a tick the
	// Aside cannot cast around, and the route to the next one is the part of the walk a cast cannot be sent on. 0 leaves
	// the default, so no other caller's walk changes.
	ClickSteps int
	// NoTeleport keeps this walk on foot: Walker.BeforeFollow is not asked. For a caller whose walk is the point (a leg
	// that must pass an NPC or a door on its way) or that must not be moved off its route.
	NoTeleport bool
	// Watch overrides Walker.Watch for this walk: side work run at every wait of the walk, not only at a straight leg's
	// polls (see Walker.Watch). nil leaves the Walker's.
	Watch func()
	// NoBlind, when set, is asked before the ladder's blind rungs (the scene click straight at the destination and the
	// one-tile unstick steps) with the player's tile, and true skips the rung and waits instead. Those clicks aim at
	// the destination with no route and no regard for what stands between: from a safespot inside a monster pen they
	// are a walk into the pen. nil (every caller but a pen's walks) clicks as before.
	NoBlind func(me Point) bool
	// KeepPath leaves the last movement click's path running when the walk gives up. By default a walk that ends
	// without arriving (unreachable, failed, timed out, interrupted, budget) sends a stop click to the player's own tile:
	// the path of the last click is still live on the server, and an abandoned walk that carries on is a walk nobody is
	// watching. For the few callers that fail a walk on purpose and mean the account to keep going.
	KeepPath bool
}

// AsidePoll is what WalkOptions.Aside is told each poll.
type AsidePoll struct {
	// Me is the player's tile; Target the tile the last movement click is heading for.
	Me, Target Point
	// Path is the tiles the last movement click walks, from the tile the account stood on when it
	// was sent to Target, in the order the engine steps them (or the leg's own route tiles before the
	// first click). Not a copy: neither kept nor changed.
	Path []Point
	// SinceClick is how long ago that click was sent (0 when this poll sent it).
	SinceClick time.Duration
	// LegEnd is true when Target is the leg's last tile: a hop, a level change or the
	// destination follows it, so the walk sends its next click or op on arriving there.
	LegEnd bool
}

// Result is how a WalkTo ended.
type Result struct {
	Outcome Outcome
	Reason  string
	// Best is the closest distance reached (levels apart count as 64 extra tiles).
	Best int
	End  Point
}

func (w *Walker) untilMet() bool { return w.until != nil && w.until() }

// Arrived reports whether the walk reached the destination.
func (r Result) Arrived() bool { return r.Outcome == OutcomeArrived }

// RequestRepath makes the active walk drop its route and plan again from the live tile at its
// next poll: Traversal.requestRepath.
func (w *Walker) RequestRepath(reason string) {
	atomic.StoreInt32(&w.repath, 1)
	if reason != "" {
		w.logf("repath requested: %s", reason)
	}
}

// memoKeyFor is the memo key of a walk to dest from where the account stands now.
func (w *Walker) memoKeyFor(dest Point) (memoKey, bool) {
	here, ok := w.Client.Tile()
	if !ok {
		return memoKey{}, false
	}
	return memoKey{zoneX: here.X >> 3, zoneZ: here.Z >> 3, level: here.Level, dest: dest}, true
}

// noteUnreachable records that a walk to dest from this zone ended unreachable, and forgets
// entries older than unreachableMemoFor so the map stays small.
func (w *Walker) noteUnreachable(dest Point) {
	key, ok := w.memoKeyFor(dest)
	if !ok {
		return
	}
	now := w.Client.Now()
	if w.unreachableAt == nil {
		w.unreachableAt = map[memoKey]time.Time{}
	}
	for k, at := range w.unreachableAt {
		if now.Sub(at) >= unreachableMemoFor {
			delete(w.unreachableAt, k)
		}
	}
	w.unreachableAt[key] = now
}

// unreachableRecently reports whether a walk to dest from this zone ended unreachable less than
// unreachableMemoFor ago, and how long ago.
func (w *Walker) unreachableRecently(dest Point) (time.Duration, bool) {
	key, ok := w.memoKeyFor(dest)
	if !ok {
		return 0, false
	}
	at, found := w.unreachableAt[key]
	if !found {
		return 0, false
	}
	ago := w.Client.Now().Sub(at)
	return ago, ago < unreachableMemoFor
}

// maxCrossCredit bounds how much of a walk's deadline one ship crossing may add (crossHop): the handler's own budgets
// are well inside it.
const maxCrossCredit = 2 * time.Minute

// pastDeadline reports whether the walk's deadline has passed, not counting the time its ship crossings took
// (walkState.crossCredit). The wait for a ferry is a fixed cost the walk cannot shorten, and a walk that had used its
// budget on the road reached the pier with little left: on 16 Oct 2026 seven Port Sarim to Ardougne walks (and one
// more in the half hour after the relaunch) ran out of time in the same second their Brimhaven ferry landed, on the
// deck, and the deadline's failure was read as a walk that could not leave the deck.
func (w *Walker) pastDeadline(deadline time.Time) bool {
	return !w.Client.Now().Before(deadline.Add(w.walk.crossCredit + w.walk.lagCredit))
}

// strandBlocked reports whether the account stands in the catalogued strand site where a walk failed
// less than strandRetryEvery ago, and names it. Every walk from there plans the same long way out
// again and fails the same way: live on 9d28c8f7 (6 Oct 2026, 01:32) a mass summon left 356 accounts
// in Rogues' Castle, whose scripts asked for a walk every few seconds, and 4,245 plans of about
// 280,000 nodes each in seven minutes overloaded the owner's laptop. The Guardian's own escape walks
// with actions.Walk, not this Walker, so it is not held back.
func (w *Walker) strandBlocked() (string, bool) {
	if w.strandFail.site == "" || w.Client.Now().Sub(w.strandFail.at) >= strandRetryEvery {
		return "", false
	}
	here, ok := w.Client.Tile()
	if !ok {
		return "", false
	}
	site, owned := randomevents.StrandHoldsWalks(here.X, here.Z, here.Level)
	if !owned || site != w.strandFail.site {
		return "", false
	}
	return site, true
}

func (w *Walker) logf(format string, args ...interface{}) {
	if w.Log != nil {
		w.Log(fmt.Sprintf(format, args...))
	}
}

func (w *Walker) arrived(me, dest Point, radius int) bool {
	if me.Level != dest.Level {
		return false
	}
	if radius < 1 && !w.Planner.col.Walkable(dest.X, dest.Z, dest.Level) {
		radius = 1
	}
	return Chebyshev(me, dest) <= radius
}

func walkDistance(me, dest Point) int {
	d := Chebyshev(me, dest)
	if me.Level != dest.Level {
		d += 64
	}
	return d
}

// WalkTo walks to dest behind the escalation ladder: Traversal.walkResilient. It returns
// when the player arrives, the destination proves unreachable, the timeout passes or the
// client's Delay reports a stop. Every rung waits at least a tick, so it never spins.
//
// A walk that ends without arriving stops the account where it stands (stopMoving), unless the caller set KeepPath.
func (w *Walker) WalkTo(dest Point, o WalkOptions) Result {
	res := w.walkTo(dest, o)
	w.stopMoving(dest, res, o)
	return res
}

// walkTo is WalkTo's ladder.
func (w *Walker) walkTo(dest Point, o WalkOptions) Result {
	o.Dest = dest
	if w.Prepare != nil {
		if done := w.Prepare(&o); done != nil {
			defer done()
		}
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	deadline := w.Client.Now().Add(timeout)
	atomic.StoreInt32(&w.repath, 0)
	w.until, w.aside = o.Until, o.Aside
	w.watch, w.noBlind, w.stepCost = o.Watch, o.NoBlind, o.Search.StepCost
	if w.watch == nil {
		w.watch = w.Watch
	}
	w.clickSteps = min(max(o.ClickSteps, legClickSteps), MaxClickSteps)
	defer func() {
		w.until, w.aside, w.clickSteps = nil, nil, 0
		w.watch, w.noBlind, w.stepCost = nil, nil, nil
	}()
	w.beginWalk(dest, o)

	dist := func() (int, Point, bool) {
		me, ok := w.Client.Tile()
		if !ok {
			return 1 << 20, me, false
		}
		return walkDistance(me, dest), me, true
	}
	d0, _, _ := dist()
	state := initialLadder(d0)
	last := OutcomeNone
	unstickDir := 0
	var lastProbe *Point
	result := func(out Outcome, reason string) Result {
		_, me, _ := dist()
		if reason != "" {
			w.logf("walk to %s: %s (%s)", dest, out, reason)
		}
		return Result{Outcome: out, Reason: reason, Best: state.bestDist, End: me}
	}
	if here, ok := w.Client.Tile(); ok {
		if site, planned := randomevents.StrandPlanned(here.X, here.Z, here.Level); planned {
			if !w.pause(strandRefuseWait) {
				return result(OutcomeStopped, "stop requested")
			}
			return result(OutcomeUnreachable, "stranded in "+site+": the Guardian's escape plan owns the way out, not this walk")
		}
	}
	if ago, ok := w.unreachableRecently(dest); ok {
		if !w.pause(unreachableMemoWait) {
			return result(OutcomeStopped, "stop requested")
		}
		return result(OutcomeUnreachable, fmt.Sprintf("unreachable from here (the same walk ended unreachable %v ago; not planned again for %v)", ago.Round(time.Second), unreachableMemoFor))
	}
	if site, blocked := w.strandBlocked(); blocked {
		ago := w.Client.Now().Sub(w.strandFail.at).Round(time.Second)
		if !w.pause(strandRefuseWait) {
			return result(OutcomeStopped, "stop requested")
		}
		return result(OutcomeUnreachable, fmt.Sprintf("stranded in %s: the Guardian's escape ladder owns the way out, not this walk (a walk from here failed %v ago)", site, ago))
	}

	for iter := 0; iter < 10000; iter++ {
		if !w.pause(0) {
			return result(OutcomeStopped, "stop requested")
		}
		if w.died() {
			return result(OutcomeDied, "the player died")
		}
		// A lagging view is waited for, not walked on: a stall or a jump read from it is the connection's, not the route's.
		if _, stopped, gaveUp := w.holdForLag(); stopped {
			return result(OutcomeStopped, "stop requested")
		} else if gaveUp {
			return result(OutcomeFailed, "the view of the game stayed behind; no click was sent from it")
		}
		if w.pastDeadline(deadline) {
			return result(OutcomeFailed, fmt.Sprintf("timed out after %v", timeout))
		}
		cur, me, ok := dist()
		if w.untilMet() {
			return Result{Outcome: OutcomeArrived, Reason: "until condition met", Best: state.bestDist, End: me}
		}
		rungNow, next, ticks := advance(state, ladderObs{curDist: cur, withinRadius: ok && w.arrived(me, dest, o.Radius), lastOutcome: last})
		state = next
		if rungNow == rungArrived {
			return Result{Outcome: OutcomeArrived, Best: state.bestDist, End: me}
		}
		if o.MaxPasses > 0 && state.noProgressPasses >= o.MaxPasses {
			return result(OutcomeFailed, fmt.Sprintf("%d passes made no progress", o.MaxPasses))
		}
		started := w.Client.Now()
		if rungNow != rungBaked {
			w.logf("walk to %s: %s (best %d, pass %d)", dest, rungNow, state.bestDist, state.noProgressPasses)
		}
		switch rungNow {
		case rungBaked, rungBakedBigBudget:
			out, why := w.walkOnce(dest, o, rungNow == rungBakedBigBudget, deadline)
			switch out {
			case OutcomeStopped, OutcomeDied:
				return result(out, why)
			case OutcomeArrived:
				last = OutcomeArrived
			case OutcomeBudget, OutcomeClosest:
				last = out
			default:
				last = OutcomeFailed
			}
			if why != "" && out != OutcomeArrived {
				w.logf("walk to %s: baked walk %s: %s", dest, out, why)
			}
			// A plan that fails from a catalogued strand site is not going to be mended by the
			// ladder below: its scene walk, unstick steps and backoff took 25 to 30 seconds per
			// call and moved the account about under the Guardian's own escape ladder (which has
			// the site's stairs, door or lever and runs on the account's own action lock). Say so
			// at once and let the caller go back to waiting on the Guardian.
			if out != OutcomeArrived {
				if here, ok := w.Client.Tile(); ok {
					if site, owned := randomevents.StrandHoldsWalks(here.X, here.Z, here.Level); owned {
						w.strandFail.site, w.strandFail.at = site, w.Client.Now()
						if !w.pause(3 * tick) {
							return result(OutcomeStopped, "stop requested")
						}
						return result(OutcomeUnreachable, "stranded in "+site+": the Guardian's escape ladder owns the way out, not this walk")
					}
				}
			}
		case rungScene:
			if !w.blindRefused(dest) && !w.sceneWalk(dest, o.Radius+1) {
				return result(OutcomeStopped, "stop requested")
			}
			last = OutcomeFailed
		case rungUnstick:
			if !w.blindRefused(dest) && !w.unstick(&unstickDir) {
				return result(OutcomeStopped, "stop requested")
			}
			last = OutcomeFailed
		case rungBackoff:
			if !w.pause(time.Duration(ticks) * tick) {
				return result(OutcomeStopped, "stop requested")
			}
			last = OutcomeNone
		case rungVerify:
			_, me, _ := dist()
			opt := w.searchOptions(o.Search, nil)
			opt.MaxExpansions = 4 * DefaultMaxExpansions
			probe, _ := w.planOutOfPocket(me, dest, opt, w.Planner.Plan(me, dest, opt))
			last = judgeProbe(lastProbe, probe.OK, probe.End())
			if probe.OK {
				end := probe.End()
				lastProbe = &end
			}
			w.logf("walk to %s: verify probe %s", dest, last)
		case rungUnreachable:
			w.noteUnreachable(dest)
			return result(OutcomeUnreachable, fmt.Sprintf("unreachable from here (best %d tiles)", state.bestDist))
		case rungInterrupted:
			return result(OutcomeInterrupted, "interrupted")
		}
		if w.Client.Now().Sub(started) < tick && !w.pause(tick) {
			return result(OutcomeStopped, "stop requested")
		}
	}
	return result(OutcomeFailed, "iteration cap")
}

// searchOptions layers the Walker's avoids, handlers and state over base.
func (w *Walker) searchOptions(base Options, avoid map[Point]bool) Options {
	opt := base
	opt.Avoid = map[Point]bool{}
	for k := range base.Avoid {
		opt.Avoid[k] = true
	}
	for k := range avoid {
		opt.Avoid[k] = true
	}
	for k := range w.banned {
		if w.isBanned(k) {
			opt.Avoid[k] = true
		}
	}
	opt.AllowNPC = w.NPCHop != nil
	opt.AllowSpecial = w.SpecialHop != nil
	if zones := w.activeZones(); len(zones) > 0 {
		opt.AvoidZones = append(append([]Rect(nil), base.AvoidZones...), zones...)
	}
	if me, ok := w.Client.Tile(); ok && base.Walls == nil {
		opt.Walls = w.liveWalls(me)
	}
	if me, ok := w.Client.Tile(); ok && base.LiveEdges == nil {
		opt.LiveEdges = w.liveDoors(me)
	}
	if base.Blocked == nil {
		opt.Blocked = w.blockedSteps()
	}
	if baseAllow, canHop := base.Allow, w.CanHop; baseAllow != nil || canHop != nil {
		opt.Allow = func(e *Edge) bool {
			if baseAllow != nil && !baseAllow(e) {
				return false
			}
			if canHop != nil && (e.Special != nil || !e.HasLoc()) {
				return canHop(e)
			}
			return true
		}
	}
	if w.State != nil {
		opt.State = w.State()
	}
	return opt
}

// walkOnce plans and follows routes until it arrives or runs out of repaths: WalkExecutor.walkTo.
func (w *Walker) walkOnce(dest Point, o WalkOptions, bigBudget bool, deadline time.Time) (Outcome, string) {
	avoid := map[Point]bool{}
	repaths, hops := 0, 0
	for repaths <= maxRepaths {
		if !w.pause(0) {
			return OutcomeStopped, "stop requested"
		}
		if _, stopped, gaveUp := w.holdForLag(); stopped {
			return OutcomeStopped, "stop requested"
		} else if gaveUp {
			return OutcomeFailed, "the view of the game stayed behind"
		}
		if w.pastDeadline(deadline) {
			return OutcomeFailed, "walk timed out"
		}
		me, ok := w.Client.Tile()
		if !ok {
			repaths++
			if !w.pause(tick) {
				return OutcomeStopped, "stop requested"
			}
			continue
		}
		if w.arrived(me, dest, o.Radius) {
			return OutcomeArrived, ""
		}
		opt := w.searchOptions(o.Search, avoid)
		if bigBudget {
			opt.MaxExpansions = 4 * DefaultMaxExpansions
		}
		route, pocket := w.planOutOfPocket(me, dest, opt, w.Planner.Plan(me, dest, opt))
		w.walk.pocket = pocket
		if w.beforeFollow(me, dest, o, avoid, route, pocket) {
			continue // the player was moved: read the tile and plan again from the landing, no repath spent
		}
		if !route.OK {
			if strings.Contains(route.Reason, "budget") {
				return OutcomeBudget, route.Reason
			}
			return OutcomeFailed, "no path: " + route.Reason
		}
		w.logf("route %s -> %s: %s", me, dest, route.String())
		if route.End() == me {
			return OutcomeClosest, fmt.Sprintf("nearest reachable tile is %s", me)
		}
		out, why := w.follow(&route, dest, o.Radius, deadline, avoid, opt)
		w.walk.pocket = false
		switch out {
		case followHop:
			hops++
			if hops > maxHopsPerWalk {
				return OutcomeFailed, "too many hops"
			}
		case followRepath:
			// From a strand site each repath plans the same long way out of the Wilderness again
			// (280,000 nodes apiece live) and stalls on the same live wall: give up at once and
			// let WalkTo hand the way out back to the Guardian (strandBlocked).
			if here, ok := w.Client.Tile(); ok {
				if site, owned := randomevents.StrandHoldsWalks(here.X, here.Z, here.Level); owned {
					return OutcomeFailed, "stalled in " + site + ": " + why
				}
			}
			repaths++
			w.logf("repathing (%d/%d): %s", repaths, maxRepaths, why)
		case followSlipped:
			// A slip is the crossing working as content wrote it, not a route that failed: the retry is what the
			// walk is for, so it spends a hop (maxHopsPerWalk bounds it) and not one of the five repaths, which
			// four misses in a row at a 40% roll would otherwise use up. noteRefusal has counted the strike, but a
			// run of slips is not the give-up of onlyWayGiveUp (follow): the hitpoint rest and the deadline bound it.
			hops++
			if hops > maxHopsPerWalk {
				return OutcomeFailed, "too many hops"
			}
		case followFlee:
			// Bounded by maxFlees, so it does not spend the repath budget.
			w.logf("fleeing: %s", why)
		case followEvaded:
			// Bounded by internal/hostileevade's own per-session break-off budget rather than
			// the repath budget: a route that legitimately passes several hostiles would
			// otherwise burn its five repaths on doing the right thing each time and then give
			// up on a walk that was never actually stuck.
			w.logf("%s, planning again from here", why)
		case followDied:
			return OutcomeDied, why
		case followArrived:
			return OutcomeArrived, ""
		case followClosest:
			if pocket {
				// The route out of a pocket ends at its exit, not at dest: plan again from there
				// with the zones back on. Bounded like a repath.
				repaths++
				w.logf("out of the pocket, planning again with the danger zones")
				continue
			}
			return OutcomeClosest, why
		case followStopped:
			return OutcomeStopped, why
		default:
			return OutcomeFailed, why
		}
	}
	return OutcomeFailed, fmt.Sprintf("giving up after %d repaths", maxRepaths)
}

type followResult int

const (
	followArrived followResult = iota
	followClosest
	followHop
	followRepath
	followStopped
	followFailed
	followLegDone
	followFlee
	followDied
	// followEvaded means the client broke off from a static-spawn hostile and moved the
	// player, so the route has to be planned again from wherever it now stands.
	followEvaded
	// followSlipped means a shortcut's dice rolled the slip (Edge.Slip): the account stands on the tile the slip
	// leaves it on, so the route is planned again from there.
	followSlipped
)

// follow runs a route's legs in order. After a hop it returns so the caller plans again from
// the landing tile.
func (w *Walker) follow(r *Route, dest Point, radius int, deadline time.Time, avoid map[Point]bool, opt Options) (followResult, string) {
	for _, leg := range r.Legs() {
		if leg.Hop == nil {
			if out, why := w.walkLeg(leg.Tiles, dest, radius, deadline); out != followLegDone {
				return out, why
			}
			continue
		}
		w.hopDeadline = deadline
		// A barrier the fleet queues at may be crossed at its twin when that saves places in line (balanceLane); the rest of
		// the crossing is accounted to the barrier actually crossed.
		hop := w.balanceLane(leg.Hop)
		ok, stopped := w.cross(hop)
		if stopped {
			return followStopped, "stop requested"
		}
		if !ok {
			if why := w.rested; why != "" {
				// Not a refusal: the crossing was never tried, because the account cannot yet take a slip. Nothing is
				// struck or banned, and the walk fails for its caller (at the walk's deadline, or when its until
				// condition is met, WalkTo says so).
				w.rested = ""
				return followFailed, why
			}
			// A slip is evidence that the crossing works and the dice did not roll, not that it never will: the
			// give-up below is for the crossing that never answers (acct18, 5 Oct), and the hitpoint rest and the
			// walk's deadline already bound the slips.
			if n := w.noteRefusal(hop, avoid, dest, opt); n >= onlyWayGiveUp && !w.slipped {
				// The only way there keeps refusing: stop replanning for this walk and wait before
				// the caller's next try (onlyWayPause).
				if !w.pause(onlyWayPause(n)) {
					return followStopped, "stop requested"
				}
				return followFailed, fmt.Sprintf("the only way to %s, %s at %d,%d, refused %d crossings in a row", dest, hop.LocName, hop.LocX, hop.LocZ, n)
			}
			if w.slipped {
				w.slipped = false
				return followSlipped, "slipped: " + hop.String()
			}
			return followRepath, "hop failed: " + hop.String()
		}
		delete(w.onlyWay, Point{X: hop.LocX, Z: hop.LocZ, Level: hop.From.Level})
		if hop.failureHurts() {
			// Consecutive failures are what the shortcut ban counts (noteRefusal): a crossing that lands starts it again.
			delete(w.strikes, Point{X: hop.LocX, Z: hop.LocZ, Level: hop.From.Level})
		}
		return followHop, ""
	}
	if me, ok := w.Client.Tile(); ok && w.arrived(me, dest, radius) {
		return followArrived, ""
	}
	return followClosest, "reached the route's end short of the destination"
}

func expand(corners []Point) []Point {
	if len(corners) == 0 {
		return nil
	}
	out := []Point{corners[0]}
	for _, c := range corners[1:] {
		cur := out[len(out)-1]
		for guard := 0; cur != c && guard < 4096; guard++ {
			if cur.Level != c.Level {
				cur = c
			} else {
				cur = Point{X: cur.X + sign(c.X-cur.X), Z: cur.Z + sign(c.Z-cur.Z), Level: cur.Level}
			}
			out = append(out, cur)
		}
	}
	return out
}

func sign(n int) int {
	if n > 0 {
		return 1
	} else if n < 0 {
		return -1
	}
	return 0
}

// locate finds where me sits on the dense path: the furthest nearest tile in the progress
// window from `from`, or anywhere on the path when the window has nothing close.
func locate(tiles []Point, me Point, from int) (int, int) {
	best, bestD := -1, 1<<30
	scan := func(lo, hi int) {
		for i := lo; i <= hi && i < len(tiles); i++ {
			if tiles[i].Level != me.Level {
				continue
			}
			if d := Chebyshev(tiles[i], me); d <= bestD {
				best, bestD = i, d
			}
		}
	}
	scan(from, from+progressWindow)
	if bestD > 2 {
		scan(0, len(tiles)-1)
	}
	return best, bestD
}

// walkLeg walks one level's corners in clicks of about legClickSteps tiles, re-clicking as it
// goes and repathing on a stall, a deviation or a level change.
func (w *Walker) walkLeg(corners []Point, dest Point, radius int, deadline time.Time) (followResult, string) {
	tiles := expand(corners)
	end := tiles[len(tiles)-1]
	pathIdx, clickIdx := 0, -1
	lastTile := Point{X: -1}
	lastMove := w.Client.Now()
	lastMoveTick, _ := w.serverTicks()
	lastClick := w.Client.Now()
	stalls := 0
	// touch starts the stall's clocks over, the wall time and the game ticks it is measured in (lag.go); stalled is whether the
	// player has stood still for the stall in both.
	touch := func() {
		lastMove = w.Client.Now()
		lastMoveTick, _ = w.serverTicks()
	}
	stalled := func() bool { return w.stalledFor(w.Client.Now().Sub(lastMove), lastMoveTick) }
	// bestIdx is the furthest along the route the account has been, and bestAt when it last got
	// further (noProgressFor).
	bestIdx, bestAt := -1, w.Client.Now()
	for {
		if atomic.CompareAndSwapInt32(&w.repath, 1, 0) {
			return followRepath, "repath requested"
		}
		if w.died() {
			return followDied, "the player died"
		}
		// A lagging view is not a stall and a jump in it is not a teleport: wait for it, then start every timer of the
		// leg over and click from the tile the account really stands on (lag.go).
		if held, stopped, gaveUp := w.holdForLag(); stopped {
			return followStopped, "stop requested"
		} else if gaveUp {
			return followFailed, "the view of the game stayed behind"
		} else if held {
			now := w.Client.Now()
			lastTile, lastClick, bestAt = Point{X: -1}, now, now
			touch()
			clickIdx, stalls = -1, 0
		}
		wasRunning := w.walk.running
		if flee, why := w.combatTick(dest); flee {
			return followFlee, why
		}
		// After combatTick on purpose: a fight already in progress is CombatClient's to handle,
		// and this one is about the fight that has not started yet.
		if moved, why := w.hostileTick(); moved {
			return followEvaded, why
		}
		// A break-off that has given up must change the ROUTE, not just the tile. Checked
		// after hostileTick because that is what fills the list, and it returns followFlee so
		// the leg is planned again -- now with the hostile's square in the avoid set. Without
		// this the walk re-plans straight back through it, which is the oscillation measured
		// live on 2026-09-22: six accounts, 167 break-offs against 163 resumes, all at 0 xp.
		if blocked, why := w.hostileBlocked(dest); blocked {
			return followFlee, why
		}
		if w.walk.running && !wasRunning {
			clickIdx = -1 // click again with run held
		}
		if me, ok := w.Client.Tile(); ok {
			if w.arrived(me, dest, radius) || w.untilMet() {
				return followArrived, ""
			}
			if me == end {
				return followLegDone, ""
			}
			if me.Level != end.Level {
				return followRepath, fmt.Sprintf("level changed to %d mid-walk", me.Level)
			}
			if lastTile.X >= 0 && Chebyshev(me, lastTile) > teleportJump {
				// A teleport (a random event, a cheat, a death) can land within the deviation
				// window of the old route; clicking on toward the old path index then stalls.
				return followRepath, fmt.Sprintf("moved %d tiles in one poll to %s (teleported)", Chebyshev(me, lastTile), me)
			}
			idx, d := locate(tiles, me, pathIdx)
			if d > deviation {
				return followRepath, fmt.Sprintf("deviated %d tiles from the route at %s", d, me)
			}
			if d <= 2 && idx > pathIdx {
				pathIdx = idx
			}
			if me != lastTile {
				lastTile, stalls = me, 0
				touch()
			}
			now := w.Client.Now()
			if idx > bestIdx || w.inCombat() {
				// A fight is not a failed route (the stall check below says the same).
				bestIdx, bestAt = max(bestIdx, idx), now
			} else if now.Sub(bestAt) >= noProgressFor {
				return followRepath, fmt.Sprintf("no progress along the route for %s, at %s", noProgressFor, me)
			}
			if w.walk.combat.any() && now.Sub(lastMove) >= combatReclick && now.Sub(lastClick) >= combatReclick && w.inCombat() {
				clickIdx = -1 // a fight stopped the walk; a click cancels the retaliation
			}
			last := len(tiles) - 1
			aim := min(pathIdx+w.legClickAim(), last)
			distToTarget := 0
			if clickIdx >= 0 {
				distToTarget = Chebyshev(me, tiles[clickIdx])
			}
			if clickNeeded(clickIdx, pathIdx, aim, last, distToTarget) {
				// Past its target (or with none yet) the click aims anywhere ahead of pathIdx; merely
				// near its target it only ever aims further than the one it is already following.
				floor, following := pathIdx, clickIdx > pathIdx
				if following {
					floor = clickIdx
				}
				clicked := false
				for t := aim; t > floor && !clicked; t -= w.clickBackoff() {
					clicked = w.click(me, tiles[t])
					if clicked {
						clickIdx = t
					}
				}
				if !clicked && following {
					// Nothing further can be clicked from here: carry on along the click already sent.
				} else if !clicked && !w.click(me, end) {
					return followRepath, fmt.Sprintf("no walkable click from %s along the route", me)
				} else if !clicked {
					clickIdx = last
				}
				lastClick = w.Client.Now()
			}
			if stalled() && w.inCombat() {
				// stallPhase's combat branch: a fight is not a failed route.
				if !w.walk.warnedCombat {
					w.walk.warnedCombat = true
					w.logf("under attack at %s, holding course", me)
				}
				clickIdx, stalls = -1, 0
				touch()
			} else if stalled() && w.closeStallModal(me) {
				// Not a refused step: the server takes none under a modal, so neither a reclick nor a learned block.
				clickIdx = -1
				touch()
			} else if stalled() {
				detail := w.stallDetail(w.Client.Now().Sub(lastMove), lastMoveTick)
				stalls++
				if stalls >= stallsToLearn {
					// The plan's next step from here was refused three times, each click sent from the tile the account stands on:
					// something live (an open door's leaf, a scripted barrier) walls it off, so keep it out of the replan.
					if idx, d := locate(tiles, me, pathIdx); d == 0 && idx+1 < len(tiles) {
						if next := tiles[idx+1]; next.Level == me.Level && Chebyshev(me, next) == 1 {
							w.learnBlockedStep(me, next)
						}
					}
					return followRepath, fmt.Sprintf("stalled at %s (%s)", me, detail)
				}
				w.logf("stalled at %s, clicking again (%s without a step; %d of %d)", me, detail, stalls, stallsToLearn)
				clickIdx = -1
				touch()
			}
			if w.aside != nil && clickIdx >= 0 {
				path := w.walk.clickTiles
				if len(path) == 0 {
					path = tiles[min(pathIdx, clickIdx) : clickIdx+1]
				}
				w.aside(AsidePoll{Me: me, Target: tiles[clickIdx], Path: path,
					SinceClick: w.Client.Now().Sub(lastClick), LegEnd: clickIdx == last})
			}
		}
		if !w.pause(tick) {
			return followStopped, "stop requested"
		}
		if w.pastDeadline(deadline) {
			return followFailed, "walk timed out"
		}
	}
}

// legClickAim is how many tiles along the route the active walk's clicks aim.
func (w *Walker) legClickAim() int {
	if w.clickSteps > legClickSteps {
		return w.clickSteps
	}
	return legClickSteps
}

// clickBackoff is how many tiles nearer a click that cannot be sent (too many corners, no path) tries next: a far
// aim backs off in bigger steps, so one that does not fit costs a few searches and not thirty.
func (w *Walker) clickBackoff() int {
	if w.clickSteps > legClickSteps {
		return 10
	}
	return 4
}

// clickNeeded is whether this poll of walkLeg sends a movement click. One goes out when none has
// been sent yet (clickIdx < 0), when the account is past the tile the last one aimed at
// (clickIdx <= pathIdx), or when it has come within arriveRadius of that tile and there is a tile
// further along to aim at (aim > clickIdx). aim is the tile a click would aim at now, pathIdx plus
// chunkSteps, no further than the leg's last tile; distToTarget is the account's distance from the
// tile the last click aimed at.
//
// The last condition is what a click storm lived in. pathIdx only moves while the account is within
// two tiles of the route, but a click's own path is planned from wherever the account stands and
// cuts corners the route does not, so the account can arrive beside its target three tiles off the
// route with pathIdx left behind. The next aim is then the very tile it is already heading for, and
// the old rule clicked it again every poll. A click is not free: the engine puts the click's start
// tile first in the path (MoveClickHandler queues userPath, whose first entry is that tile), so the
// player first walks to it, and standing on it the tick's step is spent consuming it
// (PathingEntity.validateAndAdvanceStep returns -1 for it). Clicking every tick moves nobody, and a
// view a tick stale makes each click walk the account back a step first: the back and forth at
// 2951,3430 that the stall check could not see, because the tile kept changing.
func clickNeeded(clickIdx, pathIdx, aim, last, distToTarget int) bool {
	switch {
	case clickIdx < 0:
		return true
	case clickIdx >= last:
		return false // the last click already aims at the leg's last tile
	case clickIdx <= pathIdx:
		return true // the account is past its target
	}
	return distToTarget <= arriveRadius && aim > clickIdx
}

// click plans a short walk-only path to target and sends it as one movement click: the path's
// corners after the account's own tile, as the real client sends them (see actions.Walk for why a
// start on the player's tile costs a tick, or walks a stale player back). A path with more corners
// than one click carries is refused, so the caller aims nearer rather than stopping short.
func (w *Walker) click(me, target Point) bool {
	if me == target {
		return true
	}
	sub := w.Planner.Plan(me, target, Options{WalkOnly: true, MaxExpansions: chunkBudget, AvoidZones: w.clickZones(), Walls: w.liveWalls(me), Blocked: w.blockedSteps(), StepCost: w.stepCost})
	if !sub.OK || sub.End() != target {
		return false
	}
	path := make([]Point, 0, len(sub.Waypoints))
	for _, s := range sub.Waypoints {
		if s.Point != me {
			path = append(path, s.Point)
		}
	}
	if len(path) > maxClickCorners {
		return false
	}
	if err := w.sendWalk(path, w.runFlag()); err != nil {
		w.logf("walk click: %v", err)
		return false
	}
	// What the engine will step, tile by tile, for the aside's look ahead (walk alch's zone-trigger
	// gate): the click's own sub-plan, which can cut a corner the route does not.
	w.walk.clickTiles = expand(append([]Point{me}, path...))
	return true
}

// waitUntil polls cond every tick for up to d; stopped is true when the client stopped.
func (w *Walker) waitUntil(d time.Duration, cond func() bool) (ok, stopped bool) {
	end := w.Client.Now().Add(d)
	for {
		if cond() {
			return true, false
		}
		if !w.Client.Now().Before(end) {
			return false, false
		}
		if !w.pause(tick) {
			return false, true
		}
	}
}

func (w *Walker) at(p Point) bool {
	me, ok := w.Client.Tile()
	return ok && me == p
}

// walkExact walks onto one exact tile: the stand a hop is operated from. Live tests found
// stairs, ladders and gangplanks that only accept the op from one neighbour.
func (w *Walker) walkExact(p Point) (ok, stopped bool) {
	for attempt := 0; attempt < 3; attempt++ {
		me, have := w.Client.Tile()
		if have && me == p {
			return true, false
		}
		if !have || !w.click(me, p) {
			if !w.pause(tick) {
				return false, true
			}
			continue
		}
		lastMe, lastMove := me, w.Client.Now()
		for {
			if !w.pause(tick) {
				return false, true
			}
			cur, _ := w.Client.Tile()
			if cur == p {
				return true, false
			}
			if cur != lastMe {
				lastMe, lastMove = cur, w.Client.Now()
			} else if w.Client.Now().Sub(lastMove) >= 3*tick {
				break
			}
		}
	}
	return w.at(p), false
}

// cross runs one hop. It returns stopped when the client stopped mid-hop. A hop that lands far from where it is operated is
// announced to the Guardian first (JumpClient) and the announcement withdrawn when the hop fails: one that succeeded is left to
// be used up by the Guardian's next look at the account's tile, which can come after this returns.
func (w *Walker) cross(e *Edge) (ok, stopped bool) {
	w.slipped, w.rested = false, ""
	if _, stop, gaveUp := w.holdForLag(); stop {
		return false, true
	} else if gaveUp {
		w.rested = "the view of the game stayed behind" // not a refusal: nothing is struck or banned
		return false, false
	}
	withdraw := w.announceHop(e)
	ok, stopped = w.crossHop(e)
	if !ok {
		withdraw()
	}
	return ok, stopped
}

func (w *Walker) crossHop(e *Edge) (ok, stopped bool) {
	w.logf("hop %s", e.String())
	if e.Special != nil || !e.HasLoc() {
		handler := w.NPCHop
		if e.Special != nil {
			handler = w.SpecialHop
		}
		if handler == nil {
			w.logf("no handler for %s", e.String())
			return false, false
		}
		if ok, stopped := w.walkExact(e.From); stopped {
			return false, true
		} else if !ok && e.Special != nil && e.Special.ExactApproach {
			return false, false
		}
		began := w.Client.Now()
		done := handler(e)
		// The wait for a sailing, or for a turn at a queued barrier (the handler takes it, hops.locHop), is not walking.
		switch {
		case e.Kind == KindShip:
			w.walk.crossCredit += min(w.Client.Now().Sub(began), maxCrossCredit)
		case QueuedBarrier(e):
			w.walk.crossCredit += min(w.Client.Now().Sub(began), maxLaneCredit)
		}
		return done, !w.pause(0)
	}
	if w.landed(e, nil) {
		return true, false
	}
	if ok, stopped := w.walkExact(e.From); stopped {
		return false, true
	} else if !ok {
		w.logf("could not reach the stand %s for %s", e.From, e.LocName)
		return false, false
	}
	// A queued barrier takes a few accounts at a time (EnterLane); the wait for the turn is not walking.
	turn, waited, stopped := EnterLane(w.Client, e, w.Log)
	if stopped {
		return false, true
	}
	defer turn.Leave()
	w.walk.crossCredit += min(waited, maxLaneCredit)
	if (e.Kind == KindDoor || e.Kind == KindGate) && !e.ChangesLevel() && !e.LandsOnTile {
		return w.crossDoor(e)
	}
	return w.operate(e)
}

// closedBarrier finds the hop's loc still offering its action at its placement.
func (w *Walker) closedBarrier(e *Edge) *LocMatch {
	for _, m := range w.Client.FindLocs(LocQuery{At: Point{X: e.LocX, Z: e.LocZ, Level: e.From.Level}, Radius: 1, IDs: []int{e.LocID}, Op: e.Action}) {
		if m.X == e.LocX && m.Z == e.LocZ {
			mm := m
			return &mm
		}
	}
	return nil
}

// crossDoor opens a door or gate if it is shut and steps through: exec/doorCrossing.ts,
// judged by where the player ends up rather than by the leaf.
func (w *Walker) crossDoor(e *Edge) (ok, stopped bool) {
	attempts := doorAttempts
	if e.NeedsSlashTool() {
		attempts = slashAttempts
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if w.at(e.To) {
			return true, false
		}
		if !w.at(e.From) {
			if ok, stopped := w.walkExact(e.From); stopped {
				return false, true
			} else if !ok {
				return false, false
			}
		}
		if shut := w.closedBarrier(e); shut != nil {
			mark := w.Client.Now()
			if err := w.Client.OpLoc(shut.Op, shut.X, shut.Z, shut.ID); err != nil {
				w.logf("open %s: %v", e.LocName, err)
				return false, false
			}
			opened, stopped := w.waitUntil(openWait, func() bool {
				return w.closedBarrier(e) == nil || w.Client.CantReachSince(mark)
			})
			if stopped {
				return false, true
			}
			if w.Client.CantReachSince(mark) {
				w.logf("server can't reach %s at %d,%d from %s", e.LocName, e.LocX, e.LocZ, e.From)
				return false, false
			}
			if !opened {
				// A dialogue standing open where the gate should have opened is a refusal
				// this hop can never retry its way past, and leaving it up would make the
				// engine refuse every later OPLOC as well — see ModalClient's own doc
				// comment for the Barbarian Outpost gates this was found on. Dismiss it and
				// give up on the hop now, so the ordinary strike/ban machinery routes around
				// it instead of spending the whole repath budget re-planning the same hop.
				if mc := w.modalClient(); mc != nil && mc.ModalOpen() {
					if err := mc.CloseModal(); err != nil {
						w.logf("dismissing the dialogue %s at %d,%d opened: %v", e.LocName, e.LocX, e.LocZ, err)
					}
					w.logf("%s at %d,%d answered with a dialogue instead of opening; dismissed it and giving up on this crossing", e.LocName, e.LocX, e.LocZ)
					return false, false
				}
				w.logf("%s at %d,%d did not open", e.LocName, e.LocX, e.LocZ)
				continue
			}
		}
		if err := w.sendWalk([]Point{e.From, e.To}, w.runFlag()); err != nil {
			return false, false
		}
		crossed, stopped := w.waitUntil(stepWait, func() bool { return w.at(e.To) })
		if stopped {
			return false, true
		}
		if crossed {
			w.logf("crossed %s at %d,%d", e.LocName, e.LocX, e.LocZ)
			return true, false
		}
		here, _ := w.Client.Tile()
		// Where the player actually ended matters and was not in the log: this engine's doors
		// cross by teleport (open_and_close_doors.rs2), and which side they teleport you to
		// depends on check_axis reading the player's own coord, so "still at e.From" and
		// "somewhere else entirely" are different faults that used to read the same.
		w.logf("did not step through %s at %d,%d (still at %s, wanted %s)", e.LocName, e.LocX, e.LocZ, here, e.To)
	}
	return false, false
}

// landed is exec/transportLoc.ts's matchesTransportLanding, for a player who was at before.
func (w *Walker) landed(e *Edge, before *Point) bool {
	me, ok := w.Client.Tile()
	if !ok {
		return false
	}
	switch {
	case e.ChangesLevel() && !e.AcceptAnyLanding:
		return me.Level == e.To.Level && Chebyshev(me, e.To) <= 8
	case e.AcceptAnyLanding:
		return before != nil && (me.Level != before.Level || Chebyshev(me, *before) > 64)
	case e.LandsOnTile:
		if before == nil || me.Level != e.To.Level || Chebyshev(me, e.To) > 3 {
			return false
		}
		if Chebyshev(*before, e.To) <= 3 {
			return me == e.To
		}
		return Chebyshev(me, e.To) < Chebyshev(me, *before)
	}
	return before != nil && me == e.To
}

// shortcutSlipMaxHit is the most a failed hurting shortcut deals (content's horror_jumping
// slips: 1-10), plus one so the account survives the worst slip.
const shortcutSlipMaxHit = 11

// slippedBack reports whether a slipping shortcut's dice have just swept the account, who stood at before when it
// jumped, onto the tile content sweeps a miss to (Edge.Slip).
func (w *Walker) slippedBack(e *Edge, before Point) bool {
	if e.Slip == nil {
		return false
	}
	me, ok := w.Client.Tile()
	return ok && me == e.Slip.Lands && before != e.Slip.Lands
}

// watchesHitpoints reports whether the client can say how many hitpoints the account has. Keeping a rock that slips in
// the plans is safe only if the walker can see the hitpoints a slip takes, so without it noteRefusal bans the rock as
// it always did.
func (w *Walker) watchesHitpoints() bool {
	cc := w.combatClient()
	if cc == nil {
		return false
	}
	_, maxHP, ok := cc.HP()
	return ok && maxHP > 0
}

// slipRestPoll is how often a walker resting before a slipping shortcut looks at its hitpoints.
const slipRestPoll = 5 * tick

// restForSlip holds a walker standing at a shortcut that can slip (Edge.Slip) until its hitpoints can take the worst
// slip, polling every slipRestPoll. It returns true when the hitpoints are fine, or not known. When it gives up (the
// walk's deadline, the walk's until condition, a death) it returns false with w.rested set, so follow fails the walk
// without counting a refusal; false and stopped is a stop request.
//
// Why waiting and not refusing, as the guard always did: hitpoints come back at one a minute, and a rock that
// slips its jumper back to the far shore is often the only way home. Refusing counted as a strike, four strikes banned
// the rock for 30 minutes, and the account stood on the north shore of the Fremennik basalt rocks for two hours
// (basalt.go). Nothing here makes the jump any more dangerous than the guard did: it is still not made at 11
// hitpoints or under.
func (w *Walker) restForSlip(e *Edge) (ok, stopped bool) {
	cc := w.combatClient()
	if cc == nil {
		return true, false
	}
	limit := e.slipHPLimit()
	cur, maxHP, known := cc.HP()
	if !known || maxHP <= 0 || cur > limit {
		return true, false
	}
	if maxHP <= limit {
		// Hitpoints that come back to full are still not enough: waiting would only run out the walk.
		w.logf("%s at %d,%d: not jumping on %d/%d hitpoints (a slip deals up to %d)", e.LocName, e.LocX, e.LocZ, cur, maxHP, e.Slip.MaxHit)
		w.rested = fmt.Sprintf("%s at %d,%d needs more than %d hitpoints and the account has %d at most", e.LocName, e.LocX, e.LocZ, limit, maxHP)
		return false, false
	}
	w.logf("%s at %d,%d: resting at %d/%d hitpoints until more than %d before jumping (a slip deals up to %d; a hitpoint comes back a minute)", e.LocName, e.LocX, e.LocZ, cur, maxHP, limit, e.Slip.MaxHit)
	began := w.Client.Now()
	for {
		why := ""
		switch {
		case w.untilMet():
			why = "the walk's until condition is met"
		case w.died():
			why = "the player died"
		case !w.hopDeadline.IsZero() && w.pastDeadline(w.hopDeadline):
			why = "the walk ran out of time"
		}
		if why != "" {
			w.rested = fmt.Sprintf("resting at %d/%d hitpoints before %s at %d,%d, which needs more than %d (%s)", cur, maxHP, e.LocName, e.LocX, e.LocZ, limit, why)
			return false, false
		}
		if !w.pause(slipRestPoll) {
			return false, true
		}
		cur, maxHP, known = cc.HP()
		if !known || maxHP <= 0 || cur > limit {
			w.logf("%s at %d,%d: rested %v to %d/%d hitpoints, jumping", e.LocName, e.LocX, e.LocZ, w.Client.Now().Sub(began).Round(time.Second), cur, maxHP)
			return true, false
		}
	}
}

// findHopLoc finds the hop's loc by id at its placement, then by name and action nearby
// (ids drift after ship hops, as the TS fallback notes).
func (w *Walker) findHopLoc(e *Edge) *LocMatch {
	ids := []int{e.LocID}
	if e.OpenLocID >= 0 {
		ids = append(ids, e.OpenLocID)
	}
	at := Point{X: e.LocX, Z: e.LocZ, Level: e.From.Level}
	if ms := w.Client.FindLocs(LocQuery{At: at, Radius: 1, IDs: ids, Op: e.Action}); len(ms) > 0 {
		return &ms[0]
	}
	if ms := w.Client.FindLocs(LocQuery{At: at, Radius: 5, Names: []string{e.LocName}, Op: e.Action}); len(ms) > 0 {
		return &ms[0]
	}
	return nil
}

// operate runs a loc hop that is not a plain door: stairs, ladders, trapdoors, gangplanks,
// dungeon entrances and shortcuts. WalkExecutor.handleTransport's loc branch.
func (w *Walker) operate(e *Edge) (ok, stopped bool) {
	// A shortcut whose slip hurts is never tried on hitpoints one slip can take: the basalt rocks
	// deal 1-10 a slip, and mackerel died on its second try on 7 Oct 2026. One whose slip is known
	// (Edge.Slip) is waited out, for hitpoints come back and the way home must stay in the plans
	// (restForSlip); any other fails the walk instead, and the quest or trainer above it eats or parks.
	if e.failureHurts() {
		if e.Slip != nil {
			if ok, stopped := w.restForSlip(e); !ok {
				return false, stopped
			}
		} else if cc := w.combatClient(); cc != nil {
			if cur, max, ok := cc.HP(); ok && max > 0 && cur <= e.slipHPLimit() {
				w.logf("%s at %d,%d: not jumping on %d/%d hitpoints (a slip deals up to %d)", e.LocName, e.LocX, e.LocZ, cur, max, e.slipHPLimit())
				return false, false
			}
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		var loc *LocMatch
		found, stopped := w.waitUntil(sceneRebuild, func() bool {
			loc = w.findHopLoc(e)
			return loc != nil
		})
		if stopped {
			return false, true
		}
		if !found {
			if attempt == 0 && (e.ChangesLevel() || e.LandsOnTile) {
				if opened, stopped := w.openShut(e); stopped {
					return false, true
				} else if opened {
					continue
				}
			}
			w.logf("%s %q not found near %d,%d", e.Action, e.LocName, e.LocX, e.LocZ)
			return false, false
		}
		before, _ := w.Client.Tile()
		mark := w.Client.Now()
		if err := w.Client.OpLoc(loc.Op, loc.X, loc.Z, loc.ID); err != nil {
			w.logf("%s %s: %v", e.Action, e.LocName, err)
			return false, false
		}
		done, stopped := w.waitUntil(transportWait, func() bool {
			return w.landed(e, &before) || w.Client.CantReachSince(mark) || w.slippedBack(e, before)
		})
		if stopped {
			return false, true
		}
		if done && w.landed(e, &before) {
			if e.ChangesLevel() && !w.pause(levelChangeLag) {
				return false, true
			}
			w.logf("%s %s at %d,%d ok", e.Action, e.LocName, e.LocX, e.LocZ)
			return true, false
		}
		if w.slippedBack(e, before) {
			// The dice rolled the slip: the account is on the tile content sweeps it to, hurt, and a second click
			// from there would only be answered "I can't reach that!" (the rock is across the water again).
			w.slipped = true
			hp := ""
			if cc := w.combatClient(); cc != nil {
				if cur, maxHP, ok := cc.HP(); ok && maxHP > 0 {
					hp = fmt.Sprintf(", now %d/%d hitpoints", cur, maxHP)
				}
			}
			w.logf("%s at %d,%d: slipped, swept back to %s%s", e.LocName, e.LocX, e.LocZ, e.Slip.Lands, hp)
			return false, false
		}
		if w.Client.CantReachSince(mark) {
			if attempt == 0 && !w.at(e.From) {
				w.logf("server can't reach %s from %s, stepping onto the stand %s", e.LocName, before, e.From)
				if ok, stopped := w.walkExact(e.From); stopped {
					return false, true
				} else if ok {
					continue
				}
			}
			w.logf("server can't reach %s at %d,%d from %s", e.LocName, e.LocX, e.LocZ, e.From)
			// Why LandsOnTile hops now get this recovery too: this world keeps its dungeons on
			// level 0, 6400 tiles north, so a trapdoor, manhole or dungeon ladder has
			// ChangesLevel() false and LandsOnTile true, and the old guard excluded exactly the
			// population that needs it. The Asgarnian ice dungeon ladder (1759 at 3008,3150,
			// forceapproach=13, which leaves east the only legal side) answered "I can't reach
			// that!" seven times from the baked north stand with no retry at all
			// (cmd/dungeonroutetest, 2026-09-18). cmd/webwalkplan -check counts 79 graph edges
			// standing on a forceapproach-blocked side, so pinning one override per fixture
			// would not have covered the rest; this finds the working side and logs it.
			if e.ChangesLevel() || e.LandsOnTile {
				return w.otherStands(e, loc)
			}
			return false, false
		}
		w.logf("%s %s did not resolve, retrying", e.Action, e.LocName)
	}
	return false, false
}

// otherStands retries a ladder or staircase from its other orthogonal neighbours after the
// planned stand got "I can't reach that!". Live tests keep finding ladders that answer from
// one side only (the Ardougne ship ladder, both Port Sarim ladders), and the baked data picks
// a side without knowing. The tile that works is logged so it can become an ApproachOverride.
//
// When the Client is an ApproachClient, the tiles the server's reach test accepts go first.
// The origin's four neighbours alone never reach a 2x2 or 2x3 staircase from its far side:
// Burthorpe's and Draynor Manor's top stairs are only operable from the row north of the
// loc, two tiles from the origin (see ApproachClient).
func (w *Walker) otherStands(e *Edge, loc *LocMatch) (ok, stopped bool) {
	var cands []Point
	for _, p := range w.approaches(loc) {
		if p.Level == e.From.Level && !containsPoint(cands, p) {
			cands = append(cands, p)
		}
	}
	for _, side := range cardinalSides {
		if p := (Point{X: e.LocX + side.dx, Z: e.LocZ + side.dz, Level: e.From.Level}); !containsPoint(cands, p) {
			cands = append(cands, p)
		}
	}
	for _, stand := range cands {
		if stand == e.From || !w.Planner.col.Walkable(stand.X, stand.Z, stand.Level) {
			continue
		}
		me, have := w.Client.Tile()
		if !have {
			return false, false
		}
		if r := w.Planner.Plan(me, stand, Options{WalkOnly: true, MaxExpansions: chunkBudget}); !r.OK || r.End() != stand || r.Cost > 20 {
			continue
		}
		if reached, stopped := w.walkExact(stand); stopped {
			return false, true
		} else if !reached {
			continue
		}
		before, _ := w.Client.Tile()
		mark := w.Client.Now()
		if err := w.Client.OpLoc(loc.Op, loc.X, loc.Z, loc.ID); err != nil {
			return false, false
		}
		done, stopped := w.waitUntil(transportWait, func() bool {
			return w.landed(e, &before) || w.Client.CantReachSince(mark)
		})
		if stopped {
			return false, true
		}
		if done && w.landed(e, &before) {
			w.logf("%s %s at %d,%d only answered from %s, not the planned %s: worth an ApproachOverride", e.Action, e.LocName, e.LocX, e.LocZ, stand, e.From)
			if !w.pause(levelChangeLag) {
				return false, true
			}
			return true, false
		}
	}
	return false, false
}

// openShut is transportLoc.ts's openShutTrapdoor: a closed manhole or trapdoor keeps its name
// but offers Open instead of the hop's action, so open it and wait for the hop's loc to show.
func (w *Walker) openShut(e *Edge) (opened, stopped bool) {
	at := Point{X: e.LocX, Z: e.LocZ, Level: e.From.Level}
	shut := w.Client.FindLocs(LocQuery{At: at, Radius: 3, Names: []string{e.LocName}, Op: "Open"})
	if len(shut) == 0 {
		return false, false
	}
	m := shut[0]
	if err := w.Client.OpLoc(m.Op, m.X, m.Z, m.ID); err != nil {
		w.logf("open the shut %s: %v", e.LocName, err)
		return false, false
	}
	w.logf("opened the shut %s at %d,%d before %s", e.LocName, m.X, m.Z, e.Action)
	return w.waitUntil(openWait, func() bool { return w.findHopLoc(e) != nil })
}

// noteRefusal counts a failed hop. Two strikes keep it out of this walk's plans; a door or
// gate that refuses three times is avoided for the Walker's life. Ladders and gangplanks are
// never banned for good: a refusal there is usually a scene still loading, and banning the only
// way off a ship deck strands the account (the TS barrierBannable lesson).
//
// Why a slashable web now joins them, found 2026-09-20. These counters live for the Walker's
// life, and a web refuses on the dice rather than on a reason, so the lifetime ban was not a
// risk on that route but an arithmetic certainty: the Varrock sewer web at 3210,9898 is the
// ONLY way into the western sewer (planning with it excluded leaves the whole chamber
// unreachable), a CombatTrainer run crosses it twice a bank trip, and at two swings a crossing
// failed a quarter of the time. Three such failures banned it for good, after which every later
// plan answered "unreachable" — which is how a 3h7m run ended, 51 tiles from its own stand, at
// a web it had crossed a minute earlier. Banning the only door into a room strands an account
// exactly as banning the only gangplank off a deck does. The bounded per-walk avoid below is
// still the right answer to a barrier that genuinely will not open, and it still applies.
//
// Why the guard below exists, measured on the live fleet 2026-09-22 (run/fleet.log, 269
// accounts). 100 ProgressiveFisher accounts were converted to ScarabSwarm while standing inside
// the Fishing Guild, whose only ways out are the door 2025 at 2611,3394 and the door 1530 at
// 2611,3398. Door 2025 refused 98 crossings and answered 97 in the same hour. Three refusals in
// a row are therefore ordinary, and each account that hit one got
// "Door at 2611,3394 refused 3 crossings, routing around it from now on" — after which every
// later plan answered "unreachable from here (best 111 tiles)" for a 112-tile walk, forever.
// 68 accounts were stranded that way, and the walker never tried the door again because a ban
// used to last for the Walker's life.
//
// The rule the web taught (do not ban the only way into a room) was written as an exception for
// slashable webs. This makes it general: ask the planner whether routing around the barrier
// leaves the destination reachable, and if it does not, keep trying it. That is the honest
// reading of "the barrier refused" — it means the crossing failed, not that another way exists.
//
// It returns how many times in a row the crossing has now refused while being the only way to
// dest, 0 when it is not (or not yet known to be) the only way.
func (w *Walker) noteRefusal(e *Edge, avoid map[Point]bool, dest Point, opt Options) int {
	key := Point{X: e.LocX, Z: e.LocZ, Level: e.From.Level}
	w.strikes[key]++
	n := w.strikes[key]
	if n < doorAvoidStrike {
		return 0
	}
	if n >= shortcutBanStrikes && e.failureHurts() && !(e.Slip != nil && w.watchesHitpoints()) {
		// A skill-gated shortcut that keeps failing is not a barrier that refuses on the dice: each failed
		// attempt costs hitpoints (the Fremennik basalt rocks' "You slip on the slimy causeway ... the tide
		// sweeps you back" deals 1-10 a slip, and an account under the shortcut's real odds slips most
		// tries), so it is never "the only way, keep trying". Out of every plan for shortcutBanFor even when
		// nothing else reaches dest: the walk then fails, and the quest or trainer above it parks.
		//
		// Except a shortcut whose Slip is known, while the walker can watch the hitpoints: restForSlip never
		// lets it jump on fewer than a slip can take, so keeping it costs time (a hitpoint a minute) and not the
		// account, and banning it when it is the only way home, as the Fremennik basalt rocks' southbound
		// rock is, stranded two accounts on the Lighthouse shore for two hours on 14 Oct 2026. It is banned
		// below only when a way round it exists.
		if !w.isBanned(key) {
			w.banned[key] = w.Client.Now().Add(shortcutBanFor - doorBanFor)
			w.logf("%s at %d,%d refused %d crossings and a failed attempt hurts: out of every plan for %v, even if nothing else reaches %s", e.LocName, e.LocX, e.LocZ, n, shortcutBanFor, dest)
		}
		delete(w.onlyWay, key)
		avoid[key] = true
		return 0
	}
	if w.onlyWayTo(key, dest, opt) {
		w.onlyWay[key]++
		w.logf("%s at %d,%d refused %d crossings, but routing around it leaves %s unreachable, so it stays in the plan", e.LocName, e.LocX, e.LocZ, n, dest)
		return w.onlyWay[key]
	}
	delete(w.onlyWay, key)
	avoid[key] = true
	if n >= shortcutBanStrikes && e.failureHurts() && !w.isBanned(key) {
		// A slipping shortcut with a way round it (the branch above leaves it in when there is none).
		w.banned[key] = w.Client.Now().Add(shortcutBanFor - doorBanFor)
		w.logf("%s at %d,%d refused %d crossings and a failed attempt hurts: out of every plan for %v, a way round it reaches %s", e.LocName, e.LocX, e.LocZ, n, shortcutBanFor, dest)
	}
	if n >= doorBanStrikes && (e.Kind == KindDoor || e.Kind == KindGate) && !e.NeedsSlashTool() && !w.isBanned(key) {
		w.banned[key] = w.Client.Now()
		w.logf("%s at %d,%d refused %d crossings, routing around it for the next %v", e.LocName, e.LocX, e.LocZ, n, doorBanFor)
	}
	return 0
}

// onlyWayGiveUp is the refusals in a row of the only way to a destination after which a walk stops
// replanning and fails, pausing first (onlyWayPause). A door that refuses now and then crosses well
// before this; a crossing the account cannot make at all does not. Live on 0050e8c9 (5 Oct 2026)
// acct18, inside Tree Gnome Village with its Spirit Tree "not interactable" as the only way out
// in the graph, planned the same 123,230-node route and its onlyWayTo probe about five times a
// second.
const onlyWayGiveUp = 6

// onlyWayPause is how long a walk waits before failing on the n-th refusal in a row of the only way
// to its destination: 5 s at onlyWayGiveUp, 5 s more each refusal after, at most a minute.
func onlyWayPause(n int) time.Duration {
	d := time.Duration(n-onlyWayGiveUp+1) * 5 * time.Second
	return min(max(d, 5*time.Second), time.Minute)
}

// onlyWayTo reports whether leaving the barrier placed at key out of the search makes dest
// unreachable from where the player stands. opt is the options the failed route was planned
// with, so the answer is judged under the same zones, walls, state and handlers.
func (w *Walker) onlyWayTo(key, dest Point, opt Options) bool {
	me, ok := w.Client.Tile()
	if !ok {
		return false
	}
	probe := opt
	probe.Avoid = map[Point]bool{key: true}
	for k := range opt.Avoid {
		probe.Avoid[k] = true
	}
	r := w.Planner.Plan(me, dest, probe)
	if r.OK {
		return r.End() == me
	}
	// A search that ran out of expansions has not shown that anything is unreachable, and
	// treating it as proof would quietly switch banning off on every long route.
	return !strings.Contains(r.Reason, "budget")
}

// isBanned reports whether the barrier placed at p is currently out of every plan, forgetting a
// ban that has outlived doorBanFor. A lapse is logged because a barrier coming back into the
// plans is exactly what a stuck account's log has to show.
func (w *Walker) isBanned(p Point) bool {
	at, ok := w.banned[p]
	if !ok {
		return false
	}
	if w.Client.Now().Sub(at) < doorBanFor {
		return true
	}
	delete(w.banned, p)
	w.logf("the ban on the barrier at %s has lapsed after %v; it is back in the plans", p, doorBanFor)
	return false
}

// Strikes reports how many crossings the barrier placed at p has refused, and Banned whether
// it has been put out of every later plan for this Walker's life.
//
// They are exported for one reason: a live harness that can only watch the log cannot tell
// "the web was never banned" from "nothing ever drove it near the threshold", and those two
// sentences read identically in a report. A check that passed on 0 of 0 samples is a mistake
// this repo has already shipped (docs/trustworthy_harnesses.md), so the counters themselves
// are readable and an assertion can carry the number that justifies it. Read them between
// walks: the Walker is not safe for concurrent use.
func (w *Walker) Strikes(p Point) int { return w.strikes[p] }

// Banned reports whether the barrier placed at p is currently excluded from every plan. A ban
// lapses after doorBanFor (see isBanned), so this can go from true back to false.
func (w *Walker) Banned(p Point) bool { return w.isBanned(p) }

// sceneWalk clicks straight toward dest (at most chunkSteps tiles) and lets the server walk as
// far as it can: DirectNavigator.walkTo. It returns false only on a stop.
func (w *Walker) sceneWalk(dest Point, radius int) bool {
	me, ok := w.Client.Tile()
	if !ok || me.Level != dest.Level {
		return w.pause(tick)
	}
	clamp := func(d int) int {
		if d > chunkSteps {
			return chunkSteps
		}
		if d < -chunkSteps {
			return -chunkSteps
		}
		return d
	}
	target := Point{X: me.X + clamp(dest.X-me.X), Z: me.Z + clamp(dest.Z-me.Z), Level: me.Level}
	target = stopShortOfZones(me, target, w.activeZones())
	if target == me {
		return w.pause(tick)
	}
	if err := w.sendWalk([]Point{me, target}, w.runFlag()); err != nil {
		return w.pause(tick)
	}
	last, lastMove := me, w.Client.Now()
	end := w.Client.Now().Add(sceneWalkFor)
	for w.Client.Now().Before(end) {
		if !w.pause(tick) {
			return false
		}
		cur, _ := w.Client.Tile()
		if w.arrived(cur, dest, radius) {
			return true
		}
		if cur != last {
			last, lastMove = cur, w.Client.Now()
		} else if w.Client.Now().Sub(lastMove) >= 2*tick {
			return true
		}
	}
	return true
}

// stopShortOfZones is target, or the last tile before the straight click from me to target
// would enter a zone: the server walks a click diagonally until one axis lines up, then straight.
// sceneWalk is DirectNavigator.walkTo, which predates the Go danger zones and clicked through
// them, so a walk the zones had blocked (Kandarin is cut off from Asgarnia by the wolf zone)
// could reach the ladder's scene rung and walk up White Wolf Mountain after all. A player
// already inside a zone is walking out of it and is not held back.
func stopShortOfZones(me, target Point, zones []Rect) Point {
	if len(zones) == 0 || InZones(me, zones) {
		return target
	}
	cur := me
	for cur != target {
		next := Point{X: cur.X + sign(target.X-cur.X), Z: cur.Z + sign(target.Z-cur.Z), Level: cur.Level}
		if InZones(next, zones) {
			return cur
		}
		cur = next
	}
	return target
}

// unstick opens a shut door or gate within 3 tiles, then takes one step in the next direction
// the pack allows: walkResilient's unstick rung. It returns false only on a stop.
func (w *Walker) unstick(dir *int) bool {
	if stopped := w.tryNearbyDoor(); stopped {
		return false
	}
	me, ok := w.Client.Tile()
	if !ok {
		return w.pause(tick)
	}
	mask := w.Planner.col.ExitMask(me.X, me.Z, me.Level)
	// Like sceneWalk, never a step into a zone from outside one (see stopShortOfZones).
	zones := w.activeZones()
	outside := !InZones(me, zones)
	dx, dz, found := pickUnstickStep(func(dx, dz int) bool {
		if outside && InZones(Point{X: me.X + dx, Z: me.Z + dz, Level: me.Level}, zones) {
			return false
		}
		for d := 0; d < 8; d++ {
			if dirX[d] == dx && dirZ[d] == dz {
				return mask&(1<<uint(d)) != 0
			}
		}
		return false
	}, *dir)
	*dir = (*dir + 3) % 8
	if !found {
		return w.pause(tick)
	}
	step := Point{X: me.X + dx, Z: me.Z + dz, Level: me.Level}
	if err := w.sendWalk([]Point{me, step}, false); err != nil {
		return w.pause(tick)
	}
	_, stopped := w.waitUntil(unstickStepFor, func() bool { return w.at(step) })
	return !stopped
}

// tryNearbyDoor opens the nearest shut door or gate within 3 tiles that is not banned:
// WalkExecutor.tryNearbyDoor. It returns true only on a stop.
func (w *Walker) tryNearbyDoor() (stopped bool) {
	me, ok := w.Client.Tile()
	if !ok {
		return false
	}
	for _, m := range w.Client.FindLocs(LocQuery{At: me, Radius: 3, Names: w.doorNames, Op: "Open"}) {
		if w.isBanned(Point{X: m.X, Z: m.Z, Level: m.Level}) {
			continue
		}
		door := m
		// Stand on the door tile or an orthogonal neighbour, whichever is reachable nearest.
		var stand *Point
		bestCost := 1 << 30
		for _, off := range [5][2]int{{0, 0}, {0, 1}, {1, 0}, {0, -1}, {-1, 0}} {
			c := Point{X: door.X + off[0], Z: door.Z + off[1], Level: door.Level}
			if !w.Planner.col.Walkable(c.X, c.Z, c.Level) {
				continue
			}
			if r := w.Planner.Plan(me, c, Options{WalkOnly: true, MaxExpansions: chunkBudget}); r.OK && r.End() == c && r.Cost < bestCost {
				cc := c
				stand, bestCost = &cc, r.Cost
			}
		}
		if stand == nil {
			continue
		}
		w.logf("stalled near shut %s at %d,%d, opening it", door.Name, door.X, door.Z)
		if _, stopped := w.walkExact(*stand); stopped {
			return true
		}
		if err := w.Client.OpLoc(door.Op, door.X, door.Z, door.ID); err != nil {
			return false
		}
		_, stopped := w.waitUntil(openWait, func() bool {
			for _, again := range w.Client.FindLocs(LocQuery{At: Point{X: door.X, Z: door.Z, Level: door.Level}, Radius: 1, IDs: []int{door.ID}, Op: "Open"}) {
				if again.X == door.X && again.Z == door.Z {
					return false
				}
			}
			return true
		})
		return stopped
	}
	return false
}
