package boneplan

// THE BONE HAUL (a user request): "we should get 43 prayer on each swarm account and then save each bone for master to bury
// as one of its tasks as burying bones at a bank is the fastest exp per hour and we want master to get 200m prayer first, beyond 43
// prayer doesnt benefit the swarm". Pure.
//
// Below the skip level (fleetrunner.BoneSkipPrayer, 43) a swarm account buries every bone its training kills leave, as before. At or
// above it burying is worth nothing to the swarm, so the account PICKS THE BONES UP INSTEAD and carries them to its next bank
// visit; the loot hand-off (consolidateplan.PlanLoot, which only keeps the bones an account's own Prayer goal still needs) then
// takes them to the mule as bank notes with the rest of the loot, on the hand-over trips the account makes anyway. No trip is added
// for a bone, so the mule's crowd rules (scarabplan/mulecrowd.go) are untouched. The MasterCrafter's bone round
// (mastercraftplan/bones.go) draws them from the mule.
//
// A pack is no warehouse: the account carries at most HaulCarry bones and never takes one that would leave fewer than HaulReserve
// slots free for its food, runes and the loot it exists to collect.

// Haul defaults.
const (
	// DefaultHaulCarry is the most bones a swarm account carries between two bank visits.
	DefaultHaulCarry = 12
	// DefaultHaulReserve is the pack slots a pickup never takes.
	DefaultHaulReserve = 8
)

// HaulAction is what a swarm account does with a bone it could take.
type HaulAction int

// The actions.
const (
	// HaulLeave leaves the bone where it lies.
	HaulLeave HaulAction = iota
	// HaulBury picks it up and buries it (Prayer under the skip level).
	HaulBury
	// HaulCarry picks it up and keeps it for the bank (Prayer at the skip level, room in the pack).
	HaulCarry
)

// HaulInput is what the rule reads.
type HaulInput struct {
	// Prayer is the base Prayer level and Skip the level at which burying stops paying the swarm.
	Prayer, Skip int
	// On is the haul switch (scarabBoneHaul); off, an account at the skip level leaves bones as it always did.
	On bool
	// Carried is the bones in the pack, Free its empty slots.
	Carried, Free int
	// Carry and Reserve are the most bones carried and the slots always left free (zero: the defaults).
	Carry, Reserve int
}

// HaulFor decides what to do with one more bone.
func HaulFor(in HaulInput) HaulAction {
	if in.Prayer < in.Skip {
		return HaulBury
	}
	if !in.On {
		return HaulLeave
	}
	carry, reserve := in.Carry, in.Reserve
	if carry <= 0 {
		carry = DefaultHaulCarry
	}
	if reserve <= 0 {
		reserve = DefaultHaulReserve
	}
	if in.Carried >= carry || in.Free <= reserve {
		return HaulLeave
	}
	return HaulCarry
}

// BonePrayerXP10 is the Prayer experience, in tenths of a point, one buried bone pays (content skill_prayer/configs/bones.obj,
// param bone_exp): Bones, Bat bones and Wolf bones 4.5, Big bones 15, Babydragon bones 30, Dragon bones 72. 0 for a name that is
// not one of the tradeable bones.
func BonePrayerXP10(name string) int {
	for _, b := range BoneKinds {
		if b.Name == name {
			return b.XP10
		}
	}
	return 0
}

// BoneKind is one bone the Master round may bury: its name, obj id and Prayer experience in tenths.
type BoneKind struct {
	Name string
	ID   int32
	XP10 int
}

// BoneKinds are the bones the bank round buries, dearest experience first (content 289: pack/obj.pack ids, bones.obj bone_exp).
var BoneKinds = []BoneKind{
	{"Dragon bones", 536, 720},
	{"Babydragon bones", 534, 300},
	{"Big bones", 532, 150},
	{"Bones", 526, 45},
	{"Wolf bones", 2859, 45},
	{"Bat bones", 530, 45},
}
