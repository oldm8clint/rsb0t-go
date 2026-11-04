package runerunnerplan

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// THE 2 NOVEMBER 2026 SHOP ROUND: every number below was read from content 289 (ardougne_east.inv [adventurershop],
// varrock.inv [archeryshop], catherby.inv [archeryshop2], dwarven_mine.inv [pickaxeshop], shantaypass.inv [shantayshop], the keepers'
// .npc params and arrows.obj / vials.obj / pickaxes.obj costs) and re-derived with an independent script from shop.rs2's price walk.

func TestAemadShelfMatchesContent(t *testing.T) {
	// One shop, two keepers: both npcs carry owned_shop,adventurershop, sell 1300, delta 20.
	if Aemad.Keeper != "Aemad" || Aemad.NPCID != 590 || Aemad.SellMultiplier != 1300 || Aemad.Delta != 20 || Kortan != "Kortan" {
		t.Errorf("Aemad %+v", Aemad)
	}
	for _, c := range []struct {
		item             string
		base, rate, cost int
	}{{AmmoVial, 500, 30, 2}, {AmmoBronzeArrow, 500, 10, 1}, {AmmoRope, 20, 100, 18}} {
		l, ok := Aemad.Line(c.item)
		if !ok || l.Base != c.base || l.RateTicks != c.rate || l.Cost != c.cost {
			t.Errorf("Aemad %s line %+v %v", c.item, l, ok)
		}
	}
	// A rope costs 1.3 times its obj cost at base stock (the general-store rule) and 20 percent of a tenth more for each one below.
	if p := Aemad.PriceAt(AmmoRope, 20); p != 23 {
		t.Errorf("a full-shelf rope costs %d, want 23", p)
	}
	for _, c := range []struct {
		item string
		n    int
		want int
	}{{AmmoVial, 500, 4775}, {AmmoBronzeArrow, 500, 2325}, {AmmoRope, 20, 527}, {AmmoVial, 24, 62}} {
		l, _ := Aemad.Line(c.item)
		if got := Aemad.CostToBuy(c.item, l.Base, c.n); got != c.want {
			t.Errorf("Aemad %d %s cost %d, want %d", c.n, c.item, got, c.want)
		}
	}
	// The shelf's own ceiling is six times the (1.3-scaled) cost at most: a vial never costs more than 12, an arrow 6.
	if got, want := Aemad.PriceAt(AmmoVial, 0), 12; got != want {
		t.Errorf("a vial from an empty shelf costs %d, want %d", got, want)
	}
	// Maps: m40_51.jm2 "0 53 30: 590" and "0 55 28: 591" on the origin 2560,3264; the stand is the Antipoison maker's proven tile.
	aemad, kortan := tile.Tile{X: 2560 + 53, Z: 3264 + 30}, tile.Tile{X: 2560 + 55, Z: 3264 + 28}
	if tile.Chebyshev(AemadStand, aemad) > 1 || tile.Chebyshev(AemadStand, kortan) > 1 {
		t.Errorf("the stand %v is not beside Aemad %v and Kortan %v", AemadStand, aemad, kortan)
	}
	// A whole shelf is worth about 2,900 coins, under the ammoMinVisitCoins default: Aemad's stop carries its own smaller minimum
	// (never above the setting), the other stops take the setting.
	if est := AmmoStationCost(Aemad, map[string]int{AmmoVial: 500, AmmoBronzeArrow: 500, AmmoRope: 20}, []string{AmmoVial, AmmoBronzeArrow, AmmoRope},
		AmmoCaps([]string{AmmoVial, AmmoBronzeArrow, AmmoRope}, nil, true), 0); est >= DefaultAmmoMinVisitCoins {
		t.Errorf("a full Aemad shelf is worth %d, the gate premise is that it is under %d", est, DefaultAmmoMinVisitCoins)
	}
	site, _ := AmmoSiteFor(SiteAemad)
	lowe, _ := AmmoSiteFor(SiteLowe)
	if site.MinVisit(DefaultAmmoMinVisitCoins) != AemadMinVisitCoins || site.MinVisit(100) != 100 || lowe.MinVisit(DefaultAmmoMinVisitCoins) != DefaultAmmoMinVisitCoins {
		t.Errorf("minimum visits: Aemad %d (setting 3000) %d (setting 100), Lowe %d", site.MinVisit(DefaultAmmoMinVisitCoins), site.MinVisit(100), lowe.MinVisit(DefaultAmmoMinVisitCoins))
	}
	// Vials and Rope take a slot a unit; Bronze arrows stack.
	for item, want := range map[string]bool{AmmoVial: true, AmmoRope: true, AmmoBucket: true, AmmoBronzeArrow: false, AmmoSteelArrow: false, AmmoFeather: false} {
		if it, ok := AmmoItemFor(item); !ok || it.Unstackable != want {
			t.Errorf("%s: %+v %v, unstackable want %v", item, it, ok, want)
		}
	}
}

func TestLoweAndHicktonCarryTheirArrows(t *testing.T) {
	for _, c := range []struct {
		shop             fleetplan.RuneShop
		item             string
		base, rate, cost int
		full             int
	}{
		{Lowe, AmmoSteelArrow, 1000, 20, 12, 56730},
		{Lowe, AmmoBronzeArrow, 2000, 10, 1, 10500},
		{Lowe, AmmoIronArrow, 1500, 15, 3, 22995},
		{Hickton, AmmoBronzeArrow, 1000, 10, 1, 4500},
		{Hickton, AmmoIronArrow, 800, 20, 3, 10395},
	} {
		l, ok := c.shop.Line(c.item)
		if !ok || l.Base != c.base || l.RateTicks != c.rate || l.Cost != c.cost {
			t.Errorf("%s %s line %+v %v", c.shop.Keeper, c.item, l, ok)
			continue
		}
		if got := c.shop.CostToBuy(c.item, c.base, c.base); got != c.full {
			t.Errorf("%s: a full %s shelf costs %d, want %d", c.shop.Keeper, c.item, got, c.full)
		}
	}
	// Steel arrows: 12 at base, 72 (six times) once the shelf is 500 under; the default three-times cap leaves the deep end.
	if Lowe.PriceAt(AmmoSteelArrow, 1000) != 12 || Lowe.PriceAt(AmmoSteelArrow, 500) != 72 || DerivedAmmoCap(AmmoSteelArrow) != 36 {
		t.Errorf("steel arrow prices %d %d cap %d", Lowe.PriceAt(AmmoSteelArrow, 1000), Lowe.PriceAt(AmmoSteelArrow, 500), DerivedAmmoCap(AmmoSteelArrow))
	}
	// Hickton's Steel, Mithril, Adamant and Rune ARROW lines are stocked at 0 and never regrow: not modelled.
	for _, name := range []string{"Mithril arrow", "Adamant arrow", "Rune arrow"} {
		if Hickton.Sells(name) || Lowe.Sells(name) {
			t.Errorf("%s is modelled", name)
		}
	}
	if Hickton.Sells(AmmoSteelArrow) {
		t.Error("Hickton's Steel arrow line has base 0 and is not modelled")
	}
}

// Shantay and Nurmof are registered with content-cited shelves and are not built: each says why, and neither is placed.
func TestShantayAndNurmofAreRegisteredNotBuilt(t *testing.T) {
	sh, _ := AmmoSiteFor(SiteShantay)
	if sh.Built || sh.Gate == "" || sh.Shop.Keeper != "Shantay" || sh.Shop.NPCID != 836 || sh.Stand != (tile.Tile{X: 3304, Z: 3123}) {
		t.Errorf("Shantay %+v", sh)
	}
	if l, ok := sh.Shop.Line(AmmoFeather); !ok || l.Base != 500 || l.RateTicks != 50 || l.Cost != 2 || len(sh.Shop.Lines) != 1 {
		t.Errorf("Shantay's feather line %+v %v", l, ok)
	}
	nu, _ := AmmoSiteFor(SiteNurmof)
	if nu.Built || nu.Gate == "" || nu.Shop.NPCID != 594 || nu.Shop.Delta != 20 || nu.Stand.Z != 9844 || len(nu.Shop.Lines) != 6 {
		t.Errorf("Nurmof %+v", nu)
	}
	if got := nu.Shop.CostToBuy("Mithril pickaxe", 3, 3); got != 3978 {
		t.Errorf("Nurmof's three Mithril pickaxes cost %d, want 3978 (1300, 1326, 1352)", got)
	}
	placed, left := SplitAmmoSites([]string{"Shantay", "Nurmof", "Aemad"}, []string{SiteBetty, SiteAubury, SiteLundail})
	if strings.Join(placed, ",") != "Aemad" || len(left) != 2 {
		t.Errorf("placed %v left %v", placed, left)
	}
	// Every shelf line of every registered shop is an ammo part with the shop's own cost (the registry test, for the new sites).
	for _, k := range []string{SiteShantay, SiteNurmof, SiteAemad} {
		s, _ := AmmoSiteFor(k)
		for _, l := range s.Shop.Lines {
			if it, ok := AmmoItemFor(l.Rune); !ok || it.Cost != l.Cost {
				t.Errorf("%s %s: part %+v %v", k, l.Rune, it, ok)
			}
		}
	}
}

// Aemad is placed with Lundail, before each Lundail pair and after its hop, on every world; the hop count and the rune blocks
// do not change, and the Wilderness leg starts only after the East Ardougne kit has banked the pack's coins.
func TestAemadRidesWithLundail(t *testing.T) {
	ring := liveRing([]string{SiteAemad})
	got := PlanString(ring, 0)
	const want = "[deliver@1 draw@1 market@1 hop3 Betty@3 hop1 Betty@1 hop2 Betty@2 hop2 Aubury@2 Zaff@2 hop1 Aubury@1 Zaff@1 " +
		"hop3 Aubury@3 Zaff@3 hop3 Aemad@3 Lundail@3 hop1 Aemad@1 Lundail@1 hop2 Aemad@2 Lundail@2 " +
		"hop2 WizardsGuild@2 hop1 WizardsGuild@1 hop3 WizardsGuild@3 hop1]"
	if got != want {
		t.Fatalf("ring\n got %s\nwant %s", got, want)
	}
	base := liveRing(nil)
	if realHops(ring, 1) != realHops(base, 1) || Hops(ring) != Hops(base) {
		t.Errorf("Aemad changed the hops: %d real of %d, base %d of %d", realHops(ring, 1), Hops(ring), realHops(base, 1), Hops(base))
	}
	for i, s := range ring {
		if s.Kind == KindLundail && (ring[i-1].Key() != "Aemad@"+fmt.Sprint(s.World) || ring[i-2].Kind != KindHop) {
			t.Errorf("%s is not preceded by Aemad on its world after the hop: %s", s.Key(), PlanString(ring, i-3))
		}
		if s.Kind == KindHop {
			if got, wantShops := PlanString(BlockShops(ring, i), 0), blockShopsOf(base, s, i, ring); got != wantShops {
				t.Errorf("block at %s: rune stations %s, without ammo %s", s.Key(), got, wantShops)
			}
		}
	}
	// Without Lundail in the round there is nothing to place it with.
	if placed, left := SplitAmmoSites([]string{SiteAemad}, []string{SiteBetty, SiteAubury}); len(placed) != 0 || len(left) != 1 || !strings.Contains(left[0], "placed with Lundail") {
		t.Errorf("placed %v left %v", placed, left)
	}
	if got := PlanString(BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2}, Shops: []string{SiteBetty}, Ammo: []string{SiteAemad}}), 0); strings.Contains(got, "Aemad") {
		t.Errorf("Aemad without Lundail: %s", got)
	}
	// A Lundail-first circuit (a reverse phase) opens with the hop, Aemad, then the kit.
	rev := BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2, 3}, Shops: BuiltSites(), Phase: 3, Ammo: []string{SiteAemad}})
	if s := PlanString(rev, 0); !strings.HasPrefix(s, "[deliver@1 purse@1 hop1 Aemad@1 Lundail@1 hop2 Aemad@2 Lundail@2 hop3 Aemad@3 Lundail@3 ") {
		t.Errorf("reverse ring %s", s)
	}
	// An account standing in Ardougne on a world of the ring starts with Aemad's stop, not the kit.
	if i, _ := RotationEntry(ring, AreaArdougne, 3); ring[i].Key() != "Aemad@3" {
		t.Errorf("Ardougne on world 3: %s", ring[i].Key())
	}
	// A world the ring does not buy on: the hop that opens the block.
	if i, _ := RotationEntry(ring, AreaArdougne, 9); ring[i].Kind != KindHop || ring[i+1].Key() != "Aemad@3" {
		t.Errorf("Ardougne on world 9: %s then %s", ring[i].Key(), ring[i+1].Key())
	}
	// The Wilderness entry stays at the kit: the trip finishes first.
	if i, _ := RotationEntry(ring, AreaWild, 2); ring[i].Key() != "Lundail@2" {
		t.Errorf("Wilderness on world 2: %s", ring[i].Key())
	}
	// Block membership: a skipped Lundail hop takes Aemad's stop with it.
	h := Index(ring, "Aemad@3") - 1
	if got := PlanString(BlockAmmo(ring, h), 0); got != "[Aemad@3]" {
		t.Errorf("block ammo %s", got)
	}
}

// Hickton on EVERY world: the last circuit world and the mule's keep their stops; every other world gets a hop and a stop of its
// own before the hop home. Off (the default of the plan) the ring is unchanged.
func TestHicktonOnEveryWorld(t *testing.T) {
	build := func(shops []string, phase int, every bool, worlds ...int) []Station {
		return BuildRotationRing(RotationInput{M: 1, Worlds: worlds, Shops: shops, Phase: phase, Ammo: []string{SiteHickton}, AmmoEveryWorld: every})
	}
	three := []int{1, 2, 3}
	// Off: the old ends.
	if got := PlanString(build(liveShops(), 0, false, three...), 0); strings.Count(got, "Hickton") != 1 {
		t.Errorf("off: %s", got)
	}
	// Ends on world 3 (no Wizards' Guild): Hickton@3 as before, then world 2 behind its own hop, then the hop home.
	ring := build([]string{SiteAubury, SiteBetty, SiteLundail}, 0, true, three...)
	got := PlanString(ring, 0)
	if !strings.HasPrefix(got, "[deliver@1 Hickton@1 draw@1 ") || !strings.HasSuffix(got, "Lundail@3 Hickton@3 hop2 Hickton@2 hop1]") {
		t.Errorf("ring ends on world 3: %s", got)
	}
	// Ends on the mule's world (the guild's block runs 3 2 1): Hickton@1 is the round's start; worlds 2 and 3 follow the circuit.
	got = PlanString(build(liveShops(), 0, true, three...), 0)
	if strings.Count(got, "Hickton") != 3 || !strings.HasSuffix(got, "WizardsGuild@1 hop2 Hickton@2 hop3 Hickton@3 hop1]") {
		t.Errorf("ring ends on the mule's world: %s", got)
	}
	// Every world of the round has exactly one Hickton stop, keys are unique, and the rune blocks are what they were.
	for _, r := range [][]Station{ring, build(liveShops(), 5, true, three...), build(liveShops(), 2, true, 1, 2, 3, 4)} {
		seen := map[string]bool{}
		worlds := map[int]int{}
		for _, s := range r {
			if s.Kind == KindHop {
				continue
			}
			if seen[s.Key()] {
				t.Errorf("duplicate station %s in %s", s.Key(), PlanString(r, 0))
			}
			seen[s.Key()] = true
			if s.Site == SiteHickton {
				worlds[s.World]++
			}
		}
		for w, n := range worlds {
			if n != 1 {
				t.Errorf("Hickton on world %d %d times in %s", w, n, PlanString(r, 0))
			}
		}
		if len(worlds) < 3 {
			t.Errorf("Hickton on %d worlds in %s", len(worlds), PlanString(r, 0))
		}
	}
	// The tail's hops open blocks with no rune shop in them; only the ammo stop is the reason to hop.
	last := len(ring) - 1
	tail := ring[last-2] // hop2
	if tail.Kind != KindHop || tail.World != 2 {
		t.Fatalf("tail %s", PlanString(ring, last-3))
	}
	if shops := BlockShops(ring, last-2); len(shops) != 0 {
		t.Errorf("the tail block holds rune shops %s", PlanString(shops, 0))
	}
	if stops := BlockAmmo(ring, last-2); len(stops) != 1 || stops[0].Key() != "Hickton@2" {
		t.Errorf("the tail block's ammo %s", PlanString(stops, 0))
	}
	// A one-world runner and an empty circuit are unchanged.
	one := BuildRotationRing(RotationInput{M: 1, Worlds: []int{1}, Shops: []string{SiteBetty}, Ammo: []string{SiteHickton}, AmmoEveryWorld: true})
	if got := PlanString(one, 0); got != "[deliver@1 Hickton@1 draw@1 hop1 Betty@1 hop1]" {
		t.Errorf("one world: %s", got)
	}
	if got := PlanString(BuildRotationRing(RotationInput{M: 1, Worlds: three, Ammo: []string{SiteHickton}, AmmoEveryWorld: true}), 0); got != "[deliver@1 hop1]" {
		t.Errorf("no shops: %s", got)
	}
	// The ring survives a restart anywhere in the tail: a runner at Catherby on world 2 resumes at the stop for world 2.
	st := NewState()
	st.StartRound(ring, Index(ring, "Hickton@2"), time.Now())
	st.Rotation = true
	if i, _ := Resume(st, AreaWest, 2, 1); ring[i].Key() != "Hickton@2" {
		t.Errorf("resume moved to %s", ring[i].Key())
	}
}

// An unstackable part (Vial of water, Rope) takes a slot a unit, so a visit buys what the pack has room for, in priority order.
func TestPlanAmmoVisitUnstackableParts(t *testing.T) {
	items := []string{AmmoVial, AmmoBronzeArrow, AmmoRope}
	caps := AmmoCaps(items, nil, true)
	full := map[string]int{AmmoVial: 500, AmmoBronzeArrow: 500, AmmoRope: 20}
	plan := func(free int, stacked map[string]bool, order ...string) AmmoVisitPlan {
		return PlanAmmoVisit(AmmoVisitInput{Shop: Aemad, Items: order, Seen: full, Caps: caps, Spendable: 1 << 30, FreeSlots: free, Stacked: stacked})
	}
	lines := func(p AmmoVisitPlan) string {
		var out []string
		for _, l := range p.Lines {
			out = append(out, fmt.Sprintf("%s=%d", l.Rune, l.Count))
		}
		return strings.Join(out, ",")
	}
	// 22 free slots, vials first: they take every slot and leave none for the arrows' stack.
	p := plan(22, nil, items...)
	if got := lines(p); got != "Vial of water=22" {
		t.Errorf("22 slots, vials first: %s (skips %+v)", got, p.Skips)
	}
	if len(p.Skips) != 2 || !strings.Contains(p.Skips[0].Reason, "no free pack slot") {
		t.Errorf("skips %+v", p.Skips)
	}
	// Arrows first take one slot for 500 and leave the rest for the vials.
	p = plan(22, nil, AmmoBronzeArrow, AmmoVial, AmmoRope)
	if got := lines(p); got != "Bronze arrow=500,Vial of water=21" {
		t.Errorf("arrows first: %s", got)
	}
	// A bronze stack already in the pack costs no slot.
	p = plan(22, map[string]bool{AmmoBronzeArrow: true}, AmmoBronzeArrow, AmmoVial, AmmoRope)
	if got := lines(p); got != "Bronze arrow=500,Vial of water=22" {
		t.Errorf("stacked arrows: %s", got)
	}
	// A vial in the pack does not make vials stack: every unit still needs its slot.
	p = plan(5, map[string]bool{AmmoVial: true}, AmmoVial)
	if got := lines(p); got != "Vial of water=5" {
		t.Errorf("held vials: %s", got)
	}
	// Slots unknown (an estimate): an unstackable part counts a pack's worth, a stackable one its whole shelf.
	p = plan(-1, nil, items...)
	if got := lines(p); got != "Vial of water=24,Bronze arrow=500,Rope=20" {
		t.Errorf("estimate: %s", got)
	}
	if cost := AmmoStationCost(Aemad, full, items, caps, 0); cost != 62+2325+527 {
		t.Errorf("station estimate %d, want %d", cost, 62+2325+527)
	}
	// The plan's coins stay exactly its lines'.
	sum := 0
	for _, l := range p.Lines {
		sum += l.Cost
	}
	if sum != p.Coins {
		t.Errorf("coins %d, lines %d", p.Coins, sum)
	}
	// No slot at all and nothing stacked: nothing.
	if p = plan(0, nil, items...); len(p.Lines) != 0 {
		t.Errorf("0 slots: %+v", p.Lines)
	}
}

// What Zaff's station buys must be on the delivery list or it never reaches the mule: a Staff of fire, and a Battlestaff (they
// were bought and never listed before 2 November 2026).
func TestZaffFireStaffsAreDelivered(t *testing.T) {
	s := DefaultSettings()
	if s.ZaffFireStaffs != 0 || s.ZaffFireStaffCap != fleetplan.ZaffStaffOfAirCost || !s.AmmoEveryWorld {
		t.Errorf("defaults: fire staffs %d cap %d every world %v", s.ZaffFireStaffs, s.ZaffFireStaffCap, s.AmmoEveryWorld)
	}
	r, err := s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if r.Staffs() || strings.Contains(strings.Join(r.DeliverItems(), ","), "staff") {
		t.Errorf("staffs are off by default: %v", r.DeliverItems())
	}
	s.ZaffFireStaffs = 2
	r, err = s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if !r.Staffs() || !containsItem(r.DeliverRuneItems(), StaffOfFire) {
		t.Errorf("fire staffs: Staffs %v, deliver %v", r.Staffs(), r.DeliverRuneItems())
	}
	s.ZaffFireStaffs = 0
	s.ZaffBattlestaffs = 5
	r, _ = s.Resolve()
	if !containsItem(r.DeliverRuneItems(), Battlestaff) || containsItem(r.DeliverRuneItems(), StaffOfFire) {
		t.Errorf("battlestaffs: deliver %v", r.DeliverRuneItems())
	}
	// A full shelf: 1500 then 1530; the default cap (1600) takes both, a cap of 1500 only the first.
	if n, cost := PlanZaff(StaffOfFire, 2, 2, DefaultZaffFireStaffCap, 1<<30); n != 2 || cost != 3030 {
		t.Errorf("full shelf: %d for %d", n, cost)
	}
	if n, _ := PlanZaff(StaffOfFire, 2, 2, 1500, 1<<30); n != 1 {
		t.Errorf("cap 1500: %d", n)
	}
	if got := ZaffEstCost(StaffOfFire, 2, DefaultZaffFireStaffCap); got != 3030 {
		t.Errorf("estimate %d", got)
	}
	s.ZaffFireStaffCap = 0
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "zaffFireStaffCap") {
		t.Errorf("a zero cap: %v", err)
	}
}

func containsItem(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
