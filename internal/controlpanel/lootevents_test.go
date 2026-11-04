package controlpanel

import (
	"encoding/json"
	"strings"
	"testing"

	"rsb0t-go/internal/loot"
)

// liveEventsFile is a stats file in the shape build 73ff4228 really persisted on 5 Oct 2026,
// written by hand from run/stats-state.json rather than built with today's constructors, which
// cannot produce it:
//
//   - the Genie recorded under npc 411 (macro_SWARM; the panel drew it as a Swarm) by "madness", and
//     the Mysterious Old Man under 2344 (no npc at all) by two ScarabSwarm accounts, beside the
//     Old Man's right record (410);
//   - Lamps and Strange boxes on a Cow, a Chicken and a Casket card, picked up by whatever window
//     was open when an event paid out, with a Maze's Feathers on the Cow beside them;
//   - history and ticker entries for the wrong-id keys.
const liveEventsFile = `{
  "trackingSince": 1726700000000,
  "loot": {
    "accounts": {
      "madness": {
        "event:genie:411": {"source": {"kind": "randomevent", "action": "event", "name": "Genie", "id": 411, "model": "npc"},
          "events": 0, "items": [{"id": 2528, "name": "Lamp", "count": 1, "value": 1}], "first": 1726700100000, "last": 1726700900000}
      },
      "Acct219": {
        "event:mysterious old man:2344": {"source": {"kind": "randomevent", "action": "event", "name": "Mysterious Old Man", "id": 2344, "model": "npc"},
          "events": 0, "items": [{"id": 405, "name": "Casket", "count": 1, "value": 1}], "first": 1726700050000, "last": 1726700950000},
        "event:mysterious old man:410": {"source": {"kind": "randomevent", "action": "event", "name": "Mysterious Old Man", "id": 410, "model": "npc"},
          "events": 100, "items": [{"id": 405, "name": "Casket", "count": 5, "value": 1}, {"id": 995, "name": "Coins", "count": 2400, "value": 1}], "first": 1726700200000, "last": 1726700800000},
        "kill:cow:81": {"source": {"kind": "npc", "action": "kill", "name": "Cow", "id": 81, "level": 2},
          "events": 50, "items": [{"id": 314, "name": "Feather", "count": 80, "value": 2}, {"id": 526, "name": "Bones", "count": 40, "value": 1}, {"id": 2528, "name": "Lamp", "count": 3, "value": 1}, {"id": 3062, "name": "Strange box", "count": 2, "value": 1}], "first": 1726700300000, "last": 1726700700000}
      },
      "Acct222": {
        "event:mysterious old man:2344": {"source": {"kind": "randomevent", "action": "event", "name": "Mysterious Old Man", "id": 2344, "model": "npc"},
          "events": 0, "items": [{"id": 405, "name": "Casket", "count": 1, "value": 1}], "first": 1726700060000, "last": 1726700960000}
      },
      "fisher1": {
        "open:casket:405": {"source": {"kind": "openable", "action": "open", "name": "Casket", "id": 405},
          "events": 4, "items": [{"id": 995, "name": "Coins", "count": 10, "value": 1}, {"id": 2528, "name": "Lamp", "count": 1, "value": 1}], "first": 1726700100000, "last": 1726700200000},
        "kill:chicken:41": {"source": {"kind": "npc", "action": "kill", "name": "Chicken", "id": 41, "level": 1},
          "events": 0, "items": [{"id": 2528, "name": "Lamp", "count": 1, "value": 1}], "first": 1726700100000, "last": 1726700200000},
        "event:strange box:3062": {"source": {"kind": "randomevent", "action": "event", "name": "Strange box", "id": 3062, "model": "obj"},
          "events": 7, "items": [{"id": 995, "name": "Coins", "count": 700, "value": 1}, {"id": 3062, "name": "Strange box", "count": 1, "value": 1}], "first": 1726700100000, "last": 1726700200000}
      }
    },
    "recent": [
      {"at": 1726700900000, "account": "madness", "source": {"kind": "randomevent", "action": "event", "name": "Genie", "id": 411, "model": "npc"}, "events": 0, "items": [{"id": 2528, "name": "Lamp", "count": 1}]},
      {"at": 1726700800000, "account": "Acct219", "source": {"kind": "npc", "action": "kill", "name": "Cow", "id": 81, "level": 2}, "events": 0, "items": [{"id": 526, "name": "Bones", "count": 1}, {"id": 2528, "name": "Lamp", "count": 1}]}
    ],
    "history": [
      {"start": 1726700400000, "items": {"2528": 5}, "events": {"event:genie:411": 0, "event:mysterious old man:2344": 3, "event:mysterious old man:410": 2, "kill:cow:81": 50}}
    ]
  }
}`

func genieSource() loot.Source {
	r, _ := loot.RandomEventReward("Genie")
	return r.Source()
}

// TestLegacyEventRecordsRestoreOntoTheRightIds is "genie was also showing as a swarm last time it
// displayed": the record under npc 411 is the Genie, and comes back as npc 409.
func TestLegacyEventRecordsRestoreOntoTheRightIds(t *testing.T) {
	a := restoreLootFile(t, liveEventsFile)
	body := a.lootSnapshot()
	p := body.Lifetime

	genie, ok := sourceView(p, genieSource())
	if !ok {
		t.Fatalf("no Genie at its own id in %+v", p.Sources)
	}
	if genie.Source.ID != 409 || genie.Key != "event:genie:409" || genie.Source.Model != loot.ModelNPC {
		t.Fatalf("Genie = %+v (key %q), want npc 409, which is macro_geni, and not the Swarm", genie.Source, genie.Key)
	}
	if genie.Events != 0 || itemCount(genie, 2528) != 1 {
		t.Fatalf("Genie = %d events, %d lamps; want the 0 events and the 1 Lamp the file held, still shown", genie.Events, itemCount(genie, 2528))
	}
	for _, s := range p.Sources {
		if s.Source.ID == 411 || s.Source.ID == 2344 || strings.HasSuffix(s.Key, ":411") || strings.HasSuffix(s.Key, ":2344") {
			t.Fatalf("a wrong-id record survived the restore: %+v", s)
		}
	}
	// The Old Man's stray records (two accounts) merge onto the right one: one source, not two.
	old, ok := sourceView(p, loot.Source{Kind: loot.KindRandomEvent, Action: loot.ActionEvent, Name: "Mysterious Old Man", ID: 410, Model: loot.ModelNPC})
	if !ok {
		t.Fatal("no Mysterious Old Man at id 410")
	}
	if old.Events != 100 || itemCount(old, 405) != 5+1+1 || itemCount(old, 995) != 2400 {
		t.Fatalf("Old Man = %d events, %d caskets, %d coins; want 100, 7 (5 + the two strays), 2400", old.Events, itemCount(old, 405), itemCount(old, 995))
	}
	if old.Accounts != 2 {
		t.Fatalf("Old Man accounts = %d, want the two accounts that recorded one", old.Accounts)
	}
	if old.First != 1726700050000 || old.Last != 1726700960000 {
		t.Fatalf("Old Man span %d..%d, want it widened to every record's", old.First, old.Last)
	}
	// All random events are ONE card: the account figure is the union over the whole card.
	events := 0
	for _, s := range p.Sources {
		if s.Source.Kind != loot.KindRandomEvent {
			continue
		}
		events++
		if s.Source.GroupKey() != "randomevent" {
			t.Fatalf("%s is on card %q, want every random event on randomevent", s.Key, s.Source.GroupKey())
		}
		if s.GroupAccounts != 4 {
			t.Fatalf("%s group accounts = %d, want 4 (madness, Acct219, Acct222, fisher1)", s.Key, s.GroupAccounts)
		}
	}
	if events != 3 {
		t.Fatalf("%d random-event sources, want the Genie, the Old Man and the Strange box", events)
	}
	// The history bucket and the ticker follow their records.
	for _, h := range body.History {
		if _, bad := h.Events["event:genie:411"]; bad {
			t.Fatalf("history still counts the old Genie key: %v", h.Events)
		}
		if _, bad := h.Events["event:mysterious old man:2344"]; bad || h.Events["event:mysterious old man:410"] != 2+3 {
			t.Fatalf("history events = %v, want the stray Old Man count merged into key 410 (5)", h.Events)
		}
	}
	if len(body.Recent) == 0 || body.Recent[0].Source.Key() != "event:genie:409" || body.Recent[0].Source.ID != 409 {
		t.Fatalf("ticker = %+v, want the Genie entry on npc 409", body.Recent)
	}
}

// TestLegacyEventItemsComeOffTheWrongCards is "currently lamps are showing up in wrong sections":
// a Lamp or a Strange box is only ever the Genie's or the Strange box event's, and the rest of what
// a card holds is left alone.
func TestLegacyEventItemsComeOffTheWrongCards(t *testing.T) {
	a := restoreLootFile(t, liveEventsFile)
	body := a.lootSnapshot()
	p := body.Lifetime

	cow, ok := sourceView(p, loot.LegacyGroundKill)
	if !ok {
		t.Fatal("the Cow card disappeared")
	}
	if itemCount(cow, 2528) != 0 || itemCount(cow, 3062) != 0 {
		t.Fatalf("the Cow still carries %d Lamps and %d Strange boxes", itemCount(cow, 2528), itemCount(cow, 3062))
	}
	// Its other contents are untouched. A Maze's Feathers stay: no rule can tell them from a
	// chicken's without deleting real loot, and the take windows stop adding more.
	if cow.Events != 50 || itemCount(cow, 526) != 40 || itemCount(cow, 314) != 80 {
		t.Fatalf("Cow = %d kills, %d bones, %d feathers; want 50, 40 and 80 left as they were", cow.Events, itemCount(cow, 526), itemCount(cow, 314))
	}
	casket, ok := sourceView(p, loot.Openable("Casket", 405))
	if !ok || itemCount(casket, 2528) != 0 || itemCount(casket, 995) != 10 || casket.Events != 4 {
		t.Fatalf("Casket = %+v (%v), want its 4 opens and 10 coins without the Lamp", casket, ok)
	}
	// A record the strip left with nothing at all is gone, not an empty card.
	for _, s := range p.Sources {
		if s.Key == "kill:chicken:41" {
			t.Fatalf("an empty Chicken record survived: %+v", s)
		}
	}
	// The events keep their own items: the Genie's Lamp, the Strange box event's box.
	if g, _ := sourceView(p, genieSource()); itemCount(g, 2528) != 1 {
		t.Fatalf("the Genie lost its own Lamp: %+v", g.Items)
	}
	box, _ := sourceView(p, loot.Source{Kind: loot.KindRandomEvent, Action: loot.ActionEvent, Name: "Strange box", ID: 3062, Model: loot.ModelObj})
	if itemCount(box, 3062) != 1 || itemCount(box, 995) != 700 {
		t.Fatalf("the Strange box event = %+v, want its own box and coins kept", box.Items)
	}
	// The ticker's Cow entry lost the Lamp and kept the Bones.
	for _, r := range body.Recent {
		if r.Source.Key() != "kill:cow:81" {
			continue
		}
		if len(r.Items) != 1 || r.Items[0].ID != 526 {
			t.Fatalf("ticker Cow entry = %+v, want the Bones alone", r.Items)
		}
	}
}

// TestLegacyEventRestoreIsIdempotentAcrossASave: the corrected state is written back and loading
// THAT changes nothing further, and never writes a wrong-id key again.
func TestLegacyEventRestoreIsIdempotentAcrossASave(t *testing.T) {
	a := restoreLootFile(t, liveEventsFile)
	first := a.lootSnapshot().Lifetime
	saved, err := json.Marshal(struct {
		TrackingSince int64     `json:"trackingSince"`
		Loot          *lootFile `json:"loot"`
	}{1726700000000, a.loot.persisted()})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`"event:genie:411"`, `"event:mysterious old man:2344"`, `"id":411`, `"id":2344`} {
		if strings.Contains(string(saved), bad) {
			t.Fatalf("the saved file still carries %s: %s", bad, saved)
		}
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
		for j := range f.Items {
			if f.Items[j].ID != s.Items[j].ID || f.Items[j].Count != s.Items[j].Count {
				t.Fatalf("%s item %d changed across a save: %+v vs %+v", f.Key, j, f.Items[j], s.Items[j])
			}
		}
	}
}

// TestLiveGenieRecordsOnTheNewId: what the fleet records now (loot.EventReward.Source) lands on the
// same source the restore folded the old record onto, and a live Totals arriving under a stale id
// is rewritten the same way (loot.Migrate in record).
func TestLiveGenieRecordsOnTheNewId(t *testing.T) {
	a := restoreLootFile(t, liveEventsFile)
	now := a.now()
	a.loot.record(lootReading("acct23", 1, loot.Totals{Source: genieSource(), Events: 2, Items: []loot.Item{{ID: 2528, Name: "Lamp", Count: 2, Value: 1}}}), now)
	stale := loot.Source{Kind: loot.KindRandomEvent, Action: loot.ActionEvent, Name: "Genie", ID: 411, Model: loot.ModelNPC}
	a.loot.record(lootReading("acct24", 1, loot.Totals{Source: stale, Events: 1, Items: []loot.Item{{ID: 2528, Name: "Lamp", Count: 1, Value: 1}}}), now)
	g, ok := sourceView(a.lootSnapshot().Lifetime, genieSource())
	if !ok || g.Events != 3 || itemCount(g, 2528) != 1+2+1 {
		t.Fatalf("Genie = %+v (%v), want 3 events and 4 lamps (the restored 1, the live 2, the stale-id 1)", g, ok)
	}
	if g.Accounts != 3 || g.GroupAccounts < 3 {
		t.Fatalf("Genie accounts = %d (group %d), want madness, acct23 and acct24", g.Accounts, g.GroupAccounts)
	}
	if p := a.lootSnapshot().Lifetime; p.Events[loot.ActionEvent] != 0+100+7+3 {
		t.Fatalf("the header counts %d random events, want 110 (the Old Man's 100, the box's 7, the Genie's 3)", p.Events[loot.ActionEvent])
	}
}
