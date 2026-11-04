// Package vialplan is VialFiller's own decisions, ported from rsb0t/src/bot/scripts/VialFiller's
// TypeScript source (VialFiller.ts plus its own VialFillerLogic.ts pure-logic sibling). It is
// pure: no client, actions, fleet or bot imports and no I/O.
//
// VialFiller.ts is a fixed three-leg trip run back to back — bank, an optional shop restock
// every N trips, then fill — not a live-state dispatcher the way essencePlan.ts/woodcutPlan.ts
// are. Every ported script so far that has a similarly fixed sequence runs the whole trip as one
// blocking call rather than reconstructing a resumable phase machine (see
// docs/script_porting_framework.md's own "one blocking Loop() call" rule); this package's own
// exported functions are correspondingly small: the two pure decisions VialFillerLogic.ts itself
// exports (IsShopRun, VialsToBuy), plus the places/names every runner needs.
//
// No mule: confirmed by reading the TS source's own structure before porting — VialFiller.ts
// only ever visits Jatix's shop and the fountain, no donorPlan/muleProtocol import anywhere.
//
// Deliberately not ported here, and why:
//   - The paint/status overlay — no headless equivalent, the same boundary drawn around every
//     other ported script's own HUD file.
//   - The exact "coinFloat / 2" bank-surplus floor VialFiller.ts's own coin withdrawal uses
//     before a shop run (the public-repo summary available for this port could not resolve the
//     precise formula). This port withdraws min(bank coins, CoinsPerTrip) instead — simpler and
//     safer (never withdraws more than the trip actually wants, never overdraws the bank), but a
//     real simplification versus the TS source, flagged honestly rather than guessed at.
package vialplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// ---- places and names ----
//
// Live-searched against this content pack's own config/loc archive before porting (radius search
// from a Falador-area teleport, .scratch/vialfind.go, this session) rather than trusted blind
// off the public repo's own figures — the same cross-check discipline flaxplan.FieldTile's own
// doc comment describes, after FlaxPicker's own upstream tile figure needed an 8-tile correction.
// All three of VialFiller.ts's own coordinates below matched this content pack closely (within a
// few tiles), unlike FlaxPicker's.

// Vial and VialOfWater are the pack/bank item names.
const (
	Vial        = "Vial"
	VialOfWater = "Vial of water"
	Coins       = "Coins"
)

// FaladorWestBank and FaladorEastBank are the two bank choices the "bank" setting selects
// between — VialFiller.ts's own default is West.
//
// West is the tile NORTH of the counter, not the counter itself. It was 2946,3367 until
// 2026-09-18, which is the bank booth loc (2213, shape 10 angle 2, m46_52.jm2 local 2,39):
// an unwalkable tile in the baked pack, so every leg out of the west bank planned as
// `unreachable` after 39 expansions. A* snapped the start to the first walkable tile within
// two rings, 2945,3366, which is inside the 20-tile bank interior south of the counter and
// walled off from the street, and the search then had nowhere to go. The reverse direction
// "worked" only because the destination radius let it stop on 2946,3368 — the tile this now
// names. The booth carries no forceapproach (content bank_booth.loc: op1=Use, op2=Use-quickly,
// blockrange=no), so it is served from either side; combatplan/ladder.go independently names
// the same 2946,3368 for Falador west.
var (
	FaladorWestBank = tile.Tile{X: 2946, Z: 3368, Level: 0}
	FaladorEastBank = tile.Tile{X: 3013, Z: 3355, Level: 0}
)

// BoothName and BoothOp open either Falador bank's booth.
const (
	BoothName = "Bank booth"
	BoothOp   = "Use-quickly"
)

// Fountain is the fill spot outside Falador West bank, and the loc's own display name — used
// on the pack's vials via UseOnLoc (OPLOCU), not a loc-menu click, since this content pack's own
// Fountain locs carry no "Fill"-labelled op of their own (confirmed live before porting).
var Fountain = tile.Tile{X: 2949, Z: 3381, Level: 0}

const FountainName = "Fountain"

// FountainRadius is how far from Fountain a fountain is still searched for.
const FountainRadius = 6

// JatixStand is Jatix's herblore shop in Taverley, the optional restock leg.
var JatixStand = tile.Tile{X: 2899, Z: 3430, Level: 0}

// Jatix is the shopkeeper's name, and the Trade op to open his shop.
const Jatix = "Jatix"

// PackSlots is a full pack.
const PackSlots = 28

// BankTile resolves the "bank" setting to a tile: "Falador East" (any case/spacing) picks the
// east bank, everything else (including blank) picks West, VialFiller.ts's own default.
func BankTile(setting string) tile.Tile {
	if same(setting, "Falador East") {
		return FaladorEastBank
	}
	return FaladorWestBank
}

func same(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// ---- pure decisions, ported from VialFillerLogic.ts ----

// IsShopRun ports isShopRun: only true once purchasing is enabled, at least one trip has
// completed, the interval is positive, and the completed trip count divides it evenly —
// deliberately never true on the very first trip, so a fresh account proves the bank/fountain
// loop before ever walking to Taverley (VialFillerLogic.ts's own reasoning, ported verbatim into
// this doc comment).
func IsShopRun(runs int, buyVials bool, everyN int) bool {
	if !buyVials || runs <= 0 || everyN <= 0 {
		return false
	}
	return runs%everyN == 0
}

// VialsToBuy ports vialsToBuy: bounded by both the free pack space and the desired amount, since
// empty vials do not stack, so a restock can only be as big as the free pack.
func VialsToBuy(freeSlots, want int) int {
	if freeSlots < 0 {
		freeSlots = 0
	}
	if want < 0 {
		want = 0
	}
	if freeSlots < want {
		return freeSlots
	}
	return want
}

// CoinsToWithdraw is the coins a shop-run bank leg draws for the restock: never more than the
// bank actually holds, never more than the trip's own target — see this package's own doc
// comment for how this differs from the TS source's exact "coinFloat / 2" formula.
func CoinsToWithdraw(bankCoins, target int) int {
	if target < 0 {
		target = 0
	}
	if bankCoins < target {
		if bankCoins < 0 {
			return 0
		}
		return bankCoins
	}
	return target
}
