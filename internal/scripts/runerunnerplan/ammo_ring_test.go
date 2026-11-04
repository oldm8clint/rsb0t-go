package runerunnerplan

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// liveShops is the shops the one RuneRunner (spare104, Magic 66) works: all four rune shops.
func liveShops() []string { return []string{SiteAubury, SiteBetty, SiteLundail, SiteWizardsGuild} }

func liveRing(ammo []string) []Station {
	return BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2, 3}, Shops: liveShops(), Phase: 2, Staffs: true, Market: true, Ammo: ammo})
}

// realHops counts the hops that change world: a hop to the world the runner is already on is no hop.
func realHops(ring []Station, start int) int {
	real, at := 0, start
	for _, s := range ring {
		if s.Kind == KindHop {
			if s.World != at {
				real++
			}
			at = s.World
		}
	}
	return real
}

// The ring the live runner logged on 13 October 20:39 (round 291, phase 2), character for character, is what
// a runner with ammo off still builds: switching ammoBuy off, or never on, changes nothing.
func TestAmmoOffLeavesTheLiveRingAsItWas(t *testing.T) {
	const live = "[deliver@1 draw@1 market@1 hop3 Betty@3 hop1 Betty@1 hop2 Betty@2 hop2 Aubury@2 Zaff@2 hop1 Aubury@1 Zaff@1 " +
		"hop3 Aubury@3 Zaff@3 hop3 Lundail@3 hop1 Lundail@1 hop2 Lundail@2 hop2 WizardsGuild@2 hop1 WizardsGuild@1 hop3 WizardsGuild@3 hop1]"
	if got := PlanString(liveRing(nil), 0); got != live {
		t.Fatalf("ring\n got %s\nwant %s", got, live)
	}
	if got := PlanString(liveRing([]string{}), 0); got != live {
		t.Fatalf("an empty ammo list changed the ring: %s", got)
	}
}

// THE LIVE RING WITH AMMO. Hickton right after the delivery (before the draw) and as the last stop before the
// hop home on the last world; Gerrant beside every Betty pair, the order snaking with the world order (the
// forward circuit arrives from the dock); Lowe after every Aubury pair and its Zaff station. The same ten real
// hops, and no ammo stop between a hop and the hop's own world except Gerrant before Betty.
func TestAmmoRingLiveRound(t *testing.T) {
	ring := liveRing([]string{SiteGerrant, SiteLowe, SiteHickton})
	const want = "[deliver@1 Hickton@1 draw@1 market@1 hop3 Gerrant@3 Betty@3 hop1 Betty@1 Gerrant@1 hop2 Gerrant@2 Betty@2 " +
		"hop2 Aubury@2 Zaff@2 Lowe@2 hop1 Aubury@1 Zaff@1 Lowe@1 hop3 Aubury@3 Zaff@3 Lowe@3 " +
		"hop3 Lundail@3 hop1 Lundail@1 hop2 Lundail@2 hop2 WizardsGuild@2 hop1 WizardsGuild@1 hop3 WizardsGuild@3 Hickton@3 hop1]"
	if got := PlanString(ring, 0); got != want {
		t.Fatalf("ring\n got %s\nwant %s", got, want)
	}
	base := liveRing(nil)
	if realHops(ring, 1) != realHops(base, 1) || Hops(ring) != Hops(base) || realHops(ring, 1) != 10 {
		t.Errorf("hops: %d real of %d with ammo, %d real of %d without", realHops(ring, 1), Hops(ring), realHops(base, 1), Hops(base))
	}
	// Every key is unique (State.Done, PairStrikes and PriceMismatch are keyed by it).
	seen := map[string]bool{}
	for _, s := range ring {
		if s.Kind == KindHop {
			continue
		}
		if seen[s.Key()] {
			t.Errorf("station key %s appears twice", s.Key())
		}
		seen[s.Key()] = true
	}
	// Ammo is not a rune shop: the blocks the hops open hold the same rune stations as without it.
	for i, s := range ring {
		if s.Kind != KindHop {
			continue
		}
		if got, wantShops := PlanString(BlockShops(ring, i), 0), blockShopsOf(base, s, i, ring); got != wantShops {
			t.Errorf("block at %s: rune stations %s, without ammo %s", s.Key(), got, wantShops)
		}
	}
	for _, s := range ring {
		if IsShopStation(s) && IsAmmoStation(s) {
			t.Errorf("%s is read as a rune shop", s.Key())
		}
	}
	if Index(ring, "Hickton@1") != 1 || Index(ring, "Hickton@3") != len(ring)-2 {
		t.Errorf("Hickton at %d and %d of %d", Index(ring, "Hickton@1"), Index(ring, "Hickton@3"), len(ring))
	}
	// The ring survives the state file.
	st := NewState()
	st.StartRound(ring, 0, time.Now())
	st.Rotation = true
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var back State
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if PlanString(back.Ring, 0) != want {
		t.Errorf("round trip: %s", PlanString(back.Ring, 0))
	}
	if !strings.Contains(string(raw), `"kind":"ammo"`) {
		t.Errorf("an ammo station is not written as kind ammo: %s", raw)
	}
}

// blockShopsOf is the rune stations of the same hop in a ring built without ammo: the hop at index i of the ring
// with ammo is the n'th hop of the ring, and so is the one in base.
func blockShopsOf(base []Station, hop Station, i int, withAmmo []Station) string {
	n := 0
	for j := 0; j <= i; j++ {
		if withAmmo[j].Kind == KindHop {
			n++
		}
	}
	k := 0
	for j, s := range base {
		if s.Kind == KindHop {
			k++
			if k == n {
				return PlanString(BlockShops(base, j), 0)
			}
		}
	}
	return "[]"
}

// A reverse circuit (Lundail first, Betty last) arrives at Port Sarim from Varrock: Betty first, then Gerrant on
// the way to the dock, and the snake alternates from there.
func TestAmmoRingReverseCircuitSnake(t *testing.T) {
	ring := BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2, 3}, Shops: liveShops(), Phase: 5, Ammo: []string{SiteGerrant, SiteHickton}})
	got := PlanString(ring, 0)
	if !strings.Contains(got, "hop2 Betty@2 Gerrant@2 hop1 Gerrant@1 Betty@1 hop3 Betty@3 Gerrant@3 Hickton@3 hop1]") || !strings.HasPrefix(got, "[deliver@1 Hickton@1 draw@1 ") {
		t.Errorf("reverse ring %s", got)
	}
	for phase, want := range map[int]bool{0: false, 2: false, 3: true, 5: true, 6: false, 9: true} {
		if got := RotationReversed(3, phase); got != want {
			t.Errorf("RotationReversed(3, %d) = %v", phase, got)
		}
	}
	if RotationReversed(0, 5) {
		t.Error("no worlds is never reversed")
	}
}

// Hickton's far end is a stop, not a hop: a circuit that ends on the mule's world has Hickton at its start only
// (the round wraps straight into it), and one that ends elsewhere has both ends.
func TestAmmoRingHicktonEnds(t *testing.T) {
	ends := func(shops []string, phase int) string {
		return PlanString(BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2, 3}, Shops: shops, Phase: phase, Ammo: []string{SiteHickton}}), 0)
	}
	// Phase 0, all four shops: the Wizards' Guild is last and runs 3 2 1, ending on the mule's world.
	if got := ends(liveShops(), 0); strings.Count(got, "Hickton") != 1 || !strings.HasSuffix(got, "WizardsGuild@1 hop1]") {
		t.Errorf("circuit ending on the mule's world: %s", got)
	}
	// Without the guild (Magic under 66) Lundail is last and ends on world 3.
	if got := ends([]string{SiteAubury, SiteBetty, SiteLundail}, 0); strings.Count(got, "Hickton") != 2 || !strings.HasSuffix(got, "Lundail@3 Hickton@3 hop1]") {
		t.Errorf("circuit ending on world 3: %s", got)
	}
	// A one-world runner on the mule's world: Hickton once.
	one := BuildRotationRing(RotationInput{M: 1, Worlds: []int{1}, Shops: []string{SiteBetty}, Phase: 0, Ammo: []string{SiteHickton}})
	if got := PlanString(one, 0); got != "[deliver@1 Hickton@1 draw@1 hop1 Betty@1 hop1]" {
		t.Errorf("one world: %s", got)
	}
	// No circuit, no ammo: nothing to anchor the stops to.
	if got := PlanString(BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2, 3}, Shops: nil, Ammo: []string{SiteHickton, SiteLowe, SiteGerrant}}), 0); got != "[deliver@1 hop1]" {
		t.Errorf("no shops: %s", got)
	}
	// A Lundail-first circuit draws nothing: Hickton comes before the purse station.
	back := BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2, 3}, Shops: []string{SiteBetty, SiteAubury, SiteLundail}, Phase: 3, Ammo: []string{SiteHickton}})
	if back[1].Key() != "Hickton@1" || back[2].Kind != KindPurse {
		t.Errorf("reverse ring starts %s", PlanString(back, 0))
	}
}

// Gerrant is placed with Betty and Lowe with Aubury: a round without that shop leaves the ammo shop out.
func TestAmmoRingNeedsItsAnchor(t *testing.T) {
	ring := BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2}, Shops: []string{SiteAubury}, Phase: 0, Ammo: []string{SiteGerrant, SiteLowe}})
	if got := PlanString(ring, 0); got != "[deliver@1 draw@1 hop1 Aubury@1 Lowe@1 hop2 Aubury@2 Lowe@2 hop1]" {
		t.Errorf("Aubury only: %s", got)
	}
	ring = BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2}, Shops: []string{SiteBetty}, Phase: 0, Ammo: []string{SiteGerrant, SiteLowe}})
	if got := PlanString(ring, 0); got != "[deliver@1 draw@1 hop1 Gerrant@1 Betty@1 hop2 Betty@2 Gerrant@2 hop1]" {
		t.Errorf("Betty only: %s", got)
	}
}

// A restart in the middle of a block finds its way back: the entry for an account east on a world the ring
// does not buy on is the hop before the first Betty pair, stepping back over Gerrant, which opens the block.
func TestRotationEntrySteppingOverAmmo(t *testing.T) {
	ring := BuildRotationRing(RotationInput{M: 1, Worlds: []int{1, 2, 3}, Shops: BuiltSites(), Phase: 0, Ammo: []string{SiteGerrant, SiteLowe, SiteHickton}})
	// Phase 0: hop1 Gerrant@1 Betty@1 ...
	if i := Index(ring, "Gerrant@1"); ring[i-1].Key() != "hop1" || ring[i+1].Key() != "Betty@1" {
		t.Fatalf("phase 0 starts %s", PlanString(ring, 0))
	}
	i, why := RotationEntry(ring, AreaEast, 5)
	if ring[i].Key() != "hop1" || ring[i+1].Key() != "Gerrant@1" {
		t.Errorf("east on world 5: %s (%s), want the hop that opens Gerrant@1's block", ring[i].Key(), why)
	}
	// On a world the ring buys on, the entry is that world's Betty or Aubury, not the ammo stop.
	if i, _ := RotationEntry(ring, AreaEast, 2); ring[i].Key() != "Betty@2" {
		t.Errorf("east on world 2: %s", ring[i].Key())
	}
	// Ardougne and the Wilderness are unchanged by ammo stops.
	if i, _ := RotationEntry(ring, AreaArdougne, 5); ring[i].Kind != KindHop || ring[i+1].Key() != "Lundail@1" {
		t.Errorf("Ardougne on world 5: %s", ring[i].Key())
	}
	// A saved cursor on an ammo stop stands.
	st := NewState()
	st.StartRound(ring, Index(ring, "Hickton@1"), time.Now())
	st.Rotation = true
	if i, _ := Resume(st, AreaWest, 1, 1); ring[i].Key() != "Hickton@1" {
		t.Errorf("resume moved off Hickton@1 to %s", ring[i].Key())
	}
}

// A hop that is skipped takes its block's ammo stops with it: the block the skip lands past ends at the next hop.
func TestAmmoStopsLiveInsideTheirBlocks(t *testing.T) {
	ring := liveRing([]string{SiteGerrant, SiteLowe, SiteHickton})
	open := Index(ring, "hop3")
	if got := PlanString(ring[open+1:BlockEnd(ring, open)], 0); got != "[Gerrant@3 Betty@3]" {
		t.Errorf("the first block holds %s", got)
	}
	// The last block (the guild's third world) ends with Hickton, so skipping it skips Hickton@3 too.
	last := -1
	for i, s := range ring {
		if s.Kind == KindHop && s.World == 3 && i > Index(ring, "WizardsGuild@1") {
			last = i
		}
	}
	if got := PlanString(ring[last+1:BlockEnd(ring, last)], 0); got != "[WizardsGuild@3 Hickton@3]" {
		t.Errorf("the last block holds %s", got)
	}
	if shops := BlockShops(ring, last); len(shops) != 1 || shops[0].Key() != "WizardsGuild@3" {
		t.Errorf("the last block's rune stations %v", shops)
	}
}
