package actionsim_test

import (
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// Object ids for the fast bank tests: a few unstackable items, a stackable rune, and a note pair.
const (
	fbLog     = 1511
	fbOak     = 1521
	fbBones   = 526
	fbHide    = 1739
	fbOre     = 436
	fbBar     = 2349
	fbGem     = 1623
	fbFish    = 317
	fbRune    = 556
	fbShark   = 385
	fbSharkNt = 386
	fbChisel  = 1755
)

func fbObjs() actionsim.Objs {
	return actionsim.Objs{
		Stackable: func(id int32) bool { return id == fbRune },
		NoteOf: func(id int32) int32 {
			if id == fbShark {
				return fbSharkNt
			}
			return 0
		},
		UnnoteOf: func(id int32) int32 {
			if id == fbSharkNt {
				return fbShark
			}
			return 0
		},
	}
}

// realWaiter sleeps for real: the ticker goroutine is the server.
type realWaiter struct{}

func (realWaiter) Delay(d time.Duration) bool { time.Sleep(d); return true }
func (realWaiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for !cond() {
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
	return true
}
func (realWaiter) DelayTicks(n int) bool {
	time.Sleep(time.Duration(n) * 20 * time.Millisecond)
	return true
}

// runTicks plays the server: a tick every period until the test ends.
func runTicks(t *testing.T, sim *actionsim.Session, period time.Duration) {
	t.Helper()
	sim.SetTicked(true)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tk := time.NewTicker(period)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				sim.Tick()
			}
		}
	}()
	t.Cleanup(func() { close(stop); wg.Wait() })
}

func countOf(rows []actionsim.Stack, id int32) int {
	n := 0
	for _, r := range rows {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

// opcodes is the first byte of every packet sent.
func opcodes(sent [][]byte) []int {
	out := make([]int, 0, len(sent))
	for _, b := range sent {
		if len(b) > 0 {
			out = append(out, int(b[0]))
		}
	}
	return out
}

// The whole visit goes out in bursts of at most five user events a tick: eight Deposit-Alls, a
// Withdraw-All and a Withdraw-X with its count are eleven events, three bursts, never more than
// five waiting when a tick begins, and the Withdraw-X's count is never split from its button.
func TestBatchSendsAtMostFiveUserEventsATick(t *testing.T) {
	pack := []actionsim.Stack{
		{ID: fbLog, Count: 1}, {ID: fbLog, Count: 1}, {ID: fbOak, Count: 1}, {ID: fbBones, Count: 1},
		{ID: fbHide, Count: 1}, {ID: fbOre, Count: 1}, {ID: fbBar, Count: 1}, {ID: fbGem, Count: 1},
		{ID: fbFish, Count: 1},
	}
	sim := actionsim.New(fbObjs(), pack)
	sim.OpenBank([]actionsim.Stack{{ID: fbRune, Count: 900}, {ID: fbShark, Count: 50}})
	runTicks(t, sim, 15*time.Millisecond)

	bank := actions.NewBank(sim, actionsim.Components())
	ops := []actions.BankOp{actions.WithdrawAllOp(fbRune), actions.WithdrawOp(fbShark, 13)}
	for _, id := range []int32{fbLog, fbOak, fbBones, fbHide, fbOre, fbBar, fbGem, fbFish} {
		ops = append(ops, actions.DepositAllOp(id))
	}
	r := bank.Batch(ops, realWaiter{}, actions.BatchOptions{Close: true})
	if !r.OK() {
		t.Fatalf("visit not OK: %s", r)
	}
	if r.Sent != 11 || r.Chunks != 3 {
		t.Fatalf("sent %d in %d bursts, want 11 in 3", r.Sent, r.Chunks)
	}
	if got := sim.MaxBacklog(); got > actions.BankMaxPerTick {
		t.Fatalf("%d packets were waiting when a tick began, want at most %d", got, actions.BankMaxPerTick)
	}
	for i, n := range sim.PerTick() {
		if n > actionsim.UserEventsPerTick {
			t.Fatalf("tick %d read %d packets", i, n)
		}
	}
	got := sim.Pack()
	if len(got) != 14 || countOf(got, fbRune) != 900 || countOf(got, fbShark) != 13 {
		t.Fatalf("pack after the visit = %v, want 900 runes and 13 sharks only", got)
	}
	if countOf(sim.Bank(), fbLog) != 2 || countOf(sim.Bank(), fbShark) != 37 {
		t.Fatalf("bank after the visit = %v", sim.Bank())
	}
	if !r.Closed || bank.IsOpen() {
		t.Fatalf("bank not closed after the visit (Closed=%v)", r.Closed)
	}
	// The confirm read the bank before the close: it shows the deposits, which a bank closed in
	// the same tick would never have sent.
	if actions.CountByID(r.Bank, fbLog) != 2 {
		t.Fatalf("the confirm's bank reading = %v, want the deposited logs in it", r.Bank)
	}
	// Deposits first, then the Withdraw-All, then INV_BUTTON5 and its count back to back.
	ops2 := opcodes(sim.Sent())
	x := -1
	for i, op := range ops2 {
		if op == clientprot.INV_BUTTON5 {
			x = i
		}
	}
	if x < 0 || x+1 >= len(ops2) || ops2[x+1] != clientprot.RESUME_P_COUNTDIALOG {
		t.Fatalf("Withdraw-X not followed by its count: %v", ops2)
	}
	if sim.State().CountDialogOpen() {
		t.Fatalf("the blind-answered count dialog left the client's dialog flag set")
	}
}

// A withdrawal into a pack without room, and one of an object the bank does not hold, come back
// as shortfalls (the second without sending anything); the rest of the visit still happens.
func TestBatchReportsWhatFellShort(t *testing.T) {
	pack := make([]actionsim.Stack, 0, 28)
	for i := 0; i < 26; i++ {
		pack = append(pack, actionsim.Stack{ID: fbChisel, Count: 1})
	}
	pack = append(pack, actionsim.Stack{ID: fbFish, Count: 1})
	sim := actionsim.New(fbObjs(), pack)
	sim.OpenBank([]actionsim.Stack{{ID: fbGem, Count: 40}})
	bank := actions.NewBank(sim, actionsim.Components())
	r := bank.Batch([]actions.BankOp{
		actions.DepositAllOp(fbFish),
		actions.WithdrawOp(fbGem, 3),
		actions.WithdrawAllOp(fbOre),
	}, realWaiter{}, actions.BatchOptions{ConfirmTimeout: 500 * time.Millisecond})
	if r.OK() || r.Err != nil {
		t.Fatalf("want a short visit, got %s", r)
	}
	gem, ok := r.ShortOf(actions.BankOpWithdraw, fbGem)
	if !ok || gem.Want != 3 || gem.Got != 2 || gem.Why != "the pack filled" {
		t.Fatalf("gem shortfall = %+v (found %v), want 2 of 3, the pack filled", gem, ok)
	}
	ore, ok := r.ShortOf(actions.BankOpWithdraw, fbOre)
	if !ok || ore.Why != "the bank holds none" {
		t.Fatalf("ore shortfall = %+v (found %v)", ore, ok)
	}
	if _, ok := r.ShortOf(actions.BankOpDeposit, fbFish); ok {
		t.Fatalf("the deposit was reported short: %s", r)
	}
	if r.Sent != 3 { // the deposit and the Withdraw-X pair; nothing for the ore
		t.Fatalf("sent %d user events, want 3", r.Sent)
	}
}

// dropFirst passes every packet to the simulator except the first INV_BUTTON4 for one object,
// standing in for an operation the engine silently refused (a stale slot, a delayed player).
type dropFirst struct {
	*actionsim.Session
	obj     int32
	dropped bool
}

func (d *dropFirst) Send(build func(w *packet.Packet)) error {
	p := packet.NewWriter(64)
	build(p)
	b := p.Data[:p.Pos]
	if !d.dropped && len(b) >= 3 && int(b[0]) == clientprot.INV_BUTTON4 && int32(int(b[1])<<8|int(b[2])) == d.obj {
		d.dropped = true
		return nil
	}
	return d.Session.Send(build)
}

// A deposit the server dropped is reported short by the batch, and the drop-in
// BatchDepositInventory then falls back to the old one-at-a-time deposit for it.
func TestBatchDepositFallsBackToTheOldPath(t *testing.T) {
	sim := actionsim.New(fbObjs(), []actionsim.Stack{{ID: fbLog, Count: 1}, {ID: fbOre, Count: 1}, {ID: fbRune, Count: 30}})
	sim.OpenBank(nil)
	runTicks(t, sim, 10*time.Millisecond)
	s := &dropFirst{Session: sim, obj: fbOre}
	bank := actions.NewBank(s, actionsim.Components())

	r := bank.Batch([]actions.BankOp{actions.DepositAllOp(fbLog), actions.DepositAllOp(fbOre), actions.DepositAllOp(fbRune)}, realWaiter{}, actions.BatchOptions{})
	if sh, ok := r.ShortOf(actions.BankOpDeposit, fbOre); !ok || sh.Why != "the pack still holds it" {
		t.Fatalf("the dropped deposit was not reported: %s", r)
	}
	if len(r.Short) != 1 {
		t.Fatalf("want only the ore short: %s", r)
	}

	s.dropped = false // drop the next one again: the drop-in must fall back for it
	sim.Give(fbBones, 1)
	time.Sleep(30 * time.Millisecond)
	if err := bank.BatchDepositInventory(realWaiter{}); err != nil {
		t.Fatalf("BatchDepositInventory: %v", err)
	}
	if left := sim.Pack(); len(left) != 0 {
		t.Fatalf("pack after the fallback = %v, want empty", left)
	}
	if countOf(sim.Bank(), fbOre) != 1 || countOf(sim.Bank(), fbBones) != 1 {
		t.Fatalf("bank = %v", sim.Bank())
	}
}

// QuickWithdrawX sends the count right behind the button, with no wait for the dialog, and the
// note-mode switches ride in the same burst.
func TestQuickWithdrawXAndNotesInOneBurst(t *testing.T) {
	sim := actionsim.New(fbObjs(), nil)
	sim.OpenBank([]actionsim.Stack{{ID: fbShark, Count: 400}, {ID: fbRune, Count: 77}})
	bank := actions.NewBank(sim, actionsim.Components())
	if err := bank.QuickWithdrawX(fbRune, 33, realWaiter{}); err != nil {
		t.Fatalf("QuickWithdrawX: %v", err)
	}
	if countOf(sim.Pack(), fbRune) != 33 {
		t.Fatalf("pack = %v, want 33 runes", sim.Pack())
	}
	r := bank.Batch([]actions.BankOp{actions.NoteModeOp(true), actions.WithdrawAsOp(fbShark, 300, fbSharkNt), actions.NoteModeOp(false)}, realWaiter{}, actions.BatchOptions{})
	if !r.OK() || r.Chunks != 1 || r.Sent != 4 {
		t.Fatalf("noted withdrawal: %s", r)
	}
	if countOf(sim.Pack(), fbSharkNt) != 300 || bank.NoteMode() {
		t.Fatalf("pack = %v, note mode %v; want 300 noted sharks and notes off", sim.Pack(), bank.NoteMode())
	}
	if err := bank.QuickWithdrawX(fbGem, 2, realWaiter{}); err != actions.ErrItemNotFound {
		t.Fatalf("QuickWithdrawX of a missing row = %v, want ErrItemNotFound", err)
	}
}

// AwaitOpen does not take an open bank for a fresh one until the open's own inventories arrive:
// the client keeps the previous visit's.
func TestAwaitOpenWaitsForTheOpensInventories(t *testing.T) {
	sim := actionsim.New(fbObjs(), []actionsim.Stack{{ID: fbLog, Count: 1}})
	sim.OpenBank([]actionsim.Stack{{ID: fbRune, Count: 5}})
	bank := actions.NewBank(sim, actionsim.Components())
	if _, inv := bank.InventorySnapshot(); inv == nil {
		t.Fatal("no side panel on the first visit")
	}
	_ = bank.Close()

	mark := actions.MarkBankOpen(sim)
	// The modal alone, as if IF_OPENMAIN_SIDE had been handled and the inventories not yet.
	p := packet.NewWriter(8)
	p.P2(actions.BankMainModal)
	p.P2(actionsim.SideRoot)
	sim.State().Handle(serverprot.IF_OPENMAIN_SIDE, p.Data[:p.Pos])
	fresh := actions.NewBank(sim, actionsim.Components())
	if fresh.OpenedFresh(mark) {
		t.Fatal("an open bank showing the last visit's inventories counted as fresh")
	}
	sim.OpenBank([]actionsim.Stack{{ID: fbRune, Count: 5}}) // the open's inventories land
	open, isFresh := fresh.AwaitOpen(mark, realWaiter{}, time.Second)
	if !open || !isFresh {
		t.Fatalf("AwaitOpen = %v, %v; want open and fresh", open, isFresh)
	}
}

// A booth op in ticked mode: the bank opens in the tick the op is read and AwaitOpen sees it.
func TestAwaitOpenAfterABoothOp(t *testing.T) {
	sim := actionsim.New(fbObjs(), nil)
	sim.SetBank([]actionsim.Stack{{ID: fbRune, Count: 5}})
	sim.BoothOpens = true
	runTicks(t, sim, 10*time.Millisecond)
	mark := actions.MarkBankOpen(sim)
	if err := actions.OpLoc(sim, 2, 3200, 3200, 2213); err != nil {
		t.Fatal(err)
	}
	bank := actions.NewBank(sim, actionsim.Components())
	start := time.Now()
	open, fresh := bank.AwaitOpen(mark, realWaiter{}, 2*time.Second)
	if !open || !fresh {
		t.Fatalf("AwaitOpen = %v, %v after %s", open, fresh, time.Since(start))
	}
	if len(bank.Items()) != 1 {
		t.Fatalf("bank items = %v", bank.Items())
	}
}
