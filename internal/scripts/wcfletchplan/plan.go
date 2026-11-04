// Package wcfletchplan is ProgressiveWCFletch's pure half: which tree to chop when the ladder is
// driven by FLETCHING rather than woodcutting, which product each rung's logs become, and when a
// held log should be cut rather than carried. No client, actions, fleet or bot imports and no
// I/O.
//
// # What this script is, and why it is a SECOND script
//
// The fleet owner's ask, verbatim: "since we get fletching exp from the logs while making arrow
// shafts, before mule handover we could be cutting all forms of logs to the best (u)
// shortbow/longbow we can make provided we have a knife ... that at mule hand over we are handing
// over (u)'s ready for an account to string them at a high rate of fletch exp per hour. we should
// have 2 versions of this script, one that does no fletching at all and one that does fletching."
//
// So ProgressiveWoodcutter is left EXACTLY as it is and this is a separate registration. The two
// share every piece of machinery that is not about fletching — the tiers, the anchors, the axe
// ladder, the Draynor start camp, the bank, the events, the travel — by importing
// internal/scripts/woodcutplan and internal/scripts/woodcutrunner and calling them, never by
// forking them. What is genuinely new is in this package and in internal/scripts/wcfletchrunner:
// the ladder gate, the product choice, and a hand-over that sends bows.
//
// # The one real inversion
//
// ProgressiveWoodcutter climbs on WOODCUTTING level (woodcutplan.CurrentTierIndex). This script
// climbs on FLETCHING level: it stays on a tree until it can cut the NEXT tree's logs into a
// shortbow. Those are different questions about different skills on the same account, so the
// answer is the LOWER of the two rungs — a Fletching 50 account with Woodcutting 12 cannot chop a
// maple whatever its fletching says. TierIndex is that min, and TestTierIndexNeverExceedsTheAxe
// pins it.
//
// # Deliberately not ported / not built here, and why
//
//   - Stringing the bows. The whole point of handing (u) bows to the mule is that a DIFFERENT
//     account strings them; content's stringing rows are already in internal/bankxp (Via "Attach
//     a bow string") and want a bow string per bow, which a woodcutter has none of. Nothing here
//     pretends to string anything.
//   - Attaching arrowheads to the shafts. Same reason: it needs arrowtips this script never has.
//   - Any HUD/overlay/update-checker, matching every sibling plan package.
package wcfletchplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// ---- the tree gate ----

// TreeGate is which woodcutting requirement the tree choice is held to.
type TreeGate string

const (
	// GateStartAt is woodcutplan.Tier.StartAt — the fleet owner's own RAISED gate (oak 25,
	// willow 40, maple 60, yew 70, magic 85), deliberately above what content allows because
	// success chance per swing scales with the margin over the tree's requirement.
	//
	// THE DEFAULT, and the reason is the comparison this script exists for: users want half
	// the live woodcutters moved onto it and watched against the other half. If the two scripts
	// disagreed about which tree to stand at for a given Woodcutting level, the A/B would be
	// measuring two changes at once.
	GateStartAt TreeGate = "startAt"
	// GateContent is woodcutplan.Tier.Level, the server's own minimum. Offered because an
	// operator running this script on its own may want the fletching ladder to be the only thing
	// holding it back.
	GateContent TreeGate = "content"
)

// AxeRungIndex is the best rung the WOODCUTTING level allows under gate, capped at top.
func AxeRungIndex(woodLevel int, gate TreeGate, top int) int {
	if top > len(Ladder)-1 {
		top = len(Ladder) - 1
	}
	at := 0
	for i := 0; i <= top; i++ {
		need := Ladder[i].Tier.StartAt
		if gate == GateContent {
			need = Ladder[i].Tier.Level
		}
		if woodLevel >= need {
			at = i
		}
	}
	return at
}

// FletchRungIndex is the best rung the FLETCHING level has unlocked, capped at top: the highest
// rung whose own logs can be cut into a shortbow. Rung 0 is always allowed, because plain logs
// cut into arrow shafts from Fletching 1.
func FletchRungIndex(fletchLevel int, top int) int {
	if top > len(Ladder)-1 {
		top = len(Ladder) - 1
	}
	at := 0
	for i := 1; i <= top; i++ {
		if fletchLevel >= Ladder[i].EntryLevel() {
			at = i
		}
	}
	return at
}

// LadderSnapshot is what TierIndex reads.
type LadderSnapshot struct {
	// WoodLevel is the account's Woodcutting level.
	WoodLevel int
	// FletchLevel is the account's Fletching level.
	FletchLevel int
	// Target is the settings' top rung, by woodcutplan tier label.
	Target string
	// Gate is which woodcutting requirement to hold the tree choice to.
	Gate TreeGate
	// Fletching is false when this run is not fletching at all (no knife anywhere, or the
	// setting is off). The ladder then falls back to ProgressiveWoodcutter's own woodcutting
	// climb, because a fletching gate on a run that cannot fletch would pin the account to
	// normal trees for ever.
	Fletching bool
}

// TierIndex is the rung this account works right now: the LOWER of what its fletching has
// unlocked and what its woodcutting can actually chop, capped at the settings' target.
//
// The min is not a nicety. Either half alone is wrong in a way that presents as an account doing
// nothing: fletching alone would send a Woodcutting 12 account to a maple it cannot cut (an
// endless silent swing, CLAUDE.md's own OpLocU gotcha), and woodcutting alone is what
// ProgressiveWoodcutter already does and is the thing this script exists not to do.
func TierIndex(s LadderSnapshot) int {
	top := woodcutplan.TargetIndex(s.Target)
	axe := AxeRungIndex(s.WoodLevel, s.Gate, top)
	if !s.Fletching {
		return axe
	}
	fl := FletchRungIndex(s.FletchLevel, top)
	if fl < axe {
		return fl
	}
	return axe
}

// Tier is the woodcutplan tier TierIndex chose, for the chop/walk machinery.
func Tier(s LadderSnapshot) woodcutplan.Tier { return Ladder[TierIndex(s)].Tier }

// ---- handing the chosen rung back to woodcutplan ----

// EffectiveWoodLevel and EffectiveTarget are how this script's rung is handed to
// woodcutplan.DecideWoodPhase without forking it.
//
// DecideWoodPhase's Seers branch calls CurrentTier(s.Level, s.Target) for the walk/chop decision,
// and DecideStartPhase calls AtOakGate(s.Level, s.Target) for when the Draynor camp ends. Both
// read a WOODCUTTING level. Rather than copy those two functions and let them drift, this script
// hands DecideWoodPhase the pair that makes its own CurrentTier land exactly on the rung the
// fletching ladder chose: the chosen tier's own label as the target, and its own StartAt as the
// level. CurrentTierIndex then scans i in 0..TargetIndex(label) and returns the last i whose
// StartAt is met, which for that pair is the chosen rung and nothing else.
//
// TestEffectivePairRoundTripsThroughWoodcutplan asserts this against woodcutplan's real
// CurrentTierIndex for every rung, so a change on either side fails a test rather than silently
// moving an account to the wrong tree.
func EffectiveWoodLevel(idx int) int { return Ladder[idx].Tier.StartAt }

// EffectiveTarget is the chosen rung's own label.
func EffectiveTarget(idx int) string { return Ladder[idx].Tier.Label }

// ---- the product ----

// NormalPolicy is what the first rung's plain logs become.
type NormalPolicy string

const (
	// NormalShafts cuts plain logs into arrow shafts. THE DEFAULT, and it is the fleet owner's
	// own instruction ("normal logs only cut arrow shafts").
	//
	// It is worth writing down that content does NOT force this and the default therefore costs
	// something measurable: cut_logs.dbrow gives plain logs a shortbow at Fletching 5 for 5.0 xp
	// and a longbow at Fletching 10 for 10.0 xp, against 7.5 xp for the fifteen shafts
	// (`data=shafts,15`, and cut_logs.rs2 pays `multiply($shaft_count, 5)` tenths). So from
	// Fletching 10 a plain-log LONGBOW is worth a third more experience per log than shafts are.
	// Shafts win on two other counts the owner is optimising for: fifteen of them stack into one
	// slot where a bow takes a whole one (which is what lets the Draynor camp never bank at all),
	// and a normal-log bow is near-worthless to the stringer the hand-over is feeding. The
	// setting exists so the trade can be made the other way without a code change.
	NormalShafts NormalPolicy = "shafts"
	// NormalBows cuts plain logs into the best plain-log bow the level allows, falling back to
	// shafts below Fletching 5.
	NormalBows NormalPolicy = "bows"
)

// Choose is what this rung's logs should be cut into at this Fletching level.
//
// ok is false when nothing on the rung can be made — which TierIndex's own min makes
// unreachable in an ordinary run, and which a caller must still answer by BANKING the logs
// rather than by opening a menu it cannot use. See fact 1 in ladder.go's header: the menu would
// happily offer a product the account may not make, and the refusal is a chat message this
// script does not read.
//
// Within a rung the LONGBOW displaces the shortbow the moment the level allows it, which is the
// owner's "remember at everystage we upgrade cutting longbows when we can". That is always the
// right way round on experience: content gives the longbow more than the shortbow on every one
// of the six rungs, asserted by TestLongbowAlwaysBeatsShortbow.
func Choose(idx int, fletchLevel int, policy NormalPolicy) (Product, bool) {
	r := Ladder[idx]
	if idx == 0 && policy != NormalBows {
		if r.Shaft != nil && fletchLevel >= r.Shaft.Level {
			return *r.Shaft, true
		}
		return Product{}, false
	}
	if r.Longbow != nil && fletchLevel >= r.Longbow.Level {
		return *r.Longbow, true
	}
	if r.Shortbow != nil && fletchLevel >= r.Shortbow.Level {
		return *r.Shortbow, true
	}
	// Only reachable on rung 0 under NormalBows below Fletching 5.
	if r.Shaft != nil && fletchLevel >= r.Shaft.Level {
		return *r.Shaft, true
	}
	return Product{}, false
}

// ---- when to fletch ----

// FletchBatch is how many of the rung's logs to let build up before cutting them.
//
// One knife-on-logs use opens one make menu and batches the whole count, so a handful at a time
// costs one interaction rather than one per log. The same ten woodcutplan.FletchBatch already
// uses, duplicated rather than imported only so this package's own rule reads in one place.
const FletchBatch = 10

// Action is what the fletching half of a Loop pass should do.
type Action string

const (
	// ActNone: nothing to fletch right now — carry on with the ordinary woodcutting phase.
	ActNone Action = "none"
	// ActKnife: fetch a knife before anything can be cut.
	ActKnife Action = "knife"
	// ActFletch: cut the rung's held logs into Product.
	ActFletch Action = "fletch"
)

// FletchSnapshot is the live state Decide reads.
type FletchSnapshot struct {
	// Enabled is false when fletching is switched off, or has been given up for this run.
	Enabled bool
	// HasKnife is true when a Knife is in the pack.
	HasKnife bool
	// KnifeInReach is true when a knife ground spawn is close enough to be worth the detour —
	// the caller decides that, because it depends on where the account is.
	KnifeInReach bool
	// Product is what Choose picked; KindNone means this rung cannot be fletched at all.
	Product ProductKind
	// Logs is how many of the CURRENT rung's logs the pack holds.
	Logs int
	// PackFull is true when there is no free slot left.
	PackFull bool
	// Idle is true when the last chop found no tree to cut. This is the owner's own ask —
	// "while accounts are waiting for trees to respawn at the log-arrow shaft stage they can cut
	// their logs into arrow shafts" — and it is applied at EVERY rung, not only the first: idle
	// time is idle time, and a log cut now is a log that does not have to be cut later.
	Idle bool
	// Banking is true when the caller is about to make a bank trip, so everything fletchable
	// should be cut FIRST. This is what makes the hand-over a hand-over of bows: a log that
	// reaches the bank uncut is a log the mule receives as a log.
	Banking bool
	// Stacking is true when one fletch FREES PACK SPACE — which is only the arrow shaft, and
	// only because fifteen of them land in one slot (`data=shafts,15`). Every bow is one log to
	// one unstackable item, so cutting a pack of oak logs leaves the pack exactly as full as it
	// was.
	//
	// It is the whole difference between the two opportunistic triggers below. At the first rung
	// cutting a batch early is free throughput: it keeps the pack from filling, so the account
	// never has to stop. At every rung above it, cutting a batch early buys nothing and COSTS
	// chopping time, so there the cut waits for a moment the account was going to stop anyway.
	Stacking bool
}

// Decide picks the fletching half of one Loop pass.
//
// The no-knife answer is ActNone rather than a retry, and that is the "does something sensible
// rather than spinning" rule: with no knife and none in reach this returns ActNone every pass,
// the caller falls through to ProgressiveWoodcutter's own chop-and-bank, and the account earns
// woodcutting experience and banks logs exactly as the other half of the fleet does. Nothing
// blocks, nothing loops, and the hand-over sends logs instead of bows until a knife turns up.
func Decide(s FletchSnapshot) Action {
	if !s.Enabled || s.Product == KindNone {
		return ActNone
	}
	if !s.HasKnife {
		if s.KnifeInReach {
			return ActKnife
		}
		return ActNone
	}
	if s.Logs <= 0 {
		return ActNone
	}
	// A moment the account was going to stop anyway: the pack is full, there is no tree to cut,
	// or it is about to walk to the bank. Cutting now costs nothing at any rung.
	if s.PackFull || s.Idle || s.Banking {
		return ActFletch
	}
	// Cutting EARLY, mid-chop, only pays where the product stacks — see FletchSnapshot.Stacking.
	// At the first rung it keeps the pack from ever filling, which is what lets that rung run
	// without a single bank trip. Above it, one log makes one unstackable bow, so an early cut
	// frees no slot and the chopping is stopped for nothing.
	if s.Stacking && s.Logs >= FletchBatch {
		return ActFletch
	}
	return ActNone
}

// DescribeAction is the status line for an action.
func DescribeAction(a Action) string {
	switch a {
	case ActKnife:
		return "fetching a knife so logs can be fletched"
	case ActFletch:
		return "cutting logs"
	}
	return "not fletching"
}

// ---- the mule ----

// muleRules: the fletched stock goes first, then any log that was never cut; every axe and the
// KNIFE always stay.
//
// The knife is on the keep list and it is not a nicety — it is the one item whose loss stops this
// script being this script. There are four knife ground spawns in the world
// (woodcutplan.KnifeSpawns) and a cohort of 94 accounts sharing them, so a knife handed to the
// mule is not something the account gets back quickly.
func muleRules(keep []string) donorplan.Rules {
	full := append(append([]string{}, keep...), woodcutplan.Axes...)
	if !containsFold(full, Knife) {
		full = append(full, Knife)
	}
	// First is the FLETCHED stock and nothing else, which is where this script's hand-over
	// differs from ProgressiveWoodcutter's (whose First is the logs).
	//
	// Logs are deliberately NOT marked priority even though they are also wanted. donorplan's
	// First is a single boolean and both HandoffRows and PlanLoad resolve it as a stable
	// partition, so marking logs as well would put them level with the bows and let bank order
	// decide — which it did, and the first version of this sent 40 oak logs ahead of 120 oak
	// shortbows. Unmarked, the logs fall into the ordinary "everything else" group and still
	// cross, just after the bows. That is the right order: a partial hand-over should leave the
	// logs behind, not the stock the stringer is waiting for.
	return donorplan.Rules{
		Keep:  full,
		First: func(row donorplan.ItemRow) bool { return IsHandoverProduct(row.ID) },
	}
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

// MuleRows is what leaves the bank for the mule: the unstrung bows and arrow shafts first, then
// any uncut logs, then everything else the bank gathered that is not kept.
func MuleRows(bank []donorplan.ItemRow, keep []string) []donorplan.ItemRow {
	return donorplan.HandoffRows(bank, muleRules(keep))
}

// MuleHaul counts pack slots holding something for the mule.
func MuleHaul(pack []donorplan.ItemRow, keep []string) int {
	return donorplan.HaulOf(pack, muleRules(keep))
}

// MuleLoad is what goes on the trade screen: the fletched stock first, cut to the mule's free
// slots.
func MuleLoad(pack []donorplan.ItemRow, freeSlots int, keep []string) []donorplan.LoadGroup {
	return donorplan.PlanLoad(pack, freeSlots, muleRules(keep))
}

// HandoverStock counts the fletched units (unstrung bows and arrow shafts) among rows, BY ID.
//
// This is what muleAt is measured against for this script, where ProgressiveWoodcutter measures
// target-species logs — the two scripts hand over different things, so the same threshold number
// means a different quantity. See Settings.MuleAt.
func HandoverStock(rows []donorplan.ItemRow) int {
	n := 0
	for _, r := range rows {
		if !IsHandoverProduct(r.ID) {
			continue
		}
		c := r.Count
		if c < 1 {
			c = 1
		}
		n += c
	}
	return n
}

// MuleDue reports whether the hand-over is due: fletched stock in the bank and the pack together,
// against the threshold and the retry cooldown.
func MuleDue(bankStock, packStock, threshold int, now, nextAllowedAt time.Time) bool {
	held := bankStock
	if held < 0 {
		held = 0
	}
	if packStock > 0 {
		held += packStock
	}
	return donorplan.HandoffDue(held, threshold, now, nextAllowedAt)
}

// ---- settings ----

// Settings are ProgressiveWCFletch's settings: every ProgressiveWoodcutter key unchanged (so an
// account's settings object can be moved between the two scripts without editing), plus this
// script's own.
type Settings struct {
	// Wood is the whole of ProgressiveWoodcutter's own settings, read from the identical JSON
	// keys. Embedded rather than restated so a new knob on that script reaches this one.
	Wood woodcutplan.Settings

	// Fletch is the master switch. OFF makes this script behave as ProgressiveWoodcutter does,
	// which is what makes it safe to flip an account back without moving it between scripts.
	Fletch bool `json:"fletch"`
	// NormalLogs is what the first rung's plain logs become: "shafts" (the default and the
	// owner's instruction) or "bows". See NormalPolicy for what the default costs.
	NormalLogs NormalPolicy `json:"normalLogs"`
	// TreeGate is which woodcutting requirement the tree choice is held to: "startAt" (the
	// default, and what ProgressiveWoodcutter uses) or "content".
	TreeGate TreeGate `json:"treeGate"`
	// MuleAt is the FLETCHED UNITS (unstrung bows plus arrow shafts) held in the bank and the
	// pack together that start a hand-over. It overrides Wood.MuleAt for this script, and the
	// default is deliberately different: Wood.MuleAt is 1000 LOGS, and one log makes one bow but
	// fifteen arrow shafts, so the same number would mean wildly different trip sizes at the
	// first rung and at every other one. Zero falls back to Wood.MuleAt.
	MuleAt int `json:"muleAtFletched"`
	// SpreadRungOne sends rung-1 (arrow-shaft) accounts to the least crowded of NormalPatches
	// instead of all to the ladder's own Seers camp. ON by default; OFF is the behaviour before
	// 2026-09-23, kept so an operator can take the change out without a rebuild and a harness can
	// measure both arms on one binary. See patches.go for why it exists.
	SpreadRungOne bool `json:"spreadRungOne"`
	// SpreadMaple seats maple-rung accounts over MapleCamps by the supply model instead of all
	// at the ladder's own Seers maple camp. ON by default; OFF is the behaviour before
	// 2026-09-25. See maplecamps.go for why it exists.
	SpreadMaple bool `json:"spreadMaple"`
	// MapleOverflow lets the maple board send an account to the willow camps (chopping willows
	// and cutting willow longbows) once the maple camps are crowded. ON by default; OFF keeps
	// every maple-rung account on maples, spread over the maple camps only. It does nothing
	// with SpreadMaple off.
	MapleOverflow bool `json:"mapleOverflow"`
	// SpreadYew seats yew-rung accounts over YewCamps by the supply model, counting the players they
	// see at each camp who are not this cohort's, instead of all at the ladder's own church camp
	// of three yews. ON by default; OFF is the behaviour before 2026-10-05. See yewcamps.go for why
	// it exists and campcrowd.go for what an account does with the players it sees.
	SpreadYew bool `json:"spreadYew"`
	// SpreadYewWorlds spreads the yew-rung accounts over the worlds in YewWorlds, so each world's eleven yews feed a
	// share of the cohort instead of all 23 queueing at one world's (yewworlds.go). ON by default; OFF keeps every yew
	// account on whatever world it is on (the homeWorld setting's), as before 2026-10-12. It does nothing with SpreadYew off.
	SpreadYewWorlds bool `json:"spreadYewWorlds"`
	// YewWorlds is the worlds the yew cohort is spread over, "1,2,3" by default (AdviceWorlds). A world the deployment
	// does not list is refused by the fleet and the account stays where it is; fewer than two worlds spreads nothing.
	YewWorlds string `json:"yewWorlds"`
	// SpreadMagic seats magic-rung accounts over MagicCamps by the supply model (the ladder's three trees west of the
	// Seers bank and the four in the Sorcerers' Tower garden), counting the players they see at each camp who are not
	// this cohort's, instead of all at the ladder's own three trees. ON by default; OFF is the behaviour before
	// 2026-10-13. See magiccamps.go.
	SpreadMagic bool `json:"spreadMagic"`
	// ChopManip makes a chopping account write a knife-on-log batch that the engine reads in the stale tick after each
	// swing, so the tree rolls every 3 ticks instead of 4 (internal/scripts/chopmanipplan; lab: +25% to +32% logs). It
	// needs a knife and a log the Make 1 button can use in the pack. The controller measures how late its batches reach
	// the engine and writes them that many ticks early, and never pauses or switches itself off. ON by default, for this
	// script only; OFF is the plain chop the script had before 2026-10-06.
	ChopManip bool `json:"chopManip"`
}

// DefaultMuleAt is the default hand-over threshold in fletched units.
//
// 1000, matching woodcutplan.MuleLogsAt, because from the OAK rung up the conversion is one log
// to one bow and the two scripts then fill their banks at the same rate — which is what the
// owner's side-by-side comparison wants. At the first rung it is reached fifteen times sooner in
// logs, which is correct: a stack of arrow shafts is one bank row however large it gets.
const DefaultMuleAt = woodcutplan.MuleLogsAt

// DefaultSettings are this script's defaults: ProgressiveWoodcutter's own, plus fletching on.
func DefaultSettings() Settings {
	return Settings{
		Wood:          woodcutplan.DefaultSettings(),
		Fletch:        true,
		NormalLogs:    NormalShafts,
		TreeGate:      GateStartAt,
		MuleAt:        DefaultMuleAt,
		SpreadRungOne: true,
		SpreadMaple:   true,
		MapleOverflow: true,
		SpreadYew:     true,
		ChopManip:     true,

		SpreadYewWorlds: true,
		YewWorlds:       DescribeWorldsCompact(AdviceWorlds),

		SpreadMagic: true,
	}
}

// Normalised applies this script's own fallbacks on top of woodcutplan's.
func (s Settings) Normalised() Settings {
	d := DefaultSettings()
	s.Wood = s.Wood.Normalised()
	switch NormalPolicy(strings.ToLower(strings.TrimSpace(string(s.NormalLogs)))) {
	case NormalBows:
		s.NormalLogs = NormalBows
	default:
		s.NormalLogs = NormalShafts
	}
	switch TreeGate(strings.ToLower(strings.TrimSpace(string(s.TreeGate)))) {
	case TreeGate(strings.ToLower(string(GateContent))):
		s.TreeGate = GateContent
	default:
		s.TreeGate = GateStartAt
	}
	if s.MuleAt <= 0 {
		s.MuleAt = s.Wood.MuleAt
	}
	if s.MuleAt <= 0 {
		s.MuleAt = d.MuleAt
	}
	if s.MuleAt > donorplan.MaxMuleAt {
		s.MuleAt = donorplan.MaxMuleAt
	}
	return s
}

// Donor is the hand-over settings, wired to this script's own fletched-unit threshold rather than
// woodcutplan's log one.
func (s Settings) Donor() donorplan.Settings {
	return donorplan.NewSettings(s.Wood.Mule, s.MuleAt, s.Wood.MuleKeep)
}

// LadderSnapshotFor builds the ladder question from live levels and these settings.
func (s Settings) LadderSnapshotFor(woodLevel, fletchLevel int, fletching bool) LadderSnapshot {
	return LadderSnapshot{
		WoodLevel:   woodLevel,
		FletchLevel: fletchLevel,
		Target:      s.Wood.Target,
		Gate:        s.TreeGate,
		Fletching:   fletching && s.Fletch,
	}
}
