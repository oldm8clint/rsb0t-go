// Package bankfletchplan is BankFletcher's own decisions, ported from
// rsb0t/src/bot/scripts/BankFletcher/BankFletcher.ts. It is pure: no client, actions, fleet or
// bot imports and no I/O.
//
// BankFletcher.ts's own TS source assumes the account is already standing at the bank ("No
// movement logic (assumes proximity to bank/booth)" per the public repo's own structural
// summary) — every other script this codebase has ported adds real pathfinding on top of a TS
// source that either hand-walks a fixed route or assumes proximity the same way, so this port
// does too: WalkNear to a bank stand each trip, exactly like every sibling gathering script.
//
// This first port covers BankFletcher.ts's own "cut" mode only — logs plus a knife into an
// unfinished product via the make-menu (Arrow shafts from Logs is the simplest, safest case: no
// second stringing/attaching step, no bow-string item, no arrowhead-attach item-on-item
// interaction). "String" and "cut+string" and "attach" modes are real TS source features not
// ported here — see below.
//
// No mule: confirmed by reading the whole TS source before writing this — BankFletcher.ts
// imports no donorPlan/muleProtocol at all. The bank is the only destination for every trip.
//
// Deliberately not ported here, and why:
//   - The paint/status UI and product-selector dropdown — no headless equivalent.
//   - "String"/"cut+string" modes (bow string on unstrung bows) and "attach" mode (arrow shaft
//     on arrowhead) — both are real, different item-on-item interactions
//     (actions.Equipment.UseOnHeld already exists and would work; this port simply has not
//     exercised them yet). A future session can add a StageAttach/StageString the same shape as
//     StageFletch below once it has picked and confirmed real item names/ids for a specific
//     bow+string or shaft+head pair against this content pack, the same grounding discipline
//     this package's own Knife/Logs/Arrow-shaft ids already went through.
//   - The exact withdraw-quantity click sequences ("10+1 combinations") the TS source uses for a
//     partial bank stack — this port withdraws a full load via Bank.WithdrawAll/WithdrawX
//     instead, matching every other ported script's own bank helpers.
package bankfletchplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// ---- names ----

const (
	Knife = "Knife"
	Logs  = "Logs"
)

// ArrowShaft is the only product this first port makes — the simplest cut-mode product
// (Logs + Knife -> Arrow shafts), no second stringing step.
const ArrowShaft = "Arrow shaft"

// MakeMatch is the substring FindMakeProduct matches the make menu's own product against —
// "shaft" alone (not the full "Arrow shaft" name) since the make menu's own object picture is
// captioned by the product config's own name, which this content pack may spell slightly
// differently (plural, capitalisation) the same way CasketOpener's own id-vs-name lesson
// (docs/script_parity_status.md) warns against assuming an exact string match.
const MakeMatch = "shaft"

// ---- places ----

// BankStand is the bank this port walks to each trip — the same proven Seers' Village bank row
// FlaxPicker/ProgressiveWoodcutter/ProgressiveCrafter already use, chosen deliberately over
// BankFletcher.ts's own hardcoded (3185,3440) default: this port adds real pathfinding the TS
// source never needed (see this package's own doc comment), and Seers is this codebase's own
// already-proven-safe bank region rather than an unexercised one.
var BankStand = tile.Tile{X: 2725, Z: 3493, Level: 0}

const (
	BoothName = "Bank booth"
	BoothOp   = "Use-quickly"
)

// PackSlots is a full pack.
const PackSlots = 28

// ---- the trip ----

// Stage is one leg of a fletching trip.
type Stage string

const (
	StageBank   Stage = "bank"
	StageFletch Stage = "fletch"
)

// Snapshot is what DecideStage reads.
type Snapshot struct {
	// HasKnife: a Knife is already held.
	HasKnife bool
	// HasLogs: Logs are already held.
	HasLogs bool
}

// DecideStage picks the trip's next leg: bank first when either the knife or logs are missing,
// otherwise fletch — ported from BankFletcher.ts's own BankTrip/Fletch task-validator pair.
func DecideStage(s Snapshot) Stage {
	if !s.HasKnife || !s.HasLogs {
		return StageBank
	}
	return StageFletch
}

func same(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// IsLogs reports whether name is the Logs item name.
func IsLogs(name string) bool { return same(name, Logs) }

// IsKnife reports whether name is the Knife item name.
func IsKnife(name string) bool { return same(name, Knife) }

// DescribeStage is the status line for a stage.
func DescribeStage(s Stage) string {
	switch s {
	case StageBank:
		return "restocking knife and logs at the bank"
	case StageFletch:
		return "fletching arrow shafts"
	default:
		return string(s)
	}
}
