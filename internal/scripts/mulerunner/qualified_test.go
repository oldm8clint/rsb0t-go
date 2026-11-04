package mulerunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/orderplan"
)

// Content 289 names a strung bow and its unstrung form alike. the mule's bank on 6 Oct 2026: Yew
// shortbow 130,712 strung (obj 857) and none unstrung; Yew longbow 7,922 unstrung (obj 66) and 5,519
// strung (obj 855).
func bowBank() []gamestate.SlotItem {
	return []gamestate.SlotItem{
		{Item: gamestate.Item{ID: 857, Count: 130712}},
		{Item: gamestate.Item{ID: 66, Count: 7922}},
		{Item: gamestate.Item{ID: 855, Count: 5519}},
	}
}

// bankedID resolves a plain name to the first obj its bank HOLDS in id order, which is how a kit ask
// for "Yew longbow" was handed the unstrung bows when the walk-alch kit wanted the strung ones, and
// how an ask for the unstrung Yew shortbows drew 55,634 strung ones. A QUALIFIED name is one obj.
func TestBankedIDReadsAQualifiedNameAsThatObjAlone(t *testing.T) {
	cfg := kitConfig(t)
	bank := bowBank()
	for _, tc := range []struct {
		name string
		want int32
		held int
	}{
		{"Yew shortbow", 857, 130712},       // the bug: the plain name is whatever the bank holds first
		{"Yew longbow", 66, 7922},           // item 3 of the report: the alch kit wanted the strung one
		{"Yew shortbow id857", 857, 130712}, // the strung bow, exactly
		{"Yew longbow id855", 855, 5519},    // the strung longbow, though 66 comes first in id order
		{"Yew longbow id66", 66, 7922},      // the unstrung longbow
		{"Yew shortbow id68", 68, 0},        // the unstrung shortbow: resolved, and none held
		{"yew SHORTBOW ID857", 857, 130712}, // case does not matter
		{"Maple longbow id851", 851, 0},     // resolved, none held
	} {
		id, ok := bankedID(cfg, bank, tc.name)
		if !ok || id != tc.want || countByID(bank, id) != tc.held {
			t.Errorf("bankedID(%q) = %d ok=%v holding %d, want %d holding %d", tc.name, id, ok, countByID(bank, id), tc.want, tc.held)
		}
	}
	// A qualified name of the wrong display name resolves to nothing.
	if id, ok := bankedID(cfg, bank, "Yew shortbow id66"); ok {
		t.Errorf("obj 66 is not a Yew shortbow, got %d", id)
	}
	// The note of a qualified obj is that obj's own note, never the first note of the name.
	for strung, note := range map[int32]int32{857: 858, 855: 856, 66: 67, 68: 69} {
		if got, ok := noteFor(cfg, strung); !ok || got != note {
			t.Errorf("noteFor(%d) = %d, want %d", strung, got, note)
		}
	}
}

// THE LIVE BUG, from the mule's side (6 Oct 2026 16:49, kit 3: "asking the mule for 55634 Yew
// shortbow + 14445 Bow string", then "no-bows — 70079 item(s) arrived"): the mule held strung Yew
// shortbows only, so an ask for the plain name was FULFILLED with them and 55,634 of the wrong bow
// crossed. The same ask for the unstrung obj is declined "none in the bank", and the walk-alch ask for
// the strung one is filled.
func TestAnAskForTheUnstrungBowIsDeclinedWhenOnlyStrungOnesAreBanked(t *testing.T) {
	cfg := kitConfig(t)
	bank := bowBank()
	pol := orderplan.DefaultPolicy([]string{"main"})
	pol.Open.Max, pol.Open.WindowMax, pol.MaxUnitsPerItem = 1_000_000, 10_000_000, 10_000_000
	pol.MaxOrdersPerDonor = 100
	stock := func(item string) int {
		id, found := bankedID(cfg, bank, item)
		if !found {
			return 0
		}
		return countByID(bank, id)
	}
	decide := func(item string, qty int) orderplan.Decision {
		return orderplan.Decide(time.Now(), "main", orderplan.Format(item, qty), pol, orderplan.NewLedger(orderplan.DefaultWindow), stock)
	}
	if d := decide("Yew shortbow", 55634); d.Verdict != orderplan.Fulfil || d.Qty != 55634 {
		t.Fatalf("control: the plain-name ask is filled from the strung bows, as the live log shows: %+v", d)
	}
	if d := decide("Yew shortbow id68", 55634); d.Verdict != orderplan.Decline || d.Reason != "none in the bank" {
		t.Fatalf("the unstrung ask must be declined as none in the bank: %+v", d)
	}
	if d := decide("Yew shortbow id857", 200); d.Verdict != orderplan.Fulfil || d.Qty != 200 {
		t.Fatalf("the strung ask: %+v", d)
	}
	if d := decide("Yew longbow id66", 7000); d.Verdict != orderplan.Fulfil || d.Qty != 7000 {
		t.Fatalf("the unstrung longbow ask: %+v", d)
	}
}

// drawKit draws a qualified line's own obj and its own note: a kit of 200 noted UNSTRUNG Yew
// longbows and 200 noted STRUNG ones, from a bank holding both, puts notes 67 and 856 in the pack.
func TestDrawKitDrawsEachQualifiedBowLineFromItsOwnObj(t *testing.T) {
	cfg := kitConfig(t)
	sim := actionsim.New(simObjs(cfg), nil)
	sim.OpenBank([]actionsim.Stack{{ID: 66, Count: 7922}, {ID: 855, Count: 5519}, {ID: 1777, Count: 40000}})
	d := simDeps(sim, cfg)
	bank := &lockedBank{Bank: actions.NewBank(sim, d.Components), unlock: func() {}}
	lines := []orderplan.KitLineDecision{
		{Item: "Yew longbow id66", Asked: 200, Qty: 200, Noted: true},
		{Item: "Yew longbow id855", Asked: 150, Qty: 150, Noted: true},
		{Item: "Bow string", Asked: 350, Qty: 350, Noted: true},
	}
	got, offers, stopped := drawKit(d, bank, "main", lines)
	if stopped {
		t.Fatal("stopped")
	}
	pack := sim.Pack()
	if held(pack, 67) != 200 || held(pack, 856) != 150 || held(pack, 1778) != 350 || held(pack, 66) != 0 || held(pack, 855) != 0 {
		t.Fatalf("pack %+v, want 200 notes of the unstrung bow (67), 150 of the strung one (856) and 350 string notes", pack)
	}
	if got[0].Qty != 200 || got[1].Qty != 150 || offers[0].id != 67 || offers[1].id != 856 {
		t.Fatalf("lines %+v offers %+v", got, offers)
	}
}
