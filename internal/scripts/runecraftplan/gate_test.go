package runecraftplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The pack bound: the crafter at the gate holds the talisman, the rune stack and the fare home (3 slots), so a trade brings
// 25; the design's two-slot ideal is 26; NaturePair's master (notes, coins, talisman, runes) took 24 and a second offer.
func TestGatePackBound(t *testing.T) {
	for occupied, want := range map[int]int{2: 26, 3: 25, 4: 24, 28: 0, 30: 0} {
		if got := GateTradeMax(occupied); got != want {
			t.Errorf("GateTradeMax(%d) = %d, want %d", occupied, got, want)
		}
	}
	if GateTradeMax(GateKeepSlots) != CarrierOffer {
		t.Fatalf("a crafter at the gate takes %d, a carrier brings %d", GateTradeMax(GateKeepSlots), CarrierOffer)
	}
}

// The crafter's cycle is 24 to 28 ticks for a trade, so the gate takes 5.4k to 6.5k essence an hour: the cap on the whole
// operation whatever the carriers bring.
func TestGateCycle(t *testing.T) {
	if GateCycleFast.Ticks() != 24 || GateCycleSlow.Ticks() != 28 {
		t.Fatalf("cycles %d and %d, want 24 and 28", GateCycleFast.Ticks(), GateCycleSlow.Ticks())
	}
	for _, tc := range []struct {
		cycle    GateCycle
		per      int
		lo, hi   int
		describe string
	}{
		{GateCycleFast, 26, 6400, 6600, "26 essence in 24 ticks"},
		{GateCycleSlow, 26, 5500, 5650, "26 essence in 28 ticks"},
		{GateCycleFast, 25, 6200, 6300, "25 essence in 24 ticks"},
		{GateCycleSlow, 25, 5300, 5400, "25 essence in 28 ticks"},
	} {
		if got := GateRate(tc.cycle, tc.per); got < tc.lo || got > tc.hi {
			t.Errorf("%s: %d an hour, want %d to %d", tc.describe, got, tc.lo, tc.hi)
		}
	}
	// The master that holds the notes and the coins and trades twice (24 essence, a longer trade) is about a fifth slower.
	master := GateCycle{Trade: GateCycleSlow.Trade + 6, WalkOut: 2, Talisman: 5, Inside: 11}
	if got, mine := GateRate(master, GateTradeMax(4)), GateRate(GateCycleSlow, GateTradeMax(GateKeepSlots)); got >= mine*85/100 {
		t.Errorf("the NaturePair master shape at %d an hour is not clearly slower than the carrier-holds-kit shape at %d", got, mine)
	}
	if GateRate(GateCycle{}, 25) != 0 {
		t.Fatal("a cycle of no ticks has no rate")
	}
}

// The queue: 4, 6, 8 and 10 carriers give 2.7k, 4.0k, 5.4k and 6.0k an hour, within 10%, the last at the crafter's cap.
func TestQueueModel(t *testing.T) {
	trip := TripTicks(CarrierTripDefault, 1, 100, WalkRub)
	cap := GateRate(GateCycleFast, CarrierOffer)
	for n, want := range map[int]int{4: 2700, 6: 4000, 8: 5400, 10: 6000} {
		got := QueueRate(n, CarrierOffer, trip+8, cap)
		if got < want*90/100 || got > want*110/100 {
			t.Errorf("%d carriers bring %d an hour (trip %d ticks, gate cap %d), want about %d", n, got, trip, cap, want)
		}
	}
	if QueueRate(0, 25, 224, 6000) != 0 || QueueRate(5, 25, 0, 6000) != 0 {
		t.Fatal("no carriers or no trip time bring nothing")
	}
	if got := QueueRate(100, 25, 224, 6000); got != 6000 {
		t.Fatalf("a hundred carriers are held to the crafter's cap, got %d", got)
	}
	if n := CarriersFor(CarrierOffer, trip+8, 6000); n < 8 || n > 10 {
		t.Fatalf("%d carriers saturate a 6.0k gate, the design says 8 to 10", n)
	}
	if MaxCarriers < CarriersFor(CarrierOffer, trip+8, cap) {
		t.Fatalf("MaxCarriers %d is under what saturates the gate", MaxCarriers)
	}
}

// The energy model: Agility 1, a 244-tile trip with 30 ticks standing: walking 274, running 248, running with the rub 224.
func TestEnergyEquilibrium(t *testing.T) {
	for _, tc := range []struct {
		mode WalkMode
		want int
		tol  int
	}{{WalkPlain, 274, 2}, {WalkRun, 248, 10}, {WalkRub, 224, 10}} {
		if got := TripTicks(CarrierTripDefault, 1, 100, tc.mode); got < tc.want-tc.tol || got > tc.want+tc.tol {
			t.Errorf("%s: %d ticks, want %d +- %d", tc.mode, got, tc.want, tc.tol)
		}
	}
	walk, run, rub := TripTicks(CarrierTripDefault, 1, 100, WalkPlain), TripTicks(CarrierTripDefault, 1, 100, WalkRun), TripTicks(CarrierTripDefault, 1, 100, WalkRub)
	if !(rub < run && run < walk) {
		t.Fatalf("the rub (%d) should beat running (%d) should beat walking (%d)", rub, run, walk)
	}
	// A carrier brings about 670 essence an hour with the rub.
	if got := CarrierRate(rub, CarrierOffer); got < 600 || got > 760 {
		t.Fatalf("a carrier brings %d an hour at %d ticks a trip, want about 670", got, rub)
	}
	// A higher Agility recovers more: the main account (54) is faster than a fresh account.
	if TripTicks(CarrierTripDefault, 54, 100, WalkRun) >= run {
		t.Fatal("Agility 54 should run further than Agility 1")
	}
}

func TestCourierQuantity(t *testing.T) {
	for _, tc := range []struct{ held, keep, max, want int }{
		{6300, 4000, 3000, 2300},
		{4000, 4000, 3000, 0},
		{3999, 4000, 3000, 0},
		{0, 4000, 3000, 0},
		{20000, 4000, 3000, 3000},
		{9000, 0, 3000, 3000},
		{9000, 4000, 0, 0},
	} {
		if got := CourierQuantity(tc.held, tc.keep, tc.max); got != tc.want {
			t.Errorf("CourierQuantity(held %d, keep %d, max %d) = %d, want %d", tc.held, tc.keep, tc.max, got, tc.want)
		}
	}
}

func gateSnap(mod func(s *GateSnapshot)) GateSnapshot {
	s := GateSnapshot{Level: 44, Members: true, Where: RegionOutside, Talisman: true}
	if mod != nil {
		mod(&s)
	}
	return s
}

func TestDecideGateStageTable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mod   func(s *GateSnapshot)
		stage GateStage
		in    string
	}{
		{"idle at the gate waits for a carrier", nil, GateTrade, "carrier"},
		{"essence in the pack and not at the ruins: walk", func(s *GateSnapshot) { s.PackEssence = 25 }, GateWalkToRuins, "25 essence"},
		{"essence in the pack at the ruins: the talisman", func(s *GateSnapshot) { s.PackEssence, s.AtRuins = 25, true }, GateUseTalisman, ""},
		{"inside with essence: craft", func(s *GateSnapshot) { s.Where, s.Temple, s.PackEssence = RegionTemple, KeyNature, 25 }, GateCraft, ""},
		{"inside with none: the portal", func(s *GateSnapshot) { s.Where, s.Temple = RegionTemple, KeyNature }, GatePortalOut, "no essence"},
		{"inside another temple: the portal", func(s *GateSnapshot) { s.Where, s.Temple, s.PackEssence = RegionTemple, KeyBody, 25 }, GatePortalOut, "body"},
		{"inside with essence under level 44: the portal", func(s *GateSnapshot) {
			s.Where, s.Temple, s.PackEssence, s.Level = RegionTemple, KeyNature, 25, 43
		}, GatePortalOut, "under the 44"},
		{"no talisman and nothing held: stuck", func(s *GateSnapshot) { s.Talisman = false }, GateStuck, "no Nature talisman"},
		{"essence and no talisman: stuck", func(s *GateSnapshot) { s.Talisman, s.PackEssence = false, 25 }, GateStuck, "no Nature talisman"},
		{"a free world: stuck", func(s *GateSnapshot) { s.Members = false }, GateStuck, "not a members world"},
		{"under level 44: stuck", func(s *GateSnapshot) { s.Level = 43 }, GateStuck, "under the 44"},
		{"the mine: stuck", func(s *GateSnapshot) { s.Where = RegionMine }, GateStuck, "essence mine"},
		{"the round ends with the pack empty: done", func(s *GateSnapshot) { s.End = "time" }, GateDone, "time"},
		{"the round ends but a carrier's essence is held: craft it first", func(s *GateSnapshot) { s.End, s.PackEssence, s.AtRuins = "time", 25, true }, GateUseTalisman, ""},
	} {
		got := DecideGateStage(gateSnap(tc.mod))
		if got.Stage != tc.stage || !strings.Contains(strings.ToLower(got.Why), strings.ToLower(tc.in)) {
			t.Errorf("%s: %s (%q), want %s containing %q", tc.name, got.Stage, got.Why, tc.stage, tc.in)
		}
	}
}

func TestDecideGateTradeTable(t *testing.T) {
	base := GateTradeView{Allowed: true, Partner: "Acct 1", FreeSlots: 25, Settled: 2 * time.Second, Opened: 3 * time.Second}
	for _, tc := range []struct {
		name string
		mod  func(v *GateTradeView)
		kind GateTradeKind
		qty  int
		in   string
	}{
		{"a stranger is declined", func(v *GateTradeView) { v.Allowed, v.TheirEssence = false, 25 }, GateTradeDecline, 0, "not one of the carriers"},
		{"25 essence: a full load is accepted at once", func(v *GateTradeView) { v.TheirEssence = 25 }, GateTradeAccept, 0, "full load of 25"},
		{"25 essence not settled yet: still accepted, it cannot grow", func(v *GateTradeView) { v.TheirEssence, v.Settled = 25, 0 }, GateTradeAccept, 0, "full load of 25"},
		{"24 essence settled: accept", func(v *GateTradeView) { v.TheirEssence = 24 }, GateTradeAccept, 0, "settled offer of 24"},
		{"24 essence into 24 free slots: all the room there is, accepted at once", func(v *GateTradeView) { v.TheirEssence, v.FreeSlots, v.Settled = 24, 24, 0 }, GateTradeAccept, 0, "all the room there is"},
		{"22 essence into 22 free slots (three strays): accepted at once", func(v *GateTradeView) { v.TheirEssence, v.FreeSlots, v.Settled = 22, 22, 0 }, GateTradeAccept, 0, "full load of 22"},
		{"a few essence into a nearly full pack: not a full load, it settles", func(v *GateTradeView) { v.TheirEssence, v.FreeSlots, v.Settled = 5, 5, 0 }, GateTradeWaitFor, 0, "settling"},
		{"23 essence offered into 24 free slots: it could still grow", func(v *GateTradeView) { v.TheirEssence, v.FreeSlots, v.Settled = 23, 24, 300*time.Millisecond }, GateTradeWaitFor, 0, "settling"},
		{"20 essence settled: accept", func(v *GateTradeView) { v.TheirEssence = 20 }, GateTradeAccept, 0, ""},
		{"24 essence not settled yet: wait", func(v *GateTradeView) { v.TheirEssence, v.Settled = 24, 300*time.Millisecond }, GateTradeWaitFor, 0, "settling"},
		{"7 essence settled for a while: wait", func(v *GateTradeView) { v.TheirEssence, v.Settled = 7, 2*time.Second }, GateTradeWaitFor, 0, "settling"},
		{"7 essence settled for long: take it", func(v *GateTradeView) { v.TheirEssence, v.Settled = 7, GateSettleMax }, GateTradeAccept, 0, "short offer"},
		{"7 essence while the round ends: take it at once", func(v *GateTradeView) { v.TheirEssence, v.GateEnding = 7, true }, GateTradeAccept, 0, "short offer"},
		{"more than the free slots: decline", func(v *GateTradeView) { v.TheirEssence, v.FreeSlots = 25, 24 }, GateTradeDecline, 0, "24 slots"},
		{"something else on the screen: decline", func(v *GateTradeView) { v.TheirEssence, v.TheirOther = 25, 1 }, GateTradeDecline, 0, "not Rune essence"},
		{"my side not empty: decline", func(v *GateTradeView) { v.TheirEssence, v.MyOffer = 25, 1 }, GateTradeDecline, 0, "not empty"},
		{"nothing offered yet: wait", func(v *GateTradeView) { v.Opened = 2 * time.Second }, GateTradeWaitFor, 0, "waiting"},
		{"nothing offered for long: decline", func(v *GateTradeView) { v.Opened = GateTradeWait }, GateTradeDecline, 0, "nothing was offered"},
		{"a courier with Natures to take: give", func(v *GateTradeView) { v.Courier, v.CourierQty = true, 2300 }, GateTradeGive, 2300, "courier"},
		{"a courier, Natures up, settled: accept", func(v *GateTradeView) { v.Courier, v.CourierQty, v.MyOffer = true, 2300, 1 }, GateTradeAccept, 0, "Natures are on my side"},
		{"a courier, Natures up, not settled: wait", func(v *GateTradeView) {
			v.Courier, v.CourierQty, v.MyOffer, v.Settled = true, 2300, 1, 100*time.Millisecond
		}, GateTradeWaitFor, 0, "not accepted"},
		{"a courier and nothing to give: wait", func(v *GateTradeView) { v.Courier, v.CourierQty = true, 0 }, GateTradeWaitFor, 0, "waiting"},
		{"a courier and nothing to give for long: decline", func(v *GateTradeView) { v.Courier, v.CourierQty, v.Opened = true, 0, GateTradeWait }, GateTradeDecline, 0, "nothing was offered"},
	} {
		v := base
		tc.mod(&v)
		got := DecideGateTrade(v)
		if got.Kind != tc.kind || got.Qty != tc.qty || !strings.Contains(strings.ToLower(got.Reason), strings.ToLower(tc.in)) {
			t.Errorf("%s: %+v, want %s qty %d containing %q", tc.name, got, tc.kind, tc.qty, tc.in)
		}
	}
}

func TestDecideGateEnd(t *testing.T) {
	t0 := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		v    GateEndView
		end  bool
		in   string
	}{
		{"just started", GateEndView{Now: t0.Add(time.Minute), Started: t0, MaxRound: 8 * time.Hour}, false, ""},
		{"warming up, no carrier yet", GateEndView{Now: t0.Add(GateWarmUp - time.Minute), Started: t0, MaxRound: 8 * time.Hour}, false, ""},
		{"time is up", GateEndView{Now: t0.Add(8 * time.Hour), Started: t0, MaxRound: 8 * time.Hour, Carriers: 8, LastTrade: t0.Add(8*time.Hour - time.Minute)}, true, "craftRuneMaxHours"},
		{"no carrier for a quarter hour", GateEndView{Now: t0.Add(GateWarmUp + GateNoCarriers), Started: t0, MaxRound: 8 * time.Hour, LastTrade: t0.Add(GateWarmUp + GateNoCarriers - time.Minute)}, true, "no carrier"},
		{"carriers at work, essence flowing", GateEndView{Now: t0.Add(3 * time.Hour), Started: t0, MaxRound: 8 * time.Hour, Carriers: 8, LastTrade: t0.Add(3*time.Hour - time.Minute)}, false, ""},
		{"carriers about, no essence for half an hour", GateEndView{Now: t0.Add(3 * time.Hour), Started: t0, MaxRound: 8 * time.Hour, Carriers: 8, LastCarrier: t0.Add(3 * time.Hour), LastTrade: t0.Add(3*time.Hour - GateDry)}, true, "no essence"},
		{"the last carrier was seen a minute ago", GateEndView{Now: t0.Add(3 * time.Hour), Started: t0, MaxRound: 8 * time.Hour, LastCarrier: t0.Add(3*time.Hour - time.Minute), LastTrade: t0.Add(3*time.Hour - time.Minute)}, false, ""},
		{"no limit set", GateEndView{Now: t0.Add(100 * time.Hour), Started: t0, Carriers: 8, LastTrade: t0.Add(100*time.Hour - time.Minute), LastCarrier: t0.Add(100 * time.Hour)}, false, ""},
	} {
		end, why := DecideGateEnd(tc.v)
		if end != tc.end || !strings.Contains(why, tc.in) {
			t.Errorf("%s: end %v (%q), want %v containing %q", tc.name, end, why, tc.end, tc.in)
		}
	}
}

func TestGateGeometry(t *testing.T) {
	if !NearGate(Nature.Exit) || !NearGate(tile.Tile{X: Nature.Exit.X + GateNear, Z: Nature.Exit.Z}) {
		t.Fatal("the exit tile and its surroundings are the gate")
	}
	if NearGate(tile.Tile{X: Nature.Exit.X + GateNear + 1, Z: Nature.Exit.Z}) || NearGate(tile.Tile{X: Nature.Exit.X, Z: Nature.Exit.Z, Level: 1}) {
		t.Fatal("past the radius, or upstairs, is not the gate")
	}
	if GateExit() != Nature.Exit {
		t.Fatal("GateExit is the Nature portal's exit tile")
	}
}
