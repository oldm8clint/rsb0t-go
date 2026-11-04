package randomevents

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
)

// This file watches for the one thing nothing else in this package watches: the account being
// MOVED while Guardian is not handling anything at all.
//
// checkFailTeleport (handle.go) already catches a fail-teleport, but only from inside a
// dialog-NPC handler, guarding one specific click, against a tile captured immediately before
// it. That covers an event Guardian engaged with which then moved the account mid-handle. It
// cannot cover an event Guardian never engaged with, and content fires exactly that case on the
// event's OWN timer: macro_event_genie.rs2:33 and macro_event_mysterious_old_man.rs2:71 both
// queue(macro_event_fail_teleport, 0, 0) when their NPC is ignored long enough, and
// macro_events.rs2:120-126 then picks a destination at random out of
// macro_event_fail_teleports_members (or _free on a free world). None of that needs this client
// to have detected the NPC, so an undetected event relocates the account with no handler running
// to notice and no line written anywhere.
//
// The live case this was built from, 2026-09-19, the bankspin -mode marathon arm: the account
// was at 3114,9835 in the Edgeville dungeon at 28m59s and at 3278,3500 level 1 at 30m11s, with
// NO [guardian] line anywhere in the log and its only shop trip 28 minutes earlier. Nobody could
// explain it. 3278,3500 level 1 is mapsquare 51,54 level 1, and
// content/scripts/macro events/configs/macro_events_teleports.enum's own
// macro_event_fail_teleports_free val=0 is 1_51_54_14_44 — mapsquareX*64+14 = 3278,
// mapsquareZ*64+44 = 3500, level 1. The Jolly Boar Inn, exactly. The same session's earlier
// unexplained take failure, where the account stood at 2434,3346 while the bones lay at
// 3113,9833, is mapsquare 38,52, which is the members table's own "The Outpost, Ardougne". Two
// separate incidents, both landing precisely on a catalogued fail-teleport destination, both
// unexplained only because nothing was watching.
//
// This only ever LOGS. It deliberately does not teleport, walk or recover: every script's own
// travel recovers from a displacement unaided — the live case above walked itself down a
// staircase and back to its bank at the cost of no death and no failed check. Making the move
// visible is the entire job here, so that the next occurrence explains itself instead of costing
// another session. (When this was written, checkFailTeleport's own recovery hardcoded the
// Catherby bank, which was a second reason not to recover from here. That is fixed:
// failTeleportHome now returns the account to the tile it was standing on before the jump, with
// the script's own SafeRef as the fallback.)

// maxTilesPerPoll is how far the account could legitimately have WALKED between two polls, per
// pollInterval of elapsed time. A player runs two tiles per game tick, and a stock engine ticks
// every 600ms — but the local test engine runs at 200ms, three times as fast, so a walk covers
// three times the ground per second of wall clock. Ten is that worst case with room to spare, so
// a fast engine cannot manufacture a "displacement" out of ordinary running.
const maxTilesPerPoll = 10

// displacementSlack is a flat allowance on top of the gap-scaled one, absorbing a poll loop that
// was descheduled or a burst of movement packets that all landed between two samples. A real
// fail-teleport crosses hundreds or thousands of tiles (the live case above moved 6,335 in z),
// so a generous floor costs nothing in detection and buys immunity to the false-positive class
// that made checkFailTeleport widen its own threshold from 15 tiles to 100.
const displacementSlack = 32

// undergroundMapsquareZ is how far south this content build stores the underground counterpart
// of a surface mapsquare, the same offset LooksLikeUndergroundMapsquare tests against. Named
// here because the line that reports a dungeon entrance has to quote it, and quoting the wrong
// half of the pair is what made the climb OUT print "mapsquareZ 54 is >=100".
const undergroundMapsquareZ = 100

// DisplacementKind says what a jump looks like, so a reader can tell an ordinary dungeon
// entrance from something nobody asked for.
type DisplacementKind string

const (
	// DisplacementFailTeleport is the loud one: the destination mapsquare is a catalogued
	// Genie/Mysterious Old Man "ignored too long" fail-teleport destination.
	DisplacementFailTeleport DisplacementKind = "fail-teleport"
	// DisplacementUnderground is this content build's mapsquareZ+100 convention for the
	// underground counterpart of a surface area — an ordinary dungeon entrance, such as the
	// Edgeville trapdoor's own p_telejump of 6400 tiles south in z.
	DisplacementUnderground DisplacementKind = "underground"
	// DisplacementLevelChange is an ordinary stair or ladder: the level moved, the tile did not
	// move further than a walk explains.
	DisplacementLevelChange DisplacementKind = "level change"
	// DisplacementEvent is a jump with a random event standing at the far end of it, which is
	// what a maze or a mime teleport looks like from here: the account is moved and the event
	// that moved it is right there to be detected.
	DisplacementEvent DisplacementKind = "random event"
	// DisplacementSpawnPoint is a jump that ENDS on the Lumbridge spawn tile. Two ordinary
	// things land an account there — dying, and finishing Tutorial Island — and a position
	// reading cannot tell them apart, so this deliberately names the place rather than guessing
	// the cause.
	DisplacementSpawnPoint DisplacementKind = "spawn point"
	// DisplacementDeck is a short hop that ends one level up, which is what boarding a ship
	// looks like from here: the deck is a level-1 tile a little way from the dock.
	DisplacementDeck DisplacementKind = "boarded"
	// DisplacementFixture is a move the fleet's own scripts make on purpose: a lever, a ladder, the end
	// of a Mime. See KnownFixture. Naming these is what stopped 3,500 of the 3,722 "UNEXPLAINED" lines
	// in the 2026-09-29 to 2026-10-02 log from drowning the two lines that mattered.
	DisplacementFixture DisplacementKind = "fixture"
	// DisplacementStrandSite is a landing in one of the places the server summons whole cohorts into
	// (StrandSite): the King Black Dragon's lair, Rogues' Castle. It is checked ahead of the
	// underground rule on purpose: the lair's mapsquareZ is above 100, which used to read as "an
	// ordinary dungeon entrance" for the 224 accounts the server dropped there.
	DisplacementStrandSite DisplacementKind = "strand site"
	// DisplacementUnexplained is a jump that matches none of the above, which is exactly the
	// thing worth a line in the log.
	DisplacementUnexplained DisplacementKind = "unexplained"
)

// Displacement is one observed jump, carrying the readings that justify calling it one.
type Displacement struct {
	FromX, FromZ, FromLevel int
	ToX, ToZ, ToLevel       int
	Tiles                   int
	Gap                     time.Duration
	Kind                    DisplacementKind
	// Name is the fail-teleport destination's own name from the enum, when Kind is
	// DisplacementFailTeleport; the fixture's or site's own name for DisplacementFixture and
	// DisplacementStrandSite.
	Name string
}

// String is the log line, written so the next occurrence explains itself without anyone having
// to go and decode a mapsquare by hand.
func (d Displacement) String() string {
	where := fmt.Sprintf("%d,%d,%d -> %d,%d,%d (%d tiles in %s)",
		d.FromX, d.FromZ, d.FromLevel, d.ToX, d.ToZ, d.ToLevel, d.Tiles, d.Gap.Round(100*time.Millisecond))
	switch d.Kind {
	case DisplacementFailTeleport:
		// States the reading and the one fact behind it — that this mapsquare is in the enum —
		// and stops short of asserting a cause nothing here observed. The first live run of this
		// watcher printed a confident "an event was ignored long enough for content's own timer
		// to move the account" over a ::tele the harness itself had just issued, which is the
		// same class of unjustified verdict internal/harness exists to prevent.
		return fmt.Sprintf("displaced: %s — the destination is %s, one of the fixed set of Genie/Mysterious Old Man "+
			"fail-teleport destinations (macro_event_fail_teleports_*, mapsquare %d,%d level %d). Absent a deliberate "+
			"teleport, an ignored event's own timer is the known mechanism that lands an account here, and it needs no "+
			"NPC this client ever detected — which is how this happens with no event handled and no line about one",
			where, d.Name, d.ToX>>6, d.ToZ>>6, d.ToLevel)
	case DisplacementUnderground:
		// The two directions need different wording, and sharing one line made the climb OUT
		// print its own evidence as a falsehood: "an underground counterpart mapsquare
		// (mapsquareZ 54 is >=100)", measured live 2026-09-19 on the Edgeville ladder. The
		// destination of a climb out is the SURFACE square, so the >=100 test is the wrong half
		// of the pair to quote — what justifies it is that the two squares are 100 apart.
		if d.ToZ>>6 >= undergroundMapsquareZ {
			return fmt.Sprintf("displaced: %s — an underground counterpart mapsquare (mapsquareZ %d is >=%d, this build's own "+
				"convention for the level below a surface area), which is what an ordinary dungeon entrance looks like",
				where, d.ToZ>>6, undergroundMapsquareZ)
		}
		return fmt.Sprintf("displaced: %s — back up to the surface counterpart of the mapsquare it came from (mapsquareZ %d and %d, "+
			"the pair this build stores exactly %d apart), which is what climbing out of a dungeon looks like",
			where, d.FromZ>>6, d.ToZ>>6, undergroundMapsquareZ)
	case DisplacementLevelChange:
		return fmt.Sprintf("displaced: %s — an ordinary level change, the shape of a staircase or ladder", where)
	case DisplacementEvent:
		return fmt.Sprintf("displaced: %s — a random event (%s) is present at the destination, which is how a maze or mime "+
			"teleport looks from here: the move IS the event, and Guardian's own handling of it follows", where, d.Name)
	case DisplacementSpawnPoint:
		return fmt.Sprintf("displaced: %s — it ends on the Lumbridge spawn tile, where both a death respawn and the Tutorial "+
			"Island exit put an account down. Which of the two this was is not visible from a position reading", where)
	case DisplacementDeck:
		if d.Name != "" {
			return fmt.Sprintf("displaced: %s — it ends exactly where %s sets a passenger down (content's own ~set_sail "+
				"destination), so this is an ordinary crossing rather than anywhere an event would strand an account", where, d.Name)
		}
		return fmt.Sprintf("displaced: %s — a short hop that ends one level up, the shape of boarding a ship: the deck is a "+
			"level-1 tile near the dock rather than anywhere an event would strand an account", where)
	case DisplacementFixture:
		return fmt.Sprintf("displaced: %s — expected: %s", where, d.Name)
	case DisplacementStrandSite:
		return fmt.Sprintf("displaced: %s — STRAND SITE: %s. Not a stock random event (macro_events_teleports.enum has no such destination): "+
			"the server summons whole cohorts here. The Guardian's escape ladder takes over (see strandsites.go)", where, d.Name)
	default:
		return fmt.Sprintf("displaced: %s — UNEXPLAINED: not a catalogued fail-teleport destination, not an underground "+
			"mapsquare, and too far to have been walked", where)
	}
}

// The Lumbridge spawn tile, and how close a landing has to be to count as it. Both a death
// respawn and the Tutorial Island exit put an account here, and the live readings that prompted
// this were 3221,3220 and 3222,3222, so a few tiles of slack covers the pair.
const (
	lumbridgeSpawnX    = 3222
	lumbridgeSpawnZ    = 3218
	lumbridgeSpawnNear = 8
)

// deckMaxTiles is how far a level-0 to level-1 hop may carry an account and still read as
// boarding rather than as something nobody asked for. The live ship crossings were about 95 and
// 105 tiles; the live fail-teleport to an upper floor was about 6,000.
const deckMaxTiles = 150

// nearLumbridgeSpawn reports whether a landing is on the Lumbridge spawn tile.
func nearLumbridgeSpawn(x, z, level int) bool {
	return level == 0 && chebyshev(x, z, lumbridgeSpawnX, lumbridgeSpawnZ) <= lumbridgeSpawnNear
}

// walkAllowance is how many tiles an ordinary walk could have covered in gap.
func walkAllowance(gap time.Duration) int {
	polls := int(gap / pollInterval)
	if polls < 1 {
		polls = 1
	}
	return polls*maxTilesPerPoll + displacementSlack
}

// ClassifyDisplacement decides whether a pair of position readings is a real displacement, and
// what kind. Pure, so the decision is unit-tested rather than only ever exercised by a live run
// that has to be lucky enough for a random event to fire.
//
// A level change always counts, because a stair or ladder is worth one line too and because a
// fail-teleport into a building's upper floor (the live Jolly Boar case) moves both. Otherwise
// the jump has to be further than a walk could have carried the account in the elapsed time.
func ClassifyDisplacement(fromX, fromZ, fromLevel, toX, toZ, toLevel int, gap time.Duration) (Displacement, bool) {
	dist := chebyshev(fromX, fromZ, toX, toZ)
	levelChanged := fromLevel != toLevel
	if !levelChanged && dist <= walkAllowance(gap) {
		return Displacement{}, false
	}

	d := Displacement{
		FromX: fromX, FromZ: fromZ, FromLevel: fromLevel,
		ToX: toX, ToZ: toZ, ToLevel: toLevel,
		Tiles: dist, Gap: gap,
	}
	// An ordinary stair or ladder is decided FIRST, and the order is load-bearing rather than
	// tidy. KnownFailTeleportPocket matches by MAPSQUARE, not by tile, and roughly thirty of the
	// enum's destinations are ordinary places a bot walks through every run — Lumbridge Castle
	// (mapsquare 50,50), Varrock's own fountain/museum/east-bank square (50,53), Edgeville
	// (48,54), Barbarian Village (48,53), Falador Castle (46,52). Asking the table first
	// therefore labelled every staircase inside any of them a fail-teleport; a unit test caught
	// it by picking 3200,3200 level 1 as an innocent staircase and being told it was the
	// Lumbridge Castle tower. A real fail-teleport never sets the account down two tiles from
	// where it was standing, so "the level moved and the tile hardly did" settles the case before
	// the mapsquare lookup ever runs.
	// A dungeon entrance and its own exit are one fixture travelled in two directions, and this
	// build stores the underground counterpart of a surface mapsquare at mapsquareZ+100. Deciding
	// that PAIR before the pocket lookup is what stops the climb back out reading as something
	// alarming. Going down, 3096,3468 -> 3096,9868, the destination mapsquare is 48,154 and
	// LooksLikeUndergroundMapsquare answers yes; coming back up it is 48,54, which is the enum's
	// own catalogued "Edgeville" entry, so the lookup claimed a fail-teleport on every single
	// climb out. The third multi-band arm printed exactly that ten times in one window, and it is
	// the same mapsquare-granularity trap as the staircase case below, reached by the one route
	// the level-change guard cannot cover: the trapdoor's p_telejump keeps the player on level 0.
	mxFrom, mzFrom := fromX>>6, fromZ>>6
	mxTo, mzTo := toX>>6, toZ>>6
	counterpart := mxFrom == mxTo && (mzFrom-mzTo == undergroundMapsquareZ || mzTo-mzFrom == undergroundMapsquareZ)

	if levelChanged && dist <= walkAllowance(gap) {
		d.Kind = DisplacementLevelChange
	} else if site, ok := catalogueSiteAt(toX, toZ, toLevel); ok && !site.Boarding {
		// Before the underground and pocket rules, which matched these and explained them away.
		d.Kind, d.Name = DisplacementStrandSite, site.Name
	} else if name, ok := KnownFixture(fromX, fromZ, toX, toZ); ok {
		d.Kind, d.Name = DisplacementFixture, name
	} else if counterpart {
		d.Kind = DisplacementUnderground
	} else if route, isShip := KnownShipArrival(toX, toZ, toLevel); isShip {
		// Checked BEFORE the pocket lookup, and it has to be: two of content's own ship
		// arrivals land inside a catalogued fail-teleport MAPSQUARE, and the pocket lookup
		// matches by mapsquare rather than by tile. Karamja -> Port Sarim arrives at
		// 1_47_50_24_17 = 3032,3217 level 1, and mapsquare 47,50 level 1 is exactly the
		// enum's "Port Sarim, above Brian's Battleaxe Bazaar" — so an ordinary paid crossing
		// was being reported as the loud "an event was ignored long enough to move the
		// account" line. Reported three times off the live fleet.
		//
		// This is why the misreading looked arbitrary: Brimhaven -> Ardougne arrives at
		// 1_41_51_59_4 = 2683,3268 level 1, and mapsquare 41,51 is NOT in the enum, so that
		// crossing fell through to the deck branch and read correctly all along.
		//
		// Matching by exact tile, not by mapsquare, is what keeps this from swallowing a real
		// fail-teleport: the enum drops those passengers at Brian's Battleaxe Bazaar, a
		// different tile in the same square, which still reads as a fail-teleport.
		d.Kind, d.Name = DisplacementDeck, route
	} else if name, isPocket := KnownFailTeleportPocket(toX, toZ, toLevel); isPocket {
		d.Kind, d.Name = DisplacementFailTeleport, name
	} else if nearLumbridgeSpawn(toX, toZ, toLevel) {
		// Reported from the live fleet by the root session, 2026-09-20: of seven "UNEXPLAINED"
		// lines in the first fifteen minutes, only ONE was a real fail-teleport. Two of the false
		// ones ended here — a death respawn (3120,3254 -> 3221,3220, herring died at 01:12:42) and
		// leaving Tutorial Island (3095,3106 -> 3222,3222). Both land on the same tile, and a
		// position reading cannot say which happened, so the label names the place.
		d.Kind = DisplacementSpawnPoint
	} else if fromLevel == 0 && toLevel == 1 && dist <= deckMaxTiles {
		// The other two false ones were ship crossings: 3026,3218,0 -> 2956,3143,1 (the Port Sarim
		// ship) and 2772,3232,0 -> 2683,3268,1 (Brimhaven to Ardougne). A deck is a level-1 tile a
		// short way from the dock.
		//
		// The honest cost of this branch, stated rather than buried: an UNCATALOGUED fail-teleport
		// that happens to land on a level-1 tile within deckMaxTiles now reads as boarding. Every
		// catalogued one still wins, because the pocket lookup above runs first, and the live Jolly
		// Boar case (3114,9835 -> 3278,3500 level 1) is about 6,000 tiles away, far outside this
		// cap. It trades a rare miss for silencing a noise source that was firing six times in
		// seven on the real fleet.
		d.Kind = DisplacementDeck
	} else if LooksLikeUndergroundMapsquare(toZ >> 6) {
		d.Kind = DisplacementUnderground
	} else {
		d.Kind = DisplacementUnexplained
	}
	return d, true
}

// WithEventPresent relabels an UNEXPLAINED jump as an event one, and only an unexplained one.
//
// Live-found 2026-09-19 on the first arm of the reachability fix: the account went 3292,3167 ->
// 2891,4597, 1430 tiles, and the watcher called it UNEXPLAINED — with Guardian's own next line
// reading "random event: maze — handling". A maze teleport is not a mystery, and printing one as
// unexplained in a long log is the same class of unjustified reading this watcher's own first
// version was fixed for.
//
// A catalogued fail-teleport, an underground counterpart and an ordinary level change all keep
// their label: a passing event must not be allowed to explain away a jump that already has a
// better explanation.
func WithEventPresent(d Displacement, name string) Displacement {
	if d.Kind != DisplacementUnexplained {
		return d
	}
	d.Kind, d.Name = DisplacementEvent, name
	return d
}

// watchDisplacement samples the account's tile once per poll and logs anything that moved it
// further than a walk explains. Called from Start's own ticker, ahead of kick, so it samples on
// every tick regardless of whether a handler is in flight — an event Guardian is busy with can
// relocate the account just as easily as one it never saw.
func (g *Guardian) watchDisplacement() {
	x, z, level, ok := g.session.State().Tile()
	if !ok {
		return
	}
	now := time.Now()

	g.mu.Lock()
	prevOK := g.lastTileOK
	px, pz, plevel, pat := g.lastTileX, g.lastTileZ, g.lastTileLevel, g.lastTileAt
	g.lastTileX, g.lastTileZ, g.lastTileLevel, g.lastTileAt, g.lastTileOK = x, z, level, now, true
	g.mu.Unlock()

	if !prevOK {
		return
	}
	// A view that is silent or catching up on a backlog (gamestate.State.ViewLag) moves the tile ten or thirty tiles a poll while
	// it is read, which is not a displacement: 17 Oct 12:23:09-17, 71 of those on 24 accounts in eight seconds, the kind of
	// jump the mass learner below turns into a strand site once twelve accounts share a mapsquare. The reading is recorded above,
	// so the next comparison is against where the account really is.
	if g.session.State().ViewLagging() {
		return
	}
	// A ::tele from a harness's own setup is a deliberate move with a known cause, so reporting
	// it would put a false line in the log of every staged run. Measured the first time this ran
	// live: it announced two setup teleports, and called one of them a catalogued fail-teleport
	// destination because the cows rung happens to bank in that very mapsquare. The reading above
	// is still recorded either way, so the next comparison is against where the account actually
	// is; only the line is suppressed. Every mode turns cheats off before the part under test, so
	// this silences staging and nothing else.
	if actions.CheatEnabled {
		return
	}
	if d, moved := ClassifyDisplacement(px, pz, plevel, x, z, level, now.Sub(pat)); moved {
		// A jump the account announced (ExpectTeleport for a cast, ExpectHop for a spirit tree or a portal, ExpectLandings
		// for the runecraft and essence-mine crossings) is a fixture whatever the landing's mapsquare reads as, and it
		// never reaches the mass learner below: twelve Ardougne casts inside 90 seconds would otherwise teach the Guardian
		// a twelve-hour strand site at the East Ardougne market.
		if name, ok := takeExpectedTeleport(g.session, x, z, now); ok {
			d.Kind, d.Name = DisplacementFixture, name
		}
		// An event standing at the far end of the jump is the plainest available explanation,
		// and it is an observation rather than an inference: the same poll can see it.
		if d.Kind == DisplacementUnexplained {
			// Counted BEFORE the event relabel: of the 237 accounts the server dropped on Rogues'
			// Castle at 04:35:01-33, most were still unexplained when polled and a few already had the
			// Swarm in view. A Mime or maze arrival is one an event makes on purpose and arrives in
			// bursts (21 in one minute at 02:45), so those never count.
			if !InTrapEventSquare(x, z) {
				if n, learned := mass.note(now, g.session, x, z, level); learned {
					g.log(fmt.Sprintf("MASS DISPLACEMENT: %d accounts have landed in mapsquare %d,%d level %d within %s and none of it is a known fixture or site. "+
						"This is a server-side summon like the King Black Dragon's lair (2026-10-01 21:07) and Rogues' Castle (2026-10-02 04:35): "+
						"learned as a strand site, so a teleport spell is tried from it; add its exit to strandsites.go",
						n, x>>6, z>>6, level, massWindow))
				}
			}
			if site, ok := learnedSiteAt(x, z, level, now); ok {
				d.Kind, d.Name = DisplacementStrandSite, site.Name
			} else if ev := Detect(g.session.State()); ev != nil {
				d = WithEventPresent(d, ev.Name)
			}
		}
		g.log(d.String())
	}
}
