package tickmanip

import (
	"fmt"
	"sort"
)

// ParseStep reads a Step back from its String form, the form the recorder files under Row.Sent.
func ParseStep(name string) (Step, bool) {
	for _, st := range []Step{KnifeUse, MakeShafts, MoveOwn, Horsey, Click, WalkClick, CastHigh, CastLow, RubRing, RubNecklace, Bury, Wield, ClearMove, CloseModal, CookUse, Ack} {
		if st.String() == name {
			return st, true
		}
	}
	return 0, false
}

// PlanFromRows rebuilds the plan the client actually executed from what it recorded as sent, tick by tick. Replaying
// THAT through the model, rather than the plan the driver meant to run, is what makes the comparison honest: a driver
// that reacted to what it saw (re-clicked after a depletion, waited for an animation) is judged on the packets that
// really went out, in the order they went. Names that are not steps (cheats, drops) are skipped.
func PlanFromRows(set *RowSet) Plan {
	by := map[int][]Step{}
	for n, row := range set.Rows {
		for _, name := range row.Sent {
			if st, ok := ParseStep(name); ok {
				by[n-set.Base] = append(by[n-set.Base], st)
			}
		}
	}
	return func(t int) []Step { return by[t] }
}

// Observation is a window's facts as lists of arm ticks, ascending.
type Observation struct {
	// Success are the ticks a catch landed (experience in the skill).
	Success []int
	// Anim are the ticks the server started an animation on the player; Cancel the ticks it cancelled one (an
	// animation id of -1, which no model step produces on purpose).
	Anim, Cancel []int
	// Fletch are the ticks Fletching experience landed: a queued fletch ran and a log was consumed.
	Fletch []int
}

// Observe reads rows 0..upTo of a window. skillStat is the stat whose experience marks a catch, fletchStat Fletching.
func Observe(set *RowSet, upTo int, skillStat, fletchStat int) Observation {
	var o Observation
	for t := 0; t <= upTo; t++ {
		row, ok := set.Rows[set.Base+t]
		if !ok {
			continue
		}
		if row.XP[skillStat] > 0 {
			o.Success = append(o.Success, t)
		}
		if row.XP[fletchStat] > 0 {
			o.Fletch = append(o.Fletch, t)
		}
		switch {
		case row.Anim == NoAnim:
		case row.Anim < 0:
			o.Cancel = append(o.Cancel, t)
		default:
			o.Anim = append(o.Anim, t)
		}
	}
	return o
}

// Comparison lines the model's prediction for a window against what the lab saw.
type Comparison struct {
	Horizon int
	// Model* are the model's ticks within the horizon.
	ModelRolls, ModelAnims, ModelFletch []int
	// Obs* are the lab's.
	ObsSuccess, ObsAnims, ObsFletch []int
	// SuccessOffRoll are catches on a tick the model had no roll for: the model is wrong when this is non-empty.
	SuccessOffRoll []int
	// RollNoSuccess are model rolls with no catch (expected whenever a roll can fail).
	RollNoSuccess []int
	// AnimMissed are animation ticks the model predicted that the lab did not show; AnimExtra the reverse.
	AnimMissed, AnimExtra []int
	// FletchMissed and FletchExtra are the same for leaked fletches.
	FletchMissed, FletchExtra []int
}

// OK is true when the lab agrees with the model: no catch off a roll tick, the same animation ticks, the same
// leaked fletches.
func (c Comparison) OK() bool {
	return len(c.SuccessOffRoll) == 0 && len(c.AnimMissed) == 0 && len(c.AnimExtra) == 0 && len(c.FletchMissed) == 0 && len(c.FletchExtra) == 0
}

// ExactRolls is true when every model roll had a catch and no catch was off a roll (a skill whose roll cannot fail).
func (c Comparison) ExactRolls() bool {
	return len(c.SuccessOffRoll) == 0 && len(c.RollNoSuccess) == 0
}

// Summary is one line for the log.
func (c Comparison) Summary() string {
	return fmt.Sprintf("horizon t+%d: model rolls %d, catches %d (off a roll tick: %v); animations model %d lab %d (missed %v, extra %v); leaked fletches model %d lab %d",
		c.Horizon, len(c.ModelRolls), len(c.ObsSuccess), c.SuccessOffRoll, len(c.ModelAnims), len(c.ObsAnims), c.AnimMissed, c.AnimExtra, len(c.ModelFletch), len(c.ObsFletch))
}

// Compare replays plan through the model for the skill and compares ticks 0..horizon with obs.
func Compare(sk Skill, plan Plan, horizon int, obs Observation) Comparison {
	tr := Replay(sk, plan, horizon+1)
	c := Comparison{Horizon: horizon}
	for _, r := range tr.Results {
		if r.Tick > horizon {
			break
		}
		if r.Roll {
			c.ModelRolls = append(c.ModelRolls, r.Tick)
		}
		if r.AnyAnim() {
			c.ModelAnims = append(c.ModelAnims, r.Tick)
		}
		if r.Fletched {
			c.ModelFletch = append(c.ModelFletch, r.Tick)
		}
	}
	c.ObsSuccess = upTo(obs.Success, horizon)
	// An animation UPDATE is what the model predicts, and a cancel is one too: depletion's anim(null, 0) replaces the
	// roll's own animation in the same tick (get_logs runs the roll, then fells the tree), so the tick still shows one update.
	c.ObsAnims = upTo(mergeTicks(obs.Anim, obs.Cancel), horizon)
	c.ObsFletch = upTo(obs.Fletch, horizon)

	rolls := toSet(c.ModelRolls)
	for _, t := range c.ObsSuccess {
		if !rolls[t] {
			c.SuccessOffRoll = append(c.SuccessOffRoll, t)
		}
	}
	got := toSet(c.ObsSuccess)
	for _, t := range c.ModelRolls {
		if !got[t] {
			c.RollNoSuccess = append(c.RollNoSuccess, t)
		}
	}
	c.AnimMissed, c.AnimExtra = DiffInts(c.ModelAnims, c.ObsAnims)
	c.FletchMissed, c.FletchExtra = DiffInts(c.ModelFletch, c.ObsFletch)
	return c
}

func mergeTicks(a, b []int) []int {
	set := toSet(a)
	for _, t := range b {
		set[t] = true
	}
	out := make([]int, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Ints(out)
	return out
}

func upTo(ticks []int, limit int) []int {
	var out []int
	for _, t := range ticks {
		if t <= limit {
			out = append(out, t)
		}
	}
	return out
}

func toSet(ticks []int) map[int]bool {
	m := make(map[int]bool, len(ticks))
	for _, t := range ticks {
		m[t] = true
	}
	return m
}

// SortedKeys returns a histogram's keys ascending, for printing.
func SortedKeys(h map[int]int) []int {
	keys := make([]int, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// AckName is how the recorder files the acknowledgement packet a batch carries, and AckPrefix the start of the message
// the server answers it with.
//
// The ack is `::getvar com_maxhit`, a cheat that only READS a varp (com_maxhit is protect=no, so the handler
// neither closes a modal nor clears the interaction) and answers "get com_maxhit: N" in the tick it is read. The
// lab engine shares a saturated machine with the live fleet, so a tick can start before a batch the client sent in
// reaction to the last PLAYER_INFO has arrived, and the server then reads that batch a tick LATE. The ack names the
// tick each batch was really read in, so a late batch is neither mistaken for a model failure nor allowed to pass
// as an on-time one.
const (
	AckName   = "ack"
	AckPrefix = "get com_maxhit"
)

// Acks counts the ack answers in a row.
func (r *Row) Acks() int {
	n := 0
	for _, m := range r.Msgs {
		if len(m) >= len(AckPrefix) && m[:len(AckPrefix)] == AckPrefix {
			n++
		}
	}
	return n
}

// Batch is one write: the steps it carried, the tick the client meant the server to read it in, and the tick the
// server really read it in (-1 when no ack was seen).
type Batch struct {
	Intended, Actual int
	Steps            []Step
}

// Late is how many ticks late the server read the batch (0 on time, -1 when unread).
func (b Batch) Late() int {
	if b.Actual < 0 {
		return -1
	}
	return b.Actual - b.Intended
}

// PairBatches matches each recorded write (a row whose Sent holds steps) with the ack answers in order: the server
// reads one connection's packets in the order they were written, so the k-th ack belongs to the k-th write. Ticks are
// arm-relative. The ack leads its batch, so the tick of its answer is the tick the batch's first packet was read in; the
// batch's Steps keep the Ack in front, so the model counts it against the five-a-tick user-event budget.
func PairBatches(set *RowSet) []Batch {
	var batches []Batch
	var ackTicks []int
	for _, t := range set.Ticks() {
		row := set.Rows[set.Base+t]
		var steps []Step
		wrote := false
		for _, name := range row.Sent {
			if st, ok := ParseStep(name); ok {
				steps = append(steps, st)
				wrote = true
			}
		}
		if wrote {
			batches = append(batches, Batch{Intended: t, Actual: -1, Steps: steps})
		}
		for i := 0; i < row.Acks(); i++ {
			ackTicks = append(ackTicks, t)
		}
	}
	for i := range batches {
		if i < len(ackTicks) {
			batches[i].Actual = ackTicks[i]
		}
	}
	return batches
}

// PlanFromBatches is the plan the SERVER executed: each batch placed at the tick it was really read in.
func PlanFromBatches(bs []Batch) Plan {
	by := map[int][]Step{}
	for _, b := range bs {
		at := b.Actual
		if at < 0 {
			at = b.Intended
		}
		by[at] = append(by[at], b.Steps...)
	}
	return func(t int) []Step { return by[t] }
}

// LateStats counts batches read on time, one tick late, more than one late and never.
func LateStats(bs []Batch) (onTime, late1, lateMore, unread int) {
	for _, b := range bs {
		switch l := b.Late(); {
		case l < 0:
			unread++
		case l == 0:
			onTime++
		case l == 1:
			late1++
		default:
			lateMore++
		}
	}
	return
}
