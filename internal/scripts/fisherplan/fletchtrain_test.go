package fisherplan

import (
	"reflect"
	"testing"

	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/pearlplan"
)

func logsOf(cs []FletchChoice) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.LogsName+" -> "+c.Product.ItemName)
	}
	return out
}

// The goal is the level a pearl is cut at, and the ladder the training reads is content's: the numbers
// the owner's rule rests on (shafts 7.5 xp a log from 1, oak bows 16.5 and 25 from 20 and 25, willow bows
// 33.3 and 41.5 from 35 and 40) are pinned so a regenerated table that changes one fails here.
func TestTheGoalAndTheLadderAreContents(t *testing.T) {
	pearlLevel := pearlplan.Level
	if FletchGoal != pearlLevel {
		t.Fatalf("FletchGoal = %d, want the pearl level %d", FletchGoal, pearlLevel)
	}
	if pearlLevel != 41 {
		t.Fatalf("the pearl level is %d, this test and the owner's ask say 41", pearlLevel)
	}
	want := []struct {
		level int
		logs  string
		name  string
		xp    float64
	}{
		{1, "Logs", "Arrow shaft", 7.5}, {20, "Oak logs", "Oak shortbow", 16.5}, {25, "Oak logs", "Oak longbow", 25},
		{35, "Willow logs", "Willow shortbow", 33.3}, {40, "Willow logs", "Willow longbow", 41.5},
	}
	for _, w := range want {
		best := FletchChoices(w.level)[0]
		if best.LogsName != w.logs || best.Product.ItemName != w.name || best.XPPerLog() != w.xp {
			t.Errorf("at Fletching %d the best cut is %s -> %s at %.1f xp a log, want %s -> %s at %.1f", w.level, best.LogsName, best.Product.ItemName, best.XPPerLog(), w.logs, w.name, w.xp)
		}
	}
}

// What each log can be cut into by level: plain logs are always arrow shafts (never a plain bow), oak and
// willow the best bow the level allows, and a log whose first bow is above the level is not offered.
func TestFletchChoicesFollowTheOwnersRule(t *testing.T) {
	cases := []struct {
		level int
		want  []string
	}{
		{1, []string{"Logs -> Arrow shaft"}},
		{19, []string{"Logs -> Arrow shaft"}},
		{20, []string{"Oak logs -> Oak shortbow", "Logs -> Arrow shaft"}},
		{25, []string{"Oak logs -> Oak longbow", "Logs -> Arrow shaft"}},
		{34, []string{"Oak logs -> Oak longbow", "Logs -> Arrow shaft"}},
		{35, []string{"Willow logs -> Willow shortbow", "Oak logs -> Oak longbow", "Logs -> Arrow shaft"}},
		{39, []string{"Willow logs -> Willow shortbow", "Oak logs -> Oak longbow", "Logs -> Arrow shaft"}},
		{40, []string{"Willow logs -> Willow longbow", "Oak logs -> Oak longbow", "Logs -> Arrow shaft"}},
	}
	for _, c := range cases {
		if got := logsOf(FletchChoices(c.level)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Fletching %d: %v, want %v", c.level, got, c.want)
		}
	}
}

// The best choice the stock can feed: the willow longbow while the bank has willow logs, the oak longbow
// when it does not, shafts when only plain logs are left, nothing with no logs at all.
func TestChooseFletchUsesTheBestStockedLogs(t *testing.T) {
	stock := func(m map[string]int) func(string) int { return func(n string) int { return m[n] } }
	c, ok := ChooseFletch(40, stock(map[string]int{"Logs": 50, "Oak logs": 50, "Willow logs": 50}))
	if !ok || c.Product.ItemName != "Willow longbow" {
		t.Fatalf("level 40 with every log: %+v %v", c, ok)
	}
	c, ok = ChooseFletch(40, stock(map[string]int{"Logs": 50, "Oak logs": 50}))
	if !ok || c.Product.ItemName != "Oak longbow" {
		t.Fatalf("level 40 with no willow: %+v %v", c, ok)
	}
	c, ok = ChooseFletch(40, stock(map[string]int{"Logs": 50}))
	if !ok || c.Product.ItemName != "Arrow shaft" {
		t.Fatalf("level 40 with only plain logs: %+v %v", c, ok)
	}
	if _, ok := ChooseFletch(10, stock(map[string]int{"Oak logs": 99, "Willow logs": 99})); ok {
		t.Fatal("level 10 with only oak and willow logs, which it cannot cut, chose something")
	}
	if _, ok := ChooseFletch(10, stock(nil)); ok {
		t.Fatal("no logs chose something")
	}
}

// simulate cuts one log at a time the way the training does (the best choice at the current level, with
// the stock unlimited) and counts the logs of each kind used until level 41.
func simulate(xp int) (map[string]int, int) {
	used := map[string]int{}
	xp10 := xp * 10
	for levels.LevelFromXP(xp10/10) < FletchGoal {
		c := FletchChoices(levels.LevelFromXP(xp10 / 10))[0]
		xp10 += c.Product.XP10
		used[c.LogsName]++
	}
	return used, xp10 / 10
}

// FletchNeeds from nothing matches an independent log-by-log simulation to within a log or two per level
// (it steps a level at a time and rounds each step up), and the totals are the climb the file comment
// states: about 600 plain logs, 800 oak and 540 willow (607, 794 and 544).
func TestFletchNeedsFromNothingMatchesALogByLogClimb(t *testing.T) {
	needs := FletchNeeds(0)
	used, _ := simulate(0)
	for _, name := range []string{"Logs", "Oak logs", "Willow logs"} {
		if d := needs[name] - used[name]; d < 0 || d > 25 {
			t.Errorf("%s: FletchNeeds %d against a simulated %d (the plan may overshoot by a log a level, never undershoot)", name, needs[name], used[name])
		}
	}
	if needs["Logs"] < 590 || needs["Logs"] > 640 || needs["Oak logs"] < 780 || needs["Oak logs"] > 830 || needs["Willow logs"] < 530 || needs["Willow logs"] > 570 {
		t.Fatalf("the climb from nothing needs %v, want about 607 plain, 794 oak and 544 willow", needs)
	}
	if len(needs) != 3 {
		t.Fatalf("needs %v: want exactly the three kinds", needs)
	}
	// And cutting what it says gets there: simulate the account using exactly those logs in order.
	xp10 := 0 // content stores experience in tenths
	remaining := map[string]int{}
	for k, v := range needs {
		remaining[k] = v
	}
	for levels.LevelFromXP(xp10/10) < FletchGoal {
		c, ok := ChooseFletch(levels.LevelFromXP(xp10/10), func(n string) int { return remaining[n] })
		if !ok {
			t.Fatalf("ran out of logs at %d xp (Fletching %d) with %v left: the plan is short", xp10/10, levels.LevelFromXP(xp10/10), remaining)
		}
		remaining[c.LogsName]--
		xp10 += c.Product.XP10
	}
}

// An account part-way up needs only the rest, in the logs its level cuts: nothing plain at 30, no oak at 36.
func TestFletchNeedsFromHalfwayAndAtTheGoal(t *testing.T) {
	needs := FletchNeeds(levels.XPAtLevel(30))
	if needs["Logs"] != 0 || needs["Oak logs"] == 0 || needs["Willow logs"] == 0 {
		t.Fatalf("from Fletching 30: %v, want oak and willow only", needs)
	}
	needs = FletchNeeds(levels.XPAtLevel(36))
	if needs["Logs"] != 0 || needs["Oak logs"] != 0 || needs["Willow logs"] == 0 {
		t.Fatalf("from Fletching 36: %v, want willow only", needs)
	}
	if got := FletchNeeds(levels.XPAtLevel(41)); len(got) != 0 {
		t.Fatalf("at the goal: %v, want nothing", got)
	}
	if got := FletchNeeds(levels.XPAtLevel(60)); len(got) != 0 {
		t.Fatalf("past the goal: %v, want nothing", got)
	}
	if FletchDone(40) || !FletchDone(41) || !FletchDone(70) {
		t.Fatal("FletchDone is not level 41 and up")
	}
}

// The ask is the need less what the account holds, with a margin, and nothing for a kind it has enough of.
func TestFletchAskSubtractsWhatIsHeldAndAddsAMargin(t *testing.T) {
	none := func(string) int { return 0 }
	need := FletchNeeds(0)
	ask := FletchAsk(0, none)
	for name, n := range need {
		want := n + n*3/100 + 10
		if ask[name] != want {
			t.Errorf("%s: asks %d, want %d (the need %d, 3%% and 10 over)", name, ask[name], want, n)
		}
	}
	held := func(name string) int {
		if name == "Willow logs" {
			return 5000
		}
		return 100
	}
	ask = FletchAsk(0, held)
	if _, asked := ask["Willow logs"]; asked {
		t.Errorf("asked for willow logs with 5000 held: %v", ask)
	}
	if ask["Logs"] != need["Logs"]+need["Logs"]*3/100+10-100 {
		t.Errorf("plain logs: asks %d with 100 held", ask["Logs"])
	}
	if got := FletchAsk(levels.XPAtLevel(41), none); len(got) != 0 {
		t.Errorf("asks %v at the goal", got)
	}
}
