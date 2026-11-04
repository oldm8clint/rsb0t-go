package bot

// RuneRunner's market station (runerunnerplan.KindMarket, marketstockplan): buy the runes the mule
// is short of from the Seers' Village player stalls, with the runner's own coins, when a stall
// lists them at a price under the ceiling.
//
// HOW THE PLAYER MARKET WORKS, as this code relies on it (all of it already in the repo and live
// since 21 September; nothing here is new protocol):
//
//   - example.invalid/market/inventory?character=<id> publishes each stall's bank: stock, last sold
//     price, last bought price (scarabrunner.MarketListings, cached). A stall is an ordinary
//     account standing at the Seers bank. There is no shop interface.
//   - To buy: say "buying <n> <item>" in public chat, wait for the stall's quote, trade it, put up
//     exactly the coins, accept both screens. scarabrunner.BuyRunesAtMarket drives that through
//     woodcutrunner.BuyAtMarket: the fleet's one-buyer-per-stall turn, the exact-screen rule
//     (coins go up only as the agreed total, the confirm screen is accepted only when it is
//     exactly the ordered item for exactly those coins) and ONE re-offer at the stall's own quote
//     when it is at or under the unit cap. The purchase is judged by this account's own pack.
//   - Stalls only sell what players have sold them. On 7 October the player stalls listed 17
//     Nature runes between them; S15 had sold the fleet about 10,700 at 600 each the night
//     before. So the station is mostly a look: one cached page read per marketCheckMinutes, and a
//     trip only when marketstockplan finds a worthwhile lot.
//
// WHERE IT SITS. After the draw (or purse) at the mule's post, on the mule's world, so the runes it
// buys are banked at Seers and handed to the mule by the next round's delivery, which already
// withdraws every listed rune from the bank. Nothing about the shop stations changes: the station
// takes its coins from the bank (marketCoins at most, less the coin reserve), never from the
// pack the shops are paid from, and it ends back at the mule's post.
//
// COINS. The bank is the runner's own. When the lot is bigger than the bank, the station asks the
// mule for the difference over the wall bus (runerunner.AskMuleForCoins, the draw station's own
// top-up, at most one ask per TopUpRetry); the mule's coinsPerOrder bounds each order.
//
// NOT LAB-PROVABLE: the lab has no market characters. See COLLAB.md ("naturemarket-1007") for the
// log lines to watch on the first live round.

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/marketstockplan"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/scripts/tile"
)

const (
	// runeMarketListingsTTL is how old the stalls' page may be before it is read again (the page
	// itself is a snapshot minutes old; the cache is shared by every runner in the process).
	runeMarketListingsTTL = 5 * time.Minute
	// runeMarketBookTTL and runeMarketBookPages: the trade book is the newest 15 pages of
	// example.invalid/market/trades (about 600 trades, roughly a day), read at most every 30 minutes
	// for the whole process.
	runeMarketBookTTL   = 30 * time.Minute
	runeMarketBookPages = 15
	// The rests a stall earns for an item, by how its last try ended.
	runeMarketRestAbsent = 3 * time.Hour          // not among the visible players at the Seers bank
	runeMarketRestRefuse = 30 * time.Minute       // said it cannot sell, or nothing arrived
	runeMarketRestBusy   = 3 * time.Minute        // another account held the stall's turn
	runeMarketRounds     = 3                      // plan-walk-buy passes in one station
	runeMarketSeerWait   = 10 * time.Second       // how long a stall may take to appear in view
	runeMarketBankSettle = 600 * time.Millisecond // pause after a bank closes
)

var runeMarketBookCache struct {
	sync.Mutex
	at   time.Time
	book map[string]marketstockplan.Book
}

// runeMarketFetchBook reads the newest runeMarketBookPages pages of the trades listing and folds
// them into the sell-side book. A page that fails is skipped; an error comes back only when none
// could be read.
func runeMarketFetchBook(client *http.Client) (map[string]marketstockplan.Book, error) {
	var rows []marketstockplan.TradeRow
	var firstErr error
	got := 0
	for page := 1; page <= runeMarketBookPages; page++ {
		resp, err := client.Get(marketstockplan.TradesPageURL(page))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			if firstErr == nil {
				firstErr = fmt.Errorf("trades page %d: %v %s", page, err, resp.Status)
			}
			continue
		}
		parsed := marketstockplan.ParseTradePage(string(body))
		if len(parsed) == 0 {
			break
		}
		got++
		rows = append(rows, parsed...)
	}
	if got == 0 {
		if firstErr == nil {
			firstErr = fmt.Errorf("no trade page could be read")
		}
		return nil, firstErr
	}
	return marketstockplan.BookFromTrades(rows), nil
}

// runeMarketBook answers marketstockplan.Input.Book from the trade book, refreshing it when it is
// older than runeMarketBookTTL. A failed read keeps the last good book (nothing at all is "not
// known", which marketstockplan treats as the hard ceiling).
func runeMarketBook(log func(string)) func(item string) (marketstockplan.Book, bool) {
	c := &runeMarketBookCache
	c.Lock()
	defer c.Unlock()
	if c.book == nil || time.Since(c.at) >= runeMarketBookTTL {
		c.at = time.Now() // even on failure: never hammer the site
		client := &http.Client{Timeout: 20 * time.Second}
		if book, err := runeMarketFetchBook(client); err != nil {
			if log != nil {
				log("market: the trade book could not be read (" + err.Error() + ")")
			}
		} else {
			c.book = book
		}
	}
	book := c.book
	return func(item string) (marketstockplan.Book, bool) {
		b, ok := book[strings.ToLower(strings.TrimSpace(item))]
		return b, ok
	}
}

var runeMarketRests = struct {
	sync.Mutex
	until map[string]time.Time
}{until: map[string]time.Time{}}

func runeMarketRest(market, item string, d time.Duration, now time.Time) {
	runeMarketRests.Lock()
	runeMarketRests.until[marketstockplan.RestKey(market, item)] = now.Add(d)
	runeMarketRests.Unlock()
}

// runeMarketResting is the stalls-and-items resting now, in marketstockplan.Input.Resting's shape.
func runeMarketResting(now time.Time) map[string]bool {
	runeMarketRests.Lock()
	defer runeMarketRests.Unlock()
	out := map[string]bool{}
	for k, t := range runeMarketRests.until {
		if now.Before(t) {
			out[k] = true
		} else {
			delete(runeMarketRests.until, k)
		}
	}
	return out
}

// marketStalls turns the page's rows into marketstockplan's shape: a row's last-sold price is the
// stall's listed price.
func marketStalls(rows []fleetplan.MarketListing) []marketstockplan.Listing {
	out := make([]marketstockplan.Listing, 0, len(rows))
	for _, r := range rows {
		out = append(out, marketstockplan.Listing{Market: r.Market, Item: r.Item, Stock: r.Stock, Ask: r.LastSold})
	}
	return out
}

// marketInput is one planning pass over the current page, book and mule stock.
func (b *runeRunnerBot) marketInput(ctx *Context, d *fleetrunner.Deps, coins int) (marketstockplan.Input, int) {
	logLine := func(s string) { b.logf(ctx, "%s", s) }
	var rows []fleetplan.MarketListing
	if b.marketRowsSeam != nil {
		rows = b.marketRowsSeam()
	} else {
		rows = fleetrunner.MarketListings(runeMarketListingsTTL, logLine)
	}
	book := b.marketBookSeam
	if book == nil {
		book = runeMarketBook(logLine)
	}
	names := make([]string, 0, len(b.set.MarketTargets))
	for _, t := range b.set.MarketTargets {
		names = append(names, t.Rune)
	}
	pending := runerunner.PackCounts(d, names)
	for _, n := range names {
		pending[n] += b.st.BankRunes[n]
	}
	deliverable := map[string]bool{}
	for _, r := range b.set.RuneList {
		deliverable[r] = true
	}
	// A market goal that is not a shop rune (Rune essence, 2 Nov 2026) is delivered to the mule like the runes.
	for _, t := range b.set.MarketTargets {
		if !strings.HasSuffix(t.Rune, " rune") {
			deliverable[t.Rune] = true
		}
	}
	return marketstockplan.Input{
		Targets: b.set.MarketTargets,
		Held:    muleBankCount(ctx, b.set.Mule),
		Pending: pending,
		Deliverable: func(r string) bool {
			return deliverable[r]
		},
		Listings:     marketStalls(rows),
		Book:         book,
		Stalls:       b.set.MarketStallList,
		Resting:      runeMarketResting(time.Now()),
		Coins:        coins,
		PricePercent: b.set.MarketPricePercent,
		MinLot:       b.set.MarketMinLot,
		TradeCoins:   b.set.MarketTradeCoins,
	}, len(rows)
}

// marketSpendable is what the station may spend from the bank: the last reading less the coin
// reserve, at most marketCoins.
func (b *runeRunnerBot) marketSpendable() int {
	spend := b.st.BankCoins - b.set.CoinReserve
	if spend > b.set.MarketCoins {
		spend = b.set.MarketCoins
	}
	if spend < 0 {
		spend = 0
	}
	return spend
}

// marketBudget is what the market station may spend from the bank: marketSpendable less the float
// the shops still need (runerunnerplan.ShopFloat: the round's estimated cost less what the pack
// already carries beyond the fare). The market stands right after the draw and takes coins straight
// from the bank, so without it a death-rune lot at 300-375 each (5 of the 28 live asks on 12 October,
// up to 976,000 coins) would eat the float the shops are about to need. A lot the float leaves no room
// for asks the mule for the difference, through the same ask the draw station uses.
func (b *runeRunnerBot) marketBudget(ctx *Context, packCoins int) int {
	spend := b.marketSpendable()
	if spend <= 0 {
		return 0
	}
	float := runerunnerplan.ShopFloat(b.roundNeed(ctx, b.st.Cursor+1).Est(), packCoins, b.set.FareCoins)
	if room := b.st.BankCoins - b.set.CoinReserve - float; room < spend {
		spend = room
	}
	if spend < 0 {
		return 0
	}
	return spend
}

func planCoins(trades []marketstockplan.Trade) int {
	n := 0
	for _, t := range trades {
		n += t.MaxCoins
	}
	return n
}

func (b *runeRunnerBot) marketStation(ctx *Context, d *fleetrunner.Deps, s runerunnerplan.Station) {
	now := time.Now()
	every := time.Duration(b.set.MarketCheckMinutes) * time.Minute
	switch {
	case !b.set.MarketBuy:
		b.finish(ctx, skipped("marketBuy is off"))
		return
	case ctx.World() != s.World:
		b.finish(ctx, skipped("on world %d; the station is for world %d", ctx.World(), s.World))
		return
	case b.noCoins:
		b.finish(ctx, skipped("no coins"))
		return
	case !b.st.LastMarketCheck.IsZero() && now.Sub(b.st.LastMarketCheck) < every:
		b.finish(ctx, skipped("looked at the market %s ago (marketCheckMinutes %d)", now.Sub(b.st.LastMarketCheck).Round(time.Second), b.set.MarketCheckMinutes))
		return
	}
	b.st.LastMarketCheck = now
	b.save(ctx)

	// A look, without coins held back: is any lot worth the walk at all?
	in, rows := b.marketInput(ctx, d, b.set.MarketCoins)
	if rows == 0 {
		b.finish(ctx, skipped("the market page could not be read"))
		return
	}
	look := marketstockplan.MakePlan(in)
	if len(look.Trades) == 0 {
		b.logf(ctx, "market: no buy: %s", look.Reason())
		b.finish(ctx, skipped("market: %s", look.Reason()))
		return
	}
	b.logf(ctx, "market: worth a trip: %s", describeTrades(look.Trades))

	// Coins: the bank's, less the reserve; the mule tops it up when the lot is bigger.
	post := b.mulePost(ctx)
	packStart := fleetrunner.CoinsHeld(d)
	budget := func() int { return b.marketBudget(ctx, packStart) }
	if need := planCoins(look.Trades); budget() < need {
		b.marketAskMule(ctx, d, post, need-budget(), packStart)
	}
	in, _ = b.marketInput(ctx, d, budget())
	plan := marketstockplan.MakePlan(in)
	if len(plan.Trades) == 0 {
		b.logf(ctx, "market: no buy with %d coins to spend: %s", budget(), plan.Reason())
		b.finish(ctx, skipped("market: %d coins to spend: %s", budget(), plan.Reason()))
		return
	}

	stand := fleetplan.DefaultMarketPlaces["Seers Market"]
	if !b.marketWalk(d, s.Key(), stand, 3) {
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeWalkFailed, Reason: "could not reach the Seers bank for the market"})
		return
	}
	packStart = fleetrunner.CoinsHeld(d)
	bought, spent := map[string]int{}, 0
	failed := ""
	for pass := 0; pass < runeMarketRounds && len(plan.Trades) > 0 && !ctx.Stopped(); pass++ {
		tr := plan.Trades[0]
		got, paid, why := b.marketTrade(ctx, d, stand, tr, packStart)
		if got > 0 {
			bought[tr.Item] += got
			spent += paid
			b.st.MarketSpent += paid
			if b.st.MarketBought == nil {
				b.st.MarketBought = map[string]int{}
			}
			b.st.MarketBought[tr.Item] += got
			held, _ := muleHeld(ctx, b.set.Mule, tr.Item)
			b.logf(ctx, "%s", marketstockplan.BoughtLine(got, tr.Item, paid, held+b.pendingFor(d, tr.Item)))
		} else {
			failed = why
		}
		// Plan again from the new bank and rests: what is left of the lot, or the next stall.
		in, _ = b.marketInput(ctx, d, budget())
		plan = marketstockplan.MakePlan(in)
	}
	b.save(ctx)

	if here := fleetrunner.Here(d); here == nil || tile.Chebyshev(*here, post) > 6 {
		if !b.marketWalk(d, s.Key(), post, 3) {
			b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeWalkFailed, Reason: "could not walk back to the mule's post after the market"})
			return
		}
	}
	if len(bought) == 0 {
		reason := "market: bought nothing"
		if failed != "" {
			reason += ": " + failed
		}
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeEmpty, Reason: reason})
		return
	}
	b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeBought, Bought: bought, Spent: spent,
		Reason: "market: " + runerunnerplan.FormatCounts(bought)})
}

// marketWalk walks to dest on the wolf- and bear-avoiding webwalk, three tries (a test's seam
// replaces it).
func (b *runeRunnerBot) marketWalk(d *fleetrunner.Deps, label string, dest tile.Tile, radius int) bool {
	if b.marketWalkSeam != nil {
		return b.marketWalkSeam(label, dest)
	}
	return runerunner.WalkTries(d, label, dest, radius, runerunner.ShopWalkTimeout, 3)
}

// describeTrades is the log form of a plan: "5000 Nature rune from S15 at 600 (cap 750)".
func describeTrades(ts []marketstockplan.Trade) string {
	parts := make([]string, 0, len(ts))
	for _, t := range ts {
		parts = append(parts, itoa(t.Qty)+" "+t.Item+" from "+t.Market+" at "+itoa(t.Unit)+" (cap "+itoa(t.UnitCap)+", ceiling "+itoa(t.Ceiling)+")")
	}
	return strings.Join(parts, "; ")
}

func itoa(n int) string { return strconv.Itoa(n) }

// muleHeld is the mule's published count of an item.
func muleHeld(ctx *Context, mule, item string) (int, bool) {
	f := muleBankCount(ctx, mule)
	if f == nil {
		return 0, false
	}
	return f(item)
}

// pendingFor is what this runner carries or has banked of an item, awaiting delivery.
func (b *runeRunnerBot) pendingFor(d *fleetrunner.Deps, item string) int {
	return runerunner.PackCounts(d, []string{item})[item] + b.st.BankRunes[item]
}

// marketAskMule asks the mule for `want` coins at its post when the bank cannot pay the lot
// (runerunnerplan.MuleTopUp's own gates: the mule on this world and heard, one ask per TopUpRetry),
// then banks them. The pack's own coins (packStart) are put back after, so the shops are paid as
// before.
func (b *runeRunnerBot) marketAskMule(ctx *Context, d *fleetrunner.Deps, post tile.Tile, want, packStart int) {
	if b.set.TopUpAmount <= 0 {
		b.logf(ctx, "market: the bank holds %d coins, the lot needs %d more, and topUpAmount is 0 (no mule top-up)", b.st.BankCoins, want)
		return
	}
	if want > runerunnerplan.MaxTopUpAmount {
		want = runerunnerplan.MaxTopUpAmount
	}
	now := time.Now()
	m, known := ctx.MuleWorld(b.set.Mule)
	muleCoins, muleCoinsKnown := muleHeld(ctx, b.set.Mule, runerunner.CoinsName)
	v := runerunnerplan.MuleTopUp(runerunnerplan.TopUpInput{
		Below: 1 << 30, Amount: want,
		BankCoins: b.st.BankCoins, PackCoins: packStart,
		MuleWorld: m, MuleWorldKnown: known, MyWorld: ctx.World(),
		MuleHeard: scarabWorldMuleHeard(ctx, b.set.Mule), LastAsk: b.st.LastTopUpAsk, Now: now,
		MuleCoins: muleCoins, MuleCoinsKnown: muleCoinsKnown,
		MuleFloor: b.set.MuleCoinFloor,
	})
	if v.Ask <= 0 {
		b.logf(ctx, "market: coins for the lot not asked: %s", v.Reason)
		return
	}
	if !b.marketWalk(d, "market top-up", post, 4) {
		return
	}
	b.st.LastTopUpAsk = now
	b.save(ctx)
	b.logf(ctx, "market: asking mule=%s for %d coins for the lot (bank %d)", b.set.Mule, v.Ask, b.st.BankCoins)
	rd := fleetrunner.MuleDeps(d, ctx.Bus, ctx.Username)
	res := runerunner.AskMuleForCoins(d, rd, b.set.Mule, ctx.Username, v.Ask)
	b.st.NoteTopUp(res.Got)
	b.logf(ctx, "market: top-up mule=%s asked=%d got=%d orders=%d declined=%v reason=%q round_received=%d", b.set.Mule, v.Ask, res.Got, res.Orders, res.Declined, res.Reason, b.st.RoundTopUp)
	b.noteTopUpDecline(ctx, v.Ask, res)
	// Everything into the bank, then the shops' own pack coins back out.
	bank, _, ok := runerunner.OpenBankAt(d, post)
	if !ok {
		b.logf(ctx, "market: the bank at the mule's post would not open after the top-up; the coins ride in the pack")
		return
	}
	runerunner.DepositExcept(d, bank, nil)
	read := runerunner.ReadBank(d, bank)
	if packStart > 0 {
		if got, err := runerunner.WithdrawCoins(d, bank, packStart); err != nil {
			b.logf(ctx, "market: putting %d coins back in the pack: %v", packStart, err)
		} else {
			read.Coins -= got
		}
	}
	b.st.NoteBank(read.Runes, read.Coins, time.Now())
	runerunner.CloseBank(d, bank)
	ctx.Delay(runeMarketBankSettle)
}

// marketTrade makes one planned trade at the Seers bank: check the stall is in view, draw exactly
// the coins the trade may cost on top of the pack's own, buy, bank what arrived and put the pack's
// coins back to what they were. It returns what arrived and what it cost, judged by the pack.
func (b *runeRunnerBot) marketTrade(ctx *Context, d *fleetrunner.Deps, stand tile.Tile, tr marketstockplan.Trade, packStart int) (got, paid int, why string) {
	now := time.Now()
	if !b.marketSellerInView(ctx, tr.Market) {
		b.logf(ctx, "market: %s is not among the visible players at the Seers bank — leaving it alone for %s", tr.Market, runeMarketRestAbsent)
		runeMarketRest(tr.Market, tr.Item, runeMarketRestAbsent, now)
		return 0, 0, tr.Market + " is not at the Seers bank"
	}
	// Coins: the pack must hold the trade's whole ceiling (BuyAtMarket refuses before it asks
	// otherwise), drawn exactly.
	bank, read, ok := runerunner.OpenBankAt(d, stand)
	if !ok {
		return 0, 0, "the Seers bank would not open"
	}
	b.st.NoteBank(read.Runes, read.Coins, now)
	if held := fleetrunner.CoinsHeld(d); held < tr.MaxCoins {
		if _, err := runerunner.WithdrawCoins(d, bank, tr.MaxCoins-held); err != nil {
			b.logf(ctx, "market: drawing %d coins: %v", tr.MaxCoins-held, err)
		}
	}
	runerunner.CloseBank(d, bank)
	ctx.Delay(runeMarketBankSettle)
	if fleetrunner.CoinsHeld(d) < tr.MaxCoins {
		return 0, 0, "the bank could not pay the lot"
	}
	b.logf(ctx, "market: asking %s for %d %s at %d each (re-offer up to %d, at most %d coins; ceiling %d, mule short %d)",
		tr.Market, tr.Qty, strings.ToLower(tr.Item), tr.Unit, tr.UnitCap, tr.MaxCoins, tr.Ceiling, tr.Short)
	res := fleetrunner.BuyRunesAtMarket(d, fleetrunner.MarketRuneOrder{
		Market: tr.Market, Stand: stand, Item: tr.Item, Qty: tr.Qty,
		Unit: tr.Unit, UnitCap: tr.UnitCap, MaxCoins: tr.MaxCoins, Me: ctx.Username,
	})
	got, paid = res.Got, res.Paid
	switch {
	case got > 0:
		why = res.Reason
	case res.Busy:
		runeMarketRest(tr.Market, tr.Item, runeMarketRestBusy, time.Now())
		why = tr.Market + " was busy with another buyer"
	default:
		runeMarketRest(tr.Market, tr.Item, runeMarketRestRefuse, time.Now())
		why = tr.Market + ": " + res.Reason
	}
	// Bank what arrived and put the pack's coins back to where they were, the leftover and the
	// unspent ceiling included.
	if bank, _, ok := runerunner.OpenBankAt(d, stand); ok {
		runerunner.DepositExcept(d, bank, nil)
		read := runerunner.ReadBank(d, bank)
		if packStart > 0 {
			if n, err := runerunner.WithdrawCoins(d, bank, packStart); err != nil {
				b.logf(ctx, "market: putting %d coins back in the pack: %v", packStart, err)
			} else {
				read.Coins -= n
			}
		}
		b.st.NoteBank(read.Runes, read.Coins, time.Now())
		runerunner.CloseBank(d, bank)
		ctx.Delay(runeMarketBankSettle)
	} else {
		b.logf(ctx, "market: the Seers bank would not open after the trade; %d %s ride in the pack", got, strings.ToLower(tr.Item))
	}
	if got <= 0 {
		b.logf(ctx, "market: bought no %s from %s: %s", strings.ToLower(tr.Item), tr.Market, why)
	}
	return got, paid, why
}

// marketSellerInView waits up to runeMarketSeerWait for the named stall to be among the players in
// view: the stall is an account that stands at the Seers bank, and one that is not there cannot be
// asked (the walk costs a round trip).
func (b *runeRunnerBot) marketSellerInView(ctx *Context, name string) bool {
	want := strings.ToLower(strings.TrimSpace(name))
	in := func() bool {
		for _, p := range ctx.Session.State().Players() {
			if strings.ToLower(strings.TrimSpace(p.Name)) == want {
				return true
			}
		}
		return false
	}
	return in() || ctx.DelayUntil(in, runeMarketSeerWait)
}
