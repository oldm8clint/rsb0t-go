// Package trawlerplan is the FishingTrawler crew's decisions: which phase an account is in, what to do next
// aboard the boat, when to log out before the game ends so the catch pays the anchor, and how to read the
// overlay's time text. It is pure: no client, actions, fleet or bot imports and no I/O.
//
// Facts (content 289, game_trawler): the lobby is x 2669-2673, z 3166-3182; the gangplank (loc 2178, "Cross")
// is at 2674,3170 level 1. The boat is the map copy at x 1856-1919 / z 4800-4863 and, once flooded, the copy at
// x 1984-2047. Level 0 is the hold, level 1 the deck. A game is 1000 ticks; the overlay (trawler_overlay) shows
// the time left in WHOLE MINUTES ("N mins") and "Under 1 min" for the last 100 ticks, so the last minute is the
// only part that can be timed, by watching when "Under 1 min" first appears.
package trawlerplan

import (
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Defaults.
const (
	DefaultAnchor             = "master"
	DefaultKitPaste           = 60 // a user request: "more swamp paste per fishing trawler run"; it stacks, the store holds 500 a world
	DefaultKitRope            = 0  // a crew member under NetCrafting mends nothing (Aboard), so its Rope is the menders' (4 Nov 2026)
	DefaultKitMendRope        = 8  // a mender's kit: 5-10 mends a game; the store's 30 a world cover about four of them (KitRopeFor)
	NetCrewRope               = 10 // a standing net crew member mends first at every rip
	MendRopeLow               = 3  // under this many a mender goes to its bank, and the mule, for more (NeedsRope)
	DefaultLeaveAtSecondsLeft = 12 // owner: the last tick, give or take; 12 s since live 3 Nov 2026 a logout sent in the last seconds still counted
	DefaultMaxFloodToLeave    = 80 // the boat sinks at 100; the last 12 s add at most ~36
	// HeartbeatEvery is the longest the script stays silent (the stall watchdog acts on ten minutes).
	HeartbeatEvery = 4 * time.Minute
	// UnderMinuteSeconds is the span "Under 1 min" covers (100 ticks of 0.6 s).
	UnderMinuteSeconds = 60
)

// Object, loc and interface ids (content pack files).
const (
	PasteID      = 1941 // swamppaste
	RopeID       = 954  // rope
	BucketID     = 583  // bucket_bailing
	BucketFullID = 585  // bucket_bailingfull

	LocLeak       = 2167 // shipleak, op "Fill"
	LocNetTopL    = 2172 // trawlernet_top_l: the RIPPED net, op "Inspect"
	LocNetTopR    = 2173 // trawlernet_top_r
	LocLadderUp   = 2174 // trawler_ladder, "Climb-up" (hold -> deck)
	LocLadderDown = 2175 // trawler_laddertop, "Climb-down" (deck -> hold)
	LocGangplank  = 2178 // trawler_gangplank, "Cross"

	// OverlayNetOkay, OverlayNetRipped, OverlayCatch and OverlayTimeLeft are the trawler_overlay components
	// com_26..com_29 (content/pack/interface.pack: trawler_overlay is 11908, com_N is 11909+N).
	OverlayNetOkay   = 11935
	OverlayNetRipped = 11936
	OverlayCatch     = 11937
	OverlayTimeLeft  = 11938
)

// Places.
var (
	Gangplank = tile.Tile{X: 2674, Z: 3170, Level: 1}
	// LobbyStand is where the crew waits for the board call: the Port Khazard pier beside the gangplank. The lobby
	// itself is the docked boat's deck (level 1), reached only by Cross (trawler_gangplank.rs2: a step west and a
	// telejump two west and one level up); 2 Nov 2026 the first crew walked for 2671,3170,1 and found it unreachable.
	LobbyStand = tile.Tile{X: 2676, Z: 3170, Level: 0}
	// Stores are the two Port Khazard general store keepers' spawns ("Shop keeper", npc 555).
	Stores = []tile.Tile{{X: 2641, Z: 3171, Level: 0}, {X: 2655, Z: 3152, Level: 0}}
	// PortSarim is where logging in on a trawler zone lands (content login.rs2 / trawler.rs2).
	PortSarim = tile.Tile{X: 3026, Z: 3156, Level: 0}
)

// AtStoreOrPier reports whether t is in Port Khazard (the stores, the pier): a crew member there on another world is buying
// its kit.
func AtStoreOrPier(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2630 && t.X <= 2680 && t.Z >= 3140 && t.Z <= 3185
}

// AtPier reports whether t is on the pier beside the gangplank, where the crew waits for the board call.
func AtPier(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2675 && t.X <= 2680 && t.Z >= 3165 && t.Z <= 3176
}

// InLobby reports whether t is in the lobby zone (any level).
func InLobby(t tile.Tile) bool { return t.X >= 2669 && t.X <= 2673 && t.Z >= 3166 && t.Z <= 3182 }

// OnBoat reports whether t is in either boat copy (the unflooded x 1856-1919 or the flooded x 1984-2047).
func OnBoat(t tile.Tile) bool {
	return t.Z >= 4800 && t.Z <= 4863 && ((t.X >= 1856 && t.X <= 1919) || (t.X >= 1984 && t.X <= 2047))
}

// InWreck reports whether t is in the sunken boat's water (trawler_wreck_zones, mapsquare 30_75), where the account swims
// until it climbs onto a floating barrel.
func InWreck(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 1920 && t.X <= 1983 && t.Z >= 4800 && t.Z <= 4863
}

// OnDeck reports whether t is on the docked boat's deck at Port Khazard (the lobby). The deck is bridged, so the scene may
// report level 0 or 1 (live 5 Nov 2026: 2671,3177,0). The way off is the gangplank (Cross); a walk cannot leave it.
func OnDeck(t tile.Tile) bool { return InLobby(t) }

// AtSea reports whether t is in a game: on either boat copy or swimming in the wreck. The game's end takes the account off.
func AtSea(t tile.Tile) bool { return OnBoat(t) || InWreck(t) }

// OnFloodedBoat reports whether t is in the flooded boat copy.
func OnFloodedBoat(t tile.Tile) bool { return OnBoat(t) && t.X >= 1984 }

// FarFromKhazard reports whether t is too far from Port Khazard for the lobby to be a short walk (Port Sarim
// after a login on the boat, or anywhere else a hop left the account).
func FarFromKhazard(t tile.Tile) bool {
	return !OnBoat(t) && tile.Chebyshev(t, Stores[0]) > 100
}

// ---- time left ----

// ParseTimeLeft reads the overlay's time text. seconds is an UPPER bound of the time left (the text floors to
// whole minutes: "3 mins" is 180 to 240 s left, "Under 1 min" is 0 to 60 s); underMinute marks the last
// minute, the only part SecondsLeft can tighten. ok is false for text it does not know.
func ParseTimeLeft(text string) (seconds int, underMinute, ok bool) {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return 0, false, false
	}
	if strings.HasPrefix(t, "under 1 min") {
		return UnderMinuteSeconds, true, true
	}
	f := strings.Fields(t)
	if len(f) == 2 && (f[1] == "min" || f[1] == "mins") {
		n, err := strconv.Atoi(f[0])
		if err != nil || n < 0 {
			return 0, false, false
		}
		return n*60 + 59, false, true
	}
	return 0, false, false
}

// ParseMurphyMinutes reads Murphy's "N minutes left!" (trawler_control.rs2: said every 20 steps, at N = 9 down to 1).
func ParseMurphyMinutes(line string) (int, bool) {
	f := strings.Fields(strings.ToLower(strings.TrimSpace(line)))
	if len(f) != 3 || (f[1] != "minutes" && f[1] != "minute") || f[2] != "left!" {
		return 0, false
	}
	n, err := strconv.Atoi(f[0])
	if err != nil || n < 1 || n > 9 {
		return 0, false
	}
	return n, true
}

// MurphySecondsLeft is the time left after Murphy said n minutes since ago (-1 with n unknown): he says it when exactly n
// minutes (n x 100 ticks) are left.
func MurphySecondsLeft(n int, since time.Duration) int {
	if n <= 0 {
		return -1
	}
	return n*60 - int(since.Seconds())
}

// GameAfterBoarding is how long a game runs from the teleport aboard (live 2 Nov 2026: 23:27:33 to 23:38:12, 639 s; content:
// a 20-tick sail, 33 ticks to the start, 1,000 ticks of game), less a margin for the snapshot that first saw the boat.
const GameAfterBoarding = 635 * time.Second

// BoardSecondsLeft is the time left since aboard after first seeing the boat.
func BoardSecondsLeft(since time.Duration) int { return int((GameAfterBoarding - since).Seconds()) }

// Watches is how many crews take turns at the anchor's games. A game pays by the crew aboard (trawler_catch_fish rolls once
// per player every 5 ticks) and the crew needs about 15 minutes to walk home from Port Sarim after its end-of-game hop, so
// with one crew the anchor waited out that walk every game (live 3 Nov 2026: 29-minute cycles, 11 of them at sea). Two
// watches sail alternate games: one walks home while the other plays, and each boat still holds about half the fleet,
// under the ~235 aboard that dropped connections.
const Watches = 2

// Watch is the watch (1..Watches) of the crew member named name: a stable pick by name (FNV-1a, case-insensitive).
func Watch(name string) int {
	h := uint32(2166136261)
	for _, c := range strings.ToLower(strings.TrimSpace(name)) {
		h = (h ^ uint32(c)) * 16777619
	}
	return int((h/100)%Watches) + 1
}

// GameWatch is the watch called to the anchor's game number game (0 the first since it started).
func GameWatch(game int) int { return game%Watches + 1 }

// SecondsLeft estimates the seconds left: the minute count's upper bound, and in the last minute 60 minus how
// long "Under 1 min" has been showing. -1 when the text is not understood.
func SecondsLeft(text string, sinceUnderMinute time.Duration) int {
	secs, under, ok := ParseTimeLeft(text)
	if !ok {
		return -1
	}
	if !under {
		return secs
	}
	left := UnderMinuteSeconds - int(sinceUnderMinute.Seconds())
	if left < 0 {
		left = 0
	}
	return left
}

// ---- phases ----

// Phase is where in a trip an account is.
type Phase string

// Phases.
const (
	// PhaseKit buys the kit at the Port Khazard store.
	PhaseKit Phase = "kit"
	// PhaseTravel walks to the lobby.
	PhaseTravel Phase = "travel"
	// PhaseLobby crosses the gangplank and waits to be taken aboard.
	PhaseLobby Phase = "lobby"
	// PhaseAboard works the boat.
	PhaseAboard Phase = "aboard"
	// PhaseLeave logs out of this world (a world hop) before the game ends.
	PhaseLeave Phase = "leave"
	// PhaseReturn walks back to Port Khazard after a login at Port Sarim.
	PhaseReturn Phase = "return"
	// PhaseEscape climbs onto a floating barrel after the boat sank (trawler_sink.rs2): the only way out of the wreck.
	PhaseEscape Phase = "escape"
	// PhaseHome hops back to the configured world.
	PhaseHome Phase = "home"
	// PhasePickup takes the dumped catch off the floor by the net (catch.go).
	PhasePickup Phase = "pickup"
	// PhaseBank banks the catch at the nearest bank and draws the alchemy kit there.
	PhaseBank Phase = "bank"
	// PhaseAside waits at an AsideSpot while the anchor is at sea.
	PhaseAside Phase = "aside"
	// PhaseLoot is a Looter waiting at LootStand for the relayed catch.
	PhaseLoot Phase = "loot"
)

// Snapshot is everything Decide reads.
type Snapshot struct {
	Here tile.Tile
	// World is the world this account is on; the pinned one is Settings.World (0 plays anywhere).
	World int
	// Pack counts.
	Paste, Rope, Bucket, BucketFull int
	// KitGaveUp is true after a purchase attempt that the store could not fill (stock out): go with what is held.
	KitGaveUp bool
	// SecondsLeft is the estimated time left (-1 unknown); AnchorAboard whether the anchor was seen aboard
	// recently; FloodLevel the flood level 0-100 (-1 unknown).
	SecondsLeft  int
	AnchorAboard bool
	FloodLevel   int
	// GroundFish is the raw trawler fish visible on the floor by the net; FreeSlots the pack's free slots; CatchHeld the raw
	// trawler fish in the pack.
	GroundFish, FreeSlots, CatchHeld int
	// NeedBank is the runner's "the alchemy kit wants a bank visit" (a fire staff or the natures not carried, and not looked
	// for lately).
	NeedBank bool
	// BusKnown is a wall bus to hear the anchor on; AnchorCalled the anchor's board call heard on it.
	BusKnown, AnchorCalled bool
	// AnchorAtSea is the anchor saying it is aboard (its game under way) while this account is not.
	AnchorAtSea bool
	// Looter marks a crew member that never sails and banks the catch the anchor relays (trawlerplan.Looter).
	Looter bool
}

// AsideSpots are where the crew waiting for the next game stand while the anchor is at sea, not in the lobby (live 3 Nov
// 2026, 08:49-08:53: 116 accounts dropped, 87 of them crew, while watch 1's 125 played and watch 2 waited in the lobby;
// one crowd of 187 at a time had played clean). Ground tiles the crew have walked through, 16 or more apart (out of each
// other's view) and 19-30 from the lobby: well inside the 100 ticks the boat waits after the first crossing
// (trawler_gangplank.rs2), and the crew walk in once the anchor is ashore and mustering.
var AsideSpots = []tile.Tile{{X: 2652, Z: 3155}, {X: 2641, Z: 3171}}

// AsideSpot is the aside spot of the crew member named name (a stable pick by name).
func AsideSpot(name string) tile.Tile {
	h := uint32(2166136261)
	for _, c := range strings.ToLower(strings.TrimSpace(name)) {
		h = (h ^ uint32(c)) * 16777619
	}
	return AsideSpots[int((h/7)%uint32(len(AsideSpots)))]
}

// KitComplete reports whether the pack holds the kit set wants (or the store could not supply more).
func KitComplete(s Snapshot, set Settings) bool {
	if s.KitGaveUp {
		return true
	}
	if s.Paste < set.KitPaste || s.Rope < set.KitRope {
		return false
	}
	return !set.Bucket || s.Bucket+s.BucketFull > 0
}

// Decide is the phase. Aboard (and past the leave line) is decided first, then the walk back from Port Sarim,
// then the world; then, unless the anchor is calling everyone aboard, the dumped catch by the net (pick it up while the
// pack has room, bank it when the pack is full or the floor empty) and a bank visit the alchemy kit wants; then the lobby,
// then the kit.
func Decide(s Snapshot, set Settings) Phase {
	if InWreck(s.Here) {
		return PhaseEscape
	}
	if OnBoat(s.Here) {
		if ShouldLeave(s.SecondsLeft, s.AnchorAboard, OnFloodedBoat(s.Here), s.FloodLevel, set) {
			return PhaseLeave
		}
		return PhaseAboard
	}
	if FarFromKhazard(s.Here) {
		return PhaseReturn
	}
	// Off the game world only to fill the kit at another world's store (a user request: "hop between worlds and wait
	// until each account has supplies"): home once the kit is complete.
	if set.World > 0 && s.World != set.World && (KitComplete(s, set) || !AtStoreOrPier(s.Here)) {
		return PhaseHome
	}
	if s.Looter {
		switch {
		case s.GroundFish > 0 && s.FreeSlots > 0:
			return PhasePickup
		case s.CatchHeld > 0 && (s.FreeSlots == 0 || s.GroundFish == 0), s.NeedBank:
			return PhaseBank
		}
		return PhaseLoot
	}
	if !s.AnchorCalled {
		if s.GroundFish > 0 && s.FreeSlots > 0 {
			return PhasePickup
		}
		if s.CatchHeld > 0 && (s.FreeSlots == 0 || s.GroundFish == 0) {
			return PhaseBank
		}
		if s.NeedBank && !InLobby(s.Here) {
			return PhaseBank
		}
	}
	if s.AnchorAtSea && !s.AnchorCalled && KitComplete(s, set) {
		return PhaseAside
	}
	if InLobby(s.Here) || (AtPier(s.Here) && KitComplete(s, set)) {
		return PhaseLobby
	}
	if !KitComplete(s, set) {
		return PhaseKit
	}
	return PhaseTravel
}

// ShouldLeave says the crew member logs out now: time is nearly up, the anchor is aboard to take the catch,
// and the boat is not in danger of sinking without this account. On the flooded copy it leaves only at a known
// flood level of at most set.MaxFloodToLeave.
// QuietSecondsLeft is when the crew stops every action before leaving: content's logout (logout.rs2) needs p_finduid, which a
// player mid-action (an alch, a fill, a bail) fails, and the click is ignored (live 3 Nov 2026: 221 hops at 02:10, yet the catch
// was split some 270 ways). 40 s since game 17 (05:00): with 20, 52 of 188 logouts still got no reply, the quiet having
// begun only seconds before the leave on a clock estimate.
const QuietSecondsLeft = 40

// BailUntilSecondsLeft is when a bucket holder on the flooded boat stops bailing: a bail holds the player one tick, not the
// several an alch does, and a quiet flooded boat rose past the leave limit and kept the whole crew aboard (live 3 Nov 2026,
// game 18).
const BailUntilSecondsLeft = 15

func ShouldLeave(secondsLeft int, anchorAboard, flooded bool, floodLevel int, set Settings) bool {
	if secondsLeft < 0 || secondsLeft > set.LeaveAtSecondsLeft || !anchorAboard {
		return false
	}
	if flooded && (floodLevel < 0 || floodLevel > set.MaxFloodToLeave) {
		return false
	}
	return true
}

// LeaveWorld is the world to hop to for leaving: the lowest of worlds 1..3 other than the current one.
func LeaveWorld(current int) int {
	for w := 1; w <= 3; w++ {
		if w != current {
			return w
		}
	}
	return 0
}

// ---- aboard ----

// ActionKind is one thing to do aboard.
type ActionKind string

// Aboard actions.
const (
	ActFillLeak  ActionKind = "fill-leak"
	ActFixNet    ActionKind = "fix-net"
	ActEmpty     ActionKind = "empty-bucket"
	ActBail      ActionKind = "bail"
	ActClimbUp   ActionKind = "climb-up"
	ActClimbDown ActionKind = "climb-down"
	ActIdle      ActionKind = "idle"
	ActGoTo      ActionKind = "go-to-post"
)

// BoatView is what the account sees aboard.
type BoatView struct {
	Here tile.Tile
	// Leaks and RippedNets are the visible shipleak and trawlernet_top locs on the account's own level.
	Leaks, RippedNets []tile.Tile
	// NetRipped is the overlay saying "Ripped!".
	NetRipped                       bool
	Paste, Rope, Bucket, BucketFull int
	// NetFirst puts a ripped net before the leaks (the anchor: Crafting 96 mends it every time, and no fish come in while it
	// is ripped). Post, when set, is the hull tile this account guards (HullPost).
	NetFirst bool
	Post     *tile.Tile
	// NetFixer marks one of the few crew who wait on the upper deck by the net all game, rope in hand, so a rip is mended at
	// once (the net rips about every 50 s; no fish come in while it is ripped).
	NetFixer bool
	// Crafting is the account's Crafting level (0: unknown). A net mend rolls stat_random(crafting, 30, 300): 12% at 1,
	// certain from about 88. Under NetCrafting a rope holder leaves the net to better menders and stays in the hold.
	Crafting int
	// FloodLevel is the water (0-100, %trawler_water_overlay; under 0 unknown) and BailAt the level from which this account
	// bails the flooded boat (BailAt; 0 bails at any level).
	FloodLevel, BailAt int
}

// Keeping the boat flooded (live 3 Nov 2026, game 20). With 16 or more players aboard every hull re-leaks on a coin flip
// every 5 ticks (trawler_increase_flooding: leak chance max(32/players, 2), up to 9 open), so the water always climbs to 30
// and floods the boat, and a crew that all bailed at once had it dry again in a tick or two: flooded, dry, flooded, every
// 10-20 seconds, each flip a teleport of everyone aboard (trawler_player_flood/unflood). A teleport keeps the server from
// answering a logout for seconds: 30 of the 35 crew who left just after the last flip stayed aboard and split the catch
// 36 ways (2,320 fish, 64 to the anchor). The fish come in just the same on the flooded boat (murphy_halfsunk), so the crew
// lets it flood once and holds the water between the bailers' levels: the first BailRanks of the crew aboard (by name) bail,
// each from its own level, BailLow and up 1.75 a rank, so the more the water the more bail and only a few bail near the
// bottom, where a dry boat lies; the rest only past BailEmergency, short of the sinking at 100. By rank, not a share of the
// crew: the inflow is the same 9 leaks for any crew of 16 or more, and one bailer in eight of a 16-crew watch held nothing
// (live 3 Nov 2026, 08:05: the water sat at 84-93, past the 80 the crew may leave at).
const (
	BailLow       = 15
	BailRanks     = 24
	BailEmergency = 60
)

// BailAt is the water level from which the crew member ranked rank (0-based, by name among the crew aboard; under 0
// unknown) bails the flooded boat.
func BailAt(rank int) int {
	if rank >= 0 && rank < BailRanks {
		return BailLow + rank*7/4 // 15..55, under BailEmergency
	}
	if rank < 0 {
		rank = -rank
	}
	return BailEmergency + rank%8
}

// CrewRank is me's 0-based place, case-insensitive, among the names aboard (me counted whether listed or not).
func CrewRank(me string, aboard []string) int {
	key := strings.ToLower(strings.TrimSpace(me))
	rank := 0
	for _, n := range aboard {
		if k := strings.ToLower(strings.TrimSpace(n)); k != key && k < key {
			rank++
		}
	}
	return rank
}

// bailNow reports whether the account bails at the boat's water level. A flooded boat under 1 water would have dried out, so
// a reading under 1 there is no reading: bail.
func (v BoatView) bailNow() bool {
	return v.BailAt <= 0 || v.FloodLevel < 1 || v.FloodLevel >= v.BailAt
}

// Bailing reports whether an account on the flooded boat with a bucket keeps bailing at secondsLeft (the quiet period's
// exception, BailUntilSecondsLeft): only at or above its own level.
func Bailing(flooded, bucket bool, secondsLeft, floodLevel, bailAt int) bool {
	v := BoatView{FloodLevel: floodLevel, BailAt: bailAt}
	return flooded && bucket && secondsLeft > BailUntilSecondsLeft && v.bailNow()
}

// NetCrafting is the Crafting level from which a crew member climbs to a ripped net (about half its tries succeed).
const NetCrafting = 40

// Rope goes to the crew who can mend (4 November 2026). A net mend spends one Rope, a good game mends 30-45 times and a two-hour
// block spends 150-200, but the Port Khazard store holds 30 a world (five a minute back: content port_khazard.inv stock9=rope,30,20)
// and every crew member bought one, so the first 30 to arrive took it whatever their Crafting, while Aboard sends a Rope holder
// under NetCrafting back to bail (only 7 of ~20 crew aboard carried a usable Rope in the last bad game, and one game froze at 205
// fish). So Rope is the menders' kit: KitRopeFor.

// KitRopeFor is the Rope an account buys for its trip: the menders' kit (set.KitMendRope, at least set.KitRope) for Crafting
// NetCrafting and up, the standing net crew's NetCrewRope when it is that, and set.KitRope (default 0) for the rest. A Crafting of 0
// is unknown (the skill is not read yet) and takes set.KitRope.
func KitRopeFor(crafting int, set Settings, netCrew bool) int {
	if crafting < NetCrafting {
		return set.KitRope
	}
	n := max(set.KitRope, set.KitMendRope)
	if netCrew && set.KitMendRope > 0 {
		n = max(n, NetCrewRope)
	}
	return n
}

// RopeShort is how many Rope a mender is short of its kit (0 for an account that cannot mend, or one holding the kit): what its
// bank is drawn for and, when NeedsRope, the mule asked.
func RopeShort(crafting, held int, set Settings, netCrew bool) int {
	if crafting < NetCrafting {
		return 0
	}
	return max(0, KitRopeFor(crafting, set, netCrew)-held)
}

// NeedsRope reports a mender (Crafting NetCrafting and up, a kit above 0) holding fewer than MendRopeLow Rope: its bank, and
// the mule, are asked for more.
func NeedsRope(crafting, held int, set Settings) bool {
	return crafting >= NetCrafting && KitRopeFor(crafting, set, false) > 0 && held < min(MendRopeLow, KitRopeFor(crafting, set, false))
}

// The hull tiles that leak (trawler_hulls: mapsquare 29,75, x 1885-1892 in two rows, z 4826 and 4823); the flooded copy is
// 128 tiles east.
var hullRows, hullX0 = [2]int{4826, 4823}, 1885

// HullPost is the hull tile key guards (a user request: "1 account is fixing each leak that can occur waiting near by ready
// to fix the instant it breaks"): sixteen posts, accounts spread over them by key.
func HullPost(key int, flooded bool) tile.Tile {
	k := ((key % 16) + 16) % 16
	t := tile.Tile{X: hullX0 + k%8, Z: hullRows[k/8]}
	if flooded {
		t.X += 128
	}
	return t
}

// postStand is the deck tile beside a hull post, inside the hold.
func postStand(post tile.Tile) tile.Tile {
	if post.Z == hullRows[0] {
		return tile.Tile{X: post.X, Z: post.Z - 1}
	}
	return tile.Tile{X: post.X, Z: post.Z + 1}
}

// Action is the next move; Target is the loc tile for a fill or fix.
type Action struct {
	Kind   ActionKind
	Target tile.Tile
}

func nearest(from tile.Tile, ts []tile.Tile) (tile.Tile, bool) {
	best, bestD, ok := tile.Tile{}, 1<<30, false
	for _, t := range ts {
		if d := tile.Chebyshev(from, t); d < bestD {
			best, bestD, ok = t, d, true
		}
	}
	return best, ok
}

// Aboard chooses the next move: in the flooded hold empty a full bucket; mend a ripped net (no fish while it is ripped) with
// rope, unless the flooded boat's water is past BailEmergency; bail the flooded hold at the account's own level; fill a leak
// (at the account's hull post when it has one) while paste is held; then, on the flooded boat, bail from the hold, climbing
// down to it when need be; otherwise wait in the hold, where every hull leak is.
func Aboard(v BoatView) Action {
	flooded := OnFloodedBoat(v.Here)
	inFloodedHold := flooded && v.Here.Level == 0
	if inFloodedHold && v.BucketFull > 0 {
		return Action{Kind: ActEmpty}
	}
	// A ripped net before bailing and before any leak: no fish come in while it is ripped. The bailers' ranks hold the flooded
	// boat's water near 30 without the rope holders (live 4 Nov 2026, 08:19-08:24: the water sat at 27-31 while the seven rope
	// holders aboard bailed or filled leaks, and the catch stopped at 205 for the game's last five minutes; live 3 Nov 2026,
	// 10:29-10:35: 80 rope holders guarded hull posts and the catch stopped at 724). Only from BailEmergency, or with no
	// reading, does the bucket come first (the boat sinks at 100). The anchor mends whatever its Crafting.
	mend := v.NetRipped && v.Rope > 0 && (v.NetFirst || v.Crafting == 0 || v.Crafting >= NetCrafting)
	if mend && !(inFloodedHold && v.Bucket > 0 && (v.FloodLevel < 1 || v.FloodLevel >= BailEmergency)) {
		if t, ok := nearest(v.Here, v.RippedNets); ok {
			return Action{Kind: ActFixNet, Target: t}
		}
		if v.Here.Level == 0 {
			return Action{Kind: ActClimbUp}
		}
	}
	// The flooded boat sinks at 100 water: a bucket bails before a leak (live 2 Nov 2026, 18:41: 250 bucket holders filled
	// leaks on the flooded boat and one bailed). A bail takes one water off in a tick, its empty another; a filled leak only
	// stops one water every five ticks until the hull leaks again.
	if inFloodedHold && v.Bucket > 0 && v.bailNow() {
		return Action{Kind: ActBail}
	}
	if v.Paste > 0 && v.Post != nil && v.Here.Level == 0 {
		post := *v.Post
		var near []tile.Tile
		for _, l := range v.Leaks {
			if tile.Chebyshev(l, post) <= 2 {
				near = append(near, l)
			}
		}
		if t, ok := nearest(v.Here, near); ok {
			return Action{Kind: ActFillLeak, Target: t}
		}
		if stand := postStand(post); tile.Chebyshev(v.Here, stand) > 1 {
			return Action{Kind: ActGoTo, Target: stand}
		}
		return Action{Kind: ActIdle}
	}
	if v.Paste > 0 {
		if t, ok := nearest(v.Here, v.Leaks); ok {
			return Action{Kind: ActFillLeak, Target: t}
		}
	}
	if v.NetFixer && v.Rope > 0 {
		if v.Here.Level == 0 {
			return Action{Kind: ActClimbUp}
		}
		if v.Here.Level == 1 {
			if t, ok := nearest(v.Here, v.RippedNets); ok && v.NetRipped {
				return Action{Kind: ActFixNet, Target: t}
			}
			return Action{Kind: ActIdle} // wait by the net
		}
	}
	if flooded && (v.BucketFull > 0 || (v.Bucket > 0 && v.bailNow())) {
		if v.Here.Level == 0 {
			if v.BucketFull > 0 {
				return Action{Kind: ActEmpty}
			}
			return Action{Kind: ActBail}
		}
		return Action{Kind: ActClimbDown}
	}
	// The hold is where the work is (every hull leak, and the bailing once flooded): stay or go down there.
	if v.Here.Level != 0 {
		return Action{Kind: ActClimbDown}
	}
	return Action{Kind: ActIdle}
}
