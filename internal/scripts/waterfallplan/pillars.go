package waterfallplan

import (
	"fmt"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// PillarCount and RuneKinds size the puzzle: six pillars, three runes each, 18 bits of
// %waterfall_golrie_and_puzzle (bits 1-18; bit 0 is Golrie's pebble).
const (
	PillarCount = 6
	RuneKinds   = 3
)

// Pillars are the six stands in content's own index order (waterfall_pillars.rs2
// get_pillar_index: 1 sw, 2 w, 3 nw, 4 se, 5 e, 6 ne).
var Pillars = [PillarCount]tile.Tile{
	{X: 2562, Z: 9910}, {X: 2562, Z: 9912}, {X: 2562, Z: 9914},
	{X: 2569, Z: 9910}, {X: 2569, Z: 9912}, {X: 2569, Z: 9914},
}

// PillarNames label Pillars for the log.
var PillarNames = [PillarCount]string{"south-west", "west", "north-west", "south-east", "east", "north-east"}

// PillarStand is the tile inside the room beside pillar p: east of the west column, west of the
// east column. The runner checks it against locreach and the pack before using it.
func PillarStand(p int) tile.Tile {
	t := Pillars[p]
	if t.X < 2565 {
		return tile.Tile{X: t.X + 1, Z: t.Z}
	}
	return tile.Tile{X: t.X - 1, Z: t.Z}
}

// PuzzleRunes are the three runes in content's get_rune_index order: 1 air, 2 earth, 3 water.
var PuzzleRunes = [RuneKinds]int{ItemAirRune, ItemEarthRune, ItemWaterRune}

// RuneName labels a puzzle rune for the log.
func RuneName(id int) string {
	switch id {
	case ItemAirRune:
		return "Air rune"
	case ItemEarthRune:
		return "Earth rune"
	case ItemWaterRune:
		return "Water rune"
	}
	return fmt.Sprintf("obj %d", id)
}

// PillarAck is what the server said after one rune was used on one pillar.
type PillarAck int

const (
	// AckNone: neither acknowledgement arrived.
	AckNone PillarAck = iota
	// AckPlaced: "You place the rune on the stand." — the bit was unset, is now set, and the
	// rune was deleted.
	AckPlaced
	// AckRemembered: "You remember putting that type of rune there." — the bit was already set
	// (a previous visit), nothing was consumed.
	AckRemembered
)

// ClassifyPillarMessage reads one game message. It deliberately matches the two exact sentences
// of waterfall_pillars.rs2 and nothing looser: the whole point of the ledger is that no other
// reading can stand in for the server's own word.
func ClassifyPillarMessage(text string) PillarAck {
	t := strings.ToLower(strings.TrimSpace(text))
	switch {
	case strings.Contains(t, "you place the rune on the stand"):
		return AckPlaced
	case strings.Contains(t, "you remember putting that type of rune there"):
		return AckRemembered
	}
	return AckNone
}

// PillarLedger records which of the 18 (pillar, rune) pairs the server acknowledged in THIS
// visit to the pillar room.
//
// It exists because of the statue. [oplocu,statue_queen_waterfall_quest] checks
// getbit_range(%waterfall_golrie_and_puzzle, 1, 18) and, if any bit is missing, drops a boulder
// for damage_self(20) — death for a 20-hitpoint account even at full health. The varp is not
// transmitted, so the ONLY evidence that a bit is set is the server's acknowledgement of that
// exact use. A rune count cannot stand in for it: a stack the account arrived with short, or a
// use the server dropped (a modal up, the pack not visible), leaves the count and the varp
// disagreeing, and the TS module died of exactly that shortcut (see the package doc).
//
// The zero value is an empty ledger. The runner resets it every time the account enters the
// room, because an acknowledgement from an earlier visit proves nothing about a use that has
// not been re-checked — even though the varp itself persists, re-checking a set pair costs
// nothing ("You remember putting that type of rune there." consumes no rune).
type PillarLedger struct {
	acked [PillarCount][RuneKinds]bool
}

// Empty reports a ledger with no acknowledgement at all.
func (l PillarLedger) Empty() bool {
	for p := range l.acked {
		for r := range l.acked[p] {
			if l.acked[p][r] {
				return false
			}
		}
	}
	return true
}

// Ack records the server's acknowledgement of rune kind r on pillar p. Only AckPlaced and
// AckRemembered count.
func (l *PillarLedger) Ack(p, r int, ack PillarAck) {
	if p < 0 || p >= PillarCount || r < 0 || r >= RuneKinds {
		return
	}
	if ack == AckPlaced || ack == AckRemembered {
		l.acked[p][r] = true
	}
}

// Acked reports whether pair (p, r) was acknowledged.
func (l PillarLedger) Acked(p, r int) bool {
	if p < 0 || p >= PillarCount || r < 0 || r >= RuneKinds {
		return false
	}
	return l.acked[p][r]
}

// Count is how many of the 18 pairs are acknowledged.
func (l PillarLedger) Count() int {
	n := 0
	for p := 0; p < PillarCount; p++ {
		for r := 0; r < RuneKinds; r++ {
			if l.acked[p][r] {
				n++
			}
		}
	}
	return n
}

// Complete is true only when all 18 pairs were acknowledged. It is the statue's one gate.
func (l PillarLedger) Complete() bool { return l.Count() == PillarCount*RuneKinds }

// Next is the first unacknowledged pair, pillar-major (content's own order), and ok=false when
// the ledger is complete.
func (l PillarLedger) Next() (p, r int, ok bool) {
	for p = 0; p < PillarCount; p++ {
		for r = 0; r < RuneKinds; r++ {
			if !l.acked[p][r] {
				return p, r, true
			}
		}
	}
	return 0, 0, false
}

// RunesNeeded is how many of each rune the ledger can still consume: one per unacknowledged
// pair of that kind. The pack must hold at least this many before a pair is tried, or the pair
// can only fail; RunesShort names what is missing.
func (l PillarLedger) RunesNeeded() map[int]int {
	out := map[int]int{}
	for p := 0; p < PillarCount; p++ {
		for r := 0; r < RuneKinds; r++ {
			if !l.acked[p][r] {
				out[PuzzleRunes[r]]++
			}
		}
	}
	return out
}

// RunesShort reports the first rune kind the pack does not hold even one of while a pair of that
// kind is still unacknowledged. ok is false when every remaining pair can at least be tried.
func (l PillarLedger) RunesShort(inv map[int]int) (id int, short bool) {
	for p := 0; p < PillarCount; p++ {
		for r := 0; r < RuneKinds; r++ {
			if !l.acked[p][r] && inv[PuzzleRunes[r]] <= 0 {
				return PuzzleRunes[r], true
			}
		}
	}
	return 0, false
}

// String is the log form: "12/18 (missing: east water, ...)".
func (l PillarLedger) String() string {
	var missing []string
	for p := 0; p < PillarCount; p++ {
		for r := 0; r < RuneKinds; r++ {
			if !l.acked[p][r] {
				missing = append(missing, PillarNames[p]+" "+strings.ToLower(strings.TrimSuffix(RuneName(PuzzleRunes[r]), " rune")))
			}
		}
	}
	if len(missing) == 0 {
		return fmt.Sprintf("%d/18", l.Count())
	}
	if len(missing) > 4 {
		missing = append(missing[:4], fmt.Sprintf("+%d more", len(missing)-4))
	}
	return fmt.Sprintf("%d/18 (missing: %s)", l.Count(), strings.Join(missing, ", "))
}
