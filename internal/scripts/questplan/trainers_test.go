package questplan

import (
	"math"
	"sort"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/levels"
)

// row is a valid rung row for the tests to break one field of.
func row(id, skill string, from, to int) TrainerSpec {
	return TrainerSpec{ID: id, Skill: skill, From: from, To: to, Risk: R0, Via: ViaRung, Family: "f",
		EstXPPerHour: 1000, MinStint: 20 * time.Minute, MaxStint: time.Hour, Source: "test"}
}

func loopRow(id, skill string, from, to int) TrainerSpec {
	return TrainerSpec{ID: id, Skill: skill, From: from, To: to, Risk: R0, Via: ViaLoop, Loop: "some-loop", EstXPPerHour: 1000, Source: "test"}
}

func TestTrainerValidateRejectsEachMalformedRow(t *testing.T) {
	cases := []struct {
		name string
		edit func(*TrainerSpec)
		want string
	}{
		{"empty id", func(r *TrainerSpec) { r.ID = "" }, "lower case"},
		{"upper-case id", func(r *TrainerSpec) { r.ID = "Gnome" }, "lower case"},
		{"space in id", func(r *TrainerSpec) { r.ID = "a b" }, "lower case"},
		{"upper-case skill", func(r *TrainerSpec) { r.Skill = "Mining" }, "lower case"},
		{"no skill", func(r *TrainerSpec) { r.Skill = "" }, "lower case"},
		{"from zero", func(r *TrainerSpec) { r.From = 0 }, "not a range"},
		{"empty range", func(r *TrainerSpec) { r.To = r.From }, "not a range"},
		{"past 99", func(r *TrainerSpec) { r.To = 100 }, "not a range"},
		{"bad risk", func(r *TrainerSpec) { r.Risk = 7 }, "not R0 to R3"},
		{"no rate", func(r *TrainerSpec) { r.EstXPPerHour = 0 }, "no XP an hour"},
		{"no source", func(r *TrainerSpec) { r.Source = "  " }, "no source"},
		{"rung without family", func(r *TrainerSpec) { r.Family = "" }, "rung family"},
		{"rung without stint", func(r *TrainerSpec) { r.MinStint = 0 }, "MinStint"},
		{"stint max below min", func(r *TrainerSpec) { r.MaxStint = time.Minute }, "MinStint"},
		{"named rung outside the range", func(r *TrainerSpec) { r.Rungs = map[int]string{50: "x50"} }, "outside"},
		{"rung with a loop", func(r *TrainerSpec) { r.Loop = "l" }, "no loop"},
		{"duplicate site", func(r *TrainerSpec) {
			r.Sites = []TrainerSite{{Key: "a", Capacity: 1}, {Key: "a", Capacity: 1}}
		}, "unique key"},
		{"site without capacity", func(r *TrainerSpec) { r.Sites = []TrainerSite{{Key: "a"}} }, "positive capacity"},
		{"slot without cap", func(r *TrainerSpec) { r.Slot = "s" }, "slot"},
		{"cap without slot", func(r *TrainerSpec) { r.SlotCap = 2 }, "slot"},
		{"input without ids", func(r *TrainerSpec) { r.Inputs = []TrainerInput{{Name: "x", Qty: 1}} }, "input"},
		{"unknown via", func(r *TrainerSpec) { r.Via = "magic" }, "unknown way"},
	}
	for _, c := range cases {
		r := row("a", "mining", 1, 20)
		c.edit(&r)
		if got := r.Validate(); !strings.Contains(got, c.want) {
			t.Errorf("%s: Validate = %q, want it to mention %q", c.name, got, c.want)
		}
	}
	if got := row("a", "mining", 1, 20).Validate(); got != "" {
		t.Errorf("a valid row: %q", got)
	}
	// A loop row is not stinted, so it carries no rungs, family or stint length.
	l := loopRow("l", "magic", 1, 66)
	if l.Validate() != "" {
		t.Errorf("a valid loop row: %q", l.Validate())
	}
	l.Family = "f"
	if !strings.Contains(l.Validate(), "not stinted") {
		t.Errorf("a loop with a family: %q", l.Validate())
	}
	l = loopRow("l", "magic", 1, 66)
	l.Loop = ""
	if !strings.Contains(l.Validate(), "names its loop") {
		t.Errorf("a loop with no name: %q", l.Validate())
	}
}

func TestCatalogueDropsAndReportsBadRows(t *testing.T) {
	bad := row("bad", "mining", 5, 5)
	dup := row("a", "mining", 1, 10)
	c := NewCatalogue(row("a", "mining", 1, 10), bad, dup, row("b", "smithing", 1, 10))
	if got := len(c.Trainers()); got != 2 {
		t.Errorf("rows kept: %d, want 2", got)
	}
	probs := strings.Join(c.Problems(), "\n")
	if !strings.Contains(probs, "not a range") || !strings.Contains(probs, "registered twice") {
		t.Errorf("problems: %v", c.Problems())
	}
	// Two rows may not claim one rung.
	r1, r2 := row("x", "agility", 1, 30), row("y", "agility", 1, 30)
	r1.Rungs, r2.Rungs = map[int]string{25: "agility25"}, map[int]string{25: "agility25"}
	c = NewCatalogue(r1, r2)
	if len(c.Trainers()) != 1 || !strings.Contains(strings.Join(c.Problems(), ""), "already does") {
		t.Errorf("two rows claiming a rung: %d rows, %v", len(c.Trainers()), c.Problems())
	}
}

// For is best first: the lower rank, then the lower risk, then the faster, then the id.
func TestCatalogueOrdersBestFirst(t *testing.T) {
	slow, fast := row("slow", "crafting", 1, 40), row("fast", "crafting", 1, 40)
	fast.EstXPPerHour = 2000
	risky := row("risky", "crafting", 1, 40)
	risky.Risk, risky.EstXPPerHour = R1, 99999
	ranked := row("ranked", "crafting", 1, 40)
	ranked.Rank, ranked.EstXPPerHour = 3, 99999
	measured := row("measured", "crafting", 1, 40)
	measured.EstXPPerHour, measured.MeasuredXPPerHour = 10, 3000
	c := NewCatalogue(ranked, risky, slow, fast, measured)
	var ids []string
	for _, r := range c.For("Crafting", 10) {
		ids = append(ids, r.ID)
	}
	if got := strings.Join(ids, ","); got != "measured,fast,slow,risky,ranked" {
		t.Errorf("best first = %s", got)
	}
	if r := measured; r.Rate() != 3000 || !r.Measured() {
		t.Errorf("a measurement beats the estimate: %d %v", r.Rate(), r.Measured())
	}
	if len(c.For("crafting", 40)) != 0 || len(c.For("crafting", 0)) != 0 {
		t.Error("a row covers [From, To)")
	}
}

func TestCataloguePathAndCoverage(t *testing.T) {
	a, b := row("a", "mining", 1, 15), row("b", "mining", 15, 30)
	c := NewCatalogue(a, b, row("far", "mining", 40, 50))
	legs, unc := c.Path("mining", 3, 25, nil)
	if unc != 0 || len(legs) != 2 || legs[0].Trainer.ID != "a" || legs[0].From != 3 || legs[0].To != 15 || legs[1].Trainer.ID != "b" || legs[1].To != 25 {
		t.Errorf("path 3 to 25: %+v unc %d", legs, unc)
	}
	legs, unc = c.Path("mining", 3, 35, nil)
	if unc != 30 || len(legs) != 2 {
		t.Errorf("path 3 to 35 runs out at 30: %+v unc %d", legs, unc)
	}
	if c.Covered("mining", 31) || !c.Covered("mining", 30) {
		t.Error("coverage ends where the rows end")
	}
	if got := c.FirstUncovered("mining", 20, 45); got != 30 {
		t.Errorf("first uncovered = %d", got)
	}
	// A pick that returns a row not covering the level cannot make the path advance.
	_, unc = c.Path("mining", 3, 20, func([]TrainerSpec) (TrainerSpec, bool) { return a, true }) // a covers 3..14 only
	if unc != 15 {
		t.Errorf("a pick that stops covering: unc %d", unc)
	}
	if legs, unc := c.Path("mining", 20, 20, nil); len(legs) != 0 || unc != 0 {
		t.Errorf("nothing to train: %+v %d", legs, unc)
	}
}

func TestTrainerHours(t *testing.T) {
	r := row("a", "mining", 1, 30)
	r.EstXPPerHour = 1000
	xp := levels.XPAtLevel(15)
	if got := r.HoursBetween(xp, 20); math.Abs(got-float64(levels.XPAtLevel(20)-xp)/1000) > 1e-9 {
		t.Errorf("hours = %v", got)
	}
	if got := r.HoursBetween(levels.XPAtLevel(20), 20); got != 0 {
		t.Errorf("already there: %v", got)
	}
	if id := r.GenericRungID(25); id != TrainRungID("mining", 25) {
		t.Errorf("generic rung id %q", id)
	}
}

// The catalogue the package registers is sound.
func TestTheDefaultCatalogueHasNoProblems(t *testing.T) {
	c := DefaultCatalogue()
	if p := c.Problems(); len(p) != 0 {
		t.Fatalf("problems: %v", p)
	}
	if len(c.Trainers()) < 15 {
		t.Errorf("only %d rows: a trainers_*.go file stopped registering", len(c.Trainers()))
	}
	// Deterministic: two builds are the same rows in the same order.
	var a, b []string
	for _, r := range c.Trainers() {
		a = append(a, r.ID)
	}
	for _, r := range DefaultCatalogue().Trainers() {
		b = append(b, r.ID)
	}
	if strings.Join(a, ",") != strings.Join(b, ",") {
		t.Error("the catalogue order is not stable")
	}
}

// Every training rung the ladder runs belongs to exactly one row, at the level its facts say it trains to.
func TestEveryTrainingRungBelongsToARow(t *testing.T) {
	c := DefaultCatalogue()
	claimed := map[string]bool{}
	for _, r := range c.Trainers() {
		for level, id := range r.Rungs {
			claimed[id] = true
			f, ok := LadderFacts[id]
			switch {
			case !ok:
				t.Errorf("%s names rung %s, which LadderFacts does not know", r.ID, id)
			case f.TrainSkill != r.Skill || f.TrainLevel != level:
				t.Errorf("%s names %s at %s %d, LadderFacts has it at %s %d", r.ID, id, r.Skill, level, f.TrainSkill, f.TrainLevel)
			}
		}
		for _, id := range r.RungsAtSetting {
			claimed[id] = true
			if f, ok := LadderFacts[id]; !ok || f.TrainSkill != r.Skill {
				t.Errorf("%s names %s as a rung at a setting, and LadderFacts does not train %s with it", r.ID, id, r.Skill)
			}
		}
	}
	for id, f := range LadderFacts {
		if f.TrainSkill != "" && !claimed[id] {
			t.Errorf("training rung %s belongs to no trainer row", id)
		}
	}
	if _, ok := c.RungOwner(KQPrayerRungID); !ok {
		t.Error("the Queen's prayer rung belongs to no row")
	}
}

// A rung's module files are the row's: the trainer's evidence window is the rung's.
func TestEveryTrainerRungHasModuleFiles(t *testing.T) {
	for _, r := range DefaultCatalogue().Trainers() {
		for _, id := range r.RungIDs() {
			if len(ModuleFiles[id]) == 0 {
				t.Errorf("trainer %s: rung %s has no ModuleFiles entry", r.ID, id)
			}
		}
	}
}

// The catalogue's estimates agree with the ladder's own: a rung's EstMinutes is the time the row takes from level 1.
func TestRungRatesAgreeWithTheLadderEstimates(t *testing.T) {
	for _, r := range DefaultCatalogue().Trainers() {
		for level, id := range r.Rungs {
			est := LadderFacts[id].EstMinutes
			if est == 0 {
				continue
			}
			minutes := r.HoursBetween(0, level) * 60
			if minutes < float64(est)*0.75 || minutes > float64(est)*1.25 {
				t.Errorf("%s to %s %d: the catalogue says %.0f minutes from level 1, LadderFacts.EstMinutes says %d", r.ID, r.Skill, level, minutes, est)
			}
		}
	}
}

// Loop rows are never stinted, and outrank the rung rows that would duplicate them.
func TestLoopRowsOutrankTheRungsThatDuplicateThem(t *testing.T) {
	c := DefaultCatalogue()
	for _, skill := range []string{"attack", "hitpoints"} {
		rows := c.For(skill, 15)
		if len(rows) < 2 || rows[0].Via != ViaLoop || rows[1].Via != ViaRung {
			t.Errorf("%s at 15: the Stage 5 loop comes before the cows rung: %v", skill, ids(rows))
		}
	}
	for _, r := range c.Trainers() {
		if r.Via == ViaLoop && (r.MinStint != 0 || len(r.Rungs) != 0) {
			t.Errorf("loop row %s is stinted", r.ID)
		}
	}
}

func ids(rows []TrainerSpec) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	sort.Strings(out)
	return out
}

// Sites and slots have the capacity the planner needs.
func TestEveryRungRowHasSitesWithCapacity(t *testing.T) {
	for _, r := range DefaultCatalogue().Trainers() {
		if r.Via != ViaRung {
			continue
		}
		if len(r.Sites) == 0 {
			t.Errorf("rung row %s has no site: the site board cannot cap it", r.ID)
		}
	}
}
