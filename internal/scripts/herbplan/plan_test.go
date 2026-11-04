package herbplan

import "testing"

func keys(herbs []Herb) []string {
	out := make([]string, 0, len(herbs))
	for _, h := range herbs {
		out = append(out, h.Key)
	}
	return out
}

func sameKeys(got []Herb, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i, k := range keys(got) {
		if k != want[i] {
			return false
		}
	}
	return true
}

func TestDecideStage(t *testing.T) {
	cases := []struct {
		name string
		in   Snapshot
		want Stage
	}{
		{"herbs in the pack get cleaned", Snapshot{EligibleUnidsHeld: 4, TargetsRemaining: 3}, StageClean},
		{"empty pack banks", Snapshot{EligibleUnidsHeld: 0, TargetsRemaining: 3}, StageBank},
		{"no targets left and nothing held is done", Snapshot{EligibleUnidsHeld: 0, TargetsRemaining: 0}, StageDone},
		// The pack is finished before the run ends: a last load must not be abandoned just
		// because the bank is known empty, which is what HerbCleaner.ts's own task order does.
		{"a held load still gets cleaned after the bank runs dry", Snapshot{EligibleUnidsHeld: 2, TargetsRemaining: 0}, StageClean},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DecideStage(c.in); got != c.want {
				t.Errorf("DecideStage(%+v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestEligibleGatesOnLevel(t *testing.T) {
	// Herblore 5 on a members world: the six level-3 herbs plus Marrentill (5), in table order.
	got := Eligible(5, nil, nil, true)
	if !sameKeys(got, []string{"guam", "snake_weed", "ardrigal", "sito_foil", "volencia_moss", "rogues_purse", "marrentill"}) {
		t.Errorf("Eligible(5) = %v, want the level-3 herbs then Marrentill", keys(got))
	}
	// Level 2 is a real, reachable state: content has no Druidic Ritual gate, so an account can
	// sit below every herb's requirement rather than being unable to have Herblore at all.
	if n := len(Eligible(2, nil, nil, true)); n != 0 {
		t.Errorf("Herblore 2 should allow no herb at all, got %d", n)
	}
	if n := len(Eligible(99, nil, nil, true)); n != len(Herbs) {
		t.Errorf("Herblore 99 should allow every herb, got %d of %d", n, len(Herbs))
	}
}

func TestEligibleIsLowestLevelFirst(t *testing.T) {
	got := Eligible(99, nil, nil, true)
	for i := 1; i < len(got); i++ {
		if got[i].Level < got[i-1].Level {
			t.Fatalf("table is not lowest level first: %s (%d) follows %s (%d)",
				got[i].Key, got[i].Level, got[i-1].Key, got[i-1].Level)
		}
	}
}

func TestEligibleRestrictsAndDenies(t *testing.T) {
	got := Eligible(99, []string{"torstol", "Guam leaf"}, nil, true)
	if !sameKeys(got, []string{"guam", "torstol"}) {
		t.Errorf("a selection should resolve ignoring case and stay in table order, got %v", keys(got))
	}
	got = Eligible(99, []string{"Torstol", "Guam leaf"}, map[string]bool{"torstol": true}, true)
	if !sameKeys(got, []string{"guam"}) {
		t.Errorf("a denied herb should drop out, got %v", keys(got))
	}
}

// A free world must leave nothing in the run at all: content refuses the identify before it even
// looks at the level, so every herb is equally impossible there.
func TestEligibleFreeWorldAllowsNothing(t *testing.T) {
	if n := len(Eligible(99, nil, nil, false)); n != 0 {
		t.Errorf("a free world should allow no herb at all, got %d", n)
	}
}

func TestTargetsDropsConfirmedEmpty(t *testing.T) {
	eligible := Eligible(99, []string{"Guam leaf", "Torstol"}, nil, true)
	got := Targets(eligible, map[string]bool{"guam": true})
	if !sameKeys(got, []string{"torstol"}) {
		t.Errorf("Targets should drop a herb confirmed empty in the bank, got %v", keys(got))
	}
	if n := len(Targets(eligible, map[string]bool{"guam": true, "torstol": true})); n != 0 {
		t.Errorf("every herb confirmed empty should leave no targets, got %d", n)
	}
}

// The two id traps from the package doc comment, pinned so a future edit cannot reintroduce
// either. Both were confirmed against the running engine's config archive.
func TestHerbIDsAreDistinctAndNotDerivable(t *testing.T) {
	seen := map[int32]string{}
	for _, h := range Herbs {
		for _, id := range []int32{h.UnidID, h.CleanID} {
			if other, dup := seen[id]; dup {
				t.Errorf("object id %d is claimed by both %s and %s", id, other, h.Key)
			}
			seen[id] = h.Key
		}
	}
	// Trap 2: the "note sits at id+1" heuristic does not generalise. For the Karamja herbs the
	// unid's id+1 is the CLEAN herb, so anything deriving ids by arithmetic corrupts them.
	snake, ok := HerbByName("Snake weed")
	if !ok {
		t.Fatal("Snake weed missing from the table")
	}
	if snake.CleanID != snake.UnidID+1 {
		t.Errorf("Snake weed's clean id should be its unid id + 1 (1525 -> 1526), got %d -> %d", snake.UnidID, snake.CleanID)
	}
	guam, ok := HerbByName("Guam leaf")
	if !ok {
		t.Fatal("Guam leaf missing from the table")
	}
	if guam.CleanID == guam.UnidID+1 {
		t.Error("Guam's id+1 is its NOTE, not its clean form — the table must not imply otherwise")
	}
}

func TestHerbLookups(t *testing.T) {
	h, ok := HerbByUnidID(207)
	if !ok || h.Key != "ranarr" {
		t.Errorf("HerbByUnidID(207) = %v/%v, want ranarr", h.Key, ok)
	}
	h, ok = HerbByCleanID(257)
	if !ok || h.Key != "ranarr" {
		t.Errorf("HerbByCleanID(257) = %v/%v, want ranarr", h.Key, ok)
	}
	// A note id belongs to no herb: 258 is the noted Ranarr weed and must never resolve, or a
	// run would try to Identify a stack it cannot touch.
	if _, ok := HerbByUnidID(258); ok {
		t.Error("the noted Ranarr weed (258) must not resolve as an unidentified herb")
	}
	if h, ok := HerbByName("  ranarr WEED "); !ok || h.Key != "ranarr" {
		t.Errorf("HerbByName should ignore case and surrounding space, got %v/%v", h.Key, ok)
	}
	if _, ok := HerbByName("Banana"); ok {
		t.Error("an unknown name must not resolve")
	}
	if n := len(HerbNames()); n != len(Herbs) {
		t.Errorf("HerbNames() = %d names, want %d", n, len(Herbs))
	}
}

// Every level and XP value here came from content's own herbs.obj params. Spot-check the two
// edges plus the default-level row, so a careless table edit fails loudly.
func TestTableMatchesContent(t *testing.T) {
	cases := []struct {
		name  string
		level int
		xp    int
	}{
		{"Guam leaf", 3, 25}, // no explicit level param in content; takes the default of 3
		{"Marrentill", 5, 38},
		{"Toadflax", 30, 80},
		{"Lantadyme", 67, 131},
		{"Torstol", 75, 150},
		{"Rogues purse", 3, 25},
	}
	for _, c := range cases {
		h, ok := HerbByName(c.name)
		if !ok {
			t.Fatalf("%s missing from the table", c.name)
		}
		if h.Level != c.level || h.XP != c.xp {
			t.Errorf("%s = level %d/xp %d, want level %d/xp %d", c.name, h.Level, h.XP, c.level, c.xp)
		}
	}
}

// XP is content units (tenths), and several herbs are genuinely fractional in displayed points.
// DisplayXP floors, which is why nothing decides anything from an XP delta.
func TestDisplayXPFloorsFractionalHerbs(t *testing.T) {
	guam, _ := HerbByName("Guam leaf")
	if got := guam.DisplayXP(); got != 2 {
		t.Errorf("Guam DisplayXP() = %d, want 2 (25 content units is 2.5 displayed, floored)", got)
	}
	torstol, _ := HerbByName("Torstol")
	if got := torstol.DisplayXP(); got != 15 {
		t.Errorf("Torstol DisplayXP() = %d, want 15", got)
	}
}

func TestChatClassifiers(t *testing.T) {
	if !IsLevelRefusal("You cannot identify this herb.") {
		t.Error("content's own level-refusal line should classify")
	}
	if IsLevelRefusal("This herb is Torstol.") {
		t.Error("a success line must not read as a refusal")
	}
	if !IsMembersRefusal("You need to be on a members' world to gain experience in Herblore.") {
		t.Error("content's own members line should classify")
	}
	// The two refusals must never be confused: only one is fixed by training Herblore.
	if IsMembersRefusal("You cannot identify this herb.") || IsLevelRefusal("You need to be on a members' world to gain experience in Herblore.") {
		t.Error("the level and members refusals must classify disjointly")
	}
	if !IsIdentifyMessage("This herb is a Guam Leaf.") {
		t.Error("the main-herb success shape should classify")
	}
	if !IsIdentifyMessage("You identify the herb. It is Snake Weed.") {
		t.Error("the Karamja success shape should classify")
	}
	if IsIdentifyMessage("You cannot identify this herb.") {
		t.Error("a refusal must not read as a success")
	}
}

func TestSettingsNormalise(t *testing.T) {
	got := Settings{Herbs: []string{"torstol", "Guam leaf", "TORSTOL", "nonsense", " "}, BankBooth: "  "}.Normalised()
	if len(got.Herbs) != 2 || got.Herbs[0] != "Torstol" || got.Herbs[1] != "Guam leaf" {
		t.Errorf("Normalised should de-duplicate, canonicalise spelling and keep input order, got %v", got.Herbs)
	}
	if got.BankBooth != BoothName {
		t.Errorf("a blank bank booth should fall back to %q, got %q", BoothName, got.BankBooth)
	}
	if got.Verbose() {
		t.Error("a blank LogDetail is not verbose")
	}
	if !(Settings{LogDetail: " verbose "}).Verbose() {
		t.Error("Verbose should ignore case and surrounding space")
	}
}

// A typo must be reportable rather than silently widening the run to every herb — the exact
// failure mode the GemCutter port hit through a settings bug.
func TestUnknownHerbs(t *testing.T) {
	got := UnknownHerbs([]string{"Guam leaf", "Banana", "  ", "Torstul"})
	if len(got) != 2 || got[0] != "Banana" || got[1] != "Torstul" {
		t.Errorf("UnknownHerbs = %v, want the two typos only", got)
	}
	if n := len(UnknownHerbs(nil)); n != 0 {
		t.Errorf("no names means no unknowns, got %d", n)
	}
}
