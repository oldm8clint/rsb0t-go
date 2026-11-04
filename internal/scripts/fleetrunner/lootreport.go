package fleetrunner

import (
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/randomevents"
)

// kills is this account's kill memory: the bot's own (Context.KillLog) when it supplied one, or a
// log private to this Deps when it did not, so a harness still attributes a step's bones to that
// step's kill.
func (d *Deps) kills() *loot.KillLog {
	if d.Kills == nil {
		d.Kills = &loot.KillLog{}
	}
	return d.Kills
}

// LootBefore is the pack reading a take is measured against, with the account's random-event
// activity at the same moment; nil when loot is not recorded, so a harness does not even read the
// pack. The activity is read FIRST (an event that began between the two readings would otherwise go
// unseen, and its reward lands seconds later, inside the take).
func LootBefore(d *Deps) *loot.Reading {
	if d == nil || d.Loot == nil || d.Session == nil {
		return nil
	}
	events := randomevents.ActivityOf(d.Session)
	return &loot.Reading{
		Counts:    loot.PackCounts(d.Session.State().Inventory(PlayerPack)),
		Disturbed: events.Disturbed,
	}
}

// RecordTake measures what arrived in the pack since before, waiting up to wait for it, and
// reports it under src with events events. Nothing is reported when nothing arrived, nor when a
// random event ran during the take (loot.TakeGained says why: the lifetime Cow card held an event's
// Feather x5,014 and Casket x70 from takes it paid out inside), and an event's own item is never
// part of one.
func RecordTake(d *Deps, src loot.Source, before *loot.Reading, events int, wait time.Duration) {
	if d == nil || d.Loot == nil || before == nil {
		return
	}
	read := func() *gamestate.Inventory { return d.Session.State().Inventory(PlayerPack) }
	gained, discarded := loot.TakeGained(read, before, d.Delay, wait, nil)
	if discarded {
		d.Logf("loot: not recording a take for %s: a random event ran while it was being measured", src.Name)
		return
	}
	if len(gained) == 0 {
		return
	}
	d.Loot(loot.Drop{Source: src, Events: events, Items: loot.Items(d.Config, gained)})
}
