package randomevents

import (
	"strings"
	"testing"
	"time"
)

// sim drives a StrandLadder against a fake clock and a fake account whose position the test moves.
type sim struct {
	l       StrandLadder
	site    StrandSite
	now     time.Time
	x, z    int
	level   int
	hp, max int
	hpKnown bool
	food    bool
	spell   string
	att     bool
	dialog  bool
	out     bool // the account has left every site
}

func newSim(t *testing.T, x, z, level int) *sim {
	t.Helper()
	site, ok := StrandSiteAt(x, z, level)
	if !ok {
		t.Fatalf("%d,%d,%d is in no site", x, z, level)
	}
	return &sim{site: site, now: time.Unix(2_000_000, 0), x: x, z: z, level: level, hp: 30, max: 30, hpKnown: true}
}

// fixedStepsCastle is Rogues' Castle as the catalogue had it before 6 October 2026: three staircases and the
// main door, each a fixed step with its stand tiles. The castle's own entry is an escape plan now (escape,
// strand_escape_test.go); the fixed-step rungs are still what the Wizards' Tower room, the lair's lever and the
// ship decks run, and this synthetic site is the lethal, no-teleport, four-floor shape the rung tests below were
// written against.
func fixedStepsCastle() StrandSite {
	upper := []StrandRect{
		{MinX: 3277, MaxX: 3294, MinZ: 3925, MaxZ: 3940, Level: 1},
		{MinX: 3277, MaxX: 3294, MinZ: 3925, MaxZ: 3940, Level: 2},
		{MinX: 3277, MaxX: 3294, MinZ: 3925, MaxZ: 3940, Level: 3},
	}
	room := StrandRect{MinX: 3279, MaxX: 3282, MinZ: 3934, MaxZ: 3939, Level: 0}
	return StrandSite{
		Name:       "a castle with fixed steps (test)",
		Why:        "the pre-6-October shape of Rogues' Castle's entry",
		Areas:      append(append([]StrandRect(nil), upper...), room),
		NoTeleport: true,
		Lethal:     true,
		Steps: []ExitStep{
			{
				Name: "climb down from the roof", Kind: StepLoc, Where: upper[2],
				Stands: []StrandTile{{X: 3281, Z: 3936, Level: 3}, {X: 3281, Z: 3937, Level: 3}, {X: 3281, Z: 3935, Level: 3}},
				LocID:  1740, LocX: 3280, LocZ: 3936, Op: 1, OpLabel: "Climb-down",
			},
			{
				Name: "climb down from the second floor", Kind: StepLoc, Where: upper[1],
				Stands: []StrandTile{{X: 3282, Z: 3936, Level: 2}, {X: 3279, Z: 3936, Level: 2}, {X: 3282, Z: 3937, Level: 2}},
				LocID:  1739, LocX: 3280, LocZ: 3936, Op: 3, OpLabel: "Climb-down",
			},
			{
				Name: "climb down from the first floor", Kind: StepLoc, Where: upper[0],
				Stands: []StrandTile{{X: 3282, Z: 3936, Level: 1}, {X: 3279, Z: 3936, Level: 1}, {X: 3282, Z: 3937, Level: 1}},
				LocID:  1739, LocX: 3280, LocZ: 3936, Op: 3, OpLabel: "Climb-down",
			},
			{
				Name: "open the ground-floor door", Kind: StepDoor, Where: room,
				Stands: []StrandTile{{X: 3279, Z: 3938, Level: 0}},
				LocID:  1530, LocX: 3279, LocZ: 3939, Op: 1, OpLabel: "Open",
				Beyond: StrandTile{X: 3279, Z: 3940, Level: 0},
			},
		},
		Arrivals: []StrandTile{{X: 3282, Z: 3936, Level: 3}},
	}
}

// newFixedCastleSim is newSim over fixedStepsCastle.
func newFixedCastleSim(t *testing.T, x, z, level int) *sim {
	t.Helper()
	site := fixedStepsCastle()
	if !site.Contains(x, z, level) {
		t.Fatalf("%d,%d,%d is outside the fixed-step castle", x, z, level)
	}
	return &sim{site: site, now: time.Unix(2_000_000, 0), x: x, z: z, level: level, hp: 30, max: 30, hpKnown: true}
}

func (s *sim) poll(dt time.Duration) StrandAction {
	s.now = s.now.Add(dt)
	o := StrandObs{
		Now: s.now, X: s.x, Z: s.z, Level: s.level,
		HP: s.hp, MaxHP: s.max, HPKnown: s.hpKnown, Attacked: s.att, HasFood: s.food, DialogueOpen: s.dialog, Spell: s.spell,
	}
	if !s.out {
		site := s.site
		o.Site = &site
	}
	return s.l.Next(o)
}

func (s *sim) at(x, z, level int) { s.x, s.z, s.level = x, z, level }

// pollUntil polls every dt until the ladder returns a rung other than RungWait or RungNone (or the cap).
func (s *sim) pollUntil(dt time.Duration, max time.Duration) StrandAction {
	var act StrandAction
	for waited := time.Duration(0); waited <= max; waited += dt {
		act = s.poll(dt)
		if act.Rung != RungWait && act.Rung != RungNone {
			return act
		}
	}
	return act
}

// A fixed-step site, top to bottom: walk to the stand, click, judge the level change, next floor ... and the
// ground-floor door opened, walked through, and the ladder forgetting the visit once the account is out.
func TestLadderTakesAFixedStepSiteFloorByFloorAndOutTheDoor(t *testing.T) {
	s := newFixedCastleSim(t, 3282, 3936, 3)

	// A lethal site has no grace: the very first poll acts, and with the account one tile from the
	// stand tile it walks there.
	a := s.poll(0)
	if a.Rung != RungWalk || a.Stand != (StrandTile{X: 3281, Z: 3936, Level: 3}) {
		t.Fatalf("first poll = %+v, want a walk to the roof stand 3281,3936,3", a)
	}
	if !s.l.Active(s.now) {
		t.Fatal("the ladder must be Active (Busy) while it works the visit")
	}
	if a := s.poll(300 * time.Millisecond); a.Rung != RungWait {
		t.Fatalf("a second walk inside strandWalkEvery chose %q, want wait", a.Rung)
	}
	s.at(3281, 3936, 3)
	a = s.pollUntil(300*time.Millisecond, 5*time.Second)
	if a.Rung != RungClick || a.Step == nil || a.Step.LocID != 1740 {
		t.Fatalf("at the roof stand: %+v, want a click on staircase 1740", a)
	}
	// Not landed yet: the ladder waits rather than clicking again.
	if a := s.poll(time.Second); a.Rung != RungWait {
		t.Fatalf("one second after the click chose %q, want wait", a.Rung)
	}
	// The click landed: level 2, one tile east of the stair, which is the next floor's stand.
	s.at(3282, 3936, 2)
	a = s.pollUntil(500*time.Millisecond, 5*time.Second)
	if a.Rung != RungClick || a.Step == nil || a.Step.LocID != 1739 || a.Step.Op != 3 {
		t.Fatalf("second floor: %+v, want Climb-down (option 3) on staircase 1739", a)
	}
	s.at(3282, 3936, 1)
	a = s.pollUntil(500*time.Millisecond, 5*time.Second)
	if a.Rung != RungClick || a.Step == nil || a.Step.LocID != 1739 || a.Step.Op != 3 {
		t.Fatalf("first floor: %+v, want Climb-down (option 3) on staircase 1739", a)
	}
	// Ground floor: the landing tile is in the sealed room; the door's stand is two tiles away.
	s.at(3282, 3936, 0)
	a = s.pollUntil(500*time.Millisecond, 5*time.Second)
	if a.Rung != RungWalk || a.Stand != (StrandTile{X: 3279, Z: 3938, Level: 0}) {
		t.Fatalf("ground floor: %+v, want a walk to the door's stand 3279,3938", a)
	}
	s.at(3279, 3938, 0)
	a = s.pollUntil(500*time.Millisecond, 5*time.Second)
	if a.Rung != RungClick || a.Step == nil || a.Step.Kind != StepDoor || a.Step.LocID != 1530 {
		t.Fatalf("at the door: %+v, want the Open click", a)
	}
	// The door gets strandDoorSettle to open, and then the account walks through it, without waiting
	// for the three-second judging window.
	if a := s.poll(300 * time.Millisecond); a.Rung != RungWait {
		t.Fatalf("300ms after Open chose %q, want wait", a.Rung)
	}
	a = s.poll(strandDoorSettle)
	if a.Rung != RungThrough || a.Stand != (StrandTile{X: 3279, Z: 3940, Level: 0}) {
		t.Fatalf("after the door settled: %+v, want the walk through to 3279,3940", a)
	}
	// Out: no site any more. The ladder forgets the visit and stops being Busy.
	s.at(3279, 3940, 0)
	s.out = true
	if a := s.poll(time.Second); a.Rung != RungNone {
		t.Fatalf("outside the site chose %q, want none", a.Rung)
	}
	if s.l.Active(s.now) || s.l.started {
		t.Fatal("the ladder must reset once the account is out of the site")
	}
}

// A door that never opens: the ladder clicks again, rotates through its stands, and the whole thing is
// bounded.
func TestLadderIsBoundedWhenNothingEverWorks(t *testing.T) {
	s := newFixedCastleSim(t, 3282, 3936, 3)
	s.at(3281, 3936, 3)
	var gaveUp int
	var slept, polls int
	wasInactive := false
	start := s.now
	for polls = 0; polls < 20000 && !s.l.GaveUp(); polls++ {
		a := s.poll(700 * time.Millisecond)
		if a.Rung == RungGiveUp {
			gaveUp++
		}
		if !s.l.Active(s.now) && !s.l.GaveUp() {
			wasInactive = true
			slept++
		}
	}
	if !s.l.GaveUp() || gaveUp != 1 {
		t.Fatalf("after %d polls gaveUp=%d GaveUp=%v, want the ladder to have given up exactly once", polls, gaveUp, s.l.GaveUp())
	}
	if !wasInactive || slept == 0 {
		t.Error("the ladder never slept between rounds: Busy would have held the scripts for the whole visit")
	}
	var sleeps time.Duration
	for _, d := range strandBackoff {
		sleeps += d
	}
	if got := s.now.Sub(start); got < sleeps {
		t.Errorf("gave up after %v, before the %v of backoff it owes", got, sleeps)
	}
	if s.l.Active(s.now) {
		t.Error("a ladder that has given up must not be Busy")
	}
	if a := s.poll(time.Minute); a.Rung != RungNone {
		t.Errorf("after giving up the ladder chose %q, want none", a.Rung)
	}
	if first := s.l.NoteGaveUpLogged(); first {
		t.Error("NoteGaveUpLogged's first answer must be false (not yet logged)")
	}
	if again := s.l.NoteGaveUpLogged(); !again {
		t.Error("NoteGaveUpLogged must say STRANDED once: its second answer is true")
	}
}

// Failed clicks move on to the next stand tile: a loc that answers from one side only is found.
func TestLadderRotatesStandTilesAfterAFailedClick(t *testing.T) {
	s := newFixedCastleSim(t, 3282, 3936, 3)
	s.at(3281, 3936, 3)
	a := s.pollUntil(300*time.Millisecond, 5*time.Second)
	if a.Rung != RungClick {
		t.Fatalf("first action %+v, want a click", a)
	}
	// Nothing happens. Past the judging window the next action is a walk to the SECOND stand.
	a = s.pollUntil(500*time.Millisecond, 10*time.Second)
	if a.Rung != RungWalk || a.Stand != (StrandTile{X: 3281, Z: 3937, Level: 3}) {
		t.Fatalf("after a failed click: %+v, want a walk to the second stand 3281,3937,3", a)
	}
}

func TestLadderEatsOnlyWhereAndWhenItShould(t *testing.T) {
	// The dragon's lair is lethal: eat below 60% with food, from the first poll, spaced.
	s := newSim(t, 2718, 9816, 0)
	s.hp, s.max, s.food = 10, 40, true
	if a := s.poll(0); a.Rung != RungEat {
		t.Fatalf("hurt in the lair: %+v, want eat", a)
	}
	if a := s.poll(500 * time.Millisecond); a.Rung == RungEat {
		t.Fatal("ate again inside strandEatEvery")
	}
	if a := s.poll(2 * time.Second); a.Rung != RungEat {
		t.Fatalf("still hurt two seconds later: %+v, want another bite", a)
	}
	// 75% is not hurt enough; an open dialogue must not be dismissed by eating; no food, no bite.
	for _, c := range []struct {
		name   string
		mutate func(*sim)
	}{
		{"healthy enough", func(s *sim) { s.hp = 30 }},
		{"dialogue open", func(s *sim) { s.dialog = true }},
		{"no food", func(s *sim) { s.food = false }},
		{"hitpoints unknown", func(s *sim) { s.hpKnown = false }},
	} {
		s := newSim(t, 2718, 9816, 0)
		s.hp, s.max, s.food = 10, 40, true
		c.mutate(s)
		if a := s.poll(0); a.Rung == RungEat {
			t.Errorf("%s: ate", c.name)
		}
	}
	// A non-lethal site eats only while attacked.
	p := newSim(t, 3067, 10254, 0)
	p.hp, p.max, p.food = 10, 40, true
	p.poll(0)
	if a := p.poll(strandGrace + time.Second); a.Rung == RungEat {
		t.Error("ate at a non-lethal site with nobody attacking")
	}
	p.att = true
	if a := p.poll(time.Second); a.Rung != RungEat {
		t.Errorf("attacked and hurt in the pocket: %+v, want eat", a)
	}
}

func TestLadderTeleportsBeforeWalkingAndStopsAfterTwoCasts(t *testing.T) {
	s := newSim(t, 2718, 9816, 0)
	s.spell = "lumbridge"
	a := s.poll(0)
	if a.Rung != RungTeleport || a.Spell != "lumbridge" {
		t.Fatalf("first action %+v, want the teleport before any walk", a)
	}
	// In the air: nothing else happens, then it is judged a failure (the account did not move).
	if a := s.poll(time.Second); a.Rung != RungWait {
		t.Fatalf("one second after the cast: %+v, want wait", a)
	}
	// strandTeleportEvery later a second cast; then never a third.
	casts := 1
	for i := 0; i < 400; i++ {
		a = s.poll(time.Second)
		if a.Rung == RungTeleport {
			casts++
		}
	}
	if casts != strandTeleportCasts {
		t.Fatalf("cast %d times, want exactly %d", casts, strandTeleportCasts)
	}
	// And the walk to the lever went on in between.
	s2 := newSim(t, 2718, 9816, 0)
	s2.spell = "lumbridge"
	s2.poll(0)
	s2.at(2717, 9801, 0)
	// A cast that carried the account away is a success and the ladder simply sees no site afterwards.
	s2.out = true
	s2.at(3222, 3218, 0)
	if a := s2.poll(5 * time.Second); a.Rung != RungNone {
		t.Fatalf("after the teleport landed: %+v, want none", a)
	}
}

func TestLadderNeverCastsFromAWildernessSite(t *testing.T) {
	s := newFixedCastleSim(t, 3282, 3936, 3)
	s.spell = "varrock"
	for i := 0; i < 50; i++ {
		if a := s.poll(2 * time.Second); a.Rung == RungTeleport {
			t.Fatal("cast a teleport from Wilderness level 53, which content refuses")
		}
	}
}

func TestLadderClicksTheLeverAtTheStand(t *testing.T) {
	s := newSim(t, 2718, 9816, 0)
	a := s.poll(0)
	if a.Rung != RungWalk || a.Stand != (StrandTile{X: 2717, Z: 9801, Level: 0}) {
		t.Fatalf("lair arrival: %+v, want a walk to the lever's own tile 2717,9801", a)
	}
	s.at(2717, 9801, 0)
	a = s.pollUntil(300*time.Millisecond, 5*time.Second)
	if a.Rung != RungClick || a.Step == nil || a.Step.LocID != 1817 || a.Step.OpLabel != "Pull" {
		t.Fatalf("at the lever: %+v, want Pull on lever 1817", a)
	}
}

func TestStrandBackoffAndRoundsAreBounded(t *testing.T) {
	var total time.Duration
	for _, d := range strandBackoff {
		if d <= 0 {
			t.Fatalf("backoff %v", d)
		}
		total += d
	}
	if total > 30*time.Minute {
		t.Errorf("a ladder visit may spend %v asleep; the scripts' own travel runs in those gaps and an account must not wait longer than half an hour on a ladder", total)
	}
	if strandClicksPerRound < 3 || strandTeleportCasts < 1 || strandTeleportCasts > 3 {
		t.Error("round and cast limits are out of any sensible range")
	}
}

func TestPickTeleport(t *testing.T) {
	law := map[string]int{"Law rune": 3, "Air rune": 30}
	with := func(extra map[string]int) map[string]int {
		out := map[string]int{}
		for k, v := range law {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	ardougne := &StrandTile{X: 2655, Z: 3283}
	for _, c := range []struct {
		name   string
		magic  int
		known  bool
		pack   map[string]int
		worn   []string
		prefer *StrandTile
		want   string
		why    string
	}{
		{"magic unknown", 60, false, with(map[string]int{"Earth rune": 5}), nil, nil, "", "not been reported"},
		{"magic too low", 20, true, with(map[string]int{"Fire rune": 5}), nil, nil, "", "needs Magic 25"},
		{"law runes but no element", 40, true, law, nil, nil, "", "lacks"},
		{"no law rune", 40, true, map[string]int{"Air rune": 9, "Fire rune": 3}, nil, nil, "", "a Law rune"},
		{"varrock only", 26, true, with(map[string]int{"Fire rune": 1}), nil, nil, "varrock", ""},
		{"magic 30 cannot lumbridge", 30, true, with(map[string]int{"Earth rune": 1}), nil, nil, "", "lumbridge needs Magic 31"},
		{"lumbridge by earth rune", 33, true, with(map[string]int{"Earth rune": 1}), nil, nil, "lumbridge", ""},
		{"earth staff pays the earth", 33, true, with(nil), []string{"Staff of earth"}, nil, "lumbridge", ""},
		{"air staff pays the air", 33, true, map[string]int{"Law rune": 1, "Earth rune": 1}, []string{"Staff of air"}, nil, "lumbridge", ""},
		{"falador by water rune", 40, true, with(map[string]int{"Water rune": 2}), nil, nil, "falador", ""},
		{"prefers the landing nearest home", 40, true, with(map[string]int{"Fire rune": 1, "Earth rune": 1, "Water rune": 1}), nil, ardougne, "falador", ""},
		{"no preference takes the first it can", 40, true, with(map[string]int{"Fire rune": 1, "Earth rune": 1, "Water rune": 1}), nil, nil, "varrock", ""},
		{"rune names are case-blind", 40, true, map[string]int{"law RUNE": 1, "AIR rune": 3, "fire Rune": 1}, nil, nil, "varrock", ""},
	} {
		got, why := PickTeleport(c.magic, c.known, c.pack, c.worn, c.prefer)
		if got != c.want {
			t.Errorf("%s: PickTeleport = %q (%s), want %q", c.name, got, why, c.want)
		}
		if c.want == "" && !strings.Contains(why, c.why) {
			t.Errorf("%s: refusal %q does not mention %q", c.name, why, c.why)
		}
	}
}

// Busy semantics: not Active before it has seen a site, Active while it works one, not Active while it
// sleeps between rounds or once it has given up.
func TestLadderActiveReflectsWhetherThereIsAnythingToWaitFor(t *testing.T) {
	var l StrandLadder
	if l.Active(time.Now()) {
		t.Fatal("a fresh ladder is Active")
	}
	s := newFixedCastleSim(t, 3282, 3936, 3)
	s.poll(0)
	if !s.l.Active(s.now) {
		t.Fatal("not Active while working a visit")
	}
	s.l.sleepUntil = s.now.Add(time.Minute)
	if s.l.Active(s.now) {
		t.Fatal("Active while asleep between rounds: the scripts would be held through the sleep")
	}
	if !s.l.Active(s.now.Add(2 * time.Minute)) {
		t.Fatal("not Active again once the sleep is over")
	}
	s.l.gaveUp = true
	if s.l.Active(s.now.Add(time.Hour)) {
		t.Fatal("Active after giving up")
	}
}

// The ladder keeps the account on the polls between its own actions, so a flee or an event handler cannot
// walk it off the tile it is waiting on; it does not keep it while asleep, in grace or with nothing to do.
func TestLadderHoldsTheAccountOnlyWhenItHasSomethingInHand(t *testing.T) {
	s := newFixedCastleSim(t, 3282, 3936, 3)
	if a := s.poll(0); a.Rung != RungWalk {
		t.Fatalf("first poll %+v, want a walk", a)
	}
	if a := s.poll(300 * time.Millisecond); a.Rung != RungWait || !a.Hold {
		t.Fatalf("between walk clicks: %+v, want a held wait", a)
	}

	// A non-lethal site's grace is the script's time, not the ladder's.
	p := newSim(t, 3067, 10254, 0)
	if a := p.poll(0); a.Rung != RungWait || a.Hold {
		t.Fatalf("grace: %+v, want an unheld wait", a)
	}

	// Asleep between rounds: free too.
	s.l.sleepUntil = s.now.Add(time.Minute)
	if a := s.poll(time.Second); a.Rung != RungWait || a.Hold {
		t.Fatalf("asleep: %+v, want an unheld wait", a)
	}
}

// A walk that keeps getting closer is not a stuck walk: the lair's lever is fifteen tiles from the arrival
// tile, and three clicks 2.4 seconds apart would otherwise end a round on an account that is running.
func TestLadderDoesNotCountAWalkThatIsGettingCloserAsFailed(t *testing.T) {
	s := newSim(t, 2718, 9816, 0)
	z := 9816
	for i := 0; i < 6; i++ {
		a := s.pollUntil(300*time.Millisecond, 4*time.Second)
		if a.Rung != RungWalk {
			t.Fatalf("walk %d: %+v, want another walk toward the lever", i+1, a)
		}
		z -= 2
		s.at(2718, z, 0)
	}
	if s.l.Clicks() != 0 || s.l.clicks != 0 {
		t.Fatalf("a walk that closed on the lever six times in a row was counted as %d failures", s.l.Clicks())
	}
}

// The Gnome course's platforms are an ExitFirst site: with a teleport the account can pay for, the ladder still takes the
// account down by the course's own obstacles (the far branch's stand, then its Climb-down), and casts only after that exit has
// failed strandExitFirstClicks times. Without the flag the same account is teleported at once, away from the stronghold.
func TestLadderTakesTheGnomePlatformsDownByTheCourseBeforeAnyTeleport(t *testing.T) {
	s := newSim(t, 2483, 3420, 2)
	s.spell = "Varrock Teleport"
	if !s.site.ExitFirst || s.site.NoTeleport {
		t.Fatalf("the Gnome platforms: ExitFirst %v NoTeleport %v", s.site.ExitFirst, s.site.NoTeleport)
	}
	a := s.pollUntil(500*time.Millisecond, 30*time.Second)
	if a.Rung != RungWalk || a.Stand != (StrandTile{X: 2485, Z: 3419, Level: 2}) {
		t.Fatalf("first action %+v, want the walk to the far branch's stand 2485,3419,2", a)
	}
	s.at(2485, 3419, 2)
	var got []StrandRung
	for len(got) < strandExitFirstClicks+1 {
		a = s.pollUntil(500*time.Millisecond, 30*time.Second)
		got = append(got, a.Rung)
		if a.Rung == RungClick && (a.Step == nil || a.Step.LocID != 2314) {
			t.Fatalf("click on %+v, want the far tree branch 2314", a.Step)
		}
		if a.Rung == RungTeleport {
			break
		}
	}
	want := []StrandRung{RungClick, RungClick, RungClick, RungTeleport}
	if len(got) != len(want) {
		t.Fatalf("actions %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("actions %v, want %v", got, want)
		}
	}

	// Control: the same account at a site that is not ExitFirst is teleported at once.
	c := newSim(t, 2483, 3420, 2)
	c.spell = "Varrock Teleport"
	c.site.ExitFirst = false
	if a := c.pollUntil(500*time.Millisecond, 30*time.Second); a.Rung != RungTeleport {
		t.Fatalf("without ExitFirst: first action %+v, want the teleport", a)
	}
}
