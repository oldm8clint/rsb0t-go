package mastercraftplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/cannonfacts"
)

func runSnap(mule map[string]int, in CannonRunInput) Snapshot {
	s := readySnapshot(mule)
	s.Now = cannonNow
	s.CannonRun = in
	return s
}

func ownedCannon() CannonRunInput {
	return CannonRunInput{OwnKnown: true, Parts: 4, Coins: 100, QuestKnown: true, QuestDone: true, Ranged: 40}
}

func TestCannonRunTriggersAtTenThousandBalls(t *testing.T) {
	set := DefaultSettings()
	if set.CannonRun.Start != 10000 || set.CannonRun.Balls != 10000 {
		t.Fatalf("the owner's 10k and 10k: %+v", set.CannonRun)
	}
	if _, ok := CannonRunReady(set, runSnap(map[string]int{"Cannonball": 9999}, ownedCannon()), CannonRunLedger{}); ok {
		t.Error("9,999 balls started a run")
	}
	in := ownedCannon()
	in.OwnBalls = 1000
	p, ok := CannonRunReady(set, runSnap(map[string]int{"Cannonball": 9000}, in), CannonRunLedger{})
	if !ok || p.Resume || p.Target != 10000 || p.Available != 10000 {
		t.Fatalf("10,000 between the mule and the bank did not start a run: %+v", p)
	}
	set.CannonRun.Start = 0
	if _, ok := CannonRunReady(set, runSnap(map[string]int{"Cannonball": 50000}, ownedCannon()), CannonRunLedger{}); ok {
		t.Error("start 0 turns the round off")
	}
}

func TestCannonRunKeepsRunningUntilTenThousandAreUsedThenRearms(t *testing.T) {
	set := DefaultSettings()
	var led CannonRunLedger
	led.Begin(10000, cannonNow)
	if done := led.NoteUsed(3000, cannonNow); done || led.Remaining() != 7000 {
		t.Fatalf("a stint of 3,000: done %v remaining %d", done, led.Remaining())
	}
	// Under way the trigger no longer applies: 4,000 on hand carries the run on.
	p, ok := CannonRunReady(set, runSnap(map[string]int{"Cannonball": 4000}, ownedCannon()), led)
	if !ok || !p.Resume || p.Remaining != 7000 {
		t.Fatalf("a run under way with balls on hand did not resume: %+v ok %v", p, ok)
	}
	// Out of balls: the run waits for them, and says how far it got.
	if p, ok := CannonRunReady(set, runSnap(map[string]int{"Cannonball": 10}, ownedCannon()), led); ok || !strings.Contains(p.Why, "3000 of 10000") {
		t.Errorf("a run with no balls started: %+v", p)
	}
	if done := led.NoteUsed(7000, cannonNow.Add(time.Hour)); !done || led.Active || led.Runs != 1 || led.Fired != 10000 {
		t.Fatalf("10,000 used must end the run: %+v", led)
	}
	// The next trigger is a new run again at 10k.
	if _, ok := CannonRunReady(set, runSnap(map[string]int{"Cannonball": 6000}, ownedCannon()), led); ok {
		t.Error("6,000 balls started the next run")
	}
	if p, ok := CannonRunReady(set, runSnap(map[string]int{"Cannonball": 10000}, ownedCannon()), led); !ok || p.Resume || p.Target != 10000 {
		t.Errorf("another 10k did not trigger a new run: %+v", p)
	}
}

func TestCannonRunNeedsTheQuestAndAMembersWorld(t *testing.T) {
	set := DefaultSettings()
	in := ownedCannon()
	in.QuestDone = false
	if p, ok := CannonRunReady(set, runSnap(map[string]int{"Cannonball": 20000}, in), CannonRunLedger{}); ok || !strings.Contains(p.Why, DwarfCannonQuest) {
		t.Errorf("an unfinished quest ran: %+v", p)
	}
	set.MembersWorld = false
	if _, ok := CannonRunReady(set, runSnap(map[string]int{"Cannonball": 20000}, ownedCannon()), CannonRunLedger{}); ok {
		t.Error("a free world ran")
	}
}

func TestCannonRunBuysTheSetOnlyWithTheCoinsAndSaysWhatIsMissing(t *testing.T) {
	set := DefaultSettings()
	in := ownedCannon()
	in.Parts, in.Coins = 0, 100000
	p, ok := CannonRunReady(set, runSnap(map[string]int{"Cannonball": 20000}, in), CannonRunLedger{})
	if ok || !strings.Contains(p.Why, "750000") || !strings.Contains(p.Why, "100000") {
		t.Fatalf("no cannon and 100,000 coins must say what is short: %+v", p)
	}
	in.Coins = 800000
	if p, ok := CannonRunReady(set, runSnap(map[string]int{"Cannonball": 20000}, in), CannonRunLedger{}); !ok || p.Errand != ErrandBuy {
		t.Errorf("800,000 coins buys the set: %+v ok %v", p, ok)
	}
	set.CannonRun.Buy = false
	if _, ok := CannonRunReady(set, runSnap(map[string]int{"Cannonball": 20000}, in), CannonRunLedger{}); ok {
		t.Error("craftCannonRunBuy off still bought")
	}
	set.CannonRun.Buy = true
	in.Parts = 2
	if p, ok := CannonRunReady(set, runSnap(map[string]int{"Cannonball": 20000}, in), CannonRunLedger{}); ok || !strings.Contains(p.Why, "2 of the 4") {
		t.Errorf("a partial set is not bought over: %+v", p)
	}
}

func TestCannonRunRecoversAStandingCannonBeforeAnything(t *testing.T) {
	set := DefaultSettings()
	var led CannonRunLedger
	led.Placed = led.Placed.WithPlaced(2529, 3237, cannonNow.Add(-time.Hour))
	in := ownedCannon()
	in.Parts = 0
	// Even with no balls and no parts: the cannon is never left without its record.
	p, ok := CannonRunReady(set, runSnap(map[string]int{}, in), led)
	if !ok || p.Errand != ErrandRecover {
		t.Fatalf("a recorded cannon was not recovered first: %+v ok %v", p, ok)
	}
	// Decide recovers it, and leaves it alone while a pause (the wait for the decay) runs.
	s := runSnap(map[string]int{}, in)
	s.Ledger = &Ledger{CannonRun: led}
	if d := Decide(set, s); d.Mode != ModeCannonRun || d.CannonRun.Errand != ErrandRecover {
		t.Errorf("Decide did not recover the cannon: %+v", d.Mode)
	}
	led.PauseUntil = cannonNow.Add(time.Hour)
	s.Ledger = &Ledger{CannonRun: led}
	if d := Decide(set, s); d.Mode == ModeCannonRun {
		t.Error("a paused round was started to recover the cannon")
	}
}

func TestDecideRunsTheCannonWhenTenThousandBallsAreAvailable(t *testing.T) {
	set := DefaultSettings()
	s := runSnap(map[string]int{"Cannonball": 12000}, ownedCannon())
	d := Decide(set, s)
	if d.Mode != ModeCannonRun || d.CannonRun.Target != 10000 {
		t.Fatalf("Decide = %s (%s)", d.Mode, d.Why)
	}
	s = runSnap(map[string]int{"Cannonball": 500}, ownedCannon())
	if d := Decide(set, s); d.Mode == ModeCannonRun {
		t.Errorf("500 balls ran the cannon: %s", d.Why)
	}
}

func TestCannonRunEnds(t *testing.T) {
	base := CannonRunEndInput{Now: cannonNow, Start: cannonNow, Stint: time.Hour, Target: 10000, Used: 100, Balls: 500, CannonUp: true, Food: 8, Arrows: 400}
	if end, why, _ := CannonRunEnd(base); end {
		t.Fatalf("a healthy stint ended: %s", why)
	}
	for name, mod := range map[string]func(*CannonRunEndInput){
		"time":   func(i *CannonRunEndInput) { i.Now = i.Start.Add(time.Hour) },
		"target": func(i *CannonRunEndInput) { i.Used = 10000 },
		"food":   func(i *CannonRunEndInput) { i.Food = 0 },
		"arrows": func(i *CannonRunEndInput) { i.Arrows = 0 },
		"balls":  func(i *CannonRunEndInput) { i.Balls, i.CannonLoaded, i.CannonUp = 0, 0, false },
		"died":   func(i *CannonRunEndInput) { i.Deaths = 1 },
		"stop":   func(i *CannonRunEndInput) { i.Stopped = true },
	} {
		in := base
		mod(&in)
		if end, _, _ := CannonRunEnd(in); !end {
			t.Errorf("%s did not end the stint", name)
		}
	}
	// A cannon still up and loaded is not out of balls.
	in := base
	in.Balls, in.CannonLoaded = 0, 12
	if end, why, _ := CannonRunEnd(in); end {
		t.Errorf("a loaded cannon ended the stint: %s", why)
	}
}

func TestCannonAlchemyIsGatedByTheCannonsShareOfTime(t *testing.T) {
	tick := 600 * time.Millisecond
	in := CannonAlchInput{Magic: 70, Natures: 10, Fires: 50, Alchables: 5, Duty: 100, Now: cannonNow, Tick: tick}
	if ok, why := CannonAlchDue(in); !ok {
		t.Fatalf("continuous alchemy not due: %s", why)
	}
	in.LastCast = cannonNow.Add(-2 * time.Second)
	if ok, _ := CannonAlchDue(in); ok {
		t.Error("a cast went inside the five ticks of the last")
	}
	in.LastCast = cannonNow.Add(-3 * time.Second)
	if ok, _ := CannonAlchDue(in); !ok {
		t.Error("100% duty casts every five ticks")
	}
	// 50%: the cast holds the cannon four ticks, so the next waits four more (eight ticks a cycle).
	in.Duty = 50
	if gap, _ := CannonAlchGap(50, tick); gap != 8*tick {
		t.Errorf("50%% duty gap %s, want 8 ticks", gap)
	}
	if ok, _ := CannonAlchDue(in); ok {
		t.Error("50% duty cast 3 s after the last")
	}
	in.LastCast = cannonNow.Add(-5 * time.Second)
	if ok, _ := CannonAlchDue(in); !ok {
		t.Error("50% duty did not cast after 5 s (8 ticks is 4.8 s)")
	}
	for name, mod := range map[string]func(*CannonAlchInput){
		"magic":   func(i *CannonAlchInput) { i.Magic = 54 },
		"nature":  func(i *CannonAlchInput) { i.Natures = 0 },
		"fire":    func(i *CannonAlchInput) { i.Fires = 4 },
		"nothing": func(i *CannonAlchInput) { i.Alchables = 0 },
		"off":     func(i *CannonAlchInput) { i.Duty = 0 },
	} {
		x := in
		mod(&x)
		if ok, _ := CannonAlchDue(x); ok {
			t.Errorf("%s still cast", name)
		}
	}
}

func TestCannonRunStintBalls(t *testing.T) {
	if got := CannonRunStintBalls(DefaultSettings(), 7000, 50000); got != cannonfacts.RangedBallsCarry {
		t.Errorf("stint balls %d", got)
	}
	if got := CannonRunStintBalls(DefaultSettings(), 800, 50000); got != 800 {
		t.Errorf("the run's remainder bounds the stint: %d", got)
	}
}

func TestCannonRunSettingsDecodeAndValidate(t *testing.T) {
	s, err := DecodeSettings(map[string]interface{}{KeyCannonRunStart: 5000, KeyCannonRunBalls: 8000, KeyCannonRunStintMinutes: 90, KeyCannonRunBuy: false, KeyCannonRunAlchDuty: 100})
	if err != nil || s.CannonRun.Start != 5000 || s.CannonRun.Balls != 8000 || s.CannonRun.Stint != 90*time.Minute || s.CannonRun.Buy || s.CannonRun.AlchDuty != 100 {
		t.Fatalf("decoded %+v err %v", s.CannonRun, err)
	}
	for name, raw := range map[string]map[string]interface{}{
		"duty over":   {KeyCannonRunAlchDuty: 101},
		"short stint": {KeyCannonRunStintMinutes: 5},
		"zero balls":  {KeyCannonRunBalls: 0},
	} {
		if _, err := DecodeSettings(raw); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
