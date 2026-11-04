package wcfletchplan

import (
	"math"
	"testing"

	"rsb0t-go/internal/scripts/woodcutplan"
)

// SwingChance is the engine's STAT_RANDOM over trees.dbrow's pairs, worked by hand:
// floor(low*(99-L)/98) + floor(high*(L-1)/98) + 1, out of 256.
func TestSwingChanceIsStatRandom(t *testing.T) {
	cases := []struct {
		tier  string
		level int
		axe   string
		want  int // out of 256
	}{
		{"Maple", 65, woodcutplan.RuneAxe, 28*34/98 + 87*64/98 + 1}, // 9 + 56 + 1 = 66
		{"Maple", 61, "Adamant axe", 24*38/98 + 75*60/98 + 1},       // 9 + 45 + 1 = 55
		{"Willow", 65, woodcutplan.RuneAxe, 56*34/98 + 175*64/98 + 1},
		{"Yew", 70, woodcutplan.RuneAxe, 14*29/98 + 44*69/98 + 1},
		{"Magic", 90, woodcutplan.RuneAxe, 7*9/98 + 21*89/98 + 1}, // 0 + 19 + 1 = 20
		{"Magic", 85, "Mithril axe", 5*14/98 + 15*84/98 + 1},      // 0 + 12 + 1 = 13
		{"Maple", 99, woodcutplan.RuneAxe, 87 + 1},                // level 99: the high end
		{"Maple", 120, woodcutplan.RuneAxe, 87 + 1},               // the engine clamps at 99
	}
	for _, c := range cases {
		if got := SwingChance(c.tier, c.level, c.axe); math.Abs(got-float64(c.want)/256) > 1e-12 {
			t.Errorf("SwingChance(%s, %d, %s) = %.5f, want %d/256", c.tier, c.level, c.axe, got, c.want)
		}
	}
	if SwingChance("Maple", 65, "rune AXE") != SwingChance("Maple", 65, woodcutplan.RuneAxe) {
		t.Error("the axe name should match case-insensitively, as woodcutplan's do")
	}
	if SwingChance("Normal tree", 80, woodcutplan.RuneAxe) != 0 || SwingChance("Maple", 65, "Dragon axe") != 0 {
		t.Error("a tier or axe the table does not hold must price at zero, not guess")
	}
}

// Every axe in woodcutplan.Axes is in every table, none is worse than the one before and each is
// better at the high end: the table is trees.dbrow, which lists them bronze to rune in rising
// order. The magic table gives the black and mithril axes the same low 5, so the low end may tie.
func TestTreeChanceCoversEveryAxeInOrder(t *testing.T) {
	for tier, pairs := range treeChance {
		prev := [2]int{}
		for _, axe := range woodcutplan.Axes {
			p, ok := pairs[axe]
			if !ok {
				t.Errorf("%s: no successchance for %s", tier, axe)
				continue
			}
			if p[0] < prev[0] || p[1] <= prev[1] {
				t.Errorf("%s: %s %v is not better than the axe before it %v", tier, axe, p, prev)
			}
			prev = p
		}
		if _, ok := woodcutplan.TierSupply[tier]; !ok {
			t.Errorf("%s has a swing table but no supply rule in woodcutplan.TierSupply", tier)
		}
	}
}

func TestXPPerLogIsTheLadders(t *testing.T) {
	cases := []struct {
		rung, fletch int
		want         float64
	}{
		{MapleRung, 55, 100 + 58.3}, // maple longbow
		{MapleRung, 50, 100 + 50},   // maple shortbow
		{WillowRung, 50, 67.5 + 41.5},
		{WillowRung, 63, 67.5 + 41.5},
	}
	for _, c := range cases {
		if got := XPPerLog(c.rung, c.fletch); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("XPPerLog(%d, %d) = %.2f, want %.2f", c.rung, c.fletch, got, c.want)
		}
	}
}

func TestThroughputNeverExceedsSupplyOrDemand(t *testing.T) {
	prev := 0.0
	for d := 0.0; d <= 20000; d += 250 {
		got := Throughput(1713, d)
		if got > 1713+1e-9 || got > d+1e-9 || got < prev {
			t.Fatalf("Throughput(1713, %.0f) = %.1f after %.1f", d, got, prev)
		}
		prev = got
	}
	if Throughput(0, 500) != 0 || Throughput(500, 0) != 0 {
		t.Error("no supply or no demand is no logs")
	}
}

// A load costs its walk: the same account takes fewer logs an hour from a camp further out, and
// an uncontended rune-axe account takes about the same experience at a maple or a willow (the
// reason the willow overflow is priced on SUPPLY, not on the account).
func TestAccountRateCostsTheWalk(t *testing.T) {
	near := AccountLogsPerHour("Maple", ReferenceChopper, 18, true)
	far := AccountLogsPerHour("Maple", ReferenceChopper, 81, true)
	if !(far < near) {
		t.Errorf("81 steps out (%.0f logs/h) should cost against 18 steps (%.0f)", far, near)
	}
	maple := near * XPPerLog(MapleRung, ReferenceChopper.Fletch)
	willow := AccountLogsPerHour("Willow", ReferenceChopper, 32, true) * XPPerLog(WillowRung, ReferenceChopper.Fletch)
	if r := willow / maple; r < 0.8 || r > 1.25 {
		t.Errorf("uncontended willow %.0f xp/h vs maple %.0f: the premise is that they are close", willow, maple)
	}
	t.Logf("uncontended reference account: maple %.0f logs/h = %.0f xp/h, willow %.0f xp/h", near, maple, willow)
}

// THE MEASUREMENT THE MODEL WAS GIVEN. Live e35420b6, 2026-09-25 17:13 -> 17:27 (13.2 minutes of
// two panel snapshots): the 79-account maple cohort, all at the Seers maples, gained 190,881
// Woodcutting + 102,967 Fletching = 293,848 xp an hour. The model's status-quo line must land
// within 15% of it, or the option table in maplecamps.go is built on sand.
func TestStatusQuoMatchesTheMeasurement(t *testing.T) {
	at := make([]int, len(liveMapleCohort))
	got, _ := CohortXP(MapleCamps, liveMapleCohort, at, BoardPlayers)
	const measured = 190881 + 102967
	if d := math.Abs(got-measured) / measured; d > 0.15 {
		t.Errorf("status quo priced at %.0f xp/h, measured %d: %.0f%% off", got, measured, 100*d)
	}
	t.Logf("status quo: model %.0f xp/h, measured %d", got, measured)
}

// liveMapleCohort is the (Woodcutting, Fletching) of every ProgressiveWCFletch account in game
// at 2026-09-25 17:20 (live panel snapshot; levels only), with the rune axe the cohort's settings
// buy. Every one is on the maple rung.
var liveMapleCohort = func() []Chopper {
	pairs := [][2]int{
		{61, 51}, {61, 52}, {61, 52}, {61, 52}, {61, 53}, {61, 53}, {61, 54}, {62, 51}, {62, 52}, {62, 52},
		{62, 52}, {62, 52}, {62, 53}, {62, 53}, {62, 53}, {62, 53}, {62, 57}, {62, 57}, {63, 52}, {63, 52},
		{63, 53}, {63, 53}, {63, 54}, {63, 54}, {63, 55}, {63, 56}, {63, 57}, {64, 50}, {64, 51}, {64, 52},
		{64, 53}, {64, 53}, {64, 54}, {64, 57}, {64, 58}, {64, 58}, {64, 58}, {64, 59}, {64, 59}, {65, 54},
		{65, 54}, {65, 54}, {65, 55}, {65, 55}, {65, 56}, {65, 57}, {65, 59}, {65, 59}, {65, 59}, {65, 59},
		{65, 60}, {65, 60}, {66, 53}, {66, 54}, {66, 55}, {66, 56}, {66, 59}, {66, 60}, {66, 60}, {66, 61},
		{66, 61}, {66, 61}, {67, 52}, {67, 59}, {67, 61}, {67, 61}, {68, 53}, {68, 53}, {68, 55}, {68, 55},
		{68, 56}, {68, 62}, {68, 63}, {68, 63}, {70, 51}, {71, 55}, {71, 57}, {73, 55}, {74, 63},
	}
	out := make([]Chopper, len(pairs))
	for i, p := range pairs {
		out[i] = Chopper{Wood: p[0], Fletch: p[1], Axe: woodcutplan.RuneAxe}
	}
	return out
}()
