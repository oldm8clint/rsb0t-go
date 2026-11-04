package fleetplan

import (
	"testing"
	"time"
)

func TestIsRubItemTakesEveryCharge(t *testing.T) {
	for _, n := range []string{"Games necklace(8)", "Games necklace(1)", "ring of dueling(3)", " Ring of dueling(8) "} {
		if !IsRubItem(n) {
			t.Errorf("%q is a rub item", n)
		}
	}
	for _, n := range []string{"Amulet of glory(4)", "Gold ring", "Necklace mould", ""} {
		if IsRubItem(n) {
			t.Errorf("%q is not a rub item", n)
		}
	}
}

// A rub goes out running, two ticks after the last click, three after the last rub, with the
// click's target far enough ahead that the walk will not click inside the rub's delay; never while
// walking, fighting or with a bank or shop open.
func TestShouldRubGates(t *testing.T) {
	ready := RubPoll{SinceClick: 2 * time.Second, SinceRub: time.Hour, Ahead: RubAhead(true), Running: true}
	if !ShouldRub(ready) {
		t.Fatal("a ready poll did not rub")
	}
	for name, mut := range map[string]func(*RubPoll){
		"walking":                    func(p *RubPoll) { p.Running = false },
		"in combat":                  func(p *RubPoll) { p.InCombat = true },
		"a bank open":                func(p *RubPoll) { p.Modal = true },
		"hitpoints at 12 of 26":      func(p *RubPoll) { p.HP, p.MaxHP = 12, 26 },
		"a click one tick ago":       func(p *RubPoll) { p.SinceClick = 600 * time.Millisecond },
		"the last rub two ticks ago": func(p *RubPoll) { p.SinceRub = 1200 * time.Millisecond },
		"the next click inside it":   func(p *RubPoll) { p.Ahead = RubAhead(true) - 1 },
		"a slower tick stretches it": func(p *RubPoll) { p.Tick, p.SinceRub = 700*time.Millisecond, 1800*time.Millisecond },
	} {
		p := ready
		mut(&p)
		if ShouldRub(p) {
			t.Errorf("%s: rubbed", name)
		}
	}
	if RubAhead(true) != 10 || RubAhead(false) != 7 {
		t.Errorf("RubAhead = %d running, %d walking; want 10 and 7", RubAhead(true), RubAhead(false))
	}
}

func TestRubAskLineRidesOnlyWhenNoneIsHeld(t *testing.T) {
	if _, ok := RubAskLine(true); ok {
		t.Fatal("asked with one held")
	}
	l, ok := RubAskLine(false)
	if !ok || l.Count != 1 || !l.Rider || len(l.Choices) != 16 || l.Choices[0] != "Games necklace(8)" || l.Choices[8] != "Ring of dueling(8)" {
		t.Fatalf("line = %+v", l)
	}
}

// The held share of a walk is what the casts and rubs freeze, over the walk's ticks, and never more than all of them.
func TestHeldPercent(t *testing.T) {
	for _, c := range []struct {
		casts, rubs, ticks, want int
	}{
		{0, 0, 62, 0},
		{12, 0, 62, 77},   // the lab's alch every 5: 48 of 62 ticks
		{0, 20, 62, 64},   // a rub every 3: 40 of 62
		{12, 1, 62, 80},   // alch and the one rub after the click
		{30, 30, 62, 100}, // more than the walk has
		{3, 0, 0, 0},
	} {
		if got := HeldPercent(c.casts, c.rubs, c.ticks); got != c.want {
			t.Errorf("HeldPercent(%d, %d, %d) = %d, want %d", c.casts, c.rubs, c.ticks, got, c.want)
		}
	}
}

// What a tile costs a running account with nothing holding it, from Player.updateEnergy: 0.335% bare, 0.385% at the
// lab's 10 kg, 0.670% at 64 kg and beyond, never less than bare.
func TestPlainDrainPerTile(t *testing.T) {
	for _, c := range []struct {
		kg   int
		want float64
	}{{0, 0.335}, {10, 0.385}, {32, 0.5}, {64, 0.67}, {200, 0.67}, {-5, 0.335}} {
		if got := PlainDrainPerTile(c.kg); got < c.want-0.001 || got > c.want+0.001 {
			t.Errorf("PlainDrainPerTile(%d) = %.4f, want %.3f", c.kg, got, c.want)
		}
	}
}

// The far clicks a stalling walk aims are within what one click can carry.
func TestWalkAlchClickStepsFitOneClick(t *testing.T) {
	if WalkAlchClickSteps <= 40 || WalkAlchClickSteps > 120 {
		t.Errorf("WalkAlchClickSteps = %d, want more than the walker's 40 and at most the 120 one click can aim", WalkAlchClickSteps)
	}
}

// The staller stands aside under 65 percent of the maximum, so a bite is never refused because a rub or a cast
// delayed the player (live 6 Oct 2026, Shame: 15 refused kebab bites at 5-14 of 25-27, then dead). An unread
// maximum or a not-yet-read 0 never holds it back.
func TestStallHPLow(t *testing.T) {
	for _, c := range []struct {
		hp, max int
		low     bool
	}{{9, 25, true}, {16, 25, true}, {17, 26, false}, {26, 26, false}, {0, 26, false}, {5, 0, false}} {
		if got := StallHPLow(c.hp, c.max); got != c.low {
			t.Errorf("StallHPLow(%d, %d) = %v, want %v", c.hp, c.max, got, c.low)
		}
	}
}
