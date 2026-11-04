package bot

import (
	"sync"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
)

// EXPERIENCE ON THE LOOT TAB (a user request: "XP per hour on Woodcutting, Fishing, Fletching,
// Thieving, Crafting, Magic and Prayer panels; NPC panels show XP/h per NPC's kills"). internal/loot/
// xp.go says what lands where; this is the piece that measures it, for every running account and
// without touching any script.
//
// Every rise of a skill's experience (gamestate.EventSkillXP) is held as PENDING for that skill. The
// next Drop a script reports that EARNED it takes it: a skill card's drop (a window flush of logs, a
// fish catch, a theft, a batch of arrow shafts) takes its own skill's pending experience as Drop.XP,
// and a kill takes the pending combat experience as Drop.Skills. The Context's Loot sink is wrapped
// once, before any script reads it, so every script's Deps capture the wrapper.
//
// What this refuses to attribute, because a wrong figure is worse than a missing one:
//   - a single rise above xpEventCap: a quest reward or a lamp, never a swing or a log;
//   - experience older than xpPendingTTL: the script that earned it is not the one reporting now;
//   - a teleport's Magic experience, which the cast watch (lootcast.go) takes back out.

// xpEventCap is the largest single experience rise that is attributed to an activity. A hit pays at most
// a few hundred points and a log or a fish well under that; a quest reward is thousands.
const xpEventCap = 1000

// xpPendingTTL is how long experience waits for a Drop to claim it.
const xpPendingTTL = 10 * time.Minute

type xpPending struct {
	xp int64
	at time.Time
}

// xpWatch holds each skill's pending experience. Safe for concurrent use: the subscriber and the
// script goroutine both touch it.
type xpWatch struct {
	mu      sync.Mutex
	pending [gamestate.SkillCount]xpPending
	now     func() time.Time
}

func newXPWatch() *xpWatch { return &xpWatch{now: time.Now} }

// onEvent is a gamestate.Subscriber for EventSkillXP.
func (w *xpWatch) onEvent(ev gamestate.Event) {
	if ev.Kind != gamestate.EventSkillXP || ev.XPDelta <= 0 || ev.XPDelta > xpEventCap || ev.Skill < 0 || ev.Skill >= gamestate.SkillCount {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	p := &w.pending[ev.Skill]
	if p.xp == 0 || w.now().Sub(p.at) > xpPendingTTL {
		p.xp, p.at = 0, w.now()
	}
	p.xp += int64(ev.XPDelta)
}

// discard takes xp back out of a skill's pending experience: a teleport's, which is the Teleports
// card's own business (it is not earned by whatever Magic card reports next).
func (w *xpWatch) discard(stat int, xp int) {
	if w == nil || stat < 0 || stat >= gamestate.SkillCount || xp <= 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	p := &w.pending[stat]
	p.xp -= int64(xp)
	if p.xp < 0 {
		p.xp = 0
	}
}

// takeLocked empties one skill's pending experience, or nothing when it is stale.
func (w *xpWatch) takeLocked(stat int) int64 {
	p := &w.pending[stat]
	xp := p.xp
	stale := w.now().Sub(p.at) > xpPendingTTL
	p.xp = 0
	if stale {
		return 0
	}
	return xp
}

// take empties one skill's pending experience and returns it.
func (w *xpWatch) take(stat int) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.takeLocked(stat)
}

// takeCombat empties every combat skill's pending experience and returns it as Drop.Skills items.
func (w *xpWatch) takeCombat() []loot.Item {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []loot.Item
	for _, stat := range loot.CombatStats {
		if xp := w.takeLocked(stat); xp > 0 {
			out = append(out, loot.SkillXP(stat, xp))
		}
	}
	return out
}

// attach gives d the experience it earned: a kill the combat skills', a skill card's drop its skill's.
// A drop that already carries experience (a burial) keeps its own.
func (w *xpWatch) attach(d *loot.Drop) {
	if w == nil {
		return
	}
	switch {
	case loot.PaysCombatExperience(*d) && len(d.Skills) == 0:
		d.Skills = w.takeCombat()
	case d.XP == 0 && (d.Events > 0 || len(d.Items) > 0):
		if stat, ok := loot.ClaimsExperience(d.Source); ok {
			d.XP = int(w.take(stat))
		}
	}
}

// TrackExperience wraps this Context's Loot sink so every Drop takes the experience it earned. The
// fleet calls it once, right after it sets Loot and before any script can read it, so every script's
// Deps capture the wrapper; a second call, or a Context with no sink, does nothing. Without it nothing
// is attributed and every Drop is recorded as it was reported.
func (c *Context) TrackExperience() {
	if c == nil || c.Loot == nil || c.xp != nil {
		return
	}
	w := newXPWatch()
	c.xp = w
	inner := c.Loot
	c.Loot = func(d loot.Drop) {
		w.attach(&d)
		inner(d)
	}
}

// watchExperience subscribes the watch that collects the experience TrackExperience hands out, for as
// long as the Runner runs. A no-op when nothing is tracked or there is no session.
func (c *Context) watchExperience() {
	if c == nil || c.xp == nil || c.gameState() == nil {
		return
	}
	c.On(c.xp.onEvent, gamestate.EventSkillXP)
}
