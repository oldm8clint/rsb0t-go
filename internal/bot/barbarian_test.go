package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/barbarianplan"
	"rsb0t-go/internal/scripts/tile"
)

// An earlier recover waited for !IsDead before calling ClearDead, which nothing else clears, so
// it timed out every 20 s forever and the account never banked or walked back. Recovery must
// finish after the respawn jump, clear the death and force a bank trip.
func TestBarbarianRecoverClearsTheDeathAndForcesABankTrip(t *testing.T) {
	old := respawnWait
	respawnWait = 2 * time.Second
	defer func() { respawnWait = old }()

	sess := newAPISession()
	readyScene(sess.st)
	ctx := apiContext(sess, nil)
	b := &barbarianBot{cfg: readBarbarianSettings(nil)}
	b.lap = b.freshLap()
	b.lap.Step = 3

	w := packet.NewWriter(64)
	w.PJStr("Oh dear you are dead!")
	sess.st.Handle(serverprot.MESSAGE_GAME, w.Data[:w.Pos])
	if !sess.st.IsDead() {
		t.Fatal("setup: the death line did not mark the state dead")
	}

	stop := ticker(sess.st, 5*time.Millisecond)
	defer stop()
	go func() {
		time.Sleep(50 * time.Millisecond)
		placeAt(sess.st, 0, 20, 20) // the respawn teleport: 40 tiles away
	}()

	start := time.Now()
	if err := b.Loop(ctx); err != nil {
		t.Fatalf("Loop: %v", err)
	}
	if sess.st.IsDead() {
		t.Fatal("recover returned with the death still set, so the next Loop recovers again")
	}
	if !b.banking {
		t.Fatal("recover must force a bank trip, as TS recover() sets banking")
	}
	if b.deaths != 1 || b.lap.Step != 0 {
		t.Fatalf("deaths %d, lap step %d; want 1 and a fresh lap", b.deaths, b.lap.Step)
	}
	if time.Since(start) >= respawnWait {
		t.Fatalf("recover waited the whole respawn window (%v) although the respawn jump arrived", time.Since(start))
	}
}

func TestReadBarbarianSettingsDefaultsAndClamps(t *testing.T) {
	s := readBarbarianSettings(nil)
	if s.food != barbarianplan.DefaultFood || s.foodWithdraw != 20 || s.minFood != 1 || s.maxLaps != 0 {
		t.Fatalf("defaults: %+v", s)
	}
	if s.bankTile != barbarianplan.DefaultBank || len(s.lap) != len(barbarianplan.DefaultLap) || !s.cheatSetup || s.eatBelowFrac != 0.5 {
		t.Fatalf("defaults: %+v", s)
	}

	s = readBarbarianSettings(map[string]interface{}{
		"food":         "   ",
		"foodWithdraw": float64(99),
		"minFood":      float64(-3),
		"eatBelowHp":   float64(5),
		"bankTile":     "2615,3332",
		"lap":          "Log balance, , Obstacle net",
		"cheatSetup":   false,
	})
	if s.food != barbarianplan.DefaultFood {
		t.Fatalf("blank food = %q, want the default", s.food)
	}
	if s.foodWithdraw != 28 || s.minFood != 0 || s.eatBelowFrac != 0.1 || s.cheatSetup {
		t.Fatalf("clamps: %+v", s)
	}
	if s.bankTile != (tile.Tile{X: 2615, Z: 3332, Level: 0}) {
		t.Fatalf("bankTile = %+v", s.bankTile)
	}
	if len(s.lap) != 2 || s.lap[0] != "Log balance" || s.lap[1] != "Obstacle net" {
		t.Fatalf("lap = %q", s.lap)
	}

	if s := readBarbarianSettings(map[string]interface{}{"foodWithdraw": float64(0)}); s.foodWithdraw != 1 {
		t.Fatalf("foodWithdraw 0 clamps to %d, want 1", s.foodWithdraw)
	}
}

func TestBarbarianBankTripNeedsAtLeastOneFood(t *testing.T) {
	cases := []struct {
		held, minFood int
		want          bool
	}{
		{0, 0, false}, // minFood 0 still needs one, as TS max(1, minFood)
		{1, 0, true},
		{2, 3, false},
		{3, 3, true},
	}
	for _, c := range cases {
		if got := barbarianEnoughFood(c.held, c.minFood); got != c.want {
			t.Errorf("barbarianEnoughFood(%d, %d) = %v, want %v", c.held, c.minFood, got, c.want)
		}
	}
}
