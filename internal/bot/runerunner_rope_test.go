package bot

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/scripts/tile"
)

// The Fishing Trawler's Rope round of 4 November 2026 (runerunnerplan/ammo.go, "Rope for the Fishing Trawler's net"): Port Khazard
// and Ned in the ring, the Rope-only shelf, the bank trip after a full pack of single Ropes, and the floor Ned keeps the mule at.

// ropeRig is an ammoRig with the trawlerRope default restored (ammoRig turns it off for the older tests).
func ropeRig(t *testing.T, name string, base int, extra map[string]interface{}) *rotRig {
	t.Helper()
	set := map[string]interface{}{"trawlerRope": true, "ammoShops": []interface{}{"Gerrant", "Lowe", "Hickton"}}
	for k, v := range extra {
		set[k] = v
	}
	return ammoRig(t, name, base, set)
}

// khazardShelf is the shelf as the shop panel shows it: the crew's whole kit.
func khazardShelf() map[string]int {
	return map[string]int{"Swamp paste": 500, "Rope": 30, "Bailing bucket": 30}
}

func TestRopeRoundIsInTheRingAndTheStartLine(t *testing.T) {
	r := ropeRig(t, "roperig1", 200, nil)
	ring := runerunnerplan.PlanString(r.b.st.Ring, 0)
	for _, want := range []string{"hop202 PortKhazard@202 Aemad@202 Lundail@202 hop200 PortKhazard@200 Aemad@200 Lundail@200", "Betty@202 Ned@202", "Betty@200 Gerrant@200 Ned@200"} {
		if !strings.Contains(ring, want) {
			t.Errorf("ring has no %q:\n%s", want, ring)
		}
	}
	if !strings.Contains(r.log(), "sites=Gerrant,Lowe,Hickton,PortKhazard,Aemad,Ned") || !strings.Contains(r.log(), "Rope") {
		t.Errorf("start line\n%s", r.log())
	}
	if got := strings.Join(r.b.set.DeliverNotedList, ","); got != "Rope" {
		t.Errorf("deliverNoted %q", got)
	}
}

// Port Khazard buys only the Rope (the paste and buckets are the crew's), banks the pack at East Ardougne after a full pack of
// single Ropes, and leaves the shelf alone on the world where the anchor's block runs.
func TestKhazardStationBuysRopeOnlyAndBanksAfter(t *testing.T) {
	r := ropeRig(t, "roperig2", 210, nil)
	var banked []tile.Tile
	r.b.bankPackSeam = func(at tile.Tile) { banked = append(banked, at) }
	r.b.trawlerBlockSeam = func() (int, bool) { return 0, false }
	r.atKey(t, "PortKhazard@212")
	r.ctx.CurrentWorld = 212
	r.pack(r.rich(), 24, nil)
	var wanted []string
	r.b.ammoBuySeam = func(site runerunnerplan.AmmoSite, planFor func(map[string]int, int) fleetplan.PurchasePlan) runerunner.AmmoVisit {
		seen := map[string]int{}
		for item, n := range khazardShelf() {
			if site.Shop.Sells(item) {
				seen[item] = n
			}
		}
		plan := planFor(seen, r.rich())
		v := runerunner.AmmoVisit{Walked: true, Plan: plan, ShopVisit: fleetrunner.ShopVisit{Seen: seen, Bought: map[string]int{}}}
		for _, l := range plan.Lines {
			wanted = append(wanted, l.Rune)
			v.Bought[l.Rune] = l.Count
			v.Spent += site.Shop.CostToBuy(l.Rune, seen[l.Rune], l.Count)
		}
		return v
	}
	got := r.runAmmo()
	if got.Outcome != runerunnerplan.OutcomeBought || len(wanted) != 1 || wanted[0] != "Rope" || got.Bought["Rope"] != 23 || got.Bought["Swamp paste"] != 0 || got.Bought["Bailing bucket"] != 0 {
		t.Fatalf("Khazard: %+v planned %v\n%s", got, wanted, r.log())
	}
	if len(banked) != 1 || banked[0] != runerunnerplan.ArdougneBank {
		t.Errorf("the pack was banked at %v, want the East Ardougne bank once", banked)
	}
	if !strings.Contains(r.log(), "ammo-bank station=PortKhazard@212") {
		t.Errorf("no bank line\n%s", r.log())
	}

	// khazardRopeOnly off takes the crew's paste and buckets too (the owner's 2 Nov list), and the Rope still comes first only if listed so.
	r2 := ropeRig(t, "roperig3", 220, map[string]interface{}{"khazardRopeOnly": false, "ammoItems": []interface{}{"Swamp paste", "Rope", "Bailing bucket"}, "ammoStopAbove": []interface{}{}})
	if items := r2.b.ammoItemsAt(mustSite(t, runerunnerplan.SitePortKhazard)); strings.Join(items, ",") != "Swamp paste,Rope,Bailing bucket" {
		t.Errorf("khazardRopeOnly false: %v", items)
	}
	if items := r.b.ammoItemsAt(mustSite(t, runerunnerplan.SitePortKhazard)); strings.Join(items, ",") != "Rope" {
		t.Errorf("khazardRopeOnly true: %v", items)
	}

	// The anchor's block on this world: the crew's shelf is not touched; on another world it is.
	r3 := ropeRig(t, "roperig4", 230, nil)
	r3.b.trawlerBlockSeam = func() (int, bool) { return 232, true }
	r3.b.bankPackSeam = func(tile.Tile) {}
	r3.atKey(t, "PortKhazard@232")
	r3.ctx.CurrentWorld = 232
	r3.pack(r3.rich(), 24, nil)
	visits := 0
	r3.shelf(khazardShelf(), r3.rich(), &visits, 1)
	if res := r3.runAmmo(); res.Outcome != runerunnerplan.OutcomeSkipped || visits != 0 || !strings.Contains(res.Reason, "Fishing Trawler block is on world 232") {
		t.Errorf("block world: %+v visits %d", res, visits)
	}
	r3.atKey(t, "PortKhazard@230")
	r3.ctx.CurrentWorld = 230
	if res := r3.runAmmo(); res.Outcome != runerunnerplan.OutcomeBought || visits != 1 {
		t.Errorf("other world: %+v visits %d\n%s", res, visits, r3.log())
	}
}

func mustSite(t *testing.T, key string) runerunnerplan.AmmoSite {
	t.Helper()
	s, ok := runerunnerplan.AmmoSiteFor(key)
	if !ok {
		t.Fatalf("no site %s", key)
	}
	return s
}

// Ned buys the part of ropeFloor the mule and the pack leave short, banks at Draynor, and a Ned who sells nothing strikes the pair.
func TestNedStationBuysToTheFloor(t *testing.T) {
	r := ropeRig(t, "roperig5", 240, map[string]interface{}{"ropeFloor": 100.0})
	publishRuneMule(r, map[string]int{"Rope": 91})
	var banked []tile.Tile
	var asked []int
	r.b.bankPackSeam = func(at tile.Tile) { banked = append(banked, at) }
	r.b.nedBuySeam = func(want int) (int, string) { asked = append(asked, want); return want, "" }
	r.atKey(t, "Ned@242")
	r.ctx.CurrentWorld = 242
	r.pack(0, 24, map[string]int{"Rope": 0})
	// The mule holds 91: 9 short of the floor, over the 6-rope minimum.
	res := r.runAmmo()
	if res.Outcome != runerunnerplan.OutcomeBought || res.Bought["Rope"] != 9 || res.Spent != 9*15 || len(asked) != 1 || asked[0] != 9 {
		t.Fatalf("Ned: %+v asked %v\n%s", res, asked, r.log())
	}
	if len(banked) != 1 || banked[0] != runerunnerplan.DraynorBank {
		t.Errorf("banked at %v, want Draynor", banked)
	}

	// The floor met: no walk, no conversation.
	publishRuneMule(r, map[string]int{"Rope": 400})
	r.atKey(t, "Ned@240")
	r.ctx.CurrentWorld = 240
	asked = nil
	if res = r.runAmmo(); res.Outcome != runerunnerplan.OutcomeSkipped || len(asked) != 0 || !strings.Contains(res.Reason, "floor 100 is met") {
		t.Errorf("floor met: %+v asked %v", res, asked)
	}

	// Ropes in the pack count toward the floor, and the pack's room caps the order.
	publishRuneMule(r, map[string]int{"Rope": 0})
	r.atKey(t, "Ned@241")
	r.ctx.CurrentWorld = 241
	r.pack(0, 13, map[string]int{"Rope": 20})
	if res = r.runAmmo(); res.Outcome != runerunnerplan.OutcomeBought || len(asked) != 1 || asked[0] != 12 {
		t.Errorf("pack room: %+v asked %v (13 free less the reserve %d)\n%s", res, asked, r.b.set.AmmoSlotReserve, r.log())
	}

	// A Ned who sells nothing is a strike, and nothing is banked.
	r2 := ropeRig(t, "roperig6", 250, map[string]interface{}{"ropeFloor": 100.0})
	var banked2 int
	r2.b.bankPackSeam = func(tile.Tile) { banked2++ }
	r2.b.nedBuySeam = func(int) (int, string) { return 0, "Ned is not in view" }
	r2.atKey(t, "Ned@252")
	r2.ctx.CurrentWorld = 252
	r2.pack(0, 24, nil)
	if res = r2.runAmmo(); res.Outcome != runerunnerplan.OutcomeAborted || banked2 != 0 || !strings.Contains(res.Reason, "Ned is not in view") {
		t.Errorf("failed Ned: %+v banked %d", res, banked2)
	}
	// ropeFloor 0 takes Ned out of the ring altogether.
	r3 := ropeRig(t, "roperig7", 260, map[string]interface{}{"ropeFloor": 0.0})
	if got := runerunnerplan.PlanString(r3.b.st.Ring, 0); strings.Contains(got, "Ned@") || !strings.Contains(got, "PortKhazard@") {
		t.Errorf("ropeFloor 0 ring %s", got)
	}
}

// The delivery's item lists: Rope travels with the other ammo parts, and the loose-item deposit leaves it to be drawn as notes.
func TestRopeDeliveryListsAndDeposit(t *testing.T) {
	r := ropeRig(t, "roperig8", 270, nil)
	items := r.b.set.DeliverItems()
	if items[len(items)-1] != "Rope" {
		t.Errorf("delivery items %v", items)
	}
	kept := withoutFold(items, r.b.set.DeliverNotedList)
	for _, k := range kept {
		if strings.EqualFold(k, "Rope") {
			t.Errorf("Rope is kept in the pack through the deposit: %v", kept)
		}
	}
	if len(kept) != len(items)-1 {
		t.Errorf("kept %d of %d items", len(kept), len(items))
	}
}
