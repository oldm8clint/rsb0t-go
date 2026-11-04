// Package combatrunner is the I/O half of the CombatTrainer primitives: it drives the pure
// decisions in internal/scripts/combatplan, and the prayer/autocast actions in
// internal/actions, against a live session.
//
// This is milestone 1 of docs/combat_trainer_plan.md — the primitives only. There is no
// training ladder, no gear-buying loop and no registered bot here yet, deliberately.
//
// The split is the one every sibling runner package follows (docs/script_porting_framework.md):
// the plan package decides, this one walks, clicks and waits, and every blocking wait goes
// through a caller-supplied Delayer so a running script can actually be cancelled mid-wait.
//
// The rule this package exists to enforce: internal/actions is fire-and-forget. menuAction
// and IF_BUTTON return nil when the bytes went out, never when the server acted. Every
// function here therefore sends and then VERIFIES against observed state — a varp, a stat, an
// inventory delta, a tile — before reporting success.
package combatrunner

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// Delayer sleeps for d, returning false early if the caller should stop instead of true once
// d has fully elapsed — the same shape every sibling runner package uses.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration — for a standalone tool with nothing to cancel.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything these primitives need from the outside world.
type Deps struct {
	Session    actions.Session
	Config     *cachecfg.Config
	Components archive.Components
	Pack       *pathfind.Pack
	Delay      Delayer
	// Log receives one already-formatted line per notable event; nil drops them.
	Log func(string)

	// Locs is the baked static loc index, needed to find a bank booth. Nil for the milestone
	// 1 primitives, which never banked; the trainer refuses to start without it.
	Locs *locindex.Index
	// Walker takes the long way when same-level pathfinding cannot reach a rung or a shop —
	// through doors, stairs and the dungeon entrances. Nil degrades TravelTo to same-level
	// pathfinding rather than failing.
	Walker *webwalk.Walker
	// Chat is the recent-chat ring buffer, so a purchase can quote the server's own refusal
	// instead of reporting "nothing happened".
	Chat *chatlog.Log

	// OnKill, when set, is called once for every target this package OBSERVES dying, with
	// the npc's type id and the tile it died on. Nil drops them, so every existing caller
	// and every cmd/ harness behaves exactly as before.
	//
	// WHY IT IS HERE rather than in one script. Until this existed, kills reached the
	// control panel's loot tracker from exactly ONE script -- internal/bot/advancedfighter.go
	// was the only file that owned a lootKills -- so every other script that fights recorded
	// nothing at all. Measured on the live fleet 2026-09-22: ScarabSwarm's Stage 3 trains
	// Magic by killing Cows north of the Ardougne market, and the panel had never shown a
	// single Cow, a single Cowhide or a single kill from it. The fleet owner's requirement is
	// that every NPC the fleet can fight and kill is tracked.
	//
	// WaitForKill is the one place that decides a target died (combatplan.JudgeKill weighs
	// it), so a hook here is inherited by every caller of FightOne at once instead of being
	// something twenty-two scripts each have to remember.
	//
	// The hook takes the TYPE ID, not a name: the type is what the caller can resolve a name,
	// a combat level and a drop table from through cachecfg, and passing a name would make
	// every caller repeat that lookup and disagree about it.
	OnKill func(npcType, x, z, level int)
	// OnKillSpent, when set, is called INSTEAD of OnKill with what the fight measurably used up:
	// the runes the pack lost between FightOne's attack and the kill (autocast spends them from
	// the pack; a staff's element is never deleted, so it is never counted). By obj id. Owner,
	// 4 Oct 2026: per-NPC loot should be net of the runes spent killing it.
	OnKillSpent func(npcType, x, z, level int, spent map[int32]int)
	// OnFightSpent, when set with OnKillSpent, is called for a fight that ended WITHOUT a kill after
	// it measurably used runes up (a break-off at the hitpoints floor, a target that left, a timeout):
	// the cast at that npc cost what it cost whether or not it died, and the Magic card counts every
	// offensive spell (a user request). npcType is the target's.
	OnFightSpent func(npcType int, spent map[int32]int)
	// ClickRange, when above zero, is how far (Chebyshev tiles) a target may be for FightOne to click it at once
	// instead of walking next to it first, when the route to it is a plain one (ClickAtOnce): the server walks the
	// account in and the attack lands the tick it arrives, with no webwalk plan and no wait for the walk to end
	// between the pick and the click ("combatspeed", 7 Oct 2026). Zero is the old walk-then-click.
	ClickRange int
	// Doors, for a run whose targets stand in rooms (the CombatTrainer), lets FightOne and WalkNear reach a target
	// behind a shut door: the doors the server has opened count as open (LivePack), and a route through the doors of
	// the Walker's graph is crossed (ReachFor, CrossDoors). Off, the pack's every-door-shut reading stands, which is
	// what the callers that must not go through a gate (the Ardougne pens) want. Needs Walker to cross a shut door.
	Doors bool
	// Watch, when set, runs at every wait of WalkNear: side work the account must do whenever it is on the move, which a
	// walk that polls for half a minute would otherwise starve (eating). It must return quickly and send no movement.
	Watch func()
	// StopOnAbandon makes a WalkNear that gives up after clicking send a click to the player's own tile, which the engine
	// takes as a stop: the path of the last click is still live on the server, and a walk nobody watches any more is a walk
	// into whatever stands on it. Off, WalkNear leaves the path as it always did.
	StopOnAbandon bool
	// runesAtAttack is the pack's runes when FightOne sent the attack (nil outside FightOne).
	runesAtAttack map[int32]int
}

// runeIDs are the runes a combat spell can delete (content pack/obj.pack 554 fire to 566 soul).
func isRune(id int32) bool { return id >= 554 && id <= 566 }

// PackRunes is the pack's runes by id, for a caller that measures a fight of its own (scarabrunner's
// fightToFloor and hand-cast paths) the way FightOne does.
func PackRunes(d *Deps) map[int32]int { return packRunes(d) }

// RunesSpent is what the pack lost of each rune since before.
func RunesSpent(before, after map[int32]int) map[int32]int { return runesSpent(before, after) }

// packRunes is the pack's runes by id.
func packRunes(d *Deps) map[int32]int {
	out := map[int32]int{}
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return out
	}
	for _, it := range inv.NonEmpty() {
		if isRune(it.ID) {
			out[it.ID] += int(it.Count)
		}
	}
	return out
}

// runesSpent is what the pack lost of each rune since before.
func runesSpent(before, after map[int32]int) map[int32]int {
	out := map[int32]int{}
	for id, n := range before {
		if d := n - after[id]; d > 0 {
			out[id] = d
		}
	}
	return out
}

func (d *Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

// PlayerPack is the persistent inventory-tab component id, as every sibling runner uses.
const PlayerPack = 3214

// PackSize is how many slots the backpack has, so a failure can say whether there was anywhere
// to put what it was reaching for.
const PackSize = 28

func currentTile(d *Deps) (tile.Tile, bool) {
	x, z, level, ok := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}, ok
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

// ---- prayer ----

// SetPrayer switches a prayer on or off and confirms it actually happened, by watching the
// prayer's own varp rather than trusting the click.
//
// actions.SetPrayer already refuses to send when the state is right or the account cannot
// use the prayer, so a nil error with the varp unchanged means the server ignored the click —
// most often because the account's tabs were never assigned (the Tutorial Island trap in
// cmd/equiptest's header) and Player.isComponentVisible rejected it.
func SetPrayer(d *Deps, p actions.Prayer, on bool, timeout time.Duration) error {
	if actions.PrayerActive(d.Session, p) == on {
		return nil
	}
	if err := actions.SetPrayer(d.Session, p, on); err != nil {
		return err
	}
	if !waitFor(d, fmt.Sprintf("%s to turn %s", p.Name, onOff(on)), timeout, func() bool {
		return actions.PrayerActive(d.Session, p) == on
	}) {
		return fmt.Errorf("combatrunner: clicked %s but its varp %d never changed", p.Name, p.Varp)
	}
	d.logf("prayer %s is now %s", p.Name, onOff(on))
	return nil
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// PrayerPoints returns current prayer points, and PrayerLevel the base level — see
// actions.PrayerPoints for why they are different numbers.
func PrayerPoints(d *Deps) (int, bool) { return actions.PrayerPoints(d.Session) }

// PrayerLevel returns the account's base Prayer level.
func PrayerLevel(d *Deps) (int, bool) { return actions.PrayerLevel(d.Session) }

// ---- autocast ----

// autocastCombatTabRoot is combat_staff_2, the interface root every autocast button hangs under
// (pack/interface.pack: combat_staff_2:auto_choose 353, :auto_toggle 349). It is duplicated here
// rather than imported from scarabplan because combatrunner must not depend on a script package.
const autocastCombatTabRoot = 328

// SetAutocast performs the three-click autocast sequence and confirms each step against the
// varps the server publishes: open the spell list, choose the spell, then toggle it on.
//
// Each step is verified because each can be refused silently. Choosing a spell runs
// ~check_spell_requirements server-side, which fails without the Magic level, the runes, or
// members for a Wave, and leaves both varps untouched — so a caller that only clicked would
// happily believe it was autocasting Fire Blast while meleeing.
// Note it cannot skip the work when the right spell is already set: varp 59 never reaches
// this client on content 289 (see actions.AutocastSpell), so "which spell is autocasting" is
// unknowable and the sequence is always run in full.
func SetAutocast(d *Deps, spell actions.Spell, timeout time.Duration) error {
	// PRECONDITION, added 2026-09-21 because its absence cost three sessions. Every button this
	// function clicks lives under interface root 328 (combat_staff_2), and the engine's
	// IfButtonHandler drops a click SILENTLY when isComponentVisible says that root is not in the
	// account's tabs (Player.ts:2126). Measured live: with a Staff of air equipped and confirmed
	// worn, the combat tab read 5855 (combat_unarmed) and every click below went nowhere — no
	// varp, no message, nothing. The failure then surfaced as this function's own "refused for
	// level, runes or members", which is none of those three and sent three separate sessions
	// hunting levels, runes and members.
	//
	// No equip moves that tab (tested with a Bronze sword and a Staff of air). Content refreshes
	// it unconditionally at login (login_logout/login.rs2:93 calls ~update_weapon_category) and
	// the equip path does not, so the fix is to WIELD AND THEN RELOG. Checking it here costs one
	// map read and turns a silent drop into a sentence that names the remedy.
	if root := d.Session.State().SideIcon(actions.CombatTab); root != autocastCombatTabRoot {
		return fmt.Errorf("combatrunner: cannot set autocast %s: the combat tab is interface %d, not "+
			"combat_staff_2 (%d), so the engine will silently discard every click — wield the staff and "+
			"relog, because content only refreshes this tab at login", spell.Name, root, autocastCombatTabRoot)
	}
	if err := actions.OpenAutocastMenu(d.Session); err != nil {
		return err
	}
	// open_autocast_interface sets attackstyle_magic to 0 and swaps the tab; there is no
	// varp that says "the list is showing", so settle briefly rather than assert.
	if !d.Delay(600 * time.Millisecond) {
		return fmt.Errorf("combatrunner: cancelled while opening the autocast list")
	}

	if err := actions.ChooseAutocastSpell(d.Session, spell); err != nil {
		return err
	}
	// Acceptance shows as attackstyle_magic leaving 0: set_autocast_spell writes 2 only
	// after ~check_spell_requirements has passed, so a refusal (level, runes, members)
	// leaves it at the 0 that OpenAutocastMenu just set.
	if !waitFor(d, "the spell to be accepted", timeout, func() bool {
		return actions.AutocastSpellChosen(d.Session)
	}) {
		return fmt.Errorf("combatrunner: %s was not accepted — the server refused it for level, runes or members", spell.Name)
	}

	if actions.AutocastEnabled(d.Session) {
		return nil
	}
	if err := actions.ToggleAutocast(d.Session); err != nil {
		return err
	}
	if !waitFor(d, "autocast to switch on", timeout, func() bool {
		return actions.AutocastEnabled(d.Session)
	}) {
		return fmt.Errorf("combatrunner: %s is chosen but autocast never switched on", spell.Name)
	}
	d.logf("autocasting %s", spell.Name)
	return nil
}

// ClickAtOnce reports whether the attack on target may be clicked from where the account stands (Deps.ClickRange):
// it is within the range and the route to it is plain, at most three corner points (start, one turn, end: a
// diagonal then a straight run, which is the walk the server's own approach makes), so the server's approach reaches
// it without the pathfinder's help. A target behind a wall, a door or a long detour is walked to first as before.
// False with ClickRange 0 or no collision pack.
func ClickAtOnce(d *Deps, target tile.Tile) bool {
	if d.ClickRange <= 0 || d.Pack == nil {
		return false
	}
	here, ok := currentTile(d)
	if !ok || here.Level != target.Level || tile.Chebyshev(here, target) > d.ClickRange {
		return false
	}
	res := d.Pack.ReachableWithin(
		pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level},
		pathfind.NavPoint{X: target.X, Z: target.Z, Level: target.Level},
		ApproachRadius)
	return res.OK && len(res.Waypoints) <= 3
}

// ---- kills ----

// ObserveTarget takes one reading of an NPC by index, for combatplan.JudgeKill.
func ObserveTarget(d *Deps, index int) combatplan.TargetState {
	for _, n := range d.Session.State().NPCs() {
		if n.Index != index {
			continue
		}
		return combatplan.TargetState{
			Index:       n.Index,
			Present:     true,
			Health:      n.Health,
			TotalHealth: n.TotalHealth,
			FightingUs:  n.TargetsPlayer(d.Session.State().Self()),
		}
	}
	return combatplan.TargetState{Index: index, Present: false}
}

// AttackNPC clicks Attack on an NPC, resolving the option from the NPC's own op list.
func AttackNPC(d *Deps, n gamestate.NPC) error {
	t, ok := d.Config.Npc(n.Type)
	if !ok {
		return fmt.Errorf("combatrunner: no config for npc type %d", n.Type)
	}
	op := cachecfg.OpIndex(t.Ops, "Attack")
	if op == 0 {
		return fmt.Errorf("combatrunner: %s has no Attack option", t.Name)
	}
	return actions.OpNpc(d.Session, op, n.Index)
}

// WaitForKill watches one target until it dies or the timeout expires, and reports what was
// actually observed rather than assuming a disappearance was a kill.
//
// It keeps the last reading in which the NPC was present, because once an NPC leaves the list
// the only evidence left is what it looked like beforehand — that is precisely what
// combatplan.JudgeKill weighs. The returned KillSite is where the target was last seen, for
// attributing drops.
func WaitForKill(d *Deps, index int, timeout time.Duration) (combatplan.Verdict, combatplan.KillSite) {
	w := watchKill(d, index, timeout, killWatch{})
	return w.verdict, w.site
}

// Refusal is the server's answer to an attack it would not start (content
// skill_combat/scripts/player/player_combat.rs2, player_in_combat_check, single-way areas only).
type Refusal string

const (
	// RefusedContested is "Someone else is fighting that.": another player fought the target in
	// the last 8 ticks. Leave it alone for a while.
	RefusedContested Refusal = "contested"
	// RefusedUnderAttack is "I'm already under attack!": a monster other than the target fought
	// this account in the last 8 ticks. The target is not at fault; the fight on the account is.
	RefusedUnderAttack Refusal = "under attack"
)

// refusalIn finds a refusal in chat lines said since an attack went out.
func refusalIn(chat string) Refusal {
	switch {
	case strings.Contains(chat, "Someone else is fighting that"):
		return RefusedContested
	case strings.Contains(chat, "I'm already under attack"):
		return RefusedUnderAttack
	}
	return ""
}

// killIsOurs is whether a target that died was this account's kill: any damage the account dealt
// gave it Hitpoints experience (content gives Hitpoints XP for every melee, ranged and magic hit
// that lands), so a death with none gained since the attack went out is somebody else's. A reading
// that could not be taken counts as ours, as before.
func killIsOurs(known bool, before, after int) bool {
	return !known || after > before
}

// killWatch is what a FightOne knows from the moment its attack went out.
type killWatch struct {
	// attackAt is when the attack went out; zero (WaitForKill's callers) reads no chat.
	attackAt time.Time
	// hpXP is the Hitpoints experience then; hpKnown says it was read.
	hpXP    int
	hpKnown bool
}

// killResult is how a watched fight ended.
type killResult struct {
	verdict combatplan.Verdict
	site    combatplan.KillSite
	refused Refusal
	// notOurs is a death the account dealt no damage to (killIsOurs): not reported to OnKill.
	notOurs bool
}

func watchKill(d *Deps, index int, timeout time.Duration, w killWatch) killResult {
	deadline := time.Now().Add(timeout)
	var lastPresent combatplan.TargetState
	var site combatplan.KillSite
	// npcType is remembered from the last reading in which the target was PRESENT, for the
	// same reason lastPresent is: once it leaves the list there is nothing left to read it
	// from, and it is what OnKill needs to name the kill.
	npcType := -1
	for time.Now().Before(deadline) {
		// A refused attack ends the wait now. Before this the fight sat out its whole timeout on a
		// target the server never gave it, and then credited itself with the other player's kill
		// (melee idle analysis, 6 Oct 2026: 4-7 counted kills per real one at the single-way
		// Giants).
		if !w.attackAt.IsZero() && d.Chat != nil {
			if r := refusalIn(d.Chat.Since(w.attackAt)); r != "" {
				return killResult{verdict: combatplan.Unknown, site: site, refused: r}
			}
		}
		dismissLevelUp(d)
		now := ObserveTarget(d, index)
		if now.Present {
			lastPresent = now
			for _, n := range d.Session.State().NPCs() {
				if n.Index == index {
					site = combatplan.KillSite{X: n.X, Z: n.Z, Level: n.Level, AtMs: time.Now().UnixNano() / int64(time.Millisecond)}
					npcType = n.Type
					break
				}
			}
		}
		verdict := combatplan.JudgeKill(lastPresent, now)
		switch verdict {
		case combatplan.Killed, combatplan.LikelyKilled, combatplan.Vanished:
			site.AtMs = time.Now().UnixNano() / int64(time.Millisecond)
			if w.hpKnown {
				if sk, ok := d.Session.State().Skill(HitpointsStat); ok && !killIsOurs(true, w.hpXP, sk.XP) {
					return killResult{verdict: verdict, site: site, notOurs: true}
				}
			}
			// Report the kill before returning, so a caller that stops looking at the
			// verdict still has it counted. Vanished is included deliberately: JudgeKill
			// already separates "died in view" from "walked out of view", and its own
			// Vanished case is the one where the target was being fought and then was not
			// there -- treating that as no kill undercounts every fight that ends on the
			// edge of the view. A caller that wants only confirmed kills can filter on the
			// verdict it gets back.
			switch {
			case d.OnKillSpent != nil && npcType >= 0:
				var spent map[int32]int
				if d.runesAtAttack != nil {
					spent = runesSpent(d.runesAtAttack, packRunes(d))
				}
				d.OnKillSpent(npcType, site.X, site.Z, site.Level, spent)
			case d.OnKill != nil && npcType >= 0:
				d.OnKill(npcType, site.X, site.Z, site.Level)
			}
			return killResult{verdict: verdict, site: site}
		}
		if !d.Delay(200 * time.Millisecond) {
			return killResult{verdict: combatplan.Unknown, site: site}
		}
	}
	return killResult{verdict: combatplan.Unknown, site: site}
}

// dismissLevelUp presses "Click here to continue" on a chat dialog left open mid-fight while no
// random event is on (the Guardian owns those): in practice a level-up. While a modal is open the
// engine runs no interaction (Player.processInteraction), so the account stopped swinging until
// the trainer's next pass closed it.
func dismissLevelUp(d *Deps) {
	if d.Components == nil {
		return
	}
	// The dialog is read first: the Guardian's Busy is the dearer question and is asked only when
	// there is something to press.
	cd := actions.NewChatDialog(d.Session, d.Components)
	if cd.IsOpen() && cd.CanContinue() && !randomevents.Busy(d.Session) {
		_ = cd.Continue()
	}
}

// ---- loot ----

// GroundItemsNamed returns every visible ground item whose config name matches name.
func GroundItemsNamed(d *Deps, name string) []gamestate.GroundItem {
	var out []gamestate.GroundItem
	for _, g := range d.Session.State().GroundItems() {
		if obj, ok := d.Config.Obj(g.ID); ok && strings.EqualFold(obj.Name, name) {
			out = append(out, g)
		}
	}
	return out
}

// ErrGroundItemGone reports a stack that left the ground without reaching our pack: another
// account or an NPC took it first, or it despawned. The item is lost and nothing is broken,
// which is a different thing from the take having failed, and callers that retry should not.
var ErrGroundItemGone = errors.New("combatrunner: the stack left the ground without reaching the pack")

// takeSettle is how long a stack must stay missing from the ground before its absence is read as
// somebody else's pickup. Taking an item sends an OBJ_DEL for the tile and an inventory update,
// and nothing orders the two, so a stack that is missing for one poll may simply be ours in
// flight. See combatplan.JudgeTake for the whole rule.
const takeSettle = 1200 * time.Millisecond

// ApproachAndTake walks to a ground item when it is out of reach, then takes it, and confirms
// the pack actually gained it.
//
// The approach walk is the missing half of the pickup primitive: internal/actions.OpObj sends
// a take for an absolute tile and does not walk, so a caller standing any distance away sends
// a click the server simply drops. takeRadius is how close to get before clicking (1, i.e.
// adjacent, is the safe default) and leash caps how far the script will travel at all.
//
// Everything after the walk exists because this used to report every failure as one sentence,
// "took item N but the pack never gained it", which accuses the click whatever really happened.
// A take can fail in ways that want different answers: the stack was already gone before we
// clicked, somebody else took it while we walked, or the click genuinely did nothing. Only the
// last is this primitive's own bug, and only it should read as one in a log.
//
// Honest status: the live failure that prompted this (one big bones lost during a Giants run)
// was NOT reproduced. 19 live takes passed — 8 plain, 8 with a dialogue on screen, 3 unguarded —
// and both theories for it were disproven by direct measurement (cmd/primitivebugtest -mode
// take): an open dialogue does not block an obj click, and this item's slots arrive with a count
// of 1, so the original quantity-summing confirmation would have missed none of them. What is
// fixed is the reporting, which is what makes the next occurrence diagnosable instead of
// mysterious. The likeliest remaining causes, both surfaced by the new errors but neither proven:
// a drop owned by another player (Obj.receiver64, which makes World.getObj answer null and the
// engine reply UnsetMapFlag), and a pack that filled between the caller's space check and the
// click.
func ApproachAndTake(d *Deps, item gamestate.GroundItem, takeRadius, leash int, timeout time.Duration) error {
	here, ok := currentTile(d)
	if !ok {
		return fmt.Errorf("combatrunner: no position for this account yet")
	}
	target := tile.Tile{X: item.X, Z: item.Z, Level: item.Level}
	if here.Level != target.Level {
		return fmt.Errorf("combatrunner: the item is on level %d and we are on %d", target.Level, here.Level)
	}

	switch combatplan.DecideApproach(tile.Chebyshev(here, target), takeRadius, leash) {
	case combatplan.TooFar:
		return fmt.Errorf("combatrunner: %d tiles away is beyond the %d-tile leash", tile.Chebyshev(here, target), leash)
	case combatplan.WalkCloser:
		if !WalkNear(d, target, takeRadius, timeout) {
			return fmt.Errorf("combatrunner: could not walk to the item at %d,%d", target.X, target.Z)
		}
	}

	// Close the last step onto the stack's own tile, or a tile that shares an edge with it.
	//
	// The engine accepts an obj click from either (Player.inOperableDistance answers
	// reachedEntity || reachedObj for an Obj). What it does not accept is a DIAGONAL
	// neighbour, and a Chebyshev radius of 1 is exactly what lets one through. This project
	// has now measured the same rule twice elsewhere: a Net click on a fishing spot from a
	// diagonal tile does nothing at all, and shopReachTiles had to come down to real adjacency
	// for the same reason. A take sent from a diagonal tile is ACCEPTED by OpObjHandler, which
	// clears the pending action and queues the interaction, and then tryInteract never
	// satisfies inOperableDistance — so there is no refusal, no chat line and no UnsetMapFlag,
	// and the stack simply stays on the ground until the caller's timeout. That is precisely
	// the signature the Giants runs logged: "still lying there after 20s (the pack never
	// changed; the server refused 0 clicks in that time)".
	if at, ok := currentTile(d); ok && !touchesTile(at, target) {
		if !WalkNear(d, target, 0, timeout) {
			return fmt.Errorf("combatrunner: could not stand on %s at %d,%d to take it", describeItem(d, item.ID), target.X, target.Z)
		}
	}

	// The walk takes seconds, and a contested drop does not wait for us.
	if !onGround(d, item) {
		return fmt.Errorf("%w: %s was gone from %d,%d before the take was even sent", ErrGroundItemGone, describeItem(d, item.ID), item.X, item.Z)
	}
	// An open main or chat modal makes the engine take this click and never run it, silently.
	if err := actions.ClearInteractionBlock(d.Session, Waiter(d)); err != nil {
		return fmt.Errorf("combatrunner: could not clear the interface in front of %s: %w", describeItem(d, item.ID), err)
	}

	beforeCount, beforeSlots := countInPack(d, item.ID), usedPackSlots(d)
	rejectedBefore := actions.InteractionRejections(d.Session)
	clickedAt := time.Now()
	if err := actions.TakeGroundItem(d.Session, d.Config, item); err != nil {
		return err
	}

	start := time.Now()
	var missingSince time.Time
	for {
		present := onGround(d, item)
		if present {
			missingSince = time.Time{}
		} else if missingSince.IsZero() {
			missingSince = time.Now()
		}
		obs := combatplan.TakeObservation{
			// Either reading counts as arrival. An unstackable slot reports its quantity as 0
			// or 1 depending on which update path delivered it, so a quantity comparison alone
			// can miss a pickup that plainly happened.
			PackGained: countInPack(d, item.ID) > beforeCount || usedPackSlots(d) > beforeSlots,
			OnGround:   present,
			SinceMs:    time.Since(start).Milliseconds(),
		}
		if !missingSince.IsZero() {
			obs.MissingForMs = time.Since(missingSince).Milliseconds()
		}
		switch combatplan.JudgeTake(obs, takeSettle.Milliseconds(), timeout.Milliseconds()) {
		case combatplan.TakeDone:
			return nil
		case combatplan.TakeGone:
			return fmt.Errorf("%w: %s left %d,%d %dms after the take without reaching the pack", ErrGroundItemGone, describeItem(d, item.ID), item.X, item.Z, obs.MissingForMs)
		case combatplan.TakeTimedOut:
			// Everything a reader needs to tell the three remaining causes apart, because the
			// old message named none of them: where we were standing relative to the stack
			// (diagonal is the one the engine silently drops), whether the pack had anywhere
			// to put it, and whatever the server actually said.
			at, _ := currentTile(d)
			said := ""
			if d.Chat != nil {
				said = strings.TrimSpace(d.Chat.Since(clickedAt))
			}
			if said == "" {
				said = "nothing"
			}
			return fmt.Errorf("combatrunner: took %s at %d,%d and it is still lying there after %s — standing at %d,%d (%s), %d free pack slots, the server refused %d clicks and said %q",
				describeItem(d, item.ID), item.X, item.Z, timeout, at.X, at.Z, describeOffset(at, target),
				PackSize-usedPackSlots(d), actions.InteractionRejections(d.Session)-rejectedBefore, said)
		}
		if !d.Delay(100 * time.Millisecond) {
			return fmt.Errorf("combatrunner: stopped while waiting for %s to reach the pack", describeItem(d, item.ID))
		}
	}
}

// touchesTile reports whether a take sent from at will satisfy the engine's own obj reach:
// the stack's own tile, or one that shares an EDGE with it. A diagonal neighbour does not
// count, which is the whole point — see ApproachAndTake's note on the silent drop.
func touchesTile(at, target tile.Tile) bool {
	if at.Level != target.Level {
		return false
	}
	dx, dz := at.X-target.X, at.Z-target.Z
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	return dx+dz <= 1
}

// describeOffset names where we were standing relative to a stack, in the terms that decide
// whether the engine would have honoured the click at all.
func describeOffset(at, target tile.Tile) string {
	if at.Level != target.Level {
		return fmt.Sprintf("on level %d, the stack on %d", at.Level, target.Level)
	}
	dx, dz := at.X-target.X, at.Z-target.Z
	adx, adz := dx, dz
	if adx < 0 {
		adx = -adx
	}
	if adz < 0 {
		adz = -adz
	}
	switch {
	case adx == 0 && adz == 0:
		return "on the stack's own tile"
	case adx+adz == 1:
		return fmt.Sprintf("orthogonally adjacent, %d,%d off", dx, dz)
	case adx <= 1 && adz <= 1:
		return fmt.Sprintf("DIAGONAL, %d,%d off — the engine drops an obj click from here", dx, dz)
	default:
		// Saying DIAGONAL here would be a lie that reads like a diagnosis. A take that was
		// sent from hundreds of tiles away means something moved the account between the
		// approach and the confirmation — a death, or a random event's fail-teleport — and
		// that is a different bug from the one the touching rule fixes. This case was hit
		// live: the account was at 2434,3346 while the bones lay at 3113,9833.
		return fmt.Sprintf("%d,%d off, %d tiles away — too far to have been a click problem; something moved the account after the approach", dx, dz, max(adx, adz))
	}
}

// onGround reports whether the stack we clicked is still where we clicked it. It is the second
// reading ApproachAndTake judges by, and the only way to tell a contested drop from a dead click.
func onGround(d *Deps, item gamestate.GroundItem) bool {
	for _, g := range d.Session.State().GroundItems() {
		if g.ID == item.ID && g.X == item.X && g.Z == item.Z && g.Level == item.Level {
			return true
		}
	}
	return false
}

// describeItem names an object for a log line, falling back to its id.
func describeItem(d *Deps, objID int) string {
	if d.Config != nil {
		if obj, ok := d.Config.Obj(objID); ok && strings.TrimSpace(obj.Name) != "" {
			return fmt.Sprintf("%s (item %d)", obj.Name, objID)
		}
	}
	return fmt.Sprintf("item %d", objID)
}

func usedPackSlots(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	return len(inv.NonEmpty())
}

func countInPack(d *Deps, objID int) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		if int(it.ID) == objID {
			// Defensive, not a fix: measured live, this item's slots always arrive with a count
			// of 1 (cmd/primitivebugtest reports the real values), so the old plain sum was not
			// what lost a pickup. An unstackable slot reporting 0 is still possible on some
			// update paths, and treating that as one item costs nothing.
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// WalkNear paths to dest and walks there, re-clicking from wherever the player stops, until
// within radius tiles. Duplicated per this project's established per-package convention (see
// docs/script_porting_framework.md's "why duplicated" section) rather than shared.
//
// It sends no click while the session's view is lagging (a click is planned from, and walks first to, the tile the view
// shows, which a lagging view has the player already past), runs Deps.Watch at every wait, and, with Deps.StopOnAbandon,
// stops the player where it stands when it gives up (see Deps.StopOnAbandon).
func WalkNear(d *Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	ok, clicked := walkNear(d, dest, radius, timeout)
	if !ok && clicked {
		d.stopWalking()
	}
	return ok
}

// viewLagging reports whether the session's view of the game is silent or catching up (gamestate.State.ViewLag).
func (d *Deps) viewLagging() bool {
	st := d.Session.State()
	return st != nil && st.ViewLagging()
}

// watch runs Deps.Watch, if any.
func (d *Deps) watch() {
	if d.Watch != nil {
		d.Watch()
	}
}

// stopWalking clicks the player's own tile, which the engine takes as a stop, when StopOnAbandon is set and the view is
// current: a tile that may be behind the player would walk it back to it.
func (d *Deps) stopWalking() {
	if !d.StopOnAbandon || d.viewLagging() {
		return
	}
	if here, ok := currentTile(d); ok {
		_ = actions.Walk(d.Session, []pathfind.NavPoint{{X: here.X, Z: here.Z, Level: here.Level}}, false)
	}
}

func walkNear(d *Deps, dest tile.Tile, radius int, timeout time.Duration) (arrived, clicked bool) {
	deadline := time.Now().Add(timeout)
	crossings := 0
	for time.Now().Before(deadline) {
		d.watch()
		if d.viewLagging() {
			if !d.Delay(300 * time.Millisecond) {
				return false, clicked
			}
			continue
		}
		here, ok := currentTile(d)
		if ok && tile.Chebyshev(here, dest) <= radius {
			return true, clicked
		}
		if !ok {
			if !d.Delay(200 * time.Millisecond) {
				return false, clicked
			}
			continue
		}
		res := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level})
		if !res.OK && d.Doors && here.Level == dest.Level {
			// The baked pack has every door shut. A door the server has opened is no wall (livePathNear), and a shut one
			// is the Walker's to open (doorReach, CrossDoors), a couple of times at most, then the path is asked again.
			if live, ok := livePathNear(d, here, dest); ok {
				res = live
			} else if crossings < walkNearCrossings {
				if r := doorReach(d, dest, res.Reason); r.OK && CrossDoors(d, r) {
					crossings++
					continue
				}
			}
		}
		if !res.OK {
			d.logf("no path from %d,%d,%d to %d,%d,%d: %s", here.X, here.Z, here.Level, dest.X, dest.Z, dest.Level, res.Reason)
			return false, clicked
		}
		if len(res.Waypoints) < 2 {
			return true, clicked
		}
		if err := actions.Walk(d.Session, res.Waypoints, false); err != nil {
			d.logf("walk: %v", err)
			return false, clicked
		}
		clicked = true
		lastX, lastZ, still := here.X, here.Z, time.Now()
		for time.Now().Before(deadline) {
			d.watch()
			if !d.Delay(200 * time.Millisecond) {
				return false, clicked
			}
			if d.viewLagging() {
				still = time.Now() // a frozen view is not a stalled walk, and its next click would start behind the player
				continue
			}
			cur, ok := currentTile(d)
			if !ok {
				continue
			}
			if tile.Chebyshev(cur, dest) <= radius {
				return true, clicked
			}
			if cur.X != lastX || cur.Z != lastZ {
				lastX, lastZ, still = cur.X, cur.Z, time.Now()
			} else if time.Since(still) > 2*time.Second {
				break
			}
		}
	}
	return false, clicked
}

// ---- death and re-gear ----

// HeldNames returns the display names of everything the account is wearing and carrying — the
// before/after reading combatplan.LostItems compares.
func HeldNames(d *Deps) []string {
	eq := actions.NewEquipment(d.Session, d.Components)
	var out []string
	for _, it := range eq.Worn() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Name != "" {
			out = append(out, obj.Name)
		}
	}
	for _, it := range eq.Inventory() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Name != "" {
			out = append(out, obj.Name)
		}
	}
	return out
}

// WornNames returns just what is equipped.
func WornNames(d *Deps) []string {
	eq := actions.NewEquipment(d.Session, d.Components)
	var out []string
	for _, it := range eq.Worn() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Name != "" {
			out = append(out, obj.Name)
		}
	}
	return out
}

// PackNames returns just what is carried.
func PackNames(d *Deps) []string {
	eq := actions.NewEquipment(d.Session, d.Components)
	var out []string
	for _, it := range eq.Inventory() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Name != "" {
			out = append(out, obj.Name)
		}
	}
	return out
}

// WaitForRespawn waits until the account is standing at the Lumbridge respawn after a death,
// then clears the death latch so the next death is detectable.
//
// Why the tile and not just the latch: gamestate.State.IsDead is set by the death chat line,
// which arrives before the teleport lands. A script that re-geared on the latch alone would
// act while still at the corpse.
func WaitForRespawn(d *Deps, slack int, timeout time.Duration) bool {
	ok := waitFor(d, "the Lumbridge respawn", timeout, func() bool {
		here, have := currentTile(d)
		return have && combatplan.AtRespawn(here.X, here.Z, here.Level, slack)
	})
	if ok {
		d.Session.State().ClearDead()
	}
	return ok
}

// ReGear puts back on whatever of wanted the account can still reach, and reports what is
// genuinely gone. It equips only from the pack: refilling from a bank or a mule is milestone
// 2's job, not this primitive's.
func ReGear(d *Deps, wanted []string, timeout time.Duration) (equipped []string, missing []string, err error) {
	steps, missing := combatplan.ReGearPlan(wanted, WornNames(d), PackNames(d))
	eq := actions.NewEquipment(d.Session, d.Components)
	// EVERY PIECE GOES ON IN ONE BURST (twopertick-1012): a wear click has no delay (content player/scripts/equip.rs2
	// [opheld2,_] ~equip), so the whole re-gear is five clicks to a game tick and one confirm on the worn panel, where
	// each piece used to be a click and a wait for it to show. What the burst did not land is worn again one at a time.
	var ids []int32
	itemOf := map[int32]string{}
	for _, step := range steps {
		id, ok := packItemID(d, eq, step.Item)
		if !ok {
			missing = append(missing, step.Item)
			continue
		}
		ids = append(ids, id)
		itemOf[id] = step.Item
	}
	r := eq.EquipMany(d.Config, ids, Waiter(d))
	if len(ids) > 0 {
		d.logf("%s", r.Log("re-equipped"))
	}
	if r.Err != nil {
		return equipped, missing, fmt.Errorf("combatrunner: equipping: %w", r.Err)
	}
	for _, id := range ids {
		if eq.IsWorn(id) {
			equipped = append(equipped, itemOf[id])
			d.logf("re-equipped %s", itemOf[id])
		}
	}
	if len(r.Unusable) > 0 {
		return equipped, missing, fmt.Errorf("combatrunner: equipping %s: %w", itemOf[r.Unusable[0]], actions.ErrNoSuchAction)
	}
	if len(r.Left) > 0 {
		return equipped, missing, fmt.Errorf("combatrunner: clicked wear on %s but it never appeared in the worn panel", itemOf[r.Left[0]])
	}
	return equipped, missing, nil
}

// packItemID is the pack slot id of an unnoted item named name.
//
// A bank note shares its item's name and has no Wear option: live 12 Oct the Stage 5 trainer clicked "wear" on the
// noted alch stock it carries (Adamant longsword, Adamant plateskirt ...) about 35 times an hour ("actions: no option
// with that label"). Notes are skipped.
func packItemID(d *Deps, eq *actions.Equipment, name string) (int32, bool) {
	for _, it := range eq.Inventory() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && !obj.Noted() && strings.EqualFold(obj.Name, name) {
			return it.ID, true
		}
	}
	return 0, false
}

// ---- finishing for good ----

// LogoutForGood ends a run permanently: it tells the fleet the script is finished (so the
// reconnect loop leaves the account alone), clicks the logout button, and confirms the server
// actually logged the account out.
//
// declareFinished is the caller's bot.Context.RequestFinish — passed as a function because
// this package cannot import internal/bot (that would be a cycle through the runner). Pass
// nil only for a standalone tool with no fleet behind it.
//
// The server refuses a logout during combat (content/scripts/login_logout/logout.rs2) by
// simply not replying, which is why this retries until the timeout rather than clicking once.
func LogoutForGood(d *Deps, declareFinished func(), timeout time.Duration) error {
	if declareFinished != nil {
		declareFinished()
	}
	deadline := time.Now().Add(timeout)
	for attempt := 1; time.Now().Before(deadline); attempt++ {
		if d.Session.State().IsLoggedOut() {
			d.logf("logged out for good")
			return nil
		}
		if err := actions.Logout(d.Session); err != nil {
			return err
		}
		d.logf("logout click %d sent", attempt)
		// The server replies within a tick when it accepts; in combat it never replies,
		// so wait a few ticks before trying again rather than spamming the button.
		for i := 0; i < 15 && time.Now().Before(deadline); i++ {
			if d.Session.State().IsLoggedOut() {
				d.logf("logged out for good")
				return nil
			}
			if !d.Delay(200 * time.Millisecond) {
				return fmt.Errorf("combatrunner: cancelled while logging out")
			}
		}
	}
	return fmt.Errorf("combatrunner: sent the logout click but the server never logged this account out (in combat?)")
}
