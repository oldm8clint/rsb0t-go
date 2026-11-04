package crafterrunner

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/crafterplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/tile"
)

func realConfig(t *testing.T) *cachecfg.Config {
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

// A kit the trip could not bank leaves flax notes in the pack: NotedFlaxCount counts those and
// only those, so the Seers loop banks them instead of trying to spin a note. CONTROL: FlaxCount,
// the by-name count the loop's other steps use, counts notes and flax together.
func TestNotedFlaxCountCountsOnlyTheNotes(t *testing.T) {
	cfg := realConfig(t)
	const flax = 1779 // content/pack/obj.pack: flax
	var note int32
	for _, id := range cfg.ObjIDs(crafterplan.Flax) {
		if o, ok := cfg.Obj(id); ok && o.Noted() {
			note = int32(id)
		}
	}
	if note == 0 {
		t.Fatal("no flax note in this config")
	}
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: note, Count: 1000}, {ID: flax, Count: 1}, {ID: flax, Count: 1}})
	d := &Deps{Session: sim, Config: cfg}
	if got := NotedFlaxCount(d); got != 1000 {
		t.Fatalf("NotedFlaxCount = %d, want 1000", got)
	}
	if got := FlaxCount(d); got != 1002 {
		t.Fatalf("control: FlaxCount = %d, want 1002 (notes included)", got)
	}
	if got := NotedFlaxCount(&Deps{Session: actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: flax, Count: 1}}), Config: cfg}); got != 0 {
		t.Fatalf("unnoted flax only: NotedFlaxCount = %d", got)
	}
}

// The flax kit is one bundled kit order naming Flax at flaxKitSize, stamped with the account's
// world, to the named mule. Nothing answers here, so it comes back with nothing and no bank.
func TestFetchFlaxKitAsksForTheKitSizeOfFlax(t *testing.T) {
	bus := mulebus.NewBus()
	w := mulebus.NewWatcher()
	d := &fleetrunner.Deps{Session: actionsim.New(actionsim.Objs{}, nil), Log: func(string) {},
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true }}
	kit := FetchFlaxKit(d, bus, "flaxer1", "mule", 1000, tile.Tile{}, 3, 30*time.Millisecond)
	if kit.Got != 0 || kit.Banked || kit.BankFlax != -1 || kit.Stopped {
		t.Fatalf("an unanswered ask = %+v", kit)
	}
	seen := false
	for _, m := range w.Poll(bus) {
		if m.Kind != mulebus.KindKitOrder {
			continue
		}
		seen = true
		if !strings.EqualFold(m.To, "mule") || m.World != 3 {
			t.Errorf("kit order to %q on world %d", m.To, m.World)
		}
		if len(m.Kit) != 1 || m.Kit[0].Item != crafterplan.Flax || m.Kit[0].Qty != 1000 {
			t.Errorf("kit lines = %+v, want one line of 1000 Flax", m.Kit)
		}
	}
	if !seen {
		t.Fatal("no kit order was published")
	}
}
