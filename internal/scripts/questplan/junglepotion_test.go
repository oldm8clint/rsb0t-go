package questplan

import (
	"slices"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Jungle Potion (W22, 18 Oct 2026): the decisions over whole simulated quests (a world that answers every step the way the
// content's scripts do, and renders the journal the way its script reads the pack), every state of Decide, the supplies and the
// price of the cave, and the rectangles the island's walks keep out of. The journal parser's own tests are in
// druidjungle_content_test.go beside the pages they are checked against.

var (
	jpIsland = tile.Tile{X: 2809, Z: 3086} // Trufitus's hut
	jpCave   = tile.Tile{X: 2840, Z: 9500}
)

// jpWorld is Jungle Potion as the content runs it: %junglepotion (c) and the scripts' answers to each step. The journal stage the
// snapshot carries is what junglepotion_journal.rs2 would render for the pack, as the parser reads it.
type jpWorld struct {
	*kcWorld
	c int
	// picks counts the searches; blocked[h] makes the herb's loc yield nothing (another account just took it).
	picks   int
	blocked map[int]bool
}

func newJPWorld(t *testing.T, c int, at tile.Tile, inv map[int]int) *jpWorld {
	w := &jpWorld{kcWorld: newKCWorld(t, inv, nil), c: c, blocked: map[int]bool{}}
	w.tile = at
	w.maxHP = 50
	return w
}

// stage is the parser's reading of the page the content would render: get_h reads as 2h+1; found_h as 2h+2 with the clean herb in
// the pack, as the header alone (2) for the first herb's unidentified one, and as the "pick" page (2h+1) otherwise.
func (w *jpWorld) stage() int {
	switch {
	case w.c <= 0:
		return JungleNotStarted
	case w.c >= JungleFoundAllHerbs:
		return w.c
	case w.c%2 == 1:
		return w.c
	}
	h := (w.c - 2) / 2
	herb := JungleHerbs[h]
	switch {
	case w.inv[herb.Clean] > 0:
		return w.c
	case w.inv[herb.Unid] > 0 && h == 0:
		return 2
	}
	return w.c - 1
}

func (w *jpWorld) snapshot() Snapshot {
	j := StatusInProgress
	switch {
	case w.c == JungleNotStarted:
		j = StatusNotStarted
	case w.c >= JungleComplete:
		j = StatusComplete
	}
	s := w.base(j)
	s.Stage, s.StageKnown = w.stage(), true
	s.Levels = map[string]int{"attack": 50, "strength": 50, "defence": 50, "hitpoints": w.maxHP, "magic": 30, "herblore": 3}
	return s
}

func (w *jpWorld) apply(st Step) {
	t := w.t
	t.Helper()
	if why := ValidateStep(st); why != "" {
		t.Fatalf("malformed step %s: %s", st.Describe(), why)
	}
	switch st.Kind {
	case StepProvision:
		key := "provision:" + st.KitName
		if w.provision(st) {
			w.ran[key] = true
			delete(w.failed, key)
		} else {
			w.failed[key] = true
		}
	case StepDeposit:
		w.deposit(st)
	case StepOpHeld:
		for _, h := range JungleHerbs {
			if h.Unid == st.ItemID && w.inv[h.Unid] > 0 {
				w.inv[h.Unid]--
				w.inv[h.Clean]++
			}
		}
	case StepCustom:
		ok := false
		switch st.Custom {
		case CustomJungleStart:
			w.tile = TrufitusAnchor
			if w.c == JungleNotStarted {
				w.c = 1
			}
			ok = true
		case CustomJunglePick:
			herb := JungleHerbs[st.Qty]
			w.tile = herb.Stand
			if !w.blocked[st.Qty] {
				w.picks++
				w.inv[herb.Unid]++
				if w.c == 2*st.Qty+1 { // the pick advances the quest only at its own get_ stage
					w.c++
				}
				ok = true
			}
			if herb.Underground {
				w.tile = JungleCaveSurface // the errand climbs out again
			}
		case CustomJungleHandIn:
			herb := JungleHerbs[st.Qty]
			w.tile = TrufitusAnchor
			if w.c == 2*st.Qty+2 && w.inv[herb.Clean] > 0 {
				w.inv[herb.Clean]--
				w.c++
				if st.Qty == len(JungleHerbs)-1 {
					w.c = JungleFoundAllHerbs
				}
			}
			ok = true
		case CustomJungleFinish:
			w.tile = TrufitusAnchor
			if w.c == JungleFoundAllHerbs {
				w.c = JungleComplete
			}
			ok = true
		default:
			t.Fatalf("a custom step the plan never names: %q", st.Custom)
		}
		w.log = append(w.log, st.Custom)
		if ok {
			delete(w.failed, st.Custom)
		} else {
			w.failed[st.Custom] = true
		}
	default:
		t.Fatalf("a step the quest never asks for: %s", st.Describe())
	}
}

func (w *jpWorld) run(n int) (Step, []string) {
	var seq []string
	for i := 0; i < n; i++ {
		st := DecideJunglePotion(w.snapshot())
		switch st.Kind {
		case StepDone, StepWait:
			return st, seq
		}
		switch st.Kind {
		case StepProvision:
			seq = append(seq, "provision "+st.KitName)
		case StepDeposit:
			seq = append(seq, "deposit")
		case StepOpHeld:
			seq = append(seq, st.Op)
		default:
			name := st.Custom
			if st.Custom == CustomJunglePick || st.Custom == CustomJungleHandIn {
				name += ":" + JungleHerbs[st.Qty].Name
			}
			seq = append(seq, name)
		}
		w.apply(st)
	}
	return Step{}, seq
}

var jpSupplies = map[int]int{CoinsID: 300, 385: 6}

// ---------------------------------------------------------------------------------------------------------------------
// Whole quests
// ---------------------------------------------------------------------------------------------------------------------

// The whole quest from the mainland: the supplies pass (coins for the ships, food) first, then Trufitus, then each herb picked,
// cleaned and handed over in turn, the rogues purse last, and the finishing talk.
func TestJunglePotionRunsToCompletionFromTheMainland(t *testing.T) {
	w := newJPWorld(t, JungleNotStarted, FaladorWestBank, nil)
	w.bank, w.bankKnown = map[int]int{CoinsID: 1000, 385: 20}, true
	st, seq := w.run(80)
	if st.Kind != StepDone {
		t.Fatalf("the run ended on %v (%s) after %v", st.Kind, st.Reason, seq)
	}
	want := []string{"provision jungle-supplies", "jungle-start"}
	for _, h := range JungleHerbs {
		want = append(want, "jungle-pick:"+h.Name, "Identify", "jungle-handin:"+h.Name)
	}
	want = append(want, "jungle-finish")
	if !slices.Equal(seq, want) {
		t.Errorf("steps\n  %s\nwant\n  %s", strings.Join(seq, "\n  "), strings.Join(want, "\n  "))
	}
	if w.picks != 5 {
		t.Errorf("%d searches, want five", w.picks)
	}
	for _, h := range JungleHerbs {
		if w.inv[h.Unid] != 0 || w.inv[h.Clean] != 0 {
			t.Errorf("the %s is still held after the quest", h.Name)
		}
	}
}

// An account already on the island, with its coins and food, goes straight to the work: Karamja has no bank, so no supplies pass is
// asked of it, and it does the quest from wherever it stands.
func TestJunglePotionOnTheIslandNeedsNoSuppliesPass(t *testing.T) {
	w := newJPWorld(t, 5, jpIsland, jpSupplies)
	st, seq := w.run(60)
	if st.Kind != StepDone {
		t.Fatalf("the run ended on %v (%s) after %v", st.Kind, st.Reason, seq)
	}
	for _, step := range seq {
		if strings.HasPrefix(step, "provision") || step == "deposit" {
			t.Errorf("a bank pass on the island: %v", seq)
		}
	}
	if seq[0] != "jungle-pick:Sito foil" || len(seq) != 3*3+1 {
		t.Errorf("steps %v", seq)
	}
}

// The journal says "pick" for a herb that is picked but not yet cleaned (the first herb's page says nothing at all): the pack
// settles it. A carried unidentified herb is cleaned before anything else; a clean one is handed over at its stage.
func TestJunglePotionTheUnidentifiedHerbIsCleanedFirst(t *testing.T) {
	for c := 1; c <= 10; c++ {
		h := (c - 1) / 2
		herb := JungleHerbs[h]
		// The unidentified herb of the current one in the pack: Identify, whatever the page says.
		w := newJPWorld(t, c, jpIsland, map[int]int{CoinsID: 300, 385: 6, herb.Unid: 1})
		if st := DecideJunglePotion(w.snapshot()); st.Kind != StepOpHeld || st.Op != "Identify" || st.ItemID != herb.Unid {
			t.Errorf("stage %d with the unidentified %s: %s", c, herb.Name, st.Describe())
		}
		// The clean one at its found_ stage: hand it over.
		if c%2 == 0 {
			w = newJPWorld(t, c, jpIsland, map[int]int{CoinsID: 300, 385: 6, herb.Clean: 1})
			if st := DecideJunglePotion(w.snapshot()); st.Custom != CustomJungleHandIn || st.Qty != h {
				t.Errorf("stage %d with the clean %s: %s", c, herb.Name, st.Describe())
			}
		}
		// A clean herb at a get_ stage is a stale one (Trufitus refuses it): the herb is picked afresh.
		if c%2 == 1 {
			w = newJPWorld(t, c, jpIsland, map[int]int{CoinsID: 300, 385: 6, herb.Clean: 1})
			if st := DecideJunglePotion(w.snapshot()); st.Custom != CustomJunglePick || st.Qty != h {
				t.Errorf("stage %d with a stale clean %s: %s", c, herb.Name, st.Describe())
			}
		}
		// Nothing in the pack: pick it.
		w = newJPWorld(t, c, jpIsland, map[int]int{CoinsID: 300, 385: 6})
		if st := DecideJunglePotion(w.snapshot()); st.Custom != CustomJunglePick || st.Qty != h {
			t.Errorf("stage %d with nothing in the pack: %s", c, st.Describe())
		}
	}
	// Another herb's unidentified copy in the pack is not this herb's.
	w := newJPWorld(t, 3, jpIsland, map[int]int{CoinsID: 300, 385: 6, JungleHerbs[3].Unid: 1})
	if st := DecideJunglePotion(w.snapshot()); st.Kind == StepOpHeld {
		t.Errorf("a volencia moss cleaned at the ardrigal's stage: %s", st.Describe())
	}
}

// A loc another account has just emptied: the pick is the executor's wait; a pick that failed parks the quest with its reason and
// is not retried at once.
func TestJunglePotionAFailedErrandParksForHalfAnHour(t *testing.T) {
	for _, c := range []struct {
		name   string
		stage  int
		failed string
	}{{"pick", 1, CustomJunglePick}, {"hand-in", 2, CustomJungleHandIn}, {"start", 0, CustomJungleStart}, {"finish", JungleFoundAllHerbs, CustomJungleFinish}} {
		w := newJPWorld(t, c.stage, jpIsland, map[int]int{CoinsID: 300, 385: 6, JungleHerbs[0].Clean: 1})
		w.failed[c.failed] = true
		st := DecideJunglePotion(w.snapshot())
		if st.Kind != StepWait || st.ParkFor != JungleLegPark || !strings.Contains(st.Reason, c.failed) {
			t.Errorf("%s failed: %s (park %v)", c.name, st.Describe(), st.ParkFor)
		}
	}
	// A pack with no room for the herb is not sent to pick it.
	w := newJPWorld(t, 1, jpIsland, jpSupplies)
	s := w.snapshot()
	s.FreeSlots = 0
	if st := DecideJunglePotion(s); st.Kind != StepWait || !strings.Contains(st.Reason, "no room") {
		t.Errorf("a full pack: %s", st.Describe())
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The mainland: supplies and the price of the cave
// ---------------------------------------------------------------------------------------------------------------------

func TestJunglePotionTheSuppliesPassBeforeTheShip(t *testing.T) {
	// No coins and no food: one kit, whose coins line is a flexible one and whose food is optional.
	w := newJPWorld(t, JungleNotStarted, FaladorWestBank, nil)
	st := DecideJunglePotion(w.snapshot())
	if st.Kind != StepProvision || st.KitName != KitJungleSupplies {
		t.Fatalf("an empty pack: %s", st.Describe())
	}
	coins, ok := lineOf(st.Kit, CoinsID)
	if !ok || coins.Qty != JungleCoinsQty || coins.Min != JungleCoinsMin || !coins.Stackable || coins.Optional {
		t.Errorf("the coins line is %+v", coins)
	}
	if coins.Min < 2*30+30 {
		t.Errorf("%d coins is not two ships (30 each) and a margin", coins.Min)
	}
	shark, ok := lineOf(st.Kit, 385)
	if !ok || !shark.Optional || shark.Qty != 6 {
		t.Errorf("the shark line is %+v", shark)
	}
	for _, h := range JungleHerbs {
		for _, id := range []int{h.Unid, h.Clean} {
			if !slices.Contains(st.KeepIDs, id) {
				t.Errorf("the keep list lacks the herb %d", id)
			}
		}
	}
	for _, id := range append([]int{CoinsID}, jungleFoodIDs...) {
		if !slices.Contains(st.KeepIDs, id) {
			t.Errorf("the keep list lacks %d", id)
		}
	}
	// A pack that already has the coins and some food is not sent to a bank.
	if st := DecideJunglePotion(newJPWorld(t, JungleNotStarted, FaladorWestBank, jpSupplies).snapshot()); st.Custom != CustomJungleStart {
		t.Errorf("a stocked pack: %s", st.Describe())
	}
	// After the pass the coins are checked: no coins from the bank or the mule parks the quest for an hour with the reason.
	w = newJPWorld(t, JungleNotStarted, FaladorWestBank, map[int]int{CoinsID: 40, 385: 6})
	w.ran["provision:"+KitJungleSupplies] = true
	st = DecideJunglePotion(w.snapshot())
	if st.Kind != StepWait || st.ParkFor != JungleSupplyPark || !strings.Contains(st.Reason, "coins for the ships") {
		t.Errorf("40 coins after the pass: %s (park %v)", st.Describe(), st.ParkFor)
	}
	// And a failed pass does not stop an account that holds what it needs.
	w = newJPWorld(t, JungleNotStarted, FaladorWestBank, map[int]int{CoinsID: 200})
	w.failed["provision:"+KitJungleSupplies] = true
	if st := DecideJunglePotion(w.snapshot()); st.Kind == StepProvision {
		t.Errorf("a failed pass is asked for again: %s", st.Describe())
	}
	// A pack with no room banks first.
	w = newJPWorld(t, JungleNotStarted, FaladorWestBank, jpSupplies)
	s := w.snapshot()
	s.FreeSlots = 2
	if st := DecideJunglePotion(s); st.Kind != StepDeposit || !slices.Contains(st.KeepIDs, CoinsID) {
		t.Errorf("a nearly full pack: %s", st.Describe())
	}
}

// The cave's price is paid before the account leaves the mainland: a sturdy account with food goes, an account that the model has
// lose to the jogres, or one with no food at a low Defence, parks for hours with the model's reason; on the island it is paid
// again at the cave's mouth, since the food of the departure may have been eaten and there is no bank to draw more from.
func TestJunglePotionPricesTheCaveBeforeTheAccountLeaves(t *testing.T) {
	mk := func(at tile.Tile, inv map[int]int, def, hp int) Snapshot {
		w := newJPWorld(t, 9, at, inv)
		w.maxHP = hp
		s := w.snapshot()
		s.Levels = map[string]int{"attack": def, "strength": def, "defence": def, "hitpoints": hp}
		return s
	}
	for name, c := range map[string]struct {
		s    Snapshot
		want bool
	}{
		"40/40/40, hp 40, six sharks": {mk(FaladorWestBank, jpSupplies, 40, 40), true},
		"50/50/50, hp 53, six sharks": {mk(FaladorWestBank, jpSupplies, 50, 53), true},
		"30/30/30, hp 35, six sharks": {mk(FaladorWestBank, jpSupplies, 30, 35), true},
		"1/1/1, hp 35, one shark":     {mk(FaladorWestBank, map[int]int{CoinsID: 300, 385: 1}, 1, 35), false},
		"10/10/10, hp 35, one shark":  {mk(FaladorWestBank, map[int]int{CoinsID: 300, 385: 1}, 10, 35), false},
	} {
		st := DecideJunglePotion(c.s)
		parked := st.Kind == StepWait && st.ParkFor == JungleOddsPark && strings.Contains(st.Reason, "jogres")
		if parked == c.want {
			t.Errorf("%s: %s (park %v)", name, st.Describe(), st.ParkFor)
		}
	}
	// On the island the price is paid again at the cave's mouth: an account whose food has gone does not go in, and one already
	// inside the cave is not stopped (it goes out by the Hand holds).
	mkAt := func(at tile.Tile, inv map[int]int, def, hp int) Snapshot {
		s := mk(at, inv, def, hp)
		s.Stage = 9
		return s
	}
	hungry := map[int]int{CoinsID: 300}
	if st := DecideJunglePotion(mkAt(jpIsland, hungry, 10, 35)); st.Kind != StepWait || st.ParkFor != JungleOddsPark || !strings.Contains(st.Reason, "cave of the jogres") {
		t.Errorf("on the island with the food gone: %s", st.Describe())
	}
	if st := DecideJunglePotion(mkAt(jpIsland, jpSupplies, 40, 40)); st.Custom != CustomJunglePick || st.Qty != 4 {
		t.Errorf("on the island with the food: %s", st.Describe())
	}
	if st := DecideJunglePotion(mkAt(jpCave, hungry, 10, 35)); st.Custom != CustomJunglePick {
		t.Errorf("already in the cave: %s", st.Describe())
	}
	if st := DecideJunglePotion(mkAt(jpIsland, hungry, 10, 35)); st.Kind == StepCustom {
		t.Errorf("the surface herbs are not priced like the cave: %s", st.Describe())
	}
	// The model: the same account with and without food, and the numbers behind the constants.
	rich := mk(FaladorWestBank, jpSupplies, 40, 40)
	if ok, why := JungleCaveOdds(rich); !ok {
		t.Errorf("40/40/40 with six sharks: %s", why)
	}
	poor := mk(FaladorWestBank, map[int]int{CoinsID: 300}, 10, 35)
	if ok, why := JungleCaveOdds(poor); ok || !strings.Contains(why, "jogres") {
		t.Errorf("10/10/10 with no food: %v %q", ok, why)
	}
	if f := FoeJogre; f.MaxHit() != 7 || f.AttackTicks != 6 || f.PeakHit() != 7 {
		t.Errorf("the jogre hits up to %d every %d ticks", f.MaxHit(), f.AttackTicks)
	}
	unread := poor
	unread.MaxHP = 0
	if ok, _ := JungleCaveOdds(unread); !ok {
		t.Error("an account whose hitpoints are unread is judged")
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The island's hunters
// ---------------------------------------------------------------------------------------------------------------------

// One rectangle for each spider and tribesman, as wide as the ground each wanders over and hunts across, on the ground level.
func TestJungleAvoidFencesEveryHunter(t *testing.T) {
	rects := JungleAvoid()
	hostiles := JungleHostiles()
	if len(rects) != len(hostiles) || len(hostiles) != len(jungleSpiders)+len(jungleTribesmen) {
		t.Fatalf("%d rectangles for %d hostiles", len(rects), len(hostiles))
	}
	for i, h := range hostiles {
		pad := JungleSpiderPad
		if h.NPC == NpcTribesman {
			pad = JungleTribesmanPad
		}
		r := rects[i]
		if r.MinX != h.Tile.X-pad || r.MaxX != h.Tile.X+pad || r.MinZ != h.Tile.Z-pad || r.MaxZ != h.Tile.Z+pad || r.Level != 0 {
			t.Errorf("the rectangle for %d at %v is %+v", h.NPC, h.Tile, r)
		}
		if !r.Contains(h.Tile) {
			t.Errorf("the rectangle for %d at %v does not hold its own spawn", h.NPC, h.Tile)
		}
	}
	// A tribesman's pad is its default wander range of 5 and a hunt range of 1, plus a tile; a spider's its wander of 3.
	if JungleTribesmanPad <= JungleSpiderPad || JungleSpiderPad < 3+1 {
		t.Errorf("pads %d and %d", JungleSpiderPad, JungleTribesmanPad)
	}
	// The herbs and Trufitus stand clear of every rectangle (the planner never steps into a zone from outside): the real-pack test
	// repeats this on the ground the quest walks; here the plan's own tiles.
	for name, at := range map[string]tile.Tile{"Trufitus": TrufitusAnchor, "the Rocks": JunglePotholeStand, "snake weed": JungleHerbs[0].Stand, "ardrigal": JungleHerbs[1].Stand,
		"sito foil": JungleHerbs[2].Stand, "volencia moss": JungleHerbs[3].Stand} {
		for _, r := range rects {
			if r.Contains(at) {
				t.Errorf("%s (%v) is inside the rectangle %+v", name, at, r)
			}
		}
	}
	// Underground and the cave are no part of it.
	for _, r := range rects {
		if r.Level != 0 || r.MinZ > 4000 {
			t.Errorf("a rectangle off the surface: %+v", r)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Every state, and the module
// ---------------------------------------------------------------------------------------------------------------------

func TestJunglePotionEveryStateGivesAWellFormedStep(t *testing.T) {
	known := map[string]bool{CustomJungleStart: true, CustomJunglePick: true, CustomJungleHandIn: true, CustomJungleFinish: true}
	positions := []tile.Tile{FaladorWestBank, DraynorBank, jpIsland, jpCave, JungleHerbs[0].Stand, {X: 2954, Z: 3147}, {}}
	packs := []map[int]int{nil, jpSupplies, {CoinsID: 40}, {CoinsID: 300, JungleHerbs[0].Unid: 1}, {CoinsID: 300, 385: 6, JungleHerbs[2].Clean: 1},
		{CoinsID: 300, 385: 6, JungleHerbs[4].Unid: 1, JungleHerbs[4].Clean: 1}}
	n := 0
	for _, j := range []Status{StatusUnknown, StatusNotStarted, StatusInProgress, StatusComplete} {
		for stage := -1; stage <= 13; stage++ {
			for _, at := range positions {
				for _, inv := range packs {
					for _, mark := range []string{"", "ran", "failed-kit", "failed-errands"} {
						for _, free := range []int{28, 4, 0} {
							w := newJPWorld(t, 0, at, inv)
							s := w.snapshot()
							s.Journal, s.Stage, s.StageKnown, s.FreeSlots = j, stage, true, free
							s.TileKnown = at != (tile.Tile{})
							switch mark {
							case "ran":
								s.Ran["provision:"+KitJungleSupplies] = true
							case "failed-kit":
								s.Failed["provision:"+KitJungleSupplies] = true
							case "failed-errands":
								for c := range known {
									s.Failed[c] = true
								}
							}
							st := DecideJunglePotion(s)
							n++
							if why := ValidateStep(st); why != "" {
								t.Fatalf("journal %s stage %d at %v pack %v mark %q free %d: %s (%s)", j, stage, at, inv, mark, free, why, st.Describe())
							}
							if st.Kind == StepCustom {
								if !known[st.Custom] {
									t.Fatalf("an unregistered custom step %q", st.Custom)
								}
								if st.Custom == CustomJunglePick || st.Custom == CustomJungleHandIn {
									if st.Qty < 0 || st.Qty >= len(JungleHerbs) {
										t.Fatalf("herb %d out of range: %+v", st.Qty, st)
									}
								}
							}
							if st.Kind == StepOpHeld && st.Op != "Identify" {
								t.Fatalf("an op %q", st.Op)
							}
						}
					}
				}
			}
		}
	}
	if st := DecideJunglePotion(Snapshot{}); st.Kind != StepWait {
		t.Errorf("an empty snapshot: %s", st.Describe())
	}
	t.Logf("%d states", n)
}

func TestJunglePotionModule(t *testing.T) {
	m := JunglePotion
	r := m.Record
	if r.ID != "junglepotion" || r.Name != "Jungle Potion" || r.QuestPoints != 1 || QuestTabName("junglepotion") != "Jungle Potion" {
		t.Errorf("record %+v", r)
	}
	if !slices.Equal(r.Requirements.Quests, []string{"druid"}) {
		t.Errorf("requirements %+v: the quest wants Druidic Ritual", r.Requirements)
	}
	skills := map[string]int{}
	for _, s := range r.Requirements.Skills {
		skills[s.Skill] = s.Level
	}
	if skills["herblore"] != 3 || skills["hitpoints"] != JungleMinHitpoints {
		t.Errorf("skill requirements %v", skills)
	}
	f := LadderFacts["junglepotion"]
	if !slices.Equal(f.Prereqs, []string{"druid"}) {
		t.Errorf("ladder prerequisites %v", f.Prereqs)
	}
	if m.MaxDeaths != 1 || !m.NoRetaliate || m.Decide == nil || m.ReadStage == nil || m.Sustain.EatAt < 2*FoeJogre.PeakHit() || len(m.Sustain.FoodIDs) == 0 {
		t.Errorf("module %+v", m)
	}
	for _, h := range JungleHerbs {
		for _, id := range []int{h.Unid, h.Clean} {
			if !slices.Contains(m.ToolIDs, id) {
				t.Errorf("the tools lack %d", id)
			}
		}
	}
	// The herbs are the five Trufitus asks for, in his order, and the loc and the stand of each are consistent.
	if len(JungleHerbs) != 5 || JungleHerbs[0].Name != "Snake weed" || JungleHerbs[4].Name != "Rogues purse" || !JungleHerbs[4].Underground {
		t.Errorf("herbs %+v", JungleHerbs)
	}
	for i, h := range JungleHerbs {
		if h.Underground != (i == 4) || h.Shared == h.Underground || h.Clean != h.Unid+1 {
			t.Errorf("herb %d: %+v", i, h)
		}
		if !h.Underground && tile.Chebyshev(h.At, h.Stand) > 2 {
			t.Errorf("the %s stand %v is far from its loc %v", h.Name, h.Stand, h.At)
		}
	}
	// The cave: the herb's tile, the Hand holds' stand and the landing are in it; the surface is not.
	for _, at := range []tile.Tile{JungleCaveArrival, JungleHandHoldsStand, JungleHerbs[4].Stand} {
		if !InJungleCave(at) {
			t.Errorf("%v is not in the cave", at)
		}
	}
	for _, at := range []tile.Tile{JunglePotholeStand, JungleCaveSurface, TrufitusAnchor, {X: 2830, Z: 9599}, {X: 2840, Z: 9552}} {
		if InJungleCave(at) {
			t.Errorf("%v is in the cave", at)
		}
	}
	if !jungleAway(Snapshot{TileKnown: true, Tile: FaladorWestBank}) || jungleAway(Snapshot{TileKnown: true, Tile: jpIsland}) ||
		jungleAway(Snapshot{TileKnown: true, Tile: jpCave}) || jungleAway(Snapshot{}) {
		t.Error("jungleAway: the mainland is away, the island and the cave are not, an unread position is not")
	}
}
