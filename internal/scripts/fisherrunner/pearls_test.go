package fisherrunner

import (
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/levels"
)

// setFletching feeds the account an UPDATE_STAT for Fletching at level.
func setFletching(sim *actionsim.Session, level int) {
	xp := levels.XPAtLevel(level)
	sim.State().Handle(serverprot.UPDATE_STAT, []byte{byte(FletchingStat), byte(xp >> 24), byte(xp >> 16), byte(xp >> 8), byte(xp), byte(level)})
}

// A deposit leaves the chisel in the pack from Fletching 41 on while the cut is on, and banks it before
// that, with the cut off, and for a Deps with no cycle at all (ProgressiveFisher): carrying it earlier would
// spend a slot on a tool nothing can use.
func TestDepositExceptKeepsTheChiselOnlyWhenTheCutAppliesRealConfig(t *testing.T) {
	cfg := realConfig(t)
	id := func(name string) int32 { return unnotedID(t, cfg, name, false) }
	for _, tc := range []struct {
		name      string
		level     int
		cycle     *Cycle
		wantKept  bool
		cutPearls bool
	}{
		{"41 with the cut on", 41, &Cycle{Opts: fisherplan.DefaultCycleOptions()}, true, true},
		{"99 with the cut on", 99, &Cycle{Opts: fisherplan.DefaultCycleOptions()}, true, true},
		{"40 with the cut on", 40, &Cycle{Opts: fisherplan.DefaultCycleOptions()}, false, true},
		{"41 with the cut off", 41, &Cycle{Opts: fisherplan.CycleOptions{OpenCaskets: true, OpenOysters: true}}, false, false},
		{"41 with no cycle", 41, nil, false, false},
	} {
		sim := actionsim.New(simObjsFor(cfg), []actionsim.Stack{
			{ID: id("Big fishing net"), Count: 1}, {ID: id("Chisel"), Count: 1}, {ID: id("Raw bass"), Count: 1},
		})
		sim.OpenBank(nil)
		setFletching(sim, tc.level)
		d := &Deps{Session: sim, Config: cfg, Components: actionsim.Components(), Delay: fastDelayForTests, Cycle: tc.cycle}
		DepositExcept(d, actions.NewBank(sim, d.Components), []string{"Big fishing net"})
		pack := sim.Pack()
		if stacksCount(pack, id("Raw bass")) != 0 || stacksCount(pack, id("Big fishing net")) != 1 {
			t.Fatalf("%s: pack %+v", tc.name, pack)
		}
		if kept := stacksCount(pack, id("Chisel")) == 1; kept != tc.wantKept {
			t.Errorf("%s: the chisel was kept %v, want %v (pack %+v bank %+v)", tc.name, kept, tc.wantKept, pack, sim.Bank())
		}
	}
}

// The chisel is drawn from the account's own bank when the cut applies and the pack holds none; never
// before 41, never a second one, never from a bank that holds none.
func TestDrawChiselFromTheBankRealConfig(t *testing.T) {
	cfg := realConfig(t)
	id := func(name string) int32 { return unnotedID(t, cfg, name, false) }
	for _, tc := range []struct {
		name  string
		level int
		pack  []actionsim.Stack
		bank  []actionsim.Stack
		want  bool
	}{
		{"41, none held, one banked", 41, nil, []actionsim.Stack{{ID: id("Chisel"), Count: 1}}, true},
		{"40: not yet", 40, nil, []actionsim.Stack{{ID: id("Chisel"), Count: 1}}, false},
		{"41 but one is held", 41, []actionsim.Stack{{ID: id("Chisel"), Count: 1}}, []actionsim.Stack{{ID: id("Chisel"), Count: 1}}, false},
		{"41, none banked", 41, nil, []actionsim.Stack{{ID: id("Raw bass"), Count: 4}}, false},
	} {
		sim := actionsim.New(simObjsFor(cfg), tc.pack)
		sim.OpenBank(tc.bank)
		setFletching(sim, tc.level)
		var logs []string
		d := &Deps{Session: sim, Config: cfg, Components: actionsim.Components(), Delay: fastDelayForTests, Cycle: &Cycle{Opts: fisherplan.DefaultCycleOptions()}, Log: func(s string) { logs = append(logs, s) }}
		if got := DrawChisel(d, actions.NewBank(sim, d.Components)); got != tc.want {
			t.Errorf("%s: drew %v, want %v (pack %+v)", tc.name, got, tc.want, sim.Pack())
		}
	}
	// With no cycle (ProgressiveFisher) nothing is drawn whatever the level.
	sim := actionsim.New(simObjsFor(cfg), nil)
	sim.OpenBank([]actionsim.Stack{{ID: id("Chisel"), Count: 1}})
	setFletching(sim, 99)
	d := &Deps{Session: sim, Config: cfg, Components: actionsim.Components(), Delay: fastDelayForTests}
	if DrawChisel(d, actions.NewBank(sim, d.Components)) {
		t.Fatal("a Deps with no cycle drew a chisel")
	}
}

// ReadBank counts the tools and logs the pearl climb needs, un-noted only, by display name.
func TestReadBankCountsTheKnifeTheChiselAndTheLogsRealConfig(t *testing.T) {
	cfg := realConfig(t)
	id := func(name string) int32 { return unnotedID(t, cfg, name, false) }
	sim := actionsim.New(simObjsFor(cfg), nil)
	sim.OpenBank([]actionsim.Stack{
		{ID: id("Knife"), Count: 3}, {ID: id("Chisel"), Count: 1}, {ID: id("Logs"), Count: 600}, {ID: id("Oak logs"), Count: 50},
		{ID: id("Willow logs"), Count: 9}, {ID: id("Maple logs"), Count: 7}, {ID: id("Raw bass"), Count: 2},
	})
	d := &Deps{Session: sim, Config: cfg, Components: actionsim.Components(), Delay: fastDelayForTests}
	r := ReadBank(d, actions.NewBank(sim, d.Components))
	if r.Knife != 3 || r.Chisel != 1 || r.Logs["Logs"] != 600 || r.Logs["Oak logs"] != 50 || r.Logs["Willow logs"] != 9 || len(r.Logs) != 3 {
		t.Fatalf("ReadBank = %+v: want the knife 3, the chisel 1 and the three logs the climb uses (no maple)", r)
	}
}

// The pack's knife and chisel are read as unnoted items only.
func TestKnifeAndChiselHeldIgnoreNotesRealConfig(t *testing.T) {
	r := newCycleRig(t)
	r.e.fill("Big fishing net", "Knife", "Chisel")
	if !KnifeHeld(r.d) || !ChiselHeld(r.d) {
		t.Fatal("a knife and a chisel in the pack are not held")
	}
	r.e.pack = [28]slotRow{}
	r.e.fill("Big fishing net", "Raw bass")
	if KnifeHeld(r.d) || ChiselHeld(r.d) {
		t.Fatal("held with neither in the pack")
	}
}
