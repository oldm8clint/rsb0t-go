package woodcutrunner

// Two actions a tick (twopertick-1012): the Seers deposit is one burst, not a Deposit-All and a wait per kind.

import (
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/woodcutplan"
)

func TestDepositAllExceptIsOneBurstAndKeepsTheAxe(t *testing.T) {
	cfg := axeConfig(t)
	axe, logs, oak, bucket := objNamed(t, cfg, woodcutplan.RuneAxe), objNamed(t, cfg, "Yew logs"), objNamed(t, cfg, "Oak logs"), objNamed(t, cfg, "Bucket")
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: axe, Count: 1}, {ID: logs, Count: 1}, {ID: logs, Count: 1}, {ID: oak, Count: 1}, {ID: bucket, Count: 1}})
	sim.OpenBank([]actionsim.Stack{{ID: logs, Count: 100}})
	sim.SetTicked(true)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tk := time.NewTicker(20 * time.Millisecond)
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
	var mu sync.Mutex
	var lines []string
	d := &Deps{Session: sim, Config: cfg, Components: actionsim.Components(),
		Delay: func(d time.Duration) bool { time.Sleep(d); return true },
		Log:   func(s string) { mu.Lock(); lines = append(lines, s); mu.Unlock() }}
	bank := actions.NewBank(sim, d.Components)

	// Three kinds go (yew logs, oak logs, the bucket); the rune axe stays.
	if n := DepositAllExcept(d, bank, []string{woodcutplan.RuneAxe}); n != 3 {
		t.Fatalf("deposited %d kinds, want 3", n)
	}
	most, ticks := 0, 0
	for _, n := range sim.PerTick() {
		if n > 0 {
			ticks++
		}
		if n > most {
			most = n
		}
	}
	if most != 3 || ticks != 1 {
		t.Fatalf("per tick %v, want three Deposit-Alls in one tick", sim.PerTick())
	}
	if got := sim.Pack(); len(got) != 1 || got[0].ID != axe {
		t.Fatalf("pack = %v, want only the axe", got)
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, l := range lines {
		if strings.Contains(l, "bank: woodcut deposit: 3 operation(s), 3 user event(s) in 1 tick(s)") {
			found = true
		}
	}
	if !found {
		t.Fatalf("log lines %q", lines)
	}
}
