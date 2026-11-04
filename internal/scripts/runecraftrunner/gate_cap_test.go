package runecraftrunner

// The crafter's room (OfferOptions.Cap): a carrier offers what the crafter says he takes, not an Offer-All he declines.
//
// Live 16 Oct 2026: a Strange fruit left in the crafter's pack by a random event made his trade 24 essence, and every Offer-All of 25 was
// declined ("offered 25 essence and only 24 slots are free"): 121 times in four hours, six carriers' shifts ended within 70 seconds.

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/runecraftplan"
)

// A carrier holding more than the crafter takes offers exactly what he takes, with Offer-X, and keeps the rest; one holding no more, or
// offered no cap, or a room too small to be a trade, offers everything.
func TestTradeWithCrafterOffersOnlyWhatTheCrafterTakes(t *testing.T) {
	for _, c := range []struct {
		name               string
		held, cap, offered int
	}{
		{"he takes 24 of 25", 25, 24, 24},
		{"he takes 22 of 25 (three strays)", 25, 22, 22},
		{"he takes the least load", 25, runecraftplan.MinLoad, runecraftplan.MinLoad},
		{"he takes a full load", 25, 25, 25},
		{"he takes more than the carrier holds", 25, 26, 25},
		{"he did not say", 25, 0, 25},
		{"a room under the least load is no trade: ignored", 25, runecraftplan.MinLoad - 1, 25},
		{"the carrier holds less than he takes", 23, 24, 23},
	} {
		sim, d := carrierSession(t, c.held, "main")
		var last []actionsim.Stack
		var opened atomic.Bool
		partner := &actionsim.Trade{Partner: "main", AcceptOffer: true, AcceptConfirm: true,
			OnMine: func(m []actionsim.Stack) { last = append([]actionsim.Stack(nil), m...) }}
		sim.OnSend = func(b []byte) {
			if isTradeRequest(b) && opened.CompareAndSwap(false, true) {
				sim.OpenTrade(partner)
			}
		}
		res := TradeWithCrafter(d, OfferOptions{Crafter: "main", Mode: OfferEssence, Timeout: 5 * time.Second, Cap: func() int { return c.cap }})
		if !res.Done || res.Moved != c.offered {
			t.Errorf("%s: %+v", c.name, res)
			continue
		}
		if got := countOf(last, essID); got != c.offered {
			t.Errorf("%s: %d essence went on the screen, want %d", c.name, got, c.offered)
		}
		if left := countOf(sim.Pack(), essID); left != c.held-c.offered || res.Kept != c.held-c.offered {
			t.Errorf("%s: %d essence kept in the pack (result says %d), want %d", c.name, left, res.Kept, c.held-c.offered)
		}
		if got := countOf(sim.Pack(), runecraftplan.NotedEssenceID); got != 2900 {
			t.Errorf("%s: the notes are the kit and stay: %d", c.name, got)
		}
		if desc := res.Describe(OfferEssence, "main"); c.held > c.offered && !strings.Contains(desc, "kept") {
			t.Errorf("%s: the log line must say what was kept: %s", c.name, desc)
		}
	}
}

// The cap is read when the offer is made, not when the trade is asked for: a message that changed while the carrier waited for the screen
// is the one that counts.
func TestTradeWithCrafterReadsTheCapWhenTheOfferIsMade(t *testing.T) {
	sim, d := carrierSession(t, 25, "main")
	var cap atomic.Int32
	cap.Store(25)
	var last []actionsim.Stack
	partner := &actionsim.Trade{Partner: "main", AcceptOffer: true, AcceptConfirm: true, OnMine: func(m []actionsim.Stack) { last = append([]actionsim.Stack(nil), m...) }}
	var opened atomic.Bool
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) && opened.CompareAndSwap(false, true) {
			cap.Store(24) // the crafter's pack changed while the screen was opening
			sim.OpenTrade(partner)
		}
	}
	res := TradeWithCrafter(d, OfferOptions{Crafter: "main", Mode: OfferEssence, Timeout: 5 * time.Second, Cap: func() int { return int(cap.Load()) }})
	if !res.Done || res.Moved != 24 || countOf(last, essID) != 24 {
		t.Fatalf("%+v offered %v", res, last)
	}
}

// A crafter that declines the load (no room for it) after the carrier has accepted it is a decline, not a broken handshake: the result says
// so and the pack still holds the essence. The bot's trade stage uses it with the gate message (a decline the message explains is no strike).
func TestTradeWithCrafterSaysWhenTheCrafterDeclined(t *testing.T) {
	sim, d := carrierSession(t, 25, "main")
	partner := &actionsim.Trade{Partner: "main"} // never accepts
	var opened, closed atomic.Bool
	partner.OnMine = func(m []actionsim.Stack) {
		if countOf(m, essID) >= 25 && closed.CompareAndSwap(false, true) {
			time.AfterFunc(40*time.Millisecond, func() {
				sim.PartnerClosesTrade()
				d.Chat.Record("Other player declined trade.")
			})
		}
	}
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) && opened.CompareAndSwap(false, true) {
			sim.OpenTrade(partner)
		}
	}
	res := TradeWithCrafter(d, OfferOptions{Crafter: "main", Mode: OfferEssence, Timeout: 5 * time.Second})
	if res.Done || res.Closed || !res.Declined || !strings.Contains(res.Reason, "still holds 25 of the 25") {
		t.Fatalf("%+v", res)
	}
	if got := countOf(sim.Pack(), essID); got != 25 {
		t.Fatalf("%d essence left in the pack after a declined trade", got)
	}
}
