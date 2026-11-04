package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/fisherrunner"
)

func TestFishManipSettingDefaultsOn(t *testing.T) {
	if !readFisherManip(nil) || !readFisherManip(map[string]interface{}{}) {
		t.Fatal("fishManip does not default on")
	}
	if readFisherManip(map[string]interface{}{"fishManip": false}) {
		t.Fatal("fishManip: false left the manipulation on")
	}
}

// Deps carry the manipulation only while the setting is on, and always the bot's one state, so the measured link delay and the plain
// baseline survive from one Loop call to the next.
func TestFisherDepsCarryTheManipulationState(t *testing.T) {
	ctx := &Context{}
	if d := (&fisherBot{}).deps(ctx); d.Manip != nil {
		t.Fatal("deps carry the manipulation with the setting off")
	}
	b := &fisherBot{manipOn: true}
	d1, d2 := b.deps(ctx), b.deps(ctx)
	if d1.Manip == nil || !d1.Manip.Enabled || d1.Manip.State == nil || d1.Manip.State != d2.Manip.State || d1.Manip.State != b.manip {
		t.Fatalf("deps %+v %+v: want one shared enabled state", d1.Manip, d2.Manip)
	}
	bo := &fisherBot{bassOyster: true, manipOn: true}
	bo.applyBassOysterSettings(ctx)
	if d := bo.deps(ctx); d.Manip == nil || !d.Manip.Enabled {
		t.Fatal("BassOysterCaskets' deps lack the manipulation")
	}
}

// A hand-off never gives the kit to the mule.
func TestKitKeepNamesHoldTheManipulationKit(t *testing.T) {
	on := strings.Join((&fisherBot{manipOn: true}).kitKeepNames(), ",")
	if !strings.Contains(on, "Knife") || !strings.Contains(on, "Logs") {
		t.Fatalf("keep list %q lacks the knife or the logs", on)
	}
	if names := (&fisherBot{}).kitKeepNames(); len(names) != 0 {
		t.Fatalf("keep list %v with the manipulation and the staller off", names)
	}
}

// A fisher that holds neither asks for a knife and the plain logs; what it holds in the pack or the bank is not asked for.
func TestManipKitLines(t *testing.T) {
	in := fisherKitInput{ManipOn: true, PackLogs: map[string]int{}, BankLogs: map[string]int{}}
	by := linesByItem(fisherKitLines(in))
	if len(by) != 1 || by["Knife"].Count != 1 || !by["Knife"].Rider {
		t.Fatalf("lines %+v: want the knife", by)
	}
	l, ok := manipLogsLine(in)
	if !ok || l.Choices[0] != "Logs" || l.Count != fisherrunner.ManipLogsWanted {
		t.Fatalf("logs line %+v %v", l, ok)
	}
	in.PackManipLogs, in.PackKnife = fisherrunner.ManipLogsWanted, 1
	if _, ok := manipLogsLine(in); ok {
		t.Fatal("asked for logs with enough held")
	}
	if lines := fisherKitLines(in); len(lines) != 0 {
		t.Fatalf("lines %+v with the knife held", lines)
	}
	in.ManipOn = false
	in.PackKnife, in.PackManipLogs = 0, 0
	if lines := fisherKitLines(in); len(lines) != 0 {
		t.Fatalf("lines %+v with the manipulation off", lines)
	}
}

// The kit order carries the knife and the logs un-noted (a noted log cannot be used on a knife), the logs a share of the mule's stock.
func TestManipKitPlanAsksPlainLogsUnnoted(t *testing.T) {
	in := fisherKitInput{ManipOn: true, PackLogs: map[string]int{}, BankLogs: map[string]int{}}
	in.MuleStock = stockOf(map[string]int{"Knife": 229, "Logs": 1200})
	kit, none := fisherKitPlan(in)
	if len(none) != 0 {
		t.Fatalf("none %v", none)
	}
	got := map[string]int{}
	for _, l := range kit {
		if l.Noted {
			t.Errorf("line %+v is noted", l)
		}
		got[l.Item] += l.Qty
	}
	if got["Knife"] != 1 || got["Logs"] != fisherrunner.ManipLogsWanted || len(kit) != 2 {
		t.Fatalf("kit %+v", kit)
	}
	// A mule with next to no logs hands none of its last.
	in.MuleStock = stockOf(map[string]int{"Knife": 229, "Logs": 1})
	kit, none = fisherKitPlan(in)
	for _, l := range kit {
		if l.Item == "Logs" {
			t.Errorf("asked for logs of a mule holding one: %+v", kit)
		}
	}
	// The training's noted logs and the manipulation's plain ones are separate lines.
	in = fisherKitInput{ManipOn: true, TrainOn: true, Level: 1, PackLogs: map[string]int{}, BankLogs: map[string]int{}}
	in.MuleStock = stockOf(map[string]int{"Knife": 229, "Logs": 1200, "Oak logs": 54582, "Willow logs": 138203})
	kit, _ = fisherKitPlan(in)
	var noted, plain int
	for _, l := range kit {
		if l.Item == "Logs" && l.Noted {
			noted++
		}
		if l.Item == "Logs" && !l.Noted {
			plain++
		}
		if l.Item == "Knife" && l.Qty != 1 {
			t.Errorf("knife asked %d", l.Qty)
		}
	}
	if noted != 1 || plain != 1 {
		t.Fatalf("kit %+v: want one noted and one plain Logs line", kit)
	}
}

// The bank step is never repeated inside bankRepeatMin, and the log says why it was asked for.
func TestBankRepeatWait(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	if w := bankRepeatWait(time.Time{}, now); w != 0 {
		t.Fatalf("a first bank visit waits %s", w)
	}
	if w := bankRepeatWait(now.Add(-time.Second), now); w != 9*time.Second {
		t.Fatalf("a visit one second after the last waits %s, want 9s", w)
	}
	if w := bankRepeatWait(now.Add(-bankRepeatMin), now); w != 0 {
		t.Fatalf("a visit %s after the last waits %s", bankRepeatMin, w)
	}
	why := bankWhy(fisherplan.FishSnapshot{PackFull: true, FishHeld: 0, Coins: 5, HeldTools: []string{"Big fishing net"}})
	for _, want := range []string{"the pack is full", "5 coins", "Big fishing net"} {
		if !strings.Contains(why, want) {
			t.Errorf("bankWhy %q lacks %q", why, want)
		}
	}
}

// The kit trip is for a fisher with the manipulation on and a kit missing everywhere; the kit it asks for carries the knife or the logs.
func TestManipKitTripNeedsAMissingKit(t *testing.T) {
	if _, need := (&fisherBot{}).manipKitNeeded(&fisherrunner.Deps{}); need {
		t.Fatal("a fisher with the manipulation off wants a kit trip")
	}
	if kitHasManip(nil) {
		t.Error("an empty kit carries the manipulation's items")
	}
	in := fisherKitInput{ManipOn: true, PackLogs: map[string]int{}, BankLogs: map[string]int{}}
	in.MuleStock = stockOf(map[string]int{"Knife": 226, "Logs": 14280})
	kit, _ := fisherKitPlan(in)
	if !kitHasManip(kit) {
		t.Fatalf("kit %+v lacks the knife and the logs", kit)
	}
	in.MuleStock = stockOf(map[string]int{"Knife": 0, "Logs": 0})
	if kit, _ := fisherKitPlan(in); kitHasManip(kit) {
		t.Fatalf("kit %+v asked for what the mule lacks", kit)
	}
}
