package questplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Nature Spirit (content id `druidspirit`, %druidspirit and %druidspirit_bits, not transmitted),
// from the lane Q study (private evidence notes) and the TS def
// (rsb0t-main/src/bot/api/ai/quests/defs/druidspirit), whose journal markers follow
// druidspirit_journal.rs2. The stone bits (nature fed, spirit fed) show only on the journal
// page, so the stage this module reads packs them in: stage + 1000*natureFed + 2000*spiritFed.
// Content gates the start on Priest in Peril at stage 61: a first Drezel talk at 60 only moves it
// to 61, so the start can take a second talk (drezel.rs2). Drezel never checks the ghostspeak
// amulet; only Filliman's talk needs it worn (filliman.rs2). Restless Ghost's completion itself
// is never checked.

// NatureSpiritName is the quest tab's spelling (component 8137).
const NatureSpiritName = "Nature Spirit"

// NatureSpiritID is the engine's id.
const NatureSpiritID = "spirit"

// Stages (quest_druidspirit.constant).
const (
	SpiritStarted     = 5
	SpiritSwamp       = 10
	SpiritFailedTalk  = 15
	SpiritSpoken      = 20
	SpiritMirror      = 25
	SpiritJournal     = 30
	SpiritSpell       = 35
	SpiritBlessed     = 40
	SpiritCast        = 45
	SpiritFungi       = 50
	SpiritSpoken2     = 55
	SpiritRitual      = 60
	SpiritGrotto      = 65
	SpiritTransformed = 70
	SpiritSickleBless = 75
	SpiritSickleBloom = 80
	SpiritPicked      = 85
	SpiritPouch       = 90
	SpiritGhast1      = 95
	SpiritGhast2      = 100
	SpiritGhast3      = 105
	SpiritComplete    = 110
	spiritNatureBit   = 1000
	spiritSpiritBit   = 2000
)

// SpiritTracks unpacks the packed stage.
func SpiritTracks(stage int) (st int, nature, spirit bool) {
	return stage % 1000, stage/1000&1 == 1, stage/1000&2 == 2
}

// Ids from content/pack.
const (
	NpcDrezelFree     = 1049
	NpcFilliman       = 1050
	NpcNatureSpirit   = 1051
	NpcGhastHidden    = 1052
	NpcGhastShown     = 1053
	ObjPouchEmpty     = 2957
	ObjPouch          = 2958
	ObjSickleBlessed  = 2963
	ObjMirror         = 2966
	ObjFillJournal    = 2967
	ObjBloomSpell     = 2968
	ObjUsedSpell      = 2969
	ObjMortFungi      = 2970
	ObjMortStem       = 2972
	ObjMortPear       = 2974
	ObjSickleMould    = 2976
	ObjSilverOre      = 442
	ObjSilverBar      = 2355
	LocGrottoDoor     = 3516
	LocGrottoTree     = 3517
	LocGrottoPool     = 3520
	LocNatureStone    = 3527
	LocSpiritStone    = 3529
	BloomMaxCost      = 7
	MethodMineSilver  = "mine-silver"
	MethodSmeltSilver = "smelt-silver"
)

// Tiles (study and TS areas.ts).
var (
	DrezelFreeAnchor = tile.Tile{X: 3440, Z: 9895}
	SpiritCamp       = tile.Tile{X: 3440, Z: 3337}
	FillimanTile     = tile.Tile{X: 3440, Z: 3336}
	NatureStoneTile  = tile.Tile{X: 3439, Z: 3336}
	FaithStoneTile   = tile.Tile{X: 3440, Z: 3335}
	SpiritStoneTile  = tile.Tile{X: 3441, Z: 3336}
	WashingBowlTile  = tile.Tile{X: 3437, Z: 3337}
	GrottoTreeTile   = tile.Tile{X: 3439, Z: 3338}
	GrottoDoorTile   = tile.Tile{X: 3440, Z: 3337}
	GrottoInside     = tile.Tile{X: 3442, Z: 9734}
	GrottoPoolTile   = tile.Tile{X: 3441, Z: 9740}
	PaterdomusAltar  = tile.Tile{X: 3416, Z: 3488}
	GhastHunt        = tile.Tile{X: 3424, Z: 3337}
	DommikShop       = ShopRef{Npc: "Dommik", Anchor: tile.Tile{X: 3322, Z: 3194}}
	AlKharidFurnace  = tile.Tile{X: 3272, Z: 3183}
	SilverRocks      = tile.Tile{X: 3294, Z: 3301}
	AlKharidBank     = tile.Tile{X: 3269, Z: 3167}
	spiritFoods      = []int{385, 373, 379, 365, 361, 329, 2327, 2323}
	spiritTools      = []int{ObjGhostspeak, ObjMirror, ObjFillJournal, ObjBloomSpell, ObjUsedSpell, ObjMortFungi, ObjMortStem,
		ObjMortPear, ObjSilverSickle, ObjSickleBlessed, ObjPouchEmpty, ObjPouch, ObjSickleMould, ObjSilverOre, ObjSilverBar,
		CoinsID, 2952, 1265, 1267, 1269, 1273, 1271, 1275, 554, 556, 558, 562, 1381}
)

// InMortMyre is the zone the bloom spell and the swamp decay test.
func InMortMyre(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 3392 && t.X <= 3519 && t.Z >= 3328 && t.Z <= 3455
}

// InGrotto is the sealed pocket under the camp.
func InGrotto(t tile.Tile) bool { return t.X >= 3430 && t.X <= 3455 && t.Z >= 9720 && t.Z <= 9750 }

// Custom step names (internal/scripts/questmods).
const (
	CustomSpiritTalk     = "spirit-talk"
	CustomSpiritScroll   = "spirit-scroll"
	CustomSpiritMirror   = "spirit-mirror"
	CustomSpiritJournal  = "spirit-journal"
	CustomSpiritHelp     = "spirit-help"
	CustomSpiritBloom    = "spirit-bloom"
	CustomSpiritStones   = "spirit-stones"
	CustomSpiritPuzzle   = "spirit-puzzle"
	CustomSpiritEnter    = "spirit-enter-grotto"
	CustomSpiritLeave    = "spirit-leave-grotto"
	CustomSpiritGrotto   = "spirit-grotto-talk"
	CustomSpiritDipBless = "spirit-dip-sickle"
	CustomSpiritCast     = "spirit-cast-sickle"
	CustomSpiritSmelt    = "spirit-smelt"
	CustomSpiritHarvest  = "spirit-harvest"
	CustomSpiritFill     = "spirit-fill-pouch"
	CustomSpiritGhast    = "spirit-ghast"
	CustomSpiritDrezel   = "spirit-drezel"
	KitSpiritParts       = "spirit-sickle-parts"
)

var (
	DrezelFree = NpcStop{Npc: "Drezel", ID: NpcDrezelFree, Anchor: DrezelFreeAnchor, Leash: 4, Prefer: []string{
		"Is there anything else interesting to do around here?", "Well, what is it, I may be able to help?",
		"Yes, I'll go and look for him.", "Yes, I'm sure."}}
)

var spiritJournal = []struct {
	needle string
	stage  int
}{
	{"quest complete!", SpiritComplete},
	{"i've killed three ghasts now.", SpiritGhast3},
	{"i've killed two ghasts now.", SpiritGhast2},
	{"the druid pouch made a ghast appear", SpiritGhast1},
	{"them into a druid pouch", SpiritPouch},
	{"i collected some bloomed items from the swamp.", SpiritPicked},
	{"i cast the bloom spell in the swamp.", SpiritSickleBloom},
	{"filliman has blessed the silver sickle for me.", SpiritSickleBless},
	{"filliman has turned into a nature spirit", SpiritTransformed},
	{"i entered fillimans grotto as he asked me to.", SpiritGrotto},
	{"he says that he can cast the spell now", SpiritRitual},
	{"i collected a mort myre fungi.", SpiritFungi},
	{"i've cast the bloom spell in the swamp.", SpiritCast},
	{"i've been blessed at the temple by drezel.", SpiritBlessed},
	{"spell but i need to be", SpiritSpell},
	{"might need my help with his plan", SpiritJournal},
	{"to find his journal", SpiritMirror},
	{"i've found filliman's journal", SpiritMirror},
	{"convince this poor fellow", SpiritSpoken},
	{"i can't really communicate with this", SpiritFailedTalk},
	{"i need to look for filliman tarlock", SpiritStarted},
	{"i can start this quest by speaking to", 0},
}

// ParseNatureSpiritJournal reads druidspirit_journal.rs2 into the packed stage (TS journal.ts's
// markers, newest first; the stone lines show at stages 50 and 55 only).
func ParseNatureSpiritJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, m := range spiritJournal {
		if !strings.Contains(text, m.needle) {
			continue
		}
		st := m.stage
		if st >= SpiritRitual || strings.Contains(text, "absorbed into the nature stone") {
			st += spiritNatureBit
		}
		if m.stage >= SpiritRitual || strings.Contains(text, "absorbed into the spirit stone") {
			st += spiritSpiritBit
		}
		return st, true
	}
	return 0, false
}

func spiritHarvest(s Snapshot) int {
	return s.HeldID(ObjMortPear) + s.HeldID(ObjMortStem) + s.HeldID(ObjMortFungi)
}

// sickleStep sources a silver sickle: bank, then mould (bank, mule, Dommik) and ore (bank, mule,
// Mining 20), smelted and cast at the Al Kharid furnace (Crafting 18). Zero Kind means held.
func sickleStep(s Snapshot, keep []int) Step {
	held := func(id int) bool { return s.HeldID(id) > 0 }
	if held(ObjSilverSickle) || held(ObjSickleBlessed) {
		return Step{}
	}
	for _, id := range []int{ObjSickleBlessed, ObjSilverSickle} {
		if n, _ := s.BankedID(id); n > 0 {
			return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Silver sickle", ID: id, Qty: 1}},
				Bank: AlKharidBank, BankSet: true, KeepIDs: keep}
		}
	}
	if held(ObjSickleMould) && held(ObjSilverBar) {
		return Step{Kind: StepCustom, Custom: CustomSpiritCast, Reason: "cast the silver sickle at the Al Kharid furnace"}
	}
	if held(ObjSickleMould) && held(ObjSilverOre) {
		return Step{Kind: StepCustom, Custom: CustomSpiritSmelt, Reason: "smelt the silver bar"}
	}
	kit := []KitLine{{ID: ObjSickleMould, Name: "Sickle mould", Qty: 1, Sources: []Source{{Kind: SourceShop, Shop: DommikShop, Price: 10}}}}
	if !held(ObjSilverBar) {
		if n, _ := s.BankedID(ObjSilverBar); n > 0 {
			kit = append(kit, KitLine{ID: ObjSilverBar, Name: "Silver bar", Qty: 1})
		} else {
			// The ore needs a pickaxe: without one the questproof EMPTY lab run spirit-empty-3
			// failed mine-silver round after round at the Al Kharid rocks. Mining 20 is a level an account can be trained
			// to (Train): once a Mining trainer exists, an account below it and with no ore from the bank or the mule parks
			// on "needs:mining:20" for a stint instead of waiting on a block; until one does, the catalogue's gap is
			// named in the blocked reason.
			kit = append(kit, pickaxeLine(s), KitLine{ID: ObjSilverOre, Name: "Silver ore", Qty: 1, Sources: []Source{
				{Kind: SourceSkill, Skill: "mining", Level: 20, Method: MethodMineSilver, Tile: SilverRocks, Train: true},
			}})
		}
	}
	return Step{Kind: StepProvision, KitName: KitSpiritParts, Kit: kit, KeepIDs: keep,
		Bank: AlKharidBank, BankSet: true, Reason: "a sickle mould and silver for the sickle"}
}

// DecideNatureSpirit is the whole quest.
func DecideNatureSpirit(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	}
	if s.Journal == StatusInProgress && !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	st, nature, spirit := SpiritTracks(s.Stage)
	if s.Journal == StatusNotStarted {
		st = 0
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	keep := append(append([]int(nil), spiritTools...), spiritFoods...)
	inGrotto := s.TileKnown && InGrotto(s.Tile)
	outside := func(step Step) Step {
		if inGrotto {
			return Step{Kind: StepCustom, Custom: CustomSpiritLeave, Reason: "out of the grotto first"}
		}
		return step
	}
	// Every word with either spirit needs the amulet worn.
	if s.WornIDs[ObjGhostspeak] == 0 {
		if held(ObjGhostspeak) {
			return Step{Kind: StepEquip, ItemID: ObjGhostspeak, Item: "Ghostspeak amulet"}
		}
		if n, _ := s.BankedID(ObjGhostspeak); n > 0 {
			return outside(Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Ghostspeak amulet", ID: ObjGhostspeak, Qty: 1}},
				Bank: VarrockEastBank, BankSet: true, KeepIDs: keep})
		}
		if !s.BankKnown {
			return outside(Step{Kind: StepScanBank, Bank: VarrockEastBank, BankSet: true})
		}
		return outside(Step{Kind: StepTalk, Stop: UrhneyStop})
	}
	switch st {
	case 0:
		return outside(Step{Kind: StepCustom, Custom: CustomSpiritDrezel, Reason: "Drezel, in the mausoleum"})
	case SpiritStarted, SpiritSwamp, SpiritFailedTalk:
		return outside(Step{Kind: StepCustom, Custom: CustomSpiritTalk, Reason: "find Filliman"})
	case SpiritSpoken:
		return outside(Step{Kind: StepCustom, Custom: CustomSpiritMirror, Reason: "the mirror under the washing bowl"})
	case SpiritMirror:
		return outside(Step{Kind: StepCustom, Custom: CustomSpiritJournal, Reason: "Filliman's journal in the grotto tree"})
	case SpiritJournal:
		return outside(Step{Kind: StepCustom, Custom: CustomSpiritHelp, Reason: "offer to help"})
	case SpiritSpell:
		return outside(Step{Kind: StepCustom, Custom: CustomSpiritDrezel, Reason: "Drezel's blessing"})
	case SpiritBlessed, SpiritCast, SpiritFungi, SpiritSpoken2:
		if !nature && !held(ObjMortFungi) {
			if !held(ObjBloomSpell) {
				return outside(Step{Kind: StepCustom, Custom: CustomSpiritScroll, Reason: "another bloom scroll from Filliman"})
			}
			return outside(Step{Kind: StepCustom, Custom: CustomSpiritBloom, Reason: "bloom a rotting log for a fungus"})
		}
		if !nature || !spirit {
			return outside(Step{Kind: StepCustom, Custom: CustomSpiritStones, Reason: "feed the ritual stones"})
		}
		return outside(Step{Kind: StepCustom, Custom: CustomSpiritPuzzle, Reason: "stand on the faith stone and solve the puzzle"})
	case SpiritRitual:
		return Step{Kind: StepCustom, Custom: CustomSpiritEnter, Reason: "into the grotto"}
	case SpiritGrotto:
		return Step{Kind: StepCustom, Custom: CustomSpiritGrotto, Reason: "watch the transformation"}
	case SpiritTransformed:
		if held(ObjSilverSickle) || held(ObjSickleBlessed) {
			return Step{Kind: StepCustom, Custom: CustomSpiritGrotto, Reason: "hand over the sickle"}
		}
		return outside(sickleStep(s, keep))
	case SpiritSickleBless, SpiritSickleBloom, SpiritPicked, SpiritPouch, SpiritGhast1, SpiritGhast2:
		if !held(ObjSickleBlessed) {
			if held(ObjSilverSickle) {
				return Step{Kind: StepCustom, Custom: CustomSpiritDipBless, Reason: "dip a plain sickle in the grotto pool"}
			}
			return outside(sickleStep(s, keep))
		}
		switch {
		case held(ObjPouch):
			return outside(Step{Kind: StepCustom, Custom: CustomSpiritGhast, Reason: "pop a ghast with the pouch and kill it"})
		case spiritHarvest(s) >= 3:
			return outside(Step{Kind: StepCustom, Custom: CustomSpiritFill, Reason: "three harvests into the druid pouch"})
		}
		return outside(Step{Kind: StepCustom, Custom: CustomSpiritHarvest, Reason: "bloom and harvest with the blessed sickle"})
	case SpiritGhast3:
		return Step{Kind: StepCustom, Custom: CustomSpiritGrotto, Reason: "claim the reward"}
	}
	return Step{Kind: StepWait, Reason: "nature spirit: unrecognised stage"}
}

// NatureSpiritRecord is the quest's identity: content checks Priest in Peril at 61.
var NatureSpiritRecord = Record{ID: NatureSpiritID, Name: NatureSpiritName, QuestPoints: 2,
	Requirements: Requirements{Quests: []string{PriestInPerilID}}}

// NatureSpirit is the engine module.
var NatureSpirit = Module{
	Record: NatureSpiritRecord, Bank: VarrockEastBank, BankSet: true, Anchor: SpiritCamp,
	ToolIDs:   spiritTools,
	ReadStage: ParseNatureSpiritJournal,
	Sustain:   Sustain{FoodIDs: spiritFoods, EatAt: 12},
	MaxDeaths: 3,
	Decide:    DecideNatureSpirit,
}

// Pickaxes are bronze to rune (content pickaxe objs).
var Pickaxes = []int{1265, 1267, 1269, 1273, 1271, 1275}

// pickaxeLine is an owned pickaxe from the bank, or a bronze pickaxe from Bob's Axes.
func pickaxeLine(s Snapshot) KitLine {
	for _, p := range Pickaxes {
		if s.Owned(p) > 0 {
			return KitLine{ID: p, Name: "Pickaxe", Qty: 1}
		}
	}
	return KitLine{ID: ObjBronzePickaxe, Name: "Bronze pickaxe", Qty: 1, Sources: []Source{{Kind: SourceShop, Shop: BobsAxes, Price: 1}}}
}
