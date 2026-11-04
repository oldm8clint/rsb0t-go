package questrunner

import (
	"strings"
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// seedPack puts the given object ids (one of each) in the pack, as the server's UPDATE_INV_FULL for the backpack does.
func seedPack(st *gamestate.State, ids []int) {
	w := packet.NewWriter(256)
	w.P2(PlayerPack)
	w.P2(uint16(len(ids)))
	for _, id := range ids {
		w.P2(uint16(id + 1))
		w.P1(1)
	}
	st.Handle(serverprot.UPDATE_INV_FULL, w.Data[:w.Pos])
}

// The run staller's runes and jewellery (Deps.KeepInPack) stay in the pack through every quest deposit, or are banked and
// drawn straight back by Deps.AtBank: they are never room for the kit. The provisioner counted them as depositable, so a
// pack of eleven things (five of them the staller's) offered 28 slots to a kit it could only ever give 23, and the Fletching
// rung's 26 logs came up three short on every pass (live 15 Oct 2026).
func TestProvisionDoesNotCountTheStallersKitAsRoom(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	staller := []int{554, 556, 563, 2552, 3853}
	seedPack(e.D.Session.State(), append(append([]int(nil), staller...), 2001, 2002, 2003, 2004, 2005, 2006))
	e.D.KeepInPack = func(name string) bool {
		for _, id := range staller {
			if name == ObjName(e.D, id) {
				return true
			}
		}
		return false
	}
	if got := FreeSlots(e.D); got != 17 {
		t.Fatalf("%d free slots in the fixture's pack, want 17", got)
	}
	var drawn []questplan.ItemCount
	p := &Provisioner{}
	p.bankVisitFn = func(d *Deps, bankTile tile.Tile, withdraw []questplan.ItemCount, keep func(id int) bool) bool {
		p.Bank.Note(map[int]int{1511: 3000})
		if len(withdraw) > 0 {
			drawn = withdraw
		}
		return true
	}
	kit := []questplan.KitLine{{ID: 1511, Name: "Logs", Qty: 26, Min: 1}}
	p.Provision(e.D, "fletch-1511", kit, nil, tile.Tile{X: 2725, Z: 3491})
	if len(drawn) != 1 || drawn[0].ID != 1511 || drawn[0].Count != 23 {
		t.Fatalf("withdrew %v, want 23 logs: 17 free slots and the 6 things the deposit clears, not the staller's 5", drawn)
	}
	// An account with no staller keeps no slot back.
	e2 := fakeEngine(sink)
	seedPack(e2.D.Session.State(), []int{2001, 2002, 2003, 2004, 2005, 2006, 554, 556, 563, 2552, 3853})
	drawn = nil
	p2 := &Provisioner{}
	p2.bankVisitFn = func(d *Deps, bankTile tile.Tile, withdraw []questplan.ItemCount, keep func(id int) bool) bool {
		p2.Bank.Note(map[int]int{1511: 3000})
		if len(withdraw) > 0 {
			drawn = withdraw
		}
		return true
	}
	p2.Provision(e2.D, "fletch-1511", kit, nil, tile.Tile{X: 2725, Z: 3491})
	if len(drawn) != 1 || drawn[0].Count != 26 {
		t.Fatalf("with nothing kept in the pack withdrew %v, want the 26 asked", drawn)
	}
}

// Three bank visits in a row that were to take something out and changed nothing in the pack end the provision as blocked
// instead of running out sixteen passes (the Fletching 25 rung's "withdraw 3 Logs: the pack filled").
func TestBankVisitsThatTakeNothingOutEndTheRoundAfterThree(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	visits := 0
	p := &Provisioner{}
	p.bankVisitFn = func(d *Deps, bankTile tile.Tile, withdraw []questplan.ItemCount, keep func(id int) bool) bool {
		visits++
		p.Bank.Note(map[int]int{1511: 3000}) // the bank holds logs; the pack never gains any
		return true
	}
	kit := []questplan.KitLine{{ID: 1511, Name: "Logs", Qty: 26}}
	ok, why := p.Provision(e.D, "fletch-1511", kit, nil, tile.Tile{X: 2725, Z: 3491})
	if ok {
		t.Fatal("a pack that never gained a log provisioned")
	}
	if !p.LastBlocked || !strings.Contains(why, "took nothing out 3 times") || !strings.Contains(why, "26 Logs") {
		t.Fatalf("blocked %v, reason %q", p.LastBlocked, why)
	}
	// One visit to read the bank, three that were to withdraw.
	if visits != 4 {
		t.Fatalf("%d bank visits, want 4 (not the sixteen passes)", visits)
	}
}

// The guard compares pack readings by item and count: a withdrawal that landed anything is progress.
func TestSameCountsJudgesAPackReading(t *testing.T) {
	if !sameCounts(map[int]int{1: 1}, map[int]int{1: 1}) || !sameCounts(nil, map[int]int{}) {
		t.Error("equal readings differ")
	}
	if sameCounts(map[int]int{1: 1}, map[int]int{1: 2}) || sameCounts(map[int]int{1: 1}, map[int]int{}) || sameCounts(map[int]int{1: 1}, map[int]int{2: 1}) {
		t.Error("different readings are equal")
	}
}

// What Deps.KeepInPack claims is not room for the kit.
func TestKeptInPackIsNotRoom(t *testing.T) {
	d := &Deps{}
	if keptInPack(d, 554) {
		t.Error("an account with no KeepInPack keeps something in the pack")
	}
	d.KeepInPack = func(name string) bool { return name == "obj 554" }
	if !keptInPack(d, 554) || keptInPack(d, 1511) {
		t.Error("KeepInPack's items are not told apart from the rest")
	}
	// A bare-pack stretch banks the account's kit too, so it is room again.
	end := d.BarePack()
	if keptInPack(d, 554) {
		t.Error("the run staller's kit is kept inside a bare-pack stretch")
	}
	end()
}
