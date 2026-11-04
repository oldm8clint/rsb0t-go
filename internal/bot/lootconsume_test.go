package bot

import (
	"testing"

	"rsb0t-go/internal/gamestate"
)

func foodGone(id int32, had, now int32) gamestate.Event {
	item := id
	if now == 0 {
		item = 0
	}
	return gamestate.Event{Kind: gamestate.EventInventoryChanged, Comp: lootPackComponent, PreviousID: id, PreviousCount: had, ItemID: item, Count: now}
}

func eatLine(text string) gamestate.Event {
	return gamestate.Event{Kind: gamestate.EventChatMessage, ChatType: gamestate.ChatGame, Text: text}
}

const (
	testShark = 385
	testCake  = 1891
	testPart  = 1893
)

func newTestConsume(got *[]int) *consumeWatch {
	return newConsumeWatch(func(id int) bool { return id == testShark || id == testCake || id == testPart }, func(id int) { *got = append(*got, id) })
}

// TestConsumeWatchPairsAnEdibleLeavingWithTheEatLine: a shark leaving its slot as the line arrives, in
// either order, is one shark eaten; a cake swapping to its two-thirds form is a cake eaten.
func TestConsumeWatchPairsAnEdibleLeavingWithTheEatLine(t *testing.T) {
	var got []int
	w := newTestConsume(&got)
	w.onEvent(tickEv(1))
	w.onEvent(foodGone(testShark, 1, 0))
	w.onEvent(eatLine("You eat the shark."))
	w.onEvent(tickEv(2))
	w.onEvent(eatLine("You eat part of the cake."))
	w.onEvent(gamestate.Event{Kind: gamestate.EventInventoryChanged, Comp: lootPackComponent, PreviousID: testCake, PreviousCount: 1, ItemID: testPart, Count: 1})
	if len(got) != 2 || got[0] != testShark || got[1] != testCake {
		t.Fatalf("recorded %v, want a shark and a cake", got)
	}
}

// TestConsumeWatchIgnoresWhatIsNotEating: banked, dropped or lost on a death (no line), a line with no
// edible leaving (a swap), and anything while a bank is open.
func TestConsumeWatchIgnoresWhatIsNotEating(t *testing.T) {
	var got []int
	w := newTestConsume(&got)
	w.onEvent(tickEv(1))
	w.onEvent(foodGone(testShark, 1, 0)) // deposited, dropped or died with: no line
	w.onEvent(tickEv(5))
	w.onEvent(eatLine("You eat the shark.")) // nothing left the pack near it
	w.onEvent(tickEv(9))
	open := true
	w.modal = func() bool { return open }
	w.onEvent(foodGone(testShark, 1, 0))
	w.onEvent(eatLine("You eat the shark."))
	open = false
	w.onEvent(tickEv(12))
	w.onEvent(eatLine("You drink the wine.")) // a drink of something no edible left for
	w.onEvent(gamestate.Event{Kind: gamestate.EventChatMessage, ChatType: gamestate.ChatPublic, Text: "You eat the shark."})
	w.onEvent(foodGone(testShark, 1, 0))
	// The public line was another player talking; the earlier drink line paired with this shark.
	if len(got) != 1 {
		t.Fatalf("recorded %v", got)
	}
}

// TestConsumeWatchDoesNotPairAcrossTicks: a loss and a line a long way apart are two non-events.
func TestConsumeWatchDoesNotPairAcrossTicks(t *testing.T) {
	var got []int
	w := newTestConsume(&got)
	w.onEvent(tickEv(1))
	w.onEvent(foodGone(testShark, 1, 0))
	w.onEvent(tickEv(4))
	w.onEvent(eatLine("You eat the shark."))
	if len(got) != 0 {
		t.Fatalf("recorded %v for a loss and a line three ticks apart", got)
	}
}

func TestWatchConsumptionNeedsASinkAndAConfig(t *testing.T) {
	(&Context{}).watchConsumption() // must not panic with no session, sink or config
}
