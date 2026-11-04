package runecraftrunner

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/essencerunner"
	"rsb0t-go/internal/scripts/tile"
)

// fakeMine is a tiny world for the stretch loop: outside or in the mine, a pack of essence, a bank, a
// pickaxe, and the gateway's answers scripted.
type fakeMine struct {
	inMine      bool
	pack        int
	bank        int
	pick        bool
	tick        int
	stopped     bool
	stopAfter   int // stop after this many calls (0: never)
	calls       int
	tpResults   []essencerunner.TeleportResult // consumed in order; the last repeats
	tpCalls     int
	bankFails   int // BankDeposit fails this many times first
	noRock      int // Mine reports no-rock this many times first
	pickBank    bool
	events      []string
	portalFails int
}

func (f *fakeMine) count() {
	f.calls++
	if f.stopAfter > 0 && f.calls > f.stopAfter {
		f.stopped = true
	}
}

func (f *fakeMine) InMine() bool   { return f.inMine }
func (f *fakeMine) PackFull() bool { return f.pack >= 27 }
func (f *fakeMine) Haul() int {
	if f.pack > 0 {
		return 1
	}
	return 0
}
func (f *fakeMine) PickHeld() bool   { return f.pick }
func (f *fakeMine) PickName() string { return "rune pickaxe" }
func (f *fakeMine) Tick() int        { return f.tick }
func (f *fakeMine) Stopped() bool    { return f.stopped }

func (f *fakeMine) Mine() essencerunner.MineResult {
	f.count()
	f.events = append(f.events, "mine")
	if f.noRock > 0 {
		f.noRock--
		return essencerunner.MineResult{Reason: "no-rock"}
	}
	f.tick += 84
	got := 27 - f.pack
	f.pack = 27
	return essencerunner.MineResult{Gained: got, Reason: "full"}
}

func (f *fakeMine) PortalOut() bool {
	f.count()
	f.events = append(f.events, "portal")
	if f.portalFails > 0 {
		f.portalFails--
		return false
	}
	f.inMine = false
	return true
}

func (f *fakeMine) BankDeposit() (int, int, bool) {
	f.count()
	f.events = append(f.events, "bank")
	if f.bankFails > 0 {
		f.bankFails--
		return 0, 0, false
	}
	n := f.pack
	f.pack = 0
	f.bank += n
	return f.bank, n, true
}

func (f *fakeMine) FetchPickaxe() (string, bool) {
	f.count()
	f.events = append(f.events, "pickaxe")
	if !f.pickBank {
		return "", false
	}
	f.pick = true
	return "rune pickaxe", true
}

func (f *fakeMine) TeleportIn() essencerunner.TeleportResult {
	f.count()
	f.events = append(f.events, "teleport")
	var r essencerunner.TeleportResult
	if len(f.tpResults) > 0 {
		i := min(f.tpCalls, len(f.tpResults)-1)
		r = f.tpResults[i]
	} else {
		r = essencerunner.TeleportResult{Landed: true, At: tile.Tile{X: 2891, Z: 4830}, Ticks: 5}
	}
	f.tpCalls++
	if r.Landed {
		f.inMine = true
		f.tick += 5
	}
	return r
}

var guild = func() essenceplan.Gateway { g, _ := essenceplan.GatewayByKey(essenceplan.GatewayGuild); return g }()

func stretch(f *fakeMine, goal int, mod func(o *StretchOptions)) (StretchResult, []string) {
	var logs []string
	o := StretchOptions{Goal: goal, Gateway: guild, MaxFails: 4, Logf: func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }}
	if mod != nil {
		mod(&o)
	}
	return RunStretch(f, o), logs
}

func joined(logs []string) string { return strings.Join(logs, "\n") }

// The stretch: teleport in, mine 27, portal out, bank, until the goal is in the bank. Four loads for a goal
// of 100, the bank's total tracked, the lines the log needs to prove the legs live.
func TestAStretchMinesAndBanksUntilTheGoal(t *testing.T) {
	f := &fakeMine{pick: true, bank: 50}
	res, logs := stretch(f, 100, nil)
	if !res.Done || res.Banked != 108 || res.Loads != 4 || res.Teleports != 4 || res.BankEssence != 158 || res.Fails != 0 {
		t.Fatalf("%+v", res)
	}
	want := strings.Repeat("teleport,mine,portal,bank,", 4)
	if got := strings.Join(f.events, ",") + ","; got != want {
		t.Errorf("events\n got %s\nwant %s", got, want)
	}
	out := joined(logs)
	for _, line := range []string{
		"essencerunner: guild: Distentor Teleport ok, landed (2891,4830) after 5 ticks",
		"essencerunner: guild: 27 essence in 84 ticks (rune pickaxe)",
		"essencerunner: guild: banked 27 at Yanille; the bank holds 77; stretch 27 of 100",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("log lacks %q:\n%s", line, out)
		}
	}
}

// The refusal that cost the fleet 25 wasted attempts: it ends the stretch at once, flagged, with nothing
// mined and no more clicks.
func TestARefusedGatewayEndsTheStretchAtOnce(t *testing.T) {
	f := &fakeMine{pick: true, tpResults: []essencerunner.TeleportResult{{QuestRefused: true, Reason: "Rune Mysteries is not complete"}}}
	res, logs := stretch(f, 100, nil)
	if res.Done || !res.Refused || res.Banked != 0 || f.tpCalls != 1 || !strings.Contains(res.Reason, "Wizard Distentor refused") {
		t.Fatalf("%+v, %d teleports", res, f.tpCalls)
	}
	if !strings.Contains(joined(logs), "Distentor refused (Rune Mysteries is not complete) - failing at once, not waiting for the teleport") {
		t.Errorf("log:\n%s", joined(logs))
	}
}

// Teleports that do not land are counted, not retried for ever.
func TestTeleportsThatNeverLandStopTheStretch(t *testing.T) {
	f := &fakeMine{pick: true, tpResults: []essencerunner.TeleportResult{{Reason: "the teleport did not land within 15s"}}}
	res, logs := stretch(f, 100, nil)
	if res.Done || res.Teleports != 0 || res.Fails != 4 || f.tpCalls != 4 || !strings.Contains(res.Reason, "4 failures in a row") {
		t.Fatalf("%+v, %d teleports", res, f.tpCalls)
	}
	if !strings.Contains(joined(logs), "the teleport did not land (the teleport did not land within 15s) (3 in a row)") {
		t.Errorf("log:\n%s", joined(logs))
	}
	// One that lands after two misses resets the count and goes on.
	f = &fakeMine{pick: true, tpResults: []essencerunner.TeleportResult{{Reason: "x"}, {Reason: "x"}, {Landed: true, Ticks: 5}}}
	res, _ = stretch(f, 27, nil)
	if !res.Done || res.Fails != 2 || res.Teleports != 1 {
		t.Fatalf("%+v", res)
	}
}

// A restart finds the account anywhere: inside the mine with half a pack, or outside holding essence.
func TestAStretchResumesFromWhereTheAccountStands(t *testing.T) {
	f := &fakeMine{pick: true, inMine: true, pack: 10}
	res, _ := stretch(f, 27, nil)
	if !res.Done || res.Banked != 27 || strings.Join(f.events, ",") != "mine,portal,bank" {
		t.Fatalf("in the mine: %+v %v", res, f.events)
	}
	f = &fakeMine{pick: true, pack: 27}
	res, _ = stretch(f, 28, nil)
	if !res.Done || res.Banked != 54 || strings.Join(f.events, ",") != "bank,teleport,mine,portal,bank" {
		t.Fatalf("outside with a load: %+v %v", res, f.events)
	}
}

// No pickaxe held: it is drawn from the bank; with none there, the stretch fails and says so.
func TestAStretchFetchesItsPickaxe(t *testing.T) {
	f := &fakeMine{pickBank: true}
	res, logs := stretch(f, 27, nil)
	if !res.Done || f.events[0] != "pickaxe" || !strings.Contains(joined(logs), "drew a rune pickaxe from the Yanille bank") {
		t.Fatalf("%+v %v\n%s", res, f.events, joined(logs))
	}
	f = &fakeMine{}
	res, logs = stretch(f, 27, nil)
	if res.Done || res.Fails != 4 || !strings.Contains(joined(logs), "no usable pickaxe") {
		t.Fatalf("%+v\n%s", res, joined(logs))
	}
}

// A bank that will not open, a rock that is not in sight: counted and bounded; the next success clears them.
func TestAStretchBoundsItsFailures(t *testing.T) {
	f := &fakeMine{pick: true, bankFails: 2, noRock: 1}
	res, _ := stretch(f, 27, nil)
	if !res.Done || res.Fails != 3 || res.Banked != 27 {
		t.Fatalf("%+v", res)
	}
	f = &fakeMine{pick: true, bankFails: 99, pack: 27}
	res, _ = stretch(f, 27, nil)
	if res.Done || res.Fails != 4 || !strings.Contains(res.Reason, "4 failures in a row") {
		t.Fatalf("%+v", res)
	}
	f = &fakeMine{pick: true, inMine: true, pack: 27, portalFails: 99}
	res, _ = stretch(f, 27, nil)
	if res.Done || res.Fails != 4 {
		t.Fatalf("a portal that does nothing: %+v", res)
	}
	// ... and the stretch SAYS the account is still inside, after the extra tries to leave it made.
	if !res.InMine || !f.inMine || !strings.Contains(res.Reason, "STILL INSIDE") {
		t.Fatalf("a portal that does nothing leaves the account inside and says so: %+v", res)
	}
	if got := strings.Count(strings.Join(f.events, ","), "portal"); got != 4+LeaveTries {
		t.Errorf("%d portal tries, want the 4 failures and %d to leave", got, LeaveTries)
	}
}

// A stop, the round's clock and the stretch's own budget end it.
func TestAStretchEndsOnStopHoldAndBudget(t *testing.T) {
	f := &fakeMine{pick: true, stopAfter: 3}
	res, _ := stretch(f, 100000, nil)
	if res.Done || res.Reason != "stopped" || res.Banked == 0 {
		t.Fatalf("stop: %+v", res)
	}
	f = &fakeMine{pick: true}
	calls := 0
	res, _ = stretch(f, 100000, func(o *StretchOptions) {
		o.Hold = func() bool { calls++; return calls < 6 }
	})
	if res.Done || res.Reason != "the round's time is used up" {
		t.Fatalf("hold: %+v", res)
	}
	now := time.Date(2026, 10, 13, 20, 0, 0, 0, time.UTC)
	f = &fakeMine{pick: true}
	res, _ = stretch(f, 100000, func(o *StretchOptions) {
		o.Budget = time.Hour
		o.Now = func() time.Time { now = now.Add(20 * time.Minute); return now }
	})
	if res.Done || !strings.Contains(res.Reason, "time budget") {
		t.Fatalf("budget: %+v", res)
	}
}

// Nothing to do when the goal is already met.
func TestAStretchWithNoGoalDoesNothing(t *testing.T) {
	f := &fakeMine{pick: true}
	res, _ := stretch(f, 0, nil)
	if !res.Done || len(f.events) != 0 {
		t.Fatalf("%+v %v", res, f.events)
	}
}

// THE ACCOUNT IS NEVER LEFT IN THE MINE. The round's clock (Hold), the stretch's budget and the failures in a row
// are all checked at the top of every step, including while the account is inside the mine; each of them used to
// end the stretch right there, with the account in the mine (and, at a full pack, with 27 essence on it), for the
// caller to hand on to ScarabSwarm from inside. A stretch that ends for any reason takes the portal out first.

// outsideAfter runs a stretch and fails the test when the account is still in the mine at the end.
func outsideAfter(t *testing.T, what string, f *fakeMine, goal int, mod func(o *StretchOptions)) StretchResult {
	t.Helper()
	res, logs := stretch(f, goal, mod)
	if f.inMine || res.InMine {
		t.Fatalf("%s: the stretch ended inside the mine (%+v, events %v): %s", what, res, f.events, strings.Join(logs, " | "))
	}
	return res
}

// Hold turns false at every call from the k-th on, for every k a stretch can be at: in the mine at a half pack,
// in the mine at a full one, outside, at the bank. Wherever the clock runs out the account ends outside.
func TestAStretchWhoseClockRunsOutAnywhereEndsOutsideTheMine(t *testing.T) {
	for k := 1; k <= 16; k++ {
		for _, start := range []fakeMine{{pick: true}, {pick: true, inMine: true, pack: 10}, {pick: true, inMine: true, pack: 27}} {
			f := start
			calls := 0
			res := outsideAfter(t, fmt.Sprintf("hold false from call %d (start %+v)", k, start), &f, 100000, func(o *StretchOptions) {
				o.Hold = func() bool { calls++; return calls < k }
			})
			if res.Done || res.Reason != "the round's time is used up" {
				t.Errorf("hold from call %d: %+v", k, res)
			}
		}
	}
}

// The same for the stretch's own time budget, however many steps it lasts, and for a Hold that is false before
// the first step with the account already inside (a restart in the mine after the round's time ran out).
func TestAStretchOverItsBudgetOrStartedLateEndsOutsideTheMine(t *testing.T) {
	for n := 1; n <= 12; n++ {
		for _, start := range []fakeMine{{pick: true}, {pick: true, inMine: true, pack: 5}, {pick: true, inMine: true, pack: 27}} {
			f := start
			now := time.Date(2026, 10, 13, 20, 0, 0, 0, time.UTC)
			res := outsideAfter(t, fmt.Sprintf("budget of %d steps (start %+v)", n, start), &f, 100000, func(o *StretchOptions) {
				o.Budget = time.Duration(n) * 20 * time.Minute
				o.Now = func() time.Time { now = now.Add(20 * time.Minute); return now }
			})
			if res.Done || !strings.Contains(res.Reason, "time budget") {
				t.Errorf("budget %d: %+v", n, res)
			}
		}
	}
	f := fakeMine{pick: true, inMine: true, pack: 27}
	res := outsideAfter(t, "expired before the first step", &f, 100000, func(o *StretchOptions) { o.Hold = func() bool { return false } })
	if strings.Join(f.events, ",") != "portal" || res.Banked != 0 || res.Fails != 0 {
		t.Errorf("expired in the mine with a full pack: the portal and nothing else, %v %+v", f.events, res)
	}
}

// Failures in a row end it too: no rock in sight four times, inside the mine; the portal failing four times and then
// working; teleports that never land (the account is outside throughout).
func TestAStretchWhoseFailuresRunOutEndsOutsideTheMine(t *testing.T) {
	f := fakeMine{pick: true, inMine: true, pack: 3, noRock: 99}
	res := outsideAfter(t, "no rock in sight", &f, 100000, nil)
	if res.Done || !strings.Contains(res.Reason, "4 failures in a row") || res.Fails != 4 || strings.Join(f.events, ",") != "mine,mine,mine,mine,portal" {
		t.Errorf("no rock: %+v %v", res, f.events)
	}
	f = fakeMine{pick: true, inMine: true, pack: 27, portalFails: 4}
	res = outsideAfter(t, "a portal that fails four times", &f, 100000, nil)
	if res.Done || res.Fails != 4 || strings.Join(f.events, ",") != "portal,portal,portal,portal,portal" {
		t.Errorf("portal 4 failures then ok: %+v %v", res, f.events)
	}
	f = fakeMine{pick: true, tpResults: []essencerunner.TeleportResult{{Reason: "x"}}}
	outsideAfter(t, "teleports that never land", &f, 100000, nil)
}

// A portal that takes the account out on the second of the leaving tries is enough: not inside, and not flagged.
func TestLeavingTheMineIsTriedMoreThanOnce(t *testing.T) {
	f := &fakeMine{pick: true, inMine: true, pack: 27, portalFails: 1}
	if !LeaveMine(f, LeaveTries) || f.inMine || strings.Join(f.events, ",") != "portal,portal" {
		t.Errorf("%v inMine %v", f.events, f.inMine)
	}
	// Already outside: nothing is clicked. A stop: nothing more is clicked.
	f = &fakeMine{pick: true}
	if !LeaveMine(f, LeaveTries) || len(f.events) != 0 {
		t.Errorf("outside: %v", f.events)
	}
	f = &fakeMine{pick: true, inMine: true, portalFails: 99, stopped: true}
	if LeaveMine(f, LeaveTries) || len(f.events) != 0 {
		t.Errorf("stopped: %v", f.events)
	}
}
