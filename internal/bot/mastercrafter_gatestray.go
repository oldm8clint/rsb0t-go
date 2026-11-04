package bot

// The Nature gate's stray pack items. The gate pack is the talisman, the Nature stack, the fare and (road mode) a stack of noted bows;
// the visit at the mule's post banked everything else. A trade of 25 essence needs 25 free slots, so one more item in the pack is a trade
// of 24, and the carriers' Offer-All of 25 is declined (DecideGateTrade).
//
// Live 16 Oct 2026 a Strange plant random event (the guardian picks its fruit to make it go away) left a Strange fruit in the pack at
// 16:47. From then on the log shows 121 "declined: offered 25 essence and only 24 slots are free" lines up to 20:38, 26 of the 50 carrier
// shift endings, six carriers' shifts ended inside 70 seconds, and the crafter's trades an hour falling from 134 to 8. The only thing that
// ever removed a fruit was a travel step eating one for run energy (17:17); the fruit came back with the next Strange plant.
//
// Two things stop that now. The carriers offer what the crafter says he takes (runecraftrunner.OfferOptions.Cap, the gate message's
// capacity, republished the moment the pack changes: capacityChanged), and the crafter takes the fruit out of the pack:
//
//   - A Strange fruit is EATEN (Eat is op 1; content consume.rs2 [opheld1,macro_triffidfruit]: no p_delay, it restores run energy and
//     gives poison immunity). It is eaten in the gate's idle polls only, with one click and no wait: never while a trade screen is
//     open or an answer is out (an op on an item clears a pending action like an op on a player, so it would close the screen for
//     both), never while a request waits (the answer comes first), never in a cast's lock (the engine drops a click read while the
//     player is delayed), and with at most gateStrayTries clicks, gateStrayEvery apart, for one fruit.
//   - A Drunken Dwarf's Kebab and Beer are eaten and drunk, and a random event's Casket is opened (its coins join the fare's stack), the
//     same way: one click in an idle poll (gateConsumables). Live 17 Oct 2026 the three sat in the pack from 06:39 and every trade
//     took 21 essence.
//   - A stray worth little (a store value of gateStrayDropMax or less: the Beer's empty glass, a Casket's uncut sapphire) is DROPPED the
//     same way. A held slot costs a 25th of every trade, about 160 essence an hour at 4k an hour (some 1,300 Nature runes over an
//     8-hour round), more than such an item is worth; live 17 Oct 2026 the glass and the sapphire kept every trade at 22 after the
//     Kebab, the Beer and the Casket had gone.
//   - Anything dearer is NOT touched: it is named in a warning every ten minutes and the gate says, truthfully, that it takes less than
//     25; the next round's visit at the mule's post banks it.

import (
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftrunner"
)

const (
	// gateStrayLook is how often the pack is looked at for strays; gateStrayEvery how soon an Eat click is sent again when the fruit is
	// still there (two and a half ticks, the eat delay and the latency of the pack update); gateStrayTries how many clicks one fruit
	// gets before the gate gives up on it and says so.
	gateStrayLook  = time.Second
	gateStrayEvery = 1500 * time.Millisecond
	gateStrayTries = 6
	// gateStrayWarn is how often a stray that is not eaten is named.
	gateStrayWarn = 10 * time.Minute
)

// gateStrayState is the stray items' bookkeeping for a round.
type gateStrayState struct {
	lookAt, eatAt time.Time
	// tries are the Eat clicks sent for the fruit now in the pack; eaten the fruit eaten this round.
	tries, eaten int
	said         map[string]time.Time
	// usedAt is when the last consumable click was sent; used the clicks per object id this round.
	usedAt time.Time
	used   map[int32]int
}

// gateConsumables are the random-event rewards the gate uses up rather than carries, by name, with the inventory option that does it.
var gateConsumables = map[string]string{"Kebab": "Eat", "Beer": "Drink", "Casket": "Open"}

// gateStrayDropMax is the store value (the object config's cost) up to which a stray the gate cannot use up is dropped.
const gateStrayDropMax = 3000

// gateStrayNeverDrop are the random-event items the guardian itself uses (a Genie's Lamp is rubbed, a Strange box solved): they are
// cheap by store value and must never be dropped before it does.
var gateStrayNeverDrop = map[string]bool{"Lamp": true, "Strange box": true, "Book of knowledge": true, "Mystery box": true}

func (s *gateStrayState) say(key string, every time.Duration, f func()) {
	if s.said == nil {
		s.said = map[string]time.Time{}
	}
	if t, ok := s.said[key]; ok && time.Since(t) < every {
		return
	}
	s.said[key] = time.Now()
	f()
}

// clearStray eats a Strange fruit the pack holds (one click, no wait) and names any other stray. It runs in the gate's idle polls.
func (w *masterNatureWorld) clearStray() {
	s := &w.stray
	now := time.Now()
	if now.Sub(s.lookAt) < gateStrayLook {
		return
	}
	s.lookAt = now
	d := w.d
	fruit := fleetrunner.StrangeFruitHeld(d)
	if fruit <= 0 {
		if s.tries > 0 {
			s.eaten++
			w.logf("stray: the Strange fruit is gone from the pack (%d eaten this round): the gate takes %d essence a trade", s.eaten, runecraftrunner.TradeCapacity(d))
			s.tries = 0
		}
		if w.useConsumable() {
			return
		}
		w.warnStrays()
		return
	}
	switch {
	case s.tries >= gateStrayTries:
		s.say("giveup", gateStrayWarn, func() {
			w.logf("WARNING: %d Strange fruit stay in the pack after %d Eat clicks: the gate takes %d essence a trade while they do", fruit, s.tries, runecraftrunner.TradeCapacity(d))
		})
		return
	case now.Sub(s.eatAt) < gateStrayEvery, w.watch.Pending() > 0, w.alchLocked(), randomevents.Busy(d.Session), actions.NewTrade(d.Session).Active():
		return
	}
	s.eatAt = now
	s.tries++
	if s.tries == 1 {
		w.logf("stray: %d Strange fruit (a random event's) hold a pack slot a trade of 25 essence needs: the gate takes %d a trade; eating it", fruit, runecraftrunner.TradeCapacity(d))
	}
	if err := fleetrunner.SendEatStrangeFruit(d); err != nil {
		w.logf("WARNING: eating the Strange fruit: %v (try %d of %d)", err, s.tries, gateStrayTries)
	}
}

// useConsumable sends one click that uses up a consumable stray (gateConsumables) or drops a cheap one (gateStrayDropMax), under the same
// rules as the fruit's Eat: idle polls only, gateStrayEvery apart, at most gateStrayTries clicks for one kind in a round. It reports
// whether it clicked.
func (w *masterNatureWorld) useConsumable() bool {
	s := &w.stray
	d := w.d
	now := time.Now()
	if now.Sub(s.usedAt) < gateStrayEvery || w.watch.Pending() > 0 || w.alchLocked() || randomevents.Busy(d.Session) || actions.NewTrade(d.Session).Active() {
		return false
	}
	if w.ctx == nil || w.ctx.Config == nil {
		return false
	}
	for _, it := range runecraftrunner.Strays(d, w.alch.bows) {
		o, ok := w.ctx.Config.Obj(int(it.ID))
		if !ok {
			continue
		}
		verb, ok := gateConsumables[o.Name]
		if !ok {
			if o.Cost > gateStrayDropMax || int(it.ID) == fleetplan.StrangeFruitID || gateStrayNeverDrop[o.Name] {
				continue
			}
			verb = "Drop"
		}
		op := actions.OptionIndex(actions.HeldOps(o.IOps), verb)
		if op == 0 {
			continue
		}
		if s.used == nil {
			s.used = map[int32]int{}
		}
		if s.used[it.ID] >= gateStrayTries {
			continue
		}
		s.used[it.ID]++
		s.usedAt = now
		if s.used[it.ID] == 1 {
			w.logf("stray: %s x%d (store value %d gp) holds a pack slot a trade of 25 essence needs: %s", o.Name, max(1, int(it.Count)), o.Cost, strings.ToLower(verb))
		}
		if err := actions.OpHeld(d.Session, op, it.ID, it.Slot, fleetrunner.PlayerPack); err != nil {
			w.logf("WARNING: %s %s: %v (try %d of %d)", strings.ToLower(verb), o.Name, err, s.used[it.ID], gateStrayTries)
		}
		return true
	}
	return false
}

// warnStrays names the pack items that are neither the gate's nor a Strange fruit (which clearStray deals with), at most every ten minutes.
func (w *masterNatureWorld) warnStrays() {
	var names []string
	for _, it := range runecraftrunner.Strays(w.d, w.alch.bows) {
		if int(it.ID) == fleetplan.StrangeFruitID {
			continue
		}
		names = append(names, w.itemName(int(it.ID))+" x"+strconv.Itoa(int(it.Count)))
	}
	if len(names) == 0 {
		return
	}
	w.stray.say("other", gateStrayWarn, func() {
		w.logf("WARNING: the pack holds %s, which the gate has no use for: they take pack slots, so the gate takes %d essence a trade (they are left alone: worth over %d gp, or no config to price them)",
			strings.Join(names, ", "), runecraftrunner.TradeCapacity(w.d), gateStrayDropMax)
	})
}

// itemName is an object's name from the object config, else its id.
func (w *masterNatureWorld) itemName(id int) string {
	if w.ctx != nil && w.ctx.Config != nil {
		if o, ok := w.ctx.Config.Obj(id); ok && o.Name != "" {
			return o.Name
		}
	}
	return "item " + strconv.Itoa(id)
}
