package bot

// MasterCrafter's crystal chest round (a user request; the plan and its order are
// mastercraftplan/chest.go). When the mule publishes more than 100 Crystal keys, after every other
// round, the main account asks it for all of them, spends them six a trip at the Taverley chest, banks the
// loot at Falador between trips, and hands every piece of it to the mule at the end.
//
// What it reuses rather than rewrites:
//   - LootChestFisher's chest open (lootchestfisherrunner.OpenChest: the chest resolved by name
//     at its one legal standing tile, the key used on it, the reward measured off the pack and
//     reported to the Loot panel), its Falador west bank (OpenNearestBank), its walk on the
//     Falador side (WalkNear: the Taverley gate and the chest room's door), its key ids;
//   - lootchestfisherplan's six keys a trip and its free-slot rule (no open without room for the
//     worst reward);
//   - MasterCrafter's own ask (askMule, the bundled kit order) and hand-over (masterDeliver, the
//     RuneRunner trade), and ScarabSwarm's wolf-free walk (masterWalkTo) for the long legs between
//     Catherby and Falador;
//   - Context.Teleport for Falador and Camelot Teleport, when Magic allows (37 and 45).
//
// 7 October 2026 (owner): the legs to the chest go by Games necklace to the Burthorpe Games Room and its
// stairs (mastercraftplan/chestalch.go, mastercraftrunner/gamesroute.go; craftChestGamesRoute), a Staff of
// fire is wielded and every walk is run-stalled by High Level Alchemy (or the necklace's rub), and the
// chest's loot (cut gems, the shield, the legs) is alched where the pack has no room for the next open
// (craftChestAlch; the Nature runes and noted rubies of the kit).
//
// The loot is handed over by QUANTITY (Ledger.Chest.Owed): the chest pays coins and runes, which
// the main account keeps of its own in the same bank, and only what the chest gave goes to the mule.
//
// What a trip carries and does (a user request, chestkit.go): six keys, the runes of the spells
// no worn staff pays for, the ships' fare and three Strange fruit; every bank visit banks the whole
// pack in one batch and draws the kit in another (mastercraftrunner.ChestKit); the walk to the chest
// runs and eats a fruit when run energy is low; the chest is opened while the pack has room for the
// worst reward with the key's own slot counted free; a leg that goes nowhere is planned again after
// a minute (webwalk) and bounded at five (masterChestLegWalk). A round that starts with keys of its
// own on the Falador side resumes there, with no walk to the mule's post first.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/lootchestfisherplan"
	"rsb0t-go/internal/scripts/lootchestfisherrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// masterChestLongWalk bounds a long leg between Catherby and Falador on the wolf-free route (the
// ships): BuyFireStaffFromZaff gives the same walk 20 minutes.
const masterChestLongWalk = 20 * time.Minute

// masterChestStepFails is how many times in a row one step may fail before the round gives up on
// trips (and goes home with what it has) or, for the way home and the hand-over, ends.
const masterChestStepFails = 3

// masterChestRepeats is how many times in a row one step may run, each time "working", before it
// counts as a failure (six opens a trip is the most any step legitimately repeats).
const masterChestRepeats = 8

// masterChestMaxSteps bounds one round's steps whatever happens.
const masterChestMaxSteps = 4000

// withChest is Decide with the chest round after every other round (mastercraftplan.DecideWithChest),
// except that a round under way carries on first: an account on the Falador side with keys of its own
// was in the middle of its trips when the script stopped.
func (b *masterCrafterBot) withChest(ctx *Context, s mastercraftplan.Snapshot) mastercraftplan.Decision {
	return mastercraftplan.DecideWithChest(b.set, s, mastercraftplan.ChestInput{
		OwnKeys: b.ownChestKeys(ctx), OnFaladorSide: b.onFaladorSide(ctx),
	})
}

// onFaladorSide says the account stands on the Falador side of the run: the bank, the road and the
// chest (lootchestfisherrunner.OnFaladorSide).
func (b *masterCrafterBot) onFaladorSide(ctx *Context) bool {
	if ctx.Session == nil {
		return false
	}
	x, z, level, ok := ctx.Session.State().Tile()
	return ok && lootchestfisherrunner.OnFaladorSide(tile.Tile{X: x, Z: z, Level: level})
}

// ownChestKeys is the Crystal keys this account holds: its bank's (as the session last saw it, else
// as the ledger last saw it: a restarted session has not opened the bank yet) and its pack's.
func (b *masterCrafterBot) ownChestKeys(ctx *Context) int {
	if ctx.Session == nil || ctx.Config == nil {
		return 0
	}
	ids, ok := lootchestfisherrunner.ResolveKeyIDs(ctx.Config)
	if !ok {
		return 0
	}
	st := ctx.Session.State()
	bankKnown, bank, pack := false, 0, 0
	if inv := st.Inventory(actions.BankWithdraw); inv != nil {
		bankKnown = true
		for _, it := range inv.NonEmpty() {
			if ids.Unnoted[int(it.ID)] {
				bank += int(it.Count)
			}
		}
	}
	if inv := st.Inventory(actions.PlayerPack); inv != nil {
		for _, it := range inv.NonEmpty() {
			if ids.Unnoted[int(it.ID)] || ids.Noted[int(it.ID)] {
				pack += max(1, int(it.Count))
			}
		}
	}
	return mastercraftplan.ChestOwnKeys(bankKnown, bank, b.ledger.Chest.KeysBank, pack)
}

// chestDeps is LootChestFisher's Deps over this account.
func (b *masterCrafterBot) chestDeps(ctx *Context) *lootchestfisherrunner.Deps {
	return &lootchestfisherrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		Walker: b.walker, Loot: ctx.Loot,
	}
}

// chestOpenFaladorBank opens the Falador west bank (a seam: a test has no booth to find).
var chestOpenFaladorBank = lootchestfisherrunner.OpenNearestBankFast

// chestGamesRoute is the Games necklace's rub and the climb out of the Games Room, and chestIdleAlch one High
// Level Alchemy cast where the account stands: seams, a test plays the server's side of both.
var (
	chestGamesRoute = mastercraftrunner.RubToBurthorpe
	chestIdleAlch   = func(t *fleetrunner.Travel, d *fleetrunner.Deps) bool { return t.IdleAlch(d) }
)

// masterChest is one chest round's running state.
type masterChest struct {
	b    *masterCrafterBot
	ctx  *Context
	d    *fleetrunner.Deps
	ld   *lootchestfisherrunner.Deps
	post tile.Tile
	ids  lootchestfisherrunner.KeyIDs
	// kit is the object ids behind the kit's items; paid the runes a worn staff pays for and runes
	// the kit's count of each, both read afresh at every bank visit and cast (refreshKit).
	kit    mastercraftplan.KitIDs
	magic  int
	paid   map[string]bool
	runes  map[string]int
	view   mastercraftplan.ChestView
	opened int
	handed int
	// opt is what the settings add to the kit; travel the run staller (High Level Alchemy at Magic 55 with
	// Nature runes, else a Games necklace's rub) every walk of the round carries.
	opt    mastercraftplan.ChestKitOptions
	travel *fleetrunner.Travel
	// prevWeapon is the weapon a Staff of fire replaced, kept in the pack through the bank visits and wielded
	// again before the hand-over; staffTried says the staff was looked for this round.
	prevWeapon int32
	staffTried bool
	// alchOff is set when High Level Alchemy was refused or did nothing twice this round: no more casts.
	alchOff                bool
	alchMisses, alched     int
	routeOff               bool
	routeFails, routeFlown int
	// giveUp ends the trips early (a step failed too often): home with what there is.
	giveUp bool
	// fruitAt is when a walk last looked at run energy, lastBite when a Strange fruit was last sent to
	// be eaten (mastercraftplan.ChestBiteGap), fruitEaten the Strange fruit eaten.
	fruitAt     time.Time
	lastBite    time.Time
	fruitEaten  int
	bankVisits  int
	bankTotal   time.Duration
	bankSlowest time.Duration
}

// chestRound is the chest round: see the file comment and mastercraftplan.NextChestStep.
func (b *masterCrafterBot) chestRound(ctx *Context, d *fleetrunner.Deps, started time.Time, dec mastercraftplan.Decision) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeChest}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return b.chestWaits(ctx, out)
	}
	ids, ok := lootchestfisherrunner.ResolveKeyIDs(ctx.Config)
	if !ok {
		out.Why = "the cache has no unnoted Crystal key"
		return b.chestWaits(ctx, out)
	}
	// A round that was under way carries on from where the account stands (ChestResumes): no walk to
	// the mule's post and no ask before its own keys are spent. Anything else goes to the post first.
	own := b.ownChestKeys(ctx)
	resume := mastercraftplan.ChestResumes(dec, own, b.onFaladorSide(ctx))
	if resume {
		b.logf(ctx, "chest: resuming from %s with %d Crystal key(s) of its own: no walk to %s's post first, the post is for the hand-over at the end",
			fleetrunner.HereIs(d), own, b.mule)
	} else if why, ok := b.arrive(ctx, d, post); !ok {
		out.Transient, out.Why = true, why
		return b.chestWaits(ctx, out)
	}
	c := &masterChest{b: b, ctx: ctx, d: d, ld: b.chestDeps(ctx), post: post, ids: ids,
		magic: fleetrunner.MagicLevel(d)}
	gamesRoute := b.set.ChestGamesRoute != mastercraftplan.GamesRouteOff
	c.opt = mastercraftplan.ChestKitOptions{Alch: mastercraftplan.ChestAlchOK(b.set, c.magic), Route: gamesRoute, PostRoute: gamesRoute}
	c.travel = chestTravel(c.opt, b.set)
	d.Travel = c.travel // every walk the round makes through scarabrunner.WalkTo is run-stalled too
	defer func() { d.Travel = nil }()
	c.kit = c.kitIDs()
	c.refreshKit()
	if resume {
		c.view.Asked, c.view.Resumed = true, true
		c.view.KeysBank = b.ledger.Chest.KeysBank
	}
	b.ledger.Chest.Rounds++
	fal, cam := mastercraftplan.ChestCastable(c.magic)
	b.logf(ctx, "chest: round starts at Magic %d — Falador Teleport (needs %d) %s, Camelot Teleport (needs %d) %s; %s; %s",
		c.magic, mastercraftplan.ChestSpellLevel(mastercraftplan.FaladorTeleport), castWord(fal),
		mastercraftplan.ChestSpellLevel(mastercraftplan.CamelotTeleport), castWord(cam), homeWord(c.magic),
		c.kitWord())
	b.logf(ctx, "chest: Games Room route %q (a Games necklace to Burthorpe: planned cost 141 from there to the chest, 159 from the Falador bank, 793 from the post); alchemy %s (%s)",
		b.set.ChestGamesRoute, onOff(c.opt.Alch), alchWhy(b.set, c.magic))

	fails, repeats, last := 0, 0, mastercraftplan.ChestStep("")
	reason := ""
	for step := 0; step < masterChestMaxSteps; step++ {
		if ctx.Stopped() {
			reason = "stopped"
			break
		}
		masterClearDead(ctx)
		if !fleetrunner.WaitForGuardian(d) {
			reason = "stopped"
			break
		}
		c.read(started)
		next := mastercraftplan.NextChestStep(c.view)
		if next == mastercraftplan.ChestStepDone {
			break
		}
		if next != last {
			fails, repeats = 0, 0
		} else {
			repeats++
		}
		last = next
		ok, end, why := c.do(next)
		if ok && repeats >= masterChestRepeats {
			// The same step over and over, each "working": a bank visit that draws nothing, a walk
			// that arrives somewhere the view does not count. It is a failure like any other.
			ok, why = false, fmt.Sprintf("%s ran %d times in a row without the round moving on", next, repeats+1)
		}
		if end != "" {
			reason = end
			out.Transient = out.Transient || why == "transient"
			break
		}
		if ok {
			fails = 0
			continue
		}
		fails++
		b.logf(ctx, "chest: %s failed (%d in a row): %s", next, fails, why)
		if fails < masterChestStepFails {
			ctx.Delay(3 * time.Second)
			continue
		}
		switch next {
		case mastercraftplan.ChestStepHome, mastercraftplan.ChestStepHandover:
			reason = fmt.Sprintf("%s failed %d times in a row: %s", next, fails, why)
			out.Transient = true
		default:
			if !c.giveUp {
				c.giveUp, fails = true, 0
				b.logf(ctx, "chest: giving up on trips after %s failed %d times — taking what there is home", next, masterChestStepFails)
				continue
			}
			reason = fmt.Sprintf("%s failed %d times in a row after the trips were given up: %s", next, fails, why)
			out.Transient = true
		}
		break
	}
	if reason == "" {
		reason = "every key spent and the loot handed over"
		if c.giveUp {
			reason = "trips given up, the loot handed over"
		}
	}
	out.Produced, out.Handed = c.opened, c.handed
	// Trips given up end the chain of rounds: the keys left in the bank would start another at once.
	out.Transient = out.Transient || c.giveUp
	out.HandoverTried = c.view.HandedOver
	out.HandoverFailed = c.view.HandedOver && b.ledger.Chest.OwedTotal() > 0
	out.Why = fmt.Sprintf("%d chest(s) opened, %d item(s) handed over, %d key(s) left in the bank, %d item(s) still owed (%s)",
		c.opened, c.handed, c.view.KeysBank, b.ledger.Chest.OwedTotal(), reason)
	if c.bankVisits > 0 {
		b.logf(ctx, "chest: %d bank visit(s) took %s each on average, the slowest %s; %d Strange fruit eaten",
			c.bankVisits, (c.bankTotal / time.Duration(c.bankVisits)).Round(100*time.Millisecond), c.bankSlowest.Round(100*time.Millisecond), c.fruitEaten)
	}
	return b.chestWaits(ctx, out)
}

// alchWhy says why the chest round's alchemy is on or off.
func alchWhy(set mastercraftplan.Settings, magic int) string {
	switch {
	case !set.ChestAlch:
		return mastercraftplan.KeyChestAlch + " is off"
	case magic < mastercraftplan.ChestAlchMagic:
		return fmt.Sprintf("Magic %d is under the %d High Level Alchemy needs: the walks are stalled by the necklace's rub", magic, mastercraftplan.ChestAlchMagic)
	}
	return fmt.Sprintf("High Level Alchemy on %s, %d Nature runes and %d noted gems a trip, a Staff of fire wielded", strings.Join(mastercraftplan.ChestAlchNames(), ", "), mastercraftplan.ChestNaturesCarry, mastercraftplan.ChestFodderCarry)
}

// chestTravel is the run staller the chest round's walks carry: High Level Alchemy on the chest's loot and
// the noted gems of the kit (Magic 55 and a Nature rune), the Games necklace's rub otherwise (a rub uses no
// charge). Nil when the round has neither.
func chestTravel(opt mastercraftplan.ChestKitOptions, set mastercraftplan.Settings) *fleetrunner.Travel {
	if !opt.Alch && !opt.Route {
		return nil
	}
	alch := fleetplan.WalkAlchSettings{Enabled: opt.Alch, MinMagic: mastercraftplan.ChestAlchMagic, Items: mastercraftplan.ChestAlchNames(), MinValue: 1}
	return &fleetrunner.Travel{Alch: alch.Normalised(), Rub: true, Members: set.MembersWorld}
}

func castWord(ok bool) string {
	if ok {
		return "castable"
	}
	return "not castable"
}

// unopenedWord is the line a trip leaving the chest with keys unopened logs: how many, how many slots
// were free, and what the worst reward would have needed (the key's own slot counts as free too, so
// one slot short is still an open).
func unopenedWord(keys, free, worst, fruit int) string {
	return fmt.Sprintf("chest: leaving with %d key(s) unopened: %d slot(s) free plus the key's own, the worst reward needs %d; %d Strange fruit left to eat for room",
		keys, free, worst, fruit)
}

// homeStartWord says what the way home does instead of Camelot Teleport.
func homeStartWord(spell, mule string) string {
	if spell == "" {
		return "walking the whole way home to " + mule + "'s post"
	}
	return "casting " + spell + " first, then walking home to " + mule + "'s post"
}

// homeWord says how the way home to the mule starts at this Magic level, and why: the teleport that
// is cast from the Falador side (mastercraftplan.ChestHomeSpell), or that the whole way is walked.
func homeWord(magic int) string {
	switch spell := mastercraftplan.ChestHomeSpell(magic); spell {
	case mastercraftplan.CamelotTeleport:
		return "the way home starts with Camelot Teleport, which lands by the mule's post"
	case mastercraftplan.VarrockTeleport:
		return fmt.Sprintf("the way home starts with Varrock Teleport (needs %d), the walk to the post being shorter from there than from Falador; Camelot Teleport would save the rest",
			mastercraftplan.ChestSpellLevel(mastercraftplan.VarrockTeleport))
	}
	return fmt.Sprintf("the way home to the mule is walked (Varrock Teleport needs Magic %d)", mastercraftplan.ChestSpellLevel(mastercraftplan.VarrockTeleport))
}

// chestWaits sets what a chest round earned: it made something (opens or a hand-over), so any wait
// is cleared; it failed for a passing reason, so it backs off; or it found nothing to do, so it is
// paused for craftFruitlessPauseMinutes. A Stop sets nothing. The hand-over's own backoff is
// afterRound's (recordWaits).
func (b *masterCrafterBot) chestWaits(ctx *Context, out masterRoundOutcome) masterRoundOutcome {
	if ctx.Stopped() {
		return out
	}
	now := time.Now()
	led := &b.ledger
	switch {
	case out.Produced > 0 || out.Handed > 0:
		led.Succeed(mastercraftplan.ModeChest)
		led.Chest.PauseUntil = time.Time{}
	case out.Transient:
		b.failMode(ctx, mastercraftplan.ModeChest, now, out.Why)
	default:
		led.Chest.PauseUntil = mastercraftplan.PauseAfter(0, now, b.set.FruitlessPause)
	}
	return out
}

// kitIDs resolves the kit's items through the cache: the unnoted ids a bank draws from.
func (c *masterChest) kitIDs() mastercraftplan.KitIDs {
	ids := mastercraftplan.KitIDs{Runes: map[string][]int{}}
	for id := range c.ids.Unnoted {
		ids.Keys = append(ids.Keys, id)
	}
	sort.Ints(ids.Keys)
	ids.Coins = c.unnotedNamed(scarabrunnerCoins)
	ids.Fruit = c.unnotedNamed(mastercraftplan.ChestFruit)
	for _, name := range []string{mastercraftplan.RuneAir, mastercraftplan.RuneWater, mastercraftplan.RuneLaw, mastercraftplan.RuneFire} {
		ids.Runes[name] = c.unnotedNamed(name)
	}
	ids.Extras = c.extras()
	return ids
}

// extras are the kit's other items from the cache's ids: the Nature runes and the noted gems of the alchemy,
// a Games necklace of any charge, and the weapon a Staff of fire replaced.
func (c *masterChest) extras() []mastercraftplan.ExtraKit {
	var natures, necklaces []int
	gems, notes := map[string][]int{}, map[string][]int{}
	if c.opt.Alch {
		natures = c.unnotedNamed(mastercraftplan.NatureKit)
		for _, name := range mastercraftplan.ChestFodderNames() {
			for _, id := range c.unnotedNamed(name) {
				gems[name] = append(gems[name], id)
				notes[name] = append(notes[name], int(mastercraftrunner.NotedForm(c.ctx.Config, int32(id))))
			}
		}
	}
	if c.opt.Route {
		for _, name := range mastercraftplan.KindNecklace.DemandNames() {
			necklaces = append(necklaces, c.unnotedNamed(name)...)
		}
	}
	return mastercraftplan.ChestExtrasFor(natures, necklaces, gems, notes, int(c.prevWeapon))
}

// refreshKit reads what the kit is today: the runes a worn staff pays for (the staff can change
// between visits) and so the runes the spells need.
func (c *masterChest) refreshKit() {
	c.paid = mastercraftplan.RunesPaidByStaff(mastercraftrunner.WornNames(c.d), c.b.set.MembersWorld)
	c.runes = mastercraftplan.ChestRunes(c.magic, c.paid)
	c.kit.Extras = c.extras()
}

// kitWord describes the kit for the log.
func (c *masterChest) kitWord() string {
	var paid []string
	for name := range c.paid {
		paid = append(paid, name)
	}
	sort.Strings(paid)
	s := fmt.Sprintf("kit: %d keys a trip, runes %v, %d coins for the ships, %d Strange fruit",
		mastercraftplan.ChestKeysPerTrip, c.runes, mastercraftplan.ChestFares, mastercraftplan.ChestFruitCarry)
	if c.opt.Route {
		s += fmt.Sprintf(", %d Games necklace", mastercraftplan.ChestNecklaceCarry)
	}
	if c.opt.Alch {
		s += fmt.Sprintf(", %d Nature runes, %d noted gems", mastercraftplan.ChestNaturesCarry, mastercraftplan.ChestFodderCarry)
	}
	if len(paid) > 0 {
		s += "; a worn staff pays for " + strings.Join(paid, ", ")
	}
	return s
}

const scarabrunnerCoins = "Coins"

func (c *masterChest) unnotedNamed(name string) []int {
	var out []int
	if c.ctx.Config == nil {
		return out
	}
	for _, id := range c.ctx.Config.ObjIDs(name) {
		if o, ok := c.ctx.Config.Obj(id); ok && !o.Noted() {
			out = append(out, id)
		}
	}
	return out
}

// read refreshes the view from the account.
func (c *masterChest) read(started time.Time) {
	v := &c.view
	here := lootchestfisherrunner.Here(c.ld)
	switch {
	case here.Level == c.post.Level && tile.Chebyshev(here, c.post) <= 10:
		v.Where = mastercraftplan.ChestAtPost
	case tile.Chebyshev(here, lootchestfisherplan.ChestStand) <= 2 && here.Level == lootchestfisherplan.ChestStand.Level:
		v.Where = mastercraftplan.ChestAtChest
	case tile.Chebyshev(here, lootchestfisherplan.FaladorBankStand) <= 8 && here.Level == lootchestfisherplan.FaladorBankStand.Level:
		v.Where = mastercraftplan.ChestAtFalador
	default:
		v.Where = mastercraftplan.ChestAtOther
	}
	v.KeysPack = lootchestfisherrunner.KeysInPack(c.ld, c.ids)
	v.KeysNoted = lootchestfisherrunner.NotedKeysInPack(c.ld, c.ids)
	free, held := lootchestfisherrunner.PackFree(c.ld), lootchestfisherrunner.Held(c.ld)
	v.Room = mastercraftplan.ChestRoom(free, held)
	v.Fruit = fleetrunner.StrangeFruitHeld(c.d)
	v.EatForRoom = !v.Room && v.Fruit > 0 && mastercraftplan.ChestRoomWithFruit(free, v.Fruit, held)
	v.Alchables, v.AlchReady = c.alchables(), c.alchReady()
	v.AlchForRoom = !v.Room && v.AlchReady && v.Alchables > 0 && mastercraftplan.ChestRoomWithAlch(free, v.Alchables, held)
	v.LootInPack = mastercraftplan.ChestPackHasLoot(mastercraftrunner.PackRows(c.d), c.kit, c.runes)
	v.MuleKeysReady = false
	if f := muleBankCount(c.ctx, c.b.mule); f != nil {
		n, _ := f(mastercraftplan.ChestKeyItem)
		v.MuleKeysReady = mastercraftplan.ChestReady(n)
	}
	v.Expired = c.giveUp || mastercraftplan.RoundExpired(started, time.Now(), c.b.set.MaxRound)
}

// do runs one step. ok false is a failure to retry; end non-empty ends the round (why
// "transient" marks it a passing failure).
func (c *masterChest) do(step mastercraftplan.ChestStep) (ok bool, end, why string) {
	c.ctx.SetPhase("mastercraft/chest/" + string(step))
	switch step {
	case mastercraftplan.ChestStepAsk:
		return c.ask()
	case mastercraftplan.ChestStepBank:
		return c.bank()
	case mastercraftplan.ChestStepToChest:
		return c.toChest()
	case mastercraftplan.ChestStepOpen:
		return c.open()
	case mastercraftplan.ChestStepAlch:
		return c.alchForRoom()
	case mastercraftplan.ChestStepEat:
		return c.eatForRoom()
	case mastercraftplan.ChestStepToFalador:
		return c.toFalador()
	case mastercraftplan.ChestStepHome:
		return c.home()
	case mastercraftplan.ChestStepHandover:
		return c.handover()
	}
	return false, "unknown step " + string(step), ""
}

// ask banks the pack at the post, reads the bank and asks the mule for all its keys, and about a
// thousand Strange fruit as notes, as many as it has.
func (c *masterChest) ask() (bool, string, string) {
	b, ctx := c.b, c.ctx
	// A second ask, after a resumed round's hand-over, starts the round again from the post.
	c.view.HandedOver, c.view.Resumed, c.view.PostBanked = false, false, false
	c.refreshKit()
	v := mastercraftrunner.DepositAll(c.d, c.post)
	if !v.Opened {
		return false, "", "the bank at the post: " + v.Err
	}
	own := 0
	for id := range c.ids.Unnoted {
		own += int(actions.CountByID(v.Banked, int32(id)))
	}
	ownOf := func(name string) int {
		n := 0
		for _, id := range c.unnotedNamed(name) {
			n += int(actions.CountByID(v.Banked, int32(id)))
		}
		return n
	}
	c.view.KeysBank = own
	var stock mastercraftplan.Stock
	if f := muleBankCount(ctx, b.mule); f != nil {
		stock = f
	}
	lines := mastercraftplan.ChestAskFor(c.opt, stock, c.magic, c.paid, ownOf)
	owed := b.ledger.Chest.OwedTotal()
	if len(lines) > 0 {
		// Everything as notes (one pack slot a line), the pack having just been banked.
		a := b.askMuleWith(ctx, c.d, lines, masterAskOpts{noted: true, banked: true})
		got := a.Got[mastercraftplan.ChestKeyItem]
		b.ledger.Chest.KeysGot += got
		b.logf(ctx, "chest: asked %s for %s: %s (own bank: %d key(s), %d Strange fruit)", b.mule, describeLines(lines), masterDescribe(a.Got), own, ownOf(mastercraftplan.ChestFruit))
		if a.Stopped {
			return false, "stopped", ""
		}
		if got == 0 && own == 0 && owed == 0 {
			if a.declinedEmpty() {
				return false, "the mule had no keys to give: " + a.Why, ""
			}
			return false, "no keys arrived: " + a.Why, "transient"
		}
	} else if own == 0 && owed == 0 {
		return false, "nothing to do: the mule holds no more than 100 Crystal keys and the bank none", ""
	}
	c.view.Asked = true
	b.ledger.Chest.KeysBank = own
	b.saveLedger(ctx)
	return true, "", ""
}

// bank is a kit visit, at the post or the Falador west bank: the pack is banked whole and the kit
// drawn again (mastercraftrunner.ChestKit).
func (c *masterChest) bank() (bool, string, string) {
	start := time.Now()
	atPost := c.view.Where == mastercraftplan.ChestAtPost
	if atPost {
		c.wieldStaff() // once a round, at the post: the Staff of fire the alchemy and Varrock Teleport use
	}
	c.refreshKit()
	var bank *actions.Bank
	var ok bool
	if atPost {
		bank, ok = mastercraftrunner.OpenBank(c.d, c.post)
	} else {
		bank, ok = chestOpenFaladorBank(c.ld, lootchestfisherplan.FaladorBankStand, 20)
	}
	if !ok || bank == nil {
		return false, "", "the bank would not open"
	}
	opened := time.Since(start)
	expired := c.view.Expired
	spec := mastercraftrunner.ChestKitSpec{IDs: c.kit, Want: func(keysBanked int) mastercraftplan.KitWant {
		return mastercraftplan.ChestKitWantFor(c.opt, atPost, c.magic, c.paid, expired, keysBanked)
	}}
	if c.prevWeapon != 0 {
		spec.Keep = func(id int32) bool { return id == c.prevWeapon }
	}
	res := mastercraftrunner.ChestKit(c.d, bank, spec)
	if res.Banked == nil && res.Err != "" {
		return false, "", res.Err
	}
	if res.Err != "" {
		c.b.logf(c.ctx, "chest: the bank visit said %s", res.Err)
	}
	c.view.KeysBank = res.KeysBank
	if atPost {
		c.view.PostBanked = true
	}
	if led := &c.b.ledger.Chest; led.KeysBank != res.KeysBank {
		led.KeysBank = res.KeysBank
		c.b.saveLedger(c.ctx)
	}
	took := time.Since(start)
	c.bankVisits++
	c.bankTotal += took
	c.bankSlowest = max(c.bankSlowest, took)
	where := "Falador"
	if atPost {
		where = "the post"
	}
	c.b.logf(c.ctx, "chest: banked at %s in %s (bank open in %s, kit %s) — %d key(s) drawn, %d in hand, %d left in the bank; %d Strange fruit and %d coins carried; %d chest(s) opened so far",
		where, took.Round(100*time.Millisecond), opened.Round(100*time.Millisecond), res.Took.Round(100*time.Millisecond),
		res.KeysDrawn, lootchestfisherrunner.KeysInPack(c.ld, c.ids), res.KeysBank,
		fleetrunner.StrangeFruitHeld(c.d), lootchestfisherrunner.CoinsHeld(c.ld), c.opened)
	return true, "", ""
}

// packRune is the pack's count of a rune by name.
func (c *masterChest) packRune(name string) int { return mastercraftrunner.PackCountNamed(c.d, name) }

// teleport casts spell when Magic and the pack's runes (and a worn staff) allow it; false when it was
// not cast or did not move the account.
func (c *masterChest) teleport(spell string) bool {
	c.refreshKit()
	if !mastercraftplan.CanCastWith(spell, c.magic, c.paid, c.packRune) {
		return false
	}
	arrived, err := c.ctx.Teleport(spell, 0)
	if err != nil || !arrived {
		c.b.logf(c.ctx, "chest: %s did not land (%v) — walking instead", spell, err)
		return false
	}
	c.b.logf(c.ctx, "chest: cast %s, now at %s", spell, fleetrunner.HereIs(c.d))
	return true
}

// masterChestLegWalk bounds a walk on the Falador side, bank to chest or back: 160 tiles and two doors,
// about 40 seconds when the run holds and under three minutes walked, so five minutes is a walk that
// is stuck. Under it the walker plans again a minute into a leg that gets nowhere (webwalk.
// noProgressFor); this is what ends one that cannot be mended, so a trip never loses ten minutes to
// the road (the main account, 5 Oct 2026, at 2951,3430).
const masterChestLegWalk = 5 * time.Minute

// masterChestFruitEvery is how often a walk looks at the run energy.
const masterChestFruitEvery = 2 * time.Second

// walk goes to dest: on the Falador side the walk keeps running and eats a Strange fruit when run
// energy is low (walkFalador), else ScarabSwarm's wolf-free one (the ships).
func (c *masterChest) walk(dest tile.Tile, radius int) bool {
	if lootchestfisherrunner.OnFaladorSide(lootchestfisherrunner.Here(c.ld)) && lootchestfisherrunner.OnFaladorSide(dest) {
		return c.walkFalador(dest, radius)
	}
	return masterWalkTo(c.d, dest, radius, masterChestLongWalk)
}

// walkFalador is lootchestfisherrunner.WalkNear for the Falador side with the chest round's own
// extras: run is on from the first step, a Strange fruit is eaten whenever run energy falls to
// mastercraftplan.ChestFruitRunAt (a user request: "crystal chest runs could be sped up by using
// strange fruit to restore run energy while running from the bank to the chest"), and the walk is
// bounded by masterChestLegWalk.
func (c *masterChest) walkFalador(dest tile.Tile, radius int) bool {
	here := lootchestfisherrunner.Here(c.ld)
	if tile.Chebyshev(here, dest) <= radius {
		return true
	}
	w := c.b.walker
	if w == nil {
		return lootchestfisherrunner.WalkNear(c.ld, dest, radius, masterChestLegWalk)
	}
	c.keepRunning()
	// The walk's own work (the fruit) and the run staller's: High Level Alchemy on the loot and the noted gems, else
	// the necklace's rub (scarabrunner.Travel.WalkStall); the far clicks it aims with are the staller's.
	aside, clickSteps, done := c.travel.WalkStall(c.d, webwalk.Point{X: dest.X, Z: dest.Z, Level: dest.Level}, c.walkAside)
	res := w.WalkTo(webwalk.Point{X: dest.X, Z: dest.Z, Level: dest.Level}, webwalk.WalkOptions{
		Radius: radius, Timeout: masterChestLegWalk,
		AvoidHostiles: freshstartrunner.HostileEvadeEnabled,
		Aside:         aside, ClickSteps: clickSteps,
	})
	done()
	if res.Arrived() {
		return true
	}
	c.b.logf(c.ctx, "walk to %d,%d,%d did not arrive (%v: %s), stopped at %v", dest.X, dest.Z, dest.Level, res.Outcome, res.Reason, res.End)
	return false
}

// keepRunning turns run on when the bar can pay for it (the walk helpers' own threshold) and it is off:
// the engine drops it at 0%, and nothing else turns it back on during a long walk.
func (c *masterChest) keepRunning() {
	st := c.ctx.Session.State()
	if energy, known := st.Energy(); known && energy >= freshstartrunner.RunEnergyThreshold && !st.RunEnabled() {
		if err := actions.SetRun(c.ctx.Session, true); err != nil {
			c.b.logf(c.ctx, "chest: turning run on: %v", err)
		}
	}
}

// walkAside is the walk's poll hook (webwalk.WalkOptions.Aside): keep running, and eat a Strange fruit
// when run energy is low. It returns quickly and sends nothing that moves the account; a bite does
// not stop a walk (OpHeld clears the interaction, not the path).
func (c *masterChest) walkAside(webwalk.AsidePoll) {
	now := time.Now()
	if now.Sub(c.fruitAt) < masterChestFruitEvery {
		return
	}
	c.fruitAt = now
	st := c.ctx.Session.State()
	if main, _, chat := st.Modals(); main != gamestate.NoModal || chat != gamestate.NoModal {
		return // a bank, shop or dialogue is open: a bite now would close it
	}
	c.keepRunning()
	energy, known := st.Energy()
	if mastercraftplan.ChestEatFruit(energy, known, fleetrunner.StrangeFruitHeld(c.d)) {
		c.eat(fmt.Sprintf("run energy %d%%", energy))
	}
}

// eat eats one Strange fruit and counts it; false when it did not leave the pack. A bite is held until
// mastercraftplan.ChestBiteGap after the last, which content would refuse inside it.
func (c *masterChest) eat(why string) bool {
	if wait := mastercraftplan.ChestBiteWait(time.Since(c.lastBite)); wait > 0 {
		c.ctx.Delay(wait)
	}
	c.lastBite = time.Now()
	if !fleetrunner.EatStrangeFruit(c.d) {
		return false
	}
	c.fruitEaten++
	c.b.logf(c.ctx, "chest: ate a Strange fruit (%s; %d left)", why, fleetrunner.StrangeFruitHeld(c.d))
	return true
}

// eatForRoom eats a Strange fruit at the chest: the pack has no room for the next open and would have
// it once its fruit were eaten.
func (c *masterChest) eatForRoom() (bool, string, string) {
	if !c.eat("for the slot, at the chest") {
		return false, "", "the Strange fruit would not be eaten"
	}
	return true, "", ""
}

func (c *masterChest) toChest() (bool, string, string) {
	// The Games Room route: a necklace to Burthorpe and the stairs out, from the post or the Falador bank (the
	// setting says which); a route that fails twice is not tried again this round, and one that left the account
	// in the room goes on by teleport or walk like any other leg.
	viaGames := false
	if !c.routeOff && mastercraftplan.GamesRouteFrom(c.b.set.ChestGamesRoute, c.view.Where, mastercraftrunner.GamesNecklaceHeld(c.d)) {
		viaGames = c.gamesRoute()
	}
	if !viaGames && !lootchestfisherrunner.OnFaladorSide(lootchestfisherrunner.Here(c.ld)) {
		c.teleport(mastercraftplan.FaladorTeleport)
	}
	snap := c.alchSnapshot()
	ok := c.walk(lootchestfisherplan.ChestStand, 2)
	c.noteAlched(snap)
	if !ok {
		return false, "", "could not reach the chest from " + fleetrunner.HereIs(c.d)
	}
	return true, "", ""
}

// gamesRoute rubs the Games necklace to the Burthorpe Games Room and climbs out of it; true when the account then
// stands in Burthorpe. A failure is logged and counted; when the account is left in the room, a teleport spell
// takes it out (it is not the Wilderness).
func (c *masterChest) gamesRoute() bool {
	if c.travel != nil {
		c.travel.CloseRubMenu(c.d)
	}
	hold := func() bool { return !c.ctx.Stopped() }
	r := chestGamesRoute(c.d, hold)
	if r.Out {
		c.routeFlown++
		c.b.logf(c.ctx, "chest: %s (route %d of the round; the chest is 141 tiles on from there)", r.Describe(), c.routeFlown)
		return true
	}
	c.routeFails++
	c.b.logf(c.ctx, "chest: %s", r.Describe())
	if c.routeFails >= 2 {
		c.routeOff = true
		c.b.logf(c.ctx, "chest: the Games Room route failed twice: not tried again this round")
	}
	if r.Landed && !r.Out {
		c.teleport(mastercraftplan.FaladorTeleport)
	}
	return false
}

// wieldStaff wields a Staff of fire once a round, at the post, when the alchemy is on and none is worn: from this
// account's own bank or the mule (never bought: the round does not walk to a shop). The weapon it replaces is kept in
// the pack through the bank visits and wielded again before the hand-over.
func (c *masterChest) wieldStaff() {
	if c.staffTried || !c.opt.Alch {
		return
	}
	c.staffTried = true
	staff, previous, why := c.b.fireStaffOpt(c.ctx, c.d, c.post, ownBankItems(c.ctx), false)
	switch {
	case staff == 0 && why == "":
		c.b.logf(c.ctx, "chest: a Staff of fire is already worn: it pays the Fire runes of the alchemy")
	case why != "":
		c.b.logf(c.ctx, "chest: no Staff of fire wielded (%s): the alchemy pays its Fire runes from the pack", why)
	default:
		c.prevWeapon = previous
		c.b.logf(c.ctx, "chest: wielded a Staff of fire (the weapon it replaced, %d, is kept to wield again at the hand-over)", previous)
	}
	c.refreshKit()
}

// alchables is the unnoted loot in the pack the round alchs (the noted gems of the kit are the walks'): by name.
func (c *masterChest) alchables() int {
	n := 0
	for _, it := range c.packItems() {
		if o, ok := c.ctx.Config.Obj(int(it.ID)); ok && !o.Noted() && mastercraftplan.ChestAlchable(o.Name) {
			n += max(1, int(it.Count))
		}
	}
	return n
}

// packItems is the pack's rows.
func (c *masterChest) packItems() []gamestate.SlotItem {
	if c.ctx.Config == nil || c.ctx.Session == nil {
		return nil
	}
	inv := c.ctx.Session.State().Inventory(actions.PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// alchReady is whether a cast can go now: the alchemy is on for this round, Magic and the runes (a Nature rune, and
// the Fire runes or a worn Staff of fire) are there.
func (c *masterChest) alchReady() bool {
	if !c.opt.Alch || c.alchOff || c.travel == nil || c.ctx.Config == nil {
		return false
	}
	if fleetrunner.MagicLevel(c.d) < mastercraftplan.ChestAlchMagic || c.packRune(mastercraftplan.NatureKit) <= 0 {
		return false
	}
	return mastercraftrunner.FireStaffWorn(c.d) || c.packRune(mastercraftplan.RuneFire) >= fleetplan.WalkAlchFirePerCast
}

// alchSnapshot is the alchable loot in the pack by bank object id.
func (c *masterChest) alchSnapshot() map[int]int {
	out := map[int]int{}
	if c.ctx.Config == nil {
		return out
	}
	for _, it := range c.packItems() {
		if o, ok := c.ctx.Config.Obj(int(it.ID)); ok && !o.Noted() && mastercraftplan.ChestAlchable(o.Name) {
			out[int(it.ID)] += max(1, int(it.Count))
		}
	}
	return out
}

// noteAlched takes what the snapshot held and the pack no longer does off what is owed to the mule: the loot that was
// cast away is coins now and is not the mule's. It returns the pieces counted.
func (c *masterChest) noteAlched(before map[int]int) int {
	after := c.alchSnapshot()
	total := 0
	for id, n := range before {
		if gone := n - after[id]; gone > 0 {
			c.b.ledger.Chest.RemoveOwed(id, gone)
			total += gone
		}
	}
	if total > 0 {
		c.alched += total
		c.b.saveLedger(c.ctx)
		c.b.logf(c.ctx, "chest: alched %d piece(s) of loot (%d this round), %d still owed to %s", total, c.alched, c.b.ledger.Chest.OwedTotal(), c.b.mule)
	}
	return total
}

// alchForRoom casts High Level Alchemy at the chest on a piece of its loot, so the pack has room for the next open.
func (c *masterChest) alchForRoom() (bool, string, string) {
	if c.travel == nil {
		return false, "", "no run staller to cast with"
	}
	snap := c.alchSnapshot()
	deadline := time.Now().Add(10 * time.Second)
	sent := false
	for time.Now().Before(deadline) && !sent {
		if c.ctx.Stopped() {
			return false, "stopped", ""
		}
		masterDismissChat(c.ctx)
		if sent = chestIdleAlch(c.travel, c.d); !sent && !c.ctx.Delay(150*time.Millisecond) {
			return false, "stopped", ""
		}
	}
	if !sent {
		c.alchMisses++
		if c.alchMisses >= 2 {
			c.alchOff = true
			c.b.logf(c.ctx, "chest: High Level Alchemy would not cast twice: no more alchemy this round")
		}
		return false, "", "High Level Alchemy was not sent"
	}
	if !c.ctx.Delay(c.travel.IdleAlchWait()) {
		return false, "stopped", ""
	}
	if c.noteAlched(snap) == 0 {
		c.alchMisses++
		if c.alchMisses >= 2 {
			c.alchOff = true
			c.b.logf(c.ctx, "chest: the cast alched nothing twice: no more alchemy this round")
		}
		return false, "", "the cast did not take a piece of loot"
	}
	c.alchMisses = 0
	return true, "", ""
}

func (c *masterChest) toFalador() (bool, string, string) {
	if c.view.KeysPack > 0 && c.view.Where == mastercraftplan.ChestAtChest && !c.view.Expired {
		// A trip that leaves the chest with keys in hand says why (a user request: "it went all the
		// way to the chest and didnt loot it that run"): the room rule stopped it, with this much free.
		c.b.logf(c.ctx, "%s", unopenedWord(c.view.KeysPack, lootchestfisherrunner.PackFree(c.ld), lootchestfisherplan.WorstCaseNewSlots(lootchestfisherrunner.Held(c.ld)), c.view.Fruit))
	}
	c.teleport(mastercraftplan.FaladorTeleport)
	snap := c.alchSnapshot()
	ok := c.walk(lootchestfisherplan.FaladorBankStand, 3)
	c.noteAlched(snap)
	if !ok {
		return false, "", "could not reach the Falador west bank from " + fleetrunner.HereIs(c.d)
	}
	return true, "", ""
}

// home banks everything but the kit (the fare, the runes of the teleport home) at Falador, then casts
// that teleport (Camelot's, or Varrock's from the Falador side) and walks to the post, or walks the
// whole way.
func (c *masterChest) home() (bool, string, string) {
	if c.view.Where == mastercraftplan.ChestAtFalador && (c.view.LootInPack || c.view.KeysPack > 0) {
		c.view.Expired = true
		if ok, end, why := c.bank(); !ok {
			return ok, end, why
		}
	}
	// The teleport that starts the way home: Camelot's by the post, else Varrock's from the Falador
	// side (mastercraftplan.ChestHomeSpellFrom). Short of both, the whole way is walked.
	spell := mastercraftplan.ChestHomeSpellFrom(c.magic, lootchestfisherrunner.OnFaladorSide(lootchestfisherrunner.Here(c.ld)))
	if spell != mastercraftplan.CamelotTeleport {
		c.b.logf(c.ctx, "chest: Camelot Teleport needs Magic %d and this account has %d: %s",
			mastercraftplan.ChestSpellLevel(mastercraftplan.CamelotTeleport), c.magic, homeStartWord(spell, c.b.mule))
	}
	if spell != "" {
		c.teleport(spell)
	}
	if !masterWalkTo(c.d, c.post, 3, masterChestLongWalk) {
		return false, "", "could not reach " + c.b.mule + "'s post from " + fleetrunner.HereIs(c.d)
	}
	return true, "", ""
}

// open opens the chest once and takes the reward, and anything of it that landed on the ground.
func (c *masterChest) open() (bool, string, string) {
	st := c.ctx.Session.State()
	before := masterGroundStacks(st.GroundItems())
	res := lootchestfisherrunner.OpenChest(c.ld, c.ids)
	if res.Stopped {
		return false, "stopped", ""
	}
	if !res.Opened {
		return false, "", res.Reason
	}
	c.opened++
	led := &c.b.ledger
	led.Chest.Opened++
	for id, n := range res.Gained {
		led.Chest.AddOwed(mastercraftrunner.BankForm(c.ctx.Config, id), int(n))
	}
	c.sweep(before)
	c.b.saveLedger(c.ctx)
	if c.opened%6 == 0 || c.view.KeysPack <= 1 {
		c.b.logf(c.ctx, "chest: opened %d this round (%d key(s) left in hand); %d item(s) owed to %s",
			c.opened, lootchestfisherrunner.KeysInPack(c.ld, c.ids), led.Chest.OwedTotal(), c.b.mule)
	}
	return true, "", ""
}

// sweep picks up the chest's rewards that landed at the standing tile in the last open.
func (c *masterChest) sweep(before []mastercraftplan.GroundStack) {
	st := c.ctx.Session.State()
	stand := lootchestfisherplan.ChestStand
	drops := mastercraftplan.NewChestDrops(before, masterGroundStacks(st.GroundItems()), stand.X, stand.Z, stand.Level)
	for _, g := range drops {
		if lootchestfisherrunner.PackFree(c.ld) == 0 && lootchestfisherrunner.Held(c.ld)(g.ID) == 0 {
			c.b.logf(c.ctx, "chest: WARNING %d of item %d lie at the chest and the pack is full", g.Count, g.ID)
			continue
		}
		had := lootchestfisherrunner.Held(c.ld)(g.ID)
		var types actions.ObjTypes
		if c.ctx.Config != nil {
			types = c.ctx.Config
		}
		item := groundItemOf(g)
		if err := actions.TakeGroundItem(c.ctx.Session, types, item); err != nil {
			c.b.logf(c.ctx, "chest: taking item %d off the ground: %v", g.ID, err)
			continue
		}
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) && lootchestfisherrunner.Held(c.ld)(g.ID) <= had {
			if !c.ctx.Delay(200 * time.Millisecond) {
				return
			}
		}
		if got := lootchestfisherrunner.Held(c.ld)(g.ID) - had; got > 0 {
			c.b.ledger.Chest.AddOwed(mastercraftrunner.BankForm(c.ctx.Config, g.ID), got)
			c.b.logf(c.ctx, "chest: picked up %d of item %d the chest dropped on the ground", got, g.ID)
		}
	}
}

// handover hands the owed loot to the mule; the round ends after it either way.
func (c *masterChest) handover() (bool, string, string) {
	if c.prevWeapon != 0 {
		// The hand-over banks the pack: the weapon the Staff of fire replaced is wielded again first.
		mastercraftrunner.Rewield(c.d, c.prevWeapon)
		c.prevWeapon = 0
	}
	handed, failed, why := c.b.handOverChest(c.ctx, c.d, c.post)
	c.handed += handed
	c.view.HandedOver = true
	if failed {
		c.b.logf(c.ctx, "chest: the hand-over left %d item(s) owed: %s", c.b.ledger.Chest.OwedTotal(), why)
	}
	return true, "", ""
}

// handOverChest draws the owed loot out of the bank as notes, by quantity, and trades it to the
// mule at its post (masterDeliver), in up to masterHandoverPasses loads; then banks what did not
// cross and settles the owed loot against what crossed and what the bank still holds.
func (b *masterCrafterBot) handOverChest(ctx *Context, d *fleetrunner.Deps, post tile.Tile) (handed int, failed bool, why string) {
	led := &b.ledger.Chest
	if led.OwedTotal() == 0 {
		return 0, false, ""
	}
	handedByID := map[int]int{}
	p := scarabMulePresenceOf(ctx, b.mule)
	switch {
	case ctx.Bus == nil || ctx.Username == "":
		why = "no wall bus to hand over on"
	case !p.Heard || !p.SameWorld:
		why = "the mule is not here: " + p.Why
	default:
		for pass := 0; pass < masterHandoverPasses && !ctx.Stopped(); pass++ {
			masterClearDead(ctx)
			ids, owed := led.OwedIDs()
			var wants []mastercraftrunner.LootWant
			var names []string
			seen := map[string]bool{}
			for _, id := range ids {
				left := owed[id] - handedByID[id]
				if left <= 0 {
					continue
				}
				name := masterObjName(ctx, id)
				wants = append(wants, mastercraftrunner.LootWant{ID: int32(id), Name: name, Qty: left})
				if !seen[strings.ToLower(name)] {
					seen[strings.ToLower(name)] = true
					names = append(names, name)
				}
			}
			if len(wants) == 0 {
				break
			}
			h, gotByID := mastercraftrunner.DrawLoot(d, post, wants)
			if !h.Opened || h.Err != "" && h.Rows == 0 {
				why = "drawing the loot: " + h.Err
				break
			}
			if h.Rows == 0 {
				break // none of it in the bank
			}
			rd := fleetrunner.MuleDeps(d, ctx.Bus, ctx.Username)
			WireDonorWorlds(ctx, rd)
			rd.HomeWorld = 0 // gated on the mule being on this world: no hop
			r := masterDeliver(d, rd, b.mule, post, h.Rows, h.Slots, names)
			result := "traded"
			switch {
			case r.Hopping:
				result = "hopping"
			case r.GaveUp:
				result = "gave-up"
			case !r.Traded:
				result = "not-traded"
			}
			b.logf(ctx, "HANDOVER chest mule=%s drawn=%s delivered=%s result=%s reason=%q pass=%d",
				b.mule, masterDescribe(h.Got), masterDescribe(r.Delivered), result, r.Reason, pass+1)
			creditByName(r.Delivered, wants, gotByID, handedByID)
			if r.Hopping {
				why = "the session is hopping worlds"
				break
			}
			if !r.Traded {
				why = fmt.Sprintf("the trade did not happen (%s): %s", result, r.Reason)
				break
			}
		}
	}
	for _, n := range handedByID {
		handed += n
	}
	led.Handed += handed
	v := mastercraftrunner.DepositAll(d, post)
	if !v.Opened {
		led.Settle(handedByID, nil)
		b.saveLedger(ctx)
		return handed, true, firstNonEmpty(why, "the bank would not open to bank what was left: "+v.Err)
	}
	led.Settle(handedByID, func(id int) int { return int(actions.CountByID(v.Banked, int32(id))) })
	b.saveLedger(ctx)
	return handed, led.OwedTotal() > 0, why
}

// creditByName turns what crossed, by name, back into object ids: two ids can share a name (the
// two key halves are both "Half of a key"), so a name's count is shared out over the ids drawn
// under it.
func creditByName(delivered map[string]int, wants []mastercraftrunner.LootWant, gotByID map[int32]int, handedByID map[int]int) {
	for name, n := range delivered {
		for _, w := range wants {
			if n <= 0 {
				break
			}
			if !strings.EqualFold(w.Name, name) {
				continue
			}
			take := min(n, gotByID[w.ID])
			if take <= 0 {
				continue
			}
			handedByID[int(w.ID)] += take
			gotByID[w.ID] -= take
			n -= take
		}
	}
}

// masterObjName is an object's name, or its id when the cache does not know it.
func masterObjName(ctx *Context, id int) string {
	if ctx.Config != nil {
		if o, ok := ctx.Config.Obj(id); ok && o.Name != "" {
			return o.Name
		}
	}
	return fmt.Sprintf("item %d", id)
}

// masterGroundStacks is the session's ground items as the plan's stacks.
func masterGroundStacks(items []gamestate.GroundItem) []mastercraftplan.GroundStack {
	out := make([]mastercraftplan.GroundStack, 0, len(items))
	for _, g := range items {
		out = append(out, mastercraftplan.GroundStack{ID: g.ID, Count: g.Count, X: g.X, Z: g.Z, Level: g.Level})
	}
	return out
}

func groundItemOf(g mastercraftplan.GroundStack) gamestate.GroundItem {
	return gamestate.GroundItem{ID: g.ID, Count: g.Count, X: g.X, Z: g.Z, Level: g.Level}
}
