package questplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// The Restless Ghost (content id `priest`, %prieststart varp 107, not transmitted), ported from
// defs/restlessghost.ts and corrected against content 289 by the lane Q study
// (private evidence notes):
//
//   - the skull lies at 3120,9567 (the TS SKULL_TILE 3120,9565 is the skeleton's spawn);
//   - the coffin stand is 3248,3193 (the TS COFFIN_STAND is inside the coffin's footprint);
//   - the ghost is spawned by operating the coffin and lives 100 ticks, so the ghost talk is a
//     custom step that opens (or searches) the coffin first and never Closes it, because the
//     loc change is global and would shut it on another account mid-use;
//   - the skull pickup spawns a level-13 skeleton that attacks the taker only, max hit 2,
//     leashed 8 tiles from its spawn (it cannot follow through the basement door).
//
// The Ghostspeak amulet this quest hands out must survive into Nature Spirit, so it is a tool:
// no deposit ever banks it mid-quest, and Father Urhney re-issues one to an account that owns
// none anywhere ("I've lost the amulet.").

// RestlessGhostName is the quest tab's spelling (internal/quests table: component 7337).
const RestlessGhostName = "The Restless Ghost"

// RestlessGhostID is the engine's id for the quest.
const RestlessGhostID = "ghost"

// Ids from content/pack.
const (
	NpcAereck        = 456
	NpcUrhney        = 458
	NpcRestlessGhost = 457
	NpcSkullSkeleton = 459
	ObjGhostspeak    = 552
	ObjGhostSkull    = 553
	LocCoffinShut    = 2145
	LocCoffinOpen    = 2146
)

// Tiles from content maps (m50_50, m50_49, m48_149).
var (
	GhostCoffinTile   = tile.Tile{X: 3249, Z: 3192}
	GhostCoffinStand  = tile.Tile{X: 3248, Z: 3193}
	GhostSpawnTile    = tile.Tile{X: 3249, Z: 3194}
	GhostSkullTile    = tile.Tile{X: 3120, Z: 9567}
	GhostSkeletonTile = tile.Tile{X: 3120, Z: 9565}
	// DraynorBank is the TS def's bank, on the Lumbridge to Wizards' Tower route.
	DraynorBank = tile.Tile{X: 3093, Z: 3243}
)

// Custom step names, registered by the live half (internal/scripts/questmods).
const (
	CustomGhostTalk   = "ghost-talk"
	CustomGhostSkull  = "ghost-skull"
	CustomGhostReturn = "ghost-return-skull"
)

// Stages, from quest_priest.constant.
const (
	GhostNotStarted = 0
	GhostStarted    = 1
	GhostSpokeUrhny = 2
	GhostSpokeGhost = 3
	GhostHasSkull   = 4
	GhostComplete   = 5
)

var (
	// AereckStop starts the quest: "I'm looking for a quest!" (father_aereck.rs2:45).
	AereckStop = NpcStop{Npc: "Father Aereck", ID: NpcAereck, Anchor: tile.Tile{X: 3244, Z: 3206}, Leash: 6,
		Prefer: []string{"I'm looking for a quest!"}}
	// UrhneyStop hands over the amulet at stage 1, and a spare at stage 2+ when none is owned.
	UrhneyStop = NpcStop{Npc: "Father Urhney", ID: NpcUrhney, Anchor: tile.Tile{X: 3235, Z: 3153}, Leash: 6,
		Prefer: []string{"Father Aereck sent me to talk to you.", "He's got a ghost haunting his graveyard.", "I've lost the amulet."}}
	// GhostStop is the ghost, once the coffin has spawned it.
	GhostStop = NpcStop{Npc: "Restless ghost", ID: NpcRestlessGhost, Anchor: GhostSpawnTile, Leash: 4,
		Prefer: []string{"Yep, now tell me what the problem is.", "Yes, ok. Do you know why you're a ghost?"}}
)

// ghostJournalNeedles are ParseRestlessGhostJournal's phrases, newest first, written against
// NormaliseLines (priest_journal.rs2; completed lines are struck through but kept, so only the
// order makes the answer right).
var ghostJournalNeedles = []struct {
	stage  int
	needle string
}{
	{GhostComplete, "quest complete"},
	{GhostHasSkull, "i should take the skull back to the ghost"},
	{GhostSpokeGhost, "i should go and search the wizard's tower"},
	{GhostSpokeUrhny, "i should talk to the ghost to find out why"},
	{GhostStarted, "i should find father urhney"},
	{GhostNotStarted, "i can start this quest by speaking to father aereck"},
}

// ParseRestlessGhostJournal reads the stage from the rendered journal. ok is false for an empty
// or unrecognised journal, which is unknown, never stage 0.
func ParseRestlessGhostJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range ghostJournalNeedles {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// DecideRestlessGhost is the whole quest, restartable from any stage: the journal gives the
// stage, the pack/worn/bank give the items.
func DecideRestlessGhost(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: AereckStop}
	}
	if s.HeldID(ObjGhostSkull) > 0 {
		return Step{Kind: StepCustom, Custom: CustomGhostReturn, Reason: "the skull goes in the open coffin"}
	}
	if n, known := s.BankedID(ObjGhostSkull); known && n > 0 {
		return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Skull", ID: ObjGhostSkull, Qty: 1}},
			Bank: DraynorBank, BankSet: true}
	}
	worn := s.WornIDs[ObjGhostspeak] > 0
	if !worn {
		switch {
		case s.HeldID(ObjGhostspeak) > 0:
			return Step{Kind: StepEquip, ItemID: ObjGhostspeak, Item: "Ghostspeak amulet"}
		case !s.BankKnown:
			return Step{Kind: StepScanBank, Bank: DraynorBank, BankSet: true}
		}
		if n, _ := s.BankedID(ObjGhostspeak); n > 0 {
			return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Ghostspeak amulet", ID: ObjGhostspeak, Qty: 1}},
				Bank: DraynorBank, BankSet: true}
		}
		if s.FreeSlots < 1 {
			return Step{Kind: StepDeposit, KeepIDs: []int{ObjGhostspeak, ObjGhostSkull, CoinsID}, Bank: DraynorBank, BankSet: true}
		}
		// Stage 1, or a lost amulet at stage 2+: Urhney answers both.
		return Step{Kind: StepTalk, Stop: UrhneyStop}
	}
	if !s.StageKnown || s.Stage <= GhostSpokeUrhny {
		return Step{Kind: StepCustom, Custom: CustomGhostTalk, Reason: "open the coffin and talk to the ghost"}
	}
	// Stage 3, or 4 with the skull lost: take (or re-take) the skull.
	if s.FreeSlots < 1 {
		return Step{Kind: StepDeposit, KeepIDs: []int{ObjGhostspeak, ObjGhostSkull, CoinsID}, Bank: DraynorBank, BankSet: true}
	}
	return Step{Kind: StepCustom, Custom: CustomGhostSkull, Reason: "the skull in the Wizards' Tower basement"}
}

// RestlessGhostRecord is the quest's identity. No server gates (study: requirements).
var RestlessGhostRecord = Record{ID: RestlessGhostID, Name: RestlessGhostName, QuestPoints: 1}

// RestlessGhost is the engine module.
var RestlessGhost = Module{
	Record:  RestlessGhostRecord,
	Hops:    WizardHops,
	Bank:    DraynorBank,
	BankSet: true,
	Anchor:  DraynorBank,
	ToolIDs: []int{ObjGhostspeak, ObjGhostSkull},
	ReadStage: func(lines []string) (int, bool) {
		return ParseRestlessGhostJournal(lines)
	},
	// Whatever food the account carries is eaten on the walk out of the basement (the module had no Sustain, so
	// nothing ever ate), and one death parks the quest for a day: two accounts died to the skull's skeleton on 6 Oct 2026.
	Sustain:       Sustain{FoodIDs: ghostFoods, EatAt: 12},
	MaxDeaths:     1,
	Decide:        DecideRestlessGhost,
	ExclusiveDone: GhostPastSkull,
}

// GhostPastSkull reports the account past the quest's shared step, the skull's one ground spawn: the
// skull is in hand, or the journal is at the skull's return or complete.
func GhostPastSkull(s Snapshot) bool {
	if s.Journal == StatusComplete || s.HeldID(ObjGhostSkull) > 0 {
		return true
	}
	return s.Journal == StatusInProgress && s.StageKnown && s.Stage >= GhostHasSkull
}

var ghostFoods = []int{385, 373, 379, 365, 361, 329, 333, 2309, 1971, 1891, 1893, 1895}
