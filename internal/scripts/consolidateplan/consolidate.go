// Package consolidateplan is the owner's "every account mules its coins, runes and food to the
// mule, and the mule hands them back out evenly" (2 October 2026), decided from plain numbers.
//
// THE TWO HALVES SHARE ONE KEEP. An account deposits only what is above its keep (plus a minimum
// worth the walk), and asks for a top-up only when it is under HALF its keep. The band between
// the two is the hysteresis that stops an account handing coins to the mule on Monday and asking
// for them back on Tuesday: a deposit leaves exactly the keep, and a top-up asks back to exactly
// the keep, so neither side can trigger the other.
//
// THE KEEP IS WHAT THE ACCOUNT'S OWN STAGE STILL NEEDS, not a flat number:
//
//   - Coins: a fixed reserve (fares, a death, a trainer's purse) plus, for an account that buys
//     its chaos at a player market, the chaos still needed to its Magic target priced at the
//     market's last sale, capped. That earmark is the third owner goal ("enough coins to buy all
//     the chaos from Seers"): the coins that will buy chaos stay where the buying happens, and the
//     rest (world-2 accounts, accounts at their target) is the fleet's spare.
//   - Runes: the training spell's runes for the casts still left to the Magic target. Every other
//     rune is spare (the owner's 24 Sep ruling that Death and Blood are the Queen's was reversed for
//     training on 6 Oct, scarabplan/tier.go, and the mule is where a KQ kit is drawn from), except the
//     names on Settings.KeepItems (Mind rune by default: the mind share hands those down peer to peer,
//     scarabswarm_mindshare.go), and except the runes a THIEF pockets (runekeep.go, owner 13 Oct
//     2026): a Paladin's Chaos, a Hero's Death and Blood. A thief hands NONE of such a rune over until it holds Settings.PocketHold (10,000)
//     of it, and then all of it but Settings.PocketCasts (1,000) casts' worth for its own training
//     spell, so the mule pools the runes in lots of thousands rather than being traded the dribble.
//   - Food: Stage 3's own stage3FoodKeep.
//
// It is pure: no client, actions, fleet or bot imports, and every time is passed in.
package consolidateplan

import (
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

// CoinsName is the coin object's display name.
const CoinsName = "Coins"

// Settings are the consolidation knobs. JSON keys match internal/bot's setting names; every one
// defaults to the value in Defaults. Enabled defaults to TRUE since the loot hand-off (owner,
// 4 Oct 2026: "implement loot hand off on scarab swarm periodically"); it was false while the trip
// moved only coins, runes and food.
type Settings struct {
	// Enabled is consolidateToMule.
	Enabled bool `json:"consolidateToMule"`
	// Every is consolidateEveryHours: the least time between two deposit trips by one account.
	// A trip is a walk to the mule's post and back (from East Ardougne ~200 tiles each way, and
	// two world hops for an account whose home is not the mule's world), so it is rare by design;
	// the per-account start slot (scarabplan.MuleAskSlot) spreads a cohort over the whole
	// interval. 24 hours: ~250 Stage 5 accounts make ~10 trips an hour, a tenth of the mule's
	// serving time (see the loot hand-off's COLLAB entry for the arithmetic).
	Every time.Duration `json:"-"`
	// CoinsReserve is consolidateCoinsReserve: coins that always stay with the account.
	CoinsReserve int `json:"consolidateCoinsReserve"`
	// ChaosPrice is consolidateChaosPrice: what one chaos still to buy is earmarked at. 95 is the
	// Seers Market's last sale on 2 Oct 2026 (/api/market), and the average paid over 1,034 live
	// purchases 29 Sep - 2 Oct (16,073,855 coins for 169,201 chaos).
	ChaosPrice int `json:"consolidateChaosPrice"`
	// EarmarkCap is consolidateEarmarkCap: the most coins kept back for future chaos.
	EarmarkCap int `json:"consolidateEarmarkCap"`
	// MinCoins, MinRunes and MinFood are the smallest surplus of each kind that justifies the
	// walk on its own (consolidateMinCoins/Runes/Food). Once the walk is justified, every kind's
	// surplus goes.
	MinCoins int `json:"consolidateMinCoins"`
	MinRunes int `json:"consolidateMinRunes"`
	MinFood  int `json:"consolidateMinFood"`
	// KeepItems are names never deposited (consolidateKeepItems).
	KeepItems []string `json:"consolidateKeepItems"`
	// MaxQueue is consolidateMaxMuleQueue: a deposit never joins a mule queue longer than this
	// (kits, orders and announced donors ahead), because a hand-out to a blocked account is worth
	// more than a deposit that can wait.
	MaxQueue int `json:"consolidateMaxMuleQueue"`

	// ---- the loot hand-off (loot.go) ----

	// Loot is consolidateLoot: the trip also carries the bank's loot (everything not kit).
	Loot bool `json:"consolidateLoot"`
	// MinLootValue is consolidateMinLootValue: loot worth this much (obj cost, summed) justifies
	// the trip on its own. MinLootRows is consolidateMinLootRows: so does this many rows of it,
	// whatever it is worth, because the account's own bank space is worth freeing.
	MinLootValue int `json:"consolidateMinLootValue"`
	MinLootRows  int `json:"consolidateMinLootRows"`
	// MaxLoads is consolidateMaxLoads: the most pack loads (one trade round each, ~23 rows a
	// trade as notes) one trip hands over; the rest waits for the next trip.
	MaxLoads int `json:"consolidateMaxLoads"`
	// MuleBankSlots and MuleBankReserve bound the new item types one trip may give the mule
	// (consolidateMuleBankSlots, consolidateMuleBankReserve): its bank holds MuleBankSlots
	// distinct items, and MuleBankReserve rows always stay free for its own working stock.
	MuleBankSlots   int `json:"consolidateMuleBankSlots"`
	MuleBankReserve int `json:"consolidateMuleBankReserve"`
	// LootClues is consolidateLootClues: hand clue scrolls over too. Off because every clue step
	// is its own obj, so the cohort's clues would cost the mule a bank row per step id.
	LootClues bool `json:"consolidateLootClues"`

	// OnKitAsk is consolidateOnKitAsk: a kit ask (every stage from 2 up) also hands over the bank's surplus and loot
	// (kitask.go). Needs Enabled too.
	OnKitAsk bool `json:"consolidateOnKitAsk"`

	// ---- the thieves' runes (runekeep.go) ----

	// PocketHold is consolidatePocketHold: a thief (the pickpocket rung that pays a rune is its rung) hands the
	// mule none of that rune until it holds this many of it (bank and pack together). 10,000 (owner, 13 Oct
	// 2026: "pickpocket 10k runes before handing them off"). 0 or less switches the rule off: a pocketed rune is
	// then kept or handed over like any other.
	PocketHold int `json:"consolidatePocketHold"`
	// PocketCasts is consolidatePocketCasts: once it holds PocketHold, the thief hands over all of the rune but
	// this many casts' worth for its own training spell. 1,000 (owner: "hand over all but 1k casts to train
	// with"), a training round (scarabplan.DefaultTrainCasts).
	PocketCasts int `json:"consolidatePocketCasts"`
	// PocketRunes are the runes the rule covers (consolidatePocketRunes), by name; a name no pickpocket pays
	// (scarabplan: only Chaos from Paladins, Death and Blood from Heroes) is ignored.
	PocketRunes []string `json:"consolidatePocketRunes"`

	// LootOnly is the solo dump (ScarabSwarm's supplyMode "soloDump" / "soloMarketDump",
	// scarabplan/supplymode.go): the trip carries the loot and nothing else. Coins, runes, food and
	// staffs stay, because a solo account supplies itself with exactly those and never asks for them
	// back. Not a JSON setting: the bot sets it from the mode, and Normalised turns Loot on with it.
	LootOnly bool `json:"-"`

	// AllNatures hands over every Nature rune the account holds, whatever its keep and keep list say: set
	// by a kit ask's hand-over for an account under Magic 55 (kitask.go), which cannot cast High Level
	// Alchemy, the only thing the fleet's Nature runes are for. Not a JSON setting.
	AllNatures bool `json:"-"`
}

// NatureRune is the rune AllNatures hands over.
const NatureRune = "Nature rune"

// Defaults are the settings' defaults.
//
// CoinsReserve is 20,000, not the 50,000 it was until 13 Oct 2026 (owner: one RuneRunner should "take enough
// coins to buy all runes from all shops on all worlds": up to 5.9M for a full-shelf round, about 0.7M a round
// after). Every real ScarabSwarm coin use fits under it: ship fares draw at most 600, Stage 3 shop trips spend
// above their own 2,000 and ask the mule for a shortfall, Seers chaos is protected by the earmark, and the
// largest quest provision is Dragon Slayer's ~12,500. On 13 Oct (bank-all, 304 ScarabSwarm accounts) the
// cohort's banks held 9.9M above a 50,000 keep and 17.3M above a 20,000 one; the kit-ask hand-over (surplus
// of 20,000 or more) can release 9.4M from 84 accounts at 50,000 and 17.0M from 244 accounts at 20,000.
func Defaults() Settings {
	return Settings{
		Enabled:         true,
		Every:           24 * time.Hour,
		CoinsReserve:    20_000,
		ChaosPrice:      95,
		EarmarkCap:      2_000_000,
		MinCoins:        100_000,
		MinRunes:        500,
		MinFood:         20,
		KeepItems:       []string{fleetplan.MindRune},
		MaxQueue:        2,
		Loot:            true,
		MinLootValue:    20_000,
		MinLootRows:     15,
		MaxLoads:        3,
		MuleBankSlots:   MuleBankSlots,
		MuleBankReserve: 48,
		LootClues:       false,
		OnKitAsk:        true,
		PocketHold:      DefaultPocketHold,
		PocketCasts:     DefaultPocketCasts,
		PocketRunes:     DefaultPocketRunes(),
	}
}

// Normalised clamps nonsense to something the plan can act on.
func (s Settings) Normalised() Settings {
	d := Defaults()
	if s.Every < time.Hour {
		s.Every = d.Every
	}
	if s.CoinsReserve < 0 {
		s.CoinsReserve = 0
	}
	if s.ChaosPrice < 0 {
		s.ChaosPrice = 0
	}
	if s.EarmarkCap < 0 {
		s.EarmarkCap = 0
	}
	if s.MinCoins < 1 {
		s.MinCoins = 1
	}
	if s.MinRunes < 1 {
		s.MinRunes = 1
	}
	if s.MinFood < 1 {
		s.MinFood = 1
	}
	if s.MaxQueue < 0 {
		s.MaxQueue = 0
	}
	if s.MinLootValue < 1 {
		s.MinLootValue = 1
	}
	if s.MinLootRows < 1 {
		s.MinLootRows = 1
	}
	if s.MaxLoads < 1 {
		s.MaxLoads = 1
	}
	if s.MuleBankSlots < 1 || s.MuleBankSlots > MuleBankSlots {
		s.MuleBankSlots = MuleBankSlots
	}
	if s.MuleBankReserve < 0 {
		s.MuleBankReserve = 0
	}
	if s.LootOnly {
		s.Loot = true
	}
	if s.PocketHold < 0 {
		s.PocketHold = 0
	}
	if s.PocketCasts < 0 {
		s.PocketCasts = 0
	}
	return s
}

// KeepInput is what one account's keep is computed from.
type KeepInput struct {
	// CastsLeft is the training spell's casts still left to the Magic target
	// (scarabplan.CastsToMagicTarget); 0 at or past the target.
	CastsLeft int
	// Spell is the training spell; Staff the staff that pays some of its runes.
	Spell fleetplan.Spell
	Staff fleetplan.Provided
	// MarketBuyer is true when this account buys its chaos at a player market (it stands on a
	// market world and the stage still trains); only then are coins earmarked for chaos.
	MarketBuyer bool
	// ChaosHeld is the chaos the account already holds, bank and pack, which the earmark need not buy.
	ChaosHeld int
	// FoodKeep is stage3FoodKeep.
	FoodKeep int
	// ExtraRunes are runes kept on top of the spell's: the walk-alch and teleport-travel kits
	// (natures, laws), which the account would otherwise hand over and ask back for.
	ExtraRunes map[string]int
	// ThievingLevel is the account's Thieving level when it pockets runes (it works the Paladins and Heroes, so
	// the pocket rule applies), 0 when it does not: then no rune is a pocketed one.
	ThievingLevel int
	// TrainSpells are the spells the account trains Magic with (the one being cast and the tier above or below
	// it): a pocketed rune's ration is the largest of their needs for Settings.PocketCasts casts.
	TrainSpells []fleetplan.Spell
}

// Keep is what stays with the account, per kind.
type Keep struct {
	Coins int
	// Runes is the training spell's need per rune; a rune not named keeps 0.
	Runes map[string]int
	Food  int
	// Pocket names the runes the account pockets and so holds back until PocketHold of them are in hand
	// (runekeep.go). Runes[name] is then the RATION kept once the hold is reached, not the casts to the target.
	Pocket map[string]bool
	// PocketHold is Settings.PocketHold as KeepFor read it.
	PocketHold int
}

// KeepFor is the account's keep.
func KeepFor(s Settings, in KeepInput) Keep {
	s = s.Normalised()
	k := Keep{Coins: s.CoinsReserve, Runes: map[string]int{}, Food: max(0, in.FoodKeep)}
	casts := max(0, in.CastsLeft)
	for _, r := range fleetplan.RunesForCasts(in.Spell, casts, in.Staff) {
		k.Runes[r.Name] += r.Count
	}
	for name, n := range in.ExtraRunes {
		if n > 0 {
			k.Runes[name] += n
		}
	}
	// The chaos earmark is the coins that will BUY the chaos still short of the target, so it is sized from the
	// casts to the target, before the thieves' rule below replaces the chaos keep with a ration.
	if in.MarketBuyer {
		k.Coins += ChaosEarmark(k.Runes[chaosRune]-in.ChaosHeld, s.ChaosPrice, s.EarmarkCap)
	}
	k.applyPocket(s, in)
	return k
}

const chaosRune = "Chaos rune"

// ChaosEarmark is the coins kept for chaos still to buy: short x price, capped, never negative.
func ChaosEarmark(short, price, limit int) int {
	if short <= 0 || price <= 0 {
		return 0
	}
	n := short * price
	if limit > 0 && n > limit {
		n = limit
	}
	return n
}

// Holdings is what the account holds, bank and pack together, by item name.
type Holdings struct {
	Coins int
	Runes map[string]int
	Food  map[string]int
}

// Plan is one deposit trip: what crosses to the mule. Walk is false when the surplus is not worth
// the trip, and Why says which way it went.
type Plan struct {
	Walk  bool
	Coins int
	Runes map[string]int
	Food  map[string]int
	Why   string
	// Natures is the Nature runes handed over because the account cannot alch (Settings.AllNatures); they
	// are in Runes too, and they alone justify the walk.
	Natures int
}

// Items is the plan's names in a fixed order (coins, runes by name, food by name), for the
// withdrawal and the trade's item list.
func (p Plan) Items() []string {
	var out []string
	if p.Coins > 0 {
		out = append(out, CoinsName)
	}
	out = append(out, sortedKeys(p.Runes)...)
	out = append(out, sortedKeys(p.Food)...)
	return out
}

// Count is how many of name the plan sends.
func (p Plan) Count(name string) int {
	if strings.EqualFold(name, CoinsName) {
		return p.Coins
	}
	for k, v := range p.Runes {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	for k, v := range p.Food {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return 0
}

// RuneUnits and FoodUnits are the plan's rune and food totals.
func (p Plan) RuneUnits() int { return sum(p.Runes) }
func (p Plan) FoodUnits() int { return sum(p.Food) }

// PlanDeposit is what one trip sends: every kind's surplus over its keep, but only when at least
// one kind clears its own minimum. foodSlots caps the food, which does not stack: one free pack
// slot per bite carried to the mule.
func PlanDeposit(s Settings, h Holdings, k Keep, foodSlots int) Plan {
	s = s.Normalised()
	p := Plan{Runes: map[string]int{}, Food: map[string]int{}}
	if !kept(s.KeepItems, CoinsName) {
		p.Coins = max(0, h.Coins-k.Coins)
	}
	for name, n := range h.Runes {
		if s.AllNatures && strings.EqualFold(name, NatureRune) {
			if n > 0 {
				p.Runes[name] = n // the account cannot alch: all of them go, keep or not
				p.Natures = n
			}
			continue
		}
		if kept(s.KeepItems, name) {
			continue
		}
		// A thief's pocketed rune: none of it crosses until the account holds the hold (runekeep.go); from
		// there on the spare is over the ration, like any rune's.
		if k.pocketHeld(name, n) {
			continue
		}
		if spare := n - keepOf(k.Runes, name); spare > 0 {
			p.Runes[name] = spare
		}
	}
	// Food: the surplus across every food together, taken from the most plentiful first, so the
	// account keeps a mix rather than its last of everything.
	foodTotal := 0
	for name, n := range h.Food {
		if !kept(s.KeepItems, name) && n > 0 {
			foodTotal += n
		}
	}
	if spare := min(foodTotal-k.Food, max(0, foodSlots)); spare > 0 {
		for _, name := range byCountDesc(h.Food) {
			if spare <= 0 {
				break
			}
			if kept(s.KeepItems, name) {
				continue
			}
			take := min(h.Food[name], spare)
			if take > 0 {
				p.Food[name] = take
				spare -= take
			}
		}
	}
	switch {
	case p.Natures > 0:
		p.Walk, p.Why = true, "Nature runes the account cannot cast"
	case p.Coins >= s.MinCoins:
		p.Walk, p.Why = true, "coins over the keep"
	case p.RuneUnits() >= s.MinRunes:
		p.Walk, p.Why = true, "runes over the keep"
	case p.FoodUnits() >= s.MinFood:
		p.Walk, p.Why = true, "food over the keep"
	default:
		p.Why = "nothing over the keep is worth the walk"
	}
	return p
}

// DueInput is the moment a deposit trip may start.
type DueInput struct {
	Enabled bool
	// TimerDue is the persisted, jittered per-account timer (scarabplan.MuleAskGate) for the
	// "consolidate" purpose.
	TimerDue bool
	// Stage is the ScarabSwarm stage; only a Stage 5 account deposits (Stage 3 is still buying and
	// training with what it holds, Stage 6 lives in the lair).
	Stage int
	// SafePlace is the account on the surface on the mule's side of White Wolf Mountain, outside
	// the Wilderness (the walk to the post is a land walk).
	SafePlace  bool
	TripActive bool
	InCombat   bool
	Hurt       bool
	HP, MaxHP  int
	// MuleHeard and SameWorld are the mule's heartbeat on this world.
	MuleHeard bool
	SameWorld bool
	// Errand is a world errand able to carry the trip to the mule's world (the account has a home
	// world and the mule's world is known): then another world is no reason not to go.
	Errand bool
	// Queue is the mule's queue ahead (donorrunner.MuleQueue); QueueKnown false means no reading.
	Queue      int
	QueueKnown bool
	// Fresh is the one fresh-start trip (FreshStartOwed): a Stage 3 account deposits too.
	Fresh bool
}

// Due reports whether a deposit trip may start now, and why not when it may not.
func Due(s Settings, in DueInput) (bool, string) {
	s = s.Normalised()
	switch {
	case !in.Enabled:
		return false, "consolidateToMule is off"
	case !in.TimerDue:
		return false, "not due yet"
	case in.Stage != fleetplan.StageSurvivalCombat && !(in.Fresh && in.Stage == freshStartThieving):
		return false, "only a Stage 5 account deposits"
	case !in.SafePlace:
		return false, "not on the mule's side of the mountain"
	case in.TripActive, in.InCombat, in.Hurt:
		return false, "busy (a trip, a fight, or recently hurt)"
	case in.MaxHP > 0 && in.HP*4 < in.MaxHP*3:
		return false, "below three quarters of its hitpoints"
	case !in.MuleHeard:
		return false, "the mule is not heard on the bus"
	case !in.SameWorld && !in.Errand:
		return false, "the mule is on another world and no world errand can carry the trip"
	case in.QueueKnown && in.Queue > s.MaxQueue:
		return false, "the mule's queue is too long"
	}
	return true, ""
}

// ---- the other half: even top-ups ----

// TopUpInput is one top-up decision for one item.
type TopUpInput struct {
	// Have is what the account holds; Keep is its keep (KeepFor) for this item.
	Have, Keep int
	// MuleStock is the mule's published count of the item; Cohort is how many accounts share it
	// (<= 0 means scarabplan.DefaultCohortShare).
	MuleStock, Cohort int
	// PerOrder caps one order (the mule's own per-order ceiling); 0 means no cap.
	PerOrder int
}

// TopUp is how many to ask the mule for: nothing while the account holds at least half its keep
// (the hysteresis band), else back up to the keep, never more than an equal split of the mule's
// spendable stock (scarabplan.FairShare: the sum of everyone's share is the stock by construction,
// which is what "redistribute evenly" means for a stockpile many accounts draw on) and never more
// than one order's ceiling.
func TopUp(in TopUpInput) int {
	if in.Keep <= 0 || in.Have*2 >= in.Keep {
		return 0
	}
	n := fleetplan.FairShare(in.Keep-in.Have, in.MuleStock, in.Cohort)
	if in.PerOrder > 0 && n > in.PerOrder {
		n = in.PerOrder
	}
	return max(0, n)
}

// ---- the chaos budget ----

// ChaosBudget is the third owner goal as arithmetic: the coins buying every chaos on a market's
// shelf would take, against the coins the fleet holds above its reserves.
type ChaosBudget struct {
	Stock, UnitPrice int
	// Cost is Stock x UnitPrice.
	Cost int
	// Spendable is the fleet's coins less every account's reserve.
	Spendable int
	// Covered is Spendable >= Cost; Shortfall is what is missing when it is not.
	Covered   bool
	Shortfall int
	// Days is how long the shelf lasts at perDay bought (0 when perDay is 0).
	Days float64
}

// Budget computes the chaos budget. reserve is kept per account, accounts is the fleet size,
// perDay the chaos the fleet has been buying a day.
func Budget(stock, unitPrice, fleetCoins, reserve, accounts, perDay int) ChaosBudget {
	b := ChaosBudget{Stock: max(0, stock), UnitPrice: max(0, unitPrice)}
	b.Cost = b.Stock * b.UnitPrice
	b.Spendable = max(0, fleetCoins-max(0, reserve)*max(0, accounts))
	b.Covered = b.Spendable >= b.Cost
	if !b.Covered {
		b.Shortfall = b.Cost - b.Spendable
	}
	if perDay > 0 {
		b.Days = float64(b.Stock) / float64(perDay)
	}
	return b
}

// ---- helpers ----

func kept(list []string, name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, k := range list {
		if strings.ToLower(strings.TrimSpace(k)) == n {
			return true
		}
	}
	return false
}

func keepOf(m map[string]int, name string) int {
	for k, v := range m {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return 0
}

func sum(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v > 0 {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func byCountDesc(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if m[out[i]] != m[out[j]] {
			return m[out[i]] > m[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}
