package randomevents

import (
	"fmt"
	"strings"

	"rsb0t-go/internal/gamestate"
)

// genieLifetimeTicks bounds how long a Genie can stand: macro_geni's ai_timer fires every 20
// ticks (antimacro.npc timer=20), nags three times and fail-teleports its owner on the fourth
// (macro_event_genie.rs2), so a Genie is gone about 80 ticks after it spawned. The rest is
// margin for the claim and disappear queues.
const genieLifetimeTicks = 100

// oldManLifetimeTicks is the same bound for the Mysterious Old Man, whose macro_mage timer and
// nag-then-fail sequence are the Genie's (antimacro.npc timer=20,
// macro_event_mysterious_old_man.rs2).
const oldManLifetimeTicks = 100

// dwarfLifetimeTicks bounds the Drunken Dwarf's friendly life: macro_dwarf's timer fires every
// 10 ticks and it turns hostile on the fifth (macro_event_drunken_dwarf.rs2), whistling to its
// owner at the spawn and with each nag before that. Hostile, it attacks its owner, and faces
// it, so it needs no sound to be told apart.
const dwarfLifetimeTicks = 60

// ownerSound is the sound a dialog event NPC's content plays only to its owner.
type ownerSound struct {
	id, windowTicks int
	name            string
}

// ownerSounds lists, per dialog event NPC, the sound content plays with sound_synth in the
// OWNER's script context (the spawn procs run in the owner's timer script; the nags and
// macro_event_lost's teleport-back run after finduid(%npc_macro_event_target) has made the
// owner the active player), so no bystander ever receives it. See genieMayBeOurs for why an
// unattributable NPC is gated on it and why there is no fallback.
//
// The Mysterious Old Man and the Drunken Dwarf crowd the same way the Genie did. Across the
// archived 2026-09-21..23 fleet logs plus c0da98ee's first five hours, 1,847 Old Man and
// 1,581 Dwarf rejections came from accounts that had seen neither a greeting nor a face on
// another player when they walked over (998 of the Dwarf ones had already talked to it:
// "You're not my matey!"). Both of those NPCs also name their owner in every greeting and
// nag, so their owners do not depend on the sound either.
var ownerSounds = map[string]ownerSound{
	"genie":              {gamestate.SoundGenieAppear, genieLifetimeTicks, "genie_appear"},
	"mysterious old man": {gamestate.SoundSmokepuff, oldManLifetimeTicks, "smokepuff"},
	"drunken dwarf":      {gamestate.SoundDwarfWhistle, dwarfLifetimeTicks, "dwarf_whistle"},
}

func heardOwnerSound(state *gamestate.State, snd ownerSound) (heard, anySounds bool) {
	return state.HeardSoundNear(snd.id, state.Tick()-snd.windowTicks/2, snd.windowTicks/2)
}

// genieMayBeOurs reports whether a Genie whose owner this account cannot otherwise tell — no
// greeting naming anyone, and not facing another player — could be this account's.
//
// Content plays genie_appear with sound_synth inside [proc,macro_geni_spawn], which runs in the
// owner's own script context, so only the owner hears it (lab-checked 2026-09-23 by
// cmd/crowdgenietest: the owner heard it, six bystanders standing on its tile did not). A Genie
// in view with no genie_appear heard in its whole lifetime is someone else's, and there is
// nothing to walk over and ask.
//
// The live cost of asking anyway (3dfb543a, 2026-09-23 02:58:26): 199 Fishing Guild fishers
// saw a non-fleet player's Genie at the moment its first nag replaced its spawn greeting.
// The nag did not parse (see speechOwner) and nothing this client had been told showed the
// Genie facing another player, so every one of them walked to it, and 24 talked to it, before
// a face update a moment later (slot 625, the owner's) let them reject it. An account that
// walks back into view of a Genie is in that position again: overhead text is not repeated
// for a new observer, and on the live server the face evidently is not either — acct31 and progress
// took the same Genie up a second time 14 and 19 seconds later, on their way back from the
// bank, and rejected it only once a later nag or face update arrived.
//
// Unlike plantMayBeOurs there is deliberately no "no sounds at all" fallback. The owner does
// not depend on the sound: every Genie greeting and nag names its owner, so an owner whose
// sound went missing still takes its Genie up on the next nag, three of which come before the
// fail-teleport. A fallback would only put back the crowd walk for every account that had
// happened to hear nothing yet.
func genieMayBeOurs(state *gamestate.State) bool {
	heard, _ := heardOwnerSound(state, ownerSounds["genie"])
	return heard
}

// unknownOwnerMayBeOurs is the owner-sound gate for a dialog NPC Detect cannot attribute from
// its greeting or its face: the Genie, the Mysterious Old Man and the Drunken Dwarf (see
// ownerSounds). Every other name keeps the old check-it-in-person behaviour (the Strange
// plant's own gate is plantMayBeOurs).
func unknownOwnerMayBeOurs(state *gamestate.State, name string) bool {
	snd, ok := ownerSounds[strings.ToLower(name)]
	if !ok {
		return true
	}
	heard, _ := heardOwnerSound(state, snd)
	return heard
}

// ownerSoundEvidence describes the owner-sound evidence for a rejection's log line, "" for an
// NPC without one.
func ownerSoundEvidence(state *gamestate.State, name string) string {
	snd, ok := ownerSounds[strings.ToLower(name)]
	if !ok {
		return ""
	}
	heard, anySounds := heardOwnerSound(state, snd)
	return fmt.Sprintf("%s heard=%v, sounds seen=%v", snd.name, heard, anySounds)
}
