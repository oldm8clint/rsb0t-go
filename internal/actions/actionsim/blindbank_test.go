package actionsim_test

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

// tickWaiter runs one server tick each time the code under test waits: every poll is a tick, so a
// test sees exactly which tick read what.
type tickWaiter struct{ sim *actionsim.Session }

func (w tickWaiter) Delay(time.Duration) bool { w.sim.Tick(); return true }
func (w tickWaiter) DelayUntil(cond func() bool, _ time.Duration) bool {
	for i := 0; i < 50; i++ {
		if cond() {
			return true
		}
		w.sim.Tick()
	}
	return cond()
}
func (w tickWaiter) DelayTicks(n int) bool {
	for i := 0; i < n; i++ {
		w.sim.Tick()
	}
	return true
}

var testBooth = actions.BankBooth{Op: 2, X: 3200, Z: 3200, LocID: 2213}

// blindRig is a ticked simulator standing at a booth, with the bank's side panel already learnt
// from an earlier open (the blind open needs it to name the deposits' component).
func blindRig(t *testing.T, pack, bank []actionsim.Stack) *actionsim.Session {
	t.Helper()
	sim := actionsim.New(fbObjs(), pack)
	sim.OpenBank(bank)
	if side, _ := actions.NewBank(sim, actionsim.Components()).InventorySnapshot(); side != actionsim.SideInv {
		t.Fatalf("side panel resolved to %d", side)
	}
	if err := actions.CloseModal(sim); err != nil {
		t.Fatal(err)
	}
	sim.BoothOpens = true
	sim.SetTicked(true)
	return sim
}

// The booth op is the fifth handled event of its tick (four CLOSE_MODALs ahead of it, one
// message), the bank opens in that tick, and the deposits sent right behind it are read the very
// next tick: two ticks from the click to a banked pack, with no round trip between them and
// nothing refused.
func TestOpenBankBlindDepositsTheTickAfterTheOpen(t *testing.T) {
	pack := []actionsim.Stack{
		{ID: fbLog, Count: 1}, {ID: fbLog, Count: 1}, {ID: fbOak, Count: 1}, {ID: fbBones, Count: 1},
		{ID: fbHide, Count: 1}, {ID: fbOre, Count: 1}, {ID: fbBar, Count: 1}, {ID: fbRune, Count: 40},
	}
	sim := blindRig(t, pack, []actionsim.Stack{{ID: fbGem, Count: 3}})
	ids := []int32{fbLog, fbOak, fbBones, fbHide, fbOre, fbBar, fbRune}
	o := actions.OpenBankBlind(sim, actionsim.Components(), testBooth, ids, tickWaiter{sim}, time.Second)
	if o.Bank == nil || !o.Blind || !o.Fresh {
		t.Fatalf("open = %s (bank %v, blind %v, fresh %v)", o, o.Bank != nil, o.Blind, o.Fresh)
	}
	if o.Landed != len(ids) || o.Batch.Sent != len(ids) || !o.Batch.OK() {
		t.Fatalf("open = %s, want every kind banked blind and nothing sent again", o)
	}
	if n := sim.Refused(); n != 0 {
		t.Fatalf("the engine refused %d packets", n)
	}
	per := sim.PerTick()
	if len(per) < 3 || per[0] != 5 || per[1] != 5 || per[2] != 2 {
		t.Fatalf("handled per tick = %v, want [5 5 2 ...]: the pads and the booth, then the deposits", per)
	}
	if left := sim.Pack(); len(left) != 0 {
		t.Fatalf("pack = %v, want empty", left)
	}
	if !o.Bank.IsOpen() || actions.CountByID(o.Bank.Items(), fbLog) != 2 {
		t.Fatalf("the bank reading after the open is not fresh: %v", o.Bank.Items())
	}
}

// A stray handled event already waiting in the engine's buffer pushes the booth op into the next
// tick, where the deposits behind it are read before the bank opens and refused. The open still
// happens, the deposits are found still in the pack, and they go through the paced batch.
func TestOpenBankBlindFallsBackWhenTheBoothSlipsATick(t *testing.T) {
	sim := blindRig(t, []actionsim.Stack{{ID: fbLog, Count: 1}, {ID: fbOre, Count: 1}}, nil)
	if err := sim.Send(func(w *packet.Packet) { w.P1Enc(clientprot.CLOSE_MODAL) }); err != nil {
		t.Fatal(err)
	}
	o := actions.OpenBankBlind(sim, actionsim.Components(), testBooth, []int32{fbLog, fbOre}, tickWaiter{sim}, time.Second)
	if o.Bank == nil || !o.Blind {
		t.Fatalf("open = %s", o)
	}
	if o.Landed != 0 || sim.Refused() != 2 {
		t.Fatalf("landed %d, refused %d; want the two blind deposits refused", o.Landed, sim.Refused())
	}
	if !o.Batch.OK() || o.Batch.Sent != 4 { // two blind, two again
		t.Fatalf("the paced retry: %s", o.Batch)
	}
	if left := sim.Pack(); len(left) != 0 {
		t.Fatalf("pack = %v, want empty after the retry", left)
	}
}

// With nothing of the deposits in the pack the open is the plain booth op, and the bank comes
// back open and fresh.
func TestOpenBankBlindWithNothingToDepositIsAPlainOpen(t *testing.T) {
	sim := blindRig(t, nil, []actionsim.Stack{{ID: fbGem, Count: 3}})
	o := actions.OpenBankBlind(sim, actionsim.Components(), testBooth, []int32{fbLog}, tickWaiter{sim}, time.Second)
	if o.Bank == nil || o.Blind || !o.Fresh || o.Batch.Sent != 0 {
		t.Fatalf("open = %s", o)
	}
	if per := sim.PerTick(); per[0] != 1 {
		t.Fatalf("handled per tick = %v, want the booth op alone", per)
	}
}

// A whole visit blind: the deposit, a Withdraw-X with its count, note mode on, a noted Withdraw-X
// and note mode off ride behind the booth click, naming the bank slots the last visit's bank
// predicts (compacted, as the open reorganises it). Seven events: the two ticks after the open.
func TestOpenBankBlindOpsWithdrawsFromThePredictedSlots(t *testing.T) {
	// The last visit left a gap (an emptied row) the open closes up: the prediction must too.
	sim := blindRig(t, []actionsim.Stack{{ID: fbLog, Count: 1}, {ID: fbLog, Count: 1}},
		[]actionsim.Stack{{ID: fbGem, Count: 40}, {ID: fbOre, Count: 1}, {ID: fbRune, Count: 900}, {ID: fbShark, Count: 100}})
	sim.SetTicked(false)
	bank := actions.NewBank(sim, actionsim.Components())
	sim.OpenBank(sim.Bank())
	if err := bank.WithdrawAll(fbOre); err != nil { // empties row 1: the client now shows a gap there
		t.Fatal(err)
	}
	sim.Remove(fbOre, 1)
	_ = actions.CloseModal(sim)
	sim.SetTicked(true)

	ops := []actions.BankOp{
		actions.DepositAllOp(fbLog),
		actions.WithdrawOp(fbRune, 300),
		actions.NoteModeOp(true),
		actions.WithdrawAsOp(fbShark, 50, fbSharkNt),
		actions.NoteModeOp(false),
	}
	o := actions.OpenBankBlindOps(sim, actionsim.Components(), testBooth, ops, tickWaiter{sim}, time.Second)
	if o.Bank == nil || !o.Blind || !o.Batch.OK() || o.Batch.Sent != 7 {
		t.Fatalf("open = %s", o)
	}
	if n := sim.Refused(); n != 0 {
		t.Fatalf("the engine refused %d packets: the predicted slots were wrong", n)
	}
	if per := sim.PerTick(); len(per) < 3 || per[0] != 5 || per[1] != 5 || per[2] != 2 {
		t.Fatalf("handled per tick = %v, want [5 5 2 ...]", per)
	}
	pack := sim.Pack()
	if countOf(pack, fbLog) != 0 || countOf(pack, fbRune) != 300 || countOf(pack, fbSharkNt) != 50 {
		t.Fatalf("pack = %v", pack)
	}
	if o.Bank.NoteMode() || sim.State().CountDialogOpen() {
		t.Fatalf("note mode %v, count dialog flag %v after the visit", o.Bank.NoteMode(), sim.State().CountDialogOpen())
	}
}

// A bank that changed behind the client's back (here a row added at the front) makes every
// predicted withdrawal slot wrong: the engine refuses them, and the paced retry draws them from the
// bank as it really opened.
func TestOpenBankBlindOpsRetriesWrongPredictions(t *testing.T) {
	sim := blindRig(t, nil, []actionsim.Stack{{ID: fbRune, Count: 900}, {ID: fbShark, Count: 100}})
	sim.SetBank([]actionsim.Stack{{ID: fbGem, Count: 7}, {ID: fbRune, Count: 900}, {ID: fbShark, Count: 100}})
	o := actions.OpenBankBlindOps(sim, actionsim.Components(), testBooth,
		[]actions.BankOp{actions.WithdrawOp(fbRune, 30), actions.WithdrawOp(fbShark, 3)}, tickWaiter{sim}, time.Second)
	if o.Bank == nil || !o.Blind || !o.Batch.OK() {
		t.Fatalf("open = %s", o)
	}
	if sim.Refused() == 0 || o.Landed != 0 {
		t.Fatalf("refused %d, landed %d: want the blind withdrawals refused", sim.Refused(), o.Landed)
	}
	if pack := sim.Pack(); countOf(pack, fbRune) != 30 || countOf(pack, fbShark) != 3 {
		t.Fatalf("pack = %v after the retry", pack)
	}
}
