// Package mulereceiverplan is MuleReceiver's own pure decisions: where it goes to reach a pickup
// or get home, what a round trip costs in runes, the rune kit it keeps topped up, the queue of
// accounts asking for a pickup, and the settings shape its overlay page reads and writes. Ported
// from rsb0t-scripts' mulePlan.ts (this file) and muleSettings.ts (settings.go); the tallies
// mulePlan.ts also keeps of what trades brought in and handed out live in ledger.go.
//
// It is pure: no client, actions, fleet or bot imports and no I/O. Places are named by strings and
// tile coordinates; item and rune names are plain strings. Every place mulePlan.ts read a wall-
// clock (Date.now()) or held a clock closure, this port takes the moment as an explicit
// time.Time/time.Duration parameter instead — donorplan's Handover already does this, and it keeps
// every decision here reproducible from a snapshot rather than depending on a wall clock.
//
// Deliberately not ported, and why (see COLLAB.md for the fuller account):
//   - muleDonor.ts (the donor's own side) is already covered by donorplan/handover.go.
//   - alchPlan.ts/alchCaster.ts/herbs.ts (idle alching while the mule waits) is a separate,
//     lower-priority chunk; settings.go carries the alch settings' shape (plain data the overlay
//     reads and writes) but none of the casting/withdrawal decision logic.
//   - muleProtocol.ts (the wall's BroadcastChannel bus: Roster, SkipList, message parsing) needs a
//     new in-process pub-sub design for a one-process fleet, not a line-for-line port, and is left
//     for whoever builds the internal/bot MuleReceiver integration. PickupQueue below is
//     transport-agnostic — it doesn't care whether an offer arrived over a bus or a plain function
//     call — so it did not need to wait for that design.
package mulereceiverplan

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// PackSlots is a full pack, mulePlan.ts's PACK_SLOTS.
const PackSlots = 28

// SeersStand is the Seers' Village bank stand — the client's own BankLocations entry mulePlan.ts
// carries as SEERS_STAND, and MuleReceiver's default post.
var SeersStand = tile.Tile{X: 2725, Z: 3491, Level: 0}

// ---- runes ----

// The five rune names a teleport or the kit ever names, carried over unchanged from mulePlan.ts's
// LAW/AIR/FIRE/WATER/EARTH.
const (
	LawRune   = "Law rune"
	AirRune   = "Air rune"
	FireRune  = "Fire rune"
	WaterRune = "Water rune"
	EarthRune = "Earth rune"
)

// KitRunes are every rune a teleport can ask for — the kit the mule keeps out of the deposit.
func KitRunes() []string {
	return []string{LawRune, AirRune, FireRune, WaterRune, EarthRune}
}

// RuneCost is one rune and how many a spell or a kit asks for.
type RuneCost struct {
	Rune  string
	Count int
}

func cost(rune string, count int) RuneCost { return RuneCost{Rune: rune, Count: count} }

// ---- teleports ----

// TeleportID names a standard-spellbook teleport exactly as Game.teleport resolves it.
type TeleportID string

// Every teleport the mule's trip planning knows about.
const (
	Varrock    TeleportID = "varrock"
	Lumbridge  TeleportID = "lumbridge"
	Falador    TeleportID = "falador"
	Camelot    TeleportID = "camelot"
	Ardougne   TeleportID = "ardougne"
	Watchtower TeleportID = "watchtower"
	Trollheim  TeleportID = "trollheim"
)

// TeleportSpell is one standard-spellbook teleport: where it lands, what it costs, and the quest
// (if any) that must be complete before it can be cast at all.
type TeleportSpell struct {
	ID      TeleportID
	Landing tile.Tile
	Level   int
	Runes   []RuneCost
	// Quest is empty when the spell needs no quest.
	Quest string
}

// Teleports are the standard-spellbook teleports, copied from the client's nav catalog (not
// exported through the script ABI, so these numbers live here as mulePlan.ts's own copy did).
// Landing tiles are what the trip planner checks arrival against, so a wrong one here would have
// the mule abandon every trip.
var Teleports = []TeleportSpell{
	{ID: Varrock, Landing: tile.Tile{X: 3213, Z: 3424}, Level: 25, Runes: []RuneCost{cost(FireRune, 1), cost(AirRune, 3), cost(LawRune, 1)}},
	{ID: Lumbridge, Landing: tile.Tile{X: 3221, Z: 3218}, Level: 31, Runes: []RuneCost{cost(EarthRune, 1), cost(AirRune, 3), cost(LawRune, 1)}},
	{ID: Falador, Landing: tile.Tile{X: 2965, Z: 3378}, Level: 37, Runes: []RuneCost{cost(WaterRune, 1), cost(AirRune, 3), cost(LawRune, 1)}},
	{ID: Camelot, Landing: tile.Tile{X: 2757, Z: 3478}, Level: 45, Runes: []RuneCost{cost(AirRune, 5), cost(LawRune, 1)}},
	{ID: Ardougne, Landing: tile.Tile{X: 2661, Z: 3301}, Level: 51, Runes: []RuneCost{cost(WaterRune, 2), cost(LawRune, 2)}, Quest: "Plague City"},
	{ID: Watchtower, Landing: tile.Tile{X: 2933, Z: 4713, Level: 2}, Level: 58, Runes: []RuneCost{cost(EarthRune, 2), cost(LawRune, 2)}, Quest: "Watch Tower"},
	{ID: Trollheim, Landing: tile.Tile{X: 2890, Z: 3679}, Level: 61, Runes: []RuneCost{cost(FireRune, 2), cost(LawRune, 2)}, Quest: "Eadgar's Ruse"},
}

// TeleportByID looks a teleport up by id. Unlike mulePlan.ts's teleportById, which throws on an
// unknown id, this reports ok=false — nothing in this package's own decisions calls it with
// anything but a Teleports member, but a caller building its own id is not guaranteed one.
func TeleportByID(id TeleportID) (TeleportSpell, bool) {
	for _, t := range Teleports {
		if t.ID == id {
			return t, true
		}
	}
	return TeleportSpell{}, false
}

// ---- staves ----

// staffRunes is the slice of fighterData.ts's STAFF_RUNES this package actually needs: which
// staves supply which runes for free while wielded, plainest first — this package's own copy
// rather than a shared one, the same "port only what is used" call fighterfood already made for
// BarbarianAgility's slice of fighterData.ts/fighterPlan.ts.
var staffRunes = []struct {
	Name  string
	Runes []string
}{
	{"Staff of air", []string{AirRune}},
	{"Staff of water", []string{WaterRune}},
	{"Staff of earth", []string{EarthRune}},
	{"Staff of fire", []string{FireRune}},
	{"Air battlestaff", []string{AirRune}},
	{"Water battlestaff", []string{WaterRune}},
	{"Earth battlestaff", []string{EarthRune}},
	{"Fire battlestaff", []string{FireRune}},
	{"Lava battlestaff", []string{EarthRune, FireRune}},
	{"Mystic air staff", []string{AirRune}},
	{"Mystic water staff", []string{WaterRune}},
	{"Mystic earth staff", []string{EarthRune}},
	{"Mystic fire staff", []string{FireRune}},
	{"Mystic lava staff", []string{EarthRune, FireRune}},
}

// AirStaves are the staves that pay for air, plainest first — what the mule can wield in place of
// the kit's air runes.
func AirStaves() []string {
	var out []string
	for _, s := range staffRunes {
		for _, r := range s.Runes {
			if r == AirRune {
				out = append(out, s.Name)
				break
			}
		}
	}
	return out
}

// IsRuneStaff reports whether name is any staff that pays for a rune.
func IsRuneStaff(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	for _, s := range staffRunes {
		if strings.ToLower(s.Name) == key {
			return true
		}
	}
	return false
}

// IsAirStaff reports whether name is one of AirStaves.
func IsAirStaff(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	for _, s := range AirStaves() {
		if strings.ToLower(s) == key {
			return true
		}
	}
	return false
}

func staffProvides(worn []string) map[string]bool {
	out := map[string]bool{}
	for _, item := range worn {
		want := strings.ToLower(strings.TrimSpace(item))
		for _, s := range staffRunes {
			if strings.ToLower(s.Name) == want {
				for _, r := range s.Runes {
					out[strings.ToLower(r)] = true
				}
			}
		}
	}
	return out
}

// ---- the caster ----

// QuestComplete is the status string that unlocks a quest-gated teleport (Quests.status(name)).
const QuestComplete = "complete"

// CasterWorld is what the trip planner needs to know about the caster, read fresh for each
// decision rather than cached, so a level-up or a rune spent mid-run is seen at once.
type CasterWorld struct {
	MagicLevel int
	// QuestStatus reports a quest's status; only QuestComplete unlocks a quest-gated spell.
	QuestStatus func(name string) string
	// Worn is everything worn, for staves that supply a rune for free.
	Worn []string
	// Held is how many of a rune the pack holds.
	Held func(rune string) int
}

// Castable reports whether spell can be cast at all: the level, and its quest if it has one.
func Castable(spell TeleportSpell, world CasterWorld) bool {
	if world.MagicLevel < spell.Level {
		return false
	}
	return spell.Quest == "" || world.QuestStatus(spell.Quest) == QuestComplete
}

// RunesFor is the runes one cast of spell actually consumes, after whatever a wielded staff
// supplies for free.
func RunesFor(spell TeleportSpell, worn []string) []RuneCost {
	free := staffProvides(worn)
	var out []RuneCost
	for _, r := range spell.Runes {
		if !free[strings.ToLower(r.Rune)] {
			out = append(out, r)
		}
	}
	return out
}

// SumCosts merges rune lists by name, so two legs read as one bill. The rune's spelling in the
// result is whichever list named it first, and runes appear in first-seen order.
func SumCosts(lists ...[]RuneCost) []RuneCost {
	var order []string
	total := map[string]RuneCost{}
	for _, list := range lists {
		for _, c := range list {
			key := strings.ToLower(c.Rune)
			prior, seen := total[key]
			name := c.Rune
			if seen {
				name = prior.Rune
			} else {
				order = append(order, key)
			}
			total[key] = cost(name, prior.Count+c.Count)
		}
	}
	out := make([]RuneCost, 0, len(order))
	for _, k := range order {
		out = append(out, total[k])
	}
	return out
}

// Shortfall is what is missing from the pack to pay costs; empty when it is all there.
func Shortfall(costs []RuneCost, held func(rune string) int) []RuneCost {
	var out []RuneCost
	for _, c := range costs {
		if missing := c.Count - held(c.Rune); missing > 0 {
			out = append(out, cost(c.Rune, missing))
		}
	}
	return out
}

// CanAfford reports whether held can pay costs in full.
func CanAfford(costs []RuneCost, held func(rune string) int) bool {
	return len(Shortfall(costs, held)) == 0
}

var runeSuffix = regexp.MustCompile(`(?i) rune$`)

// DescribeCosts is "1 law, 5 air" — for log lines.
func DescribeCosts(costs []RuneCost) string {
	if len(costs) == 0 {
		return "nothing"
	}
	parts := make([]string, len(costs))
	for i, c := range costs {
		parts[i] = fmt.Sprintf("%d %s", c.Count, strings.ToLower(runeSuffix.ReplaceAllString(c.Rune, "")))
	}
	return strings.Join(parts, ", ")
}

// IsWilderness reports whether t is in the classic wilderness rectangle. A pickup there is never
// worth the mule's kit.
func IsWilderness(t tile.Tile) bool {
	return t.Z >= 3520 && t.X >= 2944 && t.X <= 3392
}

// TeleportPick is a castable teleport and what taking it would cost and reach.
type TeleportPick struct {
	Spell     TeleportSpell
	Cost      []RuneCost
	WalkTiles int
}

// closer reports whether candidate beats best: nearer first, the lower-level spell breaking a tie.
func closer(candidate TeleportPick, best *TeleportPick) bool {
	if best == nil || candidate.WalkTiles < best.WalkTiles {
		return true
	}
	return candidate.WalkTiles == best.WalkTiles && candidate.Spell.Level < best.Spell.Level
}

// ClosestTeleport is the castable teleport landing nearest target, runes aside.
func ClosestTeleport(target tile.Tile, world CasterWorld) (TeleportPick, bool) {
	var best *TeleportPick
	for _, spell := range Teleports {
		if !Castable(spell, world) {
			continue
		}
		candidate := TeleportPick{Spell: spell, Cost: RunesFor(spell, world.Worn), WalkTiles: tile.Chebyshev(spell.Landing, target)}
		if closer(candidate, best) {
			c := candidate
			best = &c
		}
	}
	if best == nil {
		return TeleportPick{}, false
	}
	return *best, true
}

// HomeTeleport is the ride home: the castable landing nearest home, runes aside. It follows home
// rather than assuming Seers, so a Varrock post picks Varrock.
func HomeTeleport(home tile.Tile, world CasterWorld) (TeleportPick, bool) {
	return ClosestTeleport(home, world)
}

// PickTeleport is the castable teleport landing nearest target that the pack can pay for together
// with homeCost. Ties go to the lower-level spell.
func PickTeleport(target tile.Tile, world CasterWorld, homeCost []RuneCost) (TeleportPick, bool) {
	var best *TeleportPick
	for _, spell := range Teleports {
		if !Castable(spell, world) {
			continue
		}
		out := RunesFor(spell, world.Worn)
		if !CanAfford(SumCosts(out, homeCost), world.Held) {
			continue
		}
		candidate := TeleportPick{Spell: spell, Cost: out, WalkTiles: tile.Chebyshev(spell.Landing, target)}
		if closer(candidate, best) {
			c := candidate
			best = &c
		}
	}
	if best == nil {
		return TeleportPick{}, false
	}
	return *best, true
}

// ---- trips ----

// TripMode is how a trip out is made.
type TripMode string

// The trip modes.
const (
	TripWalk     TripMode = "walk"
	TripTeleport TripMode = "teleport"
	TripDecline  TripMode = "decline"
)

// TripPlan is how to reach a pickup: the fields that matter depend on Mode.
type TripPlan struct {
	Mode TripMode
	// Tiles is the walk distance, set for TripWalk.
	Tiles int
	// Note describes the choice, set for TripWalk and TripTeleport.
	Note string
	// Spell, OutCost, HomeCost and WalkTiles are set for TripTeleport.
	Spell     TeleportSpell
	OutCost   []RuneCost
	HomeCost  []RuneCost
	WalkTiles int
	// Reason is set for TripDecline.
	Reason string
}

// PlanTripInput is what PlanTrip reads.
type PlanTripInput struct {
	Me, Home, Target tile.Tile
	// NearRadius: within this many tiles the mule walks, whatever the pack holds.
	NearRadius int
	// MaxLegTiles is the longest leg it will walk, from wherever that leg starts.
	MaxLegTiles int
	World       CasterWorld
}

// PlanTrip decides how to reach a pickup. Nearby is always on foot and needs no runes. Anything
// further is a teleport trip, and a teleport trip is only taken when the pack holds the runes for
// both legs — the mule must never be stranded away from its bank.
func PlanTrip(in PlanTripInput) TripPlan {
	if IsWilderness(in.Target) {
		return TripPlan{Mode: TripDecline, Reason: "the pickup is in the wilderness"}
	}
	direct := tile.Chebyshev(in.Me, in.Target)
	if direct <= in.NearRadius {
		return TripPlan{Mode: TripWalk, Tiles: direct, Note: "nearby"}
	}

	home, ok := HomeTeleport(in.Home, in.World)
	if !ok {
		return TripPlan{Mode: TripDecline, Reason: fmt.Sprintf("cannot get home: no teleport is castable at magic %d", in.World.MagicLevel)}
	}
	if home.WalkTiles > in.MaxLegTiles {
		return TripPlan{Mode: TripDecline, Reason: fmt.Sprintf("cannot get home: %s lands %d tiles from the post, over the %d cap", home.Spell.ID, home.WalkTiles, in.MaxLegTiles)}
	}
	homeCost := home.Cost

	pick, ok := PickTeleport(in.Target, in.World, homeCost)
	if !ok {
		closest, ok := ClosestTeleport(in.Target, in.World)
		if !ok {
			return TripPlan{Mode: TripDecline, Reason: "no teleport is castable"}
		}
		short := Shortfall(SumCosts(closest.Cost, homeCost), in.World.Held)
		return TripPlan{Mode: TripDecline, Reason: fmt.Sprintf("no runes for %s out and %s home — short %s", closest.Spell.ID, home.Spell.ID, DescribeCosts(short))}
	}

	if direct <= pick.WalkTiles {
		// No landing gets closer than the mule already is; the runes would buy nothing.
		if direct > in.MaxLegTiles {
			return TripPlan{Mode: TripDecline, Reason: fmt.Sprintf("too far to walk (%d tiles) and no teleport lands closer", direct)}
		}
		return TripPlan{Mode: TripWalk, Tiles: direct, Note: "no teleport lands closer"}
	}
	if pick.WalkTiles > in.MaxLegTiles {
		return TripPlan{Mode: TripDecline, Reason: fmt.Sprintf("%s lands %d tiles from the pickup, over the %d cap", pick.Spell.ID, pick.WalkTiles, in.MaxLegTiles)}
	}
	return TripPlan{
		Mode: TripTeleport, Spell: pick.Spell, OutCost: pick.Cost, HomeCost: homeCost, WalkTiles: pick.WalkTiles,
		Note: fmt.Sprintf("%s lands %d tiles away (%d on foot)", pick.Spell.ID, pick.WalkTiles, direct),
	}
}

// ReturnMode is how the mule gets home.
type ReturnMode string

// The return modes.
const (
	ReturnWalk ReturnMode = "walk"
	// ReturnTeleport: a cast landing closer to the post than the mule already stands, affordable.
	ReturnTeleport ReturnMode = "teleport"
	// ReturnWalkFallback: home is mandatory, so no runes means a long walk rather than a refusal.
	ReturnWalkFallback ReturnMode = "walk-fallback"
)

// ReturnPlan is the way home.
type ReturnPlan struct {
	Mode  ReturnMode
	Tiles int
	Spell TeleportSpell
	Cost  []RuneCost
	// Reason is set for ReturnWalkFallback.
	Reason string
}

// PlanReturnInput is what PlanReturn reads.
type PlanReturnInput struct {
	Me, Home   tile.Tile
	NearRadius int
	World      CasterWorld
}

// PlanReturn is the way home. Near is a walk. Further out, the teleport landing nearest the post
// is cast when it is closer to the post than the mule already stands and the pack can pay for it;
// otherwise the long walk, never a refusal — home is mandatory.
func PlanReturn(in PlanReturnInput) ReturnPlan {
	tiles := tile.Chebyshev(in.Me, in.Home)
	if tiles <= in.NearRadius {
		return ReturnPlan{Mode: ReturnWalk, Tiles: tiles}
	}
	pick, ok := HomeTeleport(in.Home, in.World)
	if !ok {
		return ReturnPlan{Mode: ReturnWalkFallback, Tiles: tiles, Reason: fmt.Sprintf("no teleport is castable at magic %d", in.World.MagicLevel)}
	}
	if pick.WalkTiles >= tiles {
		// No landing gets closer to the post than here; the runes would buy nothing.
		return ReturnPlan{Mode: ReturnWalk, Tiles: tiles}
	}
	if !CanAfford(pick.Cost, in.World.Held) {
		return ReturnPlan{Mode: ReturnWalkFallback, Tiles: tiles, Reason: fmt.Sprintf("short %s for %s", DescribeCosts(Shortfall(pick.Cost, in.World.Held)), pick.Spell.ID)}
	}
	return ReturnPlan{Mode: ReturnTeleport, Spell: pick.Spell, Cost: pick.Cost}
}

// DefaultLandingRadius is arrivedAt's own default in mulePlan.ts; Go has no default parameters, so
// ArrivedAt takes it explicitly.
const DefaultLandingRadius = 6

// ArrivedAt reports whether me is on the landing's plane within radius of it. me nil means the
// position is not known yet, which never counts as arrived.
func ArrivedAt(me *tile.Tile, landing tile.Tile, radius int) bool {
	return me != nil && me.Level == landing.Level && tile.Chebyshev(*me, landing) <= radius
}

// Leg timing: a budget for one walking leg, generous enough for doors and a re-path or two.
const (
	legBase    = 45 * time.Second
	legPerTile = 1500 * time.Millisecond
	legMin     = 60 * time.Second
	legMax     = 8 * time.Minute
)

// LegTimeout is a budget for one walking leg of tiles tiles.
func LegTimeout(tiles int) time.Duration {
	if tiles < 0 {
		tiles = 0
	}
	raw := legBase + time.Duration(tiles)*legPerTile
	if raw < legMin {
		return legMin
	}
	if raw > legMax {
		return legMax
	}
	return raw
}

// ---- the rune kit ----

// RuneCounts is the five kit runes' pack counts, in the fixed order the settings panel shows
// them. mulePlan.ts's kitFromSettings takes an arbitrary Record<string, number>; every real
// caller passes exactly these five named runes, so this port names them instead of carrying a
// map whose Go iteration order would not match the settings panel's.
type RuneCounts struct {
	Law, Air, Fire, Water, Earth int
}

// KitFromSettings is the rune kit's counts by name, zero and negative counts dropped.
func KitFromSettings(counts RuneCounts) []RuneCost {
	var out []RuneCost
	for _, c := range []RuneCost{
		cost(LawRune, counts.Law), cost(AirRune, counts.Air), cost(FireRune, counts.Fire),
		cost(WaterRune, counts.Water), cost(EarthRune, counts.Earth),
	} {
		if c.Count > 0 {
			out = append(out, c)
		}
	}
	return out
}

// KitAfterStaff is the kit less whatever a worn staff supplies: those runes would only ride in the
// pack unspent.
func KitAfterStaff(kit []RuneCost, worn []string) []RuneCost {
	free := staffProvides(worn)
	var out []RuneCost
	for _, c := range kit {
		if !free[strings.ToLower(c.Rune)] {
			out = append(out, c)
		}
	}
	return out
}

// KitNames is the keep-list for a deposit: kit runes never go into the bank.
func KitNames(kit []RuneCost) []string {
	var out []string
	for _, c := range kit {
		if c.Count > 0 {
			out = append(out, c.Rune)
		}
	}
	return out
}

// KitWithdrawal is one rune to draw out of the bank to top the kit back up.
type KitWithdrawal struct {
	Rune     string
	Withdraw int
}

// KitShortfall is what to withdraw to bring the pack back up to kit, capped by what the bank
// holds.
func KitShortfall(kit []RuneCost, held func(rune string) int, bank func(rune string) int) []KitWithdrawal {
	var out []KitWithdrawal
	for _, c := range kit {
		missing := c.Count - held(c.Rune)
		withdraw := missing
		if b := bank(c.Rune); withdraw > b {
			withdraw = b
		}
		if withdraw > 0 {
			out = append(out, KitWithdrawal{Rune: c.Rune, Withdraw: withdraw})
		}
	}
	return out
}

// KitExcess is kit runes the pack holds more of than the kit asks for — a donor's runes, which the
// deposit's keep-list would otherwise leave riding in the pack to be spent on teleports.
func KitExcess(kit []RuneCost, held func(rune string) int) []string {
	var out []string
	for _, c := range kit {
		if held(c.Rune) > c.Count {
			out = append(out, c.Rune)
		}
	}
	return out
}

// HaulSlots is pack slots holding something other than the kit — what a deposit is for. names is
// one entry per occupied pack slot (mulePlan.ts's haulSlots reads Inventory.items(), which never
// carries an empty slot, so an empty name here is haul exactly like anything else not in kit).
func HaulSlots(names []string, kit []RuneCost) int {
	keep := map[string]bool{}
	for _, n := range KitNames(kit) {
		keep[strings.ToLower(n)] = true
	}
	n := 0
	for _, name := range names {
		if !keep[strings.ToLower(name)] {
			n++
		}
	}
	return n
}

// ---- places ----

// Place is a place a PM order or a post setting named, resolved to where the mule should take it
// or post at.
type Place struct {
	Name string
	Tile tile.Tile
}

// BankPlace is one bank the mule could be pointed at: a name and its stand tile.
type BankPlace struct {
	Name string
	Tile tile.Tile
}

var (
	apostropheRe = regexp.MustCompile(`['’]`)
	stopWordRe   = regexp.MustCompile(`(?i)\b(?:the|bank|village)\b`)
	collapseRe   = regexp.MustCompile(`\s+`)
)

func placeKey(raw string) string {
	s := strings.ToLower(raw)
	s = apostropheRe.ReplaceAllString(s, "")
	s = stopWordRe.ReplaceAllString(s, " ")
	s = collapseRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// ParsePostTile is a typed post override, "x,z" or "x,z,level"; ok is false for blank or malformed
// text.
func ParsePostTile(raw string) (tile.Tile, bool) {
	parts := strings.Split(raw, ",")
	if len(parts) < 2 || len(parts) > 3 {
		return tile.Tile{}, false
	}
	nums := make([]float64, len(parts))
	for i, p := range parts {
		n, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return tile.Tile{}, false
		}
		nums[i] = n
	}
	level := 0.0
	if len(nums) == 3 {
		level = nums[2]
	}
	return tile.Tile{X: int(nums[0]), Z: int(nums[1]), Level: int(level)}, true
}

// ResolvePlace is the tile behind a place typed into a PM: coordinates, a bank by name ("varrock
// west", "seers' village bank"), a teleport town ("varrock", "lumbridge" — the landing), or the
// one bank whose name starts with it ("shilo"). ok is false for anything else.
func ResolvePlace(raw string, banks []BankPlace) (Place, bool) {
	if t, ok := ParsePostTile(raw); ok {
		return Place{Name: fmt.Sprintf("%d,%d", t.X, t.Z), Tile: t}, true
	}
	key := placeKey(raw)
	if key == "" {
		return Place{}, false
	}
	for _, b := range banks {
		if placeKey(b.Name) == key {
			return Place{Name: b.Name, Tile: b.Tile}, true
		}
	}
	// Before the prefix match, so "varrock" is the square rather than a guess between its two banks.
	for _, spell := range Teleports {
		if string(spell.ID) == key {
			return Place{Name: string(spell.ID), Tile: spell.Landing}, true
		}
	}
	var partial []BankPlace
	for _, b := range banks {
		if strings.HasPrefix(placeKey(b.Name), key) {
			partial = append(partial, b)
		}
	}
	if len(partial) == 1 {
		return Place{Name: partial[0].Name, Tile: partial[0].Tile}, true
	}
	return Place{}, false
}
