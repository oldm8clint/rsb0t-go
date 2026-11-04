package fisherplan

import (
	"sort"
	"strings"

	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/pearlplan"
)

// THE DROP CYCLE (a user request): the one tidy-up BassOysterCaskets does of its pack while it
// fishes, instead of banking the caskets and oysters it catches and opening them at the bank:
//
//	"bassoystercasket should also open caskets while it fishes instead of banking them as they can
//	provide coins 99.5% of the time saving inventory space when going to bank them, if we are lucky
//	enough to get both half keys those should be joined before banking too, probably during the
//	dropping cycle along with the other stuff it drops while fishing"
//	"instead of opening the oysters on the way to the bank they should open them on the drop cycle
//	with the fish, leatherstuff and when they open caskets and join keys during fishing"
//	"they should also just drop the empty oyster to save inventory space also as empty oysters dont
//	have a use"
//	"they could also include bringing a chisel with them ... and chisel the oysters while fishing to
//	just be banking the bolt tips from the oysters saving even more inventory space"
//
// This file is the pure half: what one cycle clicks, in what order, and when it is due. The clicks are
// internal/scripts/fisherrunner/cycle.go's, sent by fastopen five to a game tick.
//
// THE CONTENT AND ENGINE FACTS IT RESTS ON (content 289, engine 289; read, not recalled):
//
//   - [opheld1,casket] (skill_fishing/scripts/fishing_spots/memberfish.rs2) deletes the clicked slot
//     and adds one reward: coins (60 in 128, a stack: it frees a slot whenever coins are already held),
//     an uncut sapphire, emerald, ruby or diamond, a cosmic talisman or one of the two key halves (1
//     in 128 each). The reward goes to the lowest empty slot, so it always fits.
//   - [opheld1,oystershell] deletes the lowest slot holding an oyster and adds an Oyster pearl (25 in
//     32) or an Empty oyster (7 in 32): no slot is spent, and an Empty oyster is worth nothing.
//   - [opheldu,keyhalf1] and [opheldu,keyhalf2] both reach [label,join_keys]: one of each half is
//     deleted from the FIRST slot holding it and a crystal key added (crystal_key.rs2), no delay. The
//     two halves are both named "Half of a key" (obj ids 985 and 987), so a pair is two different ids.
//   - [opheldu,chisel] with an Oyster pearl or Oyster pearls is [label,make_bolt_tips]
//     (skill_fletching/scripts/bolts.rs2): Fletching 41, one pearl a use, six Pearl bolttips from an
//     Oyster pearl and 24 from Oyster pearls, onto the one stack of tips, no delay (pearlplan).
//   - [opheld5,_] drops the clicked slot with no delay.
//   - Every one of these is a user event, and the engine reads five of them a tick
//     (ClientGameProtCategory.USER_EVENT, NetworkPlayer.decodeIn), which is the cycle's whole budget:
//     the number of clicks is what costs fishing time, ceil(clicks/5) ticks.
//   - Any OPHELD on the backpack clears the player's interaction (OpHeldHandler.clearPendingAction)
//     and so stops fishing, but leaves the walk queue alone; a cycle in the middle of a walk costs no
//     fishing at all.
//
// TWO BURSTS, NOT ONE. What an open produces (a pearl, an Empty oyster, a key half, the casket's
// reward) is not in the pack until the open has run, so the pearls to cut, the halves to join and the
// Empty oysters to drop are planned from the pack as the opens leave it. The first burst is the opens
// and the junk already held (it overlaps the wait for the opens to land); the second is everything the
// first made possible. With nothing to open the cycle is one burst.

// The names a cycle acts on, as content 289's object configs spell them.
const (
	Casket      = "Casket"
	Oyster      = "Oyster"
	EmptyOyster = "Empty oyster"
	KeyHalf     = "Half of a key"
	Chisel      = "Chisel"
)

// CycleUserEvents is the engine's user events a game tick, so the clicks one tick carries.
const CycleUserEvents = 5

// CycleKind is one kind of click a cycle makes.
type CycleKind string

// The four clicks of a cycle.
const (
	CycleOpen CycleKind = "open"
	CycleJoin CycleKind = "join"
	CycleCut  CycleKind = "cut"
	CycleDrop CycleKind = "drop"
)

// CycleAction is one click of a cycle. Slot and ID name the item clicked; for a join or a cut that is
// the TARGET and UseSlot/UseID the item used on it (a key half used on the other half, the chisel used
// on a pearl). Name is the clicked item's name, for the log.
type CycleAction struct {
	Kind           CycleKind
	Slot, ID       int
	UseSlot, UseID int
	Name           string
}

// CycleOptions are the cycle's switches, each a setting that defaults to on.
type CycleOptions struct {
	// OpenCaskets opens caskets and joins the key halves they give (openCaskets).
	OpenCaskets bool
	// OpenOysters opens oysters; the Empty oyster they leave is dropped, never banked (openOysters).
	OpenOysters bool
	// CutPearls cuts Oyster pearls into Pearl bolttips with a carried chisel once Fletching is 41
	// (cutPearls).
	CutPearls bool
}

// DefaultCycleOptions is every part on.
func DefaultCycleOptions() CycleOptions {
	return CycleOptions{OpenCaskets: true, OpenOysters: true, CutPearls: true}
}

// CycleItem is one non-empty pack slot as the cycle reads it.
type CycleItem struct {
	Slot  int
	ID    int
	Name  string
	Noted bool
}

// CycleView is everything the cycle's rules read.
type CycleView struct {
	// Pack is the non-empty slots, in any order.
	Pack []CycleItem
	Opts CycleOptions
	// Fletching is the live Fletching level, 0 when unknown (no cut is planned then).
	Fletching int
	// Junk is what the script throws away (IsBassOysterJunk for BassOysterCaskets). nil drops nothing.
	Junk func(name string) bool
}

func named(it CycleItem, want string) bool {
	return !it.Noted && strings.EqualFold(strings.TrimSpace(it.Name), want)
}

func bySlot(items []CycleItem) []CycleItem {
	out := append([]CycleItem(nil), items...)
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}

// PlanOpens is the opens a cycle makes: every unnoted Casket (OpenCaskets) and Oyster (OpenOysters),
// lowest slot first. Lowest first is not tidiness: an oyster's inv_del takes the lowest slot holding
// one, so the clicks that name the lowest slots are the ones that find their oyster where they said.
func PlanOpens(v CycleView) []CycleAction {
	var out []CycleAction
	for _, it := range bySlot(v.Pack) {
		if (v.Opts.OpenCaskets && named(it, Casket)) || (v.Opts.OpenOysters && named(it, Oyster)) {
			out = append(out, CycleAction{Kind: CycleOpen, Slot: it.Slot, ID: it.ID, Name: it.Name})
		}
	}
	return out
}

// PlanDrops is the junk the script throws away, unnoted only (a note is a stack of something, not a
// catch), lowest slot first. The Empty oyster is on the list, so an oyster the cycle has just opened
// has its Empty oyster dropped by the second burst.
func PlanDrops(v CycleView) []CycleAction {
	if v.Junk == nil {
		return nil
	}
	var out []CycleAction
	for _, it := range bySlot(v.Pack) {
		if !it.Noted && v.Junk(strings.TrimSpace(it.Name)) {
			out = append(out, CycleAction{Kind: CycleDrop, Slot: it.Slot, ID: it.ID, Name: it.Name})
		}
	}
	return out
}

// PlanJoins is the key halves joined: one use of one half on the other for every pair the pack holds
// (OpenCaskets). The two halves have the same name and two ids, so the ids found in the pack decide:
// two different unnoted "Half of a key" ids make pairs, one id alone makes none (the other half is in
// the bank, or not yet found). Pairs are taken lowest slot first, because content's inv_del takes the
// first slot holding each half.
func PlanJoins(v CycleView) []CycleAction {
	if !v.Opts.OpenCaskets {
		return nil
	}
	var halves []mulereceiverplan.HalfSlot
	ids := map[int]bool{}
	for _, it := range v.Pack {
		if named(it, KeyHalf) {
			halves = append(halves, mulereceiverplan.HalfSlot{Slot: it.Slot, ID: it.ID})
			ids[it.ID] = true
		}
	}
	if len(ids) != 2 {
		return nil
	}
	var pair []int
	for id := range ids {
		pair = append(pair, id)
	}
	sort.Ints(pair)
	var out []CycleAction
	for _, p := range mulereceiverplan.PairHalves(halves, pair[0], pair[1], len(halves)) {
		// The target is the half of the higher id, the used item the half of the lower: either way
		// round reaches join_keys (the engine falls back to the used item's trigger).
		out = append(out, CycleAction{Kind: CycleJoin, Slot: p.To.Slot, ID: p.To.ID, UseSlot: p.From.Slot, UseID: p.From.ID, Name: KeyHalf})
	}
	return out
}

// PlanCuts is the pearls cut into Pearl bolttips: one chisel use on every unnoted Oyster pearl or
// Oyster pearls the pack holds, lowest slot first, when CutPearls is on, Fletching has reached
// pearlplan.Level and the pack holds a chisel. Nothing otherwise: below 41 the engine answers with a
// message and no tips, and without the chisel there is nothing to use.
func PlanCuts(v CycleView) []CycleAction {
	if !v.Opts.CutPearls || v.Fletching < pearlplan.Level {
		return nil
	}
	chisel, ok := ChiselIn(v.Pack)
	if !ok {
		return nil
	}
	var out []CycleAction
	for _, it := range bySlot(v.Pack) {
		if it.Noted {
			continue
		}
		if k, ok := pearlplan.KindByID(int32(it.ID)); ok && int32(it.ID) == k.ID {
			out = append(out, CycleAction{Kind: CycleCut, Slot: it.Slot, ID: it.ID, UseSlot: chisel.Slot, UseID: chisel.ID, Name: it.Name})
		}
	}
	return out
}

// ChiselIn is the first unnoted chisel in the pack.
func ChiselIn(pack []CycleItem) (CycleItem, bool) {
	for _, it := range bySlot(pack) {
		if named(it, Chisel) {
			return it, true
		}
	}
	return CycleItem{}, false
}

// PlanTidy is the cycle's second burst, planned from the pack as the opens left it: the joins, then
// the cuts, then the drops. None depends on another's result, so they go out together, five a tick.
func PlanTidy(v CycleView) []CycleAction {
	out := PlanJoins(v)
	out = append(out, PlanCuts(v)...)
	return append(out, PlanDrops(v)...)
}

// PlanFirstBurst is the cycle's first burst: the opens, then the junk already held. The junk rides
// behind the opens so that its drops go out while the opens are still landing.
func PlanFirstBurst(v CycleView) []CycleAction {
	opens := PlanOpens(v)
	if len(opens) == 0 {
		return nil
	}
	return append(opens, PlanDrops(v)...)
}

// HasWork reports whether a cycle over this pack would click anything at all.
func HasWork(v CycleView) bool {
	return len(PlanOpens(v)) > 0 || len(PlanTidy(v)) > 0
}

// CycleDue reports whether a cycle should interrupt FISHING now: the pack is nearly full (the same
// JunkDropFreeSlots the junk drop always used: one Big net roll can land more than one item, and what
// does not fit falls on the floor) and there is something for the cycle to do. Dropping, opening and
// cutting all clear the fishing interaction, so one cycle per pack's worth is one re-click of the spot.
func CycleDue(freeSlots int, work bool) bool {
	return work && freeSlots <= JunkDropFreeSlots
}

// CycleTicks is how many game ticks a cycle of this many clicks holds the account for: the clicks at
// five a tick, a tick for the first burst's confirmation and a tick for the second's.
func CycleTicks(clicks int) int {
	if clicks <= 0 {
		return 0
	}
	return (clicks+CycleUserEvents-1)/CycleUserEvents + 2
}

// walkArrive is webwalk's re-click radius: the walker clicks again once this close to its target.
const walkArrive = 4

// CycleWalkAhead is the least distance (tiles) a walk's click target must still be ahead for a cycle
// of this many clicks to fit inside the walk: the walker's re-click radius plus the tiles covered in
// the cycle's ticks (two a tick running, one walking). A cycle is not delayed by walking and does not
// delay it (no click of it has a delay: the walk queue is left alone), so the only cost is the walker's
// own poll, which waits out the burst; with the target this far off it has nothing to do in that time.
func CycleWalkAhead(running bool, clicks int) int {
	per := 1
	if running {
		per = 2
	}
	return walkArrive + per*CycleTicks(clicks)
}

// WalkCycleOK decides whether a poll of a walk starts a cycle of this many clicks (the clicks the pack
// holds now, before what the opens will make) with this many free slots, the walker's click target ahead
// tiles away. It does when the cycle fits inside the walk (CycleWalkAhead, with two clicks over for what the
// opens make): no fishing time is spent at all. When it does not fit, a hop between two fishing spots is a
// few tiles, it still does when the pack is nearly full (CycleDue): that cycle is due at the next hold anyway,
// and starting it on a walk costs the account only the ticks it outlasts the walk by, where ending a hold for
// it costs the whole cycle and a re-click.
//
// DROP ON THE HOP (a user request: "bassoystercasket should drop fish while it walks to different
// fishing spots instead of dropping at the fishing spot"). Every junk slot is dropped sooner or later, so a
// hop with WalkCycleMinClicks or more clicks to make runs the cycle whatever room the pack has left: the
// ticks it outlasts the hop by are fewer than the cycle and re-click the spot would cost later. Only a
// cycle for a slot or two waits, since it would spend the same fixed ticks for less.
func WalkCycleOK(ahead int, running bool, clicks, freeSlots int) bool {
	if clicks <= 0 {
		return false
	}
	if ahead >= CycleWalkAhead(running, clicks+2) || clicks >= WalkCycleMinClicks {
		return true
	}
	return CycleDue(freeSlots, true)
}

// WalkCycleMinClicks is the fewest clicks a cycle on a hop between spots runs for when the pack still has
// room (WalkCycleOK).
const WalkCycleMinClicks = 3

// CycleCounts is what a list of actions does, by kind.
type CycleCounts struct {
	Opens, Joins, Cuts, Drops int
}

// Count totals actions by kind.
func Count(actions []CycleAction) CycleCounts {
	var c CycleCounts
	for _, a := range actions {
		switch a.Kind {
		case CycleOpen:
			c.Opens++
		case CycleJoin:
			c.Joins++
		case CycleCut:
			c.Cuts++
		case CycleDrop:
			c.Drops++
		}
	}
	return c
}

// Total is every click.
func (c CycleCounts) Total() int { return c.Opens + c.Joins + c.Cuts + c.Drops }
