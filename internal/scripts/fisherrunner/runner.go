// Package fisherrunner is the I/O half of the ProgressiveFisher port: it drives the pure
// rungs/fish rules in internal/scripts/fisherplan against a live session, the way
// cmd/fishtest proved live first for the small-net rung. It is shared by cmd/fishtest (a
// linear standalone tool) and internal/bot's registered "ProgressiveFisher" script, so the
// exact live-proven sequencing — buy the tool, find a fishing spot by NPC name AND the ops
// it offers, fish it, bank the catch — lives in exactly one place instead of being
// re-derived or drifting between the two, mirroring internal/scripts/barbarianrunner's own
// extraction for BarbarianAgility.
//
// Every blocking wait goes through a caller-supplied Delayer instead of time.Sleep
// directly, so a cancellable caller (internal/bot's Context.Delay) can make a running bot
// actually stop mid-wait, while a standalone tool with nothing to cancel can just pass
// RealDelay — the same shape barbarianrunner settled on.
package fisherrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// FishingStat is UPDATE_STAT's index for Fishing, per the same PlayerStat enum
// barbarianrunner.AgilityStat cites (engine/src/engine/entity/PlayerStat.ts and the
// client's Skill.ts): attack,defence,strength,hitpoints,ranged,prayer,magic,cooking,
// woodcutting,fletching,fishing,... — fishing is index 10.
const FishingStat = 10

// FishingLevel is the account's live Fishing level, straight off the last UPDATE_STAT for
// FishingStat. Returns 0 if the server has never sent one yet (Skill's own "unknown" zero
// value), which fisherplan.CurrentRung/ActiveRung already treat as "below every rung."
func FishingLevel(d *Deps) int {
	skill, ok := d.Session.State().Skill(FishingStat)
	if !ok {
		return 0
	}
	return skill.Level
}

// PlayerPack is the persistent inventory-tab component id — real, not guessed: confirmed
// live (see COLLAB.md) and matching internal/controlpanel/fake.go's own fakeBackpack
// constant. Needed because gamestate.State.KnownInventories() (the pattern
// internal/randomevents/internal/statlog use for "scan every known inventory component")
// keeps every component this session has EVER seen non-empty, including a shopkeeper's
// own stock (e.g. Harry's, opened once to buy a tool) — Harry's Fishing Shop stocks raw
// fish among its wares, so scanning every known component would read Harry's stock as if
// it were caught fish the moment his shop was opened, well before any real fishing
// happened. Reading this one fixed, always-current component instead avoids that
// contamination entirely.
const PlayerPack = 3214

// Delayer sleeps for d, returning false early (before the full duration elapses) if the
// caller should stop instead of true once d has fully elapsed. Same shape as
// barbarianrunner.Delayer, deliberately — these two runner packages are siblings.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration and never cancels — for a standalone tool
// with no runner to request a stop.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one fish/bank cycle needs from the outside world.
type Deps struct {
	Session actions.Session
	Pack    *pathfind.Pack
	Locs    *locindex.Index
	Config  *cachecfg.Config
	// Components is the parsed interface archive Bank needs to resolve its deposit-side
	// component; nil makes BankFish fail clearly rather than opening a bank it can't
	// actually read.
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	// Log receives one already-formatted line per notable event; nil drops them.
	Log func(string)
	// DriftHome is the reference EmergencyFlee's drift check measures against — where
	// this account is SUPPOSED to be right now. nil means randomevents.CatherbyBankRef,
	// which is correct for every ordinary fishing phase.
	//
	// Live-found 2026-09-18: it is NOT correct while the account is funding. An account
	// short of boat fare is deliberately sent to the Lumbridge pickpocket spot ~430 tiles
	// from Catherby, and with the reference pinned to Catherby the drift check fired
	// continuously — crafter1 logged "[guardian] drifted far from home while waiting
	// (now 3235,3220,0) — recovering immediately" on repeat while standing right next to
	// the spot it had been sent to, with drift-recovery pulling it back toward Catherby
	// the whole time its own fund phase was trying to keep it there. Pointing the
	// reference at wherever the account is meant to be keeps the safety net live for a
	// genuine fail-teleport instead of switching it off (compare scarabrunner, which sets
	// its drift threshold to 0 and gives that protection up entirely).
	DriftHome *pathfind.NavPoint
	// Walker, when set, lets WalkNear route across real cross-region gaps (doors, stairs,
	// ship fares) a plain same-level FindPath can't cross — see WalkNear's own doc comment
	// for the live gap this closes for an account already past its first day. nil (today's
	// exact plain-pathfind behavior) is safe; internal/bot/fisher.go's OnStart sets this
	// from the same live.NewWalker construction its freshDeps already uses.
	Walker *webwalk.Walker
	// Loot receives what opening caskets and oysters really yielded (openables.go); nil
	// records nothing. internal/bot/fisher.go passes bot.Context.Loot.
	Loot func(loot.Drop)
	// Phase, when set, reports a sub-step's phase ("walking" around a walk, "event" while
	// waiting on the random-event guardian) to the control panel's time split and returns the
	// func that restores the previous one. internal/bot/fisher.go wires it to
	// bot.Context.PushPhase; nil reports nothing. Reporting only: it never changes behaviour.
	Phase func(phase string) (restore func())

	// Junk, when set, names what the script throws away (the Big net's low-value catch and the Empty
	// oyster, for BassOysterCaskets): the drop cycle drops it (cycle.go, through junk.go), the Loot
	// panel never records it, and a raw fish it names is no catch. nil, ProgressiveFisher's own
	// setting, throws nothing away.
	Junk func(name string) bool
	// Cycle, when set, is the drop cycle (cycle.go): FishUntil runs it when the pack is nearly full, a
	// walk runs it when the walk is long enough, and the bank runs it before the booth is clicked. It
	// opens caskets and oysters, joins key halves, cuts Oyster pearls into Pearl bolttips and drops
	// Junk. nil, ProgressiveFisher's own setting, does none of it.
	Cycle *Cycle
	// Stall, when set and On, is the run staller (stall.go): a carried Games necklace or Ring of dueling
	// is rubbed every third tick of a run on a walk, and kept through every bank deposit. It is for
	// ProgressiveFisher and BassOysterCaskets alike. nil never rubs.
	Stall *Stall
	// RawDropped counts the raw fish FishUntil dropped as Junk, so a caller comparing raw fish
	// in the pack before and after a session can still tell what was caught.
	RawDropped int

	// Manip, when set and Enabled, makes FishUntil's holds fish with the knife-on-log batch and the lone spot click at
	// nets, big nets, lobster pots and harpoons (fishmanip.go). nil is the plain click.
	Manip *ManipConfig
}

// pushPhase is Deps.Phase, nil-safe.
func (d *Deps) pushPhase(phase string) func() {
	if d.Phase == nil {
		return func() {}
	}
	return d.Phase(phase)
}

func (d *Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

func currentTile(d *Deps) tile.Tile {
	x, z, level, _ := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

// waitFor polls cond every 100ms via d.Delay until it's true or timeout elapses,
// returning false immediately (without logging a timeout) if d.Delay reports a stop was
// requested. Mirrors barbarianrunner's own unexported waitFor.
func waitFor(d *Deps, what string, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(100 * time.Millisecond) {
			return false
		}
	}
	d.logf("timed out waiting for %s", what)
	return false
}

// guardianWaitLogEvery matches barbarianrunner's own identical constant — see its doc
// comment for the real production case (a 2+ minute evade event) this logging change is
// for.
const guardianWaitLogEvery = 10 * time.Second

// emergencyFleeDriftTiles is fisher's own EmergencyFlee drift threshold, unchanged from
// this check's original hardcoded value — see randomevents.EmergencyFlee's own doc comment
// for why every caller now supplies its own home/driftTiles pair instead of this package
// assuming every script operates near Catherby.
const emergencyFleeDriftTiles = 150

// driftHome is d.DriftHome or the Catherby default — see the field's own doc comment.
func driftHome(d *Deps) pathfind.NavPoint {
	if d.DriftHome != nil {
		return *d.DriftHome
	}
	return randomevents.CatherbyBankRef
}

// WaitForGuardian pauses while a random event is detected at all — pending or already
// being handled elsewhere — so the caller never starts a new interaction on top of one.
// Returns false if a stop was requested while waiting. Identical in shape and purpose to
// barbarianrunner.WaitForGuardian, logging included.
//
// Uses randomevents.Busy (cooldown-aware) rather than the raw package-level Detect —
// real live gap found 2026-09-15 on the production fleet (see COLLAB.md): once Guardian
// itself gives up on an event and enters its own 45s cooldown, Detect() still reports it
// present (nothing about the world changed — the item's still there, the NPC's still
// there), so this loop had no way to know Guardian wasn't even trying anymore and could
// block here indefinitely, logging "a random event is present; waiting" every 500ms
// forever. Busy applies the same cooldown filter Guardian's own poll loop uses (while
// still correctly waiting through an in-flight handle — see Busy's own doc comment for
// why that's not the same thing as Guardian.Pending), so once Guardian has genuinely
// given up (for now), this script is free to resume its own work rather than wait on a
// fix that isn't coming this cycle.
func WaitForGuardian(d *Deps) bool {
	return waitForGuardianWith(d, nil)
}

// waitForGuardianWith is WaitForGuardian calling onPoll at every poll of the wait, for
// FishUntil's whirlpool guard: a whirlpool within 3 tiles is itself a random event to the
// Guardian, so a fisher rolling on one would otherwise sit in this wait while the server rolls.
func waitForGuardianWith(d *Deps, onPoll func()) bool {
	randomevents.EmergencyFlee(d.Session, d.Pack, driftHome(d), emergencyFleeDriftTiles, d.Log)
	if !randomevents.Busy(d.Session) {
		return true
	}
	d.logf("[guardian] a random event is present; waiting")
	defer d.pushPhase("event")()
	lastLog := time.Now()
	for randomevents.Busy(d.Session) {
		if onPoll != nil {
			onPoll()
		}
		randomevents.EmergencyFlee(d.Session, d.Pack, driftHome(d), emergencyFleeDriftTiles, d.Log)
		step := 500 * time.Millisecond
		if onPoll != nil {
			step = 200 * time.Millisecond
		}
		if !d.Delay(step) {
			return false
		}
		if time.Since(lastLog) >= guardianWaitLogEvery {
			d.logf("[guardian] still waiting on a random event")
			lastLog = time.Now()
		}
	}
	return true
}

// TeleTo sends ::tele for a tile, using the local engine's level,mapsquareX,mapsquareZ,
// localX,localZ format. Local-engine-only.
func TeleTo(d *Deps, t tile.Tile) error {
	return actions.Cheat(d.Session, fmt.Sprintf("tele %d,%d,%d,%d,%d", t.Level, t.X>>6, t.Z>>6, t.X&0x3f, t.Z&0x3f))
}

// walkNearTimeout bounds WalkNear's entire operation, including issuing the walk and
// waiting for it to complete — not per-segment. Raised from a 30s flat cap 2026-09-15
// after live production data showed it was too short for a genuinely long route, not
// just occasionally unlucky: several accounts independently gave up at the exact same
// real corner of their own computed path (e.g. 2785,3462 — literally waypoint 8 of 21 on
// the Catherby bank route from a common starting tile), then again after a retry, both
// times at real distance-appropriate points, not an off-route drift or a teleport. That
// specific route is ~66 tiles at walking pace (~0.6s/tile) — around 40s, comfortably over
// the old 30s cap on every single attempt, not merely sometimes. See COLLAB.md's
// 2026-09-15 entry for the full live evidence. A longer timeout costs nothing when a
// route finishes faster (the loop returns the moment it's within radius of dest); it
// only matters for genuinely long routes that were being cut off mid-flight before.
// Matches crafterrunner's own walkDeadline (120s), which already carries this identical
// fix from an earlier session for the identical reason — fisherrunner/barbarianrunner
// just hadn't gotten it yet.
const walkNearTimeout = 120 * time.Second

// bankBoothOpenTimeout bounds how long openNearestBankBooth waits for a clicked booth to
// actually report open. Raised 8s -> 20s live-found 2026-09-16 (see openNearestBankBooth's
// own doc comment) for the same under-production-load reason walkNearTimeout above was
// raised. That fix only ever reached its own OpenCatherbyBank call site, though -- BankFish's
// call site below kept passing a hardcoded 8*time.Second literal, quietly reintroducing the
// exact timeout this same file already proved too short. Raised again here to 30s
// (2026-09-18): a full-fleet ~266-account soak (run/fleet.log) still logged 11 "timed out
// waiting for bank to open" hits across many different accounts sharing the same handful of
// Catherby booth candidates (ids 2213 at 2807/2809/2810/2811,3442), every one confirmed
// timing out at EXACTLY the prior 20s boundary (e.g. "acct59" 17:08:50->17:09:10,
// "crafter1" 17:12:51->17:13:11, "spare29" three times in one bank visit at 18:08) --
// consistent with real concurrent-account contention on the same few booth instances, not a
// permanently-bad booth (the exact same coordinates that "never opened" for one account
// opened fine moments earlier or later for others). Both call sites now share this one
// constant so a future timing fix can't silently miss one of them again.
const bankBoothOpenTimeout = 30 * time.Second

// WalkNear paths to dest and walks there, re-clicking from wherever the player stops,
// stopping once within radius tiles of dest. Returns false on a stop request, an
// unreachable destination, or the overall timeout.
//
// Routes through freshstartrunner.WalkWithWalker, not plain freshstartrunner.WalkNear --
// live-found 2026-09-18: an established account displaced across a real cross-region gap
// (confirmed via cmd/doordiag against the real production pack -- crafter1's own
// stranded tile sat in a genuine 216,661-tile connected component that does not include
// Catherby bank's own 109,716-tile component, despite their bounding boxes overlapping) had
// no way back at all, because this function used to call plain freshstartrunner.WalkNear
// with no Walker ever threaded through freshStartDeps -- only a brand-new account's own
// first-day flow got the long-distance Walker (doors/stairs/ship fares) an earlier session
// wired into internal/bot/fisher.go's OnStart. d.Walker nil (no live.NewWalker construction
// succeeded, or a caller that predates this field) falls straight through to today's exact
// plain-pathfind behavior -- see WalkWithWalker's own doc comment.
//
// Turns the persistent run toggle on via maybeEnableRun whenever there's energy to spare
// (see run.go), so these clicks move at running speed once the server-side toggle is on.
// The walk click's own ctrlHeld bit (actions.Walk's run parameter, below) stays false
// regardless — that is a separate, one-shot "force this single click to run" flag
// (randomevents/flee_walking.go's combat flee sets it per-click instead of relying on the
// toggle); it is not needed here once the persistent toggle is on, and this function used
// to avoid running altogether (both forms) per the position-tracking drift bug on running
// clicks documented in COLLAB.md/barbarianrunner. That bug is now root-caused and fixed
// (`e4e59a6`) and live-verified running walks are proven safe — see run.go's own doc
// comment for the evidence.
func WalkNear(d *Deps, dest tile.Tile, radius int) bool {
	return walkNearUntil(d, dest, radius, nil)
}

// walkToCampUntilSpot is FishUntil's "no matching fishing spot nearby" step: walk toward the
// camp (walk) until a matching spot is in view (spotInView), then hand straight back to the
// caller's next look. It waits a second first only when the walk ended with nothing in view,
// the pause that keeps a failing walk from being re-sent at loop speed. False means a stop was
// requested during that wait.
//
// The second used to be waited unconditionally, even when the walk had stopped precisely
// because a spot had come into view. Live on build e35420b6 this step ran 59,009 times in 37
// hours across 158 fishers (the Fishing Guild bank is more than 15 tiles from its Big net spots,
// so none is in view after banking), with a median 3 seconds from the line to "stepping beside
// the fishing spot", one of them this wait.
func walkToCampUntilSpot(d *Deps, walk func(until func() bool), spotInView func() bool) bool {
	seen := false
	walk(func() bool {
		if spotInView() {
			seen = true
		}
		return seen
	})
	if seen || spotInView() {
		return true
	}
	return d.Delay(time.Second)
}

func walkNearUntil(d *Deps, dest tile.Tile, radius int, until func() bool) bool {
	defer d.pushPhase("walking")()
	// Share door, level and sealed-pocket recovery with the travel runner.
	fd := freshStartDeps(d)
	fd.Delay = func(duration time.Duration) bool {
		if !randomevents.Busy(d.Session) {
			maybeEnableRun(d)
		}
		return d.Delay(duration)
	}
	fd.Aside = d.walkAside()
	if !randomevents.Busy(d.Session) {
		maybeEnableRun(d)
	}
	d.Stall.begin(d)
	defer d.Stall.end(d) // closes the last rub's menu and says what the walk's rubs did
	return freshstartrunner.WalkWithWalkerUntil(fd, dest, radius, walkNearTimeout, until)
}

// CheatSetup optionally sets the account's Fishing level (level > 0 — for proving a rung
// above Small net live without grinding to it, e.g. ::setstat fishing 16 for Big net, mirroring
// barbarianrunner.CheatSetup's own ::setstat precedent), then teleports to Harry's stand and
// gives the account coins, exactly like cmd/fishtest's non-live mode. Local-engine-only; never
// call this against a real account. Callers that already own a tool and are already positioned
// (a real account) should pass level 0 and rely on a live caller's own progression check (see
// internal/bot/fisher.go) to buy whatever the account's real level and coins allow, mirroring
// barbarianrunner.CheatSetup's own cheatSetup-flag convention.
func CheatSetup(d *Deps, layout fisherplan.Layout, level int, coins int) error {
	if level > 0 {
		d.logf("::setstat fishing %d", level)
		if err := actions.Cheat(d.Session, fmt.Sprintf("setstat fishing %d", level)); err != nil {
			return err
		}
		if !d.Delay(500 * time.Millisecond) {
			return nil
		}
	}
	d.logf("::tele to Harry's stand (%d,%d,%d)", layout.HarryStand.X, layout.HarryStand.Z, layout.HarryStand.Level)
	if err := TeleTo(d, layout.HarryStand); err != nil {
		return err
	}
	if !waitFor(d, "arrive at Harry's stand", 8*time.Second, func() bool {
		x, z, _, ok := d.Session.State().Tile()
		return ok && x == layout.HarryStand.X && z == layout.HarryStand.Z
	}) {
		return fmt.Errorf("did not arrive at Harry's stand after teleport")
	}
	if !d.Delay(1500 * time.Millisecond) {
		return nil
	}
	d.logf("::give coins %d (enough to buy up the ladder to the configured target)", coins)
	if err := actions.Cheat(d.Session, fmt.Sprintf("give coins %d", coins)); err != nil {
		return err
	}
	d.Delay(500 * time.Millisecond)
	return nil
}

// findNpcByName finds the nearest visible NPC whose cachecfg name matches, optionally
// filtered by having a specific op (nil skips the filter) — the same "name, then check
// what it actually offers" two-step FindSpot uses, reused here for Harry.
func findNpcByName(d *Deps, name string, near tile.Tile, radius int, requireOp *string) (gamestate.NPC, bool) {
	best := gamestate.NPC{}
	bestDist := radius + 1
	found := false
	for _, n := range d.Session.State().NPCs() {
		npcType, ok := d.Config.Npc(n.Type)
		if !ok || !strings.EqualFold(npcType.Name, name) {
			continue
		}
		if requireOp != nil && cachecfg.OpIndex(npcType.Ops, *requireOp) == 0 {
			continue
		}
		dist := tile.Chebyshev(near, tile.Tile{X: n.X, Z: n.Z, Level: n.Level})
		if dist <= radius && dist < bestDist {
			best, bestDist, found = n, dist, true
		}
	}
	return best, found
}

func packCountByID(items []gamestate.SlotItem, id int32) int32 {
	var n int32
	for _, it := range items {
		if it.ID == id {
			n += it.Count
		}
	}
	return n
}

// BuyTool walks to Harry (found by name among the visible NPCs, since shopkeepers are
// ordinary NPCs, not static content), opens his shop via the "Trade" option (resolved
// from his own cachecfg ops rather than a hardcoded option number), and buys one of tool
// from his stock, matched by cachecfg's Obj name rather than a hardcoded id — Harry's
// stock ids are this content build's own, not something to guess at.
func BuyTool(d *Deps, near tile.Tile, tool string) bool {
	return BuyToolOr(d, near, tool).Bought != ""
}

// whirlpoolAvoidRadius is how close a whirlpool NPC (randomevents.WhirlpoolNearby) can sit
// to a CANDIDATE fishing spot before FindSpot treats that spot as unsafe to walk to at all.
// Matches detect.go's own Detect()'s whirlpool-hazard radius (3 tiles) — that one is
// measured around the player reactively; this is the same distance measured around the
// spot itself, proactively, before ever walking over.
const whirlpoolAvoidRadius = 3

// combatAvoidRadius mirrors randomevents.combatAvoidRadius's own reasoning (that constant
// is unexported, so this is a deliberate local copy, not a drift risk — see
// randomevents.RecentlyAttackedNear's own doc comment): generous relative to
// fisherplan.Catherby's own SpotRange (20) so a fresh flee actually rules out a meaningful
// chunk of the camp, not just the one exact spot tile a wandering hostile has likely
// already left.
const combatAvoidRadius = 15

// ApproachRadius bounds how far the reachability check below looks for a route to a fishing
// spot. The same 24 combatrunner/fighterrunner/flinchrunner.ApproachRadius already use, and
// generous against Catherby's own SpotRange (20): a spot selected by distance from the camp
// tile is only worth walking to if there is a route from where the account is standing.
const ApproachRadius = 24

// Reachable answers the question the camp-distance filter cannot: whether there is a route
// from where the account is standing to a fishing spot, rather than only a short straight line
// to it across water or a wall.
//
// This is the same gate combatrunner/fighterrunner/flinchrunner already carry, arrived at the
// same way. A fishing spot sits on water, so its own tile is never walkable and the engine
// satisfies the click from the walkable tiles beside it whose facing side is not walled off —
// exactly Pack.ReachableWithin's goal semantics. A spot far enough out that every neighbouring
// tile is also water has no such tile, and the server will accept OPNPC on it regardless:
// OpNpcHandler.ts only checks the NPC exists, is visible and has that option, then calls
// setInteraction and lets the player walk. The walk then never satisfies inOperableDistance
// (reachedEntity -> ReachStrategy shape -2 -> reachRectangle1, which accepts only the four
// orthogonal sides), so no fishing trigger ever runs: no animation, no catch, no message.
// Nothing below this layer reports a failure, which is what makes it the same class of wedge
// as the walled-in warrior AdvancedFighter re-attacked every 8 seconds for a whole window.
//
// It fails OPEN — no pack, or no known tile yet, answers "reachable" with the reason — for the
// asymmetry those three siblings document: a false "unreachable" costs a spot that could have
// been fished, while the check exists to stop a session grinding forever on one that can never
// be, and a run with no collision data has no business overruling the server.
func Reachable(d *Deps, target tile.Tile) (bool, string) {
	ok, _, why := reachableSteps(d, target)
	return ok, why
}

// reachableSteps is Reachable plus the length of the route it already found, which is what
// FindSpot's own selection rule wants and what nothing was reading. steps is -1 whenever the
// route length is genuinely unknown — the fail-open cases above, where "reachable" is an
// assumption rather than a search result — so a caller can tell "no walking at all" from "no
// answer" and fall back to whatever it used before instead of treating an unknown as a zero.
func reachableSteps(d *Deps, target tile.Tile) (ok bool, steps int, why string) {
	if d == nil || d.Pack == nil {
		return true, -1, "no collision pack is loaded, so reachability cannot be asked"
	}
	x, z, level, known := d.Session.State().Tile()
	if !known {
		return true, -1, "our own tile is not known yet"
	}
	if level != target.Level {
		return false, -1, "a different level"
	}
	res := d.Pack.ReachableWithin(
		pathfind.NavPoint{X: x, Z: z, Level: level},
		pathfind.NavPoint{X: target.X, Z: target.Z, Level: target.Level},
		ApproachRadius)
	if res.OK {
		return true, routeSteps(res, target), ""
	}
	return false, -1, res.Reason
}

// routeSteps is res's length when it ends beside target, else -1 (unknown). FindPathAvoiding
// first searches for the tiles touching target and, failing that, settles for any tile within
// five of it; near the edge of ReachableWithin's box that second search can end up to five tiles
// short, and scoring its length would favour the spots at the edge (review of fisherwalk,
// 2026-09-25). Such a spot stays reachable; pickSpot just scores it by the straight line.
func routeSteps(res pathfind.PathResult, target tile.Tile) int {
	if len(res.Waypoints) == 0 {
		return -1
	}
	end := res.Waypoints[len(res.Waypoints)-1]
	if tile.Chebyshev(tile.Tile{X: end.X, Z: end.Z, Level: end.Level}, target) > 1 {
		return -1
	}
	return res.Steps()
}

// routableSpots drops every candidate with no route from where the account stands and reports
// how many it dropped, factored out pure so the rule can be tested without a session (the same
// reason pickSpot itself was pulled out — see spotCandidate's own doc comment).
//
// It runs BEFORE pickSpot, and that ordering is the load-bearing half rather than a detail.
// pickSpot returns the held `current` spot outright whenever it is still among the candidates,
// with no comparison against anything — so a gate applied after it would never be consulted for
// the spot the session is actually fishing. Filtering first means the held spot is revalidated
// for a route on every single call, which is precisely what AdvancedFighter was not doing when
// it kept re-attacking a walled-in warrior across Loop calls.
//
// A nil predicate keeps every candidate, so a caller with no pack behaves exactly as before.
//
// The predicate also hands back how many tiles that route walks, which is recorded on each kept
// candidate as Steps for pickSpot to score by. It costs nothing to collect: the search that
// answers "is there a route" is the same search that measured it.
func routableSpots(candidates []spotCandidate, reachable func(tile.Tile) (bool, int)) (kept []spotCandidate, noRoute int) {
	if reachable == nil {
		return candidates, 0
	}
	kept = candidates[:0:0]
	for _, c := range candidates {
		ok, steps := reachable(tile.Tile{X: c.NPC.X, Z: c.NPC.Z, Level: c.NPC.Level})
		if ok {
			// steps is -1 when reachableSteps failed open (no pack, own tile unknown): kept, but
			// unmeasured, so pickSpot scores Here rather than a -1 walk.
			if steps >= 0 {
				c.Steps, c.HaveSteps = steps, true
			}
			kept = append(kept, c)
			continue
		}
		noRoute++
	}
	return kept, noRoute
}

// FindSpot mirrors ProgressiveFisher.ts's spotMatchesMethod + nearest(): a "Fishing spot"
// NPC whose cachecfg ops include both the rung's op and its pair (the same two-op test
// that tells a small-net-and-bait spot apart from a net-and-harpoon or cage-and-harpoon
// spot sharing the same generic name), nearest the camp among the currently visible ones —
// UNLESS current is still a live, safe, matching candidate itself, in which case it always
// wins regardless of distance. See that parameter's own doc comment for why: without this,
// two ordinary spots close enough together near camp make the account oscillate between
// them instead of settling on one.
//
// A candidate with a whirlpool active nearby (randomevents.WhirlpoolNearby, within
// whirlpoolAvoidRadius) is skipped in favor of the next-nearest clean one — the account
// owner's own live request 2026-09-16 ("when a whirlpool spawns move to a different fishing
// spot that doesn't have a whirlpool or go and bank"). blocked reports true when at least one
// otherwise-usable spot exists near the camp but every single one has a whirlpool
// nearby or is excluded by recent combat. FishUntil can then pause or bank a catch;
// found and blocked are never both true.
//
// current/hasCurrent is the spot FishUntil was fishing (or just tried to) before this call —
// zero value/false on the very first call of a session. Real, live-caught bug this closes
// (account owner's own 2026-09-16 report, watching the live client directly): Catherby's
// camp comment above already notes "every spot moves between 2836 and 2860 along it" — a
// ~24-tile stretch easily holding two spots of the SAME rung within SpotRange (20) of each
// other. The old code picked strictly "nearest to the static camp tile" fresh on every call,
// with no memory of which spot was already being fished. FishUntil calls FindSpot again after
// perfectly ordinary events that don't mean the current spot went bad — most commonly "no
// bite for 12s — re-clicking" (runner.go's own FishUntil hold loop), which happens routinely
// mid-session. Whenever the account's current spot was not the single globally-nearest
// candidate (e.g. it sits half a tile farther from camp than its neighbour), that re-find
// swapped to the neighbour — which the very next 12s-no-bite cycle swapped right back from,
// since the ORIGINAL spot was then the nearer one again from the neighbour's own position.
// Two matching spots at the exact same distance made it worse still: state.NPCs() snapshots
// a map (internal/gamestate/state.go's own npcs field), so Go's randomized map iteration
// order picked a different one of an exact tie practically every call, with nothing to do
// with the account's actual position. Fixed the same way freshstartrunner's own recent
// door/staircase selection fixes did (see stairOpsDown/stairOpsUp and
// pickDirectionalMatch's own doc comments there): stability beats marginal optimality —
// once a spot is being fished, keep fishing it until it is actually gone, not just
// technically a little farther from camp than something else.
func FindSpot(d *Deps, camp tile.Tile, radius int, rung fisherplan.Rung, current gamestate.NPC, hasCurrent bool) (spot gamestate.NPC, found bool, blocked bool) {
	spot, found, safety := findSpotSafety(d, camp, radius, rung, current, hasCurrent)
	return spot, found, !found && safety.blocked()
}

// spotSafety is why otherwise-eligible candidates were passed over this call.
//
// noRoute is deliberately NOT part of blocked(). A whirlpool or a recent fight means the camp
// is briefly unsafe and waiting it out is right, which is what blocked() drives. Every spot
// being out of reach means something else entirely: the account is standing somewhere it cannot
// fish from, and the recovery it needs is FishUntil's own walk back to the camp and, failing
// that, ReturnToLayout — not standing still waiting for a hazard that was never there.
type spotSafety struct{ whirlpool, combat, noRoute int }

func (s spotSafety) blocked() bool { return s.whirlpool+s.combat > 0 }

func findSpotSafety(d *Deps, camp tile.Tile, radius int, rung fisherplan.Rung, current gamestate.NPC, hasCurrent bool) (spot gamestate.NPC, found bool, safety spotSafety) {
	state := d.Session.State()
	// Where the account actually is, for the walk each candidate would cost. camp is the
	// membership bound (radius, below); it is not where the walk starts from.
	//
	// Falls back to the camp when the position is not known yet, which restores exactly the
	// old camp-distance rule for that case. currentTile() would have handed back tile 0,0,0
	// instead and every candidate would have been scored by its distance from the map origin
	// — still inside the camp radius, so nothing visibly breaks, which is precisely why it
	// is worth being explicit about here rather than leaving it to be noticed later.
	here := camp
	if x, z, level, ok := d.Session.State().Tile(); ok {
		here = tile.Tile{X: x, Z: z, Level: level}
	}
	var candidates []spotCandidate
	for _, n := range state.NPCs() {
		npcType, ok := d.Config.Npc(n.Type)
		if !ok || !strings.EqualFold(npcType.Name, fisherplan.FishingSpot) || !spotUsable(int(n.Type)) {
			continue
		}
		if cachecfg.OpIndex(npcType.Ops, rung.Op) == 0 || cachecfg.OpIndex(npcType.Ops, rung.Pair) == 0 {
			continue
		}
		dist := tile.Chebyshev(camp, tile.Tile{X: n.X, Z: n.Z, Level: n.Level})
		if dist > radius {
			continue
		}
		if randomevents.WhirlpoolNearby(state, n.X, n.Z, whirlpoolAvoidRadius) {
			safety.whirlpool++
			continue
		}
		// Same idea as the whirlpool check just above, for combat instead of a hazard NPC —
		// found live 2026-09-16 (lc2b_36/lc2b_67, same 200-account soak): fleeFromCombat
		// correctly flees to the bank every time, but nothing remembered a fight had just
		// happened here, so FindSpot kept re-selecting the exact spot that just got the
		// account attacked the instant it was called again post-flee — flee, walk back,
		// get hit, flee again, repeat, with no healing between rounds (eatFood off) until
		// cumulative damage killed the account. See randomevents.RecentlyAttackedNear's own
		// doc comment for the full live trace.
		if randomevents.RecentlyAttackedNear(d.Session, n.X, n.Z, n.Level, combatAvoidRadius) {
			safety.combat++
			continue
		}
		// Steps stays unmeasured (HaveSteps false) until routableSpots measures it below; a nil
		// predicate there (no pack) leaves it so, and pickSpot then scores Here.
		candidates = append(candidates, spotCandidate{
			NPC: n, Dist: dist, Here: tile.Chebyshev(here, tile.Tile{X: n.X, Z: n.Z, Level: n.Level}),
			Crowd: crowdAt(state, n),
		})
	}
	candidates, safety.noRoute = routableSpots(candidates, func(t tile.Tile) (bool, int) {
		ok, steps, _ := reachableSteps(d, t)
		return ok, steps
	})
	jitter := randomJitter
	if !WhirlpoolGuard {
		jitter = nil
		for i := range candidates {
			candidates[i].Crowd = 0
		}
	}
	spot, found = pickSpot(candidates, current, hasCurrent, jitter)
	return spot, found, safety
}

// spotCandidate is one eligible fishing spot as pickSpot needs it: already filtered by name,
// ops, range, and hazard/combat safety (all of which need d.Config/d.Session, see FindSpot
// above), leaving only the plain data pickSpot's selection rule actually reads. Pulled out
// the same way RecoverLostTool's own countByName/nearestGroundItemNamed were (see
// bigfish_test.go's doc comment on why) so the actual selection rule can be unit tested
// directly against synthetic input — FindSpot itself stays proven live only, since it's
// typed against the concrete *cachecfg.Config, which has no exported constructor for a
// synthetic one.
type spotCandidate struct {
	NPC gamestate.NPC
	// Dist is Chebyshev from the CAMP tile. It is the membership bound (findSpotSafety drops
	// anything past the layout's SpotRange) and pickSpot's tie-break, not its walk cost.
	Dist int
	// Here is Chebyshev from where the account is standing — the walk this candidate actually
	// costs, and what pickSpot scores on.
	//
	// Live 2026-09-21, the whole 269-account fleet over an hour: 4,458 spot changes, median hop
	// 6 tiles, p90 10, 38% of them 8 tiles or more, 16.2 catches per hop, 13.4 account-hours =
	// 5.4% of ALL fleet time. Scoring on Dist alone is what paid for those hops: an account
	// whose spot vanished ten tiles from the camp was sent to whichever spot sat nearest the
	// camp CENTRE, which is frequently the one it just walked away from. The worst account in
	// that window (34.8% of its time walking against 45.6% fishing) was bouncing between two
	// spots about ten tiles apart, one of which was always nearer the camp than the other.
	Here int
	// Steps is how many tiles the account would actually walk to reach this spot from where it
	// is standing, filled in by routableSpots from the route its reachability search already
	// found (claude/fisherwalk). Here is a straight line and crosses water: at the Fishing Guild,
	// whose spots sit around a bay, a spot 5 tiles away by Here was 38 steps by route. Only
	// meaningful with HaveSteps: unmeasured (no collision pack, or our own tile not known
	// yet) pickSpot scores Here. A flag rather than a -1 sentinel, because 0 steps (already
	// beside the spot) is a real measurement and a zero-value candidate must mean "unmeasured".
	Steps     int
	HaveSteps bool
	// Crowd is how many other players are fishing the spot (crowdAt).
	Crowd int
}

// pickSpot is FindSpot's actual selection rule, factored out pure. See FindSpot's own doc
// comment for the oscillation this closes: current, if hasCurrent and still present among
// candidates by the same index+tile identity npcStillThere already uses, always wins outright
// — no comparison against anything merely nearer. Only when current is absent (truly gone, or
// there was none yet) does the plain nearest-by-Dist search run, ties won by whichever
// candidate appears first.
//
// A fresh choice scores each candidate by the walk it costs FROM WHERE THE ACCOUNT IS
// STANDING plus crowdWeight tiles per other fisher at it plus jitter(spotJitter) (nil jitter adds
// none), lowest wins, ties broken by distance from the camp. Live 2026-09-20: pure nearest sent
// ~139 released accounts to one spot in the same second, which turned into a whirlpool under
// them; the crowd term and the jitter spread a released crowd over the spots there are. All three
// terms are in tiles, which is why mixing them works at all.
//
// "The walk it costs" is Steps, the length of the route routableSpots already measured, when it is
// known, else Here, the straight line from the account (spotCandidate.Here has the 5.4%-of-fleet-
// time measurement that moved this off camp distance). Steps is the better of the two where the
// straight line crosses water: measured on the live fleet (f505d78), approach legs ran a
// route-to-straight-line ratio of 1.60 median and 4.00 p90, and the Fishing Guild's median approach
// was 20 steps against Catherby's 11.
//
// Drift is bounded by the caller, not by this rule: findSpotSafety already drops every candidate
// further than the layout's SpotRange from the camp, so this can only choose among spots that were
// already in the area.
// RouteScoring lets pickSpot score a measured route (Steps) instead of the straight line (Here).
// On by default; it exists so cmd/labfleettest -mode rung-rate -spot-score line can run the
// control arm on the same build, the way WhirlpoolGuard does for its A/B.
var RouteScoring = true

func pickSpot(candidates []spotCandidate, current gamestate.NPC, hasCurrent bool, jitter func(int) int) (spot gamestate.NPC, found bool) {
	if hasCurrent {
		for _, c := range candidates {
			if c.NPC.Index == current.Index && c.NPC.X == current.X && c.NPC.Z == current.Z && c.NPC.Level == current.Level {
				return c.NPC, true
			}
		}
	}
	best := gamestate.NPC{}
	bestScore, bestCamp := 0, 0
	for _, c := range candidates {
		near := c.Here
		if c.HaveSteps && RouteScoring {
			near = c.Steps
		}
		score := near + crowdWeight*c.Crowd
		if jitter != nil {
			score += jitter(spotJitter)
		}
		switch {
		case !found, score < bestScore:
		case score == bestScore && c.Dist < bestCamp:
			// Equal walk, equal crowd: take the one nearer the camp. Ties are common —
			// spots sit in clusters — and without this the winner is whichever happened to
			// come first out of the NPC list, which lets a released crowd settle at the far
			// edge of the radius instead of gravitating back to the camp core.
		default:
			continue
		}
		best, bestScore, bestCamp, found = c.NPC, score, c.Dist, true
	}
	return best, found
}

// npcStillThere reports whether an NPC with this index is still visible at (about) the
// same spot — a fresh net/bait spot NPC reusing the same index at a different tile still
// counts as "vanished" for this purpose, matching ProgressiveFisher.ts's own
// tile-equality + whirlpool check in its fishing hold loop.
func npcStillThere(d *Deps, spot gamestate.NPC) bool {
	for _, n := range d.Session.State().NPCs() {
		if n.Index == spot.Index {
			return n.X == spot.X && n.Z == spot.Z && n.Level == spot.Level && spotUsable(int(n.Type))
		}
	}
	return false
}

// CurrentRawFish sums every raw fish (fisherplan.IsRawFish) in the player's own pack
// (PlayerPack, comID 3214) — deliberately NOT internal/randomevents'/internal/statlog's
// "scan every known inventory component" approach (see PlayerPack's doc comment for the
// real contamination bug that caused).
func CurrentRawFish(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if ok && fisherplan.IsRawFish(obj.Name) {
			c := it.Count
			if c < 1 {
				c = 1
			}
			n += int(c)
		}
	}
	return n
}

// OwnedTools is which of the ladder's own tools (fisherplan.ToolNames) are currently held in
// the player's own pack (PlayerPack) — the "owned" list fisherplan.ActiveRung needs to pick
// the best rung actually fishable right now. Does not look in the bank: a live progression
// check re-evaluates every fish-phase start, so a tool bought and left in the bank gets
// noticed the next time this is called after a bank trip, same as any other pack state.
func OwnedTools(d *Deps) []string {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	ladder := fisherplan.ToolNames()
	seen := make(map[string]bool, len(ladder))
	var out []string
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok {
			continue
		}
		for _, tool := range ladder {
			if !seen[tool] && strings.EqualFold(obj.Name, tool) {
				seen[tool] = true
				out = append(out, tool)
			}
		}
	}
	return out
}

// PackItems is the player's own pack, grouped by name — the live snapshot
// freshstart.SellQueue/SellPlan need to work out what the Lumbridge store would buy
// (fisherplan.StarterKeep names everything that stays: the coins and every ladder tool).
func PackItems(d *Deps) []freshstart.PackItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	totals := map[string]int{}
	var order []string
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" {
			continue
		}
		if _, seen := totals[obj.Name]; !seen {
			order = append(order, obj.Name)
		}
		c := it.Count
		if c < 1 {
			c = 1
		}
		totals[obj.Name] += int(c)
	}
	out := make([]freshstart.PackItem, 0, len(order))
	for _, name := range order {
		out = append(out, freshstart.PackItem{Name: name, Count: totals[name]})
	}
	return out
}

// itemRows aggregates raw slot items into donorplan.ItemRow by (name, id) — the shared
// shape fisherplan.MuleRows/MuleHaul/MuleLoad (and any future script's own MuleRows) read,
// used by both BankItemRows and PackItemRows below rather than duplicating the aggregation
// twice in this file.
func itemRows(d *Deps, items []gamestate.SlotItem) []donorplan.ItemRow {
	type key struct {
		name string
		id   int
	}
	totals := map[key]int{}
	var order []key
	for _, it := range items {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" {
			continue
		}
		k := key{name: obj.Name, id: int(it.ID)}
		if _, seen := totals[k]; !seen {
			order = append(order, k)
		}
		c := it.Count
		if c < 1 {
			c = 1
		}
		totals[k] += int(c)
	}
	out := make([]donorplan.ItemRow, 0, len(order))
	for _, k := range order {
		out = append(out, donorplan.ItemRow{Name: k.name, ID: k.id, Count: totals[k]})
	}
	return out
}

// BankItemRows reads an already-open bank's contents as donorplan.ItemRow, for
// fisherplan.MuleRows to decide what leaves the bank for the mule (internal/bot/fisher.go's
// own hand-off wiring — see donorrunner's package doc for why this stays script-specific
// rather than living in donorrunner itself: each script's own bank layout differs).
func BankItemRows(d *Deps, bank *actions.Bank) []donorplan.ItemRow {
	return itemRows(d, bank.Items())
}

// PackItemRows reads the player's own pack as donorplan.ItemRow, for fisherplan.MuleHaul/
// MuleLoad — the per-(name,id) counterpart to PackItems' per-name-only aggregation (that
// one is for freshstart.SellQueue, which never needs to tell two different ids with the
// same display name apart; a hand-over's LoadGroup.ID is exactly that distinction).
func PackItemRows(d *Deps) []donorplan.ItemRow {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	return itemRows(d, inv.NonEmpty())
}

// Coins is the player's own pack coins (freshstart.Coins), used to decide whether the account
// can actually afford a rung's tool before BuyTool is ever attempted.
func Coins(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if ok && strings.EqualFold(obj.Name, freshstart.Coins) {
			c := it.Count
			if c < 1 {
				c = 1
			}
			n += int(c)
		}
	}
	return n
}

// PackFull reports whether the player's own pack (PlayerPack) holds 28 non-empty slots.
func PackFull(d *Deps) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	return inv != nil && len(inv.NonEmpty()) >= 28
}

// ---- Big fish random event recovery ----
//
// The classic OSRS "Big fish" random event: while fishing, the fishing spot's own NPC
// occasionally eats the tool being fished with and spits it onto the ground a few tiles
// away — see randomevents.BigFishNearby's own doc comment for the full ground-truthing
// trail against the local test engine's own content scripts (no chat/mesbox line of its
// own; the only real signal is the tool disappearing from the pack, confirmed via
// content/scripts/macro events/scripts/fishing/macro_event_big_fish.rs2's own
// inv_del(inv, $fishing_equipment, 1) + obj_add(...) pair). This was a confirmed, total
// gap before 2026-09-16 (grep -rni "big fish" internal/randomevents/ found nothing) —
// FishUntil used to have no way to notice a tool had just been eaten at all; it would
// simply keep re-clicking a spot it no longer had the right item for until the spot's own
// "no bite for 12s" timeout happened to notice something was wrong, by which point the
// dropped tool — while in no danger of despawning that quickly (lootdrop.constant's own
// ^lootdrop_duration is 200 ticks, ~120s) — had gone unrecovered for no reason.
//
// bigFishSearchRadius bounds the ground search for the spat-out tool.
// macro_event_big_fish.rs2's own obj_add call places it via
// map_findsquare(coord, 1, 7, ^map_findsquare_lineofwalk) around the fishing spot NPC's
// own tile (the active entity while that proc runs) — 7 is this content build's own real
// maximum; 10 leaves a small margin for the OSRS wiki's own "usually 3-10 tiles" without
// searching so wide it risks matching some unrelated dropped tool of the same name from
// another account fishing nearby (a real risk on a 179-account fleet sharing one camp).
const bigFishSearchRadius = 10

// countByName totals every item in items whose types-resolved name equals name
// (case-insensitive) — the pure core of toolCount/Coins/freshStartCoins' identical
// per-name pack scans, pulled out into its own function (rather than inlined the way
// those three are) so it can be unit-tested against synthetic gamestate.SlotItem state
// and a fake actions.ObjTypes without needing a real cache archive — this project's own
// established seam for exactly that (see actions.ObjTypes's own doc comment:
// "*cachecfg.Config satisfies it").
func countByName(items []gamestate.SlotItem, types actions.ObjTypes, name string) int {
	if types == nil {
		return 0
	}
	n := 0
	for _, it := range items {
		obj, ok := types.Obj(int(it.ID))
		if ok && strings.EqualFold(obj.Name, name) {
			c := it.Count
			if c < 1 {
				c = 1
			}
			n += int(c)
		}
	}
	return n
}

// toolCount is how many of tool (a fisherplan.Rungs entry's own Tool name) the pack
// currently holds. Needed here (rather than reusing OwnedTools' set-membership view)
// because a Big fish loss needs to notice a SPECIFIC tool going from held to not-held —
// OwnedTools alone can't tell that apart from "never had it in the first place."
func toolCount(d *Deps, tool string) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	return countByName(inv.NonEmpty(), d.Config, tool)
}

// nearestGroundItemNamed returns the nearest item in items named name (case-insensitive)
// within radius tiles of anchor, on the same level — the pure core of findGroundTool
// (itself ported in spirit from boneburierrunner.FindBone, specialised to a single exact
// name match since a Big fish loss only ever spits out the one specific item that was
// just eaten), pulled out for the same synthetic-state testability reason countByName's
// own doc comment gives.
func nearestGroundItemNamed(items []gamestate.GroundItem, types actions.ObjTypes, anchor tile.Tile, name string, radius int) (gamestate.GroundItem, bool) {
	if types == nil {
		return gamestate.GroundItem{}, false
	}
	best := gamestate.GroundItem{}
	bestDist := -1
	found := false
	for _, g := range items {
		obj, ok := types.Obj(g.ID)
		if !ok || !strings.EqualFold(obj.Name, name) {
			continue
		}
		if g.Level != anchor.Level {
			continue
		}
		gt := tile.Tile{X: g.X, Z: g.Z, Level: g.Level}
		dist := tile.Chebyshev(anchor, gt)
		if dist > radius {
			continue
		}
		if !found || dist < bestDist {
			best, bestDist, found = g, dist, true
		}
	}
	return best, found
}

// findGroundTool is nearestGroundItemNamed against the live session's own ground items
// and cache config.
func findGroundTool(d *Deps, anchor tile.Tile, tool string, radius int) (gamestate.GroundItem, bool) {
	return nearestGroundItemNamed(d.Session.State().GroundItems(), d.Config, anchor, tool, radius)
}

// RecoverLostTool checks whether tool — the rung currently being fished — has just
// disappeared from the pack, and if so tries to recover it off the ground nearby before
// giving up. No-ops immediately (recovered=false, lost=false) while tool is still held:
// this is meant to be called cheaply on every poll of an active fishing hold, not only
// once a loss is already suspected, mirroring how CurrentRawFish is already polled the
// same way in FishUntil's own hold loop.
//
// A real loss (tool held a moment ago, gone now, while nothing in this script's own flow
// banked/sold/dropped it) is, in this content build, only ever the Big fish event —
// fishing tools never degrade or break on their own here (see
// randomevents.BigFishNearby's own doc comment for the full citation trail confirming
// this against the content scripts) so there's no ordinary-wear-and-tear case this could
// be confusing it with.
//
// Search radius is bigFishSearchRadius around spotTile (the fishing spot just being
// worked, the same tile the content script's own obj_add call centers on). Found: walks
// to it (unless already adjacent), takes it, and waits for it to land back in the pack —
// recovered=true. Not found, already gone by the time we arrive, or the take itself
// fails: logs a single clear operator-visible WARNING line and returns lost=true so the
// caller can bail out of this fishing session rather than looping on a spot it no longer
// holds the tool to work. Deliberately does NOT invent a "walk to Harry and rebuy"
// fallback here — fisherplan.DecideFishPhase's own PhaseBank/PhaseSell/PhaseBuy chain
// (see plan.go: "What the bank holds decides the rest") already covers buying a
// replacement the moment HeldTools no longer contains the active rung's tool; returning
// control to the caller (internal/bot/fisher.go's own Loop, which re-derives the phase
// from live state every call) is all that's needed for that existing machinery to take
// over.
func RecoverLostTool(d *Deps, spotTile tile.Tile, tool string) (recovered, lost bool) {
	if toolCount(d, tool) > 0 {
		return false, false
	}
	state := d.Session.State()
	confirmedBigFish := randomevents.BigFishNearby(state, spotTile.X, spotTile.Z, bigFishSearchRadius)
	// A whirlpool deletes the tool outright (content macro_whirlpool_attempt_take_equipment,
	// on the fifth roll) and lasts 60 ticks, so one still at the spot is the cause. Live
	// 2026-09-20 every one of 72 such losses was blamed on a Big fish instead.
	whirlpool := !confirmedBigFish && randomevents.WhirlpoolNearby(state, spotTile.X, spotTile.Z, 2)
	item, ok := waitForGround(func() (gamestate.GroundItem, bool) {
		return findGroundTool(d, spotTile, tool, bigFishSearchRadius)
	}, d.Delay, groundToolWait)
	if !ok {
		if whirlpool {
			d.logf("WARNING: %s is no longer in the pack — lost to a whirlpool at the spot near %d,%d (each roll on one counts toward losing the tool, the fifth deletes it) — falling back to the normal re-buy flow", tool, spotTile.X, spotTile.Z)
		} else if confirmedBigFish {
			d.logf("WARNING: a Big fish random event ate our %s near %d,%d and no matching item was found on the ground within %d tiles — it may already be gone; falling back to the normal re-buy flow", tool, spotTile.X, spotTile.Z, bigFishSearchRadius)
		} else {
			d.logf("WARNING: %s is no longer in the pack (no whirlpool in sight, so most likely a Big fish random event near %d,%d) and no matching item was found on the ground within %d tiles — falling back to the normal re-buy flow", tool, spotTile.X, spotTile.Z, bigFishSearchRadius)
		}
		return false, true
	}
	d.logf("Big fish random event (confirmed nearby=%v): spotted our %s on the ground at %d,%d — recovering it", confirmedBigFish, tool, item.X, item.Z)
	dt := tile.Tile{X: item.X, Z: item.Z, Level: item.Level}
	if tile.Chebyshev(currentTile(d), dt) > 1 {
		if !WalkNear(d, dt, 1) {
			d.logf("WARNING: could not walk to the dropped %s at %d,%d — falling back to the normal re-buy flow", tool, item.X, item.Z)
			return false, true
		}
	}
	still := false
	for _, g := range d.Session.State().GroundItems() {
		if g.ID == item.ID && g.X == item.X && g.Z == item.Z && g.Level == item.Level {
			still = true
			break
		}
	}
	if !still {
		d.logf("WARNING: %s was gone by the time we got there — falling back to the normal re-buy flow", tool)
		return false, true
	}
	if err := actions.TakeGroundItem(d.Session, d.Config, item); err != nil {
		d.logf("WARNING: take %s: %v — falling back to the normal re-buy flow", tool, err)
		return false, true
	}
	if !waitFor(d, fmt.Sprintf("%s back in the pack", tool), 3*time.Second, func() bool {
		return toolCount(d, tool) > 0
	}) {
		d.logf("WARNING: picked up %s but it did not land back in the pack — falling back to the normal re-buy flow", tool)
		return false, true
	}
	d.logf("recovered %s after a Big fish random event", tool)
	return true, false
}

// FishUntil repeatedly finds a spot matching rung's op/pair near the camp, works it, and
// polls the pack's own raw fish count (per fisherplan.IsRawFish/FishCount, the same
// "count what's actually in the pack" signal ProgressiveFisher.ts's own fishStep uses via
// Inventory.used() deltas, rather than trusting a chat line — classic RS fishing has none
// per catch) until target is reached, the pack fills, the timeout expires, or a stop is
// requested. A spot that disappears (moved, or turned into something else — e.g. a
// passing whirlpool) is simply re-found on the next loop iteration; no special-casing
// needed since FindSpot always re-queries live NPCs.
//
// When every candidate spot near the camp is blocked by a whirlpool or recent
// combat, this banks a catch of at least 14 raw fish (BankFish, the
// same phase-agnostic bank primitive PhaseBank already drives) rather than repeating "no
// matching fishing spot nearby" against spots it already knows are unsafe — the account
// owner's request to avoid a bank trip for fewer than 14 fish. With a smaller catch, or when
// banking fails, wait and recheck live spots and Guardian without an empty bank trip.
func FishUntil(d *Deps, layout fisherplan.Layout, rung fisherplan.Rung, target int, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	manipBegin(d, rung.Label)
	defer manipEnd(d)
	lastCount := CurrentRawFish(d)
	// noSpotStreak/lastNoSpotTile detect a genuine stranding rather than an ordinary
	// nearby pathfind hiccup: real, live-caught gap this session (see COLLAB.md's
	// "Drunken Dwarf" entry) — an account displaced far from Catherby (a death respawn
	// near Lumbridge, at minimum; the exact trigger is still being pinned down) used to
	// just repeat "no matching fishing spot nearby" / a failing WalkNear forever, since
	// nothing here ever checked whether repeated failures were happening from the SAME
	// tile with no progress. Mirrors the threshold barbarianrunner/freshstartrunner's own
	// stranding detectors already use elsewhere in this codebase.
	noSpotStreak := 0
	var lastNoSpotTile tile.Tile
	// lastSpot/haveLastSpot carries the previously-selected spot into the next FindSpot call
	// so it can stay on it instead of re-picking fresh every time — see FindSpot's own doc
	// comment on the current/hasCurrent parameters for the oscillation this closes. Simply
	// forwarding whatever FindSpot last returned (updated right after every call below, "no
	// spot found" included) is deliberate: FindSpot itself already re-validates the tile
	// before honoring it, so a spot that genuinely vanished is harmlessly ignored next call
	// with no separate bookkeeping needed here for why the last call ended.
	var lastSpot gamestate.NPC
	var lastBlockedLog time.Time
	// lastNoRouteLog throttles the "passed over N with no route" line. Without it the gate is
	// invisible in a healthy session: quietly skipping the spots it cannot walk to reads exactly
	// like a camp that never had any, which is the same thing that hid the reachability gate in
	// AdvancedFighter until it was made to say how many it passed over.
	var lastNoRouteLog time.Time
	haveLastSpot := false
	// clicked/clickedAt are the last spot this loop sent OPNPC to, for the whirlpool guard
	// (whirlpool.go); stagger asks for a random wait before choosing a new spot, set when the
	// last spot vanished or every spot was blocked, so a released crowd does not herd.
	var clicked gamestate.NPC
	var clickedAt time.Time
	stagger := false
	// reclick is set after a junk drop (junk.go), which stops the fishing action server-side
	// before the client's animation shows it: the spot is clicked again rather than taken as
	// still being fished. junkOff stops dropping for the rest of the session after a drop that
	// dropped nothing, so a refused drop banks the pack instead of retrying at loop speed.
	reclick, junkOff := false, false
	// A short phase boundary must not restart an already-running fishing action.
	if fishingActive(d.Session.State()) {
		for _, npc := range d.Session.State().NPCs() {
			if npc.Index == d.Session.State().SelfFaceEntity() {
				lastSpot, haveLastSpot = npc, true
				break
			}
		}
	}
	for time.Now().Before(deadline) && lastCount < target {
		// The batch of the hold that just ended is read before anything else writes (fishmanip.go).
		manipSettle(d)
		guard := func() {
			if guardWhirlpool(d, clicked, clickedAt) {
				clickedAt = time.Time{}
			}
		}
		guard()
		if !waitForGuardianWith(d, guard) {
			return lastCount
		}
		if KeptFull(d) {
			d.logf("pack full of the kept catch (%d free slot(s) with the junk dropped) — banking", FreeSlots(d)+len(fisherplan.PlanDrops(cycleView(d))))
			return lastCount
		}
		if !junkOff && junkDue(d) {
			if !dropJunkMidSession(d, &lastCount, &target) {
				junkOff = true
			}
			reclick = true
			continue
		}
		spot, ok, safety := findSpotSafety(d, layout.Camp, layout.SpotRange, rung, lastSpot, haveLastSpot)
		if ok && stagger && WhirlpoolGuard {
			// Wait a random moment, then choose again with fresh crowd counts rather than
			// everyone committing to the nearest spot in the same tick.
			stagger = false
			haveLastSpot = false
			if !d.Delay(herdStagger()) {
				return lastCount
			}
			continue
		}
		lastSpot, haveLastSpot = spot, ok
		if !ok && safety.blocked() {
			stagger = true
			held := CurrentRawFish(d)
			if time.Since(lastBlockedLog) >= guardianWaitLogEvery {
				why := ""
				if safety.combat > 0 {
					why = " [" + randomevents.RecentCombatDiag(d.Session) + "]"
				}
				d.logf("fishing paused: all matching spots unsafe (%d whirlpool, %d recent combat); %d raw fish held%s", safety.whirlpool, safety.combat, held, why)
				lastBlockedLog = time.Now()
			}
			// The pause is where live accounts lost their nets: a click that crossed into a
			// whirlpool keeps rolling while this idles, so the guard is polled through the wait.
			pd := *d
			pd.Delay = func(t time.Duration) bool {
				for t > 0 {
					guard()
					step := min(t, 200*time.Millisecond)
					if !d.Delay(step) {
						return false
					}
					t -= step
				}
				return true
			}
			if blockedFishingStep(&pd, held, func() (int, bool) { return BankFish(d, layout) }) {
				return lastCount
			}
			// Empty trips cannot make fishing safer. Stay idle and recheck Guardian
			// and the live spots instead of walking bank -> camp -> bank indefinitely.
			continue
		}
		lastBlockedLog = time.Time{}
		if !ok {
			here := currentTile(d)
			if here == lastNoSpotTile {
				noSpotStreak++
			} else {
				noSpotStreak = 1
				lastNoSpotTile = here
			}
			if noSpotStreak >= 5 {
				d.logf("no fishing spot found near the camp %d times in a row from %d,%d,%d — checking for a stranding", noSpotStreak, here.X, here.Z, here.Level)
				if ReturnToLayout(d, layout) {
					noSpotStreak = 0
				} else {
					d.logf("could not recover back to Catherby — giving up this fishing session")
					return lastCount
				}
				continue
			}
			if safety.noRoute > 0 {
				d.logf("no matching fishing spot we can walk to from %d,%d,%d — %d nearby had no route — walking to the camp and retrying", here.X, here.Z, here.Level, safety.noRoute)
			} else {
				d.logf("no matching fishing spot nearby — walking to the camp and retrying")
			}
			// Stop as soon as a usable spot is in view. Live 2026-09-19: returning from the
			// bank, fishers walked on to the camp centre (2845,3431) and then back west to the
			// Big net spots at 2837-2838 they had passed, ~10s of every bank trip.
			if !walkToCampUntilSpot(d, func(until func() bool) { walkNearUntil(d, layout.Camp, 3, until) }, func() bool {
				_, found, _ := findSpotSafety(d, layout.Camp, layout.SpotRange, rung, gamestate.NPC{}, false)
				return found
			}) {
				return lastCount
			}
			continue
		}
		noSpotStreak = 0
		if safety.noRoute > 0 && time.Since(lastNoRouteLog) >= guardianWaitLogEvery {
			lastNoRouteLog = time.Now()
			here := currentTile(d)
			d.logf("no route to %d nearby fishing spot(s) from %d,%d,%d — fishing the one at %d,%d instead of clicking across the water", safety.noRoute, here.X, here.Z, here.Level, spot.X, spot.Z)
		}
		spotTile := tile.Tile{X: spot.X, Z: spot.Z, Level: spot.Level}
		if stand, ok := orthogonalStand(d, layout, spotTile); ok {
			d.logf("stepping beside the fishing spot at %d,%d (to %d,%d)", spot.X, spot.Z, stand.X, stand.Z)
			if !WalkNear(d, stand, 0) {
				if !d.Delay(time.Second) {
					return lastCount
				}
				continue
			}
		} else if tile.Chebyshev(currentTile(d), spotTile) > 1 {
			d.logf("walking to the fishing spot at %d,%d", spot.X, spot.Z)
			if !WalkNear(d, spotTile, 1) {
				if !d.Delay(time.Second) {
					return lastCount
				}
				continue
			}
		}
		npcType, _ := d.Config.Npc(spot.Type)
		op := cachecfg.OpIndex(npcType.Ops, rung.Op)
		if op == 0 {
			d.logf("spot no longer offers %q — re-finding", rung.Op)
			continue
		}
		alreadyFishing := !reclick && ((fishingActive(d.Session.State()) && d.Session.State().SelfFaceEntity() == spot.Index) || manipResumable(d, spot))
		if !alreadyFishing {
			if !waitForGuardianWith(d, guard) {
				return lastCount
			}
			// Guardian can move us or the spot while the gate is waiting. Re-read the spot
			// right before the click: a whirlpool on it, or beside it, is not clicked.
			if !npcStillThere(d, spot) || tile.Chebyshev(currentTile(d), spotTile) > 1 {
				continue
			}
			if WhirlpoolGuard && randomevents.WhirlpoolNearby(d.Session.State(), spot.X, spot.Z, whirlpoolAvoidRadius) {
				d.logf("a whirlpool appeared at or beside the spot at %d,%d before the click — choosing again", spot.X, spot.Z)
				haveLastSpot = false
				stagger = true
				continue
			}
			d.logf("OPNPC%d (%s) on fishing spot index=%d at %d,%d", op, rung.Op, spot.Index, spot.X, spot.Z)
			if err := manipClick(d, op, spot); err != nil {
				d.logf("OpNpc: %v", err)
				if !d.Delay(time.Second) {
					return lastCount
				}
				continue
			}
			clicked, clickedAt = spot, time.Now()
			reclick = false
		} else {
			manipResume(d, op, spot)
		}
		// Hold this spot until the catch stalls (no new fish for a while — animation
		// stopped, spot moved out from under us, or we're simply between bites), the
		// pack fills, or the target/deadline is reached; then let the outer loop
		// re-find a spot rather than growing this into its own retry state machine.
		still := time.Now()
		sawFishing := alreadyFishing
		// What this spot yields, for the Loot panel's Fishing spot card (see catch.go for the
		// window this measures and what it cannot separate). Reporting only.
		catch := beginCatch(d, int(spot.Type))
		// The window's events are the raw fish the script keeps (keptRawFish): a Raw mackerel
		// BassOysterCaskets will drop is no catch on the card. lastCount, the session's own
		// target, still counts every raw fish. Without Junk (ProgressiveFisher) every raw fish is
		// kept and the events are lastCount's own increments, as before.
		kept := 0
		if catch != nil && d.Junk != nil {
			kept = keptRawFish(d)
		}
		for time.Now().Before(deadline) {
			poll := 600 * time.Millisecond
			if !clickedAt.IsZero() && time.Since(clickedAt) < clickFastWindow {
				poll = clickFastPoll
			}
			if !manipWait(d, poll) {
				return catch.Done(lastCount)
			}
			// Before anything else, and before yielding to the Guardian: a whirlpool under a
			// click that crossed the change keeps rolling server-side until we move.
			if guardWhirlpool(d, clicked, clickedAt) {
				clickedAt = time.Time{}
				haveLastSpot = false
				stagger = true
				catch.Taint()
				break
			}
			if randomevents.Busy(d.Session) || d.Session.State().InCombat() {
				// Release the hold promptly so Guardian owns the next interaction. Whatever the
				// pack gains from here is the event's or the fight's, not the spot's: a Strange
				// box handed over mid-hold was being recorded as fishing loot (owner, 2026-09-20).
				catch.Taint()
				break
			}
			n := CurrentRawFish(d)
			if n > lastCount {
				manipCatch(d, n-lastCount)
				d.logf("caught one — %d raw fish in pack now", n)
				if d.Junk == nil {
					caughtSome(catch, n-lastCount)
				} else if catch != nil {
					k := keptRawFish(d)
					caughtSome(catch, k-kept)
					kept = k
				}
				lastCount = n
				still = time.Now()
				if lastCount >= target {
					return catch.Done(lastCount)
				}
			}
			// Junk to drop (Deps.Junk, never for ProgressiveFisher): end the hold here and let
			// the outer loop drop it and click the spot again.
			if KeptFull(d) {
				return catch.Done(lastCount)
			}
			if !junkOff && junkDue(d) {
				break
			}
			if PackFull(d) {
				d.logf("pack is full")
				return catch.Done(lastCount)
			}
			// Checked every poll of this hold, same cadence as CurrentRawFish just above —
			// see RecoverLostTool's own doc comment for why "the active rung's tool just
			// disappeared from the pack" is the real, reliable signal for a Big fish random
			// event (there is no chat line of its own to key off instead). No-ops instantly
			// while the tool is still held, so this costs one cheap pack scan per tick the
			// rest of the time.
			// A Big fish takes the tool and drops it; picking it back up is a pack GAIN that has
			// nothing to do with the spot, so either outcome throws this window away.
			if recovered, lost := RecoverLostTool(d, spotTile, rung.Tool); lost && !recovered {
				catch.Taint()
				return catch.Done(lastCount)
			} else if recovered {
				d.logf("re-finding a spot to resume fishing with the recovered %s", rung.Tool)
				catch.Taint()
				break
			}
			if !npcStillThere(d, spot) {
				d.logf("the spot moved or vanished — re-finding")
				stagger = true
				break
			}
			active := fishingActive(d.Session.State()) || manipAlive(d)
			if active {
				sawFishing = true
				still = time.Now()
			}
			if fishingHoldEnded(sawFishing, active) {
				d.logf("fishing action stopped — re-finding a spot")
				break
			}
			if time.Since(still) > 12*time.Second {
				d.logf("fishing inactive for 12s — re-finding a spot")
				break
			}
		}
		// The hold ended (a whirlpool, a random event, the spot moving, the catch stalling):
		// report what it yielded before the outer loop picks another spot, so the next hold's
		// window starts clean and nothing is attributed to the wrong spot.
		catch.Flush()
	}
	return lastCount
}

// orthogonalStand is where to stand to fish spot when layout.ApproachOrthogonal is set and the
// account is not already beside it (north, east, south or west): the walkable side tile a path
// reaches in the FEWEST STEPS from where the account is standing. ok is false when the layout
// does not ask for it, the account already stands beside the spot, or no side tile can be
// reached (the caller then walks as before).
//
// The four sides of a spot on open water are all one tile apart, so which one is picked looks
// like a detail — and on open water it is. It is not on a shoreline, which is the only place
// this is ever called: the near and far banks of a bay are two tiles apart across the water and
// thirty around it. This used to pick by straight-line distance from the account after running
// the very path search that could have answered properly, the same discarded-answer mistake
// pickSpot was making one layer up. Keeping the route length costs nothing here for the same
// reason it costs nothing there: FindPath has already run.
func orthogonalStand(d *Deps, layout fisherplan.Layout, spot tile.Tile) (tile.Tile, bool) {
	if !layout.ApproachOrthogonal || d.Pack == nil {
		return tile.Tile{}, false
	}
	here := currentTile(d)
	if here.Level == spot.Level && abs(here.X-spot.X)+abs(here.Z-spot.Z) == 1 {
		return tile.Tile{}, false
	}
	best, found := tile.Tile{}, false
	bestSteps := 0
	for _, o := range [4][2]int{{0, 1}, {1, 0}, {0, -1}, {-1, 0}} {
		t := tile.Tile{X: spot.X + o[0], Z: spot.Z + o[1], Level: spot.Level}
		if !d.Pack.Walkable(t.X, t.Z, t.Level) {
			continue
		}
		res := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level})
		if !res.OK {
			continue
		}
		steps := res.Steps()
		if !found || steps < bestSteps {
			best, bestSteps, found = t, steps, true
		}
	}
	return best, found
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// These sequence IDs come from content/pack/seq.pack: harpoon, lobster,
// big net, small net, casting and fishing on the spot. Animations refresh on
// unsuccessful rolls too, so a dry streak is not evidence fishing has stopped.
func fishingActive(s *gamestate.State) bool {
	seq, _ := s.SelfAnim()
	return seq >= 618 && seq <= 623 && s.Animating(12)
}

// Once server animation updates prove fishing started, cancellation or expired
// activity ends the hold. A newly clicked action still gets its existing startup
// grace; unsuccessful fishing rolls refresh animation and keep the hold alive.
func fishingHoldEnded(sawFishing, active bool) bool {
	return sawFishing && !active
}

// Bank at least half a pack of fish once, otherwise wait without opening a bank or fishing into
// a known hazard. Each retry returns to WaitForGuardian, so urgent events retain
// control and cancelled waits end the session immediately.
func blockedFishingStep(d *Deps, held int, bank func() (int, bool)) bool {
	if held >= 14 {
		d.logf("returning to bank: all fishing spots unsafe and %d raw fish held", held)
		if _, ok := bank(); ok {
			return true
		}
		d.logf("could not bank the catch while fishing spots are unsafe; waiting before retrying")
	}
	return !d.Delay(time.Second)
}

// OpenCatherbyBank walks to the Catherby bank and its booth and opens it, leaving it open
// for the caller to drive its own deposit/withdraw sequence — unlike BankFish (which does a
// fixed "deposit every raw fish, then close" pass), this is the shared primitive
// internal/bot/fisher.go's own bank/buy/sell phases each need, mirroring how
// ProgressiveFisher.ts's bankStep/buyStep/drawFishToSell all share one openBank() rather than
// each opening and closing the bank separately. Requires d.Components; fails clearly
// otherwise rather than opening a bank it can't actually read.
func OpenCatherbyBank(d *Deps, layout fisherplan.Layout) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	if !WalkNear(d, layout.Bank, 8) {
		return nil, false
	}
	beforeBank(d) // what is left of the last pack, tidied before the booth is clicked
	x, z, level, _ := d.Session.State().Tile()
	bank, ok := openNearestBankBooth(d, x, z, level, bankBoothOpenTimeout)
	if !ok {
		return nil, false
	}
	return bank, true
}

// openNearestBankBooth finds every "Bank booth" candidate near (x,z,level) and tries them
// nearest-first, actually opening one before giving up. Live-found 2026-09-16, this
// codebase's first real production run: the main account stood right next to a "Bank booth" the index
// placed at 2807,3442 and timed out on every single retry for minutes, while spare103's
// identical logic opened a different booth instance of the exact same type (id 2213) two
// and four tiles further along the same row (2809,3442 then 2811,3442) without issue. Since
// the static bake (internal/locindex's own doc comment, COLLAB.md 2026-09-13) can only ever
// be confirmed against a live server for locs that have actually changed — an untouched
// booth never sends a LOC_ADD_CHANGE either way — one specific tile in a row of otherwise-
// identical booths can be a bake artifact (extra, missing, or off-by-one from production's
// real placement) while its neighbors are all genuine. Retrying the SAME nearest match
// forever, as this used to, turns one bad tile into a permanent stranding; trying the next-
// nearest candidate instead recovers automatically the way spare103's own route past 2807
// already proves works.
func openNearestBankBooth(d *Deps, x, z, level int, timeout time.Duration) (*actions.Bank, bool) {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: "Bank booth", Op: "Use-quickly"}, live)
	if len(candidates) == 0 {
		d.logf("could not find a bank booth near the bank")
		return nil, false
	}
	candidates = orderBooths(candidates) // nearest first, a booth that rarely opens last (boothyield.go)
	const maxTries = 4
	yields := 0
	for i := 0; i < len(candidates); i++ {
		booth := candidates[i]
		if i >= maxTries {
			d.logf("gave up after %d bank booth candidates near %d,%d", maxTries, x, z)
			return nil, false
		}
		// The Guardian first: a bank trip that clicks over its work is the account's death (boothyield.go). A caller that holds the
		// action lock itself (BankFish) is not asked to wait: BusyForCaller is false for it.
		if randomevents.BusyForCaller(d.Session) {
			if !WaitForGuardian(d) {
				return nil, false
			}
		}
		// BankFish already owns Guardian's action lock here. A normal travel
		// walker would wait on that very lock and never reach the booth.
		if !freshstartrunner.WalkAdjacent(freshStartDeps(d), tile.Tile{X: booth.X, Z: booth.Z, Level: booth.Level}, 1, 8*time.Second) {
			continue
		}
		d.logf("OPLOC%d on bank booth %d at %d,%d (live=%v)", booth.Op, booth.ID, booth.X, booth.Z, booth.Live)
		mark := actions.MarkBankOpen(d.Session)
		if err := actions.OpLoc(d.Session, booth.Op, booth.X, booth.Z, booth.ID); err != nil {
			d.logf("OpLoc: %v", err)
			continue
		}
		bank := actions.NewBank(d.Session, d.Components)
		yielded := false
		// 8s -> 20s -> 30s -- live-found 2026-09-16: every timing constant in this file was
		// tuned against the local Lost City test engine (near-zero latency); production has
		// real round-trip and real playerbase, the same class of gap already found and fixed
		// once tonight for the sail-completion wait (freshstartrunner.go). See
		// bankBoothOpenTimeout's own doc comment for the 2026-09-18 evidence behind the
		// second bump and for why `timeout` (the caller-supplied budget) should always be
		// that same shared constant now, not a per-call-site literal.
		// The bank is handed over the moment the open's own inventories have arrived (AwaitOpen), not after a
		// poll at a tenth of a second and a fixed half-second pause on every visit (twopertick-1012).
		if open, _ := bank.AwaitOpen(mark, busyWaiter{Waiter: actions.DelayWaiter(d.Delay), d: d, yielded: &yielded}, timeout); open {
			noteBoothOutcome(booth, true)
			return bank, true
		}
		if yielded {
			// The Guardian has an event: its work comes first, and the same booth is clicked again once it is done.
			if yields++; yields > boothYieldMax {
				d.logf("bank booth %d at %d,%d: gave up after yielding to the Guardian %d times", booth.ID, booth.X, booth.Z, boothYieldMax)
				return nil, false
			}
			d.logf("bank booth %d at %d,%d: a random event began while waiting for the bank — yielding to it before clicking again", booth.ID, booth.X, booth.Z)
			if !WaitForGuardian(d) {
				return nil, false
			}
			i--
			continue
		}
		noteBoothOutcome(booth, false)
		if i+1 < len(candidates) && i+1 < maxTries {
			d.logf("bank booth %d at %d,%d never opened — trying the next-nearest booth instead", booth.ID, booth.X, booth.Z)
		}
	}
	return nil, false
}

// BankRead is what the bank held at the last look — coins, raw fish, and which ladder
// tools — mirroring ProgressiveFisher.ts's own BankRead interface, cached by the caller
// (internal/bot/fisher.go) across Loop calls the same way this.bank is cached there,
// since FishSnapshot's own BankCoins/BankTools/FishBanked fields must stay nil until a real
// read has happened, per DecideFishPhase's own doc comment ("nil until the bank has been
// read").
type BankRead struct {
	Coins int
	Fish  int
	Tools []string
	// Food is the bank's cooked food of any kind, so a fisher resting with none knows whether a
	// bank trip could restock it.
	Food int
	// Caskets is the unopened (un-noted) caskets banked: coins toward a lost tool.
	Caskets int
	// Rub is the Games necklaces and Rings of dueling banked (un-noted, any charge): the run
	// staller's own supply, so an account holding some never asks the mule for one.
	Rub int
	// Knife and Chisel are the tools banked, and Logs the logs the Fletching training cuts (plain, oak
	// and willow, un-noted), by display name: the account's own supply of the pearl tips' kit, so the
	// hand-off asks the mule only for what the bank lacks.
	Knife, Chisel int
	Logs          map[string]int
}

func isFood(name string) bool {
	_, ok := fighterfood.BaseFood(name)
	return ok
}

// ReadBank snapshots an already-open bank's contents — ported from readBank().
func ReadBank(d *Deps, bank *actions.Bank) BankRead {
	items := bank.Items()
	var r BankRead
	seen := map[string]bool{}
	for _, it := range items {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok {
			continue
		}
		switch {
		case strings.EqualFold(obj.Name, freshstart.Coins):
			c := it.Count
			if c < 1 {
				c = 1
			}
			r.Coins += int(c)
		case fisherplan.IsRawFish(obj.Name):
			c := it.Count
			if c < 1 {
				c = 1
			}
			r.Fish += int(c)
		case strings.EqualFold(obj.Name, "Casket") && !obj.Noted():
			c := it.Count
			if c < 1 {
				c = 1
			}
			r.Caskets += int(c)
		case !obj.Noted() && fleetplan.IsRubItem(obj.Name):
			r.Rub += int(max(it.Count, 1))
		case !obj.Noted() && strings.EqualFold(obj.Name, fisherplan.KnifeName):
			r.Knife += int(max(it.Count, 1))
		case !obj.Noted() && strings.EqualFold(obj.Name, fisherplan.Chisel):
			r.Chisel += int(max(it.Count, 1))
		case !obj.Noted() && fisherplan.IsFletchLog(obj.Name):
			if r.Logs == nil {
				r.Logs = map[string]int{}
			}
			r.Logs[strings.TrimSpace(obj.Name)] += int(max(it.Count, 1))
		case isFood(obj.Name):
			c := it.Count
			if c < 1 {
				c = 1
			}
			r.Food += int(c)
		default:
			for _, tool := range fisherplan.ToolNames() {
				if !seen[tool] && strings.EqualFold(obj.Name, tool) {
					seen[tool] = true
					r.Tools = append(r.Tools, tool)
				}
			}
		}
	}
	return r
}

// depositLeaves reports whether a bank deposit leaves the item called name in the pack: it is in the keep list, or it is
// un-noted and one of the run staller's jewellery, the pearl cut's chisel or the tick manipulation's knife.
func depositLeaves(d *Deps, kept map[string]bool, name string, noted bool) bool {
	return kept[strings.ToLower(name)] || (!noted && (d.Stall.KeepsName(name) || d.Cycle.keepsChisel(d, name) || d.Manip.KeepsName(name)))
}

// DepositExcept deposits every pack item not named in keep (case-insensitive) into an
// already-open bank, waiting for the bank's own list to reflect each deposit — ported from
// depositExcept. Returns how many distinct item ids were deposited.
func DepositExcept(d *Deps, bank *actions.Bank, keep []string) int {
	kept := make(map[string]bool, len(keep))
	for _, k := range keep {
		kept[strings.ToLower(strings.TrimSpace(k))] = true
	}

	// Every distinct id to deposit, with the counts its confirmation is judged against,
	// all sampled BEFORE any click. One id's deposit cannot change another's counts, so
	// sampling up front is equivalent to sampling per item and lets every click go first.
	type target struct {
		id     int32
		name   string
		before int32 // bank count before the deposit
		held   int32 // pack count before the deposit
	}
	var targets []target
	seen := map[int32]bool{}
	for _, it := range bank.Inventory() {
		if seen[it.ID] {
			continue
		}
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || depositLeaves(d, kept, obj.Name, obj.Noted()) {
			continue // the run staller's jewellery, the pearl cut's chisel and the tick manipulation's knife and logs stay in the pack
		}
		seen[it.ID] = true
		targets = append(targets, target{
			id:     it.ID,
			name:   obj.Name,
			before: bankCountByID(bank.Items(), it.ID),
			held:   packCountByID(bank.Inventory(), it.ID),
		})
	}
	if len(targets) == 0 {
		return 0
	}

	// Click every deposit, THEN wait once for all of them.
	//
	// This used to click one, wait up to 5s for that id to appear in the bank, then click
	// the next. Measured live 2026-09-21 on the 299-account fleet, the whole bank phase is
	// a median 10.0s and 4.24% of ALL fleet time, and the duration histogram (n=5,650) is
	// smooth with no pile-up on multiples of five -- 20.7% land on one, against the ~20%
	// chance gives -- so those seconds were NOT timeouts. They were genuine sequential
	// round trips, one per item type, and a fisher's pack holds several.
	//
	// The combined predicate is STRICTLY STRONGER than the per-item one it replaces: it
	// requires every id to have moved, not each in turn, so nothing can slip through a gap
	// between two waits. A single unbankable item now costs one shared 5s timeout instead
	// of its own, which is also an improvement on the old worst case.
	//
	// THE RISK THIS CARRIES, stated because it is the reason for the live proof:
	// actions.Bank.deposit resolves the item's SLOT when it is called, so a burst sends ops
	// carrying slot indices computed before any of them were processed. A classic inventory
	// does not re-pack on deposit -- removed slots become empty and the others keep their
	// index -- so they should all still resolve. "Should" is an assumption about the engine,
	// not about this code, which is why it is proven against a real one rather than argued.
	start := time.Now()
	fired := make([]target, 0, len(targets))
	for _, t := range targets {
		if err := bank.DepositAll(t.id); err != nil {
			d.logf("deposit %q: %v", t.name, err)
			continue
		}
		fired = append(fired, t)
	}

	// A note is banked as its item, a different id, so the pack losing it confirms the
	// deposit too; watching only the bank's count of the note's id timed out for 5s per note.
	done := func(t target) bool {
		return bankCountByID(bank.Items(), t.id) > t.before || packCountByID(bank.Inventory(), t.id) < t.held
	}
	waitFor(d, fmt.Sprintf("bank shows all %d deposit(s)", len(fired)), 5*time.Second, func() bool {
		for _, t := range fired {
			if !done(t) {
				return false
			}
		}
		return true
	})

	// Judged per target afterwards rather than taken from the combined wait, so the count
	// stays exact when one item refuses and the others went through.
	deposited, timedOut := 0, 0
	for _, t := range fired {
		if done(t) {
			deposited++
		} else {
			timedOut++
			d.logf("deposit %q never showed in the bank", t.name)
		}
	}
	if deposited > 0 || timedOut > 0 {
		d.logf("deposit: %d id(s) banked, %d timed out, %.1fs", deposited, timedOut, time.Since(start).Seconds())
	}
	return deposited
}

// WithdrawOneOf withdraws exactly one of name from an already-open bank into the pack,
// waiting for the pack to reflect it — ported from the "Withdraw-1" leg of bankStep's own
// tool top-up.
func WithdrawOneOf(d *Deps, bank *actions.Bank, name string) bool {
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !strings.EqualFold(obj.Name, name) {
			continue
		}
		before := packCountByID(bank.Inventory(), it.ID)
		if err := bank.WithdrawOne(it.ID); err != nil {
			d.logf("withdraw one %q: %v", name, err)
			return false
		}
		return waitFor(d, fmt.Sprintf("%s in pack after withdrawing", name), 3*time.Second, func() bool {
			return packCountByID(bank.Inventory(), it.ID) > before
		})
	}
	return false
}

// WithdrawAllOf withdraws every one of name from an already-open bank into the pack —
// ported from the "Withdraw-All" leg of bankStep's own coin top-up.
func WithdrawAllOf(d *Deps, bank *actions.Bank, name string) bool {
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !strings.EqualFold(obj.Name, name) {
			continue
		}
		before := packCountByID(bank.Inventory(), it.ID)
		if err := bank.WithdrawAll(it.ID); err != nil {
			d.logf("withdraw all %q: %v", name, err)
			return false
		}
		return waitFor(d, fmt.Sprintf("%s in pack after withdrawing", name), 3*time.Second, func() bool {
			return packCountByID(bank.Inventory(), it.ID) > before
		})
	}
	return false
}

func bankCountByID(items []gamestate.SlotItem, id int32) int32 {
	var n int32
	for _, it := range items {
		if it.ID == id {
			n += it.Count
		}
	}
	return n
}

// BankFish walks to the bank, finds its booth through the static loc index (the same
// name+op lookup barbarianrunner/cmd/fishtest already prove), opens it, and deposits
// every raw fish type currently held. Requires d.Components (the parsed interface
// archive) — fails clearly if nil rather than opening a bank it can't actually read.
func BankFish(d *Deps, layout fisherplan.Layout) (int, bool) {
	// Real live bug found 2026-09-15 (see COLLAB.md's "Strange box" entry): this used to
	// open the bank with no regard for whether Guardian was concurrently mid-solve on a
	// random event needing its own modal (e.g. solveStrangeBox's puzzle interface). Both
	// sides read/write the same account's single tracked MainModalID independently —
	// whichever one's click landed last "won" the value the OTHER one's own IsOpen()-style
	// check would see, so a box being solved while a bank trip ran concurrently could
	// never observe a stable, correct modal state.
	//
	// A one-shot WaitForGuardian() check here (this function's first fix) turned out NOT
	// to be enough — proven via cmd/boxracetest, built specifically to reproduce this:
	// forcing a box to spawn microseconds before this check finds nothing yet (Detect()
	// hasn't seen the server-side spawn), the check passes, the bank opens, and only
	// ~300ms later (half of Guardian's own 600ms poll interval) does Guardian catch the
	// box and start handling it — DURING the bank sequence, not before. A single gate at
	// the door cannot protect a sequence that can straddle a poll interval. Now holds
	// randomevents.Lock for this function's entire duration instead — a real mutex
	// against Guardian's own handling, not just a point-in-time check — closing the
	// window in both directions regardless of which side gets there first. Acquired right
	// before the booth click below, not at the top of this function — walking to the
	// bank touches no modal state, and holding the lock through a walk (which can take up
	// to WalkNear's own 120s budget) would needlessly block Guardian from reacting to an
	// unrelated, genuinely urgent event (a hazard, an evade) the whole time.
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return 0, false
	}
	d.logf("walking to the bank (%d,%d)", layout.Bank.X, layout.Bank.Z)
	if !WalkNear(d, layout.Bank, 8) {
		// Same stranding this session's own fishing-loop fix covers (see FishUntil's
		// noSpotStreak doc comment) — a displaced account trying to reach the bank hits
		// the identical "same-level WalkNear can never cross the water" wall. Confirmed
		// live: devlab5/devlab1's own "STRANDED: the destination leg has failed... aiming
		// for 2809,3441,0" (the Catherby bank itself) on an earlier run tonight.
		if !ReturnToCatherby(d) {
			return 0, false
		}
	}
	x, z, level, _ := d.Session.State().Tile()
	unlock := randomevents.Lock(d.Session)
	defer unlock()
	bank, ok := openNearestBankBooth(d, x, z, level, bankBoothOpenTimeout)
	if !ok {
		return 0, false
	}
	defer bank.Close()

	// Raw fish aren't stackable, so a catch of several sits in several separate slots —
	// bank.Inventory() lists one SlotItem per slot. DepositAll(id) already moves every
	// slot holding that id in one click, so each distinct id is handled once; a second
	// pass over an already-deposited id would just find nothing left to move.
	deposited := 0
	seen := map[int32]bool{}
	pack := bank.Inventory()
	for _, it := range pack {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !fisherplan.IsRawFish(obj.Name) || seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		heldBefore := int(packCountByID(pack, it.ID))
		before := bankCountByID(bank.Items(), it.ID)
		held := packCountByID(bank.Inventory(), it.ID)
		if err := bank.DepositAll(it.ID); err != nil {
			d.logf("deposit %q: %v", obj.Name, err)
			continue
		}
		// A note is banked as its item, a different id, so the pack losing it confirms the
		// deposit too; watching only the bank's count of the note's id timed out for 5 s per note.
		if waitFor(d, fmt.Sprintf("bank shows %s after deposit", obj.Name), 5*time.Second, func() bool {
			return bankCountByID(bank.Items(), it.ID) > before || packCountByID(bank.Inventory(), it.ID) < held
		}) {
			deposited += heldBefore
			d.logf("deposited %d %s", heldBefore, obj.Name)
		}
	}
	return deposited, true
}

// ---- stranded-across-water recovery ----
//
// Real, live-caught gap this session (see COLLAB.md's "Drunken Dwarf" entry): an account can
// end up displaced far from Catherby — a death respawn near Lumbridge being the confirmed
// live case, though the exact trigger is still being pinned down — and every fishing-phase
// helper above only ever attempts a same-level, same-map WalkNear. Once displaced across the
// Port Sarim<->Karamja water gap, that can never succeed: it just repeats "unreachable"
// forever, exactly the symptom observed live (freshtest3/freshtest1, 2026-09-15). This mirrors
// barbarianrunner.TravelToCourse/travelViaFreshStart — the identical fix already proven for
// the identical class of problem in an earlier session — adapted to fisherplan's own Road().

// ReturnToCatherby walks back to Catherby from wherever the account actually is. Tries a
// plain same-level WalkNear first (a few attempts, generous budget — covers an ordinary
// nearby pathfind hiccup); only once those are exhausted does it check for a genuine
// stranded-across-water displacement and fall back to freshstartrunner's own pickpocket+boat
// route. A displacement to a different level, or a disconnected area that isn't across
// water at all, still fails outright — not something this function invents a fix for.
func ReturnToCatherby(d *Deps) bool {
	const attempts = 4
	const perAttempt = 75 * time.Second
	dest := fisherplan.Catherby().Bank
	// start is only ever read by noteArrival, so the log line can say whether this recovery
	// actually went anywhere. See arrivalnote.go.
	start := currentTile(d)
	for i := 0; i < attempts; i++ {
		if here := currentTile(d); fisherplan.InCatherby(here) {
			noteArrival(d, "ReturnToCatherby", start, here, dest)
			return true
		}
		if WalkNear(d, dest, fisherplan.Catherby().BankRadius) {
			return true
		}
		d.logf("could not reach Catherby — retrying (attempt %d/%d)", i+1, attempts)
	}
	here := currentTile(d)
	if fisherplan.InCatherby(here) {
		noteArrival(d, "ReturnToCatherby, after its walk attempts", start, here, dest)
		return true
	}
	// Real, live-found gap this closes (2026-09-16, accounts "acct63"/"acct32", both
	// running ProgressiveFisher, mapsquare 40,50 — an ordinary Ardougne Monastery ground
	// floor, not a cross-water displacement at all): every attempt above only tries a
	// plain same-level WalkNear, which can never open a closed door. A small, genuinely
	// disconnected interior pocket whose real exit is a CHAIN of two or more doors (see
	// tryEscapeSealedPocket's own doc comment, sealedpocket.go, for the exact
	// ground-truthed door chain — the same incident freshstartrunner's own
	// tryEscapeSealedPocket was built for) looks identical to "unreachable" to every
	// attempt above, and the account never left the Ardougne mainland at all, so the
	// LegPier check just below never fires either — this used to fall straight through to
	// `return false`, an operator-only dead end for a stranding this package can actually
	// self-rescue from. Tried before the LegPier check since it's the cheaper, more
	// specific fix: an account genuinely across the water has no sealed-pocket door chain
	// to find at all, so this returns false immediately for that case, costing one
	// bounded room-BFS.
	if tryEscapeSealedPocket(d, here) {
		d.logf("stranded: escaped a sealed interior pocket via a real door chain — retrying the walk to Catherby")
		for i := 0; i < attempts; i++ {
			if at := currentTile(d); fisherplan.InCatherby(at) {
				noteArrival(d, "ReturnToCatherby, after a sealed-pocket escape", start, at, dest)
				return true
			}
			if WalkNear(d, dest, fisherplan.Catherby().BankRadius) {
				return true
			}
			d.logf("could not reach Catherby after the pocket escape — retrying (attempt %d/%d)", i+1, attempts)
		}
		here = currentTile(d)
		if fisherplan.InCatherby(here) {
			noteArrival(d, "ReturnToCatherby, after a sealed-pocket escape and its walk attempts", start, here, dest)
			return true
		}
	}
	if acrossWaterFromCatherby(here) {
		d.logf("stranded on the wrong side of the water at %d,%d,%d — falling back to freshstartrunner's pickpocket+boat route", here.X, here.Z, here.Level)
		return travelViaFreshStart(d)
	}
	return false
}

// ReturnToLayout is ReturnToCatherby for whichever area layout fishes: exactly
// ReturnToCatherby for the Catherby layout, and for any other (the Fishing Guild) a few
// door-aware walks to its bank, falling back to ReturnToCatherby when those fail — from
// Catherby the caller's own phase machine walks on to the Guild again.
func ReturnToLayout(d *Deps, layout fisherplan.Layout) bool {
	if layout.Bank == fisherplan.Catherby().Bank {
		return ReturnToCatherby(d)
	}
	const attempts = 3
	for i := 0; i < attempts; i++ {
		if tile.Chebyshev(currentTile(d), layout.Bank) <= layout.BankRadius {
			return true
		}
		if WalkNear(d, layout.Bank, 8) {
			return true
		}
		d.logf("could not reach the bank at %d,%d — retrying (attempt %d/%d)", layout.Bank.X, layout.Bank.Z, i+1, attempts)
	}
	if tile.Chebyshev(currentTile(d), layout.Bank) <= layout.BankRadius {
		return true
	}
	d.logf("could not reach the bank at %d,%d — recovering to Catherby instead", layout.Bank.X, layout.Bank.Z)
	return ReturnToCatherby(d)
}

// acrossWaterFromCatherby reports whether t is on a landmass that cannot walk to Catherby, so
// only a boat gets it home: either before the FIRST crossing (freshstart.LegPier — the
// Port Sarim side) or on Karamja, before the SECOND (the Brimhaven -> Ardougne ship).
//
// The Karamja half is the bug this closes. freshstart.TravelLeg was written for a fresh
// account's forward journey, where "you are on Karamja" means "your next leg heads for the
// destination", so it returns LegDestination for every Karamja tile. ReturnToCatherby's
// fallback tested only `== LegPier`, so an account stranded ON KARAMJA was treated as if it
// could simply walk home: it failed its four walks, returned false, and whoever called it
// retried the same dead end forever. travelViaFreshStart already knows how to take the
// Brimhaven ship (it is the same route a fresh fisher sails every day) — it just never
// got asked.
//
// Live 2026-09-19: a Mysterious Old Man fail-teleport dropped the mule `mule` at
// Brimhaven's northern dock (2772,3235). MuleReceiver.ensureHome goes through this function,
// logged "could not reach Catherby — retrying (attempt 1/4 … 4/4)" thirteen times over,
// never once reached the boat fallback, and the fleet completed zero trades for the entire
// run because every donor hands off through that one account.
func acrossWaterFromCatherby(t tile.Tile) bool {
	return freshstart.TravelLeg(t) == freshstart.LegPier || freshstart.OnKaramjaSide(t)
}

// freshStartDeps adapts Deps to freshstartrunner.Deps — same field-for-field copy
// barbarianrunner.freshStartDeps already does, this project's established convention.
func freshStartDeps(d *Deps) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay), Log: d.Log,
		Walker: d.Walker, Loot: d.Loot,
	}
}

// freshStartCoins counts coins in the player's pack — fisherrunner's own copy of
// barbarianrunner's/freshstartrunner's identical private helper, duplicated per this
// project's established per-package convention.
func freshStartCoins(d *Deps) int {
	n := 0
	for _, it := range d.Session.State().Inventory(PlayerPack).NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if ok && strings.EqualFold(obj.Name, freshstart.Coins) {
			c := it.Count
			if c < 1 {
				c = 1
			}
			n += int(c)
		}
	}
	return n
}

// anyObjIDIn reports whether items holds any of ids.
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

// freshStartSnapshot builds the freshstart.Snapshot fisherplan.DecidePhase reads for
// travelViaFreshStart, from live state — barbarianrunner.freshStartSnapshot's own twin.
func freshStartSnapshot(d *Deps) freshstart.Snapshot {
	main, _, _ := d.Session.State().Modals()
	s := freshstart.Snapshot{MainModal: main, Fare: freshstart.DefaultFare, SkirtCost: freshstart.DefaultSkirtCost}
	t := currentTile(d)
	s.Tile = &t
	s.Coins = freshStartCoins(d)
	eq := actions.NewEquipment(d.Session, d.Components)
	ids := d.Config.ObjIDs(freshstart.PinkSkirt)
	s.SkirtWorn = anyObjIDIn(eq.Worn(), ids)
	s.SkirtHeld = anyObjIDIn(eq.Inventory(), ids)
	// AN ESTABLISHED FISHER IS NOT A FRESH ACCOUNT, and must not be sent shopping.
	//
	// This snapshot feeds freshstart.SkirtStillToBuy, which answers "yes" for any account that
	// is not wearing one and is still on the Port Sarim side of the water -- true of EVERY
	// account that dies and respawns at Lumbridge, however long it has been playing. The skirt
	// is a first-day step; for an account that already fishes it is a cross-map errand to
	// Thessalia in Varrock for a cosmetic it will never use.
	//
	// Live 2026-09-22: acct25 (Big net, 18 fish caught this run) died, respawned at Lumbridge
	// and spent TWENTY MINUTES on "freshstart fallback: buying a pink skirt in Varrock",
	// walking 3223,3215 -> 3204,3417 over and over, before "gave up after 20 minutes" and
	// "could not recover back to Catherby -- giving up this fishing session". The whole
	// recovery budget went on the errand, and only the NEXT session got as far as the boat.
	//
	// Reported as already held rather than by changing SkirtStillToBuy: the predicate is
	// correct for the caller it was written for (a genuinely new account on its first day),
	// and this is the one caller for which its premise is false. A fisher that has caught a
	// fish has a Fishing level above 1, which is the cheapest honest tell available here.
	if FishingLevel(d) > 1 {
		s.SkirtHeld = true
	}
	return s
}

// travelViaFreshStart drives internal/scripts/freshstartrunner — built generic and
// script-agnostic anticipating more than one caller (see that package's own doc comment) —
// against fisherplan.Road(): pickpocket Men for the fare, buy and wear a pink skirt in
// Varrock, then sail Port Sarim -> Karamja -> Brimhaven -> Ardougne -> toward Catherby.
// Bounded at 20 minutes total, matching barbarianrunner.travelViaFreshStartTo's own budget
// for the identical class of genuine cross-map recovery.
func travelViaFreshStart(d *Deps) bool {
	fd := freshStartDeps(d)
	thief := freshstartrunner.NewThief()
	sail := freshstartrunner.NewSailProgress()
	dest := fisherplan.Catherby().Bank
	deadline := time.Now().Add(20 * time.Minute)
	// directRefused is set once the direct walk below fails although the pack says a path
	// exists. Since webwalk's automatic "white-wolf-dens" zone, that is exactly the Taverley
	// or Falador side: the only land path is over White Wolf Mountain, which the walker now
	// refuses, so the account must earn the fare and take the ships instead of retrying a
	// walk that can never be planned.
	directRefused := false
	// Read only by noteArrival below. See arrivalnote.go.
	start := currentTile(d)
	for time.Now().Before(deadline) {
		if here := currentTile(d); fisherplan.InCatherby(here) {
			noteArrival(d, "the freshstart fallback", start, here, dest)
			return true
		}
		// Direct-reachability short-circuit — same pattern as SailLeg's own fix (see its
		// doc comment) applied one layer up: fisherplan.DecidePhase (freshstart.DecidePhase
		// under Road()) computes PhaseFund/PhaseSkirt from freshstart.FareRemaining/
		// SkirtStillToBuy, which share freshstart.TravelLeg's ArdougneSideMaxX (x<2740)
		// blind spot — a position east of that threshold that ISN'T actually on Karamja
		// reads as "still needs the first ship," even when it's really just a small
		// pocket near Catherby's own landmass that a plain WalkNear happens to have failed
		// on for some OTHER reason (a wall/building splitting it from the bank's own
		// component, confirmed via cmd/doordiag to be a real, distinct case from a genuine
		// cross-water stranding — e.g. 2809,3350 is its own disconnected 2436-tile pocket
		// a few dozen tiles from the bank, nowhere near any ship). ReturnToCatherby only
		// reaches this fallback at all after real WalkNear attempts already failed, so
		// PhaseFund/PhaseSkirt's own wrong fare/skirt requirement would otherwise send an
		// account that needs no boat at all into pickpocketing/skirt-shopping that never
		// resolves (PickpocketSpotFor's own "away" classification just waits forever for a
		// Man that will never wander out here). Checking real pack connectivity to dest
		// first, before ever asking DecidePhase, closes this the same way SailLeg's own
		// entry check does — if a plain walk already gets there, skip Fund/Skirt/Travel
		// entirely and just walk.
		if d.Pack != nil && !directRefused {
			here := currentTile(d)
			if res := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level}); res.OK {
				d.logf("freshstart fallback: already on the destination side (no boat needed) — walking directly")
				if WalkNear(d, dest, fisherplan.Catherby().BankRadius) {
					return true
				}
				directRefused = true
				d.logf("freshstart fallback: the direct walk was refused (the only land path crosses White Wolf Mountain) — earning the fare and taking the ships instead")
				if !d.Delay(3 * time.Second) {
					return false
				}
				continue
			}
		}
		if !WaitForGuardian(d) {
			return false
		}
		snap := freshStartSnapshot(d)
		phase := fisherplan.DecidePhase(snap)
		d.logf("freshstart fallback: %s", fisherplan.DescribePhase(phase))
		switch phase {
		case freshstart.PhaseFund:
			// LOOK IN YOUR OWN BANK BEFORE STEALING. An established fisher that dies has a
			// bank, and the route to the ship already runs through Draynor -- acct25 had
			// 13,340 coins banked and pickpocketed twenty Men for 63 (2026-09-22). Throttled
			// and self-limiting: WithdrawFareFromOwnBank reports false whenever it did not
			// get to try, so this falls straight through to earning the fare and never
			// stands still. See its own doc comment.
			if _, took := freshstartrunner.WithdrawFareFromOwnBank(fd, freshstart.FundTarget(snap)); took {
				continue
			}
			freshstartrunner.PickpocketStep(fd, thief, freshstartrunner.PickpocketOpts{
				Target: freshstart.DefaultPickpocketTarget, Spot: freshstart.PickpocketSpot,
				RestBelow: freshstart.RestBelowHP, Need: freshstart.FundTarget(snap),
			})
		case freshstart.PhaseSkirt:
			freshstartrunner.BuySkirtStep(fd)
		case freshstart.PhaseTravel:
			freshstartrunner.SailLeg(fd, dest, sail)
		case freshstart.PhaseArrived:
			if WalkNear(d, dest, fisherplan.Catherby().BankRadius) {
				return true
			}
			d.logf("freshstart fallback: close to Catherby but could not finish the walk — retrying")
			if !d.Delay(3 * time.Second) {
				return false
			}
		default:
			d.logf("freshstart fallback: unexpected phase %q — waiting", phase)
			if !d.Delay(2 * time.Second) {
				return false
			}
		}
	}
	d.logf("freshstart fallback: gave up after 20 minutes")
	return false
}

// groundToolWait is how long RecoverLostTool keeps looking for the spat-out tool. Content drops
// it and deletes it from the pack in the same tick, but the client can apply the pack update a
// moment before the ground-item update. Live 2026-09-19: 18 of 39 losses logged "no matching
// item was found on the ground", every one within about a second of a Net click, while 21
// recoveries found it — so a single immediate look was losing a race it could win.
const groundToolWait = 3 * time.Second

// waitForGround polls find every 200ms until it finds something or wait has passed.
func waitForGround(find func() (gamestate.GroundItem, bool), delay func(time.Duration) bool, wait time.Duration) (gamestate.GroundItem, bool) {
	deadline := time.Now().Add(wait)
	for {
		if item, ok := find(); ok {
			return item, true
		}
		if !time.Now().Before(deadline) || !delay(200*time.Millisecond) {
			return gamestate.GroundItem{}, false
		}
	}
}
