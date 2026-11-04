package bot

// MasterCrafter, the owner's main account's script (3 Oct 2026: "every skill to 99"). It runs
// ScarabSwarm, and every craftCheckEveryMinutes (20) reads the mule's published bank. When the mule
// and this account hold at least craftGemThreshold (20,000) uncut gems its Crafting level can cut,
// or craftBowThreshold (20,000) unstrung bows with the Bow strings to string them, it stops ScarabSwarm at
// a SAFE POINT, runs a round at the mule's own post (mastercrafter_rounds.go), hands the product
// back to the mule, and starts ScarabSwarm again from its checkpoint. A storage round
// (mastercrafter_storage.go, 5 Oct 2026) does the opposite of a hand-over: when the mule's bank is
// nearly full it takes the cold, small rows OFF the mule and banks them here.
//
// WHAT IT REUSES, rather than building a second copy of anything:
//   - ScarabSwarm itself, whole, as scarabSwarmBot: its OnStart, Loop and OnStop. A round ends it
//     with OnStop (the checkpoint is saved) and a fresh scarabSwarmBot.OnStart resumes it, exactly
//     as a fleet restart does. The settings map is the same one, so "main's existing settings"
//     keep working; only craft* keys are this script's.
//   - the gem plan and primitives of GemCutter (gemplan's levels and ids, gemrunner), with the
//     burst widened from one gem a tick to five (gemrunner.CutAll, actions.UseOnHeldMany);
//   - BowStringer, whole, as bowStringerBot with bowstringer_embed.go's hooks (stringing five a
//     tick through bowstringrunner.StringAll, the bank and the kit order at the mule's post);
//   - the mule's kit order (scarabrunner.RequestKitBundle, the path every ScarabSwarm ask uses)
//     and the hand-over (runerunner.Deliver, the path RuneRunner and ScarabSwarm's consolidate use).
//
// THE SAFE POINT is read between two ScarabSwarm Loop calls, never inside one: no interface open
// (trade or bank), not hit or hurt lately, no random event, no world errand or hop in flight, no
// Stage 3 rune trip, not standing where only a quest goes, not in the Wilderness, the Mage Bank or
// the lair, above craftSafeHpPercent of its hitpoints (mastercraftplan.SafePoint). A due round waits
// for it; ScarabSwarm carries on meanwhile.
//
// The decisions are internal/scripts/mastercraftplan's; the bank visits and the product draw are
// internal/scripts/mastercraftrunner's. docs/mastercrafter_settings_reference.md has every setting.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/pearlplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/waterfallplan"
	"rsb0t-go/internal/settings"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("MasterCrafter", func() Bot { return &masterCrafterBot{} })
}

// masterCrafterRecheck is how soon a check that had no data (the mule not heard, its bank not
// published) looks again, rather than after the whole craftCheckEveryMinutes.
const masterCrafterRecheck = 2 * time.Minute

// masterCrafterWaitSay is how often a due round says it is waiting for a safe point.
const masterCrafterWaitSay = 5 * time.Minute

// masterSwarm is the part of ScarabSwarm this script drives: the Bot, and the account's reading of
// its own state for the safe point. *scarabSwarmBot is the real one; a test supplies another.
type masterSwarm interface {
	Bot
	safeInput(ctx *Context) mastercraftplan.SafeInput
}

// masterRoundOutcome is what one round came to.
type masterRoundOutcome struct {
	Mode mastercraftplan.Mode
	// Produced is the gems cut (and smashed) or the bows strung; Handed what crossed to the mule.
	Produced, Handed int
	// Transient says the round could not run for a passing reason (the walk did not arrive, the mule
	// away, queued or deferring the kit, a bank that would not open): the mode backs off
	// (mastercraftplan.Ledger.Fail) instead of trying again at the next check.
	Transient bool
	// Stranded says the round ended with the account still INSIDE a Runecrafting temple or the Rune essence mine and
	// could not take the exit (always Transient too). ScarabSwarm is not started from there: switchTo tries the exit
	// again (rescueStranded) first, because there is no bank in either place and nothing for ScarabSwarm to do.
	Stranded bool
	// Stuck says a Runecrafting or Nature round found nothing to work with (no essence anywhere and mining failed, no talisman, a
	// free world): it is paused craftFruitlessPauseMinutes WHATEVER it made before it ran dry, because the next check would find
	// the same nothing (a round that crafted a load and then had no essence was neither paused nor backed off, and was started again
	// at every check).
	Stuck bool
	// CampaignDecided says a Nature round ended by one of the campaign's own rules and its ledger has set what the account waits for (a
	// pause, no pause, a refill at once): the gate round's fruitless pause is not added to it.
	CampaignDecided bool
	// HandoverTried is set when the round ended with a hand-over to the mule, HandoverFailed when
	// products were still in the bank afterwards (or the bank could not be read to say). Failed
	// hand-overs back off on their own account and, in a row, suspend every round.
	HandoverTried, HandoverFailed bool
	Why                           string
}

type masterCrafterBot struct {
	set  mastercraftplan.Settings
	mule string

	lamps lampRubber // held Genie Lamps are rubbed for Magic (lamps.go)
	// gateAlchOffUntil: the gate's alchemy stays off until then, after a round that could not cast its bows away (mastercrafter_gatealch.go).
	gateAlchOffUntil time.Time
	// gateAlchRoad is what the last round's road took: the bows it drew, the casts the walk made and the bows left on arrival, so the
	// next round draws what the road can cast away (runecraftplan.AlchRoadDraw).
	gateAlchRoad struct{ drawn, casts, left int }

	swarm    masterSwarm
	newSwarm func() masterSwarm
	walker   *webwalk.Walker

	ledger mastercraftplan.Ledger
	store  *settings.Store
	// objs is the cache the storage round weighs the mule's rows against; nil is the context's own
	// (mastercrafter_storage.go), a test supplies a table.
	objs storeObjs
	// ledgerKeep is what a newer build wrote that this one does not know (kept through every
	// save); ledgerBlocked is the path of an unreadable store that could not be moved aside, "" when
	// saving is allowed.
	ledgerKeep    settings.Unknown
	ledgerBlocked string

	lastCheck time.Time
	every     time.Duration
	pending   mastercraftplan.Decision
	waitSaid  time.Time
	denied    map[string]bool
	// blindCheck is set when the last check was made before the mule was heard or before the account's
	// place and pack were read: Loop decides again the first pass that has both (mastercrafter_resume.go).
	blindCheck bool

	// decide and round are the two seams: the live reading and the round itself. Nil is the real
	// one; a test replaces them.
	decide func(ctx *Context) mastercraftplan.Decision
	round  func(ctx *Context, dec mastercraftplan.Decision) masterRoundOutcome
	// farFromMuleSeam answers the safe point's place rule (mastercraftplan.SafeInput.FarFromMule); nil
	// is the real one, b.farFromMule.
	farFromMuleSeam func(ctx *Context) bool
	// rescue is the exit rescueStranded makes for an account stranded inside a temple or the essence mine, reporting what
	// it did and whether the account is still stranded; nil is the real one, b.runeRescue.
	rescue func(ctx *Context) (string, bool)

	startedAt   time.Time
	swarmStarts int
	rounds      map[mastercraftplan.Mode]int

	// runeLevel is the Runecrafting level the last check or round read, 0 until one has (storeKeep keeps the
	// talismans and the essence from the swarm's loot hand-off while the Runecrafting round is unfinished).
	runeLevel int

	// trawlerPolled is the last cheap lobby count between checks, trawlerCalled the last presence message the trawler round
	// published (mastercrafter_trawler.go).
	trawlerPolled, trawlerCalled, trawlerHeld, trawlerPrecalled time.Time
	// trawlerStraight is a claim that skipped the bank because the next watch was already in the lobby: the next round boards
	// without its own bank visit (mastercrafter_trawler.go, trawlerStraightOn).
	trawlerStraight bool
	// tipSeen is the pack count of each trawlerTips product at the last trawler wait (mastercrafter_trawler.go).
	tipSeen map[int]int
}

func (b *masterCrafterBot) logf(ctx *Context, format string, args ...any) {
	ctx.logf("mastercrafter: "+format, args...)
}

func (b *masterCrafterBot) deps(ctx *Context) *fleetrunner.Deps {
	return &fleetrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Walker: b.walker,
		Log: ctx.Log, Loot: ctx.Loot, Me: ctx.Username, World: ctx.World(),
	}
}

func (b *masterCrafterBot) OnStart(ctx *Context) error {
	chooseMagicLamps(ctx) // a Genie's Lamp goes to Magic (lamps.go)
	set, err := mastercraftplan.DecodeSettings(ctx.Settings)
	if err != nil {
		return fmt.Errorf("mastercrafter: %w", err)
	}
	if ctx.Pack == nil {
		return fmt.Errorf("mastercrafter: no collision pack configured (fleet.config.json needs a packPath)")
	}
	b.set = set
	b.mule = StringSetting(ctx.Settings, "mule", bowstringplan.DefaultMule)
	b.denied = map[string]bool{}
	b.rounds = map[mastercraftplan.Mode]int{}
	b.startedAt = time.Now()
	b.every = set.CheckEvery
	b.lastCheck = time.Time{}
	b.pending = mastercraftplan.Decision{}
	if b.newSwarm == nil {
		b.newSwarm = b.defaultSwarm
	}
	// The same walker ScarabSwarm and BowStringer build; plain pathfinding is the fallback.
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
	b.store = settings.NewStore(StringSetting(ctx.Settings, "checkpointDir", scarabSwarmCheckpointDir))
	b.loadLedger(ctx)
	b.clearGiveUp(ctx)
	between := "ScarabSwarm"
	if set.Idle == mastercraftplan.IdlePost {
		between = "waiting at " + b.mule + "'s post"
	}
	if ctx.Bus == nil {
		b.logf(ctx, "WARNING: no wall bus — the mule cannot be heard or asked, so no round runs (between rounds: %s)", between)
	}
	b.logf(ctx, "anvil round (Varrock west): %s — starts over %d Steel bars (this account's bank and %s's together) and then smiths every bar it can: Rune, Adamant, Mithril, Steel into arrowtips best first (%s), Iron into platebodies (%s); superheating makes Steel bars and up only (low bars %s); ledger: round under way %v",
		onOff(set.Smith), set.SmithMinSteelBars, b.mule, onOff(set.SmithArrowtips), onOff(set.SmithIronPlatebodies), onOff(set.SuperheatLowBars), b.ledger.SmithActive)
	if set.CannonballTarget > 0 {
		b.logf(ctx, "cannonball round (Falador furnace): on — keeps %d cannonballs in stock (%s's bank and this account's together), smelting at most %d Steel bars a round and starting at %d+ bars, ranked before the anvil round; it needs Smithing %d, a members world, an Ammo mould in this account's pack or bank (untradeable) and Dwarf Cannon complete; ledger: round under way %v, %d balls made so far",
			set.CannonballTarget, b.mule, set.CannonballBatch, set.CannonballMinBars, mastercraftplan.CannonballLevel, b.ledger.Cannon.Active, b.ledger.Cannon.Balls)
	} else {
		b.logf(ctx, "cannonball round: off (%s 0)", mastercraftplan.KeyCannonballTarget)
	}
	b.logf(ctx, "between rounds %s (craftIdle %q), with a gem round when %s and this account hold %d+ cuttable uncut gems together (gems %s), then a pearl round at %d+ oyster pearls or %d+ Bolts for the tips (pearls %s), then a bow round at %d+ bow-and-string pairs (bows %s), then a storage round at %d+ rows in its bank down to %d (store %s: rows of %d items or fewer, unused for %d days); checked every %s, only at a safe point; jewellery: a batch of %d Rings of dueling when %s holds none, else of %d Games necklaces when it holds no Games necklace or Ring of dueling (%d owed, %d left of a batch under way); ledger: %d strung bow(s) at the mule, pending hand-over %v, pending cut %v, %d item type(s) stored here",
		between, set.Idle, b.mule, set.GemThreshold, onOff(set.GemCutting), set.PearlThreshold, set.BoltThreshold, onOff(set.Pearls), set.BowThreshold, onOff(set.BowStringing),
		set.StoreAtRows, set.StoreToRows, onOff(set.StoreOverflow), set.StoreMaxCount, set.StoreQuietDays, set.CheckEvery,
		set.RingBatch, b.mule, set.JewelleryBatch, b.ledger.Jewellery.OwedTotal(), b.ledger.Jewellery.Left(),
		b.strungTotal(), b.ledger.PendingDeliver, b.ledger.PendingCut, len(b.ledger.Stored))
	if r := set.Rune; r.On {
		l := &b.ledger.Rune
		b.logf(ctx, "runecraft round: ON (%s) - levels Runecrafting to %d on Air then Body (Nature, the members rune, is the next phase) from essence %s (%s), %d-essence mining batches at the %s gateway; travel teleports %s; a round is at most %s; ledger: %d round(s), %d trip(s), %d craft(s), %d essence bound, +%d xp, %d essence mined, last stop %q",
			mastercraftplan.KeyRune, r.LevelTo, r.Essence, essenceSourceWord(r.Essence), r.Batch, r.Gateway, onOff(r.Teleport), r.Round,
			l.Rounds, l.Trips, l.Crafts, l.EssenceCrafted, l.XPGained, l.Mined, l.LastStop)
		if r.BuyBudget > 0 {
			b.logf(ctx, "runecraft round: market lot ON (%s %s) - when the bank, the mule and mining give no essence, up to %s coins of Rune essence at the Seers stalls (at most %d each), funded by the mule from what it holds above %s; %s spent in %d lot(s) so far (%s essence)",
				mastercraftplan.KeyRuneBuyBudget, runecraftplan.Thousands(r.BuyBudget), runecraftplan.Thousands(r.BuyBudget), essenceplan.MarketUnitCeiling, runecraftplan.Thousands(essenceplan.MarketMuleCoinsFloor),
				runecraftplan.Thousands(l.MarketSpent), l.MarketLots, runecraftplan.Thousands(l.MarketBought))
		} else {
			b.logf(ctx, "runecraft round: market lot off (%s 0)", mastercraftplan.KeyRuneBuyBudget)
		}
		b.campaignBanner(ctx)
		g := &l.Gate
		b.logf(ctx, "runecraft gate: %s (%s) - at Runecrafting 44 the main account stands outside the Nature ruins and crafts what RuneCarrier accounts trade him (25 unnoted essence a trade); carriers %v plus whoever is at work on the bus; keep %d Natures, couriers %s up to %d a trade; a round is at most %s; ledger: %d round(s), %d trade(s), %d essence, %d Natures made (+%d xp), %d courier trade(s) carrying %d Natures, %d handed over at the end, last stop %q",
			onOff(r.Manual), mastercraftplan.KeyRuneManual, r.Runners, r.Keep, onOff(r.Courier), r.CourierMax, r.MaxHours,
			g.Rounds, g.Trades, g.EssenceTaken, g.NaturesMade, g.XPGained, g.CourierTrades, g.CourierNatures, g.HandedAtEnd, g.LastStop)
	} else {
		b.logf(ctx, "runecraft round: off (%s false)", mastercraftplan.KeyRune)
	}
	return b.startSwarm(ctx)
}

// clearGiveUp is what a restart does to a suspension by failed hand-overs: the count starts again,
// the wait the failures earned is kept.
func (b *masterCrafterBot) clearGiveUp(ctx *Context) {
	b.clearCampaignGiveUp(ctx)
	until, _ := b.ledger.BackedOff(mastercraftplan.ModeDeliver, time.Now())
	if b.ledger.ClearGiveUp(b.set) {
		b.logf(ctx, "WARNING: rounds were suspended because hand-overs to %s failed %d times in a row (the products are still in the bank); this restart tries again, after the wait the failures earned (until %s)",
			b.mule, b.set.MaxHandoverFailures, until.Format("15:04"))
		b.saveLedger(ctx)
	}
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func (b *masterCrafterBot) strungTotal() int {
	n := 0
	for _, v := range b.ledger.StrungAtMule {
		n += v
	}
	return n
}

// defaultSwarm is the ScarabSwarm this script drives: the registered one, whole, told to keep on its
// loot hand-off whatever the storage round has stored in this account's bank (storeKeep).
//
// With craftIdle "post" it is masterPostIdle instead: the account waits at the mule's post.
func (b *masterCrafterBot) defaultSwarm() masterSwarm {
	switch b.set.Idle {
	case mastercraftplan.IdlePost:
		return &masterPostIdle{b: b}
	case mastercraftplan.IdleOaks:
		return &masterOaksIdle{b: b}
	}
	return idleSwarm(b.storeKeep)
}

// startSwarm starts a fresh ScarabSwarm, which resumes from its checkpoint.
func (b *masterCrafterBot) startSwarm(ctx *Context) error {
	sw := b.newSwarm()
	if err := sw.OnStart(ctx); err != nil {
		return err
	}
	b.swarm = sw
	b.swarmStarts++
	return nil
}

// stopSwarm ends ScarabSwarm's run (its OnStop saves the checkpoint and withdraws its mind-rune
// rows) and drops it.
func (b *masterCrafterBot) stopSwarm(ctx *Context) {
	if b.swarm == nil {
		return
	}
	b.swarm.OnStop(ctx)
	b.swarm = nil
}

func (b *masterCrafterBot) OnStop(ctx *Context) {
	b.stopSwarm(ctx)
	b.saveLedger(ctx)
	l := &b.ledger
	b.logf(ctx, "stopped after %s — %d gem round(s) (%d gems cut or smashed, %d handed to %s), %d pearl round(s) (%d pearls cut, %d Pearl bolts made, %d handed over), %d bow round(s) (%d bows strung, %d handed over), %d shop trip(s) (%d bought for %d coins), %d superheat round(s) (%d bars), %d anvil round(s) (%d bars smithed into %d arrowtips and %d Iron platebodies), %d arrow round(s) (%d headless, %d steel arrows), %d herb round(s) (%d cleaned), %d storage round(s) (%d rows, %d items moved from %s to this account's bank; %d item types held here), %d jewellery round(s) (%d Games necklaces made, %d handed over, %d owed, %d mould(s) bought), %d ScarabSwarm start(s)",
		time.Since(b.startedAt).Round(time.Second), l.GemRounds, l.GemsCut, l.GemsHanded, b.mule, l.PearlRounds, l.PearlsCut, l.PearlBolts, l.PearlBoltsHanded, l.BowRounds, l.BowsStrung, l.BowsHanded,
		l.ShopRounds, l.ShopBought, l.ShopSpent, l.SuperheatRounds, l.BarsMade, l.SmithRounds, l.SmithBars, l.ArrowtipsMade, l.PlatebodiesMade, l.ArrowRounds, l.HeadlessMade, l.ArrowsMade, l.HerbRounds, l.HerbsCleaned,
		l.StoreRounds, l.RowsStored, l.ItemsStored, b.mule, len(l.Stored), l.Jewellery.Rounds, l.Jewellery.Made, l.Jewellery.Handed, l.Jewellery.OwedTotal(), l.Jewellery.MouldsBought, b.swarmStarts)
}

func (b *masterCrafterBot) Loop(ctx *Context) error {
	if b.swarm == nil {
		if err := b.startSwarm(ctx); err != nil {
			return err
		}
	}
	b.lamps.tick(ctx) // a Lamp still held is rubbed for Magic (lamps.go)
	now := time.Now()
	b.decideAgainWhenReady(ctx)
	b.trawlerPoll(ctx, now) // a lobby at its quorum is checked now, not at the next interval (mastercrafter_trawler.go)
	if mastercraftplan.CheckDue(b.lastCheck, now, b.every) {
		b.check(ctx, now)
	}
	if b.pending.Mode != "" {
		in := b.swarm.safeInput(ctx)
		far := b.farFromMule
		if b.farFromMuleSeam != nil {
			far = b.farFromMuleSeam
		}
		// A chest round under way carries on where the account stands (mastercrafter_resume.go), so the
		// walk to the mule's post it would otherwise need to be near is not asked of it.
		in.FarFromMule = far(ctx) && !b.resumesHere(ctx, b.pending)
		if o, ok := b.swarm.(*masterOaksIdle); ok && o.chopping() {
			in.FarFromMule = false // master walks from the Falador oaks to the round himself (mastercrafter_oaks.go)
		}
		safe, why := mastercraftplan.SafePoint(in, b.set.SafeHpPercent)
		if safe {
			return b.switchTo(ctx, b.pending)
		}
		if time.Since(b.waitSaid) >= masterCrafterWaitSay {
			b.waitSaid = time.Now()
			b.logf(ctx, "a %s round is due (%s) but ScarabSwarm is not at a safe point: %s — carrying on", b.pending.Mode, b.pending.Why, why)
		}
	}
	return b.swarm.Loop(ctx)
}

// check reads the mule's published bank and decides; a round that is due waits for the safe point.
func (b *masterCrafterBot) check(ctx *Context, now time.Time) {
	b.lastCheck = now
	decide := b.decide
	if decide == nil {
		decide = b.decideLive
	}
	dec := decide(ctx)
	b.blindCheck = dec.Inconclusive || !masterAccountRead(ctx)
	b.every = b.set.CheckEvery
	if dec.Mode == mastercraftplan.ModeSwarm {
		if dec.Inconclusive && b.every > masterCrafterRecheck {
			b.every = masterCrafterRecheck
		}
		if dec.GaveUp {
			b.logf(ctx, "WARNING: %s", dec.Why)
			b.pending = mastercraftplan.Decision{}
			return
		}
		if b.pending.Mode != "" {
			b.logf(ctx, "the %s round that was due is no longer: %s", b.pending.Mode, dec.Why)
		} else {
			b.logf(ctx, "check: staying on ScarabSwarm — %s", dec.Why)
		}
		b.pending = mastercraftplan.Decision{}
		return
	}
	if b.pending.Mode != dec.Mode {
		b.logf(ctx, "check: a %s round is due — %s", dec.Mode, dec.Why)
		b.waitSaid = time.Time{}
	}
	b.pending = dec
}

// masterFletchingStat is UPDATE_STAT's index for Fletching (gamestate.SkillNames[9]).
const masterFletchingStat = 9

// masterSkillXP is the account's whole experience in a stat, 0 before the server has sent it.
func masterSkillXP(ctx *Context, stat int) int {
	if sk, ok := ctx.Session.State().Skill(stat); ok {
		return sk.XP
	}
	return 0
}

// decideLive is Decide over the live account, bus and published bank.
func (b *masterCrafterBot) decideLive(ctx *Context) mastercraftplan.Decision {
	p := scarabMulePresenceOf(ctx, b.mule)
	var stock mastercraftplan.Stock
	if f := muleBankCount(ctx, b.mule); f != nil {
		stock = f
	}
	b.ledger.Reconcile(stock)
	fletching, known := bowstringrunner.Fletching(b.deps(ctx))
	snap := mastercraftplan.Snapshot{
		Now: time.Now(), MuleHeard: p.Heard, MuleSameWorld: p.SameWorld, Stock: stock,
		OwnUncut:      b.ownUncut(ctx),
		OwnPearl:      b.ownPearl(ctx),
		OwnBows:       b.ownBows(ctx),
		BowIDs:        bowIDsOf(ctx),
		CraftingLevel: masterCraftingLevel(ctx), FletchingLevel: fletching, FletchingKnown: known,
		FletchingXP: masterSkillXP(ctx, masterFletchingStat),
		Denied:      b.denied, Ledger: &b.ledger,
	}
	b.supplySnapshot(ctx, &snap)
	b.smithSnapshot(ctx, &snap)
	b.cannonSnapshot(ctx, &snap)
	b.storeSnapshot(ctx, &snap)
	b.runeSnapshot(ctx, &snap)
	b.trawlerSnapshot(ctx, &snap)
	b.campaignReconcile(ctx, snap)
	was0 := b.ledger.Trawler.BlockStart
	if mastercraftplan.TrawlerCycleBefore(b.set, snap.Trawler, &b.ledger, snap.Now) {
		if mastercraftplan.TrawlerRestLen > 0 {
			b.logf(ctx, "trawler: the two-hour block is over: resting until %s (every crafting round at its threshold first, else ScarabSwarm)", b.ledger.Trawler.RestUntil.Format("15:04"))
		} else if t := snap.Trawler; b.set.TrawlerFoodLow > 0 && t.FoodKnown {
			b.logf(ctx, "trawler: the block is over (%s played): the mule holds %d food; the next starts while it holds under %d", snap.Now.Sub(was0).Round(time.Minute), t.Food, b.set.TrawlerFoodLow)
		} else {
			b.logf(ctx, "trawler: the two-hour block is over: the next starts now (no rest; the mule's food not read)")
		}
		b.saveLedger(ctx)
	}
	dec := b.withChest(ctx, snap)
	was := b.ledger.Trawler
	if mastercraftplan.TrawlerCycleAfter(b.set, &b.ledger, snap.Now, dec) {
		now := b.ledger.Trawler
		switch {
		case was.BlockStart.IsZero() && !now.BlockStart.IsZero():
			b.ledger.Trawler.BlockXP, b.ledger.Trawler.BlockXPAt = b.trawlerXPNow(ctx), time.Now()
			b.logf(ctx, "trawler: a two-hour block starts (Fishing %d XP)", snap.Trawler.FishingXP)
		case now.RestCrafted:
			b.logf(ctx, "trawler: resting with a %s round to do: the next block starts when nothing is left to craft, or at %s", dec.Mode, b.ledger.Trawler.RestUntil.Format("15:04"))
		default:
			b.logf(ctx, "trawler: nothing left to craft: the rest ends and the next block starts")
		}
		b.saveLedger(ctx)
	}
	return dec
}

// ownUncut is the cuttable uncut gems in this account's bank as the session last saw it.
func (b *masterCrafterBot) ownUncut(ctx *Context) int {
	if ctx.Session == nil {
		return 0
	}
	inv := ctx.Session.State().Inventory(actions.BankWithdraw)
	if inv == nil {
		return 0
	}
	gems := mastercraftplan.GemsBestFirst(masterCraftingLevel(ctx), b.set.GemKinds, b.denied, b.set.MembersWorld)
	return eligibleUncut(mastercraftrunner.UncutCounts(inv.NonEmpty()), gems)
}

// ownPearl is the pearl round's items in this account's bank as the session last saw it.
func (b *masterCrafterBot) ownPearl(ctx *Context) pearlplan.Held {
	if ctx.Session == nil {
		return pearlplan.Held{}
	}
	inv := ctx.Session.State().Inventory(actions.BankWithdraw)
	if inv == nil {
		return pearlplan.Held{}
	}
	return mastercraftrunner.PearlHeld(inv.NonEmpty())
}

// masterCraftingLevel is the account's Crafting level, 0 until the server has sent it.
func masterCraftingLevel(ctx *Context) int {
	if sk, ok := ctx.Session.State().Skill(gemrunner.CraftingStat); ok && sk.Generation > 0 {
		return sk.Level
	}
	return 0
}

// switchTo is the whole of a round: stop ScarabSwarm, run it, record it, start ScarabSwarm again.
func (b *masterCrafterBot) switchTo(ctx *Context, dec mastercraftplan.Decision) error {
	b.logf(ctx, "ScarabSwarm stopped at a safe point (%s) for a %s round: %s", fleetrunner.HereIs(b.deps(ctx)), dec.Mode, dec.Why)
	b.stopSwarm(ctx)
	round := b.round
	if round == nil {
		round = b.runRound
	}
	// The next job follows the moment one is ready (gems, then bows, then pearls, then the storage
	// round, then the later rounds, then ScarabSwarm while the mule fills up again;
	// mastercraftplan.Decide): after a round, decide again, and run the next round straight away from
	// the mule's post when mastercraftplan.Chain allows it.
	for chained := 0; ; chained++ {
		started := time.Now()
		out := round(ctx, dec)
		b.afterRound(ctx, out, time.Since(started))
		b.pending = mastercraftplan.Decision{}
		b.lastCheck, b.every = time.Now(), b.set.CheckEvery
		if ctx.Stopped() {
			return nil
		}
		if out.Stranded && !b.rescueStranded(ctx, out) {
			return nil
		}
		if out.Transient || out.HandoverFailed || chained+1 >= masterChainMax {
			break
		}
		decide := b.decide
		if decide == nil {
			decide = b.decideLive
		}
		next := decide(ctx)
		if !mastercraftplan.Chain(out.Mode, next) {
			break
		}
		b.logf(ctx, "straight on to a %s round: %s", next.Mode, next.Why)
		if next.Continue {
			// A campaign's own next round (after an own hand-over trip or a refill) is one campaign, not another round in a row.
			chained = 0
		}
		dec = next
	}
	b.logf(ctx, "back to ScarabSwarm")
	return b.startSwarm(ctx)
}

// masterChainMax bounds how many rounds run one after another without ScarabSwarm between.
const masterChainMax = 8

// masterRescueTries is how many times a round that ended with the account stranded inside a temple or the essence mine
// has its exit made again, masterRescuePause apart, before ScarabSwarm is started from there as a last resort. A variable
// so a test can shorten the pause.
const masterRescueTries = 5

var masterRescuePause = 30 * time.Second

// rescueStranded holds the account where it is -- ScarabSwarm is NOT started -- while the exit is tried again: a levelling
// round that ends inside a temple or the essence mine (the portal would not take, three stages failed) would otherwise
// hand the account to ScarabSwarm there, which has no bank and nothing to do, until the next check. It returns true when
// the account is out (or after masterRescueTries failed tries, when ScarabSwarm is started from there as a last resort: a
// levelling round resumes from wherever the account stands at the next check), false when the bot was stopped.
func (b *masterCrafterBot) rescueStranded(ctx *Context, out masterRoundOutcome) bool {
	rescue := b.rescue
	if rescue == nil {
		rescue = b.runeRescue
	}
	for try := 1; try <= masterRescueTries; try++ {
		b.logf(ctx, "WARNING: the %s round ended with the account still inside a temple or the essence mine (%s): ScarabSwarm is not started from there; the exit is tried again in %s (try %d of %d)",
			out.Mode, out.Why, masterRescuePause, try, masterRescueTries)
		if !ctx.Delay(masterRescuePause) {
			return false
		}
		what, stranded := rescue(ctx)
		if !stranded {
			b.logf(ctx, "the account is out again after rescue try %d: %s", try, what)
			return true
		}
		b.logf(ctx, "WARNING: still stranded after rescue try %d: %s", try, what)
		if ctx.Stopped() {
			return false
		}
	}
	b.logf(ctx, "WARNING: still stranded after %d rescue tries at %s: ScarabSwarm starts from here as a last resort (a levelling round resumes from here at the next check)",
		masterRescueTries, fleetrunner.HereIs(b.deps(ctx)))
	return true
}

// afterRound records a round: the counters, the backoffs, and the ledger on disk. A Stop during the
// round says nothing about the round and sets no wait.
func (b *masterCrafterBot) afterRound(ctx *Context, out masterRoundOutcome, took time.Duration) {
	l := &b.ledger
	b.rounds[out.Mode]++
	switch out.Mode {
	case mastercraftplan.ModeGems:
		l.GemRounds++
		l.GemsCut += out.Produced
	case mastercraftplan.ModeBows:
		l.BowRounds++
		l.BowsStrung += out.Produced
	case mastercraftplan.ModePearls:
		l.PearlRounds++ // PearlsCut and PearlBolts are counted by the round itself
	case mastercraftplan.ModeStore:
		l.StoreRounds++ // RowsStored and ItemsStored are counted by the round itself
	case mastercraftplan.ModeShop:
		l.ShopRounds++ // the later rounds count their products themselves
	case mastercraftplan.ModeSuperheat:
		l.SuperheatRounds++
	case mastercraftplan.ModeSmith:
		l.SmithRounds++ // the bars, arrowtips and platebodies are counted by the round itself
	case mastercraftplan.ModeCannonballs:
		// The round counts its own rounds, bars, balls and hand-overs (Ledger.Cannon).
	case mastercraftplan.ModeArrows:
		l.ArrowRounds++
	case mastercraftplan.ModeHerbs:
		l.HerbRounds++
	case mastercraftplan.ModeJewellery:
		// The round counts its own rounds, necklaces and moulds (Ledger.Jewellery).
	case mastercraftplan.ModeRunecraft:
		// The round counts its own rounds, trips, crafts and mining (Ledger.Rune).
	case mastercraftplan.ModeNature:
		// The round counts its own rounds, trades and Natures (Ledger.Rune.Gate).
	case mastercraftplan.ModeTrawler:
		// The round counts its own rounds, claims and items (Ledger.Trawler).
	}
	if !ctx.Stopped() {
		b.recordWaits(ctx, out, time.Now())
	}
	b.logf(ctx, "ROUND mode=%s took=%s produced=%d handed=%d transient=%v handoverFailed=%v pendingHandover=%v pendingCut=%v: %s",
		out.Mode, took.Round(time.Second), out.Produced, out.Handed, out.Transient, out.HandoverFailed, l.PendingDeliver, l.PendingCut, out.Why)
	b.saveLedger(ctx)
}

// recordWaits sets what a round earned. A hand-over that left products in the bank, or could not
// read it, backs the hand-over off (30 minutes, doubling to 3 hours) and, set.MaxHandoverFailures in
// a row, suspends every round. A gem or bow round that made nothing for a PASSING reason backs its
// own mode off the same way; one that made nothing for a reason that will not pass by itself (no
// chisel anywhere, none of the kind in the mule) is paused for set.FruitlessPause; one that made
// something clears both. Every one of them is persisted in the ledger.
func (b *masterCrafterBot) recordWaits(ctx *Context, out masterRoundOutcome, now time.Time) {
	l, set := &b.ledger, b.set
	if out.HandoverTried {
		if out.HandoverFailed {
			b.failMode(ctx, mastercraftplan.ModeDeliver, now, "the hand-over to "+b.mule+" left products in the bank")
			if l.HandoverGaveUp(set) {
				b.logf(ctx, "WARNING: GIVING UP: %d hand-overs to %s in a row have failed. No round starts until this script restarts; the products stay in the bank (restart it, or fix the mule, to try again)",
					l.Fails(mastercraftplan.ModeDeliver), b.mule)
			}
		} else if out.Mode == mastercraftplan.ModeDeliver {
			// Only the deliver round carries every product kind, so only its success clears the
			// hand-over backoff and give-up count; a gem or bow round's clean hand-over of its own
			// kind must not wipe what a failed hand-over of the other kind left pending.
			l.Succeed(mastercraftplan.ModeDeliver)
		}
	}
	switch out.Mode {
	case mastercraftplan.ModeGems, mastercraftplan.ModeBows, mastercraftplan.ModePearls, mastercraftplan.ModeStore,
		mastercraftplan.ModeShop, mastercraftplan.ModeSuperheat, mastercraftplan.ModeSmith, mastercraftplan.ModeArrows, mastercraftplan.ModeHerbs, mastercraftplan.ModeCannonballs:
		pause := &l.GemPauseUntil
		switch out.Mode {
		case mastercraftplan.ModeBows:
			pause = &l.BowPauseUntil
		case mastercraftplan.ModePearls:
			pause = &l.PearlPauseUntil
		case mastercraftplan.ModeStore:
			pause = &l.StorePauseUntil
		case mastercraftplan.ModeShop:
			pause = &l.ShopPauseUntil
		case mastercraftplan.ModeSuperheat:
			pause = &l.SuperheatPauseUntil
		case mastercraftplan.ModeSmith:
			pause = &l.SmithPauseUntil
		case mastercraftplan.ModeCannonballs:
			pause = &l.Cannon.PauseUntil
		case mastercraftplan.ModeArrows:
			pause = &l.ArrowPauseUntil
		case mastercraftplan.ModeHerbs:
			pause = &l.HerbPauseUntil
		}
		switch {
		case out.Produced > 0:
			l.Succeed(out.Mode)
			*pause = time.Time{}
		case out.Transient:
			b.failMode(ctx, out.Mode, now, out.Why)
		default:
			*pause = mastercraftplan.PauseAfter(0, now, set.FruitlessPause)
		}
	case mastercraftplan.ModeDeliver:
		if out.Transient && !out.HandoverTried {
			b.failMode(ctx, out.Mode, now, out.Why)
		}
	case mastercraftplan.ModeTrawler:
		// A round that could not board (the walk, the bank, the lobby emptied, never taken aboard) backs off; one that played
		// its game clears the backoff, whatever the claim brought.
		if out.Transient {
			b.failMode(ctx, out.Mode, now, out.Why)
		} else {
			l.Succeed(out.Mode)
		}
	case mastercraftplan.ModeNature:
		// A gate round that crafted something clears its waits; one that failed for a passing reason backs off (even one that
		// did some work first: the failure repeats at the same step); one that found nothing to work with (no talisman, a free
		// world, no carrier ever came) is paused, and says why after a restart (Ledger.Rune.Gate.LastStop). A STUCK round is paused
		// whatever it made before it ran dry.
		switch {
		case out.Transient:
			b.failMode(ctx, out.Mode, now, out.Why)
			if out.Produced > 0 && !out.Stuck {
				l.Rune.Gate.PauseUntil = time.Time{}
			}
		case out.Produced > 0 || out.Handed > 0:
			// A round that only handed the mule what it held (an own trip begun with the surplus already in the pack) did its work too.
			l.Succeed(out.Mode)
			if !out.Stuck {
				l.Rune.Gate.PauseUntil = time.Time{}
			}
		case out.CampaignDecided:
			// The campaign's ledger has said what the account waits for.
			l.Rune.Gate.LastStop = out.Why
		default:
			l.Rune.Gate.PauseUntil = mastercraftplan.PauseAfter(0, now, set.FruitlessPause)
			l.Rune.Gate.LastStop = out.Why
		}
		if out.Stuck {
			l.Rune.Gate.PauseUntil = mastercraftplan.PauseAfter(0, now, set.FruitlessPause)
			if l.Rune.Gate.LastStop == "" {
				l.Rune.Gate.LastStop = out.Why
			}
			b.logf(ctx, "%s rounds are paused until %s: the round ran out of things to work with (%s)", out.Mode, l.Rune.Gate.PauseUntil.Format("15:04"), l.Rune.Gate.LastStop)
		}
	case mastercraftplan.ModeRunecraft:
		// A round that crafted or mined something clears its waits; one that failed for a passing reason backs off
		// (even one that did some work first: the failure repeats at the same step); one that found nothing to work
		// with is paused, and says why after a restart (Ledger.Rune.LastStop). A STUCK round (no essence anywhere, mining failed)
		// is paused whatever it made before it ran dry: the clean-up of its success must not clear the pause it earned.
		switch {
		case out.Transient:
			b.failMode(ctx, out.Mode, now, out.Why)
			if out.Produced > 0 && !out.Stuck {
				l.Rune.PauseUntil = time.Time{}
			}
		case out.Produced > 0:
			l.Succeed(out.Mode)
			if !out.Stuck {
				l.Rune.PauseUntil = time.Time{}
			}
		default:
			l.Rune.PauseUntil = mastercraftplan.PauseAfter(0, now, set.FruitlessPause)
			l.Rune.LastStop = out.Why
		}
		if out.Stuck {
			l.Rune.PauseUntil = mastercraftplan.PauseAfter(0, now, set.FruitlessPause)
			if l.Rune.LastStop == "" {
				l.Rune.LastStop = out.Why
			}
			b.logf(ctx, "%s rounds are paused until %s: the round ran out of things to work with (%s)", out.Mode, l.Rune.PauseUntil.Format("15:04"), l.Rune.LastStop)
		}
	}
}

// failMode backs mode off after a failure for a passing reason, and says until when.
func (b *masterCrafterBot) failMode(ctx *Context, mode mastercraftplan.Mode, now time.Time, why string) {
	_, short := b.ledger.WaitFor(mode, b.ledger.Fails(mode)+1, now, b.set)
	until := b.ledger.Fail(mode, now, b.set)
	note := ""
	if short {
		note = fmt.Sprintf(" [the gate traded for %s and its last trade was %s ago: the short backoff, not %s]",
			b.ledger.Rune.Gate.LastTrade.Sub(b.ledger.Rune.Gate.RunSince).Round(time.Minute), now.Sub(b.ledger.Rune.Gate.LastTrade).Round(time.Minute),
			mastercraftplan.BackoffFor(b.ledger.Fails(mode), b.set))
	}
	b.logf(ctx, "%s rounds fail for a passing reason (%d in a row): backing off until %s (%s)%s — %s",
		mode, b.ledger.Fails(mode), until.Format("15:04"), until.Sub(now).Round(time.Second), note, why)
}

// masterUndergroundZ is the first z of the dungeon map squares (100 squares of 64 tiles): the Giants'
// dungeon, the Waterfall, the sewers. The Mage Bank (z 4715) and the Wilderness are surface.
const masterUndergroundZ = 100 * 64

// masterNearPost is how close to the mule's post (tiles, same level) an account counts as a short
// walk from it wherever it stands.
const masterNearPost = 30

// farFromMule is the safe point's place rule: anywhere but the surface on the mule's side of White
// Wolf Mountain (consolidateSafePlace: west of it, off the Wilderness and the Mage Bank, with no
// ships, wolves or pass between the account and the post) or a short walk from the post itself.
func (b *masterCrafterBot) farFromMule(ctx *Context) bool {
	x, z, level, ok := ctx.Session.State().Tile()
	if !ok {
		return true
	}
	t := tile.Tile{X: x, Z: z, Level: level}
	if consolidateSafePlace(&t) {
		return false
	}
	post := scarabMulePresenceOf(ctx, b.mule).Post
	return t.Level != post.Level || tile.Chebyshev(t, post) > masterNearPost
}

// masterSafeBase is the part of a safe-point reading any idle activity shares: where the account
// stands, whether it is dead, fighting or hurt, a modal and a random event.
func masterSafeBase(ctx *Context) mastercraftplan.SafeInput {
	st := ctx.Session.State()
	in := mastercraftplan.SafeInput{}
	x, z, level, known := st.Tile()
	in.TileKnown = known
	if known {
		t := tile.Tile{X: x, Z: z, Level: level}
		in.Hazard = fleetplan.WildernessLevel(t) > 0 || fleetplan.InMageBank(t) || fleetplan.InKQLair(t)
		in.Underground = z >= masterUndergroundZ
		if area := waterfallplan.AreaOf(t, true); area != waterfallplan.AreaMainland && area != waterfallplan.AreaUnknown {
			in.Quest = true
		}
	}
	in.Dead = st.IsDead()
	in.InCombat = st.InCombat()
	hp, _ := st.HP()
	maxHP, _ := st.MaxHP()
	in.HP, in.MaxHP = hp, maxHP
	main, _, _ := st.Modals()
	in.ModalOpen = main != gamestate.NoModal
	in.RandomEvent = ctx.Guardian != nil && ctx.Guardian.Busy()
	return in
}

// describeCounts is a stable one-line form of a name-to-count map, for logs.
func masterDescribe(m map[string]int) string {
	if len(m) == 0 {
		return "nothing"
	}
	parts := make([]string, 0, len(m))
	for _, k := range consolidateSorted(m) {
		parts = append(parts, fmt.Sprintf("%d %s", m[k], k))
	}
	return strings.Join(parts, ", ")
}
