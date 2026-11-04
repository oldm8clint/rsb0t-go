package fleetplan

import (
	"strings"
	"testing"
	"time"
)

// The run staller stands aside "so a bite can land" only when there is a bite: 18 Oct 15:34:56 and 15:37:05, 18 and 20 of 55 with no food.
func TestTheStallerDoesNotStandAsideForABiteThatCannotLand(t *testing.T) {
	for _, hp := range []int{18, 20} {
		if !StallStandAside(hp, 55, false) {
			t.Errorf("%d/55 with food carried: the staller stands aside for the bite", hp)
		}
		if StallStandAside(hp, 55, true) {
			t.Errorf("%d/55 with no food: there is no bite to stand aside for", hp)
		}
	}
	if StallStandAside(40, 55, false) || StallStandAside(40, 55, true) {
		t.Error("40/55 is over the line either way")
	}

	rub := RubPoll{SinceClick: 2 * time.Second, SinceRub: time.Hour, Ahead: RubAhead(true), Running: true, HP: 18, MaxHP: 55}
	if ShouldRub(rub) {
		t.Error("18/55 with food: the rub waits for the bite")
	}
	rub.NoFood = true
	if !ShouldRub(rub) {
		t.Error("18/55 with no food: the rub goes on")
	}

	alch := readyPoll()
	alch.HP, alch.MaxHP = 18, 55
	if d := ShouldWalkAlch(walkAlchOn(), alch); d.Cast || !strings.Contains(d.Why, "free to eat") {
		t.Errorf("18/55 with food: the cast waits for the bite (%+v)", d)
	}
	alch.NoFood = true
	if d := ShouldWalkAlch(walkAlchOn(), alch); !d.Cast {
		t.Errorf("18/55 with no food: the cast goes on (%+v)", d)
	}

	ench := enchantPoll(func(p *WalkEnchantPoll) { p.HP, p.MaxHP = 18, 55 })
	if d := ShouldWalkEnchant(ench); d.Cast {
		t.Errorf("18/55 with food: the enchant waits for the bite (%+v)", d)
	}
	ench.NoFood = true
	if d := ShouldWalkEnchant(ench); !d.Cast {
		t.Errorf("18/55 with no food: the enchant goes on (%+v)", d)
	}
}
