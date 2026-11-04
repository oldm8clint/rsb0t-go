package controlpanel

import (
	"testing"

	"rsb0t-go/internal/loot"
)

// pickupFoldFile is what the live build (46a4ace1) persisted: an Ogre account that took a Death rune
// pile and some Big bones as pickups of their own, and an account with no kill at all.
const pickupFoldFile = `{
  "trackingSince": 1726700000000,
  "loot": {
    "accounts": {
      "mage1": {
        "kill:ogre:114": {"source": {"kind": "npc", "action": "kill", "name": "Ogre", "id": 114, "level": 53},
          "events": 20, "items": [{"id": 532, "name": "Big bones", "count": 5, "value": 1}], "first": 1726700300000, "last": 1726700500000},
        "kill:cow:81": {"source": {"kind": "npc", "action": "kill", "name": "Cow", "id": 81, "level": 2},
          "events": 3, "items": [], "first": 1726700300000, "last": 1726700500000},
        "take:death rune:560": {"source": {"kind": "pickup", "action": "take", "name": "Death rune", "id": 560, "model": "obj"},
          "events": 4, "items": [{"id": 560, "name": "Death rune", "count": 40, "value": 200}], "first": 1726700250000, "last": 1726700600000},
        "take:big bones:532": {"source": {"kind": "pickup", "action": "take", "name": "Big bones", "id": 532, "model": "obj"},
          "events": 2, "items": [{"id": 532, "name": "Big bones", "count": 2, "value": 1}], "first": 1726700260000, "last": 1726700610000},
        "take:knife:946": {"source": {"kind": "pickup", "action": "take", "name": "Knife", "id": 946, "model": "obj"},
          "events": 1, "items": [{"id": 946, "name": "Knife", "count": 1, "value": 1}], "first": 1726700260000, "last": 1726700610000}
      },
      "mage2": {
        "take:death rune:560": {"source": {"kind": "pickup", "action": "take", "name": "Death rune", "id": 560, "model": "obj"},
          "events": 1, "items": [{"id": 560, "name": "Death rune", "count": 5, "value": 200}], "first": 1726700250000, "last": 1726700600000}
      }
    },
    "recent": [
      {"at": 1726700600000, "account": "mage1", "source": {"kind": "pickup", "action": "take", "name": "Death rune", "id": 560, "model": "obj"}, "events": 1, "items": [{"id": 560, "name": "Death rune", "count": 5}]}
    ],
    "history": [
      {"start": 1726700400000, "items": {"560": 5}, "events": {"take:death rune:560": 1, "kill:ogre:114": 6}}
    ]
  }
}`

// TestRuneAndBonePickupsRestoreUnderTheKillThatLeftThem: the Death rune and Big bones pickup cards of
// an account that fights move under its busiest kill (items added, no kills added); a Knife is still a
// pickup; an account with no kill keeps its record.
func TestRuneAndBonePickupsRestoreUnderTheKillThatLeftThem(t *testing.T) {
	a := restoreLootFile(t, pickupFoldFile)
	snap := a.lootSnapshot()
	p := snap.Lifetime

	var ogre *LootSourceView
	pickups := map[string]int64{}
	for i, s := range p.Sources {
		if s.Source.Kind == loot.KindPickup {
			pickups[s.Source.Name] += s.Events
		}
		if s.Source.Name == "Ogre" {
			ogre = &p.Sources[i]
		}
	}
	if ogre == nil {
		t.Fatal("the Ogre kill vanished")
	}
	if ogre.Events != 20 {
		t.Fatalf("ogre kills = %d, want 20: a pickup is not a kill", ogre.Events)
	}
	counts := map[int]int64{}
	for _, it := range ogre.Items {
		counts[it.ID] = it.Count
	}
	if counts[560] != 40 || counts[532] != 7 {
		t.Fatalf("ogre items = %v, want the 40 Death runes and 2 more Big bones folded in", counts)
	}
	if pickups["Big bones"] != 0 {
		t.Fatalf("a Big bones pickup card is still there: %v", pickups)
	}
	if pickups["Knife"] != 1 {
		t.Fatalf("the Knife pickup should stay a pickup: %v", pickups)
	}
	// mage2 has no kill: its rune pile has nowhere to go and stays rather than being lost.
	if pickups["Death rune"] != 1 {
		t.Fatalf("death rune pickups = %d, want only the kill-less account's", pickups["Death rune"])
	}
	for _, r := range snap.Recent {
		if r.Source.Kind == loot.KindPickup && r.Account == "mage1" {
			t.Fatalf("the recent ticker still shows %+v as a pickup", r.Source)
		}
	}
	for _, h := range snap.History {
		if _, ok := h.Events["take:death rune:560"]; ok {
			t.Fatalf("the history still counts the pickup event: %v", h.Events)
		}
	}
	// Idempotent: a second pass of the fold finds nothing to do.
	a.loot.mu.Lock()
	keys, _ := foldRunePickups(a.loot.lifetime)
	a.loot.mu.Unlock()
	if len(keys) != 0 {
		t.Fatalf("second fold moved %v", keys)
	}
}
