package mastercraftplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

func ownOf(m map[string]int) SmithStock { return func(item string) int { return m[item] } }

// The content's platebodies: five bars, the owner's levels, and no Steel row whatever the Smithing level.
func TestPlateKindsAreContentsAndNeverSteel(t *testing.T) {
	want := map[string][3]int{"Rune platebody": {1127, 99, 5}, "Adamant platebody": {1123, 88, 5}, "Mithril platebody": {1121, 68, 5}, "Iron platebody": {1115, 33, 5}}
	if len(PlateKinds) != len(want) {
		t.Fatalf("%d kinds", len(PlateKinds))
	}
	for _, p := range PlateKinds {
		w, ok := want[p.Name]
		if !ok || int(p.ID) != w[0] || p.Level != w[1] || p.BarsPer != w[2] || p.Per != 1 || p.Kind != SmithPlatebodyKind {
			t.Errorf("%+v", p)
		}
		if p.Bar == SteelBar {
			t.Error("Steel must never be a platebody bar")
		}
	}
	got := PlateProductsFor(99)
	if got[0].Name != "Rune platebody" || got[len(got)-1].Name != "Iron platebody" {
		t.Errorf("order %v", got)
	}
	if n := len(PlateProductsFor(70)); n != 2 {
		t.Errorf("Smithing 70 allows Mithril and Iron only, got %d", n)
	}
	if len(PlateProductsFor(32)) != 0 {
		t.Error("Smithing 32 allows none")
	}
}

func TestPlatebodyReadyNoLongerWaitsForTheAdamantite(t *testing.T) {
	set := DefaultSettings()
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	mule := stockMap(map[string]int{"Mithril bar": 400, "Steel bar": 90000})
	own := ownOf(map[string]int{})
	// Adamantite is superheated last now (schedule.go), so ore left in the banks does not hold the round off.
	p, ok := PlatebodyReady(set, 99, PlateLedger{}, now, mule, own)
	if !ok || p.Bars != 400 || p.Makes != 80 {
		t.Fatalf("%+v %v", p, ok)
	}
	// Steel alone never starts it.
	if _, ok := PlatebodyReady(set, 99, PlateLedger{}, now, stockMap(map[string]int{"Steel bar": 90000}), own); ok {
		t.Error("Steel bars must not start a platebody round")
	}
	// Under the threshold, and a Smithing level that leaves only Iron.
	if _, ok := PlatebodyReady(set, 99, PlateLedger{}, now, stockMap(map[string]int{"Mithril bar": 50}), own); ok {
		t.Error("50 bars is under craftPlatebodyMinBars")
	}
	if p, ok := PlatebodyReady(set, 40, PlateLedger{}, now, stockMap(map[string]int{"Mithril bar": 900, "Iron bar": 200}), own); !ok || p.Bars != 200 {
		t.Errorf("Smithing 40 reaches Iron only: %+v %v", p, ok)
	}
}

func TestPlatebodyResumeKeepsTheClockOfTheRoundUnderWay(t *testing.T) {
	set := DefaultSettings()
	start := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	led := PlateLedger{Active: true, StartedAt: start}
	own := ownOf(map[string]int{"Mithril bar": 30})
	p, ok := PlatebodyReady(set, 99, led, start.Add(20*time.Minute), stockMap(nil), own)
	if !ok || !p.Resume || !p.Started.Equal(start) {
		t.Fatalf("%+v %v: a round inside its hour resumes, Adamantite or not", p, ok)
	}
	if _, ok := PlatebodyReady(set, 99, led, start.Add(61*time.Minute), stockMap(nil), own); ok {
		t.Error("past the hour it is no resume (and Adamantite ore is left)")
	}
	if _, ok := PlatebodyReady(set, 99, led, start.Add(time.Minute), stockMap(nil), ownOf(nil)); ok {
		t.Error("no bar, no resume")
	}
}

func TestPlanPlateAskDrawsTheHighestBarFirstWithinTheBudgetAndNeverSteel(t *testing.T) {
	set := DefaultSettings()
	set.PlatebodyKit = 1000
	mule := stockMap(map[string]int{"Runite bar": 300, "Adamantite bar": 500, "Mithril bar": 5000, "Steel bar": 99999, "Iron bar": 5000, "Hammer": 1, "Ring of dueling(8)": 1})
	lines := PlanPlateAsk(set, PlateProductsFor(99), mule, ownOf(map[string]int{"Adamantite bar": 100}), false)
	got := map[string]int{}
	var order []string
	for _, l := range lines {
		got[l.Item] += l.Qty
		order = append(order, l.Item)
	}
	// Runite 300 asked; Adamant 100 banked + 500 more = 600 within what is left (700 after the Rune, minus the 100 held); the
	// Mithril fills the rest of the 1000.
	if got["Runite bar"] != 300 || got["Adamantite bar"] != 500 || got["Mithril bar"] != 100 || got["Iron bar"] != 0 {
		t.Errorf("asks %v", got)
	}
	if got["Steel bar"] != 0 || got["Hammer"] != 1 || got["Ring of dueling(8)"] != 1 {
		t.Errorf("asks %v: no Steel, a hammer and a rub item", got)
	}
	if strings.Join(order[:3], ",") != "Runite bar,Adamantite bar,Mithril bar" {
		t.Errorf("order %v", order)
	}
}

func TestPlateTeleportKit(t *testing.T) {
	mule := stockMap(map[string]int{"Law rune": 100, "Air rune": 100, "Fire rune": 100})
	none := func(string) int { return 0 }
	lines := PlateTeleportKit(60, nil, none, mule)
	got := map[string]int{}
	for _, l := range lines {
		got[l.Item] = l.Qty
	}
	if got["Law rune"] != 1 || got["Air rune"] != 3 || got["Fire rune"] != 1 {
		t.Errorf("kit %v", got)
	}
	if PlateTeleportKit(20, nil, none, mule) != nil {
		t.Error("Magic 20 cannot cast it")
	}
	if got := PlateTeleportKit(60, map[string]bool{RuneFire: true}, none, mule); len(got) != 2 {
		t.Errorf("a worn staff of fire pays the Fire rune: %v", got)
	}
	if PlateTeleportKit(60, nil, none, stockMap(map[string]int{"Law rune": 1})) != nil {
		t.Error("a mule that cannot pay the cast leaves the walk")
	}
}

// The round sits after superheat and before the cannonball and anvil rounds.
func TestDecidePlacesThePlatebodyRoundAfterSuperheat(t *testing.T) {
	set := orderSettings()
	set.SuperheatThreshold = 1
	stock := map[string]int{"Mithril bar": 600, "Steel bar": 20000, "Iron bar": 600}
	s := readySnapshot(stock)
	s.SmithingLevel = 70
	s.OwnSuperheat = func(string) int { return 0 }
	s.OwnSmith = ownOf(nil)
	d := Decide(set, s)
	if d.Mode != ModePlatebody {
		t.Fatalf("got %s (%s), want platebody ahead of the anvil round", d.Mode, d.Why)
	}
	// Adamantite ore at the mule no longer holds it off: adamant is superheated LAST (schedule.go).
	stock["Adamantite ore"], stock["Coal"], stock["Nature rune"] = 40, 400, 400
	s = readySnapshot(stock)
	s.SmithingLevel, s.OwnSuperheat, s.OwnSmith = 80, func(string) int { return 0 }, ownOf(nil)
	s.FireStaff = true
	if d := Decide(set, s); d.Mode != ModePlatebody {
		t.Fatalf("got %s (%s), want the platebody round with Adamantite ore left", d.Mode, d.Why)
	}
	// Paused after its hour: the next round gets a turn (here the last round of all, the Adamantite batch).
	s.Ledger.PauseForLimit(ModePlatebody, s.Now, set)
	set.CannonballTarget, set.Smith = 0, false
	if d := Decide(set, s); d.Mode != ModeSuperheat || d.Heat.Name != PhaseAdamant {
		t.Fatalf("got %s %+v (%s), want the Adamantite batch last", d.Mode, d.Heat, d.Why)
	}
	delete(stock, "Adamantite ore")
	s = readySnapshot(stock)
	s.SmithingLevel, s.OwnSuperheat, s.OwnSmith = 70, func(string) int { return 0 }, ownOf(nil)
	s.Ledger.PauseForLimit(ModePlatebody, s.Now, set)
	if d := Decide(set, s); d.Mode == ModePlatebody {
		t.Fatalf("a paused platebody round was chosen: %s", d.Why)
	}
}

// "No coins are lost": a cast is made only where its payout beats its runes; the round's own platebodies are never alched.
func TestPlateWalkAlchOnlyAlchsWhatPaysItsRunes(t *testing.T) {
	if AlchRuneCost(true) != 20 || AlchRuneCost(false) != 40 {
		t.Fatalf("rune cost %d with a staff, %d without", AlchRuneCost(true), AlchRuneCost(false))
	}
	if AlchPays(20, true) || !AlchPays(21, true) || AlchPays(40, false) || !AlchPays(41, false) {
		t.Error("a cast must pay strictly more than its runes")
	}
	for _, staff := range []bool{true, false} {
		s := PlateWalkAlch(staff)
		if !s.Enabled || s.MinValue != AlchRuneCost(staff)+1 || len(s.Items) == 0 {
			t.Fatalf("%+v", s)
		}
		for _, name := range s.Items {
			if isPlateName(name) {
				t.Errorf("%q is the round's own product and must not be alched", name)
			}
			if v := fleetplanKnown(name); !AlchPays(v, staff) {
				t.Errorf("%q pays %d, not over the runes", name, v)
			}
		}
	}
}

func fleetplanKnown(name string) int { return fleetplan.KnownAlchValue(name) }
