// Package essenceplan is EssenceMiner's own decisions, ported from rsb0t-scripts' essencePlan.ts:
// which pickaxe tier it should hold, where it belongs right now (mine, portal, bank, fetch a
// pickaxe, or teleport back in), and which bank rows go to the mule once the essence stack passes
// its threshold. Settings live in settings.go.
//
// It is pure: no client, actions, fleet or bot imports and no I/O. Places are named by strings and
// tile coordinates; items are plain strings. It builds on donorplan for the hand-over the same way
// crafterplan and fisherplan do — EssenceMiner.ts's own essencePlan.ts is itself only a thin
// wrapper around donorPlan.ts's rowsFor/haulOf/planLoad, so this package is thinner still than its
// two siblings.
//
// Deliberately not ported here, and why (see COLLAB.md for the fuller account):
//   - muleDonor.ts (the donor's own wall-bus side of a hand-off) is already covered by
//     donorplan/handover.go, which replaces the browser wall's BroadcastChannel with in-game trade
//     steps for every script, not just this one.
//   - EssenceMiner.ts's loop() itself (walking to Aubury/the portal/the bank, the actual mine/
//     interact/withdraw/buy sequences, the HUD) is the future internal/bot integration's job, the
//     same boundary already drawn around ProgressiveCrafter/ProgressiveFisher/MuleReceiver's own
//     pure packages — this package is the decisions that loop reads, not the loop.
//   - essenceHud.ts / essenceRows are the Electron wall's on-screen overlay, which a headless Go
//     client has no equivalent of; IsNotedEssence is kept because it is a real decision the hand-
//     over section also needs (telling a noted essence stack apart from an unnoted one by id), not
//     because the HUD reads it.
package essenceplan

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/tile"
)

// ---- places and names ----
//
// Checked against essencePlan.ts directly (this repo's sibling rsb0t-scripts checkout, not the
// curated reference/rsb0t-scripts/ copy, which does not include EssenceMiner — see COLLAB.md).

// Essence is the pack/bank item name.
const Essence = "Rune essence"

// EssenceID is the unnoted pure-essence item id, runecraftPlan.ts's ESSENCE_ID. IsNotedEssence uses
// it to tell a noted essence stack apart from an unnoted one, since both share Essence's name.
const EssenceID = 1436

// RuneMysteries is the quest Aubury's teleport requires.
const RuneMysteries = "Rune Mysteries Quest"

// BankStand is Varrock East's bank, the built-in miner's stand and where the mule is met.
var BankStand = tile.Tile{X: 3251, Z: 3420, Level: 0}

// BoothName and BoothOp open BankStand's booth.
const (
	BoothName = "Bank booth"
	BoothOp   = "Use-quickly"
)

// AuburyTile is where Aubury stands, outside the mine.
var AuburyTile = tile.Tile{X: 3253, Z: 3402, Level: 0}

// Aubury is the NPC who teleports the miner in and out.
const Aubury = "Aubury"

// TeleportOp is Aubury's teleport-in option.
const TeleportOp = "Teleport"

// AuburyLeash is how far from AuburyTile Aubury is still searched for, essencePlan.ts's own
// unnamed 8-tile constant (AUBURY_LEASH in EssenceMiner.ts).
const AuburyLeash = 8

// EssenceRock is the mineable loc inside the mine — capitalised differently from Essence (the item
// name), exactly as ESS_ROCK/ESSENCE differ in essencePlan.ts.
const EssenceRock = "Rune Essence"

// MineOp mines EssenceRock.
const MineOp = "Mine"

// PortalName and PortalOp are the portal back to Varrock, inside the mine.
const (
	PortalName = "Portal"
	PortalOp   = "Use"
)

// PackSlots is a full pack.
const PackSlots = 28

// InEssenceMine reports whether t is inside the instanced essence mine Aubury and the portal
// teleport between, told by its mapsquare (t.x>>6, t.z>>6) == (45, 75). A nil tile (not yet known)
// is outside.
func InEssenceMine(t *tile.Tile) bool {
	return t != nil && t.X>>6 == MineMapsquareX && t.Z>>6 == MineMapsquareZ
}

// MineMapsquareX and MineMapsquareZ are the essence mine's mapsquare (45,75): every teleporter sets the account down on one of
// its 22 spots (essence_mine.rs2), and the mine's portal is the only way out.
const (
	MineMapsquareX = 45
	MineMapsquareZ = 75
)

// ---- the pickaxe ----

// BestAvailable lets the miner pick the best pickaxe its Mining level allows, worn/held/banked
// first, bought from Nurmof only when nothing usable is already owned.
const BestAvailable = "Best available"

// PickTier is one pickaxe tier: the item it needs held, and the Mining level required to use it.
type PickTier struct {
	Tier  string
	Item  string
	Level int
}

// PickTiers are every pickaxe tier the miner knows about, highest first — the order
// DesiredPickaxe/ResolvePick walk to find the best one a given Mining level can use.
var PickTiers = []PickTier{
	{Tier: "Rune", Item: "Rune pickaxe", Level: 41},
	{Tier: "Adamant", Item: "Adamant pickaxe", Level: 31},
	{Tier: "Mithril", Item: "Mithril pickaxe", Level: 21},
	{Tier: "Steel", Item: "Steel pickaxe", Level: 6},
	{Tier: "Iron", Item: "Iron pickaxe", Level: 1},
	{Tier: "Bronze", Item: "Bronze pickaxe", Level: 1},
}

// PickOptions are the pickaxe setting's choices: BestAvailable, then every tier name.
func PickOptions() []string {
	out := make([]string, 0, len(PickTiers)+1)
	out = append(out, BestAvailable)
	for _, t := range PickTiers {
		out = append(out, t.Tier)
	}
	return out
}

// IsPickaxe reports whether name is one of PickTiers' items, matched case-insensitively. The
// hand-over's rules use this to keep every pickaxe out of the mule's load regardless of the keep
// setting, the same way essencePlan.ts's rules() always skips isPickaxe.
func IsPickaxe(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, t := range PickTiers {
		if strings.ToLower(t.Item) == n {
			return true
		}
	}
	return false
}

func tierFor(selection string) (PickTier, bool) {
	sel := strings.ToLower(strings.TrimSpace(selection))
	for _, t := range PickTiers {
		if strings.ToLower(t.Tier) == sel {
			return t, true
		}
	}
	return PickTier{}, false
}

// RequiredMiningLevel is the Mining level a specific tier selection needs; ok is false for
// BestAvailable (or any other non-tier selection), which has no single required level.
func RequiredMiningLevel(selection string) (level int, ok bool) {
	t, ok := tierFor(selection)
	if !ok {
		return 0, false
	}
	return t.Level, true
}

// DesiredPickaxe is the exact tier the miner should own: the selected tier when the level allows
// it (ok is false when it does not — the miner should stop, not fall back to a lower tier it did
// not ask for), or the best tier BestAvailable's level allows.
func DesiredPickaxe(selection string, miningLevel int) (PickTier, bool) {
	if specific, ok := tierFor(selection); ok {
		if miningLevel >= specific.Level {
			return specific, true
		}
		return PickTier{}, false
	}
	for _, t := range PickTiers {
		if miningLevel >= t.Level {
			return t, true
		}
	}
	return PickTier{}, false
}

// NeedsPickaxeCheck reports whether the desired tier has moved on from lastChecked (empty for
// never checked) since a setting change or a Mining level-up — the only two things that change
// what DesiredPickaxe returns.
func NeedsPickaxeCheck(lastChecked, selection string, miningLevel int) bool {
	desired := ""
	if t, ok := DesiredPickaxe(selection, miningLevel); ok {
		desired = t.Item
	}
	return lastChecked != desired
}

// PickKind is what ResolvePick decided.
type PickKind string

// The pick resolutions.
const (
	// PickHeld: already worn or in the pack.
	PickHeld PickKind = "held"
	// PickWithdraw: in the bank, not yet in the pack.
	PickWithdraw PickKind = "withdraw"
	// PickStop: nothing usable anywhere; Reason says why.
	PickStop PickKind = "stop"
)

// PickResolution is what ResolvePick found.
type PickResolution struct {
	Kind PickKind
	// Item is set for PickHeld and PickWithdraw.
	Item string
	// Reason is set for PickStop.
	Reason string
}

func hasName(names []string, item string) bool {
	for _, n := range names {
		if strings.EqualFold(n, item) {
			return true
		}
	}
	return false
}

// ResolvePick decides what to do about the pickaxe: use one already held, withdraw one from the
// bank, or stop and say why nothing usable is available. A specific tier selection that the
// Mining level cannot use is always a stop, even if a lower tier is held — the miner does not
// silently downgrade what the user asked for.
func ResolvePick(selection string, miningLevel int, held, bank []string) PickResolution {
	specific, isSpecific := tierFor(selection)
	if isSpecific && miningLevel < specific.Level {
		return PickResolution{Kind: PickStop, Reason: "Mining " + strconv.Itoa(specific.Level) + " required for the " + specific.Item + " (have " + strconv.Itoa(miningLevel) + ")"}
	}
	candidates := PickTiers
	if isSpecific {
		candidates = []PickTier{specific}
	} else {
		var usable []PickTier
		for _, t := range PickTiers {
			if miningLevel >= t.Level {
				usable = append(usable, t)
			}
		}
		candidates = usable
	}
	for _, t := range candidates {
		if hasName(held, t.Item) {
			return PickResolution{Kind: PickHeld, Item: t.Item}
		}
	}
	for _, t := range candidates {
		if hasName(bank, t.Item) {
			return PickResolution{Kind: PickWithdraw, Item: t.Item}
		}
	}
	if isSpecific {
		return PickResolution{Kind: PickStop, Reason: "no " + specific.Item + " in inventory, equipment, or bank"}
	}
	names := make([]string, len(candidates))
	for i, t := range candidates {
		names[i] = t.Item
	}
	want := "usable pickaxe (" + strings.Join(names, ", ") + ")"
	return PickResolution{Kind: PickStop, Reason: "no " + want + " in inventory, equipment, or bank"}
}

// HeldPickaxeToKeep is the pickaxe a bank trip must leave in the pack, or "" when none usable is
// held (a bank withdrawal is needed instead).
func HeldPickaxeToKeep(selection string, miningLevel int, held []string) string {
	res := ResolvePick(selection, miningLevel, held, nil)
	if res.Kind == PickHeld {
		return res.Item
	}
	return ""
}

// ---- Nurmof, the pickaxe shop ----
//
// Confirmed against the real @rsb0t/api package source (this session found it in the sibling
// rsb0t-main checkout's packages/rsb0t-api, specifically src/bot/api/acquisition/
// ToolAcquire.ts's NURMOF_VENDOR/PICKAXE_SHOP_COSTS — not in reference/rsb0t-scripts/ or the
// sibling rsb0t-scripts checkout, which is why an earlier pass on this same port had wrongly
// concluded these constants were unreachable and left the Nurmof leg unported; see COLLAB.md/
// docs/script_parity_status.md for that correction).

// NurmofStand is where Nurmof stands, inside the Dwarven Mine (z > 9000 is this world's real,
// baked mapsquare for that underground area — not a separate "plane", the classic engine's
// dungeons genuinely live at these coordinates on level 0).
var NurmofStand = tile.Tile{X: 2997, Z: 9844, Level: 0}

// NurmofHopFrom is the surface tile beside the trapdoor down to the Dwarven Mine.
var NurmofHopFrom = tile.Tile{X: 3019, Z: 3449, Level: 0}

// NurmofHopLoc and NurmofHopOp open the trapdoor down.
const (
	NurmofHopLoc = "Trapdoor"
	NurmofHopOp  = "Climb-down"
)

// Nurmof is the shopkeeper's name, and the Trade op to open his shop.
const Nurmof = "Nurmof"

// NurmofHopZThreshold is the z coordinate the Dwarven Mine's own mapsquare starts at — a tile
// with z below this is still on the surface and needs the trapdoor hop first; at or above it,
// already underground. Matches ToolAcquire.ts's own `vendor.stand.z > 9000`/`here.z < 9000`
// checks exactly.
const NurmofHopZThreshold = 9000

// PickaxeShopCost is Nurmof's real list price per tier, ported verbatim from
// ToolAcquire.ts's PICKAXE_SHOP_COSTS.
var PickaxeShopCost = map[string]int{
	"Bronze pickaxe":  1,
	"Iron pickaxe":    140,
	"Steel pickaxe":   500,
	"Mithril pickaxe": 1300,
	"Adamant pickaxe": 3200,
	"Rune pickaxe":    32000,
}

// PickaxeCost looks up item's Nurmof price, case-insensitively.
func PickaxeCost(item string) (int, bool) {
	for name, cost := range PickaxeShopCost {
		if strings.EqualFold(name, item) {
			return cost, true
		}
	}
	return 0, false
}

// ---- the miner ----

// MinerStage is where the miner belongs right now.
type MinerStage string

// The miner's stages.
const (
	StagePickaxe  MinerStage = "pickaxe"
	StageMine     MinerStage = "mine"
	StagePortal   MinerStage = "portal"
	StageBank     MinerStage = "bank"
	StageTeleport MinerStage = "teleport"
)

// MinerSnapshot is what DecideStage reads.
type MinerSnapshot struct {
	InMine   bool
	PackFull bool
	// Haul is pack slots holding anything but the pickaxe.
	Haul     int
	PickHeld bool
}

// DecideStage picks the miner's stage, ported from essencePlan.ts's minerStage: inside the mine it
// mines until full and then takes the portal; outside, anything in the pack goes to the bank
// first, a missing pickaxe is fetched, and an empty pack goes back through Aubury.
func DecideStage(s MinerSnapshot) MinerStage {
	if s.InMine {
		if s.PackFull {
			return StagePortal
		}
		return StageMine
	}
	if s.Haul > 0 {
		return StageBank
	}
	if !s.PickHeld {
		return StagePickaxe
	}
	return StageTeleport
}

// ---- the hand-over ----

// MuleEssenceAt is the default banked-essence threshold at which the whole stack, and everything
// else in the bank, goes to the mule — essencePlan.ts's MULE_AT.
const MuleEssenceAt = 10_000

func isEssenceName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), Essence)
}

// muleRules are the hand-over rules: the essence stack goes first, pickaxes never leave whatever
// the keep list says, the keep list stays too.
func muleRules(keep []string) donorplan.Rules {
	return donorplan.Rules{
		Keep:  keep,
		Skip:  func(row donorplan.ItemRow) bool { return IsPickaxe(row.Name) },
		First: func(row donorplan.ItemRow) bool { return isEssenceName(row.Name) },
	}
}

// MuleRows is what leaves the bank for the mule: the essence stack first, then everything else not
// kept; pickaxes and the keep list stay.
func MuleRows(bank []donorplan.ItemRow, keep []string) []donorplan.ItemRow {
	return donorplan.HandoffRows(bank, muleRules(keep))
}

// MuleHaul counts pack slots holding something for the mule.
func MuleHaul(pack []donorplan.ItemRow, keep []string) int {
	return donorplan.HaulOf(pack, muleRules(keep))
}

// MuleLoad is what goes on the trade screen: the essence stack first, cut to the mule's free
// slots.
func MuleLoad(pack []donorplan.ItemRow, freeSlots int, keep []string) []donorplan.LoadGroup {
	return donorplan.PlanLoad(pack, freeSlots, muleRules(keep))
}

// IsNotedEssence reports whether row is the noted essence stack, told apart from an unnoted one
// (held while mining, before it is banked) by id.
func IsNotedEssence(row donorplan.ItemRow) bool {
	return isEssenceName(row.Name) && row.ID != EssenceID
}

// HandoffAfterBanking reports whether a bank deposit that left bankedEssence in the bank should
// start a hand-over: the mule is set, and donorplan.HandoffDue agrees on the threshold and the
// retry cooldown.
func HandoffAfterBanking(bankedEssence int, s Settings, now, nextAllowed time.Time) bool {
	d := s.Donor()
	if !d.Enabled() {
		return false
	}
	return donorplan.HandoffDue(bankedEssence, d.MuleAt, now, nextAllowed)
}

// ---- odds and ends the loop reads ----

// MineStallTimeout is how long mining can go without an essence gain before the rock is clicked
// again — essencePlan.ts's STALL_MS, held by EssenceMiner.ts itself.
const MineStallTimeout = 20 * time.Second

// MineStalled reports whether lastGain is old enough that the rock should be clicked again.
func MineStalled(lastGain, now time.Time) bool {
	return now.Sub(lastGain) >= MineStallTimeout
}

// MaxTeleportFails is how many landed-nowhere teleports in a row before the miner starts warning
// instead of quietly retrying — EssenceMiner.ts's MAX_TELEPORT_FAILS.
const MaxTeleportFails = 3

// TeleportGivingUp reports whether fails consecutive failed teleports is enough to warn.
func TeleportGivingUp(fails int) bool {
	return fails >= MaxTeleportFails
}

// questRefusalPattern matches Aubury's chat line refusing the teleport without Rune Mysteries,
// ported verbatim from EssenceMiner.ts's onStart handler.
var questRefusalPattern = regexp.MustCompile(`(?i)need to have completed the rune mysteries`)

// IsQuestRefusalMessage reports whether a chat line is Aubury refusing the teleport for a
// still-incomplete Rune Mysteries.
func IsQuestRefusalMessage(line string) bool {
	return questRefusalPattern.MatchString(line)
}
