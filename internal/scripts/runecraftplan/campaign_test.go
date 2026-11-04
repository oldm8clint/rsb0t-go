package runecraftplan

import (
	"strings"
	"testing"
	"time"
)

// startOK is a view that starts a campaign: the mule holds 72 Natures and 12,400 essence, Runecrafting 44, six carriers ready.
func startOK(mod func(v *CampaignStartView)) CampaignStartView {
	v := CampaignStartView{
		Known: true, Level: 44, Members: true, MuleHeard: true, MuleSameWorld: true, MuleKnown: true,
		MuleNature: 72, MuleEssence: 12400, MuleTalisman: 7, TalismanHeld: true,
		Listed: 0, AtWork: 0, Standby: 6,
		StartBelow: DefaultStartBelow, MinEssence: DefaultMinEssence, MinRunners: DefaultMinRunners,
	}
	if mod != nil {
		mod(&v)
	}
	return v
}

func TestCampaignTriggerTable(t *testing.T) {
	for _, c := range []struct {
		name  string
		mod   func(v *CampaignStartView)
		start bool
		why   string
	}{
		{"the design's case: mule 72, RC 44, essence 12k, 6 carriers", nil, true, "start - mule Nature 72 < 1,000; 12,400 essence on the mule; 6 carriers ready"},
		{"the mule holds 5,000 Natures", func(v *CampaignStartView) { v.MuleNature = 5000 }, false, "holds 5,000 Nature runes, not under the 1,000"},
		{"exactly the threshold does not start", func(v *CampaignStartView) { v.MuleNature = 1000 }, false, "not under the 1,000"},
		{"one under the threshold starts", func(v *CampaignStartView) { v.MuleNature = 999 }, true, ""},
		{"Runecrafting 43", func(v *CampaignStartView) { v.Level = 43 }, false, "Runecrafting 43 is under the 44"},
		{"the stat is unread", func(v *CampaignStartView) { v.Known = false }, false, "has not been read"},
		{"a free world", func(v *CampaignStartView) { v.Members = false }, false, "not a members world"},
		{"essence 3k and no buy-out", func(v *CampaignStartView) { v.MuleEssence = 3000; v.BuyWhy = "the market budget is off" }, false, "holds 3,000 Rune essence, no buy-out: the market budget is off - 5,000 are needed"},
		{"essence 3k and a buy-out that funds 40k", func(v *CampaignStartView) { v.MuleEssence = 3000; v.BuyOut = 40000 }, true, "40,000 more by a buy-out"},
		{"essence 0 and a buy-out of exactly the minimum", func(v *CampaignStartView) { v.MuleEssence = 0; v.BuyOut = 5000 }, true, ""},
		{"one carrier", func(v *CampaignStartView) { v.Standby = 1 }, false, "1 carrier(s) ready (0 listed, 0 at work, 1 standing by), 3 needed"},
		{"carriers named in the list count", func(v *CampaignStartView) { v.Standby = 0; v.Listed = 4 }, true, "4 carriers ready"},
		{"at work and standing by add up", func(v *CampaignStartView) { v.Standby = 1; v.AtWork = 2 }, true, "3 carriers ready"},
		{"the list is the larger of the two", func(v *CampaignStartView) { v.Standby = 1; v.AtWork = 1; v.Listed = 8 }, true, "8 carriers ready"},
		{"no talisman in hand and none on the mule", func(v *CampaignStartView) { v.TalismanHeld = false; v.MuleTalisman = 0 }, false, "no Nature talisman"},
		{"no talisman in hand but the mule has seven: the round asks it", func(v *CampaignStartView) { v.TalismanHeld = false }, true, ""},
		{"the mule is not heard", func(v *CampaignStartView) { v.MuleHeard = false }, false, "not heard"},
		{"the mule is on another world", func(v *CampaignStartView) { v.MuleSameWorld = false }, false, "another world"},
		{"the mule has not published: an unknown 0 must never start", func(v *CampaignStartView) { v.MuleKnown = false; v.MuleNature = 0 }, false, "has not published"},
	} {
		start, why := DecideCampaignStart(startOK(c.mod))
		if start != c.start {
			t.Errorf("%s: start %v (%s), want %v", c.name, start, why, c.start)
			continue
		}
		if c.why != "" && !strings.Contains(why, c.why) {
			t.Errorf("%s: why %q lacks %q", c.name, why, c.why)
		}
	}
}

// After a campaign finishes the trigger is disarmed until the mule's Natures fall under the start level: a stop at 60,000 that the
// fleet uses down to 59,000 must not start another, one it has used down to 800 must.
func TestCampaignHysteresis(t *testing.T) {
	for _, c := range []struct {
		mule  int
		start bool
	}{{59000, false}, {5000, false}, {1000, false}, {999, true}, {800, true}} {
		v := startOK(func(v *CampaignStartView) { v.MuleNature = c.mule; v.Disarmed = true })
		start, why := DecideCampaignStart(v)
		if start != c.start {
			t.Errorf("disarmed, mule %d: start %v (%s)", c.mule, start, why)
		}
		if !start && !strings.Contains(why, "re-arms when") {
			t.Errorf("disarmed, mule %d: %q should say it re-arms", c.mule, why)
		}
	}
	// Armed again (never finished, or re-armed by a low reading), a mule at 5,000 still does not start a NEW campaign...
	if start, _ := DecideCampaignStart(startOK(func(v *CampaignStartView) { v.MuleNature = 5000 })); start {
		t.Fatal("a campaign must not start with the mule at 5,000")
	}
	// ...but one UNDER WAY goes on whatever the mule holds (it ends at the stop level, DecideCampaignEnd's business).
	if start, why := DecideCampaignStart(startOK(func(v *CampaignStartView) { v.MuleNature = 30000; v.Active = true })); !start || !strings.Contains(why, "go on") {
		t.Fatalf("an active campaign goes on with the mule at 30,000: %v %s", start, why)
	}
}

func endView(mod func(v *CampaignEndView)) CampaignEndView {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	v := CampaignEndView{
		Now: now, Started: now.Add(-time.Hour), MaxRound: 8 * time.Hour,
		Target: DefaultTarget, Crafted: 12000, StopAbove: DefaultStopAbove, MuleNature: 700, MuleKnown: true,
		Carriers: 6, LastTrade: now.Add(-time.Minute),
		Held: 6000, Keep: 4000, HandOverAt: DefaultHandOverAt, HandOverMin: DefaultHandOverMin,
		CourierOn: true, LastCourier: now.Add(-10 * time.Minute), CourierWait: DefaultCourierWait,
	}
	if mod != nil {
		mod(&v)
	}
	return v
}

func TestCampaignEndTable(t *testing.T) {
	for _, c := range []struct {
		name string
		mod  func(v *CampaignEndView)
		kind EndKind
		why  string
	}{
		{"a quiet gate runs on", nil, EndNone, ""},
		{"the target reached", func(v *CampaignEndView) { v.Crafted = 100000 }, EndTarget, "crafted 100,000 Nature runes"},
		{"over the target", func(v *CampaignEndView) { v.Crafted = 100025 }, EndTarget, ""},
		{"one under the target runs on", func(v *CampaignEndView) { v.Crafted = 99999 }, EndNone, ""},
		{"the mule at 61,200", func(v *CampaignEndView) { v.MuleNature = 61200 }, EndMuleFull, "holds 61,200 Nature runes"},
		{"the mule at the stop level exactly", func(v *CampaignEndView) { v.MuleNature = 60000 }, EndMuleFull, ""},
		{"the mule at 59,999 runs on", func(v *CampaignEndView) { v.MuleNature = 59999 }, EndNone, ""},
		{"the mule's stock is not fresh: no stop on a stale reading", func(v *CampaignEndView) { v.MuleNature = 70000; v.MuleKnown = false }, EndNone, ""},
		{"eight hours", func(v *CampaignEndView) { v.Started = v.Now.Add(-8 * time.Hour) }, EndTime, "craftRuneMaxHours 8h0m0s"},
		{"seven hours fifty-nine", func(v *CampaignEndView) { v.Started = v.Now.Add(-8*time.Hour + time.Minute) }, EndNone, ""},
		{"fewer than two carriers for half an hour", func(v *CampaignEndView) { v.Carriers = 1; v.LowSince = v.Now.Add(-31 * time.Minute) }, EndCarriers, "fewer than 2 carriers"},
		{"fewer than two carriers for twenty minutes", func(v *CampaignEndView) { v.Carriers = 1; v.LowSince = v.Now.Add(-20 * time.Minute) }, EndNone, ""},
		{"no carriers in the warm-up", func(v *CampaignEndView) {
			v.Carriers = 0
			v.Started = v.Now.Add(-10 * time.Minute)
			v.LowSince = v.Now.Add(-10 * time.Minute)
			v.LastTrade = time.Time{}
		}, EndNone, ""},
		{"no essence for 30 minutes: two dry checks", func(v *CampaignEndView) { v.LastTrade = v.Now.Add(-30 * time.Minute) }, EndDry, "2 of 2 dry checks"},
		{"no essence for 29 minutes is one check", func(v *CampaignEndView) { v.LastTrade = v.Now.Add(-29 * time.Minute) }, EndNone, ""},
		{"a round that never traded, an hour in", func(v *CampaignEndView) { v.LastTrade = time.Time{} }, EndDry, "dry checks"},
		{"a round that never traded in the warm-up", func(v *CampaignEndView) { v.LastTrade = time.Time{}; v.Started = v.Now.Add(-11 * time.Minute) }, EndNone, ""},
		{"24,300 held, keep 4,000, no courier for 3 hours: deliver", func(v *CampaignEndView) {
			v.Held = 24300
			v.Started = v.Now.Add(-4 * time.Hour)
			v.LastCourier = v.Now.Add(-3 * time.Hour)
		}, EndHandOver, "20,300 Nature runes"},
		{"18,000 held, a courier 10 minutes ago: it will come again", func(v *CampaignEndView) { v.Held = 18000 }, EndNone, ""},
		{"32,000 held, a courier 10 minutes ago: twice the trip level, deliver", func(v *CampaignEndView) { v.Held = 32000 }, EndHandOver, "twice the 14,000"},
		{"held 4,500: nothing beyond the keep to speak of", func(v *CampaignEndView) { v.Held = 4500; v.LastCourier = time.Time{} }, EndNone, ""},
		{"the target wins over a hand-over", func(v *CampaignEndView) { v.Crafted = 100000; v.Held = 40000 }, EndTarget, ""},
		{"time wins over dry", func(v *CampaignEndView) { v.Started = v.Now.Add(-9 * time.Hour); v.LastTrade = time.Time{} }, EndTime, ""},
	} {
		end, kind, why := DecideCampaignEnd(endView(c.mod))
		if kind != c.kind || end != (c.kind != EndNone) {
			t.Errorf("%s: end %v kind %q (%s), want %q", c.name, end, kind, why, c.kind)
			continue
		}
		if c.why != "" && !strings.Contains(why, c.why) {
			t.Errorf("%s: why %q lacks %q", c.name, why, c.why)
		}
	}
}

// The refill and the walk to the ruins come before the gate opens and can take most of an hour: the dry checks, the warm-up and the
// courier's wait are measured from the opening, not from the start of the round, so a gate is not called dry before a carrier could
// have reached it.
func TestCampaignEndMeasuresFromTheOpening(t *testing.T) {
	// Started 50 minutes ago (a refill, a walk), opened 5 minutes ago: nothing has been dry yet.
	v := endView(func(v *CampaignEndView) {
		v.Started = v.Now.Add(-50 * time.Minute)
		v.Opened = v.Now.Add(-5 * time.Minute)
		v.LastTrade = time.Time{}
		v.Carriers = 0
		v.LowSince = v.Now.Add(-5 * time.Minute)
	})
	if end, kind, why := DecideCampaignEnd(v); end {
		t.Fatalf("a gate five minutes open is not dry: %s %s", kind, why)
	}
	// Opened 40 minutes ago with no trade: dry.
	v.Opened = v.Now.Add(-40 * time.Minute)
	v.LowSince = time.Time{}
	if end, kind, _ := DecideCampaignEnd(v); !end || kind != EndDry {
		t.Fatalf("40 minutes open and no trade: %v %s", end, kind)
	}
	// The round's own time still counts from the start.
	v = endView(func(v *CampaignEndView) {
		v.Started = v.Now.Add(-8*time.Hour - time.Minute)
		v.Opened = v.Now.Add(-time.Minute)
	})
	if end, kind, _ := DecideCampaignEnd(v); !end || kind != EndTime {
		t.Fatalf("%v %s", end, kind)
	}
	// An opening before the start (a resumed round) is the start.
	v = endView(func(v *CampaignEndView) { v.Opened = v.Started.Add(-time.Hour); v.LastTrade = time.Time{} })
	if end, kind, _ := DecideCampaignEnd(v); !end || kind != EndDry {
		t.Fatalf("%v %s", end, kind)
	}
}

func TestEndKinds(t *testing.T) {
	for _, k := range []EndKind{EndTarget, EndMuleFull} {
		if !k.Finishes() || k.Continues() {
			t.Errorf("%s finishes the campaign", k)
		}
	}
	for _, k := range []EndKind{EndHandOver, EndTime, EndCarriers, EndDry, EndNone} {
		if k.Finishes() {
			t.Errorf("%s must not finish the campaign (it re-arms only after a target or a full mule)", k)
		}
	}
	if !EndHandOver.Continues() || EndTime.Continues() || EndDry.Continues() {
		t.Error("only the hand-over trip goes straight on")
	}
	for _, k := range []EndKind{EndTarget, EndMuleFull, EndHandOver, EndTime, EndCarriers, EndDry} {
		if k.Describe() == "" || k.Describe() == string(k) {
			t.Errorf("%s has no phrase", k)
		}
	}
}

// The design's hand-over arithmetic: held 24,300 with a keep of 4,000 hands over 20,300; held 4,500 hands over nothing worth a trip;
// held 14,000 hands over 10,000 (a courier's, not an own trip's); at the stop it is held less keep whatever the size.
func TestHandOverMath(t *testing.T) {
	for _, c := range []struct {
		held, keep, surplus int
	}{{24300, 4000, 20300}, {4500, 4000, 500}, {14000, 4000, 10000}, {4000, 4000, 0}, {100, 4000, 0}, {9000, 0, 9000}} {
		if got := HandOverSurplus(c.held, c.keep); got != c.surplus {
			t.Errorf("held %d keep %d: surplus %d, want %d", c.held, c.keep, got, c.surplus)
		}
	}
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	trip := func(held int, courier time.Duration, on bool) bool {
		due, _ := OwnTripDue(OwnTripView{Now: now, Started: now.Add(-6 * time.Hour), Held: held, Keep: 4000, At: 14000, Min: 5000, CourierOn: on, LastCourier: now.Add(-courier), Wait: 2 * time.Hour})
		return due
	}
	for _, c := range []struct {
		name    string
		held    int
		courier time.Duration
		on      bool
		due     bool
	}{
		{"held 4,500", 4500, time.Hour, true, false},
		{"held 14,000 (surplus 10,000 under the 14,000)", 14000, 3 * time.Hour, true, false},
		{"held 18,000, courier 3h ago: no courier is coming", 18000, 3 * time.Hour, true, true},
		{"held 18,000, courier 1h ago: one will come", 18000, time.Hour, true, false},
		{"held 18,000, couriers off", 18000, 0, false, true},
		{"held 31,999, courier 1h ago (surplus 27,999 under twice 14,000)", 31999, time.Hour, true, false},
		{"held 32,000, courier 1h ago (surplus 28,000 = twice)", 32000, time.Hour, true, true},
	} {
		if got := trip(c.held, c.courier, c.on); got != c.due {
			t.Errorf("%s: due %v, want %v", c.name, got, c.due)
		}
	}
	// A surplus under the least worth a trip is never one, whatever else holds.
	if due, _ := OwnTripDue(OwnTripView{Now: now, Started: now.Add(-6 * time.Hour), Held: 7000, Keep: 4000, At: 1000, Min: 5000, CourierOn: false}); due {
		t.Error("a surplus of 3,000 is under craftRuneHandOverMin 5,000")
	}
	// No courier yet this round counts from the start of the round, not from forever ago.
	due, _ := OwnTripDue(OwnTripView{Now: now, Started: now.Add(-30 * time.Minute), Held: 20000, Keep: 4000, At: 14000, Min: 5000, CourierOn: true, Wait: 2 * time.Hour})
	if due {
		t.Error("half an hour into a round with no courier yet is not two hours without one")
	}
}

func TestDryChecksCountFromTheLastTradeOrTheStart(t *testing.T) {
	start := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		now   time.Duration
		trade time.Duration // since start; -1: none
		want  int
	}{{14 * time.Minute, -1, 0}, {15 * time.Minute, -1, 1}, {31 * time.Minute, -1, 2}, {40 * time.Minute, 20 * time.Minute, 1}, {50 * time.Minute, 20 * time.Minute, 2}, {2 * time.Hour, 119 * time.Minute, 0}} {
		trade := time.Time{}
		if c.trade >= 0 {
			trade = start.Add(c.trade)
		}
		if got := DryChecks(start.Add(c.now), start, trade); got != c.want {
			t.Errorf("now +%s, trade +%s: %d checks, want %d", c.now, c.trade, got, c.want)
		}
	}
	// A trade stamped before this round started counts from the start.
	if got := DryChecks(start.Add(20*time.Minute), start, start.Add(-3*time.Hour)); got != 1 {
		t.Errorf("a trade from a previous round: %d checks, want 1", got)
	}
}

func TestCarrierWatch(t *testing.T) {
	var w CarrierWatch
	t0 := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	if since := w.Note(t0, 5, 2); !since.IsZero() {
		t.Fatalf("5 carriers are not few: %v", since)
	}
	if since := w.Note(t0.Add(time.Minute), 1, 2); !since.Equal(t0.Add(time.Minute)) {
		t.Fatalf("since %v", since)
	}
	if since := w.Note(t0.Add(10*time.Minute), 0, 2); !since.Equal(t0.Add(time.Minute)) {
		t.Fatalf("the count stays low: since is the first low reading, %v", since)
	}
	if since := w.Note(t0.Add(11*time.Minute), 2, 2); !since.IsZero() {
		t.Fatalf("two carriers are enough and reset the watch: %v", since)
	}
	if since := w.Note(t0.Add(12*time.Minute), 1, 2); !since.Equal(t0.Add(12 * time.Minute)) {
		t.Fatalf("a new low spell starts afresh: %v", since)
	}
}

func TestCampaignNeed(t *testing.T) {
	for _, c := range []struct{ target, crafted, mule, bank, pack, want int }{
		{100000, 0, 0, 0, 0, 100000}, {100000, 20000, 5000, 0, 0, 75000}, {100000, 99000, 5000, 0, 0, 0}, {100000, 0, 40000, 2000, 25, 57975}, {100000, 100000, 0, 0, 0, 0},
	} {
		if got := CampaignNeed(c.target, c.crafted, c.mule, c.bank, c.pack); got != c.want {
			t.Errorf("%+v: %d", c, got)
		}
	}
}

func TestRateWindowAndETA(t *testing.T) {
	var r RateWindow
	t0 := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	if r.PerHour(t0) != 0 {
		t.Fatal("no samples, no rate")
	}
	// 900 Natures in ten minutes: 5,400 an hour.
	for i := 0; i <= 10; i++ {
		r.Note(t0.Add(time.Duration(i)*time.Minute), i*90)
	}
	if got := r.PerHour(t0.Add(10 * time.Minute)); got < 5300 || got > 5500 {
		t.Fatalf("rate %d, want about 5,400", got)
	}
	// Twenty more minutes with growth stopping: the window slides and the rate falls to nothing, not stays at the old figure.
	for i := 11; i <= 30; i++ {
		r.Note(t0.Add(time.Duration(i)*time.Minute), 900)
	}
	if got := r.PerHour(t0.Add(30 * time.Minute)); got != 0 {
		t.Fatalf("a stalled gate's rate is %d, want 0", got)
	}
	// A stall measured before the next sample lands falls with the minutes too.
	var q RateWindow
	q.Note(t0, 0)
	q.Note(t0.Add(5*time.Minute), 450)
	if a, b := q.PerHour(t0.Add(5*time.Minute)), q.PerHour(t0.Add(10*time.Minute)); !(b < a) {
		t.Fatalf("the rate must fall while nothing is crafted: %d then %d", a, b)
	}
	if ETA(40000, 5400) < 7*time.Hour || ETA(40000, 5400) > 7*time.Hour+30*time.Minute {
		t.Fatalf("ETA %s", ETA(40000, 5400))
	}
	if ETA(0, 5400) != 0 || ETA(100, 0) != 0 {
		t.Fatal("no ETA without a rate or a remainder")
	}
}

func TestCampaignLine(t *testing.T) {
	l := CampaignLine(12000, 100000, 5400, 6, 8200, 72, 6300, 4000)
	for _, want := range []string{"crafted 12,000 of 100,000", "5,400/h", "6 carrier(s) at work", "8,200 essence", "72 Nature runes", "the main account holds 6,300 (keep 4,000)", "16h"} {
		if !strings.Contains(l, want) {
			t.Errorf("%q lacks %q", l, want)
		}
	}
	if !strings.Contains(CampaignLine(0, 100000, 0, 0, 0, 0, 0, 0), "unknown") {
		t.Error("no rate, no ETA")
	}
}

func TestCampaignDefaultsAreTheDesigns(t *testing.T) {
	if DefaultStartBelow != 1000 || DefaultStopAbove != 60000 || DefaultTarget != 100000 || DefaultMinEssence != 5000 || DefaultMinRunners != 3 ||
		DefaultHandOverAt != 14000 || DefaultHandOverMin != 5000 || DefaultCourierWait != 2*time.Hour || DefaultMuleCoinFloor != 10000 {
		t.Fatal("the design's numbers moved")
	}
	// Two 15-minute dry checks are the gate's existing 30-minute dry rule.
	if time.Duration(CampaignDryChecks)*CampaignDryCheck != GateDry {
		t.Fatalf("%d dry checks of %s are not GateDry %s", CampaignDryChecks, CampaignDryCheck, GateDry)
	}
}
