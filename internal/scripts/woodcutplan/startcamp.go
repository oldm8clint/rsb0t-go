package woodcutplan

// The Draynor Manor start camp: chop dead trees where they are dense enough to stand still, and
// fletch every log into arrow shafts so the pack never fills and the account never banks.
//
// This is the fleet owner's own design for a new account's first levels. Every coordinate and
// every rule below is MEASURED from the game content, because three of the assumptions behind
// the design turned out to need correcting:
//
//   - WHERE. content/maps/m48_52.jm2's LOC section holds 578 dead-tree placements around the
//     manor (224 `deadtree1`, 111 `deadtree1_large`, 243 `deadtree2_dark`; loc.pack ids
//     1282/1283/1289, all named "Dead tree" with op1 "Chop down" in
//     content/scripts/skill_woodcutting/configs/trees/normal.loc). The densest standable tile is
//     3091,3345, with 54 dead trees inside six tiles — which is what makes "stay put" real
//     rather than aspirational.
//
//   - WHAT THEY DROP. content/scripts/skill_woodcutting/configs/trees.dbrow's
//     `[normal_tree_table]` lists every dead-tree variant against `data=product,logs`,
//     `data=levelrequired,0`, `data=productexp,250`. So a dead tree gives PLAIN `Logs` at
//     Woodcutting 1 for 25 xp, which is exactly the log the fletching table wants (below).
//
//   - THE REAL HAZARD IS NOT WIZARDS. The owner's warning was aggressive level-13 wizards. There
//     are NO wizard spawns in m48_52 at all; the only Draynor wizards are two level-7 Dark
//     wizards at 3084,3236 and 3085,3238 in Draynor VILLAGE, about 109 tiles south, with
//     huntrange 3 and wanderrange 3 — they cannot reach the camp. The genuine danger is
//     `nasty_tree` (npc 152): 250 hitpoints, strength 20, `huntmode=aggressive_melee` with
//     `check_nottoostrong=off`, so it attacks an account of any level. It uses the npc_deadtree
//     model with the same recolours as `deadtree2_dark`, so it looks EXACTLY like a choppable
//     dead tree. Its ten spawns all sit at x >= 3103; NastyTreeFromX and the leash below keep
//     the camp west of every one of them, and the chop target is resolved by LOC (a loc index
//     lookup for "Dead tree"), never by npc, so a nasty tree can never be clicked by mistake.
//
// The owner's instruction that willows are cut at Seers rather than Draynor still holds and is
// unaffected: this camp is only ever the first rung, below the oak gate.

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// DraynorCamp is where the account stands to chop. 3091,3345 level 0 is the densest standable
// tile found by scanning x 3085-3130 / z 3328-3385 against m48_52's own loc placements: 54 dead
// trees within six tiles, two of them orthogonally adjacent, and the tile itself carries only
// ground decor (two `dugupsoil2_upass` shape-22 locs, which do not block).
var DraynorCamp = tile.Tile{X: 3091, Z: 3345}

// DraynorLeash is how far from DraynorCamp a dead tree may be and still be ours. Six tiles is
// where the 54-tree count was measured, and it is also what keeps the camp clear of the
// aggressive `nasty_tree` belt — the nearest one is 3103,3347, twelve tiles away, so nothing
// aggressive is inside this radius at all.
const DraynorLeash = 6

// NastyTreeFromX is the westmost `nasty_tree` spawn's x. Anything at or east of this is in the
// aggressive belt and is not somewhere a low-level account should stand. Recorded so a future
// change to DraynorCamp/DraynorLeash can be checked against it rather than rediscovering the
// hazard live.
const NastyTreeFromX = 3103

// DeadTree is the loc's display name, shared by all three variants at the manor.
const DeadTree = "Dead tree"

// LumbridgeKnife is the Knife ground spawn near Lumbridge castle: obj 946 at local 24,2 of
// content/maps/m50_50.jm2's OBJ section, which is world 3224,3202 level 0. It is about four
// tiles south of the castle's chamfered south-east corner wall — immediately outside the
// castle, not inside it, which matters because the walk target must not be planned through a
// wall.
//
// It is not the only one: KnifeSpawns records all four the world has.
var LumbridgeKnife = tile.Tile{X: 3224, Z: 3202}

// SeersKnife is the Knife ground spawn nearest the Seers bank (obj 946 at local 16,19 of
// m42_54.jm2 = world 2704,3475).
var SeersKnife = tile.Tile{X: 2704, Z: 3475}

// CatherbyKnife is the Knife ground spawn at Catherby (obj 946 at local 4,58 of m44_53.jm2 =
// world 2820,3450).
//
// This is the most useful of the four for this fleet: it is ELEVEN tiles from the mule post at
// 2809,3441 that every woodcutter already walks to, so it costs essentially nothing to collect.
var CatherbyKnife = tile.Tile{X: 2820, Z: 3450}

// SouthSeersKnife is the Knife ground spawn south of Seers' Village (obj 946 at local 12,15 of
// m42_53.jm2 = world 2700,3407).
var SouthSeersKnife = tile.Tile{X: 2700, Z: 3407}

// KnifeSpawns are the knife ground spawns this script knows about, nearest-useful first.
//
// All four are obj 946, level 0, read out of the OBJ section of their own map square and checked
// against content again on 2026-09-21. The list held only the first two until then, and that was
// a real supply limit rather than a tidiness problem: a knife is what fletches logs into arrow
// shafts, and users asked for one woodcutter per rune axe on the Seers market -- 94 of
// them. Two spawns cannot start 94 accounts, and the account's other two sources are worse than
// they look. The Seers market stocks no fletching knife at all (its "Iron knife" is the throwing
// weapon, a different obj), and the mule has no restock path of any kind -- it hands out only
// what donors gave it, which was one knife.
//
// Four spawns at obj's default respawnrate of 100 ticks (60s, scarabplan.KnifeRespawnTicks) is
// four knives a minute. A knife is never consumed, so the cohort needs 94 once, ever.
var KnifeSpawns = []tile.Tile{LumbridgeKnife, SeersKnife, CatherbyKnife, SouthSeersKnife}

// Knife is the fletching tool's item name.
const Knife = "Knife"

// KnifeDetourRadius is how far the account will go out of its way for a knife spawn.
//
// 100 tiles, which is deliberately generous in one direction only: both spawns sit near a place
// the account is already passing (LumbridgeKnife is 16 tiles from freshstart.LumbridgeCentre,
// SeersKnife 21 from the Seers bank), and the camp itself is 131 tiles from Lumbridge centre —
// outside this radius. So the detour is affordable while the account is still at Lumbridge and
// is NOT taken once it has walked to Draynor, which is the behaviour wanted: the knife is
// cheap to collect on the way out and expensive to go back for.
const KnifeDetourRadius = 100

// NearestKnife is the knife ground spawn nearest from, and its Chebyshev distance. ok is false
// when none is within KnifeDetourRadius on the same level.
func NearestKnife(from tile.Tile) (at tile.Tile, dist int, ok bool) {
	best, bestD := tile.Tile{}, KnifeDetourRadius+1
	for _, k := range KnifeSpawns {
		if k.Level != from.Level {
			continue
		}
		if d := tile.Chebyshev(from, k); d < bestD {
			best, bestD = k, d
		}
	}
	if bestD > KnifeDetourRadius {
		return tile.Tile{}, 0, false
	}
	return best, bestD, true
}

// ArrowShaft is what a plain log fletches into.
const ArrowShaft = "Arrow shaft"

// ShaftsPerLog is how many shafts one plain log yields — `data=shafts,15` on
// content/scripts/skill_fletching/configs/cut_logs/cut_logs.dbrow's `[fletching_normal]` row.
const ShaftsPerLog = 15

// PlainLogs is the only log that fletches into arrow shafts.
const PlainLogs = "Logs"

// FletchesToShafts reports whether a log fletches into arrow shafts.
//
// THIS IS ONLY TRUE OF PLAIN LOGS, and that corrects the design's own assumption that "all logs
// are fletched into arrow shafts". cut_logs.dbrow gives `data=shafts,15` to `[fletching_normal]`
// and to NOTHING else: `[fletching_oak]`, `[fletching_willow]`, `[fletching_maple]`,
// `[fletching_yew]` and `[fletching_magic]` have no `shafts` column at all, and
// cut_logs.rs2's `if ($shaft_count < 1)` branch sends them to the shortbow/longbow menu
// instead — there is no arrow-shaft option on an oak log or above. So the never-bank trick
// works at the Draynor dead trees, where the drop is plain Logs, and stops working the moment
// the ladder reaches oak.
func FletchesToShafts(logs string) bool {
	return same(strings.TrimSpace(logs), PlainLogs)
}

// StartPhase is one step of the Draynor start camp.
type StartPhase string

// The start-camp phases.
const (
	// StartKnife: fetch the knife ground spawn before anything else, so the very first log can
	// be fletched instead of filling a slot.
	StartKnife StartPhase = "knife"
	// StartAxe: no axe at all — one has to be bought before a tree can be chopped.
	StartAxe StartPhase = "axe"
	// StartWalk: head for the camp.
	StartWalk StartPhase = "walk"
	// StartFletch: the pack is carrying logs that can be turned into shafts.
	StartFletch StartPhase = "fletch"
	// StartChop: stand at the camp and chop.
	StartChop StartPhase = "chop"
	// StartDone: the oak gate is reached, so the ladder moves to Seers.
	StartDone StartPhase = "done"
	// StartBank: the pack is full of something this camp cannot turn into shafts, so there is
	// no free slot to chop into and no way out on the spot. The usual cause is an undelivered
	// mule hand-off carried back as notes.
	StartBank StartPhase = "bank"
)

// StartSnapshot is the live state DecideStartPhase reads.
type StartSnapshot struct {
	// Tile is nil while the position is not known.
	Tile *tile.Tile
	// Level is the account's Woodcutting level.
	Level int
	// Target is the ladder target, which decides where the camp hands over to Seers.
	Target string
	// HasKnife is true when a Knife is in the pack.
	HasKnife bool
	// HasAxe is true when any axe can chop right now (held or worn).
	HasAxe bool
	// PackLogs is how many PLAIN logs the pack holds.
	PackLogs int
	// PackFull is true when there is no free slot left.
	PackFull bool
	// KnifeInReach is true when the knife spawn is close enough to be worth the detour —
	// the caller decides that, because it depends on where the account actually is.
	KnifeInReach bool
}

// DecideStartPhase picks the next start-camp step from live state alone.
//
// The order matters and is not arbitrary:
//   - The knife comes FIRST, while the pack is empty and a detour is cheap. An account that
//     reaches the camp without one fills 28 slots with logs and then has to bank after all,
//     which is the exact thing this camp exists to avoid.
//   - Fletching beats chopping whenever the pack is full, because a full pack is the only
//     thing that can end a chopping session here; with a knife it is always recoverable on
//     the spot.
//   - The camp ends at the OAK GATE, not at the game's own oak requirement — see Tier.StartAt.
func DecideStartPhase(s StartSnapshot) StartPhase {
	if s.Tile == nil {
		return StartWalk
	}
	if AtOakGate(s.Level, s.Target) {
		return StartDone
	}
	if !s.HasKnife && s.KnifeInReach {
		return StartKnife
	}
	if !s.HasAxe {
		return StartAxe
	}
	// A full pack cannot chop. With a knife and plain logs that is fixable where it stands;
	// without one it is not, and the caller has to fall back to banking.
	if s.PackFull && s.HasKnife && s.PackLogs > 0 {
		return StartFletch
	}
	// ... and a full pack this camp CANNOT fletch its way out of has to leave, because every
	// other phase below it assumes a free slot. Returning StartChop here is what acct150 did
	// for four restarts: it came back from an abandoned mule hand-off with 26 of 28 slots held
	// by notes it could not cut, filled the last two, and then chopped at a full pack for ever
	// at nearly zero xp. Draynor has a bank of its own; the caller decides where to take it.
	if s.PackFull {
		return StartBank
	}
	if !AtCamp(*s.Tile, DraynorTier()) {
		return StartWalk
	}
	// Below a full pack, fletch opportunistically once there is a worthwhile batch, so the
	// chopping never has to stop for it.
	if s.HasKnife && s.PackLogs >= FletchBatch {
		return StartFletch
	}
	return StartChop
}

// FletchBatch is how many plain logs to let build up before fletching them in one go. One
// knife-on-logs use opens one make menu and batches the whole count, so a handful at a time
// costs one interaction rather than one per log — and staying well under a full pack means the
// chop is never interrupted by a pack that filled mid-swing.
const FletchBatch = 10

// AtOakGate reports whether the level has reached the ladder's own oak gate (Tier.StartAt, not
// the content's requirement), which is where the start camp hands over to the Seers ladder. A
// target that never reaches oak (someone chopping normal trees on purpose) never leaves.
func AtOakGate(level int, target string) bool {
	oak := TargetIndex("Oak")
	if TargetIndex(target) < oak {
		return false
	}
	return level >= TIERS[oak].StartAt
}

// DraynorTier is the start camp as an ordinary Tier, so the same AtCamp/Chop machinery the
// Seers rungs use drives it unchanged.
func DraynorTier() Tier {
	return Tier{
		Label:   "Dead tree (Draynor Manor)",
		Level:   1,
		StartAt: 1,
		Loc:     DeadTree,
		Logs:    PlainLogs,
		XP:      25,
		Anchor:  DraynorCamp,
		Leash:   DraynorLeash,
		Note:    "dense dead trees; stay west of x" + itoa(NastyTreeFromX) + " — the aggressive nasty trees there look identical",
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
