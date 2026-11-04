package retaliateplan

import (
	"fmt"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// drive runs the controller against Sim: tick t's observation is fed to the controller, whose answer is written for the engine
// to read at tick t+1+lag (lag is the link's extra delay in ticks). The first tick is the plain chop's opening click.
func drive(t *testing.T, st Settings, cfg Config, ticks, lag int, hits func(int) bool, clear Op) (*Sim, *Controller, []Action) {
	t.Helper()
	ctl := NewController(st)
	s := New(cfg)
	pending := map[int][]Op{0: {OpClickTree}}
	var acts []Action
	for tk := 0; tk < ticks; tk++ {
		before := len(s.Rolls)
		s.Tick(pending[tk], hits(tk))
		a := ctl.Observe(Obs{N: tk, Hit: hits(tk), Roll: len(s.Rolls) > before})
		acts = append(acts, a)
		switch a {
		case ActClear:
			pending[tk+1+lag] = append(pending[tk+1+lag], clear)
		case ActClick:
			pending[tk+1+lag] = append(pending[tk+1+lag], OpClickTree)
		}
	}
	return s, ctl, acts
}

func TestControllerLocksAndRollsEveryTwoTicksWhateverTheParity(t *testing.T) {
	for phase := 0; phase < 4; phase++ {
		for lag := 0; lag <= 2; lag++ {
			st := DefaultSettings()
			st.Lead = lag
			hits := Union(EveryN(4, phase), EveryN(4, phase+2))
			s, ctl, _ := drive(t, st, shortbowRapidNoAmmo, 240, lag, hits, OpDrop)
			if !ctl.Locked() {
				t.Fatalf("phase %d lag %d: not locked, hits %v", phase, lag, ctl.Hits())
			}
			g := Gaps(s.Rolls)
			if len(g) < 60 {
				t.Fatalf("phase %d lag %d: only %d gaps in 240 ticks", phase, lag, len(g))
			}
			if !allEqual(g[len(g)-50:], 2) {
				t.Fatalf("phase %d lag %d: gaps %v, want the steady state to be 2", phase, lag, g)
			}
			if len(s.Shots) != 0 {
				t.Fatalf("phase %d lag %d: %d shots fired with no ammo", phase, lag, len(s.Shots))
			}
		}
	}
}

func TestControllerWritesExactlyOnePacketAnyTickWhileLocked(t *testing.T) {
	_, ctl, acts := drive(t, DefaultSettings(), shortbowRapidNoAmmo, 120, 1, twoRats(), OpClear)
	if !ctl.Locked() {
		t.Fatal("never locked")
	}
	var run []Action
	for _, a := range acts {
		if a != ActNone {
			run = append(run, a)
		}
	}
	for i := 1; i < len(run); i++ {
		if run[i] == run[i-1] {
			t.Fatalf("clear and click must alternate, got %v", run)
		}
	}
	// from the tick it locks, every tick carries a write
	first := -1
	for i, a := range acts {
		if a != ActNone {
			first = i
			break
		}
	}
	for i := first; i < len(acts); i++ {
		if acts[i] == ActNone {
			t.Fatalf("tick %d wrote nothing while locked", i)
		}
	}
}

func TestControllerFindsTheLeadFromTheMissedClicks(t *testing.T) {
	// the link delivers 1 tick late but the controller starts at lead 0, then 2: it must rotate until a click lands
	for _, start := range []int{0, 2} {
		st := DefaultSettings()
		st.Lead = start
		s, ctl, _ := drive(t, st, shortbowRapidNoAmmo, 400, 1, twoRats(), OpClear)
		if ctl.Lead() != 1 {
			t.Fatalf("start %d: lead %d, want 1 (stats %s)", start, ctl.Lead(), ctl.Stats().Line())
		}
		g := Gaps(s.Rolls)
		if !allEqual(g[len(g)-40:], 2) {
			t.Fatalf("start %d: gaps %v, want the tail to be 2", start, g)
		}
		if ctl.Stats().Retunes < 1 {
			t.Fatalf("start %d: the lead was never re-aimed", start)
		}
		if v, why := ctl.Verdict(); v != VerdictRun {
			t.Fatalf("start %d: verdict %v (%s) at the end of a good run", start, v, why)
		}
	}
}

func TestControllerWritesNothingWithOneRat(t *testing.T) {
	_, ctl, acts := drive(t, DefaultSettings(), shortbowRapidNoAmmo, 200, 1, EveryN(4, 1), OpClear)
	for i, a := range acts {
		if a != ActNone {
			t.Fatalf("tick %d wrote %v with a single rat", i, a)
		}
	}
	if ctl.Locked() || ctl.Pattern() != PatternOneRat {
		t.Fatalf("locked %v pattern %v", ctl.Locked(), ctl.Pattern())
	}
	if v, why := ctl.Verdict(); v != VerdictRephase || !strings.Contains(why, "every 4 ticks") {
		t.Fatalf("verdict %v %q, want a re-phase that names the every-4 pattern", v, why)
	}
}

func TestControllerNamesTheOneTickApartPattern(t *testing.T) {
	_, ctl, _ := drive(t, DefaultSettings(), shortbowRapidNoAmmo, 100, 0, Union(EveryN(4, 1), EveryN(4, 2)), OpClear)
	if ctl.Locked() || ctl.Pattern() != PatternOneApart {
		t.Fatalf("hits 1 apart: locked %v pattern %v hits %v", ctl.Locked(), ctl.Pattern(), ctl.Hits())
	}
	if v, _ := ctl.Verdict(); v != VerdictRephase {
		t.Fatalf("verdict %v, want a re-phase", v)
	}
}

func TestClassifyHits(t *testing.T) {
	cases := []struct {
		hits []int
		want Pattern
	}{
		{[]int{1, 3, 5}, PatternTooFew},
		{[]int{1, 3, 5, 7}, PatternTwoApart},
		{[]int{1, 5, 9, 13}, PatternOneRat},
		{[]int{1, 2, 5, 6}, PatternOneApart},
		{[]int{2, 5, 6, 9}, PatternOneApart},
		{[]int{1, 3, 6, 7}, PatternOther},
		{[]int{0, 9, 11, 13, 15}, PatternTwoApart},
	}
	for _, c := range cases {
		if got := ClassifyHits(c.hits); got != c.want {
			t.Errorf("%v: %v, want %v", c.hits, got, c.want)
		}
	}
}

func TestControllerUnlocksWhenTheHitsStopAndRelocksWhenTheyReturn(t *testing.T) {
	hit := func(n int) bool { return n < 80 && n%2 == 1 || n >= 120 && n%2 == 0 }
	_, ctl, acts := drive(t, DefaultSettings(), shortbowRapidNoAmmo, 100, 0, hit, OpClear)
	if ctl.Locked() {
		t.Fatalf("hits stopped at 80 but the controller is still locked (acts %v)", acts[80:])
	}
	if ctl.Stats().Unlocks != 1 {
		t.Fatalf("unlocks %d, want 1", ctl.Stats().Unlocks)
	}
	for i := 86; i < 100; i++ {
		if acts[i] != ActNone {
			t.Fatalf("tick %d wrote %v after the hits stopped", i, acts[i])
		}
	}
	_, ctl2, _ := drive(t, DefaultSettings(), shortbowRapidNoAmmo, 160, 0, hit, OpClear)
	if !ctl2.Locked() || ctl2.Parity() != 0 {
		t.Fatalf("after the hits returned on even ticks: locked %v parity %d", ctl2.Locked(), ctl2.Parity())
	}
}

func TestControllerHeldWritesNothingButKeepsThePhase(t *testing.T) {
	ctl := NewController(DefaultSettings())
	for n := 0; n < 20; n++ {
		ctl.Observe(Obs{N: n, Hit: n%2 == 1})
	}
	if !ctl.Locked() {
		t.Fatal("not locked")
	}
	ctl.SetHeld(true)
	for n := 20; n < 30; n++ {
		if a := ctl.Observe(Obs{N: n, Hit: n%2 == 1}); a != ActNone {
			t.Fatalf("held but wrote %v at %d", a, n)
		}
	}
	ctl.SetHeld(false)
	if !ctl.Locked() {
		t.Fatal("the phase was lost while held")
	}
	if a := ctl.Observe(Obs{N: 30, Hit: false}); a == ActNone {
		t.Fatal("resumed but wrote nothing")
	}
}

func TestControllerIgnoresAReplayedTick(t *testing.T) {
	ctl := NewController(DefaultSettings())
	ctl.Observe(Obs{N: 5, Hit: true})
	if a := ctl.Observe(Obs{N: 5, Hit: true}); a != ActNone || ctl.Stats().Hits != 1 {
		t.Fatalf("a repeated tick counted: %v hits %d", a, ctl.Stats().Hits)
	}
}

func TestVerdictWaitsOutTheWarmupAndTheSettlingRats(t *testing.T) {
	ctl := NewController(DefaultSettings())
	for n := 0; n < Warmup-1; n++ {
		ctl.Observe(Obs{N: n, Hit: n == 1 || n == 5})
		if v, _ := ctl.Verdict(); v != VerdictRun {
			t.Fatalf("tick %d: gave up inside the warm-up", n)
		}
	}
}

func TestLowGainShareAsksForARephase(t *testing.T) {
	// a locked loop whose rolls are 4 apart (clears and clicks lost): the share of 2-tick gaps is 0
	ctl := NewController(DefaultSettings())
	for n := 0; n < 200; n++ {
		ctl.Observe(Obs{N: n, Hit: n%2 == 1, Roll: n%4 == 2})
	}
	share, n := ctl.GainShare()
	if n < GainMinGaps || share != 0 {
		t.Fatalf("share %v of %d", share, n)
	}
	if v, why := ctl.Verdict(); v != VerdictRephase {
		t.Fatalf("verdict %v %q", v, why)
	}
}

func TestRephaserCountsAndRefusesAfterTheLastTry(t *testing.T) {
	r := Rephaser{Max: 3}
	for i := 1; i <= 3; i++ {
		if n, ok := r.Next(); !ok || n != i {
			t.Fatalf("try %d: %d %v", i, n, ok)
		}
	}
	if _, ok := r.Next(); ok {
		t.Fatal("a fourth try was allowed")
	}
	r.Held()
	if _, ok := r.Next(); !ok {
		t.Fatal("holding earns the tries back")
	}
}

func TestSettingsNormaliseAndDefaultsAreStable(t *testing.T) {
	d := DefaultSettings()
	if got := d.Normalised(); got != d {
		t.Fatalf("defaults change under Normalised: %+v vs %+v", got, d)
	}
	bad := Settings{Site: "nowhere", Clear: "x", Full: "y", Aggro: " ", PhaseTries: -1, MinGain: 7, Lead: 9, StopHPPercent: 500, Fallback: "z", FallbackMins: -3, ReportEvery: 1}.Normalised()
	if bad.Lead != MaxLead {
		t.Fatalf("a lead above the maximum clamps to it, got %d", bad.Lead)
	}
	bad.Lead = d.Lead
	if bad != d {
		t.Fatalf("bad values should fall back to the defaults: %+v", bad)
	}
	if got := (Settings{Lead: 9}).Normalised().Lead; got != MaxLead {
		t.Fatalf("lead clamp %d", got)
	}
	if l := (Settings{Aggro: "Confuse, weaken ,,Wind Strike"}).AggroList(); fmt.Sprint(l) != "[confuse weaken wind strike]" {
		t.Fatalf("aggro list %v", l)
	}
	if tl, ok := (Settings{Stand: " 3003, 3312"}).StandTile(); !ok || tl != (tile.Tile{X: 3003, Z: 3312}) {
		t.Fatalf("stand %v %v", tl, ok)
	}
	for _, s := range []string{"", "3003", "a,b", "0,5"} {
		if _, ok := (Settings{Stand: s}).StandTile(); ok {
			t.Fatalf("%q parsed", s)
		}
	}
}
