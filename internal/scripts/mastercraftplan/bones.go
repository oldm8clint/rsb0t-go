package mastercraftplan

import (
	"fmt"
	"strconv"
	"time"

	"rsb0t-go/internal/scripts/boneplan"
)

// THE BONE-BURYING ROUND (a user request: "we should get 43 prayer on each swarm account and then save each bone for master to bury
// as one of its tasks as burying bones at a bank is the fastest exp per hour and we want master to get 200m prayer first").
//
// WHERE THE BONES COME FROM. The swarm accounts past Prayer 43 pick up their training kills' bones and carry them to the next bank visit
// (boneplan/haul.go, scarabBoneHaul); the loot hand-off takes them to the mule as bank notes with the rest of the loot, on trips the
// accounts make anyway. The mule banks them. This round draws them out.
//
// THE CONTENT (content/scripts/skill_prayer/scripts/bury_bone.rs2, configs/bones.obj, branch 289): [opheld1,_bones] is label
// bury_bones: a message, an anim, p_stopaction, p_delay(0), then inv_delslot and stat_advance(prayer, bone_exp). No level check, no
// tool, no members check: any bone buries on any world. bone_exp in tenths of a point: Bones, Bat bones, Wolf bones 45 (4.5 xp), Big
// bones 150 (15), Babydragon bones 300 (30), Dragon bones 720 (72). p_delay(0) does not hold the next event, so every Bury is one
// handled user event and the engine's five-a-tick read (fast.go) is the pace.
//
// THE LOAD (fast stream BoneStream): Withdraw-All of the best bone the bank holds fills the empty pack with 28 (bones do not stack);
// 28 Bury events (OPHELD1, slot k, the lowest slot holding it); the booth op last in its tick: 30 events, six ticks a load of 28 bones,
// no Deposit-All (the pack is empty again) and no filler. Dragon bones: 28 x 72 = 2,016 xp a load, 20,160 xp every minute. Big bones
// (the swarm's commonest kill drop beside Bones) 420 xp a load, 4,200 a minute, 252,000 an hour; Bones 126 a load, 75,600 an hour.
//
// THE TRIGGER. The mule's published bank holds at least craftBonesMinStock bones (all the kinds summed; 5,000 by default); a round
// cut short resumes from this account's own bank with no threshold while it holds a load. Prayer under the 200 million cap. In the
// schedule's rotation (RotationKey "bones"): batch rounds take turns, the one that ended longest ago goes. THE LENGTH: craftBonesRoundMinutes
// (60) and paused craftBonesPauseMinutes (0) after, so the rotation, not a pause, shares the time.
//
// THE KIT. Bones are asked for as bank notes (the mule's per-ask cap, craftKitSize lines): the bank takes a deposited note as its item
// (consolidateplan.MuleBankSlots comment), so a kit of Big bones notes lands in the bank as 1,000 Big bones.

// The settings of the round.
const (
	KeyBones         = "craftBones"
	KeyBonesMinStock = "craftBonesMinStock"
	KeyBonesRound    = "craftBonesRoundMinutes"
	KeyBonesPause    = "craftBonesPauseMinutes"

	DefaultBonesMinStock = 5000
	DefaultBonesRound    = 60 * time.Minute
	DefaultBonesPause    = 0
)

func init() {
	knownKeys[KeyBones] = true
	for _, k := range []string{KeyBonesMinStock, KeyBonesRound, KeyBonesPause} {
		knownKeys[k] = true
	}
}

// BoneLoad is the bones one fast load buries: a full pack.
const BoneLoad = 28

// BoneStream is a bone-burying load: Withdraw-All of the bones, 28 Bury events, the booth op: 30 events, six ticks.
var BoneStream = FastStream{Head: []FastKind{FastWithdraw}, Uses: BoneLoad}

// ModeBones buries bones at the mule's post (bones.go).
const ModeBones Mode = "bones"

// KeyBonesRotation is the rotation's key for the round.
const KeyBonesRotation = "bones"

func (c ScheduleSettings) validateBones() []string {
	var p []string
	if c.BonesMinStock < BoneLoad {
		p = append(p, fmt.Sprintf("%s %d is below %d (one load)", KeyBonesMinStock, c.BonesMinStock, BoneLoad))
	}
	if c.BonesRound < 0 || c.BonesRound > 24*time.Hour {
		p = append(p, fmt.Sprintf("%s %s is outside 0..1440 minutes (0 is no limit)", KeyBonesRound, c.BonesRound))
	}
	if c.BonesPause < 0 || c.BonesPause > 24*time.Hour {
		p = append(p, fmt.Sprintf("%s %s is outside 0..1440 minutes", KeyBonesPause, c.BonesPause))
	}
	return p
}

// BonesLedger is the round's memory.
type BonesLedger struct {
	// PauseUntil holds the round off after its hour (craftBonesPauseMinutes) or one that buried nothing.
	PauseUntil time.Time `json:"pauseUntil,omitempty"`
	// Rounds, Buried and XP10 count the rounds, the bones buried and the Prayer experience (tenths) they paid.
	Rounds int `json:"rounds,omitempty"`
	Buried int `json:"buried,omitempty"`
	XP10   int `json:"xp10,omitempty"`
}

// BonePlan is a decided round.
type BonePlan struct {
	// Mule and Own are the bones the mule's published bank and this account's bank hold, kinds summed.
	Mule, Own int
	Resume    bool
	Why       string
}

// BoneCounts sums the bones a Stock holds by kind.
func BoneCounts(st Stock) map[string]int {
	out := map[string]int{}
	for _, k := range boneplan.BoneKinds {
		if n := stockOf(st, k.Name); n > 0 {
			out[k.Name] = n
		}
	}
	return out
}

// BoneTotal is the bones in a count map.
func BoneTotal(c map[string]int) int {
	n := 0
	for _, v := range c {
		n += v
	}
	return n
}

// BestBone is the bone to bury next out of what a bank holds: the dearest experience of those with at least one load (BoneLoad). ok
// is false when the bank holds not one load of any kind (a short load is not worth a bank visit).
func BestBone(held map[string]int) (boneplan.BoneKind, bool) {
	for _, k := range boneplan.BoneKinds {
		if held[k.Name] >= BoneLoad {
			return k, true
		}
	}
	return boneplan.BoneKind{}, false
}

// BonesReady decides the round. mule is nil when the mule's bank has not been published; own is this account's bank's bones by kind.
func BonesReady(set Settings, prayerXP int, mule Stock, own map[string]int) (BonePlan, bool) {
	p := BonePlan{Mule: BoneTotal(BoneCounts(mule)), Own: BoneTotal(own)}
	switch {
	case prayerXP >= MaxSkillXP:
		p.Why = "Prayer is at " + strconv.Itoa(MaxSkillXP) + " experience, the most it can earn"
	case func() bool { _, ok := BestBone(own); return ok }():
		p.Resume = true
		p.Why = "this account's bank holds " + strconv.Itoa(p.Own) + " bones to bury"
		return p, true
	case mule == nil:
		p.Why = "the mule has not published its bank"
	case p.Mule >= set.Sched.BonesMinStock:
		p.Why = "the mule holds " + strconv.Itoa(p.Mule) + " bones (threshold " + strconv.Itoa(set.Sched.BonesMinStock) + ")"
		return p, true
	default:
		p.Why = "the mule holds " + strconv.Itoa(p.Mule) + " bones (need " + strconv.Itoa(set.Sched.BonesMinStock) + ")"
	}
	return p, false
}

// PlanBonesAsk is one kit ask: the best bone the mule holds, at most set.KitSize in the one line (the mule's per-ask cap), as notes
// (the bot asks noted); nil when the mule holds not one load of any kind.
func PlanBonesAsk(set Settings, mule Stock) []Line {
	k, ok := BestBone(BoneCounts(mule))
	if !ok {
		return nil
	}
	n := min(stockOf(mule, k.Name), set.KitSize)
	n -= n % BoneLoad // whole loads: a remainder would sit in the bank
	if n < BoneLoad {
		return nil
	}
	return []Line{{Item: k.Name, Qty: n}}
}

// BonesPerHour is the Prayer experience an hour the fast stream buries kind at: loads of BoneLoad in BoneStream.Ticks() ticks of
// 0.6 s.
func BonesPerHour(k boneplan.BoneKind) int {
	loadsPerHour := 3600.0 / (0.6 * float64(BoneStream.Ticks()))
	return int(loadsPerHour * float64(BoneLoad*k.XP10) / 10)
}
