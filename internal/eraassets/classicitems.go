package eraassets

import (
	"fmt"

	"rsb0t-go/internal/archive"
)

// RuneScape Classic's ITEM icons — the owner's "all obj", as far as the art goes.
//
// media58.jag carries fourteen more sprite sheets beside the interface ones
// classicsprite.go already decodes: objects1.dat .. objects14.dat. They are the same
// format, so nothing new is parsed here; this file is the sheet list and the entry point.
//
// WHERE THE FRAME COUNTS COME FROM, because a wrong count is the one failure this format
// cannot detect on its own — it would read the next sprite's metadata as this one's and
// produce plausible-looking nonsense. They are not copied from anywhere and not guessed:
// each sheet's frame count is DERIVED by consuming frames until the .dat's pixel data is
// exactly exhausted, and every sheet lands on its last byte at exactly 30 frames.
// classicitems_test.go runs that derivation as a test, and runs it over the thirteen
// interface sheets too, where it reproduces the ported table's own counts (1, 6, 1, 1, 3,
// 8, 1, 1, 1, 2, 2, 4, 2) — which is what makes the method, and therefore the 30, a
// measurement rather than an assumption.
//
// WHAT THIS DOES NOT GIVE YOU, stated here so nobody wires it to a 2004 inventory and
// assumes the icons are right. This is 420 frames in sheet order. It is NOT a map from an
// item to its icon:
//
//   - A Classic item's icon is `itemSprite[id]` in config85.jag's integer.dat, and 420
//     sprites serve roughly three times as many item definitions, so many items share one.
//     Nothing here knows which.
//   - config85.jag IS readable and its decoder is NOT missing. The archive holds exactly
//     two entries, string.dat (174,875 B) and integer.dat (62,392 B), and string.dat opens
//     with the item names in item-id order — id 0 "Iron Mace", 10 "Coins", 14 "Logs",
//     measured from the operator's own file. The field ORDER of integer.dat (how many
//     items, where npc/loc definitions begin, which field is itemSprite) is what still has
//     to be ported, from 2003scape/rsc-c's game_data.c — the same repository the operator's
//     cache itself came from, named in their own era-assets/classic/PROVENANCE.txt.
//   - And a 2004 item still has to be matched to a Classic one by NAME once those names
//     exist ("Raw shrimps" against "Raw Shrimp"), which is its own list with its own
//     misses. Until both halves exist the Classic skin keeps drawing 2004 item icons,
//     which is what its manifest declares as the fallback.
//
// So: the art is decodable today and proven to decode; the KEY is the missing half.

// classicItemSheets are media58.jag's item icon sheets. Thirty frames each, derived by
// exact consumption (see above) — every sheet on a 48x32 plane with a 39-entry palette.
var classicItemSheets = func() []struct {
	name   string
	frames int
} {
	out := make([]struct {
		name   string
		frames int
	}, 0, 14)
	for i := 1; i <= 14; i++ {
		out = append(out, struct {
			name   string
			frames int
		}{fmt.Sprintf("objects%d", i), classicItemFramesPerSheet})
	}
	return out
}()

// classicItemFramesPerSheet is the derived count. See the file comment.
const classicItemFramesPerSheet = 30

// ClassicItemIconCount is how many item icons the Classic cache holds in total, which is
// what a caller wanting to know whether the art is there should check.
const ClassicItemIconCount = 14 * classicItemFramesPerSheet

// DecodeClassicItems decodes every item icon in media58.jag, in sheet order: objects1's
// frame 0 is index 0, objects2's frame 0 is index 30, and so on. The returned sprites
// carry the same fields the chrome sprites do; Name is the sheet ("objects7") and Frame
// the frame within it, so an index into this slice is reproducible from the file alone.
func DecodeClassicItems(media58 []byte) ([]ClassicSprite, error) {
	jf, err := archive.ParseJagFile(media58)
	if err != nil {
		return nil, fmt.Errorf("media58.jag: %w", err)
	}
	index, err := jf.Read("index.dat")
	if err != nil {
		return nil, fmt.Errorf("media58.jag index.dat: %w", err)
	}
	if len(index) == 0 {
		return nil, fmt.Errorf("media58.jag has no index.dat, so no item icon in it can be placed or coloured")
	}
	out := make([]ClassicSprite, 0, ClassicItemIconCount)
	for _, sheet := range classicItemSheets {
		body, err := jf.Read(sheet.name + ".dat")
		if err != nil {
			return nil, fmt.Errorf("media58.jag %s.dat: %w", sheet.name, err)
		}
		if len(body) == 0 {
			return nil, fmt.Errorf("media58.jag has no %s.dat", sheet.name)
		}
		frames, err := decodeClassicSprite(sheet.name, body, index, sheet.frames)
		if err != nil {
			return nil, err
		}
		out = append(out, frames...)
	}
	return out, nil
}
