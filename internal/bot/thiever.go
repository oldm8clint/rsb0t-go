package bot

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

func init() {
	Register("ProgressiveThiever", func() Bot { return &thieverBot{} })
}

// thieverBot ports ProgressiveThiever.ts's own loop(): every Loop call re-derives the phase
// from live state (thieverplan.DecideThievePhase) and does one phase-appropriate step, the
// same shape essenceBot/fisherBot/crafterBot/woodcutterBot already proved. The phases:
//
//   - accept, tutorial, bright: shared with every other fresh-account script.
//   - suicide: a level-1 account with no food in Lumbridge steals until it dies and carries
//     straight on — the Lumbridge respawn is one tile from the courtyard Men, so a death here
//     costs nothing. Death recovery is folded into this step itself (matching
//     ProgressiveThiever.ts's own suicideStep), not into the generic "clear the dead flag"
//     every script already does at the top of Loop.
//   - dead: a death anywhere the suicide farm does not cover — invalidates the cached bank
//     food count (whatever was carried is on the floor where it fell) and waits for the
//     respawn.
//   - fund, travel: Men pickpocketed in Lumbridge for the two boat fares (hardcoded — the TS
//     source's own fund step never reads a pickpocketTarget/pickpocketTile setting the way
//     ProgressiveCrafter/ProgressiveWoodcutter do), then Port Sarim -> Karamja -> Brimhaven ->
//     Ardougne.
//   - move, work: walk to the schedule's current rung and work it — pickpocket, steal from a
//     stall, or search a chest, dispatched by thieverplan.Method.Kind.
//   - bank: everything deposited but the food and a lockpick, food withdrawn up to
//     foodWithdraw when the rung hurts.
//   - order: waiting near the bank for the mule to bring food (see thieverrunner's own package
//     doc comment for why this never actually arrives yet).
//   - eat, stuck.
//
// The hand-over draws the coins first, then everything else the bank holds beyond the food and
// the keep list, and trades it to the mule via internal/scripts/donorrunner at the CURRENT
// rung's own bank (thievePlan.ts's own meetTile() is `this.method().bank`, not a fixed stand —
// the only one of the five ported scripts whose mule meet point moves with the schedule).
//
// Settings (the TS manifest's own keys):
//
//	schedule    string default "1: Man, 25: Warrior woman, 40: Guard, 55: Knight of Ardougne, 70: Paladin, 80: Hero"
//	suicide     bool   default true
//	food        string default "Lobster"
//	foodWithdraw int   default 20
//	minFood     int    default 1
//	eatBelowHp  int    default 50
//	bankAt      int    default 20   — loot slots before a bank trip.
//	fare        int    default 60   — coins for the boats.
//	restBelowHp int    default 40   — while funding the fare with no food, rest below this HP%.
//	guideNpc    string default "RuneScape Guide"
//	skipOption  string default "Yes please"
//	mule        string default ""  — blank never hands anything over or asks for food.
//	askMuleForFood bool default true
//	muleAt      int    default 20000 — bank ROWS (not coins, despite the constant's own name —
//	                    see bankStep's own doc comment: this is thievePlan.ts's own behavior,
//	                    preserved as coded rather than as its confusingly-worded settings help
//	                    text) worth handing over before the mule is called.
//	muleKeep    string default "Coins"
//	logDetail   string default "Normal"
type thieverBot struct {
	settings thieverplan.Settings
	schedule thieverplan.Schedule

	phase     thieverplan.ThievePhase
	havePhase bool

	bright *freshstart.Brightness

	died   bool
	deaths int
	steals int
	eats   int
	trips  int

	bankFood *int

	orderedAt time.Time
	asks      int

	// announced is the rung label already logged/warned this run, so its note/gate warning is
	// said once, when the rung is entered, not every loop after.
	announced string
	refusals  int
	standAt   int

	handoffPending bool
	handoffNext    time.Time
	handoffs       int
	handedOver     int

	sail      *freshstartrunner.SailProgress
	startedAt time.Time
	xpAtStart int
	warnedAt  map[string]time.Time
}

func (b *thieverBot) deps(ctx *Context) *thieverrunner.Deps {
	return &thieverrunner.Deps{
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

func (b *thieverBot) fresh(d *thieverrunner.Deps) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay), Log: d.Log,
		Loot: d.Loot,
	}
}

func thieverSettings(m map[string]interface{}) thieverplan.Settings {
	d := thieverplan.DefaultSettings()
	s := d
	if v, ok := m["schedule"].(string); ok && strings.TrimSpace(v) != "" {
		s.Schedule = v
	}
	s.Suicide = BoolSetting(m, "suicide", d.Suicide)
	s.Food = StringSetting(m, "food", d.Food)
	s.FoodWithdraw = IntSetting(m, "foodWithdraw", d.FoodWithdraw)
	s.MinFood = IntSetting(m, "minFood", d.MinFood)
	s.EatBelowHP = IntSetting(m, "eatBelowHp", d.EatBelowHP)
	s.BankAt = IntSetting(m, "bankAt", d.BankAt)
	s.Fare = IntSetting(m, "fare", d.Fare)
	s.RestBelowHP = IntSetting(m, "restBelowHp", d.RestBelowHP)
	s.GuideNPC = StringSetting(m, "guideNpc", d.GuideNPC)
	s.SkipOption = StringSetting(m, "skipOption", d.SkipOption)
	if v, ok := m["mule"].(string); ok {
		s.Mule = strings.TrimSpace(v)
	}
	s.AskMuleForFood = BoolSetting(m, "askMuleForFood", d.AskMuleForFood)
	s.MuleAt = IntSetting(m, "muleAt", d.MuleAt)
	if v, ok := m["muleKeep"].(string); ok {
		s.MuleKeep = v
	}
	s.LogDetail = StringSetting(m, "logDetail", d.LogDetail)
	return s.Normalised()
}

func (b *thieverBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("thiever: no collision pack configured (fleet.config.json needs a packPath) — refusing to start rather than panic later inside pathfinding")
	}
	if ctx.Locs == nil {
		return fmt.Errorf("thiever: no loc index configured (fleet.config.json needs a locsPath) — refusing to start rather than panic later looking up targets/stalls/chests/the bank")
	}
	if ctx.Components == nil {
		return fmt.Errorf("thiever: no interface archive Components configured — refusing to start rather than panic later opening the bank")
	}
	b.settings = thieverSettings(ctx.Settings)
	b.schedule = b.settings.ParseSchedule()
	b.bright = &freshstart.Brightness{}
	b.sail = freshstartrunner.NewSailProgress()
	b.warnedAt = map[string]time.Time{}
	b.havePhase = false
	b.startedAt = time.Now()
	b.xpAtStart = thieverrunner.ThievingXP(b.deps(ctx))

	for _, bad := range b.schedule.Rejected {
		ctx.logf("thiever: WARNING: %q in the schedule names nothing this server has — ignored", bad)
	}
	for _, up := range b.schedule.Raised {
		ctx.logf("thiever: the schedule asked for %s, which is the level the game opens it at", up)
	}
	if b.schedule.FellBack {
		ctx.logf("thiever: WARNING: none of the schedule could be read — using the default")
	}
	mule := "no mule"
	if b.settings.Mule != "" {
		mule = fmt.Sprintf("coins/loot to %s at %d bank rows (keep %q)", b.settings.Mule, b.settings.MuleAt, b.settings.MuleKeep)
	}
	ctx.logf("thiever: thieving %d, schedule %s, %s", thieverrunner.ThievingLevel(b.deps(ctx)), thieverplan.DescribeSchedule(b.schedule), mule)
	return nil
}

func (b *thieverBot) vlog(ctx *Context, format string, args ...any) {
	if strings.EqualFold(b.settings.LogDetail, "Verbose") {
		ctx.logf("thiever: "+format, args...)
	}
}

func (b *thieverBot) warn(ctx *Context, key string, everyMs time.Duration, format string, args ...any) {
	if time.Since(b.warnedAt[key]) < everyMs {
		return
	}
	b.warnedAt[key] = time.Now()
	ctx.logf("thiever: WARNING: "+format, args...)
}

// snapshot builds thieverplan.ThieveSnapshot from live state, ported from
// ProgressiveThiever.ts's own snapshot(). ok is false until the tile is known.
func (b *thieverBot) snapshot(ctx *Context, d *thieverrunner.Deps) (thieverplan.ThieveSnapshot, bool) {
	st := ctx.Session.State()
	x, z, level, tileOK := st.Tile()
	if !tileOK {
		return thieverplan.ThieveSnapshot{}, false
	}
	main, _, _ := st.Modals()
	here := tile.Tile{X: x, Z: z, Level: level}
	hp, _ := st.HP()
	maxHP, _ := st.MaxHP()
	thieving := thieverrunner.ThievingLevel(d)
	// lageat-1018: a view that has lagged lately raises the eat share to what the Watchman event and a failed steal on this rung could
	// have done in the worst recent episode (fighterfood.LagEatFrac, up to 70%); with no lag in the memory it is the setting.
	eatAt := b.settings.EatAt()
	if lag := st.RecentViewLag().Ticks; lag > 0 {
		combat, _ := st.CombatLevel()
		eatAt = fighterfood.LagEatFrac(maxHP, eatAt, fleetplan.SumHits(fleetplan.ThievingHits(combat, thieverplan.MethodFor(thieving, b.schedule).StunDamage)...), lag)
	}
	s := thieverplan.ThieveSnapshot{
		MainModal:      main,
		Tile:           &here,
		Level:          thieving,
		Schedule:       b.schedule,
		HP:             hp,
		MaxHP:          maxHP,
		EatAt:          eatAt,
		Heal:           fighterfood.FoodHealAmount(b.settings.Food),
		Food:           thieverrunner.FoodCount(d, b.settings.Food),
		MinFood:        b.settings.MinFood,
		BankFood:       b.bankFood,
		NotedFood:      thieverrunner.NotedFoodCount(d, b.settings.Food),
		HasMule:        b.settings.Mule != "" && b.settings.AskMuleForFood,
		PackFull:       thieverrunner.PackFull(d),
		Loot:           thieverrunner.LootSlots(d, b.settings.Food),
		BankAt:         b.settings.BankAt,
		Coins:          thieverrunner.CoinsHeld(d),
		Fare:           b.settings.Fare,
		Died:           b.died,
		SuicideAllowed: b.settings.Suicide,
		Bright:         !freshstartrunner.BrightPending(b.fresh(d), b.bright),
	}
	return s, true
}

func (b *thieverBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !thieverrunner.WaitForGuardian(d) {
		return nil
	}
	// A death is recorded (sticky, like ProgressiveThiever.ts's own this.died chat-line flag)
	// before the ordinary one-way-latch clear every script performs, so suicideStep/deadStep
	// below still see it on this same Loop call even though the dashboard-facing IsDead() is
	// cleared afterward.
	if ctx.Session.State().IsDead() {
		b.died = true
	}
	if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
		ctx.Session.State().ClearDead()
	}
	if actions.NewTrade(ctx.Session).Active() {
		ctx.Delay(2 * time.Second)
		return nil
	}
	s, ok := b.snapshot(ctx, d)
	if !ok {
		ctx.Delay(1200 * time.Millisecond)
		return nil
	}
	phase := thieverplan.DecideThievePhase(s)
	if !b.havePhase || phase != b.phase {
		b.phase, b.havePhase = phase, true
		ctx.logf("thiever: phase: %s", thieverplan.DescribeThievePhase(phase))
	}
	if phase != thieverplan.PhaseAccept && phase != thieverplan.PhaseTutorial {
		if dlg := actions.NewChatDialog(ctx.Session, ctx.Components); dlg.CanContinue() {
			_ = dlg.Continue()
			ctx.Delay(600 * time.Millisecond)
			return nil
		}
	}

	if manual := ctx.TakeHandoffRequest(); manual {
		b.handoffPending = true
	}
	if b.settings.Donor().Enabled() && b.handoffPending {
		return b.handoffStep(ctx, d)
	}

	m := thieverplan.MethodFor(s.Level, b.schedule)
	b.announce(ctx, m)
	fd := b.fresh(d)
	switch phase {
	case thieverplan.PhaseAccept:
		freshstartrunner.AcceptAppearance(fd)
	case thieverplan.PhaseTutorial:
		freshstartrunner.SkipTutorial(fd, b.settings.GuideNPC, b.settings.SkipOption)
	case thieverplan.PhaseBright:
		freshstartrunner.BrightnessStep(fd, b.bright)
	case thieverplan.PhaseWait:
		ctx.Delay(1200 * time.Millisecond)
	case thieverplan.PhaseSuicide:
		b.suicideStep(ctx, d, m)
	case thieverplan.PhaseDead:
		b.deadStep(ctx)
	case thieverplan.PhaseFund:
		freshstartrunner.PickpocketStep(fd, freshstartrunner.NewThief(), freshstartrunner.PickpocketOpts{
			Target: freshstart.DefaultPickpocketTarget, Spot: freshstart.PickpocketSpot,
			RestBelow: b.settings.RestBelow(), Need: freshstart.FareRemaining(s.Tile, b.settings.Fare),
		})
	case thieverplan.PhaseTravel:
		freshstartrunner.SailLeg(fd, thieverplan.ArdougneBank, b.sail)
	case thieverplan.PhaseMove:
		b.moveStep(ctx, d, m)
	case thieverplan.PhaseBank:
		b.bankStep(ctx, d, m)
	case thieverplan.PhaseOrder:
		b.orderStep(ctx, d, m)
	case thieverplan.PhaseEat:
		b.eatStep(ctx, d)
	case thieverplan.PhaseWork:
		b.workStep(ctx, d, m)
	case thieverplan.PhaseStuck:
		b.warn(ctx, "stuck", 120*time.Second, "out of %s, the bank has none and no mule is set — put food in the bank, or name a mule in the settings", strings.ToLower(b.settings.Food))
		ctx.Delay(3 * time.Second)
	}
	return nil
}

// announce logs a rung's own note/gate warning once, when it is entered, and resets the
// per-rung stall counters — ported from announce().
func (b *thieverBot) announce(ctx *Context, m thieverplan.Method) {
	if b.announced == m.Label {
		return
	}
	b.announced = m.Label
	if m.Gate != "" {
		b.warn(ctx, "gate:"+m.Label, 0, "%s needs %s finished — without it there is nobody here to rob", m.Label, m.Gate)
	}
	if m.Note != "" {
		ctx.logf("thiever: %s: %s", m.Label, m.Note)
	}
	if !m.Verified {
		b.vlog(ctx, "%s's spot has not been watched in the game yet — if it stands in the wrong place, that is why", m.Label)
	}
	b.refusals = 0
	b.standAt = 0
}

// ---- the opening ----

func (b *thieverBot) suicideStep(ctx *Context, d *thieverrunner.Deps, m thieverplan.Method) {
	if b.died {
		b.deaths++
		b.died = false
		ctx.logf("thiever: died in the courtyard (%d so far) — carrying on", b.deaths)
		ctx.DelayUntil(func() bool { _, _, _, ok := ctx.Session.State().Tile(); return ok }, 20*time.Second)
		ctx.Delay(1200 * time.Millisecond)
		return
	}
	res := thieverrunner.Pickpocket(d, m)
	if res.Success {
		b.steals++
	}
}

func (b *thieverBot) deadStep(ctx *Context) {
	b.deaths++
	b.died = false
	ctx.logf("thiever: died (%d so far) — waiting for the respawn, then back to the bank for food", b.deaths)
	ctx.DelayUntil(func() bool { _, _, _, ok := ctx.Session.State().Tile(); return ok }, 20*time.Second)
	ctx.Delay(1800 * time.Millisecond)
	// Whatever was carried is on the floor where it fell — the bank must be looked in again.
	b.bankFood = nil
}

// ---- working the rung ----

func (b *thieverBot) moveStep(ctx *Context, d *thieverrunner.Deps, m thieverplan.Method) {
	radius := m.Leash
	if radius > 4 {
		radius = 4
	}
	if !thieverrunner.WalkNear(d, m.Anchor, radius) {
		b.warn(ctx, "walk:"+m.Label, 60*time.Second, "could not reach %s — retrying", m.Label)
	}
}

func (b *thieverBot) workStep(ctx *Context, d *thieverrunner.Deps, m thieverplan.Method) {
	switch m.Kind {
	case thieverplan.KindPickpocket:
		res := thieverrunner.Pickpocket(d, m)
		if res.Success {
			b.steals++
		} else if res.Reason == "no-target" {
			b.warn(ctx, "none:"+m.Label, 120*time.Second, "nothing to rob within %d tiles of %d,%d — is %s really here?", m.Leash, m.Anchor.X, m.Anchor.Z, m.Label)
			ctx.Delay(1800 * time.Millisecond)
		}
	case thieverplan.KindStall:
		res := thieverrunner.StealStall(d, m, b.standAt)
		switch {
		case res.Success:
			b.steals++
			b.refusals = 0
		case res.Reason == "refused":
			b.refusals++
			if b.refusals >= thieverrunner.RefusalsBeforeMoving && len(m.Stands) > 1 {
				b.standAt++
				b.refusals = 0
				b.vlog(ctx, "refused three times — moving to the other side of the %s", strings.ToLower(m.Loc))
			}
		case res.Reason == "no-stall":
			ctx.Delay(1200 * time.Millisecond)
		case res.Reason == "unreachable":
			// The stand tile could not be walked to. Deliberately distinct from "no-stall": a
			// stall that has not restocked clears itself in a few ticks and deserves a short
			// delay, while a failed walk deserves saying out loud — because until 2026-09-21
			// this case did not exist, and an account that could not get to its own stall
			// reported "waiting for the stall to restock" forever instead.
			b.warn(ctx, "stall-reach", 60*time.Second, "could not walk to the %s — retrying", strings.ToLower(m.Loc))
			ctx.Delay(3 * time.Second)
		}
	case thieverplan.KindChest:
		res := thieverrunner.SearchChest(d, m)
		switch {
		case res.Success:
			b.steals++
		case res.Reason == "no-lockpick":
			b.warn(ctx, "lockpick", 120*time.Second, "%s is locked and there is no lockpick in the pack — put one in, or pick another method", m.Label)
			ctx.Delay(3 * time.Second)
		case res.Reason == "no-chest":
			ctx.Delay(3 * time.Second)
		}
	}
}

// ---- food ----

func (b *thieverBot) eatStep(ctx *Context, d *thieverrunner.Deps) {
	if thieverrunner.EatFood(d, b.settings.Food) {
		b.eats++
	} else {
		ctx.Delay(600 * time.Millisecond)
	}
}

// ---- the mule's food order ----

// orderStep waits near the bank for the order (see thieverrunner's own package doc comment for
// why it is never actually fulfilled yet) and publishes the same KindOrder bus message
// muleOrder.ts would, for forward compatibility with a future MuleReceiver that reads it.
func (b *thieverBot) orderStep(ctx *Context, d *thieverrunner.Deps, m thieverplan.Method) {
	if tile.Chebyshev(currentTileOfThief(d), m.Bank) > 3 {
		if !thieverrunner.WalkNear(d, m.Bank, 2) {
			ctx.Delay(2 * time.Second)
		}
		return
	}
	if b.settings.Mule != "" && ctx.Bus != nil && time.Since(b.orderedAt) > 60*time.Second {
		want := b.settings.FoodWithdraw
		ctx.Bus.Publish(mulebus.OrderMessage(ctx.Username, fmt.Sprintf("%d %s", want, b.settings.Food), b.settings.Mule))
		b.orderedAt = time.Now()
		b.asks++
		ctx.logf("thiever: asked %s for %d %s%s", b.settings.Mule, want, strings.ToLower(b.settings.Food), askCountSuffixThief(b.asks))
	}
	if b.asks >= 3 {
		b.warn(ctx, "food-order", 300*time.Second, "%s has not brought %s after %d asks: this Go port cannot yet have MuleReceiver fulfil item orders (see thieverrunner's own doc comment) — put %s in %s's own bank instead", b.settings.Mule, strings.ToLower(b.settings.Food), b.asks, strings.ToLower(b.settings.Food), ctx.Username)
	}
	ctx.Delay(2 * time.Second)
}

func askCountSuffixThief(n int) string {
	if n > 1 {
		return fmt.Sprintf(" (ask %d)", n)
	}
	return ""
}

func currentTileOfThief(d *thieverrunner.Deps) tile.Tile {
	x, z, level, _ := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

// ---- the bank ----

// bankStep deposits everything but the food and a lockpick, withdraws food when the rung
// hurts, and checks the mule threshold — ported from bankStep(). The mule threshold compares
// the bank's own loot ROW COUNT (thieverplan.MuleRows(...).length, via BankItemRows below)
// against muleAt, preserving thievePlan.ts's own behavior exactly even though its settings
// help text calls muleAt "bank slots" while its default (20,000) is a coin-sized number — see
// this file's own doc comment on the settings for the full explanation. A default-configured
// run essentially never hands off automatically; an operator who wants it to needs to set
// muleAt to a real row count (a handful, at most the bank's own tab size), or use the control
// panel's manual "hand off now" trigger.
func (b *thieverBot) bankStep(ctx *Context, d *thieverrunner.Deps, m thieverplan.Method) {
	if tile.Chebyshev(currentTileOfThief(d), m.Bank) > 4 {
		if !thieverrunner.WalkNear(d, m.Bank, 3) {
			b.warn(ctx, "walk:bank", 60*time.Second, "could not reach the bank at %d,%d — retrying", m.Bank.X, m.Bank.Z)
		}
		return
	}
	bank, ok := thieverrunner.OpenBankAt(d, m.Bank)
	if !ok {
		b.warn(ctx, "bank-open", 60*time.Second, "could not open the bank — retrying")
		ctx.Delay(3 * time.Second)
		return
	}
	keep := []string{b.settings.Food, "Lockpick"}
	usedBefore := thieverrunner.PackSlots - thieverrunner.FreeSlots(d)
	deposited := thieverrunner.DepositAllExcept(d, bank, keep)
	if deposited > 0 {
		b.trips++
		b.vlog(ctx, "banked %d slots (%d used before)", deposited, usedBefore)
	}
	bankedFood := thieverrunner.BankFoodCount(d, bank, b.settings.Food)
	if m.Hurts && bankedFood > 0 {
		_ = thieverrunner.SetNoteMode(d, bank, false)
		before := thieverrunner.FoodCount(d, b.settings.Food)
		want := b.settings.FoodWithdraw
		if want > bankedFood {
			want = bankedFood
		}
		if thieverrunner.WithdrawXOf(d, bank, b.settings.Food, want) {
			after := thieverrunner.FoodCount(d, b.settings.Food)
			b.vlog(ctx, "withdrew %d %s (%d -> %d in the pack)", want, strings.ToLower(b.settings.Food), before, after)
		}
		b.orderedAt, b.asks = time.Time{}, 0
	}
	bankedFood = thieverrunner.BankFoodCount(d, bank, b.settings.Food)
	b.bankFood = &bankedFood

	bankRows := thieverplan.MuleRows(thieverrunner.BankItemRows(d, bank), []string{b.settings.MuleKeep}, b.settings.Food)
	_ = bank.Close()

	if b.settings.Donor().Enabled() {
		settings := b.settings
		if v, ok := ctx.MuleAtOverride(); ok {
			settings.MuleAt = v
		}
		now := time.Now()
		if settings.Donor().Enabled() && donorplan.HandoffDue(len(bankRows), settings.MuleAt, now, b.handoffNext) {
			b.handoffs++
			ctx.logf("thiever: the bank is worth handing over — calling %s", settings.Mule)
			b.handoffPending = true
		}
	}
	if m.Hurts && thieverrunner.FoodCount(d, b.settings.Food) < b.settings.MinFood && bankedFood == 0 {
		b.vlog(ctx, "the bank has no %s", strings.ToLower(b.settings.Food))
	}
}

// ---- the mule ----

// handoffStep draws the coins first, then everything else the current rung's own bank holds
// beyond the food and the keep list, as notes, and drives internal/scripts/donorrunner.Run
// against that SAME bank tile — thievePlan.ts's own meetTile() is the method's own bank, not a
// fixed stand, so the meet point moves with the schedule (unlike every other ported script's
// fixed Seers/Varrock stand).
func (b *thieverBot) handoffStep(ctx *Context, d *thieverrunner.Deps) error {
	settings := b.settings
	if v, ok := ctx.MuleAtOverride(); ok {
		settings.MuleAt = v
	}
	donor := settings.Donor()
	m := thieverplan.MethodFor(thieverrunner.ThievingLevel(d), b.schedule)
	keep := []string{settings.MuleKeep}

	bank, ok := thieverrunner.OpenBankAt(d, m.Bank)
	if !ok {
		ctx.logf("thiever: hand-off: could not reach or open the bank — will retry")
		ctx.Delay(5 * time.Second)
		return nil
	}
	rows := thieverplan.MuleRows(thieverrunner.BankItemRows(d, bank), keep, settings.Food)
	// THE WHOLE DRAW IS ONE BURST (twopertick-1012): note mode on, a Withdraw-All per row (a note takes one pack slot, so
	// as many rows as the pack has free slots) and note mode off go out together, five user events a tick, and are
	// confirmed once. It was a note-mode switch that waited on the varp, a Withdraw-All and a wait per row, and another
	// switch and wait.
	drawNames := make([]string, 0, len(rows))
	for _, row := range rows {
		drawNames = append(drawNames, row.Name)
	}
	noteDraw, asked := bank.DrawAllAsNotes(bank.RowIDsByName(d.Config, drawNames), actions.NoteResolver(d.Config), thieverrunner.FreeSlots(d), thieverrunner.Waiter(d))
	if asked > 0 {
		ctx.logf("thiever: hand-off: %s", actions.VisitLog("note draw", asked+2, noteDraw, 0))
	}
	bankedFood := thieverrunner.BankFoodCount(d, bank, settings.Food)
	b.bankFood = &bankedFood
	rowsLeft := len(thieverplan.MuleRows(thieverrunner.BankItemRows(d, bank), keep, settings.Food))
	_ = bank.Close()

	packRows := func() []donorplan.ItemRow { return thieverrunner.PackSlotRows(d) }
	drawn := thieverplan.MuleHaul(packRows(), keep, settings.Food)
	if drawn <= 0 {
		b.handoffPending = false
		b.handoffNext = time.Now().Add(donorplan.HandoffRetry)
		ctx.logf("thiever: hand-off: nothing drawn from the bank — resuming")
		return nil
	}
	ctx.logf("thiever: hand-off to %q — %d row(s) in the bank, %d pack slot(s) drawn, %d row(s) left for another load",
		donor.Mule, len(rows), drawn, rowsLeft)

	rd := &donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		Bus: ctx.Bus, Me: ctx.Username,
	}
	// Reach a mule that is logged in to a different world (worldhopplan bounds it; a
	// one-world fleet no-ops). See bot.WireDonorWorlds.
	WireDonorWorlds(ctx, rd)
	res := donorrunner.Run(rd, donorrunner.Options{
		Mule: donor.Mule, Stand: m.Bank,
		RowsInBank: len(rows), Drawn: drawn,
		Load: func() []donorplan.LoadGroup {
			return thieverplan.MuleLoad(packRows(), donorplan.DefaultMuleFreeSlots, keep, settings.Food)
		},
		HaulLeft: func() int { return thieverplan.MuleHaul(packRows(), keep, settings.Food) },
	})

	b.handoffPending = false
	switch {
	case res.Stopped:
		ctx.logf("thiever: hand-off interrupted by a stop request")
		return nil
	case res.GaveUp:
		b.handoffNext = time.Now().Add(donorplan.HandoffRetry)
		ctx.logf("thiever: hand-off: %s — resuming, next attempt no sooner than %s", res.Reason, b.handoffNext.Format(time.Kitchen))
	case res.Traded:
		b.handedOver += res.Slots
		ctx.logf("thiever: hand-off complete — %d trade(s), %d pack slot(s) crossed to %q", res.Loads, res.Slots, donor.Mule)
		if rowsLeft > 0 {
			b.handoffPending = true
			ctx.logf("thiever: %d bank row(s) still for %q — drawing the next load", rowsLeft, donor.Mule)
		}
	default:
		ctx.logf("thiever: hand-off ended without trading (%s)", res.Reason)
	}
	return nil
}

func (b *thieverBot) OnStop(ctx *Context) {
	ctx.logf("thiever: stopped — thieving %d (+%d xp), %d steals, %d deaths, %d bites, %d bank trips, %d slots to the mule in %d hand-over(s)",
		thieverrunner.ThievingLevel(b.deps(ctx)), thieverrunner.ThievingXP(b.deps(ctx))-b.xpAtStart, b.steals, b.deaths, b.eats, b.trips, b.handedOver, b.handoffs)
}
