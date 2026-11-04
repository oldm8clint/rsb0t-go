package mastercraftrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/pearlplan"
)

// pearlRig is an account at the booth with a chisel and 27 Oyster pearls in the pack and more in
// the bank, Fletching at fletchXP tenths, after one 7-tick load: the 27 cut into one stack of Pearl
// bolttips in the first slot, and the booth op primed behind the last burst, as the pearl round
// leaves it.
type pearlRig struct {
	e     *tickEngine
	d     *fleetrunner.Deps
	q     *QuickBank
	kind  pearlplan.Kind
	total int // pearls in all
	xp0   int
}

func newPearlRig(t *testing.T, stock, fletchXP int) *pearlRig {
	t.Helper()
	kind, _ := pearlplan.KindByID(pearlplan.PearlsID)
	e, d, q := newTickEngine(t, load(kind.ID, 27), []struct{ id, n int32 }{{kind.ID, int32(stock)}})
	e.fletchXP = fletchXP
	e.push()
	r := &pearlRig{e: e, d: d, q: q, kind: kind, total: stock + 27, xp0: fletchXP}
	c := CutPearls(GemDeps(d), kind, func() { q.Prime(d) })
	if c.Reason != "done" || c.Cut != 27 || c.Tips != 27*24 {
		t.Fatalf("the 7-tick load: %s", c.Describe())
	}
	if !q.Primed() {
		t.Fatal("the booth op did not go out behind the last burst")
	}
	return r
}

// conserve: every pearl is in the bank or the pack, or cut into 24 tips; the cuts are the XP's.
func (r *pearlRig) conserve(t *testing.T) {
	t.Helper()
	var pearls, tips int32
	for _, b := range r.e.bank {
		switch b.id {
		case r.kind.ID:
			pearls += b.n
		case pearlplan.TipsID:
			tips += b.n
		}
	}
	pearls += r.e.packTotal(r.kind.ID)
	tips += r.e.packTotal(pearlplan.TipsID)
	cut := int(tips) / r.kind.Tips
	if int(tips)%r.kind.Tips != 0 || int(pearls)+cut != r.total {
		t.Fatalf("%d pearls + %d tips, want %d pearls in all", pearls, tips, r.total)
	}
	if got := (r.e.fletchXP - r.xp0) / pearlplan.XPTenths; got != cut {
		t.Fatalf("%d cuts by the XP, %d by the tips", got, cut)
	}
}

// THE POINT: a pearl load is the gem load event for event, six ticks: Deposit-All of the tip
// stack, Withdraw-All of 27 pearls, 27 uses, the booth op the fifth event of the sixth tick.
func TestFastPearlsSixTicksALoad(t *testing.T) {
	r := newPearlRig(t, 27*12, levels.XPAtLevel(70)*10)
	const loads = 10
	refused0 := r.e.refused
	res := FastPearls(r.d, r.q, r.kind, FastOptions{MaxLoads: loads})
	if res.Reason != "done" || res.Loads != loads || res.Cuts != 27*loads || res.XP != 27*loads*pearlplan.XPTenths/10 {
		t.Fatalf("%s", res.Describe())
	}
	if r.e.refused != refused0 || r.e.badBytes != 0 {
		t.Fatalf("the engine refused %d event(s), %d bad frames", r.e.refused-refused0, r.e.badBytes)
	}
	run := r.e.cutTicks[27:]
	for i := 1; i < loads; i++ {
		if gap := run[27*(i+1)-1] - run[27*i-1]; gap != 6 {
			t.Fatalf("load %d took %d ticks, want 6", i, gap)
		}
	}
	for tick, n := range r.e.usesTick {
		if n > 5 {
			t.Fatalf("tick %d applied %d uses", tick, n)
		}
	}
	if res.Chunks != mastercraftplan.FastChunks(loads) {
		t.Fatalf("%d chunks", res.Chunks)
	}
	// It ends with the chisel and one stack of 648 tips in the first slot, the bank shut.
	chisel, ok, why := fastLayout(packItems(r.d), pearlplan.TipsID, true, r.kind.Name)
	if !ok || r.e.open || r.e.packTotal(pearlplan.TipsID) != 27*24 || chisel != 0 {
		t.Fatalf("pack: %s, bank open %v, %d tips", why, r.e.open, r.e.packTotal(pearlplan.TipsID))
	}
	r.conserve(t)
	// The next visit is the 7-tick one, and it banks the stack and draws 27 pearls.
	if qv := r.q.Visit(r.d, KeepChisel, PearlDraw); !qv.Done || r.e.packTotal(r.kind.ID) != 27 || r.e.packTotal(pearlplan.TipsID) != 0 {
		t.Fatalf("%+v", qv)
	}
	r.conserve(t)
}

// The level-up guard reads Fletching XP: the load whose booth tick gains a level is the run's last,
// sent without its booth op, and the quick visit after it closes the box.
func TestFastPearlsEndsBeforeAFletchingLevelInABoothTick(t *testing.T) {
	// The 52nd use of the run (load 2, use 25: its booth tick) reaches Fletching 71.
	runXP := levels.XPAtLevel(71)*10 - 51*pearlplan.XPTenths - 20
	r := newPearlRig(t, 27*10, runXP-27*pearlplan.XPTenths)
	r.e.levelUps = true
	if got := mastercraftplan.FastLoads(8, runXP/10, pearlplan.XPTenths); got != 2 {
		t.Fatalf("FastLoads = %d, want 2", got)
	}
	refused0 := r.e.refused
	res := FastPearls(r.d, r.q, r.kind, FastOptions{MaxLoads: 8})
	if res.Reason != "done" || res.Planned != 2 || res.Loads != 2 || r.e.refused != refused0 {
		t.Fatalf("%s, %d refused", res.Describe(), r.e.refused-refused0)
	}
	if r.e.levelUpsSeen != 1 || !r.e.chatBox {
		t.Fatalf("level-ups %d, box open %v", r.e.levelUpsSeen, r.e.chatBox)
	}
	if qv := r.q.Visit(r.d, KeepChisel, PearlDraw); !qv.Done || r.e.chatBox || r.e.packTotal(r.kind.ID) != 27 {
		t.Fatalf("the quick visit after the level-up: %+v", qv)
	}
	r.conserve(t)
}

// A stray click desyncs the stream; the 7-tick path resyncs from whatever the pack holds, and the
// next run is clean. No pearl or tip is lost.
func TestFastPearlsStrayClickDesyncsAndTheQuickPathRecovers(t *testing.T) {
	r := newPearlRig(t, 27*20, levels.XPAtLevel(70)*10)
	r.e.strayAt = map[int]bool{r.e.ticks + 9: true}
	res := FastPearls(r.d, r.q, r.kind, FastOptions{MaxLoads: 10})
	if res.Reason != "desync" || res.Loads >= 10 || r.e.refused == 0 {
		t.Fatalf("%s, %d refused", res.Describe(), r.e.refused)
	}
	r.conserve(t)
	if qv := r.q.Visit(r.d, KeepChisel, PearlDraw); !qv.Done || r.e.packTotal(r.kind.ID) != 27 {
		t.Fatalf("the quick visit after the desync: %+v", qv)
	}
	if c := CutPearls(GemDeps(r.d), r.kind, func() { r.q.Prime(r.d) }); c.Reason != "done" || c.Cut != 27 {
		t.Fatalf("the 7-tick cut after the desync: %s", c.Describe())
	}
	refused0 := r.e.refused
	again := FastPearls(r.d, r.q, r.kind, FastOptions{MaxLoads: 4})
	if again.Reason != "done" || again.Loads != 4 || r.e.refused != refused0 {
		t.Fatalf("the next run: %s", again.Describe())
	}
	r.conserve(t)
}

// A run that cannot start (the pack is not one tip stack in the first slot, or the bank's next
// pearl kind is another) makes the visit the 7-tick way instead.
func TestFastPearlsNotFastMakesTheQuickVisit(t *testing.T) {
	r := newPearlRig(t, 27*3, levels.XPAtLevel(70)*10)
	small, _ := pearlplan.KindByID(pearlplan.PearlID) // the bank's next is Oyster pearls
	res := FastPearls(r.d, r.q, small, FastOptions{MaxLoads: 5})
	if res.Reason != "not-fast" || res.Load == nil || res.Load.Err != "" || len(res.Load.Drew) != 1 || res.Load.Drew[0] != pearlplan.PearlsID {
		t.Fatalf("%s / %+v", res.Describe(), res.Load)
	}
	if r.e.packTotal(pearlplan.PearlsID) != 27 || !res.Load.ChiselHeld {
		t.Fatalf("pack holds %d Oyster pearls", r.e.packTotal(pearlplan.PearlsID))
	}
}
