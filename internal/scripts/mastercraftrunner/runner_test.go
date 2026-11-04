package mastercraftrunner

import (
	"strings"
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/tile"
)

// The ids are gemplan's, read back from the live engine's config archive: each gem's note is the id
// above it, as every tradeable item's is in this cache. The chisel and the crushed gemstone have
// none here.
const (
	chisel   = gemplan.ChiselID
	crushed  = gemplan.CrushedGemstoneID
	sapU     = 1623
	sapCut   = 1607
	rubyU    = 1619
	rubyCut  = 1603
	opalU    = 1625
	junkFish = 317
)

func noteOf(id int32) int32 {
	switch id {
	case sapU, sapCut, rubyU, rubyCut, opalU, 1609, junkFish:
		return id + 1
	}
	return 0
}

func simObjs() actionsim.Objs {
	return actionsim.Objs{
		Stackable: func(id int32) bool { return false },
		NoteOf:    noteOf,
		UnnoteOf: func(id int32) int32 {
			for _, base := range []int32{sapU, sapCut, rubyU, rubyCut, opalU, 1609, junkFish} {
				if base+1 == id {
					return base
				}
			}
			return 0
		},
	}
}

// rig is a bank at the mule's post played by actionsim, reached through the OpenBank seam.
func rig(t *testing.T, pack, bank []actionsim.Stack) (*actionsim.Session, *fleetrunner.Deps) {
	t.Helper()
	sim := actionsim.New(simObjs(), pack)
	sim.OpenBank(bank)
	old := OpenBank
	t.Cleanup(func() { OpenBank = old })
	oldNote := NoteOfID
	t.Cleanup(func() { NoteOfID = oldNote })
	NoteOfID = func(id int32) int32 {
		if n := noteOf(id); n != 0 {
			return n
		}
		return id
	}
	OpenBank = func(d *fleetrunner.Deps, at tile.Tile) (*actions.Bank, bool) {
		sim.OpenBank(sim.Bank())
		return actions.NewBank(sim, actionsim.Components()), true
	}
	return sim, &fleetrunner.Deps{Session: sim, Components: actionsim.Components(), Delay: fleetrunner.RealDelay}
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

var post = tile.Tile{X: 2809, Z: 3441}

func bestGems(level int) func(map[string]int) (gemplan.Gem, bool) {
	gems := mastercraftplan.GemsBestFirst(level, nil, nil, true)
	return func(uncut map[string]int) (gemplan.Gem, bool) { return mastercraftplan.PickGem(gems, uncut) }
}

// THE BANK VISIT: deposit everything but the chisel (cut gems, crushed gemstones, the kit's notes
// become the item), read the bank, draw 27 of the best kind the level allows.
func TestGemBankVisitBanksTheProductAndDrawsTheBestKind(t *testing.T) {
	pack := []actionsim.Stack{
		{ID: chisel, Count: 1}, {ID: sapCut, Count: 1}, {ID: sapCut, Count: 1}, {ID: crushed, Count: 1},
		{ID: rubyU + 1, Count: 40}, // the kit's note
	}
	bank := []actionsim.Stack{{ID: sapU, Count: 100}, {ID: rubyU, Count: 10}, {ID: opalU, Count: 50}}
	sim, d := rig(t, pack, bank)
	v := GemBankVisit(d, post, bestGems(50))
	if !v.Opened || v.Err != "" {
		t.Fatalf("%+v", v)
	}
	if v.Gem.Key != "ruby" || v.Drawn != 27 || !v.ChiselHeld || v.NoChisel {
		t.Fatalf("drew %d %s, chisel %v: %+v", v.Drawn, v.Gem.Key, v.ChiselHeld, v)
	}
	if got := count(sim.Pack(), rubyU); got != 27 {
		t.Fatalf("%d uncut rubies in the pack, want 27: %v", got, sim.Pack())
	}
	if len(sim.Pack()) != 28 || count(sim.Pack(), chisel) != 1 {
		t.Fatalf("the pack is the chisel and the load: %v", sim.Pack())
	}
	b := sim.Bank()
	// 40 noted + 10 banked = 50; 27 drawn.
	if count(b, rubyU) != 23 || count(b, sapCut) != 2 || count(b, crushed) != 1 || count(b, sapU) != 100 || count(b, opalU) != 50 {
		t.Fatalf("bank %v", b)
	}
	if v.Count(rubyU) != 50 || UncutCounts(v.Banked)["ruby"] != 50 {
		t.Fatalf("the reading is the bank before the draw: %v", UncutCounts(v.Banked))
	}
	if cut, cr := CutGems(v.Banked); cut != 2 || cr != 1 {
		t.Fatalf("cut %d crushed %d", cut, cr)
	}
}

func TestGemBankVisitDrawsAChiselAndFewerGemsThanALoad(t *testing.T) {
	sim, d := rig(t, nil, []actionsim.Stack{{ID: chisel, Count: 1}, {ID: sapU, Count: 5}})
	v := GemBankVisit(d, post, bestGems(30))
	if v.Gem.Key != "sapphire" || v.Drawn != 5 || v.Err != "" {
		t.Fatalf("%+v", v)
	}
	if count(sim.Pack(), chisel) != 1 || count(sim.Pack(), sapU) != 5 {
		t.Fatalf("pack %v", sim.Pack())
	}
}

func TestGemBankVisitNoChiselAnywhere(t *testing.T) {
	sim, d := rig(t, nil, []actionsim.Stack{{ID: sapU, Count: 50}})
	v := GemBankVisit(d, post, bestGems(30))
	if !v.NoChisel || v.Drawn != 0 || v.ChiselHeld || !strings.Contains(v.Err, "chisel") {
		t.Fatalf("%+v", v)
	}
	if len(sim.Pack()) != 0 {
		t.Fatalf("no chisel: nothing should be drawn, pack %v", sim.Pack())
	}
}

func TestGemBankVisitReadsOnly(t *testing.T) {
	sim, d := rig(t, []actionsim.Stack{{ID: sapCut, Count: 1}}, []actionsim.Stack{{ID: sapU, Count: 9}, {ID: chisel, Count: 1}})
	v := GemBankVisit(d, post, nil)
	if !v.Opened || v.Err != "" || v.Drawn != 0 || !v.ChiselHeld || UncutCounts(v.Banked)["sapphire"] != 9 {
		t.Fatalf("%+v", v)
	}
	if len(sim.Pack()) != 0 || count(sim.Bank(), sapCut) != 1 {
		t.Fatalf("a read still banks the pack: pack %v bank %v", sim.Pack(), sim.Bank())
	}
}

func TestGemBankVisitNothingToCut(t *testing.T) {
	_, d := rig(t, nil, []actionsim.Stack{{ID: chisel, Count: 1}, {ID: 1631, Count: 40}}) // dragonstones at Crafting 30
	v := GemBankVisit(d, post, bestGems(30))
	if v.Drawn != 0 || v.Err != "" || !v.Opened {
		t.Fatalf("%+v", v)
	}
}

// The hand-over draw: cut gems as NOTES (one pack slot each however many), the crushed gemstone,
// which has no note, as items.
func TestDrawProductsDrawsNotesForCutGemsAndItemsForTheRest(t *testing.T) {
	pack := []actionsim.Stack{{ID: junkFish, Count: 1}, {ID: chisel, Count: 1}}
	bank := []actionsim.Stack{
		{ID: sapCut, Count: 300}, {ID: rubyCut, Count: 40}, {ID: crushed, Count: 5}, {ID: sapU, Count: 7},
	}
	sim, d := rig(t, pack, bank)
	h := DrawProducts(d, post, GemProducts())
	if !h.Opened || h.Err != "" {
		t.Fatalf("%+v", h)
	}
	if h.Rows != 3 || h.Slots != 7 || h.Got["Sapphire"] != 300 || h.Got["Ruby"] != 40 || h.Got["Crushed gemstone"] != 5 {
		t.Fatalf("rows %d slots %d got %v", h.Rows, h.Slots, h.Got)
	}
	p := sim.Pack()
	if count(p, sapCut+1) != 300 || count(p, rubyCut+1) != 40 || count(p, crushed) != 5 || len(p) != 7 {
		t.Fatalf("pack %v: want two note stacks and five crushed gemstones", p)
	}
	if count(p, sapU) != 0 || count(p, chisel) != 0 || count(p, junkFish) != 0 {
		t.Fatalf("only the products leave the bank, and the pack was banked first: %v", p)
	}
	if v := sim.State().Varp(actions.BankCertVarp); v != 0 {
		t.Fatalf("the bank was left in note mode (%d)", v)
	}
	if count(sim.Bank(), sapU) != 7 || count(sim.Bank(), junkFish) != 1 {
		t.Fatalf("bank %v", sim.Bank())
	}
}

func TestDrawProductsWithNothingBanked(t *testing.T) {
	_, d := rig(t, nil, []actionsim.Stack{{ID: sapU, Count: 7}})
	h := DrawProducts(d, post, GemProducts())
	if !h.Opened || h.Rows != 0 || h.Slots != 0 || len(h.Got) != 0 {
		t.Fatalf("%+v", h)
	}
}

func TestDepositAllBanksNotesAsTheItem(t *testing.T) {
	sim, d := rig(t, []actionsim.Stack{{ID: sapCut + 1, Count: 90}, {ID: crushed, Count: 1}, {ID: crushed, Count: 1}}, []actionsim.Stack{{ID: sapCut, Count: 10}})
	v := DepositAll(d, post)
	if !v.Opened || v.Err != "" {
		t.Fatalf("%+v", v)
	}
	if len(sim.Pack()) != 0 || count(sim.Bank(), sapCut) != 100 || count(sim.Bank(), crushed) != 2 {
		t.Fatalf("pack %v bank %v", sim.Pack(), sim.Bank())
	}
	left := CountProducts(v.Banked, GemProducts())
	if left["Sapphire"] != 100 || left["Crushed gemstone"] != 2 || Total(left) != 102 {
		t.Fatalf("left %v", left)
	}
}

func TestVisitRefusesWhenTheBankWillNotOpen(t *testing.T) {
	old := OpenBank
	defer func() { OpenBank = old }()
	OpenBank = func(*fleetrunner.Deps, tile.Tile) (*actions.Bank, bool) { return nil, false }
	d := &fleetrunner.Deps{Session: actionsim.New(simObjs(), nil), Delay: fleetrunner.RealDelay}
	if v := GemBankVisit(d, post, bestGems(30)); v.Opened || v.Err == "" {
		t.Fatalf("%+v", v)
	}
	if h := DrawProducts(d, post, GemProducts()); h.Opened || h.Err == "" {
		t.Fatalf("%+v", h)
	}
	if v := DepositAll(d, post); v.Opened || v.Err == "" {
		t.Fatalf("%+v", v)
	}
}

func TestProductsAndNames(t *testing.T) {
	ps := GemProducts()
	if len(ps) != len(gemplan.Gems)+1 {
		t.Fatalf("%d products", len(ps))
	}
	seen := map[int32]bool{}
	for _, p := range ps {
		if p.ID == 0 || p.Name == "" || seen[p.ID] {
			t.Fatalf("product %+v", p)
		}
		seen[p.ID] = true
	}
	if !seen[crushed] || !seen[sapCut] || seen[sapU] {
		t.Fatal("the products are the CUT gems and the crushed gemstone, never the uncut ones")
	}
	names := Names(append(ps, Product{ID: 1, Name: "SAPPHIRE"}, Product{ID: 2, Name: "Maple longbow"}))
	if len(names) != len(gemplan.Gems)+2 {
		t.Fatalf("names %v: a name is listed once, case-insensitively", names)
	}
	if Total(map[string]int{"a": 2, "b": 3}) != 5 {
		t.Fatal("Total")
	}
}
