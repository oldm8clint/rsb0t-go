package controlpanel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/loot"
)

// legacyLootFile is a stats file in the exact shape the live build (3dfb543a) persisted, written by
// hand from the live /api/loot and run/stats-state.json rather than built with today's
// constructors, which cannot produce it:
//
//   - pickpockets as {kind npc, no skill, no model}, a stall steal as {kind loc, model loc};
//   - the Guard record carrying the two eaten cake leftovers the live one has;
//   - a ScarabSwarm account with 3 cow kills and NO items on kill:cow:81, and its bones on an
//     openable named "Ground" with id -1 and zero events;
//   - a history bucket and a recent-ticker entry for that Ground source.
const legacyLootFile = `{
  "trackingSince": 1726700000000,
  "loot": {
    "accounts": {
      "thief1": {
        "pickpocket:man:1": {"source": {"kind": "npc", "action": "pickpocket", "name": "Man", "id": 1, "level": 2},
          "events": 40, "items": [{"id": 995, "name": "Coins", "count": 120, "value": 1}], "first": 1726700000000, "last": 1726700100000},
        "pickpocket:man:3": {"source": {"kind": "npc", "action": "pickpocket", "name": "Man", "id": 3, "level": 2},
          "events": 10, "items": [{"id": 995, "name": "Coins", "count": 30, "value": 1}], "first": 1726700000000, "last": 1726700100000},
        "pickpocket:guard:32": {"source": {"kind": "npc", "action": "pickpocket", "name": "Guard", "id": 32, "level": 21},
          "events": 5, "items": [{"id": 995, "name": "Coins", "count": 150, "value": 1}, {"id": 1893, "name": "2/3 cake", "count": 1, "value": 5}, {"id": 1895, "name": "Slice of cake", "count": 3, "value": 3}], "first": 1726700000000, "last": 1726700100000}
      },
      "thief2": {
        "pickpocket:man:1": {"source": {"kind": "npc", "action": "pickpocket", "name": "Man", "id": 1, "level": 2},
          "events": 7, "items": [{"id": 995, "name": "Coins", "count": 21, "value": 1}], "first": 1726700000000, "last": 1726700100000},
        "steal:baker's stall:2561": {"source": {"kind": "loc", "action": "steal", "name": "Baker's stall", "id": 2561, "model": "loc"},
          "events": 9, "items": [{"id": 1891, "name": "Cake", "count": 6, "value": 50}, {"id": 2309, "name": "Bread", "count": 3, "value": 12}], "first": 1726700000000, "last": 1726700200000}
      },
      "acct1": {
        "kill:cow:81": {"source": {"kind": "npc", "action": "kill", "name": "Cow", "id": 81, "level": 2},
          "events": 3, "items": [], "first": 1726700300000, "last": 1726700500000},
        "open:ground:-1": {"source": {"kind": "openable", "action": "open", "name": "Ground", "id": -1},
          "events": 0, "items": [{"id": 526, "name": "Bones", "count": 4, "value": 1}], "first": 1726700250000, "last": 1726700600000}
      }
    },
    "recent": [
      {"at": 1726700600000, "account": "acct1", "source": {"kind": "openable", "action": "open", "name": "Ground", "id": -1}, "events": 0, "items": [{"id": 526, "name": "Bones", "count": 1}]}
    ],
    "history": [
      {"start": 1726700400000, "items": {"526": 4}, "events": {"open:ground:-1": 0, "kill:cow:81": 3}}
    ]
  }
}`

func restoreLootFile(t *testing.T, body string) *statsAggregator {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stats.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return newStatsAggregator(path, nil, fixedNow(time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)))
}

// TestLegacyThievingRestoresOntoOneCardWithItsRows is "maybe combine pickpocketing to one panel"
// against a file the live build really wrote. On the old code the four sources come back on three
// cards (npc|pickpocket|man, npc|pickpocket|guard, loc|steal|baker's stall).
func TestLegacyThievingRestoresOntoOneCardWithItsRows(t *testing.T) {
	a := restoreLootFile(t, legacyLootFile)
	p := a.lootSnapshot().Lifetime

	cards := map[string]bool{}
	for _, s := range p.Sources {
		if s.Source.Action == loot.ActionPickpocket || s.Source.Action == loot.ActionSteal {
			cards[s.Source.GroupKey()] = true
			// Every card figure is the union over the WHOLE card: thief1 and thief2.
			if s.GroupAccounts != 2 {
				t.Fatalf("%s group accounts = %d, want 2", s.Key, s.GroupAccounts)
			}
		}
	}
	if len(cards) != 1 || !cards["thieving|thieving"] {
		t.Fatalf("thieving sources restored onto %v, want the one thieving|thieving card", cards)
	}
	// The rows inside it: the Man row is the union of both thieves over ids 1 and 3.
	man1, _ := sourceView(p, loot.NPC(nil, loot.ActionPickpocket, 1, "Man"))
	man3, _ := sourceView(p, loot.NPC(nil, loot.ActionPickpocket, 3, "Man"))
	guard, _ := sourceView(p, loot.NPC(nil, loot.ActionPickpocket, 32, "Guard"))
	if man1.PartAccounts != 2 || man3.PartAccounts != 2 || man3.Accounts != 1 {
		t.Fatalf("Man row accounts = %d/%d (id 3 alone %d), want the row's union 2 and id 3's own 1",
			man1.PartAccounts, man3.PartAccounts, man3.Accounts)
	}
	if guard.PartAccounts != 1 {
		t.Fatalf("Guard row accounts = %d, want 1", guard.PartAccounts)
	}
	// Keys and counts are what the file carried: nothing merged away.
	if man1.Events != 47 || guard.Events != 5 {
		t.Fatalf("events man:1 %d guard %d, want 47 and 5", man1.Events, guard.Events)
	}
	// The eaten cake leftovers are gone from the Guard's take; its coins are not.
	if itemCount(guard, 1893) != 0 || itemCount(guard, 1895) != 0 || itemCount(guard, 995) != 150 {
		t.Fatalf("guard items = %+v, want only the 150 coins", guard.Items)
	}
	stall, _ := sourceView(p, loot.Stall(nil, 2561, "Baker's stall"))
	if itemCount(stall, 1891) != 6 {
		t.Fatalf("the stall's real Cake yield was touched: %+v", stall.Items)
	}
}

// TestLegacyGroundBonesRestoreOntoTheCowThatDroppedThem is "cows show no loot even though theres
// kills". On the old code kill:cow:81 comes back with no items and open:ground:-1 with the bones.
func TestLegacyGroundBonesRestoreOntoTheCowThatDroppedThem(t *testing.T) {
	a := restoreLootFile(t, legacyLootFile)
	body := a.lootSnapshot()
	p := body.Lifetime

	cow, ok := sourceView(p, loot.LegacyGroundKill)
	if !ok {
		t.Fatal("the cow kill record disappeared")
	}
	if cow.Events != 3 || itemCount(cow, 526) != 4 {
		t.Fatalf("cow = %d kills, %d bones; want 3 kills and the 4 bones", cow.Events, itemCount(cow, 526))
	}
	if cow.First != 1726700250000 || cow.Last != 1726700600000 {
		t.Fatalf("cow span %d..%d, want it widened to both records' span", cow.First, cow.Last)
	}
	for _, s := range p.Sources {
		if loot.IsLegacyGroundPickup(s.Source) || s.Key == "open:ground:-1" {
			t.Fatalf("the Ground source survived the restore: %+v", s)
		}
	}
	if _, ok := p.Events["open:Ground"]; ok {
		t.Fatal("the header still counts Ground opens")
	}
	if len(body.Recent) != 1 || body.Recent[0].Source.Key() != "kill:cow:81" {
		t.Fatalf("recent ticker entry = %+v, want it moved onto the cow", body.Recent)
	}
	for _, h := range body.History {
		if _, ok := h.Events["open:ground:-1"]; ok || h.Events["kill:cow:81"] != 3 {
			t.Fatalf("history events = %v, want the ground key gone and the 3 cow kills intact", h.Events)
		}
	}
}

// TestLegacyRestoreIsIdempotentAcrossASave: the migrated state is written back in the new shape
// on the next save, and loading THAT changes nothing further.
func TestLegacyRestoreIsIdempotentAcrossASave(t *testing.T) {
	a := restoreLootFile(t, legacyLootFile)
	first := a.lootSnapshot().Lifetime
	saved, err := json.Marshal(struct {
		TrackingSince int64     `json:"trackingSince"`
		Loot          *lootFile `json:"loot"`
	}{1726700000000, a.loot.persisted()})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), `"open:ground:-1"`) || strings.Contains(string(saved), `"kind":"npc","action":"pickpocket"`) {
		t.Fatalf("the saved file still carries a legacy shape: %s", saved)
	}
	b := restoreLootFile(t, string(saved))
	second := b.lootSnapshot().Lifetime
	if len(first.Sources) != len(second.Sources) {
		t.Fatalf("sources %d then %d", len(first.Sources), len(second.Sources))
	}
	for i := range first.Sources {
		f, s := first.Sources[i], second.Sources[i]
		if f.Key != s.Key || f.Events != s.Events || len(f.Items) != len(s.Items) || f.Source != s.Source {
			t.Fatalf("source %d changed across a save: %+v vs %+v", i, f, s)
		}
	}
}

// TestBuryXPSurvivesTheAggregator: a bury drop carries experience, not items, and the aggregator
// used to diff only events and items — so XP would have been lost between the fleet's tally and
// the panel. It also reaches the period's per-skill XP total the Prayer tile reads.
func TestBuryXPSurvivesTheAggregator(t *testing.T) {
	base := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	bones := loot.Bury(nil, 526, "")
	a.loot.record(lootReading("burier", 1, loot.Totals{Source: bones, Events: 2, XP: 9}), base)
	a.loot.record(lootReading("burier", 2, loot.Totals{Source: bones, Events: 3, XP: 13}), base)
	p := a.lootSnapshot().Lifetime
	v, ok := sourceView(p, bones)
	if !ok || v.Events != 3 || v.XP != 13 {
		t.Fatalf("bury source = %+v (%v), want 3 bones and 13 xp", v, ok)
	}
	if p.XP[loot.SkillPrayer] != 13 || p.Events[loot.ActionBury] != 3 {
		t.Fatalf("period xp %v events %v, want Prayer 13 and 3 buried", p.XP, p.Events)
	}
	if v.CostItem != "Bones" || v.CostUnits != 3 {
		t.Fatalf("a burial should cost its bone: %+v", v)
	}
}

// TestMigrationAgainstAStatsFile runs the restore against a real stats file — a COPY of the live
// fleet's run/stats-state.json, never the file itself — and checks every invariant the migration
// promises. Skipped unless RSB0T_LOOT_STATE names one.
//
//	RSB0T_LOOT_STATE=D:/.../stats-state.copy.json go test -run TestMigrationAgainstAStatsFile ./internal/controlpanel/
func TestMigrationAgainstAStatsFile(t *testing.T) {
	path := os.Getenv("RSB0T_LOOT_STATE")
	if path == "" {
		t.Skip("set RSB0T_LOOT_STATE to a COPY of a stats-state.json to run this")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// What the file holds before, read straight from its JSON.
	var before struct {
		Loot lootFile `json:"loot"`
	}
	if err := json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	type tot struct{ events, items, bones, groundBones, leftovers, foreign int64 }
	var was tot
	groundAccounts := 0
	for _, sources := range before.Loot.Accounts {
		for key, rec := range sources {
			was.events += rec.Events
			for _, it := range rec.Items {
				was.items += it.Count
				if loot.IsLegacyGroundPickup(rec.Source) && it.ID == 526 {
					was.groundBones += it.Count
				}
				if (rec.Source.Action == loot.ActionPickpocket || rec.Source.Action == loot.ActionSteal) && loot.IsEatenLeftover(it.ID) {
					was.leftovers += it.Count
				}
				// An event's own item on a source that is not that event (the Lamps and Strange boxes
				// the restore takes back off; the source as it reads once migrated).
				if loot.ForeignEventItem(loot.Migrate(rec.Source), it.ID) {
					was.foreign += it.Count
				}
			}
			if key == "open:ground:-1" {
				groundAccounts++
			}
		}
	}

	// Restore from a scratch copy so the aggregator can never write back to the input.
	scratch := filepath.Join(t.TempDir(), "stats.json")
	if err := os.WriteFile(scratch, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	a := newStatsAggregator(scratch, nil, fixedNow(time.Now()))
	p := a.lootSnapshot().Lifetime

	var now tot
	cards := map[string]bool{}
	var cowBones int64
	for _, s := range p.Sources {
		now.events += s.Events
		for _, it := range s.Items {
			now.items += it.Count
		}
		if loot.IsLegacyGroundPickup(s.Source) {
			t.Fatalf("a Ground pickup survived: %+v", s)
		}
		if s.Source.Kind == loot.KindRandomEvent {
			if canon, _ := loot.CanonicalEventSource(s.Source); canon != s.Source || s.Source.GroupKey() != "randomevent" {
				t.Fatalf("random event %s is on a wrong id or card: %+v", s.Key, s.Source)
			}
		}
		for _, it := range s.Items {
			if loot.ForeignEventItem(s.Source, it.ID) {
				t.Fatalf("%s still carries an event's own item: %+v", s.Key, it)
			}
		}
		if s.Source.Action == loot.ActionPickpocket || s.Source.Action == loot.ActionSteal {
			cards[s.Source.GroupKey()] = true
			for _, it := range s.Items {
				if loot.IsEatenLeftover(it.ID) {
					t.Fatalf("%s still carries eaten leftover %+v", s.Key, it)
				}
			}
		}
		if s.Source.Action == loot.ActionKill && strings.EqualFold(s.Source.Name, "Cow") {
			cowBones += itemCount(s, 526)
		}
	}
	t.Logf("before: %d events, %d items (%d bones on %d Ground records, %d eaten leftovers on thieving records, %d event-own items on other cards)",
		was.events, was.items, was.groundBones, groundAccounts, was.leftovers, was.foreign)
	t.Logf("after: %d events, %d items, %d bones on Cow kills, thieving cards %v", now.events, now.items, cowBones, cards)
	if now.events != was.events {
		t.Fatalf("events changed: %d -> %d", was.events, now.events)
	}
	if now.items != was.items-was.leftovers-was.foreign {
		t.Fatalf("items %d -> %d, want exactly the %d eaten leftovers and the %d event-own items on other cards removed", was.items, now.items, was.leftovers, was.foreign)
	}
	if cowBones < was.groundBones {
		t.Fatalf("cow bones %d, want at least the %d that were on Ground", cowBones, was.groundBones)
	}
	if len(cards) > 1 {
		t.Fatalf("thieving is on %d cards: %v", len(cards), cards)
	}
}
