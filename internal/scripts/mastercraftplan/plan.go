// Package mastercraftplan is the pure half of MasterCrafter, the script that takes one account (the
// owner's main, "main", who wants every skill at 99) off ScarabSwarm now and then to cut the gems
// and string the bows the fleet has piled up in the mule's bank, and hands the products back.
//
// It imports no client, actions, fleet or bot code and does no I/O. The decisions it owns:
//
//   - WHEN to leave ScarabSwarm (Decide): the mule's published bank holds at least a threshold of
//     uncut gems this account can cut, or of unstrung bows with the Bow strings to string them;
//     checked every CheckEvery (CheckDue), and only acted on at a SafePoint.
//   - WHAT to ask the mule for (PlanGemAsk): every cuttable kind of uncut gem it holds, up to
//     KitSize of a kind, and a chisel when none is held. The bows are asked for by BowStringer's own
//     planner (bowstringplan.PlanOrder) through the Ledger's estimate of what is really unstrung.
//   - WHICH gem to cut (GemsBestFirst, PickGem): the best one the Crafting level allows first. The
//     levels are content 289's (scripts/skill_crafting/configs/gem/gem.dbrow), which gemplan
//     carries: opal 1, jade 13, red topaz 16, sapphire 20, emerald 27, ruby 34, diamond 43,
//     dragonstone 55.
//   - HOW MANY AT ONCE (gemplan.BurstSize): five a game tick, the engine's limit on user events
//     (ClientGameProtCategory.USER_EVENT). Cutting has no delay in content
//     (skill_crafting/scripts/gem/uncut_gem.rs2: inv_del, inv_add, stat_advance, no p_delay), so five
//     distinct gem slots used on the chisel in one tick are five gems cut by the next.
//
// STRUNG AND UNSTRUNG BOWS SHARE A NAME, so the mule's published bank, which counts by name, cannot
// tell them apart: the merged row of "Yew shortbow" counted the 130,712 STRUNG ones the mule held as
// bows to string (live 6 Oct 2026). peerrunner publishes every bow once per obj as well, under its
// qualified name ("Yew shortbow id68"), and the unstrung bows are counted from that row, exactly
// (bowstringplan.MuleUnstrung). Only when a bank carries no per-obj row of a bow is the merged count
// used, and then this account's own handing of strung bows to the mule is taken off it: the Ledger
// remembers how many strung bows of each name it gave the mule and UnstrungEstimate takes them off.
package mastercraftplan

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/pearlplan"
)

// Mode is what the account does now.
type Mode string

// The modes.
const (
	// ModeSwarm runs ScarabSwarm.
	ModeSwarm Mode = "swarm"
	// ModeGems is a gem round: ask the mule for its uncut gems, cut them, hand the product back.
	ModeGems Mode = "gems"
	// ModeBows is a bow round: string the mule's bows with its Bow strings, hand them back.
	ModeBows Mode = "bows"
	// ModePearls is a pearl round: ask the mule for every oyster pearl and Bolts it holds, cut the
	// pearls into Pearl bolttips, tip the Bolts, hand the Pearl bolts back (pearlplan).
	ModePearls Mode = "pearls"
	// ModeDeliver hands products a round left in the bank to the mule.
	ModeDeliver Mode = "deliver"
	// ModeShop is the shop trip: coins from the mule, Bolts (and Steel arrowtips) bought out of the
	// supply shops under a price cap, banked at the mule's post (supply.go).
	ModeShop Mode = "shop"
	// ModeSuperheat superheats the mule's ores into bars, highest bar first (superheat.go).
	ModeSuperheat Mode = "superheat"
	// ModeArrows makes headless arrows, then steel arrows, from the mule's parts (arrows.go).
	ModeArrows Mode = "arrows"
	// ModeHerbs cleans the mule's grimy herbs (herbs.go).
	ModeHerbs Mode = "herbs"
	// ModeSmith smiths the Steel and better bars into arrowtips and the Iron bars into Iron platebodies at the
	// Varrock west anvil (smith.go).
	ModeSmith Mode = "smith"
	// ModeCannonballs smelts Steel bars into cannonballs at the Falador furnace and hands the balls to the mule (cannonballs.go).
	ModeCannonballs Mode = "cannonballs"
	// ModePlatebody smiths the best bar's platebodies at Varrock west for an hour (platebody.go).
	ModePlatebody Mode = "platebody"
	// ModeStaves attaches Fire orbs to Battlestaves at the mule's post (staves.go).
	ModeStaves Mode = "staves"
)

// Stock is the mule's published bank count for an item by name; ok false when it has not
// published (or the row is stale). nil means no published bank at all.
type Stock func(item string) (count int, ok bool)

func stockOf(s Stock, item string) int {
	if s == nil {
		return 0
	}
	n, ok := s(item)
	if !ok || n < 0 {
		return 0
	}
	return n
}

// Line is one line of a kit order to the mule.
type Line struct {
	Item string
	Qty  int
}

// ---- gems ----

// GemsBestFirst is the gems this account may cut, the highest level (so the most XP) first. kinds
// restricts them when it is non-empty, denied removes gems a refusal took off the table, and
// members false drops the members-only ones (gemplan.Eligible says why).
func GemsBestFirst(level int, kinds []string, denied map[string]bool, members bool) []gemplan.Gem {
	gems := gemplan.Eligible(level, kinds, denied, members)
	sort.SliceStable(gems, func(i, j int) bool { return gems[i].Level > gems[j].Level })
	return gems
}

// GemStock counts the uncut gems of the kinds in gems that the mule's published bank holds.
func GemStock(gems []gemplan.Gem, stock Stock) (total int, perKind map[string]int) {
	perKind = map[string]int{}
	for _, g := range gems {
		if n := stockOf(stock, g.UncutName); n > 0 {
			perKind[g.Key] = n
			total += n
		}
	}
	return total, perKind
}

// GemAsk is the kit to ask the mule for in a gem round.
type GemAsk struct {
	Lines []Line
	// Gems is how many uncut gems the lines ask for.
	Gems int
	// Why is the reason Lines is empty, "" otherwise.
	Why string
	// Bulk is whether the ask is for the mule's whole stock (Settings.BulkAtSapphires).
	Bulk bool
}

// PlanGemAsk is the kit for a gem round: one line per cuttable kind the mule holds, best gem
// first, each up to set.KitSize (the mule serves at most 1000 of an uncatalogued item an order),
// and a chisel when haveChisel is false. It asks for nothing the mule does not publish.
func PlanGemAsk(level int, set Settings, denied map[string]bool, stock Stock, haveChisel bool) GemAsk {
	var a GemAsk
	if stock == nil {
		a.Why = "the mule has not published its bank"
		return a
	}
	gems := GemsBestFirst(level, set.GemKinds, denied, set.MembersWorld)
	if len(gems) == 0 {
		a.Why = "Crafting " + strconv.Itoa(level) + " allows none of the selected gems"
		return a
	}
	bulk := set.BulkAtSapphires > 0 && stockOf(stock, "Uncut sapphire") >= set.BulkAtSapphires
	a.Bulk = bulk
	for _, g := range gems {
		n := stockOf(stock, g.UncutName)
		if n <= 0 {
			continue
		}
		q := min(n, set.KitSize)
		if bulk {
			q = n
		}
		a.Lines = append(a.Lines, Line{Item: g.UncutName, Qty: q})
		a.Gems += q
	}
	if a.Gems == 0 {
		a.Lines = nil
		a.Why = "the mule's published bank holds none of the gems Crafting " + strconv.Itoa(level) + " can cut"
		return a
	}
	if !haveChisel {
		if stockOf(stock, gemplan.Chisel) <= 0 {
			a.Lines = nil
			a.Gems = 0
			a.Why = "no chisel in the pack or the bank, and the mule's published bank holds none"
			return a
		}
		a.Lines = append(a.Lines, Line{Item: gemplan.Chisel, Qty: 1})
	}
	return a
}

// PearlAsk is the kit for a pearl round.
type PearlAsk struct {
	Lines []Line
	// Pearls and Bolts are what the lines ask for.
	Pearls, Bolts int
	// Why is the reason Lines is empty, "" otherwise.
	Why string
}

// PlanPearlAsk is the kit for a pearl round (pearlplan.PlanAsk): every oyster pearl of both kinds
// the mule publishes and every Bolts, and a chisel when none is held and there are pearls to cut.
// own is this account's bank: Bolts are still asked for when the mule has no pearls left but this
// account holds tips.
func PlanPearlAsk(stock Stock, own pearlplan.Held, haveChisel bool) PearlAsk {
	var a PearlAsk
	if stock == nil {
		a.Why = "the mule has not published its bank"
		return a
	}
	mule := pearlplan.HeldIn(pearlplan.Stock(stock))
	p := pearlplan.PlanAsk(mule)
	for _, l := range p.Lines {
		a.Lines = append(a.Lines, Line{Item: l.Item, Qty: l.Qty})
	}
	a.Pearls, a.Bolts = p.Pearls, p.Bolts
	if len(a.Lines) == 0 {
		a.Why = "the mule's published bank holds no oyster pearls and no Bolts"
		return a
	}
	if !haveChisel && a.Pearls+own.PearlCount() > 0 {
		if stockOf(stock, gemplan.Chisel) <= 0 {
			a.Lines, a.Pearls, a.Bolts = nil, 0, 0
			a.Why = "no chisel in the pack or the bank, and the mule's published bank holds none"
			return a
		}
		a.Lines = append(a.Lines, Line{Item: gemplan.Chisel, Qty: 1})
	}
	return a
}

// PickGem is the first gem of gems (best first) the bank still holds uncut, by gem key.
func PickGem(gems []gemplan.Gem, bankUncut map[string]int) (gemplan.Gem, bool) {
	for _, g := range gems {
		if bankUncut[g.Key] > 0 {
			return g, true
		}
	}
	return gemplan.Gem{}, false
}

// GemLoad is how many uncut gems one bank visit draws: the pack less the chisel's slot.
func GemLoad() int { return gemplan.PackSlots - 1 }

// Burst is how many gems the next burst of chisel uses cuts: all that remain, at most
// gemplan.BurstSize (five a game tick).
func Burst(remaining int) int {
	if remaining < 0 {
		return 0
	}
	return min(remaining, gemplan.BurstSize)
}

// Bursts splits n gems into the bursts that cut them, five a tick, the last one the remainder.
func Bursts(n int) []int {
	var out []int
	for n > 0 {
		b := Burst(n)
		out = append(out, b)
		n -= b
	}
	return out
}

// ---- bows ----

// Ledger is what this script remembers across rounds (and restarts, in the side store).
type Ledger struct {
	// StrungAtMule is how many strung bows of each name (lower case) this account has handed the
	// mule that the mule's published count may still include: see the package comment.
	StrungAtMule map[string]int `json:"strungAtMule,omitempty"`
	// PendingDeliver is set when a round ended with product still in this account's bank, and
	// cleared once it has been handed over.
	PendingDeliver bool `json:"pendingDeliver,omitempty"`
	// PendingCut is set when a gem round ended with cuttable uncut gems still in this account's
	// bank (the round ran out of time): the next gem round starts from them.
	PendingCut bool `json:"pendingCut,omitempty"`
	// GemPauseUntil and BowPauseUntil hold a kind of round off after one that produced nothing
	// for a reason that will not pass by itself.
	GemPauseUntil   time.Time `json:"gemPauseUntil,omitempty"`
	BowPauseUntil   time.Time `json:"bowPauseUntil,omitempty"`
	PearlPauseUntil time.Time `json:"pearlPauseUntil,omitempty"`
	// ShopPauseUntil, SuperheatPauseUntil, ArrowPauseUntil and HerbPauseUntil are the same for the
	// four later rounds, StorePauseUntil for the storage round.
	ShopPauseUntil      time.Time `json:"shopPauseUntil,omitempty"`
	SuperheatPauseUntil time.Time `json:"superheatPauseUntil,omitempty"`
	ArrowPauseUntil     time.Time `json:"arrowPauseUntil,omitempty"`
	HerbPauseUntil      time.Time `json:"herbPauseUntil,omitempty"`
	StorePauseUntil     time.Time `json:"storePauseUntil,omitempty"`
	// SmithPauseUntil holds the anvil round off after one that smithed nothing. SmithActive is set when an
	// anvil round STARTED (the Steel threshold was passed) and cleared only when this account's bank holds no
	// bar it can smith: a round cut short (the time budget, a restart, a crash) is resumed by it, with no
	// threshold, so the whole stock is smithed (a user request).
	SmithPauseUntil time.Time `json:"smithPauseUntil,omitempty"`
	SmithActive     bool      `json:"smithActive,omitempty"`
	// Stored is what the storage round has moved into this account's bank, by lower-case item name
	// with the units: ScarabSwarm's loot hand-off, which runs on this account between rounds, keeps
	// every one of these names (they would otherwise go straight back to the mule), and a later ask
	// for one of them goes to this account. StoreRefused is the items the mule refused for a reason
	// that belongs to the item, with when each may be tried again.
	Stored       map[string]int       `json:"stored,omitempty"`
	StoreRefused map[string]time.Time `json:"storeRefused,omitempty"`
	// Shelves is what the shop trips last saw on each supply shop's shelf (ShelfKey).
	Shelves map[string]ShopShelf `json:"shelves,omitempty"`
	// Backoffs hold a mode off after rounds that failed for a PASSING reason (the walk did not
	// arrive, the mule was away, queued or deferred the kit, a bank would not open), keyed by Mode:
	// the wait doubles for each failure in a row (BackoffFor) and a round that works clears it. The
	// ModeDeliver entry counts failed hand-overs to the mule, whichever round made them.
	Backoffs map[string]Backoff `json:"backoffs,omitempty"`
	// Chest is the crystal chest round's own memory (chest.go).
	Chest ChestLedger `json:"chest,omitempty"`
	// Jewellery is the jewellery round's own memory (jewellery.go): the batch under way and the Games
	// necklaces owed to the mule.
	Jewellery JewelleryLedger `json:"jewellery,omitempty"`
	// Rune is the Runecrafting round's own memory (runecraft.go): the bank as the last visit saw it, its counters
	// and the pause after a round that found nothing.
	Rune RuneLedger `json:"rune,omitempty"`
	// Trawler is the trawler round's memory (trawler.go).
	Trawler TrawlerLedger `json:"trawler,omitempty"`
	// Cannon is the cannonball round's memory (cannonballs.go).
	Cannon CannonLedger `json:"cannon,omitempty"`
	// CannonRun is the cannon run round's memory (cannonrun.go).
	CannonRun CannonRunLedger `json:"cannonRun,omitempty"`
	// Plate is the platebody round's memory (platebody.go).
	Plate PlateLedger `json:"plate,omitempty"`
	// Staves is the Fire battlestaff round's memory (staves.go).
	Staves StavesLedger `json:"staves,omitempty"`
	// Bones is the bone-burying round's memory (bones.go).
	Bones BonesLedger `json:"bones,omitempty"`
	// Sched is the round schedule's clock-based memory (schedule.go).
	Sched ScheduleLedger `json:"sched,omitempty"`
	// Lifetime counters, for the log.
	GemRounds  int `json:"gemRounds,omitempty"`
	BowRounds  int `json:"bowRounds,omitempty"`
	GemsCut    int `json:"gemsCut,omitempty"`
	BowsStrung int `json:"bowsStrung,omitempty"`
	GemsHanded int `json:"gemsHanded,omitempty"`
	BowsHanded int `json:"bowsHanded,omitempty"`
	// PearlRounds, PearlsCut (pearls chiselled into tips), PearlBolts (Pearl bolts made) and
	// PearlBoltsHanded.
	PearlRounds      int `json:"pearlRounds,omitempty"`
	PearlsCut        int `json:"pearlsCut,omitempty"`
	PearlBolts       int `json:"pearlBolts,omitempty"`
	PearlBoltsHanded int `json:"pearlBoltsHanded,omitempty"`
	// The later rounds' counters: shop trips, units bought and coins spent; superheat rounds and
	// bars; arrow rounds, headless and steel arrows made; herb rounds and herbs cleaned.
	ShopRounds      int `json:"shopRounds,omitempty"`
	ShopBought      int `json:"shopBought,omitempty"`
	ShopSpent       int `json:"shopSpent,omitempty"`
	SuperheatRounds int `json:"superheatRounds,omitempty"`
	BarsMade        int `json:"barsMade,omitempty"`
	ArrowRounds     int `json:"arrowRounds,omitempty"`
	HeadlessMade    int `json:"headlessMade,omitempty"`
	ArrowsMade      int `json:"arrowsMade,omitempty"`
	HerbRounds      int `json:"herbRounds,omitempty"`
	HerbsCleaned    int `json:"herbsCleaned,omitempty"`
	// Anvil rounds: rounds, bars smithed, arrowtips and Iron platebodies made.
	SmithRounds     int `json:"smithRounds,omitempty"`
	SmithBars       int `json:"smithBars,omitempty"`
	ArrowtipsMade   int `json:"arrowtipsMade,omitempty"`
	PlatebodiesMade int `json:"platebodiesMade,omitempty"`
	// Storage rounds, the rows (item types) and the items they moved into this account's bank.
	StoreRounds int `json:"storeRounds,omitempty"`
	RowsStored  int `json:"rowsStored,omitempty"`
	ItemsStored int `json:"itemsStored,omitempty"`
}

// Backoff is one mode's wait after failed rounds.
type Backoff struct {
	// Fails is how many rounds in a row failed; Until is when the next may start.
	Fails int       `json:"fails,omitempty"`
	Until time.Time `json:"until,omitempty"`
}

// BackoffFor is the wait after the fails-th failure in a row: set.Backoff for the first, doubling
// for each further one, at most set.BackoffMax.
func BackoffFor(fails int, set Settings) time.Duration {
	if fails < 1 {
		return 0
	}
	d := set.Backoff
	for i := 1; i < fails && d < set.BackoffMax; i++ {
		d *= 2
	}
	if d > set.BackoffMax {
		d = set.BackoffMax
	}
	return d
}

// WaitFor is the wait after the fails-th failure in a row of mode at now: BackoffFor, except for the Nature gate with a good history,
// which waits GateBackoffFor (nature.go). short says it was the short one.
func (l *Ledger) WaitFor(mode Mode, fails int, now time.Time, set Settings) (wait time.Duration, short bool) {
	if mode == ModeNature && l != nil && l.Rune.Gate.GoodHistory(now) {
		return GateBackoffFor(fails, set), true
	}
	// A trawler round's passing failure (a walk that stopped short, a bank that did not open) is a few minutes, not half an hour of
	// the two-hour block (live 3 Nov 2026 17:54: one bank walk backed the block off until 18:24, and master strung bows).
	if mode == ModeTrawler {
		return min(time.Duration(fails)*TrawlerBackoffStep, TrawlerBackoffMax), true
	}
	return BackoffFor(fails, set), false
}

// Fail records a round of mode that failed for a passing reason at now, and returns when the next
// may start.
func (l *Ledger) Fail(mode Mode, now time.Time, set Settings) time.Time {
	if l.Backoffs == nil {
		l.Backoffs = map[string]Backoff{}
	}
	b := l.Backoffs[string(mode)]
	b.Fails++
	wait, _ := l.WaitFor(mode, b.Fails, now, set)
	b.Until = now.Add(wait)
	l.Backoffs[string(mode)] = b
	return b.Until
}

// Succeed clears mode's backoff: a round of it worked.
func (l *Ledger) Succeed(mode Mode) {
	delete(l.Backoffs, string(mode))
}

// Fails is how many rounds of mode in a row have failed.
func (l *Ledger) Fails(mode Mode) int {
	if l == nil {
		return 0
	}
	return l.Backoffs[string(mode)].Fails
}

// BackedOff reports whether mode is waiting out a backoff at now, and until when.
func (l *Ledger) BackedOff(mode Mode, now time.Time) (until time.Time, backedOff bool) {
	if l == nil {
		return time.Time{}, false
	}
	until = l.Backoffs[string(mode)].Until
	return until, now.Before(until)
}

// HandoverGaveUp reports whether set.MaxHandoverFailures hand-overs to the mule in a row have
// failed: every round is then suspended (the products stay in the bank) until the script restarts.
func (l *Ledger) HandoverGaveUp(set Settings) bool {
	return l != nil && set.MaxHandoverFailures > 0 && l.Fails(ModeDeliver) >= set.MaxHandoverFailures
}

// ClearGiveUp is what a restart does to a suspension: the count of failed hand-overs starts again
// (the backoff's own wait is kept). It reports whether there was a suspension to clear.
func (l *Ledger) ClearGiveUp(set Settings) bool {
	if !l.HandoverGaveUp(set) {
		return false
	}
	b := l.Backoffs[string(ModeDeliver)]
	b.Fails = 0
	l.Backoffs[string(ModeDeliver)] = b
	return true
}

// NoteStrungDelivered records n strung bows of name given to the mule.
func (l *Ledger) NoteStrungDelivered(name string, n int) {
	if n <= 0 {
		return
	}
	if l.StrungAtMule == nil {
		l.StrungAtMule = map[string]int{}
	}
	l.StrungAtMule[bowstringplan.Key(name)] += n
}

// Reconcile caps each remembered count at what the mule publishes: strung bows somebody took away
// (an alcher, another stringer) are no longer there to be mistaken for unstrung ones. An unknown
// published count (nil stock) changes nothing.
func (l *Ledger) Reconcile(stock Stock) {
	if stock == nil {
		return
	}
	for name, n := range l.StrungAtMule {
		pub, ok := stock(canonicalBow(name))
		if !ok {
			continue
		}
		if pub < n {
			n = max(pub, 0)
		}
		if n <= 0 {
			delete(l.StrungAtMule, name)
			continue
		}
		l.StrungAtMule[name] = n
	}
}

// canonicalBow is the game's own spelling of a bow named by its ledger key (lower case), the form a
// published bank is asked in; an unknown name is returned as it is.
func canonicalBow(key string) string {
	for _, b := range bowstringplan.Bows {
		if bowstringplan.Key(b.Name) == key {
			return b.Name
		}
	}
	return key
}

// Strung is the remembered strung count for name.
func (l *Ledger) Strung(name string) int {
	if l == nil {
		return 0
	}
	return l.StrungAtMule[bowstringplan.Key(name)]
}

// UnstrungEstimate is how many of a bow the mule probably holds unstrung: its published count by
// name less the strung ones this account handed it. It is only the FALLBACK for a published bank that
// carries no per-obj row of the bow (unstrungAt); the per-obj row is exact.
func UnstrungEstimate(published, strungAtMule int) int {
	return max(0, published-max(0, strungAtMule))
}

// unstrungAt is how many UNSTRUNG bows of name the mule holds: exact from the published per-obj row
// when ids resolves the bow (bowstringplan.MuleUnstrung), else the estimate from the merged row.
func unstrungAt(stock Stock, led *Ledger, ids bowstringplan.BowIDs, name string) int {
	if ids != nil {
		if un, st, ok := ids(name); ok {
			n, _ := bowstringplan.MuleUnstrung(stock, name, un, st, led.Strung(name))
			return n
		}
	}
	return UnstrungEstimate(stockOf(stock, name), led.Strung(name))
}

// AdjustedStock is stock as a planner that wants UNSTRUNG bows should read it: a bow's plain name, and
// the qualified name of its unstrung obj, answer with the unstrung count (exact from the per-obj row,
// else the merged count less the ledger's strung bows), so "how many Maple longbows does the mule hold"
// gets the unstrung ones. Every other name is stock's own answer. ids may be nil (the estimate only).
func AdjustedStock(stock Stock, led *Ledger, ids bowstringplan.BowIDs) Stock {
	if stock == nil {
		return nil
	}
	return func(item string) (int, bool) {
		n, ok := stock(item)
		if base, id, qualified := cachecfg.SplitQualified(item); qualified {
			if _, isBow := bowstringplan.LevelFor(base); isBow && ids != nil {
				if un, _, found := ids(base); found && un == id {
					return unstrungAt(stock, led, ids, base), true
				}
			}
			return n, ok
		}
		if !ok {
			return n, ok
		}
		if _, isBow := bowstringplan.LevelFor(item); isBow {
			return unstrungAt(stock, led, ids, item), true
		}
		return n, ok
	}
}

// BowReadiness is whether a bow round is worth starting.
type BowReadiness struct {
	// Pairs is how many bows can be strung: the unstrung ones of every allowed bow, at most the
	// Bow strings the mule holds.
	Pairs int
	// Bows and Strings are the mule's stringable unstrung bows and its Bow strings.
	Bows, Strings int
	// Best is the highest-level bow with unstrung stock; ok false when there is none.
	Best  bowstringplan.Bow
	Found bool
	Ready bool
	// Resume is set when this account's own bank funds the round (OwnBows), whatever the mule holds.
	Resume bool
	Why    string
}

// OwnBows is the bow round's items in this account's own bank when last seen: unstrung bows by bow
// name and Bow strings.
type OwnBows struct {
	Unstrung map[string]int
	Strings  int
}

// BowLoad is the bow-and-string pairs one load strings: the bank funding this many is a round.
const BowLoad = 14

// BowsReadyWithOwn is BowsReady with this account's own bank counted too: bows and strings an
// earlier round drew and did not string make a round on their own (Resume) once they reach
// set.BowThreshold pairs, and with the mule's together otherwise (a user request: "20k unstrung
// bows and 20k bow string to start that"). Live 5 Oct 2026 the main account held 32,829 unstrung Yew
// shortbows and 55,418 strings while the mule held no strings, so BowsReady said "no Bow string"
// and a pearl round ran instead (owner: "i dont think the main account considered the bows and bow string
// already in the bank").
func BowsReadyWithOwn(set Settings, fletching int, stock Stock, led *Ledger, own OwnBows, ids bowstringplan.BowIDs) BowReadiness {
	r := BowsReadyFor(set, fletching, stock, led, ids)
	ownBows := 0
	var best bowstringplan.Bow
	found := false
	seen := map[string]bool{}
	for _, name := range set.BowNames {
		key := bowstringplan.Key(name)
		lvl, _ := bowstringplan.LevelFor(name)
		if !bowstringplan.Stringable(name, fletching) || seen[key] {
			continue
		}
		seen[key] = true
		n := own.Unstrung[name]
		if n <= 0 {
			continue
		}
		ownBows += n
		if !found || lvl > best.Level {
			best, found = bowstringplan.Bow{Name: name, Level: lvl}, true
		}
	}
	ownPairs := min(ownBows, own.Strings)
	pairs := min(r.Bows+ownBows, r.Strings+own.Strings)
	switch {
	case r.Ready:
		return r
	case ownPairs >= set.BowThreshold && ownPairs >= BowLoad:
		r.Resume, r.Pairs = true, ownPairs
		r.Why = "this account's bank holds " + strconv.Itoa(ownPairs) + " bow-and-string pairs Fletching " + strconv.Itoa(fletching) + " can string (threshold " + strconv.Itoa(set.BowThreshold) + ")"
	case stock != nil && ownBows+own.Strings > 0 && pairs >= set.BowThreshold:
		r.Pairs = pairs
		r.Why = "the mule's bank and this account's hold " + strconv.Itoa(pairs) + " bow-and-string pairs Fletching " + strconv.Itoa(fletching) + " can string together (threshold " + strconv.Itoa(set.BowThreshold) + ")"
	default:
		if stock != nil && ownBows+own.Strings > 0 {
			r.Why = "the mule's bank and this account's hold " + strconv.Itoa(pairs) + " bow-and-string pair(s) together, under the threshold " + strconv.Itoa(set.BowThreshold)
		}
		return r
	}
	r.Ready = true
	if !r.Found || (found && best.Level > r.Best.Level) {
		r.Best, r.Found = best, found
	}
	return r
}

// BowsReady is BowsReadyFor with no bow ids: the unstrung bows estimated from the merged rows.
func BowsReady(set Settings, fletching int, stock Stock, led *Ledger) BowReadiness {
	return BowsReadyFor(set, fletching, stock, led, nil)
}

// BowsReadyFor reads the mule's published bank for stringable bows: the bows in set.BowNames that
// Fletching allows, each at its UNSTRUNG count (exact from the per-obj row when ids resolves the bow,
// else the merged count less the Ledger's strung bows), against the Bow strings.
func BowsReadyFor(set Settings, fletching int, stock Stock, led *Ledger, ids bowstringplan.BowIDs) BowReadiness {
	var r BowReadiness
	if stock == nil {
		r.Why = "the mule has not published its bank"
		return r
	}
	seen := map[string]bool{}
	bows := 0
	for _, name := range set.BowNames {
		key := bowstringplan.Key(name)
		lvl, _ := bowstringplan.LevelFor(name)
		if !bowstringplan.Stringable(name, fletching) || seen[key] {
			continue
		}
		seen[key] = true
		n := unstrungAt(stock, led, ids, name)
		if n <= 0 {
			continue
		}
		bows += n
		if !r.Found || lvl > r.Best.Level {
			r.Best, r.Found = bowstringplan.Bow{Name: name, Level: lvl}, true
		}
	}
	strs := stockOf(stock, bowstringplan.BowString)
	r.Bows, r.Strings = bows, strs
	r.Pairs = min(bows, strs)
	switch {
	case bows == 0:
		r.Why = "the mule holds no unstrung bow Fletching " + strconv.Itoa(fletching) + " can string"
	case strs == 0:
		r.Why = "the mule holds " + strconv.Itoa(bows) + " unstrung bow(s) and no " + bowstringplan.BowString
	case r.Pairs < set.BowThreshold:
		r.Why = "the mule holds " + strconv.Itoa(r.Pairs) + " bow-and-string pair(s), under the threshold " + strconv.Itoa(set.BowThreshold)
	default:
		r.Ready = true
		r.Why = "the mule holds " + strconv.Itoa(r.Pairs) + " bow-and-string pairs Fletching " + strconv.Itoa(fletching) + " can string (threshold " + strconv.Itoa(set.BowThreshold) + ")"
	}
	return r
}

// ---- when to switch ----

// CheckDue reports whether it is time to read the mule's published bank again.
func CheckDue(last, now time.Time, every time.Duration) bool {
	return last.IsZero() || now.Sub(last) >= every
}

// Snapshot is what Decide reads.
type Snapshot struct {
	Now time.Time
	// MuleHeard and MuleSameWorld: the mule's heartbeat is on the bus and on this account's world.
	MuleHeard, MuleSameWorld bool
	// Stock is the mule's published bank (nil: none, or a stale row).
	Stock Stock
	// OwnUncut is the cuttable uncut gems this account's own bank held when last seen (0: none or
	// never seen this session). Live 3 Oct: a bulk kit of 98,155 gems sat in the bank after a round
	// ended early, and with the mule empty no gem round was ever due again.
	OwnUncut int
	// OwnPearl is the pearl round's items in this account's own bank when last seen: pearls a round
	// did not finish, tips waiting for Bolts.
	OwnPearl pearlplan.Held
	// CraftingLevel is the account's Crafting level; FletchingLevel and FletchingKnown its
	// Fletching.
	CraftingLevel  int
	FletchingLevel int
	FletchingKnown bool
	// FletchingXP is the account's whole Fletching experience (0 when unknown): at MaxSkillXP the bows round stops
	// although craftPastGoal names it.
	FletchingXP int
	Denied      map[string]bool
	Ledger      *Ledger
	// MagicLevel and SmithingLevel are for the superheat round (0 until the server has sent
	// them); FireStaff a fire staff worn or in this account's bank; OwnSuperheat this account's
	// bank by item name (ores, coal, runes), nil when never seen.
	MagicLevel, SmithingLevel int
	FireStaff                 bool
	// GoldGauntlets is whether the Goldsmith gauntlets are worn, in the pack or in this account's bank: the
	// superheat round plans Gold bars only then.
	GoldGauntlets bool
	OwnSuperheat  SuperheatStock
	// OwnSmith is this account's bank's bars by item name for the anvil round (nil: never seen).
	OwnSmith SmithStock
	// OwnArrows is the arrow round's items in this account's bank.
	OwnArrows ArrowHeld
	// OwnBows is the bow round's items in this account's bank (BowsReadyWithOwn).
	OwnBows OwnBows
	// BowIDs resolves a bow name to its unstrung and strung obj ids, so the unstrung bows the mule holds
	// are read from its per-obj rows. nil reads the merged rows (BowsReady's estimate).
	BowIDs bowstringplan.BowIDs
	// HerbloreLevel and HerbloreKnown are for the herb round; OwnHerbs this account's bank's grimy
	// herbs by obj id.
	HerbloreLevel int
	HerbloreKnown bool
	OwnHerbs      map[int32]int
	// Store is the storage round's reading: the mule's published bank as rows, the rows this
	// account's own bank holds, and what the mule has handed out lately (store.go).
	Store StoreInput
	// OwnJewellery is the jewellery round's items in this account's own bank, and OwnJewelleryKnown
	// whether that bank has been read since the script started (a restart has not read it yet);
	// WaterPaid is whether a worn Staff of water pays the Water rune (jewellery.go).
	OwnJewellery      JewelleryHeld
	OwnJewelleryKnown bool
	WaterPaid         bool
	// AirPaid is whether a worn Staff of air pays the Air runes of Lvl-2 Enchant (the Ring of dueling).
	AirPaid bool
	// Rune is the Runecrafting round's reading of the account (runecraft.go).
	Rune RuneInput
	// Trawler is the trawler round's reading (trawler.go).
	Trawler TrawlerInput
	// Cannon is the cannonball round's reading (cannonballs.go).
	Cannon CannonballInput
	// CannonRun is the cannon run round's reading (cannonrun.go).
	CannonRun CannonRunInput
	// OwnStaves and OwnOrbs are the Battlestaff and Fire orb in this account's bank (staves.go; 0 when unread).
	OwnStaves, OwnOrbs int
	// PrayerXP is this account's Prayer experience (0 until read) and OwnBones the bones in its bank by kind (bones.go).
	PrayerXP int
	OwnBones map[string]int
	// Sched is the round schedule's reading of the fleet's banks (schedule.go).
	Sched ScheduleInput
}

// Decision is what the account does next and why.
type Decision struct {
	Mode Mode
	// Why is the one-line reason, for a log.
	Why string
	// Gems, Pairs and Pearls are the readings behind a gem, bow or pearl decision.
	Gems, Pairs, Pearls int
	// Shops is a shop trip's planned stops; Wants what it buys.
	Shops []ShopStop
	Wants ShopWants
	// Superheat is a superheat round's plan.
	Superheat SuperheatPlan
	// Smith is an anvil round's plan.
	Smith SmithPlan
	// Cannon is a cannonball round's plan.
	Cannon CannonballPlan
	// CannonRun is a cannon run round's plan.
	CannonRun CannonRunPlan
	// Plate is a platebody round's plan.
	Plate PlatePlan
	// Heat is a superheat round's phase (schedule.go).
	Heat HeatPhase
	// Staves is a Fire battlestaff round's plan.
	Staves StavesPlan
	// Bones is a bone-burying round's plan.
	Bones BonePlan
	// Jewellery is a new jewellery batch's plan (a resume has none: the ledger's batch carries on).
	Jewellery JewelleryPlan
	// Kind is what a jewellery round makes (a new batch's, or the kind of the own-bank items a resume
	// carries on).
	Kind JewelleryKind
	// StoreRows is how many rows a storage round expects to move off the mule.
	StoreRows int
	// Inconclusive is a ModeSwarm decision made without data (the mule not heard, its bank not
	// published): the caller looks again soon rather than after a whole check interval.
	Inconclusive bool
	// GaveUp is a ModeSwarm decision made because hand-overs to the mule keep failing: the caller
	// says so loudly.
	GaveUp bool
	// Campaign says a ModeNature decision is a round of the Nature campaign (campaign.go), not of craftRuneManual's switch;
	// Continue that it is the NEXT round of a campaign already under way (after the main account's own hand-over trip, a supply run), so
	// Chain runs it straight on with no ScarabSwarm between.
	Campaign, Continue bool
	// Resume is a round decided on what this account's OWN bank holds (products to hand over, uncut
	// gems or pearls an earlier round left, tips waiting for Bolts), not on the mule's published
	// bank. Right after a round the mule may not have republished its bank yet, so a round chained
	// straight on (Chain) is one of another kind, or a Resume.
	Resume bool
}

// Chain reports whether next may run straight after a round of mode last ended, without
// ScarabSwarm between: a round of another kind, or one resuming what this account's own bank holds.
// A round of the same kind decided on the mule's published bank waits for the next check, since
// that bank may still show what the last round took.
//
// A Nature campaign's next round (Continue) chains straight on even after a round of its own kind: the hand-over trip and the
// supply run between its rounds are part of one campaign and a ScarabSwarm start between them would only walk the account away.
func Chain(last Mode, next Decision) bool {
	if next.Mode == ModeSwarm || next.Mode == "" {
		return false
	}
	return next.Mode != last || next.Resume || next.Continue
}

// Decide chooses the mode: products waiting in the bank are handed over first, then a gem round,
// then a bow round, then a pearl round (a user request: gems, then pearls and pearl bolts, then
// stringing; a user request: "the main account should prioritize bow making over oyster chiseling"), else
// ScarabSwarm. Nothing is chosen unless the mule is there to deal with: a round
// walks to its post and ends in a trade with it. The caller decides again the moment a round ends,
// so the next job that is ready follows without a check interval or a ScarabSwarm run between.
func Decide(set Settings, s Snapshot) Decision {
	swarm := func(why string) Decision { return Decision{Mode: ModeSwarm, Why: why} }
	led := s.Ledger
	if led == nil {
		led = &Ledger{}
	}
	// A trawler round under way (aboard, or a catch to claim) carries on before anything: ScarabSwarm cannot start on the boat.
	if d, ok := trawlerResume(set, s, led); ok {
		return d
	}
	if led.HandoverGaveUp(set) {
		d := swarm("GIVEN UP: " + strconv.Itoa(led.Fails(ModeDeliver)) + " hand-overs to the mule in a row failed, so no round starts (the products stay in the bank); restart the script to try again")
		d.GaveUp = true
		return d
	}
	// A Nature gate round under way (standing in the Nature temple, or at the gate) carries on first of all, before the
	// levelling round's own resume, which also reads "standing in a temple" (nature.go).
	if d, ok := natureResume(set, s, led); ok {
		return d
	}
	// A Runecrafting round under way (standing in a temple or the essence mine, or on the road with the load and
	// the talisman) carries on before the mule's heartbeat is even asked for: a restart inside a temple must not
	// leave ScarabSwarm walking from there while the bus fills.
	if d, ok := runeResume(set, s, led); ok {
		return d
	}
	// The trawler round (trawler.go), before every training round and before the mule is asked about: it needs no mule, and
	// a crew waiting in the lobby waits on this account alone.
	trawlerDec, trawlerWhy := decideTrawler(set, s, led)
	if trawlerDec.Mode == ModeTrawler {
		return trawlerDec
	}
	if !s.MuleHeard {
		d := swarm("the mule is not heard on the bus")
		d.Inconclusive = true
		return d
	}
	if !s.MuleSameWorld {
		return swarm("the mule is on another world")
	}
	// blocked says a mode is held off: paused after a round that found nothing, or backing off after
	// rounds that failed for a passing reason.
	blocked := func(mode Mode, label string, pauseUntil time.Time) (string, bool) {
		if s.Now.Before(pauseUntil) {
			return label + " rounds are paused until " + pauseUntil.Format("15:04"), true
		}
		if until, ok := led.BackedOff(mode, s.Now); ok {
			return label + " rounds are backing off until " + until.Format("15:04") + " after " + strconv.Itoa(led.Fails(mode)) + " failed in a row", true
		}
		return "", false
	}
	var deliverWhy string
	if led.PendingDeliver {
		why, held := blocked(ModeDeliver, "hand-over", time.Time{})
		if !held {
			return Decision{Mode: ModeDeliver, Resume: true, Why: "products from an earlier round are still in the bank"}
		}
		deliverWhy = why
	}
	// The jewellery round, after a hand-over and before every other round (jewellery.go): the fleet's
	// run stalling takes a Games necklace or Ring of dueling from the mule, and a round makes a batch
	// only when the mule has none, so it is rare and short; waiting behind a gem round that the mule's
	// stock keeps due would leave every account without one.
	//
	// Jewellery made and not yet handed over is the mule's, so a pause or a backoff of the round (a round that
	// failed before its hand-over, live 7 Oct 2026 14:54: 50 Rings of dueling stranded in the bank for the
	// length of the backoff) does not hold the hand-over off; only the hand-over's own backoff does
	// (JewelleryReady).
	var jewelWhy string
	if why, held := blocked(ModeJewellery, "jewellery", led.Jewellery.PauseUntil); held && led.Jewellery.OwedTotal() <= 0 {
		jewelWhy = why
	} else if d, w := JewelleryReady(set, s, led); d.Mode == ModeJewellery {
		return d
	} else {
		jewelWhy = w
	}
	// The Nature gate round (nature.go), then the Runecrafting levelling round (runecraft.go), before the batch rounds: long
	// exclusive rounds that a gem or bow round, due again at every check, would otherwise pull the main account away from.
	var runeWhy string
	if d, why := decideNature(set, s, led); d.Mode == ModeNature {
		return d
	} else {
		runeWhy = why
	}
	if d, why := decideRune(set, s, led); d.Mode == ModeRunecraft {
		return d
	} else if why != "" {
		runeWhy = strings.TrimPrefix(strings.Join([]string{runeWhy, why}, "; "), "; ")
	}
	// train says whether a training round may be considered: switched on, and not held off by the skill goal (goal.go),
	// whose reason then stands in for the round's own.
	train := func(on bool, mode Mode, why *string) bool {
		if !on {
			return false
		}
		if w, held := AtGoal(set, s, mode); held {
			*why = w
			return false
		}
		return true
	}
	var gemWhy, bowWhy string
	if train(set.GemCutting, ModeGems, &gemWhy) {
		gems := GemsBestFirst(s.CraftingLevel, set.GemKinds, s.Denied, set.MembersWorld)
		total, _ := GemStock(gems, s.Stock)
		why, held := blocked(ModeGems, "gem", led.GemPauseUntil)
		switch {
		case led.PendingCut && !held:
			return Decision{Mode: ModeGems, Resume: true, Why: "cuttable uncut gems from an earlier round are still in the bank"}
		case s.OwnUncut >= set.GemThreshold && !held:
			return Decision{Mode: ModeGems, Resume: true, Gems: s.OwnUncut, Why: "this account's bank holds " + strconv.Itoa(s.OwnUncut) + " uncut gems Crafting " + strconv.Itoa(s.CraftingLevel) + " can cut"}
		case s.Stock == nil:
			gemWhy = "the mule has not published its bank"
		case held:
			gemWhy = why
		case total+s.OwnUncut >= set.GemThreshold:
			return Decision{Mode: ModeGems, Gems: total + s.OwnUncut, Why: "the mule's bank and this account's hold " + strconv.Itoa(total+s.OwnUncut) + " uncut gems Crafting " + strconv.Itoa(s.CraftingLevel) + " can cut together (threshold " + strconv.Itoa(set.GemThreshold) + ")"}
		case set.BulkAtSapphires > 0 && stockOf(s.Stock, "Uncut sapphire") >= set.BulkAtSapphires && total > 0:
			// The sapphire trigger (a user request: "gemcutting loop can trigger at 10k uncut sapphires too,
			// cutting all uncut gems the mule has"): the same stock makes PlanGemAsk a bulk ask for every kind.
			return Decision{Mode: ModeGems, Gems: total + s.OwnUncut, Why: "the mule holds " + strconv.Itoa(stockOf(s.Stock, "Uncut sapphire")) + " uncut sapphires (craftBulkAtSapphires " + strconv.Itoa(set.BulkAtSapphires) + "): cutting all " + strconv.Itoa(total) + " uncut gems it holds"}
		default:
			gemWhy = "the mule's bank and this account's hold " + strconv.Itoa(total+s.OwnUncut) + " cuttable uncut gems together, under the threshold " + strconv.Itoa(set.GemThreshold)
		}
	}
	if train(set.BowStringing, ModeBows, &bowWhy) {
		switch {
		case !s.FletchingKnown:
			bowWhy = "Fletching is not known yet"
		default:
			if why, held := blocked(ModeBows, "bow", led.BowPauseUntil); held {
				bowWhy = why
				break
			}
			r := BowsReadyWithOwn(set, s.FletchingLevel, s.Stock, led, s.OwnBows, s.BowIDs)
			if r.Ready {
				return Decision{Mode: ModeBows, Pairs: r.Pairs, Resume: r.Resume, Why: r.Why}
			}
			bowWhy = r.Why
		}
	}
	var pearlWhy string
	if train(set.Pearls, ModePearls, &pearlWhy) {
		why, held := blocked(ModePearls, "pearl", led.PearlPauseUntil)
		r := pearlplan.Ready(set.PearlThreshold, set.BoltThreshold, s.FletchingLevel, s.FletchingKnown,
			pearlplan.HeldIn(pearlplan.Stock(s.Stock)), s.OwnPearl, s.Stock != nil)
		switch {
		case !set.MembersWorld:
			pearlWhy = "pearl bolt tips need a members world (" + KeyMembersWorld + " is off)"
		case held:
			pearlWhy = why
		case r.Ready:
			n := s.OwnPearl.PearlCount()
			if !r.Resume {
				n += pearlplan.HeldIn(pearlplan.Stock(s.Stock)).PearlCount()
			}
			return Decision{Mode: ModePearls, Pearls: n, Resume: r.Resume, Why: r.Why}
		default:
			pearlWhy = r.Why
		}
	}
	// The Fire battlestaff round (staves.go), after the other fast Crafting rounds (gems, bows, pearls): the mule's Battlestaff and
	// Fire orb both at the threshold.
	var stavesWhy string
	if train(set.Staves, ModeStaves, &stavesWhy) {
		why, held := blocked(ModeStaves, "staves", led.Staves.PauseUntil)
		sp, ok := StavesReady(set, s.CraftingLevel, s.Stock, s.OwnStaves, s.OwnOrbs)
		switch {
		case held:
			stavesWhy = why
		case ok:
			return Decision{Mode: ModeStaves, Staves: sp, Resume: sp.Resume, Why: sp.Why}
		default:
			stavesWhy = "staves: " + sp.Why
		}
	}
	// The storage round (store.go), after gems, bows and pearls and before the later rounds: the mule's
	// bank is nearly full of rows the fleet does not use, and this account's is nearly empty.
	var storeWhy string
	if set.StoreOverflow {
		why, held := blocked(ModeStore, "store", led.StorePauseUntil)
		r := StoreReady(set, s.Store)
		switch {
		case held:
			storeWhy = why
		case r.Ready:
			return Decision{Mode: ModeStore, StoreRows: r.Move, Why: r.Why}
		default:
			storeWhy = "store: " + r.Why
		}
	}
	// The later rounds, in the owner's order (4 Oct 2026): after gems, pearls and bows, before the
	// ScarabSwarm fallback. The shop trip first of them: it feeds the pearl and arrow rounds.
	var shopWhy, heatWhy, smithWhy, arrowWhy, herbWhy string
	if set.BuyBolts || set.BuyArrowTips {
		mule := pearlplan.HeldIn(pearlplan.Stock(s.Stock))
		var want ShopWants
		boltOK, boltWhy := BoltsWanted(set, s.OwnPearl, mule)
		want.Bolts = boltOK && set.Pearls && set.MembersWorld
		want.ArrowTips = ArrowTipsWanted(set, s.OwnArrows, ArrowHeldIn(s.Stock))
		why, held := blocked(ModeShop, "shop", led.ShopPauseUntil)
		switch {
		case held:
			shopWhy = why
		case !want.Any():
			shopWhy = "shop: " + boltWhy
		default:
			stops, ok, w := ShopReady(set, want, led.Shelves, s.Now)
			if ok {
				return Decision{Mode: ModeShop, Shops: stops, Wants: want, Why: w}
			}
			shopWhy = "shop: " + w
		}
	}
	// The arrow round before the anvil and superheat rounds (a user request: arrows and bolts "should prioritize as the
	// amounts are stacking way above the 20k thresholds"); the anvil's arrowtips wait in the mule's bank meanwhile.
	if train(set.Arrows, ModeArrows, &arrowWhy) {
		why, held := blocked(ModeArrows, "arrow", led.ArrowPauseUntil)
		ok, resume, w := ArrowsReady(set, s.FletchingLevel, s.FletchingKnown, ArrowHeldIn(s.Stock), s.OwnArrows, s.Stock != nil)
		switch {
		case held:
			arrowWhy = why
		case set.Trawler && !resume:
			// The trawler round tips the arrows between its casts (a user request: "we are making arrows at the bank
			// instead of at the fishing trawler gaining magic and fishing exp also"): no new round at the bank.
			arrowWhy = "arrows: made aboard the trawler between casts"
		case ok:
			return Decision{Mode: ModeArrows, Resume: resume, Why: w}
		default:
			arrowWhy = "arrows: " + w
		}
	}
	if train(set.Superheat, ModeSuperheat, &heatWhy) {
		// Steel first, then Mithril (schedule.go); the Adamantite batch is the last round of all, below.
		if d, w := decideHeat(set, s, led, false); d.Mode == ModeSuperheat {
			return d
		} else {
			heatWhy = w
		}
	}
	// The platebody round (platebody.go) right after the superheat round it follows: Adamantite ore gone, the highest bar first,
	// an hour a round. Before the cannonball and anvil rounds: the anvil round (Steel > 8000) would turn these bars into arrowtips.
	var plateWhy string
	if train(set.Platebody, ModePlatebody, &plateWhy) {
		why, held := blocked(ModePlatebody, "platebody", led.Plate.PauseUntil)
		pp, ok := PlatebodyReady(set, s.SmithingLevel, led.Plate, s.Now, s.Stock, s.OwnSmith)
		switch {
		case held:
			plateWhy = why
		case ok:
			return Decision{Mode: ModePlatebody, Plate: pp, Resume: pp.Resume, Why: pp.Why}
		default:
			plateWhy = "platebody: " + pp.Why
		}
	}
	// The bone-burying round (bones.go), a batch round in the rotation: the mule holds craftBonesMinStock bones, or this account's bank
	// holds a load from a round cut short. Not held by the skill goal: Prayer is run to the 200 million cap (the owner's aim).
	var boneWhy string
	if set.Bones {
		why, held := blocked(ModeBones, "bones", led.Bones.PauseUntil)
		bp, ok := BonesReady(set, s.PrayerXP, s.Stock, s.OwnBones)
		switch {
		case held:
			boneWhy = why
		case ok:
			return Decision{Mode: ModeBones, Bones: bp, Resume: bp.Resume, Why: bp.Why}
		default:
			boneWhy = "bones: " + bp.Why
		}
	}
	// The cannon run round (cannonrun.go): a Ranged stint with a cannon at the Khazard battlefield, a batch round in the rotation. It is
	// triggered by the Cannonballs available (the mule's and this account's, 10,000), runs until 10,000 are used, and a stint is an hour, so the
	// rotation gives the other batch rounds their turns between its stints.
	var cannonRunWhy string
	if set.CannonRun.Start > 0 {
		why, held := blocked(ModeCannonRun, "cannon run", led.CannonRun.PauseUntil)
		rp, ok := CannonRunReady(set, s, led.CannonRun)
		switch {
		case held:
			cannonRunWhy = why
		case ok:
			return Decision{Mode: ModeCannonRun, CannonRun: rp, Resume: rp.Resume || rp.Errand == ErrandRecover, Why: rp.Why}
		default:
			cannonRunWhy = "cannon run: " + rp.Why
		}
	}
	// The cannonball round (cannonballs.go) after the superheat round that makes its Steel bars and BEFORE the anvil round, which
	// would turn every Steel bar the banks hold into arrowtips once they pass its threshold (a user request: "master should make
	// cannonballs ... 2k cannon balls are needed per account"). Below the trawler and Nature rounds (decided first, above) and above
	// the ScarabSwarm fallback. It is a supply round: the skill goal does not hold it (AtGoal is not asked), the 200 million
	// Smithing cap does.
	var cannonWhy string
	if set.CannonballTarget > 0 {
		why, held := blocked(ModeCannonballs, "cannonball", led.Cannon.PauseUntil)
		cp, ok := CannonballsReady(set, s, led.Cannon.Active)
		switch {
		case held:
			cannonWhy = why
		case ok:
			return Decision{Mode: ModeCannonballs, Cannon: cp, Resume: cp.Resume, Why: cp.Why}
		default:
			cannonWhy = "cannonballs: " + cp.Why
		}
	}
	// The anvil round (smith.go) after the superheat round that makes its bars and before the arrow round that
	// uses its Steel arrowtips.
	if train(set.Smith, ModeSmith, &smithWhy) {
		why, held := blocked(ModeSmith, "smith", led.SmithPauseUntil)
		sp, ok := SmithReady(set, s.SmithingLevel, led.SmithActive, s.Stock, s.OwnSmith)
		switch {
		case held:
			smithWhy = why
		case ok:
			return Decision{Mode: ModeSmith, Smith: sp, Resume: sp.Resume, Why: sp.Why}
		default:
			smithWhy = "smith: " + sp.Why
		}
	}
	if train(set.Herbs, ModeHerbs, &herbWhy) {
		why, held := blocked(ModeHerbs, "herb", led.HerbPauseUntil)
		ok, resume, w := HerbsReady(set, s.HerbloreLevel, s.HerbloreKnown, s.Stock, s.OwnHerbs)
		switch {
		case held:
			herbWhy = why
		case ok && !resume && !led.Sched.Due(KeyHerbRound, s.Now, set.Sched.HerbEvery):
			// Herbs about once every 24 hours (a user request).
			herbWhy = "herbs: the last round ended " + led.Sched.LastEnd[KeyHerbRound].Format("15:04 2 Jan") + ", the next is due " + led.Sched.LastEnd[KeyHerbRound].Add(set.Sched.HerbEvery).Format("15:04 2 Jan")
		case ok:
			return Decision{Mode: ModeHerbs, Resume: resume, Why: w}
		default:
			herbWhy = "herbs: " + w
		}
	}
	// The Adamantite batch is the LAST round of all (a user request: "only after all other tasks are done and there is no ore
	// left to create"): nothing above is due, so the Adamantite and Runite ore is superheated for an hour, then left alone for
	// craftPauseAdamantMinutes.
	if set.Superheat {
		if _, held := AtGoal(set, s, ModeSuperheat); !held {
			if d, w := decideHeat(set, s, led, true); d.Mode == ModeSuperheat {
				return d
			} else if w != "" {
				heatWhy = nonEmpty(heatWhy, "") + "; " + w
			}
		}
	}
	parts := []string{nonEmpty(gemWhy, "gems off"), nonEmpty(pearlWhy, "pearls off"), nonEmpty(bowWhy, "bows off"), nonEmpty(stavesWhy, "staves off"),
		nonEmpty(storeWhy, "store off"),
		nonEmpty(shopWhy, "shop off"), nonEmpty(heatWhy, "superheat off"), nonEmpty(plateWhy, "platebody off"), nonEmpty(boneWhy, "bones off"), nonEmpty(cannonRunWhy, "cannon run off"), nonEmpty(cannonWhy, "cannonballs off"), nonEmpty(smithWhy, "smith off"), nonEmpty(arrowWhy, "arrows off"), nonEmpty(herbWhy, "herbs off"),
		nonEmpty(jewelWhy, "jewellery off")}
	if runeWhy != "" {
		parts = append(parts, runeWhy)
	}
	if trawlerWhy != "" {
		parts = append(parts, trawlerWhy)
	}
	if deliverWhy != "" {
		parts = append([]string{deliverWhy}, parts...)
	}
	d := swarm(strings.Join(parts, "; "))
	d.Inconclusive = s.Stock == nil
	return d
}

func nonEmpty(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}

// PauseAfter is the time a kind of round is left alone after it ended: not at all when it produced
// something, else pause from now.
func PauseAfter(produced int, now time.Time, pause time.Duration) time.Time {
	if produced > 0 || pause <= 0 {
		return time.Time{}
	}
	return now.Add(pause)
}

// RoundExpired reports whether a round that began at start has used up its budget.
func RoundExpired(start, now time.Time, max time.Duration) bool {
	return max > 0 && !start.IsZero() && now.Sub(start) >= max
}

// ---- the safe point ----

// SafeInput is what the bot reads of ScarabSwarm and the account to say whether ScarabSwarm may be
// stopped here.
type SafeInput struct {
	TileKnown bool
	// Hazard is a place a long walk to the mule should not start from: the Wilderness, the Mage
	// Bank, the Kalphite lair.
	Hazard bool
	Dead   bool
	// InCombat is a hit on the account in the last few seconds; Hurt the swarm's own "recently
	// lost hitpoints" reading.
	InCombat, Hurt bool
	HP, MaxHP      int
	// ModalOpen is a bank, trade or other main interface open.
	ModalOpen bool
	// RandomEvent is a random event on screen (the guardian is busy).
	RandomEvent bool
	// Errand is a world errand, market stay or world hop in flight.
	Errand bool
	// Trip is a Stage 3 rune trip under way (kit in the pack, a walk half done).
	Trip bool
	// Quest is the account standing where only a quest goes (the Waterfall dungeon): stopping
	// there strands it.
	Quest bool
	// Underground is a dungeon map square (z 6400 and up: the Giants' dungeon, the Waterfall, the
	// sewers): the way out is a walk past what lives there, and ScarabSwarm's own eating watch stops
	// with it. Accounts died on exactly that walk.
	Underground bool
	// FarFromMule is anywhere but the mule's side of White Wolf Mountain (the surface west of it,
	// consolidateSafePlace) or a short walk from the mule's post: the walk to the post from there
	// crosses the ships, the pass or the wolves.
	FarFromMule bool
}

// SafePoint says whether ScarabSwarm may be stopped now, and if not, why. Not mid-trade (a modal
// open), not in a fight (hit lately, or hurt), not mid-quest, not mid-errand or rune trip, not
// dead, not underground, not far from the mule, not under the hitpoint floor (80% by default: the
// walk to the mule's bank is made without ScarabSwarm's eating), not in a place a walk should not
// start from.
func SafePoint(in SafeInput, minHpPercent int) (bool, string) {
	switch {
	case !in.TileKnown:
		return false, "position not known yet"
	case in.Dead:
		return false, "dead (respawning)"
	case in.InCombat:
		return false, "in a fight"
	case in.Hurt:
		return false, "recently hurt"
	case in.ModalOpen:
		return false, "an interface (bank or trade) is open"
	case in.RandomEvent:
		return false, "a random event is on screen"
	case in.Errand:
		return false, "a world errand or hop is in flight"
	case in.Trip:
		return false, "a rune trip is under way"
	case in.Quest:
		return false, "standing where only a quest goes"
	case in.Underground:
		return false, "underground (the walk out is not safe to start)"
	case in.FarFromMule:
		return false, "not on the mule's side of White Wolf Mountain or near its post"
	case in.Hazard:
		return false, "in the Wilderness, the Mage Bank or the Kalphite lair"
	}
	if in.MaxHP > 0 && in.HP > 0 && in.HP*100 < in.MaxHP*minHpPercent {
		return false, "hitpoints " + strconv.Itoa(in.HP) + "/" + strconv.Itoa(in.MaxHP) + " are under " + strconv.Itoa(minHpPercent) + "%"
	}
	return true, ""
}
