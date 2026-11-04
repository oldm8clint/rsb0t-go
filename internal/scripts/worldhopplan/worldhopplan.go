// Package worldhopplan is the pure decision half of world hopping: when an account should
// leave its own world to reach a mule on another one, when it should come home, and — the
// part that matters most — when it should stop trying. No client, no actions, no fleet, no
// bot imports, per CLAUDE.md's three-layer pattern. The live I/O half is
// internal/fleet.Manager.HopWorld plus internal/scripts/donorrunner's hop hooks.
//
// WHAT A HOP ACTUALLY IS. The login block carries no world number (see internal/worlds):
// a world is purely a different wsURL. So a "hop" is a full logout and a full login on a
// different URL. Three consequences drive every rule below.
//
//  1. A HOP IS A LOGIN. It queues behind internal/fleet's fleet-wide login-rate gate and
//     takes a slot in the route pool exactly like any other login. It is therefore not
//     cheap: at 269 accounts the fleet's login budget is the scarcest thing it has, and two
//     hops per hand-off (out and back) is two more logins per hand-off, per donor.
//     MinInterval exists so a script cannot spend that budget in a loop.
//
//  2. THE SCRIPT DOES NOT SURVIVE IT. internal/fleet stops the bot when the session goes
//     and starts a FRESH one after the new login (Manager.StartBot builds a new
//     bot.Context and a new Runner). Nothing in a script's own memory crosses a hop —
//     only its settings map and whatever internal/settings has persisted. A plan that
//     assumed otherwise would silently lose state, so the hop is modelled as a
//     restart-with-an-intent rather than a pause.
//
//  3. RESPONSE 5 IS THE HAZARD. "Already logged in" means the old session is still up on
//     the server. Retrying into it is precisely the flapping this package exists to stop,
//     and CLAUDE.md's rule is to stop the account rather than retry. OutcomeAlreadyLoggedIn
//     therefore never earns an immediate retry: it earns a wait, and on repeat it ends the
//     hop entirely.
//
// THE LIVELOCK THIS IS BUILT AGAINST. An account that hops to the mule's world, fails to
// get back (the home world is at its per-address connection ceiling, say), hops again, and
// so on, is worse than an account that simply never hopped: it burns login budget forever
// and does no work on either world. So every terminal decision lands the account SOMEWHERE
// USEFUL rather than in a retry loop:
//
//   - Failing to reach the mule's world => stay home and work. The hand-off is delayed,
//     which costs bank space; the account keeps earning.
//   - Failing to get home => stay on the mule's world and work there. The account is in
//     the game, its script restarts, and its own home-world bank is out of reach — which is
//     a real cost, and why GiveUpHere is only ever reached after ActionHome has spent its
//     attempts.
//   - Unable to log in anywhere => ActionStop. Nothing a hop can do fixes that, and
//     flapping between two worlds while it is true is the worst available answer.
package worldhopplan

import (
	"fmt"
	"time"
)

// Outcome is what the last hop attempt did.
type Outcome string

const (
	// OutcomeNone: no attempt yet.
	OutcomeNone Outcome = ""
	// OutcomeOK: the account logged in on the world it was aiming at.
	OutcomeOK Outcome = "ok"
	// OutcomeFailed: an ordinary login failure — a timeout, a refused connection, a
	// transport fault. Worth another attempt.
	OutcomeFailed Outcome = "failed"
	// OutcomeCeiling: the address is at its concurrent-connection limit (login response
	// 9). Nothing this account can do frees a slot, so this is not a fault to retry
	// quickly; internal/fleet's own per-account ceiling backoff already paces it.
	OutcomeCeiling Outcome = "ceiling"
	// OutcomeAlreadyLoggedIn: login response 5. The previous session is still up on the
	// server. Never retried immediately — see the package doc.
	OutcomeAlreadyLoggedIn Outcome = "already-logged-in"
	// OutcomeTerminal: a login failure that no wait fixes (bad credentials, a rejected
	// key that did not rotate). Ends the hop at once.
	OutcomeTerminal Outcome = "terminal"
	// OutcomeWorldFull: login response 7, the target world is at its player cap (1,000 by
	// default on this engine). Waiting does not make a popular world smaller, so the right
	// answer is a DIFFERENT world — and, having none to offer, to land and keep working.
	// Emphatically not a rate verdict: reading it as one would penalise the whole fleet
	// because one world is busy.
	OutcomeWorldFull Outcome = "world-full"
	// OutcomeHopTimer: login response 21, the server's own world-hop timer — this account
	// logged out of another world too recently (45s by default, counted from the logout).
	// Purely a wait, and the server states how long, so it is neither a failure nor worth
	// an attempt against the leg's budget.
	OutcomeHopTimer Outcome = "hop-timer"
)

// Action is what to do next.
type Action string

const (
	// ActionStay: do nothing, keep working where the account already is.
	ActionStay Action = "stay"
	// ActionHop: log out and log back in on Target.
	ActionHop Action = "hop"
	// ActionHome: go back to the account's own world.
	ActionHome Action = "home"
	// ActionGiveUpHere: stop hopping and work on whichever world the account is on now.
	// Not a failure state to retry out of — a deliberate landing.
	ActionGiveUpHere Action = "give-up-here"
	// ActionStop: stop the account. Reached only when the account cannot get into the game
	// at all, or when response 5 says a session is already up.
	ActionStop Action = "stop"
	// ActionWait: an attempt is owed a cooling-off period before the next one.
	ActionWait Action = "wait"
)

// Limits bound how hard an account may try. Defaults are deliberately small: every unit of
// this budget is spent out of the fleet's shared login pacing.
type Limits struct {
	// MaxAttempts is how many login attempts one leg of a hop (out, or back) may make
	// before the leg is abandoned.
	MaxAttempts int
	// MaxHops is how many hops one account may make inside Window, counting both legs. A
	// round trip is 2. This is the flapping bound.
	MaxHops int
	// Window is the span MaxHops is counted over.
	Window time.Duration
	// MinInterval is the shortest gap between two hops by one account, whatever the
	// reason. Stops a script that decides to hop on every loop iteration from doing so.
	MinInterval time.Duration
	// RetryWait is the pause between two attempts on the same leg.
	RetryWait time.Duration
	// AlreadyLoggedInWait is the longer pause after a response 5, giving the server time
	// to notice the old session is gone.
	AlreadyLoggedInWait time.Duration
	// MuleWorldTTL is how recently the mule must have announced its world for that world
	// to be believed. Older than this and the mule's world is UNKNOWN, which is not the
	// same as "the same world as me" — see Decide.
	MuleWorldTTL time.Duration
}

// DefaultHopTimer is the engine's own default world-hop wait: 45 seconds
// (engine/src/util/WorldConfig.ts, `hopTime: 45000`), counted from the account's LOGOUT on
// the previous world. Used only when a response 21 did not carry its seconds.
const DefaultHopTimer = 45 * time.Second

// DefaultLimits are the conservative numbers a script gets when it configures none.
//
// MaxHops 4 in 30 minutes allows two full round trips an hour and no more. The measured
// steady-state mule demand (2026-09-21) is 0.4 distinct donors a minute against a mule that
// is 29% busy, so a donor hands off on the order of once an hour anyway; four hops an hour
// is comfortably above the real rate and far below anything that could be called flapping.
//
// AlreadyLoggedInWait is 60s because that is the engine's own idle logout window (about 60s
// without bytes from the client, CLAUDE.md's Keepalive gotcha) — the outer bound on how
// long a genuinely dead session can still look alive to the server.
var DefaultLimits = Limits{
	MaxAttempts:         3,
	MaxHops:             4,
	Window:              30 * time.Minute,
	MinInterval:         2 * time.Minute,
	RetryWait:           20 * time.Second,
	AlreadyLoggedInWait: 60 * time.Second,
	MuleWorldTTL:        30 * time.Second,
}

// withDefaults fills any zero field from DefaultLimits, so a caller can set one knob
// without restating the rest.
func (l Limits) withDefaults() Limits {
	if l.MaxAttempts <= 0 {
		l.MaxAttempts = DefaultLimits.MaxAttempts
	}
	if l.MaxHops <= 0 {
		l.MaxHops = DefaultLimits.MaxHops
	}
	if l.Window <= 0 {
		l.Window = DefaultLimits.Window
	}
	if l.MinInterval <= 0 {
		l.MinInterval = DefaultLimits.MinInterval
	}
	if l.RetryWait <= 0 {
		l.RetryWait = DefaultLimits.RetryWait
	}
	if l.AlreadyLoggedInWait <= 0 {
		l.AlreadyLoggedInWait = DefaultLimits.AlreadyLoggedInWait
	}
	if l.MuleWorldTTL <= 0 {
		l.MuleWorldTTL = DefaultLimits.MuleWorldTTL
	}
	return l
}

// Presence is one account's last bus announcement of where it is, world included. Built by
// the caller from internal/mulebus messages; this package never imports that one, so the
// decision logic stays testable with plain values.
type Presence struct {
	// Name is the account that announced.
	Name string
	// World is the world it said it was on. 0 means it did not say — an older publisher,
	// or one built before worlds existed. Deliberately NOT read as "world 1": see
	// MuleWorld.
	World int
	// At is when the announcement was heard.
	At time.Time
}

// MuleWorld resolves which world the named mule is on from the presence announcements
// heard so far, newest wins. known is false when nothing fresh enough was heard, or when
// what was heard carried no world at all.
//
// THE 0 CASE IS THE ONE THAT MATTERS. A heartbeat with World 0 is a publisher that does
// not carry a world, not a publisher on world zero (there is no world zero). Reading it as
// world 1 would make a donor on world 2 hop to world 1 on the strength of a message that
// never mentioned a world — a hop costing two logins, taken on no evidence. So 0 is
// UNKNOWN, and Decide's rule for unknown is to stay put: the pre-existing behaviour, where
// every account was on one world and the hand-off simply worked, is exactly what "stay put"
// produces.
func MuleWorld(mule string, seen []Presence, now time.Time, ttl time.Duration) (world int, known bool) {
	if ttl <= 0 {
		ttl = DefaultLimits.MuleWorldTTL
	}
	var best time.Time
	for _, p := range seen {
		if !namesMatch(p.Name, mule) {
			continue
		}
		if p.World <= 0 {
			continue
		}
		if now.Sub(p.At) > ttl || p.At.After(now) {
			continue
		}
		if p.At.After(best) {
			best, world, known = p.At, p.World, true
		}
	}
	return world, known
}

// namesMatch compares account names the way every other part of this project does:
// case-insensitively, with underscores and spaces equivalent (the userhash treats
// "Bob_Smith" and "bob smith" as one account).
func namesMatch(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if foldName(a[i]) != foldName(b[i]) {
			return false
		}
	}
	return true
}

func foldName(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		c += 'a' - 'A'
	}
	if c == '_' {
		return ' '
	}
	return c
}

// State is one account's hop history. The caller owns it across hops (it must outlive a
// script restart, since a hop restarts the script — internal/fleet holds it per account).
type State struct {
	// Home is the world the roster gave this account: where it belongs when nothing is
	// pulling it elsewhere.
	Home int
	// Current is the world it is actually on right now.
	Current int
	// Leg is the world the in-progress hop is aiming at, 0 when no hop is in progress.
	Leg int
	// Attempts is how many login attempts the current leg has spent.
	Attempts int
	// Last is the outcome of the most recent attempt.
	Last Outcome
	// LastAt is when that attempt finished.
	LastAt time.Time
	// Recent holds the times of hops inside the window, for the MaxHops bound.
	Recent []time.Time
	// HomeLegSpent records that the "get back home" leg has already used its attempts, so
	// a later failure lands the account rather than starting the leg again.
	HomeLegSpent bool
}

// View is everything Decide needs: the account's state, what it wants, and the clock.
type View struct {
	State State
	// WantWorld is the world the script would like to be on — typically the mule's. 0
	// means "no preference", which resolves to going home.
	WantWorld int
	// WantKnown is false when the script could not determine the mule's world at all.
	// Distinct from WantWorld 0, because "I do not know" and "I have no preference" want
	// different answers: the first stays put, the second goes home.
	WantKnown bool
	// Now is the clock.
	Now time.Time
	// Limits bounds the attempt.
	Limits Limits
	// HopTimerWait is how long the SERVER said to wait after a response 21, taken from that
	// response's own payload byte. 0 falls back to DefaultHopTimer. Always prefer the
	// server's number: hopTime is configurable (NODE_HOP_TIME), and on this engine its
	// fallback is read from NODE_MAX_NPCS by an upstream bug in WorldConfig.ts, so the real
	// value on any given server is genuinely not predictable.
	HopTimerWait time.Duration
	// Distinct is internal/worlds.Map.Distinct(): whether this deployment can actually put
	// two accounts on two different worlds. False on a local engine, where every world
	// number resolves to the same URL — hopping there would be a pointless logout/login
	// cycle, so Decide refuses it rather than performing a hop that proves nothing.
	Distinct bool
}

// Decision is Decide's answer.
type Decision struct {
	Action Action
	// World is the world to log in on, meaningful for ActionHop and ActionHome.
	World int
	// Wait is how long to hold off, meaningful for ActionWait.
	Wait time.Duration
	// Why is a one-line reason for the log. Always set.
	Why string
}

// Decide is the whole policy. It is a pure function of View: same input, same answer.
func Decide(v View) Decision {
	l := v.Limits.withDefaults()
	s := v.State

	// A deployment that cannot tell two worlds apart must never hop. On a local engine a
	// "hop" is a logout and a login on the identical URL — real machinery, zero world
	// change — and performing one would let a single-world test be reported as a
	// two-world proof. Refused here, in the plan, so every caller inherits the refusal.
	if !v.Distinct {
		return Decision{Action: ActionStay, Why: "this deployment has only one world (no per-world URL), so a hop would change nothing"}
	}

	// A leg that has run out of attempts lands the account somewhere useful. Checked before
	// anything else, so an exhausted leg can never be restarted by a fresh want.
	if s.Leg != 0 && s.Attempts >= l.MaxAttempts {
		if s.Leg == s.Home {
			// Could not get home. Work here rather than flap.
			return Decision{Action: ActionGiveUpHere, World: s.Current,
				Why: fmt.Sprintf("could not get back to world %d in %d attempts — staying on world %d and working there", s.Home, s.Attempts, s.Current)}
		}
		if s.Current == s.Home {
			// Could not reach the mule's world, and we never left. Just work.
			return Decision{Action: ActionGiveUpHere, World: s.Current,
				Why: fmt.Sprintf("could not reach world %d in %d attempts — staying home on world %d", s.Leg, s.Attempts, s.Home)}
		}
		// Stranded somewhere that is neither home nor the target: go home once.
		if !s.HomeLegSpent {
			return Decision{Action: ActionHome, World: s.Home,
				Why: fmt.Sprintf("could not reach world %d — returning to world %d", s.Leg, s.Home)}
		}
		return Decision{Action: ActionGiveUpHere, World: s.Current,
			Why: fmt.Sprintf("out of attempts on both legs — working on world %d", s.Current)}
	}

	// The server's own hop timer is a plain wait with a number attached, so it costs the
	// leg nothing: the account did nothing wrong and the server said exactly how long. This
	// is checked before the attempt bounds for that reason — a hop must not burn its three
	// attempts on a timer that was always going to expire.
	if s.Last == OutcomeHopTimer {
		w := v.HopTimerWait
		if w <= 0 {
			w = DefaultHopTimer
		}
		if left := w - v.Now.Sub(s.LastAt); left > 0 {
			return Decision{Action: ActionWait, Wait: left,
				Why: fmt.Sprintf("the server's world-hop timer has %s left", left.Round(time.Second))}
		}
		if s.Leg == s.Home {
			return Decision{Action: ActionHome, World: s.Home, Why: "the hop timer has expired; returning home"}
		}
		if s.Leg > 0 {
			return Decision{Action: ActionHop, World: s.Leg, Why: fmt.Sprintf("the hop timer has expired; world %d", s.Leg)}
		}
	}

	// A full world is not a fault and not a wait: it is a reason to be somewhere else. With
	// no other world to offer, the account lands and keeps working rather than queueing for
	// a seat that a thousand players are sitting in.
	if s.Last == OutcomeWorldFull {
		if s.Leg == s.Home {
			return Decision{Action: ActionGiveUpHere, World: s.Current,
				Why: fmt.Sprintf("world %d is full — staying on world %d rather than queueing for a seat", s.Home, s.Current)}
		}
		return Decision{Action: ActionGiveUpHere, World: s.Current,
			Why: fmt.Sprintf("world %d is full — the hand-off waits, this account keeps working on world %d", s.Leg, s.Current)}
	}

	// Response 5 is never retried into. One occurrence buys a wait; a second ends it.
	if s.Last == OutcomeAlreadyLoggedIn {
		if s.Attempts >= 2 {
			return Decision{Action: ActionStop,
				Why: "the server still reports this account as already logged in — stopping it rather than retrying, per CLAUDE.md"}
		}
		if w := l.AlreadyLoggedInWait - v.Now.Sub(s.LastAt); w > 0 {
			return Decision{Action: ActionWait, Wait: w,
				Why: fmt.Sprintf("response 5: the previous session is still up, waiting %s for it to lapse", w.Round(time.Second))}
		}
		// The wait has elapsed — fall through and retry the same leg once.
	}

	// A login failure no wait can fix ends the hop at once.
	if s.Last == OutcomeTerminal {
		return Decision{Action: ActionStop, Why: "the login failure needs attention; a hop cannot fix it"}
	}

	// An in-progress leg that failed retries after RetryWait — or, for a connection
	// ceiling, lands rather than retrying, because nothing this account does frees a slot
	// and internal/fleet already paces its own ceiling probes far more slowly than this.
	if s.Leg != 0 && (s.Last == OutcomeFailed || s.Last == OutcomeCeiling) {
		if s.Last == OutcomeCeiling {
			if s.Leg == s.Home {
				return Decision{Action: ActionGiveUpHere, World: s.Current,
					Why: fmt.Sprintf("world %d is at its connection ceiling — staying on world %d rather than waiting out a slot", s.Home, s.Current)}
			}
			return Decision{Action: ActionGiveUpHere, World: s.Current,
				Why: fmt.Sprintf("world %d is at its connection ceiling — the hand-off waits, this account keeps working", s.Leg)}
		}
		if w := l.RetryWait - v.Now.Sub(s.LastAt); w > 0 {
			return Decision{Action: ActionWait, Wait: w, Why: fmt.Sprintf("retrying world %d in %s", s.Leg, w.Round(time.Second))}
		}
		if s.Leg == s.Home {
			return Decision{Action: ActionHome, World: s.Home, Why: fmt.Sprintf("retrying the return to world %d", s.Home)}
		}
		return Decision{Action: ActionHop, World: s.Leg, Why: fmt.Sprintf("retrying world %d", s.Leg)}
	}

	// Nothing is in progress. Work out where the account wants to be.
	want := v.WantWorld
	if !v.WantKnown {
		// The mule's world is genuinely unknown. Staying put is the pre-worlds behaviour
		// and costs nothing; hopping on a guess costs two logins.
		if s.Current == s.Home {
			return Decision{Action: ActionStay, Why: "the mule has not said which world it is on — staying home"}
		}
		want = s.Home
	}
	if want <= 0 {
		want = s.Home
	}
	if want == s.Current {
		return Decision{Action: ActionStay, Why: fmt.Sprintf("already on world %d", want)}
	}

	// Bounds. Both are checked only for a hop that would actually happen, so an account
	// sitting still never burns budget.
	if w := l.MinInterval - v.Now.Sub(s.LastAt); w > 0 && !s.LastAt.IsZero() {
		return Decision{Action: ActionWait, Wait: w,
			Why: fmt.Sprintf("hopped %s ago; the minimum gap between hops is %s", v.Now.Sub(s.LastAt).Round(time.Second), l.MinInterval)}
	}
	if hopsIn(s.Recent, v.Now, l.Window) >= l.MaxHops {
		// The flapping bound. Land the account on its home world if it can, and otherwise
		// where it stands — never another hop.
		if s.Current == s.Home {
			return Decision{Action: ActionStay,
				Why: fmt.Sprintf("hop budget spent (%d in %s) — staying home on world %d", l.MaxHops, l.Window, s.Home)}
		}
		if !s.HomeLegSpent {
			return Decision{Action: ActionHome, World: s.Home,
				Why: fmt.Sprintf("hop budget spent (%d in %s) — one last hop home to world %d", l.MaxHops, l.Window, s.Home)}
		}
		return Decision{Action: ActionGiveUpHere, World: s.Current,
			Why: fmt.Sprintf("hop budget spent (%d in %s) — working on world %d", l.MaxHops, l.Window, s.Current)}
	}

	if want == s.Home {
		return Decision{Action: ActionHome, World: s.Home, Why: fmt.Sprintf("returning to world %d", s.Home)}
	}
	// Deliberately says the WORLD and not why: this package is asked to move an account and
	// does not know whether the caller wanted the mule, room to work, or something else. The
	// caller's own reason is logged beside this by internal/fleet.HopWorld. An earlier
	// version said "the mule is on world N" here and printed that for a hop nothing to do
	// with a mule, which is the sort of log line that sends the next person the wrong way.
	return Decision{Action: ActionHop, World: want, Why: fmt.Sprintf("moving to world %d", want)}
}

// hopsIn counts the entries of recent that fall inside window ending at now.
func hopsIn(recent []time.Time, now time.Time, window time.Duration) int {
	n := 0
	for _, t := range recent {
		if !t.IsZero() && now.Sub(t) <= window && !t.After(now) {
			n++
		}
	}
	return n
}

// Begin records that a hop to world has started. The caller stores the returned State.
func Begin(s State, world int, now time.Time) State {
	s.Leg = world
	s.Attempts = 0
	s.Last = OutcomeNone
	s.LastAt = now
	s.Recent = append(trim(s.Recent, now, DefaultLimits.Window), now)
	return s
}

// Attempted records one attempt's outcome against the current leg.
func Attempted(s State, out Outcome, now time.Time) State {
	// A hop timer is the server saying "not yet", with a number. It is not an attempt that
	// failed, and counting it would let a 45-second wait eat a leg's whole budget of three.
	if out == OutcomeHopTimer {
		s.Last, s.LastAt = out, now
		return s
	}
	s.Attempts++
	s.Last = out
	s.LastAt = now
	if out == OutcomeOK && s.Leg != 0 {
		s.Current = s.Leg
		if s.Leg == s.Home {
			// Home again: the round trip is over and the home leg's budget is fresh.
			s.HomeLegSpent = false
		}
		s.Leg = 0
		s.Attempts = 0
	}
	return s
}

// LegSpent marks the home leg as having used its attempts, so a later give-up lands the
// account rather than starting the leg over.
func LegSpent(s State) State {
	if s.Leg == s.Home {
		s.HomeLegSpent = true
	}
	s.Leg = 0
	s.Attempts = 0
	return s
}

// Ended records that a hop request is over, however it ended: the in-progress leg, its attempt
// count and its last outcome are cleared, so the NEXT request is decided afresh. Recent (the
// flapping budget), LastAt (the minimum gap between hops) and HomeLegSpent are kept, because
// those bounds are meant to span requests.
//
// Without it one give-up stuck for the life of the process: a single response 7 or 9, or one
// exhausted leg, left Leg/Attempts/Last set, and Decide answered every later request with the
// same give-up without ever attempting a login (review revW, 2026-09-23). The caller runs it
// when its hop loop returns.
func Ended(s State) State {
	s.Leg = 0
	s.Attempts = 0
	s.Last = OutcomeNone
	return s
}

// trim drops hop timestamps older than window, so State.Recent cannot grow without bound
// on an account that runs for a day.
func trim(recent []time.Time, now time.Time, window time.Duration) []time.Time {
	out := recent[:0:0]
	for _, t := range recent {
		if !t.IsZero() && now.Sub(t) <= window {
			out = append(out, t)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Full-world failover
// ---------------------------------------------------------------------------
//
// THE LIVE INCIDENT THIS IS FOR, measured on the production fleet 2026-09-22:
//
//	direct    held=78  ok=58  failed=194   responses={2: 58, 7: 194}
//	warp      held=77  ok=58  failed=194   responses={2: 58, 7: 194}
//	psiphon   held=72  ok=53  failed=190   responses={2: 53, 7: 190}
//	psiphon2  held=72  ok=53  failed=190   responses={2: 53, 7: 190}
//	counts: ingame 222, reconnecting 57, queued 20
//
// 768 rejections, every one of them login response 7 — "This world is full. Please use a
// different world." (the vendored client's own strings, Client.ts:2054). Every route stayed
// healthy and kept logging accounts in, so nothing was broken; 57 accounts simply knocked on
// a world with no room, forever, earning nothing.
//
// Note what that measurement rules OUT, which is worth as much as what it rules in: each
// route still HELD 72-78 accounts and kept succeeding, so the per-address concurrent ceiling
// was NOT biting and this was not a rate verdict. The binding limit was the world's
// population cap (1,000, engine/src/util/WorldConfig.ts). Spreading the same accounts over
// w1/w2 addresses it directly. (w3 existed until 2026-09-22 and no longer does.)
//
// So a full world must mean "be somewhere else", and the rules below are what "somewhere
// else" means. They are here, in the pure package, so the live layer cannot quietly invent
// a different policy.

// DefaultFullWorldTTL is how long one account's response 7 makes a world count as full for
// every other account. A shared memory matters at fleet scale: without it each of ~300
// accounts has to discover the same full world for itself, which is exactly the 768 wasted
// attempts above. Short enough that a world which empties is retried promptly.
const DefaultFullWorldTTL = 90 * time.Second

// NextOpenWorld picks the world an account turned away from `current` should try next.
//
//   - available is every world this deployment knows about, ascending (worlds.Map.Available).
//   - full maps a world to when it last answered "full"; entries older than ttl are ignored.
//   - spread staggers which world each account tries FIRST. Without it, ~300 accounts shut
//     out of world 1 would all pick world 2 at the same instant and fill it, then all pick
//     world 3. Pass something stable per account (a name hash); it only ever rotates the
//     starting point of the search, never changes which worlds are eligible.
//
// ok is false when nothing is eligible — every known world is full, or there is only one
// world. The caller must then wait rather than invent a destination.
func NextOpenWorld(current int, available []int, full map[int]time.Time, now time.Time, ttl time.Duration, spread int) (int, bool) {
	if ttl <= 0 {
		ttl = DefaultFullWorldTTL
	}
	// Candidates: every known world that is not the one that just refused us and is not
	// known-full. Order is ascending, rotated by spread.
	var cands []int
	for _, w := range available {
		if w == current || w <= 0 {
			continue
		}
		if at, ok := full[w]; ok && now.Sub(at) <= ttl && !at.After(now) {
			continue
		}
		cands = append(cands, w)
	}
	if len(cands) == 0 {
		return 0, false
	}
	if spread < 0 {
		spread = -spread
	}
	return cands[spread%len(cands)], true
}

// AnyWorldOpen reports whether any world other than current is believed to have room. A
// convenience over NextOpenWorld for a caller that only wants the yes/no.
func AnyWorldOpen(current int, available []int, full map[int]time.Time, now time.Time, ttl time.Duration) bool {
	_, ok := NextOpenWorld(current, available, full, now, ttl, 0)
	return ok
}
