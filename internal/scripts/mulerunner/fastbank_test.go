package mulerunner

import (
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/orderplan"
)

// tickedSim starts the simulator's server ticks (every period) for the test's lifetime.
func tickedSim(t *testing.T, sim *actionsim.Session, period time.Duration) {
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

func realDeps(sim *actionsim.Session) *Deps {
	return &Deps{Session: sim, Components: actionsim.Components(), Log: func(string) {},
		Delay: func(d time.Duration) bool { time.Sleep(d); return true }}
}

// The mule's pack is banked in bursts of at most five Deposit-Alls a tick, confirmed once, and the
// OnBankClose hook still reads the bank fresh: the bank is closed only after the confirm, so its
// final contents reached the client before the close stopped them.
func TestDepositKindsFastBanksThePackFiveATickAndPublishesTheFreshBank(t *testing.T) {
	ids := []int32{1511, 1521, 526, 1739, 436, 2349, 1623}
	var pack []actionsim.Stack
	for _, id := range ids {
		pack = append(pack, actionsim.Stack{ID: id, Count: 1}, actionsim.Stack{ID: id, Count: 1})
	}
	sim := actionsim.New(actionsim.Objs{}, pack)
	sim.OpenBank([]actionsim.Stack{{ID: 995, Count: 1000}})
	tickedSim(t, sim, 15*time.Millisecond)
	d := realDeps(sim)
	var published []gamestate.SlotItem
	bank := &lockedBank{Bank: actions.NewBank(sim, d.Components), unlock: func() {}, onClose: func(items []gamestate.SlotItem) { published = items }}

	if n := depositKindsFast(d, bank, ids); n != len(ids) {
		t.Fatalf("banked %d kinds, want %d", n, len(ids))
	}
	if left := sim.Pack(); len(left) != 0 {
		t.Fatalf("pack after banking = %v, want empty", left)
	}
	if got := sim.MaxBacklog(); got > actions.BankMaxPerTick {
		t.Fatalf("%d packets were waiting when a tick began, want at most %d", got, actions.BankMaxPerTick)
	}
	_ = bank.Close()
	for _, id := range ids {
		if actions.CountByID(published, id) != 2 {
			t.Fatalf("OnBankClose published %v, want two of every banked kind", published)
		}
	}
}

// The whole kit -- the pack's own items banked, two Withdraw-X lines, note mode on, a noted line,
// note mode off -- goes out in one burst of at most five user events a tick, each Withdraw-X's count
// right behind its button.
func TestDrawKitIsOneBurst(t *testing.T) {
	cfg := kitConfig(t)
	sim := actionsim.New(simObjs(cfg), []actionsim.Stack{{ID: kitBones, Count: 1}, {ID: kitBones, Count: 1}})
	sim.OpenBank([]actionsim.Stack{{ID: kitCake, Count: 20}, {ID: kitFireRune, Count: 500}, {ID: kitShark, Count: 10}})
	tickedSim(t, sim, 15*time.Millisecond)
	d := realDeps(sim)
	d.Config = cfg
	bank := &lockedBank{Bank: actions.NewBank(sim, d.Components), unlock: func() {}}
	lines := []orderplan.KitLineDecision{
		{Item: "Fire rune", Asked: 100, Qty: 100},
		{Item: "Cake", Asked: 10, Qty: 10, Noted: true},
		{Item: "Shark", Asked: 3, Qty: 3},
	}
	start := time.Now()
	got, offers, stopped := drawKit(d, bank, "hofdon", lines)
	took := time.Since(start)
	if stopped {
		t.Fatal("stopped")
	}
	noteCake, _ := noteFor(cfg, kitCake)
	pack := sim.Pack()
	if held(pack, kitBones) != 0 || held(pack, kitFireRune) != 100 || held(pack, noteCake) != 10 || held(pack, kitShark) != 3 {
		t.Fatalf("pack %+v, want 100 fire runes, 10 noted cake, 3 sharks and no bones", pack)
	}
	for i := range got {
		if got[i].Qty != lines[i].Qty || got[i].Reason != "" {
			t.Fatalf("line %d = %+v", i, got[i])
		}
	}
	if offers[1].id != noteCake || offers[1].qty != 10 {
		t.Fatalf("offers %+v", offers)
	}
	if sim.State().Varp(actions.BankCertVarp) != 0 {
		t.Fatal("the bank was left in note mode")
	}
	if b := sim.MaxBacklog(); b > actions.BankMaxPerTick {
		t.Fatalf("%d packets waiting at a tick, want at most %d", b, actions.BankMaxPerTick)
	}
	sent := sim.Sent()
	for i, b := range sent {
		if len(b) > 0 && int(b[0]) == clientprot.INV_BUTTON5 && (i+1 >= len(sent) || int(sent[i+1][0]) != clientprot.RESUME_P_COUNTDIALOG) {
			t.Fatalf("a Withdraw-X was not followed by its count")
		}
	}
	busy := 0
	for _, n := range sim.PerTick() {
		if n > 0 {
			busy++
		}
	}
	if busy > 2 {
		t.Fatalf("the kit took %d server ticks of packets, want 2 (9 user events)", busy)
	}
	t.Logf("kit drawn in %s (%d ticks at 15 ms)", took.Round(time.Millisecond), len(sim.PerTick()))
}

// mtickWaiter runs one server tick each time the code under test waits.
type mtickWaiter struct{ sim *actionsim.Session }

func (w mtickWaiter) Delay(time.Duration) bool { w.sim.Tick(); return true }
func (w mtickWaiter) DelayUntil(cond func() bool, _ time.Duration) bool {
	for i := 0; i < 50 && !cond(); i++ {
		w.sim.Tick()
	}
	return cond()
}
func (w mtickWaiter) DelayTicks(n int) bool {
	for i := 0; i < n; i++ {
		w.sim.Tick()
	}
	return true
}

// A kit decided from the bank as the mule last read it rides behind the booth click: the pack
// banked, the unnoted lines, note mode on, the noted cake, note mode off, all read in the ticks
// right after the open, nothing refused, and the pack holds exactly the kit (the exactness
// HandoutKit checks before it skips the paced draw).
func TestABlindKitDrawIsExactInTheTicksAfterTheOpen(t *testing.T) {
	cfg := kitConfig(t)
	sim := actionsim.New(simObjs(cfg), []actionsim.Stack{{ID: kitBones, Count: 1}, {ID: kitBones, Count: 1}})
	bankRows := []actionsim.Stack{{ID: kitCake, Count: 20}, {ID: kitFireRune, Count: 500}, {ID: kitShark, Count: 10}}
	sim.OpenBank(bankRows) // the mule's last visit: the bank it read, and the side panel it learnt
	if side, _ := actions.NewBank(sim, actionsim.Components()).InventorySnapshot(); side < 0 {
		t.Fatal("no side panel")
	}
	_ = actions.CloseModal(sim)
	sim.BoothOpens = true
	sim.SetTicked(true)
	d := simDeps(sim, cfg)
	req := KitRequest{To: "acct1", Lines: []orderplan.KitLine{{Item: "Fire rune", Qty: 100}, {Item: "Cake", Qty: 10}, {Item: "Shark", Qty: 3}},
		Room: 20, Ledger: orderplan.NewLedger(time.Hour),
		Policy: orderplan.Policy{Allowed: []string{"acct1"}, Open: orderplan.OpenStock{Enabled: true, Max: 1000, WindowMax: 100000}, MaxOrdersPerDonor: 1, MaxUnitsPerItem: 100000}}
	plan := planBlindKit(d, req)
	if plan == nil {
		t.Fatal("no blind plan from the mule's last bank reading")
	}
	o := actions.OpenBankBlindOps(sim, actionsim.Components(), actions.BankBooth{Op: 2, X: 2809, Z: 3442, LocID: 2213}, plan.ops, mtickWaiter{sim}, time.Second)
	if o.Bank == nil || !o.Blind || !o.Batch.OK() {
		t.Fatalf("open = %s", o)
	}
	t.Logf("%s fresh=%v", o, o.Fresh)
	if n := sim.Refused(); n != 0 {
		t.Fatalf("the engine refused %d packets", n)
	}
	for i, l := range plan.lines {
		if l.Qty > 0 && countByID(o.Bank.Inventory(), plan.offers[i].id) != l.Qty {
			t.Fatalf("line %+v: the pack holds %d of id %d", l, countByID(o.Bank.Inventory(), plan.offers[i].id), plan.offers[i].id)
		}
	}
	if held(sim.Pack(), kitBones) != 0 || o.Bank.NoteMode() {
		t.Fatalf("pack %+v, note mode %v", sim.Pack(), o.Bank.NoteMode())
	}
	if per := sim.PerTick(); per[0] != 5 {
		t.Fatalf("handled per tick = %v: the booth op was not the fifth event of its tick", per)
	}
}
