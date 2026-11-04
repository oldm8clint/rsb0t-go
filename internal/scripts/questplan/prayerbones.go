package questplan

// Bones for the Prayer rungs (questmods/cows.go): which bones a rung buries first, and where it gets them.
//
// WHY THIS EXISTS (16 Oct 2026). The Prayer rungs (prayer31, prayerkq, prayer43) killed Lumbridge cows and buried
// what they dropped: 4.5 xp a bone. Measured over 26 accounts and 1,412 rung-minutes of the 16 Oct log (05:23-07:56)
// they gained 175 xp per rung-hour, about 58 xp a 20-minute slot, so Prayer 31 (14,833 xp) was eighty-five hours of
// rung time and every slot ended in a timeout. Meanwhile the mule's bank held 17,154 Bones and 1,488 Big bones
// (99,000 xp, three times everything the eleven accounts below 31 still needed) and the rung never looked at them:
// only at the account's own bank, which 341 of the 359 banks hold no bones in.
//
// The item rule (provision.go) is the order: the pack, the own bank, the mule, and only then the field work, which
// here is the cows. This file is the pure half: what the bones are worth, how many to take, and the next move.
// questmods/prayerbones.go carries the moves out.
//
// A bone is worth nothing to the level but its experience, so the best kind goes first: a Big bone is 15 xp, a
// Dragon bone 72, a Bone 4.5. The mule serves the bones as bank notes above a pack (orderplan.TravelsAsNotes), the
// kit bundle un-notes what the pack has room for and banks the rest at the post, so one ask of a thousand bones is
// one trade; the rung then draws a pack at a time from that bank, where it stands, and buries.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/levels"
)

// The two custom steps of a Prayer rung. The bones step (CustomPrayerBones, the "cows-prayer" the rung always had) does
// the item rule and reports failure when it found no bone to bury; the engine remembers that failure for the rest of the run
// (Snapshot.Failed), and CowsRungStep then sends the run to the cows (CustomPrayerCows) as the fallback it is.
const (
	CustomPrayerBones = "cows-prayer"
	CustomPrayerCows  = "cows-prayer-field"
)

// CowsRungStep is a cows rung's decision: done at the level, else the rung's custom step. A Prayer rung asks for
// its bones first and kills cows only once the bones step has failed this run; the other skills' rungs (Hitpoints
// for the Waterfall gate, Attack) kill cows as they always did.
func CowsRungStep(s Snapshot, skill string, level int) Step {
	if s.Level(skill) >= level {
		return Step{Kind: StepDone}
	}
	if !strings.EqualFold(skill, "prayer") {
		return Step{Kind: StepCustom, Custom: "cows-" + skill, Reason: fmt.Sprintf("cows and bones to %s %d", skill, level)}
	}
	if s.Failed[CustomPrayerBones] {
		return Step{Kind: StepCustom, Custom: CustomPrayerCows,
			Reason: fmt.Sprintf("cows and their bones to %s %d (no bone from the pack, the bank or the mule)", skill, level)}
	}
	return Step{Kind: StepCustom, Custom: CustomPrayerBones,
		Reason: fmt.Sprintf("bones from the pack, the bank and the mule to %s %d", skill, level)}
}

// The bones a Prayer rung buries, by object id (cachecfg: 526 and its note 527, 532/533, 536/537).
const (
	ObjBones       = 526
	ObjBigBones    = 532
	ObjDragonBones = 536
)

// BoneKind is one kind of bone a Prayer rung asks for, draws from the bank and buries.
type BoneKind struct {
	ID int
	// Note is the bank note of the item: a trade carries a thousand of them in one slot.
	Note int
	Name string
	// XP10 is what one burial gives in TENTHS of an experience point (bones.obj bone_exp: 45 is 4.5 xp).
	XP10 int
}

// BoneKinds are the bones in the order they are taken, the most experience a slot first. Bat, Burnt and Wolf bones
// bury for 4.5 xp too (bones.obj), but the fleet's banks held 25 Bat bones and no others on 16 Oct 2026, so they are
// not asked for. Dragon bones are members' items.
var BoneKinds = []BoneKind{
	{ID: ObjDragonBones, Note: 537, Name: "Dragon bones", XP10: 720},
	{ID: ObjBigBones, Note: 533, Name: "Big bones", XP10: 150},
	{ID: ObjBones, Note: 527, Name: "Bones", XP10: 45},
}

// BoneKindOf is the kind with object id id (the item itself, never its note).
func BoneKindOf(id int) (BoneKind, bool) {
	for _, k := range BoneKinds {
		if k.ID == id {
			return k, true
		}
	}
	return BoneKind{}, false
}

// BonesFor is how many bones of the kind carry xp experience, rounded up.
func (k BoneKind) BonesFor(xp int) int {
	if xp <= 0 || k.XP10 <= 0 {
		return 0
	}
	return (xp*10 + k.XP10 - 1) / k.XP10
}

// BoneXP10 is the experience, in tenths, that burying every bone in counts (by object id) gives.
func BoneXP10(counts map[int]int) int {
	total := 0
	for _, k := range BoneKinds {
		if n := counts[k.ID]; n > 0 {
			total += n * k.XP10
		}
	}
	return total
}

// BoneCount is how many bones counts holds.
func BoneCount(counts map[int]int) int {
	total := 0
	for _, k := range BoneKinds {
		if n := counts[k.ID]; n > 0 {
			total += n
		}
	}
	return total
}

// DescribeBones is the log form of counts' bones ("12 Big bones, 40 Bones"), "no bones" for none.
func DescribeBones(counts map[int]int) string {
	var parts []string
	for _, k := range BoneKinds {
		if n := counts[k.ID]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, k.Name))
		}
	}
	if len(parts) == 0 {
		return "no bones"
	}
	return strings.Join(parts, ", ")
}

// PrayerNeed is the experience still to gain from xp to reach level; 0 at or above it.
func PrayerNeed(xp, level int) int {
	if n := levels.XPAtLevel(level) - xp; n > 0 {
		return n
	}
	return 0
}

// PrayerRungAsksMule reports whether the Prayer rung id may ask the mule for bones. The mule's bones are one
// finite stock (about 99,000 xp on 16 Oct 2026) and the Queen's gate is what the fleet needs them for: prayerkq,
// the level kqPrayer holds at her, and prayer31, Scorpion Catcher's gate. prayer43 (Horror from the Deep, about 50,000 xp
// an account, and 170 accounts sit at 38 or 39) would take every bone in the bank before one more account reached 37,
// so it draws its own bank and kills cows, as it always did.
func PrayerRungAsksMule(id string) bool { return id == "prayer31" || id == KQPrayerRungID }

// What a rung asks of the mule, and when it stops asking.
const (
	// BoneAskMax is the most of one kind one ask takes: the mule's ceiling per order for an item its catalogue does
	// not name (orderplan.OpenMaxPerOrder, 1000; anyMaxPerOrder in the live roster).
	BoneAskMax = 1000
	// MaxBoneAsks is the most asks one run of a rung makes, whatever each brought: three thousand bones a kind is
	// the whole of any account's need, and an ask is a walk to the post and a place in the mule's line.
	MaxBoneAsks = 3
	// MinBoneAskXP is the least a mule ask is worth (about six bones); under it the cows are no slower than a
	// trip to the post.
	MinBoneAskXP = 25
	// BoneAskWindow is the least time a run must have left to ask the mule. The 35 quest kit asks of 16 Oct (05:23-09:01,
	// the walk to the post, the wait in the mule's line and the trade) took 3 minutes at the median and 6 at the 90th
	// percentile; 532 kit asks of any kind took 105 seconds from the order to the trade.
	BoneAskWindow = 8 * time.Minute
	// CowsWindow is the least time a run must have left to walk to the cows: the walk alone was 4-5 minutes.
	CowsWindow = 6 * time.Minute
	// BoneMinLeft is the least time left to start any move: a pack of bones is half a minute of burying.
	BoneMinLeft = 45 * time.Second
	// PackBones is how many bones one bank trip draws: a pack less the slot the coins keep.
	PackBones = 27
	// BankHintMaxAge is how old an earlier bank reading may be and still tell a run whether a trip to the bank
	// is worth it. Older is as good as unread. A reading with no bones in it is never refreshed by a trip the
	// account has no other reason to make (the mule's post is one it makes), so this bounds how long bones that
	// reached the bank behind the rung's back (the trainer banks what it does not keep) stay unburied.
	BankHintMaxAge = 3 * time.Hour
)

// takeBones is the bones, best kind first, that cover needXP out of avail (by object id): at most perKind of one
// kind and, when room >= 0, at most room bones in all. The last kind may overshoot the need by less than one bone.
func takeBones(needXP int, avail map[int]int, room, perKind int) []ItemCount {
	left := needXP * 10
	var out []ItemCount
	for _, k := range BoneKinds {
		if left <= 0 || room == 0 {
			break
		}
		n := min(avail[k.ID], (left+k.XP10-1)/k.XP10, perKind)
		if room > 0 {
			n = min(n, room)
		}
		if n <= 0 {
			continue
		}
		out = append(out, ItemCount{ID: k.ID, Name: k.Name, Count: n})
		left -= n * k.XP10
		if room > 0 {
			room -= n
		}
	}
	return out
}

// PlanBonesAsk is the one ask of the mule that covers need experience out of what it publishes (stock by object
// id): the best kind it holds first, a thousand of a kind at most. Nil when the ask would be worth less than
// MinBoneAskXP. A noted line takes one slot of the trade, so the number of bones is no limit.
func PlanBonesAsk(need int, stock map[int]int) []ItemCount {
	ask := takeBones(need, stock, -1, BoneAskMax)
	if BoneXP10(countsOf(ask)) < MinBoneAskXP*10 {
		return nil
	}
	return ask
}

// PlanBoneDraws is the bones one bank trip draws: the best kind the bank holds first, at most room of them (the
// pack's free slots, PackBones at the most), enough to cover need.
func PlanBoneDraws(need int, bank map[int]int, room int) []ItemCount {
	return takeBones(need, bank, min(room, PackBones), PackBones)
}

func countsOf(lines []ItemCount) map[int]int {
	out := make(map[int]int, len(lines))
	for _, l := range lines {
		out[l.ID] += l.Count
	}
	return out
}

// BankReading is what a run knows of the account's own bank.
type BankReading int

const (
	// BankUnread: never read in this process. The item rule's own bank comes before the mule, so it is read once.
	BankUnread BankReading = iota
	// BankHint: read in an earlier run. Its bones decide whether a trip to a bank is worth it; a hint without
	// bones is not walked to, because the account stands at the mule's own bank when it asks (the kit bundle
	// banks there) and reads the bank live then.
	BankHint
	// BankFresh: read in this run.
	BankFresh
	// BankDirty: the mule's bones were handed over since the last read, so they are in the bank.
	BankDirty
)

// BonesState is everything NextBonesMove reads.
type BonesState struct {
	// Need is the experience still to gain.
	Need int
	// Pack and Bank are the bones held and banked by object id (the bank as last read, whatever BankIs says).
	Pack, Bank map[int]int
	BankIs     BankReading
	// BankBlocked: a booth would not open this run, so the bank is not tried again. BuryBlocked: the pack's bones
	// would not bury (the pack is not visible to the server), so they are carried on to the bank.
	BankBlocked, BuryBlocked bool
	// Mule is the mule's PUBLISHED bones by object id; MuleKnown false means no mule, or one not heard.
	// MuleOK says the rung may ask it at all (PrayerRungAsksMule).
	Mule      map[int]int
	MuleKnown bool
	MuleOK    bool
	// Asks is the asks this run made; AskEmpty is that one of them brought nothing.
	Asks     int
	AskEmpty bool
	// Left is the time left in the run.
	Left time.Duration
}

// BonesMoveKind names a move of a Prayer rung.
type BonesMoveKind string

const (
	// MoveDone: the level is reached.
	MoveDone BonesMoveKind = "done"
	// MoveStop: no time is left for a move that would pay; the run ends as a timeout does.
	MoveStop BonesMoveKind = "stop"
	// MoveBury buries the pack's bones where the account stands.
	MoveBury BonesMoveKind = "bury"
	// MoveBank walks to the nearest bank, reads it and draws and buries its bones a pack at a time.
	MoveBank BonesMoveKind = "bank"
	// MoveAsk asks the mule for Ask in one kit.
	MoveAsk BonesMoveKind = "ask"
	// MoveCows kills cows for the rest of the run: no bone is to be had any other way.
	MoveCows BonesMoveKind = "cows"
)

// BonesMove is the next thing a Prayer rung does, and why (for the log).
type BonesMove struct {
	Kind BonesMoveKind
	Ask  []ItemCount
	Why  string
}

// NextBonesMove is the item rule for bones: bury what the pack holds, then draw the own bank's, then ask the mule
// for what is still needed (the bank it hands over to is read live afterwards), and only with none of them kill
// cows. It never loops by itself: the caller marks BankBlocked, BuryBlocked, AskEmpty and the counts as its moves
// come out, and Left bounds the whole.
func NextBonesMove(st BonesState) BonesMove {
	if st.Need <= 0 {
		return BonesMove{Kind: MoveDone, Why: "the level is reached"}
	}
	// Bones in hand are buried whatever the clock says: a pack of them is half a minute, and an ask that outlasted
	// the slot (the mule's line was long) has just brought them.
	if BoneCount(st.Pack) > 0 && !st.BuryBlocked {
		return BonesMove{Kind: MoveBury, Why: DescribeBones(st.Pack) + " in the pack"}
	}
	if st.Left < BoneMinLeft {
		return BonesMove{Kind: MoveStop, Why: "the run's time is spent"}
	}
	if !st.BankBlocked {
		switch {
		case BoneCount(st.Pack) > 0 && st.BankIs != BankFresh:
			// Only reached with BuryBlocked: the bank trip deposits the bones and draws them again.
			return BonesMove{Kind: MoveBank, Why: DescribeBones(st.Pack) + " in the pack would not bury; the bank draws them again"}
		case st.BankIs == BankUnread:
			return BonesMove{Kind: MoveBank, Why: "the bank has not been read yet, and it comes before the mule"}
		case st.BankIs == BankDirty:
			return BonesMove{Kind: MoveBank, Why: "the mule's bones are in the bank"}
		case BoneCount(st.Bank) > 0:
			return BonesMove{Kind: MoveBank, Why: DescribeBones(st.Bank) + " in the bank"}
		}
	}
	no := "no bones in the pack or the bank"
	switch {
	case st.BankBlocked:
		// The kit bundle banks what the pack has no room for, a pack's worth is all an ask could give, and the rest of
		// the thousand would sit in a bank that will not open.
		no = "no bank booth would open"
	case !st.MuleOK:
		no += ", and this rung leaves the mule's bones to the Queen's Prayer"
	case !st.MuleKnown:
		no += ", and the mule's bank is not known"
	case st.AskEmpty:
		no += ", and the mule's last ask brought nothing"
	case st.Asks >= MaxBoneAsks:
		no += fmt.Sprintf(", and %d asks of the mule are spent", st.Asks)
	case st.Left < BoneAskWindow:
		no += fmt.Sprintf(", and %s is too little for a mule trip", st.Left.Round(time.Second))
	default:
		if ask := PlanBonesAsk(st.Need, st.Mule); len(ask) > 0 {
			return BonesMove{Kind: MoveAsk, Ask: ask, Why: no + "; the mule publishes " + DescribeBones(st.Mule)}
		}
		no += ", and the mule publishes " + DescribeBones(st.Mule)
	}
	if st.Left < CowsWindow {
		return BonesMove{Kind: MoveStop, Why: no + "; " + st.Left.Round(time.Second).String() + " is too little to walk to the cows"}
	}
	return BonesMove{Kind: MoveCows, Why: no}
}
