package flinchplan

import "testing"

func TestNextSettle(t *testing.T) {
	s := FreshSettle
	if s.Ticks != SETTLE_TICKS {
		t.Fatalf("fresh settle ticks = %d, want %d", s.Ticks, SETTLE_TICKS)
	}

	// A hit buys a tick and resets the clean run.
	s2 := NextSettle(s, true)
	if s2.Ticks != SETTLE_TICKS+1 || s2.CleanRun != 0 {
		t.Fatalf("got %+v", s2)
	}

	// It caps at SETTLE_MAX.
	s3 := SettleState{Ticks: SETTLE_MAX, CleanRun: 0}
	if got := NextSettle(s3, true); got.Ticks != SETTLE_MAX {
		t.Fatalf("expected the settle to cap at %d, got %d", SETTLE_MAX, got.Ticks)
	}

	// Clean swings build up a run, easing back one tick after SETTLE_EASE_AFTER in a row.
	high := SettleState{Ticks: SETTLE_TICKS + 2, CleanRun: 0}
	for i := 0; i < SETTLE_EASE_AFTER-1; i++ {
		high = NextSettle(high, false)
		if high.Ticks != SETTLE_TICKS+2 {
			t.Fatalf("settle moved early at clean swing %d: %+v", i, high)
		}
	}
	eased := NextSettle(high, false)
	if eased.Ticks != SETTLE_TICKS+1 || eased.CleanRun != 0 {
		t.Fatalf("expected an ease after %d clean swings, got %+v", SETTLE_EASE_AFTER, eased)
	}

	// Never eases below SETTLE_TICKS.
	atFloor := SettleState{Ticks: SETTLE_TICKS, CleanRun: SETTLE_EASE_AFTER - 1}
	floored := NextSettle(atFloor, false)
	if floored.Ticks != SETTLE_TICKS {
		t.Fatalf("settle eased below the floor: %+v", floored)
	}
}

func TestResetVerdictFrom(t *testing.T) {
	cases := []struct {
		name string
		in   ResetInput
		want ResetVerdict
	}{
		{"both bars gone, not yet settled", ResetInput{MyBar: false, TargetBar: false, TicksClear: 0, SettleTicks: 2}, ResetWaiting},
		{"both bars gone, settled", ResetInput{MyBar: false, TargetBar: false, TicksClear: 2, SettleTicks: 2}, ResetReady},
		{"my bar still up", ResetInput{MyBar: true, TargetBar: false, TicksWaited: 3, MaxTicks: 25}, ResetWaiting},
		{"target bar still up", ResetInput{MyBar: false, TargetBar: true, TicksWaited: 3, MaxTicks: 25}, ResetWaiting},
		{"timed out", ResetInput{MyBar: false, TargetBar: true, TicksWaited: 25, MaxTicks: 25}, ResetTimeout},
		{"defaults used when zero", ResetInput{MyBar: false, TargetBar: false, TicksClear: SETTLE_TICKS}, ResetReady},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResetVerdictFrom(c.in); got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestDecideStage(t *testing.T) {
	cases := []struct {
		name string
		in   StageInput
		want FlinchStage
	}{
		{"urgent eat wins over retreat", StageInput{Swung: true, NeedEat: true, Urgent: true}, StageEat},
		{"non-urgent eat only when not retreating", StageInput{OnSafeTile: true, NeedEat: true, Urgent: false}, StageEat},
		{"retreat when swung", StageInput{Swung: true, OnSafeTile: true}, StageRetreat},
		{"retreat when off the safe tile", StageInput{OnSafeTile: false}, StageRetreat},
		{"seek when no target", StageInput{OnSafeTile: true, HasTarget: false}, StageSeek},
		{"reset when target not ready", StageInput{OnSafeTile: true, HasTarget: true, Reset: false}, StageReset},
		{"strike when everything lines up", StageInput{OnSafeTile: true, HasTarget: true, Reset: true}, StageStrike},
		{
			"eat deferred while not urgent and must retreat first",
			StageInput{Swung: true, NeedEat: true, Urgent: false},
			StageRetreat,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DecideStage(c.in); got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestFlinchQuality(t *testing.T) {
	if got := FlinchQuality(FlinchTally{Swings: 0}); got != 1 {
		t.Fatalf("no swings yet should read 100%%, got %v", got)
	}
	if got := FlinchQuality(FlinchTally{Swings: 10, HitsTaken: 0}); got != 1 {
		t.Fatalf("all clean should read 100%%, got %v", got)
	}
	if got := FlinchQuality(FlinchTally{Swings: 10, HitsTaken: 10}); got != 0 {
		t.Fatalf("all hit should read 0%%, got %v", got)
	}
	if got := FlinchQuality(FlinchTally{Swings: 4, HitsTaken: 1}); got != 0.75 {
		t.Fatalf("got %v want 0.75", got)
	}
}

func TestParseTile(t *testing.T) {
	cases := []struct {
		raw                 string
		wantX, wantZ, wantL int
		wantOK              bool
	}{
		{"3200,3200", 3200, 3200, 0, true},
		{"3200, 3200, 1", 3200, 3200, 1, true},
		{"3200 3200", 3200, 3200, 0, true},
		{"", 0, 0, 0, false},
		{"not a tile", 0, 0, 0, false},
		{"-10,-20", -10, -20, 0, true},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			x, z, l, ok := ParseTile(c.raw)
			if ok != c.wantOK {
				t.Fatalf("ok = %v want %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if x != c.wantX || z != c.wantZ || l != c.wantL {
				t.Fatalf("got (%d,%d,%d) want (%d,%d,%d)", x, z, l, c.wantX, c.wantZ, c.wantL)
			}
		})
	}
}

func TestSwingWatchTypeExistsElsewhere(t *testing.T) {
	// flinchrunner.SwingWatch (the live-state-driven twin of the TS SwingWatch class) is
	// intentionally not duplicated here — it needs gamestate.State.SelfAnim/Tick, an I/O
	// concern this pure package has none of. See flinchrunner's own SwingWatch.
}
