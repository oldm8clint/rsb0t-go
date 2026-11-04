package bot

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/chopmanipplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/fisherrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/scripts/wcfletchrunner"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// BASSOYSTERCASKETS: FLETCHING TO 41 AND THE KIT FOR CUTTING PEARLS (a user request: "they could also
// include bringing a chisel with them and getting 41 fletching from provided logs from the mule and chisel
// the oysters while fishing to just be banking the bolt tips from the oysters saving even more inventory
// space, maximizing exp per hour" and "should use logs, oak logs, willow logs, making arrow shafts with
// logs, and unstrung bows with the rest").
//
// The cut itself is the drop cycle's (fisherrunner/cycle.go: a chisel on every Oyster pearl once Fletching
// is 41). This file gets an account there, in two parts:
//
//   - THE KIT, asked of the mule on the hand-off the account is making anyway (handoffKit): one bundled
//     kit order, one trade, of only what the mule's published bank holds and the account is short of: the
//     run staller's jewellery, a knife and the logs of the climb (as bank notes), and a chisel. Nothing is
//     asked blind, and the account's own bank is counted first. A chisel the mule has none of is bought from
//     Arhein's store at Catherby, 11 tiles from the mule's post, for a coin or two.
//   - THE TRAINING (fletchTrainStep): while Fletching is under 41 and the bank (or the pack) holds logs and
//     a knife, one round per Loop call at the account's own bank: bank everything but the tool, draw a
//     load of the best logs the level cuts (fisherplan.ChooseFletch: arrow shafts from plain logs, the
//     best unstrung bow from oak and willow), close the bank and cut the load with the make menu
//     (wcfletchrunner.Fletch, which ProgressiveWCFletch's accounts run all day). A round that makes
//     nothing backs off, and three in a row switch the training off for the run: it is never a loop that
//     holds an account away from the fish.
//
// Settings (each defaults on): fletchTo41 (this file), cutPearls (the cycle's own, bassoyster.go). The
// training needs both: without the cut there is nothing to train for.

// fletchTrain is one account's training state.
type fletchTrain struct {
	// on is the fletchTo41 setting.
	on bool
	// off is set for the rest of the run after fletchMaxFails rounds in a row made nothing.
	off     bool
	fails   int
	retryAt time.Time
	// dirty: a round left products or logs in the pack that the bank has not seen.
	dirty bool

	rounds, logsCut, productsMade int
	startedAt                     time.Time
	startXP                       int
	said                          map[string]time.Time
}

// fletchMaxFails is how many rounds in a row may make nothing before the training stops for the run.
const fletchMaxFails = 3

// fletchRetryBase is the wait after a failed round, times the failures so far.
const fletchRetryBase = 30 * time.Second

// kitAskEvery is how long after a kit ask, answered or not, the account leaves the mule alone about it.
const kitAskEvery = 30 * time.Minute

// say logs line at most once an interval per key.
func (f *fletchTrain) say(key string, every time.Duration, line func() string, log func(string, ...any)) {
	if f.said == nil {
		f.said = map[string]time.Time{}
	}
	if at, ok := f.said[key]; ok && time.Since(at) < every {
		return
	}
	f.said[key] = time.Now()
	log("%s", line())
}

// fletchOn reports whether the training is switched on and not given up on: BassOysterCaskets, the
// setting, the cut it is for.
func (b *fisherBot) fletchOn() bool {
	return b.bassOyster && b.dropCycle != nil && b.dropCycle.Opts.CutPearls && b.fletch.on && !b.fletch.off
}

// toolKeep is what stays in the pack at a bank visit that is not a trip's own: the tool being fished with
// and the food, as bankStep keeps them.
func (b *fisherBot) toolKeep(d *fisherrunner.Deps, s fisherplan.FishSnapshot) []string {
	rung := fisherplan.CurrentRung(s.Level, s.Target)
	owned := append(append([]string{}, s.HeldTools...), s.BankTools...)
	keepRungs := []*fisherplan.Rung{&rung}
	if active, ok := fisherplan.ActiveRung(s.Level, s.Target, owned); ok {
		a := active
		keepRungs = append(keepRungs, &a)
	}
	return fisherBankKeep(fisherplan.BankKeep(false, keepRungs...), b.surv, fisherrunner.EdibleFood(d))
}

// fletchStock is the logs the training can use by display name: the last bank reading's and the pack's
// (a noted stack counts: the round banks it first).
func (b *fisherBot) fletchStock(d *fisherrunner.Deps) func(logs string) int {
	return func(name string) int {
		n := fisherrunner.PackCountByName(d, name)
		if b.bankRead != nil {
			n += b.bankRead.Logs[name]
		}
		return n
	}
}

// woodDeps is the fisher's session as woodcutrunner.Deps, for wcfletchrunner.Fletch.
func (b *fisherBot) woodDeps(ctx *Context, d *fisherrunner.Deps) *woodcutrunner.Deps {
	return &woodcutrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config, Components: ctx.Components,
		Chat: ctx.Chat, Delay: woodcutrunner.Delayer(d.Delay), Log: ctx.Log, Loot: ctx.Loot, Walker: b.walker,
	}
}

// fletchTrainStep runs one Fletching training round at a Loop boundary when there is something to train
// with, and reports whether it took this Loop call. Only while the account is fishing (PhaseFish): the
// caller checks.
func (b *fisherBot) fletchTrainStep(ctx *Context, d *fisherrunner.Deps, s fisherplan.FishSnapshot) bool {
	if !b.fletchOn() || time.Now().Before(b.fletch.retryAt) {
		return false
	}
	if s.MainModal != gamestate.NoModal || randomevents.Busy(ctx.Session) {
		return false
	}
	level := fisherrunner.FletchingLevel(d)
	if level == 0 {
		return false // the server has not told us yet
	}
	if fisherplan.FletchDone(level) {
		return b.fletch.dirty && b.fletchCleanUp(ctx, d, s, level)
	}
	choice, ok := fisherplan.ChooseFletch(level, b.fletchStock(d))
	if !ok {
		return b.fletch.dirty && b.fletchCleanUp(ctx, d, s, level)
	}
	if !fisherrunner.KnifeHeld(d) && (b.bankRead == nil || b.bankRead.Knife == 0) {
		b.fletch.say("no-knife", 10*time.Minute, func() string {
			return fmt.Sprintf("fisher: Fletching %d with %s ready to cut, and no knife in the pack or the bank: the hand-off kit asks the mule for one", level, choice.LogsName)
		}, ctx.logf)
		return false
	}
	return b.fletchRound(ctx, d, s, level)
}

// fletchFailed backs off a round that made nothing, and gives the training up for the run after
// fletchMaxFails in a row.
func (b *fisherBot) fletchFailed(ctx *Context, why string) {
	b.fletch.fails++
	if b.fletch.fails >= fletchMaxFails {
		b.fletch.off = true
		ctx.logf("fisher: fletching training: %s — %d rounds in a row made nothing, giving the training up for this run (fletchTo41 off); pearls stay pearls below Fletching 41", why, b.fletch.fails)
		return
	}
	wait := time.Duration(b.fletch.fails) * fletchRetryBase
	b.fletch.retryAt = time.Now().Add(wait)
	ctx.logf("fisher: fletching training: %s — trying again in %s (%d of %d)", why, wait, b.fletch.fails, fletchMaxFails)
}

// fletchLoad is the bank half of a round, on an open bank: everything but the tool and the knife goes in
// (the last load's products, and any logs the mule handed over as notes), then a load of the best logs the
// level cuts is drawn as items, as many as the pack has room for. It returns the choice cut, or why no load
// could be drawn.
func (b *fisherBot) fletchLoad(ctx *Context, d *fisherrunner.Deps, bank *actions.Bank, s fisherplan.FishSnapshot, level int) (fisherplan.FletchChoice, string) {
	fisherrunner.DepositExcept(d, bank, append(b.toolKeep(d, s), fisherplan.KnifeName))
	read := fisherrunner.ReadBank(d, bank)
	b.bankRead = &read
	b.fletch.dirty = false
	choice, ok := fisherplan.ChooseFletch(level, func(name string) int { return read.Logs[name] })
	if !ok {
		return choice, "the bank holds no logs the level can cut"
	}
	if !fisherrunner.KnifeHeld(d) && !fisherrunner.WithdrawOneOf(d, bank, fisherplan.KnifeName) {
		return choice, "no knife could be drawn"
	}
	n := min(fisherrunner.FreeSlots(d), read.Logs[choice.LogsName])
	if n < 1 {
		return choice, "no room in the pack for logs"
	}
	if err := fisherrunner.SetNoteMode(d, bank, false); err != nil {
		ctx.logf("fisher: fletching training: could not switch the bank to items: %v", err)
	}
	if err := fisherrunner.WithdrawXByName(d, bank, choice.LogsName, n); err != nil {
		return choice, fmt.Sprintf("withdrawing %d %s: %v", n, strings.ToLower(choice.LogsName), err)
	}
	return choice, ""
}

// fletchRound is one round: bank, draw a load, cut it.
func (b *fisherBot) fletchRound(ctx *Context, d *fisherrunner.Deps, s fisherplan.FishSnapshot, level int) bool {
	bank, ok := fisherrunner.OpenCatherbyBank(d, b.layout)
	if !ok {
		b.fletchFailed(ctx, "could not reach or open the bank")
		return true
	}
	closed := false
	closeBank := func() {
		if !closed {
			closed = true
			_ = bank.Close()
		}
	}
	defer closeBank()
	if b.fletch.rounds == 0 {
		b.fletch.startedAt = time.Now()
		if sk, ok := ctx.Session.State().Skill(fisherrunner.FletchingStat); ok {
			b.fletch.startXP = sk.XP
		}
	}
	choice, why := b.fletchLoad(ctx, d, bank, s, level)
	closeBank()
	if why != "" {
		if choice.LogsName == "" {
			return true // nothing the level can cut in the bank after all: the next step re-decides
		}
		b.fletchFailed(ctx, why)
		return true
	}
	if !fisherrunner.WaitForGuardian(d) {
		return true
	}
	res := wcfletchrunner.Fletch(b.woodDeps(ctx, d), choice.LogsName, choice.Product, 0)
	b.fletch.dirty = true // whatever the round left is in the pack until the next bank visit
	if res.Reason == "stopped" {
		return true
	}
	if !res.Made {
		b.fletchFailed(ctx, fmt.Sprintf("cutting %s into %s made nothing (%s)", strings.ToLower(choice.LogsName), strings.ToLower(choice.Product.ItemName), res.Reason))
		return true
	}
	b.fletch.fails = 0
	b.fletch.rounds++
	b.fletch.logsCut += res.Logs
	b.fletch.productsMade += res.Products
	if sk, ok := ctx.Session.State().Skill(fisherrunner.FletchingStat); ok {
		ctx.logf("fisher: fletching training: round %d cut %d %s into %d %s — Fletching %d, %d xp gained since the first round (goal %d)",
			b.fletch.rounds, res.Logs, strings.ToLower(choice.LogsName), res.Products, strings.ToLower(choice.Product.ItemName),
			sk.Level, sk.XP-b.fletch.startXP, fisherplan.FletchGoal)
	}
	return true
}

// fletchCleanUp banks what the last round left in the pack once the training cannot go on (the level is
// reached or the logs are gone), so the account fishes with a clean pack and does not spend a bank trip on
// it.
func (b *fisherBot) fletchCleanUp(ctx *Context, d *fisherrunner.Deps, s fisherplan.FishSnapshot, level int) bool {
	bank, ok := fisherrunner.OpenCatherbyBank(d, b.layout)
	if !ok {
		b.fletch.retryAt = time.Now().Add(20 * time.Second)
		return true
	}
	defer bank.Close()
	fisherrunner.DepositExcept(d, bank, b.toolKeep(d, s))
	fisherrunner.DrawManipKit(d, bank) // the deposit banked the kit's logs with the rest
	read := fisherrunner.ReadBank(d, bank)
	b.bankRead = &read
	b.fletch.dirty = false
	if fisherplan.FletchDone(level) {
		ctx.logf("fisher: fletching training done at Fletching %d after %d round(s): %d logs cut into %d products, %d xp — pearls are cut into tips from now on (a chisel: %v in the pack, %d banked)",
			level, b.fletch.rounds, b.fletch.logsCut, b.fletch.productsMade, b.xpSinceTraining(ctx), fisherrunner.ChiselHeld(d), read.Chisel)
	}
	return true
}

func (b *fisherBot) xpSinceTraining(ctx *Context) int {
	if sk, ok := ctx.Session.State().Skill(fisherrunner.FletchingStat); ok && b.fletch.rounds > 0 {
		return sk.XP - b.fletch.startXP
	}
	return 0
}

// ---- the kit, asked on the hand-off ----

// kitKeepNames is the names a hand-off must never give the mule: the run staller's jewellery, and for
// BassOysterCaskets the chisel and knife of the pearl cut and the logs of the climb while it is on.
func (b *fisherBot) kitKeepNames() []string {
	keep := b.stallKeepNames()
	if b.manipOn {
		keep = append(keep, fisherplan.KnifeName, chopmanipplan.PlainLogs) // the fishing tick manipulation's kit
	}
	if b.bassOyster && b.dropCycle != nil && b.dropCycle.Opts.CutPearls {
		keep = append(keep, fisherplan.Chisel, fisherplan.KnifeName)
		if b.fletchOn() {
			keep = append(keep, fisherplan.FletchLogNames()...)
		}
	}
	return keep
}

// fisherKitInput is what the kit is decided from.
type fisherKitInput struct {
	StallOn                bool
	PackRub, BankRub       int
	TrainOn                bool // fletchTo41 and the cut it is for
	CutOn                  bool // the cut is on at all
	Level, XP              int
	PackKnife, BankKnife   int
	PackChisel, BankChisel int
	PackLogs, BankLogs     map[string]int
	MuleStock              func(item string) (int, bool)
	MuleKeepsBack          int // the divisor of the mule's stock one account may take of a log kind (2: half)
	// ManipOn is the fishing tick manipulation (fishermanip.go): it needs a knife and a few plain logs, in the pack or the bank.
	// PackManipLogs is the plain, un-noted logs in the pack (the bank's are BankLogs["Logs"]).
	ManipOn       bool
	PackManipLogs int
	// FoodOn is eatFood: the account eats when hit. FoodWant is the bites a bank trip leaves in the pack (foodCount),
	// FoodChoices the food ladder asked for, best first, and PackFood and BankFood the bites held.
	FoodOn             bool
	FoodWant           int
	FoodChoices        []string
	PackFood, BankFood int
}

// fisherFoodLadder is the food asked of the mule when the account allows any food: the biggest bites first.
var fisherFoodLadder = []string{"Manta ray", "Sea turtle", "Shark", "Swordfish", "Bass", "Lobster", "Tuna"}

// foodLine is the food the account is short of, un-noted so the bank deposit can bank it and the next bank visit
// draws it: foodCount bites less what the pack and the bank hold, asked only when both hold none. A fisher catches
// raw fish and cooks none, so without this an account with eatFood on holds no food at all (19 Oct 2026: a River
// troll killed a 30 HP fisher at the Fishing Guild 4 seconds into its evade, with nothing in the pack to eat).
func foodLine(in fisherKitInput) (fleetplan.AskLine, bool) {
	if !in.FoodOn || in.FoodWant <= 0 || in.PackFood+in.BankFood > 0 || len(in.FoodChoices) == 0 {
		return fleetplan.AskLine{}, false
	}
	return fleetplan.AskLine{Choices: append([]string{}, in.FoodChoices...), Count: in.FoodWant, Rider: true}, true
}

// logsHeld is the logs of a kind the account holds, banked and carried.
func (in fisherKitInput) logsHeld(name string) int { return in.PackLogs[name] + in.BankLogs[name] }

// fisherKitLines is the lines the mule is asked for, before they are judged against its published bank:
// the run staller's jewellery when the pack and the bank hold none, a knife and the logs of the climb
// while the training has work left, and a chisel when the cut is on and none is held anywhere.
func fisherKitLines(in fisherKitInput) []fleetplan.AskLine {
	var lines []fleetplan.AskLine
	if in.StallOn && in.PackRub+in.BankRub == 0 {
		if l, ok := fleetplan.RubAskLine(false); ok {
			lines = append(lines, l)
		}
	}
	training := in.TrainOn && !fisherplan.FletchDone(in.Level) && in.Level > 0
	if (training || in.ManipOn) && in.PackKnife+in.BankKnife == 0 {
		lines = append(lines, fleetplan.AskLine{Choices: []string{fisherplan.KnifeName}, Count: 1, Rider: true})
	}
	if training {
		ask := fisherplan.FletchAsk(in.XP, in.logsHeld)
		for _, name := range fisherplan.FletchLogNames() {
			if n := ask[name]; n > 0 {
				lines = append(lines, fleetplan.AskLine{Choices: []string{name}, Count: min(n, orderplan.OpenMaxPerOrder), Rider: true})
			}
		}
	}
	if in.CutOn && in.PackChisel+in.BankChisel == 0 {
		lines = append(lines, fleetplan.AskLine{Choices: []string{fisherplan.Chisel}, Count: 1, Rider: true})
	}
	if l, ok := foodLine(in); ok {
		lines = append(lines, l)
	}
	return lines
}

// manipLogsLine is the plain logs the tick manipulation wants, before they are judged against the mule's bank: asked un-noted (a
// noted log cannot be used on a knife), up to manipLogsWanted counting the pack and the bank.
func manipLogsLine(in fisherKitInput) (fleetplan.AskLine, bool) {
	have := in.PackManipLogs + in.BankLogs[chopmanipplan.PlainLogs]
	if !in.ManipOn || have >= manipLogsWanted {
		return fleetplan.AskLine{}, false
	}
	return fleetplan.AskLine{Choices: []string{chopmanipplan.PlainLogs}, Count: manipLogsWanted - have, Rider: true}, true
}

// fisherKitPlan judges the lines against the mule's published bank and shapes them as a kit: a log line
// goes as bank notes (one slot a kind), is capped at what one account may take of the mule's stock, and
// never more than a mule hands out in an order.
func fisherKitPlan(in fisherKitInput) ([]orderplan.KitLine, []string) {
	lines := fisherKitLines(in)
	manip, haveManip := manipLogsLine(in)
	if (len(lines) == 0 && !haveManip) || in.MuleStock == nil {
		return nil, nil
	}
	plan, none := fleetplan.FilterAskByBank(lines, in.MuleStock)
	div := in.MuleKeepsBack
	if div < 1 {
		div = 2
	}
	var kit []orderplan.KitLine
	if haveManip {
		// Un-noted, and a share of the mule's stock like every log line.
		mplan, mnone := fleetplan.FilterAskByBank([]fleetplan.AskLine{manip}, in.MuleStock)
		none = append(none, mnone...)
		for _, p := range mplan {
			for _, r := range p.Rungs {
				qty := r.Count
				if have, ok := in.MuleStock(r.Item); ok {
					qty = min(qty, have/div)
				}
				if qty > 0 {
					kit = append(kit, orderplan.KitLine{Item: r.Item, Qty: min(qty, orderplan.OpenMaxPerOrder)})
				}
			}
		}
	}
	for _, p := range plan {
		for _, r := range p.Rungs {
			qty, noted := r.Count, false
			if fisherplan.IsFletchLog(r.Item) {
				noted = true
				if have, ok := in.MuleStock(r.Item); ok {
					qty = min(qty, have/div)
				}
			}
			if qty > 0 {
				kit = append(kit, orderplan.KitLine{Item: r.Item, Qty: min(qty, orderplan.OpenMaxPerOrder), Noted: noted})
			}
		}
	}
	return kit, none
}

// handoffKit asks the mule for the kit in one kit order, on the hand-off that has just traded: the
// account is at the mule's post with an empty pack, so it costs one more trade and no walk.
func (b *fisherBot) handoffKit(ctx *Context, d *fisherrunner.Deps) {
	if b.mule == "" || ctx.Bus == nil || time.Now().Before(b.kitAskAt) {
		return
	}
	stock := muleBankCount(ctx, b.mule)
	if stock == nil {
		return // the mule's bank is not published, or the reading is stale: nothing to judge by
	}
	in := b.fisherKitInput(ctx, d, stock)
	kit, none := fisherKitPlan(in)
	b.kitAskAt = time.Now().Add(kitAskEvery)
	if len(kit) == 0 {
		if len(none) > 0 {
			ctx.logf("fisher: hand-off kit: %q's published bank holds none of %s", b.mule, shortNames(none))
		}
		return
	}
	b.askKit(ctx, d, in, kit, "on the hand-off")
}

// askKit sends one kit order to the mule and logs what came of it. The account must be standing at the mule's post with room in its pack.
func (b *fisherBot) askKit(ctx *Context, d *fisherrunner.Deps, in fisherKitInput, kit []orderplan.KitLine, where string) donorrunner.KitAwaitResult {
	var parts []string
	for _, l := range kit {
		form := ""
		if l.Noted {
			form = " noted"
		}
		parts = append(parts, fmt.Sprintf("%d%s %s", l.Qty, form, l.Item))
	}
	ctx.logf("fisher: hand-off kit: asking %q for %s, %s (%s)", b.mule, strings.Join(parts, ", "), where, b.kitWhy(in))
	res := donorrunner.AwaitKit(&donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}, donorrunner.KitAsk{Mule: b.mule, Me: ctx.Username, Lines: kit, Bus: ctx.Bus})
	switch {
	case res.Stopped:
	case len(res.Got) == 0:
		ctx.logf("fisher: hand-off kit: %q handed over nothing (%s; declined %v)", b.mule, res.Reason, res.Declined)
	default:
		ctx.logf("fisher: hand-off kit: %q handed over %v (noted %v), declined %v", b.mule, res.Got, res.Noted, res.Declined)
	}
	return res
}

// shortNames joins names for a log line, a choice ladder of sixteen rungs ("Games necklace(8)/.../Ring of
// dueling(1)") cut to its first and last.
func shortNames(names []string) string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if parts := strings.Split(n, "/"); len(parts) > 2 {
			n = parts[0] + "/.../" + parts[len(parts)-1]
		}
		out = append(out, n)
	}
	return strings.Join(out, ", ")
}

// kitWhy says what the kit is for, for the log.
func (b *fisherBot) kitWhy(in fisherKitInput) string {
	var why []string
	if in.StallOn && in.PackRub+in.BankRub == 0 {
		why = append(why, "run staller jewellery")
	}
	if in.TrainOn && !fisherplan.FletchDone(in.Level) {
		why = append(why, fmt.Sprintf("Fletching %d towards %d", in.Level, fisherplan.FletchGoal))
	}
	if in.CutOn && in.PackChisel+in.BankChisel == 0 {
		why = append(why, "a chisel for the pearls")
	}
	if in.ManipOn && (in.PackKnife+in.BankKnife == 0 || in.PackManipLogs+in.BankLogs[chopmanipplan.PlainLogs] < manipLogsWanted) {
		why = append(why, "a knife and plain logs for the fishing tick manipulation")
	}
	if _, ok := foodLine(in); ok {
		why = append(why, "food to eat when hit")
	}
	return strings.Join(why, ", ")
}

// fisherKitInput reads the input from the live pack and the last bank reading.
func (b *fisherBot) fisherKitInput(ctx *Context, d *fisherrunner.Deps, stock func(string) (int, bool)) fisherKitInput {
	in := fisherKitInput{
		StallOn:   b.stallOn(),
		ManipOn:   b.manipOn,
		TrainOn:   b.fletchOn(),
		CutOn:     b.bassOyster && b.dropCycle != nil && b.dropCycle.Opts.CutPearls,
		Level:     fisherrunner.FletchingLevel(d),
		PackLogs:  map[string]int{},
		BankLogs:  map[string]int{},
		MuleStock: stock,
	}
	if sk, ok := ctx.Session.State().Skill(fisherrunner.FletchingStat); ok {
		in.XP = sk.XP
	}
	if fisherrunner.RubItemHeld(d) {
		in.PackRub = 1
	}
	if fisherrunner.KnifeHeld(d) {
		in.PackKnife = 1
	}
	if fisherrunner.ChiselHeld(d) {
		in.PackChisel = 1
	}
	for _, name := range fisherplan.FletchLogNames() {
		in.PackLogs[name] = fisherrunner.PackCountByName(d, name)
	}
	in.PackManipLogs = fisherrunner.ManipLogsHeld(d)
	if b.surv.Enabled {
		in.FoodOn, in.FoodWant = true, b.surv.FoodCount
		in.FoodChoices = b.surv.Foods
		if len(in.FoodChoices) == 0 {
			in.FoodChoices = fisherFoodLadder
		}
		in.PackFood = b.surv.FoodHeld(fisherrunner.EdibleFood(d))
	}
	if r := b.bankRead; r != nil {
		in.BankFood = r.Food
		in.BankRub, in.BankKnife, in.BankChisel = r.Rub, r.Knife, r.Chisel
		for name, n := range r.Logs {
			in.BankLogs[name] = n
		}
	}
	return in
}

// buyChisel buys one chisel from Arhein's store at Catherby when the cut is on and none is held in the
// pack or the bank (and the kit did not bring one): a coin or two, once. It runs at the mule's post, which
// is 11 tiles from the shop. It is tried again at most every kitAskEvery.
func (b *fisherBot) buyChisel(ctx *Context, d *fisherrunner.Deps) {
	if !b.bassOyster || b.dropCycle == nil || !b.dropCycle.Opts.CutPearls || time.Now().Before(b.chiselBuyAt) {
		return
	}
	if fisherrunner.ChiselHeld(d) || (b.bankRead != nil && b.bankRead.Chisel > 0) {
		return
	}
	level := fisherrunner.FletchingLevel(d)
	if level == 0 || (fisherplan.FletchDone(level) == false && !b.fletch.on) {
		return // nothing to cut with it: not below 41 with the training off
	}
	b.chiselBuyAt = time.Now().Add(kitAskEvery)
	res := fisherrunner.BuyFrom(d, fisherplan.Arhein, fisherplan.Catherby().ArheinStand, fisherplan.Chisel)
	switch {
	case res.Bought != "":
		ctx.logf("fisher: pearl tips: bought a chisel from Arhein's store (Fletching %d)", level)
	case res.OutOfStock:
		ctx.logf("fisher: pearl tips: Arhein has no chisel in stock — trying again later")
	case !res.Opened:
		ctx.logf("fisher: pearl tips: could not open Arhein's shop for a chisel — trying again later")
	default:
		ctx.logf("fisher: pearl tips: Arhein's chisel did not arrive — trying again later")
	}
}
