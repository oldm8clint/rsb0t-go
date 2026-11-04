package fisherplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// FishingGuildEntryLevel is checked by loc_2025 in LostCity Content's
// skill_fishing/scripts/fishing_guild.rs2. Level 67 cannot enter without a boost;
// this planner does not assume that an account owns or maintains a boost.
const FishingGuildEntryLevel = 68

// FishingGuildMuleAt is the Guild's own hand-off threshold, used instead of MuleFishAt while
// the planner picks the Guild and the account does not name muleAt itself.
//
// Doubled to 20,000 alongside MuleFishAt earlier on 2026-09-20 and PUT BACK the same day,
// because measuring the result showed the reasoning behind the doubling was wrong.
//
// The doubling was justified on the mule being capacity-bound. It is not. Measured on the
// live fleet over the 93 minutes before the change: 44 trades, the mule busy 32% of the run,
// and demand almost exactly matching supply at 1.0x. There was about 3x headroom, so raising
// the threshold removed throughput without relieving anything.
//
// The Guild figure is the one that mattered, because fishingGuild defaults ON for every
// account in the live roster (a code default, not a roster setting), so 20,000 was what the
// fleet was really using -- jittered to 25,600-26,000 in the log. Against a median banked
// stock of 1,718 fish and about 206 fish gained per account per hour, that is days of
// accumulation per hand-off; the 55 minutes after the change recorded ZERO trades against 44
// in the 93 minutes before it.
//
// MuleFishAt stays at 4,000 (the owner's call): it applies below Guild level, where the trip
// is short and the bank fills faster, so halving those trips costs little.
const FishingGuildMuleAt = 10000

type FishingArea string

const (
	AreaCatherby     FishingArea = "catherby"
	AreaFishingGuild FishingArea = "fishing-guild"
)

// FishingGuild is the Guild's layout, used only while fishingGuild selects it.
// The bank stand is beside map m40_53's booth row at x2585,z3418..3422. It is
// 2586,3419, east of the booth at 2585,3419, not 2586,3420: 2585,3420 holds no
// "Bank booth", so from 2586,3420 every booth is diagonal and the engine ignores the
// click (lab 2026-09-19: two booths timed out, 30s each, before the walker moved on).
// The camp 2603,3416 is walkable in the current collision pack. The water tile 2605,3418 is NOT a valid camp. SpotRange covers all 23
// positions in Content's fishing_movement_fishing_guild_enum.
// Guild shops need their own NPC interaction; Harry/Arhein stands remain in
// Catherby so an integration cannot accidentally address those NPCs at the guild.
func FishingGuild() Layout {
	l := Catherby()
	l.Bank = tile.Tile{X: 2586, Z: 3419}
	l.BankRadius = 35
	l.Camp = tile.Tile{X: 2603, Z: 3416}
	l.SpotRange = 12
	l.ApproachOrthogonal = true
	return l
}

// FishingGuildDoor is loc2025 in Content maps/m40_53.jm2, local (51,2) — mapsquare 40,53 has
// origin 2560,3392, so local (51,2) is 2611,3394. It is shape 0 (wall_straight) at angle 3
// (loc_south), i.e. the wall is on that tile's south face.
//
// NOTHING CROSSES THIS DOOR THROUGH THIS CONSTANT, and nothing needs to. internal/webwalk's
// baked graph already carries loc 2025 at 2611,3394 in both directions (and the guild's other
// door, loc 1530 at 2611,3398), so every script that walks with a webwalk Walker crosses it
// for free. This constant is only a label; it is kept because it names the tile that a log
// line or a stranded account will point at, and because the arithmetic above is worth not
// redoing. Checked 2026-09-22 while 68 ScarabSwarm accounts were stuck inside the guild: the
// missing edge was NOT the cause (see guildDoorWhy and noteRefusal in internal/webwalk).
var FishingGuildDoor = tile.Tile{X: 2611, Z: 3394}

// SupportsGuildRung excludes the small-net Net/Bait method: guild spots expose
// Cage/Harpoon and Net/Harpoon, as fishing.npc defines, not Net/Bait.
func SupportsGuildRung(r Rung) bool {
	return (r.Op == "Net" && r.Pair == "Harpoon") ||
		(r.Op == "Cage" && r.Pair == "Harpoon") ||
		(r.Op == "Harpoon" && (r.Pair == "Cage" || r.Pair == "Net"))
}

// FishingRoute plans the fishing region and the banking itinerary for a handoff.
// The shared account bank can be withdrawn at the guild before walking to the
// configured receiver in Catherby; fishing resumes at ReturnBank afterwards.
// Runtime code must still verify each journey, door and bank operation.
type FishingRoute struct {
	Area        FishingArea
	Layout      Layout
	MuleAt      int
	HandoffBank tile.Tile
	Receiver    tile.Tile
	ReturnBank  tile.Tile
}

// PlanFishingRoute is opt-in and pure. The caller supplies the actual active rung
// (including tool ownership), not merely the highest level-unlocked method.
// internal/bot/fisher.go wires it behind the fishingGuild setting, default off.
func PlanFishingRoute(guildEnabled bool, level int, active Rung, receiver tile.Tile) FishingRoute {
	l := Catherby()
	r := FishingRoute{Area: AreaCatherby, Layout: l, MuleAt: MuleFishAt,
		HandoffBank: l.Bank, Receiver: receiver, ReturnBank: l.Bank}
	if guildEnabled && level >= FishingGuildEntryLevel && SupportsGuildRung(active) {
		r.Area = AreaFishingGuild
		r.Layout = FishingGuild()
		r.MuleAt = FishingGuildMuleAt
		r.ReturnBank = r.Layout.Bank
	}
	return r
}

// InFishingGuild reports whether t is within reach of the Guild bank: the guild itself and
// the Hemenster ground just outside its entry door. It is the Guild's counterpart of
// InCatherby, and the two never overlap (the banks are 223 tiles apart on x).
func InFishingGuild(t tile.Tile) bool {
	l := FishingGuild()
	return tile.Chebyshev(t, l.Bank) <= l.BankRadius
}

// OnGuildMainland's box is the ground-floor stretch of Kandarin that one walk crosses between
// Catherby and the Fishing Guild: Catherby, Camelot, Seers' Village, Hemenster and the Guild
// itself, with no ship or dungeon in between. Only here does a Guild account walk straight to
// the Guild; anywhere else (Karamja, Lumbridge after a death, a fail-teleport pocket) it
// recovers to Catherby first by the existing, proven route and walks on from there.
const (
	guildMainlandMinX, guildMainlandMaxX = 2560, 2880
	guildMainlandMinZ, guildMainlandMaxZ = 3380, 3520
)

// OnGuildMainland reports whether t is on the ground floor inside GuildMainland.
func OnGuildMainland(t tile.Tile) bool {
	return t.Level == 0 && t.X >= guildMainlandMinX && t.X <= guildMainlandMaxX &&
		t.Z >= guildMainlandMinZ && t.Z <= guildMainlandMaxZ
}

// InHome reports whether s stands where its ordinary bank, buy, sell and fish phases run:
// Catherby always (the tool shops are there), and the Fishing Guild too while the snapshot
// has the Guild selected. With Guild false this is exactly InCatherby.
func InHome(s FishSnapshot) bool {
	if s.Tile == nil {
		return false
	}
	return InCatherby(*s.Tile) || (s.Guild && InFishingGuild(*s.Tile))
}

// NearerGuild reports whether t is closer to the Guild bank than to the Catherby bank. A Guild
// account measures drift and flees relative to whichever of its two homes it is nearer: the
// banks are 223 tiles apart, beyond the 150-tile drift limit, so one fixed reference would
// call the other home "drifted".
func NearerGuild(t tile.Tile) bool {
	return tile.Chebyshev(t, FishingGuild().Bank) < tile.Chebyshev(t, Catherby().Bank)
}

// GuildToolSpawn is one fishing tool lying on the ground just inside the Guild's entrance door
// (Content maps/m40_53.jm2, OBJ section, level 0). The Big net and Harpoon sit on tables, so
// their tiles are not walkable and they are taken from beside them.
type GuildToolSpawn struct {
	Tool  string
	ObjID int
	At    tile.Tile
}

// GuildToolSpawns are the Guild's tool spawns, one per tool the Guild's rungs use. They respawn
// after the engine's default obj respawnrate (100 ticks) once taken.
var GuildToolSpawns = []GuildToolSpawn{
	{Tool: "Big fishing net", ObjID: 305, At: tile.Tile{X: 2605, Z: 3395}},
	{Tool: "Harpoon", ObjID: 311, At: tile.Tile{X: 2605, Z: 3396}},
	{Tool: "Lobster pot", ObjID: 301, At: tile.Tile{X: 2608, Z: 3397}},
}

// GuildSpawnFor is the spawn of tool, if the Guild has one.
func GuildSpawnFor(tool string) (GuildToolSpawn, bool) {
	for _, s := range GuildToolSpawns {
		if strings.EqualFold(s.Tool, tool) {
			return s, true
		}
	}
	return GuildToolSpawn{}, false
}

// GuildToolWanted reports whether the rung the level has earned is one the Guild fishes and
// whose tool the Guild spawns, while that tool is owned nowhere: the account can take it from
// the spawn instead of buying it in Catherby. bank nil (never read) answers false, so a bank
// read that may find the tool always comes first.
func GuildToolWanted(level int, target string, held, bank []string) (GuildToolSpawn, bool) {
	if level < FishingGuildEntryLevel || bank == nil {
		return GuildToolSpawn{}, false
	}
	rung := CurrentRung(level, target)
	if !SupportsGuildRung(rung) || hasName(held, rung.Tool) || hasName(bank, rung.Tool) {
		return GuildToolSpawn{}, false
	}
	return GuildSpawnFor(rung.Tool)
}

// GuildToolRecoverable is GuildToolWanted before the bank has been read: the earned rung is a
// Guild rung with a spawn and the tool is not in the pack. It lets the Guild be selected (so the
// bank read happens at the Guild bank, beside the spawn) for an account that has lost its tool.
func GuildToolRecoverable(level int, target string, held, bank []string) bool {
	if level < FishingGuildEntryLevel {
		return false
	}
	rung := CurrentRung(level, target)
	if !SupportsGuildRung(rung) || hasName(held, rung.Tool) || (bank != nil && hasName(bank, rung.Tool)) {
		return false
	}
	_, ok := GuildSpawnFor(rung.Tool)
	return ok
}
