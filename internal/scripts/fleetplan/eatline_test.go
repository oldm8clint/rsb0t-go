package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/combatplan"
)

func TestEatLineTable(t *testing.T) {
	tests := []struct {
		name  string
		maxHP int
		hits  []int
		want  int
	}{
		{"nothing facing it is the half line", 55, nil, 27},
		{"nothing facing it, an even maximum", 54, nil, 27},
		{"one Ogre is under the half line, which stands", 55, []int{7}, 27},
		{"two Ogres", 55, []int{7, 7}, 28},
		{"three Ogres", 55, []int{7, 7, 7}, 33}, // 42 is over the 60% cap of 33
		{"four Ogres are capped", 55, []int{7, 7, 7, 7}, 33},
		{"a Fire giant alone is the half line", 55, []int{11}, 27},
		{"two Fire giants", 55, []int{11, 11}, 33},
		{"a small account against a Cow", 10, []int{1}, 5},
		{"a small account against two Ogres is the cap", 14, []int{7, 7}, 9},
		{"an unknown maximum", 0, []int{7}, 0},
		{"a negative hit counts for nothing", 55, []int{-3, 7}, 27},
	}
	for _, tc := range tests {
		if got := EatLine(tc.maxHP, tc.hits); got != tc.want {
			t.Errorf("%s: EatLine(%d, %v) = %d, want %d", tc.name, tc.maxHP, tc.hits, got, tc.want)
		}
	}
}

// The max hits the line is built from are the ones combatplan already carries for the monsters it ladders, and the formula
// reproduces them from content's strength and strength bonus.
func TestHunterMaxHitsAgreeWithCombatplan(t *testing.T) {
	for _, m := range []combatplan.Monster{combatplan.MonsterCow, combatplan.MonsterGoblin, combatplan.MonsterWarrior,
		combatplan.MonsterBarbarian, combatplan.MonsterGiant, combatplan.MonsterMossGiant, combatplan.MonsterOgre,
		combatplan.MonsterIceGiant, combatplan.MonsterFireGiant, combatplan.MonsterWhiteKnight, combatplan.MonsterIceWarrior} {
		// The table holds the most any config of the name deals (a Goblin with a helmet hits for 2, the pen's plain Goblin
		// for 1), so it is never under combatplan's number, and equal to it for the giants and the Ogre.
		got := MaxHitOf(m.Name)
		if got < m.MaxHit || (m.MaxHit >= 4 && got != m.MaxHit) {
			t.Errorf("%s: MaxHitOf = %d, combatplan says %d", m.Name, got, m.MaxHit)
		}
	}
	// content ogre.npc: strength 43, strengthbonus 20.
	if got := NPCMaxHit(43, 20); got != 7 {
		t.Errorf("NPCMaxHit(43, 20) = %d, want 7", got)
	}
	if got := MaxHitOf("something unlisted"); got != DefaultHunterMaxHit {
		t.Errorf("an unknown name = %d, want the default %d", got, DefaultHunterMaxHit)
	}
	if MaxHitOf("  OGRE ") != 7 {
		t.Error("the name lookup is not case and space tolerant")
	}
}
