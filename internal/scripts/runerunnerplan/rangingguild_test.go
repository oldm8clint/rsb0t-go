package runerunnerplan

import (
	"strings"
	"testing"
)

func TestRangingGuildSitesOpensAtRanged40(t *testing.T) {
	base := []string{SiteGerrant, SiteLowe, SiteHickton}
	if got := strings.Join(RangingGuildSites(base, 39, true), ","); got != "Gerrant,Lowe,Hickton" {
		t.Errorf("at 39: %s", got)
	}
	if got := strings.Join(RangingGuildSites(base, 40, true), ","); got != "Gerrant,Lowe,Hickton,Dargaud" {
		t.Errorf("at 40: %s", got)
	}
	if got := strings.Join(RangingGuildSites(base, 70, false), ","); got != "Gerrant,Lowe,Hickton" {
		t.Errorf("setting off: %s", got)
	}
	// Listed by hand under 40 it is taken out; without Hickton it has no stop to follow.
	if got := strings.Join(RangingGuildSites([]string{SiteHickton, SiteDargaud}, 12, true), ","); got != "Hickton" {
		t.Errorf("listed under 40: %s", got)
	}
	if got := strings.Join(RangingGuildSites([]string{SiteLowe}, 50, true), ","); got != "Lowe" {
		t.Errorf("no Hickton: %s", got)
	}
	if DargaudWhy(40) != "" || !strings.Contains(DargaudWhy(1), "Ranged 40 (this account has 1)") {
		t.Errorf("DargaudWhy")
	}
}

// The guild stop follows each Hickton stop, on its world: the mule's world at the start, the last circuit world, and the other
// worlds of the every-world tail.
func TestRingPlacesDargaudAfterEveryHickton(t *testing.T) {
	ring := BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2, 3}, Shops: liveShops(), Phase: 2, Staffs: true, Market: true,
		Ammo: []string{SiteGerrant, SiteLowe, SiteHickton, SiteDargaud}, AmmoEveryWorld: true})
	got := PlanString(ring, 0)
	for _, want := range []string{"deliver@1 Hickton@1 Dargaud@1 draw@1", "Hickton@3 Dargaud@3 hop2 Hickton@2 Dargaud@2 hop1]"} {
		if !strings.Contains(got, want) {
			t.Errorf("ring has no %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "Dargaud@"); n != 3 {
		t.Errorf("%d guild stops, want one a world:\n%s", n, got)
	}
	// Not listed: the ring is the old one.
	old := PlanString(BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2, 3}, Shops: liveShops(), Phase: 2, Ammo: []string{SiteHickton}}), 0)
	if strings.Contains(old, "Dargaud") {
		t.Errorf("guild stop without the site: %s", old)
	}
}

func TestWithRangedTrainingKeepsTheOperatorsKeys(t *testing.T) {
	m := map[string]interface{}{"maxStage": 5.0, "endgameRangedGoal": 60.0}
	out, added := WithRangedTraining(m, 12, true)
	if out["endgameTrainRanged"] != true || out["endgameRangedBow"] != true || out["endgameRangedRun"] != true {
		t.Errorf("stint not switched on: %v", out)
	}
	if out["endgameRangedGoal"] != 60.0 || out["maxStage"] != 5.0 {
		t.Errorf("an operator's key was overwritten: %v", out)
	}
	if len(added) == 0 || strings.Contains(strings.Join(added, ","), "endgameRangedGoal") {
		t.Errorf("added %v", added)
	}
	if _, ok := m["endgameTrainRanged"]; ok {
		t.Errorf("the input map was changed")
	}
	// 40 and over, off, or an unread level change nothing.
	for _, c := range []struct {
		ranged int
		on     bool
	}{{40, true}, {12, false}, {0, true}} {
		if o, a := WithRangedTraining(m, c.ranged, c.on); len(a) != 0 || len(o) != len(m) {
			t.Errorf("%+v changed the settings: %v", c, o)
		}
	}
}
