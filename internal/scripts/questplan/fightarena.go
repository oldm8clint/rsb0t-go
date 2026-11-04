package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Fight Arena (content id `arena`, %arenaquest, not transmitted), from the quest-chain study
// (private evidence notes). The three beasts have no
// magic stat (they defend at 1), so every fight here is cast from ArenaSafespot, 2602,3153, directly south
// of the static Jeremy (266).
//
// THE SAFESPOT IS NOT ONE. The study's claim was that a size-2 beast coming from the north freezes at
// 2602,3155 behind Jeremy and can never reach it. The real collision pack
// (fightarena_pack_test.go) shows open floor on both sides of the tile, 2601,3153 and 2603,3153, with
// body positions beside it that overlap nothing of Jeremy's, and production has the beasts standing on
// Jeremy's own tile: Bouncer at 2602,3154 on 12 Oct ("the safespot did not hold"), and the Khazard Ogre
// and the Khazard Scorpion at 2602,3154 again on 16 Oct, when an account (53 hitpoints, Magic 55, 22
// pieces of cake) lost 39 hitpoints to the ogre, came out of Hengrad's cell at 19/53, cast the scorpion
// down, spent the General's follow-up clicking through his words, and by the shape of its log (a drop of
// 11 hitpoints in two seconds, which only Bouncer's 13 explains) died to Bouncer at under 15/53 with every
// cake still in the pack. So the tile is only where the account stands to cast; every beast reaches it,
// and the arena is priced as the melee it is: ArenaOdds plays the whole chain (the ogre, the scorpion and
// Bouncer, 216 hitpoints and a hit of 13 every four ticks at the end of it) against the runes and the food
// the pack holds, and the gate refuses an account that does not win it with margin. Bouncer (137, max 13)
// is what refuses almost all of them: tanking him takes sharks, not cake.
//
// One already inside with nothing castable melees, since no door lets it out before stage 12 and a death is
// the only other exit (questmods.arenaFight): inside, the account tops up before every beast it can, eats
// at twice the beast's hardest hit and looks at its hitpoints between casts. The ogre's max hit is 8 (with its
// strength bonus of 20) and the scorpion's 5 (npc_melee_maxhit).
//
// Where the account stands decides as much as the stage (ArenaPlaceOf): Khazard's building is a
// pocket no walk leaves (its door1s are scripted and not in the walk graph), and the arena floor
// and the cells are traps only the quest's own fights lead out of.

// FightArenaName is the quest tab's spelling (component 7358).
const FightArenaName = "Fight Arena"

// FightArenaID is the engine's id.
const FightArenaID = "arena"

// Ids from content/pack.
const (
	NpcLadyServil     = 264
	NpcDrunkGuard     = 254
	NpcKhazardBarman  = 259
	NpcHengrad        = 263
	NpcJeremyCell     = 265
	NpcJeremyArena    = 266
	NpcJustinServil   = 267
	NpcArenaOgre      = 270
	NpcArenaScorpion  = 271
	NpcArenaBouncer   = 269
	ObjKhazardHelmet  = 74
	ObjKhazardArmour  = 75
	ObjKhazardKeys    = 76
	ObjKhaliBrew      = 77
	LocArenaChestShut = 75
	LocArenaChestOpen = 76
	LocJeremyDoor     = 80
	LocArenaDoor1     = 81
	LocArenaDoor2     = 82
)

// Tiles from content maps (m40_49) and the study.
var (
	LadyServilAnchor = tile.Tile{X: 2566, Z: 3199}
	ArenaChestStand  = tile.Tile{X: 2613, Z: 3190}
	ArenaChestTile   = tile.Tile{X: 2613, Z: 3189}
	ArenaDoor1Out    = tile.Tile{X: 2617, Z: 3172}
	ArenaDoor1In     = tile.Tile{X: 2617, Z: 3171}
	DrunkGuardAnchor = tile.Tile{X: 2615, Z: 3141}
	KhazardBarAnchor = tile.Tile{X: 2566, Z: 3140}
	JeremyCellDoor   = tile.Tile{X: 2617, Z: 3167}
	ArenaSafespot    = tile.Tile{X: 2602, Z: 3153}
	HengradAnchor    = tile.Tile{X: 2598, Z: 3142}
	ArenaDoor2Tile   = tile.Tile{X: 2606, Z: 3152}
	arenaFoods       = []int{385, 373, 379, 365, 361, 329, 1891, 1893, 1895, 1897, 1899, 1901, 333, 2309, 2142}
	arenaTools       = []int{ObjKhazardHelmet, ObjKhazardArmour, ObjKhazardKeys, ObjKhaliBrew, CoinsID,
		554, 555, 556, 557, 558, 560, 562, 1381, 1383, 1385, 1387, 1393, 1395, 1397, 1399, 1401, 1403, 1405, 1407}
)

// InArena reports whether t is inside the arena's fighting zone (the kill checks' zone).
func InArena(t tile.Tile) bool { return t.X >= 2581 && t.X <= 2606 && t.Z >= 3151 && t.Z <= 3171 }

// InArenaCell reports whether t is inside the prison cell the General sends the account to.
func InArenaCell(t tile.Tile) bool { return t.X >= 2597 && t.X <= 2601 && t.Z >= 3142 && t.Z <= 3144 }

// InArenaPen reports whether t is in one of the three beast pens east of the arena's gates
// (x 2607-2609): the map spawns the scorpion at 2608,3159, Bouncer at 2608,3162 and the ogre at
// 2608,3165 (m40_49), and they stay penned until a release cutscene (jeremy_servil.rs2
// ogre_attack_justin, quest_arena.rs2 arena_release_scorp and arena_release_bouncer) opens the
// gate and walks them out.
func InArenaPen(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2607 && t.X <= 2609 && t.Z >= 3159 && t.Z <= 3166
}

// ArenaBeastLoose reports whether a beast standing at t (its south-west tile) is out on the
// arena floor, where it can be fought, as against in its pen. A penned beast is visible from
// the whole arena and answers Attack, but no path reaches it: live 4 Oct 15:55-17:43, acct9
// clicked Attack on the penned ogre at 2607,3165 from 2606,3164 for two hours, never asking
// Jeremy to set the fight going, because the ogre was "out" within 20 tiles.
func ArenaBeastLoose(t tile.Tile) bool { return t.Level == 0 && InArena(t) && !InArenaPen(t) }

// Custom step names (internal/scripts/questmods).
const (
	CustomArenaChest  = "arena-chest"
	CustomArenaGuard  = "arena-drunk-guard"
	CustomArenaJeremy = "arena-jeremy"
	CustomArenaFight  = "arena-fight"
	CustomArenaFlee   = "arena-flee"
	CustomArenaLeave  = "arena-leave"
	KitArena          = "arena-kit"
	// KitArenaStrike is the kit's second try with a Strike, when the Bolt kit failed: Betty sells
	// no chaos, so a Wind Bolt kit is only ever filled from the bank and the mule.
	KitArenaStrike = "arena-kit-strike"
)

var (
	LadyServilStop = NpcStop{Npc: "Lady Servil", ID: NpcLadyServil, Anchor: LadyServilAnchor, Leash: 8,
		Prefer: []string{"Can I help you?"}}
	KhazardBarmanStop = NpcStop{Npc: "Khazard barman", ID: NpcKhazardBarman, Anchor: KhazardBarAnchor, Leash: 6,
		Prefer: []string{"I'd like a Khali brew please."}}
)

var arenaJournal = []struct {
	stage  int
	needle string
}{
	{14, "quest complete!"},
	{13, "i eventually killed"},
	{12, "challenged me to fight him, but i ran away"},
	{11, "wants to kill me himself"},
	{10, "ordered me to fight his pet"},
	{9, "going to have to fight in the arena"},
	{8, "should talk to jeremy in the fight arena"},
	{6, "i need to protect jeremy and his father"},
	{5, "now i've got the keys"},
	{3, "guards told me that he really likes"},
	{2, "luckily i found some khazard armour in a chest"},
	{1, "and try to find her family"},
	{0, "i can start this quest by speaking to"},
}

// ParseFightArenaJournal reads arena_journal.rs2's rendered stage, newest first.
func ParseFightArenaJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range arenaJournal {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// The arena's casting kit, by Magic level (as merlinFightKit): Wind Bolt (one chaos, two air) at
// Magic 17 and up, Fire Strike (one mind, three fire, two air) at 13-16, and the weaker strikes
// below that, a staff of air paying the air. The casts cover all three beasts (216 hitpoints) at
// the spell's average hit with misses, plus twenty, and never fewer than a hundred. The runes are
// required and come from Betty's rune shop when neither the bank nor the mule has them; food is
// taken only when owned or cheap (the safespot takes no hits; only the lure and the flee from the
// General can).
const arenaMinKitCasts = 100

// arenaBeastHP is what is left to kill from a stage on: the Khazard Ogre (60), the Khazard
// Scorpion (40) and Bouncer (116) (content quest_arena.npc). Stage 11 and up have nothing left.
func arenaBeastHP(stage int) int {
	switch {
	case stage <= 6:
		return 60 + 40 + 116
	case stage <= 9:
		return 40 + 116
	case stage == 10:
		return 116
	}
	return 0
}

// arenaSpell is one standard-book combat spell (actions.autocastSpells), runes by object id.
type arenaSpell struct {
	Name          string
	Level, MaxHit int
	Runes         map[int]int
}

// Rune ids.
const (
	arenaFireRune  = 554
	arenaWaterRune = 555
	arenaAirRune   = 556
	arenaEarthRune = 557
	arenaMindRune  = 558
	arenaDeathRune = 560
	arenaChaosRune = 562
)

var arenaSpells = []arenaSpell{
	{"Wind Strike", 1, 2, map[int]int{arenaMindRune: 1, arenaAirRune: 1}},
	{"Water Strike", 5, 4, map[int]int{arenaMindRune: 1, arenaWaterRune: 1, arenaAirRune: 1}},
	{"Earth Strike", 9, 6, map[int]int{arenaMindRune: 1, arenaEarthRune: 2, arenaAirRune: 1}},
	{"Fire Strike", 13, 8, map[int]int{arenaMindRune: 1, arenaFireRune: 3, arenaAirRune: 2}},
	{"Wind Bolt", 17, 9, map[int]int{arenaChaosRune: 1, arenaAirRune: 2}},
	{"Water Bolt", 23, 10, map[int]int{arenaChaosRune: 1, arenaWaterRune: 2, arenaAirRune: 2}},
	{"Earth Bolt", 29, 11, map[int]int{arenaChaosRune: 1, arenaEarthRune: 3, arenaAirRune: 2}},
	{"Fire Bolt", 35, 12, map[int]int{arenaChaosRune: 1, arenaFireRune: 4, arenaAirRune: 3}},
	{"Wind Blast", 41, 13, map[int]int{arenaDeathRune: 1, arenaAirRune: 3}},
	{"Water Blast", 47, 14, map[int]int{arenaDeathRune: 1, arenaWaterRune: 3, arenaAirRune: 3}},
	{"Earth Blast", 53, 15, map[int]int{arenaDeathRune: 1, arenaEarthRune: 4, arenaAirRune: 3}},
	{"Fire Blast", 59, 16, map[int]int{arenaDeathRune: 1, arenaFireRune: 5, arenaAirRune: 4}},
}

// arenaStaffRunes is the rune a worn elemental staff pays (questrunner.staffRunes).
var arenaStaffRunes = map[int]int{
	1381: arenaAirRune, 1397: arenaAirRune, 1405: arenaAirRune,
	1383: arenaWaterRune, 1395: arenaWaterRune, 1403: arenaWaterRune,
	1385: arenaEarthRune, 1399: arenaEarthRune, 1407: arenaEarthRune,
	1387: arenaFireRune, 1393: arenaFireRune, 1401: arenaFireRune,
}

// arenaMagic is the Magic level the plan works from: an unread level counts as Fire Strike's 13,
// the kit the quest was first written for.
func arenaMagic(s Snapshot) int {
	if m := s.Level("magic"); m > 0 {
		return m
	}
	return 13
}

// arenaCasts is how many casts of sp the pack holds runes for (a worn elemental staff pays its
// element).
func arenaCasts(s Snapshot, sp arenaSpell) int {
	free := map[int]bool{}
	for id, n := range s.WornIDs {
		if r, ok := arenaStaffRunes[id]; ok && n > 0 {
			free[r] = true
		}
	}
	casts := -1
	for id, per := range sp.Runes {
		if free[id] {
			continue
		}
		if n := s.HeldID(id) / per; casts < 0 || n < casts {
			casts = n
		}
	}
	if casts < 0 {
		return 0
	}
	return casts
}

// ArenaDamage is the hitpoints the held runes are worth: the best single spell's casts at its
// average hit with misses (2.6 casts to a maximum hit). It decides whether the account may go
// into, or back into, the arena.
func ArenaDamage(s Snapshot) int {
	best := 0
	for _, sp := range arenaSpells {
		if sp.Level > arenaMagic(s) {
			continue
		}
		if d := arenaCasts(s, sp) * sp.MaxHit * 10 / 26; d > best {
			best = d
		}
	}
	return best
}

// arenaKitSpell is the spell the kit buys runes for.
func arenaKitSpell(s Snapshot) arenaSpell {
	want := "Wind Strike"
	switch m := arenaMagic(s); {
	case m >= 17:
		want = "Wind Bolt"
	case m >= 13:
		want = "Fire Strike"
	case m >= 9:
		want = "Earth Strike"
	case m >= 5:
		want = "Water Strike"
	}
	for _, sp := range arenaSpells {
		if sp.Name == want {
			return sp
		}
	}
	return arenaSpells[0]
}

// arenaStrikeKitSpell is the Strike the second-try kit buys: Fire Strike from Magic 13, the
// weaker Strikes below, every rune of each on Betty's shelf (port_sarim.inv [magicshop]).
func arenaStrikeKitSpell(s Snapshot) arenaSpell {
	sp := arenaKitSpell(s)
	if sp.Runes[arenaChaosRune] == 0 {
		return sp
	}
	for _, c := range arenaSpells {
		if c.Name == "Fire Strike" {
			return c
		}
	}
	return arenaSpells[0]
}

// arenaKitCasts is how many casts of sp the kit carries: every beast's hitpoints at sp's average
// hit with misses, plus twenty, and at least arenaMinKitCasts.
func arenaKitCasts(sp arenaSpell) int {
	n := (arenaBeastHP(0)*26+sp.MaxHit*10-1)/(sp.MaxHit*10) + 20
	if n < arenaMinKitCasts {
		n = arenaMinKitCasts
	}
	return n
}

// The account's fitness for the arena, judged before it goes in (stage 5 and every re-entry), never once it is inside:
// the floor and the cells let nothing out before stage 12, so an account already there fights whatever it has.
//
// Live 6-7 Oct 2026, after the 21:46 relaunch: eleven deaths to Bouncer and the Khazard Scorpion in three hours (No Hope
// three times, Spare 26 twice, Spare 27, Spare31, spare6 twice, Spare32, spare3), every one at 27-28 hitpoints with
// bread to eat. The safespot beside Jeremy did not hold against Bouncer ("Bouncer is adjacent at 2602,3154: the safespot
// did not hold", six casts later dead; the beast stood on Jeremy's own tile), and 28 hitpoints are two of his hits (max 13)
// from nothing. So an account goes in with the hitpoints to stand a few hits and the food to out-eat them, or not at all.
const (
	// ArenaMinHitpoints is the base Hitpoints an account needs to enter. 40 until 12 Oct 2026, when an account at 41
	// cast at Bouncer every three seconds from the safespot he stands beside (on Jeremy's tile), ate twelve sharks in
	// ninety seconds and died with one left: at 41 the race against his 116 hitpoints is lost on a bad roll.
	ArenaMinHitpoints = 50
	// ArenaMinFood is the pieces of food it must carry in (bread, cake and the better foods all count).
	ArenaMinFood = 12
	// ArenaParkFor is how long an account that is not fit skips the quest: its Hitpoints do not change in an hour.
	ArenaParkFor = 12 * time.Hour
)

// arenaSustain is the quest's survival policy: arenaFoods best first, eating at 14 (raised for a beast by
// Sustain.VsFoe: the ogre's 16, Bouncer's 26).
var arenaSustain = Sustain{FoodIDs: arenaFoods, EatAt: 14}

// ArenaOddsParkFor is how long an account that cannot win the chain with its pack skips the quest: its
// food and runes change with a trip to the bank or a mule's answer, not in minutes.
const ArenaOddsParkFor = 6 * time.Hour

// ArenaBeastsFrom is the beasts still to be fought from a stage on, in the order the chain releases them: the
// ogre until stage 6 is past, the scorpion at 8 and 9 (stage 8 is the word with Jeremy that ends in Hengrad's
// cell), Bouncer at 10. Stage 11 is the General's challenge, which the account walks away from.
func ArenaBeastsFrom(stage int) []MeleeFoe {
	switch {
	case stage <= 6:
		return []MeleeFoe{FoeArenaOgre, FoeArenaScorpion, FoeArenaBouncer}
	case stage <= 9:
		return []MeleeFoe{FoeArenaScorpion, FoeArenaBouncer}
	case stage == 10:
		return []MeleeFoe{FoeArenaBouncer}
	}
	return nil
}

// ArenaOdds plays the beasts still to be fought from the snapshot's stage against the pack: the account tops up
// before each, casts the spells its runes pay for (the highest first, a lower one when they run out) and swings
// when nothing is castable, eating at the eat line the loops use, with the foe's damage a quarter above the
// expected (FightIncomingScale). It must win every beast without ever standing below the beast's hardest hit
// and with a tenth of the bites to spare (JudgeFight). An account whose hitpoints are unread is not judged.
//
// It is the half of the entry gate that ArenaDamage (runes worth the hitpoints) and ArenaMinFood (twelve pieces)
// cannot be: those count what the account has, this plays what the beasts do with it. Bouncer's hundred and
// sixteen hitpoints take a Magic 55 account about a hundred ticks of Water Blasts, in which he lands twenty-five
// hits of up to thirteen; the cakes that "twelve pieces" counts heal four.
func ArenaOdds(s Snapshot) FightOdds {
	return FightChain(s, ArenaBeastsFrom(s.Stage), arenaSustain, 0)
}

// arenaFoodHeld is the pieces of arenaFoods the pack holds.
func arenaFoodHeld(s Snapshot) int {
	n := 0
	for _, id := range arenaFoods {
		n += s.HeldID(id)
	}
	return n
}

// ArenaFitHitpoints judges the Hitpoints half of the entry gate: an unread maximum (0) passes, as every other gate here
// treats an unread stat.
func ArenaFitHitpoints(s Snapshot) (bool, string) {
	if s.MaxHP > 0 && s.MaxHP < ArenaMinHitpoints {
		return false, fmt.Sprintf("not doing Fight Arena yet: Bouncer (level 137, 116 hitpoints, hits up to 13) killed every low-hitpoint account that went in, and the arena has no way out before he is dead (this account has %d hitpoints, the gate is %d)", s.MaxHP, ArenaMinHitpoints)
	}
	return true, ""
}

// ArdougneBaker is the East Ardougne market baker (bread, cost 12, stock 10).
var ArdougneBaker = ShopRef{Npc: "Baker", NpcID: 571, Anchor: tile.Tile{X: 2669, Z: 3310}}

var arenaRuneNames = map[int]string{
	arenaFireRune: "Fire rune", arenaWaterRune: "Water rune", arenaAirRune: "Air rune", arenaEarthRune: "Earth rune",
	arenaMindRune: "Mind rune", arenaChaosRune: "Chaos rune",
}

// arenaRunePrices are Betty's (as the other quests' kits price them).
var arenaRunePrices = map[int]int{
	arenaFireRune: 4, arenaWaterRune: 4, arenaAirRune: 4, arenaEarthRune: 4, arenaMindRune: 3, arenaChaosRune: 15,
}

func arenaKit(s Snapshot) []KitLine { return arenaKitOf(s, arenaKitSpell(s)) }

// arenaKitOf is the kit for sp: its runes for arenaKitCasts casts, coins and food.
func arenaKitOf(s Snapshot, sp arenaSpell) []KitLine {
	casts := arenaKitCasts(sp)
	staff := ownsAirStaff(s)
	kit := []KitLine{{ID: CoinsID, Name: "Coins", Qty: 20, Stackable: true}}
	for _, id := range []int{arenaChaosRune, arenaMindRune, arenaFireRune, arenaEarthRune, arenaWaterRune, arenaAirRune} {
		per := sp.Runes[id]
		if per == 0 || (id == arenaAirRune && staff) {
			continue
		}
		kit = append(kit, KitLine{ID: id, Name: arenaRuneNames[id], Qty: per * casts, Stackable: true,
			Sources: []Source{{Kind: SourceShop, Shop: BettyShop, Price: arenaRunePrices[id]}}})
	}
	kit = append(kit,
		KitLine{ID: 385, Name: "Shark", Qty: 10, Optional: true},
		KitLine{ID: 2309, Name: "Bread", Qty: 8, Optional: true, Sources: []Source{{Kind: SourceShop, Shop: ArdougneBaker, Price: 12}}})
	if staff {
		kit = append(kit, KitLine{ID: 1381, Name: "Staff of air", Qty: 1, Optional: true, Worn: true})
	}
	return kit
}

// ownsAirStaff reports a staff of air worn, held or banked.
func ownsAirStaff(s Snapshot) bool {
	if s.WornIDs[1381] > 0 || s.InvIDs[1381] > 0 {
		return true
	}
	n, _ := s.BankedID(1381)
	return n > 0
}

// ArenaPlace is where the account stands, as far as the quest's ways in and out go.
type ArenaPlace int

const (
	// ArenaOutside is anywhere the walker plans from.
	ArenaOutside ArenaPlace = iota
	// ArenaBuilding is inside Khazard's building, outside the arena and the cells. Its only exits
	// are the two fightarena_door1 (loc 81 at 2617,3171 and 2585,3141, map m40_49), which open
	// from inside by script (quest_arena.rs2 [oploc1,fightarena_door1], check_axis) and are not
	// in the walk graph (webwalk/graph.json has no loc 81 edge), so no walk leaves it.
	ArenaBuilding
	// ArenaFloor is the fighting zone (quest_arena.constant ^arena_lower_coord..^arena_upper_coord).
	// door2 from this side lets the account out only at stage 12 and up ([oploc1,fightarena_door2]).
	ArenaFloor
	// ArenaCell is the prison cell the General's guards lock the account in at stage 9
	// ([label,general_khazard_belong_nobody]); its gate (arena_prisondoor) is always locked.
	ArenaCell
)

// ArenaPlaceOf classifies t.
func ArenaPlaceOf(t tile.Tile) ArenaPlace {
	switch {
	case t.Level != 0:
		return ArenaOutside
	case InArenaCell(t):
		return ArenaCell
	case InArena(t):
		return ArenaFloor
	case InArenaBuilding(t):
		return ArenaBuilding
	}
	return ArenaOutside
}

// InKhazardPocket is Khazard's building outside the arena and the cells (ArenaBuilding): the part
// a door1 lets the account walk out of.
func InKhazardPocket(t tile.Tile) bool { return ArenaPlaceOf(t) == ArenaBuilding }

// InArenaTrap is the arena floor or a cell: no door lets the account out of either before stage
// 12, so only the quest's own fights (or a death) do.
func InArenaTrap(t tile.Tile) bool {
	p := ArenaPlaceOf(t)
	return p == ArenaFloor || p == ArenaCell
}

// arenaGoesOutside reports a step whose walk ends outside Khazard's building.
func arenaGoesOutside(st Step) bool {
	switch st.Kind {
	case StepTalk:
		return !InArenaBuilding(st.Stop.Anchor)
	case StepProvision, StepWithdraw, StepDeposit, StepScanBank:
		return true
	}
	return false
}

// DecideFightArena is the whole quest. Every step that walks somewhere outside Khazard's building
// is preceded by leaving it through door1 (CustomArenaLeave): live 4 Oct 12:10, sike, emod and
// moderator talked to the drunk guard at stage 2, stood in his room (2615,3141) at stage 3, and
// parked on "talk to Khazard barman" with every walk to 2566,3140 "unreachable"; the ladder's
// next quests then parked the same way, and acct9's stage 5 kit failed on "could not use the
// bank at 2613,3094".
func DecideFightArena(s Snapshot) Step {
	st := decideFightArena(s)
	if s.TileKnown && InKhazardPocket(s.Tile) && arenaGoesOutside(st) {
		return Step{Kind: StepCustom, Custom: CustomArenaLeave, Reason: "out of Khazard's building first, for: " + st.Describe()}
	}
	return st
}

// ArenaWayOutOnly is decide for a run whose only job is to get a trapped account out (Stage 5
// forces Fight Arena on an account standing on the arena floor or in a cell even when its ladder
// no longer lists the quest): done as soon as the account stands outside Khazard's building
// before stage 12, and the whole quest from stage 12 on, which is only the flee and Lady Servil.
func ArenaWayOutOnly(s Snapshot, decide func(Snapshot) Step) Step {
	if s.Journal == StatusInProgress && s.StageKnown && s.Stage < 12 && s.TileKnown && ArenaPlaceOf(s.Tile) == ArenaOutside {
		return Step{Kind: StepDone, Reason: "out of Khazard's arena, which was this run's only job"}
	}
	return decide(s)
}

func decideFightArena(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		// Not started at all: do not begin a quest whose end the account cannot survive.
		if fit, why := ArenaFitHitpoints(s); !fit {
			return Step{Kind: StepWait, Reason: why, ParkFor: ArenaParkFor}
		}
		return Step{Kind: StepTalk, Stop: LadyServilStop}
	}
	place := ArenaOutside
	if s.TileKnown {
		place = ArenaPlaceOf(s.Tile)
	}
	if !s.StageKnown {
		// Inside, the step is the fight whatever the journal says (stages 6 to 11 all fight on the floor, and
		// nothing lets the account out before 12). Live 9 Oct the journal read failed every other pass mid-fight
		// ("the interface did not respond") and each failed read was a wait, about ten seconds a time without a
		// cast while Bouncer hit: three deaths at the arena with food still carried.
		if place == ArenaFloor || place == ArenaCell {
			return Step{Kind: StepCustom, Custom: CustomArenaFight, Reason: "inside the arena: fighting on while the journal stage is unread"}
		}
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	worn := func(id int) bool { return s.WornIDs[id] > 0 }
	keep := append(append([]int(nil), arenaTools...), arenaFoods...)
	kitKey := "provision:" + KitArena
	provision := func(why string) Step {
		return Step{Kind: StepProvision, KitName: KitArena, Kit: arenaKit(s), KeepIDs: keep,
			Bank: YanilleBank, BankSet: true, Reason: why}
	}
	// armed is the go/no-go for walking into the beasts: the runes held must be worth what is
	// left to kill. Live 4 Oct 12:29, acct9's kit failed, it freed Jeremy anyway, and stood in
	// the arena at stage 6 with "no castable combat spell left for Khazard Ogre", where door2
	// does not let it out. With the kit tried and still short, the quest waits (and so parks)
	// outside rather than walk in.
	armed := func() (Step, bool) {
		need := arenaBeastHP(s.Stage)
		if need == 0 {
			return Step{}, true
		}
		if fit, why := ArenaFitHitpoints(s); !fit {
			return Step{Kind: StepWait, Reason: why, ParkFor: ArenaParkFor}, false
		}
		odds := ArenaOdds(s)
		if ArenaDamage(s) >= need && arenaFoodHeld(s) >= ArenaMinFood && odds.OK {
			return Step{}, true
		}
		if !s.Ran[kitKey] && !s.Failed[kitKey] {
			return provision("runes, the staff and food for the beasts"), false
		}
		// The Bolt kit failed (no chaos in the bank or the mule, and Betty sells none): a Strike
		// kit, whose runes Betty does sell, before giving up.
		strikeKey := "provision:" + KitArenaStrike
		if s.Failed[kitKey] && arenaKitSpell(s).Runes[arenaChaosRune] > 0 && !s.Ran[strikeKey] && !s.Failed[strikeKey] {
			sp := arenaStrikeKitSpell(s)
			return Step{Kind: StepProvision, KitName: KitArenaStrike, Kit: arenaKitOf(s, sp), KeepIDs: keep,
				Bank: YanilleBank, BankSet: true, Reason: "the Wind Bolt kit failed: " + sp.Name + " runes from Betty instead"}, false
		}
		if !odds.OK {
			return Step{Kind: StepWait, ParkFor: ArenaOddsParkFor, Reason: fmt.Sprintf("not going into the arena with this pack (the runes hold %d damage of the %d the beasts have, %d pieces of food): %s", ArenaDamage(s), need, arenaFoodHeld(s), odds.Why)}, false
		}
		return Step{Kind: StepWait, Reason: fmt.Sprintf("the runes held (%d damage of the %d the beasts have) or the food (%d pieces, the gate is %d) are short of the arena, and the kit has been tried", ArenaDamage(s), need, arenaFoodHeld(s), ArenaMinFood)}, false
	}
	disguise := func() (Step, bool) {
		for _, id := range []int{ObjKhazardHelmet, ObjKhazardArmour} {
			if !worn(id) {
				if held(id) {
					return Step{Kind: StepEquip, ItemID: id, Item: "Khazard disguise"}, true
				}
				return Step{Kind: StepCustom, Custom: CustomArenaChest, Reason: "a Khazard piece is missing: the chest again"}, true
			}
		}
		return Step{}, false
	}
	switch s.Stage {
	case 1:
		return Step{Kind: StepCustom, Custom: CustomArenaChest, Reason: "the Khazard armour chest"}
	case 2:
		if st, ok := disguise(); ok {
			return st
		}
		return Step{Kind: StepCustom, Custom: CustomArenaGuard, Reason: "the drunk guard, in disguise"}
	case 3:
		if st, ok := disguise(); ok {
			return st
		}
		if !held(ObjKhaliBrew) {
			if s.HeldID(CoinsID) < 5 {
				return provision("coins for the brew, and the fight kit")
			}
			return Step{Kind: StepTalk, Stop: KhazardBarmanStop, UntilItemID: ObjKhaliBrew}
		}
		return Step{Kind: StepCustom, Custom: CustomArenaGuard, Reason: "the brew for the cell keys"}
	case 5:
		// door1 lets the account in only in disguise, and the drunk guard re-issues lost keys
		// only to a whole one (khazard_guard.rs2 [opnpc1,arena_guard2]).
		if st, ok := disguise(); ok {
			return st
		}
		if !held(ObjKhazardKeys) {
			return Step{Kind: StepCustom, Custom: CustomArenaGuard, Reason: "the spare keys"}
		}
		if st, ok := armed(); !ok {
			return st
		}
		return Step{Kind: StepCustom, Custom: CustomArenaJeremy, Reason: "the keys on Jeremy's cell"}
	case 6, 8, 9, 10, 11:
		switch place {
		case ArenaFloor:
			return Step{Kind: StepCustom, Custom: CustomArenaFight, Reason: "the arena: cast from the safespot beside Jeremy, melee with nothing castable"}
		case ArenaCell:
			return Step{Kind: StepCustom, Custom: CustomArenaFight, Reason: "Hengrad, then the arena"}
		}
		// Back in after a death or a relog, armed first.
		if st, ok := armed(); !ok {
			return st
		}
		if s.Stage == 6 || s.Stage == 8 {
			// Stages 6 and 8 go back in through door1, in disguise, then door2's building side
			// (open_arena_door); 9 to 11 are sent straight in by door1's guard (arena_enter).
			if st, ok := disguise(); ok {
				return st
			}
		}
		return Step{Kind: StepCustom, Custom: CustomArenaFight, Reason: "back into the arena"}
	case 12, 13:
		if place != ArenaOutside {
			return Step{Kind: StepCustom, Custom: CustomArenaFlee, Reason: "out through the arena doors"}
		}
		return Step{Kind: StepTalk, Stop: LadyServilStop}
	}
	return Step{Kind: StepWait, Reason: "arena: unrecognised stage"}
}

// YanilleBank is the bank nearest the arena (the TS def banks its kit there too).
var YanilleBank = tile.Tile{X: 2613, Z: 3094}

// FightArenaRecord is the quest's identity (no server gates).
var FightArenaRecord = Record{ID: FightArenaID, Name: FightArenaName, QuestPoints: 2}

// FightArena is the engine module.
var FightArena = Module{
	Record: FightArenaRecord, Bank: YanilleBank, BankSet: true, Anchor: LadyServilAnchor,
	ToolIDs:   arenaTools,
	ReadStage: ParseFightArenaJournal,
	Sustain:   arenaSustain,
	// One death in the arena skips the quest for a day, as the engine does at the cap: it was 3, and six accounts
	// died twice and No Hope three times inside four hours (6-7 Oct 2026), re-entering after each.
	MaxDeaths: 1,
	Decide:    DecideFightArena,
}

// InArenaBuilding is Khazard's building, arena and cells included (TS areas.ts building rect).
func InArenaBuilding(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2585 && t.X <= 2619 && t.Z >= 3139 && t.Z <= 3171
}
