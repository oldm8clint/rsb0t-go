package donorplan

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Translated from rsb0t-scripts test/donorPlan.test.ts. The bus-only "ask" and "coming" waits
// are not ported; handover_test.go covers the in-game steps that replace them.

func ms(v int64) time.Time { return time.UnixMilli(v) }

func TestHandoffDue(t *testing.T) {
	if !HandoffDue(10_000, 10_000, ms(1000), ms(0)) {
		t.Error("at the threshold")
	}
	if HandoffDue(9_999, 10_000, ms(1000), ms(0)) {
		t.Error("below the threshold")
	}
	if HandoffDue(10_000, 10_000, ms(1000), ms(2000)) {
		t.Error("before the retry time")
	}
	if HandoffDue(10_000, 0, ms(1000), ms(0)) {
		t.Error("a zero threshold")
	}
}

var rows = []ItemRow{
	{"Bronze pickaxe", 1265, 1},
	{"Uncut sapphire", 1623, 3},
	{"Rune essence", 1436, 10_500},
	{"Coins", 995, 40},
	{"", 0, 0},
	{"Beer", 1917, 0},
}

func isEssence(r ItemRow) bool { return r.Name == "Rune essence" }
func isPick(r ItemRow) bool    { return strings.Contains(strings.ToLower(r.Name), "pickaxe") }

func names(rs []ItemRow) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

func TestKeepListIsCaseInsensitiveAndBlanksStay(t *testing.T) {
	if !InKeep("Coins", []string{"coins"}) || InKeep("Coins", nil) || !InKeep("", nil) || !InKeep("  ", nil) {
		t.Error("InKeep")
	}
}

func TestHandoffRows(t *testing.T) {
	cases := []struct {
		rules Rules
		want  []string
	}{
		{Rules{Skip: isPick, First: isEssence}, []string{"Rune essence", "Uncut sapphire", "Coins"}},
		{Rules{Keep: []string{"coins"}, Skip: isPick, First: isEssence}, []string{"Rune essence", "Uncut sapphire"}},
		{Rules{}, []string{"Bronze pickaxe", "Uncut sapphire", "Rune essence", "Coins"}},
		{Rules{Untradeable: func(r ItemRow) bool { return r.ID == 1623 }}, []string{"Bronze pickaxe", "Rune essence", "Coins"}},
	}
	for _, c := range cases {
		if got := names(HandoffRows(rows, c.rules)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("HandoffRows = %v, want %v", got, c.want)
		}
	}
}

func TestHaulIsEveryPackSlotNotKept(t *testing.T) {
	pack := []ItemRow{{Name: "Rune pickaxe"}, {Name: "Rune essence"}, {Name: "Rune essence"}, {Name: "Uncut ruby"}}
	if got := HaulOf(pack, Rules{Skip: isPick}); got != 3 {
		t.Errorf("skip picks = %d", got)
	}
	if got := HaulOf(pack, Rules{Keep: []string{"Uncut ruby"}, Skip: isPick}); got != 2 {
		t.Errorf("keep ruby = %d", got)
	}
	if got := HaulOf(pack, Rules{}); got != 4 {
		t.Errorf("no rules = %d", got)
	}
}

func TestPlanLoad(t *testing.T) {
	pack := []ItemRow{
		{"Rune pickaxe", 1275, 1},
		{"Uncut sapphire", 1623, 1},
		{"Uncut sapphire", 1623, 1},
		{"Rune essence", 1437, 10_500},
		{"Uncut ruby", 1619, 1},
	}
	rules := Rules{Skip: isPick, First: isEssence}
	want := []LoadGroup{{"Rune essence", 1437, 1}, {"Uncut sapphire", 1623, 2}, {"Uncut ruby", 1619, 1}}
	if got := PlanLoad(pack, 28, rules); !reflect.DeepEqual(got, want) {
		t.Errorf("PlanLoad(28) = %+v", got)
	}
	groupNames := func(gs []LoadGroup) []string {
		out := []string{}
		for _, g := range gs {
			out = append(out, g.Name)
		}
		return out
	}
	if got := groupNames(PlanLoad(pack, 2, rules)); !reflect.DeepEqual(got, []string{"Rune essence", "Uncut ruby"}) {
		t.Errorf("PlanLoad(2) = %v", got)
	}
	if got := groupNames(PlanLoad(pack, 1, rules)); !reflect.DeepEqual(got, []string{"Rune essence"}) {
		t.Errorf("PlanLoad(1) = %v", got)
	}
	if got := PlanLoad(pack, 0, rules); len(got) != 0 {
		t.Errorf("PlanLoad(0) = %v", got)
	}
}

func TestMuleNearAndSeenFresh(t *testing.T) {
	stand := tile.Tile{X: 3251, Z: 3420}
	near := func(x, z, level int) bool { return MuleNear(&tile.Tile{X: x, Z: z, Level: level}, &stand, 2) }
	if !near(3251, 3421, 0) || near(3251, 3424, 0) || near(3251, 3421, 1) || MuleNear(nil, &stand, 2) {
		t.Error("MuleNear")
	}
	if !SeenFresh(ms(1000), ms(1000+MuleSeenTTL.Milliseconds()-1), MuleSeenTTL) {
		t.Error("fresh just inside the ttl")
	}
	if SeenFresh(ms(1000), ms(1000+MuleSeenTTL.Milliseconds()), MuleSeenTTL) {
		t.Error("stale at the ttl")
	}
	if SeenFresh(time.Time{}, ms(1000), MuleSeenTTL) {
		t.Error("never seen")
	}
}
