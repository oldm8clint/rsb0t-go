package bowstringrunner

import (
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/tile"
)

// A bank visit reports the owner's cycle (BankVisit.Cycle, the BowStringer's "by the 3-click cycle"
// log) only when the cycle ran: a finished load in the pack and a batch of each in the bank. Any other
// pack, or a bank short of a batch, is drawn the old way (deposit the pack, Withdraw-X twice) and Cycle
// stays false.
func TestBankVisitReportsTheCycleOnlyWhenItRan(t *testing.T) {
	defer func(old func(*fleetrunner.Deps, tile.Tile) (*actions.Bank, bool)) { openBank = old }(openBank)
	run := func(pack, bank []actionsim.Stack) (BankVisit, *actionsim.Session) {
		sim := actionsim.New(actionsim.Objs{}, pack)
		sim.OpenBank(bank)
		d := cycleDeps(sim)
		openBank = func(*fleetrunner.Deps, tile.Tile) (*actions.Bank, bool) {
			return actions.NewBank(sim, d.Components), true
		}
		return Bank(d, tile.Tile{}, yewLongbow), sim
	}
	stock := []actionsim.Stack{{ID: yewLongbow.Unstrung, Count: 500}, {ID: yewLongbow.String, Count: 300}, {ID: yewLongbow.Strung, Count: 20}}

	// A finished load of 14 strung bows: the cycle, and the visit's bank counts are those before the
	// swap with the load added to the strung bows.
	v, sim := run(strungLoad(), stock)
	if !v.Opened || v.Err != "" || !v.Cycle || v.Drew != 14 || v.BankBows != 500 || v.BankStrings != 300 || v.BankStrung != 20+14 {
		t.Fatalf("a finished load: %+v", v)
	}
	if p := sim.Pack(); held(p, yewLongbow.Unstrung) != 14 || held(p, yewLongbow.String) != 14 || len(p) != 28 {
		t.Fatalf("pack after the cycle: %+v", p)
	}

	// An empty pack: the old way, 14 of each by Withdraw-X.
	v, sim = run(nil, stock)
	if !v.Opened || v.Err != "" || v.Cycle || v.Drew != 14 || v.BankBows != 500 || v.BankStrings != 300 || v.BankStrung != 20 {
		t.Fatalf("an empty pack: %+v", v)
	}
	if p := sim.Pack(); held(p, yewLongbow.Unstrung) != 14 || held(p, yewLongbow.String) != 14 {
		t.Fatalf("pack after the old draw: %+v", p)
	}

	// A finished load but only 9 strings banked: no cycle (it would fall short), the pack is deposited and
	// the 9 pairs are drawn the old way.
	v, _ = run(strungLoad(), []actionsim.Stack{{ID: yewLongbow.Unstrung, Count: 500}, {ID: yewLongbow.String, Count: 9}})
	if !v.Opened || v.Err != "" || v.Cycle || v.Drew != 9 {
		t.Fatalf("a bank short of strings: %+v", v)
	}
}
