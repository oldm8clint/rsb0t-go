package loot

import (
	"time"

	"rsb0t-go/internal/gamestate"
)

// MEASURING A TAKE: what a pickup put in the pack, and when it must not be believed.
//
// A kill is reported when it lands, with no items. What it dropped is taken afterwards and measured
// as the pack's own change around the take (internal/bot/lootcombat.go, scarabrunner/lootreport.go).
// That is a window like any other, and a random event that pays out inside it puts its reward in the
// same pack. Live on 5 Oct 2026 the lifetime Cow card held Feather x5,014, Casket x70, Mithril ore
// x59, Coal x265 and Strange fruit x17 -- none of which a cow drops -- and the Chicken and Goblin
// cards the same, with Lamps. The window is from before the walk to the bone to after the pickup,
// and the Guardian can take the account's action lock for an event (seconds for a Genie, minutes
// for the Maze) anywhere inside it.
//
// The answer is the one loot.Window already gives a continuous activity (window.go, "taint"): when
// an interruption ran inside the window, do not report it. Here the interruption is a random event,
// which only the account's Guardian can see, so a window is handed a Disturbed func that asks it.
// Throwing a disturbed take away costs a bone now and then, since events are a fraction of a percent
// of an account's time; filing a Maze's feathers under Cow costs the card its meaning.
//
// Not by item id. The events' reward tables overlap real drops -- a giant really does drop runes
// and arrows, a chicken feathers, a goblin coins -- so no list of ids can tell a Maze's chaos runes
// from a giant's. The one list that is safe is an event's OWN item, which nothing else hands out
// (IsEventOwnItem): that is skipped in every case, disturbed or not.

// Reading is the pack as it stood when a take began, with a way to ask whether a random event ran
// since. Counts is what the take is measured against.
type Reading struct {
	Counts Counts
	// Disturbed reports whether a random event was handled at any point since the reading was taken,
	// including one still running or only just finished (randomevents.Activity.Disturbed). Nil means
	// there is nothing to ask -- no Guardian, a test -- and reads as never.
	Disturbed func() bool
}

// TakeGained is what a take put in the pack since before: WaitGained, polled for up to maxWait,
// with an event's own item skipped (plus whatever else skip names). discarded is true when
// something did arrive but a random event ran during the take, so none of it can be attributed to
// the pickup; gained is nil then, and a caller records nothing.
//
// A nil before is not recording at all: nothing gained, not discarded.
func TakeGained(read func() *gamestate.Inventory, before *Reading, delay func(time.Duration) bool, maxWait time.Duration, skip func(id int) bool) (gained Counts, discarded bool) {
	if before == nil {
		return nil, false
	}
	gained = WaitGained(read, before.Counts, delay, maxWait, func(id int) bool {
		return IsEventOwnItem(id) || (skip != nil && skip(id))
	})
	// Asked AFTER the wait, so an event that began while the pickup was still arriving is seen.
	if len(gained) > 0 && before.Disturbed != nil && before.Disturbed() {
		return nil, true
	}
	return gained, false
}
