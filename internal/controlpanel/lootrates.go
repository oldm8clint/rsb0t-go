package controlpanel

import (
	"sort"
	"strings"

	"rsb0t-go/internal/loot"
)

// KILLS PER HOUR AND RUNES PER HOUR, PER NPC (a user request: "npcs we train on that provide best loot per hour in runes that we use
// for magic training").
//
// Each npc kill source gets its kills an hour and the market value of the runes its kills dropped, in total, per kill and per hour. The
// timing is the panel's own (web/panels/loot.js hourlyRate): the count over the window from the source's first record (or the process
// start, for the since-restart period) to now, and no rate at all under RateMinHours, so a source that has only just appeared does not
// report a figure extrapolated from minutes. The window is the whole span the source has existed, idle time included, exactly as every
// other per-hour figure on a card is, so it is an average and not a rate while an account trains there.
//
// The runes are the thirteen a spell can delete (loot.IsRuneID), valued at the unit the item rows already carry (the market median, then
// alchemy, then cache cost). Runes the kills dropped are gross, before the runes the account cast to make them (Spent, which the card
// nets off its own value). RuneNPCs ranks the npcs by runes an hour, best first, merging the npc ids that share a name.

const (
	// RateMinHours is the shortest window an hourly figure is given for: five minutes, as loot.js's RATE_MIN_HOURS.
	RateMinHours = 5.0 / 60
	// rankMinKills is how many kills an npc needs before it is ranked: a lucky pile on three kills is not a source of runes.
	rankMinKills = 20
	// maxRuneNPCs bounds the ranking.
	maxRuneNPCs = 10
)

// RuneNPC is one npc's row in the ranking.
type RuneNPC struct {
	Name  string `json:"name"`
	Level int    `json:"level,omitempty"`
	Kills int64  `json:"kills"`
	// KillsPerHour and RuneGpPerHour are over the window the card's own rates use; RuneGp is the runes' total value and RuneGpPerKill its
	// share of one kill.
	KillsPerHour  float64 `json:"killsPerHour"`
	RuneGp        int64   `json:"runeGp"`
	RuneGpPerHour float64 `json:"runeGpPerHour"`
	RuneGpPerKill float64 `json:"runeGpPerKill"`
	Accounts      int     `json:"accounts"`
}

// runeValue is the value of the runes among a source's items, at each item's unit.
func runeValue(items []loot.Item) int64 {
	var gp int64
	for _, it := range items {
		if loot.IsRuneID(it.ID) {
			gp += it.Count * it.Value
		}
	}
	return gp
}

// rateHours is the hours of the window [max(first, since), now], 0 when it is under RateMinHours.
func rateHours(first, since, now int64) float64 {
	start := first
	if since > start {
		start = since
	}
	h := float64(now-start) / 3600000
	if !(h >= RateMinHours) {
		return 0
	}
	return h
}

// addKillRates fills the kill and rune rates of every npc kill source in p and ranks the npcs. It runs on a finished period view, whose
// item values are already priced.
func addKillRates(p *LootPeriod, now, since int64) {
	type agg struct {
		row   RuneNPC
		first int64
	}
	by := map[string]*agg{}
	for i := range p.Sources {
		s := &p.Sources[i]
		if s.Source.Kind != loot.KindNPC || s.Source.Action != loot.ActionKill || s.Events <= 0 {
			continue
		}
		s.RuneValue = runeValue(s.Items)
		if h := rateHours(s.First, since, now); h > 0 {
			s.KillsPerHour = float64(s.Events) / h
			s.RuneGpPerHour = float64(s.RuneValue) / h
		}
		key := strings.ToLower(strings.TrimSpace(s.Source.Name))
		a := by[key]
		if a == nil {
			a = &agg{row: RuneNPC{Name: s.Source.Name}, first: s.First}
			by[key] = a
		}
		a.row.Kills += s.Events
		a.row.RuneGp += s.RuneValue
		if s.Source.Level > a.row.Level {
			a.row.Level = s.Source.Level
		}
		if s.GroupAccounts > a.row.Accounts {
			a.row.Accounts = s.GroupAccounts
		}
		if s.First > 0 && (a.first == 0 || s.First < a.first) {
			a.first = s.First
		}
	}
	var rows []RuneNPC
	for _, a := range by {
		if a.row.Kills < rankMinKills || a.row.RuneGp <= 0 {
			continue
		}
		h := rateHours(a.first, since, now)
		if h == 0 {
			continue // no rate yet: not ranked
		}
		a.row.RuneGpPerKill = float64(a.row.RuneGp) / float64(a.row.Kills)
		a.row.KillsPerHour = float64(a.row.Kills) / h
		a.row.RuneGpPerHour = float64(a.row.RuneGp) / h
		rows = append(rows, a.row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].RuneGpPerHour != rows[j].RuneGpPerHour {
			return rows[i].RuneGpPerHour > rows[j].RuneGpPerHour
		}
		return rows[i].Name < rows[j].Name
	})
	if len(rows) > maxRuneNPCs {
		rows = rows[:maxRuneNPCs]
	}
	p.RuneNPCs = rows
}
