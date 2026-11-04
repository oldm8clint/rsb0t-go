// Package questrunner is the I/O half of the AIOQuester quest-engine port: it drives the pure
// decisions in internal/scripts/questplan against a live session. Ported from the TypeScript
// client's src/bot/api/ai/quests/engine/QuestEngine.ts, exec/steps.ts, exec/primitives.ts and
// exec/prompts.ts. See docs/script_porting_framework.md for the split this follows.
//
// # What this package owns
//
// The whole live vocabulary a quest step is built from: walking to an NPC across a level
// change, opening a dialogue, driving one to completion by preference list, clearing the
// message boxes a quest script puts up, building the snapshot the pure decide() reads, and the
// engine loop with its watchdog and its parking.
//
// # Deliberately NOT ported here
//
//   - Prayer upkeep (prayer.ts). No ported quest prays yet.
//   - engine/trace.ts's HUD publishing.
//
// # The engine (2026-09-23, lane Q)
//
// engine.go is the one quest engine: every step kind in questplan, the provisioner
// (provision.go, the owner's item rule with a MuleAsker as its only door to the mule),
// sustain (eating between steps and in fights), the module's own journal stage, death
// counting, and the side store (store.go) that remembers attempts, deaths and parking across
// runs. Run below is kept for the two questport harnesses and is now a thin wrapper over it.
package questrunner

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// PlayerPack is the inventory interface component.
const PlayerPack = 3214

// PackSlots is the inventory size.
const PackSlots = 28

// Delayer is the sibling-runner delay signature: false means the bot was asked to stop.
type Delayer func(d time.Duration) bool

// RealDelay is the plain sleeping Delayer a cmd/ tool passes.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is the standard runner dependency set, plus the long-distance Walker a quest needs
// because its stops are in four different towns.
type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	Log        func(string)

	// Loot receives what the quest ladder's training rungs and side sources make (bow strings spun,
	// logs cut, daggers smithed, bows fletched) so the Loot tab counts them under the activity that
	// produced them; nil records nothing. Set from bot.Context.Loot.
	Loot func(loot.Drop)

	// Walker is the webwalk walker for cross-map travel. It may be nil, in which case
	// travel falls back to the same-level pathfinder, which cannot leave a town.
	Walker *webwalk.Walker

	// BeforeTravel, when set, runs before each long Walker leg (Travel). ScarabSwarm casts its
	// travel teleport here, so a quest leg from Ardougne to Duke Horacio starts in Lumbridge
	// instead of failing on foot: with wolf territory avoided, no walk joins Kandarin to
	// Asgarnia (3 Oct: 28 Rune Mysteries parks on "talk to Duke Horacio", "best 601").
	BeforeTravel func(dest tile.Tile)

	// KeepInPack, when set, names more items every bank deposit of the quest engine leaves in the pack, by object
	// name, whatever the step's own keep list says: Engine.keepFunc (a deposit step, a provisioning round) and
	// DepositExcept itself (the quest modules that deposit with a keep function of their own) both ask it. ScarabSwarm's
	// run staller keeps its Games necklace or Ring of dueling here, and the walk-alch kit beside it: before this every
	// quest bank visit banked the jewellery it rubs on the walks (live, 6 Oct 2026: "acct" walked Falador to
	// Burthorpe for Death Plateau with a Games necklace in the pack and rubbed it never).
	KeepInPack func(name string) bool

	// AtBank, when set, runs once at the end of every DepositExcept, the bank still open, with the waiter the
	// deposit used: ScarabSwarm draws the run staller's jewellery there, when the pack holds none and the bank
	// holds one (an account whose necklace sat in its bank carried it never, live 6 Oct: "acct"). The deposit is
	// the one place every quest bank visit that empties the pack passes through, and the pack is at its emptiest there.
	AtBank func(bank *actions.Bank, w actions.Waiter)

	// Escape, when set, is asked by the engine's eat loop (every 600 ms of every quest, from its own goroutine) before it eats, and
	// reports whether the account left instead: ScarabSwarm teleports out of a fight its bites cannot outrun on a lagging server
	// (scarabrunner.LagEscape, lagescape-1018). It must not walk: the step's own walk is running. Nil never leaves.
	Escape func() bool

	// eatMu serialises this account's eating (EatIfNeeded): the engine's sustain and a step's
	// own eating must not both send an Eat for one bite. It lives on the account's Deps, so it
	// is per-account and goes away with them (a map keyed by session state leaked every relog).
	eatMu sync.Mutex

	// eaten counts every bite EatIfNeeded has eaten for this account, whichever loop ate it. A fight
	// reads it to tell how many times the account has eaten since the target last lost health.
	eaten atomic.Int64

	// retreatMu guards retreatN and retreatWhy: the fights the account was losing and walked away
	// from (fightloop.go) since the engine last looked, and the last one's reason. The engine reads
	// and clears them after every step and counts each toward the quest's park logic, whatever the
	// step's own return value says.
	retreatMu  sync.Mutex
	retreatN   int
	retreatWhy string

	// retreatBank is where a lost fight walks the account to: the running quest's bank when it has
	// one (Engine.Run sets it), else the nearest bank to where the fight ended.
	retreatBank    tile.Tile
	retreatBankSet bool

	// noRetaliate is the running quest's Module.NoRetaliate: a fight leaves auto-retaliate alone.
	noRetaliate bool

	// travelFails counts the long walks (TravelFor) that ended without arriving. The provisioner reads it around a
	// source to tell "the walk to the source failed" from "the source gave nothing" (Provisioner.Provision).
	travelFails atomic.Int64

	// stalls counts the walks of the running quest that ended unreachable, stranded or not (travelstall.go); the
	// engine parks the quest when they say it cannot get where it is going.
	stalls travelStalls

	// leavingPocket is set while a Pocket's Leave runs, so its own walks are not hooked again.
	leavingPocket bool

	// bare counts the bare-pack stretches in force (BarePack): while it is above zero DepositExcept is DepositBare.
	bare atomic.Int32

	// walking counts the Travel legs under way (beginWalk): Engine.Run's eat loop takes a floor bite (EatFloor) only while one
	// is, so a fight or a dialogue step keeps the module's own sustain rules.
	walking atomic.Int32

	// toolProtect says which carried items are the running quest's own tools (Module.ToolIDs, Module.Tools), set by Engine.Run
	// as it starts: the floor bite never eats those (Merlin's bread, Death Plateau's trout). Unset, it protects nothing. It is
	// atomic because the previous Run's eat loop may still be inside a tick when the next Run sets it.
	toolProtect atomic.Pointer[func(id int) bool]

	// floorBiteAt is when EatFloor last ate, under eatMu; floorGap overrides questplan.FloorEatGap between two floor bites
	// (zero: the constant; a test shortens it).
	floorBiteAt time.Time
	floorGap    time.Duration

	// kitLegSaidAt paces holdRetaliateOff's log line (the quest's own goroutine only): a provisioning loop walks a kit leg
	// every few seconds.
	kitLegSaidAt time.Time
}

// isQuestTool reports whether id is one of the running quest's own tools (Deps.toolProtect).
func (d *Deps) isQuestTool(id int) bool {
	if f := d.toolProtect.Load(); f != nil {
		return (*f)(id)
	}
	return false
}

// beginWalk marks a Travel leg as under way for the engine's eat loop and returns the function that ends it (defer it).
func (d *Deps) beginWalk() (end func()) {
	d.walking.Add(1)
	return func() { d.walking.Add(-1) }
}

// BarePack opens a bare-pack stretch and returns the function that closes it (defer it). Inside the stretch every
// DepositExcept of this account is a DepositBare: it ignores KeepInPack and does not run AtBank, and Engine.keepFunc
// leaves KeepInPack out. A quest asks for one around the bank visits of a leg that refuses the run staller's kit
// (questplan.Step.Bare); stretches nest.
func (d *Deps) BarePack() (end func()) {
	d.bare.Add(1)
	return func() { d.bare.Add(-1) }
}

// InBarePack reports whether a bare-pack stretch is open.
func (d *Deps) InBarePack() bool { return d.bare.Load() > 0 }

func (d *Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

// noteRetreat records a fight the account was losing and walked away from.
func (d *Deps) noteRetreat(why string) {
	d.retreatMu.Lock()
	d.retreatN++
	d.retreatWhy = why
	d.retreatMu.Unlock()
}

// takeRetreats returns how many fights were given up since the last call and the last reason, and
// clears them.
func (d *Deps) takeRetreats() (int, string) {
	d.retreatMu.Lock()
	n, why := d.retreatN, d.retreatWhy
	d.retreatN, d.retreatWhy = 0, ""
	d.retreatMu.Unlock()
	return n, why
}

// retreatDest is where a lost fight walks to from here.
func (d *Deps) retreatDest(here tile.Tile) (tile.Tile, string) {
	if d.retreatBankSet {
		return d.retreatBank, "the quest's bank"
	}
	b := questplan.NearestBank(here)
	return b.Tile, "the " + b.Name + " bank"
}

func currentTile(d *Deps) tile.Tile {
	x, z, level, _ := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

// CurrentTile is where the account stands (the zero tile before the first position packet).
func CurrentTile(d *Deps) tile.Tile { return currentTile(d) }

// CurrentTileOK is CurrentTile with whether the position has been read.
func CurrentTileOK(d *Deps) (tile.Tile, bool) { return currentTileOK(d) }

func currentTileOK(d *Deps) (tile.Tile, bool) {
	x, z, level, ok := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}, ok
}

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

const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint

const guardianWaitLogEvery = 10 * time.Second

// WaitForGuardian mirrors every sibling runner's identical helper: hold everything while a
// random event is on screen, because an event's modal makes the engine refuse every
// interaction anyway.
func WaitForGuardian(d *Deps) bool {
	randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
	if !randomevents.Busy(d.Session) {
		return true
	}
	d.logf("[guardian] a random event is present; waiting")
	lastLog := time.Now()
	for randomevents.Busy(d.Session) {
		randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
		if !d.Delay(500 * time.Millisecond) {
			return false
		}
		if time.Since(lastLog) >= guardianWaitLogEvery {
			d.logf("[guardian] still waiting on a random event")
			lastLog = time.Now()
		}
	}
	return true
}

const walkNearTimeout = 120 * time.Second

// WalkNear walks to within radius of dest on the same level, using the local collision pack.
// Mirrors every sibling runner's identical helper.
func WalkNear(d *Deps, dest tile.Tile, radius int) bool {
	deadline := time.Now().Add(walkNearTimeout)
	for time.Now().Before(deadline) {
		here := currentTile(d)
		if here.Level == dest.Level && tile.Chebyshev(here, dest) <= radius {
			return true
		}
		res := d.Pack.FindPath(
			pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level},
			pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level})
		if !res.OK {
			d.logf("no path from %d,%d,%d to %d,%d,%d: %s", here.X, here.Z, here.Level, dest.X, dest.Z, dest.Level, res.Reason)
			return false
		}
		if len(res.Waypoints) < 2 {
			return true
		}
		if err := actions.Walk(d.Session, res.Waypoints, true); err != nil {
			d.logf("walk: %v", err)
			return false
		}
		lastX, lastZ, still := here.X, here.Z, time.Now()
		for time.Now().Before(deadline) {
			randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			cur := currentTile(d)
			if cur.Level == dest.Level && tile.Chebyshev(cur, dest) <= radius {
				return true
			}
			if cur.X != lastX || cur.Z != lastZ {
				lastX, lastZ, still = cur.X, cur.Z, time.Now()
			} else if time.Since(still) > 2*time.Second {
				break
			}
		}
	}
	d.logf("WalkNear gave up short of %d,%d,%d", dest.X, dest.Z, dest.Level)
	return false
}

// offTheMap reports whether a walk began (start, when known) or ended (end) on a tile the collision pack has no ground
// for: the mime's room, the maze, the surprise exam's classroom and the other places a random event sets an account
// down in. Live 16 Oct 2026 an account in the mime's room (2008,4764) had every walk it asked for end "start not
// walkable" or "unreachable".
func (d *Deps) offTheMap(start tile.Tile, startKnown bool, end tile.Tile) bool {
	if d.Pack == nil {
		return false
	}
	if startKnown && !d.Pack.Walkable(start.X, start.Z, start.Level) {
		return true
	}
	return end != (tile.Tile{}) && !d.Pack.Walkable(end.X, end.Z, end.Level)
}

// TravelTimeout is the budget for one long-distance leg. A quest leg can cross two kingdoms
// and a ship fare, so it is generous.
const TravelTimeout = 6 * time.Minute

// Travel walks to within radius of dest, using the long-distance Walker when one is wired and
// falling back to the local pathfinder when it is not.
func Travel(d *Deps, dest tile.Tile, radius int) bool {
	return TravelFor(d, dest, radius, TravelTimeout)
}

// TravelFor is Travel with its own budget, for a walk that must not run the whole TravelTimeout: one made under
// attack, where six minutes of retried crossings is a death (Merlin's keep, 6 Oct 2026).
func TravelFor(d *Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	return travelFor(d, dest, radius, timeout, false)
}

// TravelCautious is Travel for the legs no fight is wanted on: a trip to a bank or a shop for a quest's kit. The walk
// breaks off from a permanently-spawned hostile (a Giant bat, a White wolf) before it engages, and plans round an
// attacker rather than carrying on past it (webwalk.WalkOptions.AvoidHostiles, CombatOptions.FleeWhenAttacked), and it holds
// auto-retaliate off for the whole leg, the fares' pickpocketing before it included (holdRetaliateOff). Live,
// 7-8 Oct 2026: eleven of the 25 quest deaths were on "provision" legs (six on the walk to the Draynor bank for the
// Imp Catcher's beads, from accounts of combat level 28 to 30), and Travel's walks had neither: only RunWhenAttacked,
// which does not stop a hit, while every walk of the Scarab bot's own breaks off from hostiles.
func TravelCautious(d *Deps, dest tile.Tile, radius int) bool {
	return travelFor(d, dest, radius, TravelTimeout, true)
}

// holdRetaliateOff turns auto-retaliate off for a leg no fight is wanted on and returns what turns it back on (defer it).
// Content answers every melee attack on the account, a hit or a miss, with queue*(playerhit_n_retaliate) (npc_combat_melee.rs2
// playerhit_n_melee), and with the option on that script is p_opnpc(2) (auto_retaliate.rs2): the attacker becomes the account's
// interaction target, which drops the walk it was following. The walker's reclick cancels that and the next attack sets it
// again, so an attacker at the account's heels can keep it in reach for as long as it keeps attacking. Live 7 Oct 2026: two
// Imp Catcher accounts "stalled at" a tile or two for 80 and 120 seconds beside the Ardougne warrior women they had
// pickpocketed for the ship fare (hitpoints 18 of 29 and 18 of 28 when the staller stood aside, nothing eating) and died.
// retreatFromFight turns it off for the same reason ("a hit in the back does not turn it round"). Nothing is done when it is
// off already (the quest's own NoRetaliate, or an account that trains with it off): that is not this leg's to put back.
func holdRetaliateOff(d *Deps) (restore func()) {
	st := d.Session.State()
	if d.noRetaliate || !st.AutoRetaliateOn() {
		return func() {}
	}
	if err := actions.SetAutoRetaliate(d.Session, false); err != nil {
		d.logf("kit leg: auto-retaliate off: %v", err)
		return func() {}
	}
	waitQuietly(d, func() bool { return !st.AutoRetaliateOn() }, 2*time.Second)
	if time.Since(d.kitLegSaidAt) > 5*time.Minute {
		d.kitLegSaidAt = time.Now()
		d.logf("kit leg: auto-retaliate off for the walk, so a hit cannot turn the account round and cancel it")
	}
	return func() {
		if err := actions.SetAutoRetaliate(d.Session, true); err != nil {
			d.logf("kit leg: auto-retaliate back on: %v", err)
		}
	}
}

// travelOptions is the walk a Travel makes: it runs when attacked, and a cautious one also breaks off from hostiles
// before they engage and plans round an attacker.
func travelOptions(radius int, timeout time.Duration, cautious bool) webwalk.WalkOptions {
	return webwalk.WalkOptions{Radius: radius, Timeout: timeout, AvoidHostiles: cautious,
		Combat: webwalk.CombatOptions{RunWhenAttacked: true, FleeWhenAttacked: cautious}}
}

func travelFor(d *Deps, dest tile.Tile, radius int, timeout time.Duration, cautious bool) bool {
	here, ok := currentTileOK(d)
	if ok && here.Level == dest.Level && tile.Chebyshev(here, dest) <= radius {
		d.stalls.arrived(dest) // standing there is as good as a walk that arrived
		return true
	}
	defer d.beginWalk()()
	if cautious {
		defer holdRetaliateOff(d)()
	}
	if d.Walker == nil {
		return WalkNear(d, dest, radius)
	}
	if ok {
		leavePocketFor(d, here, dest)
	}
	if d.BeforeTravel != nil {
		d.BeforeTravel(dest)
	}
	res := d.Walker.WalkTo(
		webwalk.Point{X: dest.X, Z: dest.Z, Level: dest.Level},
		travelOptions(radius, timeout, cautious))
	if res.Arrived() {
		d.stalls.arrived(dest)
		return true
	}
	d.logf("travel to %d,%d,%d: %s (%s), best %d", dest.X, dest.Z, dest.Level, res.Outcome, res.Reason, res.Best)
	d.travelFails.Add(1)
	// A walk that failed while a random event was on, or that began or ended off the map's ground (the mime's room), is the
	// Guardian's, not a statement about the destination: live 16 Oct 2026 two Imp Catcher accounts were taken to the mime's
	// room and the maze in one hour and every walk they asked for from there ended unreachable. A stranded one is the
	// Guardian's by its own reason and still counts (strandedReason).
	if strandedReason(res.Reason) || !(randomevents.Busy(d.Session) || d.offTheMap(here, ok, tile.Tile{X: res.End.X, Z: res.End.Z, Level: res.End.Level})) {
		d.stalls.note(dest, res.Outcome, res.Reason)
	}
	// The walker can stop just outside its own radius on the last leg; the local walk is a
	// cheap second try that costs nothing when the account is already close.
	at := currentTile(d)
	if at.Level == dest.Level && tile.Chebyshev(at, dest) <= radius+8 {
		return WalkNear(d, dest, radius)
	}
	return false
}

// ---------------------------------------------------------------------------
// Dialogue
// ---------------------------------------------------------------------------

// dialogGapMs is how long a lull may last before a conversation counts as over.
//
// A page turn's worth of quiet. A stop whose content script has longer gaps than this needs its
// own driver and its own oracle: upstream's seer runs `if_close`, then three `mes` lines a
// p_delay(3) apart, and a 1.5s driver called that conversation over with nothing granted.
// Neither ported quest has a gap that long — checked against duke_horacio.rs2, sedridor.rs2,
// aubury.rs2 and barbarian_outpost_guard.rs2, whose longest pause is a single ~mesbox.
const dialogGapMs = 1800 * time.Millisecond

// dialogSteps bounds the driver. Sedridor's full first conversation is the longest either
// quest has — about twenty continues across three option menus — so this is roughly 5x the
// worst real case, which leaves room for a re-rendered page without letting a stuck dialogue
// spin forever.
const dialogSteps = 120

// Dialogue wraps the actions.ChatDialog with the helpers a quest driver needs.
type Dialogue struct {
	d    *Deps
	chat *actions.ChatDialog
}

// NewDialogue builds a Dialogue over a session.
func NewDialogue(d *Deps) *Dialogue {
	return &Dialogue{d: d, chat: actions.NewChatDialog(d.Session, d.Components)}
}

// IsOpen reports whether a chat dialogue is up.
func (dl *Dialogue) IsOpen() bool { return dl.chat.IsOpen() }

// CanContinue reports whether an unpressed continue button is up.
func (dl *Dialogue) CanContinue() bool { return dl.chat.CanContinue() }

// Texts is what the NPC is currently saying.
func (dl *Dialogue) Texts() []string { return dl.chat.Texts() }

// OptionTexts is the current menu's option labels.
func (dl *Dialogue) OptionTexts() []string {
	opts := dl.chat.Options()
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		out = append(out, o.Text)
	}
	return out
}

// Ready reports whether there is a dialogue to drive at all.
func (dl *Dialogue) Ready() bool { return dl.chat.IsOpen() || dl.chat.CanContinue() }

// Drive drives an open dialogue to completion, choosing options from prefer by text.
//
// Ported from exec/primitives.ts driveDialog, with one deliberate difference: upstream falls
// through to the LAST option when nothing matches, and this does not — it stops and says so.
// Upstream's own docs argue for the stricter behaviour (exec/prompts.ts's driveChoice exists
// precisely because "falling through to an unmatched option is worse than stopping"), and for
// two quests whose every menu is enumerated in questplan's tests, a miss means the content
// changed, not that a reasonable guess is available.
func (dl *Dialogue) Drive(prefer []string) bool {
	d := dl.d
	for i := 0; i < dialogSteps; i++ {
		if !dl.Ready() {
			if !waitQuietly(d, dl.Ready, dialogGapMs) {
				break
			}
		}
		if dl.chat.CanContinue() {
			if err := dl.chat.Continue(); err != nil {
				d.logf("dialogue continue: %v", err)
			}
			if !d.Delay(600 * time.Millisecond) {
				return false
			}
			continue
		}
		opts := dl.OptionTexts()
		if len(opts) > 0 {
			pick, ok := questplan.PickPreferred(opts, prefer)
			if !ok && slices.Contains(opts, "Nowhere.") {
				// The run staller's jewellery rub left its teleport menu open in front of this dialogue.
				pick, ok = "Nowhere.", true
				d.logf("dialogue: dismissing a jewellery teleport menu [%s]", strings.Join(opts, " | "))
			}
			if !ok {
				d.logf("no preferred option in [%s] — stopping rather than guessing", strings.Join(opts, " | "))
				return false
			}
			if err := dl.chat.ChooseOption(pick); err != nil {
				d.logf("choose %q: %v", pick, err)
				return false
			}
			if !d.Delay(1200 * time.Millisecond) {
				return false
			}
			continue
		}
		if !d.Delay(600 * time.Millisecond) {
			return false
		}
	}
	return !dl.chat.IsOpen()
}

// DriveSequence drives a dialogue whose menus must be answered in a fixed ORDER rather than by
// preference: the i-th option menu gets answers[i] (matched as a case-insensitive substring).
// Tree Gnome Village's ballista is the case: three menus with the same five options, answered
// 4, 3, 5. It reports how many menus it answered.
func (dl *Dialogue) DriveSequence(answers []string, budget time.Duration) int {
	d := dl.d
	answered := 0
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if !dl.Ready() {
			if !waitQuietly(d, dl.Ready, dialogGapMs) {
				break
			}
		}
		if dl.chat.CanContinue() {
			if err := dl.chat.Continue(); err != nil {
				d.logf("dialogue continue: %v", err)
			}
			if !d.Delay(600 * time.Millisecond) {
				break
			}
			continue
		}
		opts := dl.OptionTexts()
		if len(opts) == 0 {
			if !d.Delay(600 * time.Millisecond) {
				break
			}
			continue
		}
		if answered >= len(answers) {
			d.logf("a menu beyond the %d answers given: [%s]", len(answers), strings.Join(opts, " | "))
			break
		}
		pick, ok := questplan.PickPreferred(opts, []string{answers[answered]})
		if !ok {
			d.logf("no option %q in [%s]", answers[answered], strings.Join(opts, " | "))
			break
		}
		if err := dl.chat.ChooseOption(pick); err != nil {
			d.logf("choose %q: %v", pick, err)
			break
		}
		answered++
		if !d.Delay(1200 * time.Millisecond) {
			break
		}
	}
	return answered
}

// DriveUntil keeps answering until expect holds or the budget runs out. Ported from
// exec/prompts.ts driveUntil.
//
// It exists because a dialogue that ends is not a dialogue that achieved anything: the guard's
// card-issuing branch is two menus deep, and stopping at the first lull leaves the choice on
// screen with the tour un-started. The goal is tested BEFORE each pass so the state carrying
// the answer is still readable.
func (dl *Dialogue) DriveUntil(expect func() bool, prefer []string, budget time.Duration) bool {
	d := dl.d
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if expect() {
			return true
		}
		if dl.Ready() {
			if !dl.Drive(prefer) {
				return expect()
			}
		}
		// A quest script's mesbox lands on the MAIN modal, which no dialogue driver can
		// see, so a chain of them would leave DriveUntil spinning against a chat box that
		// is not there. Clear one per pass.
		dl.clearMessageBox()
		if !d.Delay(600 * time.Millisecond) {
			return expect()
		}
	}
	return expect()
}

// clearMessageBox dismisses one `~mesbox` if one is up.
//
// Both ported quests raise them at exactly the moments that matter — "The Duke hands you an air
// talisman.", "The head wizard gives you a package.", "Aubury gives you his research notes.",
// "The guard hands you a Barcrawl card" — and while one is up the engine's canAccess() is
// false, so every later interaction is silently refused, not just the next one.
func (dl *Dialogue) clearMessageBox() {
	st := dl.d.Session.State()
	main, _, _ := st.Modals()
	if main == gamestate.NoModal {
		return
	}
	if err := actions.CloseModal(dl.d.Session); err != nil {
		dl.d.logf("closing a message box: %v", err)
	}
}

// waitQuietly is waitFor without the timeout log line, for the many places a lull is expected.
func waitQuietly(d *Deps, cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(100 * time.Millisecond) {
			return false
		}
	}
	return cond()
}

// TabInventory and TabEquipment are the sidebar tab indices whose roots the server must have
// set up before it will accept a click on the backpack or the worn panel.
const (
	TabInventory = 3
	TabEquipment = 4
)

// packVisibleTimeout is how long PackVisible waits for the tabs to arrive.
const packVisibleTimeout = 20 * time.Second

// PackVisible waits until the server considers the backpack panel visible, and reports whether
// it ever did.
//
// This is not a politeness wait, and it is the single most expensive thing to get wrong in a
// quest that uses an item. The server gates OPHELD1-5, INV_BUTTON and the whole
// OPLOCU/OPNPCU/OPOBJU/OPHELDU use-on family on Player.isComponentVisible, which asks whether
// the component's root layer is currently in a modal or in a TAB. While tab 3 is null it is in
// neither, so everything in that family is rejected — and actions.Equipment refuses to send
// them at all in that state, deliberately, returning ErrNotOnScreen.
//
// Both ported quests need it. Barcrawl reads its card with an opheld and hands it to ten
// bartenders with an opnpcu, and Rune Mysteries' shortcut paths ([opnpcu,head_wizard] and
// [opnpcu,aubury]) are the same family. The first live barcrawl run failed here: the card's Read
// came back "required trade screen is not open" on the very first pass, which reads as a broken
// card and is really a tab the server had not finished setting up.
//
// Reading the pack is NOT gated the same way — actions.Equipment falls back to the fixed
// PlayerPack component for reads — so a caller can see a full pack and still be unable to click
// it. That asymmetry is exactly why this has to be asked explicitly rather than inferred from
// "the item is there".
//
// Several content scripts take the tabs away mid-run as well (~macro_events_set_tabs, both ship
// procs and nine more places all call if_settab(null, ...)), so this is not only a fresh-account
// concern: anything that waits here also rides out a ship crossing or a mime.
func PackVisible(d *Deps) bool {
	eq := actions.NewEquipment(d.Session, d.Components)
	if eq.Readable() {
		return true
	}
	st := d.Session.State()
	d.logf("the backpack panel is not visible to the server yet (tab %d root %d, tab %d root %d) — waiting up to %s",
		TabInventory, st.SideIcon(TabInventory), TabEquipment, st.SideIcon(TabEquipment), packVisibleTimeout)
	ok := waitQuietly(d, eq.Readable, packVisibleTimeout)
	if !ok {
		d.logf("the backpack panel never became visible (tab %d root %d, tab %d root %d); every opheld and use-on will be refused until it does",
			TabInventory, st.SideIcon(TabInventory), TabEquipment, st.SideIcon(TabEquipment))
	}
	return ok
}

// ---------------------------------------------------------------------------
// NPCs
// ---------------------------------------------------------------------------

// TalkOps are the op labels that open a conversation, in the order to try them.
var TalkOps = []string{"Talk-to", "Talk", "Speak-to"}

// FindNpc finds the NPC an NpcStop names, by exact ID when the stop gives one and by display
// name otherwise, within radius of the account.
//
// The ID path is not an optimisation. Seven of the ten barcrawl bartenders render the identical
// display name "Bartender" in this content build, and the outpost's attackable guard shares
// "Barbarian guard" with the gate guard, so a name lookup there picks whichever is nearest.
func FindNpc(d *Deps, stop questplan.NpcStop, radius int) (entityquery.Entity, bool) {
	q := entityquery.Npcs(d.Session.State(), d.Config).Within(radius)
	if stop.ID != 0 {
		q = q.Where(func(e entityquery.Entity) bool { return e.ID == stop.ID })
	} else {
		q = q.Name(stop.Npc)
	}
	return q.Nearest()
}

// npcOpenTimeout is how long an op gets to put a dialogue on screen.
const npcOpenTimeout = 8 * time.Second

// talkAttempts is how many times a talk op is re-sent before the stop is called unreachable.
const talkAttempts = 3

// OpenDialogue walks the last tile if needed and opens the NPC's conversation.
//
// Two engine behaviours shape this, both recorded in the vendored pitfalls and both reproduced
// by this client:
//
//   - An op that opens a dialogue does so a TICK LATER. Driving immediately finds nothing open
//     and starts a FRESH conversation with the same NPC, which lands in a dead-end line. So
//     this waits for the dialogue before returning, and never drives here.
//   - This engine runs clientRoutefinder, so an op sent at an out-of-reach NPC is simply
//     dropped — it will not walk the account into position. Hence the adjacency walk first.
func OpenDialogue(d *Deps, stop questplan.NpcStop) bool {
	dl := NewDialogue(d)
	if dl.Ready() {
		return true
	}
	for attempt := 0; attempt < talkAttempts; attempt++ {
		npc, ok := FindNpc(d, stop, stop.Leash)
		if !ok {
			d.logf("no %s within %d tiles to talk to", stop.Npc, stop.Leash)
			return false
		}
		// Walk adjacent: the engine will not route us there itself.
		at := tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}
		if !canTalkFrom(d, currentTile(d), at) {
			approachNpc(d, at)
			// Re-find: the NPC may have moved while we walked.
			npc, ok = FindNpc(d, stop, stop.Leash)
			if !ok {
				continue
			}
		}
		op, opOK := talkOpFor(npc)
		if !opOK {
			d.logf("%s offers no talk op (%v)", stop.Npc, npc.Actions())
			return false
		}
		if err := entityquery.Interact(d.Session, npc, op); err != nil {
			d.logf("%s %s: %v", op, stop.Npc, err)
			if !d.Delay(1200 * time.Millisecond) {
				return false
			}
			continue
		}
		if waitQuietly(d, dl.Ready, npcOpenTimeout) {
			return true
		}
		d.logf("%s did not open a dialogue on attempt %d/%d", stop.Npc, attempt+1, talkAttempts)
	}
	return false
}

// canTalkFrom reports whether the engine accepts an NPC op from here to an NPC on at: an
// orthogonal neighbour with no wall between (pathfind.Pack.CanInteractFrom). Without a pack it
// is the old Chebyshev-1 guess.
func canTalkFrom(d *Deps, here, at tile.Tile) bool {
	if d.Pack == nil {
		return tile.Chebyshev(here, at) <= 1
	}
	return d.Pack.CanInteractFrom(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level},
		pathfind.NavPoint{X: at.X, Z: at.Z, Level: at.Level})
}

// approachNpc walks onto a tile an NPC on at can be talked to from, nearest first, on foot and
// then, when no stand is reachable on foot, by the webwalker, which opens doors. King Roald
// wanders between two palace rooms: the questproof EMPTY lab run pip-empty-2 stood across the
// shut door 3219,3472, or diagonal to him, for an hour of "no path ... unreachable" and "I can't
// reach that!", because the walk was a plain one to within Chebyshev 1.
func approachNpc(d *Deps, at tile.Tile) bool {
	if d.Pack == nil {
		return WalkNear(d, at, 1)
	}
	here := currentTile(d)
	var stands []tile.Tile
	for _, p := range d.Pack.InteractTiles(pathfind.NavPoint{X: at.X, Z: at.Z, Level: at.Level}) {
		stands = append(stands, tile.Tile{X: p.X, Z: p.Z, Level: p.Level})
	}
	sort.SliceStable(stands, func(i, j int) bool { return tile.Chebyshev(here, stands[i]) < tile.Chebyshev(here, stands[j]) })
	if len(stands) > 4 {
		stands = stands[:4]
	}
	for _, s := range stands {
		if s != at && WalkNear(d, s, 0) {
			return true
		}
	}
	if d.Walker != nil {
		for _, s := range stands {
			if s != at && Travel(d, s, 0) && currentTile(d) == s {
				return true
			}
		}
	}
	return WalkNear(d, at, 1)
}

func talkOpFor(e entityquery.Entity) (string, bool) {
	for _, want := range TalkOps {
		for _, have := range e.Actions() {
			if strings.EqualFold(have, want) {
				return have, true
			}
		}
	}
	return "", false
}

// GotoNpc walks to a stop: across the underground boundary by ladder when needed, through the
// stop's approach waypoints, then to its anchor. It reports whether the NPC is within leash at
// the end. Ported from exec/primitives.ts gotoNpc.
func GotoNpc(d *Deps, stop questplan.NpcStop, hops []questplan.LadderHop) bool {
	here, ok := currentTileOK(d)
	if !ok {
		d.logf("no position for the account yet")
		return false
	}

	// The long walk goes FIRST, even when the anchor is across the underground boundary.
	//
	// This inverts upstream's order deliberately. exec/primitives.ts's gotoNpc always crosses
	// by hand first, because its navigator does not bake dungeon ladders. This one's does:
	// internal/webwalk/graph.json carries the Wizards' Tower crossings (and many others) as
	// ordinary `dungeon` edges, so the Walker routes through them itself, from anywhere on the
	// map, with the door in front of them already accounted for. Hand-rolling the crossing
	// first threw that away and stranded the account on the wrong side of that door.
	//
	// The hops are the FALLBACK, for a level change the graph does not bake.
	if d.Walker != nil && Travel(d, stop.Anchor, walkArriveRadius(stop)) {
		here = currentTile(d)
	}
	if questplan.NeedsHop(here, stop.Anchor) {
		crossed, crossedOK := CrossHops(d, here, stop.Anchor, hops)
		if !crossedOK {
			return false
		}
		here = crossed
	}

	for _, wp := range stop.Approach {
		if tile.Chebyshev(wp, here) > 2 || wp.Level != here.Level {
			if !Travel(d, wp, 2) {
				d.logf("could not reach the approach waypoint %d,%d,%d for %s", wp.X, wp.Z, wp.Level, stop.Npc)
				return false
			}
			here = currentTile(d)
		}
	}
	if tile.Chebyshev(stop.Anchor, here) > 1 || stop.Anchor.Level != here.Level {
		Travel(d, stop.Anchor, 1)
	}
	_, found := FindNpc(d, stop, stop.Leash)
	if !found {
		at := currentTile(d)
		d.logf("reached %d,%d,%d but no %s within %d tiles", at.X, at.Z, at.Level, stop.Npc, stop.Leash)
	}
	return found
}

// walkArriveRadius is how close the long walk aims for a stop. It is the stop's leash capped
// at a few tiles: aiming at the leash itself would stop the walk far enough away that the NPC
// is out of the scene, while aiming at the anchor exactly fails whenever the anchor tile is
// occupied — which, for an NPC's own anchor, it usually is.
func walkArriveRadius(stop questplan.NpcStop) int {
	if stop.Leash < 3 {
		return 1
	}
	return 3
}

// ---------------------------------------------------------------------------
// Level changes
// ---------------------------------------------------------------------------

// hopArriveTimeout is how long a ladder gets to land the account on the far side.
const hopArriveTimeout = 10 * time.Second

// hopArriveRadius is how close to a hop's stated Arrive tile counts as landed. The engine's own
// landing tile varies by a step or two depending on which tile the climb was sent from.
const hopArriveRadius = 5

// CrossHops takes whichever ladder is needed to get from here onto dest's side of the
// underground boundary, and reports where the account ended up. Ported from
// exec/primitives.ts crossHops.
func CrossHops(d *Deps, here, dest tile.Tile, hops []questplan.LadderHop) (tile.Tile, bool) {
	if !questplan.NeedsHop(here, dest) {
		return here, true
	}
	hop, ok := questplan.NearestHopFrom(here, hops)
	if !ok {
		d.logf("no hop from %d,%d toward z %d — trying the baked graph", here.X, here.Z, dest.Z)
		return here, true
	}
	walkTo := hop.WalkTarget()
	if tile.Chebyshev(hop.Stand, here) > 2 {
		if !Travel(d, walkTo, 2) {
			d.logf("could not reach the %s stand at %d,%d,%d", hop.LocName, walkTo.X, walkTo.Z, walkTo.Level)
			return here, false
		}
	}
	if !HopLadder(d, hop) {
		return currentTile(d), false
	}
	return currentTile(d), true
}

// HopLadder operates one ladder and confirms the landing. Ported from exec/primitives.ts
// hopLadder.
//
// The landing is confirmed rather than assumed because a refused climb produces a message box
// and no movement, which is indistinguishable from a slow one until the position is read.
func HopLadder(d *Deps, hop questplan.LadderHop) bool {
	find := func(op string) (entityquery.Entity, bool) {
		return entityquery.Locs(d.Session.State(), d.Config, d.Locs, 16).
			Name(hop.LocName).
			Action(op).
			Where(func(e entityquery.Entity) bool {
				return tile.Chebyshev(tile.Tile{X: e.X, Z: e.Z, Level: e.Level}, hop.Stand) <= 3
			}).
			Nearest()
	}
	ladder, ok := find(hop.Op)
	if !ok && hop.Open != "" {
		if closed, shut := find(hop.Open); shut {
			if err := entityquery.Interact(d.Session, closed, hop.Open); err == nil {
				d.Delay(1200 * time.Millisecond)
				ladder, ok = find(hop.Op)
			}
		}
	}
	if !ok {
		d.logf("no %q offering %q near %d,%d", hop.LocName, hop.Op, hop.Stand.X, hop.Stand.Z)
		return false
	}
	// The stand tile matters: this engine's reachedLoc refuses a diagonal for the shapes a
	// ladder uses, so get onto the hop's own stand when we are not already there.
	//
	// The walk is attempted but NOT required. A stand tile the local pathfinder calls
	// unreachable is usually a door away rather than genuinely sealed, and the engine will
	// answer "I can't reach that!" to the climb if we really are out of position — which is a
	// far better diagnostic than refusing to try.
	if at := currentTile(d); tile.Chebyshev(at, hop.Stand) > 0 || at.Level != hop.Stand.Level {
		if !WalkNear(d, hop.Stand, 0) {
			d.logf("could not stand exactly on %d,%d,%d for the %s — sending %s from %d,%d,%d anyway",
				hop.Stand.X, hop.Stand.Z, hop.Stand.Level, hop.LocName, hop.Op, at.X, at.Z, at.Level)
		}
	}
	if err := entityquery.Interact(d.Session, ladder, hop.Op); err != nil {
		d.logf("%s %s: %v", hop.Op, hop.LocName, err)
		return false
	}
	landed := waitQuietly(d, func() bool {
		at, ok := currentTileOK(d)
		return ok && at.Level == hop.Arrive.Level && tile.Chebyshev(at, hop.Arrive) <= hopArriveRadius
	}, hopArriveTimeout)
	if !landed {
		at := currentTile(d)
		d.logf("%s %s did not land near %d,%d,%d — still at %d,%d,%d",
			hop.Op, hop.LocName, hop.Arrive.X, hop.Arrive.Z, hop.Arrive.Level, at.X, at.Z, at.Level)
	}
	return landed
}

// ---------------------------------------------------------------------------
// Snapshot
// ---------------------------------------------------------------------------

// BuildSnapshot reads the live session into the pure Snapshot a module's decide() takes.
//
// journalName is the quest tab's own spelling. Bank state is supplied by the caller rather than
// read here, because reading it means opening a booth and neither ported quest needs one.
func BuildSnapshot(d *Deps, journalName string, noProgress int) questplan.Snapshot {
	st := d.Session.State()
	snap := questplan.Snapshot{
		Journal:    ReadStatus(d, journalName),
		Inv:        map[string]int{},
		InvIDs:     map[int]int{},
		Worn:       map[string]int{},
		WornIDs:    map[int]int{},
		NoProgress: noProgress,
	}
	at, ok := currentTileOK(d)
	snap.Tile, snap.TileKnown = at, ok

	eq := actions.NewEquipment(d.Session, d.Components)
	free := PackSlots
	for _, it := range eq.Inventory() {
		if it.ID <= 0 {
			continue
		}
		free--
		count := int(it.Count)
		if count < 1 {
			count = 1
		}
		snap.InvIDs[int(it.ID)] += count
		snap.Inv[itemName(d, int(it.ID))] += count
	}
	snap.FreeSlots = free
	for _, it := range eq.Worn() {
		if it.ID <= 0 {
			continue
		}
		count := int(it.Count)
		if count < 1 {
			count = 1
		}
		snap.WornIDs[int(it.ID)] += count
		snap.Worn[itemName(d, int(it.ID))] += count
	}
	_ = st
	return snap
}

// itemName resolves an object id to its lower-cased display name, falling back to the id so a
// missing config never silently reads as an empty name that matches nothing in particular.
func itemName(d *Deps, id int) string {
	if d.Config != nil {
		if obj, ok := d.Config.Obj(id); ok && obj.Name != "" {
			return strings.ToLower(obj.Name)
		}
	}
	return fmt.Sprintf("obj:%d", id)
}

// ReadStatus reads one quest's tab colour through internal/quests and converts it.
//
// Note the deliberate mapping of "not in the table" to StatusUnknown rather than to
// StatusNotStarted: a quest this client has no component id for is one we cannot see, and
// treating unseeable as not-started would restart a finished quest.
func ReadStatus(d *Deps, journalName string) questplan.Status {
	status, ok := quests.Status(d.Session.State(), journalName)
	if !ok {
		return questplan.StatusUnknown
	}
	switch status {
	case quests.NotStarted:
		return questplan.StatusNotStarted
	case quests.InProgress:
		return questplan.StatusInProgress
	case quests.Complete:
		return questplan.StatusComplete
	default:
		return questplan.StatusUnknown
	}
}

// QuestPoints reads the total quest-point varp (101), which IS transmitted and is therefore
// the one numeric quest fact this client can see.
func QuestPoints(d *Deps) int { return quests.Points(d.Session.State()) }

// ReadJournal opens the quest log and returns its rendered lines. It is the expensive oracle
// and is only used where nothing cheaper distinguishes a branch — for the two ported quests,
// only by the harness, as evidence.
func ReadJournal(d *Deps, journalName string) ([]string, error) {
	lines, err := quests.Journal(d.Session, d.Components, journalName, waiterFor(d))
	if err != nil {
		return nil, err
	}
	// The journal is a main modal, and while it is up every other modal read comes back
	// empty, which reads as "stage unavailable" and parks the quest one step later.
	if cerr := actions.CloseModal(d.Session); cerr != nil {
		d.logf("closing the quest journal: %v", cerr)
	}
	d.Delay(600 * time.Millisecond)
	return lines, nil
}

// waiter adapts a Deps to actions.Waiter.
type waiter struct{ d *Deps }

func waiterFor(d *Deps) actions.Waiter { return waiter{d} }

func (w waiter) Delay(dur time.Duration) bool { return w.d.Delay(dur) }

func (w waiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	return waitQuietly(w.d, cond, timeout)
}

func (w waiter) DelayTicks(n int) bool { return w.d.Delay(time.Duration(n) * 600 * time.Millisecond) }

// ---------------------------------------------------------------------------
// Step execution
// ---------------------------------------------------------------------------

// ExecuteStep performs one step. It returns false when the step did not do its job, which the
// watchdog will notice through the snapshot not changing rather than through this return —
// the return is only used for logging, because a step can legitimately succeed at clicking and
// still not advance the quest.
func ExecuteStep(d *Deps, step questplan.Step, hops []questplan.LadderHop) bool {
	if why := questplan.ValidateStep(step); why != "" {
		d.logf("refusing a malformed step: %s", why)
		return false
	}
	switch step.Kind {
	case questplan.StepTalk:
		if !GotoNpc(d, step.Stop, hops) {
			return false
		}
		if !OpenDialogue(d, step.Stop) {
			return false
		}
		dl := NewDialogue(d)
		var ok bool
		if step.UntilItemID > 0 {
			id := step.UntilItemID
			ok = dl.DriveUntil(func() bool { return HeldID(d, id) > 0 }, step.Stop.Prefer, 30*time.Second)
		} else {
			ok = dl.Drive(step.Stop.Prefer)
		}
		// A quest script's closing mesbox blocks everything that follows, so clear it
		// here rather than leaving the next step to discover it.
		ClearLeftoverModal(d)
		return ok
	case questplan.StepInteractLoc:
		return execInteractLoc(d, step)
	case questplan.StepUseOn:
		return execUseOn(d, step)
	case questplan.StepOpHeld:
		return execOpHeld(d, step)
	case questplan.StepEquip:
		return execEquip(d, step)
	case questplan.StepGrabGround:
		return execGrabGround(d, step)
	case questplan.StepPickLoc:
		return execPickLoc(d, step)
	case questplan.StepMineRock:
		return execMineRock(d, step)
	case questplan.StepBuy:
		return execBuy(d, step)
	case questplan.StepWalk:
		return TravelAvoiding(d, step.Anchor, step.Radius, toRects(step.Avoid))
	case questplan.StepWait, questplan.StepDone:
		return true
	default:
		// withdraw, deposit, scanBank, fight, provision and custom need the engine's state
		// (its bank view, provisioner, sustain and registered executors): Engine.execute.
		d.logf("step kind %q needs the engine (Engine.Run), not ExecuteStep", step.Kind)
		return false
	}
}

// ClearLeftoverModal dismisses a quest-complete scroll or message box left on the main modal.
//
// This is the engine's own housekeeping, and it is not optional: while any modal is up the
// engine's canAccess() is false, so every subsequent interaction is dropped with no refusal —
// the same mechanism internal/webwalk/walker.go documents for the outpost gate wedging an
// account for everything that followed, not just for the gate.
func ClearLeftoverModal(d *Deps) bool {
	st := d.Session.State()
	main, _, chat := st.Modals()
	if main == gamestate.NoModal && chat == gamestate.NoModal {
		return false
	}
	if err := actions.CloseModal(d.Session); err != nil {
		d.logf("closing a leftover modal: %v", err)
		return false
	}
	d.Delay(600 * time.Millisecond)
	return true
}

// ---------------------------------------------------------------------------
// The engine loop
// ---------------------------------------------------------------------------

// Outcome is how a Run ended.
type Outcome string

const (
	// OutcomeComplete means the quest tab now reads complete.
	OutcomeComplete Outcome = "complete"
	// OutcomeParked means the quest stopped making progress and gave up.
	OutcomeParked Outcome = "parked"
	// OutcomeTimeout means the budget ran out with the quest still in progress.
	OutcomeTimeout Outcome = "timeout"
	// OutcomeStopped means the bot was asked to stop.
	OutcomeStopped Outcome = "stopped"
)

// Result is what Run observed.
type Result struct {
	Outcome Outcome
	// Reason is the parking reason or the last step, for a log line.
	Reason string
	// Ticks is how many decide passes ran.
	Ticks int
	// LastStep is the step the engine was on when it finished.
	LastStep string
	// Progressed is whether the run moved the quest on: the journal stage rose, the quest was
	// started, or the run went through enough different steps (RunProgressed). It is what
	// separates a quest whose slot was simply too short from one that spun in place until the
	// slot ran out, and it is only read for a timeout.
	Progressed bool
}

// Run drives one module to completion, to parking, or to the budget.
//
// This is QuestEngine.execute's single-quest core. The queue, the eligibility gate and the
// provisioner around it are all available in questplan but are not driven here, because with
// one ported module a queue has nothing to choose between and would be untested scaffolding.
// WaitPark is how many consecutive identical wait steps are tolerated before the quest parks.
//
// A wait is deliberately exempt from the progress watchdog, because it changes nothing by design
// and should not burn that budget — but it needs a budget of its OWN, or a quest whose journal
// never loads waits for ever and the bot looks busy while achieving nothing. That is the exact
// failure the watchdog exists to prevent, reached through the one door the watchdog does not
// cover. Ported from QuestEngine.ts's separate WAIT_PARK counter.
const WaitPark = 15

func Run(d *Deps, module questplan.Module, budget time.Duration) Result {
	e := &Engine{D: d}
	return e.Run(Quest{Module: module}, budget)
}

// watchdogCount exposes the watchdog's current run length to the snapshot, so a module can
// rotate recovery probes on it (RuneMysteries does).
func watchdogCount(w *questplan.Watchdog) int { return w.Count() }
