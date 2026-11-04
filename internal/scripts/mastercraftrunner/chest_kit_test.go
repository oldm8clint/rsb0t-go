package mastercraftrunner

import (
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
)

// Object ids of the chest kit and its loot, as content has them.
const (
	ckKey, ckKeyNote      = 989, 990
	ckAir, ckWater, ckLaw = 556, 555, 563
	ckFire                = 554
	ckCoins               = 995
	ckFruit, ckFruitNote  = 464, 465
	ckDragon, ckBar       = 1631, 2363
	ckFish                = 371
	ckOre, ckOreNote      = 440, 441
	ckEarth               = 557
)

func ckObjs() actionsim.Objs {
	stack := map[int32]bool{ckAir: true, ckWater: true, ckLaw: true, ckFire: true, ckCoins: true, ckEarth: true}
	notes := map[int32]int32{ckKeyNote: ckKey, ckFruitNote: ckFruit, ckOreNote: ckOre}
	noteOf := map[int32]int32{ckKey: ckKeyNote, ckFruit: ckFruitNote, ckOre: ckOreNote}
	return actionsim.Objs{
		Stackable: func(id int32) bool { return stack[id] },
		NoteOf:    func(id int32) int32 { return noteOf[id] },
		UnnoteOf:  func(id int32) int32 { return notes[id] },
	}
}

var ckIDs = mastercraftplan.KitIDs{
	Keys:  []int{ckKey},
	Runes: map[string][]int{mastercraftplan.RuneAir: {ckAir}, mastercraftplan.RuneWater: {ckWater}, mastercraftplan.RuneLaw: {ckLaw}, mastercraftplan.RuneFire: {ckFire}},
	Coins: []int{ckCoins},
	Fruit: []int{ckFruit},
}

// ckRun plays the server for one visit: a tick every few milliseconds until the test ends.
func ckRun(t *testing.T, sim *actionsim.Session) {
	t.Helper()
	sim.SetTicked(true)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tk := time.NewTicker(15 * time.Millisecond)
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

func ckCount(rows []actionsim.Stack, id int32) int {
	n := 0
	for _, r := range rows {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

func ckVisit(t *testing.T, pack, bank []actionsim.Stack, atPost bool, magic int, paid map[string]bool, expired bool) (*actionsim.Session, ChestKitResult) {
	t.Helper()
	sim := actionsim.New(ckObjs(), pack)
	ckRun(t, sim)
	sim.OpenBank(bank)
	d := &fleetrunner.Deps{Session: sim, Components: actionsim.Components(), Delay: func(dur time.Duration) bool { time.Sleep(dur); return true }}
	b := actions.NewBank(sim, actionsim.Components())
	// The client has to have been sent the open bank before the visit looks at it.
	deadline := time.Now().Add(2 * time.Second)
	for !b.IsOpen() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !b.IsOpen() {
		t.Fatal("the simulated bank never opened")
	}
	time.Sleep(60 * time.Millisecond) // the open's own inventories
	spec := ChestKitSpec{IDs: ckIDs, Want: func(keys int) mastercraftplan.KitWant {
		return mastercraftplan.ChestKitWant(atPost, magic, paid, expired, keys)
	}}
	res := ChestKit(d, b, spec)
	if res.Err != "" {
		t.Fatalf("the visit failed: %s", res.Err)
	}
	return sim, res
}

func lootPack() []actionsim.Stack {
	return []actionsim.Stack{
		{ID: ckKey, Count: 1}, {ID: ckKey, Count: 1},
		{ID: ckAir, Count: 68}, {ID: ckWater, Count: 56}, {ID: ckLaw, Count: 16},
		{ID: ckCoins, Count: 2620},
		{ID: ckDragon, Count: 1}, {ID: ckDragon, Count: 1}, {ID: ckDragon, Count: 1},
		{ID: ckBar, Count: 1}, {ID: ckBar, Count: 1}, {ID: ckBar, Count: 1},
		{ID: ckFish, Count: 1}, {ID: ckFish, Count: 1}, {ID: ckFish, Count: 1}, {ID: ckFish, Count: 1}, {ID: ckFish, Count: 1},
		{ID: ckOreNote, Count: 150}, {ID: ckEarth, Count: 50}, {ID: ckFruit, Count: 1}, {ID: ckFruit, Count: 1},
	}
}

func kitBank() []actionsim.Stack {
	return []actionsim.Stack{
		{ID: ckKey, Count: 300}, {ID: ckAir, Count: 500}, {ID: ckWater, Count: 90}, {ID: ckLaw, Count: 120},
		{ID: ckCoins, Count: 4000000}, {ID: ckFruit, Count: 60}, {ID: ckDragon, Count: 12}, {ID: ckFire, Count: 200},
	}
}

// A Falador visit with the trip's loot in the pack: everything is banked (the chest's coins and
// runes with it), and the kit drawn again exactly: six keys, the runes of six Falador casts and one
// Varrock cast home (Magic 39), the fare, three fruit. With an air staff worn no Air rune is drawn
// and the ones in the pack are banked.
func TestChestKitBanksTheLootAndDrawsTheKit(t *testing.T) {
	for _, staff := range []bool{false, true} {
		var paid map[string]bool
		if staff {
			paid = map[string]bool{mastercraftplan.RuneAir: true}
		}
		sim, res := ckVisit(t, lootPack(), kitBank(), false, 39, paid, false)
		pack, bank := sim.Pack(), sim.Bank()
		want := map[int32]int{ckKey: 6, ckWater: 6, ckLaw: 7, ckFire: 1, ckCoins: mastercraftplan.ChestFares, ckFruit: mastercraftplan.ChestFruitCarry}
		if !staff {
			want[ckAir] = 21
		}
		for id, n := range want {
			if got := ckCount(pack, id); got != n {
				t.Errorf("staff %v: the pack holds %d of item %d, want %d", staff, got, id, n)
			}
		}
		for _, r := range pack {
			if _, ok := want[r.ID]; !ok {
				t.Errorf("staff %v: the pack still holds %d of item %d: %+v", staff, r.Count, r.ID, pack)
			}
		}
		// The bank got everything else: the coins on top of the fare, the surplus runes, the loot.
		if got := ckCount(bank, ckCoins); got != 4000000+2620-mastercraftplan.ChestFares {
			t.Errorf("staff %v: the bank holds %d coins", staff, got)
		}
		if got := ckCount(bank, ckDragon); got != 15 {
			t.Errorf("staff %v: %d uncut dragonstones banked, want 15", staff, got)
		}
		if got := ckCount(bank, ckOre); got != 150 {
			t.Errorf("staff %v: the noted ore banked as %d ore", staff, got)
		}
		wantAir := 500 + 68 - 21
		if staff {
			wantAir = 500 + 68
		}
		if got := ckCount(bank, ckAir); got != wantAir {
			t.Errorf("staff %v: the bank holds %d air runes, want %d", staff, got, wantAir)
		}
		if got := ckCount(bank, ckKey); got != 300+2-6 || res.KeysBank != 296 || res.KeysDrawn != 6 {
			t.Errorf("staff %v: bank keys %d, result %+v", staff, got, res)
		}
		if res.Took > 2*time.Second {
			t.Errorf("staff %v: the visit took %v", staff, res.Took)
		}
		if sim.Refused() != 0 {
			t.Errorf("staff %v: the engine refused %d packet(s)", staff, sim.Refused())
		}
		t.Logf("staff %v: visit took %v, %d packets", staff, res.Took, len(sim.Sent()))
	}
}

// The post visit after the ask: the mule's hand-out is notes, and the bank has no row for them yet.
// They are banked first and the kit is drawn from the rows that creates.
func TestChestKitDrawsFromWhatItJustBanked(t *testing.T) {
	pack := []actionsim.Stack{{ID: ckKeyNote, Count: 313}, {ID: ckFruitNote, Count: 1000}, {ID: ckLaw, Count: 6}, {ID: ckWater, Count: 6}}
	bank := []actionsim.Stack{{ID: ckAir, Count: 40}, {ID: ckCoins, Count: 900000}}
	sim, res := ckVisit(t, pack, bank, true, 39, map[string]bool{mastercraftplan.RuneAir: true}, false)
	pk, bk := sim.Pack(), sim.Bank()
	if ckCount(pk, ckKey) != 6 || ckCount(bk, ckKey) != 307 || res.KeysBank != 307 {
		t.Errorf("keys: pack %d, bank %d, result %+v", ckCount(pk, ckKey), ckCount(bk, ckKey), res)
	}
	if ckCount(pk, ckFruit) != 3 || ckCount(bk, ckFruit) != 997 {
		t.Errorf("fruit: pack %d, bank %d", ckCount(pk, ckFruit), ckCount(bk, ckFruit))
	}
	if ckCount(pk, ckKeyNote) != 0 || ckCount(pk, ckFruitNote) != 0 {
		t.Errorf("a note was left in the pack: %+v", pk)
	}
	// The bank had no water or law rune row: the pack's own were banked, and the kit drawn from the
	// rows that made.
	if ckCount(pk, ckLaw) != 6 || ckCount(pk, ckWater) != 6 || ckCount(pk, ckAir) != 0 {
		t.Errorf("the runes: law %d water %d air %d", ckCount(pk, ckLaw), ckCount(pk, ckWater), ckCount(pk, ckAir))
	}
}

// The way home draws no keys: the keys in the pack are banked, and what the long walk needs (the
// fare, the runes of the Camelot Teleport when castable) is carried.
func TestChestKitOnTheWayHomeDrawsNoKeys(t *testing.T) {
	sim, res := ckVisit(t, lootPack(), kitBank(), false, 45, nil, true)
	pk := sim.Pack()
	if ckCount(pk, ckKey) != 0 || res.KeysDrawn != 0 || res.KeysBank != 302 {
		t.Errorf("keys: pack %d, result %+v", ckCount(pk, ckKey), res)
	}
	if ckCount(pk, ckCoins) != mastercraftplan.ChestFares || ckCount(pk, ckAir) != 23 || ckCount(pk, ckLaw) != 7 {
		t.Errorf("the walk home's kit: coins %d air %d law %d", ckCount(pk, ckCoins), ckCount(pk, ckAir), ckCount(pk, ckLaw))
	}
}

// At Magic 39 the way home starts with a Varrock Teleport, so the visit that draws no keys carries its
// runes (a Fire rune, three Air, a Law) and the Falador casts' on top; a bank with no Fire rune
// (the main account's own never asked the mule for any) just leaves it out and the way home is walked.
func TestChestKitCarriesTheVarrockRunesForTheWayHome(t *testing.T) {
	sim, _ := ckVisit(t, lootPack(), kitBank(), false, 39, nil, true)
	pk := sim.Pack()
	if ckCount(pk, ckFire) != 1 || ckCount(pk, ckAir) != 21 || ckCount(pk, ckLaw) != 7 || ckCount(pk, ckKey) != 0 {
		t.Errorf("fire %d air %d law %d keys %d", ckCount(pk, ckFire), ckCount(pk, ckAir), ckCount(pk, ckLaw), ckCount(pk, ckKey))
	}
	noFire := []actionsim.Stack{{ID: ckKey, Count: 300}, {ID: ckAir, Count: 500}, {ID: ckWater, Count: 90}, {ID: ckLaw, Count: 120}, {ID: ckCoins, Count: 5000}}
	sim, res := ckVisit(t, nil, noFire, false, 39, nil, true)
	if ckCount(sim.Pack(), ckFire) != 0 || res.Err != "" {
		t.Errorf("pack %+v, result %+v", sim.Pack(), res)
	}
}

// With no fruit in the bank (the mule had none) the visit draws what it can and nothing fails.
func TestChestKitWithoutFruit(t *testing.T) {
	bank := []actionsim.Stack{{ID: ckKey, Count: 300}, {ID: ckAir, Count: 500}, {ID: ckWater, Count: 90}, {ID: ckLaw, Count: 120}, {ID: ckCoins, Count: 5000}}
	sim, res := ckVisit(t, nil, bank, false, 39, nil, false)
	pk := sim.Pack()
	if ckCount(pk, ckFruit) != 0 || ckCount(pk, ckKey) != 6 || res.KeysDrawn != 6 {
		t.Errorf("pack %+v result %+v", pk, res)
	}
	// An empty pack and an empty bank: nothing to do, nothing wrong.
	_, res = ckVisit(t, nil, nil, false, 39, nil, false)
	if res.KeysDrawn != 0 || res.KeysBank != 0 {
		t.Errorf("%+v", res)
	}
}
