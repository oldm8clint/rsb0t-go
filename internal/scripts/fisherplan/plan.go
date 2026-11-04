// Package fisherplan is the Progressive Fisher's pure half: the ladder of tools, which phase the
// run is on, which fish to sell for the next tool, and what stays out of the bank and out of the
// mule's hands. Ported from rsb0t-scripts' fishPlan.ts.
//
// The ladder never touches bait or feathers: every rung is a tool and nothing else. The levels are
// the game content's own — the original TS file's comment notes the api's fishing methods carry
// none, and this port carries the same numbers over unchanged.
//
// It is pure: no client, actions, fleet or bot imports and no I/O. Fish, tools and NPCs are named
// strings; the first day (appearance, tutorial, boats) is freshstart's, shared with the crafter,
// and the mule hand-off wiring is donorplan's — this package only supplies the rules each takes
// (which names stay, which go first).
package fisherplan

import (
	"strings"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// ---- Catherby ----

// Names fishPlan.ts exports as plain constants: two shops and the spot every rung fishes.
const (
	FishingSpot = "Fishing spot"
	// Harry's Fishing Shop sells every tool on the ladder, and buys raw fish.
	Harry = "Harry"
	// Arhein's general store buys anything, at a worse price than Harry.
	Arhein = "Arhein"
)

// Layout is the Catherby places the fishing loop uses. Coordinates carried over unchanged from
// fishPlan.ts's CATHERBY_BANK/CATHERBY_CAMP/HARRY_STAND/ARHEIN_STAND.
type Layout struct {
	// Bank is the Catherby bank stand, the heart of the town.
	Bank tile.Tile
	// BankRadius: Seers' bank is 84 tiles off, so this cannot mistake one for the other.
	BankRadius int
	// Camp is the middle of the beach: every spot moves between 2836 and 2860 along it.
	Camp tile.Tile
	// SpotRange is how far from the camp a spot may be and still be ours.
	SpotRange int
	// HarryStand and ArheinStand are the tiles beside each shopkeeper's counter.
	HarryStand  tile.Tile
	ArheinStand tile.Tile
	// ApproachOrthogonal makes the fishing loop stand on a tile beside the spot, never on a
	// diagonal: the engine ignores a Net click from a diagonal tile (lab 2026-09-19, the
	// Fishing Guild pier: 2603,3416 against the spot at 2602,3415 — no movement, no animation,
	// no message, for as long as the click was repeated). False for Catherby, whose loop is
	// left exactly as it was; its shoreline has never shown the stall.
	ApproachOrthogonal bool
}

// Catherby returns the fishing camp's layout.
func Catherby() Layout {
	return Layout{
		Bank:        tile.Tile{X: 2809, Z: 3441},
		BankRadius:  60,
		Camp:        tile.Tile{X: 2845, Z: 3431},
		SpotRange:   20,
		HarryStand:  tile.Tile{X: 2833, Z: 3443},
		ArheinStand: tile.Tile{X: 2803, Z: 3430},
	}
}

// FundIsReallyJustWalkHome reports whether a PhaseFund decision should be treated as
// PhaseTravel instead: the account was told to go earn coins, but it is only displaced
// from its own bank and a plain walk already reaches it, so walking home is both cheaper
// and correct. bankReachable is the caller's own pack connectivity check.
//
// The !InCatherby guard is the load-bearing half, live-found 2026-09-18. DecideFishPhase
// returns PhaseFund (not the old dead-end PhaseStuck) for an account with no usable tool,
// no coins and nothing to sell, so it can pickpocket its way back. For an account standing
// AT the bank that is the right answer — but the bank is then trivially reachable, so
// without this guard the redirect fired, PhaseTravel's SailLeg reported "already on the
// destination side" and returned instantly, the phase recomputed to PhaseFund, and the
// account span in a hot loop at 0 XP/hour (production account "spare28", Fishing 55, at
// 2811,3441 — two tiles from the bank it was being told to travel to).
func FundIsReallyJustWalkHome(phase FishPhase, t *tile.Tile, bankReachable bool) bool {
	return phase == PhaseFund && t != nil && bankReachable && !InCatherby(*t)
}

// InCatherby reports whether t is within reach of the Catherby bank.
func InCatherby(t tile.Tile) bool {
	l := Catherby()
	return tile.Chebyshev(t, l.Bank) <= l.BankRadius
}

// ---- the ladder ----

// Rung is one step of the fishing ladder.
type Rung struct {
	Label string
	// Level is the fishing level the rung opens at.
	Level int
	Tool  string
	// ToolCost is what Harry asks for the tool.
	ToolCost int
	// Op is the spot's option; Pair is the other option that tells this kind of spot from its
	// neighbours (the same physical spot can offer more than one method).
	Op   string
	Pair string
}

// Rungs is the fishing ladder, in level order.
var Rungs = []Rung{
	{Label: "Small net", Level: 1, Tool: "Small fishing net", ToolCost: 5, Op: "Net", Pair: "Bait"},
	{Label: "Big net", Level: 16, Tool: "Big fishing net", ToolCost: 20, Op: "Net", Pair: "Harpoon"},
	{Label: "Harpoon", Level: 35, Tool: "Harpoon", ToolCost: 5, Op: "Harpoon", Pair: "Cage"},
	{Label: "Lobster cage", Level: 40, Tool: "Lobster pot", ToolCost: 20, Op: "Cage", Pair: "Harpoon"},
	{Label: "Harpoon sharks", Level: 76, Tool: "Harpoon", ToolCost: 5, Op: "Harpoon", Pair: "Net"},
}

// DefaultTarget is the target setting's default: fished through without a mule dependency.
const DefaultTarget = "Big net"

// TargetOptions are the target setting's choices, one per rung, in ladder order.
func TargetOptions() []string {
	out := make([]string, len(Rungs))
	for i, r := range Rungs {
		out[i] = r.Label
	}
	return out
}

// ToolNames are every tool the ladder uses, once each, in the order first reached (Harpoon is
// reused for sharks, so it appears once).
func ToolNames() []string {
	seen := make(map[string]bool, len(Rungs))
	var out []string
	for _, r := range Rungs {
		if !seen[r.Tool] {
			seen[r.Tool] = true
			out = append(out, r.Tool)
		}
	}
	return out
}

func hasName(list []string, name string) bool {
	for _, n := range list {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

// TargetIndex is the target's place on the ladder; an unknown name is the default.
func TargetIndex(target string) int {
	target, _ = PinnedTarget(target)
	want := strings.ToLower(strings.TrimSpace(target))
	for i, r := range Rungs {
		if strings.ToLower(r.Label) == want {
			return i
		}
	}
	for i, r := range Rungs {
		if r.Label == DefaultTarget {
			return i
		}
	}
	return 0
}

// SharkRung is the ladder's top rung, and SharkAutoLevel is the fishing level at which an
// account climbs to it WITHOUT anyone changing its settings.
//
// Users asked for this directly (2026-09-21): "I want them doing sharks at 90+
// automatically once they hit 90 fishing without me changing it, just how they go to the
// fishing guild at 68." FishingGuildEntryLevel is exactly that shape — a level the planner
// checks for itself — so this is the same idea one rung higher.
//
// Why it was needed: every account in the live roster carries target "Big net", and
// CurrentRungIndex stops AT the target. So 119 accounts at Fishing 76 or above were still
// netting, because the cap is a setting and nothing raised it as they levelled.
//
// 90 is the owner's number, not content's: the shark rung itself opens at 76
// (content/scripts/skill_fishing — Rungs' own Level field), so an account between 76 and 89
// could harpoon sharks and deliberately does not. If that gap should close, this constant is
// the one line to change.
const (
	SharkRung      = "Harpoon sharks"
	SharkAutoLevel = 90
)

// EffectiveTarget is the target the ladder actually uses: the account's own, raised to the
// shark rung once it has the level for it. Pure and separate from CurrentRungIndex so the
// rule can be tested on its own and read without unpicking the index arithmetic.
func EffectiveTarget(level int, target string) string {
	if _, pinned := PinnedTarget(target); pinned {
		return target
	}
	if level >= SharkAutoLevel {
		return SharkRung
	}
	return target
}

// pinMark is appended to a target that the shark climb must leave alone. A suffix rather than
// a second parameter because every planner function takes the target string and nothing else
// (twenty-odd call sites), and the pin is a property of the target the account was given.
const pinMark = " (pinned)"

// PinTarget marks target so EffectiveTarget never raises it to the shark rung, for an account
// that is fishing for what a lower rung catches rather than for the level. The owner's case
// (2026-10-04): big net at the Fishing Guild for its gems, caskets and oysters, which a shark
// harpoon never catches. Pinning a pinned target changes nothing.
func PinTarget(target string) string {
	if _, pinned := PinnedTarget(target); pinned {
		return target
	}
	return target + pinMark
}

// PinnedTarget is target without its pin, and whether it had one.
func PinnedTarget(target string) (string, bool) {
	if strings.HasSuffix(target, pinMark) {
		return strings.TrimSuffix(target, pinMark), true
	}
	return target, false
}

// CurrentRungIndex is the rung the level has earned, stopping at the target — or at the shark
// rung once the level alone has earned it, whatever the target says (see EffectiveTarget).
func CurrentRungIndex(level int, target string) int {
	top := TargetIndex(EffectiveTarget(level, target))
	at := 0
	for i := 0; i <= top; i++ {
		if level >= Rungs[i].Level {
			at = i
		}
	}
	return at
}

// CurrentRung is the rung at CurrentRungIndex.
func CurrentRung(level int, target string) Rung {
	return Rungs[CurrentRungIndex(level, target)]
}

// AtTarget reports whether the level has reached the target rung.
func AtTarget(level int, target string) bool {
	return CurrentRungIndex(level, target) == TargetIndex(target)
}

// NextRung is the next rung up to the target; ok is false at the target.
func NextRung(level int, target string) (Rung, bool) {
	at := CurrentRungIndex(level, target)
	top := TargetIndex(target)
	if at < top {
		return Rungs[at+1], true
	}
	return Rung{}, false
}

// ActiveRung is the best rung whose tool is owned — the current one, or a lower one while the
// upgrade is saved for. ok is false when none of the ladder's tools are owned at all.
func ActiveRung(level int, target string, owned []string) (Rung, bool) {
	for i := CurrentRungIndex(level, target); i >= 0; i-- {
		if hasName(owned, Rungs[i].Tool) {
			return Rungs[i], true
		}
	}
	return Rung{}, false
}

// RungState is how a ladder rung stands relative to the account's progress, for a status panel.
type RungState string

// The rung states.
const (
	RungDone  RungState = "done"
	RungNow   RungState = "now"
	RungNext  RungState = "next"
	RungLater RungState = "later"
)

// LadderStep is one rung as a panel would show it.
type LadderStep struct {
	Label  string
	Level  int
	State  RungState
	Target bool
}

// LadderView is the ladder up to the target, with each rung's state.
func LadderView(level int, target string) []LadderStep {
	at := CurrentRungIndex(level, target)
	top := TargetIndex(target)
	out := make([]LadderStep, 0, top+1)
	for i := 0; i <= top; i++ {
		var state RungState
		switch {
		case i < at:
			state = RungDone
		case i == at:
			state = RungNow
		case i == at+1:
			state = RungNext
		default:
			state = RungLater
		}
		out = append(out, LadderStep{Label: Rungs[i].Label, Level: Rungs[i].Level, State: state, Target: i == top})
	}
	return out
}

// ---- the fish ----

// FishValues are the raw fish item values (the shop's base price), the ones the ladder catches,
// keyed lower-case.
var FishValues = map[string]int{
	"raw shrimps":   5,
	"raw anchovies": 15,
	"raw mackerel":  17,
	"raw cod":       25,
	"raw bass":      120,
	"raw tuna":      100,
	"raw lobster":   150,
	"raw swordfish": 200,
	"raw shark":     300,
}

// IsRawFish reports whether name is one of the ladder's catches by its "raw " prefix, not by the
// FishValues table — an unlisted raw fish still counts (FishValue falls back to 5 for it).
func IsRawFish(name string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "raw ")
}

// FishValue is the shop's base price for name, 5 for anything not in the table.
func FishValue(name string) int {
	if v, ok := FishValues[strings.ToLower(strings.TrimSpace(name))]; ok {
		return v
	}
	return 5
}

// FishCount is the raw fish in a pack or a bank, counted by the fish rather than the slot.
func FishCount(items []freshstart.PackItem) int {
	n := 0
	for _, item := range items {
		if IsRawFish(item.Name) {
			c := item.Count
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// HarryPays is what Harry pays for one fish when he already holds stocked of them: 70% of its
// value, a point less for each he holds, never under 10%.
func HarryPays(value, stocked int) int {
	rate := 0.7 - 0.01*float64(stocked)
	if rate < 0.1 {
		rate = 0.1
	}
	return int(rate * float64(value))
}

// FishToSell is which fish to draw out and sell for need coins, the most valuable first — fewer
// fish for the same money, and fewer bank trips. Each fish takes a slot, so room caps the lot.
func FishToSell(need int, stock []freshstart.PackItem, room int) []freshstart.SellLine {
	totals := map[string]int{}
	var names []string
	for _, item := range stock {
		name := strings.TrimSpace(item.Name)
		if !IsRawFish(name) {
			continue
		}
		if _, seen := totals[name]; !seen {
			names = append(names, name)
		}
		c := item.Count
		if c < 1 {
			c = 1
		}
		totals[name] += c
	}
	sortByValueThenName(names)
	var out []freshstart.SellLine
	raised := 0
	slots := room
	if slots < 0 {
		slots = 0
	}
	for _, name := range names {
		count := totals[name]
		n := 0
		for n < count && raised < need && slots > 0 {
			raised += HarryPays(FishValue(name), n)
			n++
			slots--
		}
		if n > 0 {
			out = append(out, freshstart.SellLine{Name: name, Count: n})
		}
		if raised >= need || slots <= 0 {
			break
		}
	}
	return out
}

// sortByValueThenName orders names by FishValue descending, ties broken by name ascending — an
// insertion sort since the list is always small (nine fish at most).
func sortByValueThenName(names []string) {
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && lessValue(names[j], names[j-1]); j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
}

func lessValue(a, b string) bool {
	va, vb := FishValue(a), FishValue(b)
	if va != vb {
		return va > vb
	}
	return a < b
}

// ---- what stays ----

// StarterKeep is the starter kit that survives the Lumbridge store: fishing tools, the coins and any
// cooked food. Bait is sold.
//
// Why the food stays: the kit's one Shrimps and one Bread (content tutorial.rs2) sell for next to
// nothing and are the only food a new account has on the walk past the Giant bats to Catherby.
func StarterKeep() []string {
	keep := append([]string{freshstart.Coins}, append(ToolNames(), "Fishing rod", "Fly fishing rod")...)
	return append(keep, fighterfood.FoodOptions...)
}

// BankKeep is what stays in the pack at the Catherby bank: the tools being fished and bought for,
// and the coins only while they are about to be spent — fishing has no use for them. A nil rung is
// ignored, matching a caller that has no active or no next rung to report.
func BankKeep(coins bool, rungs ...*Rung) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	if coins {
		add(freshstart.Coins)
	}
	for _, r := range rungs {
		if r != nil {
			add(r.Tool)
		}
	}
	return out
}

// ---- the phase ----

// FishPhase is which leg of the fishing loop the run is on.
type FishPhase string

// The phases.
const (
	PhaseAccept   FishPhase = "accept"
	PhaseTutorial FishPhase = "tutorial"
	PhaseWait     FishPhase = "wait"
	PhaseBright   FishPhase = "bright"
	PhaseStore    FishPhase = "store"
	PhaseFund     FishPhase = "fund"
	PhaseTravel   FishPhase = "travel"
	PhaseBank     FishPhase = "bank"
	PhaseBuy      FishPhase = "buy"
	PhaseSell     FishPhase = "sell"
	PhaseFish     FishPhase = "fish"
	PhaseStuck    FishPhase = "stuck"
	// PhaseOpen opens banked caskets for their coins when the next tool is unaffordable.
	PhaseOpen FishPhase = "open"
	// PhaseAskCoins asks the mule for MuleCoinAsk coins when the bank and caskets fall short.
	PhaseAskCoins FishPhase = "askcoins"
	// PhaseRestock waits out Harry's restock away from his shop, with no tool to fish with.
	PhaseRestock FishPhase = "restock"
)

// MuleCoinAsk is how many coins a fisher short of its next tool asks the mule for: enough for
// any ladder tool at any stock-driven price, and the owner's own figure (2026-09-20).
const MuleCoinAsk = 500

// DeathFloatCoins is the coin float a donor keeps IN THE BANK as death-recovery money, rather
// than handing it to the mule with everything else.
//
// The distinction between banked and carried is the whole point. There was already a 500-coin
// reserve, but donorrunner holds it back at the TRADE SCREEN, so it lives in the donor's
// pocket — and a pocket is exactly what a death takes. An account that dies while fishing
// respawns with nothing, and the recovery it needs (walk to a bank, withdraw, pay a fare, buy
// a replacement tool) can only be funded from the bank.
//
// Measured on the live fleet before this existed: 189 of 269 accounts held ZERO banked coins
// and another 46 held under 500, so 87% could not have funded their own recovery.
//
// Kept whole rather than split because the bank interface has no arbitrary-amount withdrawal
// (bank.go's bankWithdrawX is unported, and AcquireTask.withdrawExact's batched 10/5/1 clicks
// cannot move thousands of coins). So the rule is at the row level: below the float, the coin
// row simply is not handed over and stays banked; above it, the surplus goes to the mule and
// the trade-screen reserve keeps the donor in travel money on the way back, which the next
// ordinary bank visit deposits (fisherplan.BankKeep already banks coins unless they are about
// to be spent). Steady state is therefore about this much banked while the account fishes,
// which is when it is at risk of dying.
const DeathFloatCoins = 500

// FishSnapshot is the live state DecideFishPhase reads.
type FishSnapshot struct {
	MainModal int
	// Tile is nil while the position is not known yet.
	Tile   *tile.Tile
	Level  int
	Target string
	// Coins in the pack, where they are kept.
	Coins int
	// BankCoins is nil until the bank has been read.
	BankCoins *int
	// HeldTools are ladder tools in the pack.
	HeldTools []string
	// BankTools is nil until the bank has been read; once read, it lists ladder tools present in
	// the bank (possibly none).
	BankTools []string
	FishHeld  int
	// FishBanked is nil until the bank has been read.
	FishBanked *int
	// Sellable counts starter-kit slots the Lumbridge store has not refused yet.
	Sellable int
	// BrightPending is true while the screen brightness still wants turning up to very bright.
	BrightPending bool
	PackFull      bool
	Fare          int
	// BankCaskets is the unopened caskets the last bank read saw; 0 before a read, and the
	// caller reports 0 while a previous opening made no progress, so it cannot loop.
	BankCaskets int
	// ShopOut is true while Harry recently had none of the current rung's tool to sell (live
	// 2026-09-20: 72 nets lost in 5s against a stock of 5 restocking one a minute). The caller
	// owns the retry timer; while it runs the account fishes with what it owns instead of
	// standing at the shop.
	ShopOut bool
	// MuleCoinsReady is true when asking the mule for MuleCoinAsk coins is allowed right now
	// (a mule is configured and no earlier ask is cooling down).
	MuleCoinsReady bool
	// Guild is true while the caller has selected the Fishing Guild (PlanFishingRoute's
	// AreaFishingGuild): the Guild then counts as home alongside Catherby, see InHome. False,
	// the default, is exactly the Catherby-only behaviour.
	Guild bool
}

// FishFundTarget is coins to raise before the boats: the fares still ahead, and a small net's
// price when the account holds no tool to fish with at all — it arrives in Catherby with nothing
// else.
func FishFundTarget(s FishSnapshot) int {
	target := freshstart.FareRemaining(s.Tile, s.Fare)
	if len(s.HeldTools) == 0 {
		target += Rungs[0].ToolCost
	}
	return target
}

// DecideFishPhase picks the phase from live state alone, so a relog anywhere resumes where it was.
func DecideFishPhase(s FishSnapshot) FishPhase {
	if s.MainModal == freshstart.DesignModal {
		return PhaseAccept
	}
	if s.Tile == nil {
		return PhaseWait
	}
	if freshstart.InTutorialIsland(*s.Tile) {
		return PhaseTutorial
	}
	if !InHome(s) {
		if freshstart.InLumbridge(*s.Tile) && s.Sellable > 0 {
			// A new account turns the brightness up before its first trip to the store.
			if s.BrightPending {
				return PhaseBright
			}
			return PhaseStore
		}
		if s.Coins < FishFundTarget(s) {
			return PhaseFund
		}
		return PhaseTravel
	}
	rung := CurrentRung(s.Level, s.Target)
	owned := make([]string, 0, len(s.HeldTools)+len(s.BankTools))
	owned = append(owned, s.HeldTools...)
	owned = append(owned, s.BankTools...)
	if !hasName(owned, rung.Tool) {
		// What the bank holds decides the rest: the tool may be in it, or the money for one.
		if s.BankTools == nil {
			return PhaseBank
		}
		bankCoins := 0
		if s.BankCoins != nil {
			bankCoins = *s.BankCoins
		}
		if s.Coins+bankCoins >= rung.ToolCost {
			if !s.ShopOut {
				if s.Coins >= rung.ToolCost && !s.PackFull {
					return PhaseBuy
				}
				return PhaseBank
			}
			// Harry has none right now: fish with the best tool owned until the retry, and
			// wait away from the shop only when there is nothing at all to fish with.
			if _, ok := ActiveRung(s.Level, s.Target, owned); !ok {
				return PhaseRestock
			}
		} else {
			// Short of the price. The owner's order (2026-09-20): coins already banked (above),
			// then banked caskets, then the mule, and only then selling the catch.
			if s.BankCaskets > 0 {
				return PhaseOpen
			}
			if s.MuleCoinsReady {
				return PhaseAskCoins
			}
			fishBanked := 0
			if s.FishBanked != nil {
				fishBanked = *s.FishBanked
			}
			if s.FishHeld+fishBanked > 0 {
				return PhaseSell
			}
			// Nothing to sell yet: fish with the best tool owned and try again once there is.
		}
	}
	active, ok := ActiveRung(s.Level, s.Target, owned)
	if !ok {
		// No usable tool held or banked, not enough coins for one (checked just above,
		// including the bank's own coins) and nothing to sell. Pickpocketing is the one
		// recovery that needs no starting capital, and PickpocketStep already walks
		// itself to its own spot from wherever the account happens to be (see
		// freshstart.PickpocketSpotFor/NoTargetAction) — so recover rather than
		// dead-ending.
		//
		// Live-found 2026-09-18, reported by the account owner: production account
		// "spare28" (Fishing 55 — a genuinely valuable account, not a fresh one) sat at
		// the Catherby bank logging "stuck — no Big fishing net, 0 coins and nothing to
		// sell; hand this account a small fishing net or a few coins" every 10 seconds
		// indefinitely. PhaseFund is only reachable from the !InCatherby(*s.Tile)
		// branch above — it was written for the first-day Lumbridge flow — so an
		// established account that loses its tool WHILE in Catherby had no path back to
		// it at all and idled until an operator noticed.
		return PhaseFund
	}
	// Coins in the pack are one slot and cost nothing while fishing: they are banked with everything else when the pack is full
	// (7 Oct 2026: a drop cycle's casket coins sent acct38 to the bank with an empty pack). The branches above already spend
	// them on the next tool when they cover it.
	if !hasName(s.HeldTools, active.Tool) || s.PackFull {
		return PhaseBank
	}
	return PhaseFish
}

// BankReason says why DecideFishPhase answered PhaseBank for s, for the log line every bank visit starts with. It is only
// meaningful for a snapshot whose phase is the bank.
func BankReason(s FishSnapshot) string {
	owned := append(append([]string{}, s.HeldTools...), s.BankTools...)
	switch {
	case s.PackFull:
		return "the pack is full"
	case s.BankTools == nil:
		return "the bank has not been read yet"
	}
	if active, ok := ActiveRung(s.Level, s.Target, owned); ok && !hasName(s.HeldTools, active.Tool) {
		return "the " + active.Tool + " is not in the pack"
	}
	rung := CurrentRung(s.Level, s.Target)
	if !hasName(owned, rung.Tool) {
		return "the next tool (" + rung.Tool + ") needs the bank's coins"
	}
	return "the phase asked for it"
}

// DescribeFishPhase is the status line for a phase.
func DescribeFishPhase(p FishPhase) string {
	switch p {
	case PhaseAccept:
		return "accepting the appearance screen"
	case PhaseTutorial:
		return "skipping the tutorial"
	case PhaseBright:
		return "turning the brightness up"
	case PhaseWait:
		return "waiting for the world"
	case PhaseStore:
		return "selling the starter kit"
	case PhaseFund:
		return "pickpocketing for the fare"
	case PhaseTravel:
		return "sailing to Catherby"
	case PhaseBank:
		return "at the Catherby bank"
	case PhaseBuy:
		return "buying the next tool"
	case PhaseSell:
		return "selling fish for the next tool"
	case PhaseFish:
		return "fishing"
	case PhaseStuck:
		return "no tool, no coins, no fish"
	case PhaseOpen:
		return "opening banked caskets for the next tool"
	case PhaseAskCoins:
		return "asking the mule for coins for the next tool"
	case PhaseRestock:
		return "waiting for Harry's restock"
	}
	return string(p)
}

// ---- the mule ----

// MuleFishAt is the fish banked at which, once the target is reached, the lot goes to the mule.
// MuleFishAt is how many fish must be banked before an account walks its haul to the mule.
//
// Doubled from 2,000 to 4,000 on the owner's instruction (2026-09-20) for every account at
// once, which works because no account in the live roster overrides muleAt — all 269 take
// this default. The measurement behind it: the mule is CAPACITY-bound, not scheduling-bound
// (84% busy, median gap to the next request 0s, 1.51 trades/min against 5.0 distinct
// donors/min, a 3.3x shortfall). Halving how often each donor arrives is the one lever that
// does not need a second mule, and the cost is carried by the donors' banks, not the mule's.
const MuleFishAt = 4000

// DefaultMuleKeep is the hand-over setting's default: nothing but the emergency coins stay.
const DefaultMuleKeep = freshstart.Coins

// NeverHandOver are items a fisher must not give the mule whatever its keep list says, beyond
// its own tools.
//
// A crystal key joined the list on 2026-09-20 with LootChestFisher. That script FISHES while it
// waits for the mule to make enough keys to be worth a trip to Taverley, so the same account is
// a ProgressiveFisher for long stretches with crystal keys in its bank — and the mule is where
// those keys came from. Without this the fisher's ordinary hand-off would carry them straight
// back, and the pair would trade the same keys in a circle instead of ever opening a chest. A
// fisher not running LootChestFisher never holds one (only the mule joins the halves), so the
// rule costs nothing anywhere else. Key HALVES are deliberately NOT here: they are exactly what
// the mule wants, since joining them is its job.
// THE PREMISE ABOVE BROKE, and the fleet owner found it on 2026-09-22. An ORDINARY fisher
// does join halves now -- internal/bot/fisher.go's joinHalvesBeforeHandoff runs right before
// every hand-off and logs "joined N key half pair(s) into crystal key(s) before handing over".
// acct118 did exactly that at 23:48:44, handed over three stacks at 23:53:20, and kept every
// key it had just made, because this list blocked them. Meanwhile the mule sat on 69 keys and
// spare104 could not start a chest run, which needs 100.
//
// So the rule is no longer free anywhere else: it strands keys on the very accounts that make
// them, and starves the account the keys exist for.
//
// It is NOT deleted, because the circle it prevents is real: LootChestFisher fishes as a plain
// ProgressiveFisher while it waits, holding keys the mule gave it, and handing those back would
// trade the same keys round and round. What changes is that keeping them is now a property of
// that ROLE rather than of every fisher -- fisherBot.keepCrystalKeys, set only by the
// LootChestFisher delegate -- so provenance decides, which is what the original comment was
// reaching for when it assumed only the mule ever joined halves.
var NeverHandOver = []string{"Crystal key"}

// muleRules are the hand-over rules: the tools and NeverHandOver are never handed over,
// whatever the keep list says — the run fishes with them — and raw fish go first.
func muleRules(keep []string) donorplan.Rules {
	// NeverHandOver is deliberately NOT appended here any more. A caller that must keep
	// crystal keys says so in its own keep list -- internal/bot/fisher.go does it when
	// keepCrystalKeys is set, which only the LootChestFisher delegate sets. See
	// NeverHandOver's own comment for the live incident that made this a role rather than a
	// blanket rule.
	k := make([]string, 0, len(keep)+len(ToolNames()))
	k = append(k, keep...)
	k = append(k, ToolNames()...)
	return donorplan.Rules{Keep: k, First: func(row donorplan.ItemRow) bool { return IsRawFish(row.Name) }}
}

// MuleRows is what leaves the Catherby bank for the mule: the fish first, then everything else not
// kept.
func MuleRows(bank []donorplan.ItemRow, keep []string) []donorplan.ItemRow {
	return donorplan.HandoffRows(bank, muleRules(keep))
}

// MuleHaul counts pack slots holding something for the mule.
func MuleHaul(pack []donorplan.ItemRow, keep []string) int {
	return donorplan.HaulOf(pack, muleRules(keep))
}

// MuleLoad is what goes on the trade screen: the fish first, cut to the mule's free slots.
func MuleLoad(pack []donorplan.ItemRow, freeSlots int, keep []string) []donorplan.LoadGroup {
	return donorplan.PlanLoad(pack, freeSlots, muleRules(keep))
}

// ---- long-distance recovery ----

// Road is Catherby's own freshstart.Road, for the identical stranded-across-water recovery
// barbarianplan/crafterplan already have (see their own Road doc comments) — a real, live gap
// this package never had covered until now. No WoolUntil: fishing needs no wool-selling leg the
// way crafting's own flax-to-bowstring first day does.
func Road() freshstart.Road {
	return freshstart.Road{Destination: Catherby().Bank, Radius: freshstart.DestinationRadius}
}

// ArrivedDescription describes Road's own destination loop for DescribePhase.
const ArrivedDescription = "back within reach of Catherby"

// DecidePhase is Catherby's own freshstart phase from live state, fixed to Road().
func DecidePhase(s freshstart.Snapshot) freshstart.Phase {
	return freshstart.DecidePhase(s, Road())
}

// DescribePhase is Catherby's own status line for a freshstart phase.
func DescribePhase(p freshstart.Phase) string {
	return freshstart.Describe(p, ArrivedDescription)
}
