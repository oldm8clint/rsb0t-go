// Package flaxplan is FlaxPicker's own decisions, ported from rsb0t/src/bot/scripts/FlaxPicker's
// TypeScript source (a single-file script, no separate *Plan.ts sibling — every decision lives
// inside FlaxPicker.ts's own task validators). It is pure: no client, actions, fleet or bot
// imports and no I/O.
//
// FlaxPicker.ts is one of the simplest scripts in the public catalogue: pick Flax at a fixed
// field, bank the whole pack when full, no mule, no combat, no shop, no NPC beyond the bank.
// Confirmed by reading the TS source structure before porting (a five/six-task validator list:
// ContinueDialog, EscapeFlaxTrap, StartupReset, BankTrip, Pick, GoToField): nothing here trades
// with another player or hands anything to a mule.
//
// Deliberately not ported here, and why:
//   - The on-screen paint/status overlay — no headless equivalent, the same boundary drawn
//     around every other ported script's own HUD file.
//   - EscapeFlaxTrap's BFS "boxed in by flax, drop N to carve a path out" recovery. This
//     project has no established primitive for a local reachability probe from inside a script
//     (cmd/doordiag does something adjacent but as an offline diagnostic tool, not a runtime
//     primitive a script can call mid-loop), and the failure mode it guards against is rare
//     enough (a live pile of already-picked flax on the player's own tile, not the flax bushes
//     themselves, which do not block movement) that inventing a bespoke BFS here — rather than
//     reusing a real, already-proven primitive — is exactly the kind of ad hoc code
//     docs/script_porting_framework.md warns against. If a live run ever actually gets stuck
//     this way, WalkNear's own generic stall/timeout handling still recovers it, just slower.
//     Flagged here rather than silently dropped.
package flaxplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// FieldTile is the flax field's own centre. FlaxPicker.ts's own fieldTile default (2741,3444)
// was close but not exact for this content pack's own baked loc placement — live-searched from
// the Seers bank via internal/locindex against the real running local engine (radius 50, "Flax"/
// "Pick") before porting rather than trusted blind, matching this project's own standing rule to
// cross-check a public-repo constant against local ground truth: the real nearest Flax bush from
// Seers is (2742,3452,0), about 8 tiles off the TS source's own figure.
var FieldTile = tile.Tile{X: 2742, Z: 3452, Level: 0}

// BankStand is the bank this field's own default settings send the picker to — the same Seers'
// Village bank row every other Seers-area ported script (ProgressiveWoodcutter, ProgressiveCrafter,
// the mule's own SeersStand) already uses, duplicated here per this project's established
// per-package convention rather than imported across plan packages.
var BankStand = tile.Tile{X: 2725, Z: 3493, Level: 0}

// Flax is the loc/item name.
const Flax = "Flax"

// PickOp picks Flax.
const PickOp = "Pick"

// BoothName and BoothOp open the bank.
const (
	BoothName = "Bank booth"
	BoothOp   = "Use-quickly"
)

// PackSlots is a full pack.
const PackSlots = 28

// FieldScope is how far from the field's own centre a Flax bush still counts as reachable —
// FlaxPicker.ts's own FIELD_SCOPE.
const FieldScope = 12

// FieldArriveRadius is how close to the field centre counts as arrived — FIELD_ARRIVE.
const FieldArriveRadius = 3

// PickRadius is how close to a specific Flax bush the picker walks before clicking it.
const PickRadius = 1

// Stage is where the picker belongs right now.
type Stage string

// The picker's stages.
const (
	StageBank   Stage = "bank"
	StagePick   Stage = "pick"
	StageTravel Stage = "travel"
)

// Snapshot is what DecideStage reads.
type Snapshot struct {
	PackFull bool
	// Haul is non-empty pack slots, any item.
	Haul int
	// StartupPending is true only until the very first Loop call has seen an empty pack —
	// FlaxPicker.ts's own StartupReset task, which banks whatever the pack already held before
	// this run started, once, rather than assuming an empty pack at boot.
	StartupPending bool
	// FlaxNearby is true when a Flax bush was found within FieldScope of the current tile.
	FlaxNearby bool
}

// DecideStage picks the picker's stage, ported from FlaxPicker.ts's own task-validator order:
// a full pack (or, once at startup, any leftover haul) goes to the bank first; otherwise pick
// where Flax is in reach, or travel to the field when it isn't.
func DecideStage(s Snapshot) Stage {
	if s.PackFull {
		return StageBank
	}
	if s.StartupPending && s.Haul > 0 {
		return StageBank
	}
	if s.FlaxNearby {
		return StagePick
	}
	return StageTravel
}

// DescribeStage is the status line for a stage.
func DescribeStage(s Stage) string {
	switch s {
	case StageBank:
		return "banking the pack"
	case StagePick:
		return "picking flax"
	case StageTravel:
		return "walking to the flax field"
	default:
		return string(s)
	}
}

func same(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// IsFlax reports whether name is the Flax item/loc name.
func IsFlax(name string) bool {
	return same(name, Flax)
}
