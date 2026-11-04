package bot

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/wcfletchplan"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// Two actions a tick (twopertick-1012): ProgressiveWCFletch's Seers visit draws the chop manipulation's carrier
// log and the banked logs in ONE burst.

func wcConfig(t *testing.T) *cachecfg.Config {
	t.Helper()
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func wcObj(t *testing.T, cfg *cachecfg.Config, name string) int32 {
	t.Helper()
	for _, id := range cfg.ObjIDs(name) {
		if o, ok := cfg.Obj(id); ok && !o.Noted() {
			return int32(id)
		}
	}
	t.Fatalf("no %q in this config", name)
	return 0
}

type wcTrip struct {
	b     *wcfletchBot
	ctx   *Context
	d     *woodcutrunner.Deps
	sim   *actionsim.Session
	bank  *actions.Bank
	lines *[]string
	mu    *sync.Mutex
}

func newWCTrip(t *testing.T, pack, bankRows func(cfg *cachecfg.Config) []actionsim.Stack) *wcTrip {
	t.Helper()
	cfg := wcConfig(t)
	objs := actionsim.Objs{Stackable: func(id int32) bool { o, ok := cfg.Obj(int(id)); return ok && o.Stackable }}
	sim := actionsim.New(objs, pack(cfg))
	sim.OpenBank(bankRows(cfg))
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
	var lines []string
	var mu sync.Mutex
	ctx := &Context{Username: "wctrip", Log: func(s string) { mu.Lock(); lines = append(lines, s); mu.Unlock() }}
	b := &wcfletchBot{cfg: wcfletchplan.DefaultSettings(), manip: woodcutrunner.NewManipState()}
	b.cfg.Fletch = true
	d := &woodcutrunner.Deps{Session: sim, Config: cfg, Components: actionsim.Components(),
		Delay: func(d time.Duration) bool { time.Sleep(d); return true }}
	return &wcTrip{b: b, ctx: ctx, d: d, sim: sim, bank: actions.NewBank(sim, d.Components), lines: &lines, mu: &mu}
}

func (w *wcTrip) said(sub string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, l := range *w.lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func (w *wcTrip) heldOf(rows []actionsim.Stack, id int32) int {
	n := 0
	for _, r := range rows {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

func TestDrawCarrierAndLogsIsOneBurst(t *testing.T) {
	var knife, plain, oak int32
	trip := newWCTrip(t,
		func(cfg *cachecfg.Config) []actionsim.Stack {
			knife = wcObj(t, cfg, woodcutplan.Knife)
			return []actionsim.Stack{{ID: knife, Count: 1}}
		},
		func(cfg *cachecfg.Config) []actionsim.Stack {
			plain, oak = wcObj(t, cfg, "Logs"), wcObj(t, cfg, "Oak logs")
			return []actionsim.Stack{{ID: plain, Count: 50}, {ID: oak, Count: 300}}
		})
	rung := wcfletchplan.Rung{Tier: woodcutplan.Tier{Logs: "Oak logs"}, LogsName: "Oak logs", LogsID: int(oak)}

	if !trip.b.drawCarrierAndLogs(trip.ctx, trip.d, trip.bank, rung) {
		t.Fatalf("the banked logs were not drawn: %q", *trip.lines)
	}
	most, ticks := 0, 0
	for _, n := range trip.sim.PerTick() {
		if n > 0 {
			ticks++
		}
		if n > most {
			most = n
		}
	}
	if most != 2 || ticks != 1 {
		t.Fatalf("per tick %v, want the carrier and the logs as 2 events in 1 tick", trip.sim.PerTick())
	}
	pack := trip.sim.Pack()
	// The knife, one plain log for the manipulation, and the oak logs that fit.
	if trip.heldOf(pack, plain) != 1 || trip.heldOf(pack, oak) != 26 || trip.heldOf(pack, knife) != 1 {
		t.Fatalf("pack = %v, want a knife, 1 log and 26 oak logs", pack)
	}
	if trip.b.bankDraws != 1 || trip.b.drawFails != 0 {
		t.Fatalf("bankDraws %d drawFails %d", trip.b.bankDraws, trip.b.drawFails)
	}
	if !trip.said("wcfletch: bank: carrier and banked logs: 2 operation(s), 2 user event(s) in 1 tick(s)") {
		t.Fatalf("log lines %q", *trip.lines)
	}
}

// A pack with no room for the logs draws only the carrier, and counts the failed draw.
func TestDrawCarrierAndLogsKeepsTheRoomItPromised(t *testing.T) {
	var plain, oak int32
	trip := newWCTrip(t,
		func(cfg *cachecfg.Config) []actionsim.Stack {
			knife := wcObj(t, cfg, woodcutplan.Knife)
			junk := wcObj(t, cfg, "Bucket")
			pack := []actionsim.Stack{{ID: knife, Count: 1}}
			for i := 0; i < 25; i++ {
				pack = append(pack, actionsim.Stack{ID: junk, Count: 1})
			}
			return pack
		},
		func(cfg *cachecfg.Config) []actionsim.Stack {
			plain, oak = wcObj(t, cfg, "Logs"), wcObj(t, cfg, "Oak logs")
			return []actionsim.Stack{{ID: plain, Count: 50}, {ID: oak, Count: 300}}
		})
	rung := wcfletchplan.Rung{Tier: woodcutplan.Tier{Logs: "Oak logs"}, LogsName: "Oak logs", LogsID: int(oak)}

	if trip.b.drawCarrierAndLogs(trip.ctx, trip.d, trip.bank, rung) {
		t.Fatal("logs were drawn into a pack with two free slots")
	}
	if trip.heldOf(trip.sim.Pack(), plain) != 1 || trip.heldOf(trip.sim.Pack(), oak) != 0 {
		t.Fatalf("pack = %v, want the carrier only", trip.sim.Pack())
	}
	if trip.b.drawFails != 1 || trip.b.bankDraws != 0 {
		t.Fatalf("bankDraws %d drawFails %d", trip.b.bankDraws, trip.b.drawFails)
	}
}
