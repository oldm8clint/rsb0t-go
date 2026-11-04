package antipoisonmakeplan

import "testing"

func snap(mod func(*Snapshot)) Snapshot {
	s := Snapshot{Level: 10, LevelKnown: true, HandOffAt: 27, PackFree: 28}
	if mod != nil {
		mod(&s)
	}
	return s
}

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		s    Snapshot
		want Step
		n    int
	}{
		{"level unknown waits", snap(func(s *Snapshot) { s.LevelKnown = false }), StepWait, 0},
		{"low level stops", snap(func(s *Snapshot) { s.Level = 4 }), StepStop, 0},
		{"enough potions hands off", snap(func(s *Snapshot) { s.Antipoison = 27; s.Unf = 14; s.Dust = 14 }), StepHandOff, 27},
		{"hand-off backing off carries on", snap(func(s *Snapshot) { s.Antipoison = 27; s.HandOffBackoff = true; s.Unf = 3; s.Dust = 3 }), StepAntipoison, 3},
		{"unf and dust finish potions", snap(func(s *Snapshot) { s.Unf = 20; s.Dust = 30 }), StepAntipoison, 14},
		{"spare dust makes unf", snap(func(s *Snapshot) { s.Dust = 10; s.Marrentill = 500; s.VialWater = 27 }), StepUnf, 10},
		{"unf bounded by a batch", snap(func(s *Snapshot) { s.Dust = 100; s.Marrentill = 500; s.VialWater = 500 }), StepUnf, 14},
		{"unf bounded by water", snap(func(s *Snapshot) { s.Dust = 100; s.Marrentill = 500; s.VialWater = 6 }), StepUnf, 6},
		{"unf counts unfinished already made against dust", snap(func(s *Snapshot) { s.Unf = 8; s.Dust = 10; s.Marrentill = 5; s.VialWater = 5 }), StepAntipoison, 8},
		{"dust but no herbs asks the mule", snap(func(s *Snapshot) { s.Dust = 10; s.VialWater = 10 }), StepAskMule, 0},
		{"dust, herbs but no water shops", snap(func(s *Snapshot) { s.Dust = 10; s.Marrentill = 10 }), StepShop, 0},
		{"dust but ask backing off falls to shop for water", snap(func(s *Snapshot) { s.Dust = 10; s.AskBackoff = true }), StepShop, 0},
		{"horns with a pestle grind", snap(func(s *Snapshot) { s.Horn = 20; s.HasPestle = true }), StepGrind, 20},
		{"a full pack of horns leaves the pestle a slot", snap(func(s *Snapshot) { s.Horn = 40; s.HasPestle = true }), StepGrind, 27},
		{"horns with no pestle shop", snap(func(s *Snapshot) { s.Horn = 5 }), StepShop, 0},
		{"nothing at all hunts", snap(nil), StepHunt, 27},
		{"unf waiting on dust hunts", snap(func(s *Snapshot) { s.Unf = 14; s.Marrentill = 400 }), StepHunt, 27},
		{"mule dry with nothing unfinished stops", snap(func(s *Snapshot) { s.AskExhausted = true }), StepStop, 0},
		{"mule dry hands over what it has", snap(func(s *Snapshot) { s.AskExhausted = true; s.Antipoison = 9 }), StepHandOff, 9},
		{"everything backing off waits", snap(func(s *Snapshot) { s.Dust = 5; s.AskBackoff = true; s.ShopBackoff = true }), StepWait, 0},
		{"shop backing off with horns but no pestle waits", snap(func(s *Snapshot) { s.Horn = 5; s.ShopBackoff = true }), StepWait, 0},
	}
	for _, c := range cases {
		got := Decide(c.s)
		if got.Step != c.want {
			t.Errorf("%s: step %s (%s), want %s", c.name, got.Step, got.Why, c.want)
			continue
		}
		if c.n != 0 && got.N != c.n {
			t.Errorf("%s: N %d, want %d", c.name, got.N, c.n)
		}
		if got.Why == "" {
			t.Errorf("%s: no reason given", c.name)
		}
	}
}

func TestUnfBatch(t *testing.T) {
	for _, c := range []struct{ m, w, d, want int }{{100, 100, 100, 14}, {3, 9, 9, 3}, {9, 3, 9, 3}, {9, 9, 3, 3}, {0, 5, 5, 0}, {5, 5, -2, 0}} {
		if got := UnfBatch(c.m, c.w, c.d); got != c.want {
			t.Errorf("UnfBatch(%d,%d,%d) = %d, want %d", c.m, c.w, c.d, got, c.want)
		}
	}
}

func TestHuntHorns(t *testing.T) {
	for _, c := range []struct{ free, want int }{{28, 27}, {27, 27}, {10, 10}, {0, 27}, {-3, 27}} {
		if got := HuntHorns(c.free); got != c.want {
			t.Errorf("HuntHorns(%d) = %d, want %d", c.free, got, c.want)
		}
	}
}

func TestAfterAsk(t *testing.T) {
	f, ex := AfterAsk(false, 0, 3)
	f, ex = AfterAsk(false, f, 3)
	if ex {
		t.Fatalf("exhausted after 2 of 3")
	}
	f, ex = AfterAsk(false, f, 3)
	if !ex || f != 3 {
		t.Fatalf("not exhausted after 3: %d %v", f, ex)
	}
	if f, ex = AfterAsk(true, f, 3); f != 0 || ex {
		t.Fatalf("an arrival should reset: %d %v", f, ex)
	}
	if _, ex = AfterAsk(false, 99, 0); ex {
		t.Fatalf("max 0 must never exhaust")
	}
}

func TestShopFor(t *testing.T) {
	if l := ShopFor(27, 27, true); l.Vials != 27 || l.Pestle {
		t.Errorf("%+v", l)
	}
	if l := ShopFor(27, 27, false); l.Vials != 26 || !l.Pestle {
		t.Errorf("a missing pestle takes a slot: %+v", l)
	}
	if l := ShopFor(10, 27, false); l.Vials != 9 || !l.Pestle {
		t.Errorf("%+v", l)
	}
	if l := ShopFor(0, 27, false); l.Vials != 0 {
		t.Errorf("%+v", l)
	}
}

func TestSettings(t *testing.T) {
	s, err := DecodeSettings(map[string]interface{}{})
	if err != nil || s.Mule != DefaultMule || s.HandOffAt != 27 {
		t.Fatalf("defaults: %+v %v", s, err)
	}
	s, err = DecodeSettings(map[string]interface{}{"mule": "m2", "handOffAt": "20", "kitSize": 100.0, "pinWorld": 3})
	if err != nil || s.Mule != "m2" || s.HandOffAt != 20 || s.KitSize != 100 {
		t.Fatalf("overrides: %+v %v", s, err)
	}
	for name, raw := range map[string]map[string]interface{}{
		"unknown key": {"bogus": 1},
		"bad number":  {"handOffAt": "x"},
		"range":       {"handOffAt": 99},
		"empty mule":  {"mule": ""},
		"log detail":  {"logDetail": "Loud"},
	} {
		if _, err := DecodeSettings(raw); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
