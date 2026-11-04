// Package thieverplan is ProgressiveThiever's pure half, ported from rsb0t-scripts'
// thievePlan.ts: every way this server lets you steal, the schedule of which one to use at
// which level, and which phase of the run the account is in. No client, actions, fleet or bot
// imports and no I/O.
//
// This replaces an earlier, honestly-scoped stand-in that lived in this file: at the time it
// was written, ProgressiveThiever.ts/thievePlan.ts were not present anywhere this project's
// checkouts could find (its own doc comment explained exactly where it had looked). They have
// since turned up in the sibling rsb0t-scripts checkout the account owner pointed at
// directly, so this is now a real, source-faithful port rather than the earlier substitute.
//
// Three things from thievePlan.ts's own doc comment are worth knowing before reading the
// table:
//
// The opening is free. A failed pickpocket on a Man does one point of damage, and the
// Lumbridge respawn (3221,3218) is one tile from the courtyard where the Men wander
// (3222,3218) — freshstart.PickpocketSpot. So an account with no food does not need any: it
// steals until it dies, stands up where it fell, and carries on. There is no Lumbridge bank on
// this server, so the coins go with each death; the boat fare is raised afterwards, in a phase
// where nothing dies.
//
// Stalls are not the foodless option they look like: a steal cannot fail, but a listed guard
// within five tiles with line of sight retaliates with real combat.
//
// Chests are a money rung and never an experience one — 0.5 to 0.8 experience a tick across the
// whole family, against 2 for the Baker's stall and many times that for a high pickpocket. They
// are in the table because they are in the game; only a schedule that names one will work one.
//
// Deliberately not ported here, and why:
//   - thieveHud.ts — the Electron wall's on-screen overlay; no headless equivalent, the same
//     boundary drawn around every other ported script's own HUD file.
//   - the update-checker and the bank/tally feed publishers.
//   - asking the mule for food when the bank has none (ProgressiveThiever.ts's own
//     askMuleForFood/orderStep, driven by muleOrder.ts/orderPlan.ts over the wall's channel).
//     See internal/scripts/thieverrunner's own package doc comment for exactly why: the
//     transport exists (internal/mulebus) but MuleReceiver does not fulfil item orders yet, so
//     claiming that round-trip "proven" without a live MuleReceiver actually answering it would
//     be exactly the overclaiming docs/script_porting_framework.md's own "what done looks
//     like" table warns against.
package thieverplan

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/tile"
)

// ---- the shape of a method ----

// MethodKind is which family a Method belongs to.
type MethodKind string

// The method kinds.
const (
	KindPickpocket MethodKind = "pickpocket"
	KindStall      MethodKind = "stall"
	KindChest      MethodKind = "chest"
)

// Region is which side of the two ships a method lives on. The boats are only for the far
// side.
type Region string

// The two regions.
const (
	RegionMainland Region = "mainland"
	RegionArdougne Region = "ardougne"
)

// Method is one way this server lets you steal — a pickpocket target, a stall, or a chest.
// Ported as one flat struct (rather than a discriminated union, which Go has no direct
// equivalent for) with Kind-specific fields documented as such; a field left at its zero value
// is meaningless for that Kind, matching the TS union's own per-variant field lists exactly.
type Method struct {
	// Label is the name the schedule, the log and the panel all use. Unique across METHODS.
	Label string
	Kind  MethodKind
	// Level is the Thieving level the method opens at — the server's own number.
	Level int
	// XP is Thieving experience for one success.
	XP float64
	// Anchor is where the method is worked, and Leash how far from there a target may be and
	// still be ours.
	Anchor tile.Tile
	Leash  int
	Region Region
	// Bank is the bank that serves this method: food in, loot out.
	Bank tile.Tile
	// Hurts is true when the method can hurt you, so food is wanted before it starts.
	Hurts bool
	// Verified is false while nobody has stood on Anchor in-game — a guess, not a promise.
	Verified bool
	// Gate is a quest the server enforces before this works at all; "" when there is none.
	Gate string
	// Loot is what the method yields, for the mule's keep list.
	Loot []string
	// Note is one line of warning, logged when the rung is entered.
	Note string

	// ---- pickpocket only ----

	// NPC is the name the query matches.
	NPC string
	// AlsoNPC are other names the same rung accepts (Woman beside Man, the Fremennik's ten
	// proper names).
	AlsoNPC []string
	// SuccessLow/SuccessHigh are the server's stat_random roll out of 256: the chance at level
	// 1 and at 99.
	SuccessLow, SuccessHigh int
	StunTicks               int
	// StunDamage is damage one failure does — one, on a Man, is what makes the suicide farm
	// free.
	StunDamage int
	// RespawnsOnTop is true only for the courtyard Men: the Lumbridge respawn tile is this
	// Anchor, the whole reason the suicide phase exists.
	RespawnsOnTop bool

	// ---- stall only ----

	// Loc is the loc's display name, and Op its interaction — Ardougne's stalls are "Steal
	// from"; the Varrock tea stall is "Steal-from", hyphenated.
	Loc string
	Op  string
	// RespawnTicks are ticks before the stall itself restocks — the only rate limit; there is
	// no success roll.
	RespawnTicks int
	// Stands are where to stand: beside the stall and out of the merchant's line of sight.
	Stands []tile.Tile
	// Flee is where to walk when a guard is roused.
	Flee tile.Tile
	// Guards are NPCs that catch you at it and fight back.
	Guards []string

	// ---- chest only ----

	// Tile is the chest's own tile — every trapped chest on this server is called "Chest", so
	// the tile is the identity.
	Tile tile.Tile
	// LandsAt is where the chest throws you when it opens; nil when it leaves you alone.
	LandsAt *tile.Tile
	// NeedsLockpick is true for the one chest this script never buys a lockpick for.
	NeedsLockpick bool
}

// ---- the places ----

// Courtyard is the pickpocket spot, one tile from the Lumbridge respawn — shared with the
// fresh-start road.
var Courtyard = freshstart.PickpocketSpot

// ArdougneBank is East Ardougne, beside Captain Barnaby's pier.
var ArdougneBank = tile.Tile{X: 2655, Z: 3283}

// ArdougneMarket is the middle of the East Ardougne market: stalls, guards and knights are all
// within a leash.
var ArdougneMarket = tile.Tile{X: 2661, Z: 3306}

// ArdougneFlee is far enough from the market that a roused guard loses interest.
var ArdougneFlee = tile.Tile{X: 2655, Z: 3298}

var VarrockEastBank = tile.Tile{X: 3253, Z: 3420}
var YanilleBank = tile.Tile{X: 2612, Z: 3092}
var GrandTreeBank = tile.Tile{X: 2449, Z: 3482, Level: 1}

// BoothName/BoothOp identify a bank booth loc.
const (
	BoothName = "Bank booth"
	BoothOp   = "Use-quickly"
)

var ardougneGuards = []string{"Guard", "Knight of Ardougne", "Paladin"}

// The interactions every kind uses.
const (
	PickpocketOp = "Pickpocket"
	StallOp      = "Steal from"
	ChestOp      = "Search for traps"
	ChestLoc     = "Chest"
)

// AttemptTicks: an attempt that lands costs about this many ticks, which the rate estimates use.
const AttemptTicks = 3

func landsAt(x, z, level int) *tile.Tile {
	t := tile.Tile{X: x, Z: z, Level: level}
	return &t
}

// METHODS is the whole table, ported verbatim from thievePlan.ts's own METHODS: eleven
// pickpocket targets, seven stalls, six chests.
var METHODS = []Method{
	// ---- pickpocketing ----
	{
		Label: "Man", Kind: KindPickpocket, Level: 1, XP: 8,
		NPC: "Man", AlsoNPC: []string{"Woman"}, SuccessLow: 180, SuccessHigh: 240, StunTicks: 8, StunDamage: 1,
		RespawnsOnTop: true,
		Anchor:        Courtyard, Leash: freshstart.PickpocketLeash, Region: RegionMainland, Bank: VarrockEastBank,
		Hurts: true, Verified: true, Loot: []string{freshstart.Coins},
		Note: "seven Men and two Women wander inside the leash, and the respawn is one tile away",
	},
	{
		Label: "Farmer", Kind: KindPickpocket, Level: 10, XP: 14.5,
		NPC: "Farmer", SuccessLow: 150, SuccessHigh: 240, StunTicks: 8, StunDamage: 1, RespawnsOnTop: false,
		Anchor: tile.Tile{X: 3227, Z: 3290}, Leash: 14, Region: RegionMainland, Bank: VarrockEastBank,
		Hurts: true, Loot: []string{freshstart.Coins},
		Note: "the farm is seventy tiles north of the respawn, so here a death costs a walk",
	},
	{
		Label: "Warrior woman", Kind: KindPickpocket, Level: 25, XP: 26,
		NPC: "Warrior woman", AlsoNPC: []string{"Al-Kharid warrior"}, SuccessLow: 100, SuccessHigh: 240,
		StunTicks: 8, StunDamage: 2, RespawnsOnTop: false,
		Anchor: tile.Tile{X: 2630, Z: 3297}, Leash: 10, Region: RegionArdougne, Bank: ArdougneBank,
		Hurts: true, Loot: []string{freshstart.Coins},
	},
	{
		Label: "Rogue", Kind: KindPickpocket, Level: 32, XP: 36.5,
		NPC: "Rogue", SuccessLow: 74, SuccessHigh: 240, StunTicks: 8, StunDamage: 2, RespawnsOnTop: false,
		Anchor: tile.Tile{X: 3285, Z: 3931}, Leash: 14, Region: RegionMainland, Bank: VarrockEastBank,
		Hurts: true, Loot: []string{freshstart.Coins, "Air rune", "Lockpick", "Iron dagger(p)", "Jug of wine"},
		Note: "every Rogue on this server spawns in the Wilderness, where other players will kill you",
	},
	{
		Label: "Guard", Kind: KindPickpocket, Level: 40, XP: 46.8,
		NPC: "Guard", SuccessLow: 50, SuccessHigh: 240, StunTicks: 8, StunDamage: 2, RespawnsOnTop: false,
		Anchor: ArdougneMarket, Leash: 19, Region: RegionArdougne, Bank: ArdougneBank,
		Hurts: true, Verified: true, Loot: []string{freshstart.Coins},
	},
	{
		Label: "Fremennik", Kind: KindPickpocket, Level: 45, XP: 65,
		NPC:        "Agnar",
		AlsoNPC:    []string{"Freidir", "Borrokar", "Lanzig", "Pontak", "Freygerd", "Lensa", "Jennella", "Sassilik", "Inga"},
		SuccessLow: 40, SuccessHigh: 240, StunTicks: 8, StunDamage: 2, RespawnsOnTop: false,
		Anchor: tile.Tile{X: 2658, Z: 3660}, Leash: 20, Region: RegionArdougne, Bank: ArdougneBank,
		Hurts: true, Gate: "The Fremennik Trials", Loot: []string{freshstart.Coins},
		Note: "ten separately named villagers, and every one refuses you until the Trials are done",
	},
	{
		Label: "Knight of Ardougne", Kind: KindPickpocket, Level: 55, XP: 84.3,
		NPC: "Knight of Ardougne", SuccessLow: 50, SuccessHigh: 240, StunTicks: 8, StunDamage: 3, RespawnsOnTop: false,
		Anchor: ArdougneMarket, Leash: 29, Region: RegionArdougne, Bank: ArdougneBank,
		Hurts: true, Verified: true, Loot: []string{freshstart.Coins},
		Note: "only five exist on the whole map; four of them are inside this leash",
	},
	{
		Label: "Watchman", Kind: KindPickpocket, Level: 65, XP: 137.5,
		NPC: "Watchman", SuccessLow: 15, SuccessHigh: 160, StunTicks: 8, StunDamage: 3, RespawnsOnTop: false,
		Anchor: tile.Tile{X: 2547, Z: 3114, Level: 1}, Leash: 10, Region: RegionArdougne, Bank: YanilleBank,
		Hurts: true, Loot: []string{freshstart.Coins, "Bread"},
		Note: "upstairs in the Yanille watchtower, so the walk in is a ladder",
	},
	{
		Label: "Paladin", Kind: KindPickpocket, Level: 70, XP: 151.8,
		NPC: "Paladin", SuccessLow: 50, SuccessHigh: 150, StunTicks: 8, StunDamage: 3, RespawnsOnTop: false,
		Anchor: tile.Tile{X: 2580, Z: 3297}, Leash: 20, Region: RegionArdougne, Bank: ArdougneBank,
		Hurts: true, Loot: []string{freshstart.Coins, "Chaos rune"},
		Note: "twenty of them in Ardougne castle, over both floors; the two in the market are not worth the walk",
	},
	{
		Label: "Gnome", Kind: KindPickpocket, Level: 75, XP: 198.3,
		NPC: "Gnome", AlsoNPC: []string{"Gnome child", "Gnome woman"}, SuccessLow: 8, SuccessHigh: 120,
		StunTicks: 8, StunDamage: 1, RespawnsOnTop: false,
		Anchor: tile.Tile{X: 2445, Z: 3430}, Leash: 25, Region: RegionArdougne, Bank: GrandTreeBank,
		Hurts: true, Loot: []string{"King worm", freshstart.Coins, "Swamp toad", "Gold ore", "Earth rune", "Fire orb"},
		Note: "forty-four of them, and a failure costs one point rather than three",
	},
	{
		Label: "Hero", Kind: KindPickpocket, Level: 80, XP: 273.3,
		NPC: "Hero", SuccessLow: 6, SuccessHigh: 100, StunTicks: 10, StunDamage: 4, RespawnsOnTop: false,
		Anchor: tile.Tile{X: 2657, Z: 3311}, Leash: 17, Region: RegionArdougne, Bank: ArdougneBank,
		Hurts: true, Verified: true,
		Loot: []string{freshstart.Coins, "Death rune", "Blood rune", "Jug of wine", "Fire orb", "Diamond", "Gold ore"},
		Note: "the best rate on paper and three spawns in the world, so it waits more than it steals",
	},

	// ---- stalls: no success roll, but the guards are real ----
	{
		Label: "Baker's stall", Kind: KindStall, Level: 5, XP: 16, Loc: "Baker's stall", Op: StallOp,
		RespawnTicks: 8,
		Anchor:       tile.Tile{X: 2667, Z: 3310}, Leash: 6, Region: RegionArdougne, Bank: ArdougneBank,
		Stands: []tile.Tile{{X: 2668, Z: 3312}, {X: 2669, Z: 3310}},
		Flee:   ArdougneFlee, Guards: []string{"Guard"},
		Hurts: true, Verified: true, Loot: []string{"Cake", "Bread", "Chocolate slice"},
		Note: "the loot is food, so this rung feeds itself",
	},
	{
		Label: "Tea stall", Kind: KindStall, Level: 5, XP: 16, Loc: "Tea stall", Op: "Steal-from",
		RespawnTicks: 16,
		Anchor:       tile.Tile{X: 3269, Z: 3410}, Leash: 6, Region: RegionMainland, Bank: VarrockEastBank,
		Stands: []tile.Tile{{X: 3271, Z: 3411}}, Flee: tile.Tile{X: 3280, Z: 3400}, Guards: nil,
		Hurts: false, Loot: []string{"Cup of tea"},
		Note: "the one stall whose op is hyphenated, and the only one on the Lumbridge side",
	},
	{
		Label: "Silk stall", Kind: KindStall, Level: 20, XP: 24, Loc: "Silk stall", Op: StallOp,
		RespawnTicks: 16,
		Anchor:       tile.Tile{X: 2656, Z: 3302}, Leash: 6, Region: RegionArdougne, Bank: ArdougneBank,
		Stands: []tile.Tile{{X: 2655, Z: 3303}}, Flee: ArdougneFlee, Guards: ardougneGuards,
		Hurts: true, Loot: []string{"Silk"},
	},
	{
		Label: "Fur stall", Kind: KindStall, Level: 35, XP: 36, Loc: "Fur stall", Op: StallOp,
		RespawnTicks: 32,
		Anchor:       tile.Tile{X: 2663, Z: 3296}, Leash: 6, Region: RegionArdougne, Bank: ArdougneBank,
		Stands: []tile.Tile{{X: 2662, Z: 3295}}, Flee: ArdougneFlee, Guards: ardougneGuards,
		Hurts: true, Loot: []string{"Grey wolf fur"},
	},
	{
		Label: "Silver stall", Kind: KindStall, Level: 50, XP: 54, Loc: "Silver stall", Op: StallOp,
		RespawnTicks: 100,
		Anchor:       tile.Tile{X: 2657, Z: 3314}, Leash: 6, Region: RegionArdougne, Bank: ArdougneBank,
		Stands: []tile.Tile{{X: 2656, Z: 3315}}, Flee: ArdougneFlee, Guards: ardougneGuards,
		Hurts: true, Loot: []string{"Silver ore"},
	},
	{
		Label: "Spice stall", Kind: KindStall, Level: 65, XP: 81, Loc: "Spice stall", Op: StallOp,
		RespawnTicks: 166,
		Anchor:       tile.Tile{X: 2658, Z: 3297}, Leash: 6, Region: RegionArdougne, Bank: ArdougneBank,
		Stands: []tile.Tile{{X: 2657, Z: 3296}}, Flee: ArdougneFlee, Guards: ardougneGuards,
		Hurts: true, Loot: []string{"Spice"},
	},
	{
		Label: "Gem stall", Kind: KindStall, Level: 75, XP: 16, Loc: "Gem stall", Op: StallOp,
		RespawnTicks: 600,
		Anchor:       tile.Tile{X: 2667, Z: 3303}, Leash: 6, Region: RegionArdougne, Bank: ArdougneBank,
		Stands: []tile.Tile{{X: 2668, Z: 3304}}, Flee: ArdougneFlee, Guards: ardougneGuards,
		Hurts: true, Loot: []string{"Uncut sapphire", "Uncut emerald", "Uncut ruby", "Uncut diamond"},
		Note: "the server cuts this to 16 experience on purpose — it is gems, not levels",
	},

	// ---- chests: found by tile, because every one of them is called Chest ----
	{
		Label: "Chest (10 coins)", Kind: KindChest, Level: 13, XP: 7.8, Loc: ChestLoc, Op: ChestOp,
		RespawnTicks: 15, NeedsLockpick: false,
		Tile: tile.Tile{X: 2673, Z: 3307}, Anchor: tile.Tile{X: 2673, Z: 3307}, Leash: 3,
		Region: RegionArdougne, Bank: ArdougneBank,
		Loot: []string{freshstart.Coins},
	},
	{
		Label: "Chest (nature rune)", Kind: KindChest, Level: 28, XP: 25, Loc: ChestLoc, Op: ChestOp,
		RespawnTicks: 30, NeedsLockpick: false,
		Tile: tile.Tile{X: 2671, Z: 3301, Level: 1}, Anchor: tile.Tile{X: 2671, Z: 3301, Level: 1}, Leash: 3,
		Region: RegionArdougne, Bank: ArdougneBank,
		Loot: []string{"Nature rune", freshstart.Coins},
	},
	{
		Label: "Chest (50 coins)", Kind: KindChest, Level: 43, XP: 125, Loc: ChestLoc, Op: ChestOp,
		RespawnTicks: 150, NeedsLockpick: false,
		Tile: tile.Tile{X: 2671, Z: 3299, Level: 1}, Anchor: tile.Tile{X: 2671, Z: 3299, Level: 1}, Leash: 3,
		Region: RegionArdougne, Bank: ArdougneBank,
		Loot: []string{freshstart.Coins},
	},
	{
		Label: "Chest (steel arrowtips)", Kind: KindChest, Level: 47, XP: 150, Loc: ChestLoc, Op: ChestOp,
		RespawnTicks: 250, NeedsLockpick: true,
		Tile: tile.Tile{X: 2639, Z: 3424}, Anchor: tile.Tile{X: 2639, Z: 3424}, Leash: 3,
		Region: RegionArdougne, Bank: ArdougneBank,
		Loot: []string{"Steel arrowtips", freshstart.Coins},
		Note: "a lockpick has to be in the pack already; this script never buys one",
	},
	{
		Label: "Chest (blood runes)", Kind: KindChest, Level: 59, XP: 250, Loc: ChestLoc, Op: ChestOp,
		RespawnTicks: 400, LandsAt: landsAt(2584, 3337, 0), NeedsLockpick: false,
		Tile: tile.Tile{X: 2586, Z: 9734}, Anchor: tile.Tile{X: 2586, Z: 9734}, Leash: 3,
		Region: RegionArdougne, Bank: ArdougneBank,
		Loot: []string{"Blood rune", freshstart.Coins},
		Note: "it throws you to (2584,3337) when it opens, and the walk back underground is long",
	},
	{
		Label: "Chest (Ardougne castle)", Kind: KindChest, Level: 72, XP: 500, Loc: ChestLoc, Op: ChestOp,
		RespawnTicks: 800, LandsAt: landsAt(2696, 3281, 0), NeedsLockpick: false,
		Tile: tile.Tile{X: 2588, Z: 3302, Level: 1}, Anchor: tile.Tile{X: 2588, Z: 3302, Level: 1}, Leash: 3,
		Region: RegionArdougne, Bank: ArdougneBank,
		Loot: []string{freshstart.Coins, "Raw shark", "Adamantite ore", "Uncut sapphire"},
		Note: "it throws you to (2696,3281) when it opens",
	},
}

// METHOD_LABELS are every method's own label, in table order.
var METHOD_LABELS = methodLabels()

func methodLabels() []string {
	out := make([]string, len(METHODS))
	for i, m := range METHODS {
		out[i] = m.Label
	}
	return out
}

// XPPerTick is experience a tick of m is worth at best: a pickpocket's XP over AttemptTicks, a
// stall/chest's XP over its own respawn ticks.
func XPPerTick(m Method) float64 {
	if m.Kind == KindPickpocket {
		return m.XP / AttemptTicks
	}
	return m.XP / float64(m.RespawnTicks)
}

// NPCNames are the names a pickpocket rung will click, its alternates included.
func NPCNames(m Method) []string {
	return append([]string{m.NPC}, m.AlsoNPC...)
}

var tidyStrip = regexp.MustCompile(`[^a-z0-9 ]`)
var tidySpace = regexp.MustCompile(`\s+`)

func tidy(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = tidyStrip.ReplaceAllString(s, "")
	s = tidySpace.ReplaceAllString(s, " ")
	return s
}

// MethodByLabel finds a method by its label, matched loosely (case/punctuation-insensitive).
func MethodByLabel(label string) (Method, bool) {
	want := tidy(label)
	for _, m := range METHODS {
		if tidy(m.Label) == want {
			return m, true
		}
	}
	return Method{}, false
}

// ---- the schedule ----

// aliases are what people call these things when they are not reading the table.
var aliases = map[string]string{
	"men": "Man", "woman": "Man", "women": "Man",
	"cake stall": "Baker's stall", "cakes": "Baker's stall", "bakers stall": "Baker's stall", "bakery stall": "Baker's stall",
	"knight": "Knight of Ardougne", "knights": "Knight of Ardougne", "ardougne knight": "Knight of Ardougne",
	"guards": "Guard", "paladins": "Paladin", "heroes": "Hero", "gnomes": "Gnome",
	"viking": "Fremennik", "vikings": "Fremennik",
	"al kharid warrior": "Warrior woman", "alkharid warrior": "Warrior woman", "warrior": "Warrior woman",
	"watchmen":   "Watchman",
	"coin chest": "Chest (10 coins)", "10 coin chest": "Chest (10 coins)",
	"nature chest": "Chest (nature rune)", "nature rune chest": "Chest (nature rune)",
	"50 coin chest": "Chest (50 coins)",
	"blood chest":   "Chest (blood runes)", "blood rune chest": "Chest (blood runes)",
	"castle chest": "Chest (Ardougne castle)", "arrowtip chest": "Chest (steel arrowtips)",
}

// DefaultSchedule keeps the courtyard Men until 25 rather than moving to a stall at 5, because
// the opening is the only part of the run that costs nothing at all. Farmer at 10 is skipped on
// purpose: nearly twice the experience, but every death there costs a walk.
const DefaultSchedule = "1: Man, 25: Warrior woman, 40: Guard, 55: Knight of Ardougne, 70: Paladin, 80: Hero"

// Rung is one line of a Schedule: the level it starts at, and the method worked from there.
type Rung struct {
	Level  int
	Method Method
}

// Schedule is a parsed method schedule.
type Schedule struct {
	Rungs []Rung
	// Rejected are segments that could not be read at all.
	Rejected []string
	// Raised are rungs whose level was raised to what the game actually requires, as "1
	// Paladin -> 70".
	Raised []string
	// FellBack is true when nothing could be read and DefaultSchedule was used instead.
	FellBack bool
}

var segmentPattern = regexp.MustCompile(`^(?:(\d{1,2})\s*[:=]?\s*)?(.+)$`)
var splitPattern = regexp.MustCompile(`(?i)\s*,\s*|\s+and\s+`)

type readSegment struct {
	level  int
	hasLvl bool
	method Method
}

func readOneSegment(raw string) (readSegment, bool) {
	m := segmentPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return readSegment{}, false
	}
	name := tidy(m[2])
	if name == "" {
		return readSegment{}, false
	}
	method, ok := MethodByLabel(name)
	if !ok {
		if alias, aliasOK := aliases[name]; aliasOK {
			method, ok = MethodByLabel(alias)
		}
	}
	if !ok {
		return readSegment{}, false
	}
	if m[1] == "" {
		return readSegment{method: method}, true
	}
	lvl := 0
	for _, c := range m[1] {
		lvl = lvl*10 + int(c-'0')
	}
	return readSegment{level: lvl, hasLvl: true, method: method}, true
}

// ParseSchedule reads a schedule string, ported 1:1 from thievePlan.ts's own parseSchedule.
// Forgiving about the input (commas or " and ", any spacing, a colon or nothing, a bare method
// name meaning "from the level it opens at", and the aliases above); decided about the output:
// an unreadable segment is dropped and recorded, a level below what the game requires is raised
// and recorded, a schedule that starts above level 1 gets Man prepended, and a schedule that
// reads as nothing at all falls back to DefaultSchedule.
func ParseSchedule(raw string) Schedule {
	var rejected, raised []string
	byLabel := map[string]Rung{}
	var order []string
	for _, segment := range splitPattern.Split(raw, -1) {
		if strings.TrimSpace(segment) == "" {
			continue
		}
		read, ok := readOneSegment(segment)
		if !ok {
			rejected = append(rejected, strings.TrimSpace(segment))
			continue
		}
		asked := read.method.Level
		if read.hasLvl {
			asked = read.level
		}
		level := asked
		if level < 1 {
			level = 1
		}
		if level < read.method.Level {
			level = read.method.Level
		}
		if level > 99 {
			level = 99
		}
		if read.hasLvl && level != asked {
			raised = append(raised, formatRaised(asked, read.method.Label, level))
		}
		if _, seen := byLabel[read.method.Label]; !seen {
			order = append(order, read.method.Label)
		}
		byLabel[read.method.Label] = Rung{Level: level, Method: read.method}
	}
	if len(byLabel) == 0 {
		if strings.TrimSpace(raw) == DefaultSchedule {
			return Schedule{Rejected: rejected, Raised: raised, FellBack: true}
		}
		fallback := ParseSchedule(DefaultSchedule)
		return Schedule{Rungs: fallback.Rungs, Rejected: rejected, Raised: raised, FellBack: true}
	}
	rungs := make([]Rung, 0, len(order))
	for _, label := range order {
		rungs = append(rungs, byLabel[label])
	}
	sortRungs(rungs)
	if len(rungs) == 0 || rungs[0].Level > 1 {
		if man, ok := MethodByLabel("Man"); ok {
			has := false
			for _, r := range rungs {
				if r.Method.Label == man.Label {
					has = true
					break
				}
			}
			if !has {
				rungs = append([]Rung{{Level: 1, Method: man}}, rungs...)
			}
		}
	}
	return Schedule{Rungs: rungs, Rejected: rejected, Raised: raised}
}

func formatRaised(asked int, label string, level int) string {
	return itoa(asked) + " " + label + " -> " + itoa(level)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func sortRungs(rungs []Rung) {
	// Simple insertion sort: schedules are tiny (well under two dozen rungs).
	for i := 1; i < len(rungs); i++ {
		j := i
		for j > 0 && rungLess(rungs[j], rungs[j-1]) {
			rungs[j], rungs[j-1] = rungs[j-1], rungs[j]
			j--
		}
	}
}

func rungLess(a, b Rung) bool {
	if a.Level != b.Level {
		return a.Level < b.Level
	}
	return a.Method.Level < b.Method.Level
}

// MethodFor is the rung the level has earned: the last one at or below it, and never nothing.
func MethodFor(level int, s Schedule) Method {
	if len(s.Rungs) == 0 {
		return METHODS[0]
	}
	at := s.Rungs[0].Method
	for _, r := range s.Rungs {
		if level >= r.Level {
			at = r.Method
		}
	}
	return at
}

// NextRung is the rung after the one being worked, or nil at the top of the schedule.
func NextRung(level int, s Schedule) *Rung {
	for i := range s.Rungs {
		if s.Rungs[i].Level > level {
			r := s.Rungs[i]
			return &r
		}
	}
	return nil
}

// RungState is one schedule row's state for the panel/log.
type RungState string

// The states.
const (
	RungDone  RungState = "done"
	RungNow   RungState = "now"
	RungNext  RungState = "next"
	RungLater RungState = "later"
)

// ScheduleRow is one row of ScheduleView.
type ScheduleRow struct {
	Label    string
	Level    int
	Kind     MethodKind
	State    RungState
	Verified bool
}

// ScheduleView is the schedule as the panel/log would list it.
func ScheduleView(level int, s Schedule) []ScheduleRow {
	now := MethodFor(level, s)
	after := NextRung(level, s)
	out := make([]ScheduleRow, 0, len(s.Rungs))
	for _, r := range s.Rungs {
		state := RungLater
		switch {
		case r.Method.Label == now.Label:
			state = RungNow
		case after != nil && r.Method.Label == after.Method.Label:
			state = RungNext
		case r.Level < now.Level:
			state = RungDone
		}
		out = append(out, ScheduleRow{Label: r.Method.Label, Level: r.Level, Kind: r.Method.Kind, State: state, Verified: r.Method.Verified})
	}
	return out
}

// DescribeSchedule is a one-line "1 Man -> 25 Warrior woman -> ..." summary.
func DescribeSchedule(s Schedule) string {
	parts := make([]string, len(s.Rungs))
	for i, r := range s.Rungs {
		parts[i] = itoa(r.Level) + " " + r.Method.Label
	}
	return strings.Join(parts, " → ")
}

// ---- the phase ----

// ThievePhase is one step of the run.
type ThievePhase string

// The phases, ported 1:1 from thievePlan.ts's own ThievePhase union.
const (
	PhaseAccept   ThievePhase = "accept"
	PhaseTutorial ThievePhase = "tutorial"
	PhaseBright   ThievePhase = "bright"
	PhaseWait     ThievePhase = "wait"
	PhaseSuicide  ThievePhase = "suicide"
	PhaseFund     ThievePhase = "fund"
	PhaseTravel   ThievePhase = "travel"
	PhaseMove     ThievePhase = "move"
	PhaseBank     ThievePhase = "bank"
	PhaseOrder    ThievePhase = "order"
	PhaseEat      ThievePhase = "eat"
	PhaseWork     ThievePhase = "work"
	PhaseDead     ThievePhase = "dead"
	PhaseStuck    ThievePhase = "stuck"
)

// ThieveSnapshot is the live state DecideThievePhase reads, ported 1:1 from ThieveSnapshot.
type ThieveSnapshot struct {
	MainModal int
	// Tile is nil while the position is not known yet.
	Tile     *tile.Tile
	Level    int
	Schedule Schedule
	// HP/MaxHP are hitpoints now and at full; EatAt is the share at which a bite is taken.
	HP, MaxHP int
	EatAt     float64
	// Heal is what one bite of the chosen food heals.
	Heal int
	// Food is edible food in the pack — notes do not count.
	Food int
	// MinFood is food to have in hand before a rung that hurts is worked.
	MinFood int
	// BankFood is food the bank held at the last look; nil until the bank has been read.
	BankFood *int
	// NotedFood is noted food in the pack (the mule's delivery), which the bank turns back.
	NotedFood int
	// HasMule is whether a mule is named at all.
	HasMule  bool
	PackFull bool
	// Loot is pack slots holding loot that belongs in the bank.
	Loot   int
	BankAt int
	Coins  int
	Fare   int
	// Died is true from the tick a death line was seen until the phase deals with it.
	Died bool
	// SuicideAllowed is the setting.
	SuicideAllowed bool
	// Bright is true once the brightness has been turned up (or given up on).
	Bright bool
}

// AtMethod reports whether t is inside the patch of ground m is worked on.
func AtMethod(t tile.Tile, m Method) bool {
	return t.Level == m.Anchor.Level && tile.Chebyshev(t, m.Anchor) <= m.Leash
}

// ChooseTarget picks who to pickpocket: a visible NPC whose name matches one of names, with a
// Pickpocket op, on spot's own level within leash tiles of it — adjacent targets first, then
// nearest. Unlike freshstart.ChooseTarget (a single name and a fixed leash, built for the
// fresh-account fare funder) this takes a pickpocket method's own name list (Fremennik alone
// answers to ten) and its own leash, since every rung in METHODS has a different one. Returns
// -1 for none in reach.
func ChooseTarget(npcs []freshstart.NPC, names []string, spot tile.Tile, leash int) int {
	matches := func(n string) bool {
		for _, want := range names {
			if strings.EqualFold(strings.TrimSpace(n), strings.TrimSpace(want)) {
				return true
			}
		}
		return false
	}
	var idx []int
	for i, n := range npcs {
		if matches(n.Name) && freshstart.HasOp(n.Ops, PickpocketOp) && n.Tile.Level == spot.Level && tile.Chebyshev(n.Tile, spot) <= leash {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return -1
	}
	sort.SliceStable(idx, func(a, b int) bool {
		na, nb := npcs[idx[a]], npcs[idx[b]]
		if fa, fb := na.Distance > 1, nb.Distance > 1; fa != fb {
			return !fa
		}
		return na.Distance < nb.Distance
	})
	return idx[0]
}

// SuicideNow reports whether the account should be suicide-farming right now: a low account
// with no food, standing in Lumbridge, on a rung whose respawn IS the spot. Not a mode that
// gets switched on — derived, and it stops being true by itself the moment there is food, the
// account leaves Lumbridge, or the schedule has moved past the courtyard Men.
func SuicideNow(s ThieveSnapshot, m Method) bool {
	return s.SuicideAllowed &&
		m.Kind == KindPickpocket && m.RespawnsOnTop &&
		s.Food == 0 && s.NotedFood == 0 &&
		s.Tile != nil && freshstart.InLumbridge(*s.Tile)
}

func wantsFood(s ThieveSnapshot, m Method) bool {
	return m.Hurts && s.Food < s.MinFood
}

// DecideThievePhase picks the next phase from live state alone, ported 1:1 from
// decideThievePhase.
func DecideThievePhase(s ThieveSnapshot) ThievePhase {
	if s.MainModal == freshstart.DesignModal {
		return PhaseAccept
	}
	if s.Tile == nil {
		return PhaseWait
	}
	if freshstart.InTutorialIsland(*s.Tile) {
		return PhaseTutorial
	}
	if !s.Bright {
		return PhaseBright
	}

	m := MethodFor(s.Level, s.Schedule)

	// Above the death handler on purpose: in the suicide phase a death is the plan, and
	// nothing follows it but the next pocket.
	if SuicideNow(s, m) {
		return PhaseSuicide
	}
	if s.Died {
		return PhaseDead
	}

	// Getting there. The boats are only for the far side, and only from the near one.
	if m.Region == RegionArdougne && freshstart.TravelLeg(*s.Tile) == freshstart.LegPier {
		if s.Coins < freshstart.FareRemaining(s.Tile, s.Fare) {
			return PhaseFund
		}
		return PhaseTravel
	}

	if fighterfood.ShouldEat(fighterfood.EatInput{HP: s.HP, MaxHP: s.MaxHP, Heal: s.Heal, FoodCount: s.Food, Mode: fighterfood.EatThreshold, ThresholdFrac: s.EatAt}) {
		return PhaseEat
	}
	// Notes are the mule's doing rather than a mistake, but they have to be swapped for food.
	if s.NotedFood > 0 {
		return PhaseBank
	}
	if wantsFood(s, m) {
		if s.BankFood == nil || *s.BankFood > 0 {
			return PhaseBank
		}
		if s.HasMule {
			return PhaseOrder
		}
		return PhaseStuck
	}
	if s.PackFull || s.Loot >= s.BankAt {
		return PhaseBank
	}
	if !AtMethod(*s.Tile, m) {
		return PhaseMove
	}
	return PhaseWork
}

// DescribeThievePhase is the status line for a phase.
func DescribeThievePhase(p ThievePhase) string {
	switch p {
	case PhaseAccept:
		return "accepting the appearance screen"
	case PhaseTutorial:
		return "skipping the tutorial"
	case PhaseBright:
		return "turning the brightness up"
	case PhaseWait:
		return "waiting for the world"
	case PhaseSuicide:
		return "picking pockets in the courtyard, dying as it goes"
	case PhaseFund:
		return "pickpocketing for the fare"
	case PhaseTravel:
		return "sailing to Ardougne"
	case PhaseMove:
		return "walking to the next spot"
	case PhaseBank:
		return "at the bank"
	case PhaseOrder:
		return "asking the mule for food"
	case PhaseEat:
		return "eating"
	case PhaseWork:
		return "stealing"
	case PhaseDead:
		return "died — picking itself up"
	case PhaseStuck:
		return "no food, no bank, no mule"
	}
	return string(p)
}

// ---- settings ----

// Settings are ProgressiveThiever's settings. The JSON keys are the TS manifest's, so an
// account's fleet.config.json settings object decodes straight into DefaultSettings().
type Settings struct {
	Schedule       string `json:"schedule"`
	Suicide        bool   `json:"suicide"`
	Food           string `json:"food"`
	FoodWithdraw   int    `json:"foodWithdraw"`
	MinFood        int    `json:"minFood"`
	EatBelowHP     int    `json:"eatBelowHp"`
	BankAt         int    `json:"bankAt"`
	Fare           int    `json:"fare"`
	RestBelowHP    int    `json:"restBelowHp"`
	GuideNPC       string `json:"guideNpc"`
	SkipOption     string `json:"skipOption"`
	Mule           string `json:"mule"`
	AskMuleForFood bool   `json:"askMuleForFood"`
	MuleAt         int    `json:"muleAt"`
	MuleKeep       string `json:"muleKeep"`
	LogDetail      string `json:"logDetail"`
}

// MuleCoinsAt matches thievePlan.ts's own MULE_COINS_AT.
const MuleCoinsAt = 20000

// DefaultMuleKeep matches thievePlan.ts's own DEFAULT_MULE_KEEP: the coins.
const DefaultMuleKeep = freshstart.Coins

// DefaultSettings are the manifest defaults.
func DefaultSettings() Settings {
	return Settings{
		Schedule:       DefaultSchedule,
		Suicide:        true,
		Food:           "Lobster",
		FoodWithdraw:   20,
		MinFood:        1,
		EatBelowHP:     50,
		BankAt:         20,
		Fare:           freshstart.DefaultFare,
		RestBelowHP:    40,
		GuideNPC:       freshstart.DefaultGuideNPC,
		SkipOption:     freshstart.DefaultSkipOption,
		Mule:           "",
		AskMuleForFood: true,
		MuleAt:         MuleCoinsAt,
		MuleKeep:       DefaultMuleKeep,
		LogDetail:      "Normal",
	}
}

// Normalised applies the TS setting readers' own fallbacks and bounds.
func (s Settings) Normalised() Settings {
	d := DefaultSettings()
	if strings.TrimSpace(s.Food) == "" {
		s.Food = d.Food
	}
	s.Food = strings.TrimSpace(s.Food)
	if strings.TrimSpace(s.GuideNPC) == "" {
		s.GuideNPC = d.GuideNPC
	}
	if strings.TrimSpace(s.SkipOption) == "" {
		s.SkipOption = d.SkipOption
	}
	s.GuideNPC, s.SkipOption = strings.TrimSpace(s.GuideNPC), strings.TrimSpace(s.SkipOption)
	if s.FoodWithdraw < 1 {
		s.FoodWithdraw = 1
	}
	if s.MinFood < 1 {
		s.MinFood = 1
	}
	if s.MinFood > s.FoodWithdraw {
		s.MinFood = s.FoodWithdraw
	}
	if s.BankAt < 1 {
		s.BankAt = 1
	}
	if s.BankAt > 27 {
		s.BankAt = 27
	}
	if s.Fare < 0 {
		s.Fare = 0
	}
	if s.MuleAt < 0 {
		s.MuleAt = 0
	}
	if s.MuleAt > donorplan.MaxMuleAt {
		s.MuleAt = donorplan.MaxMuleAt
	}
	return s
}

// EatAt is the health share below which a bite is taken, clamped 5% through 95%.
func (s Settings) EatAt() float64 {
	f := float64(s.EatBelowHP) / 100
	if f < 0.05 {
		return 0.05
	}
	if f > 0.95 {
		return 0.95
	}
	return f
}

// RestBelow is the health share to rest at while funding the fare, clamped 5% through 90%.
func (s Settings) RestBelow() float64 {
	f := float64(s.RestBelowHP) / 100
	if f < 0.05 {
		return 0.05
	}
	if f > 0.9 {
		return 0.9
	}
	return f
}

// Schedule parses this settings' own schedule string.
func (s Settings) ParseSchedule() Schedule {
	return ParseSchedule(s.Schedule)
}

// ---- the mule ----

// Donor is the hand-over settings, wired to this settings' mule fields.
func (s Settings) Donor() donorplan.Settings {
	return donorplan.NewSettings(s.Mule, s.MuleAt, s.MuleKeep)
}

// HandoffDue reports whether a hand-over should start now, from the coins banked and this
// settings' mule threshold. bankedCoins is nil until a bank visit has counted it.
func (s Settings) HandoffDue(bankedCoins *int, now, nextAllowed time.Time) bool {
	d := s.Donor()
	if !d.Enabled() || bankedCoins == nil {
		return false
	}
	return donorplan.HandoffDue(*bankedCoins, d.MuleAt, now, nextAllowed)
}

// muleRules: coins go first (they are the loot; everything else in the bank is incidental),
// the food and the keep list stay.
func muleRules(keep []string, food string) donorplan.Rules {
	full := append(append([]string{}, keep...), food)
	return donorplan.Rules{
		Keep: full,
		First: func(row donorplan.ItemRow) bool {
			return strings.EqualFold(strings.TrimSpace(row.Name), freshstart.Coins)
		},
	}
}

// MuleRows is what leaves the bank for the mule: coins first, then anything else not kept.
func MuleRows(bank []donorplan.ItemRow, keep []string, food string) []donorplan.ItemRow {
	return donorplan.HandoffRows(bank, muleRules(keep, food))
}

// MuleHaul counts pack slots holding something for the mule.
func MuleHaul(pack []donorplan.ItemRow, keep []string, food string) int {
	return donorplan.HaulOf(pack, muleRules(keep, food))
}

// MuleLoad is what goes on the trade screen: coins first, cut to the mule's free slots.
func MuleLoad(pack []donorplan.ItemRow, freeSlots int, keep []string, food string) []donorplan.LoadGroup {
	return donorplan.PlanLoad(pack, freeSlots, muleRules(keep, food))
}

// ---- progress ----

// Progress reports how far a thieving level is through its span, and the experience remaining
// to the next one.
func Progress(level, xp int) levels.Progress {
	return levels.LevelProgress(level, xp)
}
