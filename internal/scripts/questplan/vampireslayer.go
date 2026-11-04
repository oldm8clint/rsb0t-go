package questplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Vampire Slayer (content id `vampire`, %vampire varp 178, not transmitted), from the lane Q study
// (private evidence notes). The kill is gated by the SERVER on the
// killing blow (count_draynor.rs2:49-80): without a Stake in the pack the Count heals to full,
// and without a Hammer in the pack as well he heals too. The stake is untradeable and only Dr
// Harlow gives it, at stage 2, for one beer; garlic (optional, weakens him) comes free and
// unlimited from Morgan's upstairs cupboard.
//
// The quest is decided from items rather than the journal wherever it can be: a held stake means
// stage 2 is done; no stake means Harlow (who at stage 1 moves the quest to 2 and at stage 2
// swaps a beer for a stake), so the same talk step is right at both stages.

// VampireSlayerName is the quest tab's spelling (component 7347).
const VampireSlayerName = "Vampire Slayer"

// VampireSlayerID is the engine's id.
const VampireSlayerID = "vampire"

// Ids from content/pack.
const (
	NpcMorgan        = 755
	NpcHarlow        = 756
	NpcCountDraynor  = 757
	NpcJollyBoarBar  = 731
	NpcVarrockGenKpr = 522
	ObjStake         = 1549
	ObjHammer        = 2347
	ObjGarlic        = 1550
	ObjBeer          = 1917
	LocManorDoorL    = 134
	LocCryptStairs   = 2616
	LocVampCoffin    = 2614
	LocGarlicCupShut = 2612
)

// Tiles from content maps (m48_51, m51_54, m50_53, m48_152).
var (
	MorganAnchor    = tile.Tile{X: 3098, Z: 3268}
	HarlowAnchor    = tile.Tile{X: 3277, Z: 3492}
	JollyBoarBar    = tile.Tile{X: 3277, Z: 3488}
	ManorDoorSouth  = tile.Tile{X: 3108, Z: 3352}
	CryptStairs     = tile.Tile{X: 3115, Z: 3357}
	VampCoffinTile  = tile.Tile{X: 3077, Z: 9775}
	GarlicCupboard  = tile.Tile{X: 3096, Z: 3268, Level: 1}
	VarrockGenStore = ShopRef{Npc: "Shop keeper", NpcID: NpcVarrockGenKpr, Anchor: tile.Tile{X: 3218, Z: 3415}}
	VarrockEastBank = tile.Tile{X: 3253, Z: 3420}
	vampireFoodIDs  = []int{385, 373, 379, 365, 361, 329, 1891, 1893, 1895, 333, 2309, 2142}
	vampireKeepIDs  = []int{ObjStake, ObjHammer, ObjGarlic, ObjBeer, CoinsID}
)

// Custom step names (internal/scripts/questmods).
const (
	CustomVampireGarlic = "vampire-garlic"
	CustomVampireCount  = "vampire-count"
	KitVampire          = "vampire"
	KitVampireFood      = "vampire-food"
)

var (
	// MorganStop starts the quest (morgan.rs2:16; options 2 and 3 both start it).
	MorganStop = NpcStop{Npc: "Morgan", ID: NpcMorgan, Anchor: MorganAnchor, Leash: 6,
		Prefer: []string{"Ok, I'm up for an adventure."}}
	// HarlowStop moves stage 1 to 2 ("Morgan needs your help") and swaps a beer for the stake at
	// stage 2 ("Ok mate." only when a stake is already held).
	HarlowStop = NpcStop{Npc: "Dr Harlow", ID: NpcHarlow, Anchor: HarlowAnchor, Leash: 6,
		Prefer: []string{"Morgan needs your help", "Ok mate."}}
	// JollyBoarBarStop sells a beer for 2 coins (bartender.rs2).
	JollyBoarBarStop = NpcStop{Npc: "Bartender", ID: NpcJollyBoarBar, Anchor: JollyBoarBar, Leash: 6,
		Prefer: []string{"I'll have a beer please."}}
)

// ParseVampireSlayerJournal reads vampire_journal.rs2's rendered stage.
func ParseVampireSlayerJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	switch {
	case text == "":
		return 0, false
	case strings.Contains(text, "quest complete"):
		return 3, true
	case strings.Contains(text, "i have spoken to dr harlow"):
		return 2, true
	case strings.Contains(text, "i need to speak to dr harlow"):
		return 1, true
	case strings.Contains(text, "i can start this quest by speaking to morgan"):
		return 0, true
	}
	return 0, false
}

func vampireKit() []KitLine {
	return []KitLine{
		{ID: ObjHammer, Name: "Hammer", Qty: 1, Sources: []Source{{Kind: SourceShop, Shop: VarrockGenStore, Price: 1}}},
		{ID: ObjGarlic, Name: "Garlic", Qty: 1, Optional: true},
		{ID: ObjBeer, Name: "Beer", Qty: 1, Optional: true},
		{ID: CoinsID, Name: "Coins", Qty: 10, Stackable: true},
		{ID: 385, Name: "Shark", Qty: 6, Optional: true},
	}
}

// vampireBeerFloat is the coins drawn from the bank for the beer (it costs 2).
const vampireBeerFloat = 10

// vampireCoinsStep is the account with fewer than the 2 coins a beer costs: it draws coins from
// the bank before it parks. Live 30 Sep (acct18) a Stage 5 account reached stage 2 with none and
// parked on "no coins for Dr Harlow's beer" with coins in the bank (the Stage 5 trainer banks all
// but its own float, and the kit's coin line only runs once a run). The bank is read first when
// this run has not read it (a view from an earlier run is not trusted: the trainer banks between
// runs); the quest waits only when the pack and the bank are both without.
func vampireCoinsStep(s Snapshot) Step {
	if !s.BankKnown {
		return Step{Kind: StepScanBank, Bank: VarrockEastBank, BankSet: true}
	}
	if n, _ := s.BankedID(CoinsID); n > 0 {
		return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Coins", ID: CoinsID, Qty: minInt(n, vampireBeerFloat)}},
			Bank: VarrockEastBank, BankSet: true, KeepIDs: vampireKeepIDs}
	}
	return Step{Kind: StepWait, Reason: "no coins for Dr Harlow's beer (none in the pack or the bank)"}
}

// DecideVampireSlayer is the whole quest.
func DecideVampireSlayer(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: MorganStop}
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	if !s.Ran["provision:"+KitVampire] && !s.Failed["provision:"+KitVampire] {
		return Step{Kind: StepProvision, KitName: KitVampire, Kit: vampireKit(), KeepIDs: append(vampireKeepIDs, vampireFoodIDs...),
			Bank: VarrockEastBank, BankSet: true, Reason: "hammer, coins for a beer, and whatever garlic and food the bank or mule has"}
	}
	if !held(ObjStake) {
		if !held(ObjBeer) && (!s.StageKnown || s.Stage >= 2) {
			if s.HeldID(CoinsID) < 2 {
				return vampireCoinsStep(s)
			}
			return Step{Kind: StepTalk, Stop: JollyBoarBarStop, UntilItemID: ObjBeer}
		}
		return Step{Kind: StepTalk, Stop: HarlowStop, UntilItemID: ObjStake}
	}
	if !held(ObjHammer) {
		return ShopItemStep(s, "vampire-hammer", ObjHammer, "Hammer", 1, VarrockGenStore, 1, vampireKeepIDs)
	}
	if !held(ObjGarlic) && !s.Failed[CustomVampireGarlic] {
		return Step{Kind: StepCustom, Custom: CustomVampireGarlic, Reason: "a clove from Morgan's cupboard weakens the Count"}
	}
	return Step{Kind: StepCustom, Custom: CustomVampireCount, Reason: "stake and hammer in the pack"}
}

// VampireSlayerRecord is the quest's identity. The server gates nothing; the skill gates are the
// fleet's own safety floor for the Count Draynor fight (owner rule: no deaths). Live 27 Sep 10:01 a
// Stage 5 account at Defence 1, Hitpoints 29 died to the Count for the fourth time; the Stage 5
// trainer raises both, and the ladder runs this quest once they are met.
var VampireSlayerRecord = Record{ID: VampireSlayerID, Name: VampireSlayerName, QuestPoints: 3,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "defence", Level: 20}, {Skill: "hitpoints", Level: 30}}}}

// VampireSlayer is the engine module.
var VampireSlayer = Module{
	Record: VampireSlayerRecord, Bank: VarrockEastBank, BankSet: true, Anchor: MorganAnchor,
	ToolIDs:   vampireKeepIDs,
	ReadStage: ParseVampireSlayerJournal,
	Sustain:   Sustain{FoodIDs: vampireFoodIDs, EatAt: 12},
	MaxDeaths: 3,
	Decide:    DecideVampireSlayer,
}
