package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/fisherrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/orderplan"
)

// stockOf is a mule bank as muleBankCount reports it: every item listed answers its count, any other 0.
func stockOf(held map[string]int) func(string) (int, bool) {
	return func(item string) (int, bool) { return held[item], true }
}

func linesByItem(lines []fleetplan.AskLine) map[string]fleetplan.AskLine {
	out := map[string]fleetplan.AskLine{}
	for _, l := range lines {
		out[l.Choices[0]] = l
	}
	return out
}

// An account at Fletching 1 holding nothing is short of everything: the staller's jewellery (a choice of
// sixteen), a knife, the logs of the whole climb and a chisel.
func TestFisherKitLinesForAFreshAccount(t *testing.T) {
	in := fisherKitInput{StallOn: true, TrainOn: true, CutOn: true, Level: 1, XP: 0, PackLogs: map[string]int{}, BankLogs: map[string]int{}}
	lines := fisherKitLines(in)
	by := linesByItem(lines)
	if len(lines) != 6 {
		t.Fatalf("%d lines %+v, want the jewellery, the knife, three logs and the chisel", len(lines), lines)
	}
	if rub := lines[0]; len(rub.Choices) != 16 || rub.Count != 1 || !rub.Rider {
		t.Fatalf("the jewellery line %+v", rub)
	}
	if by["Knife"].Count != 1 || by["Chisel"].Count != 1 {
		t.Fatalf("knife %+v chisel %+v", by["Knife"], by["Chisel"])
	}
	want := fisherplan.FletchAsk(0, func(string) int { return 0 })
	for _, name := range fisherplan.FletchLogNames() {
		if by[name].Count != want[name] || by[name].Count < 500 {
			t.Errorf("%s: asks %d, want %d (the climb's need and a margin)", name, by[name].Count, want[name])
		}
	}
}

// What the account holds, in the pack or the bank, is not asked for; the logs are asked net of the bank's.
func TestFisherKitLinesSubtractWhatIsHeld(t *testing.T) {
	in := fisherKitInput{
		StallOn: true, TrainOn: true, CutOn: true, Level: 1, XP: 0,
		PackRub: 1, BankKnife: 2, PackChisel: 1,
		PackLogs: map[string]int{"Logs": 100}, BankLogs: map[string]int{"Logs": 600, "Oak logs": 5000},
	}
	by := linesByItem(fisherKitLines(in))
	if len(by) != 1 || by["Willow logs"].Count == 0 {
		t.Fatalf("lines %+v: with the jewellery, knife and chisel held and the plain and oak logs banked, only willow logs are short", by)
	}
	in.BankLogs = map[string]int{"Logs": 300}
	in.PackLogs = nil
	by = linesByItem(fisherKitLines(in))
	need := fisherplan.FletchAsk(0, func(n string) int { return map[string]int{"Logs": 300}[n] })
	if by["Logs"].Count != need["Logs"] || by["Logs"].Count < 300 {
		t.Fatalf("plain logs: asks %d with 300 banked, want %d", by["Logs"].Count, need["Logs"])
	}
}

// With the training off, or done, no knife or logs are asked; the chisel only while the cut is on; and an
// account below 41 whose Fletching is unknown asks nothing for the climb.
func TestFisherKitLinesNeedTheTrainingToBeOnAndNotDone(t *testing.T) {
	base := fisherKitInput{StallOn: false, CutOn: true, Level: 1, PackLogs: map[string]int{}, BankLogs: map[string]int{}}
	if by := linesByItem(fisherKitLines(base)); len(by) != 1 || by["Chisel"].Count != 1 {
		t.Fatalf("training off: %+v, want the chisel alone", by)
	}
	done := base
	done.TrainOn, done.Level, done.XP = true, 41, levels.XPAtLevel(41)
	if by := linesByItem(fisherKitLines(done)); len(by) != 1 || by["Chisel"].Count != 1 {
		t.Fatalf("training done: %+v, want the chisel alone", by)
	}
	noCut := done
	noCut.CutOn = false
	if lines := fisherKitLines(noCut); len(lines) != 0 {
		t.Fatalf("no cut and the climb done: %+v", lines)
	}
	unknown := base
	unknown.TrainOn, unknown.Level = true, 0
	if by := linesByItem(fisherKitLines(unknown)); by["Knife"].Count != 0 || by["Logs"].Count != 0 {
		t.Fatalf("Fletching unknown asked for the climb: %+v", by)
	}
}

// The kit is the lines judged against the mule's published bank: nothing it lacks, the logs as bank notes
// and at most half what it holds of a kind, and never above an order's limit.
func TestFisherKitPlanJudgesTheLinesAgainstTheMule(t *testing.T) {
	in := fisherKitInput{StallOn: true, TrainOn: true, CutOn: true, Level: 1, PackLogs: map[string]int{}, BankLogs: map[string]int{}}
	in.MuleStock = stockOf(map[string]int{"Games necklace(8)": 50, "Knife": 229, "Logs": 1200, "Oak logs": 54582, "Willow logs": 138203, "Chisel": 0})
	kit, none := fisherKitPlan(in)
	got := map[string]orderplan.KitLine{}
	for _, l := range kit {
		got[l.Item] = l
	}
	if got["Games necklace(8)"].Qty != 1 || got["Knife"].Qty != 1 || got["Knife"].Noted || got["Games necklace(8)"].Noted {
		t.Fatalf("kit %+v", kit)
	}
	if _, asked := got["Chisel"]; asked || !strings.Contains(strings.Join(none, ","), "Chisel") {
		t.Fatalf("a chisel the mule holds none of was asked for (kit %+v, none %v)", kit, none)
	}
	want := fisherplan.FletchAsk(0, func(string) int { return 0 })
	if l := got["Logs"]; !l.Noted || l.Qty != 600 {
		t.Fatalf("plain logs %+v: the mule holds 1,200, so half of it (600) of the %d wanted, as notes", l, want["Logs"])
	}
	for _, name := range []string{"Oak logs", "Willow logs"} {
		l := got[name]
		if !l.Noted || l.Qty != want[name] || l.Qty > orderplan.OpenMaxPerOrder {
			t.Errorf("%s %+v, want %d noted (under the order limit)", name, l, want[name])
		}
	}
	// An unpublished bank asks nothing, blind.
	in.MuleStock = nil
	if kit, _ := fisherKitPlan(in); kit != nil {
		t.Fatalf("an unpublished mule bank was asked for %+v", kit)
	}
}

// With eatFood on, an account holding no food asks the mule for foodCount bites un-noted, of the first food on the
// ladder the mule holds; one holding food in the pack or the bank asks none, and eatFood off asks none.
func TestFisherKitAsksFoodOnlyWhenNoneIsHeld(t *testing.T) {
	in := fisherKitInput{FoodOn: true, FoodWant: 3, FoodChoices: fisherFoodLadder, PackLogs: map[string]int{}, BankLogs: map[string]int{}}
	in.MuleStock = stockOf(map[string]int{"Bass": 1240000, "Cake": 8})
	kit, _ := fisherKitPlan(in)
	if len(kit) != 1 || kit[0].Item != "Bass" || kit[0].Qty != 3 || kit[0].Noted {
		t.Fatalf("kit %+v, want 3 un-noted Bass (the first rung of the ladder the mule holds)", kit)
	}
	for _, held := range []fisherKitInput{
		{FoodOn: true, FoodWant: 3, FoodChoices: fisherFoodLadder, PackFood: 1},
		{FoodOn: true, FoodWant: 3, FoodChoices: fisherFoodLadder, BankFood: 40},
		{FoodOn: false, FoodWant: 3, FoodChoices: fisherFoodLadder},
		{FoodOn: true, FoodWant: 0, FoodChoices: fisherFoodLadder},
	} {
		if l, ok := foodLine(held); ok {
			t.Errorf("%+v asked %+v", held, l)
		}
	}
}

// A hand-off never gives the mule the staller's jewellery, and for BassOysterCaskets the chisel, the knife
// and, while the climb is on, the logs.
func TestKitKeepNames(t *testing.T) {
	pf := &fisherBot{stall: &fisherrunner.Stall{On: true}}
	if names := pf.kitKeepNames(); len(names) != 16 {
		t.Fatalf("ProgressiveFisher keeps %v, want the sixteen rub names", names)
	}
	b := &fisherBot{bassOyster: true, stall: &fisherrunner.Stall{On: true}, dropCycle: &fisherrunner.Cycle{Opts: fisherplan.DefaultCycleOptions()}}
	b.fletch.on = true
	names := strings.Join(b.kitKeepNames(), ",")
	for _, want := range []string{"Games necklace(8)", "Chisel", "Knife", "Logs", "Oak logs", "Willow logs"} {
		if !strings.Contains(names, want) {
			t.Errorf("BassOysterCaskets with the climb on does not keep %q (%s)", want, names)
		}
	}
	b.fletch.on = false
	if names := strings.Join(b.kitKeepNames(), ","); strings.Contains(names, "Oak logs") || !strings.Contains(names, "Chisel") {
		t.Errorf("the climb off: keeps %s, want the chisel but no logs", names)
	}
	b.dropCycle.Opts.CutPearls = false
	if names := strings.Join(b.kitKeepNames(), ","); strings.Contains(names, "Chisel") {
		t.Errorf("the cut off: still keeps the chisel (%s)", names)
	}
}

// The training is on only for BassOysterCaskets with the cut on, and gives itself up after three failed rounds.
func TestFletchOnAndFailureBackoff(t *testing.T) {
	b := &fisherBot{bassOyster: true, dropCycle: &fisherrunner.Cycle{Opts: fisherplan.DefaultCycleOptions()}}
	b.fletch.on = true
	if !b.fletchOn() {
		t.Fatal("the climb is not on with fletchTo41 and the cut on")
	}
	if (&fisherBot{dropCycle: b.dropCycle, fletch: fletchTrain{on: true}}).fletchOn() {
		t.Fatal("ProgressiveFisher trains Fletching")
	}
	b.dropCycle.Opts.CutPearls = false
	if b.fletchOn() {
		t.Fatal("the climb is on with the cut it is for off")
	}
	b.dropCycle.Opts.CutPearls = true
	ctx := &Context{}
	b.fletchFailed(ctx, "a round")
	b.fletchFailed(ctx, "a round")
	if !b.fletchOn() || !b.fletch.retryAt.After(time.Now()) {
		t.Fatal("two failures should back off but keep the training on")
	}
	b.fletchFailed(ctx, "a round")
	if b.fletchOn() || !b.fletch.off {
		t.Fatal("three failures in a row did not switch the training off for the run")
	}
}

// fletchRig is a fisher at its bank: a bank-and-pack simulator over the real item config, with the Big
// fishing net as its tool.
type fletchRig struct {
	b    *fisherBot
	d    *fisherrunner.Deps
	sim  *actionsim.Session
	bank *actions.Bank
	s    fisherplan.FishSnapshot
	logs []string
}

func newFletchRig(t *testing.T, level int, pack, bank []actionsim.Stack) *fletchRig {
	t.Helper()
	cfg := chestCfg(t)
	r := &fletchRig{}
	r.sim = actionsim.New(cfgObjs(cfg), pack)
	r.sim.OpenBank(bank)
	r.sim.State().Handle(serverprot.UPDATE_STAT, []byte{byte(fisherrunner.FletchingStat), byte(levels.XPAtLevel(level) >> 24), byte(levels.XPAtLevel(level) >> 16), byte(levels.XPAtLevel(level) >> 8), byte(levels.XPAtLevel(level)), byte(level)})
	r.b = &fisherBot{bassOyster: true, dropCycle: &fisherrunner.Cycle{Opts: fisherplan.DefaultCycleOptions()}}
	r.b.fletch.on = true
	r.b.stall = &fisherrunner.Stall{On: false}
	r.d = &fisherrunner.Deps{
		Session: r.sim, Config: cfg, Components: actionsim.Components(), Cycle: r.b.dropCycle, Junk: fisherplan.IsBassOysterJunk,
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
		Log:   func(s string) { r.logs = append(r.logs, s) },
	}
	r.bank = actions.NewBank(r.sim, r.d.Components)
	r.s = fisherplan.FishSnapshot{Level: 90, Target: fisherplan.BassOysterTarget(), HeldTools: []string{"Big fishing net"}}
	return r
}

func countOf(rows []actionsim.Stack, id int32) int {
	n := 0
	for _, r := range rows {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

// The bank half of a training round: the net and the knife stay, everything else goes in (the products of
// the last load, the notes the mule handed over), and the best logs the level cuts come out as items, as
// many as the pack has room for. At Fletching 20 that is the oak logs, not the plain ones.
func TestFletchLoadBanksTheRestAndDrawsTheBestLogsRealConfig(t *testing.T) {
	cfg := chestCfg(t)
	id := func(name string) int32 { return itemID(t, cfg, name, false) }
	note := func(name string) int32 { return itemID(t, cfg, name, true) }
	pack := []actionsim.Stack{
		{ID: id("Big fishing net"), Count: 1}, {ID: id("Knife"), Count: 1}, {ID: id("Raw bass"), Count: 1},
		{ID: id("Oak shortbow"), Count: 1}, {ID: note("Willow logs"), Count: 400},
	}
	bank := []actionsim.Stack{{ID: id("Logs"), Count: 90}, {ID: id("Oak logs"), Count: 90}, {ID: id("Willow logs"), Count: 90}}
	r := newFletchRig(t, 20, pack, bank)
	choice, why := r.b.fletchLoad(&Context{}, r.d, r.bank, r.s, 20)
	if why != "" {
		t.Fatalf("no load: %s (pack %+v bank %+v; log %q)", why, r.sim.Pack(), r.sim.Bank(), r.logs)
	}
	if choice.LogsName != "Oak logs" || choice.Product.MakeMatch != "shortbow" {
		t.Fatalf("at Fletching 20 the choice is %s -> %s, want the oak shortbow", choice.LogsName, choice.Product.ItemName)
	}
	p := r.sim.Pack()
	if countOf(p, id("Big fishing net")) != 1 || countOf(p, id("Knife")) != 1 {
		t.Fatalf("the net or the knife left the pack: %+v", p)
	}
	if countOf(p, id("Raw bass")) != 0 || countOf(p, id("Oak shortbow")) != 0 || countOf(p, note("Willow logs")) != 0 {
		t.Fatalf("what was not kept stayed in the pack: %+v", p)
	}
	if countOf(p, id("Oak logs")) != 26 {
		t.Fatalf("%d oak logs drawn, want the 26 slots the pack has free: %+v", countOf(p, id("Oak logs")), p)
	}
	if countOf(r.sim.Bank(), id("Willow logs")) != 490 || countOf(r.sim.Bank(), id("Oak shortbow")) != 1 || countOf(r.sim.Bank(), id("Raw bass")) != 1 {
		t.Fatalf("the notes and the products did not reach the bank: %+v", r.sim.Bank())
	}
	if r.b.bankRead == nil || r.b.bankRead.Logs["Willow logs"] != 490 {
		t.Fatalf("the bank reading was not refreshed: %+v", r.b.bankRead)
	}
}

// With no knife in the pack, one is drawn from the bank; with none anywhere, or no logs the level can cut,
// no load is drawn and the reason says why.
func TestFletchLoadNeedsAKnifeAndLogsTheLevelCutsRealConfig(t *testing.T) {
	cfg := chestCfg(t)
	id := func(name string) int32 { return itemID(t, cfg, name, false) }
	net := actionsim.Stack{ID: id("Big fishing net"), Count: 1}

	r := newFletchRig(t, 1, []actionsim.Stack{net}, []actionsim.Stack{{ID: id("Knife"), Count: 3}, {ID: id("Logs"), Count: 90}})
	if choice, why := r.b.fletchLoad(&Context{}, r.d, r.bank, r.s, 1); why != "" || choice.Product.MakeMatch != "shaft" {
		t.Fatalf("a knife in the bank only: %q (%s)", why, choice.Product.ItemName)
	}
	if countOf(r.sim.Pack(), id("Knife")) != 1 || countOf(r.sim.Pack(), id("Logs")) != 26 {
		t.Fatalf("the knife was not drawn or the plain logs not loaded: %+v", r.sim.Pack())
	}

	r = newFletchRig(t, 1, []actionsim.Stack{net}, []actionsim.Stack{{ID: id("Logs"), Count: 90}})
	if _, why := r.b.fletchLoad(&Context{}, r.d, r.bank, r.s, 1); !strings.Contains(why, "knife") {
		t.Fatalf("no knife anywhere: %q", why)
	}

	r = newFletchRig(t, 5, []actionsim.Stack{net, {ID: id("Knife"), Count: 1}}, []actionsim.Stack{{ID: id("Willow logs"), Count: 90}})
	if _, why := r.b.fletchLoad(&Context{}, r.d, r.bank, r.s, 5); !strings.Contains(why, "no logs") {
		t.Fatalf("only willow logs at Fletching 5: %q", why)
	}
}

// The step itself decides nothing to do for an account with nothing to train with, or at the goal with a
// clean pack, and stays off a ProgressiveFisher.
func TestFletchTrainStepStaysOutOfTheWayRealConfig(t *testing.T) {
	cfg := chestCfg(t)
	id := func(name string) int32 { return itemID(t, cfg, name, false) }
	net := actionsim.Stack{ID: id("Big fishing net"), Count: 1}
	r := newFletchRig(t, 1, []actionsim.Stack{net}, nil)
	ctx := &Context{Session: r.sim}
	if r.b.fletchTrainStep(ctx, r.d, r.s) {
		t.Fatal("trained with no bank reading and no logs")
	}
	r.b.bankRead = &fisherrunner.BankRead{Logs: map[string]int{"Logs": 90}}
	if r.b.fletchTrainStep(ctx, r.d, r.s) {
		t.Fatal("trained with logs and no knife anywhere")
	}
	r.b.fletch.off = true
	r.b.bankRead.Knife = 1
	if r.b.fletchTrainStep(ctx, r.d, r.s) {
		t.Fatal("trained after the training gave itself up")
	}
	r = newFletchRig(t, 41, []actionsim.Stack{net}, nil)
	r.b.bankRead = &fisherrunner.BankRead{Logs: map[string]int{"Logs": 90}, Knife: 1}
	if r.b.fletchTrainStep(&Context{Session: r.sim}, r.d, r.s) {
		t.Fatal("trained at the goal")
	}
	pf := &fisherBot{}
	if pf.fletchTrainStep(ctx, r.d, r.s) {
		t.Fatal("ProgressiveFisher trained")
	}
}

// A choice ladder of sixteen rungs ("Games necklace(8)/.../Ring of dueling(1)") would fill a log line: the
// kit's "holds none" line shows its first and last, and leaves a single name as it is.
func TestShortNamesCutsALongChoiceLadder(t *testing.T) {
	line, _ := fleetplan.RubAskLine(false)
	ladder := strings.Join(line.Choices, "/")
	if got := shortNames([]string{"Knife", ladder}); !strings.HasPrefix(got, "Knife, ") || strings.Count(got, "/") != 2 || !strings.Contains(got, "/.../") {
		t.Fatalf("shortNames = %q, want the knife and the ladder cut to first/.../last", got)
	}
	if got := shortNames([]string{"Knife", "Chisel"}); got != "Knife, Chisel" {
		t.Fatalf("shortNames of two plain names = %q", got)
	}
	if got := shortNames(nil); got != "" {
		t.Fatalf("shortNames of nothing = %q", got)
	}
}
