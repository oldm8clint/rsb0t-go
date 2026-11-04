package randomevents

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/pathfind"
)

// pollInterval mirrors RandomEventGuardian.ts's own cadence closely enough for a
// headless client without a frame loop to piggyback on — the original checks on every
// game tick (600ms) via BotHost's tick listener.
const pollInterval = 600 * time.Millisecond

// maxAttempts/giveUpCooldown port RandomEvents.ts's own MAX_ATTEMPTS(4)/
// GIVE_UP_COOLDOWN_MS(45000): a per-event-signature retry cap so an event whose handler
// keeps returning without actually clearing it (an unreachable NPC, a stuck dialogue, a
// maze door that never opens — see COLLAB.md's "maze worktree" entry for a real example
// of exactly this) doesn't spin forever at pollInterval. This closes a real, previously
// flagged gap: an earlier cross-project comparison (see COLLAB.md's "headlessreview"
// entry, citing rs-sdk's own CLAUDE.md) found this Guardian had no such protection,
// unlike the TS original it ports from. KindTrapped events (maze/mime) are exempted from
// the give-up/cooldown, matching RandomEventsImpl.handleInner's own TRAPPED_KINDS set —
// the player is physically stuck on that mapsquare regardless of whether the last attempt
// "counted", so retrying is the only option, not a choice to ignore it for a while.
const (
	maxAttempts    = 4
	giveUpCooldown = 45 * time.Second
	// maxGiveUpCooldown caps escalatedCooldown below — see its own doc comment.
	maxGiveUpCooldown = 15 * time.Minute
)

// escalatedCooldown multiplies giveUpCooldown by the number of CONSECUTIVE times this
// exact event signature has given up in a row without ever clearing in between, capped
// at maxGiveUpCooldown. Added 2026-09-15 after live production data showed a real,
// bounded-but-genuinely-permanent case the flat 45s cooldown handles poorly: an event
// NPC that can structurally never be cleared by this account at all (confirmed live —
// see COLLAB.md's "It's not here for you" entry: classic RS random events are personal
// to one account, and Detect()'s own name+distance matching has no concept of ownership,
// so a second account standing near someone else's stationary event NPC can chase it
// forever). Two accounts cycled "handling" -> "still present" -> "gave up... 45s" against
// the identical NPC continuously for over an hour real time before this fix — not
// dangerous (confirmed zero damage/death either account the whole time) but genuinely
// wasteful, and the flat 45s cooldown gave it no way to ever back off further.
//
// This does not fix the root cause (Guardian still has no way to know WHY an event keeps
// failing, whether it's a structurally-impossible ownership mismatch or just bad luck a
// few times in a row) — it only makes repeated, sustained failure against the same
// signature progressively cheaper to keep checking on, the same escalate-on-repeated-
// failure shape already proven in this package for HandleEvade's own fleeDistFor. A
// genuinely fixable event that just needed a bit more luck still gets the same fast 45s
// first retry as before — this only kicks in once something has already failed an entire
// 4-attempt cycle more than once in a row.
func escalatedCooldown(consecutiveGiveups int) time.Duration {
	d := giveUpCooldown * time.Duration(consecutiveGiveups)
	if d > maxGiveUpCooldown {
		return maxGiveUpCooldown
	}
	return d
}

// Retries belong to an instance, not every NPC with the same display name.
func eventSignature(ev *Event) string {
	if ev.NpcIndex >= 0 {
		return fmt.Sprintf("%d:%s:npc:%d:%d", ev.Kind, ev.Name, ev.NpcIndex, ev.NpcType)
	}
	if ev.Kind == KindBoxLamp {
		return fmt.Sprintf("%d:%s:item:%d:%d:%d", ev.Kind, ev.Name, ev.InvComID, ev.Slot, ev.ItemID)
	}
	if ev.Kind == KindHazard {
		return fmt.Sprintf("%d:%s:loc:%d:%d", ev.Kind, ev.Name, ev.ThreatX, ev.ThreatZ)
	}
	return fmt.Sprintf("%d:%s", ev.Kind, ev.Name)
}

type npcIdentity struct{ index, typeID int }

// Guardian is the Go equivalent of RandomEventGuardian: a single-flight background
// poller that detects and handles a random event on its own, independently of
// whatever a script's own loop is doing — matching the original's reason for existing
// (host-scoped waits keep random-event handling alive between script loops and while
// a script is paused). Only the numeric-id-detectable event subset (see detect.go's
// doc comment) is currently handled.
type Guardian struct {
	session  actions.Session
	pack     *pathfind.Pack
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	inFlight atomic.Bool
	// handling is true while the poll goroutine runs a handler (kick), not while a script holds Lock; holdTailUntil
	// (unix nanoseconds) is how long past a handler the account's input stays held; lockHolder is the goroutine that
	// holds the action lock through Lock or TryLock (0 for none). They are the hold's inputs: see inputhold.go.
	handling      atomic.Bool
	handlingSince atomic.Int64
	holdTailUntil atomic.Int64
	lockHolder    atomic.Uint64
	hold          atomic.Pointer[actions.MovementHold] // the hold registered for the session, released when the Guardian is
	// plants are the Strange plants this guardian has engaged: Busy holds the script for each through its dangerous
	// stretch whatever type it is by then (inputhold.go).
	plants plantWatches
	// actionMu is the account-wide mutual-exclusion lock between Guardian's own handling
	// and a script's modal-touching action sequence (bank, boat-boarding dialogue, etc.)
	// — see the package-level Lock's own doc comment for the real live bug this closes,
	// and why inFlight alone (a status flag, checked once) isn't sufficient.
	actionMu sync.Mutex

	mu                 sync.Mutex
	attempts           map[string]int
	firstAttempt       map[string]time.Time // when this run of attempts began; see noteAttempt
	cooldownUntil      map[string]time.Time
	consecutiveGiveups map[string]int
	// combatFleeStreak counts consecutive kick() polls that found InCombat() true, reset
	// to 0 the moment a poll finds it false — see kick()'s own in-combat branch for why
	// this needs to escalate the flee distance rather than repeat the same fixed one.
	combatFleeStreak int
	// combatFleeYieldUntil is set once combatFleeStreak crosses combatFleeYieldThreshold —
	// see that const's own doc comment above kick() for the full reasoning. Zero value
	// means "not currently yielding"; time.Now().Before(zero Time) is always false, so no
	// separate "is this set" flag is needed.
	combatFleeYieldUntil time.Time
	// corneredUntil is the end of a fight-back window (cornered.go): no escape, the threat
	// attacking, so the account fights and its script may eat while the guardian stands aside.
	corneredUntil time.Time
	// driftWantedUntil is set by EmergencyFlee's drift recovery when it could not take the
	// action lock; until then box/lamp handling steps aside for it (see requestDriftYield).
	driftWantedUntil time.Time

	// stickyPlantIndex is the NPC index of a Strange Plant (or similar dialog NPC) this
	// account has already engaged and hasn't yet confirmed is NOT its own — fed to
	// DetectPreferring so kick() keeps working on the SAME plant instead of Detect's own
	// first-match scan bouncing to a different nearby one each poll. -1 = none. See
	// DetectPreferring's own doc comment for the live death pattern this closes.
	stickyPlantIndex int
	// Keep every confirmed non-owner NPC, not just the last one. Type changes,
	// disappearance and expiry invalidate these observations.
	rejectedUntil map[npcIdentity]time.Time
	// A growing plant or unanswered click yields briefly so other events can run.
	retryUntil    map[string]time.Time
	trackedEvents map[string]Event

	// The previous poll's position reading, for watchDisplacement (displacement.go). Kept
	// here rather than in that file's own struct so it shares this mutex with everything
	// else Guardian samples, and lastTileOK distinguishes "not sampled yet" from 0,0,0.
	lastTileX, lastTileZ, lastTileLevel int
	lastTileOK                          bool
	lastTileAt                          time.Time

	// trapped remembers a mime/maze this Guardian handled until the account has left the trap
	// square and any reward dialogue it left open has been pressed — see trapreward.go for the
	// live twenty-minute silence that exists to end.
	trapped trapExit

	// strand is the escape ladder's memory of this account's visit to a strand site (the King Black
	// Dragon's lair, Rogues' Castle: strandsites.go). Guarded by mu; Busy reads it from script
	// goroutines.
	strand StrandLadder
	// void is the void walk's memory (strand_void.go); touched only under actionMu.
	void voidWalk
	// strandStay is when the account entered its current strand site (StrandSite.MinStay).
	strandStay strandStay
	// pvp is the PvP escape's memory (strand_pvp.go); touched only under actionMu.
	pvp pvpState
	// strandHold is the site the PvP escape or void walk is holding the account in, and since when;
	// Busy is true while it is set. Guarded by mu.
	strandHold strandStay
	// handedBack is the site (StrandSite.HandBack) whose void or PvP walk made no net progress and was handed to the
	// escape ladder for the rest of the visit; "" for none. Cleared when the account leaves the site. Guarded by mu.
	handedBack string
	// HopWorld and World, when set (internal/fleet), let the PvP escape move a stranded account to
	// pvpWorld, so every stranded account fights at one rally on one world.
	HopWorld func(world int, reason string) error
	World    func() int
	// pvpHopAt is when the PvP escape last asked for a hop to pvpWorld. Guarded by actionMu.
	pvpHopAt time.Time

	// noWayHome is when the drift check last found no walk home from the account's tile (a
	// StrandSite.NoWayHomeOnly site counts only after it). Guarded by mu.
	noWayHome time.Time

	// eventSeq and eventEnd are the Guardian's record of when it handled a random event, for any
	// window that measures the pack across an interval (eventmark.go says why): eventSeq is odd
	// while a handler runs and rises by one at each start and end, eventEnd is when the last one
	// ended (unix nanoseconds, 0 for none). Atomics: scripts read them from their own goroutines.
	eventSeq atomic.Uint64
	eventEnd atomic.Int64

	// Log receives one line per detected/handled event (nil = silent).
	Log func(string)
	// Chat, when set, is this account's own captured MESSAGE_GAME history — lets
	// HandleDialogNpc distinguish a real "not ready yet" rejection from genuine success
	// for events whose only server-side response is a chat line, not a modal (see that
	// function's own doc comment on the Strange Plant case this was added for). nil is
	// safe — every caller that doesn't set it keeps today's behavior exactly.
	Chat *chatlog.Log

	// SafeRef is where THIS account's flee logic should head for when it has no specific
	// direction to run in — its own script's operating home. nil falls back to
	// CatherbyBankRef, so every caller that never sets it keeps today's exact behavior.
	//
	// Live-found 2026-09-18 (4/4 reproducible, reported from the Scarab Swarm Stage 1
	// work): both the cheat-teleport destination in fleeFromCombat and the flee-candidate
	// bias in localEscapeAvoiding/HandleHazard hardcoded CatherbyBankRef, so an account
	// operating anywhere else was pulled toward Catherby whenever it tripped "in combat
	// with no tracked event". A fresh Lumbridge account pickpocketing at 3222,3218 was
	// teleported ~400 tiles to the Catherby bank (2809,3441) — exactly the reported
	// landing neighbourhood of 2820-2828,3437-3450 — and then could not navigate back,
	// with WalkNear's whole escalation ladder (door search, tryEscapeKnownPocket,
	// tryEscapeSealedPocket, attemptSpellRescue) failing and STRANDED firing. Catherby is
	// only "always safe" for the ProgressiveFisher accounts this package was first written
	// for; it is actively harmful for every script operating elsewhere.
	SafeRef *pathfind.NavPoint

	// CombatExpected disables kick()'s own "InCombat with no tracked event — flee"
	// branch (see that branch's own doc comment): every script ported before
	// AdvancedFighter/MeleeFlincher is a skilling script for which InCombat only ever
	// means "something unexpected/hostile just hit me," so fleeing on sight is correct.
	// A combat script's InCombat is its ordinary, desired operating state — without this
	// flag, live-found running cmd/advancedfightertest against a local engine (2026-09-16):
	// fleet.go's own automatic per-account Guardian.Start() fled the account away from
	// its own intended target within a couple of poll ticks every single fight, making
	// real combat scripts unusable end to end. Default false keeps every existing
	// script's behavior exactly as it was; a fighter script's own OnStart sets this true
	// on the *Context.Guardian it's handed (see internal/bot/advancedfighter.go/
	// meleeflincher.go). A genuine random-event hostile is still caught the ordinary way
	// (Detect()-based HandleEvade/HandleHazard/etc. below) — this only turns off the
	// blind "any InCombat at all" fallback that has no way to tell a wanted fight from
	// an unwanted one.
	CombatExpected bool
}

func NewGuardian(session actions.Session, pack *pathfind.Pack) *Guardian {
	g := &Guardian{
		session:            session,
		pack:               pack,
		stop:               make(chan struct{}),
		done:               make(chan struct{}),
		attempts:           make(map[string]int),
		firstAttempt:       make(map[string]time.Time),
		cooldownUntil:      make(map[string]time.Time),
		consecutiveGiveups: make(map[string]int),
		stickyPlantIndex:   -1,
		rejectedUntil:      make(map[npcIdentity]time.Time),
		retryUntil:         make(map[string]time.Time),
		trackedEvents:      make(map[string]Event),
	}
	registryMu.Lock()
	registry[session] = g
	registryMu.Unlock()
	return g
}

// registry maps a session to the one Guardian instance that owns it (fleet.go creates
// exactly one Guardian per account), so package-level Pending (below) can reach a
// specific account's own cooldown state without every caller needing to thread a
// *Guardian through its own Deps-style struct — see Pending's own doc comment for the
// real live bug this closes.
var (
	registryMu sync.Mutex
	registry   = make(map[actions.Session]*Guardian)
)

// Busy is the package-level, cooldown-aware replacement for what every script's own
// "WaitForGuardian" helper (fisherrunner, barbarianrunner, internal/bot/mule.go) has
// always done — call the raw package-level Detect(session.State()) != nil directly — and
// a real gap found live 2026-09-15 on the production fleet (see COLLAB.md): once
// Guardian itself gives up on a specific event and enters its own giveUpCooldown,
// Detect() still finds that same event present (nothing about the world has changed —
// the item is still in inventory, the NPC is still nearby), so a script's own wait loop
// had no way to know Guardian isn't even trying anymore and could block on
// WaitForGuardian indefinitely, repeating "a random event is present; waiting" forever
// even though nothing is working on it during the cooldown window.
//
// Deliberately NOT the same thing as Guardian.Pending() below, despite the similar
// cooldown-aware shape — Pending exists for a script to check *before starting a new
// action of its own* and so correctly returns false while a handle is already in
// flight (no need to also wait if Guardian's already on it). Busy is for a script that
// wants to keep waiting for as long as anything is actively happening OR about to
// happen, so it returns true during an in-flight handle, not false — using Pending here
// by mistake would let the calling script's own actions resume concurrently with
// Guardian's still-in-progress one, the exact race this whole fix exists to close.
//
// If session has a registered Guardian, defers to it; for a session with no registered
// Guardian (some cmd/* diagnostic tools that never created one), falls back to the old
// plain Detect()!=nil behavior unchanged.
func Busy(session actions.Session) bool {
	registryMu.Lock()
	g := registry[session]
	registryMu.Unlock()
	if g != nil {
		return g.Busy()
	}
	return Detect(session.State()) != nil
}

// chatFor returns session's registered Guardian's Chat log (nil if no Guardian is
// registered, or none set its Chat field) — lets HandleDialogNpc read an account's
// MESSAGE_GAME history without changing Handle/HandleWithLog's own public signature,
// the same registry lookup Busy/Pending already use for the identical reason.
func chatFor(session actions.Session) *chatlog.Log {
	registryMu.Lock()
	g := registry[session]
	registryMu.Unlock()
	if g == nil {
		return nil
	}
	return g.Chat
}

// safeRefFor returns session's own registered safe reference point, falling back to
// CatherbyBankRef when no Guardian is registered or none was set — the same registry
// lookup chatFor/markPlantRejected already use, for the same reason (it keeps every
// flee helper's public signature unchanged). See Guardian.SafeRef's own doc comment for
// the live cross-map stranding this exists to stop.
func safeRefFor(session actions.Session) pathfind.NavPoint {
	registryMu.Lock()
	g := registry[session]
	registryMu.Unlock()
	if g == nil || g.SafeRef == nil {
		return CatherbyBankRef
	}
	return *g.SafeRef
}

// markPlantRejected records that npcIndex is confirmed NOT session's own Strange Plant
// (or similar dialog NPC) — called from solveStrangePlant's own "not here for you" branch.
// Same registry lookup as chatFor, for the same reason (keeps Handle's own signature
// unchanged). A no-op if no Guardian is registered for session.
func markPlantRejected(session actions.Session, npcIndex int) {
	registryMu.Lock()
	g := registry[session]
	registryMu.Unlock()
	if g == nil {
		return
	}
	g.mu.Lock()
	for _, npc := range session.State().NPCs() {
		if npc.Index == npcIndex {
			g.rejectedUntil[npcIdentity{npc.Index, npc.Type}] = time.Now().Add(2 * time.Minute)
			break
		}
	}
	if g.stickyPlantIndex == npcIndex {
		g.stickyPlantIndex = -1
	}
	g.mu.Unlock()
	g.plants.forget(npcIndex) // not this account's: nothing to hold its script for
}

// Busy is Guardian's own instance method backing the package-level Busy above — see its
// doc comment for the semantic distinction from Pending.
func (g *Guardian) Busy() bool {
	if g.inFlight.Load() {
		return true
	}
	// A trapped event's exit still to finish is the Guardian's work too: until its reward
	// dialogue is pressed the account's backpack and worn tabs are null (trapreward.go).
	if _, due := g.trapExitDue(); due {
		return true
	}
	// Standing on a roof or in a lair the server dropped the account into: the escape ladder has
	// something to try (strand_exec.go), and every script's wait on this answer is bounded by the
	// ladder's own rounds, so it cannot hold an account for good.
	if g.strandActive() {
		return true
	}
	// Standing in the mime's or the maze's own mapsquare is being held by that event, whatever
	// this poll's filters say: the 2 x pollInterval retry window after a "still present" used to
	// let WaitForGuardian return with the account still in the cage (eight times for Acct188,
	// and in the lab the bot planned a 14-tile walk out of it). No script works in those squares.
	if g.session != nil {
		if x, z, _, ok := g.session.State().Tile(); ok && InTrapEventSquare(x, z) {
			return true
		}
	}
	// Between two tries at the same event: after a "still present" the handler gives the
	// account lock back for 2 x pollInterval, and a script that acts in that gap undoes the try.
	// Live 4 Oct 2026 23:54, Ridiculous's Strange plant at the Seers spinning wheel: each "no
	// plant reply yet" was followed within a second by the spinner's next flax on the wheel
	// ("the pack also lost 1 x Flax while it was handled"), the pick never landed, and the plant
	// killed the account. A solver's own longer wait (a growing plant) still lets the script work.
	if g.retryImminent() {
		return true
	}
	// A Strange plant this account has engaged is held to from nine ticks before it can be picked until it is gone, by
	// its index and whatever its type is: the type change to the angry plant is not a gap (inputhold.go).
	if g.plantBusy() {
		return true
	}
	return g.nextEvent() != nil
}

// busyRetryWindow is how near an event's next try must be for Busy to hold scripts: the
// 2 x pollInterval gap after a "still present", with a poll of slack.
const busyRetryWindow = 3 * pollInterval

// retryImminent reports whether a tracked event is due another try within busyRetryWindow.
func (g *Guardian) retryImminent() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	for sig, until := range g.retryUntil {
		if _, tracked := g.trackedEvents[sig]; !tracked {
			continue
		}
		if left := until.Sub(now); left > 0 && left <= busyRetryWindow {
			return true
		}
	}
	return false
}

// isRejected checks every confirmed non-owner NPC within its exclusion window.
// Selection also invalidates observations when the NPC disappears or changes type.
func (g *Guardian) isRejected(ev *Event) bool {
	if ev == nil || ev.Kind != KindDialogNpc {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return time.Now().Before(g.rejectedUntil[npcIdentity{ev.NpcIndex, ev.NpcType}])
}

// Shared by dispatch and script gates. Filter *before* selecting so a rejected,
// deferred or cooled-down candidate never masks another event.
func (g *Guardian) nextEvent() *Event {
	return g.nextEventExcept("")
}

func (g *Guardian) nextEventExcept(excluded string) *Event {
	state := g.session.State()
	present := make(map[npcIdentity]bool)
	for _, npc := range state.NPCs() {
		present[npcIdentity{npc.Index, npc.Type}] = true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	for identity, until := range g.rejectedUntil {
		if !present[identity] || !now.Before(until) {
			delete(g.rejectedUntil, identity)
		}
	}
	for sig, ev := range g.trackedEvents {
		if ev.NpcIndex >= 0 && !present[npcIdentity{ev.NpcIndex, ev.NpcType}] {
			delete(g.trackedEvents, sig)
			delete(g.attempts, sig)
			delete(g.firstAttempt, sig)
			delete(g.consecutiveGiveups, sig)
			delete(g.cooldownUntil, sig)
			delete(g.retryUntil, sig)
		}
	}
	for sig, until := range g.retryUntil {
		if !now.Before(until) {
			delete(g.retryUntil, sig)
		}
	}
	// Stranded in a catalogued site, an account leaves its own Genie or Mysterious Old Man alone: the
	// event's fail-teleport (macro_event_fail_teleport) is a way out the ladder cannot otherwise get
	// (owner, 3 Oct: "ignore a mysterious old man/genie random event when we are stuck").
	// Since 3 Oct 12:40 a stranded account handles only a Strange box and a trapped event (the maze,
	// the mime) and leaves everything else alone (owner: "turn off all random event handling except
	// for strange box and maze, that way we will get killed by them or teleport out").
	stranded := false
	if x, z, level, ok := state.Tile(); ok {
		_, stranded = g.siteAtLocked(x, z, level, now)
	}
	return detectEligible(state, g.stickyPlantIndex, func(ev *Event) bool {
		if stranded && !strandHandles(ev) {
			return false
		}
		if ev.Kind == KindDialogNpc && now.Before(g.rejectedUntil[npcIdentity{ev.NpcIndex, ev.NpcType}]) &&
			!greetingNamesSelf(state, ev) {
			return false
		}
		sig := eventSignature(ev)
		return sig != excluded && !now.Before(g.cooldownUntil[sig]) && !now.Before(g.retryUntil[sig])
	})
}

func deferEvent(session actions.Session, ev *Event, delay time.Duration) {
	registryMu.Lock()
	g := registry[session]
	registryMu.Unlock()
	if g == nil {
		return
	}
	g.mu.Lock()
	sig := eventSignature(ev)
	g.retryUntil[sig] = time.Now().Add(delay)
	g.trackedEvents[sig] = *ev
	g.mu.Unlock()
}

// Lock is the package-level counterpart to Guardian.Lock below, for a script about to
// start a modal-touching action sequence (opening a bank, driving boat-boarding
// dialogue) that must not run concurrently with Guardian's own handling of a random
// event — both touch the same account-wide modal state, and there is no way for either
// side to safely proceed while the other might also be mid-sequence.
//
// Real live bug this closes, found and root-caused 2026-09-15 (see COLLAB.md's "Strange
// box" entry): fisherrunner.BankFish/crafterrunner.BankAll originally called the
// cooldown-aware Busy() (above) exactly ONCE, at the very start of the bank sequence, as
// a "is it safe to proceed" gate. That is not enough — confirmed live via
// cmd/boxracetest, a deliberate reproduction built for this fix: a box forced to spawn
// microseconds before BankFish's own one-time check finds nothing yet (Detect() hasn't
// seen it spawn server-side), the check passes, BankFish opens the bank, and only ~300ms
// later (half of Guardian's own 600ms poll interval) does Guardian's poller catch the
// box and start handling it — DURING the bank sequence, not before it, so the one-shot
// gate never had a chance to catch this ordering. A single check at the door cannot
// protect a sequence that takes longer than one poll interval to complete; only a real
// mutex, held for the sequence's entire duration, closes the window in both directions.
//
// Returns an unlock function the caller must call (typically via defer) once its own
// sequence is done. For a session with no registered Guardian, returns a no-op —
// matching Busy/Pending's own "no guardian, nothing to coordinate with" fallback.
func Lock(session actions.Session) func() {
	registryMu.Lock()
	g := registry[session]
	registryMu.Unlock()
	if g != nil {
		return g.Lock()
	}
	return func() {}
}

// Lock acquires the account-wide action mutex, blocking until Guardian's own concurrent
// handling (if any) finishes, then marks the account busy (via the same inFlight flag
// Busy()/Pending() already read) so Guardian's own next poll tick skips this account
// until the returned function is called. See the package-level Lock's own doc comment
// for the real bug this exists to close.
func (g *Guardian) Lock() func() {
	g.actionMu.Lock()
	g.inFlight.Store(true)
	g.lockHolder.Store(actions.GoroutineID())
	return func() {
		g.lockHolder.Store(0)
		g.inFlight.Store(false)
		g.actionMu.Unlock()
	}
}

// TryLock is the package-level counterpart to Guardian.TryLock below, for a caller
// (EmergencyFlee) that must not stack a second, possibly conflicting walk command on top
// of whatever Guardian's own kick() might already be doing for this account (its own
// in-progress combat-flee holds this same lock for up to 36s — see kick()'s own InCombat
// branch). Returns ok=false if Guardian is already busy, in which case the caller should
// do nothing this poll rather than race it. For a session with no registered Guardian,
// always succeeds (no-op unlock), matching Lock's own fallback.
func TryLock(session actions.Session) (unlock func(), ok bool) {
	registryMu.Lock()
	g := registry[session]
	registryMu.Unlock()
	if g != nil {
		return g.TryLock()
	}
	return func() {}, true
}

// TryLock is Guardian's own instance method backing the package-level TryLock above.
func (g *Guardian) TryLock() (func(), bool) {
	if !g.actionMu.TryLock() {
		return nil, false
	}
	g.inFlight.Store(true)
	g.lockHolder.Store(actions.GoroutineID())
	return func() {
		g.lockHolder.Store(0)
		g.inFlight.Store(false)
		g.actionMu.Unlock()
	}, true
}

// cooledDown reports whether sig is currently being ignored after MAX_ATTEMPTS worth of
// failed handling — mirrors RandomEventsImpl.cooledDown.
func (g *Guardian) cooledDown(sig string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	until, ok := g.cooldownUntil[sig]
	return ok && time.Now().Before(until)
}

// Pending reports whether a handleable event is currently detected but not yet being
// acted on — mirrors RandomEvents.pending(), for a script's own loop to check before
// starting an action that a random event would otherwise interrupt mid-way.
func (g *Guardian) Pending() bool {
	if g.inFlight.Load() {
		return false
	}
	// A cooled-down (given-up-on) event should let the script resume, the same way
	// RandomEvents.pending() does in TS (it calls this.detect(), which applies the
	// identical cooldown filter) — otherwise a script using Pending() as its own
	// pre-action check would stay paused forever waiting for an event Guardian itself has
	// already stopped trying to clear. Scripts waiting for guardian to finish altogether
	// (barbarianrunner/fisherrunner/muleBot.waitForGuardian) now use the package-level
	// Busy() above instead of raw Detect() for the identical reason, with the opposite
	// in-flight polarity — see Busy's own doc comment.
	return g.nextEvent() != nil
}

// Start runs the poll loop until Stop is called. Call it once per session, typically
// right after login, in its own goroutine.
func (g *Guardian) Start() {
	defer close(g.done)
	defer g.unregister()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-g.stop:
			return
		case <-ticker.C:
			// Ahead of kick, and outside its own inFlight guard, so a relocation is seen on
			// every tick whether or not a handler is running — see displacement.go for the
			// unexplained excursion this exists to make self-explaining.
			g.watchDisplacement()
			g.kick()
		}
	}
}

// Wait joins a guardian whose Start loop was launched by the session owner.
func (g *Guardian) Wait() { <-g.done }

// Stop is idempotent: fleet.Manager calls it from both StopBot and Disconnect, and an
// operator stopping a bot then disconnecting the account panicked with "close of closed
// channel" (seen live, unified.log 2026-09-14 18:37).
func (g *Guardian) Stop() {
	g.stopOnce.Do(func() { close(g.stop) })
	// Keep an active handler registered until it observes cancellation and exits.
	if !g.inFlight.Load() {
		g.unregister()
	}
}

func (g *Guardian) unregister() {
	registryMu.Lock()
	if registry[g.session] == g {
		delete(registry, g.session)
	}
	registryMu.Unlock()
	if h := g.hold.Load(); h != nil {
		h.Release() // a stopped Guardian holds nothing, and the table must not keep its session
	}
}

func (g *Guardian) log(msg string) {
	if g.Log != nil {
		g.Log(msg)
	}
}

// combatFleeYieldThreshold/combatFleeYieldWindow give kick()'s own in-combat local-flee
// branch a bounded "step back" once local escape has demonstrably failed to create real
// separation several times in a row against the same ongoing threat.
//
// Live-found 2026-09-18: account crafter1 (ProgressiveFisher) spammed "no reachable
// flee candidate found" / "drifted far from home ... but the action lock is held
// elsewhere" in run/fleet.log for several minutes (~18:02-18:05 server time, positions
// drifting 3214,3221 -> 3159,3290 — nowhere near Catherby, more consistent with a
// Lumbridge-area coordinate than this account's own operating area), unproductive but not
// killed. Root cause: fleeFromCombat/fleeFromCombatWalking's own local escape search
// (localEscapeAvoiding, handle.go) deliberately caps its own routing to within 96 tiles of
// the account's CURRENT tile ("Escape routes are local" — see that function's own doc
// comment) and can be structurally unable to find any reachable candidate from a bad local
// position, even though a completely different recovery already exists and is already
// designed to fire for exactly this situation: emergencyFlee's own drift-recovery branch
// (handle.go) walks/teleports the account all the way back to its OWN script's home
// reference (not a local search) whenever it's both far from home AND in combat. That
// branch was being starved out entirely: kick() holds actionMu for up to fleeWalkDeadline
// (20s) per local-flee attempt and, when the hostile is still in range afterward, returns
// and is immediately re-entered on the very next 600ms poll tick — leaving only a few
// milliseconds of real contention window per cycle for any other TryLock caller.
// EmergencyFlee is polled every 500ms by every script's own WaitForGuardian loop (see e.g.
// fisherrunner.WaitForGuardian), so in principle it gets many tries, but each one loses the
// TryLock race almost every time.
//
// Once combatFleeStreak shows the SAME threat has survived combatFleeYieldThreshold
// consecutive full local-flee attempts, kick() deliberately does not contest actionMu for
// combatFleeYieldWindow, handing EmergencyFlee's drift-recovery branch a real, largely
// uncontested window to try a completely different route home. This does not stop
// Guardian from fleeing, and does not touch hostile safety in any way that matters here:
// local escape still runs and still fires immediately on every attempt outside a yield
// window (the account is never left literally undefended — EmergencyFlee's own InCombat
// branch, called from the exact same loop, keeps fleeing locally too whenever it wins the
// lock during the yield window), and the yield itself is short and bounded, not a give-up
// — if drift-recovery doesn't resolve it either, kick() simply resumes local-flee
// escalation (streak keeps climbing, so does fleeDistFor's own dist) once the window
// elapses. Deliberately NOT wired through hostileCapableNames/hostileKinds' own
// give-up/cooldown machinery below (maxAttempts/giveUpCooldown) — this in-combat branch has
// no tracked Event at all and is a completely separate code path from that machinery,
// which governs a different decision (whether to keep retrying a specific event's own
// handler). Widening or narrowing this yield has no effect on that exemption logic, and
// those genuinely-dangerous hostiles are still fled from immediately, every single time,
// exactly as before.
const (
	combatFleeYieldThreshold = 3
	combatFleeYieldWindow    = 3 * time.Second
)

// nextCombatFleeYieldUntil returns the new combatFleeYieldUntil deadline kick() should set
// after a local-flee attempt against an ongoing threat has now failed streak times in a
// row, or the zero Time if this streak doesn't yet warrant yielding — see
// combatFleeYieldThreshold's own doc comment. Pure and unit-testable, the same
// pure-decision/impure-caller split this file already uses for fleeDistFor/
// escalatedCooldown.
func nextCombatFleeYieldUntil(streak int, now time.Time) time.Time {
	if streak < combatFleeYieldThreshold {
		return time.Time{}
	}
	return now.Add(combatFleeYieldWindow)
}

// guardianHandler is what kick runs for a detected event: HandleWithLog. A variable only so a test
// can put a stub in its place and prove what kick does AROUND a handler (the event span, below)
// without a live session; nothing else assigns it.
var guardianHandler = HandleWithLog

func (g *Guardian) kick() {
	if g.inFlight.Load() {
		return
	}
	// Before every other branch, in-combat flee included: on a roof with a Swarm beside the account
	// the staircase two tiles away is the escape, and in the dragon's lair running about it is not
	// (strandsites.go). It only ever acts inside a catalogued strand site.
	if !g.strandBoxDue() && g.recoverStrand() {
		return
	}
	// Checked before Detect() and independent of it entirely: InCombat (a hitmark on the
	// local player within the last few ticks) fires regardless of whether anything
	// tracked as a "random event" is involved at all. Live-found 2026-09-15 (a 200-account
	// Catherby soak, several deaths): a hostile-transformed "Strange plant"/"Drunken Dwarf"
	// (see hostileCapableNames) that Guardian never got to handle in time — or already gave
	// up on and is now under its own escalatedCooldown, or was never detected as an event
	// at all because whatever spawned it happened on another account's own detection
	// window — becomes a completely invisible threat once Detect() no longer reports it as
	// an active event: nothing in the ordinary fishing/pickpocketing loop, nor this
	// Guardian's own event-name-keyed logic, ever checks "am I currently under attack right
	// now" as its own independent signal. Confirmed live: a scan at an exact death
	// coordinate moments after two accounts died there found no hostile NPC present at all
	// — whatever killed them had already resolved and despawned, exactly the class of gap
	// this closes. Reuses HandleHazard's own flee-a-short-distance-and-wait shape (its own
	// ev parameter is unused, safe to pass nil) rather than inventing a new response.
	//
	// Two changes 2026-09-20, after three live deaths on c36f809 (see fleeFromCombatWalking):
	//   - "Under attack" also covers a recorded attacker still pursuing the account between its
	//     hits (underAttack/pursuingAttacker), so it outranks every event this poll could
	//     otherwise dispatch, a Strange box above all (herring, 23:55-23:57, reopened boxes
	//     beside its attacker five times, then died).
	//   - When the account's own event monster is what is attacking it, this branch leaves it
	//     to the ordinary KindEvade dispatch below (HandleEvade, which tracks that NPC and logs
	//     it) instead of a blind flee: at 22:12:09 spare102 logged "in combat with no
	//     tracked event" and one second later "a random event is present", so Detect had its
	//     attacker all along; this branch simply ran first and never asked.
	if !g.CombatExpected && underAttack(g.session) && !g.ownEvadeEventPresent() && !g.cornered() {
		g.mu.Lock()
		yieldUntil := g.combatFleeYieldUntil
		g.mu.Unlock()
		if time.Now().Before(yieldUntil) {
			// Deliberately not contesting actionMu this tick — see
			// combatFleeYieldThreshold's own doc comment above. EmergencyFlee's own
			// drift-recovery branch (handle.go), polled every 500ms by the script's own
			// WaitForGuardian loop, gets a real chance at TryLock during this window
			// instead of losing the race to this branch re-entering on the very next
			// 600ms tick.
			return
		}
		if !g.actionMu.TryLock() {
			return
		}
		g.inFlight.Store(true)
		defer func() { g.inFlight.Store(false); g.actionMu.Unlock() }()
		g.mu.Lock()
		g.combatFleeStreak++
		streak := g.combatFleeStreak
		g.mu.Unlock()
		// Escalating distance, the same fleeDistFor curve HandleEvade already uses for a
		// chasing hostile-event NPC — not HandleHazard's flat 4 tiles. Live-found
		// 2026-09-15 (this same soak): several accounts kept dying and re-fleeing over and
		// over in a tight loop (lc2b_79/83/161 each 2+ times) — their fishing spot itself
		// sits close enough to wherever these hostiles keep appearing that a fixed 4-tile
		// hop and a 36s wait lands them right back in range the moment they resume fishing.
		dist := fleeDistFor(streak)
		g.log(fmt.Sprintf("in combat with no tracked event — fleeing immediately (streak %d, dist %d, %s)", streak, dist, attackerDiag(g.session.State())))
		// fleeFromCombat, not a duplicated inline walkTo — live-found 2026-09-16 (lc2b_170,
		// same 200-account soak, AFTER fleeFromCombat/HandleEvade were already fixed to
		// teleport straight to CatherbyBankRef): this branch had its own separate, never-
		// updated copy of the old biasedFleeCandidates/walkableFlee/walkTo sequence, so an
		// account caught here still got the slow, multi-tick WALKED flee the rest of the
		// package had already moved away from — traced directly to a real death shortly
		// after this exact log line fired. Reusing the shared, already-fixed helper closes
		// the gap instead of maintaining a second copy that can silently drift out of sync.
		// The account's own script is held off while the flee walks (inputhold.go); the 36 s watch below is the
		// Busy() wait's, not a handler's, and holds nothing.
		func() {
			defer g.takeInput()()
			fleeFromCombat(g.session, g.pack, dist, g.log)
		}()
		// If a known hostile-event-NPC type is still standing within engage range right
		// now, skip the blind 36s watch below entirely rather than trusting InCombat()
		// alone to decide whether to bother -- live-found 2026-09-16 (lc2b_40 on the main
		// soak, corroborated locally on wfixtest2): fleeFromCombat/fleeFromCombatWalking's
		// own internal escalation (see HostileEventNpcNearby's doc comment) closes the
		// common case, but its own 20s deadline can still elapse with a genuinely
		// unshakeable chaser still adjacent. InCombat()'s ~6s hit-recency window can be
		// reading false at this exact instant even though the account is not remotely
		// safe, so don't gate this check on it. Returning immediately (no wait) lets the
		// very next 600ms poll re-evaluate fresh: since Detect() itself already classifies
		// exactly this NPC type+range as a proper "event monster" KindEvade event (see
		// detect.go's own hostileEventNpcIDs/hostileEngageDist), that next tick hands off
		// to HandleEvade -- which tracks this specific NPC's actual disappearance for up
		// to 45s, a strictly stronger guarantee than this generic branch's own InCombat()
		// polling -- instead of wasting up to 36s here first.
		if x, z, _, ok := g.session.State().Tile(); ok && (HostileEventNpcNearby(g.session.State(), x, z, hostileEngageDist) || g.pursued()) {
			if until := nextCombatFleeYieldUntil(streak, time.Now()); !until.IsZero() {
				g.mu.Lock()
				g.combatFleeYieldUntil = until
				g.mu.Unlock()
				g.log(fmt.Sprintf("in combat with no tracked event — hostile still nearby after %d consecutive local-flee attempts; yielding the action lock for %s so drift-recovery can try a different route", streak, combatFleeYieldWindow))
			} else {
				g.log("in combat with no tracked event — hostile still nearby after fleeing; retrying next poll")
			}
			return
		}
		// sleepWatchingCombat, not a bare time.Sleep — found during a repo-wide audit
		// following tonight's incident response: this cooldown holds inFlight/actionMu
		// (via the defer above) for the entire 36s, which makes kick()'s own next tick a
		// guaranteed no-op the whole time (its very first line is
		// `if g.inFlight.Load() { return }`) — the exact same "blind wait, account can
		// already be dead by the time it ends" shape already fixed today for
		// HandleHazard's whirlpool wait and every multi-second retry loop in handle.go.
		// A hostile that catches back up (or was never actually shaken off) during this
		// cooldown would otherwise go completely unnoticed by anything for the full 36s,
		// since this IS the one thing meant to notice it. Returning the moment combat
		// resumes releases inFlight immediately, so the very next scheduled tick
		// (pollInterval later) re-enters this same branch fresh and flees again with an
		// escalated streak, instead of waiting out the rest of an already-failed cooldown.
		sleepWatchingCombat(g.session, 36*time.Second)
		return
	}
	g.mu.Lock()
	g.combatFleeStreak = 0
	g.combatFleeYieldUntil = time.Time{}
	g.mu.Unlock()
	// A mime's return teleport usually lands between two handling attempts, so its exit is
	// finished here rather than only in the "cleared" branch below (trapreward.go).
	if name, due := g.trapExitDue(); due {
		g.runTrapExit(name)
		return
	}
	ev := g.nextEvent()
	if ev == nil {
		g.mu.Lock()
		g.stickyPlantIndex = -1
		g.mu.Unlock()
		return
	}
	if ev.Kind == KindDialogNpc {
		// Widened from "Strange Plant" only, 2026-09-15/16: HandleDialogNpc's own generic
		// rejection check (chatFor + "trying to talk to") now covers Genie/Mysterious Old
		// Man/Sandwich Lady/Frog too, via markPlantRejected, so stickiness is safe to widen
		// to any dialog NPC — a confirmed-not-mine one always gets unstuck the same way
		// the plant does, rather than only the plant ever benefiting from not getting its
		// attention stolen by a competing nearby dialog NPC each poll.
		g.mu.Lock()
		g.stickyPlantIndex = ev.NpcIndex
		g.mu.Unlock()
	} else {
		g.mu.Lock()
		g.stickyPlantIndex = -1
		g.mu.Unlock()
	}
	if ev.Kind == KindBoxLamp && g.driftRecoveryWanted() {
		// A box or lamp keeps; a drifted account waiting on the lock does not. See
		// requestDriftYield.
		return
	}
	sig := eventSignature(ev)
	// Non-blocking: if a script-side Lock() (or another kick(), though Start()'s own
	// ticker never overlaps itself) currently holds actionMu, skip this poll tick rather
	// than waiting — the next tick 600ms later tries again. Blocking here would stall
	// Guardian's whole poll loop for as long as the script's own action sequence takes,
	// which is the wrong tradeoff for an event Detect() just found a moment ago and can
	// simply re-detect next tick. See the package-level Lock's own doc comment for why
	// this mutex exists at all, not just the status-flag inFlight check above.
	if !g.actionMu.TryLock() {
		return
	}
	g.inFlight.Store(true)
	defer func() { g.inFlight.Store(false); g.actionMu.Unlock() }()
	g.mu.Lock()
	g.trackedEvents[sig] = *ev
	g.mu.Unlock()

	n := g.noteAttempt(sig)
	if n > maxAttempts && !trappedKinds[ev.Kind] && !hostileCapableNames[ev.Name] && !hostileKinds[ev.Kind] && !g.keepTryingOwnEvent(ev, sig, n) {
		g.mu.Lock()
		delete(g.attempts, sig)
		delete(g.firstAttempt, sig)
		g.consecutiveGiveups[sig]++
		cooldown := escalatedCooldown(g.consecutiveGiveups[sig])
		g.cooldownUntil[sig] = time.Now().Add(cooldown)
		g.mu.Unlock()
		g.log(fmt.Sprintf("random event: gave up on %s after %d attempts — ignoring it for %s", ev.Name, maxAttempts, cooldown))
		return
	}

	// The handler's whole run, to its last check, is one stretch of event activity: any window
	// measuring the pack across it is not measuring what it thinks (eventmark.go). Deferred, so it
	// ends before the action lock is released below and a script resuming sees it over.
	endEvent := g.BeginEvent()
	defer endEvent()
	// The account's own script is held off for the handler's run, and past it when the event is one that can hurt
	// (holdPastHandler): see inputhold.go. Deferred after endEvent, so it ends first.
	defer g.takeInput()()
	g.log("random event: " + ev.Name + " — handling")
	var progress func(string)
	if g.Log != nil {
		progress = func(line string) { g.log("random event: " + ev.Name + " — " + line) }
	}
	if trappedKinds[ev.Kind] {
		// Noted before the handler runs, so an exit the handler itself sees still counts.
		g.trapped.note(ev.Name, time.Now())
	}
	if err := guardianHandler(g.session, g.pack, ev, n, progress); err != nil {
		g.log("random event: " + ev.Name + " — handler error: " + err.Error())
		g.holdPastHandler(ev)
		return
	}
	if g.isRejected(ev) {
		g.mu.Lock()
		delete(g.attempts, sig)
		delete(g.firstAttempt, sig)
		delete(g.consecutiveGiveups, sig)
		g.mu.Unlock()
		g.log("random event: " + ev.Name + " — rejected for this account; checking other events next")
		return
	}
	// A different event winning selection does not prove this instance cleared.
	after := detectEligible(g.session.State(), -1, func(candidate *Event) bool {
		return eventSignature(candidate) == sig
	})
	if after == nil {
		g.mu.Lock()
		delete(g.attempts, sig)
		delete(g.firstAttempt, sig)
		delete(g.consecutiveGiveups, sig)
		g.mu.Unlock()
		g.log("random event: " + ev.Name + " — cleared")
		if name, due := g.trapExitDue(); due {
			g.finishTrapExitLocked(name) // still under this handling's own action lock
		}
	} else {
		// Failed or partial handlers release the account lock before another try.
		// Preserve a longer plant-growth delay set by its solver.
		g.mu.Lock()
		if !time.Now().Before(g.retryUntil[sig]) {
			g.retryUntil[sig] = time.Now().Add(2 * pollInterval)
		}
		g.mu.Unlock()
		g.holdPastHandler(ev)
		g.log("random event: " + ev.Name + " — still present after handling")
	}
}

// noteAttempt increments and returns sig's attempt count, mirroring RandomEventsImpl.
// handleInner's own `n = (this.attempts.get(sig) ?? 0) + 1`.
// It also records when a signature's run of attempts began (firstAttempt, cleared with attempts),
// which keepTryingOwnEvent measures against ownedFailTeleportWindow.
func (g *Guardian) noteAttempt(sig string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.attempts[sig]++
	if g.attempts[sig] == 1 {
		g.firstAttempt[sig] = time.Now()
	}
	return g.attempts[sig]
}

// failTeleportDialogNames are the dialog events whose content answers being ignored with a
// fail-teleport: macro_event_genie.rs2 and macro_event_mysterious_old_man.rs2 both run a
// 20-tick ai_timer (their .npc configs' timer=20) that nags three times naming the owner and
// on the fourth fire queues macro_event_fail_teleport — 80 ticks, 48 seconds after spawn.
var failTeleportDialogNames = map[string]bool{"Mysterious Old Man": true, "Genie": true}

// ownedFailTeleportWindow is how long attempts on this account's OWN Genie or Mysterious Old
// Man keep going past maxAttempts: the 48-second content timer above plus a little, measured
// from the first attempt, which is never earlier than the spawn. Giving up inside that window
// is exactly how the NPC is left to teleport the account; past it the NPC has either been
// answered or has already teleported, so the ordinary give-up applies again and a truly
// unreachable one still ends. ownedFailTeleportMaxAttempts caps the count as well, so a handler
// that fails instantly cannot spin the whole window at poll speed.
//
// Live on build e35420b6 (2026-09-24 18:16:09-21, the mule): four "no approach path" answers in
// four seconds on the mule's own Old Man, "gave up … ignoring it for 45s", and 38 seconds later
// the Old Man fail-teleported the fleet's only mule to the Lumbridge basement — 3h18m without
// a mule.
const (
	ownedFailTeleportWindow      = 55 * time.Second
	ownedFailTeleportMaxAttempts = 60
)

// keepTryingOwnEvent reports whether attempt n on ev, which is past maxAttempts, should still
// be made rather than given up: ev is a fail-teleporting dialog NPC whose own overhead lines
// name this account, and the run of attempts on it is still inside ownedFailTeleportWindow. A
// Genie or Old Man that names nobody or somebody else keeps the ordinary four-attempt give-up
// (the ownership-mismatch waste escalatedCooldown exists for).
func (g *Guardian) keepTryingOwnEvent(ev *Event, sig string, n int) bool {
	if ev.Kind != KindDialogNpc || !failTeleportDialogNames[ev.Name] || n > ownedFailTeleportMaxAttempts {
		return false
	}
	g.mu.Lock()
	first, ok := g.firstAttempt[sig]
	g.mu.Unlock()
	if !ok || time.Since(first) >= ownedFailTeleportWindow {
		return false
	}
	return greetingNamesSelf(g.session.State(), ev)
}

// trappedKinds mirrors RandomEventsImpl's own TRAPPED_KINDS set: events that physically
// confine the player (maze/mime — see detect.go's KindTrapped) are exempt from the give-up
// cooldown above, since ignoring one doesn't make it go away the way stepping back from a
// hazard or walking off from a dialogue NPC does.
var trappedKinds = map[Kind]bool{KindTrapped: true}

// hostileCapableNames are the dialog-NPC events whose own content script can turn them
// genuinely hostile — real melee damage, not a relocation — if ignored long enough:
// content/scripts/macro events/scripts/general/macro_event_drunken_dwarf.rs2 (its own
// ai_timer reaching %npc_int=4 sets npc_setmode(opplayer2) and starts attacking) and
// macro_event_triffid.rs2 (its second ai_timer phase, ^triffid_ready_to_attack, does the
// identical npc_changetype_keepall+opplayer2 switch). Confirmed as the real, live
// mechanism behind two deaths this session (2026-09-15, see COLLAB.md): 45s of "gave up,
// ignoring it" is a genuinely dangerous window for these two specifically, since nothing
// is even trying to resolve a ticking timer that ends in real damage during that pause —
// safe to give up on a Genie/Mysterious Old Man (worst case there is the classic
// ignored-NPC fail-teleport, not combat), but not safe here. Exempted from the give-up
// cooldown the same way trappedKinds already is, for the identical reason: retrying is
// the only option, giving up isn't one. Genie and Mysterious Old Man are deliberately NOT
// in this set — excluded from the cooldown too aggressively would bring back the exact
// "every 45s forever, uncapped" waste escalatedCooldown was built to fix for the
// ownership-mismatch case (see COLLAB.md's "It's not here for you" entry), for two NPCs
// that don't carry this specific danger.
var hostileCapableNames = map[string]bool{"Drunken Dwarf": true, "Strange plant": true}

// hostileKinds is hostileCapableNames' own kind-level twin: KindEvade ("event monster" —
// see detect.go's HOSTILE_EVENT_NPC_IDS set: River troll, Swarm, Rock Golem, Zombie,
// Shade, Watchman, Tree spirit) is a live combat NPC by construction, not a name that
// happens to sometimes be one — every event this Kind ever produces is already hostile,
// dealing real melee damage via the same combat_damage_player path the dwarf/triffid
// scripts reach, not a relocation. Found live 2026-09-15 while root-causing cyclev2_4's
// death: its guardian log showed "event monster — handling" / "— cleared" immediately
// before the stranding, not a Drunken Dwarf or Strange plant, meaning the
// hostileCapableNames fix above (name-keyed, HandleDialogNpc-only) never covered this
// death vector at all — HandleEvade's give-up path was still fully subject to the 45s+
// cooldown, leaving an unanswered hostile NPC free to keep attacking during the pause.
// Exempted from the give-up cooldown for the same reason trappedKinds and
// hostileCapableNames already are: retrying is the only safe option here.
var hostileKinds = map[Kind]bool{KindEvade: true}
