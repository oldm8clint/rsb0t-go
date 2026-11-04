package mastercraftplan

import "rsb0t-go/internal/scripts/gemplan"

// THE SUPERHEAT LOAD (a user request: "banking after finishing superheat inventory the main account stays
// idle for too long, should have the fast bank ... ensure superheating is running at its fastest
// rate and banking withdrawing depositing sped up tremendously").
//
// THE ENGINE (engine/src, branch 289), on top of THE PACE (superheat.go):
//
//   - Every cast closes the bank. OpHeldTHandler calls Player.clearPendingAction, which is
//     clearInteraction and closeModal: the bank cannot stay open through a load.
//   - INV_BUTTON (a deposit or a withdrawal) and OPLOC (the booth) are refused while the player is
//     delayed, like the cast itself (InvButtonHandler, OpLocHandler: "cannot interact while
//     delayed"). The old round sent the booth op the moment the last bar showed, inside the cast's
//     delay, so the engine dropped it and every load waited out a 20-second "timed out waiting for
//     bank to open".
//   - A cast after the booth op in the same tick clears the booth's interaction (clearPendingAction
//     again), and a booth op after a cast is refused (the cast's delay). So the booth op needs a
//     tick of its own: the first tick the last cast's delay has passed. The bank opens in that
//     tick's player turn (processInteraction, after the reads).
//   - The next tick takes the whole load and the first cast, five user events
//     (NetworkPlayer.decodeIn reads five handled ones a tick): Deposit-All of the bars (one event
//     banks every bar of the kind), Withdraw-X of the ore and its count (two: bank.rs2
//     @bank_withdraw stops at p_countdialog and RESUME_P_COUNTDIALOG resumes it in the same read),
//     Withdraw-All of the second ingredient (one: it fills every slot left), and the cast on the
//     first ore's slot (one). The cast must come last: its p_delay(1) refuses anything after it.
//   - Withdraw-X and Withdraw-All put each ore and coal in the lowest empty slot in turn
//     (Inventory.add), and a stackable rune joins its stack, so the slot the first cast names is
//     known before the server answers.
//
// So a load of n bars is n casts three ticks apart and one booth tick: 28 ticks for nine steel
// bars, against 34 ticks of casts plus a full bank visit, a 20-second timeout and a mule trade for
// every load before.

// HeatSlot is one pack slot or bank row as a load plan reads it: the object (0: empty), how many,
// and, for a bank row, the bank slot.
type HeatSlot struct {
	ID    int32
	Count int
	Slot  int
}

// HeatOpKind names one bank operation of a load.
type HeatOpKind int

const (
	// HeatDeposit is Deposit-All of ID from pack slot Slot: every one of ID goes.
	HeatDeposit HeatOpKind = iota
	// HeatWithdrawAll is Withdraw-All of ID from bank slot Slot: as many as the pack has room for,
	// or the whole stack of a stackable item.
	HeatWithdrawAll
	// HeatWithdrawX is Withdraw-X of Count of ID from bank slot Slot and its count answer.
	HeatWithdrawX
)

// HeatOp is one bank operation of a load.
type HeatOp struct {
	Kind  HeatOpKind
	ID    int32
	Slot  int
	Count int
}

// Events is the user events the op takes: two for a Withdraw-X (the button and the count), else one.
func (o HeatOp) Events() int {
	if o.Kind == HeatWithdrawX {
		return 2
	}
	return 1
}

// HeatLoad is one load: the bank operations in the order they are sent, then the first cast on
// CastSlot.
type HeatLoad struct {
	Ops      []HeatOp
	CastSlot int
	// Casts is the casts the pack the ops leave can make; Events the user events, the cast
	// included; Pack that pack.
	Casts, Events int
	Pack          [gemplan.PackSlots]HeatSlot
}

// heatStackable reports whether an item stacks in the pack (the spell's runes; ores, coal and bars
// do not).
func heatStackable(id int32) bool { return id == NatureRuneID || id == FireRuneID }

func heatTotal(pack *[gemplan.PackSlots]HeatSlot, id int32) int {
	n := 0
	for _, s := range pack {
		if s.ID == id {
			n += max(1, s.Count)
		}
	}
	return n
}

func heatFree(pack *[gemplan.PackSlots]HeatSlot) int {
	n := 0
	for _, s := range pack {
		if s.ID == 0 {
			n++
		}
	}
	return n
}

// heatAdd adds up to n of id as the engine's Inventory.add does (a stack joins its stack or takes
// the lowest empty slot; anything else one to each lowest empty slot) and returns how many went in.
func heatAdd(pack *[gemplan.PackSlots]HeatSlot, id int32, n int) int {
	if n <= 0 {
		return 0
	}
	if heatStackable(id) {
		for i := range pack {
			if pack[i].ID == id {
				pack[i].Count += n
				return n
			}
		}
		for i := range pack {
			if pack[i].ID == 0 {
				pack[i] = HeatSlot{ID: id, Count: n, Slot: i}
				return n
			}
		}
		return 0
	}
	in := 0
	for i := range pack {
		if in == n {
			break
		}
		if pack[i].ID == 0 {
			pack[i] = HeatSlot{ID: id, Count: 1, Slot: i}
			in++
		}
	}
	return in
}

// HeatCasts is how many casts of bar a pack can make: its ore, its second ingredient at its count
// a bar, a Nature rune each and, with no fire staff, four Fire runes each.
func HeatCasts(bar Bar, pack *[gemplan.PackSlots]HeatSlot, staff bool) int {
	n := min(heatTotal(pack, bar.OreID), heatTotal(pack, NatureRuneID))
	if bar.SecondPer > 0 {
		n = min(n, heatTotal(pack, bar.SecondID)/bar.SecondPer)
	}
	if !staff {
		n = min(n, heatTotal(pack, FireRuneID)/SuperheatFire)
	}
	return max(n, 0)
}

// PlanHeatLoad plans the load of bar from pack (by slot) and the bank's rows: Deposit-All of every
// kind the load does not use (keep names more to leave in the pack: the weapon a fire staff
// replaced), Withdraw-All of the runes the pack lacks, then the ore (Withdraw-X, or Withdraw-All
// when that draws exactly what the load wants) and Withdraw-All of the second ingredient, the most
// casts the free slots and the bank allow. Ore or coal already in the pack stays and counts. It
// returns why when the load makes no cast.
func PlanHeatLoad(bar Bar, pack [gemplan.PackSlots]HeatSlot, bank []HeatSlot, keep func(id int32) bool, staff bool) (HeatLoad, string) {
	var l HeatLoad
	sim := pack
	for i := range sim {
		if sim[i].ID != 0 && sim[i].Count <= 0 {
			sim[i].Count = 1
		}
	}
	row := func(id int32) (slot, n int) {
		for _, r := range bank {
			if r.ID == id && r.Count > 0 {
				return r.Slot, r.Count
			}
		}
		return -1, 0
	}
	k := bar.SecondPer
	uses := func(id int32) bool {
		return id == NatureRuneID || (!staff && id == FireRuneID) || id == bar.OreID || (k > 0 && id == bar.SecondID) || (keep != nil && keep(id))
	}
	// 1. Deposit-All of every other kind, lowest slot first.
	gone := map[int32]bool{}
	for s, it := range sim {
		if it.ID == 0 || gone[it.ID] || uses(it.ID) {
			continue
		}
		gone[it.ID] = true
		l.Ops = append(l.Ops, HeatOp{Kind: HeatDeposit, ID: it.ID, Slot: s, Count: heatTotal(&sim, it.ID)})
	}
	for i := range sim {
		if gone[sim[i].ID] {
			sim[i] = HeatSlot{}
		}
	}
	// 2. The runes the pack lacks.
	runes := []int32{NatureRuneID}
	if !staff {
		runes = append(runes, FireRuneID)
	}
	for _, id := range runes {
		if heatTotal(&sim, id) > 0 {
			continue
		}
		slot, n := row(id)
		if n <= 0 || heatAdd(&sim, id, n) == 0 {
			continue
		}
		l.Ops = append(l.Ops, HeatOp{Kind: HeatWithdrawAll, ID: id, Slot: slot, Count: n})
	}
	// 3. The ore and the second ingredient: the most casts the slots and the bank allow.
	free := heatFree(&sim)
	oreSlot, oreBank := row(bar.OreID)
	oreHave := heatTotal(&sim, bar.OreID)
	secSlot, secBank, secHave := -1, 0, 0
	if k > 0 {
		secSlot, secBank = row(bar.SecondID)
		secHave = heatTotal(&sim, bar.SecondID)
	}
	casts, draw := 0, 0
	for n := min(oreHave+oreBank, gemplan.PackSlots); n > 0; n-- {
		dr := max(0, n-oreHave)
		if dr > free {
			continue
		}
		if k > 0 && secHave+min(free-dr, secBank) < k*n {
			continue
		}
		casts, draw = n, dr
		break
	}
	if draw > 0 {
		if k == 0 || draw == oreBank {
			// Withdraw-All draws min(free, the bank's): exactly draw here.
			l.Ops = append(l.Ops, HeatOp{Kind: HeatWithdrawAll, ID: bar.OreID, Slot: oreSlot, Count: draw})
		} else {
			l.Ops = append(l.Ops, HeatOp{Kind: HeatWithdrawX, ID: bar.OreID, Slot: oreSlot, Count: draw})
		}
		heatAdd(&sim, bar.OreID, draw)
	}
	if k > 0 && casts > 0 && secHave < k*casts && secBank > 0 {
		in := heatAdd(&sim, bar.SecondID, min(heatFree(&sim), secBank))
		l.Ops = append(l.Ops, HeatOp{Kind: HeatWithdrawAll, ID: bar.SecondID, Slot: secSlot, Count: in})
	}
	l.Pack = sim
	l.Casts = HeatCasts(bar, &sim, staff)
	l.CastSlot = -1
	for i, s := range sim {
		if s.ID == bar.OreID {
			l.CastSlot = i
			break
		}
	}
	l.Events = 1
	for _, op := range l.Ops {
		l.Events += op.Events()
	}
	switch {
	case l.Casts > 0:
		return l, ""
	case heatTotal(&sim, NatureRuneID) == 0:
		return l, "no Nature runes in the pack or the bank"
	case !staff && heatTotal(&sim, FireRuneID) < SuperheatFire:
		return l, "not enough Fire runes (no fire staff)"
	case oreHave+oreBank == 0:
		return l, "no " + bar.Ore + " in the pack or the bank"
	case k > 0 && secHave+secBank < k:
		return l, "not enough " + bar.Second + " for a " + bar.Name
	}
	return l, "no pack room for a " + bar.Name + "'s ore"
}
