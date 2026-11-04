package mastercraftplan

import "strconv"

// THE STEEL ARROW ROUND (a user request: "with the scarabswarm now doing smithing, gathering steel
// arrow tips at the end, we will also add making steel arrows to mastercrafter ... we would make
// headless arrows first, then attach steel arrow tips to the headless arrows making steel arrows to
// hand over to the mule upon completion").
//
// THE CONTENT (content/scripts, branch 289):
//
//   - Headless arrows (skill_fletching/scripts/arrows.rs2 [opheldu,arrow_shaft] case feather ->
//     [label,make_headless_arrows]): members world only, NO level check, up to 15 a use (min of the
//     feathers, the shafts and 15), 1 Fletching XP an arrow (stat_advance 10 tenths x count), no
//     delay.
//   - Steel arrows ([opheldu,headless_arrow] / [opheldu,_category_968] -> [label,make_arrows];
//     skill_fletching/configs/arrows/arrows.dbrow [fletching_steel_arrow]): members only, Fletching 30,
//     up to 15 a use (product,steel_arrow,15), 5 XP an arrow (experience,50), no delay.
//   - Every item stacks (arrows.obj: arrow_shaft 52, headless_arrow 53, steel_arrowheads 41 "Steel
//     arrowtips"; fishing.obj feather 314; ranged/arrows.obj steel_arrow 886), so a load is two stacks
//     and five uses a tick on them make 75 arrows a tick: the same paced burst a tick the pearl
//     round's tips-on-bolts uses (mastercraftrunner.MakeStack), confirmed once at the end of a run.
//
// SUPPLY: the mule holds shafts and feathers (and ScarabSwarm's smithers hand it steel arrowtips);
// the shop trip buys Steel arrowtips at Hickton (shopplan.SupplyShops) when headless arrows wait. No
// chosen supply shop sells shafts or feathers: shafts are stocked only by Dargaud in the Ranging Guild
// (minigames/game_ranging/configs/ranging.inv arrow_shaft,1000,50; the guild door needs Ranged 40)
// and feathers by fishing shops (Gerrant in Port Sarim feather,1000,1; the Fishing Guild 1500,1, Fishing
// 68 to enter), none on the bolt trip's route.

// Arrow item names and ids (content pack/obj.pack).
const (
	ArrowShaft          = "Arrow shaft"
	ArrowShaftID  int32 = 52
	Feather             = "Feather"
	FeatherID     int32 = 314
	Headless            = "Headless arrow"
	HeadlessID    int32 = 53
	ArrowTips           = "Steel arrowtips"
	ArrowTipsID   int32 = 41
	SteelArrow          = "Steel arrow"
	SteelArrowID  int32 = 886
	ArrowsPerUse        = 15
	SteelArrowLvl       = 30
	// HeadlessXPTenths and SteelXPTenths are one arrow's Fletching XP in content's tenths.
	HeadlessXPTenths = 10
	SteelXPTenths    = 50
)

// ArrowHeld is the arrow round's items in one place.
type ArrowHeld struct {
	Shafts, Feathers, Headless, Tips, Arrows int
}

// ArrowHeldIn reads the counts from a published bank.
func ArrowHeldIn(s Stock) ArrowHeld {
	return ArrowHeld{
		Shafts: stockOf(s, ArrowShaft), Feathers: stockOf(s, Feather), Headless: stockOf(s, Headless),
		Tips: stockOf(s, ArrowTips), Arrows: stockOf(s, SteelArrow),
	}
}

// Add is the two holdings together.
func (h ArrowHeld) Add(o ArrowHeld) ArrowHeld {
	return ArrowHeld{h.Shafts + o.Shafts, h.Feathers + o.Feathers, h.Headless + o.Headless, h.Tips + o.Tips, h.Arrows + o.Arrows}
}

// HeadlessToMake is the headless arrows the shafts and feathers make.
func (h ArrowHeld) HeadlessToMake() int { return max(0, min(h.Shafts, h.Feathers)) }

// SteelToMake is the steel arrows the headless arrows (held and to make) and the tips make.
func (h ArrowHeld) SteelToMake() int { return max(0, min(h.Headless+h.HeadlessToMake(), h.Tips)) }

// Uses is how many uses make n arrows, fifteen a use.
func ArrowUses(n int) int {
	if n <= 0 {
		return 0
	}
	return (n + ArrowsPerUse - 1) / ArrowsPerUse
}

// ArrowsReady decides an arrow round from the mule's published bank and this account's own: the
// parts in the two banks together must make set.ArrowThreshold arrows, steel or headless (owner, 6
// Oct 2026: "at least 20,000 arrow shaft for arrows"), and a round this account's bank funds alone
// is a resume. Fletching 30 is needed for the steel step; below it the round makes headless arrows
// only.
func ArrowsReady(set Settings, fletching int, fletchingKnown bool, mule, own ArrowHeld, muleKnown bool) (ok, resume bool, why string) {
	makes := func(h ArrowHeld) int {
		n := h.HeadlessToMake()
		if fletching >= SteelArrowLvl {
			n = max(n, h.SteelToMake())
		}
		return n
	}
	switch {
	case !fletchingKnown:
		return false, false, "Fletching is not known yet"
	case !set.MembersWorld:
		return false, false, "arrows need a members world (" + KeyMembersWorld + " is off)"
	case makes(own) >= set.ArrowThreshold:
		return true, true, "this account's bank holds the parts of " + strconv.Itoa(makes(own)) + " arrows (threshold " + strconv.Itoa(set.ArrowThreshold) + ")"
	case !muleKnown:
		return false, false, "the mule has not published its bank"
	}
	n := makes(mule.Add(own))
	if n < set.ArrowThreshold {
		return false, false, "the arrow parts make " + strconv.Itoa(n) + " arrows, under the threshold " + strconv.Itoa(set.ArrowThreshold)
	}
	return true, false, "the arrow parts make " + strconv.Itoa(n) + " arrows (threshold " + strconv.Itoa(set.ArrowThreshold) + ")"
}

// PlanArrowAsk is the kit for an arrow round: the mule's shafts and feathers, matched to each other
// (what one has more of than the other cannot make anything), its headless arrows, and its Steel
// arrowtips up to what the headless arrows will need. Every line at most set.ArrowKit.
func PlanArrowAsk(set Settings, mule, own ArrowHeld) []Line {
	var lines []Line
	add := func(item string, n int) {
		if n = min(n, set.ArrowKit); n > 0 {
			lines = append(lines, Line{Item: item, Qty: n})
		}
	}
	pairs := min(mule.Shafts+own.Shafts, mule.Feathers+own.Feathers)
	add(ArrowShaft, min(mule.Shafts, pairs-own.Shafts))
	add(Feather, min(mule.Feathers, pairs-own.Feathers))
	add(Headless, mule.Headless)
	headless := own.Headless + min(mule.Headless, set.ArrowKit) + min(pairs, set.ArrowKit)
	add(ArrowTips, min(mule.Tips, headless-own.Tips))
	return lines
}

// ArrowTipsWanted says whether the shop trip should buy Steel arrowtips: arrows are on, this
// account's bank holds headless arrows waiting, and there are fewer tips (its own and the mule's)
// than set.ShopBuyBelow.
func ArrowTipsWanted(set Settings, own, mule ArrowHeld) bool {
	return set.Arrows && set.BuyArrowTips && own.Headless >= set.ShopMinWaiting && own.Tips+mule.Tips < set.ShopBuyBelow
}
