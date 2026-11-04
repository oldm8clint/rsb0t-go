package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Sheep Herder (content id `sheepherder`, varp %sheepherderquest and the bit field %sheepherdervar, 4 quest points, 3,100 coins and no
// experience), ported from the TS def (defs/sheepherder/*.ts, docs/decisions/quest-pitfalls-23.md) and checked against content 289:
// quests/quest_sheepherder (quest_sheepherder.constant, sheepherder_area.dbrow, sheepherder_sheep_data.dbrow, npcs/councillor_halgrive,
// doctor_orbon, farmer_brumty and diseased_sheep, locs/sheepherder_furnace and sheepherder_gate, sheepherder_journal.rs2).
//
// THE QUEST. Councillor Halgrive (East Ardougne, near the zoo) asks for four plague sheep to be herded into the enclosure on Farmer
// Brumty's land north of the city, killed with poisoned feed and their remains burnt in the incinerator beside it, and gives the feed.
// Doctor Orbon, in the chapel just north of Halgrive, sells the protective suit for 100 coins (a jacket and trousers, untradeable, no
// shop). The enclosure's gate lets nobody in who is not wearing both, and the sheep can only be moved by a cattle prod worn in the
// right hand; the prod lies on the barn floor inside the enclosure. A Prod sends the sheep one tile along coord_direction(player, sheep),
// away from the account; a sheep standing on the three columns west of the fence is sent over it by any prod. Feeding the poison to a
// sheep inside the enclosure kills it and leaves its remains, and using the remains on the incinerator burns them and sets the sheep's
// bits. When all four are burnt Halgrive pays 3,100 coins (the 100 back and 3,000).
//
// THE STATE. %sheepherderquest is 1 until the suit is bought, 2 until the reward and 3 after. Which sheep is done is %sheepherdervar,
// three bits a sheep at bits 1, 4, 7 and 10 (1 herded, 2 killed, 6 burnt: the kill REPLACES the herded bit), and nothing transmits
// either: the journal's prose is the only reader. Its stage here is HerdDisposing plus two bits a sheep (HerdSheepState): the journal
// prints a line per sheep (herded; killed with the remains in the pack; burnt), and a killed sheep whose remains are NOT in the pack
// reads as a herded one, which is why the pack settles what the journal cannot. When all four are burnt the page drops the per-sheep
// lines for one (HerdAllBurnt).
//
// WHAT CAN HURT. Nothing in the quest attacks anybody: there is no combat in the content at all (the sheep have no attack options
// and no combat stats). The ground the legs cover (the fields between East Ardougne and the enclosure) has no hunter on it that a
// fleet account meets: the chaos druids that hunt combat 26 and under stand 30 tiles from the nearest sheep and the pen, the warrior
// women and the mourners are not aggressive. So the module prices no fight, never retaliates and parks for a day after the first
// death anywhere on its walks.
//
// THE SHARED THINGS. The four sheep have three copies each on the map and are world npcs, the barn holds one prod that comes back
// 100 ticks after a pickup, and the sheep in the enclosure is a 150 tick npc_add at a fixed tile: a second account herding the same
// sheep type pushes the same copy the other is pushing. The quest is therefore one account at a time (questrunner slots), released
// once the fourth sheep is burnt (Module.ExclusiveDone).

// SheepHerderName is the quest tab's spelling (component 7375, `questlist:sheepherder`).
const SheepHerderName = "Sheep Herder"

// SheepHerderID is the engine's id.
const SheepHerderID = "sheepherder"

// The journal's stage here: the three %sheepherderquest values below the reward, and the disposing stage with the four sheep's
// states in its low byte.
const (
	HerdNotStarted = 0
	HerdNeedSuit   = 1
	// HerdDisposing is the stage of an account that has bought the suit: HerdDisposing plus the sheep's states, two bits a sheep (sheep 1
	// in the lowest bits).
	HerdDisposing = 256
	// HerdAllBurnt is HerdDisposing with every sheep burnt: the page that says to return to Halgrive.
	HerdAllBurnt = HerdDisposing + 0xFF
	// HerdComplete is the finished quest.
	HerdComplete = 1000
)

// A sheep's state in the journal, in the two bits HerdSheepState returns.
const (
	// HerdSheepWild is a sheep that has not been herded to the enclosure.
	HerdSheepWild = 0
	// HerdSheepPenned is a herded sheep (or a killed one whose remains are not in the pack: the journal cannot tell them apart).
	HerdSheepPenned = 1
	// HerdSheepKilled is a killed sheep whose remains the pack holds.
	HerdSheepKilled = 2
	// HerdSheepBurnt is a sheep whose remains were burnt.
	HerdSheepBurnt = 3
)

// HerdSheepState is sheep n's (1 to 4) state in a disposing stage.
func HerdSheepState(stage, n int) int {
	if stage < HerdDisposing || stage > HerdAllBurnt || n < 1 || n > 4 {
		return HerdSheepWild
	}
	return (stage - HerdDisposing) >> (2 * (n - 1)) & 3
}

// HerdStageWith is the disposing stage with the four sheep in the given states.
func HerdStageWith(states [4]int) int {
	stage := HerdDisposing
	for i, st := range states {
		stage += (st & 3) << (2 * i)
	}
	return stage
}

// Ids from content/pack (checked against the production config by TestSheepHerderIDsMatchTheProductionConfig).
const (
	NpcHalgrive        = 289 // [councillor_halgrive]
	NpcOrbon           = 290 // [doctor_orbon]
	NpcBrumty          = 291 // [farmer_brumty], inside the enclosure
	NpcPlagueSheep1    = 292 // [plaguesheep_1] .. [plaguesheep_4] are 292..295
	ObjCattleProd      = 278 // [cattleprod]: wielded in the right hand, untradeable
	ObjSheepFeed       = 279 // [poisoned_feed]: never used up, untradeable
	ObjSheepBones1     = 280 // [sheepbonesa] .. [sheepbonesd] are 280..283: all render "Bones"
	ObjPlagueJacket    = 284
	ObjPlagueTrousers  = 285
	LocPlagueFurnace   = 165 // [plaguesheep_furnace], 3x3, forceapproach=east
	LocPlagueGateLeft  = 166 // [plaguesheep_gatel] at 2594,3362
	LocPlagueGateRight = 167 // [plaguesheep_gater] at 2594,3361
)

// NpcPlagueSheep is sheep n's npc id (n 1 to 4).
func NpcPlagueSheep(n int) int { return NpcPlagueSheep1 + n - 1 }

// ObjSheepBones is the remains of sheep n (n 1 to 4).
func ObjSheepBones(n int) int { return ObjSheepBones1 + n - 1 }

// HerdSuitGP is Doctor Orbon's price for the protective suit, a jacket and a pair of trousers.
const HerdSuitGP = 100

// HerdEquipRoom is the free pack slots wanted before the suit or the prod is put on: the jacket takes the torso, the arms and the
// hands, the trousers the legs and the feet, the prod the right hand, and everything they replace goes to the pack.
const HerdEquipRoom = 6

// Tiles from content maps (the sheep's four map spawns have three copies each, within a tile of the middle one named here).
var (
	HalgriveAnchor = tile.Tile{X: 2617, Z: 3299}
	OrbonAnchor    = tile.Tile{X: 2614, Z: 3306}
	BrumtyAnchor   = tile.Tile{X: 2600, Z: 3358}
	// HerdSheepSpawns are the middle tile of each sheep's three map spawns (sheep 1 first).
	HerdSheepSpawns = [4]tile.Tile{{X: 2610, Z: 3344}, {X: 2622, Z: 3367}, {X: 2561, Z: 3389}, {X: 2611, Z: 3391}}
	// HerdPenSpots are the tiles the sheep are set down on inside the enclosure after the jump (sheepherder_sheep_data.dbrow
	// sheep_in_pen_coord, 150 ticks), sheep 1 first.
	HerdPenSpots = [4]tile.Tile{{X: 2596, Z: 3362}, {X: 2597, Z: 3363}, {X: 2597, Z: 3360}, {X: 2596, Z: 3359}}
	// HerdGateOutside is the gate's own tile west of the fence: the clothing check runs only from there (check_axis wants the account in
	// the gate's column), and the gate is a p_teleport to the tile east of it. HerdGateInside is the tile across the fence, where the
	// way out is opened from.
	HerdGateOutside = tile.Tile{X: 2594, Z: 3361}
	HerdGateInside  = tile.Tile{X: 2595, Z: 3361}
	// HerdFurnaceStand is west of the incinerator (forceapproach=east, angle 2: the only legal side in world space is the west).
	HerdFurnaceStand = tile.Tile{X: 2604, Z: 3361}
	// HerdFurnaceAnchor is the incinerator's own tile (3x3, so it spans 2605..2607, 3360..3362).
	HerdFurnaceAnchor = tile.Tile{X: 2605, Z: 3360}
	// HerdProdSpawn is the barn's prod, inside the enclosure.
	HerdProdSpawn = tile.Tile{X: 2604, Z: 3357}
)

// HerdPen is `sheepherder_in_pen` (0_40_52_35_23 to 49_36) and HerdGateZone is `sheepherder_pen_gate` (32_32 to 34_35).
var (
	HerdPen      = HerdBox{X0: 2595, X1: 2609, Z0: 3351, Z1: 3364}
	HerdGateZone = HerdBox{X0: 2592, X1: 2594, Z0: 3360, Z1: 3363}
)

// InHerdPen reports a tile of the enclosure.
func InHerdPen(t tile.Tile) bool { return t.Level == 0 && HerdPen.Contains(t.X, t.Z) }

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomHerdProd  = "herd-prod"
	CustomHerdSheep = "herd-sheep"
	CustomHerdBurn  = "herd-burn"

	KitHerdCoins = "herd-coins"
	KitHerdGear  = "herd-gear"
)

// Parks.
const (
	// HerdLegPark is how long the quest skips an account whose leg failed: a sheep that could not be herded, a prod that was not there,
	// remains that would not burn. The legs are bounded by pushes and by minutes, so this is a failure, not a wait.
	HerdLegPark = 30 * time.Minute
	// HerdCoinsPark is how long it skips an account with no 100 coins in its bank or at the mule.
	HerdCoinsPark = 3 * time.Hour
)

// herdKeep is what every bank visit of the quest leaves in the pack: the coins, the suit and the prod (untradeable: the between-run
// dump banks them, and a banked one is drawn again by the gear kit), the feed and the remains.
var herdKeep = func() []int {
	out := []int{CoinsID, ObjPlagueJacket, ObjPlagueTrousers, ObjCattleProd, ObjSheepFeed}
	for n := 1; n <= 4; n++ {
		out = append(out, ObjSheepBones(n))
	}
	return out
}()

// HerdKeepIDs returns the keep list (a copy).
func HerdKeepIDs() []int { return append([]int(nil), herdKeep...) }

// The stops. The talks with no menu carry a placeholder preference (a talk step must name one).
var (
	// HalgriveStop starts the quest ("Hello. How are you?" then "What's wrong?" / "That's life for you.", then "I can do that for you."
	// / "That's not a job for me."), gives another feed when the pack has none (no menu: "Some more sheep poison would be
	// appreciated...") and pays the reward (no menu).
	HalgriveStop = NpcStop{Npc: "Councillor Halgrive", ID: NpcHalgrive, Anchor: HalgriveAnchor, Leash: 8,
		Prefer: []string{"What's wrong?", "I can do that for you."}}
	// OrbonStop sells the suit for 100 coins at stage 1 ("Ok, I'll take it." / "Sorry doc, that's too much.") and again, when the pack
	// and the worn slots hold no complete suit, at stage 2 ("Ok, I'll take it." / "No, I don't need anymore.").
	OrbonStop = NpcStop{Npc: "Doctor Orbon", ID: NpcOrbon, Anchor: OrbonAnchor, Leash: 8, Prefer: []string{"Ok, I'll take it."}}
)

// herdMarkers are the journal's pages that settle the stage outside the disposing page, newest first.
var herdMarkers = []struct {
	stage int
	text  string
}{
	{HerdComplete, "quest complete!"},
	{HerdNotStarted, "i can start this quest by speaking to"},
	{HerdNeedSuit, "i need to get some protective clothing"},
}

var herdOrdinals = [4]string{"first", "second", "third", "fourth"}

// ParseSheepHerderJournal reads the stage from the rendered journal. ok is false for an empty or unrecognised page, which is unknown,
// never stage 0.
func ParseSheepHerderJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	for _, m := range herdMarkers {
		if strings.Contains(text, m.text) {
			return m.stage, true
		}
	}
	if !strings.Contains(text, "i bought some protective clothing") {
		return 0, false
	}
	// The last page drops the four per-sheep lines for one summary, so its own line is the only evidence all four are burnt.
	if strings.Contains(text, "i should return to") {
		return HerdAllBurnt, true
	}
	var states [4]int
	for i, ord := range herdOrdinals {
		switch {
		case strings.Contains(text, "killed the "+ord+" sheep and incinerated its bones"):
			states[i] = HerdSheepBurnt
		case strings.Contains(text, "killed the "+ord+" sheep. now i must incinerate"):
			states[i] = HerdSheepKilled
		case strings.Contains(text, "herded the "+ord+" sheep to the pen"):
			states[i] = HerdSheepPenned
		}
	}
	return HerdStageWith(states), true
}

// HerdNextSheep is the first sheep (1 to 4) of a disposing stage that is not burnt, 0 when all four are.
func HerdNextSheep(stage int) int {
	for n := 1; n <= 4; n++ {
		if HerdSheepState(stage, n) != HerdSheepBurnt {
			return n
		}
	}
	return 0
}

// HerdPastTheSheep reports that the account is past the quest's one-at-a-time work: every sheep burnt, or the quest done.
func HerdPastTheSheep(s Snapshot) bool {
	return s.Journal == StatusComplete || (s.StageKnown && s.Stage >= HerdAllBurnt)
}

// herdCoinsKit is the purse for the suit: the bank and the mule are the only sources of coins.
func herdCoinsKit() []KitLine {
	return []KitLine{{ID: CoinsID, Name: "Coins", Qty: HerdSuitGP, Stackable: true}}
}

// herdGearKit draws whatever the between-run bank dump banked of the quest's own items, once per run: the suit, the prod, the feed and
// the four sets of remains. All quest items (never asked of the mule or bought), all optional, so a bank without them costs only the
// bank read.
func herdGearKit() []KitLine {
	lines := []KitLine{
		{ID: ObjPlagueJacket, Name: "Plague jacket", Qty: 1, Untradeable: true, Optional: true, Worn: true},
		{ID: ObjPlagueTrousers, Name: "Plague trousers", Qty: 1, Untradeable: true, Optional: true, Worn: true},
		{ID: ObjCattleProd, Name: "Prod", Qty: 1, Untradeable: true, Optional: true, Worn: true},
		{ID: ObjSheepFeed, Name: "Feed", Qty: 1, Untradeable: true, Optional: true},
	}
	for n := 1; n <= 4; n++ {
		lines = append(lines, KitLine{ID: ObjSheepBones(n), Name: "Bones", Qty: 1, Untradeable: true, Optional: true})
	}
	return lines
}

// herdWearing reports a piece worn.
func herdWearing(s Snapshot, id int) bool { return s.WornIDs[id] > 0 }

// herdHas reports a piece worn or in the pack.
func herdHas(s Snapshot, id int) bool { return herdWearing(s, id) || s.HeldID(id) > 0 }

// herdEquip is the step that puts one piece on, behind the room it needs.
func herdEquip(s Snapshot, id int, name string) Step {
	if s.FreeSlots < HerdEquipRoom {
		return Step{Kind: StepDeposit, KeepIDs: herdKeep, Bank: EastArdougneNorth, BankSet: true,
			Reason: "room for what the protective suit and the prod replace"}
	}
	return Step{Kind: StepEquip, Item: name, ItemID: id, Reason: "put on " + name}
}

// decideHerdSuit is stage 1: the 100 coins, then Doctor Orbon.
func decideHerdSuit(s Snapshot) Step {
	if s.HeldID(CoinsID) >= HerdSuitGP {
		return Step{Kind: StepTalk, Stop: OrbonStop, Reason: "buy the protective suit"}
	}
	return herdCoins(s)
}

// herdCoins is the provisioning of the 100 coins the suit costs, a park when the bank and the mule have none.
func herdCoins(s Snapshot) Step {
	if s.Blocked["provision:"+KitHerdCoins] {
		return Step{Kind: StepWait, ParkFor: HerdCoinsPark, Reason: fmt.Sprintf("no %d coins for Doctor Orbon's protective suit from the bank or the mule", HerdSuitGP)}
	}
	return Step{Kind: StepProvision, KitName: KitHerdCoins, Kit: herdCoinsKit(), KeepIDs: herdKeep, Bank: EastArdougneNorth, BankSet: true,
		Reason: "the coins for Doctor Orbon's protective suit"}
}

// decideHerdDisposing is stage 2: the suit, any remains to burn, the prod, the feed and the next sheep.
func decideHerdDisposing(s Snapshot) Step {
	n := HerdNextSheep(s.Stage)
	if n == 0 {
		return Step{Kind: StepTalk, Stop: HalgriveStop, Reason: "Councillor Halgrive's reward"}
	}
	suitMissing := !herdHas(s, ObjPlagueJacket) || !herdHas(s, ObjPlagueTrousers)
	prodMissing := !herdHas(s, ObjCattleProd)
	feedMissing := s.HeldID(ObjSheepFeed) == 0
	lostRemains := false
	for i := 1; i <= 4; i++ {
		if HerdSheepState(s.Stage, i) == HerdSheepPenned && s.HeldID(ObjSheepBones(i)) == 0 {
			lostRemains = true
		}
	}
	// What the between-run bank dump may have banked, drawn once a run before anything is bought or fetched again.
	if (suitMissing || prodMissing || feedMissing || lostRemains) && !s.Ran["provision:"+KitHerdGear] && !s.Failed["provision:"+KitHerdGear] {
		return Step{Kind: StepProvision, KitName: KitHerdGear, Kit: herdGearKit(), KeepIDs: herdKeep, Bank: EastArdougneNorth, BankSet: true,
			Reason: "the suit, the prod, the feed and the remains the bank may hold"}
	}
	if suitMissing {
		if s.HeldID(CoinsID) < HerdSuitGP {
			return herdCoins(s)
		}
		return Step{Kind: StepTalk, Stop: OrbonStop, Reason: "buy the protective suit again"}
	}
	// The incinerator and the herd are inside the enclosure, whose gate reads the clothing worn: the suit comes first.
	if !herdWearing(s, ObjPlagueJacket) {
		return herdEquip(s, ObjPlagueJacket, "Plague jacket")
	}
	if !herdWearing(s, ObjPlagueTrousers) {
		return herdEquip(s, ObjPlagueTrousers, "Plague trousers")
	}
	// Remains in the pack are burnt before another sheep is touched (a sheep killed out of turn).
	for i := 1; i <= 4; i++ {
		if s.HeldID(ObjSheepBones(i)) > 0 && HerdSheepState(s.Stage, i) != HerdSheepBurnt {
			if s.Failed[CustomHerdBurn] {
				return Step{Kind: StepWait, ParkFor: HerdLegPark, Reason: "the incinerator would not take the sheep's remains"}
			}
			return Step{Kind: StepCustom, Custom: CustomHerdBurn, Qty: i, Reason: fmt.Sprintf("burn the remains of sheep %d in the incinerator", i)}
		}
	}
	if !herdWearing(s, ObjCattleProd) {
		if s.HeldID(ObjCattleProd) > 0 {
			return herdEquip(s, ObjCattleProd, "Prod")
		}
		if s.Failed[CustomHerdProd] {
			return Step{Kind: StepWait, ParkFor: HerdLegPark, Reason: "no prod on the barn floor inside the enclosure, or the gate did not let the account in"}
		}
		return Step{Kind: StepCustom, Custom: CustomHerdProd, Reason: "fetch the prod from the barn inside the enclosure"}
	}
	if feedMissing {
		return Step{Kind: StepTalk, Stop: HalgriveStop, Reason: "Halgrive's poisoned sheep feed"}
	}
	if s.Failed[CustomHerdSheep] {
		return Step{Kind: StepWait, ParkFor: HerdLegPark, Reason: fmt.Sprintf("sheep %d would not be herded into the enclosure and fed (a lost sheep, a trap, an empty field)", n)}
	}
	return Step{Kind: StepCustom, Custom: CustomHerdSheep, Qty: n, Reason: fmt.Sprintf("herd sheep %d into the enclosure, poison it and take its remains", n)}
}

// DecideSheepHerder is the whole quest, read from the journal's stage and the pack.
func DecideSheepHerder(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: HalgriveStop, Reason: "start Sheep Herder"}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	switch {
	case s.Stage == HerdNotStarted:
		return Step{Kind: StepTalk, Stop: HalgriveStop, Reason: "start Sheep Herder"}
	case s.Stage == HerdNeedSuit:
		return decideHerdSuit(s)
	case s.Stage >= HerdDisposing && s.Stage <= HerdAllBurnt:
		return decideHerdDisposing(s)
	case s.Stage == HerdComplete:
		return Step{Kind: StepDone}
	}
	return Step{Kind: StepWait, Reason: fmt.Sprintf("unrecognised Sheep Herder stage %d", s.Stage)}
}

// SheepHerderRecord is the quest's identity. The server gates nothing, and nothing in it fights.
var SheepHerderRecord = Record{ID: SheepHerderID, Name: SheepHerderName, QuestPoints: 4}

// SheepHerder is the engine module. Nothing attacks anybody in it (NoRetaliate), so the first death anywhere on its walks parks it
// for a day; the slot is given up once the fourth sheep is burnt.
var SheepHerder = Module{
	Record: SheepHerderRecord, Bank: EastArdougneNorth, BankSet: true, Anchor: HalgriveAnchor,
	ToolIDs:       herdKeep,
	ReadStage:     ParseSheepHerderJournal,
	NoRetaliate:   true,
	MaxDeaths:     1,
	ExclusiveDone: HerdPastTheSheep,
	Decide:        DecideSheepHerder,
}
