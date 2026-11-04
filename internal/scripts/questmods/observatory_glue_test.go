package questmods

import (
	"testing"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
)

// When a fight with the goblin guard is given up: no walk over open ground reaches a tile to strike him from, another player holds him
// (he is single-combat, so the attack on him is refused however often it is sent), or no blow has landed in forty seconds.
func TestObsGuardGiveUpReasons(t *testing.T) {
	began := time.Now()
	at := func(s time.Duration) time.Time { return began.Add(s) }
	g := entityquery.Entity{ID: questplan.NpcGoblinGuard, Index: 9, X: 2391, Z: 9458}
	hit := g
	hit.TotalHealth, hit.Health = 43, 30
	busy := g
	busy.InCombat, busy.FaceEntity = true, 32768+4
	own := busy
	own.FaceEntity = 32768 + 7
	cases := []struct {
		name  string
		cur   entityquery.Entity
		self  int
		after time.Duration
		steps int
		want  bool
	}{
		{"fresh fight", g, 7, time.Second, 2, false},
		{"walled in at once", g, 7, time.Second, -1, true},
		{"another player's fight, at the start (the face may be stale)", busy, 7, 3 * time.Second, 2, false},
		{"another player's fight, kept", busy, 7, 8 * time.Second, 2, true},
		{"this account's own fight", own, 7, 30 * time.Second, 2, false},
		{"no blow in thirty-nine seconds", g, 7, 39 * time.Second, 2, false},
		{"no blow in forty seconds", g, 7, 40 * time.Second, 2, true},
		{"a blow landed, a long fight", hit, 7, 3 * time.Minute, 2, false},
	}
	for _, c := range cases {
		if got := obsGuardGiveUp(c.cur, c.self, began, at(c.after), c.steps) != ""; got != c.want {
			t.Errorf("%s: gave up %v, want %v", c.name, got, c.want)
		}
	}
	// The budget is long enough for the model's kill and short enough that a fight that is not being won ends before the account does.
	if obsFightBudget < 2*time.Minute || obsFightBudget > 6*time.Minute {
		t.Errorf("the fight budget is %s", obsFightBudget)
	}
}

// The module's own regions drive the customs: a half-finished trip is picked up where the account stands.
func TestTheObservatoryCustomsAreAllRegistered(t *testing.T) {
	q := Observatory()
	for _, name := range []string{questplan.CustomObsMould, questplan.CustomObsTelescope, questplan.CustomObsAsh, questplan.CustomObsLeave} {
		if q.Customs[name] == nil {
			t.Errorf("no executor for %q", name)
		}
	}
	s := SeaSlug()
	for _, name := range []string{questplan.CustomSlugToPlatform, questplan.CustomSlugToIsland, questplan.CustomSlugToMainland, questplan.CustomSlugDown,
		questplan.CustomSlugKennith, questplan.CustomSlugKent, questplan.CustomSlugRub, questplan.CustomSlugPanel, questplan.CustomSlugCrane} {
		if s.Customs[name] == nil {
			t.Errorf("no executor for %q", name)
		}
	}
	if len(q.Customs) != 4 || len(s.Customs) != 9 {
		t.Errorf("%d and %d executors: a custom step was added with no test of its name", len(q.Customs), len(s.Customs))
	}
}
