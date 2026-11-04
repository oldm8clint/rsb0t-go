package combatplan

import (
	"hash/fnv"
	"math"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Spreading a swarm over the ladder (a user request: "Look into more places we can train at ...
// accounts need to prioritise getting food and training ... training to actually happen and be
// quicker").
//
// The measurement behind it, from the live fleet's own log and /api/status on 2 Oct (one hour,
// build 774db79f): 93 of the 315 ScarabSwarm accounts stood on the ONE rung that covers melee
// 25-60 (giants, 12 spawns in the Edgeville dungeon, 31-49 of them per world), 1,082 of the
// fleet's 1,117 "STALLED: no melee xp" lines were that rung, and the median account there earned
// 812 melee XP an hour. That is not a combat-speed problem, it is a supply problem: a spawn tile
// is empty for RespawnTicks after every kill, so the experience a rung can feed is fixed by how
// many spawns it has, no matter how many accounts queue for them.
//
//	supply per spawn-hour = kills an hour * 4 * hitpoints
//	kills an hour         = 3600 / (respawn seconds + the time to kill + walking about)
//
// Twelve Giants (35 hp, respawn 36 s) feed about 96,000 XP an hour in each world; thirty-five
// accounts sharing that is 2,700 XP an hour each, which is what the fleet measured (3,228 mean
// including the minutes spent idle). More rungs in the same band multiply the supply, so the
// answer is more places and a deterministic spread over them, not a faster fighter.
//
// The new rungs below are ground truth read from the content repository (npc configs under
// scripts/, spawn tiles from maps/*.jm2 cross-checked with the loc index, drops from drop
// tables/scripts, multiway zones from maps/multiway.csv), every route planned offline with
// cmd/webwalkplan over the live collision pack. Not one has been walked live by this client:
// the Notes say so and the settings flag `stage5SpreadRungs` switches the whole spread off.

// Assumptions of the supply estimate. They only rank rungs against each other, so they need to be
// roughly right and the same for every rung rather than measured.
const (
	// AssumedKillDPS is the hitpoints a mid-band account removes a second (accuracy times average
	// hit over the attack speed, at melee 40 against a monster of defence 20-47).
	AssumedKillDPS = 1.5
	// KillOverheadSeconds is the walk to the next target and the click, per kill.
	KillOverheadSeconds = 4.0
	tickSeconds         = 0.6
)

// SupplyPerSpawnHour is the melee experience one spawn of m can feed an hour: the hitpoints are
// worth 4 XP each in the trained stat, and the cycle is the respawn wait plus the kill.
func SupplyPerSpawnHour(m Monster) float64 {
	respawn := m.RespawnTicks
	if respawn <= 0 {
		respawn = 60
	}
	cycle := float64(respawn)*tickSeconds + float64(m.Hitpoints)/AssumedKillDPS + KillOverheadSeconds
	if cycle <= 0 {
		return 0
	}
	return 3600 / cycle * float64(m.Hitpoints) * 4
}

// PersonalXPHour is the melee experience one account fighting m alone gains an hour: the kill
// and the walk to the next one, with no wait for a respawn (an uncrowded rung always has another
// monster standing).
func PersonalXPHour(m Monster) float64 {
	cycle := float64(m.Hitpoints)/AssumedKillDPS + KillOverheadSeconds
	if cycle <= 0 {
		return 0
	}
	return 3600 / cycle * float64(m.Hitpoints) * 4
}

// SupplyXPH is what the whole rung can feed one world an hour. It is the weight the spread
// divides accounts by.
func (r Rung) SupplyXPH() float64 {
	return float64(r.SpawnCount()) * SupplyPerSpawnHour(r.Monster)
}

// AccountsAt is how many accounts the rung feeds at perAccountXPH each, per world.
func (r Rung) AccountsAt(perAccountXPH float64) int {
	if perAccountXPH <= 0 {
		return 0
	}
	return int(r.SupplyXPH() / perAccountXPH)
}

// The rungs the spread adds. They overlap the existing bands on purpose: PickRung only ever
// returned the deepest one, which is how every account ended on the same rung.
var spreadRungs = []Rung{
	{
		Key: "barbarians", Monster: MonsterBarbarian,
		// 3080,3427 is in the middle of the village's spawn ring: the planner reaches it from the
		// Edgeville bank in 73 (cmd/webwalkplan, 2 Oct). Barbarians wander 10 and chase 12 from
		// their spawn, so a wide leash is what keeps a fight inside the rung. Gunthor the Brave
		// (lvl 29, another name) stands in the same streets and is not a target. The five Barbarian
		// WOMEN (lvl 8: 14 hp, respawn 50, the same drops) are: MonsterBarbarian names them (Also, 6 Oct
		// 2026, "accounts training at barbarians dont consider attacking the barbarian women"), and
		// their spawns, from content m48_53 (npc 17), are in the list below.
		Stand: tile.Tile{X: 3080, Z: 3427},
		Spawns: []tile.Tile{
			{X: 3076, Z: 3414}, {X: 3078, Z: 3419}, {X: 3078, Z: 3436}, {X: 3078, Z: 3442},
			{X: 3079, Z: 3409}, {X: 3079, Z: 3437}, {X: 3079, Z: 3444}, {X: 3080, Z: 3423},
			{X: 3083, Z: 3429}, {X: 3085, Z: 3409}, {X: 3086, Z: 3440},
			// The Barbarian women (npc 17), m48_53 local 3,28 3,53 6,48 12,34 14,27: all within the leash.
			{X: 3075, Z: 3420}, {X: 3075, Z: 3445}, {X: 3078, Z: 3440}, {X: 3084, Z: 3426}, {X: 3086, Z: 3419},
		},
		Leash: 20, FromLevel: 5, ToLevel: 25, MinCombatLevel: 3,
		Members: false, TravelProven: true,
		Bank:     tile.Tile{X: 3095, Z: 3491},
		Entrance: "Barbarian Village, open ground south of the Edgeville bank",
		Note:     "11 Barbarians and 5 Barbarian women (lvl 7 and 8, 14 hp, max hit 2, not aggressive, respawn 30 s): half the XP per spawn of an Al-Kharid warrior but nearly twice the spawns, plain Bones, chaos 2 at 5/128. A multiway zone. Route planned offline (cost 73 from the bank), not walked live",
	},
	{
		Key: "whiteknights", Monster: MonsterWhiteKnight,
		// The ground-floor courtyard of Falador castle. Two more floors hold 23 more White Knights
		// behind stairs this ladder has not walked, so they are not counted. Eight of the eleven
		// ground spawns sit inside 11 tiles of this stand.
		Stand: tile.Tile{X: 2978, Z: 3341},
		Spawns: []tile.Tile{
			{X: 2972, Z: 3345}, {X: 2973, Z: 3347}, {X: 2976, Z: 3339}, {X: 2978, Z: 3348},
			{X: 2983, Z: 3330}, {X: 2983, Z: 3334}, {X: 2983, Z: 3343}, {X: 2987, Z: 3341},
		},
		Leash: 12, FromLevel: 25, ToLevel: 60, MinCombatLevel: 30,
		Members: false, TravelProven: true,
		Bank:     tile.Tile{X: 2946, Z: 3368},
		Entrance: "Falador castle courtyard, 63 tiles from the Falador west bank, no door on the planned route",
		Note:     "8 White Knights (lvl 36, 52 hp, max hit 6, not aggressive, respawn a minute): the slowest respawn on the ladder but the safest ground, in a bank's reach. A multiway zone. Drops mind, nature, chaos, blood, law and coins, plain Bones. Route planned offline, not walked live",
	},
	{
		Key: "icewarriors", Monster: MonsterIceWarrior,
		// The warriors fill the same cave as the ice giants rung (stand 3058,9568): 11 spawns
		// between x 3044-3062 and z 9566-9590 against that rung's 3, and the planner reaches 3054,9578
		// from the giants' stand in 11. Single-combat, nothing else spawns in the cave.
		Stand: tile.Tile{X: 3054, Z: 9578},
		Spawns: []tile.Tile{
			{X: 3044, Z: 9581}, {X: 3046, Z: 9575}, {X: 3048, Z: 9583}, {X: 3049, Z: 9590},
			{X: 3052, Z: 9568}, {X: 3053, Z: 9576}, {X: 3056, Z: 9583}, {X: 3056, Z: 9587},
			{X: 3057, Z: 9566}, {X: 3062, Z: 9576}, {X: 3062, Z: 9581},
		},
		//
		// GATED AT 60 HITPOINTS AND A HERD (17 Oct 2026). Nine accounts died here in the logs of 15-17 Oct: 9
		// deaths in 327 account-hours against none on any other trainer rung, at the lowest kill rate on the ladder
		// (6.6 kills an hour against 18 to 140) and 31 bites an hour, every one of them with a random event monster
		// beside it and the herd round it. The fleet's accounts were 48 to 55 hitpoints. The guardian can now leave
		// an event's leash from the stand (randomevents/event_leash.go), but the cave is a bad place to be wrong
		// in: the only exit is a ladder 97 steps away, and a pack of 5 or 11 bites does not outlast ten minutes of
		// it. Until an account has the hitpoints for it, the other rungs of the band take it.
		Leash: 14, FromLevel: 40, ToLevel: 75, MinCombatLevel: 50, MinHitpoints: 60, Herd: true,
		Members: false, TravelProven: true,
		Bank:     tile.Tile{X: 3013, Z: 3354},
		Entrance: "Asgarnian ice dungeon ladder at 3008,3150 (loc 1759), the same entrance and bank the icegiants rung has walked",
		Note:     "11 Ice warriors (lvl 57, 59 hp, max hit 6, aggressive, respawn 36 s) beside the ice giants: the most XP per spawn of any rung a band 40-60 account can take, chaos 3 at 8/128 and death 2 at 3/128 per kill. Only the cave is new; the route to it is the icegiants rung's",
	},
}

// THE GIANT SPREAD ("trainspots-1003", a user request: "We spend too long at cows, chickens and
// goblins; move to giants sooner" and "find more giant spawns across the map to spread accounts out
// even further, so prayer levels come much faster (big bones)").
//
// Sooner: the giants rung's band starts at melee 20 on this ladder instead of 25, for an account
// with 30 base hitpoints (MinHitpointsBelow 25 keeps the old band ungated). The trainer only gets
// an account once every melee stat has left the Ardougne pens at stage5CowsMeleeTo (now 20, was
// 30: scarabplan.DefaultStage5CowsMeleeTo), so this is the band that makes 20 mean "giants".
// Barbarians and the Al-Kharid warriors still cover 20-24, so an account under 30 hitpoints (or
// under the rung's combat 30) has somewhere to go and the spread can still send others there when
// the giants are full. Estimated with the usual accuracy and max-hit rolls against a Giant (attack
// 18 +18, defence 26, max hit 4) and not measured: an account with a mithril scimitar and armour at
// melee 20 loses about 17 hitpoints a kill against 10 at 25 and 7 at 30, so a kill costs it about
// twice the food, which is why the early band is gated on hitpoints (eat at half of 30 leaves 15,
// nearly four max hits) and not on the band alone. MeleeLadder (the standalone CombatTrainer's)
// keeps 25.
//
// Further: every Giant, Moss giant, Ice giant and Ogre group in content with enough spawns that a
// plain walk reaches (cmd/webwalkplan over the collision pack), off and on the Wilderness, is a
// rung here. What content holds and why the rest are not rungs is in docs/trainspots_2026-10-03.md:
// the Tree Gnome Village's three Giants are two a maze wall apart, the Wilderness Giants at level
// 42-45 have two in reach beside two Lesser demons (maxrange 16), the level-17 pair is two, the
// dungeon six are inside the PvP strand site, and the Ice giants north of Catherby, the Ice Queen's
// lair and every blue dragon are unreachable on foot.

// earlyGiantsFrom is where the spread ladder's giants band starts, and earlyGiantsHitpoints the
// base hitpoints it asks below the old start of 25.
const (
	earlyGiantsFrom      = 20
	earlyGiantsHitpoints = 30
)

// westRungs are the big-bones rungs on Scarab's own side of the map: an account coming off a Stage 5
// magic slice in Ardougne fights there with no ship crossing.
var westRungs = []Rung{
	{
		Key: "mossgiants-ardougne", Monster: MonsterMossGiant,
		// The four Moss giants west of the North Ardougne goblins (content m39_53), the herd Stage 3's
		// magic pen MossGiantsNorthArdougne trains Magic on (lab mg66a, 0 deaths). Planned offline:
		// 86 from the East Ardougne north bank, a plain walk.
		Stand:  tile.Tile{X: 2559, Z: 3405},
		Spawns: []tile.Tile{{X: 2549, Z: 3408}, {X: 2554, Z: 3401}, {X: 2554, Z: 3409}, {X: 2556, Z: 3406}},
		Leash:  10, FromLevel: 40, ToLevel: 60, MinCombatLevel: 45, MinHitpoints: 40,
		Members: true, TravelProven: true, WestSide: true,
		Bank:     tile.Tile{X: 2616, Z: 3332},
		Entrance: "open ground north of East Ardougne, 86 from the north bank, no door",
		Note:     "4 Moss giants (lvl 42, 60 hp, max hit 6, aggressive, respawn 36 s), big bones every kill. Shared with Stage 3's magic pen on the same herd. Route planned offline, not walked live by the trainer",
		SafeSpot: tile.Tile{X: 2563, Z: 3401}, SafeSpotSpawns: 2,
	},
}

// wildernessRungs are giant groups on Wilderness ground, offered only with SpreadOptions.Wilderness.
// Both are deep (PvP from almost any combat level), both bank at Edgeville three hundred tiles and
// more south, and in the Wilderness the cowardly hunt mode attacks every player whatever its level.
var wildernessRungs = []Rung{
	{
		Key: "mossgiants-wilderness", Monster: MonsterMossGiant,
		// Content m49_59: 3139,3818 3140,3805 3141,3826, Wilderness 36-39. A Skeleton (lvl 22,
		// wanderrange 9) spawns at 3143,3802.
		Stand:  tile.Tile{X: 3140, Z: 3816},
		Spawns: []tile.Tile{{X: 3139, Z: 3818}, {X: 3140, Z: 3805}, {X: 3141, Z: 3826}},
		Leash:  11, FromLevel: 40, ToLevel: 60, MinCombatLevel: 50, MinHitpoints: 45,
		Members: false, TravelProven: true, Wilderness: 39,
		Bank:     tile.Tile{X: 3094, Z: 3491},
		Entrance: "north from Edgeville over the ditch, 326 from the bank through Wilderness 1-39, planned offline",
		Note:     "WILDERNESS 36-39: player killers can attack from almost any combat level, a death drops everything but three items. 3 Moss giants, big bones. Off unless stage5WildernessRungs",
		SafeSpot: tile.Tile{X: 3146, Z: 3818}, SafeSpotSpawns: 2,
	},
	{
		Key: "icegiants-wilderness", Monster: MonsterIceGiant,
		// Content m46_60: five Ice giants at 2947-2958,3889-3902 (Wilderness 47-48) on the frozen
		// plateau, with thirteen Ice warriors (huntrange 5) at 2945-2963,3857-3887 just south and more
		// Ice giants and warriors further north (levels 50-55), an Ice spider at 2959,3917.
		Stand:  tile.Tile{X: 2953, Z: 3895},
		Spawns: []tile.Tile{{X: 2947, Z: 3895}, {X: 2952, Z: 3902}, {X: 2953, Z: 3889}, {X: 2954, Z: 3894}, {X: 2958, Z: 3898}},
		Leash:  8, FromLevel: 50, ToLevel: 75, MinCombatLevel: 60, MinHitpoints: 50,
		Members: false, TravelProven: true, Wilderness: 48,
		Bank:     tile.Tile{X: 3094, Z: 3491},
		Entrance: "north-west from Edgeville, 405 from the bank through Wilderness 1-48, planned offline",
		Note:     "WILDERNESS 47-48: the deepest rung on the ladder, PvP from any combat level and Ice warriors (huntrange 5) on the way in. 5 Ice giants (lvl 53, 70 hp, max hit 7), big bones. Off unless stage5WildernessRungs",
		SafeSpot: tile.Tile{X: 2945, Z: 3903}, SafeSpotSpawns: 3,
	},
}

// SpreadOptions picks the optional parts of the spread ladder.
type SpreadOptions struct {
	// Wilderness adds wildernessRungs (stage5WildernessRungs; off by default).
	Wilderness bool
}

// SpreadLadder is the default spread ladder: SpreadLadderWith with no Wilderness rungs.
func SpreadLadder() []Rung { return SpreadLadderWith(SpreadOptions{}) }

// SpreadLadderWith is MeleeLadder with the giants band brought down to melee 20 (earlyGiantsFrom,
// gated on earlyGiantsHitpoints) and the Yanille Ogres taken as planned, plus the rungs above: the
// ladder the swarm's Stage 5 picks from with PickRungSpread. MeleeLadder itself is unchanged so
// everything that reads it, and the pinned "rung" setting, behaves exactly as before.
func SpreadLadderWith(o SpreadOptions) []Rung {
	out := MeleeLadder()
	for i := range out {
		switch out[i].Key {
		case "giants":
			out[i].FromLevel = earlyGiantsFrom
			out[i].MinHitpoints, out[i].MinHitpointsBelow = earlyGiantsHitpoints, 25
		case "ogres":
			// Planned offline over the collision pack (116 from the Yanille bank through the two west
			// gates), the same proof the spread rungs of 2 Oct went live on. Melee 45: an Ogre is a
			// Moss giant's level with one more max hit, and the band is where the free giants thin out.
			out[i].TravelProven, out[i].WestSide = true, true
			out[i].FromLevel, out[i].MinCombatLevel, out[i].MinHitpoints = 45, 50, 40
		}
	}
	out = append(out, spreadRungs...)
	out = append(out, westRungs...)
	if o.Wilderness {
		out = append(out, wildernessRungs...)
	}
	return out
}

// Crowd is what a pick knows about the other accounts, and about the account itself beyond its band.
type Crowd struct {
	// Account is the picker's own name; it only seeds the tie-break so one account always breaks
	// a tie the same way.
	Account string
	// Loads is accounts seated per rung key in this account's own world (RungBoard.Loads), this
	// account excluded. Nil or empty means nothing is known and the pick falls back to a weighted
	// hash of the name, which still spreads a cohort that all picks at once.
	Loads map[string]int
	// Avoid names rungs this account has just stalled at (StallAvoidFor): left out while another
	// rung qualifies.
	Avoid map[string]bool
	// Hitpoints is the account's base Hitpoints level, for the rungs' MinHitpoints gates; 0 (not
	// known) refuses every gated rung.
	Hitpoints int
	// West is whether the account stands on Scarab's side of the map (WestSide): WestSide rungs are
	// offered only then, and a rung across the water is charged CrossPenalty.
	West bool
	// SeatsPerSpawn caps every rung at that many accounts a spawn on this world (Rung.Cap): a rung with Loads at
	// or over its cap is passed over while another usable rung has a seat, and with every rung full the least
	// full one is taken (RungChoice.Full). 0 is no cap, the pick of 2 Oct.
	SeatsPerSpawn int
}

// CrossPenalty weighs a rung across the water from a West account: the ship crossing costs 15-25
// minutes of a 120-minute melee slice, about a fifth of what the rung could feed it.
const CrossPenalty = 1.25

// usableRungs are the in-band rungs the account may take now: PickRung's own filters, all of them
// rather than the deepest, plus the spread's hitpoints and side gates (Rung.Admits).
func usableRungs(ladder []Rung, lowestMelee, combatLevel int, membersWorld bool, c Crowd) []Rung {
	a := Account{LowestMelee: lowestMelee, CombatLevel: combatLevel, Hitpoints: c.Hitpoints, Members: membersWorld, West: c.West}
	var out []Rung
	for _, r := range ladder {
		if r.Admits(a) {
			out = append(out, r)
		}
	}
	return out
}

// overflowRungs are the rungs below the account's band (their whole band under its lowest melee
// level) that it passes every other gate of, Cows excepted (FromLevel under 5: the cow field is
// Stage 3's and Stage 5's own Cows-first ground). A weaker monster is always safe for an account
// above its band, and when the band's own rungs are crowded it feeds more experience an hour:
// on 50ec96f0's log (6 Oct 2026) 22-33 accounts a world shared the Giants' 12 spawns and 9-22 the
// White Knights' 8 at about 2,000 melee XP an hour each, while the Barbarians' 11 spawns and Al
// Kharid's 9 held 1-3 accounts.
func overflowRungs(ladder []Rung, lowestMelee, combatLevel int, membersWorld bool, c Crowd) []Rung {
	a := Account{LowestMelee: lowestMelee, CombatLevel: combatLevel, Hitpoints: c.Hitpoints, Members: membersWorld, West: c.West}
	var out []Rung
	for _, r := range ladder {
		if r.OverflowFor(a) {
			out = append(out, r)
		}
	}
	return out
}

// OverflowFor is whether r is a rung below a's band that a may overflow to (overflowRungs): its
// whole band under a's lowest melee level, not the Cows, and every other gate passed.
func (r Rung) OverflowFor(a Account) bool {
	if r.FromLevel < 5 || r.ToLevel > a.LowestMelee {
		return false
	}
	in := a
	in.LowestMelee = r.FromLevel
	return r.Admits(in)
}

// expectedXPH is what one more account at r should expect an hour: the rung's supply shared by
// everyone seated there and it, never more than one account alone can kill, and less for a rung
// across the water from a West account (CrossPenalty).
func expectedXPH(r Rung, c Crowd) float64 {
	xph := math.Min(r.SupplyXPH()/float64(c.Loads[r.Key]+1), PersonalXPHour(r.Monster))
	if c.West && !r.WestSide {
		xph /= CrossPenalty
	}
	return xph
}

// PickRungSpread chooses among every usable rung for the band by how full each is, so a swarm
// spreads over the ladder in proportion to what each rung can feed. With a Crowd it fills the rung
// with the lowest (seated+1)/supply first; without one it hashes the account name against each
// rung's supply (weighted rendezvous hashing), so the split is stable, needs no shared state and
// moves an account only when the rung it was on stops qualifying. When nothing is usable it answers
// exactly as PickRung does, reason included.
func PickRungSpread(ladder []Rung, lowestMelee, combatLevel int, membersWorld bool, c Crowd) RungChoice {
	pool := usableRungs(ladder, lowestMelee, combatLevel, membersWorld, c)
	if len(pool) == 0 {
		// PickRung names the reason, but it knows nothing of the spread's own gates: a rung it would
		// still take was refused here for the account's hitpoints or its side of the map.
		ch := PickRung(ladder, lowestMelee, combatLevel, membersWorld)
		if ch.OK {
			why := BlockedHitpoints
			if ch.Rung.WestSide && !c.West {
				why = BlockedSide
			}
			return RungChoice{Why: why, Detail: ch.Rung.Key}
		}
		return ch
	}
	if len(c.Loads) > 0 {
		// With the loads known, a crowded band may overflow to the rungs below it; the pick is then
		// the rung with the most experience an hour for this account (expectedXPH).
		pool = append(pool, overflowRungs(ladder, lowestMelee, combatLevel, membersWorld, c)...)
	}
	if len(c.Avoid) > 0 {
		var kept []Rung
		for _, r := range pool {
			if !c.Avoid[r.Key] {
				kept = append(kept, r)
			}
		}
		if len(kept) > 0 {
			pool = kept
		}
	}
	if len(c.Loads) > 0 || c.SeatsPerSpawn > 0 {
		if c.SeatsPerSpawn > 0 {
			// A rung at its cap has no seat for one more: the rungs with a seat are the only ones weighed.
			var open []Rung
			for _, r := range pool {
				if c.Loads[r.Key] < r.Cap(c.SeatsPerSpawn) {
					open = append(open, r)
				}
			}
			if len(open) > 0 {
				pool = open
			} else {
				// Every usable rung is full: the least full against its cap, so an account always trains somewhere.
				least, leastFill := pool[0], math.Inf(1)
				for _, r := range pool {
					f := float64(c.Loads[r.Key]) / float64(r.Cap(c.SeatsPerSpawn))
					if f < leastFill || (f == leastFill && (expectedXPH(r, c) > expectedXPH(least, c) ||
						(expectedXPH(r, c) == expectedXPH(least, c) && hashUnit(c.Account, r.Key) < hashUnit(c.Account, least.Key)))) {
						least, leastFill = r, f
					}
				}
				return RungChoice{Rung: least, OK: true, Full: true}
			}
		}
		best, bestXPH := pool[0], -1.0
		for _, r := range pool {
			x := expectedXPH(r, c)
			if x > bestXPH || (x == bestXPH && hashUnit(c.Account, r.Key) < hashUnit(c.Account, best.Key)) {
				best, bestXPH = r, x
			}
		}
		return RungChoice{Rung: best, OK: true}
	}
	best, bestScore := pool[0], math.Inf(1)
	for _, r := range pool {
		score := spreadScore(r, c)
		if score < bestScore || (score == bestScore && hashUnit(c.Account, r.Key) < hashUnit(c.Account, best.Key)) {
			best, bestScore = r, score
		}
	}
	return RungChoice{Rung: best, OK: true}
}

// spreadScore is lower for the rung to prefer.
func spreadScore(r Rung, c Crowd) float64 {
	w := r.SupplyXPH()
	if w <= 0 {
		return math.Inf(1)
	}
	if c.West && !r.WestSide {
		w /= CrossPenalty // the ship crossing eats part of the slice this rung would feed
	}
	if len(c.Loads) > 0 {
		return float64(c.Loads[r.Key]+1) / w
	}
	// Weighted rendezvous: the rung with the smallest -ln(u)/w wins with probability w/sum(w).
	u := hashUnit(c.Account, r.Key)
	if u <= 0 {
		u = 1e-12
	}
	return -math.Log(u) / w
}

// hashUnit maps (account, rung) to a stable number in (0,1).
func hashUnit(account, rung string) float64 {
	h := fnv.New64a()
	h.Write([]byte(strings.ToLower(account)))
	h.Write([]byte{0})
	h.Write([]byte(rung))
	return (float64(h.Sum64()>>11) + 0.5) / float64(uint64(1)<<53)
}

// DefaultRungSeatsPerSpawn is how many accounts one spawn of a melee rung seats (setting stage5RungSeatsPerSpawn,
// 1-6). One, as the Magic pens have seated one a spawn since 7 Oct 2026 (scarabplan.MidSeatsPerSpawn,
// DefaultGiantSeatsPerSpawn): a spawn feeds one kill every respawn-plus-kill to ONE account however many wait on
// it, and the spread of 2 Oct, which only weighed the rungs against each other, put 14 accounts on the 12 Giants'
// spawns of one world at 03:42 on 7 Oct ("giants 14/12 spawns"; owner: accounts "are still crowding at NPC
// training spots"). The spare accounts go to the rungs below their band (overflowRungs), the other rungs in it,
// and when every one is full the least full.
const DefaultRungSeatsPerSpawn = 1

// Cap is the most accounts the rung seats on one world at seatsPerSpawn a spawn, at least one.
func (r Rung) Cap(seatsPerSpawn int) int { return max(r.SpawnCount()*max(seatsPerSpawn, 1), 1) }

// RungSeatTTL is how long a seat counts without being refreshed: a trainer that died or walked off
// without leaving stops weighing on the rung. A trainer refreshes its seat on every pass (RungBoard.Sit); the
// walk to a rung has its own hold (RungWalkHold).
const RungSeatTTL = 10 * time.Minute

// RungWalkHold is how long a seat taken for a rung the account has yet to walk to counts without a refresh
// (RungBoard.SitFor): the bank visit, the ship crossing and the walk in, with a margin. Without it a rung read
// as emptier than it was while the accounts bound for it were on the road, and the next accounts took the same
// seats (the 7 Oct 2026 finding that gave the Magic pens PenWalkHold).
const RungWalkHold = 20 * time.Minute

type rungSeat struct {
	world int
	rung  string
	at    time.Time // the last refresh
	since time.Time // when the account took this rung: the order of the seats
	hold  time.Time // the seat counts until at least here (a walk to the rung); the zero time for none
}

func (s rungSeat) live(now time.Time) bool {
	return now.Sub(s.at) <= RungSeatTTL || now.Before(s.hold)
}

// RungBoard is which swarm account trains at which rung, for the whole process (the shape of
// scarabplan.PenBoard, kept here because this package cannot import it), with the same seat rules: a seat keeps
// its Since while the account stays on the rung, SitFor holds a seat for the walk, TrySit seats an account only
// under the rung's cap, and Senior says how many seats are ahead of an account's own, so a rung over its cap
// loses its newest accounts and not all of them. Safe for concurrent use.
type RungBoard struct {
	mu    sync.Mutex
	seats map[string]rungSeat
}

// NewRungBoard returns an empty board.
func NewRungBoard() *RungBoard { return &RungBoard{seats: map[string]rungSeat{}} }

// seat records account at rung, keeping Since and the walk hold while it stays on the same rung and world.
func (b *RungBoard) seat(account string, world int, rung string, now time.Time, hold time.Duration) {
	key := strings.ToLower(account)
	n := rungSeat{world: world, rung: rung, at: now, since: now}
	if old, ok := b.seats[key]; ok && old.live(now) && old.rung == rung && old.world == world {
		n.since, n.hold = old.since, old.hold
	}
	if hold > 0 {
		if h := now.Add(hold); h.After(n.hold) {
			n.hold = h
		}
	}
	b.seats[key] = n
}

// Sit seats account at rung in world, replacing any earlier seat, and refreshes its time.
func (b *RungBoard) Sit(account string, world int, rung string, now time.Time) {
	if b == nil || account == "" || rung == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seat(account, world, rung, now, 0)
}

// SitFor is Sit for an account that has yet to walk to the rung: the seat holds for `hold` without a refresh.
func (b *RungBoard) SitFor(account string, world int, rung string, now time.Time, hold time.Duration) {
	if b == nil || account == "" || rung == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seat(account, world, rung, now, hold)
}

// TrySit seats account at rung like SitFor only while fewer than capacity OTHER accounts hold it on that
// world, and reports whether it did. Atomic: of two accounts choosing the last seat in the same instant one gets
// it and the other is told no. An account already seated there is always let stay.
func (b *RungBoard) TrySit(account string, world int, rung string, capacity int, now time.Time, hold time.Duration) bool {
	if b == nil || account == "" || rung == "" {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	me := strings.ToLower(account)
	n := 0
	for a, s := range b.seats {
		if !s.live(now) {
			delete(b.seats, a)
			continue
		}
		if a != me && s.world == world && s.rung == rung {
			n++
		}
	}
	if old, ok := b.seats[me]; !(ok && old.live(now) && old.rung == rung && old.world == world) && n >= capacity {
		return false
	}
	b.seat(account, world, rung, now, hold)
	return true
}

// Leave removes account's seat.
func (b *RungBoard) Leave(account string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.seats, strings.ToLower(account))
}

// Senior is, for account's own live seat, the rung, its world, and how many OTHER accounts seated at the same
// rung on the same world took their seat before it (a tie by name): the places ahead of it. ok is false when it
// holds no live seat. A rung over its cap loses the accounts whose Senior is at or above the cap, the newest.
func (b *RungBoard) Senior(account string, now time.Time) (rung string, world, senior int, ok bool) {
	if b == nil {
		return "", 0, 0, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	me := strings.ToLower(account)
	mine, has := b.seats[me]
	if !has || !mine.live(now) {
		return "", 0, 0, false
	}
	for a, s := range b.seats {
		if a == me || !s.live(now) || s.world != mine.world || s.rung != mine.rung {
			continue
		}
		if s.since.Before(mine.since) || (s.since.Equal(mine.since) && a < me) {
			senior++
		}
	}
	return mine.rung, mine.world, senior, true
}

// Loads counts the live seats per rung in world, leaving out the asking account.
func (b *RungBoard) Loads(except string, world int, now time.Time) map[string]int {
	out := map[string]int{}
	if b == nil {
		return out
	}
	except = strings.ToLower(except)
	b.mu.Lock()
	defer b.mu.Unlock()
	for name, s := range b.seats {
		if name == except || s.world != world || !s.live(now) {
			continue
		}
		out[s.rung]++
	}
	return out
}

// StallAvoidFor is how long a rung is left alone after a run there ended for lack of melee XP: it
// was too crowded or its target was out of reach, and sending the same account straight back is
// how 76 accounts spent forty minutes "waiting for one we can reach" on 29 Sep.
const StallAvoidFor = 45 * time.Minute

// StallEnd reports whether a trainer's end reason is the no-melee-XP stall.
func StallEnd(reason string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(reason)), "no melee xp")
}

// AvoidEnd reports whether a trainer's end reason says the rung itself was the trouble: the
// no-melee-XP stall (too crowded, or its targets out of reach), a walk that never arrived, or a run that left an
// aggressive herd rung for want of a food load (HerdFoodShort). An account that ended for want of food elsewhere
// has nothing against the rung, and a death is not an end reason at all: it is avoided for DeathAvoidFor
// (the caller records that itself).
func AvoidEnd(reason string) bool {
	r := strings.ToLower(strings.TrimSpace(reason))
	return StallEnd(r) || strings.Contains(r, "could not reach rung") || strings.HasPrefix(r, HerdFoodShort)
}

// DeathAvoidFor is how long a rung is left alone after an account died at it. AvoidEnd ignored deaths ("nothing
// against the rung"), so every death in the Ice warrior cave sent the respawned account straight back through the
// bank to the same stand (nine of nine did). A death is the strongest thing a rung can say against itself; the
// avoid is advice (PickRungSpread passes it over only while another rung qualifies), so an account with nowhere
// else to go still trains.
const DeathAvoidFor = 3 * time.Hour

// AvoidSet is the rung keys still avoided at now, from key -> avoided-until.
func AvoidSet(until map[string]time.Time, now time.Time) map[string]bool {
	var out map[string]bool
	for k, t := range until {
		if now.Before(t) {
			if out == nil {
				out = map[string]bool{}
			}
			out[k] = true
		}
	}
	return out
}

// WantBones reports whether a kill's bone is worth picking up now: a big bone always (the giant
// rungs' Prayer feed has always been taken), a plain Bones only while Prayer is short of its goal,
// since the 4.5 XP is not worth a pickup walk once the goal is met.
func WantBones(m Monster, prayerLevel, prayerGoal int) bool {
	switch {
	case m.BigBones:
		return true
	case m.PlainBones:
		return prayerGoal > 0 && prayerLevel < prayerGoal
	}
	return false
}
