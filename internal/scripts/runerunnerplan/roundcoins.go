package runerunnerplan

// The coins a WHOLE round needs.
//
// a user request: "more runerunners arent needed 1 acc should take enough coins to buy all
// runes from all shops on all worlds". One runner works every shop on every world, so its purse has
// to be sized for the round, not for the leg in front of it.
//
// LIVE, 12 October 16:49-23:47 (five runners, 53 rounds, 5,781,336 coins spent), the purse was short
// in four ways, all of them in how it was sized:
//
//   - the estimate was taken at the DRAW INSTANT: a shelf emptied one round ago is reached up to
//     forty minutes later after regrowing, so the spend was 1.8 to 2.2 times the estimate in the
//     rounds that had the money;
//   - Lundail's purchase, paid from the bank at Gundai, was not part of the ask at all;
//   - there was no margin on an estimate that is only ever a projection; and
//   - the ask itself was refused by the fleet-wide memory of a "none in the bank" (see
//     runerunner.AskMuleForCoins), so 13 of 28 asks never reached the mule.
//
// RoundCoins fixes the first three. Each shop station still ahead is valued at the stock the board
// expects there when the round REACHES it (the freshest sighting any account made, projected by the
// content's restock rule to that moment, holds ignored -- a hold is a plan, not coins), Lundail is
// counted on its own (it is paid from the bank, not the pack), Zaff's staffs are added, and every
// estimate carries RoundMarginPercent more, never above a full shelf (a shelf cannot hold more than
// its base). Pure: the bot supplies the board as a function.

import (
	"fmt"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

const (
	// RoundMarginPercent is the margin added to each station's estimated cost: an estimate is a
	// projection of what other accounts left, not a reading.
	RoundMarginPercent = 20
	// RoundLeadMax is how long the whole ring is taken to last, from the first station after the
	// draw to the last: the 53 live rounds averaged 29.8 minutes, and a three-world round with the
	// Wizards' Guild and the Wilderness leg runs longer.
	RoundLeadMax = 40 * time.Minute
)

// StationLead is how long after the draw the pos'th of n remaining stations (0-based) is reached,
// spreading RoundLeadMax evenly over the stations: the last is reached at RoundLeadMax, the first
// 1/n of it in. Never negative.
func StationLead(pos, n int) time.Duration {
	if n <= 0 || pos < 0 {
		return 0
	}
	if pos >= n {
		pos = n - 1
	}
	return RoundLeadMax * time.Duration(pos+1) / time.Duration(n)
}

// BaseStock is a shop's full shelf for the listed runes it sells, keyed by the rune as listed.
func BaseStock(sh fleetplan.RuneShop, runes []string) map[string]int {
	out := map[string]int{}
	for _, r := range runes {
		if l, ok := sh.Line(r); ok {
			out[r] = l.Base
		}
	}
	return out
}

// FullShelfCost is what buying every listed rune off a full shelf costs under the caps: the most
// one visit to the shop can spend.
func FullShelfCost(sh fleetplan.RuneShop, runes []string, caps Caps) int {
	return EstCost([]StationEstimate{{Shop: sh, Stock: BaseStock(sh, runes)}}, runes, caps)
}

// stationNeed is one station's estimated cost and the margin on it: RoundMarginPercent of the
// cost, but never taking the pair above what a full shelf would cost.
func stationNeed(st StationEstimate, runes []string, caps Caps) (cost, margin int) {
	cost = EstCost([]StationEstimate{st}, runes, caps)
	margin = cost * RoundMarginPercent / 100
	if full := FullShelfCost(st.Shop, runes, caps); cost+margin > full {
		margin = full - cost
	}
	if margin < 0 {
		margin = 0
	}
	return cost, margin
}

// RoundNeed is the coins the rest of a round is expected to spend, by who pays.
//
// AMMO (ammo.go, 13 October 2026) is paid from the pack and is the ring's lowest priority, so it is NOT part of
// the rune and staff need: PackNeed, Est and Total are the rune and staff figures alone, and what sizes a shop-bank
// top-up, the market station's float and Lundail's keep from them is exactly what it was without ammo. The ammo
// estimate (the Ammo field) is carried beside them. Since 14 October 2026 (owner: "spend it on everything") it IS
// part of the round's full BILL (Bill: Total and Ammo), which sizes the purse the mule is asked to fill, so a
// runner that holds the bill has the ammo funded too; the pack draw adds it (AmmoDrawShare) from coins left once
// the whole rune need is covered; and an ammo station spends the pack's coins beyond the fare and the part of that
// same rune need the bank cannot fund for the stations still ahead (HoldAhead). Only a purse that is short of
// the runes even with the bank (after the mule's whole stack was taken) buys runes first and ammo nothing.
type RoundNeed struct {
	// Pack is the pack-paid shops' estimated cost (Betty, Aubury, the Wizards' Guild) and
	// PackMargin the margin on it. The pack draws them from the bank at the mule's post and at the
	// shops' own banks.
	Pack, PackMargin int
	// Lundail is Lundail's estimated cost and LundailMargin the margin on it: paid from the bank at
	// Gundai, inside the Mage Bank, so it only has to be in the bank.
	Lundail, LundailMargin int
	// Staffs is Zaff's expected cost: a full shelf, so no margin.
	Staffs int
	// Ammo is the ammo stations' expected cost (AmmoCost over the stations still ahead, less than the ammoCoins
	// left), without margin: the shelves are read off the board's estimate and the budget bounds the rest. It is
	// outside PackNeed, Est and Total (see the type's comment): a figure to fund from surplus, not a need.
	Ammo int
	// Fares is the boat fare coins carried (never spent in a shop).
	Fares int
	// Next is the first pack-paid station's expected cost, without margin: what the station being
	// visited (or the first one after the draw) is expected to take. NextMargin is the margin on it (never
	// taking the pair past a full shelf): a pack that holds Next but not Next and NextMargin can leave the
	// top of a regrown shelf "for coins" with the bank full (a user request: spend it on everything).
	Next, NextMargin int
	// Stations is how many shop and staff stations the figure covers.
	Stations int
}

// Margin is the margin on every estimate.
func (n RoundNeed) Margin() int { return n.PackMargin + n.LundailMargin }

// PackNeed is what the pack must carry through the pack-paid stations: their estimate, its margin and
// the staffs (what DrawAmount and TopUpDue are given), without the fares and without the ammo.
func (n RoundNeed) PackNeed() int { return n.Pack + n.PackMargin + n.Staffs }

// Est is every coin the rune and staff shops are expected to take, margin included, without the fares and
// without the ammo: the est CircuitTopUp adds the reserve and the fare to, and the float the market station
// leaves alone.
func (n RoundNeed) Est() int { return n.PackNeed() + n.Lundail + n.LundailMargin }

// Total is Est and the fares: the purse the round wants to start with, above the bank's reserve.
func (n RoundNeed) Total() int { return n.Est() + n.Fares }

// Bill is the round's FULL bill: Total and the ammo stations' estimate, above the bank's reserve. The mule is
// asked for coins whenever bank plus pack holds less than Bill and the reserve.
func (n RoundNeed) Bill() int { return n.Total() + n.Ammo }

// String is the round need in the log's words. ammo= is the ammo estimate beside the need, not in round_need.
func (n RoundNeed) String() string {
	return fmt.Sprintf("round_need=%d pack=%d lundail=%d staffs=%d ammo=%d margin=%d fares=%d stations=%d",
		n.Total(), n.Pack, n.Lundail, n.Staffs, n.Ammo, n.Margin(), n.Fares, n.Stations)
}

// PurseInput is one round-need reading.
type PurseInput struct {
	// Ring is the round's stations and From the first one to count (the station after the draw, or
	// the station being visited).
	Ring []Station
	From int
	Now  time.Time
	// Runes and Caps are the runes bought and their price caps.
	Runes []string
	Caps  Caps
	// Fares is fareCoins.
	Fares int
	// StaffCost is what one Zaff station is expected to cost (StaffEstCost), 0 with staffs off.
	StaffCost int
	// AmmoCost is what one ammo station is expected to cost from the board's estimate of its shelf (nil: ammo
	// off, the station is not counted) and AmmoLimit the most the ammo stations still ahead may cost in all
	// (ammoCoins less the round's spend so far; 0 is no limit).
	AmmoCost  func(s Station, at time.Time) int
	AmmoLimit int
	// Skip reports a station that cannot be visited this round (a pair off the ring, a door the
	// account's Magic does not open, Lundail stopped by its deaths). nil skips none.
	Skip func(Station) bool
	// Stock is the board's expected stock for each listed rune at a shop on a world at the moment
	// the round will reach it, holds ignored. nil takes every shelf as full.
	Stock func(site ShopSite, world int, at time.Time) map[string]int
}

// PlanRoundCoins is the coins the stations from in.From on are expected to need.
func PlanRoundCoins(in PurseInput) RoundNeed {
	n := RoundNeed{Fares: in.Fares}
	from := in.From
	if from < 0 {
		from = 0
	}
	left := len(in.Ring) - from
	first := true
	for i := from; i < len(in.Ring); i++ {
		s := in.Ring[i]
		if s.Kind != KindBuy && s.Kind != KindLundail && s.Kind != KindStaff && s.Kind != KindAmmo {
			continue
		}
		if in.Skip != nil && in.Skip(s) {
			continue
		}
		if s.Kind == KindStaff {
			n.Staffs += in.StaffCost
			n.Stations++
			continue
		}
		if s.Kind == KindAmmo {
			if in.AmmoCost != nil {
				n.Ammo += in.AmmoCost(s, in.Now.Add(StationLead(i-from, left)))
				n.Stations++
			}
			continue
		}
		site, ok := Site(s.Site)
		if !ok {
			continue
		}
		var stock map[string]int
		if in.Stock != nil {
			stock = in.Stock(site, s.World, in.Now.Add(StationLead(i-from, left)))
		} else {
			stock = BaseStock(site.Shop, in.Runes)
		}
		cost, margin := stationNeed(StationEstimate{Shop: site.Shop, Stock: stock}, in.Runes, in.Caps)
		if s.Kind == KindLundail {
			n.Lundail += cost
			n.LundailMargin += margin
		} else {
			n.Pack += cost
			n.PackMargin += margin
			if first {
				n.Next, n.NextMargin, first = cost, margin, false
			}
		}
		n.Stations++
	}
	if in.AmmoLimit > 0 && n.Ammo > in.AmmoLimit {
		n.Ammo = in.AmmoLimit
	}
	return n
}

// LundailWithdraw is the coins Gundai's bank is asked for: the plan's coins and RoundMarginPercent
// more (the plan is made from a projection, and the real shelf has held a sixth more than planned),
// never above a full shelf nor above what the bank can spare. What is not spent is banked again at
// Gundai, so the Wilderness pack still holds only the purchase and its margin.
func LundailWithdraw(planCoins, fullShelf, spendable int) int {
	if planCoins <= 0 {
		return 0
	}
	want := planCoins + planCoins*RoundMarginPercent/100
	if fullShelf > 0 && want > fullShelf {
		want = fullShelf
	}
	if want > spendable {
		want = spendable
	}
	if want < planCoins {
		want = planCoins
	}
	return want
}

// ShopFloat is the coins the bank must keep for the shops: the round's estimate less what the pack
// already carries beyond the fare. The market station may spend only what the bank holds above it.
func ShopFloat(est, packCoins, fare int) int {
	carried := packCoins - fare
	if carried < 0 {
		carried = 0
	}
	if f := est - carried; f > 0 {
		return f
	}
	return 0
}

// StationShort reports whether the pack cannot pay the station it is about to visit its expected
// cost (next: RoundNeed.Next) while the bank can add at least NoCoinsMargin: a stop at the shop's own
// bank is then worth it, whatever share of the whole leg's need the shortfall is. TopUpDue alone waits
// for a quarter of the leg's need, and on 12 October 73 of the 187 shelf lines left "for coins" came with
// the bank still above 15,000: the pack ran short mid-leg and nothing fetched more. A bank never read is
// taken as able to add it.
func StationShort(inPack, next, fare int, bankRead bool, bankCoins, reserve int) bool {
	short := next + fare - inPack
	if next <= 0 || short < NoCoinsMargin {
		return false
	}
	return !bankRead || bankCoins-reserve >= NoCoinsMargin
}

// BankKeep is the coins the bank keeps back from a pack-paid draw for the bank-paid stations still
// ahead (Lundail, drawn at Gundai): all of Lundail's share when the bank can pay the whole round, and
// a pro-rata part of what it can spare when it cannot -- so a short purse is spread over the round's
// stations instead of the pack-paid draw taking every coin above the reserve and Lundail finding
// 10,000 at Gundai. It is worked out ONCE, at the round's draw, and kept in State.LundailKeep: a later
// shop-bank top-up reads KeepNow of it, not a fresh BankKeep of the bank it finds (that share of an
// ever smaller spare shrank geometrically, 39,216 then about 2,500 then under 200 in a 610,000-coin round).
func (n RoundNeed) BankKeep(bankCoins, reserve int) int {
	share := n.Lundail + n.LundailMargin
	est := n.Est()
	spare := bankCoins - reserve
	if share <= 0 || est <= 0 || spare <= 0 {
		return 0
	}
	if spare >= est {
		return share
	}
	return int(int64(share) * int64(spare) / int64(est))
}

// KeepNow is what a stored keep (State.LundailKeep, from BankKeep at the draw) still holds back from a
// shop-bank top-up: the stored coins, never more than the bank-paid stations still ahead are expected to
// need (n, read from the station being visited), so the keep falls away as the Lundail stations pass.
func (n RoundNeed) KeepNow(stored int) int {
	if stored <= 0 {
		return 0
	}
	if rest := n.Lundail + n.LundailMargin; stored > rest {
		if rest < 0 {
			return 0
		}
		return rest
	}
	return stored
}

// ReserveUncovered is a visit's reserve (ReserveAhead) cut to the share of the stations ahead that the
// bank cannot fund. The reserve exists so a pack that is all the coins there are keeps something for
// the dearer runes later; when the bank holds the round's float, the next station's top-up refills the
// pack and the hold only leaves runes on the shelf. LIVE 12 October: 2,781 Law runes (~667k) were left
// at the Wizards' Guild "for higher-priority runes ahead" with the mule's coins in hand, and a first
// full-shelf round would do it again to about 350,000 coins of Death, Nature and Law at each guild
// visit. aheadNeed is what the pack-paid stations ahead are expected to cost (RoundNeed.PackNeed) and
// bankSpare what the bank can add above its reserve and Lundail's keep: a bank that covers the lot
// holds nothing back (nil), one that covers none of it holds back all, and in between each hold is
// scaled by the uncovered share.
func ReserveUncovered(reserve map[string]int, aheadNeed, bankSpare int) map[string]int {
	if len(reserve) == 0 || aheadNeed <= 0 {
		return reserve
	}
	if bankSpare >= aheadNeed {
		return nil
	}
	if bankSpare <= 0 {
		return reserve
	}
	uncovered := int64(aheadNeed - bankSpare)
	out := make(map[string]int, len(reserve))
	for r, hold := range reserve {
		out[r] = int(int64(hold) * uncovered / int64(aheadNeed))
	}
	return out
}
