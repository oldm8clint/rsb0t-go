package consolidateplan

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

func spellNamed(t *testing.T, name string) fleetplan.Spell {
	t.Helper()
	sp, ok := fleetplan.SpellByName(name)
	if !ok {
		t.Fatalf("no %s in the spell ladder", name)
	}
	return sp
}

// thiefKeep is the keep of a Paladin thief (Thieving 75) training Wind Bolt with a Staff of air, with
// the casts left to a far target: the old keep would be every rune it will ever cast.
func thiefKeep(t *testing.T, level int, spells ...string) Keep {
	t.Helper()
	var train []fleetplan.Spell
	for _, n := range spells {
		train = append(train, spellNamed(t, n))
	}
	return KeepFor(Defaults(), KeepInput{
		CastsLeft: 24_000, Spell: windBolt(t), Staff: fleetplan.StaffProvides("Staff of air"),
		ThievingLevel: level, TrainSpells: train,
	})
}

func TestPocketDefaults(t *testing.T) {
	d := Defaults()
	if d.PocketHold != 10_000 || d.PocketCasts != 1_000 {
		t.Fatalf("the owner's numbers are 10,000 held and 1,000 casts kept: %+v", d)
	}
	if want := []string{"Chaos rune", "Death rune", "Blood rune"}; !reflect.DeepEqual(d.PocketRunes, want) {
		t.Fatalf("pocket runes: %v want %v", d.PocketRunes, want)
	}
	if got := (Settings{PocketHold: -1, PocketCasts: -9}).Normalised(); got.PocketHold != 0 || got.PocketCasts != 0 {
		t.Fatalf("negative numbers mean off: %+v", got)
	}
}

func TestPocketRuneLevels(t *testing.T) {
	for name, want := range map[string]int{"Chaos rune": 70, "chaos RUNE": 70, " Death rune ": 80, "Blood rune": 80} {
		if got, ok := PocketRuneLevel(name); !ok || got != want {
			t.Errorf("PocketRuneLevel(%q) = %d, %v; want %d", name, got, ok, want)
		}
	}
	for _, name := range []string{"Water rune", "Air rune", "Nature rune", ""} {
		if _, ok := PocketRuneLevel(name); ok {
			t.Errorf("%q is no pickpocket's rune", name)
		}
	}
}

// Below the hold nothing of a pocketed rune crosses, however much the casts-to-the-target keep would have
// let go; at the hold everything but the 1,000-cast ration does, and the plan after that sees no surplus.
func TestAThiefHoldsAPocketedRuneUntilItHoldsTenThousand(t *testing.T) {
	s := Defaults()
	k := thiefKeep(t, 75, "Wind Bolt")
	if !k.IsPocket("Chaos rune") || k.IsPocket("Death rune") || k.IsPocket("Blood rune") {
		t.Fatalf("Thieving 75 pockets chaos only: %v", k.Pocket)
	}
	if k.Runes["Chaos rune"] != 1000 {
		t.Fatalf("the ration is 1,000 casts of Wind Bolt = 1,000 chaos, not the %d to the target: %v", 24_000, k.Runes)
	}

	under := PlanDeposit(s, Holdings{Runes: map[string]int{"Chaos rune": 9_999}}, k, 0)
	if under.Walk || under.Runes["Chaos rune"] != 0 || under.RuneUnits() != 0 {
		t.Fatalf("9,999 chaos is under the hold: nothing crosses, no walk: %+v", under)
	}

	at := PlanDeposit(s, Holdings{Runes: map[string]int{"Chaos rune": 10_000}}, k, 0)
	if !at.Walk || at.Why != "runes over the keep" || at.Runes["Chaos rune"] != 9_000 {
		t.Fatalf("10,000 chaos hands 9,000 and walks: %+v", at)
	}

	over := PlanDeposit(s, Holdings{Runes: map[string]int{"Chaos rune": 14_321}}, k, 0)
	if over.Runes["Chaos rune"] != 13_321 {
		t.Fatalf("14,321 chaos hands all but 1,000: %+v", over)
	}

	// No ping-pong: the account left with its ration has nothing over its keep.
	after := PlanDeposit(s, Holdings{Runes: map[string]int{"Chaos rune": 10_000 - at.Runes["Chaos rune"]}}, k, 0)
	if after.Walk || after.RuneUnits() != 0 {
		t.Fatalf("after the hand-over the account holds exactly its ration: %+v", after)
	}
}

func TestAHeroHoldsDeathAndBloodAndKeepsOnlyWhatItsSpellCasts(t *testing.T) {
	s := Defaults()
	// Thieving 85 pockets all three; Wind Bolt uses chaos only, so a Death or Blood ration is 0 and all of
	// it goes once 10,000 are held.
	k := thiefKeep(t, 85, "Wind Bolt")
	for _, r := range []string{"Chaos rune", "Death rune", "Blood rune"} {
		if !k.IsPocket(r) {
			t.Fatalf("Thieving 85 pockets %s: %v", r, k.Pocket)
		}
	}
	h := Holdings{Runes: map[string]int{"Death rune": 9_999, "Blood rune": 12_000, "Chaos rune": 3_000}}
	p := PlanDeposit(s, h, k, 0)
	if p.Runes["Death rune"] != 0 || p.Runes["Chaos rune"] != 0 {
		t.Fatalf("death 9,999 and chaos 3,000 are under the hold: %+v", p)
	}
	if p.Runes["Blood rune"] != 12_000 {
		t.Fatalf("blood is at the hold and Wind Bolt casts none: all 12,000 go: %+v", p)
	}

	// A Wind Blast trainee casts death: the ration is 1,000 of them.
	k = thiefKeep(t, 85, "Wind Bolt", "Wind Blast")
	p = PlanDeposit(s, Holdings{Runes: map[string]int{"Death rune": 10_500}}, k, 0)
	if p.Runes["Death rune"] != 9_500 {
		t.Fatalf("a Wind Blast trainee keeps 1,000 death: %+v", p)
	}
}

func TestOnlyTheRunesTheLevelPaysAreFloored(t *testing.T) {
	s := Defaults()
	h := Holdings{Runes: map[string]int{"Chaos rune": 3_000, "Death rune": 3_000, "Blood rune": 3_000}}
	cases := []struct {
		level   int
		floored []string
	}{
		{69, nil},
		{70, []string{"Chaos rune"}},
		{75, []string{"Chaos rune"}},
		{80, []string{"Blood rune", "Chaos rune", "Death rune"}},
		{0, nil}, // not a thief at all
	}
	for _, c := range cases {
		k := thiefKeep(t, c.level, "Wind Bolt")
		var got []string
		for _, r := range PocketReport(k, h) {
			got = append(got, r.Name)
		}
		if !reflect.DeepEqual(got, c.floored) {
			t.Errorf("Thieving %d: pocket runes %v, want %v", c.level, got, c.floored)
		}
		p := PlanDeposit(s, h, k, 0)
		for _, r := range []string{"Chaos rune", "Death rune", "Blood rune"} {
			floored := false
			for _, f := range c.floored {
				floored = floored || f == r
			}
			if floored && p.Runes[r] != 0 {
				t.Errorf("Thieving %d hands %d %s under the hold", c.level, p.Runes[r], r)
			}
		}
	}
	// Not a thief: today's keep, which for 24,000 casts of Wind Bolt is every chaos it holds.
	k := thiefKeep(t, 0, "Wind Bolt")
	if k.Runes["Chaos rune"] != 24_000 || len(k.Pocket) != 0 || k.PocketHold != 0 {
		t.Fatalf("a non-thief keeps what it always kept: %+v", k)
	}
}

func TestAPocketRuneSettingIsMatchedByNameOnce(t *testing.T) {
	s := Defaults()
	s.PocketRunes = []string{"chaos rune", "Water rune", "  ", "CHAOS RUNE"}
	k := KeepFor(s, KeepInput{CastsLeft: 5_000, Spell: windBolt(t), ThievingLevel: 90,
		TrainSpells: []fleetplan.Spell{windBolt(t)}, ExtraRunes: map[string]int{"Chaos rune": 200}})
	if len(k.Pocket) != 1 || !k.Pocket["Chaos rune"] {
		t.Fatalf("only the chaos is a pocket rune, under its canonical key: %v", k.Pocket)
	}
	n := 0
	for name := range k.Runes {
		if strings.EqualFold(name, "chaos rune") {
			n++
		}
	}
	if n != 1 || k.Runes["Chaos rune"] != 1000 {
		t.Fatalf("one chaos key at the ration: %v", k.Runes)
	}
	// Water rune is no pickpocket's rune: it keeps what the spell keeps (none for Wind Bolt), and is spare.
	p := PlanDeposit(s, Holdings{Runes: map[string]int{"Water rune": 700}}, k, 0)
	if p.Runes["Water rune"] != 700 {
		t.Fatalf("a rune that is not pocketed crosses as it always did: %+v", p)
	}
}

func TestAKitExtraBeatsASmallerRation(t *testing.T) {
	s := Defaults()
	s.PocketCasts = 10
	k := KeepFor(s, KeepInput{CastsLeft: 100, Spell: windBolt(t), ThievingLevel: 75,
		TrainSpells: []fleetplan.Spell{windBolt(t)}, ExtraRunes: map[string]int{"Chaos rune": 400}})
	if k.Runes["Chaos rune"] != 400 {
		t.Fatalf("the larger of the ration (10) and the kit (400) stays: %v", k.Runes)
	}
}

func TestPocketHoldZeroIsTheOldKeep(t *testing.T) {
	s := Defaults()
	s.PocketHold = 0
	in := KeepInput{CastsLeft: 24_000, Spell: windBolt(t), ThievingLevel: 90, TrainSpells: []fleetplan.Spell{windBolt(t)}}
	got := KeepFor(s, in)
	in.ThievingLevel = 0
	want := KeepFor(Defaults(), in)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("with the hold off a thief keeps what a non-thief keeps:\n got %+v\nwant %+v", got, want)
	}
	// And the zero Settings{} every older test builds is the same.
	if z := KeepFor(Settings{}, KeepInput{CastsLeft: 100, Spell: windBolt(t), ThievingLevel: 90}); len(z.Pocket) != 0 {
		t.Fatalf("a zero Settings pockets nothing: %+v", z)
	}
}

// The coins a market buyer keeps back for chaos are sized from the casts to the target, exactly as before:
// the thieves' ration changes what crosses, never the purse.
func TestAThiefsRationNeverMovesTheChaosEarmark(t *testing.T) {
	s := Defaults()
	base := KeepInput{CastsLeft: 100_000, Spell: windBolt(t), Staff: fleetplan.StaffProvides("Staff of air"), MarketBuyer: true, ChaosHeld: 400}
	plain := KeepFor(s, base)
	if plain.Coins != s.CoinsReserve+s.EarmarkCap {
		t.Fatalf("control: reserve + cap, got %d", plain.Coins)
	}
	base.ThievingLevel, base.TrainSpells = 75, []fleetplan.Spell{windBolt(t)}
	thief := KeepFor(s, base)
	if thief.Coins != plain.Coins {
		t.Fatalf("a thief's purse is the buyer's: %d vs %d", thief.Coins, plain.Coins)
	}
	if thief.Runes["Chaos rune"] != 1000 {
		t.Fatalf("while its chaos keep is the ration: %v", thief.Runes)
	}
	// And no casts left (at the target): no earmark, and still a ration.
	base.CastsLeft = 0
	none := KeepFor(s, base)
	if none.Coins != s.CoinsReserve {
		t.Fatalf("at the target there is nothing to earmark: %d", none.Coins)
	}
}

func TestAKitAskThatNamesTheRuneKeepsAllOfIt(t *testing.T) {
	k := thiefKeep(t, 75, "Wind Bolt")
	s, ok, why := KitAskHandOver(Defaults(), KitAskInput{
		Stage: fleetplan.StageSurvivalCombat, Purpose: fleetplan.MuleAskStage3, Kit: []string{"Chaos rune", "Cake"},
	})
	if !ok {
		t.Fatalf("a Stage 5 kit ask hands over: %s", why)
	}
	p := PlanDeposit(s, Holdings{Runes: map[string]int{"Chaos rune": 20_000, "Water rune": 300}}, k, 0)
	if p.Runes["Chaos rune"] != 0 || p.Runes["Water rune"] != 300 {
		t.Fatalf("the kit's own names stay whole; the rest crosses at the kit ask's minimum: %+v", p)
	}
}

func TestADropOffKeepsThePocketRule(t *testing.T) {
	d := DropOff(Defaults(), 3*time.Hour)
	if d.PocketHold != 10_000 || d.PocketCasts != 1_000 || len(d.PocketRunes) != 3 {
		t.Fatalf("the endgame's drop-off plans with the account's own pocket rule: %+v", d)
	}
	k := thiefKeep(t, 75, "Wind Bolt")
	under := PlanDeposit(d, Holdings{Runes: map[string]int{"Chaos rune": 9_000}}, k, 0)
	if under.Walk || under.RuneUnits() != 0 {
		t.Fatalf("a drop-off with 9,000 chaos hands none and does not walk for it: %+v", under)
	}
	over := PlanDeposit(d, Holdings{Runes: map[string]int{"Chaos rune": 10_000}}, k, 0)
	if !over.Walk || over.Runes["Chaos rune"] != 9_000 {
		t.Fatalf("at 10,000 it hands 9,000: %+v", over)
	}
}

func TestPocketReport(t *testing.T) {
	k := thiefKeep(t, 85, "Wind Bolt")
	rows := PocketReport(k, Holdings{Runes: map[string]int{"Chaos rune": 2_500, "Death rune": 11_000, "Water rune": 9_999_999}})
	want := []PocketRow{
		{Name: "Chaos rune", Held: 2_500, Hold: 10_000, Keep: 2_500, Hand: 0},
		{Name: "Death rune", Held: 11_000, Hold: 10_000, Keep: 0, Hand: 11_000},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("report:\n got %+v\nwant %+v", rows, want)
	}
	if PocketReport(thiefKeep(t, 0, "Wind Bolt"), Holdings{Runes: map[string]int{"Chaos rune": 5}}) != nil {
		t.Fatal("a non-thief has no pocket rows")
	}
}

// The loot hand-off's plan (PlanHandOff) rides PlanDeposit: its walk test sees the same pocket rule.
func TestTheLootPlanSeesThePocketRule(t *testing.T) {
	s := Defaults()
	k := thiefKeep(t, 75, "Wind Bolt")
	h := PlanHandOff(s, HandOffInput{Holdings: Holdings{Runes: map[string]int{"Chaos rune": 9_000}}, Keep: k})
	if h.Walk || h.Deposit.RuneUnits() != 0 {
		t.Fatalf("9,000 chaos alone is no trip: %+v", h)
	}
	h = PlanHandOff(s, HandOffInput{Holdings: Holdings{Runes: map[string]int{"Chaos rune": 12_000}}, Keep: k})
	if !h.Walk || h.Deposit.Runes["Chaos rune"] != 11_000 || h.Why != "runes over the keep" {
		t.Fatalf("12,000 chaos is a trip of 11,000: %+v", h)
	}
}

// A hundred hours of a Paladin thief at the measured median (552 chaos an hour, the 13 Oct audit) with the endgame's
// drop-off every three hours: the mule is traded a lot of nine thousand or more, never the dribble, and the first
// one is not before the account holds ten thousand (about eighteen hours).
func TestAThiefFeedsTheMuleInLotsOfThousands(t *testing.T) {
	s := DropOff(Defaults(), 3*time.Hour)
	k := thiefKeep(t, 75, "Wind Bolt")
	held, firstAt := 0, 0
	var lots []int
	for hour := 1; hour <= 100; hour++ {
		held += 552
		if hour%3 != 0 {
			continue
		}
		p := PlanDeposit(s, Holdings{Runes: map[string]int{"Chaos rune": held}}, k, 0)
		if !p.Walk {
			if p.RuneUnits() != 0 {
				t.Fatalf("hour %d: runes planned (%d) but no walk", hour, p.RuneUnits())
			}
			continue
		}
		if firstAt == 0 {
			firstAt = hour
		}
		lots = append(lots, p.Runes["Chaos rune"])
		held -= p.Runes["Chaos rune"]
		if held != 1000 {
			t.Fatalf("hour %d: the account is left with %d chaos, want exactly the 1,000-cast ration", hour, held)
		}
	}
	if firstAt < 18 || firstAt > 21 {
		t.Fatalf("the first lot went at hour %d, want the first drop-off after the account passed 10,000 (hour 18-21)", firstAt)
	}
	if len(lots) < 4 || len(lots) > 7 {
		t.Fatalf("%d lots in 100 hours: %v", len(lots), lots)
	}
	for _, l := range lots {
		if l < 9_000 {
			t.Fatalf("a lot of %d: the mule is traded the dribble: %v", l, lots)
		}
	}
}
