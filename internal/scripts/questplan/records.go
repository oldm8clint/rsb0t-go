package questplan

import (
	"sort"
	"strings"
	"sync"
)

// The quest records, by ladder id.
//
// A quest's Record (its server-side requirements, the part CheckRequirements reads) is a package variable of
// the quest's own file, and until the needs engine nothing needed them all at once: the live ladder reads the
// record of the Quest it is about to run. The requirement model (needs.go) asks a different question of the
// whole table ("what does every quest want that this account does not have?"), so the records are listed here.
//
// Two quests keep their record outside this package: Waterfall (waterfallplan, which imports questplan and so
// cannot be imported back) and the Barcrawl (questmods, which declares no requirement at all). Waterfall
// registers its own record from its init (RegisterRecord); the Barcrawl has nothing to register. A test in
// internal/bot (TestTheRecordRegistryIsTheFactoriesRecords) compares this table with the record of every
// registered quest factory, so a record that is added, renamed or changed without a line here fails there.

// QuestRecords is every record this package owns, by ladder id (the id LadderFacts and OwnerLadder use).
var QuestRecords = map[string]Record{
	BiohazardID:            BiohazardRecord,
	BlackKnightsFortressID: BlackKnightsFortressRecord,
	ChompyID:               ChompyRecord,
	KnightsSwordID:         KnightsSwordRecord,
	LostCityID:             LostCityRecord,
	ArravID:                ArravRecord,
	MorttonID:              MorttonRecord,
	TouristTrapID:          TouristTrapRecord,
	WatchtowerID:           WatchtowerRecord,
	ClockTowerID:           ClockTowerRecord,
	CooksAssistantID:       CooksAssistantRecord,
	DeathPlateauID:         DeathPlateauRecord,
	DemonSlayerID:          DemonSlayerRecord,
	DigsiteID:              DigsiteRecord,
	DoricsQuestID:          DoricsQuestRecord,
	DragonSlayerID:         DragonSlayerRecord,
	DruidicRitualID:        DruidicRitualRecord,
	DwarfCannonID:          DwarfCannonRecord,
	ElementalWorkshopID:    ElementalWorkshopRecord,
	ErnestChickenID:        ErnestChickenRecord,
	FightArenaID:           FightArenaRecord,
	FishingContestID:       FishingContestRecord,
	FremennikID:            FremennikRecord,
	GertrudesCatID:         GertrudesCatRecord,
	GoblinDiplomacyID:      GoblinDiplomacyRecord,
	GrandTreeID:            GrandTreeRecord,
	HazeelCultID:           HazeelCultRecord,
	HolyGrailID:            HolyGrailRecord,
	HorrorID:               HorrorRecord,
	ImpCatcherID:           ImpCatcherRecord,
	JunglePotionID:         JunglePotionRecord,
	MerlinsCrystalID:       MerlinsCrystalRecord,
	MonksFriendID:          MonksFriendRecord,
	MurderMysteryID:        MurderMysteryRecord,
	NatureSpiritID:         NatureSpiritRecord,
	ObservatoryID:          ObservatoryRecord,
	PiratesTreasureID:      PiratesTreasureRecord,
	PlagueCityID:           PlagueCityRecord,
	PriestInPerilID:        PriestInPerilRecord,
	PrinceAliID:            PrinceAliRecord,
	RestlessGhostID:        RestlessGhostRecord,
	RomeoJulietID:          RomeoJulietRecord,
	RuneMysteriesID:        RuneMysteries.Record,
	ScorpionCatcherID:      ScorpionCatcherRecord,
	SeaSlugID:              SeaSlugRecord,
	SheepHerderID:          SheepHerderRecord,
	SheepShearerID:         SheepShearerRecord,
	TaiBwoID:               TaiBwoRecord,
	TreeGnomeVillageID:     TreeGnomeVillageRecord,
	TribalTotemID:          TribalTotemRecord,
	VampireSlayerID:        VampireSlayerRecord,
	WitchsHouseID:          WitchsHouseRecord,
	WitchsPotionID:         WitchsPotionRecord,
}

var (
	recordsMu  sync.RWMutex
	recordsReg = map[string]Record{}
)

// RegisterRecord adds the record of a quest that lives in a package questplan cannot import (Waterfall), from
// that package's init. A record registered twice under one id keeps the last.
func RegisterRecord(r Record) {
	recordsMu.Lock()
	defer recordsMu.Unlock()
	recordsReg[strings.ToLower(r.ID)] = r
}

// RecordOf is the record of quest id: the table above, else a registered one. A training rung and a quest
// that declares no record (the Barcrawl) have none.
func RecordOf(id string) (Record, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	if r, ok := QuestRecords[id]; ok {
		return r, true
	}
	recordsMu.RLock()
	defer recordsMu.RUnlock()
	r, ok := recordsReg[id]
	return r, ok
}

// RecordIDs are the ids RecordOf answers for, sorted.
func RecordIDs() []string {
	seen := map[string]bool{}
	for id := range QuestRecords {
		seen[id] = true
	}
	recordsMu.RLock()
	for id := range recordsReg {
		seen[id] = true
	}
	recordsMu.RUnlock()
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
