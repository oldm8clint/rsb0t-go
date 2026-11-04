package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/questplan"
)

// A quest's eat line after a view lag (lageat-1018, 18 Oct 2026): the module's own line (or the walk floor's half) is raised, while the
// account is being hit, to what the foe could have done in the worst recent episode.

// lagQuestHitSelf lands a hitmark on the local player (PLAYER_INFO's local-player update with the hitmark mask), which puts the state in
// combat: 1 + 2 + 8 + 11 bits of the movement block, then the extended block.
func lagQuestHitSelf(s *gamestate.State) {
	buf := make([]byte, 3)
	pos := 0
	put := func(bits int, v uint32) {
		for i := bits - 1; i >= 0; i-- {
			if v>>uint(i)&1 == 1 {
				buf[pos>>3] |= 1 << uint(7-pos&7)
			}
			pos++
		}
	}
	put(1, 1)
	put(2, 0)
	put(8, 0)
	put(11, 2047)
	w := packet.NewWriter(8)
	w.P1(0x10) // the hitmark update
	w.P1(5)
	w.P1(1)
	w.P1(7)
	w.P1(10)
	s.Handle(serverprot.PLAYER_INFO, append(buf, w.Data[:w.Pos]...))
}

func TestTheQuestEatLineRisesOnlyWhileTheAccountIsHitAfterALag(t *testing.T) {
	st := gamestate.New()
	d := &Deps{Session: fakeSession{st}}
	const maxHP, half = 54, 27
	if got := lagEatAt(d, 20, maxHP, half, 11); got != half {
		t.Fatalf("no lag in the memory: %d, want the line %d", got, half)
	}
	lag := &gamestate.ViewLag{Recent: gamestate.RecentLag{Ticks: 12, Ago: 15 * time.Second}}
	st.OverrideViewLag(lag)
	if got := lagEatAt(d, 20, maxHP, half, 11); got != half {
		t.Errorf("a lag but nothing hitting the account: %d, want the line %d", got, half)
	}
	lagQuestHitSelf(st)
	if !st.InCombat() {
		t.Fatal("setup: the hitmark did not put the account in combat")
	}
	if got := lagEatAt(d, 20, maxHP, half, 11); got != 33 {
		t.Errorf("hit after a 12-tick lag by a foe that hits for 11: %d, want 33 (three volleys)", got)
	}
	if got := lagEatAt(d, 20, maxHP, half, 0); got != half {
		t.Errorf("an unknown foe (one unnamed hitter, 3 x 8 = 24) = %d, want the line %d: it does not reach the half line", got, half)
	}
	if got := lagEatAt(d, 39, maxHP, half, 11); got != half {
		t.Errorf("39 of 54, over the lag cap = %d, want the line %d", got, half)
	}
	// A bigger foe through the walk floor (peak unknown there, so the unnamed hitter): still the half line.
	if got := lagEatAt(d, 27, maxHP, questplan.FloorEatLine(maxHP), 0); got != half {
		t.Errorf("the floor line after a lag = %d, want %d", got, half)
	}
	// And the same account on a quest module that names its foe's hardest hit gets the raise on the fight guard's own line.
	st.OverrideViewLag(&gamestate.ViewLag{Recent: gamestate.RecentLag{Ticks: 16, Ago: time.Second}})
	if got := lagEatAt(d, 20, maxHP, 15, 11); got != 38 {
		t.Errorf("a module line of 15 against a foe of 11 after a 16-tick lag = %d, want the cap 38", got)
	}
}

func TestFloorEatLineIsTheHalfRoundedDown(t *testing.T) {
	for _, c := range []struct{ max, want int }{{54, 27}, {55, 27}, {10, 5}, {0, 0}} {
		if got := questplan.FloorEatLine(c.max); got != c.want {
			t.Errorf("FloorEatLine(%d) = %d, want %d", c.max, got, c.want)
		}
	}
	// FloorEatDue is FloorEatDueAt at that line.
	for hp := 0; hp <= 56; hp++ {
		if questplan.FloorEatDue(hp, 54, time.Hour) != questplan.FloorEatDueAt(hp, 54, questplan.FloorEatLine(54), time.Hour) {
			t.Fatalf("FloorEatDue and FloorEatDueAt disagree at %d", hp)
		}
	}
}
