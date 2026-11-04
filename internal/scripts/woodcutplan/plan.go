// Package woodcutplan is ProgressiveWoodcutter's pure half, ported from rsb0t-scripts'
// woodcutPlan.ts: the ladder of Seers trees, which phase the run is in, which axe to carry,
// and what goes to the mule. No client, actions, fleet or bot imports and no I/O — it is a
// straight port of woodcutPlan.ts's own exported functions, unit-tested with plain structs.
//
// Three facts from the game content shape all of it, carried over from woodcutPlan.ts's own
// doc comment:
//
// Axes have no Woodcutting requirement on this server (woodcutting_axe_checker's level checks
// are commented out, "no wc req in 2004"), so a level-1 account chops with a rune axe carried
// in the pack; the starter bronze axe is sold with the rest of the kit and the rune axe is
// never wielded (that needs Attack 41, not Woodcutting).
//
// Every tier grows within seventy tiles of the Seers bank, so the anchors below are all Seers
// tiles, magic grove included.
//
// The mule always delivers an order as notes, and a noted axe cannot chop — so the axe is
// ordered from the Seers bank, where it can be un-noted the moment it arrives, never from
// Lumbridge (which has no bank on this server at all).
//
// Deliberately not ported here, and why:
//   - woodcutHud.ts — the Electron wall's on-screen overlay; no headless equivalent, the same
//     boundary drawn around every other ported script's own HUD file.
//   - the update-checker and the bank/tally feed publishers — nothing in this project reads
//     them.
//   - the pink-skirt purchase ProgressiveCrafter/ProgressiveFisher make on the way to their
//     own destinations: ProgressiveWoodcutter.ts's own phase list has no 'skirt' phase and its
//     fund target is the plain fare (fareRemaining alone, never freshstart's FundTarget which
//     adds the skirt cost) — confirmed by reading the TS source, not assumed.
package woodcutplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/tile"
)

// ---- the ladder ----

// Tier is one rung of the woodcutting ladder: a tree species, the level it opens at, and where
// it grows.
type Tier struct {
	// Label is what the settings and the panel call it.
	Label string
	// Level is the Woodcutting level the CONTENT opens the tree at — the server's own
	// requirement, which nothing here may go below. Kept separate from StartAt on purpose: a
	// harness precondition ("is this account allowed to chop this at all?", cmd/woodcuttest's
	// own check) and the panel both want the real requirement, not our preference.
	Level int
	// StartAt is the level this ladder actually SWITCHES to the tier at, which the fleet owner
	// deliberately sets ABOVE Level. Starting a tree the moment it is legal trades real
	// logs-per-hour for the novelty of a higher tier: success chance per swing scales with the
	// margin between the account's level and the tree's requirement, so a barely-legal account
	// spends most of its swings failing and earns less than it would on the tier below. Oak 25 /
	// Willow 40 / Maple 60 / Yew 70 / Magic 85 are the owner's own numbers (2026-09-20).
	//
	// Invariant, pinned by TestStartAtNeverBelowTheContentRequirement: StartAt >= Level, always.
	// A StartAt below Level would hand CurrentTier a tree the server refuses to let the account
	// chop, which presents as an endless silent swing rather than as an error.
	StartAt int
	// Loc is the loc's display name.
	Loc string
	// Logs is the log item's display name.
	Logs string
	// XP is experience per log, the content's productexp over ten.
	XP float64
	// Anchor is where the trees stand.
	Anchor tile.Tile
	// Leash is how far from Anchor a tree may be and still be ours.
	Leash int
	// Note is one line of warning for the panel/log, when the tier has something worth saying.
	Note string
}

// ChopOp is the loc interaction that fells a tree.
const ChopOp = "Chop down"

// Seers is where the ladder lives — every tier's own anchor sits within DestinationRadius of
// this, the exact SEERS.bank tile ProgressiveCrafter.ts's craftStage.js also defines
// (2725,3493), duplicated here rather than imported across plan packages (see
// docs/script_porting_framework.md's own "duplicated, not shared" convention for small
// script-specific constants).
var Seers = tile.Tile{X: 2725, Z: 3493}

// SeersRadius is how close to Seers counts as arrived, matching freshstart.DestinationRadius.
const SeersRadius = freshstart.DestinationRadius

// DefaultMuleStand is where the hand-over trade happens unless the settings say otherwise: the
// Seers' Village bank stand, the same tile every sibling script defaults `muleAtTile` to.
//
// Duplicated from mulereceiverplan.SeersStand rather than imported, following the same
// "duplicated, not shared" convention Seers above already follows for small script-specific
// constants (docs/script_porting_framework.md). internal/bot's own
// TestWoodcutterDefaultMuleStandMatchesTheMuleReceiver asserts the two have not drifted, so the
// duplication cannot quietly go stale.
var DefaultMuleStand = tile.Tile{X: 2725, Z: 3491, Level: 0}

// TIERS is the ladder. The tree species, Level (the content's own requirement), Loc, Logs and
// XP are ported verbatim from woodcutPlan.ts's own TIERS table; StartAt is the fleet owner's
// own raised gate (see Tier.StartAt) and is NOT from the TS source.
//
// The anchors were checked against the owner's own description of where each tier should be
// chopped (2026-09-20) and already agreed, so none of them moved for it: willow 2711,3508 and
// maple 2727,3500 are both north of the Seers bank at 2725,3493, and yew 2709,3462 and magic
// 2693,3425 are both south of it. All six are validated against the real collision pack and loc
// index by cmd/woodanchortest.
var TIERS = []Tier{
	{Label: "Normal tree", Level: 1, StartAt: 1, Loc: "Tree", Logs: "Logs", XP: 25, Anchor: tile.Tile{X: 2730, Z: 3486}, Leash: 10},
	{Label: "Oak", Level: 15, StartAt: 25, Loc: "Oak", Logs: "Oak logs", XP: 37.5, Anchor: tile.Tile{X: 2724, Z: 3488}, Leash: 14},
	{Label: "Willow", Level: 30, StartAt: 40, Loc: "Willow", Logs: "Willow logs", XP: 67.5, Anchor: tile.Tile{X: 2711, Z: 3508}, Leash: 9},
	// The Maple anchor is five tiles north of where it used to sit. It was 2727,3496 until
	// 2026-09-18, a walkable tile inside a sealed 22-tile strip (x 2720-2729, z 3495-3497)
	// that the Seers bank cannot reach, so bank-to-maple planned as `unreachable` after the
	// full 445,667-node budget and the reverse gave up after 22 — the size of the pocket. The
	// maples themselves are not there at all: loc 1307 is placed at 2721,3501, 2727,3501 and
	// 2730,3501, plus 2733,3499 (m42_54.jm2), and each is 2x2. 2727,3500 stands against the
	// middle one, is in the main walkable component, and plans from the bank at cost 29.
	{Label: "Maple", Level: 45, StartAt: 60, Loc: "Maple tree", Logs: "Maple logs", XP: 100, Anchor: tile.Tile{X: 2727, Z: 3500}, Leash: 10},
	{Label: "Yew", Level: 60, StartAt: 70, Loc: "Yew", Logs: "Yew logs", XP: 175, Anchor: tile.Tile{X: 2709, Z: 3462}, Leash: 8, Note: "three yews behind the church, slow to grow back"},
	{Label: "Magic", Level: 75, StartAt: 85, Loc: "Magic tree", Logs: "Magic logs", XP: 250, Anchor: tile.Tile{X: 2693, Z: 3425}, Leash: 8, Note: "three trees 65 tiles south-west of the bank"},
}

// TargetOptions are the settings dropdown's option labels, in ladder order.
var TargetOptions = tierLabels()

func tierLabels() []string {
	out := make([]string, len(TIERS))
	for i, t := range TIERS {
		out[i] = t.Label
	}
	return out
}

// LogNames are every tier's log item name.
var LogNames = logNames()

func logNames() []string {
	out := make([]string, len(TIERS))
	for i, t := range TIERS {
		out[i] = t.Logs
	}
	return out
}

// DefaultTarget matches woodcutPlan.ts's own DEFAULT_TARGET.
const DefaultTarget = "Yew"

func same(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// TargetIndex is the target's place on the ladder; an unknown name falls back to DefaultTarget.
func TargetIndex(target string) int {
	for i, t := range TIERS {
		if same(target, t.Label) {
			return i
		}
	}
	for i, t := range TIERS {
		if t.Label == DefaultTarget {
			return i
		}
	}
	return 0
}

// TargetTier is the tier target names.
func TargetTier(target string) Tier {
	return TIERS[TargetIndex(target)]
}

// CurrentTierIndex is the best tree the level allows, stopping at target's own tier.
//
// Gated on StartAt (our raised gate), never on Level (the content's own requirement): see
// Tier.StartAt for why users want these later than the minimum. Since StartAt >= Level is
// an invariant, a tier this returns is always one the server would also allow.
func CurrentTierIndex(level int, target string) int {
	return CurrentTierIndexWith(level, target, false)
}

// CurrentTierIndexWith is CurrentTierIndex with the owner's rune-axe exemption.
//
// THE OWNER'S RULE, verbatim (2026-09-22): "accounts with 60+ woodcutting and a rune axe should
// be moving onto yew logs... i set the levels to move on higher on purpose but i did say it
// should only come in place if they dont have a rune axe."
//
// So StartAt is a gate for an account chopping with a SLOW axe. The raised gates exist because
// a low tier with a poor axe still out-earns a high tier the account can barely cut -- see
// Tier.StartAt. A rune axe removes that reason: it cuts the higher tier fast enough that the
// content requirement (Tier.Level) is the only honest gate left. With fastAxe set, Yew opens at
// 60 rather than 70 and Magic at 75 rather than 85.
//
// The invariant that makes this safe is already pinned: StartAt >= Level always
// (TestStartAtNeverBelowTheContentRequirement), so this can only ever move the account UP the
// ladder, never onto a tree the server would refuse.
//
// Deliberately a separate function rather than a change to CurrentTierIndex. internal/scripts/
// wcfletchplan chooses the log/product pair whose own CurrentTierIndex lands exactly on the
// rung its fletching ladder picked, and asserts that round trip in a test; silently re-gating
// the shared function would desync the two. ProgressiveWCFletch therefore keeps the old gates
// and only ProgressiveWoodcutter opts in -- which is what users asked for.
func CurrentTierIndexWith(level int, target string, fastAxe bool) int {
	top := TargetIndex(target)
	at := 0
	for i := 0; i <= top; i++ {
		gate := TIERS[i].StartAt
		if fastAxe {
			gate = TIERS[i].Level
		}
		if level >= gate {
			at = i
		}
	}
	return at
}

// CurrentTierWith is CurrentTier with the rune-axe exemption. See CurrentTierIndexWith.
func CurrentTierWith(level int, target string, fastAxe bool) Tier {
	return TIERS[CurrentTierIndexWith(level, target, fastAxe)]
}

// FastAxe is the axe that earns the exemption: the best one in the game, and the only one the
// owner named. Held in the pack or worn both count, exactly as WoodSnapshot.AxesHeld reports
// them, because content's own woodcut.rs2 counts an axe from the inventory and does not require
// it to be wielded (see internal/scripts/woodcutplan/axe.go's header).
const FastAxe = "Rune axe"

// FastAxeHeld reports whether one of axes is the rune axe.
func FastAxeHeld(axes []string) bool {
	for _, a := range axes {
		if strings.EqualFold(strings.TrimSpace(a), FastAxe) {
			return true
		}
	}
	return false
}

// CurrentTier is the tier the level currently works, stopping at target.
func CurrentTier(level int, target string) Tier {
	return TIERS[CurrentTierIndex(level, target)]
}

// AtTarget reports whether the level has reached target's own tier.
func AtTarget(level int, target string) bool {
	return CurrentTierIndex(level, target) == TargetIndex(target)
}

// NextTier is the tier above the one currently worked, or nil at the target.
func NextTier(level int, target string) *Tier {
	at := CurrentTierIndex(level, target)
	top := TargetIndex(target)
	if at < top {
		t := TIERS[at+1]
		return &t
	}
	return nil
}

// TierState is one ladder row's state for the panel/log.
type TierState string

// The ladder row states.
const (
	TierDone  TierState = "done"
	TierNow   TierState = "now"
	TierNext  TierState = "next"
	TierLater TierState = "later"
)

// LadderRow is one row of LadderView. Level and StartAt are both carried so a panel can show
// "Yew (needs 60, we start at 70)" rather than implying the raised gate is the game's own rule.
type LadderRow struct {
	Label   string
	Level   int
	StartAt int
	State   TierState
	Target  bool
}

// LadderView is the ladder up to target, as the panel/log would show it.
func LadderView(level int, target string) []LadderRow {
	at := CurrentTierIndex(level, target)
	top := TargetIndex(target)
	out := make([]LadderRow, 0, top+1)
	for i := 0; i <= top; i++ {
		state := TierLater
		switch {
		case i < at:
			state = TierDone
		case i == at:
			state = TierNow
		case i == at+1:
			state = TierNext
		}
		out = append(out, LadderRow{Label: TIERS[i].Label, Level: TIERS[i].Level, StartAt: TIERS[i].StartAt, State: state, Target: i == top})
	}
	return out
}

// AtCamp reports whether t is inside the patch of trees tier is chopped in.
func AtCamp(t tile.Tile, tier Tier) bool {
	return t.Level == tier.Anchor.Level && tile.Chebyshev(t, tier.Anchor) <= tier.Leash
}

// IsLogs reports whether name is one of LogNames.
func IsLogs(name string) bool {
	for _, l := range LogNames {
		if same(name, l) {
			return true
		}
	}
	return false
}

// PackLine is one pack or bank line: a name and how many.
type PackLine struct {
	Name  string
	Count int
}

// LogCount totals items named logs across items, counting by the log rather than the slot (a
// count of 0 or less on an unstackable item counts as one, matching every sibling plan
// package's own "count<1 counts as 1" convention).
func LogCount(items []PackLine, logs string) int {
	n := 0
	for _, it := range items {
		if same(it.Name, logs) {
			c := it.Count
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// ---- axes ----

// RuneAxe is the best axe this ladder ever needs.
const RuneAxe = "Rune axe"

// Axes are every axe this server has, worst to best. The game swings the best one held,
// whatever the level.
var Axes = []string{"Bronze axe", "Iron axe", "Steel axe", "Black axe", "Mithril axe", "Adamant axe", RuneAxe}

// IsAxe reports whether name is one of Axes.
func IsAxe(name string) bool {
	for _, a := range Axes {
		if same(name, a) {
			return true
		}
	}
	return false
}

// BestAxe is the best axe among names, or "" when there is none.
func BestAxe(names []string) string {
	for i := len(Axes) - 1; i >= 0; i-- {
		for _, n := range names {
			if same(n, Axes[i]) {
				return Axes[i]
			}
		}
	}
	return ""
}

func axeIndex(name string) int {
	for i, a := range Axes {
		if same(a, name) {
			return i
		}
	}
	return -1
}

// StoreKeep is what the Lumbridge store never sees: the coins that pay the boats, and a rune
// axe should a resumed account already carry one. The bronze axe IS sold — no rule stops a
// level-1 account using the rune axe instead.
var StoreKeep = []string{freshstart.Coins, RuneAxe}

// StartCampStoreKeep is StoreKeep for a run that is about to chop at the Draynor start camp:
// every axe, the KNIFE and the arrow shafts are all kept back.
//
// This is the one place the two starts genuinely disagree, and every item on the list is here
// because leaving it off is silent rather than loud.
//
//   - EVERY AXE, the starter bronze one included. The ordinary run sells it because the next
//     axe it will ever hold comes out of the Seers bank or the mule, both across two ship
//     fares. The start camp chops FIRST and travels afterwards, so selling the bronze axe
//     leaves it with nothing to chop with — and because DecideWoodPhase gates PhaseStart on
//     holding an axe, the camp would then be skipped entirely and without a word.
//   - THE KNIFE, and this one is not theoretical: it was measured on the lab engine,
//     2026-09-20, and it is an infinite loop. The knife spawn is in Lumbridge, so the account
//     picks the knife up while still inside freshstart.InLumbridge; the knife is then an
//     ordinary sellable item, so Sellable goes to 1, so DecideWoodPhase returns PhaseStore
//     (which is checked before the camp), so the account walks to the general store and sells
//     the knife for 2gp, so Sellable is 0, so the camp runs, so it has no knife, so it walks
//     back to the spawn... The log said "knife collected" / "sold the starter kit: 1 items for
//     2 gp" four times in 2m26s and the account never left Lumbridge.
//   - THE ARROW SHAFTS, for the same reason one step later: they are what the camp exists to
//     make, and a store trip that happened to catch the account inside Lumbridge would sell the
//     whole run's output for a gp each.
var StartCampStoreKeep = startCampStoreKeep()

func startCampStoreKeep() []string {
	out := append([]string{}, StoreKeep...)
	for _, a := range Axes {
		if !containsFold(out, a) {
			out = append(out, a)
		}
	}
	for _, extra := range []string{Knife, ArrowShaft} {
		if !containsFold(out, extra) {
			out = append(out, extra)
		}
	}
	return out
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if same(s, want) {
			return true
		}
	}
	return false
}

// StoreKeepFor is the store's keep list for a run with or without the start camp.
func StoreKeepFor(startCamp bool) []string {
	if startCamp {
		return StartCampStoreKeep
	}
	return StoreKeep
}

// ---- the phase ----

// WoodPhase is one step of the run.
type WoodPhase string

// wantsStartCamp is whether this account belongs at the Draynor start camp right now: the camp
// is switched on, it can actually chop, and it has not reached the oak gate yet.
//
// Holding an axe is part of the question and not an accident. With none, the fund/travel/bank/
// order path is the one that can fix that, and it needs the Seers bank or the mule -- so an
// axeless account must NOT be sent to a camp where neither exists.
func wantsStartCamp(s WoodSnapshot) bool {
	if !s.StartCamp || len(s.AxesHeld) == 0 || AtOakGate(campLevel(s), campTarget(s)) {
		return false
	}
	if s.Tile == nil {
		return false
	}
	// FROM THE SEERS SIDE IT MUST BE ABLE TO PAY ITS WAY, and only from there.
	//
	// A fresh account starts at Lumbridge and WALKS to Draynor -- no ship, no fare -- which is
	// why the camp deliberately comes before the fare on that road. From the Seers landmass the
	// same camp is across ship crossings, and an account that cannot buy the fare does not fail
	// loudly: it announces the camp and then stands still, because it cannot PLAN the route at
	// all. Measured, not feared -- cmd/woodkilltest -stage swapin put a real account at the
	// Catherby bank with an axe and no coins and it sat at 2811,3438 for 3m41s.
	//
	// This is live now because the fleet's converted fishers hold almost nothing: a sample on
	// 2026-09-21 found 0, 0 and 240 banked coins. They CAN get coins -- the mule hands them out
	// and they draw 150,000 for a rune axe -- so it is a "not yet" rather than a "never", and
	// an account below the gate with no fare keeps using the Seers ladder until it has one.
	if OnSeersSide(*s.Tile) {
		return s.Coins >= freshstart.FareRemaining(s.Tile, s.Fare)
	}
	return true
}

// KeepFareAtBank reports whether the ordinary bank trip must leave this account's coins in the
// pack rather than depositing them.
//
// It is wantsStartCamp WITHOUT the coin test, and that omission is the whole point: the coins
// are exactly what is being protected, so asking "does it already have the fare?" would keep
// them only once it no longer needed keeping.
//
// Measured live on 2026-09-22. The fare funding worked perfectly -- acct144's own log reads
// "opened banked caskets and got 760 coins toward the 60 needed for a start camp fare" and
// "my own caskets covered it" -- and then the very next bank trip deposited the lot, because
// bankStep's keep list was the axe and nothing else. Ninety-four seconds later the same
// account logged "drew 820 coins from my own bank toward a start camp fare (had 0 in hand)".
// Fund, bank, go broke, fund again: twenty-two accounts sat at Woodcutting 9-14 doing that
// while the Seers trees they were fighting over ran out, and only four of twenty-seven ever
// reached Draynor.
//
// This is the same shape as the axe coin leak fixed the night before -- an account banking the
// money it was just given and then asking for more -- and it is worth naming as a class: a
// script that funds a purchase in one step and spends it in another must protect the money in
// between, or the bank trip in the middle undoes it.
func KeepFareAtBank(s WoodSnapshot) bool {
	if !s.StartCamp || len(s.AxesHeld) == 0 || AtOakGate(campLevel(s), campTarget(s)) || s.Tile == nil {
		return false
	}
	return OnSeersSide(*s.Tile)
}

// NeedsCampFare reports that this account belongs at the start camp and the ONLY thing stopping
// it is the ship fare from the Seers landmass.
//
// Exists so the caller can go and get the money instead of quietly leaving the account on a
// ladder that starves it. The fleet owner's instruction (2026-09-21): "have them check the bank
// for coins, if no coins for travel, grab from the mule" -- which is the same order the axe
// purchase already uses, and for the same reason: the account's own bank is a few tiles away
// and the mule is 83.
//
// Deliberately narrow. It is false when the camp is off, when the account has no axe, when it
// has already reached the oak gate, and when it is NOT on the Seers side -- a fresh account
// walks to Draynor from Lumbridge with no fare at all, so there is nothing to fund there.
func NeedsCampFare(s WoodSnapshot) (int, bool) {
	if !s.StartCamp || len(s.AxesHeld) == 0 || AtOakGate(campLevel(s), campTarget(s)) || s.Tile == nil {
		return 0, false
	}
	if !OnSeersSide(*s.Tile) {
		return 0, false
	}
	need := freshstart.FareRemaining(s.Tile, s.Fare)
	if s.Coins >= need {
		return 0, false
	}
	return need, true
}

// The phases, ported 1:1 from woodcutPlan.ts's own WoodPhase union.
const (
	PhaseAccept   WoodPhase = "accept"
	PhaseTutorial WoodPhase = "tutorial"
	PhaseWait     WoodPhase = "wait"
	PhaseBright   WoodPhase = "bright"
	PhaseStore    WoodPhase = "store"
	// PhaseStart is the Draynor Manor start camp: chop dead trees and fletch every log into
	// arrow shafts, so an account below the oak gate never banks and never sails. NOT from the
	// TS source — see startcamp.go for the camp's own content evidence and DecideWoodPhase for
	// why the branch sits exactly where it does.
	PhaseStart  WoodPhase = "start"
	PhaseFund   WoodPhase = "fund"
	PhaseTravel WoodPhase = "travel"
	PhaseBank   WoodPhase = "bank"
	PhaseOrder  WoodPhase = "order"
	PhaseWalk   WoodPhase = "walk"
	PhaseChop   WoodPhase = "chop"
	PhaseStuck  WoodPhase = "stuck"
)

// WoodSnapshot is the live state DecideWoodPhase reads, ported 1:1 from WoodSnapshot.
type WoodSnapshot struct {
	MainModal int
	// Tile is nil while the position is not known yet.
	Tile   *tile.Tile
	Level  int
	Target string
	Coins  int
	Fare   int
	// Sellable is kit slots the Lumbridge store has not refused yet.
	Sellable int
	// Bright is false while the brightness still wants turning up to very bright.
	Bright bool
	// FastAxeTiers opts this account into the owner's rune-axe exemption: with a rune axe in
	// hand the ladder gates on the content requirement (Tier.Level) instead of the raised
	// StartAt, so Yew opens at 60 and Magic at 75. Set by ProgressiveWoodcutter only; see
	// CurrentTierIndexWith for why ProgressiveWCFletch must keep the raised gates.
	FastAxeTiers bool
	// AxesHeld are axes that can chop right now: in the pack as items, or worn.
	AxesHeld []string
	// RuneAxeNoted is true when a rune axe sits in the pack as a note — the mule's delivery,
	// which the bank turns back into an axe.
	RuneAxeNoted bool
	// BankAxes are axes the bank held at the last look; nil until the bank has been read.
	BankAxes []string
	BankRead bool
	PackFull bool
	// AxeUpgradeDue is set by the caller when a better axe than the one in hand can actually be
	// obtained right now -- see seersPhase. Computed outside this package because it needs the
	// account's coins and a market price, neither of which a pure planner should know.
	AxeUpgradeDue bool
	// HasMule is true when a mule is named and the wall channel is open.
	HasMule bool
	// StartCamp is true when this run is allowed to use the Draynor Manor start camp. The
	// caller turns it off for the rest of the run when the camp cannot be made to work (no
	// knife anywhere), so a failed camp degrades to the proven Seers ladder instead of
	// looping.
	StartCamp bool
	// CampLevel is the WOODCUTTING level the start camp's own oak gate is judged on, for a
	// caller whose Level above is not the account's real one. Zero means "use Level", so
	// ProgressiveWoodcutter, which sets only Level, is unchanged.
	//
	// It exists because ProgressiveWCFletch deliberately replaces Level with
	// wcfletchplan.EffectiveWoodLevel -- the level that makes woodcutplan's own CurrentTier land
	// on the rung the FLETCHING ladder chose -- and an account held at rung 0 by Fletching 1
	// therefore presented as Woodcutting 1 to every gate in this file. Live 2026-09-23, that
	// sent NINETEEN accounts of Woodcutting 28 to 61 to a camp their config says ends at 25,
	// by a four-leg sea voyage (Catherby -> Ardougne -> Brimhaven -> Musa -> Port Sarim ->
	// Draynor, ~7 minutes and 72 coins in fares), where twelve of them then deadlocked. It
	// also created the 60-coin fare demand that starved the mule of pickup capacity, and sent
	// accounts pickpocketing strangers to pay for it.
	CampLevel int
	// CampTarget is the ladder target the start camp's oak gate is judged against, for the same
	// caller and the same reason as CampLevel. Empty means "use Target".
	//
	// BOTH HALVES ARE NEEDED, and shipping only the level half proved it: AtOakGate answers
	// false for ANY target below oak, whatever the level ("a target that never reaches oak
	// never leaves"), and ProgressiveWCFletch substitutes the RUNG's label -- "Normal tree" for
	// an account held at rung 0. So on the build that carried CampLevel alone, the same
	// nineteen accounts of Woodcutting 31 to 61 walked back to the camp on the startup path,
	// measured live within four minutes of the roster filling. The rung's label is an artefact
	// of this script's tier machinery; the account's real target is what the owner set.
	CampTarget string
}

// campLevel is the Woodcutting level the start-camp gates judge, which is the caller's real one
// when it gave one and Level otherwise. See WoodSnapshot.CampLevel.
func campLevel(s WoodSnapshot) int {
	if s.CampLevel > 0 {
		return s.CampLevel
	}
	return s.Level
}

// campTarget is the ladder target the start-camp gates judge. See WoodSnapshot.CampTarget.
func campTarget(s WoodSnapshot) string {
	if s.CampTarget != "" {
		return s.CampTarget
	}
	return s.Target
}

// WantsRuneAxe reports there being no rune axe anywhere the account can reach it. Always false
// until the bank has been read at least once (BankRead), and false while a delivery is already
// noted in the pack (about to be un-noted at the next bank visit).
func WantsRuneAxe(s WoodSnapshot) bool {
	if !s.BankRead || s.RuneAxeNoted {
		return false
	}
	for _, a := range s.AxesHeld {
		if same(a, RuneAxe) {
			return false
		}
	}
	for _, a := range s.BankAxes {
		if same(a, RuneAxe) {
			return false
		}
	}
	return true
}

// DecideWoodPhase picks the next phase from live state alone, ported 1:1 from decideWoodPhase.
func DecideWoodPhase(s WoodSnapshot) WoodPhase {
	if s.MainModal == freshstart.DesignModal {
		return PhaseAccept
	}
	if s.Tile == nil {
		return PhaseWait
	}
	if freshstart.InTutorialIsland(*s.Tile) {
		return PhaseTutorial
	}
	if !s.Bright {
		return PhaseBright
	}
	if !inSeers(*s.Tile) {
		// ALREADY ON THE SEERS LANDMASS: walk, do not sail. This branch is the whole of the
		// "swap a running ProgressiveFisher account over to woodcutting" case, which nothing had
		// ever measured until 2026-09-21 and which turns out not to work without it.
		//
		// The fleet's fishers stand at the Catherby bank, 2809,3441. That is Chebyshev **83**
		// from Seers — four tiles outside SeersRadius — so every decision below treats it as a
		// brand-new account at the start of the world. freshstart.FareRemaining answers the FULL
		// fare there (Catherby is east of ArdougneSideMaxX and not on the Karamja side), so a
		// swapped account with no coins goes to PhaseFund, which pickpockets Men in the
		// LUMBRIDGE castle courtyard, most of the map away, before sailing a route it is already
		// on the far side of. MEASURED with a swapped account's real start state (a bronze axe,
		// no coins; cmd/woodkilltest -stage swapin) it did something else wrong for the same
		// reason: it set off for the DRAYNOR start camp, because PhaseStart also sits under
		// !inSeers. An account 83 tiles from its own bank does not need the fresh-start road at
		// all — it needs a walk, and every step of the Seers branch below already walks with
		// webwalk.
		// NARROWED 2026-09-21, and it reverses part of the decision recorded directly above.
		//
		// That measurement was right for what it tested: a swapped account with an axe and a
		// level does not need the fresh-start road, and walking it to Draynor was wasteful. It
		// was taken when twelve accounts chopped at Seers. The fleet owner then ramped to 94,
		// and the paragraph above this one had already predicted what would happen -- "the
		// Seers normal-tree camp holds 12 trees and feeds 2 accounts, so a cohort started at
		// Woodcutting 1 starves there and never reaches the oak gate".
		//
		// MEASURED on the live fleet within the hour: 81 accounts below the oak gate averaged
		// 122 Woodcutting xp while the 13 at or above it averaged 6,580 -- a 54x gap, and 31 of
		// the 81 had gained nothing at all. They were not short of an axe or a knife; they were
		// queueing for twelve trees.
		//
		// So the short-circuit still holds for an account that can USE the Seers ladder, and a
		// sub-oak account goes to the camp instead: Draynor has 56 dead trees in one leash, and
		// the camp is where the knife and the fletching live, so it never banks either. Above
		// the gate they come back and compete for oaks, which is cheap because an oak yields
		// several logs per cutting cycle rather than one (the fleet owner's own point).
		if OnSeersSide(*s.Tile) && !wantsStartCamp(s) {
			return seersPhase(s)
		}
		if freshstart.InLumbridge(*s.Tile) && s.Sellable > 0 {
			return PhaseStore
		}
		// The Draynor start camp, and it sits HERE for three reasons worth stating, because
		// every other position for it is wrong:
		//
		//   - After PhaseStore, so the starter kit is still sold for the coins the fare will
		//     later need. StartCampStoreKeep keeps the axe back; nothing else changes.
		//   - Before the fare/sail, because the whole point is not to sail yet: the Seers
		//     normal-tree camp holds 12 trees and feeds 2 accounts, so a cohort started at
		//     Woodcutting 1 starves there and never reaches the oak gate that would let it move
		//     to a camp that can feed it. Draynor has 56 dead trees inside the same leash.
		//   - Inside the !inSeers branch, so an account that has ALREADY travelled — a restart,
		//     or one displaced by a random event — is never walked back across the world. It
		//     stays on the proven Seers ladder whatever this setting says.
		//
		// Gated on actually holding an axe: with none, the existing fund/travel/bank/order path
		// is the one that can fix that, and it needs the Seers bank or the mule to do it.
		if wantsStartCamp(s) {
			return PhaseStart
		}
		if s.Coins < freshstart.FareRemaining(s.Tile, s.Fare) {
			return PhaseFund
		}
		return PhaseTravel
	}
	return seersPhase(s)
}

// seersPhase is the ladder half of DecideWoodPhase: bank, axe, walk, chop. Split out of it
// unchanged so the "already on the Seers landmass" branch above can reach the same decisions
// without the fresh-start road, rather than the two drifting apart.
func seersPhase(s WoodSnapshot) WoodPhase {
	if s.RuneAxeNoted || !s.BankRead {
		return PhaseBank
	}
	held := BestAxe(s.AxesHeld)
	banked := BestAxe(s.BankAxes)
	if held == "" {
		if banked != "" {
			return PhaseBank
		}
		if s.HasMule {
			return PhaseOrder
		}
		return PhaseStuck
	}
	if s.PackFull {
		return PhaseBank
	}
	if !same(held, RuneAxe) && banked != "" && same(banked, RuneAxe) {
		return PhaseBank
	}
	// A better axe can be BOUGHT even when none is banked, and until 2026-09-21 nothing here
	// ever went looking. PhaseOrder was reachable only from `held == ""` above, so an account
	// that owned any axe at all never checked the market again -- which is exactly what the
	// fleet owner saw: twelve woodcutters carrying the mithril axe the mule handed them while
	// the Seers market listed 94 rune axes.
	//
	// The decision itself is NOT made here. DecideAxeBuy already knows how to compare what is
	// held against the target and price the difference, but it needs coins and a price lookup
	// that this pure planner has no business holding, so the caller computes it and sets the
	// flag. This branch only has to sit AFTER the banked-rune check above, so a free axe in
	// the bank is always taken before anything is bought.
	if s.AxeUpgradeDue {
		return PhaseOrder
	}
	// FastAxeTiers is the owner's rune-axe exemption, set only by ProgressiveWoodcutter --
	// see CurrentTierIndexWith for why ProgressiveWCFletch must keep the raised gates.
	tier := CurrentTierWith(s.Level, s.Target, s.FastAxeTiers && FastAxeHeld(s.AxesHeld))
	if AtCamp(*s.Tile, tier) {
		return PhaseChop
	}
	return PhaseWalk
}

// inSeers reports whether t is close enough to the Seers bank to count as arrived — the same
// SeersRadius freshstart.Road{Destination: Seers} would use for ProgressiveCrafter's own
// identical arrival check.
func inSeers(t tile.Tile) bool {
	return t.Level == 0 && tile.Chebyshev(t, Seers) <= SeersRadius
}

// NearSeers exports inSeers for the bot adapter, which has to ask the same "is this account at
// its own bank" question outside the phase decision (see woodcutteraxe.go's fundRepairTrip).
func NearSeers(t tile.Tile) bool { return inSeers(t) }

// SeersSideRadius is how far from the Seers bank an account is still considered to be on the
// same landmass, i.e. able to WALK there rather than needing the fresh-start boats.
//
// 200 tiles, and each edge was checked rather than picked round:
//
//   - IN: the Catherby bank stand at 2809,3441 (Chebyshev 83), which is where the fleet's own
//     ProgressiveFisher accounts stand and therefore the case this exists for; the Ardougne
//     bank around 2655,3283 (210 — see below); the whole Seers/Camelot/Sinclair area.
//   - OUT: Lumbridge at 3222,3218 (Chebyshev 497), Draynor Manor at 3091,3345 (366) and the
//     whole Karamja box (MinX 2740, MaxZ 3260 — the nearest corner is 233 away and the ships
//     are genuinely needed). So a fresh account still takes the fresh-start road, and the
//     Draynor start camp still runs, exactly as before.
//
// Ardougne at 210 falls outside, which is deliberate: this is the narrow answer to a measured
// problem (an account four tiles outside the arrival radius being sent round the world), not a
// general "walk anywhere" rule. Widening it means claiming a walk this package has not proven.
const SeersSideRadius = 200

// OnSeersSide reports whether t is close enough to Seers to walk there — the same landmass, not
// across a ship route. Level 0 only: an account up a ladder or in a dungeon is not walking
// anywhere until it comes down, and the ladder's own steps are all ground level.
func OnSeersSide(t tile.Tile) bool {
	if t.Level != 0 {
		return false
	}
	if freshstart.OnKaramjaSide(t) {
		return false
	}
	return tile.Chebyshev(t, Seers) <= SeersSideRadius
}

// DescribeWoodPhase is the status line for a phase.
func DescribeWoodPhase(p WoodPhase) string {
	switch p {
	case PhaseAccept:
		return "accepting the appearance screen"
	case PhaseTutorial:
		return "skipping the tutorial"
	case PhaseWait:
		return "waiting for the world"
	case PhaseBright:
		return "turning the brightness up"
	case PhaseStore:
		return "selling the starter kit"
	case PhaseFund:
		return "pickpocketing for the fare"
	case PhaseStart:
		return "at the Draynor start camp, chopping dead trees and fletching shafts"
	case PhaseTravel:
		return "sailing to Ardougne, then Seers"
	case PhaseBank:
		return "at the Seers bank"
	case PhaseOrder:
		return "waiting for the mule to bring a rune axe"
	case PhaseWalk:
		return "walking to the trees"
	case PhaseChop:
		return "chopping"
	case PhaseStuck:
		return "no axe and no mule"
	}
	return string(p)
}

// AxeToKeep is the best axe in the pack or worn, which stays out of every deposit.
func AxeToKeep(held []string) string {
	return BestAxe(held)
}

// AxeToWithdraw is what the bank step draws out when no axe is in hand, or a better one is
// banked: the best axe in the bank, or "" when nothing there beats what is held.
func AxeToWithdraw(held, banked []string) string {
	inBank := BestAxe(banked)
	if inBank == "" {
		return ""
	}
	inHand := BestAxe(held)
	if inHand == "" || axeIndex(inBank) > axeIndex(inHand) {
		return inBank
	}
	return ""
}

// ---- settings ----

// Settings are ProgressiveWoodcutter's settings. The JSON keys are the TS manifest's, so an
// account's fleet.config.json settings object decodes straight into DefaultSettings().
type Settings struct {
	Target           string    `json:"target"`
	Fare             int       `json:"fare"`
	PickpocketTarget string    `json:"pickpocketTarget"`
	PickpocketTile   tile.Tile `json:"pickpocketTile"`
	RestBelowHP      int       `json:"restBelowHp"`
	GuideNPC         string    `json:"guideNpc"`
	SkipOption       string    `json:"skipOption"`
	// Mule is the MuleReceiver account; blank never hands anything over or asks for an axe.
	Mule string `json:"mule"`
	// MuleAt is the target-species logs held (bank + pack) that starts a hand-over.
	MuleAt   int    `json:"muleAt"`
	MuleKeep string `json:"muleKeep"`
	// MuleStand is where the hand-over trade happens: the tile the MuleReceiver posts at.
	//
	// THIS EXISTS BECAUSE IT WAS MISSING, and the omission was a deployment blocker rather
	// than a rough edge. `internal/bot/woodcutter.go` hardcoded `mulereceiverplan.SeersStand`
	// in the one place it mattered, while every sibling script — fisher.go, lootchestfisher.go,
	// advancedfighter.go — already read a `muleAtTile` setting with the same default. So an
	// operator could name the fleet's real mule with `"mule"` and still have this script walk
	// to a tile that mule has never stood on, with nothing in the log to say so. The default
	// matches the siblings; the fleet sets it.
	MuleStand tile.Tile `json:"muleAtTile"`
	LogDetail string    `json:"logDetail"`

	// StartCamp turns the Draynor Manor start camp on: below the oak gate, a fresh account
	// chops dead trees beside the manor and fletches every log into arrow shafts instead of
	// sailing to Seers. Defaults ON, and the reason is a measured one rather than a
	// preference — the Seers normal-tree camp holds 12 trees and feeds 2 accounts, so a
	// cohort started together at Woodcutting 1 shares it and never levels out of it. Only
	// ever applies to an account that has not reached Seers yet; see DecideWoodPhase.
	StartCamp bool `json:"startCamp"`

	// ---- buying a better axe ----
	//
	// AxeWanted is the axe to work toward. Any axe chops at its own speed from Woodcutting 1 on
	// this server (see axe.go's own header for the measured proof), so this is purely an
	// output-per-hour choice, not a level gate.
	AxeWanted string `json:"axeWanted"`
	// AxePrice is what the operator expects to pay per axe, in coins. The control panel's
	// Seers market panel shows the market's own last-traded figure, which is what this is for.
	// Zero never buys — deliberately, since there is nothing safe to offer without a price.
	AxePrice int `json:"axePrice"`
	// AxePrices is what to pay for EACH tier, keyed by axe name. AxePrice above is the top
	// tier's price and stays the one an operator normally sets; this is what stops a cheaper
	// axe being bought at the expensive one's price.
	//
	// It exists because that is exactly what happened. The fleet ran axePrice 150,000 and
	// axeBudget 165,000 -- right for a rune axe -- and those two numbers were the ONLY prices
	// the script knew, so a market with no rune axe left the account looping instead of
	// buying. Measured on the live market the same night: rune 150,000 over 98 trades,
	// ADAMANT 3,150, MITHRIL 600. An adamant axe is a fiftieth of the price and chops from
	// Woodcutting 1 like every other axe (woodcutplan/axe.go's own content research).
	//
	// The owner's instruction, and it is a safety property rather than a preference: "if
	// theres no rune axes, it should choose a lower tier available, and realise the price for
	// it, dont pay 150k for lower tier axes."
	AxePrices map[string]int `json:"axePrices"`
	// AxeBudget is a HARD ceiling on coins spent on one axe, whatever anyone asks or offers.
	// Zero never buys.
	AxeBudget int `json:"axeBudget"`
	// MarketStand is where the Seers market character stands.
	MarketStand tile.Tile `json:"marketStand"`
	// MarketSeller, when set, is the only account a market trade will be completed with.
	MarketSeller string `json:"marketSeller"`
	// BobStand is where Bob's Brilliant Axes is served from: the steel-and-below shop
	// fallback, and the ONLY place a broken axe can be repaired. Defaults to BobsAxes.
	BobStand tile.Tile `json:"bobStand"`
}

// MuleLogsAt is the target logs held at which the lot goes to the mule, matching
// woodcutPlan.ts's own MULE_LOGS_AT.
const MuleLogsAt = 1000

// DefaultMuleKeep matches woodcutPlan.ts's own DEFAULT_MULE_KEEP: the coins.
const DefaultMuleKeep = freshstart.Coins

// DefaultSettings are the manifest defaults. The mule defaults to Mulecon (the TS manifest's
// own default), unlike thieverplan's — a fresh woodcutter with no axe anywhere is otherwise
// permanently PhaseStuck, so the manifest ships a real default partner name.
func DefaultSettings() Settings {
	return Settings{
		Target:           DefaultTarget,
		Fare:             freshstart.DefaultFare,
		PickpocketTarget: freshstart.DefaultPickpocketTarget,
		PickpocketTile:   freshstart.PickpocketSpot,
		RestBelowHP:      40,
		GuideNPC:         freshstart.DefaultGuideNPC,
		SkipOption:       freshstart.DefaultSkipOption,
		Mule:             "Mulecon",
		MuleAt:           MuleLogsAt,
		MuleKeep:         DefaultMuleKeep,
		MuleStand:        DefaultMuleStand,
		LogDetail:        "Normal",
		// The Draynor start camp is ON by default. Unlike the axe buying below, whose
		// counterparty is a person and whose worst case is coins handed to the wrong account,
		// the worst case here is an account chopping at one camp rather than another — and
		// the measured alternative is a cohort permanently stuck at 6 xp/account/min.
		StartCamp: true,
		// Buying defaults to OFF: AxePrice and AxeBudget are both zero, so a run behaves
		// exactly as it did before unless an operator deliberately sets a price. That is the
		// safe default for a mechanism whose counterparty is a person and whose worst case is
		// handing coins to the wrong account.
		AxeWanted:   RuneAxe,
		MarketStand: Seers,
		// BobsAxes (3232,3203), read out of the map's own NPC spawn row — NOT
		// freshstart.StoreStand, which this defaulted to until 2026-09-20 and which is the
		// Lumbridge GENERAL store, a different building 44 tiles away that sells no axes and
		// repairs nothing. See woodcutplan/axe.go's BobsAxes for the evidence.
		BobStand: BobsAxes,
	}
}

// Normalised applies the TS setting readers' own fallbacks and bounds.
func (s Settings) Normalised() Settings {
	d := DefaultSettings()
	if strings.TrimSpace(s.Target) == "" {
		s.Target = d.Target
	}
	if strings.TrimSpace(s.GuideNPC) == "" {
		s.GuideNPC = d.GuideNPC
	}
	if strings.TrimSpace(s.SkipOption) == "" {
		s.SkipOption = d.SkipOption
	}
	if strings.TrimSpace(s.PickpocketTarget) == "" {
		s.PickpocketTarget = d.PickpocketTarget
	}
	s.GuideNPC, s.SkipOption, s.PickpocketTarget = strings.TrimSpace(s.GuideNPC), strings.TrimSpace(s.SkipOption), strings.TrimSpace(s.PickpocketTarget)
	if s.Fare < 0 {
		s.Fare = 0
	}
	if s.RestBelowHP < 5 {
		s.RestBelowHP = 5
	}
	if s.RestBelowHP > 90 {
		s.RestBelowHP = 90
	}
	if s.MuleAt < 0 {
		s.MuleAt = 0
	}
	if s.MuleAt > donorplan.MaxMuleAt {
		s.MuleAt = donorplan.MaxMuleAt
	}
	if strings.TrimSpace(s.AxeWanted) == "" || !IsAxe(s.AxeWanted) {
		s.AxeWanted = d.AxeWanted
	}
	s.MarketSeller = strings.TrimSpace(s.MarketSeller)
	if s.AxePrice < 0 {
		s.AxePrice = 0
	}
	if s.AxeBudget < 0 {
		s.AxeBudget = 0
	}
	// A budget below the price can never buy anything, and silently "helping" by raising the
	// budget would defeat the point of having a hard ceiling — so the PRICE is what gives way,
	// which fails closed (nothing is bought) rather than open (more is spent than allowed).
	if s.AxeBudget > 0 && s.AxePrice > s.AxeBudget {
		s.AxePrice = 0
	}
	if s.MuleStand == (tile.Tile{}) {
		s.MuleStand = d.MuleStand
	}
	if s.MarketStand == (tile.Tile{}) {
		s.MarketStand = d.MarketStand
	}
	if s.BobStand == (tile.Tile{}) {
		s.BobStand = d.BobStand
	}
	return s
}

// BuysAxes reports whether this settings actually permits buying an axe at all. Both a price and
// a budget are required: either one missing means nothing is bought, which is the default.
func (s Settings) BuysAxes() bool {
	return s.AxePrice > 0 && s.AxeBudget > 0 && s.AxePrice <= s.AxeBudget && IsAxe(s.AxeWanted)
}

// RestBelow is the health share to rest at, clamped 5% through 90%.
func (s Settings) RestBelow() float64 {
	f := float64(s.RestBelowHP) / 100
	if f < 0.05 {
		return 0.05
	}
	if f > 0.9 {
		return 0.9
	}
	return f
}

// Donor is the hand-over settings, wired to this settings' mule fields.
func (s Settings) Donor() donorplan.Settings {
	return donorplan.NewSettings(s.Mule, s.MuleAt, s.MuleKeep)
}

// ---- the mule ----

// muleRules: every axe stays whatever the keep list says (the run chops with one, and a spare
// is a spare); logs go first.
func muleRules(keep []string) donorplan.Rules {
	full := append(append([]string{}, keep...), Axes...)
	return donorplan.Rules{
		Keep:  full,
		First: func(row donorplan.ItemRow) bool { return IsLogs(row.Name) },
	}
}

// MuleRows is what leaves the Seers bank for the mule: every log of any type first, then
// everything else the bank has gathered that is not kept — including random-event items,
// which HandoffRows already knows how to leave out where the game refuses to trade them.
func MuleRows(bank []donorplan.ItemRow, keep []string) []donorplan.ItemRow {
	return donorplan.HandoffRows(bank, muleRules(keep))
}

// MuleHaul counts pack slots holding something for the mule.
func MuleHaul(pack []donorplan.ItemRow, keep []string) int {
	return donorplan.HaulOf(pack, muleRules(keep))
}

// MuleLoad is what goes on the trade screen: the logs first, cut to the mule's free slots.
func MuleLoad(pack []donorplan.ItemRow, freeSlots int, keep []string) []donorplan.LoadGroup {
	return donorplan.PlanLoad(pack, freeSlots, muleRules(keep))
}

// MuleDue reports whether the hand-over is due: target logs in the bank and the pack together,
// against the threshold.
func MuleDue(bankTargetLogs, packTargetLogs, threshold int, now, nextAllowedAt time.Time) bool {
	held := bankTargetLogs
	if held < 0 {
		held = 0
	}
	if packTargetLogs > 0 {
		held += packTargetLogs
	}
	return donorplan.HandoffDue(held, threshold, now, nextAllowedAt)
}

// ---- progress ----

// Progress reports how far the current tier is through its span, and the experience remaining
// to the next one.
func Progress(level, xp int) levels.Progress {
	return levels.LevelProgress(level, xp)
}

// ---- the fare ladder's third rung ----

// robMargin is how much closer a pickpocket target has to be than the mule before robbing it
// is the better move.
//
// The comparison is not really distance against distance. The mule's real cost is the QUEUE:
// the mule takes one trade partner at a time, and the fleet measured 1.51 trades a minute against
// 5.0 distinct donors a minute -- a 3.3x shortfall -- so a 60-coin ask is 83 tiles each way
// plus a wait behind the whole cohort. A Man two tiles away has no queue at all.
//
// So this deliberately does NOT try to price the queue. It requires the target to be plainly
// closer and leaves the queue as unpriced headroom in robbing's favour, which is the honest way
// to use a number nobody has measured per-account.
const robMargin = 2

// PreferRobbing reports whether a pickpocket target at toTarget tiles beats walking toMule
// tiles to the mule. Both distances are Chebyshev from where the account is standing.
//
// False when there is no target (a caller passes a distance beyond its own leash), and false
// when the two are close enough together that the walk is not the deciding factor -- there the
// mule's hand-out is the surer coin, since a Man carries 3-25 a pocket and can miss.
func PreferRobbing(toTarget, toMule int) bool {
	if toTarget < 0 || toMule < 0 {
		return false
	}
	return toTarget+robMargin < toMule
}

// DefaultAxePrices is what each axe last traded for on the server's own market, measured
// 2026-09-22 from completed trades: rune 150,000 over 98 trades, adamant 3,150 over 7, mithril
// 600 over 4. Steel, iron and bronze are absent deliberately -- the mule hands those out free
// (orderplan.AxeHandouts), so buying one is never the right move and an unpriced tier is simply
// never bought.
//
// These are defaults, not truth: a market moves. An operator's own axePrices override them
// entry by entry, and the control panel's PriceBook is the live figure to check against.
var DefaultAxePrices = map[string]int{
	RuneAxe:       150000,
	"Adamant axe": 3150,
	"Mithril axe": 600,
}

// canonicalAxe maps a name to the spelling in Axes, so a roster written "rune axe" matches.
func canonicalAxe(name string) string {
	for _, a := range Axes {
		if same(a, name) {
			return a
		}
	}
	return strings.TrimSpace(name)
}

// PriceOfAxe is what this account will pay for one axe, and whether it knows at all. An unknown
// price must never read as affordable -- that is what stops a tier nobody priced being bought
// at whatever the seller asks.
//
// Resolved at lookup rather than seeded into Settings, deliberately. DefaultAxePrices is a
// FALLBACK, not state: writing it into the struct made DefaultSettings() and a roster with no
// axe keys differ by that one field, which is the quiet drift settings.example.json's own drift
// test exists to catch. Precedence is the operator's own table, then the single axePrice for
// the axe it is about, then the measured defaults.
func (s Settings) PriceOfAxe(axe string) (int, bool) {
	name := canonicalAxe(axe)
	if p, ok := s.AxePrices[name]; ok && p > 0 {
		return p, true
	}
	for k, v := range s.AxePrices {
		if v > 0 && same(k, name) {
			return v, true
		}
	}
	if s.AxePrice > 0 && same(s.AxeWanted, name) {
		return s.AxePrice, true
	}
	if p, ok := DefaultAxePrices[name]; ok && p > 0 {
		return p, true
	}
	return 0, false
}

// MaxCoinsForAxe is the hard ceiling on ONE purchase: never more than the budget, and never
// more than a tenth above this tier's own price.
//
// The second half is the whole point. axeBudget is 165,000, sized for a rune axe, and it was
// the ceiling on EVERY purchase -- so a 600-coin mithril axe could have cost 165,000 if a
// seller asked. A ceiling that does not track the item is not a ceiling.
func (s Settings) MaxCoinsForAxe(axe string) int {
	price, ok := s.PriceOfAxe(axe)
	if !ok {
		return 0
	}
	ceiling := price + price/10
	if s.AxeBudget > 0 && s.AxeBudget < ceiling {
		return s.AxeBudget
	}
	return ceiling
}
