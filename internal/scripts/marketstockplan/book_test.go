package marketstockplan

import (
	"fmt"
	"strings"
	"testing"
)

// row is the live page's row shape (example.invalid/market/trades, 7 October 2026).
func row(id int, item, badge string, units, unit int64) string {
	total := units * unit
	return fmt.Sprintf(`<tr data-id="%d" data-maker="1" data-other-market=""><td class="mk-col-item"><a href="/market/item/x" title="%s"><span class="mk-item">%s</span></a></td><td class="mk-col-price"><span class="mk-price"><span class="mk-badge-%s" title="x">[X]</span> <span title="%d">%d</span> <span class="mk-for">for</span> <span class="mk-coin" title="%s coins">K</span> <span class="mk-ea">ea.</span> <span class="mk-for">=</span> <span class="mk-coin" title="%s coins">K</span></span></td></tr>`,
		id, item, item, badge, units, units, commas(unit), commas(total))
}

func commas(n int64) string {
	s := fmt.Sprint(n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func TestParseTradePageAndBook(t *testing.T) {
	var page strings.Builder
	id := 1
	add := func(item, badge string, units, unit int64, times int) {
		for i := 0; i < times; i++ {
			page.WriteString(row(id, item, badge, units, unit))
			id++
		}
	}
	add("Nature rune", "sell", 1670, 600, 6) // S15 sold the mule 1.67K at 600, six times
	add("Nature rune", "sell", 737, 600, 1)  //
	add("Nature rune", "buy", 3500, 280, 5)  // a stall bought from a player: not a price we pay
	add("Death rune", "sell", 2220, 300, 1)  // one sell trade: untrusted
	add("Chaos rune", "sell", 100, 150, 3)   //
	add("Chaos rune", "sell", 1, 9000000, 1) // a coin transfer, dropped
	add("Fire rune", "sell", 100, 10, 3)     // incoherent: a spread of 15x that is no coin transfer
	add("Fire rune", "sell", 100, 50, 3)     //
	add("Fire rune", "sell", 100, 150, 4)    //
	rows := ParseTradePage(page.String())
	if len(rows) != 6+1+5+1+3+1+10 {
		t.Fatalf("%d rows", len(rows))
	}
	book := BookFromTrades(rows)
	nat := book["nature rune"]
	if nat.Sell != 600 || nat.SellTrades != 7 || !nat.Coherent || !nat.Trusted() {
		t.Fatalf("nature %+v", nat)
	}
	if d := book["death rune"]; d.Sell != 300 || d.SellTrades != 1 || d.Trusted() {
		t.Fatalf("death %+v", d)
	}
	if c := book["chaos rune"]; c.Sell != 150 || c.SellTrades != 3 || !c.Trusted() {
		t.Fatalf("chaos %+v (the 9,000,000 transfer must be dropped)", c)
	}
	if f := book["fire rune"]; f.Coherent || f.Trusted() {
		t.Fatalf("fire %+v should be incoherent", f)
	}
	if _, ok := book["rune essence"]; ok {
		t.Fatal("an item with no trades has a book")
	}
	// A buy-only item has no sell book.
	buyOnly := BookFromTrades(ParseTradePage(row(1, "Law rune", "buy", 10, 300)))
	if _, ok := buyOnly["law rune"]; ok {
		t.Fatal("a market purchase made a sell price")
	}
}

func TestParseTradePageDerivesTheUnitFromTheTotal(t *testing.T) {
	// A decimal unit price ("672.22 coins") does not match the whole-number pattern; the unit is
	// the total over the quantity.
	page := `<tr data-id="9"><td><a href="/market/item/big_bones" title="Big bones"></a></td><td><span class="mk-badge-sell">[S]</span> <span title="3,124">3.12K</span> <span class="mk-coin" title="672.22 coins">672</span> <span class="mk-coin" title="2,100,000 coins">2.1M</span></td></tr>`
	rows := ParseTradePage(page)
	if len(rows) != 1 || rows[0].Unit != 672 || rows[0].Units != 3124 || !rows[0].MarketSold {
		t.Fatalf("%+v", rows)
	}
}

func TestTradesPageURL(t *testing.T) {
	if TradesPageURL(1) != TradesURL || TradesPageURL(3) != TradesURL+"?page=3" {
		t.Fatal(TradesPageURL(1), TradesPageURL(3))
	}
}
