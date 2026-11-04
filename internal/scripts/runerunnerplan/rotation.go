package runerunnerplan

// The rotation: a runner works a circuit over every enabled shop on every listed world, buying
// every rune each shop stocks, instead of the classic ring's two worlds.
//
// a user request: "rune runners buy from mage bank, aubury, betty on all 3 worlds every
// single rune they have".
//
// THE CIRCUIT. Shops are visited in their places on the boat-chain loop -- Port Sarim (Betty),
// Varrock (Aubury), Ardougne (Lundail) -- and at each place the runner buys on every world in
// turn by hopping where it stands, so a hop never costs a walk. The world order snakes (1 2 3 at
// the first shop, 3 2 1 at the next, 1 2 3 at the third), so the first world at a shop is the
// one the runner is already on. A three-world, three-shop round is
//
//	deliver@M draw@M  hop1 Betty@1 hop2 Betty@2 hop3 Betty@3
//	                  hop3 Aubury@3 hop2 Aubury@2 hop1 Aubury@1
//	                  hop1 Lundail@1 hop2 Lundail@2 hop3 Lundail@3  hop M
//
// and, for a runner whose CURRENT Magic is 66 or more (the guild's door; EligibleShops, applied
// when the bot plans each round), a fourth block after Lundail -- south from Ardougne to the
// Wizards' Guild in Yanille -- "WizardsGuild@3 WizardsGuild@2 WizardsGuild@1" before the hop home,
//
// with every shop station preceded by its own hop station, so whatever the previous stations did
// (a skipped hop leaves the runner on another world) the station runs on its own world or not at
// all. A hop to the world the runner is already on finishes at once (OutcomeHere): the round
// above makes seven real hops. The delivery (on the mule's world, at its post) opens each round,
// exactly as on the classic ring, and its gate (deliverMinRunes, deliverMaxAgeMinutes) decides
// whether this round hands anything over.
//
// AMMO STOPS (ammo.go, the ammoBuy setting, a user request). The same walk passes three ammo shops, and an
// ammo station is placed where it costs no extra hop and next to nothing in tiles:
//
//   - Hickton (Catherby, 13 tiles from the mule's post) right after the delivery and BEFORE the draw, paid from
//     the coins the last round left in the pack, so the draw's deposit banks what it bought (no pack slot, and it
//     reaches the mule with the next delivery); and again as the last station before the final hop home, on the
//     last circuit world (the walk from Yanille or Ardougne to the post is made anyway). Both ends are one stop
//     each; a circuit whose last world is the mule's has the first only.
//   - Gerrant (Port Sarim, 34 tiles south of Betty, 16 from the boat dock) beside every Betty pair, the pair
//     order snaking with the world order -- [Gerrant, Betty] on the first world, [Betty, Gerrant] on the second --
//     so the runner crosses the 34 tiles once per world change, not twice (a forward circuit arrives from the
//     dock and starts with Gerrant; a reverse one arrives from Varrock and starts with Betty).
//   - Lowe (Varrock, 22 tiles from Aubury, 31 from Zaff) after every Aubury pair, and after its Zaff station when
//     there is one.
//   - Aemad (East Ardougne, 42 tiles from the bank the Lundail kit starts at; a user request) BEFORE every Lundail pair:
//     the kit banks everything in Ardougne before the lever, so the pack's coins are spent at Aemad's shelf first and never ride
//     into the Wilderness.
//   - Port Khazard (the trawler store, Rope: ropes for the Fishing Trawler's net, 4 November 2026) BEFORE Aemad on every world, with the
//     pack banked at East Ardougne after it; and Ned (Draynor, Rope at 15 coins a conversation) LAST in every Betty pair, his Ropes
//     banked at Draynor. See RopeSupply in ammo.go for the measured shelves.
//   - Hickton on EVERY world when RotationInput.AmmoEveryWorld is set (a user request: "Hickton on every world"): after
//     Hickton@last, each remaining world other than the mule's gets its own "hop w, Hickton@w" before the hop home. Those hops have
//     no rune shop in their block, so the bot hops for them only when the shelf is worth ammoMinVisitCoins.
//
// An ammo station is never a hop's reason: BlockShops, the hop's worth and IdleWait read the rune stations
// only, and a block a hop skips takes its ammo stops with it. Because Gerrant can sit between a hop and its
// Betty station, RotationEntry steps back over ammo stations to find the hop (hopBefore).
//
// TIMED BY RESTOCK. Nothing is visited because it is next: a hop station looks at the board's
// estimate for the shop on the world it would hop to (the freshest stock any account read,
// projected forward by the content's restock rule, less every other account's live hold) and
// hops only for at least hopMinGain runes under the caps; the shop station then walks only for
// at least minVisitRunes. A pair that is not worth it is skipped and the circuit goes on to the
// next pair that is. A round that buys nothing waits IdleWait -- the soonest any line on the
// circuit regrows minVisitRunes -- before the next.
//
// SPREAD. Each runner has a phase (the rotationPhase setting, or a hash of its name): the phase
// picks the direction round the loop (Betty first or Lundail first) and how far the world order
// is turned. Runners with different phases that start together are never at the same shop on
// the same world, except where a forward and a reverse runner both reach Aubury, the middle of
// the loop, in step -- which the Lundail block's length makes unlikely. Whatever the phases, a
// hop station reserves the pair's stock on the shared board before it hops, so a second runner
// deciding a moment later sees the shelf taken and moves on (rotationHold in the bot).
//
// HOPS. Every hop is a full logout and login on the fleet's paced login gate. The rotation keeps
// worldhopplan's two minutes between hops (RotationHopGap), at most maxHopsPerHour in any hour
// (HopBudget), never while hurt, in combat, in the Wilderness, on a Mage Bank trip or while the
// fleet's login path is busy (MayHop), and the fleet itself waits out the server's 45-second
// world-hop timer (login response 21).

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/worldhopplan"
)

// DefaultRotationWorlds are the worlds a rotation works when the worlds setting is empty.
func DefaultRotationWorlds() WorldList { return WorldList{1, 2, 3} }

// stockPriority is the order FullStockRunes lists the runes in: blood first, then the cohort's
// training runes (minds, then the four elementals, then chaos for the bolts), then body and death,
// then the three only Lundail stocks.
//
// BLOOD FIRST (a user request: "runerunner doesnt appear to buy blood runes"). Only the Wizards'
// Guild sells it, and as the last rune it was never bought: live, spare104 reached the Guild with
// 796,549 coins, spent half on minds and elementals at six times their price and held the rest for
// "higher-priority runes ahead", so 1,000 Blood runes stayed on the shelf every visit. It stocks at no
// other shop, so leading the list costs the cohort runes nothing anywhere else.
var stockPriority = []string{
	"Blood rune",
	"Mind rune", "Air rune", "Water rune", "Earth rune", "Fire rune", "Chaos rune",
	"Body rune", "Death rune", "Nature rune", "Law rune", "Cosmic rune",
}

// FullStockRunes is every rune any of the shops stocks, in stockPriority order (a rune the list
// does not know comes after, in the shop's own order). Aubury and Betty stock fire, water, air,
// earth, mind, body, chaos and death; Lundail those and nature, law and cosmic.
func FullStockRunes(shops []string) []string {
	sold := map[string]bool{}
	var extra []string
	for _, key := range shops {
		site, ok := Site(key)
		if !ok {
			continue
		}
		for _, l := range site.Shop.Lines {
			if !sold[l.Rune] {
				sold[l.Rune] = true
				extra = append(extra, l.Rune)
			}
		}
	}
	var out []string
	listed := map[string]bool{}
	for _, r := range stockPriority {
		if sold[r] {
			out = append(out, r)
			listed[r] = true
		}
	}
	for _, r := range extra {
		if !listed[r] {
			out = append(out, r)
		}
	}
	return out
}

// DefaultCapMultiple is the derived price cap as a multiple of a rune's base price (its obj cost,
// what the shop charges at full stock).
//
// Why three. shop.rs2 adds delta/10 percent for every rune below base, so the price reaches three
// times the cost 200 runes below base at Aubury and Betty (delta 10) and about 67 below at Lundail
// (delta 30), and six times -- the ceiling -- at 500 and 167. A line regrows one rune every
// RateTicks only while it is below base, so what a shop yields an hour does not depend on the cap
// as long as the shelf is never left full: at three times, an Aubury or Betty elemental, mind or
// body line takes 200 x 10 ticks = 20 minutes to refill (Lundail's 67 x 50 ticks, 33 minutes;
// chaos and death far longer), and a circuit that comes back to a pair sooner than that loses no
// regrowth. A higher cap buys the same hourly flow and, once, the deep end of a full shelf at up
// to twice the price; a lower one risks a full shelf between visits.
const DefaultCapMultiple = 3

// DerivedCap is DefaultCapMultiple times the rune's cost (0 for a rune no built shop sells),
// never above MaxPriceCap.
func DerivedCap(rune string) int {
	for _, key := range DefaultSites() {
		s, _ := Site(key)
		if l, ok := s.Shop.Line(rune); ok {
			c := DefaultCapMultiple * l.Cost
			if c > MaxPriceCap {
				c = MaxPriceCap
			}
			return c
		}
	}
	return 0
}

// DerivedCaps is the priceCaps entry ("Rune=gp") DerivedCap gives each rune. A rune no shop sells
// keeps its own spelling, so Validate names it.
func DerivedCaps(runes []string) []string {
	out := make([]string, 0, len(runes))
	for _, r := range runes {
		name := r
		if c, ok := CanonicalRune(r); ok {
			name = c
		}
		out = append(out, name+"="+strconv.Itoa(DerivedCap(name)))
	}
	return out
}

// Pair is one shop on one world.
type Pair struct {
	Site  string
	World int
}

// Key is the pair as the ring's station keys spell it ("Betty@2").
func (p Pair) Key() string { return fmt.Sprintf("%s@%d", p.Site, p.World) }

// RotationPhases is how many distinct phases a rotation over n worlds has: two directions times
// n turns of the world order.
func RotationPhases(n int) int {
	if n < 1 {
		n = 1
	}
	return 2 * n
}

// RotationPhaseFor is an account's phase from its name (FNV-1a of the lower-cased name), for a
// runner whose rotationPhase setting is -1. Names can share a phase: set rotationPhase on each
// runner to be sure they differ.
func RotationPhaseFor(name string, worlds int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(name))))
	return int(h.Sum32() % uint32(RotationPhases(worlds)))
}

// ResolvePhase is the phase a runner uses: the setting when it is 0 or more (modulo the phase
// count), else RotationPhaseFor.
func ResolvePhase(setting int, name string, worlds int) int {
	if setting >= 0 {
		return setting % RotationPhases(worlds)
	}
	return RotationPhaseFor(name, worlds)
}

// loopOrder is the shops' order round the boat-chain loop from the mule's post: Port Sarim,
// Varrock, Ardougne, and Yanille (the Wizards' Guild, a land walk south of Ardougne) last.
var loopOrder = []string{SiteBetty, SiteAubury, SiteLundail, SiteWizardsGuild}

// RotationCircuit is the round's pairs in order for this phase: the enabled shops round the loop
// (backwards for phases n..2n-1), each on every world, the world order turned by phase mod n and
// reversed at every other shop so a shop's first world is the one the last shop ended on.
func RotationCircuit(shops []string, worlds []int, phase int) []Pair {
	n := len(worlds)
	if n == 0 {
		return nil
	}
	phases := RotationPhases(n)
	p := ((phase % phases) + phases) % phases
	shift, reverse := p%n, p >= n
	var order []string
	for _, s := range loopOrder {
		if enabled(shops, s) {
			order = append(order, s)
		}
	}
	if reverse {
		for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
			order[i], order[j] = order[j], order[i]
		}
	}
	turned := make([]int, n)
	for i := range worlds {
		turned[i] = worlds[(i+shift)%n]
	}
	var out []Pair
	for k, site := range order {
		for i := 0; i < n; i++ {
			w := turned[i]
			if k%2 == 1 {
				w = turned[n-1-i]
			}
			out = append(out, Pair{Site: site, World: w})
		}
	}
	return out
}

// RotationInput is what a rotation round is built from.
type RotationInput struct {
	// M is the mule's world: the delivery's, and the round's last hop.
	M int
	// Worlds are the worlds to buy on (the filled worlds setting).
	Worlds []int
	// Shops are the enabled sites (Settings.ShopKeys).
	Shops []string
	// Phase is the runner's phase (ResolvePhase).
	Phase int
	// Staffs adds a Zaff station after each Aubury pair.
	Staffs bool
	// Market adds the player-market station after the draw (or purse).
	Market bool
	// Ammo are the ammo sites to place (SplitAmmoSites' placed list, canonical spelling); nil places none.
	Ammo []string
	// AmmoEveryWorld visits Hickton on every world of the round, not only the mule's and the last circuit world.
	AmmoEveryWorld bool
}

// RotationReversed reports whether a phase runs the loop backwards (Lundail first, Betty last): phases
// n..2n-1 over n worlds.
func RotationReversed(worlds, phase int) bool {
	if worlds < 1 {
		return false
	}
	phases := RotationPhases(worlds)
	return ((phase%phases)+phases)%phases >= worlds
}

// BuildRotationRing lays out one rotation round: the delivery, the coin draw when the circuit
// starts at a pack-paid shop (a Lundail-first circuit draws its coins at Gundai, and Aubury's and
// Betty's later come from the nearest bank; it gets the purse station instead, which asks the mule
// for coins and banks them), every pair of the circuit behind its own hop station, and the hop
// back to the mule's world.
func BuildRotationRing(in RotationInput) []Station {
	circuit := RotationCircuit(in.Shops, in.Worlds, in.Phase)
	ring := []Station{{Kind: KindDeliver, World: in.M}}
	ammo := func(site string) bool { return len(circuit) > 0 && enabled(in.Ammo, site) }
	if ammo(SiteHickton) {
		// Paid from the coins the last round left in the pack; the draw's deposit banks the purchase.
		ring = append(ring, Station{Kind: KindAmmo, Site: SiteHickton, World: in.M})
	}
	if len(circuit) > 0 {
		if circuit[0].Site != SiteLundail {
			ring = append(ring, Station{Kind: KindDraw, World: in.M})
		} else {
			// A Lundail-first circuit draws its coins at Gundai out of the bank, so it has no draw
			// station -- and the mule top-up lived only in the draw station. LIVE 7 October 2026:
			// acct35 (phase 4) and Acct171 (phase 3) went round with an empty bank for hours,
			// every shop station "cannot afford: no coins to spend (0)", while the two runners whose
			// circuits began at Betty topped up and bought. The purse station is the top-up alone.
			ring = append(ring, Station{Kind: KindPurse, World: in.M})
		}
	}
	if in.Market {
		ring = append(ring, Station{Kind: KindMarket, World: in.M})
	}
	reverse := RotationReversed(len(in.Worlds), in.Phase)
	bettys := 0
	for _, p := range circuit {
		ring = append(ring, Station{Kind: KindHop, World: p.World})
		gerrant := p.Site == SiteBetty && ammo(SiteGerrant)
		// The snake: the first Betty pair of a forward circuit starts at the dock (Gerrant first), of a reverse
		// one at Varrock's end (Betty first); each next pair is the other way round.
		gerrantFirst := (bettys%2 == 0) != reverse
		if gerrant && gerrantFirst {
			ring = append(ring, Station{Kind: KindAmmo, Site: SiteGerrant, World: p.World})
		}
		if p.Site == SiteLundail && ammo(SitePortKhazard) {
			// Port Khazard's Rope shelf (30, five a minute), a hundred tiles south of East Ardougne: first, with the bank trip its
			// full pack of single Ropes then makes to the East Ardougne bank (AmmoSite.BankAfter), so Aemad's shelf finds a free pack.
			ring = append(ring, Station{Kind: KindAmmo, Site: SitePortKhazard, World: p.World})
		}
		if p.Site == SiteLundail && ammo(SiteAemad) {
			// Before the kit: its bank walk puts every coin the pack still holds into the East Ardougne bank.
			ring = append(ring, Station{Kind: KindAmmo, Site: SiteAemad, World: p.World})
		}
		kind := KindBuy
		if p.Site == SiteLundail {
			kind = KindLundail
		}
		ring = append(ring, Station{Kind: kind, Site: p.Site, World: p.World})
		if p.Site == SiteAubury && in.Staffs {
			ring = append(ring, Station{Kind: KindStaff, Site: SiteZaff, World: p.World})
		}
		if gerrant && !gerrantFirst {
			ring = append(ring, Station{Kind: KindAmmo, Site: SiteGerrant, World: p.World})
		}
		if p.Site == SiteBetty && ammo(SiteNed) {
			// Ned in Draynor, on the walk east from Port Sarim and 13 tiles from the bank Betty's coins come from: last in the pair,
			// his Ropes banked there (AmmoSite.BankAfter) before Varrock's rune stacks need the room.
			ring = append(ring, Station{Kind: KindAmmo, Site: SiteNed, World: p.World})
		}
		if p.Site == SiteAubury && ammo(SiteLowe) {
			ring = append(ring, Station{Kind: KindAmmo, Site: SiteLowe, World: p.World})
		}
		if p.Site == SiteBetty {
			bettys++
		}
	}
	if ammo(SiteHickton) {
		// The last stop before the hop home, on the last circuit world; a circuit that ends on the mule's world
		// has Hickton at its start already (the round wraps straight into it).
		last := circuit[len(circuit)-1].World
		if last != in.M {
			ring = append(ring, Station{Kind: KindAmmo, Site: SiteHickton, World: last})
		}
		if in.AmmoEveryWorld {
			// The other worlds, each behind its own hop (the mule's has Hickton at the round's start already).
			seen := map[int]bool{in.M: true, last: true}
			for _, w := range in.Worlds {
				if seen[w] {
					continue
				}
				seen[w] = true
				ring = append(ring, Station{Kind: KindHop, World: w}, Station{Kind: KindAmmo, Site: SiteHickton, World: w})
			}
		}
	}
	return append(ring, Station{Kind: KindHop, World: in.M})
}

// IsShopStation reports whether a station buys at a rune shop (Betty, Aubury or Lundail).
func IsShopStation(s Station) bool { return s.Kind == KindBuy || s.Kind == KindLundail }

// BlockEnd is the index of the first hop station after i, or len(ring): the end of the block a
// hop at i opens.
func BlockEnd(ring []Station, i int) int {
	for j := i + 1; j < len(ring); j++ {
		if ring[j].Kind == KindHop {
			return j
		}
	}
	return len(ring)
}

// BlockAmmo are the ammo stations in the block a hop at i opens.
func BlockAmmo(ring []Station, i int) []Station {
	var out []Station
	for j := i + 1; j < BlockEnd(ring, i); j++ {
		if IsAmmoStation(ring[j]) {
			out = append(out, ring[j])
		}
	}
	return out
}

// BlockShops are the rune-shop stations in the block a hop at i opens.
func BlockShops(ring []Station, i int) []Station {
	var out []Station
	for j := i + 1; j < BlockEnd(ring, i); j++ {
		if IsShopStation(ring[j]) {
			out = append(out, ring[j])
		}
	}
	return out
}

// RotationEntry is where a rotation round picks up from a standing start, given where the account
// is and which world it is on:
//
//   - the Wilderness or the Mage Bank: this world's Lundail station (the trip finishes first),
//     else the first Lundail station;
//   - Ardougne: this world's Lundail station, else the hop before the first Lundail station;
//   - the east side: this world's first Betty or Aubury station, else the hop before the first;
//   - anywhere else: the delivery, whose own gate hops to the mule's world.
func RotationEntry(ring []Station, area Area, world int) (int, string) {
	lundail := func(s Station) bool { return s.Kind == KindLundail }
	buy := func(s Station) bool { return s.Kind == KindBuy }
	onWorld := func(pred func(Station) bool) func(Station) bool {
		return func(s Station) bool { return pred(s) && s.World == world }
	}
	// hopBefore is the hop station that opens the block station i is in, stepping back over the ammo stations
	// a block may open with (Gerrant before Betty): i itself when there is none.
	hopBefore := func(i int) int {
		j := i
		for j > 0 && ring[j-1].Kind == KindAmmo {
			j--
		}
		if j > 0 && ring[j-1].Kind == KindHop {
			return j - 1
		}
		return i
	}
	switch area {
	case AreaWild:
		if i := firstWhere(ring, onWorld(lundail)); i >= 0 {
			return i, "in the Wilderness: finishing the Mage Bank trip"
		}
		if i := firstWhere(ring, lundail); i >= 0 {
			return i, "in the Wilderness: finishing the Mage Bank trip"
		}
	case AreaArdougne:
		if i := firstWhere(ring, onWorld(lundail)); i >= 0 {
			// Aemad's stop opens the block after its hop: an account already in Ardougne on this world does it first.
			for i > 0 && ring[i-1].Kind == KindAmmo && ring[i-1].World == world {
				i--
			}
			return i, fmt.Sprintf("in Ardougne on world %d", world)
		}
		if i := firstWhere(ring, lundail); i >= 0 {
			return hopBefore(i), fmt.Sprintf("in Ardougne on world %d: hopping for Lundail", world)
		}
	case AreaEast:
		if i := firstWhere(ring, onWorld(buy)); i >= 0 {
			return i, fmt.Sprintf("east on world %d", world)
		}
		if i := firstWhere(ring, buy); i >= 0 {
			return hopBefore(i), fmt.Sprintf("east on world %d: hopping for %s", world, ring[i].Key())
		}
	}
	return 0, fmt.Sprintf("%s on world %d: starting at the delivery", area, world)
}

// ---- the hop budget ----

// RotationHopGap is the shortest gap between two of a rotating runner's hops:
// worldhopplan.DefaultLimits.MinInterval, the fleet's rule for every script that hops.
var RotationHopGap = worldhopplan.DefaultLimits.MinInterval

// FleetHopBudget is the fleet-side hop bound a rotating runner asks for (Context.SetHopBudget):
// perHour is its maxHopsPerHour, and the fleet counts over worldhopplan's thirty-minute window,
// so that is half of it, rounded up and never below worldhopplan's own four. Without it the
// fleet refused 18 of the four runners' 77 hops on 7 October 2026 ("hop budget spent (4 in
// 30m0s) -- staying home") while the ring itself allows twelve an hour.
func FleetHopBudget(perHour int) (maxHops int, window time.Duration) {
	window = worldhopplan.DefaultLimits.Window
	maxHops = (perHour + 1) / 2
	if maxHops < worldhopplan.DefaultLimits.MaxHops {
		maxHops = worldhopplan.DefaultLimits.MaxHops
	}
	return maxHops, window
}

// RotationHopWaitMax is the longest a hop station waits for the hop budget before it skips its
// block instead.
const RotationHopWaitMax = 5 * time.Minute

// HopWindow is the span maxHopsPerHour counts over.
const HopWindow = time.Hour

// PruneHops keeps the hop times inside HopWindow, oldest first.
func PruneHops(recent []time.Time, now time.Time) []time.Time {
	var out []time.Time
	for _, t := range recent {
		if now.Sub(t) < HopWindow {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// HopBudget is how long a rotating runner must wait before its next hop (0: it may hop now) and
// why: RotationHopGap after its last hop, and no more than perHour hops in any HopWindow.
func HopBudget(recent []time.Time, now time.Time, perHour int) (time.Duration, string) {
	recent = PruneHops(recent, now)
	var wait time.Duration
	why := ""
	if n := len(recent); n > 0 {
		if since := now.Sub(recent[n-1]); since < RotationHopGap {
			wait = RotationHopGap - since
			why = fmt.Sprintf("last hop %s ago (hops are %s apart)", since.Round(time.Second), RotationHopGap)
		}
	}
	if perHour > 0 && len(recent) >= perHour {
		free := recent[len(recent)-perHour].Add(HopWindow).Sub(now)
		if free > wait {
			wait = free
			why = fmt.Sprintf("%d hops in the last hour (maxHopsPerHour %d)", len(recent), perHour)
		}
	}
	return wait, why
}
