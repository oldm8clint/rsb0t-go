package fleetrunner

import (
	"strings"
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/fleetplan"
)

// natureWorld is a Magic 60 account with walk alch on, holding stack in its pack (item id, count) and, in its bank, bank.
type natureWorld struct {
	sim *actionsim.Session
	d   *Deps
	tr  *Travel
	log []string
}

func newNatureWorld(t *testing.T, pack, bank []actionsim.Stack) *natureWorld {
	t.Helper()
	cfg := travelTestConfig(t)
	w := &natureWorld{sim: actionsim.New(certObjs(cfg), pack)}
	if bank != nil {
		w.sim.OpenBank(bank)
	}
	setStat(w.sim, fleetplan.MagicStat, 200000, 60)
	w.d = fastDeps(w.sim, cfg)
	w.d.Log = func(s string) { w.log = append(w.log, s) }
	w.tr = &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true, Kit: 200, Carry: 200}.Normalised()}
	return w
}

func (w *natureWorld) said(sub string) bool {
	for _, l := range w.log {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func (w *natureWorld) packCount(id int32) int { return held(w.sim.Pack(), id) }

// AlchKeeps: the runes stay for walk alch whatever the pack holds, the stock only while the pack holds a Nature rune.
func TestAlchKeepsTheStockOnlyWhileThePackHoldsNatures(t *testing.T) {
	cfg := travelTestConfig(t)
	nature := objOf(t, cfg, "Nature rune", false)
	bows := objOf(t, cfg, "Yew shortbow", true)

	bare := newNatureWorld(t, []actionsim.Stack{{ID: bows, Count: 67}}, nil)
	for name, want := range map[string]bool{"Nature rune": true, "Fire rune": true, "Yew shortbow": false, "Adamant platelegs": false, "Magic staff": false, "Lobster": false} {
		if got := bare.tr.AlchKeeps(bare.d, name); got != want {
			t.Errorf("no natures in the pack: AlchKeeps(%q) = %v, want %v", name, got, want)
		}
	}
	withNatures := newNatureWorld(t, []actionsim.Stack{{ID: bows, Count: 67}, {ID: nature, Count: 1}}, nil)
	for name, want := range map[string]bool{"Nature rune": true, "Yew shortbow": true, "Adamant platelegs": true, "Lobster": false} {
		if got := withNatures.tr.AlchKeeps(withNatures.d, name); got != want {
			t.Errorf("one nature rune in the pack: AlchKeeps(%q) = %v, want %v", name, got, want)
		}
	}
}

// StockKeeper, the deposit of a Heroes bank pass or a pen kit: with natures it keeps the visit's budget of stock, with
// none it banks every note whatever the budget -- and says why.
func TestStockKeeperBanksEveryNoteWhenThePackHoldsNoNature(t *testing.T) {
	cfg := travelTestConfig(t)
	nature, fire := objOf(t, cfg, "Nature rune", false), objOf(t, cfg, "Fire rune", false)
	bows, plates := objOf(t, cfg, "Yew shortbow", true), objOf(t, cfg, "Adamant platelegs", true)
	lobster := objOf(t, cfg, "Lobster", false)

	// 0 natures, 67 alchables, as logged live: every stock slot goes into the bank, the fire runes (a kit's) stay.
	w := newNatureWorld(t, []actionsim.Stack{{ID: bows, Count: 40}, {ID: plates, Count: 27}, {ID: fire, Count: 100}, {ID: lobster, Count: 1}}, nil)
	keeps := w.tr.StockKeeper(w.d, 200, "the Heroes loop")
	for _, it := range PackSlotItems(w.d) {
		want := it.ID == fire // the fire runes stay; the food and the stock do not here (the caller's own rules decide food)
		if it.ID == lobster {
			want = false
		}
		if got := keeps(it); got != want {
			t.Errorf("no natures: keeps(%s x%d) = %v, want %v", ObjName(w.d, it.ID), it.Count, got, want)
		}
	}
	if !w.said("no Nature runes") || !w.said("0 of 67") {
		t.Errorf("the bank pass did not say why the stock stays banked: %q", w.log)
	}

	// One nature rune: the stock stays, up to the budget.
	w = newNatureWorld(t, []actionsim.Stack{{ID: bows, Count: 40}, {ID: plates, Count: 27}, {ID: nature, Count: 1}}, nil)
	keeps = w.tr.StockKeeper(w.d, 200, "the Heroes loop")
	for _, it := range PackSlotItems(w.d) {
		if got := keeps(it); !got {
			t.Errorf("a nature rune in the pack: keeps(%s x%d) = false, want true", ObjName(w.d, it.ID), it.Count)
		}
	}
	// ... and the budget still cuts it: the plates (27) fit under 30, the bows (40) do not.
	keeps = w.tr.StockKeeper(w.d, 30, "a safespot pen")
	for _, it := range PackSlotItems(w.d) {
		want := it.ID != bows
		if got := keeps(it); got != want {
			t.Errorf("budget 30: keeps(%s x%d) = %v, want %v", ObjName(w.d, it.ID), it.Count, got, want)
		}
	}
}

// DrawAlchKit: the draw out of the account's own bank takes the stock only against natures -- the pack's, or the ones this
// same draw takes out of the bank.
func TestDrawAlchKitDrawsNoStockWithoutNatures(t *testing.T) {
	cfg := travelTestConfig(t)
	nature, fire := objOf(t, cfg, "Nature rune", false), objOf(t, cfg, "Fire rune", false)
	strung := int32(857)
	plates := objOf(t, cfg, "Adamant platelegs", false)
	bowNote, plateNote := objOf(t, cfg, "Yew shortbow", true), objOf(t, cfg, "Adamant platelegs", true)
	stockInBank := []actionsim.Stack{{ID: fire, Count: 2000}, {ID: strung, Count: 150}, {ID: plates, Count: 44}}

	// A bank with fire runes and stock but no natures, and a pack with none: the stock stays in the bank.
	w := newNatureWorld(t, nil, stockInBank)
	w.tr.DrawAlchKit(w.d, actions.NewBank(w.sim, w.d.Components), testWaiter{w.d})
	if w.packCount(bowNote) != 0 || w.packCount(plateNote) != 0 || w.packCount(strung) != 0 || w.packCount(plates) != 0 {
		t.Errorf("stock drawn with no Nature rune anywhere: pack %+v", w.sim.Pack())
	}
	if held(w.sim.Bank(), strung) != 150 || held(w.sim.Bank(), plates) != 44 {
		t.Errorf("the stock left the bank: %+v", w.sim.Bank())
	}
	if !w.said("no Nature runes in the pack or the bank") {
		t.Errorf("the draw did not say why it drew no stock: %q", w.log)
	}

	// The same bank with natures in it: the natures come out first and the stock with them.
	w = newNatureWorld(t, nil, append([]actionsim.Stack{{ID: nature, Count: 500}}, stockInBank...))
	w.tr.DrawAlchKit(w.d, actions.NewBank(w.sim, w.d.Components), testWaiter{w.d})
	if w.packCount(nature) != 200 || w.packCount(bowNote) != 150 || w.packCount(plateNote) != 44 {
		t.Errorf("natures in the bank: %d natures %d bows %d plates, want 200 150 44 (%+v)", w.packCount(nature), w.packCount(bowNote), w.packCount(plateNote), w.sim.Pack())
	}

	// No natures in the bank, one in the pack: the stock is drawn against it.
	w = newNatureWorld(t, []actionsim.Stack{{ID: nature, Count: 1}}, stockInBank)
	w.tr.DrawAlchKit(w.d, actions.NewBank(w.sim, w.d.Components), testWaiter{w.d})
	if w.packCount(bowNote) != 150 || w.packCount(plateNote) != 44 {
		t.Errorf("a nature rune in the pack: %d bows %d plates, want 150 44 (%+v)", w.packCount(bowNote), w.packCount(plateNote), w.sim.Pack())
	}
}

// IsAlchStock reads the qualified name a mule ask uses for the strung bow as the bow.
func TestIsAlchStockReadsQualifiedNames(t *testing.T) {
	tr := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised()}
	for name, want := range map[string]bool{
		"Yew shortbow": true, "yew shortbow id857": true, "Yew shortbow id857": true, "Ruby ring id1641": true,
		"Nature rune": false, "Cake": false, "Willow longbow id847": false, "": false,
	} {
		if got := tr.IsAlchStock(name); got != want {
			t.Errorf("IsAlchStock(%q) = %v, want %v", name, got, want)
		}
	}
	var nilTravel *Travel
	if nilTravel.IsAlchStock("Yew shortbow") {
		t.Error("a nil Travel has no stock")
	}
}
