package bankxp

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// findRoute is the test's own lookup so a failure names the missing key rather than panicking
// on an index.
func findRoute(t *testing.T, key string) Route {
	t.Helper()
	for _, r := range Routes {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("no route %q in the generated table; regenerate it from content", key)
	return Route{}
}

// TestTableIsWellFormed guards the shape of the generated table. A malformed row here would
// otherwise surface as a silently wrong number in the panel.
func TestTableIsWellFormed(t *testing.T) {
	if len(Routes) < 100 {
		t.Fatalf("the generated table has only %d routes; that is far short of a real content pack", len(Routes))
	}
	seen := map[string]bool{}
	for _, r := range Routes {
		if seen[r.Key] {
			t.Errorf("duplicate route key %q", r.Key)
		}
		seen[r.Key] = true
		if len(r.Inputs) == 0 {
			t.Errorf("%s: no inputs", r.Key)
			continue
		}
		if r.XP10 <= 0 {
			t.Errorf("%s: XP10 is %d; a route that pays nothing is not a route", r.Key, r.XP10)
		}
		if r.Skill == "" || r.Via == "" || r.Source == "" {
			t.Errorf("%s: missing skill/via/source", r.Key)
		}
		for i, in := range r.Inputs {
			if in.ID <= 0 {
				t.Errorf("%s: input %d has id %d", r.Key, i, in.ID)
			}
			if in.Name == "" {
				t.Errorf("%s: input %d (id %d) has no name", r.Key, i, in.ID)
			}
			if in.Count <= 0 {
				t.Errorf("%s: input %d consumes %d", r.Key, i, in.Count)
			}
		}
	}
}

// TestKnownContentValues pins numbers that were read out of the content checkout by hand during
// this work, with the file and row named. If a regeneration moves one of these, that is a real
// content change and somebody should look at it rather than shrug.
func TestKnownContentValues(t *testing.T) {
	cases := []struct {
		key     string
		skill   string
		inputID int
		xp10    int
		level   int
		// why cites where the number was verified.
		why string
	}{
		// scripts/skill_cooking/configs/cooking_source/cooking_generic.dbrow
		{"cooking_generic_lobster", "Cooking", 377, 1200, 40, "experience=1200, levelrequired=40"},
		{"cooking_generic_shrimp", "Cooking", 317, 300, 1, "experience=300, levelrequired=1"},
		{"cooking_generic_beef", "Cooking", 2132, 300, 1, "experience=300, levelrequired=1"},
		// scripts/skill_prayer/configs/bones.obj: param=bone_exp,45
		{"bury_bones", "Prayer", 526, 45, 1, "bone_exp=45 on [bones]"},
		// scripts/skill_firemaking/configs/firemaking.obj: [logs] productexp=400
		{"burn_logs", "Firemaking", 1511, 400, 1, "productexp=400, levelrequire=1"},
		// scripts/skill_fletching/configs/cut_logs/cut_logs.dbrow: shafts=15, and
		// cut_logs.rs2 pays multiply($shaft_count, 5).
		{"fletching_normal_shafts", "Fletching", 1511, 75, 1, "15 shafts x 5 tenths"},
		// same row: longbow,unstrung_longbow,10,100
		{"fletching_normal_longbow", "Fletching", 1511, 100, 10, "longbow column level 10 exp 100"},
		// scripts/skill_crafting/configs/spinning/spinning.struct: [flax] productexp=150
		{"flax", "Crafting", 1779, 150, 10, "productexp=150, levelrequire=10"},
		// scripts/skill_crafting/configs/gem/gem.dbrow
		{"sapphire_cutting", "Crafting", 1623, 500, 20, "experience=500, level=20"},
		// scripts/skill_smithing/configs/smelting/smelting.struct
		{"smelting_iron_bar", "Smithing", 440, 125, 15, "productexp=125, levelrequired=15"},
		// scripts/skill_runecraft/configs/runecraft.dbrow
		{"runecraft_air", "Runecraft", 1436, 50, 1, "experience=50, level=1"},
	}
	for _, c := range cases {
		r := findRoute(t, c.key)
		if r.Skill != c.skill {
			t.Errorf("%s: skill %q, want %q (%s)", c.key, r.Skill, c.skill, c.why)
		}
		if got := r.Primary().ID; got != c.inputID {
			t.Errorf("%s: primary input id %d, want %d (%s)", c.key, got, c.inputID, c.why)
		}
		if r.XP10 != c.xp10 {
			t.Errorf("%s: XP10 %d, want %d (%s)", c.key, r.XP10, c.xp10, c.why)
		}
		if r.Level != c.level {
			t.Errorf("%s: level %d, want %d (%s)", c.key, r.Level, c.level, c.why)
		}
	}
}

// TestArrowRoutesDoNotConsumeABowString is a regression for a real bug in the first draft of the
// generator: content shares one `fletching_table` between bows, arrows, darts and bolts, and
// keying the secondary-ingredient overlay by table name gave every arrow a bow string and
// multiplied every arrow's product count by the batch cap.
func TestArrowRoutesDoNotConsumeABowString(t *testing.T) {
	arrow := findRoute(t, "fletching_bronze_arrow")
	if len(arrow.Inputs) != 2 || arrow.Inputs[1].Name != "Headless arrow" {
		t.Fatalf("bronze arrow inputs = %+v; arrows.rs2 deletes arrow tips and headless arrows", arrow.Inputs)
	}
	if arrow.Product.Count != 1 {
		t.Errorf("bronze arrow product count = %d; the 15 in content is $max_count, a batch cap", arrow.Product.Count)
	}
	bow := findRoute(t, "stringing_normal_shortbow")
	if len(bow.Inputs) != 2 || bow.Inputs[1].Name != "Bow string" {
		t.Fatalf("shortbow stringing inputs = %+v; bows.rs2 deletes a bow string", bow.Inputs)
	}
}

// TestSuccessChanceMatchesEngineFormula checks the roll against the engine's own arithmetic
// (PlayerOps.ts, ScriptOpcode.STAT_RANDOM), including both clamps.
func TestSuccessChanceMatchesEngineFormula(t *testing.T) {
	lob := findRoute(t, "cooking_generic_lobster")
	if !lob.Roll {
		t.Fatal("the lobster row has successchance in content; Roll should be set")
	}
	engine := func(low, high, level int) float64 {
		v := (low*(99-level))/98 + (high*(level-1))/98 + 1
		if v <= 0 {
			return 0
		}
		if v >= 256 {
			return 1
		}
		return float64(v) / 256
	}
	for _, level := range []int{1, 40, 68, 74, 99} {
		want := engine(lob.SuccessLow, lob.SuccessHigh, level)
		if got := lob.SuccessChance(level); math.Abs(got-want) > 1e-12 {
			t.Errorf("lobster at %d: %v, want %v", level, got, want)
		}
	}
	// A level above 99 clamps to 99, and a level below 1 clamps to 1, as the engine does.
	if lob.SuccessChance(200) != lob.SuccessChance(99) {
		t.Error("levels above 99 should clamp")
	}
	if lob.SuccessChance(0) != lob.SuccessChance(1) {
		t.Error("levels below 1 should clamp")
	}
	// A route content gives no roll always pays.
	if got := findRoute(t, "bury_bones").SuccessChance(1); got != 1 {
		t.Errorf("burying has no roll; chance = %v", got)
	}
}

func TestComputePicksOneRoutePerItemAndNamesIt(t *testing.T) {
	// 100 logs. Content offers three fletching routes and one firemaking route for them.
	have := map[int]int64{1511: 100}
	names := map[int]string{1511: "Logs"}

	best := Compute(have, names, Options{Policy: PolicyBest})
	if len(best.Lines) != 1 {
		t.Fatalf("one banked item should make one line, got %d", len(best.Lines))
	}
	line := best.Lines[0]
	if line.Alternatives < 4 {
		t.Errorf("logs should offer at least 4 routes, table says %d", line.Alternatives)
	}
	// Burning is 40 XP against the longbow's 10; "best" must take it AND say so.
	if line.RouteKey != "burn_logs" {
		t.Errorf("best route for logs = %q, want burn_logs (40 XP beats every fletch)", line.RouteKey)
	}
	if line.XP != 100*40 {
		t.Errorf("100 logs burnt = %d XP, want 4000", line.XP)
	}

	fleet := Compute(have, names, Options{Policy: PolicyFleet})
	if fleet.Lines[0].RouteKey != "fletching_normal_shafts" {
		t.Errorf("fleet route for logs = %q, want the BankFletcher shaft cut", fleet.Lines[0].RouteKey)
	}
	if fleet.Lines[0].XP != 100*75/10 {
		t.Errorf("100 logs into shafts = %d XP, want 750", fleet.Lines[0].XP)
	}
	if fleet.XP >= best.XP {
		t.Error("the fleet's own route should be worth less than the best available one here")
	}
}

func TestComputeLevelGateAndBurnChance(t *testing.T) {
	have := map[int]int64{377: 1000} // Raw lobster
	names := map[int]string{377: "Raw lobster"}

	// No levels at all: aspirational, no burn applied.
	open := Compute(have, names, Options{Policy: PolicyFleet})
	if open.XP != 1000*120 {
		t.Errorf("ungated lobster XP = %d, want 120000", open.XP)
	}

	// Cooking 1 cannot cook a lobster at all (content levelrequired 40).
	low := Compute(have, names, Options{Policy: PolicyFleet, Levels: Levels{"Cooking": 1}})
	if low.XP != 0 {
		t.Errorf("a level-1 cook should bank no lobster XP, got %d", low.XP)
	}
	if low.Lines[0].Blocked != "level" || low.Lines[0].NeedLevel != 40 {
		t.Errorf("blocked line = %+v, want level/40", low.Lines[0])
	}
	if low.LevelBlocked != 1000 {
		t.Errorf("LevelBlocked = %d, want 1000", low.LevelBlocked)
	}

	// At exactly 40 the cook is allowed but burns often, so the gated figure must be well
	// under the ungated one.
	at40 := Compute(have, names, Options{Policy: PolicyFleet, Levels: Levels{"Cooking": 40}})
	if at40.XP <= 0 || at40.XP >= open.XP {
		t.Errorf("lobster at level 40: %d XP, expected something between 0 and %d", at40.XP, open.XP)
	}
	if at40.Lines[0].Chance <= 0 || at40.Lines[0].Chance >= 1 {
		t.Errorf("burn chance at 40 = %v, expected a real probability under 1", at40.Lines[0].Chance)
	}
}

func TestComputeReportsTheLimitingIngredient(t *testing.T) {
	// Steel needs one iron ore and two coal. 100 iron, 10 coal -> 5 steel bars, coal-limited.
	have := map[int]int64{440: 100, 453: 10}
	names := map[int]string{440: "Iron ore", 453: "Coal"}
	p := Compute(have, names, Options{Policy: PolicyBest, Levels: Levels{"Smithing": 99}})
	var iron Line
	for _, l := range p.Lines {
		if l.ItemID == 440 {
			iron = l
		}
	}
	if iron.RouteKey != "smelting_steel_bar" {
		t.Fatalf("iron ore best route = %q, want smelting_steel_bar", iron.RouteKey)
	}
	if iron.Crafts != 5 {
		t.Errorf("crafts = %d, want 5 (10 coal at 2 each)", iron.Crafts)
	}
	if iron.LimitedBy != "Coal" {
		t.Errorf("LimitedBy = %q, want Coal", iron.LimitedBy)
	}
}

func TestComputeCountsUnroutedItems(t *testing.T) {
	// 995 is Coins: valuable, but nothing processes it into experience.
	have := map[int]int64{995: 5000}
	p := Compute(have, map[int]string{995: "Coins"}, Options{Policy: PolicyBest})
	if p.XP != 0 || p.Unrouted != 5000 || p.UnroutedKinds != 1 {
		t.Errorf("coins should be unrouted: %+v", p)
	}
	if p.Lines[0].Blocked != "no-route" {
		t.Errorf("blocked = %q, want no-route", p.Lines[0].Blocked)
	}
}

func TestXPRateScales(t *testing.T) {
	have := map[int]int64{526: 100} // Bones
	names := map[int]string{526: "Bones"}
	one := Compute(have, names, Options{Policy: PolicyFleet, XPRate: 1})
	five := Compute(have, names, Options{Policy: PolicyFleet, XPRate: 5})
	if one.XP != 450 {
		t.Errorf("100 bones = %d XP, want 450", one.XP)
	}
	if five.XP != 5*one.XP {
		t.Errorf("xpRate 5 gave %d, want %d", five.XP, 5*one.XP)
	}
}

// TestGeneratedTableMatchesContent re-runs the generator against a real Content checkout and
// fails if the committed table has drifted. It SKIPS without one, so the suite still passes on a
// machine that has no content, exactly as the collision-pack tests do.
//
//	set RSB0T_CONTENT=<Content checkout>
func TestGeneratedTableMatchesContent(t *testing.T) {
	root := os.Getenv("RSB0T_CONTENT")
	if root == "" {
		t.Skip("set RSB0T_CONTENT to a Content checkout to verify the generated table against it")
	}
	out := filepath.Join(t.TempDir(), "table.go")
	cmd := exec.Command("go", "run", "./gen", "-content", root, "-out", out)
	cmd.Dir = "."
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("regenerating: %v\n%s", err, b)
	}
	fresh, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("table.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(fresh) != string(committed) {
		t.Error("table.go differs from what this content checkout generates; re-run internal/bankxp/gen and commit the result")
	}
}

// TestAnUnreportedSkillIsLevelOneNotZero: a fleet reports a skill only after a stat update for
// it, so a partially-reported account must not have every level-1 route declared impossible.
// One is the game's own floor.
func TestAnUnreportedSkillIsLevelOneNotZero(t *testing.T) {
	// 100 Bones, and an account that has reported Fishing but never Prayer.
	have := map[int]int64{526: 100}
	names := map[int]string{526: "Bones"}
	p := Compute(have, names, Options{Policy: PolicyFleet, Levels: Levels{"Fishing": 40}})
	if p.XP != 450 {
		t.Errorf("burying 100 bones = %d XP, want 450: burying needs Prayer 1, and 1 is the floor", p.XP)
	}
	if p.LevelBlocked != 0 {
		t.Errorf("nothing should be level-blocked here, got %d items", p.LevelBlocked)
	}
	// A route that really does need a level still blocks: cooking a lobster needs 40.
	lob := Compute(map[int]int64{377: 10}, map[int]string{377: "Raw lobster"}, Options{Policy: PolicyFleet, Levels: Levels{"Fishing": 40}})
	if lob.XP != 0 || lob.LevelBlocked != 10 {
		t.Errorf("a level-40 cook route on an unreported Cooking stat should block: %+v", lob)
	}
}
