package naturepairplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func i64(n int64) *int64   { return &n }
func str(s string) *string { return &s }

func TestParseMasterWait(t *testing.T) {
	if ParseMasterWait("Inside") != WaitInside {
		t.Error("Inside should parse to WaitInside")
	}
	if ParseMasterWait("Outside") != WaitOutside {
		t.Error("Outside should parse to WaitOutside")
	}
	if ParseMasterWait("Inside, out on signal") != WaitSignal {
		t.Error("anything else should default to WaitSignal")
	}
}

func TestSignalFresh(t *testing.T) {
	if SignalFresh(nil, 1000, SignalTTLMs) {
		t.Error("a nil sentAt should never be fresh")
	}
	if !SignalFresh(i64(1000), 1000+SignalTTLMs-1, SignalTTLMs) {
		t.Error("just under the ttl should be fresh")
	}
	if SignalFresh(i64(1000), 1000+SignalTTLMs, SignalTTLMs) {
		t.Error("at or past the ttl should be stale")
	}
}

func TestOnKaramjaAndInTemple(t *testing.T) {
	surface := tile.Tile{X: 2800, Z: 3000, Level: 0}
	if !OnKaramja(&surface) {
		t.Error("a tile inside Karamja's box should be on the island")
	}
	temple := tile.Tile{X: 2865, Z: 5000, Level: 0}
	if !InTemple(&temple) {
		t.Error("a tile past TempleZ should read as in the temple")
	}
	if !OnKaramja(&temple) {
		t.Error("the temple should count as on Karamja too")
	}
	elsewhere := tile.Tile{X: 3200, Z: 3200, Level: 0}
	if OnKaramja(&elsewhere) {
		t.Error("Lumbridge should not read as on Karamja")
	}
	if OnKaramja(nil) {
		t.Error("a nil tile should never be on Karamja")
	}
}

func TestSpiderSafeVia(t *testing.T) {
	far := tile.Tile{X: 2900, Z: 3200, Level: 0}
	if !SpiderSafeVia(&far, Store) {
		t.Error("a walk into the store from far away should route via the spider-safe tile")
	}
	near := tile.Tile{X: 2768, Z: 3123, Level: 0}
	if SpiderSafeVia(&near, Store) {
		t.Error("already standing at the destination should not detour")
	}
	atSafe := SpiderSafe
	if SpiderSafeVia(&atSafe, Store) {
		t.Error("already at the spider-safe tile should not detour again")
	}
	if SpiderSafeVia(&far, RunnerBank) {
		t.Error("a walk to a destination that is neither the store nor the ruins should never detour")
	}
}

func TestCountUnnotedAndNoted(t *testing.T) {
	rows := []ItemRow{
		{Name: "Rune essence", ID: EssenceID, Count: 5},
		{Name: "Rune essence", ID: 9999, Count: 20},
		{Name: "Coins", ID: 995, Count: 100},
	}
	if got := CountUnnoted(rows); got != 5 {
		t.Errorf("CountUnnoted = %d, want 5", got)
	}
	if got := CountNoted(rows); got != 20 {
		t.Errorf("CountNoted = %d, want 20", got)
	}
}

func TestPlanStoreStep(t *testing.T) {
	if s := PlanStoreStep(50, 10, 10, TradeCap); s.Op != "buy" || s.N != 15 {
		t.Errorf("plentiful stock should buy the shortfall: got %+v", s)
	}
	if s := PlanStoreStep(5, 20, 10, TradeCap); s.Op != "sell" {
		t.Errorf("low stock with notes to sell should sell first: got %+v", s)
	}
	if s := PlanStoreStep(0, 0, 25, TradeCap); s.Op != "done" {
		t.Errorf("a full batch already unnoted should be done: got %+v", s)
	}
	if s := PlanStoreStep(0, 0, 10, TradeCap); s.Op != "done" {
		t.Errorf("nothing to sell and nothing in stock should be done: got %+v", s)
	}
}

func TestBatchToOffer(t *testing.T) {
	if count, all := BatchToOffer(10, TradeCap); count != 10 || !all {
		t.Errorf("held less than a batch should offer all of it: got %d,%v", count, all)
	}
	if count, all := BatchToOffer(40, TradeCap); count != TradeCap || all {
		t.Errorf("held more than a batch should cap at the batch: got %d,%v", count, all)
	}
}

func TestEssenceNeeded(t *testing.T) {
	if got := EssenceNeeded(25, 0, 0); got != 25 {
		t.Errorf("empty-handed should ask for a full batch, got %d", got)
	}
	if got := EssenceNeeded(25, 25, 0); got != 0 {
		t.Errorf("already holding a full batch of notes should ask for nothing, got %d", got)
	}
	if got := EssenceNeeded(25, 0, 30); got != 20 {
		t.Errorf("carrying a surplus over the batch should ask for less, got %d", got)
	}
}

func TestMasterCoinTargetAndShort(t *testing.T) {
	if got := MasterCoinTarget(50, 25, 100); got != 2*100+MasterFareCoins {
		t.Errorf("MasterCoinTarget = %d", got)
	}
	if got := MasterCoinTarget(0, 25, 100); got != MasterFareCoins {
		t.Errorf("no notes should need no hand-out coins, got %d", got)
	}
	if !MasterCoinsShort(50, 100) {
		t.Error("50 coins with 100 per trade should be short")
	}
	if MasterCoinsShort(50, 0) {
		t.Error("0 coins per trade (notes-only) should never be short")
	}
}

func TestAntipoisonHelpers(t *testing.T) {
	if !IsAntipoison("Antipoison(4)") {
		t.Error("should classify an antipoison dose")
	}
	if name, ok := AntipoisonFromStock([]string{"Pot", "Antipoison(2)"}); !ok || name != "Antipoison(2)" {
		t.Errorf("AntipoisonFromStock = %q,%v", name, ok)
	}
	if _, ok := AntipoisonFromStock([]string{"Pot"}); ok {
		t.Error("no antipoison in stock should report false")
	}
}

func TestIsDroppableLitter(t *testing.T) {
	keep := []string{"Rune essence", "Coins", "Lobster"}
	if !IsDroppableLitter("Grapes", keep, []string{"Drop", "Eat"}) {
		t.Error("an unlisted item offering Drop should be litter")
	}
	if IsDroppableLitter("Lobster", keep, []string{"Drop", "Eat"}) {
		t.Error("a kept item should never be litter")
	}
	if IsDroppableLitter("Strange box", keep, []string{"Open"}) {
		t.Error("an event-held puzzle item should never be litter")
	}
	if IsDroppableLitter("Grapes", keep, []string{"Eat"}) {
		t.Error("an item with no Drop option should not be droppable")
	}
}

func TestDecideRunnerStage(t *testing.T) {
	cases := []struct {
		name string
		s    RunnerSnapshot
		want RunnerStage
	}{
		{"dead", RunnerSnapshot{Dead: true}, RunnerRecover},
		{"poisoned with a cure", RunnerSnapshot{Poisoned: true, CureHeld: true}, RunnerCure},
		{"needs to eat", RunnerSnapshot{NeedEat: true}, RunnerEat},
		{"resting, no food", RunnerSnapshot{Resting: true, Food: 0}, RunnerRest},
		{"stranded off the island", RunnerSnapshot{OnIsland: false, Coins: 10}, RunnerStranded},
		{"holding essence", RunnerSnapshot{OnIsland: true, Unnoted: 5}, RunnerDeliver},
		{"noted essence, still in the temple", RunnerSnapshot{OnIsland: true, InTemple: true, Noted: 10}, RunnerReturn},
		{"noted essence, outside", RunnerSnapshot{OnIsland: true, Noted: 10}, RunnerUnnote},
		{"low on food, can afford the trip", RunnerSnapshot{OnIsland: true, Food: 0, FoodReserve: 2, Coins: 100, NearBank: true}, RunnerRestock},
		{"nothing to do", RunnerSnapshot{OnIsland: true, Food: 5, FoodReserve: 2}, RunnerCollect},
	}
	for _, c := range cases {
		if got := DecideRunnerStage(c.s); got != c.want {
			t.Errorf("%s: DecideRunnerStage = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestRunnerCanAffordTrip(t *testing.T) {
	if !RunnerCanAffordTrip(ShipFare, true) {
		t.Error("near the bank, one fare should be enough")
	}
	if RunnerCanAffordTrip(ShipFare, false) {
		t.Error("off the island, one fare alone should not be enough for a round trip")
	}
	if !RunnerCanAffordTrip(RunnerTripFare, false) {
		t.Error("off the island, two fares should be enough")
	}
}

func TestRunnerWaitsForCoins(t *testing.T) {
	if RunnerWaitsForCoins(10, nil, 0) {
		t.Error("coins already up should never wait")
	}
	if !RunnerWaitsForCoins(0, nil, 0) {
		t.Error("no coins and no notes-seen timestamp should wait (grace not started)")
	}
	if !RunnerWaitsForCoins(0, i64(1000), 1000+CoinsGraceMs-1) {
		t.Error("within the grace window should still wait")
	}
	if RunnerWaitsForCoins(0, i64(1000), 1000+CoinsGraceMs) {
		t.Error("past the grace window should stop waiting")
	}
}

func TestDecideMasterStage(t *testing.T) {
	cases := []struct {
		name string
		s    MasterSnapshot
		want MasterStage
	}{
		{"dead", MasterSnapshot{Dead: true}, MasterRecover},
		{"no talisman", MasterSnapshot{HasTalisman: false}, MasterNoTalisman},
		{"crafting", MasterSnapshot{HasTalisman: true, InTemple: true, Essence: 5}, MasterCraft},
		{"needs the bank", MasterSnapshot{HasTalisman: true, BankTrips: true, Noted: 0, Essence: 0}, MasterBank},
		{"resting outside heads in", MasterSnapshot{HasTalisman: true, Resting: true, InTemple: false}, MasterEnter},
		{"resting inside waits", MasterSnapshot{HasTalisman: true, Resting: true, InTemple: true}, MasterStageWait},
		{"outside wait mode exits", MasterSnapshot{HasTalisman: true, InTemple: true, WaitMode: WaitOutside}, MasterExit},
		{"signalled with notes exits", MasterSnapshot{HasTalisman: true, InTemple: true, WaitMode: WaitSignal, Signal: true, Noted: 5}, MasterExit},
		{"inside wait mode stays", MasterSnapshot{HasTalisman: true, InTemple: true, WaitMode: WaitInside}, MasterStageWait},
		{"enters when inside mode and outside", MasterSnapshot{HasTalisman: true, WaitMode: WaitInside}, MasterEnter},
		{"signal mode enters to listen", MasterSnapshot{HasTalisman: true, WaitMode: WaitSignal}, MasterEnter},
		{"signal mode waits once a runner is visible", MasterSnapshot{HasTalisman: true, WaitMode: WaitSignal, RunnerVisible: true}, MasterStageWait},
	}
	for _, c := range cases {
		if got := DecideMasterStage(c.s); got != c.want {
			t.Errorf("%s: DecideMasterStage = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestMasterNextOffer(t *testing.T) {
	if got := MasterNextOffer(MasterOfferState{HandOut: 0}); got != OfferNone {
		t.Errorf("a zero hand-out should offer nothing, got %s", got)
	}
	if got := MasterNextOffer(MasterOfferState{HandOut: 10, NotedHeld: 20}); got != OfferNotes {
		t.Errorf("notes not yet up should offer notes, got %s", got)
	}
	if got := MasterNextOffer(MasterOfferState{HandOut: 10, MyNotedUp: 10, CoinsPerTrade: 100, CoinsHeld: 500}); got != OfferCoins {
		t.Errorf("notes up, coins not yet, should offer coins, got %s", got)
	}
	if got := MasterNextOffer(MasterOfferState{HandOut: 10, MyNotedUp: 10, MyCoinsUp: 100, CoinsPerTrade: 100}); got != OfferNone {
		t.Errorf("both sides up should offer nothing more, got %s", got)
	}
}

func TestDecideMasterTrade(t *testing.T) {
	partners := []string{"runner1"}
	if a := DecideMasterTrade(MasterOfferState{}, nil, partners); a.Kind != ActionWaitHeader {
		t.Errorf("no header yet should wait for it, got %+v", a)
	}
	stranger := "someoneelse"
	if a := DecideMasterTrade(MasterOfferState{}, &stranger, partners); a.Kind != ActionDecline {
		t.Errorf("an unconfigured partner should be declined, got %+v", a)
	}
	partner := "runner1"
	if a := DecideMasterTrade(MasterOfferState{HandOut: 10, NotedHeld: 20}, &partner, partners); a.Kind != ActionOffer {
		t.Errorf("something still owed should offer, got %+v", a)
	}
	if a := DecideMasterTrade(MasterOfferState{HandOut: 0}, &partner, partners); a.Kind != ActionAccept {
		t.Errorf("a zero hand-out with nothing owed should accept whatever the runner brought, got %+v", a)
	}
	if a := DecideMasterTrade(MasterOfferState{HandOut: 10, NotedHeld: 0}, &partner, partners); a.Kind != ActionDecline {
		t.Errorf("out of notes with something still owed should decline, got %+v", a)
	}
}

func TestDecideRunnerTradeReusedFromRunecraftPlan(t *testing.T) {
	crafter := "master1"
	if a := DecideRunnerTrade(nil, crafter, 5, 0, 0, 0, 0); a.Kind != ActionWaitHeader {
		t.Errorf("no header should wait, got %+v", a)
	}
	if a := DecideRunnerTrade(str(crafter), crafter, 5, 0, 0, 0, 0); a.Kind != ActionOffer {
		t.Errorf("unnoted held and not yet offered should offer, got %+v", a)
	}
	if a := DecideRunnerTrade(str(crafter), crafter, 0, 5, 0, 10, 0); a.Kind != ActionWait {
		t.Errorf("needing notes with none offered yet, within grace, should wait, got %+v", a)
	}
	if a := DecideRunnerTrade(str(crafter), crafter, 0, 5, 0, 10, NotesGraceMs); a.Kind != ActionDecline {
		t.Errorf("needing notes past the grace should decline to re-ask, got %+v", a)
	}
	if a := DecideRunnerTrade(str(crafter), crafter, 0, 0, 0, 0, 0); a.Kind != ActionAccept {
		t.Errorf("nothing owed either way should accept, got %+v", a)
	}
}

func TestTradeWindowIsFor(t *testing.T) {
	if !TradeWindowIsFor(nil, "anyone") {
		t.Error("a nil/blank header should always match the click just sent")
	}
	blank := ""
	if !TradeWindowIsFor(&blank, "anyone") {
		t.Error("an empty header should match too")
	}
	name := "Bob"
	if !TradeWindowIsFor(&name, "bob") {
		t.Error("names should match case-insensitively")
	}
	if TradeWindowIsFor(&name, "Alice") {
		t.Error("a different header should not match")
	}
}

func TestMasterPickTradeTarget(t *testing.T) {
	asked := "runner1"
	if got := MasterPickTradeTarget(&asked, 100, 50, true, 0, 200); got == nil || *got != asked {
		t.Errorf("a fresh, in-range ask outside the hold should be picked, got %v", got)
	}
	if got := MasterPickTradeTarget(&asked, 100, 50, true, 300, 200); got != nil {
		t.Error("inside the handshake hold should pick nothing")
	}
	if got := MasterPickTradeTarget(&asked, 100, 50, false, 0, 200); got != nil {
		t.Error("out of range should pick nothing")
	}
	if got := MasterPickTradeTarget(&asked, 40, 50, true, 0, 200); got != nil {
		t.Error("an ask older than the last accept should pick nothing")
	}
	if got := MasterPickTradeTarget(nil, 100, 50, true, 0, 200); got != nil {
		t.Error("no ask at all should pick nothing")
	}
}

func TestParseAvoidRect(t *testing.T) {
	b, ok := ParseAvoidRect("2800,2900,3100,3200")
	if !ok || b != (Box{MinX: 2800, MaxX: 2900, MinZ: 3100, MaxZ: 3200}) {
		t.Errorf("ParseAvoidRect = %+v,%v", b, ok)
	}
	// out of order should normalise
	b2, ok2 := ParseAvoidRect("2900,2800,3200,3100")
	if !ok2 || b2 != (Box{MinX: 2800, MaxX: 2900, MinZ: 3100, MaxZ: 3200}) {
		t.Errorf("out-of-order ParseAvoidRect = %+v,%v", b2, ok2)
	}
	if _, ok := ParseAvoidRect(""); ok {
		t.Error("blank input should report false")
	}
	if _, ok := ParseAvoidRect("1,2,3"); ok {
		t.Error("too few parts should report false")
	}
	if _, ok := ParseAvoidRect("a,b,c,d"); ok {
		t.Error("non-numeric parts should report false")
	}
}

func TestIsPoisonLine(t *testing.T) {
	if !IsPoisonLine("You have been poisoned!") {
		t.Error("should classify the poison message")
	}
	if IsPoisonLine("You eat the lobster.") {
		t.Error("an ordinary line should not classify as poison")
	}
}
