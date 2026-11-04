package fleetrunner

// Buying from a player-run market account (scarabplan/market.go has the rule and its HONEST
// STATUS): say the order in public chat, read the market's price from its public reply, answer its
// trade request, and put up the coins only when scarabplan.DecideMarketScreen says the screen is
// exactly the order at the agreed price — on both screens. The purchase is judged by the pack.

import (
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/mulerunner"
)

// MarketResult is what a purchase actually did, by pack delta.
type MarketResult struct {
	Price  int
	Got    int
	Paid   int
	Reason string
}

// BuyFromMarket orders qty of item from the market account standing in view, paying at most
// maxPrice for the lot. It never pays without a stated price and a screen that matches it.
func BuyFromMarket(d *Deps, market, item string, qty, maxPrice int, timeout time.Duration) MarketResult {
	var res MarketResult
	var mu sync.Mutex
	var replies []string
	cancel := d.Session.State().Subscribe(func(ev gamestate.Event) {
		if ev.Kind != gamestate.EventChatMessage || (ev.ChatType != gamestate.ChatPublic && ev.ChatType != gamestate.ChatPublicMod) {
			return
		}
		if !actions.SameUsername(ev.Username, market) {
			return
		}
		mu.Lock()
		replies = append(replies, ev.Text)
		mu.Unlock()
	})
	defer cancel()

	itemBefore, coinsBefore := CountPackNamed(d, item), CoinsHeld(d)
	chatCursor := 0
	if d.Chat != nil {
		chatCursor = d.Chat.Total()
	}
	order := fleetplan.MarketOrderText(item, qty)
	if err := actions.SayPublic(d.Session, order); err != nil {
		res.Reason = "saying the order: " + err.Error()
		return res
	}
	d.Logf("market: said %q to %s", order, market)

	trade := actions.NewTrade(d.Session)
	deadline := time.Now().Add(timeout)
	accepted, lastConfirm, offered := false, false, false
	for time.Now().Before(deadline) {
		mu.Lock()
		for _, r := range replies {
			// Logged word for word (owner, 2026-09-19): the market's real reply format is not
			// known yet, and these lines are what the price reader gets tightened from. They are
			// the market account's own public chat; no data of ours is in them.
			d.Logf("MARKET REPLY (verbatim) from %s: %q", market, r)
			if p, ok := fleetplan.ParseMarketPrice(r); ok && res.Price == 0 {
				res.Price = p
				d.Logf("market: %s quoted %d coins (%q)", market, p, r)
			}
		}
		replies = nil
		mu.Unlock()

		if !trade.Active() {
			if accepted && offered {
				break // the trade closed after we accepted: judge it by the pack below
			}
			if d.Chat != nil {
				lines, next := d.Chat.ReadAfter(chatCursor, time.Time{})
				chatCursor = next
				for _, line := range lines {
					if name, ok := mulerunner.TradeRequestFrom(line); ok && actions.SameUsername(name, market) {
						if res.Price == 0 {
							d.Logf("market: %s opened a trade before quoting a price — not answering", market)
							continue
						}
						if err := trade.RequestPlayer(market); err != nil {
							d.Logf("market: answering %s's trade: %v", market, err)
						}
					}
				}
			}
			if !d.Delay(300 * time.Millisecond) {
				res.Reason = "stopped"
				return res
			}
			continue
		}
		confirm := trade.OnConfirmScreen()
		if confirm != lastConfirm {
			accepted, lastConfirm = false, confirm
		}
		screen := fleetplan.MarketScreen{Confirm: confirm, Partner: trade.Partner(), Market: market,
			Item: item, Qty: qty, Price: res.Price, MaxPrice: maxPrice}
		mine, theirs := trade.MyOffer(), trade.TheirOffer()
		if confirm {
			mine, theirs = trade.ConfirmOffers()
		}
		screen.Mine, screen.Theirs = namedCounts(d, mine), namedCounts(d, theirs)
		action, why := fleetplan.DecideMarketScreen(screen)
		switch action {
		case fleetplan.MarketDecline:
			d.Logf("market: declining the trade with %s: %s", market, why)
			_ = trade.Decline(3 * time.Second)
			res.Reason = why
			return res
		case fleetplan.MarketOffer:
			if !offered {
				if err := trade.Offer(CoinsID, int32(res.Price), 3*time.Second); err != nil {
					d.Logf("market: putting up %d coins: %v", res.Price, err)
				}
				offered = true
			}
		case fleetplan.MarketAccept:
			if !accepted {
				if err := trade.Accept(); err == nil {
					accepted = true
				}
			}
		}
		if !d.Delay(300 * time.Millisecond) {
			res.Reason = "stopped"
			return res
		}
	}
	if trade.Active() {
		// Out of time with a screen still open: walk away, which hands back anything we put up.
		d.Logf("market: the trade with %s never completed — declining", market)
		_ = trade.Decline(3 * time.Second)
		if res.Reason == "" {
			res.Reason = "the trade never completed"
		}
	}
	WaitFor(d, "the purchase to land", 5*time.Second, func() bool { return CountPackNamed(d, item) >= itemBefore+qty })
	res.Got = CountPackNamed(d, item) - itemBefore
	res.Paid = coinsBefore - CoinsHeld(d)
	if res.Got <= 0 && res.Reason == "" {
		res.Reason = "nothing arrived"
	}
	return res
}

func namedCounts(d *Deps, items []gamestate.SlotItem) []fleetplan.ItemCount {
	byName := map[string]int{}
	var order []string
	for _, it := range items {
		n := ObjName(d, it.ID)
		if n == "" {
			continue
		}
		if _, seen := byName[n]; !seen {
			order = append(order, n)
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		byName[n] += c
	}
	out := make([]fleetplan.ItemCount, 0, len(order))
	for _, n := range order {
		out = append(out, fleetplan.ItemCount{Item: n, Count: byName[n]})
	}
	return out
}

// marketNameMatch is exported for the lab's scripted market.
func MarketNameMatch(a, b string) bool {
	return actions.SameUsername(strings.TrimSpace(a), strings.TrimSpace(b))
}
