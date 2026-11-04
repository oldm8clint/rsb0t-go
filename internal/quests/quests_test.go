package quests

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// Pure lookup/table logic only — no live engine, no *gamestate.State packet decode
// involved (that side is proven live by cmd/questtest; see its doc comment).

func TestComponentIDKnownQuest(t *testing.T) {
	id, ok := ComponentID("cook's assistant")
	if !ok {
		t.Fatalf("expected cook's assistant to resolve")
	}
	if id != 7333 {
		t.Fatalf("got component id %d, want 7333 (found live against the local engine's interface archive)", id)
	}
}

func TestComponentIDCaseInsensitive(t *testing.T) {
	id1, ok1 := ComponentID("Cook's Assistant")
	id2, ok2 := ComponentID("COOK'S ASSISTANT")
	id3, ok3 := ComponentID("cook's assistant")
	if !ok1 || !ok2 || !ok3 {
		t.Fatalf("expected all case variants to resolve: %v %v %v", ok1, ok2, ok3)
	}
	if id1 != id2 || id2 != id3 {
		t.Fatalf("expected the same id regardless of case: %d %d %d", id1, id2, id3)
	}
}

func TestComponentIDUnknownQuest(t *testing.T) {
	// The table now lists every 289 quest, Dragon Slayer included; Recipe for Disaster is not
	// in this content build at all.
	if _, ok := ComponentID("Recipe for Disaster"); ok {
		t.Fatalf("Recipe for Disaster is not in the table; expected ok=false")
	}
	if _, ok := ComponentID(""); ok {
		t.Fatalf("empty name should not resolve")
	}
}

func TestKnownListsEveryTableEntry(t *testing.T) {
	known := Known()
	if len(known) != len(componentIDs) {
		t.Fatalf("Known() returned %d names, want %d", len(known), len(componentIDs))
	}
	seen := make(map[string]bool, len(known))
	for _, name := range known {
		if _, ok := componentIDs[name]; !ok {
			t.Fatalf("Known() returned %q which isn't a table key", name)
		}
		seen[name] = true
	}
	for name := range componentIDs {
		if !seen[name] {
			t.Fatalf("Known() missing table entry %q", name)
		}
	}
}

// Classify's synthetic cases mirror general/scripts/quests.rs2's
// send_quest_progress_colour proc exactly (progress==0 -> red, >=complete -> green,
// else yellow), through the wire's lossy 5-bit-per-channel encoding — this checks the
// channel pattern the real IF_SETCOLOUR payload actually produces for content's own
// red_rgb/yellow_rgb/green_rgb (0xFF0000/0xFFFF00/0x00FF00), not just invented values.
func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		c    gamestate.Colour
		want QuestStatus
	}{
		{"pure red (0xFF0000 quantized to 5 bits: 31,0,0)", gamestate.Colour{R: 31, G: 0, B: 0}, NotStarted},
		{"any red-only value counts as not started", gamestate.Colour{R: 5, G: 0, B: 0}, NotStarted},
		{"pure yellow (0xFFFF00 quantized: 31,31,0)", gamestate.Colour{R: 31, G: 31, B: 0}, InProgress},
		{"any red+green (no blue) counts as in progress", gamestate.Colour{R: 3, G: 9, B: 0}, InProgress},
		{"pure green (0x00FF00 quantized: 0,31,0)", gamestate.Colour{R: 0, G: 31, B: 0}, Complete},
		{"any green-only value counts as complete", gamestate.Colour{R: 0, G: 4, B: 0}, Complete},
		{"black (no colour ever set) is unknown", gamestate.Colour{R: 0, G: 0, B: 0}, Unknown},
		{"any blue component is unknown (not a quest-status colour)", gamestate.Colour{R: 10, G: 10, B: 1}, Unknown},
		{"blue only is unknown", gamestate.Colour{R: 0, G: 0, B: 31}, Unknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.c); got != tc.want {
				t.Errorf("Classify(%+v) = %s, want %s", tc.c, got, tc.want)
			}
		})
	}
}

func TestPointsReadsQuestPointsVarp(t *testing.T) {
	state := gamestate.New()
	if got := Points(state); got != 0 {
		t.Fatalf("fresh state should read qp as 0 (never told), got %d", got)
	}
	// Feed a real VARP_SMALL packet for varp 101 (qp) through State.Handle, the same
	// path a live server connection uses — this is the synthetic-data equivalent of
	// what cmd/questtest confirms live, using the actual wire format (varpId:u16 +
	// value:i8) rather than reaching into gamestate's unexported fields.
	w := packet.NewWriter(3)
	w.P2(uint16(QuestPointsVarp))
	w.P1(5)
	state.Handle(serverprot.VARP_SMALL, w.Data[:w.Pos])
	if got := Points(state); got != 5 {
		t.Fatalf("Points() = %d, want 5 after a VARP_SMALL for varp %d", got, QuestPointsVarp)
	}
}

func TestStatusUnknownComponent(t *testing.T) {
	state := gamestate.New()
	status, ok := Status(state, "Recipe for Disaster") // not a 289 quest
	if ok {
		t.Fatalf("expected ok=false for a quest with no component-id entry")
	}
	if status != Unknown {
		t.Fatalf("expected Unknown status, got %s", status)
	}
}

func TestStatusNoColourReceivedYet(t *testing.T) {
	state := gamestate.New()
	status, ok := Status(state, "cook's assistant")
	if !ok {
		t.Fatalf("expected ok=true: this package does track cook's assistant")
	}
	if status != Unknown {
		t.Fatalf("expected Unknown before any IF_SETCOLOUR arrives, got %s", status)
	}
}
