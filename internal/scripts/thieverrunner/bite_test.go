package thieverrunner

import (
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/protocol/serverprot"
)

type biteWorld struct {
	now  time.Time
	due  []biteDue
	logs []string
}

type biteDue struct {
	at time.Time
	fn func()
}

func (w *biteWorld) Now() time.Time { return w.now }

func (w *biteWorld) Delay(d time.Duration) bool {
	target := w.now.Add(d)
	for {
		sort.SliceStable(w.due, func(i, j int) bool { return w.due[i].at.Before(w.due[j].at) })
		if len(w.due) == 0 || w.due[0].at.After(target) {
			break
		}
		e := w.due[0]
		w.due = w.due[1:]
		if e.at.After(w.now) {
			w.now = e.at
		}
		e.fn()
	}
	w.now = target
	return true
}

func (w *biteWorld) after(d time.Duration, fn func()) {
	w.due = append(w.due, biteDue{at: w.now.Add(d), fn: fn})
}

func biteObj(t *testing.T, cfg *cachecfg.Config, name string) int32 {
	t.Helper()
	for _, id := range cfg.ObjIDs(name) {
		if o, ok := cfg.Obj(id); ok && !o.Noted() {
			return int32(id)
		}
	}
	t.Fatalf("no %q in this config", name)
	return 0
}

func setHP(sim *actionsim.Session, level int) {
	sim.State().Handle(serverprot.UPDATE_STAT, []byte{3, 0, 0, 0, 100, byte(level)})
}

// LIVE 15 Oct 2026 13:59 (a Stage 3 account, Cod 19 -> 26): the mule's fish go through EatFood, which called a bite that landed seven seconds
// after its click a failure. The food leaving its slot, or the hitpoints rising, credits it on the next call, with no second click.
func TestEatFoodCreditsALateLandingWithoutAnotherClick(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cod := biteObj(t, cfg, "Cod")
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: cod, Count: 1}, {ID: cod, Count: 1}})
	setHP(sim, 53)
	setHP(sim, 19)
	clicks := 0
	sim.OnSend = func([]byte) { clicks++ }
	w := &biteWorld{now: time.Date(2026, 10, 15, 13, 59, 41, 0, time.UTC)}
	biteClock = w.Now
	t.Cleanup(func() { biteClock = nil })
	d := &Deps{Session: sim, Config: cfg, Delay: w.Delay, Log: func(s string) { w.logs = append(w.logs, s) }}

	w.after(7*time.Second, func() {
		setHP(sim, 26)
		sim.Remove(cod, 1)
	})
	if EatFood(d, "Cod") {
		t.Fatal("a bite that showed at 7 s was confirmed inside six seconds")
	}
	if _, ok := actions.BiteInFlightAt(sim.State(), w.now); !ok {
		t.Fatal("the late bite is not in flight")
	}
	w.Delay(1500 * time.Millisecond)
	if !EatFood(d, "Cod") {
		t.Fatalf("the landed bite was not credited; log %v", w.logs)
	}
	if clicks != 1 {
		t.Fatalf("%d eat clicks, want 1", clicks)
	}
	if !strings.Contains(strings.Join(w.logs, "\n"), "credited and not eaten again") {
		t.Fatalf("the credit was not logged: %v", w.logs)
	}
}

// The wait is the shared one, and the exported names the other packages and tests read still say so.
func TestEatConfirmIsTheSharedWait(t *testing.T) {
	if EatConfirmWait != actions.BiteConfirmWait || EatSlowBite != actions.BiteSlowAfter {
		t.Fatalf("EatConfirmWait %v, EatSlowBite %v", EatConfirmWait, EatSlowBite)
	}
}
