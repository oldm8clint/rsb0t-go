package fleetplan

// Never attack a Stage 3/5 target somebody else is already fighting ("scarabprod", 2026-09-23).
//
// THE CONTENT RULE (skill_combat/scripts/player/player_combat.rs2, player_in_combat_check): outside
// a multiway area, attacking an NPC that has been in combat in the last 8 ticks with ANOTHER player
// is refused with "Someone else is fighting that." -- and nothing in Stage 3 or 5 read that line,
// or skipped a busy target before clicking it. With ten Cows in the Ardougne pen and a cohort of
// dozens, every account but the first ten either sat on combatrunner.WaitForKill's full sixty
// seconds for a fight it was never given, or credited itself with another account's kill.
//
// Three layers, cheapest first:
//
//   - TargetContested: what the client can see -- the NPC faces another player, or is in combat
//     and not with this account (fighterplan.TakenByAnother, AJ's proven rule, reused unchanged).
//   - TargetBoard claims: the swarm's own accounts live in ONE process, so two of them picking the
//     same free Cow in the same tick -- the race the client-side test cannot see -- is settled
//     here before either clicks.
//   - TargetBoard skips: a target the server refused with ContestedLine is left alone for a while
//     by the account that was refused.
//
// Pure: every time is passed in.

import (
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/scripts/fighterplan"
)

// ContestedLine is content's refusal, verbatim.
const ContestedLine = "Someone else is fighting that."

// TargetClaimTTL bounds a claim, so an account that dies or stops mid-fight cannot hold a Cow for
// ever. A Cow's whole fight at these levels is well under this.
const TargetClaimTTL = 60 * time.Second

// TargetSkipFor is how long a refused target is left alone by the account the server refused.
const TargetSkipFor = 30 * time.Second

// TargetContested reports whether an NPC belongs to someone else's fight as the client sees it.
// isOurs is the target this account is itself fighting, which is exempt because its face target
// flickers between attacks.
func TargetContested(isOurs, inCombat, targetsMe, targetsOther bool) bool {
	return fighterplan.TakenByAnother(isOurs, inCombat, targetsMe, targetsOther)
}

// SaidContested reports whether a stretch of game chat carries content's refusal.
func SaidContested(chat string) bool { return strings.Contains(chat, ContestedLine) }

type targetKey struct{ world, index int }

type targetClaim struct {
	by    string
	until time.Time
}

// TargetBoard is the swarm's in-process claim and skip book for combat targets. Safe for
// concurrent use. NPC indices are the server's own ids, so they agree across every account on the
// same world; the world is part of the key because a second world's identical content numbers
// its NPCs the same way.
type TargetBoard struct {
	mu     sync.Mutex
	claims map[targetKey]targetClaim
	skips  map[string]map[targetKey]time.Time
}

// NewTargetBoard returns an empty board.
func NewTargetBoard() *TargetBoard {
	return &TargetBoard{claims: map[targetKey]targetClaim{}, skips: map[string]map[targetKey]time.Time{}}
}

func AcctKey(a string) string { return strings.ToLower(strings.TrimSpace(a)) }

// ClaimedByOther reports a live claim on the target by any account but `account`.
func (b *TargetBoard) ClaimedByOther(world, index int, account string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.claims[targetKey{world, index}]
	return ok && now.Before(c.until) && c.by != AcctKey(account)
}

// HeldBy reports a live claim on the target by `account` itself: its own current fight, which the
// client-side test must exempt because a target's face flickers between attacks.
func (b *TargetBoard) HeldBy(world, index int, account string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.claims[targetKey{world, index}]
	return ok && now.Before(c.until) && c.by == AcctKey(account)
}

// Claim takes the target for `account` until now+ttl, and reports false when another account
// already holds a live claim on it.
func (b *TargetBoard) Claim(world, index int, account string, now time.Time, ttl time.Duration) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	k, me := targetKey{world, index}, AcctKey(account)
	if c, ok := b.claims[k]; ok && now.Before(c.until) && c.by != me {
		return false
	}
	b.claims[k] = targetClaim{by: me, until: now.Add(ttl)}
	// Prune expired claims while the lock is held, so the map is bounded by live fights.
	for kk, c := range b.claims {
		if !now.Before(c.until) {
			delete(b.claims, kk)
		}
	}
	return true
}

// Release drops `account`'s claim on the target, if it holds one.
func (b *TargetBoard) Release(world, index int, account string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := targetKey{world, index}
	if c, ok := b.claims[k]; ok && c.by == AcctKey(account) {
		delete(b.claims, k)
	}
}

// Skip leaves the target alone for `account` until `until`.
func (b *TargetBoard) Skip(world, index int, account string, until time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	me := AcctKey(account)
	if b.skips[me] == nil {
		b.skips[me] = map[targetKey]time.Time{}
	}
	b.skips[me][targetKey{world, index}] = until
}

// Skipped reports a live skip of the target for `account`.
func (b *TargetBoard) Skipped(world, index int, account string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	until, ok := b.skips[AcctKey(account)][targetKey{world, index}]
	return ok && now.Before(until)
}
