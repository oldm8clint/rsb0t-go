package fleetplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// TravelTeleport is one teleport a walk may use: the standard teleport and its quest gate.
type TravelTeleport struct {
	Teleport
	// Quest must be complete before the spell can be cast ("" for none).
	Quest string
}

// ArdougneTeleport is the Plague City teleport (magic_spells.dbrow). Not in Teleports: Stage 3's
// training mode would otherwise choose it (more xp than Camelot) for accounts the server refuses.
var ArdougneTeleport = TravelTeleport{
	Teleport: Teleport{Spell: Spell{Name: "Ardougne Teleport", Level: 51, XP: 610, Members: true,
		Runes: []Rune{{Name: RuneWater, Count: 2}, {Name: RuneLaw, Count: 2}}},
		DestX: 41*64 + 37, DestZ: 51*64 + 37},
	Quest: "Plague City",
}

// TravelTeleports is every teleport a walk may use, lowest level first.
func TravelTeleports() []TravelTeleport {
	out := make([]TravelTeleport, 0, len(Teleports)+1)
	for _, tp := range Teleports {
		out = append(out, TravelTeleport{Teleport: tp})
	}
	return append(out, ArdougneTeleport)
}

// TeleTravelSettings are the teleport-travel knobs (teleTravel* settings).
type TeleTravelSettings struct {
	// Enabled is teleTravel, default false: not lab- or live-proven yet.
	Enabled bool
	// MinSave is the least walking a teleport must save, in tiles (teleTravelMinSave, default
	// 150): law runes are scarce (about 4,500 on the mule, 3 Oct).
	MinSave int
	// LawReserve is law runes in the pack travel never spends (teleTravelLawReserve, default 0).
	LawReserve int
	// AirRunes lets a teleport pay its air runes from the pack (teleTravelAirRunes, default true since 13 Oct 2026): the
	// mule holds 655,000 of them. Off, only an account with a Staff of air WORN casts an air teleport.
	AirRunes bool
	// Kit is the casts one dedicated mule ask sizes the laws and elementals for (teleTravelKit, default 30, the TripLaws).
	Kit int
	// MuleLawFloor is the mule's law stock below which no account asks for laws for travel
	// (teleTravelMuleLawFloor, default 1000).
	MuleLawFloor int
	// AskEvery is the travel kit ask's own interval (teleTravelAskMinutes, default 60).
	AskEvery time.Duration
	// AnyWalk lets a teleport start any long walk (teleTravelAnyWalk, default true since 13 Oct 2026: the owner's word
	// "every navigation trip"). Off, only the walk to the mule's post may (the 6 Oct ruling, the rollback), and the
	// dedicated travel kit (Kit, MuleLawFloor, AskEvery above) is not asked for: the laws ride a mule ask that is going anyway.
	AnyWalk bool
	// TripLaws is how many casts' runes a mule ask tops the account's pack up to, for the next trips' teleports
	// (teleTravelMuleLaws, default DefaultTripLaws; 0 asks for none): laws, five times as many air runes, and fire runes
	// for Varrock (TripKitLines).
	TripLaws int
	// TripLawFloor is the mule's law stock below which no account asks for them on a trip
	// (teleTravelMuleLawsFloor, default DefaultTripLawFloor): the last few are left for the fleet's other
	// uses (the Telekinetic Grab, the teleport training mode).
	TripLawFloor int

	// The tick-based decision (DecideTeleport, teledecide.go; teleport-walks, 13 Oct 2026). Each is filled with its
	// Default when zero by Normalised; FleetPerHour and MaxPerHour take a negative value for "none".

	// MinSaveTicks is the least ticks a cast must save after its own delay (teleTravelMinSaveTicks, DefaultMinSaveTicks),
	// before the law price multiplies it (LawPrice).
	MinSaveTicks int
	// CastTicks is what the cast itself costs in ticks (teleTravelCastTicks, DefaultCastTicks).
	CastTicks int
	// SettleTicks is how long after its own alchemy cast or rub the spell click waits (teleTravelSettleTicks,
	// DefaultSettleTicks): a delayed player drops it silently.
	SettleTicks int
	// ShipTicks is the extra ticks one ship hop counts in a walk's price (teleTravelShipTicks, DefaultShipTicks).
	ShipTicks int
	// MaxPerHour is the casts one account makes an hour that save under CheapCastTicks (teleTravelMaxPerHour,
	// DefaultMaxPerHour); a negative value allows only casts that save CheapCastTicks or more.
	MaxPerHour int
	// FleetPerHour is the casts the whole process makes an hour (teleTravelFleetPerHour, DefaultFleetPerHour); negative
	// switches casting off without touching Enabled.
	FleetPerHour int
	// MissedTicks is the walking, in ticks, that casts the account could not pay for would have saved, inside MissedWindow,
	// that is worth a dedicated ask for a kit (teleTravelMissedTicks, DefaultMissedTicks).
	MissedTicks int
}

// DefaultTripLaws is teleTravelMuleLaws's default: thirty casts. The mule trades about a hundred hand-outs an hour and is close
// to saturated (13 Oct), so a kit rides the trades that happen and must last: at one or two casts an account would ask for
// more every walk, and a dedicated ask per ten casts would have added a hundred asks an hour.
const DefaultTripLaws = 30

// DefaultTripLawFloor is teleTravelMuleLawsFloor's default: 1,000. The mule held 33,673 laws on 13 Oct 2026, the only shop
// that sells them (Lundail's) restocks twenty an hour, and LawPrice makes a cast dearer long before the floor.
const DefaultTripLawFloor = 1000

// Normalised fills the defaults a zero or negative value stands for.
func (s TeleTravelSettings) Normalised() TeleTravelSettings {
	if s.MinSave < 1 {
		s.MinSave = 150
	}
	if s.LawReserve < 0 {
		s.LawReserve = 0
	}
	if s.Kit < 1 {
		s.Kit = DefaultTripLaws
	}
	if s.MuleLawFloor < 0 {
		s.MuleLawFloor = 0
	}
	if s.AskEvery < 10*time.Minute {
		s.AskEvery = 60 * time.Minute
	}
	if s.TripLaws < 0 {
		s.TripLaws = 0
	}
	if s.TripLawFloor < 0 {
		s.TripLawFloor = 0
	}
	if s.MinSaveTicks < 1 {
		s.MinSaveTicks = DefaultMinSaveTicks
	}
	if s.CastTicks < 1 {
		s.CastTicks = DefaultCastTicks
	}
	if s.SettleTicks < 1 {
		s.SettleTicks = DefaultSettleTicks
	}
	if s.ShipTicks < 1 {
		s.ShipTicks = DefaultShipTicks
	}
	if s.MaxPerHour == 0 {
		s.MaxPerHour = DefaultMaxPerHour
	}
	if s.FleetPerHour == 0 {
		s.FleetPerHour = DefaultFleetPerHour
	}
	if s.MissedTicks < 1 {
		s.MissedTicks = DefaultMissedTicks
	}
	return s
}

// TravelSide is which side of the sea and White Wolf Mountain a surface tile is on: 1 west of the
// mountain (Ardougne, Seers', Catherby, Yanille), 2 east of it (Falador, Varrock, Lumbridge, the
// Wilderness), 3 Karamja. 0 is underground or unknown (z 6400 and above). A walk between two
// sides is the ship chain or the wolf pass, so a landing only helps on the destination's side.
func TravelSide(t tile.Tile) int {
	switch {
	case t.Z >= 6400:
		return 0
	case freshstart.Karamja.Contains(tile.Tile{X: t.X, Z: t.Z}):
		return 3
	case t.X < 2850:
		return 1
	}
	return 2
}

// TravelCrossingTiles is what a walk between two sides is worth in tiles on top of its
// straight-line distance: the Port Sarim - Karamja - Brimhaven - Ardougne chain took 17-20
// minutes live (ShipRouteWalkFloor), several hundred tiles of walking.
const TravelCrossingTiles = 300

// TravelStraightTiles is the straight-line (Chebyshev) distance on the map, levels ignored.
func TravelStraightTiles(a, b tile.Tile) int {
	return tile.Chebyshev(tile.Tile{X: a.X, Z: a.Z}, tile.Tile{X: b.X, Z: b.Z})
}

// TravelWalkTiles is the walk estimate: Chebyshev, plus TravelCrossingTiles across sides.
func TravelWalkTiles(a, b tile.Tile) int {
	d := TravelStraightTiles(a, b)
	if TravelSide(a) != TravelSide(b) {
		d += TravelCrossingTiles
	}
	return d
}

// TeleTravelInput is what one pre-walk decision reads.
type TeleTravelInput struct {
	Here, Dest tile.Tile
	Magic      int
	// Members is false only on a free world.
	Members bool
	// Pack is the runes carried; Staff the staff wielded ("" for none).
	Pack  RuneStock
	Staff string
	// QuestDone answers a quest gate; nil treats every quest as not done.
	QuestDone func(quest string) bool
	// Blocked is a spell this account was refused recently; nil blocks none.
	Blocked func(spell string) bool
	// Carrying names what the pack holds, for TeleportDestroys.
	Carrying []string
}

// TeleportDestroys is what teleport.rs2's tele_checks and post_tele_checks delete from the pack
// (Karamja rum, a Biohazard plague sample, Ana in a barrel): a walk carrying one never teleports.
var TeleportDestroys = []string{"Karamja rum", "Plague sample", "Ana in a Barrel"}

func carriesTeleportCasualty(names []string) (string, bool) {
	for _, n := range names {
		for _, d := range TeleportDestroys {
			if strings.EqualFold(strings.TrimSpace(n), d) {
				return d, true
			}
		}
	}
	return "", false
}

// TravelChoice is the teleport to cast before a walk.
type TravelChoice struct {
	Teleport TravelTeleport
	// Walk is the estimated walk without it, Save what it saves.
	Walk, Save int
	Why        string
}

// Landing is the teleport's landing square.
func (c TravelChoice) Landing() tile.Tile {
	return tile.Tile{X: c.Teleport.DestX, Z: c.Teleport.DestZ}
}

// TravelFree is what the wielded staff pays for.
func TravelFree(staff string) Provided { return StaffProvides(staff) }

// TeleportPayable is whether the pack and staff pay one cast of tp with the law reserve kept and,
// unless airRunes, every air rune paid by the staff.
func TeleportPayable(tp TravelTeleport, pack RuneStock, staff string, lawReserve int, airRunes bool) bool {
	free := TravelFree(staff)
	stock := RuneStock{}
	for k, v := range pack {
		stock[k] = v
	}
	stock[RuneLaw] -= lawReserve
	for _, r := range tp.Spell.Runes {
		if r.Name == RuneAir && !free[RuneAir] && !airRunes {
			return false
		}
	}
	return tp.Spell.CanCast(stock, free)
}

// TeleportAllowed is the level, world, quest and back-off gates (not the runes).
func TeleportAllowed(tp TravelTeleport, magic int, members bool, questDone, blocked func(string) bool) bool {
	if magic < tp.Spell.Level || (tp.Spell.Members && !members) || SpendsQueenRunes(tp.Spell) {
		return false
	}
	if tp.Quest != "" && (questDone == nil || !questDone(tp.Quest)) {
		return false
	}
	return blocked == nil || !blocked(tp.Spell.Name)
}

// ChooseTravelTeleport is the teleport, if any, to cast before walking from in.Here to in.Dest:
// the payable one that saves the most walking, when that is at least MinSave tiles. Never above
// Wilderness level 20 (teleport.rs2 refuses), never from or to underground, and never to a
// landing on another side of the sea or the mountain from the destination.
func ChooseTravelTeleport(s TeleTravelSettings, in TeleTravelInput) (TravelChoice, bool) {
	if !s.Enabled {
		return TravelChoice{Why: "teleport travel off"}, false
	}
	s = s.Normalised()
	if item, ok := carriesTeleportCasualty(in.Carrying); ok {
		return TravelChoice{Why: "carrying " + item + ", which a teleport destroys"}, false
	}
	if WildernessLevel(in.Here) > 20 {
		return TravelChoice{Why: "above Wilderness level 20"}, false
	}
	if TravelSide(in.Here) == 0 || TravelSide(in.Dest) == 0 {
		return TravelChoice{Why: "underground"}, false
	}
	// The straight line alone must be long: the crossing allowance only adds to what a teleport
	// saves, so a few steps across the x 2850 line at the mountain never look like a sea voyage.
	if TravelStraightTiles(in.Here, in.Dest) < s.MinSave {
		return TravelChoice{Walk: TravelStraightTiles(in.Here, in.Dest), Why: "a short walk"}, false
	}
	walk := TravelWalkTiles(in.Here, in.Dest)
	var best TravelChoice
	found := false
	for _, tp := range TravelTeleports() {
		if !TeleportAllowed(tp, in.Magic, in.Members, in.QuestDone, in.Blocked) {
			continue
		}
		if !TeleportPayable(tp, in.Pack, in.Staff, s.LawReserve, s.AirRunes) {
			continue
		}
		land := tile.Tile{X: tp.DestX, Z: tp.DestZ}
		if TravelSide(land) != TravelSide(in.Dest) {
			continue
		}
		save := walk - TravelWalkTiles(land, in.Dest)
		if save < s.MinSave {
			continue
		}
		if !found || save > best.Save || (save == best.Save && LawCost(tp) < LawCost(best.Teleport)) {
			best, found = TravelChoice{Teleport: tp, Walk: walk, Save: save}, true
		}
	}
	if !found {
		return TravelChoice{Walk: walk, Why: "no payable teleport saves " + Itoa(s.MinSave) + " tiles"}, false
	}
	best.Why = best.Teleport.Spell.Name + " saves about " + Itoa(best.Save) + " of " + Itoa(walk) + " tiles"
	return best, true
}

func LawCost(tp TravelTeleport) int {
	for _, r := range tp.Spell.Runes {
		if r.Name == RuneLaw {
			return r.Count
		}
	}
	return 0
}

// TeleTravelAskInput is what the travel kit ask reads.
type TeleTravelAskInput struct {
	Magic   int
	Members bool
	// Pack is the runes carried; Staff the staff wielded or carried.
	Pack  RuneStock
	Staff string
	// QuestDone answers a quest gate; nil treats every quest as not done.
	QuestDone func(quest string) bool
	// Mule is the mule's published bank, nil when unpublished.
	Mule func(item string) (int, bool)
}

// TeleTravelAskLines is the dedicated travel kit for one mule ask, made when the account has walked enough that a cast it
// could not pay for would have saved (teleTravelMissedTicks): law runes (a FairShare line, rationed at the mule's law floor),
// and the elementals each castable teleport needs, sized for s.Kit casts, each only when the account holds less than a quarter of
// it. Never air runes: ScarabSwarm's ask carries them as the trip kit's rider (TripKitLines), RuneRunner asks for its own.
// Empty when travel is off or the account can cast no teleport its staff makes payable.
func TeleTravelAskLines(s TeleTravelSettings, in TeleTravelAskInput) []AskLine {
	if !s.Enabled {
		return nil
	}
	s = s.Normalised()
	free := TravelFree(in.Staff)
	want := map[string]int{}
	var order []string
	for _, tp := range TravelTeleports() {
		if !TeleportAllowed(tp, in.Magic, in.Members, in.QuestDone, nil) {
			continue
		}
		usable := true
		for _, r := range tp.Spell.Runes {
			if r.Name == RuneAir && !free[RuneAir] && !s.AirRunes {
				usable = false
			}
		}
		if !usable {
			continue
		}
		for _, r := range tp.Spell.Runes {
			if r.Name == RuneAir || free[r.Name] {
				continue // the air rides the same ask as a rider (TripKitLines); RuneRunner asks for its own (runerunnerplan.TravelAirLine)
			}
			n := r.Count * s.Kit
			if r.Name == RuneLaw {
				n += s.LawReserve
			}
			if _, seen := want[r.Name]; !seen {
				order = append(order, r.Name)
			}
			if n > want[r.Name] {
				want[r.Name] = n
			}
		}
	}
	if want[RuneLaw] == 0 {
		return nil // nothing castable: no law line, so no elemental line either
	}
	if in.Mule != nil {
		if laws, known := in.Mule(RuneLaw); known && laws < s.MuleLawFloor {
			return nil // the mule's last laws are rationed: travel stops asking
		}
	}
	var lines []AskLine
	for _, name := range order {
		w := want[name]
		if need := w - in.Pack[name]; need > w*3/4 {
			lines = append(lines, AskLine{Choices: []string{name}, Count: need, FairShare: true})
		}
	}
	if len(lines) > 0 && !AskLinesHave(lines, RuneLaw) && in.Pack[RuneLaw] <= s.LawReserve {
		return nil
	}
	return lines
}

func AskLinesHave(lines []AskLine, item string) bool {
	for _, l := range lines {
		for _, c := range l.Choices {
			if strings.EqualFold(c, item) {
				return true
			}
		}
	}
	return false
}

// TeleportRefusalBlocks is how long a teleport the server refused stays out of travel: the
// refusal ("You haven't learnt how to cast this spell yet", a quest gate, runes) will not change
// in minutes.
const TeleportRefusalBlocks = 60 * time.Minute
