package combatplan

import (
	"fmt"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The giant spread of 3 Oct 2026 (spread.go, "THE GIANT SPREAD").

func TestSpreadLadderWildernessRungsOnlyWhenAsked(t *testing.T) {
	plain, wild := SpreadLadder(), SpreadLadderWith(SpreadOptions{Wilderness: true})
	if len(wild) != len(plain)+len(wildernessRungs) {
		t.Fatalf("the Wilderness option adds %d rungs, want %d", len(wild)-len(plain), len(wildernessRungs))
	}
	for _, r := range plain {
		if r.Wilderness > 0 {
			t.Fatalf("the default spread ladder holds Wilderness rung %q", r.Key)
		}
	}
	for _, key := range []string{"mossgiants-wilderness", "icegiants-wilderness"} {
		if _, ok := RungByKey(wild, key); !ok {
			t.Fatalf("the Wilderness option lacks %q", key)
		}
	}
	for i, r := range plain {
		if wild[i].Key != r.Key {
			t.Fatalf("rung %d is %q with the option and %q without: the order must be kept", i, wild[i].Key, r.Key)
		}
	}
}

// Every rung's Wilderness field is the deepest content level among its spawns, and nothing that
// says 0 stands on Wilderness ground.
func TestRungWildernessLevelsMatchTheirSpawns(t *testing.T) {
	for _, r := range SpreadLadderWith(SpreadOptions{Wilderness: true}) {
		deepest := 0
		for _, s := range r.Spawns {
			deepest = max(deepest, WildernessLevel(s))
		}
		deepest = max(deepest, WildernessLevel(r.Stand))
		if deepest != r.Wilderness {
			t.Errorf("rung %q says Wilderness %d, its spawns and stand reach %d", r.Key, r.Wilderness, deepest)
		}
	}
}

func TestWildernessLevel(t *testing.T) {
	for _, c := range []struct {
		t    tile.Tile
		want int
	}{
		{tile.Tile{X: 3094, Z: 3491}, 0},   // Edgeville bank, south of the ditch
		{tile.Tile{X: 3094, Z: 3520}, 1},   // the first row past it
		{tile.Tile{X: 3140, Z: 3816}, 38},  // the Wilderness Moss giants
		{tile.Tile{X: 2953, Z: 3895}, 47},  // the Wilderness Ice giants
		{tile.Tile{X: 3088, Z: 10278}, 45}, // the PvP rally (randomevents strand_pvp.go)
		{tile.Tile{X: 3113, Z: 9836}, 0},   // the Edgeville dungeon Giants
		{tile.Tile{X: 2940, Z: 3895}, 0},   // west of the Wilderness box
	} {
		if got := WildernessLevel(c.t); got != c.want {
			t.Errorf("WildernessLevel(%v) = %d, want %d", c.t, got, c.want)
		}
	}
}

func TestWestSide(t *testing.T) {
	for _, c := range []struct {
		name string
		t    tile.Tile
		want bool
	}{
		{"East Ardougne north bank", tile.Tile{X: 2616, Z: 3332}, true},
		{"East Ardougne south bank", tile.Tile{X: 2655, Z: 3283}, true},
		{"Yanille bank", tile.Tile{X: 2613, Z: 3094}, true},
		{"the Yanille Ogres", tile.Tile{X: 2503, Z: 3100}, true},
		{"Catherby bank", tile.Tile{X: 2809, Z: 3440}, true},
		{"Brimhaven, on Karamja", tile.Tile{X: 2760, Z: 3180}, false},
		{"Falador west bank", tile.Tile{X: 2946, Z: 3368}, false},
		{"Edgeville bank", tile.Tile{X: 3094, Z: 3491}, false},
		{"the Edgeville dungeon Giants", tile.Tile{X: 3113, Z: 9836}, false},
		{"under Ardougne (a dungeon)", tile.Tile{X: 2600, Z: 9700}, false},
	} {
		if got := WestSide(c.t); got != c.want {
			t.Errorf("WestSide(%s %v) = %v, want %v", c.name, c.t, got, c.want)
		}
	}
	// Every WestSide rung stands, banks and spawns on that side; no other rung's bank does.
	for _, r := range SpreadLadderWith(SpreadOptions{Wilderness: true}) {
		if !r.WestSide {
			continue
		}
		if !WestSide(r.Stand) || !WestSide(r.Bank) {
			t.Errorf("rung %q is WestSide but its stand %v or bank %v is not", r.Key, r.Stand, r.Bank)
		}
		for _, s := range r.Spawns {
			if !WestSide(s) {
				t.Errorf("rung %q spawn %v is not on the west side", r.Key, s)
			}
		}
	}
}

// Sooner (owner, 3 Oct): the spread ladder's giants band starts at 20 with a hitpoints gate below
// 25; MeleeLadder keeps 25 for the standalone CombatTrainer.
func TestEarlyGiantsBand(t *testing.T) {
	base, _ := RungByKey(MeleeLadder(), "giants")
	early, _ := RungByKey(SpreadLadder(), "giants")
	if base.FromLevel != 25 || base.MinHitpoints != 0 {
		t.Fatalf("MeleeLadder giants is %d-%d hp %d, want the unchanged 25 with no hitpoints gate", base.FromLevel, base.ToLevel, base.MinHitpoints)
	}
	if early.FromLevel != 20 || early.MinHitpoints != 30 || early.MinHitpointsBelow != 25 {
		t.Fatalf("spread giants is from %d with hp %d below %d, want 20 / 30 / 25", early.FromLevel, early.MinHitpoints, early.MinHitpointsBelow)
	}
	at := func(lowest, hp int) Account {
		return Account{LowestMelee: lowest, CombatLevel: 34, Hitpoints: hp, Members: true}
	}
	for _, c := range []struct {
		a    Account
		want bool
	}{
		{at(19, 50), false}, // below the band
		{at(20, 30), true},
		{at(20, 29), false}, // the early band's gate
		{at(24, 0), false},  // unknown hitpoints refuse a gated band
		{at(25, 20), true},  // the old band: no gate, as before
		{at(59, 12), true},
		{Account{LowestMelee: 22, CombatLevel: 29, Hitpoints: 40, Members: true}, false}, // combat 30 still holds
	} {
		if got := early.Admits(c.a); got != c.want {
			t.Errorf("giants admits %+v = %v, want %v", c.a, got, c.want)
		}
	}
	// In the spread: at 22 with 30 hp the giants share the band with the Barbarians and Al-Kharid
	// warriors; at 22 with 25 hp the account still trains, just never at the giants.
	seen := map[string]int{}
	low := map[string]int{}
	for i := 0; i < 600; i++ {
		name := fmt.Sprint("acct", i)
		if c := PickRungSpread(SpreadLadder(), 22, 34, true, Crowd{Account: name, Hitpoints: 30}); c.OK {
			seen[c.Rung.Key]++
		} else {
			t.Fatalf("melee 22 / 30 hp blocked: %s", c.Why)
		}
		if c := PickRungSpread(SpreadLadder(), 22, 34, true, Crowd{Account: name, Hitpoints: 25}); c.OK {
			low[c.Rung.Key]++
		} else {
			t.Fatalf("melee 22 / 25 hp blocked: %s", c.Why)
		}
	}
	if seen["giants"] == 0 || seen["barbarians"] == 0 || seen["alkharid"] == 0 {
		t.Fatalf("melee 22 with 30 hp picks %v, want giants, barbarians and alkharid all used", seen)
	}
	if low["giants"] != 0 || low["barbarians"] == 0 {
		t.Fatalf("melee 22 with 25 hp picks %v: never the giants", low)
	}
	// Unknown hitpoints still train somewhere (the ungated rungs), so a missing stat never stops a run.
	if c := PickRungSpread(SpreadLadder(), 22, 34, true, Crowd{Account: "x"}); !c.OK || c.Rung.Key == "giants" {
		t.Fatalf("melee 22, hitpoints unknown: %+v", c)
	}
}

// Every level from 20 to 74 has an ungated, east-side, proven rung on the spread ladder: lowering the
// Cows hand-over to 20 leaves no level where an account with 20 hitpoints standing in Edgeville
// has nowhere to go.
func TestSpreadLadderHasAnUngatedRungFromTwenty(t *testing.T) {
	for level := 20; level < 75; level++ {
		c := PickRungSpread(SpreadLadder(), level, 75, true, Crowd{Account: "x", Hitpoints: 20})
		if !c.OK {
			t.Fatalf("lowest melee %d, 20 hp, east: blocked (%s %s)", level, c.Why, c.Detail)
		}
		if c.Rung.WestSide || c.Rung.Wilderness > 0 {
			t.Fatalf("lowest melee %d east of the water picked %q", level, c.Rung.Key)
		}
	}
}

// West rungs go only to accounts already on the west side, and there they are preferred over the
// same supply across the water (CrossPenalty).
func TestWestRungsOnlyForWestAccounts(t *testing.T) {
	ladder := SpreadLadder()
	east, west := map[string]int{}, map[string]int{}
	for i := 0; i < 3000; i++ {
		name := fmt.Sprint("swarm", i)
		e := PickRungSpread(ladder, 50, 65, true, Crowd{Account: name, Hitpoints: 50})
		w := PickRungSpread(ladder, 50, 65, true, Crowd{Account: name, Hitpoints: 50, West: true})
		if !e.OK || !w.OK {
			t.Fatalf("blocked: %v / %v", e, w)
		}
		east[e.Rung.Key]++
		west[w.Rung.Key]++
	}
	for _, key := range []string{"mossgiants-ardougne", "ogres"} {
		if east[key] != 0 {
			t.Fatalf("an east account was sent to west rung %q: %v", key, east)
		}
		if west[key] == 0 {
			t.Fatalf("no west account took %q: %v", key, west)
		}
	}
	// A west rung's share against the giants is its supply ratio times CrossPenalty.
	og, _ := RungByKey(ladder, "ogres")
	gi, _ := RungByKey(ladder, "giants")
	want := og.SupplyXPH() / gi.SupplyXPH() * CrossPenalty
	got := float64(west["ogres"]) / float64(west["giants"])
	if got < want*0.75 || got > want*1.3 {
		t.Fatalf("ogres/giants share for west accounts %.2f, want near %.2f (%v)", got, want, west)
	}
	// Below the west rungs' hitpoints gate a west account trains on the east ladder.
	if c := PickRungSpread(ladder, 50, 65, true, Crowd{Account: "x", Hitpoints: 39, West: true}); !c.OK || c.Rung.WestSide {
		t.Fatalf("39 hp: %+v", c)
	}
	// When the band's only rung is on the west side, an east account is told why.
	only := []Rung{westRungs[0]}
	if c := PickRungSpread(only, 45, 60, true, Crowd{Account: "x", Hitpoints: 50}); c.OK || c.Why != BlockedSide {
		t.Fatalf("east account, west-only band: %+v", c)
	}
	if c := PickRungSpread(only, 45, 60, true, Crowd{Account: "x", Hitpoints: 30, West: true}); c.OK || c.Why != BlockedHitpoints {
		t.Fatalf("west account under the gate: %+v", c)
	}
}

func TestWildernessRungsAreGatedAndNeverTheBoneRung(t *testing.T) {
	ladder := SpreadLadderWith(SpreadOptions{Wilderness: true})
	for _, r := range wildernessRungs {
		if r.MinCombatLevel < 50 || r.MinHitpoints < 45 {
			t.Errorf("Wilderness rung %q gates combat %d / hp %d: deep PvP ground wants more", r.Key, r.MinCombatLevel, r.MinHitpoints)
		}
		if r.WestSide {
			t.Errorf("Wilderness rung %q is not on the west side", r.Key)
		}
	}
	// A bone hunt never walks into the Wilderness while a giant rung off it qualifies.
	for _, combat := range []int{40, 80, 126} {
		if c := PrayerRung(ladder, combat, true); !c.OK || c.Rung.Wilderness > 0 {
			t.Fatalf("PrayerRung at combat %d chose %+v", combat, c.Rung.Key)
		}
	}
	// With nothing else, it may.
	if c := PrayerRung(wildernessRungs, 70, true); !c.OK || c.Rung.Wilderness == 0 {
		t.Fatalf("PrayerRung over the Wilderness rungs alone: %+v", c)
	}
}

// The new rungs are sound in the same sense as 2 Oct's: big bones, spawns inside the leash, a stand
// some spawn reaches, a bank, an entrance and a note.
func TestGiantSpreadRungsAreSound(t *testing.T) {
	ladder := SpreadLadderWith(SpreadOptions{Wilderness: true})
	for _, key := range []string{"mossgiants-ardougne", "ogres", "mossgiants-wilderness", "icegiants-wilderness"} {
		r, ok := RungByKey(ladder, key)
		if !ok {
			t.Fatalf("no rung %q", key)
		}
		if !r.Monster.BigBones || !r.TravelProven {
			t.Fatalf("%s: big bones %v, proven %v", key, r.Monster.BigBones, r.TravelProven)
		}
		if r.FromLevel >= r.ToLevel || r.MinCombatLevel < 1 || r.MinHitpoints < 1 {
			t.Fatalf("%s has a bad band %d-%d / combat %d / hp %d", key, r.FromLevel, r.ToLevel, r.MinCombatLevel, r.MinHitpoints)
		}
		near := 0
		for _, s := range r.Spawns {
			if d := tile.Chebyshev(r.Stand, s); d > r.Leash {
				t.Fatalf("%s spawn %v is %d from the stand, outside the %d leash", key, s, d, r.Leash)
			}
			if tile.Chebyshev(r.Stand, s) <= r.Monster.DisengageTiles {
				near++
			}
		}
		if len(r.Spawns) < 3 || near == 0 {
			t.Fatalf("%s: %d spawns, %d within the monster's own chase of the stand", key, len(r.Spawns), near)
		}
		if r.Bank == (tile.Tile{}) || r.Entrance == "" || r.Note == "" {
			t.Fatalf("%s needs a bank, an entrance and a note", key)
		}
	}
}

// safeSpotCastReach is a spell's reach less a tile's margin (scarabplan.CastRange - CastMargin),
// restated because this package cannot import scarabplan.
const safeSpotCastReach = 9

// A recorded safespot is leash-safe from every one of its rung's own spawns (the engine drops a
// melee target more than maxrange+1 tiles from the NPC's spawn, the corner at exactly maxrange+1 on
// both axes included) and within a spell's reach of at least SafeSpotSpawns of them. Line of sight
// and the other monsters nearby are checked on the real pack (the ScarabSwarm plan package's
// rungsafespot_pack_test.go).
func TestSafeSpotsAreOutsideEveryLeash(t *testing.T) {
	n := 0
	for _, r := range SpreadLadderWith(SpreadOptions{Wilderness: true}) {
		if !r.HasSafeSpot() {
			continue
		}
		n++
		cut := r.Monster.DisengageTiles + 1
		inReach := 0
		for _, s := range r.Spawns {
			dx, dz := abs(r.SafeSpot.X-s.X), abs(r.SafeSpot.Z-s.Z)
			if max(dx, dz) <= cut && !(dx == cut && dz == cut) {
				t.Errorf("%s safespot %v is %d,%d from spawn %v: inside its %d-tile chase", r.Key, r.SafeSpot, dx, dz, s, cut)
			}
			if max(dx, dz) <= safeSpotCastReach {
				inReach++
			}
		}
		if inReach < r.SafeSpotSpawns {
			t.Errorf("%s safespot %v has %d spawns in spell reach, records %d", r.Key, r.SafeSpot, inReach, r.SafeSpotSpawns)
		}
	}
	if n < 4 {
		t.Fatalf("%d safespots recorded, want the four verified ones", n)
	}
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
