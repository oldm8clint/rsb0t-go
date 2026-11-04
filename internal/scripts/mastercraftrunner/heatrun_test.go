package mastercraftrunner

import (
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/tile"
)

// heatRig is an account at the booth (the tick engine) with an empty pack and bank in its bank,
// Magic and Smithing at 60, the bank's note mode off, and OpenBank played by the booth op.
func heatRig(t *testing.T, lag int, bank []struct{ id, n int32 }) (*tickEngine, *fleetrunner.Deps, *QuickBank) {
	t.Helper()
	e, d, q := newTickEngine(t, nil, bank)
	e.lag = lag
	e.magicXP, e.smithXP = levels.XPAtLevel(60)*10, levels.XPAtLevel(60)*10
	e.st.Handle(serverprot.VARP_SMALL, []byte{0, byte(actions.BankCertVarp), 0})
	e.push()
	old := OpenBank
	t.Cleanup(func() { OpenBank = old })
	OpenBank = func(d *fleetrunner.Deps, _ tile.Tile) (*actions.Bank, bool) {
		b := actions.NewBank(d.Session, d.Components)
		if !b.IsOpen() {
			if err := actions.OpLoc(d.Session, q.booth.Op, q.booth.X, q.booth.Z, q.booth.ID); err != nil {
				return nil, false
			}
			for i := 0; i < 10 && !b.IsOpen(); i++ {
				d.Delay(0)
			}
		}
		return b, b.IsOpen()
	}
	return e, d, q
}

// heatPick is the round's pick: the highest bar the stock makes (PlanSuperheat), Smithing 60.
func heatPick(stock mastercraftplan.SuperheatStock) (mastercraftplan.Bar, bool) {
	p := mastercraftplan.PlanSuperheat(mastercraftplan.DefaultSettings(), 60, stock, true)
	return p.Bar, p.Casts > 0
}

func (e *tickEngine) heatTotal(id int32) int32 {
	n := e.packTotal(id)
	for _, b := range e.bank {
		if b.id == id {
			n += b.n
		}
	}
	return n
}

// gaps is how many times each gap between consecutive casts occurred.
func gaps(ticks []int) map[int]int {
	out := map[int]int{}
	for i := 1; i < len(ticks); i++ {
		out[ticks[i]-ticks[i-1]]++
	}
	return out
}

func heatStock() []struct{ id, n int32 } {
	return []struct{ id, n int32 }{{mastercraftplan.NatureRuneID, 1000}, {tIronOre, 45}, {mastercraftplan.CoalID, 90}}
}

// heatPickLow is heatPick with craftSuperheatLowBars on: the fleet no longer superheats Iron bars (Steel and up
// only, 8 Oct 2026), but the run's switch from one bar to the next is the same mechanism.
func heatPickLow(stock mastercraftplan.SuperheatStock) (mastercraftplan.Bar, bool) {
	set := mastercraftplan.DefaultSettings()
	set.SuperheatLowBars = true
	p := mastercraftplan.PlanSuperheat(set, 60, stock, true)
	return p.Bar, p.Casts > 0
}

// THE POINT: a cast every three ticks and one booth tick a load, on a quick link and on a slow
// one alike: 45 steel bars in five loads are 40 three-tick gaps and four four-tick ones, nothing
// waits on the bank, and every ore and coal is accounted for.
func TestSuperheatRunThreeTicksACast(t *testing.T) {
	for _, lag := range []int{0, 1} {
		e, d, q := heatRig(t, lag, heatStock())
		r := SuperheatRun(d, tile.Tile{X: 3212, Z: 3212}, HeatOptions{Pick: heatPick, Staff: true, Quick: q})
		t.Logf("lag %d: %s", lag, r.Describe())
		if r.Reason != "done" || r.Casts != 45 || r.Loads != 5 {
			t.Fatalf("lag %d: %+v", lag, r)
		}
		if got := gaps(e.castTicks); got[3] != 40 || got[4] != 4 || len(got) != 2 {
			t.Fatalf("lag %d: cast gaps %v (ticks %v)", lag, got, e.castTicks)
		}
		if e.heatTotal(tSteelBar) != 45 || e.heatTotal(tIronOre) != 0 || e.heatTotal(mastercraftplan.CoalID) != 0 || e.heatTotal(mastercraftplan.NatureRuneID) != 955 {
			t.Fatalf("lag %d: steel %d ore %d coal %d natures %d", lag, e.heatTotal(tSteelBar), e.heatTotal(tIronOre), e.heatTotal(mastercraftplan.CoalID), e.heatTotal(mastercraftplan.NatureRuneID))
		}
		if r.Booths != 4 || r.Lag != lag {
			t.Fatalf("lag %d: %d booth ops, measured lag %d", lag, r.Booths, r.Lag)
		}
		if e.badBytes != 0 {
			t.Fatalf("lag %d: %d packets the sim could not parse", lag, e.badBytes)
		}
	}
}

// A link that is quick one frame and slow the next still loses nothing: every bar made, a cast
// at most a tick late now and then.
func TestSuperheatRunAbsorbsJitter(t *testing.T) {
	e, d, q := heatRig(t, 0, heatStock())
	e.lagOf = func(frame int) int { return (frame / 3) % 2 }
	r := SuperheatRun(d, tile.Tile{X: 3212, Z: 3212}, HeatOptions{Pick: heatPick, Staff: true, Quick: q})
	t.Log(r.Describe())
	if r.Reason != "done" || r.Casts != 45 || e.heatTotal(tSteelBar) != 45 {
		t.Fatalf("%+v; steel %d", r, e.heatTotal(tSteelBar))
	}
	span := e.castTicks[len(e.castTicks)-1] - e.castTicks[0]
	if span > 136+20 {
		t.Fatalf("45 casts over %d ticks, against 136 on a steady link", span)
	}
}

// When the coal runs out the round goes on with iron bars: the steel bars banked, the iron drawn
// with Withdraw-All, nothing left.
func TestSuperheatRunSwitchesToIronWhenTheCoalRunsOut(t *testing.T) {
	e, d, q := heatRig(t, 0, []struct{ id, n int32 }{{mastercraftplan.NatureRuneID, 500}, {tIronOre, 30}, {mastercraftplan.CoalID, 40}})
	r := SuperheatRun(d, tile.Tile{X: 3212, Z: 3212}, HeatOptions{Pick: heatPickLow, Staff: true, Quick: q})
	t.Log(r.Describe())
	if r.Reason != "done" || r.Casts != 30 {
		t.Fatalf("%+v", r)
	}
	if e.heatTotal(tSteelBar) != 20 || e.heatTotal(tIronBar) != 10 || e.heatTotal(tIronOre) != 0 {
		t.Fatalf("steel %d iron bars %d ore %d", e.heatTotal(tSteelBar), e.heatTotal(tIronBar), e.heatTotal(tIronOre))
	}
	if len(r.Made) != 2 || r.Made[0].Casts != 20 || r.Made[1].Casts != 10 {
		t.Fatalf("made %+v", r.Made)
	}
}

// The hold ends the run at once, and the next run carries on from the pack as it was left.
func TestSuperheatRunStopsOnTheHoldAndResumes(t *testing.T) {
	e, d, q := heatRig(t, 0, heatStock())
	n := 0
	r := SuperheatRun(d, tile.Tile{X: 3212, Z: 3212}, HeatOptions{Pick: heatPick, Staff: true, Quick: q, Hold: func() bool { n++; return n < 6 }})
	if r.Reason != "held" || r.Casts == 0 || r.Casts >= 45 {
		t.Fatalf("held run %+v", r)
	}
	first := r.Casts
	r = SuperheatRun(d, tile.Tile{X: 3212, Z: 3212}, HeatOptions{Pick: heatPick, Staff: true, Quick: q})
	if r.Reason != "done" || first+r.Casts != 45 || e.heatTotal(tSteelBar) != 45 {
		t.Fatalf("resumed run %+v after %d; steel %d", r, first, e.heatTotal(tSteelBar))
	}
}

// Nothing to make is "done" at once, with the bank opened and closed and no cast sent.
func TestSuperheatRunWithNothingToMake(t *testing.T) {
	e, d, q := heatRig(t, 0, []struct{ id, n int32 }{{mastercraftplan.NatureRuneID, 10}})
	r := SuperheatRun(d, tile.Tile{X: 3212, Z: 3212}, HeatOptions{Pick: heatPick, Staff: true, Quick: q})
	if r.Reason != "done" || r.Casts != 0 || r.Copies != 0 || len(e.castTicks) != 0 {
		t.Fatalf("%+v", r)
	}
}
