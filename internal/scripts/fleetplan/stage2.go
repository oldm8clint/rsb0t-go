package fleetplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/tile"
)

// The level gates, all four content-verified against
// scripts/skill_thieving/configs/pickpocking/pickpocket.dbrow and
// scripts/skill_thieving/configs/stalls/stealing.dbrow on the local Content checkout, and
// cross-checked against docs/fleet_expansion_2026-09-18.md's own "Verified Content corrections"
// subsection, which names knights 55, paladins 70 (80 coins and two chaos runes) and heroes 80.
const (
	// BakersStallLevel is what the Baker's stall opens at ([stealing_bakery_stall]: level,5).
	BakersStallLevel = 5
	// KnightLevel is Stage 2's own transition point ([pickpocket_knight_of_ardougne]: level,55).
	KnightLevel = 55
	// PaladinLevel/HeroLevel are named for a later session's rung ladder; nothing here selects
	// them (see this file's own doc comment).
	PaladinLevel = 70
	HeroLevel    = 80
)

// ArdougneReach is how close to the East Ardougne market counts as "the westward traversal is
// finished". Big enough to cover the whole working area — the market, its seven stalls, and the
// Ardougne bank 23 tiles south — and no bigger.
//
// Deliberately tight, on live evidence. A first attempt used 120, which is comfortably enough to
// contain the working area but also accepted 2772,3232 — a tile 111 tiles east of the market,
// outside East Ardougne's own wall. The account genuinely was on the Ardougne land mass there,
// having crossed both ships under its own power, but the walker could not path from it to the
// stall at all ("unreachable from here (best 103 tiles)", after its full scene/unstick/backoff/
// verify ladder). Meanwhile freshstartrunner.SailLeg WAS still making progress from that tile —
// its next leg moved the account 50 tiles and opened a gate on the way — so the correct answer
// is to keep sailing, and the only thing that made the stage stop sailing was this constant
// being loose enough to call that spot "arrived".
const ArdougneReach = 40

// InArdougneReach reports whether the account has genuinely arrived in East Ardougne.
//
// This deliberately does NOT use freshstart.TravelLeg, and that is the whole point.
// Live-found 2026-09-18 running this stage's own travel leg: an account that started in the
// Lumbridge courtyard walked west, climbed onto the Port Sarim ship, and came to rest at
// 2956,3143 on level 1 — the ship's own deck, still on the mainland side with both crossings
// ahead of it. freshstart.TravelLeg called that LegDestination, because its OnKaramjaSide test
// is a rectangle-with-a-corner-cut that flattens the level away, and that tile falls inside it.
// Stage 2 believed it had arrived, stopped sailing, and asked the walker for the Baker's stall —
// which failed over and over with "unreachable from here (best 352 tiles)", because the stall
// genuinely is 352 tiles and two ships away.
//
// A distance test against the place the stage actually works cannot make that mistake: if the
// account is not near the market, it has not arrived, wherever a region predicate thinks it is.
// TravelLeg is still the right tool for the question it was written for — how much boat fare is
// still ahead — and FareRemaining keeps using it.
//
// The underlying OnKaramjaSide false positive is NOT fixed here: internal/scripts/freshstart is
// shared by every fresh-account script and is outside this stage's claim. Reported in COLLAB.md.
func InArdougneReach(t tile.Tile) bool {
	return tile.Chebyshev(tile.Tile{X: t.X, Z: t.Z}, tile.Tile{X: thieverplan.ArdougneMarket.X, Z: thieverplan.ArdougneMarket.Z}) <= ArdougneReach ||
		OnArdougneLowRung(t) // the Man and Farmer rungs the stall gate sends Stage 2 to (lowrungs.go)
}

func BakersStall() thieverplan.Method {
	m, _ := thieverplan.MethodByLabel("Baker's stall")
	return m
}

// ArdougneKnight is the Knight of Ardougne rung from the same table.
func ArdougneKnight() thieverplan.Method {
	m, _ := thieverplan.MethodByLabel("Knight of Ardougne")
	return m
}

// GameTick is this content's own canonical tick at real server speed. A lab engine may run
// faster; every wait below is therefore expressed in ticks and converted by the caller, except
// IdleStepFloor, which is deliberately absolute (see its doc comment).
const GameTick = 600 * time.Millisecond

// IdleStepFloor is the minimum wall-clock time one Stage 2 Loop iteration may take.
//
// This exists because of a whole CLASS of bug, not a specific one: a decision loop that
// re-derives its phase from live state every iteration, calling a primitive that can return
// instantly without doing anything, spins at CPU speed. The fleet has already been bitten hard by
// exactly this shape elsewhere — a single production account drove freshstartrunner.SailLeg's
// no-boat-needed short-circuit 398,648 times in 27 seconds (~14,800 iterations/second), earned 0
// XP, and wrote a 6.7 GB fleet log on its own.
//
// Stage 2 had FOUR instances of the same shape, all found by audit rather than live failure:
//
//   - Phase2Stall: thieverrunner.StealStall returns {Reason:"no-stall"} INSTANTLY whenever the
//     stall has not restocked, which at real tick speed is most of every 8-tick respawn window.
//   - Phase2Knight: thieverrunner.Pickpocket returns {Reason:"no-target"} instantly when no
//     Knight is in range — routine, since they wander.
//   - Phase2Drop: DropBakeryFood returns false instantly on several branches, while shouldDrop
//     stays true, so the phase is re-chosen immediately.
//   - Phase2Eat: EatBakeryFood returns false instantly when the pack read and the eat read
//     disagree about what is edible.
//
// A floor on the whole dispatch is the right shape rather than a delay per branch: it cannot be
// forgotten by the next branch someone adds, and it bounds the damage of any primitive that
// starts returning early in future. It is deliberately well UNDER one tick (so it never slows a
// loop that is doing real work, including on an accelerated lab engine) and well over zero (so a
// spinning loop costs ~7 iterations a second instead of ~14,800).
const IdleStepFloor = 150 * time.Millisecond

// Pacing is the one place a duration derived from game ticks is computed, and the engine's tick
// is its single explicit input.
//
// This shape exists because of a real, measured mistake rather than tidiness. The first version
// of these waits were free functions each call site passed GameTick to, and five sites did
// exactly that — so when the accelerated 200ms lab engine ran them, every one of them over-waited
// by 3x. Concretely: the stall's restock wait went from a bounded poll to 2.25x too long and
// dropped the Baker's stall from ~4.9s to ~13.8s a steal. Nothing was wrong with any individual
// function; the bug was that "which tick is this engine actually running?" was answered
// independently at five call sites, all with the same hardcoded guess.
//
// With a Pacing the question is answered once, by whoever constructs it (internal/bot's own
// "tickMs" setting, defaulting to the production-correct 600ms), and no site can reach past it
// for a constant. That is also precisely the failure mode the owner's "prove it at real server
// speed" requirement exists to catch: a script tuned against fast ticks hides timing bugs that
// only appear at 600ms, and the reverse — a real-speed constant applied to a fast engine — hides
// as lost throughput rather than as an error.
type Pacing struct {
	TickLen time.Duration
}

// Tick is the engine tick this Pacing was built for.
func (p Pacing) Tick() time.Duration {
	if p.TickLen <= 0 {
		return GameTick
	}
	return p.TickLen
}

// Ticks is n game ticks in wall clock — the general helper, so a new content-derived wait never
// needs to reach for a constant either.
func (p Pacing) Ticks(n int) time.Duration {
	if n < 0 {
		n = 0
	}
	return time.Duration(n) * p.Tick()
}

// IdleFloor is deliberately NOT tick-derived: see IdleStepFloor's own doc comment. It is a floor
// under a spinning loop, not a wait for anything in the game, so it must stay below one tick at
// every engine speed rather than scaling with it.
func (p Pacing) IdleFloor() time.Duration { return IdleStepFloor }

// StallRestock is how long to wait after the stall reports it has not restocked.
//
// A quarter of the content's own respawn — two ticks, for the Baker's stall's eight. The point is
// to BOUND the spin, not to sleep exactly until the restock: a few polls per respawn window costs
// nothing and stays robust if this Pacing's tick is slightly wrong.
func (p Pacing) StallRestock() time.Duration {
	ticks := BakersStall().RespawnTicks
	if ticks < 1 {
		ticks = 1
	}
	return p.Ticks(ticks) / 4
}

// NoTarget is how long to wait when a rung's own NPC is not in range. Short — they wander, and a
// Knight can walk back into leash within a tick or two.
func (p Pacing) NoTarget() time.Duration { return p.Ticks(2) }

// StallRefusal is why a Baker's-stall steal produced no experience. The classification matters
// because the two common causes want opposite responses, and getting that wrong costs real
// throughput — measured live 2026-09-18, see COLLAB.md.
type StallRefusal string

// The refusals, each traced to a specific branch of
// content/scripts/skill_thieving/scripts/stalls/stealing.rs2 on this Content checkout.
const (
	// StallRefusalNone is "nothing refused it" — used for a success or for a click that was
	// never made.
	StallRefusalNone StallRefusal = "none"
	// StallRefusalCombat is the ten-tick combat lockout:
	//
	//	if (add(%lastcombat, 10) > map_clock) {
	//	    mes("You can't steal from the market stall during combat!");
	//
	// so for ten ticks after ANY combat — and continuously while a roused guard keeps hitting —
	// every steal is refused outright. The right response is to wait it out where you are, NOT
	// to walk to the other stand tile: moving does not shorten the lockout, and walking while a
	// guard is mid-swing just keeps %lastcombat fresh.
	StallRefusalCombat StallRefusal = "combat"
	// StallRefusalPackFull is `if ($free_space = 0) ~mesbox("Your inventory is too full...")`.
	// The drop phase is what clears this; the stall step should not treat it as a stand problem.
	StallRefusalPackFull StallRefusal = "pack-full"
	// StallRefusalLevel is the level gate's own mesbox. It should be impossible for this stage
	// (the stall opens at 5 and Stage 1 hands over at 5), so seeing it means something is wrong
	// with the account, not with the stand tile.
	StallRefusalLevel StallRefusal = "level"
	// StallRefusalUnknown is the SILENT refusal, and the common one worth moving for:
	//
	//	if (~stealing_check_for_guard($data) = true) { return; }
	//	else if (~stealing_check_for_owner($data) = true) { return; }
	//
	// Both of those only `npc_say` ("Hey! Get your hands off there!") — an overhead line, not a
	// MESSAGE_GAME — and then return with no player-facing message at all, so the ONLY signal is
	// that no experience arrived. Both checks are `npc_huntall(coord, 5, 0)` plus
	// `lineofsight(npc_coord, coord)`, i.e. purely about where the account is standing, which is
	// exactly what the stand-tile rotation exists to change.
	StallRefusalUnknown StallRefusal = "unknown"
)

// stallRefusalLines are the exact message texts stealing.rs2 produces, lower-cased. Matched as
// substrings because the engine wraps some of them.
var stallRefusalLines = []struct {
	fragment string
	refusal  StallRefusal
}{
	{"steal from the market stall during combat", StallRefusalCombat},
	{"inventory is too full", StallRefusalPackFull},
	{"you need to be level", StallRefusalLevel},
}

// ClassifyStallChat reads the chat produced since a stall click and says why it was refused.
// Text with none of the known lines in it is StallRefusalUnknown — which, per the constants
// above, is the guard/owner line-of-sight case, because that one produces no chat at all.
func ClassifyStallChat(text string) StallRefusal {
	lower := strings.ToLower(text)
	for _, row := range stallRefusalLines {
		if strings.Contains(lower, row.fragment) {
			return row.refusal
		}
	}
	return StallRefusalUnknown
}

// ShouldMoveStand reports whether a refusal is one that standing somewhere else could fix.
func ShouldMoveStand(r StallRefusal) bool { return r == StallRefusalUnknown }

// SuccessChance is a pickpocket rung's own chance of success at the given level, 0-1.
//
// Ported exactly from this engine's own STAT_RANDOM opcode
// (engine/src/engine/script/handlers/PlayerOps.ts), which every pickpocket in
// scripts/skill_thieving/scripts/pickpocket.rs2 calls with the rung's own success_chance pair:
//
//	value  = floor(low*(99-L)/98) + floor(high*(L-1)/98) + 1     (L clamped to 99)
//	chance = floor(random()*256)                                 (uniform 0..255)
//	success iff value > chance
//
// so the probability is value/256, clamped into 0-1. A stall has no roll at all and always
// returns 1.
func SuccessChance(m thieverplan.Method, level int) float64 {
	if m.Kind != thieverplan.KindPickpocket {
		return 1
	}
	if level > levels.MaxLevel {
		level = levels.MaxLevel
	}
	if level < 1 {
		level = 1
	}
	value := (m.SuccessLow*(99-level))/98 + (m.SuccessHigh*(level-1))/98 + 1
	p := float64(value) / 256
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}
