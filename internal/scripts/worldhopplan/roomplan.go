package worldhopplan

import "time"

// MAKING ROOM ON A FULL WORLD — the decision half.
//
// THE PROBLEM THIS EXISTS FOR. worldfailover.go answers "this world is full" by moving the
// refused account somewhere else. That is right for an account that only wants to be
// SOMEWHERE, and wrong for one that needs to be on a PARTICULAR world — a donor going to
// the mule, or a swarm account joining a gather, where "world 2 instead" is not a weaker
// version of the answer but a different answer that does not work at all. Those accounts
// need the world they asked for, so the fleet has to free a slot on it rather than send
// them away.
//
// The lever is that a world's population is partly the fleet's own. The live server caps a world at
// 1,000 players and this fleet alone holds hundreds of them, most with no reason to be on
// any particular world. Asking one of those to log out and come back on world 2 or 3 costs
// that account a hop and frees a slot for the account that genuinely needs it.
//
// WHAT THIS FILE DOES AND DOES NOT DECIDE. It chooses WHICH account should be asked to
// leave, and refuses to choose at all when no choice is safe. It does not perform the hop,
// does not know what a session is, and cannot observe a world's true population — the
// fleet's own view of who it has where is the only census available, and a slot freed can
// always be taken by a real player before the waiting account gets there. So this is a
// best-effort nudge, and every caller must still tolerate failing again.
//
// THE RULES, in the order they disqualify a candidate. Each exists for a failure it
// prevents, and the ones that matter most are the first two: evicting the wrong account
// is worse than not making room at all, because it breaks work that was going fine.
//
//  1. PINNED accounts are never moved. A pin means "this account needs this world" — the
//     mule above all, since every donor in the fleet is walking toward wherever it is, so
//     moving the mule to free one slot would strand every hand-off at once. A pinned
//     account is the whole reason room is being made; it must not also be the price.
//  2. An account already HOPPING is never picked. It is between worlds, its slot is
//     already in flux, and a second hop on top of a running one is refused anyway
//     (fleet.ErrHopInProgress) — picking it would burn the attempt for nothing.
//  3. An account not actually IN GAME on that world frees nothing by moving, so it is not
//     a candidate. This is the rule that keeps a reconnecting or stopped account from
//     being counted as room that does not exist.
//  4. An account evicted RECENTLY is not picked again. Without this the same unlucky
//     account is chosen every time — it is the one that most recently left, so it is the
//     one whose own failover most recently put it back — and it ping-pongs between worlds
//     doing no work at all while the rest of the fleet is untouched.
//
// Among whatever survives, the choice is by the caller's stable spread number rather than
// by position, so two managers asking at the same instant do not converge on one account,
// and so the choice does not silently track roster order.

// RoomCandidate is one account the fleet could ask to leave a world, with everything the
// rules above need. The fleet fills these in from its own state; nothing here reaches into
// a session.
type RoomCandidate struct {
	// Username identifies the account and is what the caller acts on.
	Username string
	// World is the world this account is currently on.
	World int
	// Pinned is true when this account has declared it needs its world (rule 1).
	Pinned bool
	// Hopping is true when a world hop is already running for it (rule 2).
	Hopping bool
	// InGame is true when the account is actually logged in and occupying a slot (rule 3).
	InGame bool
	// LastEvicted is when this account was last asked to leave a world to make room, or
	// the zero time if it never has been (rule 4).
	LastEvicted time.Time
	// Spread is a stable, well-distributed number for this account — fleet.spreadOf's own
	// FNV-1a of the username. Only its stability and distribution matter.
	Spread int
}

// DefaultRevisitCooldown is how long an account is left alone after being asked to leave a
// world. Long enough that it has landed, restarted its script and done some work before it
// can be considered again — a hop costs a logout, a paced login and a script restart, so
// an account cycled much faster than this would spend its life in transit.
const DefaultRevisitCooldown = 10 * time.Minute

// DefaultMakeRoomInterval is the shortest gap between two accounts being asked to leave
// the SAME world. Room is made one account at a time on purpose: a hop is not instant, so
// asking ten accounts at once would empty ten slots to fill one, and the nine wasted hops
// are nine scripts stopped and restarted for nothing.
const DefaultMakeRoomInterval = 20 * time.Second

// ChooseEvictee picks the account that should be asked to leave world so that someone who
// needs it can get in.
//
// Reports ok=false when no candidate is eligible, which is a normal and common answer: a
// world holding only pinned accounts has no room to give, and so does one whose every
// movable account was already asked recently. The caller must treat that as "wait", never
// as a reason to relax the rules.
func ChooseEvictee(world int, cands []RoomCandidate, now time.Time, cooldown time.Duration) (RoomCandidate, bool) {
	if cooldown <= 0 {
		cooldown = DefaultRevisitCooldown
	}
	var best RoomCandidate
	found := false
	for _, c := range cands {
		if c.World != world || !c.InGame || c.Pinned || c.Hopping {
			continue
		}
		if !c.LastEvicted.IsZero() && now.Sub(c.LastEvicted) < cooldown {
			continue
		}
		// Stable pick: lowest spread wins, username breaking an exact tie so the result
		// is total rather than dependent on the order the fleet happened to list them.
		if !found || c.Spread < best.Spread || (c.Spread == best.Spread && c.Username < best.Username) {
			best, found = c, true
		}
	}
	return best, found
}

// RoomIsNeeded reports whether an account that must reach `want` should have room made for
// it, rather than being failed over to a different world.
//
// The distinction is the whole point of this file: only an account PINNED to `want` gets
// room made, because only it has said that another world is not an acceptable answer.
// Everything else is better served by worldfailover.go's ordinary move, which costs the
// fleet nothing and does not disturb an account that was working.
func RoomIsNeeded(pinnedWorld int, pinned bool, want int) bool {
	return pinned && pinnedWorld == want && want > 0
}

// CanAskAgain reports whether enough time has passed since room was last made on a world.
// lastAsk is the zero time when nobody has been asked yet.
func CanAskAgain(lastAsk, now time.Time, interval time.Duration) bool {
	if interval <= 0 {
		interval = DefaultMakeRoomInterval
	}
	return lastAsk.IsZero() || now.Sub(lastAsk) >= interval
}
