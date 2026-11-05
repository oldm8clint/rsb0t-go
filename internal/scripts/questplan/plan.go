// Package questplan is the pure half of the AIOQuester quest engine, ported from the
// TypeScript client's src/bot/api/ai/quests/ (engine/types.ts, engine/queue.ts,
// engine/watchdog.ts, EligibilityEvaluator.ts, RequirementChecker.ts, ItemChecker.ts and the
// option-picking half of exec/primitives.ts). It is pure: no client, actions, fleet or bot
// imports and no I/O, per docs/script_porting_framework.md.
//
// # What a quest is, in this engine
//
// A quest module is a *pure decision function* over a snapshot: decide(Snapshot) Step. That is
// the whole design. Because the decision depends only on observable state and never on the
// module's own memory, a quest is restartable from any point — kill the bot mid-quest, start
// it again, and it re-derives where it is. Every rule below exists to keep that property.
//
// # Why state is read the way it is
//
// Classic-RS quest progress is NOT on the wire. Checked directly against this content build:
// content/scripts/quests/quest_runemysteries/configs/quest_runemysteries.varp is
// `[runemysteries] scope=perm` with no `transmit=yes`, and quest_barcrawl.varp is the same, so
// internal/gamestate never observes either. A non-transmitted varp reads 0, which is
// indistinguishable from "never started", so branching on one is reading silence. The same
// conclusion is reached independently in internal/quests' own package doc and in the vendored
// docs/decisions/quest-state-not-varps.md.
//
// What is actually readable, in the order a module should prefer it:
//
//  1. Held / worn / banked items (by object ID where display names collide).
//  2. Game messages after an action.
//  3. Scene behaviour (a loc that refuses proves the state that makes it refuse).
//  4. The quest tab's colour — three-way only, via internal/quests.Status.
//  5. The rendered journal body — only when nothing else distinguishes the branch, because
//     reading it opens a modal.
//
// # Two consequences that cost the upstream project live runs
//
//   - StatusUnknown is NOT StatusNotStarted. The quest tab is not coloured for the first
//     moments after login, and treating that as "not started" restarts a finished quest. Every
//     module must return a wait step for it, and RuneMysteries does.
//   - A held quest item is part of the state machine's memory, which is why a step that hands
//     an item over and a step that acquires it must never both be reachable from one snapshot.
//
// # Deliberately not ported, and why
//
//   - The other ~53 quest defs under defs/. This package is a bounded first slice: the engine
//     plus Rune Mysteries (which gates EssenceMiner) and, in the sibling barcrawlplan package,
//     Alfred Grimhand's Barcrawl (which gates the Barbarian Outpost gate BarbarianAgility
//     walks through). Nothing here is quest-specific beyond runemysteries.go.
//   - prayer.ts, gear.ts and weapons.ts — the combat-gear catalogues. The provisioner that
//     replaces engine/provisioning.ts (provision.go) sources items by the owner's item rule
//     instead, and a quest that needs a weapon names it in its own kit.
//   - engine/trace.ts — the upstream step tracer, which publishes to the Electron wall's
//     on-screen overlay. HUD/overlay publishers are never ported (same rule casketplan
//     records).
//   - QuestModule's grind, pray, ownsInventory, foodReady, warnReadiness and observe hooks.
//     The ones a ported quest needs have a Go equivalent below (StepProvision for gather,
//     Module.Sustain for sustain, StepCustom for exit); the rest are declared by quests not
//     ported yet, and a hook with no implementation behind it is a claim, not a feature.
//
// # The step vocabulary (2026-09-23, lane Q)
//
// The TS union's full set is here: talk, grabGround, pickLoc, interactLoc, useOn, equip,
// scanBank, withdraw, deposit, mineRock, buy, custom, wait and done, plus four this port adds
// because the TS hid them inside custom closures: walk (a leg to a tile), opHeld (Read, Eat,
// Empty ...), fight (one NPC, with the module's sustain) and provision (make the pack hold a
// kit by the owner's item rule, see provision.go). custom names an executor the quest's own
// runner package registers; the pure side stays a name, so decide() is still pure.
package questplan

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Status is the quest tab's three-way colour classification, mirroring internal/quests.
// QuestStatus. It is duplicated as its own type rather than imported so this package stays
// free of every other package, which is what makes it unit-testable without a session; the
// runner converts.
type Status string

const (
	// StatusNotStarted is a red quest-tab entry.
	StatusNotStarted Status = "notStarted"
	// StatusInProgress is a yellow one.
	StatusInProgress Status = "inProgress"
	// StatusComplete is a green one.
	StatusComplete Status = "complete"
	// StatusUnknown means no colour has been observed yet. It is NOT StatusNotStarted: the
	// quest tab is uncoloured for the first moments after login, and a module that treats
	// the two alike restarts a finished quest.
	StatusUnknown Status = "unknown"
)

// Snapshot is everything decide() may look at. Ported from engine/types.ts QuestSnapshot,
// minus the fields only the unported combat quests use (prayer, attack, ranged).
type Snapshot struct {
	// Journal is the quest tab's colour for this quest.
	Journal Status
	// Inv counts held items by lower-cased display name.
	Inv map[string]int
	// InvIDs counts held items by exact object ID. Use it wherever two objects share a
	// display name: Inv silently accepts the wrong one and every downstream check then
	// passes for the wrong reason.
	InvIDs map[int]int
	// Worn holds equipped items by lower-cased display name.
	Worn map[string]int
	// WornIDs holds equipped items by exact object ID.
	WornIDs map[int]int
	// NoProgress is how many consecutive decide ticks have produced an identical
	// ProgressSignature. A module may use it to rotate through recovery probes.
	NoProgress int
	// BankCoins is the coin stack the last bank read saw.
	BankCoins int
	// Bank counts banked items by lower-cased display name, from the last complete read.
	Bank map[string]int
	// BankIDs counts banked items by exact object ID.
	BankIDs map[int]int
	// BankKnown is false until a bank has actually been read. An unread bank is not an
	// empty one, and a bare "banked(x) > 0" test on an unread bank sent the upstream bot
	// across two kingdoms for a pickaxe it already owned.
	BankKnown bool
	// Tile is where the account is standing, if a position packet has arrived.
	Tile tile.Tile
	// TileKnown is false before the first position packet.
	TileKnown bool
	// FreeSlots is the free inventory count.
	FreeSlots int

	// Stage is the module's own reading of the quest stage (Module.ReadStage over the
	// rendered journal), valid only when StageKnown. An unread or unrecognised journal is
	// StageKnown false, never stage 0.
	Stage      int
	StageKnown bool
	// HP and MaxHP are the current and base Hitpoints.
	HP, MaxHP int
	// Levels holds base (unboosted) skill levels by lower-case skill name.
	Levels map[string]int
	// QuestPoints is varp 101.
	QuestPoints int
	// Deaths is how many deaths the engine has counted during this quest (all runs, from the
	// side store).
	Deaths int
	// Failed names the custom steps and provision kits that reported failure on their last
	// run, so decide() can pick a fallback instead of repeating the same step until parking.
	// It is engine memory, but only ever of what a step itself reported.
	Failed map[string]bool
	// Blocked is the part of Failed that is a provision kit ("provision:"+kit) whose last round
	// ended with every source dead (the plan was BLOCKED), as against one that failed on the
	// way, a walk to the bank or a shop that timed out. A blocked kit does not mend by trying
	// again at once, so decide() waits on it (and the quest parks, freeing its one-at-a-time
	// slot); a kit that merely failed on the way is tried again, and the walk carries on.
	Blocked map[string]bool
	// Ran names the custom steps and provision kits that succeeded since the quest's run
	// started (or since the last death), so decide() can order a fallback after a first try.
	Ran map[string]bool
	// QuestsDone names, by ladder id, the quests this module's soft prerequisites
	// (LadderFacts SoftPrereqs) read complete on the quest tab. A quest that is absent, or whose
	// tab is not coloured yet, is not known complete: an uncoloured tab is not a reading.
	QuestsDone map[string]bool
	// Pair is the account's partner for a two-account quest (Shield of Arrav, Heroes' Quest), as the runner's PairLink reports it: the zero
	// value is "no partner configured", which a pair quest answers with a long wait and never a guess.
	Pair PairState
}

// QuestDone reports whether the soft prerequisite quest id read complete (QuestsDone).
func (s Snapshot) QuestDone(id string) bool { return s.QuestsDone[id] }

// Level reports a base skill level by lower-case name, 0 when unknown.
func (s Snapshot) Level(skill string) int { return s.Levels[strings.ToLower(skill)] }

// Owned counts an exact object id held, worn and (when read) banked.
func (s Snapshot) Owned(id int) int {
	n := s.InvIDs[id] + s.WornIDs[id]
	if s.BankKnown {
		n += s.BankIDs[id]
	}
	return n
}

// Held reports how many of a display name the pack holds. Name matching is case-insensitive.
func (s Snapshot) Held(name string) int { return s.Inv[strings.ToLower(name)] }

// Has reports whether the pack holds any of a display name.
func (s Snapshot) Has(name string) bool { return s.Held(name) > 0 }

// HeldID reports how many of an exact object id the pack holds. Prefer it over Held wherever
// display names collide.
func (s Snapshot) HeldID(id int) int { return s.InvIDs[id] }

// Banked reports how many of a display name the last bank read saw, and false when no bank
// has been read yet. The second return is what separates "none banked" from "unknown".
func (s Snapshot) Banked(name string) (int, bool) {
	if !s.BankKnown {
		return 0, false
	}
	return s.Bank[strings.ToLower(name)], true
}

// BankedID is Banked by exact object id.
func (s Snapshot) BankedID(id int) (int, bool) {
	if !s.BankKnown {
		return 0, false
	}
	return s.BankIDs[id], true
}

// StepKind names one of the closed set of things a module may ask for. Go has no sum type, so
// the TS discriminated union becomes a Kind plus the fields that kind reads; ValidateStep
// enforces that the fields a kind needs are actually set.
type StepKind string

const (
	// StepTalk walks to an NpcStop and drives its dialogue to completion.
	StepTalk StepKind = "talk"
	// StepInteractLoc operates a named loc by op name.
	StepInteractLoc StepKind = "interactLoc"
	// StepUseOn uses a held item on an npc, loc or item.
	StepUseOn StepKind = "useOn"
	// StepWithdraw draws named items from the bank.
	StepWithdraw StepKind = "withdraw"
	// StepDeposit banks everything but a keep list.
	StepDeposit StepKind = "deposit"
	// StepScanBank opens a bank purely so the snapshot learns what is in it.
	StepScanBank StepKind = "scanBank"
	// StepWalk travels to Anchor (within Radius), keeping out of Avoid.
	StepWalk StepKind = "walk"
	// StepOpHeld sends an item op (Read, Eat, Empty ...) on a held item.
	StepOpHeld StepKind = "opHeld"
	// StepEquip wears a held item.
	StepEquip StepKind = "equip"
	// StepGrabGround picks a ground item up near Anchor.
	StepGrabGround StepKind = "grabGround"
	// StepPickLoc operates a loc (Op) until ExpectItem arrives: picking, or searching a crate.
	StepPickLoc StepKind = "pickLoc"
	// StepMineRock mines Loc until Qty of Item are held.
	StepMineRock StepKind = "mineRock"
	// StepBuy buys Qty of Item from Shop.
	StepBuy StepKind = "buy"
	// StepFight attacks one NPC (Target/TargetID near Anchor) and fights it to a kill, eating
	// by the module's Sustain.
	StepFight StepKind = "fight"
	// StepProvision makes the pack hold Kit, sourcing each line by the owner's item rule
	// (provision.go): own bank, one bundled mule ask, ground spawns, own skills, shops.
	StepProvision StepKind = "provision"
	// StepCustom runs the executor the quest's runner registered under Custom.
	StepCustom StepKind = "custom"
	// StepWait advances nothing and says why.
	StepWait StepKind = "wait"
	// StepDone means the quest is finished.
	StepDone StepKind = "done"
)

// TargetKind is what a StepUseOn aims at.
type TargetKind string

const (
	// TargetNpc is a use-item-on-npc (OPNPCU).
	TargetNpc TargetKind = "npc"
	// TargetLoc is a use-item-on-loc (OPLOCU).
	TargetLoc TargetKind = "loc"
	// TargetItem is a use-item-on-item.
	TargetItem TargetKind = "item"
)

// WithdrawItem is one line of a StepWithdraw. ID is optional and wins over Name when set,
// for the objects whose display names collide.
type WithdrawItem struct {
	Name string
	Qty  int
	ID   int
}

// Step is what decide() returns. Only the fields the Kind names are read.
type Step struct {
	Kind StepKind

	// Stop is read by StepTalk.
	Stop NpcStop

	// Loc, Op and Anchor are read by StepInteractLoc.
	Loc    string
	Op     string
	Anchor tile.Tile
	// ExpectItem, when set, is the item StepInteractLoc must see arrive to count.
	ExpectItem string

	// Item, TargetKind and Target are read by StepUseOn; Anchor is shared with
	// StepInteractLoc.
	Item       string
	TargetKind TargetKind
	Target     string
	// TargetID disambiguates same-named targets. 0 means "match by name only".
	TargetID int

	// Items is read by StepWithdraw.
	Items []WithdrawItem
	// Keep and KeepIDs are read by StepDeposit. KeepIDs is combined with Keep rather than
	// replacing it.
	Keep    []string
	KeepIDs []int
	// Bank overrides the module's bank for this step when BankSet is true.
	Bank    tile.Tile
	BankSet bool

	// Reason is read by StepWait, and logged beside a StepCustom.
	Reason string
	// ParkFor, on a StepWait, is a wait that will not end for hours (a stats gate): the engine parks
	// the quest at once for this long instead of waiting WaitPark ticks and parking for the
	// escalating Backoff.
	ParkFor time.Duration

	// ItemID disambiguates Item (StepUseOn, StepOpHeld, StepEquip, StepGrabGround,
	// StepMineRock, StepBuy). 0 means match by name.
	ItemID int
	// LocID disambiguates Loc (StepInteractLoc, StepUseOn on a loc, StepPickLoc,
	// StepMineRock). 0 means match by name.
	LocID int
	// ExpectItemID is ExpectItem by exact id (StepInteractLoc, StepPickLoc).
	ExpectItemID int
	// UntilItemID, on a StepTalk, keeps driving the stop's dialogue (DriveUntil) until this
	// item is held: for a conversation with gaps longer than a page turn (Golrie's).
	UntilItemID int
	// Radius is StepWalk's arrival radius (0 is exact).
	Radius int
	// Avoid is rectangles StepWalk's long walk must keep out of.
	Avoid []Rect
	// Qty is read by StepMineRock and StepBuy, and by a custom step that takes a count
	// (Waterfall's free slots).
	Qty int
	// Shop is read by StepBuy.
	Shop ShopRef
	// Kit is read by StepProvision; KitName labels it in logs and in Snapshot.Failed. A
	// provision step also reads Keep and KeepIDs: what may stay in the pack when it banks
	// first (everything that is neither kept nor a kit line is deposited).
	Kit     []KitLine
	KitName string
	// Custom names a StepCustom executor.
	Custom string
	// Bare makes the step's bank visits (StepDeposit, StepProvision, StepWithdraw) bare-pack ones: they leave the account's own
	// kit alone (questrunner.Deps.KeepInPack is not asked, Deps.AtBank does not run), so the pack ends holding only what the
	// step keeps and the kit's lines. A quest sets it on the steps of a leg that refuses that kit, such as Waterfall's tomb
	// trip, whose tomb-safe check a run staller's ring of dueling would fail after every visit.
	Bare bool
	// Spell, on a StepFight, casts instead of meleeing: a spell name, or "best" for the
	// highest-level combat spell the account can cast from its pack (a worn staff pays its
	// element). The fight is then cast from Anchor, a safespot where the quest has one.
	Spell string
}

// Rect is an inclusive tile rectangle on one level.
type Rect struct {
	MinX, MaxX, MinZ, MaxZ int
	Level                  int
}

// Contains reports whether t is inside r.
func (r Rect) Contains(t tile.Tile) bool {
	return t.Level == r.Level && t.X >= r.MinX && t.X <= r.MaxX && t.Z >= r.MinZ && t.Z <= r.MaxZ
}

// ShopRef is a shopkeeper worth walking to.
type ShopRef struct {
	Npc    string
	NpcID  int
	Anchor tile.Tile
}

// bankReason is a bank step's own reason in parentheses, so a log line says why the account went to the bank (a napkin the
// bank holds, a slot for an item an NPC is about to add), or nothing when the step names none.
func (s Step) bankReason() string {
	if s.Reason == "" {
		return ""
	}
	return " (" + s.Reason + ")"
}

// Describe is the one-line log form of a step, ported from QuestEngine.describeStep.
func (s Step) Describe() string {
	switch s.Kind {
	case StepTalk:
		return "talk to " + s.Stop.Npc
	case StepInteractLoc:
		return s.Op + " " + s.Loc
	case StepUseOn:
		return "use " + s.Item + " on " + s.Target
	case StepWithdraw:
		parts := make([]string, 0, len(s.Items))
		for _, i := range s.Items {
			parts = append(parts, fmt.Sprintf("%s x%d", i.Name, i.Qty))
		}
		return "withdraw " + strings.Join(parts, ", ") + s.bankReason()
	case StepDeposit:
		return fmt.Sprintf("bank all but %d kept item type(s)", len(s.Keep)+len(s.KeepIDs)) + s.bankReason()
	case StepScanBank:
		return "check the bank" + s.bankReason()
	case StepWalk:
		return fmt.Sprintf("walk to %d,%d,%d", s.Anchor.X, s.Anchor.Z, s.Anchor.Level)
	case StepOpHeld:
		return s.Op + " " + s.Item
	case StepEquip:
		return "equip " + s.Item
	case StepGrabGround:
		return "grab " + s.Item
	case StepPickLoc:
		return s.Op + " " + s.Loc + " for " + s.ExpectItem
	case StepMineRock:
		return fmt.Sprintf("mine %d %s", s.Qty, s.Item)
	case StepBuy:
		return fmt.Sprintf("buy %d %s from %s", s.Qty, s.Item, s.Shop.Npc)
	case StepFight:
		if s.Spell != "" {
			return "cast " + s.Spell + " at " + s.Target
		}
		return "fight " + s.Target
	case StepProvision:
		name := s.KitName
		if name == "" {
			name = "kit"
		}
		return fmt.Sprintf("provision %s (%d line(s))", name, len(s.Kit))
	case StepCustom:
		if s.Reason != "" {
			return s.Custom + " (" + s.Reason + ")"
		}
		return s.Custom
	case StepWait:
		return s.Reason
	case StepDone:
		return "done"
	}
	return string(s.Kind)
}

// AdvancesWorld reports whether a step is one the watchdog should hold to account. A wait or a
// done changes nothing by design, so neither should burn the no-progress budget.
func (s Step) AdvancesWorld() bool {
	return s.Kind != StepWait && s.Kind != StepDone
}

// ValidateStep reports why a step is malformed, or "" when it is well formed. It exists
// because Go's struct-plus-Kind encoding of a TS discriminated union cannot make an unset
// field a compile error the way the union does, so the check moves to a test: every module's
// every reachable step is validated in questplan's own tests.
func ValidateStep(s Step) string {
	switch s.Kind {
	case StepTalk:
		if s.Stop.Npc == "" {
			return "talk step with no npc"
		}
		if len(s.Stop.Prefer) == 0 {
			return "talk step for " + s.Stop.Npc + " with an empty prefer list"
		}
		if s.Stop.Leash <= 0 {
			return "talk step for " + s.Stop.Npc + " with a non-positive leash"
		}
	case StepInteractLoc:
		if (s.Loc == "" && s.LocID == 0) || s.Op == "" {
			return "interactLoc step missing a loc or an op"
		}
	case StepUseOn:
		if (s.Item == "" && s.ItemID == 0) || (s.Target == "" && s.TargetID == 0) {
			return "useOn step missing an item or a target"
		}
		switch s.TargetKind {
		case TargetNpc, TargetLoc, TargetItem:
		default:
			return "useOn step with an unknown target kind " + string(s.TargetKind)
		}
	case StepWithdraw:
		if len(s.Items) == 0 {
			return "withdraw step with no items"
		}
		for _, i := range s.Items {
			if i.Name == "" && i.ID == 0 {
				return "withdraw step with an item that names neither a name nor an id"
			}
			if i.Qty <= 0 {
				return "withdraw step asking for a non-positive quantity of " + i.Name
			}
		}
	case StepWalk:
		if s.Radius < 0 {
			return "walk step with a negative radius"
		}
	case StepOpHeld:
		if (s.Item == "" && s.ItemID == 0) || s.Op == "" {
			return "opHeld step missing an item or an op"
		}
	case StepEquip:
		if s.Item == "" && s.ItemID == 0 {
			return "equip step with no item"
		}
	case StepGrabGround:
		if s.Item == "" && s.ItemID == 0 {
			return "grabGround step with no item"
		}
	case StepPickLoc:
		if (s.Loc == "" && s.LocID == 0) || s.Op == "" || (s.ExpectItem == "" && s.ExpectItemID == 0) {
			return "pickLoc step missing a loc, an op or the item it yields"
		}
	case StepMineRock:
		if (s.Loc == "" && s.LocID == 0) || (s.Item == "" && s.ItemID == 0) || s.Qty <= 0 {
			return "mineRock step missing a rock, an item or a positive quantity"
		}
	case StepBuy:
		if (s.Item == "" && s.ItemID == 0) || s.Qty <= 0 || (s.Shop.Npc == "" && s.Shop.NpcID == 0) {
			return "buy step missing an item, a positive quantity or a shop"
		}
	case StepFight:
		if s.Target == "" && s.TargetID == 0 {
			return "fight step with no target"
		}
	case StepProvision:
		if len(s.Kit) == 0 {
			return "provision step with an empty kit"
		}
		for _, l := range s.Kit {
			if why := ValidateKitLine(l); why != "" {
				return "provision step: " + why
			}
		}
	case StepCustom:
		if s.Custom == "" {
			return "custom step with no executor name"
		}
	case StepDeposit, StepScanBank, StepDone:
	case StepWait:
		if s.Reason == "" {
			return "wait step with no reason"
		}
	default:
		return "unknown step kind " + string(s.Kind)
	}
	return ""
}

// NpcStop is an NPC worth walking to and the dialogue to drive once there. Ported from
// exec/primitives.ts.
type NpcStop struct {
	// Npc is the display name, as internal/cachecfg resolves it.
	Npc string
	// Anchor is where the NPC normally stands.
	Anchor tile.Tile
	// Leash is how far from the account the NPC may be and still count as reached.
	Leash int
	// Prefer is the dialogue options to choose, in order of preference, matched as
	// case-insensitive substrings of the option text rather than by index — so the server
	// reordering a menu does not break the quest.
	Prefer []string
	// Approach is waypoints to walk through before the anchor, for an anchor the long walk
	// cannot pin directly.
	Approach []tile.Tile
	// ID, when non-zero, is the exact NPC id to aim at. Set it wherever the display name is
	// shared: "Bartender" names seven different NPCs in this content build.
	ID int
}

// LadderHop is one level change a quest needs. Ported from exec/primitives.ts LadderHop.
type LadderHop struct {
	// Stand is where the account must be to operate the loc.
	Stand tile.Tile
	// Walk is the long-walk destination when Stand itself is behind a door the baked graph
	// cannot pin. Zero means walk to Stand.
	Walk tile.Tile
	// WalkSet distinguishes an unset Walk from tile 0,0,0.
	WalkSet bool
	// LocName and Op name the ladder and the option to send.
	LocName string
	Op      string
	// Open is an op to send first when the loc offering Op is not found (a trapdoor that
	// must be opened before it can be climbed). Empty means there is none.
	Open string
	// Arrive is where the account lands, used to confirm the hop actually happened.
	Arrive tile.Tile
}

// WalkTarget is where a hop's long walk should aim.
func (h LadderHop) WalkTarget() tile.Tile {
	if h.WalkSet {
		return h.Walk
	}
	return h.Stand
}

// UndergroundZ is the z at or above which this map's coordinates are an underground copy.
// Ported from exec/primitives.ts isUnderground.
const UndergroundZ = 5000

// IsUnderground reports whether a tile is in the underground band.
func IsUnderground(t tile.Tile) bool { return t.Z >= UndergroundZ }

// NeedsHop reports whether getting from here to anchor crosses the underground boundary, and
// therefore needs a ladder rather than a walk.
func NeedsHop(here, anchor tile.Tile) bool { return IsUnderground(here) != IsUnderground(anchor) }

// NearestHopFrom picks the hop to take out of where the account is standing: the closest one
// on the same side of the underground boundary. ok is false when no hop qualifies.
func NearestHopFrom(here tile.Tile, hops []LadderHop) (LadderHop, bool) {
	best, found := LadderHop{}, false
	for _, h := range hops {
		if IsUnderground(h.Stand) != IsUnderground(here) {
			continue
		}
		if !found || tile.Chebyshev(h.Stand, here) < tile.Chebyshev(best.Stand, here) {
			best, found = h, true
		}
	}
	return best, found
}

// Module is one quest's decisions. Ported from engine/types.ts QuestModule, minus the hooks
// this slice does not implement (see the package doc).
type Module struct {
	// Record is the quest's identity and requirements.
	Record Record
	// Hops are the level changes this quest needs.
	Hops []LadderHop
	// Bank is the bank this quest uses. BankSet false means "let the step work it out".
	Bank    tile.Tile
	BankSet bool
	// Tools are the quest items the provisioner must not bank between steps.
	Tools []string
	// ToolIDs is Tools by exact object id.
	ToolIDs []int
	// Decide is the pure decision function.
	Decide func(Snapshot) Step

	// ReadStage, when set, parses the rendered journal into an exact stage. The engine then
	// reads the journal while the quest is in progress (again after every step that advanced
	// the world, since a journal read opens a modal) and passes it as Snapshot.Stage.
	ReadStage func(lines []string) (int, bool)
	// Sustain is the quest's survival policy: what counts as food and when to eat.
	Sustain Sustain
	// Anchor is where the quest's work mostly happens (its bank, usually), for a caller's
	// flee reference and the ladder's travel estimate.
	Anchor tile.Tile
	// MaxDeaths parks the quest after this many deaths (0 means no cap).
	MaxDeaths int
	// MaxNearDeaths parks the quest for a day after this many fights the account was losing and
	// walked away from inside NearDeathWindow (0 means NearDeathsToPark). A quest whose fight needs
	// more luck than that takes a lower number.
	MaxNearDeaths int
	// StepDeathCaps caps deaths on one custom step (by its Custom name) inside a window: the
	// Cap-th death there inside Window parks the whole quest for Skip, whatever MaxDeaths says,
	// so a fight that keeps killing accounts is not walked back into after a short backoff.
	StepDeathCaps map[string]StepDeathCap
	// ExclusiveDone, for a quest with a one-at-a-time step (questrunner's exclusive quests: only
	// one account at a time may be in it), reports from a snapshot that the account is past that
	// step. The engine then gives up the fleet-wide slot at once instead of holding it for the
	// whole run, and remembers it in the side store so the quest's later runs do not queue for
	// it. Nil keeps the slot for the whole run.
	ExclusiveDone func(Snapshot) bool
	// NoRetaliate turns auto-retaliate off for the quest and never fights back: a quest whose
	// route runs through monsters the account cannot beat (Waterfall's dungeon at 20 HP) keeps
	// moving instead (the Waterfall study's rule).
	NoRetaliate bool
}

// StepDeathCap is one step's death cap (Module.StepDeathCaps): Max deaths on the step inside
// Window park the quest for Skip.
type StepDeathCap struct {
	Max    int
	Window time.Duration
	Skip   time.Duration
}

// Sustain is a quest's survival policy, ported from types.ts QuestSustain.
type Sustain struct {
	// FoodIDs are the foods the quest carries and eats, best first.
	FoodIDs []int
	// EatAt is the Hitpoints at or below which to eat; 0 means never eat between steps.
	EatAt int
}

// IsFood reports whether an object id is one of the sustain's foods.
func (s Sustain) IsFood(id int) bool {
	for _, f := range s.FoodIDs {
		if f == id {
			return true
		}
	}
	return false
}

// FoodHeld counts the sustain's foods in a pack.
func (s Sustain) FoodHeld(inv map[int]int) int {
	n := 0
	for _, f := range s.FoodIDs {
		n += inv[f]
	}
	return n
}

// Record is a quest's identity, its requirements and the items it needs. Ported from types.ts
// QuestRecord.
type Record struct {
	ID           string
	Name         string
	QuestPoints  int
	Requirements Requirements
	Items        []Item
}

// Requirements is what the server or the wiki gates a quest on. It deliberately does not carry
// combat or gear floors: those are not server gates, and recording an unproven one as a
// requirement blocks a quest that would in fact run.
type Requirements struct {
	MinQuestPoints int
	Skills         []SkillReq
	// Quests names prerequisite quest IDs.
	Quests []string
}

// SkillReq is one skill gate.
type SkillReq struct {
	Skill string
	Level int
}

// ItemKind separates "you cannot start this" from "the bot will go and get it".
type ItemKind string

const (
	// MustHave blocks the quest when it is missing.
	MustHave ItemKind = "mustHave"
	// Acquirable is sourced by the module itself.
	Acquirable ItemKind = "acquirable"
)

// Item is one item requirement.
type Item struct {
	Name string
	Qty  int
	Kind ItemKind
}

// PlayerState is what RequirementChecker reads.
type PlayerState struct {
	QuestPoints     int
	SkillLevels     map[string]int
	CompletedQuests map[string]bool
}

// BankSnapshot is what ItemChecker reads: item counts plus whether a bank was actually read.
type BankSnapshot struct {
	Counts map[string]int
	// Known is false until a bank has been read. An unread bank is not an empty one.
	Known bool
}

// RequirementResult is one requirement and why it failed.
type RequirementResult struct {
	OK     bool
	Reason string
}

// CheckRequirements ports RequirementChecker.checkRequirements.
func CheckRequirements(r Record, p PlayerState) []RequirementResult {
	var out []RequirementResult
	if r.Requirements.MinQuestPoints > 0 {
		ok := p.QuestPoints >= r.Requirements.MinQuestPoints
		reason := ""
		if !ok {
			reason = fmt.Sprintf("needs %d quest points (have %d)", r.Requirements.MinQuestPoints, p.QuestPoints)
		}
		out = append(out, RequirementResult{OK: ok, Reason: reason})
	}
	for _, s := range r.Requirements.Skills {
		have := p.SkillLevels[s.Skill]
		ok := have >= s.Level
		reason := ""
		if !ok {
			reason = fmt.Sprintf("needs %s %d (have %d)", skillLabel(s.Skill), s.Level, have)
		}
		out = append(out, RequirementResult{OK: ok, Reason: reason})
	}
	for _, q := range r.Requirements.Quests {
		ok := p.CompletedQuests[q]
		reason := ""
		if !ok {
			reason = "prerequisite quest not complete: " + q
		}
		out = append(out, RequirementResult{OK: ok, Reason: reason})
	}
	return out
}

func skillLabel(skill string) string {
	if skill == "" {
		return skill
	}
	return strings.ToUpper(skill[:1]) + skill[1:]
}

// ItemResult is one item requirement and whether it holds.
type ItemResult struct {
	Name       string
	Qty        int
	Kind       ItemKind
	Present    int
	OK         bool
	WillGather bool
}

// CheckItems ports ItemChecker.checkItems. A MustHave item on an unread bank passes, because
// blocking on a bank nothing has opened yet stops the queue before it ever opens a booth.
func CheckItems(r Record, snap BankSnapshot) []ItemResult {
	out := make([]ItemResult, 0, len(r.Items))
	for _, item := range r.Items {
		present := snap.Counts[strings.ToLower(item.Name)]
		if item.Kind == MustHave {
			out = append(out, ItemResult{
				Name: item.Name, Qty: item.Qty, Kind: item.Kind, Present: present,
				OK: present >= item.Qty || !snap.Known,
			})
			continue
		}
		out = append(out, ItemResult{
			Name: item.Name, Qty: item.Qty, Kind: item.Kind, Present: present,
			OK: true, WillGather: present < item.Qty,
		})
	}
	return out
}

// Eligibility is a quest's verdict with its reasons. Ported from types.ts QuestEligibility.
type Eligibility struct {
	ID      string
	Name    string
	Status  EligibilityStatus
	Reasons []string
}

// EligibilityStatus is DONE, READY or BLOCKED.
type EligibilityStatus string

const (
	// EligDone means the quest tab already reads complete.
	EligDone EligibilityStatus = "DONE"
	// EligReady means every requirement holds.
	EligReady EligibilityStatus = "READY"
	// EligBlocked means at least one does not, and Reasons says which.
	EligBlocked EligibilityStatus = "BLOCKED"
)

// Evaluate ports EligibilityEvaluator.evaluate.
//
// Item requirements gate *starting* a quest only. Re-imposing them on a quest already in
// progress blocks resuming it, since by then the items may legitimately be gone — handed over,
// ground up, drunk. Skill and quest-point requirements are checked either way, because nothing
// consumes a level or a quest point.
func Evaluate(r Record, p PlayerState, bank BankSnapshot, journal Status) Eligibility {
	if journal == StatusComplete {
		return Eligibility{ID: r.ID, Name: r.Name, Status: EligDone}
	}
	var reasons []string
	for _, res := range CheckRequirements(r, p) {
		if !res.OK {
			reasons = append(reasons, res.Reason)
		}
	}
	if journal != StatusInProgress {
		for _, it := range CheckItems(r, bank) {
			if !it.OK {
				reasons = append(reasons, fmt.Sprintf("missing item: %s x%d (have %d)", it.Name, it.Qty, it.Present))
			}
		}
	}
	status := EligReady
	if len(reasons) > 0 {
		status = EligBlocked
	}
	return Eligibility{ID: r.ID, Name: r.Name, Status: status, Reasons: reasons}
}

// QueueStatus is a row's state in the queue view.
type QueueStatus string

const (
	// QueueDone is a finished quest.
	QueueDone QueueStatus = "DONE"
	// QueueRunning is the one quest currently being driven.
	QueueRunning QueueStatus = "RUNNING"
	// QueueReady is eligible but not running.
	QueueReady QueueStatus = "READY"
	// QueueParked is eligible but gave up making progress.
	QueueParked QueueStatus = "PARKED"
	// QueueBlocked is ineligible, with reasons.
	QueueBlocked QueueStatus = "BLOCKED"
	// QueueUnknown is a quest eligibility has not been evaluated for.
	QueueUnknown QueueStatus = "UNKNOWN"
)

// QueueRow is one line of the queue view.
type QueueRow struct {
	ID      string
	Name    string
	Status  QueueStatus
	Reasons []string
}

// NextQuest ports engine/queue.ts nextQuest: the first picked, READY quest that is not parked,
// falling back to the first READY one at all so a fully parked queue still reports something
// to run rather than stopping.
func NextQuest(order []string, picked map[string]bool, elig map[string]Eligibility, parked map[string]bool) string {
	var ready []string
	for _, id := range order {
		if picked[id] && elig[id].Status == EligReady {
			ready = append(ready, id)
		}
	}
	for _, id := range ready {
		if !parked[id] {
			return id
		}
	}
	if len(ready) > 0 {
		return ready[0]
	}
	return ""
}

// QueueRows ports engine/queue.ts queueRows.
func QueueRows(order []string, picked map[string]bool, elig map[string]Eligibility, parked map[string]bool, running string) []QueueRow {
	var out []QueueRow
	for _, id := range order {
		if !picked[id] {
			continue
		}
		el, ok := elig[id]
		switch {
		case id == running:
			name := el.Name
			if name == "" {
				name = id
			}
			out = append(out, QueueRow{ID: id, Name: name, Status: QueueRunning})
		case !ok:
			out = append(out, QueueRow{ID: id, Name: id, Status: QueueUnknown, Reasons: []string{"eligibility not evaluated yet"}})
		case parked[id] && el.Status == EligReady:
			out = append(out, QueueRow{ID: id, Name: el.Name, Status: QueueParked, Reasons: []string{"no progress — parked"}})
		default:
			out = append(out, QueueRow{ID: id, Name: el.Name, Status: QueueStatus(el.Status), Reasons: el.Reasons})
		}
	}
	return out
}

// NoProgressWarn and NoProgressPark are the watchdog's two thresholds, from engine/watchdog.ts.
//
// A step that keeps failing must eventually park. A failing step that never parks is the worst
// outcome available: the bot looks busy forever.
const (
	NoProgressWarn = 3
	NoProgressPark = 8
)

// ProgressSignature is the string the watchdog compares tick to tick. Anything that counts as
// the world having moved must appear in it, and nothing else may: a signature that includes a
// clock never repeats, and a watchdog that never sees a repeat never parks.
//
// Ported from engine/watchdog.ts progressSignature. Maps are sorted here because Go's map
// iteration order is randomised, and an unsorted signature would differ from itself on the very
// next tick — which would silently disable parking altogether.
func ProgressSignature(s Snapshot) string {
	tileStr := "?"
	if s.TileKnown {
		tileStr = fmt.Sprintf("%d,%d,%d", s.Tile.X, s.Tile.Z, s.Tile.Level)
	}
	stage := "?"
	if s.StageKnown {
		stage = fmt.Sprint(s.Stage)
	}
	return fmt.Sprintf("%s|stage:%s|tile:%s|inv:%s|invIds:%s|worn:%s|wornIds:%s",
		s.Journal, stage, tileStr,
		sortedNameCounts(s.Inv), sortedIDCounts(s.InvIDs),
		sortedNameCounts(s.Worn), sortedIDCounts(s.WornIDs))
}

func sortedNameCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", k, m[k]))
	}
	return strings.Join(parts, ",")
}

func sortedIDCounts(m map[int]int) string {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d:%d", k, m[k]))
	}
	return strings.Join(parts, ",")
}

// Watchdog counts consecutive identical snapshots. Ported from engine/watchdog.ts.
type Watchdog struct {
	last  string
	count int
}

// Note records a signature and returns how many consecutive ticks have now produced it. Zero
// means the world just moved.
func (w *Watchdog) Note(signature string) int {
	if signature != w.last {
		w.last = signature
		w.count = 0
		return 0
	}
	w.count++
	return w.count
}

// Count is the current run length without recording anything, which is what a snapshot's
// NoProgress carries so a module can rotate recovery probes on it.
func (w *Watchdog) Count() int { return w.count }

// Reset forgets the last signature, which a caller does when it switches quests.
func (w *Watchdog) Reset() {
	w.last = ""
	w.count = 0
}
