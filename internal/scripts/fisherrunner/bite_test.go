package fisherrunner

import (
	"os"
	"sort"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/protocol/serverprot"
)

// fisherrunner.EatFood waited 2.4 s for a bite, and called every later one a failure. It uses the shared wait now, and a bite that
// has not shown by then is credited by the next EatFood rather than eaten twice.
func TestEatFoodCreditsALateLandingWithoutAnotherClick(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var trout int32
	for _, id := range cfg.ObjIDs("Trout") {
		if o, ok := cfg.Obj(id); ok && !o.Noted() {
			trout = int32(id)
		}
	}
	if trout == 0 {
		t.Fatal("no Trout in this config")
	}
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: trout, Count: 1}, {ID: trout, Count: 1}})
	setHP := func(level int) { sim.State().Handle(serverprot.UPDATE_STAT, []byte{3, 0, 0, 0, 100, byte(level)}) }
	setHP(53)
	setHP(20)
	clicks := 0
	sim.OnSend = func([]byte) { clicks++ }

	now := time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC)
	type due struct {
		at time.Time
		fn func()
	}
	var pending []due
	delay := func(d time.Duration) bool {
		target := now.Add(d)
		for {
			sort.SliceStable(pending, func(i, j int) bool { return pending[i].at.Before(pending[j].at) })
			if len(pending) == 0 || pending[0].at.After(target) {
				break
			}
			e := pending[0]
			pending = pending[1:]
			if e.at.After(now) {
				now = e.at
			}
			e.fn()
		}
		now = target
		return true
	}
	biteClock = func() time.Time { return now }
	t.Cleanup(func() { biteClock = nil })
	d := &Deps{Session: sim, Config: cfg, Delay: delay}

	pending = append(pending, due{at: now.Add(5 * time.Second), fn: func() { setHP(23); sim.Remove(trout, 1) }})
	if !EatFood(d, "Trout") {
		t.Fatal("a bite that showed at 5 s was not confirmed: the old 2.4 s wait is gone, the wait is six seconds")
	}
	if clicks != 1 {
		t.Fatalf("%d clicks", clicks)
	}

	pending = append(pending, due{at: now.Add(8 * time.Second), fn: func() { setHP(26); sim.Remove(trout, 1) }})
	if EatFood(d, "Trout") {
		t.Fatal("a bite that showed at 8 s was confirmed inside six seconds")
	}
	delay(3 * time.Second)
	if _, ok := actions.BiteInFlightAt(sim.State(), now); !ok {
		t.Fatal("the late bite is not in flight")
	}
	// The pack is empty now; the late bite is credited without a pick or a click.
	if !EatFood(d, "Trout") || clicks != 2 {
		t.Fatalf("credited %v after %d clicks, want credited after 2", clicks == 2, clicks)
	}
}
