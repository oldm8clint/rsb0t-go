// Package retaliateplan is a small pure model of the one way Engine-TS 289 + Content 289 can make a tree roll every
// 2 ticks instead of 4: the auto-retaliate "flinch" of a monster that hits a player who has no interaction.
// It has no client, actions, fleet or bot imports; it exists so the arithmetic in
// docs/two_tick_retaliate_research_2026-11-04.md is concrete and unit-tested, and so a future script has a reference
// for the tick order it must write to. Nothing here runs against a server.
//
// # The rules it models (each one cited in the doc with file and line)
//
//   - Tick order: client packets, then NPCs, then the player's queues, then the player's interaction
//     (engine src/engine/World.ts cycle 340-411, processPlayers 687-731).
//   - A monster's hit queues playerhit_n_retaliate with delay 0, so it runs in the same tick (npc_combat_melee.rs2:47-48).
//     That script does something only when auto-retaliate is on AND busy2 is false, i.e. the player has no interaction and
//     no waypoints (auto_retaliate.rs2:7, PlayerOps.ts BUSY2). If %action_delay < clock it sets
//     %action_delay = clock + attackrate/2 (minus 1 with the ranged rapid style) and then p_opnpc(2), which replaces
//     the player's interaction with an attack on the monster (auto_retaliate.rs2:9-25).
//   - A woodcutting interaction rolls when %action_delay == clock, re-arms it to clock+3 when it is below the clock, and
//     does nothing when it is above (woodcut.rs2:55-62, 84-86, 118-125). Both the first-click label and the repeat label
//     roll on the tick that equals the clock.
//   - An attack with a bow sets %action_delay = clock + attackrate (-1 rapid) BEFORE the ammo check and, with no ammo,
//     refuses and stops the interaction (player_ranged.rs2:6-31).
//   - A knife on a log sets %action_delay = clock+2 when it is below the clock (cut_logs.rs2:83-86); the same batch's tree
//     click cancels the queued fletch (the existing chop manipulation, docs/tick_manipulation_lab_2026-10-06.md).
//   - Any inventory item operation or item-on-item use calls clearPendingAction, which drops the player's interaction
//     (OpHeldHandler.ts:55, OpHeldUHandler.ts:86).
package retaliateplan

// Op is one client action the engine reads at the start of a tick.
type Op int

const (
	// OpClear is the cheapest interaction clear: an item used on an item that has no script ("Nothing interesting happens").
	OpClear Op = iota
	// OpDrop is an inventory Drop (opheld5): it also clears the interaction, and removes a log from the pack.
	OpDrop
	// OpClickTree is OPLOC1 on the tree.
	OpClickTree
	// OpKnifeBatch is the existing manipulation batch: knife on a log, Make 1, tree click.
	OpKnifeBatch
)

type target int

const (
	tNone target = iota
	tTreeFirst
	tTreeCont
	tNPC
)

// Config is the player's side of the model.
type Config struct {
	// AutoRetaliate is the controls tab setting (on by default: %option_nodef = 0).
	AutoRetaliate bool
	// AttackRate is the worn weapon's attackrate param (4 for every shortbow, 6 longbows, 5 axes, 4 unarmed).
	AttackRate int
	// Rapid is the ranged rapid attack style (%damagestyle = style_ranged_rapid), available with a bow.
	Rapid bool
	// Ammo says the quiver holds arrows. False is the owner's "no arrows": an attack that does start is refused.
	Ammo bool
}

// Sim is one player, one tree and a stream of monster hits.
type Sim struct {
	Cfg   Config
	Clock int
	// AD is %action_delay.
	AD int

	tgt target

	// Rolls are the ticks a woodcutting roll happened on.
	Rolls []int
	// Flinches are the ticks the retaliate script wrote %action_delay.
	Flinches []int
	// Hijacks are the ticks the retaliate script replaced a (cleared) interaction without moving %action_delay.
	Hijacks []int
	// Refused are the ticks a no-ammo attack was refused and the interaction stopped.
	Refused []int
	// Shots are the ticks an attack with ammo was fired.
	Shots []int
	// Batches counts knife batches read.
	Batches int
}

// New returns a player at a tree with nothing running. The clock starts at 100 and %action_delay at 0, like a fresh varp.
func New(cfg Config) *Sim {
	return &Sim{Cfg: cfg, Clock: 100, AD: 0}
}

// Busy2 mirrors the content's busy2: the player has an interaction (or waypoints, which the model never has).
func (s *Sim) Busy2() bool { return s.tgt != tNone }

// LastRoll is the tick of the newest roll, or -1.
func (s *Sim) LastRoll() int {
	if len(s.Rolls) == 0 {
		return -1
	}
	return s.Rolls[len(s.Rolls)-1]
}

// Tick runs one game tick: the client's ops are read first, then a monster hit if hit is true, then the player's queue,
// then the player's interaction.
func (s *Sim) Tick(ops []Op, hit bool) {
	for _, op := range ops {
		switch op {
		case OpClear, OpDrop:
			s.tgt = tNone
		case OpClickTree:
			s.tgt = tTreeFirst
		case OpKnifeBatch:
			s.Batches++
			if s.AD < s.Clock {
				s.AD = s.Clock + 2
			}
			s.tgt = tTreeFirst
		}
	}
	if hit {
		s.retaliate()
	}
	s.interact()
	s.Clock++
}

// retaliate is [queue,playerhit_n_retaliate] (auto_retaliate.rs2:1-26).
func (s *Sim) retaliate() {
	if !s.Cfg.AutoRetaliate || s.Busy2() {
		return
	}
	if s.AD < s.Clock {
		s.AD = s.Clock + Flinch(s.Cfg.AttackRate, s.Cfg.Rapid)
		s.Flinches = append(s.Flinches, s.Clock)
	} else {
		s.Hijacks = append(s.Hijacks, s.Clock)
	}
	s.tgt = tNPC
}

// Flinch is the ticks the retaliate script adds to the clock: attackrate/2 (integer divide), minus one with rapid.
func Flinch(attackRate int, rapid bool) int {
	d := attackRate / 2
	if rapid {
		d--
	}
	return d
}

func (s *Sim) interact() {
	switch s.tgt {
	case tTreeFirst:
		// attempt_cut_tree (woodcut.rs2:55-62, 84-88)
		if s.AD < s.Clock {
			s.AD = s.Clock + 3
			return // p_oploc(1)
		}
		if s.AD == s.Clock {
			s.Rolls = append(s.Rolls, s.Clock)
		}
		s.tgt = tTreeCont // p_oploc(3)
	case tTreeCont:
		// cut_tree (woodcut.rs2:118-125)
		if s.AD < s.Clock {
			s.AD = s.Clock + 3
		} else if s.AD == s.Clock {
			s.Rolls = append(s.Rolls, s.Clock)
		}
	case tNPC:
		// player_ranged_attack (player_ranged.rs2:6-31)
		if s.AD > s.Clock {
			return // p_opnpc(2)
		}
		s.AD = s.Clock + s.Cfg.AttackRate
		if s.Cfg.Rapid {
			s.AD--
		}
		if !s.Cfg.Ammo {
			s.Refused = append(s.Refused, s.Clock)
			s.tgt = tNone // p_stopaction
			return
		}
		s.Shots = append(s.Shots, s.Clock)
	}
}

// Gaps returns the spacing between consecutive roll ticks.
func Gaps(rolls []int) []int {
	var g []int
	for i := 1; i < len(rolls); i++ {
		g = append(g, rolls[i]-rolls[i-1])
	}
	return g
}

// Policy chooses the client ops written for the tick about to be read. hit tells it which ticks a monster will hit
// (a real client reads that from the monsters' attack animations and its own hitsplats).
type Policy func(s *Sim, hitNow, hitPrev bool) []Op

// PolicyPlainClick clicks the tree once, on the first tick, and never again.
func PolicyPlainClick() Policy {
	first := true
	return func(s *Sim, hitNow, hitPrev bool) []Op {
		if first {
			first = false
			return []Op{OpClickTree}
		}
		return nil
	}
}

// PolicyKnife is the existing manipulation: a batch in the stale tick after every roll (and the opening click).
func PolicyKnife() Policy {
	first := true
	return func(s *Sim, hitNow, hitPrev bool) []Op {
		if first {
			first = false
			return []Op{OpClickTree}
		}
		if r := s.LastRoll(); r >= 0 && s.Clock == r+1 {
			return []Op{OpKnifeBatch}
		}
		return nil
	}
}

// PolicyRetaliate is the two-tick loop: on a tick in which a monster will hit, clear the interaction; on the tick
// after a hit, click the tree. clear is OpClear or OpDrop.
func PolicyRetaliate(clear Op) Policy {
	started := false
	return func(s *Sim, hitNow, hitPrev bool) []Op {
		switch {
		case hitNow:
			return []Op{clear}
		case hitPrev:
			return []Op{OpClickTree}
		case !started:
			started = true
			return []Op{OpClickTree}
		}
		return nil
	}
}

// Run drives a Sim for ticks ticks. hits(t) says whether a monster hits at tick offset t (0-based from the start).
func Run(cfg Config, ticks int, hits func(t int) bool, p Policy) *Sim {
	s := New(cfg)
	for t := 0; t < ticks; t++ {
		s.Tick(p(s, hits(t), t > 0 && hits(t-1)), hits(t))
	}
	return s
}

// EveryN returns a hit schedule of one hit every n ticks starting at offset phase.
func EveryN(n, phase int) func(int) bool {
	return func(t int) bool { return t >= phase && (t-phase)%n == 0 }
}

// Union is the hit schedule of several monsters.
func Union(fs ...func(int) bool) func(int) bool {
	return func(t int) bool {
		for _, f := range fs {
			if f(t) {
				return true
			}
		}
		return false
	}
}
