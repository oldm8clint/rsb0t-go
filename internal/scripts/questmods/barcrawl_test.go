package questmods

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/barcrawlplan"
	"rsb0t-go/internal/scripts/barcrawlrunner"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The Barcrawl's card is in the bank when a run starts (everything between two runs banks what it does not keep), the
// guard issues no second one while that is so, and nothing used to draw it: live 13-15 Oct 2026, 2,196 guard talks
// in the logs scanned, every one "cards in pack: 0". These tests pin the draw (a bank read, then a withdrawal, then the tour), the
// coin ask the drinks need, and the keep lists that stop the engine's own deposits banking the card and the coins again.

// bcSnap is a snapshot of an account standing at the Draynor bank.
func bcSnap(coins int, mutate func(*questplan.Snapshot)) questplan.Snapshot {
	s := questplan.Snapshot{
		InvIDs: map[int]int{}, WornIDs: map[int]int{}, Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{},
		FreeSlots: 20, Tile: tile.Tile{X: 3093, Z: 3243}, TileKnown: true,
	}
	if coins > 0 {
		s.InvIDs[questplan.CoinsID] = coins
	}
	if mutate != nil {
		mutate(&s)
	}
	return s
}

func bankHolds(ids map[int]int) func(*questplan.Snapshot) {
	return func(s *questplan.Snapshot) { s.BankKnown, s.BankIDs = true, ids }
}

func hasID(ids []int, want int) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestBarcrawlDrawsTheCardFromTheBankBeforeTheTour(t *testing.T) {
	float := barcrawlplan.CoinFloat
	cases := []struct {
		name string
		snap questplan.Snapshot
		want questplan.StepKind
		why  string
	}{
		{"finished", bcSnap(float, func(s *questplan.Snapshot) { s.Ran[customBarcrawl] = true }), questplan.StepDone, "the tour ran: done"},
		{"card in the pack", bcSnap(float, func(s *questplan.Snapshot) { s.InvIDs[barcrawlplan.CardID] = 1 }), questplan.StepCustom, "straight to the tour"},
		{"card in the pack, purse spent down", bcSnap(50, func(s *questplan.Snapshot) { s.InvIDs[barcrawlplan.CardID] = 1 }), questplan.StepCustom, "a card held means the coins are already out"},
		{"bank unread", bcSnap(float, nil), questplan.StepScanBank, "the card may be in the bank: read it first"},
		{"card banked", bcSnap(float, bankHolds(map[int]int{barcrawlplan.CardID: 1, questplan.CoinsID: 70000})), questplan.StepWithdraw, "draw the card"},
		{"no card anywhere", bcSnap(float, bankHolds(map[int]int{questplan.CoinsID: 70000})), questplan.StepCustom, "the guard issues (or reissues) one"},
		{"short of coins, bank unread", bcSnap(100, nil), questplan.StepProvision, "the coin ask comes first and reads the bank"},
		{"short of coins, card banked", bcSnap(100, bankHolds(map[int]int{barcrawlplan.CardID: 1, questplan.CoinsID: 70000})), questplan.StepProvision, "coins first, with the card still in the bank"},
		{"coins provisioned, card banked", bcSnap(float, func(s *questplan.Snapshot) {
			bankHolds(map[int]int{barcrawlplan.CardID: 1})(s)
			s.Ran["provision:"+KitBarcrawlCoins] = true
		}), questplan.StepWithdraw, "the kit ran: draw the card"},
		{"coin ask ran but the purse is still short", bcSnap(300, func(s *questplan.Snapshot) {
			bankHolds(map[int]int{barcrawlplan.CardID: 1})(s)
			s.Ran["provision:"+KitBarcrawlCoins] = true
		}), questplan.StepWithdraw, "the ask is made once a run: draw the card with what there is"},
	}
	for _, c := range cases {
		step := decideBarcrawl(c.snap)
		if step.Kind != c.want {
			t.Errorf("%s: %s, want %s (%s)", c.name, step.Kind, c.want, c.why)
		}
		if why := questplan.ValidateStep(step); why != "" {
			t.Errorf("%s: malformed step: %s", c.name, why)
		}
	}
}

func TestBarcrawlWithdrawsExactlyTheCardAndKeepsItAndTheCoins(t *testing.T) {
	step := decideBarcrawl(bcSnap(barcrawlplan.CoinFloat, bankHolds(map[int]int{barcrawlplan.CardID: 1})))
	if step.Kind != questplan.StepWithdraw || len(step.Items) != 1 {
		t.Fatalf("step %+v, want one withdrawal", step)
	}
	if it := step.Items[0]; it.ID != barcrawlplan.CardID || it.Qty != 1 || it.Name != barcrawlplan.CardName {
		t.Errorf("withdrawal %+v, want exactly one %s (obj %d)", it, barcrawlplan.CardName, barcrawlplan.CardID)
	}
	if !hasID(step.KeepIDs, barcrawlplan.CardID) || !hasID(step.KeepIDs, questplan.CoinsID) {
		t.Errorf("the withdrawal's keep list %v must hold the card and the coins: making room for the card must not bank the purse", step.KeepIDs)
	}
}

func TestBarcrawlCoinAskIsEightHundredAndKeepsTheCard(t *testing.T) {
	step := decideBarcrawl(bcSnap(120, nil))
	if step.Kind != questplan.StepProvision || step.KitName != KitBarcrawlCoins || len(step.Kit) != 1 {
		t.Fatalf("step %+v, want the coin kit", step)
	}
	if l := step.Kit[0]; l.ID != questplan.CoinsID || l.Qty != 800 || !l.Stackable {
		t.Errorf("kit line %+v, want 800 stackable coins (the first full tour spent 448; the float is the owner's 800)", l)
	}
	if !hasID(step.KeepIDs, barcrawlplan.CardID) || !hasID(step.KeepIDs, questplan.CoinsID) {
		t.Errorf("the coin ask's keep list %v must hold the card and the coins", step.KeepIDs)
	}
}

func TestBarcrawlBanksAtTheNearestBank(t *testing.T) {
	// An account at the Seers' bank reads and draws there, not at the module's Draynor default.
	at := func(s *questplan.Snapshot) { s.Tile, s.TileKnown = tile.Tile{X: 2725, Z: 3491}, true }
	scan := decideBarcrawl(bcSnap(barcrawlplan.CoinFloat, at))
	if scan.Kind != questplan.StepScanBank || !scan.BankSet || scan.Bank != questplan.NearestBank(tile.Tile{X: 2725, Z: 3491}).Tile {
		t.Errorf("scan %+v, want the bank nearest the account", scan)
	}
	// Before a position packet has arrived the step names no bank: the engine's default stands.
	blind := decideBarcrawl(bcSnap(barcrawlplan.CoinFloat, func(s *questplan.Snapshot) { s.TileKnown = false }))
	if blind.Kind != questplan.StepScanBank || blind.BankSet {
		t.Errorf("with no position the scan %+v must leave the bank to the engine's default", blind)
	}
}

func TestBarcrawlModuleKeepsTheCardAndTheCoinsInEveryEngineDeposit(t *testing.T) {
	m := Barcrawl().Module
	if !hasID(m.ToolIDs, barcrawlplan.CardID) || !hasID(m.ToolIDs, questplan.CoinsID) {
		t.Errorf("Module.ToolIDs %v must hold the card and the coins: the engine's keep function reads them for every deposit", m.ToolIDs)
	}
	named := false
	for _, n := range m.Tools {
		if strings.Contains(strings.ToLower(barcrawlplan.CardName), strings.ToLower(n)) {
			named = true
		}
	}
	if !named {
		t.Errorf("Module.Tools %v no longer names %q", m.Tools, barcrawlplan.CardName)
	}
}

// ---- through the real engine, with a simulated bank ----

// bcJunk is the first of 27 distinct, non-stackable objects that fill the pack.
const bcJunk = 1265

// bcRig is an account whose pack is full (27 distinct items and the coins), standing at an open bank that holds the
// barcrawl card. Its engine reads the bank as a step before the run did (the view is stamped ahead of the run's start so
// the snapshot trusts it), and the tour is the test's.
func bcRig(t *testing.T, tour func(d *barcrawlrunner.Deps) bool) (*questrunner.Engine, *actionsim.Session, *[]string) {
	t.Helper()
	var pack []actionsim.Stack
	for i := 0; i < 27; i++ {
		pack = append(pack, actionsim.Stack{ID: int32(bcJunk + i), Count: 1})
	}
	pack = append(pack, actionsim.Stack{ID: questplan.CoinsID, Count: 900})
	sim := actionsim.New(actionsim.Objs{Stackable: func(id int32) bool { return id == questplan.CoinsID }}, pack)
	sim.OpenBank([]actionsim.Stack{{ID: barcrawlplan.CardID, Count: 1}, {ID: questplan.CoinsID, Count: 70000}})
	var log []string
	d := &questrunner.Deps{
		Session: sim, Components: actionsim.Components(),
		Log:   func(s string) { log = append(log, s) },
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
	}
	prov := &questrunner.Provisioner{Bank: &questrunner.BankView{
		Counts: map[int]int{barcrawlplan.CardID: 1, questplan.CoinsID: 70000}, Known: true, At: time.Now().Add(time.Hour),
	}}
	e := &questrunner.Engine{D: d, Prov: prov, Store: questplan.NewStore()}
	old := barcrawlTour
	barcrawlTour = func(d *barcrawlrunner.Deps, _ barcrawlrunner.Progress) bool { return tour(d) }
	t.Cleanup(func() { barcrawlTour = old })
	return e, sim, &log
}

func packHolds(sim *actionsim.Session, id int32) int {
	n := 0
	for _, r := range sim.Pack() {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

// The whole chain on a full pack: decide asks for the card, the engine banks the junk to make room (and keeps the purse),
// draws the card, the tour finds it in the pack, the hand-in is recorded and the run ends complete with the ladder reading
// the quest done. Before this fix the run never got past the guard, and a finished tour would still have read incomplete.
func TestBarcrawlDrawsTheCardMakingRoomAndCompletes(t *testing.T) {
	var cardAtTour, coinsAtTour int
	e, sim, log := bcRig(t, func(d *barcrawlrunner.Deps) bool {
		cardAtTour, coinsAtTour = questrunner.HeldID(d, barcrawlplan.CardID), questrunner.HeldID(d, questplan.CoinsID)
		return true
	})
	q := Barcrawl()
	res := e.Run(q, time.Minute)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, *log)
	}
	if cardAtTour != 1 {
		t.Errorf("the tour began with %d barcrawl card(s) in the pack, want the one drawn from the bank: %v", cardAtTour, *log)
	}
	if coinsAtTour != 900 {
		t.Errorf("the tour began with %d coins, want the 900 the account carried: making room banked the purse", coinsAtTour)
	}
	if got := packHolds(sim, barcrawlplan.CardID); got != 1 {
		t.Errorf("the pack holds %d cards after the run, want 1 (the stub tour does not hand it in)", got)
	}
	if packHolds(sim, bcJunk) != 0 {
		t.Errorf("the junk is still in the pack: nothing made room for the card")
	}
	if !q.Done(e) {
		t.Errorf("the quest reads incomplete after its tour was handed in and the run ended complete: %v", *log)
	}
	joined := strings.Join(*log, "\n")
	if !strings.Contains(joined, "withdraw Barcrawl card x1") {
		t.Errorf("the engine never logged the card's withdrawal: %v", *log)
	}
	if !strings.Contains(joined, "barcrawl: starting with 1 card(s) and 900 coins in the pack") {
		t.Errorf("the tour's start line is missing or wrong: %v", *log)
	}
}

// A tour that fails (the guard did not answer, a bar would not sign) is not complete, and the quest is not recorded done.
func TestBarcrawlAFailedTourIsNotRecordedDone(t *testing.T) {
	e, _, _ := bcRig(t, func(*barcrawlrunner.Deps) bool { return false })
	q := Barcrawl()
	res := e.Run(q, 3*time.Second)
	if res.Outcome == questrunner.OutcomeComplete || q.Done(e) {
		t.Fatalf("a failed tour ended %s with Done %v", res.Outcome, q.Done(e))
	}
}
