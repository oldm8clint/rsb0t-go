package spritepack

import (
	"bytes"
	"fmt"
	"image/png"

	"rsb0t-go/internal/archive"
)

// skillIconFrame names which (sprite family, frame index) inside media.jag draws each skill's
// real in-game icon. Identified by hand this session against a real media.jag fetched from a
// running local engine (GET /media<crc>, crc = index 4 of /crc0) and decoded with DecodeFamily,
// by eye, against unmistakable RS iconography — sword, wizard hat, anvil, and so on — not
// guessed from position. See docs/control_panel.md and this session's COLLAB.md entry for the
// full account, including which skills this deliberately leaves OUT.
//
// Left out on purpose, not forgotten: Cooking, Woodcutting, Crafting, Herblore, Agility,
// Farming and Runecraft. "staticons"/"staticons2" hold a mix of clear icons and a handful of
// plain colour/texture swatches with no recognisable glyph (plus, in staticons2's case, a long
// run of frames that decode as fully transparent — almost certainly this decoder reading past
// the family's real content into unrelated bytes, since the format has no explicit frame count)
// — assigning any of those seven skills to a guessed frame risked shipping a wrong icon next to
// a real level, which is worse than the honest monogram-tile fallback the caller uses for a
// skill with no entry here. Pinning down the rest needs either the actual LostCityRS/Client
// source (not present in this checkout) confirming the true index order, or a more careful
// pass distinguishing genuine-but-abstract icons from decode overrun — a good follow-up task.
//
// Superseded in practice for most skills: internal/controlpanel/web/skillicons/ now bundles 20
// of 21 real skill icons (all but Farming, which this content build's revision predates), a
// different session's own extraction from a rasterised sprite sheet rather than this package's
// live media.jag decode. account.js tries this package's live-decoded /api/sprites/skill first
// (in case a future -media run identifies a skill this map doesn't have, or corrects one), then
// that static bundle, then the monogram tile — see account.js's own comment at the call site for
// the exact order. This map is kept, not deleted, because it is real working infrastructure for
// decoding the format from a live engine (proven live this session) and is still the only source
// for anything not already in the static bundle, should that ever change.
var skillIconFrame = map[string]struct {
	family string
	index  int
}{
	"Attack":     {"staticons", 0},
	"Defence":    {"staticons", 2},
	"Strength":   {"staticons2", 0},
	"Hitpoints":  {"staticons", 6},
	"Ranged":     {"staticons", 3},
	"Prayer":     {"staticons", 4},
	"Magic":      {"staticons", 5},
	"Fletching":  {"staticons", 11},
	"Fishing":    {"staticons", 14},
	"Firemaking": {"staticons", 16},
	"Smithing":   {"staticons", 13},
	"Mining":     {"staticons", 12},
	"Thieving":   {"staticons", 9},
	"Slayer":     {"staticons2", 1},
}

// SkillIconNames lists every skill name skillIconFrame covers, for callers that want to know
// what's available without attempting every skill and checking for a miss.
func SkillIconNames() []string {
	out := make([]string, 0, len(skillIconFrame))
	for name := range skillIconFrame {
		out = append(out, name)
	}
	return out
}

// SkillIconPNGs decodes every real icon this package knows the frame for out of media (parsed
// from a fetched media.jag — see this file's own doc comment for how to get one), returning
// skill name -> PNG-encoded bytes. A family this build's media.jag doesn't have (wrong archive,
// or a server whose media crc doesn't match what this map was built against) is skipped with
// its error attached, not fatal — callers get real icons for whatever did decode.
func SkillIconPNGs(media *archive.JagFile) (map[string][]byte, error) {
	families := map[string][]Frame{}
	out := make(map[string][]byte, len(skillIconFrame))
	var firstErr error
	for skill, ref := range skillIconFrame {
		frames, ok := families[ref.family]
		if !ok {
			var err error
			frames, err = DecodeFamily(media, ref.family)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				families[ref.family] = nil
				continue
			}
			families[ref.family] = frames
		}
		if ref.index < 0 || ref.index >= len(frames) {
			continue
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, frames[ref.index].Image()); err != nil {
			return out, fmt.Errorf("spritepack: encoding %s's icon: %w", skill, err)
		}
		out[skill] = buf.Bytes()
	}
	if len(out) == 0 && firstErr != nil {
		return out, firstErr
	}
	return out, nil
}
