package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Black Knights' Fortress (content id `blackknight`, %spy, not transmitted: the quest tab's colour and the journal are what the
// client sees), ported from the TS def (defs/blackknight.ts) and checked against content 289: quests/quest_blackknight
// (quest_blackknight.rs2, fortress_guard.rs2, blackknight_journal.rs2, quest_blackknight.npc), areas/area_falador/scripts/
// sir_amik_varze.rs2 and the map square m47_54.
//
// Sir Amik Varze, in the White Knights' Castle in Falador, gives the quest to an account with 12 quest points ("I seek a quest!",
// "I laugh in the face of danger!", stage 1). The account is to get into the Black Knights' Fortress on Ice Mountain in a
// disguise (an iron chainbody and a bronze medium helmet WORN, which is all the front door looks at), listen at a grill to a
// witch and a knight (stage 2: the witch is brewing an invincibility potion and wants a cabbage from Draynor Manor), and drop
// an ordinary cabbage down the hole above her cauldron (stage 3: "You hear the witch groan in dismay."). Sir Amik pays 2,500
// coins and 3 quest points when it is told (stage 4). The TS def kept "I have listened" in a module variable, which a restart
// forgets; here the journal stage says it.
//
// The ingredients, in the owner's item rule: the account's own bank, then the mule (on 17 Oct 2026 it held 348 bronze med helms
// and one cabbage, no iron chainbody), then the shops (Wayne's chain mail shop in Falador and Horvik's in Varrock stock iron chainbodies;
// Peksa's helmet shop in Barbarian Village the helm), and the cabbage from the field south of Falador (loc 1161 "cabbage", Pick,
// 70 of them in the farm at 3044..3066 x 3291..3297) when neither bank nor mule has one. The magic cabbage (obj 1967, the
// Draynor Manor one) is the WRONG one: "You are meant to be hindering the witch. Not helping her."
//
// THE HARD PART IS THE WALK (blackknightsroute.go): the fortress is a loop of ladders and doors in which the grill and the hole
// are in different halves, joined only by the banquet hall, and fifteen aggressive level-33 knights (a hit of up to 4, attack
// rate 5) stand about it. The module wears the disguise, takes food, walks the account to the front door round the three
// sentries in the open ground, runs the crossings of the route one by one (never fights: Module.NoRetaliate), and leaves
// by the same hall. It is gated on hitpoints and food against a priced number of knight attacks (BKFit); an account the
// knights do not hunt (combat above 66, outside the Wilderness) needs far less. A death parks the quest for a day.

// BlackKnightsFortressName is the quest tab's spelling (component 7332, `questlist:fortress`): "Knight's", not "Knights'".
const BlackKnightsFortressName = "Black Knight's Fortress"

// BlackKnightsFortressID is the engine's id.
const BlackKnightsFortressID = "blackknight"

// The %spy stages (general/configs/quest.constant: ^blackknight_complete = 4).
const (
	BKNotStarted = 0
	// BKStarted: Sir Amik has sent the account to the fortress.
	BKStarted = 1
	// BKListened: the grill has been listened at.
	BKListened = 2
	// BKSabotaged: the cabbage is in the cauldron; Sir Amik pays.
	BKSabotaged = 3
	BKComplete  = 4
)

// BKMinQuestPoints is what Sir Amik asks ("You need at least 12 quest points before you may attempt this quest").
const BKMinQuestPoints = 12

// Ids from content/pack (checked against the production config by TestBlackKnightsFortressIDsMatchContentAndTheProductionConfig).
const (
	NpcSirAmik               = 608 // [sir_amik_varze], the White Knights' Castle's top floor
	NpcFortressGuard         = 609 // [fortressguard], level 20, Talk-to and Attack, not aggressive
	NpcBlackKnight           = 178 // [black_knight], level 33, not aggressive
	NpcAggressiveBlackKnight = 179 // [aggressive_black_knight], level 33, huntmode cowardly, huntrange 5
	NpcWayne                 = 581 // Wayne's Chains, Falador
	NpcHorvik                = 549 // Horvik's armour shop, Varrock
	NpcPeksa                 = 538 // Peksa's helmet shop, Barbarian Village

	ObjIronChainbody = 1101
	ObjBronzeMedHelm = 1139
	ObjMagicCabbage  = 1967 // the wrong one
	ObjBass          = 365

	// LocCabbage is the pickable cabbage ([oploc2,cabbage], "You pick a cabbage.", back after 70 ticks).
	LocCabbage = 1161
)

// Tiles from content maps (m46_52 for Amik, m47_51 for the field, shop keepers from the shop stands the fleet already uses).
var (
	// SirAmikAnchor is Sir Amik Varze on the White Knights' Castle's top floor (m46_52).
	SirAmikAnchor = t3(2962, 3338, 2)
	// CabbageField is the middle of the farm's cabbage rows south of Falador.
	CabbageField = t3(3055, 3294, 0)
	// WayneShop sells iron chainbodies in Falador (falador.inv [chainshop]: stock 2).
	WayneShop = ShopRef{Npc: "Wayne", NpcID: NpcWayne, Anchor: t3(2973, 3312, 0)}
	// HorvikShop is the Varrock armour shop (varrock.inv [armourshop]: stock 3).
	HorvikShop = ShopRef{Npc: "Horvik", NpcID: NpcHorvik, Anchor: t3(3229, 3438, 0)}
	// PeksaShop sells bronze med helms (barbarian_village.inv [helmetshop]: stock 5).
	PeksaShop = ShopRef{Npc: "Peksa", NpcID: NpcPeksa, Anchor: t3(3076, 3429, 0)}
)

// Kit names and custom step names.
const (
	KitBlackKnight = "blackknight"

	CustomBKSpy      = "bk-spy"
	CustomBKSabotage = "bk-sabotage"
	CustomBKLeave    = "bk-leave"
)

// How long the quest is skipped when a step is a dead end the account cannot get past by itself.
const (
	// BKQuestPointRetry: under 12 quest points. The ladder runs the fillers and the proven quests first, so a day is not long.
	BKQuestPointRetry = 6 * time.Hour
	// BKFitRetry: too few hitpoints or too little food for the knights. Hitpoints come slowly.
	BKFitRetry = 12 * time.Hour
	// BKDisguiseRetry: no iron chainbody or bronze med helm from the bank, the mule or a shop. The shops restock one in five
	// minutes and the mule's world errand delivers to the bank.
	BKDisguiseRetry = 3 * time.Hour
	// BKRouteRetry: a crossing of the fortress would not land, or the walk to the door did not arrive. The account is taken out first,
	// then the quest waits: nothing about a ladder that refused three clicks changes in five minutes, and every minute spent among the
	// knights is a beating.
	BKRouteRetry = 6 * time.Hour
)

// BKFoods are the foods the account eats on the way, best first (the fight foods' table, shark to bread).
var BKFoods = []int{385, 373, 379, 365, 361, 329, 333, 1891, 1893, 1895, 1897, 1899, 1901, 2309}

// BKTools are the quest's own items and the coins: what every bank visit of the quest leaves in the pack. The disguise is worn
// once equipped; in the pack it is still the quest's.
var BKTools = []int{CoinsID, ObjIronChainbody, ObjBronzeMedHelm, ObjCabbage}

// bkKeep is a bank visit's keep list: the tools and the food.
func bkKeep() []int { return append(append([]int(nil), BKTools...), BKFoods...) }

// BKKeepIDs returns the keep list (a copy).
func BKKeepIDs() []int { return bkKeep() }

// bkBank is the bank nearest the account, else Falador's west bank (the castle is two streets from it).
func bkBank(s Snapshot) tile.Tile {
	if s.TileKnown {
		return NearestBank(s.Tile).Tile
	}
	return FaladorWestBank
}

// SirAmikStop starts the quest and pays the reward. His first menu is "I seek a quest!" / "I don't, I'm just looking around.",
// the second "I laugh in the face of danger!" / "I go and cower in a corner at the first sign of danger!"; at stage 3 he has no menu.
var SirAmikStop = NpcStop{Npc: "Sir Amik Varze", ID: NpcSirAmik, Anchor: SirAmikAnchor, Leash: 6,
	Prefer: []string{"I seek a quest!", "I laugh in the face of danger!"}}

// bkJournalNeedles are ParseBlackKnightsFortressJournal's phrases against NormaliseLines, newest stage first (a later stage
// strikes the earlier lines through and keeps them, so the earlier needles also match there). blackknight_journal.rs2.
var bkJournalNeedles = []struct {
	stage  int
	needle string
}{
	{BKComplete, "quest complete"},
	{BKSabotaged, "now that i have sabotaged the witch's potion"},
	{BKListened, "i eavesdropped on a witch and the black knight captain"},
	{BKStarted, "which is located on ice mountain"},
	{BKNotStarted, "i can start this quest at the white knight's castle"},
}

// ParseBlackKnightsFortressJournal reads %spy from the rendered journal. ok is false for an empty or unrecognised journal, which
// is unknown, never stage 0.
func ParseBlackKnightsFortressJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range bkJournalNeedles {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// ---------------------------------------------------------------------------------------------------------
// The knights and what the walk is priced at.
// ---------------------------------------------------------------------------------------------------------

// FoeBlackKnight is aggressive_black_knight (quests/quest_blackknight and _unpack/225/all.npc [aggressive_black_knight]): attack,
// defence and strength 25, 42 hitpoints, attackrate 5, attackbonus 18, strengthbonus 16 (a hit of up to 4), defence bonuses 73,
// 76 and 70. The module never fights one; the model prices the hits it takes while it walks.
var FoeBlackKnight = MeleeFoe{Name: "Black Knight", Attack: 25, Strength: 25, Defence: 25, Magic: 1, Hitpoints: 42,
	AttackTicks: 5, AttackBonus: 18, StrengthBonus: 16, DefenceBonus: 70}

// The knights hunt a player only when its combat level is no more than twice theirs (npc.ts HuntCheckNotTooStrong, outside the
// Wilderness): 33 * 2.
const BKHuntCeiling = 66

// BKGauntletAttacks is how many knight attacks the walk is priced at, for an account the knights hunt. The route crosses six
// rooms of knights and the banquet hall twice; a knight lands an attack every five ticks and only while it stands next to the
// account, which a door or a ladder click (two to four ticks of standing still) allows once or twice. Twelve to sixteen
// attacks is the count a bad pass gives; 16 is charged. BKImmuneAttacks is the count for an account the idle hunt ignores: the
// scripted ones only (the hall door's "I don't care", the sturdy door from the east, the hall ladder down).
const (
	BKGauntletAttacks = 16
	BKImmuneAttacks   = 6
)

// BKMinHitpoints is the least maximum Hitpoints an account the knights hunt goes in with (BKMinHitpointsImmune for one they do not).
const (
	BKMinHitpoints       = 30
	BKMinHitpointsImmune = 20
)

// bkCombat is the 2004 combat level from base levels by lower-case name, 0 when the attack or defence level is unread.
func bkCombat(levels map[string]int) int {
	lv := func(k string) float64 { return float64(levels[k]) }
	if levels["defence"] <= 0 || levels["hitpoints"] <= 0 {
		return 0
	}
	base := 0.25 * (lv("defence") + lv("hitpoints") + float64(levels["prayer"]/2))
	melee := 0.325 * (lv("attack") + lv("strength"))
	ranged := 0.325 * float64(int(lv("ranged")*1.5))
	magic := 0.325 * float64(int(lv("magic")*1.5))
	best := melee
	if ranged > best {
		best = ranged
	}
	if magic > best {
		best = magic
	}
	return int(base + best)
}

// BKImmune reports whether the account's combat level is above the hunt ceiling: an unread level is not.
func BKImmune(s Snapshot) bool {
	c := bkCombat(s.Levels)
	return c > BKHuntCeiling
}

// BKGauntletDamage is the hitpoints the walk is priced to take from the knights: the attacks above, each the model's expected
// damage on this account's Defence, with the fight model's quarter on top (FightIncomingScale).
func BKGauntletDamage(s Snapshot) float64 {
	attacks := BKGauntletAttacks
	if BKImmune(s) {
		attacks = BKImmuneAttacks
	}
	p := MeleeFighter{Attack: s.Level("attack"), Strength: s.Level("strength"), Defence: max(s.Level("defence"), 1), Magic: s.Level("magic")}
	return float64(attacks) * FoePerAttack(FoeBlackKnight, p) * FightIncomingScale
}

// bkFoodHeld is the hitpoints of food the pack holds.
func bkFoodHeld(s Snapshot) int { return FoodPoolHeal(s.InvIDs, BKFoods) }

// BKFitHitpoints judges the hitpoints half of the gate, which needs no food: an unread maximum passes, as every other gate here
// treats an unread stat.
func BKFitHitpoints(s Snapshot) (bool, string) {
	if s.MaxHP <= 0 {
		return true, ""
	}
	need := BKMinHitpoints
	if BKImmune(s) {
		need = BKMinHitpointsImmune
	}
	if s.MaxHP < need {
		return false, fmt.Sprintf("not doing Black Knights' Fortress yet: fifteen level-33 knights stand about it (a hit of up to %d) and the way in and out is a gauntlet of ladders and doors (this account has %d hitpoints at combat %d, the gate is %d)",
			FoeBlackKnight.PeakHit(), s.MaxHP, bkCombat(s.Levels), need)
	}
	return true, ""
}

// BKFit is the gate before the fortress door: the hitpoints, and the hitpoints above two knight hits plus the food in the pack
// covering twice what the walk is priced at. A pack of food is the second half, so the pass that has not yet drawn its food from
// the bank asks again after it has.
func BKFit(s Snapshot) (bool, string) {
	if ok, why := BKFitHitpoints(s); !ok {
		return false, why
	}
	if s.MaxHP <= 0 {
		return true, ""
	}
	damage := BKGauntletDamage(s)
	room := float64(s.MaxHP - 2*FoeBlackKnight.PeakHit())
	if have := room + float64(bkFoodHeld(s)); have < 2*damage {
		return false, fmt.Sprintf("not going into the fortress with this pack: the knights are priced at %d hitpoints (combat %d, Defence %d) and %d hitpoints less two of their hits plus %d of food is %d, short of twice that",
			int(damage+0.5), bkCombat(s.Levels), s.Level("defence"), s.MaxHP, bkFoodHeld(s), int(have))
	}
	return true, ""
}

// ---------------------------------------------------------------------------------------------------------
// The kits.
// ---------------------------------------------------------------------------------------------------------

// bkKit is the one bundled provisioning pass: the disguise (the bank, the mule, then the shops that stock it), the cabbage the
// bank, the mule or (for an account nearer Port Sarim than the farm) Wydin's shelf may hold, and food. The cabbage and the food are
// optional, because the field makes the one and the bank's other food the other; the disguise is the quest's.
func bkKit(s Snapshot) []KitLine {
	// Wydin's food store sells the cabbage too (3 in stock, one back a minute): a shop before gathering, for an account that
	// stands nearer Port Sarim than the farm's rows (the same rule as Cook's Assistant's flour).
	var cabbage []Source
	if ShopOnTheWay(s, WydinShop, CabbageField) {
		cabbage = append(cabbage, Source{Kind: SourceShop, Shop: WydinShop, Price: 2, Note: "Wydin's food store keeps 3 cabbages, restocking one a minute"})
	}
	return []KitLine{
		{ID: ObjIronChainbody, Name: "Iron chainbody", Qty: 1, Worn: true, Sources: []Source{
			{Kind: SourceShop, Shop: WayneShop, Price: 300, Note: "Wayne's Chains keeps 2, restocking one in five minutes"},
			{Kind: SourceShop, Shop: HorvikShop, Price: 300, Note: "Horvik's keeps 3"}}},
		{ID: ObjBronzeMedHelm, Name: "Bronze med helm", Qty: 1, Worn: true, Sources: []Source{
			{Kind: SourceShop, Shop: PeksaShop, Price: 40, Note: "Peksa's keeps 5"}}},
		{ID: ObjCabbage, Name: "Cabbage", Qty: 1, Optional: true, Sources: cabbage},
		{ID: ObjShark, Name: "Shark", Qty: 4, Optional: true},
		{ID: ObjBass, Name: "Bass", Qty: 4, Optional: true},
	}
}

// ---------------------------------------------------------------------------------------------------------
// Decide.
// ---------------------------------------------------------------------------------------------------------

// DecideBlackKnightsFortress is the whole quest, read from the quest tab, the journal stage and what the pack holds and the account wears.
func DecideBlackKnightsFortress(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		// Not started at all: do not begin a quest the account cannot see through.
		if s.QuestPoints < BKMinQuestPoints {
			return Step{Kind: StepWait, ParkFor: BKQuestPointRetry,
				Reason: fmt.Sprintf("Sir Amik gives the quest to 12 quest points and over (this account has %d)", s.QuestPoints)}
		}
		if ok, why := BKFitHitpoints(s); !ok {
			return Step{Kind: StepWait, ParkFor: BKFitRetry, Reason: why}
		}
		return Step{Kind: StepTalk, Stop: SirAmikStop}
	}
	inside := s.TileKnown && BKInside(s.Tile)
	// On the open ground by the door, east of the west waypoint, with the way west not yet taken this run: the walker's own way
	// from there to Falador or a bank runs south past the three sentry knights, so the next walk is the one to the waypoint.
	// Taken once (a walk that failed or ran is not decided again).
	unclear := s.TileKnown && BKNeedsClearing(s.Tile) && !s.Failed[CustomBKLeave] && !s.Ran[CustomBKLeave]
	if !s.StageKnown {
		// Inside, the way is out whatever the journal says: a read that failed mid-fortress is no reason to wait among the knights.
		if inside {
			return Step{Kind: StepCustom, Custom: CustomBKLeave, Reason: "the journal stage is unread and the account is inside the fortress: out"}
		}
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	switch s.Stage {
	case BKStarted, BKListened:
		spy := s.Stage == BKStarted
		work := Step{Kind: StepCustom, Custom: CustomBKSpy, Reason: "into the fortress, to the grill, and a word with the witch"}
		if !spy {
			work = Step{Kind: StepCustom, Custom: CustomBKSabotage, Reason: "into the fortress, to the hole above the cauldron, and the cabbage down it"}
		}
		// One failed walk is enough: out of the fortress, then the quest waits. The engine's no-progress watchdog counts identical
		// snapshots in a row, and a walk that fails at one ladder and is tried again from the other side of it never repeats one.
		if routeFailed := s.Failed[CustomBKSpy] || s.Failed[CustomBKSabotage]; routeFailed {
			if inside {
				return Step{Kind: StepCustom, Custom: CustomBKLeave, Reason: "the walk through the fortress failed on its last try: out, and the quest waits"}
			}
			if unclear {
				// The walk walks itself clear on every way it ends outside; this is for the one that could not (the walk to the
				// waypoint failed, or a step died on the way): once more, then the wait below.
				return Step{Kind: StepCustom, Custom: CustomBKLeave, Reason: "the walk to the fortress failed with the account on the open ground by its door: west, clear of the knights, and the quest waits"}
			}
			return Step{Kind: StepWait, ParkFor: BKRouteRetry,
				Reason: "the walk to or through the Black Knights' Fortress failed on its last try (a crossing would not land, or the walk to the door did not arrive): not trying again for a while"}
		}
		if inside {
			// The cabbage is a pack item and the account that lost it (it is never banked inside) cannot do stage 2: out for one.
			if !spy && !held(ObjCabbage) {
				return Step{Kind: StepCustom, Custom: CustomBKLeave, Reason: "inside the fortress with no cabbage to drop: out to fetch one"}
			}
			return work
		}
		kitKey := "provision:" + KitBlackKnight
		if !s.Ran[kitKey] && !s.Failed[kitKey] {
			return Step{Kind: StepProvision, KitName: KitBlackKnight, Kit: bkKit(s), KeepIDs: bkKeep(), Bank: bkBank(s), BankSet: true,
				Reason: "the iron chainbody, the bronze med helm, a cabbage and food from the bank, the mule or the shops"}
		}
		for _, p := range []struct {
			id   int
			name string
		}{{ObjIronChainbody, "Iron chainbody"}, {ObjBronzeMedHelm, "Bronze med helm"}} {
			if s.WornIDs[p.id] > 0 {
				continue
			}
			if held(p.id) {
				return Step{Kind: StepEquip, ItemID: p.id, Item: p.name}
			}
			return Step{Kind: StepWait, ParkFor: BKDisguiseRetry,
				Reason: "no " + strings.ToLower(p.name) + " for the fortress door from the bank, the mule or the shops"}
		}
		if !held(ObjCabbage) {
			// The field south of Falador: the cabbage is the one thing the owner's rule leaves to the account's own hands.
			if s.FreeSlots < 2 {
				return Step{Kind: StepDeposit, KeepIDs: bkKeep(), Bank: bkBank(s), BankSet: true, Reason: "room for a cabbage"}
			}
			return Step{Kind: StepPickLoc, LocID: LocCabbage, Loc: "cabbage", Op: "Pick", Anchor: CabbageField,
				ExpectItemID: ObjCabbage, ExpectItem: "Cabbage"}
		}
		if ok, why := BKFit(s); !ok {
			return Step{Kind: StepWait, ParkFor: BKFitRetry, Reason: why}
		}
		return work
	case BKSabotaged:
		if inside {
			return Step{Kind: StepCustom, Custom: CustomBKLeave, Reason: "the potion is ruined: out of the fortress"}
		}
		if unclear {
			return Step{Kind: StepCustom, Custom: CustomBKLeave, Reason: "the potion is ruined and the account stands on the open ground by the door: west, clear of the knights, before the walk to Sir Amik"}
		}
		return Step{Kind: StepTalk, Stop: SirAmikStop}
	}
	if inside {
		return Step{Kind: StepCustom, Custom: CustomBKLeave, Reason: "an unrecognised stage and the account is inside the fortress: out"}
	}
	return Step{Kind: StepWait, Reason: "unrecognised Black Knights' Fortress stage"}
}

// BlackKnightsFortressRecord is the quest's identity: Sir Amik's 12 quest points.
var BlackKnightsFortressRecord = Record{ID: BlackKnightsFortressID, Name: BlackKnightsFortressName, QuestPoints: 3,
	Requirements: Requirements{MinQuestPoints: BKMinQuestPoints}}

// bkSustain is the quest's survival policy: eat at 18 hitpoints. A knight's hardest hit is 4, but up to five stand in the banquet
// hall and a hall crossing is one of them landing together: 18 leaves the account standing after a bad round at the line.
var bkSustain = Sustain{FoodIDs: BKFoods, EatAt: 18}

// BKPastFortress is the exclusive slot's release: the potion is ruined and the account is out of the fortress.
func BKPastFortress(s Snapshot) bool {
	if s.Journal == StatusComplete {
		return true
	}
	return s.StageKnown && s.Stage >= BKSabotaged && s.TileKnown && !BKInside(s.Tile)
}

// BlackKnightsFortress is the engine module. MaxDeaths 1: the first death anywhere in it parks the quest for a day (the knights
// are what killed the account, and they are where it would walk back to). NoRetaliate: the account never fights a knight.
var BlackKnightsFortress = Module{
	Record: BlackKnightsFortressRecord, Bank: FaladorWestBank, BankSet: true, Anchor: SirAmikAnchor,
	ToolIDs:       bkKeep(),
	ReadStage:     ParseBlackKnightsFortressJournal,
	Sustain:       bkSustain,
	MaxDeaths:     1,
	NoRetaliate:   true,
	ExclusiveDone: BKPastFortress,
	Decide:        DecideBlackKnightsFortress,
}
