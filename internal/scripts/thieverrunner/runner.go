// Package thieverrunner is the I/O half of the ProgressiveThiever port: it drives the pure
// method-table/schedule/phase decisions in internal/scripts/thieverplan against a live
// session, following the same split internal/scripts/essencerunner and
// internal/scripts/woodcutrunner already proved. See docs/script_porting_framework.md for the
// recipe, and essencerunner's own doc comment for the primitive shapes duplicated here on
// purpose (WaitForGuardian/bank-booth retry).
//
// WalkNear is the one exception, and for the same reason woodcutrunner's own WalkNear is: this
// script's 24 methods are scattered across Lumbridge, Varrock, Ardougne, Yanille and the Grand
// Tree, several of them behind real closed doors/gates the static collision pack's own
// FindPath graph reports as traversable but the live server silently refuses to walk through
// (see woodcutrunner's own package doc comment for the Seers-area repro that first found this
// class of gap this same session). Rather than hand-porting a per-door fix for every one of
// this script's own routes, WalkNear here is a thin wrapper around
// internal/scripts/freshstartrunner.WalkNear, which already has the generic door-opening/
// level-changing recovery (tryOpenDoor/tryChangeLevel/tryClimbOver) every other fresh-account
// script's own travel already relies on.
//
// The appearance screen, the tutorial skip, the brightness press and the boat crossing are NOT
// re-implemented here — internal/scripts/freshstartrunner already has
// AcceptAppearance/SkipTutorial/BrightnessStep/SailLeg, shared with every other fresh-account
// script, and internal/bot/thiever.go calls those directly.
//
// Deliberately NOT ported here, and why:
//   - thieveHud.ts — the Electron wall's on-screen overlay; no headless equivalent.
//   - the update-checker and the bank/tally feed publishers.
//   - asking the mule for food when the bank has none (ProgressiveThiever.ts's own
//     askMuleForFood/orderStep). Same reasoning as woodcutrunner's own package doc comment:
//     internal/mulebus carries the transport (mulebus.OrderMessage), but MuleReceiver does not
//     fulfil item orders yet, so PhaseOrder here publishes the same message for forward
//     compatibility and logs a clear warning rather than claiming a round-trip this project
//     cannot actually drive live. An operator without order fulfilment keeps a run going by
//     leaving food in the account's own bank ahead of time.
//   - actively fleeing a stall's roused guard to m.Flee. randomevents.EmergencyFlee's own
//     InCombat branch already runs unconditionally inside WaitForGuardian/WalkNear regardless
//     of this package's driftTiles setting, and is this project's one generic "something
//     hostile is attacking, get away" answer everywhere else too — a second, stall-specific
//     evasion path layered on top is exactly the kind of duplicated policy
//     docs/script_porting_framework.md's own "why duplicated, not shared" section warns is a
//     liability once it disagrees with the shared one, not a safety net.
package thieverrunner

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
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
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/tile"
)

// ThievingStat is UPDATE_STAT's index for Thieving — confirmed against gamestate/skills.go's
// own names table (Attack,Defence,Strength,Hitpoints,Ranged,Prayer,Magic,Cooking,Woodcutting,
// Fletching,Fishing,Firemaking,Crafting,Smithing,Mining,Herblore,Agility,Thieving,...): index 17.
const ThievingStat = 17

// PlayerPack is the persistent inventory-tab component id, matching every sibling runner's own
// PlayerPack.
const PlayerPack = 3214

// PackSlots is a full pack.
const PackSlots = 28

// Delayer sleeps for d, returning false early if the caller should stop.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration — for a standalone tool with nothing to cancel.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one steal/bank/travel/hand-off step needs from the outside world.
type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	Log        func(string)
	// Loot receives what each successful pickpocket really took (Pickpocket); nil records
	// nothing. Set from bot.Context.Loot.
	Loot func(loot.Drop)
	// OnStun, when set, spends the wait a failed pickpocket's stun holds the caller for (Attempt): it
	// is handed the whole wait and must not return before it is over, but may use it (the endgame casts
	// High Level Alchemy inside it: alchemy.rs2 never reads %stunned). Nil is a plain Delay.
	OnStun func(wait time.Duration)
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

// emergencyFleeDriftTiles is 0 — this script has no single fixed "home" (it works whichever of
// 24 different anchors the schedule names, from Lumbridge to Yanille to the Grand Tree).
const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint

const guardianWaitLogEvery = 10 * time.Second

// WaitForGuardian pauses while a random event is detected at all — mirrors every sibling
// runner's own WaitForGuardian exactly.
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

// fresh adapts d to freshstartrunner.Deps — same field-for-field copy every sibling runner
// that calls into freshstartrunner already does (see e.g. crafterrunner.Fresh,
// woodcutrunner.fresh).
func fresh(d *Deps) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay), Log: d.Log,
		Loot: d.Loot,
	}
}

// WalkNear paths to dest and walks there, stopping once within radius tiles of dest — a thin
// wrapper around freshstartrunner.WalkNear; see this package's own doc comment for why.
func WalkNear(d *Deps, dest tile.Tile, radius int) bool {
	return freshstartrunner.WalkNear(fresh(d), dest, radius, walkNearTimeout)
}

// ---- live reads ----

// ThievingLevel is the account's live Thieving level.
func ThievingLevel(d *Deps) int {
	skill, ok := d.Session.State().Skill(ThievingStat)
	if !ok {
		return 0
	}
	return skill.Level
}

// ThievingXP is the account's live Thieving experience.
func ThievingXP(d *Deps) int {
	skill, ok := d.Session.State().Skill(ThievingStat)
	if !ok {
		return 0
	}
	return skill.XP
}

func packCountByName(d *Deps, name string) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(strings.TrimSpace(obj.Name), name) {
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// CoinsHeld is the player's own pack coins.
func CoinsHeld(d *Deps) int { return packCountByName(d, freshstart.Coins) }

// FoodCount is edible food in the pack matching want — notes do not count, matching
// ProgressiveThiever.ts's own foodCount().
func FoodCount(d *Deps, want string) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Noted() || !fighterfood.IsFoodItem(obj.Name, want) {
			continue
		}
		n++
	}
	return n
}

// NotedFoodCount is want held as a note — the mule's delivery, matching notedFoodCount().
func NotedFoodCount(d *Deps, want string) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !obj.Noted() || !fighterfood.IsFoodItem(obj.Name, want) {
			continue
		}
		n++
	}
	return n
}

// LootSlots are pack slots holding something worth a bank trip: not food, not a lockpick —
// matching lootSlots().
func LootSlots(d *Deps, food string) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok {
			continue
		}
		if strings.EqualFold(obj.Name, food) || strings.EqualFold(obj.Name, "Lockpick") {
			continue
		}
		n++
	}
	return n
}

// PackFull reports whether the pack holds PackSlots non-empty slots.
func PackFull(d *Deps) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	return inv != nil && len(inv.NonEmpty()) >= PackSlots
}

// FreeSlots is the pack's empty slots.
func FreeSlots(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return PackSlots
	}
	free := PackSlots - len(inv.NonEmpty())
	if free < 0 {
		return 0
	}
	return free
}

// PackSlotRows reads the pack one row per slot (not merged by name), as thieverplan.MuleHaul/
// MuleLoad need.
func PackSlotRows(d *Deps) []donorplan.ItemRow {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	var out []donorplan.ItemRow
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		out = append(out, donorplan.ItemRow{Name: obj.Name, ID: int(it.ID), Count: c})
	}
	return out
}

// BankItemRows reads an already-open bank's contents as donorplan.ItemRow, merged by name.
func BankItemRows(d *Deps, bank *actions.Bank) []donorplan.ItemRow {
	type key struct {
		name string
		id   int
	}
	totals := map[key]int{}
	var order []key
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" {
			continue
		}
		k := key{name: obj.Name, id: int(it.ID)}
		if _, seen := totals[k]; !seen {
			order = append(order, k)
		}
		c := it.Count
		if c < 1 {
			c = 1
		}
		totals[k] += int(c)
	}
	out := make([]donorplan.ItemRow, 0, len(order))
	for _, k := range order {
		out = append(out, donorplan.ItemRow{Name: k.name, ID: k.id, Count: totals[k]})
	}
	return out
}

// BankFoodCount counts food in an already-open bank.
func BankFoodCount(d *Deps, bank *actions.Bank, want string) int {
	n := 0
	for _, it := range bank.Items() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && fighterfood.IsFoodItem(obj.Name, want) {
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// ---- npcs ----

// pickpocketTargets gathers every visible NPC matching one of names into freshstart.NPC rows,
// for thieverplan.ChooseTarget's own sort/leash/level filter.
func pickpocketTargets(d *Deps, names []string) []freshstart.NPC {
	here := currentTile(d)
	var out []freshstart.NPC
	for _, n := range d.Session.State().NPCs() {
		npcType, found := d.Config.Npc(n.Type)
		if !found {
			continue
		}
		match := false
		for _, want := range names {
			if strings.EqualFold(npcType.Name, want) {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		out = append(out, freshstart.NPC{
			Name: npcType.Name, Tile: tile.Tile{X: n.X, Z: n.Z, Level: n.Level},
			Distance: tile.Chebyshev(here, tile.Tile{X: n.X, Z: n.Z, Level: n.Level}),
			Ops:      npcType.Ops[:],
		})
	}
	return out
}

// reachableTargets drops the targets the baked pack has no path to. Live 7 Oct 2026: the East
// Ardougne Men wander into a house whose door is a thieving lock (loc_2556: Open only from inside,
// Pick Lock at Thieving 13 from outside), and eight accounts stood outside it clicking Open until
// the Guardian called them STRANDED. A Man that walks back out is a target again on the next poll.
func reachableTargets(d *Deps, targets []freshstart.NPC) []freshstart.NPC {
	if d.Pack == nil {
		return targets
	}
	here := currentTile(d)
	// The doors as the server has them now: the bake has every door shut, and a Hero wandering through an open
	// one read as unreachable (live 12 October 2026: 1,333 "reachable: 0" waits in 90 minutes at the Heroes).
	pk := freshstartrunner.LiveNavigationPack(fresh(d))
	out := targets[:0:0]
	for _, t := range targets {
		if t.Distance <= 1 || t.Tile.Level != here.Level {
			out = append(out, t)
			continue
		}
		res := pk.ReachableWithin(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: t.Tile.X, Z: t.Tile.Z, Level: t.Tile.Level}, 32)
		if res.OK {
			out = append(out, t)
		}
	}
	return out
}

// unreachableAt names, for the log, the tile of the first target inside the leash the pack has no path to.
func unreachableAt(inView, reachable []freshstart.NPC, m thieverplan.Method) string {
	seen := map[tile.Tile]bool{}
	for _, t := range reachable {
		seen[t.Tile] = true
	}
	for _, t := range inView {
		if !seen[t.Tile] && t.Tile.Level == m.Anchor.Level && tile.Chebyshev(t.Tile, m.Anchor) <= m.Leash {
			return fmt.Sprintf("; one stands at %d,%d with no path from here", t.Tile.X, t.Tile.Z)
		}
	}
	return ""
}

// insideLeash counts the targets ChooseTarget could choose by place: on the anchor's level and within the leash.
func insideLeash(targets []freshstart.NPC, m thieverplan.Method) int {
	n := 0
	for _, t := range targets {
		if t.Tile.Level == m.Anchor.Level && tile.Chebyshev(t.Tile, m.Anchor) <= m.Leash {
			n++
		}
	}
	return n
}

func findNpcIndexByType(d *Deps, name string, near tile.Tile, radius int) (gamestate.NPC, bool) {
	best := gamestate.NPC{}
	bestDist := radius + 1
	found := false
	for _, n := range d.Session.State().NPCs() {
		npcType, ok := d.Config.Npc(n.Type)
		if !ok || !strings.EqualFold(npcType.Name, name) {
			continue
		}
		dist := tile.Chebyshev(near, tile.Tile{X: n.X, Z: n.Z, Level: n.Level})
		if dist <= radius && dist < bestDist {
			best, bestDist, found = n, dist, true
		}
	}
	return best, found
}

// ---- the attempt ----

// attemptTimeout bounds one click's confirmation wait, matching ProgressiveThiever.ts's own
// ATTEMPT_MS (2500ms).
const attemptTimeout = 2500 * time.Millisecond

var stunLinePattern = regexp.MustCompile(`(?i)been stunned|fail to pick`)

// tickMs is this project's own assumed tick length, matching every sibling runner package's
// wall-clock conversion (see freshstartrunner.stunFor's own doc comment).
const tickMs = 600 * time.Millisecond

// Attempt performs one click and confirms it by the Thieving XP rising — every success on this
// server does, pickpockets/stalls/chests alike, ported from ProgressiveThiever.ts's own
// attempt(), which explains why XP is the confirmation rather than a chat line: the Baker's
// stall never prints one at all. On a miss, if the chat log shows a stun line, this waits out
// stunTicks (the rung's own stun length — 8 for every rung but the Hero's 10) before returning,
// exactly like the TS original's own stunned()-gated wait.
//
// A chat that is up while the click waits for its answer (the level-up the last steal earned: it opens the tick after the xp and
// blocks the steal until it is closed) is answered HERE with its Continue, once, and the wait goes on. It used to end the attempt
// as a miss at once, so a caller with nothing to wait on clicked again in the same tick, with the client still showing the chat
// for the tick the server's IF_CLOSE takes (the essence miner's 8,400 clicks a second, docs/essence_mine_exit_2026-10-15.md);
// the miss also counted towards the stall's stand rotation. The callers' clicks go through the click gate (gatedNpc, gatedLoc:
// the same target at most once per actions.ClickGapTicks game ticks). A chat that was up when the click was sent is the click's
// to close, and a random event's dialogue is its handler's: neither is answered here.
func Attempt(d *Deps, stunTicks int, click func() error) bool {
	before := ThievingXP(d)
	st := d.Session.State()
	_, _, staleChat := st.Modals()
	staleTick := st.Tick()
	clickAt := time.Now()
	if err := click(); err != nil {
		d.logf("attempt: %v", err)
		d.Delay(2 * tickMs)
		return false
	}
	dlg := actions.NewChatDialog(d.Session, d.Components)
	deadline := time.Now().Add(attemptTimeout)
	stunned := false
	for time.Now().Before(deadline) {
		if ThievingXP(d) > before {
			return true
		}
		if dlg.CanContinue() {
			_, _, chat := st.Modals()
			closing := chat == staleChat && st.Tick()-staleTick < actions.HoldSettleTicks && time.Since(clickAt) < actions.HoldSettleTicks*tickMs
			if randomevents.Busy(d.Session) {
				break
			}
			if !closing {
				_ = dlg.Continue() // once per open chat: ResumePauseButton latches
			}
		}
		if stunLinePattern.MatchString(d.Chat.Since(clickAt)) {
			stunned = true
			break
		}
		if !d.Delay(150 * time.Millisecond) {
			return false
		}
	}
	if ThievingXP(d) > before {
		return true
	}
	if !stunned {
		stunned = stunLinePattern.MatchString(d.Chat.Since(clickAt))
	}
	if stunned {
		if stunTicks <= 0 {
			stunTicks = 8
		}
		wait := time.Duration(stunTicks)*tickMs + 1200*time.Millisecond
		if d.OnStun != nil {
			d.OnStun(wait)
		} else {
			d.Delay(wait)
		}
	}
	return false
}

// gatedNpc is the click Attempt sends at an NPC, through the click gate: the same NPC is clicked at most once per
// actions.ClickGapTicks game ticks however often the caller comes round.
func gatedNpc(d *Deps, op, index int) func() error {
	return func() error {
		return actions.GatedClick(d.Session, Waiter(d), actions.NpcKey(index), nil, func() error { return actions.OpNpc(d.Session, op, index) })
	}
}

// gatedLoc is gatedNpc for a stall or a chest.
func gatedLoc(d *Deps, op, x, z, id int) func() error {
	return func() error {
		return actions.GatedClick(d.Session, Waiter(d), actions.LocKey(x, z, id), nil, func() error { return actions.OpLoc(d.Session, op, x, z, id) })
	}
}

// ---- pickpocketing ----

// WalkToTargetBeyond mirrors freshstart.WalkToTargetBeyond: past this many tiles, walk to the
// target before clicking rather than clicking from wherever we stand.
const WalkToTargetBeyond = 8

// PickResult is one Pickpocket call's outcome.
type PickResult struct {
	Attempted bool
	Success   bool
	Reason    string // "no-target", "walked", "ok", "missed"
}

// Pickpocket finds the nearest reachable target matching m's own name list and pickpockets it
// once — ported from pickpocket().
func Pickpocket(d *Deps, m thieverplan.Method) PickResult {
	inView := pickpocketTargets(d, thieverplan.NPCNames(m))
	targets := reachableTargets(d, inView)
	idx := thieverplan.ChooseTarget(targets, thieverplan.NPCNames(m), m.Anchor, m.Leash)
	if idx == -1 {
		// What it could see, so the line says WHY nothing is to rob: none in view (look where they spawn: PickpocketRung's
		// seek), some in view but outside the leash, or some inside it that the baked pack has no path to.
		d.logf("nothing to rob within %d tiles of %d,%d — is %s really here? (in view: %d, inside the leash: %d, reachable: %d%s)",
			m.Leash, m.Anchor.X, m.Anchor.Z, m.Label, len(inView), insideLeash(inView, m), insideLeash(targets, m), unreachableAt(inView, targets, m))
		return PickResult{Reason: "no-target"}
	}
	target := targets[idx]
	if target.Distance > WalkToTargetBeyond {
		WalkNear(d, target.Tile, 1)
		return PickResult{Reason: "walked"}
	}
	npc, ok := findNpcIndexByType(d, target.Name, target.Tile, 3)
	if !ok {
		return PickResult{Reason: "no-target"}
	}
	npcType, _ := d.Config.Npc(npc.Type)
	op := cachecfg.OpIndex(npcType.Ops, thieverplan.PickpocketOp)
	if op == 0 {
		d.logf("%s has no %s option", npcType.Name, thieverplan.PickpocketOp)
		return PickResult{Reason: "no-target"}
	}
	var packBefore loot.Counts
	if d.Loot != nil {
		packBefore = loot.PackCounts(d.Session.State().Inventory(PlayerPack))
	}
	ok = Attempt(d, m.StunTicks, gatedNpc(d, op, npc.Index))
	if ok {
		freshstartrunner.RecordPickpocketLoot(fresh(d), npc.Type, npcType.Name, packBefore)
		return PickResult{Attempted: true, Success: true, Reason: "ok"}
	}
	return PickResult{Attempted: true, Reason: "missed"}
}

// ---- stalls ----

// RefusalsBeforeMoving: the merchant refuses whoever he can see; this many in a row and it is
// worth standing somewhere else, matching REFUSALS_BEFORE_MOVING.
const RefusalsBeforeMoving = 3

// StallResult is one StealStall call's outcome.
type StallResult struct {
	Attempted bool
	Success   bool
	Reason    string // "no-stall", "walked", "ok", "refused"
}

// StealStall steals once from m's stall, standing at m.Stands[standAt%len] — ported from
// stealFromStall(). Guard evasion is deliberately not reimplemented here (see this package's
// own doc comment); a stolen stall that rouses a guard is left to randomevents' own generic
// hostile-NPC handling.
func StealStall(d *Deps, m thieverplan.Method, standAt int) StallResult {
	here := currentTile(d)

	// APPROACH FIRST, THEN LOOK. The other way round — which this function did until
	// 2026-09-21 — cannot work from anywhere except on top of the stall, and it fails silently
	// with a message that says the opposite of what is actually wrong.
	//
	// MEASURED on the lab engine: the Ardougne bank at 2655,3283 is Chebyshev **27** from the
	// Baker's stall anchor at 2667,3310, and the search below is `m.Leash + 4` = **10**. So an
	// account standing at its own method's own BANK — the tile thieverplan.Method.Bank names,
	// where every bank trip and every recovery leaves it — searched a 10-tile radius around
	// ITSELF, found no stall, logged "waiting for the baker's stall to restock" and returned
	// before ever reaching the walk below. Forever. cmd/thievtest's own phase 2 resets to
	// exactly that bank tile, which is why the stall proof had never once completed; the
	// previous session recorded that as "the local engine's reliability issue", and the engine
	// was not the problem.
	//
	// The same shape strands a live account after any displacement, and a random-event flee —
	// the ordinary one — happened in the very run that found this.
	if len(m.Stands) > 0 {
		stand := m.Stands[standAt%len(m.Stands)]
		if tile.Chebyshev(here, stand) > 1 {
			if !WalkNear(d, stand, 0) {
				// Say which of the two things went wrong. "Could not get there" and "not
				// restocked yet" are different problems and only one of them clears on its own.
				d.logf("could not reach the %s stand at %d,%d (from %d,%d)", strings.ToLower(m.Loc), stand.X, stand.Z, here.X, here.Z)
				return StallResult{Reason: "unreachable"}
			}
			return StallResult{Reason: "walked"}
		}
	}

	// Anchored on the METHOD, not on wherever the account happens to be standing, so the radius
	// means "this stall's own patch" rather than "whatever I can see from here".
	loc, ok := d.Locs.Nearest(d.Config, locindex.Query{X: m.Anchor.X, Z: m.Anchor.Z, Level: m.Anchor.Level, Radius: m.Leash + 4, Name: m.Loc, Op: m.Op}, locindex.ChangesFrom(d.Session.State().LocChanges()))
	if !ok {
		if line := restockLine(d.Session, strings.ToLower(m.Loc)); line != "" { // once per half minute per account
			d.logf("%s", line)
		}
		return StallResult{Reason: "no-stall"}
	}
	var packBefore loot.Counts
	if d.Loot != nil {
		packBefore = loot.PackCounts(d.Session.State().Inventory(PlayerPack))
	}
	ok = Attempt(d, 0, gatedLoc(d, loc.Op, loc.X, loc.Z, loc.ID))
	if ok {
		recordLocLoot(d, loc.ID, m.Loc, packBefore)
		return StallResult{Attempted: true, Success: true, Reason: "ok"}
	}
	return StallResult{Attempted: true, Reason: "refused"}
}

// locLootWait is how long to watch the pack for what a stall or chest gave, matching the
// pickpocket path's own wait: the take arrives a tick or two after the click, and a fixed
// sleep would either miss it or cost the steal rate.
const locLootWait = 2 * time.Second

// recordLocLoot reports one successful steal from a loc and whatever it put in the pack.
//
// This is the stall/chest half of the loot tracker, and it did not exist before 2026-09-22:
// Pickpocket beside it recorded every take while StealStall and SearchChest recorded nothing,
// because internal/loot had no Source that could describe a loc at all (see loot.KindLoc).
// ScarabSwarm's Stage 2 works the Baker's stall for its entire larder, so every one of those
// steals was invisible on the panel.
//
// Nil-safe: an account with no loot sink (every cmd/ harness) does not even read the pack.
func recordLocLoot(d *Deps, locID int, fallbackName string, packBefore loot.Counts) {
	if d.Loot == nil {
		return
	}
	read := func() *gamestate.Inventory { return d.Session.State().Inventory(PlayerPack) }
	// Skips a partly eaten food the window saw arrive: no stall or chest gives one, so it is the
	// account eating, not the steal (loot.IsEatenLeftover).
	gained := loot.WaitGained(read, packBefore, d.Delay, locLootWait, loot.IsEatenLeftover)
	d.Loot(loot.Drop{
		Source: loot.Stall(d.Config, locID, fallbackName),
		Events: 1,
		Items:  loot.Items(d.Config, gained),
	})
}

// ---- chests ----

// ChestResult is one SearchChest call's outcome.
type ChestResult struct {
	Attempted bool
	Success   bool
	Reason    string // "no-lockpick", "no-chest", "ok", "missed"
}

// SearchChest searches m's own chest, found by its exact tile (every trapped chest on this
// server is called "Chest") — ported from searchChest().
func SearchChest(d *Deps, m thieverplan.Method) ChestResult {
	if m.NeedsLockpick && packCountByName(d, "Lockpick") == 0 {
		d.logf("%s is locked and there is no lockpick in the pack", m.Label)
		return ChestResult{Reason: "no-lockpick"}
	}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	chest, ok := d.Locs.Nearest(d.Config, locindex.Query{X: m.Tile.X, Z: m.Tile.Z, Level: m.Tile.Level, Radius: 0, Name: m.Loc, Op: m.Op}, live)
	if !ok {
		return ChestResult{Reason: "no-chest"}
	}
	here := currentTile(d)
	if tile.Chebyshev(here, m.Tile) > 1 {
		if !WalkNear(d, m.Tile, 1) {
			return ChestResult{Reason: "no-chest"}
		}
	}
	var packBefore loot.Counts
	if d.Loot != nil {
		packBefore = loot.PackCounts(d.Session.State().Inventory(PlayerPack))
	}
	ok = Attempt(d, 0, gatedLoc(d, chest.Op, chest.X, chest.Z, chest.ID))
	if ok {
		// Recorded before the walk-back log, so a chest that teleports the account still has
		// its take attributed to the chest rather than to wherever it lands.
		recordLocLoot(d, chest.ID, m.Loc, packBefore)
		if m.LandsAt != nil {
			d.logf("the chest moved us — walking back from %d,%d", currentTile(d).X, currentTile(d).Z)
		}
		return ChestResult{Attempted: true, Success: true, Reason: "ok"}
	}
	return ChestResult{Attempted: true, Reason: "missed"}
}

// ---- food ----

// EatConfirmWait bounds the first wait for a bite to show, HP up or the food gone from its slot. It is
// actions.BiteConfirmWait; the wait does not end the watch (actions.EatBite credits a bite that lands later).
//
// It was 2.4 seconds until 12 Oct 2026, and that was shorter than a bite takes to come back on a slow
// route. The archived 11 Oct 12:00-24:00 log has 5,070 "a bite ... did not register" lines in 2,016
// runs, and of the 3,054 consecutive pairs in those runs, 3,044 (99.7%) show hitpoints HIGHER at the
// next bite than at the last: the bites had landed, and the confirmation gave up before the answer
// arrived. The answer is the click's trip to the server (half a round trip: the fleet's Psiphon
// routes pass a latency gate of up to 2.5 s), the wait for the next tick, and the stat and
// inventory update's trip back. Each false failure then held the account off everything that hurts
// for 30 seconds (scarabplan.BiteFailHold) and walked it to the bank.
//
// Six seconds was not enough either: 15 Oct 2026 13:05-14:00 has 104 "timed out waiting for the bite to
// register" from 56 accounts, and wherever one account failed twice within 20 seconds (20 pairs) the
// hitpoints stood one food higher at the second bite, seven to nine seconds after the click. See
// actions/bite.go for what the engine does with an eat and for the ledger that now carries a bite past
// its wait.
//
// A confirmed bite returns the moment it shows, so only a bite that really did not land waits the
// whole of it. EatSlowBite is the share of that wait worth a log line.
const EatConfirmWait = actions.BiteConfirmWait

// EatSlowBite is how long a bite may take to show before the confirmation says so in the log: the
// old 2.4 s confirmation would have called such a bite a failure.
const EatSlowBite = actions.BiteSlowAfter

// EatFood eats one pack item named want through its Eat op and waits for HP to rise or the food to
// leave its slot (EatConfirmWait). A bite that has not shown by then stays in actions' ledger: the
// next EatFood (or scarabrunner.EatBakeryFood) on the session credits it if it landed, without a
// second click, and a caller asks actions.BiteInFlight what a false means.
func EatFood(d *Deps, want string) bool {
	if d.Session.State().Inventory(PlayerPack) == nil {
		return false
	}
	return actions.EatBite(biteEnv(d), actions.BiteRequest{
		Food: want,
		Pick: func() (actions.BitePick, bool) {
			inv := d.Session.State().Inventory(PlayerPack)
			if inv == nil {
				return actions.BitePick{}, false
			}
			slot, objID := -1, int32(0)
			for _, it := range inv.NonEmpty() {
				obj, ok := d.Config.Obj(int(it.ID))
				if ok && !obj.Noted() && fighterfood.IsFoodItem(obj.Name, want) {
					slot, objID = it.Slot, it.ID
					break
				}
			}
			if slot < 0 {
				d.logf("no %s in the pack to eat", want)
				return actions.BitePick{}, false
			}
			return actions.BitePick{Obj: objID, Slot: slot, Changed: func() bool { return eatSlotChanged(d, slot, objID) }}, true
		},
	})
}

// biteClock is the clock the bites are timed on; nil is the wall clock. Tests set it.
var biteClock func() time.Time

// biteEnv is what actions.EatBite borrows from d.
func biteEnv(d *Deps) actions.BiteEnv {
	return actions.BiteEnv{Session: d.Session, Now: biteClock, Delay: d.Delay, Logf: d.logf}
}

// LogSlowBite says so when a confirmed bite took EatSlowBite or longer to show, so the slow routes
// are countable in the log ("the bite of Shrimps took 3.1s to show").
func LogSlowBite(logf func(string, ...any), food string, took time.Duration) {
	actions.LogSlowBite(logf, food, took)
}

// eatSlotChanged reports whether pack slot no longer holds objID -- either emptied (consumed) or
// transformed in place (a whole Cake becomes a 2/3 cake in the SAME slot, so "slot is empty" is
// not sufficient on its own). Either is proof the server took the bite.
//
// Live-found 2026-09-19 by the Scarab Swarm session, which hit it at the Baker's stall and fixed
// it in its own scarabrunner copy while correctly leaving this one alone as another claim. The
// bug is real and general: EatFood confirmed a bite ONLY by waiting for HP to RISE, but an
// account eats precisely when something is hitting it, and a guard's 3-a-tick against a cake's 4
// means HP frequently never exceeds its pre-eat value at any sample. EatFood then timed out and
// reported failure while the food HAD been eaten -- observed live as an account sitting in its
// eat phase holding a full pack, "failing" to eat, over and over. It presents as starvation with
// food in hand, not as an error, which is what makes it worth fixing everywhere rather than only
// where it was first seen. ProgressiveThiever pickpockets, and a failed pickpocket rouses the
// guard, so this caller eats under fire too.
//
// HP rising is still accepted first, so the ordinary undamaged case is exactly as fast as before.
// A missing inventory reads as "not changed" rather than success: a read failure must never fake
// a confirmation.
func eatSlotChanged(d *Deps, slot int, objID int32) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return false
	}
	for _, it := range inv.NonEmpty() {
		if it.Slot == slot {
			return it.ID != objID
		}
	}
	return true
}

// ---- the bank ----

func openNearestBankBooth(d *Deps, x, z, level int, timeout time.Duration, ops func() []actions.BankOp) (*actions.Bank, bool) {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: thieverplan.BoothName, Op: thieverplan.BoothOp}, live)
	if len(candidates) == 0 {
		d.logf("could not find a bank booth near the bank")
		return nil, false
	}
	const maxTries = 4
	for i, booth := range candidates {
		if i >= maxTries {
			d.logf("gave up after %d bank booth candidates near %d,%d", maxTries, x, z)
			return nil, false
		}
		// Onto a tile the booth answers from: a diagonal one is ignored by the server in silence
		// (freshstartrunner.WalkToUse).
		if !freshstartrunner.WalkToUse(fresh(d), tile.Tile{X: booth.X, Z: booth.Z, Level: booth.Level}, walkNearTimeout) {
			// A stopping bot fails every candidate the same way, each with its own walk and search: three more
			// booths of the Catherby bank cost the 14 Oct stop (04:45:07) its extra seconds of "no path" lines.
			if !d.Delay(0) {
				return nil, false
			}
			continue
		}
		// The open hands the bank over the moment the open's own inventories have arrived
		// (actions.Bank.AwaitOpen), not after a fixed pause. With ops, the booth op goes out blind
		// with them right behind it, read the tick after the bank opens (actions.OpenBankBlindOps),
		// and the visit is confirmed once.
		var visit []actions.BankOp
		if ops != nil {
			visit = ops()
		}
		o := actions.OpenBankBlindOps(d.Session, d.Components, actions.BankBooth{Op: booth.Op, X: booth.X, Z: booth.Z, LocID: booth.ID}, visit, Waiter(d), timeout)
		if o.Bank != nil {
			if len(visit) > 0 && !o.Batch.OK() {
				d.logf("bank: %s", o)
			}
			return o.Bank, true
		}
		if errors.Is(o.Batch.Err, actions.ErrBankStopped) {
			return nil, false
		}
		if o.Batch.Err != nil && !errors.Is(o.Batch.Err, actions.ErrNotOnScreen) {
			d.logf("OpLoc: %v", o.Batch.Err)
			continue
		}
		d.logf("timed out waiting for bank to open")
	}
	return nil, false
}

// PackDeposits is a Deposit-All of every kind in the backpack match accepts, read now from the
// backpack component (the view a blind open's deposits name): for OpenBankAtWith.
func PackDeposits(d *Deps, match func(gamestate.SlotItem) bool) []actions.BankOp {
	inv := d.Session.State().Inventory(actions.PlayerPack)
	if inv == nil {
		return nil
	}
	var ops []actions.BankOp
	seen := map[int32]bool{}
	for _, it := range inv.NonEmpty() {
		if !seen[it.ID] && (match == nil || match(it)) {
			seen[it.ID] = true
			ops = append(ops, actions.DepositAllOp(it.ID))
		}
	}
	return ops
}

// OpenBankAt walks to bankTile and opens its booth, leaving it open for the caller to drive
// its own deposit/withdraw sequence — the same shape essencerunner.OpenVarrockBank/
// woodcutrunner.OpenSeersBank use, generalized to any of the schedule's own bank tiles
// (Varrock East, Ardougne, Yanille, the Grand Tree).
func OpenBankAt(d *Deps, bankTile tile.Tile) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	if !WalkNear(d, bankTile, 3) {
		return nil, false
	}
	x, z, level, _ := d.Session.State().Tile()
	// No fixed pause after the open (it was 500 ms): openNearestBankBooth hands the bank over once
	// the open's own inventories have arrived (actions.Bank.AwaitOpen).
	return openNearestBankBooth(d, x, z, level, 20*time.Second, nil)
}

// OpenBankAtWith is OpenBankAt opening blind: ops (read once the account stands at the booth, so
// they name the pack as it is then) go out right behind the booth click and are read the tick
// after the bank opens, then confirmed once, with whatever the blind burst missed sent again
// paced (actions.OpenBankBlindOps). The bank comes back open, its readings fresh.
func OpenBankAtWith(d *Deps, bankTile tile.Tile, ops func() []actions.BankOp) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	if !WalkNear(d, bankTile, 3) {
		return nil, false
	}
	x, z, level, _ := d.Session.State().Tile()
	return openNearestBankBooth(d, x, z, level, 20*time.Second, ops)
}

func packCountByID(items []gamestate.SlotItem, id int32) int32 {
	var n int32
	for _, it := range items {
		if it.ID == id {
			n += it.Count
		}
	}
	return n
}

func bankCountByID(items []gamestate.SlotItem, id int32) int32 {
	var n int32
	for _, it := range items {
		if it.ID == id {
			n += it.Count
		}
	}
	return n
}

// DepositAllExcept deposits every pack item not named in keep into an already-open bank and
// reports how many kinds went in. Every Deposit-All goes out together, five a tick, and is
// confirmed once (actions.Bank.Batch); a kind the batch could not confirm is deposited the old
// way, one at a time with its own wait.
func DepositAllExcept(d *Deps, bank *actions.Bank, keep []string) int {
	kept := make(map[string]bool, len(keep))
	for _, k := range keep {
		kept[strings.ToLower(strings.TrimSpace(k))] = true
	}
	var ops []actions.BankOp
	seen := map[int32]bool{}
	for _, it := range bank.Inventory() {
		if seen[it.ID] {
			continue
		}
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || kept[strings.ToLower(obj.Name)] {
			continue
		}
		seen[it.ID] = true
		ops = append(ops, actions.DepositAllOp(it.ID))
	}
	if len(ops) == 0 {
		return 0
	}
	r := bank.Batch(ops, Waiter(d), actions.BatchOptions{})
	if errors.Is(r.Err, actions.ErrBankStopped) {
		return 0
	}
	deposited := 0
	for _, op := range ops {
		if _, short := r.ShortOf(actions.BankOpDeposit, op.ID); r.Err == nil && !short {
			deposited++
			continue
		}
		if depositOneSlow(d, bank, op.ID) {
			deposited++
		}
	}
	return deposited
}

// depositOneSlow is the one-at-a-time Deposit-All of id with its own wait: DepositAllExcept's
// fallback for a kind the batch could not confirm.
func depositOneSlow(d *Deps, bank *actions.Bank, id int32) bool {
	for _, it := range bank.Inventory() {
		if it.ID != id {
			continue
		}
		obj, _ := d.Config.Obj(int(it.ID))
		before := bankCountByID(bank.Items(), it.ID)
		held := packCountByID(bank.Inventory(), it.ID)
		if err := bank.DepositAll(it.ID); err != nil {
			d.logf("deposit %q: %v", obj.Name, err)
			return false
		}
		return waitFor(d, fmt.Sprintf("bank shows %s after deposit", obj.Name), 5*time.Second, func() bool {
			return bankCountByID(bank.Items(), it.ID) > before || packCountByID(bank.Inventory(), it.ID) < held
		})
	}
	return true // the pack no longer holds any: nothing left to deposit
}

// waiter adapts a Deps' Delayer to actions.Waiter.
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
func (w waiter) DelayTicks(n int) bool { return w.d.Delay(time.Duration(n) * tickMs) }

// Waiter is d as an actions.Waiter.
func Waiter(d *Deps) actions.Waiter { return waiter{d} }

// SetNoteMode sets the open bank's withdraw mode.
func SetNoteMode(d *Deps, bank *actions.Bank, on bool) error {
	return bank.SetNoteMode(on, Waiter(d))
}

// WithdrawXOf withdraws exactly n of name (as items, never notes) from an already-open bank.
func WithdrawXOf(d *Deps, bank *actions.Bank, name string, n int) bool {
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Noted() || !fighterfood.IsFoodItem(obj.Name, name) {
			continue
		}
		before := FoodCount(d, name)
		if err := bank.QuickWithdrawX(it.ID, int32(n), Waiter(d)); err != nil {
			d.logf("withdraw %d %q: %v", n, name, err)
			return false
		}
		return waitFor(d, fmt.Sprintf("%s in pack after withdrawing", name), 4*time.Second, func() bool {
			return FoodCount(d, name) > before
		})
	}
	return false
}

// WithdrawAllByName withdraws every one of name with Withdraw-All and waits until the pack
// holds more of that name — for the mule hand-off, counting by name so a note (a different id)
// still confirms.
func WithdrawAllByName(d *Deps, bank *actions.Bank, name string) bool {
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !strings.EqualFold(obj.Name, name) {
			continue
		}
		before := packCountByName(d, name)
		used := len(bank.Inventory())
		if err := bank.WithdrawAll(it.ID); err != nil {
			d.logf("withdraw all %q: %v", name, err)
			return false
		}
		return waitFor(d, fmt.Sprintf("%s in pack after withdrawing", name), 3*time.Second, func() bool {
			return packCountByName(d, name) > before || len(bank.Inventory()) > used
		})
	}
	return false
}
