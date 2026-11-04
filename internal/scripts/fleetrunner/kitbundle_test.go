package fleetrunner

import (
	"os"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/fleetplan"
)

// Review of r4, blocking item 3: KitResult.Got must count the item itself in the pack, never its
// note. 10 cake as notes plus 3 as cake is 3 cake a Stage 6 account can eat. CONTROL: the by-name
// count RequestKitFromMule uses counts all 13, the over-count the review found.
func TestKitGotCountsOnlyTheUnnotedItem(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const cake = 1891
	var note int32
	for _, id := range cfg.ObjIDs("Cake") {
		if o, ok := cfg.Obj(id); ok && o.Noted() {
			note = int32(id)
		}
	}
	if note == 0 {
		t.Fatal("no cake note in this config")
	}
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: note, Count: 10}, {ID: cake, Count: 1}, {ID: cake, Count: 1}, {ID: cake, Count: 1}})
	d := &Deps{Session: sim, Config: cfg}
	if got := CountPackUnnoted(d, "Cake"); got != 3 {
		t.Fatalf("countPackUnnoted = %d, want 3", got)
	}
	if got := CountPackNamed(d, "Cake"); got != 13 {
		t.Fatalf("control: the by-name count is %d, want 13 (notes included)", got)
	}
}

// revK surviving mutant: RequestKitBundle's Got must come from the unnoted count. kitGotFromPack is
// that step: 10 cake notes plus 3 cake gained is Got 3, with the 10 reported as notes still held.
func TestKitGotFromPackReportsTheItemAndTheNotesApart(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var note int32
	for _, id := range cfg.ObjIDs("Cake") {
		if o, ok := cfg.Obj(id); ok && o.Noted() {
			note = int32(id)
		}
	}
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: note, Count: 10}, {ID: 1891, Count: 1}, {ID: 1891, Count: 1}, {ID: 1891, Count: 1}, {ID: 1891, Count: 1}})
	d := &Deps{Session: sim, Config: cfg}
	got, notes := kitGotFromPack(d, []fleetplan.ItemCount{{Item: "Cake", Count: 20}}, map[string]int{"Cake": 1})
	if got["Cake"] != 3 || notes["Cake"] != 10 {
		t.Fatalf("got %v notes %v, want 3 cake (4 held less 1 before) and 10 notes", got, notes)
	}
}

// revK: MuleDeps never set MyWorld, so a Scarab kit order carried no world and a mule on another
// world could not refuse it. KitBundleOptions.World stamps it.
func TestAKitBundleOrderCarriesTheAccountsWorld(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bus := mulebus.NewBus()
	w := mulebus.NewWatcher()
	d := &Deps{Session: actionsim.New(actionsim.Objs{}, nil), Config: cfg, Log: func(string) {},
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true }}
	RequestKitBundle(d, bus, "acct1", "mule", []fleetplan.ItemCount{{Item: "Cake", Count: 5}},
		KitBundleOptions{Timeout: 30 * time.Millisecond, FallbackTimeout: 10 * time.Millisecond, World: 2})
	seen := false
	for _, m := range w.Poll(bus) {
		if m.Kind == mulebus.KindKitOrder {
			seen = true
			if m.World != 2 {
				t.Fatalf("the kit order carries world %d, want 2", m.World)
			}
		}
	}
	if !seen {
		t.Fatal("no kit order was published")
	}
}

// A pack full of kept food has no room for the kit: the tight keep list banks food, rope and the
// pass but keeps coins, runes, staffs, the kit's own items and the caller's extras.
func TestKitRoomKeepBanksFood(t *testing.T) {
	keep := []string{"coins", "kebab", "lobster", "rope", "shantay pass", "chaos rune", "staff of air", "yew shortbow", "law rune"}
	got := KitRoomKeep(keep, []fleetplan.ItemCount{{Item: "Law rune", Count: 20}}, []string{"yew shortbow"})
	want := map[string]bool{"coins": true, "chaos rune": true, "staff of air": true, "yew shortbow": true, "law rune": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for _, n := range got {
		if !want[n] {
			t.Fatalf("kept %q: got %v", n, got)
		}
	}
}
