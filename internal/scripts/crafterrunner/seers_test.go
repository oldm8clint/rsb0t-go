package crafterrunner

import (
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/crafterplan"
)

// Two actions a tick (twopertick-1012): the Seers bank visit is one burst, not a Deposit-All and a wait per kind
// and then a Withdraw-All and a wait.

func tickSim(t *testing.T, sim *actionsim.Session, period time.Duration) {
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

func objID(t *testing.T, cfg *cachecfg.Config, name string) int32 {
	t.Helper()
	for _, id := range cfg.ObjIDs(name) {
		if o, ok := cfg.Obj(id); ok && !o.Noted() {
			return int32(id)
		}
	}
	t.Fatalf("no %q in this config", name)
	return 0
}

type seersFixture struct {
	sim                          *actionsim.Session
	d                            *Deps
	bank                         *actions.Bank
	flax, strings_, bucket, logs int32
	logLines                     []string
	mu                           sync.Mutex
}

func newSeers(t *testing.T, ids func(f *seersFixture) ([]actionsim.Stack, []actionsim.Stack)) *seersFixture {
	t.Helper()
	cfg := realConfig(t)
	f := &seersFixture{
		flax:     objID(t, cfg, crafterplan.Flax),
		strings_: objID(t, cfg, crafterplan.BowString),
		bucket:   objID(t, cfg, "Bucket"),
		logs:     objID(t, cfg, "Logs"),
	}
	pk, bk := ids(f)
	objs := actionsim.Objs{Stackable: func(id int32) bool { o, ok := cfg.Obj(int(id)); return ok && o.Stackable }}
	f.sim = actionsim.New(objs, pk)
	f.sim.OpenBank(bk)
	tickSim(t, f.sim, 20*time.Millisecond)
	f.d = &Deps{Session: f.sim, Config: cfg, Components: actionsim.Components(),
		Delay: func(d time.Duration) bool { time.Sleep(d); return true },
		Log:   func(s string) { f.mu.Lock(); f.logLines = append(f.logLines, s); f.mu.Unlock() }}
	f.bank = actions.NewBank(f.sim, f.d.Components)
	return f
}

func (f *seersFixture) logged(sub string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.logLines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func perTickMost(sim *actionsim.Session) (most, ticks int) {
	for _, n := range sim.PerTick() {
		if n > 0 {
			ticks++
		}
		if n > most {
			most = n
		}
	}
	return most, ticks
}

func count(rows []actionsim.Stack, id int32) int {
	n := 0
	for _, r := range rows {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

// Three kinds of leftover are three Deposit-Alls in the one tick, and the count of items that went in is right.
func TestDepositExceptIsOneBurst(t *testing.T) {
	f := newSeers(t, func(f *seersFixture) ([]actionsim.Stack, []actionsim.Stack) {
		return []actionsim.Stack{{ID: f.strings_, Count: 14}, {ID: f.bucket, Count: 1}, {ID: f.bucket, Count: 1}, {ID: f.logs, Count: 1}},
			[]actionsim.Stack{{ID: f.flax, Count: 500}}
	})
	n := DepositExcept(f.d, f.bank, nil)
	if n != 17 {
		t.Fatalf("deposited %d items, want 17", n)
	}
	if most, ticks := perTickMost(f.sim); most != 3 || ticks != 1 {
		t.Fatalf("per tick %v, want 3 events in 1 tick", f.sim.PerTick())
	}
	if got := f.sim.Pack(); len(got) != 0 {
		t.Fatalf("pack = %v, want empty", got)
	}
	if count(f.sim.Bank(), f.strings_) != 14 || count(f.sim.Bank(), f.bucket) != 2 {
		t.Fatalf("bank = %v", f.sim.Bank())
	}
	if !f.logged("crafter deposit: 3 operation(s), 3 user event(s) in 1 tick(s)") {
		t.Fatalf("log lines %q", f.logLines)
	}
}

// DrawFlax: the deposit of the leftovers and the Withdraw-All of flax are ONE burst, and the flax in the pack is
// counted from before it.
func TestDrawFlaxDepositsAndDrawsInOneBurst(t *testing.T) {
	f := newSeers(t, func(f *seersFixture) ([]actionsim.Stack, []actionsim.Stack) {
		return []actionsim.Stack{{ID: f.bucket, Count: 1}, {ID: f.logs, Count: 1}},
			[]actionsim.Stack{{ID: f.flax, Count: 27}}
	})
	got, counts, ok := drawFlaxAt(f.d, f.bank)
	if !ok || got != 27 || counts.Flax != 0 {
		t.Fatalf("got %d flax, counts %+v, ok %v", got, counts, ok)
	}
	if most, ticks := perTickMost(f.sim); most != 3 || ticks != 1 {
		t.Fatalf("per tick %v, want 2 deposits + 1 withdraw = 3 events in 1 tick", f.sim.PerTick())
	}
	if count(f.sim.Pack(), f.flax) != 27 || len(f.sim.Pack()) != 27 {
		t.Fatalf("pack = %v", f.sim.Pack())
	}
	if !f.logged("crafter deposit and draw flax") {
		t.Fatalf("log lines %q", f.logLines)
	}
}

// The strings round: bank the strings AND draw the flax in the same visit when want says so; and when it does
// not (a hand-over is due), only the deposit happens.
func TestBankAllDrawFlaxIsOneVisit(t *testing.T) {
	mk := func(t *testing.T) *seersFixture {
		return newSeers(t, func(f *seersFixture) ([]actionsim.Stack, []actionsim.Stack) {
			var pack []actionsim.Stack
			pack = append(pack, actionsim.Stack{ID: f.strings_, Count: 27})
			return pack, []actionsim.Stack{{ID: f.flax, Count: 300}, {ID: f.strings_, Count: 100}}
		})
	}
	f := mk(t)
	var seen BankCounts
	deposited, got, counts, ok := bankAllDrawFlaxAt(f.d, f.bank, crafterplan.SeersKeep(), func(opened BankCounts) bool { seen = opened; return true })
	if !ok || deposited != 27 || got != 28 {
		t.Fatalf("deposited %d drew %d (ok %v), want 27 banked and a full pack of 28 flax drawn", deposited, got, ok)
	}
	if seen.Flax != 300 || seen.Strings != 100 {
		t.Fatalf("want saw the counts as the bank opened: %+v", seen)
	}
	if counts.Strings != 127 || counts.Flax != 272 {
		t.Fatalf("counts after = %+v, want 127 strings and 272 flax", counts)
	}
	if most, ticks := perTickMost(f.sim); most != 2 || ticks != 1 {
		t.Fatalf("per tick %v, want a deposit and a withdraw in 1 tick", f.sim.PerTick())
	}

	g := mk(t)
	deposited, got, counts, ok = bankAllDrawFlaxAt(g.d, g.bank, crafterplan.SeersKeep(), func(BankCounts) bool { return false })
	if !ok || deposited != 27 || got != 0 || counts.Flax != 300 {
		t.Fatalf("no draw wanted: deposited %d drew %d counts %+v", deposited, got, counts)
	}
	if len(g.sim.Pack()) != 0 {
		t.Fatalf("pack = %v", g.sim.Pack())
	}
}

// Twelve balls of wool are dropped five a tick (5 + 5 + 2), not twelve round trips, and the shears stay.
func TestDropDropsFiveATick(t *testing.T) {
	cfg := realConfig(t)
	ball, shears := objID(t, cfg, "Ball of wool"), objID(t, cfg, "Shears")
	var pack []actionsim.Stack
	for i := 0; i < 12; i++ {
		pack = append(pack, actionsim.Stack{ID: ball, Count: 1})
		if i == 2 {
			pack = append(pack, actionsim.Stack{ID: shears, Count: 1})
		}
	}
	sim := actionsim.New(actionsim.Objs{}, pack)
	sim.ShowTabs()
	tickSim(t, sim, 20*time.Millisecond)
	var lines []string
	d := &Deps{Session: sim, Config: cfg, Components: actionsim.TabComponents(),
		Delay: func(d time.Duration) bool { time.Sleep(d); return true },
		Log:   func(s string) { lines = append(lines, s) }}

	if n := Drop(d, []string{"Ball of wool"}); n != 12 {
		t.Fatalf("dropped %d, want 12 (log %q)", n, lines)
	}
	most, ticks := perTickMost(sim)
	if most != 5 || ticks != 3 {
		t.Fatalf("per tick %v, want ticks of 5, 5 and 2", sim.PerTick())
	}
	if got := sim.Pack(); len(got) != 1 || got[0].ID != shears {
		t.Fatalf("pack = %v, want only the shears", got)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "drop: 12 of 12 slot(s) in 3 tick(s)") {
		t.Fatalf("log lines %q", lines)
	}
}
