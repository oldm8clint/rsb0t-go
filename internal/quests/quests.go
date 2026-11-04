// Package quests answers "what quest state does the protocol actually expose",
// following the investigation this package's own COLLAB.md task asked for: classic RS
// quest progress is not a dedicated packet family, and — the real finding here — it is
// mostly not a readable varp either, despite VARP_SMALL/VARP_LARGE/VARP_SYNC now being
// decoded generically in internal/gamestate (see gamestate/varp.go).
//
// Content marks each varp `transmit=yes` or leaves it untransmitted (the default), and
// the engine's Player.setVar only ever writes VARP_SMALL/VARP_LARGE to the client when
// transmit is set (engine/src/engine/entity/Player.ts). Checked directly against
// content/scripts/quests/*/configs/*.varp: individual quest-progress variables like
// `cookquest` (Cook's Assistant), `zanaris`, `waterfall_quest` and
// `elemental_workshop_bits` are all `scope=perm` with no `transmit=yes` line — they
// never cross the wire at all, so gamestate.State.Varp for one of these ids reads back
// 0 forever, indistinguishable from "quest not started". This is documented in the
// vendored client's own rsb0t-main/docs/decisions/quest-state-not-varps.md and
// rsb0t-main/docs/reference/api-quests.md, and this package's live proof
// (cmd/questtest) reproduces it directly against the local engine: `::setvar cookquest
// 2` (the same var Cook's Assistant's own completion script writes) changes it
// server-side, but gamestate.State.Varp(29)/VarpKnown(29) never observe that change,
// before or after — confirmed never-transmitted, not just never-yet-sent.
//
// What IS real and readable, also confirmed live by cmd/questtest:
//   - QuestPointsVarp (id 101, content name `qp`) is one of the rare
//     `transmit=yes` exceptions — general/scripts/quests.rs2's update_questpoints proc
//     recomputes and writes it via a plain %qp assignment whenever total quest points
//     change, so completing any quest does produce a real VARP_SMALL/VARP_LARGE the
//     client can see. Points() reads exactly this; cmd/questtest exercises both wire
//     encodings (`::setvar qp 7` for VARP_SMALL, `::setvar qp 5000` to force
//     VARP_LARGE) and confirms gamestate decodes both correctly.
//   - Per-quest stage comes from the quest tab itself: send_quest_progress_colour
//     (general/scripts/quests.rs2) sets each quest's tab entry to
//     red/yellow/green (not-started/in-progress/complete) via IF_SETCOLOUR on that
//     quest's own text component in the questlist interface — never a varp. Status()
//     reads that instead, via gamestate.State.Colour and the component-id table below.
//     login_logout/login.rs2's initalltabs proc calls update_questlist
//     unconditionally on every login, resending every quest's colour from current var
//     state — cmd/questtest confirms this live too: Cook's Assistant reads
//     red/NotStarted on first login, and after `::setvar cookquest 2` plus a real
//     second login (the engine's own account-busy cooldown and all), reads
//     green/Complete, driven entirely by the real script re-running against the
//     changed var, not by this tool injecting anything itself.
//
// A "quest bitfield/stage" API (matching @rsb0t/api's richer per-quest stage
// integers) is not buildable from the client side alone: it needs Content to mark the
// specific stage varp(s) transmit=yes first, same conclusion the vendored docs already
// reached. Status() here matches @rsb0t/api's own Quests.status: coarse
// notStarted/inProgress/complete/unknown, nothing finer.
package quests

import "rsb0t-go/internal/gamestate"

// QuestPointsVarp is player varp id 101 (content name `qp`), the total-quest-points
// counter. Confirmed against content/pack/varp.pack ("101=qp") and
// general/scripts/quests.rs2's update_questpoints proc, which is the only place %qp is
// ever written. One of the few quest-related varps actually marked transmit=yes.
const QuestPointsVarp = 101

// QuestStatus mirrors the quest tab's own three-colour classification — the same
// coarse granularity @rsb0t/api's Quests.status exposes (its 'unknown' covers both
// "we don't track this quest's component id" and "no IF_SETCOLOUR observed for it
// yet").
type QuestStatus string

const (
	NotStarted QuestStatus = "notStarted"
	InProgress QuestStatus = "inProgress"
	Complete   QuestStatus = "complete"
	Unknown    QuestStatus = "unknown"
)

// componentIDs maps a lower-cased quest name to its text component id inside the questlist
// interface. It is built from questRows (table.go): all 68 rows of content 289's
// player/interfaces/questlist.if with the ids content/pack/interface.pack gives them.
//
// Nine of those ids were first found live against the local engine's interface archive with
// cmd/questtest's -dump mode, and the pack gives the same nine: cook's assistant 7333,
// black knight's fortress 7332, demon slayer 7334, the restless ghost 7337, imp catcher 7340,
// pirate's treasure 7341, romeo & juliet 7343, sheep shearer 7344 and vampire slayer 7347.
// The other 59 come from the pack and have not been read off a live quest tab yet.
var componentIDs = buildComponentIDs()

// ComponentID returns the questlist text component id for a quest name (case
// -insensitive, matching the label text content/scripts gives it), and false when this
// package doesn't have an entry for it yet.
func ComponentID(name string) (int, bool) {
	id, ok := componentIDs[normalise(name)]
	return id, ok
}

// Known lists every quest name this package can currently resolve a component id for.
func Known() []string {
	out := make([]string, 0, len(componentIDs))
	for name := range componentIDs {
		out = append(out, name)
	}
	return out
}

func normalise(name string) string {
	out := make([]byte, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

// Classify turns a decoded IF_SETCOLOUR value into a QuestStatus, matching
// send_quest_progress_colour's own red/yellow/green rule (general/scripts/quests.rs2):
// progress==0 -> red, progress>=complete -> green, else yellow. The wire format is
// lossy 5-bit-per-channel (see gamestate.Colour's doc comment), so this checks which
// channels are lit rather than exact values — content's own red_rgb/yellow_rgb/
// green_rgb (0xFF0000/0xFFFF00/0x00FF00, general/configs/colour.constant) all round-trip
// through 5-bit quantization to exactly the patterns matched below.
func Classify(c gamestate.Colour) QuestStatus {
	switch {
	case c.R > 0 && c.G == 0 && c.B == 0:
		return NotStarted
	case c.R > 0 && c.G > 0 && c.B == 0:
		return InProgress
	case c.R == 0 && c.G > 0 && c.B == 0:
		return Complete
	default:
		return Unknown
	}
}

// Points returns the account's total quest points (content name `qp`, varp 101) — see
// QuestPointsVarp's doc comment for why this one varp is trustworthy where per-quest
// stage varps are not.
func Points(state *gamestate.State) int {
	return state.Varp(QuestPointsVarp)
}

// Status returns name's current coarse status by reading the questlist tab's own
// IF_SETCOLOUR state, not a varp (see this package's doc comment for why). false means
// this package has no component-id entry for name — not that the quest is unknown to
// the server.
func Status(state *gamestate.State, name string) (QuestStatus, bool) {
	comID, ok := ComponentID(name)
	if !ok {
		return Unknown, false
	}
	colour, ok := state.Colour(comID)
	if !ok {
		// In practice this should be rare: login_logout/login.rs2's initalltabs proc
		// calls update_questlist unconditionally on every login, which re-sends
		// send_quest_progress_colour (and so a real IF_SETCOLOUR) for every quest in
		// the table above, confirmed live by cmd/questtest across two separate real
		// logins of the same account. This branch is only reachable before that first
		// login packet burst arrives, or if a future content build drops a quest from
		// update_questlist's call list — Unknown rather than NotStarted either way,
		// since this package cannot tell "still red" from "never told" apart.
		return Unknown, true
	}
	return Classify(colour), true
}
