package fleetrunner

import (
	"strings"
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/randomevents"
)

// TestATakeARandomEventRanInsideIsNotFiledOnTheCow is the Cow card's Feather x5,014, Casket x70,
// Mithril ore x59, Coal x265 and Strange fruit x17 (live lifetime file, 5 Oct 2026): ScarabSwarm
// reads the pack before it walks to a bone, the Guardian takes the account for a Maze, and the take
// that follows measured the whole reward as the cow's drop. Replayed here exactly as Stage 3 and
// Stage 5 run it, with a real Guardian standing in for the event; on the old code the Bones, the
// Feathers and the Coins all land on kill:cow:81.
func TestATakeARandomEventRanInsideIsNotFiledOnTheCow(t *testing.T) {
	d, s, got := testDeps()
	var logs []string
	d.Log = func(line string) { logs = append(logs, line) }
	g := randomevents.NewGuardian(s, nil)
	defer g.Stop()

	NoteKill(d, 81, 2605, 3300, 0)
	before := LootBefore(d)
	end := g.BeginEvent() // the Guardian takes the account for a Maze
	s.put(0, 526, 1)      // the bones
	s.put(1, 314, 80)     // the Maze's feathers
	s.put(2, 995, 400)    // and its coins
	end()
	RecordGroundTake(d, before, gamestate.GroundItem{ID: 526, X: 2606, Z: 3300, Level: 0})

	if len(*got) != 1 || (*got)[0].Events != 1 || len((*got)[0].Items) != 0 {
		t.Fatalf("recorded %+v, want the kill alone: the take was measured across an event", *got)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "a random event ran") {
		t.Fatalf("log = %q, want a line saying why the take was dropped", logs)
	}
}

// TestATakeWithNoEventInItIsRecordedWithoutAnEventsOwnItems: the gate costs an ordinary take
// nothing, and a Lamp or a Strange box waiting in the pack is never part of one.
func TestATakeWithNoEventInItIsRecordedWithoutAnEventsOwnItems(t *testing.T) {
	d, s, got := testDeps()
	g := randomevents.NewGuardian(s, nil)
	defer g.Stop()
	NoteKill(d, 81, 2605, 3300, 0)
	before := LootBefore(d)
	s.put(0, 526, 1)
	s.put(1, 2528, 1) // a Genie's lamp, waiting to be claimed
	s.put(2, 3062, 1) // and a Strange box
	RecordGroundTake(d, before, gamestate.GroundItem{ID: 526, X: 2606, Z: 3300, Level: 0})
	if len(*got) != 2 {
		t.Fatalf("recorded %+v, want the kill and the take", *got)
	}
	take := (*got)[1]
	if len(take.Items) != 1 || take.Items[0].ID != 526 || take.Source != (*got)[0].Source {
		t.Fatalf("take = %+v, want the Bones on the cow and no Lamp or Strange box", take)
	}
	// A script run without a loot sink takes no reading at all.
	if LootBefore(&Deps{Session: s}) != nil {
		t.Fatal("a Deps with no loot sink took a pack reading")
	}
}
