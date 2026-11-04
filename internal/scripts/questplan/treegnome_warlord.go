package questplan

import (
	"sort"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// The Khazard warlord fight (Tree Gnome Village stage 7), the pure half. On 4 Oct 2026 the step
// killed acct18, moderator and acct81 (deaths 1 to 4 on "tree-warlord"): the fight kit's food
// lines were optional and the mule had none, no line packed runes, so fightBest found no
// castable spell and meleed a 170-hitpoint warlord that hits 9 at combat 49.
//
// THE WARLORD, FROM CONTENT (quest_tree/configs/gnome_battlefield.npc [khazard_warlord], npc 477;
// spawn maps/m38_51.jm2 "0 25 38: 477", which is 2457,3302,0):
//
//	wanderrange=9 maxrange=10 attack=75 defence=80 strength=78 hitpoints=170 respawnrate=300
//
// No huntrange or huntmode: Engine-TS's NpcType defaults are huntrange 0 and huntmode -1, so he
// never attacks first; walking past him is safe. No magic level and no magicdefence param, so he
// defends magic at level 1 (a caster hits him ~87% of the time). Strength 78 with no bonus is a
// maximum hit of 9 (the npc formula: (78+8)*64/640 + 0.5).
//
// HOW HE IS KILLED (the owner's tactic, Engine-TS 289 Npc.ts the authority). Hit by a spell he
// retaliates (OPPLAYER2, an op trigger), and Npc.validateTarget drops a target that
// targetWithinMaxRange refuses: for an op trigger, a target more than maxrange+1 (11) tiles
// (Chebyshev) from his SPAWN, never from where he stands. So an account casting from 12 or more
// tiles off his spawn is never chased and never hit: he turns back to his wander ("walks
// backwards when hit"). The danger is the caster's own approach: a hand cast (OPNPCT) whose
// target is out of reach or sight when the server reads it walks the caster towards him
// (castreach.go), and that walk drags the account inside the 11 tiles where he may chase and
// hit. So the account casts only from WarlordCastTiles at a warlord SafeCastableMoving reaches,
// watches its tile after every cast, and walks straight back out when it moved.

// The warlord's content facts.
const (
	WarlordWanderRange = 9
	WarlordMaxRange    = 10
	WarlordHitpoints   = 170
	WarlordMaxHit      = 9
	// WarlordRespawn is respawnrate=300 ticks.
	WarlordRespawn = 300 * 600 * time.Millisecond
)

// WarlordSpawn is his spawn tile (WarlordAnchor).
var WarlordSpawn = WarlordAnchor

// WarlordSafeDist is the least Chebyshev distance from his spawn at which he cannot keep an
// account as his target: maxrange+1 is still a valid target, one more is not.
const WarlordSafeDist = WarlordMaxRange + 2

// WarlordCastRing is how far beyond WarlordSafeDist a cast tile may lie: further out a spell
// reaches less of his ground.
const WarlordCastRing = 3

// WarlordStandFlood is how far from his spawn a walk to a cast tile may stray: the cast ring and
// some room to go round a wall.
const WarlordStandFlood = WarlordSafeDist + WarlordCastRing + 8

// WarlordOutOfReach reports whether an account on t is beyond the warlord's reach: on another
// level, or WarlordSafeDist or more from his spawn.
func WarlordOutOfReach(t tile.Tile) bool {
	if t.Level != WarlordSpawn.Level {
		return true
	}
	return tile.Chebyshev(tile.Tile{X: t.X, Z: t.Z}, tile.Tile{X: WarlordSpawn.X, Z: WarlordSpawn.Z}) >= WarlordSafeDist
}

// WarlordRoam is the warlord's ground: the tiles within his wander range of his spawn that
// reach (a walk from the spawn) accepts, the spawn first.
func WarlordRoam(reach func(t tile.Tile) bool) []tile.Tile {
	out := []tile.Tile{WarlordSpawn}
	for z := WarlordSpawn.Z - WarlordWanderRange; z <= WarlordSpawn.Z+WarlordWanderRange; z++ {
		for x := WarlordSpawn.X - WarlordWanderRange; x <= WarlordSpawn.X+WarlordWanderRange; x++ {
			t := tile.Tile{X: x, Z: z, Level: WarlordSpawn.Level}
			if t == WarlordSpawn || (reach != nil && !reach(t)) {
				continue
			}
			out = append(out, t)
		}
	}
	return out
}

// WarlordCastTile is a tile to cast at the warlord from, with how much of his ground a spell
// from it reaches.
type WarlordCastTile struct {
	Tile  tile.Tile
	Cover int
}

// WarlordCastTiles is every tile stand accepts (walkable and reached by the account) in the ring
// WarlordSafeDist..WarlordSafeDist+WarlordCastRing off his spawn from which a spell reaches
// (scarabplan.CastableFrom) at least one tile of roam, the most cover first, then nearest the
// spawn, then by position. A nil sight casts at nothing: no tile is returned.
func WarlordCastTiles(stand func(t tile.Tile) bool, roam []tile.Tile, sight func(x, z int) fleetplan.Sight) []WarlordCastTile {
	if sight == nil {
		return nil
	}
	var out []WarlordCastTile
	r := WarlordSafeDist + WarlordCastRing
	for z := WarlordSpawn.Z - r; z <= WarlordSpawn.Z+r; z++ {
		for x := WarlordSpawn.X - r; x <= WarlordSpawn.X+r; x++ {
			t := tile.Tile{X: x, Z: z, Level: WarlordSpawn.Level}
			if !WarlordOutOfReach(t) || (stand != nil && !stand(t)) {
				continue
			}
			cover := 0
			for _, w := range roam {
				if fleetplan.CastableFrom(t, w, sight) {
					cover++
				}
			}
			if cover > 0 {
				out = append(out, WarlordCastTile{Tile: t, Cover: cover})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Cover != b.Cover {
			return a.Cover > b.Cover
		}
		da, db := tile.Chebyshev(a.Tile, WarlordSpawn), tile.Chebyshev(b.Tile, WarlordSpawn)
		if da != db {
			return da < db
		}
		if a.Tile.Z != b.Tile.Z {
			return a.Tile.Z < b.Tile.Z
		}
		return a.Tile.X < b.Tile.X
	})
	return out
}

// isWarlordCastTile reports whether t is one of cands.
func isWarlordCastTile(t tile.Tile, cands []WarlordCastTile) bool {
	for _, c := range cands {
		if c.Tile == t {
			return true
		}
	}
	return false
}

// ChooseWarlordCast is where to cast at the warlord standing at `at` from: here when here is a
// cast tile from which the spell reaches him now and after his next step
// (scarabplan.SafeCastableMoving), else the nearest such cast tile to here. ok is false when no
// cast tile reaches him: the account waits out of reach rather than walking towards him.
func ChooseWarlordCast(here, at tile.Tile, cands []WarlordCastTile, sight func(x, z int) fleetplan.Sight) (tile.Tile, bool) {
	if sight == nil {
		return tile.Tile{}, false
	}
	if WarlordOutOfReach(here) && isWarlordCastTile(here, cands) && fleetplan.SafeCastableMoving(here, at, 1, sight) {
		return here, true
	}
	var best tile.Tile
	bestD, found := 0, false
	for _, c := range cands {
		if !fleetplan.SafeCastableMoving(c.Tile, at, 1, sight) {
			continue
		}
		d := tile.Chebyshev(here, c.Tile)
		if !found || d < bestD {
			best, bestD, found = c.Tile, d, true
		}
	}
	return best, found
}

// WarlordWaitTile is where to wait while no cast tile reaches him (or he is out of view): here
// when it is a cast tile covering at least half the best cover, else the nearest such tile.
// false only without cast tiles.
func WarlordWaitTile(here tile.Tile, cands []WarlordCastTile) (tile.Tile, bool) {
	if len(cands) == 0 {
		return tile.Tile{}, false
	}
	floor := (cands[0].Cover + 1) / 2
	var best tile.Tile
	bestD, found := 0, false
	for _, c := range cands {
		if c.Cover < floor {
			continue
		}
		if c.Tile == here {
			return here, true
		}
		d := tile.Chebyshev(here, c.Tile)
		if !found || d < bestD {
			best, bestD, found = c.Tile, d, true
		}
	}
	return best, found
}

// WarlordStepBack is the step-back rule after a cast from spot: the account now on here has
// been moved (the server walked it towards him because the cast was out of reach when read) or
// stands inside his reach, so it stops and walks back to the nearest cast tile, the one it cast
// from when that is as near. back is false when it is still on spot and out of reach.
func WarlordStepBack(spot, here tile.Tile, cands []WarlordCastTile) (tile.Tile, bool) {
	if here == spot && WarlordOutOfReach(here) {
		return tile.Tile{}, false
	}
	best, bestD, found := spot, tile.Chebyshev(here, spot), isWarlordCastTile(spot, cands)
	for _, c := range cands {
		if d := tile.Chebyshev(here, c.Tile); !found || d < bestD {
			best, bestD, found = c.Tile, d, true
		}
	}
	if !found {
		return spot, true
	}
	return best, true
}

// WarlordEatAt is the hitpoints at or below which the fight eats: two of his maximum hits and a
// little, or half the account's hitpoints when that is more.
func WarlordEatAt(maxHP int) int {
	return max(2*WarlordMaxHit+2, maxHP/2)
}

// WarlordLeaveAt is the hitpoints at or below which an account with no food left stops casting
// and walks out of his reach: two of his maximum hits and one.
const WarlordLeaveAt = 2*WarlordMaxHit + 1

// WarlordShouldLeave reports whether the fight must stop now: hitpoints at or below
// WarlordLeaveAt with no food left to eat.
func WarlordShouldLeave(hp, food int) bool {
	return hp > 0 && hp <= WarlordLeaveAt && food == 0
}

// The fight kit's runes. The casts cover his hitpoints at the spell's average hit with misses
// (2.6 casts to a maximum hit, as ArenaDamage), plus a margin for casts the drift wastes, and
// never fewer than warlordMinKitCasts.
const (
	warlordMinKitCasts = 60
	warlordKitSpare    = 30
)

// WarlordMinCasts is the fewest casts of the best castable spell the fight starts with: fewer
// and it is not worth the walk.
const WarlordMinCasts = 10

// warlordKitCasts is how many casts of sp the kit carries.
func warlordKitCasts(sp arenaSpell) int {
	n := (WarlordHitpoints*26+sp.MaxHit*10-1)/(sp.MaxHit*10) + warlordKitSpare
	return max(n, warlordMinKitCasts)
}

// warlordFreeRunes are the runes a staff pays: a worn elemental staff's, and air when a staff of
// air is owned (the kit carries and wears it).
func warlordFreeRunes(s Snapshot) map[int]bool {
	free := map[int]bool{}
	for id, n := range s.WornIDs {
		if r, ok := arenaStaffRunes[id]; ok && n > 0 {
			free[r] = true
		}
	}
	if ownsAirStaff(s) {
		free[arenaAirRune] = true
	}
	return free
}

// warlordOwnedCasts is how many casts of sp the account owns runes for, pack, worn and bank.
func warlordOwnedCasts(s Snapshot, sp arenaSpell, free map[int]bool) int {
	casts := -1
	for id, per := range sp.Runes {
		if free[id] {
			continue
		}
		if n := s.Owned(id) / per; casts < 0 || n < casts {
			casts = n
		}
	}
	if casts < 0 {
		return 1 << 20
	}
	return casts
}

// warlordKitSpell is the spell the fight kit packs runes for: the strongest the account's Magic
// allows whose runes it already owns for the whole kit, else the Fight Arena kit's spell, whose
// runes Betty sells cheaply.
func warlordKitSpell(s Snapshot) arenaSpell {
	free := warlordFreeRunes(s)
	magic := arenaMagic(s)
	for i := len(arenaSpells) - 1; i >= 0; i-- {
		sp := arenaSpells[i]
		if sp.Level > magic {
			continue
		}
		if warlordOwnedCasts(s, sp, free) >= warlordKitCasts(sp) {
			return sp
		}
	}
	return arenaKitSpell(s)
}

// WarlordKitSpell names the spell the fight kit packs runes for, for the log.
func WarlordKitSpell(s Snapshot) string { return warlordKitSpell(s).Name }

// warlordRunePrices are Betty's prices (content runes.obj cost) for every rune a kit spell uses.
var warlordRunePrices = map[int]int{
	arenaFireRune: 4, arenaWaterRune: 4, arenaAirRune: 4, arenaEarthRune: 4, arenaMindRune: 3,
	arenaChaosRune: 15, arenaDeathRune: 30,
}

// warlordRuneOrder is the order the kit lists runes in.
var warlordRuneOrder = []int{arenaDeathRune, arenaChaosRune, arenaMindRune, arenaFireRune, arenaEarthRune, arenaWaterRune, arenaAirRune}

// treeFightKit is the warlord fight's kit: the kit spell's runes (required: no runes, no fight),
// a staff of air when one is owned, and food when any source has it.
func treeFightKit(s Snapshot) []KitLine {
	sp := warlordKitSpell(s)
	casts := warlordKitCasts(sp)
	free := warlordFreeRunes(s)
	var kit []KitLine
	for _, id := range warlordRuneOrder {
		per := sp.Runes[id]
		if per == 0 || free[id] {
			continue
		}
		name := arenaRuneNames[id]
		if id == arenaDeathRune {
			name = "Death rune"
		}
		kit = append(kit, KitLine{ID: id, Name: name, Qty: per * casts, Stackable: true,
			Sources: []Source{{Kind: SourceShop, Shop: BettyShop, Price: warlordRunePrices[id]}}})
	}
	if ownsAirStaff(s) {
		kit = append(kit, KitLine{ID: 1381, Name: "Staff of air", Qty: 1, Optional: true, Worn: true})
	}
	return append(kit,
		KitLine{ID: 385, Name: "Shark", Qty: 10, Optional: true},
		KitLine{ID: 379, Name: "Lobster", Qty: 10, Optional: true},
		KitLine{ID: 2309, Name: "Bread", Qty: 8, Optional: true, Sources: []Source{{Kind: SourceShop, Shop: ArdougneBaker, Price: 12}}})
}

// WarlordRuneDamage is the hitpoints the pack's runes are worth against him (ArenaDamage: the
// best single spell's casts at its average hit with misses).
func WarlordRuneDamage(s Snapshot) int { return ArenaDamage(s) }

// Step death caps: a step that kills an account twice in a day is not tried again that day.
const (
	WarlordDeathCap    = 2
	WarlordDeathWindow = 24 * time.Hour
	WarlordDeathSkip   = 24 * time.Hour
)
