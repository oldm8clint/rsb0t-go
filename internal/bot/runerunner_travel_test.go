package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/runerunnerplan"
)

func TestRuneRunnerTravelFollowsItsSettings(t *testing.T) {
	r, err := runerunnerplan.DefaultSettings().Resolve()
	if err != nil {
		t.Fatal(err)
	}
	tr := runeRunnerTravel(r)
	if tr == nil {
		t.Fatal("the three travel settings default on: a Travel was expected")
	}
	if !tr.Alch.Enabled || !tr.Rub || !tr.Tele.Enabled {
		t.Errorf("travel %+v", tr)
	}
	if !tr.Tele.AnyWalk {
		t.Error("RuneRunner's walks are long ones to the shops, not only to the mule: a teleport may start any of them")
	}
	if !tr.Tele.AirRunes {
		t.Error("a runner with no Staff of air pays a teleport's air from its pack")
	}
	if tr.Alch.Normalised().MinMagic < 55 {
		t.Errorf("walk alch below Magic 55: %+v", tr.Alch)
	}

	off := runerunnerplan.DefaultSettings()
	off.WalkAlch, off.WalkRub, off.TeleTravel = false, false, false
	ro, err := off.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if runeRunnerTravel(ro) != nil {
		t.Error("all three off must leave no Travel (and so no aside on a walk)")
	}

	only := runerunnerplan.DefaultSettings()
	only.WalkAlch, only.TeleTravel = false, false
	rr, err := only.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if tr := runeRunnerTravel(rr); tr == nil || tr.Alch.Enabled || tr.Tele.Enabled || !tr.Rub {
		t.Errorf("rub only: %+v", tr)
	}
}

func TestRuneRunnerSettingsCarryTheTravelAndBuyAllDefaults(t *testing.T) {
	s, err := decodeRuneRunnerSettings(map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if !s.BuyAll || !s.WalkAlch || !s.WalkRub || !s.TeleTravel {
		t.Errorf("defaults buyAll=%v walkAlch=%v walkRub=%v teleTravel=%v", s.BuyAll, s.WalkAlch, s.WalkRub, s.TeleTravel)
	}
	if s.TripCoins < 1000000 {
		t.Errorf("tripCoins default %d is under a circuit's cost", s.TripCoins)
	}
	s, err = decodeRuneRunnerSettings(map[string]interface{}{"buyAll": false, "walkAlch": false, "teleTravel": false})
	if err != nil {
		t.Fatal(err)
	}
	if s.BuyAll || s.WalkAlch || s.TeleTravel || !s.WalkRub {
		t.Errorf("explicit values lost: %+v", s.Settings)
	}
}
