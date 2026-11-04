package woodcutplan

import (
	"testing"

	"rsb0t-go/internal/scripts/bankfletchplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// TestFletchOnlyWorksOnPlainLogs is the most important test in this file, because it pins a
// CORRECTION to the design rather than the design itself. The camp was specified as "all logs
// are fletched into arrow shafts"; the content says otherwise.
// content/scripts/skill_fletching/configs/cut_logs/cut_logs.dbrow gives `data=shafts,15` to
// `[fletching_normal]` and to no other row, and cut_logs.rs2's `if ($shaft_count < 1)` branch
// sends every other log to the shortbow/longbow menu instead. So the never-bank trick is a
// property of the DEAD TREE camp specifically, and silently stops applying at oak.
func TestFletchOnlyWorksOnPlainLogs(t *testing.T) {
	if !FletchesToShafts("Logs") {
		t.Error("plain Logs must fletch to arrow shafts")
	}
	if !FletchesToShafts("  logs  ") {
		t.Error("FletchesToShafts should trim and ignore case")
	}
	for _, no := range []string{"Oak logs", "Willow logs", "Maple logs", "Yew logs", "Magic logs"} {
		if FletchesToShafts(no) {
			t.Errorf("FletchesToShafts(%q) is true — that row has no shafts column in the content", no)
		}
	}
	// Every tier above the first is therefore un-fletchable, which is what forces banking
	// from oak onward. Checked against the ladder rather than against a hand-written list, so
	// adding a tier cannot quietly break the assumption.
	for i, tr := range TIERS {
		if got := FletchesToShafts(tr.Logs); got != (i == 0) {
			t.Errorf("tier %s: FletchesToShafts(%q) = %v", tr.Label, tr.Logs, got)
		}
	}
}

// TestItemNamesMatchTheFletchingRunner pins that the names this package uses are the exact ones
// bankfletchrunner.Fletch looks up in the pack. The woodcutter reuses that proven
// knife-on-logs implementation rather than duplicating it, so a drift in either name would
// present as "no-knife"/"no-logs" at runtime rather than as a compile error.
func TestItemNamesMatchTheFletchingRunner(t *testing.T) {
	if Knife != bankfletchplan.Knife {
		t.Errorf("Knife = %q, but the fletching runner looks for %q", Knife, bankfletchplan.Knife)
	}
	if ArrowShaft != bankfletchplan.ArrowShaft {
		t.Errorf("ArrowShaft = %q, but the fletching runner looks for %q", ArrowShaft, bankfletchplan.ArrowShaft)
	}
}

// TestDraynorCampSitsClearOfTheAggressiveTrees pins the safety margin that made 3091,3345 the
// chosen tile. `nasty_tree` (npc 152) is aggressive with check_nottoostrong=off, has 250
// hitpoints, and uses the same model and recolours as `deadtree2_dark` — it is visually
// indistinguishable from a choppable dead tree. Every one of its ten spawns is at x >= 3103.
func TestDraynorCampSitsClearOfTheAggressiveTrees(t *testing.T) {
	tier := DraynorTier()
	east := DraynorCamp.X + tier.Leash
	if east >= NastyTreeFromX {
		t.Errorf("the camp's leash reaches x%d, into the nasty-tree belt that starts at x%d", east, NastyTreeFromX)
	}
	// The nearest real spawn is 3103,3347; keep a visible margin rather than a single tile.
	if NastyTreeFromX-east < 5 {
		t.Errorf("only %d tiles between the leash edge (x%d) and the belt (x%d)", NastyTreeFromX-east, east, NastyTreeFromX)
	}
}

func TestDraynorTierIsAnOrdinaryTier(t *testing.T) {
	tier := DraynorTier()
	if tier.Loc != DeadTree || tier.Logs != PlainLogs {
		t.Errorf("DraynorTier = %+v", tier)
	}
	if tier.StartAt < tier.Level {
		t.Error("the start camp must honour the StartAt >= Level invariant like every other tier")
	}
	// It is driven by the same AtCamp the Seers rungs use, so the anchor must read as at-camp.
	if !AtCamp(DraynorCamp, tier) {
		t.Error("the camp's own anchor should be at camp")
	}
	far := tile.Tile{X: DraynorCamp.X + DraynorLeash + 1, Z: DraynorCamp.Z}
	if AtCamp(far, tier) {
		t.Error("a tile past the leash should not be at camp")
	}
}

func TestAtOakGateUsesTheLaddersGateNotTheContents(t *testing.T) {
	// 15 is the content's oak requirement and 25 is the ladder's gate; the camp must run on
	// to 25, which is the whole point of the raised ladder.
	if AtOakGate(15, "Yew") {
		t.Error("level 15 is legal for oak but below the ladder's gate of 25")
	}
	if AtOakGate(24, "Yew") {
		t.Error("level 24 is one below the oak gate")
	}
	if !AtOakGate(25, "Yew") {
		t.Error("level 25 is the oak gate")
	}
	// A run deliberately targeting normal trees never leaves the camp.
	if AtOakGate(99, "Normal tree") {
		t.Error("a Normal tree target should never reach the oak gate")
	}
}

func TestDecideStartPhase(t *testing.T) {
	at := DraynorCamp
	away := tile.Tile{X: 3200, Z: 3250}
	base := StartSnapshot{Tile: &at, Level: 5, Target: "Yew", HasKnife: true, HasAxe: true}

	cases := []struct {
		name string
		mut  func(*StartSnapshot)
		want StartPhase
	}{
		{"no position yet", func(s *StartSnapshot) { s.Tile = nil }, StartWalk},
		{"past the oak gate", func(s *StartSnapshot) { s.Level = 25 }, StartDone},
		// The knife comes before everything, while the pack is still empty.
		{"no knife, one in reach", func(s *StartSnapshot) { s.HasKnife = false; s.KnifeInReach = true }, StartKnife},
		// ...but a knife that is nowhere near is not worth a cross-map detour, so the run
		// carries on and simply banks when it fills.
		{"no knife, none in reach", func(s *StartSnapshot) { s.HasKnife = false }, StartChop},
		{"no axe at all", func(s *StartSnapshot) { s.HasAxe = false }, StartAxe},
		{"away from camp", func(s *StartSnapshot) { s.Tile = &away }, StartWalk},
		{"at camp, nothing to fletch", func(s *StartSnapshot) {}, StartChop},
		{"a batch has built up", func(s *StartSnapshot) { s.PackLogs = FletchBatch }, StartFletch},
		{"just under a batch", func(s *StartSnapshot) { s.PackLogs = FletchBatch - 1 }, StartChop},
		// A full pack is the only thing that can stop the chop here, and with a knife it is
		// fixable on the spot rather than by walking to a bank.
		{"full pack with a knife", func(s *StartSnapshot) { s.PackFull = true; s.PackLogs = 28 }, StartFletch},
		// Without a knife a full pack is genuinely stuck here, and must NOT report as
		// fletchable — the caller has to fall back to banking.
		//
		// These two said StartChop until 2026-09-23, and that was the bug, not the spec: with no
		// free slot there is nothing to chop INTO, so "chop" meant swinging at a tree for ever
		// while earning nothing. Live that day, eleven wcfletch accounts stood on the Draynor
		// camp tile at 3091,3345 doing exactly this, each one having come back from an
		// abandoned mule hand-off with a pack of noted logs it could not cut — one more dying
		// every ninety seconds, and a restart did not clear it.
		{"full pack without a knife", func(s *StartSnapshot) { s.PackFull = true; s.PackLogs = 28; s.HasKnife = false }, StartBank},
		// A full pack of something that is not logs cannot be fletched away either.
		{"full pack of non-logs", func(s *StartSnapshot) { s.PackFull = true; s.PackLogs = 0 }, StartBank},
	}
	for _, c := range cases {
		s := base
		if s.Tile != nil {
			t2 := *s.Tile
			s.Tile = &t2
		}
		c.mut(&s)
		if got := DecideStartPhase(s); got != c.want {
			t.Errorf("%s: DecideStartPhase = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestKnifeSpawnsAreTheOnesFromTheMaps pins all four spawns, each read out of the OBJ section of
// its own map file (obj 946 = `knife` in content/pack/obj.pack) and re-checked against content on
// 2026-09-21: m50_50.jm2 local 24,2 -> 3224,3202 beside Lumbridge castle, m42_54.jm2 local 16,19
// -> 2704,3475 near the Seers bank, m44_53.jm2 local 4,58 -> 2820,3450 at Catherby, and
// m42_53.jm2 local 12,15 -> 2700,3407 south of Seers.
func TestKnifeSpawnsAreTheOnesFromTheMaps(t *testing.T) {
	for _, c := range []struct {
		name string
		at   tile.Tile
		x, z int
	}{
		{"LumbridgeKnife", LumbridgeKnife, 3224, 3202},
		{"SeersKnife", SeersKnife, 2704, 3475},
		{"CatherbyKnife", CatherbyKnife, 2820, 3450},
		{"SouthSeersKnife", SouthSeersKnife, 2700, 3407},
	} {
		if c.at.X != c.x || c.at.Z != c.z || c.at.Level != 0 {
			t.Errorf("%s = %+v, want %d,%d,0", c.name, c.at, c.x, c.z)
		}
	}
	if len(KnifeSpawns) != 4 {
		t.Errorf("KnifeSpawns = %v", KnifeSpawns)
	}
	// The Seers spawn has to be genuinely near the Seers bank for the "already at Seers" case
	// to be worth taking at all.
	if d := tile.Chebyshev(SeersKnife, Seers); d > 30 {
		t.Errorf("the Seers knife is %d tiles from the bank", d)
	}
}

// TestCatherbyKnifeIsReachableFromTheMulePost is the whole reason the Catherby spawn was added.
//
// Every woodcutter walks to the mule post to be handed an axe and to ask for coins. If the
// nearest knife from there is the Catherby one and the detour is small, a 94-account cohort gets
// its knives as a side effect of a trip it already makes. If some later edit drops the spawn or
// moves the post, this fails rather than the camp quietly going knifeless again.
func TestCatherbyKnifeIsReachableFromTheMulePost(t *testing.T) {
	post := tile.Tile{X: 2809, Z: 3441}
	at, dist, ok := NearestKnife(post)
	if !ok {
		t.Fatalf("no knife spawn within %d tiles of the mule post", KnifeDetourRadius)
	}
	if at != CatherbyKnife {
		t.Errorf("nearest knife to the mule post = %+v, want the Catherby spawn %+v", at, CatherbyKnife)
	}
	if dist > 15 {
		t.Errorf("the Catherby knife is %d tiles from the mule post; that is no longer a free detour", dist)
	}
}

// ---- the camp wired into the phase machine (2026-09-20) ----
//
// Everything above this line tested the camp as a self-contained plan. These test the WIRING,
// which is what "woodspec" wrote the camp for and then deliberately left undone: until now
// DecideStartPhase had no production caller at all, and DecideWoodPhase sailed every fresh
// account straight past Draynor to a Seers camp measured to feed 2.0 accounts.

// TestDecideWoodPhaseTakesTheStartCampBeforeTheFare pins where the branch sits. Its position is
// the whole design, so each neighbour is asserted rather than the branch alone.
func TestDecideWoodPhaseTakesTheStartCampBeforeTheFare(t *testing.T) {
	lb := freshstart.LumbridgeCentre
	base := WoodSnapshot{
		Tile: &lb, Bright: true, Fare: 60, Coins: 0,
		Target: "Yew", Level: 1, AxesHeld: []string{"Bronze axe"}, StartCamp: true,
	}

	// Selling the starter kit still comes first: the camp does not need the coins, but the
	// fare that follows the camp does, and Lumbridge is the only place they can be had.
	sell := base
	sell.Sellable = 3
	if got := DecideWoodPhase(sell); got != PhaseStore {
		t.Errorf("with a kit to sell: got %v, want %v", got, PhaseStore)
	}

	// With the kit gone and no coins, the OLD behaviour was PhaseFund. Now the camp wins.
	if got := DecideWoodPhase(base); got != PhaseStart {
		t.Errorf("below the oak gate with an axe: got %v, want %v", got, PhaseStart)
	}
	// And it still wins with the fare already in hand — the point is not to sail yet.
	funded := base
	funded.Coins = 600
	if got := DecideWoodPhase(funded); got != PhaseStart {
		t.Errorf("below the oak gate with the fare paid for: got %v, want %v", got, PhaseStart)
	}

	// At the oak gate the camp is over and the ordinary fund/travel pair takes back over.
	graduated := base
	graduated.Level = TIERS[TargetIndex("Oak")].StartAt
	if got := DecideWoodPhase(graduated); got != PhaseFund {
		t.Errorf("at the oak gate with no coins: got %v, want %v", got, PhaseFund)
	}
	graduated.Coins = 60
	if got := DecideWoodPhase(graduated); got != PhaseTravel {
		t.Errorf("at the oak gate with the fare: got %v, want %v", got, PhaseTravel)
	}

	// No axe: the camp cannot help, and the Seers bank / the mule can. This is the gate that
	// stops a fresh account whose kit sale took the bronze axe from standing at Draynor
	// forever with nothing to chop with.
	axeless := base
	axeless.AxesHeld = nil
	if got := DecideWoodPhase(axeless); got != PhaseFund {
		t.Errorf("with no axe at all: got %v, want %v", got, PhaseFund)
	}

	// Setting off: an account that has already reached Seers is never walked back across the
	// world, whatever the setting says. This is the restart and random-event-displacement case.
	atSeers := base
	seers := Seers
	atSeers.Tile = &seers
	atSeers.BankRead = true
	if got := DecideWoodPhase(atSeers); got == PhaseStart {
		t.Errorf("an account already at Seers must never be sent to the Draynor camp; got %v", got)
	}

	// And with the camp switched off the behaviour is exactly what it was before this change.
	off := base
	off.StartCamp = false
	if got := DecideWoodPhase(off); got != PhaseFund {
		t.Errorf("camp off: got %v, want %v", got, PhaseFund)
	}
}

// TestStartCampStoreKeepKeepsEveryAxe pins the one place the two starts genuinely disagree.
//
// The ordinary run SELLS the starter bronze axe, which is correct for it: the next axe it holds
// comes out of the Seers bank or from the mule, both across two ship fares. The camp chops
// before it travels, so selling it leaves the account with nothing to chop with — and, because
// DecideWoodPhase gates PhaseStart on holding an axe, the camp would then be skipped entirely
// and silently. Asserted against Axes rather than a hand-written list so a new metal cannot
// quietly fall out of the keep list.
func TestStartCampStoreKeepKeepsEveryAxe(t *testing.T) {
	for _, a := range Axes {
		if !containsFold(StartCampStoreKeep, a) {
			t.Errorf("StartCampStoreKeep does not keep %q", a)
		}
	}
	if !containsFold(StartCampStoreKeep, freshstart.Coins) {
		t.Error("StartCampStoreKeep must still keep the coins that pay the fare")
	}
	// The ordinary list is unchanged, which is the other half of the claim.
	if containsFold(StoreKeep, "Bronze axe") {
		t.Error("StoreKeep must still sell the bronze axe — the camp's keep list is the exception, not a new rule")
	}
	if got, want := StoreKeepFor(true), StartCampStoreKeep; len(got) != len(want) {
		t.Errorf("StoreKeepFor(true) = %v", got)
	}
	if got := StoreKeepFor(false); len(got) != len(StoreKeep) {
		t.Errorf("StoreKeepFor(false) = %v, want StoreKeep %v", got, StoreKeep)
	}
}

// TestNearestKnifePicksTheReachableSpawn pins that the detour radius does the job it is there
// for: cheap on the way out of Lumbridge, and never taken once the account is at the camp.
func TestNearestKnifePicksTheReachableSpawn(t *testing.T) {
	at, dist, ok := NearestKnife(freshstart.LumbridgeCentre)
	if !ok || at != LumbridgeKnife {
		t.Fatalf("from Lumbridge: got %+v ok=%v, want the Lumbridge knife", at, ok)
	}
	if dist != tile.Chebyshev(freshstart.LumbridgeCentre, LumbridgeKnife) {
		t.Errorf("distance = %d", dist)
	}
	if at, _, ok := NearestKnife(Seers); !ok || at != SeersKnife {
		t.Errorf("from Seers: got %+v ok=%v, want the Seers knife", at, ok)
	}
	// From the camp itself, neither spawn is worth the walk — so an account that arrives
	// without a knife does not turn round, it reports the failure and the caller gives the
	// camp up. Both distances are asserted so this cannot pass by accident.
	if _, _, ok := NearestKnife(DraynorCamp); ok {
		t.Error("a knife spawn must NOT be in reach from the camp itself")
	}
	for _, k := range KnifeSpawns {
		if d := tile.Chebyshev(DraynorCamp, k); d <= KnifeDetourRadius {
			t.Errorf("%+v is %d tiles from the camp, inside the %d-tile detour radius", k, d, KnifeDetourRadius)
		}
	}
	// A different level is not a candidate at all.
	up := freshstart.LumbridgeCentre
	up.Level = 1
	if _, _, ok := NearestKnife(up); ok {
		t.Error("a spawn on another level must not be reachable")
	}
}

// TestTheCampEndsWhereTheOakGateIs ties the two halves of the hand-over together: the level at
// which DecideStartPhase says "done" is the same level at which DecideWoodPhase stops returning
// PhaseStart. If those ever disagree the account either leaves the camp early or is stuck in it.
func TestTheCampEndsWhereTheOakGateIs(t *testing.T) {
	lb := freshstart.LumbridgeCentre
	for level := 1; level <= 30; level++ {
		s := WoodSnapshot{Tile: &lb, Bright: true, Fare: 60, Coins: 600, Target: "Yew",
			Level: level, AxesHeld: []string{"Bronze axe"}, StartCamp: true}
		inCamp := DecideWoodPhase(s) == PhaseStart
		campSays := DecideStartPhase(StartSnapshot{Tile: &lb, Level: level, Target: "Yew",
			HasKnife: true, HasAxe: true}) != StartDone
		if inCamp != campSays {
			t.Errorf("level %d: DecideWoodPhase in-camp=%v but DecideStartPhase not-done=%v", level, inCamp, campSays)
		}
	}
}

// TestStartCampKeepsTheKnifeAndShaftsFromTheStore pins a loop that was MEASURED live on the lab
// engine (2026-09-20, cmd/woodkilltest -stage startcamp) rather than reasoned about, and which
// the offline tests could not have found because it needs the store and the camp in the same
// run:
//
// the knife ground spawn is at 3224,3202, INSIDE freshstart.InLumbridge. The account picks the
// knife up, the knife is an ordinary sellable item, so Sellable goes to 1 — and DecideWoodPhase
// checks the store BEFORE the camp, so the account walks to the general store and sells the
// knife for 2gp. Sellable is 0 again, the camp runs, it has no knife, and it walks back to the
// spawn. Measured: four "knife collected" / "sold the starter kit: 1 items for 2 gp" pairs in
// 2m26s, and the account never left Lumbridge.
//
// The shafts are the same shape one step later: they are the camp's entire output.
func TestStartCampKeepsTheKnifeAndShaftsFromTheStore(t *testing.T) {
	for _, want := range []string{Knife, ArrowShaft} {
		if !containsFold(StartCampStoreKeep, want) {
			t.Errorf("StartCampStoreKeep does not keep %q — this is the measured Lumbridge sell/re-fetch loop", want)
		}
		// And the ordinary keep list deliberately does NOT: a run with no camp has no knife
		// and no shafts, and adding them there would change a proven path for no reason.
		if containsFold(StoreKeep, want) {
			t.Errorf("StoreKeep keeps %q — the camp's keep list is the exception, not a new rule", want)
		}
	}
	// The knife spawn really is inside Lumbridge, which is what makes the loop possible at all.
	// Asserted here so a future move of either constant re-opens the question loudly.
	if !freshstart.InLumbridge(LumbridgeKnife) {
		t.Error("LumbridgeKnife is no longer inside freshstart.InLumbridge — re-check whether the store keep list still needs the knife")
	}
}

// TestASwappedInAccountWalksToSeersInsteadOfSailing pins the branch that makes "switch a running
// ProgressiveFisher account to woodcutting" work at all, which is how the fleet actually intends
// to start woodcutting rather than with fresh accounts.
//
// MEASURED before the fix (cmd/woodkilltest -stage swapin, a real lab account staged at the
// Catherby bank with a bronze axe and no coins): the account announced the Draynor start camp and
// then sat at 2811,3438 for 3m41s making no progress at all — it cannot plan a route to Draynor,
// because that needs the ships it has no fare for. Without the start camp it is no better:
// freshstart.FareRemaining answers the FULL fare at Catherby, so it would go to PhaseFund and
// pickpocket Men in the Lumbridge castle courtyard, most of the map away.
//
// The Catherby bank is Chebyshev 83 from Seers. SeersRadius is 80. Four tiles.
func TestASwappedInAccountWalksToSeersInsteadOfSailing(t *testing.T) {
	catherby := tile.Tile{X: 2809, Z: 3441}
	if d := tile.Chebyshev(catherby, Seers); d <= SeersRadius {
		t.Fatalf("the Catherby stand is %d from Seers, inside the %d arrival radius — this test no longer tests anything", d, SeersRadius)
	}
	if !OnSeersSide(catherby) {
		t.Fatal("the Catherby bank stand must count as the Seers landmass")
	}
	// The swapped account's real start state: an axe, no coins, nothing banked yet.
	s := WoodSnapshot{Tile: &catherby, Bright: true, Fare: 60, Coins: 0, Target: "Yew",
		Level: 1, AxesHeld: []string{"Bronze axe"}, StartCamp: true}
	got := DecideWoodPhase(s)
	if got == PhaseStart {
		t.Error("a swapped-in Catherby account with NO COINS must NOT be sent to the Draynor start camp — it cannot get there")
	}
	if got == PhaseFund || got == PhaseTravel {
		t.Errorf("a swapped-in Catherby account must not take the fresh-start road; got %v", got)
	}
	// It has never read its bank, so the first thing it should do is go and read it.
	if got != PhaseBank {
		t.Errorf("got %v, want %v — read the Seers bank first", got, PhaseBank)
	}
	// And once the bank is read and an axe is in hand, it chops like any other account.
	s.BankRead = true
	if got := DecideWoodPhase(s); got != PhaseWalk {
		t.Errorf("after the bank read: got %v, want %v", got, PhaseWalk)
	}
}

// TestOnSeersSideExcludesEverywhereTheBoatsAreActuallyNeeded pins the other half: the branch
// above must not swallow the fresh-start road, or a brand-new account would try to walk to Seers
// from Lumbridge and the Draynor camp would never run.
func TestOnSeersSideExcludesEverywhereTheBoatsAreActuallyNeeded(t *testing.T) {
	for _, out := range []struct {
		name string
		at   tile.Tile
	}{
		{"Lumbridge", freshstart.LumbridgeCentre},
		{"the Draynor camp", DraynorCamp},
		{"the Lumbridge knife spawn", LumbridgeKnife},
		{"Karamja (Musa Point)", tile.Tile{X: 2956, Z: 3143}},
		{"Karamja (Brimhaven)", tile.Tile{X: 2775, Z: 3159}},
	} {
		if OnSeersSide(out.at) {
			t.Errorf("%s (%d,%d) must NOT count as the Seers landmass — it is %d tiles away and the ships are needed",
				out.name, out.at.X, out.at.Z, tile.Chebyshev(out.at, Seers))
		}
	}
	for _, in := range []struct {
		name string
		at   tile.Tile
	}{
		{"the Catherby bank", tile.Tile{X: 2809, Z: 3441}},
		{"the Seers bank", Seers},
		{"the magic grove", TIERS[len(TIERS)-1].Anchor},
	} {
		if !OnSeersSide(in.at) {
			t.Errorf("%s (%d,%d) must count as the Seers landmass", in.name, in.at.X, in.at.Z)
		}
	}
	// Another floor is not walkable-to from here whatever the distance.
	up := Seers
	up.Level = 1
	if OnSeersSide(up) {
		t.Error("a tile on another level must not count as the Seers landmass")
	}
	// A fresh Lumbridge account still takes the fresh-start road, camp and all.
	lb := freshstart.LumbridgeCentre
	s := WoodSnapshot{Tile: &lb, Bright: true, Fare: 60, Coins: 600, Target: "Yew",
		Level: 1, AxesHeld: []string{"Bronze axe"}, StartCamp: true}
	if got := DecideWoodPhase(s); got != PhaseStart {
		t.Errorf("a fresh Lumbridge account should still go to the Draynor camp; got %v", got)
	}
}

// TestASubOakAccountWITHTheFareDoesGoToTheCamp is the other half of the rule above, added
// 2026-09-21 when the fleet went from 12 woodcutters to 94.
//
// The sibling test pins that a swapped account with NO COINS stays on the Seers ladder, because
// it cannot plan a route it cannot pay for. That is still true and still measured. What changed
// is the cost of staying: with 94 accounts, 81 of them below the oak gate averaged 122
// Woodcutting xp against 6,580 for the 13 above it, because the Seers camp holds twelve trees.
// Draynor holds 56 in one leash, and is where the knife and the fletching are.
//
// So the fare is the discriminator, not the swap. An account that can pay goes.
func TestASubOakAccountWITHTheFareDoesGoToTheCamp(t *testing.T) {
	catherby := tile.Tile{X: 2809, Z: 3441}
	s := WoodSnapshot{Tile: &catherby, Bright: true, Fare: 60, Target: "Yew",
		Level: 1, AxesHeld: []string{"Bronze axe"}, StartCamp: true,
		Coins: freshstart.FareRemaining(&catherby, 60)}
	if got := DecideWoodPhase(s); got != PhaseStart {
		t.Errorf("a sub-oak account holding the fare should go to the camp; got %v", got)
	}
	// One coin short is still not enough: it must not set off and stall.
	s.Coins--
	if got := DecideWoodPhase(s); got == PhaseStart {
		t.Error("one coin short of the fare must NOT set off for Draynor")
	}
	// And above the oak gate it stays on the Seers ladder whatever it is carrying, because oaks
	// yield several logs per cutting cycle so competing for them is cheap.
	s.Coins = freshstart.FareRemaining(&catherby, 60)
	s.Level = 99
	if got := DecideWoodPhase(s); got == PhaseStart {
		t.Error("an account past the oak gate must not go back to the camp")
	}
}

// TestNeedsCampFareOnlyWhenTheFareIsTheOnlyThingMissing pins the narrow question the bot funds
// against, so it never walks an account to the mule for a trip it was not going to take.
//
// The fleet owner's instruction was "have them check the bank for coins, if no coins for travel,
// grab from the mule". That is only correct when the fare is genuinely the last obstacle: an
// account with no axe, or already past the oak gate, or one that walks to Draynor for free from
// Lumbridge, must not be sent begging.
func TestNeedsCampFareOnlyWhenTheFareIsTheOnlyThingMissing(t *testing.T) {
	catherby := tile.Tile{X: 2809, Z: 3441}
	lumbridge := tile.Tile{X: 3222, Z: 3218}
	full := freshstart.FareRemaining(&catherby, 60)

	base := func() WoodSnapshot {
		return WoodSnapshot{Tile: &catherby, Bright: true, Fare: 60, Target: "Yew",
			Level: 1, AxesHeld: []string{"Bronze axe"}, StartCamp: true, Coins: 0}
	}

	if need, want := NeedsCampFare(base()); !want || need != full {
		t.Errorf("a broke sub-oak account on the Seers side: got (%d, %v), want (%d, true)", need, want, full)
	}

	// Already funded: nothing to ask for.
	s := base()
	s.Coins = full
	if _, want := NeedsCampFare(s); want {
		t.Error("an account already holding the fare must not ask for more")
	}

	// One short still needs it — the guard is the whole fare, not "some coins".
	s.Coins = full - 1
	if _, want := NeedsCampFare(s); !want {
		t.Error("one coin short of the fare still needs funding")
	}

	// No axe: the bank/mule path exists for that too, but this is not the reason to walk.
	s = base()
	s.AxesHeld = nil
	if _, want := NeedsCampFare(s); want {
		t.Error("an axeless account must not be funded for the camp — it cannot chop there")
	}

	// Past the oak gate: it belongs on the Seers ladder competing for oaks.
	s = base()
	s.Level = 99
	if _, want := NeedsCampFare(s); want {
		t.Error("an account past the oak gate must not be funded for the camp")
	}

	// Camp switched off entirely.
	s = base()
	s.StartCamp = false
	if _, want := NeedsCampFare(s); want {
		t.Error("with the camp off there is nothing to fund")
	}

	// From LUMBRIDGE the camp is a walk, not a sail: there is no fare to raise.
	s = base()
	s.Tile = &lumbridge
	if _, want := NeedsCampFare(s); want {
		t.Error("a Lumbridge account walks to Draynor free — it must not be funded")
	}
}
