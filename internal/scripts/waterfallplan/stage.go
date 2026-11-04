package waterfallplan

import (
	"fmt"
	"strings"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// Stage is %waterfall_quest, from quest_waterfall.constant (^waterfall_complete = 10 in
// general/configs/quest.constant). There is no 7 and no 9.
type Stage int

const (
	StageNotStarted       Stage = 0
	StageStarted          Stage = 1  // Almera, "How can I help?"
	StageSpokenToHudon    Stage = 2  // set mid-way through Hudon's dialogue
	StageReadBook         Stage = 3  // opheld1 on the Book on baxtorian
	StageEnteredTomb      Stage = 4  // the pebble on the tombstone, with a tomb-safe pack
	StageEnteredWaterfall Stage = 5  // the ledge door, with the amulet worn OR carried
	StageEnteredPuzzle    Stage = 6  // the key on the puzzle door (2566,9901) at stage 5
	StageRaisedFloor      Stage = 8  // the amulet on the statue with all 18 pillar bits set
	StageComplete         Stage = 10 // the full urn on the chalice
)

// Valid reports whether s is one of content's stage values.
func (s Stage) Valid() bool {
	switch s {
	case StageNotStarted, StageStarted, StageSpokenToHudon, StageReadBook, StageEnteredTomb,
		StageEnteredWaterfall, StageEnteredPuzzle, StageRaisedFloor, StageComplete:
		return true
	}
	return false
}

func (s Stage) String() string {
	switch s {
	case StageNotStarted:
		return "not started"
	case StageStarted:
		return "started (spoke to Almera)"
	case StageSpokenToHudon:
		return "spoke to Hudon"
	case StageReadBook:
		return "read the Book on Baxtorian"
	case StageEnteredTomb:
		return "entered Glarial's tomb"
	case StageEnteredWaterfall:
		return "entered Baxtorian Falls"
	case StageEnteredPuzzle:
		return "entered the pillar room"
	case StageRaisedFloor:
		return "raised the floor"
	case StageComplete:
		return "complete"
	}
	return fmt.Sprintf("stage %d", int(s))
}

// journalNeedles are ParseJournal's phrases, NEWEST FIRST. waterfall_journal.rs2 strikes the
// earlier entries through (@str@) but keeps every one of them, so an older phrase is present at
// every later stage and only the order makes the answer right. Each needle is written against
// questplan.NormaliseLines' form (lower case, colour tags and line breaks as single spaces) and
// checked against the real journal text in stage_test.go.
var journalNeedles = []struct {
	stage  Stage
	needle string
}{
	{StageComplete, "quest complete!"},
	{StageRaisedFloor, "worked out how to raise the floor"},
	{StageRaisedFloor, "now i just need to retrieve the treasure"},
	{StageEnteredPuzzle, "it is floating out of reach"},
	{StageEnteredPuzzle, "i'll need to work out how to get to it"},
	{StageEnteredWaterfall, "used it to enter baxtorian falls"},
	{StageEnteredTomb, "managed to enter the tomb of glarial"},
	{StageReadBook, "the book also mentions glarial's tomb"},
	{StageSpokenToHudon, "i found hudon a short raft ride down the river"},
	{StageStarted, "i spoke to almera in a house close to the baxtorian waterfall"},
	{StageNotStarted, "i can start this quest"},
}

// ParseJournal reads the stage from the rendered journal lines. ok is false for an empty,
// loading or unrecognised journal, which a caller must treat as "unknown", never as stage 0.
// Ported from parseWaterfallJournal.
func ParseJournal(lines []string) (Stage, bool) {
	text := questplan.NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range journalNeedles {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// GolriePebbleFound reports whether the stage-3 journal says Golrie's pebble was already found:
// the journal strikes the pebble line through once bit 0 of %waterfall_golrie_and_puzzle is set.
// It is the only client-side reading of that bit, and it matters because once it is set the
// gate's op1 answers "Golrie has locked himself in." and only the key opens it.
func GolriePebbleFound(lines []string) bool {
	for _, l := range lines {
		ll := strings.ToLower(l)
		if strings.Contains(ll, "@str@from what i understand glarial's pebble") {
			return true
		}
	}
	return false
}

// Area is where the account is, as far as the quest cares. The mound, the tree bank and the
// ledge are one-way; the three dungeons are left only by their own exits.
type Area int

const (
	AreaUnknown Area = iota
	AreaMainland
	AreaMound
	AreaTreeBank
	AreaLedge
	AreaTGV
	AreaTomb
	// AreaDungeonSouth is the waterfall dungeon below the south keyed door: the entry, the
	// shadow-spider room, the crate room and the fire-giant lane.
	AreaDungeonSouth
	// AreaCorridor is between the south keyed door and the puzzle door.
	AreaCorridor
	AreaPuzzleRoom
	AreaRaisedRoom
	// AreaDungeonOther is anywhere else inside the waterfall dungeon's bounds.
	AreaDungeonOther
)

func (a Area) String() string {
	switch a {
	case AreaMainland:
		return "mainland"
	case AreaMound:
		return "Hudon's mound"
	case AreaTreeBank:
		return "the tree bank"
	case AreaLedge:
		return "the waterfall ledge"
	case AreaTGV:
		return "the Tree Gnome Village dungeon"
	case AreaTomb:
		return "Glarial's tomb"
	case AreaDungeonSouth:
		return "the waterfall dungeon (south)"
	case AreaCorridor:
		return "the waterfall dungeon corridor"
	case AreaPuzzleRoom:
		return "the pillar room"
	case AreaRaisedRoom:
		return "the raised chalice room"
	case AreaDungeonOther:
		return "the waterfall dungeon"
	}
	return "unknown"
}

// InWaterfallDungeon reports whether a is any part of the waterfall dungeon.
func (a Area) InWaterfallDungeon() bool {
	switch a {
	case AreaDungeonSouth, AreaCorridor, AreaPuzzleRoom, AreaRaisedRoom, AreaDungeonOther:
		return true
	}
	return false
}

// OnFalls reports whether a is one of the three one-way river areas.
func (a Area) OnFalls() bool { return a == AreaMound || a == AreaTreeBank || a == AreaLedge }

// AreaOf classifies a tile. The specific rooms are tested before the dungeon's enclosing box.
// Ported from waterfallArea, with the dungeon split at the two keyed doors and the raised room
// widened to include ^waterfall_raised_room_door_coord (2604,9901), which the TS box (z 9905+)
// left out.
func AreaOf(t tile.Tile, known bool) Area {
	if !known {
		return AreaUnknown
	}
	x, z := t.X, t.Z
	if t.Level != 0 {
		return AreaMainland
	}
	switch {
	case x >= 2596 && x <= 2625 && z >= 9901 && z <= 9930:
		return AreaRaisedRoom
	case x >= 2555 && x <= 2575 && z >= 9902 && z <= 9925:
		return AreaPuzzleRoom
	case x >= 2560 && x <= 2572 && z >= 9894 && z <= 9901:
		return AreaCorridor
	case x >= 2555 && x <= 2600 && z >= 9850 && z <= 9893:
		return AreaDungeonSouth
	case x >= 2490 && x <= 2630 && z >= 9850 && z <= 9950:
		return AreaDungeonOther
	case x >= 2500 && x <= 2570 && z >= 9780 && z < 9850:
		return AreaTomb
	case x >= 2480 && x <= 2570 && z >= 9500 && z <= 9650:
		return AreaTGV
	case x >= 2505 && x <= 2518 && z >= 3474 && z <= 3486:
		return AreaMound
	case x >= 2505 && x <= 2518 && z >= 3466 && z <= 3473:
		return AreaTreeBank
	case x >= 2505 && x <= 2518 && z >= 3460 && z <= 3465:
		return AreaLedge
	}
	return AreaMainland
}

// InLane reports whether t is in the fire-giant lane: north of the west Large door and south of
// the south keyed door, x 2560-2570. The three fire giants (level 86, max hit 11) spawn on it at
// 2562,9886, 2565,9887 and 2568,9889.
func InLane(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2560 && t.X <= 2571 && t.Z >= 9882 && t.Z <= 9893
}
