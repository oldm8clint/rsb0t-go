package controlpanel

import (
	"sort"

	"rsb0t-go/internal/loot"
)

// THE RUNECRAFTING SECTION OF THE LOOT PANEL (a user request: "runecrafting loot panel is needed").
//
// Nothing here is a new store. The crafts and the essence hand-overs are loot Drops like every other (internal/loot/
// runecraft.go): runecraftrunner.Craft reports a verified craft, the EssenceMiner supply loop reports the essence a
// hand-over moved, and both ride the same path as the Fishing and Fletching cards -- the fleet's per-account Tally, this
// aggregator's lifetime record (persisted in stats-state.json, so it survives a restart) and its since-restart record.
// The generic "Runecraft" and "Essence supply" cards come from those records with no code at all.
//
// This file only READS the records into the section's own figures, because the owner's list is not a card's: the runes
// by type with their market value, the essence bound, the experience and the level, the crafts, each account's share,
// and the supply side (the essence the miners mined and the essence they handed to the mule). A card cannot give the
// per-account split, and the mined essence lives on the Mining card's rock rows, not on a source of its own.
//
// THE LEVEL is Source.Level of the Runecraft rows, the highest of the accounts' last readings. The PER-HOUR rates are not
// computed here: they depend on whether the panel is showing the lifetime or the since-restart window (and on a window
// being long enough to mean anything), a rule the page already has for every card (hourlyRate in loot.js), so this sends
// the counts and the moments they started.
//
// THE MULE'S STOCK is not an event and is not in the records: it is read, live, from the mule's cached bank (EssenceStock).

// RunecraftRune is one rune type the altars made, with what it is worth at the market price the panel uses everywhere.
type RunecraftRune struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
	// Unit is what one rune is worth (lootUnit), Value is Count x Unit.
	Unit  int64 `json:"unit"`
	Value int64 `json:"value"`
}

// RunecraftAltar is one altar's row: what was crafted there.
type RunecraftAltar struct {
	Name     string `json:"name"`
	RuneID   int    `json:"runeId"`
	Crafts   int64  `json:"crafts"`
	Essence  int64  `json:"essence"`
	Runes    int64  `json:"runes"`
	XP       int64  `json:"xp"`
	Level    int    `json:"level,omitempty"`
	Value    int64  `json:"value"`
	Accounts int    `json:"accounts"`
	First    int64  `json:"first,omitempty"`
	Last     int64  `json:"last,omitempty"`
}

// RunecraftSupply is the essence side: what was mined and what was handed to the mule.
type RunecraftSupply struct {
	// Mined is the Rune essence the Mining card's rock rows record arriving; Handed what crossed to the mule.
	Mined          int64 `json:"mined"`
	MinedFirst     int64 `json:"minedFirst,omitempty"`
	MinedLast      int64 `json:"minedLast,omitempty"`
	MinedAccounts  int   `json:"minedAccounts"`
	Handed         int64 `json:"handed"`
	HandedFirst    int64 `json:"handedFirst,omitempty"`
	HandedLast     int64 `json:"handedLast,omitempty"`
	HandedAccounts int   `json:"handedAccounts"`
}

// RunecraftAccount is one account's share: crafting, mining essence and handing it over.
type RunecraftAccount struct {
	Account string `json:"account"`
	Crafts  int64  `json:"crafts"`
	Essence int64  `json:"essence"`
	Runes   int64  `json:"runes"`
	// Value is the runes' worth; Cost the essence they used up when it has a trustworthy price (0 otherwise).
	Value  int64 `json:"value"`
	Cost   int64 `json:"cost,omitempty"`
	XP     int64 `json:"xp"`
	Level  int   `json:"level,omitempty"`
	Mined  int64 `json:"mined,omitempty"`
	Handed int64 `json:"handed,omitempty"`
	Last   int64 `json:"last,omitempty"`
}

// RunecraftPeriod is the Runecrafting section of one period (lifetime or since restart).
type RunecraftPeriod struct {
	Crafts  int64 `json:"crafts"`
	Essence int64 `json:"essence"`
	Runes   int64 `json:"runes"`
	XP      int64 `json:"xp"`
	Level   int   `json:"level,omitempty"`
	// Gross is what the runes are worth; Cost what the essence they bound was worth, known only when every craft's
	// essence has a trustworthy price (CostKnown). Net is Gross less the known Cost, the same rule as every card.
	Gross     int64 `json:"gross"`
	Cost      int64 `json:"cost"`
	CostKnown bool  `json:"costKnown"`
	Net       int64 `json:"net"`
	// Accounts is how many accounts crafted. First and Last are the first and the latest craft.
	Accounts int   `json:"accounts"`
	First    int64 `json:"first,omitempty"`
	Last     int64 `json:"last,omitempty"`

	ByRune  []RunecraftRune    `json:"byRune"`
	Altars  []RunecraftAltar   `json:"altars"`
	Supply  RunecraftSupply    `json:"supply"`
	PerAcct []RunecraftAccount `json:"perAccount"`
}

// EssenceStock is the Rune essence one mule's bank last showed (noted or not), and when that bank was read.
type EssenceStock struct {
	Account string `json:"account"`
	Essence int64  `json:"essence"`
	Updated int64  `json:"updated"`
}

// essenceIn sums the Rune essence, noted or not, among items.
func essenceIn(items []loot.Item) int64 {
	var n int64
	for _, it := range items {
		if loot.IsRuneEssence(it.ID) {
			n += it.Count
		}
	}
	return n
}

// widen moves a [first, last] span to cover another moment pair; a zero first is "not set".
func widen(first, last *int64, f, l int64) {
	if f > 0 && (*first == 0 || f < *first) {
		*first = f
	}
	if l > *last {
		*last = l
	}
}

// runecraftView reads one period's records into the section. It returns nil when the period holds nothing of
// runecrafting at all (no craft, no hand-over, no essence mined), so a fleet that never crafted sends no block.
func runecraftView(m map[string]map[string]*lootRec, prices *PriceBook, credited creditBook) *RunecraftPeriod {
	rc := &RunecraftPeriod{CostKnown: true, ByRune: []RunecraftRune{}, Altars: []RunecraftAltar{}, PerAcct: []RunecraftAccount{}}
	runes := map[int]*RunecraftRune{}
	altars := map[string]*RunecraftAltar{}
	altarAccts := map[string]map[string]bool{}
	minedAccts, handedAccts := 0, 0
	seen := false

	accounts := make([]string, 0, len(m))
	for acct := range m {
		accounts = append(accounts, acct)
	}
	sort.Strings(accounts)
	for _, acct := range accounts {
		row := RunecraftAccount{Account: acct}
		crafted, mined, handed := false, false, false
		for _, rec := range m[acct] {
			src := rec.Source
			switch {
			case loot.IsRunecraft(src):
				seen, crafted = true, true
				essence := essenceIn(rec.Spent)
				row.Crafts += rec.Events
				row.Essence += essence
				row.XP += rec.XP
				row.Level = max(row.Level, src.Level)
				row.Last = max(row.Last, rec.Last)
				widen(&rc.First, &rc.Last, rec.First, rec.Last)

				al := altars[src.PartKey()]
				if al == nil {
					al = &RunecraftAltar{Name: src.Name, RuneID: src.ID}
					altars[src.PartKey()] = al
					altarAccts[src.PartKey()] = map[string]bool{}
				}
				altarAccts[src.PartKey()][acct] = true
				al.Crafts += rec.Events
				al.Essence += essence
				al.XP += rec.XP
				al.Level = max(al.Level, src.Level)
				widen(&al.First, &al.Last, rec.First, rec.Last)
				for _, it := range rec.Items {
					unit := lootUnit(prices, it)
					row.Runes += it.Count
					row.Value += it.Count * unit
					al.Runes += it.Count
					al.Value += it.Count * unit
					r := runes[it.ID]
					if r == nil {
						r = &RunecraftRune{ID: it.ID, Name: it.Name}
						runes[it.ID] = r
					}
					if r.Name == "" {
						r.Name = it.Name
					}
					r.Count += it.Count
					r.Unit = unit
					r.Value += it.Count * unit
				}
				if len(rec.Spent) > 0 {
					if c, known := spentCost(prices, src, rec.Spent, credited); known {
						row.Cost += c
					} else {
						rc.CostKnown = false
					}
				}
			case loot.IsEssenceSupply(src):
				seen, handed = true, true
				row.Handed += rec.Events
				row.Last = max(row.Last, rec.Last)
				rc.Supply.Handed += rec.Events
				widen(&rc.Supply.HandedFirst, &rc.Supply.HandedLast, rec.First, rec.Last)
			case src.Kind == loot.KindGathering && src.Skill == loot.SkillMining:
				// The essence the miners took out of the rock, from the rock rows' own items (nothing else in the fleet
				// mines it; ore from another rock is not Rune essence and is not counted).
				n := essenceIn(rec.Items)
				if n <= 0 {
					continue
				}
				seen, mined = true, true
				row.Mined += n
				row.Last = max(row.Last, rec.Last)
				rc.Supply.Mined += n
				widen(&rc.Supply.MinedFirst, &rc.Supply.MinedLast, rec.First, rec.Last)
			}
		}
		if crafted {
			rc.Accounts++
			rc.Crafts += row.Crafts
			rc.Essence += row.Essence
			rc.Runes += row.Runes
			rc.XP += row.XP
			rc.Level = max(rc.Level, row.Level)
			rc.Gross += row.Value
			rc.Cost += row.Cost
		}
		if mined {
			minedAccts++
		}
		if handed {
			handedAccts++
		}
		if crafted || mined || handed {
			rc.PerAcct = append(rc.PerAcct, row)
		}
	}
	if !seen {
		return nil
	}
	rc.Supply.MinedAccounts, rc.Supply.HandedAccounts = minedAccts, handedAccts
	rc.Net = rc.Gross
	if rc.CostKnown {
		rc.Net = rc.Gross - rc.Cost
	}

	for _, r := range runes {
		rc.ByRune = append(rc.ByRune, *r)
	}
	sort.Slice(rc.ByRune, func(i, j int) bool {
		a, b := rc.ByRune[i], rc.ByRune[j]
		return a.Value > b.Value || a.Value == b.Value && (a.Count > b.Count || a.Count == b.Count && a.ID < b.ID)
	})
	for key, al := range altars {
		al.Accounts = len(altarAccts[key])
		rc.Altars = append(rc.Altars, *al)
	}
	sort.Slice(rc.Altars, func(i, j int) bool {
		a, b := rc.Altars[i], rc.Altars[j]
		return a.Crafts > b.Crafts || a.Crafts == b.Crafts && a.Name < b.Name
	})
	sort.Slice(rc.PerAcct, func(i, j int) bool {
		a, b := rc.PerAcct[i], rc.PerAcct[j]
		switch {
		case a.Value != b.Value:
			return a.Value > b.Value
		case a.Crafts != b.Crafts:
			return a.Crafts > b.Crafts
		case a.Handed != b.Handed:
			return a.Handed > b.Handed
		case a.Mined != b.Mined:
			return a.Mined > b.Mined
		}
		return a.Account < b.Account
	})
	return rc
}

// essenceStock reads the Rune essence each mule's cached bank last showed, for the section's "mule stock" line. It is empty
// when the Source reports no mules or no banks, when a mule's bank has never been read, and when nothing of runecrafting is
// recorded yet (wanted false: the page has no section to put it in, and this is a call into the fleet on a request path).
func (a *statsAggregator) essenceStock(wanted bool) []EssenceStock {
	if !wanted {
		return nil
	}
	ms, ok := a.src.(MuleSource)
	bs, ok2 := a.src.(BankSource)
	if !ok || !ok2 {
		return nil
	}
	var out []EssenceStock
	done := map[string]bool{}
	for _, st := range ms.MuleStatuses() {
		if st.Account == "" || done[st.Account] {
			continue
		}
		done[st.Account] = true
		slots, updated, ok := bs.Bank(st.Account)
		if !ok || updated <= 0 {
			continue
		}
		var n int64
		for _, sl := range slots {
			if loot.IsRuneEssence(int(sl.ID)) {
				n += int64(sl.Count)
			}
		}
		out = append(out, EssenceStock{Account: st.Account, Essence: n, Updated: updated})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Account < out[j].Account })
	return out
}
