package waterfallplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// journalText is waterfall_journal.rs2's $text for each stage, verbatim (appends joined), so the
// parser is tested against the server's own words rather than paraphrases of them. The client
// renders it split on "|" into the scroll's lines, which journalLines reproduces.
var journalText = map[Stage]string{
	StageNotStarted: "@dbl@I can start this quest by speaking to @dre@Almera@dbl@ in her house|@dbl@next to the @dre@Baxtorian Falls@dbl@.||" +
		"@dbl@I need to be able to fight @dre@Level 84 Giants@dbl@.",
	StageStarted: "@dbl@I spoke to @dre@Almera@dbl@ in a house close to the Baxtorian waterfall. Her son was missing so I offered to help find him. The boy, @dre@Hudon's@dbl@ looking for treasure in the waterfall.",
	StageSpokenToHudon: strikeStart +
		"@dbl@I found Hudon a short raft ride down the river. He is convinced there is treasure here somewhere. Maybe I need to do a little research.|",
	StageReadBook: strikeStart + strikeHudon + strikeBook +
		"@dbl@The book also mentions @dre@Glarial's tomb@dbl@ and a pebble, it appears that the pebble is used to enter the tomb.|" +
		"@dbl@From what I understand @dre@Glarial's pebble@dbl@ was hidden in a cave under the @dre@Tree Gnome Village@dbl@ by @dre@Golrie's@dbl@ ancestors.",
	StageEnteredTomb: strikeStart + strikeHudon + strikeBook + strikeTomb +
		"@str@I have managed to enter the tomb of Glarial.|@dbl@There must be something that will help me down here.",
	StageEnteredWaterfall: strikeStart + strikeHudon + strikeBook + strikeTomb + "@str@I have managed to enter the tomb of Glarial.||" +
		"@dbl@Inside the tomb I found @dre@Glarial's Amulet@dbl@ and used it to enter @dre@Baxtorian Falls@dbl@. I should look around and see if I can find the treasure.",
	StageEnteredPuzzle: strikeStart + strikeHudon + strikeBook + strikeTomb + "@str@I have managed to enter the tomb of Glarial.||" +
		"@str@Inside the tomb I found Glarial's Amulet and used it to|@str@enter Baxtorian Falls.||" +
		"@dbl@I found a @dre@Chalice@dbl@ within @dre@Baxtorian Falls@dbl@ which looks like it contains @dre@Baxtorian's Treasure@dbl@. However, it is floating out of reach. I'll need to work out how to get to it.",
	StageRaisedFloor: strikeStart + strikeHudon + strikeBook + strikeTomb + "@str@I have managed to enter the tomb of Glarial.||" +
		"@str@Inside the tomb I found Glarial's Amulet and used it to|@str@enter Baxtorian Falls.||" +
		"@dbl@I found a @dre@Chalice@dbl@ within @dre@Baxtorian Falls@dbl@ which looks like it contains @dre@Baxtorian's Treasure@dbl@. The @dre@Chalice@dbl@ was floating out of reach but I worked out how to raise the floor to get to it. Now I just need to retrieve the treasure from it.",
	StageComplete: strikeStart + strikeHudon + strikeBook + strikeTomb + "@str@I have managed to enter the tomb of Glarial.||" +
		"@str@Inside the tomb I found Glarial's Amulet and used it to|@str@enter Baxtorian Falls.||" +
		"@str@I found a Chalice within Baxtorian Falls which contained|@str@Baxtorian's Treasure. The Chalice was floating out of reach|" +
		"@str@but I worked out how to raise the floor to get to it. I then|@str@managed to obtain the treasure by swapping it for Glarial's|" +
		"@str@Ashes.||@red@QUEST COMPLETE!",
}

const (
	strikeStart = "@str@I spoke to Almera in a house close to the Baxtorian|@str@waterfall. Her son was missing so I offered to help find|" +
		"@str@him. The boy, Hudon's looking for treasure in the waterfall.||"
	strikeHudon = "@str@I found Hudon a short raft ride down the river. He is|@str@convinced there is treasure here somewhere. Maybe I|" +
		"@str@need to do a little research.||"
	strikeBook = "@str@I found a book in the tourist office about Baxtorian. The|@str@book tells of a sad love story about 2 elf lovers. It ends|" +
		"@str@with Baxtorian withdrawing to his home under the waterfall|@str@after his wife dies. It is told that only Glarial could enter|" +
		"@str@their home.||"
	strikeTomb = "@str@The book also mentions Glarial's tomb and a pebble, it|@str@appears that the pebble is used to enter the tomb.|" +
		"@str@From what I understand Glarial's pebble was hidden in a|@str@cave under the Tree Gnome Village by Golrie's ancestors.||"
)

func journalLines(text string) []string { return strings.Split(text, "|") }

func TestParseJournalReadsEveryStageFromTheServersOwnText(t *testing.T) {
	for want, text := range journalText {
		got, ok := ParseJournal(journalLines(text))
		if !ok || got != want {
			t.Errorf("stage %d: ParseJournal = %d, %v", want, got, ok)
		}
		// The whole string as one line (a client that did not split) reads the same.
		if got, ok := ParseJournal([]string{text}); !ok || got != want {
			t.Errorf("stage %d as one line: ParseJournal = %d, %v", want, got, ok)
		}
	}
}

func TestParseJournalFailsClosed(t *testing.T) {
	for _, lines := range [][]string{nil, {}, {""}, {"Please wait..."}, {"A future server journal phrase."}} {
		if s, ok := ParseJournal(lines); ok {
			t.Errorf("ParseJournal(%q) = %d, want unknown", lines, s)
		}
	}
}

func TestGolriePebbleFoundReadsTheStruckLine(t *testing.T) {
	if GolriePebbleFound(journalLines(journalText[StageReadBook])) {
		t.Error("the unstruck stage-3 line read as the pebble already found")
	}
	struck := strings.Replace(journalText[StageReadBook],
		"@dbl@From what I understand @dre@Glarial's pebble@dbl@ was hidden",
		"@str@From what I understand Glarial's pebble was hidden", 1)
	if !GolriePebbleFound(journalLines(struck)) {
		t.Error("the struck line was not read as the pebble found")
	}
}

func TestAreaOfClassifiesEveryLandingContentTeleportsTo(t *testing.T) {
	cases := []struct {
		at   tile.Tile
		want Area
	}{
		{tile.Tile{X: 3222, Z: 3218}, AreaMainland},
		{ArdougneBank, AreaMainland},
		{Washout, AreaMainland},
		{BookcaseStand, AreaMainland},
		{MoundLanding, AreaMound},
		{SwimStand, AreaMound},
		{TreeBankLanding, AreaTreeBank},
		{DeadTreeStand, AreaTreeBank},
		{LedgeLanding, AreaLedge},
		{TGVLanding, AreaTGV},
		{GolrieAnchor, AreaTGV},
		{TombLanding, AreaTomb},
		{TombLadderStand, AreaTomb},
		{DungeonEntry, AreaDungeonSouth},
		{BaxCrateStand, AreaDungeonSouth},
		{LaneWait, AreaDungeonSouth},
		{SouthDoorSouth, AreaDungeonSouth},
		{SouthDoorNorth, AreaCorridor},
		{PuzzleDoorSouth, AreaCorridor},
		{PuzzleDoorNorth, AreaPuzzleRoom},
		{StatueStand, AreaPuzzleRoom},
		{RaisedDoorNorth, AreaRaisedRoom},
		{RaisedLanding, AreaRaisedRoom},
		{ChaliceStand, AreaRaisedRoom},
		{TombExit, AreaMainland},
		{TGVLadderStand, AreaMainland},
	}
	for _, c := range cases {
		if got := AreaOf(c.at, true); got != c.want {
			t.Errorf("AreaOf(%v) = %s, want %s", c.at, got, c.want)
		}
	}
	if AreaOf(tile.Tile{}, false) != AreaUnknown {
		t.Error("an unknown tile must classify as unknown")
	}
	for _, p := range Pillars {
		if AreaOf(PillarStand(indexOf(p)), true) != AreaPuzzleRoom {
			t.Errorf("pillar stand %v is not in the pillar room", PillarStand(indexOf(p)))
		}
	}
}

func indexOf(t tile.Tile) int {
	for i, p := range Pillars {
		if p == t {
			return i
		}
	}
	return -1
}

func TestInLaneCoversTheThreeFireGiantSpawns(t *testing.T) {
	for _, g := range []tile.Tile{{X: 2562, Z: 9886}, {X: 2565, Z: 9887}, {X: 2568, Z: 9889}} {
		if !InLane(g) {
			t.Errorf("fire giant spawn %v is not in the lane", g)
		}
	}
	for _, not := range []tile.Tile{LaneWait, SouthDoorNorth, DungeonEntry, BaxCrateStand} {
		if InLane(not) {
			t.Errorf("%v read as the lane", not)
		}
	}
}
