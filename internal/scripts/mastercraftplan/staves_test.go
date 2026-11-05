package mastercraftplan

import "testing"

func TestStavesReadyAtTheOwnersThresholds(t *testing.T) {
	set := DefaultSettings()
	if set.StavesMinStaves != 5000 || set.StavesMinOrbs != 5000 || !set.Staves {
		t.Fatalf("defaults %+v", set)
	}
	both := stockMap(map[string]int{"Battlestaff": 5000, "Fire orb": 5000})
	if p, ok := StavesReady(set, 62, both, 0, 0); !ok || p.Resume {
		t.Fatalf("5k and 5k at Crafting 62 must start: %+v %v", p, ok)
	}
	for name, m := range map[string]map[string]int{
		"short staves": {"Battlestaff": 4999, "Fire orb": 9000},
		"short orbs":   {"Battlestaff": 9000, "Fire orb": 4999},
		"no orbs":      {"Battlestaff": 9000},
	} {
		if _, ok := StavesReady(set, 99, stockMap(m), 0, 0); ok {
			t.Errorf("%s started", name)
		}
	}
	if _, ok := StavesReady(set, 61, both, 0, 0); ok {
		t.Error("Crafting 61 is under the 62 the content asks")
	}
	if _, ok := StavesReady(set, 99, nil, 0, 0); ok {
		t.Error("an unpublished mule starts nothing")
	}
	set.MembersWorld = false
	if _, ok := StavesReady(set, 99, both, 0, 0); ok {
		t.Error("a free world cannot attach orbs (content: members only)")
	}
}

func TestStavesResumeFromTheOwnBankWithNoThreshold(t *testing.T) {
	set := DefaultSettings()
	p, ok := StavesReady(set, 70, stockMap(nil), 300, 280)
	if !ok || !p.Resume {
		t.Fatalf("%+v %v", p, ok)
	}
	if _, ok := StavesReady(set, 70, stockMap(nil), 300, 10); ok {
		t.Error("10 orbs are under a load")
	}
}

func TestPlanStavesAskIsEqualLinesAtTheKitCap(t *testing.T) {
	set := DefaultSettings()
	lines := PlanStavesAsk(set, stockMap(map[string]int{"Battlestaff": 9000, "Fire orb": 5200}))
	if len(lines) != 2 || lines[0] != (Line{BattlestaffName, set.KitSize}) || lines[1] != (Line{FireOrbName, set.KitSize}) {
		t.Errorf("%v", lines)
	}
	lines = PlanStavesAsk(set, stockMap(map[string]int{"Battlestaff": 9000, "Fire orb": 40}))
	if len(lines) != 2 || lines[0].Qty != 40 || lines[1].Qty != 40 {
		t.Errorf("a short orb stock bounds both lines: %v", lines)
	}
	if PlanStavesAsk(set, stockMap(map[string]int{"Battlestaff": 9000})) != nil {
		t.Error("no orbs, no ask")
	}
}

// The round sits after the gem, bow and pearl rounds (the other fast Crafting rounds) and holds off for its pause.
func TestDecidePlacesTheStavesRound(t *testing.T) {
	set := orderSettings()
	stock := map[string]int{"Battlestaff": 6000, "Fire orb": 6000}
	s := readySnapshot(stock)
	s.CraftingLevel = 70
	if d := Decide(set, s); d.Mode != ModeStaves {
		t.Fatalf("got %s (%s), want staves", d.Mode, d.Why)
	}
	s.Ledger.Staves.PauseUntil = s.Now.Add(set.FruitlessPause)
	if d := Decide(set, s); d.Mode == ModeStaves {
		t.Fatalf("a paused staves round was chosen: %s", d.Why)
	}
}
