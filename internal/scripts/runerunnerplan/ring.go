package runerunnerplan

// The round: a fixed ring of stations with at most two world hops.
//
// M is the mule's world and O the other fleet world. In ring order:
//
//	1  deliver@M   Catherby bank and the mule's post
//	2  draw@M      Catherby bank: coins for the east leg
//	3  Betty@M     Port Sarim
//	4  Aubury@M    Varrock
//	   Zaff@M      Varrock, only with zaffStaffs > 0
//	5  hopO        at Varrock, once quiet            -+
//	6  Aubury@O                                       |
//	   Zaff@O      only with zaffStaffs > 0           |
//	7  Betty@O                                        | the O block
//	8  Lundail@O   through the Ardougne lever         |
//	9  hopM        in Ardougne, out of the Wilderness -+
//	10 Lundail@M
//
// and back to 1. The whole O block is left out of a round whose other world is not worth two
// hops (PlanRound), and a one-world buyer never has one, so a round makes at most two hops --
// inside worldhopplan's own bound of four per 30 minutes, two minutes apart.
//
// Travel is implicit: every station begins with its own walk (Catherby, Port Sarim, Varrock and
// Ardougne are all on scarabrunner.WalkTo's wolf-free boat chain).

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// Kind is what a station does.
type Kind string

const (
	KindDeliver Kind = "deliver"
	KindDraw    Kind = "draw"
	KindBuy     Kind = "buy"
	KindLundail Kind = "lundail"
	KindHop     Kind = "hop"
	// KindStaff is a Zaff visit for Staffs of air (the zaffStaffs setting).
	KindStaff Kind = "staff"

	// KindPurse is the mule top-up alone: at the mule's post it asks for coins when bank plus pack
	// is under topUpBelow and banks every coin, drawing none (rotation.go: a Lundail-first circuit
	// has no draw, and used to have no top-up either).
	KindPurse Kind = "purse"

	// KindMarket is the player-market station (market.go in the bot, marketstockplan): after the
	// draw, on the mule's world, it reads the Seers stalls' listings and, only when one lists a
	// rune the mule is short of at a price under the ceiling, walks to Seers and buys it.
	KindMarket Kind = "market"

	// KindAmmo is an ammo shop stop (ammo.go): Hickton in Catherby, Lowe in Varrock, Gerrant in Port Sarim.
	// It is never a hop's reason and never a rune shop: a rotation round places it beside the stations it
	// shares a walk with (BuildRotationRing), and a round that skips a block skips its ammo stops too.
	KindAmmo Kind = "ammo"
)

// Station is one stop of the ring.
type Station struct {
	Kind Kind `json:"kind"`
	// Site is the shop key for a buy, Lundail, staff or ammo station.
	Site string `json:"site,omitempty"`
	// World is where the station runs; for a hop, the world it goes to.
	World int `json:"world"`
}

// Key names a station in logs and in State.Done: "deliver@1", "draw@1", "Betty@1", "Hickton@1", "hop2".
func (s Station) Key() string {
	switch s.Kind {
	case KindHop:
		return fmt.Sprintf("hop%d", s.World)
	case KindBuy, KindLundail, KindStaff, KindAmmo:
		return fmt.Sprintf("%s@%d", s.Site, s.World)
	}
	return fmt.Sprintf("%s@%d", s.Kind, s.World)
}

// RingInput is what a ring is built from.
type RingInput struct {
	M, O int
	// Shops are the enabled sites (Settings.ShopKeys).
	Shops []string
	// OBlock includes stations 5-9.
	OBlock bool
	// Staffs adds a Zaff station after each Varrock visit.
	Staffs bool
	// Market adds the player-market station after the draw.
	Market bool
}

func enabled(shops []string, site string) bool {
	for _, s := range shops {
		if strings.EqualFold(s, site) {
			return true
		}
	}
	return false
}

// BuildRing lays out one round in the order of this file's doc comment, leaving out stations
// for shops that are not enabled, the draw when no pack-paid shop is, and the O block unless
// asked for and a distinct other world exists.
func BuildRing(in RingInput) []Station {
	en := func(site string) bool { return enabled(in.Shops, site) }
	ring := []Station{{Kind: KindDeliver, World: in.M}}
	if en(SiteBetty) || en(SiteAubury) {
		ring = append(ring, Station{Kind: KindDraw, World: in.M})
	}
	if in.Market {
		ring = append(ring, Station{Kind: KindMarket, World: in.M})
	}
	if en(SiteBetty) {
		ring = append(ring, Station{Kind: KindBuy, Site: SiteBetty, World: in.M})
	}
	if en(SiteAubury) {
		ring = append(ring, Station{Kind: KindBuy, Site: SiteAubury, World: in.M})
	}
	if in.Staffs {
		ring = append(ring, Station{Kind: KindStaff, Site: SiteZaff, World: in.M})
	}
	if in.OBlock && in.O > 0 && in.O != in.M {
		ring = append(ring, Station{Kind: KindHop, World: in.O})
		if en(SiteAubury) {
			ring = append(ring, Station{Kind: KindBuy, Site: SiteAubury, World: in.O})
		}
		if in.Staffs {
			ring = append(ring, Station{Kind: KindStaff, Site: SiteZaff, World: in.O})
		}
		if en(SiteBetty) {
			ring = append(ring, Station{Kind: KindBuy, Site: SiteBetty, World: in.O})
		}
		if en(SiteLundail) {
			ring = append(ring, Station{Kind: KindLundail, Site: SiteLundail, World: in.O})
		}
		ring = append(ring, Station{Kind: KindHop, World: in.M})
	}
	if en(SiteLundail) {
		ring = append(ring, Station{Kind: KindLundail, Site: SiteLundail, World: in.M})
	}
	return ring
}

// OBlock finds the O block: the index of its opening hop and of its closing hop.
func OBlock(ring []Station) (open, close int, ok bool) {
	open = -1
	for i, s := range ring {
		if s.Kind != KindHop {
			continue
		}
		if open < 0 {
			open = i
			continue
		}
		return open, i, true
	}
	return -1, -1, false
}

// Hops counts the hop stations in a ring.
func Hops(ring []Station) int {
	n := 0
	for _, s := range ring {
		if s.Kind == KindHop {
			n++
		}
	}
	return n
}

// Index is the position of the station with this key, -1 when the ring has none.
func Index(ring []Station, key string) int {
	for i, s := range ring {
		if s.Key() == key {
			return i
		}
	}
	return -1
}

// RoundInput is what PlanRound decides the O block from.
type RoundInput struct {
	M, O            int
	Shops           []string
	MaxHopsPerRound int
	HopMinGain      int
	// Staffs adds the Zaff stations (they never decide the O block: runes do).
	Staffs bool
	// Market adds the player-market station after the draw.
	Market bool
	// HopsAvailable is false when this account cannot hop at all (no fleet hook, one world, a
	// refused hop earlier in the run).
	HopsAvailable bool
	// Worth is the runes a station would buy under the caps from the board's estimate for that
	// shop on that world (Worth over the estimate).
	Worth func(site string, world int) int
}

// RoundPlan is PlanRound's answer.
type RoundPlan struct {
	Ring   []Station
	OBlock bool
	// OWorth is the expected purchase on the other world (0 when not computed).
	OWorth int
	Why    string
}

// OWorth sums the expected purchase at the O block's shop stations.
func OWorth(in RoundInput) int {
	if in.Worth == nil || in.O <= 0 {
		return 0
	}
	total := 0
	for _, site := range []string{SiteAubury, SiteBetty, SiteLundail} {
		if enabled(in.Shops, site) {
			total += in.Worth(site, in.O)
		}
	}
	return total
}

// PlanRound builds a round's ring, with the O block only when it is worth its two hops: a
// distinct other world, hops possible, maxHopsPerRound 2, and an expected purchase there of at
// least hopMinGain. The O block is checked again when its opening hop comes up (the cohort buys
// too), so this is the plan, not a promise.
func PlanRound(in RoundInput) RoundPlan {
	p := RoundPlan{}
	switch {
	case in.O <= 0 || in.O == in.M:
		p.Why = "one world"
	case in.MaxHopsPerRound < 2:
		p.Why = "maxHopsPerRound 0: one-world buyer"
	case !in.HopsAvailable:
		p.Why = "hops unavailable"
	default:
		p.OWorth = OWorth(in)
		if p.OWorth >= in.HopMinGain {
			p.OBlock = true
			p.Why = fmt.Sprintf("world %d expects %d runes (hopMinGain %d)", in.O, p.OWorth, in.HopMinGain)
		} else {
			p.Why = fmt.Sprintf("world %d expects %d runes, under hopMinGain %d", in.O, p.OWorth, in.HopMinGain)
		}
	}
	p.Ring = BuildRing(RingInput{M: in.M, O: in.O, Shops: in.Shops, OBlock: p.OBlock, Staffs: in.Staffs, Market: in.Market})
	return p
}

// WorthVisiting is the skip rule for one station, checked again just before it starts: a visit
// is walked to only for at least minVisit runes under the caps.
func WorthVisiting(worth, minVisit int) (bool, string) {
	if worth < minVisit {
		return false, fmt.Sprintf("expects %d runes under the caps, under minVisitRunes %d", worth, minVisit)
	}
	return true, ""
}

// PlanString is the ring from `from` to its end, as the round log line prints it.
func PlanString(ring []Station, from int) string {
	if from < 0 {
		from = 0
	}
	keys := make([]string, 0, len(ring))
	for i := from; i < len(ring); i++ {
		keys = append(keys, ring[i].Key())
	}
	return "[" + strings.Join(keys, " ") + "]"
}

// ---- where the account is ----

// Area is where the account is, as the ring cares about it.
type Area string

const (
	AreaUnknown  Area = "unknown"
	AreaWild     Area = "wilderness"
	AreaArdougne Area = "ardougne"
	AreaEast     Area = "east"
	AreaWest     Area = "west"
)

// EastX is the x from which a surface tile counts as the east side: Port Sarim, Draynor,
// Lumbridge (a respawn), Varrock and everything between.
const EastX = 2900

// ArdougneBankReach is how close to the East Ardougne bank still counts as Ardougne.
const ArdougneBankReach = 30

// AreaOf classifies a tile. The Wilderness and the Mage Bank come first (the Lundail
// sub-machine owns them), then Ardougne (the lever and the bank), then east of EastX, and
// everything else -- Catherby, Seers', the land between -- is west.
func AreaOf(t *tile.Tile) Area {
	if t == nil {
		return AreaUnknown
	}
	switch {
	case fleetplan.InMageBank(*t) || fleetplan.WildernessLevel(*t) > 0:
		return AreaWild
	case fleetplan.NearArdougneLever(*t) || fleetplan.InArdougneReach(*t) ||
		(t.Level == 0 && tile.Chebyshev(tile.Tile{X: t.X, Z: t.Z}, ArdougneBank) <= ArdougneBankReach):
		return AreaArdougne
	case t.X >= EastX:
		return AreaEast
	}
	return AreaWest
}

func firstWhere(ring []Station, pred func(Station) bool) int {
	for i, s := range ring {
		if pred(s) {
			return i
		}
	}
	return -1
}

// Entry is where a round picks up from a standing start -- a first start, a restart that lost
// its place, or a respawn -- given where the account is and which world it is on:
//
//   - the Wilderness or the Mage Bank: that world's Lundail station (its sub-machine finishes
//     the trip first);
//   - Ardougne: that world's Lundail station (on the other world, else its closing hop);
//   - the east side: that world's first shop station (Betty or Aubury), else its Lundail;
//   - the west on the mule's world: the delivery; on the other world, its Lundail.
//
// Anything else starts the ring at the delivery, whose own gate hops to the mule's world.
func Entry(ring []Station, area Area, world, m int) (int, string) {
	pick := func(why string, preds ...func(Station) bool) (int, string) {
		for _, p := range preds {
			if i := firstWhere(ring, p); i >= 0 {
				return i, why
			}
		}
		return 0, why + "; no station suits it, starting at the delivery"
	}
	lundailHere := func(s Station) bool { return s.Kind == KindLundail && s.World == world }
	buyHere := func(s Station) bool { return s.Kind == KindBuy && s.World == world }
	hopHome := func(s Station) bool { return s.Kind == KindHop && s.World == m && world != m }
	switch area {
	case AreaWild:
		return pick("in the Wilderness: finishing the Mage Bank trip", lundailHere)
	case AreaArdougne:
		return pick(fmt.Sprintf("in Ardougne on world %d", world), lundailHere, hopHome)
	case AreaEast:
		return pick(fmt.Sprintf("east on world %d", world), buyHere, lundailHere, hopHome)
	case AreaWest:
		if world == m {
			return 0, fmt.Sprintf("west on the mule's world %d", world)
		}
		return pick(fmt.Sprintf("west on world %d", world), lundailHere, hopHome)
	}
	return 0, "position unknown"
}

// Resume is where a saved round continues after the script restarts (a hop restarts it by
// design). The saved cursor stands unless the account is back on the mule's world in the middle
// of the O block -- the hop out did not land -- when the rest of the block is skipped. A station
// for another world is otherwise left to its own executor, which hops (a hop or the delivery) or
// skips it (a shop).
func Resume(st State, area Area, world, m int) (int, string) {
	if len(st.Ring) == 0 {
		return Entry(st.Ring, area, world, m)
	}
	if st.Cursor < 0 || st.Cursor >= len(st.Ring) {
		return 0, "saved cursor out of range: starting at the delivery"
	}
	c := st.Ring[st.Cursor]
	if st.Rotation {
		// Every rotation shop station has its own hop just before it, and runs only on its world,
		// so the saved cursor always stands.
		return st.Cursor, "resuming at " + c.Key()
	}
	if open, close, ok := OBlock(st.Ring); ok && world == m && st.Cursor > open && st.Cursor < close && c.World != world {
		return close + 1, fmt.Sprintf("on world %d inside the other world's block: the hop did not land, skipping to %s", world, keyAt(st.Ring, close+1))
	}
	return st.Cursor, "resuming at " + c.Key()
}

func keyAt(ring []Station, i int) string {
	if i >= 0 && i < len(ring) {
		return ring[i].Key()
	}
	return "the end of the round"
}

// ---- idling ----

// MinIdle is the shortest idle wait: a floor so a line whose regrowth is already due cannot
// spin the ring.
const MinIdle = time.Minute

// FailureIdle is the shortest wait after a round that bought and delivered nothing AND had a
// station fail (a walk, a bank, a keeper, a hop). The restock estimate cannot bound that wait: a
// shelf nobody reached is still estimated full, so IdleWait would answer MinIdle, and a buyer whose
// walks fail fast would go round the ring -- and its two hops -- every minute or so.
const FailureIdle = 5 * time.Minute

// RoundIdle is the wait after an unproductive round: IdleWait's answer, raised to FailureIdle when
// the round had failures, and never above maxWait.
func RoundIdle(wait, maxWait time.Duration, failures int) time.Duration {
	if failures > 0 && wait < FailureIdle {
		wait = FailureIdle
	}
	if wait > maxWait {
		wait = maxWait
	}
	if wait < MinIdle {
		wait = MinIdle
	}
	return wait
}

// Failed reports whether a station outcome is a failure (for RoundIdle), not a skip by rule.
func Failed(outcome string) bool {
	return outcome == OutcomeWalkFailed || outcome == OutcomeAborted
}

// WaitLine is one shop line the idle wait considers: its shop, rune, the estimated stock now and
// the rune's cap.
type WaitLine struct {
	Shop fleetplan.RuneShop
	Rune string
	Est  int
	Cap  int
}

// IdleWait is how long to wait when nothing in the ring is worth a visit: min(maxWait, the
// smallest StockLine.RestockWait for a line to regrow minVisit runes), never below MinIdle.
// Lines whose cap can never buy minVisit runes even from a full shelf are left out; with no line
// left the wait is maxWait.
func IdleWait(maxWait time.Duration, lines []WaitLine, minVisit int, tick time.Duration) time.Duration {
	best := maxWait
	for _, wl := range lines {
		l, ok := wl.Shop.Line(wl.Rune)
		if !ok || wl.Cap <= 0 {
			continue
		}
		if n, _ := BuyableUnderCap(wl.Shop, l.Rune, l.Base, wl.Cap); n < minVisit {
			continue
		}
		if w := l.RestockWait(wl.Est, wl.Est+minVisit, tick); w < best {
			best = w
		}
	}
	if best < MinIdle {
		best = MinIdle
	}
	return best
}
