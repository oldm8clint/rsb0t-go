package lootchestfisherplan

import (
	"testing"

	"rsb0t-go/internal/locreach"
)

// TestChestStandIsTheOnlyTileTheEngineAccepts re-derives the standing tile from
// internal/locreach rather than trusting the constant, using the chest's real placement:
// loc 172, 1x1, angle 2, shape 10, forceapproach 14 (cachecfg.Loc(172) on the live config
// archive). If the constant and the reach strategy ever disagree, this fails instead of the
// script silently sending OPLOCU from a tile the engine ignores.
func TestChestStandIsTheOnlyTileTheEngineAccepts(t *testing.T) {
	l := locreach.Loc{
		X: ChestTile.X, Z: ChestTile.Z, Level: ChestTile.Level,
		Width: 1, Length: 1, Angle: 2, Shape: 10, ForceApproach: 14,
	}
	tiles := locreach.Tiles(l)
	if len(tiles) != 2 {
		t.Fatalf("locreach offers %d tiles %v, want exactly the south tile and the footprint", len(tiles), tiles)
	}
	if tiles[0] != ChestStand {
		t.Fatalf("locreach's first (non-footprint) tile is %v, but ChestStand is %v", tiles[0], ChestStand)
	}
	if tiles[1] != ChestTile {
		t.Fatalf("locreach's fallback tile is %v, want the chest's own footprint %v", tiles[1], ChestTile)
	}
	// And the three tiles the script must NOT use.
	for _, bad := range [][2]int{{ChestTile.X - 1, ChestTile.Z}, {ChestTile.X + 1, ChestTile.Z}, {ChestTile.X, ChestTile.Z + 1}} {
		reached, ok := locreach.Reached(bad[0], bad[1], l)
		if !ok {
			t.Fatalf("locreach cannot model shape 10, which it must")
		}
		if reached {
			t.Fatalf("%v is reported reachable; forceapproach 14 at angle 2 leaves only the south side open", bad)
		}
	}
	if reached, ok := locreach.Reached(ChestStand.X, ChestStand.Z, l); !ok || !reached {
		t.Fatalf("ChestStand %v is not reachable by the engine's own test", ChestStand)
	}
}

func TestChestStandIsDueSouthOfTheChest(t *testing.T) {
	if ChestStand.X != ChestTile.X || ChestStand.Z != ChestTile.Z-1 || ChestStand.Level != ChestTile.Level {
		t.Fatalf("ChestStand %v is not the tile due south of ChestTile %v", ChestStand, ChestTile)
	}
}

func TestHouseDoorSidesAreAdjacent(t *testing.T) {
	if HouseDoorInside.X-HouseDoorOutside.X != 1 || HouseDoorInside.Z != HouseDoorOutside.Z {
		t.Fatalf("the door's inside %v is not immediately east of its outside %v — cmd/doordiag measured 2896,3450 -> 2897,3450",
			HouseDoorInside, HouseDoorOutside)
	}
}

// --- the economy ---

func TestDefaultEconomyBeatsFishingPerTrip(t *testing.T) {
	e := DefaultEconomy
	if got := e.CoinsPerFishingHour(); got != 41200 {
		t.Fatalf("fishing hour = %v, want 206 x 200 = 41200", got)
	}
	if e.SurplusPerTrip() <= 0 {
		t.Fatalf("a chest trip (%v opens x %v coins in %v minutes) does not beat %v coins an hour of fishing — at these prices the script should not run at all",
			e.OpensPerTrip, e.CoinsPerKey, e.TripMinutes, e.CoinsPerFishingHour())
	}
}

func TestMinKeysDefaultMatchesTheArithmetic(t *testing.T) {
	want := DefaultEconomy.BreakEvenKeys()
	if MinKeysDefault != want {
		t.Fatalf("MinKeysDefault = %d but the default economy breaks even at %d keys — the constant is stale",
			MinKeysDefault, want)
	}
}

func TestBreakEvenKeysRisesAsTheChestGetsWorse(t *testing.T) {
	e := DefaultEconomy
	cheap := e
	cheap.CoinsPerKey = e.CoinsPerKey / 4
	if cheap.BreakEvenKeys() < e.BreakEvenKeys() {
		t.Fatalf("a quarter-value key breaks even at %d keys, no worse than %d at full value",
			cheap.BreakEvenKeys(), e.BreakEvenKeys())
	}
}

func TestBreakEvenKeysIsZeroWhenATripCannotBeatFishing(t *testing.T) {
	e := DefaultEconomy
	e.CoinsPerKey = 1 // a chest worth nothing
	if got := e.BreakEvenKeys(); got != 0 {
		t.Fatalf("BreakEvenKeys = %d for a worthless chest, want 0 — no number of trips fixes a trip that loses money", got)
	}
}

func TestLongerTravelNeedsMoreKeys(t *testing.T) {
	near, far := DefaultEconomy, DefaultEconomy
	far.TravelMinutes = near.TravelMinutes * 10
	if far.BreakEvenKeys() <= near.BreakEvenKeys() {
		t.Fatalf("ten times the travel breaks even at %d keys, not more than %d", far.BreakEvenKeys(), near.BreakEvenKeys())
	}
}

// --- the dispatcher ---

func empty(int) int { return 0 }

func TestDecidePhaseAtTheMule(t *testing.T) {
	base := Snapshot{AtMule: true, PackFree: 28, Held: empty, MinKeys: 6}

	s := base
	s.LootInPack = 3
	if got := DecidePhase(s); got != PhaseHandoff {
		t.Fatalf("loot in the pack at the mule = %q, want %q", got, PhaseHandoff)
	}

	s = base
	s.KeysInBank = 2
	if got := DecidePhase(s); got != PhaseFish {
		t.Fatalf("2 keys against a threshold of 6 = %q, want %q", got, PhaseFish)
	}

	s = base
	s.KeysInBank = 6
	if got := DecidePhase(s); got != PhaseTravelToFalador {
		t.Fatalf("6 banked keys, empty pack = %q, want %q", got, PhaseTravelToFalador)
	}

	s = base
	s.KeysInPack = 6
	if got := DecidePhase(s); got != PhaseBankForTravel {
		t.Fatalf("keys in the PACK must be banked before the walk, got %q", got)
	}
}

func TestDecidePhaseAtFalador(t *testing.T) {
	base := Snapshot{AtFalador: true, PackFree: 28, Held: empty, MinKeys: 6}

	s := base
	s.KeysInBank = 6
	if got := DecidePhase(s); got != PhaseWithdrawKeys {
		t.Fatalf("keys banked, pack empty = %q, want %q", got, PhaseWithdrawKeys)
	}

	s = base
	s.KeysInPack, s.PackFree = 6, 22
	if got := DecidePhase(s); got != PhaseOpenChest {
		t.Fatalf("keys in hand and room to spare = %q, want %q", got, PhaseOpenChest)
	}

	s = base
	s.KeysInPack, s.PackFree, s.LootInPack = 3, 4, 21
	if got := DecidePhase(s); got != PhaseBankLoot {
		t.Fatalf("loot in the pack at Falador = %q, want %q", got, PhaseBankLoot)
	}

	// Keys left, no loot recorded, but not enough room for the worst roll: bank rather than
	// risk content dropping a reward on the floor.
	s = base
	s.KeysInPack, s.PackFree, s.LootInPack = 3, 2, 0
	if got := DecidePhase(s); got != PhaseBankLoot {
		t.Fatalf("too little room for another open = %q, want %q", got, PhaseBankLoot)
	}

	s = base
	if got := DecidePhase(s); got != PhaseTravelHome {
		t.Fatalf("no keys anywhere at Falador = %q, want %q", got, PhaseTravelHome)
	}
}

func TestDecidePhaseOnTheRoad(t *testing.T) {
	base := Snapshot{PackFree: 28, Held: empty, MinKeys: 6}

	s := base
	s.KeysInBank = 8
	if got := DecidePhase(s); got != PhaseTravelToFalador {
		t.Fatalf("plenty of banked keys, on the road = %q, want %q", got, PhaseTravelToFalador)
	}

	// CHANGED 2026-09-21, with the old expectation kept just below as the control.
	//
	// This used to be PhaseTravelHome unconditionally, and that is half of the fleet owner's
	// "its trying to mule way too early": an account on the road with one key, nothing to give
	// and nothing known to collect walked the whole ship chain back to Catherby to ask a
	// question it already had the answer to. With no errand, the answer is work.
	s = base
	s.KeysInBank = 1
	if got := DecidePhase(s); got != PhaseFish {
		t.Fatalf("not enough keys and no errand, on the road = %q, want %q", got, PhaseFish)
	}

	// ... and with a real errand it still goes home, which is what keeps the change honest.
	s = base
	s.KeysInBank = 1
	s.BankHaulRows = 7
	if got := DecidePhase(s); got != PhaseTravelHome {
		t.Fatalf("not enough keys but a banked haul to hand over = %q, want %q", got, PhaseTravelHome)
	}

	s = base
	s.LootInPack = 5
	if got := DecidePhase(s); got != PhaseBankForTravel {
		t.Fatalf("carrying loot on the road = %q, want %q — nothing walks over White Wolf Mountain loaded", got, PhaseBankForTravel)
	}
}

func TestDecidePhaseFallsBackToTheDefaultThreshold(t *testing.T) {
	s := Snapshot{AtMule: true, PackFree: 28, Held: empty, KeysInBank: MinKeysDefault - 1}
	if got := DecidePhase(s); got != PhaseFish {
		t.Fatalf("with MinKeys unset the default %d should apply, got %q", MinKeysDefault, got)
	}
	s.KeysInBank = MinKeysDefault
	if got := DecidePhase(s); got != PhaseTravelToFalador {
		t.Fatalf("exactly the default threshold should go, got %q", got)
	}
}

func TestHandoffDue(t *testing.T) {
	if !HandoffDue(Snapshot{LootInPack: 1, KeysInBank: 10}) {
		t.Fatal("loot in the pack is always worth handing over")
	}
	if !HandoffDue(Snapshot{}) {
		t.Fatal("out of keys entirely: ask the mule whether it made more")
	}
	if HandoffDue(Snapshot{KeysInBank: 4}) {
		t.Fatal("keys still banked and nothing to give: no reason to trade")
	}
}

// The comparison the whole script has to justify: is a chest hour worth more than a fishing
// hour, at the floor price? If this ever goes the other way the script should not run.
func TestAChestHourBeatsAFishingHourAtTheFloorPrice(t *testing.T) {
	e := DefaultEconomy
	chest, fishing := e.CoinsPerChestHour(), e.CoinsPerFishingHour()
	if chest <= fishing {
		t.Fatalf("a chest hour is worth %.0f and a fishing hour %.0f — the script loses money and should not be run", chest, fishing)
	}
	t.Logf("ceiling: %.0f coins/hour of chests against %.0f of fishing (%.1fx), IF keys were unlimited", chest, fishing, chest/fishing)
}

// A bank-for-travel step that left the keys in the pack would be asked for again immediately,
// forever: DecidePhase returns PhaseBankForTravel precisely BECAUSE keys are in the pack. This
// pins the property the two keep sets in internal/bot/lootchestfisher.go exist to satisfy.
func TestBankingForTravelMustActuallyBankTheKeys(t *testing.T) {
	s := Snapshot{AtMule: true, PackFree: 22, Held: empty, MinKeys: 6, KeysInPack: 6}
	if got := DecidePhase(s); got != PhaseBankForTravel {
		t.Fatalf("keys in the pack at the mule = %q, want %q", got, PhaseBankForTravel)
	}
	// What the step must leave behind: the keys banked, nothing in the pack.
	after := s
	after.KeysInPack, after.KeysInBank, after.PackFree = 0, 6, 28
	if got := DecidePhase(after); got != PhaseTravelToFalador {
		t.Fatalf("after banking the keys = %q, want %q — anything else is a loop", got, PhaseTravelToFalador)
	}
	// And the failure mode it must not have: the keys still held.
	if got := DecidePhase(s); got == PhaseTravelToFalador {
		t.Fatal("an unchanged snapshot moved on, so this test could not catch the loop")
	}
}

// At the far end the opposite holds: the keys must survive the bank trip, or every chest would
// need another 150-tile walk to fetch one.
func TestBankingLootAtFaladorKeepsTheKeys(t *testing.T) {
	s := Snapshot{AtFalador: true, PackFree: 4, Held: empty, MinKeys: 6, KeysInPack: 3, LootInPack: 21}
	if got := DecidePhase(s); got != PhaseBankLoot {
		t.Fatalf("loot in the pack at Falador = %q, want %q", got, PhaseBankLoot)
	}
	after := s
	after.LootInPack, after.PackFree = 0, 25
	if got := DecidePhase(after); got != PhaseOpenChest {
		t.Fatalf("after banking the haul with the keys kept = %q, want %q", got, PhaseOpenChest)
	}
}

// --- rule 4: a running fisher is not dragged home ---

// TestDecidePhaseKeepsFishingAwayFromTheMule is spare104's live 2026-09-20 situation, reduced to
// the dispatcher: Fishing 80 with fishingGuild on, so the delegated fisher walked to the Fishing
// Guild ~220 tiles from the Catherby mule stand, which put the account well outside
// muleStandRadius. Before rule 4 this returned PhaseTravelHome and the account was walked all
// the way back, then dispatched to fish again — 10 walks started, 7 timed out, 0 arrivals, and
// zero XP in 29.5 minutes of uptime.
func TestDecidePhaseKeepsFishingAwayFromTheMule(t *testing.T) {
	s := Snapshot{
		Fishing:    true,
		AtMule:     false, // at the Guild, ~220 tiles away
		PackFree:   28,
		Held:       empty,
		MinKeys:    6,
		KeysInBank: 0,
	}
	if got := DecidePhase(s); got != PhaseFish {
		t.Fatalf("a running fisher away from the mule = %q, want %q (it must not be walked home)", got, PhaseFish)
	}

	// Mid-walk, still carrying nothing, is the same answer — the fisher owns its own travel.
	s.AtMule = false
	if got := DecidePhase(s); got != PhaseFish {
		t.Fatalf("a running fisher in transit = %q, want %q", got, PhaseFish)
	}
}

// TestDecidePhaseFishingHoldsItsOwnCatch is the second half of the same bug: the fish a working
// fisher carries are not a haul for this script to bank or hand over. Without rule 4 the "in
// between" branch read LootInPack > 0 and returned PhaseBankForTravel, and the AtMule branch
// read it as chest loot and returned PhaseHandoff — both of which stop the fisher mid-cycle.
func TestDecidePhaseFishingHoldsItsOwnCatch(t *testing.T) {
	s := Snapshot{Fishing: true, PackFree: 4, Held: empty, MinKeys: 6, LootInPack: 24}

	if got := DecidePhase(s); got != PhaseFish {
		t.Fatalf("a fisher carrying its own catch away from the mule = %q, want %q", got, PhaseFish)
	}

	s.AtMule = true
	if got := DecidePhase(s); got != PhaseFish {
		t.Fatalf("a fisher carrying its own catch at the bank = %q, want %q", got, PhaseFish)
	}
}

// TestDecidePhaseFishingStopsWhenTheKeysArrive is the other side of rule 4: it must be the key
// count that ends fishing, and it must still end it. Anything else and the account fishes for
// ever while the mule piles up keys it never collects.
func TestDecidePhaseFishingStopsWhenTheKeysArrive(t *testing.T) {
	s := Snapshot{Fishing: true, AtMule: true, PackFree: 28, Held: empty, MinKeys: 6, KeysInBank: 6}
	if got := DecidePhase(s); got != PhaseTravelToFalador {
		t.Fatalf("the threshold reached at the mule = %q, want %q", got, PhaseTravelToFalador)
	}

	s = Snapshot{Fishing: true, AtMule: false, PackFree: 28, Held: empty, MinKeys: 6, KeysInBank: 6}
	if got := DecidePhase(s); got != PhaseTravelToFalador {
		t.Fatalf("the threshold reached away from the mule = %q, want %q", got, PhaseTravelToFalador)
	}
}

// TestDecidePhaseFishingNeverStrandsLootAtFalador pins rule 4 BELOW rule 2. If the fisher were
// somehow still marked running at the far end, banking the haul must still win — otherwise a
// death on White Wolf Mountain loses a whole chest trip.
func TestDecidePhaseFishingNeverStrandsLootAtFalador(t *testing.T) {
	s := Snapshot{Fishing: true, AtFalador: true, PackFree: 4, Held: empty, MinKeys: 6, LootInPack: 24}
	if got := DecidePhase(s); got != PhaseBankLoot {
		t.Fatalf("loot at the far end with the fisher running = %q, want %q", got, PhaseBankLoot)
	}
}

// TestDecidePhaseUnchangedWhenNotFishing guards the whole pre-existing dispatcher: with Fishing
// false, every answer must be exactly what it was before rule 4 existed.
func TestDecidePhaseUnchangedWhenNotFishing(t *testing.T) {
	cases := []struct {
		name string
		s    Snapshot
		want Phase
	}{
		{"idle at the mule below the threshold", Snapshot{AtMule: true, PackFree: 28, Held: empty, MinKeys: 6}, PhaseFish},
		{"loot at the mule", Snapshot{AtMule: true, PackFree: 28, Held: empty, MinKeys: 6, LootInPack: 3}, PhaseHandoff},
		{"on the road carrying a haul", Snapshot{PackFree: 4, Held: empty, MinKeys: 6, LootInPack: 24}, PhaseBankForTravel},
		{"on the road with enough keys", Snapshot{PackFree: 28, Held: empty, MinKeys: 6, KeysInBank: 6}, PhaseTravelToFalador},
		// CHANGED 2026-09-21: "too few keys and nothing to go home for" is now work rather
		// than a walk across the map. The errand version of the same case is the line below,
		// so both halves of the new rule are pinned here.
		{"on the road with too few keys and no errand", Snapshot{PackFree: 28, Held: empty, MinKeys: 6, KeysInBank: 1}, PhaseFish},
		{"on the road with too few keys but a banked haul", Snapshot{PackFree: 28, Held: empty, MinKeys: 6, KeysInBank: 1, BankHaulRows: 4}, PhaseTravelHome},
	}
	for _, c := range cases {
		if got := DecidePhase(c.s); got != c.want {
			t.Errorf("%s = %q, want %q", c.name, got, c.want)
		}
	}
}

// --- crystal keys held as bank notes ---

// TestDecideBanksNotedKeysRatherThanFishing is the trap this field exists for. The mule hands a
// big key order over as NOTES (they stack, so a whole stock crosses in one slot). Counting only
// unnoted keys made an account that had just collected 400 of them read TotalKeys() == 0 and go
// straight back to fishing, on top of the stock it had just been given.
func TestDecideBanksNotedKeysRatherThanFishing(t *testing.T) {
	s := Snapshot{AtMule: true, PackFree: 27, Held: empty, MinKeys: 6, NotedKeysInPack: 400}
	if got := DecidePhase(s); got != PhaseBankForTravel {
		t.Fatalf("400 noted keys at the mule = %q, want %q (they must be banked, not ignored)", got, PhaseBankForTravel)
	}
	if s.TotalKeys() != 400 {
		t.Fatalf("TotalKeys = %d, want 400 — notes are real keys once banked", s.TotalKeys())
	}
}

// TestDecideBanksNotedKeysOnTheRoad is the same rule for an account that is neither at the mule
// nor at Falador: notes in the pack mean a bank trip, never a long walk carrying them.
func TestDecideBanksNotedKeysOnTheRoad(t *testing.T) {
	s := Snapshot{PackFree: 27, Held: empty, MinKeys: 6, NotedKeysInPack: 120}
	if got := DecidePhase(s); got != PhaseBankForTravel {
		t.Fatalf("noted keys on the road = %q, want %q", got, PhaseBankForTravel)
	}
}

// TestDecideNeverOpensAChestWithNotes pins the other half: a note cannot be used on the chest,
// so the open guard must keep counting UNNOTED keys only. Getting this wrong would send the
// account to the chest holding nothing it can actually spend.
func TestDecideNeverOpensAChestWithNotes(t *testing.T) {
	s := Snapshot{AtFalador: true, PackFree: 27, Held: empty, MinKeys: 6, NotedKeysInPack: 400, KeysInPack: 0}
	if got := DecidePhase(s); got == PhaseOpenChest {
		t.Fatalf("notes alone at the chest = %q, want anything but %q", got, PhaseOpenChest)
	}
}

// Live 2026-09-21: spare104 drew six keys, opened ONE, and walked back to the Falador bank with
// five still in its pack. The chest and that bank are about 85 tiles apart, so banking on the
// first reward turns a six-key trip into six round trips. The cause was case ORDER --
// LootInPack was tested before "is there room for another?", so any reward at all won.
func TestKeepsOpeningWhileThereIsRoom(t *testing.T) {
	room := func(int) int { return 0 } // nothing held yet, so the worst case is the full one
	s := Snapshot{
		AtFalador:  true,
		KeysInPack: 5,
		LootInPack: 1,  // the first chest's reward, which used to send it to the bank
		PackFree:   20, // plenty of room
		Held:       room,
	}
	if got := DecidePhase(s); got != PhaseOpenChest {
		t.Fatalf("with 5 keys, loot held and room to spare, phase = %v, want %v — this is the six-round-trips bug", got, PhaseOpenChest)
	}
}

// The reorder must not let it open into a pack that cannot certainly hold the reward: content's
// inv_add drops the overflow on the floor, so a near-full pack loses the loot silently.
func TestBanksRatherThanOpeningIntoAFullPack(t *testing.T) {
	s := Snapshot{
		AtFalador:  true,
		KeysInPack: 5,
		LootInPack: 3,
		PackFree:   0, // no room at all
		Held:       func(int) int { return 0 },
	}
	if got := DecidePhase(s); got != PhaseBankLoot {
		t.Fatalf("with no pack room, phase = %v, want %v", got, PhaseBankLoot)
	}
}

// And with the keys spent it still banks the haul rather than stranding it at the far end.
func TestBanksTheHaulOnceTheKeysAreSpent(t *testing.T) {
	s := Snapshot{
		AtFalador:  true,
		KeysInPack: 0,
		LootInPack: 7,
		PackFree:   15,
		Held:       func(int) int { return 0 },
	}
	if got := DecidePhase(s); got != PhaseBankLoot {
		t.Fatalf("out of keys with loot held, phase = %v, want %v", got, PhaseBankLoot)
	}
}

// spare104's live trace on 2026-09-21, replayed as a decision sequence. The account banked its
// haul holding one key with 36 in the bank, and the next decision sent it to the chest with
// that one key -- a 170-tile round trip for a single open. This asserts the account tops up
// instead, and that it only does so while standing at the bank.
func TestAPartialLoadIsToppedUpAtTheBankNotCarriedToTheChest(t *testing.T) {
	roomy := Snapshot{
		AtFalador: true, AtFaladorBank: true,
		KeysInPack: 1, KeysInBank: 36,
		LootInPack: 0, PackFree: 28,
		Held: func(int) int { return 0 },
	}
	if got := DecidePhase(roomy); got != PhaseWithdrawKeys {
		t.Errorf("one key in hand and 36 in the bank, standing AT the bank: DecidePhase = %q, want %q — this is the 170-tile round trip for a single open", got, PhaseWithdrawKeys)
	}

	// Rule 2 still wins: the haul is banked first, so the join and the coin deposit still run.
	withHaul := roomy
	withHaul.LootInPack = 14
	withHaul.PackFree = 8
	if got := DecidePhase(withHaul); got != PhaseBankLoot {
		t.Errorf("a haul in the pack: DecidePhase = %q, want %q — loot must never be stranded at the far end", got, PhaseBankLoot)
	}

	// AT THE CHEST the same partial load is correct: opening is what it walked there for, and
	// topping up would mean walking back. This is what the AtFaladorBank gate buys.
	atChest := roomy
	atChest.AtFaladorBank = false
	if got := DecidePhase(atChest); got != PhaseOpenChest {
		t.Errorf("one key in hand standing at the CHEST: DecidePhase = %q, want %q — topping up from here costs the round trip it is meant to save", got, PhaseOpenChest)
	}

	// A full trip's worth needs no top-up, even at the bank.
	full := roomy
	full.KeysInPack = KeysPerTrip
	if got := DecidePhase(full); got != PhaseOpenChest {
		t.Errorf("a full trip already in hand: DecidePhase = %q, want %q", got, PhaseOpenChest)
	}

	// Nothing left to top up with: go and spend what is held rather than opening the bank.
	empty := roomy
	empty.KeysInBank = 0
	if got := DecidePhase(empty); got != PhaseOpenChest {
		t.Errorf("an empty bank: DecidePhase = %q, want %q", got, PhaseOpenChest)
	}
}
