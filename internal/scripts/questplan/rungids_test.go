package questplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRungIDReadsBothForms(t *testing.T) {
	good := []struct {
		id    string
		skill string
		level int
	}{
		{"train:crafting:19", "crafting", 19},
		{"Train:Mining:20 ", "mining", 20},
		{"agility25", "agility", 25},
		{"smithing34", "smithing", 34},
		{"woodcutting40", "woodcutting", 40},
		{"hitpoints20", "hitpoints", 20},
		{"prayer43", "prayer", 43},
		{"mining2", "mining", 2},
		{"fletching99", "fletching", 99},
		{"runecrafting50", "runecrafting", 50},
		{"runecraft50", "runecraft", 50},
		{"train:runecrafting:50", "runecrafting", 50},
	}
	for _, c := range good {
		skill, level, ok := ParseRungID(c.id)
		if !ok || skill != c.skill || level != c.level {
			t.Errorf("ParseRungID(%q) = %q %d %v, want %q %d", c.id, skill, level, ok, c.skill, c.level)
		}
	}
	// Not rung ids: a quest, a rung whose level is a setting, a name that is not a skill's, a level written another way or
	// outside 2 to 99, a form with a part too many or too few.
	for _, id := range []string{"", "waterfall", "prayerkq", "cows20", "agility", "agility1", "agility0", "agility100", "agility025",
		"agility-5", "agility+25", "agility 25", "train:", "train:crafting", "train:crafting:", "train:crafting:1", "train:crafting:100",
		"train:crafting:019", "train:crafting:20:x", "train:foo:20", "train::20", "trains:crafting:20", "20", "hetty", "mm"} {
		if skill, level, ok := ParseRungID(id); ok {
			t.Errorf("ParseRungID(%q) = %q %d, want not a rung id", id, skill, level)
		}
	}
}

// Every id the ladder has always run, but the two whose levels are not a skill's, is a rung id of its own skill and level, and TrainRungID
// is the other form of the same rung.
func TestEveryLadderRungIsARungIDOfItsOwnLevel(t *testing.T) {
	for id, f := range LadderFacts {
		if f.TrainSkill == "" || id == KQPrayerRungID || id == "cows20" {
			continue
		}
		skill, level, ok := ParseRungID(id)
		if !ok || skill != f.TrainSkill || level != f.TrainLevel {
			t.Errorf("rung %s parses to %q %d %v, but LadderFacts has it at %s %d", id, skill, level, ok, f.TrainSkill, f.TrainLevel)
		}
		if skill, level, ok := ParseRungID(TrainRungID(f.TrainSkill, f.TrainLevel)); !ok || skill != f.TrainSkill || level != f.TrainLevel {
			t.Errorf("rung %s: its generic id %s parses to %q %d %v", id, TrainRungID(f.TrainSkill, f.TrainLevel), skill, level, ok)
		}
	}
}

func TestResolveRungNamedAndGeneric(t *testing.T) {
	c := DefaultCatalogue()
	named := []struct {
		id      string
		trainer string
		skill   string
		level   int
	}{
		{"agility25", "gnome-course", "agility", 25},
		{"agility35", "gnome-course", "agility", 35},
		{"crafting18", "crafting-spin", "crafting", 18},
		{"cows20", "cows-hitpoints", "hitpoints", 20},
		{"attack20", "cows-attack", "attack", 20},
		{"prayer31", "prayer-bones", "prayer", 31},
		{"prayer43", "prayer-bones", "prayer", 43},
		{"smithing34", "anvil-daggers", "smithing", 34},
		{"woodcutting40", "seers-chop", "woodcutting", 40},
		{"fletching25", "seers-fletch", "fletching", 25},
	}
	for _, n := range named {
		res, why := c.ResolveRung(n.id)
		if res.ID != n.id || !res.Named || res.Trainer.ID != n.trainer || res.Skill != n.skill || res.Level != n.level {
			t.Errorf("%s resolves to %+v (%s), want named %s %s %d", n.id, res, why, n.trainer, n.skill, n.level)
		}
	}
	generic := []struct {
		id      string
		trainer string
		skill   string
		level   int
	}{
		// Between two named rungs, and past the last of a row's named rungs but inside its range.
		{"train:crafting:19", "crafting-spin", "crafting", 19},
		{"crafting19", "crafting-spin", "crafting", 19},
		{"train:agility:30", "gnome-course", "agility", 30},
		{"train:smithing:20", "anvil-daggers", "smithing", 20},
		{"train:woodcutting:36", "seers-chop", "woodcutting", 36},
		{"train:fletching:10", "seers-fletch", "fletching", 10},
		{"train:prayer:33", "prayer-bones", "prayer", 33},
		{"train:hitpoints:25", "cows-hitpoints", "hitpoints", 25},
		// The top level of a row is reachable (From < level <= To): the row trains an account at To-1 up to To.
		{"train:agility:35", "gnome-course", "agility", 35},
		{"train:smithing:34", "anvil-daggers", "smithing", 34},
	}
	for _, g := range generic {
		res, why := c.ResolveRung(g.id)
		if res.ID != g.id || res.Named || res.Trainer.ID != g.trainer || res.Skill != g.skill || res.Level != g.level {
			t.Errorf("%s resolves to %+v (%s), want generic %s %s %d", g.id, res, why, g.trainer, g.skill, g.level)
		}
	}
}

func TestResolveRungSaysWhyNot(t *testing.T) {
	c := DefaultCatalogue()
	cases := []struct {
		id   string
		want []string
	}{
		{"waterfall", []string{"not a training rung id"}},
		{"prayerkq", []string{"a setting decides"}},
		// A level past the last row: the gap's lane and reason, the same ones the needs planner reports.
		{"train:agility:36", []string{"no trainer for agility 36", "lane T8"}},
		{"train:smithing:35", []string{"no trainer for smithing 35", "lane T2"}},
		{"train:prayer:46", []string{"lane T7"}},
		// A skill nothing trains yet.
		{"train:mining:20", []string{"no trainer for mining 20", "lane T1"}},
		{"herblore15", []string{"no trainer for herblore 15", "lane T5"}},
		// A skill a loop trains: the needs engine does not stint a loop.
		{"train:magic:50", []string{"loop", "stage5-magic"}},
		{"train:thieving:60", []string{"stage5-thieving"}},
	}
	for _, tc := range cases {
		res, why := c.ResolveRung(tc.id)
		if res.ID != "" {
			t.Errorf("%s resolved to %+v, want no rung", tc.id, res)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(why, w) {
				t.Errorf("%s: reason %q does not mention %q", tc.id, why, w)
			}
		}
	}
}

// A rung id must name the row that works it, so every level a rung row reaches resolves, to a row that reaches it, and the level
// just past the row's top is a NAMED gap or a loop and never a rung id with nothing behind it.
func TestEveryLevelAnyRungRowReachesResolves(t *testing.T) {
	c := DefaultCatalogue()
	for _, tr := range c.Trainers() {
		if tr.Via != ViaRung {
			continue
		}
		for level := tr.From + 1; level <= tr.To; level++ {
			generic := tr.GenericRungID(level)
			res, why := c.ResolveRung(generic)
			if res.ID != generic {
				t.Errorf("%s: %s does not resolve: %s", tr.ID, generic, why)
				continue
			}
			if res.Named || res.Level != level || res.Skill != tr.Skill || !res.Trainer.Covers(level-1) || res.Trainer.Via != ViaRung {
				t.Errorf("%s: %s resolves to %+v", tr.ID, generic, res)
			}
			if named, ok := tr.RungFor(level); ok {
				nres, nwhy := c.ResolveRung(named)
				if nres.ID != named || !nres.Named || nres.Level != level || nres.Trainer.ID != tr.ID {
					t.Errorf("%s: its named rung %s resolves to %+v (%s)", tr.ID, named, nres, nwhy)
				}
			}
		}
		if tr.To >= 99 {
			continue
		}
		past := tr.GenericRungID(tr.To + 1)
		if res, _ := c.ResolveRung(past); res.ID != "" && res.Trainer.ID == tr.ID {
			t.Errorf("%s resolves %s, a level past its top (%d)", tr.ID, past, tr.To)
		} else if res.ID == "" {
			_, why := c.ResolveRung(past)
			if !strings.Contains(why, "lane ") && !strings.Contains(why, "loop") {
				t.Errorf("%s: the level past its top, %s, is refused without a lane or a loop to name: %s", tr.ID, past, why)
			}
		}
	}
}

func TestGateRungIsTheNamedRungElseTheGenericStopTheCatalogueCanBuild(t *testing.T) {
	c := DefaultCatalogue()
	for _, tc := range []struct {
		gate Gate
		id   string
		ok   bool
		why  string
	}{
		{Gate{Skill: "Crafting", Level: 18, Kind: GateItem}, "crafting18", true, ""},
		{Gate{Skill: "crafting", Level: 40, Kind: GateServer}, "crafting40", true, ""},
		// Between the named rungs: the generic stop, to that level.
		{Gate{Skill: "crafting", Level: 19, Kind: GateServer}, "train:crafting:19", true, ""},
		{Gate{Skill: "Smithing", Level: 20, Kind: GateServer}, "train:smithing:20", true, ""},
		{Gate{Skill: "agility", Level: 28, Kind: GateModule}, "train:agility:28", true, ""},
		// No row reaches the level: no rung, and the gap's lane.
		{Gate{Skill: "mining", Level: 20, Kind: GateServer}, "train:mining:20", false, "lane T1"},
		{Gate{Skill: "agility", Level: 50, Kind: GateServer}, "train:agility:50", false, "lane T8"},
	} {
		id, ok, why := c.GateRung(tc.gate)
		if id != tc.id || ok != tc.ok || (tc.why != "" && !strings.Contains(why, tc.why)) || (tc.ok && why != "") {
			t.Errorf("GateRung(%s %d) = %q %v %q, want %q %v %q", tc.gate.Skill, tc.gate.Level, id, ok, why, tc.id, tc.ok, tc.why)
		}
	}
}

// Two rows that reach one level are tried best first, and a loop row is never the rung.
func TestRungRowPicksTheBestRungRowNotALoop(t *testing.T) {
	a, b := row("slow", "mining", 1, 30), row("fast", "mining", 1, 30)
	a.EstXPPerHour, b.EstXPPerHour = 1000, 5000
	loop := loopRow("loop", "mining", 1, 30)
	loop.Rank = -1
	c := NewCatalogue(a, b, loop)
	if got, ok := c.RungRow("mining", 10); !ok || got.ID != "fast" {
		t.Errorf("RungRow = %q %v, want the faster rung row", got.ID, ok)
	}
	if _, ok := c.RungRow("mining", 31); ok {
		t.Error("a level above every row has a rung row")
	}
	if _, ok := c.RungRow("mining", 1); ok {
		t.Error("level 1 needs no training and has a rung row")
	}
	if got, ok := NewCatalogue(loop).RungRow("mining", 10); ok {
		t.Errorf("a loop row is a rung row: %q", got.ID)
	}
}

func TestSharedCatalogueIsTheDefaultOne(t *testing.T) {
	shared, def := SharedCatalogue(), DefaultCatalogue()
	if len(shared.Trainers()) != len(def.Trainers()) || len(shared.Problems()) != 0 {
		t.Errorf("the shared catalogue has %d rows and %d problems, the default %d", len(shared.Trainers()), len(shared.Problems()), len(def.Trainers()))
	}
}

func TestModuleFilesForAGenericRungAreTheRowsOwn(t *testing.T) {
	same := func(a, b []string) bool { return strings.Join(a, "|") == strings.Join(b, "|") }
	// A quest and a named rung are their ModuleFiles entry.
	if got := ModuleFilesFor("waterfall"); !same(got, ModuleFiles["waterfall"]) || len(got) == 0 {
		t.Errorf("waterfall: %v", got)
	}
	if got := ModuleFilesFor("Crafting18"); !same(got, ModuleFiles["crafting18"]) || len(got) == 0 {
		t.Errorf("crafting18: %v", got)
	}
	// A generic rung is the row's: a change to the Cows rung resets every level of it, named or not.
	for _, c := range []struct{ id, as string }{
		{"train:crafting:19", "crafting18"}, {"crafting25", "crafting18"}, {"train:agility:30", "agility25"}, {"train:smithing:20", "smithing34"},
		{"train:woodcutting:36", "woodcutting40"}, {"train:fletching:10", "fletching25"}, {"train:hitpoints:25", "cows20"}, {"train:attack:25", "attack20"},
		{"train:prayer:33", "prayer31"},
	} {
		got := ModuleFilesFor(c.id)
		if len(got) == 0 || !same(got, ModuleFiles[c.as]) {
			t.Errorf("%s: %v, want %s's %v", c.id, got, c.as, ModuleFiles[c.as])
		}
	}
	// Nothing for an id that is no module, and nothing for a level no row reaches.
	for _, id := range []string{"", "nosuchquest", "train:mining:20", "train:agility:50"} {
		if got := ModuleFilesFor(id); got != nil {
			t.Errorf("%q: %v, want none", id, got)
		}
	}
	// A row's own Files come before its rungs'.
	r := row("mining-test", "mining", 1, 30)
	r.Rungs = map[int]string{20: "agility25"}
	if got := r.SourceFiles(); !same(got, ModuleFiles["agility25"]) {
		t.Errorf("a row with a named rung: %v", got)
	}
	r.Files = []string{"internal/scripts/questmods/mining.go"}
	if got := r.SourceFiles(); len(got) != 1 || got[0] != "internal/scripts/questmods/mining.go" {
		t.Errorf("a row with Files: %v", got)
	}
	if got := row("bare", "mining", 1, 30).SourceFiles(); got != nil {
		t.Errorf("a row with neither: %v", got)
	}
}

func TestTrainerFilesMustBeRepoRelativePaths(t *testing.T) {
	r := row("a", "mining", 1, 20)
	r.Files = []string{"internal/scripts/questmods/mining.go", "internal/scripts/questplan/mining.go"}
	if got := r.Validate(); got != "" {
		t.Errorf("valid Files: %q", got)
	}
	for _, f := range []string{"", "  ", `internal\scripts\mining.go`} {
		r.Files = []string{f}
		if got := r.Validate(); !strings.Contains(got, "repo-relative") {
			t.Errorf("Files %q: %q", f, got)
		}
	}
}

// A generic rung's promotion window is its row's source files, so every rung row has some, and they exist: the row a trainer lane adds
// without a named rung must say where its executor is (TrainerSpec.Files), or cmd/questpromote cannot say where the module last changed.
func TestEveryRungRowHasSourceFilesThatExist(t *testing.T) {
	root := repoRoot(t)
	for _, tr := range DefaultCatalogue().Trainers() {
		if tr.Via != ViaRung {
			continue
		}
		files := tr.SourceFiles()
		if len(files) == 0 {
			t.Errorf("trainer %s: no source files (no ModuleFiles entry for any named rung and no Files)", tr.ID)
		}
		for _, f := range files {
			if st, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err != nil || st.IsDir() {
				t.Errorf("trainer %s: %s is not a file (%v)", tr.ID, f, err)
			}
		}
		generic := tr.GenericRungID(tr.From + 1)
		if got := ModuleFilesFor(generic); len(got) == 0 {
			t.Errorf("trainer %s: ModuleFilesFor(%s) is empty", tr.ID, generic)
		}
	}
}

// The pure half of the bot's TestEveryHardGateHasARungThatProducesIt: every hard gate of every rung in LadderFacts (an item gate included, the
// owner's own rule too) resolves to a rung the catalogue works, to the gate's own level. A lane that adds a gate no row reaches fails here, and
// the fix is a trainer row or a named gap (TestEveryHardGateIsCoveredByARowOrANamedGap), never an id with no factory.
func TestEveryHardGateHasARungTheCatalogueResolves(t *testing.T) {
	c := DefaultCatalogue()
	checked := 0
	for id, f := range LadderFacts {
		for _, g := range f.Gates {
			if g.Kind == GateAdvised {
				continue
			}
			checked++
			rung, ok, why := c.GateRung(g)
			if !ok {
				t.Errorf("%s: gate %s %d (%s) inserts %q, which the catalogue cannot work: %s", id, g.Skill, g.Level, g.Kind, rung, why)
				continue
			}
			if res, _ := c.ResolveRung(rung); res.Level != g.Level || res.Skill != strings.ToLower(g.Skill) {
				t.Errorf("%s: gate %s %d resolves to %s %d", id, g.Skill, g.Level, res.Skill, res.Level)
			}
		}
	}
	if checked < 10 {
		t.Errorf("only %d hard gates were checked", checked)
	}
}
