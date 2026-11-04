package loot

import "strings"

// What a source CONSUMES, so the panel can report profit rather than turnover.
//
// The owner, 2026-09-21, in two separate asks that are the same rule:
//
//	"loot panel should consider the cost of a log or any other fletching material when its
//	 putting a profit price or gp earned through fletching"
//	"crystal chest loot total should consider it costs 1 crystal key to open the chest and
//	 would save totals doubling up for the overral"
//
// Until now every figure on the Loot panel was GROSS. A fletching card showed what the shafts
// are worth and said nothing about the logs they were cut from; the Crystal chest card showed
// what the chest paid out and said nothing about the key that opened it. For a source that
// consumes something valuable those are turnover, not earnings, and for fletching they can even
// be negative — fifteen arrow shafts at a coin each against a log worth more than fifteen.
//
// WHERE THE NUMBER COMES FROM, AND WHAT IT DELIBERATELY DOES NOT DO
//
// This file says only WHAT is consumed and HOW MANY per event. It does not price anything:
// pricing belongs to internal/controlpanel's PriceBook, which already resolves a name to what it
// actually trades for and already treats a missing price as UNKNOWN rather than zero. A cost
// whose price the book declines to answer is therefore reported as unknown and NOT subtracted,
// which is the only honest answer — subtracting zero would silently claim the input was free.
//
// THE COUNT IS Events, WHICH IS WHY ActionFletch COUNTS MATERIALS
//
// Every source's Events is "how many times the action happened", and for a consuming source that
// is also how many inputs it ate: one open is one key, one fletch is one log. ActionFletch
// counted PRODUCTS for a few hours on 2026-09-21 and was changed to count materials for exactly
// this reason — a cost of "shafts x price of a log" would have been fifteen times the truth.
// The shafts are not lost by that change; they are the Drop's Items, which is where a tree's
// logs live too.
//
// WHAT WAS CHECKED FOR DOUBLE-COUNTING, AND WHAT TURNED OUT NOT TO BE THERE
//
// The crystal-chest loop looks like it should double-count and mostly does not. Read rather than
// assumed, on 2026-09-21:
//
//   - A key ARRIVING is never counted as chest loot. lootchestfisherrunner/runner.go:491 skips
//     both the unnoted and the noted crystal key ids from the measured gain.
//   - A key JOINED from two halves is never counted either: the join path (internal/bot/
//     lootchestfisher.go's joinLootedKeyHalves, and internal/bot/mule.go's fleet-wide pass)
//     reports no Drop at all. There is no second count to remove.
//   - A key HALF the chest pays out IS counted, at runner.go:493, and that is correct — the
//     chest really paid it. It is also what makes netting the key off matter: those halves
//     become the key that opens the next chest, so counting the halves as income while never
//     subtracting the key turned a loop into a money printer on the panel.
//
// So the single real error was the missing subtraction, and this is it.

// Cost is what ONE event of a source consumes. Per is how many units per event — 1 for every
// source today, and a field rather than an assumption because a recipe that eats two of
// something would otherwise be silently wrong.
type Cost struct {
	ID   int
	Name string
	Per  int64
}

// Consumes reports what one event of s consumes, and false when the source consumes nothing.
//
// Only the two users asked for answer true, and the omissions are deliberate rather than
// unfinished:
//
//   - A CASKET has exactly this shape — opening one consumes the casket, which was itself recorded
//     as loot on the Fishing card — so netting it would be the same one line. It is not done because
//     the owner has not asked, the numbers involved are the panel's biggest (254,683 catches' worth
//     of caskets), and quietly re-basing them is not a change to make on inference. The mechanism is
//     here when he wants it: give KindOpenable a Cost of its own obj id. An OYSTER consumed one
//     oyster per open for part of 7 Oct 2026; later that day users asked that the closed oyster
//     never show at all ("just oyster pearls"), so it is no longer loot when caught and opening it
//     costs nothing (loot.go oysterOpen, HiddenItem).
//   - A KILL, a PICKPOCKET and every gathering source consume nothing. Food and axes are a real
//     running cost, but they are not consumed BY the event and there is no per-event count to
//     multiply, so guessing one would be worse than the gross figure.
func (s Source) Consumes() (Cost, bool) {
	switch {
	case s.MirrorsCost():
		// The Combat spells row carries its runes as measured Spent, never as a fixed input.
		return Cost{}, false
	case IsRunecraft(s):
		// A craft binds however much essence the pack held, which is measured (Drop.Spent), not a fixed
		// input per event; and the row is named for its ALTAR, which is no item (runecraft.go).
		return Cost{}, false
	case s.Kind == KindProduction:
		// A production source is keyed on its INPUT (production.go), so the material is the
		// source's own name and id. One per event, because one event is one material cut —
		// or, on the Prayer card, one bone buried (prayer.go says why netting it is right).
		if s.ID < 0 || s.Name == "" {
			return Cost{}, false
		}
		return Cost{ID: s.ID, Name: s.Name, Per: 1}, true
	case s.Kind == KindOpenable && s.Name == CrystalChestName:
		// One open is one key, whatever the chest gives back: content's
		// [label,open_crystal_chest] does inv_del(inv, crystal_key, 1) before it adds anything
		// (crystalchest.go). Named explicitly rather than taken from s.ID even though the
		// Crystal chest source already uses the key's obj id as its model — the two being equal
		// is a presentation choice that file could revisit, and this must not silently follow it.
		return Cost{ID: CrystalKeyID, Name: CrystalKeyName, Per: 1}, true
	}
	return Cost{}, false
}

// Inputs is everything one event of s consumes: Consumes' input first, then the second input of a
// two-input recipe (craft.go's secondInputs: Bolts for a tip put on a bolt, a Bow string for a bow
// strung). Empty when the source consumes nothing.
//
// Consumes stays the one-input answer; the panel prices a card's cost from Inputs, so a Pearl
// bolts row is net of the tips AND the Bolts it used (a user request: the main account buys those Bolts
// from shops, so they are a real cost, not a free one).
func (s Source) Inputs() []Cost {
	first, ok := s.Consumes()
	if !ok {
		return nil
	}
	out := []Cost{first}
	if s.Kind == KindProduction && s.Skill == SkillFletching {
		if c, ok := secondInputs[s.ID]; ok {
			out = append(out, c)
		}
	}
	if s.Kind == KindProduction && s.Skill == SkillSmithing && s.Action == ActionSmith {
		out = append(out, barSecond[s.ID]...) // the bar's coal or tin (magic.go); an anvil job uses the bar alone
	}
	if s.Kind == KindProduction && (s.Skill == SkillMagic || strings.TrimSuffix(s.Action, runesSuffix) == ActionSuperheat) {
		out = append(out, magicInputs(s)...)
	}
	return out
}

// natureRuneID is the Nature rune a superheat or an alchemy cast uses.
const natureRuneID = 561
