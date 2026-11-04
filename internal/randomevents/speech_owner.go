package randomevents

import (
	"strings"

	"rsb0t-go/internal/gamestate"
)

// Templates are from the local Lost City macro-event content, not arbitrary
// substring searches. Only these server-authored greetings establish ownership.
//
// The fixed text around the name is matched IGNORING CASE, because production's is not
// ours: The live server's Genie opens its first nag with a lowercase "ehem... master <name>?"
// where local content has "Ehem...". A case-sensitive match never parsed it: all 190
// Genie rejections in the archived 2026-09-22/23 fleet logs that quoted it logged it as
// unrecognised speech. That miss is half of why 199 Fishing Guild fishers walked to a
// non-fleet player's Genie at 02:58:26 on 3dfb543a — the nag replaced the greeting they
// had been parsing, and nothing else they had been told showed the Genie facing another
// player (genieMayBeOurs is the other half). The name itself is still read from the
// server's own text, so the match cannot widen who a greeting names.
func speechOwner(kind, speech string) string {
	if speech == "" {
		return ""
	}
	var templates [][2]string
	switch {
	case strings.EqualFold(kind, "genie"):
		templates = genieGreetings
	case strings.EqualFold(kind, "drunken dwarf"):
		templates = dwarfGreetings
	case strings.EqualFold(kind, "mysterious old man"):
		templates = mysteriousGreetings
	}
	for _, template := range templates {
		if len(speech) < len(template[0])+len(template[1]) ||
			!strings.EqualFold(speech[:len(template[0])], template[0]) ||
			!strings.EqualFold(speech[len(speech)-len(template[1]):], template[1]) {
			continue
		}
		name := strings.TrimSpace(speech[len(template[0]) : len(speech)-len(template[1])])
		if len(name) == 0 || len(name) > 12 {
			continue
		}
		valid := true
		for _, c := range name {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == ' ' || c == '_') {
				valid = false
				break
			}
		}
		if valid {
			return strings.ReplaceAll(strings.ToLower(name), "_", " ")
		}
	}
	return ""
}

var genieGreetings = [][2]string{
	{"Greetings master ", "!"}, {"Greetings mistress ", "!"},
	{"Ehem... master ", "?"}, {"Ehem... mistress ", "?"},
	{"Are you there, master ", "?"}, {"Are you there, mistress ", "?"},
	{"You shouldn't ignore a Genie master ", "?"}, {"You shouldn't ignore a Genie mistress ", "?"},
}
var dwarfGreetings = [][2]string{{"'Ello der ", "! *hic*"}, {"Aww comeon, talk to ikle me ", "!"}, {"Oi, are you der ", "!"}, {"Dunt ignore your mate, ", "!"}, {"I hates yoo ", "!"}}
var mysteriousGreetings = [][2]string{{"Aha, you're required ", "!"}, {"Greetings ", "!"}, {"Here you go ", ", it's all yours!"}, {"Aha, you'll do ", "!"}, {"ehem... Hello ", "!"}, {"Hello, are you there ", "!"}, {"It's really rude to ignore someone, ", "!"}}

// speechNamesSelf reports whether npc's own latest overhead greeting (a server-authored
// template from speechOwner's tables) names this account, and whether it names anyone at
// all. A greeting is the one ownership signal the server cannot change after the fact:
// content's ~chatnpc proc switches the NPC to playerfaceclose on WHOEVER talks to it
// (content/scripts/interface_chat/scripts/chat.rs2), so a single Talk-to from any other
// player — including someone else's bot probing an unknown owner — turns the NPC's face
// toward that player for the rest of its life, while macro_mage's ai_timer keeps nagging
// and finally fail-teleports its real owner (%npc_macro_event_target, fixed at spawn).
// Face is therefore only a fallback for NPCs that have not yet named anyone this account
// has seen.
func speechNamesSelf(state *gamestate.State, kind string, npc gamestate.NPC) (self, named bool) {
	owner := npcSpeechOwner(kind, npc)
	if owner == "" {
		return false, false
	}
	selfName, ok := state.SelfName()
	if !ok {
		return false, false
	}
	return owner == strings.ReplaceAll(strings.ToLower(selfName), "_", " "), true
}

// npcSpeechOwner is the owner npc's own overhead lines name: the newest line in its
// SpeechLog that a template parses, else its latest Speech (a snapshot built without a
// log). An event NPC's owner (%npc_macro_event_target) is fixed at spawn and every greeting
// and nag names it, so a line that names nobody must not erase an earlier one that did.
// Live 2026-09-23 07:01:31 on c0da98ee: 192 Catherby fishers walked to a Mysterious Old
// Man the moment its despawn line replaced the greeting they had been skipping it on, and
// it vanished two seconds later; the 02:58:26 Genie crowd on 3dfb543a was the same
// replacement by a nag worded differently on production.
func npcSpeechOwner(kind string, npc gamestate.NPC) string {
	for i := len(npc.SpeechLog) - 1; i >= 0; i-- {
		if owner := speechOwner(kind, npc.SpeechLog[i]); owner != "" {
			return owner
		}
	}
	return speechOwner(kind, npc.Speech)
}

// eventOverLines are what each dialog event NPC says only once its event can no longer be
// claimed by anyone (content 289): its despawn line, queued by macro_event_lost when the
// owner logged out, left the area or has a newer event, two ticks before it disappears;
// and its fail line, said as it fail-teleports its owner and deletes itself.
var eventOverLines = map[string][]string{
	"genie":              {"I lose more friends that way.", "No one ignores me!"},
	"mysterious old man": {"I lose more friends that way.", "No-one ignores me!"},
	"drunken dwarf":      {"Aww, me is all alone."},
}

// eventOver reports whether speech, an NPC's latest line, says its event is over. Nobody
// walks to it then, its owner included: there is nothing left to claim.
func eventOver(kind, speech string) bool {
	speech = strings.TrimSpace(speech)
	for _, line := range eventOverLines[strings.ToLower(kind)] {
		if strings.EqualFold(speech, line) {
			return true
		}
	}
	return false
}

// greetingNamesSelf reports whether ev's NPC is still in view and its greeting names this
// account. The guardian's two-minute rejection window yields to it: a rejection made from
// the face alone (before any greeting was seen, or after another player's Talk-to turned
// the NPC) must not outlive the server naming this account as the owner, since every
// macro_mage nag after the first repeats the owner's name.
func greetingNamesSelf(state *gamestate.State, ev *Event) bool {
	for _, npc := range state.NPCs() {
		if npc.Index == ev.NpcIndex && npc.Type == ev.NpcType {
			self, _ := speechNamesSelf(state, ev.Name, npc)
			return self
		}
	}
	return false
}
