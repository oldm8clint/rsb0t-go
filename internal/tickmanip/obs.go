package tickmanip

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// What a client saw, tick by tick. The recorder (internal/tickmanip/rec) fills Rows from the decoded packet
// stream; everything here is pure, so the attribution rules and the arithmetic over rows are unit-tested
// without a socket.

// Phase says where in a server cycle a packet was written, which decides which tick the client must credit it to.
//
// The engine's cycle (engine src/engine/World.ts cycle(), lab checkout) writes to a client in two places:
//
//   - while the player is being processed (processClientsIn, processPlayers: handlers and scripts call
//     player.write at once), so MESSAGE_GAME, VARP_* and IF_SET* of tick T arrive BEFORE tick T's PLAYER_INFO;
//   - in processClientsOut, after processInfo: updateMap, PLAYER_INFO, NPC_INFO, zone updates, inventories
//     (UPDATE_INV_*), stats (UPDATE_STAT, UPDATE_RUNENERGY) and, last of all, encodeOut's modal opens and
//     closes, so those arrive AFTER tick T's PLAYER_INFO.
//
// A client that counts PLAYER_INFO packets therefore credits a Processing packet to the counter plus one (the
// next PLAYER_INFO has not arrived yet) and a Flush packet to the counter itself. Getting this wrong by one
// would shift every message against every inventory change, which is exactly the comparison a roll needs.
type Phase int

const (
	// Processing is written while the tick is processed: before that tick's PLAYER_INFO.
	Processing Phase = iota
	// Flush is written by processClientsOut after PLAYER_INFO: it belongs to the tick that PLAYER_INFO closed.
	Flush
)

// ServerTick is the PLAYER_INFO ordinal of the server tick a packet belongs to, given how many PLAYER_INFO the
// client had counted when the packet arrived.
func ServerTick(p Phase, seen int) int {
	if p == Processing {
		return seen + 1
	}
	return seen
}

// NoAnim marks a Row in which the local player started no animation.
const NoAnim = -1 << 20

// InvDelta is one inventory slot change.
type InvDelta struct {
	Comp, Slot       int
	ID, PrevID       int32
	Count, PrevCount int32
}

// Row is what one server tick looked like to the client.
type Row struct {
	// N is the PLAYER_INFO ordinal.
	N int
	// At is when this tick's PLAYER_INFO arrived.
	At time.Time
	// X, Z is the local player's tile after the tick's movement; HaveTile is false before the first position.
	X, Z     int
	HaveTile bool
	// Anim is the animation sequence the server started on the local player this tick, NoAnim for none (a
	// cancelled animation reads as -1, never as NoAnim).
	Anim int
	// Msgs are the game messages this tick's scripts produced, in packet order.
	Msgs []string
	// XP is the whole-xp gained per stat landed this tick.
	XP map[int]int
	// Inv are the backpack/bank slot changes landed this tick.
	Inv []InvDelta
	// Energy is the run energy percent the client held after this tick (-1 unknown).
	Energy int
	// Sent names the packets the client wrote for this tick, in the order it wrote them. A packet written after the
	// client saw tick N is read by the server in tick N+1, so the recorder files it under N+1.
	Sent []string
	// Steps is how far the local player moved this tick, in tiles (capped at 2: a run), from the tile before it.
	Steps int
	// MapUnset counts the UNSET_MAP_FLAG packets this tick's processing wrote: the server writes one when it drops the
	// player's route or refuses a movement or op click while the player is delayed (engine MoveClickHandler, OpLocHandler,
	// OpNpcHandler, World.processClientsIn), and one when a route ends.
	MapUnset int
	// Modal is true when a main or chat interface was open at the end of this tick's flush (the client learns it one
	// PLAYER_INFO later, so the recorder files it under the tick before).
	Modal bool
}

// NewRow returns an empty Row for tick n.
func NewRow(n int) *Row {
	return &Row{N: n, Anim: NoAnim, Energy: -1}
}

// Net sums the inventory changes of a row into obj id -> net count change. A slot whose item id changed counts
// the old item as lost and the new one as gained, so a swap of two different items shows both.
func (r *Row) Net() map[int32]int {
	out := map[int32]int{}
	for _, d := range r.Inv {
		if d.PrevID == d.ID {
			out[d.ID] += int(d.Count - d.PrevCount)
			continue
		}
		if d.PrevID != 0 {
			out[d.PrevID] -= int(d.PrevCount)
		}
		if d.ID != 0 {
			out[d.ID] += int(d.Count)
		}
	}
	for id, n := range out {
		if n == 0 {
			delete(out, id)
		}
	}
	return out
}

// TotalXP is the xp gained this tick across every stat.
func (r *Row) TotalXP() int {
	t := 0
	for _, v := range r.XP {
		t += v
	}
	return t
}

// HasMsg reports whether any message of the row contains substr (case-insensitive).
func (r *Row) HasMsg(substr string) bool {
	want := strings.ToLower(substr)
	for _, m := range r.Msgs {
		if strings.Contains(strings.ToLower(m), want) {
			return true
		}
	}
	return false
}

// RowSet is a window of rows keyed by tick, plus the arm geometry: arm tick 0 is server tick Base.
type RowSet struct {
	Base int
	Rows map[int]*Row
}

// At returns the row of arm tick t, or an empty one (never nil) when the client saw nothing that tick.
func (s *RowSet) At(t int) *Row {
	if r, ok := s.Rows[s.Base+t]; ok {
		return r
	}
	return NewRow(s.Base + t)
}

// Ticks lists the arm ticks present, ascending.
func (s *RowSet) Ticks() []int {
	out := make([]int, 0, len(s.Rows))
	for n := range s.Rows {
		out = append(out, n-s.Base)
	}
	sort.Ints(out)
	return out
}

// Intervals are the wall-clock gaps between consecutive PLAYER_INFO arrivals, for checking the world's tick rate
// (200 ms at the lab's default, 600 ms after ::speed 600). Ticks missing from the set break the chain.
func (s *RowSet) Intervals() []time.Duration {
	var out []time.Duration
	var prev *Row
	for _, t := range s.Ticks() {
		r := s.Rows[s.Base+t]
		if prev != nil && r.N == prev.N+1 && !r.At.IsZero() && !prev.At.IsZero() {
			out = append(out, r.At.Sub(prev.At))
		}
		prev = r
	}
	return out
}

// DurationStats is the mean and standard deviation, in milliseconds, of a set of gaps.
func DurationStats(ds []time.Duration) (meanMs, sdMs float64) {
	vals := make([]float64, len(ds))
	for i, d := range ds {
		vals[i] = float64(d) / float64(time.Millisecond)
	}
	return Mean(vals), SD(vals)
}

// Describe is one trace line: the tick, what was sent, and what came back.
func (r *Row) Describe(armTick int) string {
	var parts []string
	if len(r.Sent) > 0 {
		parts = append(parts, "sent["+strings.Join(r.Sent, ",")+"]")
	}
	if r.Anim != NoAnim {
		parts = append(parts, fmt.Sprintf("anim=%d", r.Anim))
	}
	for _, m := range r.Msgs {
		parts = append(parts, fmt.Sprintf("msg=%q", m))
	}
	if xp := r.TotalXP(); xp != 0 {
		parts = append(parts, fmt.Sprintf("xp=%v", r.XP))
	}
	if net := r.Net(); len(net) > 0 {
		parts = append(parts, fmt.Sprintf("inv=%v", net))
	}
	if r.Energy >= 0 {
		parts = append(parts, fmt.Sprintf("energy=%d%%", r.Energy))
	}
	if r.MapUnset > 0 {
		parts = append(parts, fmt.Sprintf("unsetflag=%d", r.MapUnset))
	}
	if r.Steps > 0 {
		parts = append(parts, fmt.Sprintf("moved=%d@%d,%d", r.Steps, r.X, r.Z))
	}
	return fmt.Sprintf("t+%d (tick %d): %s", armTick, r.N, strings.Join(parts, " "))
}
