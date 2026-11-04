package loot

import "strings"

// Random-event rewards (2026-09-20, owner request: "random event item rewards, per event, in the
// same Loot panel"). A reward is measured, like every other source: the pack before the
// Guardian starts handling an event and after it finishes, attributed to that event only when
// the item is one the event's own content script can hand out. Nothing is predicted; the table
// below only decides which of the pack's real gains may belong to the event.
//
// The table is content 289's own "macro events" reward code
// (content/scripts/macro events/scripts/general/*.rs2), obj and npc ids from content/pack:
//
//	Strange box         [proc,macro_cube_reward]: coins, uncut sapphire/emerald/ruby/diamond,
//	                    either key half — only once the LAST box is solved (macro_cube_success)
//	Mysterious Old Man  [proc,macro_mysterious_old_man_reward]: kebab, spinach roll, 240 coins,
//	                    casket
//	Drunken Dwarf       [opnpc1,macro_dwarf]: a kebab and a beer
//	Strange plant       [opnpc1,macro_triffidseed]: its fruit
//	Mime                [proc,macro_mime_reward]: a mime costume piece (or two), else 500 coins
//	Maze                [proc,macro_maze_calc_reward] x3 plus the chest's
//	                    [proc,macro_maze_calculate_chest_loot]
//
// The Genie is recorded too, differently from the rest. It WAS excluded ("record nothing", the
// owner's earlier call) because its Lamp is rubbed for Experience and leaves the pack again.
// Reversed on 2026-09-21 at the owner's request: "just have genie show how many lamps its given
// rather than lamps showing up in fishing spots etc". The other half of that request is the
// filter: IsEventOnlyItem keeps the Lamp off every fishing and tree card.
//
// WHY IT STILL SHOWED NOTHING (found 5 Oct 2026, owner: "genie still doesnt show"). The Genie
// was in the table with PaysOnCompletion and no Allowed items, and MeasureEventWindow ends with
// `Completed = len(Reward) > 0` for such an event -- so with an empty Allowed set a Genie could
// never complete, and the window never recorded one. One live fleet.log (build 73ff4228, 4-5 Oct
// 2026) has 133 "Genie — handling" and 80 "Genie — lamp claimed" lines and not one
// `[loot] random event Genie: completed` (the only two Genie loot lines are "nothing recorded"
// ones from crafters whose own flax was being spun). The lamp is also gone by the end of the
// window in the common case (talked, rubbed, "lamp claimed", all inside one handling), so the
// before/after pack readings never see it at all. The Genie is now
// an event that GIVES its Own item (EventReward.GivesOwn): a window counts one hand-over, from
// the readings it took in between (MeasureEventWindowSeen) as well as the end of it, and records
// that Lamp as the Genie's one item per event. A Lamp is the Genie's and nobody else's:
// ForeignEventItem keeps it, and the Strange box, off every other source.
//
// Not recorded, deliberately:
//   - Big fish takes the account's OWN fishing tool and drops it on the ground; picking it back
//     up is not a reward. Whirlpools only delete the tool.
//   - River troll drops land on the ground only when the troll is KILLED; the Guardian flees
//     from event monsters and never kills or loots one.
//   - A reward the server could not fit in a full pack: content's inv_add then puts it on the
//     floor, where nothing picks it up as a reward. It is not in the pack, so it is not counted.
//
// Content 289 has no Sandwich lady, Rick Turpentine, Certer, Frog or Quiz master event.

// KindRandomEvent groups the random-event reward sources in the panel.
const KindRandomEvent = "randomevent"

// ActionEvent is a completed random event; its Events count is completions.
const ActionEvent = "event"

// Model kinds for Source.Model: which cache model the panel draws for a source whose Kind
// alone does not say (a random event is an npc for most events, an item for the Strange box).
const (
	ModelNPC = "npc"
	ModelObj = "obj"
	// ModelLoc is a scenery loc type: a tree, a rock. Gathering sources use it (gather.go).
	ModelLoc = "loc"
)

// Obj ids the random-event measurement treats specially.
const (
	objStrangeBox = 3062 // macro_cube, name=Strange box
	objLamp       = 2528 // macro_genilamp, name=Lamp
)

// ObjCasket is the casket obj id, exported because a gathering filter has to decide for itself
// whether a casket is plausible: big-net fishing produces them and a tree does not.
const ObjCasket = objCasket

// IsEventOnlyItem reports whether an obj id is something ONLY a random event hands out, so a
// gathering window that sees it gained knows it did not come from the tree or the spot.
//
// Built from the eventRewards table rather than a second hand-written list, so an event added
// there is filtered here automatically. Two deliberate exclusions, because a blanket rule would
// throw away real measurements:
//
//   - CASKET is a Mysterious Old Man reward AND a genuine big-net fishing drop. Excluding it
//     everywhere would delete the fleet's single largest loot line.
//   - MITHRIL ORE is a maze reward AND something mining actually produces.
//
// Both stay gatherable and each gathering filter decides for itself: woodcutting produces
// neither, so notAChop excludes them; fishing produces caskets, so notACatch keeps them.
//
// The fleet owner listed the symptom exactly (2026-09-21): "strange fruit steel arrow chaos rune
// iron arrow kebab strange box beer death rune mithril ore nature rune casket spinach roll all
// random event items not from fishing spots or woodcutting spots" -- every one of those is in an
// Allowed set above, which is why this is derived from the table rather than transcribed.
func IsEventOnlyItem(id int) bool {
	if gatherableAnyway[id] {
		return false
	}
	if id == objLamp || id == objStrangeBox {
		return true
	}
	for _, r := range eventRewards {
		if r.Own >= 0 && r.Own == id {
			return true
		}
		if r.Allowed[id] {
			return true
		}
	}
	return false
}

// gatherableAnyway are ids an event can hand out that a gathering skill also genuinely produces.
// Coins are here because a gathering filter already excludes them on its own terms.
//
// NOTED MITHRIL ORE AND COAL WERE HERE AND SHOULD NOT HAVE BEEN (corrected 2026-09-21 by the
// fleet owner: "mithril ore comes from maze event"). I reasoned that mining produces ore, which
// is true in general and irrelevant here: this list is consulted by the FISHING and WOODCUTTING
// filters, and neither a spot nor a tree has ever produced an ore. The one mining script in the
// fleet mines rune essence. So they are event-only after all, and leaving them out of this map
// is what puts a maze's ore on the maze's card.
//
// The casket stays, and the asymmetry is the point: big-net fishing genuinely lands caskets and
// they are the fleet's largest loot line, while a TREE produces none -- which is why notAChop
// excludes the casket on its own terms rather than through this map (owner, same message:
// "woodcutting caskets come from mysterious old man").
// NOTED AND UNNOTED ARE DIFFERENT IDS, and that is what makes this safe for a future miner.
// The maze pays in CERTS: 448 is cert_mithril_ore and 454 is cert_coal. What a pickaxe produces
// is 447 mithril_ore and 453 coal, which appear in no event's Allowed set at all and are
// therefore never filtered. So ProgressiveMiner, when it lands, will have its ore counted on
// its own rock's card with no change here (owner, 2026-09-21: "soon we will have progressive
// miner which will gather unnoted mithril ore so that logic needs to be there").
var gatherableAnyway = map[int]bool{
	objCasket: true, // big-net fishing genuinely lands these; a tree does not, see notAChop
	objCoins:  true,
}

// EventReward describes one random event whose rewards are recorded.
type EventReward struct {
	// Name is the card name; the Guardian's own event name is matched case-insensitively.
	Name string
	// Model and ID pick the card's model: the event npc's type id (ModelNPC), the event item's
	// obj id (ModelObj), or no model at all (Model "" and ID -1: the maze has neither).
	Model string
	ID    int
	// Allowed is every obj id the event's content can hand out. A gain of anything else in the
	// same window came from somewhere else (a fish the server's fishing action still landed,
	// a pickpocket that finished a tick late) and is never attributed to the event.
	Allowed map[int]bool
	// Own is the obj id of the event's own item (the Strange box) or -1. Its losses are the
	// event being solved, not the script touching the pack; and when Own >= 0 the event is
	// complete only when the pack holds none of it any more, which is when content pays out.
	Own int
	// PaysOnCompletion marks an event whose content ALWAYS hands an item over when it is done
	// (the Old Man, the Dwarf, the plant): its completion is the reward arriving, not the
	// Guardian's "cleared" line. Lab 2026-09-20: the Old Man's handling ends "still present"
	// (he lingers two ticks to wave) and he is simply gone by the next poll, so no "cleared" line
	// is ever written; and a re-detected, already-happy Old Man could be "cleared" a second time.
	PaysOnCompletion bool
	// GivesOwn marks an event whose Own item is what the event HANDS OVER, not something the
	// account was given to solve: the Genie's Lamp. It arrives during the handling and is rubbed
	// for Experience before the window closes, so the before/after readings usually cannot see
	// it. The window counts one hand-over per handling -- Own gained against the window's start,
	// on any reading it took, including the end -- as the event's completion, and records that
	// item as its reward (see MeasureEventWindowSeen).
	GivesOwn bool
}

// Source is the loot Source r is tallied and persisted under.
func (r EventReward) Source() Source {
	return Source{Kind: KindRandomEvent, Action: ActionEvent, Name: r.Name, ID: r.ID, Model: r.Model}
}

func ids(list ...int) map[int]bool {
	out := make(map[int]bool, len(list))
	for _, id := range list {
		out[id] = true
	}
	return out
}

// Obj ids from content/pack/obj.pack.
const (
	objCoins        = 995
	objCasket       = 405
	objKebab        = 1971
	objSpinachRoll  = 1969
	objBeer         = 1917
	objTriffidFruit = 464
)

var eventRewards = map[string]EventReward{
	"strange box": {Name: "Strange box", Model: ModelObj, ID: objStrangeBox, Own: objStrangeBox,
		// coins, uncut sapphire, emerald, ruby, diamond, keyhalf1, keyhalf2
		Allowed: ids(objCoins, 1623, 1621, 1619, 1617, 985, 987)},
	"mysterious old man": {Name: "Mysterious Old Man", Model: ModelNPC, ID: 410, Own: -1, PaysOnCompletion: true,
		Allowed: ids(objKebab, objSpinachRoll, objCoins, objCasket)},
	"drunken dwarf": {Name: "Drunken Dwarf", Model: ModelNPC, ID: 956, Own: -1, PaysOnCompletion: true,
		Allowed: ids(objKebab, objBeer)},
	"strange plant": {Name: "Strange plant", Model: ModelNPC, ID: 407, Own: -1, PaysOnCompletion: true,
		Allowed: ids(objTriffidFruit)},
	"mime": {Name: "Mime", Model: ModelNPC, ID: 1056, Own: -1,
		// mime mask, top, legs, gloves, boots; coins once the outfit is complete
		Allowed: ids(3057, 3058, 3059, 3060, 3061, objCoins)},
	// The Genie hands over a Lamp, and the Lamp is spent on Experience. The card counts LAMPS
	// GIVEN: one event, and the Lamp as its one item, per hand-over (GivesOwn). Allowed is
	// deliberately empty: anything else gained while a Genie was on screen came from somewhere
	// else, and the Lamp itself is counted as the hand-over rather than as an allowed gain.
	// npc 409 is macro_geni. It was written as 411 at first, which is macro_SWARM -- so the
	// card drew the event monster's model and the owner spotted it immediately. Read off
	// content's own npc pack rather than guessed this time. Records persisted under 411 are
	// folded onto 409 when they are read back (Migrate).
	"genie": {Name: "Genie", Model: ModelNPC, ID: 409, Own: objLamp, GivesOwn: true, PaysOnCompletion: true,
		Allowed: ids()},
	"maze": {Name: "Maze", Model: "", ID: -1, Own: -1,
		// macro_maze_calc_reward: coins, feather, iron arrow, chaos rune, steel arrow, death
		// rune, noted coal, noted mithril ore, nature rune; the chest: air, water, earth and
		// fire runes, bronze arrows, bolts, iron arrows, strength/attack/defence potions (2)
		Allowed: ids(objCoins, 314, 884, 562, 886, 560, 454, 448, 561,
			556, 555, 557, 554, 882, 877, 117, 123, 135)},
}

// IsEventOwnItem reports whether an obj is a random event's OWN item — the Strange box the
// account is given to solve, or the Genie's Lamp — rather than a reward.
//
// It exists so another source's measurement can refuse to claim one. The owner saw a Strange box
// on the Loot panel's Fishing spot card (2026-09-20): the Mysterious Old Man hands those out,
// and it landed on fishing only because the account happened to be fishing when it arrived.
//
// Deliberately NOT the events' Allowed reward sets. Those overlap real loot from other sources —
// the Mysterious Old Man gives a Casket and the Big net fishing spot gives Caskets too, the maze
// gives feathers and so does a chicken — so skipping every reward id would throw away genuine
// drops. An event's own item has no such overlap: nothing else in the game hands one out. The
// general defence against a reward being mis-attributed is for the measuring window to discard
// itself when an event ran inside it (see fisherrunner/catch.go's taint), not a longer id list.
func IsEventOwnItem(id int) bool {
	if id == objLamp {
		return true
	}
	for _, r := range eventRewards {
		if r.Own >= 0 && r.Own == id {
			return true
		}
	}
	return false
}

// RandomEventReward is the reward description for the Guardian's event name, if that event's
// rewards are recorded. Genie, Lamp and every hazard or monster answer false.
func RandomEventReward(name string) (EventReward, bool) {
	r, ok := eventRewards[strings.ToLower(strings.TrimSpace(name))]
	return r, ok
}

// RandomEventRewards lists every recorded event, for tests and the fake fleet.
func RandomEventRewards() []EventReward {
	out := make([]EventReward, 0, len(eventRewards))
	for _, r := range eventRewards {
		out = append(out, r)
	}
	return out
}

// EventWindow is what one handling window of an event measured.
type EventWindow struct {
	// Reward is the gain attributed to the event: allowed ids only, and for an event with an
	// Own item only in the window that finished it.
	Reward Counts
	// Ignored is every other positive gain in the window (fish, the Lamp, a box replicating).
	Ignored Counts
	// Contested is every item that LEFT the pack in the window other than the event's own
	// item. Nothing a recorded event does removes one, so a loss means the script (or another
	// handler) was working the pack at the same time — a casket opened, a trade — and its
	// gains could be mistaken for the reward. Such a window records nothing.
	Contested Counts
	// Completed is whether this window finished the event: its Own item is gone; for a
	// PaysOnCompletion event, its reward arrived; otherwise the Guardian logged it cleared.
	Completed bool
}

// MeasureEventWindow attributes one window's pack change to r. cleared is whether the
// Guardian logged the event cleared at the end of the window.
func MeasureEventWindow(r EventReward, before, after Counts, cleared bool) EventWindow {
	return MeasureEventWindowSeen(r, before, nil, after, cleared)
}

// MeasureEventWindowSeen is MeasureEventWindow for a window that took readings of its own between
// the first and the last. seen holds, for each obj id, the highest count any of them showed (nil
// for none); only an event that GIVES its Own item reads it, because that item can come and go
// entirely between the two ends of the window. The Genie's Lamp does: the Guardian talks to the
// Genie, rubs the lamp and logs "lamp claimed" inside one handling, so before and after both read
// zero lamps and a window that looked only at them recorded no Genie at all.
//
// One hand-over is one completed event and one item. It is counted when the lamp was ever seen above
// what the window started with, and never from a lamp that was already in the pack at the start:
// that one was handed over, and counted, by an earlier window.
func MeasureEventWindowSeen(r EventReward, before, seen, after Counts, cleared bool) EventWindow {
	w := EventWindow{Reward: Counts{}, Ignored: Counts{}, Contested: Counts{}}
	var handed int64
	if r.GivesOwn && r.Own >= 0 {
		if max(after[r.Own], seen[r.Own]) > before[r.Own] {
			handed = 1
		}
	}
	switch {
	case r.GivesOwn:
		w.Completed = handed > 0
	case r.Own >= 0:
		w.Completed = before[r.Own] > 0 && after[r.Own] == 0
	case !r.PaysOnCompletion:
		w.Completed = cleared
	}
	for id, n := range before {
		if d := after[id] - n; d < 0 && id != r.Own && id != objLamp {
			w.Contested[id] = -d
		}
	}
	for id, n := range after {
		d := n - before[id]
		if d <= 0 {
			continue
		}
		switch {
		case r.GivesOwn && id == r.Own:
			// The hand-over itself, counted once below however many of it the pack ended with.
		case id == r.Own || id == objLamp || id == objStrangeBox:
			w.Ignored[id] = d
		case r.Allowed[id] && (r.Own < 0 || w.Completed):
			w.Reward[id] = d
		default:
			w.Ignored[id] = d
		}
	}
	if handed > 0 {
		w.Reward[r.Own] = handed
	}
	if r.PaysOnCompletion && !r.GivesOwn {
		w.Completed = len(w.Reward) > 0
	}
	// The Strange box event is DELIVERED by a Mysterious Old Man (lab 2026-09-20: ::forcerandom
	// box spawns macro_mage, whose dialogue is "You have been given a Mysterious box!"). That
	// window is the box arriving, not an Old Man event paying out: the box's own window
	// records its reward later.
	if r.Own != objStrangeBox && !r.GivesOwn && w.Ignored[objStrangeBox] > 0 {
		w.Completed = false
	}
	// A loss in the window means the script was working the pack too, so the ambiguous gains of
	// the other events could be its own. An event that hands over its Own item is not ambiguous:
	// nothing but the event produces a Lamp, so a loss elsewhere does not unsay it.
	if len(w.Contested) > 0 && !r.GivesOwn {
		for id, n := range w.Reward {
			w.Ignored[id] += n
		}
		w.Reward = Counts{}
		w.Completed = false
	}
	return w
}

// ForeignEventItem reports whether id is an event's OWN item (IsEventOwnItem) arriving on a source
// that is not that event: the Genie's Lamp on a Cow, the Strange box on a pickpocket.
//
// Nothing but the event hands those out, so such an item is never what src's own action produced.
// It is the pack the account happened to be carrying when the event paid out, picked up by whatever
// window was open: fourteen lifetime cards (Cow, Chicken, the Goblins, a Giant, a Casket, a stall...)
// carried 195 Lamps and Strange boxes between them by 5 Oct 2026 (owner: "currently lamps are
// showing up in wrong sections"). Each measuring window skips them where it can (IsEventOwnItem), and
// Tally.Add applies this to every Drop whichever window made it, so a window that forgets cannot
// put one on the wrong card.
//
// Derived from the same table as everything else here: src may hold the item when its own event's
// Own or Allowed names it.
func ForeignEventItem(src Source, id int) bool {
	if !IsEventOwnItem(id) {
		return false
	}
	if src.Kind != KindRandomEvent {
		return true
	}
	r, ok := RandomEventReward(src.Name)
	return !ok || (r.Own != id && !r.Allowed[id])
}

// WithoutForeignEventItems is items with every ForeignEventItem of src taken out. It returns items
// itself when there is none, and never changes the slice it is given.
func WithoutForeignEventItems(src Source, items []Item) []Item {
	foreign := 0
	for _, it := range items {
		if ForeignEventItem(src, it.ID) {
			foreign++
		}
	}
	if foreign == 0 {
		return items
	}
	out := make([]Item, 0, len(items)-foreign)
	for _, it := range items {
		if !ForeignEventItem(src, it.ID) {
			out = append(out, it)
		}
	}
	return out
}

// CanonicalEventSource is src as the reward table spells it now: the id and model of the event src
// names, which differ from src's own when an earlier build wrote them wrongly. ok is true for any
// KindRandomEvent source the table knows, and false for every other source and for an event it does
// not (src comes back as it was). Migrate uses it.
//
// The live lifetime file held `event:genie:411` -- npc 411 is macro_SWARM, so the panel drew the
// Genie as a Swarm (a user request: "genie was also showing as a swarm last time it displayed")
// -- and `event:mysterious old man:2344`, which is no npc at all (content/pack/npc.pack ends at
// 1595; 2344 is an obj, cert_cooked_oomlie). Both are the same event under a wrong id.
func CanonicalEventSource(src Source) (Source, bool) {
	if src.Kind != KindRandomEvent {
		return src, false
	}
	r, ok := RandomEventReward(src.Name)
	if !ok {
		return src, false
	}
	return r.Source(), true
}
