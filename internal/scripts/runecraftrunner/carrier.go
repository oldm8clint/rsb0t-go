package runecraftrunner

// The RuneCarrier's I/O (runecraft P3): the walks of the Karamja corridor, Jiminua's shelf and a few reads. The decisions are
// runecraftplan's (PlanShelf, DecideCarrierStage); the trade with the crafter is gate.go.

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/naturepairplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// The carrier's items.
const (
	// CoinsID is the Coins object.
	CoinsID = 995
	// KeeperName is Jiminua, the Jungle Store's keeper.
	KeeperName = "Jiminua"
)

// Notes is the noted Rune essence in the pack (the kit), Coins the coins.
func Notes(d *fleetrunner.Deps) int { return PackCount(d, runecraftplan.NotedEssenceID) }

// Coins is the coins in the pack.
func Coins(d *fleetrunner.Deps) int { return PackCount(d, CoinsID) }

// RubItem is the name of the Games necklace or Ring of dueling the pack holds (the run staller rubs it), "" when it holds none.
func RubItem(d *fleetrunner.Deps) string {
	if d.Config == nil {
		return ""
	}
	for _, it := range packItems(d) {
		if o, ok := d.Config.Obj(int(it.ID)); ok && !o.Noted() && fleetplan.IsRubItem(o.Name) {
			return o.Name
		}
	}
	return ""
}

// CrafterVisible reports whether a player of that name is in view.
func CrafterVisible(d *fleetrunner.Deps, name string) bool {
	for _, p := range d.Session.State().Players() {
		if actions.SameUsername(p.Name, name) {
			return true
		}
	}
	return false
}

// ---- the corridor ----

var (
	rectsOnce sync.Once
	rects     []webwalk.Rect
)

// KaramjaRects is the corridor's boxes as walker rectangles: every spider, tribesman and scorpion spawn of the island grown by what
// the monster can reach (runecraftplan.AvoidBoxes), and NaturePair's wrong-boat zone, the ground east of Brimhaven that leads to
// the Port Sarim boat (naturepairplan.WrongBoatZone: the TS script kept it out of every runner walk so a repath never boards it; on
// the real pack the plans do not change with it, it is insurance against a repath after a failed hop). Built once.
func KaramjaRects() []webwalk.Rect {
	rectsOnce.Do(func() {
		for _, b := range runecraftplan.AvoidBoxes() {
			rects = append(rects, webwalk.Rect{MinX: b.MinX, MaxX: b.MaxX, MinZ: b.MinZ, MaxZ: b.MaxZ, Level: 0})
		}
		z := naturepairplan.WrongBoatZone
		rects = append(rects, webwalk.Rect{MinX: z.MinX, MaxX: z.MaxX, MinZ: z.MinZ, MaxZ: z.MaxZ, Level: 0})
	})
	return rects
}

// WalkKaramja walks to dest on the walker (ships, doors, the run staller of d.Travel) with the corridor's boxes as no-go
// rectangles beside the wolves' and the bears'. The walk to or from the island is planned with them too: the rectangles only
// matter where the route passes them.
func WalkKaramja(d *fleetrunner.Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	return fleetrunner.WalkToAvoiding(d, dest, radius, timeout, KaramjaRects())
}

// GateStand is the tile beside the Nature ruins that the engine accepts the talisman from and that is nearest the portal's exit
// tile, so the walk after every portal is as short as the map allows. ok is false when no such tile can be walked to.
func GateStand(d *fleetrunner.Deps, ruins locindex.Match) (tile.Tile, bool) {
	stands := Stands(d, ruins, runecraftplan.Nature.Exit)
	if len(stands) == 0 {
		return tile.Tile{}, false
	}
	return stands[0], true
}

// ---- the shelf ----

// shelfShop is the part of an open shop the shelf step uses (actions.Shop is one).
type shelfShop interface {
	IsOpen() bool
	Stock() []gamestate.SlotItem
	Pack() []gamestate.SlotItem
	SellCount(objID, n int32, w actions.Waiter) (int32, error)
	BuyCount(objID, n int32, w actions.Waiter) (int32, error)
	Close() error
}

// OpenJiminua opens Jiminua's shop; a test replaces it.
var OpenJiminua = func(d *fleetrunner.Deps) (shelfShop, error) {
	sh := actions.NewShop(d.Session)
	if sh.IsOpen() {
		return sh, nil
	}
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	err := entityquery.OpenShop(d.Session, d.Config, KeeperName, w)
	if errors.Is(err, entityquery.ErrKeeperOutOfReach) {
		if npc, ok := entityquery.Npcs(d.Session.State(), d.Config).Name(KeeperName).Nearest(); ok {
			fleetrunner.WalkTo(d, tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}, 1, 20*time.Second)
		}
		err = entityquery.OpenShop(d.Session, d.Config, KeeperName, w)
	}
	if err != nil {
		return nil, err
	}
	return sh, nil
}

// ShelfOptions are one shelf step's policy.
type ShelfOptions struct {
	// Floor is carrierShelfFloor; BuyOnly forbids the sale (the shop was found to have no room for it).
	Floor   int
	BuyOnly bool
	// Want is the unnoted essence the trip needs; 0 is runecraftplan.CarrierOffer.
	Want int
}

// ShelfResult is one shelf step.
type ShelfResult struct {
	// Plan is what was decided from the shelf as found (the first round's); StockBefore the essence on it then, StockAfter when the
	// step left.
	Plan                     runecraftplan.ShelfPlan
	StockBefore, StockAfter  int
	Sold, Bought             int
	Income, Cost             int
	UnnotedBefore, UnnotedAt int
	// Rounds is how many times the visit sold and bought (1 unless a stack was taken from under it, see shelfRounds), Taken how
	// many of those rounds found the units it had just sold gone from the shelf or short.
	Rounds, Taken int
	// OutOfSpace is set when the shop refused the sale ("The shop has run out of space"): the caller buys only from here on.
	OutOfSpace bool
	Took       time.Duration
	// Reason is why the step fell short; empty when the pack holds the load wanted (or all the shelf had).
	Reason string
}

// OK reports whether the pack holds the load the step went for.
func (r ShelfResult) OK(want int) bool { return r.Reason == "" && r.UnnotedAt >= want }

// shelfTurn is Jiminua's shelf, one carrier at a time across the fleet (every account runs in this one process): a visit
// reads the shelf, sells its notes and buys its load back, and another carrier that bought in between took the load it had
// just sold. Live 16 Oct 2026 with four carriers one shift ended on "shelf failed 3 times in a row: buying 24 essence:
// actions: item not found in inventory". A visit takes a few seconds, so a carrier waits for the turn at most shelfTurnWait
// and then goes ahead. The turn is taken once the shop is OPEN, not before: a shop that will not open (two visits on the 16th
// held the turn twelve seconds each for nothing, and the carriers behind them waited with it) costs only its own carrier's time.
var shelfTurn = make(chan struct{}, 1)

const shelfTurnWait = time.Minute

// shelfRounds is how many times a visit may sell and buy again in the shop it has open. The shelf is shared with shoppers this
// process does not run (sixteen accounts of another fleet strung along the same road on the 16th): the units a visit has just sold
// can be bought from under it in the second or so between the sale and its own first buy, and the units that go are not lost, only
// the visit's sale. Selling and buying again at once, in the open shop, costs a few seconds and a few notes; leaving, pausing three
// seconds, walking up again and queueing for the turn costs a strike.
const shelfRounds = 3

// takeShelfTurn waits for the shelf's turn and returns its release (a no-op when the wait ran out or the bot stopped).
func takeShelfTurn(d *fleetrunner.Deps) (release func()) {
	deadline := time.Now().Add(shelfTurnWait)
	for {
		select {
		case shelfTurn <- struct{}{}:
			return func() { <-shelfTurn }
		default:
		}
		if !time.Now().Before(deadline) || !d.Delay(poll) {
			return func() {}
		}
	}
}

// Shelf opens Jiminua's shop and turns notes into a load: it reads the shelf, plans the visit (runecraftplan.PlanShelf), sells
// the notes and buys the units, five clicks a tick (actions.Shop.SellCount/BuyCount), and judges the visit by the pack: the notes
// that left, the essence and coins that came. A visit whose purchase finds the units gone (another shopper bought the stack just
// sold) sells and buys again in the open shop, up to shelfRounds times. The caller must be standing near her.
func Shelf(d *fleetrunner.Deps, o ShelfOptions) (res ShelfResult) {
	started := time.Now()
	defer func() { res.Took = time.Since(started) }()
	want := o.Want
	if want <= 0 {
		want = runecraftplan.CarrierOffer
	}
	sh, err := OpenJiminua(d)
	if err != nil {
		res.Reason = "opening " + KeeperName + "'s shop: " + err.Error()
		return res
	}
	defer func() { _ = sh.Close() }()
	defer takeShelfTurn(d)()
	if !waitUntil(d, func() bool { return len(sh.Stock()) > 0 }, 5*time.Second) {
		res.Reason = "the shop opened with no stock panel"
		return res
	}
	stock := func() int {
		n := 0
		for _, it := range sh.Stock() {
			if int(it.ID) == runecraftplan.EssenceID {
				n += max(1, int(it.Count))
			}
		}
		return n
	}
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	at := started
	buyOnly := o.BuyOnly
	res.StockBefore, res.UnnotedBefore = stock(), Essence(d)
	for round := 1; round <= shelfRounds && Essence(d) < want; round++ {
		state := runecraftplan.ShelfState{
			Stock: stock(), FreeRow: len(sh.Stock()) < runecraftplan.ShopRows,
			Notes: Notes(d), Unnoted: Essence(d), Coins: Coins(d), PackFree: PackFree(d), Want: want, Floor: o.Floor,
		}
		if buyOnly {
			// The shop was found to have no room for a sale: buy what is on the shelf.
			state.Notes = 0
		}
		plan := runecraftplan.PlanShelf(state)
		if round == 1 {
			res.Plan = plan
		}
		res.Rounds = round
		if !plan.OK {
			if round == 1 {
				res.Reason = "no visit possible: " + plan.Why
			}
			break
		}
		coins0, notes0 := Coins(d), Notes(d)
		if plan.Sell > 0 {
			if _, err := sh.SellCount(runecraftplan.NotedEssenceID, int32(plan.Sell), w); err != nil {
				res.Reason = fmt.Sprintf("selling %d notes: %v", plan.Sell, err)
				break
			}
		}
		sold := notes0 - Notes(d)
		res.Sold += sold
		res.Income += Coins(d) - coins0
		if sold < plan.Sell && strings.Contains(strings.ToLower(said(d, at)), "run out of space") {
			res.OutOfSpace, buyOnly = true, true
		}
		coins1, have0 := Coins(d), Essence(d)
		taken := false
		if plan.Buy > 0 {
			// A sale that fell short buys what the shelf now holds.
			buy := plan.Buy
			if sold < plan.Sell {
				buy = min(buy, state.Stock+sold)
			}
			if buy > 0 {
				got, err := sh.BuyCount(int32(runecraftplan.EssenceID), int32(buy), w)
				switch {
				case errors.Is(err, actions.ErrItemNotFound) && sold > 0:
					// The sale landed and the row it made is gone: another shopper bought the stack. Not a failure of the visit: look
					// at the shelf again and sell and buy once more.
					taken = true
				case err != nil:
					res.Reason = fmt.Sprintf("buying %d essence: %v", buy, err)
				case int(got) < buy && sold > 0:
					taken = true // part of the stack went
				}
			}
			res.Bought += Essence(d) - have0
			res.Cost += coins1 - Coins(d)
		}
		if taken {
			res.Taken++
		}
		if res.Reason != "" || !taken || (res.OutOfSpace && res.Bought == 0) {
			break
		}
	}
	res.UnnotedAt = Essence(d)
	res.StockAfter = stock()
	switch {
	case res.Reason != "":
	case res.OutOfSpace && res.Bought == 0:
		res.Reason = "the shop has no room for the notes and none of the essence on the shelf was bought"
	case res.UnnotedAt < want && res.Rounds > 0 && res.Plan.OK && res.UnnotedAt-res.UnnotedBefore < res.Plan.Buy:
		res.Reason = fmt.Sprintf("bought %d of %d essence in %d round(s) (%d taken by other shoppers; shelf %d, pack holds %d)", res.Bought, res.Plan.Buy, res.Rounds, res.Taken, res.StockAfter, res.UnnotedAt)
	}
	return res
}

// Line is the shelf step's log line.
func (r ShelfResult) Line() string {
	rounds := ""
	if r.Rounds > 1 || r.Taken > 0 {
		rounds = fmt.Sprintf("; %d round(s), %d taken by other shoppers", r.Rounds, r.Taken)
	}
	return fmt.Sprintf("shelf: Jiminua stock %d Rune essence; sold %d notes for %d gp, bought %d for %d gp; shelf %d; pack %d unnoted; %s%s",
		r.StockBefore, r.Sold, r.Income, r.Bought, r.Cost, r.StockAfter, r.UnnotedAt, r.Took.Round(100*time.Millisecond), rounds)
}
