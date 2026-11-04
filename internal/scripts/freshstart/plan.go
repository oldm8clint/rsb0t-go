// Package freshstart decides which leg of a brand-new account's first day it is on: the
// appearance screen, Tutorial Island, the Lumbridge store, shearing to crafting 10, pickpocketing
// the boat fare, the pink skirt, the boat legs, or the destination loop. It is ported from
// rsb0t-scripts' freshStartPlan.ts (plan.go) and the decisions inside newAccount.ts
// (newaccount.go), and shared by the crafter and the fisher.
//
// Everything here is pure: plain snapshots in, decisions out, no client, actions, fleet or bot
// imports and no I/O. Items, NPCs and locs are named by strings for the integration to resolve.
package freshstart

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// DesignModal is the player-design (appearance) interface a brand-new account logs in behind.
const DesignModal = 3559

// BrimhavenCustomsMaxX splits the two Karamja customs officers: content's customs_officer.rs2 sails
// an officer standing west of this x to Ardougne and any other (Musa Point) back to Port Sarim.
const BrimhavenCustomsMaxX = 2815

// Phase is one leg of the first day.
type Phase string

// The phases, in the order an account normally meets them.
const (
	PhaseAccept   Phase = "accept"
	PhaseTutorial Phase = "tutorial"
	PhaseBright   Phase = "bright"
	PhaseDrop     Phase = "drop"
	PhaseStore    Phase = "store"
	PhaseWool     Phase = "wool"
	PhaseFund     Phase = "fund"
	PhaseSkirt    Phase = "skirt"
	PhaseTravel   Phase = "travel"
	// PhaseArrived is the destination's own loop: Seers flax for the crafter. The TS name was "seers".
	PhaseArrived Phase = "arrived"
)

// Phases lists every phase.
func Phases() []Phase {
	return []Phase{PhaseAccept, PhaseTutorial, PhaseBright, PhaseDrop, PhaseStore, PhaseWool, PhaseFund, PhaseSkirt, PhaseTravel, PhaseArrived}
}

// Box is a ground-floor rectangle of the map.
type Box struct {
	MinX, MaxX, MinZ, MaxZ int
}

// Contains reports whether t is on the ground floor inside the box.
func (b Box) Contains(t tile.Tile) bool {
	return t.Level == 0 && t.X >= b.MinX && t.X <= b.MaxX && t.Z >= b.MinZ && t.Z <= b.MaxZ
}

// Places on the road, from freshStartPlan.ts.
var (
	// TutorialIsland is the union of the rectangle this port originally carried
	// (3050-3160 x 3050-3130, copied from the TS client) and the zone content's own
	// ~in_tutorial_island actually tests (x 3053-3156, z 3056-3136 — see
	// content/scripts/tutorial/scripts/util.rs2). The two disagree at the edges, and the
	// union is taken deliberately: this box decides whether a script believes it is still
	// in the tutorial, so a tile that is recognised today must not stop being recognised,
	// and content's own extra six rows of z must start being.
	TutorialIsland = Box{MinX: 3050, MaxX: 3160, MinZ: 3050, MaxZ: 3136}
	// TutorialUnderground is the tutorial's own below-ground area — the mining and combat
	// section — which content's ~in_tutorial_island names as a second zone
	// (x 3072-3118, z 9492-9535) and which the original box missed entirely. An account
	// standing here is as blinded as one standing on the beach: login.rs2 runs
	// @start_tutorial, whose first act strips the sidebar tabs, for either. Leaving it out
	// let a script conclude it was off the island and start reading a pack it cannot see.
	TutorialUnderground = Box{MinX: 3072, MaxX: 3118, MinZ: 9492, MaxZ: 9535}
	// Karamja is Musa Point and Brimhaven, the stretch between the two ships.
	Karamja         = Box{MinX: 2740, MaxX: 2980, MinZ: 3130, MaxZ: 3260}
	LumbridgeCentre = tile.Tile{X: 3222, Z: 3218}
	// StoreStand is the ground-floor tile beside the Lumbridge general store counter.
	StoreStand = tile.Tile{X: 3209, Z: 3247}
	// ShearsSpawn is Fred's farm, where a free pair of shears respawns.
	ShearsSpawn  = tile.Tile{X: 3152, Z: 3306}
	LumbridgePen = tile.Tile{X: 3197, Z: 3266}
	// LumbridgeWheel is upstairs in the castle; south is its only open side.
	LumbridgeWheel = tile.Tile{X: 3209, Z: 3213, Level: 1}
	// BrimhavenPier is the ground-floor pier tile the first boat leg walks to.
	BrimhavenPier = tile.Tile{X: 2772, Z: 3234}
	// PortSarimDock is where the Karamja-bound sailors stand (Seaman Lorris, Seaman
	// Thresnor, Captain Tobias — content/scripts/areas/area_port_sarim/scripts/
	// sailors.rs2's category=sailor NPCs). internal/pathfind has no transport-graph edge
	// for the Port Sarim -> Karamja ship, so a plain WalkNear straight to BrimhavenPier
	// from anywhere east of the water (e.g. Lumbridge) fails instantly with no route at
	// all — the account needs this as an explicit intermediate waypoint before boarding,
	// not a target pathfind can reach on its own. See freshstartrunner.SailLeg/BoardShip.
	//
	// Corrected 2026-09-15, twice. First correction (from the original (3025,3220),
	// which was actually ~20 tiles north of the real dock and sent accounts through a
	// Falador building) moved this to (3028,3202) — verified against real dockside
	// terrain via cmd/doordiag (Row boat/Anchor/Crane/Barrel/Crate/Full fishing
	// net/Pile of rope), but STILL wrong: a plain direct FindPath from Lumbridge to
	// (3028,3202) kept wandering north through Rimmington/Falador-adjacent buildings
	// instead of the real road through Draynor, and the account owner confirmed live
	// ("we are ending up very north east of Draynor") that this coordinate itself
	// wasn't the real dock either. Second, authoritative correction: the account
	// owner supplied the real dock tile directly (3027,3218) — this is where the
	// Karamja-bound sailors actually stand. See Draynor below: a direct one-leg walk
	// from Lumbridge to even the correct dock tile still isn't reliable (the
	// unconstrained route wanders), so SailLeg now stages through Draynor first.
	PortSarimDock = tile.Tile{X: 3027, Z: 3218}
	// Draynor is a reliable, account-owner-supplied waypoint (2026-09-15) on the real
	// road between Lumbridge and Port Sarim — "Port Sarim is east of Draynor." Used as
	// an explicit intermediate stop for the pier leg's walk (see SailLeg) instead of
	// one long, unconstrained FindPath straight to PortSarimDock, which kept wandering
	// north into Falador/Rimmington-adjacent buildings and stranding accounts there.
	Draynor = tile.Tile{X: 3093, Z: 3243}
	// ArdougneArrival is the exact tile p_telejump lands an account on after the Brimhaven ->
	// Ardougne ship (content/scripts/interface_boat/scripts/sail.rs2's set_sail, decoded from
	// customs_officer.rs2's coord literal 1_41_51_59_4). Confirmed live via ::tele + NPC scan
	// (2026-09-15, Captain Barnaby and a cluster of chickens right beside it — genuine East
	// Ardougne dock) AND via cmd/doordiag against the real collision pack: level 1 (not 0 —
	// an earlier version of this fix wrongly assumed 0 and had to be reverted), a real
	// 24-tile walkable area, not a dead end. The only way down to the ground-level dock is
	// ArdougneLadder, right next to it. See freshstartrunner.SailLeg's own doc comment for
	// why that fixture needs hardcoded, direction-locked handling rather than the generic
	// stairNames search every other level change goes through.
	ArdougneArrival = tile.Tile{X: 2683, Z: 3268, Level: 1}
	// ArdougneLadder is the specific "Ship's ladder" (loc id 273, op "Climb-down") beside
	// ArdougneArrival — confirmed live via cmd/locops. A DIFFERENT loc id (245, "Climb-up")
	// shares the exact same display name on real ships elsewhere (e.g. near Karamja), which
	// is why this is addressed by id, not by name — see SailLeg's own doc comment.
	ArdougneLadder = tile.Tile{X: 2682, Z: 3267, Level: 1}
	// ArdougneLadderApproach is the ONLY tile ArdougneLadder accepts an interaction from —
	// confirmed live via cmd/guidecrash -testloc, trying all 4 orthogonal neighbours: north
	// (2682,3268), south (2682,3266) and east (2683,3267) all got the server's own real "I
	// can't reach that!" rejection; only west succeeded, dropping to level 0 within 0.5s.
	// internal/pathfind's generic walkAdjacent (any tile within Chebyshev 1) cannot be
	// trusted to land on this specific side, so SailLeg walks here explicitly instead.
	ArdougneLadderApproach = tile.Tile{X: 2681, Z: 3267, Level: 1}
	// ArdougneLadderID/ArdougneLadderOp are ArdougneLadder's exact loc id and op, confirmed
	// live via cmd/locops (id=273, ops=["Climb-down"]).
	ArdougneLadderID = 273
	ArdougneLadderOp = "Climb-down"
	// ArdougneGangplank is the REAL way down from ArdougneArrival — a "Gangplank" (loc id
	// 2086, op "Cross"), not ArdougneLadder above. Account-owner-supplied ground truth
	// (2026-09-15): "need to cross gangplank, then you'll be at 2683,3271,0, ready to
	// walk to catherby" — confirmed live via cmd/guidecrash -testloc from ArdougneArrival
	// itself (2683,3268,1): OpLoc(Cross) on this exact loc landed at 2683,3271,0 on the
	// very next tick, an exact match. ArdougneLadder may be a real fixture for some other
	// purpose (or the same reverse-direction-decoy class already seen elsewhere this
	// session, e.g. the Musa Point Customs officer) but is not what a fresh account
	// boarding from Brimhaven actually needs — SailLeg now crosses this instead.
	ArdougneGangplank   = tile.Tile{X: 2683, Z: 3269, Level: 1}
	ArdougneGangplankID = 2086
	ArdougneGangplankOp = "Cross"
	// BrimhavenShipDeck is the tile p_telejump lands an account on after Captain Barnaby's
	// Ardougne -> Brimhaven ship, and the fourth ship deck of exactly the same shape as the
	// three below: a level-1 pocket the collision bake treats as isolated, whose only way off
	// is one hardcoded gangplank.
	//
	// It is the only one of the four that had no handler until 2026-09-22, and the live cost was
	// plain: over one afternoon 707 accounts rode that ship in (`Ardougne->Brimhaven ship:
	// arrived at 2775,3234,1`) and NOT ONE took the Brimhaven->Ardougne ship back, because the
	// return leg begins with getting off the deck. A read of the running panel while the fleet
	// was live had nine ScarabSwarm accounts standing on exactly this tile, each logging
	// "boat leg: Brimhaven ship -> Ardougne -> destination" on a loop.
	//
	// Both tiles and the id/op come from the webwalk transport graph's own edge (kind
	// "gangplank", locId 2088 at 2774,3234, action "Cross", 2775,3234,1 -> 2772,3234,0) and are
	// live-proven by the fleet using that edge successfully under the walker — e.g. acct83,
	// 2026-09-22 15:54:42-45: "hop [gangplank] Cross \"Gangplank\" loc=2088@2774,3234
	// 2775,3234,1 -> 2772,3234,0" then "Cross Gangplank at 2774,3234 ok". What was missing was
	// SailLeg knowing about it when it is driving the boats itself rather than walking.
	BrimhavenShipDeck    = tile.Tile{X: 2775, Z: 3234, Level: 1}
	BrimhavenGangplank   = tile.Tile{X: 2774, Z: 3234, Level: 1}
	BrimhavenGangplankID = 2088
	BrimhavenGangplankOp = "Cross"
	// KaramjaShipDeck is the rough centre of a small ship deck near Musa Point (bbox
	// 2951-2963,3139-3144, level 1) that stranded many accounts crossing Karamja toward
	// Brimhaven — confirmed live (2026-09-15) via cmd/doordiag: a real, walkable 48-tile
	// pocket with no baked path to the mainland at all, matching the same class of gap as
	// ArdougneArrival (a ship structure the offline collision bake treats as isolated).
	KaramjaShipDeck = tile.Tile{X: 2957, Z: 3141, Level: 1}
	// KaramjaGangplank is the "Gangplank" (loc id 2082, op "Cross") that leads off the deck
	// onto Karamja's real mainland — confirmed live via cmd/guidecrash -testloc: id/op
	// confirmed, and (unlike ArdougneLadder) no paired reverse-direction id nearby, so this
	// one is safe from the oscillation trap. It DOES still have a directional restriction
	// though, found live the same way: all 4 orthogonal neighbours worked, but all 4
	// diagonal neighbours got the server's own "I can't reach that!" — walkAdjacent's plain
	// "within Chebyshev 1" can land on a diagonal tile depending on the account's approach
	// direction, which is exactly what happened live (devlab22 timed out repeatedly from a
	// diagonal tile). KaramjaGangplankApproach pins one confirmed-orthogonal tile instead.
	KaramjaGangplank         = tile.Tile{X: 2956, Z: 3144, Level: 1}
	KaramjaGangplankApproach = tile.Tile{X: 2956, Z: 3143, Level: 1}
	KaramjaGangplankID       = 2082
	KaramjaGangplankOp       = "Cross"
	// KaramjaMainlandGate is a real, separate barrier from KaramjaShipDeck above: even
	// once off the ship deck, the Musa Point area and the Brimhaven pier are two entirely
	// separate flood-fill components in the collision pack (confirmed live 2026-09-15 via
	// cmd/doordiag: 4325 tiles reachable from Musa Point, bbox up to x=2816,z=3206; 21951
	// tiles reachable from BrimhavenPier, bbox from x=2704; FindPath between them fails
	// outright as unreachable both ways, not merely blocked by one closed-door tile).
	// doordiag found the actual join: a closed gate pair (ids 1596/1597, both op "Open")
	// at (2816,3182) and (2816,3183) — both gate tiles themselves read inComp=true from
	// the Musa Point side, only their western neighbour is on the Brimhaven side. This
	// gate is ~140 tiles from where the first ship drops accounts off, far outside
	// walkNearCore's own nearby-door search radius (which is why the mainland's generic
	// door recovery, proven working for the Draynor/Port Sarim doors this same run, never
	// even tried this one) — SailLeg needs this as an explicit staged intermediate target,
	// the same shape as Draynor before PortSarimDock, to get close enough for that generic
	// recovery to take over from here.
	KaramjaMainlandGate = tile.Tile{X: 2816, Z: 3182}
	// PortSarimLadder is a third, same-shaped stranding near Port Sarim (2954,3390, level
	// 2->0): a plain "Ladder" (id 1749, op "Climb-down") that already matches
	// freshstartrunner's generic stairNames/stairOps search by name — unlike the two cases
	// above, this one WAS already being found by tryChangeLevel's own search — but its plain
	// walkAdjacent (radius 1, any of 8 neighbours) can't be trusted to land on the one side
	// this fixture accepts, the same class of bug as ArdougneLadder. Confirmed live via
	// cmd/guidecrash -testloc against all 4 orthogonal neighbours: north, west and south all
	// got the server's own "I can't reach that!"; only EAST worked, dropping to level 1
	// within 0.5s.
	PortSarimLadder         = tile.Tile{X: 2954, Z: 3390, Level: 2}
	PortSarimLadderApproach = tile.Tile{X: 2955, Z: 3390, Level: 2}
	PortSarimLadderID       = 1749
	PortSarimLadderOp       = "Climb-down"

	// PortSarimLadderLower is the SECOND descent, and the real reason accounts kept
	// stranding here even though PortSarimLadderApproach above was already correct.
	// Climbing 1749 does not reach the ground: it lands on level 1, inside a sealed 7-tile
	// pocket (2953-2955,3388-3390) whose only way down is a different ladder, id 1746
	// ("Ladder", "Climb-down") one tile north at 2954,3389. Level 0 below it is the
	// 216662-tile mainland, so this second climb is the whole difference between stranded
	// and free. Confirmed live 2026-09-18 with cmd/guidecrash -testloc from all 8
	// neighbours of each ladder, not just the 4 orthogonal ones:
	//
	//	1749 from 2955,3390,2 (east) -> 2955,3390,1. The other SEVEN, diagonals included,
	//	    all got the server's own "I can't reach that!".
	//	1746 from all four ORTHOGONAL neighbours (2954,3388 / 2953,3389 / 2955,3389 /
	//	    2954,3390) -> level 0. All four DIAGONALS were refused.
	//
	// Orthogonal-only is the same restriction KaramjaGangplank has, so this is pinned to one
	// confirmed tile and walked to with radius 0 rather than left to walkAdjacent, which
	// counts any of 8 neighbours and can settle on a diagonal. 2955,3389 is chosen because it
	// is orthogonal to 1746 and directly beside 2955,3390,1, where the first climb lands.
	//
	// Why this must be addressed by id and not through the generic stairNames search: at
	// level 1 there are TWO locs displaying the name "Ladder" one tile apart — 1746
	// ("Climb-down", at 2954,3389) and 1750 ("Climb-up", at 2954,3390). stairOpsDown keeps
	// "Climb-up" as a last-resort fallback, so a generic search that cannot reach 1746 can
	// climb 1750 straight back up to level 2 and oscillate. That is the reverse-climb risk,
	// and it is why stairNames itself is deliberately left alone.
	PortSarimLadderLower         = tile.Tile{X: 2954, Z: 3389, Level: 1}
	PortSarimLadderLowerApproach = tile.Tile{X: 2955, Z: 3389, Level: 1}
	PortSarimLadderLowerID       = 1746
	PortSarimLadderLowerOp       = "Climb-down"
	// ThessaliaStand is beside the counter of Thessalia's Fine Clothes in Varrock.
	ThessaliaStand = tile.Tile{X: 3204, Z: 3417}
	// PickpocketSpot is the Lumbridge castle courtyard, where Men and Women wander.
	PickpocketSpot = tile.Tile{X: 3222, Z: 3218}
)

// Names and numbers from freshStartPlan.ts, checked against content (branch 289).
const (
	// ArdougneSideMaxX: west of Brimhaven is the Ardougne side of the second ship.
	ArdougneSideMaxX  = 2740
	LumbridgeRadius   = 80
	DestinationRadius = 80

	Thessalia        = "Thessalia"
	PinkSkirt        = "Pink skirt"
	DefaultSkirtCost = 3

	PickpocketLeash         = 20
	PickpocketOp            = "Pickpocket"
	DefaultPickpocketTarget = "Man"
	// StunTicks: a failed pickpocket stuns for about five seconds.
	StunTicks = 8
	// RestBelowHP and ResumeAtHP are the health shares resting starts and ends at.
	RestBelowHP = 0.4
	ResumeAtHP  = 0.9
	// RestMax is the longest one rest waits for health to come back. Health regenerates a point a
	// minute ([timer,health_regen] settimer 100 in login.rs2), so 8 minutes is the whole of a
	// starting account's bar and more; a rest that outlasts it is waiting for something that is not
	// coming (see ShouldRestBounded).
	RestMax = 8 * time.Minute
	// RestCooldown is how long after a rest was given up the account works without resting again,
	// so a regeneration that is not coming does not turn the bound into one rest after another.
	RestCooldown = 15 * time.Minute

	Shears = "Shears"
	Coins  = "Coins"
	Wool   = "Wool"
	Ball   = "Ball of wool"
	// DefaultFare is two ship fares of 30 coins.
	DefaultFare   = 60
	DefaultDropAt = 27

	DefaultGuideNPC   = "RuneScape Guide"
	DefaultSkipOption = "Yes please"
	TalkOp            = "Talk-to"
)

// ShopKeepers are the two NPCs the Lumbridge general store answers to, in the order tried.
func ShopKeepers() []string {
	return []string{"Shop keeper", "Shop assistant"}
}

// InTutorialIsland reports whether t is anywhere in the tutorial: the island itself, or its
// below-ground mining and combat area. Both count, because the server treats them the same —
// login.rs2 runs @start_tutorial for either, and that strips the sidebar tabs.
func InTutorialIsland(t tile.Tile) bool {
	return TutorialIsland.Contains(t) || TutorialUnderground.Contains(t)
}

// InLumbridge reports whether t is within reach of Lumbridge: the castle, the pen and the store.
func InLumbridge(t tile.Tile) bool {
	return tile.Chebyshev(t, LumbridgeCentre) <= LumbridgeRadius
}

// OnKaramjaSide reports whether t is between the two ships, on any floor.
// The broad route rectangle also overlaps mainland Rimmington north of the
// eastern coast. That overlap must not trigger island-only banana fare recovery
// or skip the Port Sarim boat leg after a random-event displacement.
func OnKaramjaSide(t tile.Tile) bool {
	t.Level = 0
	return Karamja.Contains(t) && !(t.X >= 2880 && t.Z >= 3200)
}

// Leg is which boat leg is next.
type Leg string

// The two boat legs.
const (
	LegPier        Leg = "pier"
	LegDestination Leg = "destination"
)

// TravelLeg says which boat leg is next. Anyone already across the first ship (on Karamja) or
// across the second (the Ardougne side) heads for the destination; everyone else heads for the
// Brimhaven pier, which the walker can only reach through the Port Sarim ship.
func TravelLeg(t tile.Tile) Leg {
	if OnKaramjaSide(t) || t.X < ArdougneSideMaxX {
		return LegDestination
	}
	return LegPier
}

// Road is what differs between the scripts that share the first day.
type Road struct {
	// Destination is the centre of where the road ends: the Seers bank for the crafter.
	Destination tile.Tile
	// Radius is how close to Destination counts as arrived.
	Radius int
	// WoolUntil is the crafting level the shear-and-spin loop runs to before the pockets and the
	// boats. 0 skips the wool loop.
	WoolUntil int
}

// Arrived reports whether t is at the road's destination.
func (r Road) Arrived(t tile.Tile) bool {
	return tile.Chebyshev(t, r.Destination) <= r.Radius
}

// Snapshot is the live state DecidePhase reads.
type Snapshot struct {
	MainModal int
	// Tile is nil while the position is not known yet.
	Tile      *tile.Tile
	Crafting  int
	Coins     int
	HasShears bool
	Wool      int
	Balls     int
	// Sellable counts pack slots holding something the store might take: not coins, shears,
	// wool or balls.
	Sellable int
	PackFull bool
	Fare     int
	// DropAt is the ball-of-wool count that justifies a drop pass.
	DropAt    int
	SkirtWorn bool
	SkirtHeld bool
	SkirtCost int
	// BrightPending is true while the screen brightness still wants turning up to very bright.
	BrightPending bool
}

// FareRemaining is the fare still ahead: the full amount from the mainland, one ship's worth on
// Karamja, and nothing on the Ardougne side.
func FareRemaining(t *tile.Tile, fare int) int {
	switch {
	case t == nil:
		return fare
	case OnKaramjaSide(*t):
		return (fare + 1) / 2
	case t.X < ArdougneSideMaxX:
		return 0
	}
	return fare
}

// SkirtStillToBuy reports whether the skirt is still to buy. It is only ever bought on the
// mainland, before the boats.
func SkirtStillToBuy(t *tile.Tile, worn, held bool) bool {
	if worn || held {
		return false
	}
	return t == nil || TravelLeg(*t) == LegPier
}

// FundTarget is the coins to raise where the account stands: the fares still ahead, plus the skirt
// while it is still to buy.
func FundTarget(s Snapshot) int {
	target := FareRemaining(s.Tile, s.Fare)
	if SkirtStillToBuy(s.Tile, s.SkirtWorn, s.SkirtHeld) && s.SkirtCost > 0 {
		target += s.SkirtCost
	}
	return target
}

// NeedsDropPass reports whether to drop balls of wool, which the store pays nothing for.
func NeedsDropPass(balls, dropAt int, packFull bool, wool int) bool {
	if dropAt < 1 {
		dropAt = 1
	}
	return balls >= dropAt || (packFull && wool == 0 && balls > 0)
}

// NeedsStoreTrip reports whether to visit the store: for missing shears, or a pack full of junk
// it might take. Never for wool.
func NeedsStoreTrip(hasShears, packFull bool, wool, balls, sellable int) bool {
	return !hasShears || (packFull && wool == 0 && balls == 0 && sellable > 0)
}

// ShouldRest reports whether to sit out pickpocketing for health, with the default marks.
func ShouldRest(hpFraction float64, resting bool) bool {
	return ShouldRestWith(hpFraction, resting, RestBelowHP, ResumeAtHP)
}

// ShouldRestWith is ShouldRest with explicit marks. Rest starts at low and ends at resume, so a
// single regenerated point does not restart the beatings.
func ShouldRestWith(hpFraction float64, resting bool, low, resume float64) bool {
	if resting {
		return hpFraction < resume
	}
	return hpFraction <= low
}

// ShouldRestBounded is ShouldRestWith with a bound on how long one rest may last: restFor is how
// long the current rest has gone on (ignored when resting is false) and max the bound. A rest that
// has lasted max returns rest false and gaveUp true; the caller says so, with the hitpoints it read,
// and holds off resting for RestCooldown.
//
// Live 5 Oct 2026, Howdy Folk (Hitpoints 10): "health at 40% - resting until it regenerates" at
// 14:53 and nothing for 1h14m. The Genie random event's last page ("Your wish has been granted!
// Click here to continue") was left open on the chat, and a NORMAL player timer, which
// health_regen is, runs only while the player can be accessed (Player.processTimers: canAccess()
// is false while a main or chat modal is open), so not one hitpoint regenerated. A bot restart
// closed it and the account pickpocketed at once; with no bound, the loop would have waited for
// ever.
func ShouldRestBounded(hpFraction float64, resting bool, restFor time.Duration, low, resume float64, max time.Duration) (rest, gaveUp bool) {
	if !resting {
		return ShouldRestWith(hpFraction, false, low, resume), false
	}
	if hpFraction >= resume {
		return false, false
	}
	if max > 0 && restFor >= max {
		return false, true
	}
	return true, false
}

// CoinsShort reports whether coins are short of target.
func CoinsShort(coins, target int) bool {
	return coins < target
}

// DecidePhase picks the leg from live state alone, so a relog anywhere resumes in the right place.
func DecidePhase(s Snapshot, r Road) Phase {
	if s.MainModal == DesignModal {
		return PhaseAccept
	}
	if s.Tile != nil && InTutorialIsland(*s.Tile) {
		return PhaseTutorial
	}
	if s.Crafting < r.WoolUntil {
		if NeedsDropPass(s.Balls, s.DropAt, s.PackFull, s.Wool) {
			return PhaseDrop
		}
		if NeedsStoreTrip(s.HasShears, s.PackFull, s.Wool, s.Balls, s.Sellable) {
			// A new account turns the brightness up before its first trip to the store.
			if s.BrightPending {
				return PhaseBright
			}
			return PhaseStore
		}
		return PhaseWool
	}
	if s.Tile != nil && r.Arrived(*s.Tile) {
		return PhaseArrived
	}
	// Wool and balls are worthless past the wool loop; clear them before the pockets.
	if s.Balls > 0 || s.Wool > 0 {
		return PhaseDrop
	}
	if CoinsShort(s.Coins, FundTarget(s)) {
		return PhaseFund
	}
	if !s.SkirtWorn && (s.SkirtHeld || SkirtStillToBuy(s.Tile, s.SkirtWorn, s.SkirtHeld)) {
		return PhaseSkirt
	}
	return PhaseTravel
}

// PackItem is one pack slot, by name. An empty name is an empty slot.
type PackItem struct {
	Name  string
	Count int
}

// SellLine is one item to offer the store.
type SellLine struct {
	Name  string
	Count int
}

// SellPlan lists what to offer the store: every pack name that is not kept, with its total count,
// in the order first seen. Keep names match case-insensitively.
func SellPlan(pack []PackItem, keep []string) []SellLine {
	kept := make(map[string]bool, len(keep))
	for _, k := range keep {
		kept[strings.ToLower(k)] = true
	}
	var out []SellLine
	index := map[string]int{}
	for _, item := range pack {
		name := strings.TrimSpace(item.Name)
		if name == "" || kept[strings.ToLower(name)] {
			continue
		}
		count := item.Count
		if count < 1 {
			count = 1
		}
		if i, ok := index[name]; ok {
			out[i].Count += count
			continue
		}
		index[name] = len(out)
		out = append(out, SellLine{Name: name, Count: count})
	}
	return out
}

// Describe is the status line for a phase; arrived describes the destination loop.
func Describe(p Phase, arrived string) string {
	switch p {
	case PhaseAccept:
		return "accepting the appearance screen"
	case PhaseTutorial:
		return "skipping the tutorial"
	case PhaseBright:
		return "turning the brightness up"
	case PhaseDrop:
		return "dropping balls of wool"
	case PhaseStore:
		return "at the general store"
	case PhaseWool:
		return "shearing and spinning"
	case PhaseFund:
		return "pickpocketing for the fare"
	case PhaseSkirt:
		return "buying a pink skirt in Varrock"
	case PhaseTravel:
		return "sailing to the destination"
	case PhaseArrived:
		return arrived
	}
	return string(p)
}
