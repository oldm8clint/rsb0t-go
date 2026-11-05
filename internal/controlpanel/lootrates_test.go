package controlpanel

import (
	"testing"

	"rsb0t-go/internal/loot"
)

func killView(name string, id int, events, first int64, items ...loot.Item) LootSourceView {
	return LootSourceView{Source: loot.Source{Kind: loot.KindNPC, Action: loot.ActionKill, Name: name, ID: id, Level: 5}, Events: events, First: first, Items: items}
}

func TestAddKillRatesRanksNpcsByRuneGpPerHour(t *testing.T) {
	const hour = int64(3600000)
	now := 10 * hour
	p := LootPeriod{Sources: []LootSourceView{
		killView("Dwarf", 118, 200, now-2*hour, loot.Item{ID: 562, Name: "Chaos rune", Count: 100, Value: 90}, loot.Item{ID: 995, Name: "Coins", Count: 5000, Value: 1}),
		killView("Dwarf", 119, 100, now-4*hour, loot.Item{ID: 561, Name: "Nature rune", Count: 10, Value: 200}),
		killView("Khazard trooper", 475, 500, now-hour),
		killView("Giant", 7, 10, now-hour, loot.Item{ID: 565, Name: "Blood rune", Count: 50, Value: 400}), // under rankMinKills
		killView("Fresh", 8, 50, now-60000, loot.Item{ID: 554, Name: "Fire rune", Count: 500, Value: 5}),  // under a window
	}}
	addKillRates(&p, now, 0)
	if p.Sources[0].RuneValue != 9000 || p.Sources[0].KillsPerHour != 100 || p.Sources[0].RuneGpPerHour != 4500 {
		t.Errorf("source 0: %+v", p.Sources[0])
	}
	if p.Sources[4].KillsPerHour != 0 || p.Sources[4].RuneValue != 2500 {
		t.Errorf("a source with a one minute window got a rate: %+v", p.Sources[4])
	}
	if len(p.RuneNPCs) != 1 {
		t.Fatalf("ranking %+v", p.RuneNPCs)
	}
	d := p.RuneNPCs[0]
	// Both Dwarf ids are one npc: 300 kills, 9000+2000 gp of runes, over the older window (4 h).
	if d.Name != "Dwarf" || d.Kills != 300 || d.RuneGp != 11000 || d.KillsPerHour != 75 || d.RuneGpPerHour != 2750 {
		t.Errorf("Dwarf row %+v", d)
	}
	// The since-restart window starts at the process start.
	q := LootPeriod{Sources: []LootSourceView{killView("Dwarf", 118, 100, now-5*hour, loot.Item{ID: 562, Name: "Chaos rune", Count: 10, Value: 100})}}
	addKillRates(&q, now, now-hour)
	if q.Sources[0].KillsPerHour != 100 {
		t.Errorf("since-restart rate %v", q.Sources[0].KillsPerHour)
	}
}
