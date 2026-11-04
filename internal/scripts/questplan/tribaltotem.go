package questplan

import (
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Tribal Totem (content id `totem`, %totemquest 0 to 5, not transmitted), ported from the TS def (defs/tribaltotem.ts) and checked against
// content 289: quests/quest_totem/{configs,scripts,interfaces}, areas/area_brimhaven/scripts/kangai_mau.rs2, areas/area_ardougne_east/
// scripts/{rdpt_employee,wizard_cromperty}.rs2 and the maps (m43_49, m41_51). One quest point, 1,775 Thieving experience
// (stat_advance 17750 at 10x) and five swordfish; Thieving 21 is the one gate, and it is enforced only at the trap on the stairs.
//
// The story, with the stage each step moves %totemquest to:
//
//	0   Kangai Mau, in the Shrimp & Parrot at Brimhaven: "I'm in search of adventure!" then "Ok, I will get it back." (1).
//	1   The Wizard Cromperty's experimental teleport block is in a crate at the R.P.D.T. depot in East Ardougne, and the crate that
//	    carries the parcel for Lord Handelmort is its neighbour: Investigate (op 2) on the mansion's crate (loc 2707) peels off its
//	    address label (an Address label, 1858, handed over by a message box; it comes again for as long as the pack holds none),
//	    and the label used on the other crate (loc 2708) covers the Wizards' Tower one: 2.
//	2   The R.P.D.T. employee, "So, when are you going to deliver this crate?": the parcel is delivered "now" (3).
//	3   The Wizard Cromperty, who has invented the teleport block: "So what have you invented?", "Can I be teleported please?",
//	    "Yes, that sounds good. Teleport me!" teleports the player to the block, which is now at Lord Handelmort's mansion, inside its
//	    walls at 2638,3321 (4, set by the script as the player lands).
//	4   Inside the mansion the way on is a security door (loc 2705) whose lock is four letters on an interface (tribal_door2), a
//	    trapped staircase (loc 2711) and a chest upstairs (locs 2709 and 2710). The combination is KURT ("Lord Francis Kurt
//	    Handelmort", from the Tourist Guide to Ardougne): the dials are lock values 10, 20, 17 and 19 of an A-to-Z wheel, and the
//	    check is the script's own, so nothing in the pack matters. The lock's state is bit 0 of %handelmort_traps_disabled, the trap's
//	    bit 21; neither is transmitted, the first shows in the journal. The stairs drop a player who has not noted the trap through
//	    to the Ardougne sewers for a fifth of their hitpoints (plus one); Investigate (op 2) on them notes it for a player of
//	    Thieving 21 or more and says so in a message box. The chest is opened (2709 to 2710) and searched for the totem, and it gives
//	    none to a player who has one in the pack or the bank. The way out is the staircase down upstairs, the door and the mansion's
//	    front door, which opens from the inside only.
//	4   with the totem: Kangai Mau takes it (5), and pays the experience and five swordfish.
//
// What the content does that a script must plan for:
//   - The Wizard's teleport lands a few ticks after his dialogue closes (rpdt_teleport: if_close, a four-tick delay, the jump). The
//     custom step waits for the mansion's tile, so the engine's next look at the world does not talk to him a second time.
//   - Every message box in the content is continued, never closed: closing one aborts the script it was raised by (the label is
//     handed over after its box, the label used on the crate is consumed after its box, the totem is granted after its box).
//   - The lock's interface is a main modal that nothing here may close: the dials are clicked one letter per click, each click
//     answered by the server writing the new letter into the dial's text (tribal_door_combo), so the next click waits for it.
//   - The mansion is not a pocket. Its front door opens only from inside, and the walk graph has the door in that direction alone
//     (door 2706 from 2635,3322 to 2635,3321); the trapped staircase is not in the graph at all (the staircase down upstairs, 1723,
//     is), so no walk can climb into the trap. Both are proven on the real pack in questmods/totemfish_pack_test.go.
//   - Eight level 44 guard dogs ("cowardly": they hunt only accounts of combat 88 or less, and only within a tile of themselves)
//     keep the garden. Two stand near the mansion (2641,3315 and 2646,3325), and the walk out of the front door runs straight south
//     along x 2635, three tiles or more from either one's whole wander box; Module.NoRetaliate keeps an account from turning on one.
//
// The item rule: the quest needs nothing but coins for the ships (Ardougne and Brimhaven are two boat rides apart, and the walker
// plans no crossing it cannot pay for); one optional supplies pass asks the bank and the mule for them. Everything else is quest
// items the quest hands out and the bank must never hold, so they are in the keep list.

// TribalTotemName is the quest tab's spelling (component 7381, `questlist:totem`).
const TribalTotemName = "Tribal Totem"

// TribalTotemID is the engine's id.
const TribalTotemID = "totem"

// The %totemquest stages (quest_totem.constant). TotemUnlocked is not a varp value: it is the journal's "I worked out the combination
// for the door" at stage 4, so that the decision can tell a lock to solve from a staircase to climb.
const (
	TotemNotStarted     = 0
	TotemStarted        = 1
	TotemCrateMarked    = 2
	TotemCrateDelivered = 3
	TotemTeleported     = 4
	TotemComplete       = 5
	TotemUnlocked       = 41
)

// Ids from content/pack (checked against the production config by TestTotemFishingIdsMatchTheProductionConfig).
const (
	NpcKangaiMau       = 846 // [kangai_mau]
	NpcRPDTEmployee    = 843 // [rpdt_employee]
	NpcWizardCromperty = 844 // [ardounge_wizard], "Wizard Cromperty"
	ObjTribalTotem     = 1857
	ObjTotemLabel      = 1858 // [tribal_totem_label], "Address label"
	LocHornCrate       = 2707 // the crate carrying Lord Handelmort's parcel, op 2 Investigate: the label
	LocTeleportCrate   = 2708 // the crate carrying Cromperty's block (and the one in the mansion), the label is used on it
	LocComboDoor       = 2705 // [combodoor], op 1 Open: the lock's interface until it is solved
	LocMansionDoor     = 2706 // [tribaltotemdoor], the front door: opens from the inside only
	LocTrapStairs      = 2711 // [totemtrapstairs], op 1 Climb-up, op 2 Investigate
	LocTotemChestShut  = 2709 // [totemshutchest], op 1 Open
	LocTotemChestOpen  = 2710 // [totemopenchest], op 1 Search, op 2 Close
	LocMansionStairsDn = 1723 // the staircase down upstairs (a graph edge)
)

// The lock's interface (tribal_door2, packed as 716; its components are 716 + 1 + their index, content/pack/interface.pack): four
// dial texts, a down and an up arrow under each, and a confirm button. The wheel is A to Z, 26 letters, and wraps both ways.
const (
	TotemLockInterface = 716
	TotemLockConfirm   = 774 // com_57
	TotemLockLetters   = 26
	// TotemCombination is the lock's answer: lock values 10, 20, 17 and 19 (K, U, R, T).
	TotemCombination = "KURT"
)

var (
	totemDialText = [4]int{760, 761, 762, 763} // com_43 to com_46
	totemDialDown = [4]int{764, 766, 768, 770} // com_47, 49, 51, 53
	totemDialUp   = [4]int{765, 767, 769, 771} // com_48, 50, 52, 54
)

// TotemDialText, TotemDialDown and TotemDialUp are the lock's components for dial i (0 to 3).
func TotemDialText(i int) int { return totemDialText[i] }

// TotemDialDown is dial i's down arrow, which steps the wheel back a letter.
func TotemDialDown(i int) int { return totemDialDown[i] }

// TotemDialUp is dial i's up arrow, which steps the wheel on a letter.
func TotemDialUp(i int) int { return totemDialUp[i] }

// TotemDialLetter reads a dial's letter from its text. The text is the wheel's letter on its own; anything else (an empty text, a
// digit) is not a reading.
func TotemDialLetter(text string) (byte, bool) {
	t := strings.ToUpper(strings.TrimSpace(text))
	if len(t) != 1 || t[0] < 'A' || t[0] > 'Z' {
		return 0, false
	}
	return t[0], true
}

// TotemDialClicks is the shortest way round the wheel from one letter to another: which arrow and how many clicks. The wheel has 26
// letters and a half turn is 13 either way, so the up arrow is taken on a tie.
func TotemDialClicks(from, to byte) (up bool, clicks int) {
	forward := (int(to) - int(from) + TotemLockLetters) % TotemLockLetters
	if forward*2 <= TotemLockLetters {
		return true, forward
	}
	return false, TotemLockLetters - forward
}

// Tiles from content maps (m43_49, m41_51) and the loc index.
var (
	// KangaiAnchor is Kangai Mau (m43_49, 2791,3182), inside the Shrimp & Parrot.
	KangaiAnchor = tile.Tile{X: 2791, Z: 3182}
	// RPDTAnchor is the employee at the depot's east end (m41_51, 2647,3271); two more stand at 2644,3274 and 2644,3276.
	RPDTAnchor = tile.Tile{X: 2647, Z: 3271}
	// DepotCrates is the depot's crate room: the mansion's crate (loc 2707) at 2650,3273 and the teleport block's (loc 2708) at
	// 2650,3271 stand two tiles apart.
	DepotCrates = tile.Tile{X: 2650, Z: 3272}
	// CrompertyAnchor is the Wizard Cromperty (m41_51, 2683,3326).
	CrompertyAnchor = tile.Tile{X: 2683, Z: 3326}
	// MansionArrival is where the teleport block lands the player (rpdt_teleport: 0_41_51_14_57), in the mansion's east alcove.
	MansionArrival = tile.Tile{X: 2638, Z: 3321}
	// ComboStand is the east side of the security door: the door (loc 2705) is a wall on the west edge of 2634,3323, and the engine
	// reaches it from that tile and from the one across the edge, 2633,3323.
	ComboStand = tile.Tile{X: 2634, Z: 3323}
	// StairsStand is the tile three north of the trapped staircase's origin (2631,3322), where its script walks the player.
	StairsStand = tile.Tile{X: 2631, Z: 3325}
	// StairsLanding is where the staircase puts the player upstairs (1_41_51_7_57).
	StairsLanding = tile.Tile{X: 2631, Z: 3321, Level: 1}
	// TotemChest is the chest upstairs (loc 2709, 2638,3324, angle 2).
	TotemChest = tile.Tile{X: 2638, Z: 3324, Level: 1}
	// MansionFrontOutside is the garden path just outside the front door.
	MansionFrontOutside = tile.Tile{X: 2635, Z: 3320}
)

// totemMansionBoxes are the tiles everything the front door seals is made of: the hall, the three bays on its south side and the
// floor above. The porch and the garden are outside.
var totemMansionBoxes = []struct{ level, x0, x1, z0, z1 int }{
	{0, 2627, 2643, 3322, 3325},
	{0, 2627, 2628, 3320, 3321},
	{0, 2631, 2633, 3320, 3321},
	{0, 2637, 2639, 3320, 3321},
	{1, 2630, 2640, 3318, 3325},
}

// InTotemMansion reports a tile inside Lord Handelmort's mansion, behind its front door.
func InTotemMansion(t tile.Tile) bool {
	for _, b := range totemMansionBoxes {
		if t.Level == b.level && t.X >= b.x0 && t.X <= b.x1 && t.Z >= b.z0 && t.Z <= b.z1 {
			return true
		}
	}
	return false
}

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomTotemLabel    = "totem-label"
	CustomTotemMark     = "totem-mark"
	CustomTotemTeleport = "totem-teleport"
	CustomTotemLock     = "totem-lock"
	CustomTotemSteal    = "totem-steal"

	KitTotemCoins = "totem-coins"
)

// The coins the ships want: Ardougne to Brimhaven is 30 each way, three crossings in all from the Ardougne side, and the supplies pass
// asks for a margin. Below TotemCoinsFloor in the pack the pass is made (once); the line is optional, because the fares are also
// drawn by the ladder's own travel hook and the pickpocket behind it.
const (
	TotemCoinsQty   = 200
	TotemCoinsFloor = 100
)

// TotemThieving is the level the trap on the stairs wants (totem_trap_stairs: stat(thieving) < 21).
const TotemThieving = 21

// TotemLevelPark is how long an account below Thieving 21 is left alone: its level does not change in an hour.
const TotemLevelPark = 6 * time.Hour

// TotemRetryPark is how long a leg that failed on its last run waits before it is tried again.
const TotemRetryPark = 30 * time.Minute

// totemFoodIDs are the foods an account that carries any eats if a dog does bite (the eat loop's floor bite takes any).
var totemFoodIDs = []int{385, 373, 379, 365, 361, 329, 333, 2309, 1891, 1893, 1895}

// totemTools are the quest's own items, which the module protects from the floor bite and every bank visit leaves in the pack: the label,
// the totem and the coins for the ships. totemKeep adds the food an account that carries any eats if a dog does bite.
var (
	totemTools = []int{ObjTotemLabel, ObjTribalTotem, CoinsID}
	totemKeep  = append(append([]int(nil), totemTools...), totemFoodIDs...)
)

// TotemKeepIDs returns the keep list (a copy).
func TotemKeepIDs() []int { return append([]int(nil), totemKeep...) }

// The stops. The Wizard's and the employee's menus are the ones the stage needs; the hand-in to Kangai has no menu, and carries the
// start's last option as its placeholder (a talk step must name one, and an unexpected menu is stopped at, not guessed at).
var (
	KangaiStartStop = NpcStop{Npc: "Kangai Mau", ID: NpcKangaiMau, Anchor: KangaiAnchor, Leash: 6,
		Prefer: []string{"I'm in search of adventure!", "Ok, I will get it back."}}
	KangaiHandInStop = NpcStop{Npc: "Kangai Mau", ID: NpcKangaiMau, Anchor: KangaiAnchor, Leash: 6,
		Prefer: []string{"Ok, I will get it back."}}
	RPDTStop = NpcStop{Npc: "RPDT employee", ID: NpcRPDTEmployee, Anchor: RPDTAnchor, Leash: 8,
		Prefer: []string{"So, when are you going to deliver this crate?"}}
	CrompertyStop = NpcStop{Npc: "Wizard Cromperty", ID: NpcWizardCromperty, Anchor: CrompertyAnchor, Leash: 8,
		Prefer: []string{"So what have you invented?", "Can I be teleported please?", "Yes, that sounds good. Teleport me!"}}
)

// totemJournalNeedles are ParseTribalTotemJournal's phrases, newest first, written against NormaliseLines (totem_journal.rs2; finished
// lines are struck through but kept, so every page repeats the ones above it and only this order separates them).
var totemJournalNeedles = []struct {
	stage  int
	needle string
}{
	{TotemComplete, "quest complete"},
	{TotemTeleported, "teleported myself inside"},
	{TotemCrateDelivered, "get wizard cromperty to teleport me inside"},
	{TotemCrateMarked, "deliver the crate now"},
	{TotemStarted, "lord handelmorts mansion"},
	{TotemNotStarted, "i can start this quest by speaking to"},
}

// ParseTribalTotemJournal reads the stage from the rendered journal; ok is false for an empty or unrecognised journal, which is
// unknown, never stage 0. At stage 4 it also tells TotemUnlocked (the lock solved) from TotemTeleported.
func ParseTribalTotemJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range totemJournalNeedles {
		if !strings.Contains(text, n.needle) {
			continue
		}
		if n.stage == TotemTeleported && strings.Contains(text, "i worked out the combination for the door") {
			return TotemUnlocked, true
		}
		return n.stage, true
	}
	return 0, false
}

// totemThievingShort reports an account whose Thieving is read and under 21. A skill the snapshot does not name is unread (BaseLevels
// leaves out a stat no packet has set), and an unread level is not judged.
func totemThievingShort(s Snapshot) bool {
	lvl, read := s.Levels["thieving"]
	return read && lvl < TotemThieving
}

// totemBank is the bank a visit uses: the one nearest the account, East Ardougne's south bank before a position has arrived.
func totemBank(s Snapshot) tile.Tile {
	if s.TileKnown {
		return NearestBank(s.Tile).Tile
	}
	return ArdougneSouthBank
}

// totemCoinsKit is the coins for the ships.
func totemCoinsKit() []KitLine {
	return []KitLine{{ID: CoinsID, Name: "Coins", Qty: TotemCoinsQty, Stackable: true, Optional: true}}
}

// totemWait is a wait that parks the quest for d with the reason.
func totemWait(d time.Duration, reason string) Step {
	return Step{Kind: StepWait, Reason: reason, ParkFor: d}
}

// totemStart is stage 0: the level, the fares, and Kangai Mau.
func totemStart(s Snapshot) Step {
	if totemThievingShort(s) {
		return totemWait(TotemLevelPark, "Thieving 21 is needed for the trap on the mansion's stairs (the account has "+strconv.Itoa(s.Level("thieving"))+")")
	}
	if s.HeldID(CoinsID) < TotemCoinsFloor && !s.Ran["provision:"+KitTotemCoins] && !s.Failed["provision:"+KitTotemCoins] {
		return Step{Kind: StepProvision, KitName: KitTotemCoins, Kit: totemCoinsKit(), KeepIDs: totemKeep, Bank: totemBank(s), BankSet: true,
			Reason: "coins for the Ardougne and Brimhaven ships"}
	}
	return Step{Kind: StepTalk, Stop: KangaiStartStop}
}

// totemInside is the stage 4 decision for an account in the mansion.
func totemInside(s Snapshot) Step {
	if s.Stage == TotemTeleported {
		if s.Failed[CustomTotemLock] {
			return totemWait(TotemRetryPark, "the lock on the mansion's security door did not take the combination on its last try")
		}
		return Step{Kind: StepCustom, Custom: CustomTotemLock, Reason: "the security door's combination, KURT"}
	}
	if totemThievingShort(s) {
		return totemWait(TotemLevelPark, "Thieving 21 is needed for the trap on the mansion's stairs (the account has "+strconv.Itoa(s.Level("thieving"))+")")
	}
	if s.Failed[CustomTotemSteal] {
		// The chest gives no totem to a player whose bank holds one: the bank is read before anything is retried.
		if !s.BankKnown {
			return Step{Kind: StepScanBank, Bank: totemBank(s), BankSet: true, Reason: "the chest was empty: a totem may be in the bank"}
		}
		return totemWait(TotemRetryPark, "the mansion's chest gave no totem on its last try and the bank holds none")
	}
	return Step{Kind: StepCustom, Custom: CustomTotemSteal, Reason: "disarm the stairs, climb them and open the chest for the totem"}
}

// DecideTribalTotem is the whole quest, read from the journal stage, the pack and where the account stands.
func DecideTribalTotem(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return totemStart(s)
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	switch s.Stage {
	case TotemNotStarted:
		return totemStart(s)
	case TotemStarted:
		if s.HeldID(ObjTotemLabel) > 0 {
			if s.Failed[CustomTotemMark] {
				return totemWait(TotemRetryPark, "the address label would not go on the Wizards' Tower crate on its last try")
			}
			return Step{Kind: StepCustom, Custom: CustomTotemMark, Reason: "cover the Wizards' Tower label with the mansion's"}
		}
		if s.Failed[CustomTotemLabel] {
			return totemWait(TotemRetryPark, "the mansion's address label could not be peeled off its crate on the last try")
		}
		if s.FreeSlots < 1 {
			return Step{Kind: StepDeposit, KeepIDs: totemKeep, Bank: totemBank(s), BankSet: true, Reason: "room for the address label"}
		}
		return Step{Kind: StepCustom, Custom: CustomTotemLabel, Reason: "peel the address label off the mansion's crate at the R.P.D.T. depot"}
	case TotemCrateMarked:
		return Step{Kind: StepTalk, Stop: RPDTStop}
	case TotemCrateDelivered:
		if s.Failed[CustomTotemTeleport] {
			return totemWait(TotemRetryPark, "the Wizard Cromperty's teleport did not take the account into the mansion on its last try")
		}
		return Step{Kind: StepCustom, Custom: CustomTotemTeleport, Reason: "have the Wizard Cromperty teleport the account into Lord Handelmort's mansion"}
	case TotemTeleported, TotemUnlocked:
		// The totem in the pack ends the quest wherever the account stands: the walk out is the generic walker's (the stairs down, the
		// door, the front door).
		if s.HeldID(ObjTribalTotem) > 0 {
			return Step{Kind: StepTalk, Stop: KangaiHandInStop}
		}
		if s.BankKnown {
			if n, _ := s.BankedID(ObjTribalTotem); n > 0 {
				return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Totem", Qty: 1, ID: ObjTribalTotem}}, Bank: totemBank(s), BankSet: true,
					Reason: "the totem is in the bank"}
			}
		}
		if s.TileKnown && InTotemMansion(s.Tile) {
			return totemInside(s)
		}
		if s.TileKnown && IsUnderground(s.Tile) {
			// A fall through the stairs' trap lands in the Ardougne sewers: out by the walker's own ladders.
			return Step{Kind: StepWalk, Anchor: ArdougneSouthBank, Radius: 3, Reason: "out of the sewers"}
		}
		if s.Failed[CustomTotemTeleport] {
			return totemWait(TotemRetryPark, "the Wizard Cromperty's teleport did not take the account into the mansion on its last try")
		}
		return Step{Kind: StepCustom, Custom: CustomTotemTeleport, Reason: "back into the mansion by the Wizard Cromperty's teleport"}
	case TotemComplete:
		return Step{Kind: StepWait, Reason: "the quest tab has not turned green yet"}
	}
	return Step{Kind: StepWait, Reason: "unrecognised Tribal Totem stage"}
}

// TribalTotemRecord is the quest's identity. Thieving 21 is the content's own gate, stated in the record so that the ladder
// holds an account short of it from the start and does not walk it to Brimhaven and Ardougne only to be refused at the stairs.
var TribalTotemRecord = Record{ID: TribalTotemID, Name: TribalTotemName, QuestPoints: 1,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "thieving", Level: TotemThieving}}}}

// TribalTotem is the engine module. It never fights (NoRetaliate: the garden's dogs hunt an account of combat 88 or less within a
// tile, and are walked away from), and a single death anywhere on its walks parks it for a day.
var TribalTotem = Module{
	Record: TribalTotemRecord, Bank: ArdougneSouthBank, BankSet: true, Anchor: DepotCrates,
	ToolIDs:     totemTools,
	ReadStage:   ParseTribalTotemJournal,
	Sustain:     Sustain{FoodIDs: totemFoodIDs, EatAt: 12},
	NoRetaliate: true,
	MaxDeaths:   1,
	Decide:      DecideTribalTotem,
}
