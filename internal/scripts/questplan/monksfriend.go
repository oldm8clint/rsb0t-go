package questplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Monk's Friend (content id `drunkmonk`, %drunkmonkquest, not transmitted), ported from the TS def (defs/monksfriend.ts) and
// checked against content 289: quests/quest_drunkmonk/{configs,scripts}, areas/area_ardougne_east/scripts/{brother_omad,
// brother_cedric}.rs2, ladders+stairs/scripts/ladders.rs2 (wildymirrorladdertop1), general_use/scripts/water_sources.rs2 and
// the maps (m40_50, m40_150). One quest point, 2,000 Woodcutting experience (stat_advance 20000 at 10x) and 8 law runes.
//
// The story, with the stage each step moves %drunkmonkquest to:
//
//	0   Brother Omad (the monastery south of East Ardougne) cannot sleep: thieves stole Brother Androe's son's blanket.
//	    "Why can't you sleep, what's wrong?" then "Can I help at all?" starts the quest (10) and starts the player's
//	    blanket_ladder timer.
//	10  The blanket lies in a thieves' cave (obj 90, one ground spawn at 2570,9604, back 50 ticks after a pickup). The cave
//	    is reached by a ladder that is not in the map: the timer adds loc 1765 (wildymirrorladdertop1) at 2561,3222 for 200
//	    ticks whenever the player stands within 2 tiles of it and none stands ("A ladder mysteriously appears."). It climbs
//	    down to 2561,9621. The loc is the world's, not the player's: another account's ladder is this one's.
//	    Handed to Omad (no menu): 20.
//	20  "Who's Brother Cedric?" then "Where should I look?": 30.
//	30  Brother Cedric (2614,3259, the forest by his broken cart) is drunk: 40.
//	40  A jug of water, handed over by talking to him, sobers him up (50); "Could you help?" / "Yes, I'd be happy to!" in the
//	    same conversation makes it 60. The journal renders 50 and 60 as one page.
//	60  One log (obj 1511 only, a tree's) handed over by talking to him: 70.
//	70  Omad again ("Hi Omad, Brother Cedric is on his way!"): the party, a script of about 23 ticks of p_delay that ends in
//	    queue(drunkmonk_complete): %drunkmonkquest is 80 only when it has run.
//
// What the content does that a script must plan for:
//   - The party must be waited out. %drunkmonkquest moves only in the closing queue, so a talk that returned when the
//     dialogue closed would find stage 70 and start a second party (CustomMonkParty waits for the tab to turn green).
//   - The blanket is shared: one ground spawn, 50 ticks. The quest holds a fleet-wide slot until the blanket is in hand
//     (MonkPastBlanket), and the custom waits for a respawn instead of walking away.
//   - The thieves are huntmode cowardly (huntrange 1): the Thief (npc 282, level 14, hits 2) hunts accounts of combat 28 and
//     under, the Head Thief (283, level 26, hits 3) accounts of 52 and under, only when one stands next to it. Nothing here is
//     fought (Module.NoRetaliate); Hitpoints 20 is the fleet's floor, not the server's.
//   - Cedric wants a plain log and a plain jug of water. The jug is filled at any Sink (loc 873/874, category watersource);
//     a Well takes only a bucket. The log comes off a tree (an axe in the kit), and a banked log is drawn first.
//
// The item rule for what the quest needs (provision.go): the jug from the bank or the mule (206 are published), then the
// Port Khazard general store; the log from the bank, then a tree beside Cedric's cart with an axe from the bank or, failing
// that, Aemad's.

// MonksFriendName is the quest tab's spelling (component 7369, `questlist:drunkmonk`).
const MonksFriendName = "Monk's Friend"

// MonksFriendID is the engine's id.
const MonksFriendID = "drunkmonk"

// The %drunkmonkquest stages (quest_drunkmonk.constant and general/configs/quest.constant). The journal cannot tell 50 (given
// water) from 60 (fixing the cart): MonkMendingCart stands for both.
const (
	MonkNotStarted       = 0
	MonkSpokeOmad        = 10
	MonkRetrievedBlanket = 20
	MonkLookingCedric    = 30
	MonkFindingWater     = 40
	MonkMendingCart      = 50
	MonkFixedCart        = 70
	MonkComplete         = 80
)

// Ids from content/pack (checked against the production config by TestMonkCogIdsMatchTheProductionConfig). The jug of water, 1937,
// is ObjJugOfWater of princeali.go: one name for the one object in the package.
const (
	NpcBrotherOmad       = 279
	NpcBrotherCedric     = 280
	NpcKhazardShopkeeper = 555 // [khazard_shopkeeper]: Port Khazard's general store, two keepers, one shop
	ObjChildsBlanket     = 90  // [childs_blanket], untradeable
	ObjJug               = 1935
	LocMonkRingLadder    = 1765 // [wildymirrorladdertop1], op1 Climb-down: the ladder the timer adds
	LocCaveLadderUp      = 1755 // [ladder_from_cellar], op1 Climb-up (the cave's way out, and the cellar's)
	LocSink              = 873  // [sink], category watersource
	LocSink2             = 874  // [sink2], the same
)

// Tiles from content maps and the loc index.
var (
	// OmadAnchor and CedricAnchor are the two monks' spawns (m40_50).
	OmadAnchor   = tile.Tile{X: 2604, Z: 3209}
	CedricAnchor = tile.Tile{X: 2614, Z: 3259}
	// MonkRingStand is the tile beside the ladder's, standing on which the timer adds it (distance 1 of 2561,3222).
	MonkRingStand = tile.Tile{X: 2562, Z: 3222}
	// MonkRingLadder is where the timer adds the ladder (^blanket_ladder_coord, 0_40_50_1_22).
	MonkRingLadder = tile.Tile{X: 2561, Z: 3222}
	// MonkCaveFoot is where the ladder lands (0_40_150_1_21) and MonkCaveLadder the cave's way up, south of which it stands.
	MonkCaveFoot   = tile.Tile{X: 2561, Z: 9621}
	MonkCaveLadder = tile.Tile{X: 2561, Z: 9622}
	// MonkBlanketSpawn is the blanket's ground spawn (m40_150 "0 10 4: 90").
	MonkBlanketSpawn = tile.Tile{X: 2570, Z: 9604}
	// MonkTree is a regular tree beside Cedric's cart (loc 1276 at 2613,3252); the chop-logs source works the trees round it.
	MonkTree = tile.Tile{X: 2613, Z: 3252}
	// ArdougneSouthBank is the bank the quest uses (the "East Ardougne south" entry of Banks).
	ArdougneSouthBank = tile.Tile{X: 2655, Z: 3286}
	// KhazardGeneralStore is Port Khazard's general store (khazardshop: jug, bucket, pot and the rest at 2 gp), the keeper at
	// m41_49 "0 17 35". Its other keeper (2655,3152) opens the same shop and is the stand ScarabSwarm's rope trips use.
	KhazardGeneralStore = ShopRef{Npc: "Shop keeper", NpcID: NpcKhazardShopkeeper, Anchor: tile.Tile{X: 2641, Z: 3171}}
)

// MonkSinks are the sinks a jug is filled at, nearest the East Ardougne south bank first: the one in the town beside it, then the
// three round the monastery and Port Khazard's guard house (all `Sink` locs of the map between x 2555 and 2660, z 3170 and 3300).
var MonkSinks = []tile.Tile{{X: 2658, Z: 3273}, {X: 2610, Z: 3195}, {X: 2608, Z: 3187}, {X: 2591, Z: 3188}}

// InMonkCave reports a tile in the thieves' cave under the ring of stones. It is a pocket for every walk: the graph's edge for
// the cave's ladder stands north of it, on a tile the cave does not reach, and the way in is a loc no map has. The rectangle
// is the 151 tiles the planner reaches from the ladder's foot (x 2561-2579, z 9602-9622), doors included; the Clock Tower's
// cellar begins at z 9625 and its ogre cave at x 2583, so neither is in it.
func InMonkCave(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2561 && t.X <= 2579 && t.Z >= 9602 && t.Z <= 9622
}

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomMonkBlanket = "monk-blanket"
	CustomMonkJug     = "monk-jug"
	CustomMonkParty   = "monk-party"

	KitMonkJug  = "monk-jug"
	KitMonkLogs = "monk-logs"
)

// monkFoodIDs are the foods the quest eats if it carries any (the eat loop's floor bite takes them whatever the module names).
var monkFoodIDs = []int{385, 373, 379, 365, 361, 329, 1891, 1893, 1895, 333, 2309, 2142}

// monkKeep is what every bank visit of the quest leaves in the pack: its own items, any axe, the coins for a shop and food.
var monkKeep = append(append([]int{ObjChildsBlanket, ObjJug, ObjJugOfWater, ObjLogs, CoinsID}, Axes...), monkFoodIDs...)

// MonkKeepIDs returns the keep list (a copy).
func MonkKeepIDs() []int { return append([]int(nil), monkKeep...) }

// The stops. The talks with no menu carry a placeholder preference (a talk step must name one): the option text of an
// earlier stage, which an unexpected menu would simply not match.
var (
	// OmadStartStop starts the quest: two menus (omad_whats_wrong, omad_why).
	OmadStartStop = NpcStop{Npc: "Brother Omad", ID: NpcBrotherOmad, Anchor: OmadAnchor, Leash: 6,
		Prefer: []string{"Why can't you sleep, what's wrong?", "Can I help at all?"}}
	// OmadBlanketStop takes the blanket (omad_have_blanket, no menu once it is held).
	OmadBlanketStop = NpcStop{Npc: "Brother Omad", ID: NpcBrotherOmad, Anchor: OmadAnchor, Leash: 6,
		Prefer: []string{"Can I help at all?"}}
	// OmadCedricStop sends the player after Cedric: the party talk's first menu, then the three-way one (omad_organize_party).
	OmadCedricStop = NpcStop{Npc: "Brother Omad", ID: NpcBrotherOmad, Anchor: OmadAnchor, Leash: 6,
		Prefer: []string{"Who's Brother Cedric?", "Where should I look?"}}
	// OmadPartyStop is the last talk (omad_party): no menu, 8 law runes in a message box, then the party.
	OmadPartyStop = NpcStop{Npc: "Brother Omad", ID: NpcBrotherOmad, Anchor: OmadAnchor, Leash: 6,
		Prefer: []string{"Can I help at all?"}}
	// CedricStop is every talk with Cedric (cedric_okay, cedric_need_water, cedric_fix_cart, cedric_get_wood); only the cart
	// has a menu. His wander range is the engine's default 5, so the leash is wide.
	CedricStop = NpcStop{Npc: "Brother Cedric", ID: NpcBrotherCedric, Anchor: CedricAnchor, Leash: 8,
		Prefer: []string{"Yes, I'd be happy to!"}}
)

// monkJournalNeedles are ParseMonksFriendJournal's phrases, newest first, written against NormaliseLines
// (drunkmonk_journal.rs2; finished lines are struck through but kept, so every page from stage 20 repeats the blanket line).
var monkJournalNeedles = []struct {
	stage  int
	needle string
}{
	{MonkComplete, "quest complete"},
	{MonkFixedCart, "he is on the way"},
	{MonkMendingCart, "he needs some wood for his cart"},
	{MonkFindingWater, "i need to take him a jug of water"},
	{MonkLookingCedric, "i agreed to find brother cedric"},
	{MonkRetrievedBlanket, "i found the secret cave and gave back the blanket"},
	{MonkSpokeOmad, "i need to find a secret cave"},
	{MonkNotStarted, "i can start this quest by speaking to brother omad"},
}

// ParseMonksFriendJournal reads the stage from the rendered journal; ok is false for an empty or unrecognised journal, which is
// unknown, never stage 0. Stage 50 stands for 50 and 60.
func ParseMonksFriendJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range monkJournalNeedles {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// MonkPastBlanket reports the account past the quest's one shared step, the blanket's single ground spawn: the blanket is in
// hand or handed in, or the quest is done. The fleet-wide slot is held until then.
func MonkPastBlanket(s Snapshot) bool {
	if s.Journal == StatusComplete || s.HeldID(ObjChildsBlanket) > 0 {
		return true
	}
	return s.Journal == StatusInProgress && s.StageKnown && s.Stage >= MonkRetrievedBlanket
}

// monkJugKit is the jug of water, or the jug it is filled from. A jug of water in the bank is drawn first; the empty jug is
// wanted only while no jug of water is held, and comes from the bank, the mule, then Port Khazard's store.
func monkJugKit() []KitLine {
	return []KitLine{
		{ID: ObjJugOfWater, Name: "Jug of water", Qty: 1, Optional: true},
		{ID: ObjJug, Name: "Jug", Qty: 1, OnlyIfShort: ObjJugOfWater, Sources: []Source{
			{Kind: SourceShop, Shop: KhazardGeneralStore, Price: 2, Note: "Port Khazard's general store keeps 2 jugs"},
		}},
	}
}

// monkLogsKit is the log for Cedric's cart: the best axe the bank holds (else an iron one from Aemad) and a log from the bank
// or from a tree beside the cart.
func monkLogsKit(s Snapshot) []KitLine {
	return []KitLine{
		AxeLine(s),
		{ID: ObjLogs, Name: "Logs", Qty: 1, Sources: []Source{{
			Kind: SourceSkill, Skill: "woodcutting", Method: MethodChopLogs, Tile: MonkTree,
			Note: "the trees beside Brother Cedric's cart, with the kit's axe",
		}}},
	}
}

// monkWaterStep makes the pack hold a jug of water for a Cedric at stage 40: fill the jug in the pack at a sink, else provision
// one (the bank or the mule, then the store), and park when that failed.
func monkWaterStep(s Snapshot) Step {
	if s.HeldID(ObjJug) > 0 {
		return Step{Kind: StepCustom, Custom: CustomMonkJug, Reason: "fill the jug at a sink"}
	}
	if s.Failed["provision:"+KitMonkJug] {
		return Step{Kind: StepWait, Reason: "no jug of water from the bank, the mule or Port Khazard's store", ParkFor: 30 * time.Minute}
	}
	return Step{Kind: StepProvision, KitName: KitMonkJug, Kit: monkJugKit(), KeepIDs: monkKeep, Bank: ArdougneSouthBank, BankSet: true,
		Reason: "a jug of water (or the jug to fill) for Brother Cedric"}
}

// monkLogsStep makes the pack hold a log for Cedric at stage 50 or 60.
func monkLogsStep(s Snapshot) Step {
	if !AxeHeld(s) && !s.BankKnown {
		return Step{Kind: StepScanBank, Bank: ArdougneSouthBank, BankSet: true, Reason: "an axe, or a log, for Cedric's cart"}
	}
	if s.Failed["provision:"+KitMonkLogs] {
		return Step{Kind: StepWait, Reason: "no log or axe from the bank, the mule, a tree or Aemad's shop", ParkFor: 30 * time.Minute}
	}
	return Step{Kind: StepProvision, KitName: KitMonkLogs, Kit: monkLogsKit(s), KeepIDs: monkKeep, Bank: ArdougneSouthBank, BankSet: true,
		Reason: "a log for Brother Cedric's cart"}
}

// DecideMonksFriend is the whole quest, read from the journal stage and the pack.
func DecideMonksFriend(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: OmadStartStop}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	// At stage 10 the journal reads "find the secret cave" with or without the blanket, so the pack is the only evidence the
	// cave was robbed.
	if s.Stage <= MonkSpokeOmad && held(ObjChildsBlanket) {
		return Step{Kind: StepTalk, Stop: OmadBlanketStop}
	}
	// The blanket, the jug and the log each take a free slot (and Omad's runes one).
	room := func(next Step) Step {
		if s.FreeSlots < 2 {
			return Step{Kind: StepDeposit, KeepIDs: monkKeep, Bank: ArdougneSouthBank, BankSet: true, Reason: "room for the quest's items"}
		}
		return next
	}
	switch s.Stage {
	case MonkNotStarted:
		return Step{Kind: StepTalk, Stop: OmadStartStop}
	case MonkSpokeOmad:
		return room(Step{Kind: StepCustom, Custom: CustomMonkBlanket, Reason: "the child's blanket from the thieves' cave"})
	case MonkRetrievedBlanket:
		return Step{Kind: StepTalk, Stop: OmadCedricStop}
	case MonkLookingCedric:
		return Step{Kind: StepTalk, Stop: CedricStop}
	case MonkFindingWater:
		if held(ObjJugOfWater) {
			return Step{Kind: StepTalk, Stop: CedricStop}
		}
		return room(monkWaterStep(s))
	case MonkMendingCart:
		if held(ObjLogs) {
			return Step{Kind: StepTalk, Stop: CedricStop}
		}
		return room(monkLogsStep(s))
	case MonkFixedCart:
		return Step{Kind: StepCustom, Custom: CustomMonkParty, Reason: "tell Omad Cedric is on his way, then wait out the party"}
	}
	return Step{Kind: StepWait, Reason: "unrecognised Monk's Friend stage"}
}

// MonksFriendRecord is the quest's identity. The server gates nothing; Hitpoints 20 is the fleet's own floor for the cave's
// thieves (a combat 3 account is hunted by the Thief and the Head Thief alike).
var MonksFriendRecord = Record{ID: MonksFriendID, Name: MonksFriendName, QuestPoints: 1,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "hitpoints", Level: 20}}}}

// MonksFriend is the engine module. It never fights (NoRetaliate): the cave's thieves hit for 2 and 3 and are walked away
// from. Two deaths park it for a day.
var MonksFriend = Module{
	Record: MonksFriendRecord, Bank: ArdougneSouthBank, BankSet: true, Anchor: OmadAnchor,
	ToolIDs:       monkKeep,
	ReadStage:     ParseMonksFriendJournal,
	NoRetaliate:   true,
	MaxDeaths:     2,
	ExclusiveDone: MonkPastBlanket,
	Decide:        DecideMonksFriend,
}
