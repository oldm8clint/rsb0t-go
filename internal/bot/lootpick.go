package bot

import (
	"strings"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
)

// PICKING, on a card of its own (a user request: "flax picking isnt considered on the loot panel").
//
// Content 289 picks a flax in one script step (general_use/scripts/pickables.rs2, [oploc2,flax]): it
// prints "You pick some flax." and adds exactly one Flax to the pack, and prints nothing when the pack is
// full. So the line alone is the pick, and every script that picks flax (ProgressiveCrafter's Seers round,
// ScarabSwarm's quest crafting, FlaxPicker) reaches the card without reporting anything.
//
// The card is a gathering card titled "Picking", one row per plant, drawn with the plant's loc. Its flax
// is loot here and an input wherever it is spun (the Crafting card nets it off the bow strings), so the
// two cards together say what picking and spinning each earned.

// pickable is one plant whose pick prints a line and adds one obj.
type pickable struct {
	obj  int    // the obj added (content 289 obj.pack)
	loc  int    // the plant's loc, for the card's picture
	name string // the row's name
}

// pickLines maps content's pick line to the plant it picked.
var pickLines = map[string]pickable{
	"you pick some flax.": {obj: 1779, loc: 2646, name: "Flax"},
}

// skillPicking titles the Picking card; it is not a game skill, only the card's name.
const skillPicking = "Picking"

// actionPick is one plant picked, for the panel's verb.
const actionPick = "pick"

// pickDrop is the drop one pick line records, and whether the line is one.
func pickDrop(text string) (pickable, bool) {
	p, ok := pickLines[strings.ToLower(strings.TrimSpace(text))]
	return p, ok
}

// pickLogEvery spaces the log line: the first pick of a run, then every this many.
const pickLogEvery = 500

// watchPicking starts the pick watch on this Context's session, for as long as the Runner runs. A no-op
// with no loot sink or no session. The subscription is made through On, so the Runner's
// disposeSubscriptions cancels it with the others.
func (c *Context) watchPicking() {
	if c == nil || c.Loot == nil || c.gameState() == nil {
		return
	}
	n := 0
	c.On(func(ev gamestate.Event) {
		if ev.ChatType != gamestate.ChatGame {
			return
		}
		p, ok := pickDrop(ev.Text)
		if !ok {
			return
		}
		c.Loot(loot.Drop{
			Source: loot.Gathering(skillPicking, actionPick, loot.ModelLoc, p.loc, p.name),
			Events: 1,
			Items:  loot.Items(c.Config, loot.Counts{p.obj: 1}),
		})
		if n++; n == 1 || n%pickLogEvery == 0 {
			c.logf("loot: picked %s (%d this run)", p.name, n)
		}
	}, gamestate.EventChatMessage)
}
