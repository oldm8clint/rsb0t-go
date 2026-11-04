package fisherplan

import (
	"reflect"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// Translated from rsb0t-scripts test/fishPlan.test.ts (this port's own edge cases, following the
// crafterplan/barbarianplan test style).

func intp(v int) *int { return &v }

func TestInCatherby(t *testing.T) {
	l := Catherby()
	edge := tile.Tile{X: l.Bank.X + l.BankRadius, Z: l.Bank.Z}
	if !InCatherby(edge) {
		t.Fatalf("expected %+v exactly at the radius to count", edge)
	}
	beyond := tile.Tile{X: l.Bank.X + l.BankRadius + 1, Z: l.Bank.Z}
	if InCatherby(beyond) {
		t.Fatalf("expected %+v one tile past the radius to not count", beyond)
	}
	// Seers' bank is 84 tiles from Catherby's, so it must not be mistaken for it.
	seers := tile.Tile{X: 2725, Z: 3493}
	if InCatherby(seers) {
		t.Fatal("the Seers bank must not read as Catherby")
	}
}

func TestTargetIndexAndLadderPosition(t *testing.T) {
	cases := []struct {
		target string
		want   int
	}{
		{"Small net", 0},
		{"Big net", 1},
		{"harpoon", 2}, // case-insensitive
		{" Lobster cage ", 3},
		{"Lobster cage", 3},
		{"Harpoon sharks", 4},
		{"nonexistent", 1}, // falls back to DefaultTarget = "Big net"
		{"", 1},
	}
	for _, c := range cases {
		if got := TargetIndex(c.target); got != c.want {
			t.Errorf("TargetIndex(%q) = %d, want %d", c.target, got, c.want)
		}
	}
	if got := TargetOptions(); len(got) != len(Rungs) || got[0] != "Small net" || got[4] != "Harpoon sharks" {
		t.Errorf("TargetOptions = %v", got)
	}
}

func TestToolNamesDeduped(t *testing.T) {
	got := ToolNames()
	want := []string{"Small fishing net", "Big fishing net", "Harpoon", "Lobster pot"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ToolNames = %v, want %v (Harpoon must appear once, not twice for the shark rung)", got, want)
	}
}

func TestCurrentRungIndexStopsAtTarget(t *testing.T) {
	cases := []struct {
		level  int
		target string
		want   int
	}{
		{1, DefaultTarget, 0},
		{15, DefaultTarget, 0},
		{16, DefaultTarget, 1},
		// Level 99 used to stop at Big net. The shark gate raises it now (EffectiveTarget),
		// which is the whole point of that change; 89 below shows the cap still binding
		// everywhere under the gate.
		{99, DefaultTarget, 4},
		{SharkAutoLevel - 1, DefaultTarget, 1},
		{40, "Lobster cage", 3},
		{76, "Harpoon sharks", 4},
		{75, "Harpoon sharks", 3}, // one level short of sharks, still Harpoon
	}
	for _, c := range cases {
		if got := CurrentRungIndex(c.level, c.target); got != c.want {
			t.Errorf("CurrentRungIndex(%d, %q) = %d, want %d", c.level, c.target, got, c.want)
		}
	}
}

func TestAtTargetAndNextRung(t *testing.T) {
	if AtTarget(1, DefaultTarget) {
		t.Fatal("level 1 has not reached Big net")
	}
	if !AtTarget(16, DefaultTarget) {
		t.Fatal("level 16 reaches Big net")
	}
	// The cap holds all the way to the shark gate, and no further. Level 99 with target
	// "Big net" used to be AtTarget; it is not any more, because SharkAutoLevel raises the
	// target for it (see EffectiveTarget). That is a deliberate contract change made on the
	// fleet owner's instruction, not a regression -- 119 live accounts at Fishing 76+ were
	// still netting because the cap was a setting nobody raised as they levelled.
	if !AtTarget(SharkAutoLevel-1, DefaultTarget) {
		t.Fatalf("level %d still stays at the target rung", SharkAutoLevel-1)
	}
	if AtTarget(SharkAutoLevel, DefaultTarget) {
		t.Fatalf("level %d should have been raised past %q to sharks", SharkAutoLevel, DefaultTarget)
	}
	next, ok := NextRung(1, DefaultTarget)
	if !ok || next.Label != "Big net" {
		t.Fatalf("NextRung(1, Big net) = %+v, ok=%v", next, ok)
	}
	if _, ok := NextRung(16, DefaultTarget); ok {
		t.Fatal("at the target, NextRung should report ok=false")
	}
}

func TestActiveRung(t *testing.T) {
	// The current rung's tool is owned.
	r, ok := ActiveRung(16, DefaultTarget, []string{"Big fishing net"})
	if !ok || r.Label != "Big net" {
		t.Fatalf("ActiveRung = %+v, ok=%v", r, ok)
	}
	// The current rung's tool is not owned yet, but a lower one is: fish with that while saving up.
	r, ok = ActiveRung(16, DefaultTarget, []string{"Small fishing net"})
	if !ok || r.Label != "Small net" {
		t.Fatalf("expected the fallback to Small net, got %+v ok=%v", r, ok)
	}
	// Nothing owned at all.
	if _, ok := ActiveRung(16, DefaultTarget, nil); ok {
		t.Fatal("no tools owned should report ok=false")
	}
	// Case-insensitive match.
	if _, ok := ActiveRung(1, DefaultTarget, []string{"small FISHING net"}); !ok {
		t.Fatal("tool ownership should match case-insensitively")
	}
}

func TestLadderView(t *testing.T) {
	view := LadderView(20, "Lobster cage")
	if len(view) != 4 {
		t.Fatalf("expected 4 rungs up to Lobster cage, got %d", len(view))
	}
	want := []RungState{RungDone, RungNow, RungNext, RungLater}
	for i, w := range want {
		if view[i].State != w {
			t.Errorf("rung %d (%s): state = %s, want %s", i, view[i].Label, view[i].State, w)
		}
	}
	if !view[3].Target {
		t.Error("the last entry should be marked as the target")
	}
	if view[0].Target || view[1].Target || view[2].Target {
		t.Error("only the target rung should be marked")
	}
}

func TestIsRawFish(t *testing.T) {
	cases := map[string]bool{
		"raw shrimps":  true,
		"Raw Lobster":  true,
		" raw cod ":    true,
		"shrimps":      false,
		"":             false,
		"cooked shark": false,
	}
	for name, want := range cases {
		if got := IsRawFish(name); got != want {
			t.Errorf("IsRawFish(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestFishValue(t *testing.T) {
	if v := FishValue("raw shrimps"); v != 5 {
		t.Errorf("raw shrimps = %d, want 5", v)
	}
	if v := FishValue("Raw Shark"); v != 300 {
		t.Errorf("Raw Shark = %d, want 300 (case-insensitive)", v)
	}
	if v := FishValue("raw unknown fish"); v != 5 {
		t.Errorf("unlisted raw fish should default to 5, got %d", v)
	}
}

func TestFishCount(t *testing.T) {
	items := []freshstart.PackItem{
		{Name: "raw shrimps", Count: 5},
		{Name: "raw lobster", Count: 0}, // a stack with count<1 counts as 1
		{Name: "Coins", Count: 500},
		{Name: "", Count: 0},
	}
	if got := FishCount(items); got != 6 {
		t.Errorf("FishCount = %d, want 6", got)
	}
}

func TestHarryPays(t *testing.T) {
	if got := HarryPays(100, 0); got != 70 {
		t.Errorf("HarryPays(100, 0) = %d, want 70", got)
	}
	if got := HarryPays(100, 10); got != 60 {
		t.Errorf("HarryPays(100, 10) = %d, want 60", got)
	}
	// Rate floors at 10% no matter how many are already stocked.
	if got := HarryPays(100, 1000); got != 10 {
		t.Errorf("HarryPays(100, 1000) = %d, want 10 (rate floors at 0.1)", got)
	}
}

func TestFishToSell(t *testing.T) {
	stock := []freshstart.PackItem{
		{Name: "raw shrimps", Count: 10},
		{Name: "raw lobster", Count: 3},
		{Name: "raw shark", Count: 1},
		{Name: "Coins", Count: 100}, // never offered
	}
	// The most valuable fish go first: shark (300), then lobster (150), then shrimps (5).
	plan := FishToSell(1000, stock, 28)
	if len(plan) == 0 || plan[0].Name != "raw shark" {
		t.Fatalf("expected shark drawn first, got %+v", plan)
	}
	// A room cap of 1 slot can only draw one fish, whichever is most valuable.
	oneSlot := FishToSell(1000, stock, 1)
	if len(oneSlot) != 1 || oneSlot[0].Count != 1 || oneSlot[0].Name != "raw shark" {
		t.Fatalf("room=1 should draw exactly one of the priciest fish, got %+v", oneSlot)
	}
	// need=0 draws nothing.
	if got := FishToSell(0, stock, 28); len(got) != 0 {
		t.Fatalf("need=0 should draw nothing, got %+v", got)
	}
	// No fish in stock at all.
	if got := FishToSell(100, []freshstart.PackItem{{Name: "Coins", Count: 500}}, 28); len(got) != 0 {
		t.Fatalf("no fish in stock should draw nothing, got %+v", got)
	}
	// Duplicate stacks of the same fish are combined.
	dupes := []freshstart.PackItem{{Name: "raw cod", Count: 2}, {Name: "raw cod", Count: 3}}
	got := FishToSell(1, dupes, 28)
	if len(got) != 1 || got[0].Count != 1 {
		t.Fatalf("expected one line for the combined cod stacks needing only 1, got %+v", got)
	}
}

func TestStarterKeep(t *testing.T) {
	keep := StarterKeep()
	for _, want := range []string{"Coins", "Small fishing net", "Harpoon", "Fishing rod", "Fly fishing rod"} {
		if !hasName(keep, want) {
			t.Errorf("StarterKeep missing %q: %v", want, keep)
		}
	}
}

func TestBankKeep(t *testing.T) {
	small := Rungs[0]
	big := Rungs[1]
	if got := BankKeep(false); got != nil {
		t.Errorf("no coins, no rungs should keep nothing, got %v", got)
	}
	if got := BankKeep(true); !reflect.DeepEqual(got, []string{"Coins"}) {
		t.Errorf("coins alone should keep just Coins, got %v", got)
	}
	got := BankKeep(true, &small, nil, &big)
	want := []string{"Coins", "Small fishing net", "Big fishing net"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BankKeep(true, small, nil, big) = %v, want %v (nil skipped, order preserved)", got, want)
	}
	// The same tool named twice should not duplicate.
	dup := BankKeep(false, &small, &small)
	if !reflect.DeepEqual(dup, []string{"Small fishing net"}) {
		t.Errorf("expected the repeated tool deduped, got %v", dup)
	}
}

func TestFishFundTarget(t *testing.T) {
	lumbridge := tile.Tile{X: 3222, Z: 3218}
	// On the mainland with a tool already: just the full fare.
	got := FishFundTarget(FishSnapshot{Tile: &lumbridge, Fare: 60, HeldTools: []string{"Small fishing net"}})
	if got != 60 {
		t.Errorf("mainland with a tool = %d, want 60", got)
	}
	// On the mainland with nothing to fish with: the fare plus a small net.
	got = FishFundTarget(FishSnapshot{Tile: &lumbridge, Fare: 60, HeldTools: nil})
	if got != 60+Rungs[0].ToolCost {
		t.Errorf("mainland with no tool = %d, want %d", got, 60+Rungs[0].ToolCost)
	}
	// Unknown position (nil tile): the full fare is assumed, same as freshstart.FareRemaining.
	got = FishFundTarget(FishSnapshot{Tile: nil, Fare: 60, HeldTools: []string{"Harpoon"}})
	if got != 60 {
		t.Errorf("nil tile = %d, want 60", got)
	}
}

// ---- DecideFishPhase ----

func TestDecideFishPhaseAcceptAndWaitAndTutorial(t *testing.T) {
	if got := DecideFishPhase(FishSnapshot{MainModal: freshstart.DesignModal}); got != PhaseAccept {
		t.Errorf("design modal open = %s, want accept", got)
	}
	if got := DecideFishPhase(FishSnapshot{Tile: nil}); got != PhaseWait {
		t.Errorf("no tile yet = %s, want wait", got)
	}
	tutorial := tile.Tile{X: 3100, Z: 3100}
	if got := DecideFishPhase(FishSnapshot{Tile: &tutorial}); got != PhaseTutorial {
		t.Errorf("on tutorial island = %s, want tutorial", got)
	}
}

func TestDecideFishPhaseLumbridgeStoreAndBright(t *testing.T) {
	lumbridge := tile.Tile{X: 3222, Z: 3218}
	// Sellable junk, brightness not yet done: turn it up first.
	got := DecideFishPhase(FishSnapshot{Tile: &lumbridge, Sellable: 2, BrightPending: true})
	if got != PhaseBright {
		t.Errorf("bright pending with sellable junk = %s, want bright", got)
	}
	// Sellable junk, brightness already done: sell it.
	got = DecideFishPhase(FishSnapshot{Tile: &lumbridge, Sellable: 2, BrightPending: false})
	if got != PhaseStore {
		t.Errorf("sellable junk, bright done = %s, want store", got)
	}
	// Nothing sellable: falls through to the fund/travel branch.
	got = DecideFishPhase(FishSnapshot{Tile: &lumbridge, Sellable: 0, Coins: 0, Fare: 60})
	if got != PhaseFund {
		t.Errorf("no sellable junk, short of the fare = %s, want fund", got)
	}
}

func TestDecideFishPhaseFundAndTravel(t *testing.T) {
	lumbridge := tile.Tile{X: 3222, Z: 3218}
	if got := DecideFishPhase(FishSnapshot{Tile: &lumbridge, Coins: 0, Fare: 60, HeldTools: nil}); got != PhaseFund {
		t.Errorf("short of the fare = %s, want fund", got)
	}
	if got := DecideFishPhase(FishSnapshot{Tile: &lumbridge, Coins: 1000, Fare: 60, HeldTools: []string{"Small fishing net"}}); got != PhaseTravel {
		t.Errorf("funded = %s, want travel", got)
	}
}

func TestDecideFishPhaseInCatherbyBankPaths(t *testing.T) {
	here := Catherby().Camp
	// The rung's tool is not owned, and the bank has never been read: look.
	got := DecideFishPhase(FishSnapshot{Tile: &here, Level: 1, Target: DefaultTarget, BankTools: nil})
	if got != PhaseBank {
		t.Errorf("unread bank, tool not owned = %s, want bank", got)
	}
	// Bank read, holds enough for the tool, pack not full, coins in the pack cover it: buy now.
	got = DecideFishPhase(FishSnapshot{
		Tile: &here, Level: 1, Target: DefaultTarget,
		BankTools: []string{}, Coins: 5, BankCoins: intp(0), PackFull: false,
	})
	if got != PhaseBuy {
		t.Errorf("coins in pack cover the tool = %s, want buy", got)
	}
	// Bank read, coins cover the tool only combined with the bank's: fetch them first.
	got = DecideFishPhase(FishSnapshot{
		Tile: &here, Level: 1, Target: DefaultTarget,
		BankTools: []string{}, Coins: 2, BankCoins: intp(10), PackFull: false,
	})
	if got != PhaseBank {
		t.Errorf("coins only cover the tool combined with the bank's = %s, want bank", got)
	}
	// Coins cover it but the pack is full: bank first to make room.
	got = DecideFishPhase(FishSnapshot{
		Tile: &here, Level: 1, Target: DefaultTarget,
		BankTools: []string{}, Coins: 5, BankCoins: intp(0), PackFull: true,
	})
	if got != PhaseBank {
		t.Errorf("pack full even with enough coins = %s, want bank", got)
	}
	// Short of coins for the tool, but fish are held or banked: sell them.
	got = DecideFishPhase(FishSnapshot{
		Tile: &here, Level: 1, Target: DefaultTarget,
		BankTools: []string{}, Coins: 0, BankCoins: intp(0), FishHeld: 3,
	})
	if got != PhaseSell {
		t.Errorf("short of coins with fish to sell = %s, want sell", got)
	}
	// Short of coins, no fish anywhere, but a lower rung's tool is owned: keep fishing with it.
	got = DecideFishPhase(FishSnapshot{
		Tile: &here, Level: 16, Target: DefaultTarget,
		BankTools: []string{}, Coins: 0, BankCoins: intp(0), FishHeld: 0, FishBanked: intp(0),
		HeldTools: []string{"Small fishing net"},
	})
	if got != PhaseFish {
		t.Errorf("no coins, no fish, but a lower tool owned = %s, want fish (falls back to it)", got)
	}
	// No tool at all, no coins, no fish: recover by pickpocketing, don't dead-end.
	//
	// This case asserted PhaseStuck until 2026-09-18, when the account owner reported
	// production account "spare28" (Fishing 55) sitting at the Catherby bank logging
	// "stuck — no Big fishing net, 0 coins and nothing to sell" every 10 seconds
	// indefinitely. PhaseStuck only logs and sleeps, so an established account that lost
	// its tool inside Catherby idled until an operator handed it coins — even though
	// PhaseFund (pickpocket for coins, no starting capital needed) was already
	// implemented and reachable for the same account anywhere OUTSIDE Catherby.
	// PickpocketStep walks itself to its own spot from wherever the account is, so
	// routing here is safe from Catherby too. See DecideFishPhase's own comment.
	got = DecideFishPhase(FishSnapshot{
		Tile: &here, Level: 1, Target: DefaultTarget,
		BankTools: []string{}, Coins: 0, BankCoins: intp(0), FishHeld: 0, FishBanked: intp(0),
	})
	if got != PhaseFund {
		t.Errorf("nothing to fish with, buy with or sell = %s, want fund (pickpocket to recover)", got)
	}
}

// A high-level account that loses its tool inside Catherby must recover by itself, the
// same way a fresh one does outside it — the real shape of the live "spare28" incident
// (Fishing 55, at the Catherby bank, empty bank, zero coins).
func TestEstablishedAccountInCatherbyRecoversInsteadOfStalling(t *testing.T) {
	here := Catherby().Bank
	got := DecideFishPhase(FishSnapshot{
		Tile: &here, Level: 55, Target: DefaultTarget,
		BankTools: []string{}, HeldTools: []string{},
		Coins: 0, BankCoins: intp(0), FishHeld: 0, FishBanked: intp(0),
	})
	if got == PhaseStuck {
		t.Fatal("an established Catherby account with no tool and no coins dead-ended on PhaseStuck instead of recovering")
	}
	if got != PhaseFund {
		t.Errorf("established Catherby account with nothing = %s, want fund", got)
	}
}

func TestDecideFishPhaseFishAndBank(t *testing.T) {
	here := Catherby().Camp
	// Tool owned, held (not just banked), pack not full, no coins to bank first: fish.
	got := DecideFishPhase(FishSnapshot{
		Tile: &here, Level: 1, Target: DefaultTarget,
		BankTools: []string{"Small fishing net"}, HeldTools: []string{"Small fishing net"},
		Coins: 0, PackFull: false,
	})
	if got != PhaseFish {
		t.Errorf("clear to fish = %s, want fish", got)
	}
	// Tool owned but sitting in the bank, not the pack yet: go get it.
	got = DecideFishPhase(FishSnapshot{
		Tile: &here, Level: 1, Target: DefaultTarget,
		BankTools: []string{"Small fishing net"}, HeldTools: nil,
		Coins: 0, PackFull: false,
	})
	if got != PhaseBank {
		t.Errorf("tool only in the bank = %s, want bank", got)
	}
	// Pack full: bank the catch before fishing on.
	got = DecideFishPhase(FishSnapshot{
		Tile: &here, Level: 1, Target: DefaultTarget,
		BankTools: []string{"Small fishing net"}, HeldTools: []string{"Small fishing net"},
		Coins: 0, PackFull: true,
	})
	if got != PhaseBank {
		t.Errorf("pack full = %s, want bank", got)
	}
	// Coins sitting in the pack are one slot: they ride along while fishing and are banked when the pack is full.
	got = DecideFishPhase(FishSnapshot{
		Tile: &here, Level: 1, Target: DefaultTarget,
		BankTools: []string{"Small fishing net"}, HeldTools: []string{"Small fishing net"},
		Coins: 1, PackFull: false,
	})
	if got != PhaseFish {
		t.Errorf("coins held while fishing = %s, want fish", got)
	}
	got = DecideFishPhase(FishSnapshot{
		Tile: &here, Level: 1, Target: DefaultTarget,
		BankTools: []string{"Small fishing net"}, HeldTools: []string{"Small fishing net"},
		Coins: 1, PackFull: true,
	})
	if got != PhaseBank {
		t.Errorf("a full pack with coins = %s, want bank", got)
	}
}

func TestDescribeFishPhaseCoversEveryPhase(t *testing.T) {
	all := []FishPhase{
		PhaseAccept, PhaseTutorial, PhaseWait, PhaseBright, PhaseStore, PhaseFund,
		PhaseTravel, PhaseBank, PhaseBuy, PhaseSell, PhaseFish, PhaseStuck,
		PhaseOpen, PhaseAskCoins, PhaseRestock,
	}
	seen := map[string]bool{}
	for _, p := range all {
		d := DescribeFishPhase(p)
		if d == "" || d == string(p) {
			t.Errorf("DescribeFishPhase(%s) looks unhandled: %q", p, d)
		}
		if seen[d] {
			t.Errorf("phase %s reuses another phase's description %q", p, d)
		}
		seen[d] = true
	}
}

// ---- the mule ----

func TestMuleRowsFishFirstToolsNeverLeave(t *testing.T) {
	bank := []donorplan.ItemRow{
		{Name: "Coins", Count: 500},
		{Name: "Small fishing net", Count: 1},
		{Name: "raw lobster", Count: 20},
		{Name: "Rune essence", Count: 10},
	}
	rows := MuleRows(bank, []string{"Coins"})
	if len(rows) != 2 {
		t.Fatalf("expected fish and the essence to leave, coins and the tool to stay: %+v", rows)
	}
	if rows[0].Name != "raw lobster" {
		t.Errorf("fish should be first in the hand-over, got %+v", rows[0])
	}
	for _, r := range rows {
		if strings.EqualFold(r.Name, "Small fishing net") || strings.EqualFold(r.Name, "Coins") {
			t.Errorf("tools and kept names must never appear in the hand-over: %+v", r)
		}
	}
}

func TestMuleHaulAndLoad(t *testing.T) {
	pack := []donorplan.ItemRow{
		{Name: "raw shark", Count: 1},
		{Name: "Harpoon", Count: 1},
		{Name: "Coins", Count: 1},
	}
	if got := MuleHaul(pack, []string{"Coins"}); got != 1 {
		t.Errorf("MuleHaul = %d, want 1 (only the fish counts)", got)
	}
	load := MuleLoad(pack, 28, []string{"Coins"})
	if len(load) != 1 || load[0].Name != "raw shark" {
		t.Fatalf("MuleLoad = %+v, want just the fish", load)
	}
}

// The live hot loop of 2026-09-18: production account "spare28" (Fishing 55) stood at
// 2811,3441 — two tiles from the Catherby bank — with no usable tool, no coins and nothing
// to sell. DecideFishPhase correctly returned PhaseFund so it would pickpocket its way
// back, the caller's redirect saw the bank was trivially reachable and flipped it to
// PhaseTravel, SailLeg answered "already on the destination side" and returned instantly,
// and the phase recomputed to PhaseFund again — logging that line many times a second at
// 0 XP/hour. The redirect is only ever meant for an account displaced OUT of the bank's
// radius, so being at the bank must never trigger it.
func TestFundIsReallyJustWalkHomeOnlyWhenActuallyDisplaced(t *testing.T) {
	atBank := Catherby().Bank
	twoTilesOff := tile.Tile{X: atBank.X + 2, Z: atBank.Z}
	displaced := tile.Tile{X: atBank.X + Catherby().BankRadius + 25, Z: atBank.Z}

	if FundIsReallyJustWalkHome(PhaseFund, &twoTilesOff, true) {
		t.Fatal("an account standing at the bank was redirected to PhaseTravel — this is the live hot loop")
	}
	if FundIsReallyJustWalkHome(PhaseFund, &atBank, true) {
		t.Fatal("an account exactly on the bank tile was redirected to PhaseTravel")
	}
	if !FundIsReallyJustWalkHome(PhaseFund, &displaced, true) {
		t.Fatal("a genuinely displaced account with a walkable bank was NOT redirected — that is the case the redirect exists for")
	}
	if FundIsReallyJustWalkHome(PhaseFund, &displaced, false) {
		t.Fatal("redirected despite the bank being unreachable; that account really does need to fund a boat")
	}
	if FundIsReallyJustWalkHome(PhaseFish, &displaced, true) {
		t.Fatal("redirected a phase that was never PhaseFund")
	}
	if FundIsReallyJustWalkHome(PhaseFund, nil, true) {
		t.Fatal("redirected with an unknown position")
	}
}

func TestACrystalKeyGoesToTheMuleUnlessTheCallerKeepsIt(t *testing.T) {
	bank := []donorplan.ItemRow{
		{Name: "Raw lobster", Count: 100},
		{Name: "Crystal key", Count: 6},
		{Name: "Half of a key", Count: 3},
	}
	// INVERTED 2026-09-23, and the assertion this replaces is now the bug it guards against.
	//
	// It used to require that NO fisher ever hand a crystal key over, on NeverHandOver's own
	// premise that "a fisher not running LootChestFisher never holds one (only the mule joins
	// the halves)". That premise broke: internal/bot/fisher.go's joinHalvesBeforeHandoff runs
	// before every hand-off, and acct118 logged "joined 2 key half pair(s) into crystal
	// key(s) before handing over" at 23:48:44 on 2026-09-22 and then kept every key it had
	// just made -- while the mule sat on 69 keys and spare104 could not start a chest run, which
	// needs 100.
	//
	// Keeping keys is now a property of the ROLE (fisherBot.keepCrystalKeys, set only by the
	// LootChestFisher delegate, which holds keys the MULE gave it), expressed through the
	// caller's own keep list. So the plan hands them over by default and honours a keep.
	var sawKey, sawHalf bool
	for _, row := range MuleRows(bank, nil) {
		switch row.Name {
		case "Crystal key":
			sawKey = true
		case "Half of a key":
			sawHalf = true
		}
	}
	if !sawKey {
		t.Fatal("an ordinary fisher did not draw its crystal keys for the mule — joining halves " +
			"into keys for the mule is pointless if it then keeps them")
	}
	if !sawHalf {
		t.Fatal("key halves must still go to the mule — joining them is its job")
	}
	// The LootChestFisher delegate passes NeverHandOver in its own keep list and still keeps
	// them; without that the pair would trade the same keys in a circle.
	for _, row := range MuleRows(bank, NeverHandOver) {
		if row.Name == "Crystal key" {
			t.Fatal("a caller that asked to keep crystal keys had them drawn for the mule anyway")
		}
	}
	pack := []donorplan.ItemRow{{Name: "Crystal key", Count: 6}}
	if got := MuleHaul(pack, NeverHandOver); got != 0 {
		t.Fatalf("MuleHaul counts %d slot(s) of KEPT crystal keys as haul, want 0", got)
	}
	if got := MuleLoad(pack, 28, NeverHandOver); len(got) != 0 {
		t.Fatalf("MuleLoad offers kept keys %+v, want nothing", got)
	}
	if got := MuleHaul(pack, nil); got == 0 {
		t.Fatal("MuleHaul counts a pack of keys as no haul for an ordinary fisher — it is " +
			"exactly what the mule is waiting for")
	}
}

// The automatic shark gate, asked for by the fleet owner on 2026-09-21: "I want them doing
// sharks at 90+ automatically once they hit 90 fishing without me changing it, just how they go
// to the fishing guild at 68."
func TestSharkGateRaisesTheTargetAtNinety(t *testing.T) {
	sharks := TargetIndex(SharkRung)
	for _, c := range []struct {
		level int
		want  int
		why   string
	}{
		{SharkAutoLevel, sharks, "at the gate, target Big net, must be sharks"},
		{99, sharks, "well past the gate"},
		{SharkAutoLevel - 1, 1, "one level short of the gate: the configured Big net cap still binds"},
	} {
		if got := CurrentRungIndex(c.level, DefaultTarget); got != c.want {
			t.Errorf("level %d with target %q -> rung %d, want %d (%s)", c.level, DefaultTarget, got, c.want, c.why)
		}
	}
	// Below the gate the configured target is still obeyed exactly as before.
	if got := CurrentRungIndex(20, "Big net"); got != 1 {
		t.Errorf("level 20 capped at Big net -> %d, want 1", got)
	}
	// And a target ABOVE sharks is impossible, so the gate can never lower anything.
	if EffectiveTarget(99, SharkRung) != SharkRung {
		t.Error("the gate must not change a target already at sharks")
	}
}

// Every bank visit says why, and a pack with a few slots of loot, coins and the kit is not a reason by itself.
func TestBankReasonAndTheCoinRule(t *testing.T) {
	here := Catherby().Bank
	base := FishSnapshot{Tile: &here, Level: 60, Target: "Big net", HeldTools: []string{"Big fishing net"}, BankTools: []string{"Small fishing net"}}
	if got := DecideFishPhase(base); got != PhaseFish {
		t.Fatalf("a pack holding the tool = %s, want fish", got)
	}
	base.Coins = 300 // a casket's coins
	if got := DecideFishPhase(base); got != PhaseFish {
		t.Fatalf("coins from a casket = %s, want fish (no bank trip for one slot)", got)
	}
	base.PackFull = true
	if got := DecideFishPhase(base); got != PhaseBank || BankReason(base) != "the pack is full" {
		t.Fatalf("a full pack = %s, reason %q", got, BankReason(base))
	}
	lost := base
	lost.PackFull, lost.HeldTools = false, nil
	lost.BankTools = []string{"Big fishing net"}
	if got := DecideFishPhase(lost); got != PhaseBank || !strings.Contains(BankReason(lost), "Big fishing net is not in the pack") {
		t.Fatalf("tool banked = %s, reason %q", got, BankReason(lost))
	}
	unread := FishSnapshot{Tile: &here, Level: 1, Target: DefaultTarget}
	if got := DecideFishPhase(unread); got != PhaseBank || BankReason(unread) != "the bank has not been read yet" {
		t.Fatalf("unread bank = %s, reason %q", got, BankReason(unread))
	}
}
