// Package pearlplan is the pure half of MasterCrafter's pearl round: the mule's oyster pearls cut
// into Pearl bolttips with a chisel, and the tips put on the mule's Bolts to make Pearl bolts. It
// holds content 289's facts about the items and the two actions, and the arithmetic of a round; it
// imports nothing that does I/O.
//
// THE CONTENT (content/scripts, branch 289):
//
//   - The items (skill_fishing/configs/fishing.obj; skill_fletching/configs/bolts/bolts.obj;
//     skill_combat/configs/ranged/bolts.obj; ids from pack/obj.pack):
//     "Oyster pearl" smalloysterpearls 411 (note 412), not stackable, members;
//     "Oyster pearls" bigoysterpearls 413 (note 414), not stackable, members;
//     "Pearl bolttips" pearl_bolttips 46, stackable, members, category_530;
//     "Bolts" bolt 877, stackable (the plain bolt, the only one tips go on);
//     "Pearl bolts" pearl_bolt 880, stackable, members.
//   - The tips (skill_crafting/scripts/gem/uncut_gem.rs2 [opheldu,chisel]: case smalloysterpearls,
//     bigoysterpearls -> @make_bolt_tips; skill_fletching/scripts/bolts.rs2 [label,make_bolt_tips];
//     skill_fletching/configs/bolts/bolts.dbrow fletching_oyster_pearl_tips and
//     fletching_oyster_pearls_tips): a FLETCHING action, not Crafting: Fletching 41, members world
//     only, one pearl a use (inv_del 1), 6 tips from an Oyster pearl and 24 from Oyster pearls,
//     experience 32 tenths (3.2 Fletching XP) a use, no delay.
//   - The bolts (bolts.rs2 [opheldu,bolt] / [opheldu,_category_530] -> [label,make_bolts];
//     bolts.dbrow fletching_pearl_bolts): Fletching 41, members world only, up to TEN bolts a use
//     (min of the tips, the Bolts and the row's count 10), 32 tenths (3.2 XP) a bolt, no delay. Both
//     inputs and the product stack, so a use leaves the two stacks where they were and five uses a
//     tick naming the same two slots make fifty bolts a tick.
package pearlplan

import (
	"regexp"
	"strconv"
	"strings"
)

// Item names, as the game's object config and the mule's published bank spell them.
const (
	Pearl     = "Oyster pearl"
	Pearls    = "Oyster pearls"
	Tips      = "Pearl bolttips"
	Bolt      = "Bolts"
	PearlBolt = "Pearl bolts"
)

// Object ids (content pack/obj.pack).
const (
	PearlID     int32 = 411
	PearlNoteID int32 = 412
	PearlsID    int32 = 413
	PearlsNote  int32 = 414
	TipsID      int32 = 46
	BoltID      int32 = 877
	PearlBoltID int32 = 880
)

const (
	// Level is the Fletching level both actions need (bolts.dbrow: level,41 on all three rows).
	Level = 41
	// XPTenths is the experience of one tip use (a pearl) and of one bolt made, in content's tenths.
	XPTenths = 32
	// BoltsPerUse is the most bolts one use of tips on bolts makes (fletching_pearl_bolts:
	// product,pearl_bolt,10).
	BoltsPerUse = 10
	// UsesPerTick is the engine's five user events a player a game tick.
	UsesPerTick = 5
	// TickMillis is the game tick.
	TickMillis = 600
)

// Kind is one pearl item.
type Kind struct {
	Name     string
	ID, Note int32
	// Tips is how many Pearl bolttips one chisel use on it makes.
	Tips int
}

// Kinds are the two pearl items, the most tips a use first (so a round that runs out of time has
// made the most tips it could).
var Kinds = []Kind{
	{Name: Pearls, ID: PearlsID, Note: PearlsNote, Tips: 24},
	{Name: Pearl, ID: PearlID, Note: PearlNoteID, Tips: 6},
}

// KindByID is the pearl kind of an object id (the item or its note).
func KindByID(id int32) (Kind, bool) {
	for _, k := range Kinds {
		if id == k.ID || id == k.Note {
			return k, true
		}
	}
	return Kind{}, false
}

// Stock is a published bank count by item name; ok false when it is not known.
type Stock func(item string) (count int, ok bool)

func count(s Stock, item string) int {
	if s == nil {
		return 0
	}
	n, ok := s(item)
	if !ok || n < 0 {
		return 0
	}
	return n
}

// Held is the pearl round's items in one place (the mule's published bank, or this account's own).
type Held struct {
	Pearl, Pearls, Tips, Bolts int
}

// HeldIn reads the four counts from a published bank.
func HeldIn(s Stock) Held {
	return Held{Pearl: count(s, Pearl), Pearls: count(s, Pearls), Tips: count(s, Tips), Bolts: count(s, Bolt)}
}

// PearlCount is the pearl items, of both kinds: each is one chisel use.
func (h Held) PearlCount() int { return h.Pearl + h.Pearls }

// TipsToCome is the tips the pearls will make, on top of the tips already held.
func (h Held) TipsToCome() int { return h.Pearl*6 + h.Pearls*24 }

// Add is the two holdings together.
func (h Held) Add(o Held) Held {
	return Held{Pearl: h.Pearl + o.Pearl, Pearls: h.Pearls + o.Pearls, Tips: h.Tips + o.Tips, Bolts: h.Bolts + o.Bolts}
}

// BoltUses is how many uses of tips on bolts make every bolt tips and bolts allow: ten a use.
func BoltUses(tips, bolts int) int {
	n := min(tips, bolts)
	if n <= 0 {
		return 0
	}
	return (n + BoltsPerUse - 1) / BoltsPerUse
}

// Line is one line of a kit order to the mule.
type Line struct {
	Item string
	Qty  int
}

// Ask is a pearl round's kit order.
type Ask struct {
	Lines []Line
	// Pearls, Bolts and Tips are what the lines ask for.
	Pearls, Bolts, Tips int
}

// PlanAsk is the order a pearl round makes of the mule: EVERY pearl it publishes, of both kinds,
// and EVERY Bolts (a user request: "request them all plus all the bolts the mule is holding").
// The mule's own caps (anyMaxPerOrder, anyPerWindow) may clamp a line; what is left is asked for
// again later in the round.
func PlanAsk(mule Held) Ask {
	var a Ask
	for _, k := range Kinds {
		n := mule.Pearl
		if k.ID == PearlsID {
			n = mule.Pearls
		}
		if n > 0 {
			a.Lines = append(a.Lines, Line{Item: k.Name, Qty: n})
			a.Pearls += n
		}
	}
	if mule.Bolts > 0 {
		a.Lines = append(a.Lines, Line{Item: Bolt, Qty: mule.Bolts})
		a.Bolts = mule.Bolts
		// The mule's own Pearl bolttips go on those Bolts too (2 Nov 2026: the mule held 4.3M tips and 975k Bolts while the
		// round waited for tips in this account's own bank, and none was made).
		if n := min(mule.Tips, mule.Bolts); n > 0 {
			a.Lines = append(a.Lines, Line{Item: Tips, Qty: n})
			a.Tips = n
		}
	}
	return a
}

// Readiness says whether a pearl round is worth starting, and why not.
type Readiness struct {
	Ready bool
	// Resume is a round started for what this account's own bank still holds from an earlier one.
	Resume bool
	Why    string
}

// Ready decides a pearl round from the mule's published bank and this account's own (owner, 6 Oct
// 2026: "20,000 bolts for pearl bolt making ... 20k oyster pearls must be in the mule's or the main account's bank
// before taking that on"): the oyster pearls (both kinds) in the two banks together reach
// pearlThreshold, or this account's Pearl bolttips have boltThreshold Bolts to go on, the two banks
// together. A round its own bank funds alone is a Resume. Fletching must be at least Level (members
// world assumed: the caller checks it).
func Ready(pearlThreshold, boltThreshold, fletching int, fletchingKnown bool, mule, own Held, muleKnown bool) Readiness {
	switch {
	case !fletchingKnown:
		return Readiness{Why: "Fletching is not known yet"}
	case fletching < Level:
		return Readiness{Why: "Fletching " + strconv.Itoa(fletching) + " is under the " + strconv.Itoa(Level) + " pearl bolt tips need"}
	case own.PearlCount() >= pearlThreshold:
		return Readiness{Ready: true, Resume: true, Why: "this account's bank holds " + strconv.Itoa(own.PearlCount()) + " oyster pearls to cut (threshold " + strconv.Itoa(pearlThreshold) + ")"}
	case own.Tips > 0 && own.Bolts >= boltThreshold:
		return Readiness{Ready: true, Resume: true, Why: "this account's bank holds " + strconv.Itoa(own.Tips) + " Pearl bolttips and " + strconv.Itoa(own.Bolts) + " Bolts to tip (threshold " + strconv.Itoa(boltThreshold) + ")"}
	case !muleKnown:
		return Readiness{Why: "the mule has not published its bank"}
	}
	pearls, bolts := own.PearlCount()+mule.PearlCount(), own.Bolts+mule.Bolts
	switch {
	case pearls >= pearlThreshold:
		return Readiness{Ready: true, Why: "the mule's bank and this account's hold " + strconv.Itoa(pearls) + " oyster pearls together (threshold " + strconv.Itoa(pearlThreshold) + ") and " + strconv.Itoa(bolts) + " Bolts"}
	case own.Tips > 0 && bolts >= boltThreshold:
		return Readiness{Ready: true, Why: "this account's bank holds " + strconv.Itoa(own.Tips) + " Pearl bolttips and the two banks " + strconv.Itoa(bolts) + " Bolts to tip (threshold " + strconv.Itoa(boltThreshold) + ")"}
	case mule.Tips > 0 && bolts >= boltThreshold:
		return Readiness{Ready: true, Why: "the mule's bank holds " + strconv.Itoa(mule.Tips) + " Pearl bolttips and the two banks " + strconv.Itoa(bolts) + " Bolts to tip (threshold " + strconv.Itoa(boltThreshold) + ")"}
	}
	why := "the mule's bank and this account's hold " + strconv.Itoa(pearls) + " oyster pearls, under the threshold " + strconv.Itoa(pearlThreshold)
	if bolts >= boltThreshold {
		return Readiness{Why: why + ", and " + strconv.Itoa(bolts) + " Bolts but no Pearl bolttips in this account's bank to put on them"}
	}
	return Readiness{Why: why + ", and " + strconv.Itoa(bolts) + " Bolts, under the threshold " + strconv.Itoa(boltThreshold)}
}

// ---- the chat ----

var levelRefusal = regexp.MustCompile(`You need a Fletching level of (\d+) to`)

// Refusal reads a pearl action's chat for content's refusals: the Fletching level (bolts.rs2's
// mesbox "You need a Fletching level of 41 to do that." and doubleobjbox "... to make that kind of
// bolt."), "refused" with the level; the members-world line (^mes_members_fletching in
// general/configs/free_to_play.constant, "You need to be on a members' world to gain experience in
// Fletching.", and the engine's own "To use this item please login to a members' server."),
// "members".
func Refusal(said string) (string, int) {
	if m := levelRefusal.FindStringSubmatch(said); m != nil {
		n, _ := strconv.Atoi(m[1])
		return "refused", n
	}
	if strings.Contains(said, "members' world to gain experience in Fletching") || strings.Contains(said, "login to a members' server") {
		return "members", 0
	}
	return "", 0
}

// ---- rates, for the log and the report ----

// TipXPPerHour is the Fletching XP an hour of cutting pearls makes when a bank visit is bankTicks
// ticks a load of 27 pearls (five uses a tick otherwise).
func TipXPPerHour(bankTicks int) int {
	load := 27
	ticks := (load+UsesPerTick-1)/UsesPerTick + bankTicks
	loadsPerHour := 3600 * 1000 / (ticks * TickMillis)
	return loadsPerHour * load * XPTenths / 10
}

// BoltXPPerHour is the Fletching XP an hour of tipping bolts makes: fifty bolts a tick.
func BoltXPPerHour() int {
	return 3600 * 1000 / TickMillis * UsesPerTick * BoltsPerUse * XPTenths / 10
}
