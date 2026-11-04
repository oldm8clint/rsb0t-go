// Package essencerunner is the I/O half of the EssenceMiner port: it drives the pure
// stage/pickaxe/hand-off decisions in internal/scripts/essenceplan against a live session,
// following the exact split internal/scripts/fisherrunner and internal/scripts/crafterrunner
// already proved twice — a pure plan package decides, this package does the walking/
// clicking/banking, and internal/bot/essence.go wires the two together the way
// internal/bot/fisher.go wires fisherplan+fisherrunner. See docs/script_porting_framework.md
// for the recipe this package follows.
//
// Every blocking wait goes through a caller-supplied Delayer instead of time.Sleep directly,
// matching every sibling runner package, so internal/bot's Context.Delay can actually cancel
// a running miner mid-wait.
//
// Nurmof's pickaxe shop IS ported (BuyFromNurmof, below), including the Dwarven Mine trapdoor
// hop: NURMOF_VENDOR/PICKAXE_SHOP_COSTS were first thought unreachable (neither
// EssenceMiner.ts nor essencePlan.ts define them, and a grep of reference/rsb0t-scripts/ and
// the sibling rsb0t-scripts checkout found nothing) until a later grep across every sibling
// checkout this session had access to found the real @rsb0t/api package source itself in
// rsb0t-main/packages/rsb0t-api (src/bot/api/acquisition/ToolAcquire.ts) — the actual vendor
// tile, hop tile/loc/op, and real Nurmof list prices, now ported verbatim into
// essenceplan.NurmofStand/NurmofHopFrom/NurmofHopLoc/NurmofHopOp/PickaxeShopCost. If you hit a
// similar "this constant must live in an unreachable private package" wall on a future port,
// search every sibling checkout under the parent directory first — it may already be sitting
// there, the way it was here.
//
// Deliberately NOT ported here:
//   - the HUD/bank feed/tally (essenceHud.ts, bankFeed.ts, tallyFeed.ts) — no headless
//     equivalent, same boundary already drawn around every other ported script.
package essencerunner

import (
	"fmt"
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
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
)

// MiningStat is UPDATE_STAT's index for Mining, per the same PlayerStat enum
// fisherrunner.FishingStat/crafterrunner.CraftingStat cite (attack,defence,strength,
// hitpoints,ranged,prayer,magic,cooking,woodcutting,fletching,fishing,firemaking,crafting,
// smithing,mining,... — confirmed against gamestate/skills.go's own names table and
// COLLAB.md's live ::advancestat cross-check): mining is index 14.
const MiningStat = 14

// PlayerPack is the persistent inventory-tab component id — see fisherrunner.PlayerPack's own
// doc comment for why this fixed, always-current component is read instead of scanning every
// known inventory (a shopkeeper's stock, e.g. Nurmof's, would otherwise contaminate the count).
const PlayerPack = 3214

// Delayer sleeps for d, returning false early if the caller should stop instead of true once
// d has fully elapsed — the same shape every sibling runner package uses.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration — for a standalone tool with nothing to cancel.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one mine/bank/hand-off cycle needs from the outside world.
type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	// Loot receives what a rock really yielded, for the Loot panel's Mining card (see
	// minecount.go). Reporting only, and nil records nothing. internal/bot/essence.go passes
	// bot.Context.Loot.
	Loot func(loot.Drop)
	// Log receives one already-formatted line per notable event; nil drops them.
	Log func(string)
	// Manip, when set and Enabled, makes Mine write a knife-on-log batch after each roll so the rock rolls every 3 ticks
	// instead of every pickaxe-rate-plus-one (minemanip.go, internal/scripts/minemanipplan). Nil is the plain mine this
	// package always had.
	Manip *ManipConfig
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

// emergencyFleeDriftTiles is 0, the same choice internal/scripts/donorrunner makes and for the
// same reason (see its own doc comment on emergencyFleeDriftTiles): the miner has no single
// fixed "home" the way fisherrunner's Catherby camp does — it spends real time both at
// BankStand and inside the instanced mine, which is nowhere near it. EmergencyFlee's drift
// check is disabled by driftTiles<=0; only its InCombat branch still runs.
const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint // unused while emergencyFleeDriftTiles <= 0

const guardianWaitLogEvery = 10 * time.Second

// WaitForGuardian pauses while a random event is detected at all, so the caller never starts
// an interaction on top of one. Returns false if a stop was requested while waiting. Mirrors
// fisherrunner.WaitForGuardian/barbarianrunner.WaitForGuardian exactly.
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

// walkNearTimeout bounds WalkNear's entire operation — see fisherrunner.walkNearTimeout's own
// doc comment for the live evidence behind 120s rather than a shorter cap.
const walkNearTimeout = 120 * time.Second

// WalkNear paths to dest and walks there (never running), re-clicking from wherever the
// player stops, stopping once within radius tiles of dest. Returns false on a stop request,
// an unreachable destination, or the overall timeout. Duplicated per this project's
// established per-package convention (see fisherrunner.WalkNear's own doc comment) rather
// than shared, since each runner's own EmergencyFlee reference point differs.
func WalkNear(d *Deps, dest tile.Tile, radius int) bool {
	return walkNearFor(d, dest, radius, walkNearTimeout)
}

// walkNearFor is WalkNear with its own overall timeout, for a short step that must not hold the account for two minutes.
func walkNearFor(d *Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
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
		wp := res.Waypoints[len(res.Waypoints)-1]
		closeWalkModal(d)
		d.logf("walk from %d,%d toward %d,%d (%d corners)", here.X, here.Z, wp.X, wp.Z, len(res.Waypoints))
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
	here := currentTile(d)
	d.logf("gave up walking; stopped at %d,%d,%d", here.X, here.Z, here.Level)
	return false
}

// closeWalkModal answers a main or chat modal left open before a walk is sent: a courtesy, NOT the cure for the 15 Oct 2026
// strand that put it here. Engine-TS does refuse a step while the player has a modal open and anything queued
// (Player.updateMovement), but a MOVE_GAMECLICK closes the modal itself (MoveClickHandler: clearPendingAction), so a walk is
// never held by a chat it can close. What held the Mining 3 Guild miner for ten minutes, twice, was its own clicks on the
// rock queued in front of the walk, the Continue and every CLOSE_MODAL (see mine.go: the lab reproduced it and the fix is
// there). A modal a random event is still using is left to its handler.
func closeWalkModal(d *Deps) {
	main, _, chat := d.Session.State().Modals()
	if main == gamestate.NoModal && chat == gamestate.NoModal {
		return
	}
	if randomevents.Busy(d.Session) {
		return
	}
	// A message box or level-up chat is a script paused on its pause button (content's ~mesbox and @levelup end in
	// p_pausebutton), and the client answers it with "Click here to continue", not CLOSE_MODAL. (Live 15 Oct 2026 the
	// Mining 3 miner closed the "inventory is too full" box 949 times in an hour with CLOSE_MODAL and the box came back:
	// each close was read behind thousands of queued clicks on the rock, each of which reopened it.)
	if dlg := actions.NewChatDialog(d.Session, d.Components); chat != gamestate.NoModal && d.Components != nil && dlg.CanContinue() {
		if err := dlg.Continue(); err == nil {
			d.logf("walk: pressed continue on the chat %d left open before the walk", chat)
			d.Delay(600 * time.Millisecond)
			return
		}
	}
	if err := actions.CloseModal(d.Session); err != nil {
		d.logf("walk: closing the modal left open (main %d, chat %d): %v", main, chat, err)
		return
	}
	d.logf("walk: closed a modal left open before the walk (main %d, chat %d)", main, chat)
}

// fresh adapts d to freshstartrunner.Deps, the same field-for-field copy as thieverrunner.fresh,
// for freshstartrunner.WalkToUse.
func fresh(d *Deps) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay), Log: d.Log,
		Loot: d.Loot,
	}
}

// ---- live reads ----

// MiningLevel is the account's live Mining level, straight off the last UPDATE_STAT for
// MiningStat. Returns 0 if the server has never sent one yet.
func MiningLevel(d *Deps) int {
	skill, ok := d.Session.State().Skill(MiningStat)
	if !ok {
		return 0
	}
	return skill.Level
}

// MiningXP is the account's live Mining experience.
func MiningXP(d *Deps) int {
	skill, ok := d.Session.State().Skill(MiningStat)
	if !ok {
		return 0
	}
	return skill.XP
}

// InMine reports whether the account is currently inside the instanced essence mine.
func InMine(d *Deps) bool {
	x, z, level, ok := d.Session.State().Tile()
	if !ok {
		return false
	}
	t := tile.Tile{X: x, Z: z, Level: level}
	return essenceplan.InEssenceMine(&t)
}

// ContainersReadable reports whether the backpack and worn panels resolve to real components
// right now, so a caller can tell "this account is holding nothing" apart from "this client
// cannot see what it is holding". Same shape and same reason as combatrunner.ContainersReadable:
// content nulls sidebar tabs 3 and 4 in ~macro_events_set_tabs (macro_events.rs2:218), in
// ~set_sail (interface_boat/scripts/sail.rs2) and in ~set_sail_cairn/captain_shanks. The two
// windows are nothing like the same length: a mime restores nothing until its mapzoneexit, while
// a crossing gets the tabs back at ~initalltabs before the arrival telejump, so that window is
// the route's own p_delay and not the voyage. Short is not the same as harmless — a
// WaitForGuardian return was measured with each window still open — so a caller that must not
// read "unknown" as "nothing" asks this rather than assuming the event is over.
//
// internal/bot/essence.go's containersBlind calls this once a pass, before the stage is
// derived. That one site is where a blind read changes a decision: it hides a WIELDED pickaxe,
// so ResolvePick never returns PickHeld and DecideStage answers StagePickaxe instead of
// StageTeleport, costing a pointless bank trip. It is wired as a bounded wait rather than a
// gate — see containersBlind for why this script must never refuse to make progress.
//
// What protects the deposit path is a different mechanism, HeldNames' PlayerPack read, set out
// on HeldNames itself.
func ContainersReadable(d *Deps) bool {
	return actions.NewEquipment(d.Session, d.Components).Readable()
}

// HeldNames is every named item the account currently wears or carries — ported from
// heldNames() (Equipment.items() ++ Inventory.items()).
//
// The pack half names PlayerPack directly, and the stakes for getting it wrong are high: this
// feeds essenceBot.keepInPack, which is the keep list DepositAllExcept works from, so a read that
// comes back empty while the account is blinded leaves the keep list empty and the bank trip
// deposits the one tool the account needs to mine at all. PlayerPack cannot go blind — Player.setTab
// (engine's src/engine/entity/Player.ts:2121) only reassigns this.tabs[tab] and writes IF_SETTAB,
// never invStopListenOnCom, so ~initalltabs's inv_transmit(inv, inventory:inv) listener survives
// and UPDATE_INV keeps flowing to 3214 throughout.
//
// actions.Equipment.Inventory() used to return nil in that state; it now falls back to this same
// component (see actions.PlayerPack), so the two routes agree. This one stays direct because it
// says plainly which component it means.
//
// The worn half goes through Equipment.Worn(), which reads the fixed PlayerWorn component when
// tab 4 is nulled, so a blinded worn read no longer hides a WIELDED pickaxe either. Every pickaxe
// tier in this content build is wieldable (skill_mining/configs/pickaxes.obj gives all six
// wearpos=righthand and iop2=Wield), so that gap used to cost a redundant withdrawal of a second
// pickaxe; it is closed rather than merely cheap now.
func HeldNames(d *Deps) []string {
	eq := actions.NewEquipment(d.Session, d.Components)
	var out []string
	for _, it := range eq.Worn() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Name != "" {
			out = append(out, obj.Name)
		}
	}
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return out
	}
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Name != "" {
			out = append(out, obj.Name)
		}
	}
	return out
}

// EssenceHeld counts unnoted Rune essence currently in the pack.
func EssenceHeld(d *Deps) int {
	return PackCountByName(d, essenceplan.Essence)
}

// Coins is the player's own pack coins.
func Coins(d *Deps) int {
	return PackCountByName(d, "Coins")
}

// PackCountByName totals the pack's items named name (case-insensitive).
func PackCountByName(d *Deps, name string) int {
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

// PackFull reports whether the player's own pack holds essenceplan.PackSlots non-empty slots.
func PackFull(d *Deps) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	return inv != nil && len(inv.NonEmpty()) >= essenceplan.PackSlots
}

// FreeSlots is the pack's empty slots.
func FreeSlots(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return essenceplan.PackSlots
	}
	free := essenceplan.PackSlots - len(inv.NonEmpty())
	if free < 0 {
		return 0
	}
	return free
}

func itemRows(d *Deps, items []gamestate.SlotItem) []donorplan.ItemRow {
	type key struct {
		name string
		id   int
	}
	totals := map[key]int{}
	var order []key
	for _, it := range items {
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

// BankItemRows reads an already-open bank's contents as donorplan.ItemRow, for
// essenceplan.MuleRows.
func BankItemRows(d *Deps, bank *actions.Bank) []donorplan.ItemRow {
	return itemRows(d, bank.Items())
}

// BankNames is every distinct item name currently in an already-open bank.
func BankNames(d *Deps, bank *actions.Bank) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" || seen[obj.Name] {
			continue
		}
		seen[obj.Name] = true
		out = append(out, obj.Name)
	}
	return out
}

// PackSlotRows reads the player's own pack one row per slot (not merged by name), as
// essenceplan.MuleHaul/MuleLoad need — see fisherrunner.PackSlotRows' own doc comment for why
// a merged stack of unstackable items must not be counted as a single mule slot.
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

// ---- Aubury / teleport ----

// findNpcByName finds the nearest visible NPC whose cachecfg name matches, optionally
// filtered by having a specific op — duplicated per this project's established per-package
// convention (see fisherrunner.findNpcByName's own twin).
func findNpcByName(d *Deps, name string, near tile.Tile, radius int, requireOp *string) (gamestate.NPC, bool) {
	best := gamestate.NPC{}
	bestDist := radius + 1
	found := false
	for _, n := range d.Session.State().NPCs() {
		npcType, ok := d.Config.Npc(n.Type)
		if !ok || !strings.EqualFold(npcType.Name, name) {
			continue
		}
		if requireOp != nil && cachecfg.OpIndex(npcType.Ops, *requireOp) == 0 {
			continue
		}
		dist := tile.Chebyshev(near, tile.Tile{X: n.X, Z: n.Z, Level: n.Level})
		if dist <= radius && dist < bestDist {
			best, bestDist, found = n, dist, true
		}
	}
	return best, found
}

// FindAubury finds Aubury within essenceplan.AuburyLeash of essenceplan.AuburyTile, offering
// essenceplan.TeleportOp.
func FindAubury(d *Deps) (gamestate.NPC, bool) {
	return FindGateway(d, essenceplan.AuburyGateway())
}

// FindGateway finds the gateway's NPC within its leash of its stand, offering its teleport option.
func FindGateway(d *Deps, gw essenceplan.Gateway) (gamestate.NPC, bool) {
	op := gw.Op
	return findNpcByName(d, gw.NPC, gw.Stand, gw.Leash, &op)
}

// TeleportResult is one TeleportIn attempt's outcome.
type TeleportResult struct {
	// Landed is true once the account is confirmed inside the mine.
	Landed bool
	// QuestRefused is true when the gateway's chat line refusing the teleport (Rune Mysteries
	// incomplete) was seen after this attempt's click.
	QuestRefused bool
	// Reason says why a teleport that did not land did not: the refusal, a click that could not be
	// sent, no NPC in sight, the wait running out.
	Reason string
	// At is the tile the account landed on, Ticks the server ticks the teleport took (the cast holds
	// five), both zero unless it landed.
	At    tile.Tile
	Ticks int
	// Clicks is how many times the gateway was clicked, the first included.
	Clicks int
}

// teleportLandWait is how long a teleport is given to land; a refusal ends the wait at once.
const teleportLandWait = 15 * time.Second

// teleportReclickTicks is how long a click on the gateway goes unanswered before it is sent again.
// Engine-TS drops an OPNPC in silence while the player or the NPC is delayed (OpNpcHandler: "cannot
// interact while delayed", "cannot interact with delayed npcs"), so a click sent straight after the
// Magic guild door's open script, or while the wizard is busy, is lost and nothing ever lands. Live
// 14 Oct 2026: the main account's Distentor clicks failed 14 of 19 times, each costing the whole 15 s wait; the
// pre-reboot log shows the click right after a door crossing failing and the next one landing. A click
// sent while the teleport is already under way is dropped the same way (the cast's p_delay), so
// sending again does no harm.
const teleportReclickTicks = 4

// TeleportIn makes one attempt to reach Aubury and use her teleport, ported from teleportIn().
// Walks toward essenceplan.AuburyTile first if she isn't already visible.
func TeleportIn(d *Deps) TeleportResult {
	return TeleportInVia(d, essenceplan.AuburyGateway())
}

// TeleportInVia is TeleportIn through any gateway (the Wizards' Guild's Wizard Distentor, East Ardougne's
// Wizard Cromperty): TeleportInWith with the plain collision-pack walk, for a gateway the plain walk can reach
// (a gateway behind a door must be reached by the caller's own walk before this, or through TeleportInWith).
func TeleportInVia(d *Deps, gw essenceplan.Gateway) TeleportResult {
	return TeleportInWith(d, gw, TeleportOptions{})
}

// TeleportInWith makes one attempt to reach the gateway's NPC and use his teleport. The option is clicked from BESIDE him
// (approach.go: the engine takes a click on an NPC only from a tile that touches him, and Distentor, Aubury and Cromperty
// wander); the NPC is read again after every step. A click that goes unanswered teleportReclickTicks ticks is sent again, and
// one the game answered "I can't reach that!" at once, after stepping beside him again. What the account, the NPC and the game's
// chat looked like is logged for every click and for a teleport that does not land. A Rune Mysteries refusal in the chat ends the
// wait the moment it is seen, instead of after the whole landing wait. Without a collision pack it is the old single click.
func TeleportInWith(d *Deps, gw essenceplan.Gateway, opt TeleportOptions) TeleportResult {
	return teleportIn(d, gw, opt, teleportLandWait)
}

func teleportIn(d *Deps, gw essenceplan.Gateway, opt TeleportOptions, wait time.Duration) TeleportResult {
	ap := newApproacher(d, gw, opt)
	npc, why := ap.first()
	if why != "" {
		return TeleportResult{Reason: why}
	}
	clicks := 0
	click := func(npc gamestate.NPC, again string) (chatMark, error) {
		mark, here := markChat(d), currentTile(d)
		clicks++
		suffix := ""
		if again != "" {
			suffix = " (again: " + again + ")"
		}
		d.logf("teleport: click %d on %s (npc %d at %d,%d) from %d,%d, %s%s", clicks, gw.NPC, npc.Index, npc.X, npc.Z, here.X, here.Z, describeReach(d, here, npcTile(npc)), suffix)
		err := actions.OpNpc(d.Session, cachecfg.OpIndex(mustNpcOps(d, npc.Type), gw.Op), npc.Index)
		if err != nil {
			clicks--
			d.logf("OpNpc(%s) on %s: %v", gw.Op, gw.NPC, err)
		}
		return mark, err
	}
	clickAt, tick0 := time.Now(), d.Session.State().Tick()
	// Announced to the account's Guardian: the teleporter sets the account down on one of the mine's 22 spots, a landing no
	// fail-teleport table lists (live 14 Oct 2026: "2596,3087 -> 2897,4808 ... UNEXPLAINED" for the Distentor's teleport).
	// The window covers the resends' approach walks, which extend the wait.
	withdraw := randomevents.ExpectLandings(d.Session, "essence mine: "+gw.NPC+"'s teleport", wait+time.Minute,
		randomevents.LandingSquare(essenceplan.MineMapsquareX, essenceplan.MineMapsquareZ))
	if _, err := click(npc, ""); err != nil {
		withdraw()
		return TeleportResult{Reason: "the click on " + gw.NPC + " could not be sent: " + err.Error()}
	}
	reclick := func(r resend) (chatMark, bool) {
		return ap.again(r, func(n gamestate.NPC, again string) (chatMark, bool) {
			mark, err := click(n, again)
			return mark, err == nil
		})
	}
	res := awaitLanding(d, gw, clickAt, tick0, wait, reclick)
	if !res.Landed {
		withdraw()
	}
	if !res.Landed && !res.QuestRefused {
		d.logf("teleport: %s did not take us after %d click(s) in %s: %s; the game said: %s", gw.NPC, res.Clicks, wait, whereAreWe(d, gw), saidSince(d, clickAt))
	}
	return res
}

// awaitLanding waits up to wait for the account to be seen inside the mine after a click at clickAt (server tick
// tick0). A refusal line ends it at once; the chat is read afresh every poll. reclick, when set, sends the click
// again every teleportReclickTicks server ticks without a landing (see there), and at once when the game answered
// the last click "I can't reach that!" (teleportMaxClicks in all). The time a reclick takes (it may step the account
// beside the NPC first) is not held against the wait.
func awaitLanding(d *Deps, gw essenceplan.Gateway, clickAt time.Time, tick0 int, wait time.Duration, reclick func(resend) (chatMark, bool)) TeleportResult {
	deadline := time.Now().Add(wait)
	clicks, lastClick, lastMark := 1, tick0, markChat(d)
	for time.Now().Before(deadline) {
		if InMine(d) {
			res := TeleportResult{Landed: true, At: currentTile(d), Ticks: d.Session.State().Tick() - tick0, Clicks: clicks}
			if clicks > 1 {
				d.logf("%s teleport landed after %d click(s), %d ticks after the first", gw.NPC, clicks, res.Ticks)
			}
			return res
		}
		if d.Chat != nil {
			if why := essenceplan.RefusalReason(d.Chat.Since(clickAt)); why != "" {
				d.logf("%s refused (%s) - failing at once, not waiting for the teleport", gw.NPC, why)
				return TeleportResult{QuestRefused: true, Reason: why, Clicks: clicks}
			}
		}
		if reclick != nil && clicks < teleportMaxClicks {
			now := d.Session.State().Tick()
			r := resend{CantReach: lastMark.cantReach(d), Ticks: now - lastClick}
			if r.CantReach || r.Ticks >= teleportReclickTicks {
				started := time.Now()
				mark, sent := reclick(r)
				deadline = deadline.Add(time.Since(started))
				lastClick, lastMark = d.Session.State().Tick(), markChat(d)
				if sent {
					clicks++
					lastMark = mark
				}
			}
		}
		if !d.Delay(100 * time.Millisecond) {
			return TeleportResult{Reason: "stopped", Clicks: clicks}
		}
	}
	d.logf("timed out waiting for the essence mine after teleporting (%d click(s) on %s)", clicks, gw.NPC)
	return TeleportResult{Reason: "the teleport did not land within " + wait.String(), Clicks: clicks}
}

func mustNpcOps(d *Deps, npcType int) [5]string {
	t, _ := d.Config.Npc(npcType)
	return t.Ops
}

// ---- portal ----

// PortalOut finds the portal loc and uses it, waiting up to 15s to confirm the account has
// left the mine — ported from portalOut().
func PortalOut(d *Deps) bool {
	here := currentTile(d)
	m, ok := d.Locs.Nearest(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: here.Level, Radius: 20, Name: essenceplan.PortalName, Op: essenceplan.PortalOp}, locindex.ChangesFrom(d.Session.State().LocChanges()))
	if !ok {
		d.logf("no portal in sight — waiting")
		return false
	}
	portalTile := tile.Tile{X: m.X, Z: m.Z, Level: m.Level}
	if tile.Chebyshev(here, portalTile) > 1 {
		WalkNear(d, portalTile, 1)
	}
	// Announced to the account's Guardian: the portal puts the account down beside the NPC that sent it in (essence_mine.rs2),
	// and which one that was is not known here, so every gateway's doorstep is expected (live 14 Oct 2026:
	// "2931,4854 -> 2590,3086 ... UNEXPLAINED" for the Distentor's portal).
	withdraw := randomevents.ExpectLandings(d.Session, "essence mine: the portal out", 25*time.Second, exitLandings()...)
	if err := actions.OpLoc(d.Session, m.Op, m.X, m.Z, m.ID); err != nil {
		withdraw()
		d.logf("OpLoc(Use) on the portal: %v", err)
		return false
	}
	if !waitFor(d, "leaving the mine through the portal", 15*time.Second, func() bool { return !InMine(d) }) {
		withdraw()
		return false
	}
	return true
}

// exitLandings are the doorsteps the mine's portal can set an account down on: within exitReach tiles of each gateway's NPC
// (the landing is a free square beside it; the live ones were 4 tiles from the Distentor's stand).
func exitLandings() []randomevents.Landing {
	var out []randomevents.Landing
	for _, gw := range essenceplan.Gateways() {
		out = append(out, randomevents.LandingNear(gw.Stand.X, gw.Stand.Z, exitReach))
	}
	return out
}

// exitReach is how far from a gateway's stand tile the portal's landing may be.
const exitReach = 7

// ---- the bank ----

// openNearestBankBooth finds every "Bank booth" candidate near (x,z,level) and tries them
// nearest-first — duplicated per this project's established per-package convention (see
// fisherrunner.openNearestBankBooth's own doc comment for the live bake-artifact bug this
// nearest-first retry recovers from).
func openNearestBankBooth(d *Deps, x, z, level int, timeout time.Duration) (*actions.Bank, bool) {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: essenceplan.BoothName, Op: essenceplan.BoothOp}, live)
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
		if open, _ := bank.AwaitOpen(mark, actions.DelayWaiter(d.Delay), timeout); open {
			return bank, true
		}
	}
	return nil, false
}

// OpenVarrockBank walks to essenceplan.BankStand and opens its booth, leaving it open for the
// caller to drive its own deposit/withdraw sequence — ported from openBank(). Requires
// d.Components; fails clearly otherwise rather than opening a bank it can't actually read.
func OpenVarrockBank(d *Deps) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	if !WalkNear(d, essenceplan.BankStand, 3) {
		return nil, false
	}
	x, z, level, _ := d.Session.State().Tile()
	bank, ok := openNearestBankBooth(d, x, z, level, 20*time.Second)
	if !ok {
		return nil, false
	}
	return bank, true
}

// DepositAllExcept deposits every pack item not named in keep (case-insensitive) into an
// already-open bank and returns how many distinct item ids were deposited. Every kind goes
// out in one burst, five Deposit-Alls a game tick, confirmed once, with the one-at-a-time path
// behind it (twopertick-1012: this was a Deposit-All and a wait for the bank to show it, per kind).
func DepositAllExcept(d *Deps, bank *actions.Bank, keep []string) int {
	kept := make(map[string]bool, len(keep))
	for _, k := range keep {
		kept[strings.ToLower(strings.TrimSpace(k))] = true
	}
	var ids []int32
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
		ids = append(ids, it.ID)
	}
	deposited, _, r, retried := bank.DepositKinds(ids, Waiter(d))
	if len(ids) > 0 {
		d.logf("%s", actions.VisitLog("essence deposit", len(ids), r, retried))
	}
	return len(deposited)
}

// WithdrawOneOf withdraws exactly one of name from an already-open bank into the pack,
// waiting for the pack to reflect it.
func WithdrawOneOf(d *Deps, bank *actions.Bank, name string) bool {
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !strings.EqualFold(obj.Name, name) {
			continue
		}
		before := packCountByID(bank.Inventory(), it.ID)
		if err := bank.WithdrawOne(it.ID); err != nil {
			d.logf("withdraw one %q: %v", name, err)
			return false
		}
		return waitFor(d, fmt.Sprintf("%s in pack after withdrawing", name), 3*time.Second, func() bool {
			return packCountByID(bank.Inventory(), it.ID) > before
		})
	}
	return false
}

// waiter adapts a Deps' Delayer to actions.Waiter, so the confirmation waits inside
// actions.Bank.SetNoteMode stop with the bot.
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

// Waiter is d as an actions.Waiter.
func Waiter(d *Deps) actions.Waiter { return waiter{d} }

// SetNoteMode sets the open bank's withdraw mode.
func SetNoteMode(d *Deps, bank *actions.Bank, on bool) error {
	return bank.SetNoteMode(on, Waiter(d))
}

// WithdrawAllByName withdraws every one of name with Withdraw-All and waits until the pack
// holds more of that name — counts by name (not id) so it confirms a withdrawal that lands as
// a note (a different id) in note mode, matching fisherrunner.WithdrawAllByName.
func WithdrawAllByName(d *Deps, bank *actions.Bank, name string) bool {
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !strings.EqualFold(obj.Name, name) {
			continue
		}
		before := PackCountByName(d, name)
		used := len(bank.Inventory())
		if err := bank.WithdrawAll(it.ID); err != nil {
			d.logf("withdraw all %q: %v", name, err)
			return false
		}
		return waitFor(d, fmt.Sprintf("%s in pack after withdrawing", name), 3*time.Second, func() bool {
			return PackCountByName(d, name) > before || len(bank.Inventory()) > used
		})
	}
	return false
}

// WithdrawXOf withdraws exactly n of name (as items, never notes) from an already-open bank
// and waits for the pack to reflect it — for an exact coin amount (the pickaxe's price), where
// WithdrawAllOf/WithdrawOneOf would take too much or too little.
func WithdrawXOf(d *Deps, bank *actions.Bank, name string, n int) bool {
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Noted() || !strings.EqualFold(obj.Name, name) {
			continue
		}
		before := PackCountByName(d, name)
		if err := bank.WithdrawX(it.ID, int32(n), Waiter(d)); err != nil {
			d.logf("withdraw %d %q: %v", n, name, err)
			return false
		}
		return waitFor(d, fmt.Sprintf("%s in pack after withdrawing", name), 4*time.Second, func() bool {
			return PackCountByName(d, name) > before
		})
	}
	return false
}

// ---- Nurmof, the pickaxe shop ----

// BuyFromNurmof hops down the Dwarven Mine trapdoor when still on the surface, walks to
// Nurmof's stand, and buys one of item from his stock — ported from buyFromNurmof(). Requires
// the account to already be carrying at least cost coins (the caller withdraws them from the
// bank first, exactly like upgradePickaxe() does before ever calling this).
func BuyFromNurmof(d *Deps, item string) bool {
	_, z, _, ok := d.Session.State().Tile()
	if ok && z < essenceplan.NurmofHopZThreshold {
		d.logf("heading into the Dwarven Mine for a %s", item)
		if !WalkNear(d, essenceplan.NurmofHopFrom, 2) {
			d.logf("could not reach the Dwarven Mine trapdoor")
			return false
		}
		if here := currentTile(d); here.Z < essenceplan.NurmofHopZThreshold {
			m, found := d.Locs.Nearest(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: here.Level, Radius: 10, Name: essenceplan.NurmofHopLoc, Op: essenceplan.NurmofHopOp}, locindex.ChangesFrom(d.Session.State().LocChanges()))
			if !found {
				d.logf("could not find the %s near the Dwarven Mine entrance", essenceplan.NurmofHopLoc)
				return false
			}
			if err := actions.OpLoc(d.Session, m.Op, m.X, m.Z, m.ID); err != nil {
				d.logf("OpLoc(%s) on the trapdoor: %v", essenceplan.NurmofHopOp, err)
				return false
			}
			if !waitFor(d, "entering the Dwarven Mine", 10*time.Second, func() bool {
				_, zz, _, ok := d.Session.State().Tile()
				return ok && zz >= essenceplan.NurmofHopZThreshold
			}) {
				d.logf("the trapdoor hop did not land underground — will retry")
				return false
			}
		}
	}
	if !WalkNear(d, essenceplan.NurmofStand, 4) {
		d.logf("could not reach Nurmof's shop")
		return false
	}
	nurmof, ok := findNpcByName(d, essenceplan.Nurmof, essenceplan.NurmofStand, 15, nil)
	if !ok {
		d.logf("could not find Nurmof near his stand")
		return false
	}
	npcType, _ := d.Config.Npc(nurmof.Type)
	tradeOp := cachecfg.OpIndex(npcType.Ops, "Trade")
	if tradeOp == 0 {
		tradeOp = 3 // matches every other shopkeeper fallback in this codebase for this content build
	}
	shop := actions.NewShop(d.Session)
	if err := actions.OpNpc(d.Session, tradeOp, nurmof.Index); err != nil {
		d.logf("OpNpc(Trade) on Nurmof: %v", err)
		return false
	}
	if !waitFor(d, "Nurmof's shop to open", 5*time.Second, shop.IsOpen) {
		return false
	}
	defer shop.Close()

	var itemID int32 = -1
	for _, it := range shop.Stock() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(obj.Name, item) {
			itemID = it.ID
			break
		}
	}
	if itemID == -1 {
		d.logf("%q not found in Nurmof's stock", item)
		return false
	}
	before := packCountByID(shop.Pack(), itemID)
	if err := shop.Buy(itemID, 1); err != nil {
		d.logf("Buy: %v", err)
		return false
	}
	return waitFor(d, fmt.Sprintf("%s in pack after buying", item), 5*time.Second, func() bool {
		return packCountByID(shop.Pack(), itemID) > before
	})
}
