// Package lootchestfisherrunner is the I/O half of the LootChestFisher port: it drives the
// pure decisions in internal/scripts/lootchestfisherplan against a live session, following the
// split every sibling runner uses (docs/script_porting_framework.md).
//
// What lives here: the walking, the bank trips, the one use-key-on-chest interaction, and the
// measurement that turns a chest opening into a loot.Drop.
//
// Deliberately NOT here, and why:
//   - The mule hand-off. internal/scripts/donorrunner already owns both directions of it —
//     Run to give, Await to ask — and is script-agnostic on purpose. The bot adapter calls it.
//   - The fishing fallback. It is a genuine hand-over to the registered ProgressiveFisher bot
//     in internal/bot, not a reimplementation of fishing here.
//   - Long-distance route planning. freshstartrunner.WalkWithWalker (over internal/webwalk)
//     does the doors, the gates and the danger zones, exactly as ProgressiveFisher's own
//     travel does.
//   - Any HUD/overlay/tally publishing: no headless equivalent, same boundary as every other
//     ported script.
package lootchestfisherrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/lootchestfisherplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// PlayerPack is the persistent inventory-tab component id.
const PlayerPack = 3214

const (
	// BoothName/BoothOp are the Falador bank booth's own name and op.
	BoothName = "Bank booth"
	BoothOp   = "Use-quickly"
	// ChestName is the chest loc's cache name. Resolved BY NAME at its known tile rather
	// than by a hardcoded id: CLAUDE.md's OpLocU gotcha #1 is exactly this mistake, a loc
	// id hardcoded at a coordinate where content places a different loc, producing a
	// completely silent no-op.
	ChestName = "Closed chest"
	ChestOp   = "Open"
	// KeyName is the crystal key's cache name; there is also a noted variant.
	KeyName = "Crystal key"
	// CoinsName is how orderplan.DefaultCatalog spells the coin row, so a fare order the mule
	// will actually recognise uses this rather than a second spelling of the same word.
	CoinsName = "Coins"
)

type Delayer func(d time.Duration) bool

func RealDelay(d time.Duration) bool { time.Sleep(d); return true }

type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	Log        func(string)
	// Walker routes the long legs over webwalk's door/gate/danger-zone graph. Without it
	// travel falls back to plain same-level pathfinding, which cannot cross the Taverley
	// gate or the chest room's door and will simply report "unreachable".
	Walker *webwalk.Walker
	// Loot receives what a chest opening really yielded; nil records nothing.
	Loot func(loot.Drop)
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

// Here is the account's current tile, for a caller building a plan Snapshot.
func Here(d *Deps) tile.Tile { return currentTile(d) }

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

// emergencyFleeDriftTiles is 0 and emergencyFleeHome is the zero NavPoint, the same as
// donorrunner's and essencerunner's: this script has no single fixed home to measure drift
// from — it works at two ends of a long route — so a drift reference would be meaningless.
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

// walkNearTimeout is this project's floor for a "walk somewhere nearby" primitive — raised
// from 30s after live data showed accounts giving up at real, distance-appropriate waypoints
// (docs/script_porting_framework.md bug class #5).
const walkNearTimeout = 120 * time.Second

// TravelTimeout bounds one long leg. The measured Catherby-to-Falador plan is cost 380 over
// White Wolf Mountain, which is minutes of walking, plus whatever a wolf costs.
const TravelTimeout = 12 * time.Minute

func freshDeps(d *Deps) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay), Log: d.Log,
		Walker: d.Walker, Loot: d.Loot,
	}
}

// WalkNear walks to within radius of dest, routing over the webwalk graph so doors, gates and
// danger zones are handled. Short hops and long legs both go through it: the chest room's own
// door is a door either way.
func WalkNear(d *Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = walkNearTimeout
	}
	if tile.Chebyshev(currentTile(d), dest) <= radius {
		return true
	}
	return freshstartrunner.WalkWithWalker(freshDeps(d), dest, radius, timeout)
}

// ---- reading the pack ----

func packItems(d *Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// PackFree is how many backpack slots are empty. A pack the client cannot read at all reads
// as zero free rather than 28, so nothing opens a chest into an unknown pack.
func PackFree(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	return lootchestfisherplan.PackSlots - len(inv.NonEmpty())
}

// Held reports the pack's count of an obj id, for the plan's room guard.
func Held(d *Deps) func(id int) int {
	counts := map[int]int{}
	for _, it := range packItems(d) {
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		counts[int(it.ID)] += c
	}
	return func(id int) int { return counts[id] }
}

// KeyIDs is the unnoted and noted crystal key ids, resolved through the cache by name the way
// casketrunner.ResolveCasketIDs resolves "Casket" — several ids can share a display name and
// only the unnoted one can be used on anything.
type KeyIDs struct {
	Unnoted map[int]bool
	Noted   map[int]bool
	// First is the lowest unnoted id, for a withdraw.
	First int
}

// ResolveKeyIDs reports false when the cache has no unnoted "Crystal key" at all.
func ResolveKeyIDs(cfg *cachecfg.Config) (KeyIDs, bool) {
	out := KeyIDs{Unnoted: map[int]bool{}, Noted: map[int]bool{}, First: -1}
	if cfg == nil {
		return out, false
	}
	for _, id := range cfg.ObjIDs(KeyName) {
		obj, ok := cfg.Obj(id)
		if !ok {
			continue
		}
		if obj.Noted() {
			out.Noted[id] = true
			continue
		}
		out.Unnoted[id] = true
		if out.First == -1 || id < out.First {
			out.First = id
		}
	}
	return out, out.First != -1
}

// KeysInPack counts unnoted crystal keys held.
func KeysInPack(d *Deps, ids KeyIDs) int {
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

// NotedKeysInPack counts crystal keys held as BANK NOTES.
//
// Separate from KeysInPack on purpose, and both are needed. A note cannot be used on the
// chest, so the open guard must keep counting unnoted keys only — but a pack holding 400
// noted keys is very much not "out of keys", and reading it as zero sends the account back to
// fishing on top of the stock it just collected. Notes stack, which is the whole reason the
// mule hands them over this way: 400 keys ride home in one slot instead of needing fifteen
// round trips at a 28-slot pack. They become ordinary keys again simply by being banked,
// because a bank always stores the unnoted form.
func NotedKeysInPack(d *Deps, ids KeyIDs) int {
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

// LootInPack is how many slots hold something that is neither a crystal key nor named in
// keep. That is the haul: what a bank trip deposits and what a hand-off gives the mule.
func LootInPack(d *Deps, ids KeyIDs, keep map[int]bool) int {
	n := 0
	for _, it := range packItems(d) {
		id := int(it.ID)
		if ids.Unnoted[id] || ids.Noted[id] || keep[id] {
			continue
		}
		// AN UNTRADEABLE ITEM IS NOT A HAUL, because it can never be handed over.
		//
		// Counting one made spare104 loop at the mule post for the whole run, live on
		// 2026-09-22: LootInPack was 1, so PhaseHandoff opened a trade, donorplan skipped the
		// single group as untradeable, the offer was empty, the trade declined, and the count
		// was still 1 on the next pass. Its own log named the cause every fourteen seconds --
		// "the load it was sent to deliver is not offerable (1 group(s) already skipped as
		// untradeable)" -- and the account's bank was EMPTY while the mule held 106 crystal keys
		// it had come to collect.
		//
		// donorplan.Untradeable is the same 963-id set generated from content's tradeable=no
		// that the hand-over itself uses, so this cannot disagree with what the trade will do.
		// That is the point: the decision to GO and the decision to OFFER must read the same
		// rule, or the account is sent to deliver something it will then refuse to offer.
		if donorplan.Untradeable(id) {
			continue
		}
		n++
	}
	return n
}

// ---- the bank ----

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

// Waiter adapts this package's Delayer to actions.Waiter.
func Waiter(d *Deps) actions.Waiter { return waiter{d} }

// OpenNearestBank walks to and opens a bank booth, trying up to four nearest-first candidates
// rather than retrying one possibly-bad tile forever (docs/script_porting_framework.md bug
// class #1).
func OpenNearestBank(d *Deps, near tile.Tile, radius int) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive configured — cannot open the bank")
		return nil, false
	}
	if !WalkNear(d, near, 6, walkNearTimeout) {
		d.logf("could not reach the bank at %d,%d", near.X, near.Z)
		return nil, false
	}
	here := currentTile(d)
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: here.Level, Radius: radius, Name: BoothName, Op: BoothOp}, live)
	if len(candidates) == 0 {
		d.logf("no bank booth within %d tiles of %d,%d", radius, here.X, here.Z)
		return nil, false
	}
	const maxTries = 4
	for i, booth := range candidates {
		if i >= maxTries {
			return nil, false
		}
		// Onto a tile the booth answers from: the server ignores a click from a diagonal one in
		// silence (freshstartrunner.WalkToUse).
		if !freshstartrunner.WalkToUse(freshDeps(d), tile.Tile{X: booth.X, Z: booth.Z, Level: booth.Level}, walkNearTimeout) {
			continue
		}
		if err := actions.OpLoc(d.Session, booth.Op, booth.X, booth.Z, booth.ID); err != nil {
			d.logf("OpLoc on booth: %v", err)
			continue
		}
		bank := actions.NewBank(d.Session, d.Components)
		if waitFor(d, "the bank to open", 20*time.Second, bank.IsOpen) {
			d.Delay(500 * time.Millisecond)
			return bank, true
		}
	}
	return nil, false
}

// bankOpenTimeout bounds the wait for one booth to open the bank.
const bankOpenTimeout = 20 * time.Second

// OpenNearestBankFast is OpenNearestBank without its two fixed waits: the bank is waited for with
// actions.Bank.AwaitOpen, which returns the moment the open's own inventories have arrived (a tick
// or two after the booth op), instead of polling at a tenth of a second and then pausing another half
// second. A chest round opens this bank every trip. The walk and the booth choice are the same.
func OpenNearestBankFast(d *Deps, near tile.Tile, radius int) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive configured — cannot open the bank")
		return nil, false
	}
	if !WalkNear(d, near, 6, walkNearTimeout) {
		d.logf("could not reach the bank at %d,%d", near.X, near.Z)
		return nil, false
	}
	here := currentTile(d)
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: here.Level, Radius: radius, Name: BoothName, Op: BoothOp}, live)
	if len(candidates) == 0 {
		d.logf("no bank booth within %d tiles of %d,%d", radius, here.X, here.Z)
		return nil, false
	}
	const maxTries = 4
	for i, booth := range candidates {
		if i >= maxTries {
			return nil, false
		}
		// Onto a tile the booth answers from: the server ignores a click from a diagonal one in
		// silence (freshstartrunner.WalkToUse).
		if !freshstartrunner.WalkToUse(freshDeps(d), tile.Tile{X: booth.X, Z: booth.Z, Level: booth.Level}, walkNearTimeout) {
			continue
		}
		mark := actions.MarkBankOpen(d.Session)
		if err := actions.OpLoc(d.Session, booth.Op, booth.X, booth.Z, booth.ID); err != nil {
			d.logf("OpLoc on booth: %v", err)
			continue
		}
		bank := actions.NewBank(d.Session, d.Components)
		if open, _ := bank.AwaitOpen(mark, Waiter(d), bankOpenTimeout); open {
			return bank, true
		}
		if !d.Delay(0) {
			return nil, false
		}
	}
	return nil, false
}

// DepositAllExcept deposits every pack stack whose id is not in keep, and reports how many
// slots actually emptied — measured, not assumed, the same rule every receipt check here uses.
func DepositAllExcept(d *Deps, bank *actions.Bank, keep map[int]bool) int {
	before := len(packItems(d))
	if err := bank.BatchDepositAllMatching(func(it gamestate.SlotItem) bool { return !keep[int(it.ID)] }, Waiter(d)); err != nil {
		d.logf("deposit: %v", err)
	}
	after := len(packItems(d))
	if after >= before {
		return 0
	}
	return before - after
}

// BankKeyCount reads how many unnoted crystal keys an already-open bank holds.
func BankKeyCount(bank *actions.Bank, ids KeyIDs) int {
	n := 0
	for _, it := range bank.Items() {
		if ids.Unnoted[int(it.ID)] {
			n += int(it.Count)
		}
	}
	return n
}

// WithdrawKeys takes n crystal keys out of an open bank and waits for them to land, returning
// how many really arrived.
func WithdrawKeys(d *Deps, bank *actions.Bank, ids KeyIDs, n int) int {
	if n <= 0 {
		return 0
	}
	if err := bank.SetNoteMode(false, Waiter(d)); err != nil {
		d.logf("switching the bank to items: %v", err)
	}
	before := KeysInPack(d, ids)
	if err := bank.WithdrawX(int32(ids.First), int32(n), Waiter(d)); err != nil {
		d.logf("withdrawing %d crystal key(s): %v", n, err)
		return 0
	}
	waitFor(d, "crystal keys in the pack", 5*time.Second, func() bool { return KeysInPack(d, ids) >= before+n })
	got := KeysInPack(d, ids) - before
	if got < 0 {
		got = 0
	}
	return got
}

// ---- the chest ----

// FindChest resolves the chest loc at its known tile, by name and op, through the live loc
// index. It is looked up rather than hardcoded because the id at a coordinate is the exact
// thing that silently goes wrong (CLAUDE.md's OpLocU gotcha).
func FindChest(d *Deps) (locindex.Match, bool) {
	if d.Locs == nil || d.Config == nil {
		return locindex.Match{}, false
	}
	t := lootchestfisherplan.ChestTile
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	matches := d.Locs.Find(d.Config, locindex.Query{X: t.X, Z: t.Z, Level: t.Level, Radius: 1, Name: ChestName, Op: ChestOp}, live)
	for _, m := range matches {
		if m.X == t.X && m.Z == t.Z && m.Level == t.Level {
			return m, true
		}
	}
	return locindex.Match{}, false
}

// chestReopenWait is how long to wait for an already-open chest to revert to its closed loc.
// Content changes it for two ticks; this is generous.
const chestReopenWait = 6 * time.Second

// openTimeout bounds one open: content's [label,open_crystal_chest] is a p_arrivedelay, an
// animation and a loc_change with no long delay, so the key should be gone within a couple of
// ticks.
const openTimeout = 6 * time.Second

// OpenResult is one OpenChest call's outcome.
type OpenResult struct {
	// Opened is true only once the key is confirmed GONE from the pack — content deletes it
	// before it adds anything, so that is the one unambiguous sign the chest really opened.
	Opened bool
	// Gained is every item id whose pack count went up.
	Gained loot.Counts
	// Reason explains a failure.
	Reason string
	// Stopped is true when the delay asked to stop.
	Stopped bool
}

// OpenChest stands on the chest's one legal tile, uses a crystal key on it, and measures what
// arrived. It reports the drop through d.Loot as a loot.CrystalChest source.
//
// The caller must have checked lootchestfisherplan.CanOpenAnother first: content's inv_add
// puts a reward that does not fit on the floor, where nothing picks it up.
func OpenChest(d *Deps, ids KeyIDs) OpenResult {
	// An open chest is a DIFFERENT loc (content: loc_change(crystal_chestopen, 2)), so for two
	// ticks after an open there is no "Closed chest" at that tile at all. Wait it out rather
	// than reporting the chest missing, which is what a back-to-back second open would see.
	var chest locindex.Match
	var found bool
	waitFor(d, "the chest to close again", chestReopenWait, func() bool {
		chest, found = FindChest(d)
		return found
	})
	if !found {
		return OpenResult{Reason: "no \"" + ChestName + "\" with an Open option at " +
			fmt.Sprintf("%d,%d,%d", lootchestfisherplan.ChestTile.X, lootchestfisherplan.ChestTile.Z, lootchestfisherplan.ChestTile.Level)}
	}
	if KeysInPack(d, ids) == 0 {
		return OpenResult{Reason: "no crystal key in the pack"}
	}
	// The ONE tile the engine's reach test accepts. Radius 0: anywhere else and the OPLOCU is
	// ignored with no error a script that does not read MESSAGE_GAME would ever see. That
	// requirement is not negotiable and is deliberately not relaxed here.
	//
	// What IS relaxed is giving up when the long walk ends a couple of tiles short. Live
	// 2026-09-21: spare104's walk to 2914,3451 timed out after the full 120s having reached
	// 2912,3450 -- TWO tiles away -- and returned "could not reach the chest's own standing
	// tile"; the very next attempt covered those two tiles and opened the chest six seconds
	// later. The long walk crosses a door and about 150 tiles, so exhausting its budget near
	// the end is an ordinary outcome, and unwinding the whole phase for it is not.
	if !WalkNear(d, lootchestfisherplan.ChestStand, 0, walkNearTimeout) {
		if !closeApproachRetry(d) {
			return OpenResult{Reason: "could not reach the chest's own standing tile"}
		}
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	before := loot.PackCounts(d.Session.State().Inventory(PlayerPack))
	keysBefore := KeysInPack(d, ids)
	if err := eq.UseOnLoc(int32(ids.First), chest.X, chest.Z, chest.ID); err != nil {
		return OpenResult{Reason: "use key on chest: " + err.Error()}
	}
	if !waitFor(d, "the chest to open", openTimeout, func() bool { return KeysInPack(d, ids) < keysBefore }) {
		if !d.Delay(0) {
			return OpenResult{Stopped: true, Reason: "stopped"}
		}
		return OpenResult{Reason: "the key was never consumed — the chest did not open"}
	}
	// The reward lands in the same tick the key leaves, but the inventory update for it can
	// be processed a moment later; WaitGained returns as soon as anything has arrived.
	gained := loot.WaitGained(func() *gamestate.Inventory { return d.Session.State().Inventory(PlayerPack) },
		before, d.Delay, 2*time.Second, func(id int) bool { return ids.Unnoted[id] || ids.Noted[id] })
	if d.Loot != nil {
		d.Loot(loot.Drop{Source: loot.CrystalChest(), Events: 1, Items: loot.Items(d.Config, gained)})
	}
	return OpenResult{Opened: true, Gained: gained}
}

// AtFalador reports whether the account is at the Falador west bank.
func AtFalador(d *Deps) bool {
	here := currentTile(d)
	if tile.Chebyshev(here, lootchestfisherplan.FaladorBankStand) <= faladorRadius {
		return true
	}
	// Anywhere on the far END of the expedition counts, not just the bank floor.
	//
	// Live 2026-09-20, the stall this box exists for: spare104 opened its first chest, still
	// holding 5 keys and the haul, and this returned false because the chest sits 82 tiles
	// from the bank stand against a 12-tile radius. DecidePhase therefore took the "in
	// between" branch instead of the AtFalador one, and bankForTravelStep aimed at the MULE'S
	// bank in Catherby -- about 105 tiles away and far outside bankSearchRadius -- so it
	// logged "could not reach the bank at 2809,3441" and retried forever, five keys unspent.
	//
	// A box rather than a wider radius: the bank and the chest are 82 tiles apart, so any
	// single radius covering both from either end would also swallow ground that belongs to
	// neither. This spans the whole working area -- the Falador west bank, the Taverley gate
	// the route crosses, and the chest room -- and stops well short of Catherby (x 2809).
	return OnFaladorSide(here)
}

// OnFaladorSide is the box test above, pure so it can be checked against the script's own
// named tiles without a live session. See AtFalador for why it is a box.
func OnFaladorSide(t tile.Tile) bool {
	return t.Level == 0 &&
		t.X >= faladorSideMinX && t.X <= faladorSideMaxX &&
		t.Z >= faladorSideMinZ && t.Z <= faladorSideMaxZ
}

// closeApproachRetry finishes the last few tiles to ChestStand after the main walk ran out of
// budget near the end. It only ever runs when the account is ALREADY close, so it cannot turn a
// genuine failure -- stranded, wrong side of the door, somewhere else entirely -- into a long
// retry loop; that case still returns false immediately and the caller reports it.
func closeApproachRetry(d *Deps) bool {
	here := Here(d)
	if tile.Chebyshev(here, lootchestfisherplan.ChestStand) > closeApproachTiles {
		return false
	}
	for i := 0; i < closeApproachTries; i++ {
		d.logf("lootchest: %d tile(s) short of the chest stand after the walk timed out — finishing the approach (%d/%d)",
			tile.Chebyshev(Here(d), lootchestfisherplan.ChestStand), i+1, closeApproachTries)
		if WalkNear(d, lootchestfisherplan.ChestStand, 0, closeApproachTimeout) {
			return true
		}
	}
	return false
}

// closeApproachTiles/Tries/Timeout bound that retry. Eight tiles is well inside the chest room,
// so "close" cannot mean "outside the door"; two short attempts cost at most 30s against the
// 120s the failed walk already spent, and a third would be chasing something other than a
// timeout.
const (
	closeApproachTiles   = 8
	closeApproachTries   = 2
	closeApproachTimeout = 15 * time.Second
)

// The Falador-side working box. Measured against the tiles this script already names:
// FaladorBankStand 2946,3369, TaverleyGateEast/West 2936/2934,3451, ChestStand 2914,3451.
// Catherby's mule stand (2809,3441) is outside it by 71 tiles of x, which is the separation
// that matters -- the two ends must never both look true.
const (
	faladorSideMinX = 2880
	// 2990, not 2960: the account really does transit east of the bank stand on the way in,
	// observed live at 2963,3408 and 2964,3377 on 2026-09-21 walking in off the ship chain.
	// At a 2960 edge those tiles matched NEITHER end, which is the same shape as the bug this
	// box replaced. Still stops well short of the Falador EAST bank at 3010,3354, which belongs
	// to no part of this run.
	faladorSideMaxX = 2990
	faladorSideMinZ = 3350
	faladorSideMaxZ = 3470
)

// faladorRadius is how close counts as "at the Falador bank" — the booth row is five tiles
// wide and the stand is two north of it, so this covers the whole bank floor without
// stretching to the city gates.
const faladorRadius = 12

// ---- the expedition legs ----

// TravelLeg walks one of the two long legs between the mule's post and Falador.
//
// It exists because the ordinary safe walk cannot do it. Measured 2026-09-20
// (cmd/lootchesttest -mode longleg): a combat-3 account asked to walk the Catherby bank to the
// Falador west bank gives up in 16 seconds with "unreachable (best 134 tiles)". The automatic
// White Wolf Mountain den zones (webwalk's own wolfdens.go, on for every walk at every combat
// level) close the only land route, and the planner's alternative — cost 696 through two spirit
// trees — needs quest progress a fisher does not have. So with the zones on there is no route
// at all, and the script would simply never leave Catherby.
//
// The owner's own design is the answer: "banking them for safe travel". With NOTHING in the
// pack, the wolf pass costs at worst the walk — a death drops the account at Lumbridge, which is
// on the Falador side of the mountain and an easy walk from it. So:
//
//  1. Try the safe route first. If a route avoiding the dens exists (a higher-combat account, a
//     spirit tree an account does have), take it and never go near a wolf.
//  2. Only if that is unreachable, and only with an EMPTY PACK, walk it with the automatic
//     zones off and webwalk's own hostile break-off on.
//
// A pack that is not empty refuses step 2 and returns false, loudly. That is not a safety
// blanket: it is the whole premise of the design, and a caller that skipped its bank trip must
// not quietly gamble the haul on a level-73 wolf.
func TravelLeg(d *Deps, dest tile.Tile, radius int) bool {
	if d.Walker == nil {
		d.logf("travel: no webwalk walker — the long legs cannot be routed at all")
		return false
	}
	if WalkNear(d, dest, radius, TravelTimeout) {
		return true
	}
	held := len(packItems(d))
	if held > 0 {
		d.logf("travel: no route to %d,%d that avoids the White Wolf dens, and the pack holds %d slot(s) — refusing the pass until the pack is banked",
			dest.X, dest.Z, held)
		return false
	}
	d.logf("travel: no wolf-free route to %d,%d; taking the White Wolf pass with an empty pack, where a death costs only the walk",
		dest.X, dest.Z)
	res := d.Walker.WalkTo(webwalk.Point{X: dest.X, Z: dest.Z, Level: dest.Level}, webwalk.WalkOptions{
		Radius:           radius,
		Timeout:          TravelTimeout,
		NoAutomaticZones: true,
		AvoidHostiles:    true,
	})
	here := currentTile(d)
	arrived := tile.Chebyshev(here, dest) <= radius
	if !arrived {
		d.logf("travel: the pass did not get there either (%s: %s), stopped at %d,%d", res.Outcome, res.Reason, here.X, here.Z)
	}
	return arrived
}

// CoinsHeld is how many coins are in the pack.
func CoinsHeld(d *Deps) int {
	n := 0
	for _, it := range packItems(d) {
		if int(it.ID) == lootchestfisherplan.Coins {
			n += int(it.Count)
		}
	}
	return n
}

// EnsureFare tops the pack up to want coins from an already-open bank, and reports how many are
// held afterwards. It exists for the one leg that needs money: the wolf-free route between the
// mule's post and Falador is the ship chain, and a crossing without a fare strands the account
// on the wrong side. Withdrawing less than asked is not an error — whatever is banked is what
// there is.
func EnsureFare(d *Deps, bank *actions.Bank, want int) int {
	held := CoinsHeld(d)
	if held >= want || want <= 0 {
		return held
	}
	if err := bank.SetNoteMode(false, Waiter(d)); err != nil {
		d.logf("switching the bank to items for the fare: %v", err)
	}
	if err := bank.WithdrawX(int32(lootchestfisherplan.Coins), int32(want-held), Waiter(d)); err != nil {
		d.logf("withdrawing %d coin(s) for the ship fare: %v", want-held, err)
		return CoinsHeld(d)
	}
	waitFor(d, "the ship fare in the pack", 5*time.Second, func() bool { return CoinsHeld(d) > held })
	return CoinsHeld(d)
}
