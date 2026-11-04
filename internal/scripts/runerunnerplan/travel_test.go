package runerunnerplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/fleetplan"
)

func muleBank(items map[string]int) func(string) (int, bool) {
	return func(item string) (int, bool) {
		for k, n := range items {
			if strings.EqualFold(k, item) {
				return n, true
			}
		}
		return 0, true
	}
}

func TestAlchSpareFollowsTheMulesNatures(t *testing.T) {
	if ok, why := AlchSpare(107, true); ok || !strings.Contains(why, "107") {
		t.Errorf("the 7 October mule (107 natures) must not be drawn on: %v %q", ok, why)
	}
	if ok, _ := AlchSpare(0, false); ok {
		t.Error("an unpublished bank is not spare")
	}
	if ok, why := AlchSpare(AlchNatureFloor(), true); !ok || why != "" {
		t.Errorf("a mule at the floor can spare: %v %q", ok, why)
	}
}

func baseKit() TravelKitInput {
	return TravelKitInput{
		Alch: fleetplan.WalkAlchSettings{Enabled: true, MinMagic: 55, Kit: TravelAlchKit}.Normalised(),
		Tele: fleetplan.TeleTravelSettings{Enabled: true, AirRunes: true, Kit: TravelTeleKit, MuleLawFloor: 1000, AnyWalk: true}.Normalised(),
		Rub:  true, Members: true, Magic: 66, Pack: fleetplan.RuneStock{},
		QuestDone: func(string) bool { return false },
	}
}

func askedItems(plan []fleetplan.AskPlan) map[string]int {
	out := map[string]int{}
	for _, p := range plan {
		for _, r := range p.Rungs {
			out[r.Item] += r.Count
		}
	}
	return out
}

func TestTravelKitWhenTheMuleIsShort(t *testing.T) {
	in := baseKit()
	in.Mule = muleBank(map[string]int{"Nature rune": 107, "Law rune": 512, "Air rune": 80000, "Fire rune": 100, "Games necklace(8)": 3})
	plan, _, notes := TravelKit(in)
	got := askedItems(plan)
	if got["Nature rune"] != 0 || got["Fire rune"] != 0 {
		t.Errorf("a short mule must keep its natures and fire runes: %v", got)
	}
	if got["Law rune"] != 0 {
		t.Errorf("laws under the 1000 floor are rationed: %v", got)
	}
	if got["Games necklace(8)"] != 1 {
		t.Errorf("the rub item should be asked of a mule that holds one: %v", got)
	}
	found := false
	for _, n := range notes {
		if strings.Contains(n, "no walk alch kit") {
			found = true
		}
	}
	if !found {
		t.Errorf("the log should say why no alch kit: %v", notes)
	}
}

func TestTravelKitWhenTheMuleCanSpare(t *testing.T) {
	in := baseKit()
	in.Mule = muleBank(map[string]int{"Nature rune": 5000, "Law rune": 4000, "Air rune": 80000, "Fire rune": 9000,
		"Water rune": 90000, "Games necklace(8)": 2, "Yew shortbow": 600})
	plan, none, _ := TravelKit(in)
	got := askedItems(plan)
	if got["Nature rune"] <= 0 {
		t.Errorf("natures expected for walk alch: %v (none %v)", got, none)
	}
	if got["Law rune"] <= 0 {
		t.Errorf("laws expected for teleports: %v", got)
	}
	if got["Air rune"] <= 0 {
		t.Errorf("air expected: a runner with no Staff of air pays teleports from the pack: %v", got)
	}
	// With a Staff of air worn the air line goes.
	in.Staff = "Staff of air"
	plan2, _, _ := TravelKit(in)
	if askedItems(plan2)["Air rune"] != 0 {
		t.Errorf("a Staff of air pays the air: %v", askedItems(plan2))
	}
	// A runner already holding a rub item is offered none.
	in.RubHeld = true
	plan3, _, _ := TravelKit(in)
	for item := range askedItems(plan3) {
		if fleetplan.IsRubItem(item) {
			t.Errorf("a runner holding a necklace was offered one: %v", askedItems(plan3))
		}
	}
}

func TestTravelKitNeedsAPublishedBank(t *testing.T) {
	in := baseKit()
	if plan, _, notes := TravelKit(in); len(plan) != 0 || len(notes) == 0 {
		t.Errorf("an unpublished mule bank asks nothing: %v %v", plan, notes)
	}
}

func TestTravelAirLine(t *testing.T) {
	if _, ok := TravelAirLine("", 0, 80000, true, false); ok {
		t.Error("no laws asked: no air")
	}
	if _, ok := TravelAirLine("Staff of air", 0, 80000, true, true); ok {
		t.Error("the staff pays the air")
	}
	if _, ok := TravelAirLine("", 0, 100, true, true); ok {
		t.Error("a mule under the air floor keeps its air")
	}
	l, ok := TravelAirLine("", 12, 80000, true, true)
	if !ok || l.Count != TravelAirPerCast*TravelTeleKit-12 {
		t.Errorf("air line %+v %v", l, ok)
	}
	if _, ok := TravelAirLine("", TravelAirPerCast*TravelTeleKit, 80000, true, true); ok {
		t.Error("a pack with the air asks for none")
	}
}
