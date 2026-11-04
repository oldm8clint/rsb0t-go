package questplan

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// The Holy Grail's Black Knight Titan, the pure half.
//
// WHAT THE LIVE FLEET DID (13 Oct 2026, 00:41 to 11:10, a dozen ScarabSwarm accounts, every one the
// same): "the Titan: no swing in 3m0s (0 swings so far, me at {2793 4721 0}): giving the fight up",
// then a 12 hour park. It was never a combat failure. The account never reached him.
//
// WHY (Engine-TS 289 and content 289, run against the engine's own routefinder on the real map
// square m43_73; docs/evidence/grail-titan-2026-10-13 has the probe and the findings):
//
//   - The production engine runs clientRoutefinder (WorldConfig.node.clientRoutefinder, true by
//     default), so a player's moveStrategy is NAIVE: an Attack click is answered by
//     findNaivePath, a straight line to the target's nearest tile, and the walk is only re-planned
//     when its last waypoint is more than one tile from the target (Player.naivePathToTarget).
//     This client sends OPNPC without the move packet the real client sends first.
//   - The Titan (npc 221, spawn 2791,4722) stands in the middle of a three tile bridge over a
//     chasm: terrain at x 2790 to 2792 is blocked everywhere except z 4722. From any tile south of
//     the bridge row the naive line to him ends on 2791,4721, the blocked tile just south of him,
//     which is within one tile of the target, so it is never re-planned; the walk slides north
//     along the chasm's edge and stops on 2793,4721 for good. From the arrival tile it works,
//     which is why the first runs (6 Oct) swung.
//   - The fisherman walks that preceded the fight (the old grailRealm went to the Fisherman first
//     whenever the Titan was more than 12 tiles off, which he is from the arrival tile) dragged
//     every account south of the bridge row before the first click.
//
// So an Attack or a cast is sent only from a tile the account walked to by itself: the cast tiles
// below, or TitanMeleeApproach for the melee fallback.
//
// WHY SPELLS. His magic defence param is 1000, but the defence roll uses the npc MAGIC LEVEL, which
// his config leaves at 1: (1+9)*(1000+64) = 10640 against a player's (magic+9)*(bonus+64), about
// 4400 at Magic 60 with no bonus, so a cast lands about one in five (A/2D). Melee does not: Excalibur
// (slash 29, strength 25, one swing in five ticks) at Attack and Strength 30 lands 21% of its swings
// against his defence roll of 9100 for at most 5, which is 270 swings for his 142 hitpoints, and at
// 20 it is 16% for 4, 440 or more, with his regeneration (1 per 100 ticks) on top. The first live
// fights (6 Oct 2026) swung every 5 ticks for 25 minutes, 500 swings, took no hit and never won: not
// a bug, the arithmetic. A Wind Bolt (maxhit 9) averages 0.94 a cast, a Fire Bolt
// 1.25: his 142 hitpoints fall in 110 to 150 casts, 6 to 8 minutes, with no hit taken. He is
// maxrange 0, nomove: Npc.targetWithinMaxRange drops any target that is not on one of the four
// tiles orthogonally next to his spawn, so from two tiles away he can neither follow nor hit.
// black_knight_titan.rs2 does not look at the damage source, and a spell's pvm_spell_success calls
// npc_retaliate first, so %npc_aggressive_player is set when ai_queue3 fires: with Excalibur WORN
// and the caster within 11 tiles (queue_defeat_titan), the caster is teleported to the far end of
// the bridge and he dies; otherwise he heals to full ("INVINCIBLE") or dies without a pass.

// The Titan's facts.
const (
	TitanHitpoints = 142
	// TitanDefeatRange is queue_defeat_titan's `distance(coord, npc_coord) > 11`: a caster further
	// than this when he falls gets no pass, and he dies for nothing.
	TitanDefeatRange = 11
	// TitanCastMin and TitanCastMax bound how far from him a cast tile lies (Chebyshev): two or
	// more keeps him unable to follow (maxrange 0), three leaves a margin, and eight leaves the
	// spell's ten tiles (scarabplan.CastRange less CastMargin) and the defeat range two to spare.
	TitanCastMin = 3
	TitanCastMax = 8
	// TitanCastPrefer is the distance the stand is chosen nearest to.
	TitanCastPrefer = 5
	// TitanCastBudget is how many casts of either spell family the kit pays for. It is sized on the
	// SLOWEST spell, Fire Strike (maxhit 8), which is the one most accounts cast (chaos runes are the
	// mule's to hand out only from a stack of 20,000): a cast is worth 0.83 hitpoints against his 142
	// and the 1 he regenerates every 20 casts, so he falls in about 180 casts on average and within
	// 260 about nine fights in ten (titanrunes_test.go simulates the engine's roll). The review of 13
	// Oct 2026 measured 49% within 180 casts, 85% within 220 and 97% within 260 for Fire Strike, and
	// 95% within 220 for Wind Bolt, which is why the first kit (180 casts of Fire Strike) was short.
	TitanCastBudget = 260
	// TitanMinCasts is the fewest casts a spell must have runes for to BEGIN with it; the real bar
	// to start a fight is the gate, TitanRunesEnough, which asks for the damage and not the casts.
	TitanMinCasts = 24
)

// The tiles of the arrival pocket and his bridge.
var (
	// TitanArrival is where the Magic whistle sets an account down (opheld1,magic_whistle:
	// p_teleport(0_43_73_54_43)); the live "displaced" lines read 2805,4715.
	TitanArrival = tile.Tile{X: 2806, Z: 4715}
	// TitanAttackStand is the one tile an account melees him from: east of him.
	TitanAttackStand = tile.Tile{X: 2792, Z: 4722}
	// TitanMeleeApproach is the tile the melee fallback walks to first: the server's walk from here
	// ends on TitanAttackStand.
	TitanMeleeApproach = tile.Tile{X: 2793, Z: 4722}
	// TitanPassTile is where the engine sets the victor down: movecoord(npc_coord, -1, 0, 0).
	TitanPassTile = tile.Tile{X: 2790, Z: 4722}
	// TitanNaiveTrap is where every walk of the old fight ended (see above).
	TitanNaiveTrap = tile.Tile{X: 2793, Z: 4721}
)

// titanPocketMinX is the western edge of the arrival pocket on each row, z 4709 (index 0) to 4732:
// the pocket is the ground east of that edge, out to its eastern wall at x 2811. The edge steps
// because the river runs diagonally across the south (the Fisherman's bank is the same x range
// below it) and the chasm under the bridge runs north to south at x 2790 to 2792. Measured from
// pathfind.Pack.Flood of the real collision pack from the arrival tile (320 tiles) against the same
// flood from the far end of the bridge (grailtitan_pack_test.go checks every tile of both).
var titanPocketMinX = [...]int{
	2805, 2801, 2799, 2797, 2794, 2794, 2794, // 4709 to 4715
	2793, 2793, 2793, 2793, 2793, 2793, // 4716 to 4721
	2792, 2793, 2792, 2792, 2792, 2793, // 4722 to 4727
	2790, 2789, 2789, 2789, 2790, // 4728 to 4732
}

// OnTitanSide reports whether t is in the arrival pocket east of the Titan's bridge: the 320
// tiles a walk from the arrival tile reaches, from which nothing else in the realm is reachable
// until he has been passed.
func OnTitanSide(t tile.Tile) bool {
	if t.Level != 0 || !InGrailRealm(t) || t.X > 2811 {
		return false
	}
	i := t.Z - 4709
	return i >= 0 && i < len(titanPocketMinX) && t.X >= titanPocketMinX[i]
}

// GrailPastTitan reports whether the account is past the one-at-a-time step of The Holy Grail:
// the Titan, whom one account's fight holds for everyone (his hitpoints and his %npc_aggressive
// player are shared, and "Someone else is fighting that." refuses a second caster). Past him
// means the quest is done or at the Fisher King's cure (stage 8 on), or the account stands
// beyond the bridge.
func GrailPastTitan(s Snapshot) bool {
	if s.Journal == StatusComplete {
		return true
	}
	if s.Journal != StatusInProgress || !s.StageKnown {
		return false
	}
	if s.Stage >= GrailFindPercy {
		return true
	}
	return s.TileKnown && s.Tile.Level == 0 && InGrailRealm(s.Tile) && !OnTitanSide(s.Tile)
}

// ---------------------------------------------------------------------------------------------
// Spells

// ObjDeathRune is the death rune (content pack/obj.pack); the other runes' ids are horror.go's.
const ObjDeathRune = 560

// TitanRune is one rune a cast spends.
type TitanRune struct {
	ID    int
	Name  string
	Count int
}

// TitanSpell is a combat spell the Titan fight may cast: the elemental strikes, bolts and blasts
// (skill_combat/configs/magic/magic_combat_spells.dbrow), with every rune paid, because Excalibur
// is the weapon, so no staff makes an element free. It must equal actions.AutocastSpells
// (questmods tests it).
type TitanSpell struct {
	Name   string
	Level  int
	MaxHit int
	Runes  []TitanRune
}

func rAir(n int) TitanRune   { return TitanRune{ObjAirRune, "Air rune", n} }
func rWater(n int) TitanRune { return TitanRune{ObjWaterRune, "Water rune", n} }
func rEarth(n int) TitanRune { return TitanRune{ObjEarthRune, "Earth rune", n} }
func rFire(n int) TitanRune  { return TitanRune{ObjFireRune, "Fire rune", n} }
func rMind(n int) TitanRune  { return TitanRune{ObjMindRune, "Mind rune", n} }
func rDeath(n int) TitanRune { return TitanRune{ObjDeathRune, "Death rune", n} }
func rChaos(n int) TitanRune { return TitanRune{ObjChaosRune, "Chaos rune", n} }

// TitanSpells are the candidates, lowest level first.
var TitanSpells = []TitanSpell{
	{"Wind Strike", 1, 2, []TitanRune{rMind(1), rAir(1)}},
	{"Water Strike", 5, 4, []TitanRune{rMind(1), rWater(1), rAir(1)}},
	{"Earth Strike", 9, 6, []TitanRune{rMind(1), rEarth(2), rAir(1)}},
	{"Fire Strike", 13, 8, []TitanRune{rMind(1), rFire(3), rAir(2)}},
	{"Wind Bolt", 17, 9, []TitanRune{rChaos(1), rAir(2)}},
	{"Water Bolt", 23, 10, []TitanRune{rChaos(1), rWater(2), rAir(2)}},
	{"Earth Bolt", 29, 11, []TitanRune{rChaos(1), rEarth(3), rAir(2)}},
	{"Fire Bolt", 35, 12, []TitanRune{rChaos(1), rFire(4), rAir(3)}},
	{"Wind Blast", 41, 13, []TitanRune{rDeath(1), rAir(3)}},
	{"Water Blast", 47, 14, []TitanRune{rDeath(1), rWater(3), rAir(3)}},
	{"Earth Blast", 53, 15, []TitanRune{rDeath(1), rEarth(4), rAir(3)}},
	{"Fire Blast", 59, 16, []TitanRune{rDeath(1), rFire(5), rAir(4)}},
}

// Casts is how many casts of s the stock pays for.
func (s TitanSpell) Casts(stock map[int]int) int {
	n := -1
	for _, r := range s.Runes {
		if r.Count <= 0 {
			continue
		}
		if c := stock[r.ID] / r.Count; n < 0 || c < n {
			n = c
		}
	}
	if n < 0 {
		return 0
	}
	return n
}

// PickTitanSpell is the spell to fight with: of the spells the Magic level allows and the stock
// pays TitanMinCasts casts of, the one that delivers the most damage within budget casts (its
// casts, capped at the budget, times its maximum hit: a cast lands the same share of the time
// whatever the spell), the stronger spell on a tie. affordable is how many casts the stock pays
// for. ok is false when no spell has the runes.
func PickTitanSpell(magic int, stock map[int]int, budget int) (spell TitanSpell, affordable int, ok bool) {
	return pickTitanSpell(magic, stock, budget, TitanMinCasts)
}

// pickTitanSpell is PickTitanSpell with the fewest casts a spell may have runes for.
func pickTitanSpell(magic int, stock map[int]int, budget, minCasts int) (spell TitanSpell, affordable int, ok bool) {
	if budget <= 0 {
		budget = TitanCastBudget
	}
	if minCasts < 1 {
		minCasts = 1
	}
	bestValue := 0
	for _, s := range TitanSpells {
		if s.Level > magic {
			continue
		}
		n := s.Casts(stock)
		if n < minCasts {
			continue
		}
		value := minInt(n, budget) * s.MaxHit
		if !ok || value > bestValue || (value == bestValue && s.MaxHit > spell.MaxHit) {
			spell, affordable, bestValue, ok = s, n, value, true
		}
	}
	return spell, affordable, ok
}

// TitanRuneLines are the kit lines that pay for the fight: the two spell families a Stage 5 account
// can cast without a staff or a death rune, each sized for the whole TitanCastBudget casts, with the
// air they share counted once at the larger need. Every line is optional and stackable (one slot
// each), because the families are alternatives and a bank or a mule that lacks one must not block
// the quest; the gate that makes the runes REQUIRED is TitanRunesEnough, checked before the
// whistles and again before the realm. The account's own bank is asked first, then the mule, which
// holds the air and the fire (and hands the chaos out only from its stack of 20,000), so Fire
// Strike's mind runes come from the account's own bank.
//
//	Wind Bolt   chaos 1, air 2          (260 casts: 260 chaos, 520 air)
//	Fire Strike mind 1, fire 3, air 2   (260 casts: 260 mind, 780 fire, 520 air)
func TitanRuneLines() []KitLine {
	need := map[int]int{}
	note := func(name string, casts int) {
		for _, s := range TitanSpells {
			if s.Name != name {
				continue
			}
			for _, r := range s.Runes {
				if q := r.Count * casts; q > need[r.ID] {
					need[r.ID] = q
				}
			}
		}
	}
	note("Wind Bolt", TitanCastBudget)
	note("Fire Strike", TitanCastBudget)
	var lines []KitLine
	for _, id := range []int{ObjChaosRune, ObjAirRune, ObjMindRune, ObjFireRune} {
		name := ""
		for _, s := range TitanSpells {
			for _, r := range s.Runes {
				if r.ID == id {
					name = r.Name
				}
			}
		}
		lines = append(lines, KitLine{ID: id, Name: name, Qty: need[id], Stackable: true, Optional: true})
	}
	return lines
}

// TitanRuneIDs are the runes the fight may spend, for the keep lists.
func TitanRuneIDs() []int {
	var ids []int
	for _, l := range TitanRuneLines() {
		ids = append(ids, l.ID)
	}
	return ids
}

// ---------------------------------------------------------------------------------------------
// Where to stand

// TitanCastTile is a tile a spell reaches him from.
type TitanCastTile struct {
	Tile tile.Tile
	// Dist is its Chebyshev distance from his spawn.
	Dist int
}

// TitanCastTiles lists every tile stand accepts (walkable and in the arrival pocket) that is
// TitanCastMin to TitanCastMax from his spawn and from which a spell reaches him
// (scarabplan.CastableFrom: within range and in line of sight), the straight run along his row
// first, then the nearest to TitanCastPrefer tiles off, then by position. A nil sight casts at
// nothing: no tile is returned.
func TitanCastTiles(stand func(t tile.Tile) bool, sight func(x, z int) fleetplan.Sight) []TitanCastTile {
	if sight == nil {
		return nil
	}
	var out []TitanCastTile
	for z := TitanTile.Z - TitanCastMax; z <= TitanTile.Z+TitanCastMax; z++ {
		for x := TitanTile.X - TitanCastMax; x <= TitanTile.X+TitanCastMax; x++ {
			t := tile.Tile{X: x, Z: z, Level: TitanTile.Level}
			d := tile.Chebyshev(t, TitanTile)
			if d < TitanCastMin || d > TitanCastMax || (stand != nil && !stand(t)) {
				continue
			}
			if !fleetplan.CastableFrom(t, TitanTile, sight) {
				continue
			}
			out = append(out, TitanCastTile{Tile: t, Dist: d})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ar, br := a.Tile.Z == TitanTile.Z, b.Tile.Z == TitanTile.Z; ar != br {
			return ar
		}
		if da, db := absInt(a.Dist-TitanCastPrefer), absInt(b.Dist-TitanCastPrefer); da != db {
			return da < db
		}
		if a.Tile.Z != b.Tile.Z {
			return a.Tile.Z < b.Tile.Z
		}
		return a.Tile.X < b.Tile.X
	})
	return out
}

// ChooseTitanStand is the tile to cast from: where the account stands when that is a cast tile,
// else the nearest cast tile (ties to the better ranked one). false when there is none.
func ChooseTitanStand(here tile.Tile, cands []TitanCastTile) (tile.Tile, bool) {
	if len(cands) == 0 {
		return tile.Tile{}, false
	}
	best, bestD := cands[0].Tile, -1
	for _, c := range cands {
		if c.Tile == here {
			return here, true
		}
		d := tile.Chebyshev(here, c.Tile)
		if bestD < 0 || d < bestD {
			best, bestD = c.Tile, d
		}
	}
	return best, true
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ---------------------------------------------------------------------------------------------
// The fight's rules

// How long the fight may take and how long a failure keeps the quest out.
const (
	// TitanFightBudget is the whole fight: 110 to 150 casts at a cast every 3 s, with the waits.
	TitanFightBudget = 20 * time.Minute
	// TitanTurnWait is how long "Someone else is fighting that." may keep the account from casting
	// before it gives its turn up; the ladder admits one account to The Holy Grail at a time
	// (questrunner exclusiveQuests), so another caster is a stranger.
	TitanTurnWait = 8 * time.Minute
	// TitanUnseenWait is how long the fight waits for a Titan out of view (he respawns in ten ticks).
	TitanUnseenWait = 90 * time.Second
	// TitanParkSpent keeps the quest out after a fight that spent its budget.
	TitanParkSpent = 3 * time.Hour
	// TitanParkRunes keeps it out after a fight with nothing to cast and no swing landing: the next
	// run re-provisions the kit.
	TitanParkRunes = 45 * time.Minute
	// TitanParkTurn keeps it out after a turn that never came.
	TitanParkTurn = 25 * time.Minute
	// TitanParkFell keeps it out after he fell and the pass did not come.
	TitanParkFell = 30 * time.Minute
	// TitanFellHealthPct and TitanFellGrace: he was last seen at or under this share of his health
	// and then left view, and the account is still not across the bridge after this long. The pass
	// teleport is part of his death queue, so it comes within a tick or two.
	TitanFellHealthPct = 40
	TitanFellGrace     = 8 * time.Second
)

// TitanReading is one poll of the fight.
type TitanReading struct {
	// Excalibur is whether it is worn now.
	Excalibur bool
	// Died is whether the account's death line arrived during the fight.
	Died bool
	// Past is whether the account stands beyond the bridge.
	Past bool
	// Here and Stand are where the account is and where it casts from (Stand zero when no cast
	// tile is known).
	Here, Stand tile.Tile
	// TitanSeen is whether he is in view.
	TitanSeen bool
	// HaveSpell is whether PickTitanSpell found a spell with runes.
	HaveSpell bool
	// NoRunesSaid is whether the server refused the last cast for want of runes.
	NoRunesSaid bool
	// Contended is whether the last cast was refused with "Someone else is fighting that.".
	Contended bool
	// ContendedFor is how long refusals have been the only answer.
	ContendedFor time.Duration
	// UnseenFor is how long he has been out of view.
	UnseenFor time.Duration
	// Elapsed is the time since the fight began.
	Elapsed time.Duration
	// MeleeTried is whether the melee fallback has already run.
	MeleeTried bool
	// Fell is whether he fell (last seen at TitanFellHealthPct or less, then out of view) and the
	// account is still not across the bridge TitanFellGrace later: the pass did not come (Excalibur
	// off, out of range), and another cast would only kill his respawn.
	Fell bool
}

// TitanAct is what the fight does next.
type TitanAct int

const (
	// TitanCast sends a cast.
	TitanCast TitanAct = iota
	// TitanWalk walks to the stand first.
	TitanWalk
	// TitanWait waits a moment (a respawn, another caster's turn).
	TitanWait
	// TitanMelee falls back to the sword.
	TitanMelee
	// TitanPassed ends the fight: the account is across.
	TitanPassed
	// TitanQuit ends the fight without the pass; Verdict.ParkFor says how long the quest sits out.
	TitanQuit
)

// TitanVerdict is DecideTitanFight's answer.
type TitanVerdict struct {
	Act TitanAct
	Why string
	// ParkFor, on a TitanQuit that is the fight's failure (zero: leave the park to others).
	ParkFor time.Duration
}

// DecideTitanFight is the fight's single rule, in order: across the bridge is won; dead is over;
// without Excalibur worn nothing is sent (he would heal to full and set the quest back to stage
// 7); a spent budget ends it; a Titan out of view is waited for; no runes is the sword's job;
// off the stand the account walks there (an Attack or a cast from anywhere else is the naive
// walk that stranded every account at 2793,4721); another caster's turn is waited out; else cast.
func DecideTitanFight(r TitanReading) TitanVerdict {
	switch {
	case r.Past:
		return TitanVerdict{Act: TitanPassed, Why: "across the bridge"}
	case r.Died:
		return TitanVerdict{Act: TitanQuit, Why: "the account died at the Titan"}
	case !r.Excalibur:
		return TitanVerdict{Act: TitanQuit, Why: "Excalibur is not worn: he would heal to full and set the quest back"}
	case r.Fell:
		return TitanVerdict{Act: TitanQuit, Why: "he fell and the pass did not come", ParkFor: TitanParkFell}
	case r.Elapsed >= TitanFightBudget:
		return TitanVerdict{Act: TitanQuit, Why: "the fight's budget is spent", ParkFor: TitanParkSpent}
	case !r.TitanSeen:
		if r.UnseenFor >= TitanUnseenWait {
			return TitanVerdict{Act: TitanQuit, Why: "no Titan in view", ParkFor: TitanParkRunes}
		}
		return TitanVerdict{Act: TitanWait, Why: "no Titan in view (respawning?)"}
	case r.NoRunesSaid || !r.HaveSpell:
		if r.MeleeTried {
			return TitanVerdict{Act: TitanQuit, Why: "no runes to cast and the sword did not do it", ParkFor: TitanParkRunes}
		}
		return TitanVerdict{Act: TitanMelee, Why: "no runes to cast"}
	case r.Stand != (tile.Tile{}) && r.Here != r.Stand:
		return TitanVerdict{Act: TitanWalk, Why: "not on the cast tile"}
	case r.Stand == (tile.Tile{}):
		if r.MeleeTried {
			return TitanVerdict{Act: TitanQuit, Why: "no tile to cast from and the sword did not do it", ParkFor: TitanParkRunes}
		}
		return TitanVerdict{Act: TitanMelee, Why: "no tile to cast from"}
	case r.Contended:
		if r.ContendedFor >= TitanTurnWait {
			return TitanVerdict{Act: TitanQuit, Why: "someone else has held the Titan for " + r.ContendedFor.Round(time.Second).String(), ParkFor: TitanParkTurn}
		}
		return TitanVerdict{Act: TitanWait, Why: "another caster's turn"}
	}
	return TitanVerdict{Act: TitanCast, Why: "on the cast tile with runes"}
}

// TitanParkReasonPrefix starts every park reason the new fight writes.
const TitanParkReasonPrefix = "the Titan fight: "

// ParkSuperseded reports whether a park reason on quest id was written by a build whose cause is
// fixed, so the park should be cleared and the quest tried again: the old Titan fight's 12 hour
// park (a fight that never reached him) and its twin, the 20 minute park of the walks to the
// Fisherman across the river. The ladder clears them when it meets them.
func ParkSuperseded(id, reason string) bool {
	if id != HolyGrailID {
		return false
	}
	if strings.HasPrefix(reason, "the Black Knight Titan did not fall") {
		return true
	}
	// The twin is only the Fisherman's walks (2802,4706 and the tiles round him), not every
	// unreachable walk a Holy Grail run can park on.
	return strings.HasPrefix(reason, "its walks cannot get there: ") && fishermanWalk.MatchString(reason)
}

// fishermanWalk matches the walks the old realm flow made to the Fisherman's bank, x 2796 to 2809 and
// z 4704 to 4707 on the ground floor.
var fishermanWalk = regexp.MustCompile(`(279[6-9]|280[0-9]),470[4-7],0`)
