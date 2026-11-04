// Package casketrunner is the I/O half of the CasketOpener port: it drives the pure
// pack/bank decisions in internal/scripts/casketplan against a live session, following the
// same split every sibling runner package uses. See docs/script_porting_framework.md for the
// recipe.
//
// Deliberately NOT ported here: the HUD/bank feed/tally/update-checker — no headless
// equivalent, same boundary drawn around every other ported script.
package casketrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/casketplan"
	"rsb0t-go/internal/scripts/fastopen"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
)

const PlayerPack = 3214
const PackSlots = 28

const (
	BoothName = "Bank booth"
	BoothOp   = "Use-quickly"
)

type Delayer func(d time.Duration) bool

func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	Log        func(string)
}

func (d *Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

func currentTile(d *Deps) tile.Tile {
	x, z, level, _ := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

func waitFor(d *Deps, what string, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(100 * time.Millisecond) {
			return false
		}
	}
	d.logf("timed out waiting for %s", what)
	return false
}

const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint

const guardianWaitLogEvery = 10 * time.Second

// WaitForGuardian mirrors every sibling runner's identical helper.
func WaitForGuardian(d *Deps) bool {
	randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
	if !randomevents.Busy(d.Session) {
		return true
	}
	d.logf("[guardian] a random event is present; waiting")
	lastLog := time.Now()
	for randomevents.Busy(d.Session) {
		randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
		if !d.Delay(500 * time.Millisecond) {
			return false
		}
		if time.Since(lastLog) >= guardianWaitLogEvery {
			d.logf("[guardian] still waiting on a random event")
			lastLog = time.Now()
		}
	}
	return true
}

const walkNearTimeout = 120 * time.Second

// WalkNear mirrors every sibling runner's identical helper (see
// docs/script_porting_framework.md's "why duplicated" section).
func WalkNear(d *Deps, dest tile.Tile, radius int) bool {
	deadline := time.Now().Add(walkNearTimeout)
	for time.Now().Before(deadline) {
		here := currentTile(d)
		if tile.Chebyshev(here, dest) <= radius {
			return true
		}
		res := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level})
		if !res.OK {
			d.logf("no path from %d,%d,%d to %d,%d,%d: %s", here.X, here.Z, here.Level, dest.X, dest.Z, dest.Level, res.Reason)
			return false
		}
		if len(res.Waypoints) < 2 {
			return true
		}
		if err := actions.Walk(d.Session, res.Waypoints, false); err != nil {
			d.logf("walk: %v", err)
			return false
		}
		lastX, lastZ, still := here.X, here.Z, time.Now()
		for time.Now().Before(deadline) {
			randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			cur := currentTile(d)
			if tile.Chebyshev(cur, dest) <= radius {
				return true
			}
			if cur.X != lastX || cur.Z != lastZ {
				lastX, lastZ, still = cur.X, cur.Z, time.Now()
			} else if time.Since(still) > 2*time.Second {
				break
			}
		}
	}
	return false
}

// fresh adapts d to freshstartrunner.Deps, the same field-for-field copy as thieverrunner.fresh,
// for freshstartrunner.WalkToUse.
func fresh(d *Deps) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay), Log: d.Log,
	}
}

// ---- resolving "Casket" against the live content pack ----

// CasketIDs is every object id this content pack uses for a casket: Unnoted is every id an
// OPHELD1 Open click actually works on, Noted is every id the bank would turn back into one of
// those — resolved live rather than trusting rsb0t-scripts' own hardcoded single id 405,
// since this client's content pack numbering is not guaranteed to match upstream's (see
// casketplan's own doc comment).
//
// A set on both sides, not one scalar id, because this content pack maps several distinct
// object ids (treasure-trail reward caskets, one per tier, an older RuneScape convention this
// build apparently still uses) to the identical display name "Casket" — found live
// (cmd/casketopenertest, 2026-09-16): an earlier version of this type picked just one of them
// (the last match in id order), and a real `::give casket` landed a *different* id than the
// one resolved, so `Openable` read zero on a pack that genuinely held an openable casket. Every
// id sharing the name is now treated as "a casket."
type CasketIDs struct {
	Unnoted map[int]bool
	Noted   map[int]bool
}

// ResolveCasketIDs finds every "Casket" object id via cachecfg.Config.ObjIDs and classifies
// each by whether it is a bank note (Obj.Noted()) of the unnoted item.
func ResolveCasketIDs(cfg *cachecfg.Config) (CasketIDs, bool) {
	ids := cfg.ObjIDs(casketplan.CasketName)
	out := CasketIDs{Unnoted: map[int]bool{}, Noted: map[int]bool{}}
	found := false
	for _, id := range ids {
		obj, ok := cfg.Obj(id)
		if !ok {
			continue
		}
		if obj.Noted() {
			out.Noted[id] = true
		} else {
			out.Unnoted[id] = true
		}
		found = true
	}
	return out, found
}

// IsCasket reports whether id is any known casket variant, noted or not.
func (c CasketIDs) IsCasket(id int) bool { return c.Unnoted[id] || c.Noted[id] }

// PackItems is every non-empty backpack slot, live.
func PackItems(d *Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

func packItems(d *Deps) []gamestate.SlotItem { return PackItems(d) }

// Openable counts unnoted caskets currently in the pack — the ones a click can open right now.
func Openable(d *Deps, ids CasketIDs) int {
	n := 0
	for _, it := range packItems(d) {
		if ids.Unnoted[int(it.ID)] {
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// NotedHeld counts noted caskets in the pack — caskets the bank has to turn back before they
// can be opened.
func NotedHeld(d *Deps, ids CasketIDs) int {
	n := 0
	for _, it := range packItems(d) {
		if ids.Noted[int(it.ID)] {
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

func packStacks(d *Deps) []casketplan.Stack {
	var out []casketplan.Stack
	for _, it := range packItems(d) {
		out = append(out, casketplan.Stack{ID: int(it.ID), Count: int(it.Count)})
	}
	return out
}

func nameFor(d *Deps, id int) string {
	if obj, ok := d.Config.Obj(id); ok && obj.Name != "" {
		return obj.Name
	}
	return fmt.Sprintf("#%d", id)
}

// OpenBatchResult is one openBatch pass's outcome.
type OpenBatchResult struct {
	// Opened is how many caskets actually left the pack this pass.
	Opened int
	// Gained is every item id whose pack total went up, keyed by name.
	Gained map[string]int
	// Refused is true when not even one Open click was accepted.
	Refused bool
}

const openTicks = 4
const boxesPerPass = 28

// OpenBatch opens every unnoted casket in the pack five a tick (fastopen.Open: one message a tick,
// the next click closing the last reward box itself) and reports what was gained. Whatever that
// leaves goes the old way, ported from CasketOpener.ts's openBatch(): an Open click at every slot
// with no wait between them, a settle on the count falling, and the reward boxes cleared.
func OpenBatch(d *Deps, ids CasketIDs) OpenBatchResult {
	held := Openable(d, ids)
	if held == 0 {
		return OpenBatchResult{}
	}
	before := casketplan.PackCounts(packStacks(d))
	result := func() OpenBatchResult {
		opened := max(0, held-Openable(d, ids))
		gained := map[string]int{}
		for id, count := range casketplan.Gained(before, casketplan.PackCounts(packStacks(d)), ids.IsCasket) {
			casketplan.AddFind(gained, nameFor(d, id), count)
		}
		return OpenBatchResult{Opened: opened, Gained: gained}
	}
	// Five opens a tick, one message a tick, confirmed once (fastopen); what it could not open goes
	// the old way below.
	var unnoted []gamestate.SlotItem
	for _, it := range packItems(d) {
		if ids.Unnoted[int(it.ID)] {
			unnoted = append(unnoted, it)
		}
	}
	if r := fastopen.Open(d.Session, fastopen.Delayer(d.Delay), PlayerPack, unnoted); r.Reason == "done" || r.Reason == "stopped" {
		return result()
	}
	clicked := 0
	for _, it := range packItems(d) {
		if !ids.Unnoted[int(it.ID)] {
			continue
		}
		if err := actions.Open(d.Session, it.ID, it.Slot, PlayerPack); err != nil {
			d.logf("Open: %v", err)
			continue
		}
		clicked++
	}
	if clicked == 0 {
		return OpenBatchResult{Refused: true}
	}
	if !waitFor(d, "caskets to open", time.Duration(openTicks)*600*time.Millisecond, func() bool {
		return Openable(d, ids) < held
	}) {
		return OpenBatchResult{}
	}
	// Each opening ends on a reward box, and the next casket only opens once it is gone.
	dlg := actions.NewChatDialog(d.Session, d.Components)
	for i := 0; i < boxesPerPass && dlg.CanContinue(); i++ {
		_ = dlg.Continue()
		d.Delay(150 * time.Millisecond)
	}
	waitFor(d, "the batch to finish", time.Duration(openTicks)*600*time.Millisecond, func() bool {
		return Openable(d, ids) == 0
	})
	return result()
}

// ---- the bank ----

// OpenNearestBank mirrors every sibling runner's identical helper (a growing-radius,
// nearest-first, up-to-4-candidate retry — see docs/script_porting_framework.md's bug class
// #1).
func OpenNearestBank(d *Deps, radius int) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	here := currentTile(d)
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: here.Level, Radius: radius, Name: BoothName, Op: BoothOp}, live)
	if len(candidates) == 0 {
		d.logf("no bank booth within %d tiles — start at or near a bank", radius)
		return nil, false
	}
	const maxTries = 4
	for i, booth := range candidates {
		if i >= maxTries {
			return nil, false
		}
		// Onto a tile the booth answers from: the server ignores a click from a diagonal one in
		// silence (freshstartrunner.WalkToUse).
		if !freshstartrunner.WalkToUse(fresh(d), tile.Tile{X: booth.X, Z: booth.Z, Level: booth.Level}, walkNearTimeout) {
			continue
		}
		mark := actions.MarkBankOpen(d.Session)
		if err := actions.OpLoc(d.Session, booth.Op, booth.X, booth.Z, booth.ID); err != nil {
			d.logf("OpLoc: %v", err)
			continue
		}
		bank := actions.NewBank(d.Session, d.Components)
		if open, _ := bank.AwaitOpen(mark, actions.DelayWaiter(d.Delay), 20*time.Second); open {
			return bank, true
		}
	}
	return nil, false
}

type waiter struct{ d *Deps }

func (w waiter) Delay(t time.Duration) bool { return w.d.Delay(t) }
func (w waiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		if !w.d.Delay(100 * time.Millisecond) {
			return false
		}
	}
}
func (w waiter) DelayTicks(n int) bool { return w.d.Delay(time.Duration(n) * 600 * time.Millisecond) }

func Waiter(d *Deps) actions.Waiter { return waiter{d} }

func SetNoteMode(d *Deps, bank *actions.Bank, on bool) error {
	return bank.SetNoteMode(on, Waiter(d))
}

// DepositRewards deposits every pack item not in keep (by id) — the rewards, plus any noted
// caskets — and reports how many distinct ids moved.
func DepositRewards(d *Deps, bank *actions.Bank, keep map[int32]bool) int {
	before := len(packItems(d))
	if err := bank.BatchDepositAllMatching(func(it gamestate.SlotItem) bool { return !keep[it.ID] }, Waiter(d)); err != nil {
		d.logf("deposit: %v", err)
	}
	after := len(packItems(d))
	if after >= before {
		return 0
	}
	return before - after
}

// BankCasketCount reads how many unnoted caskets (any variant) an already-open bank holds.
func BankCasketCount(bank *actions.Bank, ids CasketIDs) int {
	n := 0
	for _, it := range bank.Items() {
		if ids.Unnoted[int(it.ID)] {
			n += int(it.Count)
		}
	}
	return n
}

// WithdrawCaskets withdraws every casket variant the bank holds ("Withdraw-All" per row),
// waiting for the pack to reflect each one.
func WithdrawCaskets(d *Deps, bank *actions.Bank, ids CasketIDs) int {
	if err := SetNoteMode(d, bank, false); err != nil {
		d.logf("set note mode: %v", err)
	}
	drawn := 0
	// Snapshot the rows first: withdrawing changes the bank's own slot layout mid-loop.
	var rows []int32
	for _, it := range bank.Items() {
		if ids.Unnoted[int(it.ID)] && it.Count > 0 {
			rows = append(rows, it.ID)
		}
	}
	for _, id := range rows {
		before := Openable(d, ids)
		if err := bank.WithdrawAll(id); err != nil {
			d.logf("withdraw all %d: %v", id, err)
			continue
		}
		if waitFor(d, "caskets in pack after withdrawing", 3*time.Second, func() bool {
			return Openable(d, ids) > before
		}) {
			drawn += Openable(d, ids) - before
		}
	}
	return drawn
}
