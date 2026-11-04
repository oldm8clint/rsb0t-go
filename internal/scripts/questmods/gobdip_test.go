package questmods

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// gobdipSpawns are the Goblin Village's 21 goblins (content maps m46_54, npc 298 and 299; the generals 296 and 297 are not
// farmed).
var gobdipSpawns = []tile.Tile{{X: 2951, Z: 3481}, {X: 2951, Z: 3483}, {X: 2951, Z: 3497}, {X: 2951, Z: 3501}, {X: 2952, Z: 3507},
	{X: 2952, Z: 3510}, {X: 2953, Z: 3487}, {X: 2954, Z: 3491}, {X: 2954, Z: 3500}, {X: 2954, Z: 3503}, {X: 2955, Z: 3507},
	{X: 2956, Z: 3500}, {X: 2957, Z: 3482}, {X: 2957, Z: 3484}, {X: 2957, Z: 3492}, {X: 2957, Z: 3508}, {X: 2958, Z: 3507},
	{X: 2959, Z: 3499}, {X: 2962, Z: 3503}, {X: 2963, Z: 3514}, {X: 2964, Z: 3511}}

func gobNpc(index, x, z int) entityquery.Entity {
	return entityquery.Entity{Kind: entityquery.KindNpc, Index: index, ID: questplan.NpcGoblinGreenArmour, Name: "Goblin", X: x, Z: z, FaceEntity: -1}
}

func TestPickGoblinTakesTheNearestByTheWalkNotByDistance(t *testing.T) {
	now := time.Now()
	// The goblin at 5 is two tiles away by distance and fifty by the walk; the one at 9 is nine and nine.
	steps := func(t tile.Tile) int {
		if t.X == 5 {
			return 50
		}
		return t.X
	}
	npcs := []entityquery.Entity{gobNpc(1, 5, 0), gobNpc(2, 9, 0)}
	g, n, ok := pickGoblin(npcs, 7, now, nil, steps)
	if !ok || g.Index != 2 || n != 9 {
		t.Errorf("picked %d (%d steps, %v), want the goblin nine steps away", g.Index, n, ok)
	}
}

func TestPickGoblinSkipsWhatItCannotOrShouldNotFight(t *testing.T) {
	now := time.Now()
	const self = 7
	open := func(t tile.Tile) int { return t.X }
	busy := gobNpc(1, 1, 0)
	busy.InCombat, busy.FaceEntity = true, 32768+3 // another player's fight
	stale := gobNpc(2, 2, 0)
	stale.FaceEntity = 32768 + 3 // still faces the last player to hit it, who has gone
	own := gobNpc(3, 3, 0)
	own.InCombat, own.FaceEntity = true, 32768+self // this account's own
	north := gobNpc(4, 4, questplan.GoblinVillageNorth+1)
	other := gobNpc(5, 5, 0)
	other.ID = questplan.NpcGeneralWartface // a general is not farmed
	walled := gobNpc(6, 6, 0)
	skipped := gobNpc(7, 7, 0)
	skip := map[int]time.Time{7: now.Add(time.Minute), 99: now.Add(-time.Minute)}
	steps := func(t tile.Tile) int {
		if t.X == 6 {
			return -1 // no walk over open ground
		}
		return open(t)
	}

	npcs := []entityquery.Entity{busy, stale, own, north, other, walled, skipped}
	g, _, ok := pickGoblin(npcs, self, now, skip, steps)
	if !ok || g.Index != 2 {
		t.Fatalf("picked %d (%v), want the goblin that merely still faces a player who has gone (the nearest that is free)", g.Index, ok)
	}
	// Without it, the account's own fight is next; without that, none of the rest qualifies.
	g, _, ok = pickGoblin([]entityquery.Entity{busy, own, north, other, walled, skipped}, self, now, skip, steps)
	if !ok || g.Index != 3 {
		t.Errorf("picked %d (%v), want the goblin in this account's own fight", g.Index, ok)
	}
	if g, _, ok = pickGoblin([]entityquery.Entity{busy, north, other, walled, skipped}, self, now, skip, steps); ok {
		t.Errorf("picked %d, want none: busy, north of the edge, not a goblin, behind a wall and given up on", g.Index)
	}
	// The skip ends on its own.
	if g, _, ok = pickGoblin([]entityquery.Entity{skipped}, self, now.Add(2*time.Minute), skip, steps); !ok || g.Index != 7 {
		t.Errorf("a goblin given up on is still left alone after the skip ended: %d %v", g.Index, ok)
	}
	// With the account's own index unknown no goblin is another player's.
	if g, _, ok = pickGoblin([]entityquery.Entity{busy}, -1, now, nil, steps); !ok || g.Index != 1 {
		t.Errorf("with self unknown the busy goblin was refused: %d %v", g.Index, ok)
	}
}

func TestPickGoblinBetweenEqualsTakesTheLowerIndex(t *testing.T) {
	steps := func(tile.Tile) int { return 4 }
	for _, order := range [][]entityquery.Entity{{gobNpc(8, 1, 0), gobNpc(3, 2, 0), gobNpc(5, 3, 0)}, {gobNpc(5, 3, 0), gobNpc(3, 2, 0), gobNpc(8, 1, 0)}} {
		if g, _, ok := pickGoblin(order, 0, time.Now(), nil, steps); !ok || g.Index != 3 {
			t.Errorf("picked %d, want the lowest index of three equals whatever the order", g.Index)
		}
	}
}

// A fight is given up for the reasons the log showed: the goblin walled in or wandered out, another player's fight, and no
// blow landing in thirty seconds. A fresh fight is not (the health bar is drawn by the first hit).
func TestGoblinGiveUpReasons(t *testing.T) {
	began := time.Now()
	at := func(s time.Duration) time.Time { return began.Add(s) }
	g := gobNpc(1, 2955, 3500)
	hit := g
	hit.TotalHealth, hit.Health = 12, 6
	busy := g
	busy.InCombat, busy.FaceEntity = true, 32768+4
	north := g
	north.Z = questplan.GoblinVillageNorth + 2
	cases := []struct {
		name  string
		cur   entityquery.Entity
		self  int
		after time.Duration
		steps int
		want  bool
	}{
		{"fresh fight", g, 7, time.Second, 6, false},
		{"walled in at once", g, 7, time.Second, -1, true},
		{"wandered north", north, 7, time.Second, 6, true},
		{"another player's fight, at the start (the face may be stale)", busy, 7, 2 * time.Second, 6, false},
		{"another player's fight, kept", busy, 7, 5 * time.Second, 6, true},
		{"this account's own fight", func() entityquery.Entity { e := busy; e.FaceEntity = 32768 + 7; return e }(), 7, 20 * time.Second, 6, false},
		{"no blow in twenty-nine seconds", g, 7, 29 * time.Second, 6, false},
		{"no blow in thirty seconds", g, 7, 30 * time.Second, 6, true},
		{"a blow landed, a long fight", hit, 7, 50 * time.Second, 6, false},
	}
	for _, c := range cases {
		why, skip := goblinGiveUp(c.cur, c.self, began, at(c.after), c.steps)
		if got := why != ""; got != c.want {
			t.Errorf("%s: give up = %v, want %v (%q)", c.name, got, c.want, why)
		}
		// A goblin another player is fighting is back in half a minute; one behind a wall, north of the edge or never hit is not.
		wantSkip := goblinSkipFor
		if strings.Contains(why, "another player") {
			wantSkip = goblinSkipBusy
		}
		if c.want && skip != wantSkip {
			t.Errorf("%s: left alone for %s, want %s", c.name, skip, wantSkip)
		}
		if !c.want && skip != 0 {
			t.Errorf("%s: a fight kept up left the goblin alone for %s", c.name, skip)
		}
	}
}

// The farm's pick on the real ground. Live 17 Oct 2026 FightNpc took the nearest goblin by distance, and two of the village's
// 21 spawns are inside huts whose doors are shut in the collision data: the walk to them is "no path ... unreachable", the
// attack goes nowhere, and the same goblin stays the nearest. 115 of 181 three-minute timeouts followed such a line.
func TestTheGoblinFarmLeavesTheHutGoblinsAlone(t *testing.T) {
	pk := impPackOrSkip(t)
	huts := map[tile.Tile]bool{{X: 2951, Z: 3501}: true, {X: 2952, Z: 3507}: true}
	for _, stand := range []tile.Tile{questplan.GoblinVillageField, {X: 2955, Z: 3489}, {X: 2958, Z: 3503}, {X: 2960, Z: 3495}} {
		reach := pk.ReachFrom(pathfind.NavPoint{X: stand.X, Z: stand.Z}, goblinReachWindow, nil)
		if reach.Size() == 0 {
			t.Fatalf("standing at %v the flood is empty", stand)
		}
		open, shut := 0, 0
		for _, sp := range gobdipSpawns {
			steps := interactSteps(pk, reach, stand, sp)
			switch {
			case huts[sp] && steps >= 0:
				t.Errorf("from %v the hut spawn %v is %d steps over open ground; its door is shut", stand, sp, steps)
			case !huts[sp] && steps < 0:
				t.Errorf("from %v the open spawn %v has no walk over open ground", stand, sp)
			case steps < 0:
				shut++
			default:
				open++
			}
		}
		if shut != 2 || open != 19 {
			t.Errorf("from %v: %d open spawns and %d shut, want 19 and 2", stand, open, shut)
		}
	}
}

// The tiles the log's timed-out fights stood in front of ("no path from ... to 2950,3500 ... 2953,3506") are the insides of the
// two huts; none of them has a tile to fight from.
func TestTheGoblinFarmReachesNoTileInsideTheHuts(t *testing.T) {
	pk := impPackOrSkip(t)
	stand := questplan.GoblinVillageField
	reach := pk.ReachFrom(pathfind.NavPoint{X: stand.X, Z: stand.Z}, goblinReachWindow, nil)
	for _, hut := range [][4]int{{2950, 3500, 2951, 3502}, {2952, 3505, 2953, 3506}} {
		for x := hut[0]; x <= hut[2]; x++ {
			for z := hut[1]; z <= hut[3]; z++ {
				if n := interactSteps(pk, reach, stand, tile.Tile{X: x, Z: z}); n >= 0 {
					t.Errorf("a goblin at %d,%d inside a hut is %d steps from the field", x, z, n)
				}
			}
		}
	}
	// And the open ground beside them is fought as before.
	for _, open := range []tile.Tile{{X: 2954, Z: 3500}, {X: 2956, Z: 3500}, {X: 2955, Z: 3507}, {X: 2957, Z: 3492}} {
		if n := interactSteps(pk, reach, stand, open); n < 0 || n > 30 {
			t.Errorf("open ground at %v is %d steps from the field", open, n)
		}
	}
}
