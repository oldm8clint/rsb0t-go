package actionsim_test

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/clientprot"
)

// Two actions a tick (twopertick-1012): dropping several slots in one burst, and a whole bank visit through
// Bank.Visit (Batch with the one-at-a-time path behind it).

const dropBall = 1759

func dropObjs() eqTypes {
	return eqTypes{
		dropBall: cachecfg.Obj{ID: dropBall, Name: "Ball of wool", IOps: [5]string{"", "", "", "", ""}},
		eqJunk:   cachecfg.Obj{ID: eqJunk, Name: "Bucket", IOps: [5]string{"Fill"}},
	}
}

func dropSim(t *testing.T, balls int) (*actionsim.Session, *actions.Equipment) {
	t.Helper()
	var pack []actionsim.Stack
	for i := 0; i < balls; i++ {
		pack = append(pack, actionsim.Stack{ID: dropBall, Count: 1})
		if i == 3 {
			pack = append(pack, actionsim.Stack{ID: eqJunk, Count: 1})
		}
	}
	sim := actionsim.New(actionsim.Objs{}, pack)
	sim.ShowTabs()
	return sim, actions.NewEquipment(sim, actionsim.TabComponents())
}

func ballSlots(eq *actions.Equipment) []gamestate.SlotItem {
	var out []gamestate.SlotItem
	for _, it := range eq.Inventory() {
		if it.ID == dropBall {
			out = append(out, it)
		}
	}
	return out
}

// Twelve balls of wool are 5 + 5 + 2 Drop clicks over three ticks, not twelve round trips; the bucket between
// them stays.
func TestDropManyDropsFiveATick(t *testing.T) {
	sim, eq := dropSim(t, 12)
	runTicks(t, sim, 30*time.Millisecond)

	r := eq.DropMany(dropObjs(), ballSlots(eq), realWaiter{})
	if r.Dropped() != 12 || r.Left != 0 || r.Retried != 0 {
		t.Fatalf("batch = %+v", r)
	}
	if r.Bursts != 3 || r.Sent != 12 {
		t.Fatalf("%d click(s) in %d burst(s), want 12 in 3", r.Sent, r.Bursts)
	}
	most, ticks := maxPerTick(sim)
	if most != 5 || ticks != 3 {
		t.Fatalf("per tick %v, want ticks of 5, 5, 2", sim.PerTick())
	}
	if got := sim.MaxBacklog(); got > actionsim.UserEventsPerTick {
		t.Fatalf("%d packets waiting when a tick began", got)
	}
	if got := sim.Pack(); len(got) != 1 || got[0].ID != eqJunk {
		t.Fatalf("pack = %v, want only the bucket", got)
	}
	if len(sim.Ground()) != 12 {
		t.Fatalf("ground = %v", sim.Ground())
	}
	t.Log(r.Log())
}

// Clicks the server refused (the player was delayed) are dropped again one at a time once it is over.
func TestDropManyRetriesWhatWasRefused(t *testing.T) {
	sim, eq := dropSim(t, 4)
	runTicks(t, sim, 30*time.Millisecond)
	sim.Delay(2)

	r := eq.DropMany(dropObjs(), ballSlots(eq), clampWaiter{})
	if r.Dropped() != 4 || r.Left != 0 {
		t.Fatalf("batch = %+v", r)
	}
	if r.Retried == 0 {
		t.Fatalf("batch = %+v, want the refused clicks on the retry", r)
	}
	if got := sim.Pack(); len(got) != 1 {
		t.Fatalf("pack = %v", got)
	}
	t.Log(r.Log())
}

// A slot that no longer holds the named item is skipped; an item with no Drop option is left.
func TestDropManySkipsWhatIsGone(t *testing.T) {
	sim, eq := dropSim(t, 3)
	runTicks(t, sim, 30*time.Millisecond)
	slots := ballSlots(eq)
	slots = append(slots, gamestate.SlotItem{Slot: 20, Item: gamestate.Item{ID: dropBall, Count: 1}}) // nothing there
	r := eq.DropMany(dropObjs(), slots, realWaiter{})
	if r.Wanted != 4 || r.Sent != 3 || r.Dropped() != 4 {
		t.Fatalf("batch = %+v", r)
	}
}

// ---- Bank.Visit ----

// A visit of three deposits, a Withdraw-1 and a Withdraw-All is five user events in ONE tick, confirmed once.
func TestVisitIsOneTickOfFiveEvents(t *testing.T) {
	pack := []actionsim.Stack{{ID: fbFish, Count: 1}, {ID: fbFish, Count: 1}, {ID: fbBones, Count: 1}, {ID: fbHide, Count: 1}}
	sim := actionsim.New(fbObjs(), pack)
	sim.OpenBank([]actionsim.Stack{{ID: fbChisel, Count: 1}, {ID: fbLog, Count: 1}, {ID: fbLog, Count: 1}, {ID: fbOak, Count: 1}})
	runTicks(t, sim, 30*time.Millisecond)

	bank := actions.NewBank(sim, actionsim.Components())
	ops := []actions.BankOp{
		actions.DepositAllOp(fbFish), actions.DepositAllOp(fbBones), actions.DepositAllOp(fbHide),
		actions.WithdrawOp(fbChisel, 1), actions.WithdrawAllOp(fbOak),
	}
	r, retried := bank.Visit(ops, realWaiter{}, actions.BatchOptions{})
	if !r.OK() || retried != 0 {
		t.Fatalf("visit = %s (retried %d)", r, retried)
	}
	if r.Sent != 5 || r.Chunks != 1 {
		t.Fatalf("%d events in %d burst(s), want 5 in 1", r.Sent, r.Chunks)
	}
	if most, ticks := maxPerTick(sim); most != 5 || ticks != 1 {
		t.Fatalf("per tick %v, want 5 events in 1 tick", sim.PerTick())
	}
	if countOf(sim.Pack(), fbChisel) != 1 || countOf(sim.Pack(), fbOak) != 1 || countOf(sim.Pack(), fbFish) != 0 {
		t.Fatalf("pack = %v", sim.Pack())
	}
	t.Log(actions.VisitLog("test", len(ops), r, retried))
}

// The burst is refused (a random event had the player delayed): every operation is redone the old way and the
// visit still ends complete, with the retried count saying so.
func TestVisitRedoesWhatWasRefused(t *testing.T) {
	pack := []actionsim.Stack{{ID: fbFish, Count: 1}, {ID: fbBones, Count: 1}}
	sim := actionsim.New(fbObjs(), pack)
	sim.OpenBank([]actionsim.Stack{{ID: fbChisel, Count: 1}, {ID: fbShark, Count: 30}})
	runTicks(t, sim, 30*time.Millisecond)
	sim.Delay(2)

	bank := actions.NewBank(sim, actionsim.Components())
	ops := []actions.BankOp{actions.DepositAllOp(fbFish), actions.DepositAllOp(fbBones), actions.WithdrawOp(fbChisel, 1), actions.WithdrawOp(fbShark, 7)}
	r, retried := bank.Visit(ops, clampWaiter{}, actions.BatchOptions{ConfirmTimeout: 150 * time.Millisecond, Close: true})
	if !r.OK() {
		t.Fatalf("visit = %s", r)
	}
	if retried != len(ops) {
		t.Fatalf("retried %d, want all %d on the old path", retried, len(ops))
	}
	if countOf(sim.Pack(), fbChisel) != 1 || countOf(sim.Pack(), fbShark) != 7 || countOf(sim.Pack(), fbFish) != 0 {
		t.Fatalf("pack = %v", sim.Pack())
	}
	if bank.IsOpen() || !r.Closed {
		t.Fatal("the bank should be closed after the retries")
	}
}

// What the bank does not hold is not retried (nothing could land).
func TestVisitDoesNotRetryWhatTheBankLacks(t *testing.T) {
	sim := actionsim.New(fbObjs(), []actionsim.Stack{{ID: fbFish, Count: 1}})
	sim.OpenBank([]actionsim.Stack{{ID: fbChisel, Count: 1}})
	runTicks(t, sim, 30*time.Millisecond)
	bank := actions.NewBank(sim, actionsim.Components())
	r, retried := bank.Visit([]actions.BankOp{actions.DepositAllOp(fbFish), actions.WithdrawOp(fbOre, 1)}, realWaiter{}, actions.BatchOptions{ConfirmTimeout: 150 * time.Millisecond})
	if r.OK() || retried != 0 {
		t.Fatalf("visit = %s (retried %d), want the missing ore reported", r, retried)
	}
	if _, ok := r.ShortOf(actions.BankOpWithdraw, fbOre); !ok {
		t.Fatalf("no shortfall for the ore: %+v", r.Short)
	}
	if countOf(sim.Bank(), fbFish) != 1 {
		t.Fatalf("the fish should be banked: %v", sim.Bank())
	}
}

// ---- the hand-off's note draw ----

// Note mode on, three Withdraw-Alls and note mode off are five user events in ONE tick; the shark lands as a
// note (a stack), the rune stack as itself, the bones as items (no note), and the bank is back on items.
func TestDrawAllAsNotesIsOneBurst(t *testing.T) {
	sim := actionsim.New(fbObjs(), nil)
	sim.OpenBank([]actionsim.Stack{{ID: fbShark, Count: 40}, {ID: fbRune, Count: 500}, {ID: fbBones, Count: 10}})
	runTicks(t, sim, 30*time.Millisecond)

	bank := actions.NewBank(sim, actionsim.Components())
	noteOf := func(id int32) int32 {
		if id == fbShark {
			return fbSharkNt
		}
		return id
	}
	r, rows := bank.DrawAllAsNotes([]int32{fbShark, fbRune, fbBones}, noteOf, 27, realWaiter{})
	if !r.OK() || rows != 3 {
		t.Fatalf("draw = %s (%d rows)", r, rows)
	}
	if r.Sent != 5 || r.Chunks != 1 {
		t.Fatalf("%d events in %d burst(s), want 5 in 1", r.Sent, r.Chunks)
	}
	if most, ticks := maxPerTick(sim); most != 5 || ticks != 1 {
		t.Fatalf("per tick %v, want 5 events in 1 tick", sim.PerTick())
	}
	pack := sim.Pack()
	if countOf(pack, fbSharkNt) != 40 || countOf(pack, fbRune) != 500 || countOf(pack, fbBones) != 10 {
		t.Fatalf("pack = %v, want 40 noted sharks, 500 runes and 10 bones", pack)
	}
	if sim.State().Varp(actions.BankCertVarp) != 0 {
		t.Fatal("the bank was left in note mode")
	}
}

// At most maxRows rows are drawn: a pack with two free slots takes two notes, not the whole list.
func TestDrawAllAsNotesStopsAtTheFreeSlots(t *testing.T) {
	sim := actionsim.New(fbObjs(), nil)
	sim.OpenBank([]actionsim.Stack{{ID: fbShark, Count: 40}, {ID: fbRune, Count: 500}, {ID: fbOre, Count: 10}})
	runTicks(t, sim, 30*time.Millisecond)
	bank := actions.NewBank(sim, actionsim.Components())
	r, rows := bank.DrawAllAsNotes([]int32{fbShark, fbRune, fbOre}, nil, 2, realWaiter{})
	if rows != 2 || len(sim.Pack()) != 2 {
		t.Fatalf("draw = %s, %d rows, pack %v; want two rows drawn", r, rows, sim.Pack())
	}
	if countOf(sim.Bank(), fbOre) != 10 {
		t.Fatalf("the third row should stay banked: %v", sim.Bank())
	}
}

// A deposit the server keeps refusing costs a Batch burst and ONE retry click, not DepositAllMatching's 32 rounds.
func TestVisitRetriesARefusedDepositOnce(t *testing.T) {
	sim := actionsim.New(fbObjs(), []actionsim.Stack{{ID: fbFish, Count: 1}})
	sim.OpenBank([]actionsim.Stack{{ID: fbChisel, Count: 1}})
	runTicks(t, sim, 30*time.Millisecond)
	sim.Delay(1000) // every inventory button is refused

	bank := actions.NewBank(sim, actionsim.Components())
	start := time.Now()
	r, retried := bank.Visit([]actions.BankOp{actions.DepositAllOp(fbFish)}, clampWaiter{}, actions.BatchOptions{ConfirmTimeout: 100 * time.Millisecond})
	if r.OK() || retried != 0 {
		t.Fatalf("visit = %s (retried %d), want the deposit still short", r, retried)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("the visit took %s: a refused deposit must not be retried for rounds", took)
	}
	deposits := 0
	for _, b := range sim.Sent() {
		if len(b) > 0 && int(b[0]) == clientprot.INV_BUTTON4 {
			deposits++
		}
	}
	if deposits != 2 {
		t.Fatalf("%d Deposit-All clicks sent, want the burst's one and the retry's one", deposits)
	}
}

// DepositKinds on a bank that is not open judges by what the pack still holds, not by blacklisting every kind.
func TestDepositKindsJudgesByThePackWhenTheVisitCannotRun(t *testing.T) {
	sim := actionsim.New(fbObjs(), []actionsim.Stack{{ID: fbFish, Count: 1}})
	bank := actions.NewBank(sim, actionsim.Components())
	deposited, stuck, r, _ := bank.DepositKinds([]int32{fbFish, fbOre}, realWaiter{})
	if r.Err == nil {
		t.Fatalf("a closed bank should refuse the visit: %s", r)
	}
	// The ore is not in the pack at all, so it is not stuck.
	if len(stuck) != 1 || stuck[0] != fbFish || len(deposited) != 1 || deposited[0] != fbOre {
		t.Fatalf("deposited %v stuck %v", deposited, stuck)
	}
}
