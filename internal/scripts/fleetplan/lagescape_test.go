package fleetplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The 18 Oct 15:14 death: an Ogre chieftain (9) and an Ogre (7) on a 53 hitpoint account, a view that had hidden ten ticks and bites six
// seconds late. Sixteen a volley, three volleys in ten ticks: 48, capped at half of 53, 27.
func TestEscapeLineAgainstTheChieftainAndTheOgre(t *testing.T) {
	hits := []int{MaxHitOf("Ogre chieftain"), MaxHitOf("Ogre")}
	if hits[0] != 9 || hits[1] != 7 {
		t.Fatalf("max hits = %v, want 9 and 7 (content's Ogre chieftain and Ogre)", hits)
	}
	cases := []struct {
		name    string
		lag     int
		delay   time.Duration
		horizon int
		line    int
	}{
		{"no lag evidence of a size: two volleys", 0, 0, 8, 27},
		{"a ten-tick view lag", 10, 0, 10, 27},
		{"a six second bite", 0, 6 * time.Second, 10, 27},
		{"a seven second bite over a short lag", 5, 7 * time.Second, 12, 27},
	}
	for _, c := range cases {
		h := EscapeHorizonTicks(c.lag, c.delay)
		if h != c.horizon {
			t.Errorf("%s: horizon = %d, want %d", c.name, h, c.horizon)
		}
		if got := EscapeLine(53, hits, false, h); got != c.line {
			t.Errorf("%s: line = %d, want %d", c.name, got, c.line)
		}
	}
}

// The line is what the hitters can deal, not a share of the hitpoints: a Cow cannot kill a hurt account between two late bites, so it
// stays and eats; an Ogre pair at the same hitpoints cannot be out-eaten.
func TestEscapeLineScalesWithWhatHitsTheAccount(t *testing.T) {
	if got := EscapeLine(55, []int{1}, false, 10); got != 3 {
		t.Errorf("one Cow, ten ticks: line = %d, want 3 (three volleys of 1)", got)
	}
	if got := EscapeLine(55, []int{7}, false, 8); got != 14 {
		t.Errorf("one Ogre, two volleys: line = %d, want 14", got)
	}
	if got := EscapeLine(55, []int{7, 7, 7, 7}, false, 10); got != 28 {
		t.Errorf("four Ogres: line = %d, want the cap, half of 55, 28", got)
	}
	if got := EscapeLine(55, nil, false, 10); got != 0 {
		t.Errorf("nothing hitting it: line = %d, want 0", got)
	}
	if got := EscapeLine(0, []int{7}, false, 10); got != 0 {
		t.Errorf("no maximum: line = %d, want 0", got)
	}
	// An unnamed hitter: the 30% floor over the hit it is assumed to deal.
	if got := EscapeLine(55, []int{8}, true, 8); got != 17 {
		t.Errorf("an unnamed hitter at 55: line = %d, want the 30%% floor, 17 (two volleys of 8 are 16)", got)
	}
	if got := EscapeLine(55, []int{8}, true, 10); got != 24 {
		t.Errorf("an unnamed hitter, three volleys: line = %d, want 24", got)
	}
}

func escapeIncident() EscapeInput {
	return EscapeInput{HP: 14, MaxHP: 53, Known: true, Hits: []int{9, 7}, Threatened: true, LagTicks: 10, BiteLate: true, BiteDelay: 6 * time.Second}
}

// 15:13:30, 14 of 53 with the pair on it and the bites late: leave. And so at 7 and 6.
func TestDecideEscapeLeavesTheIncident(t *testing.T) {
	for _, hp := range []int{14, 7, 6} {
		in := escapeIncident()
		in.HP = hp
		v := DecideEscape(in)
		if !v.Go {
			t.Errorf("hp %d: did not leave: %s", hp, v.Why)
		}
		if v.Line != 27 || v.Horizon != 10 {
			t.Errorf("hp %d: line %d horizon %d, want 27 and 10", hp, v.Line, v.Horizon)
		}
	}
	if v := DecideEscape(escapeIncident()); !strings.Contains(v.Why, "3 volleys") || !strings.Contains(v.Why, "16") {
		t.Errorf("the reason does not name the volleys and the hit: %q", v.Why)
	}
}

// Each of the rule's conditions, one at a time.
func TestDecideEscapeNeedsEveryCondition(t *testing.T) {
	cases := []struct {
		name string
		edit func(*EscapeInput)
	}{
		{"hitpoints over the line", func(in *EscapeInput) { in.HP = 28 }},
		{"over the cap whatever hits it", func(in *EscapeInput) { in.HP = 30; in.Hits = []int{20, 20, 20} }},
		{"nothing is hitting the account", func(in *EscapeInput) { in.Threatened = false }},
		{"nothing can hit it", func(in *EscapeInput) { in.Hits = nil }},
		{"a current view and current bites", func(in *EscapeInput) { in.LagTicks, in.BiteLate, in.ViewLagging, in.BiteDelay = 0, false, false, 0 }},
		{"hitpoints unknown", func(in *EscapeInput) { in.Known = false }},
		{"dead", func(in *EscapeInput) { in.HP = 0 }},
		{"no maximum", func(in *EscapeInput) { in.MaxHP = 0 }},
	}
	for _, c := range cases {
		in := escapeIncident()
		c.edit(&in)
		if v := DecideEscape(in); v.Go {
			t.Errorf("%s: left (%s)", c.name, v.Why)
		}
	}
}

// Any one of the three signs of lateness is enough: the view's recent lag, the view lagging now, or a bite the ledger saw run late.
func TestDecideEscapeTakesAnyLagEvidence(t *testing.T) {
	for name, edit := range map[string]func(*EscapeInput){
		"recent view lag":   func(in *EscapeInput) { in.LagTicks, in.BiteLate, in.BiteDelay = 6, false, 0 },
		"the view lags now": func(in *EscapeInput) { in.LagTicks, in.ViewLagging, in.BiteLate, in.BiteDelay = 0, true, false, 0 },
		"a late bite":       func(in *EscapeInput) { in.LagTicks, in.BiteLate, in.BiteDelay = 0, true, 6*time.Second },
	} {
		in := escapeIncident()
		edit(&in)
		in.HP = 10
		if v := DecideEscape(in); !v.Go {
			t.Errorf("%s: stayed (%s)", name, v.Why)
		}
	}
}

// A single Cow between late bites is not worth a teleport; two Ogres are, at a hitpoint level where the ordinary eat line (half) would
// have had the account eating in place.
func TestDecideEscapeIsForWhatCannotBeOutEaten(t *testing.T) {
	cow := EscapeInput{HP: 14, MaxHP: 55, Known: true, Hits: []int{1}, Threatened: true, LagTicks: 10, BiteLate: true, BiteDelay: 6 * time.Second}
	if v := DecideEscape(cow); v.Go {
		t.Errorf("14 of 55 against one Cow with late bites left: %s", v.Why)
	}
	ogres := cow
	ogres.Hits = []int{7, 7}
	if v := DecideEscape(ogres); !v.Go {
		t.Errorf("14 of 55 against two Ogres with late bites stayed: %s", v.Why)
	}
	// The ordinary eat line against the same two Ogres, on a current view, is 28: an account at 14 eats, and nothing here changes that.
	if line := EatLine(55, ogres.Hits); line != 28 {
		t.Fatalf("eat line = %d, want 28", line)
	}
	ogres.LagTicks, ogres.BiteLate, ogres.BiteDelay = 0, false, 0
	if v := DecideEscape(ogres); v.Go {
		t.Errorf("a current view left: %s", v.Why)
	}
}

// The unnamed hitter (a hit nothing explains) is judged at the floor.
func TestDecideEscapeForAnUnnamedHitter(t *testing.T) {
	in := EscapeInput{HP: 16, MaxHP: 55, Known: true, Hits: []int{8}, Unseen: true, Threatened: true, LagTicks: 4}
	if v := DecideEscape(in); !v.Go || v.Line != 17 {
		t.Errorf("16 of 55, an unnamed hitter, short lag: %+v, want to leave at the 17 floor", v)
	}
	in.HP = 18
	if v := DecideEscape(in); v.Go {
		t.Errorf("18 of 55 left against an unnamed hitter: %s", v.Why)
	}
	if !strings.Contains(DecideEscape(EscapeInput{HP: 5, MaxHP: 55, Known: true, Hits: []int{8}, Unseen: true, Threatened: true, LagTicks: 4}).Why, "unnamed") {
		t.Error("the reason does not say the hitter is unnamed")
	}
}

// The cap is the half line the eaters start from, under the caps they raise it to: leaving is for an account the eaters have not kept
// above half.
func TestEscapeCapIsTheBaseEatLine(t *testing.T) {
	if EscapeCapPercent != EatBasePercent || EscapeCapPercent >= EatCapPercent {
		t.Errorf("EscapeCapPercent = %d, EatBasePercent = %d, EatCapPercent = %d", EscapeCapPercent, EatBasePercent, EatCapPercent)
	}
	if EscapeMinHorizonTicks != 2*4 {
		t.Errorf("EscapeMinHorizonTicks = %d, want two attack gaps of 4", EscapeMinHorizonTicks)
	}
}

// The decider's training line counts everything that can hit the account, not only what faces it (lagescape-1018). 30 of 53 after a
// ten-tick lag with a hunter pair near and none facing: the line is 28 without them (it trains) and 39 with them (it eats first).
func TestTheDeciderEatsBeforeAFightAmongHittersThatDoNotFaceIt(t *testing.T) {
	s := Stage3Snapshot{
		Tile: &tile.Tile{X: TrainStand.X, Z: TrainStand.Z}, HP: 30, MaxHP: 53, EatBelowHP: 6,
		MagicLevel: 60, MagicTarget: 66, HaveStaff: true, TrainCasts: 200, Food: 5, FoodWanted: 8, LagTicks: 10,
	}
	if got := s.TrainEatBelowHP(); got != 28 {
		t.Fatalf("setup: the lag with nothing near raised the line to %d, want 28", got)
	}
	if got := DecideStage3Phase(s); got != Phase3Train {
		t.Fatalf("30 of 53 with nothing near it: %s, want %s", got, Phase3Train)
	}
	s.LagReach = 16
	if got := s.TrainEatBelowHP(); got != 39 {
		t.Errorf("the pair near it: line %d, want 39 (three volleys of 16, capped at 70%% of 53, plus the one)", got)
	}
	if got := DecideStage3Phase(s); got != Phase3Eat {
		t.Errorf("30 of 53 with the pair near it after a ten-tick lag: %s, want %s", got, Phase3Eat)
	}
	// What faces it and what is near it are not added twice: the larger stands.
	s.LagFacing = 16
	if got := s.TrainEatBelowHP(); got != 39 {
		t.Errorf("the same pair facing and near: line %d, want the same 39", got)
	}
	// And with no lag in the memory nothing near it changes the line.
	s.LagTicks = 0
	if got := s.TrainEatBelowHP(); got != 28 {
		t.Errorf("no lag: line %d, want 28", got)
	}
}
