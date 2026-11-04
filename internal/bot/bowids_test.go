package bot

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/peerrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Content 289 names a strung bow and its unstrung form alike. These tests read the real item config
// (RSB0T_CONFIG_JAG) and skip without it; it is decoded once for all of them.
var bowCfgOnce = sync.OnceValues(func() (*cachecfg.Config, error) {
	return cachecfg.LoadFile(os.Getenv("RSB0T_CONFIG_JAG"))
})

func bowCfg(t *testing.T) *cachecfg.Config {
	t.Helper()
	if os.Getenv("RSB0T_CONFIG_JAG") == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := bowCfgOnce()
	if err != nil {
		t.Fatalf("load %s: %v", os.Getenv("RSB0T_CONFIG_JAG"), err)
	}
	return cfg
}

func slotOf(id int32, n int32) gamestate.SlotItem {
	return gamestate.SlotItem{Item: gamestate.Item{ID: id, Count: n}}
}

// The ids of the bows in the mule's bank on 6 Oct 2026 (GET /api/bank?account=the mule): strung Yew
// shortbows 130,712 and no unstrung one; Yew longbows 7,922 unstrung and 5,519 strung; Maple longbows
// 117,044 unstrung and 55,826 strung; Bow string.
const (
	idYewShortStrung, idYewShortUnstrung = 857, 68
	idYewLongStrung, idYewLongUnstrung   = 855, 66
	idMapleLongStrung, idMapleLongUnstr  = 851, 62
	idBowString                          = 1777
)

// publishBowMule puts items on the board as the mule's bank the way the live fleet does, through
// peerrunner.Holdings (the merged row of every name and one row per obj of a keyed name).
func publishBowMule(r *masterRig, cfg *cachecfg.Config, items []gamestate.SlotItem) {
	r.ctx.Peers.Publish("mule", tile.Tile{X: 2809, Z: 3441}, true, peerrunner.Holdings(cfg, items), nil, false)
}

// ITEM 1 OF THE REPORT, end to end through the real publish and the real decision: the mule holds
// 130,712 STRUNG Yew shortbows, no unstrung bow and 36,368 Bow strings. Read by name that was 36,368
// bow-and-string pairs and a bow round every few minutes; read by obj it is none.
func TestMasterCrafterDoesNotStringTheMulesStrungBows(t *testing.T) {
	cfg := bowCfg(t)
	r := newSimRig(t, nil, nil, nil)
	r.ctx.Config = cfg
	r.b.set = mastercraftplan.DefaultSettings() // the owner's 20,000
	r.b.decide = nil
	mindHoldStat(r.ctx.Session.State(), bowstringrunner.FletchingStat, levels.XPAtLevel(99), 99)

	publishBowMule(r, cfg, []gamestate.SlotItem{slotOf(idYewShortStrung, 130712), slotOf(idBowString, 36368)})
	d := r.b.decideLive(r.ctx)
	if d.Mode == mastercraftplan.ModeBows {
		t.Fatalf("a bow round for bows that are all strung: %+v", d)
	}
	if !strings.Contains(d.Why, "no unstrung bow") {
		t.Fatalf("the check should say the mule holds no unstrung bow: %q", d.Why)
	}

	// The same bank with 25,000 unstrung Maple longbows among the strung ones: a round, and the pairs
	// are the 25,000 unstrung bows, not the 80,826 the name holds.
	publishBowMule(r, cfg, []gamestate.SlotItem{
		slotOf(idYewShortStrung, 130712), slotOf(idMapleLongUnstr, 25000), slotOf(idMapleLongStrung, 55826), slotOf(idBowString, 36368),
	})
	d = r.b.decideLive(r.ctx)
	if d.Mode != mastercraftplan.ModeBows || d.Pairs != 25000 {
		t.Fatalf("want a bow round of 25000 pairs: %+v", d)
	}
}

// ITEM 2 OF THE REPORT, the BowStringer's kit: asked of a mule holding strung Yew shortbows only, the
// bow line is the QUALIFIED unstrung name and the mule's count is read under it, so the plan finds
// the unstrung bows exhausted instead of asking a plain name the mule fills with the strung ones.
func TestBowStringerAsksTheUnstrungObjByQualifiedName(t *testing.T) {
	cfg := bowCfg(t)
	r := newSimRig(t, nil, nil, nil)
	r.ctx.Config = cfg
	bs := &bowStringerBot{set: bowstringplan.Settings{Mule: "mule", KitSize: 1000}}
	ids := bowstringrunner.IDs{Unstrung: idYewShortUnstrung, Strung: idYewShortStrung, String: idBowString}
	yew := bowstringplan.Bow{Name: "Yew shortbow", Level: 65}
	var visit bowstringrunner.BankVisit // an empty bank: the bank can fund no batch

	// 130,712 strung, no unstrung: nothing to ask for.
	publishBowMule(r, cfg, []gamestate.SlotItem{slotOf(idYewShortStrung, 130712), slotOf(idBowString, 36368)})
	stock := bowstringplan.MuleStock(muleBankCount(r.ctx, "mule"))
	item, plan := bs.kitPlanFor(yew, ids, visit, stock)
	if item != "Yew shortbow id68" || !plan.Exhausted || len(plan.Lines) != 0 {
		t.Fatalf("a mule with only strung Yew shortbows: item %q plan %+v", item, plan)
	}
	// The control, as the code was: by the plain name the mule reads as holding 130,712.
	if old := bowstringplan.PlanOrder("Yew shortbow", 0, 0, 1000, stock); old.Exhausted || len(old.Lines) == 0 || old.Lines[0].Item != "Yew shortbow" {
		t.Fatalf("control: %+v", old)
	}

	// Unstrung ones held: the line names the unstrung obj, and at most what it holds.
	publishBowMule(r, cfg, []gamestate.SlotItem{slotOf(idYewShortStrung, 130712), slotOf(idYewShortUnstrung, 700), slotOf(idBowString, 36368)})
	stock = muleBankCount(r.ctx, "mule")
	item, plan = bs.kitPlanFor(yew, ids, visit, stock)
	if plan.Exhausted || len(plan.Lines) != 2 || plan.Lines[0].Item != "Yew shortbow id68" || plan.Lines[0].Qty != 700 ||
		plan.Lines[1].Item != bowstringplan.BowString || plan.Lines[1].Qty != 700 {
		t.Fatalf("a mule with 700 unstrung Yew shortbows: item %q plan %+v", item, plan)
	}

	// Through MasterCrafter's embedded round the stock is the adjusted one: the same plan.
	adj := mastercraftplan.AdjustedStock(mastercraftplan.Stock(stock), &mastercraftplan.Ledger{}, bowIDsOf(r.ctx))
	if _, plan := bs.kitPlanFor(yew, ids, visit, bowstringplan.MuleStock(adj)); plan.Exhausted || plan.Lines[0].Item != "Yew shortbow id68" || plan.Lines[0].Qty != 700 {
		t.Fatalf("through the adjusted stock: %+v", plan)
	}
}

// bowIDsOf resolves the bow names through the real object config.
func TestBowIDsOfResolvesEveryBowName(t *testing.T) {
	cfg := bowCfg(t)
	ctx := &Context{Config: cfg}
	ids := bowIDsOf(ctx)
	for _, name := range bowstringplan.DefaultBowNames() {
		un, st, ok := ids(name)
		if !ok || un == st || un <= 0 || st <= 0 {
			t.Errorf("%s: unstrung %d strung %d ok=%v", name, un, st, ok)
		}
	}
	if un, st, _ := ids("Yew shortbow"); un != idYewShortUnstrung || st != idYewShortStrung {
		t.Errorf("Yew shortbow: %d %d", un, st)
	}
	if _, _, ok := ids("Crossbow"); ok {
		t.Error("an unknown bow resolved")
	}
	if bowIDsOf(&Context{}) != nil || bowIDsOf(nil) != nil {
		t.Error("no config, no resolver")
	}
}

// The readers that sum or count a published list must not count a bow twice: the merged row and its
// per-obj rows are one stock.
func TestPublishedBowRowsAreCountedOnceByTheReaders(t *testing.T) {
	cfg := bowCfg(t)
	r := newSimRig(t, nil, nil, nil)
	r.ctx.Config = cfg
	items := []gamestate.SlotItem{
		slotOf(idYewShortStrung, 130712), slotOf(idYewLongUnstrung, 7922), slotOf(idYewLongStrung, 5519), slotOf(idBowString, 36368), slotOf(995, 12),
	}
	publishBowMule(r, cfg, items)

	// muleBankKinds: five distinct names are five rows, not the nine the per-obj rows would make.
	kinds, rows, known := muleBankKinds(r.ctx, "mule")
	if !known || rows != 4 || !kinds["yew shortbow"] || !kinds["yew longbow"] || !kinds["bow string"] || !kinds["coins"] {
		t.Fatalf("kinds %v rows %d known %v, want 4 rows (Yew shortbow, Yew longbow, Bow string, Coins)", kinds, rows, known)
	}
	for k := range kinds {
		if _, qualified := peerrunner.QualifiedBase(k); qualified {
			t.Errorf("the per-obj row %q is counted as a row of the mule's bank", k)
		}
	}

	// holdingsByID: each obj once, by id.
	var hs []peerbank.Holding
	for _, e := range r.ctx.Peers.Entries(time.Now()) {
		if e.Account == "mule" {
			hs = e.Bank
		}
	}
	by := holdingsByID(r.ctx, hs)
	if by[idYewShortStrung] != 130712 || by[idYewLongUnstrung] != 7922 || by[idYewLongStrung] != 5519 || by[idBowString] != 36368 {
		t.Fatalf("holdings by id %v: each obj is counted once under its own id", by)
	}
	if by[idYewShortUnstrung] != 0 {
		t.Fatalf("the unstrung Yew shortbow (obj %d) is not held: %d", idYewShortUnstrung, by[idYewShortUnstrung])
	}

	// muleStoreRows: the per-obj rows are the storage round's rows (and every bow row is protected).
	got := map[string]int{}
	for _, row := range muleStoreRows(r.ctx, "mule", cfg, nil) {
		got[row.Item] = row.Count
		if _, qualified := peerrunner.QualifiedBase(row.Item); qualified && !mastercraftplan.StoreProtected(row.Item) {
			t.Errorf("%q would be moved by the storage round", row.Item)
		}
	}
	if got["Yew shortbow"] != 0 || got["Yew shortbow id857"] != 130712 || got["Yew longbow id66"] != 7922 || got["Yew longbow id855"] != 5519 || got["Bow string"] != 36368 {
		t.Fatalf("store rows %v", got)
	}
}
