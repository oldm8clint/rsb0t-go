package retaliateplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func tl(x, z int) tile.Tile { return tile.Tile{X: x, Z: z} }

func TestChooseStandPrefersTheTwoTreeTileAndSkipsABlockedOne(t *testing.T) {
	cands := []StandCand{
		{Tile: tl(3003, 3312), Trees: 2, Walkable: true, Open: 2},
		{Tile: tl(3004, 3312), Trees: 2, Walkable: true, Open: 2},
		{Tile: tl(3002, 3308), Trees: 1, Walkable: true, Open: 3},
	}
	if got, ok := Falador.ChooseStand(cands); !ok || got != tl(3003, 3312) {
		t.Fatalf("got %v %v, want the preferred middle tile", got, ok)
	}
	cands[0].Walkable = false
	if got, ok := Falador.ChooseStand(cands); !ok || got != tl(3004, 3312) {
		t.Fatalf("blocked tile: got %v %v, want the next two-tree tile", got, ok)
	}
	cands[1].Trees = 0
	if got, ok := Falador.ChooseStand(cands); !ok || got != tl(3002, 3308) {
		t.Fatalf("both pair tiles unusable: got %v %v, want the one-tree tile", got, ok)
	}
}

func TestChooseStandRefusesTilesOutsideMultiwayOrTheLeashOrWithoutRoomForTwoRats(t *testing.T) {
	cases := map[string]StandCand{
		"single combat": {Tile: tl(3010, 3312), Trees: 2, Walkable: true, Open: 3},
		"too far north": {Tile: tl(3003, 3326), Trees: 2, Walkable: true, Open: 3},
		"one neighbour": {Tile: tl(3003, 3312), Trees: 2, Walkable: true, Open: 1},
		"no tree":       {Tile: tl(3003, 3312), Trees: 0, Walkable: true, Open: 3},
	}
	for name, c := range cases {
		if got, ok := Falador.ChooseStand([]StandCand{c}); ok {
			t.Errorf("%s: chose %v", name, got)
		}
	}
}

func TestTheDocumentedStandTileIsInsideBothRatLeashes(t *testing.T) {
	s := tl(3003, 3312)
	if !Falador.InMultiway(s) {
		t.Fatal("the stand tile is outside the multi-combat zones")
	}
	for _, r := range Falador.Rats {
		if !Falador.RatReaches(r, s) {
			t.Fatalf("rat spawn %v is beyond the leash of %v", r, s)
		}
	}
	if Falador.InMultiway(tl(3008, 3312)) || Falador.InMultiway(tl(3003, 3303)) || !Falador.InMultiway(tl(2992, 3327)) {
		t.Fatal("the multi-combat rectangle is x 2992-3007, z 3304-3327")
	}
}

func TestNudgeTilesAreDeterministicRotatedAndInTheZone(t *testing.T) {
	stand := tl(3003, 3312)
	ok := func(t tile.Tile) bool { return true }
	a := Falador.NudgeTiles(stand, 0, ok)
	if len(a) == 0 {
		t.Fatal("no nudge tiles")
	}
	for _, n := range a {
		if d := tile.Chebyshev(stand, n); d < 3 || d > 6 || !Falador.InMultiway(n) {
			t.Fatalf("bad nudge tile %v", n)
		}
	}
	b := Falador.NudgeTiles(stand, 1, ok)
	if a[0] == b[0] || len(a) != len(b) {
		t.Fatalf("each try should start somewhere else: %v vs %v", a[0], b[0])
	}
	if c := Falador.NudgeTiles(stand, 0, ok); c[0] != a[0] {
		t.Fatal("not deterministic")
	}
	if Falador.NudgeTiles(stand, 0, func(tile.Tile) bool { return false }) != nil {
		t.Fatal("nothing walkable should give none")
	}
}

func TestPlanGear(t *testing.T) {
	cases := []struct {
		name string
		v    GearView
		step GearStep
		with string
	}{
		{"all set", GearView{Weapon: "Shortbow", AxeHeld: true, Ranged: 1}, GearOK, ""},
		{"quiver first", GearView{Weapon: "Shortbow", Ammo: "Bronze arrow", AxeHeld: true}, GearRemoveQuiver, "Bronze arrow"},
		{"wield the cheapest bow", GearView{Weapon: "Rune axe", PackBows: []string{"Yew shortbow", "Oak shortbow", "Shortbow"}, AxeHeld: true, Ranged: 60}, GearWield, "Shortbow"},
		{"ranged too low", GearView{PackBows: []string{"Oak shortbow"}, AxeHeld: true, Ranged: 1}, GearNoBow, ""},
		{"a longbow is not a shortbow", GearView{Weapon: "Longbow", AxeHeld: true}, GearNoBow, ""},
		{"no axe", GearView{Weapon: "Shortbow"}, GearNoAxe, ""},
	}
	for _, c := range cases {
		step, with := PlanGear(c.v)
		if step != c.step || with != c.with {
			t.Errorf("%s: %v %q, want %v %q", c.name, step, with, c.step, c.with)
		}
	}
	for n, want := range map[string]bool{"Shortbow": true, "OAK SHORTBOW": true, "Shortbow (u)": false, "Longbow": false, "Ogre bow": false, "Magic shortbow": true} {
		if IsShortbow(n) != want {
			t.Errorf("IsShortbow(%q) = %v", n, !want)
		}
	}
	for n, want := range map[string]bool{"Rune axe": true, "Bronze pickaxe": false, "Rune battleaxe": false, "Black axe": true, "Knife": false} {
		if IsChopAxe(n) != want {
			t.Errorf("IsChopAxe(%q) = %v", n, !want)
		}
	}
}

func TestChooseAggroFallsBackAcrossSpellsAndNamesWhatIsMissing(t *testing.T) {
	prefs := []string{"confuse", "weaken", "curse"}
	full := map[string]int{"body rune": 10, "water rune": 30, "earth rune": 30}
	if s, _, ok := ChooseAggro(prefs, 30, full); !ok || s.Name != "confuse" {
		t.Fatalf("%v %v", s, ok)
	}
	// 5 water runes: not enough for two Confuse or Weaken (3 each) but enough for two Curse (2 each)
	thin := map[string]int{"body rune": 2, "water rune": 5, "earth rune": 6}
	s, why, ok := ChooseAggro(prefs, 30, thin)
	if !ok || s.Name != "curse" {
		t.Fatalf("thin runes: %v %v (%s)", s, ok, why)
	}
	if !strings.Contains(why, "confuse needs") || !strings.Contains(why, "weaken needs") {
		t.Fatalf("the reasons for skipping are not logged: %q", why)
	}
	if _, why, ok := ChooseAggro(prefs, 30, map[string]int{"air rune": 100}); ok || !strings.Contains(why, "1 body rune, 2 earth rune, 3 water rune") {
		t.Fatalf("no runes at all: %v %q", ok, why)
	}
	if s, why, ok := ChooseAggro([]string{"weaken", "confuse"}, 5, full); !ok || s.Name != "confuse" || !strings.Contains(why, "needs Magic 11") {
		t.Fatalf("magic 5 skips weaken: %v %v %q", s, ok, why)
	}
	// wind strike only when asked for
	air := map[string]int{"air rune": 4, "mind rune": 4}
	if _, _, ok := ChooseAggro(prefs, 30, air); ok {
		t.Fatal("wind strike used without being listed")
	}
	if s, _, ok := ChooseAggro(append(prefs, "wind strike"), 30, air); !ok || !s.Damages {
		t.Fatalf("listed wind strike: %v %v", s, ok)
	}
	if AggroGap%4 != 2 {
		t.Fatalf("the cast gap %d must be 2 mod 4", AggroGap)
	}
	if AggroGap < 5 {
		t.Fatal("a second cast sooner than clock+5 is refused")
	}
}

func TestHPAbortAndRatsHurt(t *testing.T) {
	if HPAbort(30, 30, 70) || HPAbort(21, 30, 70) {
		t.Fatal("21 of 30 is exactly 70 percent: keep going")
	}
	if !HPAbort(20, 30, 70) {
		t.Fatal("20 of 30 is below 70 percent")
	}
	if HPAbort(0, 0, 70) || HPAbort(-1, 30, 70) {
		t.Fatal("an unknown reading is not a reason to stop")
	}
	if _, hurt := RatsHurt([]NpcView{{Name: "Rat", Health: 2, TotalHealth: 2}, {Name: "Goblin", Health: 1, TotalHealth: 5}}, "Rat"); hurt {
		t.Fatal("a full-health rat and a hurt goblin are not our rats hurt")
	}
	if why, hurt := RatsHurt([]NpcView{{Name: "rat", Health: 1, TotalHealth: 2}}, "Rat"); !hurt || !strings.Contains(why, "1 of 2") {
		t.Fatalf("hurt rat: %v %q", hurt, why)
	}
	if _, hurt := RatsHurt([]NpcView{{Name: "Rat", Health: 0, TotalHealth: 0}}, "Rat"); hurt {
		t.Fatal("a rat that has had no hitmark has TotalHealth 0")
	}
}

func TestWindowLine(t *testing.T) {
	w := Window{Ticks: 300, Logs: 150, XP: 5625, Rolls: 150, Gaps2: 140, Gaps: 149, HitGaps: []int{2, 2, 2}, Clears: 150, Drops: 150, Lead: 1, Pattern: PatternTwoApart, Locked: true}
	if got := w.TicksPerRoll(); got != 2 {
		t.Fatalf("ticks per roll %v", got)
	}
	if got := PerHour(float64(w.Logs), w.Ticks); got != 3000 {
		t.Fatalf("logs per hour %v", got)
	}
	line := w.Line()
	for _, want := range []string{"2.00 ticks per roll", "3000 logs/h", "112500 xp/h", "2 ticks apart", "locked", "lead 1"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q lacks %q", line, want)
		}
	}
	if (Window{}).TicksPerRoll() != 0 || PerHour(5, 0) != 0 {
		t.Fatal("empty window")
	}
	if g := HitGapsOf([]int{1, 3, 5, 9, 11}, 3); len(g) != 3 || g[0] != 2 || g[1] != 4 || g[2] != 2 {
		t.Fatalf("hit gaps %v", g)
	}
}

func TestDropPickerNeverAimsTwiceAtASlotStillInFlight(t *testing.T) {
	p := NewDropPicker()
	logs := []DropCand{{Slot: 3, ID: 1521}, {Slot: 7, ID: 1521}}
	a, ok := p.Pick(logs, 100, 1)
	if !ok || a.Slot != 7 {
		t.Fatalf("first pick %v %v, want the highest slot", a, ok)
	}
	// two ticks later the snapshot still shows slot 7 (the drop is not read yet): the other log must be chosen
	b, ok := p.Pick(logs, 102, 1)
	if !ok || b.Slot != 3 {
		t.Fatalf("second pick %v %v, want slot 3 while 7 is in flight", b, ok)
	}
	if c, ok := p.Pick(logs, 103, 1); ok {
		t.Fatalf("both slots are in flight but %v was offered", c)
	}
	// lead+3 = 4 ticks later slot 7 is free again (and the snapshot would have shown it gone, if it was dropped)
	if c, ok := p.Pick(logs, 104, 1); !ok || c.Slot != 7 {
		t.Fatalf("after the hold: %v %v", c, ok)
	}
	if _, ok := p.Pick(nil, 200, 0); ok {
		t.Fatal("an empty pack offers nothing")
	}
}

func TestSuperviseOrderAndReasons(t *testing.T) {
	ok := Check{HP: 30, MaxHP: 30, StopPct: 70, WeaponOK: true, QuiverEmpty: true}
	if w, _ := Supervise(ok); w != WhyNone {
		t.Fatalf("a healthy loop stopped: %v", w)
	}
	cases := []struct {
		mut  func(c *Check)
		want Why
	}{
		{func(c *Check) { c.Dead = true; c.HP = 1 }, WhyDead},
		{func(c *Check) { c.HP = 10; c.EventBusy = true }, WhyHP},
		{func(c *Check) { c.RatsHurt = "a rat is at 1 of 2 hitpoints"; c.PackFull = true }, WhyRatsHurt},
		{func(c *Check) { c.EventBusy = true; c.WeaponOK = false }, WhyEvent},
		{func(c *Check) { c.WeaponOK = false }, WhyGear},
		{func(c *Check) { c.QuiverEmpty = false }, WhyGear},
		{func(c *Check) { c.PackFull = true }, WhyFull},
	}
	for i, k := range cases {
		c := ok
		k.mut(&c)
		if w, why := Supervise(c); w != k.want || why == "" {
			t.Errorf("case %d: %v %q, want %v", i, w, why, k.want)
		}
	}
}
