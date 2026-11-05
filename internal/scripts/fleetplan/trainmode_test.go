package fleetplan

import (
	"testing"
)

func modesOn() TrainModeSettings {
	return TrainModeSettings{Enabled: true, Alch: true, Teleport: true, AlchKit: 1000, LawReserve: 10,
		MuleLawFloor: 500, TeleportKit: 100, TeleportLandOnly: true}
}

// The owner's stock, best-paying first; never runes, coins, staffs or food; nothing off the list (the
// Willow longbows the fleet makes are not on it).
func TestRankAlchables(t *testing.T) {
	held := []AlchCandidate{
		{Name: "Willow longbow", Count: 95294, Value: 96},
		{Name: "Maple longbow", Count: 22377, Value: 192},
		{Name: "Adamant platelegs", Count: 44, Value: 3840},
		{Name: "Nature rune", Count: 100, Value: 12},
		{Name: "Coins", Count: 5000, Value: 1},
		{Name: "Staff of fire", Count: 1, Value: 900},
		{Name: "Rune kiteshield", Count: 1, Value: 39000},
		{Name: "Shortbow", Count: 0, Value: 13},
	}
	got := RankAlchables(held, modesOn())
	if len(got) != 2 || got[0].Name != "Adamant platelegs" || got[1].Name != "Maple longbow" {
		t.Fatalf("ranked %+v", got)
	}
	s := modesOn()
	s.AlchItems = []string{"Rune kiteshield", "Nature rune", "Staff of fire"}
	got = RankAlchables(held, s)
	if len(got) != 1 || got[0].Name != "Rune kiteshield" {
		t.Fatalf("a named item is allowed, a rune or a staff never: %+v", got)
	}
	s.AlchMinValue = 50000
	if got := RankAlchables(held, s); len(got) != 0 {
		t.Fatalf("below the minimum value: %+v", got)
	}
	r := modesOn().AlchItemsRanked()
	if len(r) != len(DefaultAlchItems) || r[0] != DefaultAlchItems[0] || r[len(r)-1] != "Hardleather body" {
		t.Fatalf("ask ladder %v", r)
	}
}

func askFor(lines []AskLine, item string) (AskLine, bool) {
	for _, l := range lines {
		for _, c := range l.Choices {
			if c == item {
				return l, true
			}
		}
	}
	return AskLine{}, false
}
