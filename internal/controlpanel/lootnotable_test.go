package controlpanel

import (
	"testing"

	"rsb0t-go/internal/loot"
)

func TestTrimRecentKeepsNotableRecordsPastTheRing(t *testing.T) {
	var recent []LootRecent
	for i := 0; i < maxLootRecent+30; i++ {
		r := LootRecent{At: int64(1000 - i)}
		if i == maxLootRecent+5 || i == maxLootRecent+9 {
			r.Notable = []int{3140}
		}
		recent = append(recent, r)
	}
	got := trimRecent(recent)
	if len(got) != maxLootRecent+2 {
		t.Fatalf("kept %d records", len(got))
	}
	if got[len(got)-1].At != recent[maxLootRecent+9].At {
		t.Errorf("kept the wrong tail record")
	}
	if short := trimRecent(recent[:10]); len(short) != 10 {
		t.Errorf("short ring trimmed to %d", len(short))
	}
}

func TestNotableIDsUsesTheRule(t *testing.T) {
	items := []loot.Item{
		{ID: 3140, Name: "Dragon chainbody", Count: 1, Value: 50000},
		{ID: 526, Name: "Bones", Count: 1, Value: 1},
		{ID: 995, Name: "Coins", Count: 90000, Value: 1},
	}
	got := notableIDs(priced(map[string]MarketPrice{}), items)
	if len(got) != 1 || got[0] != 3140 {
		t.Errorf("notable ids %v", got)
	}
}
