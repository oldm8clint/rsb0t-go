package questplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Romeo & Juliet (content id `romeo`, %rjquest, not transmitted), ported from defs/romeojuliet.ts and checked against
// content 289 (quests/quest_romeojuliet and areas/area_varrock/scripts/{romeo,juliet,father_lawrence,apothecary}.rs2).
//
// Seven talks and one berry. Romeo (stage 0, "Can I help find her for you?") sends the account to Juliet upstairs in
// the house west of Varrock (stage 10), who gives it a message (an untradeable item; stage 20); Romeo takes the message
// (stage 30); Father Lawrence, in the church north-east of the square, sends it to the Apothecary (stage 40); the
// Apothecary says he needs cadava berries (stage 50) and, given one, makes the cadava potion; Juliet takes the potion
// (stage 60) and Romeo, told, completes the quest (stage 100): 5 quest points, no experience.
//
// What the content does that a script must plan for:
//   - Juliet checks the pack AND THE BANK before she issues a message again ("juliet_deliver_letter"): an account whose
//     trainer banked the message between two ladder passes is told to hurry, and given nothing. So stage 20 looks in
//     the bank first (the message kit), exactly as Murder Mystery's evidence kit does.
//   - Cadava berries are three ground spawns, one each, in the patch south-east of Varrock beside the redberries
//     (content/maps/m51_52, obj 753) - the TS def's "three Cadava berry ground spawns" - and an imp drops them 4 in 128.
//     They are not poisonous to take in this content. The Apothecary makes the potion only for an account that holds no
//     potion already ("apothecary_make_cadava").
//   - The potion is untradeable and Juliet takes it from the pack only.
// A banked potion, then, is withdrawn before another berry is looked for, as the TS def does.

// RomeoJulietName is the quest tab's spelling (component 7343).
const RomeoJulietName = "Romeo & Juliet"

// RomeoJulietID is the engine's id.
const RomeoJulietID = "romeo"

// Ids from content/pack.
const (
	NpcRomeo          = 639
	NpcJuliet         = 637
	NpcFatherLawrence = 640
	NpcApothecary     = 638
	ObjCadavaBerries  = 753
	ObjJulietMessage  = 755
	ObjCadavaPotion   = 756
)

// Stages, from quest_romeojuliet.constant and general/configs/quest.constant (^romeojuliet_*), %rjquest.
const (
	RomeoNotStarted      = 0
	RomeoSpokeRomeo      = 10
	RomeoSpokeJuliet     = 20
	RomeoPassedMessage   = 30
	RomeoSpokeFather     = 40
	RomeoSpokeApothecary = 50
	RomeoJulietCrypt     = 60
	RomeoComplete        = 100
)

// Tiles from content maps (npc and obj spawns).
var (
	RomeoAnchor     = tile.Tile{X: 3211, Z: 3425}
	JulietAnchor    = tile.Tile{X: 3158, Z: 3425, Level: 1}
	LawrenceAnchor  = tile.Tile{X: 3254, Z: 3475}
	ApothecaryHouse = tile.Tile{X: 3195, Z: 3404}
	// VarrockWestBank is the bank by Juliet's house and Romeo.
	VarrockWestBank = tile.Tile{X: 3185, Z: 3436}
	// CadavaSpawns are the three cadava berry ground spawns (m51_52).
	CadavaSpawns = []tile.Tile{{X: 3266, Z: 3361}, {X: 3273, Z: 3375}, {X: 3277, Z: 3370}}
)

// Kit names.
const (
	KitRomeoMessage = "romeo-message"
	KitRomeoPotion  = "romeo-potion"
)

var (
	// RomeoStartStop starts the quest at the first of its three menus; at stage 30 and 60 Romeo has no menu.
	RomeoStartStop = NpcStop{Npc: "Romeo", ID: NpcRomeo, Anchor: RomeoAnchor, Leash: 8,
		Prefer: []string{"Can I help find her for you?"}}
	// JulietStop gives the message (stage 10) and the replacement (stage 20), and takes the potion (stage 50): no menu in
	// any of them. Her first-meeting menu is the only one she has, so its answer is the placeholder.
	JulietStop = NpcStop{Npc: "Juliet", ID: NpcJuliet, Anchor: JulietAnchor, Leash: 6,
		Prefer: []string{"Certainly, I will do so straight away!", "I guess I could find him."}}
	// LawrenceStop is Father Lawrence at stage 30: no menu (his menu is the pre-quest one).
	LawrenceStop = NpcStop{Npc: "Father Lawrence", ID: NpcFatherLawrence, Anchor: LawrenceAnchor, Leash: 6,
		Prefer: []string{"I am always looking for a quest."}}
	// ApothecaryStop is the Apothecary at stage 40 and 50: no menu (his menu is the standard one).
	ApothecaryStop = NpcStop{Npc: "Apothecary", ID: NpcApothecary, Anchor: ApothecaryHouse, Leash: 6,
		Prefer: []string{"Can you make a strength potion?"}}
)

// romeoJournalNeedles are ParseRomeoJulietJournal's phrases, newest first, written against NormaliseLines
// (romeojuliet_journal.rs2; finished lines are struck through but kept). The 50 stage is the struck apothecary line
// alone: its closing line varies with what the pack holds (the potion, the berries, neither). Content branch 274 spelt
// the item "cadaver"; none of the needles names it.
var romeoJournalNeedles = []struct {
	stage  int
	needle string
}{
	{RomeoComplete, "quest complete"},
	{RomeoJulietCrypt, "i have to find romeo and tell him"},
	{RomeoSpokeApothecary, "and he told me to bring him some"},
	{RomeoSpokeFather, "i need to find the apothecary"},
	{RomeoPassedMessage, "i should find father lawrence"},
	{RomeoSpokeJuliet, "i should take the message"},
	{RomeoSpokeRomeo, "i should go and speak to juliet"},
	{RomeoNotStarted, "i can start this quest by talking to romeo"},
}

// ParseRomeoJulietJournal reads the stage from the rendered journal. ok is false for an empty or unrecognised
// journal, which is unknown, never stage 0.
func ParseRomeoJulietJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range romeoJournalNeedles {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// romeoTools are the quest's own items, kept by every bank visit of the quest: the message, the berries and the potion.
var romeoTools = []int{CoinsID, ObjJulietMessage, ObjCadavaBerries, ObjCadavaPotion}

// romeoBank is the bank a visit uses: the one nearest the account, else Varrock west's.
func romeoBank(s Snapshot) tile.Tile {
	if s.TileKnown {
		return NearestBank(s.Tile).Tile
	}
	return VarrockWestBank
}

// romeoMessageKit asks the bank for the message. It is optional and untradeable, so the pass reads the bank, draws the
// message when it is there and is satisfied when it is not.
func romeoMessageKit() []KitLine {
	return []KitLine{{ID: ObjJulietMessage, Name: "Message", Qty: 1, Optional: true, Untradeable: true}}
}

// romeoPotionKit is the potion, or the berry it is made from. The potion is looked for in the bank first (optional,
// untradeable); the berry is wanted only while the potion is short, and comes from the bank, the mule, then the three
// ground spawns.
func romeoPotionKit() []KitLine {
	var src []Source
	for _, t := range CadavaSpawns {
		src = append(src, Source{Kind: SourceGround, Tile: t, PerSpawn: 1, RespawnTicks: 100, Note: "the berry patch south-east of Varrock"})
	}
	return []KitLine{
		{ID: ObjCadavaPotion, Name: "Cadava potion", Qty: 1, Optional: true, Untradeable: true},
		{ID: ObjCadavaBerries, Name: "Cadava berries", Qty: 1, OnlyIfShort: ObjCadavaPotion, Sources: src},
	}
}

// DecideRomeoJuliet is the whole quest, read from the journal stage and the pack.
func DecideRomeoJuliet(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: RomeoStartStop}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	// Juliet gives the message into a free slot, and the Apothecary the potion: a full pack loses it.
	room := func(next Step) Step {
		if s.FreeSlots < 2 {
			return Step{Kind: StepDeposit, KeepIDs: romeoTools, Bank: romeoBank(s), BankSet: true}
		}
		return next
	}
	switch s.Stage {
	case RomeoNotStarted:
		return Step{Kind: StepTalk, Stop: RomeoStartStop}
	case RomeoSpokeRomeo:
		return room(Step{Kind: StepTalk, Stop: JulietStop})
	case RomeoSpokeJuliet:
		if held(ObjJulietMessage) {
			return Step{Kind: StepTalk, Stop: RomeoStartStop}
		}
		// Juliet re-issues a lost message only when neither the pack nor the bank holds one.
		if !s.Ran["provision:"+KitRomeoMessage] && !s.Failed["provision:"+KitRomeoMessage] {
			return room(Step{Kind: StepProvision, KitName: KitRomeoMessage, Kit: romeoMessageKit(), KeepIDs: romeoTools,
				Bank: romeoBank(s), BankSet: true, Reason: "Juliet's message, if the trainer banked it (she re-issues none while it is in the bank)"})
		}
		return room(Step{Kind: StepTalk, Stop: JulietStop})
	case RomeoPassedMessage:
		return Step{Kind: StepTalk, Stop: LawrenceStop}
	case RomeoSpokeFather:
		return Step{Kind: StepTalk, Stop: ApothecaryStop}
	case RomeoSpokeApothecary:
		if held(ObjCadavaPotion) {
			return Step{Kind: StepTalk, Stop: JulietStop}
		}
		if held(ObjCadavaBerries) {
			return room(Step{Kind: StepTalk, Stop: ApothecaryStop})
		}
		if s.Failed["provision:"+KitRomeoPotion] {
			return Step{Kind: StepWait, Reason: "no cadava berries from the bank, the mule or the berry patch"}
		}
		return room(Step{Kind: StepProvision, KitName: KitRomeoPotion, Kit: romeoPotionKit(), KeepIDs: romeoTools,
			Bank: romeoBank(s), BankSet: true, Reason: "the potion if banked, else one cadava berry"})
	case RomeoJulietCrypt:
		return Step{Kind: StepTalk, Stop: RomeoStartStop}
	}
	return Step{Kind: StepWait, Reason: "unrecognised Romeo & Juliet stage"}
}

// RomeoJulietRecord is the quest's identity. The server gates nothing.
var RomeoJulietRecord = Record{ID: RomeoJulietID, Name: RomeoJulietName, QuestPoints: 5}

// RomeoJuliet is the engine module. The quest has no fight; two deaths (a stray hostile on the long walks) park it a day.
var RomeoJuliet = Module{
	Record: RomeoJulietRecord, Bank: VarrockWestBank, BankSet: true, Anchor: RomeoAnchor,
	ToolIDs:   romeoTools,
	ReadStage: ParseRomeoJulietJournal,
	MaxDeaths: 2,
	Decide:    DecideRomeoJuliet,
}
