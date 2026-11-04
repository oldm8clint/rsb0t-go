package fleet

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/randomevents"
)

// The Genie (a user request: "genie still doesnt show").
//
// The sequences below are the ones the live fleet logged. On build 73ff4228 there were 366
// "random event: Genie" lines and not one "[loot] random event Genie" line after them: the Genie's
// lamp is handed over, rubbed and gone inside one handling, and as written it could never complete.

const testLamp = 2528

// genieLine is one Guardian progress line for the Genie.
func genieLine(tail string) string { return "random event: Genie — " + tail }

// genieEvents is what the tally holds for the Genie source, and fails the test if it holds any other.
func genieEvents(t *testing.T, tally *loot.Tally) (events int64, lamps int64) {
	t.Helper()
	for _, s := range tally.Snapshot() {
		if s.Source.Name != "Genie" {
			t.Fatalf("a source other than the Genie was recorded: %+v", s)
		}
		if s.Source.Kind != loot.KindRandomEvent || s.Source.ID != 409 || s.Source.Model != loot.ModelNPC || s.Source.Key() != "event:genie:409" {
			t.Fatalf("Genie source = %+v (key %q), want npc 409 macro_geni and not 411, which is the Swarm", s.Source, s.Source.Key())
		}
		for _, it := range s.Items {
			if it.ID != testLamp {
				t.Fatalf("the Genie recorded %+v, want its Lamp alone", it)
			}
			lamps += it.Count
		}
		events += s.Events
	}
	return events, lamps
}

// TestRandomEventLootGenieCountsALampHandedOverAndSpentInOneHandling is acct23, 01:32:40-46: the
// lamp arrives with the dialogue and is gone again before the Guardian says "cleared", so the pack
// reads the same at both ends.
func TestRandomEventLootGenieCountsALampHandedOverAndSpentInOneHandling(t *testing.T) {
	p := &fakePack{c: loot.Counts{testShrimps: 4}}
	r, tally, lines := newTestEventLoot(p)
	r.Note(genieLine("handling"))
	r.Note(genieLine("walking to 3106,3421"))
	p.set(loot.Counts{testShrimps: 4, testLamp: 1}) // "Please take this lamp"
	r.Note(genieLine("talked to the Genie; claiming the lamp"))
	p.set(loot.Counts{testShrimps: 4}) // rubbed, Fishing chosen, confirmed
	r.Note(genieLine("lamp claimed"))
	r.Note(genieLine("cleared"))
	if snap := waitTally(t, tally, 1); len(snap) != 1 {
		t.Fatalf("tally = %+v, want the Genie", snap)
	}
	if events, lamps := genieEvents(t, tally); events != 1 || lamps != 1 {
		t.Fatalf("Genie: %d event(s) and %d lamp(s), want one and one", events, lamps)
	}
	time.Sleep(400 * time.Millisecond) // past the settle: nothing more may arrive
	if events, lamps := genieEvents(t, tally); events != 1 || lamps != 1 {
		t.Fatalf("after the settle: %d event(s), %d lamp(s), want still one and one", events, lamps)
	}
	if len(*lines) != 1 || !strings.Contains((*lines)[0], "Genie: completed, reward 1 x obj 2528") {
		t.Fatalf("log = %q", *lines)
	}
}

// TestRandomEventLootGenieCountsALampLeftInThePack is acct31, 01:33:00-07: the claim did not finish
// ("cleared", no "lamp claimed"), the lamp sat in the pack, and the standalone Lamp event spent it
// afterwards. One lamp was handed over; the Lamp event after it is not a second Genie.
func TestRandomEventLootGenieCountsALampLeftInThePack(t *testing.T) {
	p := &fakePack{c: loot.Counts{}}
	r, tally, _ := newTestEventLoot(p)
	r.Note(genieLine("handling"))
	p.set(loot.Counts{testLamp: 1})
	r.Note(genieLine("talked to the Genie; claiming the lamp"))
	r.Note(genieLine("cleared"))
	r.Note("random event: Lamp — handling") // finishes the Genie's window at once, with the lamp still in the pack
	p.set(loot.Counts{})
	r.Note("random event: Lamp — cleared")
	waitTally(t, tally, 1)
	time.Sleep(400 * time.Millisecond)
	if events, lamps := genieEvents(t, tally); events != 1 || lamps != 1 {
		t.Fatalf("Genie: %d event(s) and %d lamp(s), want one and one", events, lamps)
	}
}

// TestRandomEventLootGenieCountsFromTheClaimWhenThePackReadingLags: the inventory update can land
// a beat after the Guardian's line, so the reading at "claiming the lamp" shows no lamp yet. The
// Guardian's own "lamp claimed" (written only when a lamp it found is gone again) still counts it.
func TestRandomEventLootGenieCountsFromTheClaimWhenThePackReadingLags(t *testing.T) {
	p := &fakePack{c: loot.Counts{}}
	r, tally, _ := newTestEventLoot(p)
	r.Note(genieLine("handling"))
	r.Note(genieLine(randomevents.GenieTalkedLine)) // the pack still reads empty
	r.Note(genieLine(randomevents.LampClaimedLine))
	r.Note(genieLine("cleared"))
	waitTally(t, tally, 1)
	if events, lamps := genieEvents(t, tally); events != 1 || lamps != 1 {
		t.Fatalf("Genie: %d event(s) and %d lamp(s), want one and one", events, lamps)
	}
}

// TestRandomEventLootGenieNeverCountsOneLampTwice: a Genie that is still there after the first
// handling is handled again, and its repeat talk ("I hope you're happy with your wish") gives no
// lamp. Whatever that second window sees of the FIRST lamp -- still in the pack, then spent -- is
// the same lamp.
func TestRandomEventLootGenieNeverCountsOneLampTwice(t *testing.T) {
	// The first lamp was handed over, counted, and spent; a second handling finds nothing.
	p := &fakePack{c: loot.Counts{}}
	r, tally, _ := newTestEventLoot(p)
	r.Note(genieLine("handling"))
	p.set(loot.Counts{testLamp: 1})
	r.Note(genieLine(randomevents.GenieTalkedLine))
	p.set(loot.Counts{})
	r.Note(genieLine(randomevents.LampClaimedLine))
	r.Note(genieLine("still present after handling"))
	waitTally(t, tally, 1)
	r.Note(genieLine("handling"))
	r.Note(genieLine(randomevents.GenieTalkedLine)) // "happy with your wish": accepted, no lamp
	r.Note(genieLine("cleared"))
	time.Sleep(400 * time.Millisecond)
	if events, lamps := genieEvents(t, tally); events != 1 || lamps != 1 {
		t.Fatalf("a repeat talk after a spent lamp: %d event(s), %d lamp(s), want one and one", events, lamps)
	}

	// The first lamp was counted but left unclaimed; the second handling finds it in the pack at
	// its start and spends it ("lamp claimed"). That is not a new lamp.
	p2 := &fakePack{c: loot.Counts{}}
	r2, tally2, _ := newTestEventLoot(p2)
	r2.Note(genieLine("handling"))
	p2.set(loot.Counts{testLamp: 1})
	r2.Note(genieLine(randomevents.GenieTalkedLine))
	r2.Note(genieLine("still present after handling"))
	waitTally(t, tally2, 1)
	r2.Note(genieLine("handling")) // the lamp is in the pack at this window's start
	r2.Note(genieLine(randomevents.GenieTalkedLine))
	p2.set(loot.Counts{})
	r2.Note(genieLine(randomevents.LampClaimedLine))
	r2.Note(genieLine("cleared"))
	time.Sleep(400 * time.Millisecond)
	if events, lamps := genieEvents(t, tally2); events != 1 || lamps != 1 {
		t.Fatalf("a leftover lamp spent by the second window: %d event(s), %d lamp(s), want one and one", events, lamps)
	}
}

// TestRandomEventLootGenieIgnoresLossesAndNeighbours: the lamp is the one thing nothing but the
// Genie hands out, so food eaten in the window does not unsay it (the other events' ambiguous gains
// are voided by a loss; this one is not ambiguous), and other things gained are not the Genie's.
func TestRandomEventLootGenieIgnoresLossesAndNeighbours(t *testing.T) {
	p := &fakePack{c: loot.Counts{testShrimps: 3}}
	r, tally, lines := newTestEventLoot(p)
	r.Note(genieLine("handling"))
	p.set(loot.Counts{testShrimps: 2, testLamp: 1, testCoins: 25})
	r.Note(genieLine(randomevents.GenieTalkedLine))
	p.set(loot.Counts{testShrimps: 2, testCoins: 25})
	r.Note(genieLine(randomevents.LampClaimedLine))
	r.Note(genieLine("cleared"))
	waitTally(t, tally, 1)
	if events, lamps := genieEvents(t, tally); events != 1 || lamps != 1 {
		t.Fatalf("Genie: %d event(s) and %d lamp(s), want one and one (the coins and the missing shrimp are not its)", events, lamps)
	}
	time.Sleep(20 * time.Millisecond)
	if len(*lines) != 1 || strings.Contains((*lines)[0], "nothing recorded") || !strings.Contains((*lines)[0], "completed, reward 1 x obj 2528") {
		t.Fatalf("log = %q, want the Genie logged as completed", *lines)
	}
}

// TestRandomEventLootGenieRecordsNothingWithoutALamp: a Genie handling that never yielded a lamp
// (an NPC that was somebody else's, a reply that was not accepted, a handler error) is no event.
func TestRandomEventLootGenieRecordsNothingWithoutALamp(t *testing.T) {
	p := &fakePack{c: loot.Counts{testShrimps: 1}}
	r, tally, lines := newTestEventLoot(p)
	r.Note(genieLine("handling"))
	r.Note(genieLine("no recognised Genie reply yet; checking other events before retrying"))
	r.Note(genieLine("still present after handling"))
	r.Note(genieLine("handling"))
	r.Note(genieLine("handler error: no approach path"))
	time.Sleep(400 * time.Millisecond)
	if snap := tally.Snapshot(); len(snap) != 0 {
		t.Fatalf("recorded %+v for a Genie that gave no lamp", snap)
	}
	if len(*lines) != 0 {
		t.Fatalf("logged %q for a Genie that gave no lamp", *lines)
	}
}

// TestGenieProgressLinesAreTheGuardiansOwnText: the Guardian writes these two lines and this reads
// them, so the text is pinned against what the live fleet logged (the 5 Oct 2026 fleet.log) and
// against the constants both sides use.
func TestGenieProgressLinesAreTheGuardiansOwnText(t *testing.T) {
	if randomevents.GenieTalkedLine != "talked to the Genie; claiming the lamp" || randomevents.LampClaimedLine != "lamp claimed" {
		t.Fatalf("the Genie's progress lines changed (%q, %q): the live log, cmd/crowdgenietest and this reader all use the old text",
			randomevents.GenieTalkedLine, randomevents.LampClaimedLine)
	}
	name, tail, ok := splitEventLine("random event: Genie — lamp claimed")
	if !ok || name != "Genie" || tail != randomevents.LampClaimedLine {
		t.Fatalf("splitEventLine = %q, %q, %v", name, tail, ok)
	}
}
