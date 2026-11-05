package mastercraftrunner

// The bone-burying round's fast run: Withdraw-All of the best bone the bank holds, 28 Bury events, the booth op, six ticks a load. See
// mastercraftplan/bones.go for the content (bury_bone.rs2: p_delay(0), stat_advance(prayer, bone_exp); no level, tool or members check)
// and the stream's arithmetic.

import (
	"fmt"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/boneplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
)

// boneLayoutOK reads a pack for a finished bone load: empty (every bone buried).
func boneLayoutOK(pack []gamestate.SlotItem) (bool, string) {
	if len(pack) == 0 {
		return true, ""
	}
	return false, fmt.Sprintf("item %d in slot %d", pack[0].ID, pack[0].Slot)
}

// BonesOwn reads the bones a bank reading holds, by kind name.
func BonesOwn(banked []gamestate.SlotItem) map[string]int {
	out := map[string]int{}
	for _, it := range banked {
		for _, k := range boneplan.BoneKinds {
			if it.ID == k.ID {
				out[k.Name] += int(it.Count)
			}
		}
	}
	return out
}

// boneSpec is the fast run's spec for burying the best bone the bank holds (each load is withdrawn from the bank slot the run read
// as the bank opened, so a bank that holds two kinds runs the dearer kind to under a load, and the round's next run takes the other).
func boneSpec() fastSpec {
	var kind boneplan.BoneKind
	bankSlot := -1
	s := fastSpec{
		what: "bones", stream: mastercraftplan.BoneStream, stat: PrayerStat, xpTenths: 0,
		done: func(pack []gamestate.SlotItem) (bool, string) { return boneLayoutOK(pack) },
	}
	s.prepare = func(pack, bank []gamestate.SlotItem) (int, string) {
		if ok, why := boneLayoutOK(pack); !ok {
			return 0, why
		}
		held := BonesOwn(bank)
		k, ok := mastercraftplan.BestBone(held)
		if !ok {
			return 0, "the bank holds less than a load of any bone"
		}
		kind = k
		for _, it := range bank {
			if it.ID == k.ID {
				bankSlot = it.Slot
			}
		}
		if bankSlot < 0 {
			return 0, "the bank slot of " + k.Name + " was not read"
		}
		return held[k.Name] / mastercraftplan.BoneLoad, ""
	}
	s.useXP = func() int { return kind.XP10 }
	s.send = func(b actions.Session, ev mastercraftplan.FastEvent, invCom, sideCom int) error {
		switch ev.Kind {
		case mastercraftplan.FastWithdraw:
			return actions.InvButton(b, 4, kind.ID, bankSlot, actions.BankWithdraw) // Withdraw-All: a full pack
		case mastercraftplan.FastUse:
			return actions.OpHeld(b, 1, kind.ID, ev.Use, invCom) // Bury: slot k holds a bone, p_delay(0)
		}
		return fmt.Errorf("the bone stream has no event %d", ev.Kind)
	}
	return s
}

// PrayerStat is UPDATE_STAT's index for Prayer.
const PrayerStat = actions.PrayerStat

// FastBones buries bones the fast way from a primed quick bank (see FastStaves): 28 a load in six ticks. xpTenths is read as the
// bank opens, so the XP guard of the run uses the kind it will bury. When the run cannot start the visit banks the pack and draws
// nothing (Load; Short when the bank holds less than a load of any kind).
func FastBones(d *fleetrunner.Deps, q *QuickBank, o FastOptions) FastResult {
	r, qv := fastRun(d, q, boneSpec(), o)
	if qv != nil {
		r.Load = loadVisit(d, qv)
	}
	return r
}
