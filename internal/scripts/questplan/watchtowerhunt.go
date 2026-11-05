package questplan

import (
	"fmt"

	"rsb0t-go/internal/scripts/tile"
)

// The Watch Tower's hunters and what a visit among them costs.
//
// WHY THIS FILE. The first version priced the ogre country as "two ogres fought one after the other" and the shamans' enclave as "a chieftain's
// attention for 80 ticks". The maps say otherwise (m40_47, m39_48, m39_47, m40_147), and the account never fights back, so every hunter in reach
// of a tile hits it for as long as it stands there, and nothing is eaten inside a chat:
//
//   - Toban's camp holds four ogres (npc 115) within eight tiles of the arrival, the chest and Toban, and two of them within eight of Gorad;
//   - Grew's island holds four (a 15 by 14 island they cannot leave, with the berries and Grew within one to three tiles of them), and four more
//     stand round the branch the rope is tied to, four round Og, one beside the camp's cave mouth;
//   - the shamans' enclave is a nest of SIX BLUE DRAGONS (npc 55: level 111, 105 hitpoints, size 4, cowardly, hunt range 4, wander range 4, which
//     hunts any account of combat under 222), a greater demon (npc 83: level 92) and three skeletons beside the six chieftains and the six shamans. One
//     attack in four of a dragon is dragonfire (dragon.rs2 dragon_fire): up to 30, up to 50 when its magic roll beats the account's defence roll, 5 with
//     an anti-dragon shield worn, 10 under Protect from Magic, no matter what else the account does. Measured on the real collision pack, the walk from
//     the arrival tile to the exit cave is 58 tiles and 55 dragon-ticks (a dragon in reach for a tick is one; never more than two at a time), and every shaman, the rock,
//     the robe and the exit are reached only through tiles in a dragon's reach (the search that avoids them finds nothing: questmods
//     TestWatchtowerNoRouteInTheEnclaveIsFencedFromTheDragons). The nightshade run-past is the only door in, and the enclave has no other door out.
//
// So every visit is priced against the hunters of its stands, from tables cut from the maps (watchtower_content_test.go recounts every one of them),
// and the enclave is priced as the walk it is.

// The hunters' reach, from the npc configs (content scripts/_unpack/225/all.npc and areas/area_yanille/configs/ogre.npc): a hunter finds a target within
// huntrange of where it stands (its spawn tile moved by up to wanderrange), chases it while the target is within maxrange (wander + 2) of the spawn,
// and a body of size n reaches n-1 tiles further; one tile more is slack.
const (
	// WtOgreReach is the ogres' (npc 114 and 115, size 2, wander 3, hunt 2) and the chieftains' (npc 852): the same as WtChieftainPad.
	WtOgreReach = 3 + 2 + 1 + 1
	// WtDragonReach is the blue dragons' (size 4, wander 4, hunt 4): a dragon at the edge of its wander finds an account eight tiles from its spawn.
	WtDragonReach = 4 + 4 + 1
	// WtDemonReach is the greater demon's (size 3, wander 6, hunt 3).
	WtDemonReach = 6 + 3 + 1
)

// Dragonfire and the shield that blunts it (dragon.rs2 [proc,dragon_fire]): the content ids and numbers.
const (
	WtObjAntiDragonShield = 1540
	// WtDragonFireMax is dragonfire's maximum hit, WtDragonFireBonus what it adds when the dragon's magic roll beats the account's defence roll, and
	// WtDragonShieldFire the maximum with an anti-dragon shield worn.
	WtDragonFireMax    = 30
	WtDragonFireBonus  = 20
	WtDragonShieldFire = 5
	// WtDragonFireOneIn: one attack in this many is dragonfire (random(4) = 0).
	WtDragonFireOneIn = 4
)

// The enclave's creatures, from the npc configs. The blue dragon and the greater demon are in scripts/_unpack/225/all.npc (the engine reads them from there),
// the skeleton too: a blue dragon's claw or bite is a melee attack of up to 10 (strength 95, no bonus), every fourth attack is fire.
var (
	// FoeBlueDragon: ZapMax is the hardest single hit (the fire), ZapOneIn stays 0 because the fire is not rolled against the account's magic defence
	// (WtDragonPerAttack prices it) and FoePerAttack must not add a zap of its own.
	FoeBlueDragon = MeleeFoe{Name: "Blue dragon", Attack: 95, Strength: 95, Defence: 95, Magic: 1, Hitpoints: 105, AttackTicks: 4,
		ZapMax: WtDragonFireMax + WtDragonFireBonus}
	// FoeGreaterDemon (level 92, huntmode cowardly with huntrange 3 and wanderrange 6) is declared once, in lostcity.go: the same npc 83.
)

// WtDragonPeak is the hardest single hit of a blue dragon: its fire, or its claw when a shield has taken the fire down to 5.
func WtDragonPeak(shielded bool) int {
	if shielded {
		return max(FoeBlueDragon.MaxHit(), WtDragonShieldFire)
	}
	return WtDragonFireMax + WtDragonFireBonus
}

// WtDragonPerAttack is the hitpoints one attack of a blue dragon is expected to take off an account with these levels (no armour, no prayer): three in
// four are a claw or a bite (FoePerAttack), one in four is dragonfire, which always lands. Its maximum is 30, or 50 when the dragon's magic roll
// ((1+9)*64) beats the account's defence roll ((defence+8)*(bonus+64), the slash style the dragon fights in), and a hit is uniform from 0 to the maximum.
func WtDragonPerAttack(p MeleeFighter, shielded bool) float64 {
	melee := FoePerAttack(FoeBlueDragon, p)
	fire := float64(WtDragonShieldFire) / 2
	if !shielded {
		bonus := rollHit((FoeBlueDragon.Magic+9)*64, (p.Defence+8)*(p.DefenceBonus+64))
		fire = float64(WtDragonFireMax)/2 + bonus*float64(WtDragonFireBonus)/2
	}
	share := 1 / float64(WtDragonFireOneIn)
	return (1-share)*melee + share*fire
}

// WtDragonPerTick is WtDragonPerAttack as a tick's expected damage from one dragon in reach, scaled like every other fight price.
func WtDragonPerTick(p MeleeFighter, shielded bool) float64 {
	return WtDragonPerAttack(p, shielded) * FightIncomingScale / float64(FoeBlueDragon.AttackTicks)
}

// wtFoePerTick is a melee foe's expected damage per tick on the account, scaled.
func wtFoePerTick(f MeleeFoe, p MeleeFighter) float64 {
	return FoePerAttack(f, p) * FightIncomingScale / float64(f.AttackTicks)
}

// ---------------------------------------------------------------------------------------------------------------------
// The enclave
// ---------------------------------------------------------------------------------------------------------------------

// The enclave's spawns (maps/m40_147.jm2, origin 2560,9408, level 0). The dragons, the demon at 2586,9458 and the skeletons are in the arrival tile's
// region (one flood fill on the real pack reaches all of them, every shaman, the rock, the robe and the exit); the other four demons and the four giant bats
// stand in a region of their own, which no tile of the enclave's walks touches (questmods TestWatchtowerEnclaveSealedHuntersAreSealed).
var (
	WtEnclaveDragons   = []tile.Tile{{X: 2568, Z: 9437}, {X: 2579, Z: 9445}, {X: 2590, Z: 9461}, {X: 2592, Z: 9431}, {X: 2604, Z: 9443}, {X: 2609, Z: 9459}}
	WtEnclaveDemons    = []tile.Tile{{X: 2586, Z: 9458}}
	WtEnclaveSkeletons = []tile.Tile{{X: 2576, Z: 9436}, {X: 2579, Z: 9461}, {X: 2590, Z: 9437}}
	// WtEnclaveSealedDemons and WtEnclaveSealedBats are the hunters of the enclave's map square that are not in the region the account walks in.
	WtEnclaveSealedDemons = []tile.Tile{{X: 2612, Z: 9423}, {X: 2615, Z: 9423}, {X: 2615, Z: 9426}, {X: 2618, Z: 9424}}
	WtEnclaveSealedBats   = []tile.Tile{{X: 2608, Z: 9421}, {X: 2612, Z: 9416}, {X: 2619, Z: 9427}, {X: 2620, Z: 9419}}
)

// WtHuntersCovering counts, for a tile, the hunters whose reach covers it: dragons, the connected demon and chieftains of the enclave.
func WtHuntersCovering(t tile.Tile) (dragons, demons, chiefs int) {
	for _, d := range WtEnclaveDragons {
		if t.Level == d.Level && tile.Chebyshev(t, d) <= WtDragonReach {
			dragons++
		}
	}
	for _, d := range WtEnclaveDemons {
		if t.Level == d.Level && tile.Chebyshev(t, d) <= WtDemonReach {
			demons++
		}
	}
	for _, c := range WtEnclaveChieftains {
		if t.Level == c.Level && tile.Chebyshev(t, c) <= WtOgreReach {
			chiefs++
		}
	}
	return dragons, demons, chiefs
}

// WtVisit is a stay in the shamans' enclave: how long it is and how much of it is spent in the hunters' reach, counted in hunter-ticks (a tick with two
// dragons in reach is two). Every number is measured on the real collision pack (questmods TestWatchtowerEnclaveVisitsAreTheWalksTheyName: the
// A* route between the visit's points, tile by tile, plus the stands) and is a ceiling, within a third of the measurement.
type WtVisit struct {
	Name string
	// Ticks is the stay: the walk (one tick a tile), the stands and the cave's op.
	Ticks int
	// Dragon, Demon and Chief are the hunter-ticks of the blue dragons, the greater demon and the ogre chieftains.
	Dragon, Demon, Chief int
	// Walk is the route's tiles, for the reason.
	Walk int
}

// WtShamanStandTicks is how long the account stands at a shaman: the walk to a tile beside it, the potion used on it, the six-tick animation, the next
// shaman looked for. WtRockStandTicks is the rock's: a bronze pickaxe on a rock of Mining 40 is a swing every four ticks and the executor gives it 25 s.
const (
	WtShamanStandTicks = 8
	WtRockStandTicks   = 30
	WtExitOpTicks      = 4
)

// The visits. The dragon-ticks are the pack's with the stands added: the arrival, the rock stand and the robe are outside every dragon's reach (nine tiles),
// the exit and five of the six shaman stands are in one dragon's, the sixth in two.
var (
	// WtVisitExit is the walk out from the arrival tile, which is all that the stage 7 hop (the nightshade run-past) leaves the account to do.
	WtVisitExit = WtVisit{Name: "the walk out of the enclave", Walk: 58, Ticks: 62, Dragon: 60, Demon: 0, Chief: 15}
	// WtVisitWork is the whole of stage 10: the six shamans by the nearest-first tour, the rock and the way out.
	WtVisitWork = WtVisit{Name: "the six shamans, the rock and the way out", Walk: 171, Ticks: 253, Dragon: 240, Demon: 102, Chief: 90}
	// WtVisitRock is the rock alone, for an account that came back for it.
	WtVisitRock = WtVisit{Name: "the rock and the way out", Walk: 78, Ticks: 112, Dragon: 76, Demon: 68, Chief: 12}
	// WtVisitRobe is the robe alone, for an account that lost the third crystal.
	WtVisitRobe = WtVisit{Name: "the robe and the way out", Walk: 76, Ticks: 90, Dragon: 50, Demon: 0, Chief: 28}
)

// WtEnclaveEatLine is the hitpoints at or under which the account eats in the enclave: twice the dragons' hardest hit, never over three quarters of its
// hitpoints (FoeEatLine). Above the fire's maximum it keeps a breath from finishing the account; under it, the price is refused.
func WtEnclaveEatLine(maxHP int, shielded bool) int {
	return FoeEatLine(wtSustain.EatAt, WtDragonPeak(shielded), maxHP)
}

// wtBudget is what a stretch of expected damage must stay under.
type wtBudget struct {
	// hp is the hitpoints the account starts with after a top-up from its food, pool the food that is left, and spare nine tenths of the pool.
	hp, pool, spare float64
}

// wtBudgetOf reads an account's hitpoints and food into the budget the visit's damage must stay under.
func wtBudgetOf(s Snapshot) wtBudget {
	hp := float64(s.HP)
	if s.HP <= 0 || s.HP > s.MaxHP {
		hp = float64(s.MaxHP)
	}
	pool := float64(FoodPoolHeal(s.InvIDs, WtFoodIDs))
	up := min(float64(s.MaxHP)-hp, pool)
	hp += up
	pool -= up
	return wtBudget{hp: hp, pool: pool, spare: 0.9 * pool}
}

// WtEnclaveOdds prices a visit to the shamans' enclave. The account never fights back and the dragons are not outrun, so every hunter-tick of the visit is
// a tick of that hunter's attacks on the account. The price has two parts: the account can be kept out of the reach of one dragonfire (a bite is taken
// before a breath could finish it: the eat line must be above the fire's maximum), and what the visit costs, with two of the hardest hits to the good, is
// covered by the hitpoints it starts with and nine tenths of its food. An anti-dragon shield worn takes the fire down to 5.
func WtEnclaveOdds(s Snapshot, v WtVisit) (bool, string) {
	if s.MaxHP <= 0 {
		return true, ""
	}
	p := fighterOf(s)
	shielded := s.WornIDs[WtObjAntiDragonShield] > 0
	peak := WtDragonPeak(shielded)
	line := WtEnclaveEatLine(s.MaxHP, shielded)
	shield := "no anti-dragon shield worn"
	if shielded {
		shield = "an anti-dragon shield worn"
	}
	if line <= peak {
		return false, fmt.Sprintf("the shamans' enclave is a nest of six blue dragons (level 111) whose fire hits for up to %d, and this account (%s, %d hitpoints) would eat at %d, under it: %s",
			peak, shield, s.MaxHP, line, v.Name)
	}
	dmg := float64(v.Dragon)*WtDragonPerTick(p, shielded) + float64(v.Demon)*wtFoePerTick(FoeGreaterDemon, p) + float64(v.Chief)*wtFoePerTick(FoeOgreChieftain, p)
	b := wtBudgetOf(s)
	margin := float64(2 * peak)
	if b.hp+b.spare-dmg < margin {
		return false, fmt.Sprintf("the shamans' enclave is a nest of six blue dragons (level 111, fire up to %d): %s is %d ticks in their reach (%d dragon-ticks, %d of the greater demon's, %d of the chieftains') and would cost an account at Defence %d with %s about %.0f hitpoints, and this one has %.0f hitpoints and %.0f of food to spend (it must keep %.0f)",
			peak, v.Name, v.Ticks, v.Dragon, v.Demon, v.Chief, s.Level("defence"), shield, dmg, b.hp, b.spare, margin)
	}
	return true, ""
}

// ---------------------------------------------------------------------------------------------------------------------
// The ogre sites
// ---------------------------------------------------------------------------------------------------------------------

// WtSite is a place in ogre country where the account stands, with the number of ogres (npc 114 and 115, level 53, a hit of up to 7 every six ticks)
// whose reach covers its stands and how long the visit is. The ogres of a pocket (the island, the camp) are the ones inside it; those of the surface are the
// ones on the surface. watchtower_content_test.go recounts Ogres from the maps for every site.
type WtSite struct {
	Name string
	// Stands are the tiles the visit stands on or passes; Area is where the ogres counted stand (WtAreaMain for the surface).
	Stands []tile.Tile
	Area   WtArea
	// Ogres is the count; Ticks the visit; Chat the part of it spent in conversations and boxes, where nothing can be eaten.
	Ogres, Ticks, Chat int
	// Road is the ogre-ticks of the road from the bank to the site and back (twice the one-way count of the real pack's A* route: the surface ogres whose reach
	// covers each tile of it, summed), which is not a stand and has no chat. The pack test recounts it.
	Road int
}

// The sites. A chat page costs about two ticks (the driver clicks continue about every 1.2 s), and the pages are counted from the scripts: Og's first
// talk is nine, Grew's eight (twelve with the tooth), Toban's ten.
var (
	WtSiteOg = WtSite{Name: "Og's stand", Stands: []tile.Tile{WtOgAnchor}, Area: WtAreaMain, Ogres: 4, Ticks: 34, Chat: 20, Road: 56}
	// WtSiteBranch is the road to the branch the rope is tied to and the swing: the island's ogres cannot cross the water, the mainland's four are on the bank.
	WtSiteBranch = WtSite{Name: "the branch above the swing", Stands: []tile.Tile{WtBranchStand}, Area: WtAreaMain, Ogres: 4, Ticks: 20, Road: 88}
	// WtSiteGrew is Grew's island: the landing, the talk, the walk to the swing back and the swing.
	WtSiteGrew = WtSite{Name: "Grew's island", Stands: []tile.Tile{WtIslandLanding, WtGrewAnchor, WtSwingStand}, Area: WtAreaIsland, Ogres: 4, Ticks: 40, Chat: 16}
	// WtSiteBerries is the same island walked round its four berry bushes.
	WtSiteBerries = WtSite{Name: "the island's berry bushes", Stands: []tile.Tile{WtBerries[0], WtBerries[1], WtBerries[2], WtBerries[3]}, Area: WtAreaIsland,
		Ogres: 4, Ticks: 50}
	// WtSiteCaveMouth is the stand beside the camp's cave, in the reach of one ogre of the road, the walk in and the cave's op.
	WtSiteCaveMouth = WtSite{Name: "the camp's cave mouth", Stands: []tile.Tile{WtCampCaveStand}, Area: WtAreaMain, Ogres: 1, Ticks: 16, Road: 170}
	// WtSiteCamp is the camp short of the fight: the arrival, the chest (a key on it, a box), Toban's ten-page talk and the ladder.
	WtSiteCamp = WtSite{Name: "Toban's camp", Stands: []tile.Tile{WtCampArrival, WtCampChest, WtTobanAnchor}, Area: WtAreaCamp, Ogres: 4, Ticks: 48, Chat: 26}
	// WtSiteGorad is where Gorad is fought, with the two ogres that reach it at the account's back (WtGoradOdds).
	WtSiteGorad = WtSite{Name: "Gorad's corner of the camp", Stands: []tile.Tile{WtGoradAnchor}, Area: WtAreaCamp, Ogres: 2}
)

// WtSites lists the sites, for the content test.
func WtSites() []WtSite {
	return []WtSite{WtSiteOg, WtSiteBranch, WtSiteGrew, WtSiteBerries, WtSiteCaveMouth, WtSiteCamp, WtSiteGorad}
}

// WtOgreVolley is the most a site's ogres can take off an account in one round of their swings (they are not in step, so this is a ceiling).
func WtOgreVolley(ogres int) int { return ogres * FoeOgre.PeakHit() }

// WtTripOdds prices a trip through ogre sites: for each site, what its ogres cost an account that does not fight back, in expected hitpoints a tick; the
// stretch of a site spent in chats (where nothing can be eaten) must leave the account at or above one volley of the site's ogres, and the whole trip must
// be covered by the hitpoints the account starts with after a top-up and nine tenths of its food, with a volley of the largest site to the good.
func WtTripOdds(s Snapshot, sites ...WtSite) (bool, string) {
	if s.MaxHP <= 0 || len(sites) == 0 {
		return true, ""
	}
	p := fighterOf(s)
	per := wtFoePerTick(FoeOgre, p)
	b := wtBudgetOf(s)
	total, margin := 0.0, 0.0
	for _, site := range sites {
		volley := float64(WtOgreVolley(site.Ogres))
		margin = max(margin, volley)
		unfed := per * float64(site.Ogres*site.Chat)
		if b.hp-unfed < volley {
			return false, fmt.Sprintf("%s holds %d ogres (level 53, a hit of up to %d) and the %d ticks of its talking cost an account at Defence %d about %.0f of the %.0f hitpoints it would have, with no bite to be taken inside a chat (it must keep %.0f)",
				site.Name, site.Ogres, FoeOgre.PeakHit(), site.Chat, s.Level("defence"), unfed, b.hp, volley)
		}
		total += per * float64(site.Ogres*site.Ticks+site.Road)
	}
	if b.hp+b.spare-total < margin {
		return false, fmt.Sprintf("the trip through %s costs an account at Defence %d about %.0f hitpoints among the ogres that hunt there, and this one has %.0f hitpoints and %.0f of food to spend (it must keep %.0f)",
			wtSiteNames(sites), s.Level("defence"), total, b.hp, b.spare, margin)
	}
	return true, ""
}

func wtSiteNames(sites []WtSite) string {
	out := ""
	for i, s := range sites {
		switch {
		case i == 0:
		case i == len(sites)-1:
			out += " and "
		default:
			out += ", "
		}
		out += s.Name
	}
	return out
}

// WtGoradOdds prices Gorad (level 68, 80 hitpoints), fought for his tooth by an explicit attack with the two camp ogres that reach his corner hitting the
// account all the while (WtSiteGorad): the fight's price is the shared model's with the ogres' ticks added (MeleeChainAmid). The hardest single hit it
// must keep above is a round of all three.
func WtGoradOdds(s Snapshot) FightOdds {
	g := FoeGorad
	g.ZapMax = FoeGorad.MaxHit() + WtOgreVolley(WtSiteGorad.Ogres)
	extra := float64(WtSiteGorad.Ogres) * FoePerAttack(FoeOgre, fighterOf(s)) / float64(FoeOgre.AttackTicks)
	return MeleeChainAmid(s, []MeleeFoe{g}, wtSustain, extra)
}

// ---------------------------------------------------------------------------------------------------------------------
// The hunters of the maps the module visits
// ---------------------------------------------------------------------------------------------------------------------

// WtMapSquares are the map squares (x>>6, z>>6) the module's tiles and walks lie in: the surface from the camp's cave to Yanille, the camp, and the two
// underground squares of the skavid caves and the enclave.
var WtMapSquares = [][2]int{{39, 46}, {39, 47}, {39, 48}, {40, 46}, {40, 47}, {40, 48}, {39, 147}, {40, 147}}

// WtHunterKind says how the module deals with a kind of hunter.
type WtHunterKind string

const (
	// WtHunterSite: the ogres, priced by WtSite counts and the pair price.
	WtHunterSite WtHunterKind = "site"
	// WtHunterChieftain: fenced on the surface where a route round exists (WtAvoid), priced where none does (WtChieftainOdds) and inside the enclave.
	WtHunterChieftain WtHunterKind = "chieftain"
	// WtHunterEnclave: priced by the enclave's visits (WtEnclaveOdds).
	WtHunterEnclave WtHunterKind = "enclave"
	// WtHunterMargin: too weak to price: a hit of at most 3, inside the margins every price keeps (the two hardest hits of what is priced).
	WtHunterMargin WtHunterKind = "margin"
	// WtHunterFar: no tile of the plan is within its reach, which for these is exactly where the engine can find an account (wander range plus hunt range from the
	// spawn: HuntIterator measures from the hunter's south-west tile, which wanders that far); the content test checks the distance of every tile, the pack test of
	// every route.
	WtHunterFar WtHunterKind = "far"
	// WtHunterSealed: in a region no tile of the module's walks touches (the pack test floods it).
	WtHunterSealed WtHunterKind = "sealed"
)

// WtHunter is one kind of hunter the maps hold, with the reach (tiles from its spawn) the far and sealed claims are checked against.
type WtHunter struct {
	Name  string
	Kind  WtHunterKind
	Reach int
}

// WtHunterTable is every npc with a hunt mode that stands in the module's map squares, by npc id. The content test lists the squares' npcs and fails on
// any hunter that is not here, so a spawn the price does not know cannot go unnoticed.
var WtHunterTable = map[int]WtHunter{
	114: {"Ogre", WtHunterSite, WtOgreReach},
	115: {"Ogre", WtHunterSite, WtOgreReach},
	852: {"Ogre chieftain", WtHunterChieftain, WtOgreReach},
	55:  {"Blue dragon", WtHunterEnclave, WtDragonReach},
	83:  {"Greater demon", WtHunterEnclave, WtDemonReach},
	90:  {"Skeleton", WtHunterMargin, 1 + 8 + 1},
	// The wolves (level 64, wander 6, hunt 2, a hit of 7 like an ogre's) of the hills south of the camp's cave and of the east of it: far, the routes pass them
	// at a distance (pack test). The giant spiders (level 27, a hit of 3, wander 10, hunt 1) on the camp cave's road and the bear (level 19, a hit of 2) south-east
	// of Gu'Tanoth are too weak to price. The bats of the enclave's second region (level 27, a hit of 3) are in it.
	95:  {"Wolf", WtHunterFar, 6 + 2},
	60:  {"Giant spider", WtHunterMargin, 10 + 1 + 1 + 1},
	106: {"Bear", WtHunterMargin, 12 + 1 + 1 + 1},
	78:  {"Giant bat", WtHunterSealed, 8 + 2 + 2},
}

// WtFarSpawn is the spawn of a hunter the module's tiles and walks are out of the reach of (WtHunterFar).
type WtFarSpawn struct {
	ID int
	At tile.Tile
}

// WtFarSpawns are the far hunters' spawns in the module's map squares: the wolves (npc 95) of the hills south of the camp's cave and of the east of it. The
// content test lists them again from the maps, the pack test walks the routes past them.
var WtFarSpawns = []WtFarSpawn{
	{95, tile.Tile{X: 2498, Z: 2968}}, {95, tile.Tile{X: 2500, Z: 2959}}, {95, tile.Tile{X: 2502, Z: 2969}}, {95, tile.Tile{X: 2506, Z: 2959}}, {95, tile.Tile{X: 2509, Z: 2964}},
	{95, tile.Tile{X: 2602, Z: 2955}}, {95, tile.Tile{X: 2605, Z: 2963}}, {95, tile.Tile{X: 2607, Z: 2967}}, {95, tile.Tile{X: 2610, Z: 2958}}, {95, tile.Tile{X: 2610, Z: 2961}},
	{95, tile.Tile{X: 2610, Z: 2965}},
}

// WtPlanTiles is every tile the plan names to stand on, arrive at or talk from on the surface and in the camp, for the far hunters' distance check.
func WtPlanTiles() []tile.Tile {
	out := []tile.Tile{WtBank, WtWallStand, WtTowerLadderStand, WtBushStand, WtCandle, WtOgAnchor, WtBranch, WtBranchStand, WtIslandLanding, WtGrewAnchor,
		WtSwingStand, WtSwingLanding, WtCampCave, WtCampCaveStand, WtCampArrival, WtCampLadder, WtCampChest, WtTobanAnchor, WtGoradAnchor, WtRelicGateStand,
		WtRelicGateInside, WtStallStand, WtStall, WtWallGuardAnchor, WtBattlementWest, WtBattlementEast, WtJumpStand, WtJumpLanding, WtJumpBackStand,
		WtCityGuardAnchor, WtEastGateStand, WtEastGateInside, WtEnclaveGuard}
	out = append(out, WtBerries[:]...)
	out = append(out, WtCaveStands[:]...)
	out = append(out, WtCaveExits[:]...)
	return out
}
