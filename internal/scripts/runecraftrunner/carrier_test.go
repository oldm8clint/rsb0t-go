package runecraftrunner

import (
	"errors"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
)

// jiminua is the Jungle Store as content 289 runs it (shop.rs2 buy_item and sell_item, calc_shop_value): a shelf of unnoted
// essence that a sold note joins, a unit priced at the stock it leaves, 25 base rows of the shop's 40, and "The shop has run out
// of space" when a sale needs a row the shop has not got. The pack is the simulated session's.
type jiminua struct {
	sim *actionsim.Session
	d   *fleetrunner.Deps
	// stock is the essence on the shelf, baseRows the other items' rows, open whether the shop is open.
	stock, baseRows int
	open            bool
	closed          int
	clickLimit      int
}

func newJiminua(t *testing.T, stock, baseRows int, notes, coins int) (*jiminua, *fleetrunner.Deps) {
	t.Helper()
	sim := actionsim.New(gateObjs(), []actionsim.Stack{{ID: runecraftplan.NotedEssenceID, Count: notes}, {ID: coinsID, Count: coins}})
	d := &fleetrunner.Deps{Session: sim, Components: actionsim.Components(), Delay: func(dur time.Duration) bool { time.Sleep(dur); return true }}
	d.Chat = chatlog.New()
	j := &jiminua{sim: sim, d: d, stock: stock, baseRows: baseRows, open: true}
	return j, d
}

func (j *jiminua) IsOpen() bool { return j.open }
func (j *jiminua) Close() error { j.open = false; j.closed++; return nil }
func (j *jiminua) Pack() []gamestate.SlotItem {
	var out []gamestate.SlotItem
	for i, r := range j.sim.Pack() {
		out = append(out, gamestate.SlotItem{Slot: i, Item: gamestate.Item{ID: r.ID, Count: int32(r.Count)}})
	}
	return out
}
func (j *jiminua) Stock() []gamestate.SlotItem {
	var out []gamestate.SlotItem
	for i := 0; i < j.baseRows; i++ {
		out = append(out, gamestate.SlotItem{Slot: i, Item: gamestate.Item{ID: int32(5000 + i), Count: 10}})
	}
	if j.stock > 0 {
		out = append(out, gamestate.SlotItem{Slot: j.baseRows, Item: gamestate.Item{ID: essID, Count: int32(j.stock)}})
	}
	return out
}
func (j *jiminua) rows() int {
	n := j.baseRows
	if j.stock > 0 {
		n++
	}
	return n
}
func (j *jiminua) SellCount(id, n int32, w actions.Waiter) (int32, error) {
	held := countOf(j.sim.Pack(), id)
	if held == 0 {
		return 0, actions.ErrItemNotFound
	}
	k := min(int(n), held)
	if j.stock == 0 && j.rows() >= runecraftplan.ShopRows {
		j.d.Chat.Record("The shop has run out of space.")
		return 0, nil
	}
	income := runecraftplan.ShelfSellValue(j.stock, k)
	j.sim.Remove(id, k)
	j.sim.Give(coinsID, income)
	j.stock += k
	return int32(k), nil
}
func (j *jiminua) BuyCount(id, n int32, w actions.Waiter) (int32, error) {
	if j.stock == 0 {
		return 0, actions.ErrItemNotFound
	}
	coins := countOf(j.sim.Pack(), coinsID)
	bought := 0
	for i := 0; i < int(n) && j.stock > 0; i++ {
		price := runecraftplan.ShelfBuyPrice(j.stock)
		if price > coins || countOf(j.sim.Pack(), 0) < 0 {
			break
		}
		if got := j.sim.Give(essID, 1); got == 0 {
			break // no room in the pack
		}
		coins -= price
		j.sim.Remove(coinsID, price)
		j.stock--
		bought++
	}
	return int32(bought), nil
}

func withJiminua(j *jiminua) func() {
	old := OpenJiminua
	OpenJiminua = func(d *fleetrunner.Deps) (shelfShop, error) { return j, nil }
	return func() { OpenJiminua = old }
}

func TestShelfTurnsNotesIntoALoadAtTheFloorPrice(t *testing.T) {
	j, d := newJiminua(t, 40, 25, 3000, 10000)
	defer withJiminua(j)()
	res := Shelf(d, ShelfOptions{Floor: 40})
	if !res.OK(25) || res.Sold != 25 || res.Bought != 25 || res.Cost != 50 || res.Income != 0 || res.StockBefore != 40 || res.StockAfter != 40 {
		t.Fatalf("%+v", res)
	}
	if Essence(d) != 25 || Notes(d) != 2975 || Coins(d) != 9950 {
		t.Fatalf("pack: %d unnoted, %d notes, %d coins", Essence(d), Notes(d), Coins(d))
	}
	if j.closed != 1 || j.open {
		t.Fatal("the shop is closed again")
	}
	if line := res.Line(); !strings.Contains(line, "sold 25 notes for 0 gp, bought 25 for 50 gp; shelf 40") {
		t.Fatal(line)
	}
}

// An empty shelf is built up to the floor: 65 notes in, 25 units out, the shelf at 40, and the visit costs 36 net.
func TestShelfBuildsAnEmptyShelfUpToTheFloor(t *testing.T) {
	j, d := newJiminua(t, 0, 25, 3000, 10000)
	defer withJiminua(j)()
	res := Shelf(d, ShelfOptions{Floor: 40})
	if !res.OK(25) || res.Sold != 65 || res.Bought != 25 || res.StockAfter != 40 {
		t.Fatalf("%+v", res)
	}
	if res.Income != 14 || res.Cost != 50 {
		t.Fatalf("income %d cost %d, want 14 and 50", res.Income, res.Cost)
	}
	if Coins(d) != 10000+14-50 {
		t.Fatalf("coins %d", Coins(d))
	}
}

// A shop with no free row cannot take essence it holds none of: buy only, and say so.
func TestShelfFullShopTakesNoSale(t *testing.T) {
	j, d := newJiminua(t, 0, 40, 3000, 10000)
	defer withJiminua(j)()
	res := Shelf(d, ShelfOptions{Floor: 40})
	if res.OK(25) || res.Bought != 0 || !strings.Contains(res.Reason, "no visit possible") {
		t.Fatalf("%+v", res)
	}
}

// The shop may fill between the look and the sale: the refusal is read, the flag raised, and the next visit buys only.
func TestShelfReadsTheShopsRefusalAndBuysOnly(t *testing.T) {
	j, d := newJiminua(t, 0, 39, 3000, 10000)
	defer withJiminua(j)()
	// A free row at the look, none at the sale: another seller took it.
	orig := OpenJiminua
	OpenJiminua = func(d *fleetrunner.Deps) (shelfShop, error) { return &lateFull{j}, nil }
	defer func() { OpenJiminua = orig }()
	res := Shelf(d, ShelfOptions{Floor: 40})
	if !res.OutOfSpace || res.Sold != 0 || res.OK(25) || !strings.Contains(res.Reason, "no room") {
		t.Fatalf("%+v", res)
	}
	// The next visit, told to buy only, takes what a seller left on the shelf.
	j.stock, j.baseRows = 30, 40
	OpenJiminua = func(d *fleetrunner.Deps) (shelfShop, error) { return j, nil }
	res = Shelf(d, ShelfOptions{Floor: 40, BuyOnly: true})
	if res.Sold != 0 || res.Bought != 25 || !res.OK(25) {
		t.Fatalf("%+v", res)
	}
}

// lateFull is a shop that looks as if it had a free row and then does not.
type lateFull struct{ *jiminua }

func (l *lateFull) SellCount(id, n int32, w actions.Waiter) (int32, error) {
	l.baseRows = runecraftplan.ShopRows
	return l.jiminua.SellCount(id, n, w)
}

func TestShelfWithTooFewCoinsDoesNotShop(t *testing.T) {
	j, d := newJiminua(t, 40, 25, 3000, 150)
	defer withJiminua(j)()
	res := Shelf(d, ShelfOptions{Floor: 40})
	if res.OK(25) || res.Sold != 0 || !strings.Contains(res.Reason, "150 coins") {
		t.Fatalf("%+v", res)
	}
	if Notes(d) != 3000 {
		t.Fatal("no note should have been sold")
	}
}

func TestShelfTopsUpAPartialLoad(t *testing.T) {
	j, d := newJiminua(t, 50, 25, 3000, 10000)
	defer withJiminua(j)()
	j.sim.Give(essID, 10)
	res := Shelf(d, ShelfOptions{Floor: 40})
	if !res.OK(25) || res.Bought != 15 || Essence(d) != 25 {
		t.Fatalf("%+v", res)
	}
}

func TestShelfSurvivesAShopThatWillNotOpen(t *testing.T) {
	old := OpenJiminua
	OpenJiminua = func(d *fleetrunner.Deps) (shelfShop, error) { return nil, errors.New("no shopkeeper") }
	defer func() { OpenJiminua = old }()
	_, d := newJiminua(t, 40, 25, 3000, 10000)
	res := Shelf(d, ShelfOptions{})
	if res.OK(25) || !strings.Contains(res.Reason, "no shopkeeper") {
		t.Fatalf("%+v", res)
	}
}

// The notes run out mid-shift: the last visit sells what there is.
func TestShelfLastNotes(t *testing.T) {
	j, d := newJiminua(t, 60, 25, 10, 10000)
	defer withJiminua(j)()
	res := Shelf(d, ShelfOptions{Floor: 40})
	if res.Sold != 10 || res.Bought != 25 || !res.OK(25) || Notes(d) != 0 {
		t.Fatalf("%+v", res)
	}
}

// The shelf already holds the load (the default floor of 0): it is bought as it stands and nothing is sold into it. A sale is the visit's
// one window for another shopper to buy the units just sold.
func TestShelfBuysFromAShelfThatAlreadyHoldsTheLoad(t *testing.T) {
	j, d := newJiminua(t, 30, 25, 3000, 10000)
	defer withJiminua(j)()
	res := Shelf(d, ShelfOptions{})
	if !res.OK(25) || res.Sold != 0 || res.Bought != 25 || res.Rounds != 1 || Notes(d) != 3000 || j.stock != 5 {
		t.Fatalf("%+v notes %d shelf %d", res, Notes(d), j.stock)
	}
	// A shelf that does not hold the load is sold into, as before: 25 notes in, 25 units out.
	j2, d2 := newJiminua(t, 10, 25, 3000, 10000)
	defer withJiminua(j2)()
	if res := Shelf(d2, ShelfOptions{}); !res.OK(25) || res.Sold != 25 || res.Bought != 25 {
		t.Fatalf("%+v", res)
	}
}

// taker is a shop another shopper empties the shelf of right after a sale: the stack the visit has just sold is gone before its buy.
type taker struct {
	*jiminua
	takes int
}

func (k *taker) SellCount(id, n int32, w actions.Waiter) (int32, error) {
	got, err := k.jiminua.SellCount(id, n, w)
	if k.takes > 0 {
		k.takes--
		k.jiminua.stock = 0
	}
	return got, err
}

// The stack sold is bought from under the visit: it is not a failure. The visit sells and buys again in the open shop and ends with
// the load, in one visit and one opening of the shop.
func TestShelfSellsAgainWhenTheStackIsTakenFromUnderIt(t *testing.T) {
	j, d := newJiminua(t, 0, 25, 3000, 10000)
	opens := 0
	old := OpenJiminua
	OpenJiminua = func(*fleetrunner.Deps) (shelfShop, error) { opens++; return &taker{jiminua: j, takes: 1}, nil }
	defer func() { OpenJiminua = old }()
	res := Shelf(d, ShelfOptions{})
	if !res.OK(25) || res.Rounds != 2 || res.Taken != 1 || res.Sold != 50 || res.Bought != 25 || Essence(d) != 25 || opens != 1 {
		t.Fatalf("%+v opens %d", res, opens)
	}
	if line := res.Line(); !strings.Contains(line, "2 round(s), 1 taken by other shoppers") {
		t.Fatal(line)
	}
	// Taken every time: the visit ends after shelfRounds with a reason, not a loop.
	j2, d2 := newJiminua(t, 0, 25, 3000, 10000)
	OpenJiminua = func(*fleetrunner.Deps) (shelfShop, error) { return &taker{jiminua: j2, takes: 99}, nil }
	res = Shelf(d2, ShelfOptions{})
	if res.OK(25) || res.Rounds != shelfRounds || res.Taken != shelfRounds || res.Bought != 0 || !strings.Contains(res.Reason, "taken by other shoppers") {
		t.Fatalf("%+v", res)
	}
}

// A shop that will not open does not hold the shelf's turn: the carriers behind it are not kept waiting for a visit that never began.
func TestShelfDoesNotHoldTheTurnForAShopThatWillNotOpen(t *testing.T) {
	old := OpenJiminua
	OpenJiminua = func(d *fleetrunner.Deps) (shelfShop, error) {
		select {
		case shelfTurn <- struct{}{}:
			<-shelfTurn
		default:
			t.Error("the shelf's turn was held while the shop was being opened")
		}
		return nil, errors.New("did not open")
	}
	defer func() { OpenJiminua = old }()
	_, d := newJiminua(t, 40, 25, 3000, 10000)
	if res := Shelf(d, ShelfOptions{}); res.OK(25) || !strings.Contains(res.Reason, "did not open") {
		t.Fatalf("%+v", res)
	}
	select {
	case shelfTurn <- struct{}{}:
		<-shelfTurn
	default:
		t.Fatal("the turn is still held")
	}
}

func TestKaramjaRectsAreTheSpawnBoxes(t *testing.T) {
	r := KaramjaRects()
	if len(r) != len(runecraftplan.AvoidBoxes())+1 || len(r) < 80 {
		t.Fatalf("%d rects", len(r))
	}
	for _, x := range r {
		if x.Level != 0 || x.MinX > x.MaxX || x.MinZ > x.MaxZ {
			t.Fatalf("%+v", x)
		}
	}
	// The same list every time (built once).
	if &KaramjaRects()[0] != &r[0] {
		t.Fatal("KaramjaRects builds its list once")
	}
}

func TestCrafterVisible(t *testing.T) {
	_, d := crafterSession(t, 4000, "Acct 1", "Main Craft")
	if !CrafterVisible(d, "acct_1") || !CrafterVisible(d, "Main_Craft") || CrafterVisible(d, "Bob") {
		t.Fatal("players are matched by name with underscores and spaces alike")
	}
}

// Jiminua's shelf is one carrier's at a time: a second carrier waits for the first visit's release, so no carrier buys the
// load another has just sold (live 16 Oct 2026: "buying 24 essence: actions: item not found in inventory").
func TestTheShelfIsOneCarriersAtATime(t *testing.T) {
	d := &fleetrunner.Deps{Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true }}
	release := takeShelfTurn(d)
	got := make(chan struct{})
	go func() { takeShelfTurn(d)(); close(got) }()
	select {
	case <-got:
		t.Fatal("a second carrier had the shelf while the first held it")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("the second carrier never had its turn after the release")
	}
	// A stopped bot gives up the wait at once and releases nothing it did not take.
	hold := takeShelfTurn(d)
	stopped := &fleetrunner.Deps{Delay: func(time.Duration) bool { return false }}
	takeShelfTurn(stopped)()
	hold()
	select {
	case shelfTurn <- struct{}{}:
		<-shelfTurn
	default:
		t.Fatal("the turn is still held after every release")
	}
}
