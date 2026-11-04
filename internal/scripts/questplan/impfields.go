package questplan

import (
	"sort"

	"rsb0t-go/internal/scripts/tile"
)

// The places Imp Catcher's beads are farmed.
//
// The four beads drop 5/128 each from any imp, so they cost about 53 kills whichever imp they are taken from, and the
// supply is the imps content spawns: one per spawn, back 100 ticks (a minute) after its death. The quest used to farm
// one field (Ardougne's south strip, nine spawns) and its slot admits twelve accounts at once. Live 16 Oct 2026, the
// first 95 minutes after the quest was promoted: twelve accounts on those nine imps secured 33 beads in 11.5 account
// hours (about 3 an hour each), and of the 15 runs that ended on their own 4 completed, 3 parked and 8 ran out of their
// 56 minute slot (seven of them short of beads, one holding all four). The kills were there to be had; the imps were not
// (one imp, one kill a minute, shared by twelve).
//
// Content has 47 imp spawns. Three places hold eight or nine of them within a few tiles of one another; every other
// spawn stands alone (Draynor 1, Rimmington 1, Falador 5 in three groups, Lumbridge 4, Varrock and Edgeville 11 spread
// over seventy tiles), too thin to feed even one account. TestImpSitesAreTheContentsClusters keeps this list honest
// against the maps.
//
//   - Ardougne south (m41_50): nine spawns on open ground, the monastery to its west.
//   - Yanille north (m40_48): eight spawns on open ground between the town and the Fight Arena; the Yanille bank is 31 tiles
//     away, where Ardougne's field has its nearest bank 69 tiles off.
//   - Karamja volcano (m44_49): eight spawns that are left out. They are across the sea (a 30 coin boat from Ardougne's
//     dock, which the walker's ship hop has refused or fumbled in the live log: "nothing happened after the op",
//     "could not get beside Captain Barnaby"), with no bank on the island and a coast of scorpions and snakes that
//     killed five of this quest's six deaths in the first week of October (impBank's note). An account is never asked there.

// ImpSite is one place to farm imps.
type ImpSite struct {
	// ID names it in the log and in the slot table.
	ID, Name string
	// Stand is a walkable tile near the middle of the spawns, where an account waits and regroups.
	Stand tile.Tile
	// Spawns are the content's imp spawn tiles.
	Spawns []tile.Tile
	// Leash is how far from Stand an imp may be and still be this site's: imps teleport up to 20 tiles on a hit and every
	// 50 to 200 ticks, so one wanders off the strip within minutes, and an account that follows it follows it to town.
	Leash int
}

// ImpSiteArdougne is the strip south of East Ardougne, the TS def's field.
var ImpSiteArdougne = ImpSite{
	ID: "ardougne-south", Name: "Ardougne south",
	Stand: tile.Tile{X: 2629, Z: 3217},
	Spawns: []tile.Tile{
		{X: 2625, Z: 3203}, {X: 2625, Z: 3217}, {X: 2629, Z: 3233}, {X: 2630, Z: 3210}, {X: 2632, Z: 3202},
		{X: 2633, Z: 3222}, {X: 2633, Z: 3243}, {X: 2639, Z: 3206}, {X: 2639, Z: 3230},
	},
	Leash: 35,
}

// ImpSiteYanille is the open ground north of Yanille.
var ImpSiteYanille = ImpSite{
	ID: "yanille-north", Name: "Yanille north",
	Stand: tile.Tile{X: 2610, Z: 3125},
	Spawns: []tile.Tile{
		{X: 2595, Z: 3131}, {X: 2597, Z: 3120}, {X: 2606, Z: 3126}, {X: 2608, Z: 3134}, {X: 2614, Z: 3116},
		{X: 2616, Z: 3126}, {X: 2621, Z: 3133}, {X: 2622, Z: 3118},
	},
	Leash: 35,
}

// ImpSites are the sites an account may be sent to, in the order ties are broken.
var ImpSites = []ImpSite{ImpSiteArdougne, ImpSiteYanille}

// ImpSiteByID finds a site by its ID.
func ImpSiteByID(id string) (ImpSite, bool) {
	for _, s := range ImpSites {
		if s.ID == id {
			return s, true
		}
	}
	return ImpSite{}, false
}

// Has reports whether an imp at t is this site's: on the same level and within the leash of the stand.
func (s ImpSite) Has(t tile.Tile) bool {
	return t.Level == s.Stand.Level && tile.Chebyshev(t, s.Stand) <= s.Leash
}

// PickImpSite chooses the site an account farms. farmers is how many OTHER accounts work each site now (by ID); the
// choice is the site where one more account leaves the fewest per spawn, so twelve accounts fall six and six over
// seventeen spawns, not twelve over nine. Equal crowding goes to the site nearest from when it is known, then to the
// first in ImpSites.
func PickImpSite(farmers map[string]int, from tile.Tile, known bool) ImpSite {
	best := 0
	for i := 1; i < len(ImpSites); i++ {
		a, b := ImpSites[i], ImpSites[best]
		// (farmers+1)/spawns, compared without division.
		la, lb := (farmers[a.ID]+1)*len(b.Spawns), (farmers[b.ID]+1)*len(a.Spawns)
		switch {
		case la < lb:
			best = i
		case la == lb && known && tile.Chebyshev(from, a.Stand) < tile.Chebyshev(from, b.Stand):
			best = i
		}
	}
	return ImpSites[best]
}

// Patrol is the order an account looks for imps in when none is in view: the site's spawns, nearest first from from and
// each next one the nearest to the last. A killed imp comes back on its spawn tile, and the client shows only the
// ground near the account, so an account idling at the stand sees the middle of the strip and none of its ends.
func (s ImpSite) Patrol(from tile.Tile) []tile.Tile {
	left := append([]tile.Tile(nil), s.Spawns...)
	// A stable start whatever the order the table lists them in.
	sort.Slice(left, func(i, j int) bool {
		if left[i].X != left[j].X {
			return left[i].X < left[j].X
		}
		return left[i].Z < left[j].Z
	})
	out := make([]tile.Tile, 0, len(left))
	at := from
	for len(left) > 0 {
		bi := 0
		for i := 1; i < len(left); i++ {
			if tile.Chebyshev(at, left[i]) < tile.Chebyshev(at, left[bi]) {
				bi = i
			}
		}
		out = append(out, left[bi])
		at = left[bi]
		left = append(left[:bi], left[bi+1:]...)
	}
	return out
}

// ImpCandidate is an imp in view, as the farm sees it.
type ImpCandidate struct {
	// At is where it stands.
	At tile.Tile
	// Steps is how many steps a walk takes to reach it over open ground (no door opened, no gate), -1 when none does:
	// the monastery's monks' rooms, the Fight Arena's yard and every other pocket an imp's teleport can drop it into.
	Steps int
	// Busy is whether another account is fighting it (it faces another player and has just been hit): in single combat
	// the server refuses a second attacker.
	Busy bool
}

// PickImp chooses the imp to fight: of those the site claims (the leash), an open-ground walk reaches and nobody else is
// fighting, the fewest steps away. False when none qualifies, which sends the account round the patrol. An imp behind a
// door is not a candidate: the walker would open the door and follow it (the monastery's two doors were crossed 20 times
// an hour by one account), a pocket without a door would hold the fight loop for its three minutes ("attacking again"
// 1,517 times in 95 minutes), and Yanille's pocket is the Fight Arena. A busy imp is not either: the fields are single
// combat, the server refuses a second attacker, and the attack would be sent again for as long as the fight lasts.
func PickImp(site ImpSite, cands []ImpCandidate) (int, bool) {
	best, bestSteps := -1, 0
	for i, c := range cands {
		if c.Steps < 0 || c.Busy || !site.Has(c.At) {
			continue
		}
		if best < 0 || c.Steps < bestSteps {
			best, bestSteps = i, c.Steps
		}
	}
	return best, best >= 0
}
