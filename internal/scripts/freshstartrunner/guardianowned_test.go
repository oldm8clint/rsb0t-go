package freshstartrunner

import (
	"testing"

	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// guardianOwned is the castle's walls, floor by floor: the ground floor ends at z 3939, the three above go to 3941.
func TestGuardianOwnedIsTheCastlesWallsFloorByFloor(t *testing.T) {
	at := func(x, z, level int) locindex.Match {
		return locindex.Match{Loc: locindex.Loc{X: x, Z: z, Level: level}}
	}
	for _, c := range []struct {
		name string
		m    locindex.Match
		want bool
	}{
		{"the main door", at(3279, 3939, 0), true},
		{"the stairs", at(3280, 3936, 0), true},
		{"a door of the east chamber", at(3294, 3932, 0), true},
		{"the first floor's north-east room door", at(3293, 3934, 1), true},
		{"the roof's stairs", at(3280, 3936, 3), true},
		{"the first tile north of the main door", at(3279, 3940, 0), false},
		{"the same tile one floor up", at(3279, 3940, 1), true},
		{"beyond the east wall", at(3299, 3930, 0), false},
		{"beyond the west wall", at(3274, 3930, 0), false},
		{"south of the walls", at(3280, 3924, 0), false},
		{"a door in Lumbridge", at(3207, 3217, 0), false},
		{"the lair's lever", at(2717, 9801, 0), false},
	} {
		if got := guardianOwned(c.m); got != c.want {
			t.Errorf("%s (%d,%d,%d): guardianOwned = %v, want %v", c.name, c.m.X, c.m.Z, c.m.Level, got, c.want)
		}
	}
}

// From open ground in the deep Wilderness the whole of Rogues' Castle is "near", and every one of a script's own
// recoveries (the door it opens to clear the way, the chain of doors out of a sealed pocket) picked a castle door
// and walked the account back into the courtyards the Guardian had just got it out of: on 6 October 2026 a
// ProgressiveCrafter and four ProgressiveWCFletch/Fisher accounts did it for two hours, opening the east room's
// door, the east chamber's door and each other's leaves. CONTROL: the castle's doors ARE among the nearby
// candidates the loc index and the loc configs give, so it is the filter and not their absence that keeps them out.
func TestADoorHuntNextToTheCastleKeepsOutOfIt(t *testing.T) {
	pk := realRoutePack(t)
	ix, cfg := realLocs(t)
	dest := tile.Tile{X: 3222, Z: 3218, Level: 0}
	for _, from := range []tile.Tile{
		{X: 3277, Z: 3937, Level: 0}, // the west courtyard, inside the walls but open ground
		{X: 3279, Z: 3941, Level: 0}, // just north of the main door
		{X: 3296, Z: 3935, Level: 0}, // the east courtyard
	} {
		raw, owned := 0, 0
		for _, name := range doorNames {
			for _, op := range doorOps {
				for _, m := range ix.Find(cfg, locindex.Query{X: from.X, Z: from.Z, Level: from.Level, Radius: pickDoorCandidateRadius, Name: name, Op: op}, nil) {
					raw++
					if guardianOwned(m) {
						owned++
					}
				}
			}
		}
		if owned == 0 {
			t.Fatalf("control: no castle door is among the %d doors within %d tiles of %v: this test no longer proves the filter", raw, pickDoorCandidateRadius, from)
		}
		found, name, _, ok := pickDoorCandidate(pk, ix, cfg, nil, from, dest)
		if ok && guardianOwned(found) {
			t.Errorf("from %v the door hunt picked the castle's %s %d at %d,%d,%d", from, name, found.ID, found.X, found.Z, found.Level)
		}
	}
}

// The Draynor Manor basement's locs are the quest module's, not the script's recoveries': its doors are lever doors and its ladder
// up is behind them. The control: the basement's own doors and ladder are among the candidates the loc index gives, so it is the
// filter that keeps them out; and a door anywhere else in Draynor is still a candidate.
func TestTheBasementsDoorsAndLadderAreNotRecoveryCandidates(t *testing.T) {
	ix, cfg := realLocs(t)
	from := tile.Tile{X: 3097, Z: 9763, Level: 0}
	raw, owned := 0, 0
	for _, name := range append(append([]string(nil), doorNames...), stairNames...) {
		for _, op := range append(append([]string(nil), doorOps...), stairOpsUp...) {
			for _, m := range ix.Find(cfg, locindex.Query{X: from.X, Z: from.Z, Level: from.Level, Radius: 30, Name: name, Op: op}, nil) {
				raw++
				if guardianOwned(m) {
					owned++
				}
				// The nine puzzle doors (loc types 1to2 ... 5to8) and the ladder up are the basement's, wherever they are found from.
				puzzleDoor := m.ID >= 137 && m.ID <= 145
				if (puzzleDoor || m.ID == questplan.LocPuzzleLadderUp) && !guardianOwned(m) {
					t.Errorf("%s %d at %d,%d,%d is a recovery candidate", name, m.ID, m.X, m.Z, m.Level)
				}
			}
		}
	}
	if owned < 10 {
		t.Fatalf("control: only %d of the %d doors and ladders near %v are owned: the basement holds nine doors and a ladder", owned, raw, from)
	}
	pk := realRoutePack(t)
	found, name, _, ok := pickDoorCandidate(pk, ix, cfg, nil, from, tile.Tile{X: 3053, Z: 3250, Level: 0})
	if ok {
		t.Errorf("a door hunt from the basement's 3to6 door picked the %s %d at %d,%d,%d", name, found.ID, found.X, found.Z, found.Level)
	}
	// The stair search that falls back to "the nearest ladder by name" (tryEscapeKnownPocket, tryChangeLevel) names nothing from inside
	// the basement, where it named the ladder up 172 times and could not walk to it; the same search a short way from the manor still
	// finds the stairs it always found.
	if _, ok := ix.Nearest(cfg, locindex.Query{X: 3097, Z: 9763, Radius: 25, Name: "Ladder", Op: "Climb-up"}, nil); !ok {
		t.Fatal("control: the ladder up is not within 25 tiles of room 3, so the stair search finding nothing proves nothing")
	}
	for _, in := range []tile.Tile{{X: 3097, Z: 9763}, {X: 3108, Z: 9758}, {X: 3116, Z: 9754}} {
		if m, name := pickDirectionalMatch(ix, cfg, nil, in, stairNames, stairOpsUp, 25); m != nil {
			t.Errorf("from %v the directional stair search named the %s %d at %d,%d", in, name, m.ID, m.X, m.Z)
		}
	}
	if m, _ := pickDirectionalMatch(ix, cfg, nil, tile.Tile{X: 3205, Z: 3212}, stairNames, stairOpsUp, 25); m == nil {
		t.Error("control: no stair within 25 tiles of Lumbridge castle")
	}
	// The same ladder name outside the basement stays a candidate: the front door of the manor is not the basement's.
	front := locindex.Match{Loc: locindex.Loc{X: 3109, Z: 3353, Level: 0}}
	if guardianOwned(front) {
		t.Error("the manor's front door above the basement is owned")
	}
}
