package bot

// AntipoisonMaker brews Antipoison(3) by Herblore and hands it to the fleet's mule, which supplies
// the Kalphite Queen fighters (scarabswarm_stage6.go asks the mule for antipoison once the mule's
// published bank shows some). It trains Herblore as it goes.
//
// It works from the bank at the mule's post (Catherby), so the hand-off needs no walk: bank, draw a
// batch, brew, bank, and when handOffAt potions are banked draw them and trade them to the mule.
// The chain, each link a step of antipoisonmakeplan.Decide:
//
//	Marrentill      asked from the mule in a bundled kit (the mule holds thousands).
//	Vial of water   Bought filled at Aemad's Adventuring Supplies (East Ardougne, 500 in stock).
//	Horn dust       Unicorn horns from the unicorn field north of Catherby (they always drop one),
//	                ground with a Pestle and mortar bought once at Jatix's (Taverley).
//
// Needs coins in its own bank for the shop run (a few hundred); the mule does not hand coins out.
// The mule must list this account in allowedPartners.
//
// Settings (antipoisonmakeplan.Settings, strict): mule, handOffAt, kitSize, shopVials, huntHPFloor,
// maxFruitlessAsks, backoffMinutes, logDetail (and pinWorld, which the fleet reads).

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/antipoisonmakeplan"
	"rsb0t-go/internal/scripts/antipoisonmakerunner"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("AntipoisonMaker", func() Bot { return &antipoisonMakerBot{} })
}

// antipoisonBeat is the longest a wait goes without a log line (the stall watchdog acts after ten
// silent minutes).
const antipoisonBeat = 5 * time.Minute

type antipoisonMakerBot struct {
	set    antipoisonmakeplan.Settings
	walker *webwalk.Walker

	askFruitless   int
	askExhausted   bool
	askBackoff     time.Time
	shopBackoff    time.Time
	handBackoff    time.Time
	refusals       int
	lastBeat       time.Time
	made, unf      int
	ground, kills  int
	handed, trips  int
	handoffs, asks int
}

func (b *antipoisonMakerBot) deps(ctx *Context) *fleetrunner.Deps {
	return &fleetrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Walker: b.walker,
		Log: ctx.Log, Loot: ctx.Loot, Me: ctx.Username, World: ctx.World(),
	}
}

func (b *antipoisonMakerBot) logf(ctx *Context, format string, args ...any) {
	b.lastBeat = time.Now()
	ctx.logf("antipoison: "+format, args...)
}

func (b *antipoisonMakerBot) verbosef(ctx *Context, format string, args ...any) {
	if b.set.Verbose() {
		b.logf(ctx, format, args...)
	}
}

func (b *antipoisonMakerBot) stop(ctx *Context, reason string) {
	b.logf(ctx, "STOP reason=%q %s", reason, b.totals())
	ctx.RequestFinish("antipoison: " + reason)
	ctx.requestStop()
}

func (b *antipoisonMakerBot) totals() string {
	return fmt.Sprintf("made %d Antipoison(3) (%d unfinished, %d horns ground, %d unicorns killed), handed %d to the mule in %d hand-off(s), %d mule ask(s)",
		b.made, b.unf, b.ground, b.kills, b.handed, b.handoffs, b.asks)
}

func (b *antipoisonMakerBot) OnStart(ctx *Context) error {
	set, err := antipoisonmakeplan.DecodeSettings(ctx.Settings)
	if err != nil {
		return err
	}
	if ctx.Pack == nil {
		return fmt.Errorf("antipoison: no collision pack configured (fleet.config.json needs a packPath)")
	}
	b.set = set
	b.lastBeat = time.Now()
	if w, werr := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); werr != nil {
		b.logf(ctx, "no webwalk (%v) - walks fall back to plain pathfinding", werr)
	} else {
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{
				Client: w.Client, Session: ctx.Session, Config: ctx.Config,
				Components: ctx.Components, Log: ctx.Log,
			}))
		}
		b.walker = w
	}
	b.logf(ctx, "making Antipoison(3) for %s, handing over at %d (mule asks of %d Marrentill)", set.Mule, set.HandOffAt, set.KitSize)
	return nil
}

func (b *antipoisonMakerBot) OnStop(ctx *Context) { b.logf(ctx, "stopped - %s", b.totals()) }

func (b *antipoisonMakerBot) backoffDur() time.Duration {
	return time.Duration(b.set.BackoffMinutes) * time.Minute
}

func (b *antipoisonMakerBot) Loop(ctx *Context) error {
	if ctx.Guardian != nil && ctx.Guardian.Busy() {
		ctx.Delay(time.Second)
		return nil
	}
	d := b.deps(ctx)
	level, known := antipoisonmakerunner.Herblore(d)
	if known && level < antipoisonmakeplan.MinLevel {
		b.stop(ctx, fmt.Sprintf("Herblore %d is below %d: Antipoison cannot be brewed", level, antipoisonmakeplan.MinLevel))
		return nil
	}
	if !known {
		ctx.Delay(time.Second)
		return nil
	}
	post := scarabMulePresenceOf(ctx, b.set.Mule).Post
	bank, c, errText := antipoisonmakerunner.OpenAndRead(d, post)
	if errText != "" {
		b.logf(ctx, "%s - retrying", errText)
		ctx.Delay(5 * time.Second)
		return nil
	}
	now := time.Now()
	dec := antipoisonmakeplan.Decide(antipoisonmakeplan.Snapshot{
		Level: level, LevelKnown: true,
		Marrentill: c.Marrentill, VialWater: c.VialWater, VialEmpty: c.VialEmpty, Dust: c.Dust, Horn: c.Horn,
		Unf: c.Unf, Antipoison: c.Antipoison, HasPestle: c.Pestle, Coins: c.Coins, PackFree: antipoisonmakeplan.PackSlots,
		HandOffAt:      b.set.HandOffAt,
		HandOffBackoff: now.Before(b.handBackoff), AskBackoff: now.Before(b.askBackoff),
		ShopBackoff: now.Before(b.shopBackoff), AskExhausted: b.askExhausted,
	})
	b.verbosef(ctx, "bank: %d Marrentill, %d water vials, %d dust, %d horns, %d unfinished, %d Antipoison, pestle %v, %d coins -> %s (%s)",
		c.Marrentill, c.VialWater, c.Dust, c.Horn, c.Unf, c.Antipoison, c.Pestle, c.Coins, dec.Step, dec.Why)

	switch dec.Step {
	case antipoisonmakeplan.StepStop:
		_ = bank.Close()
		b.stop(ctx, dec.Why)
	case antipoisonmakeplan.StepHandOff:
		b.handOff(ctx, d, bank, dec.N)
	case antipoisonmakeplan.StepAntipoison:
		b.brew(ctx, d, bank, dec.N, antipoisonmakeplan.UnfID, antipoisonmakeplan.HornDustID, antipoisonmakerunner.MakeAntipoison, &b.made, "Antipoison(3)", c)
	case antipoisonmakeplan.StepUnf:
		b.brew(ctx, d, bank, dec.N, antipoisonmakeplan.MarrentillID, antipoisonmakeplan.VialWaterID, antipoisonmakerunner.MakeUnf, &b.unf, "unfinished potion(s)", c)
	case antipoisonmakeplan.StepGrind:
		if err := antipoisonmakerunner.Draw(d, bank, antipoisonmakerunner.Line{ID: antipoisonmakeplan.PestleID, N: 1}, antipoisonmakerunner.Line{ID: antipoisonmakeplan.HornID, N: dec.N}); err != nil {
			_ = bank.Close()
			b.logf(ctx, "drawing horns: %v", err)
			ctx.Delay(3 * time.Second)
			return nil
		}
		_ = bank.Close()
		r := antipoisonmakerunner.Grind(d)
		b.ground += r.Made
		b.logf(ctx, "ground %d horn(s) into dust (%s); bank held %d dust", r.Made, r.Reason, c.Dust)
		b.afterMake(ctx, r.Made, r.Reason)
	case antipoisonmakeplan.StepHunt:
		_ = bank.Close()
		b.logf(ctx, "hunting unicorns for %d horn(s) (%s)", dec.N, dec.Why)
		r := antipoisonmakerunner.Hunt(d, dec.N, b.set.HuntHPFloor)
		b.kills += r.Kills
		b.logf(ctx, "hunt over: %s - %d kill(s), %d horn(s) in the pack", r.Reason, r.Kills, r.Horns)
		if r.Reason == "no-unicorns" || r.Reason == "unreachable" || r.Reason == "hp" {
			ctx.Delay(10 * time.Second)
		}
	case antipoisonmakeplan.StepShop:
		b.shop(ctx, d, bank, c)
	case antipoisonmakeplan.StepAskMule:
		_ = bank.Close()
		b.askMule(ctx, d, post)
	default: // wait
		_ = bank.Close()
		if time.Since(b.lastBeat) >= antipoisonBeat {
			b.logf(ctx, "waiting: %s", dec.Why)
		}
		ctx.Delay(30 * time.Second)
	}
	return nil
}

// brew draws n of each input, brews them and returns; the next pass banks the product.
func (b *antipoisonMakerBot) brew(ctx *Context, d *fleetrunner.Deps, bank *actions.Bank, n int, a, c2 int32,
	make func(*fleetrunner.Deps, int) antipoisonmakerunner.MakeResult, counter *int, what string, c antipoisonmakerunner.Counts) {
	if err := antipoisonmakerunner.Draw(d, bank, antipoisonmakerunner.Line{ID: a, N: n}, antipoisonmakerunner.Line{ID: c2, N: n}); err != nil {
		_ = bank.Close()
		b.logf(ctx, "drawing a batch of %d: %v", n, err)
		ctx.Delay(3 * time.Second)
		return
	}
	_ = bank.Close()
	r := make(d, n)
	*counter += r.Made
	b.trips++
	b.logf(ctx, "batch %d: made %d %s (%s); bank held %d Marrentill, %d water vials, %d dust, %d unfinished, %d Antipoison",
		b.trips, r.Made, what, r.Reason, c.Marrentill, c.VialWater, c.Dust, c.Unf, c.Antipoison)
	b.afterMake(ctx, r.Made, r.Reason)
}

func (b *antipoisonMakerBot) afterMake(ctx *Context, made int, reason string) {
	switch {
	case reason == "refused" && made == 0:
		b.refusals++
		if b.refusals >= 3 {
			b.stop(ctx, "brewing changed nothing 3 times in a row (check the Herblore level and that this is a members world)")
		}
	case reason == "use-failed" && made == 0:
		ctx.Delay(2 * time.Second)
	case made > 0:
		b.refusals = 0
	}
}

// muleReady reports why the mule cannot be dealt with now, "" when it can.
func (b *antipoisonMakerBot) muleReady(ctx *Context) string {
	if ctx.Bus == nil || ctx.Username == "" {
		return "no wall bus to reach the mule"
	}
	p := scarabMulePresenceOf(ctx, b.set.Mule)
	if !p.Heard || !p.SameWorld {
		return p.Why
	}
	if why, long := scarabMuleQueueTooLong(ctx, b.set.Mule, donorrunner.DefaultKitTimeout); long {
		return why
	}
	return ""
}

func (b *antipoisonMakerBot) handOff(ctx *Context, d *fleetrunner.Deps, bank *actions.Bank, n int) {
	if why := b.muleReady(ctx); why != "" {
		_ = bank.Close()
		b.handBackoff = time.Now().Add(b.backoffDur())
		b.logf(ctx, "hand-off deferred: %s - making more, asking again in %d minute(s)", why, b.set.BackoffMinutes)
		return
	}
	if err := antipoisonmakerunner.Draw(d, bank, antipoisonmakerunner.Line{ID: antipoisonmakeplan.AntipoisonID, N: n}); err != nil {
		_ = bank.Close()
		b.logf(ctx, "drawing the Antipoison: %v", err)
		ctx.Delay(3 * time.Second)
		return
	}
	_ = bank.Close()
	before := antipoisonmakerunner.PackCount(d, antipoisonmakeplan.AntipoisonID)
	post := scarabMulePresenceOf(ctx, b.set.Mule).Post
	rows := func() []donorplan.ItemRow {
		var out []donorplan.ItemRow
		for _, it := range fleetrunner.PackSlotItems(d) {
			if name := fleetrunner.ObjName(d, it.ID); name != "" {
				out = append(out, donorplan.ItemRow{Name: name, ID: int(it.ID), Count: max(1, int(it.Count))})
			}
		}
		return out
	}
	rd := fleetrunner.MuleDeps(d, ctx.Bus, ctx.Username)
	WireDonorWorlds(ctx, rd)
	b.logf(ctx, "handing %d Antipoison(3) to %s at %d,%d", before, b.set.Mule, post.X, post.Z)
	res := donorrunner.Run(rd, donorrunner.Options{
		Mule: b.set.Mule, Stand: post, RowsInBank: 1, Drawn: before,
		Load: func() []donorplan.LoadGroup {
			return donorplan.PlanLoad(rows(), donorplan.DefaultMuleFreeSlots, donorplan.Rules{})
		},
		HaulLeft: func() int { return donorplan.HaulOf(rows(), donorplan.Rules{}) },
	})
	gone := before - antipoisonmakerunner.PackCount(d, antipoisonmakeplan.AntipoisonID)
	if res.Stopped {
		return
	}
	if gone > 0 {
		b.handed += gone
		b.handoffs++
		b.logf(ctx, "hand-off %d: %d Antipoison(3) crossed to %s (%d handed over this run)", b.handoffs, gone, b.set.Mule, b.handed)
		return
	}
	b.handBackoff = time.Now().Add(b.backoffDur())
	b.logf(ctx, "hand-off to %s failed (%s) - keeping the potions, trying again in %d minute(s)", b.set.Mule, res.Reason, b.set.BackoffMinutes)
}

func (b *antipoisonMakerBot) askMule(ctx *Context, d *fleetrunner.Deps, post tile.Tile) {
	if why := b.muleReady(ctx); why != "" {
		b.askBackoff = time.Now().Add(b.backoffDur())
		b.logf(ctx, "Marrentill ask deferred: %s - asking again in %d minute(s)", why, b.set.BackoffMinutes)
		return
	}
	b.asks++
	b.logf(ctx, "ask %d: %s for %d Marrentill", b.asks, b.set.Mule, b.set.KitSize)
	got, reason, stopped := antipoisonmakerunner.OrderMarrentill(d, ctx.Bus, ctx.Username, b.set.Mule, b.set.KitSize, post, ctx.CurrentWorld, donorrunner.DefaultKitTimeout)
	if stopped {
		return
	}
	var exhausted bool
	b.askFruitless, exhausted = antipoisonmakeplan.AfterAsk(got > 0, b.askFruitless, b.set.MaxFruitlessAsks)
	if got > 0 {
		b.logf(ctx, "ask %d: %d Marrentill arrived from %s", b.asks, got, b.set.Mule)
		return
	}
	b.askBackoff = time.Now().Add(b.backoffDur())
	b.askExhausted = exhausted
	b.logf(ctx, "ask %d: no Marrentill from %s (%s) - %d fruitless in a row%s", b.asks, b.set.Mule, reason, b.askFruitless, map[bool]string{true: ", treating the mule as out of Marrentill", false: ""}[exhausted])
}

func (b *antipoisonMakerBot) shop(ctx *Context, d *fleetrunner.Deps, bank *actions.Bank, c antipoisonmakerunner.Counts) {
	if c.Coins < 50 {
		_ = bank.Close()
		b.shopBackoff = time.Now().Add(b.backoffDur())
		b.logf(ctx, "shop run needs coins: the bank holds %d - put a few hundred in this account's bank; trying again in %d minute(s)", c.Coins, b.set.BackoffMinutes)
		return
	}
	if err := antipoisonmakerunner.Draw(d, bank, antipoisonmakerunner.Line{ID: antipoisonmakeplan.CoinsID, N: min(c.Coins, antipoisonmakeplan.CoinsPerRun)}); err != nil {
		_ = bank.Close()
		b.logf(ctx, "drawing coins: %v", err)
		ctx.Delay(3 * time.Second)
		return
	}
	_ = bank.Close()
	list := antipoisonmakeplan.ShopFor(antipoisonmakerunner.FreeSlots(d), b.set.ShopVials, c.Pestle)
	b.logf(ctx, "shop run: %d vial(s) of water from %s%s", list.Vials, antipoisonmakeplan.Aemad, map[bool]string{true: ", then a pestle and mortar from " + antipoisonmakeplan.Jatix, false: ""}[list.Pestle])
	// Ardougne first: the walker's way to Taverley passes it.
	vials, verr := antipoisonmakerunner.BuyAt(d, []string{antipoisonmakeplan.Aemad, antipoisonmakeplan.Kortan}, antipoisonmakeplan.AemadStand,
		antipoisonmakeplan.AemadWalk, antipoisonmakerunner.Line{ID: antipoisonmakeplan.VialWaterID, N: list.Vials})
	var pestles int
	var perr error
	if list.Pestle {
		pestles, perr = antipoisonmakerunner.BuyAt(d, []string{antipoisonmakeplan.Jatix}, antipoisonmakeplan.JatixStand,
			antipoisonmakeplan.JatixWalk, antipoisonmakerunner.Line{ID: antipoisonmakeplan.PestleID, N: 1})
	}
	b.logf(ctx, "shop run: bought %d vial(s) of water, pestle %v", vials, pestles > 0)
	if verr != nil || perr != nil || (vials == 0 && pestles == 0) {
		b.shopBackoff = time.Now().Add(b.backoffDur())
		b.logf(ctx, "shop run fell short (vials: %v; pestle: %v) - trying again in %d minute(s)", verr, perr, b.set.BackoffMinutes)
	}
}
