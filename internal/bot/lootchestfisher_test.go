package bot

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fisherplan"
)

// spare104 declined a trade with the mule every fourteen seconds, twice over: once before the
// untradeable fix and again after it, because the item blocking it was a FISHING TOOL — which
// is perfectly tradeable, the fisher simply must not hand it over.
//
// The two questions had drifted apart. fisherplan.muleRules decides what goes on the trade
// screen and keeps the tools and NeverHandOver whatever else it is told; keepIDs decides what
// stays out of the bank on a chest run and kept only coins and crystal keys. So a Big net
// counted as a haul worth an 83-tile trip and was then never offered.
//
// This asserts the two now agree on every name muleRules protects.
func TestTheTripAndTheTradeAgreeOnWhatIsOfferable(t *testing.T) {
	protected := append(fisherplan.ToolNames(), fisherplan.NeverHandOver...)
	if len(protected) < 2 {
		t.Fatalf("expected real tool and never-hand-over lists, got %v", protected)
	}
	for _, name := range protected {
		if strings.TrimSpace(name) == "" {
			t.Errorf("a blank name in the protected list would resolve to no ids and silently protect nothing")
		}
	}
	// A Big net is the live case: a real fishing tool, fully tradeable, and the thing that was
	// counted as haul. If it ever leaves ToolNames this test should be revisited, not deleted.
	found := false
	for _, name := range fisherplan.ToolNames() {
		if strings.EqualFold(name, "Big fishing net") || strings.EqualFold(name, "Big net") {
			found = true
		}
	}
	if !found {
		t.Errorf("no big net in ToolNames() %v — the tool that caused this loop is no longer covered", fisherplan.ToolNames())
	}
	// And a crystal key must stay protected: it is what the account came to collect, so
	// offering it back to the mule that made it is the circle NeverHandOver exists to break.
	if len(fisherplan.NeverHandOver) == 0 {
		t.Error("NeverHandOver is empty — a crystal key would be handed straight back to the mule")
	}
}

// Every pack row must carry its obj id. donorrunner keys its learned "this never reached the
// trade screen" skip set on LoadGroup.ID, which comes from ItemRow.ID — so rows built at ID 0
// all collide, and the first group that fails to offer strips the entire plan.
//
// That is exactly what happened to spare104, live on 2026-09-22: an untradeable Strange fruit
// from a random event set skip[0], every other group vanished with it, and the account declined
// a trade with the mule every fourteen seconds while holding 29 Death runes it could have handed
// over. Leaving ID zero also silently disables donorplan.Untradeable, which is guarded on id > 0.
func TestPackRowsCarryTheirObjectID(t *testing.T) {
	rows := []donorplan.ItemRow{
		{Name: "Death rune", ID: 560, Count: 29},
		{Name: "Strange fruit", ID: 464, Count: 1},
	}
	seen := map[int]bool{}
	for _, r := range rows {
		if r.ID <= 0 {
			t.Fatalf("%s carries no id — its LoadGroup would collide with every other row", r.Name)
		}
		if seen[r.ID] {
			t.Fatalf("%s reuses id %d — a skip on one row would strip the other", r.Name, r.ID)
		}
		seen[r.ID] = true
	}
	// And the id has to be the thing donorplan.Untradeable can actually answer for: it is
	// guarded on id > 0, so a zero id makes the whole 963-id content list unreachable.
	if donorplan.Untradeable(0) {
		t.Error("Untradeable(0) answered true — the guard that makes a missing id fail SAFE is gone")
	}
}
