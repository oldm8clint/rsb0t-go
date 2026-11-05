package questrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// The live half of the provisioner: it executes questplan.PlanProvision one phase at a time and
// re-plans after each, so every fall-through (a short bank, a declined mule, an empty shelf, a
// ground spawn that never came back) is simply the next pass choosing the next rung.

// MuleAsker is the quest engine's ONE door to the mule. The engine never talks to mulebus or
// donorrunner itself: ScarabSwarm's single mule-ask entry (lane P, internal/bot/
// scarabswarm_muleask.go) and the bundled kit order (lane H, scarabrunner/kitbundle.go) will
// implement this, and a standalone caller adapts whatever mule plumbing it has (internal/bot's
// QuestLadder adapts donorrunner.Await per item until the bundle lands).
//
// The contract, from AIO-PLAN A7 and the owner's rules:
//   - Published is the mule's PUBLISHED bank by object id, and ok=false when there is no mule
//     or it has not been heard (a stale reading is not a reading). The provisioner filters the
//     ask by it BEFORE any walk, so an empty mule costs nothing.
//   - Ask places ONE bundled order (never more than questplan.MaxBundleSlots unstacked items)
//     and walks wherever it needs to. It returns what arrived in the pack by object id and a
//     reason when anything did not. It never blocks the quest: a decline or a timeout is an
//     answer, and the provisioner falls through to ground, skill and shop.
type MuleAsker interface {
	Published() (map[int]int, bool)
	Ask(purpose string, lines []questplan.ItemCount) (got map[int]int, reason string)
}

// SkillMethod works one skill source (thieving cake, mining clay ...) and reports whether it
// produced anything. It is registered by name on the Provisioner.
type SkillMethod func(d *Deps, task questplan.SourceTask) bool

// Provisioner carries what one account's provisioning needs across steps.
type Provisioner struct {
	// Mule is nil when the account has no mule.
	Mule MuleAsker
	// Purpose prefixes every mule ask ("quest:waterfall").
	Purpose string
	// Methods are the skill sources by method name.
	Methods map[string]SkillMethod
	// Bank is the engine's cached bank view.
	Bank *BankView
	// DefaultBank is where to bank when a step names none.
	DefaultBank tile.Tile
	// Disabled makes every provisioning run fail at once. It exists for a harness's CONTROL
	// arm only: the same quest, the same fixture, no provisioner, must not complete.
	Disabled bool
	// SourcesDisabled is the deeper control: the provisioner still reads the bank, withdraws
	// what the bank holds and accepts a kit the pack already satisfies, but never asks the mule
	// and never works a ground, skill or shop source. A control arm with it goes as far as the
	// quest can without the item rule's acquisition, and must still fail where items are needed.
	SourcesDisabled bool
	// TravelFloat is the coins drawn on top of shop prices for the trip (ship fares); 0 means
	// DefaultTravelFloat.
	TravelFloat int
	// LastBlocked is whether the last Provision call failed because the plan was BLOCKED (no
	// source left for a line, or the passes ran out), not because a walk or a bank visit on the
	// way failed. The engine publishes it as Snapshot.Blocked.
	LastBlocked bool
	// LastTrain is the stints the last Provision call ended asking for (questplan.PhaseTrain): the kit is complete but for a
	// skill source the account is below the level of, which the trainer catalogue can train. The runner never works it: the
	// engine reads it with TakeTrain after a failed step, ends the run (which releases a crowded quest's fleet-wide slot) and
	// parks the quest on "needs:<skill>:<level>", so the stint runs outside the slot. A Provision call that ends any other
	// way leaves it empty.
	LastTrain []questplan.TrainNeed
	// LastErrand is set when the round's one bundled mule ask was not answered here because a world errand carries it (the mule is
	// on another world): the kit arrives when the errand is home, so the round is not a dead end. The engine reads it with
	// TakeErrand after a failed step and parks the quest for ErrandParkFor instead of letting it wait on a kit that "could not be
	// filled" (live 5-6 Nov 2026: Olaf's Raw shark, 93k on the mule, was asked for, carried by the errand, and the trial still
	// parked on the blocked kit for the rest of the run).
	LastErrand string
	// Trainers is the catalogue a Train source is checked against; nil is questplan.SharedCatalogue.
	Trainers *questplan.Catalogue
	// bankFails counts the bank visits in a row that failed (the walk or the booth); at
	// bankFailsBlocked the plan counts as blocked. Live 3 Oct 13:30: accounts in Kandarin tried
	// Priest in Peril's essence bank at 3253,3420, which their walker calls unreachable, every 16 s
	// (172 failed routes for acct56 in 7 minutes) without the quest ever parking.
	bankFails int
	// LastTravelBlocked is whether the last Provision call failed because a source's walk could not arrive (the
	// source is then dead for SourceDeadFor, see deadSources), not because the world holds nothing: the engine
	// parks the quest after ProvisionTravelParkRounds such rounds in a row.
	LastTravelBlocked bool
	// deadSources are the sources whose walk failed, until when. A source whose walk is unreachable fails the rest
	// of this round and the next few minutes at once, instead of walking (and failing) again every five seconds:
	// live 7 Oct 2026 Imp Catcher at Draynor asked for the imp field at Ardougne 20+ minutes running.
	deadSources map[string]time.Time
	// bankVisitFn replaces bankVisit in tests (nil: the real one).
	bankVisitFn func(d *Deps, bankTile tile.Tile, withdraw []questplan.ItemCount, keep func(id int) bool) bool
	// SourceUnavailable, when set, says why a skill or market executor (by its Method name) cannot
	// be worked by this account at all right now, or "" when it can. The caller knows what the
	// quest engine does not: which world the account is on, which supply mode it runs in. An
	// unavailable source is skipped up front, with its reason in the log and in the BLOCKED line,
	// instead of being walked to and reported as a source that failed this round (live 14 Oct 2026:
	// 18 of 18 market-essence rounds "failed", 14 because the Seers stalls do not stand on the
	// account's world and 4 because its supply mode shops at no player market).
	SourceUnavailable func(method string) string
}

// TakeTrain returns the stints the last Provision call waits for and forgets them, so a train need that was read once is
// never read again by a later, unrelated failure.
func (p *Provisioner) TakeTrain() []questplan.TrainNeed {
	n := p.LastTrain
	p.LastTrain = nil
	return n
}

// ErrandMarker is the text a MuleAsker's reason carries when a world errand takes the ask (scarabswarm_worldseam.go).
const ErrandMarker = "a world errand carries the ask"

// ErrandParkFor is how long a quest whose kit rides a world errand is parked: the errand hops to the mule's world, trades and hops
// home (about five minutes on a clear fleet), and the kit is then in the pack or the bank the next pass reads.
const ErrandParkFor = 8 * time.Minute

// TakeErrand returns the reason the last Provision call left its ask to a world errand and forgets it.
func (p *Provisioner) TakeErrand() string {
	r := p.LastErrand
	p.LastErrand = ""
	return r
}

func (p *Provisioner) visitBank() func(d *Deps, bankTile tile.Tile, withdraw []questplan.ItemCount, keep func(id int) bool) bool {
	if p.bankVisitFn != nil {
		return p.bankVisitFn
	}
	return p.bankVisit
}

// SourceDeadFor is how long a source whose walk could not arrive is skipped.
const SourceDeadFor = 5 * time.Minute

// ProvisionTravelParkRounds is how many Provision rounds in a row blocked by an unreachable source park the quest.
const ProvisionTravelParkRounds = 3

// bankFailsBlocked is how many failed bank visits in a row make a provision BLOCKED.
const bankFailsBlocked = 3

// bankStalledBlocked is how many bank visits in a row that were to withdraw something and changed nothing in the
// pack make a provision BLOCKED, instead of running out the remaining passes.
const bankStalledBlocked = 3

// keptInPack reports whether Deps.KeepInPack claims the pack item id (it does not in a bare-pack stretch, which
// banks the account's own kit too).
func keptInPack(d *Deps, id int) bool {
	return d.KeepInPack != nil && !d.InBarePack() && d.KeepInPack(ObjName(d, id))
}

// sameCounts reports two pack readings equal.
func sameCounts(a, b map[int]int) bool {
	if len(a) != len(b) {
		return false
	}
	for id, n := range a {
		if b[id] != n {
			return false
		}
	}
	return true
}

// describeWithdraw is a withdrawal list in the log's words.
func describeWithdraw(ws []questplan.ItemCount) string {
	parts := make([]string, 0, len(ws))
	for _, w := range ws {
		name := w.Name
		if name == "" {
			name = fmt.Sprintf("obj %d", w.ID)
		}
		parts = append(parts, fmt.Sprintf("%d %s", w.Count, name))
	}
	return strings.Join(parts, ", ")
}

// DefaultTravelFloat covers two ship fares and a price rise.
const DefaultTravelFloat = 200

// provisionPasses bounds one provisioning run. Each pass is one phase (a bank visit, the mule
// ask, one source task), and a kit has at most a handful of lines, so this is several times any
// real run while still ending a loop the plan cannot escape.
const provisionPasses = 16

// Provision makes the pack hold kit, by the item rule. keep says which non-kit pack items may
// stay when it banks first. It reports success and, on failure, why.
func (p *Provisioner) Provision(d *Deps, name string, kit []questplan.KitLine, keep func(id int) bool, bankTile tile.Tile) (bool, string) {
	if p.Bank == nil {
		p.Bank = &BankView{}
	}
	p.LastBlocked = false
	p.LastTravelBlocked = false
	p.LastTrain = nil
	p.LastErrand = ""
	if p.Disabled {
		return false, "provisioning is disabled (a harness control arm)"
	}
	if bankTile == (tile.Tile{}) {
		bankTile = p.DefaultBank
	}
	kitIDs := map[int]bool{}
	for _, l := range kit {
		kitIDs[l.ID] = true
	}
	keepFn := func(id int) bool {
		return kitIDs[id] || id == questplan.CoinsID || (keep != nil && keep(id))
	}
	exhausted := map[string]bool{}
	deadNow := false // some exhausted source is exhausted because its walk could not arrive
	for k, until := range p.deadSources {
		if time.Now().Before(until) {
			exhausted[k] = true
			deadNow = true
		} else {
			delete(p.deadSources, k)
		}
	}
	unavailable := p.unavailableSources(d, name, kit, exhausted)
	muleAsked := false
	bankedThisRound := false
	stalledBank := 0 // bank visits in a row that withdrew something and changed nothing in the pack
	// coinsDrawn is whether a bank visit of this round has already withdrawn coins: only then
	// is the shops' margin and float not drawn again (a first visit that only READS the bank
	// must not count, or the round leaves with the bare prices, as the sixth Waterfall EMPTY lab
	// run did: 120 coins for 120 gp of bread, then "You don't have enough coins").
	coinsDrawn := false
	lastSummary := ""

	for pass := 0; pass < provisionPasses; pass++ {
		if !WaitForGuardian(d) {
			return false, "stopped"
		}
		pack, worn := PackCounts(d), WornCounts(d)
		depositable := 0
		for _, it := range actions.NewEquipment(d.Session, d.Components).Inventory() {
			// What Deps.KeepInPack claims (the run staller's runes and jewellery) is not room: the deposit leaves
			// it, or banks it and AtBank draws it straight back, so the slots it holds are never the kit's.
			if it.ID > 0 && !keepFn(int(it.ID)) && !keptInPack(d, int(it.ID)) {
				depositable++
			}
		}
		st := questplan.ProvisionState{
			Pack: pack, Worn: worn,
			Bank: p.Bank.Counts, BankKnown: p.Bank.Known && bankedThisRound,
			MuleAsked: muleAsked, Levels: BaseLevels(d),
			FreeSlots: FreeSlots(d), Depositable: depositable, Exhausted: exhausted, Unavailable: unavailable,
			TravelFloat: p.TravelFloat, CoinsDrawn: coinsDrawn, Trainers: p.Trainers,
		}
		if st.TravelFloat == 0 {
			st.TravelFloat = DefaultTravelFloat
		}
		if p.Mule != nil {
			st.Mule, st.MuleKnown = p.Mule.Published()
		}
		plan := questplan.PlanProvision(kit, st)
		if s := plan.Summary(); s != lastSummary {
			d.logf("[provision %s] %s", name, s)
			lastSummary = s
		}

		switch plan.Phase {
		case questplan.PhaseSatisfied:
			return true, ""
		case questplan.PhaseBlocked:
			p.LastBlocked = true
			p.LastTravelBlocked = deadNow
			return false, strings.Join(plan.Blocked, "; ")
		case questplan.PhaseTrain:
			// Everything the account could fetch is fetched; what is left is a level. Not worked here (a stint inside the
			// quest's slot would hold the slot for hours): the need is handed to the engine, and the call fails like a
			// blocked one for any caller that does not read it.
			p.LastBlocked = true
			p.LastTrain = plan.Train
			return false, questplan.TrainReason(plan.Train)
		case questplan.PhaseBank:
			if bankedThisRound && len(plan.Withdraw) == 0 {
				// Read already; nothing to take. Should not happen, but never loop on it.
				p.LastBlocked = true
				return false, "the bank was read and holds nothing the plan can use"
			}
			packBefore := PackCounts(d)
			if !p.visitBank()(d, bankTile, plan.Withdraw, keepFn) {
				p.bankFails++
				if p.bankFails >= bankFailsBlocked {
					p.LastBlocked = true
					return false, fmt.Sprintf("could not use the bank at %d,%d,%d (%d times in a row)", bankTile.X, bankTile.Z, bankTile.Level, p.bankFails)
				}
				return false, fmt.Sprintf("could not use the bank at %d,%d,%d", bankTile.X, bankTile.Z, bankTile.Level)
			}
			p.bankFails = 0
			// A visit that was meant to take something out and left the pack as it was, again and again, is a
			// pack with no room: the fletching rung's "withdraw 3 Logs: the pack filled" ran all sixteen passes.
			if bankedThisRound && len(plan.Withdraw) > 0 && sameCounts(packBefore, PackCounts(d)) {
				stalledBank++
			} else {
				stalledBank = 0
			}
			bankedThisRound = true
			if stalledBank >= bankStalledBlocked {
				p.LastBlocked = true
				return false, fmt.Sprintf("the bank visits took nothing out %d times in a row (the pack has no room for %s)",
					stalledBank, describeWithdraw(plan.Withdraw))
			}
			for _, w := range plan.Withdraw {
				if w.ID == questplan.CoinsID {
					coinsDrawn = true
				}
			}
		case questplan.PhaseMule:
			if why, refused := p.controlRefuses(plan.Phase); refused {
				return false, why + ": " + plan.Summary()
			}
			muleAsked = true
			if p.Mule == nil {
				continue
			}
			purpose := p.Purpose
			if purpose == "" {
				purpose = "quest"
			}
			got, reason := p.Mule.Ask(purpose+":"+name, plan.MuleAsk)
			if len(got) == 0 && strings.Contains(reason, ErrandMarker) {
				p.LastErrand = reason
			}
			if len(got) == 0 {
				d.logf("[provision %s] the mule gave nothing (%s); falling through to ground, skill and shop", name, reason)
			} else {
				d.logf("[provision %s] the mule gave %v (%s)", name, got, reason)
			}
		case questplan.PhaseSource:
			if why, refused := p.controlRefuses(plan.Phase); refused {
				return false, why + ": " + plan.Summary()
			}
			task := nearestTask(plan.Tasks, CurrentTile(d))
			key := questplan.SourceKey(task.Line, task.Source)
			walksFailed := d.travelFails.Load()
			if !p.runSource(d, task) {
				exhausted[key] = true
				if d.travelFails.Load() > walksFailed {
					// The walk to the source did not arrive: not worth another walk for a while.
					if p.deadSources == nil {
						p.deadSources = map[string]time.Time{}
					}
					p.deadSources[key] = time.Now().Add(SourceDeadFor)
					deadNow = true
					d.logf("[provision %s] %s: the walk there did not arrive — skipping that source for %s", name, task.Describe(), SourceDeadFor)
				} else {
					d.logf("[provision %s] %s failed; trying the next source", name, task.Describe())
				}
			}
		}
	}
	p.LastBlocked = true
	p.LastTravelBlocked = deadNow
	return false, fmt.Sprintf("gave up after %d passes: %s", provisionPasses, lastSummary)
}

// unavailableSources marks every kit source whose executor SourceUnavailable rules out as
// exhausted for the round and returns each one's reason by SourceKey. It logs a source once.
func (p *Provisioner) unavailableSources(d *Deps, name string, kit []questplan.KitLine, exhausted map[string]bool) map[string]string {
	out := map[string]string{}
	if p.SourceUnavailable == nil {
		return out
	}
	said := map[string]bool{}
	for _, l := range kit {
		for _, src := range l.Sources {
			if src.Method == "" {
				continue
			}
			why := p.SourceUnavailable(src.Method)
			if why == "" {
				continue
			}
			key := questplan.SourceKey(l, src)
			exhausted[key] = true
			out[key] = why
			if !said[src.Method] {
				said[src.Method] = true
				d.logf("[provision %s] %s is not available to this account: %s", name, src.Label(), why)
			}
		}
	}
	return out
}

// controlRefuses reports whether a control arm's provisioner refuses a phase: SourcesDisabled
// lets the bank read, the withdrawals and a satisfied kit through, and refuses the mule and
// every source.
func (p *Provisioner) controlRefuses(phase questplan.ProvisionPhase) (string, bool) {
	if !p.SourcesDisabled {
		return "", false
	}
	switch phase {
	case questplan.PhaseMule, questplan.PhaseSource:
		return "the mule and every source are disabled (a harness control arm)", true
	}
	return "", false
}

// bankVisit opens the bank, reads it, deposits what is neither kit nor kept, withdraws the
// plan's lines and closes the bank again.
func (p *Provisioner) bankVisit(d *Deps, bankTile tile.Tile, withdraw []questplan.ItemCount, keep func(id int) bool) bool {
	bank, ok := OpenBankAt(d, bankTile)
	if !ok {
		return false
	}
	DepositExcept(d, bank, keep)
	p.Bank.Note(BankCounts(bank))
	draws := make([]Draw, 0, len(withdraw))
	for _, w := range withdraw {
		draws = append(draws, Draw{ID: w.ID, N: w.Count})
	}
	WithdrawExactMany(d, bank, draws) // every line in one burst, confirmed once
	p.Bank.Note(BankCounts(bank))
	_ = bank.Close()
	d.Delay(600 * time.Millisecond)
	return true
}

// runSource works one ground, skill or shop task.
func (p *Provisioner) runSource(d *Deps, t questplan.SourceTask) bool {
	switch t.Source.Kind {
	case questplan.SourceShop:
		return BuyFromShopAt(d, t.Source.Shop, t.Line.ID, t.Qty, t.Source.Price) > 0
	case questplan.SourceGround:
		return GatherGround(d, t.Line.ID, t.Qty, t.Source)
	case questplan.SourceSkill, questplan.SourceMarket:
		m := p.Methods[t.Source.Method]
		if m == nil {
			d.logf("no executor registered for the %s source %q", t.Source.Kind, t.Source.Method)
			return false
		}
		return m(d, t)
	}
	return false
}

// GatherGround stands at a ground spawn and picks it up until qty are held or the spawn stops
// coming back within its respawn time.
func GatherGround(d *Deps, id, qty int, src questplan.Source) bool {
	// Onto the spawn itself where it can be stood on: a radius can end on the far side of a wall
	// (merlin-empty-2 stopped two tiles from the repellent, outside the house it lies in).
	if !TravelCautious(d, src.Tile, 0) && !TravelCautious(d, src.Tile, 2) {
		return false
	}
	start := HeldID(d, id)
	respawn := time.Duration(src.RespawnTicks) * 600 * time.Millisecond
	if respawn < 30*time.Second {
		respawn = 30 * time.Second
	}
	if respawn > 3*time.Minute {
		respawn = 3 * time.Minute
	}
	for HeldID(d, id)-start < qty {
		if FreeSlots(d) == 0 {
			break
		}
		if !waitQuietly(d, func() bool { return len(GroundItemsNear(d, id, src.Tile, 1)) > 0 }, respawn) {
			break
		}
		if !TakeGround(d, id, src.Tile, 1) {
			break
		}
	}
	return HeldID(d, id) > start
}

// FareReserve is the coins BuyFromShop never spends: three ship fares home. A shop's price rises
// as its shared stock falls, and questproof lab runs (arena-empty-3 and -4, merlin-empty-6) spent
// the whole kit draw at Betty's in Port Sarim and were stranded there with no fare back.
const FareReserve = 100

// CheapSpend is how many coins one shop visit may spend out of the fare reserve. The reserve is
// for the ship fares home, and it kept an account from buying a 1 gp Hammer: live 2 Oct, acct158
// held 97 coins ("keeping 97 coins for the fares back: Hammer costs 0 each here now", then 51, 37,
// 46 and 14 as it spent on other things), eight Vampire Slayer parks. An account that cannot pay a
// fare anyway loses nothing by a coin, and a quest item is worth a few: ten coins in all, per
// visit, are never held back by the reserve.
const CheapSpend = 10

// ShopChunk is how many of an item to buy next. coins are held now, unit is the shelf price
// learned from the first purchase (0 before it), hint the plan's own base price for the item (0
// when it has none), left how many are still wanted and spent what this visit has already spent.
//
// Above the reserve the rule is the old one: the first purchase is one item to learn the price,
// then chunks of at most ten that leave FareReserve coins. Below it, only what CheapSpend still
// covers is bought: a first purchase when the plan's base price is known and within it, and
// later chunks up to it. 0 means stop.
func ShopChunk(coins, unit, hint, left, spent int) int {
	if left <= 0 || coins <= 0 {
		return 0
	}
	n := left
	if n > 10 {
		n = 10
	}
	spendable := coins - FareReserve // above the reserve
	cheap := CheapSpend - spent      // out of the reserve
	if cheap < 0 {
		cheap = 0
	}
	if cheap > coins {
		cheap = coins
	}
	if unit <= 0 {
		// The first purchase learns the shelf price.
		switch {
		case hint > 0 && hint <= spendable:
			return 1
		case hint > 0 && hint <= cheap:
			return 1
		case hint <= 0 && spendable > 0:
			return 1
		}
		return 0
	}
	room := 0
	if spendable > 0 {
		room = spendable / unit
	}
	if c := cheap / unit; c > room {
		room = c
	}
	if room < n {
		n = room
	}
	if n < 0 {
		return 0
	}
	return n
}

// BuyFromShop travels to a keeper and buys up to qty of id, and reports how many arrived. It
// buys one first to learn the shelf price, then in chunks that leave FareReserve coins held.
func BuyFromShop(d *Deps, shop questplan.ShopRef, id, qty int) int {
	return BuyFromShopAt(d, shop, id, qty, 0)
}

// BuyFromShopAt is BuyFromShop with the plan's base price for the item (0 when unknown), which
// the first purchase is checked against before any coin is spent (ShopChunk).
func BuyFromShopAt(d *Deps, shop questplan.ShopRef, id, qty, hint int) int {
	if !TravelCautious(d, shop.Anchor, 4) {
		return 0
	}
	name := shop.Npc
	if name == "" && d.Config != nil {
		if n, ok := d.Config.Npc(shop.NpcID); ok {
			name = n.Name
		}
	}
	if err := entityquery.OpenShop(d.Session, d.Config, name, waiterFor(d)); err != nil {
		keeper, ok := FindNpcByID(d, shop.NpcID, name, 16)
		if !ok {
			d.logf("opening %s's shop: %v", name, err)
			return 0
		}
		WalkNear(d, tile.Tile{X: keeper.X, Z: keeper.Z, Level: keeper.Level}, 1)
		if err := entityquery.OpenShop(d.Session, d.Config, name, waiterFor(d)); err != nil {
			d.logf("opening %s's shop after walking to the keeper: %v", name, err)
			return 0
		}
	}
	sh := actions.NewShop(d.Session)
	defer func() { _ = sh.Close() }()
	stocked := false
	for _, it := range sh.Stock() {
		if int(it.ID) == id && it.Count > 0 {
			stocked = true
		}
	}
	if !stocked {
		d.logf("%s has no %s on the shelf", name, ObjName(d, id))
		return 0
	}
	before := HeldID(d, id)
	mark := time.Now()
	unit := 0     // coins per item, learned from the first purchase
	spentAll := 0 // coins this visit has spent so far
	for left := qty; left > 0; {
		n := left
		if n > 10 {
			n = 10
		}
		coins := HeldID(d, questplan.CoinsID)
		if id != questplan.CoinsID {
			// The first purchase learns the shelf price; later ones leave the fare reserve,
			// bar the few coins CheapSpend lets through.
			if n = ShopChunk(coins, unit, hint, left, spentAll); n <= 0 {
				d.logf("keeping %d coins for the fares back: %s costs %d each here now (the plan's base price is %d)", coins, ObjName(d, id), unit, hint)
				break
			}
		}
		got0 := HeldID(d, id)
		if err := sh.Buy(int32(id), int32(n)); err != nil {
			d.logf("buying %s: %v", ObjName(d, id), err)
			break
		}
		if !waitQuietly(d, func() bool { return HeldID(d, id) > got0 }, 5*time.Second) {
			if d.Chat != nil {
				d.logf("the purchase did not arrive: %q", d.Chat.Since(mark))
			}
			break
		}
		if bought := HeldID(d, id) - got0; bought > 0 && id != questplan.CoinsID {
			spent := coins - HeldID(d, questplan.CoinsID)
			if spent > 0 {
				spentAll += spent
			}
			if spent > unit*bought {
				unit = (spent + bought - 1) / bought
			} else if unit == 0 {
				unit = 1
			}
		}
		left = qty - (HeldID(d, id) - before)
	}
	got := HeldID(d, id) - before
	d.logf("bought %d %s from %s", got, ObjName(d, id), name)
	return got
}

// nearestTask is the source task whose place is closest to here. The plan lists one task per
// short line in kit order, and working them in that order sent the second Waterfall lab run from
// Ardougne to Betty in Port Sarim and back past Aemad, whose rope was round the corner.
func nearestTask(tasks []questplan.SourceTask, here tile.Tile) questplan.SourceTask {
	best, bestD := tasks[0], -1
	for _, t := range tasks {
		at := t.Source.Tile
		if t.Source.Kind == questplan.SourceShop {
			at = t.Source.Shop.Anchor
		}
		if at == (tile.Tile{}) {
			continue
		}
		dist := tile.Chebyshev(here, at)
		if bestD < 0 || dist < bestD {
			best, bestD = t, dist
		}
	}
	return best
}
