package webwalk

import (
	"fmt"
	"testing"
)

// basaltState is a members account with the given Agility, as the planner sees it.
func basaltState(agility int) *WorldState {
	return &WorldState{Members: true, Skills: map[string]int{"agility": agility}, FreeSlots: 28,
		QuestStatus: func(string) QuestProgress { return QuestNotStarted },
		ItemCount:   func(string) int { return 0 }, WornCount: func(string) int { return 0 }}
}

// basaltPlan plans a walk as a ScarabSwarm account of combat level 57 does: every automatic danger zone for that
// level on (the Rellekka-road wolves close the overland road to anything at combat 64 or under, which is why the rocks
// are its only way to Rellekka and back), the special crossings and NPC hops allowed.
func basaltPlan(p *Planner, from, to Point, agility int) Route {
	opt := Options{AllowNPC: true, AllowSpecial: true, State: basaltState(agility)}
	opt.AvoidZones = zonesWithout("", from, to, 57)
	return p.Plan(from, to, opt)
}

func rockIDs(r Route) []int {
	var ids []int
	for _, h := range r.Hops() {
		if h.LocName == "Basalt rock" {
			ids = append(ids, h.LocID)
		}
	}
	return ids
}

var (
	ardougneBank   = Point{X: 2655, Z: 3283}
	seersBank      = Point{X: 2725, Z: 3491}
	outpost        = Point{X: 2544, Z: 3572}
	mulePost       = Point{X: 2809, Z: 3441}
	olaf           = Point{X: 2738, Z: 3638}
	southboundRock = []int{4559, 4557, 4555, 4553, 4551}
	northboundRock = []int{4550, 4552, 4554, 4556, 4558}
)

// TestEveryTileOnTheBasaltCausewayHasAWayHome plans from each tile a jump, or a slip, can leave an account on to the
// mainland, for an Agility 1 account as well as an Agility 35 one: content lets any account jump, so the way back must
// not be gated on the level that gates the way out. Before the correction every one of the Agility 1 plans was
// "unreachable", and the account stayed where it stood however long it waited.
func TestEveryTileOnTheBasaltCausewayHasAWayHome(t *testing.T) {
	p := realPack(t)
	tiles := []Point{
		{X: 2515, Z: 3619}, // spot 8's miss: the tide sweeps a southbound jumper back to the Lighthouse shore (the strand of 14 Oct)
		{X: 2514, Z: 3620}, // spot 9's landing, the Lighthouse shore
		{X: 2514, Z: 3616}, // spot 7's and spot 10's landing
		{X: 2514, Z: 3612}, // spot 8's landing
		{X: 2515, Z: 3611}, // spot 5's landing
		{X: 2518, Z: 3610}, // spot 6's landing
		{X: 2522, Z: 3603}, // spot 3's landing
		{X: 2522, Z: 3599}, // spot 4's landing
		{X: 2522, Z: 3598}, // spot 1's landing
		{X: 2521, Z: 3595}, // spot 3's miss: back on the Barbarian Outpost shore
	}
	for _, agility := range []int{1, 35} {
		for _, from := range tiles {
			for _, to := range []Point{ardougneBank, seersBank, outpost} {
				r := basaltPlan(p, from, to, agility)
				if !r.OK {
					t.Errorf("agility %d: no way from %s to %s (%s)", agility, from, to, r.Reason)
				}
			}
		}
	}
	// The strand tile's whole way, spot by spot.
	r := basaltPlan(p, Point{X: 2515, Z: 3619}, ardougneBank, 1)
	if got := rockIDs(r); !equalInts(got, southboundRock) {
		t.Errorf("2515,3619 -> East Ardougne jumps %v, want the five southbound rocks %v", got, southboundRock)
	}
}

// TestRellekkaHasAWayHomeAtAnyAgility is the whole trip back from Olaf's trial: over the Broken bridge and then the
// five southbound rocks, for the Agility 35 account that went and for an Agility 1 one (a fail-teleport, a stat sync
// that has not arrived, a script's choice put it there).
func TestRellekkaHasAWayHomeAtAnyAgility(t *testing.T) {
	p := realPack(t)
	for _, agility := range []int{1, 35} {
		for _, to := range []Point{seersBank, ardougneBank, mulePost} {
			r := basaltPlan(p, olaf, to, agility)
			if !r.OK {
				t.Errorf("agility %d: no way from Olaf's trial %s to %s (%s)", agility, olaf, to, r.Reason)
				continue
			}
			if got := rockIDs(r); !equalInts(got, southboundRock) {
				t.Errorf("agility %d: %s -> %s jumps %v, want %v", agility, olaf, to, got, southboundRock)
			}
		}
	}
}

// TestNoRouteToRellekkaStrandsOnTheLighthouseSide is the invariant: a route over the rocks exists only for an account
// that can also get back, and every tile it stands on, jumps to or can be swept to has its own way home. The Lighthouse
// side is not a dead end for Olaf's trial (its north shore runs east to Rellekka over the Broken bridge); the danger was
// a gate on one direction only.
func TestNoRouteToRellekkaStrandsOnTheLighthouseSide(t *testing.T) {
	p := realPack(t)
	for _, from := range []Point{seersBank, ardougneBank, mulePost, outpost} {
		for _, agility := range []int{1, 20, 34, 35} {
			r := basaltPlan(p, from, olaf, agility)
			if agility < 35 {
				if r.OK {
					t.Errorf("agility %d: %s has a route to Olaf's trial (cost %d): an account that cannot take the northbound dice rock is sent over the water", agility, from, r.Cost)
				}
				continue
			}
			if !r.OK {
				t.Errorf("agility %d: no route from %s to Olaf's trial (%s)", agility, from, r.Reason)
				continue
			}
			if got := rockIDs(r); !equalInts(got, northboundRock) {
				t.Errorf("%s -> Olaf's trial jumps %v, want the five northbound rocks %v", from, got, northboundRock)
			}
			// Every tile the account can end up on along the way: each hop's stand and landing, the tile a slip
			// sweeps a dice rock's jumper to, and the two tiles the Broken bridge's miss lands on.
			stand := map[Point]string{}
			for _, h := range r.Hops() {
				if h.LocName != "Basalt rock" && h.LocName != "Broken bridge" {
					continue // the road to the rocks has its own tests
				}
				stand[h.From] = fmt.Sprintf("%s's stand", h.LocName)
				stand[h.To] = fmt.Sprintf("%s's landing", h.LocName)
				if h.Slip != nil {
					stand[h.Slip.Lands] = fmt.Sprintf("%s's slip", h.LocName)
				}
			}
			stand[Point{X: 2598, Z: 3608}] = "the Broken bridge's eastbound miss"
			stand[Point{X: 2596, Z: 3608}] = "the Broken bridge's westbound miss"
			for tile, why := range stand {
				if back := basaltPlan(p, tile, from, agility); !back.OK {
					t.Errorf("agility %d: %s (%s) on the way from %s to Olaf's trial has no way back (%s)", agility, tile, why, from, back.Reason)
				}
			}
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
