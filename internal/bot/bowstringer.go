package bot

// BowStringer strings the fleet's unstrung bows so they alch for double (a user request: a
// strung Maple longbow alchs for 384 coins, the unstrung one for 192). It runs on one Fletching
// account on a members world beside the mule: bank at the mule's post, draw 14 unstrung bows and
// 14 Bow strings, string all 14 one action at a time, deposit, repeat; when the bank runs out,
// order a kit of noted bows and strings from the mule and bank it. Strung bows stay in this
// account's bank (a later lane hands them to alchers).
//
// The decisions are internal/scripts/bowstringplan's, the I/O internal/scripts/bowstringrunner's
// over scarabrunner's bank, walk and bundled kit order. The mule-side checks (heartbeat, world,
// queue, published bank) are ScarabSwarm's own helpers in scarabswarm_muleask.go and
// woodcutterpeer.go.
//
// ROLLOUT GATES: the mule must list this account in allowedPartners, and its hand-out caps apply
// (orderplan's open-stock path: at most 1000 of an item per order and 10000 per window; one order
// per donor per window unless the mule's handoutPerDonor is raised). A kit the mule defers for its
// window is simply asked again after orderBackoffMinutes.
//
// Settings (bowstringplan.Settings, strict): mule, bows, kitSize, maxFruitlessKits,
// orderBackoffMinutes, logDetail (and pinWorld, which the fleet reads).

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("BowStringer", func() Bot { return &bowStringerBot{} })
}

// bowStringerBeat is the longest a wait goes without a log line (the stall watchdog acts after
// ten silent minutes).
const bowStringerBeat = 5 * time.Minute

// bowStringerMaxRefusals is how many stringing attempts in a row that change nothing stop the
// script: the level, a free world or a wrong id would never come right by retrying.
const bowStringerMaxRefusals = 3

type bowStringerBot struct {
	set    bowstringplan.Settings
	walker *webwalk.Walker
	// embed is set when another bot runs this one as a round (bowstringer_embed.go); nil for the
	// registered script.
	embed *bowStringerEmbed

	exhausted map[string]bool
	ids       map[string]bowstringrunner.IDs
	fruitless int
	refusals  int
	nextOrder time.Time

	strung map[string]int
	trips  int
	cycles int // of those trips, the ones the owner's three-click bank cycle drew
	kits   int
	// landed counts the kits that left the bank able to fund more bows than before: what an
	// embedding round's fast run waits on (mastercrafter_bows.go), since order() banks a kit's
	// notes itself and the pack never shows them.
	landed   int
	lastBeat time.Time
	said     map[string]time.Time
}

func (b *bowStringerBot) deps(ctx *Context) *fleetrunner.Deps {
	return &fleetrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Walker: b.walker,
		Log: ctx.Log, Loot: ctx.Loot, Me: ctx.Username, World: ctx.World(),
	}
}

func (b *bowStringerBot) logf(ctx *Context, format string, args ...any) {
	b.lastBeat = time.Now()
	ctx.logf("bowstringer: "+format, args...)
}

func (b *bowStringerBot) verbosef(ctx *Context, format string, args ...any) {
	if b.set.Verbose() {
		b.logf(ctx, format, args...)
	}
}

func (b *bowStringerBot) sayEvery(ctx *Context, key string, every time.Duration, format string, args ...any) {
	if t, ok := b.said[key]; ok && time.Since(t) < every {
		return
	}
	b.said[key] = time.Now()
	b.logf(ctx, format, args...)
}

// stop ends the script for good with a logged reason; the fleet does not log it back in.
func (b *bowStringerBot) stop(ctx *Context, reason string) {
	if b.embed != nil {
		// A round of another script: hand control back, never end the run or the account's session.
		b.logf(ctx, "round over: %s (strung %s)", reason, b.describeStrung())
		b.embed.finish(reason)
		return
	}
	b.logf(ctx, "STOP reason=%q strung=%s", reason, b.describeStrung())
	ctx.RequestFinish("bowstringer: " + reason)
	ctx.requestStop()
}

func (b *bowStringerBot) describeStrung() string {
	if len(b.strung) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(b.strung))
	for name, n := range b.strung {
		parts = append(parts, fmt.Sprintf("%d %s", n, name))
	}
	return strings.Join(parts, ", ")
}

func (b *bowStringerBot) OnStart(ctx *Context) error {
	set, err := b.settingsFor(ctx)
	if err != nil {
		return fmt.Errorf("bowstringer: %w", err)
	}
	if ctx.Pack == nil {
		return fmt.Errorf("bowstringer: no collision pack configured (fleet.config.json needs a packPath)")
	}
	b.set = set
	b.exhausted = map[string]bool{}
	b.ids = map[string]bowstringrunner.IDs{}
	b.strung = map[string]int{}
	b.said = map[string]time.Time{}
	b.lastBeat = time.Now()
	// The same walker ScarabSwarm and RuneRunner build; plain pathfinding is the fallback.
	if w, werr := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); werr != nil {
		b.logf(ctx, "no webwalk (%v) — walks fall back to plain pathfinding", werr)
	} else {
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{
				Client: w.Client, Session: ctx.Session, Config: ctx.Config,
				Components: ctx.Components, Log: ctx.Log,
			}))
		}
		b.walker = w
	}
	b.logf(ctx, "stringing %s from %s, kits of %d", strings.Join(set.Bows, ", "), set.Mule, set.KitSize)
	return nil
}

func (b *bowStringerBot) OnStop(ctx *Context) {
	b.logf(ctx, "stopped — strung %s in %d bank trip(s) (%d by the 3-click bank cycle), %d kit order(s)", b.describeStrung(), b.trips, b.cycles, b.kits)
}

func (b *bowStringerBot) Loop(ctx *Context) error {
	if ctx.Guardian != nil && ctx.Guardian.Busy() {
		ctx.Delay(time.Second)
		return nil
	}
	d := b.deps(ctx)
	level, known := bowstringrunner.Fletching(d)
	if !known {
		ctx.Delay(time.Second)
		return nil
	}
	bow, ok := bowstringplan.PickBow(b.set.Bows, level, b.exhausted)
	if !ok {
		b.stop(ctx, fmt.Sprintf("no bow left to string at Fletching %d (exhausted: %s)", level, b.exhaustedList()))
		return nil
	}
	ids, ok := b.ids[bow.Name]
	if !ok {
		var err error
		if ids, err = bowstringrunner.ResolveIDs(ctx.Config, bow.Name); err != nil {
			b.logf(ctx, "%s: %v — skipping it", bow.Name, err)
			b.exhausted[bowstringplan.Key(bow.Name)] = true
			return nil
		}
		b.ids[bow.Name] = ids
		b.logf(ctx, "%s (Fletching %d): unstrung id %d, strung id %d, %s id %d", bow.Name, bow.Level, ids.Unstrung, ids.Strung, bowstringplan.BowString, ids.String)
	}
	if b.embed != nil && b.embed.fast != nil && b.embed.fast(ctx, d, bow, ids) {
		return nil
	}
	snap := bowstringplan.Snapshot{PackBows: bowstringrunner.PackCount(d, ids.Unstrung), PackStrings: bowstringrunner.PackCount(d, ids.String)}
	switch bowstringplan.Decide(snap) {
	case bowstringplan.StepString:
		b.stringStep(ctx, d, bow, ids)
	default:
		b.bankStep(ctx, d, bow, ids)
	}
	return nil
}

func (b *bowStringerBot) exhaustedList() string {
	if len(b.exhausted) == 0 {
		return "none"
	}
	var out []string
	for k := range b.exhausted {
		out = append(out, k)
	}
	return strings.Join(out, ", ")
}

func (b *bowStringerBot) stringStep(ctx *Context, d *fleetrunner.Deps, bow bowstringplan.Bow, ids bowstringrunner.IDs) {
	r := bowstringrunner.StringAll(d, ids)
	if r.Strung > 0 {
		b.refusals = 0
		if b.embed != nil {
			b.embed.useFails = 0
		}
		b.strung[bow.Name] += r.Strung
		lootStrung(ctx, ids.Unstrung, ids.Strung, bow.Name, r.Strung)
		b.verbosef(ctx, "strung %d %s (%d this run)", r.Strung, bow.Name, b.strung[bow.Name])
	}
	switch r.Reason {
	case "use-failed":
		// Not counted toward the stop: the backpack component can be unresolvable for a while
		// after login (bankfletchrunner.Fletch's live note), which retrying outlasts.
		b.sayEvery(ctx, "use-failed", time.Minute, "could not use a %s on a %s — retrying", bowstringplan.BowString, bow.Name)
		if b.embed != nil && b.embed.fail(ctx, &b.embed.useFails, "could not use a "+bowstringplan.BowString+" on a "+bow.Name) {
			return
		}
		ctx.Delay(2 * time.Second)
	case "refused":
		b.refusals++
		b.logf(ctx, "stringing %s: %s (%d in a row)", bow.Name, r.Reason, b.refusals)
		if b.refusals >= bowStringerMaxRefusals {
			b.stop(ctx, fmt.Sprintf("stringing %s changed nothing %d times in a row (%s): check the level, a members world and the ids", bow.Name, b.refusals, r.Reason))
			return
		}
		ctx.Delay(2 * time.Second)
	}
}

func (b *bowStringerBot) bankTile(ctx *Context) tile.Tile {
	return scarabMulePresenceOf(ctx, b.set.Mule).Post
}

func (b *bowStringerBot) bankStep(ctx *Context, d *fleetrunner.Deps, bow bowstringplan.Bow, ids bowstringrunner.IDs) {
	// A deferred order is backing off, which is only ever set once the bank was found unable to
	// fund a batch: wait rather than reopen the bank every pass.
	if wait := time.Until(b.nextOrder); wait > 0 {
		b.sayEvery(ctx, "backoff", bowStringerBeat, "waiting for stock: asking %s again in %s", b.set.Mule, wait.Round(time.Second))
		ctx.Delay(min(wait, 30*time.Second))
		return
	}
	at := b.bankTile(ctx)
	v := bowstringrunner.Bank(d, at, ids)
	if !v.Opened || v.Err != "" {
		b.sayEvery(ctx, "bank", time.Minute, "bank at %d,%d: %s — retrying", at.X, at.Z, v.Err)
		if b.embed != nil && b.embed.fail(ctx, &b.embed.bankFails, "the bank at the mule's post: "+v.Err) {
			return
		}
		ctx.Delay(3 * time.Second)
		return
	}
	if b.embed != nil {
		b.embed.bankFails = 0
	}
	b.trips++
	how := ""
	if v.Cycle {
		b.cycles++
		how = ", by the 3-click cycle"
	}
	if v.Drew > 0 {
		b.verbosef(ctx, "trip %d: drew %d %s and strings (bank: %d unstrung, %d strings, %d strung%s)", b.trips, v.Drew, bow.Name, v.BankBows, v.BankStrings, v.BankStrung, how)
		if b.trips%25 == 0 {
			b.logf(ctx, "trip %d: strung %s; bank holds %d unstrung %s, %d strings, %d strung; %d of %d trips by the 3-click bank cycle", b.trips, b.describeStrung(), v.BankBows-v.Drew, bow.Name, v.BankStrings-v.Drew, v.BankStrung, b.cycles, b.trips)
		}
		return
	}
	// The bank cannot fund a batch: order from the mule.
	b.order(ctx, d, bow, ids, v)
}

func (b *bowStringerBot) backoff(ctx *Context, why string) {
	b.nextOrder = time.Now().Add(time.Duration(b.set.OrderBackoffMinutes) * time.Minute)
	if b.embed != nil {
		// The host decides when to ask again (its own check interval): the round ends here rather
		// than waiting out the back-off with the account standing at the bank.
		b.logf(ctx, "kit not placed or not useful: %s — ending the round", why)
		b.embed.finish("deferred: " + why)
		return
	}
	b.logf(ctx, "kit not placed or not useful: %s — asking again in %d minute(s)", why, b.set.OrderBackoffMinutes)
}

// kitPlanFor is the kit to ask the mule for when the bank can no longer fund a batch of bow, and the
// name its bow line is asked under. The bows are asked for by the QUALIFIED name of the unstrung obj
// ("Yew shortbow id68", bowstringplan.AskName): the strung bow shares the plain name, and the mule hands
// over the first obj its bank holds under a name (live 6 Oct 2026: 55,634 strung Yew shortbows to a kit
// that wanted the unstrung ones, "no-bows"). The mule's count is read under that name too.
func (b *bowStringerBot) kitPlanFor(bow bowstringplan.Bow, ids bowstringrunner.IDs, v bowstringrunner.BankVisit, stock bowstringplan.MuleStock) (item string, plan bowstringplan.Order) {
	item = bowstringplan.AskName(bow.Name, int(ids.Unstrung))
	return item, bowstringplan.PlanOrderAs(bow.Name, item, v.BankBows, v.BankStrings, b.set.KitSize, stock)
}

func (b *bowStringerBot) order(ctx *Context, d *fleetrunner.Deps, bow bowstringplan.Bow, ids bowstringrunner.IDs, v bowstringrunner.BankVisit) {
	mule := b.set.Mule
	var stock bowstringplan.MuleStock
	if f := b.muleStock(ctx, mule); f != nil {
		stock = f
	}
	item, plan := b.kitPlanFor(bow, ids, v, stock)
	if plan.Exhausted {
		b.exhausted[bowstringplan.Key(bow.Name)] = true
		b.logf(ctx, "%s is done: %s (strung %d this run)", bow.Name, plan.Why, b.strung[bow.Name])
		return
	}
	if len(plan.Lines) == 0 {
		b.stop(ctx, plan.Why)
		return
	}
	if ctx.Bus == nil || ctx.Username == "" {
		b.stop(ctx, "no wall bus to order from the mule")
		return
	}
	p := scarabMulePresenceOf(ctx, mule)
	if !p.Heard || !p.SameWorld {
		b.backoff(ctx, p.Why)
		return
	}
	if why, long := scarabMuleQueueTooLong(ctx, mule, donorrunner.DefaultKitTimeout); long {
		b.backoff(ctx, why)
		return
	}
	if !fleetrunner.WalkTo(d, p.Post, 3, 5*time.Minute) {
		b.backoff(ctx, fmt.Sprintf("could not reach %s's post at %d,%d", mule, p.Post.X, p.Post.Z))
		return
	}
	lines := make([]string, 0, len(plan.Lines))
	for _, l := range plan.Lines {
		lines = append(lines, fmt.Sprintf("%d %s", l.Qty, l.Item))
	}
	b.kits++
	b.logf(ctx, "kit %d: asking %s for %s (bank held %d unstrung, %d strings, %d strung)", b.kits, mule, strings.Join(lines, " + "), v.BankBows, v.BankStrings, v.BankStrung)
	res := bowstringrunner.OrderKit(d, ctx.Bus, ctx.Username, mule, plan.Lines, p.Post, ctx.CurrentWorld, donorrunner.DefaultKitTimeout)
	if res.Stopped {
		return
	}
	traded := 0
	for _, n := range res.Got {
		traded += n
	}
	// Bank what came (a note deposits as the item) and measure the gain by id.
	after := bowstringrunner.Bank(d, p.Post, ids)
	// Useful is what the bank can now fund, in bow-and-string pairs: the bank could fund none
	// before the kit, so a strings-only kit counts as much as a bows one.
	gained := 0
	if after.Opened && after.Err == "" {
		b.trips++
		gained = min(after.BankBows, after.BankStrings)
	}
	if gained > 0 {
		b.landed++
	}
	if why := res.Declined[bowstringplan.BowString]; gained == 0 && bowstringplan.DeclineMeansEmpty(why) {
		b.stop(ctx, fmt.Sprintf("%s has no %s left (%s)", mule, bowstringplan.BowString, why))
		return
	}
	reason := res.Declined[item]
	if reason == "" && res.Reason != "" && traded == 0 {
		reason = res.Reason
	}
	out := bowstringplan.ClassifyKit(gained, traded > 0, reason)
	var exhausted bool
	b.fruitless, exhausted = bowstringplan.AfterKit(out, b.fruitless, b.set.MaxFruitlessKits)
	b.logf(ctx, "kit %d: %s — %d item(s) arrived, bank can now fund %d %s, bank holds %d strings (declined: %v)",
		b.kits, out, traded, gained, bow.Name, after.BankStrings, res.Declined)
	switch {
	case exhausted:
		b.exhausted[bowstringplan.Key(bow.Name)] = true
		b.logf(ctx, "%s is done: %s has none left to give (strung %d this run)", bow.Name, mule, b.strung[bow.Name])
	case out != bowstringplan.KitGained:
		b.backoff(ctx, fmt.Sprintf("kit %s (%s)", out, reason))
	}
}
