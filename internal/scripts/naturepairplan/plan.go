// Package naturepairplan is NaturePair's own decisions, ported from rsb0t-scripts'
// naturePlan.ts and the runecraftPlan.ts helpers NaturePair.ts itself reuses
// (countNoted/countUnnoted/decideRunnerTrade). It is pure: no client, actions, fleet or bot
// imports and no I/O — food/rest/death-line logic is reused from the already-ported
// internal/scripts/fighterfood and internal/scripts/freshstart packages exactly the way
// NaturePair.ts itself imports fighterData.ts/fighterPlan.ts/freshStartPlan.ts rather than
// duplicating them.
//
// Honest scope note: this session ported the pure decision layer only (this package, fully
// tested) — the master/runner pair's own I/O half (internal/scripts/naturepairrunner, not yet
// written) and internal/bot/naturepair.go are NOT wired up, and nothing here has been proven
// against a live engine. This is deliberately not claimed as more than "Pure plan logic" per
// docs/script_porting_framework.md's own tiers — see docs/script_parity_status.md's NaturePair
// section for exactly why: a two real, coordinated accounts trading live (not just one account
// banking) is a substantially larger live-test surface than any of BoneBurier/CasketOpener/
// Alcher needed, and this session's remaining time went to those three's full wiring + live
// proof instead. A future session picking this up should start from this package (already
// real, tested decision logic) rather than from zero.
//
// Deliberately not ported at all, even at the plan layer: natureHud.ts/hudPanel.ts/bankFeed.ts/
// tallyFeed.ts/updateCheck.ts (no headless equivalent) and the BroadcastChannel wall-bus
// protocol itself (NATURE_CHANNEL) — a future naturepairrunner should use
// internal/mulebus.Bus (already proven by MuleReceiver, see internal/bot/mule.go), not attempt
// to reconstruct same-browser BroadcastChannel semantics.
package naturepairplan

import (
	"regexp"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// ---- constants (naturePlan.ts + runecraftPlan.ts) ----

const (
	Rune     = "Nature rune"
	Talisman = "Nature talisman"
	Level    = 44

	Essence = "Rune essence"
	// EssenceID is the unnoted essence's own id (`blankrune`) — the bank note shares the
	// display name and differs only by id, so counting/offering always picks on id, never name.
	EssenceID = 1436

	Ruins    = "Mysterious ruins"
	AltarOp  = "Craft-rune"
	PortalOp = "Use"
	BoothOp  = "Use-quickly"

	StoreNPC = "Jiminua"

	// TradeCap is essence per trade window; a pack holds 28 and the runner keeps a slot or two.
	TradeCap = 25
	// CoinsPerTrade is coins the master hands a runner with each batch.
	CoinsPerTrade = 100
	// MasterFareCoins is coins the master keeps back for its own restock trip.
	MasterFareCoins = 100
	// ShipFare is Captain Barnaby's ship, each way.
	ShipFare = 30
	// RunnerTripFare is a runner's food trip: two crossings.
	RunnerTripFare = ShipFare * 2
	// CoinsGraceMs: once the notes are up, how long a runner holds its accept for the coins.
	CoinsGraceMs = 5000
	// BuyOnlyStock: store stock above which the runner only buys, never sells notes into it.
	BuyOnlyStock = 30
	StorePasses  = 6
	// RunnerAskMs: runners re-ask on this interval; asking every tick cancels the open window.
	RunnerAskMs = 1800
	// MasterHandshakeMs: after Trade-with, the master does not click again for this long.
	MasterHandshakeMs = 3000
	PickupRange       = 20
	TempleZ           = 4000

	SignalTTLMs   = 60_000
	SignalEveryMs = 5000

	// DefaultBatch mirrors runecraftPlan.ts's own DEFAULT_BATCH.
	DefaultBatch = 25
	// NotesGraceMs: how long a runner holds a screen for notes it asked for before backing out.
	NotesGraceMs = 12_000
)

// RuinsTile is the Mysterious ruins on Karamja: altar entrance for the master, meeting point
// for both.
var RuinsTile = tile.Tile{X: 2865, Z: 3022, Level: 0}

// RunnerBank is Ardougne East, beside Captain Barnaby's pier.
var RunnerBank = tile.Tile{X: 2655, Z: 3283, Level: 0}

// Pier is Captain Barnaby's own pier tile.
var Pier = tile.Tile{X: 2683, Z: 3272, Level: 0}

// Store is Jiminua's Jungle Store, Tai Bwo Wannai.
var Store = tile.Tile{X: 2767, Z: 3122, Level: 0}

// SpiderSafe is the jungle-spider-safe hop between the store and the ruins.
var SpiderSafe = tile.Tile{X: 2790, Z: 3094, Level: 0}

// Box is an inclusive world-tile rectangle on one level.
type Box struct {
	MinX, MaxX, MinZ, MaxZ int
}

// WrongBoatZone is the corridor east of Barnaby's pier, kept out of every runner walk so a
// repath never boards the Port Sarim boat.
var WrongBoatZone = Box{MinX: 2780, MaxX: 3040, MinZ: 3130, MaxZ: 3330}

// Karamja is the island on the surface, Brimhaven down to the ruins; the temple counts as on
// it too (via InTemple, not this box).
var Karamja = Box{MinX: 2700, MaxX: 2960, MinZ: 2880, MaxZ: 3240}

// ---- master wait mode ----

type MasterWait string

const (
	WaitInside  MasterWait = "inside"
	WaitOutside MasterWait = "outside"
	WaitSignal  MasterWait = "signal"
)

var MasterWaitOptions = []string{"Inside, out on signal", "Inside", "Outside"}

func ParseMasterWait(label string) MasterWait {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "inside":
		return WaitInside
	case "outside":
		return WaitOutside
	default:
		return WaitSignal
	}
}

// SignalFresh reports whether a signal sent at sentAt is still within ttl of now. A nil sentAt
// is never fresh.
func SignalFresh(sentAt *int64, nowMs int64, ttlMs int64) bool {
	return sentAt != nil && nowMs-*sentAt < ttlMs
}

// ---- geometry ----

func IsNear(a, b tile.Tile, radius int) bool { return tile.Chebyshev(a, b) <= radius }

func InBox(t tile.Tile, b Box) bool {
	return t.X >= b.MinX && t.X <= b.MaxX && t.Z >= b.MinZ && t.Z <= b.MaxZ
}

// OnKaramja: on the island, temple included — anywhere a runner can reach the store and the
// ruins without a ship.
func OnKaramja(t *tile.Tile) bool {
	return t != nil && (InTemple(t) || InBox(*t, Karamja))
}

// InTemple: altar interiors sit far north of the surface map on this world.
func InTemple(t *tile.Tile) bool { return t != nil && t.Z > TempleZ }

// SpiderSafeVia reports whether a walk to the store or the ruins should stop at the
// spider-safe tile first.
func SpiderSafeVia(here *tile.Tile, dest tile.Tile) bool {
	if !IsNear(dest, Store, 3) && !IsNear(dest, RuinsTile, 3) {
		return false
	}
	if here != nil && IsNear(*here, dest, 3) {
		return false
	}
	return here == nil || !IsNear(*here, SpiderSafe, 3)
}

// NearerSafeSpot: with no food, the nearer of the two spider-free places.
func NearerSafeSpot(here tile.Tile) string {
	if tile.Chebyshev(here, Store) <= tile.Chebyshev(here, RuinsTile) {
		return "store"
	}
	return "temple"
}

// ---- essence counting (runecraftPlan.ts) ----

type ItemRow struct {
	Name  string
	ID    int
	Count int
}

func isEssence(r ItemRow) bool { return strings.EqualFold(strings.TrimSpace(r.Name), Essence) }

// CountUnnoted is essence that can go on an altar: the unnoted id only.
func CountUnnoted(rows []ItemRow) int {
	n := 0
	for _, r := range rows {
		if r.ID == EssenceID {
			c := r.Count
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// CountNoted is essence that can only be banked: same display name, any other id.
func CountNoted(rows []ItemRow) int {
	n := 0
	for _, r := range rows {
		if isEssence(r) && r.ID != EssenceID {
			c := r.Count
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// ---- the store ----

type StoreStep struct {
	Op string // "buy", "sell", or "done"
	N  int
}

// PlanStoreStep ports planStoreStep: fill up to a batch of unnoted essence, selling notes into
// the shop only when its stock cannot cover the top-up on its own.
func PlanStoreStep(stock, noted, unnoted int, batch int) StoreStep {
	if batch <= 0 {
		batch = TradeCap
	}
	need := batch - unnoted
	if need <= 0 {
		return StoreStep{Op: "done"}
	}
	if stock > BuyOnlyStock {
		return StoreStep{Op: "buy", N: need}
	}
	toSell := noted
	if need-stock < toSell {
		toSell = need - stock
	}
	if toSell < 0 {
		toSell = 0
	}
	if toSell > 0 {
		return StoreStep{Op: "sell", N: toSell}
	}
	if stock > 0 {
		n := need
		if stock < n {
			n = stock
		}
		return StoreStep{Op: "buy", N: n}
	}
	return StoreStep{Op: "done"}
}

// BatchToOffer is what goes up on one side of the trade screen: a batch, or everything held
// when that is less.
func BatchToOffer(held, batch int) (count int, all bool) {
	count = held
	if count > batch {
		count = batch
	}
	if count < 0 {
		count = 0
	}
	return count, held <= batch
}

// EssenceNeeded is the count a runner asks the master for over the bus: enough to bring what
// it will still be holding once the trade is done up to one batch, and not a single essence
// more.
func EssenceNeeded(batch, notedHeld, unnotedHeld int) int {
	keeping := unnotedHeld - batch
	if keeping < 0 {
		keeping = 0
	}
	need := batch - notedHeld - keeping
	if need < 0 {
		return 0
	}
	return need
}

// CoinsToOffer is the coins the master puts up beside the notes.
func CoinsToOffer(coins, perTrade int) int {
	n := coins
	if n > perTrade {
		n = perTrade
	}
	if n < 0 {
		return 0
	}
	return n
}

// MasterCoinTarget is coins a note stack needs on the master: one hand-out per batch, plus the
// master's own fares.
func MasterCoinTarget(noted, batch, perTrade int) int {
	trades := 0
	if batch > 0 && noted > 0 {
		trades = (noted + batch - 1) / batch
	}
	if perTrade < 0 {
		perTrade = 0
	}
	return trades*perTrade + MasterFareCoins
}

func MasterCoinsShort(coins, perTrade int) bool {
	return perTrade > 0 && coins < perTrade
}

// poisonLine ports fighterPlan.ts's own isPoisonLine — not yet in internal/scripts/fighterfood
// (only isDeathLine/shouldEat were needed there so far), so it lives here until a second
// script wants it too.
var poisonLine = regexp.MustCompile(`(?i)you have been poisoned`)

// IsPoisonLine reports whether text is the game's "you have been poisoned" message.
func IsPoisonLine(text string) bool { return poisonLine.MatchString(text) }

var antipoisonPattern = regexp.MustCompile(`(?i)antipoison`)

// AntipoisonFromStock is the first antipoison the shop lists, whatever dose form it stocks.
func AntipoisonFromStock(names []string) (string, bool) {
	for _, n := range names {
		if antipoisonPattern.MatchString(n) {
			return n, true
		}
	}
	return "", false
}

func IsAntipoison(name string) bool { return antipoisonPattern.MatchString(name) }

func FoodToWithdraw(held, carry int) int {
	if carry-held < 0 {
		return 0
	}
	return carry - held
}

// ---- litter ----

// eventHeld are inventory puzzles the runtime solves itself; they have no Drop and must be
// left alone.
var eventHeld = map[string]bool{"strange box": true, "lamp": true}

// IsLitter reports whether name is worth dropping: not kept, not an event-held puzzle item,
// not antipoison.
func IsLitter(name string, keep []string) bool {
	key := strings.ToLower(name)
	if eventHeld[key] {
		return false
	}
	if strings.TrimSpace(name) == "" {
		return true
	}
	for _, k := range keep {
		if strings.EqualFold(k, name) {
			return false
		}
	}
	return !IsAntipoison(name)
}

func IsDroppableLitter(name string, keep []string, actions []string) bool {
	if !IsLitter(name, keep) {
		return false
	}
	for _, a := range actions {
		if strings.EqualFold(a, "drop") {
			return true
		}
	}
	return false
}

// ---- the runner ----

type RunnerStage string

const (
	RunnerRecover  RunnerStage = "recover"
	RunnerCure     RunnerStage = "cure"
	RunnerEat      RunnerStage = "eat"
	RunnerRest     RunnerStage = "rest"
	RunnerStranded RunnerStage = "stranded"
	RunnerDeliver  RunnerStage = "deliver"
	RunnerReturn   RunnerStage = "return"
	RunnerUnnote   RunnerStage = "unnote"
	RunnerRestock  RunnerStage = "restock"
	RunnerCollect  RunnerStage = "collect"
)

type RunnerSnapshot struct {
	Dead     bool
	Poisoned bool
	CureHeld bool
	NeedEat  bool
	// Resting: rest hysteresis already applied by the caller (freshstart.ShouldRestWith).
	Resting     bool
	Food        int
	InTemple    bool
	Noted       int
	Unnoted     int
	Coins       int
	OnIsland    bool
	NearBank    bool
	FoodReserve int
}

// RunnerCanAffordTrip: a food trip needs the fares there and back, or just back when the
// runner is already at the bank.
func RunnerCanAffordTrip(coins int, nearBank bool) bool {
	if nearBank {
		return coins >= ShipFare
	}
	return coins >= RunnerTripFare
}

// DecideRunnerStage ports runnerStage: health first, then whatever is in the pack names the
// destination; the bank is only for food, and only when the coins left over cover the ships.
func DecideRunnerStage(s RunnerSnapshot) RunnerStage {
	if s.Dead {
		return RunnerRecover
	}
	if s.Poisoned && s.CureHeld {
		return RunnerCure
	}
	if s.NeedEat {
		return RunnerEat
	}
	if s.Resting && s.Food == 0 {
		return RunnerRest
	}
	if !s.OnIsland && s.Coins < ShipFare {
		return RunnerStranded
	}
	if s.Unnoted > 0 {
		return RunnerDeliver
	}
	if s.InTemple && s.Noted > 0 {
		return RunnerReturn
	}
	if s.Noted > 0 {
		return RunnerUnnote
	}
	if s.Food < s.FoodReserve && RunnerCanAffordTrip(s.Coins, s.NearBank) {
		return RunnerRestock
	}
	return RunnerCollect
}

// RunnerWaitsForCoins: once the master's notes are up, the runner holds its accept for the
// coins that follow — accepting early would be reset by the master's next offer.
func RunnerWaitsForCoins(theirCoins int, notesSeenAtMs *int64, nowMs int64) bool {
	if theirCoins > 0 {
		return false
	}
	return notesSeenAtMs == nil || nowMs-*notesSeenAtMs < CoinsGraceMs
}

func RunnerShouldRequestTrade(tradeActive bool, lastRequestAtMs, nowMs int64) bool {
	if tradeActive {
		return false
	}
	return nowMs-lastRequestAtMs >= RunnerAskMs
}

func RunnerMayLeaveAltar(tradeActive bool, unnoted int) bool {
	return !tradeActive && unnoted == 0
}

func DescribeRunnerStage(s RunnerStage) string {
	switch s {
	case RunnerRecover:
		return "recovering from a death"
	case RunnerCure:
		return "curing poison"
	case RunnerEat:
		return "eating"
	case RunnerRest:
		return "resting for health"
	case RunnerStranded:
		return "stranded off the island without the fare"
	case RunnerDeliver:
		return "delivering essence"
	case RunnerReturn:
		return "leaving the altar"
	case RunnerUnnote:
		return "un-noting at the store"
	case RunnerRestock:
		return "restocking at the bank"
	case RunnerCollect:
		return "collecting a batch from the master"
	default:
		return string(s)
	}
}

// ---- the master ----

type MasterStage string

const (
	MasterRecover    MasterStage = "recover"
	MasterNoTalisman MasterStage = "no-talisman"
	MasterCraft      MasterStage = "craft"
	MasterBank       MasterStage = "bank"
	MasterExit       MasterStage = "exit"
	MasterEnter      MasterStage = "enter"
	MasterStageWait  MasterStage = "wait"
)

type MasterSnapshot struct {
	Dead          bool
	HasTalisman   bool
	InTemple      bool
	Essence       int
	Noted         int
	CoinsShort    bool
	WaitMode      MasterWait
	BankTrips     bool
	Signal        bool
	RunnerVisible bool
	Resting       bool
}

// MasterNeedsBank: the master fetches more notes, or the coins to go with them, only when it
// has nothing left to craft.
func MasterNeedsBank(noted, essence int, bankTrips bool, coinsShort bool) bool {
	return bankTrips && essence == 0 && (noted == 0 || coinsShort)
}

func DecideMasterStage(s MasterSnapshot) MasterStage {
	if s.Dead {
		return MasterRecover
	}
	if !s.HasTalisman {
		return MasterNoTalisman
	}
	if s.InTemple && s.Essence > 0 {
		return MasterCraft
	}
	if MasterNeedsBank(s.Noted, s.Essence, s.BankTrips, s.CoinsShort) {
		return MasterBank
	}
	if s.Resting {
		if s.InTemple {
			return MasterStageWait
		}
		return MasterEnter
	}
	if s.InTemple {
		if s.WaitMode == WaitOutside {
			return MasterExit
		}
		if s.WaitMode == WaitSignal && s.Signal && s.Noted > 0 {
			return MasterExit
		}
		return MasterStageWait
	}
	if s.Essence > 0 || s.WaitMode == WaitInside {
		return MasterEnter
	}
	if s.WaitMode == WaitSignal && !s.Signal && !s.RunnerVisible {
		return MasterEnter
	}
	return MasterStageWait
}

// ---- trade decisions ----

type TradeActionKind string

const (
	ActionWaitHeader TradeActionKind = "wait-header"
	ActionDecline    TradeActionKind = "decline"
	ActionOffer      TradeActionKind = "offer"
	ActionAccept     TradeActionKind = "accept"
	ActionWait       TradeActionKind = "wait"
)

type TradeAction struct {
	Kind   TradeActionKind
	Reason string // decline reason, or wait's "why"
}

type MasterOffer string

const (
	OfferNotes MasterOffer = "notes"
	OfferCoins MasterOffer = "coins"
	OfferNone  MasterOffer = ""
)

type MasterOfferState struct {
	MyNotedUp     int
	MyCoinsUp     int
	NotedHeld     int
	CoinsHeld     int
	CoinsPerTrade int
	HandOut       int
}

// MasterNextOffer: the notes first, then the runner's coins, then nothing.
func MasterNextOffer(o MasterOfferState) MasterOffer {
	if o.HandOut <= 0 {
		return OfferNone
	}
	if o.MyNotedUp <= 0 {
		if o.NotedHeld > 0 {
			return OfferNotes
		}
		return OfferNone
	}
	if o.CoinsPerTrade > 0 && o.MyCoinsUp <= 0 && o.CoinsHeld > 0 {
		return OfferCoins
	}
	return OfferNone
}

func namesMatch(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func partnerMatches(header string, partners []string) bool {
	for _, p := range partners {
		if namesMatch(header, p) {
			return true
		}
	}
	return false
}

// DecideMasterTrade: the batch of notes and the coins for it go up, then accept.
func DecideMasterTrade(o MasterOfferState, partnerHeader *string, partners []string) TradeAction {
	if partnerHeader == nil {
		return TradeAction{Kind: ActionWaitHeader}
	}
	if !partnerMatches(*partnerHeader, partners) {
		return TradeAction{Kind: ActionDecline, Reason: *partnerHeader + " is not a configured runner"}
	}
	if MasterNextOffer(o) != OfferNone {
		return TradeAction{Kind: ActionOffer}
	}
	if o.MyNotedUp > 0 || o.HandOut <= 0 {
		return TradeAction{Kind: ActionAccept}
	}
	return TradeAction{Kind: ActionDecline, Reason: "out of noted essence"}
}

// DecideRunnerTrade ports runecraftPlan.ts's decideRunnerTrade, which NaturePair.ts's own
// runner side reuses directly rather than duplicating.
func DecideRunnerTrade(partnerHeader *string, crafter string, unnotedHeld, myOfferCount, theirNotedCount, needed int, waitedMs int64) TradeAction {
	if partnerHeader == nil {
		return TradeAction{Kind: ActionWaitHeader}
	}
	if !namesMatch(*partnerHeader, crafter) {
		return TradeAction{Kind: ActionDecline, Reason: *partnerHeader + " is not the crafter"}
	}
	if unnotedHeld > 0 && myOfferCount <= 0 {
		return TradeAction{Kind: ActionOffer}
	}
	if needed > 0 && theirNotedCount <= 0 {
		if waitedMs >= NotesGraceMs {
			return TradeAction{Kind: ActionDecline, Reason: "no notes offered in time — backing out to ask again"}
		}
		return TradeAction{Kind: ActionWait, Reason: "waiting for the noted essence"}
	}
	return TradeAction{Kind: ActionAccept}
}

// TradeWindowIsFor: true when the open trade header is the player just clicked (a blank header
// is still that click).
func TradeWindowIsFor(partnerHeader *string, clicked string) bool {
	if partnerHeader == nil || *partnerHeader == "" {
		return true
	}
	return namesMatch(*partnerHeader, clicked)
}

// MasterPickTradeTarget: only an ask newer than the last accept, from a runner in range,
// outside the handshake hold.
func MasterPickTradeTarget(asked *string, askedAtMs, lastAcceptAtMs int64, askedInRange bool, holdUntilMs, nowMs int64) *string {
	if nowMs < holdUntilMs {
		return nil
	}
	if asked == nil || !askedInRange {
		return nil
	}
	if askedAtMs <= lastAcceptAtMs {
		return nil
	}
	return asked
}

// ---- settings ----

// ParseAvoidRect reads "minX,maxX,minZ,maxZ" into a Box the walker must not enter, or
// (Box{},false) for blank or malformed input.
func ParseAvoidRect(raw string) (Box, bool) {
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return Box{}, false
	}
	var nums [4]int
	for i, p := range parts {
		n, ok := parseInt(strings.TrimSpace(p))
		if !ok {
			return Box{}, false
		}
		nums[i] = n
	}
	minX, maxX := nums[0], nums[1]
	if minX > maxX {
		minX, maxX = maxX, minX
	}
	minZ, maxZ := nums[2], nums[3]
	if minZ > maxZ {
		minZ, maxZ = maxZ, minZ
	}
	return Box{MinX: minX, MaxX: maxX, MinZ: minZ, MaxZ: maxZ}, true
}

func parseInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
	}
	if s == "" {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}
