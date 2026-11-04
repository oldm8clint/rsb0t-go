package woodcutrunner

// The live half of the Draynor Manor start camp — the one thing internal/scripts/woodcutplan/
// startcamp.go was written for and then deliberately not wired to (see its own header, and the
// "woodspec" entry in COLLAB_ARCHIVE.md item 10: "the Draynor start camp is NOT wired into
// DecideWoodPhase ... remaining glue: knife pickup via actions.OpObj, and bankfletchrunner.Fletch
// is reusable as-is").
//
// This file is that glue and nothing more. It adds no new mechanism:
//
//   - Fletching is internal/scripts/bankfletchrunner.Fletch, called unchanged. Its Deps is
//     field-for-field identical to this package's, so the adapter below is a struct literal
//     rather than a re-implementation. Fletch is already live-proven (BankFletcher: "real
//     knife-on-logs fletch via the make menu, real bank deposit/withdraw cycle") and it already
//     carries the hard-won rule that the wait is for a menu OFFERING arrow shafts rather than
//     for "a make menu is open" — a stale menu satisfies the weak check instantly and the
//     caller then retries into it forever.
//   - The knife pickup is the same three steps RecoverAxeHead already uses for the thrown axe
//     head: find the ground item, walk ONTO its tile (a ground item is taken from its own
//     square, not from beside it), OpObj the take option, and then wait for the item to appear
//     in this account's own PlayerPack 3214 read. Never a "we clicked it" receipt.
//
// What is genuinely new here is only the give-up. The camp needs a knife it does not start with,
// and a ground spawn can be missing because another account took it. So TakeKnife reports WHY it
// failed, and internal/bot/woodcutter.go turns the camp off for the rest of the run after a
// bounded number of failures rather than looping — the account then falls through to the Seers
// ladder that was already proven live.

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/bankfletchplan"
	"rsb0t-go/internal/scripts/bankfletchrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// knifeSearchRadius is how far from the account a Knife on the ground is still ours to take.
//
// Deliberately small. The spawn's coordinates are known exactly (woodcutplan.KnifeSpawns, read
// out of each map's own OBJ section), so this is only slack for standing a tile or two off the
// square after a walk — not a search. A wide radius would happily pick up a knife some other
// account had just dropped, which is a different item with the same name.
const knifeSearchRadius = 3

// KnifeResult is one knife-collection attempt's outcome.
type KnifeResult struct {
	// Got is true only once a Knife is confirmed in this account's own PlayerPack read.
	Got bool
	// Reason is why not.
	Reason string
	// Stopped is true when the caller's own Delay asked to stop.
	Stopped bool
}

// HasKnife reports whether the pack holds a Knife, read through PlayerPack 3214.
func HasKnife(d *Deps) bool { return packCountByName(d, woodcutplan.Knife) > 0 }

// PlainLogsHeld is how many plain Logs the pack holds — the only log that fletches into arrow
// shafts (woodcutplan.FletchesToShafts; oak and above have no shafts column at all).
func PlainLogsHeld(d *Deps) int { return packCountByName(d, woodcutplan.PlainLogs) }

// ArrowShaftsHeld is how many Arrow shafts the pack holds. Stackable, so this is one slot
// however large it gets, which is the whole reason the camp never has to bank.
func ArrowShaftsHeld(d *Deps) int { return packCountByName(d, woodcutplan.ArrowShaft) }

// TakeKnife walks to the nearest knife ground spawn and picks it up.
//
// Already holding one is a success with no packets sent, so a caller may call this
// unconditionally.
func TakeKnife(d *Deps) KnifeResult {
	if HasKnife(d) {
		return KnifeResult{Got: true}
	}
	if FreeSlots(d) == 0 {
		return KnifeResult{Reason: "the pack is full, so a knife cannot be picked up"}
	}
	here := currentTile(d)
	spawn, dist, ok := woodcutplan.NearestKnife(here)
	if !ok {
		return KnifeResult{Reason: "no knife spawn within " + itoa(woodcutplan.KnifeDetourRadius) + " tiles"}
	}
	if dist > 0 {
		d.logf("fetching a knife from the ground spawn at %d,%d (%d tiles)", spawn.X, spawn.Z, dist)
		// TravelTo rather than WalkNear: from Lumbridge this is short, but an account that has
		// already walked out toward the camp is far enough for the leg to want webwalk's own
		// door/stair graph and the cross-region budget.
		if !TravelTo(d, spawn, 1) {
			return KnifeResult{Reason: "could not reach the knife spawn"}
		}
	}
	id, at, found := knifeOnGround(d)
	if !found {
		// The real, ordinary failure: ground spawns are shared, and another account can hold
		// the only knife. Named separately from "could not reach it" so the caller's give-up
		// can say which happened.
		return KnifeResult{Reason: "no knife on the ground at the spawn — another account has it"}
	}
	if tile.Chebyshev(currentTile(d), at) > 0 {
		if !WalkNear(d, at, 0) {
			return KnifeResult{Reason: "could not stand on the knife's own tile"}
		}
	}
	before := packCountByName(d, woodcutplan.Knife)
	if err := actions.OpObj(d.Session, takeObjOption, at.X, at.Z, id); err != nil {
		d.logf("taking the knife: %v", err)
		return KnifeResult{Reason: "could not pick the knife up"}
	}
	if !waitFor(d, "the knife to reach my pack", 5*time.Second, func() bool {
		return packCountByName(d, woodcutplan.Knife) > before
	}) {
		return KnifeResult{Reason: "the knife never reached my pack"}
	}
	d.logf("knife collected")
	return KnifeResult{Got: true}
}

// knifeOnGround is the nearest Knife ground item within knifeSearchRadius on this level.
func knifeOnGround(d *Deps) (int, tile.Tile, bool) {
	if d.Config == nil {
		return 0, tile.Tile{}, false
	}
	here := currentTile(d)
	bestDist := knifeSearchRadius + 1
	var bestAt tile.Tile
	bestID, found := 0, false
	for _, g := range d.Session.State().GroundItems() {
		obj, ok := d.Config.Obj(g.ID)
		if !ok || !strings.EqualFold(strings.TrimSpace(obj.Name), woodcutplan.Knife) {
			continue
		}
		at := tile.Tile{X: g.X, Z: g.Z, Level: g.Level}
		if at.Level != here.Level {
			continue
		}
		if dist := tile.Chebyshev(here, at); dist <= knifeSearchRadius && dist < bestDist {
			bestDist, bestAt, bestID, found = dist, at, g.ID, true
		}
	}
	return bestID, bestAt, found
}

// FletchResult is one fletching attempt's outcome.
type FletchResult struct {
	// Made is true only once the Arrow shaft count in this account's own pack has actually
	// risen. bankfletchrunner.Fletch already waits on exactly that, and this re-reads it here
	// rather than trusting the returned reason, because "made" is the one outcome the caller
	// counts.
	Made bool
	// Shafts is how many arrow shafts arrived.
	Shafts int
	// Logs is how many plain logs went in.
	Logs int
	// Reason is bankfletchrunner's own reason string: "made", "no-knife", "no-logs",
	// "use-failed", "no-menu" or "stopped".
	Reason string
}

// FletchShafts turns every plain log in the pack into arrow shafts.
//
// The point of the camp: one log becomes 15 shafts (woodcutplan.ShaftsPerLog, `data=shafts,15`
// on cut_logs.dbrow's [fletching_normal] row), shafts stack, so 28 slots of logs collapse into
// one slot and the account never has to leave for a bank. Only plain Logs fletch this way —
// woodcutplan.FletchesToShafts is false from oak up, which is exactly where the camp ends.
func FletchShafts(d *Deps) FletchResult {
	logsBefore := PlainLogsHeld(d)
	if logsBefore == 0 {
		return FletchResult{Reason: "no-logs"}
	}
	shaftsBefore := ArrowShaftsHeld(d)
	res := bankfletchrunner.Fletch(fletchDeps(d), woodcutplan.PlainLogs)
	shafts := ArrowShaftsHeld(d) - shaftsBefore
	logs := logsBefore - PlainLogsHeld(d)
	out := FletchResult{Shafts: shafts, Logs: logs, Reason: res.Reason}
	// The receipt is the pack, not the reason. Fletch can return "made" having produced
	// nothing if the batch was interrupted, and it can return "stopped" having already made
	// most of a batch; only the count decides.
	out.Made = shafts > 0
	if out.Made {
		d.logf("fletched %d log(s) into %d arrow shaft(s)", logs, shafts)
	}
	return out
}

// fletchDeps adapts this package's Deps to bankfletchrunner's. Every field is the same type
// and the same meaning; only Delay is re-typed, because each package declares its own named
// Delayer over the identical func signature.
func fletchDeps(d *Deps) *bankfletchrunner.Deps {
	return &bankfletchrunner.Deps{
		Session:    d.Session,
		Pack:       d.Pack,
		Locs:       d.Locs,
		Config:     d.Config,
		Components: d.Components,
		Chat:       d.Chat,
		Delay:      bankfletchrunner.Delayer(d.Delay),
		Log:        d.Log,
		// The camp's shafts belong on the Loot panel's FLETCHING card, not its Woodcutting one:
		// woodcutting produced the logs, fletching produced the shafts (owner, 2026-09-21).
		// Passing the same sink through means the camp and BankFletcher report identically,
		// through bankfletchrunner/fletchcount.go, rather than this file growing a second
		// recorder that can drift from it.
		Loot: d.Loot,
	}
}

// StartSnapshot builds woodcutplan.StartSnapshot from live state.
//
// Every reading goes through this account's own PlayerPack 3214, never a tab-resolved view: an
// account whose sidebar tabs the server has not assigned reads as holding nothing, and a
// start-camp decision taken on that would walk a fully-equipped account off to buy an axe. The
// caller's own snapshot already refuses to decide anything while ContainersReadable is false,
// and this is the second line of that same defence.
func StartSnapshot(d *Deps, level int, target string) woodcutplan.StartSnapshot {
	here := currentTile(d)
	_, _, ok := woodcutplan.NearestKnife(here)
	return woodcutplan.StartSnapshot{
		Tile:         &here,
		Level:        level,
		Target:       target,
		HasKnife:     HasKnife(d),
		HasAxe:       HasWorkingAxe(d),
		PackLogs:     PlainLogsHeld(d),
		PackFull:     PackFull(d),
		KnifeInReach: ok,
	}
}

// ChopDraynor chops one session at the Draynor start camp. Separate from Chop only so a caller
// does not have to know that the camp is an ordinary Tier; everything else — the Ent exclusion,
// the stall timeout, the loot window — is Chop's, unchanged.
func ChopDraynor(d *Deps) ChopResult { return Chop(d, woodcutplan.DraynorTier()) }

// StartCampMakeMatch is what the make menu is matched on when fletching here. Exported so a
// harness can assert the camp and BankFletcher agree rather than assuming it.
const StartCampMakeMatch = bankfletchplan.MakeMatch

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
