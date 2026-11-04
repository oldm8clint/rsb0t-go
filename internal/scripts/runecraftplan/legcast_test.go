package runecraftplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/levels"
)

func castView(l Leg, mode LegCastMode) LegCastView {
	return LegCastView{Leg: l, Mode: mode, Hold: true, Stack: 300, Steps: 4, Magic: 66, MagicXP: levels.XPAtLevel(66) + 100, FireStaff: true, Natures: 4000}
}

// The engine's numbers: p_delay(n) drops the reads of the n+1 ticks after the cast's own and accepts the next, and a cast read the tick
// after the walk and a click read the moment the lock ends costs max(0, n+3-steps) ticks.
func TestACastCostsWhatTheLockLeavesOfTheLeg(t *testing.T) {
	if LowLegSpell.AcceptedAfter() != 3 || HighLegSpell.AcceptedAfter() != 5 {
		t.Fatalf("the first accepted read: low %d, high %d", LowLegSpell.AcceptedAfter(), HighLegSpell.AcceptedAfter())
	}
	for _, c := range []struct {
		spell LegSpell
		steps int
		want  int
	}{
		{LowLegSpell, 4, 0}, {LowLegSpell, 3, 1}, {LowLegSpell, 6, 0}, {LowLegSpell, 2, 2},
		{HighLegSpell, 4, 2}, {HighLegSpell, 5, 1}, {HighLegSpell, 6, 0}, {HighLegSpell, 3, 3},
	} {
		if got := LegCastCost(c.spell, c.steps); got != c.want {
			t.Errorf("%s on %d steps: %d tick(s), want %d", c.spell.Name, c.steps, got, c.want)
		}
	}
	if LowLegSpell.Pays(2560) != 1024 || HighLegSpell.Pays(2560) != 1536 || HighLegSpell.Pays(1600) != 960 || LowLegSpell.Pays(1) != 1 {
		t.Errorf("the pay: low longbow %d, high longbow %d, high shortbow %d", LowLegSpell.Pays(2560), HighLegSpell.Pays(2560), HighLegSpell.Pays(1600))
	}
}

func TestTheModesCastOnTheLegsThePlanSays(t *testing.T) {
	for _, c := range []struct {
		mode          LegCastMode
		altar, portal string
	}{
		{LegCastsOff, "", ""}, {LegCastsLow2, "low", "low"}, {LegCastsHigh1, "", "high"}, {LegCastsHigh2, "high", "high"},
	} {
		for l, want := range map[Leg]string{LegAltar: c.altar, LegPortal: c.portal} {
			sp, ok := c.mode.SpellFor(l)
			if (want != "") != ok || (ok && sp.Short != want) {
				t.Errorf("%s on the %s leg: %q ok %v, want %q", c.mode, l, sp.Short, ok, want)
			}
		}
		if c.mode.Casts() != (c.altar != "" || c.portal != "") {
			t.Errorf("%s Casts() is %v", c.mode, c.mode.Casts())
		}
	}
	if m, ok := ParseLegCastMode(" HIGH1 "); !ok || m != LegCastsHigh1 {
		t.Errorf("parse: %q %v", m, ok)
	}
	if _, ok := ParseLegCastMode("both"); ok {
		t.Error("an unknown mode parsed")
	}
	if DefaultLegCasts != LegCastsLow2 {
		t.Error("the default is not low2")
	}
}

func TestDecideLegCastCastsOnAWalkingLegWithTheKit(t *testing.T) {
	for _, l := range []Leg{LegAltar, LegPortal} {
		d := DecideLegCast(castView(l, LegCastsLow2))
		if !d.Cast || d.Spell != LowLegSpell || d.Cost != 0 {
			t.Errorf("%s: %+v", l, d)
		}
	}
	// High1 casts on the portal leg only, and pays for it with two ticks on a leg of four steps.
	if d := DecideLegCast(castView(LegPortal, LegCastsHigh1)); !d.Cast || d.Spell != HighLegSpell || d.Cost != 2 {
		t.Errorf("high1 portal: %+v", d)
	}
	if d := DecideLegCast(castView(LegAltar, LegCastsHigh1)); d.Cast || !strings.Contains(d.Why, "nothing on the altar leg") {
		t.Errorf("high1 altar: %+v", d)
	}
}

// Every reason not to cast is the plain direct op, and says why.
func TestDecideLegCastRefusesAndSaysWhy(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*LegCastView)
		want   string
	}{
		{"off", func(v *LegCastView) { v.Mode = LegCastsOff }, "casts nothing"},
		{"no hold", func(v *LegCastView) { v.Hold = false }, "craftRuneAlchHold"},
		{"no bows", func(v *LegCastView) { v.Stack = 0 }, "no bows"},
		{"low Magic", func(v *LegCastView) { v.Magic = 20 }, "Magic 20"},
		{"no staff", func(v *LegCastView) { v.FireStaff = false }, "fire staff"},
		{"no natures", func(v *LegCastView) { v.Natures = AlchNaturesFloor / 2 }, "Nature"},
		{"shaky view", func(v *LegCastView) { v.Shaky = true }, "lagging"},
		{"a slow server", func(v *LegCastView) { v.PaceTicks = 3 }, "ticking slowly"},
		{"locked", func(v *LegCastView) { v.Locked = true }, "lock"},
		{"random event", func(v *LegCastView) { v.Busy = true }, "random event"},
		{"chat box", func(v *LegCastView) { v.Chat = true }, "chat box"},
		{"a short leg", func(v *LegCastView) { v.Steps = 2 }, "too few"},
		{"a leg that is run", func(v *LegCastView) { v.Running = true }, "runs this leg"},
		{"the level-up", func(v *LegCastView) { v.MagicXP = levels.XPAtLevel(67) - 20 }, "level Magic up"},
	}
	for _, c := range cases {
		v := castView(LegAltar, LegCastsLow2)
		c.mutate(&v)
		d := DecideLegCast(v)
		if d.Cast || !strings.Contains(d.Why, c.want) {
			t.Errorf("%s: %+v, want a refusal naming %q", c.name, d, c.want)
		}
	}
	// Four tiles are four ticks walked and two run; five run are three.
	if LegWalkTicks(4, false) != 4 || LegWalkTicks(4, true) != 2 || LegWalkTicks(5, true) != 3 || LegWalkTicks(1, true) != 1 {
		t.Errorf("walk ticks: %d %d %d %d", LegWalkTicks(4, false), LegWalkTicks(4, true), LegWalkTicks(5, true), LegWalkTicks(1, true))
	}
	// High Level Alchemy on a three-step leg costs three ticks, over the two it is allowed.
	v := castView(LegPortal, LegCastsHigh2)
	v.Steps = 3
	if d := DecideLegCast(v); d.Cast || !strings.Contains(d.Why, "costs 3 tick(s)") {
		t.Errorf("high on three steps: %+v", d)
	}
	// A healthy server shows five or six ticks in three seconds, and a view that has not counted yet (0) is not refused for it.
	for _, pace := range []int{0, 4, 5, 6} {
		v = castView(LegAltar, LegCastsLow2)
		v.PaceTicks = pace
		if d := DecideLegCast(v); !d.Cast {
			t.Errorf("%d ticks in the window: %+v", pace, d)
		}
	}
	// Low Level Alchemy is castable at Magic 21: the High level is not needed for it.
	v = castView(LegAltar, LegCastsLow2)
	v.Magic, v.MagicXP = 30, levels.XPAtLevel(30)
	if d := DecideLegCast(v); !d.Cast {
		t.Errorf("low at Magic 30: %+v", d)
	}
}

func TestLegCastLevelsUpOnlyWhenTheCastReachesTheNextLevel(t *testing.T) {
	next := levels.XPAtLevel(67)
	if !LegCastLevelsUp(66, next-31, 31) || !LegCastLevelsUp(66, next-1, 31) {
		t.Error("a cast that reaches the next level was not seen")
	}
	if LegCastLevelsUp(66, next-32, 31) || LegCastLevelsUp(0, 0, 31) {
		t.Error("a cast that stays under the level was refused")
	}
}

func rec(l Leg, spell LegSpell, first, sent, landed, op int) LegCastRecord {
	return LegCastRecord{Leg: l, Spell: spell, Bow: "Magic longbow", Steps: 4, FirstStep: first, Sent: sent, Landed: landed, Op: op, Tried: true, Resends: 3}
}

func TestTheRecordMeasuresWhatTheCastCost(t *testing.T) {
	// The walk seen in 100, the cast read in 101 (on time), the click read in 104: the op in 104, which is the plain direct op's frame.
	r := rec(LegAltar, LowLegSpell, 100, 100, 101, 104)
	if r.Ideal() != 104 || r.Cost() != 0 || r.Slip() != 0 || r.Excess() != 0 {
		t.Errorf("on time: ideal %d cost %d slip %d excess %d", r.Ideal(), r.Cost(), r.Slip(), r.Excess())
	}
	// A cast one tick late, a click one tick late with it: a tick over the plan.
	r = rec(LegAltar, LowLegSpell, 100, 101, 102, 105)
	if r.Cost() != 1 || r.Slip() != 1 || r.Excess() != 1 {
		t.Errorf("late: cost %d slip %d excess %d", r.Cost(), r.Slip(), r.Excess())
	}
	// High Level Alchemy is planned to cost two: the click is read at 106, which is exactly the plan.
	r = rec(LegPortal, HighLegSpell, 100, 100, 101, 106)
	if r.Cost() != 2 || r.Excess() != 0 {
		t.Errorf("high on time: cost %d excess %d", r.Cost(), r.Excess())
	}
	// Nothing known of the op: no cost either way.
	r = rec(LegAltar, LowLegSpell, 100, 100, 101, 0)
	if r.Cost() != 0 {
		t.Errorf("unseen op: cost %d", r.Cost())
	}
	line := rec(LegAltar, LowLegSpell, 100, 100, 101, 104).String()
	for _, want := range []string{"altar low (Magic longbow)", "walk seen 100", "cast written 100", "landed 101 (on time)", "click sent again 3", "op seen 104: +0 tick(s)", "planned +0"} {
		if !strings.Contains(line, want) {
			t.Errorf("the line %q lacks %q", line, want)
		}
	}
	if line := (LegCastRecord{Leg: LegPortal, Spell: HighLegSpell, Why: "a random event"}).String(); !strings.Contains(line, "not cast (a random event)") {
		t.Errorf("not cast: %q", line)
	}
	lost := rec(LegAltar, LowLegSpell, 105, 100, 100, 108)
	lost.WalkLost = true
	if line := lost.String(); !strings.Contains(line, "WALK THROWN AWAY") || !strings.Contains(line, "in the walk's own tick") {
		t.Errorf("a lost walk: %q", line)
	}
}

func okCast(excessTicks int) LegCastRecord {
	return rec(LegAltar, LowLegSpell, 100, 100, 101, 104+excessTicks)
}

func TestACleanWindowKeepsTheCastsGoing(t *testing.T) {
	c := NewLegCasts(LegCastsLow2)
	for i := 0; i < 3*LegCastWindow; i++ {
		if !c.Try(LegAltar) {
			t.Fatalf("cycle %d: the leg stood down on a clean run", i)
		}
		if line := c.Note(LegAltar, LegCastOK, okCast(0)); line != "" {
			t.Fatalf("a clean cast said %q", line)
		}
	}
	// A one-tick slip now and then (the mean is under 0.6) is not a reason.
	for i := 0; i < LegCastWindow; i++ {
		e := 0
		if i%3 == 0 {
			e = 1
		}
		c.Try(LegAltar)
		if line := c.Note(LegAltar, LegCastOK, okCast(e)); line != "" {
			t.Fatalf("a window with 3 slips in 8 said %q", line)
		}
	}
	if casts, xp := c.Total(); casts != 4*LegCastWindow || xp != 4*LegCastWindow*31 {
		t.Errorf("total %d casts, %d xp", casts, xp)
	}
}

func TestAWindowOverTheLimitStandsTheLegDownThenGivesUp(t *testing.T) {
	c := NewLegCasts(LegCastsLow2)
	var line string
	for i := 0; i < LegCastWindow; i++ {
		c.Try(LegAltar)
		line = c.Note(LegAltar, LegCastOK, okCast(1)) // a tick over every time
	}
	if !strings.Contains(line, "walks without a cast for the next 30 cycle") {
		t.Fatalf("the window said %q", line)
	}
	// The portal leg has its own score.
	if !c.Try(LegPortal) {
		t.Error("the portal leg stood down for the altar's casts")
	}
	for i := 0; i < LegCastBackoff; i++ {
		if c.Try(LegAltar) {
			t.Fatalf("cycle %d of the back-off tried a cast", i)
		}
	}
	if !c.Try(LegAltar) {
		t.Fatal("the leg did not come back after its back-off")
	}
	// Two more bad windows with no healthy one between give it up for the round.
	for w := 0; w < 2; w++ {
		for i := 0; i < LegCastWindow; i++ {
			c.Note(LegAltar, LegCastOK, okCast(2))
		}
		for c.legs[LegAltar].wait > 0 {
			c.Try(LegAltar)
		}
	}
	if !c.GivenUp(LegAltar) || c.Try(LegAltar) {
		t.Errorf("not given up: %+v", c.legs[LegAltar])
	}
	if s := c.Summary(); !strings.Contains(s, "(given up)") {
		t.Errorf("the summary %q", s)
	}
	// A healthy window between bad ones resets the count.
	c = NewLegCasts(LegCastsLow2)
	for _, e := range []int{2, 0, 2, 0, 2} {
		for i := 0; i < LegCastWindow; i++ {
			c.Note(LegAltar, LegCastOK, okCast(e))
		}
		c.legs[LegAltar].wait = 0
	}
	if c.GivenUp(LegAltar) {
		t.Error("a healthy window in between did not reset the stand-downs")
	}
}

func TestFailuresStandTheLegDownAndAUnseenResultChangesNothing(t *testing.T) {
	c := NewLegCasts(LegCastsLow2)
	bad := LegCastRecord{Leg: LegAltar, Spell: LowLegSpell, Tried: true, Why: "the cast was never seen to land"}
	if line := c.Note(LegAltar, LegCastFailed, bad); !strings.Contains(line, "plain way this cycle") {
		t.Fatalf("one failure said %q", line)
	}
	if !c.Try(LegAltar) {
		t.Fatal("one failure stood the leg down")
	}
	c.Note(LegAltar, LegCastUnseen, LegCastRecord{})
	if line := c.Note(LegAltar, LegCastFailed, bad); !strings.Contains(line, "next 15 cycle") {
		t.Fatalf("two in a row said %q", line)
	}
	if c.Try(LegAltar) {
		t.Error("the leg tried a cast in its back-off")
	}
	// A cast that worked clears the failures in a row.
	c = NewLegCasts(LegCastsLow2)
	c.Note(LegAltar, LegCastFailed, bad)
	c.Note(LegAltar, LegCastOK, okCast(0))
	if line := c.Note(LegAltar, LegCastFailed, bad); strings.Contains(line, "next 15") {
		t.Errorf("a failure after a success stood the leg down: %q", line)
	}
	lost := bad
	lost.WalkLost = true
	c.Note(LegPortal, LegCastFailed, lost)
	if c.legs[LegPortal].lost != 1 {
		t.Error("a lost walk was not counted")
	}
}

func TestLegCastsOffOrNilNeverCast(t *testing.T) {
	var nilState *LegCasts
	for _, c := range []*LegCasts{nilState, NewLegCasts(LegCastsOff)} {
		if c.On() || c.Try(LegAltar) || c.GivenUp(LegAltar) || c.Note(LegAltar, LegCastOK, okCast(0)) != "" || c.Summary() != "leg casts: off" {
			t.Errorf("%+v cast", c)
		}
		if n, xp := c.Total(); n != 0 || xp != 0 {
			t.Errorf("%+v counted", c)
		}
	}
	// A mode that casts on one leg only does not try the other.
	h := NewLegCasts(LegCastsHigh1)
	if h.Try(LegAltar) || !h.Try(LegPortal) {
		t.Error("high1 tried the wrong leg")
	}
	if s := h.Summary(); !strings.Contains(s, "altar none") {
		t.Errorf("summary %q", s)
	}
}
