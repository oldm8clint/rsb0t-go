package bot

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/essencerunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/minemanipplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("EssenceMiner", func() Bot { return &essenceBot{} })
}

// essenceBot ports EssenceMiner.ts's own loop() 1:1, the same shape internal/bot/fisher.go
// and internal/bot/crafter.go already proved: every Loop call re-derives the account's stage
// from live state alone (essenceplan.DecideStage) and does one stage-appropriate action,
// rather than remembering "what it was doing" across calls. A relog, a stop, or a death
// anywhere on the map resumes in the right place next Loop call for the same reason
// fisherBot's own doc comment gives: nothing here is remembered except the small bits of
// state that must survive one call to the next (which pickaxe tier was last confirmed, the
// teleport-fail streak, whether Aubury has ever refused the teleport, and the pending
// hand-off flag).
//
// The mine/portal/bank/pickaxe/teleport stage machine is essenceplan.DecideStage, ported
// directly from essencePlan.ts's minerStage. The mule hand-off is wired the same way
// fisherBot/crafterBot wire theirs: checked before the ordinary stage dispatch (so a manual
// control-panel trigger — see Context.RequestHandoff/SetMuleAt — can fire regardless of
// which stage the miner is in) and driven by internal/scripts/donorrunner.Run, which is
// already script-agnostic per its own doc comment. Unlike ProgressiveFisher, EssenceMiner.ts
// hands the whole bank over with no coin travel reserve (Aubury's teleport costs nothing), so
// donorrunner.Options.Reserve/CoinsName are left unset here.
//
// Settings, all optional (read via essenceplan.Settings, so both Go values and the
// float64/bool/string a JSON-decoded fleet.config.json roster entry produces work):
//
//	pickaxe   string  default essenceplan.BestAvailable — a specific tier ("Rune", ...) or
//	                                                       the best the Mining level allows.
//	mule      string  default ""                        — the MuleReceiver account hand-overs
//	                                                       go to; blank never hands over.
//	muleAt    int     default essenceplan.MuleEssenceAt (10,000) — banked essence that starts
//	                                                       a hand-over. Live-overridable from
//	                                                       the control panel via
//	                                                       Context.SetMuleAt.
//	muleKeep  string  default ""                        — bank rows that never go to the mule,
//	                                                       beyond pickaxes (always kept).
//	logDetail string  default "Normal"                  — "Verbose" logs every stage change
//	                                                       and step, matching vlog().
//
// The essence SUPPLY settings (runecraft P2, docs/runecraft_p2_2026-10-13.md; loading is strict, a key that is not
// one of these is an error):
//
//	gateway    string default "aubury"  — "aubury" (this loop, the Varrock east bank), "guild" (the Wizards' Guild's
//	                                       Wizard Distentor, Magic 66, the Yanille bank) or "cromperty": the last two
//	                                       run the supply loop (essence_supply.go) instead of this one.
//	muleStopAt int    default 150000    — mining stops while the mule's PUBLISHED Rune essence has reached it (0 never).
//	muleItems  string default "all" at aubury, "essence" at the others — what a hand-over carries: the whole bank
//	                                       but the pickaxes ("all"), or one note stack of Rune essence ("essence").
//	muleAtTile tile   default blank     — where the mule is met ("x,z"); blank is the post its heartbeat publishes
//	                                       (the supply loop) or the Varrock east bank (this loop).
//	batch      int    default 270       — essence one supply-loop mining stretch brings (whole loads of 27).
//	buyPickaxe bool   default false     — the supply loop may fetch a better pickaxe from Nurmof's shop when the Mining level and the coins allow.
//	mineTickManip bool default false    — the supply loop's mining stretches write a knife-on-log batch in every stale tick, so the rock rolls every 3
//	                                       ticks (essencerunner/minemanip.go); needs a Knife and a plain Log in the pack or the bank.
//	muleAt is 5000 rather than 10000 when gateway is not aubury and it is not set.
//
// Deliberately not ported (see essencerunner's own package doc comment for exactly why and
// what's left): buying a pickaxe from Nurmof when nothing better is already owned or banked.
type essenceBot struct {
	settings essenceplan.Settings

	checkedPickaxe string
	teleportFails  int
	questRefused   bool

	handoffPending     bool
	handoffNextAllowed time.Time
	handoffsDone       int
	handedOver         int
	knownUntradable    map[int]bool

	stage     string
	mined     int
	banked    int
	trips     int
	startedAt time.Time
	xpAtStart int
	warnedAt  map[string]time.Time

	// blindSince is when the backpack and worn panels last stopped resolving, and zero
	// while they read normally. See containersBlind.
	blindSince time.Time

	// The supply loop (essence_supply.go): its memory, the door-aware walker its walks use and whether the missing
	// mule has been said.
	supply       *supplyRun
	walker       *webwalk.Walker
	warnedNoMule bool
	lastHoldLog  time.Time

	// mineManip is the knife-on-log batch of the supply loop's mining stretches (setting mineTickManip), nil when off.
	mineManip *essencerunner.ManipConfig
}

func (b *essenceBot) deps(ctx *Context) *essencerunner.Deps {
	return &essencerunner.Deps{
		Session:    ctx.Session,
		Pack:       ctx.Pack,
		Locs:       ctx.Locs,
		Config:     ctx.Config,
		Components: ctx.Components,
		Chat:       ctx.Chat,
		Delay:      ctx.Delay,
		Log:        ctx.Log,
		Loot:       ctx.Loot,
	}
}

func (b *essenceBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("essence: no collision pack configured (fleet.config.json needs a packPath) — refusing to start rather than panic later inside pathfinding")
	}
	set, err := essenceplan.DecodeSettings(ctx.Settings)
	if err != nil {
		return fmt.Errorf("essence: %w", err)
	}
	b.settings = set
	b.knownUntradable = map[int]bool{}
	b.warnedAt = map[string]time.Time{}
	b.startedAt = time.Now()
	b.xpAtStart = essencerunner.MiningXP(b.deps(ctx))

	if status, ok := quests.Status(ctx.Session.State(), essenceplan.RuneMysteries); ok && status != quests.Complete {
		ctx.logf("essence: WARNING: %s is not complete — Aubury will refuse the teleport", essenceplan.RuneMysteries)
	}
	if gate, ok := essenceplan.RequiredMiningLevel(b.settings.Pickaxe); ok {
		if level := essencerunner.MiningLevel(b.deps(ctx)); level < gate {
			ctx.logf("essence: WARNING: Mining %d is required for the %s pickaxe (have %d)", gate, b.settings.Pickaxe, level)
		}
	}
	if b.settings.MineTickManip {
		if b.settings.SupplyLoop() {
			b.mineManip = &essencerunner.ManipConfig{Enabled: true, State: essencerunner.NewManipState()}
			ctx.logf("essence: mine tick manipulation is ON: a knife-on-log batch in the stale tick after every roll makes the rock roll every %d ticks (a bronze pickaxe's own is 8); the knife and one plain log are kept in the pack and drawn from the bank, a load is 25 essence", minemanipplan.ManipPeriod)
		} else {
			ctx.logf("essence: WARNING: mineTickManip is a supply-loop setting (gateway guild or cromperty); the Aubury loop mines plain")
		}
	}
	if b.settings.SupplyLoop() {
		gw := b.settings.GatewayInfo()
		if ok, why := gw.Allows(scarabMagicOf(ctx)); !ok && scarabMagicOf(ctx) > 0 {
			ctx.logf("essence: WARNING: %s", why)
		}
		// The same door-aware walker ScarabSwarm and MasterCrafter build: the Guild's east door is a double door a plain
		// path cannot open. Plain pathfinding is the fallback.
		if w, werr := live.NewWalker(live.Deps{
			Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
			Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		}); werr != nil {
			ctx.logf("essence: no webwalk (%v) - walks fall back to plain pathfinding", werr)
		} else {
			if ctx.Components != nil && ctx.Config != nil {
				hops.Install(w, hops.New(hops.Deps{Client: w.Client, Session: ctx.Session, Config: ctx.Config, Components: ctx.Components, Log: ctx.Log}))
			}
			b.walker = w
		}
		ctx.logf("essence: %s", supplyBanner(b.settings, b.settings.Donor().Mule))
		return nil
	}
	muleMsg := "no mule set"
	if b.settings.Donor().Enabled() {
		muleMsg = fmt.Sprintf("handing over %s to %s at %d banked essence, holding mining while the mule has %d", b.handoffWhat(), b.settings.Mule, b.settings.MuleAt, b.settings.MuleStopAt)
	}
	ctx.logf("essence: pickaxe %q, bank at Varrock East, %s", b.settings.Pickaxe, muleMsg)
	return nil
}

// handoffWhat names what a hand-over of this script carries, for the log.
func (b *essenceBot) handoffWhat() string {
	if b.settings.MuleItems == essenceplan.MuleItemsEssence {
		return "one note stack of Rune essence"
	}
	return "the whole bank"
}

// scarabMagicOf is the account's Magic level as the session last reported it (0 when not yet read).
func scarabMagicOf(ctx *Context) int {
	if ctx == nil || ctx.Session == nil {
		return 0
	}
	if sk, ok := ctx.Session.State().Skill(fleetplan.MagicStat); ok {
		return sk.Level
	}
	return 0
}

func (b *essenceBot) vlog(ctx *Context, format string, args ...any) {
	if b.settings.Verbose() {
		ctx.logf("essence: "+format, args...)
	}
}

// warn rate-limits a warning to once every everyMs per key, ported from EssenceMiner.ts's
// own warn().
func (b *essenceBot) warn(ctx *Context, key string, everyMs time.Duration, format string, args ...any) {
	if time.Since(b.warnedAt[key]) < everyMs {
		return
	}
	b.warnedAt[key] = time.Now()
	ctx.logf("essence: WARNING: "+format, args...)
}

func (b *essenceBot) setStage(ctx *Context, stage string) {
	if stage != b.stage {
		b.stage = stage
		b.vlog(ctx, "-> %s", stage)
	}
}

func (b *essenceBot) pause(ctx *Context, d time.Duration) {
	ctx.Delay(d)
}

// essenceBlindLimit is how long a pass waits for the backpack and worn panels before deciding
// the stage on whatever it can read. It is sized for the only event that blinds this script —
// the mime, which a healthy Guardian clears in about four handle cycles, well under a minute —
// with room to spare. It is deliberately not a stop: past it the miner does what it did before
// this wait existed.
const essenceBlindLimit = 2 * time.Minute

// containersBlind reports whether this pass should be skipped because the backpack and worn
// panels cannot be read yet, and clears the window as soon as they can.
//
// It degrades rather than gating. Past essenceBlindLimit it answers false and lets the pass run
// on the blind read, because nothing on this path is irretractable: the deposit cannot lose the
// pickaxe (essencerunner.HeldNames' pack half reads PlayerPack, which if_settab cannot null,
// and DepositAllExcept only ever deposits from the pack) and nothing here puts an order on the
// mule bus. That is the difference from woodcutrunner, which earns a hard gate because its
// blind path can order a rune axe that no later pass retracts. A wait that never gave up would
// trade a wasted bank trip for a stalled loop, and a stalled loop is a failure this script has
// already produced five times.
func (b *essenceBot) containersBlind(ctx *Context, d *essencerunner.Deps) bool {
	if essencerunner.ContainersReadable(d) {
		b.blindSince = time.Time{}
		return false
	}
	if b.blindSince.IsZero() {
		b.blindSince = time.Now()
	}
	if time.Since(b.blindSince) >= essenceBlindLimit {
		return false
	}
	b.warn(ctx, "blind-containers", 30*time.Second, "the backpack and worn panels are not readable — a macro event nulls the sidebar tabs, so a wielded pickaxe would read as missing and send this trip to the bank for nothing. Waiting")
	return true
}

func (b *essenceBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !essencerunner.WaitForGuardian(d) {
		return nil
	}
	// A death here just drops the account near Lumbridge, far from both Aubury and the
	// Varrock bank — essenceplan.DecideStage has no special case for it (neither does
	// EssenceMiner.ts), so it simply resumes as "outside the mine, empty pack, no pickaxe
	// held" and re-walks itself back via the ordinary pickaxe/teleport stages. See
	// fisherBot's own doc comment for why the dead-flag clear is done here regardless.
	if ctx.Session.State().IsDead() {
		if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
			ctx.Session.State().ClearDead()
		}
	}
	if actions.NewTrade(ctx.Session).Active() {
		b.pause(ctx, 2*time.Second)
		return nil
	}
	dlg := actions.NewChatDialog(ctx.Session, ctx.Components)
	if dlg.CanContinue() && !actions.NewShop(ctx.Session).IsOpen() {
		_ = dlg.Continue()
		ctx.Delay(300 * time.Millisecond)
		return nil
	}

	if manual := ctx.TakeHandoffRequest(); manual {
		b.handoffPending = true
	}
	threshold := b.settings.MuleAt
	if v, ok := ctx.MuleAtOverride(); ok {
		threshold = v
	}
	donorSettings := donorplan.NewSettings(b.settings.Mule, threshold, b.settings.MuleKeep)
	if b.settings.SupplyLoop() {
		// The supply loop (essence_supply.go) decides for itself; it waits as long as the panels are blind, as this
		// loop does (a wielded pickaxe would read as missing and send the stretch to the bank for one).
		if b.containersBlind(ctx, d) {
			b.pause(ctx, time.Second)
			return nil
		}
		b.supplyStep(ctx, threshold)
		return nil
	}
	if donorSettings.Enabled() && b.handoffPending {
		return b.handoffStep(ctx, d, donorSettings)
	}

	// A blinded worn read is the one thing here that changes a decision. HeldNames' pack half
	// reads PlayerPack, which if_settab cannot take away, but its worn half goes through the
	// sidebar tab — so a WIELDED pickaxe disappears from held while a macro event holds the
	// account, ResolvePick never reaches PickHeld, and DecideStage below answers StagePickaxe
	// instead of StageTeleport. That buys a pointless Varrock East trip plus either a second
	// pickaxe withdrawn on top of the one being held or a "no usable pickaxe" warning that is
	// not true. The miner boards no ship, so the mime is the only event that does this to it.
	if b.containersBlind(ctx, d) {
		b.pause(ctx, time.Second)
		return nil
	}
	level := essencerunner.MiningLevel(d)
	held := essencerunner.HeldNames(d)
	stage := essenceplan.DecideStage(essenceplan.MinerSnapshot{
		InMine:   essencerunner.InMine(d),
		PackFull: essencerunner.PackFull(d),
		Haul:     essenceplan.MuleHaul(essencerunner.PackSlotRows(d), nil),
		PickHeld: essenceplan.ResolvePick(b.settings.Pickaxe, level, held, nil).Kind == essenceplan.PickHeld,
	})
	b.setStage(ctx, string(stage))
	switch stage {
	case essenceplan.StageMine:
		b.mineStep(ctx, d)
	case essenceplan.StagePortal:
		if !essencerunner.PortalOut(d) {
			b.pause(ctx, time.Second)
		}
	case essenceplan.StageBank:
		b.bankStep(ctx, d, donorSettings)
	case essenceplan.StagePickaxe:
		b.pickaxeStep(ctx, d, level, held)
	case essenceplan.StageTeleport:
		if b.holdAtCap(ctx) {
			return nil
		}
		if essenceplan.NeedsPickaxeCheck(b.checkedPickaxe, b.settings.Pickaxe, level) {
			b.upgradePickaxeStep(ctx, d, level, held)
		} else {
			b.teleportStep(ctx, d)
		}
	}
	return nil
}

// atCap reports the mule's PUBLISHED Rune essence and whether it has reached muleStopAt (0 never; an unpublished bank
// never does either).
func (b *essenceBot) atCap(ctx *Context) (n int, reached bool) {
	stop := b.settings.MuleStopAt
	if stop <= 0 {
		return 0, false
	}
	stock := muleBankCount(ctx, b.settings.Mule)
	if stock == nil {
		return 0, false
	}
	n, _ = stock(essenceplan.Essence)
	return n, n >= stop
}

// holdAtCap waits instead of teleporting in while the mule's PUBLISHED Rune essence has reached muleStopAt: the
// original loop's share of the stop the supply loop makes through essenceplan.DecideSupply. What is already mined is
// banked and handed over as ever.
func (b *essenceBot) holdAtCap(ctx *Context) bool {
	n, reached := b.atCap(ctx)
	if !reached {
		return false
	}
	if b.lastHoldLog.IsZero() || time.Since(b.lastHoldLog) >= supplyHoldLog {
		b.lastHoldLog = time.Now()
		ctx.logf("essence: HOLDING - %s holds %d Rune essence, at the muleStopAt cap of %d; not mining until it uses some", b.settings.Mule, n, b.settings.MuleStopAt)
	}
	b.pause(ctx, supplyHoldPause)
	return true
}

func (b *essenceBot) OnStop(ctx *Context) {
	if st := b.supply; st != nil {
		ctx.logf("essence: stopped - supply loop at the %s gateway: %d essence banked in %d load(s) over %d stretch(es) (%s mining), %d hand-over(s) crossed %d to the mule, %d pickaxe(s) bought; the bank held %s",
			st.set.Gateway, st.mined, st.loads, st.stretches, st.mineTime.Round(time.Second), st.handoffs, st.delivered, st.pickBought, bankWord(st))
		return
	}
	ctx.logf("essence: stopped — %d essence mined, %d banked in %d trips, %d slot(s) handed to the mule in %d hand-over(s)",
		b.mined, b.banked, b.trips, b.handedOver, b.handoffsDone)
}

// ---- mining ----

func (b *essenceBot) mineStep(ctx *Context, d *essencerunner.Deps) {
	res := essencerunner.Mine(d)
	b.mined += res.Gained
	switch res.Reason {
	case "no-rock":
		b.warn(ctx, "rock", 60*time.Second, "no essence rock in sight — waiting")
		b.pause(ctx, 2*time.Second)
	case "stalled":
		b.vlog(ctx, "no essence for a while — clicking the rock again")
	case "pickaxe-head":
		b.warn(ctx, "pickaxe-head", 60*time.Second, "the lost-pickaxe event took the pickaxe and the head could not be put back")
		b.pause(ctx, 5*time.Second)
	case "full":
		b.vlog(ctx, "pack full (%d essence)", essencerunner.EssenceHeld(d))
	case "dialog", "left-mine", "stopped":
		// Nothing to do — the next Loop call re-derives the stage from live state, exactly
		// like ProgressiveFisher/ProgressiveCrafter's own early returns.
	}
}

// ---- the bank ----

func (b *essenceBot) keepInPack(level int, held []string) []string {
	if pick := essenceplan.HeldPickaxeToKeep(b.settings.Pickaxe, level, held); pick != "" {
		return []string{pick}
	}
	return nil
}

// bankStep deposits everything but the pickaxe, then checks whether the freshly-read banked
// essence total starts a hand-off — ported from bankEssence().
func (b *essenceBot) bankStep(ctx *Context, d *essencerunner.Deps, donorSettings donorplan.Settings) {
	level := essencerunner.MiningLevel(d)
	held := essencerunner.HeldNames(d)
	n := essencerunner.EssenceHeld(d)
	bank, ok := essencerunner.OpenVarrockBank(d)
	if !ok {
		b.warn(ctx, "bank-open", 60*time.Second, "could not reach or open the Varrock East bank — retrying")
		b.pause(ctx, 3*time.Second)
		return
	}
	deposited := essencerunner.DepositAllExcept(d, bank, b.keepInPack(level, held))
	bankedEssence := essencerunner.PackCountByName(d, essenceplan.Essence) // recomputed below after close
	_ = bankedEssence
	// Read the bank's own essence total for the hand-off threshold — must be read from the
	// bank's contents, not the pack (which should now be empty of essence).
	bankedEssence = bankNamedCount(bank, d, essenceplan.Essence)
	_ = bank.Close()
	if deposited > 0 {
		b.trips++
		b.banked += n
		ctx.logf("essence: banked %d essence (trip %d, %d in the bank)", n, b.trips, bankedEssence)
	}
	if donorSettings.Enabled() && essenceplan.HandoffAfterBanking(bankedEssence, b.settings, time.Now(), b.handoffNextAllowed) {
		b.handoffPending = true
		ctx.logf("essence: %d essence banked — handing the lot to %s", bankedEssence, donorSettings.Mule)
	}
}

func bankNamedCount(bank *actions.Bank, d *essencerunner.Deps, name string) int {
	n := 0
	for _, it := range bank.Items() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(strings.TrimSpace(obj.Name), name) {
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// pickaxeStep fetches a usable pickaxe from the bank — ported from getPickaxe(), which (in the
// TS source too) never goes to Nurmof itself: only upgradePickaxeStep's TS twin,
// upgradePickaxe(), does that. A bank with nothing usable here is a real PickStop: warn and
// wait for the operator to put one in the bank.
func (b *essenceBot) pickaxeStep(ctx *Context, d *essencerunner.Deps, level int, held []string) {
	bank, ok := essencerunner.OpenVarrockBank(d)
	if !ok {
		b.warn(ctx, "bank-open", 60*time.Second, "could not reach or open the Varrock East bank — retrying")
		b.pause(ctx, 3*time.Second)
		return
	}
	if essencerunner.PackFull(d) {
		essencerunner.DepositAllExcept(d, bank, b.keepInPack(level, held))
	}
	bankNames := essencerunner.BankNames(d, bank)
	res := essenceplan.ResolvePick(b.settings.Pickaxe, level, held, bankNames)
	switch res.Kind {
	case essenceplan.PickStop:
		_ = bank.Close()
		b.warn(ctx, "pickaxe", 60*time.Second, "%s — put one in the bank and the miner resumes", res.Reason)
		b.pause(ctx, 3*time.Second)
		return
	case essenceplan.PickWithdraw:
		if essencerunner.WithdrawOneOf(d, bank, res.Item) {
			ctx.logf("essence: withdrew %s", res.Item)
		} else {
			b.warn(ctx, "pick-withdraw", 60*time.Second, "could not withdraw %s — retrying", res.Item)
		}
	}
	_ = bank.Close()
}

// upgradePickaxeStep checks whether a better tier than currently held is available, once per
// pickaxe-tier/level change (essenceplan.NeedsPickaxeCheck) — ported from upgradePickaxe():
// already held, then the bank, then Nurmof when the coins cover it, else keep what's held.
func (b *essenceBot) upgradePickaxeStep(ctx *Context, d *essencerunner.Deps, level int, held []string) {
	target, ok := essenceplan.DesiredPickaxe(b.settings.Pickaxe, level)
	if !ok {
		b.checkedPickaxe = ""
		return
	}
	for _, h := range held {
		if strings.EqualFold(h, target.Item) {
			b.checkedPickaxe = target.Item
			return
		}
	}
	bank, ok := essencerunner.OpenVarrockBank(d)
	if !ok {
		b.warn(ctx, "bank-open", 60*time.Second, "could not reach or open the Varrock East bank — retrying")
		return
	}
	if essencerunner.WithdrawOneOf(d, bank, target.Item) {
		ctx.logf("essence: withdrew the best usable pickaxe, a %s", target.Item)
		b.checkedPickaxe = target.Item
		_ = bank.Close()
		return
	}

	cost, hasCost := essenceplan.PickaxeCost(target.Item)
	held0 := essencerunner.Coins(d)
	bankCoins := bankNamedCount(bank, d, "Coins")
	if !hasCost || held0+bankCoins < cost {
		_ = bank.Close()
		ctx.logf("essence: a %s costs %s coins and the bank and pack hold %d — keeping the pickaxe already owned", target.Item, costLabel(cost, hasCost), held0+bankCoins)
		b.checkedPickaxe = target.Item
		return
	}
	need := cost - held0
	if need > 0 && !essencerunner.WithdrawXOf(d, bank, "Coins", need) {
		b.warn(ctx, "pick-coins", 60*time.Second, "could not withdraw the pickaxe coins — retrying")
		_ = bank.Close()
		return
	}
	_ = bank.Close()
	if essencerunner.Coins(d) < cost {
		return
	}
	if essencerunner.BuyFromNurmof(d, target.Item) {
		ctx.logf("essence: bought a %s from Nurmof for %d coins", target.Item, cost)
	} else {
		ctx.logf("essence: Nurmof was out of %ss — keeping the pickaxe already owned", target.Item)
	}
	b.checkedPickaxe = target.Item
}

// legacyStand is where this loop meets the mule: muleAtTile when it is set, else the Varrock east bank, the original
// script's own meet spot.
func (b *essenceBot) legacyStand() tile.Tile {
	if t, ok := b.settings.Meet(); ok {
		return t
	}
	return essenceplan.BankStand
}

func costLabel(cost int, ok bool) string {
	if !ok {
		return "?"
	}
	return fmt.Sprintf("%d", cost)
}

// ---- teleport ----

func (b *essenceBot) teleportStep(ctx *Context, d *essencerunner.Deps) {
	if b.questRefused {
		b.warn(ctx, "quest", 60*time.Second, "Aubury refused the teleport: complete %s on this account, then restart", essenceplan.RuneMysteries)
		b.pause(ctx, 5*time.Second)
		return
	}
	res := essencerunner.TeleportIn(d)
	if res.QuestRefused {
		b.questRefused = true
		return
	}
	if res.Landed {
		b.teleportFails = 0
		return
	}
	b.teleportFails++
	if essenceplan.TeleportGivingUp(b.teleportFails) {
		b.warn(ctx, "teleport", 60*time.Second, "the teleport did not land %d times running — still trying", b.teleportFails)
	} else {
		b.vlog(ctx, "the teleport did not land — retrying")
	}
}

// ---- the hand-over ----

// handoffStep draws everything the bank holds (but the pickaxes and the keep list, essence
// first) as notes and drives internal/scripts/donorrunner.Run against essenceplan.BankStand —
// the same stand doubles as the ordinary bank and the mule's meet spot, matching
// EssenceMiner.ts's own meetTile(). Blocking, like bankStep/pickaxeStep — one Loop call,
// cancellable throughout via ctx.Delay. Mirrors fisherBot.handoffStep/crafterBot's own twin.
//
// Loop returns into this before containersBlind runs, so the held read below can be a blinded
// one — and unlike the stage derivation, here that costs nothing. The only thing the worn half
// of HeldNames contributes to keep is a pickaxe name, since keepInPack returns nothing else, and
// essenceplan's own muleRules skips every PickTiers item unconditionally, whatever the keep list
// says. That one rule feeds all three consumers below — MuleRows (what leaves the bank),
// MuleHaul (the drawn count) and MuleLoad (what goes on the trade screen) — so dropping the name
// from keep changes none of them. Nor can a note slip past it under another name: content 289
// gives no pickaxe a noted form at all (skill_mining/configs/pickaxes.obj holds six entries and
// no cert line). What is offered is built from PackSlotRows, which reads PlayerPack, and a
// wielded pickaxe is not in the pack to offer in the first place. So this path needs no gate,
// and essenceplan's TestMuleHandoffIgnoresABlindedWornRead is what keeps that true.
func (b *essenceBot) handoffStep(ctx *Context, d *essencerunner.Deps, settings donorplan.Settings) error {
	level := essencerunner.MiningLevel(d)
	held := essencerunner.HeldNames(d)
	keep := b.keepInPack(level, held)
	if len(settings.Keep) > 0 {
		keep = append(append([]string{}, keep...), settings.Keep...)
	}

	bank, ok := essencerunner.OpenVarrockBank(d)
	if !ok {
		ctx.logf("essence: hand-off: could not reach or open the Varrock East bank — will retry")
		b.pause(ctx, 5*time.Second)
		return nil
	}
	rules := essenceplan.RulesFor(b.settings.MuleItems, keep)
	rows := donorplan.HandoffRows(essencerunner.BankItemRows(d, bank), rules)
	// THE WHOLE DRAW IS ONE BURST (twopertick-1012): note mode on, a Withdraw-All per row (a note takes one pack slot, so
	// as many rows as the pack has free slots) and note mode off go out together, five user events a tick, and are
	// confirmed once. It was a note-mode switch that waited on the varp, a Withdraw-All and a wait per row, and another
	// switch and wait.
	drawNames := make([]string, 0, len(rows))
	for _, row := range rows {
		drawNames = append(drawNames, row.Name)
	}
	noteDraw, asked := bank.DrawAllAsNotes(bank.RowIDsByName(d.Config, drawNames), actions.NoteResolver(d.Config), essencerunner.FreeSlots(d), essencerunner.Waiter(d))
	if asked > 0 {
		ctx.logf("essence: hand-off: %s", actions.VisitLog("note draw", asked+2, noteDraw, 0))
	}
	rowsLeft := len(donorplan.HandoffRows(essencerunner.BankItemRows(d, bank), rules))
	_ = bank.Close()

	packRows := func() []donorplan.ItemRow { return essencerunner.PackSlotRows(d) }
	drawn := donorplan.HaulOf(packRows(), rules)
	if drawn <= 0 {
		b.handoffPending = false
		b.handoffNextAllowed = time.Now().Add(donorplan.HandoffRetry)
		ctx.logf("essence: hand-off: nothing drawn from the bank — resuming mining")
		return nil
	}
	ctx.logf("essence: hand-off to %q — %d row(s) in the bank, %d pack slot(s) drawn, %d row(s) left for another load",
		settings.Mule, len(rows), drawn, rowsLeft)

	rd := &donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		Bus: ctx.Bus, Me: ctx.Username,
	}
	// Reach a mule that is logged in to a different world (worldhopplan bounds it; a
	// one-world fleet no-ops). See bot.WireDonorWorlds.
	WireDonorWorlds(ctx, rd)
	res := donorrunner.Run(rd, donorrunner.Options{
		Mule: settings.Mule, Stand: b.legacyStand(),
		RowsInBank: len(rows), Drawn: drawn,
		Load: func() []donorplan.LoadGroup {
			return donorplan.PlanLoad(packRows(), donorplan.DefaultMuleFreeSlots, rules)
		},
		HaulLeft:        func() int { return donorplan.HaulOf(packRows(), rules) },
		KnownUntradable: b.knownUntradable,
	})

	b.handoffPending = false
	switch {
	case res.Stopped:
		ctx.logf("essence: hand-off interrupted by a stop request")
		return nil
	case res.GaveUp:
		b.handoffNextAllowed = time.Now().Add(donorplan.HandoffRetry)
		ctx.logf("essence: hand-off: %s — resuming mining, next attempt no sooner than %s", res.Reason, b.handoffNextAllowed.Format(time.Kitchen))
	case res.Traded:
		b.handoffsDone++
		b.handedOver += res.Slots
		ctx.logf("essence: hand-off #%d complete — %d trade(s), %d pack slot(s) crossed to %q", b.handoffsDone, res.Loads, res.Slots, settings.Mule)
		if rowsLeft > 0 {
			b.handoffPending = true
			ctx.logf("essence: %d bank row(s) still for %q — drawing the next load", rowsLeft, settings.Mule)
		}
	default:
		ctx.logf("essence: hand-off ended without trading (%s)", res.Reason)
	}
	return nil
}
