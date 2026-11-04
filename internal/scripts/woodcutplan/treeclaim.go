package woodcutplan

// Cross-account tree de-confliction: stopping N accounts at one camp from all walking at the
// same tree.
//
// THE OWNER'S ASK, verbatim: "for the woodcutting script doing logs, make sure they spread out
// and not target the same tree as another because trees only give 1 set of logs."
//
// THE PREMISE IS EXACTLY RIGHT, AND capacity.go IN THIS PACKAGE ALREADY PROVES IT FROM CONTENT
// — read its header first rather than taking this one's word for it. In short:
// `trees.dbrow`'s `[normal_tree_table]` carries `respawnrate,0`, and `woodcut.rs2`'s get_logs
// forces `$deplete_chance = 0` in that case, so the deplete roll is `random(0) = 0` and a
// NORMAL tree falls on its FIRST successful log, every time, then stands as a stump for a mean
// 159.5 ticks (~96s). `[normal_tree_table]` is also the table every `deadtree*` belongs to,
// which is what the Draynor Manor start camp is made of, so BOTH of the fleet's crowded cohorts
// are the one-log case. Oak and willow are `respawnrate,30` with the 1-in-8 roll intact, so
// they yield about eight logs and several accounts can share one standing tree; they are the
// mild case.
//
// WHAT THIS CAN AND CANNOT BUY, stated up front because capacity.go's numbers make the limit
// obvious and a later reader should not have to rediscover it. It CANNOT raise a camp's supply:
// twelve normal trees yield about twelve logs every 96 seconds however many accounts stand in
// them, and no amount of coordination changes that. What it can do is stop the supply being
// WASTED. A camp's supply is only collected if somebody is chopping each tree as it comes back;
// when ninety-four accounts all rank the same nearest tree first, one of them gets that log,
// ninety-three spent a walk on a tree that fell before they arrived, and the other eleven trees
// respawn and stand there uncollected. Spreading turns the collected yield toward the supply
// ceiling instead of a fraction of it. That mechanism is reasoned from content, NOT measured —
// see the claim row in COLLAB.md for exactly what was and was not proven.
//
// WHY A SHARED REGISTRY AND NOT internal/mulebus. mulebus already carries KindClaim/KindUnclaim
// for exactly this shape of problem (scarabplan.ClaimBoard is the ground-loot version), and
// going that way was the obvious first answer. It is the wrong one here, for one reason:
// mulebus is eventually consistent by construction. Every reader keeps its OWN board and folds
// in messages when it next polls, so two accounts choosing in the same instant BOTH believe
// they won until the messages propagate; scarabplan says so in its own header and accepts it
// ("a missed message must degrade, not break — one wasted trip"). A wasted trip is a fine cost
// for a cactus that will still be there. It is the entire cost being paid here. The requirement
// is an ATOMIC winner, so the decision has to happen inside one lock that both accounts go
// through, which is what this file is: one mutex-guarded map, read-and-take in a single
// critical section, so of two simultaneous callers exactly one leaves holding the tree.
//
// It stays in the plan package rather than a new sibling because it imports nothing but
// sync/time/tile — no session, no actions, no fleet, no bot — so the three-layer rule
// (docs/script_porting_framework.md) is satisfied and the decision logic stays unit-testable
// without a live client. Concurrency is not I/O.
//
// THE FOUR RULES IT IS BUILT AROUND, because the happy path is the easy part:
//
//  1. A CLAIM MUST EXPIRE. An account that dies, disconnects, is fail-teleported by a Genie or
//     is simply stopped mid-walk stops refreshing, and TreeClaimTTL then frees its tree for
//     everybody else. Nothing is ever locked forever, and no observation by anyone else is
//     needed to unlock it — the same self-healing argument TreeBlocks makes in trees.go.
//  2. AN ACCOUNT NEVER STANDS STILL. A claim is a PREFERENCE, not a permit. When every tree in
//     range is claimed by somebody else, PickTreeFor still returns one to chop — flagged
//     Shared, so the caller can say so — because a camp with more accounts than trees is the
//     fleet's normal state and refusing to chop there would cost more than doubling up does.
//     That is the opposite of TreeBlocks, where "blocked" means "chopping this breaks the axe"
//     and falling back is genuinely wrong; the two are kept separate for that reason.
//  3. TWO SIMULTANEOUS CHOOSERS MUST NOT BOTH WIN. ClaimFirstFree walks the caller's whole
//     preference order inside one lock and takes the first tree that is free at that moment,
//     so there is no read-then-write window for a second account to slip into.
//  4. THE FALLBACK MUST SPREAD TOO. When everything is claimed, returning "the nearest" would
//     stack every spare account on one tile — which is the pathology, just moved one step
//     along. The spare accounts are spread deterministically instead, by rendezvous-weighting
//     each candidate with a hash of the account's own identity and the tree's own tile (see
//     spreadPick for why the tile has to be in the hash). HONEST STATUS: reasoned from the
//     geometry, NOT measured on its own — the live A/B below measured the whole change, not
//     this rule separately.

import (
	"sync"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// TreeClaimTTL is how long a claim stands without a refresh.
//
// It has to outlast the two things a holder legitimately does while holding: the walk from
// wherever it was to the tree (a camp leash is 8-14 tiles, so a few seconds, and
// walkToInteractLoc bounds one approach at interactApproachTimeout = 20s and may try four),
// and then the chop itself, which on an oak or a willow runs until the pack fills. The chop
// half refreshes (TreeClaimRefresh), so this only has to cover the walk plus slack. A minute
// covers four full 20s approaches with room to spare, and frees a dead account's tree inside
// one respawn cycle of a normal tree.
const TreeClaimTTL = 60 * time.Second

// TreeClaimRefresh is how often a holder re-stamps its claim while still working the tree.
// Well under TreeClaimTTL so one slow pass does not drop a live claim.
const TreeClaimRefresh = 10 * time.Second

type treeClaim struct {
	by string
	at time.Time
}

// TreeClaims is the set of trees accounts in this process have reserved.
//
// Safe for concurrent use: every account in the fleet is a goroutine in one process and they
// all share one instance, so the lock is the whole point rather than an afterthought.
type TreeClaims struct {
	mu   sync.Mutex
	ttl  time.Duration
	held map[tile.Tile]treeClaim
}

// NewTreeClaims returns an empty registry with the default TTL. Tests and single-account
// harnesses make their own so they do not share the fleet's.
func NewTreeClaims() *TreeClaims { return NewTreeClaimsTTL(TreeClaimTTL) }

// NewTreeClaimsTTL returns an empty registry with a caller-chosen TTL, so a test can prove the
// expiry rule without sleeping a minute.
func NewTreeClaimsTTL(ttl time.Duration) *TreeClaims {
	if ttl <= 0 {
		ttl = TreeClaimTTL
	}
	return &TreeClaims{ttl: ttl, held: map[tile.Tile]treeClaim{}}
}

// SharedTreeClaims is the one registry every account in this process uses.
//
// A process-wide singleton is defensible here for a reason that does not generalise: a claim is
// only meaningful between accounts standing in the same game world, every account this process
// runs is in that one world, and tree tiles are globally unique coordinates in it. Two separate
// registries in one process would simply fail to de-conflict, which is the bug. Anything that
// wants isolation — a unit test, a single-account harness measuring one account's behaviour —
// calls NewTreeClaims instead and passes it explicitly.
func SharedTreeClaims() *TreeClaims { return SharedTreeClaimsFor(0) }

// sharedByWorld holds one registry per world, because a claim only means anything between
// accounts that can actually contend for the tree.
//
// TWO ACCOUNTS ON DIFFERENT WORLDS STANDING AT THE SAME TILE ARE DIFFERENT PLAYERS. They
// cannot see each other and cannot take each other's tree, so one must never block the other.
// The registry was keyed on tile alone, which was right while the fleet lived on one world and
// silently wrong the moment it did not: with three worlds it would de-conflict accounts that
// cannot contend, handing out "every tree is taken" to a world that is empty and giving back
// the +29% cohort woodcutting XP this file was written to win.
//
// One registry per world rather than a world in the key: the key is used by eight methods and a
// composite would have to thread a world through every one of them, where a missed call site
// fails SILENTLY and looks exactly like ordinary contention. A registry an account cannot even
// name cannot be shared by mistake.
var (
	sharedMu      sync.Mutex
	sharedByWorld = map[int]*TreeClaims{}
)

// SharedTreeClaimsFor is the process-wide registry for one world. World 0 means "the fleet's
// own world", which is what a caller with no world information has, and it keeps a
// single-world fleet behaving exactly as it did.
func SharedTreeClaimsFor(world int) *TreeClaims {
	if world < 0 {
		world = 0
	}
	sharedMu.Lock()
	defer sharedMu.Unlock()
	c := sharedByWorld[world]
	if c == nil {
		c = NewTreeClaims()
		sharedByWorld[world] = c
	}
	return c
}

// A NIL *TreeClaims IS A USABLE "NO DE-CONFLICTION" REGISTRY, and every method below tolerates
// one. This is not defensive decoration; it was a real crash.
//
// PickTreeFor already treated a nil registry as "fall back to nearest-first", so nil is a
// legitimate value a caller can hold — and woodcutrunner's own kill switch produces exactly
// that. But Release did not tolerate it, so the first live A/B run of this change spent five
// minutes measuring a nil-pointer panic: internal/bot/runner.go's recover() swallowed it, every
// account logged an ordinary-looking "stopped", and the arm came back at 25 cohort xp against
// 3,624 for the build it was supposed to be imitating. The number looked like a spectacular
// result for the feature and was a crash in the switch that turns it off.
//
// The lesson kept next to the fix: an A/B arm that produces a dramatic number has to be checked
// against the real thing it claims to stand in for. Here that was a build of origin/main, and
// it is what caught this.

// live reports whether c is still standing at now. Caller holds the lock.
func (c *TreeClaims) live(cl treeClaim, now time.Time) bool {
	return now.Sub(cl.at) < c.ttl
}

// Claim takes one tree for `by`, and reports whether it now holds it.
//
// Free, expired, or already this account's (which re-stamps, so a holder that keeps calling
// keeps its tree) all succeed. A tree another account holds live fails, and that failure is the
// whole de-confliction: the caller must then pick a different one.
func (c *TreeClaims) Claim(at tile.Tile, by string, now time.Time) bool {
	if c == nil {
		return false
	}
	if by == "" {
		// No identity, no claim. Callers that cannot name themselves fall back to the
		// nearest-first behaviour this package had before, rather than claiming under a
		// shared empty name and locking each other out of the whole camp.
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.claimLocked(at, by, now)
}

func (c *TreeClaims) claimLocked(at tile.Tile, by string, now time.Time) bool {
	if c.held == nil {
		c.held = map[tile.Tile]treeClaim{}
	}
	held, ok := c.held[at]
	if ok && c.live(held, now) && held.by != by {
		return false
	}
	c.held[at] = treeClaim{by: by, at: now}
	return true
}

// ClaimFirstFree takes the first candidate, in the caller's own preference order, that is free
// at this instant — the whole read-and-take in ONE critical section.
//
// This is rule 3. A caller that asked "is this one free?" and then claimed it in a second call
// would leave a window between the two for another account to take it; there is no such window
// here. free is how many of cands were takeable when the decision was made, counted under the
// same lock so the number cannot disagree with the decision it is reported beside.
func (c *TreeClaims) ClaimFirstFree(cands []TreeCandidate, by string, now time.Time) (got TreeCandidate, free int, ok bool) {
	if c == nil || by == "" || len(cands) == 0 {
		return TreeCandidate{}, 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cand := range cands {
		held, exists := c.held[cand.At]
		if exists && c.live(held, now) && held.by != by {
			continue
		}
		free++
		if ok {
			continue
		}
		c.claimLocked(cand.At, by, now)
		// Whatever else this account was holding, it is not working on it any more.
		c.releaseOthersLocked(cand.At, by)
		got, ok = cand, true
	}
	return got, free, ok
}

// Release drops a claim, but only when `by` is the holder — a stray release from another
// account cannot free somebody else's tree.
func (c *TreeClaims) Release(at tile.Tile, by string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	held, ok := c.held[at]
	if !ok || held.by != by {
		return false
	}
	delete(c.held, at)
	return true
}

// ReleaseAll drops every tree `by` holds — what an account calls when it leaves the camp
// entirely (a bank trip, a random event, a stop). Cheaper and more certain than waiting out
// TreeClaimTTL, which is only the backstop for an account that cannot say anything at all.
func (c *TreeClaims) ReleaseAll(by string) int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for at, held := range c.held {
		if held.by == by {
			delete(c.held, at)
			n++
		}
	}
	return n
}

// releaseOthersLocked drops every tree `by` holds except keep, so one account never reserves
// two trees at once. Caller holds the lock.
func (c *TreeClaims) releaseOthersLocked(keep tile.Tile, by string) int {
	n := 0
	for at, held := range c.held {
		if held.by == by && at != keep {
			delete(c.held, at)
			n++
		}
	}
	return n
}

// HolderOf names who holds a tree right now, or "" when it is free.
func (c *TreeClaims) HolderOf(at tile.Tile, now time.Time) string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	held, ok := c.held[at]
	if !ok || !c.live(held, now) {
		return ""
	}
	return held.by
}

// Len is how many trees are claimed right now, for a log line or a test.
func (c *TreeClaims) Len(now time.Time) int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, held := range c.held {
		if c.live(held, now) {
			n++
		}
	}
	return n
}

// Sweep drops expired entries so the map cannot grow without bound over a long run. The TTL
// already makes an expired entry invisible to every reader, so this is hygiene rather than
// correctness; it is O(claims), which is at most one per account.
func (c *TreeClaims) Sweep(now time.Time) int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for at, held := range c.held {
		if !c.live(held, now) {
			delete(c.held, at)
			n++
		}
	}
	return n
}

// TreePick is what PickTreeFor decided, and why.
type TreePick struct {
	// Tree is the tree to chop.
	Tree TreeCandidate
	// Mine is true when this account now holds the claim on Tree. False means it is chopping
	// a tree somebody else has reserved, which is allowed and deliberate — see Shared.
	Mine bool
	// Shared is true when EVERY candidate in range was already claimed by another account, so
	// this pick is a deliberate double-up rather than a free choice. The caller should log it
	// at most occasionally: on an oversubscribed camp it is the normal state, not a fault.
	Shared bool
	// Free is how many candidates were unclaimed when the choice was made, and Claimed how
	// many were held by somebody else. For a log line; a camp whose Free is persistently 0 is
	// telling the operator it has more accounts in it than trees.
	Free, Claimed int
}

// PickTreeFor chooses which tree this account should chop, de-conflicting against every other
// account in this process.
//
// Preference order, and each step is one of the rules in this file's header:
//
//  1. Drop anything TreeBlocks excludes. Those are trees chopping would be actively WRONG at
//     (an Ent that breaks the axe, a tree no reachable tile can interact with, a tree that gave
//     nothing for fifteen seconds). This is the one filter with no fallback — PickTree's own
//     doc comment explains why, and nothing here weakens it.
//  2. Order what is left nearest first, breaking ties on the tile itself so that two accounts
//     looking at the same camp in the same instant produce the SAME order rather than two
//     arbitrary ones. Without that, "first free" would not be a shared rule at all.
//  3. Take the first one that is free, atomically (rule 3).
//  4. If none is free, chop anyway (rule 2), spreading the spare accounts across the candidates
//     by a hash of the account's own name instead of stacking them all on the nearest (rule 4).
//
// claims may be nil and me may be empty — a single-account harness, or an account whose own
// name has not arrived from the server yet. Either falls straight through to PickTree's
// nearest-first behaviour, so nothing that predates this is changed.
func PickTreeFor(candidates []TreeCandidate, blocks *TreeBlocks, claims *TreeClaims, me string, now time.Time) (TreePick, bool) {
	open := make([]TreeCandidate, 0, len(candidates))
	for _, c := range candidates {
		if blocks != nil && blocks.Blocked(c.At, now) {
			continue
		}
		open = append(open, c)
	}
	if len(open) == 0 {
		return TreePick{}, false
	}
	sortCandidates(open)
	if claims == nil || me == "" {
		return TreePick{Tree: open[0], Free: len(open)}, true
	}
	if got, free, ok := claims.ClaimFirstFree(open, me, now); ok {
		return TreePick{Tree: got, Mine: true, Free: free, Claimed: len(open) - free}, true
	}
	// Rule 2 and rule 4: everything is spoken for, and standing still is worse than doubling
	// up. Spread rather than stack.
	return TreePick{
		Tree:    spreadPick(me, open),
		Shared:  true,
		Claimed: len(open),
	}, true
}

// sortCandidates orders nearest first with a total, account-independent tiebreak, so every
// account ranks the same camp identically. Insertion sort: a camp is 3 to 56 trees and this
// runs once per chop, so the constant factor beats pulling in sort for a closure per call.
func sortCandidates(cands []TreeCandidate) {
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0 && candBefore(cands[j], cands[j-1]); j-- {
			cands[j], cands[j-1] = cands[j-1], cands[j]
		}
	}
}

func candBefore(a, b TreeCandidate) bool {
	if a.Distance != b.Distance {
		return a.Distance < b.Distance
	}
	if a.At.X != b.At.X {
		return a.At.X < b.At.X
	}
	if a.At.Z != b.At.Z {
		return a.At.Z < b.At.Z
	}
	return a.At.Level < b.At.Level
}

// spreadPick chooses a tree for an account that could not claim one, deterministically from its
// own name and the TREE'S OWN TILE.
//
// Highest-random-weight (rendezvous) selection, not `hash(name) % len(candidates)`, and the
// difference is the point. A modulo of the candidate COUNT re-shuffles every account's answer
// whenever the count changes — and at a normal-tree camp the count changes constantly, because
// each tree that falls leaves the set and each respawn rejoins it. Every spare account would
// then hop to a different tree every time any tree anywhere in the camp fell, which is walking,
// which is the thing being minimised. Weighting each candidate by hash(name, tile) and taking
// the highest instead means an account's preference for a given tree does not depend on which
// OTHER trees are in the set: when a tree falls, only the accounts that were pointed at it move.
//
// The hash is FNV-1a, which is not good in any cryptographic sense and does not need to be. It
// needs to scatter ninety identities across a dozen tiles.
func spreadPick(me string, cands []TreeCandidate) TreeCandidate {
	best, bestWeight := cands[0], uint64(0)
	for i, c := range cands {
		w := spreadWeight(me, c.At)
		if i == 0 || w > bestWeight {
			best, bestWeight = c, w
		}
	}
	return best
}

func spreadWeight(me string, at tile.Tile) uint64 {
	const (
		offset = 14695981039346656037
		prime  = 1099511628211
	)
	h := uint64(offset)
	mix := func(b byte) {
		h ^= uint64(b)
		h *= prime
	}
	for i := 0; i < len(me); i++ {
		mix(me[i])
	}
	for _, v := range [3]int{at.X, at.Z, at.Level} {
		u := uint64(uint32(v))
		mix(byte(u))
		mix(byte(u >> 8))
		mix(byte(u >> 16))
		mix(byte(u >> 24))
	}
	return h
}
