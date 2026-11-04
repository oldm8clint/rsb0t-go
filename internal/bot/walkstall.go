package bot

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/walkstall"
	"rsb0t-go/internal/webwalk"
)

// The shared run staller on a skilling script's walks (skillmanip-1007; walkstall/walkstall.go has the engine facts).
//
// a user request: tick manipulation and run stalling "across all scripts: maximum XP an hour, no time wasted walking". The
// fishers and ScarabSwarm already rub a carried Games necklace or Ring of dueling every third tick of a walk; this is the same
// thing for the scripts that walk as part of a loop and had no staller: ProgressiveWCFletch (the yew camp to the Seers bank
// and back), ProgressiveCrafter (the Seers bank to the wheel upstairs and back), RuneRunner and MasterCrafter.
//
// The pieces an account needs, all in this file or walkstall:
//   - the aside on every walk its Walker makes (install -> webwalk.Walker.Prepare), a no-op while the pack holds no rub item;
//   - the item kept through bank deposits and hand-offs (keepNames), drawn from the account's own bank when the pack holds none
//     (drawRub), and asked of the mule on a hand-off the account is making anyway (askMuleRub).
//
// Setting: walkRub (bool, default true), the same key and meaning as ScarabSwarm's and the fishers'.

// walkStall is one script's run staller and its mule-ask clock.
type walkStall struct {
	stall *walkstall.Stall
	label string
	// kitAskAt is when the mule may be asked for a rub item again.
	kitAskAt time.Time
}

// newWalkStall reads walkRub (default true) from the account's settings.
func newWalkStall(settings map[string]interface{}, label string) *walkStall {
	return &walkStall{stall: walkstall.New(BoolSetting(settings, "walkRub", true)), label: label}
}

func (w *walkStall) on() bool { return w != nil && w.stall.Active() }

func (w *walkStall) env(ctx *Context) *walkstall.Env {
	return &walkstall.Env{Session: ctx.Session, Config: ctx.Config, Log: ctx.Log, Label: w.label}
}

// prepare is the Walker.Prepare hook of the stall: every walk it prepares first turns running on when the account has the energy
// (walkstall.EnsureRun: a walk made straight through webwalk never did), then, with a rub item in the pack, gets the rub aside.
func (w *walkStall) prepare(ctx *Context) func(o *webwalk.WalkOptions) func() {
	if w == nil {
		return nil
	}
	env := w.env(ctx)
	inner := w.stall.Prepare(env)
	return func(o *webwalk.WalkOptions) func() {
		walkstall.EnsureRun(env)
		if inner == nil {
			return nil
		}
		return inner(o)
	}
}

// install sets the Walker's Prepare to the stall composed with any other asides.
func (w *walkStall) install(ctx *Context, walker *webwalk.Walker, others ...func(o *webwalk.WalkOptions) func()) {
	if walker == nil {
		return
	}
	walker.Prepare = walkstall.Compose(append([]func(o *webwalk.WalkOptions) func(){w.prepare(ctx)}, others...)...)
}

// keepNames is the names a deposit leaves in the pack and a hand-off never gives the mule.
func (w *walkStall) keepNames() []string {
	if !w.on() {
		return nil
	}
	return w.stall.KeepNames()
}

// held is whether the pack holds a rub item.
func (w *walkStall) held(ctx *Context) bool { return w.on() && walkstall.Held(w.env(ctx)) }

// drawRub draws one rub item from the open bank when the pack holds none; withdrawOne is the script's own single withdraw.
func (w *walkStall) drawRub(ctx *Context, bank *actions.Bank, withdrawOne func(name string) bool) bool {
	if !w.on() {
		return false
	}
	return w.stall.DrawRubItem(w.env(ctx), bank, withdrawOne)
}

// askMuleRub asks the mule for one Games necklace or Ring of dueling (any charge) when the pack holds none and the mule's
// published bank holds one. The account must be standing at the mule's post with a slot free, which is where a hand-off ends:
// the ask is one more trade and no walk (scarabplan.RubAskLine: a Rider). bankHolds is how many the account's own bank holds
// (read at its last visit; the ask is skipped when that is above zero, the bank draw serves it). At most every kitAskEvery.
func (w *walkStall) askMuleRub(ctx *Context, mule string, bankHolds int) bool {
	if !w.on() || mule == "" || ctx.Bus == nil || time.Now().Before(w.kitAskAt) || bankHolds > 0 || w.held(ctx) {
		return false
	}
	stock := muleBankCount(ctx, mule)
	if stock == nil {
		return false // the mule's bank is not published, or the reading is stale: nothing to judge by
	}
	line, _ := fleetplan.RubAskLine(false)
	plan, none := fleetplan.FilterAskByBank([]fleetplan.AskLine{line}, stock)
	w.kitAskAt = time.Now().Add(kitAskEvery)
	var kit []orderplan.KitLine
	for _, p := range plan {
		for _, r := range p.Rungs {
			if r.Count > 0 {
				kit = append(kit, orderplan.KitLine{Item: r.Item, Qty: 1})
			}
		}
	}
	if len(kit) == 0 {
		if len(none) > 0 {
			ctx.logf("%s: run staller: %q's published bank holds no Games necklace or Ring of dueling to ask for", w.label, mule)
		}
		return false
	}
	// One of the first rung the mule holds is all a walk needs.
	kit = kit[:1]
	ctx.logf("%s: run staller: asking %q for a %s on the hand-off (the walks have nothing to rub)", w.label, mule, kit[0].Item)
	res := donorrunner.AwaitKit(&donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}, donorrunner.KitAsk{Mule: mule, Me: ctx.Username, Lines: kit, Bus: ctx.Bus})
	got := 0
	for _, n := range res.Got {
		got += n
	}
	if got == 0 {
		ctx.logf("%s: run staller: %q handed over nothing (%s)", w.label, mule, strings.TrimSpace(res.Reason))
		return false
	}
	ctx.logf("%s: run staller: %q handed over %v", w.label, mule, res.Got)
	return true
}

// withdrawOneNamed takes one un-noted item of exactly this name out of the open bank and waits a moment for it to show in the
// pack, for the scripts that have no withdraw helper of their own. It reports whether the pack's count rose.
func withdrawOneNamed(ctx *Context, bank *actions.Bank, name string) bool {
	for _, it := range bank.Items() {
		obj, ok := ctx.Config.Obj(int(it.ID))
		if !ok || obj.Noted() || !strings.EqualFold(strings.TrimSpace(obj.Name), name) {
			continue
		}
		before := packCountOfID(bank, it.ID)
		if err := bank.WithdrawOne(it.ID); err != nil {
			ctx.logf("%s: withdraw one %q: %v", "run staller", name, err)
			return false
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if packCountOfID(bank, it.ID) > before {
				return true
			}
			if !ctx.Delay(100 * time.Millisecond) {
				return false
			}
		}
		return false
	}
	return false
}

func packCountOfID(bank *actions.Bank, id int32) int {
	n := 0
	for _, it := range bank.Inventory() {
		if it.ID == id {
			n += max(1, int(it.Count))
		}
	}
	return n
}

// drawRubAtBank is the bank hook that draws the run staller's jewellery from the account's own bank when the pack holds none,
// nil while the stall is off.
func (w *walkStall) drawRubAtBank(ctx *Context) func(bank *actions.Bank) {
	if !w.on() {
		return nil
	}
	return func(bank *actions.Bank) {
		w.drawRub(ctx, bank, func(name string) bool { return withdrawOneNamed(ctx, bank, name) })
	}
}
