package questplan

import (
	"regexp"
	"strconv"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Sheep Shearer (content id `sheep`, %sheep varp: 0 not started, 1 started, +1 for every ball Fred has taken, 20 with 19
// taken, 22 complete; not transmitted), ported from the TS def (defs/sheepshearer.ts, exec/wool.ts) and checked against
// content 289: areas/area_lumbridge/scripts/fred_the_farmer.rs2, quests/quest_sheep/scripts/quest_sheep.rs2 and
// sheep_journal.rs2. Fred the Farmer wants 20 balls of wool, taken one by one (fred_give_wool: while the pack holds a ball
// and %sheep is not 20, delete one, %sheep + 1, one tick apart; the 20th is the one that completes: queue(sheep_complete)
// deletes it and pays 60 coins and 150 Crafting XP at 1x). He takes a partial delivery, so the quest survives a run that
// ends part way, and the journal says how many are still owed ("I need to collect N more balls of wool", N = 21 - %sheep).
// There is no fight, no skill gate and nothing the quest hands out, so no death is possible by design.
//
// Where the 20 balls come from, in the owner's item rule (provision.go): the account's own bank, the mule, then Aemad's
// Adventurers' Store in East Ardougne, which stocks 30 balls of wool at 2 gp (content ardougne_east.inv [adventurershop]) and
// is the fleet's own home town. The shop is listed only for an account that stands nearer to it than to Fred (ShopOnTheWay):
// from Lumbridge it is 850 steps away, and the module's own work there is a few minutes. That one provisioning pass runs
// first, before the quest is even started, so an account in Ardougne buys its wool where it stands and walks to Fred once.
// Only when that gives nothing does the module make them, as the Crafting rung does: shears (Fred's farm floor, else the
// general store), the Lumbridge pen's sheep (shear_sheep.rs2: one wool a shearing, a quarter of the attempts fail), and the
// castle's spinning wheel, on crafterrunner's proven legs.

// SheepShearerName is the quest tab's spelling (component 7344, `questlist:sheep`).
const SheepShearerName = "Sheep Shearer"

// SheepShearerID is the engine's id.
const SheepShearerID = "sheep"

// SheepBalls is how many balls of wool Fred wants in all (^sheep_last_wool).
const SheepBalls = 20

// The %sheep stages (quest_sheep.constant and general/configs/quest.constant).
const (
	SheepStarted  = 1
	SheepLastWool = 20 // 19 balls taken: the next one completes the quest
	SheepComplete = 22
)

// Ids from content/pack (checked against the production config by TestCookSheepIdsMatchTheProductionConfig).
const (
	NpcFred        = 758  // [fred_the_farmer]
	NpcWoollySheep = 43   // [sheepunsheered]; 42 is [sheepsheered]
	ObjShears      = 1735 // [shears]
	ObjWool        = 1737 // [wool]
	ObjBallOfWool  = 1759 // [ball_of_wool]
)

// Tiles from content maps (m49_51, m50_51) and the shops' keepers.
var (
	// FredAnchor is Fred the Farmer, outside the sheep pen beside his farmhouse.
	FredAnchor = tile.Tile{X: 3189, Z: 3273}
	// ShearsFarm is the free pair of shears that respawns on the farm floor (m49_51, 3152,3306).
	ShearsFarm = tile.Tile{X: 3152, Z: 3306}
	// AemadStore is Aemad of the East Ardougne Adventurers' Store (npc 590, 2613,3294).
	AemadStore = ShopRef{Npc: "Aemad", NpcID: 590, Anchor: tile.Tile{X: 2613, Z: 3294}}
)

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomSheepWool   = "sheep-wool"
	CustomSheepHandIn = "sheep-hand-in"

	KitSheepBalls  = "sheep-balls"
	KitSheepShears = "sheep-shears"
)

// FredStop starts the quest and takes the balls. The first menu is "I'm looking for a quest." / "I'm looking for something
// to kill." / "I'm lost.", the second (three pages later) "Yes okay. I can do that." / "That doesn't sound a very exciting
// quest." / "What do you mean, The Thing?". The hand-in has no menu, but its balls go one at a time with the dialogue shut
// between them: the module drives it with CustomSheepHandIn, not a plain talk.
var FredStop = NpcStop{Npc: "Fred the Farmer", ID: NpcFred, Anchor: FredAnchor, Leash: 6,
	Prefer: []string{"I'm looking for a quest.", "Yes okay. I can do that."}}

// sheepKeep is what a bank visit of this quest leaves in the pack.
var sheepKeep = []int{ObjShears, ObjWool, ObjBallOfWool, CoinsID}

// SheepKeepIDs returns the keep list (a copy).
func SheepKeepIDs() []int { return append([]int(nil), sheepKeep...) }

var sheepOwedRe = regexp.MustCompile(`i need to collect (\d+) more balls? of wool`)

// ParseSheepShearerJournal reads %sheep from sheep_journal.rs2's rendered text. In progress the journal gives the balls
// still owed, which is 21 - %sheep.
func ParseSheepShearerJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	switch {
	case text == "":
		return 0, false
	case strings.Contains(text, "quest complete"):
		return SheepComplete, true
	case strings.Contains(text, "i can start this quest by speaking to farmer fred"):
		return 0, true
	case strings.Contains(text, "i have collected all 20 balls of wool"):
		return SheepLastWool + 1, true
	}
	if m := sheepOwedRe.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 1 && n <= SheepBalls {
			return SheepBalls + 1 - n, true
		}
	}
	return 0, false
}

// SheepBallsOwed is how many balls Fred still wants at a journal stage: all 20 before the quest has taken any, one more
// than he has had thereafter, and at least one (the ball that completes it).
func SheepBallsOwed(stage int) int {
	if stage < SheepStarted {
		return SheepBalls
	}
	if n := SheepBalls + 1 - stage; n >= 1 {
		return n
	}
	return 1
}

// sheepBallsKit is the balls the bank, the mule or (for an account nearer to it than to Fred) Aemad's shelf can give. The
// line is optional: the module makes what they cannot, so a short bank or an empty shelf never blocks the quest.
func sheepBallsKit(s Snapshot, qty int) []KitLine {
	var src []Source
	if ShopOnTheWay(s, AemadStore, FredAnchor) {
		src = append(src, Source{Kind: SourceShop, Shop: AemadStore, Price: 2, Note: "Aemad keeps 30, East Ardougne"})
	}
	return []KitLine{{ID: ObjBallOfWool, Name: "Ball of wool", Qty: qty, Optional: true, Sources: src}}
}

// sheepShearsKit is the shears for the pen: Fred's farm floor, else the general store.
func sheepShearsKit() []KitLine {
	return []KitLine{{ID: ObjShears, Name: "Shears", Qty: 1, Sources: []Source{
		{Kind: SourceGround, Tile: ShearsFarm, PerSpawn: 1, RespawnTicks: 100, Note: "Fred's farm floor"},
		{Kind: SourceShop, Shop: LumbridgeStore, Price: 1},
	}}}
}

// DecideSheepShearer is the whole quest.
func DecideSheepShearer(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	}
	// Balls Fred still wants. Before the quest starts it is all 20, and so it is while the journal is unread: an unread
	// journal is not "nothing taken", it asks for 20, which only ever over-gathers.
	owed := SheepBalls
	if s.Journal == StatusInProgress && s.StageKnown {
		owed = SheepBallsOwed(s.Stage)
	}
	balls, wool := s.HeldID(ObjBallOfWool), s.HeldID(ObjWool)
	// The balls the pack still lacks once the wool it holds is spun.
	lacking := owed - balls - wool
	// The cheap rungs first, once, wherever the account stands and even before the quest is started: the bank and the mule
	// answer for all of it at no walk, and Aemad's shelf is listed for an account that is already in Ardougne.
	if lacking > 0 && !s.Ran["provision:"+KitSheepBalls] && !s.Failed["provision:"+KitSheepBalls] {
		return Step{Kind: StepProvision, KitName: KitSheepBalls, Kit: sheepBallsKit(s, owed-wool), KeepIDs: sheepKeep,
			Bank: cookSheepBank(s), BankSet: true, Reason: "balls of wool from the bank, the mule or Aemad's shelf"}
	}
	if s.Journal == StatusNotStarted {
		return Step{Kind: StepTalk, Stop: FredStop}
	}
	if balls >= owed {
		return Step{Kind: StepCustom, Custom: CustomSheepHandIn, Reason: "the balls of wool to Fred"}
	}
	if lacking > 0 {
		// Shear them. Shears first, then room for the wool (the shears are in the pack by then).
		if s.HeldID(ObjShears) == 0 {
			if s.Failed["provision:"+KitSheepShears] {
				return Step{Kind: StepWait, Reason: "no shears from the bank, the mule, Fred's farm or the general store"}
			}
			return Step{Kind: StepProvision, KitName: KitSheepShears, Kit: sheepShearsKit(), KeepIDs: sheepKeep,
				Bank: cookSheepBank(s), BankSet: true, Reason: "shears for the Lumbridge sheep"}
		}
		if s.FreeSlots < lacking {
			return Step{Kind: StepDeposit, KeepIDs: sheepKeep, Bank: cookSheepBank(s), BankSet: true}
		}
	}
	return Step{Kind: StepCustom, Custom: CustomSheepWool, Qty: owed - balls,
		Reason: "shear the Lumbridge pen and spin the wool at the castle wheel"}
}

// SheepShearerRecord is the quest's identity (no server gates).
var SheepShearerRecord = Record{ID: SheepShearerID, Name: SheepShearerName, QuestPoints: 1}

// SheepShearer is the engine module. MaxDeaths 1: nothing in it can kill an account, so the first death anywhere on its
// walks parks the quest for a day instead of walking back into whatever did it.
var SheepShearer = Module{
	Record: SheepShearerRecord, Bank: DraynorBank, BankSet: true, Anchor: FredAnchor,
	ToolIDs:   sheepKeep,
	ReadStage: ParseSheepShearerJournal,
	MaxDeaths: 1,
	Decide:    DecideSheepShearer,
}
