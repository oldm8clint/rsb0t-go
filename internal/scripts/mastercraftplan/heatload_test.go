package mastercraftplan

import (
	"testing"

	"rsb0t-go/internal/scripts/gemplan"
)

func heatSteel(t *testing.T) Bar {
	t.Helper()
	b, ok := BarByName("Steel bar")
	if !ok {
		t.Fatal("no steel bar")
	}
	return b
}

// heatPackOf lays out ids slot by slot (0: empty); the Nature runes are one stack of n.
func heatPackOf(natures int, ids ...int32) (p [gemplan.PackSlots]HeatSlot) {
	for i, id := range ids {
		if id == 0 {
			continue
		}
		n := 1
		if id == NatureRuneID {
			n = natures
		}
		p[i] = HeatSlot{ID: id, Count: n, Slot: i}
	}
	return p
}

func repeat(id int32, n int) []int32 {
	out := make([]int32, n)
	for i := range out {
		out[i] = id
	}
	return out
}

const steelBarID int32 = 2353

// THE POINT: a load after a load is five user events, the cast included: Deposit-All of the bars,
// Withdraw-X of nine iron ore and its count, Withdraw-All of the coal (eighteen fill the pack), and
// the cast on the first ore, which lands in the slot the first bar left.
func TestHeatLoadAfterALoadIsFiveEvents(t *testing.T) {
	steel := heatSteel(t)
	pack := heatPackOf(5000, append([]int32{NatureRuneID}, repeat(steelBarID, 9)...)...)
	bank := []HeatSlot{{ID: 995, Count: 10, Slot: 0}, {ID: 440, Count: 1000, Slot: 3}, {ID: CoalID, Count: 2000, Slot: 5}, {ID: steelBarID, Count: 40, Slot: 7}}
	l, why := PlanHeatLoad(steel, pack, bank, nil, true)
	if why != "" {
		t.Fatal(why)
	}
	want := []HeatOp{
		{Kind: HeatDeposit, ID: steelBarID, Slot: 1, Count: 9},
		{Kind: HeatWithdrawX, ID: 440, Slot: 3, Count: 9},
		{Kind: HeatWithdrawAll, ID: CoalID, Slot: 5, Count: 18},
	}
	if len(l.Ops) != len(want) {
		t.Fatalf("ops %+v", l.Ops)
	}
	for i := range want {
		if l.Ops[i] != want[i] {
			t.Fatalf("op %d: %+v, want %+v", i, l.Ops[i], want[i])
		}
	}
	if l.Casts != 9 || l.CastSlot != 1 || l.Events != 5 {
		t.Fatalf("casts %d slot %d events %d", l.Casts, l.CastSlot, l.Events)
	}
	if heatFree(&l.Pack) != 0 || heatTotal(&l.Pack, NatureRuneID) != 5000 {
		t.Fatalf("pack %+v", l.Pack)
	}
}

// The first load of a run, from a pack the bank emptied: the Nature runes are drawn whole, then
// the load; still five events.
func TestHeatLoadFirstLoadDrawsTheRunes(t *testing.T) {
	steel := heatSteel(t)
	bank := []HeatSlot{{ID: NatureRuneID, Count: 11777, Slot: 0}, {ID: 440, Count: 11783, Slot: 1}, {ID: CoalID, Count: 23519, Slot: 2}}
	l, why := PlanHeatLoad(steel, [gemplan.PackSlots]HeatSlot{}, bank, nil, true)
	if why != "" {
		t.Fatal(why)
	}
	if len(l.Ops) != 3 || l.Ops[0].Kind != HeatWithdrawAll || l.Ops[0].ID != NatureRuneID || l.Ops[1].Kind != HeatWithdrawX || l.Ops[1].Count != 9 {
		t.Fatalf("ops %+v", l.Ops)
	}
	if l.Casts != 9 || l.CastSlot != 1 || l.Events != 5 {
		t.Fatalf("casts %d slot %d events %d", l.Casts, l.CastSlot, l.Events)
	}
}

// A kept weapon takes a slot, and coal left from the last load stays and is topped up: eight bars
// a load, still one deposit.
func TestHeatLoadKeepsTheWeaponAndTheLeftoverCoal(t *testing.T) {
	steel := heatSteel(t)
	const sword int32 = 1291
	ids := append([]int32{NatureRuneID, sword}, repeat(steelBarID, 8)...)
	ids = append(ids, CoalID, CoalID)
	pack := heatPackOf(300, ids...)
	bank := []HeatSlot{{ID: 440, Count: 500, Slot: 0}, {ID: CoalID, Count: 900, Slot: 1}}
	l, why := PlanHeatLoad(steel, pack, bank, func(id int32) bool { return id == sword }, true)
	if why != "" {
		t.Fatal(why)
	}
	if len(l.Ops) != 3 || l.Ops[0].Kind != HeatDeposit || l.Ops[0].ID != steelBarID || l.Ops[1].Count != 8 {
		t.Fatalf("ops %+v", l.Ops)
	}
	if l.Casts != 8 || l.CastSlot != 2 || heatTotal(&l.Pack, sword) != 1 || heatTotal(&l.Pack, CoalID) != 18 {
		t.Fatalf("casts %d slot %d pack %+v", l.Casts, l.CastSlot, l.Pack)
	}
}

// Iron bars take no coal, and two coal in the pack would make the cast a steel bar: the coal goes
// back with the bars, and the ore is one Withdraw-All.
func TestHeatLoadIronBarsBankTheCoal(t *testing.T) {
	iron, _ := BarByName("Iron bar")
	ids := append([]int32{NatureRuneID, CoalID, CoalID}, repeat(iron.BarID, 4)...)
	pack := heatPackOf(50, ids...)
	bank := []HeatSlot{{ID: 440, Count: 100, Slot: 2}}
	l, why := PlanHeatLoad(iron, pack, bank, nil, true)
	if why != "" {
		t.Fatal(why)
	}
	if len(l.Ops) != 3 || l.Ops[0].ID != CoalID || l.Ops[1].ID != iron.BarID || l.Ops[2].Kind != HeatWithdrawAll || l.Ops[2].ID != 440 {
		t.Fatalf("ops %+v", l.Ops)
	}
	if l.Casts != 27 || heatTotal(&l.Pack, CoalID) != 0 || l.Events != 4 {
		t.Fatalf("casts %d events %d pack %+v", l.Casts, l.Events, l.Pack)
	}
}

// The bank's coal bounds the load; the ore is the bank's last, drawn with Withdraw-All.
func TestHeatLoadShortStock(t *testing.T) {
	steel := heatSteel(t)
	pack := heatPackOf(40, NatureRuneID)
	l, _ := PlanHeatLoad(steel, pack, []HeatSlot{{ID: 440, Count: 100, Slot: 0}, {ID: CoalID, Count: 5, Slot: 1}}, nil, true)
	if l.Casts != 2 || l.Ops[0].Kind != HeatWithdrawX || l.Ops[0].Count != 2 || l.Ops[1].Count != 5 {
		t.Fatalf("coal-bound: casts %d ops %+v", l.Casts, l.Ops)
	}
	l, _ = PlanHeatLoad(steel, pack, []HeatSlot{{ID: 440, Count: 3, Slot: 0}, {ID: CoalID, Count: 900, Slot: 1}}, nil, true)
	if l.Casts != 3 || l.Ops[0].Kind != HeatWithdrawAll || l.Ops[0].Count != 3 || l.Events != 3 {
		t.Fatalf("ore-bound: casts %d events %d ops %+v", l.Casts, l.Events, l.Ops)
	}
}

// Without a fire staff the Fire runes are drawn and kept too, and they bound the casts.
func TestHeatLoadWithoutAStaff(t *testing.T) {
	steel := heatSteel(t)
	bank := []HeatSlot{{ID: NatureRuneID, Count: 100, Slot: 0}, {ID: FireRuneID, Count: 20, Slot: 1}, {ID: 440, Count: 100, Slot: 2}, {ID: CoalID, Count: 900, Slot: 3}}
	l, why := PlanHeatLoad(steel, [gemplan.PackSlots]HeatSlot{}, bank, nil, false)
	if why != "" {
		t.Fatal(why)
	}
	if l.Ops[1].ID != FireRuneID || l.Casts != 5 || l.CastSlot != 2 {
		t.Fatalf("casts %d slot %d ops %+v", l.Casts, l.CastSlot, l.Ops)
	}
}

func TestHeatLoadSaysWhy(t *testing.T) {
	steel := heatSteel(t)
	if _, why := PlanHeatLoad(steel, [gemplan.PackSlots]HeatSlot{}, []HeatSlot{{ID: 440, Count: 9, Slot: 0}, {ID: CoalID, Count: 18, Slot: 1}}, nil, true); why != "no Nature runes in the pack or the bank" {
		t.Fatalf("natures: %q", why)
	}
	pack := heatPackOf(9, NatureRuneID)
	if _, why := PlanHeatLoad(steel, pack, []HeatSlot{{ID: CoalID, Count: 18, Slot: 1}}, nil, true); why != "no Iron ore in the pack or the bank" {
		t.Fatalf("ore: %q", why)
	}
	if _, why := PlanHeatLoad(steel, pack, []HeatSlot{{ID: 440, Count: 9, Slot: 0}, {ID: CoalID, Count: 1, Slot: 1}}, nil, true); why != "not enough Coal for a Steel bar" {
		t.Fatalf("coal: %q", why)
	}
}
