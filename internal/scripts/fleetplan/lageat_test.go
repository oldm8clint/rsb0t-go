package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/fighterfood"
)

// The Watchman event's hits, by combat tier: strengths 9, 22, 38, 65, 100 and 130 with no strength bonus.
func TestWatchmanEventMaxHitByCombatLevel(t *testing.T) {
	for _, c := range []struct{ combat, want int }{
		{3, 2}, {10, 2}, {11, 3}, {20, 3}, {21, 5}, {40, 5}, {41, 7}, {60, 7}, {61, 11}, {63, 11}, {90, 11}, {91, 14}, {126, 14},
		{0, 14}, // unknown: the hardest
	} {
		if got := WatchmanEventMaxHit(c.combat); got != c.want {
			t.Errorf("WatchmanEventMaxHit(%d) = %d, want %d", c.combat, got, c.want)
		}
	}
}

// The event monsters the eat line meets by type, not by name: a Watchman of the event is not the Yanille Watchman.
func TestEventMonsterMaxHitIsByTypeAndTier(t *testing.T) {
	for _, c := range []struct {
		typ  int
		want int
		ok   bool
	}{
		{391, 2, true}, {396, 14, true}, {413, 2, true}, {418, 14, true}, {419, 2, true}, {424, 14, true}, {425, 2, true}, {430, 14, true},
		{431, 2, true}, {432, 3, true}, {433, 5, true}, {434, 7, true}, {435, 11, true}, {436, 14, true}, {438, 2, true}, {443, 14, true},
		{437, 0, false}, {390, 0, false}, {444, 0, false}, {34, 0, false}, {114, 0, false}, {411, 0, false},
	} {
		got, ok := EventMonsterMaxHit(c.typ)
		if got != c.want || ok != c.ok {
			t.Errorf("EventMonsterMaxHit(%d) = %d, %v; want %d, %v", c.typ, got, ok, c.want, c.ok)
		}
	}
	if got := NPCTypeMaxHit(435, "Watchman"); got != 11 {
		t.Errorf("the event Watchman (type 435) = %d, want 11", got)
	}
	if got := NPCTypeMaxHit(34, "Watchman"); got != DefaultHunterMaxHit {
		t.Errorf("the Yanille Watchman (type 34) = %d, want the name's default %d", got, DefaultHunterMaxHit)
	}
	if got := NPCTypeMaxHit(114, "Ogre"); got != 7 {
		t.Errorf("an Ogre = %d, want 7", got)
	}
}

func TestThievingHitsAreTheWatchmanAndTheStun(t *testing.T) {
	got := ThievingHits(61, 3)
	if len(got) != 2 || got[0] != 11 || got[1] != 3 {
		t.Errorf("ThievingHits(61, 3) = %v, want the Watchman's 11 and the stun's 3", got)
	}
	if SumHits(got...) != 14 {
		t.Errorf("SumHits = %d, want 14", SumHits(got...))
	}
	if SumHits(-4, 5) != 5 {
		t.Error("a negative hit counts for nothing")
	}
}

func TestEatLineLagTable(t *testing.T) {
	ogre := MaxHitOf("Ogre")
	for _, c := range []struct {
		name  string
		maxHP int
		hits  []int
		lag   int
		want  int
	}{
		{"no lag is EatLine", 55, []int{ogre, ogre}, 0, 28},
		{"nothing facing it: the lag adds nothing", 55, nil, 13, 27},
		{"two Ogres after a 13-tick episode: four volleys of 14 are over the cap, 39", 55, []int{ogre, ogre}, 13, 39},
		{"one Ogre after a 13-tick episode: four volleys of 7", 55, []int{ogre}, 13, 28},
		{"one Ogre after a 5-tick episode: two volleys of 7 under the half line", 55, []int{ogre}, 5, 27},
		{"the Watchman beside the account, 61 combat, 54 maximum, 12 ticks", 54, []int{11}, 12, 33},
		{"an unknown maximum", 0, []int{7}, 12, 0},
	} {
		if got := EatLineLag(c.maxHP, c.hits, c.lag); got != c.want {
			t.Errorf("%s: EatLineLag(%d, %v, %d) = %d, want %d", c.name, c.maxHP, c.hits, c.lag, got, c.want)
		}
	}
	if !EatDueLag(33, 54, []int{11}, 12) || EatDueLag(34, 54, []int{11}, 12) {
		t.Error("EatDueLag: the line is 33 of 54 against the Watchman after a 12-tick episode")
	}
	if EatDueLag(0, 54, []int{11}, 12) {
		t.Error("a dead account is due a bite")
	}
}

// THE 18 OCT 04:41 DEATH IN NUMBERS. A Paladin thief of combat 61, 54 hitpoints, at 27 (50%): the thieving rule's 40% line (22) did not
// ask for a bite, and the three hits the Watchman landed in the seven-second silence (11, 9, 7) took it to 0. After a 12-tick episode
// the line is 38; at 38 the worst three Watchman hits leave 5.
func TestTheDeathOfThe18thOctoberIsCoveredByTheLagLine(t *testing.T) {
	const maxHP, combat, stun, hidden = 54, 61, 3, 12
	hits := ThievingHits(combat, stun)
	line := fighterfood.LagEatLine(maxHP, 22, SumHits(hits...), hidden)
	if line != 38 {
		t.Fatalf("the lag line = %d, want 38 (70%% of 54, under the 42 that three volleys of 14 could take)", line)
	}
	if 27 > line {
		t.Fatal("27 of 54 is over the lag line: no bite is asked for")
	}
	watchman := WatchmanEventMaxHit(combat)
	if left := line - 3*watchman; left < 1 {
		t.Errorf("at the line, three Watchman hits of %d leave %d", watchman, left)
	}
	if left := 27 - (11 + 9 + 7); left > 0 {
		t.Errorf("setup: the three hits of the incident should have killed an account at 27, left %d", left)
	}
}
