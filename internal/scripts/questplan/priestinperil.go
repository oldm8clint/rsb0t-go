package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Priest in Peril (content id `priestperil`, not transmitted), from the quest-chain study
// (private evidence notes). Every stage is read from
// the journal, which names each one uniquely; the items say the rest. Notes the port relies on:
//
//   - the Temple guardian can only be attacked at stage 2 and is immune to magic (melee only);
//   - the golden key drops from a Monk of Zamorak id 1046 only (1044 and 1045 share the name),
//     and only to the account that killed it (evil_monks.rs2 [ai_queue3,priestperilevilmonk3]);
//   - which of the seven monuments swaps the golden key for the iron key is random per account,
//     so the custom step tries each with a use-item-on (never Take-from, which damages);
//   - gate 1 opens on stage >= 5 alone, gate 2 on stage >= 8;
//   - Drezel refuses NOTED essence, so 50 unnoted rune essence are carried in trips;
//   - Nature Spirit needs stage 61 (the holy barrier talk), one more talk after the tab turns
//     green, which the Nature Spirit module does.

// PriestInPerilName is the quest tab's spelling.
const PriestInPerilName = "Priest in Peril"

// PriestInPerilID is the engine's id.
const PriestInPerilID = "pip"

// Ids from content/pack.
const (
	NpcKingRoald      = 648
	NpcTempleGuardian = 1047
	NpcTrappedDrezel  = 1048
	NpcMausoleumDrez  = 1049
	NpcZamorakKeyMonk = 1046
	ObjGoldenKey      = 2944
	ObjIronKey        = 2945
	ObjMurkyWater     = 2953
	ObjBlessedWater   = 2954
	ObjWolfbane       = 2952
	ObjBronzePickaxe  = 1265
	LocTempleDoorL    = 3489
	LocPipTrapdoor    = 1568
	LocPipTrapdoorOp  = 1570
	LocCellDoor       = 3463
	LocPipCoffin      = 3480
	LocPipWell        = 3485
	MethodMineEssence = "mine-essence"
	KitPipBucket      = "pip-bucket"
	KitPipEssence     = "pip-essence"
	KitPipFood        = "pip-food"
)

// PipMonuments are the seven monuments, loc ids 3493-3499.
var PipMonuments = []int{3493, 3494, 3495, 3496, 3497, 3498, 3499}

// Tiles from content maps (m50_54, m53_54, m53_154).
var (
	RoaldAnchor        = tile.Tile{X: 3222, Z: 3476}
	TempleDoorOutside  = tile.Tile{X: 3407, Z: 3489}
	PipTrapdoorStand   = tile.Tile{X: 3405, Z: 3506}
	PipDungeonLanding  = tile.Tile{X: 3405, Z: 9906}
	TempleGuardianTile = tile.Tile{X: 3405, Z: 9902}
	CellDoorTile       = tile.Tile{X: 3415, Z: 3489, Level: 2}
	CellInside         = tile.Tile{X: 3417, Z: 3489, Level: 2}
	PipCoffinTile      = tile.Tile{X: 3413, Z: 3486, Level: 2}
	TempleMonks        = tile.Tile{X: 3413, Z: 3488}
	PipWellTile        = tile.Tile{X: 3423, Z: 9890}
	PipMonumentsCentre = tile.Tile{X: 3423, Z: 9890}
	MausoleumDrezel    = tile.Tile{X: 3440, Z: 9895}
	AuburyShop         = tile.Tile{X: 3253, Z: 3402}
	pipTools           = []int{ObjGoldenKey, ObjIronKey, ObjBucket, ObjMurkyWater, ObjBlessedWater, ObjRuneEssence, ObjWolfbane, CoinsID}
	pipFoods           = []int{385, 373, 379, 365, 361, 329, 1891, 1893, 1895, 333, 2309, 2142}
)

// Custom step names (internal/scripts/questmods).
const (
	CustomPipKnock     = "pip-knock"
	CustomPipDog       = "pip-dog"
	CustomPipMonk      = "pip-monk"
	CustomPipCellTalk  = "pip-cell-talk"
	CustomPipMausoleum = "pip-mausoleum"
	CustomPipUnlock    = "pip-unlock"
	CustomPipBless     = "pip-bless"
	CustomPipCoffin    = "pip-coffin"
	CustomPipEssence   = "pip-essence"
)

// RoaldStop is King Roald (start, then the no-option stage 3 report).
var RoaldStop = NpcStop{Npc: "King Roald", ID: NpcKingRoald, Anchor: RoaldAnchor, Leash: 8, Prefer: []string{"Sure."}}

// pipJournal are the stage needles, newest first (priestperil_journal.rs2, from the study).
var pipJournal = []struct {
	stage  int
	needle string
}{
	{60, "quest complete"},
	{10, "i need to bring drezel"},
	{8, "i should head downstairs to the"},
	{7, "i should speak to drezel again"},
	{6, "but i still have to do something about that"},
	{5, "to his cell and free him"},
	{4, "will have me executed"},
	{3, "claim my"},
	{2, "has asked me to"},
	{1, "i should head there and"},
	{0, "i can start this quest by speaking to"},
}

// ParsePriestInPerilJournal reads the stage (10 stands for any of 10-59).
func ParsePriestInPerilJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range pipJournal {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

func pipBucketKit() []KitLine {
	return []KitLine{{ID: ObjBucket, Name: "Bucket", Qty: 1, Sources: []Source{
		{Kind: SourceGround, Tile: tile.Tile{X: 3221, Z: 3497, Level: 1}, RespawnTicks: 100},
		{Kind: SourceShop, Shop: VarrockGenStore, Price: 2},
	}}}
}

// BobsAxes is Lumbridge's axe shop (Bob, npc 519, m50_50), which stocks five bronze pickaxes.
var BobsAxes = ShopRef{Npc: "Bob", NpcID: 519, Anchor: tile.Tile{X: 3231, Z: 3203}}

// pipEssenceKit is the unnoted rune essence for Drezel, by what the account can do about it.
//
// Mining it needs Aubury's teleport, which the content refuses until Rune Mysteries is done
// (essence_mine.rs2 opens with a %runemysteries check). So the kit's route depends on the quest
// (rmDone, Snapshot.QuestDone):
//
//   - Rune Mysteries done: a pickaxe and the essence it mines, nothing else. The pickaxe was an
//     optional bank-only line, and the questproof EMPTY lab run pip-empty-3 teleported into the
//     mine with none, mined nothing and walked out through the portal, round after round. The
//     stalls are left out: mining is free and a teleport away, the stalls cost 100-150 gp an
//     essence and a ship-chain walk to Seers.
//   - Rune Mysteries not done: the essence alone, with the Seers' Village player stalls (owner,
//     2026-09-28) as its only source besides the bank and the mule. No pickaxe (a Bronze pickaxe
//     bought at Lumbridge for a mine the account cannot enter was a walk for nothing), and no
//     mining line: live 14 Oct 2026 (04:06-04:45) 18 of 18 rounds walked to Aubury and were
//     refused ("Aubury refuses the teleport: Rune Mysteries is not complete", 13 lines), each a
//     six-minute walk, and then waited.
//
// The market source is budgeted at PipEssenceUnitCeiling each; the runner (MethodMarketEssence,
// wired by ScarabSwarm) chooses the stall and the real price (marketsource.go). It stands on one
// world only and only in a supply mode that shops at player markets, which the runner reports
// (Provisioner.SourceUnavailable) so the plan skips it up front.
func pipEssenceKit(rmDone bool) []KitLine {
	if rmDone {
		return []KitLine{
			{ID: ObjBronzePickaxe, Name: "Bronze pickaxe", Qty: 1, OnlyIfShort: ObjRuneEssence, Sources: []Source{{Kind: SourceShop, Shop: BobsAxes, Price: 1}}},
			{ID: ObjRuneEssence, Name: "Rune essence", Qty: 24, Sources: []Source{
				{Kind: SourceSkill, Skill: "mining", Method: MethodMineEssence, Tile: AuburyShop,
					Note: "Aubury's teleport, open because Rune Mysteries is done"},
			}},
		}
	}
	return []KitLine{
		{ID: ObjRuneEssence, Name: "Rune essence", Qty: 24, Sources: []Source{
			{Kind: SourceMarket, Method: MethodMarketEssence, Tile: SeersMarketStand, Price: PipEssenceUnitCeiling,
				Note: "Seers Market, Tradevault or Sil Stuff; the stall hands notes, un-noted at the Seers bank"},
		}},
	}
}

// PipEssenceNeedsRuneMysteries is the wait reason (and so the park reason) of a Priest in Peril
// whose essence kit came up empty while Rune Mysteries is not done. An older build wrote
// pipEssenceNeedsRMOld; ParkAwaitedQuest knows both.
const (
	PipEssenceNeedsRuneMysteries = "Rune Mysteries is not complete (Aubury's teleport needs it)"
	pipEssenceNeedsRMOld         = "Aubury's teleport needs Rune Mysteries"
)

// PipEssenceRetry is how long a Priest in Peril whose essence kit came up empty stays parked. The
// escalating Backoff (5 minutes, 10, 20, 40, then 2 hours) is the wrong shape for it: each try is
// a bank trip from wherever the account is training, so the early tries are wasted and the late
// ones leave essence the mule now publishes unclaimed for two hours. Thirty minutes, fixed. An
// account that completes Rune Mysteries does not wait it out: ParkAwaitedQuest.
const PipEssenceRetry = 30 * time.Minute

// ParkAwaitedQuest is the quest a park is waiting on, as a ladder id, or "" when the park does not
// wait on one. A Priest in Peril parked on its essence while Rune Mysteries was not done is
// waiting for Rune Mysteries: the ladder clears that park the moment the quest tab reads
// complete, instead of leaving the account out of the quest for the rest of the backoff.
func ParkAwaitedQuest(id, reason string) string {
	if id == PriestInPerilID && (strings.Contains(reason, PipEssenceNeedsRuneMysteries) || strings.Contains(reason, pipEssenceNeedsRMOld)) {
		return RuneMysteriesID
	}
	return ""
}

// pipBucketStep is the bucket for the well. A bucket kit that ended BLOCKED (neither the ground
// spawn nor the general store gave one) waits and parks rather than being provisioned again at
// once, as the essence kit does.
func pipBucketStep(s Snapshot) Step {
	if s.Blocked["provision:"+KitPipBucket] {
		return Step{Kind: StepWait, Reason: "no bucket from the bank, the mule, the Varrock spawn or the general store"}
	}
	return Step{Kind: StepProvision, KitName: KitPipBucket, Kit: pipBucketKit(), KeepIDs: pipTools,
		Bank: VarrockEastBank, BankSet: true, Reason: "a bucket for the well"}
}

// PipKeyMonk is the Monk of Zamorak that drops the golden key: npc 1046, [priestperilevilmonk3] in
// content/scripts/quests/quest_priestperil/configs/priestperil.npc, level 30 (the other two with
// the name are 1044, level 22, and 1045, level 17; neither drops the key). It is not aggressive
// (no huntmode), so it fights only an account that attacked it, in a single-combat area (the
// temple is not in maps/multiway.csv). Its attack script (evil_monks.rs2) rolls random(4): on 0
// a zap, npc_zap_attack(8, attackrate), a magic roll of up to 8; otherwise the default melee hit,
// maximum 3 at Strength 25. Spawns, maps/m53_54.jm2: two on the ground floor (3411,3489 and
// 3415,3485) and one upstairs, among five other monks, all with wanderrange 12.
var PipKeyMonk = MeleeFoe{
	Name: "Monk of Zamorak", Attack: 25, Strength: 25, Defence: 25, Magic: 40, Hitpoints: 25,
	AttackTicks: 4, ZapMax: 8, ZapOneIn: 4,
}

// PipMonkMargin is how many times the expected damage the account's hitpoints and food must cover
// before it walks into the temple: one standard deviation of a fight this long is a third of it,
// and the model assumes no weapon or armour, so twice is a margin that still lets an ordinary
// Stage 5 account in.
const PipMonkMargin = 2

// pipFoodHeal is the hitpoints one item of each of pipFoods restores in all (a cake is three bites
// of 4, content consume_normal.dbrow; fighterfood.foodHeal has the per-bite figures).
var pipFoodHeal = map[int]int{
	385: 20, 373: 14, 379: 12, 365: 13, 361: 10, 329: 9, 1891: 12, 1893: 8, 1895: 4, 333: 7, 2309: 4, 2142: 3,
}

// PipMonkVerdict is PipMonkReady's answer.
type PipMonkVerdict struct {
	OK  bool
	Why string
	// ExpectedLoss is the hitpoints the model expects the fight to cost, Pool the hitpoints and
	// food the account has to pay it with.
	ExpectedLoss float64
	Pool         int
}

// PipMonkReady judges whether the account can be expected to win the monk fight with what it
// holds, by questplan.MeleeExchange (a ceiling on the loss: no weapon or armour is assumed). The
// 5 Oct 2026 deaths at this step were a fight lost for want of attacking, not of odds (fightwatch.go),
// but two of them (acct50, twice) went in with no food at all, and a minimum account (Attack 30,
// Defence 20) with none loses about as many hitpoints as it has. An account whose levels or
// hitpoints are not read yet is not judged.
func PipMonkReady(s Snapshot) PipMonkVerdict {
	att, str, def, mag := s.Level("attack"), s.Level("strength"), s.Level("defence"), s.Level("magic")
	if s.MaxHP == 0 || att == 0 || str == 0 || def == 0 {
		return PipMonkVerdict{OK: true, Why: "levels not read"}
	}
	x := MeleeExchange(MeleeFighter{Attack: att, Strength: str, Defence: def, Magic: mag}, PipKeyMonk)
	pool := s.HP
	for id, n := range s.InvIDs {
		pool += pipFoodHeal[id] * n
	}
	v := PipMonkVerdict{ExpectedLoss: x.DamageTaken, Pool: pool}
	v.OK = float64(pool-1) >= PipMonkMargin*x.DamageTaken
	if !v.OK {
		v.Why = fmt.Sprintf("it costs about %.0f hitpoints at Attack %d, Strength %d, Defence %d and the account has %d hitpoints and food worth %d more, less than %d times that",
			x.DamageTaken, att, str, def, s.HP, pool-s.HP, PipMonkMargin)
	}
	return v
}

// pipMonkStep is the golden key's monk fight behind its food and its odds. Food first: the guardian
// fight's kit (stage 2) is a top-up, but a death or a lost fight empties the pack and the quest
// came back to the monk at stage 4 with nothing to eat (acct50 died twice that way, 5 Oct). Then
// the odds: with the food held, an account that cannot be expected to survive waits (and so parks).
func pipMonkStep(s Snapshot, reason string) Step {
	if !s.Ran["provision:"+KitPipFood] && !s.Failed["provision:"+KitPipFood] {
		return pipFoodStep()
	}
	if v := PipMonkReady(s); !v.OK {
		return Step{Kind: StepWait, Reason: "not fighting the Monk of Zamorak for the key: " + v.Why}
	}
	return Step{Kind: StepCustom, Custom: CustomPipMonk, Reason: reason}
}

func pipFoodStep() Step {
	return Step{Kind: StepProvision, KitName: KitPipFood, Kit: []KitLine{{ID: 379, Name: "Lobster", Qty: 4, Optional: true},
		{ID: 385, Name: "Shark", Qty: 2, Optional: true}}, KeepIDs: append(append([]int(nil), pipTools...), pipFoods...),
		Bank: VarrockEastBank, BankSet: true, Reason: "a little food for the temple guardian (max 3) and the monk"}
}

// DecidePriestInPeril is the whole quest.
func DecidePriestInPeril(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: RoaldStop}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	switch s.Stage {
	case 1:
		return Step{Kind: StepCustom, Custom: CustomPipKnock}
	case 2:
		if !s.Ran["provision:"+KitPipFood] && !s.Failed["provision:"+KitPipFood] {
			return pipFoodStep()
		}
		return Step{Kind: StepCustom, Custom: CustomPipDog, Reason: "melee only: the guardian is immune to magic"}
	case 3:
		return Step{Kind: StepTalk, Stop: RoaldStop}
	case 4:
		if !held(ObjGoldenKey) {
			return pipMonkStep(s, "a Monk of Zamorak (1046) for the golden key")
		}
		return Step{Kind: StepCustom, Custom: CustomPipCellTalk, Reason: "talk to Drezel through the cell door"}
	case 5:
		if held(ObjIronKey) {
			return Step{Kind: StepCustom, Custom: CustomPipUnlock, Reason: "the iron key on the cell door"}
		}
		if !held(ObjBucket) && !held(ObjMurkyWater) {
			return pipBucketStep(s)
		}
		if !held(ObjGoldenKey) {
			return pipMonkStep(s, "the golden key was lost")
		}
		return Step{Kind: StepCustom, Custom: CustomPipMausoleum, Reason: "the monuments for the iron key, the well for water"}
	case 6:
		switch {
		case held(ObjBlessedWater):
			return Step{Kind: StepCustom, Custom: CustomPipCoffin, Reason: "the blessed water on the coffin"}
		case held(ObjMurkyWater):
			return Step{Kind: StepCustom, Custom: CustomPipBless, Reason: "Drezel blesses the water"}
		case !held(ObjBucket):
			return pipBucketStep(s)
		}
		return Step{Kind: StepCustom, Custom: CustomPipMausoleum, Reason: "the well for water"}
	case 7:
		return Step{Kind: StepCustom, Custom: CustomPipBless, Reason: "talk to Drezel again"}
	case 8:
		return Step{Kind: StepCustom, Custom: CustomPipEssence, Reason: "meet Drezel in the mausoleum"}
	case 10:
		if held(ObjRuneEssence) {
			return Step{Kind: StepCustom, Custom: CustomPipEssence, Reason: "hand the essence to Drezel"}
		}
		rmDone := s.QuestDone(RuneMysteriesID)
		if s.Blocked["provision:"+KitPipEssence] {
			// Live 29 Sep - 2 Oct: 423 rounds of "market-essence failed this round; mine-essence failed
			// this round" (no Seers stalls on worlds 2 and 3, Aubury refusing a teleport without Rune
			// Mysteries), each a bank trip, an Aubury walk and a refused teleport, until the slot ran
			// out - all the while holding Priest in Peril's one-at-a-time slot. Wait instead: the
			// quest parks and gives the slot up, and tries again when something may have changed.
			// Live 14 Oct 04:06-04:45 it was still 18 rounds of the same, and 70 parks on "waited 15
			// ticks": the mining line was still tried without Rune Mysteries, and the park was the
			// escalating Backoff.
			if rmDone {
				return Step{Kind: StepWait, ParkFor: PipEssenceRetry,
					Reason: "no unnoted rune essence from the bank, the mule or mining"}
			}
			return Step{Kind: StepWait, ParkFor: PipEssenceRetry,
				Reason: "no unnoted rune essence from the bank, the mule or the Seers market, and " + PipEssenceNeedsRuneMysteries +
					": waiting for the quest or for essence at the mule"}
		}
		reason := "unnoted rune essence: the bank, the mule, then mining it (Rune Mysteries is done)"
		if !rmDone {
			reason = "unnoted rune essence: the bank, the mule, then the Seers market (Rune Mysteries is not done: no mining)"
		}
		return Step{Kind: StepProvision, KitName: KitPipEssence, Kit: pipEssenceKit(rmDone),
			KeepIDs: pipTools, Bank: VarrockEastBank, BankSet: true, Reason: reason}
	}
	return Step{Kind: StepWait, Reason: "pip: unrecognised stage"}
}

// PipPastGuardian is whether the account is past Priest in Peril's one-at-a-time step: the Temple
// guardian, which only one account's kill can remove at a time (live 27 Sep: 71 "no temple
// guardian (respawning after another account's kill?)"). The kill is stage 2; stage 3 is the
// report to King Roald after it, and nothing from there on (the monks, the cell, the well, the
// essence) is shared in a way that needs one account at a time. Holding the slot for the whole 40
// minute quest kept 100 accounts in a queue ("busy: another account is on its one-at-a-time step",
// one account's 24 hours of it, 29 - 30 Sep); holding it for the first stages does not.
func PipPastGuardian(s Snapshot) bool {
	if s.Journal == StatusComplete {
		return true
	}
	return s.Journal == StatusInProgress && s.StageKnown && s.Stage >= 3
}

// PriestInPerilRecord is the quest's identity (no server gates).
// PriestInPerilRecord carries the fleet's safety floor for the temple guardian (melee only; it is immune to
// magic). Live 29 Sep 02:16 acct95 at combat 28 died to it during "pip-dog". The Stage 5 trainer raises
// these; the ladder runs the quest once they are met (as VampireSlayerRecord does).
var PriestInPerilRecord = Record{ID: PriestInPerilID, Name: PriestInPerilName, QuestPoints: 1,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "attack", Level: 30}, {Skill: "defence", Level: 20}, {Skill: "hitpoints", Level: 30}}}}

// PriestInPeril is the engine module.
var PriestInPeril = Module{
	Record: PriestInPerilRecord, Bank: VarrockEastBank, BankSet: true, Anchor: VarrockEastBank,
	ToolIDs:   pipTools,
	ReadStage: ParsePriestInPerilJournal,
	Sustain:   Sustain{FoodIDs: pipFoods, EatAt: 10},
	MaxDeaths: 3,
	// Two deaths at the golden key's monk inside a day skip the quest for a day: live on 5 Oct 2026
	// acct50 died there twice in ten minutes (no food in the pack) and was one death from MaxDeaths.
	StepDeathCaps: map[string]StepDeathCap{
		CustomPipMonk: {Max: 2, Window: 24 * time.Hour, Skip: 24 * time.Hour},
	},
	Decide: DecidePriestInPeril,
	// The Temple guardian is the one-at-a-time step; see PipPastGuardian.
	ExclusiveDone: PipPastGuardian,
}
