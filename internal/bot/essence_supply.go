package bot

// EssenceMiner's SUPPLY loop (runecraft P2, docs/runecraft_p2_2026-10-13.md): for every gateway but Aubury the
// miner is a production line for the mule's Rune essence row, which the carriers' kits, the main account's levelling, a Nature
// campaign and about 105 Priest in Peril ladders (24 each) all draw on.
//
//	read     the gateway's bank once, after a start (the essence it holds is what decides the rest)
//	mine     a stretch of `batch` essence (270, ten loads): runecraftrunner.RunStretch, the teleport in (Wizard
//	         Distentor in the Wizards' Guild at Magic 66), 27 essence, the portal, the bank at Yanille 20 steps from
//	         the exit; each load banked, the stretch ends at its goal
//	hand over  with `muleAt` essence banked (5,000 at a Guild miner): ONE bank-note stack of Rune essence is drawn,
//	         the miner walks to the mule's post and trades it (donorrunner: Offer-All moves the whole stack), then
//	         goes back to mining
//	hold     the mule's PUBLISHED Rune essence has reached `muleStopAt` (150,000): nothing more is mined until it uses
//	         some
//
// Everything is decided afresh each step by essenceplan.DecideSupply over a supplyWorld, so the loop is tested
// against a simulated world (essence_supply_test.go); essenceSupplyWorld is the live one. The Wizards' Guild's east
// door is a double door the miner's own plain walk cannot open, so every walk here is the door-aware webwalk.
//
// NEVER SEEN TO RUN LIVE: the Guild mining legs have never succeeded in the Go fleet (13 Oct 2026: 39 "timed out
// waiting for the essence mine after teleporting", 25 "Aubury refuses ... Rune Mysteries is not complete", no
// success). Every teleport's landing tile and ticks, every load's time and every bank's total is logged.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/chopmanipplan"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/essencerunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// Waits of the supply loop.
const (
	// supplyHoldPause is how long the loop waits between looks at a full mule; supplyHoldLog how often it says so.
	supplyHoldPause = 60 * time.Second
	supplyHoldLog   = 15 * time.Minute
	// supplyReadRetry is the wait after a bank that would not open.
	supplyReadRetry = 20 * time.Second
	// supplyBarrenPause is the first wait after a stretch that banked nothing; it doubles to supplyBarrenMax.
	supplyBarrenPause = 30 * time.Second
	supplyBarrenMax   = 10 * time.Minute
	// supplyIneligiblePause is the wait while the account cannot use its gateway (Magic under 66, Rune Mysteries
	// not complete); supplyRefusedPause after the gateway itself refused.
	supplyIneligiblePause = 30 * time.Second
	supplyRefusedPause    = 5 * time.Minute
	// supplyWarnEvery rate-limits a repeated warning.
	supplyWarnEvery = 5 * time.Minute
	// supplyMulePostWalk bounds the walk to the mule's post (about 450 tiles from the Guild).
	supplyMulePostWalk = 12 * time.Minute
)

// supplyHandoff is what one hand-over attempt came to.
type supplyHandoff struct {
	// Drawn is the notes drawn from the bank, Crossed the essence that left the pack for the mule.
	Drawn, Crossed int
	// Traded: the mule took the stack. Stopped: the bot was told to stop (or the session is hopping worlds).
	Traded, Stopped bool
	// BankKnown says BankEssence is the bank's essence after the attempt.
	BankKnown   bool
	BankEssence int
	Why         string
}

// supplyBank is what a visit to the gateway's bank read.
type supplyBank struct {
	// Essence is the unnoted Rune essence the bank holds; Coins the account's coins, bank and pack.
	Essence, Coins int
	// Pick says a usable pickaxe is carried, worn or in the bank; Owned names every pickaxe held, worn or banked.
	Pick  bool
	Owned []string
	// KitMissing names what the knife-on-log batch (mineTickManip) lacks after the visit drew all it could from the bank: "Knife",
	// "Logs" or both. Empty when the setting is off or the pack holds the kit. Kit says the pack holds the whole kit.
	KitMissing []string
	Kit        bool
}

// kitAsker is the part of the world that can ask the mule for the batch's knife and log; a world without it leaves the ask out.
type kitAsker interface {
	// AskKit asks the mule for the missing kit lines and reports what arrived or why nothing did, and whether the account walked
	// to the mule's post to ask (a walk of about 450 tiles from the Guild, so a failure after one is not tried again soon).
	AskKit(missing []string) (got, why string, walked bool)
}

// supplyWorld is the loop's world: what it reads and what each action does.
type supplyWorld interface {
	Now() time.Time
	Stopped() bool
	// Pause waits d; false when the bot was stopped meanwhile.
	Pause(d time.Duration) bool
	// Eligible says whether the account can use its gateway at all, and why not.
	Eligible() (ok bool, why string)
	// InMine says the account stands inside the essence mine (a start in the middle of a stretch).
	InMine() bool
	// MuleEssence is the mule's published Rune essence; known is false when it has not published (or the row is stale).
	MuleEssence() (n int, known bool)
	// ReadBank visits the gateway's bank, banks anything carried but the pickaxe and reads what it holds.
	ReadBank() (b supplyBank, ok bool)
	// AskPickaxe asks the mule for the best pickaxe it publishes that this account can use, once the account has none
	// anywhere; it reports the item that arrived or why none did.
	AskPickaxe() (item, why string)
	// MiningLevel is the account's Mining level; BuyPickaxe makes the trip to Nurmof's shop for the first of offers it can
	// get (the opt-in buyPickaxe), reporting the item bought or why none was.
	MiningLevel() int
	BuyPickaxe(offers []essenceplan.UpgradeOffer) (item, why string)
	// Mine runs a mining stretch that banks goal more essence.
	Mine(goal int) runecraftrunner.StretchResult
	// Handoff draws send essence from the bank as one note stack and trades it to the mule.
	Handoff(send int) supplyHandoff
}

// supplyRun is the loop's memory across steps.
type supplyRun struct {
	set essenceplan.Settings
	// muleAt is the live threshold (set.MuleAt unless the control panel overrode it).
	muleAt int

	bankKnown   bool
	bankEssence int
	nextHandoff time.Time
	manual      bool
	barren      int
	refusedTill time.Time
	pickAskedAt time.Time
	// The pickaxe upgrade: the tier last looked at (nothing to do until the Mining level moves the best tier), the wait
	// after a failed trip, and the pickaxes bought.
	checkedPick string
	upgradeNext time.Time
	pickBought  int
	// kitAskedAt is when the mule was last asked for the batch's knife and log, and kitAskEvery how long to wait before the next ask.
	kitAskedAt  time.Time
	kitAskEvery time.Duration

	// The run's figures.
	startedAt                                    time.Time
	stretches, loads, mined, handoffs, delivered int
	mineTime                                     time.Duration

	lastHold time.Time
	warned   map[string]time.Time

	// onHandover, when set, is told every hand-over the trade was seen to complete, with the essence that crossed: the
	// Loot panel's Essence supply card (internal/loot/runecraft.go). Set by supplyStep; a test sets its own.
	onHandover func(crossed int)
}

func newSupplyRun(set essenceplan.Settings, now time.Time) *supplyRun {
	return &supplyRun{set: set, muleAt: set.MuleAt, startedAt: now, warned: map[string]time.Time{}}
}

func (st *supplyRun) warn(w supplyWorld, logf func(string, ...any), key string, format string, args ...any) {
	if at, ok := st.warned[key]; ok && w.Now().Sub(at) < supplyWarnEvery {
		return
	}
	st.warned[key] = w.Now()
	logf("WARNING: "+format, args...)
}

// handoverOff says why nothing can be handed over, or "" when a hand-over is possible: the same test as every other
// script's (donorplan.Settings.Enabled: a mule is named and the threshold is above 0), made with the LIVE threshold so a
// muleAt moved from the control panel counts. A blank mule is a player named "" that the trade waits for until it gives up.
func (st *supplyRun) handoverOff() string {
	d := donorplan.NewSettings(st.set.Mule, st.muleAt, st.set.MuleKeep)
	switch {
	case d.Mule == "":
		return "no mule is set"
	case d.MuleAt <= 0:
		return "muleAt is 0"
	}
	return ""
}

// view is essenceplan.DecideSupply's reading of the run and the world. With hand-overs off (no mule, muleAt 0) it offers
// no threshold and no manual request, so no decision can be a trip to nobody.
func (st *supplyRun) view(w supplyWorld) essenceplan.SupplyView {
	n, known := w.MuleEssence()
	v := essenceplan.SupplyView{
		Now: w.Now(), BankKnown: st.bankKnown, BankEssence: st.bankEssence,
		MuleAt: st.muleAt, MuleStopAt: st.set.MuleStopAt, Batch: st.set.Batch,
		MuleKnown: known, MuleEssence: n, Delivered: st.delivered, NextHandoff: st.nextHandoff, Manual: st.manual,
	}
	if st.handoverOff() != "" {
		v.HandoverOff, v.MuleAt, v.Manual = true, 0, false
	}
	return v
}

// step does one thing: read the bank, mine a stretch, hand over, or hold. logf lines are already prefixed by the caller.
func (st *supplyRun) step(w supplyWorld, logf func(format string, args ...any)) {
	if w.Stopped() {
		return
	}
	if now := w.Now(); now.Before(st.refusedTill) {
		w.Pause(min(supplyRefusedPause, st.refusedTill.Sub(now)))
		return
	}
	if ok, why := w.Eligible(); !ok {
		st.warn(w, logf, "eligible", "supply: this account cannot use the %s gateway: %s", st.set.Gateway, why)
		w.Pause(supplyIneligiblePause)
		return
	}
	if !st.bankKnown && w.InMine() {
		// A start inside the mine (the bank is a separate region, no walk reaches it from here): finish the load, take the
		// portal out and bank it - the stretch does all three - and read the bank from there.
		st.mine(w, logf, essenceplan.SupplyDecision{Action: essenceplan.SupplyMine, Goal: essenceplan.LoadSize, Source: "not yet read",
			Why: "the account starts inside the essence mine: this load is finished and banked first"})
		return
	}
	d := essenceplan.DecideSupply(st.view(w))
	if st.manual && d.Action != essenceplan.SupplyHandoff && d.Action != essenceplan.SupplyRead {
		// A hand-over asked for from the control panel is for what is banked NOW; with less than a load there is nothing to
		// hand over, and the request does not wait for the next load to turn up.
		st.manual = false
		if off := st.handoverOff(); off != "" {
			logf("supply: a hand-over was asked for, but %s - nothing is handed over", off)
		} else {
			logf("supply: a hand-over was asked for, but the bank holds %s Rune essence - nothing to hand over", bankWord(st))
		}
	}
	switch d.Action {
	case essenceplan.SupplyRead:
		b, ok := w.ReadBank()
		if !ok {
			st.warn(w, logf, "bank", "supply: could not reach or open the %s bank - retrying", st.set.GatewayInfo().BankName)
			w.Pause(supplyReadRetry)
			return
		}
		st.bankKnown, st.bankEssence = true, b.Essence
		logf("supply: the %s bank holds %s Rune essence (muleAt %s, muleStopAt %s); a usable pickaxe is %s", st.set.GatewayInfo().BankName,
			runecraftplan.Thousands(b.Essence), runecraftplan.Thousands(st.muleAt), runecraftplan.Thousands(st.set.MuleStopAt), yesNo(b.Pick))
		if !b.Pick {
			if !st.askPickaxe(w, logf) {
				st.maybeUpgrade(w, logf, b) // the mule has none to give: buy one, when buyPickaxe allows
			}
		} else {
			st.maybeUpgrade(w, logf, b)
		}
		if len(b.KitMissing) > 0 {
			st.askKit(w, logf, b.KitMissing)
		}

	case essenceplan.SupplyMine:
		st.mine(w, logf, d)

	case essenceplan.SupplyHandoff:
		st.handoff(w, logf, d)

	case essenceplan.SupplyHold:
		if st.lastHold.IsZero() || w.Now().Sub(st.lastHold) >= supplyHoldLog {
			st.lastHold = w.Now()
			logf("supply: HOLDING - %s", d.Why)
		}
		w.Pause(supplyHoldPause)
	}
}

func (st *supplyRun) mine(w supplyWorld, logf func(string, ...any), d essenceplan.SupplyDecision) {
	gw := st.set.GatewayInfo()
	logf("supply: mining %s essence at the %s gateway (%s): %s; the mule holds %s (%s)", runecraftplan.Thousands(d.Goal), gw.Key, gw.NPC,
		d.Why, runecraftplan.Thousands(d.Stock), d.Source)
	res := w.Mine(d.Goal)
	st.stretches++
	st.mined += res.Banked
	st.loads += res.Loads
	st.mineTime += res.Took
	if res.BankEssence >= 0 {
		st.bankKnown, st.bankEssence = true, res.BankEssence
	} else if res.Banked > 0 && st.bankKnown {
		st.bankEssence += res.Banked
	}
	rate := 0
	if res.Took > 0 && res.Banked > 0 {
		rate = int(float64(res.Banked) / res.Took.Hours())
	}
	logf("supply: stretch over after %s: %s essence banked in %d load(s) (%s/h), %d teleport(s), %d failure(s); the bank holds %s; %s",
		res.Took.Round(time.Second), runecraftplan.Thousands(res.Banked), res.Loads, runecraftplan.Thousands(rate), res.Teleports, res.Fails,
		bankWord(st), res.Reason)
	switch {
	case res.Refused:
		st.refusedTill = w.Now().Add(supplyRefusedPause)
		st.warn(w, logf, "refused", "supply: %s refused the teleport (%s) - nothing can be mined from this account until that is fixed; looking again in %s", gw.NPC, res.Reason, supplyRefusedPause)
	case res.InMine:
		st.warn(w, logf, "inmine", "supply: the stretch ended with the account still INSIDE the mine (%s)", res.Reason)
	case res.Banked <= 0:
		st.barren++
		wait := supplyBarrenPause << min(st.barren-1, 5)
		if wait > supplyBarrenMax {
			wait = supplyBarrenMax
		}
		st.warn(w, logf, "barren", "supply: %d stretch(es) in a row banked nothing (%s) - waiting %s before the next", st.barren, res.Reason, wait)
		st.bankKnown = false // the next step reads the bank again, which also notices a pickaxe that is gone
		w.Pause(wait)
	default:
		st.barren = 0
		if st.set.BuyPickaxe && res.Banked > 0 && essenceplan.NeedsPickaxeCheck(st.checkedPick, st.set.Pickaxe, w.MiningLevel()) {
			st.bankKnown = false // Mining moved the best tier the level allows: the next step reads the bank and looks at the shop
		}
	}
}

// maybeUpgrade is the opt-in pickaxe upgrade (buyPickaxe): when Mining has moved the best tier the level allows past the best
// pickaxe the account owns, and its coins pay for a better one, one trip to Nurmof's shop in the Dwarven Mine for the best
// that the shelf has. A tier looked at is not looked at again until the level moves it; a failed trip waits a while.
//
// An account with no usable pickaxe at all (b.Pick false, and the mule had none to give) makes the trip whatever the batch
// kit and the tier last looked at say: the lost-pickaxe random event leaves only a handle, and on 19 Oct 2026 five of the
// seven miners had lost theirs and banked nothing for hours, skipping the trip because their kit was held.
func (st *supplyRun) maybeUpgrade(w supplyWorld, logf func(string, ...any), b supplyBank) {
	if !st.set.BuyPickaxe {
		return
	}
	if b.Pick && st.set.MineTickManip && len(b.KitMissing) == 0 && b.Kit {
		// With the knife-on-log batch every pickaxe rolls every 3 ticks (a bronze one's own 8 included), so a 25-minute trip to
		// Nurmof's shop buys nothing; without the batch (no kit, a link too slow for it) the upgrade is wanted again.
		st.warn(w, logf, "upgrade-manip", "supply: pickaxe upgrades are skipped while the mining batch has its kit: every pickaxe rolls every 3 ticks with it")
		return
	}
	level := w.MiningLevel()
	if level <= 0 || (b.Pick && !essenceplan.NeedsPickaxeCheck(st.checkedPick, st.set.Pickaxe, level)) {
		return
	}
	target, ok := essenceplan.DesiredPickaxe(st.set.Pickaxe, level)
	if !ok {
		return
	}
	d := essenceplan.DecideUpgrade(essenceplan.UpgradeView{
		Now: w.Now(), Selection: st.set.Pickaxe, MiningLevel: level, Owned: b.Owned, Coins: b.Coins, NextTry: st.upgradeNext,
	})
	if !d.Buy {
		if !d.Wait {
			st.checkedPick = target.Item // nothing to buy at this level: until Mining moves the tier there is nothing to look at
		}
		logf("supply: pickaxe at Mining %d: %s", level, d.Why)
		return
	}
	if !b.Pick && st.set.MineTickManip && b.Kit && len(b.KitMissing) == 0 {
		// The batch makes every pickaxe roll every 3 ticks, and the lost-pickaxe event takes a rune head as readily as a
		// bronze one: the cheapest on the shelf first.
		slices.Reverse(d.Offers)
	}
	logf("supply: PICKAXE upgrade at Mining %d - %s; the trip to Nurmof's shop in the Dwarven Mine, coins carried %s, trying %s",
		level, d.Why, runecraftplan.Thousands(d.Offers[0].Carry), offerNames(d.Offers))
	item, why := w.BuyPickaxe(d.Offers)
	if item == "" {
		st.upgradeNext = w.Now().Add(essenceplan.UpgradeRetry)
		st.warn(w, logf, "upgrade", "supply: the pickaxe trip bought nothing (%s) - the next not before %s", firstNonEmpty(why, "no reason given"), st.upgradeNext.Format(time.Kitchen))
		st.bankKnown = false
		return
	}
	st.pickBought++
	if item == target.Item {
		st.checkedPick = target.Item
	} else {
		st.upgradeNext = w.Now().Add(essenceplan.UpgradeRetry) // the best tier's shelf was empty: look again later
	}
	logf("supply: PICKAXE bought - a %s (%.0f essence/h against %.0f); %s", item, essenceplan.EssencePerHour(item), d.OwnedRate, firstNonEmpty(why, "back on the surface"))
	st.bankKnown = false
}

func offerNames(offers []essenceplan.UpgradeOffer) string {
	names := make([]string, 0, len(offers))
	for _, o := range offers {
		names = append(names, strings.ToLower(o.Item))
	}
	return strings.Join(names, ", then ")
}

// supplyPickAskEvery is how often the mule is asked for a pickaxe.
const supplyPickAskEvery = 30 * time.Minute

// askPickaxe asks the mule for a pickaxe when the account has none anywhere (at most every supplyPickAskEvery): the
// mule holds a few bronze ones, and an account taken from ScarabSwarm often has none in its own bank. It reports whether
// the mule gave one.
func (st *supplyRun) askPickaxe(w supplyWorld, logf func(string, ...any)) bool {
	now := w.Now()
	if !st.pickAskedAt.IsZero() && now.Sub(st.pickAskedAt) < supplyPickAskEvery {
		st.warn(w, logf, "pickaxe", "supply: no usable pickaxe in the pack, the worn items or the bank (asked the mule at %s)", st.pickAskedAt.Format(time.Kitchen))
		return false
	}
	st.pickAskedAt = now
	if st.set.Donor().Mule == "" {
		st.warn(w, logf, "pickaxe", "supply: no usable pickaxe in the pack, the worn items or the bank, and no mule is set to ask - put one in the bank")
		return false
	}
	item, why := w.AskPickaxe()
	if item == "" {
		st.warn(w, logf, "pickaxe", "supply: no usable pickaxe, and the mule gave none (%s) - put one in the bank", firstNonEmpty(why, "no answer"))
		return false
	}
	logf("supply: the mule gave a %s", item)
	st.bankKnown = false // it is in the pack; the next read finds it and the stretch uses it
	return true
}

// The mule is asked for the batch's knife and log at most every supplyKitAskEvery, and after an ask that walked to its post and
// brought nothing back (it declined, did not answer) at most every supplyKitAskAfterWalk: the walk is the cost, a missing kit costs
// only the gain.
const (
	supplyKitAskEvery     = 30 * time.Minute
	supplyKitAskAfterWalk = 3 * time.Hour
)

// askKit asks the mule for the knife and the plain log of the mining batch when neither the pack nor the bank has them (at most
// every supplyKitAskEvery). The account mines plain meanwhile: a missing kit costs nothing but the gain.
func (st *supplyRun) askKit(w supplyWorld, logf func(string, ...any), missing []string) {
	ka, ok := w.(kitAsker)
	if !ok {
		return
	}
	now := w.Now()
	every := st.kitAskEvery
	if every == 0 {
		every = supplyKitAskEvery
	}
	if !st.kitAskedAt.IsZero() && now.Sub(st.kitAskedAt) < every {
		return
	}
	st.kitAskedAt, st.kitAskEvery = now, supplyKitAskEvery
	if st.set.Donor().Mule == "" {
		st.warn(w, logf, "kit", "supply: mineTickManip is on but the pack and the bank hold no %s, and no mule is set to ask - put them in the bank", strings.Join(missing, " and "))
		return
	}
	got, why, walked := ka.AskKit(missing)
	if got == "" {
		if walked {
			st.kitAskEvery = supplyKitAskAfterWalk
		}
		st.warn(w, logf, "kit", "supply: no %s for the mining batch, and the mule gave none (%s) - mining plain, asking again in %s", strings.Join(missing, " and "), firstNonEmpty(why, "no answer"), st.kitAskEvery)
		return
	}
	logf("supply: the mule gave %s for the mining batch", got)
	st.bankKnown = false // it is in the pack; the next read keeps it and the stretch uses it
}

func yesNo(b bool) string {
	if b {
		return "held"
	}
	return "MISSING"
}

func bankWord(st *supplyRun) string {
	if !st.bankKnown {
		return "unknown"
	}
	return runecraftplan.Thousands(st.bankEssence)
}

func (st *supplyRun) handoff(w supplyWorld, logf func(string, ...any), d essenceplan.SupplyDecision) {
	logf("supply: HANDOFF due - %s; drawing %s essence as one note stack for %s", d.Why, runecraftplan.Thousands(d.Send), st.set.Donor().Mule)
	r := w.Handoff(d.Send)
	st.manual = false
	if r.BankKnown {
		st.bankKnown, st.bankEssence = true, r.BankEssence
	} else if r.Drawn > 0 {
		st.bankKnown = false // the bank was opened and something happened to the stack: read it again
	}
	switch {
	case r.Stopped:
		logf("supply: hand-over interrupted (%s)", firstNonEmpty(r.Why, "stop request"))
	case r.Traded && r.Crossed > 0:
		st.handoffs++
		st.delivered += r.Crossed
		st.nextHandoff = time.Time{}
		if st.onHandover != nil {
			st.onHandover(r.Crossed)
		}
		logf("supply: HANDOVER #%d - %s Rune essence crossed to %s as one note stack (drawn %s); this run has handed over %s in all, the mule's stock was %s; the bank holds %s",
			st.handoffs, runecraftplan.Thousands(r.Crossed), st.set.Donor().Mule, runecraftplan.Thousands(r.Drawn), runecraftplan.Thousands(st.delivered),
			runecraftplan.Thousands(d.Stock), bankWord(st))
	default:
		st.nextHandoff = w.Now().Add(donorplan.HandoffRetry)
		logf("supply: hand-over failed (%s) - mining goes on, the next try no sooner than %s", firstNonEmpty(r.Why, "nothing crossed"), st.nextHandoff.Format(time.Kitchen))
	}
}

// ---- the bot ----

// supplyStep is one Loop pass of the supply loop.
func (b *essenceBot) supplyStep(ctx *Context, threshold int) {
	if b.supply == nil {
		b.supply = newSupplyRun(b.settings, time.Now())
	}
	st := b.supply
	st.muleAt = threshold
	st.onHandover = func(crossed int) { lootHandover(ctx, crossed) }
	if b.handoffPending {
		st.manual = true
		b.handoffPending = false
	}
	w := &essenceSupplyWorld{b: b, ctx: ctx, gw: b.settings.GatewayInfo()}
	w.sd = b.scarabDeps(ctx)
	w.d = runecraftrunner.EssenceDeps(w.sd)
	w.d.Manip = b.mineManip
	if off := st.handoverOff(); off != "" && !b.warnedNoMule {
		b.warnedNoMule = true
		ctx.logf("essence: supply: WARNING: %s, so nothing is ever handed over (not by muleAt, the cap or a control-panel request) - the essence stays in the bank", off)
	}
	st.step(w, func(f string, a ...any) { ctx.logf("essence: "+f, a...) })
}

// scarabDeps is the scarabrunner Deps the supply loop walks, banks and trades with.
func (b *essenceBot) scarabDeps(ctx *Context) *fleetrunner.Deps {
	return &fleetrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Walker: b.walker,
		Log: ctx.Log, Loot: ctx.Loot, Me: ctx.Username, World: ctx.World(),
	}
}

// supplyMineEnv is the mining stretch's world: the live one unless a test replaced it.
var supplyMineEnv = runecraftrunner.NewLiveEnv

// essenceSupplyWorld is the supply loop's live world.
type essenceSupplyWorld struct {
	b   *essenceBot
	ctx *Context
	sd  *fleetrunner.Deps
	d   *essencerunner.Deps
	gw  essenceplan.Gateway
}

func (w *essenceSupplyWorld) Now() time.Time             { return time.Now() }
func (w *essenceSupplyWorld) Stopped() bool              { return w.ctx.Stopped() }
func (w *essenceSupplyWorld) Pause(d time.Duration) bool { return w.ctx.Delay(d) }
func (w *essenceSupplyWorld) walk(dest tile.Tile, radius int, timeout time.Duration) bool {
	return fleetrunner.WalkTo(w.sd, dest, radius, timeout)
}

func (w *essenceSupplyWorld) Eligible() (bool, string) {
	magic := fleetrunner.MagicLevel(w.sd)
	if magic <= 0 {
		return false, "Magic has not been read yet"
	}
	if ok, why := w.gw.Allows(magic); !ok {
		return false, why
	}
	// A colour never sent (Unknown) is not held against the account: the gateway's refusal, read at once, says so.
	if status, known := quests.Status(w.ctx.Session.State(), essenceplan.RuneMysteries); known && (status == quests.NotStarted || status == quests.InProgress) {
		return false, essenceplan.RuneMysteries + " is not complete"
	}
	return true, ""
}

func (w *essenceSupplyWorld) InMine() bool { return essencerunner.InMine(w.d) }

func (w *essenceSupplyWorld) MuleEssence() (int, bool) {
	stock := muleBankCount(w.ctx, w.b.settings.Mule)
	if stock == nil {
		return 0, false
	}
	n, _ := stock(essenceplan.Essence)
	return max(0, n), true
}

// keepPick is the pickaxe a visit leaves in the pack.
//
// With mineTickManip on it is the knife and the plain log of the batch as well: they stay in the pack through every visit (the
// Rune essence note stack of a hand-over is the only thing that crosses to the mule, whatever else the pack holds).
func (w *essenceSupplyWorld) keepPick() []string {
	keep := essencerunner.KitKeep(w.d)
	if pick := essenceplan.HeldPickaxeToKeep(w.b.settings.Pickaxe, essencerunner.MiningLevel(w.d), essencerunner.HeldNames(w.d)); pick != "" {
		return append([]string{pick}, keep...)
	}
	return keep
}

func (w *essenceSupplyWorld) ReadBank() (supplyBank, bool) {
	var out supplyBank
	bank, ok := runecraftrunner.OpenBank(w.sd, w.gw.Bank)
	if !ok || bank == nil {
		return out, false
	}
	defer func() { _ = bank.Close() }()
	// Whatever the pack carries but the pickaxe (a stack left by a hand-over that failed) is banked first, so the figure
	// is the whole of it.
	essencerunner.DepositAllExcept(w.d, bank, w.keepPick())
	// The first visit of a run with mineTickManip on draws the knife and the log, so the first load is already a batch run.
	essencerunner.DrawKit(w.d, bank)
	out.KitMissing = w.kitMissing()
	out.Kit = w.d.Manip != nil && w.d.Manip.Enabled && essencerunner.KitWanted(w.d) && essencerunner.HasKit(w.d)
	out.Essence, _, _ = runecraftrunner.Counts(bank.Items())
	held, banked := essencerunner.HeldNames(w.d), essencerunner.BankNames(w.d, bank)
	res := essenceplan.ResolvePick(w.b.settings.Pickaxe, essencerunner.MiningLevel(w.d), held, banked)
	out.Pick = res.Kind != essenceplan.PickStop
	seen := map[string]bool{}
	for _, n := range append(append([]string{}, held...), banked...) {
		if essenceplan.IsPickaxe(n) && !seen[strings.ToLower(n)] {
			seen[strings.ToLower(n)] = true
			out.Owned = append(out.Owned, n)
		}
	}
	out.Coins = essencerunner.Coins(w.d) + coinsInBank(w.d, bank)
	return out, true
}

// kitMissing names the lines of the mining batch's kit the pack lacks (nil when mineTickManip is off or it is complete).
func (w *essenceSupplyWorld) kitMissing() []string {
	if !essencerunner.KitWanted(w.d) {
		return nil
	}
	keep := essencerunner.KitKeep(w.d)
	var missing []string
	if !containsFold(keep, woodcutplan.Knife) {
		missing = append(missing, woodcutplan.Knife)
	}
	if !containsFold(keep, chopmanipplan.PlainLogs) {
		missing = append(missing, chopmanipplan.PlainLogs)
	}
	return missing
}

// AskKit asks the mule for the missing lines of the mining batch's kit, one of each, that it publishes: the walk to its post and one
// bundled kit ask with the pack banked first, as AskPickaxe does. It reports what arrived or why nothing did.
func (w *essenceSupplyWorld) AskKit(missing []string) (string, string, bool) {
	set, ctx := w.b.settings, w.ctx
	p := scarabMulePresenceOf(ctx, set.Mule)
	if !p.Heard {
		return "", "the mule is not here: " + p.Why, false
	}
	stock := muleBankCount(ctx, set.Mule)
	if stock == nil {
		return "", "the mule has not published its bank", false
	}
	var lines []fleetplan.ItemCount
	var names []string
	for _, item := range missing {
		if n, _ := stock(item); n > 0 {
			lines = append(lines, fleetplan.ItemCount{Item: item, Count: 1})
			names = append(names, item)
		}
	}
	if len(lines) == 0 {
		return "", "the mule publishes no " + strings.Join(missing, " or "), false
	}
	if !w.walk(p.Post, 3, supplyMulePostWalk) {
		return "", "the walk to the mule's post did not arrive (at " + fleetrunner.HereIs(w.sd) + ")", true
	}
	ctx.logf("essence: supply: asking %s for %s (the mining batch's kit)", set.Mule, strings.Join(names, " and "))
	kit, info := scarabKitBundle(w.sd, ctx.Bus, ctx.Username, set.Mule, lines, fleetrunner.KitBundleOptions{
		Timeout: donorrunner.DefaultKitTimeout, Bank: p.Post, KeepNotes: true, PerOrder: 1,
		FallbackTimeout: 90 * time.Second, World: ctx.CurrentWorld,
	})
	var got []string
	for _, l := range lines {
		if kit.Got[l.Item] > 0 {
			got = append(got, l.Item)
		}
	}
	if len(got) > 0 {
		return strings.Join(got, " and "), "", true
	}
	why := info.Reason
	for _, d := range kit.Declined {
		why = d
	}
	return "", firstNonEmpty(why, "nothing arrived"), true
}

// coinsInBank is the coins an open bank holds.
func coinsInBank(d *essencerunner.Deps, bank *actions.Bank) int {
	return bankNamedCount(bank, d, "Coins")
}

func (w *essenceSupplyWorld) MiningLevel() int { return essencerunner.MiningLevel(w.d) }

// BuyPickaxe is the trip: the coins drawn from the Yanille bank (the best offer's price and a quarter on top), the
// door-aware walk to the Dwarven Mine's trapdoor, Nurmof's shelf (the best offer he has), and the ladder back up. The
// leftover coins are banked by the next visit.
// pickaxeTripCoins is the coins a trip to Nurmof carries: the dearest offer it may buy (an empty shelf moves it down the list)
// and the fares there and back.
func pickaxeTripCoins(offers []essenceplan.UpgradeOffer) int {
	carry := 0
	for _, o := range offers {
		carry = max(carry, o.Carry)
	}
	return carry + essenceplan.NurmofFareCoins
}

func (w *essenceSupplyWorld) BuyPickaxe(offers []essenceplan.UpgradeOffer) (string, string) {
	if len(offers) == 0 {
		return "", "no offers"
	}
	bank, ok := runecraftrunner.OpenBank(w.sd, w.gw.Bank)
	if !ok || bank == nil {
		return "", "could not reach or open the " + w.gw.BankName + " bank for the coins"
	}
	if need := pickaxeTripCoins(offers) - essencerunner.Coins(w.d); need > 0 && !essencerunner.WithdrawXOf(w.d, bank, "Coins", need) {
		_ = bank.Close()
		return "", fmt.Sprintf("could not draw %d coins from the bank", need)
	}
	_ = bank.Close()
	if !w.walk(essenceplan.NurmofHopFrom, 2, 20*time.Minute) {
		return "", "the walk to the Dwarven Mine trapdoor did not arrive (at " + fleetrunner.HereIs(w.sd) + ")"
	}
	items := make([]string, 0, len(offers))
	for _, o := range offers {
		items = append(items, o.Item)
	}
	return essencerunner.BuyPickaxeAtNurmof(w.d, items)
}

// AskPickaxe asks the mule for the best pickaxe it publishes that this account's Mining level can use (the named tier
// when the pickaxe setting names one): walks to its post, one bundled kit ask with the pack banked first.
func (w *essenceSupplyWorld) AskPickaxe() (string, string) {
	set, ctx := w.b.settings, w.ctx
	p := scarabMulePresenceOf(ctx, set.Mule)
	if !p.Heard {
		return "", "the mule is not here: " + p.Why
	}
	stock := muleBankCount(ctx, set.Mule)
	if stock == nil {
		return "", "the mule has not published its bank"
	}
	level := essencerunner.MiningLevel(w.d)
	item := ""
	for _, t := range essenceplan.PickTiers { // best first
		if level < t.Level || (!strings.EqualFold(set.Pickaxe, essenceplan.BestAvailable) && !strings.EqualFold(set.Pickaxe, t.Tier)) {
			continue
		}
		if n, _ := stock(t.Item); n > 0 {
			item = t.Item
			break
		}
	}
	if item == "" {
		return "", "the mule publishes no pickaxe this account can use at Mining " + strconv.Itoa(level)
	}
	if !w.walk(p.Post, 3, supplyMulePostWalk) {
		return "", "the walk to the mule's post did not arrive (at " + fleetrunner.HereIs(w.sd) + ")"
	}
	ctx.logf("essence: supply: asking %s for a %s", set.Mule, item)
	kit, info := scarabKitBundle(w.sd, ctx.Bus, ctx.Username, set.Mule, []fleetplan.ItemCount{{Item: item, Count: 1}}, fleetrunner.KitBundleOptions{
		Timeout: donorrunner.DefaultKitTimeout, Bank: p.Post, KeepNotes: true, PerOrder: 1,
		FallbackTimeout: 90 * time.Second, World: ctx.CurrentWorld,
	})
	if kit.Got[item] > 0 {
		return item, ""
	}
	why := info.Reason
	for _, d := range kit.Declined {
		why = d
	}
	return "", firstNonEmpty(why, "nothing arrived")
}

func (w *essenceSupplyWorld) Mine(goal int) runecraftrunner.StretchResult {
	set := w.b.settings
	env := supplyMineEnv(w.sd, runecraftrunner.LiveOptions{Gateway: w.gw, Walk: w.walk, Pickaxe: set.Pickaxe, WalkTimeout: 10 * time.Minute, Manip: w.b.mineManip})
	// A load is 2.6 minutes at bronze; six is room for the walk, the door and a bank that opens slowly.
	budget := time.Duration(goal/essenceplan.LoadSize+4) * 6 * time.Minute
	return runecraftrunner.RunStretch(env, runecraftrunner.StretchOptions{
		Goal: goal, Gateway: w.gw, MaxFails: 4, Budget: budget,
		Logf: func(f string, a ...any) { w.ctx.logf(f, a...) },
	})
}

// Handoff is the hand-over of one note stack: the gateway's bank gives up send essence as notes, the walk to the mule's
// post, the trade (Offer-All: the whole stack in one click), and the bank at the post for whatever did not cross.
func (w *essenceSupplyWorld) Handoff(send int) (r supplyHandoff) {
	set := w.b.settings
	ctx := w.ctx
	if set.Donor().Mule == "" {
		// scarabMulePresenceOf answers Heard with the Catherby post for a blank name, so without this the stack would be drawn
		// and walked across the map to a trade that waits for a player named "" (DecideSupply never asks for it either).
		r.Why = "no mule is set"
		return r
	}
	p := scarabMulePresenceOf(ctx, set.Mule)
	if !p.Heard {
		r.Why = "the mule is not here: " + p.Why // nothing is drawn, and the walk to Catherby is not made for nobody
		return r
	}
	stand := p.Post
	if t, ok := set.Meet(); ok {
		stand = t
	}
	bank, ok := runecraftrunner.OpenBank(w.sd, w.gw.Bank)
	if !ok || bank == nil {
		r.Why = "could not reach or open the " + w.gw.BankName + " bank"
		return r
	}
	essencerunner.DepositAllExcept(w.d, bank, w.keepPick()) // old notes and junk go in, so the draw is the one stack
	draw := essencerunner.DrawNoteStack(w.d, bank, send)
	_ = bank.Close()
	r.BankKnown, r.BankEssence = true, max(0, draw.Held-draw.Drawn)
	r.Drawn = draw.Drawn
	if draw.Drawn <= 0 {
		r.Why = "nothing was drawn: " + draw.Why
		return r
	}
	ctx.logf("essence: hand-off: drew %s notes of the %s Rune essence the bank held in %s; walking to the mule's post at %d,%d",
		runecraftplan.Thousands(draw.Drawn), runecraftplan.Thousands(draw.Held), draw.Took.Round(time.Millisecond), stand.X, stand.Z)
	if !w.walk(stand, 3, supplyMulePostWalk) {
		r.Why = "the walk to the mule's post did not arrive (at " + fleetrunner.HereIs(w.sd) + ")"
		w.bankLeftovers(&r, stand)
		return r
	}
	rd := &donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		Bus: ctx.Bus, Me: ctx.Username,
	}
	WireDonorWorlds(ctx, rd)
	rules := essenceplan.StackRules()
	packRows := func() []donorplan.ItemRow { return essencerunner.PackSlotRows(w.d) }
	res := donorrunner.Run(rd, donorrunner.Options{
		Mule: set.Mule, Stand: stand, RowsInBank: 1, Drawn: 1,
		Load: func() []donorplan.LoadGroup {
			return donorplan.PlanLoad(packRows(), donorplan.DefaultMuleFreeSlots, rules)
		},
		HaulLeft:        func() int { return donorplan.HaulOf(packRows(), rules) },
		KnownUntradable: w.b.knownUntradable,
	})
	left := essencerunner.PackCountByName(w.d, essenceplan.Essence)
	r.Crossed = max(0, draw.Drawn-left)
	switch {
	case res.Stopped:
		r.Stopped, r.Why = true, firstNonEmpty(res.Reason, "stopped")
		return r
	case res.Traded && r.Crossed > 0:
		r.Traded = true
		if left > 0 {
			ctx.logf("essence: hand-off: %s of the stack did not cross; banking it at the mule's post", runecraftplan.Thousands(left))
			w.bankLeftovers(&r, stand)
		} else {
			r.BankKnown, r.BankEssence = true, max(0, draw.Held-r.Crossed)
		}
	default:
		r.Why = firstNonEmpty(res.Reason, "the trade did not happen")
		w.bankLeftovers(&r, stand)
	}
	return r
}

// bankLeftovers banks the pack at the bank by the mule's post (the Catherby bank is the post): the notes that did not
// cross, ready for the next draw, and re-reads the bank's essence.
func (w *essenceSupplyWorld) bankLeftovers(r *supplyHandoff, stand tile.Tile) {
	if essencerunner.PackCountByName(w.d, essenceplan.Essence) == 0 {
		return
	}
	bank, ok := runecraftrunner.OpenBank(w.sd, stand)
	if !ok || bank == nil {
		r.BankKnown = false // the stack is still in the pack: the next pass banks it at the gateway's bank
		return
	}
	defer func() { _ = bank.Close() }()
	essencerunner.DepositAllExcept(w.d, bank, w.keepPick())
	n, _, _ := runecraftrunner.Counts(bank.Items())
	r.BankKnown, r.BankEssence = true, n
}

// supplyBanner is the start-up line of a supply miner.
func supplyBanner(set essenceplan.Settings, mule string) string {
	gw := set.GatewayInfo()
	rate := fmt.Sprintf("about %.0f essence/h with a bronze pickaxe, %.0f with a rune one", essenceplan.EssencePerHour("Bronze pickaxe"), essenceplan.EssencePerHour("Rune pickaxe"))
	for _, t := range essenceplan.PickTiers {
		if strings.EqualFold(set.Pickaxe, t.Tier) {
			rate = fmt.Sprintf("about %.0f essence/h with a %s", essenceplan.EssencePerHour(t.Item), strings.ToLower(t.Item))
		}
	}
	hand := "no mule set (or muleAt 0): nothing is handed over"
	if set.Donor().Enabled() {
		hand = fmt.Sprintf("one note stack of Rune essence to %s at %s banked, stopping while the mule holds %s", mule,
			runecraftplan.Thousands(set.MuleAt), runecraftplan.Thousands(set.MuleStopAt))
	}
	shop := "pickaxe upgrades off (buyPickaxe false)"
	if set.BuyPickaxe {
		shop = "buyPickaxe ON: a better pickaxe is fetched from Nurmof's shop when the Mining level and the coins allow"
	}
	magic := "no Magic level needed"
	if gw.MinMagic > 0 {
		magic = fmt.Sprintf("Magic %d needed", gw.MinMagic)
	}
	return fmt.Sprintf("supply loop at the %s gateway (%s, bank %s, %s), pickaxe %q, stretches of %s essence, %s; %s; %s",
		gw.Key, gw.NPC, gw.BankName, magic, set.Pickaxe, runecraftplan.Thousands(essenceplan.RoundUpToLoads(set.Batch)), rate, hand, shop)
}
