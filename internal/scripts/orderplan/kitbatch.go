package orderplan

// KIT BATCHING (a user request: "the mule is overloaded"). Live, the mule made one bank visit per kit: open
// the bank, draw, close, trade, and only then the next kit's visit. With several kits queued it now draws up to
// KitBatchMax of them in ONE visit, as long as the pack holds them all, trades them out one after the other and
// banks once at the end. Kits are mostly noted stacks (food always crosses noted, runes and coins stack), so a
// kit is usually one to six pack slots and three of them fit with room to spare.
//
// This file is the pure half: which of the queued kits, already decided against the bank (DecideKit), go into the
// visit. Each kit is judged in queue order and the first that does not fit ends the batch, so a big kit at the
// head is never overtaken by small ones behind it. The first kit is always taken: alone it is exactly the
// single-kit visit the mule made before.
//
// THE SLOTS A KIT TAKES IN THE MULE'S PACK, which is not the same as the donor's trade slots: two kits that both
// carry noted sharks share ONE pack stack of shark notes, and so do two kits of fire runes. An unnoted
// non-stackable line takes a slot a unit. A kit that also carries the donor's hand-over back (the two-way kit
// trade, KitRequest.Give) reserves the slots that hand-over will take in the mule's pack, since what is received
// stays there until the visit's one bank at the end.

import "strings"

// KitBatchMax is the most kits one bank visit draws (a user request: "draw up to 3 kits in ONE bank visit").
const KitBatchMax = 3

// PackSlots is a pack's size.
const PackSlots = 28

// KitBatch sizes one bank visit's kits against the mule's pack. The zero value is not usable; see NewKitBatch.
type KitBatch struct {
	capacity int
	maxKits  int
	slots    int
	kits     int
	shared   map[string]bool
}

// NewKitBatch is an empty visit of capacity pack slots and at most maxKits kits (both clamped to at least one).
func NewKitBatch(capacity, maxKits int) *KitBatch {
	if capacity < 1 {
		capacity = PackSlots
	}
	if maxKits < 1 {
		maxKits = 1
	}
	return &KitBatch{capacity: capacity, maxKits: maxKits, shared: map[string]bool{}}
}

// Slots is the pack slots the kits taken so far need, hand-overs included.
func (b *KitBatch) Slots() int { return b.slots }

// Kits is how many kits have been taken.
func (b *KitBatch) Kits() int { return b.kits }

// stackKey names the one pack stack a noted or stackable line sits in, "" for a line that takes a slot a unit.
func stackKey(l KitLineDecision, facts func(item string) (KitFacts, bool)) string {
	f, _ := facts(l.Item)
	switch {
	case l.Noted:
		return "note:" + strings.ToLower(strings.TrimSpace(l.Item))
	case f.Stackable:
		return "item:" + strings.ToLower(strings.TrimSpace(l.Item))
	}
	return ""
}

// PackSlotsFor is how many NEW pack slots lines would take on top of the stacks already named in shared (which
// it does not change): a noted or stackable line one slot unless its stack is already there, an unnoted
// non-stackable line a slot a unit.
func PackSlotsFor(lines []KitLineDecision, facts func(item string) (KitFacts, bool), shared map[string]bool) int {
	if facts == nil {
		facts = func(string) (KitFacts, bool) { return KitFacts{}, false }
	}
	seen := map[string]bool{}
	n := 0
	for _, l := range lines {
		if l.Qty <= 0 {
			continue
		}
		key := stackKey(l, facts)
		if key == "" {
			n += l.Qty
			continue
		}
		if shared[key] || seen[key] {
			continue
		}
		seen[key] = true
		n++
	}
	return n
}

// Add takes the kit into the visit when it fits, and reports whether it did; a kit that does not fit changes
// nothing. giveSlots is the pack slots the donor's hand-over back will take (0: none). The first kit is always
// taken (DecideKit already sized it to one trade).
func (b *KitBatch) Add(lines []KitLineDecision, facts func(item string) (KitFacts, bool), giveSlots int) bool {
	if facts == nil {
		facts = func(string) (KitFacts, bool) { return KitFacts{}, false }
	}
	if b.kits >= b.maxKits {
		return false
	}
	need := PackSlotsFor(lines, facts, b.shared) + max(0, giveSlots)
	if b.kits > 0 && b.slots+need > b.capacity {
		return false
	}
	for _, l := range lines {
		if l.Qty > 0 {
			if key := stackKey(l, facts); key != "" {
				b.shared[key] = true
			}
		}
	}
	b.slots += need
	b.kits++
	return true
}

// GiveRoomOK reports whether the mule, holding free empty pack slots and about to offer a kit whose offers empty
// leaving more slots, has room for a hand-over back of giveSlots: the trade screen refuses a trade the receiving
// pack cannot hold ("You don't have enough inventory space"), and offering moves the kit out of the pack first.
func GiveRoomOK(free, leaving, giveSlots int) bool {
	return giveSlots <= 0 || free+leaving >= giveSlots
}

// Clone is a copy of the ledger that can be written without touching l: a bank visit that decides several kits
// records each one in a clone, so the next kit is judged against the window caps as if the earlier ones had
// already crossed. The real ledger is written only on a confirmed delivery.
func (l *Ledger) Clone() *Ledger {
	if l == nil {
		return nil
	}
	return &Ledger{window: l.window, grants: append([]grant(nil), l.grants...)}
}

// ---- the mule's side of a two-way kit trade ----
//
// A kit order may carry Give: what the donor will hand the mule in the SAME trade (mulebus.Message.Give). The mule
// offers exactly the kit it drew, and takes in nothing but that agreed hand-over. JudgeGive is the whole rule for
// the donor's side of the screen, run on every read of it, offer screen and confirm screen alike.

// GiveState is how the donor's side of the screen stands against what was agreed.
type GiveState int

const (
	// GivePartial: nothing, or part, of the agreed hand-over shows, and nothing else.
	GivePartial GiveState = iota
	// GiveComplete: the whole agreed hand-over shows, and nothing else.
	GiveComplete
	// GiveBad: something shows that was not agreed, or more of it than was agreed. The trade is declined.
	GiveBad
)

// giveKey names a stack the way a hand-over is agreed: by lower-cased name, a bank note apart from the item.
func giveKey(l KitLine) string {
	k := strings.ToLower(strings.TrimSpace(l.Item))
	if l.Noted {
		k += "#noted"
	}
	return k
}

// JudgeGive compares the donor's side of the screen (theirs, one row per stack) with the hand-over agreed. A
// row with no count is ignored. With nothing agreed, anything at all on their side is bad.
func JudgeGive(agreed, theirs []KitLine) (GiveState, string) {
	want := map[string]int{}
	for _, l := range agreed {
		if l.Qty > 0 {
			want[giveKey(l)] += l.Qty
		}
	}
	have := map[string]int{}
	for _, l := range theirs {
		if l.Qty > 0 {
			have[giveKey(l)] += l.Qty
		}
	}
	complete := true
	for key, n := range have {
		if n > want[key] {
			if len(want) == 0 {
				return GiveBad, "you offered me something on a hand-out"
			}
			return GiveBad, "you offered me more than the hand-over we agreed"
		}
	}
	for key, n := range want {
		if have[key] < n {
			complete = false
		}
	}
	if complete {
		return GiveComplete, ""
	}
	return GivePartial, ""
}

// GiveSlotsFor is how many pack slots a hand-over of lines takes in the mule's pack, as the donor reckoned it
// (mulebus.Message.GiveSlots), checked here against the lines themselves: a bank note or a stackable one slot, any
// other item a slot a unit. facts says which items stack; the donor's own count wins only when it is larger.
func GiveSlotsFor(lines []KitLine, claimed int, facts func(item string) (KitFacts, bool)) int {
	if facts == nil {
		facts = func(string) (KitFacts, bool) { return KitFacts{}, false }
	}
	n := 0
	for _, l := range lines {
		if l.Qty <= 0 {
			continue
		}
		f, _ := facts(l.Item)
		if l.Noted || f.Stackable {
			n++
		} else {
			n += l.Qty
		}
	}
	return max(n, min(claimed, PackSlots))
}
