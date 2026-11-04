package bot

import (
	"strings"
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/randomevents"
)

// putPack sets one backpack slot to count of obj id, the way the server's own UPDATE_INV_PARTIAL
// does (ids ride as id+1).
func putPack(st *gamestate.State, slot, id, count int) {
	enc := id + 1
	st.Handle(serverprot.UPDATE_INV_PARTIAL, []byte{byte(lootPackComponent >> 8), byte(lootPackComponent & 0xff), byte(slot), byte(enc >> 8), byte(enc & 0xff), byte(count)})
}

// takeContext is a Context whose Loot sink collects Drops, with a Guardian registered for its
// session so a test can stand in for an event.
func takeContext(t *testing.T) (*Context, *apiSession, *randomevents.Guardian, *[]loot.Drop, *[]string) {
	t.Helper()
	var drops []loot.Drop
	var logs []string
	sess := newAPISession()
	ctx := &Context{Session: sess, Loot: func(d loot.Drop) { drops = append(drops, d) }, Log: func(s string) { logs = append(logs, s) }}
	g := randomevents.NewGuardian(sess, nil)
	t.Cleanup(g.Stop)
	return ctx, sess, g, &drops, &logs
}

// TestATakeARandomEventRanInsideIsNotRecordedOnTheKill is the Cow card's Feather x5,014 and Casket
// x70 (a user request, the live lifetime file): the pack is read before the walk to a bone, a
// Maze pays out while the Guardian holds the account, and the take that follows would have filed the
// whole reward under the cow. The Bones are lost with it, which is the price of not filing a Maze.
func TestATakeARandomEventRanInsideIsNotRecordedOnTheKill(t *testing.T) {
	ctx, sess, g, drops, logs := takeContext(t)
	before := lootPackBefore(ctx)
	end := g.BeginEvent() // the Guardian takes the account for a Maze
	putPack(sess.st, 0, 526, 1)
	putPack(sess.st, 1, 314, 80) // the Maze's feathers
	end()
	ctx.noteKill(81, "Cow", 2605, 3300, 0)
	ctx.NotePickup(before, 526, 2606, 3300, 0)
	if len(*drops) != 1 || (*drops)[0].Events != 1 || len((*drops)[0].Items) != 0 {
		t.Fatalf("recorded %+v, want the kill alone: the take was measured across an event", *drops)
	}
	if len(*logs) != 1 || !strings.Contains((*logs)[0], "a random event ran") {
		t.Fatalf("log = %q, want a line saying why the take was dropped", *logs)
	}
}

// TestATakeWithNoEventInItIsStillRecorded: nothing about the gate costs an ordinary take, and an
// event's own item waiting in the pack is not part of it.
func TestATakeWithNoEventInItIsStillRecorded(t *testing.T) {
	ctx, sess, _, drops, _ := takeContext(t)
	ctx.noteKill(81, "Cow", 2605, 3300, 0)
	before := lootPackBefore(ctx)
	putPack(sess.st, 0, 526, 1)
	putPack(sess.st, 1, 2528, 1) // a Genie's lamp, waiting to be claimed
	putPack(sess.st, 2, 3062, 1) // and a Strange box
	ctx.NotePickup(before, 526, 2606, 3300, 0)
	if len(*drops) != 2 {
		t.Fatalf("recorded %+v, want the kill and the take", *drops)
	}
	take := (*drops)[1]
	if take.Source.Key() != "kill:cow:81" {
		t.Fatalf("the take went to %+v, want the cow", take.Source)
	}
	if len(take.Items) != 1 || take.Items[0].ID != 526 {
		t.Fatalf("take items = %+v, want the Bones alone (the Lamp and the Strange box are an event's)", take.Items)
	}
}

// TestATakeOpenedWhileAnEventWasRunningIsNotRecorded: the reward has not arrived yet when the pack
// is read, so the window that follows is wrong however quiet the rest of it is.
func TestATakeOpenedWhileAnEventWasRunningIsNotRecorded(t *testing.T) {
	ctx, sess, g, drops, _ := takeContext(t)
	ctx.noteKill(81, "Cow", 2605, 3300, 0)
	end := g.BeginEvent()
	before := lootPackBefore(ctx)
	end()
	putPack(sess.st, 0, 526, 1)
	ctx.NotePickup(before, 526, 2606, 3300, 0)
	if len(*drops) != 1 {
		t.Fatalf("recorded %+v, want the kill alone: the take was opened mid-event", *drops)
	}
}

// TestNoRecordingMeansNoReading: a script run without a loot sink reads nothing and records
// nothing, as before.
func TestNoRecordingMeansNoReading(t *testing.T) {
	ctx := &Context{Session: newAPISession()}
	if lootPackBefore(ctx) != nil {
		t.Fatal("a Context with no loot sink took a pack reading")
	}
	ctx.NotePickup(nil, 526, 0, 0, 0) // must not panic
}
