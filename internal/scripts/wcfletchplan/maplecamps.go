package wcfletchplan

// Where the maple rung chops: every safe maple camp around the Seers bank, the willow camps it
// overflows to when the maples cannot feed it, and the board that seats the cohort over them.
//
// # The loss this exists for (live build e35420b6, 2026-09-25)
//
// The biggest XP loss on the live fleet. 79 ProgressiveWCFletch accounts were in game, every
// one of them on the MAPLE rung (Woodcutting 61-74, Fletching 50-63: the ladder holds an account
// on maples until it can cut yew shortbows at Fletching 65), and 70 of them stood at the one
// Seers maple camp logging "no maple tree in sight — waiting for one to grow back". Over a 13
// minute window of two panel snapshots the whole cohort gained 190,881 Woodcutting and 102,967
// Fletching experience an hour: 1,909 maple logs an hour between 79 accounts, 3,720 combined XP/h
// each (median 3,595), against 21,059 for the fleet's fishers. The Seers willows, a 32-step walk
// from the bank, had NOBODY at them.
//
// THE CAMP IS SUPPLY-BOUND, and that is content. A maple is a 1-in-8 deplete on a 120-tick stump
// (woodcut.rs2 get_logs; woodcutplan/capacity.go has the lines), so the four trees give about
// 1,716 logs an hour at this world's size, however many accounts wait for them. The measured
// 1,909 is that and a little more (an account standing at the bank is "at" this camp, and can
// reach the maple at 2729,3481 from there). No choice of which maple to click there buys more;
// only more trees can.
//
// # Every option, priced before choosing
//
// With CohortXP (chopmodel.go) over the live cohort's own 79 level pairs, rune axes, a world of
// 270, each arrangement being this board's own seating restricted to those camps
// (TestMapleOptionsArePriced recomputes every row and fails if one moves by 3%):
//
//	arrangement                                        cohort XP/h   per account
//	all 79 at the Seers maples (the live behaviour)        265k          3.4k    measured 294k / 3.7k
//	the three maple camps beside the bank                  595k          7.5k
//	every maple camp (mapleOverflow off)                 1,248k         15.8k
//	the three near maple camps + Seers willows           1,521k         19.2k
//	every maple camp + Seers willows                     1,872k         23.7k
//	every camp, both willow camps (THE DEFAULT)          2,147k         27.2k
//	willows only, no maples                              1,667k         21.1k
//
// What each line is made of:
//
//   - MORE MAPLES. Of the 89 maples within 140 tiles of the bank, 21 are safe and in reach and
//     are in the five maple camps below, about 9,000 logs an hour between them. The rest: 29 in
//     the north-west field's FENCED northern half, whose only door-free route from the bank
//     passes the Seers Highwaymen; 29 in the field north of Seers among the Rellekka-road wolves
//     (content npc 95; several stand tiles inside a wolf's own reach) and behind the Highwaymen;
//     six far to the south, 110-125 tiles out and outside SeersRadius; one with no route at
//     all; and three that are safe alone but that no camp can be drawn round without handing out
//     a tree from the fenced half (safeButCampless in maplecamps_data_test.go).
//   - WILLOWS. 30 ticks on the stump with the same 1-in-8 deplete, so one willow gives four times
//     a maple's supply, and a rune axe lands one about twice as often. A willow log is 67.5 +
//     41.5 (willow longbow) = 109 xp against 100 + 58.3 = 158.3 for a maple, so an UNCONTENDED
//     account earns about the same at either (maple 48k, willow 51k, bank trips included;
//     TestAccountRateCostsTheWalk). What differs is supply: the six Seers willows give about
//     10,300 logs an hour, more than every maple camp together, and the five Catherby willows (73
//     tiles out) about 8,600. So once the maples are crowded the next account is worth more at a
//     willow than as one more at a maple, and the board sends it there. It keeps fletching: its
//     product becomes the willow longbow (Fletching 40), which every account on this rung can cut
//     (TestOverflowStillFletchesWhatItChops).
//   - YEWS, for the five accounts at Woodcutting 70+ (the only ones the owner's raised gate lets
//     chop them). They are at Fletching 51-63, below the yew shortbow's 65, so a yew pays 175
//     Woodcutting and NOTHING else: about 32k Woodcutting xp/h alone at the church yews against
//     26-32k combined at their seats here, so about +5k xp/h each, 25k on the cohort (1%). And it
//     would freeze them: the ladder moves an account to yews at FLETCHING 65, and a yew pays no
//     Fletching, so an account overflowed to yews would stay on the maple rung for good. Not built
//     (TestYewsForTheFewArePricedAndRefused prices it).
//   - MARGINAL rather than share seating (each account where the COHORT gains most, not where
//     its own share is best) priced 0.8% better in the offline model, for a rule that leaves some
//     accounts at half the others' rate. Share seating is what PatchBoard already does; kept.
//
// The supply and the swing are content; the round trip is an estimate (chopmodel.go says which
// numbers). The status-quo line is 10% under the measurement, the check the model was given
// (TestStatusQuoMatchesTheMeasurement). Every other line is a PREDICTION: nothing in this table
// has been measured on the new arrangement, in the lab or live.
//
// # What the owner decided, and what this does not change
//
// The ladder is the owner's ("stay at maples until we can cut yew logs into shortbows"), and it
// stands: TierIndex still puts these accounts on the maple rung, a seat at a maple camp is that
// rung exactly, and the moment an account can cut yew shortbows it leaves the board. The
// overflow is the one place the rung's LOGS change, and only while the maples have no room;
// the setting mapleOverflow turns it off (default ON), and spreadMaple turns the whole board off
// (default ON; off is the one-camp behaviour before this file).
//
// # Every camp below was chosen, not guessed
//
// Each is a cluster of one tree table's locs found by clustering the static loc index around
// the Seers bank, held to patches.go's rules and one more:
//
//   - every counted tree has a stand tile the collision pack routes to from the Seers bank with
//     no door on the way;
//   - every counted stand tile keeps 3 tiles outside every hunting NPC's reach, and the route
//     from the bank and every walk from the anchor to a tree the account could be handed keep 2
//     (patches_data_test.go's table of hunters, read out of content);
//   - no tree the account could be handed is a DETOUR (at most twice the straight line plus 30);
//   - AND no stand tile, route tile or in-camp walk tile lies in a webwalk danger zone that
//     applies to a combat-3 account. That is stricter than patches.go's box test where it
//     matters (it checks the tiles actually walked) and it is what admits the ladder's own willow
//     and maple camps, whose generous boxes clip the Seers Highwaymen's square while no tile an
//     account there stands on or walks does.
//
// TestMapleCampsAgreeWithTheLocIndex re-derives every Trees and Steps figure and every one of
// those rules from the real loc index and pack, the same way TestPatchesAgreeWithTheLocIndex
// does, so a content or pack change fails a test rather than quietly sending accounts to a camp
// that is not there.

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// MapleRung and WillowRung are the ladder positions of the maple and willow tiers.
var (
	MapleRung  = mustRung(MapleLogsID)
	WillowRung = mustRung(WillowLogsID)
)

func mustRung(logsID int) int {
	idx, ok := RungIndexForLogs(logsID)
	if !ok {
		panic("wcfletchplan: the ladder has no rung for log " + strconv.Itoa(logsID))
	}
	return idx
}

// Camp is one place a board's rung chops: the maple rung (and its willow overflow) over MapleCamps,
// the yew rung over YewCamps, the magic rung over MagicCamps.
type Camp struct {
	// Name is what the log line and the panel call it.
	Name string
	// Rung is the ladder rung whose logs the camp yields: MapleRung, or WillowRung for an
	// overflow camp, or YewRung. The account chops, cuts and banks exactly as that rung does while
	// seated.
	Rung int
	// Loc is the display name woodcutrunner.Chop searches for.
	Loc string
	// Anchor is a walkable tile the pack routes to from the Seers bank.
	Anchor tile.Tile
	// Leash is how far from Anchor a counted tree stands at most.
	Leash int
	// Trees is how many Loc locs stand within Leash of Anchor with a stand tile routable from the
	// bank: the camp's share of supply.
	Trees int
	// Steps is the door-free walk from the camp's bank floor (Bank.Floor; the Seers bank's is
	// 2725,3491) to Anchor, which is what a load costs to carry back.
	Steps int
	// Home marks the rung's own woodcutplan camp, TIERS unchanged.
	Home bool
	// Bank is where the camp's loads are banked and its hand-offs drawn. The zero value is the
	// Seers bank (BankOf).
	Bank CampBank
}

// BankOf is the camp's bank: Bank, or SeersBank when the camp names none.
func (c Camp) BankOf() CampBank {
	if c.Bank.Name == "" {
		return SeersBank
	}
	return c.Bank
}

// Overflow reports whether the camp yields the logs of a rung that has no board of its own: the
// maple board's willow camps. A maple camp is never overflow, and neither is a yew or a magic camp
// (the yew and magic boards, yewcamps.go and magiccamps.go, hold their own rung's camps only, so an
// account seated on either never changes what it chops).
func (c Camp) Overflow() bool { return c.Rung != MapleRung && c.Rung != YewRung && c.Rung != MagicRung }

// Tier is the camp as a woodcutplan.Tier, so woodcutrunner.Chop, WalkNear and AtCamp run on it
// unchanged: the camp's rung's own tier with the place and loc name replaced.
func (c Camp) Tier() woodcutplan.Tier {
	t := Ladder[c.Rung].Tier
	t.Loc, t.Anchor, t.Leash = c.Loc, c.Anchor, c.Leash
	return t
}

// MapleCamps is every camp the maple rung may be seated at: the four maple camps first (the
// ladder's own first), then the willow overflow (the ladder's own willow camp first).
//
// The distance in each comment is the door-free walk from the Seers bank, Steps.
var MapleCamps = []Camp{
	{Name: "Seers maples", Rung: MapleRung, Loc: "Maple tree", Anchor: Ladder[MapleRung].Tier.Anchor, Leash: Ladder[MapleRung].Tier.Leash, Trees: 4, Steps: 27, Home: true}, // the ladder's own camp
	{Name: "Seers south maples", Rung: MapleRung, Loc: "Maple tree", Anchor: tile.Tile{X: 2724, Z: 3473}, Leash: 8, Trees: 3, Steps: 18},                                    // 18
	{Name: "Camelot maples", Rung: MapleRung, Loc: "Maple tree", Anchor: tile.Tile{X: 2748, Z: 3466}, Leash: 3, Trees: 2, Steps: 36},                                        // 36
	{Name: "north-west field", Rung: MapleRung, Loc: "Maple tree", Anchor: tile.Tile{X: 2654, Z: 3525}, Leash: 7, Trees: 8, Steps: 81},                                      // 81
	{Name: "north-west field east", Rung: MapleRung, Loc: "Maple tree", Anchor: tile.Tile{X: 2662, Z: 3532}, Leash: 3, Trees: 4, Steps: 74},                                 // 74
	{Name: "Seers willows", Rung: WillowRung, Loc: "Willow", Anchor: Ladder[WillowRung].Tier.Anchor, Leash: Ladder[WillowRung].Tier.Leash, Trees: 6, Steps: 32, Home: true}, // the ladder's own willow camp
	{Name: "Catherby willows", Rung: WillowRung, Loc: "Willow", Anchor: tile.Tile{X: 2777, Z: 3436}, Leash: 9, Trees: 5, Steps: 73},                                         // 73
}

// UsesMapleCamps reports whether an account on rung idx is seated on the maple board.
func UsesMapleCamps(idx int) bool { return idx == MapleRung }

// ---- the board ----

// CampBoard is the process-wide seating of the maple rung: which account chops at which camp.
//
// The same shape as PatchBoard, and for the same reasons (one process, one lock, an exact view
// of the cohort; a restart's burst of logins cannot all read an empty board). What differs is
// what a seat is worth. A patch is scored by trees per account because every normal tree is the
// same; these camps are not — a willow gives four times a maple's logs at two thirds of its
// experience, and a camp 73 tiles out costs a walk every load — so a seat is scored by the
// experience an hour one more account would get there, CampPrice.Share, priced for
// ReferenceChopper. Water-filling on that is what sends the cohort to the maples until they are
// full and to the willows after.
//
// A nil *CampBoard is usable and seats every account at the ladder's own maple camp.
//
// The yew rung has a board of the same type (yewcamps.go), and every board also carries what its
// accounts have seen of players that are not seats on it (campcrowd.go): a camp somebody else is
// chopping is priced as more crowded than its seats alone say.
type CampBoard struct {
	camps  []Camp
	prices []CampPrice
	// tie is how close two camps' shares must be to count as equal for a new seat, so the nearer
	// one wins: patchNearTie unless the board was built with another (withNearTie).
	tie    float64
	mu     sync.Mutex
	seats  map[string]patchSeat
	pins   map[string]int
	crowds []crowdPeaks
}

// NewCampBoard returns an empty board over camps (MapleCamps when nil), priced for
// ReferenceChopper at BoardPlayers.
func NewCampBoard(camps []Camp) *CampBoard {
	return NewCampBoardFor(camps, ReferenceChopper, BoardPlayers)
}

// NewCampBoardFor returns an empty board over camps (MapleCamps when nil) priced for the account
// ch at a world of players: the yew board prices for a yew-rung account (ReferenceYewChopper),
// whose Woodcutting, Fletching and walk are not the maple cohort's.
func NewCampBoardFor(camps []Camp, ch Chopper, players int) *CampBoard {
	if camps == nil {
		camps = MapleCamps
	}
	prices := make([]CampPrice, len(camps))
	for i, c := range camps {
		prices[i] = PriceCamp(c, ch, players)
	}
	return &CampBoard{camps: camps, prices: prices, tie: patchNearTie, seats: map[string]patchSeat{}, pins: map[string]int{}, crowds: make([]crowdPeaks, len(camps))}
}

// withNearTie sets the board's near tie, for a board built and not yet shared.
func (b *CampBoard) withNearTie(tie float64) *CampBoard {
	b.tie = tie
	return b
}

// CampSeating is what Seat decided.
type CampSeating struct {
	// Camp is the camp to chop at, an index into the board's camps.
	Camp int
	// Moved is true when this call placed the account somewhere it was not seated before.
	Moved bool
	// Occupants is how many accounts, this one included, are seated at Camp after the call.
	Occupants int
	// Crowd is how many players that are not seats on this board the board counts at Camp
	// (campcrowd.go), and Share is counted over them as well.
	Crowd int
	// Share is the experience an hour the model gives one account there at that occupancy.
	Share float64
}

// Seat returns where who should chop, seating it if it has no live seat and refreshing the seat
// if it has. overflow false keeps the account to maple camps: a seat at an overflow camp is
// then given up and replaced.
//
// A new seat goes to the camp whose share is highest once this account is counted; among camps
// within the board's near tie (patchNearTie) of the best, the nearest to from wins, then the lower
// index. An existing
// seat is kept, and once it is PatchRebalanceAfter old it moves only for CampRebalanceGain
// times its current share.
//
// A camp's share is counted over the players the board has been told stand at it that are not
// seats (ObserveCrowd, campcrowd.go) as well as over the seats: a camp a stranger is chopping is
// priced as if one more account were there. That is for a NEW seat, and for the camp a seated
// account might move TO. The camp it is seated at is priced over the seats alone when it is
// reconsidered: strangers arriving at a tree an account is chopping never walk it off that tree
// (the a user request: "it should have kept tick manipulating the tree until there's
// really none available"). Crowding by strangers is answered by a change of world, not of tree.
func (b *CampBoard) Seat(who string, from tile.Tile, overflow bool, now time.Time) CampSeating {
	if b == nil || len(b.camps) == 0 {
		return CampSeating{Camp: 0, Occupants: 1}
	}
	key := normWho(who)
	noteOwnKey(key)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked(now)

	// One allocation: the seats at each camp (this account not counted), then the strangers at it.
	n := len(b.camps)
	counts := make([]int, 2*n)
	occ, crowd := counts[:n], counts[n:]
	for k, s := range b.seats {
		if k != key && s.patch >= 0 && s.patch < n {
			occ[s.patch]++
		}
	}
	for i := range crowd {
		crowd[i] = b.crowdLocked(i, now)
	}
	seated := func(i int, moved bool) CampSeating {
		return CampSeating{Camp: i, Moved: moved, Occupants: occ[i] + 1, Crowd: crowd[i], Share: b.prices[i].Share(occ[i] + crowd[i] + 1)}
	}

	if pin, ok := b.pins[key]; ok {
		s, had := b.seats[key]
		moved := !had || s.patch != pin
		if moved {
			s = patchSeat{patch: pin, since: now}
		}
		s.seen = now
		b.seats[key] = s
		return seated(pin, moved)
	}

	if s, ok := b.seats[key]; ok && s.patch >= 0 && s.patch < len(b.camps) && b.allowed(s.patch, overflow) {
		s.seen = now
		if now.Sub(s.since) >= PatchRebalanceAfter {
			// The camp the account is chopping at is priced over our own seats only: a stranger who
			// walks up to its tree is not a reason to leave it. A camp it might go to keeps its
			// strangers in the price, so the account does not walk into somebody else's crowd.
			here := b.prices[s.patch].Share(occ[s.patch] + 1)
			best := b.bestLocked(occ, crowd, from, s.patch, overflow)
			if best >= 0 && best != s.patch && b.prices[best].Share(occ[best]+crowd[best]+1) >= CampRebalanceGain*here {
				b.seats[key] = patchSeat{patch: best, since: now, seen: now}
				return seated(best, true)
			}
			s.since = now
		}
		b.seats[key] = s
		return seated(s.patch, false)
	}

	best := b.bestLocked(occ, crowd, from, -1, overflow)
	if best < 0 {
		best = 0
	}
	b.seats[key] = patchSeat{patch: best, since: now, seen: now}
	return seated(best, true)
}

func (b *CampBoard) allowed(i int, overflow bool) bool {
	return overflow || !b.camps[i].Overflow()
}

// bestLocked is the allowed camp with the best share for one more account, near-ties broken by
// distance from `from` and then by index; `except` is skipped. -1 when nothing is allowed. occ is
// the seats at each camp and crowd the strangers the board counts there.
func (b *CampBoard) bestLocked(occ, crowd []int, from tile.Tile, except int, overflow bool) int {
	top := 0.0
	for i := range b.camps {
		if i == except || !b.allowed(i, overflow) {
			continue
		}
		if s := b.prices[i].Share(occ[i] + crowd[i] + 1); s > top {
			top = s
		}
	}
	best, bestDist := -1, 0
	for i, c := range b.camps {
		if i == except || !b.allowed(i, overflow) || b.prices[i].Share(occ[i]+crowd[i]+1) < top*b.tie {
			continue
		}
		d := tile.Chebyshev(from, c.Anchor)
		if from.Level != c.Anchor.Level {
			d += 1 << 20
		}
		if best < 0 || d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// Leave releases who's seat, and any pin. Called when the account leaves the maple rung or stops.
func (b *CampBoard) Leave(who string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	delete(b.seats, normWho(who))
	delete(b.pins, normWho(who))
	b.mu.Unlock()
}

// Pin holds who at camp: from the next Seat on it is seated there whatever the shares say, is
// never rebalanced and ignores the overflow flag, until Leave. It is for a harness that has to
// put one account at a named camp (cmd/wcmapletest's safety arm walks each new camp's route);
// nothing in the fleet pins. A camp outside the board is ignored.
func (b *CampBoard) Pin(who string, camp int) {
	if b == nil || camp < 0 || camp >= len(b.camps) {
		return
	}
	b.mu.Lock()
	b.pins[normWho(who)] = camp
	b.mu.Unlock()
}

// CampIndex is the index of the camp called name on this board, or -1.
func (b *CampBoard) CampIndex(name string) int {
	if b == nil {
		return -1
	}
	for i, c := range b.camps {
		if strings.EqualFold(c.Name, strings.TrimSpace(name)) {
			return i
		}
	}
	return -1
}

// Occupancy is how many live seats each camp holds.
func (b *CampBoard) Occupancy(now time.Time) []int {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked(now)
	out := make([]int, len(b.camps))
	for _, s := range b.seats {
		if s.patch >= 0 && s.patch < len(out) {
			out[s.patch]++
		}
	}
	return out
}

// Camps is the board's camp list.
func (b *CampBoard) Camps() []Camp {
	if b == nil {
		return nil
	}
	return b.camps
}

// Prices is the board's price of each camp, in the order of Camps.
func (b *CampBoard) Prices() []CampPrice {
	if b == nil {
		return nil
	}
	return b.prices
}

func (b *CampBoard) sweepLocked(now time.Time) {
	for k, s := range b.seats {
		if now.Sub(s.seen) > PatchSeatTTL {
			delete(b.seats, k)
		}
	}
}

// CampRebalanceGain is how much better another camp's share must be before a seated account
// walks to it: a fifth. Smaller than PatchRebalanceGain's half because a share here is
// experience an hour, not trees: at the cohort's ~27k xp/h a fifth is ~5k xp/h, which repays the
// longest move on the board (the Seers willows to the Catherby willows, about a minute's walk,
// ~450 xp) inside the ten minutes before the seat is considered again. At half, a camp emptied by
// accounts graduating to yews refilled by ONE account and then stopped with its share 1.49x the
// board's (TestCampRebalanceRefillsAnEmptiedCamp).
const CampRebalanceGain = 1.2

// One board per world, for the reason SharedPatchBoardFor gives.
var (
	campBoardsMu sync.Mutex
	campBoards   = map[int]*CampBoard{}
)

// SharedCampBoardFor is the process-wide maple board for one world (0 when unknown).
func SharedCampBoardFor(world int) *CampBoard {
	if world < 0 {
		world = 0
	}
	campBoardsMu.Lock()
	defer campBoardsMu.Unlock()
	b := campBoards[world]
	if b == nil {
		b = NewCampBoard(nil)
		campBoards[world] = b
	}
	return b
}

// DescribeCamps is the board as one log line: "Seers willows 20/6, Seers maples 5/4, ...",
// accounts over trees, camps with nobody seated left out.
func DescribeCamps(camps []Camp, occ []int) string {
	type row struct {
		name  string
		n, of int
	}
	var rows []row
	for i, c := range camps {
		if i < len(occ) && occ[i] > 0 {
			rows = append(rows, row{c.Name, occ[i], c.Trees})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].n > rows[j].n })
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, r.name+" "+strconv.Itoa(r.n)+"/"+strconv.Itoa(r.of))
	}
	if len(parts) == 0 {
		return "nobody seated"
	}
	return strings.Join(parts, ", ")
}
