package fleet

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/randomevents"
)

// RandomEventLoot records random-event rewards into the loot tally from one Guardian's own log
// lines, so internal/randomevents needs no hook and no handling behaviour changes (the same
// reason GuardianActivity and parseRandomEvent read those lines).
//
// Guardian.kick logs "random event: <name> — handling" while holding the account's action
// lock and immediately before the handler runs, and exactly one terminal line for that same
// attempt ("— cleared", "— still present after handling", "— handler error: ...",
// "— rejected for this account; ..."), still holding the lock. That pair is a window: the pack
// is read at "handling" and again after the terminal line, and loot.MeasureEventWindow keeps
// only what the event's own content can hand out (see internal/loot/randomevent.go).
//
// After the terminal line the pack is re-read every Poll for up to Settle, finishing as soon as
// a reward is visible: the server's inventory update can land a moment after the handler's
// last check. A new "handling" line finishes any open window at once, with the same reading
// the new window starts from, so nothing is counted twice. A rejected window (the event was
// another account's) records nothing.
//
// The Genie is the one event whose reward the pack cannot show at the end of its window: the lamp
// is handed over, rubbed for Experience and gone inside a single handling. Its two progress lines
// (randomevents.GenieTalkedLine, LampClaimedLine) are read as they go by, so a window can still
// count the hand-over (noteLamp). Until 5 Oct 2026 it counted nothing: with no allowed items the
// Genie could never complete, and 133 live "Genie — handling" lines produced no "[loot] ...
// completed" line (loot.MeasureEventWindowSeen has the history).
//
// Cheap at fleet scale: two pack reads per handling attempt plus at most Settle/Poll more,
// one short-lived goroutine per attempt, and nothing at all between random events.
type RandomEventLoot struct {
	// Settle and Poll bound the post-terminal re-reads (defaults 1.2s and 100ms). Settle is
	// kept under the time a resumed script needs to land its own next gain: the script only
	// resumes once the Guardian releases its lock, and a catch or pickpocket takes at least a
	// click and a game tick after that; the allow-list catches the rest.
	Settle, Poll time.Duration

	read   func() loot.Counts
	cfg    *cachecfg.Config
	record func(loot.Drop)
	logf   func(string)

	mu   sync.Mutex
	open *eventLootWindow
}

type eventLootWindow struct {
	reward loot.EventReward
	before loot.Counts
	// seen is the highest count of the event's own item any reading inside the window showed, for
	// an event that gives it (loot.EventReward.GivesOwn). Guarded by RandomEventLoot.mu.
	seen    loot.Counts
	cleared bool
	ended   bool
	done    bool
}

// playerPackComponent is the backpack's interface component (every runner's PlayerPack).
const playerPackComponent = 3214

// randomEventLoot is StartBot's RandomEventLoot for account a: its pack, its loot tally, and
// "[loot]" lines in its activity log. a.mu is held by the caller; nothing here takes it later.
func (m *Manager) randomEventLoot(a *account, username, prefix string) *RandomEventLoot {
	sess := a.session
	return NewRandomEventLoot(func() loot.Counts { return loot.PackCounts(sess.State().Inventory(playerPackComponent)) },
		m.cfg.Cache, m.lootRecorder(username), func(line string) { m.alogf(a, "%s[loot] %s", prefix, line) })
}

// NewRandomEventLoot measures with read (the account's pack), resolves item names through cfg
// (nil leaves them empty), reports each finished window's reward to record and logs a line per
// completed or contested event through logf (nil logs nothing).
func NewRandomEventLoot(read func() loot.Counts, cfg *cachecfg.Config, record func(loot.Drop), logf func(string)) *RandomEventLoot {
	return &RandomEventLoot{Settle: 1200 * time.Millisecond, Poll: 100 * time.Millisecond, read: read, cfg: cfg, record: record, logf: logf}
}

// splitEventLine splits a Guardian "random event: <name> — <tail>" line.
func splitEventLine(msg string) (name, tail string, ok bool) {
	if !strings.HasPrefix(msg, randomEventPrefix) {
		return "", "", false
	}
	rest := msg[len(randomEventPrefix):]
	const sep = " — "
	i := strings.Index(rest, sep)
	if i < 0 {
		return "", "", false
	}
	return rest[:i], rest[i+len(sep):], true
}

// Note reads one Guardian log line. Safe to call from the Guardian's goroutine only (it is
// called in order); the settle re-reads run on their own goroutine.
func (r *RandomEventLoot) Note(msg string) {
	if r == nil {
		return
	}
	name, tail, ok := splitEventLine(msg)
	if !ok {
		return
	}
	switch {
	case tail == "handling":
		pack := r.read()
		r.mu.Lock()
		prev := r.open
		r.open = nil
		r.mu.Unlock()
		if prev != nil {
			r.finish(prev, pack)
		}
		if reward, ok := loot.RandomEventReward(name); ok {
			w := &eventLootWindow{reward: reward, before: pack}
			r.mu.Lock()
			r.open = w
			r.mu.Unlock()
		}
	case tail == "cleared", tail == "still present after handling", strings.HasPrefix(tail, "handler error: "):
		r.mu.Lock()
		w := r.open
		if w == nil || w.ended || !sameEvent(w.reward, name) {
			r.mu.Unlock()
			return
		}
		w.ended, w.cleared = true, tail == "cleared"
		r.mu.Unlock()
		go r.settle(w)
	case strings.HasPrefix(tail, "rejected for this account"):
		r.mu.Lock()
		if w := r.open; w != nil && sameEvent(w.reward, name) {
			w.done = true
			r.open = nil
		}
		r.mu.Unlock()
	case tail == randomevents.GenieTalkedLine, tail == randomevents.LampClaimedLine:
		r.noteLamp(name, tail)
	}
}

// noteLamp reads one of the Genie's two hand-over lines into its open window (see
// loot.EventReward.GivesOwn for why the window's end readings cannot be trusted to see the lamp).
//
// GenieTalkedLine is logged when the Genie's reply was accepted, which is the first talk ("take
// this lamp") and also a repeat one ("happy with your wish": no lamp), so it only prompts a look at
// the pack: a lamp above what the window started with was handed over. LampClaimedLine is the
// Guardian saying a lamp it found is gone from the pack again, which is a hand-over too -- but only
// when the window started with none, because a lamp already in the pack at the start was handed over,
// and counted, by an earlier window (a Genie that is still there is handled again).
func (r *RandomEventLoot) noteLamp(name, tail string) {
	r.mu.Lock()
	w := r.open
	live := w != nil && !w.ended && !w.done && w.reward.GivesOwn && sameEvent(w.reward, name)
	r.mu.Unlock()
	if !live {
		return
	}
	own := w.reward.Own
	var at int64
	switch tail {
	case randomevents.GenieTalkedLine:
		at = r.read()[own]
	case randomevents.LampClaimedLine:
		if w.before[own] == 0 {
			at = 1
		}
	}
	r.mu.Lock()
	if w.seen == nil {
		w.seen = loot.Counts{}
	}
	if at > w.seen[own] {
		w.seen[own] = at
	}
	r.mu.Unlock()
}

// measure is loot.MeasureEventWindowSeen for w against the pack reading after, with a copy of what
// the window saw taken under the lock (noteLamp writes it from the Guardian's goroutine).
func (r *RandomEventLoot) measure(w *eventLootWindow, after loot.Counts, cleared bool) loot.EventWindow {
	r.mu.Lock()
	var seen loot.Counts
	if len(w.seen) > 0 {
		seen = make(loot.Counts, len(w.seen))
		for id, n := range w.seen {
			seen[id] = n
		}
	}
	r.mu.Unlock()
	return loot.MeasureEventWindowSeen(w.reward, w.before, seen, after, cleared)
}

// sameEvent reports whether the Guardian's event name is the one reward describes.
func sameEvent(reward loot.EventReward, name string) bool {
	other, ok := loot.RandomEventReward(name)
	return ok && other.Name == reward.Name
}

// settle re-reads the pack after w's terminal line until a reward shows or Settle passes.
func (r *RandomEventLoot) settle(w *eventLootWindow) {
	deadline := time.Now().Add(r.Settle)
	for {
		r.mu.Lock()
		done := w.done
		r.mu.Unlock()
		if done {
			return
		}
		after := r.read()
		m := r.measure(w, after, w.cleared)
		if len(m.Reward) > 0 || !time.Now().Before(deadline) {
			r.finish(w, after)
			return
		}
		time.Sleep(r.Poll)
	}
}

// finish records w against the pack reading after, once.
func (r *RandomEventLoot) finish(w *eventLootWindow, after loot.Counts) {
	r.mu.Lock()
	if w.done {
		r.mu.Unlock()
		return
	}
	w.done = true
	if r.open == w {
		r.open = nil
	}
	cleared := w.cleared && w.ended
	r.mu.Unlock()

	m := r.measure(w, after, cleared)
	events := 0
	if m.Completed {
		events = 1
	}
	if events > 0 || len(m.Reward) > 0 {
		r.record(loot.Drop{Source: w.reward.Source(), Events: events, Items: loot.Items(r.cfg, m.Reward)})
	}
	if r.logf == nil {
		return
	}
	switch {
	case events == 0 && len(m.Reward) == 0 && len(m.Contested) > 0 && len(m.Ignored) > 0:
		r.logf(fmt.Sprintf("random event %s: nothing recorded — the pack also lost %s while it was handled, so its gains (%s) may be the script's own", w.reward.Name, r.describe(m.Contested), r.describe(m.Ignored)))
	case events > 0 || len(m.Reward) > 0:
		line := fmt.Sprintf("random event %s: ", w.reward.Name)
		if events > 0 {
			line += "completed, "
		}
		if len(m.Reward) > 0 {
			line += "reward " + r.describe(m.Reward)
		} else {
			line += "no item reward in the pack"
		}
		if others := withoutEventItems(m.Ignored); len(others) > 0 {
			line += " (not the event's: " + r.describe(others) + ")"
		}
		r.logf(line)
	}
}

// withoutEventItems drops the Strange box and Lamp from a log list: their changes are the
// events themselves, not anything worth pointing out.
func withoutEventItems(c loot.Counts) loot.Counts {
	out := loot.Counts{}
	for id, n := range c {
		if id != 3062 && id != 2528 {
			out[id] = n
		}
	}
	return out
}

func (r *RandomEventLoot) describe(c loot.Counts) string {
	items := loot.Items(r.cfg, c)
	parts := make([]string, 0, len(items))
	for _, it := range items {
		name := it.Name
		if name == "" {
			name = fmt.Sprintf("obj %d", it.ID)
		}
		parts = append(parts, fmt.Sprintf("%d x %s", it.Count, name))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
