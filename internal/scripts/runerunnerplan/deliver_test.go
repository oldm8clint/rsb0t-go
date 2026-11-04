package runerunnerplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
)

// Test 15: the delivery decision.
func TestDeliverDecision(t *testing.T) {
	now := time.Unix(2_000_000, 0)
	ready := DeliverInput{
		Held: 400, LastDelivery: now.Add(-time.Hour), Now: now, MinRunes: 300, MaxAge: 3 * time.Hour,
		MuleWorld: 1, MuleWorldKnown: true, MyWorld: 1, MuleHeard: true, HopAllowed: true,
	}
	for _, c := range []struct {
		name string
		edit func(*DeliverInput)
		want DeliverAction
		why  string
	}{
		{"at the threshold", func(*DeliverInput) {}, DeliverNow, ""},
		{"under the threshold, recent", func(in *DeliverInput) { in.Held = 299 }, DeliverSkip, "under deliverMinRunes"},
		{"under the threshold, old", func(in *DeliverInput) { in.Held = 10; in.LastDelivery = now.Add(-4 * time.Hour) }, DeliverNow, ""},
		{"never delivered", func(in *DeliverInput) { in.Held = 10; in.LastDelivery = time.Time{} }, DeliverNow, ""},
		{"nothing held, old", func(in *DeliverInput) { in.Held = 0; in.LastDelivery = time.Time{} }, DeliverSkip, "nothing held"},
		{"mule world unknown", func(in *DeliverInput) { in.MuleWorldKnown = false; in.MuleWorld = 0 }, DeliverDefer, "unknown"},
		{"mule on another world, hop allowed", func(in *DeliverInput) { in.MyWorld = 2 }, DeliverHop, "world 1"},
		{"mule on another world, no hop", func(in *DeliverInput) { in.MyWorld = 2; in.HopAllowed = false; in.HopWhy = "in combat" }, DeliverDefer, "in combat"},
		{"mule not heard", func(in *DeliverInput) { in.MuleHeard = false }, DeliverDefer, "not heard"},
		{"queue too long", func(in *DeliverInput) { in.QueueTooLong = true; in.QueueWhy = "mule has 9 ahead" }, DeliverDefer, "9 ahead"},
	} {
		in := ready
		c.edit(&in)
		got := DeliverDecision(in)
		if got.Action != c.want || !strings.Contains(got.Reason, c.why) {
			t.Errorf("%s: %s (%q), want %s containing %q", c.name, got.Action, got.Reason, c.want, c.why)
		}
	}
	// The unknown world is NEVER read as "the same world": even with MyWorld 0 matching a zero
	// MuleWorld it defers.
	in := ready
	in.MuleWorldKnown, in.MuleWorld, in.MyWorld = false, 0, 0
	if DeliverDecision(in).Action != DeliverDefer {
		t.Error("an unknown mule world must defer")
	}
}

func TestAllowListHint(t *testing.T) {
	if AllowListHint(1) || !AllowListHint(2) || !AllowListHint(5) {
		t.Error("the allow-list line is due from 2 refusals in a row")
	}
}

// Test 16: only listed runes cross, minds first; never coins, a sword, food or a blade.
func TestMuleRules(t *testing.T) {
	r := MuleRules([]string{"Mind rune", "Chaos rune"})
	pack := []donorplan.ItemRow{
		{Name: "Coins", ID: 995, Count: 5000},
		{Name: "Chaos rune", ID: 562, Count: 300},
		{Name: "Bronze sword", ID: 1277, Count: 1},
		{Name: "Knife", ID: 946, Count: 1},
		{Name: "Cake", ID: 1891, Count: 1},
		{Name: "Air rune", ID: 556, Count: 50},
		{Name: "Mind rune", ID: 558, Count: 900},
	}
	load := donorplan.PlanLoad(pack, donorplan.DefaultMuleFreeSlots, r)
	if len(load) != 2 || load[0].Name != "Mind rune" || load[1].Name != "Chaos rune" {
		t.Fatalf("load %+v, want minds then chaos only", load)
	}
	if n := donorplan.HaulOf(pack, r); n != 2 {
		t.Errorf("haul %d slots, want 2", n)
	}
	for _, keep := range []string{"Coins", "Bronze sword", "Knife", "Cake", "Air rune"} {
		if !r.Stays(donorplan.ItemRow{Name: keep, ID: 1, Count: 1}) {
			t.Errorf("%s must never be offered", keep)
		}
	}
}

func TestDeliverables(t *testing.T) {
	by, total := Deliverables(map[string]int{"Mind rune": 100, "Coins": 50}, map[string]int{"mind rune": 200, "Chaos rune": 5, "Air rune": 7}, DefaultRunes())
	if total != 312 || by["Mind rune"] != 300 || by["Air rune"] != 7 || by["Chaos rune"] != 5 {
		t.Errorf("deliverables %v total %d", by, total)
	}
}
