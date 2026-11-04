// Package donorrunner is the I/O half of a skilling script's side of a mule hand-over: it
// drives internal/scripts/donorplan's pure Handover state machine (walk to the mule's post,
// request a trade, offer the load, accept, confirm, repeat if the pack still holds haul,
// give up after MeetTimeout) against a live session, the way fisherrunner/barbarianrunner/
// mulerunner each extract one script's own proven mechanics into a shared package so a
// standalone tool and a registered internal/bot script never drift into two copies.
//
// Deliberately built script-agnostic, per the account owner's own ask (2026-09-14): every
// skilling script hands its bank to the same mule at the same Seers' Village stand
// (mulereceiverplan.SeersStand), so ProgressiveFisher today and ProgressiveCrafter/
// EssenceRunner whenever they're wired up next all drive the identical meet-and-trade
// sequence. What differs per script — which bank, which items are "for the mule," how the
// load gets drawn out in the first place — stays in that script's own bot.go and its own
// plan package's MuleRows/MuleHaul/MuleLoad wrappers (see fisherplan.MuleRows,
// crafterplan.MuleRows, essenceplan.MuleRows: the same three-function shape by design). This
// package only takes over once that load is already sitting in the pack.
//
// Every blocking wait goes through a caller-supplied Delayer instead of time.Sleep directly,
// matching every sibling runner package, so internal/bot's Context.Delay can actually cancel
// a running hand-over mid-wait.
package donorrunner

import (
	"fmt"
	"rsb0t-go/internal/mulebus"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Delayer sleeps for d, returning false early if the caller should stop instead of true
// once d has fully elapsed — the same shape barbarianrunner/fisherrunner/mulerunner use.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration — for a standalone tool with nothing to cancel.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one hand-over needs from the outside world, the same sibling shape as
// mulerunner.Deps/fisherrunner.Deps (Locs/Config/Components are only used to find the
// nearest reachable spot, not for name resolution — the caller already resolved item names
// into ItemRow/LoadGroup before handing them to Run).
type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	Log        func(string)
	// Bus and Me, when both set, let Run tell its mule it is coming: KindReady when a load is
	// drawn, KindCancel when Run ends however it ends. Owner 2026-09-19: the mule cooks
	// without waiting unless a donor has said it wants to hand off. Nil keeps the old silence.
	Bus *mulebus.Bus
	Me  string

	// WalkFallback, when set, is a door-aware walk (webwalk with crossings) tried once when the
	// baked pack has no path, which treats every shut door as a wall. Live 7 Oct 2026: acct75
	// stood in a Seers house with its door shut after a random event and logged "donor: no path
	// to 2809,3441,0" every 2 s for over 30 minutes. It reports whether the walk arrived.
	WalkFallback func(dest tile.Tile, radius int) bool

	// hopHooks is world hopping (worldhop.go in this package): reaching a mule that is
	// logged in to a different game world. Every field is optional and a Deps that leaves
	// them all zero behaves exactly as it did before they existed.
	hopHooks
}

func (d *Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

// Options is one hand-over's script-specific policy, supplied by the caller (a bot.go).
type Options struct {
	// Mule is the partner name to request a trade from and to offer everything to.
	Mule string
	// Stand is where the mule posts — mulereceiverplan.SeersStand for every script tonight.
	Stand tile.Tile
	// RowsInBank/Drawn are donorplan.Handover.Loaded's own inputs: how many distinct bank
	// rows existed for the hand-off, and how many pack slots the withdrawal actually
	// filled — the caller has already done the withdrawal (its own bank, its own layout)
	// before calling Run.
	RowsInBank, Drawn int
	// Load returns the current trade-screen plan (e.g. fisherplan.MuleLoad(packRows,
	// freeSlots, keep)) — called fresh each poll, since offering a group changes the pack.
	Load func() []donorplan.LoadGroup
	// HaulLeft returns how many pack slots still hold something for the mule (e.g.
	// fisherplan.MuleHaul(packRows, keep)) — called after a trade screen closes to decide
	// whether another round is needed (donorplan.TradeEnded/Handover.Traded).
	HaulLeft func() int
	// CoinsName/CoinsID/CoinsHeld/Reserve implement the account owner's exact ask
	// (2026-09-14): withdraw everything except tools and 500gp. Rather than a whole-row
	// keep/hand-over choice (donorplan.Rules' own all-or-nothing model), coins are split:
	// held-Reserve crosses to the mule, Reserve stays in the donor's own pocket as a
	// travel/fare buffer, matching the account owner's stated number exactly. CoinsName
	// blank or Reserve <= 0 disables the split (coins behave like any other row, governed
	// purely by the caller's own Load/HaulLeft — e.g. a script whose Keep list already
	// excludes coins entirely).
	CoinsName string
	CoinsID   int32
	CoinsHeld func() int
	Reserve   int

	// KnownUntradable is the caller's own long-lived record of item ids this account has
	// already discovered can't cross a trade screen (e.g. a quest item drawn into a load
	// once already found to sit at the offer screen forever — see driveTrade's own
	// maxOfferTries doc comment). Optional and nil-safe: nil just means "nothing known yet,"
	// exactly like before this field existed. When set, Run seeds this hand-over's own
	// per-round skip set from it (so an already-known offender is never even attempted) and
	// adds any newly-discovered offender back into it — the account owner's own ask
	// (2026-09-16): a genuinely untradeable item already costs one wasted round of
	// maxOfferTries offers to discover; without this, every SEPARATE future hand-off paid
	// that same cost again forever, which reads to an operator watching the log as "it
	// keeps looping trying to hand this over." The caller owns the map's lifetime (e.g. one
	// field on a long-lived bot struct, matching internal/bot/fisher.go's own thief/sail
	// trackers) so it survives across many Run calls, not just the rounds within one.
	KnownUntradable map[int]bool
}

// Result is one Run call's outcome.
type Result struct {
	Loads, Slots int
	// Traded is true once at least one item stack actually crossed to the mule.
	Traded bool
	// GaveUp is true when the mule never showed up within donorplan.MeetTimeout — the
	// caller should return the load to the bank and resume its own script loop, trying
	// again no sooner than donorplan.HandoffRetry (see donorplan.NextAllowed).
	GaveUp bool
	// Stopped is true when the caller's own Delay reported a stop request mid hand-over —
	// distinct from GaveUp so a caller doesn't apply the retry cooldown to a plain Stop().
	Stopped bool
	// Hopping is true when this run ended because a WORLD HOP was accepted (see
	// worldhop.go). Stopped is set alongside it, so a caller written before worlds existed
	// already does the right thing — return from its loop without applying a retry
	// cooldown — and a caller that wants to say something specific can read this.
	//
	// The session is closing when this is true. Do not send anything else.
	Hopping bool
	Reason  string
}

func currentTile(d *Deps) (tile.Tile, bool) {
	x, z, level, ok := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}, ok
}

// emergencyFleeHome/emergencyFleeDriftTiles: this package opts OUT of EmergencyFlee's own
// drift check (driftTiles 0 — see that function's own doc comment for why every caller now
// supplies its own pair instead of this package assuming every script operates near
// Catherby), on purpose: donorrunner is deliberately script-agnostic (per this file's own
// package doc comment — the same mule hand-over drives fisher/crafter/whatever's own
// skilling script next), and walkNear's origin varies with whichever script called it, with
// no single fixed "home" to check against. Its own bounded (75s) walk already re-checks
// real FindPath progress toward dest every iteration, unlike a blind sleep, so EmergencyFlee
// is called here purely for its InCombat branch (unaffected by driftTiles<=0), not the
// drift one.
var emergencyFleeHome pathfind.NavPoint // unused while emergencyFleeDriftTiles <= 0

const emergencyFleeDriftTiles = 0

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// walkNear paths to dest and walks there, re-clicking from wherever the account stops,
// until within radius tiles — donorrunner's own copy of the same small helper every runner
// package keeps (see mulerunner.walkNear's own doc comment on why this is duplicated
// per-package rather than shared).
func walkNear(d *Deps, dest tile.Tile, radius int) bool {
	return walkNearOutcome(d, dest, radius) == walkReached
}

// walkOutcome is how one walkNear call ended. MeetWalk needs to tell them apart: only a
// FindPath "unreachable" says anything about water, and a walk that simply ran out of time on a
// road the pack can route is not evidence of a stranding.
type walkOutcome int

const (
	walkReached  walkOutcome = iota // within radius of dest
	walkNoPath                      // the pack has no path from here: the only real "unreachable"
	walkTimedOut                    // walkNearTimeout ran out on a walkable road
	walkFailed                      // the walk packet failed, or the caller asked to stop
)

// walkNearTimeout bounds one walkNear call.
const walkNearTimeout = 75 * time.Second

// walkNearOutcome is walkNear with the reason it stopped.
func walkNearOutcome(d *Deps, dest tile.Tile, radius int) walkOutcome {
	deadline := time.Now().Add(walkNearTimeout)
	for time.Now().Before(deadline) {
		// EmergencyFlee here — found during a repo-wide audit following tonight's
		// incident response (same gap already fixed in every sibling runner's own walk
		// helper): this loop can hold the account mid-walk for up to 75s with nothing
		// checking combat or drift in between. Safe every tick — TryLock inside
		// EmergencyFlee already makes it a no-op whenever Guardian's kick() owns the
		// action lock instead.
		randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
		here, ok := currentTile(d)
		if ok && here.Level == dest.Level && abs(here.X-dest.X) <= radius && abs(here.Z-dest.Z) <= radius {
			return walkReached
		}
		if !ok {
			if !d.Delay(200 * time.Millisecond) {
				return walkFailed
			}
			continue
		}
		res := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level})
		if !res.OK {
			d.logf("donor: no path to %d,%d,%d: %s", dest.X, dest.Z, dest.Level, res.Reason)
			if d.WalkFallback != nil {
				if d.WalkFallback(dest, radius) {
					d.logf("donor: the door-aware walk reached %d,%d,%d", dest.X, dest.Z, dest.Level)
					return walkReached
				}
				d.logf("donor: the door-aware walk to %d,%d,%d did not arrive either", dest.X, dest.Z, dest.Level)
			}
			return walkNoPath
		}
		if len(res.Waypoints) < 2 {
			return walkReached
		}
		if err := actions.Walk(d.Session, res.Waypoints, false); err != nil {
			d.logf("donor: walk: %v", err)
			return walkFailed
		}
		if !d.Delay(1500 * time.Millisecond) {
			return walkFailed
		}
	}
	return walkTimedOut
}

// nextStandUnreachable is MeetWalk's consecutive-"unreachable" count after one walk to the
// stand ended the way o says. Only walkNoPath counts toward standUnreachableBeforeCrossing: it
// is the deterministic pack answer the water check was written for. A timeout or a failed walk
// resets the count, because the pack HAS a path from there.
//
// THE FALSE ALARM THIS ENDS. The count used to step on any walkNear failure, and walkNear also
// fails, silently, when its 75-second budget runs out. On production the Seers-to-Catherby walk
// overran that budget three times in a row for ProgressiveWCFletch donors: 99 of the 102
// "stranded across water" lines on 2026-09-22/23 fired 229-235 s after the load was drawn (three
// budgets plus two 2 s retries), none preceded by a "no path" line, on 51 accounts walking a
// road the pack routes end to end.
func nextStandUnreachable(prev int, o walkOutcome) int {
	if o == walkNoPath {
		return prev + 1
	}
	return 0
}

// ---- stranded-across-water recovery ----
//
// Real, live-caught gap (production account "acct", ProgressiveFisher, 2026-09-16): a
// donor account can end up displaced far from o.Stand — the caller's own configured mule
// post, e.g. a muleAtTile setting pointed at Catherby's bank (2809,3441,0) — with a real
// water gap between wherever it actually is and that tile. walkNear's plain same-level
// WalkNear can never cross that; it just repeats "unreachable" forever (see Run's own
// standUnreachable doc comment). This mirrors fisherrunner.ReturnToCatherby/
// travelViaFreshStart — the identical fix already proven live for the identical class of
// problem — generalised over an arbitrary o.Stand instead of a fixed Catherby tile, since
// donorrunner is deliberately script-agnostic (see this file's own package doc comment): the
// mule hand-off is shared logic across whichever skilling script's own donor calls Run, not
// just the fisher's.
//
// Duplicated here rather than calling fisherrunner directly: fisherrunner (and every other
// skilling script's own runner) imports donorrunner already (to drive its own hand-off), so
// donorrunner importing fisherrunner back would be a real import cycle, not just an
// awkward layering — confirmed by grep across the whole tree before writing this (only
// internal/bot/*.go and cmd/handofftest import donorrunner; no script runner package does).
// freshstartrunner has no such issue: it's a lower-level primitive package (WalkNear,
// PickpocketStep, BuySkirtStep, SailLeg, the Thief/SailProgress trackers) with no reverse
// dependency on donorrunner or any other script runner, so donorrunner can import it
// directly — the same way fisherrunner/barbarianrunner/crafterrunner already do for their
// own first-day/stranding recovery.

// standUnreachableBeforeCrossing is how many consecutive MeetWalk rounds walkNear must fail
// to reach o.Stand before Run checks for a genuine cross-water stranding at all. A single
// failure is enough signal for a real FindPath "unreachable" (deterministic, not flaky), but
// this still gives an ordinary transient hiccup (e.g. a door mid-open elsewhere, a momentary
// bad read) a couple of rounds to clear on its own before paying for the check — matching the
// spirit of ReturnToCatherby's own multi-attempt loop before it falls back.
const standUnreachableBeforeCrossing = 3

// strandedAcrossWater reports whether here, walking to stand, needs the Port Sarim pier
// crossing at all — the same landmass classification every sibling Road() (barbarianplan/
// fisherplan/crafterplan) already relies on for this exact class of recovery. Kept as its own
// small, pure, testable function per this codebase's established pattern (see
// freshstartrunner's stairselect_test.go for the style) rather than inlined at the one call site.
//
// RELATIVE TO THE STAND, since 2026-09-23. freshstart.TravelLeg never looks at the destination:
// it answers LegPier for any tile off Karamja with X >= ArdougneSideMaxX (2740). That is the
// right reading for barbarianrunner, whose outpost is west of 2740 and copied it here — and
// wrong for a stand at the Catherby bank (2809,3441), which reads LegPier itself. From the Seers
// bank the classifier flipped to "stranded" at step 21 of the 106-tile walk (2740,3481) and
// stayed there to the stand. Two cases are now never a stranding:
//
//   - within freshstart.DestinationRadius of the stand, because crossWaterTo's own road would
//     answer "arrived" there before sailing anything, so the check could only ever be a no-op
//     that logs a misleading line;
//   - inside a maze or mime instance, whose square lies east of 2740 too: the event sends the
//     account back by itself (crossWaterTo waits it out in waitForGuardian anyway).
//
// Every live true positive is kept: 3222,3217 (a Lumbridge respawn), 3220,3259 (the "catherby"
// incident) and 3278,3500,1 (Varrock), all far outside the radius and all unreachable by pack.
func strandedAcrossWater(here, stand tile.Tile) bool {
	if randomevents.InTrapEventSquare(here.X, here.Z) {
		return false
	}
	if tile.Chebyshev(here, stand) <= freshstart.DestinationRadius {
		return false
	}
	return freshstart.TravelLeg(here) == freshstart.LegPier
}

// freshStartDeps adapts Deps to freshstartrunner.Deps — same field-for-field copy
// fisherrunner.freshStartDeps/barbarianrunner.freshStartDeps already do, this project's
// established convention for this exact adapter.
func freshStartDeps(d *Deps) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay), Log: d.Log,
	}
}

// currentTileOrZero is currentTile without the ok flag, for callers (freshStartSnapshot) that
// only need a best-effort tile — matching freshstartrunner's own currentTileOrZero.
func currentTileOrZero(d *Deps) tile.Tile {
	t, _ := currentTile(d)
	return t
}

// anyObjIDIn reports whether items holds any of ids — donorrunner's own copy of
// fisherrunner's identical private helper, duplicated per this project's established
// per-package convention (see normaliseTargetName's own doc comment for why).
func anyObjIDIn(items []gamestate.SlotItem, ids []int) bool {
	for _, it := range items {
		for _, id := range ids {
			if int(it.ID) == id {
				return true
			}
		}
	}
	return false
}

// freshStartCoinsHeld counts coins in the donor's own pack — donorrunner's copy of
// fisherrunner.freshStartCoins/barbarianrunner's identical helper.
//
// Reads PlayerPack, the same fixed component every sibling runner uses. An earlier version of
// this comment said donorrunner read the pack through actions.Equipment because it "never
// needed that constant before"; that was an accident of history rather than a decision, and it
// put this count on the one view content can take away (see heldOf's doc comment). It matters
// here because this feeds the fare arithmetic on the recovery route that SAILS — and ~set_sail
// nulls the backpack tab from boarding until ~initalltabs runs at the end of the route's own
// p_delay. That window is shorter than the crossing, but the fare is counted once, and a count
// taken inside it reads no coins at all.
func freshStartCoinsHeld(d *Deps) int {
	if d.Config == nil {
		return 0
	}
	ids := d.Config.ObjIDs(freshstart.Coins)
	n := 0
	for _, it := range packItems(d) {
		for _, id := range ids {
			if int(it.ID) == id {
				c := it.Count
				if c < 1 {
					c = 1
				}
				n += int(c)
				break
			}
		}
	}
	return n
}

// freshStartSnapshot builds the freshstart.Snapshot fisherplan/barbarianplan's own
// DecidePhase reads, from live state — donorrunner's own copy of fisherrunner's identical
// twin, generalised over crossWaterTo's own dest/road instead of a fixed Catherby layout.
func freshStartSnapshot(d *Deps) freshstart.Snapshot {
	main, _, _ := d.Session.State().Modals()
	s := freshstart.Snapshot{MainModal: main, Fare: freshstart.DefaultFare, SkirtCost: freshstart.DefaultSkirtCost}
	t := currentTileOrZero(d)
	s.Tile = &t
	s.Coins = freshStartCoinsHeld(d)
	if d.Components != nil && d.Config != nil {
		eq := actions.NewEquipment(d.Session, d.Components)
		ids := d.Config.ObjIDs(freshstart.PinkSkirt)
		s.SkirtWorn = anyObjIDIn(eq.Worn(), ids)
		s.SkirtHeld = anyObjIDIn(eq.Inventory(), ids)
	}
	return s
}

// waitForGuardian pauses while a random event is detected at all, the same shape every
// sibling runner's own WaitForGuardian already has (fisherrunner.WaitForGuardian,
// barbarianrunner.WaitForGuardian) — duplicated per package per this project's established
// convention. Uses driftTiles 0 (see emergencyFleeHome's own doc comment at the top of this
// file): donorrunner has no single fixed "home" to drift-check against, since crossWaterTo's
// own destination varies with whichever script's own o.Stand called Run.
func waitForGuardian(d *Deps) bool {
	randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
	if !randomevents.Busy(d.Session) {
		return true
	}
	d.logf("donor: [guardian] a random event is present; waiting")
	lastLog := time.Now()
	for randomevents.Busy(d.Session) {
		randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
		if !d.Delay(500 * time.Millisecond) {
			return false
		}
		if time.Since(lastLog) >= 10*time.Second {
			d.logf("donor: [guardian] still waiting on a random event")
			lastLog = time.Now()
		}
	}
	return true
}

// crossWaterTo drives internal/scripts/freshstartrunner — already built generic and
// script-agnostic anticipating more than one caller (see that package's own doc comment) —
// against a freshstart.Road pointed at dest instead of a fixed Catherby bank: pickpocket Men
// for the fare, buy and wear a pink skirt in Varrock, then sail Port Sarim -> Karamja ->
// Brimhaven -> Ardougne -> toward dest. Adapted from fisherrunner.travelViaFreshStart/
// barbarianrunner.travelViaFreshStartTo (this package's own copy, per the same duplication
// convention — see this file's own package-level doc comment on why an extraction into a
// deeper shared package isn't needed: freshstartrunner already IS that shared package).
// Bounded at 20 minutes, matching every sibling's identical budget for this exact class of
// genuine cross-map recovery.
//
// Uses freshstart.DestinationRadius (80) as the road's own "arrived" tolerance, the same
// generous radius fisherplan.Road()/barbarianplan's own Road use, rather than o.Stand's
// tight donorplan.StandRadius (3): SailProgress converges the last leg of a real sail-plus-
// walk journey to within tens of tiles, not exactly three, and Run's own ordinary MeetWalk
// loop already closes the last few tiles with a plain walkNear once back on the correct
// landmass — this function's job is only getting there at all, not landing precisely.
//
// arrived reports reaching the road's radius; stopped reports that the caller's Delay asked to
// stop part way, which Run turns into Result.Stopped rather than a failed crossing.
func crossWaterTo(d *Deps, dest tile.Tile) (arrived, stopped bool) {
	fd := freshStartDeps(d)
	thief := freshstartrunner.NewThief()
	sail := freshstartrunner.NewSailProgress()
	road := freshstart.Road{Destination: dest, Radius: freshstart.DestinationRadius}
	deadline := time.Now().Add(20 * time.Minute)
	for time.Now().Before(deadline) {
		// A stop request must end the crossing here, not twenty minutes later. Several phases
		// below (the skirt purchase, a sail leg) return on a refused Delay without this loop
		// ever calling one itself, so without this check a stopped account went on walking its
		// route until the deadline. Delay(0) is the stop check that waits for nothing.
		if !d.Delay(0) {
			return false, true
		}
		if road.Arrived(currentTileOrZero(d)) {
			return true, false
		}
		// Direct-reachability short-circuit — same pattern travelViaFreshStart's own doc
		// comment describes: PhaseFund/PhaseSkirt can misfire on a position that reads as
		// "still needs the first ship" under freshstart.TravelLeg's own ArdougneSideMaxX
		// blind spot even when it isn't really stranded at all (some OTHER, non-water
		// reason a plain walk failed). Checking real pack connectivity to dest first
		// skips Fund/Skirt/Travel entirely whenever a plain walk already gets there.
		if d.Pack != nil {
			here := currentTileOrZero(d)
			if res := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level}); res.OK {
				d.logf("donor freshstart fallback: already on the destination side (no boat needed) — walking directly")
				if walkNear(d, dest, road.Radius) {
					return true, false
				}
				if !d.Delay(3 * time.Second) {
					return false, true
				}
				continue
			}
		}
		if !waitForGuardian(d) {
			return false, true
		}
		snap := freshStartSnapshot(d)
		phase := freshstart.DecidePhase(snap, road)
		d.logf("donor freshstart fallback: %s", freshstart.Describe(phase, "back within reach of the mule stand"))
		switch phase {
		case freshstart.PhaseFund:
			freshstartrunner.PickpocketStep(fd, thief, freshstartrunner.PickpocketOpts{
				Target: freshstart.DefaultPickpocketTarget, Spot: freshstart.PickpocketSpot,
				RestBelow: freshstart.RestBelowHP, Need: freshstart.FundTarget(snap),
			})
		case freshstart.PhaseSkirt:
			freshstartrunner.BuySkirtStep(fd)
		case freshstart.PhaseTravel:
			freshstartrunner.SailLeg(fd, dest, sail)
		case freshstart.PhaseArrived:
			if walkNear(d, dest, road.Radius) {
				return true, false
			}
			d.logf("donor freshstart fallback: close to the mule stand but could not finish the walk — retrying")
			if !d.Delay(3 * time.Second) {
				return false, true
			}
		default:
			d.logf("donor freshstart fallback: unexpected phase %q — waiting", phase)
			if !d.Delay(2 * time.Second) {
				return false, true
			}
		}
	}
	d.logf("donor freshstart fallback: gave up after 20 minutes")
	return false, false
}

// normaliseTargetName folds a configured target name the identical way
// mulerunner.normalisePlayerName does (case-insensitive, underscore-to-space) — see that
// function's own doc comment for why: an account's login username uses underscores where
// its real in-game display name (what Player.Name always is, decoded off the wire) shows a
// space, so a `mule` setting typed as a login-style username (e.g. "some_mule_account")
// must be normalized the same way before comparing against Players(), or it silently never
// matches. Duplicated rather than imported from mulerunner to avoid a cross-package
// dependency for a two-line transform — MUST stay identical to it, not just "similar."
func normaliseTargetName(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "_", " ")
}

// findMule returns the mule's live tile by name via gamestate.State.Players() — nil while
// out of sight, exactly what donorplan.MeetView.Mule expects.
func findMule(d *Deps, name string) *tile.Tile {
	target := normaliseTargetName(name)
	for _, p := range d.Session.State().Players() {
		if normaliseTargetName(p.Name) == target {
			t := tile.Tile{X: p.X, Z: p.Z, Level: p.Level}
			return &t
		}
	}
	return nil
}

// findMuleSlot resolves the mule's name to a live PLAYER_INFO index, the same way
// mulerunner.ResolvePlayerSlot does — needed to send Trade.Request, which addresses by
// index, not name.
func findMuleSlot(d *Deps, name string) (int32, bool) {
	target := normaliseTargetName(name)
	for _, p := range d.Session.State().Players() {
		if normaliseTargetName(p.Name) == target {
			return int32(p.Index), true
		}
	}
	return 0, false
}

// effectiveLoad wraps o.Load with the coins/Reserve split: once held coins are at or below
// Reserve, "Coins" (or whatever CoinsName is) is dropped from the plan entirely rather than
// offered as a group of zero, which would otherwise never register in Offered and stall
// donorplan.DecideTrade on that one group forever.
func effectiveLoad(o Options) []donorplan.LoadGroup {
	groups := o.Load()
	if _, full := muleCoinsFull(o.Mule, o.CoinsName); full {
		return withoutCoins(groups, o.CoinsName) // the mule's coin cap (coincap.go)
	}
	if o.Reserve <= 0 || o.CoinsName == "" || o.CoinsHeld == nil {
		return groups
	}
	if o.CoinsHeld()-o.Reserve > 0 {
		return groups
	}
	return withoutCoins(groups, o.CoinsName)
}

// withoutCoins is groups less the coins group (named coinsName, or "Coins").
func withoutCoins(groups []donorplan.LoadGroup, coinsName string) []donorplan.LoadGroup {
	if coinsName == "" {
		coinsName = "Coins"
	}
	out := make([]donorplan.LoadGroup, 0, len(groups))
	for _, g := range groups {
		if !strings.EqualFold(g.Name, coinsName) {
			out = append(out, g)
		}
	}
	return out
}

// emptyPlanReason says why this hand-off must not meet the mule, or "" when it may: the pack
// holds haul (o.HaulLeft, less anything already found untradeable) but the trade plan
// (o.Load after the coin reserve and the skip set) has nothing on it.
//
// THE LOOP THIS ENDS, live on build 3dfb543a (2026-09-23, 02:05-02:44 Perth): ten
// ProgressiveWCFletch donors drew a load with 26 unnoted logs already in the pack. MuleHaul
// counted all 26 slots as haul, but PlanLoad offers a group whole or not at all and one group of
// 26 slots does not fit DefaultMuleFreeSlots (23), so the plan was empty. Every donor walked to
// Catherby anyway and re-requested a trade every ~13 seconds: 144 trades that closed with
// nothing on either side ("the load it was sent to deliver is not offerable" on the donor,
// "no nonempty stable offer arrived — declining" on the mule), 784 seconds of the mule's time
// against 288 for the 41 trades that did cross in the same forty minutes. Only the five-minute
// meet cap ended each loop, and the donor then drew the same load again.
//
// A plan that is empty now stays empty: nothing on the way to the mule or at its post adds a
// plannable group to the pack. So the right moment to say so is before walking anywhere, and
// again whenever a trade closes with haul still left. The caller puts the load back and retries
// after its usual cooldown, which is what it already does for every other GaveUp.
//
// HaulLeft is read BEFORE the plan on purpose: after a trade closes, the offered stacks return
// to the pack a tick or two later, and reading the plan first could see the pack before they
// land and the haul after, which would read as exactly this condition when it is not.
func emptyPlanReason(d *Deps, o Options, skip map[int]bool) string {
	if o.HaulLeft == nil || o.Load == nil {
		return ""
	}
	raw := o.HaulLeft()
	if raw <= 0 {
		return ""
	}
	haul := raw - skippedSlots(d, skip)
	if haul <= 0 {
		// EVERY SLOT OF HAUL IS SOMETHING ALREADY LEARNED NOT TO CROSS. This used to read as
		// "nothing left" and return "", which is right after a trade (the tradeable part has
		// gone) but wrong BEFORE one: live 2026-09-26 06:40-06:43 spare104 set off with a pack of
		// Cosmic, Nature and Law runes its previous round had put in KnownUntradable, summoned
		// the mule ("on its way here by walk"), skipped all three groups, declined an empty trade,
		// and was sent again 5 s later -- more than ten times. A load the trade screen will not
		// take is not a hand-off; the caller banks it and goes back to work.
		return fmt.Sprintf("all %d pack slot(s) of haul are items already found untradeable this session — not meeting the mule for a load that cannot cross", raw)
	}
	if plan := withoutSkipped(effectiveLoad(o), skip); len(plan) > 0 {
		return ""
	}
	return fmt.Sprintf("%d pack slot(s) hold something for the mule but none of it can go on a trade screen (a stack too big for the mule's free slots, or only coins inside the travel reserve) — putting the load back instead of trading nothing", haul)
}

// offerGroup places g on the trade screen: coins are offered as held-Reserve via
// Trade.Offer (an exact quantity, leaving Reserve behind in the donor's own pack — the
// account owner's "500gp for travel"); everything else is offered whole via OfferAll,
// matching every other script's hand-over exactly as donorplan.LoadGroup already models it
// (a stack crosses complete, never split).
func offerGroup(d *Deps, trade *actions.Trade, g donorplan.LoadGroup, o Options) {
	if o.Reserve > 0 && o.CoinsName != "" && strings.EqualFold(g.Name, o.CoinsName) && o.CoinsHeld != nil {
		held := o.CoinsHeld()
		n := held - o.Reserve
		if n <= 0 {
			return // effectiveLoad should already have dropped this group; defensive only.
		}
		if err := trade.Offer(int32(g.ID), int32(n), 5*time.Second); err != nil {
			d.logf("donor: offer %d %s: %v", n, g.Name, err)
		}
		return
	}
	if err := trade.OfferAll(int32(g.ID)); err != nil {
		d.logf("donor: offer %s: %v", g.Name, err)
	}
}

// Run drives one full hand-over: the caller has already withdrawn the load into the pack
// (RowsInBank/Drawn describe that withdrawal). Walks to o.Stand, requests o.Mule, offers
// each planned group, accepts both screens, repeats while the pack still holds haul, and
// returns once nothing is left, the mule never showed (GaveUp), or the caller's own Delay
// asked to stop (Stopped).
// Run drives one hand-over, wrapping runHandover with the two world-hop points: reach the
// mule's world first, and come home afterwards. Both no-op unless Deps carries the hop hooks
// (worldhop.go), so a caller that has not wired worlds up calls runHandover and nothing else.
//
// The wrapper exists because runHandover has fourteen return sites and the hop home has to
// happen after every one of them; adding it to each would have been fourteen chances to miss
// one.
func Run(d *Deps, o Options) Result {
	// Before anything is walked or drawn: if the mule is on another world, this account
	// cannot meet it at all, however well the rest of this function works. Ask to move and
	// stop — the session is about to close and this script is about to be restarted on the
	// other world. No-ops on a fleet with one world, which is every caller that has not
	// wired the hooks up.
	if hopping, reason := d.hopToMule(o.Mule); hopping {
		return Result{Stopped: true, Hopping: true, Reason: reason}
	}
	if n, full := muleCoinsFull(o.Mule, o.CoinsName); full {
		d.logf("donor: %s holds %d coins (cap %d): this hand-off keeps its coins", o.Mule, n, MuleCoinCap)
	}

	res := runHandover(d, o)

	// However that ended — traded, gave up, or stopped — an account sitting on somebody
	// else's world has to come back to its own, where its bank and its skilling spot are.
	// Deliberately not conditional on res.Traded: an account that failed to trade is exactly
	// the one that must not be left stranded.
	if res.Stopped && !res.Hopping {
		// The caller asked to stop. Do not start a hop it cannot observe.
		return res
	}
	if hopping, reason := d.hopHome(); hopping {
		res.Stopped, res.Hopping = true, true
		if res.Reason == "" {
			res.Reason = reason
		} else {
			res.Reason = res.Reason + "; " + reason
		}
	}
	return res
}

func runHandover(d *Deps, o Options) Result {
	now := time.Now()
	h := donorplan.Begin(now)
	switch h.Loaded(o.RowsInBank, o.Drawn, now) {
	case donorplan.Finished:
		return Result{Reason: "nothing in the bank for the mule"}
	case donorplan.Paused:
		return Result{Reason: "the load did not fit the pack"}
	}
	// Refuse to set off with a load no trade screen can carry. See emptyPlanReason. The skip
	// set is the caller's KnownUntradable, the same one the trade rounds below start from: a
	// load made only of those is exactly as unofferable as one that does not fit.
	if why := emptyPlanReason(d, o, o.KnownUntradable); why != "" {
		d.logf("donor: not meeting %q: %s", o.Mule, why)
		return Result{GaveUp: true, Reason: why}
	}
	// Announce only once nearly there. Owner 2026-09-19: a Fishing Guild donor calling from
	// the Guild would pin the mule at its post, not cooking, for the whole walk over.
	finishAnnounce := func() {}
	announced := false
	// RESERVE A PLACE FIRST: tell the mule a load is on its way before the walk starts, so the
	// queue depth it publishes counts this donor while it walks (see mulebus.KindReserve). It
	// does not make the mule wait for anybody -- the announcement on arrival still does that --
	// and whichever way this run ends, the same KindCancel takes the reservation back.
	reserved := reserveHandoff(d, o)
	defer func() {
		if announced {
			finishAnnounce()
		} else if reserved {
			cancelHandoff(d, o)
		}
	}()

	// The mule's own answers to this hand-off, heard on the bus: KindComing (it is travelling
	// here, with an ETA) and KindDeclined (it is not coming at all). A donor that hears neither
	// — every donor today, and every donor of a mule with travelling switched off — decides
	// exactly as it did before: donorplan's own timeouts, untouched.
	var busWatch *mulebus.Watcher
	if d.Bus != nil && d.Me != "" && o.Mule != "" {
		busWatch = mulebus.NewWatcher()
	}
	var comingUntil *time.Time
	comingLogged, declined := false, false
	var nextCollect time.Time

	trade := actions.NewTrade(d.Session)
	// skip holds item ids the trade screen refused this hand-over (an untradeable random-event lamp,
	// say); they stay in the pack instead of stalling every trade round on an offer that never lands.
	// Seeded from o.KnownUntradable (already-discovered offenders from a PAST hand-off, if the
	// caller tracks one) so a repeat offender is excluded from round one instead of being
	// rediscovered at the cost of another maxOfferTries offers — see that field's own doc comment.
	skip := map[int]bool{}
	for id := range o.KnownUntradable {
		skip[id] = true
	}
	var res Result
	// standUnreachable counts consecutive MeetWalk rounds in a row where walkNear failed to
	// reach o.Stand at all — see standUnreachableBeforeCrossing's own doc comment for why
	// this is what finally triggers crossWaterTo instead of reacting to the first failure.
	standUnreachable := 0
	walkOverrunLogged := false
	muleUnreachable := 0
	for {
		view := donorplan.MeetView{Now: time.Now(), Stand: o.Stand, TradeOpen: trade.Active()}
		if here, ok := currentTile(d); ok {
			t := here
			view.Me = &t
			// TELL THE MULE AS SOON AS THE HAND-OFF STARTS, not on arrival.
			//
			// This used to wait until the donor was within announceWithin (20 tiles) of the
			// stand, on the reasoning that ten seconds is "enough for the mule to stop a cook
			// round and step back from the range beside the bank". Winding down a cook round
			// is not a step back: cook.go answers a yield with comeBackAndBank, which crosses
			// the Catherby door, walks to the bank, opens it and deposits. Ten seconds does
			// not cover that, and the donor's own wait is about 77 seconds.
			//
			// Measured on one 72-minute uninterrupted run (2026-09-22): 225 hand-off loads
			// drawn across 64 accounts, 21 delivered across 19; 204 donors logged "giving up
			// waiting for the mule at 2809,3441,0"; the mule yielded to an incoming trade 11
			// times against 29 cook rounds. 46 of 64 accounts drew a load -- walking to the
			// bank, withdrawing ~20 rows as notes, walking to the stand and carrying it all
			// back -- an average of 3.5 times each, and never delivered once.
			//
			// Announcing at the start of the walk hands the mule the whole journey (tens of
			// seconds, often more) instead of ten. The cost is bounded and lands in the right
			// place: a donor that announces and then dies on the way holds the mule at its own
			// post for incomingTTL (6 minutes), and its post is exactly where a mule with a
			// queue should be. The ordinary end still cancels immediately via finishAnnounce.
			// ANNOUNCE ON ARRIVAL, NOT ON SETTING OUT. Between 23:20 and 23:50 on
			// 2026-09-22 this announced as soon as the load was drawn, to give the mule the
			// whole walk to wind a cook round down. That was the wrong half of the problem
			// and the fleet owner caught it: "the donors dont even get to catherby bank
			// before they request the mule". A donor announcing from the Seers bank puts the
			// mule on standby for the 84 tiles it then walks, and a mule standing idle for a
			// donor who is not there is the same lost throughput as a donor waiting for a
			// mule who is not there.
			//
			// The real missing piece was never the timing of the announcement -- it was that
			// nothing ANSWERED it, so the donor fell back to donorplan.MeetTimeout (1 minute)
			// and left. internal/bot/mule.go now replies with a KindComing carrying a queue
			// eta, which switches the donor to MeetTimeoutComing (10 minutes) and makes it
			// queue instead. Arrive, ask, wait your turn -- which is what users asked for.
			if !announced && t.Level == o.Stand.Level && tile.Chebyshev(t, o.Stand) <= announceWithin {
				finishAnnounce = announceHandoff(d, o)
				announced = true
			}
			// publishCollect stays gated on actually being near the stand, and that gate is
			// NOT the same question. It exists so a mule that travels to its donors is never
			// summoned to wherever one happens to be standing mid-walk -- see its own doc
			// comment. Only the "I am coming" announcement moved.
			nearStand := t.Level == o.Stand.Level && tile.Chebyshev(t, o.Stand) <= announceWithin
			// Where this hand-off is waiting, repeated while it waits: a mule that travels to
			// its donors has no other way to learn where they are. Gated on `announced` for the
			// same reason the announcement is — a donor still walking over must not summon a
			// mule to wherever it happens to be standing on the way.
			if nearStand && busWatch != nil && time.Now().After(nextCollect) {
				publishCollect(d, o, t)
				nextCollect = time.Now().Add(mulebus.CollectInterval)
			}
		}
		view.Mule = findMule(d, o.Mule)
		if busWatch != nil {
			for _, msg := range busWatch.Poll(d.Bus) {
				if !mulebus.NamesMatch(msg.From, o.Mule) || !mulebus.AddressedTo(msg, d.Me) {
					continue
				}
				switch msg.Kind {
				case mulebus.KindComing:
					until := time.Now().Add(time.Duration(msg.EtaMs)*time.Millisecond + comingGrace)
					comingUntil = &until
					if !comingLogged {
						comingLogged = true
						d.logf("donor: %q says it is on its way here by %s, about %s out — waiting for it rather than giving up", msg.From, msg.Via, (time.Duration(msg.EtaMs) * time.Millisecond).Round(time.Second))
					}
				case mulebus.KindDeclined:
					declined = true
					d.logf("donor: %q will not come: %s", msg.From, msg.Reason)
				}
			}
		}
		view.WaitUntil, view.Declined = comingUntil, declined

		action := h.Meet(view)
		switch action {
		case donorplan.MeetWalk:
			walked := walkNearOutcome(d, o.Stand, donorplan.StandRadius)
			standUnreachable = nextStandUnreachable(standUnreachable, walked)
			if walked == walkReached {
				continue
			}
			if walked == walkTimedOut && !walkOverrunLogged {
				// Said once per hand-off, with where it stopped. A road the pack routes is not
				// a stranding however long it takes, so this never feeds the water check —
				// but a real overrun is worth seeing, and it used to hide behind a misleading
				// "stranded across water" line three walks later.
				walkOverrunLogged = true
				if here, ok := currentTile(d); ok {
					d.logf("donor: the walk to the mule stand at %d,%d,%d ran out of time at %d,%d,%d (%d tiles away) — retrying",
						o.Stand.X, o.Stand.Z, o.Stand.Level, here.X, here.Z, here.Level, tile.Chebyshev(here, o.Stand))
				}
			}
			// Found live on a real production account ("acct", 2026-09-16): o.Stand
			// (the account's own configured muleAtTile) sat across a genuine water gap
			// from wherever the donor was displaced to, and walkNear's plain same-level
			// FindPath can never cross that — it just re-fails "unreachable" instantly
			// every round forever (see this file's own package doc comment on the
			// "jagex" zero-delay-loop incident this streak counter's sibling fix
			// already closed for the busy-spin case; this is the same shape one level
			// up — the loop no longer spins, but it never MAKES PROGRESS either).
			// Ground-truthed against the real collision pack (cmd/doordiag-style
			// FindPath call): a stuck tile on the Lumbridge/mainland side and o.Stand's
			// own Catherby-area tile both report "unreachable" with the same ~216k
			// expanded-node signature as searching the entire mainland component,
			// while a tile already past the second ship (e.g. 2683,3271,0) reaches
			// o.Stand in a couple thousand nodes — confirming the boat-staged route
			// below actually finishes the trip once triggered.
			if standUnreachable >= standUnreachableBeforeCrossing {
				if here, ok := currentTile(d); ok && strandedAcrossWater(here, o.Stand) {
					d.logf("donor: stranded across water heading for the mule stand at %d,%d,%d from %d,%d,%d — falling back to freshstartrunner's pickpocket+boat route",
						o.Stand.X, o.Stand.Z, o.Stand.Level, here.X, here.Z, here.Level)
					arrived, stopped := crossWaterTo(d, o.Stand)
					if arrived {
						standUnreachable = 0
						continue
					}
					if stopped {
						// A stop request mid-crossing is a stop, not a failed crossing: the
						// caller must not apply its retry cooldown to a plain Stop().
						res.Stopped = true
						return res
					}
					d.logf("donor: could not cross back to the mule stand at %d,%d,%d — giving up this hand-over", o.Stand.X, o.Stand.Z, o.Stand.Level)
					res.GaveUp = true
					res.Reason = "could not reach the mule stand across the water"
					return res
				}
				// Not a water case (a different level, or a genuinely disconnected
				// pocket that isn't across water at all) — this package invents no
				// fix for that, same as ReturnToCatherby's own doc comment. Reset so
				// the ordinary 2s retry pace below doesn't re-run this check every
				// single round for nothing.
				standUnreachable = 0
			}
			// Found live: a zero-delay `continue` here span-looped at full CPU —
			// FindPath failing instantly (e.g. "start not walkable") re-entered
			// Meet -> MeetWalk -> walkNear -> fail on every iteration with no wait
			// at all, 12M+ identical log lines in under 5 minutes on a real
			// production account ("acct", 2026-09-14). walkNear's own internal
			// loop already delays between real walk attempts; this only covers
			// the case where it fails before ever calling Delay once.
			if !d.Delay(2 * time.Second) {
				res.Stopped = true
				return res
			}
			continue
		case donorplan.MeetApproach:
			if view.Mule == nil {
				if !d.Delay(2 * time.Second) {
					res.Stopped = true
					return res
				}
				continue
			}
			if walkNear(d, *view.Mule, donorplan.MeetRange) {
				muleUnreachable = 0
				continue
			}
			// Measured live 2026-09-21 on the 269-account fleet: 19 "donor: no path to
			// 2817,3443,0: unreachable" lines in 42 minutes, across six of fifteen
			// hand-offs. The cause is not a navigation bug — the mule leaves its post at
			// 2809,3441 to cook at the Catherby range and stands INSIDE the building,
			// on a tile the donor genuinely cannot path to. Every one of those six
			// hand-offs then completed normally once the mule came back.
			//
			// So the retry is correct and the pathfinding call in it is not: the donor
			// is already standing at the agreed post (it walked there in MeetWalk), the
			// mule is coming back to that same post, and re-running FindPath against an
			// unreachable interior tile every two seconds buys nothing. It costs a
			// failed A* per donor per tick and writes a line that reads, to anyone
			// watching the log, exactly like a real routing failure.
			//
			// After the first failure, wait instead — but only while the donor really is
			// in position at the post. If it is not, keep trying, because then the walk
			// genuinely does have work to do.
			muleUnreachable++
			if muleUnreachable == 1 {
				d.logf("donor: %q is not reachable from the post right now (it is probably away from it) — waiting at %d,%d,%d",
					o.Mule, o.Stand.X, o.Stand.Z, o.Stand.Level)
			}
			if here, ok := currentTile(d); ok && atPost(here, o.Stand) {
				if !d.Delay(2 * time.Second) {
					res.Stopped = true
					return res
				}
				continue
			}
			if !d.Delay(2 * time.Second) {
				res.Stopped = true
				return res
			}
			continue
		case donorplan.MeetWait:
			if !d.Delay(700 * time.Millisecond) {
				res.Stopped = true
				return res
			}
			continue
		case donorplan.MeetRequest:
			if slot, ok := findMuleSlot(d, o.Mule); ok {
				if err := trade.Request(slot); err != nil {
					d.logf("donor: requesting a trade with %q: %v", o.Mule, err)
				}
			} else {
				d.logf("donor: %q is nearby but not resolvable to a player index yet", o.Mule)
			}
			if !d.Delay(300 * time.Millisecond) {
				res.Stopped = true
				return res
			}
			continue
		case donorplan.MeetWaitReply:
			if !d.Delay(400 * time.Millisecond) {
				res.Stopped = true
				return res
			}
			continue
		case donorplan.MeetGiveUp:
			res.GaveUp = true
			if h.TooManyFailedTrades() {
				res.Reason = fmt.Sprintf("%d trades in a row with %q closed with nothing crossing", h.FailedTrades, o.Mule)
				d.logf("donor: giving up on %q: %s", o.Mule, res.Reason)
				return res
			}
			res.Reason = "the mule never showed up at the meet spot"
			d.logf("donor: giving up waiting for %q at %d,%d,%d", o.Mule, o.Stand.X, o.Stand.Z, o.Stand.Level)
			return res
		case donorplan.MeetTrading:
			// fall through into the trade loop below.
		}

		closed, traded, slots, stopped, why := driveTrade(d, &h, trade, o, skip)
		if stopped {
			res.Stopped = true
			return res
		}
		if !closed {
			continue
		}
		if traded {
			res.Traded = true
			res.Reason = ""
		} else if why != "" {
			// Without this the caller's own non-traded branch printed an empty
			// parenthesis — see driveTrade's doc comment. A later round that
			// succeeds clears it again just above, so a Result never carries the
			// reason an earlier round failed for.
			res.Reason = why
		}
		_ = slots
		res.Loads, res.Slots = h.Loads, h.Slots
		if h.Step != donorplan.StepMeet {
			// Nothing left in the pack for this trip — done (see Handover.Traded's own
			// doc comment: StepLoad means the next hand-over cycle would draw a fresh
			// load from the bank, which is a new Run call, not this one continuing).
			return res
		}
		// What is left must still be something a trade screen can carry, or the next round
		// is the same empty trade again. See emptyPlanReason.
		if why := emptyPlanReason(d, o, skip); why != "" {
			d.logf("donor: not meeting %q again: %s", o.Mule, why)
			res.GaveUp = !res.Traded
			res.Reason = why
			return res
		}
		// Haul is still in the pack (the load didn't fit one trade screen) — loop back
		// into Meet for another round with the same mule at the same spot.
	}
}

// driveTrade drives an already-open (or about to open) trade screen through
// donorplan.DecideTrade until it closes, times out, or the caller's Delay asks to stop.
// Returns closed (the screen is shut, one way or another), traded/slots (from
// donorplan.TradeEnded, only meaningful when closed), stopped, and — when it closed
// without anything crossing — a reason naming what this side actually saw. That reason
// exists because every donor script in the fleet logs Result.Reason on its non-traded
// branch (fisher.go's "hand-off ended without trading (%s)" and the rest), and this
// function's own close path left it empty, so the live log said literally
// "hand-off ended without trading ()" for the single most common failure there is.
func driveTrade(d *Deps, h *donorplan.Handover, trade *actions.Trade, o Options, skip map[int]bool) (closed, traded bool, slots int, stopped bool, reason string) {
	// Pack slots holding something the trade screen refused never count as haul, or a hand-over
	// would keep meeting the mule for them.
	haulLeft := func() int {
		if o.HaulLeft == nil {
			return 0
		}
		return o.HaulLeft() - skippedSlots(d, skip)
	}
	haulBefore := haulLeft()
	chatCursor := 0
	if d.Chat != nil {
		chatCursor = d.Chat.Total()
	}
	offerTries := map[int]int{}
	// Snapshotted once, not recomputed every tick: o.Load()/effectiveLoad read the donor's
	// live pack, which empties as items are offered (offered items leave the visible pack
	// for the trade box). Found live: recomputing every tick let the plan drift mid-trade —
	// DecideTrade would occasionally see a "new" unoffered group appear after the donor had
	// already accepted (accepted never resets except on a screen change), so it returned
	// TradeOffer again while driveTrade's local accepted stayed true forever — the donor sat
	// on TradeWait believing it had accepted while the real trade never reached the confirm
	// screen. This was the root cause of production hand-offs from ProgressiveFisher donors
	// (e.g. "lost") looping "closed without anything crossing" against the mule indefinitely.
	plan := withoutSkipped(effectiveLoad(o), skip)
	accepted := false
	lastScreen := donorplan.ScreenNone
	// lastOffered/lastAcceptedAt: under-logged compared to mulerunner's own side —
	// live investigation 2026-09-18 (see COLLAB.md's mule trade investigation entry)
	// found this loop only ever logs on TradeDecline or an Accept() error, never on
	// a successful accept/confirm click or a screen transition, so when a trade
	// closed from underneath the mule with no matching "donor: declining trade"
	// line, there was no way to tell from this side whether the donor had even
	// reached/accepted the confirm screen yet. lastOffered lets the close-path log
	// what state the donor itself was actually in at the moment it noticed the close.
	lastOffered := 0
	for {
		if !trade.Active() {
			reallyClosed, stopped := stillClosed(d, trade)
			if stopped {
				return false, false, 0, true, ""
			}
			if !reallyClosed {
				d.logf("donor: trade interface with %q briefly went inactive but the trade is still open — continuing", o.Mule)
				continue
			}
			after := haulLeft()
			tr, sl := donorplan.TradeEnded(haulBefore, after)
			// Offered items temporarily leave the pack. A cancel can close before
			// its return inventory arrives, so that transient drop is not a transfer.
			if !serverAcceptedTrade(d.Chat, chatCursor) {
				tr, sl = false, 0
			}
			if !tr {
				// Nothing crossed, so the whole offered load is coming back — but
				// it is still inside the (now closed) trade box at this instant, so
				// haulLeft() reads far too low, usually 0. Handover.Traded reads
				// that 0 as "nothing left for the mule" and moves to StepLoad, which
				// makes Run return and the calling script walk away from the bank
				// with its entire load intact. Wait for the returned inventory first.
				after = waitForReturnedHaul(d, haulLeft, haulBefore)
			}
			h.Traded(sl, after, time.Now())
			if tr {
				d.logf("donor: trade closed — %d slot(s) crossed to %q", sl, o.Mule)
				return true, tr, sl, false, ""
			}
			why := closeReason(lastScreen, lastOffered, accepted, len(plan), len(skip))
			d.logf("donor: trade with %q closed without anything crossing (last seen: screen=%v offered=%d accepted=%v) — %s",
				o.Mule, lastScreen, lastOffered, accepted, why)
			return true, false, sl, false, why
		}
		if h.TradeExpired(time.Now()) {
			_ = trade.Decline(3 * time.Second)
			h.Failed(time.Now())
			d.logf("donor: trade with %q timed out on the screen — declining", o.Mule)
			return true, false, 0, false, "the trade screen timed out before the mule confirmed"
		}

		screen := donorplan.ScreenOffer
		if trade.OnConfirmScreen() {
			screen = donorplan.ScreenConfirm
		}
		if screen != lastScreen {
			if screen == donorplan.ScreenConfirm {
				d.logf("donor: trade with %q reached the confirm screen", o.Mule)
			}
			accepted, lastScreen = false, screen
		}

		mineItems, theirItems := trade.MyOffer(), trade.TheirOffer()
		if screen == donorplan.ScreenConfirm {
			// The offer inventories can be stopped/cleared when confirmation
			// opens. Read the confirmation screen's own inventories instead.
			mineItems, theirItems = trade.ConfirmOffers()
			if len(mineItems) == 0 {
				if !d.Delay(400 * time.Millisecond) {
					return false, false, 0, true, ""
				}
				continue // packets may arrive after the interface-open packet
			}
		}
		var offered, theirs []donorplan.ItemRow
		for _, it := range mineItems {
			offered = append(offered, donorplan.ItemRow{ID: int(it.ID), Count: int(it.Count)})
		}
		for _, it := range theirItems {
			theirs = append(theirs, donorplan.ItemRow{ID: int(it.ID), Count: int(it.Count)})
		}
		lastOffered = len(offered)
		view := donorplan.TradeView{
			Screen: screen, Partner: trade.Partner(), Mule: o.Mule,
			Load: plan, Offered: namedRows(offered, plan), TheirOffer: theirs,
			Accepted: accepted,
		}
		action, group := donorplan.DecideTrade(view)
		switch action {
		case donorplan.TradeDecline:
			d.logf("donor: declining trade with %q (partner=%q offered=%d theirs=%d screen=%v accepted=%v)",
				o.Mule, view.Partner, len(view.Offered), len(view.TheirOffer), screen, accepted)
			_ = trade.Decline(3 * time.Second)
		case donorplan.TradeOffer:
			offerTries[group.ID]++
			if offerTries[group.ID] > maxOfferTries {
				skip[group.ID] = true
				if o.KnownUntradable != nil {
					o.KnownUntradable[group.ID] = true
				}
				plan = withoutSkipped(plan, skip)
				d.logf("donor: %s never reached the trade screen after %d offers (untradeable?) — leaving it in the pack and remembering not to offer it again", group.Name, maxOfferTries)
				continue
			}
			offerGroup(d, trade, group, o)
			// AND EVERYTHING ELSE STILL OFF THE SCREEN, IN ONE BURST.
			//
			// DecideTrade hands back the first group that is not on the trade screen yet, so
			// this loop used to place exactly one stack per pass and then settle: 700ms here
			// plus 400ms at the bottom, 1.1 SECONDS PER STACK. A live load is 18-23 stacks, so
			// the donor spent 20-25 seconds drawing its offer -- and the mule trades one
			// partner at a time, so every second of that is the whole fleet's hand-off
			// capacity standing still. Measured 2026-09-23: 27 deliveries against 105 loads
			// drawn in 43 minutes, a median cycle of 30s of which 20s was this.
			//
			// The offers are ordinary packets and OfferAll does not wait for anything, so
			// there is nothing to serialise them for. Sending them back to back with a short
			// gap costs the same packets in a fraction of the time, and the settle below then
			// waits ONCE for all of them instead of once each.
			//
			// Nothing is weakened: the loop still re-reads the real trade screen next pass and
			// DecideTrade still names anything that did not land, so a dropped offer is
			// retried exactly as before and maxOfferTries still ends an untradeable one. The
			// coins group is deliberately left out -- it goes through Trade.Offer with a
			// five-second wait inside it, and bursting those would serialise the burst.
			for _, g := range plan {
				if g.ID == group.ID || skip[g.ID] || onScreen(offered, g) {
					continue
				}
				if o.Reserve > 0 && o.CoinsName != "" && strings.EqualFold(g.Name, o.CoinsName) {
					continue
				}
				if offerTries[g.ID] >= maxOfferTries {
					continue // let the ordinary path above name it and remember it
				}
				offerTries[g.ID]++
				if !d.Delay(offerBurstGap) {
					return false, false, 0, true, ""
				}
				offerGroup(d, trade, g, o)
			}
			if !d.Delay(700 * time.Millisecond) {
				return false, false, 0, true, ""
			}
			continue
		case donorplan.TradeAccept, donorplan.TradeConfirm:
			if err := trade.Accept(); err != nil {
				d.logf("donor: accept: %v", err)
			} else {
				accepted = true
				d.logf("donor: accepted trade with %q (screen=%v offered=%d stack(s))", o.Mule, screen, len(offered))
			}
		case donorplan.TradeWait, donorplan.TradeClosed:
			// nothing to do this tick
		}
		if !d.Delay(400 * time.Millisecond) {
			return false, false, 0, true, ""
		}
	}
}

// closeDebouncePolls/closeDebounceEvery: how long a single false trade.Active() reading
// is given to reverse itself before the donor believes the trade really closed (20 x
// 100ms = 2s of real time; expressed as a poll count so a test's instant Delay does not
// spin on the wall clock).
//
// Live-found 2026-09-18, and the root cause of the two-sided handshake desync that held
// the mule hand-over success rate at roughly 1-in-9. The offer->confirm transition is
// two server packets: the offer interface closes (IF_CLOSE, which state.go maps to
// MainModalID = NoModal) and the confirm interface opens (IF_OPENMAIN 3443) up to a game
// tick later. actions.Trade.Active() is a single snapshot of that one field, so any poll
// landing in the gap reads false on a perfectly live trade. This loop polls at 400ms, so
// it landed in the gap most of the time and bailed out of a trade it had already accepted.
//
// Real evidence from run/fleet.log (donor "acct" and the mule, the same trade):
//
//	19:53:37 [lost]   donor: accepted trade with "mule" (screen=1 offered=13 stack(s))
//	19:53:38 [mule] offer changed — accepting the new settled offer (13 stack(s))
//	19:53:38 [mule] confirming: giving 0 stack(s), receiving 13 stack(s)
//	19:53:38 [lost]   donor: trade with "mule" closed without anything crossing
//	                        (last seen: screen=1 offered=13 accepted=true)
//	19:53:44 [mule] timed out waiting for received trade inventory
//
// the mule presenting a confirm screen for that trade in the same second is proof the trade
// was still live when the donor declared it closed — a closed trade cannot advance to
// confirm. The successful trades in the same window (spare102 19:54:05-08, Acct8
// 19:55:09-12) differ only in that the donor's poll happened to land AFTER the confirm
// interface opened, and they log "reached the confirm screen" instead. the mule's own side
// of three of the failures also logs "trade interface briefly went inactive but X's trade
// is still open — continuing" — the debounce cbea997 gave the mule, catching the very same
// flicker at a 250ms poll. This is the donor half of that fix.
const (
	closeDebouncePolls = 20
	closeDebounceEvery = 100 * time.Millisecond
)

// stillClosed re-polls trade.Active() after the caller saw it report false, returning
// closed=false the moment it reports true again (a flicker, not a genuine close) and
// closed=true only once the whole debounce window has passed without that happening.
// stopped reports that d.Delay asked to stop, which the caller must not confuse with
// either outcome.
func stillClosed(d *Deps, trade *actions.Trade) (closed, stopped bool) {
	for i := 0; i < closeDebouncePolls; i++ {
		if !d.Delay(closeDebounceEvery) {
			return false, true
		}
		if trade.Active() {
			return false, false
		}
	}
	return true, false
}

// haulReturnPolls/haulReturnEvery bound waitForReturnedHaul (15 x 200ms = 3s of real time).
const (
	haulReturnPolls = 15
	haulReturnEvery = 200 * time.Millisecond
)

// waitForReturnedHaul re-reads haulLeft until the load the closed trade screen was holding
// is back in the pack, returning the largest count it saw. Offered stacks live in the trade
// box, not the pack, so the instant a trade closes with nothing crossed haulLeft() reads far
// too low; the engine returns them a tick or two later.
//
// Live-found alongside the flicker above, and what made each flicker cost a whole hand-over
// rather than one retry: donor "acct" logged "closed without anything crossing" at 19:53:38
// and, in the very same second, "fisher: hand-off ended without trading ()" followed by a
// walk back to the fishing spot — because with all 13 stacks still in the trade box
// haulLeft() was 0, so Handover.Traded took the StepLoad branch ("nothing left to hand
// over") instead of StepMeet, and Run returned rather than meeting the mule again.
func waitForReturnedHaul(d *Deps, haulLeft func() int, want int) int {
	best := haulLeft()
	for i := 0; i < haulReturnPolls && best < want; i++ {
		if !d.Delay(haulReturnEvery) {
			return best
		}
		if n := haulLeft(); n > best {
			best = n
		}
	}
	return best
}

// Only the server's exact system message is completion proof; public chat or
// a stale accepted message from a prior trade must not count.
func serverAcceptedTrade(chat *chatlog.Log, cursor int) bool {
	if chat == nil {
		return false
	}
	lines, _ := chat.ReadAfter(cursor, time.Time{})
	for _, line := range lines {
		if strings.TrimSpace(line) == "Accepted trade." {
			return true
		}
	}
	return false
}

// namedRows re-attaches item names to the trade screen's own SlotItem reads (ID/Count only
// — the game never sends a name over the wire) by matching against the planned load, the
// same way DecideTrade's own offered() check only ever needs Name+ID for. Anything on the
// offer screen that isn't part of the plan (should not happen — the donor never offers
// anything it didn't plan) is passed through with an empty name, which simply never matches
// offered() and does not block the loop.
func namedRows(rows []donorplan.ItemRow, plan []donorplan.LoadGroup) []donorplan.ItemRow {
	if len(rows) == 0 {
		return rows
	}
	byID := map[int]string{}
	for _, g := range plan {
		byID[g.ID] = g.Name
	}
	out := make([]donorplan.ItemRow, len(rows))
	for i, r := range rows {
		r.Name = byID[r.ID]
		out[i] = r
	}
	return out
}

// offerBurstGap spaces the offers inside one burst. Small enough that a whole load reaches the
// trade screen in a few seconds rather than half a minute, large enough that the account is not
// firing twenty packets inside one 600ms game tick. At 120ms a 23-stack load takes about 2.6s
// against the 25s it used to.
const offerBurstGap = 120 * time.Millisecond

// onScreen reports whether a load group is already on the trade screen, matching donorplan's own
// rule (an id match, or a name match when either side has no id).
func onScreen(rows []donorplan.ItemRow, g donorplan.LoadGroup) bool {
	for _, r := range rows {
		if r.Count > 0 && r.ID != 0 && g.ID != 0 && r.ID == g.ID {
			return true
		}
	}
	return false
}

// maxOfferTries is how many times a load group is offered before the trade goes on without it.
// Found live on the local soak: a Genie lamp the fisher's bank refused was drawn into the load, its
// offer never reached the trade screen, and DecideTrade kept asking to offer it until the 60-second
// trade budget ran out and the donor declined, with the fish already offered never crossing.
const maxOfferTries = 4

// withoutSkipped is plan with the groups whose item ids are in skip left out.
func withoutSkipped(plan []donorplan.LoadGroup, skip map[int]bool) []donorplan.LoadGroup {
	if len(skip) == 0 {
		return plan
	}
	out := make([]donorplan.LoadGroup, 0, len(plan))
	for _, g := range plan {
		if !skip[g.ID] {
			out = append(out, g)
		}
	}
	return out
}

// skippedSlots counts the pack slots holding an item id in skip.
//
// Reads the fixed PlayerPack component rather than the sidebar-tab view for the reason
// heldOf's own doc comment sets out: content nulls tabs 3 and 4 for the whole of a mime and for
// part of every ship crossing, and the tab view then reports an empty pack instead of an
// unreadable one. Here that would silently overstate haulLeft.
func skippedSlots(d *Deps, skip map[int]bool) int {
	if len(skip) == 0 {
		return 0
	}
	n := 0
	for _, it := range packItems(d) {
		if skip[int(it.ID)] {
			n++
		}
	}
	return n
}

// announceWithin is how close to the meet stand a donor must be before it tells the mule it is
// coming: about ten seconds' walk, enough for the mule to stop a cook round and step back from
// the range beside the bank.
const announceWithin = 20

// comingGrace is added to a mule's published ETA before the donor stops believing it: the ETA is
// the mule's own walk budget, and arriving, being noticed and opening a trade all happen after
// the walk itself ends.
const comingGrace = 30 * time.Second

// publishCollect tells the mule where this hand-off is standing, so a mule that travels to its
// donors knows where "here" is. This is the message MuleReceiver's own pickup queue was built to
// consume and that nothing in this repo ever sent.
//
// Repeated by the caller every mulebus.CollectInterval on purpose: the mule's queue expires a
// request mulebus.CollectTTL (20s) after the last one, so a donor that walked off, finished or
// gave up drops out of the batch by itself and a mule can never travel to an audience that has
// already left. Membership in a batch is therefore defined by a live heartbeat, not by a message
// somebody sent once.
func publishCollect(d *Deps, o Options, at tile.Tile) {
	slots := o.Drawn
	if o.HaulLeft != nil {
		if left := o.HaulLeft(); left > 0 {
			slots = left
		}
	}
	d.Bus.Publish(mulebus.CollectMessage(d.Me, at, slots, o.Mule))
}

// reserveHandoff tells the donor's mule over the bus that a load has been drawn for it and is on
// its way (KindReserve), reporting whether it did. A no-op without a bus.
func reserveHandoff(d *Deps, o Options) bool {
	if d.Bus == nil || d.Me == "" || o.Mule == "" {
		return false
	}
	d.Bus.Publish(mulebus.ReserveMessage(d.Me, o.Mule))
	return true
}

// cancelHandoff tells the donor's mule this hand-off is over (KindCancel).
func cancelHandoff(d *Deps, o Options) {
	done := mulebus.CancelMessage(d.Me)
	done.To = o.Mule
	d.Bus.Publish(done)
}

// muleQueueFresh is how old a heartbeat MuleQueue still believes. A minute, not a few heartbeat
// intervals: the mule now heartbeats from every wait, a trade screen included (mule.go busTick),
// but a mule held up somewhere busTick does not reach -- a random event, a long walk home -- goes
// quiet for longer, and a queue depth a minute old is still a far better guide to the next few
// minutes than none.
const muleQueueFresh = time.Minute

// MuleQueue reads the mule's own last heartbeat off the bus: how many donors it already expects
// and what one hand-off costs it. ok is false with no recent heartbeat, or one from a mule too
// old to carry the queue, which a caller treats as "unknown" -- never as an empty queue.
func MuleQueue(bus *mulebus.Bus, mule string, now time.Time) (queue int, cycle time.Duration, ok bool) {
	if bus == nil || mule == "" {
		return 0, 0, false
	}
	msg, found := bus.Latest(mulebus.KindMuleHere, mule, now.Add(-muleQueueFresh))
	if !found || msg.CycleMs <= 0 {
		return 0, 0, false
	}
	return msg.Queue, time.Duration(msg.CycleMs) * time.Millisecond, true
}

// announceHandoff tells the donor's mule over the bus that a hand-off is starting (KindReady,
// with the slots drawn) and returns the call that says it is over (KindCancel). Both are
// addressed to o.Mule so a second mule on the same bus ignores them. A no-op without a bus.
func announceHandoff(d *Deps, o Options) func() {
	if d.Bus == nil || d.Me == "" || o.Mule == "" {
		return func() {}
	}
	ready := mulebus.ReadyMessage(d.Me, o.Drawn)
	ready.To = o.Mule
	d.Bus.Publish(ready)
	return func() { cancelHandoff(d, o) }
}
