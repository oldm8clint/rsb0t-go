package fleet

// The fleet's half of the Deaths view of the Loot panel (internal/deathloot says what it records and why): every death's
// items are put in a ledger when the death line arrives, and every item any account's backpack gains is offered to the ledger,
// which attributes it to a death pile when it came from one. Script-independent on purpose: the scripts that recover their
// own death loot each report differently (or not at all), and a pickup by ANOTHER account is by definition not reported by the
// script of the one that died.

import (
	"fmt"
	"sync"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/deathloot"
	"rsb0t-go/internal/deathreport"
	"rsb0t-go/internal/gamestate"
)

// packComponent is the backpack's inventory component (actions.PlayerPack; fleet reads it the same way gamestate does).
const packComponent = 3214

// deathLootBook is the Manager's death ledger; its zero value is ready to use.
type deathLootBook struct {
	once   sync.Once
	ledger *deathloot.Ledger
}

func (b *deathLootBook) get() *deathloot.Ledger {
	b.once.Do(func() { b.ledger = deathloot.NewLedger(0) })
	return b.ledger
}

// DeathEvents is every death this process has seen, with the recoveries matched to it so far, oldest first. The control panel polls
// it and keeps its own persisted copy (internal/controlpanel/lootdeaths.go).
func (m *Manager) DeathEvents() []deathloot.Death { return m.deathLoot.get().Deaths() }

// heldOf names a death's slots through cfg (nil: "obj 858") with each item's cache cost.
func heldOf(cfg *cachecfg.Config, slots []gamestate.SlotItem, worn bool) []deathloot.Held {
	out := make([]deathloot.Held, 0, len(slots))
	for _, s := range slots {
		h := deathloot.Held{ID: int(s.ID), Name: fmt.Sprintf("obj %d", s.ID), Count: int64(s.Count), Worn: worn}
		if cfg != nil {
			if o, ok := cfg.Obj(int(s.ID)); ok {
				if o.Name != "" {
					h.Name = o.Name
				}
				h.Cost = int64(o.Cost)
			}
		}
		out = append(out, h)
	}
	return out
}

// deathOf builds the ledger's death from the client's reading of one.
func deathOf(username, script string, now time.Time, dc gamestate.DeathContext, in deathreport.Input, cfg *cachecfg.Config) deathloot.Death {
	d := deathloot.Death{
		ID: fmt.Sprintf("%s@%d", username, now.UnixMilli()), Account: username, Script: script, At: now.UnixMilli(),
		HaveTile: dc.HaveTile, X: dc.X, Z: dc.Z, Level: dc.Level, Cause: deathreport.CauseKey(in),
	}
	if dc.HaveTile {
		d.Place = deathreport.Area(dc.X, dc.Z, dc.Level)
	}
	held := append(heldOf(cfg, dc.Pack, false), heldOf(cfg, dc.Worn, true)...)
	// The server keeps the three priciest items of an unskulled player; the skull and Protect Item are not read here.
	d.Items, d.Kept = deathloot.Lost(held, false, false)
	return d
}

// noteDeathLoot records a's death in the ledger. It runs on the read loop's goroutine (logDeath).
func (m *Manager) noteDeathLoot(a *account, script string, dc gamestate.DeathContext, in deathreport.Input) {
	m.deathLoot.get().NoteDeath(deathOf(a.username, script, time.Now(), dc, in, m.cfg.Cache))
}

// noteGroundGain offers a backpack slot change to the ledger: if the account gained an item within reach of a recent death pile, it is
// a pickup of that pile. The first test is one atomic read, so the hook costs nothing while no death is recent.
func (m *Manager) noteGroundGain(a *account, st *gamestate.State, ev gamestate.Event) {
	now := time.Now().UnixMilli()
	ledger := m.deathLoot.get()
	if !ledger.Active(now) {
		return
	}
	n := deathloot.GainOf(ev.PreviousID, ev.PreviousCount, ev.ItemID, ev.Count)
	if n <= 0 {
		return
	}
	x, z, level, ok := st.Tile()
	if !ok {
		return
	}
	ledger.NoteGain(deathloot.Gain{Account: a.username, At: now, X: x, Z: z, Level: level, ID: int(ev.ItemID), Count: n})
}
