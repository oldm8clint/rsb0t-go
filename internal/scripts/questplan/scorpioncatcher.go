package questplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Scorpion Catcher (content id `scorpcatcher`, %scorpcatcher, not transmitted), from the lane Q
// study (private evidence notes) and the TS def
// (rsb0t-main/src/bot/api/ai/quests/defs/scorpcatcher). Thormac only checks the full cage at the
// end; the one stage gate is Scorpion A's secret wall (stage 2, a Seer's hint). Scorpion B is
// behind the Barbarian Outpost gate (the Barcrawl), C upstairs in the Edgeville Monastery (base
// Prayer 31 to join), A in Taverley Dungeon behind the Jailer's key, Velrak's dusty key and the
// blue-dragon corridor. What each cage holds is its object id.

// ScorpionCatcherName is the quest tab's spelling (component 7373).
const ScorpionCatcherName = "Scorpion Catcher"

// ScorpionCatcherID is the engine's id (the ladder's rung id).
const ScorpionCatcherID = "scorpion"

// Stages.
const (
	ScorpStarted  = 1
	ScorpHint     = 2
	ScorpHint2    = 3
	ScorpComplete = 6
)

// Cages by what they hold.
const (
	CageEmpty = 456
	CageA     = 457
	CageAB    = 458
	CageAC    = 459
	CageB     = 460
	CageBC    = 461
	CageC     = 462
	CageFull  = 463
)

// Ids from content/pack.
const (
	NpcThormac      = 389
	NpcSeer         = 388
	NpcScorpionA    = 385
	NpcScorpionB    = 386
	NpcScorpionC    = 387
	NpcJailer       = 201
	NpcVelrak       = 798
	NpcAbbotLangley = 801
	ObjDustyKey     = 1590
	ObjJailKey      = 1591
	ObjAntiDragon   = 1540
	LocSecretWall   = 2117
	LocDeepGate     = 2623
	LocJailDoor     = 2631
)

// Tiles (TS areas.ts, checked against the study).
var (
	ThormacAnchor   = tile.Tile{X: 2702, Z: 3405, Level: 3}
	SeerAnchor      = tile.Tile{X: 2702, Z: 3475}
	OutpostHut      = tile.Tile{X: 2552, Z: 3571}
	JailDoorStand   = tile.Tile{X: 2931, Z: 9690}
	JailCell        = tile.Tile{X: 2931, Z: 9689}
	VelrakAnchor    = tile.Tile{X: 2931, Z: 9685}
	DeepGateEast    = tile.Tile{X: 2924, Z: 9803}
	DeepGateWest    = tile.Tile{X: 2923, Z: 9803}
	SecretWallStand = tile.Tile{X: 2875, Z: 9799}
	SecretRoomTile  = tile.Tile{X: 2875, Z: 9798}
	ScorpionATile   = tile.Tile{X: 2877, Z: 9796}
	AbbotAnchor     = tile.Tile{X: 3058, Z: 3482}
	MonasteryRoom   = tile.Tile{X: 3058, Z: 3487, Level: 1}
	scorpFoods      = []int{385, 373, 379, 365, 361, 329, 333, 2309}
	scorpAllCages   = []int{CageEmpty, CageA, CageAB, CageAC, CageB, CageBC, CageC, CageFull}
	scorpTools      = append(append([]int{CoinsID, ObjDustyKey, ObjJailKey, ObjAntiDragon, 175, 177, 179}, scorpAllCages...), 562, 556, 1381)
)

// CageHolds reports which scorpions a cage holds.
func CageHolds(cage int) (a, b, c bool) {
	switch cage {
	case CageA:
		return true, false, false
	case CageB:
		return false, true, false
	case CageC:
		return false, false, true
	case CageAB:
		return true, true, false
	case CageAC:
		return true, false, true
	case CageBC:
		return false, true, true
	case CageFull:
		return true, true, true
	}
	return false, false, false
}

// HeldCage is the cage the pack holds, or 0.
func HeldCage(s Snapshot) int {
	for _, id := range scorpAllCages {
		if s.HeldID(id) > 0 {
			return id
		}
	}
	return 0
}

// InDeepDungeon is Taverley's far half west of the dusty-key gate (TS dungeon.ts).
func InDeepDungeon(t tile.Tile) bool {
	if t.Level != 0 || t.Z < 9700 {
		return false
	}
	return t.X <= 2880 || (t.X >= 2890 && t.X <= DeepGateWest.X && t.Z <= 9815)
}

// InVelrakCell is Velrak's cell.
func InVelrakCell(t tile.Tile) bool {
	return t.Level == 0 && t.Z <= JailCell.Z && t.X >= 2928 && t.X <= 2934 && t.Z >= 9683
}

// ParseScorpionCatcherJournal reads scorpcatcher_journal.rs2.
func ParseScorpionCatcherJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	switch {
	case text == "":
		return 0, false
	case strings.Contains(text, "quest complete!"):
		return ScorpComplete, true
	case strings.Contains(text, "i can start this quest"):
		return 0, true
	case strings.Contains(text, "the second kharid scorpion"):
		return ScorpHint2, true
	case strings.Contains(text, "i've spoken to a seer"):
		return ScorpHint, true
	case strings.Contains(text, "i've spoken to thormac"):
		return ScorpStarted, true
	}
	return 0, false
}

// Custom step names (internal/scripts/questmods).
const (
	CustomScorpB     = "scorp-b"
	CustomScorpC     = "scorp-c"
	CustomScorpA     = "scorp-a"
	CustomScorpLeave = "scorp-leave-deep"
	KitScorpA        = "scorp-a-kit"
)

var (
	ThormacStop = NpcStop{Npc: "Thormac", ID: NpcThormac, Anchor: ThormacAnchor, Leash: 6, Prefer: []string{
		"What do you need assistance with?", "So how would I go about catching them then?", "Ok, I will do it then",
		"I've lost my cage.", "I've not caught all the scorpions yet."}}
	SeerStop = NpcStop{Npc: "Seer", ID: NpcSeer, Anchor: SeerAnchor, Leash: 8, Prefer: []string{
		"I need to locate some scorpions.", "Where did you say that scorpion was again?",
		"Your friend Thormac sent me to speak to you.", "Many greetings."}}
)

// scorpAKit is the Taverley kit: the anti-dragon shield worn (the corridor's blue dragons
// breathe for 30-50, 5 with the shield; it comes only from a bank, the mule or Dragon Slayer's
// Duke Horacio), food, and an antipoison when owned.
func scorpAKit() []KitLine {
	return []KitLine{
		{ID: ObjAntiDragon, Name: "Anti-dragon shield", Qty: 1, Worn: true},
		{ID: 385, Name: "Shark", Qty: 10, Optional: true},
		{ID: 379, Name: "Lobster", Qty: 10, Optional: true},
		{ID: 175, Name: "Antipoison(3)", Qty: 1, Optional: true},
	}
}

// DecideScorpionCatcher is the whole quest: Thormac, a Seer, then B, C and A, then Thormac.
func DecideScorpionCatcher(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: ThormacStop}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	keep := append(append([]int(nil), scorpTools...), scorpFoods...)
	here := s.TileKnown
	cage := HeldCage(s)
	if cage == 0 {
		// An unread bank is not an empty one. Thormac's lost-cage line is offered only to an account that owns
		// no cage ANYWHERE (thormac.rs2 counts the bank: inv_totalcat(bank, scorpcage)), so an account whose cage
		// sits in the bank gets "remember to go speak to the Seers" and nothing else, however often it asks.
		// Live 15-16 Oct 2026 every account that parked on "talk to Thormac" (15 of 15 with a bank on file) held
		// one empty cage in the bank and none in the pack (the quest started on an earlier run, the cage banked
		// by a later deposit): the walk to the tower's roof was never the fault. Read the bank first (it is the
		// Seers' bank, on the way to the tower), draw the cage if it is there, and only then ask Thormac.
		if !s.BankKnown {
			return Step{Kind: StepScanBank, Bank: SeersBank, BankSet: true, Reason: "a cage the bank holds is not lost: Thormac gives a new one only to an account with none anywhere"}
		}
		for _, id := range scorpAllCages {
			if n, _ := s.BankedID(id); n > 0 {
				return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Scorpion cage", ID: id, Qty: 1}},
					Bank: SeersBank, BankSet: true, KeepIDs: keep}
			}
		}
		return Step{Kind: StepTalk, Stop: ThormacStop}
	}
	a, b, c := CageHolds(cage)
	if a && here && InDeepDungeon(s.Tile) {
		return Step{Kind: StepCustom, Custom: CustomScorpLeave, Reason: "out through the secret wall and the deep gate"}
	}
	if a && b && c {
		return Step{Kind: StepTalk, Stop: ThormacStop}
	}
	if s.Stage < ScorpHint {
		return Step{Kind: StepTalk, Stop: SeerStop}
	}
	switch {
	case !b:
		return Step{Kind: StepCustom, Custom: CustomScorpB, Reason: "scorpion B in Ivor's hut at the Barbarian Outpost"}
	case !c:
		return Step{Kind: StepCustom, Custom: CustomScorpC, Reason: "scorpion C upstairs in the Edgeville Monastery"}
	}
	if !s.Ran["provision:"+KitScorpA] && !s.Failed["provision:"+KitScorpA] && !(here && InDeepDungeon(s.Tile)) {
		return Step{Kind: StepProvision, KitName: KitScorpA, Kit: scorpAKit(), KeepIDs: keep,
			Bank: FaladorWestBank, BankSet: true, Reason: "the anti-dragon shield and food for the dragon corridor"}
	}
	if s.WornIDs[ObjAntiDragon] == 0 && s.HeldID(ObjAntiDragon) > 0 {
		return Step{Kind: StepEquip, ItemID: ObjAntiDragon, Item: "Anti-dragon shield"}
	}
	// Never the corridor without the shield: a breath is 30-50 without it.
	if s.WornIDs[ObjAntiDragon] == 0 && !(here && InDeepDungeon(s.Tile)) {
		return Step{Kind: StepWait, Reason: "no anti-dragon shield owned (bank or mule): the dragon corridor is not run without one"}
	}
	return Step{Kind: StepCustom, Custom: CustomScorpA, Reason: "scorpion A behind the Taverley secret wall"}
}

// ScorpionCatcherRecord is the quest's identity; content gates it only through the monastery's
// base Prayer 31 and the outpost's Barcrawl.
var ScorpionCatcherRecord = Record{ID: ScorpionCatcherID, Name: ScorpionCatcherName, QuestPoints: 1}

// ScorpionCatcher is the engine module.
var ScorpionCatcher = Module{
	Record: ScorpionCatcherRecord, Bank: SeersBank, BankSet: true, Anchor: ThormacAnchor,
	ToolIDs:   scorpTools,
	ReadStage: ParseScorpionCatcherJournal,
	Sustain:   Sustain{FoodIDs: scorpFoods, EatAt: 20},
	MaxDeaths: 2,
	Decide:    DecideScorpionCatcher,
}
