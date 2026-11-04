package fleetrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/fleetplan"
)

// IdleAlchDuringStun (the endgame thief, a user request): a failed pickpocket leaves the account hurt (the stun
// damage is a hitmark, so the combat window is open) and low, and the plain idle cast stands aside for both. Inside
// the stun neither can matter: alchemy.rs2 never reads %stunned and nothing else can hurt the account before the
// wait is over, so the stun cast goes out and the five-tick lock still stops a second one inside it.
func TestIdleAlchDuringStunIgnoresTheHitpointRule(t *testing.T) {
	cfg := travelTestConfig(t)
	nature := objOf(t, cfg, "Nature rune", false)
	fire := objOf(t, cfg, "Fire rune", false)
	bowNote := objOf(t, cfg, "Yew shortbow", true)
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: nature, Count: 10}, {ID: fire, Count: 50}, {ID: bowNote, Count: 20}})
	setStat(sim, fleetplan.MagicStat, 200000, 66)
	setStat(sim, 3, 101333, 50) // Hitpoints 50 seen ...
	setStat(sim, 3, 101333, 5)  // ... and 5 now: far under the low-hitpoint line
	simPlace(sim.State(), 2657, 3311)
	now := time.Unix(1_000_000, 0)
	d := &Deps{Session: sim, Config: cfg, Delay: func(time.Duration) bool { return true }}
	tr := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised(), Now: func() time.Time { return now }}

	if tr.IdleAlch(d) {
		t.Fatal("the plain idle cast should stand aside at 5 of 50 hitpoints")
	}
	if !tr.IdleAlchDuringStun(d) {
		t.Fatal("no cast inside a stun at 5 of 50 hitpoints")
	}
	if got := castsSent(sim); len(got) != 1 || got[0].obj != int(bowNote) {
		t.Fatalf("casts = %+v, want one on the bow note", got)
	}
	now = now.Add(time.Second)
	if tr.IdleAlchDuringStun(d) {
		t.Fatal("a second cast inside the lock of the first")
	}
	now = now.Add(3 * time.Second)
	if !tr.IdleAlchDuringStun(d) {
		t.Fatal("no cast once the lock was over")
	}
	if tr.ignoreHP || tr.ignoreCombat {
		t.Fatal("the stun cast left its overrides set for the next caller")
	}
	if tr.IdleAlch(d) {
		t.Fatal("the hitpoint rule did not come back for the plain cast")
	}
	var nilTravel *Travel
	if nilTravel.IdleAlchDuringStun(d) {
		t.Fatal("a nil Travel cast")
	}
}
