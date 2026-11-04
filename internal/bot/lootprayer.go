package bot

import (
	"sync"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
)

// THE PRAYER CARD: every bone buried by any script, recorded without touching any script.
//
// The owner, 2026-09-23: "not sure if giants and the prayer section is hooked either". It was not.
// Four scripts bury bones — ScarabSwarm after each training kill (scarabrunner.LootAndBuryBones,
// through boneburierrunner.BuryOne), CombatTrainer on its giant rungs (combatrunner.BuryOneBone),
// AdvancedFighter with buryBones on (fighterrunner.BuryOne) and the BoneBurier script itself —
// through three different bury primitives, and not one of them reported anything. Wiring each of
// them would have meant edits to AJ's combat packages and to ScarabSwarm's claimed runner, and a
// fifth script that buries would have been silent again.
//
// So the Runner watches for burying itself. Content 289 buries in one script step
// (content/scripts/skill_prayer/scripts/bury_bone.rs2):
//
//	inv_delslot(inv, $slot);
//	stat_advance(prayer, oc_param($last_item, bone_exp));
//
// so a burial is a BONE LEAVING THE PACK in the same server tick as PRAYER XP RISING, and nothing
// else in content produces that pair: the only other routine Prayer experience is three quests, the
// Mort'ton pyres and one Ghast (scarabplan/prayerroutes.go enumerates them), none of which takes a
// bone out of the pack, while banking or dropping a bone takes it out with no experience at all.
// Either half alone is ignored.
//
// The XP recorded is the skill's own reading rising (gamestate.Event.XPDelta). The wire carries
// whole experience, so a 4.5-xp Bones burial reads 4 and 5 alternately; the total over many is
// exact to within one point. On the 10x-XP lab engine a burial reads 45.

// buryPairTicks is how many server ticks apart the two halves of a burial may be observed and
// still pair. Both packets are written in the same script step, so 0 is the expectation; 1 allows
// for the tick counter advancing between them.
const buryPairTicks = 1

// buryPairWindow bounds a half's life in wall-clock time as well, for a session whose tick events
// stop (a disconnect) with a half still pending.
const buryPairWindow = 3 * time.Second

type buryHalf struct {
	id, xp int
	tick   int
	at     time.Time
}

// buryWatch pairs the two halves of each burial. Pure apart from its record callback, so it is
// tested by feeding it events.
type buryWatch struct {
	mu     sync.Mutex
	tick   int
	bones  []buryHalf // bones that left the pack, not yet paired
	gains  []buryHalf // Prayer XP rises, not yet paired
	now    func() time.Time
	record func(boneID, xp int)
}

func newBuryWatch(record func(boneID, xp int)) *buryWatch {
	return &buryWatch{now: time.Now, record: record}
}

// onEvent is a gamestate.Subscriber.
func (w *buryWatch) onEvent(ev gamestate.Event) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	switch ev.Kind {
	case gamestate.EventTick:
		w.tick = ev.Tick
	case gamestate.EventInventoryChanged:
		if ev.Comp != lootPackComponent {
			return
		}
		prev := int(ev.PreviousID)
		if !loot.IsBuryable(prev) {
			return
		}
		left := int(ev.PreviousCount)
		if ev.ItemID == ev.PreviousID {
			left = int(ev.PreviousCount - ev.Count)
		}
		for i := 0; i < left; i++ {
			w.bones = append(w.bones, buryHalf{id: prev, tick: w.tick, at: now})
		}
	case gamestate.EventSkillXP:
		if ev.Skill != loot.PrayerStat || ev.XPDelta <= 0 {
			return
		}
		w.gains = append(w.gains, buryHalf{xp: ev.XPDelta, tick: w.tick, at: now})
	default:
		return
	}
	w.expireLocked(now)
	w.pairLocked()
}

func (w *buryWatch) fresh(h buryHalf, now time.Time) bool {
	return w.tick-h.tick <= buryPairTicks && now.Sub(h.at) <= buryPairWindow
}

func (w *buryWatch) expireLocked(now time.Time) {
	keep := func(in []buryHalf) []buryHalf {
		out := in[:0]
		for _, h := range in {
			if w.fresh(h, now) {
				out = append(out, h)
			}
		}
		return out
	}
	w.bones, w.gains = keep(w.bones), keep(w.gains)
}

// pairLocked matches the oldest pending bone with the oldest pending gain, while both exist and
// were seen within buryPairTicks of each other.
func (w *buryWatch) pairLocked() {
	for len(w.bones) > 0 && len(w.gains) > 0 {
		b, g := w.bones[0], w.gains[0]
		d := b.tick - g.tick
		if d < 0 {
			d = -d
		}
		if d > buryPairTicks {
			// The older of the two can never pair now: drop it and try again.
			if b.tick < g.tick {
				w.bones = w.bones[1:]
			} else {
				w.gains = w.gains[1:]
			}
			continue
		}
		w.bones, w.gains = w.bones[1:], w.gains[1:]
		if w.record != nil {
			w.record(b.id, g.xp)
		}
	}
}

// watchBurials starts the bury watch on this Context's session, for as long as the Runner runs.
// A no-op with no loot sink or no session. The subscription is made through On, so the Runner's
// disposeSubscriptions cancels it with the others.
func (c *Context) watchBurials() {
	if c == nil || c.Loot == nil || c.gameState() == nil {
		return
	}
	w := newBuryWatch(func(boneID, xp int) {
		c.Loot(loot.Drop{Source: loot.Bury(c.Config, boneID, ""), Events: 1, XP: xp})
	})
	c.On(w.onEvent, gamestate.EventTick, gamestate.EventInventoryChanged, gamestate.EventSkillXP)
}
