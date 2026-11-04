package bot

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/phaselog"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/peerrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("ProgressiveWoodcutter", func() Bot { return &woodcutterBot{} })
}

// woodcutterBot ports ProgressiveWoodcutter.ts's own loop(): every Loop call re-derives the
// phase from live state (woodcutplan.DecideWoodPhase) and does one phase-appropriate step, the
// same shape essenceBot/fisherBot/crafterBot already proved. The phases:
//
//   - accept, tutorial, bright: the appearance screen, the tutorial skip, and the brightness
//     press — all freshstartrunner, shared with every other fresh-account script.
//   - store: the whole starter kit sold at the Lumbridge general store, the bronze axe
//     included (an axe has no Woodcutting requirement on this server).
//   - fund, travel: Men pickpocketed in Lumbridge for the two boat fares, then Port Sarim ->
//     Karamja -> Brimhaven -> Ardougne -> Seers, via freshstartrunner.PickpocketStep/SailLeg.
//   - bank: at the Seers bank, everything deposited but the best axe held, the best banked axe
//     drawn out when it beats what is held (and a noted rune axe delivery un-noted the moment
//     the bank sees it).
//   - order: no axe anywhere the account can reach one. Walks back to the Seers bank and first
//     ASKS THE MULE FOR AN AXE (woodcutterhandout.go) — the only path here that works with
//     nobody watching, and the one five production accounts sat blocked without on 2026-09-21.
//     Only if that fails does it hand over to axeStep (woodcutteraxe.go): ask the mule for
//     COINS over the bus, then buy an axe with them at the Seers PLAYER market (which needs a
//     bot stall, always present -- see market.go), with Bob's Axes in Lumbridge as the
//     steel-and-below shop fallback. The
//     buying half is off unless axePrice and axeBudget are both set; the asking half is not.
//   - walk, chop: the best tree the level and the chosen target allow, banking every load.
//   - stuck: no axe anywhere reachable and no mule configured at all.
//
// Ahead of all of that, every Loop handles this script's OWN woodcutting random events
// (woodcutplan.DecideWoodEvent): an axe whose head flew off is found and re-attached before
// anything else, because the head despawns about two minutes after it lands, and a broken axe
// with nothing else to chop with is taken to Bob's. The Tree spirit is NOT handled here — the
// shared Guardian already carries npc 438-443 and scopes its flee to the account actually
// targeted. See internal/scripts/woodcutplan/events.go for the content evidence on all three,
// including why an Ent cannot break this client's axe at all.
//
// Once the Seers bank and the pack together hold muleAt logs of the target species, every log
// and everything else the bank has gathered (random-event drops included) goes to the mule via
// internal/scripts/donorrunner — the identical hand-over machinery essenceBot/fisherBot/
// crafterBot already drive, at the meet tile the muleAtTile setting names (woodcutplan.DefaultMuleStand,
// the Seers bank stand, unless the fleet says otherwise — see Settings.MuleStand).
//
// Settings (the TS manifest's own keys):
//
//	target      string default "Yew"    — the tree tier to reach and chop from then on.
//	fare        int    default 60       — coins for the two boat fares.
//	pickpocketTarget string default "Man"
//	pickpocketTile   tile   default the Lumbridge castle courtyard
//	restBelowHp int    default 40       — while funding the fare, rest below this HP%.
//	guideNpc    string default "RuneScape Guide"
//	skipOption  string default "Yes please"
//	mule        string default "Mulecon" — the MuleReceiver account; blank never hands over.
//	muleAt      int    default 1000     — target-species logs (bank + pack) that start a hand-over.
//	muleKeep    string default "Coins"  — bank rows that never go to the mule, beyond axes (always kept).
//	muleAtTile  tile   default the Seers bank stand — where the MuleReceiver posts. THE FLEET SETS
//	                                      THIS: a mule posted anywhere else is never met.
//	logDetail   string default "Normal" — "Verbose" logs every phase change and step.
//	axeWanted   string default "Rune axe" — the axe to work toward. Any axe chops at its own
//	                                        speed from Woodcutting 1 here, so this is an
//	                                        output choice, not a level gate.
//	axePrice    int    default 0        — expected coins per axe. ZERO NEVER BUYS. The control
//	                                      panel's Seers market panel shows the market's own
//	                                      last-traded figure, which is what to set this from.
//	axeBudget   int    default 0        — hard ceiling on coins spent on one axe. ZERO NEVER BUYS.
//	marketStand tile   default the Seers bank — where the market character stands.
//	marketSeller string default ""      — when set, the only account a market trade completes with.
//	bobStand    tile   default the Lumbridge store stand — Bob's Axes, the fallback.
type woodcutterBot struct {
	settings woodcutplan.Settings

	// fastAxeTiers opts this account into the owner's rune-axe exemption: with a rune axe the
	// ladder gates on the content requirement instead of the raised StartAt, so Yew opens at
	// 60 and Magic at 75 (woodcutplan.CurrentTierIndexWith). ProgressiveWoodcutter turns it on
	// in OnStart; wcfletchBot embeds this type and turns it back OFF, because its own pairing
	// logic is asserted against the raised gates.
	fastAxeTiers bool

	phase     woodcutplan.WoodPhase
	havePhase bool

	// peerWatch and lastBankItems are fleet-wide peer item sharing (woodcutterpeer.go).
	// lastBankItems is this account's most recent bank reading, kept so a peer can be served
	// without opening the bank again to find out whether there is anything spare.
	peerWatch     *peerrunner.Watcher
	lastBankItems []gamestate.SlotItem

	thief   *freshstartrunner.Thief
	sail    *freshstartrunner.SailProgress
	bright  *freshstart.Brightness
	refused map[string]bool

	bankRead       bool
	bankAxes       []string
	bankTargetLogs int

	orderedAt time.Time
	asks      int

	// The axe upgrade and this script's own random events — see woodcutteraxe.go. axeNext is
	// the cooldown after an attempt that bought nothing; event is the last one logged, so a
	// repeating event does not repeat its line; the rest are counters for the stop line.
	event         woodcutplan.WoodEvent
	axeHeadsFixed int
	axesRepaired  int
	// brokenReplaced counts broken axes the mule replaced instead of Bob (woodcuttermuleaxe.go).
	brokenReplaced int
	repairSpent    int
	axeNext        time.Time
	axesBought     int
	axeSpent       int
	coinAsks       int
	coinsGot       int

	// axeAsk is this account's memory of asking the mule for an AXE (woodcutterhandout.go).
	// Lazily allocated, matching fisherBot.ask.
	axeAsk *axeAsk

	// muleNoAxe and marketNo are what each SOURCE has said it cannot supply, so a question
	// already answered is not asked again (woodcutplan.AxeUnavailable, whose zero value is
	// usable and whose every method is nil-safe, so neither needs initialising anywhere).
	//
	// THE FLEET OWNER'S REPORT, 2026-09-22: "woodcutters are asking the mule for axes they dont
	// even have instock". Both answers were always truthful and both were thrown away — the
	// mule's "none in the bank" (orderplan.Decide, after really reading its bank) and the
	// stall's "it cannot sell one" (woodcutrunner.awaitSellerReply) — so the same questions
	// were asked again on the next cooldown, forever, with an 83-tile walk attached to the
	// first one.
	//
	// TWO MEMORIES, NEVER ONE SHARED. "The mule has no mithril axe" and "the stall will not
	// sell a mithril axe" are different facts with different remedies, and measured on
	// 2026-09-22 they genuinely disagreed: the mule held no mithril axe at all while the Seers
	// market had nine at 600 coins. One map would have let the mule's shortage veto the
	// purchase that was sitting right there.
	muleNoAxe woodcutplan.AxeUnavailable
	marketNo  woodcutplan.AxeUnavailable
	// marketLosses counts stall turns lost in a row, for woodcutplan.MarketBusyBackoff. Reset by
	// any trip that actually got the turn. See woodcutteraxe.go.
	marketLosses int

	handoffPending bool
	handoffNext    time.Time
	handoffs       int
	handedOver     int

	// The Draynor start camp (woodcutplan/startcamp.go, woodcutrunner/startcamp.go).
	//
	// startCampOff latches the camp off for the REST of this run once it cannot be made to
	// work, which today means only one thing: no knife. A knife ground spawn is shared, so
	// another account can be holding the only one, and without a knife the camp's whole
	// premise — 28 slots of logs collapsing into one stacked slot of arrow shafts — is gone
	// and the account would fill its pack at a camp 131 tiles from any bank it knows. Latched
	// rather than re-tried every pass so the fallback is a decision taken once, logged once,
	// and visible in the stop line; the account then runs the Seers ladder that was already
	// proven live.
	startCampOff bool
	knifeFails   int
	campLogs     int
	campShafts   int

	// blocks are trees an Ent was seen at, excluded until the exclusion expires on its own.
	// Owned here rather than rebuilt per Loop call, because the whole point is that it outlives
	// the click that noticed the Ent: the engine keeps the player on the Ent until this client
	// does something else, and the Ent stands there for 60 ticks.
	blocks   woodcutplan.TreeBlocks
	entsSeen int
	// stuck counts consecutive chop passes that found trees in reach and chopped none, so an
	// account away from its camp's anchor walks back to it instead of waiting forever where it
	// stands (woodcutplan.DecideStuck). cutOffs counts the "cut-off" escapes, for the stop line.
	stuck   woodcutplan.StuckRun
	cutOffs int

	// walker plans every long leg over webwalk's door/gate/stair/ship-fare graph. Built once
	// in OnStart, exactly as internal/bot/fisher.go and internal/bot/mule.go already do, and
	// nil when live.NewWalker fails — in which case every walk degrades to the plain
	// pathfind this script used to do everywhere.
	//
	// This is what makes Bob's Axes reachable at all. Until 2026-09-20 nothing in the
	// woodcutter's axe path used webwalk, so a Seers -> Lumbridge leg was one A* across the
	// collision pack, came back "unreachable", and then thrashed doors until it timed out.
	walker *webwalk.Walker

	chopped, trips, soldGP int
	startedAt              time.Time
	xpAtStart              int
	warnedAt               map[string]time.Time

	// loopWaits is the memory of the two waits at the top of Loop (loopgate.go), shared by
	// ProgressiveWCFletch through the embedding. Not called "gate": wcfletchBot's hand-off work
	// (claude/handoff0923) adds its own gate field, which would shadow this one.
	loopWaits loopGate
}

func (b *woodcutterBot) deps(ctx *Context) *woodcutrunner.Deps {
	return &woodcutrunner.Deps{
		Session:    ctx.Session,
		Pack:       ctx.Pack,
		Locs:       ctx.Locs,
		Config:     ctx.Config,
		Components: ctx.Components,
		Chat:       ctx.Chat,
		Delay:      ctx.Delay,
		Log:        ctx.Log,
		Loot:       ctx.Loot,
		Blocks:     &b.blocks,
		Walker:     b.walker,
		// The tree-claim registry is PER WORLD: two accounts on different worlds standing at
		// the same tile are different players who cannot take each other's tree, so they must
		// not de-conflict against one another. See woodcutplan.SharedTreeClaimsFor.
		World: ctx.World(),
	}
}

func (b *woodcutterBot) fresh(d *woodcutrunner.Deps) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay), Log: d.Log,
		Walker: d.Walker,
	}
}

func woodcutterSettings(m map[string]interface{}) woodcutplan.Settings {
	d := woodcutplan.DefaultSettings()
	s := d
	s.Target = StringSetting(m, "target", d.Target)
	s.Fare = IntSetting(m, "fare", d.Fare)
	s.PickpocketTarget = StringSetting(m, "pickpocketTarget", d.PickpocketTarget)
	s.PickpocketTile = fisherTileSetting(m, "pickpocketTile", d.PickpocketTile)
	s.RestBelowHP = IntSetting(m, "restBelowHp", d.RestBelowHP)
	s.GuideNPC = StringSetting(m, "guideNpc", d.GuideNPC)
	s.SkipOption = StringSetting(m, "skipOption", d.SkipOption)
	if v, ok := m["mule"].(string); ok {
		s.Mule = strings.TrimSpace(v)
	}
	s.MuleAt = IntSetting(m, "muleAt", d.MuleAt)
	if v, ok := m["muleKeep"].(string); ok {
		s.MuleKeep = v
	}
	s.LogDetail = StringSetting(m, "logDetail", d.LogDetail)
	s.MuleStand = fisherTileSetting(m, "muleAtTile", d.MuleStand)
	s.StartCamp = BoolSetting(m, "startCamp", d.StartCamp)
	// Buying an axe — off unless both a price and a budget are set. See woodcutteraxe.go.
	s.AxeWanted = StringSetting(m, "axeWanted", d.AxeWanted)
	s.AxePrice = IntSetting(m, "axePrice", d.AxePrice)
	s.AxeBudget = IntSetting(m, "axeBudget", d.AxeBudget)
	s.MarketStand = fisherTileSetting(m, "marketStand", d.MarketStand)
	s.MarketSeller = StringSetting(m, "marketSeller", d.MarketSeller)
	s.BobStand = fisherTileSetting(m, "bobStand", d.BobStand)
	return s.Normalised()
}

func (b *woodcutterBot) OnStart(ctx *Context) error {
	// The owner's rune-axe exemption is on for the plain woodcutter. wcfletchBot clears it
	// again after calling this; see woodcutplan.CurrentTierIndexWith for why.
	b.fastAxeTiers = true
	if ctx.Pack == nil {
		return fmt.Errorf("woodcutter: no collision pack configured (fleet.config.json needs a packPath) — refusing to start rather than panic later inside pathfinding")
	}
	if ctx.Locs == nil {
		return fmt.Errorf("woodcutter: no loc index configured (fleet.config.json needs a locsPath) — refusing to start rather than panic later looking up trees/the bank")
	}
	if ctx.Components == nil {
		return fmt.Errorf("woodcutter: no interface archive Components configured — refusing to start rather than panic later opening the bank")
	}
	b.settings = woodcutterSettings(ctx.Settings)
	b.peerWatch = NewPeerWatcher()
	// Spread this account's hand-off threshold across the cohort, exactly as
	// internal/bot/fisher.go and internal/bot/fisherguild.go already do. This script did not,
	// and the omission bites harder here than it does for the fisher: a woodcutter cohort is
	// normally started together at Woodcutting 1 and climbs the same ladder at the same rate,
	// so without jitter every account crosses muleAt logs within minutes of every other one
	// and arrives at a mule that trades with one partner at a time. Applied here rather than
	// inside woodcutterSettings so that function stays a pure reading of the settings map.
	// Derived from the account name, so it is stable across restarts — see jitterMuleAt.
	b.settings.MuleAt = jitterMuleAt(b.settings.MuleAt, ctx.Username)
	b.thief = freshstartrunner.NewThief()
	b.sail = freshstartrunner.NewSailProgress()
	b.bright = &freshstart.Brightness{}
	b.refused = map[string]bool{}
	b.warnedAt = map[string]time.Time{}
	b.havePhase = false
	b.startedAt = time.Now()
	// The long-distance walker, before the first deps() that wants it. Same construction and
	// the same hops.Install as ProgressiveFisher/MuleReceiver/BarbarianAgility: without the
	// NPC-hop handler the Walker cannot plan a ship fare, and the Seers <-> Lumbridge leg
	// this script needs for Bob's Axes is exactly a ship-fare chain
	// (Ardougne <-> Brimhaven <-> Karamja <-> Port Sarim).
	if w, err := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); err != nil {
		ctx.logf("woodcutter: no webwalk (%v) — long legs fall back to the plain walker, and Bob's Axes will be out of reach", err)
	} else {
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{
				Client: w.Client, Session: ctx.Session, Config: ctx.Config,
				Components: ctx.Components, Log: ctx.Log,
			}))
		}
		b.walker = w
	}
	b.xpAtStart = woodcutrunner.WoodcuttingXP(b.deps(ctx))

	mule := "no mule"
	if b.settings.Mule != "" {
		mule = fmt.Sprintf("logs to %s at %d %s (keep %q)", b.settings.Mule, b.settings.MuleAt, strings.ToLower(woodcutplan.TargetTier(b.settings.Target).Logs), b.settings.MuleKeep)
	}
	camp := "start camp off (straight to Seers)"
	if b.settings.StartCamp {
		dc := woodcutplan.DraynorTier()
		camp = fmt.Sprintf("Draynor start camp on until Woodcutting %d (%d,%d)",
			woodcutplan.TIERS[woodcutplan.TargetIndex("Oak")].StartAt, dc.Anchor.X, dc.Anchor.Z)
	}
	ctx.logf("woodcutter: target %s, fare %d, %s, %s", b.settings.Target, b.settings.Fare, mule, camp)
	return nil
}

func (b *woodcutterBot) vlog(ctx *Context, format string, args ...any) {
	if strings.EqualFold(b.settings.LogDetail, "Verbose") {
		ctx.logf("woodcutter: "+format, args...)
	}
}

func (b *woodcutterBot) warn(ctx *Context, key string, everyMs time.Duration, format string, args ...any) {
	if time.Since(b.warnedAt[key]) < everyMs {
		return
	}
	b.warnedAt[key] = time.Now()
	ctx.logf("woodcutter: WARNING: "+format, args...)
}

// snapshot builds woodcutplan.WoodSnapshot from live state, ported from
// ProgressiveWoodcutter.ts's own snapshot(). ok is false until the tile is known.
func (b *woodcutterBot) snapshot(ctx *Context, d *woodcutrunner.Deps) (woodcutplan.WoodSnapshot, bool) {
	st := ctx.Session.State()
	x, z, level, tileOK := st.Tile()
	if !tileOK {
		return woodcutplan.WoodSnapshot{}, false
	}
	// Refuse to decide a phase while the backpack and worn panels cannot be read at all. Every
	// axe this script wields lives in the worn panel, which resolves through sidebar tab 4, and
	// content nulls tabs 0-6 for the whole of a macro event (~macro_events_set_tabs) and at the
	// start of every ship crossing (~set_sail, ~set_sail_cairn) — and this script sails twice on
	// its way to Seers. The crossing's window is NOT the whole voyage, which this comment used to
	// claim: ~initalltabs re-transmits and re-assigns tabs 3 and 4 before the arrival telejump,
	// so it is the route's own p_delay, while a mime restores nothing until its mapzoneexit.
	// Shorter is not harmless — a WaitForGuardian return was measured with each window still
	// open. Read blind, a wielding account looks axeless, and DecideWoodPhase answers
	// that with a bank trip, a redundant withdrawal, or PhaseOrder, which puts a rune-axe order on
	// the mule bus that no later pass takes back. Returning "not ready" costs one 1200ms delay and
	// the next pass re-derives everything, which is the same answer this function already gives
	// for a position it does not know yet.
	if !woodcutrunner.ContainersReadable(d) {
		return woodcutplan.WoodSnapshot{}, false
	}
	main, _, _ := st.Modals()
	here := tile.Tile{X: x, Z: z, Level: level}
	s := woodcutplan.WoodSnapshot{
		MainModal:     main,
		Tile:          &here,
		Level:         woodcutrunner.WoodcuttingLevel(d),
		Target:        b.settings.Target,
		Coins:         woodcutrunner.CoinsHeld(d),
		Fare:          b.settings.Fare,
		Sellable:      len(freshstart.SellQueue(woodcutrunner.PackView(d), b.storeKeep(), b.refused)),
		Bright:        !freshstartrunner.BrightPending(b.fresh(d), b.bright),
		AxesHeld:      woodcutrunner.HeldAxes(d),
		RuneAxeNoted:  woodcutrunner.RuneAxeNoted(d),
		BankAxes:      b.bankAxes,
		BankRead:      b.bankRead,
		PackFull:      woodcutrunner.PackFull(d),
		HasMule:       b.settings.Mule != "",
		AxeUpgradeDue: b.axeUpgradeDue(ctx, d),
		FastAxeTiers:  b.fastAxeTiers,
		StartCamp:     b.startCampOn(),
	}
	return s, true
}

// axeUpgradeDue reports whether a better axe than the one in hand can actually be obtained
// right now. It is what lets seersPhase reach PhaseOrder for an account that already owns an
// axe -- which it could not do at all before 2026-09-21, so twelve live woodcutters carried the
// mithril axe the mule handed them while the Seers market listed 94 rune axes.
//
// "Actually obtainable" is the whole point, and it is DecideAxeBuy that decides it: nothing is
// due when what is held already meets the target, when no target is configured, or when the
// purchase is switched off (axePrice or axeBudget at zero, where zero never buys). A bank axe
// is deliberately NOT counted here -- seersPhase checks that first and PhaseBank is the cheaper
// answer -- so this only ever fires for something that has to be bought.
func (b *woodcutterBot) axeUpgradeDue(ctx *Context, d *woodcutrunner.Deps) bool {
	// THE MULE'S OWN STOCK FIRST (2026-10-04): the mule holds Rune axes for this cohort, so an
	// account chopping with a worse axe is due an upgrade whenever the mule's published bank
	// shows a better one, priced or not. Gated by axeNext like the purchase below, and known
	// stock only, so a mule without one never sends anybody walking. See woodcuttermuleaxe.go.
	if held := woodcutrunner.HeldAxes(d); len(held) > 0 && !time.Now().Before(b.axeNext) &&
		b.muleUpgradeAvailable(ctx, woodcutplan.BestAxe(held)) {
		return true
	}
	if b.settings.AxePrice <= 0 || b.settings.AxeBudget <= 0 {
		return false
	}
	// AN UPGRADE NEVER OUTRANKS CHOPPING WITH THE AXE ALREADY IN HAND.
	//
	// The first version of this returned true whenever a better axe existed, and it put twelve
	// live woodcutters into a loop within minutes (2026-09-21): PhaseOrder walks to the mule and
	// asks for the best axe it stocks, the mule is out of everything above bronze, the ask walks
	// back down the ladder, and the next pass asks again -- 104 tiles each way, zero experience
	// gained by any of the twelve. The log line that gave it away reads "handed over a Bronze
	// axe -- back to work with [Steel axe Bronze axe]" immediately followed by "no axe anywhere
	// -- asking the mule for a Mithril axe".
	//
	// Two conditions close it. An account that already HOLDS an axe can chop right now, so its
	// upgrade is worth exactly one attempt per axeRetry rather than every pass; axeNext is the
	// same cooldown axeStep already sets when a purchase cannot be completed. And the mule is
	// asked for an upgrade only when its published bank shows one (the check above, added
	// 2026-10-04 when rune axes joined its catalogue); otherwise an axe-holder goes to the
	// market, which is axeStep's business.
	if len(woodcutrunner.HeldAxes(d)) > 0 && time.Now().Before(b.axeNext) {
		return false
	}
	// The SAME ladder axeStep will buy from, not the single configured tier. Without this the
	// phase machine keeps answering "an upgrade is due" for a rune axe the stall has already
	// said it has none of, so the account routes to a purchase that cannot happen -- the
	// 2026-09-22 acct134 shape. woodcutplan.AxeTargets + marketNo is the one place that knows
	// which tiers are still worth a trip.
	now := time.Now()
	targets := woodcutplan.AxeTargets(b.settings.AxeWanted, b.settings.PriceOfAxe, b.marketSkip(now))
	buy := woodcutplan.DecideAxeBuyAmong(
		woodcutrunner.HeldAxes(d), b.bankAxes, targets, woodcutrunner.CoinsHeld(d),
		b.settings.PriceOfAxe)
	return buy.Want != "" && buy.From != woodcutplan.AxeFromBank
}

// askMuleForKnife walks to the mule's post and asks it for a Knife, returning true only once
// one is confirmed in this account's own pack.
//
// The walk is not optional and the reason is already written down twice in this file: a hand-out
// is a TRADE, mulerunner.Handout resolves the donor's player slot before drawing anything, and
// it declines outright when it cannot see them. Both the axe hand-out and (as of today) the coin
// ask learned that the hard way in production.
func (b *woodcutterBot) askMuleForKnife(ctx *Context, d *woodcutrunner.Deps) bool {
	if b.settings.Mule == "" || ctx.Bus == nil {
		return false
	}
	stand := b.settings.MuleStand
	if tile.Chebyshev(currentTileOf(d), stand) > muleMeetRadius {
		if !woodcutrunner.TravelTo(d, stand, muleMeetRadius) {
			b.warn(ctx, "knife-walk", 60*time.Second,
				"could not reach %q's post at %d,%d to be handed a knife", b.settings.Mule, stand.X, stand.Z)
			return false
		}
	}
	ctx.logf("woodcutter: no knife on the ground — asking %q for one", b.settings.Mule)
	res := donorrunner.Await(&donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}, donorrunner.AwaitOptions{
		Mule: b.settings.Mule, Me: ctx.Username, Item: woodcutplan.Knife, Qty: 1, Bus: ctx.Bus,
	})
	if !res.Got {
		ctx.logf("woodcutter: %q did not hand over a knife (%s)", b.settings.Mule, res.Reason)
		return false
	}
	// Asked and answered is not the same as holding one: read the pack.
	if !woodcutrunner.HasKnife(d) {
		ctx.logf("woodcutter: %q reported handing a knife over but none is in the pack", b.settings.Mule)
		return false
	}
	ctx.logf("woodcutter: %q handed over a knife — the start camp can fletch", b.settings.Mule)
	return true
}

// startCampOn is whether the Draynor start camp is available right now: asked for in the
// settings and not latched off by a failure this run.
func (b *woodcutterBot) startCampOn() bool { return b.settings.StartCamp && !b.startCampOff }

// storeKeep is what the Lumbridge general store never sees. With the start camp on, every axe
// is kept back — the camp chops before it travels, so selling the starter bronze axe would
// leave it with nothing to chop with and send it straight past the camp to PhaseFund.
func (b *woodcutterBot) storeKeep() []string {
	return woodcutplan.StoreKeepFor(b.startCampOn())
}

func (b *woodcutterBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !woodcutrunner.WaitForGuardian(d) {
		return nil
	}
	// Answer at most one peer request per pass, before this account's own work. See
	// woodcutterpeer.go -- with 94 woodcutters all banking at Seers, the account holding the
	// spare axe is usually standing next to the one that needs it.
	b.servePeersOnce(ctx)
	if ctx.Session.State().IsDead() {
		if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
			ctx.Session.State().ClearDead()
		}
	}
	// Both waits used to be silent and unbounded; see loopgate.go for the live twenty-minute
	// silence the second one caused.
	if b.loopWaits.holdForTrade(ctx, "woodcutter") {
		return nil
	}
	s, ok := b.snapshot(ctx, d)
	if !ok {
		b.loopWaits.refused(ctx, d, "woodcutter")
		return nil
	}
	b.loopWaits.readable(ctx, "woodcutter")
	// FUND THE CAMP FARE BEFORE DECIDING ANYTHING ELSE.
	//
	// A sub-oak account on the Seers side belongs at the Draynor camp -- 56 dead trees against
	// Seers' twelve, plus the knife and the fletching -- and the ONLY thing keeping it here is
	// the ship fare. Left alone it stays on a ladder that starves it: measured on the live
	// fleet, 81 accounts below the oak gate averaged 122 Woodcutting xp against 6,580 for the
	// 13 above it.
	//
	// So go and get the money, in the fleet owner's own order (2026-09-21): "have them check
	// the bank for coins, if no coins for travel, grab from the mule". That is exactly what
	// askMuleForCoins already does for the axe -- its own Seers bank first, which is a few
	// tiles, and the mule only for the shortfall, which is 83.
	//
	// Bounded by the same axeAsk cooldown the axe hand-out uses, so an account the mule cannot
	// fund right now chops on and tries later rather than standing at the post. The fare is
	// tiny next to an axe, so this rarely competes with the 150,000-coin asks.
	if need, want := woodcutplan.NeedsCampFare(s); want && b.askMuleForAnAxeReady() {
		if b.askMuleForCoins(ctx, d, "start camp fare", need) {
			ctx.logf("woodcutter: funded the %d-coin fare for the Draynor camp", need)
			return nil
		}
	}

	phase := woodcutplan.DecideWoodPhase(s)
	if !b.havePhase || phase != b.phase {
		b.phase, b.havePhase = phase, true
		ctx.logf("woodcutter: phase: %s", woodcutplan.DescribeWoodPhase(phase))
	}
	// Level-ups and the like; the tutorial's own dialogue is the tutorial step's to answer.
	if phase != woodcutplan.PhaseAccept && phase != woodcutplan.PhaseTutorial {
		if dlg := actions.NewChatDialog(ctx.Session, ctx.Components); dlg.CanContinue() {
			_ = dlg.Continue()
			ctx.Delay(600 * time.Millisecond)
			return nil
		}
	}

	// This script's OWN random events, ahead of the ordinary dispatch. The shared Guardian
	// (WaitForGuardian, at the top of this Loop) already covers the Tree spirit —
	// internal/randomevents carries npc 438-443 and scopes the flee to the account the monster
	// is actually targeting — so only the two it does not know about are handled here. The axe
	// head in particular cannot wait for a convenient moment: it is on a ~2 minute despawn.
	// See internal/scripts/woodcutplan/events.go for the content evidence behind all three.
	if ev := woodcutplan.DecideWoodEvent(woodcutrunner.EventSnapshot(d, b.bankAxes)); ev != woodcutplan.EventNone {
		b.eventStep(ctx, d, ev)
		return nil
	}

	if manual := ctx.TakeHandoffRequest(); manual {
		b.handoffPending = true
	}
	if b.settings.Donor().Enabled() && b.handoffPending {
		ctx.SetPhase(phaselog.Handoff)
		return b.handoffStep(ctx, d)
	}

	// Report what this step is, for the control panel's time split. Reporting only: it never
	// changes what the script does. Until this existed ProgressiveWoodcutter reported nothing
	// at all, so the panel's "where the time goes" read 0% on every phase for it — including
	// its own chopping — while the accounts were visibly gaining levels.
	ctx.SetPhase(woodcutterPhaseName(phase))

	fd := b.fresh(d)
	switch phase {
	case woodcutplan.PhaseAccept:
		freshstartrunner.AcceptAppearance(fd)
	case woodcutplan.PhaseTutorial:
		freshstartrunner.SkipTutorial(fd, b.settings.GuideNPC, b.settings.SkipOption)
	case woodcutplan.PhaseBright:
		freshstartrunner.BrightnessStep(fd, b.bright)
	case woodcutplan.PhaseWait:
		ctx.Delay(1200 * time.Millisecond)
	case woodcutplan.PhaseStore:
		b.storeStep(ctx, d)
	case woodcutplan.PhaseStart:
		b.startCampStep(ctx, d, s)
	case woodcutplan.PhaseFund:
		freshstartrunner.PickpocketStep(fd, b.thief, freshstartrunner.PickpocketOpts{
			Target: b.settings.PickpocketTarget, Spot: b.settings.PickpocketTile,
			RestBelow: b.settings.RestBelow(), Need: freshstart.FareRemaining(s.Tile, b.settings.Fare),
		})
	case woodcutplan.PhaseTravel:
		freshstartrunner.SailLeg(fd, woodcutplan.Seers, b.sail)
	case woodcutplan.PhaseBank:
		b.bankStep(ctx, d)
	case woodcutplan.PhaseOrder:
		b.orderStep(ctx, d)
	case woodcutplan.PhaseWalk:
		tier := woodcutplan.CurrentTierWith(s.Level, s.Target, s.FastAxeTiers && woodcutplan.FastAxeHeld(s.AxesHeld))
		radius := tier.Leash
		if radius > 3 {
			radius = 3
		}
		if !woodcutrunner.WalkNear(d, tier.Anchor, radius) {
			ctx.Delay(2 * time.Second)
		}
	case woodcutplan.PhaseChop:
		b.chopStep(ctx, d, s)
	case woodcutplan.PhaseStuck:
		b.warn(ctx, "stuck", 120*time.Second, "no axe in the pack, worn or in the bank, and no mule to ask — put an axe in the Seers bank, or name a mule in the settings")
		ctx.Delay(3 * time.Second)
	}
	return nil
}

// ---- the general store ----

// woodcutterPhaseName maps a WoodPhase onto internal/phaselog's canonical names, mirroring
// fisher.go's own fisherPhaseName so the two scripts' timelines are comparable rather than each
// inventing its own vocabulary. The mapping follows what the account is actually DOING, not
// which branch of the switch runs it:
//
//   - PhaseWalk is the walk out to the tier's anchor, so it is walking, exactly as a fisher's
//     PhaseTravel is.
//   - PhaseFund is pickpocketing for a boat fare: the account is broke or displaced, not
//     woodcutting, which is what fisher.go calls recovering.
//   - PhaseOrder is asking the mule for an axe. That is the hand-out side of a hand-off, and
//     phaselog already has the name the mule's own side uses for it.
//   - PhaseStuck has no axe anywhere and no mule to ask: stranded, not idle, because nothing
//     it does by itself will fix it.
func woodcutterPhaseName(p woodcutplan.WoodPhase) string {
	switch p {
	case woodcutplan.PhaseChop:
		return phaselog.Woodcutting
	case woodcutplan.PhaseBank:
		return phaselog.Banking
	case woodcutplan.PhaseWalk, woodcutplan.PhaseTravel:
		return phaselog.Walking
	case woodcutplan.PhaseOrder:
		return phaselog.Handout
	case woodcutplan.PhaseFund:
		return phaselog.Recovering
	case woodcutplan.PhaseStuck:
		return phaselog.Stranded
	case woodcutplan.PhaseWait:
		return phaselog.Idle
	default: // accept, tutorial, brightness, starter store, start camp
		return phaselog.Setup
	}
}

func (b *woodcutterBot) storeStep(ctx *Context, d *woodcutrunner.Deps) {
	res := woodcutrunner.StoreTrip(d, freshstart.StoreStand, b.storeKeep(), b.refused)
	if !res.Opened {
		return
	}
	b.soldGP += res.GP
	ctx.logf("woodcutter: sold the starter kit: %d items for %d gp (%d coins)", res.Sold, res.GP, woodcutrunner.CoinsHeld(d))
}

// ---- the Draynor start camp ----

// maxKnifeFails is how many knife-collection attempts the camp gets before it is turned off for
// the rest of the run.
//
// Three, because the two failures that matter are different in kind and only one of them is
// worth retrying. "Could not reach the spawn" is a walk that can succeed next time (a door, a
// crowded tile, an NPC standing on the square). "No knife on the ground at the spawn" means
// another account is holding the only one, and content respawns a ground spawn on its own
// timer — so a couple of retries is right and a loop is not. Past that the camp cannot run,
// and pretending otherwise would leave the account chopping into a pack it cannot empty.
const maxKnifeFails = 3

// startCampStep runs one step of the Draynor Manor start camp: a fresh account chops dead trees
// beside the manor and fletches every log into arrow shafts, never banking and never sailing,
// until it reaches the ladder's own oak gate.
//
// The decision is woodcutplan.DecideStartPhase's, from live state alone, exactly as every other
// phase in this file works. What this function adds is the two things a pure decision cannot
// know: whether an attempt actually succeeded, and when to give the camp up.
//
// Why the camp exists at all, measured rather than assumed (COLLAB_ARCHIVE.md, session
// "woodlive"): the Seers normal-tree camp holds 12 trees, a normal tree yields exactly ONE log
// and is then a stump for 120-199 ticks, and it is winner-takes-all — so that camp's whole
// supply feeds 2.0 accounts. A cohort started together at Woodcutting 1 therefore shares a
// 2-account camp and, at the 6 xp/account/min that was measured there with 100 accounts, never
// levels out of it to reach a camp that could feed it. Draynor has 56 dead trees inside the same
// six-tile leash, 47 of them with a tile the engine accepts a "Chop down" from.
func (b *woodcutterBot) startCampStep(ctx *Context, d *woodcutrunner.Deps, s woodcutplan.WoodSnapshot) {
	camp := woodcutplan.DraynorTier()
	ss := woodcutrunner.StartSnapshot(d, s.Level, s.Target)
	switch woodcutplan.DecideStartPhase(ss) {
	case woodcutplan.StartKnife:
		res := woodcutrunner.TakeKnife(d)
		if res.Got {
			b.knifeFails = 0
			return
		}
		if res.Stopped {
			return
		}
		b.knifeFails++
		if b.knifeFails >= maxKnifeFails {
			// ASK THE MULE BEFORE GIVING THE CAMP UP. The spawn failing is not the same as
			// there being no knife to be had: there are exactly TWO in the world
			// (woodcutplan.KnifeSpawns), and the more accounts run this camp the more often
			// another one has already taken the nearer of them. At the cohort size the fleet
			// users asked for -- one woodcutter per rune axe on the Seers market, 94 of them --
			// nearly every account would lose that race, quietly drop its camp, and land on the
			// Seers normal camp whose own supply feeds 2.0 accounts. The camp would not look
			// broken; it would just never be used.
			//
			// So the mule is the supply for a cohort and the spawn is the free shortcut for a
			// single account. A knife is one per account and never consumed, which is why it is
			// capped at 1 in orderplan's catalogue.
			if b.askMuleForKnife(ctx, d) {
				b.knifeFails = 0
				return
			}
			b.startCampOff = true
			ctx.logf("woodcutter: the Draynor start camp needs a knife, %s (%d attempts), and %q could not supply one either — giving the camp up for this run and going to Seers instead",
				res.Reason, b.knifeFails, b.settings.Mule)
			return
		}
		b.warn(ctx, "camp-knife", 30*time.Second, "could not get a knife for the start camp: %s (attempt %d of %d)", res.Reason, b.knifeFails, maxKnifeFails)
		ctx.Delay(3 * time.Second)

	case woodcutplan.StartAxe:
		// DecideWoodPhase only routes here while an axe is held, so this is an axe lost DURING
		// the camp — a broken one, or a head that flew off and could not be recovered. Nothing
		// at Draynor can replace it: Bob's is the only shop, and the account's own bank is at
		// Seers. Hand back to the ladder, which knows how to do both.
		b.startCampOff = true
		ctx.logf("woodcutter: no axe left at the Draynor start camp — leaving the camp and going to Seers, which can reach a bank and Bob's")

	case woodcutplan.StartWalk:
		if !woodcutrunner.TravelTo(d, camp.Anchor, 2) {
			b.warn(ctx, "camp-walk", 60*time.Second, "could not reach the Draynor start camp at %d,%d — retrying", camp.Anchor.X, camp.Anchor.Z)
			ctx.Delay(3 * time.Second)
		}

	case woodcutplan.StartFletch:
		res := woodcutrunner.FletchShafts(d)
		if res.Made {
			b.campShafts += res.Shafts
			return
		}
		// A fletch that made nothing with a FULL pack is the one state the camp cannot chop
		// its way out of, so it is the second give-up. Anything else just costs a pass.
		if ss.PackFull {
			b.startCampOff = true
			ctx.logf("woodcutter: the start camp's pack is full and fletching made nothing (%s) — leaving the camp for the Seers bank", res.Reason)
			return
		}
		b.warn(ctx, "camp-fletch", 30*time.Second, "fletching made no arrow shafts (%s) — carrying on chopping", res.Reason)

	case woodcutplan.StartBank:
		// The camp cannot work with a full pack it cannot fletch, and it cannot empty one:
		// there is nothing here to deposit into that the ladder does not already own. Hand back
		// to Seers, whose very first step is a bank trip that deposits everything.
		b.startCampOff = true
		ctx.logf("woodcutter: the start camp's pack is full of things it cannot fletch — leaving the camp for the Seers bank to empty it")

	case woodcutplan.StartChop:
		res := woodcutrunner.ChopDraynor(d)
		b.chopped += res.Gained
		b.campLogs += res.Gained
		b.chopOutcome(ctx, d, camp, res)

	case woodcutplan.StartDone:
		// AtOakGate is also DecideWoodPhase's own gate, so this is unreachable from the phase
		// machine. Kept because DecideStartPhase can return it and a silent default would hide
		// a real disagreement between the two.
		ctx.logf("woodcutter: the Draynor start camp is done at Woodcutting %d — heading for Seers", s.Level)
	}
}

// ---- getting an axe ----

// orderStep runs when the account has no axe anywhere it can reach one. It walks back to the
// Seers bank and hands over to axeStep (woodcutteraxe.go), which asks the mule for coins and
// buys an axe with them.
//
// This USED to publish a mulebus.OrderMessage for a rune axe and warn that nothing would answer
// it, because MuleReceiver did not read KindOrder. That is no longer true — internal/bot/mule.go
// now handles KindOrder via mulerunner.Handout — but asking the mule for an AXE is still the
// wrong request, because a fleet mule's bank holds coins and loot, not rune axes, and no NPC
// shop on this server can restock one. Asking for the COINS and buying the axe is what the
// users asked for and what actually works. See woodcutteraxe.go's own header.
func (b *woodcutterBot) orderStep(ctx *Context, d *woodcutrunner.Deps) {
	// ASK THE MULE FOR AN AXE FIRST, AND WALK TO ITS POST TO DO IT. This is the one path that
	// works with nobody watching, and until 2026-09-21 it did not exist: axeStep below asks for
	// COINS and then buys at the Seers PLAYER market -- a BOT stall that is always there, so
	// this path is autonomous after all (see market.go; an earlier comment here said it needed
	// a human and that was wrong) -- and
	// axePrice/axeBudget default to 0 where zero never buys. Five production accounts parked on
	// "waiting for the mule to bring a rune axe" while the mule held six mithril axes.
	//
	// THE WALK IS NOT OPTIONAL, AND THAT WAS MEASURED. A hand-out is a TRADE, so the two
	// accounts have to be standing together: mulerunner.Handout resolves the donor's player
	// slot before it draws anything and declines outright when it cannot see them. This step
	// used to walk to woodcutplan.Seers, which is right for the market path below and wrong for
	// this one — the fleet's mule posts at Catherby, 83 tiles from the Seers bank, so every
	// order came back "you are not in sight of me" (production, 2026-09-21). donorrunner.Await
	// does not travel; the caller has to be there already, which is exactly why
	// ProgressiveFisher's own ask works — it fishes where its mule stands.
	//
	// Gated on askMuleForAnAxeReady so a cooled-down or unconfigured ask never walks the
	// account across the world for a question it is not going to ask.
	//
	// AND ONLY WHEN THE ACCOUNT IS ACTUALLY AXELESS. (Until 2026-10-04 the mule's catalogue
	// stopped at a mithril axe, so it could not supply an UPGRADE; an axe-holder now asks it
	// below, through upgradeFromMule, and only for an axe its published bank shows.) Asking anyway is what put twelve live
	// woodcutters into a 104-tile round trip per pass, asking for axes the mule did not have
	// and walking back to ask again, at zero experience (2026-09-21). An axe-holder that wants
	// better belongs in axeStep below, which buys.
	// ASK THE FLEET BEFORE THE MULE. Every woodcutter banks at Seers, so an account holding a
	// superseded axe is a few tiles away while the mule is 83 and a queue. False means no peer
	// is worth interrupting and the mule path below runs exactly as it did.
	if len(woodcutrunner.HeldAxes(d)) == 0 && b.askPeerForAnAxe(ctx, d) {
		return
	}
	// AND ONLY WHEN THE MULE MIGHT ACTUALLY HAVE ONE. The walk is 83 tiles each way and the
	// account earns nothing for the whole of it; muleMightHaveAnAxe is false only when
	// something genuinely known says there is nothing to ask for, so an unconfigured fleet or
	// an unpublished mule still walks exactly as before. See woodcutterhandout.go.
	if len(woodcutrunner.HeldAxes(d)) == 0 && b.askMuleForAnAxeReady() && b.muleMightHaveAnAxe(ctx) {
		stand := b.settings.MuleStand
		if tile.Chebyshev(currentTileOf(d), stand) > muleMeetRadius {
			// TravelTo, not WalkNear: Seers -> Catherby is a cross-region leg, and TravelTo is
			// the one that plans over webwalk's graph and re-plans when a hop's NPC has
			// wandered. A single A* is what left the axe path reporting "unreachable" before.
			if !woodcutrunner.TravelTo(d, stand, muleMeetRadius) {
				b.warn(ctx, "mule-walk", 60*time.Second, "could not reach %q's post at %d,%d to be handed an axe — retrying", b.settings.Mule, stand.X, stand.Z)
				ctx.Delay(3 * time.Second)
			}
			return
		}
		// Bounded and non-committal: false means the mule said no, said nothing, or is not
		// running at all, and the market path below then runs exactly as it did before. Asking
		// can never leave the account more stuck than it already was.
		if b.askMuleForAnAxe(ctx, d) {
			return
		}
	}
	// AN UPGRADE FROM THE MULE, for an account that already holds an axe, when the mule's
	// published bank shows a better one (woodcuttermuleaxe.go). False falls through to the market.
	if len(woodcutrunner.HeldAxes(d)) > 0 && b.upgradeFromMule(ctx, d) {
		return
	}
	// The market and Bob's-shop path is the Seers one.
	if tile.Chebyshev(currentTileOf(d), woodcutplan.Seers) > 3 {
		if !woodcutrunner.WalkNear(d, woodcutplan.Seers, 2) {
			ctx.Delay(2 * time.Second)
		}
		return
	}
	b.axeStep(ctx, d)
}

// muleMeetRadius is how close to the mule's post counts as "it can see me".
//
// A hand-out needs mulerunner.ResolvePlayerSlot to find this account in the mule's own player
// list, which is a render-distance question rather than a tile-exact one; three tiles is well
// inside that and leaves room for the mule shuffling around its own post tile. Deliberately not
// zero: insisting on the exact tile would fight the mule for it.
const muleMeetRadius = 3

func currentTileOf(d *woodcutrunner.Deps) tile.Tile {
	x, z, level, _ := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

// ---- the Seers bank ----

func (b *woodcutterBot) bankStep(ctx *Context, d *woodcutrunner.Deps) {
	bank, ok := woodcutrunner.OpenSeersBank(d)
	if !ok {
		b.warn(ctx, "bank-open", 60*time.Second, "could not reach or open the Seers bank — retrying")
		ctx.Delay(3 * time.Second)
		return
	}
	// Tell the fleet what is in here while it is open. One map write, no extra read, and it is
	// the entire cost of fleet-wide bank visibility.
	b.lastBankItems = bank.Items()
	b.publishBankToPeers(ctx, bank)
	held := woodcutrunner.HeldAxes(d)
	keepAxe := woodcutplan.AxeToKeep(held)
	keep := []string{}
	if keepAxe != "" {
		keep = append(keep, keepAxe)
	}
	// KEEP THE SHIP FARE. Without this the trip that funds the Draynor camp is undone by the
	// next bank trip, and the account funds it again forever. See KeepFareAtBank.
	if s, ok := b.snapshot(ctx, d); ok && woodcutplan.KeepFareAtBank(s) {
		keep = append(keep, freshstart.Coins)
	}
	usedBefore := usedSlots(d)
	deposited := woodcutrunner.DepositAllExcept(d, bank, keep)
	if deposited > 0 {
		b.trips++
		b.vlog(ctx, "banked %d slots (%d used before, %d after)", deposited, usedBefore, usedSlots(d))
	}
	b.bankAxes = woodcutrunner.BankAxes(d, bank)
	b.bankRead = true

	want := woodcutplan.AxeToWithdraw(woodcutrunner.HeldAxes(d), b.bankAxes)
	if want != "" {
		if woodcutrunner.WithdrawOneOf(d, bank, want) {
			ctx.logf("woodcutter: took the %s out of the bank", strings.ToLower(want))
			b.asks, b.orderedAt = 0, time.Time{}
		} else {
			b.warn(ctx, "axe-withdraw", 60*time.Second, "could not withdraw the %s — retrying", strings.ToLower(want))
		}
		b.bankAxes = woodcutrunner.BankAxes(d, bank)
	}

	target := woodcutplan.TargetTier(b.settings.Target)
	b.bankTargetLogs = woodcutrunner.BankTargetLogs(d, bank, target.Logs)
	_ = bank.Close()

	if b.settings.Donor().Enabled() {
		packLogs := 0
		for _, r := range woodcutrunner.PackSlotRows(d) {
			if strings.EqualFold(r.Name, target.Logs) {
				packLogs += r.Count
			}
		}
		settings := b.settings
		if v, ok := ctx.MuleAtOverride(); ok {
			settings.MuleAt = v
		}
		if woodcutplan.MuleDue(b.bankTargetLogs, packLogs, settings.MuleAt, time.Now(), b.handoffNext) {
			ctx.logf("woodcutter: %d %s held — handing every log and the random-event haul to %s", b.bankTargetLogs+packLogs, strings.ToLower(target.Logs), settings.Mule)
			b.handoffPending = true
		}
	}

	// An axe UPGRADE — wanting a better axe while already holding a working one — is checked
	// here rather than in the phase machine, because this is the one moment that has
	// everything it needs: the pack has just been emptied (so there is room for the axe and
	// the coins), the account is standing at the Seers bank (which is where the market is),
	// and the bank's own axes have just been read (so DecideAxeBuy can prefer a free banked
	// one over buying a second).
	//
	// PhaseOrder covers the other case, having no axe at all. Never run while a hand-over is
	// pending: both want to open a trade, and the hand-over is the one carrying the fleet's
	// logs.
	if b.settings.BuysAxes() && !b.handoffPending && time.Now().After(b.axeNext) {
		b.axeStep(ctx, d)
	}
}

func usedSlots(d *woodcutrunner.Deps) int {
	return woodcutrunner.PackSlots - woodcutrunner.FreeSlots(d)
}

// ---- chopping ----

func (b *woodcutterBot) chopStep(ctx *Context, d *woodcutrunner.Deps, s woodcutplan.WoodSnapshot) {
	// Same gate seersPhase used, or the bot would walk to one camp and chop at another.
	tier := woodcutplan.CurrentTierWith(s.Level, s.Target, s.FastAxeTiers && woodcutplan.FastAxeHeld(s.AxesHeld))
	res := woodcutrunner.Chop(d, tier)
	b.chopped += res.Gained
	b.chopOutcome(ctx, d, tier, res)
}

// chopOutcome answers one Chop call's reason. Shared by the ladder's own chopStep and by the
// Draynor start camp, because the Ent is the same hazard at both — the camp's own content note
// makes that sharper rather than softer: `macro_ent_dead_tree1` is literally named "Dead tree",
// so at Draynor an Ent is indistinguishable from the thing the camp exists to chop, and the
// tile-keyed exclusion is the only thing that tells them apart.
func (b *woodcutterBot) chopOutcome(ctx *Context, d *woodcutrunner.Deps, tier woodcutplan.Tier, res woodcutrunner.ChopResult) {
	// The pass is stuck when it ends having found trees and chopped none. Anything else — a
	// chop, a full pack, a level-up, nothing in sight at all — ends the run.
	final := res
	defer func() {
		switch final.Reason {
		case "all-blocked", "unreachable", "cut-off":
		default:
			b.stuck.Reset()
		}
	}()
	switch res.Reason {
	case "ent":
		// The one case that must NOT wait. The engine put the account on the Ent
		// (macro_event_ent_spawn ends with p_opnpc(1)) and keeps re-queueing it, damaging the
		// axe once every three ticks and breaking it on the seventh — and %macro_ent_counter
		// persists across Ents, so an account that "gets away with it" four times still loses
		// the axe on the fifth. Only another action from this client takes it off. So the very
		// next thing that happens is a fresh Chop, which picks a DIFFERENT tree (the one with
		// the Ent is now excluded) and clicks it. No delay at all.
		b.entsSeen++
		res2 := woodcutrunner.Chop(d, tier)
		b.chopped += res2.Gained
		final = res2
		switch res2.Reason {
		case "all-blocked", "no-tree", "unreachable":
			// Nothing else to click, so the interrupt has to be a step. Standing still here is
			// what actually breaks the axe: Chop's no-tree branch used to return and the
			// caller delayed two seconds without sending anything, which is ten ticks of the
			// Ent chopping back. "unreachable" belongs here too: it can return without sending
			// a single walk, which leaves the account on the Ent just the same.
			woodcutrunner.StepAwayFrom(d, tier)
		case "cut-off":
			b.returnToCamp(ctx, d, tier)
		}
	case "cut-off":
		b.returnToCamp(ctx, d, tier)
	case "all-blocked":
		b.waitOrReturn(ctx, d, tier, res.Excluded, fmt.Sprintf("every %s within reach is excluded (%s)", strings.ToLower(tier.Loc), res.Excluded))
	case "unreachable":
		// The tree is there; no tile the engine would accept a chop from could be stood on.
		// Chop has already excluded it for woodcutplan.UnreachableBlockFor, so the very next
		// call picks a different one — no delay, exactly like the Ent case, because waiting
		// changes nothing about which tiles are blocked and the camp has 55 other trees.
		res2 := woodcutrunner.Chop(d, tier)
		b.chopped += res2.Gained
		final = res2
		switch res2.Reason {
		case "cut-off":
			b.returnToCamp(ctx, d, tier)
		case "all-blocked":
			b.waitOrReturn(ctx, d, tier, res2.Excluded, fmt.Sprintf("every %s within reach is excluded (%s)", strings.ToLower(tier.Loc), res2.Excluded))
		case "unreachable":
			b.waitOrReturn(ctx, d, tier, woodcutplan.BlockTally{}, fmt.Sprintf("no %s within reach has a tile the engine accepts a chop from that this account can reach", strings.ToLower(tier.Loc)))
		case "no-tree":
			b.warn(ctx, "tree-reach", 60*time.Second, "no %s within reach has a tile the engine accepts a chop from — stepping away", strings.ToLower(tier.Loc))
			woodcutrunner.StepAwayFrom(d, tier)
			ctx.Delay(2 * time.Second)
		}
	case "no-tree":
		b.warn(ctx, "tree", 60*time.Second, "no %s in sight — waiting for one to grow back", strings.ToLower(tier.Loc))
		ctx.Delay(2 * time.Second)
	case "stalled":
		b.vlog(ctx, "no logs for a while — clicking the %s again", strings.ToLower(tier.Loc))
	case "full":
		b.vlog(ctx, "pack full (%d logs)", woodcutrunner.LogSlots(d))
	case "dialog", "stopped":
		// Nothing to do — the next Loop call re-derives the phase from live state.
	}
}

// returnToCamp answers a "cut-off" Chop: the account has no route to its camp's anchor or to any
// stand tile of the tree it picked, so it walks back with the door-aware walker rather than
// excluding the camp's trees one by one and waiting for exclusions that cannot help. See
// woodcutrunner.Chop's cut-off branch for the live wedge (21.9 minutes, one account) this replaces.
func (b *woodcutterBot) returnToCamp(ctx *Context, d *woodcutrunner.Deps, tier woodcutplan.Tier) {
	b.cutOffs++
	here := currentTileOf(d)
	ctx.logf("woodcutter: cut off from the %s camp at %d,%d,%d — walking back to %d,%d through the door-aware walker (escape %d this run)",
		strings.ToLower(tier.Label), here.X, here.Z, here.Level, tier.Anchor.X, tier.Anchor.Z, b.cutOffs)
	if woodcutrunner.ReturnToCamp(d, tier) {
		b.stuck.Reset()
		return
	}
	b.warn(ctx, "cut-off-walk", 30*time.Second, "could not walk back to the %s camp at %d,%d from %v — trying again next pass",
		strings.ToLower(tier.Label), tier.Anchor.X, tier.Anchor.Z, currentTileOf(d))
	ctx.Delay(3 * time.Second)
}

// waitOrReturn answers a pass that found trees in reach and chopped none of them: every one
// excluded, or none of them standable. What it does is woodcutplan.DecideStuck's call — a bounded
// wait for the first exclusion to lift where the account is at its camp, or, once the run has gone
// on for woodcutplan.StuckReturnAfter passes away from the anchor, a walk back to the anchor.
//
// GENUINE ENT AVOIDANCE IS UNCHANGED: an Ent among the exclusions still gets the StepAwayFrom
// interrupt every pass, because the engine keeps an account on an Ent until this client sends
// something else, and the wait never chops an excluded tree. What changed is that a camp with NO
// Ent in it is no longer described as one, and an account that is simply in the wrong place stops
// waiting there.
func (b *woodcutterBot) waitOrReturn(ctx *Context, d *woodcutrunner.Deps, tier woodcutplan.Tier, excluded woodcutplan.BlockTally, why string) {
	now := time.Now()
	passes := b.stuck.Note(now)
	here := currentTileOf(d)
	act, wait := woodcutplan.DecideStuck(woodcutplan.StuckSnapshot{Passes: passes, Here: here, Tier: tier, Excluded: excluded, Now: now})
	if act == woodcutplan.StuckReturn {
		ctx.logf("woodcutter: %s, %d passes in a row at %d,%d — walking back to the %s camp at %d,%d rather than waiting here again",
			why, passes, here.X, here.Z, strings.ToLower(tier.Label), tier.Anchor.X, tier.Anchor.Z)
		if !woodcutrunner.ReturnToCamp(d, tier) {
			b.warn(ctx, "stuck-walk", 30*time.Second, "could not walk back to the %s camp at %d,%d from %v — waiting instead",
				strings.ToLower(tier.Label), tier.Anchor.X, tier.Anchor.Z, currentTileOf(d))
			ctx.Delay(woodcutplan.StuckWaitMin)
		}
		return
	}
	if excluded.Ent > 0 || woodcutplan.AwayFromAnchor(here, tier) {
		// The Ent interrupt, and — away from the anchor — a planned step toward where the camp's
		// trees are, so the next pass searches from somewhere better.
		woodcutrunner.StepAwayFrom(d, tier)
	}
	b.warn(ctx, "all-blocked", 30*time.Second, "%s — waiting %s (pass %d in a row at %d,%d)", why, wait.Round(100*time.Millisecond), passes, here.X, here.Z)
	ctx.Delay(wait)
}

// ---- the mule ----

// handoffStep draws every log and everything else the Seers bank holds beyond the axes and the
// keep list as notes and drives internal/scripts/donorrunner.Run against
// the meet tile settings.MuleStand names — muleAtTile, the same setting fisherBot and
// lootChestFisherBot already read.
func (b *woodcutterBot) handoffStep(ctx *Context, d *woodcutrunner.Deps) error {
	held := woodcutrunner.HeldAxes(d)
	keep := []string{}
	if a := woodcutplan.AxeToKeep(held); a != "" {
		keep = append(keep, a)
	}
	settings := b.settings
	if v, ok := ctx.MuleAtOverride(); ok {
		settings.MuleAt = v
	}
	donor := settings.Donor()
	keepList := append(append([]string{}, keep...), splitKeep(settings.MuleKeep)...)

	bank, ok := woodcutrunner.OpenSeersBank(d)
	if !ok {
		ctx.logf("woodcutter: hand-off: could not reach or open the Seers bank — will retry")
		ctx.Delay(5 * time.Second)
		return nil
	}
	rows := woodcutplan.MuleRows(woodcutrunner.BankItemRows(d, bank), keepList)
	// THE WHOLE DRAW IS ONE BURST (twopertick-1012): note mode on, a Withdraw-All per row (a note takes one pack slot, so
	// as many rows as the pack has free slots) and note mode off go out together, five user events a tick, and are
	// confirmed once. It was a note-mode switch that waited on the varp, a Withdraw-All and a wait per row, and another
	// switch and wait.
	drawNames := make([]string, 0, len(rows))
	for _, row := range rows {
		drawNames = append(drawNames, row.Name)
	}
	noteDraw, asked := bank.DrawAllAsNotes(bank.RowIDsByName(d.Config, drawNames), actions.NoteResolver(d.Config), woodcutrunner.FreeSlots(d), woodcutrunner.Waiter(d))
	if asked > 0 {
		ctx.logf("woodcutter: hand-off: %s", actions.VisitLog("note draw", asked+2, noteDraw, 0))
	}
	b.bankAxes = woodcutrunner.BankAxes(d, bank)
	target := woodcutplan.TargetTier(settings.Target)
	b.bankTargetLogs = woodcutrunner.BankTargetLogs(d, bank, target.Logs)
	rowsLeft := len(woodcutplan.MuleRows(woodcutrunner.BankItemRows(d, bank), keepList))
	_ = bank.Close()

	packRows := func() []donorplan.ItemRow { return woodcutrunner.PackSlotRows(d) }
	drawn := woodcutplan.MuleHaul(packRows(), keepList)
	if drawn <= 0 {
		b.handoffPending = false
		b.handoffNext = time.Now().Add(donorplan.HandoffRetry)
		ctx.logf("woodcutter: hand-off: nothing drawn from the bank — resuming chopping")
		return nil
	}
	ctx.logf("woodcutter: hand-off to %q — %d row(s) in the bank, %d pack slot(s) drawn, %d row(s) left for another load",
		donor.Mule, len(rows), drawn, rowsLeft)

	rd := &donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		Bus: ctx.Bus, Me: ctx.Username,
	}
	// Let the hand-off reach a mule on another world; a one-world fleet no-ops.
	// See bot.WireDonorWorlds.
	WireDonorWorlds(ctx, rd)
	res := donorrunner.Run(rd, donorrunner.Options{
		Mule: donor.Mule, Stand: settings.MuleStand,
		RowsInBank: len(rows), Drawn: drawn,
		Load: func() []donorplan.LoadGroup {
			return woodcutplan.MuleLoad(packRows(), donorplan.DefaultMuleFreeSlots, keepList)
		},
		HaulLeft: func() int { return woodcutplan.MuleHaul(packRows(), keepList) },
	})

	b.handoffPending = false
	switch {
	case res.Stopped:
		ctx.logf("woodcutter: hand-off interrupted by a stop request")
		return nil
	case res.GaveUp:
		b.handoffNext = time.Now().Add(donorplan.HandoffRetry)
		ctx.logf("woodcutter: hand-off: %s — resuming chopping, next attempt no sooner than %s", res.Reason, b.handoffNext.Format(time.Kitchen))
	case res.Traded:
		b.handoffs++
		b.handedOver += res.Slots
		ctx.logf("woodcutter: hand-off #%d complete — %d trade(s), %d pack slot(s) crossed to %q", b.handoffs, res.Loads, res.Slots, donor.Mule)
		if rowsLeft > 0 {
			b.handoffPending = true
			ctx.logf("woodcutter: %d bank row(s) still for %q — drawing the next load", rowsLeft, donor.Mule)
		}
	default:
		ctx.logf("woodcutter: hand-off ended without trading (%s)", res.Reason)
	}
	return nil
}

func splitKeep(csv string) []string {
	var out []string
	for _, p := range strings.Split(csv, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (b *woodcutterBot) OnStop(ctx *Context) {
	axes := ""
	if b.coinAsks > 0 || b.axesBought > 0 {
		axes = fmt.Sprintf(", %d axe(s) bought for %d gp from %d coin ask(s) (%d answered)",
			b.axesBought, b.axeSpent, b.coinAsks, b.coinsGot)
	}
	if b.axeHeadsFixed > 0 || b.axesRepaired > 0 || b.brokenReplaced > 0 {
		axes += fmt.Sprintf(", %d axe head(s) re-attached, %d repaired at Bob's", b.axeHeadsFixed, b.axesRepaired)
		if b.brokenReplaced > 0 {
			axes += fmt.Sprintf(", %d broken axe(s) replaced by the mule", b.brokenReplaced)
		}
	}
	if b.campLogs > 0 || b.campShafts > 0 || b.startCampOff {
		camp := fmt.Sprintf(", Draynor start camp: %d log(s) chopped into %d arrow shaft(s)", b.campLogs, b.campShafts)
		if b.startCampOff {
			camp += " (given up part way — see the log line that says why)"
		}
		axes += camp
	}
	if b.cutOffs > 0 {
		axes += fmt.Sprintf(", %d walk(s) back to a camp it was cut off from", b.cutOffs)
	}
	ctx.logf("woodcutter: stopped — woodcutting %d (+%d xp), %d logs, %d bank trips, %d gp from the starter kit, %d slots to the mule in %d hand-over(s)%s",
		woodcutrunner.WoodcuttingLevel(b.deps(ctx)), woodcutrunner.WoodcuttingXP(b.deps(ctx))-b.xpAtStart, b.chopped, b.trips, b.soldGP, b.handedOver, b.handoffs, axes)
}
