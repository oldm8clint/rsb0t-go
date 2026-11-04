package fleetplan

import (
	"strings"
	"testing"
	"time"
)

func walkAlchOn() WalkAlchSettings { return WalkAlchSettings{Enabled: true} }

// readyPoll is a poll every gate passes: Magic 55, a click two ticks old, no cast yet, a long
// stretch ahead, fire runes, natures and bows in the pack.
func readyPoll() WalkAlchPoll {
	return WalkAlchPoll{Magic: 55, SinceClick: 2 * time.Second, SinceCast: time.Hour, Ahead: 16, Running: true,
		Natures: 10, Fire: 50, Alchables: 10}
}

func TestShouldWalkAlchCastsWhenEveryGatePasses(t *testing.T) {
	if d := ShouldWalkAlch(walkAlchOn(), readyPoll()); !d.Cast {
		t.Fatalf("ready poll refused: %s", d.Why)
	}
}

func TestShouldWalkAlchGates(t *testing.T) {
	cases := []struct {
		name string
		edit func(*WalkAlchPoll)
		set  func(*WalkAlchSettings)
		why  string
	}{
		{"off", nil, func(s *WalkAlchSettings) { s.Enabled = false }, "off"},
		{"Magic 54", func(p *WalkAlchPoll) { p.Magic = 54 }, nil, "Magic below 55"},
		{"a raised minimum", nil, func(s *WalkAlchSettings) { s.MinMagic = 70 }, "Magic below 70"},
		{"a minimum below 55 is clamped", func(p *WalkAlchPoll) { p.Magic = 40 }, func(s *WalkAlchSettings) { s.MinMagic = 30 }, "Magic below 55"},
		{"in combat", func(p *WalkAlchPoll) { p.InCombat = true }, nil, "combat"},
		{"in the Wilderness", func(p *WalkAlchPoll) { p.Wilderness = 3 }, nil, "Wilderness"},
		{"hitpoints at 9 of 25", func(p *WalkAlchPoll) { p.HP, p.MaxHP = 9, 25 }, nil, "hitpoints"},
		{"a modal", func(p *WalkAlchPoll) { p.Modal = true }, nil, "interface"},
		{"a click this tick", func(p *WalkAlchPoll) { p.SinceClick = 0 }, nil, "click"},
		{"a click one tick ago", func(p *WalkAlchPoll) { p.SinceClick = 600 * time.Millisecond }, nil, "click"},
		{"the last cast's delay", func(p *WalkAlchPoll) { p.SinceCast = 2900 * time.Millisecond }, nil, "delay"},
		{"close to the click target running", func(p *WalkAlchPoll) { p.Ahead = 13 }, nil, "click again"},
		{"close to the click target walking", func(p *WalkAlchPoll) { p.Ahead = 8; p.Running = false }, nil, "click again"},
		{"a zone trigger just crossed", func(p *WalkAlchPoll) { p.ZoneTriggerIn = 1 }, nil, "zone trigger"},
		{"a zone trigger 10 tiles ahead running", func(p *WalkAlchPoll) { p.ZoneTriggerIn = 10 }, nil, "zone trigger"},
		{"a zone trigger 6 tiles ahead walking", func(p *WalkAlchPoll) { p.ZoneTriggerIn = 6; p.Running = false; p.Ahead = 9 }, nil, "zone trigger"},
		{"no natures", func(p *WalkAlchPoll) { p.Natures = 0 }, nil, "nature"},
		{"four fire runes", func(p *WalkAlchPoll) { p.Fire = 4 }, nil, "fire"},
		{"fire runes inside the reserve", func(p *WalkAlchPoll) { p.Fire = 54 }, func(s *WalkAlchSettings) { s.FireReserve = 50 }, "fire"},
		{"nothing to alch", func(p *WalkAlchPoll) { p.Alchables = 0 }, nil, "allow-list"},
	}
	for _, c := range cases {
		p, s := readyPoll(), walkAlchOn()
		if c.edit != nil {
			c.edit(&p)
		}
		if c.set != nil {
			c.set(&s)
		}
		d := ShouldWalkAlch(s, p)
		if d.Cast {
			t.Errorf("%s: cast", c.name)
		} else if !strings.Contains(d.Why, c.why) {
			t.Errorf("%s: why = %q, want it to mention %q", c.name, d.Why, c.why)
		}
	}
}

func TestShouldWalkAlchWithAFireStaffNeedsNoFireRunes(t *testing.T) {
	p := readyPoll()
	p.Fire, p.FireStaff = 0, true
	if d := ShouldWalkAlch(walkAlchOn(), p); !d.Cast {
		t.Fatalf("fire staff wielded, no fire runes: refused (%s)", d.Why)
	}
}

func TestShouldWalkAlchCastsExactlyAtTheEdges(t *testing.T) {
	p := readyPoll()
	p.SinceClick, p.SinceCast, p.Ahead = WalkAlchAfterClick, 5*600*time.Millisecond, WalkAlchAhead(true)
	if d := ShouldWalkAlch(walkAlchOn(), p); !d.Cast {
		t.Fatalf("at every edge: refused (%s)", d.Why)
	}
	// A slower engine tick stretches the cast delay with it.
	p.Tick = 700 * time.Millisecond
	if d := ShouldWalkAlch(walkAlchOn(), p); d.Cast {
		t.Error("a 700 ms tick: cast inside the delay")
	}
}

// A zone trigger beyond the cast's delay does not hold the cast back.
func TestShouldWalkAlchCastsBeforeADistantZoneTrigger(t *testing.T) {
	p := readyPoll()
	p.ZoneTriggerIn = WalkAlchTriggerClear(true) + 1
	if d := ShouldWalkAlch(walkAlchOn(), p); !d.Cast {
		t.Fatalf("a trigger 11 tiles ahead running: refused (%s)", d.Why)
	}
	if WalkAlchTriggerClear(true) != 10 || WalkAlchTriggerClear(false) != 6 {
		t.Fatalf("clear = %d running, %d walking; want 10 and 6", WalkAlchTriggerClear(true), WalkAlchTriggerClear(false))
	}
}

func TestWalkAlchAhead(t *testing.T) {
	if got := WalkAlchAhead(true); got != 14 {
		t.Errorf("running = %d, want 14 (re-click 4 + 2 tiles x 5 ticks)", got)
	}
	if got := WalkAlchAhead(false); got != 9 {
		t.Errorf("walking = %d, want 9", got)
	}
}

// TestWalkAlchAllowList: only the allow-list is alched, never a rune, staff, coins or food the
// list names by mistake, best payout first.
func TestWalkAlchAllowList(t *testing.T) {
	held := []AlchCandidate{
		{Name: "Willow shortbow", Count: 5, Value: 60}, // not the owner's stock
		{Name: "Yew shortbow", Count: 3, Value: 240},
		{Name: "Maple longbow", Count: 7, Value: 192},
		{Name: "Lobster", Count: 10, Value: 90},
		{Name: "Nature rune", Count: 100, Value: 1},
		{Name: "Staff of air", Count: 1, Value: 900},
		{Name: "Coins", Count: 500, Value: 1},
	}
	got := walkAlchOn().RankWalkAlchables(held)
	if len(got) != 2 || got[0].Name != "Yew shortbow" || got[1].Name != "Maple longbow" {
		t.Fatalf("default list ranked %+v, want the two listed bows, yew first", got)
	}
	s := walkAlchOn()
	s.Items = []string{"Staff of air", "Nature rune", "Coins", "Lobster", "Pie", "Willow shortbow"}
	held = append(held, AlchCandidate{Name: "Pie", Count: 2, Value: 10, Edible: true})
	if got := s.RankWalkAlchables(held); len(got) != 1 || got[0].Name != "Willow shortbow" {
		t.Errorf("a list naming staffs, runes, coins and food alched %+v; only the bow it also names may be", got)
	}
	s.MinValue = 100
	if got := s.RankWalkAlchables(held); len(got) != 0 {
		t.Errorf("min value 100: %+v", got)
	}
}

func TestWalkAlchAskLines(t *testing.T) {
	s := walkAlchOn()
	in := WalkAlchAskInput{Magic: 55}
	lines := WalkAlchAskLines(s, in)
	if len(lines) != 3 {
		t.Fatalf("empty pack: %+v, want natures, fire and bows", lines)
	}
	if lines[0].Choices[0] != "Nature rune" || lines[0].Count != 120 || !lines[0].FairShare {
		t.Errorf("natures line = %+v", lines[0])
	}
	if lines[1].Choices[0] != "Fire rune" || lines[1].Count != 600 {
		t.Errorf("fire line = %+v", lines[1])
	}
	if lines[2].Choices[0] != DefaultAlchItems[0] || lines[2].Count != 120 || lines[2].FairShare || lines[2].FewestFirst {
		t.Errorf("alchables line = %+v", lines[2])
	}
	// A fire staff: no fire runes asked for.
	in.FireStaff = true
	for _, l := range WalkAlchAskLines(s, in) {
		if l.Choices[0] == "Fire rune" {
			t.Error("fire runes asked for with a fire staff")
		}
	}
	// Holding more than a quarter of each: nothing.
	if got := WalkAlchAskLines(s, WalkAlchAskInput{Magic: 60, Natures: 40, Fire: 200, Alchables: 40}); len(got) != 0 {
		t.Errorf("a quarter-full carry asked for %+v", got)
	}
	// Below 55, or off: nothing.
	if got := WalkAlchAskLines(s, WalkAlchAskInput{Magic: 54}); got != nil {
		t.Errorf("Magic 54 asked for %+v", got)
	}
	if got := WalkAlchAskLines(WalkAlchSettings{}, WalkAlchAskInput{Magic: 99}); got != nil {
		t.Errorf("off asked for %+v", got)
	}
}

// TestWalkAlchAskSkippedWhenTheMuleLacksTheKit: with the mule's published bank holding no bows (or
// no natures) the filtered plan is not worth a walk, even if it would bring fire runes.
func TestWalkAlchAskSkippedWhenTheMuleLacksTheKit(t *testing.T) {
	s := walkAlchOn()
	in := WalkAlchAskInput{Magic: 60}
	lines := WalkAlchAskLines(s, in)
	noBows := func(item string) (int, bool) {
		switch item {
		case "Nature rune":
			return 39000, true
		case "Fire rune":
			return 72000, true
		}
		return 0, true
	}
	plan, _ := FilterAskByBank(lines, noBows)
	if WalkAlchKitWorthAsking(in, plan) {
		t.Errorf("no bows on the mule, none carried: worth asking (%+v)", plan)
	}
	full := func(item string) (int, bool) {
		if item == "Yew shortbow" {
			return 845000, true
		}
		return noBows(item)
	}
	plan, _ = FilterAskByBank(lines, full)
	if !WalkAlchKitWorthAsking(in, plan) {
		t.Errorf("the mule holds the kit: not worth asking (%+v)", plan)
	}
	// Bows already carried, natures on the mule: worth it.
	in.Alchables = 500
	plan, _ = FilterAskByBank(WalkAlchAskLines(s, in), noBows)
	if !WalkAlchKitWorthAsking(in, plan) {
		t.Errorf("bows carried, natures on the mule: not worth asking (%+v)", plan)
	}
}

// The kit the mule is asked for is a trip's worth (Carry), whatever the kit setting says, and the carries never exceed it.
func TestWalkAlchCarryIsATripsWorthAndNeverMoreThanTheKit(t *testing.T) {
	s := walkAlchOn() // Kit 200
	if got := s.CarryCasts(); got != DefaultWalkAlchCarry {
		t.Errorf("CarryCasts = %d, want %d", got, DefaultWalkAlchCarry)
	}
	if got := s.FightCarryCasts(); got != DefaultWalkAlchFightCarry {
		t.Errorf("FightCarryCasts = %d, want %d", got, DefaultWalkAlchFightCarry)
	}
	s.Kit, s.Carry = 80, 500
	if got := s.CarryCasts(); got != 80 {
		t.Errorf("a kit of 80 carried %d", got)
	}
	s.FightCarry = 300
	if got := s.FightCarryCasts(); got != 80 {
		t.Errorf("the pen carry %d exceeds the walk carry", got)
	}
	// An endgame kit of 400 with an endgame carry of 200 asks for 200.
	e := walkAlchOn()
	e.Kit, e.Carry = 400, 200
	lines := WalkAlchAskLines(e, WalkAlchAskInput{Magic: 99})
	if len(lines) != 3 || lines[0].Count != 200 || lines[2].Count != 200 {
		t.Errorf("endgame ask = %+v, want 200 casts' worth", lines)
	}
}

func TestTrainModeCarryKit(t *testing.T) {
	for _, c := range []struct{ kit, carry, want int }{{1000, 0, 300}, {1000, 500, 500}, {100, 300, 100}, {0, 0, 300}} {
		if got := (TrainModeSettings{AlchKit: c.kit, AlchCarry: c.carry}).CarryKit(); got != c.want {
			t.Errorf("kit %d carry %d: %d, want %d", c.kit, c.carry, got, c.want)
		}
	}
}
