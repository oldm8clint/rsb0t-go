package fleetrunner

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/alchplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/zonetrigger"
)

// Travel is one account's travel helpers and their state. A nil *Travel (Deps.Travel unset) does
// neither; so does a Travel whose settings are both off. One Travel per account, kept by the bot
// across loops; it is only ever used from that account's goroutine, the mutex is for the counters
// the bot reads to log.
type Travel struct {
	Alch fleetplan.WalkAlchSettings
	Tele fleetplan.TeleTravelSettings
	// Rub is the run-energy staller (scarabplan/runstall.go): an account that does not walk-alch
	// rubs a carried Games necklace or Ring of dueling every three ticks while it runs.
	Rub bool
	// Members is false only on a free world.
	Members bool
	// KeepRunning turns run back on during a walk while any energy is left, and eats a carried Strange
	// fruit (30% energy) when the energy is at or under FruitRunAt (a user request: the main account walked at
	// 29% energy with Strange fruit in the pack).
	KeepRunning bool
	// Tune is the lab's knobs: the zero value is what the fleet runs.
	Tune Tuning
	// Tick is the engine tick (600 ms when zero).
	Tick time.Duration
	// Now is the clock (time.Now when nil); a test sets it.
	Now func() time.Time

	mu sync.Mutex
	// runKeptAt is the run keeper's last check (runPoll).
	runKeptAt time.Time
	// lastCast is the last alch sent; pending is that cast's check, xpAt the Magic xp before it.
	lastCast time.Time
	pending  bool
	pendObj  int32
	xpAt     int
	misses   int
	// coinsAt is the pack's coins and pendStaff whether a fire staff was worn when the pending cast went out, for the
	// loot panel's Magic card (settleCast reports the cast).
	coinsAt   int
	pendStaff bool
	// lastPaid is when a cast last gave experience (settleCast).
	lastPaid time.Time
	// alchOffUntil pauses walk alch after repeated casts that gave nothing.
	alchOffUntil time.Time
	// refusedObj is an obj the server answered "You cannot use alchemy on that item." for (a
	// note, if this server refuses notes): never cast on again this session.
	refusedObj map[int32]bool
	// teleBlocked is a teleport the server refused, until when.
	teleBlocked map[string]time.Time
	// muleWalk is set for the length of the walk to the mule's post (WalkToMule): unless Tele.AnyWalk it
	// is the one walk a travel teleport may start (a user request).
	muleWalk bool
	// walkPrev is the account's tile at the last walk poll (walkPrevOK once there was one), for
	// the zone trigger its last step fired; pathBuf is zoneTriggerIn's reused scratch.
	walkPrev   tile.Tile
	walkPrevOK bool
	pathBuf    []tile.Tile
	// ench is the walk enchant (travelenchant.go).
	ench enchantWalkState
	// lastRub is the last rub sent; rubMenu that its teleport menu may still be open (closed when
	// the walk ends, CloseRubMenu); rubs counts them for the log.
	lastRub time.Time
	// topUpAt is the last bite stallTopUp sent.
	topUpAt time.Time
	rubMenu bool
	rubs    int
	// castsSent counts every cast sent (Casts counts the ones that paid), clears every clear-move; walk is the walk in
	// progress, for the line each long one logs (walklog.go).
	castsSent, clears int
	walk              walkLog
	// Casts/XP/Teleports/Saved are this session's counters, for the log.
	Casts, XP, Teleports, Saved int
	said                        map[string]time.Time

	// The teleport hook's state (telehook.go). lastTele is the last cast that landed (the loop guard); teleCasts the account's
	// casts in the last hour; Missed the walking casts it could not pay for would have saved (missedAt spaces the entries);
	// landMemo the landings priced lately; teleStrikes the casts in a row that moved nothing and said nothing; teleBlockLevel and
	// teleBlockRunes the spells refused for a level or a rune; est the walk the last cast predicted.
	lastTele       fleetplan.TeleCast
	teleCasts      fleetplan.SlidingCount
	Missed         fleetplan.MissedLedger
	missedAt       time.Time
	landMemo       map[landKey]landEntry
	teleStrikes    map[string]int
	teleBlockLevel map[string]int
	teleBlockRunes map[string]runeBlock
	est            walkEstimate
	castFailedAt   time.Time
	// MuleLaws is the mule's published law stock (false with no fresh reading), for the price a cast must clear
	// (scarabplan.LawPrice); nil is unknown. The bot sets it.
	MuleLaws func() (int, bool)

	// ignoreCombat is set while IdleAlchBetweenFights decides: the ten-tick combat window after the last hit does not
	// stop the cast (one goroutine drives a Travel, as everywhere else on it).
	ignoreCombat bool
	// ignoreHP is set while IdleAlchDuringStun decides: the hitpoint rule ("stand aside so a bite can land") does
	// not hold a cast back inside a stun, where nothing can hurt the account and the bite comes after the wait.
	ignoreHP bool
}

// Tuning is the lab's knobs (cmd/tickmanipprobe's walkalch mode); every zero value is what the fleet runs.
type Tuning struct {
	// NoClear sends each cast alone, without the clear-move packet in front of it (actions.ClearMoveRequest).
	NoClear bool
	// ZoneGate keeps casts off the ticks a zone trigger would hold up. It is what stood in for the clear-move before it
	// existed, and is always on with NoClear.
	ZoneGate bool
	// ClickSteps is how far the walk's clicks aim: 0 is scarabplan.WalkAlchClickSteps, 40 the walker's default.
	ClickSteps int
}

// zoneGated is whether casts are kept off the ticks a zone trigger would hold up: only when nothing clears the walk's
// moveClickRequest in front of them.
func (t *Travel) zoneGated() bool { return t.Tune.NoClear || t.Tune.ZoneGate }

func (t *Travel) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

func (t *Travel) TickLen() time.Duration {
	if t.Tick <= 0 {
		return 600 * time.Millisecond
	}
	return t.Tick
}

// Counters is the session's walk-alch casts and xp, teleports cast and the ticks they saved.
func (t *Travel) Counters() (casts, xp, teleports, saved int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Casts, t.XP, t.Teleports, t.Saved
}

// sayOnce logs line at most once every ten minutes per key, so a reason that holds for hours is
// one line, not one a walk.
func (t *Travel) sayOnce(d *Deps, key, line string) { t.sayEvery(d, key, 10*time.Minute, line) }

// sayEvery logs line at most once every `every` per key.
func (t *Travel) sayEvery(d *Deps, key string, every time.Duration, line string) {
	if t.said == nil {
		t.said = map[string]time.Time{}
	}
	if at, ok := t.said[key]; ok && t.now().Sub(at) < every {
		return
	}
	t.said[key] = t.now()
	d.Logf("%s", line)
}

// walkAside is the WalkOptions.Aside of one WalkTo, or nil when walk alch and the rub are off.
func (t *Travel) walkAside(d *Deps) func(webwalk.AsidePoll) {
	if t == nil || (!t.Alch.Enabled && !t.Rub && !t.KeepRunning && !t.enchantActive()) {
		return nil
	}
	return func(p webwalk.AsidePoll) {
		if t.KeepRunning {
			t.runPoll(d)
		}
		// The enchant owns the casts of a walk while a piece is left to cast on (travelenchant.go); once it is
		// done, or refused, the walk's alchemy and rub go on as they would.
		if t.enchantActive() && t.enchantPoll(d, p) {
			return
		}
		if t.Alch.Enabled || t.Rub {
			if t.stallTopUp(d) {
				return
			}
			t.alchPoll(d, p)
		}
	}
}

// stallTopUp eats one bite on a walk whose hitpoints hold the staller back (fleetplan.StallStandAside) while food is carried,
// and reports whether it ate. The walk's eater bites only under its own line, lower than the staller's 65%, so an account
// between the two walked unstalled and never ate: 2 Nov 2026 20:25-20:33, 19 walks stood aside at 50-64% of their hitpoints
// (owner, 2 Nov: "we want alch/jewelery stalls fully optimised and working across the board"). One bite clears the gate and
// the walk stalls again; food is plentiful (the mule holds over a million sharks). ChooseFood eats only what heals in full.
func (t *Travel) stallTopUp(d *Deps) bool {
	hp, maxHP := ownHitpoints(d)
	if !fleetplan.StallHPLow(hp, maxHP) || t.now().Sub(t.topUpAt) < WalkEatGap {
		return false
	}
	if main, _, _ := d.Session.State().Modals(); main != gamestate.NoModal {
		return false
	}
	choice, ok := fleetplan.ChooseFood(EatableFoods(d), hp, maxHP, 0)
	if !ok {
		return false
	}
	t.topUpAt = t.now()
	if !EatAnyFood(d, choice.Food) {
		return false
	}
	t.sayEvery(d, "stall-topup", 2*time.Minute, fmt.Sprintf("run staller: a bite of %s at %d/%d hitpoints so the walk can stall again", choice.Food, hp, maxHP))
	return true
}

// coinsID is the Coins obj (content 289 obj.pack).
const coinsID = 995

// FruitRunAt is the run energy (percent) at or under which KeepRunning eats a Strange fruit.
const FruitRunAt = 30

// runKeepEvery spaces the run keeper's checks; a bite also needs content's two-tick eat delay behind it.
const runKeepEvery = 2 * time.Second

// runPoll is KeepRunning's part of a walk poll: run on while there is energy, and a Strange fruit when it
// runs low. Nothing here moves the account; a bite clears the interaction, not the walk's path.
func (t *Travel) runPoll(d *Deps) {
	now := t.now()
	t.mu.Lock()
	if now.Sub(t.runKeptAt) < runKeepEvery {
		t.mu.Unlock()
		return
	}
	t.runKeptAt = now
	t.mu.Unlock()
	st := d.Session.State()
	if main, _, chat := st.Modals(); main != gamestate.NoModal || chat != gamestate.NoModal {
		return
	}
	energy, known := st.Energy()
	if !known {
		return
	}
	if energy <= FruitRunAt && StrangeFruitHeld(d) > 0 {
		if EatStrangeFruit(d) {
			t.sayEvery(d, "fruit", time.Minute, fmt.Sprintf("travel: ate a Strange fruit at %d%% run energy (%d left)", energy, StrangeFruitHeld(d)))
			energy += 30
		}
	}
	if energy >= 1 && !st.RunEnabled() {
		_ = actions.SetRun(d.Session, true)
	}
}

// alchPoll is one walk poll: settle the last cast's outcome, then cast if every gate passes.
func (t *Travel) alchPoll(d *Deps, p webwalk.AsidePoll) {
	// The step since the last poll first, whatever the gates say: a zone trigger it fired queued an
	// ENGINE script that a cast landing now would hold up (zonetrigger's doc comment).
	here := tile.Tile{X: p.Me.X, Z: p.Me.Z, Level: p.Me.Level}
	gated := t.zoneGated()
	crossed := gated && t.walkPrevOK && zonetrigger.Fires(t.walkPrev, here)
	if t.walkPrevOK {
		t.walk.tiles += tile.Chebyshev(t.walkPrev, here)
	}
	t.walkPrev, t.walkPrevOK = here, true
	now := t.now()
	running := d.Session.State().RunEnabled()
	ahead := tile.Chebyshev(tile.Tile{X: p.Me.X, Z: p.Me.Z}, tile.Tile{X: p.Target.X, Z: p.Target.Z})
	// A poll that sends no cast rubs the jewellery instead once the last cast's delay is over: alchemy paused after silent
	// casts, nothing alchable, a full pack, or the walk's end too near for a cast. Before 1 Nov 2026 such a walk was not
	// stalled at all: 1,664 of 1,995 "walk alch" walks held 0% of their ticks, against 321 of 993 rub walks.
	rub := func() {
		if t.gapOver(now) {
			t.RubPoll(d, now, p.SinceClick, ahead, running)
		}
	}
	if !t.alchReady(d, now) {
		if now.Before(t.alchOffUntil) {
			t.noteWhy("walk alch paused after refused or silent casts")
		}
		rub()
		return
	}
	// The cheap gates first, before anything reads the pack.
	if p.SinceClick < fleetplan.WalkAlchAfterClick {
		return
	}
	// An account that can walk-alch casts (the cast's own delay stalls the run too, and pays xp);
	// any other rubs its jewellery instead.
	if !t.Alch.Enabled || MagicLevel(d) < t.Alch.Normalised().MinMagic || CountPackNamed(d, alchplan.NatureRune) <= 0 {
		t.RubPoll(d, now, p.SinceClick, ahead, running)
		return
	}
	if ahead < fleetplan.WalkAlchAhead(running) {
		rub()
		return
	}
	// The clear-move in front of every cast (tryAlch) is what lets the walk go on through a zone trigger inside the cast's
	// delay, so the route is not scanned for triggers at all; without it (the lab's NoClear) the old gate stands.
	trig, window := 0, fleetplan.WalkAlchTriggerClear(running)
	if gated {
		trig = 1
		if !crossed {
			trig = t.zoneTriggerIn(here, p.Path, window)
		}
		if trig > 0 && trig <= window {
			return
		}
	}
	if !t.tryAlch(d, now, p.Me, p.SinceClick, ahead, p.LegEnd, trig, false, true) {
		rub()
	}
}

// RubPoll rubs a carried Games necklace or Ring of dueling once when scarabplan.ShouldRub says so:
// the run-energy staller of an account that does not walk-alch.
func (t *Travel) RubPoll(d *Deps, now time.Time, sinceClick time.Duration, ahead int, running bool) {
	if !t.Rub {
		return
	}
	st := d.Session.State()
	main, side, _ := st.Modals()
	hp, maxHP := ownHitpoints(d)
	poll := fleetplan.RubPoll{SinceClick: sinceClick, SinceRub: now.Sub(t.lastRub), Tick: t.TickLen(), Ahead: ahead,
		Running: running, InCombat: st.InCombat(), Modal: main != gamestate.NoModal || side != gamestate.NoModal,
		HP: hp, MaxHP: maxHP, NoFood: noFoodAtLowHP(d, hp, maxHP)}
	if !fleetplan.ShouldRub(poll) {
		if fleetplan.StallStandAside(hp, maxHP, poll.NoFood) {
			t.sayEvery(d, "stall-hp", 2*time.Minute, "run staller: standing aside at "+strconv.Itoa(hp)+"/"+strconv.Itoa(maxHP)+" hitpoints so a bite can land (a rub or a cast delays the player and the eat is refused)")
		}
		return
	}
	slot, ok := rubSlot(d)
	if !ok {
		return
	}
	if err := actions.OpHeld(d.Session, 4, slot.ID, slot.Slot, PlayerPack); err != nil {
		t.sayOnce(d, "rub-send", "run staller: rubbing the "+ObjName(d, slot.ID)+": "+err.Error())
		return
	}
	t.lastRub, t.rubMenu = now, true
	actions.NoteDelayTicks(d.Session, rubHeldTicks) // the rub's p_delay(1): an eat read inside it is dropped (actions.EatBite waits)
	t.mu.Lock()
	t.rubs++
	t.mu.Unlock()
}

// rubHeldTicks is how many ticks after a rub goes out the engine can still be dropping an eat: ring_of_dueling.rs2 and
// necklace_of_minigames.rs2 run p_delay(1), so the clicks of the two ticks after the one it ran in are refused, and one more for the
// click's own trip.
const rubHeldTicks = 2 + 1

// noFoodAtLowHP is the pack holding nothing to eat while the hitpoints are under the run staller's line, false above it: the pack is
// read only when the answer can matter, as the walk polls come every few hundred milliseconds. With nothing to eat the staller has no
// bite to make room for, so it neither stands aside nor says it does (scarabplan.StallStandAside; live 18 Oct 15:34:56 and 15:37:05, "standing
// aside at 18/55 hitpoints so a bite can land" on an account with no food).
func noFoodAtLowHP(d *Deps, hp, maxHP int) bool {
	return fleetplan.StallHPLow(hp, maxHP) && TotalFoodHeld(d) == 0
}

// ownHitpoints is the account's current and maximum hitpoints, 0 and 0 before the first stat packet.
func ownHitpoints(d *Deps) (hp, maxHP int) {
	st := d.Session.State()
	hp, ok := st.HP()
	if !ok {
		return 0, 0
	}
	maxHP, _ = st.MaxHP()
	return hp, maxHP
}

// rubSlot is the first rub item in the pack.
func rubSlot(d *Deps) (gamestate.SlotItem, bool) {
	for _, it := range PackSlotItems(d) {
		if fleetplan.IsRubItem(ObjName(d, it.ID)) {
			return it, true
		}
	}
	return gamestate.SlotItem{}, false
}

// RubItemHeld is whether the pack holds a Games necklace or Ring of dueling.
func RubItemHeld(d *Deps) bool {
	_, ok := rubSlot(d)
	return ok
}

// Rubs is the session's rubs, for the log.
func (t *Travel) Rubs() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rubs
}

// CloseRubMenu shuts the teleport menu the walk's last rub left up, so the next action does not find
// a dialogue open. Answering nothing teleports nowhere and uses no charge.
func (t *Travel) CloseRubMenu(d *Deps) {
	if t == nil || !t.rubMenu {
		return
	}
	t.rubMenu = false
	_ = actions.CloseModal(d.Session)
}

// zoneTriggerIn is the tiles along the walk's path from here to the first step that fires a zone
// trigger, looking within tiles ahead; 0 when none does. The path is the leg's route, which the
// account follows within a tile or two, so the scan starts from the route tile nearest it.
func (t *Travel) zoneTriggerIn(here tile.Tile, path []webwalk.Point, within int) int {
	near, best := 0, 1<<30
	for i, pt := range path {
		if dist := tile.Chebyshev(here, tile.Tile{X: pt.X, Z: pt.Z}); pt.Level == here.Level && dist < best {
			near, best = i, dist
		}
	}
	buf := append(t.pathBuf[:0], here)
	for _, pt := range path[min(near+1, len(path)):] {
		buf = append(buf, tile.Tile{X: pt.X, Z: pt.Z, Level: pt.Level})
	}
	t.pathBuf = buf
	return zonetrigger.FirstFiring(buf, within)
}

// IdleAlch casts High Level Alchemy once while the account stands waiting with nothing else to do
// (a user request: "alch ... inbetween waiting to kill npcs"): a safespot with no target in its
// reach. The gates are walk alch's but the walk's own (no movement click is due, nothing ahead to
// reach), so it never costs another skill's experience. It reports whether a cast went out, so the
// caller waits out the cast's p_delay before its next spell (one sent inside it is refused).
func (t *Travel) IdleAlch(d *Deps) bool { return t.idleAlch(d, false) }

// IdleAlchClocked is IdleAlch for a caller whose cast clock has already seen the engine's delay over (castclock.go):
// ticks, not the walk's wall-clock gap, say when the next alchemy cast may go, so a cast written 2.95 s after the
// last is not held back for the 3.0 s the gap asks.
func (t *Travel) IdleAlchClocked(d *Deps) bool { return t.idleAlch(d, true) }

// IdleAlchBetweenFights is IdleAlch for the melee trainer standing between two fights (a user request: alch the
// kills' loot while fighting): the cast goes out although the combat window (ten ticks from the last hit) is still
// open, because the caller has judged nothing living is on the account and no target is there to attack. A target
// clicked inside the cast's delay (IdleAlchWait) is refused, so the caller waits it out and only casts when the next
// spawn is not due back before it ends.
func (t *Travel) IdleAlchBetweenFights(d *Deps) bool {
	if t == nil {
		return false
	}
	t.ignoreCombat = true
	defer func() { t.ignoreCombat = false }()
	return t.idleAlch(d, false)
}

// IdleAlchDuringStun is IdleAlchBetweenFights for the endgame thief inside a failed pickpocket's stun (owner, 7 Oct
// 2026: "alch while stunned"). alchemy.rs2 never reads %stunned, the fail's own p_delay(0)s are over before the
// stun line, and the damage that comes with the failure is not combat, so neither the ten-tick combat window
// nor the low-hitpoint rule (which keeps a bite free for the NEXT thing that could hurt, and nothing can inside
// the stun) holds the cast back. The caller schedules the casts against the stun (scarabplan.StunCastFits) and
// waits each one out (IdleAlchWait).
func (t *Travel) IdleAlchDuringStun(d *Deps) bool {
	if t == nil {
		return false
	}
	t.ignoreCombat, t.ignoreHP = true, true
	defer func() { t.ignoreCombat, t.ignoreHP = false, false }()
	return t.idleAlch(d, false)
}

func (t *Travel) idleAlch(d *Deps, clocked bool) bool {
	if t == nil || !t.Alch.Enabled {
		return false
	}
	now := t.now()
	if !t.alchSettled(d, now) || (!clocked && !t.gapOver(now)) {
		return false
	}
	here := CurrentTile(d)
	if here == nil {
		return false
	}
	return t.tryAlch(d, now, webwalk.Point{X: here.X, Z: here.Z, Level: here.Level}, time.Hour, 1<<20, false, 0, clocked, false)
}

// IdleAlchWait is how long a caller waits after IdleAlch cast: the cast's p_delay(3) and the tick it
// ends on (scarabplan.WalkAlchCastTicks), so the next spell is not refused inside it.
func (t *Travel) IdleAlchWait() time.Duration {
	return time.Duration(fleetplan.WalkAlchCastTicks) * t.TickLen()
}

// alchSettled settles the last cast and reports whether alchemy is not paused after refusals.
func (t *Travel) alchSettled(d *Deps, now time.Time) bool {
	if t.pending {
		t.settleCast(d, now)
	}
	return !now.Before(t.alchOffUntil)
}

// gapOver is the last cast's delay over, by the wall clock.
func (t *Travel) gapOver(now time.Time) bool {
	return now.Sub(t.lastCast) >= time.Duration(fleetplan.WalkAlchCastTicks)*t.TickLen()
}

// alchReady settles the last cast and reports whether another may go out now: no pause after
// refusals, and the last cast's delay over.
func (t *Travel) alchReady(d *Deps, now time.Time) bool {
	return t.alchSettled(d, now) && t.gapOver(now)
}

// tryAlch reads the pack, decides (scarabplan.ShouldWalkAlch) and sends the cast on the best
// allow-listed alchable, reporting whether it went out. clocked says the caller's cast clock has checked the last
// cast's delay is over, so the decision's own wall-clock test of it is waived. walking is the run staller's cast on a walk: with nothing
// to eat the hitpoints gate has no bite to make room for and stands down (scarabplan.StallStandAside); the idle cast keeps its gate.
func (t *Travel) tryAlch(d *Deps, now time.Time, me webwalk.Point, sinceClick time.Duration, ahead int, legEnd bool, trig int, clocked, walking bool) bool {
	magic := MagicLevel(d)
	if magic < t.Alch.Normalised().MinMagic {
		return false
	}
	st := d.Session.State()
	// A bank or shop screen stops a cast; a chat box does not (a level-up message is one, and the
	// cast's own if_close shuts it).
	main, side, _ := st.Modals()
	pack := PackSlotItems(d)
	// A full pack with no coin stack has nowhere for the cast's coins: they would fall to the floor
	// (a user request). A single unnoted alchable empties its own slot when it is cast on, and the
	// coins land there (alchemy.rs2 deletes the item before adding the coins), so then only such items
	// are cast on; with none, no cast until a slot frees or coins are carried (803 pauses in three hours
	// on 1 Nov 2026, mostly full Queen and supply kits).
	castPack := pack
	if FreeSlots(d) == 0 && PackCountOf(d, coinsID) == 0 {
		castPack = nil
		for _, it := range pack {
			if it.Count <= 1 {
				castPack = append(castPack, it)
			}
		}
	}
	cands := t.walkAlchCandidates(d, castPack)
	if len(cands) == 0 && len(castPack) < len(pack) {
		t.noteWhy("the pack is full and holds no coins")
		t.sayEvery(d, "alch-full", 5*time.Minute, "walk alch: paused: the pack is full and holds no coins, so a cast's coins would drop to the floor")
		return false
	}
	alchables := 0
	for _, c := range cands {
		alchables += c.Count
	}
	runes := RuneCounts(d, pack)
	hp, maxHP := ownHitpoints(d)
	if t.ignoreHP {
		hp, maxHP = 0, 0
	}
	poll := fleetplan.WalkAlchPoll{
		HP: hp, MaxHP: maxHP, NoFood: walking && noFoodAtLowHP(d, hp, maxHP),
		Magic: magic, SinceClick: sinceClick, SinceCast: sinceCastFor(now.Sub(t.lastCast), clocked), Tick: t.TickLen(),
		Ahead: ahead, LegEnd: legEnd, ZoneTriggerIn: trig, Running: st.RunEnabled(),
		InCombat:   st.InCombat() && !t.ignoreCombat,
		Wilderness: fleetplan.WildernessLevel(tile.Tile{X: me.X, Z: me.Z}),
		Modal:      main != gamestate.NoModal || side != gamestate.NoModal,
		Natures:    runes[alchplan.NatureRune], Fire: runes["Fire rune"],
		Alchables: alchables,
	}
	if poll.Natures > 0 && alchables > 0 && poll.Fire-t.Alch.FireReserve < fleetplan.WalkAlchFirePerCast {
		poll.FireStaff = FireStaffWorn(d) // the equipment read only when it decides the cast
	}
	dec := fleetplan.ShouldWalkAlch(t.Alch, poll)
	if !dec.Cast {
		t.noteWhy(dec.Why)
		if fleetplan.StallStandAside(hp, maxHP, poll.NoFood) {
			t.sayEvery(d, "stall-hp", 2*time.Minute, "run staller: standing aside at "+strconv.Itoa(hp)+"/"+strconv.Itoa(maxHP)+" hitpoints so a bite can land (a rub or a cast delays the player and the eat is refused)")
		}
		return false
	}
	slot, ok := t.alchSlot(d, castPack, cands[0].Name)
	if !ok {
		return false
	}
	xp := MagicXP(d)
	if err := t.sendCast(d, pack, slot); err != nil {
		t.sayOnce(d, "alch-send", "walk alch: sending the cast: "+err.Error())
		return false
	}
	t.lastCast, t.pending, t.pendObj, t.xpAt = now, true, slot.ID, xp
	t.coinsAt, t.pendStaff = PackCountOf(d, coinsID), FireStaffWorn(d)
	t.castsSent++
	return true
}

// sinceCastFor is the time since the last cast the decision is given: the real one, or an hour when the caller's
// cast clock has already seen the delay over.
func sinceCastFor(since time.Duration, clocked bool) time.Duration {
	if clocked {
		return time.Hour
	}
	return since
}

// sendCast sends one High Alchemy cast on slot (sendSpell).
func (t *Travel) sendCast(d *Deps, pack []gamestate.SlotItem, slot gamestate.SlotItem) error {
	return t.sendSpell(d, alchplan.HighAlch.Name, pack, slot)
}

// CastOn sends one cast of spell ("Low Level Alchemy", "High Level Alchemy") on slot, the clear-move in front of it in the same write
// (sendSpell): the cast of a caller that has decided it itself and judges the outcome from the pack, the gate's casts on the temple's
// walking legs (runecraftplan/legcast.go). Nothing here counts the cast (settleCast is not armed), but the walk alchemy's clock is told, so
// an idle cast is not written inside the lock.
func (t *Travel) CastOn(d *Deps, spell string, slot gamestate.SlotItem) error {
	if t == nil {
		return fmt.Errorf("no travel to cast with")
	}
	if err := t.sendSpell(d, spell, PackSlotItems(d), slot); err != nil {
		return err
	}
	t.lastCast = t.now()
	return nil
}

// sendSpell sends one cast of spell on slot. In front of it, in the same write, goes the clear-move
// (actions.ClearMoveRequest): the engine reads both in one tick, in order, so the player is not delayed when it clears
// moveClickRequest and is delayed by the cast after, and a zone trigger the walk crosses inside the delay can no longer
// halt it. A pack with no slot to name wrongly sends the cast alone.
func (t *Travel) sendSpell(d *Deps, spell string, pack []gamestate.SlotItem, slot gamestate.SlotItem) error {
	if t.Tune.NoClear {
		return actions.CastOnItem(d.Session, spell, slot.ID, slot.Slot, PlayerPack)
	}
	b := actions.NewCoalescer(d.Session)
	held := make(map[int]bool, len(pack))
	for _, it := range pack {
		if it.ID == slot.ID {
			held[it.Slot] = true
		}
	}
	if err := actions.ClearMoveRequest(b.Session(), slot.ID, PlayerPack, func(s int) bool { return held[s] }); err == nil {
		t.clears++
	}
	if err := actions.CastOnItem(b.Session(), spell, slot.ID, slot.Slot, PlayerPack); err != nil {
		b.Discard()
		return err
	}
	return b.Flush()
}

// AlchKeeps is whether a bank deposit should leave name in the pack for walk alch: nature and fire
// runes and the allow-listed alchables, while walk alch is on and the account has the level. Before
// this every Stage 5 magic kit (PrepareTraining) and melee trip (the trainer's bank step) banked the
// mule's walk-alch kit with everything else: live on 6 Oct 2026 spare104's 200 noted Yew shortbows went
// into its bank within minutes of the hand-over, and it alched six times in a session.
//
// The allow-listed alchables stay only while the pack holds a Nature rune to cast them with (the nature rule,
// scarabplan/alchnatures.go; a user request: "if accounts dont have nature runes they shouldnt take things to
// alch"); a pack without one banks them at this visit. The runes stay whatever the pack holds.
func (t *Travel) AlchKeeps(d *Deps, name string) bool {
	if t != nil && t.Rub && fleetplan.IsRubItem(name) {
		return true // the run staller's jewellery stays in the pack too
	}
	if t.keepsTripRunes(d, name) {
		return true // the trip kit's laws, air and fire (scarabplan.TripKitLines) stay too
	}
	if !t.alchOn(d) {
		return false
	}
	n := strings.TrimSpace(name)
	if strings.EqualFold(n, alchplan.NatureRune) || strings.EqualFold(n, "Fire rune") {
		return true
	}
	for _, it := range t.Alch.ItemsRanked() {
		if strings.EqualFold(it, n) {
			// The stock stays only against Nature runes the pack holds (the nature rule, scarabplan/alchnatures.go): a
			// pack with none banks it at this visit.
			return fleetplan.StockBudget(1, CountPackNamed(d, alchplan.NatureRune)) > 0
		}
	}
	return false
}

// alchOn is whether walk alch is on for this account now: switched on and the Magic level reached. False for a nil Travel.
func (t *Travel) alchOn(d *Deps) bool {
	return t != nil && t.Alch.Enabled && MagicLevel(d) >= t.Alch.Normalised().MinMagic
}

// StockRanks is the alchemy stock's names, lower case, mapped to their rank on the allow-list (0 pays the most).
func (t *Travel) StockRanks() map[string]int {
	if t == nil {
		return nil
	}
	items := t.Alch.ItemsRanked()
	ranks := make(map[string]int, len(items))
	for i, it := range items {
		key := strings.ToLower(strings.TrimSpace(it))
		if _, dup := ranks[key]; !dup {
			ranks[key] = i
		}
	}
	return ranks
}

// CarryCasts is the walk carry (0 for a nil Travel).
func (t *Travel) CarryCasts() int {
	if t == nil {
		return 0
	}
	return t.Alch.CarryCasts()
}

// IsAlchStock is whether name is on the walk-alch allow-list: the stock a death loses, not the nature and fire runes
// that cast it. A qualified name ("Yew shortbow id857", how a mule ask names the strung bow) is read as the name it
// qualifies.
func (t *Travel) IsAlchStock(name string) bool {
	name = strings.TrimSpace(name)
	if base, _, qualified := cachecfg.SplitQualified(name); qualified {
		name = base
	}
	_, ok := t.StockRanks()[strings.ToLower(name)]
	return ok
}

// AlchKeepsNoStock is AlchKeeps for a bank visit that leads somewhere a death is likely (the melee trainer, a quest,
// a pen with no safespot): the runes, the jewellery and the laws stay, and every alchable goes into the bank
// (scarabplan/alchcarry.go says why).
func (t *Travel) AlchKeepsNoStock(d *Deps, name string) bool {
	return !t.IsAlchStock(name) && t.AlchKeeps(d, name)
}

// StockKeeper is the deposit predicate for a bank visit that kits the account for where: AlchKeeps' answer for
// everything but the alchemy stock, and for the stock the best-ranked slots whose casts total at most budget (a slot
// bigger than what is left is banked, and DrawAlchKitFor draws the budget's worth back). A budget of zero banks all of
// it, and so does a pack holding no Nature rune, whatever the budget (the nature rule, scarabplan/alchnatures.go: the
// Heroes thieves that kept 67 alchables with 0 natures). The choice is made once, from the pack as it is now, so a
// predicate called again by the second deposit pass agrees with the first. It says what it left behind in one line
// (scarabplan.CarryLine, scarabplan.NoNaturesLine).
func (t *Travel) StockKeeper(d *Deps, budget int, where string) func(gamestate.SlotItem) bool {
	if t == nil {
		return func(gamestate.SlotItem) bool { return false }
	}
	ranks := t.StockRanks()
	on := t.alchOn(d)
	var slots []fleetplan.StockSlot
	held := 0
	for _, it := range PackSlotItems(d) {
		name := ObjName(d, it.ID)
		rank, stock := ranks[strings.ToLower(strings.TrimSpace(name))]
		if !stock || !on {
			continue
		}
		slots = append(slots, fleetplan.StockSlot{Slot: it.Slot, Count: int(it.Count), Rank: rank})
		held += int(it.Count)
	}
	allowed := fleetplan.StockBudget(budget, CountPackNamed(d, alchplan.NatureRune))
	keep, kept := fleetplan.StockKeepSlots(slots, allowed)
	if held > kept {
		line := fleetplan.CarryLine(where, kept, held, allowed)
		if budget > 0 && allowed == 0 {
			line = fleetplan.NoNaturesLine(where, held)
		}
		t.sayEvery(d, "stock-carry-"+where, time.Minute, line)
	}
	return func(it gamestate.SlotItem) bool {
		name := ObjName(d, it.ID)
		if _, stock := ranks[strings.ToLower(strings.TrimSpace(name))]; stock {
			return keep[it.Slot] && on
		}
		return t.AlchKeeps(d, name)
	}
}

// tripRunes are the runes of the trip kit (scarabplan.TripKitLines) the pack keeps through a bank deposit and draws back from
// its own bank, with how many a cast of the dearest spell spends: the pack holds TripLaws casts' worth of each.
var tripRunes = []struct {
	name    string
	perCast int
}{{tripLawRune, 1}, {tripAirRune, 5}, {tripFireRune, 1}}

// keepsTripRunes is whether name is a rune of the trip kit the pack keeps through a bank deposit, for an account whose Magic
// opens a teleport (the lowest, Varrock at 25; it used to be Camelot's 45, the one that reaches the mule): laws, air runes when
// travel may spend them from the pack, and the fire runes of Varrock.
func (t *Travel) keepsTripRunes(d *Deps, name string) bool {
	if t == nil || !t.Tele.Enabled || t.Tele.Normalised().TripLaws <= 0 {
		return false
	}
	n := strings.TrimSpace(name)
	switch {
	case strings.EqualFold(n, tripLawRune), strings.EqualFold(n, tripFireRune):
	case strings.EqualFold(n, tripAirRune):
		if !t.Tele.AirRunes {
			return false
		}
	default:
		return false
	}
	return MagicLevel(d) >= fleetplan.LowestTeleportLevel()
}

// The runes the trip kit holds besides the law rune.
const (
	tripLawRune  = "Law rune"
	tripAirRune  = "Air rune"
	tripFireRune = "Fire rune"
)

// drawTripRunes takes the casts' runes the pack lacks out of the open bank, for the walks that follow: the training kit's draw,
// for the kit a mule ask banked or the kit's deposit would otherwise leave there. Own bank first: a rider only tops up what the
// bank does not hold.
func (t *Travel) drawTripRunes(d *Deps, bank *actions.Bank, w actions.Waiter) {
	if bank == nil {
		return
	}
	for _, r := range tripRunes {
		if !t.keepsTripRunes(d, r.name) {
			continue
		}
		want := t.Tele.Normalised().TripLaws*r.perCast - CountPackNamed(d, r.name)
		if want <= 0 {
			continue
		}
		for _, it := range bank.Items() {
			if it.Count <= 0 || ObjName(d, it.ID) != r.name {
				continue
			}
			if err := bank.QuickWithdrawX(it.ID, int32(min(want, int(it.Count))), w); err != nil {
				d.Logf("teleport travel: withdrawing %d %s: %v", min(want, int(it.Count)), r.name, err)
			}
			break
		}
	}
}

// DrawAlchKit tops the pack up from the open bank for walk alch: nature runes to the kit's casts,
// fire runes for them unless a fire staff is worn or carried, and the best allow-listed alchable the
// bank holds, drawn as notes (one slot) -- and only when the pack holds a Nature rune after the natures
// were drawn: with none in the pack or the bank the stock stays banked (scarabplan/alchnatures.go). Nothing
// while walk alch is off or below its level. The bank is left withdrawing items, as the callers expect.
func (t *Travel) DrawAlchKit(d *Deps, bank *actions.Bank, w actions.Waiter) {
	if t == nil {
		t.drawTripRunes(d, bank, w)
		t.DrawRubItem(d, bank, w)
		return
	}
	t.DrawAlchKitFor(d, bank, w, t.Alch.CarryCasts())
}

// DrawAlchKitFor is DrawAlchKit sized for budget casts: the visit's carry (CarryCasts for the walks, FightCarryCasts for
// a safespot pen, 0 where a death is likely and the stock stays banked). The bank is the only place a kit bigger than a
// trip's worth lives.
func (t *Travel) DrawAlchKitFor(d *Deps, bank *actions.Bank, w actions.Waiter, budget int) {
	t.drawTripRunes(d, bank, w)
	t.DrawRubItem(d, bank, w) // the run staller's jewellery, one, when the bank holds one and the pack none (walkprepare.go)
	if t == nil || !t.Alch.Enabled || MagicLevel(d) < t.Alch.Normalised().MinMagic || bank == nil || budget <= 0 {
		return
	}
	set := t.Alch.Normalised()
	kit := min(set.Kit, budget)
	natures, fire, alchables, fireStaff := t.WalkAlchCarried(d)
	items := bank.Items()
	draw := func(name string, want int) {
		if want <= 0 {
			return
		}
		for _, it := range items {
			if ObjName(d, it.ID) != name {
				continue
			}
			if n := min(want, int(it.Count)); n > 0 {
				if err := bank.QuickWithdrawX(it.ID, int32(n), w); err != nil {
					d.Logf("walk alch: withdrawing %d %s: %v", n, name, err)
				}
			}
			return
		}
	}
	draw(alchplan.NatureRune, kit-natures)
	if !fireStaff {
		draw("Fire rune", kit*fleetplan.WalkAlchFirePerCast+set.FireReserve-fire)
	}
	// The nature rule (scarabplan/alchnatures.go): no stock without a Nature rune to cast it with, in the pack (read again
	// after the draw above) or already there. It stays in the bank, and the next account with natures takes it.
	if fleetplan.StockBudget(kit, CountPackNamed(d, alchplan.NatureRune)) == 0 {
		t.sayEvery(d, "alch-kit-no-natures", 30*time.Minute, fleetplan.NoNaturesLine("the walk kit", 0))
		return
	}
	drawCoinSlot(d, bank, items, w)
	if alchables >= kit/4 {
		return
	}
	// The best-paying allow-listed alchables the bank holds, by the allow-list's own ranking, down the
	// ladder until the kit's casts are covered (a user request): for each name the obj a cast pays
	// most for (the strung bow, not the unstrung one that shares its name), drawn as notes, one slot a
	// kind, at most scarabplan.FillLinesMax kinds.
	left, kinds, drew, noted := kit-alchables, 0, 0, false
	for _, name := range set.ItemsRanked() {
		if left <= 0 || kinds >= fleetplan.FillLinesMax {
			break
		}
		best := NamedByCostDesc(d, items, name)
		if len(best) == 0 {
			continue
		}
		pick := best[0]
		n := min(left, int(pick.Count))
		if n <= 0 {
			continue
		}
		if !noted {
			if err := bank.SetNoteMode(true, w); err != nil {
				d.Logf("walk alch: switching the bank to notes: %v", err)
				return
			}
			noted = true
		}
		// The note of the obj drawn (a bank note of a bow names the same bow): the confirm waits for it.
		var err error
		if note := NotedID(d, pick.ID); note > 0 {
			err = bank.QuickWithdrawXAs(pick.ID, int32(n), note, w)
		} else {
			err = bank.QuickWithdrawX(pick.ID, int32(n), w)
		}
		if err != nil {
			d.Logf("walk alch: withdrawing %d noted %s: %v", n, ObjName(d, pick.ID), err)
			continue
		}
		left -= n
		drew += n
		kinds++
	}
	if noted {
		if err := bank.SetNoteMode(false, w); err != nil {
			d.Logf("walk alch: switching the bank back to items: %v", err)
		}
	}
	if drew > 0 {
		t.sayOnce(d, "alch-kit-bank", "walk alch: drew "+strconv.Itoa(drew)+" noted alchable(s) of "+strconv.Itoa(kinds)+" kind(s) from the account's own bank")
	}
}

// drawCoinSlot withdraws one coin when the pack holds none and the bank some, so the walk's casts always have a stack for their
// coins. A full pack with no coin stack pauses walk alch (tryAlch: a cast's coins would drop to the floor) and the walk goes
// unstalled: 2 Nov 2026 20:25-20:40, 35 accounts paused, most of them endgame accounts at Magic 69-72 that rub nothing (owner,
// 2 Nov: "we want alch/jewelery stalls fully optimised and working across the board").
func drawCoinSlot(d *Deps, bank *actions.Bank, items []gamestate.SlotItem, w actions.Waiter) {
	if PackCountOf(d, coinsID) > 0 {
		return
	}
	for _, it := range items {
		if it.ID != coinsID || it.Count <= 0 {
			continue
		}
		if err := bank.QuickWithdrawX(it.ID, 1, w); err != nil {
			d.Logf("walk alch: withdrawing a coin for the casts' coins: %v", err)
		}
		return
	}
}

// settleCast reads the outcome of the last cast once the server has had time to answer: experience
// gained is a cast; a refusal message pauses or bans; silence counts a miss, and three in a row pause
// walk alch for ten minutes. The answer is waited for four ticks, not two: the next cast is five away
// anyway, and on a link with a round trip near a second (a proxied account) the experience arrives
// after the second tick, which read as a miss and paused walk alch for ten minutes at a time on
// accounts whose casts had landed (live, 6 October 2026: 10 pauses in 75 minutes on 5 accounts).
func (t *Travel) settleCast(d *Deps, now time.Time) {
	if now.Sub(t.lastCast) < 4*t.TickLen() {
		return
	}
	t.pending = false
	if now.Sub(t.lastCast) > 12*t.TickLen() {
		return // the walk ended right after the cast; other casting since makes the reading unsafe
	}
	gained := MagicXP(d) - t.xpAt
	said := strings.ToLower(ChatSince(d, t.lastCast))
	switch {
	case gained > 0:
		t.mu.Lock()
		t.Casts++
		t.XP += gained
		t.mu.Unlock()
		t.misses = 0
		t.lastPaid = now
		t.reportCast(d)
	case strings.Contains(said, "cannot use alchemy"):
		if t.refusedObj == nil {
			t.refusedObj = map[int32]bool{}
		}
		t.refusedObj[t.pendObj] = true
		d.Logf("walk alch: the server refused alchemy on %s (obj %d%s); never casting on it again this session",
			ObjName(d, t.pendObj), t.pendObj, notedSuffix(d, t.pendObj))
	case alchplan.IsNotEnoughRunesMessage(said), alchplan.IsLevelTooLowMessage(said):
		t.alchOffUntil = now.Add(10 * time.Minute)
		t.sayOnce(d, "alch-refused", "walk alch: the server refused the cast ("+strings.TrimSpace(said)+"); pausing it for 10 minutes")
	default:
		t.misses++
		if t.misses >= 3 {
			t.misses = 0
			// An account whose casts are paying loses a few at a stun's edge or under a Hero's hit; ten minutes off for
			// those cost the Heroes loop about 7,000 of 18,600 account-minutes of alching on 1 Nov 2026 (705 pauses in
			// three hours). The long pause is kept for casts that have stopped paying altogether.
			if !t.lastPaid.IsZero() && now.Sub(t.lastPaid) < silentPauseLong {
				t.alchOffUntil = now.Add(silentPauseShort)
				return
			}
			t.alchOffUntil = now.Add(silentPauseLong)
			t.sayOnce(d, "alch-silent", "walk alch: three casts gave no experience and no message; pausing it for 10 minutes")
		}
	}
}

// reportCast tells the loot panel's Magic card about a walk, stun or idle High Level Alchemy that paid, as
// alchrunner.CastOnce does for its own casts: before 1 Nov 2026 only that loop reported, so the panel showed 4 accounts
// alching while 76 were. The item is named unnoted (a note is alched as its item); the coins are what the pack gained
// since the cast went out.
func (t *Travel) reportCast(d *Deps) {
	if d.Loot == nil || d.Config == nil {
		return
	}
	id := int(t.pendObj)
	if o, ok := d.Config.Obj(id); ok && o.Noted() && o.CertLink > 0 {
		id = o.CertLink
	}
	loot.Made(d.Loot, d.Config, loot.Alchemy(id, ObjName(d, int32(id)), true, t.pendStaff), 1,
		loot.Product(coinsID, "Coins", max(PackCountOf(d, coinsID)-t.coinsAt, 0)))
}

// silentPauseLong is the pause after three silent casts with none paying in that long; silentPauseShort the pause when
// one has.
const (
	silentPauseLong  = 10 * time.Minute
	silentPauseShort = 30 * time.Second
)

func notedSuffix(d *Deps, id int32) string {
	if d.Config == nil {
		return ""
	}
	if o, ok := d.Config.Obj(int(id)); ok && o.Noted() {
		return ", a bank note"
	}
	return ""
}

// walkAlchCandidates is the pack's allow-listed alchables, best first, by name: notes counted with
// their item, food never, an obj the server refused never.
func (t *Travel) walkAlchCandidates(d *Deps, pack []gamestate.SlotItem) []fleetplan.AlchCandidate {
	byName := map[string]*fleetplan.AlchCandidate{}
	var order []string
	for _, it := range pack {
		if t.refusedObj[it.ID] {
			continue
		}
		name := ObjName(d, it.ID)
		if name == "" {
			continue
		}
		edible := false
		if d.Config != nil {
			if o, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(o.IOps[0], "Eat") {
				edible = true
			}
		}
		key := strings.ToLower(name)
		v := fleetplan.HighAlchValue(ObjCost(d, it.ID))
		c, ok := byName[key]
		if !ok {
			c = &fleetplan.AlchCandidate{Name: name, Value: v}
			byName[key] = c
			order = append(order, key)
		}
		c.Count += max(int(it.Count), 1)
		c.Edible = c.Edible || edible
		if v < c.Value {
			c.Value = v
		}
	}
	held := make([]fleetplan.AlchCandidate, 0, len(order))
	for _, k := range order {
		held = append(held, *byName[k])
	}
	return t.Alch.RankWalkAlchables(held)
}

// alchSlot is the pack slot to cast on for name: the best-paying obj of that name (a strung bow
// before an unstrung one), skipping refused objs.
func (t *Travel) alchSlot(d *Deps, pack []gamestate.SlotItem, name string) (gamestate.SlotItem, bool) {
	var best gamestate.SlotItem
	bestCost, found := -1, false
	for _, it := range pack {
		if t.refusedObj[it.ID] || !strings.EqualFold(ObjName(d, it.ID), name) {
			continue
		}
		if c := ObjCost(d, it.ID); c > bestCost {
			best, bestCost, found = it, c, true
		}
	}
	return best, found
}

// questDone reads the quest tab's colour (quests.Status): only a green quest counts.
func questDone(d *Deps) func(string) bool {
	return func(name string) bool {
		s, ok := quests.Status(d.Session.State(), name)
		return ok && s == quests.Complete
	}
}

func (t *Travel) blocked(spell string) bool {
	until, ok := t.teleBlocked[spell]
	return ok && t.now().Before(until)
}

// travelCastWait is how long a travel teleport has to move the account: the click is handled
// on the next tick, then p_delay(1) and p_delay(0) before the telejump, about four ticks.
const travelCastWait = 6 * time.Second

// mayTeleport is whether the walk now starting may begin with a teleport: travel is on, and it is the walk to
// the mule's post (forMuleWalk) or the operator allowed any walk (Tele.AnyWalk). Every other long walk goes
// on foot: a law rune is the fleet's scarcest travel input (512 on the mule on 6 Oct 2026) and the owner's
// rule is "they should only teleport if they require to go back to the mule".
func (t *Travel) mayTeleport() bool {
	return t != nil && t.Tele.Enabled && (t.Tele.AnyWalk || t.muleWalk)
}

// ForMuleWalk runs walk as the walk to the mule's post: a travel teleport may start it. Safe on a nil Travel.
func (t *Travel) ForMuleWalk(walk func() bool) bool {
	if t == nil {
		return walk()
	}
	t.muleWalk = true
	defer func() { t.muleWalk = false }()
	return walk()
}

// teleportBeforeWalk casts the travel teleport ChooseTravelTeleport picks for a walk to dest, if
// any, and reports whether the account moved. A cast that does not move the account blocks that
// spell for scarabplan.TeleportRefusalBlocks and the walk goes on on foot.
func (t *Travel) teleportBeforeWalk(d *Deps, dest tile.Tile) bool {
	if !t.mayTeleport() {
		return false
	}
	here := CurrentTile(d)
	if here == nil {
		return false
	}
	if here.Z >= 3520 && here.Level == 0 {
		// The Wilderness (and the Mage Bank in it): a cast here is refused or cancelled, and a refusal would block the
		// spell for an hour of walks that are not in the Wilderness.
		return false
	}
	// The cheap gate first: a walk too short to save MinSave even with the best landing on its
	// doorstep never reads the pack or the quest tab.
	if fleetplan.TravelStraightTiles(*here, dest) < t.Tele.Normalised().MinSave {
		return false
	}
	staff, worn := StaffOnHand(d)
	if !worn {
		staff = ""
	}
	var carrying []string
	for _, it := range PackItems(d) {
		carrying = append(carrying, it.Name)
	}
	choice, ok := fleetplan.ChooseTravelTeleport(t.Tele, fleetplan.TeleTravelInput{
		Here: *here, Dest: dest, Magic: MagicLevel(d), Members: t.Members,
		Pack: PackRunes(d), Staff: staff, QuestDone: questDone(d), Blocked: t.blocked,
		Carrying: carrying,
	})
	if !ok {
		return false
	}
	if !WaitForGuardian(d) {
		return false
	}
	spell := choice.Teleport.Spell.Name
	land := choice.Landing()
	at := t.now()
	if err := actions.Teleport(d.Session, spell); err != nil {
		t.block(spell)
		d.Logf("teleport travel: %s: %v; walking", spell, err)
		return false
	}
	moved := WaitFor(d, "the travel teleport", travelCastWait, func() bool {
		cur := CurrentTile(d)
		return cur != nil && tile.Chebyshev(tile.Tile{X: cur.X, Z: cur.Z}, land) <= 4
	})
	if !moved {
		t.block(spell)
		why := strings.TrimSpace(ChatSince(d, at))
		if why == "" {
			why = "no move and no message"
		}
		d.Logf("teleport travel: %s from %s did not land (%s); not casting it for %s, walking", spell, HereIs(d), why, fleetplan.TeleportRefusalBlocks)
		return false
	}
	t.mu.Lock()
	t.Teleports++
	t.Saved += choice.Save / 2 // tiles run, two a tick: Saved counts ticks
	t.mu.Unlock()
	d.Logf("teleport travel: %s from %d,%d toward %d,%d: %s", spell, here.X, here.Z, dest.X, dest.Z, choice.Why)
	d.Delay(t.TickLen()) // the cast's last tick (anim reset) before the first walk click
	return true
}

// TeleportBeforeWalk is teleportBeforeWalk for a walk that does not go through WalkTo (a quest
// leg's questrunner.Travel). Safe on a nil Travel.
func (t *Travel) TeleportBeforeWalk(d *Deps, dest tile.Tile) bool {
	return t.teleportBeforeWalk(d, dest)
}

func (t *Travel) block(spell string) {
	if t.teleBlocked == nil {
		t.teleBlocked = map[string]time.Time{}
	}
	t.teleBlocked[spell] = t.now().Add(fleetplan.TeleportRefusalBlocks)
}

// KitKeepsNoted is whether a kit line of item is asked as bank notes and stays notes in the pack:
// walk alch's own stock, the allow-list (a qualified name, "Yew shortbow id857", is read as the name it
// qualifies). A note is cast on like the item (the tick lab: "noted bows alch fine") and takes one pack
// slot a line however many there are, so a kit of a dozen kinds fits the pack; un-noting it at the bank
// would turn the kit into a bank deposit and a few items drawn back. Safe on a nil Travel (false).
func (t *Travel) KitKeepsNoted(item string) bool {
	if t == nil || !t.Alch.Enabled {
		return false
	}
	base := strings.TrimSpace(item)
	if b, _, qualified := cachecfg.SplitQualified(base); qualified {
		base = b
	}
	for _, n := range t.Alch.ItemsRanked() {
		if strings.EqualFold(n, base) {
			return true
		}
	}
	return false
}

// TravelKeep is the items a kit deposit should leave in the pack for the travel helpers: the
// walk-alch allow-list (the runes and staffs KitKeep keeps already).
func (t *Travel) TravelKeep() []string {
	if t == nil || !t.Alch.Enabled {
		return nil
	}
	return t.Alch.ItemsRanked()
}

// WalkAlchCarried is what the walk-alch kit ask reads from the pack: natures, fire runes,
// allow-listed alchables, and a fire staff worn or carried.
func (t *Travel) WalkAlchCarried(d *Deps) (natures, fire, alchables int, fireStaff bool) {
	pack := PackSlotItems(d)
	runes := RuneCounts(d, pack)
	for _, c := range t.walkAlchCandidates(d, pack) {
		alchables += c.Count
	}
	return runes[alchplan.NatureRune], runes["Fire rune"], alchables, FireStaffWorn(d) || FireStaffInPack(d)
}

// QuestDoneFunc is the quest-tab reader the travel ask uses.
func QuestDoneFunc(d *Deps) func(string) bool { return questDone(d) }
