package runecraftplan

// THE GATE'S ALCHEMY (a user request: "the main account could alch with fire staff and noted yew or magic strung bows can be alched inbetween
// trades"). MasterCrafter casts High Level Alchemy on noted STRUNG bows (Magic longbow, Magic shortbow, Yew longbow, Yew shortbow) with his
// Staff of fire wielded (the Fire runes) and the Nature runes he crafts, at the Nature gate. This file is the decisions, all pure.
//
// WHAT THE CONTENT SAYS (content 289 skill_magic/scripts/spells/alchemy.rs2, interface_trade/scripts/trade.rs2, engine OpPlayerHandler):
//
//   - The spell opens with `if_close`, so a cast with a trade screen open CLOSES the screen for both players.
//   - It ends in p_delay(3): the player is delayed from the tick after the cast for four ticks and the first packet the engine
//     accepts is the fifth tick's (AlchLockTicks). A delayed player is `busy`: a carrier's trade request is refused ("<name> is busy at
//     the moment") and the crafter's own answer to a request (an OPPLAYER4) is dropped without a word. So a cast is never sent while a
//     request is waiting, an answer is out or a screen is open, and never when a carrier could be asking before the lock is over.
//   - The pay is 0.6 of the item's cost (max(scale(6,10,cost),1)): a Magic shortbow pays 960 coins, a Yew longbow 768. A bank note
//     is alchable (it carries its item's cost and name), so a whole stack is one pack slot.
//
// THE PACK. The gate's trade is 25 essence into 25 free slots, and the pack holds the talisman, the Nature stack and the fare: 3
// slots (GateKeepSlots). A stack of noted bows is a 4th slot, and with it a trade of 25 is refused by the engine (enough_trade_space:
// "Other player does not have enough inventory space for this trade") and the carriers have to bring 24, 4 percent less essence on
// every trade. Two modes follow, the first the default because it costs the gate nothing:
//
//   - ROAD (craftRuneAlchHold off): "alching must never cost the gate's throughput" means the stack is GONE before a carrier can trade.
//     It is drawn at the start of the round, cast away on the road to the gate (the walk's casts cost no time: a cast does not stop a
//     walk) and, if some is left on arrival, before the first trade is answered (BurnDown). The gate then trades 25 as always.
//   - HOLD (craftRuneAlchHold on): the stack is kept through the trades and cast in the idle seconds between them (DecideGateAlch's
//     clearance rule). The gate says on its message that it takes 24 a trade (mulebus.GateView.Capacity) and the carriers load 24
//     (runecraftplan.GateInfo.ShelfWant): 4 percent of the essence for the casts.

import (
	"strconv"
	"strings"
	"time"
)

// The alchemy's numbers.
const (
	// AlchMagic is High Level Alchemy's Magic level (alchplan.HighAlch).
	AlchMagic = 55
	// AlchLockTicks is the ticks one cast locks the player for: p_delay(3) delays him four ticks and the fifth tick's packets are the
	// first read (alchplan.HighAlch.LockTicks).
	AlchLockTicks = 5
	// AlchClearTiles is how close a visible carrier must be to stop a cast: one that could be next to the crafter and asking before
	// the lock is over. A carrier covers two tiles a tick at the very most, so the tiles it covers in the lock plus the tile it
	// stands beside the stand on.
	AlchClearTiles = 2*AlchLockTicks + 2
	// AlchNaturesFloor is the Nature runes the pack keeps: a gate round never casts its stock to nothing (the chest round and the
	// superheat live on it).
	AlchNaturesFloor = 50
	// DefaultGateAlch is craftRuneGateAlch's default; DefaultAlchBows craftRuneAlchBows': the bows one round draws. The road to the
	// gate (about 10 minutes of walking, a cast every 5 ticks) takes 100 casts, so this is cast away on the way.
	DefaultGateAlch = true
	DefaultAlchBows = 100
	// AlchRoadCasts is the casts the walk to the gate takes at most: 12 minutes of walking at one cast per five ticks. A round draws no
	// more than the road can cast away (craftRuneAlchBows is bounded by it), because a stack that is still held at the gate holds the
	// first trade up.
	AlchRoadCasts = 140
	MaxAlchBows   = AlchRoadCasts
	// MaxAlchBowsHold is the stack a round that holds its bows through the trades (craftRuneAlchHold) draws at most.
	MaxAlchBowsHold = 2000
	// AlchAskFactor is how many rounds' worth of bows a mule ask brings, so the bank holds the next rounds' too.
	AlchAskFactor = 4
	// AlchBurnMax is the longest a stack left on arrival is waited on before the round gives it up and ends (the bank takes the
	// stack back at the post): a whole kit of 100 bows if the road cast none is 5 minutes of casts, so a burn that is going on is not cut off.
	AlchBurnMax = 6 * time.Minute
	// AlchBurnWarn is the stack left on arrival that is worth a warning: at most a minute of casts.
	AlchBurnWarn = 20
	// AlchFirstDraw is the bows the first round draws, before the road has been measured: the way from the post to the gate took 5m05s on
	// 16 Oct 2026 and the ship and the teleports in it cast nothing, so the walk casts about 60 times. A stack that is left at the gate
	// holds the first trade up (BurnDown), so the draw follows what the road really takes (AlchRoadDraw).
	AlchFirstDraw = 60
	// AlchMinDraw is the fewest bows a measured round draws, and AlchDrawMargin the bows beyond the last road's casts.
	AlchMinDraw    = 20
	AlchDrawMargin = 5
)

// AlchBow is one item the gate casts on and its content cost. ID names the obj when the name is shared (an unstrung bow); 0 is the
// strung bow of the name, or the one obj of a name no other obj shares.
type AlchBow struct {
	Name string
	Cost int
	ID   int
}

// AlchBows are the strung bows the gate first cast on, dearest first. The gate now draws everything on the fleet's alch list
// (runecraftrunner.ResolveAlchBows, a user request); these stay the bows PlanAlchDraw and the tests name.
var AlchBows = []AlchBow{{"Magic longbow", 2560, 0}, {"Magic shortbow", 1600, 0}, {"Yew longbow", 1280, 0}, {"Yew shortbow", 800, 0}}

// Key is the item's name, qualified by its obj ("Yew longbow id66") when ID names one.
func (b AlchBow) Key() string {
	if b.ID > 0 {
		return b.Name + " id" + strconv.Itoa(b.ID)
	}
	return b.Name
}

// AlchBowNames are AlchBows' names, dearest first.
func AlchBowNames() []string {
	out := make([]string, 0, len(AlchBows))
	for _, b := range AlchBows {
		out = append(out, b.Name)
	}
	return out
}

// IsAlchBow reports whether name is one of the bows (case-insensitive).
func IsAlchBow(name string) bool {
	for _, b := range AlchBows {
		if strings.EqualFold(strings.TrimSpace(name), b.Name) {
			return true
		}
	}
	return false
}

// AlchPays is what one cast on an item of that cost pays: 0.6 of it, at least 1 (alchemy.rs2).
func AlchPays(cost int) int { return max(cost*6/10, 1) }

// AlchDraw is one row of a kit: Qty of the bow Name.
type AlchDraw struct {
	Name string
	Qty  int
}

// PlanAlchDraw chooses the bows a visit draws: want in all, the dearest first, no more of a bow than have(name) says the bank holds.
// The draw may be short of want (the bank holds what it holds).
func PlanAlchDraw(have func(name string) int, want int) []AlchDraw {
	var out []AlchDraw
	left := want
	for _, b := range AlchBows {
		if left <= 0 {
			break
		}
		n := min(left, have(b.Key()))
		if n > 0 {
			out = append(out, AlchDraw{b.Key(), n})
			left -= n
		}
	}
	return out
}

// AlchBowsLimit is the most craftRuneAlchBows may name: what the road can cast away, or with the stack held through the trades
// (hold) the bows a long round casts in its idle seconds.
func AlchBowsLimit(hold bool) int {
	if hold {
		return MaxAlchBowsHold
	}
	return MaxAlchBows
}

// GateAlchKit is the bows a round should draw: the setting, but 0 when the account cannot cast (Magic under AlchMagic, no fire staff
// worn, too few Natures to cast with), and, when the stack is not held through the trades, under AlchRoadCasts (and two thirds of it
// when loaded carriers are already waiting at the gate), so the trip alone takes the stack away before the first trade: nothing waits on
// a stack at the gate. natures is the Nature runes the pack holds after the visit.
func GateAlchKit(on, hold bool, want, magic, natures int, fireStaff bool, carriersWaiting int) int {
	if !on || want <= 0 || magic < AlchMagic || !fireStaff {
		return 0
	}
	n := min(want, AlchBowsLimit(hold))
	if !hold && carriersWaiting > 0 {
		n = min(n, AlchRoadCasts*2/3)
	}
	// A cast needs a Nature rune, and the pack keeps a floor.
	return max(0, min(n, natures-AlchNaturesFloor))
}

// AlchRoadDraw is the bows a round that casts its stack away on the road draws: craftRuneAlchBows (want) at most, and no more than the
// road is expected to take, because every bow still held at the gate is three seconds the first trade waits. The first round (nothing
// measured) draws AlchFirstDraw; after a round that left bows at the gate the road's own casts plus a margin; after one that cast
// everything it drew, a fifth more than that (the road may take more), so the draw settles on the road's real casts.
func AlchRoadDraw(want, lastDrawn, lastCasts, lastLeft int) int {
	n := AlchFirstDraw
	if lastDrawn > 0 {
		if lastLeft > 0 {
			n = lastCasts + AlchDrawMargin
		} else {
			n = lastDrawn + max(AlchDrawMargin, lastDrawn/5)
		}
	}
	return min(want, max(AlchMinDraw, n))
}

// GateAlchView is what DecideGateAlch reads.
type GateAlchView struct {
	// Enabled is the setting; Magic the level; FireStaff a fire staff worn; Natures the Nature runes in the pack; Stack the bows held.
	Enabled   bool
	Magic     int
	FireStaff bool
	Natures   int
	Stack     int
	// TradeOpen: a trade screen is open. Pending: request lines waiting to be answered. Awaiting: an answer is out and its screen
	// not seen.
	TradeOpen bool
	Pending   int
	Awaiting  bool
	// Locked: the last cast's lock is not over.
	Locked bool
	// Busy: a random event is on screen.
	Busy bool
	// Nearest is the tiles to the nearest carrier in view that did not trade a moment ago (-1: none in view).
	Nearest int
	// Burning says the stack must be gone before the gate trades (BurnDown): the carriers wait on it, so the clearance rule is waived,
	// and the gate answers nobody meanwhile (GateOptions.Hold), so a request that waits is waived too: it cannot be answered before the
	// bows are gone, and the bows cannot go while it blocks the cast. (Reviewed 16 Oct 2026: with one carrier asking, a pending request
	// held the first trade for ever, the stuck clock ended the round and the alchemy was off for six hours.)
	Burning bool
}

// GateAlchDecision is DecideGateAlch's answer.
type GateAlchDecision struct {
	Cast bool
	Why  string
}

// DecideGateAlch says whether a cast goes out now. It never sends one that could close a screen or drop the crafter's answer: no screen
// is open, no answer is out and the last cast's lock is over. Unless the stack is being burned down before the gate trades, it also
// leaves a request that waits to be answered first and keeps clear of a carrier near enough to be asking before this one's lock is over;
// a burn-down answers nobody (a request is refused as "busy" for the four ticks of a cast and asked again) so it casts through both.
func DecideGateAlch(v GateAlchView) GateAlchDecision {
	no := func(why string) GateAlchDecision { return GateAlchDecision{Why: why} }
	switch {
	case !v.Enabled:
		return no("gate alchemy is off")
	case v.Stack <= 0:
		return no("no bows held")
	case v.Magic < AlchMagic:
		return no("Magic " + strconv.Itoa(v.Magic) + " is under the " + strconv.Itoa(AlchMagic) + " High Level Alchemy needs")
	case !v.FireStaff:
		return no("no fire staff is worn for the Fire runes")
	case v.Natures <= 0:
		return no("no Nature rune to cast with")
	case v.TradeOpen:
		return no("a trade screen is open (the cast would close it)")
	case v.Awaiting:
		return no("an answer to a trade request is out")
	case v.Pending > 0 && !v.Burning:
		return no(strconv.Itoa(v.Pending) + " trade request(s) wait to be answered")
	case v.Busy:
		return no("a random event is on screen")
	case v.Locked:
		return no("the last cast's lock is not over")
	case !v.Burning && v.Nearest >= 0 && v.Nearest <= AlchClearTiles:
		return no("a carrier is " + strconv.Itoa(v.Nearest) + " tiles away: it could be asking before the lock is over")
	}
	return GateAlchDecision{Cast: true, Why: "nothing is waiting and no carrier is near"}
}

// BurnDown says the gate must not answer a trade request now: a stack of bows is held (a 4th slot besides the talisman, the Natures and
// the fare, so a trade of 25 essence would be refused) and casting it away can still go on. Once the casting cannot go on (stuck) or the
// burn has run for AlchBurnMax the gate gives it up and the round ends, see AlchStuck.
func BurnDown(stack int, canCast bool, burning time.Duration) bool {
	return stack > 0 && canCast && burning < AlchBurnMax
}

// AlchStuck says why the round must end because the stack cannot be cast away: bows are held and either the casting cannot go on or
// the burn has run for AlchBurnMax. "" while it can. (The bank at the mule's post takes the stack back; a gate with bows in the pack
// can only trade 24 essence, which the carriers do not bring.)
func AlchStuck(stack int, canCast bool, burning time.Duration) string {
	switch {
	case stack <= 0:
		return ""
	case !canCast:
		return strconv.Itoa(stack) + " noted bow(s) are held and cannot be cast away: they take a pack slot a trade of 25 essence needs"
	case burning >= AlchBurnMax:
		return strconv.Itoa(stack) + " noted bow(s) are still held after " + burning.Round(time.Second).String() + " of casting: they take a pack slot a trade of 25 essence needs"
	}
	return ""
}

// GateAlchLine is the log line of the round's alchemy so far.
func GateAlchLine(casts, coins, xp int, castSecs time.Duration, left int, bow string) string {
	b := strings.Builder{}
	b.WriteString("alch: ")
	b.WriteString(Thousands(casts))
	b.WriteString(" High Level Alchemy cast(s), ")
	b.WriteString(Thousands(coins))
	b.WriteString(" gp, ")
	b.WriteString(Thousands(xp))
	b.WriteString(" Magic xp, ")
	b.WriteString(castSecs.Round(time.Second).String())
	b.WriteString(" of casting; ")
	b.WriteString(Thousands(left))
	b.WriteString(" bow(s) left")
	if bow != "" {
		b.WriteString(" (")
		b.WriteString(bow)
		b.WriteString(")")
	}
	return b.String()
}
