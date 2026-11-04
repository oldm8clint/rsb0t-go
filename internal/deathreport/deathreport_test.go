package deathreport

import (
	"strings"
	"testing"
)

func inAlch(name string) bool {
	switch name {
	case "Yew shortbow", "Yew longbow", "Adamant platebody":
		return true
	}
	return false
}

func coins(name string) int {
	switch name {
	case "Yew shortbow":
		return 480
	case "Yew longbow":
		return 768
	}
	return 100
}

func food(name string) bool { return name == "Shark" || name == "Lobster" }

func TestLineNamesTheKillerTheHitpointsTheFoodAndTheStock(t *testing.T) {
	got := Line(Input{
		HaveTile: true, X: 2508, Z: 3097,
		HPPeak: 50, HPLast: 7, MaxHP: 50,
		Attackers: []string{"Ogre", "Ogre"},
		Pack: []PackItem{
			{"Yew shortbow", 721, true}, {"Yew longbow", 225, true}, {"Shark", 3, false}, {"Shark", 40, true},
			{"Nature rune", 132, false}, {"Fire rune", 179, false}, {"Coins", 5000, false},
		},
		IsFood: food, IsAlch: inAlch, AlchValue: coins,
	})
	want := "DEATH at 2508,3097,0 (Yanille west ogres): killed by 2 Ogre, HP 50/50 before, 7 left, food left 3, " +
		"carrying 946 alchables ~518,880 coins (721 Yew shortbow, 225 Yew longbow), 132 Nature rune, 179 Fire rune"
	if got != want {
		t.Fatalf("line =\n%s\nwant\n%s", got, want)
	}
}

func TestLineWithNothingKnown(t *testing.T) {
	got := Line(Input{})
	for _, frag := range []string{"an unknown tile", "killed by unknown", "HP unknown", "food left 0", "no alch stock"} {
		if !strings.Contains(got, frag) {
			t.Errorf("%q lacks %q", got, frag)
		}
	}
}

func TestKillersListsNPCsPlayersAndPoison(t *testing.T) {
	cases := []struct {
		in   Input
		want string
	}{
		{Input{Attackers: []string{"Moss giant"}}, "Moss giant"},
		{Input{Attackers: []string{"Ogre", "Moss giant", "Ogre"}, Players: 2, Poisoned: true}, "2 Ogre, Moss giant, 2 player(s), poison"},
		{Input{Poisoned: true}, "poison"},
		{Input{Nearby: []string{"Man", "Man", "Guard"}}, "unknown (nothing faced the account; near it: Man, Guard)"},
		{Input{}, "unknown (nothing faced the account, nothing near it)"},
	}
	for _, c := range cases {
		if got := Killers(c.in); got != c.want {
			t.Errorf("Killers(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSummariseCountsOnlyTheAlchList(t *testing.T) {
	st := Summarise([]PackItem{{"Yew shortbow", 10, true}, {"Yew shortbow", 5, false}, {"Bronze dagger", 9, false}}, inAlch, coins)
	if st.Alchables != 15 || st.Value != 15*480 || len(st.Lines) != 1 || st.Lines[0] != "15 Yew shortbow" {
		t.Fatalf("stock = %+v", st)
	}
	if got := (Stock{}).Describe(); got != "no alch stock" {
		t.Fatalf("empty stock = %q", got)
	}
}

func TestDescribeTrimsLongLists(t *testing.T) {
	var pack []PackItem
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		pack = append(pack, PackItem{n, 1, true})
	}
	got := Summarise(pack, func(string) bool { return true }, nil).Describe()
	if !strings.Contains(got, "2 more kind(s)") || strings.Contains(got, ", g") {
		t.Fatalf("list not trimmed: %q", got)
	}
}

func TestCommas(t *testing.T) {
	for in, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -2500: "-2,500"} {
		if got := Commas(in); got != want {
			t.Errorf("Commas(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestAreaNamesThePlacesAccountsDieIn(t *testing.T) {
	cases := []struct {
		x, z, level int
		want        string
	}{
		{2508, 3097, 0, "Yanille west ogres"},
		{2577, 2970, 0, "Feldip ogres (east)"},
		{2562, 3399, 0, "North Ardougne moss giants"},
		{2655, 3283, 0, "East Ardougne"},
		{3222, 3218, 0, "Lumbridge"},
		{3099, 9840, 0, "Edgeville dungeon"},
		{3099, 10040, 0, "Wilderness (underground)"},
		{3099, 10100, 0, "Wilderness (underground)"},
		{3100, 3600, 0, "Wilderness level 11"},
		{3100, 3600, 1, "m48_56"},
		{2539, 4716, 0, "Mage Bank"},
		{100, 100, 0, "m1_1"},
	}
	for _, c := range cases {
		if got := Area(c.x, c.z, c.level); got != c.want {
			t.Errorf("Area(%d,%d,%d) = %q, want %q", c.x, c.z, c.level, got, c.want)
		}
	}
}

func TestCauseKeyIsTheDistinctNamesWithoutCounts(t *testing.T) {
	cases := []struct {
		in   Input
		want string
	}{
		{Input{Attackers: []string{"Ogre", "Ogre", "Moss giant"}}, "Moss giant, Ogre"},
		{Input{Attackers: []string{"Ogre"}, Players: 2, Poisoned: true}, "Ogre, player, poison"},
		{Input{Nearby: []string{"Cow"}}, "unknown"},
		{Input{}, "unknown"},
	}
	for _, c := range cases {
		if got := CauseKey(c.in); got != c.want {
			t.Errorf("CauseKey(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}
