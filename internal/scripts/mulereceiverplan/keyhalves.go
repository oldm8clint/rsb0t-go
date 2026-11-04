package mulereceiverplan

// Joining key halves into crystal keys, the mule's third idle job after opening the fleet's
// banked caskets/oysters and cooking its raw fish (owner, 2026-09-20: "we can also make it
// that the mulereciever combines the key halves after doing casket openings withdrawing 14 of
// each half and using them on each other, i think 5 can be made per tick").
//
// This file is the pure half: which slots to use on which, how many may be sent before
// waiting, and how many of each half to withdraw. The live withdraw/click/bank sequence is
// internal/scripts/mulerunner/keys.go.
//
// Three facts this is built on, each read out of the engine and the content rather than
// assumed:
//
//   - The join is content/scripts/areas/area_taverly/scripts/crystal_key.rs2's
//     [opheldu,keyhalf1] / [opheldu,keyhalf2] pair, both reaching [label,join_keys]:
//     inv_del one of each half, inv_add one crystal_key, "You join the two halves of the key
//     together." There is no p_delay and no anim, so a join costs no player delay at all.
//   - Either direction works. Engine-TS's OpHeldUHandler looks for [opheldu,<target obj>]
//     first and falls back to [opheldu,<used obj>], swapping lastItem/lastUseItem when it
//     does, so using 985 on 987 and 987 on 985 both land in join_keys.
//   - JoinsPerTick below is the engine's own per-tick ceiling, not a guess. See its comment.
//
// What is deliberately NOT modelled here: which of the two ids is "keyhalf1". Content 289
// names BOTH unnoted halves "Half of a key" (obj.pack: 985=keyhalf1, 987=keyhalf2, and
// cfg.Obj(985).Name == cfg.Obj(987).Name), so a name lookup cannot tell them apart — the same
// trap casketrunner.ResolveCasketIDs documents for "Casket". The caller resolves the two
// distinct unnoted ids and passes them in; this package only ever pairs one of each.

// JoinsPerTick is how many joins the engine will actually run in one 600ms tick, measured
// from Engine-TS 289's own source rather than taken from the owner's estimate (which was 5,
// and is right):
//
//	network/game/client/model/OpHeldU.ts   category = ClientGameProtCategory.USER_EVENT
//	network/game/client/ClientGameProtCategory.ts  USER_EVENT = new (1, 5)   // limit 5
//	engine/entity/NetworkPlayer.ts   while (this.userLimit < USER_EVENT.limit && ... this.read())
//	                                 ... if (success && category === USER_EVENT) this.userLimit++
//
// so decoding stops after the fifth SUCCESSFULLY handled OPHELDU of a tick and resumes next
// tick. Sending more than five at once is therefore not an error — the engine simply buffers
// the rest — but it is sent against a pack snapshot that is already one join stale, which is
// how a slot reference goes wrong. Send at most this many, then read the pack again.
const JoinsPerTick = 5

// HalvesPerTrip is how many of EACH half to withdraw: 14 and 14 fill this package's own
// PackSlots (28) exactly, which is the number the owner named and the largest that wastes no
// slot.
const HalvesPerTrip = 14

// HalfSlot is one backpack slot holding a key half.
type HalfSlot struct {
	Slot int
	ID   int
}

// JoinPair is one join to send: use the item in From on the item in To. They always hold
// different half ids.
type JoinPair struct {
	From HalfSlot
	To   HalfSlot
}

// PairHalves pairs each slot holding idA with a slot holding idB, lowest slot first, and
// returns at most max pairs (max <= 0 means JoinsPerTick). Slots holding neither id are
// ignored, and a slot is never used twice.
//
// Lowest-slot-first matters for more than tidiness. Content's inv_del(inv, keyhalf1, 1)
// deletes from the FIRST slot holding that id, not from the slot the packet named, so pairing
// in slot order keeps the engine's deletions and this plan's own slot references walking down
// the pack together.
func PairHalves(pack []HalfSlot, idA, idB, max int) []JoinPair {
	if max <= 0 {
		max = JoinsPerTick
	}
	var as, bs []HalfSlot
	for _, s := range pack {
		switch s.ID {
		case idA:
			as = append(as, s)
		case idB:
			bs = append(bs, s)
		}
	}
	if idA == idB {
		return nil
	}
	sortBySlot(as)
	sortBySlot(bs)
	n := len(as)
	if len(bs) < n {
		n = len(bs)
	}
	if n > max {
		n = max
	}
	out := make([]JoinPair, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, JoinPair{From: as[i], To: bs[i]})
	}
	return out
}

func sortBySlot(s []HalfSlot) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Slot < s[j-1].Slot; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// JoinableHalves is how many crystal keys a pack (or a bank) holding these counts of each
// half can make: the smaller of the two, since a join needs one of each.
func JoinableHalves(countA, countB int) int {
	if countA < countB {
		return countA
	}
	return countB
}

// WithdrawPerHalf is how many of each half to take out for one joining round, given how many
// of each the bank holds and how many pack slots are free. Never more than HalvesPerTrip, and
// never more than half the free slots, so both halves fit.
func WithdrawPerHalf(bankA, bankB, freeSlots int) int {
	n := JoinableHalves(bankA, bankB)
	if n > HalvesPerTrip {
		n = HalvesPerTrip
	}
	if half := freeSlots / 2; n > half {
		n = half
	}
	if n < 0 {
		n = 0
	}
	return n
}
