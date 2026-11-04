package actions

import (
	"fmt"
	"sync"
	"time"

	"rsb0t-go/internal/gamestate"
)

// THE BITE LEDGER: one eat click's life, from the send to a landing that may come long after the confirmation gave up.
//
// WHAT THE ENGINE DOES WITH AN EAT (Engine-TS 289 and content 289, read 15 Oct 2026):
//
//   - OpHeldHandler.handle starts with `if (player.delayed) return false`: a held-item click read while the player is
//     delayed (p_delay) is DROPPED, not queued, and nothing is said. It counts as a client event, not a user event.
//   - `delayed` is cleared in World.processPlayers, AFTER processClientsIn has read the tick's packets. p_delay(n) run in tick T
//     sets delayedUntil = T+1+n, so the clicks of ticks T+1 .. T+1+n are dropped: n+1 ticks, then free. A pickpocket
//     (thieving.rs2 pick_pocket, fail_pick_pocket) runs p_delay(0) once on a hit (one tick; the Thieving xp comes after it, so
//     once the xp is seen the player is free) and three times on a miss (three ticks: "You fail to pick" comes after the first,
//     "You've been stunned!" after the third); a stall steal p_arrivedelay (one tick, only if the player moved) and p_delay(0);
//     High Level Alchemy p_delay(3) (four ticks); Low Level Alchemy, Superheat, the enchants and a ring rub p_delay(1) (two); a
//     teleport p_delay(1) then p_delay(0) (three).
//   - THE STUN ITSELF DOES NOT BLOCK AN EAT. %stunned and %action_delay (8-10 ticks) are varps that only the next pickpocket and
//     walking read. An eat is refused by the engine only while the player is DELAYED, and that is one to four ticks.
//   - The bite itself is immediate (consume.rs2 player_consume_item), but a second eat inside %eat_delay (2 ticks; a cake 1,
//     the last bite of one 2) is refused in silence: no message, no heal.
//   - The engine reads at most five USER_EVENT packets a player a tick and leaves the rest in the socket buffer in order
//     (NetworkPlayer.decodeIn; docs/click_flood_audit_2026-10-15.md), so a click that waits behind others lands ticks late.
//
// WHAT THE LIVE LOG SHOWED (fleet.log, 15 Oct 2026, build abae5e46, the wait already six seconds): 104 "timed out waiting for the
// bite to register" lines from 56 ScarabSwarm accounts between 13:05 and 14:00 (189 from 93 accounts through 14:12). Wherever one
// account failed twice within 20 seconds (20 pairs) the hitpoints at the second bite stood higher than at the first, by exactly one
// food's heal (13, 17, 21, 25 of 53 for Bread then Cake; 19 -> 26 for Cod; 19 -> 25 for Mackerel): the bites LANDED, seven to nine
// seconds after the click, and each was called a failure, held the account off thieving for 30 seconds and walked it to the bank.
// A longer fixed wait only moves the cliff; the confirmation has to outlive its own timeout.
//
// WHAT THIS DOES. EatBite sends the click and waits BiteConfirmWait for the bite to show, as every eater did. A bite that has
// not shown by then is not forgotten: it stays in the ledger, "in flight", and
//
//   - the next EatBite on the session first asks whether it landed (hitpoints up by two or more, or the food left its slot or
//     changed form), and if it did credits it and returns true WITHOUT a second click, so a late landing is never eaten twice;
//   - with hitpoints above BiteUrgentPercent it waits for that bite until BiteWatchWindow after its click before it calls it
//     lost; below it (or once hitpoints fell since the click) it does not wait, as the eaters never did, and sends another;
//   - before any click it waits out a delay a cast or a rub is known to hold (NoteDelay), up to BiteDelayWaitMax, because the
//     engine would drop the click.
//
// BiteInFlight tells a caller what a false means: not shown YET (do not hold the account off for it) or not at all.

// Bite timing. The wait is unchanged from thieverrunner.EatConfirmWait, which this replaces.
const (
	// BiteConfirmWait bounds the first wait for a bite to show, hitpoints up or the food gone from its slot.
	BiteConfirmWait = 6 * time.Second
	// BiteSlowAfter is the wait the 11 Oct 2026 confirmation (2.4 s) would have called a failure: a bite that takes this long to
	// show is logged as slow, not failed.
	BiteSlowAfter = 2400 * time.Millisecond
	// BiteWatchWindow is how long after its click a bite can still be credited, and how long a later EatBite waits for it
	// while hitpoints allow. The latest landings in the 15 Oct log were 8 to 9 s after the click.
	BiteWatchWindow = 14 * time.Second
	// BiteUrgentPercent is the hitpoints, as a percent of the maximum, under which a bite that has not shown is not waited for:
	// another is sent at once. The eaters' old behaviour, kept for the case where a dropped click could cost a life.
	BiteUrgentPercent = 40
	// BiteLateRise is the hitpoints a late credit needs above the click's baseline. Regeneration is one point a minute, so a rise of
	// one is not evidence on its own; the food leaving its slot is, whatever the hitpoints did.
	BiteLateRise = 2
	// BiteDelayWaitMax is the longest EatBite holds a click for a delay it knows of.
	BiteDelayWaitMax = 3 * time.Second
	// BiteLagAfter is how long a bite may take to show before it is evidence that the route to the server is slow
	// (LastBiteLag). The 14 Oct 2026 deaths (20:09 and 20:13) all came inside fleet-wide windows of 3.2 to 3.8 second bites and
	// bites that took fourteen seconds; the hitpoints and the damage reach the client that late, and so does every decision the
	// account takes on them.
	BiteLagAfter = 3 * time.Second

	biteStep         = 150 * time.Millisecond
	biteLogEvery     = 30 * time.Second
	biteSessionsKept = 2048
)

// BiteEnv is what an eater lends EatBite: the session, and its clock, its way to wait and its log. Nil Now is time.Now, nil Delay
// sleeps, nil Logf is silent. A Delay that returns false (the script was told to stop) ends the bite at once.
type BiteEnv struct {
	Session Session
	Now     func() time.Time
	Delay   func(time.Duration) bool
	Logf    func(format string, args ...any)
}

func (e BiteEnv) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e BiteEnv) delay(d time.Duration) bool {
	if e.Delay != nil {
		return e.Delay(d)
	}
	time.Sleep(d)
	return true
}

func (e BiteEnv) logf(format string, args ...any) {
	if e.Logf != nil {
		e.Logf(format, args...)
	}
}

// BitePick is the food an eater chose, read at the moment the click goes out.
type BitePick struct {
	Obj  int32
	Slot int
	// Changed reports whether the pack shows the bite: the food gone from its slot, or changed form (a whole cake becoming a 2/3
	// cake). Its baseline is the pack when the pick was made; it is read again at any time up to BiteWatchWindow later.
	Changed func() bool
}

// BiteRequest is one bite to eat.
type BiteRequest struct {
	// Food names the bite in the log lines.
	Food string
	// Pick finds the food in the pack now and says so in the log when there is none (ok false). It is called after any wait for an
	// earlier bite or a delay, so it never names a slot that bite has just emptied.
	Pick func() (BitePick, bool)
	// Wait overrides BiteConfirmWait.
	Wait time.Duration
}

type biteRec struct {
	food     string
	sentAt   time.Time
	hpBefore int // -1 when hitpoints were unread at the click
	changed  func() bool
}

type biteBook struct {
	rec        *biteRec
	delayUntil time.Time
	said       map[string]time.Time
	lag        BiteLag
	hasLag     bool
}

// BiteLag is the latest evidence that this session's route is slow: a bite that took BiteLagAfter or longer to show, landed after
// its eater gave up, or never showed.
type BiteLag struct {
	// At is when the evidence was noted.
	At time.Time
	// Took is how long the bite took to show, or was waited for.
	Took time.Duration
	// Lost is true when the bite never showed within its window.
	Lost bool
}

// LastBiteLag is the session's latest bite lag, and false when it has had none. A bot compares At with the last it acted on, and
// keeps its account away from anything that hurts until its hitpoints are back (scarabswarm's lag hold): the hitpoints it reads
// are as old as the route is slow.
func LastBiteLag(st *gamestate.State) (BiteLag, bool) {
	if st == nil {
		return BiteLag{}, false
	}
	biteBooks.mu.Lock()
	defer biteBooks.mu.Unlock()
	if b, ok := biteBooks.m[st]; ok && b.hasLag {
		return b.lag, true
	}
	return BiteLag{}, false
}

// noteBiteLag records lag evidence of took (at least BiteLagAfter, or lost) at now.
func noteBiteLag(st *gamestate.State, now time.Time, took time.Duration, lost bool) {
	if st == nil || (!lost && took < BiteLagAfter) {
		return
	}
	biteBooks.mu.Lock()
	defer biteBooks.mu.Unlock()
	b := bookOf(st)
	b.lag, b.hasLag = BiteLag{At: now, Took: took, Lost: lost}, true
}

var biteBooks = struct {
	mu sync.Mutex
	m  map[*gamestate.State]*biteBook
}{m: map[*gamestate.State]*biteBook{}}

// bookOf is the session's book; the caller holds biteBooks.mu.
func bookOf(st *gamestate.State) *biteBook {
	if b, ok := biteBooks.m[st]; ok {
		return b
	}
	if len(biteBooks.m) >= biteSessionsKept {
		biteBooks.m = map[*gamestate.State]*biteBook{} // sessions that closed leave their book; losing one only forgets a bite
	}
	b := &biteBook{said: map[string]time.Time{}}
	biteBooks.m[st] = b
	return b
}

func pendingBite(st *gamestate.State) *biteRec {
	biteBooks.mu.Lock()
	defer biteBooks.mu.Unlock()
	if b, ok := biteBooks.m[st]; ok {
		return b.rec
	}
	return nil
}

func setPendingBite(st *gamestate.State, rec *biteRec) {
	biteBooks.mu.Lock()
	defer biteBooks.mu.Unlock()
	bookOf(st).rec = rec
}

// clearPendingBite empties the book if rec is still what it holds.
func clearPendingBite(st *gamestate.State, rec *biteRec) {
	biteBooks.mu.Lock()
	defer biteBooks.mu.Unlock()
	if b, ok := biteBooks.m[st]; ok && b.rec == rec {
		b.rec = nil
	}
}

// sayDue reports whether the throttled line key may be said now (once per biteLogEvery per session).
func sayDue(st *gamestate.State, key string, now time.Time) bool {
	biteBooks.mu.Lock()
	defer biteBooks.mu.Unlock()
	b := bookOf(st)
	if last, ok := b.said[key]; ok && now.Sub(last) < biteLogEvery {
		return false
	}
	b.said[key] = now
	return true
}

// BiteInFlight reports whether the session's last bite has not shown yet but may still: it was sent no more than BiteWatchWindow
// ago and nothing has credited or dropped it. A caller whose eater returned false asks this to tell "not shown yet" (the bite may
// land any second: no failure, no hold) from a bite that is lost.
func BiteInFlight(st *gamestate.State) (age time.Duration, ok bool) {
	return BiteInFlightAt(st, time.Now())
}

// BiteInFlightAt is BiteInFlight at an explicit time.
func BiteInFlightAt(st *gamestate.State, now time.Time) (age time.Duration, ok bool) {
	if st == nil {
		return 0, false
	}
	rec := pendingBite(st)
	if rec == nil {
		return 0, false
	}
	age = now.Sub(rec.sentAt)
	if age < 0 {
		age = 0
	}
	return age, age <= BiteWatchWindow
}

// NoteDelay records that the server holds the player delayed (a cast's or a rub's p_delay) until the given time: an eat clicked
// before then would be dropped, so EatBite waits for it. A later time replaces an earlier one, an earlier one is ignored.
func NoteDelay(s Session, until time.Time) {
	if s == nil {
		return
	}
	st := s.State()
	if st == nil {
		return
	}
	biteBooks.mu.Lock()
	defer biteBooks.mu.Unlock()
	if b := bookOf(st); until.After(b.delayUntil) {
		b.delayUntil = until
	}
}

// NoteDelayTicks is NoteDelay for a delay of the given number of game ticks from now.
func NoteDelayTicks(s Session, ticks int) {
	NoteDelay(s, time.Now().Add(time.Duration(ticks)*sleepTick))
}

// DelayLeft is how much longer the session is known to be held, from now; zero when it is not.
func DelayLeft(s Session, now time.Time) time.Duration {
	if s == nil {
		return 0
	}
	st := s.State()
	if st == nil {
		return 0
	}
	biteBooks.mu.Lock()
	defer biteBooks.mu.Unlock()
	if b, ok := biteBooks.m[st]; ok && b.delayUntil.After(now) {
		return b.delayUntil.Sub(now)
	}
	return 0
}

// castHeldTicks is how many ticks after a cast goes out the engine can still be dropping held-item clicks, plus one for the
// click's own trip: p_delay(n) refuses the clicks of the n+1 ticks after the one it ran in (alchemy.rs2, enchant.rs2, superheat.rs2).
func castHeldTicks(spellCom int) int {
	if spellCom == 1178 { // High Level Alchemy: p_delay(3)
		return 4 + 1
	}
	return 2 + 1 // Low Level Alchemy, Superheat, the enchants: p_delay(1)
}

// underFire is whether the account took a hitmark in the last ten ticks (State.InCombat); a variable so a test can say so without
// building a PLAYER_INFO.
var underFire = func(st *gamestate.State) bool { return st.InCombat() }

// urgent reports whether a bite that has not shown is too risky to wait for: hitpoints unread or under BiteUrgentPercent of the
// maximum, lower than they were when the bite went out, or a hit taken in the last ten ticks (something is hitting the account:
// a fight keeps the hitpoints hovering, so a fall below the baseline alone would miss it).
func urgent(st *gamestate.State, rec *biteRec) bool {
	hp, ok := st.HP()
	maxHP, ok2 := st.MaxHP()
	if !ok || !ok2 || maxHP < 1 {
		return true
	}
	if hp*100 < maxHP*BiteUrgentPercent {
		return true
	}
	if rec != nil && rec.hpBefore >= 0 && hp < rec.hpBefore {
		return true
	}
	return underFire(st)
}

// landed reports whether rec shows in the session now, and how. Hitpoints count with a rise of one while the first wait is on,
// and of BiteLateRise afterwards.
func landed(st *gamestate.State, rec *biteRec, late bool) (bool, string) {
	if rec.changed != nil && rec.changed() {
		return true, "the food left its slot or changed form"
	}
	if rec.hpBefore >= 0 {
		rise := 1
		if late {
			rise = BiteLateRise
		}
		if hp, ok := st.HP(); ok && hp >= rec.hpBefore+rise {
			return true, fmt.Sprintf("hitpoints %d -> %d", rec.hpBefore, hp)
		}
	}
	return false, ""
}

// waitUntil polls cond every biteStep until it holds or deadline passes (one last look at the deadline). stopped is true when the
// Delay said stop.
func (e BiteEnv) waitUntil(deadline time.Time, cond func() bool) (ok, stopped bool) {
	for {
		if cond() {
			return true, false
		}
		left := deadline.Sub(e.now())
		if left <= 0 {
			return false, false
		}
		if !e.delay(min(biteStep, left)) {
			return false, true
		}
	}
}

func roundMS(d time.Duration) time.Duration { return d.Round(100 * time.Millisecond) }

// LogSlowBite says so when a confirmed bite took BiteSlowAfter or longer to show, so the slow routes are countable in the log
// ("the bite of Shrimps took 3.1s to show").
func LogSlowBite(logf func(string, ...any), food string, took time.Duration) {
	if took >= BiteSlowAfter && logf != nil {
		logf("the bite of %s took %s to show (confirmed: a slow route, not a failed bite)", food, roundMS(took))
	}
}

// EatBite sends one bite (or credits the earlier one that has landed since) and reports whether a bite is known to have landed.
// See the top of this file for what a false means and what the ledger does between calls.
func EatBite(env BiteEnv, req BiteRequest) bool {
	if env.Session == nil || req.Pick == nil {
		return false
	}
	st := env.Session.State()
	if st == nil {
		return false
	}
	if prev := pendingBite(st); prev != nil {
		if done, result := resolveEarlier(env, st, prev); done {
			return result
		}
	}
	if left := DelayLeft(env.Session, env.now()); left > 0 {
		left = min(left, BiteDelayWaitMax)
		if sayDue(st, "delay", env.now()) {
			env.logf("bite: the player is held for another %s by a cast or a rub (the engine drops an eat while it is delayed): the bite of %s waits for it",
				roundMS(left), req.Food)
		}
		if !env.delay(left) {
			return false
		}
	}
	pick, ok := req.Pick()
	if !ok {
		return false
	}
	hpBefore := -1
	if hp, ok := st.HP(); ok {
		hpBefore = hp
	}
	if err := Eat(env.Session, pick.Obj, pick.Slot, PlayerPack); err != nil {
		env.logf("eat %s: %v", req.Food, err)
		return false
	}
	sent := env.now()
	rec := &biteRec{food: req.Food, sentAt: sent, hpBefore: hpBefore, changed: pick.Changed}
	setPendingBite(st, rec)
	wait := req.Wait
	if wait <= 0 {
		wait = BiteConfirmWait
	}
	ok, stopped := env.waitUntil(sent.Add(wait), func() bool { l, _ := landed(st, rec, false); return l })
	if ok {
		clearPendingBite(st, rec)
		took := env.now().Sub(sent)
		LogSlowBite(env.Logf, req.Food, took)
		noteBiteLag(st, env.now(), took, false)
		return true
	}
	if stopped {
		return false
	}
	hp, _ := st.HP()
	maxHP, _ := st.MaxHP()
	waited := env.now().Sub(sent)
	noteBiteLag(st, env.now(), waited, false)
	env.logf("bite: the bite of %s has not shown after %s at %d/%d hitpoints: watching for it for %s from the click (a late landing is credited, not eaten again)",
		req.Food, roundMS(waited), hp, maxHP, BiteWatchWindow)
	return false
}

// resolveEarlier deals with a bite still in the ledger when another is asked for. done is true when EatBite must return result
// without sending: the earlier bite landed (true), or it was waited for and never did (false: the caller's failure to record).
func resolveEarlier(env BiteEnv, st *gamestate.State, prev *biteRec) (done, result bool) {
	age := env.now().Sub(prev.sentAt)
	if age < 0 {
		age = 0
	}
	if age > BiteWatchWindow {
		// Too old to tell a landing from anything else that moved hitpoints or the pack since: forget it.
		clearPendingBite(st, prev)
		noteBiteLag(st, env.now(), age, true)
		return false, false
	}
	if ok, how := landed(st, prev, true); ok {
		clearPendingBite(st, prev)
		noteBiteLag(st, env.now(), age, false)
		env.logf("bite: the bite of %s landed %s after its click (%s): a late landing, credited and not eaten again", prev.food, roundMS(age), how)
		return true, true
	}
	if urgent(st, prev) {
		hp, _ := st.HP()
		maxHP, _ := st.MaxHP()
		clearPendingBite(st, prev)
		noteBiteLag(st, env.now(), age, true)
		env.logf("bite: hitpoints %d/%d: not waiting for the bite of %s sent %s ago; sending another", hp, maxHP, prev.food, roundMS(age))
		return false, false
	}
	// The wait ends early if the account turns urgent while it lasts (a hit lands, hitpoints fall): then the next bite goes out.
	hurt := false
	ok, stopped := env.waitUntil(prev.sentAt.Add(BiteWatchWindow), func() bool {
		if l, _ := landed(st, prev, true); l {
			return true
		}
		hurt = urgent(st, prev)
		return hurt
	})
	if stopped {
		return true, false
	}
	if ok {
		if landedNow, how := landed(st, prev, true); landedNow {
			clearPendingBite(st, prev)
			noteBiteLag(st, env.now(), env.now().Sub(prev.sentAt), false)
			env.logf("bite: the bite of %s landed %s after its click (%s): a late landing, credited and not eaten again", prev.food, roundMS(env.now().Sub(prev.sentAt)), how)
			return true, true
		}
		hp, _ := st.HP()
		maxHP, _ := st.MaxHP()
		clearPendingBite(st, prev)
		noteBiteLag(st, env.now(), env.now().Sub(prev.sentAt), true)
		env.logf("bite: hitpoints %d/%d: not waiting any longer for the bite of %s sent %s ago; sending another", hp, maxHP, prev.food, roundMS(env.now().Sub(prev.sentAt)))
		return false, false
	}
	clearPendingBite(st, prev)
	hp, _ := st.HP()
	noteBiteLag(st, env.now(), env.now().Sub(prev.sentAt), true)
	env.logf("timed out waiting for the bite to register: the bite of %s sent %s ago never showed (hitpoints %d now)", prev.food, roundMS(env.now().Sub(prev.sentAt)), hp)
	return true, false
}

// SettleBite looks at the session's bite in flight without eating: a landing is credited and logged, a bite that outlived its
// window is dropped (and logged, if only just). The bot calls it once a pass, so a bite that landed after its eater gave up is
// counted even when the account no longer needs to eat.
func SettleBite(env BiteEnv) {
	if env.Session == nil {
		return
	}
	st := env.Session.State()
	if st == nil {
		return
	}
	rec := pendingBite(st)
	if rec == nil {
		return
	}
	age := env.now().Sub(rec.sentAt)
	if age < 0 {
		age = 0
	}
	if age > BiteWatchWindow {
		clearPendingBite(st, rec)
		noteBiteLag(st, env.now(), age, true)
		if age <= 2*BiteWatchWindow {
			hp, _ := st.HP()
			env.logf("timed out waiting for the bite to register: the bite of %s sent %s ago never showed (hitpoints %d now)", rec.food, roundMS(age), hp)
		}
		return
	}
	if ok, how := landed(st, rec, true); ok {
		clearPendingBite(st, rec)
		noteBiteLag(st, env.now(), age, false)
		env.logf("bite: the bite of %s landed %s after its click (%s): a late landing, credited and not eaten again", rec.food, roundMS(age), how)
	}
}
