package questplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Tree Gnome Village (content id `tree`, %treequest varp 111, not transmitted), from the lane Q
// study (private evidence notes). The ballista's answer is FIXED in
// this content (height 4, x 3, y 5: quest_tree.rs2:21), so the three trackers are skipped; the
// three menus offer identical options, which is why the ballista is a custom step that answers
// them in sequence rather than a preference list (a list would pick "0004" three times). The
// logs must be obj 1511 exactly. The Khazard warlord (112, 170 HP, max 9) has magic level 1 and
// no magic defence, so a caster hits him ~87% of the time; he is cast at from outside his reach
// (treegnome_warlord.go).

// TreeGnomeVillageName is the quest tab's spelling (component 7380).
const TreeGnomeVillageName = "Tree Gnome Village"

// TreeGnomeVillageID is the engine's id.
const TreeGnomeVillageID = "treegnome"

// Ids from content/pack.
const (
	NpcKingBolren     = 469
	NpcMontai         = 470
	NpcKhazardWarlord = 477
	// NpcKhazardCommander is the two commanders of the Khazard stronghold (treegnome_stronghold.go).
	NpcKhazardCommander = 478
	ObjLogs             = 1511
	ObjIronAxe          = 1349
	ObjOrbOfProt        = 587
	ObjOrbsOfProt       = 588
	LocBallista         = 2181
	LocKhazardWall      = 2185
	LocKhazardLadder    = 1747
	LocKhazardLadderT   = 1746
	LocKhazardChestC    = 2183
	LocKhazardChestO    = 2182
	LocKhazardDoor      = 2184
	// LocKhazardInnerDoor is the diagonal door between the stronghold's hall and its ladder room.
	LocKhazardInnerDoor = 1530
	MethodChopLogs      = "chop-logs"
	KitTreeLogs         = "treegnome-logs"
	KitTreeFight        = "treegnome-fight"
	// KitTreeOrb is the food the orb step carries into the Khazard stronghold.
	KitTreeOrb = "treegnome-orb"
)

// Axes by id, any of which chops normal trees.
var Axes = []int{1351, 1349, 1353, 1355, 1357, 1359}

// Tiles from content maps (m39_49, m39_50, m38_51).
var (
	BolrenAnchor      = tile.Tile{X: 2542, Z: 3170}
	MontaiAnchor      = tile.Tile{X: 2523, Z: 3211}
	BallistaStand     = tile.Tile{X: 2511, Z: 3211}
	BallistaTile      = tile.Tile{X: 2508, Z: 3210}
	KhazardWallSouth  = tile.Tile{X: 2509, Z: 3252}
	KhazardLadderTile = tile.Tile{X: 2503, Z: 3252}
	KhazardChestTile  = tile.Tile{X: 2506, Z: 3259, Level: 1}
	KhazardDoorTile   = tile.Tile{X: 2502, Z: 3250}
	WarlordAnchor     = tile.Tile{X: 2457, Z: 3302}
	MontaiTrees       = tile.Tile{X: 2521, Z: 3201}
	treeFoods         = []int{385, 373, 379, 365, 361, 329, 1891, 1893, 1895, 333, 2309, 2142}
	treeTools         = []int{ObjLogs, ObjOrbOfProt, ObjOrbsOfProt, 1351, 1349, 1353, 1355, 1357, 1359, CoinsID}
	// treeSustain is the quest's survival policy: the foods it carries, and the floor its eat line never goes under
	// (a fight raises it for the foe, Sustain.VsFoe).
	treeSustain = Sustain{FoodIDs: treeFoods, EatAt: 14}
)

// BallistaAnswers are the three coordinates, in the order the menus ask for them.
var BallistaAnswers = []string{"0004", "0003", "0005"}

// Custom step names (internal/scripts/questmods).
const (
	CustomTreeBallista = "tree-ballista"
	CustomTreeOrb      = "tree-orb"
	CustomTreeWarlord  = "tree-warlord"
	CustomTreeCeremony = "tree-ceremony"
	// CustomTreeLeave walks the account out of the Khazard stronghold: down the ladder, through the inner door,
	// out of the front door.
	CustomTreeLeave = "tree-leave"
)

var (
	BolrenStop = NpcStop{Npc: "King Bolren", ID: NpcKingBolren, Anchor: BolrenAnchor, Leash: 8,
		Prefer: []string{"Can I help at all?", "I would be glad to help.", "I will find the warlord and bring back the orbs."}}
	MontaiStop = NpcStop{Npc: "Commander Montai", ID: NpcMontai, Anchor: MontaiAnchor, Leash: 8,
		Prefer: []string{"Ok, I'll gather some wood.", "I'll try my best."}}
)

var treeJournal = []struct {
	stage  int
	needle string
}{
	{9, "quest complete"},
	{8, "i should return them now to king bolren"},
	{7, "i should retrieve the two remaining orbs"},
	{6, "i should return the orb to king bolren immediately"},
	{5, "i should get inside and retrieve the gnomes"},
	{4, "find the three gnome trackers"},
	{3, "i should speak to montai again"},
	{2, "i need to bring montai logs"},
	{1, "can help me get it"},
	{0, "i can start this quest by speaking to bolren"},
}

// ParseTreeGnomeVillageJournal reads tree_journal.rs2's rendered stage, newest first.
func ParseTreeGnomeVillageJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range treeJournal {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// AemadShop is East Ardougne's adventuring supplies (the iron axe, 56 gp).
var AemadShop = ShopRef{Npc: "Aemad", NpcID: 590, Anchor: tile.Tile{X: 2613, Z: 3294}}

// treeLogsKit is the six logs, with the axe that chops them first: an owned axe is drawn from
// the bank, otherwise an iron axe is a shop line, so the provisioner draws its coins. The
// questproof EMPTY lab run treegnome-empty-1 left the coin float in the bank and tried to buy
// the axe inside the chop-logs source with an empty pack ("You don't have enough coins.") for
// its whole budget.
func treeLogsKit(s Snapshot) []KitLine {
	axe := KitLine{ID: ObjIronAxe, Name: "Iron axe", Qty: 1, Sources: []Source{{Kind: SourceShop, Shop: AemadShop, Price: 56}}}
	for _, a := range Axes {
		if s.Owned(a) > 0 {
			axe = KitLine{ID: a, Name: "Axe", Qty: 1}
			break
		}
	}
	return []KitLine{axe, {ID: ObjLogs, Name: "Logs", Qty: 6, Sources: []Source{{
		Kind: SourceSkill, Skill: "woodcutting", Method: MethodChopLogs, Tile: MontaiTrees,
		Note: "the trees beside Montai, with the kit's axe",
	}}}}
}

// DecideTreeGnomeVillage is the whole quest.
func DecideTreeGnomeVillage(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: BolrenStop}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	switch s.Stage {
	case 1:
		return Step{Kind: StepTalk, Stop: MontaiStop}
	case 2:
		if s.HeldID(ObjLogs) < 6 {
			return Step{Kind: StepProvision, KitName: KitTreeLogs, Kit: treeLogsKit(s), KeepIDs: treeTools,
				Bank: EastArdougneNorth, BankSet: true, Reason: "six normal logs for Montai"}
		}
		return Step{Kind: StepTalk, Stop: MontaiStop}
	case 3:
		return Step{Kind: StepTalk, Stop: MontaiStop}
	case 4:
		return Step{Kind: StepCustom, Custom: CustomTreeBallista, Reason: "height 4, x 3, y 5"}
	case 5:
		return treeOrbStep(s, "over the wall, up the ladder, the chest")
	case 6:
		if s.HeldID(ObjOrbOfProt) == 0 {
			if n, known := s.BankedID(ObjOrbOfProt); known && n > 0 {
				return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Orb of protection", ID: ObjOrbOfProt, Qty: 1}},
					Bank: EastArdougneNorth, BankSet: true}
			}
			return treeOrbStep(s, "the orb was lost: the chest again")
		}
		return Step{Kind: StepTalk, Stop: BolrenStop}
	case 7:
		return treeWarlordStep(s)
	case 8:
		if s.HeldID(ObjOrbsOfProt) > 0 {
			return Step{Kind: StepCustom, Custom: CustomTreeCeremony, Reason: "the orbs to Bolren, then his ceremony"}
		}
		return treeOrbsLostStep(s)
	}
	return Step{Kind: StepWait, Reason: "treegnome: unrecognised stage"}
}

// treeWarlordStep is the Khazard warlord leg (stage 7, and stage 8 when the orbs are gone): the fight kit once a run,
// then the cast from outside his reach.
func treeWarlordStep(s Snapshot) Step {
	if !s.Ran["provision:"+KitTreeFight] && !s.Failed["provision:"+KitTreeFight] {
		// The kit packs the strongest spell the account owns runes for: read the bank first.
		if !s.BankKnown && WarlordRuneDamage(s) < WarlordHitpoints {
			return Step{Kind: StepScanBank, Bank: EastArdougneNorth, BankSet: true}
		}
		return Step{Kind: StepProvision, KitName: KitTreeFight, Kit: treeFightKit(s), KeepIDs: append(append([]int(nil), treeTools...), treeFoods...),
			Bank: EastArdougneNorth, BankSet: true, Reason: "runes for " + WarlordKitSpell(s) + " and food for the Khazard warlord (170 HP, max 9)"}
	}
	// No runes, no fight (treegnome_warlord.go): a melee at combat 49 was the 4 Oct deaths.
	// The kit that failed on the way is tried again on the quest's next run, after the park.
	if WarlordRuneDamage(s) < WarlordHitpoints {
		return Step{Kind: StepWait, Reason: "the pack's runes are not enough for the Khazard warlord (170 HP) and the fight kit did not bring them"}
	}
	return Step{Kind: StepCustom, Custom: CustomTreeWarlord, Reason: "casting from outside his reach"}
}

// treeCeremonyRange is how near King Bolren an account with no orbs is taken to be inside his ceremony (TS
// CEREMONY_RANGE): he deletes the orbs a tick before the queue that completes the quest (king_bolren.rs2). Only an
// account that has itself run the ceremony step this run is waited for; one that merely stands there is not.
const treeCeremonyRange = 12

// treeOrbsLostStep is stage 8 (the warlord is dead, the orbs are Bolren's to place) with no orbs of protection in the
// pack. Talking to Bolren then only gets "No, I'm afraid not." (king_bolren.rs2), and the step used to do exactly
// that whatever the pack held. The four Tree Gnome accounts the quest stores leave at tree-ceremony have parked 8, 73,
// 8 and 6 times; the log of one (15 Oct 2026 22:21-22:34) is the shape of that dialogue and nothing else: the walk
// to Bolren, then no line at all for ten minutes (Talk, then DriveUntil's sixty seconds for a completion, eight
// ticks to a park). The log does not say where those accounts' orbs went (lost to a death, banked by a trip), so this
// does not guess: it reads the pack, and when the pair is not in it does what the TS bot's reclaim() does. Near
// him, the ceremony has the orbs; else a banked pair is drawn out, else the warlord is killed again, which the
// content pays in orbs while the pack and the bank hold none (khazard_warlord.rs2 defeat_warlord).
func treeOrbsLostStep(s Snapshot) Step {
	ceremonyRun := s.Ran[CustomTreeCeremony] || s.Failed[CustomTreeCeremony]
	if ceremonyRun && s.TileKnown && s.Tile.Level == BolrenAnchor.Level && tile.Chebyshev(s.Tile, BolrenAnchor) <= treeCeremonyRange {
		return Step{Kind: StepWait, Reason: "King Bolren is running the ceremony"}
	}
	if !s.BankKnown {
		return Step{Kind: StepScanBank, Bank: EastArdougneNorth, BankSet: true}
	}
	if n, known := s.BankedID(ObjOrbsOfProt); known && n > 0 {
		return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Orb of protection", ID: ObjOrbsOfProt, Qty: 1}},
			Bank: EastArdougneNorth, BankSet: true}
	}
	return treeWarlordStep(s)
}

// EastArdougneNorth is the bank nearest the Tree Gnome Village.
var EastArdougneNorth = tile.Tile{X: 2616, Z: 3332}

// TreeGnomeVillageRecord is the quest's identity (no server gates).
var TreeGnomeVillageRecord = Record{ID: TreeGnomeVillageID, Name: TreeGnomeVillageName, QuestPoints: 2}

// TreeGnomeVillage is the engine module.
var TreeGnomeVillage = Module{
	Record: TreeGnomeVillageRecord, Bank: EastArdougneNorth, BankSet: true, Anchor: BolrenAnchor,
	ToolIDs:   treeTools,
	ReadStage: ParseTreeGnomeVillageJournal,
	Sustain:   treeSustain,
	MaxDeaths: 3,
	StepDeathCaps: map[string]StepDeathCap{
		CustomTreeWarlord: {Max: WarlordDeathCap, Window: WarlordDeathWindow, Skip: WarlordDeathSkip},
		CustomTreeOrb:     {Max: TreeOrbDeathCap, Window: TreeOrbDeathWindow, Skip: TreeOrbDeathSkip},
	},
	Decide: DecideTreeGnomeVillage,
}
