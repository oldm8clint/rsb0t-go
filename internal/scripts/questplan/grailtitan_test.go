package questplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

func stock(m map[int]int) map[int]int { return m }

// The spell the fight casts is the one that delivers the most damage within the budget from the
// runes the pack holds, and nothing is cast at all below TitanMinCasts of it.
func TestPickTitanSpell(t *testing.T) {
	wind := stock(map[int]int{ObjChaosRune: 220, ObjAirRune: 440})
	fire := stock(map[int]int{ObjChaosRune: 220, ObjAirRune: 1000, ObjFireRune: 1000})
	strikes := stock(map[int]int{ObjMindRune: 600, ObjAirRune: 600, ObjFireRune: 600})
	for _, c := range []struct {
		name  string
		magic int
		stock map[int]int
		want  string
		casts int
	}{
		{"chaos and air: wind bolt", 55, wind, "Wind Bolt", 220},
		{"fire to burn: fire bolt beats wind bolt", 55, fire, "Fire Bolt", 220},
		{"magic 30 is under fire bolt (35)", 30, fire, "Wind Bolt", 220},
		{"no chaos: fire strike from mind, fire and air", 55, strikes, "Fire Strike", 200},
		{"magic 12 is under fire strike (13)", 12, strikes, "Wind Strike", 600},
	} {
		sp, n, ok := PickTitanSpell(c.magic, c.stock, TitanCastBudget)
		if !ok || sp.Name != c.want || n != c.casts {
			t.Errorf("%s: got %q for %d casts, ok=%v", c.name, sp.Name, n, ok)
		}
	}
	if _, _, ok := PickTitanSpell(55, stock(map[int]int{ObjChaosRune: 5, ObjAirRune: 400}), TitanCastBudget); ok {
		t.Error("five chaos runes is not a fight")
	}
	if _, _, ok := PickTitanSpell(55, nil, TitanCastBudget); ok {
		t.Error("an empty pack cast something")
	}
	// Death runes open the blasts, which beat the bolts by maximum hit.
	sp, _, ok := PickTitanSpell(60, stock(map[int]int{ObjDeathRune: 300, ObjAirRune: 2000, ObjFireRune: 2000, ObjChaosRune: 300}), TitanCastBudget)
	if !ok || sp.Name != "Fire Blast" {
		t.Errorf("death runes: %q %v", sp.Name, ok)
	}
}

// A fire bolt with runes for 30 casts delivers less than a wind bolt with 220: the stock decides.
func TestPickTitanSpellWeighsTheStockNotTheLevel(t *testing.T) {
	st := stock(map[int]int{ObjChaosRune: 220, ObjAirRune: 440, ObjFireRune: 120})
	sp, n, ok := PickTitanSpell(60, st, TitanCastBudget)
	if !ok || sp.Name != "Wind Bolt" || n != 220 {
		t.Fatalf("fire for 30 casts, air for 146: %q %d %v", sp.Name, n, ok)
	}
}

// The kit's rune lines pay for the budget of either family and are all optional single-slot lines:
// a mule or a bank that lacks one never blocks the quest.
func TestTitanRuneLinesPayForBothFamilies(t *testing.T) {
	lines := TitanRuneLines()
	have := map[int]int{}
	for _, l := range lines {
		if !l.Optional || !l.Stackable || l.Untradeable || l.Qty <= 0 {
			t.Errorf("%s must be an optional, stackable, tradeable line: %+v", l.Name, l)
		}
		if why := ValidateKitLine(l); why != "" {
			t.Error(why)
		}
		have[l.ID] += l.Qty
	}
	if sp, n, ok := PickTitanSpell(55, have, TitanCastBudget); !ok || n < TitanCastBudget {
		t.Fatalf("the whole kit does not pay for the budget: %q %d %v (%v)", sp.Name, n, ok, have)
	}
	// Only the wind family: chaos and air.
	wind := map[int]int{ObjChaosRune: have[ObjChaosRune], ObjAirRune: have[ObjAirRune]}
	if sp, n, ok := PickTitanSpell(55, wind, TitanCastBudget); !ok || sp.Name != "Wind Bolt" || n < TitanCastBudget {
		t.Errorf("chaos and air alone: %q %d %v", sp.Name, n, ok)
	}
	// Only the strike family: mind, fire and air (no chaos in the bank).
	strike := map[int]int{ObjMindRune: have[ObjMindRune], ObjFireRune: have[ObjFireRune], ObjAirRune: have[ObjAirRune]}
	if sp, n, ok := PickTitanSpell(55, strike, TitanCastBudget); !ok || sp.Name != "Fire Strike" || n < TitanCastBudget {
		t.Errorf("mind, fire and air alone: %q %d %v", sp.Name, n, ok)
	}
	ids := TitanRuneIDs()
	if len(ids) != len(lines) {
		t.Errorf("ids %v for %d lines", ids, len(lines))
	}
}

func TestDecideTitanFight(t *testing.T) {
	stand := tile.Tile{X: 2796, Z: 4722}
	ok := TitanReading{Excalibur: true, Here: stand, Stand: stand, TitanSeen: true, HaveSpell: true}
	for _, c := range []struct {
		name string
		mod  func(r *TitanReading)
		act  TitanAct
		park bool
	}{
		{"cast", func(r *TitanReading) {}, TitanCast, false},
		{"across the bridge", func(r *TitanReading) { r.Past, r.Excalibur = true, false }, TitanPassed, false},
		{"died", func(r *TitanReading) { r.Died = true }, TitanQuit, false},
		{"Excalibur gone: nothing is cast", func(r *TitanReading) { r.Excalibur = false }, TitanQuit, false},
		{"budget spent", func(r *TitanReading) { r.Elapsed = TitanFightBudget }, TitanQuit, true},
		{"he fell and the account was not passed", func(r *TitanReading) { r.Fell, r.TitanSeen = true, true }, TitanQuit, true},
		{"he fell and the account was passed", func(r *TitanReading) { r.Fell, r.Past = true, true }, TitanPassed, false},
		{"no Titan in view yet", func(r *TitanReading) { r.TitanSeen = false }, TitanWait, false},
		{"no Titan for ever", func(r *TitanReading) { r.TitanSeen, r.UnseenFor = false, TitanUnseenWait }, TitanQuit, true},
		{"off the stand: walk first, never click from here", func(r *TitanReading) { r.Here = tile.Tile{X: 2793, Z: 4721} }, TitanWalk, false},
		{"no runes: the sword", func(r *TitanReading) { r.HaveSpell = false }, TitanMelee, false},
		{"the server said no runes", func(r *TitanReading) { r.NoRunesSaid = true }, TitanMelee, false},
		{"no runes and the sword tried", func(r *TitanReading) { r.HaveSpell, r.MeleeTried = false, true }, TitanQuit, true},
		{"no cast tile known: the sword", func(r *TitanReading) { r.Stand, r.Here = tile.Tile{}, tile.Tile{X: 2800, Z: 4710} }, TitanMelee, false},
		{"another caster's turn", func(r *TitanReading) { r.Contended, r.ContendedFor = true, time.Minute }, TitanWait, false},
		{"a turn that never comes", func(r *TitanReading) { r.Contended, r.ContendedFor = true, TitanTurnWait }, TitanQuit, true},
	} {
		r := ok
		c.mod(&r)
		v := DecideTitanFight(r)
		if v.Act != c.act || (v.ParkFor > 0) != c.park || v.Why == "" {
			t.Errorf("%s: %+v", c.name, v)
		}
	}
	// Without Excalibur worn nothing is ever cast or swung, whatever else holds.
	for _, runes := range []bool{true, false} {
		for _, seen := range []bool{true, false} {
			for _, onStand := range []bool{true, false} {
				r := TitanReading{Excalibur: false, HaveSpell: runes, TitanSeen: seen, Stand: stand, Here: stand}
				if !onStand {
					r.Here = tile.Tile{X: 2801, Z: 4712}
				}
				if v := DecideTitanFight(r); v.Act == TitanCast || v.Act == TitanMelee || v.Act == TitanWalk {
					t.Errorf("no Excalibur worn (runes %v, seen %v, on stand %v): %+v", runes, seen, onStand, v)
				}
			}
		}
	}
}

func TestOnTitanSideAndPastTitan(t *testing.T) {
	for _, c := range []struct {
		t    tile.Tile
		side bool
	}{
		{TitanArrival, true},
		{TitanAttackStand, true},
		{TitanMeleeApproach, true},
		{TitanNaiveTrap, true},
		{tile.Tile{X: 2804, Z: 4710}, true},
		{TitanPassTile, false},
		{GrailFisherman, false},
		{tile.Tile{X: 2796, Z: 4708}, false},
		{tile.Tile{X: 2793, Z: 4710}, false},
		{tile.Tile{X: 2742, Z: 3236}, false},
		{tile.Tile{X: 2796, Z: 4722, Level: 1}, false},
	} {
		if got := OnTitanSide(c.t); got != c.side {
			t.Errorf("OnTitanSide(%v) = %v", c.t, got)
		}
	}
	snap := func(stage int, at tile.Tile) Snapshot {
		return Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, Tile: at, TileKnown: true}
	}
	if GrailPastTitan(snap(GrailSpokeCrone, CatherbyBank)) || GrailPastTitan(snap(GrailSpokeCrone, TitanArrival)) {
		t.Error("an account that has not crossed is not past him")
	}
	if !GrailPastTitan(snap(GrailSpokeCrone, TitanPassTile)) || !GrailPastTitan(snap(GrailSpokeCrone, GrailFisherman)) {
		t.Error("an account on the far side is past him")
	}
	if !GrailPastTitan(snap(GrailFindPercy, CatherbyBank)) || !GrailPastTitan(Snapshot{Journal: StatusComplete}) {
		t.Error("the Fisher King's cure and a finished quest are past him")
	}
	if GrailPastTitan(Snapshot{Journal: StatusInProgress}) {
		t.Error("an unread stage is not past him")
	}
}

// The old Titan fight's 12 hour park and the Fisherman walks' park are cleared; nothing else is.
func TestParkSuperseded(t *testing.T) {
	for _, c := range []struct {
		id, reason string
		want       bool
	}{
		{HolyGrailID, "the Black Knight Titan did not fall (the fight's budget is spent): not trying again for 12h0m0s", true},
		{HolyGrailID, "its walks cannot get there: 5 walks in this run to 2802,4706,0 ended unreachable (unreachable from here)", true},
		{HolyGrailID, "its walks cannot get there: 5 walks in this run to 2804,4705,0 ended unreachable", true},
		{HolyGrailID, "its walks cannot get there: 5 walks in this run to 3105,3957,0 ended unreachable", false},
		{HolyGrailID, TitanParkReasonPrefix + "the fight's budget is spent", false},
		{HolyGrailID, "budget ran out: grail-whistles", false},
		{MerlinsCrystalID, "the Black Knight Titan did not fall", false},
	} {
		if got := ParkSuperseded(c.id, c.reason); got != c.want {
			t.Errorf("%s %q: %v", c.id, c.reason, got)
		}
	}
	if strings.HasPrefix(TitanParkReasonPrefix, "the Black Knight Titan did not fall") {
		t.Error("the new park reason would be cleared as the old one")
	}
}

// Cast tiles: none without a sight reader; with open ground every tile 3 to 8 from him on his side
// (here: all walkable tiles) is a candidate, the straight run along his row first, nearest to five
// tiles off first, and the account standing on one stays there.
func TestTitanCastTilesOnOpenGround(t *testing.T) {
	if got := TitanCastTiles(nil, nil); got != nil {
		t.Fatalf("no sight, %d tiles", len(got))
	}
	open := func(x, z int) fleetplan.Sight { return 0 }
	east := func(tt tile.Tile) bool { return tt.X > TitanTile.X }
	cands := TitanCastTiles(east, open)
	if len(cands) == 0 {
		t.Fatal("open ground east of him offers no cast tile")
	}
	first := cands[0]
	if first.Tile.Z != TitanTile.Z || first.Dist != TitanCastPrefer {
		t.Errorf("first candidate %+v, want his row, %d tiles off", first, TitanCastPrefer)
	}
	seenOffRow := false
	for _, c := range cands {
		if c.Dist < TitanCastMin || c.Dist > TitanCastMax || c.Tile.X <= TitanTile.X || c.Tile.Level != 0 {
			t.Errorf("bad candidate %+v", c)
		}
		if c.Tile.Z != TitanTile.Z {
			seenOffRow = true
		} else if seenOffRow {
			t.Errorf("a row tile %+v after an off-row one", c)
		}
	}
	if st, ok := ChooseTitanStand(first.Tile, cands); !ok || st != first.Tile {
		t.Errorf("standing on a cast tile: %v %v", st, ok)
	}
	// From the arrival tile: the nearest cast tile.
	st, ok := ChooseTitanStand(TitanArrival, cands)
	if !ok || tile.Chebyshev(st, TitanArrival) > tile.Chebyshev(first.Tile, TitanArrival) {
		t.Errorf("from the arrival tile: %v %v", st, ok)
	}
	if _, ok := ChooseTitanStand(TitanArrival, nil); ok {
		t.Error("no candidates, a stand")
	}
	// A wall on the bridge's row blinds the straight run and the stand moves off it.
	wall := func(x, z int) fleetplan.Sight {
		if x == TitanTile.X+2 {
			return fleetplan.SightFull
		}
		return 0
	}
	for _, c := range TitanCastTiles(east, wall) {
		if c.Tile.X > TitanTile.X+2 && c.Tile.Z == TitanTile.Z {
			t.Errorf("%+v casts through a wall", c)
		}
	}
}
