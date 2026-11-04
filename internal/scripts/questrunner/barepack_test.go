package questrunner

// The bare-pack deposit (queststrip-1015): a strip for a place that refuses the run staller's kit must end with the kit in
// the bank. Through DepositExcept it never did: Deps.KeepInPack kept it in the pack and Deps.AtBank drew the ring of
// dueling back out of the bank behind the deposit (live 14-15 Oct 2026: 238 Waterfall wf-strip parks, ten of ten Merlin
// strips).

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// The objects of the run staller's kit and a quest's food, by id: none of this needs the cache.
const (
	bareFireRune = 554
	bareAirRune  = 556
	bareLawRune  = 563
	bareDueling  = 2552
	bareGames    = 3853
	bareBread    = 2309
	bareCoins    = 995
)

// bareRig is an account at an open bank holding the run staller's whole kit, a bread and coins, whose Deps keep EVERYTHING
// in the pack by KeepInPack (the worst case: every item is the staller's) and whose AtBank draws the ring of dueling back
// out of the bank the way scarabrunner.Travel.DrawRubItem does. atBank counts its calls.
func bareRig(t *testing.T) (d *Deps, bank *actions.Bank, sim *actionsim.Session, atBank *int) {
	t.Helper()
	sim = actionsim.New(actionsim.Objs{}, []actionsim.Stack{
		{ID: bareFireRune, Count: 1}, {ID: bareAirRune, Count: 1}, {ID: bareLawRune, Count: 1},
		{ID: bareDueling, Count: 1}, {ID: bareGames, Count: 1}, {ID: bareBread, Count: 1}, {ID: bareCoins, Count: 1},
	})
	sim.OpenBank(nil)
	atBank = new(int)
	d = &Deps{
		Session: sim, Components: actionsim.Components(), Log: func(string) {},
		Delay:      func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
		KeepInPack: func(string) bool { return true },
		AtBank: func(b *actions.Bank, w actions.Waiter) {
			*atBank++
			// Draw the ring of dueling out of the bank, if the bank holds one: a rub uses no charge.
			_ = b.QuickWithdrawX(bareDueling, 1, w)
		},
	}
	return d, actions.NewBank(sim, d.Components), sim, atBank
}

func TestDepositBareBanksTheRunStallersKitAndDrawsNothingBack(t *testing.T) {
	d, bank, sim, atBank := bareRig(t)
	food := func(id int) bool { return id == bareBread || id == bareCoins }

	// Control, the live bug: the ordinary deposit keeps what KeepInPack names and runs AtBank, so the pack still holds
	// the kit (the ring, drawn back or never banked) and a tomb-safe check on it fails.
	DepositExcept(d, bank, food)
	if got := PackCounts(d); got[bareDueling] == 0 || got[bareFireRune] == 0 {
		t.Fatalf("control: the ordinary deposit left %v, want the staller's kit still in the pack (the bug under test)", got)
	}
	if *atBank == 0 {
		t.Fatal("control: AtBank did not run after the ordinary deposit")
	}

	// The bare deposit: only bread and coins stay, the whole kit is in the bank, and AtBank did not run again.
	before := *atBank
	if !DepositBare(d, bank, food) {
		t.Fatalf("DepositBare reported a pack that still holds items it does not keep: %v", PackCounts(d))
	}
	got := PackCounts(d)
	if len(got) != 2 || got[bareBread] != 1 || got[bareCoins] != 1 {
		t.Errorf("the pack holds %v after the bare deposit, want the bread and the coins alone", got)
	}
	if *atBank != before {
		t.Errorf("AtBank ran %d more time(s) behind a bare deposit, want none (it would draw the ring straight back)", *atBank-before)
	}
	banked := map[int]int{}
	for _, r := range sim.Bank() {
		banked[int(r.ID)] += r.Count
	}
	for _, id := range []int{bareFireRune, bareAirRune, bareLawRune, bareDueling, bareGames} {
		if banked[id] == 0 {
			t.Errorf("object %d is not in the bank after the bare deposit: %v", id, banked)
		}
	}
}

// A bare deposit that keeps nothing leaves the pack empty, even when KeepInPack claims every item.
func TestDepositBareWithNothingToKeepEmptiesThePack(t *testing.T) {
	d, bank, _, atBank := bareRig(t)
	if !DepositBare(d, bank, func(int) bool { return false }) {
		t.Fatalf("DepositBare reported items left: %v", PackCounts(d))
	}
	if got := PackCounts(d); len(got) != 0 {
		t.Errorf("the pack holds %v, want it empty", got)
	}
	if *atBank != 0 {
		t.Errorf("AtBank ran %d time(s), want none", *atBank)
	}
}

// A bare-pack stretch turns every DepositExcept inside it into a bare one, nests, and ends: the deposit after it is the
// ordinary one again (the kit comes back at the first ordinary deposit once the quest has left the room).
func TestBarePackStretchMakesDepositExceptBareAndEnds(t *testing.T) {
	d, bank, _, atBank := bareRig(t)
	if d.InBarePack() {
		t.Fatal("a fresh Deps is in a bare-pack stretch")
	}
	endOuter := d.BarePack()
	endInner := d.BarePack()
	endInner()
	if !d.InBarePack() {
		t.Fatal("closing the inner stretch ended the outer one")
	}
	DepositExcept(d, bank, func(id int) bool { return id == bareBread })
	if got := PackCounts(d); len(got) != 1 || got[bareBread] != 1 {
		t.Errorf("inside the stretch the pack holds %v, want the bread alone", got)
	}
	if *atBank != 0 {
		t.Errorf("AtBank ran %d time(s) inside the stretch, want none", *atBank)
	}
	endOuter()
	if d.InBarePack() {
		t.Fatal("the stretch is still open after both ends")
	}

	// After the stretch the ordinary deposit runs, and its AtBank draws the ring of dueling back.
	DepositExcept(d, bank, func(id int) bool { return id == bareBread })
	if *atBank != 1 {
		t.Errorf("AtBank ran %d time(s) after the stretch, want once", *atBank)
	}
	if got := PackCounts(d); got[bareDueling] != 1 {
		t.Errorf("the pack holds %v after the ordinary deposit, want the ring of dueling drawn back", got)
	}
}

// A step marked Bare keeps KeepInPack out of the engine's keep function, but still keeps what the step itself names, and
// a plain step still asks KeepInPack.
func TestBareStepKeepFuncIgnoresKeepInPack(t *testing.T) {
	e := fakeEngine(&logSink{})
	e.D.KeepInPack = func(name string) bool { return name == "obj 2552" }
	if keep := e.keepFunc(questplan.Step{}); !keep(bareDueling) {
		t.Fatal("a plain step's keep function does not ask KeepInPack")
	}
	keep := e.keepFunc(questplan.Step{Bare: true, KeepIDs: []int{bareBread}})
	if keep(bareDueling) {
		t.Error("a bare step kept the ring of dueling KeepInPack names")
	}
	if !keep(bareBread) {
		t.Error("a bare step dropped an item its own KeepIDs name")
	}
}

// Engine.execute opens the bare-pack stretch for a Bare step and closes it behind the step, so a custom executor (and the
// bank visits a provision or deposit step makes) run inside it.
func TestExecuteOpensTheBarePackStretchForABareStep(t *testing.T) {
	e := fakeEngine(&logSink{})
	var inside []bool
	e.quest = Quest{Customs: map[string]CustomFunc{
		"probe": func(e *Engine, _ questplan.Step) bool { inside = append(inside, e.Deps().InBarePack()); return true },
	}}
	if !e.Execute(questplan.Step{Kind: questplan.StepCustom, Custom: "probe", Bare: true}) {
		t.Fatal("the bare probe step failed")
	}
	if !e.Execute(questplan.Step{Kind: questplan.StepCustom, Custom: "probe"}) {
		t.Fatal("the plain probe step failed")
	}
	if len(inside) != 2 || !inside[0] || inside[1] {
		t.Errorf("the probe saw bare=%v, want [true false] (bare inside the bare step, not inside the plain one)", inside)
	}
	if e.Deps().InBarePack() {
		t.Error("the bare-pack stretch is still open after the step")
	}
}

// A provisioning round's bank visit inside a bare-pack stretch leaves the kit in the bank and draws only the plan's lines:
// the tomb kit's visit after the strip, which put the ring of dueling back in the pack when it was an ordinary one.
func TestProvisionBankVisitInABareStretchDrawsNoKitBack(t *testing.T) {
	visit := func(bare bool) (map[int]int, int) {
		d, _, sim, atBank := bareRig(t)
		sim.OpenBank([]actionsim.Stack{{ID: bareBread, Count: 5}})
		if bare {
			defer d.BarePack()()
		}
		p := &Provisioner{Bank: &BankView{}}
		keep := func(id int) bool { return id == bareCoins }
		if !p.bankVisit(d, tile.Tile{}, []questplan.ItemCount{{ID: bareBread, Count: 2}}, keep) {
			t.Fatal("the bank visit failed")
		}
		return PackCounts(d), *atBank
	}

	// Control: an ordinary visit keeps the kit KeepInPack names, and AtBank draws the ring.
	pack, atBank := visit(false)
	if pack[bareDueling] == 0 || pack[bareFireRune] == 0 || atBank == 0 {
		t.Fatalf("control: the ordinary visit left %v (AtBank %d time(s)), want the kit still in the pack", pack, atBank)
	}

	pack, atBank = visit(true)
	if len(pack) != 2 || pack[bareCoins] != 1 || pack[bareBread] != 2 {
		t.Errorf("the bare visit left %v, want the coins and the two breads the plan drew", pack)
	}
	if atBank != 0 {
		t.Errorf("AtBank ran %d time(s) in the bare visit, want none", atBank)
	}
}
