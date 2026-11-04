// Package minemanipplan is the pure half of essence-mining tick manipulation: what content 289 says about the timing of an
// essence roll, which pickaxes gain from a knife-on-log batch and by how much, and the controller settings that drive the
// same knife-on-log controller woodcutting uses (internal/scripts/chopmanipplan). No client, actions, fleet or bot imports,
// so every rule below is unit-tested against a tick-accurate model of the content's mining script (engine.go).
//
// # The essence roll, line by line (content 289, scripts/skill_mining/scripts/mining.rs2)
//
//	10-11  [oploc1,blankrunestone] @mining_firstswing(get_ore_essence);  [oploc3,blankrunestone] @mining_continue(get_ore_essence)
//	41-45  firstswing: no free pack slot -> a message box ("too full") and the interaction ends
//	65-69  firstswing: if %action_delay < map_clock { %action_delay = map_clock + mining_rate; p_oploc(1); return }
//	70-75  firstswing: swing animation; if %action_delay = map_clock { jump get_ore } else p_oploc(3)
//	107-113 continue (every later tick): if %action_delay < map_clock { %action_delay = map_clock + mining_rate }
//	        else if %action_delay = map_clock { swing animation; jump get_ore }; p_oploc(3)
//	197-207 get_ore_essence: inv_add(blankrune, 1); stat_advance(mining, 50); p_oploc(3)   <- NO success roll, NO depletion
//
// So an essence rock is a pure timer on the per-player varp %action_delay. The first click arms it (+rate), the roll comes
// when it equals map_clock, and a roll leaves the varp AT map_clock, so the next tick is STALE: the loop re-arms it
// (+rate) and the next roll is rate+1 ticks after the last. pickaxes.obj gives mining_rate 7 (bronze), 6, 5, 4 (mithril),
// 3, 2 (rune): one essence every 8, 7, 6, 5, 4, 3 ticks. Every roll is an essence. The doc's 620, 690, 780, 900, 1,060 and
// 1,290 an hour (docs/runecraft_p2_2026-10-13.md) are exactly this period with a 27-essence load and 45 ticks of teleport,
// portal, walk and deposit around it (essenceplan.EssencePerHour).
//
// # Why a knife on a log makes it faster, and how much
//
// skill_fletching/scripts/cut_logs.rs2:83-84 (process_fletch_logs) sets %action_delay = map_clock + 2 when it is below
// map_clock and queues the fletch on the WEAK queue; a click read in the same tick (or a CLOSE_MODAL) clears that queue, so
// the log is kept and only the varp moved. The woodcutting loop (woodcut.rs2:55-59, 118-120) is this same loop with a
// rate of 3, and the fleet has run [knife on log, Make 1, CLOSE_MODAL, tree click] in the stale tick since 6 October
// (docs/tick_manipulation_lab_2026-10-06.md: x1.36 logs). In mining the knife re-arms the stale varp to +2 instead of
// +rate, so the roll follows 3 ticks after the last whatever the pickaxe:
//
//	bronze 8 -> 3 ticks (x2.67)   iron 7 -> 3 (x2.33)   steel 6 -> 3 (x2.0)   mithril 5 -> 3 (x1.67)
//	adamant 4 -> 3 (x1.33)        rune 3 -> 3 (nothing: its own period is already the floor)
//
// 3 is the floor. Every writer of %action_delay that an item use can reach adds at least 2 (cut_logs 84, glass.rs2:88,
// leather.rs2:140, pottery.rs2:84, all "map_clock + 2" when stale); the +1 writers (pottery.rs2:152, smelting.rs2:205,226)
// sit inside weak-queue callbacks the click cancels; bullroarer.rs2:8 sets it to map_clock but refuses unless the varp is
// 8 or more ticks stale; eating ADDS 3 to the old value (consume.rs2:130) and uses the food up.
//
// # Costs and what stays the same
//
//   - The kit (a knife and one log) takes 2 pack slots: a load is 25 essence with the pickaxe in the pack (27 without a kit).
//   - A rune pickaxe gains nothing and pays the 2 slots, so it never runs the batch (Worth).
//   - firstswing is where the random-event flag is read (mining.rs2:53, World.processClientsIn sets it with probability 4%,
//     8% in a zone held 1000 ticks, once per 500 ticks and consumes it at the next firstswing): a batch every 3 ticks reads it
//     within 3 ticks of its being set, plain mining reads it at the next load. Events per HOUR are the same; per essence they
//     are fewer.
//   - A level-up chat opens in the tick AFTER the roll that earned it (Player.addXp queues it on the engine queue, which runs
//     before processInteraction) and blocks the interaction (canAccess is false with a chat open) until it is closed. The batch
//     read in that tick has already moved the varp, so a Continue read the next tick loses nothing.
//   - Never more than four user events in a tick and one batch in flight (the engine reads five).
package minemanipplan

import (
	"strings"

	"rsb0t-go/internal/scripts/chopmanipplan"
)

// Content numbers.
const (
	// ManipPeriod is the ticks between essence with a batch read in every stale tick: the varp is re-armed to +2 from the stale
	// tick, which is the tick after the roll.
	ManipPeriod = chopmanipplan.Period
	// FirstRollManip is the ticks from the opening batch to the first roll (the varp is stale then, so it is re-armed to +2).
	FirstRollManip = 2
	// PackSlots is the backpack.
	PackSlots = 28
	// KitSlots is what the carrier takes: the knife and one log.
	KitSlots = 2
	// BatchEvents is the user events one batch carries: knife on log, Make 1, CLOSE_MODAL, the rock click.
	BatchEvents = 4
	// EngineEventsPerTick is how many user events the engine reads from a player in one tick (NetworkPlayer.decodeIn).
	EngineEventsPerTick = 5
	// EssenceXP is stat_advance's argument for one essence (mine.dbrow rock_exp 50: tenths of an experience point).
	EssenceXP = 50
	// OverheadTicks is the teleport in, the portal, the walk to the bank and back and the deposit around a load
	// (essenceplan.LoadOverheadTicks, the design's number).
	OverheadTicks = 45
)

// Pickaxe is one pickaxe row of content/scripts/skill_mining/configs/pickaxes.obj.
type Pickaxe struct {
	Item string
	// Level is param levelrequire; Rate is param mining_rate; Anim is the mining_animation seq id (content/pack/seq.pack).
	Level, Rate, Anim int
}

// Pickaxes are the six pickaxes, best first (the order pickaxe_checker.rs2 tries them).
var Pickaxes = []Pickaxe{
	{Item: "Rune pickaxe", Level: 41, Rate: 2, Anim: 624},
	{Item: "Adamant pickaxe", Level: 31, Rate: 3, Anim: 628},
	{Item: "Mithril pickaxe", Level: 21, Rate: 4, Anim: 629},
	{Item: "Steel pickaxe", Level: 6, Rate: 5, Anim: 627},
	{Item: "Iron pickaxe", Level: 1, Rate: 6, Anim: 626},
	{Item: "Bronze pickaxe", Level: 1, Rate: 7, Anim: 625},
}

// Period is the ticks between two essence without a batch: the rate plus the stale tick.
func (p Pickaxe) Period() int { return p.Rate + 1 }

// FirstRoll is the ticks from the first click to the first essence without a batch.
func (p Pickaxe) FirstRoll() int { return p.Rate }

// Worth reports whether a batch beats the pickaxe's own period.
func (p Pickaxe) Worth() bool { return p.Period() > ManipPeriod }

// Gain is the essence rate with batches over the rate without, in steady state.
func (p Pickaxe) Gain() float64 { return float64(p.Period()) / float64(ManipPeriod) }

// ByItem finds a pickaxe by its item name, case-insensitively.
func ByItem(name string) (Pickaxe, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, p := range Pickaxes {
		if strings.ToLower(p.Item) == n {
			return p, true
		}
	}
	return Pickaxe{}, false
}

// Best is the pickaxe pickaxe_checker.rs2 would pick for a Mining level among the item names held (pack or worn): the best
// one the level allows.
func Best(level int, held []string) (Pickaxe, bool) {
	for _, p := range Pickaxes {
		if level < p.Level {
			continue
		}
		for _, h := range held {
			if strings.EqualFold(strings.TrimSpace(h), p.Item) {
				return p, true
			}
		}
	}
	return Pickaxe{}, false
}

// IsPickAnim reports whether seq is a pickaxe's mining animation. It is played by every roll AND by the first swing of an unprimed
// click (mining.rs2:70), so on its own it does not say a roll happened.
func IsPickAnim(seq int) bool {
	for _, p := range Pickaxes {
		if p.Anim == seq {
			return true
		}
	}
	return false
}

// ControllerSettings are chopmanipplan's settings for mining with the pickaxe. The period without a batch is the pickaxe's
// own, and no animation counts as a roll: the controller is fed chopmanipplan.Obs.Roll from the Mining experience, because
// every essence roll earns exactly one XP update and the pickaxe animation lies (the unprimed click's first swing plays it
// with no roll, and a batch read in the roll tick hides it under the fletching animation; see Obs.Roll).
func ControllerSettings(p Pickaxe) chopmanipplan.Settings {
	s := chopmanipplan.DefaultSettings()
	s.Swing = func(int) bool { return false }
	s.Plain = p.Period()
	return s
}

// LoadSize is the essence one full pack holds: the pack less the pickaxe's slot when it is carried, less the kit.
func LoadSize(pickInPack, kit bool) int {
	n := PackSlots
	if pickInPack {
		n--
	}
	if kit {
		n -= KitSlots
	}
	return n
}

// MineTicks is the ticks to mine n essence from the first click: without a batch the first roll is rate ticks on and each
// later one rate+1; with batches (every stale tick) the first is 2 on and each later one 3.
func MineTicks(p Pickaxe, n int, manip bool) int {
	if n <= 0 {
		return 0
	}
	if manip && p.Worth() {
		return FirstRollManip + ManipPeriod*(n-1)
	}
	return p.FirstRoll() + p.Period()*(n-1)
}

// LoadTicks is a whole load: mining it and the overhead around it.
func LoadTicks(p Pickaxe, pickInPack, kit bool) int {
	n := LoadSize(pickInPack, kit)
	return MineTicks(p, n, kit) + OverheadTicks
}

// EssencePerHour is the essence banked an hour (6,000 ticks) with the pickaxe, a kit or not, the pickaxe in the pack.
func EssencePerHour(p Pickaxe, kit bool) float64 {
	return float64(LoadSize(true, kit)) / float64(LoadTicks(p, true, kit)) * 6000
}
