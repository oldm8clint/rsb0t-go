package retaliateplan

import (
	"fmt"
	"os"
	"testing"
)

// TestPrintTables prints the numbers quoted in docs/two_tick_retaliate_research_2026-11-04.md.
// It is skipped unless RETALIATE_TABLES=1:  RETALIATE_TABLES=1 go test -run TestPrintTables -v ./internal/scripts/retaliateplan/
func TestPrintTables(t *testing.T) {
	if os.Getenv("RETALIATE_TABLES") != "1" {
		t.Skip("set RETALIATE_TABLES=1 to print the document tables")
	}
	for _, axe := range []string{"rune", "adamant"} {
		fmt.Println("AXE", axe)
		for _, L := range []int{50, 60, 70, 80, 90, 99} {
			fmt.Printf("L%d:", L)
			for _, tr := range Trees {
				if L < tr.LevelRequired {
					fmt.Printf(" | %s -", tr.Name)
					continue
				}
				lo, hi := tr.Low, tr.High
				if axe == "adamant" {
					lo, hi = tr.ALow, tr.AHigh
				}
				p := RollChance(L, lo, hi)
				fmt.Printf(" | %s p=%.3f xp/roll=%.1f 4t=%.0f 3t=%.0f 2t=%.0f", tr.Name, p, p*tr.XP, XPPerHour(tr.XP, p, 4, 1), XPPerHour(tr.XP, p, 3, 1), XPPerHour(tr.XP, p, 2, 1))
			}
			fmt.Println()
		}
	}
	fmt.Println("OAK SITE (rune axe, respawn 28 ticks)")
	tr := Trees[0]
	for _, L := range []int{50, 60, 70} {
		p := RollChance(L, tr.Low, tr.High)
		for _, c := range []struct {
			name   string
			period float64
			m      int
			swap   float64
		}{
			{"plain 4t, 3 oaks, swap 4", 4, 3, 4},
			{"plain 4t, pair, swap 1", 4, 2, 1},
			{"plain 4t, pair plus third, swap 2", 4, 3, 2},
			{"knife 3t, 3 oaks, swap 4", 3, 3, 4},
			{"knife 3t, pair, swap 1", 3, 2, 1},
			{"knife 3t, pair plus third, swap 2", 3, 3, 2},
			{"2t, 3 oaks, swap 4", 2, 3, 4},
			{"2t, pair, swap 1", 2, 2, 1},
			{"2t, pair plus third, swap 2", 2, 3, 2},
			{"2t, 1 oak", 2, 1, 0},
		} {
			chop := RollsToFell(p) * c.period
			u := Uptime(c.m, chop, c.swap, 28)
			fmt.Printf("L%d p=%.3f %-36s chop=%.1f uptime=%.2f xp/h=%.0f\n", L, p, c.name, chop, u, XPPerHour(tr.XP, p, c.period, u))
		}
	}
	for _, d := range []int{1, 30, 60, 80} {
		fmt.Printf("def %d: goblin (roll 430) hit %.3f hp/h %.0f; goblin_armed (roll 912) hit %.3f hp/h %.0f\n", d,
			HitChance(430, PlayerDefenceRoll(d)), 3000*HitChance(430, PlayerDefenceRoll(d))*0.5,
			HitChance(912, PlayerDefenceRoll(d)), 3000*HitChance(912, PlayerDefenceRoll(d))*0.5)
	}
}
