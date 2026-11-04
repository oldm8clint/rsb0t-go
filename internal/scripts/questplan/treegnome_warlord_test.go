package questplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// openSight sees through everything.
func openSight(int, int) fleetplan.Sight { return 0 }

func at(dx, dz int) tile.Tile {
	return tile.Tile{X: WarlordSpawn.X + dx, Z: WarlordSpawn.Z + dz, Level: WarlordSpawn.Level}
}

// He keeps a target maxrange+1 (11) off his spawn and drops one at 12, counted from the spawn
// whatever tile he stands on.
func TestWarlordReachIsCountedFromHisSpawn(t *testing.T) {
	for _, c := range []struct {
		t   tile.Tile
		out bool
	}{
		{WarlordSpawn, false}, {at(9, 0), false}, {at(11, -11), false}, {at(0, 11), false},
		{at(12, 0), true}, {at(-12, 5), true}, {at(3, -14), true},
		{tile.Tile{X: WarlordSpawn.X, Z: WarlordSpawn.Z, Level: 1}, true},
	} {
		if got := WarlordOutOfReach(c.t); got != c.out {
			t.Errorf("%v: out of reach %v, want %v", c.t, got, c.out)
		}
	}
	if WarlordSafeDist != 12 {
		t.Fatalf("WarlordSafeDist %d: maxrange 10 keeps a target at 11", WarlordSafeDist)
	}
}

// Open ground all round: every cast tile is out of his reach and inside the ring, the nearest
// ring covers the most, and tiles stand refuses are never offered.
func TestWarlordCastTilesOnOpenGround(t *testing.T) {
	roam := WarlordRoam(nil)
	if len(roam) != 19*19 || roam[0] != WarlordSpawn {
		t.Fatalf("open ground: %d roam tiles, first %v", len(roam), roam[0])
	}
	cands := WarlordCastTiles(func(t tile.Tile) bool { return t.Z < WarlordSpawn.Z }, roam, openSight)
	if len(cands) == 0 {
		t.Fatal("no cast tiles on open ground")
	}
	for _, c := range cands {
		d := tile.Chebyshev(c.Tile, WarlordSpawn)
		if !WarlordOutOfReach(c.Tile) || d > WarlordSafeDist+WarlordCastRing || c.Tile.Z >= WarlordSpawn.Z || c.Cover <= 0 {
			t.Fatalf("bad cast tile %+v (%d off the spawn)", c, d)
		}
	}
	if d := tile.Chebyshev(cands[0].Tile, WarlordSpawn); d != WarlordSafeDist {
		t.Errorf("the best cast tile %+v is %d off his spawn, want the inner ring", cands[0], d)
	}
	for i := 1; i < len(cands); i++ {
		if cands[i].Cover > cands[i-1].Cover {
			t.Fatalf("not sorted by cover at %d: %+v after %+v", i, cands[i], cands[i-1])
		}
	}
	if WarlordCastTiles(nil, roam, nil) != nil {
		t.Fatal("cast tiles without a line of sight to judge them by")
	}
}

// A projectile-blocking row between a cast tile and his ground takes that ground off its cover.
func TestWarlordCastTilesCountOnlyWhatTheSpellSees(t *testing.T) {
	roam := WarlordRoam(nil)
	south := func(t tile.Tile) bool { return t == at(0, -12) }
	open := WarlordCastTiles(south, roam, openSight)
	wall := func(x, z int) fleetplan.Sight {
		if z == WarlordSpawn.Z-8 && x >= WarlordSpawn.X-1 && x <= WarlordSpawn.X+1 {
			return fleetplan.SightFull
		}
		return 0
	}
	blocked := WarlordCastTiles(south, roam, wall)
	if len(open) != 1 || len(blocked) != 1 || blocked[0].Cover >= open[0].Cover {
		t.Fatalf("open %+v, behind a wall %+v", open, blocked)
	}
}

// The cast tile: here when the spell reaches him from here now and after his next step, else the
// nearest tile that does; none when he has wandered out of every tile's reach.
func TestChooseWarlordCast(t *testing.T) {
	roam := WarlordRoam(nil)
	cands := WarlordCastTiles(nil, roam, openSight)
	south, north := at(0, -12), at(0, 12)
	if got, ok := ChooseWarlordCast(south, at(0, -4), cands, openSight); !ok || got != south {
		t.Fatalf("he is 8 from the tile it stands on: got %v %v", got, ok)
	}
	// From the south tile he is 21 away at the north edge: the nearest tile that reaches him.
	got, ok := ChooseWarlordCast(south, at(0, 9), cands, openSight)
	if !ok || tile.Chebyshev(got, at(0, 9)) > fleetplan.CastRange-fleetplan.CastMargin || !WarlordOutOfReach(got) {
		t.Fatalf("he is at the north edge: got %v %v", got, ok)
	}
	// The reach is 9 with the margin, and his next step may take him to 10, the spell's own
	// range: 9 is a cast, 10 is not.
	one := []WarlordCastTile{{Tile: north, Cover: 1}}
	if _, ok := ChooseWarlordCast(north, at(0, 2), one, openSight); ok {
		t.Fatal("cast at a warlord 10 tiles off")
	}
	if got, ok := ChooseWarlordCast(north, at(0, 3), one, openSight); !ok || got != north {
		t.Fatalf("he is 9 from the north tile: got %v %v", got, ok)
	}
	// Only south tiles, and he is at the north edge: wait, never walk towards him.
	southOnly := WarlordCastTiles(func(t tile.Tile) bool { return t.Z < WarlordSpawn.Z-11 }, roam, openSight)
	if got, ok := ChooseWarlordCast(south, at(0, 9), southOnly, openSight); ok {
		t.Fatalf("a cast tile %v that does not reach him", got)
	}
	// Standing inside his reach is never a cast tile, even in reach of him.
	if got, ok := ChooseWarlordCast(at(0, -11), at(0, -4), cands, openSight); !ok || !WarlordOutOfReach(got) {
		t.Fatalf("from inside his reach: got %v %v", got, ok)
	}
}

// The step back: nothing while the account is on its tile; a cast that walked it towards him
// walks it back to the nearest cast tile, its own when that is as near.
func TestWarlordStepBack(t *testing.T) {
	cands := WarlordCastTiles(nil, WarlordRoam(nil), openSight)
	spot := at(0, -12)
	if _, back := WarlordStepBack(spot, spot, cands); back {
		t.Fatal("stepped back from the tile it cast from")
	}
	// Walked one tile in (11 off his spawn): inside his reach, back to the spot.
	if got, back := WarlordStepBack(spot, at(0, -11), cands); !back || got != spot {
		t.Fatalf("one tile in: %v %v", got, back)
	}
	// Walked two tiles in and one east: back to the nearest cast tile, out of his reach.
	got, back := WarlordStepBack(spot, at(1, -10), cands)
	if !back || !WarlordOutOfReach(got) || tile.Chebyshev(got, at(1, -10)) != 2 {
		t.Fatalf("two tiles in: %v %v", got, back)
	}
	// Moved along the ring but still out of reach: it is a drift all the same (the server is
	// walking it), back to a cast tile.
	if got, back := WarlordStepBack(spot, at(1, -13), cands); !back || !WarlordOutOfReach(got) {
		t.Fatalf("moved along the ring: %v %v", got, back)
	}
	// No cast tiles at all: back to the spot.
	if got, back := WarlordStepBack(spot, at(0, -10), nil); !back || got != spot {
		t.Fatalf("no cast tiles: %v %v", got, back)
	}
}

func TestWarlordWaitTile(t *testing.T) {
	cands := WarlordCastTiles(nil, WarlordRoam(nil), openSight)
	if _, ok := WarlordWaitTile(at(0, -12), nil); ok {
		t.Fatal("a wait tile without cast tiles")
	}
	got, ok := WarlordWaitTile(at(0, -12), cands)
	if !ok || got != at(0, -12) {
		t.Fatalf("on a good cast tile already: %v %v", got, ok)
	}
	far := tile.Tile{X: 2616, Z: 3332}
	got, ok = WarlordWaitTile(far, cands)
	if !ok || !WarlordOutOfReach(got) {
		t.Fatalf("from the bank: %v %v", got, ok)
	}
}

func TestWarlordEatAndLeave(t *testing.T) {
	if WarlordEatAt(43) != 21 || WarlordEatAt(30) != 20 || WarlordEatAt(60) != 30 {
		t.Fatalf("eat at %d, %d, %d", WarlordEatAt(43), WarlordEatAt(30), WarlordEatAt(60))
	}
	for _, c := range []struct {
		hp, food int
		leave    bool
	}{{19, 0, true}, {19, 1, false}, {20, 0, false}, {5, 0, true}, {0, 0, false}} {
		if got := WarlordShouldLeave(c.hp, c.food); got != c.leave {
			t.Errorf("hp %d food %d: leave %v", c.hp, c.food, got)
		}
	}
}

func warlordSnap(magic int) Snapshot {
	return Snapshot{Journal: StatusInProgress, Stage: 7, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{},
		FreeSlots: 20, BankKnown: true, BankIDs: map[int]int{}, Ran: map[string]bool{}, Failed: map[string]bool{},
		Levels: map[string]int{"magic": magic}}
}

func kitQty(kit []KitLine, id int) int {
	for _, l := range kit {
		if l.ID == id {
			return l.Qty
		}
	}
	return 0
}

// The kit packs runes for the strongest spell the account owns enough runes for, else the
// arena's cheap shop spell; a staff of air pays the air.
func TestTreeFightKitPacksRunes(t *testing.T) {
	s := warlordSnap(66)
	s.BankIDs[arenaDeathRune], s.BankIDs[arenaFireRune], s.BankIDs[arenaAirRune] = 200, 1000, 1000
	if sp := warlordKitSpell(s); sp.Name != "Fire Blast" {
		t.Fatalf("owned blast runes at Magic 66: %s", sp.Name)
	}
	kit := treeFightKit(s)
	if kitQty(kit, arenaDeathRune) != 60 || kitQty(kit, arenaFireRune) != 300 || kitQty(kit, arenaAirRune) != 240 {
		t.Fatalf("blast kit %+v", kit)
	}
	// Too few deaths for the kit: the strongest the bank covers (Fire Bolt needs chaos).
	s.BankIDs[arenaDeathRune], s.BankIDs[arenaChaosRune] = 20, 500
	if sp := warlordKitSpell(s); sp.Name != "Fire Bolt" {
		t.Fatalf("20 deaths and 500 chaos: %s", sp.Name)
	}
	// Nothing owned: the arena's shop spell, Wind Bolt at 17 and up, every rune a Betty line.
	s = warlordSnap(40)
	kit = treeFightKit(s)
	if sp := warlordKitSpell(s); sp.Name != "Wind Bolt" || kitQty(kit, arenaChaosRune) != 80 || kitQty(kit, arenaAirRune) != 160 {
		t.Fatalf("nothing owned: %s, %+v", sp.Name, kit)
	}
	for _, l := range kit {
		if (l.ID == arenaChaosRune || l.ID == arenaAirRune) && (l.Optional || len(l.Sources) == 0 || l.Sources[0].Shop.NpcID != NpcBetty) {
			t.Fatalf("rune line %+v is not a required Betty line", l)
		}
	}
	// A staff of air in the bank: no air line, the staff instead.
	s.BankIDs[1381] = 1
	kit = treeFightKit(s)
	if kitQty(kit, arenaAirRune) != 0 || kitQty(kit, 1381) != 1 {
		t.Fatalf("staff of air owned: %+v", kit)
	}
	for _, l := range kit {
		if why := ValidateKitLine(l); why != "" {
			t.Fatal(why)
		}
	}
}

// Stage 7 never fights without runes: the kit first, then a wait (and the park) when the pack
// still holds too few, and the fight only with runes worth his hitpoints.
func TestTreeWarlordNeedsRunes(t *testing.T) {
	s := warlordSnap(40)
	s.BankKnown = false
	if st := DecideTreeGnomeVillage(s); st.Kind != StepScanBank {
		t.Fatalf("no kit yet and the bank unread: %s", st.Describe())
	}
	s.BankKnown = true
	if st := DecideTreeGnomeVillage(s); st.Kind != StepProvision || st.KitName != KitTreeFight {
		t.Fatalf("no kit yet: %s", st.Describe())
	}
	s.Failed["provision:"+KitTreeFight] = true
	if st := DecideTreeGnomeVillage(s); st.Kind != StepWait {
		t.Fatalf("the kit failed and the pack is empty: %s", st.Describe())
	}
	s.InvIDs[arenaChaosRune], s.InvIDs[arenaAirRune] = 30, 60 // 30 Wind Bolts: 30*9/2.6 = 103
	if st := DecideTreeGnomeVillage(s); st.Kind != StepWait {
		t.Fatalf("runes worth %d of 170: %s", WarlordRuneDamage(s), st.Describe())
	}
	s.InvIDs[arenaChaosRune], s.InvIDs[arenaAirRune] = 80, 160
	if st := DecideTreeGnomeVillage(s); st.Kind != StepCustom || st.Custom != CustomTreeWarlord {
		t.Fatalf("runes worth %d: %s", WarlordRuneDamage(s), st.Describe())
	}
	if c := TreeGnomeVillage.StepDeathCaps[CustomTreeWarlord]; c.Max != 2 || c.Window != 24*time.Hour || c.Skip != 24*time.Hour {
		t.Fatalf("the warlord's death cap %+v", c)
	}
}

// Two deaths on one step inside the window count; an older one is forgotten.
func TestStoreStepDeaths(t *testing.T) {
	s := NewStore()
	t0 := time.Date(2026, 10, 4, 18, 0, 0, 0, time.UTC)
	if n := s.NoteStepDeath("treegnome", CustomTreeWarlord, t0, 24*time.Hour); n != 1 {
		t.Fatalf("first death: %d", n)
	}
	if n := s.NoteStepDeath("treegnome", CustomTreeWarlord, t0.Add(25*time.Hour), 24*time.Hour); n != 1 {
		t.Fatalf("a death 25 hours later: %d", n)
	}
	if n := s.NoteStepDeath("treegnome", CustomTreeWarlord, t0.Add(30*time.Hour), 24*time.Hour); n != 2 {
		t.Fatalf("two inside the window: %d", n)
	}
	if n := s.NoteStepDeath("treegnome", CustomTreeOrb, t0.Add(30*time.Hour), 24*time.Hour); n != 1 {
		t.Fatalf("another step's death: %d", n)
	}
	now := t0.Add(30 * time.Hour)
	s.NoteParkFor("treegnome", "two deaths", now, 24*time.Hour)
	if parked, _ := s.Parked("treegnome", now.Add(23*time.Hour)); !parked {
		t.Fatal("not parked 23 hours on")
	}
	if parked, _ := s.Parked("treegnome", now.Add(24*time.Hour+time.Second)); parked {
		t.Fatal("still parked after the skip")
	}
	data, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeStore(data)
	if err != nil || len(back.Record("treegnome").StepDeaths[CustomTreeWarlord]) != 2 {
		t.Fatalf("round trip: %v %+v", err, back.Record("treegnome"))
	}
}
