package combatplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The reserve has to be big enough to be a real margin and small enough that a full load clears
// it, or the run either dies on the way to the bank or walks straight back into it.
func TestFoodReserveIsAMarginBelowAFullLoad(t *testing.T) {
	for _, perTrip := range []int{4, 5, 8, 10, 12} {
		r := FoodReserve(perTrip)
		if r < 2 {
			t.Errorf("FoodReserve(%d) = %d, want at least 2 bites of margin", perTrip, r)
		}
		if r >= perTrip {
			t.Errorf("FoodReserve(%d) = %d, which a full load never clears — that is a bank spin", perTrip, r)
		}
		if ShouldRestock(perTrip, perTrip) {
			t.Errorf("a full load of %d asked to restock immediately", perTrip)
		}
	}
}

// The whole point of the change: a run breaks off while it still has food, not at zero.
func TestShouldRestockFiresBeforeTheFoodRunsOut(t *testing.T) {
	const perTrip = 12
	if !ShouldRestock(0, perTrip) {
		t.Error("an empty pack must still ask for the bank")
	}
	r := FoodReserve(perTrip)
	if !ShouldRestock(r, perTrip) {
		t.Errorf("holding exactly the reserve (%d) must ask for the bank", r)
	}
	if ShouldRestock(r+1, perTrip) {
		t.Errorf("holding %d, one above the reserve, must not ask yet", r+1)
	}
	// The measured failure this guards: the old rule only fired at zero.
	if !ShouldRestock(2, perTrip) {
		t.Error("holding 2 bites at a 12-bite rung must break off; fighting that down to zero is what killed five accounts in one run")
	}
}

// The dungeon fold is the whole reason these distances mean anything. A raw Chebyshev from the
// giants stand to its bank reads about 6,345 tiles, because this world keeps its dungeons on
// level 0 at z+6400, and a sizing rule fed that number would be sizing from a fiction.
func TestRouteTilesFoldsTheDungeonLayer(t *testing.T) {
	giants, ok := RungByKey(MeleeLadder(), "giants")
	if !ok {
		t.Fatal("the ladder has no giants rung")
	}
	raw := tile.Chebyshev(giants.Stand, giants.Bank)
	if raw < 6000 {
		t.Fatalf("the fixture has moved: a raw Chebyshev to the giants bank should be about 6,345, got %d", raw)
	}
	got := RouteTiles(giants.Stand, giants.Bank)
	if got >= 1000 || got <= 0 {
		t.Fatalf("RouteTiles across the dungeon layer = %d, want a real walking distance, not the 6,400 offset", got)
	}
	// The climb has to be charged, or a rung that banks through a trapdoor reads as cheap as one
	// that banks next door.
	if flat := RouteTiles(surfaceOf(giants.Stand), giants.Bank); got <= flat {
		t.Fatalf("RouteTiles charged nothing for the trapdoor: %d underground against %d on the surface", got, flat)
	}
}

// The measured failure this whole change exists for: the giants rung carried six bites and left
// 25 pack slots empty while every restock climbed out of a dungeon.
func TestFoodLoadFillsThePackWhenTheBankIsThroughATrapdoor(t *testing.T) {
	giants, ok := RungByKey(MeleeLadder(), "giants")
	if !ok {
		t.Fatal("the ladder has no giants rung")
	}
	const packSize, trout = 28, 7
	danger := FoodPerTrip(giants, trout)
	load := FoodLoad(giants, trout, packSize)
	if load <= danger {
		t.Fatalf("FoodLoad = %d against a danger floor of %d: a rung that banks on the surface and fights underground must carry more than the monster alone justifies", load, danger)
	}
	if want := packSize - PackKeepSlots; load != want {
		t.Fatalf("FoodLoad = %d, want %d — an expensive trip should fill the pack to the room it has", load, want)
	}
	// The band between a full load and the break-off has to be worth the walk. Four bites is
	// what it was, and four bites bought 1.2 kills per trip.
	if band := load - FoodReserve(load); band < 12 {
		t.Fatalf("the usable band is %d bites; the old four-bite band is what made this rung commute", band)
	}
}

// A rung whose bank is next door keeps the number the monster justifies. Without this the rule
// is not "the trip's cost informs the load", it is just "always carry everything".
func TestFoodLoadKeepsTheDangerFloorForACheapTrip(t *testing.T) {
	next := Rung{
		Key:     "next-door",
		Monster: MonsterWarrior,
		Stand:   tile.Tile{X: 3200, Z: 3200},
		Bank:    tile.Tile{X: 3202, Z: 3200},
	}
	const trout = 7
	if load, danger := FoodLoad(next, trout, 28), FoodPerTrip(next, trout); load != danger {
		t.Fatalf("FoodLoad = %d for a 2-tile trip, want the danger floor of %d", load, danger)
	}
}

// Whatever the distance says, the load has to leave the pack room for coins, a bone between the
// drop and the burial, and arrows. A load that fills every slot makes a full pack its own reason
// to bank, which is the round trip this change removes.
func TestFoodLoadAlwaysLeavesPackRoom(t *testing.T) {
	const packSize, trout = 28, 7
	for _, r := range MeleeLadder() {
		load := FoodLoad(r, trout, packSize)
		if load > packSize-PackKeepSlots {
			t.Errorf("rung %q: FoodLoad %d leaves fewer than %d slots free", r.Key, load, PackKeepSlots)
		}
		if load < FoodPerTrip(r, trout) {
			t.Errorf("rung %q: FoodLoad %d is under the danger floor %d", r.Key, load, FoodPerTrip(r, trout))
		}
		// The reserve rule still has to hold at the bigger number, or a trip that fills the pack
		// comes back already asking for the bank. That is a spin, and this script has grown one
		// three times.
		if ShouldRestock(load, load) {
			t.Errorf("rung %q: a full load of %d asks to restock immediately", r.Key, load)
		}
	}
}

// The two numbers must stay separable, because conflating them is a regression this change
// nearly shipped. Sizing a load from the walk as well as the danger makes the cows rung ask for
// 20 bites where it used to ask for 4, and the reserve is a quarter of whatever it is handed. If
// the scarcity rule were handed the trip-sized load, a fresh account whose bank holds 4 Trout
// would stop dead — an account that trained perfectly well before, because a cow hits for 1.
// combattrainer.bankFoodPolicy is therefore given the DANGER floor and never the load.
func TestScarcityIsJudgedAgainstTheDangerFloorNotTheTripLoad(t *testing.T) {
	cows, ok := RungByKey(MeleeLadder(), "cows")
	if !ok {
		t.Fatal("the ladder has no cows rung")
	}
	const trout = 7
	floor := FoodPerTrip(cows, trout)
	load := FoodLoad(cows, trout, 28)
	if load <= floor {
		t.Fatalf("fixture drift: the cows bank is far enough away that the load (%d) should exceed the danger floor (%d)", load, floor)
	}
	// Against the danger floor, four bites is a working supply.
	if ShouldRestock(4, floor) {
		t.Errorf("4 bites reads as scarce against a danger floor of %d — a fresh account with 4 Trout banked would stop rather than train", floor)
	}
	// Against the trip-sized load it is not, and that is precisely the trap: the same reading,
	// judged by the other number, ends the run.
	if !ShouldRestock(4, load) {
		t.Errorf("fixture drift: 4 bites should sit at or under the reserve of a %d-bite load, which is why the floor has to be passed instead", load)
	}
}

// The spin BreakOffLoad exists to end, as a pure case. Measured live first: -mode train on a
// fresh account made 29 bank trips in five minutes with 0 kills, melee XP frozen at 1,164, its
// pack holding 5 Trout and its bank holding none.
//
// The shape is that the two rules disagree about the same reading, and each is right on its own
// terms: against the trip-sized load the run must go to the bank, and against the danger floor it
// is not short enough to be allowed to stop.
func TestAnEmptyBankCannotMakeTheTripLoadAPermanentReasonToBank(t *testing.T) {
	cows, ok := RungByKey(MeleeLadder(), "cows")
	if !ok {
		t.Fatal("the ladder has no cows rung")
	}
	const trout, packSize, held = 7, 28, 5
	floor := FoodPerTrip(cows, trout)
	load := FoodLoad(cows, trout, packSize)

	if !ShouldRestock(held, load) {
		t.Fatalf("fixture drift: %d bites should sit at or under the reserve of a %d-bite load, which is the half of the spin that keeps sending it to the bank", held, load)
	}
	if ShouldRestock(held, floor) {
		t.Fatalf("fixture drift: %d bites should read as a working supply against a danger floor of %d, which is the half that never lets it stop", held, floor)
	}
	// Once the bank has refused a trip-sized load, the floor is what the break-off is measured
	// against and the run goes back to fighting.
	if ShouldRestock(held, BreakOffLoad(load, floor, false)) {
		t.Errorf("holding %d bites with an empty bank still asks for the bank — that is the 29-trip spin", held)
	}
	// And nothing changes while the bank can still pay for the walk.
	if got := BreakOffLoad(load, floor, true); got != load {
		t.Errorf("BreakOffLoad with a bank that can fill the trip = %d, want the trip load %d", got, load)
	}
}

// The FIFTH zero-progress bank loop, stated as a test, with the numbers the 2026-09-20 run
// actually produced.
//
// At the giants rung the trip-sized load is 20 bites and its reserve is 5, while the danger
// floor is 6 and its reserve is 2. Holding 3, 4 or 5 bites therefore satisfies "at the reserve,
// go to the bank" AND "a working supply, not allowed to stop" at the same moment. foodShort
// exists to collapse that window, and it used to be skipped whenever the withdrawal was refused
// for want of pack space — which is exactly the case that produces the window, because a pack
// that cannot be emptied is why the load could not be filled in the first place. The run made
// 351 bank trips and froze its melee XP for 44m18s.
func TestAPackWithNoRoomStillClosesTheBreakOffDeadlock(t *testing.T) {
	giants, ok := RungByKey(MeleeLadder(), "giants")
	if !ok {
		t.Fatal("the ladder has no giants rung")
	}
	const trout, packSize = 7, 28
	load := FoodLoad(giants, trout, packSize)
	floor := FoodPerTrip(giants, trout)

	band := 0
	for held := 1; held < load; held++ {
		mustBank := ShouldRestock(held, load)
		mayStop := ShouldRestock(held, floor)
		if !mustBank || mayStop {
			continue
		}
		band++

		// The trip happens, and the pack has no room for the rest of the load. That is a refusal
		// the old rule exempted.
		reach := JudgeTripLoad(held, load, true, false)
		if reach.Reached {
			t.Fatalf("holding %d of a %d-bite load is not a load reached", held, load)
		}
		if reach.Why == "" {
			t.Fatal("a refused trip has to say what refused it, or the log cannot be audited")
		}
		// With the latch set from that judgement, the break-off drops to the floor and the run
		// goes back to fighting instead of walking into the bank again.
		if ShouldRestock(held, BreakOffLoad(load, floor, reach.Reached)) {
			t.Fatalf("holding %d bites still asks for the bank after a trip that could not fill the load — that is the 351-trip stall", held)
		}
	}
	if band == 0 {
		t.Fatalf("fixture drift: the giants rung no longer has a deadlock band at all (load %d, floor %d), so this test is asserting nothing", load, floor)
	}
}

// Each refusal names itself, because the log line is the only thing a reader has afterwards, and
// "the bank could not supply it" and "the pack had no room for it" want different answers from
// whoever reads them.
func TestJudgeTripLoadNamesWhatRefusedIt(t *testing.T) {
	if r := JudgeTripLoad(20, 20, false, false); !r.Reached || r.Why != "" {
		t.Fatalf("a full load is reached and says nothing, got %+v", r)
	}
	if r := JudgeTripLoad(25, 20, false, false); !r.Reached {
		t.Fatal("more than was asked for is still reached")
	}
	full := JudgeTripLoad(4, 20, true, false)
	empty := JudgeTripLoad(4, 20, false, true)
	short := JudgeTripLoad(4, 20, false, false)
	for _, r := range []TripLoad{full, empty, short} {
		if r.Reached {
			t.Fatalf("4 of 20 is not reached, got %+v", r)
		}
	}
	if full.Why == empty.Why || empty.Why == short.Why || full.Why == short.Why {
		t.Fatalf("the three refusals must read differently: pack %q, bank-empty %q, bank-short %q", full.Why, empty.Why, short.Why)
	}
}

// The budget has to reproduce the one complete measurement that exists, or it is a model rather
// than a reading: 800 Trout carried a fresh account to 67,448 melee XP.
func TestFoodBudgetReproducesTheMeasuredRun(t *testing.T) {
	const measuredXP, measuredBites, trout = 67448, 800, 7
	got := FoodBudget(measuredXP, trout)
	if got < measuredBites {
		t.Fatalf("FoodBudget(%d, %d) = %d, under the %d bites that XP actually cost — a budget that under-states is the defect it exists to fix", measuredXP, trout, got, measuredBites)
	}
	if got > measuredBites*3/2 {
		t.Fatalf("FoodBudget(%d, %d) = %d, more than half again the measured %d — that is not a margin, it is a different number", measuredXP, trout, got, measuredBites)
	}
	// The question the run could not answer for itself: the same climb plus the ~12,000 XP it was
	// short of Defence 45 wants materially more than the 800 that was staged.
	if full := FoodBudget(measuredXP+12000, trout); full <= measuredBites {
		t.Fatalf("reaching 45 costs %d bites against the %d staged, so the budget cannot say the supply was short", full, measuredBites)
	}
	// Better food is fewer bites for the same XP, which is what makes this worth sizing at all.
	if lobster := FoodBudget(measuredXP, 12); lobster >= got {
		t.Fatalf("a 12-hitpoint bite should buy more XP than a 7-hitpoint one: %d against %d", lobster, got)
	}
	if FoodBudget(0, trout) != 0 {
		t.Fatal("a goal already met needs no food")
	}
}

// A degenerate load must not produce a negative or self-triggering reserve.
func TestFoodReserveHandlesTinyLoads(t *testing.T) {
	for _, perTrip := range []int{0, 1, 2, 3} {
		r := FoodReserve(perTrip)
		if r < 0 {
			t.Errorf("FoodReserve(%d) = %d, negative", perTrip, r)
		}
		if perTrip > 0 && r >= perTrip {
			t.Errorf("FoodReserve(%d) = %d, not below a full load", perTrip, r)
		}
	}
}
