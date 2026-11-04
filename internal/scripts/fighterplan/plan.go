// Package fighterplan is AdvancedFighter's own decisions, ported from rsb0t-scripts'
// fighterPlan.ts / the task list embedded in AdvancedFighter.ts. It is pure: no client,
// actions, fleet or bot imports and no I/O — see internal/scripts/essenceplan/plan.go's own
// doc comment for the convention this follows.
//
// Deliberately not ported here, and why:
//   - fighterHud.ts / hudPanel.ts — the Electron wall's on-screen overlay; no headless
//     equivalent (see docs/script_porting_framework.md's "never ported" list).
//   - prayer (prayerAction, PROTECT_PRAYERS/BOOST_PRAYERS) and potions (potionToSip,
//     plannedPotions, boostFaded) — this Go client has no Prayer scripting surface at all
//     (grepped: only gamestate/skills.go's stat-index table and health.go's own doc comment
//     mention the word "prayer", neither is an action). A future port can add this once a
//     Prayer primitive exists.
//   - ranged/mage (attackRangeFor beyond melee, castsAvailable, runesPerCast, spellLevel,
//     supplyThreshold) — melee only for this first port; AdvancedFighter.ts's own supplies()
//     returns null for melee, which is the only branch ported.
//   - treasure trails (clueX.solver, TrailLog, RewardWatch) — no headless clue-solver exists;
//     a clue scroll simply matches the ordinary loot filter and is banked like anything else.
//   - safespotting (usesSafespot, ReturnToSafespot, activeSafespot/fallback) — melee has no
//     safespot in the TS source either (usesSafespot() is false whenever style==='melee'), so
//     dropping it costs nothing for a melee-only port.
//   - teleport-flee (Game.teleport) — ThreatEscape's flee-to-bank branch is ported (BankReason
//     type "asked for" style urgency is not modelled; see fighterrunner's own ThreatEscape);
//     the teleport branch needs Game.teleport, not present in this client.
package fighterplan

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/tile"
)

// ---- targets ----

// TargetRecord is one candidate NPC, read once per pick — ported from fighterPlan.ts's
// TargetRecord.
type TargetRecord struct {
	Index        int
	Name         string
	Tile         tile.Tile
	Distance     int
	InCombat     bool
	Health       int
	TotalHealth  int
	TargetsMe    bool
	TargetsOther bool
}

// TargetContext is everything PickTarget needs beyond the candidate list — ported from
// fighterPlan.ts's TargetContext.
type TargetContext struct {
	Anchor tile.Tile
	Leash  int
	// OwnIndex is our current target's index, exempt from the "taken" test since its face
	// flickers too.
	OwnIndex int
	// HaveOwn is false when there is no held target (OwnIndex is meaningless then).
	HaveOwn bool
	// AttackOthersTargets engages NPCs already fighting other players.
	AttackOthersTargets bool
	// PreferAttackers scores NPCs already hitting us above everything else.
	PreferAttackers bool
	// SkipUntil holds indices to leave alone until the given millisecond timestamp.
	SkipUntil map[int]int64
	NowMs     int64
	// MaxDistance, when > 0, restricts to NPCs already within reach.
	MaxDistance int
	// Reachable, when set, answers whether there is a walkable route from where we are
	// standing to a candidate. A nil Reachable accepts everything, which is what every
	// caller without a collision pack gets and what the pure tests use.
	//
	// It is injected rather than computed here because this package is pure and owns no
	// collision data. The predicate is the whole reason this file no longer ranks by
	// Chebyshev alone: see PickTarget.
	Reachable func(TargetRecord) bool
}

// TakenByAnother reports whether an NPC already belongs to someone else's fight — ported
// from takenByAnother. An NPC's face target clears between its attacks, so "in combat but
// not with us" closes that gap; our own target is exempt because it flickers the same way.
func TakenByAnother(isOurs, inCombat, targetsMe, targetsOther bool) bool {
	if isOurs {
		return false
	}
	return targetsOther || (inCombat && !targetsMe)
}

// EligibleTarget ports eligibleTarget.
func EligibleTarget(t TargetRecord, ctx TargetContext) bool {
	if until, ok := ctx.SkipUntil[t.Index]; ok && until > ctx.NowMs {
		return false
	}
	if t.Tile.Level != ctx.Anchor.Level || tile.Chebyshev(t.Tile, ctx.Anchor) > ctx.Leash {
		return false
	}
	if ctx.MaxDistance > 0 && t.Distance > ctx.MaxDistance {
		return false
	}
	if t.TotalHealth > 0 && t.Health == 0 {
		return false
	}
	isOurs := ctx.HaveOwn && ctx.OwnIndex == t.Index
	if !ctx.AttackOthersTargets && TakenByAnother(isOurs, t.InCombat, t.TargetsMe, t.TargetsOther) {
		return false
	}
	return true
}

// ScoreTarget ports scoreTarget: lower is better — attackers first, then free NPCs, then
// the most worn down, then the nearest.
func ScoreTarget(t TargetRecord, ctx TargetContext) float64 {
	score := float64(t.Distance)
	if ctx.PreferAttackers && t.TargetsMe {
		score -= 1000
	}
	if ctx.HaveOwn && ctx.OwnIndex == t.Index {
		score -= 500
	}
	if t.InCombat && !t.TargetsMe {
		score += 50
	}
	if t.TotalHealth > 0 {
		score += (float64(t.Health) / float64(t.TotalHealth)) * 10
	}
	return score
}

// PickTarget ports pickTarget: the best-scoring eligible candidate this run can actually walk
// to, or ok=false when none qualifies.
//
// "Can actually walk to" is the part the TS source never had and this port needed. ScoreTarget
// ranks by `float64(t.Distance)`, which is plain Chebyshev, and Chebyshev 1 includes the tile
// on the far side of a wall. Both callers click the winner straight through OpNpc, and the
// server accepts an attack click whatever is in between, so nothing downstream ever reports the
// failure: the fight simply never starts and the same walled-in NPC wins the next pass, and the
// one after that. That is the bug that wedged three CombatTrainer windows before the same rule
// was applied there, and it is the fifth time in this repo that a short straight line has been
// taken as proof that two tiles touch.
//
// Candidates are ranked by score exactly as before and the best one WITH A ROUTE wins, so every
// existing preference (attackers first, our own target, free NPCs, the most worn down, then the
// nearest) is unchanged and the healthy case costs one bounded search. A nil ctx.Reachable
// keeps the old behaviour for callers that have no collision data to ask.
func PickTarget(list []TargetRecord, ctx TargetContext) (TargetRecord, bool) {
	eligible := make([]TargetRecord, 0, len(list))
	for _, t := range list {
		if EligibleTarget(t, ctx) {
			eligible = append(eligible, t)
		}
	}
	if len(eligible) == 0 {
		return TargetRecord{}, false
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		return ScoreTarget(eligible[i], ctx) < ScoreTarget(eligible[j], ctx)
	})
	if ctx.Reachable == nil {
		return eligible[0], true
	}
	for _, t := range eligible {
		if ctx.Reachable(t) {
			return t, true
		}
	}
	return TargetRecord{}, false
}

// EngagementStale ports engagementStale: whether the held target has gone quiet long enough
// that the attack needs re-issuing.
func EngagementStale(nowMs, engagedAtMs, reEngageMs int64) bool {
	return nowMs-engagedAtMs >= reEngageMs
}

// OtherPlayersNear ports otherPlayersNear.
func OtherPlayersNear(playerTiles []tile.Tile, anchor tile.Tile, radius int) int {
	n := 0
	for _, t := range playerTiles {
		if t.Level == anchor.Level && tile.Chebyshev(t, anchor) <= radius {
			n++
		}
	}
	return n
}

// ---- panic ----

// ShouldPanic ports shouldPanic: hp fraction below panicAt with no food left at all.
func ShouldPanic(hpFraction, panicAt float64, foodCount int) bool {
	return hpFraction < panicAt && foodCount == 0
}

// ---- loot ----

// LootFilter is what WantsLoot reads — ported from fighterPlan.ts's LootFilter.
type LootFilter struct {
	// Patterns are lower-cased "contains" patterns.
	Patterns  []string
	BuryBones bool
	Bones     []string
}

// MatchesAny ports matchesAny: name contains any of patterns, case-insensitive.
func MatchesAny(name string, patterns []string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	for _, p := range patterns {
		if p != "" && strings.Contains(n, p) {
			return true
		}
	}
	return false
}

// IsBone ports isBone: name equals (not just contains) one of bones, case-insensitive.
func IsBone(name string, bones []string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, b := range bones {
		if strings.ToLower(b) == n {
			return true
		}
	}
	return false
}

// WantsLoot ports wantsLoot: bones you asked to bury are wanted whatever the loot list
// says (ammo pickup is not ported — melee only, see the package doc comment).
func WantsLoot(name string, f LootFilter) bool {
	if name == "" {
		return false
	}
	if f.BuryBones && IsBone(name, f.Bones) {
		return true
	}
	return MatchesAny(name, f.Patterns)
}

// KillSite is where our own target died lately — ported from fighterPlan.ts's KillSite.
type KillSite struct {
	Tile tile.Tile
	AtMs int64
}

// BoneFromKill ports boneFromKill: bones are a kill's drop only when they lie where a
// target died within the window, allowing for the up-to-2-tile offset a multi-tile NPC's
// reported centre has from where its drops actually land (see the TS source's own comment
// on this, ported verbatim below).
//
// The client reports an NPC at the tile under its centre, which for a 2x2 or 3x3 NPC is one
// tile north-east of the south-west corner where drops land (two tiles for 4x4 and 5x5), so
// a drop up to two tiles south and west of the recorded tile is the kill's.
func BoneFromKill(dropTile tile.Tile, kills []KillSite, nowMs, windowMs int64) bool {
	for _, k := range kills {
		if nowMs-k.AtMs > windowMs || k.Tile.Level != dropTile.Level {
			continue
		}
		dx, dz := k.Tile.X-dropTile.X, k.Tile.Z-dropTile.Z
		if dx >= 0 && dx <= 2 && dz >= 0 && dz <= 2 {
			return true
		}
	}
	return false
}

// ---- banking ----

// KeepInput is what ShouldKeep reads — ported from fighterPlan.ts's KeepInput, minus the
// ammo/rune fields (melee only).
type KeepInput struct {
	Food      string
	Gear      []string
	Extra     []string
	BuryBones bool
	Bones     []string
}

// IsFoodItem matches fighterfood.IsFoodItem's own signature; injected so this package need
// not import internal/scripts/fighterfood's name-forms table twice — callers pass that
// function in directly (see fighterrunner's own call site).
type IsFoodItemFunc func(name, foodName string) bool

// ShouldKeep ports shouldKeep: whether a pack item stays through a deposit.
func ShouldKeep(name string, k KeepInput, isFoodItem IsFoodItemFunc) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	if isFoodItem(n, k.Food) {
		return true
	}
	if n == "coins" {
		return true
	}
	for _, g := range k.Gear {
		if strings.ToLower(g) == n {
			return true
		}
	}
	if k.BuryBones && IsBone(n, k.Bones) {
		return true
	}
	lower := make([]string, len(k.Extra))
	for i, e := range k.Extra {
		lower[i] = strings.ToLower(e)
	}
	return MatchesAny(n, lower)
}

// BankReason is why a bank trip is due — ported from fighterPlan.ts's BankReason, minus
// "supplies low" (melee has no ammo/casts to run out of).
type BankReason string

const (
	BankNone       BankReason = ""
	BankAskedFor   BankReason = "asked for"
	BankLoot       BankReason = "loot"
	BankPackFull   BankReason = "pack full"
	BankTimer      BankReason = "timer"
	BankOutOfFood  BankReason = "out of food"
	BankAfterDeath BankReason = "after death"
	BankGoal       BankReason = "goal"
)

// BankTriggerState is what DecideBankReason reads — ported from fighterPlan.ts's
// BankTriggerState, minus the supplies fields.
type BankTriggerState struct {
	AutoBank         bool
	LootSlots        int
	BankAtLootSlots  int
	PackFull         bool
	MinutesSinceBank float64
	BankEveryMinutes float64
	FoodCount        int
	FoodWanted       int
	FoodKnownEmpty   bool
	AfterDeath       bool
	GoalReached      bool
	Requested        bool
}

// DecideBankReason ports bankReason.
func DecideBankReason(s BankTriggerState) BankReason {
	if s.AfterDeath {
		return BankAfterDeath
	}
	if s.Requested {
		return BankAskedFor
	}
	if s.GoalReached {
		return BankGoal
	}
	if s.FoodWanted > 0 && s.FoodCount == 0 && !s.FoodKnownEmpty {
		return BankOutOfFood
	}
	if s.AutoBank && s.LootSlots >= s.BankAtLootSlots {
		return BankLoot
	}
	if s.AutoBank && s.PackFull && s.LootSlots > 0 {
		return BankPackFull
	}
	if s.AutoBank && s.BankEveryMinutes > 0 && s.LootSlots > 0 && s.MinutesSinceBank >= s.BankEveryMinutes {
		return BankTimer
	}
	return BankNone
}

// RestockWant is one thing a bank trip wants to top up to a count — ported from
// fighterPlan.ts's RestockWant.
type RestockWant struct {
	Name  string
	Qty   int
	Equip bool
}

// RestockStep is what to withdraw for one want — ported from fighterPlan.ts's RestockStep.
type RestockStep struct {
	Name  string
	Qty   int
	Equip bool
	// Short is true when the bank held fewer than asked.
	Short bool
}

// RestockPlan ports restockPlan: each want topped up to its count, capped by the bank and
// by free pack slots. Stackables (only coins/runes in the TS source; melee has none by
// default) cost one slot however many are drawn.
func RestockPlan(wants []RestockWant, held func(name string) int, bank func(name string) int, freeSlots int, stackable func(name string) bool) []RestockStep {
	var out []RestockStep
	free := freeSlots
	for _, want := range wants {
		need := want.Qty - held(want.Name)
		if need <= 0 {
			continue
		}
		inBank := bank(want.Name)
		stacks := stackable(want.Name)
		room := need
		if !stacks {
			if need < free {
				room = need
			} else {
				room = free
			}
		} else if held(want.Name) <= 0 && free <= 0 {
			room = 0
		}
		qty := room
		if inBank < qty {
			qty = inBank
		}
		if qty <= 0 {
			out = append(out, RestockStep{Name: want.Name, Qty: 0, Equip: want.Equip, Short: true})
			continue
		}
		if stacks {
			if held(want.Name) <= 0 {
				free--
			}
		} else {
			free -= qty
		}
		out = append(out, RestockStep{Name: want.Name, Qty: qty, Equip: want.Equip, Short: qty < need})
	}
	return out
}

// FreeSlots ports freeSlots for a 28-slot pack.
func FreeSlots(used int) int {
	if free := 28 - used; free > 0 {
		return free
	}
	return 0
}

// ---- mule hand-off ----
//
// AdvancedFighter.ts has no mule protocol of its own to port from (rsb0t-scripts' donor
// scripts are ProgressiveFisher/ProgressiveCrafter/EssenceMiner only) — this follows the
// same donorplan-based shape those three Go ports already use (see
// internal/scripts/essenceplan's own MuleRows/MuleHaul/MuleLoad trio, cited in
// docs/script_porting_framework.md as "the same three-function shape by design") rather
// than inventing a separate mechanism, per the account owner's own ask that combat loot
// hand off to a mule the same way a skilling script's catch does.

// muleRules is the hand-over policy: gear/food/bones-to-bury (anything ShouldKeep already
// protects from an ordinary bank deposit) never leaves, whatever loot the loot filter wants
// goes to the front of the load, and keep is the script's own extra muleKeep setting.
func muleRules(keep []string, keepInput KeepInput, filter LootFilter, isFoodItem IsFoodItemFunc) donorplan.Rules {
	return donorplan.Rules{
		Keep:  keep,
		Skip:  func(row donorplan.ItemRow) bool { return ShouldKeep(row.Name, keepInput, isFoodItem) },
		First: func(row donorplan.ItemRow) bool { return WantsLoot(row.Name, filter) },
	}
}

// MuleRows is what leaves the bank for the mule: loot first, then anything else not kept.
func MuleRows(bank []donorplan.ItemRow, keep []string, keepInput KeepInput, filter LootFilter, isFoodItem IsFoodItemFunc) []donorplan.ItemRow {
	return donorplan.HandoffRows(bank, muleRules(keep, keepInput, filter, isFoodItem))
}

// MuleHaul counts pack slots holding something for the mule.
func MuleHaul(pack []donorplan.ItemRow, keep []string, keepInput KeepInput, filter LootFilter, isFoodItem IsFoodItemFunc) int {
	return donorplan.HaulOf(pack, muleRules(keep, keepInput, filter, isFoodItem))
}

// MuleLoad is what goes on the trade screen: loot first, cut to the mule's free slots.
func MuleLoad(pack []donorplan.ItemRow, freeSlots int, keep []string, keepInput KeepInput, filter LootFilter, isFoodItem IsFoodItemFunc) []donorplan.LoadGroup {
	return donorplan.PlanLoad(pack, freeSlots, muleRules(keep, keepInput, filter, isFoodItem))
}

// ---- melee style ladder ----

// MeleeStyle is a melee training style — ported from fighterPlan.ts's MeleeStyle.
type MeleeStyle string

const (
	StyleAttack     MeleeStyle = "attack"
	StyleStrength   MeleeStyle = "strength"
	StyleControlled MeleeStyle = "controlled"
	StyleDefence    MeleeStyle = "defence"
)

// MeleeLevels is what NextMeleeStyle reads — ported from fighterPlan.ts's MeleeLevels.
type MeleeLevels struct {
	Attack, Strength, Defence int
}

// NextMeleeStyle ports nextMeleeStyle: the progressive ladder trains whichever of attack,
// strength and defence is lowest right now, ties going to the earlier rung (attack, then
// strength, then defence).
func NextMeleeStyle(levels MeleeLevels) MeleeStyle {
	best := StyleAttack
	bestLevel := levels.Attack
	if levels.Strength < bestLevel {
		best, bestLevel = StyleStrength, levels.Strength
	}
	if levels.Defence < bestLevel {
		best = StyleDefence
	}
	return best
}

// ParseMeleeStyle ports parseMeleeStyle, "progressive" included.
func ParseMeleeStyle(label string) MeleeStyle {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "attack", "strength", "controlled", "defence", "progressive":
		return MeleeStyle(strings.ToLower(strings.TrimSpace(label)))
	}
	return StyleStrength
}

// ---- goals ----

// Goal is a stop condition — ported from fighterPlan.ts's Goal, level goal only (kills and
// minutes are plain numbers, ported the same as the TS source).
type Goal struct {
	// LevelSkill/LevelTarget are the parsed skill:level goal; LevelSkill is "" for none.
	LevelSkill  string
	LevelTarget int
	Kills       int
	Minutes     float64
}

var levelGoalRe = regexp.MustCompile(`(?i)^\s*([a-z]+)\s*[:=]\s*(\d{1,2})\s*$`)

// ParseLevelGoal ports parseLevelGoal.
func ParseLevelGoal(raw string) (skill string, level int, ok bool) {
	m := levelGoalRe.FindStringSubmatch(raw)
	if m == nil {
		return "", 0, false
	}
	lvl, _ := strconv.Atoi(m[2])
	if lvl < 2 || lvl > 99 {
		return "", 0, false
	}
	return strings.ToLower(m[1]), lvl, true
}

// GoalReached ports goalReached: the first goal met, or "" for none.
func GoalReached(g Goal, level func(skill string) int, kills int, minutes float64) string {
	if g.LevelSkill != "" && level(g.LevelSkill) >= g.LevelTarget {
		return g.LevelSkill + " " + strconv.Itoa(g.LevelTarget) + " reached"
	}
	if g.Kills > 0 && kills >= g.Kills {
		return strconv.Itoa(kills) + " kills"
	}
	if g.Minutes > 0 && minutes >= g.Minutes {
		return strconv.Itoa(int(minutes)) + " minutes"
	}
	return ""
}

// ---- chat lines ----

// SplitNames ports splitNames: a comma-joined line into trimmed, non-empty names.
func SplitNames(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---- the stage dispatcher ----

// Stage is which single action AdvancedFighter's Loop should take this call — ported from
// AdvancedFighter.ts's own priority-ordered task list (ContinueDialog is handled by the Go
// bot adapter directly, matching every other ported script's own dialogue-continue guard).
type Stage string

const (
	StageDeathRecovery Stage = "death-recovery"
	StageHalted        Stage = "halted"
	StagePanicRetreat  Stage = "panic-retreat"
	StageEat           Stage = "eat"
	StageFixStyle      Stage = "fix-style"
	StageEnsureGear    Stage = "ensure-gear"
	StageBuryBones     Stage = "bury-bones"
	StageBankRun       Stage = "bank-run"
	StageLoot          Stage = "loot"
	StageFight         Stage = "fight"
	StageReturnAnchor  Stage = "return-anchor"
	StageIdle          Stage = "idle"
)

// Snapshot is everything DecideStage needs, read fresh from live state every call — ported
// from the validate() order of AdvancedFighter.ts's own task list: ContinueDialog, Death
// Recovery, Halted, ThreatEscape (folded into fighterrunner's own flee-to-bank check ahead
// of this call — see its own doc comment for why it's handled before DecideStage rather
// than as a stage), PanicRetreat, Cure (not ported — no antipoison support),
// EatOutOfCombat, DropPrayers (not ported), EnsureRetaliate+SetAttackStyle (folded into one
// FixStyle stage), EnsureGear, DropVials (not ported — no potions), BuryBones, CheckGoal
// (folded into the Halted read below rather than its own stage — it has no action beyond
// noting the goal, which the caller does inline), BankRun, PanelHold (no overlay to hold
// from), Loot, ReturnToSafespot (not ported — melee has none), Fight, ReturnToAnchor.
type Snapshot struct {
	Died          bool
	Halted        bool
	PanicNeeded   bool
	NeedEat       bool
	StyleWrong    bool
	GearMissing   bool
	NeedBury      bool
	BankDue       bool
	LootAvailable bool
	HaveFight     bool
	AnchorFar     bool
}

// DecideStage ports the task list's own priority order 1:1.
func DecideStage(s Snapshot) Stage {
	if s.Died {
		return StageDeathRecovery
	}
	if s.Halted {
		return StageHalted
	}
	if s.PanicNeeded {
		return StagePanicRetreat
	}
	if s.NeedEat {
		return StageEat
	}
	if s.StyleWrong {
		return StageFixStyle
	}
	if s.GearMissing {
		return StageEnsureGear
	}
	if s.NeedBury {
		return StageBuryBones
	}
	if s.BankDue {
		return StageBankRun
	}
	if s.LootAvailable {
		return StageLoot
	}
	if s.HaveFight {
		return StageFight
	}
	if s.AnchorFar {
		return StageReturnAnchor
	}
	return StageIdle
}
