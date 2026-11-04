package woodcutplan

// Choosing WHICH tree to chop, when "the nearest one" is the wrong answer.
//
// The ordinary case is nearest-first and always was. This file exists for the case the fleet
// owner described: an Ent spawns at the tree being cut, and carrying on chopping BREAKS THE AXE
// — which then has to be carried to Bob's in Lumbridge to be repaired, a trip that costs far
// more than the logs the tree would have given. So a tree with an Ent at it has to be skipped,
// and skipped for as long as the Ent is standing there, not just for the one click that noticed
// it.
//
// Why the exclusion EXPIRES rather than being cleared when the Ent leaves: an account that has
// walked away from a tree cannot see whether the NPC beside it is still there, and a blocklist
// that is only ever cleared by an observation it cannot make is a blocklist that grows until
// the camp has no trees left in it. A timed exclusion self-heals with no observation at all,
// and re-blocks immediately if the Ent is still there when the tree comes back up for
// selection.
//
// Kept pure, like the rest of this package: the caller decides what "has an Ent at it" means
// from live NPC state and calls Block; nothing here reads a session.

import (
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// EntBlockFor is how long a tree stays excluded after an Ent is seen at it.
//
// Chosen to be comfortably longer than an Ent's own stay rather than tuned finely: the cost of
// blocking a tree slightly too long is one extra step to a neighbouring tree, and at the
// Draynor camp there are 56 dead trees inside the leash, so that cost is close to zero. The cost
// of un-blocking too early is a broken axe and a cross-map repair trip. The asymmetry is the
// whole argument, and it is why this is deliberately generous.
const EntBlockFor = 3 * time.Minute

// TreeCandidate is one tree the caller is choosing between.
type TreeCandidate struct {
	At tile.Tile
	// Distance is how far the account is from it; PickTree prefers the smallest.
	Distance int
}

// TreeBlocks is a set of tree tiles not to chop, each with an expiry. The zero value is an
// empty, usable set.
//
// Keyed by TILE rather than by loc id on purpose: a felled tree respawns as a different loc at
// the same place, so an id-keyed block would be silently dropped the moment the tree changed
// state — which is exactly when an Ent is most likely to still be standing there.
type TreeBlocks struct {
	until map[tile.Tile]time.Time
	// why is the reason each live entry was written, for the log line and for the caller's
	// decision about what to do when every candidate is excluded. Kept beside until rather than
	// folded into it so the zero value and every existing method stay exactly as they were.
	why map[tile.Tile]BlockReason
}

// BlockReason is WHY a tree is excluded.
//
// It exists because one exclusion set serves three different risks, and the account's answer to
// "every tree in reach is excluded" depends on which one it is. Waiting out an Ent is right —
// chopping it breaks the axe. Waiting out a tree nobody can stand at is not, when the reason nobody
// can stand at it is that the ACCOUNT is on the wrong side of a wall.
//
// MEASURED on the live fleet, 2026-09-23, build 3dfb543a: the only line the account could log was
// "every tree within reach has an Ent at it", and it logged it for every kind. Acct212 logged it
// 658 times over 21.9 minutes inside the walled building at 2730,3478 with no Ent anywhere near
// it; across the other 80 WCFletch accounts it was 5,921 lines in 69 minutes, and a lab crowd run
// attributed 343 of 353 of those exclusions to a player standing on the stand tile and none to an
// Ent. A log that names the wrong cause is how that went unnoticed.
type BlockReason uint8

const (
	// BlockOther is a block written without a reason (BlockFor, kept for its existing callers).
	BlockOther BlockReason = iota
	// BlockEnt: an Ent was seen at the tree. EntBlockFor, and the one reason chopping anyway would
	// break the axe.
	BlockEnt
	// BlockUnreachable: no tile the engine accepts a chop from could be reached.
	BlockUnreachable
	// BlockStall: the swing was sent and the tree gave nothing for the runner's stall timeout.
	BlockStall
)

// String is the reason as the log line says it.
func (r BlockReason) String() string {
	switch r {
	case BlockEnt:
		return "Ent"
	case BlockUnreachable:
		return "unreachable"
	case BlockStall:
		return "gave nothing"
	default:
		return "other"
	}
}

// UnreachableBlockFor is how long a tree stays excluded after no tile the engine would accept a
// chop from could be reached.
//
// Much shorter than EntBlockFor, because the two exclusions answer different risks. An Ent
// exclusion protects the axe and errs long. This one only says "the tiles around this tree were
// blocked a moment ago", which at a dense camp is a passing state: a neighbouring tree being
// felled opens a stand tile, and another account moving off one does the same. Long enough that
// the picker moves on instead of re-choosing the same tree every pass, short enough that a whole
// pocket of the camp is not written off for minutes.
const UnreachableBlockFor = 30 * time.Second

// Block excludes a tree until now.Add(EntBlockFor), because an Ent was seen at it.
func (b *TreeBlocks) Block(at tile.Tile, now time.Time) {
	b.BlockBecause(at, now, EntBlockFor, BlockEnt)
}

// BlockFor excludes a tree for a caller-chosen duration, with no recorded reason. A longer
// exclusion already in place is never shortened: an Ent block must not be weakened by an
// unreachable block landing on the same tile a second later.
func (b *TreeBlocks) BlockFor(at tile.Tile, now time.Time, d time.Duration) {
	b.BlockBecause(at, now, d, BlockOther)
}

// BlockBecause is BlockFor with the reason recorded. The never-shorten rule applies to the reason
// too: an exclusion that is kept keeps its own reason, so an Ent block that an unreachable block
// lands on a second later still reads as an Ent.
func (b *TreeBlocks) BlockBecause(at tile.Tile, now time.Time, d time.Duration, why BlockReason) {
	if b.until == nil {
		b.until = map[tile.Tile]time.Time{}
	}
	if b.why == nil {
		b.why = map[tile.Tile]BlockReason{}
	}
	until := now.Add(d)
	if existing, ok := b.until[at]; ok && existing.After(until) {
		return
	}
	b.until[at] = until
	b.why[at] = why
}

// Why reports the reason a tree is excluded and when the exclusion lifts. ok is false when it is
// not excluded now.
func (b *TreeBlocks) Why(at tile.Tile, now time.Time) (why BlockReason, until time.Time, ok bool) {
	if b == nil || !b.Blocked(at, now) {
		return BlockOther, time.Time{}, false
	}
	return b.why[at], b.until[at], true
}

// BlockTally counts the exclusions among a given set of trees, by reason.
type BlockTally struct {
	Ent, Unreachable, Stall, Other int
	// Lifts is when the FIRST of them expires — the soonest one of these trees can be picked
	// again. Zero when none is excluded.
	Lifts time.Time
}

// Total is how many of the trees were excluded.
func (t BlockTally) Total() int { return t.Ent + t.Unreachable + t.Stall + t.Other }

// String names every non-zero kind, for the log line: "2 Ent, 3 unreachable, 1 gave nothing".
func (t BlockTally) String() string {
	parts := make([]string, 0, 4)
	for _, p := range []struct {
		n  int
		as BlockReason
	}{{t.Ent, BlockEnt}, {t.Unreachable, BlockUnreachable}, {t.Stall, BlockStall}, {t.Other, BlockOther}} {
		if p.n > 0 {
			parts = append(parts, itoa(p.n)+" "+p.as.String())
		}
	}
	if len(parts) == 0 {
		return "none excluded"
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += ", " + p
	}
	return out
}

// Tally counts which of trees are excluded now, and why.
//
// ONLY THE TREES PASSED, and that is the point. Len counts every live entry in the set, including
// a tree the account blocked on the far side of the camp a minute ago, so "every tree within reach
// has an Ent at it (N excluded)" used to print a number that was not about the trees in reach at
// all — and it printed even when there were no trees in reach, mislabelling a plain "no tree in
// sight" as an exclusion. A caller that asks about the candidates it actually had gets a count that
// means something.
func (b *TreeBlocks) Tally(trees []tile.Tile, now time.Time) BlockTally {
	var t BlockTally
	if b == nil {
		return t
	}
	for _, at := range trees {
		why, until, ok := b.Why(at, now)
		if !ok {
			continue
		}
		switch why {
		case BlockEnt:
			t.Ent++
		case BlockUnreachable:
			t.Unreachable++
		case BlockStall:
			t.Stall++
		default:
			t.Other++
		}
		if t.Lifts.IsZero() || until.Before(t.Lifts) {
			t.Lifts = until
		}
	}
	return t
}

// Blocked reports whether a tree is currently excluded.
func (b *TreeBlocks) Blocked(at tile.Tile, now time.Time) bool {
	if b.until == nil {
		return false
	}
	until, ok := b.until[at]
	return ok && now.Before(until)
}

// Sweep drops expired entries so the map cannot grow without bound over a long run. Safe to
// call on every pass; it is O(blocked trees), which is a handful at most.
func (b *TreeBlocks) Sweep(now time.Time) {
	for at, until := range b.until {
		if !now.Before(until) {
			delete(b.until, at)
			delete(b.why, at)
		}
	}
}

// Len is how many trees are currently excluded, for a log line.
func (b *TreeBlocks) Len(now time.Time) int {
	n := 0
	for at := range b.until {
		if b.Blocked(at, now) {
			n++
		}
	}
	return n
}

// PickTree chooses the nearest candidate that is not currently blocked.
//
// Returns ok=false when every candidate is blocked, which the caller must treat as "wait and
// look again", NOT as "chop the nearest one anyway" — chopping a blocked tree is the exact
// thing that breaks the axe. A camp with every tree blocked is a temporary state that expires
// on its own.
func PickTree(candidates []TreeCandidate, blocks *TreeBlocks, now time.Time) (TreeCandidate, bool) {
	var best TreeCandidate
	found := false
	for _, c := range candidates {
		if blocks != nil && blocks.Blocked(c.At, now) {
			continue
		}
		if !found || c.Distance < best.Distance {
			best, found = c, true
		}
	}
	return best, found
}
