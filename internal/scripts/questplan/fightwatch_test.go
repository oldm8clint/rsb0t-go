package questplan

import (
	"strings"
	"testing"
	"time"
)

func reading(mod func(*FightReading)) FightReading {
	r := FightReading{HP: 25, MaxHP: 33, EatAt: 10, Food: 4, SinceClick: 2 * time.Second, TargetInCombat: true}
	if mod != nil {
		mod(&r)
	}
	return r
}

func TestDecideFightKeepsFightingAHealthyFight(t *testing.T) {
	if v := DecideFight(reading(nil)); v.Action != FightOn {
		t.Fatalf("%+v", v)
	}
}

// The last food is not spent: at the eat line with none or one piece left the fight is given up so
// the account walks away with it.
func TestDecideFightRetreatsAtTheEatLineWithTheLastFood(t *testing.T) {
	for _, food := range []int{0, 1} {
		v := DecideFight(reading(func(r *FightReading) { r.HP, r.Food = 10, food }))
		if v.Action != FightRetreat || !strings.Contains(v.Why, "eat line") {
			t.Errorf("food %d at 10 hitpoints: %+v", food, v)
		}
	}
	// Two pieces left is a fight still worth the food.
	if v := DecideFight(reading(func(r *FightReading) { r.HP, r.Food = 10, 2 })); v.Action == FightRetreat {
		t.Errorf("two pieces left: %+v", v)
	}
	// Above the line nothing is wrong with one piece left.
	if v := DecideFight(reading(func(r *FightReading) { r.HP, r.Food = 11, 1 })); v.Action == FightRetreat {
		t.Errorf("11 hitpoints, one piece: %+v", v)
	}
	// No sustain (eat line 0) has nothing to run out of.
	if v := DecideFight(reading(func(r *FightReading) { r.HP, r.Food, r.EatAt = 3, 0, 0 })); v.Action == FightRetreat {
		t.Errorf("a quest that never eats: %+v", v)
	}
	// An unread hitpoints bar (max 0) is not a reason to run.
	if v := DecideFight(reading(func(r *FightReading) { r.HP, r.MaxHP, r.Food = 0, 0, 0 })); v.Action == FightRetreat {
		t.Errorf("unknown hitpoints: %+v", v)
	}
}

func TestDecideFightRetreatsAfterSixBitesWithoutProgress(t *testing.T) {
	v := DecideFight(reading(func(r *FightReading) { r.EatsSinceProgress = FightStallEats - 1 }))
	if v.Action == FightRetreat {
		t.Fatalf("five bites: %+v", v)
	}
	v = DecideFight(reading(func(r *FightReading) { r.EatsSinceProgress = FightStallEats }))
	if v.Action != FightRetreat || !strings.Contains(v.Why, "6 times") {
		t.Fatalf("six bites: %+v", v)
	}
}

func TestDecideFightReengagesAfterABite(t *testing.T) {
	v := DecideFight(reading(func(r *FightReading) { r.AteSinceClick = true }))
	if v.Action != FightReengage {
		t.Fatalf("%+v", v)
	}
	// Never faster than the gap, bite or no bite.
	v = DecideFight(reading(func(r *FightReading) { r.AteSinceClick, r.SinceClick = true, FightClickMin-time.Millisecond }))
	if v.Action != FightOn {
		t.Fatalf("clicking inside the gap: %+v", v)
	}
}

func TestDecideFightReengagesAnUnhitTarget(t *testing.T) {
	// Hits landing: no click until the slow guard.
	if v := DecideFight(reading(func(r *FightReading) { r.SinceClick = FightClickIdle })); v.Action != FightOn {
		t.Fatalf("hits landing: %+v", v)
	}
	if v := DecideFight(reading(func(r *FightReading) { r.SinceClick = FightClickAny })); v.Action != FightReengage {
		t.Fatalf("the slow guard: %+v", v)
	}
	// No hitmark inside the combat window: click once the idle time has passed.
	idle := func(since time.Duration) FightReading {
		return reading(func(r *FightReading) { r.TargetInCombat, r.SinceClick = false, since })
	}
	if v := DecideFight(idle(FightClickIdle - time.Millisecond)); v.Action != FightOn {
		t.Fatalf("idle but early: %+v", v)
	}
	if v := DecideFight(idle(FightClickIdle)); v.Action != FightReengage {
		t.Fatalf("idle: %+v", v)
	}
}

// A retreat outranks a click: a fight that is being lost is not made better by attacking again.
func TestDecideFightPrefersTheRetreat(t *testing.T) {
	v := DecideFight(reading(func(r *FightReading) {
		r.HP, r.Food, r.AteSinceClick, r.TargetInCombat, r.SinceClick = 9, 0, true, false, 10*time.Second
	}))
	if v.Action != FightRetreat {
		t.Fatalf("%+v", v)
	}
}

func TestHealthFell(t *testing.T) {
	cases := []struct {
		name      string
		prev, cur FightHealth
		want      bool
	}{
		{"no bar yet", FightHealth{}, FightHealth{}, false},
		{"first hit, damage", FightHealth{}, FightHealth{Health: 20, Total: 25}, true},
		{"first hit, a block", FightHealth{}, FightHealth{Health: 25, Total: 25}, false},
		{"lower", FightHealth{Health: 20, Total: 25}, FightHealth{Health: 17, Total: 25}, true},
		{"same", FightHealth{Health: 20, Total: 25}, FightHealth{Health: 20, Total: 25}, false},
		{"regenerated", FightHealth{Health: 20, Total: 25}, FightHealth{Health: 21, Total: 25}, false},
		{"bar lost (no hit read)", FightHealth{Health: 20, Total: 25}, FightHealth{}, false},
	}
	for _, c := range cases {
		if got := HealthFell(c.prev, c.cur); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// Near-deaths are counted inside their window and forgotten after it, like step deaths.
func TestNoteNearDeathCountsInsideTheWindow(t *testing.T) {
	s := NewStore()
	now := time.Unix(2_000_000, 0)
	if n := s.NoteNearDeath("pip", now, NearDeathWindow); n != 1 {
		t.Fatalf("first: %d", n)
	}
	if n := s.NoteNearDeath("pip", now.Add(time.Hour), NearDeathWindow); n != 2 {
		t.Fatalf("second inside the day: %d", n)
	}
	if n := s.NoteNearDeath("pip", now.Add(NearDeathWindow+30*time.Minute), NearDeathWindow); n != 2 {
		// the first fell out of the window, the second (1h) is still inside, this one is new
		t.Fatalf("after the first expired: %d", n)
	}
	if n := s.NoteNearDeath("other", now, NearDeathWindow); n != 1 {
		t.Fatalf("a different quest: %d", n)
	}
}

// The record survives the store's file round trip and an older reader's file (no nearDeaths) loads.
func TestNearDeathsRoundTrip(t *testing.T) {
	s := NewStore()
	s.NoteNearDeath("pip", time.Unix(2_000_000, 0), NearDeathWindow)
	data, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeStore(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Record("pip").NearDeaths; len(got) != 1 {
		t.Fatalf("lost across the round trip: %v", got)
	}
	old, err := DecodeStore([]byte(`{"version":1,"quests":{"pip":{"attempts":3,"deaths":1}}}`))
	if err != nil || len(old.Record("pip").NearDeaths) != 0 || old.Record("pip").Deaths != 1 {
		t.Fatalf("%v %+v", err, old.Record("pip"))
	}
}
