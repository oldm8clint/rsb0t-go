package runecraftplan

import "testing"

// The teleport kit is a convenience: a kit-only ask is Optional, is made only when it would make the kit usable, and not again for
// KitAskGap (the review of 13 Oct: it went through the required-ask path, so a mule short of Water was asked on every round and a
// busy mule failed the round).
func TestKitOnlyAskIsOptionalAndWorthATrade(t *testing.T) {
	kitBase := func() Snapshot {
		s := base()
		s.BankTalisman[KeyAir], s.BankEssence = 1, 300
		s.KitWanted = true
		s.MuleKit = Kit{Law: 28403, Water: 500, Air: 683500}
		return s
	}
	// A mule with everything: asked, and the ask is Optional.
	step := DecideLevelStage(kitBase())
	if step.Stage != StageAskMule || !step.Optional {
		t.Fatalf("a kit-only ask is optional: %+v", step)
	}
	for name, mod := range map[string]func(s *Snapshot){
		"no Water on the mule":                   func(s *Snapshot) { s.MuleKit.Water = 0 },
		"one Water on the mule":                  func(s *Snapshot) { s.MuleKit.Water = 1 },
		"no Law on the mule":                     func(s *Snapshot) { s.MuleKit.Law = 0 },
		"too little Air on the mule":             func(s *Snapshot) { s.MuleKit.Air = 10 },
		"asked within the gap":                   func(s *Snapshot) { s.KitAskRecent = true },
		"a bank short of Water and a dry mule":   func(s *Snapshot) { s.BankKit = Kit{Law: 20, Water: 0, Air: 80}; s.MuleKit.Water = 0 },
		"a bank short of Water, a mule with one": func(s *Snapshot) { s.BankKit = Kit{Law: 20, Water: 0, Air: 80}; s.MuleKit.Water = 1 },
	} {
		s := kitBase()
		mod(&s)
		if got := DecideLevelStage(s); got.Stage != StageBank {
			t.Errorf("%s: %+v, want a bank visit and no trade", name, got)
		}
	}
	// A top-up that completes what is short is worth it: Air alone from a bank that holds the rest.
	s := kitBase()
	s.BankKit = Kit{Law: 20, Water: 10, Air: 10}
	if got := DecideLevelStage(s); got.Stage != StageAskMule || !got.Optional || len(got.Lines) != 1 || got.Lines[0].Item != RuneAir || got.Lines[0].Qty != KitTarget.Air-10 {
		t.Fatalf("Air alone: %+v", got)
	}
	// A required ask (essence or a talisman missing) carries the kit lines along and is NOT optional: its trade is made anyway.
	r := kitBase()
	r.BankEssence, r.MuleEssence = 0, 3000
	got := DecideLevelStage(r)
	if got.Stage != StageAskMule || got.Optional {
		t.Fatalf("a required ask: %+v", got)
	}
}

func TestKitCompletesNeedsEveryRuneAtKitMin(t *testing.T) {
	s := Snapshot{BankKit: Kit{Law: 1, Water: 1, Air: 1}}
	for _, c := range []struct {
		lines []Line
		want  bool
	}{
		{[]Line{{RuneLaw, 3}, {RuneWater, 1}, {RuneAir, 15}}, true},
		{[]Line{{RuneLaw, 3}, {RuneWater, 1}, {RuneAir, 14}}, false},
		{[]Line{{RuneLaw, 2}, {RuneWater, 1}, {RuneAir, 15}}, false},
		{[]Line{{RuneLaw, 3}, {RuneAir, 15}}, false},
		{nil, false},
	} {
		if got := s.kitCompletes(c.lines); got != c.want {
			t.Errorf("%v: %v, want %v", c.lines, got, c.want)
		}
	}
}
