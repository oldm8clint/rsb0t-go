package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Merlin's Crystal (content id `arthur`, %arthur, not transmitted), ported from
// defs/merlinscrystal.ts and corrected against content 289 by the quest-chain study
// (private evidence notes):
//
//   - the stage comes from the journal, not the TS's in-memory "Mordred briefed" flag, so a
//     restart mid-quest is safe; the chaos-altar words are the one sub-flag only the journal
//     shows, so ParseMerlinsCrystalJournal returns MerlinWordsKnown (41) for "stage 4 with the
//     words memorised";
//   - bread for the beggar comes from the bank, the mule or the East Ardougne baker: Wydin and
//     the Port Sarim food shop stock 0 bread in content (the TS bought from Wydin and never got
//     any);
//   - a missing crystal is NOT proof it broke (one account's break deletes it for 50 ticks for
//     everyone): only the journal moving to stage 6 counts;
//   - the Wilderness altar is visited only while the words are not memorised;
//   - the giant bats are the three just west of the keep's front door, not the TS's anchor 180
//     tiles away.

// MerlinsCrystalName is the quest tab's spelling.
const MerlinsCrystalName = "Merlin's Crystal"

// MerlinsCrystalID is the engine's id.
const MerlinsCrystalID = "merlin"

// Stages (quest_arthur.constant) plus the journal-only sub-flag.
const (
	MerlinStarted     = 1 // Arthur
	MerlinGawain      = 2
	MerlinLancelot    = 3
	MerlinMorgan      = 4 // Morgan's brief after Mordred
	MerlinWordsKnown  = 41
	MerlinBound       = 5 // Thrantax bound
	MerlinCrystalGone = 6
	MerlinComplete    = 7
)

// Ids from content/pack.
const (
	NpcKingArthur     = 251
	NpcGawain         = 240
	NpcLancelot       = 239
	NpcMordred        = 247
	NpcMorganLeFaye   = 248
	NpcLadyOfTheLake  = 250
	NpcBeggar         = 252
	NpcCandleMaker    = 562
	NpcArhein         = 563
	NpcGiantBat       = 78
	ObjExcalibur      = 35
	ObjBlackCandle    = 38
	ObjLitBlackCandle = 32
	ObjBatBones       = 530
	ObjBucketOfWax    = 30
	ObjBucket         = 1925
	ObjInsectRepel    = 28
	ObjBread          = 2309
	ObjTinderbox      = 590
	LocCatherbyCrate  = 63
	LocKeepCrate      = 64
	LocChaosAltarWord = 61
	LocGiantCrystal   = 62
	LocBeehive        = 68
	LocJewellerDoor   = 59
)

// Tiles from content maps (m43_53, m43_54, m45_53, m50_56) and the study.
var (
	ArthurAnchor      = tile.Tile{X: 2764, Z: 3515}
	GawainAnchor      = tile.Tile{X: 2763, Z: 3506}
	LancelotAnchor    = tile.Tile{X: 2759, Z: 3515, Level: 1}
	CatherbyCrate     = tile.Tile{X: 2801, Z: 3442}
	KeepCrate         = tile.Tile{X: 2779, Z: 3401}
	MordredAnchor     = tile.Tile{X: 2769, Z: 3403, Level: 2}
	KeepBats          = tile.Tile{X: 2755, Z: 3402}
	BatSafespot       = tile.Tile{X: 2762, Z: 3399}
	ChaosAltarStand   = tile.Tile{X: 3239, Z: 3607}
	LadyAnchor        = tile.Tile{X: 2924, Z: 3405}
	JewellerDoorStand = tile.Tile{X: 3016, Z: 3247}
	CandleMakerTile   = tile.Tile{X: 2800, Z: 3439}
	BeehiveTile       = tile.Tile{X: 2758, Z: 3444}
	RepellentSpawn    = tile.Tile{X: 2807, Z: 3450}
	BucketSpawn       = tile.Tile{X: 2766, Z: 3441}
	MagicSymbol       = tile.Tile{X: 2780, Z: 3515}
	MerlinCrystalTile = tile.Tile{X: 2767, Z: 3493, Level: 2}
	CatherbyBank      = tile.Tile{X: 2809, Z: 3440}
	ArheinStore       = ShopRef{Npc: "Arhein", NpcID: NpcArhein, Anchor: tile.Tile{X: 2803, Z: 3430}}
	MerlinFoodIDs     = []int{385, 373, 379, 365, 361, 329, 1891, 1893, 1895, objChocolateSlice, 333, 2142}
	merlinTools       = []int{ObjExcalibur, ObjBlackCandle, ObjLitBlackCandle, ObjBatBones, ObjBucketOfWax,
		ObjBucket, ObjInsectRepel, ObjBread, ObjTinderbox, CoinsID}
)

// Custom step names (internal/scripts/questmods).
const (
	CustomMerlinKeep      = "merlin-keep"
	CustomMerlinLeaveKeep = "merlin-leave-keep"
	CustomMerlinBat       = "merlin-bat"
	CustomMerlinWax       = "merlin-wax"
	CustomMerlinExcalibur = "merlin-excalibur"
	CustomMerlinAltar     = "merlin-altar"
	CustomMerlinStrip     = "merlin-strip"
	CustomMerlinSummon    = "merlin-summon"
	CustomMerlinCrystal   = "merlin-crystal"
	KitMerlin             = "merlin"
	KitMerlinFight        = "merlin-fight"
)

var (
	ArthurStop = NpcStop{Npc: "King Arthur", ID: NpcKingArthur, Anchor: ArthurAnchor, Leash: 6,
		Prefer: []string{"I want to become a Knight of the Round Table!"}}
	GawainStop = NpcStop{Npc: "Sir Gawain", ID: NpcGawain, Anchor: GawainAnchor, Leash: 8,
		Prefer: []string{"Do you know how Merlin got trapped?", "Thank you for the information."}}
	// LancelotStop: "ideas" (plural) is Lancelot's; Gawain's is the singular "idea".
	LancelotStop = NpcStop{Npc: "Sir Lancelot", ID: NpcLancelot, Anchor: LancelotAnchor, Leash: 8,
		Prefer: []string{"Any ideas on how to get into Morgan Le Faye's stronghold?"}}
	CandleMakerStop = NpcStop{Npc: "Candle maker", ID: NpcCandleMaker, Anchor: CandleMakerTile, Leash: 6,
		Prefer: []string{"Have you got any black candles?"}}
)

// MorganPrefer is Morgan Le Faye's answer that sets stage 4, then the brief's closing choice.
var MorganPrefer = []string{"Tell me how to untrap Merlin and I might.", "OK I will go do all that."}

// ThrantaxWords is the only safe answer; the two wrong ones delete the lit candle and make
// Thrantax (92, max 9) attack.
const ThrantaxWords = "Snarthon Candtrick Termanto"

// ParseMerlinsCrystalJournal reads arthur_journal.rs2's rendered stage, newest first.
func ParseMerlinsCrystalJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	switch {
	case text == "":
		return 0, false
	case strings.Contains(text, "quest complete"):
		return MerlinComplete, true
	case strings.Contains(text, "now that merlin is freed i should speak to king arthur"):
		return MerlinCrystalGone, true
	case strings.Contains(text, "i should free merlin by using excalibur on the crystal"):
		return MerlinBound, true
	case strings.Contains(text, "to bind thrantax to my will i will need the following"):
		if strings.Contains(text, "i have the magic words from the chaos altar memorised") {
			return MerlinWordsKnown, true
		}
		return MerlinMorgan, true
	case strings.Contains(text, "there might be a way to enter with a delivery by sea"):
		return MerlinLancelot, true
	case strings.Contains(text, "who lives in an impenetrable fortress"):
		return MerlinGawain, true
	case strings.Contains(text, "i should ask the other knights if they have any advice"):
		return MerlinStarted, true
	}
	return 0, false
}

// InsideKeep reports whether t is inside Keep Le Faye (any floor).
func InsideKeep(t tile.Tile) bool {
	return t.X >= 2762 && t.X <= 2782 && t.Z >= 3396 && t.Z <= 3410
}

func merlinKit(s Snapshot) []KitLine {
	kit := []KitLine{
		{ID: ObjBread, Name: "Bread", Qty: 1, Sources: []Source{{Kind: SourceShop, Shop: ShopRef{Npc: "Baker", NpcID: 571,
			Anchor: tile.Tile{X: 2669, Z: 3310}}, Price: 12}}},
		{ID: ObjInsectRepel, Name: "Insect repellent", Qty: 1, Sources: []Source{{Kind: SourceGround, Tile: RepellentSpawn, RespawnTicks: 100}}},
		{ID: ObjBucket, Name: "Bucket", Qty: 1, Sources: []Source{
			{Kind: SourceGround, Tile: BucketSpawn, RespawnTicks: 100},
			{Kind: SourceShop, Shop: ArheinStore, Price: 2}}},
		{ID: ObjTinderbox, Name: "Tinderbox", Qty: 1, Sources: []Source{{Kind: SourceShop, Shop: ArheinStore, Price: 1}}},
		{ID: ObjBatBones, Name: "Bat bones", Qty: 1, Optional: true},
	}
	return append(kit, merlinBatLines(s)...)
}

// merlinBatCasts is the runes drawn back for the giant bat (32 hitpoints, magic defence 1).
const merlinBatCasts = 25

// merlinBatLines draw back what the bat is cast at with: the Mordred kit's leftover runes and an
// air staff, both banked by the strip before the Wilderness. Optional and never bought (a bat can
// be meleed), so an account that has none loses nothing but the casting.
func merlinBatLines(s Snapshot) []KitLine {
	var out []KitLine
	staff := wearsAirStaff(s)
	if !staff {
		if n, _ := s.BankedID(1381); n > 0 || s.InvIDs[1381] > 0 {
			out = append(out, KitLine{ID: 1381, Name: "Staff of air", Qty: 1, Optional: true, Worn: true})
			staff = true
		}
	}
	switch magic := s.Level("magic"); {
	case magic >= 17:
		out = append(out, KitLine{ID: objChaosRune, Name: "Chaos rune", Qty: merlinBatCasts, Stackable: true, Optional: true})
	case magic >= 13:
		out = append(out,
			KitLine{ID: objMindRune, Name: "Mind rune", Qty: merlinBatCasts, Stackable: true, Optional: true},
			KitLine{ID: objFireRune, Name: "Fire rune", Qty: 3 * merlinBatCasts, Stackable: true, Optional: true})
	default:
		return out
	}
	if !staff {
		out = append(out, KitLine{ID: objAirRune, Name: "Air rune", Qty: 2 * merlinBatCasts, Stackable: true, Optional: true})
	}
	return out
}

// merlinCasts is how many casts the Mordred kit carries: he has 38 hitpoints and magic defence 1,
// yet the second Merlin EMPTY lab run (merlin-empty-2) spent 29 Wind Bolts on him, leaving the
// giant bat to be meleed down to 4 hitpoints. Sixty cover him, a Renegade Knight and the bat,
// whose runes the stage-4 kit draws back from the bank.
const merlinCasts = 60

// Rune ids the Mordred kit buys.
const (
	objAirRune   = 556
	objMindRune  = 558
	objFireRune  = 554
	objChaosRune = 562
)

// merlinFightRunes are the rune ids the fight kit may carry, kept through its bank visits.
var merlinFightRunes = []int{objAirRune, objMindRune, objFireRune, objChaosRune}

// wearsAirStaff reports a worn staff that pays air runes (a staff only in the pack or the bank
// pays nothing: BestCastableSpell counts worn staves).
func wearsAirStaff(s Snapshot) bool {
	return s.WornIDs[1381] > 0 || s.WornIDs[1397] > 0 || s.WornIDs[1405] > 0
}

// merlinFightKit is what Sir Mordred is fought with. The study (journals/wf_240af715-c0f__study_merlin.json)
// puts him at level 39, crush defence 45 and magic defence 1: a 20-hitpoint Stage 5 account in
// melee does a tenth of a hitpoint a tick to his 38 while he lands about 70% of his 0-5 swings
// every four ticks, so melee is a death, and the kit is runes and food, each by the item rule
// (bank, mule, then the shop):
//
//   - Magic 17 and up: Wind Bolt (the owner's spell), one chaos rune a cast, plus two air runes
//     without a worn air staff. Magic 13-16: Fire Strike, one mind and three fire (and two air).
//     Below 13 no runes: fightBest melees and the quest may die, which the ladder's advice
//     (MinCombat 30) is there to prevent. Betty's in Port Sarim sells all of them.
//   - Food: sharks and lobsters only from the bank or the mule (never bought), and the East
//     Ardougne baker's cakes, which the account buys when it has nothing better. Every food line
//     is Optional; a rune line whose shop fails fails the kit, and the keep is then tried anyway
//     (decide goes on once the kit has run or failed), melee being fightBest's fallback.
func merlinFightKit(s Snapshot) []KitLine {
	kit := []KitLine{
		{ID: 385, Name: "Shark", Qty: 10, Optional: true},
		{ID: 379, Name: "Lobster", Qty: 6, Optional: true},
		// The baker stocks three cakes (ardougne_east.inv, shared), so its eight chocolate slices
		// come with them (not its bread: bread is the beggar's quest item): the questproof EMPTY
		// lab run merlin-empty-3 bought the three cakes alone, ate all nine bites mid-fight at 20
		// Hitpoints against Mordred and the Renegade Knights, and died with the fight unfinished.
		{ID: 1891, Name: "Cake", Qty: 3, Optional: true, Sources: []Source{{Kind: SourceShop, Shop: ArdougneBaker, Price: 50}}},
		{ID: objChocolateSlice, Name: "Chocolate slice", Qty: 8, Optional: true, Sources: []Source{{Kind: SourceShop, Shop: ArdougneBaker, Price: 30}}},
	}
	betty := func(price int) []Source { return []Source{{Kind: SourceShop, Shop: BettyShop, Price: price}} }
	air := 0
	switch magic := s.Level("magic"); {
	case magic >= 17:
		kit = append(kit, KitLine{ID: objChaosRune, Name: "Chaos rune", Qty: merlinCasts, Stackable: true, Sources: betty(15)})
		air = 2
	case magic >= 13:
		kit = append(kit,
			KitLine{ID: objMindRune, Name: "Mind rune", Qty: merlinCasts, Stackable: true, Sources: betty(3)},
			KitLine{ID: objFireRune, Name: "Fire rune", Qty: 3 * merlinCasts, Stackable: true, Sources: betty(4)})
		air = 2
	}
	if air > 0 && !wearsAirStaff(s) {
		kit = append(kit, KitLine{ID: objAirRune, Name: "Air rune", Qty: air * merlinCasts, Stackable: true, Sources: betty(4)})
	}
	return kit
}

// MerlinOddsParkFor is how long an account whose pack cannot finish Sir Mordred skips the quest: Betty's
// chaos runes and the mule's answer are what mend it, and neither changes inside the hour.
const MerlinOddsParkFor = 3 * time.Hour

// merlinSustain is the quest's survival policy (see MerlinsCrystal).
var merlinSustain = Sustain{FoodIDs: MerlinFoodIDs, EatAt: 12}

// MerlinVerdict is MerlinMordredReady's answer.
type MerlinVerdict struct {
	OK  bool
	Why string
	// Casts is how many casts the runes pay for, in the order they are taken, Odds the fight played out.
	Casts string
	Odds  FightOdds
}

// MerlinMordredReady judges whether the pack can deal Sir Mordred his 38 hitpoints (content heals him to full
// at his death, so one kill is all the quest needs) and stand up to what he does meanwhile: the casts the
// runes pay for, then the account's own swing, against a foe who hits up to 5 every four ticks (attack bonus 20,
// strength bonus 16), with the food the pack holds. It is FightChain over one foe (fightodds.go).
//
// WHY: 16 Oct 2026 01:14-01:18, an account whose fight kit had failed (Betty had no chaos rune, the mule ask was
// spent: "provisioning merlin-fight failed") went up the stairs with a handful of chaos runes and 894 fire
// runes, cast three Fire Bolts, and meleed him for three minutes at 8-12 of 53 hitpoints with the Attack of a
// caster, eating 20 bites of 4 and 5 until there were none. Its swing against his defence bonus of 45 takes
// about an hour to deal 38. The gate refuses that pack before the crate ride.
func MerlinMordredReady(s Snapshot) MerlinVerdict {
	odds := FightChain(s, []MeleeFoe{FoeMordred}, merlinSustain, 0)
	v := MerlinVerdict{OK: odds.OK, Why: odds.Why, Odds: odds}
	if s.MaxHP > 0 && s.Level("magic") > 0 {
		v.Casts = SpellsOf(CastPlan(s.InvIDs, s.WornIDs, s.Level("magic"), 400))
		if !v.OK {
			v.Why = fmt.Sprintf("%s (the runes pay for: %s; Attack %d, Strength %d, Defence %d, %d hitpoints, %d pieces of food)",
				v.Why, v.Casts, s.Level("attack"), s.Level("strength"), s.Level("defence"), s.MaxHP, len(FoodBites(s.InvIDs, MerlinFoodIDs)))
		}
	}
	return v
}

// DecideMerlinsCrystal is the whole quest.
func DecideMerlinsCrystal(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: ArthurStop}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	inKeep := s.TileKnown && InsideKeep(s.Tile)
	held := func(id int) bool { return s.HeldID(id) > 0 }
	owned := func(id int) bool { return s.Owned(id) > 0 }
	switch s.Stage {
	case MerlinStarted:
		return Step{Kind: StepTalk, Stop: GawainStop}
	case MerlinGawain:
		return Step{Kind: StepTalk, Stop: LancelotStop}
	case MerlinLancelot:
		if !inKeep && !s.Ran["provision:"+KitMerlinFight] && !s.Failed["provision:"+KitMerlinFight] {
			return Step{Kind: StepProvision, KitName: KitMerlinFight, Kit: merlinFightKit(s),
				KeepIDs: append(append(append([]int(nil), merlinTools...), MerlinFoodIDs...), merlinFightRunes...), Bank: CatherbyBank, BankSet: true,
				Reason: "runes and food for Sir Mordred (38 HP, max 5, magic defence 1) and the Renegade Knights"}
		}
		// A fight kit that failed and left no food in the pack is not a fight: the questproof
		// CONTROL lab run merlin-control-1 (provisioning off) walked into the keep with nothing
		// and died to Sir Mordred and the Renegade Knights until the death cap. Live, an account
		// whose kit cannot be bought would do the same, so it waits here, and the wait parks.
		if !inKeep && s.Failed["provision:"+KitMerlinFight] && !merlinHoldsFood(s) {
			return Step{Kind: StepWait, Reason: "no food for Sir Mordred: the fight kit could not be provisioned"}
		}
		// The pack has to be able to finish him, kit or no kit: the failed kit is exactly the pack that cannot
		// (live 16 Oct 2026, three Fire Bolts and then a mage's swing until the food ran out). Outside, the quest
		// waits and parks; inside, it walks out.
		if v := MerlinMordredReady(s); !v.OK {
			if inKeep {
				return Step{Kind: StepCustom, Custom: CustomMerlinLeaveKeep, Reason: "the pack cannot finish Sir Mordred: " + v.Why}
			}
			return Step{Kind: StepWait, ParkFor: MerlinOddsParkFor, Reason: "not going up to Sir Mordred with this pack: " + v.Why}
		}
		return Step{Kind: StepCustom, Custom: CustomMerlinKeep, Reason: "the Catherby crate, the keep's stairs, Sir Mordred, Morgan's brief"}
	case MerlinCrystalGone:
		if inKeep {
			return Step{Kind: StepCustom, Custom: CustomMerlinLeaveKeep}
		}
		return Step{Kind: StepTalk, Stop: ArthurStop}
	case MerlinBound:
		if !owned(ObjExcalibur) {
			// Lost after the bind: the Lady's test again.
			return merlinExcaliburStep(s)
		}
		if !held(ObjExcalibur) {
			return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Excalibur", ID: ObjExcalibur, Qty: 1}}, Bank: CatherbyBank, BankSet: true}
		}
		return Step{Kind: StepCustom, Custom: CustomMerlinCrystal, Reason: "Excalibur on the Giant crystal"}
	case MerlinMorgan, MerlinWordsKnown:
		if inKeep {
			return Step{Kind: StepCustom, Custom: CustomMerlinLeaveKeep}
		}
		// The words first, and into the Wilderness carrying only coins and food: an account
		// that dies there loses all but three items, and the altar's Check needs nothing held.
		// Every item the summon needs is gathered after, so nothing gathered is risked.
		if s.Stage == MerlinMorgan {
			if !WildernessSafe(s) {
				return Step{Kind: StepCustom, Custom: CustomMerlinStrip, Reason: "bank everything but coins and food before the Wilderness"}
			}
			return Step{Kind: StepCustom, Custom: CustomMerlinAltar, Reason: "the Wilderness chaos altar's words"}
		}
		if !s.Ran["provision:"+KitMerlin] && !s.Failed["provision:"+KitMerlin] {
			return Step{Kind: StepProvision, KitName: KitMerlin, Kit: merlinKit(s),
				KeepIDs: append(append(append([]int(nil), merlinTools...), merlinFightRunes...), 1381),
				Bank:    CatherbyBank, BankSet: true, Reason: "bread, repellent, bucket, tinderbox, and bat bones if banked"}
		}
		if !held(ObjBatBones) {
			return Step{Kind: StepCustom, Custom: CustomMerlinBat, Reason: "a giant bat by the keep's front door"}
		}
		if !held(ObjLitBlackCandle) {
			switch {
			case held(ObjBlackCandle) && held(ObjTinderbox):
				return Step{Kind: StepUseOn, ItemID: ObjTinderbox, Item: "Tinderbox", TargetKind: TargetItem,
					TargetID: ObjBlackCandle, Target: "Black candle"}
			case held(ObjBlackCandle):
				return ShopItemStep(s, "merlin-tinderbox", ObjTinderbox, "Tinderbox", 1, ArheinStore, 1, merlinTools)
			case held(ObjBucketOfWax):
				return Step{Kind: StepTalk, Stop: CandleMakerStop, UntilItemID: ObjBlackCandle}
			case held(ObjInsectRepel) && held(ObjBucket):
				return Step{Kind: StepCustom, Custom: CustomMerlinWax, Reason: "repellent, then the bucket, on a Catherby beehive"}
			case !held(ObjBucket):
				return ShopItemStep(s, "merlin-bucket", ObjBucket, "Bucket", 1, ArheinStore, 2, merlinTools)
			default:
				return Step{Kind: StepGrabGround, ItemID: ObjInsectRepel, Item: "Insect repellent", Anchor: RepellentSpawn, Radius: 70}
			}
		}
		if !owned(ObjExcalibur) {
			return merlinExcaliburStep(s)
		}
		if s.Stage != MerlinWordsKnown {
			return Step{Kind: StepCustom, Custom: CustomMerlinAltar, Reason: "the Wilderness chaos altar's words"}
		}
		return Step{Kind: StepCustom, Custom: CustomMerlinSummon, Reason: "bat bones dropped on the magic symbol, the lit candle held"}
	}
	return Step{Kind: StepWait, Reason: "merlin: unrecognised stage"}
}

func merlinExcaliburStep(s Snapshot) Step {
	if s.HeldID(ObjBread) == 0 {
		return ShopItemStep(s, "merlin-bread", ObjBread, "Bread", 1,
			ShopRef{Npc: "Baker", NpcID: 571, Anchor: tile.Tile{X: 2669, Z: 3310}}, 12, merlinTools)
	}
	return Step{Kind: StepCustom, Custom: CustomMerlinExcalibur, Reason: "the Lady of the Lake's test, then the beggar at the jeweller's door"}
}

// MerlinsCrystalRecord is the quest's identity (no server gates).
var MerlinsCrystalRecord = Record{ID: MerlinsCrystalID, Name: MerlinsCrystalName, QuestPoints: 6}

// MerlinsCrystal is the engine module.
var MerlinsCrystal = Module{
	Record: MerlinsCrystalRecord, Bank: CatherbyBank, BankSet: true, Anchor: CatherbyBank,
	ToolIDs:   merlinTools,
	ReadStage: ParseMerlinsCrystalJournal,
	Sustain:   merlinSustain,
	// One death skips the quest for a day: Off Da Chain died leaving the keep twice in 20 minutes (6-7 Oct 2026).
	MaxDeaths: 1,
	Decide:    DecideMerlinsCrystal,
}

// WildernessSafe reports a pack of only coins and food and nothing worn: what may go into the
// Wilderness for the chaos altar.
// objChocolateSlice is the East Ardougne baker's chocolate slice (content cakes.obj).
const objChocolateSlice = 1901

// merlinHoldsFood reports whether the pack holds any of the quest's foods.
func merlinHoldsFood(s Snapshot) bool {
	for _, f := range MerlinFoodIDs {
		if s.HeldID(f) > 0 {
			return true
		}
	}
	return false
}

func WildernessSafe(s Snapshot) bool {
	for _, n := range s.WornIDs {
		if n > 0 {
			return false
		}
	}
	food := map[int]bool{}
	for _, f := range MerlinFoodIDs {
		food[f] = true
	}
	for id, n := range s.InvIDs {
		if n > 0 && id != CoinsID && !food[id] {
			return false
		}
	}
	return true
}
