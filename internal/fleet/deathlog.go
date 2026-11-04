package fleet

// The one death line every account gets, whatever script it runs (deaths-1008, 8 Oct 2026). The scripts' own death
// lines each say a little (a tile that is the last loop pass's, a counter) and most scripts say nothing; this one is
// written where the death line arrives (gamestate.EventDeath), while the client still holds the pack and the tile of
// the death, and names the killer, the hitpoints of the fight, the food left and the alchemy stock the account was
// carrying (internal/deathreport words it).

import (
	"fmt"
	"strings"
	"sync"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/deathreport"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/fleetplan"
)

var (
	alchSetOnce sync.Once
	alchSet     map[string]bool
)

// isAlchStock is whether name is on the owner's alchemy list (scarabplan.DefaultAlchItems), any case.
func isAlchStock(name string) bool {
	alchSetOnce.Do(func() {
		alchSet = make(map[string]bool, len(fleetplan.DefaultAlchItems))
		for _, n := range fleetplan.DefaultAlchItems {
			alchSet[strings.ToLower(strings.TrimSpace(n))] = true
		}
	})
	return alchSet[strings.ToLower(strings.TrimSpace(name))]
}

// deathInput turns the client's reading of a death into the report's input, naming ids through cfg (nil: "obj 858").
func deathInput(dc gamestate.DeathContext, cfg *cachecfg.Config) deathreport.Input {
	npcName := func(id int) string {
		if cfg != nil {
			if n, ok := cfg.Npc(id); ok && n.Name != "" {
				return n.Name
			}
		}
		return fmt.Sprintf("npc %d", id)
	}
	in := deathreport.Input{
		HaveTile: dc.HaveTile, X: dc.X, Z: dc.Z, Level: dc.Level,
		HPPeak: dc.HPPeak, HPLast: dc.HPLast, MaxHP: dc.MaxHP,
		Players: dc.Players, Poisoned: dc.Poisoned,
		IsFood: func(name string) bool { _, ok := fighterfood.BaseFood(name); return ok },
		IsAlch: isAlchStock,
	}
	for _, a := range dc.Attackers {
		in.Attackers = append(in.Attackers, npcName(a.Type))
	}
	for _, a := range dc.Nearby {
		in.Nearby = append(in.Nearby, npcName(a.Type))
	}
	costs := map[string]int{}
	for _, it := range dc.Pack {
		name, noted := fmt.Sprintf("obj %d", it.ID), false
		if cfg != nil {
			if o, ok := cfg.Obj(int(it.ID)); ok {
				name, noted = o.Name, o.Noted()
				if _, seen := costs[o.Name]; !seen {
					costs[o.Name] = fleetplan.HighAlchValue(int(o.Cost))
				}
			}
		}
		in.Pack = append(in.Pack, deathreport.PackItem{Name: name, Count: int(it.Count), Noted: noted})
	}
	in.AlchValue = func(name string) int {
		if v := fleetplan.KnownAlchValue(name); v > 0 {
			return v
		}
		return costs[name]
	}
	return in
}

// logDeath writes a's death line. It runs on the read loop's goroutine, so it must not wait for a.mu (a writer holding it
// while it waits for this loop would deadlock): the script's name is read only if the lock is free.
func (m *Manager) logDeath(a *account, st *gamestate.State) {
	dc, ok := st.LastDeath()
	if !ok {
		return
	}
	script := "?"
	if a.mu.TryRLock() {
		script = a.script
		a.mu.RUnlock()
	}
	in := deathInput(dc, m.cfg.Cache)
	m.alogf(a, "[%s/%s] %s", a.username, script, deathreport.Line(in))
	m.noteDeathLoot(a, script, dc, in)
}
