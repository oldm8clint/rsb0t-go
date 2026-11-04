package barcrawlrunner

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/barcrawlplan"
)

// The card draw against a simulated bank and the real item config (RSB0T_CONFIG_JAG; these skip without it).

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

// bankRig is an account at an open bank. pack and bank are the rows each holds; log collects the runner's lines.
func bankRig(t *testing.T, cfg *cachecfg.Config, pack, bank []actionsim.Stack) (*Deps, *actionsim.Session, *actions.Bank, *[]string) {
	t.Helper()
	sim := actionsim.New(actionsim.Objs{Stackable: func(id int32) bool { return id == barcrawlplan.CoinsID }}, pack)
	sim.OpenBank(bank)
	var log []string
	d := &Deps{
		Session: sim, Config: cfg, Components: actionsim.Components(),
		Log:   func(s string) { log = append(log, s) },
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
	}
	return d, sim, actions.NewBank(sim, d.Components), &log
}

func held(sim *actionsim.Session, id int32) int {
	n := 0
	for _, r := range sim.Pack() {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

func TestTheCardIDIsTheObjectTheNameResolvesTo(t *testing.T) {
	cfg := realConfig(t)
	ids := cfg.ObjIDs(barcrawlplan.CardName)
	if len(ids) != 1 || ids[0] != barcrawlplan.CardID {
		t.Fatalf("the cache resolves %q to %v, want exactly [%d]", barcrawlplan.CardName, ids, barcrawlplan.CardID)
	}
	if o, ok := cfg.Obj(barcrawlplan.CardID); !ok || !strings.EqualFold(o.Name, barcrawlplan.CardName) {
		t.Errorf("obj %d is %+v, want %q", barcrawlplan.CardID, o, barcrawlplan.CardName)
	}
}

// The live state of 15 Oct 2026: the card in the bank, the pack without it. Drawing it puts one in the pack and takes it
// out of the bank, and leaves the coins where they were.
func TestTakeCardFromDrawsTheBankedCard(t *testing.T) {
	cfg := realConfig(t)
	d, sim, bank, log := bankRig(t, cfg,
		[]actionsim.Stack{{ID: barcrawlplan.CoinsID, Count: 900}, {ID: 1265, Count: 1}},
		[]actionsim.Stack{{ID: barcrawlplan.CardID, Count: 1}, {ID: barcrawlplan.CoinsID, Count: 70000}})
	if CardCount(d) != 0 {
		t.Fatal("fixture: the pack already holds a card")
	}
	if !TakeCardFrom(d, bank) {
		t.Fatalf("TakeCardFrom reported no card in the pack: %v", *log)
	}
	if CardCount(d) != 1 || held(sim, barcrawlplan.CardID) != 1 {
		t.Errorf("the pack holds %d card(s), want 1", CardCount(d))
	}
	for _, r := range sim.Bank() {
		if r.ID == barcrawlplan.CardID {
			t.Errorf("the bank still holds %d card(s)", r.Count)
		}
	}
	if Coins(d) != 900 || held(sim, 1265) != 1 {
		t.Errorf("a pack with room was deposited from: coins %d, other item %d", Coins(d), held(sim, 1265))
	}
	if !strings.Contains(strings.Join(*log, "\n"), "drew 1 barcrawl card out of the bank") {
		t.Errorf("no live log line for the draw: %v", *log)
	}
}

// A pack with no room banks everything but the coins first (the card is one slot), never the purse.
func TestTakeCardFromAFullPackKeepsTheCoins(t *testing.T) {
	cfg := realConfig(t)
	pack := []actionsim.Stack{{ID: barcrawlplan.CoinsID, Count: 900}}
	for i := 0; i < 27; i++ {
		pack = append(pack, actionsim.Stack{ID: int32(1265 + i), Count: 1})
	}
	d, sim, bank, log := bankRig(t, cfg, pack, []actionsim.Stack{{ID: barcrawlplan.CardID, Count: 1}})
	if !TakeCardFrom(d, bank) {
		t.Fatalf("a full pack: no card drawn: %v", *log)
	}
	if held(sim, barcrawlplan.CardID) != 1 || Coins(d) != 900 {
		t.Errorf("card %d, coins %d: want the card drawn and the 900 coins kept", held(sim, barcrawlplan.CardID), Coins(d))
	}
	if !strings.Contains(strings.Join(*log, "\n"), "the pack is full") {
		t.Errorf("the room-making deposit was not logged: %v", *log)
	}
}

// A bank without the card draws nothing and says so (the lost-card case, which the guard answers with a new one).
func TestTakeCardFromABankWithoutTheCard(t *testing.T) {
	cfg := realConfig(t)
	d, sim, bank, log := bankRig(t, cfg, []actionsim.Stack{{ID: barcrawlplan.CoinsID, Count: 900}},
		[]actionsim.Stack{{ID: barcrawlplan.CoinsID, Count: 70000}})
	if TakeCardFrom(d, bank) {
		t.Fatal("TakeCardFrom reported a card from a bank that holds none")
	}
	if held(sim, barcrawlplan.CardID) != 0 || Coins(d) != 900 {
		t.Errorf("the pack changed: card %d coins %d", held(sim, barcrawlplan.CardID), Coins(d))
	}
	if !strings.Contains(strings.Join(*log, "\n"), "the bank holds no barcrawl card") {
		t.Errorf("an empty bank was not logged: %v", *log)
	}
}

// A card already in the pack is left alone: no second is drawn from a bank that somehow holds one too.
func TestTakeCardFromWithACardAlreadyHeld(t *testing.T) {
	cfg := realConfig(t)
	d, sim, bank, _ := bankRig(t, cfg, []actionsim.Stack{{ID: barcrawlplan.CardID, Count: 1}},
		[]actionsim.Stack{{ID: barcrawlplan.CardID, Count: 1}})
	if !TakeCardFrom(d, bank) {
		t.Fatal("a held card read as no card")
	}
	if held(sim, barcrawlplan.CardID) != 1 {
		t.Errorf("the pack holds %d cards, want the one it had", held(sim, barcrawlplan.CardID))
	}
}
