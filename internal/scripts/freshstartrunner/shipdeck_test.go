package freshstartrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// EVERY SHIP THAT LANDS AN ACCOUNT ON A DECK NEEDS A WAY OFF THAT DECK.
//
// SailLeg carries one hardcoded disembark block per deck, because the offline collision bake
// treats each deck as an isolated pocket and the generic stairNames search cannot be trusted on
// fixtures whose display names repeat in both directions. Three of the four were there; the
// Brimhaven one, where Captain Barnaby's Ardougne -> Brimhaven ship sets a passenger down, was
// not, and had never been.
//
// The live cost, 2026-09-22, run/logs/fleet-20260922-101543-232-16204.log, the 110-account
// ScarabSwarm cohort: 707 lines of "Ardougne->Brimhaven ship: arrived at 2775,3234,1" and ZERO
// lines of "Brimhaven->Ardougne ship" — nobody ever came back, because coming back starts with
// getting off the deck. Reading the running panel at the same time found nine accounts standing
// on exactly 2775,3234,1, each repeating "boat leg: Brimhaven ship -> Ardougne -> destination".
//
// The tiles are checked against the webwalk transport graph's own gangplank edges rather than
// retyped, because the graph edge is the thing that has actually been proven live (acct83,
// 15:54:42-45: the hop ran and logged "Cross Gangplank at 2774,3234 ok"). If someone corrects a
// deck in one place and not the other, this fails rather than stranding a cohort again.
func TestEveryShipDeckHasAGangplankMatchingTheTransportGraph(t *testing.T) {
	graph, err := webwalk.Default()
	if err != nil {
		t.Fatalf("loading the webwalk graph: %v", err)
	}

	for _, c := range []struct {
		name  string
		deck  tile.Tile
		loc   tile.Tile
		locID int
		op    string
	}{
		{"Brimhaven", freshstart.BrimhavenShipDeck, freshstart.BrimhavenGangplank, freshstart.BrimhavenGangplankID, freshstart.BrimhavenGangplankOp},
		{"Ardougne", freshstart.ArdougneArrival, freshstart.ArdougneGangplank, freshstart.ArdougneGangplankID, freshstart.ArdougneGangplankOp},
		{"Karamja", freshstart.KaramjaShipDeck, freshstart.KaramjaGangplank, freshstart.KaramjaGangplankID, freshstart.KaramjaGangplankOp},
	} {
		var found *webwalk.Edge
		for i := range graph.Edges {
			e := &graph.Edges[i]
			if e.LocID == c.locID && e.Action == c.op {
				found = e
				break
			}
		}
		if found == nil {
			t.Errorf("%s: the transport graph has no %q edge for loc %d, so SailLeg's hardcoded "+
				"disembark is not backed by anything that has been walked live", c.name, c.op, c.locID)
			continue
		}
		if found.LocX != c.loc.X || found.LocZ != c.loc.Z {
			t.Errorf("%s: SailLeg crosses loc %d at %d,%d but the graph edge puts it at %d,%d — one of "+
				"the two is wrong and the account will get \"I can't reach that!\"",
				c.name, c.locID, c.loc.X, c.loc.Z, found.LocX, found.LocZ)
		}
		// The deck tile is where the ship lands and where SailLeg walks to at radius 0 before
		// crossing; the graph names that same tile as the crossing's From.
		if c.name == "Brimhaven" && (found.From.X != c.deck.X || found.From.Z != c.deck.Z || found.From.Level != c.deck.Level) {
			t.Errorf("%s: SailLeg holds the account at %d,%d,%d but the graph crosses from %d,%d,%d",
				c.name, c.deck.X, c.deck.Z, c.deck.Level, found.From.X, found.From.Z, found.From.Level)
		}
		if found.To.Level >= found.From.Level {
			t.Errorf("%s: the gangplank edge goes from level %d to level %d — SailLeg confirms a "+
				"crossing by the level going DOWN, so this would never be believed",
				c.name, found.From.Level, found.To.Level)
		}
	}
}

// SailLeg's Brimhaven disembark block is gated on leg == LegDestination, the same way the
// Ardougne and Karamja blocks are gated on their own legs. That gate has to actually match for an
// account standing on the deck, or the block is dead code — which is exactly how the Karamja
// block was found broken on 2026-09-15 (three accounts sat on that deck and none of them ever
// logged its lines, because `leg` was stale).
func TestTheBrimhavenDeckIsOnTheDestinationLeg(t *testing.T) {
	if got := freshstart.TravelLeg(freshstart.BrimhavenShipDeck); got != freshstart.LegDestination {
		t.Errorf("TravelLeg(Brimhaven ship deck %v) = %q, want %q — SailLeg's disembark block is "+
			"gated on that leg and would never run", freshstart.BrimhavenShipDeck, got, freshstart.LegDestination)
	}
	// And the account really is between the ships there, which is what makes the Ardougne
	// crossing the next thing it needs rather than a walk.
	if !freshstart.OnKaramjaSide(freshstart.BrimhavenShipDeck) {
		t.Errorf("OnKaramjaSide(%v) = false; want true — the Brimhaven deck is on Karamja, and "+
			"SailLeg's skipBoard rule reads this to decide whether to look for a boarding NPC",
			freshstart.BrimhavenShipDeck)
	}
	// It must not be confused with the Ardougne deck: both are level-1 dock tiles, and picking
	// the wrong gangplank id at either one is the "two ids, one display name, opposite
	// directions" trap the hardcoded handling exists to avoid.
	if freshstart.BrimhavenGangplankID == freshstart.ArdougneGangplankID {
		t.Error("the Brimhaven and Ardougne gangplanks share a loc id; SailLeg addresses them by id " +
			"precisely so it cannot cross the wrong one")
	}
}
