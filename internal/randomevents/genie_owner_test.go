package randomevents

import (
	"strings"
	"testing"

	"rsb0t-go/internal/gamestate"
)

// heardOwnGenie gives s the one signal only a Genie's owner receives: content's spawn proc
// plays genie_appear in the owner's script context. Tests that use a Genie of unknown owner as
// "a dialog NPC this account may own" need it, because without it the Genie is someone else's.
func heardOwnGenie(s *gamestate.State) {
	playSound(s, gamestate.SoundGenieAppear)
}

// heardOwnEvent is heardOwnGenie for any dialog event NPC with an owner-only sound (see
// ownerSounds); for every other name it does nothing.
func heardOwnEvent(s *gamestate.State, name string) {
	if snd, ok := ownerSounds[strings.ToLower(name)]; ok {
		playSound(s, snd.id)
	}
}

const genieType = 500

func withGenie(t *testing.T) {
	t.Helper()
	withResolver(t, fakeResolver{npcNames: map[int]string{genieType: "Genie"}})
}

// The exact overhead text every live Genie rejection quoted on 2026-09-22/23 (190 of them):
// The live server opens its first nag in lowercase, where local content has "Ehem...".
func TestProductionLowercaseGenieNagNamesItsOwner(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"ehem... master Sprinter?", "sprinter"},
		{"ehem... master Walker?", "walker"},
		{"EHEM... MISTRESS Aj_Bot?", "aj bot"},
		{"are you there, Master Big net?", "big net"},
		{"ehem... master Sprinter", ""},   // suffix still required
		{"ehem.. master Sprinter?", ""},   // so is the exact fixed text
		{"xehem... master Sprinter?", ""}, // and its position
	} {
		if got := speechOwner("Genie", tc.text); got != tc.want {
			t.Errorf("%q: owner %q, want %q", tc.text, got, tc.want)
		}
	}
}

// The live 02:58:26 state on 3dfb543a, as each of the 199 bystanders saw it: the first nag
// ("ehem... master Sprinter?") had just replaced the spawn greeting, the Genie faced nobody
// yet, and the bystander had never heard its spawn sound. It is not this account's event.
func TestLiveCrowdGenieIsNotABystandersEvent(t *testing.T) {
	withGenie(t)
	s := overlapState()
	setSelfName(t, s, "acct12")
	playSound(s, gamestate.SoundSwarmAppear) // sounds do reach this session, just not that one
	addGreetingNPC(s, genieType, -1, "ehem... master Sprinter?")
	if ev := Detect(s); ev != nil {
		t.Fatalf("a bystander took up another player's Genie: %+v", ev)
	}
	g := NewGuardian(&overlapSession{state: s}, nil)
	defer g.Stop()
	if ev := g.nextEvent(); ev != nil {
		t.Fatalf("the Guardian would handle another player's Genie: %+v", ev)
	}
}

// The same Genie seen by the account it names: the greeting alone is enough, sound or not.
func TestLiveCrowdGenieIsItsOwnersEvent(t *testing.T) {
	withGenie(t)
	s := overlapState()
	setSelfName(t, s, "sprinter")
	addGreetingNPC(s, genieType, -1, "ehem... master Sprinter?")
	if ev := Detect(s); ev == nil || ev.NpcIndex != 1 || ev.Name != "Genie" {
		t.Fatalf("the owner did not take up its own named Genie: %+v", ev)
	}
}

// acct31 and progress coming back from the bank: a Genie already in the world enters view
// with no greeting and no face. Without its spawn sound it is someone else's — with sounds
// arriving, and equally with none at all (there is no fallback; see genieMayBeOurs).
func TestUnknownGenieWithoutOurSpawnSoundIsSomeoneElses(t *testing.T) {
	withGenie(t)
	for _, sounds := range []bool{true, false} {
		s := overlapState()
		if sounds {
			playSound(s, gamestate.SoundTriffidAppear)
		}
		addNPCFacing(s, 1, genieType, 1, 0, -1)
		if ev := Detect(s); ev != nil {
			t.Fatalf("sounds seen=%v: an unattributable Genie with no genie_appear heard was taken up: %+v", sounds, ev)
		}
	}
}

// The owner that heard genie_appear still takes up a Genie it cannot otherwise attribute (for
// example after walking back into view of it), for the Genie's whole lifetime and no longer.
func TestUnknownGenieAfterOurSpawnSoundIsTakenUp(t *testing.T) {
	withGenie(t)
	s := overlapState()
	heardOwnGenie(s)
	advanceTicks(s, genieLifetimeTicks-5)
	addNPCFacing(s, 1, genieType, 1, 0, -1)
	if ev := Detect(s); ev == nil || ev.NpcIndex != 1 {
		t.Fatalf("our own Genie was ignored inside its lifetime: %+v", ev)
	}

	later := overlapState()
	heardOwnGenie(later)
	advanceTicks(later, genieLifetimeTicks+10)
	addNPCFacing(later, 1, genieType, 1, 0, -1)
	if ev := Detect(later); ev != nil {
		t.Fatalf("a Genie long after our last genie_appear was taken up: %+v", ev)
	}
}

// A Genie facing this account (playerfollow on its owner) is ours without the sound.
func TestGenieFacingSelfIsTakenUpWithoutSound(t *testing.T) {
	withGenie(t)
	s := overlapState()
	addNPCFacing(s, 1, genieType, 1, 0, 32768+5)
	if ev := Detect(s); ev == nil || ev.NpcIndex != 1 {
		t.Fatalf("a Genie facing this account was ignored: %+v", ev)
	}
}

// The gate covers the three nagging events and nothing else: a dialog NPC without an
// owner-only sound (the Sandwich Lady, the Frog) is still checked in person.
func TestUnknownOwnerGateIsTheNaggingEventsOnly(t *testing.T) {
	for _, name := range []string{"Sandwich Lady", "Frog"} {
		withResolver(t, fakeResolver{npcNames: map[int]string{501: name}})
		s := overlapState()
		playSound(s, gamestate.SoundSwarmAppear)
		addNPCFacing(s, 1, 501, 1, 0, -1)
		if ev := Detect(s); ev == nil || ev.NpcIndex != 1 {
			t.Fatalf("an unknown-owner %s is no longer checked: %+v", name, ev)
		}
	}
}

// A Genie rejection says whether the spawn sound was heard, so the next live case can be read
// rather than inferred.
func TestGenieRejectionLogsSpawnSound(t *testing.T) {
	withGenie(t)
	s := &overlapSession{state: overlapState()}
	playSound(s.state, gamestate.SoundSwarmAppear)
	addGreetingNPC(s.state, genieType, 32768+9, "ehem... master Sprinter?")
	var lines []string
	if err := rejectDialogNPC(s, newDialogEvent(1, genieType, "Genie"), func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "genie_appear heard=false, sounds seen=true") || !strings.Contains(lines[0], "greeting names sprinter") {
		t.Fatalf("logged %q", lines)
	}
}
