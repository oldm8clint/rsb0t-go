package bowstringrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/fleetrunner"
)

// The Yew longbow's ids in content 289: unstrung 66, strung 855, and the Bow string 1777.
var yewLongbow = IDs{Unstrung: 66, Strung: 855, String: 1777}

func cycleDeps(sim *actionsim.Session) *fleetrunner.Deps {
	return &fleetrunner.Deps{Session: sim, Components: actionsim.Components(), Log: func(string) {},
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true }}
}

func strungLoad() []actionsim.Stack {
	var out []actionsim.Stack
	for i := 0; i < bowstringplan.Batch; i++ {
		out = append(out, actionsim.Stack{ID: yewLongbow.Strung, Count: 1})
	}
	return out
}

func held(rows []actionsim.Stack, id int32) int {
	n := 0
	for _, r := range rows {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

// The pack is a finished load only when it is exactly the batch of strung bows.
func TestLoadHeldIsExactlyABatchOfStrungBows(t *testing.T) {
	load := func(ids ...int32) []gamestate.SlotItem {
		var out []gamestate.SlotItem
		for i, id := range ids {
			out = append(out, gamestate.SlotItem{Slot: i, Item: gamestate.Item{ID: id, Count: 1}})
		}
		return out
	}
	fourteen := func(extra ...int32) []int32 {
		ids := make([]int32, bowstringplan.Batch-len(extra))
		for i := range ids {
			ids[i] = yewLongbow.Strung
		}
		return append(ids, extra...)
	}
	if !loadHeld(load(fourteen()...), yewLongbow) {
		t.Error("14 strung bows are a finished load")
	}
	for name, p := range map[string][]gamestate.SlotItem{
		"empty":                 nil,
		"13 strung":             load(fourteen()[:13]...),
		"15 strung":             load(append(fourteen(), yewLongbow.Strung)...),
		"14 with an unstrung":   load(fourteen(yewLongbow.Unstrung)...),
		"14 with a string left": load(fourteen(yewLongbow.String)...),
	} {
		if loadHeld(p, yewLongbow) {
			t.Errorf("%s is not a finished load", name)
		}
	}
}

// THE OWNER'S CYCLE (6 Oct 2026: "when it banks it should withdraw all bow string, bank the strung bows,
// withdraw all unstrung bows this will save us doing withdraw x inputs"): the pack holds the last load's
// 14 strung bows, so Withdraw-All of the strings fills exactly the 14 free slots, Deposit-All empties
// the others and Withdraw-All of the bows fills them, three clicks, no count dialog.
func TestOwnerCycleSwapsAFinishedLoadForTheNextInThreeClicks(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, strungLoad())
	sim.OpenBank([]actionsim.Stack{{ID: yewLongbow.Unstrung, Count: 500}, {ID: yewLongbow.String, Count: 300}, {ID: yewLongbow.Strung, Count: 20}})
	d := cycleDeps(sim)
	bank := actions.NewBank(sim, d.Components)
	before := len(sim.Sent())
	bows, strs := ownerCycle(d, bank, waiter{d}, yewLongbow)
	if bows != 14 || strs != 14 {
		t.Fatalf("the pack shows %d bows and %d strings, want 14 of each", bows, strs)
	}
	p := sim.Pack()
	if held(p, yewLongbow.Unstrung) != 14 || held(p, yewLongbow.String) != 14 || held(p, yewLongbow.Strung) != 0 || len(p) != 28 {
		t.Fatalf("pack %+v", p)
	}
	b := sim.Bank()
	if held(b, yewLongbow.Strung) != 20+14 || held(b, yewLongbow.Unstrung) != 500-14 || held(b, yewLongbow.String) != 300-14 {
		t.Fatalf("bank %+v", b)
	}
	// Three packets, in the owner's order, all Withdraw-All or Deposit-All (option 4): never a Withdraw-X
	// (option 5) and never a count dialog.
	sent := sim.Sent()[before:]
	var ops []int
	for _, pkt := range sent {
		switch int(pkt[0]) {
		case clientprot.INV_BUTTON4:
			ops = append(ops, int(pkt[1])<<8|int(pkt[2]))
		case clientprot.INV_BUTTON5, clientprot.RESUME_P_COUNTDIALOG:
			t.Fatalf("a count dialog was opened: opcode %d", pkt[0])
		default:
			t.Fatalf("unexpected packet %v", pkt)
		}
	}
	want := []int{int(yewLongbow.String), int(yewLongbow.Strung), int(yewLongbow.Unstrung)}
	if len(ops) != 3 || ops[0] != want[0] || ops[1] != want[1] || ops[2] != want[2] {
		t.Fatalf("the cycle sent objs %v, want %v (strings, strung bows, unstrung bows)", ops, want)
	}
}

// With too little in the bank the cycle is not the way (Bank checks for a batch of each first); were it
// run anyway, the cycle reports what the pack shows (the Withdraw-All of the bows fills every slot the
// few strings left), so the caller sees it fell short and banks the pack.
func TestOwnerCycleReportsAShortLoad(t *testing.T) {
	defer func(old time.Duration) { cycleConfirm = old }(cycleConfirm)
	cycleConfirm = 20 * time.Millisecond
	sim := actionsim.New(actionsim.Objs{}, strungLoad())
	// Only 9 strings in the bank: Withdraw-All of them, bank the strung bows, 14 bows: 9 pairs.
	sim.OpenBank([]actionsim.Stack{{ID: yewLongbow.Unstrung, Count: 500}, {ID: yewLongbow.String, Count: 9}})
	d := cycleDeps(sim)
	bows, strs := ownerCycle(d, actions.NewBank(sim, d.Components), waiter{d}, yewLongbow)
	if strs != 9 || bows != 28-9 {
		t.Fatalf("%d bows, %d strings", bows, strs)
	}
}
